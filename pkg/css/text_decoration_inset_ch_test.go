package css

import (
	"testing"
)

// TestGetTextDecorationInsetChUnit asserts that the `ch` unit in
// text-decoration-inset resolves against the element's measured "0"-glyph
// advance (Style.ChWidth), not a fixed 0.5em heuristic.
//
// CSS Values §6.1: `ch` is the advance measure of "0" in the inline axis.
// For Ahem the "0" glyph advance is 1em, so `2ch` at font-size 10px must be
// 20px, and `-2ch` must be -20px. The previous implementation routed through
// ParseLengthOrPercentageFontRelative, which hardcodes a 0.5em ch multiplier,
// yielding 10px / -10px — a 1ch positional error that drifts the inset edge.
//
// Root cause for WPT text-decoration-inset-019 / -023: their inset edges are
// 1ch off because of this 0.5em approximation. The reference renders the
// underline from real glyph cells, landing at the true 2ch boundary.
//
// Mirrors Style.GetLength's metric threading (parseLengthFullWithCh with
// s.chScale()), which itself mirrors Blink's CSSToLengthConversionData ch
// resolution from FontMetrics zero-advance at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func TestGetTextDecorationInsetChUnit(t *testing.T) {
	s := NewStyle()
	s.Set("font-size", "10px")
	// Ahem: advance of "0" is 1em, so ChWidth == font-size.
	s.ChWidth = 10.0
	s.Set("text-decoration-inset", "2ch -2ch")

	got := s.GetTextDecorationInset()

	const wantStart, wantEnd = 20.0, -20.0
	if got.InlineStart != wantStart {
		t.Errorf("InlineStart = %v, want %v (2ch against 10px ch advance)", got.InlineStart, wantStart)
	}
	if got.InlineEnd != wantEnd {
		t.Errorf("InlineEnd = %v, want %v (-2ch against 10px ch advance)", got.InlineEnd, wantEnd)
	}
}

// TestGetTextDecorationInsetChUnitSingleValue covers the one-value shorthand
// path (both edges share the value) through the same measured-ch resolution.
func TestGetTextDecorationInsetChUnitSingleValue(t *testing.T) {
	s := NewStyle()
	s.Set("font-size", "10px")
	s.ChWidth = 10.0
	s.Set("text-decoration-inset", "3ch")

	got := s.GetTextDecorationInset()

	const want = 30.0
	if got.InlineStart != want || got.InlineEnd != want {
		t.Errorf("got {%v, %v}, want {%v, %v} (3ch against 10px ch advance)",
			got.InlineStart, got.InlineEnd, want, want)
	}
}
