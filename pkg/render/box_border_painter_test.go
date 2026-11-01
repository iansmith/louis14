package render_test

import (
	"image/color"
	"math"
	"testing"
)

// LOU-552: BoxBorderPainter port (box_border_painter.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and RoundedInnerRectClipper
// for non-renderable background clips.

func assertPixel(t *testing.T, img interface{ At(x, y int) color.Color }, x, y int, want color.RGBA, why string) {
	t.Helper()
	r, g, b, _ := img.At(x, y).RGBA()
	got := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255}
	if got != want {
		t.Errorf("pixel (%d,%d) = %v, want %v: %s", x, y, got, want, why)
	}
}

var (
	black = color.RGBA{0, 0, 0, 255}
	white = color.RGBA{255, 255, 255, 255}
)

// A uniform double border paints two nested stripe rings
// (DrawDoubleBorder, :1238-1274), so the corner gap between the stripes
// stays empty: the inner stripe starts at the inner-third rect's corner,
// not at the box edge.
func TestBoxBorderPainter_DoubleBorderCornerGap(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="width:50px;height:50px;border:9px double black"></div></body></html>`, 100, 100)
	assertPixel(t, img, 1, 1, black, "outer stripe corner")
	assertPixel(t, img, 7, 7, black, "inner stripe corner")
	assertPixel(t, img, 7, 4, white, "gap between stripes in the corner (inner stripe must not run to the box edge)")
	assertPixel(t, img, 4, 7, white, "gap between stripes in the corner (inner stripe must not run to the box edge)")
}

// A non-renderable inner border (inner radius 25 on a 50x50 inner rect)
// takes the slow path: each side clips to its polygon and clips out the
// adjusted inner border (PaintOneBorderSide :1728-1783,
// CalculateAdjustedInnerBorder :170-260), which leaves no hole in the
// square bottom-right corner.
func TestBoxBorderPainter_NonRenderableInnerFillsSquareCorner(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="width:50px;height:50px;border:25px solid black;border-radius:100% 0 0 0"></div></body></html>`, 150, 150)
	assertPixel(t, img, 98, 98, black, "square bottom-right border corner")
	assertPixel(t, img, 90, 90, black, "bottom-right border corner")
	assertPixel(t, img, 2, 2, white, "outside the rounded top-left corner")
}

// A non-renderable background clip keeps its unconstrained inset radii
// and clips corner by corner (RoundedInnerRectClipper,
// rounded_inner_rect_clipper.cc:14-66): the content box (50,50)-(150,150)
// is clipped by a radius-150 arc centred on the border box's far corner
// (200,200), not by the radius-100 arc ConstrainRadii would give.
func TestRoundedInnerRectClipper_NonRenderableBackgroundClip(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="width:100px;height:100px;padding:25px;border:25px solid transparent;`+
		`border-radius:100% 0 0 0;background-color:black;background-clip:content-box"></div></body></html>`, 250, 250)
	assertPixel(t, img, 110, 70, white, "outside the radius-150 corner arc")
	assertPixel(t, img, 140, 140, black, "inside the content box")
	assertPixel(t, img, 149, 70, black, "inside the arc near the top edge")
}

// LOU-578: with anti-aliased fills, the border's inner edge is the exact
// complement of the padding-box background's anti-aliased clip
// (ClipOutContouredRect, box_border_painter.cc:1467-1469, and the
// background's ClipContouredRect — both kAntiAliased, graphics_context.h:
// 379-388 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A black border over
// a black padding-box background then leaves at most Skia's conflation
// seam (coverage c then 1-c composited Over: luminance ≤ 25%) along the
// inner curve — not a light seam where the border was erased from every
// pixel the inner rrect touches. The outer curve is anti-aliased.
func TestBoxBorderPainter_RoundedInnerEdgeAntialiasedWithoutSeam(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="width:20px;height:20px;padding:20px;border:20px solid black;`+
		`border-radius:100% 0 0 0;background-color:black;background-clip:padding-box"></div></body></html>`, 150, 150)
	// The top-left outer arc has radius 100 about (100,100); the inner
	// (padding-box) arc has radius 80 about the same centre.
	maxLum, sawPartialOuter := 0, false
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			lum := int(r >> 8)
			d := math.Hypot(float64(x)+0.5-100, float64(y)+0.5-100)
			if d < 98.5 && lum > maxLum {
				maxLum = lum
			}
			if d > 99 && d < 101 && lum > 0 && lum < 255 {
				sawPartialOuter = true
			}
		}
	}
	if maxLum > 80 {
		t.Errorf("max luminance inside the rounded border box = %d, want ≤ 80 (seam along the inner curve)", maxLum)
	}
	if !sawPartialOuter {
		t.Error("no anti-aliased (partially covered) pixel along the outer border curve")
	}
}
