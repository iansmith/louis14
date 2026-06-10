package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// findFragmentByNode walks the fragment tree and returns the first fragment
// whose layout node wraps the given DOM node, along with its size.
func findFragmentByNode(frag *PhysicalFragment, target *html.Node) *PhysicalFragment {
	if frag.LayoutNode != nil && frag.LayoutNode.DOMNode == target {
		return frag
	}
	for _, ch := range frag.Children {
		if found := findFragmentByNode(ch.Fragment, target); found != nil {
			return found
		}
	}
	return nil
}

// TestFlexLayout_RowMaxHeightClampsLineForPercentResolution mirrors WPT
// css-flexbox/flexbox-definite-sizes-004: a row flex container with
// max-height (no explicit height) must clamp its single flex line's
// cross-size BEFORE the §9.8 re-layout pass, so a child's percentage
// max-height resolves against the clamped (definite) size.
//
// Per css-flexbox §9.4 step 8, the single line's cross-size is clamped to
// the container's min/max cross sizes.  Blink applies this by computing
// total_block_size_ (min/max-clamped, flex_layout_algorithm.cc:1266 @
// 4883d11f) before GiveItemsFinalPositionAndSize sets the single line's
// cross size to it (:1741) and re-lays-out items against it.
//
// DOM: flex(row, max-height:100px, align-items:flex-end)
//
//	> block(max-height:100%) > inner(height:9999px)
//
// Buggy behavior: the line cross-size seen by the re-layout pass is the
// unclamped content size (9999), so max-height:100% resolves against 9999
// and block stays 9999 tall.  Expected: container 100 tall, block 100 tall.
func TestFlexLayout_RowMaxHeightClampsLineForPercentResolution(t *testing.T) {
	inner := makeNode("div")
	block := makeNode("div", inner)
	flex := makeNode("div", block)

	styles := map[*html.Node]*css.Style{
		flex:  makeStyle("display", "flex", "width", "100px", "max-height", "100px", "align-items", "flex-end"),
		block: makeStyle("display", "block", "width", "100px", "max-height", "100%"),
		inner: makeStyle("display", "block", "height", "9999px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("flex container height: got %.1f, want 100 (max-height clamp)", got)
	}
	blockFrag := findFragmentByNode(result.Fragment, block)
	if blockFrag == nil {
		t.Fatal("block fragment not found")
	}
	if got := blockFrag.Size.HeightF64(); got != 100 {
		t.Errorf("block height: got %.1f, want 100 (max-height:100%% must resolve against the clamped 100px line cross-size)", got)
	}
}

// TestFlexLayout_OuterMaxHeightStretchMakesItemCrossDefinite mirrors WPT
// css-flexbox/flexbox-definite-sizes-003: the max-height sits on the OUTER
// flex container; the inner flex container is a default align-items:stretch
// item, so it must stretch to the clamped 100px line and present that as a
// definite height to ITS child's max-height:100%.
//
// DOM: outer(row flex, max-height:100px)
//
//	> innerFlex(row flex, align-items:flex-end)
//	  > block(max-height:100%) > inner(height:9999px)
func TestFlexLayout_OuterMaxHeightStretchMakesItemCrossDefinite(t *testing.T) {
	inner := makeNode("div")
	block := makeNode("div", inner)
	innerFlex := makeNode("div", block)
	outer := makeNode("div", innerFlex)

	styles := map[*html.Node]*css.Style{
		outer:     makeStyle("display", "flex", "width", "100px", "max-height", "100px"),
		innerFlex: makeStyle("display", "flex", "width", "100px", "align-items", "flex-end"),
		block:     makeStyle("display", "block", "width", "100px", "max-height", "100%"),
		inner:     makeStyle("display", "block", "height", "9999px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(outer, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("outer flex height: got %.1f, want 100 (max-height clamp)", got)
	}
	innerFrag := findFragmentByNode(result.Fragment, innerFlex)
	if innerFrag == nil {
		t.Fatal("inner flex fragment not found")
	}
	if got := innerFrag.Size.HeightF64(); got != 100 {
		t.Errorf("inner flex height: got %.1f, want 100 (stretch to clamped line)", got)
	}
	blockFrag := findFragmentByNode(result.Fragment, block)
	if blockFrag == nil {
		t.Fatal("block fragment not found")
	}
	if got := blockFrag.Size.HeightF64(); got != 100 {
		t.Errorf("block height: got %.1f, want 100 (max-height:100%% against stretched definite 100px)", got)
	}
}

// TestFlexLayout_RowMinHeightExpandsLineForStretch covers the clamp-UP
// direction of §9.4 step 8: a row flex container with min-height (no
// explicit height) and short content must expand its single line to the
// min-height, and a default align-items:stretch item must stretch to it.
// A flex-start sibling keeps its content height, pinning that only the
// line (not every item) grows.
func TestFlexLayout_RowMinHeightExpandsLineForStretch(t *testing.T) {
	stretchItem := makeNode("div")
	startItem := makeNode("div")
	flex := makeNode("div", stretchItem, startItem)

	styles := map[*html.Node]*css.Style{
		flex:        makeStyle("display", "flex", "width", "100px", "min-height", "100px"),
		stretchItem: makeStyle("display", "block", "width", "50px", "height", "auto"),
		startItem:   makeStyle("display", "block", "width", "50px", "height", "30px", "align-self", "flex-start"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("flex container height: got %.1f, want 100 (min-height expands the line)", got)
	}
	stretchFrag := findFragmentByNode(result.Fragment, stretchItem)
	if stretchFrag == nil {
		t.Fatal("stretch item fragment not found")
	}
	if got := stretchFrag.Size.HeightF64(); got != 100 {
		t.Errorf("stretch item height: got %.1f, want 100 (stretch to min-height-expanded line)", got)
	}
	startFrag := findFragmentByNode(result.Fragment, startItem)
	if startFrag == nil {
		t.Fatal("flex-start item fragment not found")
	}
	if got := startFrag.Size.HeightF64(); got != 30 {
		t.Errorf("flex-start item height: got %.1f, want 30 (non-stretch item keeps content height)", got)
	}
}

// TestFlexLayout_WrapLinesNotClampedByMaxHeight guards that the §9.4 step 8
// clamp applies to single-line (nowrap) containers ONLY: in a wrapping
// container, individual flex lines keep their content-derived cross sizes
// even when the container's max-height clamps the container box itself.
func TestFlexLayout_WrapLinesNotClampedByMaxHeight(t *testing.T) {
	item1 := makeNode("div")
	item2 := makeNode("div")
	flex := makeNode("div", item1, item2)

	styles := map[*html.Node]*css.Style{
		flex:  makeStyle("display", "flex", "flex-wrap", "wrap", "width", "100px", "max-height", "50px"),
		item1: makeStyle("display", "block", "width", "60px", "height", "60px"),
		item2: makeStyle("display", "block", "width", "60px", "height", "60px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	// 60px items don't fit side-by-side in 100px → two lines of 60px each.
	// The container box clamps to max-height:50px; the lines (and items)
	// keep their 60px content sizes and overflow.
	if got := result.Fragment.Size.HeightF64(); got != 50 {
		t.Errorf("flex container height: got %.1f, want 50 (max-height clamps the box)", got)
	}
	for i, n := range []*html.Node{item1, item2} {
		frag := findFragmentByNode(result.Fragment, n)
		if frag == nil {
			t.Fatalf("item%d fragment not found", i+1)
		}
		if got := frag.Size.HeightF64(); got != 60 {
			t.Errorf("item%d height: got %.1f, want 60 (wrapped lines are not clamped)", i+1, got)
		}
	}
}

// TestFlexLayout_RowMaxHeightDoesNotShrinkSmallContent guards the other
// direction: max-height must only CLAMP — a row flex container whose content
// is shorter than max-height keeps its content height, and the line is not
// inflated to the max-height value.  (Blink: total_block_size_ resolves the
// intrinsic size through min/max; it does not substitute the max.)
func TestFlexLayout_RowMaxHeightDoesNotShrinkSmallContent(t *testing.T) {
	item := makeNode("div")
	flex := makeNode("div", item)

	styles := map[*html.Node]*css.Style{
		flex: makeStyle("display", "flex", "width", "100px", "max-height", "100px"),
		item: makeStyle("display", "block", "width", "100px", "height", "30px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 30 {
		t.Errorf("flex container height: got %.1f, want 30 (content below max-height must not grow)", got)
	}
	itemFrag := findFragmentByNode(result.Fragment, item)
	if itemFrag == nil {
		t.Fatal("item fragment not found")
	}
	if got := itemFrag.Size.HeightF64(); got != 30 {
		t.Errorf("item height: got %.1f, want 30", got)
	}
}
