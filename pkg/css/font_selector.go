package css

// FontSelector is the platform font lookup the cascade consults to build an
// element's font: given a style whose font properties (family, size, weight,
// style, synthesis, size-adjust) are final, it selects the primary font and
// returns its metrics. pkg/css cannot reach the font machinery directly
// (pkg/text imports pkg/css), so the layout engine injects an implementation
// (text.CSSFontSelector) — as Blink hands FontBuilder the document's
// FontSelector rather than letting core/css reach into platform/fonts.
// Mirrors Blink's FontSelector (platform/fonts/font_selector.h:51) as used by
// FontBuilder::CreateFont (core/css/resolver/font_builder.cc:656), which runs
// UpdateAdjustedSize against the selector (:680) and then builds
// Font(description, font_selector) (:682), all @
// 5992657f556bcd9d207a4aca187488462f027cab.
type FontSelector interface {
	CreateFont(s *Style) Font
}

// Font is the slice of Blink's Font that the cascade reads: the primary
// font's metrics at the font-size-adjust used size. Mirrors the values
// CSSToLengthConversionData::FontSizes reads from Font for ex / ch / ic / cap
// (core/css/css_to_length_conversion_data.cc:95/117/139/169): FontMetrics
// XHeight / CapHeight / SetZeroWidth (platform/fonts/font_metrics.h:98/86/173)
// and SimpleFontData::IdeographicInlineSize
// (platform/fonts/simple_font_data.cc:461), all @
// 5992657f556bcd9d207a4aca187488462f027cab.
type Font struct {
	// AdjustedSize is the font-size-adjust used size (Blink
	// FontDescription::AdjustedSize, set by FontBuilder::UpdateAdjustedSize,
	// font_builder.cc:378 @ 5992657f556b). Stored as Style.UsedFontSize.
	AdjustedSize float64
	// ZeroWidth is the advance of "0" (Blink FontMetrics::ZeroWidth). Always
	// a measurement — Style.SetZeroWidth marks it measured.
	ZeroWidth float64
	// XHeight is the x-height in px; 0 means the font has none, and ex falls
	// back to 0.5em (Blink FontMetrics::HasXHeight requires x_height_ > 0,
	// font_metrics.h:107 @ 5992657f556b).
	XHeight float64
	// CapHeight is the cap-height in px; 0 = none (cap falls back to 0.7em).
	CapHeight float64
	// IdeographicInlineSize is the advance of 水 in px. 0 means none, where
	// Blink uses std::optional (simple_font_data.cc:461 @ 5992657f556b), so a
	// measured zero advance still falls back to 1em (LOU-422, not fixed here).
	IdeographicInlineSize float64
}
