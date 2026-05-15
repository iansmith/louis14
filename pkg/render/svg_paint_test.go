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
