package css

import "testing"

// TestMulticolLengths_FontRelativeUnits (LOU-369): column-width, column-gap,
// row-gap and column-height resolve font-relative and viewport units against
// the element's own font metrics and viewport, like Style.GetLength. Blink
// converts them at style-building time (ConvertComputedLength<float>,
// ConvertGapLength; css_properties.json5:7037, :7368 @ a9f50e522efa).
func TestMulticolLengths_FontRelativeUnits(t *testing.T) {
	newStyle := func(prop, val string) *Style {
		s := NewStyle()
		s.Set("font-size", "20px")
		s.ChWidth = 12
		s.ViewportWidth = 800
		s.Set(prop, val)
		return s
	}
	for _, tc := range []struct {
		prop, val string
		get       func(*Style) float64
		want      float64
	}{
		{"column-width", "2em", (*Style).GetColumnWidth, 40},
		{"column-width", "3ch", (*Style).GetColumnWidth, 36},
		{"column-width", "10vw", (*Style).GetColumnWidth, 80},
		{"column-gap", "3ch", (*Style).GetColumnGapMulticol, 36},
		{"row-gap", "3ch", (*Style).GetRowGapMulticol, 36},
		{"column-height", "3ch", (*Style).GetColumnHeight, 36},
	} {
		if got := tc.get(newStyle(tc.prop, tc.val)); got != tc.want {
			t.Errorf("%s: %s = %v, want %v (font-size 20px, ch advance 12px, viewport 800px)", tc.prop, tc.val, got, tc.want)
		}
	}
}
