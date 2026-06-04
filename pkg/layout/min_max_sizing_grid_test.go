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
// (vertical-rl) child containing two inline-blocks (50px x 100px each).
// In vertical-rl: the two blocks stack in the inline direction (100px each),
// giving logical-inline-size=100px, logical-block-size=100px.
// When viewed from the parent's horizontal-tb, the child's physical width=100px
// (the logical-block-size). The grid's min-content inline size should be 100px.
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

	// Second inline-block: 50px width, 100px height
	ib2Style := css.NewStyle()
	ib2Style.Properties["display"] = "inline-block"
	ib2Style.Properties["width"] = "50px"
	ib2Style.Properties["height"] = "100px"

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

	// Assert: the grid's min-content inline size must be 100px
	// The orthogonal child (vertical-rl) with two 50px-wide inline-blocks stacked
	// has logical-block-size=100px. Viewed from parent's horizontal-tb, this
	// becomes physical-width=100px, which is the contribution to the grid's inline
	// min-content (the implicit column track).
	if mm.MinContent != 100 {
		t.Errorf("MinContent: got %.2f, want 100 (orthogonal child's block-size)", mm.MinContent)
	}
	if mm.MaxContent != 100 {
		t.Errorf("MaxContent: got %.2f, want 100 (orthogonal child's block-size)", mm.MaxContent)
	}
}
