package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestGridLayout_ReplacedItem_NotStretchedInline is the LOU-253 contract test
// (WPT css-grid/grid-item-percentage-quirk-001).
//
// A replaced grid item (a <canvas> with natural size 10×10) with the default
// self-alignment (justify-self/align-self: normal) must NOT be stretched to the
// grid track's inline size. Per CSS Box Alignment §6.1 and Blink's
// AxisEdgeFromItemPosition (grid_item.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
//
//	case ItemPosition::kNormal:
//	  *auto_behavior = is_replaced ? AutoSizeBehavior::kFitContent
//	                               : AutoSizeBehavior::kStretchImplicit;
//
// i.e. a replaced item resolves `normal` to fit-content (natural size, no
// stretch), unlike a non-replaced item which stretches to fill the track.
//
// With the bug, shouldStretchInline ignores the replaced distinction and the
// canvas is stretched to the 100px column, so the red canvas paints across the
// green overflow:hidden box instead of being clipped off-screen at left:-20px.
func TestGridLayout_ReplacedItem_NotStretchedInline(t *testing.T) {
	canvas := makeNode("canvas")
	canvas.Attributes = map[string]string{"width": "10", "height": "10"}
	grid := makeNode("div", canvas)

	styles := map[*html.Node]*css.Style{
		grid:   makeStyle("display", "grid", "width", "100px"),
		canvas: makeStyle("display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(grid, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	canvasFrag := findFragmentByNode(result.Fragment, canvas)
	if canvasFrag == nil {
		t.Fatalf("no canvas fragment found in layout result")
	}
	if got := canvasFrag.Size.WidthF64(); got != 10 {
		t.Errorf("canvas inline-size: got %.1f, want 10 (replaced grid item with default alignment must keep its natural width, not stretch to the 100px track)", got)
	}
}
