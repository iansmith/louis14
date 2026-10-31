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
