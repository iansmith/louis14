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
