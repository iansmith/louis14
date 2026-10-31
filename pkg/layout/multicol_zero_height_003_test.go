package layout

import (
	"testing"
	"time"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestZeroHeightClonedChildProgresses reproduces multicol-zero-height-003: a
// box-decoration-break:clone block (border 5px, block-size 2px) in a
// zero-height multicol must make progress, expending 1px of its content box
// per column, so it needs exactly two fragments — each 5+1+5 = 11px tall.
//
// Blink's progress guarantee is the clone-aware capacity: the column's
// capacity is ClampedToValidFragmentainerCapacity(0) = 1px
// (column_layout_algorithm.cc:1914), and the child's own FinishFragmentation
// space left uses FragmentainerSpaceLeft(builder, is_for_children=false)
// (fragmentation_utils.cc:550-551), whose ClampedToValidFragmentainerCapacity
// floor adds the cloned border/padding (fragmentation_utils.h:163-173) — so
// 1px of content fits between the repeated edges @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestZeroHeightClonedChildProgresses(t *testing.T) {
	child := makeNode("div")
	mc := makeNode("div", child)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-width", "100px",
			"width", "300px", "height", "0"),
		child: makeStyle("display", "block", "width", "100px", "height", "2px",
			"border-top-width", "5px", "border-top-style", "solid",
			"border-bottom-width", "5px", "border-bottom-style", "solid",
			"box-decoration-break", "clone"),
	}

	done := make(chan *LayoutResult, 1)
	go func() { done <- layoutMulticolForTest(t, mc, styles) }()
	var result *LayoutResult
	select {
	case result = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("layout did not terminate: the cloned child makes no progress")
	}

	var childHeights []float64
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.BoxType != BoxTypeColumn {
			continue
		}
		for _, gc := range ch.Fragment.Children {
			childHeights = append(childHeights, gc.Fragment.Size.HeightF64())
		}
	}
	if len(childHeights) != 2 || childHeights[0] != 11 || childHeights[1] != 11 {
		t.Errorf("child fragment heights = %v, want [11 11] (5px border + 1px "+
			"content + 5px border per column, two columns)", childHeights)
	}
}
