package render

import (
	"image"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

func numberBox(n float64) css.BorderImageLengthBox {
	b := css.BorderImageLengthNumber(n)
	return css.BorderImageLengthBox{Top: b, Right: b, Bottom: b, Left: b}
}

func fixedSlices(px float64) css.LengthBox {
	l := css.Length{Fixed: px}
	return css.LengthBox{Top: l, Right: l, Bottom: l, Left: l}
}

// TestNinePieceImageGrid_OverlappingSlicesClampNotHalve: a 1x1 image with the
// default 100% slices clamps every slice to the image size (ComputeEdgeSlice,
// nine_piece_image_grid.cc:26-39 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// so all four corners draw and the edges/middle do not
// (nine_piece_image_grid_test.cc NinePieceImagePainter_DrawsNothing-style).
func TestNinePieceImageGrid_OverlappingSlicesClampNotHalve(t *testing.T) {
	p := css.Length{Percent: 100}
	n := css.NinePieceImage{ImageSlices: css.LengthBox{Top: p, Right: p, Bottom: p, Left: p}, BorderSlices: numberBox(1)}
	g := NewNinePieceImageGrid(n, geometry.SizeF{Width: 1, Height: 1}, geometry.PointF{X: 1, Y: 1}, 1,
		image.Rect(0, 0, 100, 100), [4]int{10, 10, 10, 10})
	for _, c := range []NinePiece{TopLeftPiece, TopRightPiece, BottomLeftPiece, BottomRightPiece} {
		if !g.GetNinePieceDrawInfo(c).IsDrawable {
			t.Errorf("corner %d not drawable", c)
		}
	}
	for _, e := range []NinePiece{TopPiece, BottomPiece, LeftPiece, RightPiece, MiddlePiece} {
		if g.GetNinePieceDrawInfo(e).IsDrawable {
			t.Errorf("piece %d drawable, want not", e)
		}
	}
}

// TestNinePieceImageGrid_ScaleDownWidths: widths whose sum exceeds the area
// are scaled by f = min(L/(W1+W2)) (nine_piece_image_grid.cc:130-148).
func TestNinePieceImageGrid_ScaleDownWidths(t *testing.T) {
	n := css.NinePieceImage{ImageSlices: fixedSlices(10), BorderSlices: numberBox(1)}
	g := NewNinePieceImageGrid(n, geometry.SizeF{Width: 30, Height: 30}, geometry.PointF{X: 1, Y: 1}, 1,
		image.Rect(0, 0, 100, 50), [4]int{50, 50, 50, 50})
	if g.Top.Width != 25 || g.Left.Width != 25 || g.Bottom.Width != 25 || g.Right.Width != 25 {
		t.Errorf("widths = %d %d %d %d, want 25 each", g.Top.Width, g.Right.Width, g.Bottom.Width, g.Left.Width)
	}
}

// TestComputeTileParameters_Space: n whole tiles with n+1 gaps and a leading
// gap (CalculateSpaceNeeded nine_piece_image_painter.cc:23-36 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). border-image-repeat-space-3:
// a 60px edge with 27px tiles → gap 2, tiles at 2 and 31.
func TestComputeTileParameters_Space(t *testing.T) {
	p, ok := computeTileParameters(css.SpaceImageRule, 60, 27)
	if !ok || p.phase != 2 || p.spacing != 2 || p.scaleFactor != 1 {
		t.Errorf("space(60, 27) = %+v ok=%v, want phase=spacing=2", p, ok)
	}
	if _, ok := computeTileParameters(css.SpaceImageRule, 26, 27); ok {
		t.Errorf("space(26, 27) ok, want no tiles")
	}
}

// TestComputeTileParameters_RepeatCentresOneTile: the repeat phase centres a
// single tile, (dst-src)/2 (nine_piece_image_painter.cc:55-60).
func TestComputeTileParameters_RepeatCentresOneTile(t *testing.T) {
	p, _ := computeTileParameters(css.RepeatImageRule, 100, 40)
	if p.phase != 30 || p.scaleFactor != 1 {
		t.Errorf("repeat(100, 40) = %+v, want phase 30", p)
	}
	r, _ := computeTileParameters(css.RoundImageRule, 100, 40)
	if want := 100.0 / 120; r.scaleFactor != want {
		t.Errorf("round(100, 40) scale = %v, want %v", r.scaleFactor, want)
	}
}
