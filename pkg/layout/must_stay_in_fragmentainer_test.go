package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestMustStayInCurrentFragmentainer_ClampedBlockSizeForFragmentation
// (LOU-501, worklist C5 + C12 RequiresContentBeforeBreaking): a box whose
// restricted block-size fits in the rest of the column must stay there, and
// so must its first child, even when that (monolithic) child is taller than
// the space left; the child overflows the column. Blink:
// SetupFragmentBuilderForFragmentation sets RequiresContentBeforeBreaking
// from MustStayInCurrentFragmentainer (fragmentation_utils.cc:484-487, :500
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b; box_fragment_builder.h:
// 401-413); MovePastBreakpoint then moves past the child that does not fit
// and calls SetIsBlockSizeForFragmentationClamped (fragmentation_utils.cc:
// 1223-1228), so ToBoxFragment records the box's own block-size as its
// block-size for fragmentation (box_fragment_builder.cc:743-761) and the
// parent sees the box fit.
//
// Column 1 (100px): a 50px block, then a 40px-tall box (fits at offset 50)
// whose first child is an 80px scroll container (monolithic).
func TestMustStayInCurrentFragmentainer_ClampedBlockSizeForFragmentation(t *testing.T) {
	first := makeNode("div")
	mono := makeNode("div")
	box := makeNode("div", mono)
	mc := makeNode("div", first, box)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
			"column-fill", "auto", "width", "200px", "height", "100px"),
		first: makeStyle("display", "block", "height", "50px"),
		box:   makeStyle("display", "block", "height", "40px"),
		mono:  makeStyle("display", "block", "height", "80px", "overflow", "auto"),
	}
	result := layoutMulticolForTest(t, mc, styles)

	var columns []*PhysicalFragment
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.BoxType == BoxTypeColumn {
			columns = append(columns, ch.Fragment)
		}
	}
	if len(columns) != 1 {
		t.Fatalf("got %d columns, want 1 (the box stays in the first column)", len(columns))
	}
	if n := len(columns[0].Children); n != 2 {
		t.Fatalf("first column has %d children, want 2 (the 50px block and the box)", n)
	}
	boxLink := columns[0].Children[1]
	if top := boxLink.Offset.Top.Float64(); top != 50 {
		t.Errorf("box offset = %v, want 50", top)
	}
	if h := boxLink.Fragment.Size.HeightF64(); h != 40 {
		t.Errorf("box height = %v, want 40", h)
	}
	if n := len(boxLink.Fragment.Children); n != 1 {
		t.Fatalf("box has %d children, want 1 (the monolithic child, overflowing)", n)
	}
	if h := boxLink.Fragment.Children[0].Fragment.Size.HeightF64(); h != 80 {
		t.Errorf("monolithic child height = %v, want 80", h)
	}
}
