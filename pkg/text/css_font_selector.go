package text

import (
	"math"

	"louis14/pkg/css"
)

// CSSFontSelector builds an element's font for the cascade from a
// FontConfig (builtin families plus the @font-face Registry) and implements
// css.FontSelector. Mirrors Blink's CSSFontSelector
// (core/css/css_font_selector.h:45 @ 5992657f556bcd9d207a4aca187488462f027cab),
// the document's font lookup over its @font-face cache (GetFontFaceCache
// :64 — louis14's FontConfig.Registry). Each font's glyph presence and
// measurements are cached per (font, rounded size), the analog of
// SimpleFontData computing them once per font. One selector serves one
// cascade; it is not safe for concurrent use.
type CSSFontSelector struct {
	fc    FontConfig
	cache map[fontKey]simpleFontData
}

// fontKey keys the simpleFontData cache. Sub-pixel size jitter is rare;
// rounding the size stabilises the cache for documents rendering many
// elements at one size (openFont rounds it the same way).
type fontKey struct {
	path     string
	fontSize int32
}

// NewCSSFontSelector returns a selector over fc with an empty metric cache.
func NewCSSFontSelector(fc FontConfig) *CSSFontSelector {
	return &CSSFontSelector{fc: fc, cache: make(map[fontKey]simpleFontData)}
}

// CreateFont selects s's per-probe primary fonts from its font properties
// and returns their metrics at the CSS Fonts 5 §1.7 font-size-adjust used
// size. Mirrors Blink's FontBuilder::CreateFont (core/css/resolver/
// font_builder.cc:656): UpdateAdjustedSize (:378) derives the used size from
// the aspect value of Font::PrimaryFont() (:397) — the first font covering
// U+0020 — for every font-size-adjust metric. The unit metrics then come from
// the primary font for each probe character, as
// CSSToLengthConversionData::FontSizes reads them (core/css/
// css_to_length_conversion_data.cc): x-height (Ex :97) and cap-height (Cap
// :171) from PrimaryFont(); the "0" advance (Ch :119) from
// PrimaryFontWithDigitZero() — the first font covering U+0030, whose
// ZeroWidth is SetZeroWidth(ZeroInlineSize()) (platform/fonts/
// simple_font_data.cc:245); and the optional 水 advance (Ic :141) from
// PrimaryFontWithCjkWater()'s IdeographicInlineSize (simple_font_data.cc:461)
// — all @ 5992657f556bcd9d207a4aca187488462f027cab. The fonts carry s's
// FontOrientation, as FontBuilder puts ComputeFontOrientation into the
// FontDescription.
func (fs *CSSFontSelector) CreateFont(s *css.Style) css.Font {
	fc := fs.fc
	specifiedSize := s.GetFontSize()
	family, _ := s.Get("font-family")
	bold := s.GetFontWeight() == css.FontWeightBold
	italic := s.GetFontStyle() == css.FontStyleItalic
	mono := s.IsMonospaceFamily()
	ahem := s.IsAhemFamily()
	synth := s.GetFontSynthesis()
	orientation := s.ComputeFontOrientation()
	// The primary font for a probe character: Blink's Font::PrimaryFont()
	// (U+0020), PrimaryFontWithDigitZero() (U+0030) and
	// PrimaryFontWithCjkWater() (U+6C34), platform/fonts/font.h:231/:237/:243
	// @ 5992657f556bcd9d207a4aca187488462f027cab.
	primaryFontFor := func(lookupChar rune) string {
		return fc.FontPathForCovering(family, bold, italic, mono, ahem, synth.Weight, synth.Style, lookupChar)
	}
	spacePath, zeroPath, icPath := primaryFontFor(kSpace), primaryFontFor(kDigitZero), primaryFontFor(kCjkWater)

	// Measure at the specified size so we can read the primary font's actual
	// aspect ratio for font-size-adjust resolution.
	baseData := fs.simpleFontData(spacePath, specifiedSize, orientation)

	// CSS Fonts 5 §1.7: derive the used font-size from font-size-adjust.
	// `none` and `from-font` leave the used size equal to the specified
	// size (from-font matches the first font's own aspect → factor 1).
	usedSize := specifiedSize
	adjust := s.GetFontSizeAdjust()
	if adjust.IsActive() && specifiedSize > 0 {
		actualRatio := firstFontAspectRatio(adjust.Metric, baseData)
		if actualRatio > 0 {
			usedSize = specifiedSize * adjust.Value / actualRatio
		} else {
			// A present metric that measures 0: louis14 clamps the used size
			// to 0, hiding the glyphs. Not ported: Blink's
			// FontSizeFunctions::MetricsMultiplierAdjustedFontSize returns
			// nullopt and leaves the size unadjusted
			// (core/css/font_size_functions.cc:292-295 @
			// 5992657f556bcd9d207a4aca187488462f027cab).
			usedSize = 0
		}
	}

	usedData := baseData
	if usedSize != specifiedSize {
		usedData = fs.simpleFontData(spacePath, usedSize, orientation)
	}
	icWidth, hasIC := fs.simpleFontData(icPath, usedSize, orientation).IdeographicInlineSize()

	return css.Font{
		AdjustedSize:             usedSize,
		ZeroWidth:                fs.simpleFontData(zeroPath, usedSize, orientation).ZeroInlineSize(),
		XHeight:                  usedData.xHeight,
		CapHeight:                usedData.capHeight,
		IdeographicInlineSize:    icWidth,
		HasIdeographicInlineSize: hasIC,
	}
}

// simpleFontData returns the font at fontPath instantiated at fontSize and
// orientation o. Glyph presence and measurements are cached per (font,
// rounded size) — safe because every measurement shapes the font opened at
// the rounded size (openFont), so sizes in one bucket measure identically;
// the 水 advance is filled lazily, on first IdeographicAdvanceWidth. The exact
// size and the orientation are stamped on each returned copy, so
// ZeroInlineSize's fallback scales with the exact size, as Blink's reads
// font_.getSize().
func (fs *CSSFontSelector) simpleFontData(fontPath string, fontSize float64, o css.FontOrientation) simpleFontData {
	k := fontKey{path: fontPath, fontSize: int32(math.Round(fontSize))}
	d, ok := fs.cache[k]
	if !ok {
		d = simpleFontData{
			xHeight:   FontXHeightFromFont(fontSize, fontPath),
			capHeight: FontCapHeightFromFont(fontSize, fontPath),
		}
		d.zeroGlyph, d.zeroAdvance = glyphAdvance(fontPath, fontSize, kDigitZero)
		d.ideographic = new(ideographicAdvance)
		fs.cache[k] = d
	}
	d.platformData = fontPlatformData{fontPath: fontPath, size: fontSize, orientation: o}
	return d
}

// firstFontAspectRatio returns the first available font's actual aspect ratio
// for the requested font-size-adjust metric (CSS Fonts 5 §1.7). Mirrors
// Blink's AspectValue (core/css/font_size_functions.cc:126-169 @
// 5992657f556bcd9d207a4aca187488462f027cab): d is the U+0020 primary font
// (FontBuilder::UpdateAdjustedSize uses font->PrimaryFont() for every
// metric, core/css/resolver/font_builder.cc:397 @ the same SHA), d's size is
// the one AspectValue divides by (PlatformData().size(), :128), and a metric
// the font lacks keeps the aspect at 1.0 (:136, csswg-drafts#6384): no 水
// glyph for ic-width / ic-height (IdeographicAdvanceWidth / Height empty,
// :148-160), cap-height <= 0 (:140-142), x-height <= 0 (HasXHeight, :163).
// ch-width reads ZeroInlineSize, so a font with no "0" glyph has aspect 0.5,
// as Blink's kChWidth reads ZeroWidth whenever HasZeroWidth (:143-145).
// Returns 0 only for a zero size or a present metric that measures 0 (a 水
// or "0" glyph with a zero advance). Not ported: Blink's HasXHeight is also
// false for a font with no OS/2 x-height (simple_font_data.cc:164-167 @ the
// same SHA); louis14's xHeight has no presence flag.
func firstFontAspectRatio(metric css.FontSizeAdjustMetric, d simpleFontData) float64 {
	fontSize := d.platformData.size
	if fontSize <= 0 {
		return 0
	}
	aspect := 1.0
	switch metric {
	case css.FontSizeAdjustCapHeight:
		if d.capHeight > 0 {
			aspect = d.capHeight / fontSize
		}
	case css.FontSizeAdjustChWidth:
		aspect = d.ZeroInlineSize() / fontSize
	case css.FontSizeAdjustIcWidth:
		if w, ok := d.IdeographicAdvanceWidth(); ok {
			aspect = w / fontSize
		}
	case css.FontSizeAdjustIcHeight:
		if h, ok := d.IdeographicAdvanceHeight(); ok {
			aspect = h / fontSize
		}
	default:
		// ex-height (default)
		if d.xHeight > 0 {
			aspect = d.xHeight / fontSize
		}
	}
	return aspect
}
