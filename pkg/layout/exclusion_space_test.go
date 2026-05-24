package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
	"testing"
)

func TestExclusionSpace_Empty(t *testing.T) {
	es := &ExclusionSpace{}
	if !es.IsEmpty() {
		t.Error("new ExclusionSpace should be empty")
	}

	start, end := es.FindAvailableInlineSize(0, 100, 800)
	if start != 0 || end != 0 {
		t.Errorf("empty space: got offsets (%v, %v), want (0, 0)", start, end)
	}
}

func TestExclusionSpace_NilSafe(t *testing.T) {
	var es *ExclusionSpace
	if !es.IsEmpty() {
		t.Error("nil ExclusionSpace should be empty")
	}

	start, end := es.FindAvailableInlineSize(0, 100, 800)
	if start != 0 || end != 0 {
		t.Errorf("nil space: got offsets (%v, %v), want (0, 0)", start, end)
	}

	ltrWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	cleared := es.ClearanceOffset(css.ClearBoth, layoutunit.New(50), ltrWDM)
	if cleared.Float64() != 50 {
		t.Errorf("nil clearance: got %v, want 50", cleared.Float64())
	}
}

func TestExclusionSpace_SingleLeftFloat(t *testing.T) {
	es := &ExclusionSpace{}
	es = es.Add(Exclusion{
		InlineOffset: 0,
		BlockOffset:  0,
		InlineSize:   100,
		BlockSize:    200,
		Side:         ExclusionInlineStart,
	})

	if es.IsEmpty() {
		t.Error("should not be empty after adding exclusion")
	}

	// At block=50 (within float range), left float occupies 100px inline.
	start, end := es.FindAvailableInlineSize(50, 10, 800)
	if start != 100 || end != 0 {
		t.Errorf("at block=50: got (%v, %v), want (100, 0)", start, end)
	}

	// At block=250 (past float), no offsets.
	start, end = es.FindAvailableInlineSize(250, 10, 800)
	if start != 0 || end != 0 {
		t.Errorf("at block=250: got (%v, %v), want (0, 0)", start, end)
	}
}

func TestExclusionSpace_SingleRightFloat(t *testing.T) {
	es := &ExclusionSpace{}
	es = es.Add(Exclusion{
		InlineOffset: 500, // positioned at inline=500 in a 600px container
		BlockOffset:  0,
		InlineSize:   100,
		BlockSize:    150,
		Side:         ExclusionInlineEnd,
	})

	// containerInlineSize=600, float at offset 500 → consumed from end = 600-500 = 100
	start, end := es.FindAvailableInlineSize(50, 10, 600)
	if start != 0 || end != 100 {
		t.Errorf("got (%v, %v), want (0, 100)", start, end)
	}
}

func TestExclusionSpace_BothSides(t *testing.T) {
	es := &ExclusionSpace{}
	es = es.Add(Exclusion{
		InlineOffset: 0, BlockOffset: 0,
		InlineSize: 80, BlockSize: 200,
		Side: ExclusionInlineStart,
	})
	es = es.Add(Exclusion{
		InlineOffset: 500, BlockOffset: 0,
		InlineSize: 120, BlockSize: 150,
		Side: ExclusionInlineEnd,
	})

	// containerInlineSize=620, float at offset 500 → consumed from end = 620-500 = 120
	start, end := es.FindAvailableInlineSize(50, 10, 620)
	if start != 80 || end != 120 {
		t.Errorf("both sides: got (%v, %v), want (80, 120)", start, end)
	}

	// Past right float but still in left float range.
	start, end = es.FindAvailableInlineSize(160, 10, 620)
	if start != 80 || end != 0 {
		t.Errorf("past right: got (%v, %v), want (80, 0)", start, end)
	}
}

func TestExclusionSpace_Clearance(t *testing.T) {
	es := &ExclusionSpace{}
	es = es.Add(Exclusion{
		InlineOffset: 0, BlockOffset: 10,
		InlineSize: 100, BlockSize: 90,
		Side: ExclusionInlineStart,
	})
	es = es.Add(Exclusion{
		InlineOffset: 500, BlockOffset: 20,
		InlineSize: 100, BlockSize: 130,
		Side: ExclusionInlineEnd,
	})

	// Tests use HTB-LTR: clear:left matches inline-start, clear:right matches inline-end.
	ltrWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	// Clear left: should clear past block=10+90=100.
	cleared := es.ClearanceOffset(css.ClearLeft, layoutunit.New(0), ltrWDM)
	if cleared.Float64() != 100 {
		t.Errorf("clear left: got %v, want 100", cleared.Float64())
	}

	// Clear right: should clear past block=20+130=150.
	cleared = es.ClearanceOffset(css.ClearRight, layoutunit.New(0), ltrWDM)
	if cleared.Float64() != 150 {
		t.Errorf("clear right: got %v, want 150", cleared.Float64())
	}

	// Clear both: max(100, 150) = 150.
	cleared = es.ClearanceOffset(css.ClearBoth, layoutunit.New(0), ltrWDM)
	if cleared.Float64() != 150 {
		t.Errorf("clear both: got %v, want 150", cleared.Float64())
	}

	// Clear left when already past: no change.
	cleared = es.ClearanceOffset(css.ClearLeft, layoutunit.New(200), ltrWDM)
	if cleared.Float64() != 200 {
		t.Errorf("clear left past: got %v, want 200", cleared.Float64())
	}
}

func TestExclusionSpace_FloatDrop(t *testing.T) {
	es := &ExclusionSpace{}
	// Left float occupying 400px of a 600px container, blocks 0-100.
	es = es.Add(Exclusion{
		InlineOffset: 0, BlockOffset: 0,
		InlineSize: 400, BlockSize: 100,
		Side: ExclusionInlineStart,
	})

	// A 300px float should fit beside the 400px float (600-400=200 < 300), so drops.
	pos := es.FindFloatPosition(css.FloatLeft, 300, 50, 600, 0)
	if pos != 100 {
		t.Errorf("float drop: got %v, want 100", pos)
	}

	// A 150px float should fit beside (200 >= 150), stays at 0.
	pos = es.FindFloatPosition(css.FloatLeft, 150, 50, 600, 0)
	if pos != 0 {
		t.Errorf("float fit: got %v, want 0", pos)
	}
}

func TestExclusionSpace_Immutable(t *testing.T) {
	es1 := &ExclusionSpace{}
	es2 := es1.Add(Exclusion{
		InlineOffset: 0, BlockOffset: 0,
		InlineSize: 100, BlockSize: 100,
		Side: ExclusionInlineStart,
	})

	// Original should be unchanged.
	if !es1.IsEmpty() {
		t.Error("original should still be empty")
	}
	if es2.IsEmpty() {
		t.Error("new space should not be empty")
	}
}

// TestBlockLayout_BFCChildAvoidsFloat verifies CSS 2.1 §9.5: a block-level
// child that establishes a new block formatting context (here via
// `display: flow-root`) is positioned beside a preceding left float, not
// underneath it. A plain in-flow block (no new BFC) would flow as if the
// float didn't exist; only line boxes inside it shorten. Mirrors Blink's
// BlockLayoutAlgorithm::LayoutNewFormattingContext path, where the new-FC
// child consults the parent's ExclusionSpace for an inline-offset shift.
func TestBlockLayout_BFCChildAvoidsFloat(t *testing.T) {
	floatChild := makeNode("div")
	blockChild := makeNode("div")
	parent := makeNode("div", floatChild, blockChild)

	styles := map[*html.Node]*css.Style{
		parent:     makeStyle("width", "600px", "display", "block"),
		floatChild: makeStyle("width", "200px", "height", "100px", "float", "left", "display", "block"),
		blockChild: makeStyle("height", "50px", "display", "flow-root"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if len(result.Fragment.Children) != 2 {
		t.Fatalf("children: got %d, want 2", len(result.Fragment.Children))
	}

	// Float should be at (0, 0) with size 200x100.
	floatFrag := result.Fragment.Children[0]
	if floatFrag.Fragment.Size.WidthF64() != 200 || floatFrag.Fragment.Size.HeightF64() != 100 {
		t.Errorf("float size: got %vx%v, want 200x100",
			floatFrag.Fragment.Size.WidthF64(), floatFrag.Fragment.Size.HeightF64())
	}
	if floatFrag.Offset.LeftF64() != 0 || floatFrag.Offset.TopF64() != 0 {
		t.Errorf("float offset: got (%v,%v), want (0,0)",
			floatFrag.Offset.LeftF64(), floatFrag.Offset.TopF64())
	}

	// BFC child should be shifted right by the float's width (inline offset = 200).
	blockFrag := result.Fragment.Children[1]
	if blockFrag.Offset.LeftF64() != 200 {
		t.Errorf("BFC child inline offset: got %v, want 200", blockFrag.Offset.LeftF64())
	}
	// BFC child should be at Y=0 (it fits alongside the float; no need to clear).
	if blockFrag.Offset.TopF64() != 0 {
		t.Errorf("BFC child Y: got %v, want 0", blockFrag.Offset.TopF64())
	}
}

func TestBlockLayout_FloatRight(t *testing.T) {
	floatChild := makeNode("div")
	parent := makeNode("div", floatChild)

	styles := map[*html.Node]*css.Style{
		parent:     makeStyle("width", "600px", "display", "block"),
		floatChild: makeStyle("width", "200px", "height", "100px", "float", "right", "display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	// Right float should be at inline = 600 - 200 = 400.
	floatFrag := result.Fragment.Children[0]
	if floatFrag.Offset.LeftF64() != 400 {
		t.Errorf("right float X: got %v, want 400", floatFrag.Offset.LeftF64())
	}

	// Parent auto height should include float (clear:both at end).
	if result.Fragment.Size.HeightF64() != 100 {
		t.Errorf("parent height: got %v, want 100", result.Fragment.Size.HeightF64())
	}
}

func TestBlockLayout_ClearBoth(t *testing.T) {
	floatChild := makeNode("div")
	clearChild := makeNode("div")
	parent := makeNode("div", floatChild, clearChild)

	clearStyle := makeStyle("height", "50px", "display", "block", "clear", "both")

	styles := map[*html.Node]*css.Style{
		parent:     makeStyle("width", "600px", "display", "block"),
		floatChild: makeStyle("width", "200px", "height", "100px", "float", "left", "display", "block"),
		clearChild: clearStyle,
	}

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	// Clear child should be pushed below the float (Y=100).
	clearFrag := result.Fragment.Children[1]
	if clearFrag.Offset.TopF64() != 100 {
		t.Errorf("clear child Y: got %v, want 100", clearFrag.Offset.TopF64())
	}
}
