package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
)

// TestBoxTotalBlockSize pins Blink's stitched block-size of a box's
// fragments: the last fragment with a non-zero block-size plus what the
// fragments before it consumed (layout_box_utils.cc:327-352 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBoxTotalBlockSize(t *testing.T) {
	n := &LayoutInputNode{style: css.NewStyle()}
	frag := func(w, h float64) *PhysicalFragment {
		return &PhysicalFragment{Size: geometry.PhysicalSizeFromF64Round(w, h)}
	}
	consumed := func(v float64) *BlockBreakToken {
		return &BlockBreakToken{ConsumedBlockSize: layoutunit.FromFloat64Round(v)}
	}
	n.SetLayoutResult(&LayoutResult{Fragment: frag(40, 100), BreakToken: consumed(100)}, 0)
	n.SetLayoutResult(&LayoutResult{Fragment: frag(30, 70), BreakToken: consumed(170)}, 1)
	n.SetLayoutResult(&LayoutResult{Fragment: frag(30, 0)}, 2)
	if got := BoxTotalBlockSize(n); got != 170 {
		t.Errorf("BoxTotalBlockSize = %v, want 170 (the empty last fragment skipped)", got)
	}
	if got := len(n.PhysicalFragments()); got != 3 {
		t.Errorf("PhysicalFragments has %d fragments, want 3", got)
	}
}
