package text

import (
	"testing"

	"louis14/pkg/css"
)

// TestCSSToLengthConversionData_Normal (LOU-425) ports
// CSSToLengthConversionDataTest.Normal (core/css/
// css_to_length_conversion_data_test.cc:163-178 @
// 5992657f556bcd9d207a4aca187488462f027cab): an Ahem font at 20px resolves
// 1em = 20, 1ex = 16, 1ch = 20, 1ic = 20, 1cap = 16 and calc(1em + 1ex) = 36,
// through CSSFontSelector.CreateFont and the style's conversion data.
// Omitted rows: rem and the root-font r* units, which need a root style, and
// lh, which needs the line-height setup. Blink's Ahem.woff has no 水, so its
// 1ic is the 1em fallback; louis14's Ahem.ttf maps 水 at 1em, the same 20.
func TestCSSToLengthConversionData_Normal(t *testing.T) {
	fr := NewFontRegistry()
	registerTestFace(t, fr, "LOU425-Normal-Ahem", "Ahem.ttf", fetchTestFont)
	s := css.NewStyle()
	s.Set("font-size", "20px")
	s.Set("font-family", "LOU425-Normal-Ahem")
	s.UpdateFont(NewCSSFontSelector(configWithRegistry(fr)))

	for _, tc := range []struct {
		val  string
		want float64
	}{
		{"1px", 1},
		{"1em", 20},
		{"1ex", 16},
		{"1ch", 20},
		{"1ic", 20},
		{"calc(1em + 1ex)", 36},
		{"1cap", 16},
	} {
		if got, ok := s.GetLengthForVal(tc.val); !ok || got != tc.want {
			t.Errorf("%s = (%v, %v), want %v", tc.val, got, ok, tc.want)
		}
	}
}
