package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/text"
)

// TestBaseFontAscentFromSubLine_EmHeight pins the base ascent ruby
// annotations sit on: Blink's ComputeEmHeight, the run fonts' normalized
// typo ascent rounded up and capped at the primary font's ascent
// (core/layout/inline/ruby_utils.cc:82-107 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). For an integer ascent that is
// the over edge an emphasis mark sits on (ascent less
// text.EmHeightOverAdjustment), so emphasis and ruby references agree. Liberation Serif 16px with LOU-920's
// hhea metrics (mazarin #125): ascent 14, normalized typo ascent 12.203125
// -> min(13, 14) = 13. Ahem 16px: min(ceil(12.796875), 13) = 13.
func TestBaseFontAscentFromSubLine_EmHeight(t *testing.T) {
	fc := text.DefaultFontConfig()
	for _, tc := range []struct {
		family string
		want   float64
	}{
		{"serif", 13},
		{"Ahem", 13},
	} {
		s := css.NewStyle()
		s.Set("font-family", tc.family)
		s.Set("font-size", "16px")
		s.UpdateFont(text.NewCSSFontSelector(fc))
		line := &LineInfo{Results: []InlineItemResult{{Item: &InlineItem{Type: InlineItemText, Style: s}}}}
		if got := baseFontAscentFromSubLine(line, NewWritingDirectionMode(s), fc, false); got != tc.want {
			t.Errorf("%s 16px: base em-height ascent = %v, want %v", tc.family, got, tc.want)
		}
	}
}

// TestBaseFontAscentFromSubLine_CentralEmHeight: on the central baseline
// ComputeEmHeight works from Height() / 2, which can be a half pixel, with no
// floor (ruby_utils.cc:90-107 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and
// :106 moves the em height to the integer text top, so the annotation sits
// ceil(H / 2) - max(0, H / 2 - ceil(normalized ascent)) above the baseline.
// Liberation Serif 16px with LOU-920's hhea metrics (mazarin #125): H =
// 14 + 3 = 17, normalized central ascent 16 - 8 = 8, so 9 - 0.5 = 8.5;
// Ahem 16px: H = 16, 8 - 0 = 8.
func TestBaseFontAscentFromSubLine_CentralEmHeight(t *testing.T) {
	fc := text.DefaultFontConfig()
	for _, tc := range []struct {
		family string
		want   float64
	}{
		{"serif", 8.5},
		{"Ahem", 8},
	} {
		s := css.NewStyle()
		s.Set("font-family", tc.family)
		s.Set("font-size", "16px")
		s.Set("writing-mode", "vertical-rl")
		s.UpdateFont(text.NewCSSFontSelector(fc))
		line := &LineInfo{Results: []InlineItemResult{{Item: &InlineItem{Type: InlineItemText, Style: s}}}}
		if got := baseFontAscentFromSubLine(line, NewWritingDirectionMode(s), fc, true); got != tc.want {
			t.Errorf("%s 16px central: base em-height ascent = %v, want %v", tc.family, got, tc.want)
		}
	}
}
