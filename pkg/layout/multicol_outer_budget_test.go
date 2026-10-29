package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestMulticolNested_LineBudgetIgnoresBlockEndBorder (LOU-391): a nested
// multicol's line budget is FragmentainerSpaceLeftForChildren() − line_offset
// (column_layout_algorithm.cc:819-829 @ a9f50e522efa). With
// box-decoration-break:slice nothing is reserved for the block-end
// border/padding (fragmentation_utils.h:187-257): it belongs to the last
// fragment only. Geometry from Chrome 155 (scratchpad probe w1a): a 100px
// outer column holds an inner fragment 100 tall whose two columns are 100
// tall (five 20px blocks each), not 80.
func TestMulticolNested_LineBudgetIgnoresBlockEndBorder(t *testing.T) {
	var blocks []*html.Node
	styles := map[*html.Node]*css.Style{}
	for i := 0; i < 14; i++ {
		b := makeNode("div")
		styles[b] = makeStyle("display", "block", "height", "20px")
		blocks = append(blocks, b)
	}
	innerMC := makeNode("div", blocks...)
	outerMC := makeNode("div", innerMC)
	styles[outerMC] = makeStyle("display", "block", "width", "200px", "height", "100px",
		"column-count", "2", "column-gap", "0", "column-fill", "auto")
	styles[innerMC] = makeStyle("display", "block", "column-count", "2", "column-gap", "0",
		"border-bottom-width", "20px", "border-bottom-style", "solid")

	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		Build()
	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil || len(result.Fragment.Children) == 0 {
		t.Fatal("outer mc layout returned no columns")
	}
	dumpFragmentTree(t, "", result.Fragment)
	inner := childLinkFor(result.Fragment.Children[0].Fragment, innerMC)
	if inner == nil {
		t.Fatal("outer col 1 has no inner mc fragment")
	}
	if got := NewLogicalFragment(wdm, inner.Fragment).BlockSize(); got != 100 {
		t.Errorf("first inner fragment block-size = %v, want 100", got)
	}
	for i, col := range inner.Fragment.Children {
		if got := NewLogicalFragment(wdm, col.Fragment).BlockSize(); got != 100 {
			t.Errorf("first inner fragment column %d block-size = %v, want 100", i, got)
		}
	}
}

// TestMulticolNested_ZeroSpaceLeftIsALimit (LOU-391): with no space left in
// the outer fragmentainer the available outer space is 0, a real limit
// (ConstrainColumnBlockSize clamps to available_outer_space.ClampNegativeToZero(),
// column_layout_algorithm.cc:1743-1746 @ a9f50e522efa), not "unconstrained".
// Columns still accept 1px of content each (ClampedToValidFragmentainerCapacity,
// fragmentation_utils.h:156-173), so the inner multicol places some content
// and reports a break token for the rest — it must not consume everything.
func TestMulticolNested_ZeroSpaceLeftIsALimit(t *testing.T) {
	var blocks []*html.Node
	styles := map[*html.Node]*css.Style{}
	for i := 0; i < 4; i++ {
		b := makeNode("div")
		styles[b] = makeStyle("display", "block", "height", "20px")
		blocks = append(blocks, b)
	}
	innerMC := makeNode("div", blocks...)
	styles[innerMC] = makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto")

	result := layoutMulticolInFragmentainer(t, innerMC, styles, 100, 100)
	if result.BreakToken == nil {
		t.Errorf("inner mc at 0 space left completed with no break token; want a break token for unplaced content")
	}
}

// TestMulticolNested_SpannerFragmentainerOffsetIncludesBlockStartEdge
// (LOU-391): a spanner's fragmentainer offset is
// FragmentainerOffsetForChildren() + block_offset, with block_offset measured
// from the multicol's border-box edge (column_layout_algorithm.cc:1354-1356,
// :1402-1403 @ a9f50e522efa). With 30px of block-start padding in a 100px
// outer fragmentainer, the spanner starts at 30, so of its two 40px blocks
// only the first fits and the spanner breaks inside.
func TestMulticolNested_SpannerFragmentainerOffsetIncludesBlockStartEdge(t *testing.T) {
	first := makeNode("div")
	second := makeNode("div")
	spanner := makeNode("div", first, second)
	innerMC := makeNode("div", spanner)
	block := makeStyle("display", "block", "height", "40px")
	styles := map[*html.Node]*css.Style{
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0", "padding-top", "30px"),
		spanner: makeStyle("display", "block", "column-span", "all"),
		first:   block,
		second:  block,
	}
	result := layoutMulticolInFragmentainer(t, innerMC, styles, 100, 0)
	if result.BreakToken == nil {
		t.Errorf("inner mc completed; want a break inside the spanner (its second block ends at 110 > 100)")
	}
}

// TestMulticolFragmentainerSpaceForChildren_Clone (LOU-391): cloned
// decorations shrink the capacity by both block edges and shift the
// children's offset by the block-start edge
// (fragmentation_utils.h:187-226 @ a9f50e522efa); slice leaves both alone.
func TestMulticolFragmentainerSpaceForChildren_Clone(t *testing.T) {
	for _, tc := range []struct {
		cloned                   bool
		wantCapacity, wantOffset float64
		wantSpaceLeftForChildren float64
	}{
		{cloned: false, wantCapacity: 100, wantOffset: 20, wantSpaceLeftForChildren: 80},
		{cloned: true, wantCapacity: 85, wantOffset: 10, wantSpaceLeftForChildren: 75},
	} {
		mla := &MulticolLayoutAlgorithm{
			space: ConstraintSpace{
				HasBlockFragmentation:  true,
				FragmentainerBlockSize: 100,
				FragmentainerOffset:    20,
			},
			borderScrollbarPadding:  LogicalEdges{BlockStart: 10, BlockEnd: 5},
			hasClonedBoxDecorations: tc.cloned,
		}
		if got := mla.fragmentainerCapacityForChildren(); got != tc.wantCapacity {
			t.Errorf("cloned=%v: capacity = %v, want %v", tc.cloned, got, tc.wantCapacity)
		}
		if got := mla.fragmentainerOffsetForChildren(); got != tc.wantOffset {
			t.Errorf("cloned=%v: offset = %v, want %v", tc.cloned, got, tc.wantOffset)
		}
		if got := mla.fragmentainerSpaceLeftForChildren(); got != tc.wantSpaceLeftForChildren {
			t.Errorf("cloned=%v: space left = %v, want %v", tc.cloned, got, tc.wantSpaceLeftForChildren)
		}
	}
}

// TestMulticolNested_OverflowRegrowsLineAndParentPushesIt (LOU-388 WI-3):
// when a column of a nested multicol overflows past the outer fragmentainer
// (monolithic content taller than the line), the line is laid out again with
// columns tall enough to encompass the overflow (LayoutLine
// minimum_column_block_size, column_layout_algorithm.cc:1027-1066 @
// a9f50e522efa), and the parent's MovePastBreakpoint then pushes the taller
// multicol whole to the next outer column. multicol-nested-024; Chrome 155
// (probe p024): the inner multicol starts in outer column 2, 150 tall, with
// one 100px contain:size block per 25px inner column.
func TestMulticolNested_OverflowRegrowsLineAndParentPushesIt(t *testing.T) {
	first := makeNode("div")
	b1, b2, b3 := makeNode("div"), makeNode("div"), makeNode("div")
	innerMC := makeNode("div", b1, b2, b3)
	outerMC := makeNode("div", first, innerMC)
	mono := makeStyle("display", "block", "height", "100px", "contain", "size")
	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle("display", "block", "width", "100px", "height", "150px",
			"column-count", "2", "column-gap", "0", "column-fill", "auto"),
		first:   makeStyle("display", "block", "height", "100px"),
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0"),
		b1:      mono, b2: mono, b3: mono,
	}
	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()
	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil || len(result.Fragment.Children) < 2 {
		t.Fatal("outer mc layout returned fewer than 2 columns")
	}
	dumpFragmentTree(t, "", result.Fragment)
	if l := childLinkFor(result.Fragment.Children[0].Fragment, innerMC); l != nil {
		t.Errorf("outer col 1 holds an inner mc fragment (h=%v); want it pushed to col 2",
			NewLogicalFragment(wdm, l.Fragment).BlockSize())
	}
	inner := childLinkFor(result.Fragment.Children[1].Fragment, innerMC)
	if inner == nil {
		t.Fatal("outer col 2 has no inner mc fragment")
	}
	if got := inner.Offset.Top.Float64(); got != 0 {
		t.Errorf("inner mc in outer col 2 at y=%v, want 0", got)
	}
	if got := NewLogicalFragment(wdm, inner.Fragment).BlockSize(); got != 150 {
		t.Errorf("inner mc in outer col 2: block-size = %v, want 150", got)
	}
}

// TestMulticolNested_OverflowInBalancedColumnsPropagatesShortage (LOU-393):
// inside an outer balanced multicol (not its initial pass), a nested line
// whose column overflows the outer fragmentainer reports the column's space
// shortage so the outer multicol can stretch (column_layout_algorithm.cc:
// 1043-1049 @ a9f50e522efa).
func TestMulticolNested_OverflowInBalancedColumnsPropagatesShortage(t *testing.T) {
	mono := makeNode("div")
	innerMC := makeNode("div", mono)
	styles := map[*html.Node]*css.Style{
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto"),
		mono:    makeStyle("display", "block", "height", "100px", "contain", "size"),
	}
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(60).
		SetFragmentainerOffset(20).
		SetBlockFragmentationType(FragmentColumn).
		SetIsInsideBalancedColumns(true).
		Build()
	result := NewMulticolLayoutAlgorithm(ctx, buildTestTree(innerMC, styles), space).Layout()
	if result == nil || result.MinSpaceShortage <= 0 {
		t.Fatalf("inner mc MinSpaceShortage = %v, want > 0 (100px monolith in a 40px line)", result.MinSpaceShortage)
	}
}

// TestMulticolNested_SpannerAtOuterEndIsPushed (LOU-388 WI-4): a spanner of a
// nested multicol goes through BreakBeforeChildIfNeeded
// (column_layout_algorithm.cc:1398-1411 @ a9f50e522efa); one that starts at
// the outer fragmentainer end is broken before and placed whole at the top of
// the next outer column, and content after it is not laid out in the
// exhausted outer column. multicol-span-all-children-height-007; Chrome 155
// (probe p007): inner fragments 110, 110, 70 tall; the spanner at the top of
// the second; block2's first row 60 tall below it.
func TestMulticolNested_SpannerAtOuterEndIsPushed(t *testing.T) {
	block1 := makeNode("div")
	spanner := makeNode("div")
	block2 := makeNode("div")
	innerMC := makeNode("div", block1, spanner, block2)
	outerMC := makeNode("div", innerMC)
	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle("display", "block", "width", "400px", "height", "110px",
			"column-count", "2", "column-gap", "16px"),
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "16px", "height", "270px",
			"border-top-width", "10px", "border-top-style", "solid",
			"border-bottom-width", "10px", "border-bottom-style", "solid",
			"border-left-width", "10px", "border-left-style", "solid",
			"border-right-width", "10px", "border-right-style", "solid"),
		block1:  makeStyle("display", "block", "height", "200px"),
		spanner: makeStyle("display", "block", "height", "50px", "column-span", "all"),
		block2:  makeStyle("display", "block", "height", "240px"),
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
	var heights []float64
	var frags []*PhysicalFragment
	for _, col := range result.Fragment.Children {
		if l := childLinkFor(col.Fragment, innerMC); l != nil {
			heights = append(heights, NewLogicalFragment(wdm, l.Fragment).BlockSize())
			frags = append(frags, l.Fragment)
		}
	}
	if len(heights) != 3 || heights[0] != 110 || heights[1] != 110 || heights[2] != 70 {
		t.Fatalf("inner mc fragment heights = %v, want [110 110 70]", heights)
	}
	if l := childLinkFor(frags[0], spanner); l != nil {
		t.Errorf("spanner placed in the first inner fragment at y=%v; want it pushed", l.Offset.Top.Float64())
	}
	l := childLinkFor(frags[1], spanner)
	if l == nil {
		t.Fatal("second inner fragment has no spanner")
	}
	if got := l.Offset.Top.Float64(); got != 0 {
		t.Errorf("spanner in the second inner fragment at y=%v, want 0", got)
	}
}

// TestMulticolNested_LineThatDoesNotFitIsBrokenBefore (LOU-388 WI-4): after
// a spanner, a nested multicol's column line that does not fit in the outer
// fragmentainer is broken before as a whole (LayoutLine → MovePastBreakpoint →
// nullptr, column_layout_algorithm.cc:1194-1213 @ a9f50e522efa), so it starts
// the next outer column. multicol-nested-017; Chrome 155 (scratchpad probe
// p017): inner fragments 50 then 100 tall; the spanner is in the first.
func TestMulticolNested_LineThatDoesNotFitIsBrokenBefore(t *testing.T) {
	filler := makeNode("div")
	a := makeNode("div")
	sp := makeNode("div")
	line := makeNode("div", makeTextNode("x"))
	last := makeNode("div")
	box := makeNode("div", sp, line, last)
	innerMC := makeNode("div", a, box)
	outerMC := makeNode("div", filler, innerMC)
	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle("display", "block", "width", "100px", "height", "110px",
			"column-count", "2", "column-gap", "0", "column-fill", "auto", "line-height", "50px"),
		filler:  makeStyle("display", "block", "height", "60px"),
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto", "line-height", "50px"),
		a:       makeStyle("display", "block", "height", "40px"),
		box:     makeStyle("display", "block", "height", "100px", "line-height", "50px"),
		sp:      makeStyle("display", "block", "height", "20px", "column-span", "all"),
		line:    makeStyle("display", "block", "width", "200%", "line-height", "50px"),
		last:    makeStyle("display", "block", "width", "200%", "height", "50px"),
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
	var heights []float64
	var frags []*PhysicalFragment
	for _, col := range result.Fragment.Children {
		if l := childLinkFor(col.Fragment, innerMC); l != nil {
			heights = append(heights, NewLogicalFragment(wdm, l.Fragment).BlockSize())
			frags = append(frags, l.Fragment)
		}
	}
	if len(heights) != 2 || heights[0] != 50 || heights[1] != 100 {
		t.Fatalf("inner mc fragment heights = %v, want [50 100]", heights)
	}
	if childLinkFor(frags[0], sp) == nil {
		t.Errorf("first inner fragment has no spanner")
	}
}

// TestMulticolNested_ParallelFlowSpannerDoesNotStopLayout (LOU-394): a
// spanner whose box fits while its content overflows continues in a parallel
// flow — not an in-flow break inside — so layout continues after it in the
// same outer fragmentainer (column_layout_algorithm.cc:656-660 @
// a9f50e522efa). spanner-fragmentation-004's structure: the second spanner
// is placed in the first outer column, right after the first.
func TestMulticolNested_ParallelFlowSpannerDoesNotStopLayout(t *testing.T) {
	c1, c2, c3 := makeNode("div"), makeNode("div"), makeNode("div")
	sp1 := makeNode("div", c1, c2, c3)
	sp2 := makeNode("div")
	after := makeNode("div")
	innerMC := makeNode("div", sp1, sp2, after)
	outerMC := makeNode("div", innerMC)
	styles := map[*html.Node]*css.Style{
		outerMC: makeStyle("display", "block", "width", "100px", "height", "100px",
			"column-count", "2", "column-gap", "0", "column-fill", "auto"),
		innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0"),
		sp1:     makeStyle("display", "block", "height", "50px", "column-span", "all"),
		c1:      makeStyle("display", "block", "height", "50px"),
		c2:      makeStyle("display", "block", "height", "50px"),
		c3:      makeStyle("display", "block", "height", "100px"),
		sp2:     makeStyle("display", "block", "height", "10px", "column-span", "all"),
		after:   makeStyle("display", "block", "height", "80px"),
	}
	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()
	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil || len(result.Fragment.Children) == 0 {
		t.Fatal("outer mc layout returned no columns")
	}
	inner := childLinkFor(result.Fragment.Children[0].Fragment, innerMC)
	if inner == nil {
		t.Fatal("outer col 1 has no inner mc fragment")
	}
	l := childLinkFor(inner.Fragment, sp2)
	if l == nil {
		t.Fatal("second spanner not placed in outer col 1; layout stopped after the parallel-flow spanner")
	}
	if got := l.Offset.Top.Float64(); got != 50 {
		t.Errorf("second spanner at y=%v, want 50", got)
	}
}

// innerFragmentHeights lays out outerMC and returns the block-sizes of
// innerMC's fragments in outer-column order, plus the block offset of node
// `after` in the first outer column that holds it (-1 if none).
func innerFragmentHeights(t *testing.T, outerMC, innerMC, after *html.Node, styles map[*html.Node]*css.Style, width float64) ([]float64, float64) {
	t.Helper()
	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: width, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: width, BlockSize: Indefinite}).
		Build()
	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil {
		t.Fatal("outer mc layout returned nil")
	}
	dumpFragmentTree(t, "", result.Fragment)
	var heights []float64
	afterOffset := -1.0
	for _, col := range result.Fragment.Children {
		if l := childLinkFor(col.Fragment, innerMC); l != nil {
			heights = append(heights, NewLogicalFragment(wdm, l.Fragment).BlockSize())
		}
		if l := childLinkFor(col.Fragment, after); l != nil && afterOffset < 0 {
			afterOffset = l.Offset.Top.Float64()
		}
	}
	return heights, afterOffset
}

// TestMulticolNested_MinHeightAppliesPerFragment (LOU-388 WI-6b): a nested
// multicol's fragment block-size comes from ComputeBlockSizeForFragment
// (height, min-height, max-height over all fragments) and FinishFragmentation
// (column_layout_algorithm.cc:376-393 @ a9f50e522efa). Chrome 155 (scratchpad
// probes p027/p028): multicol-nested-027's inner multicol with min-height:75
// and 50px of content is 75 tall, its sibling at y=75; multicol-nested-028's
// with min-height:125 spans fragments of 100 and 25.
func TestMulticolNested_MinHeightAppliesPerFragment(t *testing.T) {
	t.Run("nested-027", func(t *testing.T) {
		content := makeNode("div")
		innerMC := makeNode("div", content)
		sib := makeNode("div")
		avoid := makeNode("div")
		outerMC := makeNode("div", innerMC, sib, avoid)
		styles := map[*html.Node]*css.Style{
			outerMC: makeStyle("display", "block", "width", "100px", "height", "100px", "column-count", "2", "column-gap", "0", "column-fill", "auto"),
			innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto", "min-height", "75px"),
			content: makeStyle("display", "block", "height", "50px"),
			sib:     makeStyle("display", "block", "height", "25px"),
			avoid:   makeStyle("display", "block", "height", "100px", "break-inside", "avoid"),
		}
		heights, sibOffset := innerFragmentHeights(t, outerMC, innerMC, sib, styles, 100)
		if len(heights) != 1 || heights[0] != 75 {
			t.Errorf("inner fragment heights = %v, want [75]", heights)
		}
		if sibOffset != 75 {
			t.Errorf("sibling at y=%v, want 75", sibOffset)
		}
	})
	t.Run("nested-028", func(t *testing.T) {
		content := makeNode("div")
		innerMC := makeNode("div", content)
		a1 := makeNode("div")
		a2 := makeNode("div")
		outerMC := makeNode("div", innerMC, a1, a2)
		styles := map[*html.Node]*css.Style{
			outerMC: makeStyle("display", "block", "width", "100px", "height", "100px", "column-count", "4", "column-gap", "0", "column-fill", "auto"),
			innerMC: makeStyle("display", "block", "column-count", "2", "column-gap", "0", "column-fill", "auto", "min-height", "125px"),
			content: makeStyle("display", "block", "height", "205px"),
			a1:      makeStyle("display", "block", "height", "75px", "break-inside", "avoid"),
			a2:      makeStyle("display", "block", "height", "200px", "break-inside", "avoid"),
		}
		heights, a1Offset := innerFragmentHeights(t, outerMC, innerMC, a1, styles, 100)
		if len(heights) != 2 || heights[0] != 100 || heights[1] != 25 {
			t.Errorf("inner fragment heights = %v, want [100 25]", heights)
		}
		if a1Offset != 25 {
			t.Errorf("first avoid block at y=%v, want 25", a1Offset)
		}
	})
}
