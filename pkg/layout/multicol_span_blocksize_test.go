package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-320: block-size distribution of a definite-height block split by
// column-span:all descendants.
//
// Mirrors css-multicol/multicol-span-all-children-height-004a. A
// `div.container{height:450px}` sits inside an auto-height column-count:2
// multicol and contains [block1, spanner1, block2, spanner2, block3]. The two
// spanners fragment the container into three column-set segments. The
// container's 450px MUST be distributed across the three fragments so each
// holds just enough for its children, the total equalling 450 — NOT
// re-applied in full to each fragment.
//
// Expected layout (from multicol-span-all-children-height-004a-ref.html, which
// renders the distribution as three separate articles):
//
//	segment 1 (block1 200px): container fragment 200px → balanced 2×100px → col-set 100px tall
//	spanner1 50px                                                          → at y=100
//	segment 2 (block2 200px): container fragment 200px → balanced 2×100px → col-set 100px tall
//	spanner2 50px                                                          → at y=250
//	segment 3 (block3 200px): only 50px of container budget LEFT
//	                          (450 − 200 − 200) → col-set 50px tall, block3 clipped to 50px
//
//	total multicol block-size = 100 + 50 + 100 + 50 + 50 = 350px
//
// Bug: louis14 gives the resumed third container fragment its intrinsic content
// size (200px) instead of the remaining declared budget (50px), so segment 3
// renders 100px tall (block3 balanced 2×100px) and the multicol totals 400px.
//
// block_layout.go:1733-1738 — resumed non-override block uses
// `finalBlockSize = intrinsicBlockSize`, ignoring `explicitBlockSize − consumed`.
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

	got := result.Fragment.Size.HeightF64()
	const want = 350.0
	if got != want {
		t.Errorf("multicol block-size = %v, want %v "+
			"(container 450px must distribute 200+200+50 across the spanner splits: "+
			"segments of 100px+100px+50px column rows plus two 50px spanners)", got, want)
	}
}
