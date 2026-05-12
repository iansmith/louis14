package layout

import (
	"testing"

	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
)

func TestBoxFragmentBuilder_HTB_LTR(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetSize(LogicalSize{InlineSize: 800, BlockSize: 600})

	// Add a child at logical offset (100, 50) with size 200x100.
	childFragment := &PhysicalFragment{
		Size: geometry.NewPhysicalSize(200, 100),
	}
	builder.AddChild(childFragment, LogicalOffset{InlineOffset: 100, BlockOffset: 50})

	result := builder.Build()

	// HTB+LTR: physical size = (800, 600), child at (100, 50).
	if result.Fragment.Size.WidthF64() != 800 || result.Fragment.Size.HeightF64() != 600 {
		t.Errorf("size: got %v, want 800x600", result.Fragment.Size)
	}
	if len(result.Fragment.Children) != 1 {
		t.Fatalf("children: got %d, want 1", len(result.Fragment.Children))
	}
	child := result.Fragment.Children[0]
	if child.Offset.LeftF64() != 100 || child.Offset.TopF64() != 50 {
		t.Errorf("child offset: got (%v,%v), want (100,50)", child.Offset.LeftF64(), child.Offset.TopF64())
	}
}

func TestBoxFragmentBuilder_VRL_LTR(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)
	// Logical: inline=600 (maps to height), block=800 (maps to width).
	builder.SetSize(LogicalSize{InlineSize: 600, BlockSize: 800})

	// A child of logical size inline=50, block=100 (physical 100x50).
	// At logical offset inline=20, block=30.
	childFragment := &PhysicalFragment{
		Size: geometry.NewPhysicalSize(100, 50), // block=100(W), inline=50(H)
	}
	builder.AddChild(childFragment, LogicalOffset{InlineOffset: 20, BlockOffset: 30})

	result := builder.Build()

	// VRL: physical size = (block=800, inline=600) = (800, 600).
	if result.Fragment.Size.WidthF64() != 800 || result.Fragment.Size.HeightF64() != 600 {
		t.Errorf("size: got %v, want 800x600", result.Fragment.Size)
	}

	// VRL+LTR offset: x = outerW - block - innerW = 800 - 30 - 100 = 670
	//                  y = inline = 20
	child := result.Fragment.Children[0]
	if child.Offset.LeftF64() != 670 || child.Offset.TopF64() != 20 {
		t.Errorf("child offset: got (%v,%v), want (670,20)", child.Offset.LeftF64(), child.Offset.TopF64())
	}
}

func TestBoxFragmentBuilder_VLR_LTR(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeVerticalLR, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetSize(LogicalSize{InlineSize: 600, BlockSize: 800})

	childFragment := &PhysicalFragment{
		Size: geometry.NewPhysicalSize(100, 50),
	}
	builder.AddChild(childFragment, LogicalOffset{InlineOffset: 20, BlockOffset: 30})

	result := builder.Build()

	// VLR+LTR offset: x = block = 30, y = inline = 20
	child := result.Fragment.Children[0]
	if child.Offset.LeftF64() != 30 || child.Offset.TopF64() != 20 {
		t.Errorf("child offset: got (%v,%v), want (30,20)", child.Offset.LeftF64(), child.Offset.TopF64())
	}
}

func TestBoxFragmentBuilder_HTB_RTL(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionRTL}
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetSize(LogicalSize{InlineSize: 800, BlockSize: 600})

	childFragment := &PhysicalFragment{
		Size: geometry.NewPhysicalSize(200, 100),
	}
	// Logical inline=100 from inline-start (which is the right edge in RTL).
	builder.AddChild(childFragment, LogicalOffset{InlineOffset: 100, BlockOffset: 50})

	result := builder.Build()

	// HTB+RTL offset: x = outerW - inline - innerW = 800 - 100 - 200 = 500
	//                 y = block = 50
	child := result.Fragment.Children[0]
	if child.Offset.LeftF64() != 500 || child.Offset.TopF64() != 50 {
		t.Errorf("child offset: got (%v,%v), want (500,50)", child.Offset.LeftF64(), child.Offset.TopF64())
	}
}

func TestMarginStrut(t *testing.T) {
	var ms MarginStrut

	if !ms.IsEmpty() {
		t.Error("fresh strut should be empty")
	}

	ms.Append(10)
	ms.Append(20)
	ms.Append(-5)
	ms.Append(-15)
	ms.Append(15)

	// Largest positive = 20, most negative = -15.
	// Collapsed = 20 + (-15) = 5.
	if ms.Resolve() != 5 {
		t.Errorf("resolve: got %v, want 5", ms.Resolve())
	}
	if ms.IsEmpty() {
		t.Error("should not be empty after appending")
	}
}

func TestBSFFPropagation_NilChild(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)

	builder.PropagateChildBlockSizeForFragmentation(nil, LogicalOffset{BlockOffset: 50})
	if _, ok := builder.BlockSizeForFragmentation(); ok {
		t.Error("nil child should not set BSFF")
	}

	builder.PropagateChildBlockSizeForFragmentation(&LayoutResult{}, LogicalOffset{BlockOffset: 50})
	if _, ok := builder.BlockSizeForFragmentation(); ok {
		t.Error("child with nil Fragment should not set BSFF")
	}
}

func TestBSFFPropagation_FragmentSize(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)

	// Child at block-offset 30, fragment block-size 100 → BSFF = 130.
	child := &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 100)}}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 30})

	v, ok := builder.BlockSizeForFragmentation()
	if !ok || v != 130 {
		t.Errorf("BSFF: got (%v,%v), want (130,true)", v, ok)
	}
}

func TestBSFFPropagation_MaxMerge(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)

	// Smaller contribution first, then a larger one — both block-end
	// values are max-merged; lower extents do not lower the floor.
	small := &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 40)}}
	large := &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 80)}}

	builder.PropagateChildBlockSizeForFragmentation(small, LogicalOffset{BlockOffset: 20}) // → 60
	builder.PropagateChildBlockSizeForFragmentation(large, LogicalOffset{BlockOffset: 50}) // → 130
	builder.PropagateChildBlockSizeForFragmentation(small, LogicalOffset{BlockOffset: 10}) // → 50 (no-op)

	v, _ := builder.BlockSizeForFragmentation()
	if v != 130 {
		t.Errorf("BSFF: got %v, want 130", v)
	}
}

func TestBSFFPropagation_ExplicitChildBSFFWins(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)

	// Child fragment is 50 tall but reports BSFF=200 (overflow). Effective
	// child contribution should be 200, so parent BSFF = offset + 200.
	child := &LayoutResult{
		Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 50)},
		BlockSizeForFragmentation: 200,
	}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 10})

	v, _ := builder.BlockSizeForFragmentation()
	if v != 210 {
		t.Errorf("BSFF (explicit override): got %v, want 210", v)
	}
}

func TestBSFFPropagation_ConsumedBlockSize(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)

	// Child fragment is 60 tall in this fragmentainer but was already
	// 40 tall in a prior fragment (ConsumedBlockSize=40). The effective
	// child contribution is 60+40=100, so parent BSFF = 20 + 100 = 120.
	child := &LayoutResult{
		Fragment:   &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 60)},
		BreakToken: &BlockBreakToken{ConsumedBlockSize: layoutunit.FromFloat64Round(40)},
	}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 20})

	v, _ := builder.BlockSizeForFragmentation()
	if v != 120 {
		t.Errorf("BSFF (with consumed): got %v, want 120", v)
	}
}

func TestBSFFPropagation_FlowsThroughBuild(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetSize(LogicalSize{InlineSize: 200, BlockSize: 100})

	child := &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 150)}}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 10})

	result := builder.Build()
	if result.BlockSizeForFragmentation != 160 {
		t.Errorf("result.BSFF: got %v, want 160", result.BlockSizeForFragmentation)
	}
}

func TestBSFFPropagation_VerticalRL(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)

	// VRL: block-axis = horizontal (width). PhysicalFragment is 100w x 50h
	// → block-size in VRL = width = 100.
	child := &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.NewPhysicalSize(100, 50)}}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 20})

	v, _ := builder.BlockSizeForFragmentation()
	if v != 120 {
		t.Errorf("BSFF (VRL): got %v, want 120", v)
	}
}

func TestLogicalFragment_ReadLogicalSize(t *testing.T) {
	// A physical fragment of size 800x600.
	phys := &PhysicalFragment{
		Size: geometry.NewPhysicalSize(800, 600),
	}

	// Read in HTB: inline=800 (width), block=600 (height).
	htb := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	lf := NewLogicalFragment(htb, phys)
	if lf.InlineSize() != 800 || lf.BlockSize() != 600 {
		t.Errorf("HTB: inline=%v block=%v, want 800,600", lf.InlineSize(), lf.BlockSize())
	}

	// Read in VRL: inline=600 (height), block=800 (width).
	vrl := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	lf2 := NewLogicalFragment(vrl, phys)
	if lf2.InlineSize() != 600 || lf2.BlockSize() != 800 {
		t.Errorf("VRL: inline=%v block=%v, want 600,800", lf2.InlineSize(), lf2.BlockSize())
	}
}
