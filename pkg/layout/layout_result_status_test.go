package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// outOfFragmentainerSpaceFixture returns a column-fragmentation space (100px
// fragmentainers), a block child and a builder for exercising
// BreakBeforeChildIfNeeded with an aborted child result.
func outOfFragmentainerSpaceFixture() (ConstraintSpace, *LayoutInputNode, *BoxFragmentBuilder) {
	childEl := makeNode("div")
	root := makeNode("div", childEl)
	styles := map[*html.Node]*css.Style{
		root:    makeStyle("display", "block"),
		childEl: makeStyle("display", "block"),
	}
	tree := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 100}).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetFragmentainerBlockSize(100).
		Build()
	return space, tree.Children()[0], NewBoxFragmentBuilder(wdm)
}

// TestBreakBeforeChildIfNeeded_OutOfFragmentainerSpaceBreaksBefore (LOU-468):
// a child whose layout aborted with kOutOfFragmentainerSpace produced no
// fragment, so there is nothing to move past and the container must break
// before it (child-overload MovePastBreakpoint,
// fragmentation_utils.cc:1077-1083 @ a9f50e522efa), even though the
// fragmentainer has plenty of space left.
func TestBreakBeforeChildIfNeeded_OutOfFragmentainerSpaceBreaksBefore(t *testing.T) {
	space, child, builder := outOfFragmentainerSpaceFixture()
	aborted := &LayoutResult{Status: LayoutResultOutOfFragmentainerSpace}
	status, isForced := BreakBeforeChildIfNeeded(space, child, aborted,
		10, space.FragmentainerBlockSize, true, builder)
	if status != BreakStatusBrokeBefore || isForced {
		t.Fatalf("BreakBeforeChildIfNeeded = (%v, forced=%v), want (BrokeBefore, forced=false)", status, isForced)
	}
	if !builder.hasBreakAppeal || builder.breakAppeal != BreakAppealPerfect {
		t.Errorf("break appeal = %v (set=%v), want BreakAppealPerfect", builder.breakAppeal, builder.hasBreakAppeal)
	}
}

// TestCalculateBreakAppealBefore_NoContainerSeparationOutOfSpaceIsPerfect
// (LOU-419, worklist B22): with no container separation, breaking before the
// first child is normally a last-resort break, but only when the child laid
// out successfully. A child that ran out of fragmentainer space must move
// to the next fragmentainer anyway, so the break before it is perfect
// (CalculateBreakAppealBefore gates the no-separation branch on
// layout_result_status == kSuccess, fragmentation_utils.cc:244-245 @
// a9f50e522efa).
func TestCalculateBreakAppealBefore_NoContainerSeparationOutOfSpaceIsPerfect(t *testing.T) {
	space, child, builder := outOfFragmentainerSpaceFixture()
	aborted := &LayoutResult{Status: LayoutResultOutOfFragmentainerSpace}
	if got := CalculateBreakAppealBefore(space, child, aborted, builder, false); got != BreakAppealPerfect {
		t.Errorf("CalculateBreakAppealBefore = %v, want BreakAppealPerfect", got)
	}
	status, _ := BreakBeforeChildIfNeeded(space, child, aborted,
		0, space.FragmentainerBlockSize, false, builder)
	if status != BreakStatusBrokeBefore {
		t.Fatalf("BreakBeforeChildIfNeeded = %v, want BrokeBefore", status)
	}
	if builder.breakAppeal != BreakAppealPerfect {
		t.Errorf("break appeal = %v, want BreakAppealPerfect", builder.breakAppeal)
	}
}

// TestCalculateBreakAppealBefore_NoContainerSeparationSuccessIsLastResort
// (LOU-419): the kSuccess side of the same gate — breaking before the first
// successfully laid-out child, without a resumed-container exemption, stays
// last-resort (fragmentation_utils.cc:244-254 @ a9f50e522efa).
func TestCalculateBreakAppealBefore_NoContainerSeparationSuccessIsLastResort(t *testing.T) {
	space, child, builder := outOfFragmentainerSpaceFixture()
	if got := CalculateBreakAppealBeforeWithStatus(space, LayoutResultSuccess, "auto", false, false); got != BreakAppealLastResort {
		t.Errorf("success, no separation: appeal = %v, want BreakAppealLastResort", got)
	}
	if got := CalculateBreakAppealBefore(space, child, &LayoutResult{}, builder, true); got != BreakAppealPerfect {
		t.Errorf("success, with separation: appeal = %v, want BreakAppealPerfect", got)
	}
}

// TestCalculateBreakAppealBefore_ResumedChildAtStartOfResumedContainerIsLastResort
// (LOU-419 review): at the start of a resumed container that permits a
// break there (MinBreakAppeal above LastResort, previous break inside),
// only a child producing its first fragment may take that floor. A child
// itself resumed after a break inside it is not first-for-node, so breaking
// before it stays last-resort (IsBreakableAtStartOfResumedContainer result
// overload :198-212 ANDs in IsFirstForNode via :214-219 @ a9f50e522efa).
func TestCalculateBreakAppealBefore_ResumedChildAtStartOfResumedContainerIsLastResort(t *testing.T) {
	_, child, builder := outOfFragmentainerSpaceFixture()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 100}).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetFragmentainerBlockSize(100).
		SetMinBreakAppeal(BreakAppealPerfect).
		SetBreakToken(&BlockBreakToken{}).
		Build()

	firstForNode := &LayoutResult{Fragment: &PhysicalFragment{}}
	if got := CalculateBreakAppealBefore(space, child, firstForNode, builder, false); got != BreakAppealPerfect {
		t.Errorf("first-for-node child: appeal = %v, want MinBreakAppeal (BreakAppealPerfect)", got)
	}
	resumed := &LayoutResult{Fragment: &PhysicalFragment{notFirstForNode: true}}
	if got := CalculateBreakAppealBefore(space, child, resumed, builder, false); got != BreakAppealLastResort {
		t.Errorf("resumed child: appeal = %v, want BreakAppealLastResort", got)
	}
}

// TestIsFirstForNode_ResumedFragmentsAreNotFirst (LOU-419 review): a block
// and a nested multicol that break across two columns produce a first
// fragment (IsFirstForNode) and a resumed one (not), as Blink's
// SetupFragmentBuilderForFragmentation clears is_first_for_node after a
// break inside (fragmentation_utils.cc:426-430 @ a9f50e522efa).
func TestIsFirstForNode_ResumedFragmentsAreNotFirst(t *testing.T) {
	for _, tc := range []struct {
		name  string
		style *css.Style
	}{
		{"block", makeStyle("display", "block")},
		{"multicol", makeStyle("display", "block", "column-count", "1", "column-gap", "0")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tall := makeNode("div")
			container := makeNode("div", tall)
			styles := map[*html.Node]*css.Style{
				container: tc.style,
				tall:      makeStyle("display", "block", "height", "150px"),
			}
			mc := layoutTwoColumn(t, container, styles, "auto")
			first := childLinkFor(mc.Children[0].Fragment, container)
			second := childLinkFor(mc.Children[1].Fragment, container)
			if first == nil || second == nil {
				t.Fatalf("container fragments: col1=%v col2=%v, want both", first != nil, second != nil)
			}
			if !first.Fragment.IsFirstForNode() {
				t.Errorf("column 1 fragment: IsFirstForNode = false, want true")
			}
			if second.Fragment.IsFirstForNode() {
				t.Errorf("column 2 fragment: IsFirstForNode = true, want false")
			}
		})
	}
}
