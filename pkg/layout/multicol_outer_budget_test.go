package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestMulticolNested_LineBudgetIgnoresBlockEndBorder (LOU-391): a nested
// multicol's line budget is FragmentainerSpaceLeftForChildren() − line_offset
// (column_layout_algorithm.cc:819-829 @ a9f50e522efa). With
// box-decoration-break:slice nothing is reserved for the block-end
// border/padding (fragmentation_utils.h:187-257): it belongs to the last
// fragment only. Geometry from Chrome 155 (scratchpad probe w1a): a 100px
// outer column holds an inner fragment 100 tall whose two columns are 100
// tall (five 20px blocks each), not 80.
func TestMulticolNested_LineBudgetIgnoresBlockEndBorder(t *testing.T) {
	var blocks []*html.Node
	styles := map[*html.Node]*css.Style{}
	for i := 0; i < 14; i++ {
		b := makeNode("div")
		styles[b] = makeStyle("display", "block", "height", "20px")
		blocks = append(blocks, b)
	}
	innerMC := makeNode("div", blocks...)
	outerMC := makeNode("div", innerMC)
	styles[outerMC] = makeStyle("display", "block", "width", "200px", "height", "100px",
		"column-count", "2", "column-gap", "0", "column-fill", "auto")
	styles[innerMC] = makeStyle("display", "block", "column-count", "2", "column-gap", "0",
		"border-bottom-width", "20px", "border-bottom-style", "solid")

	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		Build()
	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil || len(result.Fragment.Children) == 0 {
		t.Fatal("outer mc layout returned no columns")
	}
	dumpFragmentTree(t, "", result.Fragment)
	inner := childLinkFor(result.Fragment.Children[0].Fragment, innerMC)
	if inner == nil {
		t.Fatal("outer col 1 has no inner mc fragment")
	}
	if got := NewLogicalFragment(wdm, inner.Fragment).BlockSize(); got != 100 {
		t.Errorf("first inner fragment block-size = %v, want 100", got)
	}
	for i, col := range inner.Fragment.Children {
		if got := NewLogicalFragment(wdm, col.Fragment).BlockSize(); got != 100 {
			t.Errorf("first inner fragment column %d block-size = %v, want 100", i, got)
		}
	}
}

// TestMulticolNested_ZeroSpaceLeftIsALimit (LOU-391): with no space left in
// the outer fragmentainer the available outer space is 0, a real limit
// (ConstrainColumnBlockSize clamps to available_outer_space.ClampNegativeToZero(),
// column_layout_algorithm.cc:1743-1746 @ a9f50e522efa), not "unconstrained".
// Columns still accept 1px of content each (ClampedToValidFragmentainerCapacity,
// fragmentation_utils.h:156-173), so the inner multicol places some content
// and reports a break token for the rest — it must not consume everything.
func TestMulticolNested_ZeroSpaceLeftIsALimit(t *testing.T) {
	var blocks []*html.Node
	styles := map[*html.Node]*css.Style{}
	for i := 0; i < 4; i++ {
		b := makeNode("div")
		styles[b] = makeStyle("display", "block", "height", "20px")
		blocks = append(blocks, b)
	}
	innerMC := makeNode("div", blocks...)
	styles[innerMC] = makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto")

	result := layoutMulticolInFragmentainer(t, innerMC, styles, 100, 100)
	if result.BreakToken == nil {
		t.Errorf("inner mc at 0 space left completed with no break token; want a break token for unplaced content")
	}
}
