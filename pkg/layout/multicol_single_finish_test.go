package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestMulticolBrokenInMainFlowFinishesLikeAnyFragment (LOU-509): a nested
// multicol that breaks in its main flow (its content continues in the next
// outer fragmentainer) finishes through the same Layout tail as one that
// does not — Blink's ColumnLayoutAlgorithm::Layout has a single
// FinishFragmentation / ToBoxFragment path after LayoutChildren
// (column_layout_algorithm.cc:337-431 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So its fragment is tagged a
// multicol container, as the unbroken path tags every non-empty multicol
// fragment.
func TestMulticolBrokenInMainFlowFinishesLikeAnyFragment(t *testing.T) {
	innerChild := makeNode("div")
	innerMC := makeNode("div", innerChild)
	outerMC := makeNode("div", innerMC)
	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle("display", "block", "width", "100px", "height", "50px",
			"column-count", "2", "column-gap", "0", "column-fill", "auto"),
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
			"column-fill", "auto"),
		innerChild: makeStyle("display", "block", "height", "300px"),
	}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()
	result := layoutElement(testContext(), buildTestTree(outerMC, styles), space)
	if result == nil || result.Fragment == nil || len(result.Fragment.Children) == 0 {
		t.Fatal("outer multicol produced no columns")
	}
	inner := childLinkFor(result.Fragment.Children[0].Fragment, innerMC)
	if inner == nil {
		t.Fatal("outer column 1 holds no inner multicol fragment")
	}
	if !inner.Fragment.IsMulticolContainer {
		t.Error("inner multicol broken in its main flow: IsMulticolContainer = false, want true")
	}
}
