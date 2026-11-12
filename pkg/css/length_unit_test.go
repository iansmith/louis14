package css

import (
	"math"
	"testing"
)

// TestAbsoluteLengthUnits checks the absolute length units, Q among them,
// and that a unit is matched case-insensitively, as Blink's
// CSSPrimitiveValue::StringToUnitType does for the units of
// css_primitive_value_units.json5 (core/css @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), including inside calc()
// (LOU-861).
func TestAbsoluteLengthUnits(t *testing.T) {
	const tenMM = 96.0 / 25.4 * 10 // 40Q
	tests := []struct {
		val  string
		want float64
	}{
		{"40Q", tenMM},
		{"40q", tenMM},
		{"105.83333Q", 100},
		{"10PX", 10},
		{"1In", 96},
		{"10MM", tenMM},
		{"1CM", 96.0 / 2.54},
		{"12PT", 16},
		{"1PC", 16},
		{"calc(40Q + 1PX)", tenMM + 1},
	}
	for _, tt := range tests {
		got, ok := ParseLength(tt.val)
		if !ok || math.Abs(got-tt.want) > 0.001 {
			t.Errorf("ParseLength(%q) = %v, %v; want %v", tt.val, got, ok, tt.want)
		}
	}
}

// TestValidateCSSLengthTakesQ checks the parse-time <length> check behind
// typed attr() accepts Q in either case (LOU-861).
func TestValidateCSSLengthTakesQ(t *testing.T) {
	for _, v := range []string{"40Q", "40q", "10PX"} {
		if _, ok := validateCSSLength(v); !ok {
			t.Errorf("validateCSSLength(%q) rejected it", v)
		}
	}
}
