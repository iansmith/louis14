package layout

import (
	"testing"

	"louis14/pkg/geometry/layoutunit"
)

// TestShouldCloneBlockStartBorderPaddingAfterMonolithicOverflow_LOU420: a
// fragment resumed after monolithic overflow does not clone its block-start
// border/padding when the fragmentainer space left is less than the box's
// block border+padding. Mirrors Blink's ShouldCloneBlockStartBorderPadding
// (fragmentation_utils.cc:70-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Edges are 5+5; the
// fragmentainer is 100 tall.
func TestShouldCloneBlockStartBorderPaddingAfterMonolithicOverflow_LOU420(t *testing.T) {
	for _, tc := range []struct {
		name               string
		monolithicOverflow float64
		fragmentainerOff   float64
		want               bool
	}{
		{"monolithic overflow, space left 5 < 10: no clone", 50, 95, false},
		{"monolithic overflow, space left 10 = 10: clone", 50, 90, true},
		{"monolithic overflow, space left 20 > 10: clone", 50, 80, true},
		{"no monolithic overflow, space left 5: clone", 0, 95, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			space := ConstraintSpace{
				HasBlockFragmentation:  true,
				BlockFragmentationType: FragmentPage,
				FragmentainerBlockSize: 100,
				FragmentainerOffset:    tc.fragmentainerOff,
				BreakToken: &BlockBreakToken{
					ConsumedBlockSize:  layoutunit.FromFloat64Round(40),
					MonolithicOverflow: tc.monolithicOverflow,
				},
			}
			box := FragmentainerBox{
				Space:                   &space,
				BorderScrollbarPadding:  LogicalEdges{BlockStart: 5, BlockEnd: 5},
				HasClonedBoxDecorations: true,
			}
			if got := ShouldCloneBlockStartBorderPadding(box); got != tc.want {
				t.Errorf("ShouldCloneBlockStartBorderPadding = %v, want %v", got, tc.want)
			}
		})
	}
}
