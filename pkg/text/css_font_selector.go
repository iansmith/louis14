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
// :64 — louis14's FontConfig.Registry). Measured metrics are cached per
// (font, rounded size), the analog of SimpleFontData computing its metrics
// once per font. One selector serves one cascade; it is not safe for
// concurrent use.
type CSSFontSelector struct {
	fc      FontConfig
	cache   map[fontKey]fontMetrics
	icCache map[icKey]float64
}

// icKey keys the 水-advance cache by the covering font and the exact used
// size (the ic advance was never size-rounded).
type icKey struct {
	path     string
	fontSize float64
}

// fontKey keys the metric cache. Sub-pixel size jitter is rare; rounding the
// size stabilises the cache for documents rendering many elements at one size.
type fontKey struct {
	path     string
	fontSize int32
}

// fontMetrics holds the per-font measurements that drive CSS font-relative
// unit resolution (ch/ex/cap) for a given (font, size). The ic advance is
// resolved separately (see CreateFont) so that U+6C34 (水) is sourced from a
// declared family whose @font-face unicode-range covers it.
type fontMetrics struct {
	chWidth float64
	xHeight float64
	capHght float64
}

// NewCSSFontSelector returns a selector over fc with an empty metric cache.
func NewCSSFontSelector(fc FontConfig) *CSSFontSelector {
	return &CSSFontSelector{fc: fc, cache: make(map[fontKey]fontMetrics), icCache: make(map[icKey]float64)}
}

// CreateFont selects s's primary font from its font properties and returns
// that font's metrics at the CSS Fonts 5 §1.7 font-size-adjust used size.
// Mirrors Blink's FontBuilder::CreateFont (core/css/resolver/font_builder.cc
// :656): UpdateAdjustedSize (:378) derives the used size from the first
// available font's aspect value, then the Font's primary SimpleFontData
// supplies the metrics — the "0" advance (SetZeroWidth(ZeroInlineSize()),
// platform/fonts/simple_font_data.cc:243-245), x-height and cap-height
// (FontMetrics, platform/fonts/font_metrics.h:98/86), all @
// 5992657f556bcd9d207a4aca187488462f027cab, and the optional 水 advance
// (SimpleFontData::IdeographicInlineSize, simple_font_data.cc:459-473 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func (fs *CSSFontSelector) CreateFont(s *css.Style) css.Font {
	fc := fs.fc
	specifiedSize := s.GetFontSize()
	family, _ := s.Get("font-family")
	bold := s.GetFontWeight() == css.FontWeightBold
	italic := s.GetFontStyle() == css.FontStyleItalic
	mono := s.IsMonospaceFamily()
	ahem := s.IsAhemFamily()
	synth := s.GetFontSynthesis()
	fontPath := fc.FontPathForFamilyWithSynthesis(family, bold, italic, mono, ahem, synth.Weight, synth.Style)

	// Measure metrics at the specified size so we can read the first font's
	// actual aspect ratio for font-size-adjust resolution.
	baseMetrics := fs.metrics(fontPath, specifiedSize)

	// CSS Fonts 5 §1.7: derive the used font-size from font-size-adjust.
	// `none` and `from-font` leave the used size equal to the specified
	// size (from-font matches the first font's own aspect → factor 1).
	usedSize := specifiedSize
	adjust := s.GetFontSizeAdjust()
	if adjust.IsActive() && specifiedSize > 0 {
		actualRatio := firstFontAspectRatio(adjust.Metric, baseMetrics, specifiedSize, fontPath)
		if actualRatio > 0 {
			usedSize = specifiedSize * adjust.Value / actualRatio
		} else {
			// Spec: when the first font lacks the requested metric, the used
			// size is undefined; clamp to 0 to match Blink's behavior of
			// hiding the glyphs (matches font-size-adjust-zero-* refs).
			usedSize = 0
		}
	}

	usedMetrics := baseMetrics
	if usedSize != specifiedSize {
		usedMetrics = fs.metrics(fontPath, usedSize)
	}

	// ic unit: CSS Values 4 §6.1 defines ic as the advance of U+6C34 (水) in
	// the element's font. Only measure from a declared family whose
	// @font-face unicode-range covers 水 (via the registry), as Blink picks
	// Font::PrimaryFontWithCjkWater (platform/fonts/font.h:154/:250 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520). The advance is present only
	// then — even when it is 0 — as SimpleFontData::IdeographicAdvanceWidth
	// sets its optional only for a font with the 水 glyph
	// (simple_font_data.cc:435-445 @ d4ecdfed88f962439247c2ad36b8fe47805b1520);
	// louis14 checks the unicode-range, not the cmap. With no such family it
	// is absent and the ic unit falls back to 1em. We do NOT fall back to
	// builtin serif/sans-serif here — doing so would pick up any system CJK
	// font and produce wrong ic values for Latin-only declared fonts.
	ic, hasIC := 0.0, false
	if fc.Registry != nil {
		for _, fam := range ParseFontFamilyList(family) {
			if icPath := fc.Registry.LookupForCodepoint(fam, '水', bold, italic, synth.Style); icPath != "" {
				ic, hasIC = fs.waterAdvance(icPath, usedSize), true
				break
			}
		}
	}

	return css.Font{
		AdjustedSize:             usedSize,
		ZeroWidth:                usedMetrics.chWidth,
		XHeight:                  usedMetrics.xHeight,
		CapHeight:                usedMetrics.capHght,
		IdeographicInlineSize:    ic,
		HasIdeographicInlineSize: hasIC,
	}
}

// metrics measures (or returns the cached) ch/ex/cap metrics of the font at
// fontPath and fontSize.
func (fs *CSSFontSelector) metrics(fontPath string, fontSize float64) fontMetrics {
	k := fontKey{path: fontPath, fontSize: int32(math.Round(fontSize))}
	if m, ok := fs.cache[k]; ok {
		return m
	}
	chLU, _ := MeasureText("0", fontSize, fontPath)
	m := fontMetrics{
		chWidth: chLU.Float64(),
		xHeight: FontXHeightFromFont(fontSize, fontPath),
		capHght: FontCapHeightFromFont(fontSize, fontPath),
	}
	fs.cache[k] = m
	return m
}

// waterAdvance measures (or returns the cached) advance of 水 in the font at
// fontPath and fontSize.
func (fs *CSSFontSelector) waterAdvance(fontPath string, fontSize float64) float64 {
	k := icKey{path: fontPath, fontSize: fontSize}
	if w, ok := fs.icCache[k]; ok {
		return w
	}
	icLU, _ := MeasureText("水", fontSize, fontPath)
	fs.icCache[k] = icLU.Float64()
	return fs.icCache[k]
}

// firstFontAspectRatio returns the first available font's actual aspect ratio
// for the requested font-size-adjust metric (CSS Fonts 5 §1.7). Returns 0 when
// the font has no usable value for the metric — callers treat that as a
// zero-sized used font-size.
func firstFontAspectRatio(metric css.FontSizeAdjustMetric, m fontMetrics, fontSize float64, fontPath string) float64 {
	if fontSize <= 0 {
		return 0
	}
	switch metric {
	case css.FontSizeAdjustCapHeight:
		return m.capHght / fontSize
	case css.FontSizeAdjustChWidth:
		return m.chWidth / fontSize
	case css.FontSizeAdjustIcWidth, css.FontSizeAdjustIcHeight:
		// CSS Fonts 5 §1.7: ic-width / ic-height use the advance width or
		// height of the U+6C34 (水) glyph. Mirror Blink's behavior: the metric
		// is sourced from the same fontPath; when 水 isn't present (e.g. Ahem),
		// the metric is undefined and the spec calls for the user agent to
		// behave as if `font-size-adjust: none` — Blink resolves the ratio to
		// 0 which clamps the used size to 0. We follow Blink.
		w, h := MeasureText("水", fontSize, fontPath)
		wf, hf := w.Float64(), h.Float64()
		if wf == 0 || hf == 0 {
			return 0
		}
		if metric == css.FontSizeAdjustIcWidth {
			return wf / fontSize
		}
		return hf / fontSize
	default:
		// ex-height (default)
		return m.xHeight / fontSize
	}
}
