package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-403: the builder owns the column-spanner state, as Blink's
// FragmentBuilder does (fragment_builder.h:430-450 @ a9f50e522efa):
// SetColumnSpannerPath also records that a spanner was found,
// SetHasColumnSpanner records one without a path (the deferral case of
// block_layout_algorithm.cc:1025-1040), and Build() copies the path and the
// empty-spanner-parent bit onto the LayoutResult (layout_result.h:207-221).
func TestBoxFragmentBuilder_ColumnSpannerState_LOU403(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	b := NewBoxFragmentBuilder(wdm)
	if b.FoundColumnSpanner() {
		t.Fatal("fresh builder reports a spanner")
	}
	path := &ColumnSpannerPath{Box: &LayoutInputNode{}}
	b.SetColumnSpannerPath(path)
	if !b.FoundColumnSpanner() {
		t.Error("SetColumnSpannerPath did not record that a spanner was found")
	}
	b.SetIsEmptySpannerParent(true)
	result := b.Build()
	if result.ColumnSpannerPath != path {
		t.Errorf("Build() ColumnSpannerPath = %v, want the builder's path", result.ColumnSpannerPath)
	}
	if !result.IsEmptySpannerParent {
		t.Error("Build() dropped IsEmptySpannerParent")
	}

	deferred := NewBoxFragmentBuilder(wdm)
	deferred.SetHasColumnSpanner(true)
	if !deferred.FoundColumnSpanner() {
		t.Error("SetHasColumnSpanner(true) without a path: FoundColumnSpanner() = false")
	}
	if got := deferred.Build().ColumnSpannerPath; got != nil {
		t.Errorf("deferred spanner built a path %v, want nil", got)
	}
}

// LOU-403: a block resumed after a spanner, whose next child is a second
// spanner, interrupts again with no children placed in this fragment, so its
// result is an empty spanner parent (Blink SetIsEmptySpannerParent(
// Children().empty() && is_resuming_), block_layout_algorithm.cc:1054-1055 @
// a9f50e522efa) and its break token is caused by the spanner
// (block_break_token.cc:70). A fresh wrapper is not empty.
func TestBlockLayout_SpannerParentState_LOU403(t *testing.T) {
	spanner1 := makeNode("div")
	spanner2 := makeNode("div")
	wrapper := makeNode("div", spanner1, spanner2)

	styles := map[*html.Node]*css.Style{
		wrapper:  makeStyle("display", "block"),
		spanner1: makeStyle("display", "block", "column-span", "all", "height", "10px"),
		spanner2: makeStyle("display", "block", "column-span", "all", "height", "10px"),
	}
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	wrapperNode := buildTestTree(wrapper, styles)
	children := wrapperNode.Children()

	columnSpace := func(token *BlockBreakToken) ConstraintSpace {
		return NewConstraintSpaceBuilder(wdm, wdm, false).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(100).
			SetBlockFragmentationType(FragmentColumn).
			SetBreakToken(token).
			Build()
	}

	fresh := NewBlockLayoutAlgorithm(ctx, wrapperNode, columnSpace(nil)).Layout()
	if fresh.ColumnSpannerPath == nil || fresh.ColumnSpannerPath.Box != children[0] {
		t.Fatalf("fresh wrapper: spanner path = %+v, want spanner1", fresh.ColumnSpannerPath)
	}
	if fresh.IsEmptySpannerParent {
		t.Error("fresh wrapper reported as an empty spanner parent")
	}
	if fresh.BreakToken == nil || !fresh.BreakToken.IsCausedByColumnSpanner {
		t.Errorf("fresh wrapper: break token %+v not caused by the spanner", fresh.BreakToken)
	}

	resumed := NewBlockLayoutAlgorithm(ctx, wrapperNode, columnSpace(&BlockBreakToken{
		Node:                    wrapperNode,
		IsCausedByColumnSpanner: true,
		ChildBreakTokens: []*BlockBreakToken{{
			Node: children[1], IsBreakBefore: true, IsForcedBreak: true,
		}},
	})).Layout()
	if resumed.ColumnSpannerPath == nil || resumed.ColumnSpannerPath.Box != children[1] {
		t.Fatalf("resumed wrapper: spanner path = %+v, want spanner2", resumed.ColumnSpannerPath)
	}
	if !resumed.IsEmptySpannerParent {
		t.Error("resumed wrapper with no children placed is not an empty spanner parent")
	}
}

// LOU-403: the empty-spanner-parent bit of the innermost parent propagates up
// through an intermediate block on the spanner path, as Blink's
// PropagateBreakInfo copies the child's IsEmptySpannerParent
// (box_fragment_builder.cc:594-608 @ a9f50e522efa).
func TestBlockLayout_NestedSpannerParentState_LOU403(t *testing.T) {
	spanner1 := makeNode("div")
	spanner2 := makeNode("div")
	inner := makeNode("div", spanner1, spanner2)
	outer := makeNode("div", inner)

	styles := map[*html.Node]*css.Style{
		outer:    makeStyle("display", "block"),
		inner:    makeStyle("display", "block"),
		spanner1: makeStyle("display", "block", "column-span", "all", "height", "10px"),
		spanner2: makeStyle("display", "block", "column-span", "all", "height", "10px"),
	}
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	outerNode := buildTestTree(outer, styles)
	innerNode := outerNode.Children()[0]
	spanner2Node := innerNode.Children()[1]

	space := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(100).
		SetBlockFragmentationType(FragmentColumn).
		SetBreakToken(&BlockBreakToken{
			Node:                    outerNode,
			IsCausedByColumnSpanner: true,
			ChildBreakTokens: []*BlockBreakToken{{
				Node:                    innerNode,
				IsCausedByColumnSpanner: true,
				ChildBreakTokens: []*BlockBreakToken{{
					Node: spanner2Node, IsBreakBefore: true, IsForcedBreak: true,
				}},
			}},
		}).
		Build()
	result := NewBlockLayoutAlgorithm(ctx, outerNode, space).Layout()
	path := result.ColumnSpannerPath
	if path == nil || path.Box != innerNode || path.Child == nil || path.Child.Box != spanner2Node {
		t.Fatalf("spanner path = %+v, want inner → spanner2", path)
	}
	if !result.IsEmptySpannerParent {
		t.Error("outer did not propagate the resumed inner block's IsEmptySpannerParent")
	}
}
