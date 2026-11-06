package html

import "testing"

// ParseDimensionValue follows Blink's rules for parsing dimension values
// (html/html_dimension.cc:146-192 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// leading HTML spaces, digits, an optional fraction, then '%' for a
// percentage or '*' for a relative value; anything else trails as garbage.
func TestParseDimensionValue(t *testing.T) {
	cases := []struct {
		in    string
		ok    bool
		value float64
		typ   HTMLDimensionType
	}{
		{"60", true, 60, HTMLDimensionAbsolute},
		{"0", true, 0, HTMLDimensionAbsolute},
		{" 60", true, 60, HTMLDimensionAbsolute},
		{"60px", true, 60, HTMLDimensionAbsolute},
		{"60abc", true, 60, HTMLDimensionAbsolute},
		{"  60px", true, 60, HTMLDimensionAbsolute},
		{"12.5", true, 12.5, HTMLDimensionAbsolute},
		{"12.", true, 12, HTMLDimensionAbsolute},
		{"50%", true, 50, HTMLDimensionPercentage},
		{"3*", true, 3, HTMLDimensionRelative},
		{"", false, 0, 0},
		{"abc", false, 0, 0},
		{"-5", false, 0, 0},
		{"+5", false, 0, 0},
		{".5", false, 0, 0},
	}
	for _, tc := range cases {
		d, ok := ParseDimensionValue(tc.in)
		if ok != tc.ok {
			t.Errorf("ParseDimensionValue(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && (d.Value != tc.value || d.Type != tc.typ) {
			t.Errorf("ParseDimensionValue(%q) = %+v, want {%v %v}", tc.in, d, tc.value, tc.typ)
		}
	}
}
