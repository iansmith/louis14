package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// Pure BackgroundImageGeometry unit tests (LOU-531), against
// core/paint/background_image_geometry.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.

func boxCtx() *BoxBackgroundPaintContext { return &BoxBackgroundPaintContext{} }

func sz(w, h int) geometry.PhysicalSize { return geometry.NewPhysicalSize(w, h) }

func calcTile(t *testing.T, fl *css.FillLayer, info NaturalSizingInfo, area geometry.PhysicalSize) geometry.PhysicalSize {
	t.Helper()
	var g BackgroundImageGeometry
	g.calculateFillTileSize(fl, info, area, area)
	return g.TileSize()
}

// kCover grows the image to cover the snapped area even when one side of
// the area is zero (background-size-cover-003): 100x50 into 0x100 → 200x100.
// CalculateFillTileSize :485-516.
func TestCalculateFillTileSize_CoverZeroWidthArea(t *testing.T) {
	fl := &css.FillLayer{Size: css.BackgroundSize{Cover: true}}
	got := calcTile(t, fl, naturalSizingInfoForRaster(100, 50), sz(0, 100))
	if got != sz(200, 100) {
		t.Errorf("cover tile = %v, want 200x100", got)
	}
}

// An image with no natural size or ratio (orange-intrinsic-none.svg,
// background-size-043/044): `200px auto` resolves the auto height as 100%
// of the positioning area. CalculateFillTileSize :464-473.
func TestCalculateFillTileSize_AutoWithoutRatioIsHundredPercent(t *testing.T) {
	fl := &css.FillLayer{Size: css.BackgroundSize{Width: 200}}
	got := calcTile(t, fl, NaturalSizingInfo{}, sz(400, 300))
	if got != sz(200, 300) {
		t.Errorf("tile = %v, want 200x300", got)
	}
}

// `auto auto` on a generated image (gradient) is the positioning area —
// the default object size. CalculateFillTileSize :474-480.
func TestCalculateFillTileSize_GradientAutoIsArea(t *testing.T) {
	got := calcTile(t, &css.FillLayer{}, NaturalSizingInfo{}, sz(123, 45))
	if got != sz(123, 45) {
		t.Errorf("tile = %v, want 123x45", got)
	}
}

// background-repeat: space places floor(area/tile) tiles with equal gaps;
// with one tile only it degrades to no-repeat. GetSpaceBetweenImageTiles
// :20-31, CalculateRepeatAndPosition :596-605.
func TestCalculateRepeatAndPosition_Space(t *testing.T) {
	fl := &css.FillLayer{Repeat: css.BackgroundRepeat{X: css.BackgroundRepeatSpace, Y: css.BackgroundRepeatSpace}}
	var g BackgroundImageGeometry
	g.Calculate(fl, boxCtx(), geometry.NewPhysicalRect(0, 0, 100, 30), naturalSizingInfoForRaster(30, 20))
	// X: 3 tiles of 30 in 100 → gap 5. Y: one 20px tile in 30 → no-repeat.
	if w := g.SpaceSize().Width; w != geometry.NewPhysicalSize(5, 0).Width {
		t.Errorf("x spacing = %v, want 5", w.Float64())
	}
	if h := g.SnappedDestRect().Size.Height; h.Float64() != 20 {
		t.Errorf("no-repeat Y dest height = %v, want one 20px tile", h.Float64())
	}
}

// Repeat phase: a 30px tile positioned at x=10 puts a tile origin at 10,
// so the first origin left of the dest is -20. ComputePhase :834-842.
func TestComputePhase_RepeatPosition(t *testing.T) {
	fl := &css.FillLayer{
		Repeat:   css.BackgroundRepeat{X: css.BackgroundRepeatRepeat, Y: css.BackgroundRepeatRepeat},
		Position: css.BackgroundPosition{X: 10, XIsPixel: true, YIsPixel: true},
	}
	var g BackgroundImageGeometry
	g.Calculate(fl, boxCtx(), geometry.NewPhysicalRect(0, 0, 100, 100), naturalSizingInfoForRaster(30, 30))
	if got := g.ComputePhase().Left.Float64(); got != -20 {
		t.Errorf("phase x = %v, want -20", got)
	}
}
