package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestNestedMulticol_ShrinkToFitColumnBlockSize verifies that when an inner
// multicol has indefinite height and its content is shorter than the outer
// fragmentainer's remaining space, the inner multicol's fragment shrinks to
// content height rather than consuming all available outer space.
//
// Setup (mirrors multicol-nested-013's structure):
//   - Outer: columns:2, column-fill:auto, column-gap:0, width:100, height:100
//   - First outer child: height:50 (consumes half of first outer column)
//   - Inner multicol: columns:2, column-fill:auto, column-gap:0, no explicit height
//   - Inner content: one div, height:30 (fits in first inner column with room to spare)
//
// Expected: The inner multicol fragment should have block-size ~30 (content height),
// NOT 50 (remaining outer space). This is Blink's "shrink_to_fit_column_block_size"
// behavior (cla.cc:857-872 @ a9f50e522efa).
func TestNestedMulticol_ShrinkToFitColumnBlockSize(t *testing.T) {
	innerChild := makeNode("div")
	innerMC := makeNode("div", innerChild)
	firstChild := makeNode("div")
	outerMC := makeNode("div", firstChild, innerMC)

	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle(
			"display", "block",
			"width", "100px",
			"height", "100px",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		firstChild: makeStyle(
			"display", "block",
			"height", "50px",
			"background", "green",
		),
		innerMC: makeStyle(
			"display", "block",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		innerChild: makeStyle(
			"display", "block",
			"height", "30px",
			"background", "green",
		),
	}

	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()

	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil {
		t.Fatal("outer mc layout returned nil")
	}

	// The outer mc should have at least 1 column.
	if len(result.Fragment.Children) < 1 {
		t.Fatalf("expected at least 1 outer column, got %d", len(result.Fragment.Children))
	}

	// First outer column should contain: firstChild (50px) + innerMC.
	col1 := result.Fragment.Children[0]
	if len(col1.Fragment.Children) < 2 {
		t.Fatalf("outer col 1 has %d children, want >= 2 (firstChild + innerMC)", len(col1.Fragment.Children))
	}

	innerMCFrag := col1.Fragment.Children[1]
	innerBlockSize := NewLogicalFragment(wdm, innerMCFrag.Fragment).BlockSize()

	// The inner multicol should shrink to ~30px (content height), not expand
	// to 50px (remaining outer space).
	if innerBlockSize > 35 {
		t.Errorf("inner mc block-size = %v, want <= 35 (shrink-to-fit content height 30); "+
			"got full outer remaining instead of shrink-to-fit", innerBlockSize)
	}
}

// TestNestedMulticol_BreakAppealPropagation verifies that when content inside
// an inner multicol column violates break-inside:avoid, the inner multicol's
// layout result carries a sub-perfect BreakAppeal, signaling to the outer
// fragmentainer that a better breakpoint may exist.
//
// Blink ref: may_have_more_space_in_next_outer_fragmentainer + min_break_appeal
// (cla.cc:893-902, :1027-1066 @ a9f50e522efa).
func TestNestedMulticol_BreakAppealPropagation(t *testing.T) {
	// Inner content: a break-inside:avoid container with two children,
	// totaling 80px — taller than the inner column height (50px, constrained
	// by outer remaining space). The BLA must break between the children,
	// violating break-inside:avoid, which should demote the appeal.
	grandchild1 := makeNode("div")
	grandchild2 := makeNode("div")
	innerChild := makeNode("div", grandchild1, grandchild2)
	innerMC := makeNode("div", innerChild)

	grandchildStyle := makeStyle(
		"display", "block",
		"height", "40px",
		"background", "green",
	)
	styles := map[*html.Node]*css.Style{
		innerMC: makeStyle(
			"display", "block",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		innerChild: makeStyle(
			"display", "block",
			"break-inside", "avoid",
		),
		grandchild1: grandchildStyle,
		grandchild2: grandchildStyle,
	}

	ctx := testContext()
	innerMCNode := buildTestTree(innerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	// Simulate being inside an outer fragmentainer with 50px remaining.
	// The inner child is 80px with break-inside:avoid, which can't fit
	// in a 50px column without violating the avoidance.
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 50}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: 50}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(50).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		Build()

	result := NewMulticolLayoutAlgorithm(ctx, innerMCNode, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("inner multicol layout returned nil result")
	}

	// The inner multicol should report a sub-perfect break appeal because
	// the break-inside:avoid constraint was violated.
	if result.BreakAppeal >= BreakAppealPerfect {
		t.Errorf("inner mc BreakAppeal = %v, want < BreakAppealPerfect; "+
			"break-inside:avoid violation should propagate sub-perfect appeal",
			result.BreakAppeal)
	}
}

// TestNestedMulticol_ContentDistribution verifies that content inside a nested
// multicol fills inner columns correctly before content overflows to the next
// outer fragmentainer. This is the core "green square" test pattern: an outer
// 2-col multicol (100x100, column-fill:auto, gap:0) with inner content that
// should fill both inner columns, covering the entire 100x100 area in green.
//
// Setup (simplified multicol-nested-013):
//   - Outer: columns:2, column-fill:auto, column-gap:0, width:100, height:100
//   - First outer child: height:50 (fills half of first outer col)
//   - Inner multicol: columns:2, column-fill:auto, column-gap:0
//   - Inner content 1: height:50 (fills first inner col at 25x50)
//   - Inner content 2: height:100, break-inside:avoid
//
// Expected: The inner multicol's first fragment (in outer col 1) should have
// both inner columns populated. The second inner child should either:
//   - Fit in the second inner column of the first outer column, OR
//   - Be pushed to the next outer column (break-inside:avoid honored)
//
// Either way, the inner multicol should produce a break token when it can't
// fit all content in the first outer column.
func TestNestedMulticol_ContentDistribution(t *testing.T) {
	innerChild1 := makeNode("div")
	innerChild2 := makeNode("div")
	innerMC := makeNode("div", innerChild1, innerChild2)
	firstChild := makeNode("div")
	outerMC := makeNode("div", firstChild, innerMC)

	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle(
			"display", "block",
			"width", "100px",
			"height", "100px",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		firstChild: makeStyle(
			"display", "block",
			"height", "50px",
			"background", "green",
		),
		innerMC: makeStyle(
			"display", "block",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		innerChild1: makeStyle(
			"display", "block",
			"height", "50px",
			"background", "green",
		),
		innerChild2: makeStyle(
			"display", "block",
			"height", "100px",
			"break-inside", "avoid",
			"background", "green",
		),
	}

	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()

	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil {
		t.Fatal("outer mc layout returned nil")
	}

	// The outer mc should use both columns (inner multicol content overflows
	// first outer column).
	if len(result.Fragment.Children) < 2 {
		t.Fatalf("expected 2 outer columns, got %d; inner content should overflow "+
			"to second outer column", len(result.Fragment.Children))
	}

	// Check that the second outer column has content (the inner multicol
	// continuation or the break-inside:avoid div).
	col2 := result.Fragment.Children[1]
	if len(col2.Fragment.Children) == 0 {
		t.Errorf("outer col 2 has no children; inner multicol content should " +
			"continue into the second outer column")
	}

	// The total content placed across both outer columns should account for
	// all inner content (50px firstChild + inner multicol with 50+100 content).
	// Verify the outer mc uses its full height.
	outerBlockSize := NewLogicalFragment(wdm, result.Fragment).BlockSize()
	if outerBlockSize < 99 {
		t.Errorf("outer mc block-size = %v, want ~100 (should use full declared height "+
			"to fit all content across columns)", outerBlockSize)
	}
}
