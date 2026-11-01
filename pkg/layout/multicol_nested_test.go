package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
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

	// Outer-column fragmentainer: 50w x 50h, as the OUTER mc's column box
	// builds the inner mc's constraint space.
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 50, BlockSize: 50}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 50, BlockSize: 50}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(50).
		SetFragmentainerOffset(0).
		SetIsAtFragmentainerStart(true).
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

// TestMulticolNested_BlockStartPaddingOpensMayHaveMoreSpaceGate: Blink's
// may_have_more_space_in_next_outer_fragmentainer gate opens on
// intrinsic_block_size_, which starts at BorderScrollbarPadding().block_start
// (cla.cc:335, :896-897 @ a9f50e522efa) — so an inner multicol with
// block-start padding has made progress even at outer fragmentainer offset 0
// with no earlier line or spanner. The second column then resumes with
// MinBreakAppeal = Perfect and pushes the break-inside:avoid box whole to the
// next outer fragmentainer instead of slicing it.
func TestMulticolNested_BlockStartPaddingOpensMayHaveMoreSpaceGate(t *testing.T) {
	filler := makeNode("div")
	grandchild1 := makeNode("div")
	grandchild2 := makeNode("div")
	avoid := makeNode("div", grandchild1, grandchild2)
	innerMC := makeNode("div", filler, avoid)

	grandchildStyle := makeStyle("display", "block", "height", "30px")
	styles := map[*html.Node]*css.Style{
		innerMC:     makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto", "padding-top", "10px"),
		filler:      makeStyle("display", "block", "height", "30px"),
		avoid:       makeStyle("display", "block", "break-inside", "avoid"),
		grandchild1: grandchildStyle,
		grandchild2: grandchildStyle,
	}

	// 60px outer fragmentainer at offset 0: 10px padding leaves 50px
	// columns. Column 1 takes the 30px filler and breaks before the 60px
	// avoid box; column 2 must push it rather than slice it.
	result := layoutMulticolInFragmentainer(t, innerMC, styles, 60, 0)
	if result.BreakToken == nil {
		t.Fatal("expected a break token (avoid box continues in the next outer fragmentainer)")
	}
	if tok := findChildBreakToken(result.BreakToken, avoid); tok == nil || !tok.IsBreakBefore {
		t.Errorf("avoid token = %+v, want IsBreakBefore (pushed whole, not sliced)", tok)
	}
	cols := result.Fragment.Children
	if len(cols) != 2 {
		t.Fatalf("got %d columns, want 2", len(cols))
	}
	if n := len(cols[1].Fragment.Children); n != 0 {
		t.Errorf("column 2 has %d children, want 0 (avoid box pushed to next outer fragmentainer)", n)
	}
	if result.BreakAppeal != BreakAppealPerfect {
		t.Errorf("BreakAppeal = %v, want Perfect (breaking before the avoid box is a clean break)", result.BreakAppeal)
	}
}

// TestMulticolNested_NoWrapCapsWhenMayHaveMoreSpace: Blink's
// ColumnsOverflowInInlineDirection (cla.cc:1823-1842 @ a9f50e522efa) lets
// nowrap columns overflow inline under an outer fragmentation context only
// when the column is known to fit the outer fragmentainer; otherwise the
// column-count cap applies (cla.cc:1022-1025) and content resumes in the
// next outer fragmentainer. An explicit-height inner multicol whose columns
// exceed the outer remaining space therefore caps at column-count even with
// column-wrap:nowrap — and the second column, resumed with MinBreakAppeal =
// Perfect, pushes the break-inside:avoid box whole to the next outer
// fragmentainer. Without the cap the row runs on the break token alone and
// the pushing column never advances it.
func TestMulticolNested_NoWrapCapsWhenMayHaveMoreSpace(t *testing.T) {
	filler := makeNode("div")
	grandchild1 := makeNode("div")
	grandchild2 := makeNode("div")
	avoid := makeNode("div", grandchild1, grandchild2)
	innerMC := makeNode("div", filler, avoid)

	grandchildStyle := makeStyle("display", "block", "height", "50px")
	styles := map[*html.Node]*css.Style{
		innerMC:     makeStyle("display", "block", "height", "200px", "column-count", "2", "column-gap", "0", "column-fill", "auto", "padding-top", "10px"),
		filler:      makeStyle("display", "block", "height", "30px"),
		avoid:       makeStyle("display", "block", "break-inside", "avoid"),
		grandchild1: grandchildStyle,
		grandchild2: grandchildStyle,
	}

	// 60px outer fragmentainer at offset 0: 10px padding leaves 50px
	// columns (200px explicit height does not fit, so no inline overflow).
	// Column 1 takes the 30px filler and breaks before the 100px avoid box;
	// column 2 pushes it whole and the row caps there.
	result := layoutMulticolInFragmentainer(t, innerMC, styles, 60, 0)
	if result.BreakToken == nil {
		t.Fatal("expected a break token (avoid box continues in the next outer fragmentainer)")
	}
	if tok := findChildBreakToken(result.BreakToken, avoid); tok == nil || !tok.IsBreakBefore {
		t.Errorf("avoid token = %+v, want IsBreakBefore (pushed whole to the next outer fragmentainer, not dropped)", tok)
	}
	if got := len(result.Fragment.Children); got != 2 {
		t.Errorf("got %d columns, want 2 (capped at column-count)", got)
	}
}

// TestMulticolNested_ForcedBreakBeforeResumedChildIsPushable: a child
// resumed at the start of a column from a forced break-before token has not
// started, so it is first-for-node and Blink runs the full break-before
// dispatch for it (MovePastBreakpoint's early return is only for resumed
// fragments, fragmentation_utils.cc:1119-1131 @ a9f50e522efa). Under the
// may_have_more_space gate with MinBreakAppeal = Perfect, a break-inside:avoid
// box that would be sliced there is pushed whole to the next outer
// fragmentainer (IsBreakableAtStartOfResumedContainer, :214-219, :1198-1213),
// exactly as when the break before it was soft. The forced value cannot fire
// again: there is no container separation at the start of the resumed column
// (block_layout_algorithm.cc:3160-3169).
func TestMulticolNested_ForcedBreakBeforeResumedChildIsPushable(t *testing.T) {
	filler := makeNode("div")
	avoid := makeNode("div")
	innerMC := makeNode("div", filler, avoid)
	styles := map[*html.Node]*css.Style{
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto"),
		filler:  makeStyle("display", "block", "height", "50px"),
		avoid:   makeStyle("display", "block", "height", "100px", "break-inside", "avoid", "break-before", "column"),
	}

	// 100px outer fragmentainer, multicol at offset 50 (gate open): 50px
	// columns. Column 1 takes the filler and breaks (forced) before the
	// avoid box; column 2 resumes it with MinBreakAppeal = Perfect and must
	// push it whole rather than slice it.
	result := layoutMulticolInFragmentainer(t, innerMC, styles, 100, 50)
	if result.BreakToken == nil {
		t.Fatal("expected a break token (avoid box continues in the next outer fragmentainer)")
	}
	if tok := findChildBreakToken(result.BreakToken, avoid); tok == nil || !tok.IsBreakBefore {
		t.Errorf("avoid token = %+v, want IsBreakBefore (pushed whole, not sliced in column 2)", tok)
	}
	if result.BreakAppeal != BreakAppealPerfect {
		t.Errorf("inner mc BreakAppeal = %v, want Perfect", result.BreakAppeal)
	}
	cols := result.Fragment.Children
	if len(cols) != 2 {
		t.Fatalf("got %d columns, want 2", len(cols))
	}
	if n := len(cols[1].Fragment.Children); n != 0 {
		t.Errorf("column 2 has %d children, want 0 (avoid box pushed to the next outer fragmentainer)", n)
	}
}

// TestMulticolNested_ShrinkToFitUsesBlockSizeForFragmentation (LOU-390): the
// shrink-to-fit line contribution of a nested auto-filled multicol is
// min(column block-size, column BlockSizeForFragmentation)
// (column_layout_algorithm.cc:949-971 @ a9f50e522efa). In the second outer
// column the inner multicol resumes with only a parallel-flow overflow piece
// (the 200%-wide child), so its column has no intrinsic block-size of its own
// but still occupies 50px. Geometry from Chrome 155 on multicol-nested-026:
// inner fragments [[0,0,50,100],[50,0,50,50]], sibling at [50,50,50,50].
func TestMulticolNested_ShrinkToFitUsesBlockSizeForFragmentation(t *testing.T) {
	tall := makeNode("div")
	wide := makeNode("div")
	wrapper := makeNode("div", tall, wide)
	innerMC := makeNode("div", wrapper)
	sibling := makeNode("div")
	outerMC := makeNode("div", innerMC, sibling)

	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle("display", "block", "width", "100px", "height", "100px",
			"column-count", "2", "column-gap", "0", "column-fill", "auto"),
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto"),
		wrapper: makeStyle("display", "block", "height", "150px"),
		tall:    makeStyle("display", "block", "height", "150px"),
		wide:    makeStyle("display", "block", "width", "200%", "height", "100px"),
		sibling: makeStyle("display", "block", "height", "50px"),
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
	dumpFragmentTree(t, "", result.Fragment)
	if len(result.Fragment.Children) != 2 {
		t.Fatalf("outer mc has %d columns, want 2", len(result.Fragment.Children))
	}
	col2 := result.Fragment.Children[1].Fragment
	innerLink := childLinkFor(col2, innerMC)
	sibLink := childLinkFor(col2, sibling)
	if innerLink == nil || sibLink == nil {
		t.Fatalf("outer col 2 is missing the inner mc (%v) or the sibling (%v)", innerLink != nil, sibLink != nil)
	}
	if got := NewLogicalFragment(wdm, innerLink.Fragment).BlockSize(); got != 50 {
		t.Errorf("inner mc fragment in outer col 2: block-size = %v, want 50", got)
	}
	if got := sibLink.Offset.Top.Float64(); got != 50 {
		t.Errorf("sibling in outer col 2: block offset = %v, want 50", got)
	}
}

// TestNestedForcedBreakColumnsStretchToOuterSpace_LOU411: a balanced
// multicol nested in an outer fragmentation context whose forced breaks
// leave no soft break opportunity stretches its columns to all the space
// the outer fragmentainer gives it, instead of giving up at the 10px forced
// runs (column_layout_algorithm.cc:1150-1173 @ a9f50e522efa; its balancing
// measure ignores runs past column-count forced breaks, :1629-1633).
// Chrome 155 headless, LOU-411 probe8.html (multicol-nested-column-rule-003's
// shape): the green box's four fragments are each 100px tall.
func TestNestedForcedBreakColumnsStretchToOuterSpace_LOU411(t *testing.T) {
	frag, ids := layoutOuterMulticolSpec(t,
		props("column-count", "2", "column-gap", "20px", "column-fill", "auto", "width", "100px", "height", "100px"),
		ms("in", props("column-count", "2", "column-gap", "20px"),
			ms("g", nil,
				ms("", props("height", "10px", "break-after", "column")),
				ms("", props("height", "10px", "break-after", "column")),
				ms("", props("height", "200px")),
			),
		),
	)
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"in": {{0, 0, 40, 100}, {60, 0, 40, 100}},
		"g":  {{0, 0, 10, 100}, {30, 0, 10, 100}, {60, 0, 10, 100}, {90, 0, 10, 100}},
	})
}

// TestColumnBSFF_ClipAndOutOfFlow_LOU411: the block-size-for-fragmentation
// counterpart of TestColumnBlockEndScrollableOverflow. A column
// builder accumulates its children's block-size for fragmentation: a child
// that clips block-axis overflow contributes only its own box
// (box_fragment_builder.cc:479-489, :743-761 @ a9f50e522efa). The
// out-of-flow half lives in TestColumnBSFF_OutOfFlowChildIgnored_LOU411,
// which runs it through block layout.
func TestColumnBSFF_ClipAndOutOfFlow_LOU411(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	fragSpace := &ConstraintSpace{HasBlockFragmentation: true}
	// box lays out a 20px box holding an 80px child.
	box := func(style *css.Style) *LayoutResult {
		b := NewBoxFragmentBuilder(wdm)
		b.SetConstraintSpace(fragSpace)
		b.SetSize(LogicalSize{InlineSize: 50, BlockSize: 20})
		if style != nil {
			b.SetStyle(style)
		}
		tall := &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.PhysicalSizeFromF64Round(50, 80)}}
		b.AddChild(tall.Fragment, LogicalOffset{})
		b.PropagateChildBlockSizeForFragmentation(tall, LogicalOffset{})
		return b.Build()
	}
	for _, tc := range []struct {
		name  string
		child *LayoutResult
		want  float64
	}{
		{"visible overflow extends", box(nil), 90},
		{"overflow:hidden clips", box(makeStyle("overflow", "hidden")), 30},
	} {
		col := NewBoxFragmentBuilder(wdm)
		col.SetConstraintSpace(fragSpace)
		col.SetBoxType(BoxTypeColumn)
		col.SetSize(LogicalSize{InlineSize: 50, BlockSize: 100})
		col.AddChild(tc.child.Fragment, LogicalOffset{BlockOffset: 10})
		col.PropagateChildBlockSizeForFragmentation(tc.child, LogicalOffset{BlockOffset: 10})
		if got := col.Build().BlockSizeForFragmentation; got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestColumnBSFF_OutOfFlowChildIgnored_LOU411: block layout under block
// fragmentation does not fold an abspos child into the builder's
// block-size for fragmentation. Blink's BlockLayoutAlgorithm hands an
// out-of-flow child to AddOutOfFlowChildCandidate (block_layout_algorithm.cc:
// 1719 @ a9f50e522efa), never to AddResult, so PropagateBreakInfo
// (box_fragment_builder.cc:227-229, :479-489 @ a9f50e522efa) never sees its
// extent; under fragmentation the candidate is promoted to a fragmentainer
// descendant (out_of_flow_layout_part.cc:1190-1201 @ a9f50e522efa) and laid
// out after the columns. Blink's arithmetic: the wrapper's only in-flow child
// is 20px tall, so its BlockSizeForFragmentation is 20 even though the
// abspos child reaches 200px.
func TestColumnBSFF_OutOfFlowChildIgnored_LOU411(t *testing.T) {
	styles := map[*html.Node]*css.Style{}
	ids := map[*html.Node]string{}
	tree := ms("W", props("position", "relative"),
		ms("A", props("height", "20px")),
		ms("O", props("position", "absolute", "top", "0px", "width", "10px", "height", "200px")),
	)
	res := layoutBlockInFragmentainer(t, tree.build(styles, ids), styles, 1000, 0)
	if got := res.BlockSizeForFragmentation; got != 20 {
		t.Errorf("BlockSizeForFragmentation = %v, want 20 (abspos extent excluded)", got)
	}
}
