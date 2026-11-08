package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestMeasureGridMinMax verifies that orthogonal grid items contribute their
// block-size (not inline-size) to the grid's inline-axis min-content sizing.
//
// Test case: A grid container (default horizontal-tb) with an orthogonal
// (vertical-rl) child containing two vertical-rl inline-blocks (50px wide,
// 100px tall), in the child's own flow. Each is 100px along the child's
// inline (vertical) axis and 50px along its block (horizontal) axis.
//
// This isolates the defect in measureGridMinMax where orthogonal children's
// block-sizes do not feed the inline track contributions correctly.
func TestMeasureGridMinMax_OrthogonalChildBlockSize(t *testing.T) {
	// Build the DOM: grid { vertical-rl { inline-block 50x100 } { inline-block 50x100 } }
	// In vertical-rl, the two inline-blocks are stacked vertically (in the inline direction).
	ib1 := makeNode("span")
	ib2 := makeNode("span")
	orthoChild := makeNode("div", ib1, ib2)
	gridContainer := makeNode("div", orthoChild)

	// Grid container: display:grid, no explicit columns (default row-flow)
	gridStyle := css.NewStyle()
	gridStyle.Properties["display"] = "grid"

	// Orthogonal child: vertical-rl writing-mode
	orthoStyle := css.NewStyle()
	orthoStyle.Properties["writing-mode"] = "vertical-rl"
	orthoStyle.Properties["line-height"] = "0"

	// First inline-block: 50px width, 100px height
	ib1Style := css.NewStyle()
	ib1Style.Properties["display"] = "inline-block"
	ib1Style.Properties["width"] = "50px"
	ib1Style.Properties["height"] = "100px"
	// The test tree does not inherit writing-mode: set it, so the
	// inline-blocks are in the vertical-rl child's own (parallel) flow.
	ib1Style.Properties["writing-mode"] = "vertical-rl"

	// Second inline-block: 50px width, 100px height
	ib2Style := css.NewStyle()
	ib2Style.Properties["display"] = "inline-block"
	ib2Style.Properties["width"] = "50px"
	ib2Style.Properties["height"] = "100px"
	// The test tree does not inherit writing-mode: set it, so the
	// inline-blocks are in the vertical-rl child's own (parallel) flow.
	ib2Style.Properties["writing-mode"] = "vertical-rl"

	styles := map[*html.Node]*css.Style{
		gridContainer: gridStyle,
		orthoChild:    orthoStyle,
		ib1:           ib1Style,
		ib2:           ib2Style,
	}

	ctx := testContext()
	layoutRoot := buildTestTree(gridContainer, styles)

	// Container writing mode: horizontal-tb, ltr
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	// Call measureGridMinMax on the grid container.
	mm := measureGridMinMax(layoutRoot, ctx, space)

	// The orthogonal child contributes its block size (its physical width)
	// to the grid's inline axis. louis14's measureOrthogonalChild lays it out
	// at its min-content inline size (100: one inline-block per line, two
	// lines, 100 wide) for the min contribution, and at its max-content
	// inline size (200: both on one line, 50 wide) for the max. Blink lays an
	// orthogonal grid item out once and gives 50 for both
	// (GridLayoutAlgorithm::ContributionSizeForGridItem's
	// BlockContributionSize, grid_layout_algorithm.cc:662-715;
	// BlockNode::ComputeMinMaxSizes, block_node.cc:981-1008 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); that gap is LOU-770.
	if mm.MinContent != 100 { // LOU-793: expect 50
		t.Errorf("MinContent: got %.2f, want 100 (block size at the child's min-content)", mm.MinContent)
	}
	// Deliberately away from Blink, tracked by LOU-793: Blink gives 50/50
	// for this configuration (its first column pass gives 50, the auto row
	// grows to the item's 200 max-content, the additional column pass gives
	// 50 again: GridLayoutAlgorithm::ComputeMinMaxSizes,
	// grid_layout_algorithm.cc:287-327 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14 gives 100 because
	// measureOrthogonalChild's min-content contribution of 100 (the LOU-770
	// bug) feeds a faithful ResolveIntrinsicTrackSizes: the auto column's
	// base size is its items' min-content contribution
	// (grid_track_sizing_algorithm.cc:911-1022), and its growth limit is
	// raised to it (grid_track_collection.cc:463-466). LOU-793, which
	// absorbs LOU-770, makes this 50/50.
	if mm.MaxContent != 100 { // LOU-793: expect 50
		t.Errorf("MaxContent: got %.2f, want 100 (the min-content base size; see above)", mm.MaxContent)
	}
}
