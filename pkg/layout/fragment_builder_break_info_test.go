package layout

import (
	"slices"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/html"
)

// breakInfoFixture returns a builder laid out under column fragmentation
// (a 100px fragmentainer, known size) and an in-flow block child, a float
// and an absolutely positioned child of its node, for exercising the
// builder-owned break bookkeeping (LOU-492, worklist D1).
func breakInfoFixture(t *testing.T) (*BoxFragmentBuilder, *LayoutInputNode, *LayoutInputNode, *LayoutInputNode) {
	t.Helper()
	inflowEl := makeNode("div")
	floatEl := makeNode("div")
	oofEl := makeNode("div")
	root := makeNode("div", inflowEl, floatEl, oofEl)
	styles := map[*html.Node]*css.Style{
		root:     makeStyle("display", "block"),
		inflowEl: makeStyle("display", "block"),
		floatEl:  makeStyle("display", "block", "float", "left"),
		oofEl:    makeStyle("display", "block", "position", "absolute"),
	}
	tree := buildTestTree(root, styles)
	space := columnFragmentationSpace()
	builder := NewBoxFragmentBuilder(space.WritingDirection)
	builder.SetConstraintSpace(&space)
	children := tree.Children()
	return builder, children[0], children[1], children[2]
}

// TestBoxFragmentBuilder_AddBreakBeforeChild mirrors Blink's
// BoxFragmentBuilder::AddBreakBeforeChild (box_fragment_builder.cc:121-178 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the child's break-before
// token joins the child break tokens; an in-flow child (not a float or an
// out-of-flow box, :139-140) means the container breaks inside in its own
// flow; a forced break sets the forced-break flag, which drops the minimal
// space shortage (SetHasForcedBreak, box_fragment_builder.h:734-737); an
// unforced break clamps the break appeal (:127-134).
func TestBoxFragmentBuilder_AddBreakBeforeChild(t *testing.T) {
	builder, inflow, float, oof := breakInfoFixture(t)

	builder.AddBreakBeforeChild(float, nil, false)
	builder.AddBreakBeforeChild(oof, nil, false)
	if builder.HasInflowChildBreakInside() {
		t.Error("break before a float or out-of-flow child: HasInflowChildBreakInside = true, want false")
	}
	if !builder.HasInsertedChildBreak() || !builder.ShouldBreakInside() {
		t.Error("break before a float: want HasInsertedChildBreak and ShouldBreakInside")
	}

	builder.PropagateSpaceShortage(30)
	appeal := BreakAppealViolatingBreakAvoid
	builder.AddBreakBeforeChild(inflow, &appeal, false)
	if !builder.HasInflowChildBreakInside() {
		t.Error("break before an in-flow child: HasInflowChildBreakInside = false, want true")
	}
	if builder.HasForcedBreak() {
		t.Error("unforced break: HasForcedBreak = true")
	}
	if !builder.hasBreakAppeal || builder.breakAppeal != BreakAppealViolatingBreakAvoid {
		t.Errorf("break appeal = %v (set=%v), want clamped to ViolatingBreakAvoid", builder.breakAppeal, builder.hasBreakAppeal)
	}
	tokens := builder.ChildBreakTokens()
	if len(tokens) != 3 {
		t.Fatalf("ChildBreakTokens = %d, want 3", len(tokens))
	}
	if last := builder.LastChildBreakToken(); last.Node != inflow || !last.IsBreakBefore || last.IsForcedBreak {
		t.Errorf("LastChildBreakToken = %+v, want an unforced break-before for the in-flow child", last)
	}
	if got := builder.Build().MinSpaceShortage; got != 30 {
		t.Errorf("unforced break: MinSpaceShortage = %v, want 30", got)
	}

	builder.AddBreakBeforeChild(inflow, nil, true)
	if !builder.HasForcedBreak() {
		t.Error("forced break: HasForcedBreak = false")
	}
	if !builder.LastChildBreakToken().IsForcedBreak {
		t.Error("forced break: the break-before token is not forced")
	}
	result := builder.Build()
	if !result.HasForcedBreak {
		t.Error("forced break: LayoutResult.HasForcedBreak = false")
	}
	if result.MinSpaceShortage != 0 {
		t.Errorf("forced break: MinSpaceShortage = %v, want none (SetHasForcedBreak drops it)", result.MinSpaceShortage)
	}
}

// TestBoxFragmentBuilder_AddBreakToken mirrors Blink's BoxFragmentBuilder::
// AddBreakToken (box_fragment_builder.cc:381-389 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a parallel-flow token is a
// child break, but the container's own flow goes on.
func TestBoxFragmentBuilder_AddBreakToken(t *testing.T) {
	builder, inflow, float, _ := breakInfoFixture(t)
	if builder.ShouldBreakInside() {
		t.Fatal("fresh builder: ShouldBreakInside = true")
	}
	floatToken := &BlockBreakToken{Node: float}
	builder.AddBreakToken(floatToken, true)
	if builder.HasInflowChildBreakInside() || !builder.ShouldBreakInside() {
		t.Errorf("parallel-flow token: (HasInflowChildBreakInside, ShouldBreakInside) = (%v, %v), want (false, true)",
			builder.HasInflowChildBreakInside(), builder.ShouldBreakInside())
	}
	inflowToken := &BlockBreakToken{Node: inflow}
	builder.AddBreakToken(inflowToken, false)
	if !builder.HasInflowChildBreakInside() {
		t.Error("same-flow token: HasInflowChildBreakInside = false")
	}
	if got := builder.ChildBreakTokens(); !slices.Equal(got, []*BlockBreakToken{floatToken, inflowToken}) {
		t.Errorf("ChildBreakTokens = %v, want the tokens in the order added", got)
	}
}

// TestBoxFragmentBuilder_AddResultPropagatesBreakInfo mirrors Blink's
// BoxFragmentBuilder::AddResult (box_fragment_builder.cc:180-233) collecting
// the child's break token (FragmentBuilder::PropagateFromFragment,
// fragment_builder.cc:455-464) and running PropagateBreakInfo
// (box_fragment_builder.cc:479-589 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// a child broken inside before its block end breaks this box in its own
// flow and clamps its appeal; a child past its block end (a parallel flow)
// adds its token only; a forced break inside a child is this box's; any
// other child shortage propagates.
func TestBoxFragmentBuilder_AddResultPropagatesBreakInfo(t *testing.T) {
	builder, inflow, _, _ := breakInfoFixture(t)
	frag := func() *PhysicalFragment {
		return &PhysicalFragment{Type: FragmentBox, Size: geometry.NewPhysicalSize(100, 40)}
	}

	parallel := &LayoutResult{Fragment: frag(), BreakToken: &BlockBreakToken{Node: inflow, IsAtBlockEnd: true}, MinSpaceShortage: 12}
	builder.AddResult(inflow, parallel, LogicalOffset{BlockOffset: 10}, LogicalOffset{})
	if builder.HasInflowChildBreakInside() {
		t.Error("child past its block end: HasInflowChildBreakInside = true, want false (parallel flow)")
	}
	if !builder.ShouldBreakInside() || builder.LastChildBreakToken() != parallel.BreakToken {
		t.Error("child past its block end: its token is not a child break token")
	}
	if v, ok := builder.BlockSizeForFragmentation(); !ok || v != 50 {
		t.Errorf("BlockSizeForFragmentation = (%v, %v), want (50, true)", v, ok)
	}
	if got := builder.Build().MinSpaceShortage; got != 12 {
		t.Errorf("MinSpaceShortage = %v, want the child's 12", got)
	}

	inside := &LayoutResult{Fragment: frag(), BreakToken: &BlockBreakToken{Node: inflow}, HasForcedBreak: true, BreakAppeal: BreakAppealPerfect}
	builder.AddResult(inflow, inside, LogicalOffset{BlockOffset: 50}, LogicalOffset{})
	if !builder.HasInflowChildBreakInside() {
		t.Error("child broken inside: HasInflowChildBreakInside = false")
	}
	if !builder.HasForcedBreak() {
		t.Error("child with a forced break inside: HasForcedBreak = false")
	}
	if got := len(builder.ChildBreakTokens()); got != 2 {
		t.Errorf("ChildBreakTokens = %d, want 2", got)
	}
	if got := len(builder.children); got != 2 {
		t.Errorf("children = %d, want both results added", got)
	}

	unbroken := &LayoutResult{Fragment: frag()}
	builder.AddResult(inflow, unbroken, LogicalOffset{BlockOffset: 90}, LogicalOffset{})
	if got := len(builder.ChildBreakTokens()); got != 2 {
		t.Errorf("unbroken child: ChildBreakTokens = %d, want still 2", got)
	}
}

// TestBoxFragmentBuilder_HasSeenAllChildren mirrors Blink's
// BoxFragmentBuilder::SetHasSeenAllChildren (box_fragment_builder.h:547-548 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBoxFragmentBuilder_HasSeenAllChildren(t *testing.T) {
	builder, _, _, _ := breakInfoFixture(t)
	if builder.HasSeenAllChildren() {
		t.Fatal("fresh builder: HasSeenAllChildren = true")
	}
	builder.SetHasSeenAllChildren()
	if !builder.HasSeenAllChildren() {
		t.Error("SetHasSeenAllChildren did not stick")
	}
}
