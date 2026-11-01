package render

import "testing"

// TestThumbPositionFollowsScrollPosition pins ScrollbarTheme::ThumbPosition
// (scroll/scrollbar_theme.cc:195-208 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) on louis14's half-track thumb:
// the position relative to the minimum offset, scaled from the scroll range
// onto the free track; zero when nothing scrolls.
func TestThumbPositionFollowsScrollPosition(t *testing.T) {
	cases := []struct {
		name                     string
		offset, minimum, maximum float64
		want                     float64
	}{
		{"no range", 0, 0, 0, 0},
		{"start", 0, 0, 300, 0},
		{"end", 300, 0, 300, 50},
		{"middle", 150, 0, 300, 25},
		// vertical-rl: range [-300, 0], initial offset 0 is the end.
		{"leftward initial", 0, -300, 0, 50},
		{"leftward start", -300, -300, 0, 0},
	}
	for _, tc := range cases {
		if got := thumbPosition(100, tc.offset, tc.minimum, tc.maximum); got != tc.want {
			t.Errorf("%s: thumbPosition = %v, want %v", tc.name, got, tc.want)
		}
	}
}
