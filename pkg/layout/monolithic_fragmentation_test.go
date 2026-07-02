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
	// 217 = 200px content + 17px reserved horizontal scrollbar
	// (overflow:scroll on both axes; classic scrollbar width, see
	// fragment_geometry.go). The load-bearing part is that it is NOT
	// sliced at the 50px fragmentainer boundary.
	if got := scrollFrag.Size.HeightF64(); got != 217 {
		t.Errorf("scroll container fragment height = %v, want 217 (unfragmented)", got)
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

// TestMonolithic_BalancingFloorExcludesOffset: the unbreakable floor of a
// child that starts partway down the fragmentainer is the child's own
// block-size, NOT offset+size — a break before the child can always place
// it at the top of a fresh column, so the column only needs to be as tall
// as the child itself. Blink's CalculateUnbreakableBlockSize
// (fragmentation_utils.cc:979-993 @ a9f50e522efa) adds
// fragmentainer_block_offset only when it is NEGATIVE (content straddling
// in from the previous fragmentainer). An offset+size floor over-grows
// balanced columns and swallows the break entirely
// (multicol-overflow-clip-auto-sized regression).
func TestMonolithic_BalancingFloorExcludesOffset(t *testing.T) {
	first := makeNode("div")
	inner := makeNode("div")
	scroll := makeNode("div", inner)
	parent := makeNode("div", first, scroll)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		first:  makeStyle("display", "block", "height", "30px"),
		scroll: makeStyle("display", "block", "overflow", "auto", "width", "50px"),
		inner:  makeStyle("display", "block", "height", "200px"),
	})

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
	if got := result.TallestUnbreakableBlockSize; got != 200 {
		t.Errorf("TallestUnbreakableBlockSize = %v, want exactly 200 (child size only, offset excluded)", got)
	}
}

// TestAvoidInside_BalancingFloorUsesFullBlockSize: a break-inside:avoid
// child's unbreakable floor is its full border-box block-size. Louis14's
// CalculateUnbreakableBlockSize used to short-circuit on the child result's
// own TallestUnbreakableBlockSize — but every box self-propagates its
// border+padding as floors during the balancing pass (block_layout.go
// SetupFragmentation port), so an avoid box with any padding reported just
// its padding (here 2px) as the floor instead of its full size. Blink's
// CalculateUnbreakableBlockSize (fragmentation_utils.cc:979-993 @
// a9f50e522efa) has no such short-circuit: it uses
// BlockSizeForFragmentation(result) — the fragment's border-box size when
// no explicit fragmentation size was computed (:1545-1583). Deeper
// descendant floors still flow up via the separate carrier propagation
// (block_layout.go, Blink box_fragment_builder.cc:566-569), and
// PropagateTallestUnbreakableBlockSize keeps the max.
func TestAvoidInside_BalancingFloorUsesFullBlockSize(t *testing.T) {
	inner := makeNode("div")
	avoid := makeNode("div", inner)
	parent := makeNode("div", avoid)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		avoid: makeStyle("display", "block", "break-inside", "avoid",
			"padding-top", "2px", "padding-bottom", "2px"),
		inner: makeStyle("display", "block", "height", "60px"),
	})

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
	// 64 = 2px padding + 60px content + 2px padding.
	if got := result.TallestUnbreakableBlockSize; got != 64 {
		t.Errorf("TallestUnbreakableBlockSize = %v, want 64 (full border-box size of the avoid child)", got)
	}
}

// TestAvoidInside_ChildInternalShortagePropagates: when a break-avoidance
// child breaks inside (is sliced at the fragmentainer boundary), the space
// shortage it reported internally must surface on the parent's result so
// the multicol stretch loop can grow the columns. Blink's
// CalculateSpaceShortage: "if space shortage was reported inside the
// child, use that" (fragmentation_utils.cc:1027-1049 @ a9f50e522efa;
// propagated via PropagateSpaceShortage :995-1015). Louis14's
// frag-overflow branch used to compute only its own blockCursor−fragEnd
// (zero when the child's slice ends exactly at the boundary), starving the
// stretch loop — the break-inside:avoid half of
// multicol-overflow-clip-auto-sized.
//
// Mirrors the driver's structure: the avoid box holds two TEXT lines (via
// <br>), so its own inline layout records the line-level shortage on its
// result; the column-level walk must lift it. Exact metrics depend on the
// font, so the assertions are positivity of the shortage plus the avoid
// violation — the exact-pixel bar is held by the reftest.
func TestAvoidInside_ChildInternalShortagePropagates(t *testing.T) {
	first := makeNode("div")
	t1 := &html.Node{Type: html.TextNode, Text: "line one"}
	br := makeNode("br")
	t2 := &html.Node{Type: html.TextNode, Text: "line two"}
	avoid := makeNode("div", t1, br, t2)
	parent := makeNode("div", first, avoid)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "200px"),
		first:  makeStyle("display", "block", "height", "20px"),
		avoid:  makeStyle("display", "block", "break-inside", "avoid"),
		br:     makeStyle("display", "inline"),
	})

	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	// Fragmentainer 40: the 20px sibling leaves 20px — enough for the
	// avoid box's first text line but not the second, so the avoid box is
	// sliced and reports its internal line shortage.
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(40).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		Build()

	result := NewBlockLayoutAlgorithm(testContext(), node, space).Layout()
	if result == nil {
		t.Fatal("layout returned nil")
	}
	if result.BreakToken == nil {
		t.Fatal("expected the avoid box to break (fragmentainer too short for both lines)")
	}
	if result.BreakAppeal >= BreakAppealPerfect {
		t.Errorf("BreakAppeal = %v, want < Perfect (avoid violated by slicing)", result.BreakAppeal)
	}
	if got := result.MinSpaceShortage; got <= 0 {
		t.Errorf("MinSpaceShortage = %v, want > 0 (avoid child's internal line shortage)", got)
	}
}

// TestMonolithicFloat_BalancingFloorIncludesMargins: an unbreakable
// (contain:size, hence monolithic) block-level float floors the balanced
// column block-size at its MARGIN box — float margins do not break or
// truncate. Blink's BlockSizeForFragmentation adds a float's block margins
// (fragmentation_utils.cc:1545-1583, "Margins on floats do not break or
// truncate", @ a9f50e522efa); the floor propagates via
// BreakBeforeChildIfNeeded (:1105-1113), which louis14's block-path floats
// bypass (layoutFloat), so layoutFloat must propagate it directly.
// Driver: multicol-fill-balance-037 (float h20 + margin 40px 0 → the
// 2-column balance must not split the 100px margin box in half).
func TestMonolithicFloat_BalancingFloorIncludesMargins(t *testing.T) {
	float := makeNode("div")
	parent := makeNode("div", float)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		float: makeStyle("display", "block", "float", "left", "width", "100%",
			"contain", "size", "height", "20px",
			"margin-top", "40px", "margin-bottom", "40px"),
	})

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
	// 100 = 40 margin-top + 20 border box + 40 margin-bottom.
	if got := result.TallestUnbreakableBlockSize; got != 100 {
		t.Errorf("TallestUnbreakableBlockSize = %v, want 100 (float margin box)", got)
	}
}

// TestParallelFlow_CompletedParentCarriesChildContinuation: a fixed-size
// child whose own content overflows into later fragmentainers (a parallel
// flow: the child box is at block-end, its content continues) must have its
// continuation carried on the parent's outgoing break token even when the
// parent itself completes comfortably inside the fragmentainer. The walk
// used to drop such tokens: only the fragmentainer-boundary branches
// carried child break tokens, so a parent ending at blockCursor < fragEnd
// lost the parallel flow entirely (fixed-size-child-with-overflow: green
// content rendered in column 1 only). Mirrors Blink's FinishFragmentation,
// which builds the outgoing token from ALL of the builder's child break
// tokens (fragmentation_utils.cc:677-815 @ a9f50e522efa) — a completed
// parent with an unfinished child emits a token with the child continuation
// rather than none.
//
// Geometry: wrapper (auto) > mid (height:50) > inner (height:400);
// fragmentainer 100. Mid finishes at 50 (at block-end) with inner having
// consumed 100; the wrapper must break with mid's token chained.
func TestParallelFlow_CompletedParentCarriesChildContinuation(t *testing.T) {
	inner := makeNode("div")
	mid := makeNode("div", inner)
	parent := makeNode("div", mid)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		mid:    makeStyle("display", "block", "height", "50px"),
		inner:  makeStyle("display", "block", "height", "400px"),
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
	if result == nil {
		t.Fatal("layout returned nil")
	}
	if result.BreakToken == nil {
		t.Fatal("expected a break token carrying the parallel-flow continuation")
	}
	if !result.BreakToken.HasSeenAllChildren {
		t.Error("HasSeenAllChildren must be true (the walk completed)")
	}
	if got := len(result.BreakToken.ChildBreakTokens); got != 1 {
		t.Fatalf("child break tokens = %d, want 1 (mid's continuation)", got)
	}
	midToken := result.BreakToken.ChildBreakTokens[0]
	if !midToken.IsAtBlockEnd {
		t.Error("mid's token must be at-block-end (its box is done; content continues)")
	}
	if got := len(midToken.ChildBreakTokens); got != 1 {
		t.Fatalf("mid child tokens = %d, want 1 (inner's continuation)", got)
	}
	if got := midToken.ChildBreakTokens[0].ConsumedBlockSize.Float64(); got != 100 {
		t.Errorf("inner consumed = %v, want 100 (one fragmentainer's worth)", got)
	}
}

// TestMonolithic_LeafNeverGetsResumeToken: a monolithic leaf child taller
// than the fragmentainer must be moved past the break point AS A UNIT —
// the parent must never emit a consumed-progress resume token for it.
// Blink: "If it's too tall to fit, it will either overflow the
// fragmentainer or get brutally sliced ... depending on fragmentation type
// (multicol vs. printing)" — for multicol it overflows, never resumes
// (fragmentation_utils.cc:329-339 @ a9f50e522efa). Louis14's parent-side
// LEAF-slicing dispatch used to emit `{Node: child, ConsumedBlockSize: N}`
// tokens for monolithic leaves; since the space gate also (correctly)
// stops forwarding resume tokens into monolithic children, the resumed
// child re-rendered at full height every time and the token never
// finished — the multicol-nested-011 infinite row loop (30s watchdog
// hang).
//
// Geometry mirrors nested-011's inner wrapper: fragmentainer 50 with
// IsBlockSizeOverride (a column slot), monolithic contain:size leaf h100.
func TestMonolithic_LeafNeverGetsResumeToken(t *testing.T) {
	leaf := makeNode("div")
	parent := makeNode("div", leaf)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		leaf: makeStyle("display", "block", "contain", "size",
			"width", "100px", "height", "100px"),
	})

	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 50}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: 50}).
		SetIsFixedBlockSize(true).
		SetIsBlockSizeOverride(true).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(50).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		Build()

	result := NewBlockLayoutAlgorithm(testContext(), node, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	// The monolithic leaf must be placed whole (100, overflowing the 50px
	// fragmentainer) ...
	leafFrag := findScrollFragment(result.Fragment)
	if leafFrag == nil {
		t.Fatal("monolithic leaf fragment not placed")
	}
	if got := leafFrag.Size.HeightF64(); got != 100 {
		t.Errorf("leaf fragment height = %v, want 100 (moved past the breakpoint as a unit)", got)
	}
	// ... and must NOT appear in the outgoing break token as an in-progress
	// resume (that token can never finish: the gate correctly refuses to
	// slice the child on resume, so consumed progress never reaches the
	// leaf's declared size — an infinite fragmentainer loop).
	if result.BreakToken != nil {
		for _, ct := range result.BreakToken.ChildBreakTokens {
			if ct.Node == node.Children()[0] && !ct.IsBreakBefore {
				t.Errorf("outgoing token carries an in-progress resume for the monolithic leaf (consumed=%v)",
					ct.ConsumedBlockSize.Float64())
			}
		}
	}
}

// --- CodeRabbit review findings on PR #163 (LOU-365) ---

// TestMinPositiveSpaceShortage_Table: contract of the min-merge helper —
// non-positive means "no shortage" and the result is never negative
// (CodeRabbit 3514362242: MinPositiveSpaceShortage(0, -1) returned -1).
func TestMinPositiveSpaceShortage_Table(t *testing.T) {
	cases := []struct{ a, b, want float64 }{
		{0, 0, 0},
		{0, -1, 0},
		{-2, -1, 0},
		{0, 5, 5},
		{5, 0, 5},
		{-1, 5, 5},
		{5, -1, 5},
		{3, 7, 3},
		{7, 3, 3},
	}
	for _, tc := range cases {
		if got := MinPositiveSpaceShortage(tc.a, tc.b); got != tc.want {
			t.Errorf("MinPositiveSpaceShortage(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestMonolithic_ParallelWritingModeFlipTagged: a PARALLEL writing-mode
// flip (vertical-rl parent, vertical-lr child — same axes, different mode)
// is a writing-mode root in Blink (LayoutBox::IsWritingModeRoot compares
// modes, not axes) and therefore monolithic (layout_box.cc:3651-3666 @
// a9f50e522efa). The space gate already compares full writing modes, but
// the child-side builder tagging only saw IsOrthogonalWritingModeRoot, so
// the fragment stayed untagged — no balancing floor, no
// ShouldAvoidBreakInside (CodeRabbit 3514362226). The parent-side
// supplement must tag the returned fragment.
func TestMonolithic_ParallelWritingModeFlipTagged(t *testing.T) {
	child := makeNode("div")
	parent := makeNode("div", child)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "writing-mode", "vertical-rl",
			"width", "100px", "height", "100px"),
		child: makeStyle("display", "block", "writing-mode", "vertical-lr",
			"width", "80px", "height", "80px"),
	})

	wdm := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 100}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: 100}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(50).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		Build()

	result := NewBlockLayoutAlgorithm(testContext(), node, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	childFrag := findScrollFragment(result.Fragment)
	if childFrag == nil {
		t.Fatal("writing-mode-flip child fragment not placed")
	}
	if !childFrag.IsMonolithic {
		t.Error("parallel writing-mode flip child fragment must be tagged IsMonolithic")
	}
}

// TestFloatFragmentainerOffset_UsesFinalPosition: the fragmentainer offset
// handed to a non-monolithic float must reflect the float's FINAL block
// position — clear / exclusion repositioning can move it well past the
// tentative cursor position, and a stale offset lets fragmentation
// contexts inside the float see too much remaining space (CodeRabbit
// 3514362236). Mirrors Blink's PositionFloat, which lays out the float
// with a constraint space built at its resolved BFC offset when
// fragmentation is involved.
//
// Geometry: float A (left, 60 tall), float B (clear:left, explicit 80) in
// a 100px fragmentainer. B's final position is y=60, leaving 40 — its
// fragment must be truncated at 40 (Blink's first fragment), not laid out
// as if the whole 100px were still available.
func TestFloatFragmentainerOffset_UsesFinalPosition(t *testing.T) {
	fa := makeNode("div")
	fb := makeNode("div")
	parent := makeNode("div", fa, fb)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		fa: makeStyle("display", "block", "float", "left",
			"width", "100px", "height", "60px"),
		fb: makeStyle("display", "block", "float", "left", "clear", "left",
			"width", "100px", "height", "80px"),
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
	if got := len(result.Fragment.Children); got != 2 {
		t.Fatalf("placed fragments = %d, want 2 (both floats)", got)
	}
	fbFrag := result.Fragment.Children[1].Fragment
	if got := fbFrag.Size.HeightF64(); got != 40 {
		t.Errorf("cleared float fragment height = %v, want 40 (truncated at the fragmentainer from its FINAL y=60 position)", got)
	}
}

// TestResume_ConsumesAllChildBreakTokens: when the outgoing break token
// carries multiple child continuations (parallel flows), the RESUME must
// consume every one of them and must not re-lay completed untokened
// siblings. Mirrors Blink's BlockChildIterator: resumption walks ALL child
// break tokens in order, then proceeds to the sibling after the last
// tokened child only when !HasSeenAllChildren (block_child_iterator.cc
// :80-95 @ a9f50e522efa). The old resume path read only
// ChildBreakTokens[0] and re-laid every later sibling from scratch
// (CodeRabbit 3514362231).
//
// Geometry: A (h30, inner 400), B (h30, inner 400), C (h20) in a 100px
// fragmentainer. Fragment 1 sees all children and carries TWO at-end
// parallel tokens (A, B). The continuation must contain exactly A's and
// B's resumed slices — C must not reappear — and must carry both tokens
// forward (each inner still has 200px left after the second fragment).
func TestResume_ConsumesAllChildBreakTokens(t *testing.T) {
	innerA := makeNode("div")
	a := makeNode("div", innerA)
	innerB := makeNode("div")
	b := makeNode("div", innerB)
	c := makeNode("div")
	parent := makeNode("div", a, b, c)
	node := buildTestTree(parent, map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "100px"),
		a:      makeStyle("display", "block", "height", "30px"),
		innerA: makeStyle("display", "block", "height", "400px"),
		b:      makeStyle("display", "block", "height", "30px"),
		innerB: makeStyle("display", "block", "height", "400px"),
		c:      makeStyle("display", "block", "height", "20px"),
	})

	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	mkSpace := func(tok *BlockBreakToken) ConstraintSpace {
		bld := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(100).
			SetFragmentainerOffset(0).
			SetBlockFragmentationType(FragmentColumn)
		if tok != nil {
			bld = bld.SetBreakToken(tok)
		}
		return bld.Build()
	}

	first := NewBlockLayoutAlgorithm(testContext(), node, mkSpace(nil)).Layout()
	if first == nil || first.BreakToken == nil {
		t.Fatal("first fragment must carry a break token")
	}
	if got := len(first.BreakToken.ChildBreakTokens); got != 2 {
		t.Fatalf("first fragment child tokens = %d, want 2 (A and B parallel flows)", got)
	}
	if !first.BreakToken.HasSeenAllChildren {
		t.Fatal("first fragment must have seen all children")
	}

	second := NewBlockLayoutAlgorithm(testContext(), node, mkSpace(first.BreakToken)).Layout()
	if second == nil || second.Fragment == nil {
		t.Fatal("second fragment layout returned nil")
	}
	// Exactly A's and B's continuations — C completed in fragment 1 and
	// must not be re-laid.
	if got := len(second.Fragment.Children); got != 2 {
		t.Fatalf("second fragment children = %d, want 2 (A and B continuations only)", got)
	}
	if second.BreakToken == nil {
		t.Fatal("second fragment must still carry a break token (200px left in each inner)")
	}
	if got := len(second.BreakToken.ChildBreakTokens); got != 2 {
		t.Errorf("second fragment child tokens = %d, want 2 (both continuations carried)", got)
	}
}
