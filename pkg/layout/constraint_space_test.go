package layout

import "testing"

func TestConstraintSpaceBuilder_ParallelFlow(t *testing.T) {
	parentWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	childWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionRTL}

	cs := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	// Parallel flow: sizes pass through unchanged.
	if cs.AvailableSize.InlineSize.Float64() != 800 || cs.AvailableSize.BlockSize.Float64() != 600 {
		t.Errorf("available: got %v, want inline=800 block=600", cs.AvailableSize)
	}
	if cs.IsOrthogonalWritingModeRoot {
		t.Error("should not be orthogonal")
	}
	if cs.WritingDirection != childWDM {
		t.Errorf("writing direction: got %v, want %v", cs.WritingDirection, childWDM)
	}
}

func TestConstraintSpaceBuilder_OrthogonalFlow(t *testing.T) {
	parentWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	childWDM := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}

	cs := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	// Orthogonal: parent's inline (800) becomes child's block,
	// parent's block (600) becomes child's inline.
	if cs.AvailableSize.InlineSize.Float64() != 600 || cs.AvailableSize.BlockSize.Float64() != 800 {
		t.Errorf("available: got %v, want inline=600 block=800", cs.AvailableSize)
	}
	if !cs.IsOrthogonalWritingModeRoot {
		t.Error("should be orthogonal")
	}
}

func TestConstraintSpaceBuilder_NewBFC_ClearsExclusions(t *testing.T) {
	parentWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	childWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	es := &ExclusionSpace{}

	cs := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: Indefinite}).
		SetExclusionSpace(es).
		Build()

	// New BFC: exclusion space should be nil (not inherited).
	if cs.ExclusionSpace != nil {
		t.Error("new BFC should not inherit exclusion space")
	}
	if !cs.IsNewFormattingContext {
		t.Error("should be marked as new FC")
	}
}

func TestConstraintSpace_IndefiniteBlockSize(t *testing.T) {
	cs := ConstraintSpace{
		AvailableSize: oldLogicalToGeom(LogicalSize{InlineSize: 800, BlockSize: Indefinite}),
	}
	if !cs.IsBlockSizeIndefinite() {
		t.Error("should be indefinite")
	}

	cs2 := ConstraintSpace{
		AvailableSize: oldLogicalToGeom(LogicalSize{InlineSize: 800, BlockSize: 600}),
	}
	if cs2.IsBlockSizeIndefinite() {
		t.Error("should not be indefinite")
	}
}

func TestConstraintSpaceBuilder_OrthogonalPercentage(t *testing.T) {
	parentWDM := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	childWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	cs := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
		SetAvailableSize(LogicalSize{InlineSize: 500, BlockSize: 300}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 500, BlockSize: 300}).
		Build()

	// VRL parent (inline=height, block=width) with HTB child (orthogonal).
	// Parent's inline (500) → child's block, parent's block (300) → child's inline.
	if cs.PercentageResolutionSize.InlineSize != 300 || cs.PercentageResolutionSize.BlockSize != 500 {
		t.Errorf("pct: got %v, want inline=300 block=500", cs.PercentageResolutionSize)
	}
}
