package css

import "testing"

// contain-intrinsic-size and its longhands take [auto]? [none |
// <length [0,∞]>] per axis; an invalid declaration is dropped whole, so an
// earlier valid one stands (ContainIntrinsicSize::ParseShorthand /
// ConsumeIntrinsicSizeLonghand @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b)
// (#359 review).
func TestContainIntrinsicSizeInvalidDropped(t *testing.T) {
	for _, tc := range []struct {
		prop, value string
	}{
		{"contain-intrinsic-size", "auto"},
		{"contain-intrinsic-size", "10px foo"},
		{"contain-intrinsic-size", "10%"},
		{"contain-intrinsic-size", "-5px"},
		{"contain-intrinsic-size", "10px -5px"},
		{"contain-intrinsic-size", "1px 2px 3px"},
		{"contain-intrinsic-size", "auto auto"},
		{"contain-intrinsic-size", "10"},
		{"contain-intrinsic-width", "-5px"},
		{"contain-intrinsic-width", "30%"},
		{"contain-intrinsic-width", "auto"},
		{"contain-intrinsic-height", "foo"},
	} {
		s := NewStyle()
		expandShorthand(s, "contain-intrinsic-size", "50px 100px")
		expandShorthand(s, tc.prop, tc.value)
		w, wok := s.ContainIntrinsicWidth()
		h, hok := s.ContainIntrinsicHeight()
		if !wok || !hok || w != 50 || h != 100 {
			t.Errorf("%s: %s: got %v,%v (%v,%v), want the earlier 50px 100px to stand", tc.prop, tc.value, w, h, wok, hok)
		}
	}
}

func TestContainIntrinsicSizeValid(t *testing.T) {
	s := NewStyle()
	expandShorthand(s, "contain-intrinsic-size", "auto 10px none")
	if w, ok := s.ContainIntrinsicWidth(); !ok || w != 10 {
		t.Errorf("width = %v,%v, want 10", w, ok)
	}
	if _, ok := s.ContainIntrinsicHeight(); ok {
		t.Errorf("height should be none")
	}
	expandShorthand(s, "contain-intrinsic-height", "0")
	if h, ok := s.ContainIntrinsicHeight(); !ok || h != 0 {
		t.Errorf("height = %v,%v, want 0", h, ok)
	}
}
