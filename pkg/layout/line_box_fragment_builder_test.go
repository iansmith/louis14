package layout

import "testing"

// TestLineBoxFragmentBuilderBuildsLineBox pins that a line box is typed by
// its builder, before Build, as Blink's PhysicalLineBoxFragment is
// constructed from a LineBoxFragmentBuilder with kFragmentLineBox
// (inline/physical_line_box_fragment.cc:41-46 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and that a line box records
// no box-fragment scrollable overflow (PhysicalBoxFragment::Create's
// ScrollableOverflowCalculator runs only for box fragments) even when its
// children overflow it.
func TestLineBoxFragmentBuilderBuildsLineBox(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	b := NewLineBoxFragmentBuilder(wdm)
	b.SetSize(LogicalSize{InlineSize: 100, BlockSize: 20})
	// A line box built inside a block keeps a LayoutNode for its
	// container's style lookups; the overflow gate must not depend on its
	// absence.
	b.layoutNode = &LayoutInputNode{}
	b.AddChild(&PhysicalFragment{Type: FragmentText, Size: oldSizeToGeom(PhysicalSize{Width: 300, Height: 20})}, LogicalOffset{})
	frag := b.Build().Fragment
	if frag.Type != FragmentLineBox {
		t.Fatalf("Type = %v, want FragmentLineBox", frag.Type)
	}
	if frag.scrollableOverflow != nil {
		t.Errorf("line box recorded scrollable overflow %+v", *frag.scrollableOverflow)
	}
}
