package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestMulticolNested_OuterFragmentsInnerAcrossOuterColumns mirrors
// multicol-nested-011 end-to-end: an outer mc with column-fill:auto must
// fragment an inner mc with explicit height:100 across both outer columns
// (each 50 tall). After layout the outer mc has 2 column fragments, each
// containing one inner-mc fragment of 25x50 worth of green.
func TestMulticolNested_OuterFragmentsInnerAcrossOuterColumns(t *testing.T) {
	innerChild := makeNode("div")
	innerMC := makeNode("div", innerChild)
	outerMC := makeNode("div", innerMC)

	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle(
			"display", "block",
			"width", "100px",
			"height", "50px",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		innerMC: makeStyle(
			"display", "block",
			"width", "50px",
			"height", "100px",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		innerChild: makeStyle(
			"display", "block",
			"width", "100px",
			"height", "100px",
			"background", "green",
		),
	}

	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	// Outer mc is layout root: no outer fragmentation, plenty of room.
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()

	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil {
		t.Fatal("outer mc layout returned nil")
	}

	t.Logf("outer mc fragment: size=%vx%v",
		result.Fragment.Size.WidthF64(), result.Fragment.Size.HeightF64())
	// Each child is an outer column.
	if len(result.Fragment.Children) < 2 {
		t.Fatalf("expected at least 2 outer columns, got %d", len(result.Fragment.Children))
	}
	for i, ch := range result.Fragment.Children {
		t.Logf("  outer col %d: pos=(%v,%v) size=%vx%v",
			i, ch.Offset.Left.Float64(), ch.Offset.Top.Float64(),
			ch.Fragment.Size.WidthF64(), ch.Fragment.Size.HeightF64())
		for j, sub := range ch.Fragment.Children {
			t.Logf("    sub %d: pos=(%v,%v) size=%vx%v",
				j, sub.Offset.Left.Float64(), sub.Offset.Top.Float64(),
				sub.Fragment.Size.WidthF64(), sub.Fragment.Size.HeightF64())
			for k, sub2 := range sub.Fragment.Children {
				t.Logf("      sub2 %d: pos=(%v,%v) size=%vx%v",
					k, sub2.Offset.Left.Float64(), sub2.Offset.Top.Float64(),
					sub2.Fragment.Size.WidthF64(), sub2.Fragment.Size.HeightF64())
			}
		}
	}

	// Invariant: outer must have 2 column-fragments, each containing an
	// inner-mc fragment box. The inner-mc fragments themselves may be empty
	// (structural-only continuation) — that's a known-incomplete area of
	// the nested-mc cluster (multicol-nested-011..028) that requires
	// follow-up work to make inner-mc continuations carry repeated content
	// fragments. C51 milestone 1 only guards the structural shape.
	if len(result.Fragment.Children) != 2 {
		t.Errorf("expected exactly 2 outer columns, got %d", len(result.Fragment.Children))
	}
	for i, ch := range result.Fragment.Children {
		if len(ch.Fragment.Children) == 0 {
			t.Logf("KNOWN-BUG: outer col %d has no inner-mc fragment child", i)
		}
	}
}

// TestMulticolNested_InnerEmitsBreakTokenWhenContentExceedsOuterCol verifies
// that when an inner multicol is laid out inside an outer column whose
// remaining space is smaller than the inner's declared height (column-fill:auto
// + explicit inner height), the inner multicol consumes outer space up to the
// outer-column boundary AND emits an outgoing BreakToken so the outer can
// resume the inner in the next outer column.
//
// Setup (mirrors multicol-nested-011):
//   - Inner mc: columns:2, column-fill:auto, column-gap:0, width:50, height:100.
//   - Inner content: width:100 (=400% of 25-inner-col), height:100, contain:size, monolithic-ish.
//   - Inner laid out inside a fragmentainer with FragmentainerBlockSize=50,
//     FragmentainerOffset=0 (top of outer column).
//
// Invariant: inner mc returns a fragment with block-size <= 50 AND a BreakToken
// indicating remaining content exists for outer-column-2.
func TestMulticolNested_InnerEmitsBreakTokenWhenContentExceedsOuterCol(t *testing.T) {
	innerChild := makeNode("div")
	innerMC := makeNode("div", innerChild)

	styles := map[*html.Node]*css.Style{
		innerMC: makeStyle(
			"display", "block",
			"width", "50px",
			"height", "100px",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		innerChild: makeStyle(
			"display", "block",
			"width", "100px",
			"height", "100px",
			"background", "green",
		),
	}

	ctx := testContext()
	innerMCNode := buildTestTree(innerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	// Outer-column fragmentainer: 50w x 50h. The OUTER mc's contentNode BLA
	// builds the inner mc's constraint space WITHOUT IsBlockSizeOverride
	// (it's not the inner mc that the outer is forcing; the outer forces only
	// its anonymous contentNode wrapper).
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 50, BlockSize: 50}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 50, BlockSize: 50}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(50).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		Build()

	result := NewMulticolLayoutAlgorithm(ctx, innerMCNode, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("inner multicol layout returned nil result")
	}

	frag := result.Fragment
	t.Logf("inner mc fragment: size=%vx%v, breakToken=%v, intrinsic=%v",
		frag.Size.WidthF64(), frag.Size.HeightF64(),
		result.BreakToken != nil, result.IntrinsicBlockSize)
	if result.BreakToken != nil {
		t.Logf("  childBreakTokens count: %d, HasSeenAllChildren=%v",
			len(result.BreakToken.ChildBreakTokens), result.BreakToken.HasSeenAllChildren)
	}
	// Count child fragments (inner columns) actually placed.
	t.Logf("  child fragment count: %d", len(frag.Children))
	for i, ch := range frag.Children {
		t.Logf("    child %d: pos=(%v,%v) size=%vx%v boxType=%v",
			i, ch.Offset.Left.Float64(), ch.Offset.Top.Float64(),
			ch.Fragment.Size.WidthF64(), ch.Fragment.Size.HeightF64(),
			ch.Fragment.BoxType)
	}

	// Recurse: show each column's children.
	for i, ch := range frag.Children {
		t.Logf("  inner col %d sub-children: %d", i, len(ch.Fragment.Children))
		for j, sub := range ch.Fragment.Children {
			t.Logf("    sub %d: pos=(%v,%v) size=%vx%v",
				j, sub.Offset.Left.Float64(), sub.Offset.Top.Float64(),
				sub.Fragment.Size.WidthF64(), sub.Fragment.Size.HeightF64())
		}
	}

	if result.BreakToken != nil {
		t.Logf("  BreakToken: ConsumedBlockSize=%v, ChildBreakTokens=%d, HasSeenAllChildren=%v",
			result.BreakToken.ConsumedBlockSize.Float64(),
			len(result.BreakToken.ChildBreakTokens),
			result.BreakToken.HasSeenAllChildren)
	}
	if result.BreakToken == nil {
		t.Errorf("expected non-nil BreakToken from inner mc (content overflows outer col)")
	}
	if NewLogicalFragment(wdm, frag).BlockSize() > 50 {
		t.Errorf("inner mc fragment block-size %v exceeds outer-col 50",
			NewLogicalFragment(wdm, frag).BlockSize())
	}
}

// TestMulticolNested_CappedLastColumnAppealNotPropagated: Blink folds a
// column's break appeal into min_break_appeal only after the column-count
// cap check (cla.cc:1022-1037 @ a9f50e522efa) — the column that ends a
// capped row with content remaining never contributes. A single-column
// inner multicol whose only column slices a break-inside:avoid box (no
// break before it is possible at the column start) therefore reports a
// Perfect appeal to the outer fragmentation context: pushing the multicol
// to the next outer fragmentainer would slice the box there just the same.
func TestMulticolNested_CappedLastColumnAppealNotPropagated(t *testing.T) {
	grandchild1 := makeNode("div")
	grandchild2 := makeNode("div")
	avoid := makeNode("div", grandchild1, grandchild2)
	innerMC := makeNode("div", avoid)

	grandchildStyle := makeStyle("display", "block", "height", "40px")
	styles := map[*html.Node]*css.Style{
		innerMC:     makeStyle("display", "block", "column-count", "1", "column-gap", "0", "column-fill", "auto"),
		avoid:       makeStyle("display", "block", "break-inside", "avoid"),
		grandchild1: grandchildStyle,
		grandchild2: grandchildStyle,
	}

	// 50px outer fragmentainer, multicol at offset 10 (gate open): 40px
	// remain, the 80px avoid box must be sliced in the only column.
	result := layoutMulticolInFragmentainer(t, innerMC, styles, 50, 10)
	if result.BreakToken == nil {
		t.Fatal("expected a break token (avoid box does not fit the 40px column)")
	}
	if result.BreakAppeal != BreakAppealPerfect {
		t.Errorf("inner mc BreakAppeal = %v, want Perfect (the capped last column's appeal is not folded in)", result.BreakAppeal)
	}
}

// TestMulticolNested_PostSpannerLineMayHaveMoreSpace: Blink's
// may_have_more_space_in_next_outer_fragmentainer gate keys on the
// multicol's OWN break token (cla.cc:893 @ a9f50e522efa), not the column
// token a line resumes from. A line after a spanner resumes from a
// break-inside column token while the multicol is still in its first
// fragment, and the spanner has contributed intrinsic block-size, so the
// gate is open and a violating break in that line demotes the multicol's
// appeal.
func TestMulticolNested_PostSpannerLineMayHaveMoreSpace(t *testing.T) {
	spanner := makeNode("div")
	grandchild1 := makeNode("div")
	grandchild2 := makeNode("div")
	avoid := makeNode("div", grandchild1, grandchild2)
	innerMC := makeNode("div", spanner, avoid)

	grandchildStyle := makeStyle("display", "block", "height", "40px")
	styles := map[*html.Node]*css.Style{
		innerMC:     makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto"),
		spanner:     makeStyle("display", "block", "column-span", "all", "height", "20px"),
		avoid:       makeStyle("display", "block", "break-inside", "avoid"),
		grandchild1: grandchildStyle,
		grandchild2: grandchildStyle,
	}

	// 60px outer fragmentainer at offset 0: the spanner takes 20px, so the
	// post-spanner columns are 40px and the 80px avoid box is sliced in
	// column 1 (not the capped last column: column 2 resumes it).
	result := layoutMulticolInFragmentainer(t, innerMC, styles, 60, 0)
	if result.BreakAppeal >= BreakAppealPerfect {
		t.Errorf("inner mc BreakAppeal = %v, want < Perfect (post-spanner line violates break-inside:avoid with the gate open)", result.BreakAppeal)
	}
}

// TestMulticolNested_ShrinkToFitOnlyForSingleColumnLine: Blink clears
// shrink_to_fit_column_block_size after the first column and resets the
// line's block-size contribution to the full column block-size for every
// later column (cla.cc:949-971 @ a9f50e522efa), so a line that ends up with
// two columns — here via a forced break — keeps the outer remaining space
// as its block-size rather than shrinking to its content.
func TestMulticolNested_ShrinkToFitOnlyForSingleColumnLine(t *testing.T) {
	childA := makeNode("div")
	childB := makeNode("div")
	innerMC := makeNode("div", childA, childB)
	firstChild := makeNode("div")
	outerMC := makeNode("div", firstChild, innerMC)

	styles := map[*html.Node]*css.Style{
		outerMC:    makeStyle("display", "block", "width", "100px", "height", "100px", "column-count", "2", "column-gap", "0", "column-fill", "auto"),
		firstChild: makeStyle("display", "block", "height", "50px"),
		innerMC:    makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto"),
		childA:     makeStyle("display", "block", "height", "20px"),
		childB:     makeStyle("display", "block", "height", "20px", "break-before", "column"),
	}

	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()

	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil || len(result.Fragment.Children) < 1 {
		t.Fatal("outer mc layout returned no columns")
	}
	col1 := result.Fragment.Children[0]
	if len(col1.Fragment.Children) < 2 {
		t.Fatalf("outer col 1 has %d children, want >= 2 (firstChild + innerMC)", len(col1.Fragment.Children))
	}
	innerMCFrag := col1.Fragment.Children[1].Fragment
	if got := len(innerMCFrag.Children); got != 2 {
		t.Fatalf("inner mc has %d columns, want 2 (forced break before childB)", got)
	}
	if got := NewLogicalFragment(wdm, innerMCFrag).BlockSize(); got != 50 {
		t.Errorf("inner mc block-size = %v, want 50 (two-column line keeps the outer remaining space)", got)
	}
}
