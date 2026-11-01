package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestBlockLayout_NoListMarkerSeededInAnonymousSpace (LOU-515 item 3): a
// list item laid out in an anonymous space — a column of its multicol —
// leaves the marker to the multicol container. Mirrors Blink's
// ShouldPlaceUnpositionedListMarker gate on the marker seed
// (block_layout_algorithm.h:470-483, block_layout_algorithm.cc:321-329 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBlockLayout_NoListMarkerSeededInAnonymousSpace(t *testing.T) {
	li := &html.Node{Type: html.ElementNode, TagName: "li"}
	li.Children = []*html.Node{{Type: html.TextNode, Text: "item", Parent: li}}
	styles := map[*html.Node]*css.Style{
		li: makeStyle("display", "list-item", "list-style-position", "outside"),
	}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	for _, anonymous := range []bool{false, true} {
		space := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
			SetIsAnonymous(anonymous).
			Build()
		result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(li, styles), space).Layout()
		hasMarker := markerBoxChild(result.Fragment) != nil
		if hasMarker == anonymous {
			t.Errorf("anonymous=%v: marker placed = %v, want %v", anonymous, hasMarker, !anonymous)
		}
	}
}

// TestLayoutLine_NestedAutoHeightTakesOuterSpace (LOU-515 item 2): an
// auto-height column-fill:auto multicol constrained by an outer
// fragmentainer has no content-based column block-size: its columns take
// the available outer space, which is the only clamp on that path —
// max-height constrains only a content-based (balanced or unconstrained)
// column block-size (LayoutLine, column_layout_algorithm.cc:845-872;
// ConstrainColumnBlockSize from ResolveColumnAutoBlockSize @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestLayoutLine_NestedAutoHeightTakesOuterSpace(t *testing.T) {
	innerChild := makeNode("div")
	innerMC := makeNode("div", innerChild)
	outerMC := makeNode("div", innerMC)
	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle("display", "block", "width", "200px", "height", "100px",
			"column-count", "2", "column-gap", "0", "column-fill", "auto"),
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
			"column-fill", "auto", "max-height", "50px"),
		innerChild: makeStyle("display", "block", "height", "300px"),
	}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		Build()
	result := layoutElement(testContext(), buildTestTree(outerMC, styles), space)
	if result == nil || result.Fragment == nil || len(result.Fragment.Children) == 0 {
		t.Fatal("outer multicol produced no columns")
	}
	inner := childLinkFor(result.Fragment.Children[0].Fragment, innerMC)
	if inner == nil || len(inner.Fragment.Children) == 0 {
		t.Fatal("outer column 1 holds no inner multicol columns")
	}
	if got := NewLogicalFragment(wdm, inner.Fragment.Children[0].Fragment).BlockSize(); got != 100 {
		t.Errorf("inner column block-size = %v, want the outer space left, 100", got)
	}
}
