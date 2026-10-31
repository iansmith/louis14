package css

import "testing"

// TestComputeFontOrientation (LOU-425): FontOrientation follows
// ComputedStyleBuilder::ComputeFontOrientation (core/style/
// computed_style.cc:3256-3272 @ 5992657f556bcd9d207a4aca187488462f027cab):
// horizontal in a horizontal typographic mode (horizontal-tb, sideways-*;
// IsHorizontalTypographicMode, platform/text/writing_mode.h:97 @ the same SHA)
// whatever text-orientation says, else mixed / upright / sideways map to
// kVerticalMixed / kVerticalUpright / kVerticalRotated.
func TestComputeFontOrientation(t *testing.T) {
	for _, tc := range []struct {
		writingMode, textOrientation string
		want                         FontOrientation
	}{
		{"", "", FontOrientationHorizontal},
		{"horizontal-tb", "upright", FontOrientationHorizontal},
		{"sideways-rl", "upright", FontOrientationHorizontal},
		{"sideways-lr", "mixed", FontOrientationHorizontal},
		{"vertical-rl", "", FontOrientationVerticalMixed},
		{"vertical-rl", "mixed", FontOrientationVerticalMixed},
		{"vertical-lr", "upright", FontOrientationVerticalUpright},
		{"vertical-rl", "sideways", FontOrientationVerticalRotated},
	} {
		s := NewStyle()
		if tc.writingMode != "" {
			s.Set("writing-mode", tc.writingMode)
		}
		if tc.textOrientation != "" {
			s.Set("text-orientation", tc.textOrientation)
		}
		if got := s.ComputeFontOrientation(); got != tc.want {
			t.Errorf("writing-mode %q, text-orientation %q: ComputeFontOrientation = %v, want %v",
				tc.writingMode, tc.textOrientation, got, tc.want)
		}
	}
}
