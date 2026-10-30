package css

import "testing"

// TestIcUnit_MeasuredZeroAdvance (LOU-431): a font whose 水 (U+6C34) glyph has
// a zero advance gives ic = 0, not the 1em fallback; only an unmeasured ic
// falls back. Blink: CSSToLengthConversionData::FontSizes::Ic returns Em only
// when the std::optional IdeographicInlineSize has no value
// (core/css/css_to_length_conversion_data.cc:145-158 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). WPT: css-values/ic-unit-013.
func TestIcUnit_MeasuredZeroAdvance(t *testing.T) {
	newStyle := func() *Style {
		s := NewStyle()
		s.Set("font-size", "20px")
		s.Set("width", "calc(100px + 10ic)")
		return s
	}
	check := func(name string, s *Style, wantWidth, wantIc, wantPct float64) {
		t.Helper()
		if got, _ := s.GetLength("width"); got != wantWidth {
			t.Errorf("%s: calc(100px + 10ic) = %v, want %v", name, got, wantWidth)
		}
		if got, _ := s.GetLengthForVal("10ic"); got != wantIc {
			t.Errorf("%s: 10ic = %v, want %v", name, got, wantIc)
		}
		if got, _ := s.EvalMathWithBase("calc(50% + 10ic)", 200); got != wantPct {
			t.Errorf("%s: calc(50%% + 10ic) of 200 = %v, want %v", name, got, wantPct)
		}
	}

	measured := newStyle()
	measured.IcWidth, measured.HasIdeographicInlineSize = 0, true
	check("measured zero advance", measured, 100, 0, 100)

	check("unmeasured (1em fallback)", newStyle(), 300, 200, 300)

	half := newStyle()
	half.IcWidth, half.HasIdeographicInlineSize = 10, true
	check("measured 10px advance", half, 200, 100, 200)

	// Callers with no Style have no font: ic falls back to 1em.
	if got, _ := ParseLengthFull("10ic", 20, 0, 0); got != 200 {
		t.Errorf("ParseLengthFull(10ic, 20px) = %v, want 200 (1em fallback)", got)
	}
	if got, _ := EvalMathFunctionWithPercent("calc(10ic)", 20, 0); got != 200 {
		t.Errorf("EvalMathFunctionWithPercent(calc(10ic), 20px) = %v, want 200 (1em fallback)", got)
	}
}
