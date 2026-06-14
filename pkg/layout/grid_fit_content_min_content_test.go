package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestGridFitContentMinContentFloor is the LOU-254 contract test.
//
// A fit-content(arg) track's used size is max(min-content, min(max-content, arg)).
// The base (min-content) side is NEVER clamped by the argument — only the growth
// limit (max-content) side is. Mirrors Blink's
// GridTrackSizingAlgorithm::ResolveIntrinsicTrackSizes /
// GrowthPotentialForSet (the fit-content branch only caps the growth limit) @
// third_party/blink/renderer/core/layout/grid/grid_track_sizing_algorithm.cc
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Reproduces wpt css-grid/child-border-box-and-max-content-002: two grid items
// (box-sizing:border-box; max-width:max-content; padding:10px 20px) each holding a
// fixed 50x50 block, in fit-content(30px)/fit-content(80px) columns. Each item's
// min-content == max-content == 50 (content) + 40 (padding) == 90, which exceeds
// both fit-content arguments, so both columns must resolve to 90px.
func TestGridFitContentMinContentFloor(t *testing.T) {
	content1 := makeNode("div")
	content2 := makeNode("div")
	item1 := makeNode("div", content1)
	item2 := makeNode("div", content2)
	grid := makeNode("div", item1, item2)

	gridStyle := css.NewStyle()
	gridStyle.Properties["display"] = "grid"
	gridStyle.Properties["grid-template-columns"] = "fit-content(30px) fit-content(80px)"
	gridStyle.Properties["width"] = "max-content"

	mkItem := func() *css.Style {
		s := css.NewStyle()
		s.Properties["max-width"] = "max-content"
		s.Properties["box-sizing"] = "border-box"
		s.Properties["padding"] = "10px 20px"
		return s
	}
	mkContent := func() *css.Style {
		s := css.NewStyle()
		s.Properties["width"] = "50px"
		s.Properties["height"] = "50px"
		return s
	}

	styles := map[*html.Node]*css.Style{
		grid:     gridStyle,
		item1:    mkItem(),
		item2:    mkItem(),
		content1: mkContent(),
		content2: mkContent(),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(grid, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	// (1) Container intrinsic width: width:max-content must resolve to two 90px
	// columns => 180 (measureGridMinMax / Bug B).
	mm := measureGridMinMax(layoutRoot, ctx, space)
	if mm.MaxContent != 180 {
		t.Errorf("measureGridMinMax.MaxContent: got %.1f, want 180 (two fit-content cols floored to min-content 90)", mm.MaxContent)
	}

	// (2) Actual track sizing: both grid items must be 90px wide and laid out at
	// x-offsets 0 and 90 (resolveTrackSizes / Bug A). Currently both collapse to 0.
	res := layoutElement(ctx, layoutRoot, space)
	frag := res.Fragment
	if len(frag.Children) != 2 {
		t.Fatalf("expected 2 grid-item children, got %d", len(frag.Children))
	}
	wantX := []float64{0, 90}
	for i, ch := range frag.Children {
		w := ch.Fragment.Size.WidthF64()
		x := ch.Offset.LeftF64()
		if w != 90 {
			t.Errorf("child[%d] width: got %.1f, want 90", i, w)
		}
		if x != wantX[i] {
			t.Errorf("child[%d] x-offset: got %.1f, want %.1f", i, x, wantX[i])
		}
	}
}
