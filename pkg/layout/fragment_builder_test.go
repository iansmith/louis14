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

// LOU-411 (LOU-399's BSFF half): the builder accumulates Blink's
// BlockSizeForFragmentation. Blink @ a9f50e522efa: AddResult propagates
// every in-flow child when the space has block fragmentation
// (box_fragment_builder.cc:227-229) — PropagateBreakInfo max-merges
// offset + BlockSizeForFragmentation(child) (:479-489), no overflow-only
// gate and no consumed term; ToBoxFragment finalizes a non-fragmentainer
// box to max(that, FragmentBlockSize()), or FragmentBlockSize() when it
// clips block-axis overflow, and leaves a fragmentainer's children-only
// value (:743-761); the result records it only under block fragmentation
// (layout_result.cc:75-79). BSFF is not Chrome-observable, so the expected
// values are Blink's arithmetic.

// newFragmentationBuilder returns a builder whose space has block
// fragmentation, so it accumulates BlockSizeForFragmentation.
func newFragmentationBuilder(wdm WritingDirectionMode) *BoxFragmentBuilder {
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetConstraintSpace(&ConstraintSpace{HasBlockFragmentation: true})
	return builder
}

func TestBSFFPropagation_NilChild(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := newFragmentationBuilder(wdm)

	builder.PropagateChildBlockSizeForFragmentation(nil, LogicalOffset{BlockOffset: 50})
	if _, ok := builder.BlockSizeForFragmentation(); ok {
		t.Error("nil child should not set BSFF")
	}

	builder.PropagateChildBlockSizeForFragmentation(&LayoutResult{}, LogicalOffset{BlockOffset: 50})
	if _, ok := builder.BlockSizeForFragmentation(); ok {
		t.Error("child with nil Fragment should not set BSFF")
	}
}

// A child that fits inside its own box still contributes its block-end
// (box_fragment_builder.cc:484-489): 30 + 100. Corrected from the
// louis14-only overflow gate's no-op (ledger L86).
func TestBSFFPropagation_NonOverflowingChildAccumulates(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := newFragmentationBuilder(wdm)

	child := &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 100)}}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 30})

	if v, ok := builder.BlockSizeForFragmentation(); !ok || v != 130 {
		t.Errorf("BSFF: got (%v,%v), want (130,true)", v, ok)
	}
}

// Without block fragmentation nothing is accumulated (AddResult's gate,
// box_fragment_builder.cc:227-229).
func TestBSFFPropagation_NoBlockFragmentationIsNoOp(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetConstraintSpace(&ConstraintSpace{})

	child := &LayoutResult{
		Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 50)},
		BlockSizeForFragmentation: 200,
	}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 10})
	if _, ok := builder.BlockSizeForFragmentation(); ok {
		t.Error("BSFF accumulated without block fragmentation")
	}
}

func TestBSFFPropagation_ExplicitChildBSFFWins(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := newFragmentationBuilder(wdm)

	// Child fragment is 50 tall but reports BSFF=200 (true overflow,
	// e.g. a spanner with parallel-flow children). Effective child
	// contribution = 200, so parent BSFF = offset + 200.
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

func TestBSFFPropagation_MaxMergeOverChildren(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := newFragmentationBuilder(wdm)

	// Block-ends are max-merged; smaller extents do not lower the value, and
	// a child that fits in its own box counts like any other.
	small := &LayoutResult{
		Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 40)},
		BlockSizeForFragmentation: 60,
	}
	large := &LayoutResult{
		Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 80)},
		BlockSizeForFragmentation: 100,
	}
	fits := &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 40)}}

	builder.PropagateChildBlockSizeForFragmentation(small, LogicalOffset{BlockOffset: 20}) // → 80
	builder.PropagateChildBlockSizeForFragmentation(large, LogicalOffset{BlockOffset: 50}) // → 150
	builder.PropagateChildBlockSizeForFragmentation(small, LogicalOffset{BlockOffset: 10}) // → 70 (lower)
	builder.PropagateChildBlockSizeForFragmentation(fits, LogicalOffset{BlockOffset: 120}) // → 160

	v, _ := builder.BlockSizeForFragmentation()
	if v != 160 {
		t.Errorf("BSFF: got %v, want 160", v)
	}
}

// Blink's BSFF is per fragment: a resumed child's consumed block-size is
// not added (box_fragment_builder.cc:484-489). Corrected from the louis14
// physical-extent floor (ledger L86).
func TestBSFFPropagation_NoConsumedBlockSizeTerm(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	consumed := &BlockBreakToken{ConsumedBlockSize: layoutunit.FromFloat64Round(40)}
	for _, tc := range []struct {
		name  string
		child *LayoutResult
		want  float64
	}{
		// 20 + max(60, 140).
		{"explicit BSFF", &LayoutResult{
			Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 60)},
			BreakToken:                consumed,
			BlockSizeForFragmentation: 140,
		}, 160},
		// 20 + 60: the 40px consumed earlier is not this fragment's.
		{"fragment size only", &LayoutResult{
			Fragment:   &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 60)},
			BreakToken: consumed,
		}, 80},
	} {
		builder := newFragmentationBuilder(wdm)
		builder.PropagateChildBlockSizeForFragmentation(tc.child, LogicalOffset{BlockOffset: 20})
		if v, ok := builder.BlockSizeForFragmentation(); !ok || v != tc.want {
			t.Errorf("%s: BSFF got (%v,%v), want (%v,true)", tc.name, v, ok, tc.want)
		}
	}
}

// A child whose BSFF equals its own extent contributes it. Corrected from
// the louis14-only overflow gate's no-op (ledger L86).
func TestBSFFPropagation_BSFFAtPhysicalExtentAccumulates(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := newFragmentationBuilder(wdm)

	child := &LayoutResult{
		Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 100)},
		BlockSizeForFragmentation: 100,
	}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 0})

	if v, ok := builder.BlockSizeForFragmentation(); !ok || v != 100 {
		t.Errorf("BSFF: got (%v,%v), want (100,true)", v, ok)
	}
}

func TestBSFFPropagation_FlowsThroughBuild(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := newFragmentationBuilder(wdm)
	builder.SetSize(LogicalSize{InlineSize: 200, BlockSize: 100})

	child := &LayoutResult{
		Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 80)},
		BlockSizeForFragmentation: 150,
	}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 10})

	result := builder.Build()
	if result.BlockSizeForFragmentation != 160 {
		t.Errorf("result.BSFF: got %v, want 160", result.BlockSizeForFragmentation)
	}
}

func TestBSFFPropagation_VerticalRL(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	builder := newFragmentationBuilder(wdm)

	// VRL: block-axis = horizontal (width). PhysicalFragment is 100w x 50h
	// → block-size in VRL = width = 100. Explicit BSFF=180 indicates
	// overflow past the 100 physical extent.
	child := &LayoutResult{
		Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(100, 50)},
		BlockSizeForFragmentation: 180,
	}
	builder.PropagateChildBlockSizeForFragmentation(child, LogicalOffset{BlockOffset: 20})

	v, _ := builder.BlockSizeForFragmentation()
	if v != 200 {
		t.Errorf("BSFF (VRL): got %v, want 200", v)
	}
}

func TestBSFFPropagation_SetterBypassesGate(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	builder := NewBoxFragmentBuilder(wdm)

	// SetBlockSizeForFragmentation is the unconditional setter for
	// callsites that want to inject a value directly.
	builder.SetBlockSizeForFragmentation(220)
	v, ok := builder.BlockSizeForFragmentation()
	if !ok || v != 220 {
		t.Errorf("BSFF: got (%v,%v), want (220,true)", v, ok)
	}
}

// TestBSFFFinalize_LOU411: the value Build records (box_fragment_builder.cc:
// 743-761, layout_result.cc:75-79 @ a9f50e522efa). Child offsets are
// content-box relative in louis14, so the block-start border and padding
// are added to reach Blink's border-box block-end.
func TestBSFFFinalize_LOU411(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	child := func(h int) *LayoutResult {
		return &LayoutResult{Fragment: &PhysicalFragment{Size: geometry.NewPhysicalSize(50, h)}}
	}
	type kid struct {
		result *LayoutResult
		offset float64
	}
	for _, tc := range []struct {
		name    string
		noFrag  bool
		size    float64
		style   []string
		boxData *PhysicalBoxData
		boxType BoxType
		kids    []kid
		want    float64
	}{
		{name: "children overflow the box", size: 50, kids: []kid{{child(80), 0}}, want: 80},
		{name: "box taller than its children", size: 120, kids: []kid{{child(80), 0}}, want: 120},
		{name: "childless box", size: 40, want: 40},
		{name: "block-start border and padding", size: 50,
			boxData: &PhysicalBoxData{Border: PhysicalEdges{Top: 3}, Padding: PhysicalEdges{Top: 5}},
			kids:    []kid{{child(100), 0}}, want: 108},
		{name: "overflow:hidden clips", size: 50, style: []string{"overflow", "hidden"}, kids: []kid{{child(100), 0}}, want: 50},
		{name: "column keeps children only", size: 100, boxType: BoxTypeColumn, kids: []kid{{child(20), 10}}, want: 30},
		{name: "empty column", size: 100, boxType: BoxTypeColumn, want: 0},
		{name: "no block fragmentation", noFrag: true, size: 50, kids: []kid{{child(80), 0}}, want: 0},
	} {
		builder := NewBoxFragmentBuilder(wdm)
		builder.SetConstraintSpace(&ConstraintSpace{HasBlockFragmentation: !tc.noFrag})
		builder.SetBoxType(tc.boxType)
		builder.SetSize(LogicalSize{InlineSize: 50, BlockSize: tc.size})
		if tc.style != nil {
			builder.SetStyle(makeStyle(tc.style...))
		}
		if tc.boxData != nil {
			builder.SetBoxData(tc.boxData)
		}
		for _, k := range tc.kids {
			builder.AddChild(k.result.Fragment, LogicalOffset{BlockOffset: k.offset})
			builder.PropagateChildBlockSizeForFragmentation(k.result, LogicalOffset{BlockOffset: k.offset})
		}
		result := builder.Build()
		if got := result.BlockSizeForFragmentation; got != tc.want {
			t.Errorf("%s: BlockSizeForFragmentation = %v, want %v", tc.name, got, tc.want)
		}
		if result.Fragment.BoxType != tc.boxType {
			t.Errorf("%s: fragment BoxType = %v, want %v", tc.name, result.Fragment.BoxType, tc.boxType)
		}
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
