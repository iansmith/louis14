package layout

import "testing"

// TestSetupSpaceBuilderForFragmentation_RareFields (LOU-501, worklist C12):
// SetupSpaceBuilderForFragmentation copies RequiresContentBeforeBreaking
// from its argument, and IsInsideBreakAvoid and ShouldIgnoreForcedBreaks from
// the parent space (fragmentation_utils.cc:347-353, :364-367 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the parent-builder overload
// takes RequiresContentBeforeBreaking from the builder and puts the child
// inside break avoidance under a break-inside:avoid parent (:379-397).
// ShouldIgnoreForcedBreaks is read by IsForcedBreakValue (:91-93).
func TestSetupSpaceBuilderForFragmentation_RareFields(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	parent := ConstraintSpace{
		HasBlockFragmentation:    true,
		FragmentainerBlockSize:   100,
		BlockFragmentationType:   FragmentColumn,
		IsInsideBreakAvoid:       true,
		ShouldIgnoreForcedBreaks: true,
	}
	b := NewConstraintSpaceBuilder(wdm, wdm, false)
	SetupSpaceBuilderForFragmentation(parent, 10, 100, true, b)
	child := b.Build()
	if !child.RequiresContentBeforeBreaking || !child.IsInsideBreakAvoid || !child.ShouldIgnoreForcedBreaks {
		t.Errorf("child space: RequiresContentBeforeBreaking=%v IsInsideBreakAvoid=%v ShouldIgnoreForcedBreaks=%v, want all true",
			child.RequiresContentBeforeBreaking, child.IsInsideBreakAvoid, child.ShouldIgnoreForcedBreaks)
	}
	if IsForcedBreakValue(child, "column") {
		t.Errorf("IsForcedBreakValue(column) inside a space ignoring forced breaks = true, want false")
	}

	plain := ConstraintSpace{HasBlockFragmentation: true, FragmentainerBlockSize: 100, BlockFragmentationType: FragmentColumn}
	b = NewConstraintSpaceBuilder(wdm, wdm, false)
	SetupSpaceBuilderForFragmentation(plain, 10, 100, false, b)
	child = b.Build()
	if child.RequiresContentBeforeBreaking || child.IsInsideBreakAvoid || child.ShouldIgnoreForcedBreaks {
		t.Errorf("plain parent: child space has rare fields set: %+v", child)
	}
	if !IsForcedBreakValue(child, "column") {
		t.Errorf("IsForcedBreakValue(column) in a column space = false, want true")
	}

	// Parent-builder overload: the builder's RequiresContentBeforeBreaking,
	// and break-inside:avoid on the parent.
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetConstraintSpace(&plain)
	builder.SetLayoutNode(buildTestTree(makeNode("div"), nil))
	builder.style = makeStyle("display", "block", "break-inside", "avoid")
	builder.SetRequiresContentBeforeBreaking(true)
	b = NewConstraintSpaceBuilder(wdm, wdm, false)
	SetupSpaceBuilderForFragmentationFromBuilder(builder, 0, 100, b)
	child = b.Build()
	if !child.RequiresContentBeforeBreaking || !child.IsInsideBreakAvoid {
		t.Errorf("from builder: RequiresContentBeforeBreaking=%v IsInsideBreakAvoid=%v, want both true",
			child.RequiresContentBeforeBreaking, child.IsInsideBreakAvoid)
	}

	// Adding child content ends the requirement (box_fragment_builder.cc:
	// 376-377).
	builder.AddChild(&PhysicalFragment{}, LogicalOffset{})
	if builder.RequiresContentBeforeBreaking() {
		t.Errorf("RequiresContentBeforeBreaking after AddChild = true, want false")
	}
}
