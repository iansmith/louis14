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
// XHeight / CapHeight / SetZeroWidth (platform/fonts/font_metrics.h:98/86/173,
// @ 5992657f556bcd9d207a4aca187488462f027cab) and the optional
// SimpleFontData::IdeographicInlineSize (platform/fonts/simple_font_data.h:110
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
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
	// IdeographicInlineSize is the advance of 水 in px; valid only when
	// HasIdeographicInlineSize, so a measured 0 is a real zero advance.
	IdeographicInlineSize float64
	// HasIdeographicInlineSize reports that a font covering 水 supplied
	// IdeographicInlineSize: has_value() of Blink's std::optional<float>
	// SimpleFontData::IdeographicInlineSize (platform/fonts/simple_font_data.h
	// :110, set at simple_font_data.cc:459-473 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520). Without it ic falls back to
	// 1em.
	HasIdeographicInlineSize bool
}

// UpdateFont builds s's font from its now-final font properties and stores
// the metrics the font-relative units read (Style.UsedFontSize, ChWidth,
// XHeight, CapHeight, IcWidth / HasIdeographicInlineSize). A non-nil fs
// becomes s's selector; with nil, s keeps the selector it carries (set by the
// cascade, inherited by pseudo and anonymous styles and clones); with none, s
// keeps the unmeasured fallbacks. Mirrors StyleResolverState::UpdateFont
// (core/css/resolver/style_resolver_state.cc:553 @
// 5992657f556bcd9d207a4aca187488462f027cab), which runs FontBuilder::CreateFont
// and then points CSSToLengthConversionData::FontSizes at the new font.
func (s *Style) UpdateFont(fs FontSelector) {
	if fs != nil {
		s.fontSelector = fs
	}
	if s.fontSelector == nil {
		return
	}
	f := s.fontSelector.CreateFont(s)
	s.UsedFontSize = f.AdjustedSize
	s.UsedFontSizeSet = true
	s.SetZeroWidth(f.ZeroWidth)
	s.XHeight = f.XHeight
	s.CapHeight = f.CapHeight
	s.IcWidth, s.HasIdeographicInlineSize = f.IdeographicInlineSize, f.HasIdeographicInlineSize
}

// UpdateLineHeight records s's computed line-height as the lh unit's size.
// Mirrors StyleResolverState::UpdateLineHeight
// (core/css/resolver/style_resolver_state.cc:565 @
// 5992657f556bcd9d207a4aca187488462f027cab), which sets the conversion data's
// LineHeightSize once line-height is applied.
func (s *Style) UpdateLineHeight() {
	s.LhSize = s.GetLineHeight()
}

// applyFontAndLineHeight is the font / line-height step of the cascade: build
// the element's font, compute line-height against it (lh against parent's),
// then record the lh size. Mirrors StyleCascade::Apply's UpdateFont →
// line-height → UpdateLineHeight sequence
// (core/css/resolver/style_cascade.cc:310-315 @
// 5992657f556bcd9d207a4aca187488462f027cab). applyStylesToNode runs it once, after
// @container rules. It re-builds the font idempotently, so first-frame
// transitions re-run it; a line-height already absolutized to px is not
// re-derived from the new font.
func applyFontAndLineHeight(s, parent *Style) {
	s.UpdateFont(nil)
	absolutizeLineHeight(s, parent)
	s.UpdateLineHeight()
}
