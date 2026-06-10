package css

import "testing"

// LOU-228: percentage-in-calc() margins/padding must resolve the percent
// component against a zero containing-block size (contributing the length
// component) instead of failing the calc parse and dropping the whole value.
// Mirrors Blink's ComputePhysicalMargins (core/layout/length_utils.cc:1443 @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f) → MinimumValueForLength
// (platform/geometry/length_functions.cc:64): kCalculated never fails;
// percent resolves against the passed base, including 0.

func TestCalcPercentMarginAtZeroBase(t *testing.T) {
	s := NewStyle()
	s.Set("margin-left", "calc(10% + 100px)")
	if got := s.GetAllMarginsForWidth(0).Left; got != 100 {
		t.Errorf("margin-left calc(10%% + 100px) at base 0 = %v, want 100", got)
	}
}

func TestCalcPercentMarginNegativeAtZeroBase(t *testing.T) {
	// Margins may be negative (CSS 2.1 §8.3) — no clamp.
	s := NewStyle()
	s.Set("margin-left", "calc(10% - 50px)")
	if got := s.GetAllMarginsForWidth(0).Left; got != -50 {
		t.Errorf("margin-left calc(10%% - 50px) at base 0 = %v, want -50", got)
	}
}

func TestCalcPercentPaddingAtZeroBase(t *testing.T) {
	s := NewStyle()
	s.Set("padding-left", "calc(10% + 100px)")
	if got := s.GetPaddingForWidth(0).Left; got != 100 {
		t.Errorf("padding-left calc(10%% + 100px) at base 0 = %v, want 100", got)
	}
}

func TestCalcPercentPaddingClampAtZeroBase(t *testing.T) {
	// Padding cannot be negative (CSS 2.1 §8.4) — clamps to 0.
	s := NewStyle()
	s.Set("padding-left", "calc(10% - 50px)")
	if got := s.GetPaddingForWidth(0).Left; got != 0 {
		t.Errorf("padding-left calc(10%% - 50px) at base 0 = %v, want 0 (clamped)", got)
	}
}

func TestCalcPercentMarginPositiveBaseUnchanged(t *testing.T) {
	// Guard: positive containing widths keep resolving percent normally.
	s := NewStyle()
	s.Set("margin-left", "calc(10% + 100px)")
	if got := s.GetAllMarginsForWidth(200).Left; got != 120 {
		t.Errorf("margin-left calc(10%% + 100px) at base 200 = %v, want 120", got)
	}
}
