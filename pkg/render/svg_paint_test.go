package render_test

import (
	"image"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

// renderToImage is a tiny harness that wires layout → render against
// a fresh in-memory RGBA so tests can inspect pixels without going
// through the on-disk PNG path. Mirrors how render.NewRendererForImage
// is used by the reftest harness.
func renderToImage(t *testing.T, htmlContent string, w, h int) *image.RGBA {
	t.Helper()
	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eng := layout.NewLayoutEngine(float64(w), float64(h))
	boxes := eng.Layout(doc)

	target := image.NewRGBA(image.Rect(0, 0, w, h))
	r := render.NewRendererForImage(target)
	r.Render(boxes)
	return target
}

// TestSVG_RedRectInsideSVG hand-verifies the Phase-2 painter: a
// <rect fill="red"> inside an inline <svg> should produce a red
// square at the rect's user-coords (10,10)–(90,90) on the page.
//
// The test is the smallest possible end-to-end exercise of the SVG
// pipeline: HTML parse → layout (SVGRootAlgorithm) → render
// (paintSelfForeground → paintSVGRoot → paintSVGNode → svgShapePainter
// → DrawContext.Fill). It pulls pixel samples at known offsets and
// fails if the fill didn't land.
func TestSVG_RedRectInsideSVG(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<rect x="10" y="10" width="80" height="80" fill="red"/>` +
		`</svg></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)

	// Sample inside the rect: (50, 50) should be red.
	rRed, gRed, bRed, aRed := img.At(50, 50).RGBA()
	if rRed>>8 < 200 || gRed>>8 > 50 || bRed>>8 > 50 || aRed == 0 {
		t.Errorf("at (50,50): got RGBA=(%d,%d,%d,%d), want approx red",
			rRed>>8, gRed>>8, bRed>>8, aRed>>8)
	}
	// Sample outside the rect but inside the <svg>: (5, 5) should
	// not be red (it's the SVG canvas — transparent/white).
	rOut, gOut, bOut, _ := img.At(5, 5).RGBA()
	if rOut>>8 > 200 && gOut>>8 < 100 && bOut>>8 < 100 {
		t.Errorf("at (5,5) outside rect: got RGBA=(%d,%d,%d), unexpectedly red",
			rOut>>8, gOut>>8, bOut>>8)
	}
}

// TestSVG_DefaultBlackFill: a <rect> with no fill attribute paints
// black (SVG default per SVG 1.1 §11.3).
func TestSVG_DefaultBlackFill(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="50" height="50">` +
		`<rect x="0" y="0" width="50" height="50"/>` +
		`</svg></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)
	rC, gC, bC, aC := img.At(25, 25).RGBA()
	if rC>>8 > 30 || gC>>8 > 30 || bC>>8 > 30 || aC == 0 {
		t.Errorf("default fill at (25,25): RGBA=(%d,%d,%d,%d), want approx black",
			rC>>8, gC>>8, bC>>8, aC>>8)
	}
}

// TestSVG_FillNoneSkipped: a <rect fill="none"> paints nothing —
// the area underneath remains the canvas background (white from
// the renderer's clear).
func TestSVG_FillNoneSkipped(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="50" height="50">` +
		`<rect x="0" y="0" width="50" height="50" fill="none"/>` +
		`</svg></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)
	rC, gC, bC, _ := img.At(25, 25).RGBA()
	if rC>>8 < 200 || gC>>8 < 200 || bC>>8 < 200 {
		t.Errorf("fill:none at (25,25): RGBA=(%d,%d,%d), expected near-white background",
			rC>>8, gC>>8, bC>>8)
	}
}

// TestSVG_GroupTransformTranslate: a <g transform="translate(20,30)">
// shifts the contained rect by (20, 30). With the rect's user-space
// box being (0,0)–(40,40), the on-page red region is (20,30)–(60,70).
// Sample inside that region (the rect's center post-translate at
// (40, 50)) and outside it (10, 10).
func TestSVG_GroupTransformTranslate(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<g transform="translate(20,30)">` +
		`<rect width="40" height="40" fill="red"/>` +
		`</g></svg></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)
	rR, gR, bR, aR := img.At(40, 50).RGBA()
	if rR>>8 < 200 || gR>>8 > 50 || bR>>8 > 50 || aR == 0 {
		t.Errorf("<g translate> center at (40,50): RGBA=(%d,%d,%d,%d), want red",
			rR>>8, gR>>8, bR>>8, aR>>8)
	}
	// Outside the translated rect — sample where the rect *was* before
	// translation; should be unpainted (white canvas).
	rO, gO, bO, _ := img.At(10, 10).RGBA()
	if rO>>8 > 200 && gO>>8 < 100 && bO>>8 < 100 {
		t.Errorf("outside translated rect at (10,10): RGBA=(%d,%d,%d), unexpectedly red",
			rO>>8, gO>>8, bO>>8)
	}
}

// TestSVG_NestedViewBox: a nested <svg> at (10,10) of size 40×40 with
// viewBox="0 0 1 1" maps its child <rect width=1 height=1 fill=red> to
// fill the entire 40×40 nested viewport. Sample (30, 30) — the center
// of the 40×40 nested area — should be red. Sample (5, 5) outside the
// nested viewport should be unpainted (canvas).
func TestSVG_NestedViewBox(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<svg x="10" y="10" width="40" height="40" viewBox="0 0 1 1">` +
		`<rect width="1" height="1" fill="red"/>` +
		`</svg></svg></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)
	rR, gR, bR, aR := img.At(30, 30).RGBA()
	if rR>>8 < 200 || gR>>8 > 50 || bR>>8 > 50 || aR == 0 {
		t.Errorf("nested viewport center at (30,30): RGBA=(%d,%d,%d,%d), want red",
			rR>>8, gR>>8, bR>>8, aR>>8)
	}
	rO, gO, bO, _ := img.At(5, 5).RGBA()
	if rO>>8 > 200 && gO>>8 < 100 && bO>>8 < 100 {
		t.Errorf("outside nested viewport at (5,5): RGBA=(%d,%d,%d), unexpectedly red",
			rO>>8, gO>>8, bO>>8)
	}
}

// TestSVG_CombinedTransforms: two nested <g> elements, an outer
// translate(50,50) and an inner rotate(45), rotate a 20×20 rect
// (centered at the local origin via x=-10 y=-10) by 45° around (50,
// 50). The rotated rect's center stays at (50, 50). A corner of the
// rotated square is √200 ≈ 14.14 px from the center, so sampling at
// (50, 64) should still be inside the rotated rect. Sampling (10, 10)
// is well outside.
func TestSVG_CombinedTransforms(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<g transform="translate(50,50)">` +
		`<g transform="rotate(45)">` +
		`<rect x="-10" y="-10" width="20" height="20" fill="red"/>` +
		`</g></g></svg></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)
	// Center: invariant under rotation around the center.
	rC, gC, bC, aC := img.At(50, 50).RGBA()
	if rC>>8 < 200 || gC>>8 > 50 || bC>>8 > 50 || aC == 0 {
		t.Errorf("rotated rect center at (50,50): RGBA=(%d,%d,%d,%d), want red",
			rC>>8, gC>>8, bC>>8, aC>>8)
	}
	// Sample 14 px below center — well inside the rotated rect (the
	// rect's local-y extends to ±10, but the rotated diagonal reaches
	// √200 ≈ 14.14 px from the center).
	rD, gD, bD, aD := img.At(50, 64).RGBA()
	if rD>>8 < 200 || gD>>8 > 50 || bD>>8 > 50 || aD == 0 {
		t.Errorf("rotated rect diagonal at (50,64): RGBA=(%d,%d,%d,%d), want red",
			rD>>8, gD>>8, bD>>8, aD>>8)
	}
	// Outside the rotated rect.
	rO, gO, bO, _ := img.At(10, 10).RGBA()
	if rO>>8 > 200 && gO>>8 < 100 && bO>>8 < 100 {
		t.Errorf("outside rotated rect at (10,10): RGBA=(%d,%d,%d), unexpectedly red",
			rO>>8, gO>>8, bO>>8)
	}
}
