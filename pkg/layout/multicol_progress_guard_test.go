package layout

import (
	"math"
	"testing"
)

// TestColumnBreakTokenAdvanced locks the load-bearing comparison of the inner
// column loop's non-advancing-token guard (multicol_layout.go). The guard
// breaks the row when a column's outgoing break token fails to advance; this
// test pins BOTH halves of that decision so an inversion or a broken
// comparison is caught directly:
//
//   - zero-progress (consumed == prev, or consumed < prev): NOT advanced → the
//     loop MUST break, otherwise it spins forever placing 0-progress columns
//     (the LOU-368 nested-multicol / OOF-drain watchdog hangs).
//   - progress (consumed > prev): advanced → the loop MUST continue, otherwise
//     legitimately fragmenting content is truncated at one column.
//   - first column (prev == NaN): always counts as progress so the first token
//     is never falsely treated as stuck.
func TestColumnBreakTokenAdvanced(t *testing.T) {
	cases := []struct {
		name          string
		consumed      float64
		prevConsumed  float64
		wantAdvanced  bool
		wantLoopBreak bool // the guard breaks the row when !advanced
	}{
		{"first column (prev NaN)", 100, math.NaN(), true, false},
		{"strict progress", 200, 100, true, false},
		{"tiny progress", 100.5, 100, true, false},
		{"stalled (equal consumed)", 600, 600, false, true},
		{"regressed (consumed < prev)", 50, 600, false, true},
		{"zero from zero", 0, 0, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			advanced := columnBreakTokenAdvanced(tc.consumed, tc.prevConsumed)
			if advanced != tc.wantAdvanced {
				t.Errorf("columnBreakTokenAdvanced(%v, %v) = %v, want %v",
					tc.consumed, tc.prevConsumed, advanced, tc.wantAdvanced)
			}
			// The guard's action: break the row iff the token did NOT advance.
			loopBreaks := !advanced
			if loopBreaks != tc.wantLoopBreak {
				t.Errorf("guard break for (%v, %v) = %v, want %v (a wrong sense "+
					"either spins forever or truncates progressing content)",
					tc.consumed, tc.prevConsumed, loopBreaks, tc.wantLoopBreak)
			}
		})
	}
}
