package text

import (
	"sync"

	"louis14/pkg/css"

	"mazarin/textshape"
)

// Glyph is a glyph id within one font; 0 means the font has no glyph for the
// character (it maps to .notdef). Mirrors Blink's Glyph (platform/fonts/
// glyph.h:37 @ 5992657f556bcd9d207a4aca187488462f027cab).
type Glyph uint16

// The probe characters of the per-metric primary fonts. Mirror Blink's
// uchar::kSpace / kDigitZero / kCjkWater (platform/wtf/text/
// character_names.h:48/:56/:194 @ 5992657f556bcd9d207a4aca187488462f027cab).
const (
	kSpace     rune = 0x0020
	kDigitZero rune = 0x0030
	kCjkWater  rune = 0x6C34
)

// glyphForCharacter returns the glyph the font at fontPath maps c to, or 0
// when it has none. Mirrors SimpleFontData::GlyphForCharacter
// (platform/fonts/simple_font_data.cc:252 @
// 5992657f556bcd9d207a4aca187488462f027cab), which reads HarfBuzz's
// character-to-glyph mapping so that coverage matches shaping. louis14 gets
// the same answer by shaping c alone through the layout that owns the font's
// FontID — the one MeasureText shapes with — so the check holds when
// SetTextLayout installed a layout with its own provider. A font that fails
// to open has no glyph, as PlatformGlyphInit zeroes zero_glyph_ for a
// typeface with no glyphs (:230-234).
func glyphForCharacter(fontPath string, fontSize float64, c rune) Glyph {
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return 0
	}
	run, err := getLayout().ShapeText(textshape.ShapingParams{Text: string(c), FontID: m.FontID})
	if err != nil || len(run.Glyphs) == 0 {
		return 0
	}
	return Glyph(run.Glyphs[0].GID)
}

// glyphAdvance returns c's glyph in the font at fontPath and, when the font
// has one, its advance (0 otherwise). Mirrors Blink's GlyphForCharacter then
// WidthForGlyph pairing for "0" (zero_glyph_ set at :243, read by
// ZeroInlineSize at :571) and for 水 (IdeographicAdvanceWidth :442-443), in
// platform/fonts/simple_font_data.cc @
// 5992657f556bcd9d207a4aca187488462f027cab.
func glyphAdvance(fontPath string, fontSize float64, c rune) (Glyph, float64) {
	g := glyphForCharacter(fontPath, fontSize, c)
	if g == 0 {
		return 0, 0
	}
	w, _ := MeasureText(string(c), fontSize, fontPath)
	return g, w.Float64()
}

// fontPlatformData is the font, size and orientation a font is instantiated
// at. Mirrors the parts of Blink's FontPlatformData that SimpleFontData reads
// here: the typeface (louis14's font path), size() and Orientation()
// (platform/fonts/font_platform_data.h:94/:105 @
// 5992657f556bcd9d207a4aca187488462f027cab).
type fontPlatformData struct {
	fontPath    string
	size        float64
	orientation css.FontOrientation
}

// ideographicAdvance is the lazily filled 水 glyph and advance of one cached
// font: Blink's ideographic_advance_width_once_ / ideographic_advance_width_
// pair (platform/fonts/simple_font_data.h:217/:220 @
// 5992657f556bcd9d207a4aca187488462f027cab), plus the glyph
// IdeographicAdvanceHeight reads. Copies of a simpleFontData share it, so the
// probe runs once per cached font, and only when an ic unit (or
// font-size-adjust ic-*) asks for it.
type ideographicAdvance struct {
	once  sync.Once
	glyph Glyph
	width float64
	ok    bool
}

// simpleFontData is one font at one size: the per-font record the
// font-relative units read. Mirrors Blink's SimpleFontData
// (platform/fonts/simple_font_data.h @
// 5992657f556bcd9d207a4aca187488462f027cab), with zeroGlyph its zero_glyph_
// (:209) and zeroAdvance standing in for WidthForGlyph(zero_glyph_), and
// ideographic its std::optional<float> ideographic_advance_width_ (:220).
// louis14's advances are the shaped, LayoutUnit-ceiled MeasureText of the
// character, where Blink reads the unshaped float advance — equal for
// advances on the 1/64 px grid.
type simpleFontData struct {
	platformData fontPlatformData
	zeroGlyph    Glyph
	zeroAdvance  float64 // valid only when zeroGlyph != 0
	ideographic  *ideographicAdvance
	xHeight      float64
	capHeight    float64
}

// IdeographicAdvanceWidth is the advance of 水 (U+6C34), present only when
// the font has the 水 glyph — even when the advance is 0. Mirrors
// SimpleFontData::IdeographicAdvanceWidth (platform/fonts/
// simple_font_data.cc:437-447 @ 5992657f556bcd9d207a4aca187488462f027cab),
// which fills its optional once (std::call_once), only when
// GlyphForCharacter(kCjkWater) is non-zero.
func (d simpleFontData) IdeographicAdvanceWidth() (float64, bool) {
	a := d.ideographic
	a.once.Do(func() {
		if g, w := glyphAdvance(d.platformData.fontPath, d.platformData.size, kCjkWater); g != 0 {
			a.glyph, a.width, a.ok = g, w, true
		}
	})
	return a.width, a.ok
}

// IdeographicAdvanceHeight is the vertical advance of 水 (U+6C34), present only
// when the font has the 水 glyph: the HarfBuzzFace's
// OpenTypeVerticalData::AdvanceHeight of that glyph, its vmtx advance or, with
// no vmtx, the FontMetrics Height fallback. Mirrors
// SimpleFontData::IdeographicAdvanceHeight (platform/fonts/
// simple_font_data.cc:449-459 @ 5992657f556bcd9d207a4aca187488462f027cab).
// Blink fills it once per SimpleFontData, which is per exact size; louis14's
// cache entry is shared across the exact sizes of one rounded size, so the 水
// glyph is found once (with the width) and the advance is scaled per call.
func (d simpleFontData) IdeographicAdvanceHeight() (float64, bool) {
	if _, ok := d.IdeographicAdvanceWidth(); !ok {
		return 0, false
	}
	return d.platformData.GetHarfBuzzFace().VerticalData().AdvanceHeight(d.ideographic.glyph), true
}

// IdeographicInlineSize is the optional inline advance of 水, the value the
// ic unit reads: IdeographicAdvanceWidth, or IdeographicAdvanceHeight when
// the orientation is upright. Mirrors SimpleFontData::IdeographicInlineSize
// (platform/fonts/simple_font_data.cc:461-475 @
// 5992657f556bcd9d207a4aca187488462f027cab). Not cached, unlike Blink's
// ideographic_inline_size_once_: louis14's cache entry is shared across
// orientations.
func (d simpleFontData) IdeographicInlineSize() (float64, bool) {
	if d.platformData.orientation != css.FontOrientationVerticalUpright {
		return d.IdeographicAdvanceWidth()
	}
	return d.IdeographicAdvanceHeight()
}

// ZeroInlineSize is the inline advance of "0", the font's FontMetrics
// ZeroWidth. Mirrors SimpleFontData::ZeroInlineSize (platform/fonts/
// simple_font_data.cc:561-598 @ 5992657f556bcd9d207a4aca187488462f027cab):
// with a "0" glyph, its advance (even 0), or upright its vertical advance —
// the HarfBuzzFace's OpenTypeVerticalData::AdvanceHeight (:572-576), the vmtx
// advance or, with no vmtx, the FontMetrics Height fallback; with no glyph,
// 0.5em, or 1em upright (ZeroInlineSizeFallback).
func (d simpleFontData) ZeroInlineSize() float64 {
	if d.zeroGlyph == 0 {
		return css.ZeroInlineSizeFallback(d.platformData.size, d.platformData.orientation)
	}
	if d.platformData.orientation != css.FontOrientationVerticalUpright {
		return d.zeroAdvance
	}
	return d.platformData.GetHarfBuzzFace().VerticalData().AdvanceHeight(d.zeroGlyph)
}
