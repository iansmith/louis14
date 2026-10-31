package css

// FontOrientation is the glyph orientation a font is set in, derived from
// writing-mode and text-orientation. Mirrors Blink's FontOrientation
// (platform/fonts/font_orientation.h:35-47 @
// 5992657f556bcd9d207a4aca187488462f027cab), which FontBuilder stores in the
// FontDescription and FontPlatformData::Orientation carries to SimpleFontData.
type FontOrientation uint8

const (
	// FontOrientationHorizontal: a horizontal typographic mode
	// (horizontal-tb, sideways-rl, sideways-lr).
	FontOrientationHorizontal FontOrientation = 0
	// FontOrientationVerticalRotated: vertical-* with text-orientation:
	// sideways — horizontal typography rotated.
	FontOrientationVerticalRotated FontOrientation = 1
	// FontOrientationVerticalMixed: vertical-* with text-orientation: mixed
	// — CJK upright, non-CJK rotated.
	FontOrientationVerticalMixed FontOrientation = 2
	// FontOrientationVerticalUpright: vertical-* with text-orientation:
	// upright — every glyph upright.
	FontOrientationVerticalUpright FontOrientation = 3
)

// IsVerticalNonCjkUpright reports that non-CJK glyphs are set upright too.
// Mirrors Blink's IsVerticalNonCjkUpright (platform/fonts/
// font_orientation.h:54 @ 5992657f556bcd9d207a4aca187488462f027cab).
func IsVerticalNonCjkUpright(o FontOrientation) bool {
	return o == FontOrientationVerticalUpright
}

// ComputeFontOrientation derives s's FontOrientation from its writing-mode
// and text-orientation. Mirrors ComputedStyleBuilder::ComputeFontOrientation
// (core/style/computed_style.cc:3256-3272 @
// 5992657f556bcd9d207a4aca187488462f027cab): text-orientation has no effect
// in a horizontal typographic mode — horizontal-tb, sideways-rl, sideways-lr
// (IsHorizontalTypographicMode, platform/text/writing_mode.h:97 @ the same
// SHA). sideways-right is text-orientation's legacy alias of sideways.
func (s *Style) ComputeFontOrientation() FontOrientation {
	switch wm, _ := s.Get("writing-mode"); wm {
	case "vertical-rl", "vertical-lr":
	default:
		return FontOrientationHorizontal
	}
	switch to, _ := s.Get("text-orientation"); to {
	case "upright":
		return FontOrientationVerticalUpright
	case "sideways", "sideways-right":
		return FontOrientationVerticalRotated
	default:
		return FontOrientationVerticalMixed
	}
}

// ZeroInlineSizeFallback is the ch advance assumed for a font with no "0"
// glyph: 0.5em, or 1em when typeset upright. It is the no-glyph tail of
// Blink's SimpleFontData::ZeroInlineSize (platform/fonts/
// simple_font_data.cc:579-598 @ 5992657f556bcd9d207a4aca187488462f027cab)
// with CSSChUnitSpecCompliantFallback enabled — status "stable"
// (platform/runtime_enabled_features.json5:1732-1733 @ the same SHA) — so the
// legacy size/0.5 path (:596) is not ported. No standalone Blink analog: it is
// a function here because pkg/text's ZeroInlineSize and pkg/css's
// selectorless chScale both need it, and pkg/text imports pkg/css.
func ZeroInlineSizeFallback(size float64, o FontOrientation) float64 {
	if IsVerticalNonCjkUpright(o) {
		return size
	}
	return size * 0.5
}
