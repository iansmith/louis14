package css

import "testing"

// TestGetTextDecorationThicknessResolved_PercentAgainstComputedFontSize
// captures C55's percent-base fix.
//
// Per Blink's `core/paint/text_decoration_info.cc::ComputeDecorationThickness`
// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f:
//
//	float used_font_size = used_font.UsedSize();
//
// where `UsedSize() = ComputedSize() * text_fit_scaling_factor_` and
// `ComputedSize()` is the specified size post-zoom/min-font but PRE-
// `font-size-adjust`. So `<percentage>` text-decoration-thickness resolves
// against the computed (pre-adjust) font-size, not the post-adjust used size.
//
// Regression case: text-decoration-thickness-percent-001.html (CSS Fonts 5
// §1.7.3 + CSS Text Decor 4 §3.4). Test uses `font-size-adjust: 0.25 / 1.25`
// to make the used vs computed font-size distinction load-bearing.
func TestGetTextDecorationThicknessResolved_PercentAgainstComputedFontSize(t *testing.T) {
	s := NewStyle()
	s.Properties["font-size"] = "20px"
	s.Properties["text-decoration-thickness"] = "100%"
	// Simulate a layout pass that has already applied `font-size-adjust`,
	// producing a USED font-size that differs from the computed font-size.
	s.UsedFontSize = 10.0 // post-adjust used size (would be the resolution base if we'd been buggy).
	s.UsedFontSizeSet = true

	got := s.GetTextDecorationThicknessResolved()
	if got.Kind != TextDecorationThicknessLength {
		t.Fatalf("Kind = %v; want Length", got.Kind)
	}
	// 100% of computed font-size (20px) = 20px, NOT 100% of used size (10px).
	if got.Value != 20.0 {
		t.Errorf("100%% thickness resolved to %v; want 20 (= 100%% of computed 20px font-size)", got.Value)
	}
}

// TestGetTextDecorationThicknessResolved_LengthUntouched verifies the length
// path is unchanged by the percent-base fix.
func TestGetTextDecorationThicknessResolved_LengthUntouched(t *testing.T) {
	s := NewStyle()
	s.Properties["font-size"] = "20px"
	s.Properties["text-decoration-thickness"] = "2.7px"

	got := s.GetTextDecorationThicknessResolved()
	if got.Kind != TextDecorationThicknessLength {
		t.Fatalf("Kind = %v; want Length", got.Kind)
	}
	if got.Value != 2.7 {
		t.Errorf("2.7px thickness resolved to %v; want 2.7", got.Value)
	}
}

// TestGetTextDecorationThicknessResolved_AutoFromFont verifies the keyword
// path is unchanged.
func TestGetTextDecorationThicknessResolved_AutoFromFont(t *testing.T) {
	cases := []struct {
		val  string
		kind TextDecorationThicknessKind
	}{
		{"auto", TextDecorationThicknessAuto},
		{"from-font", TextDecorationThicknessFromFont},
	}
	for _, c := range cases {
		s := NewStyle()
		s.Properties["font-size"] = "20px"
		s.Properties["text-decoration-thickness"] = c.val
		got := s.GetTextDecorationThicknessResolved()
		if got.Kind != c.kind {
			t.Errorf("%q → Kind=%v; want %v", c.val, got.Kind, c.kind)
		}
	}
}
