package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestMovePastBreakpoint_InnerMulticolAtFragmentainerEndBreaksBefore
// (LOU-383): a child whose border edge starts exactly at the fragmentainer
// end and that broke inside must be broken before, not placed as an empty
// fragment and dropped (MovePastBreakpoint must_break_before,
// fragmentation_utils.cc:1172-1189 @ a9f50e522efa). Mirrors
// multicol-rule-nested-balancing-001 with column-gap:0; Chrome 155 (probe
// prnb0) pushes the 200px inner multicol whole to outer column 2, where its
// two columns are 150 tall.
func TestMovePastBreakpoint_InnerMulticolAtFragmentainerEndBreaksBefore(t *testing.T) {
	innerBlock := makeNode("div")
	innerMC := makeNode("div", innerBlock)
	outerBlock := makeNode("div")
	outerMC := makeNode("div", outerBlock, innerMC)
	styles := map[*html.Node]*css.Style{
		outerMC:    makeStyle("display", "block", "width", "400px", "height", "250px", "column-count", "2", "column-gap", "0"),
		outerBlock: makeStyle("display", "block", "height", "200px"),
		innerMC:    makeStyle("display", "block", "height", "200px", "column-count", "2", "column-gap", "0"),
		innerBlock: makeStyle("display", "block", "height", "300px"),
	}
	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		Build()
	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil {
		t.Fatal("outer mc layout returned nil")
	}
	dumpFragmentTree(t, "", result.Fragment)
	if len(result.Fragment.Children) != 2 {
		t.Fatalf("outer mc has %d columns, want 2", len(result.Fragment.Children))
	}
	if l := childLinkFor(result.Fragment.Children[0].Fragment, innerMC); l != nil {
		t.Errorf("outer col 1 holds an inner mc fragment (h=%v); want it broken before",
			NewLogicalFragment(wdm, l.Fragment).BlockSize())
	}
	inner := childLinkFor(result.Fragment.Children[1].Fragment, innerMC)
	if inner == nil {
		t.Fatal("outer col 2 has no inner mc fragment")
	}
	if got := NewLogicalFragment(wdm, inner.Fragment).BlockSize(); got != 200 {
		t.Errorf("inner mc in outer col 2: block-size = %v, want 200", got)
	}
	if got := len(inner.Fragment.Children); got != 2 {
		t.Fatalf("inner mc in outer col 2 has %d columns, want 2", got)
	}
	for i, col := range inner.Fragment.Children {
		if got := NewLogicalFragment(wdm, col.Fragment).BlockSize(); got != 150 {
			t.Errorf("inner column %d block-size = %v, want 150", i, got)
		}
	}
}
