package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestFlexLayout_ColumnMinHeightAutoPercentageChildren mirrors WPT
// css-flexbox/flex-minimum-height-flex-items-017/018: a column flex
// container (height:0, width:100px) holds an item with flex:0 1 0px and
// height:100px. The item contains a percentage-height child chain
// (height:100% → height:100px). The §4.5 content suggestion must treat
// the percentage-resolution block-size as Indefinite so height:100%
// resolves as auto, letting the 100px grandchild size propagate up.
// min-height:auto then keeps the item at 100px instead of shrinking to 0.
//
// Bug: flexItemMinMain's content suggestion layout passes BlockSize=0
// (Go zero value) in SetPercentageResolutionSize instead of Indefinite
// (-1), so percentage children resolve against 0 and the content
// suggestion is 0.
//
// Blink: flex_layout_algorithm.cc content-size suggestion layout uses
// kIndefiniteSize for the percentage resolution block-size
// @ Chromium main d70076d8.
func TestFlexLayout_ColumnMinHeightAutoPercentageChildren(t *testing.T) {
	grandchild := makeNode("div")
	child := makeNode("div", grandchild)
	item := makeNode("div", child)
	flex := makeNode("div", item)

	styles := map[*html.Node]*css.Style{
		flex:       makeStyle("display", "flex", "flex-direction", "column", "width", "100px", "height", "0px"),
		item:       makeStyle("flex-grow", "0", "flex-shrink", "1", "flex-basis", "0px", "height", "100px"),
		child:      makeStyle("height", "100%"),
		grandchild: makeStyle("height", "100px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)
	itemFrag := findFragmentByNode(result.Fragment, item)
	if itemFrag == nil {
		t.Fatal("item fragment not found")
	}
	gotHeight := itemFrag.Size.HeightF64()
	if gotHeight != 100 {
		t.Errorf("item height: got %.1f, want 100 (min-height:auto content suggestion should prevent shrinking)", gotHeight)
	}
}
