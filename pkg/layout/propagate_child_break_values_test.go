package layout

import "testing"

// TestShouldPropagateChildBreakValues_DedicatedBit (LOU-486, worklist B17):
// ConstraintSpace::ShouldPropagateChildBreakValues reads a dedicated space
// bit (constraint_space.h:618-620 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// not HasBlockFragmentation. Blink sets the bit where it sets up a
// fragmentation space: SetupSpaceBuilderForFragmentation
// (fragmentation_utils.cc:346), CreateConstraintSpaceForFragmentainer
// (:1391) — louis14's createConstraintSpaceForColumn — and the
// column-balancing space (column_layout_algorithm.cc:1850).
func TestShouldPropagateChildBreakValues_DedicatedBit(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	// A space that only has block fragmentation, without the bit, does not
	// propagate.
	bare := ConstraintSpace{HasBlockFragmentation: true, FragmentainerBlockSize: 100}
	if bare.ShouldPropagateChildBreakValues() {
		t.Errorf("HasBlockFragmentation alone: ShouldPropagateChildBreakValues() = true, want false (dedicated bit unset)")
	}

	// SetupSpaceBuilderForFragmentation sets the bit.
	parent := ConstraintSpace{HasBlockFragmentation: true, FragmentainerBlockSize: 100, BlockFragmentationType: FragmentColumn}
	b := NewConstraintSpaceBuilder(wdm, wdm, false)
	SetupSpaceBuilderForFragmentation(parent, 10, 100, b)
	if got := b.Build(); !got.ShouldPropagateChildBreakValues() {
		t.Errorf("SetupSpaceBuilderForFragmentation: ShouldPropagateChildBreakValues() = false, want true")
	}

	// The column (fragmentainer) space sets the bit.
	mla := &MulticolLayoutAlgorithm{remainingContentBlockSize: Indefinite}
	col := mla.createConstraintSpaceForColumn(wdm, 100, 50, Indefinite, nil, false, BreakAppealLastResort)
	if !col.ShouldPropagateChildBreakValues() {
		t.Errorf("createConstraintSpaceForColumn: ShouldPropagateChildBreakValues() = false, want true")
	}
}
