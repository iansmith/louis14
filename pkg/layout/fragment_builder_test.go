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

// TestBlockSizeForFragmentation_FragmentainerIsRaw_LOU411: a fragmentainer's
// block-size for fragmentation is its children-only value; any other box's
// is at least its own block-size (fragmentation_utils.cc:1545-1583 with
// box_fragment_builder.cc:743-745 @ a9f50e522efa; louis14 records 0 for
// Blink's kIndefiniteSize "not calculated").
func TestBlockSizeForFragmentation_FragmentainerIsRaw_LOU411(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	result := func(boxType BoxType, h int, bsff float64) *LayoutResult {
		return &LayoutResult{
			Fragment:                  &PhysicalFragment{Size: geometry.NewPhysicalSize(50, h), BoxType: boxType},
			BlockSizeForFragmentation: bsff,
		}
	}
	for _, tc := range []struct {
		name   string
		result *LayoutResult
		want   float64
	}{
		{"column: children only", result(BoxTypeColumn, 100, 40), 40},
		{"box, not recorded: own size", result(BoxTypeNormal, 100, 0), 100},
		{"box with overflow", result(BoxTypeNormal, 100, 130), 130},
	} {
		if got := BlockSizeForFragmentation(wdm, tc.result); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// newFragmentationTestBuilder returns a builder laid out in space whose
// geometry has the given block border+scrollbar+padding (all in Border),
// with box-decoration-break:clone when cloned: what Blink's
// FragmentainerCapacity / FragmentainerOffset / FragmentainerSpaceLeft read
// off the builder (fragmentation_utils.h:153-257 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func newFragmentationTestBuilder(space *ConstraintSpace, edges LogicalEdges, cloned bool) *BoxFragmentBuilder {
	builder := NewBoxFragmentBuilder(WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR})
	builder.SetConstraintSpace(space)
	builder.SetInitialFragmentGeometry(&FragmentGeometry{Border: edges})
	if cloned {
		builder.SetStyle(makeStyle("box-decoration-break", "clone"))
	}
	return builder
}

// TestBoxFragmentBuilder_FragmentationAccessors (LOU-475): the builder
// exposes what Blink's fragmentation helpers read off BoxFragmentBuilder —
// BorderScrollbarPadding over the initial fragment geometry, cleared in
// place per edge (box_fragment_builder.h:298-312), PreviousBreakToken from
// the constraint space (:151-153), ShouldCloneBoxEndDecorations (:418-423),
// GetConstraintSpace and Style (fragment_builder.h:57-65), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestBoxFragmentBuilder_FragmentationAccessors(t *testing.T) {
	empty := NewBoxFragmentBuilder(WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR})
	if got := empty.BorderScrollbarPadding(); got != (LogicalEdges{}) {
		t.Errorf("no geometry: BorderScrollbarPadding = %v, want zero", got)
	}
	if empty.PreviousBreakToken() != nil || empty.GetConstraintSpace() != nil || empty.Style() != nil {
		t.Errorf("no space/style: want nil PreviousBreakToken, GetConstraintSpace and Style")
	}

	token := &BlockBreakToken{}
	space := ConstraintSpace{BreakToken: token}
	geom := FragmentGeometry{
		Border:    LogicalEdges{BlockStart: 1, BlockEnd: 2, InlineStart: 7},
		Scrollbar: LogicalEdges{BlockEnd: 3},
		Padding:   LogicalEdges{BlockStart: 4, BlockEnd: 5},
	}
	builder := NewBoxFragmentBuilder(WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR})
	builder.SetConstraintSpace(&space)
	builder.SetInitialFragmentGeometry(&geom)
	style := makeStyle("box-decoration-break", "clone")
	builder.SetStyle(style)
	if builder.GetConstraintSpace() != &space || builder.PreviousBreakToken() != token || builder.Style() != style {
		t.Errorf("GetConstraintSpace / PreviousBreakToken / Style do not return what was set")
	}
	if builder.InitialFragmentGeometry() != &geom {
		t.Errorf("InitialFragmentGeometry does not return the geometry set")
	}
	if got, want := builder.BorderScrollbarPadding(), (LogicalEdges{BlockStart: 5, BlockEnd: 10, InlineStart: 7}); got != want {
		t.Errorf("BorderScrollbarPadding = %v, want %v", got, want)
	}
	builder.ClearBorderScrollbarPaddingBlockStart()
	if got, want := builder.BorderScrollbarPadding(), (LogicalEdges{BlockEnd: 10, InlineStart: 7}); got != want {
		t.Errorf("after ClearBorderScrollbarPaddingBlockStart: %v, want %v", got, want)
	}
	builder.ClearBorderScrollbarPaddingBlockEnd()
	if got, want := builder.BorderScrollbarPadding(), (LogicalEdges{InlineStart: 7}); got != want {
		t.Errorf("after ClearBorderScrollbarPaddingBlockEnd: %v, want %v", got, want)
	}
	if builder.ShouldCloneBoxEndDecorations() {
		t.Errorf("ShouldCloneBoxEndDecorations defaults to true")
	}
	builder.SetShouldCloneBoxEndDecorations(true)
	if !builder.ShouldCloneBoxEndDecorations() {
		t.Errorf("SetShouldCloneBoxEndDecorations(true) not reflected")
	}
}

// TestFragmentainerHelpersReadBuilder (LOU-475): FragmentainerCapacity /
// FragmentainerOffset / FragmentainerSpaceLeft read the builder's style,
// BorderScrollbarPadding, ShouldCloneBoxEndDecorations and previous break
// token (fragmentation_utils.h:159-257 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Fragmentainer 100, offset 20,
// edges 10+5.
func TestFragmentainerHelpersReadBuilder(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		cloned, cloneEnd           bool
		token                      *BlockBreakToken
		wantCapChildren, wantCapIt float64
		wantOffChildren            float64
		wantLeftChildren           float64
	}{
		{"slice", false, false, nil, 100, 100, 20, 80},
		{"clone start only", true, false, nil, 90, 100, 10, 80},
		{"clone both ends", true, true, nil, 85, 100, 10, 75},
		{"clone past block end keeps offset", true, false, &BlockBreakToken{IsAtBlockEnd: true}, 90, 100, 20, 70},
	} {
		space := ConstraintSpace{HasBlockFragmentation: true, FragmentainerBlockSize: 100, FragmentainerOffset: 20, BreakToken: tc.token}
		builder := newFragmentationTestBuilder(&space, LogicalEdges{BlockStart: 10, BlockEnd: 5}, tc.cloned)
		builder.SetShouldCloneBoxEndDecorations(tc.cloneEnd)
		if got := FragmentainerCapacity(builder, true); got != tc.wantCapChildren {
			t.Errorf("%s: capacity for children = %v, want %v", tc.name, got, tc.wantCapChildren)
		}
		if got := FragmentainerCapacity(builder, false); got != tc.wantCapIt {
			t.Errorf("%s: capacity = %v, want %v", tc.name, got, tc.wantCapIt)
		}
		if got := FragmentainerOffset(builder, true); got != tc.wantOffChildren {
			t.Errorf("%s: offset for children = %v, want %v", tc.name, got, tc.wantOffChildren)
		}
		if got := FragmentainerOffset(builder, false); got != 20 {
			t.Errorf("%s: offset = %v, want 20", tc.name, got)
		}
		if got := FragmentainerSpaceLeft(builder, true); got != tc.wantLeftChildren {
			t.Errorf("%s: space left for children = %v, want %v", tc.name, got, tc.wantLeftChildren)
		}
	}
	// ClampedToValidFragmentainerCapacity(builder, ...): a box cloning its
	// block-end decorations fits them plus 1px (fragmentation_utils.h:159-173).
	space := ConstraintSpace{HasBlockFragmentation: true, FragmentainerBlockSize: 0}
	builder := newFragmentationTestBuilder(&space, LogicalEdges{BlockStart: 10, BlockEnd: 5}, true)
	if got := ClampedToValidFragmentainerCapacityForBuilder(builder, 0, false); got != 1 {
		t.Errorf("no end clone: clamp = %v, want 1", got)
	}
	builder.SetShouldCloneBoxEndDecorations(true)
	if got := ClampedToValidFragmentainerCapacityForBuilder(builder, 0, false); got != 16 {
		t.Errorf("end clone, fragment: clamp = %v, want 16", got)
	}
	if got := ClampedToValidFragmentainerCapacityForBuilder(builder, 0, true); got != 1 {
		t.Errorf("end clone, children: clamp = %v, want 1", got)
	}
	unknown := ConstraintSpace{HasBlockFragmentation: true, FragmentainerBlockSize: Indefinite}
	if got := FragmentainerSpaceLeft(newFragmentationTestBuilder(&unknown, LogicalEdges{}, false), true); got != Indefinite {
		t.Errorf("unknown fragmentainer: space left = %v, want Indefinite", got)
	}
}
