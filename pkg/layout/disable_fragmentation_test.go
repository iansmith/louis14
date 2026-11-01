package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestDisableFragmentation_ClippedBoxEndsInSecondColumn (LOU-501, worklist
// C4): a block-axis overflow-clipping (overflow-y:clip, not a scroll
// container, so not monolithic) box that reaches its block end in a column
// while a child is still breaking inside it is laid out again without
// fragmentation, so it produces no further (zero-sized, clipped) fragments
// and no superfluous columns. Blink: FinishFragmentation returns
// kDisableFragmentation (fragmentation_utils.cc:748-758 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), FinalizeForFragmentation
// aborts with LayoutResult::kDisableFragmentation (block_layout_algorithm.cc:
// 1479-1487) and HandleNonsuccessfulLayoutResult relays out with
// RelayoutWithoutFragmentation (:652-654, layout_algorithm.h:277-281) using
// ConstraintSpace::CloneWithoutFragmentation (constraint_space.h:143-153).
//
// Mirrors css-multicol/abspos-multicol-in-second-outer-clipped.html: a
// 100px-tall column-fill:auto multicol; the clip box is 200px tall, its
// content 1100px.
func TestDisableFragmentation_ClippedBoxEndsInSecondColumn(t *testing.T) {
	tall := makeNode("div")
	clip := makeNode("div", tall)
	mc := makeNode("div", clip)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
			"column-fill", "auto", "width", "100px", "height", "100px"),
		clip: makeStyle("display", "block", "overflow-y", "clip", "height", "200px"),
		tall: makeStyle("display", "block", "height", "1100px"),
	}
	result := layoutMulticolForTest(t, mc, styles)

	var columns []*PhysicalFragment
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.BoxType == BoxTypeColumn {
			columns = append(columns, ch.Fragment)
		}
	}
	if len(columns) != 2 {
		t.Fatalf("got %d columns, want 2 (no superfluous columns after the clip box ends)", len(columns))
	}
	// The second fragment of the clip box is its last: 100px tall, no
	// further fragments, and its child continues to its own end
	// (1100 - 100 consumed = 1000px), clipped instead of fragmented.
	if len(columns[1].Children) != 1 {
		t.Fatalf("second column has %d children, want 1 (the clip box)", len(columns[1].Children))
	}
	clipFrag := columns[1].Children[0].Fragment
	if h := clipFrag.Size.HeightF64(); h != 100 {
		t.Errorf("second clip-box fragment height = %v, want 100", h)
	}
	if len(clipFrag.Children) != 1 {
		t.Fatalf("second clip-box fragment has %d children, want 1", len(clipFrag.Children))
	}
	if h := clipFrag.Children[0].Fragment.Size.HeightF64(); h != 1000 {
		t.Errorf("tall child's last fragment height = %v, want 1000 (rest of the child, unfragmented)", h)
	}
}
