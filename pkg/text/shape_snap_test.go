package text

import (
	"testing"

	"louis14/pkg/geometry/layoutunit"
)

func TestShapeWidthSnap(t *testing.T) {
	cases := []struct {
		v       float64
		wantRaw int32
	}{
		{0.0, 0},
		{1.0 / 64.0, 1},  // exact-on-quantum
		{0.0001, 1},      // sub-quantum positive → CEIL up to 1 raw
		{1.0, 64},        // exact 1 px
		{1.0 + 1e-9, 65}, // sub-quantum residue past 1 px → 65 raw
	}
	for _, c := range cases {
		if got := shapeWidthSnap(c.v).Raw(); got != c.wantRaw {
			t.Errorf("shapeWidthSnap(%v).Raw() = %d, want %d", c.v, got, c.wantRaw)
		}
	}
}

// TestShapeAdvancePairSnap verifies the pair-of-positions invariant on the
// representative HarfBuzz hot-path inputs (exact-1/64-px) and on the
// future-proofing case (sub-quantum residue). The load-bearing claim:
// End[i] - Start[i] is 0 at exact quanta and 1 raw quantum otherwise.
func TestShapeAdvancePairSnap(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s, e := shapeAdvancePairSnap(nil)
		if len(s) != 0 || len(e) != 0 {
			t.Errorf("empty input: got len(start)=%d len(end)=%d, want 0,0", len(s), len(e))
		}
	})

	t.Run("HarfBuzz hot path: exact-1/64-px values, Start==End everywhere", func(t *testing.T) {
		// Simulates int32 XAdvance / 64.0 cumulative positions: every entry
		// lands on an exact LayoutUnit raw quantum boundary.
		const q = 1.0 / 64.0
		input := []float64{0, 8 * q, 16 * q, 32 * q, 64 * q, 96 * q, 12345 * q}
		start, end := shapeAdvancePairSnap(input)
		for i := range input {
			if start[i].Raw() != end[i].Raw() {
				t.Errorf("i=%d (v=%v exact-quantum): start.Raw=%d end.Raw=%d, want equal",
					i, input[i], start[i].Raw(), end[i].Raw())
			}
			if got, want := end[i].Sub(start[i]).Raw(), int32(0); got != want {
				t.Errorf("i=%d: end-start=%d, want 0 (exact-quantum input must round-trip lossless)", i, got)
			}
		}
	})

	t.Run("sub-quantum residue, End-Start = 1 raw quantum", func(t *testing.T) {
		// Values with sub-1/64-px residue.
		input := []float64{0.01, 1.0 + 1e-9, 1000.0 + 0.005}
		start, end := shapeAdvancePairSnap(input)
		for i := range input {
			diff := end[i].Sub(start[i]).Raw()
			if diff != 1 {
				t.Errorf("i=%d (v=%v sub-quantum): end-start=%d, want 1", i, input[i], diff)
			}
		}
	})

	t.Run("monotone non-decreasing input → both Start and End are monotone non-decreasing", func(t *testing.T) {
		// ShapeAdvances guarantees its cum slice is monotone non-decreasing
		// (cluster positions are forward-filled). Both snapped slices must
		// preserve that — a regression here would cause negative segment
		// widths in line_breaker's cum[end].Sub(cum[start]).
		input := []float64{0, 0.3, 0.5, 0.5, 1.7, 1.700001, 100.0}
		start, end := shapeAdvancePairSnap(input)
		var prevS, prevE layoutunit.LayoutUnit
		for i := range input {
			if i > 0 {
				if start[i].Less(prevS) {
					t.Errorf("i=%d start went backward: %v < %v", i, start[i].Float64(), prevS.Float64())
				}
				if end[i].Less(prevE) {
					t.Errorf("i=%d end went backward: %v < %v", i, end[i].Float64(), prevE.Float64())
				}
			}
			prevS, prevE = start[i], end[i]
		}
	})

	t.Run("pair-snap upper-bound is at most 2 raw quanta over the unsnapped float diff", func(t *testing.T) {
		// The load-bearing 13f claim: pair-of-positions over-reports the
		// unsnapped width by at most 2 quanta total, regardless of how many
		// cluster offsets are between s and e. This is the property that
		// makes pair-of-snaps better than summing-snapped-widths
		// (which would accumulate N quanta over N clusters).
		const q = 1.0 / 64.0
		// 100 cluster positions, each with a 0.001-px sub-quantum residue.
		input := make([]float64, 100)
		for i := range input {
			input[i] = float64(i) * (q + 0.001/100.0)
		}
		start, end := shapeAdvancePairSnap(input)
		s, e := 0, len(input)-1
		unsnapped := input[e] - input[s]
		snapped := end[e].Sub(start[s]).Float64()
		overReport := snapped - unsnapped
		// 2 raw quanta = 2/64 = 0.03125. Allow a tiny float-arithmetic slack.
		if overReport < 0 || overReport > 2.0/64.0+1e-9 {
			t.Errorf("100-cluster pair-snap over-report = %v, want in [0, 2/64]", overReport)
		}
	})
}
