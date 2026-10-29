package layout

import (
	"slices"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-320: block-size distribution of a definite-height block split by
// column-span:all descendants.
//
// Mirrors css-multicol/multicol-span-all-children-height-004a. A
// `div.container{height:450px}` sits inside an auto-height column-count:2
// multicol and contains [block1, spanner1, block2, spanner2, block3]. The
// container's 450px is distributed across its fragments (Blink
// FinishFragmentation, fragmentation_utils.cc:606-640 @ a9f50e522efa) while
// the column rows stay content-driven: the last segment has 50px of the
// container left, but block3 (200px) still balances to a 100px row and
// overflows its container. Chrome 155 (headless, getClientRects, LOU-400):
//
//	container fragments: 100,100 | 100,100 | 50,0
//	spanner1 y=100, spanner2 y=250, multicol block-size 400
func TestSpanBlockSize_LOU320_DefiniteHeightContainerSplitBySpanners(t *testing.T) {
	block1 := makeNode("div")
	spanner1 := makeNode("div")
	block2 := makeNode("div")
	spanner2 := makeNode("div")
	block3 := makeNode("div")
	container := makeNode("div", block1, spanner1, block2, spanner2, block3)
	mc := makeNode("div", container)

	blockStyle := func() *css.Style {
		return makeStyle("display", "block", "width", "100px", "height", "200px")
	}
	spanStyle := func() *css.Style {
		return makeStyle("display", "block", "height", "50px", "column-span", "all")
	}

	styles := map[*html.Node]*css.Style{
		mc: makeStyle(
			"display", "block",
			"column-count", "2",
			"column-gap", "0",
		),
		container: makeStyle("display", "block", "height", "450px"),
		block1:    blockStyle(),
		block2:    blockStyle(),
		block3:    blockStyle(),
		spanner1:  spanStyle(),
		spanner2:  spanStyle(),
	}

	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	mcNode := buildTestTree(mc, styles)

	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		Build()

	result := NewMulticolLayoutAlgorithm(ctx, mcNode, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil")
	}

	// Diagnostic: dump the top-level fragment structure so a failure is
	// legible (column-set heights, spanner offsets).
	var spannerTops []float64
	for i, ch := range result.Fragment.Children {
		t.Logf("child[%d] boxType=%v offset.top=%v size=%vx%v",
			i, ch.Fragment.BoxType, ch.Offset.Top.Float64(),
			ch.Fragment.Size.WidthF64(), ch.Fragment.Size.HeightF64())
		if ch.Fragment.BoxType != BoxTypeColumn {
			spannerTops = append(spannerTops, ch.Offset.Top.Float64())
		}
	}

	// Spanner positions are the clearest witness: each must sit BELOW the
	// column-set segment that precedes it. Currently the container's blocks
	// are dropped and both spanners collapse to the top (y=0, y=50).
	if len(spannerTops) != 2 {
		t.Fatalf("expected 2 spanner fragments, got %d", len(spannerTops))
	}
	if spannerTops[0] != 100 {
		t.Errorf("spanner1 at y=%v, want y=100 (after segment 1: block1 balanced to a 100px column row)", spannerTops[0])
	}
	if spannerTops[1] != 250 {
		t.Errorf("spanner2 at y=%v, want y=250 (after segments 1+2 and spanner1: 100+50+100)", spannerTops[1])
	}

	assertColumnFirstChildHeights(t, result, []float64{100, 100, 100, 100, 50, 0})
	if got := result.Fragment.Size.HeightF64(); got != 400 {
		t.Errorf("multicol block-size = %v, want 400 (rows 100+50+100+50+100, Chrome)", got)
	}
}

// assertColumnFirstChildHeights checks the block-size of the first child of
// every column of a multicol result, in column order.
func assertColumnFirstChildHeights(t *testing.T, result *LayoutResult, want []float64) {
	t.Helper()
	var got []float64
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.BoxType == BoxTypeColumn && len(ch.Fragment.Children) > 0 {
			got = append(got, ch.Fragment.Children[0].Fragment.Size.HeightF64())
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("container fragment heights = %v, want %v (Chrome)", got, want)
	}
}

// LOU-325: a MIDDLE segment whose block content OVERFLOWS the container's
// remaining budget must still surface the trailing spanner and the final
// segment.
//
// Mirrors css-multicol/multicol-span-all-children-height-004b. Same shape as
// 004a but the container is height:350px, so its budget is exhausted inside
// the second segment. The container's fragments take what is left of it, the
// column rows stay content-driven (Blink FinishFragmentation,
// fragmentation_utils.cc:606-640 @ a9f50e522efa). Chrome 155 (headless,
// getClientRects, LOU-400):
//
//	container fragments: 100,100 | 100,50 | 0,0
//	spanner1 y=100, spanner2 y=250, multicol block-size 400
func TestSpanBlockSize_LOU325_MiddleSegmentOverflowsBudget(t *testing.T) {
	block1 := makeNode("div")
	spanner1 := makeNode("div")
	block2 := makeNode("div")
	spanner2 := makeNode("div")
	block3 := makeNode("div")
	container := makeNode("div", block1, spanner1, block2, spanner2, block3)
	mc := makeNode("div", container)

	blockStyle := func() *css.Style {
		return makeStyle("display", "block", "width", "100px", "height", "200px")
	}
	spanStyle := func() *css.Style {
		return makeStyle("display", "block", "height", "50px", "column-span", "all")
	}

	styles := map[*html.Node]*css.Style{
		mc: makeStyle(
			"display", "block",
			"column-count", "2",
			"column-gap", "0",
		),
		container: makeStyle("display", "block", "height", "350px"),
		block1:    blockStyle(),
		block2:    blockStyle(),
		block3:    blockStyle(),
		spanner1:  spanStyle(),
		spanner2:  spanStyle(),
	}

	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	mcNode := buildTestTree(mc, styles)

	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		Build()

	result := NewMulticolLayoutAlgorithm(ctx, mcNode, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil")
	}

	var spannerTops []float64
	for i, ch := range result.Fragment.Children {
		t.Logf("child[%d] boxType=%v offset.top=%v size=%vx%v",
			i, ch.Fragment.BoxType, ch.Offset.Top.Float64(),
			ch.Fragment.Size.WidthF64(), ch.Fragment.Size.HeightF64())
		if ch.Fragment.BoxType != BoxTypeColumn {
			spannerTops = append(spannerTops, ch.Offset.Top.Float64())
		}
	}

	// Both spanners must be present — the pre-fix bug dropped spanner2.
	if len(spannerTops) != 2 {
		t.Fatalf("expected 2 spanner fragments, got %d (spanner2 dropped when block2 overflowed the budget)", len(spannerTops))
	}
	if spannerTops[0] != 100 {
		t.Errorf("spanner1 at y=%v, want y=100 (after segment 1: block1 balanced to a 100px column row)", spannerTops[0])
	}
	if spannerTops[1] != 250 {
		t.Errorf("spanner2 at y=%v, want y=250 (100 + 50 + 100, Chrome)", spannerTops[1])
	}
	assertColumnFirstChildHeights(t, result, []float64{100, 100, 100, 50, 0, 0})

	if got := result.Fragment.Size.HeightF64(); got != 400 {
		t.Errorf("multicol block-size = %v, want 400 (rows 100+50+100+50+100, Chrome)", got)
	}
}
