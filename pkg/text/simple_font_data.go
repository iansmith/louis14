package text

import (
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

// fontPlatformData is the size and orientation a font is instantiated at.
// Mirrors the parts of Blink's FontPlatformData that SimpleFontData reads
// here: size() and Orientation() (platform/fonts/font_platform_data.h:105 @
// 5992657f556bcd9d207a4aca187488462f027cab).
type fontPlatformData struct {
	size        float64
	orientation css.FontOrientation
}

// simpleFontData is one font at one size: the per-font record the
// font-relative units read. Mirrors Blink's SimpleFontData
// (platform/fonts/simple_font_data.h @
// 5992657f556bcd9d207a4aca187488462f027cab), with zeroGlyph its zero_glyph_
// (:209) and zeroAdvance standing in for WidthForGlyph(zero_glyph_). louis14's
// zeroAdvance is the shaped, LayoutUnit-ceiled MeasureText("0"), where Blink
// reads the unshaped float advance — equal for advances on the 1/64 px grid.
type simpleFontData struct {
	platformData fontPlatformData
	zeroGlyph    Glyph
	zeroAdvance  float64 // valid only when zeroGlyph != 0
	xHeight      float64
	capHeight    float64
}

// ZeroInlineSize is the inline advance of "0", the font's FontMetrics
// ZeroWidth. Mirrors SimpleFontData::ZeroInlineSize (platform/fonts/
// simple_font_data.cc:561-598 @ 5992657f556bcd9d207a4aca187488462f027cab):
// with a "0" glyph, its advance (even 0) unless upright; with none, 0.5em, or
// 1em upright (ZeroInlineSizeFallback).
func (d simpleFontData) ZeroInlineSize() float64 {
	if d.zeroGlyph == 0 {
		return css.ZeroInlineSizeFallback(d.platformData.size, d.platformData.orientation)
	}
	if d.platformData.orientation != css.FontOrientationVerticalUpright {
		return d.zeroAdvance
	}
	// Blink returns the vmtx OpenTypeVerticalData::AdvanceHeight of the "0"
	// glyph here (:572-576); louis14 keeps 1em until LOU-424 ports it.
	return d.platformData.size
}
