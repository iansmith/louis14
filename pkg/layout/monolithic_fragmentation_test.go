package layout

// LOU-365: monolithic (unbreakable) nodes in block fragmentation.
//
// Blink never fragments scroll containers (overflow scroll/auto/hidden on
// screen media), size-contained boxes, or writing-mode roots — such children
// are laid out unfragmented and moved past the break point as a unit
// (overflowing the fragmentainer if they cannot fit anywhere).
//
// Blink references (Chromium main @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//   - LayoutBox::IsMonolithic            layout_box.cc:3651-3666
//   - HasUnsplittableScrollingOverflow   layout_box.cc:3639-3649
//   - SetupSpaceBuilderForFragmentation  fragmentation_utils.cc:321-345
//     (monolithic non-inline child gets NO fragmentainer size/offset/type)
//   - ShouldAvoidBreakInside             fragmentation_utils.cc:188-196
//     (a monolithic fragment always avoids inside-breaks)

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestLayoutInputNode_IsMonolithic exercises the node-level predicate,
// mirroring LayoutBox::IsMonolithic (layout_box.cc:3651-3666 @ a9f50e522efa).
// The isWritingModeRoot argument stands in for Blink's
// `Parent() && IsWritingModeRoot()` clause: louis14's LayoutInputNode has no
// parent pointer (two-tier collapse), so the caller supplies the comparison.
func TestLayoutInputNode_IsMonolithic(t *testing.T) {
	cases := []struct {
		name              string
		props             []string
		isWritingModeRoot bool
		want              bool
	}{
		{"overflow scroll", []string{"overflow", "scroll"}, false, true},
		{"overflow auto", []string{"overflow", "auto"}, false, true},
		{"overflow hidden", []string{"overflow", "hidden"}, false, true},
		{"overflow-y scroll only", []string{"overflow-y", "scroll"}, false, true},
		{"overflow clip", []string{"overflow", "clip"}, false, false},
		{"overflow visible", []string{"overflow", "visible"}, false, false},
		{"plain block", nil, false, false},
		{"contain size", []string{"contain", "size"}, false, true},
		{"contain strict", []string{"contain", "strict"}, false, true},
		{"contain inline-size", []string{"contain", "inline-size"}, false, false},
		{"writing mode root", nil, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := append([]string{"display", "block"}, tc.props...)
			dom := makeNode("div")
			node := buildTestTree(dom, map[*html.Node]*css.Style{
				dom: makeStyle(props...),
			})
			if got := node.IsMonolithic(tc.isWritingModeRoot); got != tc.want {
				t.Errorf("IsMonolithic(%v) with %v = %v, want %v",
					tc.isWritingModeRoot, tc.props, got, tc.want)
			}
		})
	}
}

// monolithicTestTree builds:
//
//	parent div (block, width 100)
//	  scroll div (overflow per arg, width 50)
//	    inner div (height per arg)
//
// and returns the parent LayoutInputNode.
func monolithicTestTree(t *testing.T, overflow string, innerHeight string) *LayoutInputNode {
	t.Helper()
	inner := makeNode("div")
	scroll := makeNode("div", inner)
	parent := makeNode("div", scroll)
	return buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		scroll: makeStyle("display", "block", "overflow", overflow, "width", "50px"),
		inner:  makeStyle("display", "block", "height", innerHeight),
	})
}

// findScrollFragment returns the first box-typed child fragment one level
// below parent. Nil if none.
func findScrollFragment(parent *PhysicalFragment) *PhysicalFragment {
	for _, link := range parent.Children {
		if link.Fragment != nil && link.Fragment.Type == FragmentBox {
			return link.Fragment
		}
	}
	return nil
}

// TestMonolithic_ScrollContainerNotFragmented: an overflow:scroll block whose
// content is taller than the fragmentainer must come back as a single
// unbroken fragment (its content laid out without fragmentainer constraints),
// tagged IsMonolithic. Mirrors fragmentation_utils.cc:321-345 @ a9f50e522efa:
// a monolithic non-inline child's constraint space gets no fragmentainer
// block-size/offset/type at all.
func TestMonolithic_ScrollContainerNotFragmented(t *testing.T) {
	node := monolithicTestTree(t, "scroll", "200px")
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(50).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		Build()

	result := NewBlockLayoutAlgorithm(testContext(), node, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil")
	}

	scrollFrag := findScrollFragment(result.Fragment)
	if scrollFrag == nil {
		t.Fatal("no scroll-container child fragment placed")
	}
	if got := scrollFrag.Size.HeightF64(); got != 200 {
		t.Errorf("scroll container fragment height = %v, want 200 (unfragmented)", got)
	}
	if !scrollFrag.IsMonolithic {
		t.Error("scroll container fragment must be tagged IsMonolithic")
	}
	// The inner 200px block must also be whole (no slicing inside the
	// scroll container).
	if innerFrag := findScrollFragment(scrollFrag); innerFrag == nil {
		t.Error("scroll container lost its inner child")
	} else if got := innerFrag.Size.HeightF64(); got != 200 {
		t.Errorf("inner content fragment height = %v, want 200", got)
	}
}

// TestMonolithic_PushedToNextFragmentainerWhenUnfit: a monolithic child that
// does not fit in the remaining fragmentainer space — but had a preceding
// sibling (container separation) — must be pushed whole to the next
// fragmentainer via a break-before, never sliced. Mirrors
// fragmentation_utils.cc:188-196 (monolithic fragments avoid inside-breaks)
// + the MovePastBreakpoint push, @ a9f50e522efa.
func TestMonolithic_PushedToNextFragmentainerWhenUnfit(t *testing.T) {
	first := makeNode("div")
	inner := makeNode("div")
	scroll := makeNode("div", inner)
	parent := makeNode("div", first, scroll)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		first:  makeStyle("display", "block", "height", "60px"),
		scroll: makeStyle("display", "block", "overflow", "auto", "width", "50px"),
		inner:  makeStyle("display", "block", "height", "80px"),
	})

	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(100).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		Build()

	result := NewBlockLayoutAlgorithm(testContext(), node, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	if result.BreakToken == nil {
		t.Fatal("expected a break token pushing the monolithic child to the next fragmentainer")
	}
	if len(result.BreakToken.ChildBreakTokens) != 1 {
		t.Fatalf("child break tokens = %d, want 1", len(result.BreakToken.ChildBreakTokens))
	}
	childToken := result.BreakToken.ChildBreakTokens[0]
	if !childToken.IsBreakBefore {
		t.Error("monolithic child must break BEFORE (pushed whole), not be sliced in-progress")
	}
	// Only the 60px first child may be placed in this fragmentainer.
	if got := len(result.Fragment.Children); got != 1 {
		t.Errorf("fragments placed = %d, want 1 (monolithic child pushed out)", got)
	}
}

// TestMonolithic_BalancingFloor: during the initial column-balancing pass, a
// monolithic child's full block-size must reach the balancer as an
// unbreakable floor (TallestUnbreakableBlockSize), so columns are sized tall
// enough to hold it whole. Mirrors the parent-side floor propagation in
// Blink's BreakBeforeChildIfNeeded via ShouldAvoidBreakInside
// (fragmentation_utils.cc:188-196 @ a9f50e522efa).
func TestMonolithic_BalancingFloor(t *testing.T) {
	node := monolithicTestTree(t, "scroll", "200px")
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(Indefinite).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		SetIsInitialColumnBalancingPass(true).
		Build()

	result := NewBlockLayoutAlgorithm(testContext(), node, space).Layout()
	if result == nil {
		t.Fatal("layout returned nil")
	}
	if got := result.TallestUnbreakableBlockSize; got < 200 {
		t.Errorf("TallestUnbreakableBlockSize = %v, want >= 200 (monolithic floor)", got)
	}
}
