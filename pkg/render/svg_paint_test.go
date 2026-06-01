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

// ---------------------------------------------------------------------------
// Phase 4 — paint server (gradient / pattern) hand-check tests
// ---------------------------------------------------------------------------
//
// These tests are the Phase 4 gate. They each render a small piece of
// SVG containing a `<linearGradient>`, `<radialGradient>`, or
// `<pattern>` resource element referenced via `fill="url(#…)"`, and
// sample pixels at specific user-space locations to verify the paint
// server resolves correctly and pixel-aligns with the expected ramp.
//
// Tolerance: each per-channel expected value carries a ±2 cushion to
// absorb rounding noise from the sub-pixel rasterizer and the
// straight-alpha ↔ premultiplied-alpha round trip the gradient stop
// interpolation performs.

// sampleColorClose reports whether the pixel at (x, y) in img matches
// the given expected RGBA (8-bit) within the given per-channel
// tolerance. Helper for the gradient hand-checks below.
func sampleColorClose(t *testing.T, img *image.RGBA, x, y int, wantR, wantG, wantB uint8, tol int, where string) {
	t.Helper()
	r, g, b, a := img.At(x, y).RGBA()
	gotR := int(r >> 8)
	gotG := int(g >> 8)
	gotB := int(b >> 8)
	if abs(gotR-int(wantR)) > tol || abs(gotG-int(wantG)) > tol || abs(gotB-int(wantB)) > tol {
		t.Errorf("%s at (%d,%d): got RGBA=(%d,%d,%d,%d), want approx (%d,%d,%d) ±%d",
			where, x, y, gotR, gotG, gotB, a>>8, wantR, wantG, wantB, tol)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// TestSVG_LinearGradientObjectBBox: the most basic linear gradient,
// black→white across a 100×100 rect with default `gradientUnits=
// objectBoundingBox` and default endpoints x1=0,y1=0,x2=1,y2=0
// (horizontal). Verifies left edge ≈ black, right edge ≈ white,
// midpoint ≈ mid-gray.
func TestSVG_LinearGradientObjectBBox(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<linearGradient id="lg">` +
		`<stop offset="0" stop-color="black"/>` +
		`<stop offset="1" stop-color="white"/>` +
		`</linearGradient>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#lg)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Left edge: black.
	sampleColorClose(t, img, 2, 50, 5, 5, 5, 8, "left edge")
	// Right edge: white.
	sampleColorClose(t, img, 97, 50, 250, 250, 250, 8, "right edge")
	// Midpoint: mid-gray (~127).
	sampleColorClose(t, img, 50, 50, 127, 127, 127, 8, "midpoint")
}

// TestSVG_LinearGradientUserSpaceOnUse: explicit gradient endpoints in
// user-space coords, gradientUnits=userSpaceOnUse. The endpoints define
// a horizontal ramp from (10, 0) → (90, 0) — same visual effect as the
// objectBBox case at this rect's geometry, but the resolution path
// goes through the user-space length code.
func TestSVG_LinearGradientUserSpaceOnUse(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<linearGradient id="lg" x1="10" y1="0" x2="90" y2="0" gradientUnits="userSpaceOnUse">` +
		`<stop offset="0" stop-color="black"/>` +
		`<stop offset="1" stop-color="white"/>` +
		`</linearGradient>` +
		`</defs>` +
		`<rect x="10" y="10" width="80" height="80" fill="url(#lg)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// At user-space x=10 (just inside the gradient start): black.
	sampleColorClose(t, img, 12, 50, 5, 5, 5, 12, "left start")
	// At user-space x=90 (just inside the gradient end): white.
	sampleColorClose(t, img, 88, 50, 250, 250, 250, 12, "right end")
	// At user-space x=50 (midpoint): mid-gray.
	sampleColorClose(t, img, 50, 50, 127, 127, 127, 12, "midpoint")
}

// TestSVG_LinearGradientWithTransform: a gradientTransform="rotate(90)"
// rotates the gradient line by 90°, turning the default horizontal
// (left→right) ramp into a vertical (top→bottom) ramp. Sample at the
// top/bottom edges and the center.
func TestSVG_LinearGradientWithTransform(t *testing.T) {
	// rotate(90) maps (1,0) → (0,1). Applied AFTER the objectBBox
	// mapping (translate(0,0) ∘ scale(100,100)), the line direction
	// becomes (0, 100) in user space — vertical ramp.
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<linearGradient id="lg" gradientTransform="rotate(90 0.5 0.5)">` +
		`<stop offset="0" stop-color="black"/>` +
		`<stop offset="1" stop-color="white"/>` +
		`</linearGradient>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#lg)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Top edge: black.
	sampleColorClose(t, img, 50, 2, 5, 5, 5, 12, "top edge")
	// Bottom edge: white.
	sampleColorClose(t, img, 50, 97, 250, 250, 250, 12, "bottom edge")
	// Center: mid-gray.
	sampleColorClose(t, img, 50, 50, 127, 127, 127, 12, "center")
}

// TestSVG_RadialGradient: default objectBBox radial gradient with
// center at (0.5, 0.5), r=0.5. Black at center, white at the edge of
// the enclosing circle.
func TestSVG_RadialGradient(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<radialGradient id="rg">` +
		`<stop offset="0" stop-color="black"/>` +
		`<stop offset="1" stop-color="white"/>` +
		`</radialGradient>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#rg)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Center: black.
	sampleColorClose(t, img, 50, 50, 5, 5, 5, 8, "center")
	// Edge (distance ~r from center, along x axis): white.
	sampleColorClose(t, img, 98, 50, 250, 250, 250, 12, "right edge")
}

// TestSVG_GradientFallbackColor: a `fill="url(#missing) red"` paints
// red because the referenced gradient doesn't exist (SVG 2
// fallback-color syntax). This exercises the paint-server-not-found
// branch in resolvePaint.
func TestSVG_GradientFallbackColor(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="50" height="50">` +
		`<rect width="50" height="50" fill="url(#missing) red"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Sample the rect center — should be opaque red.
	sampleColorClose(t, img, 25, 25, 255, 0, 0, 8, "fallback red")
}

// TestSVG_GradientNoneIfNotFound: a `fill="url(#missing)"` with no
// fallback paints nothing — the page background (white from the
// renderer's clear) shows through. SVG 2 §13.1 "treat as if `fill: none`
// was specified" branch.
func TestSVG_GradientNoneIfNotFound(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="50" height="50">` +
		`<rect width="50" height="50" fill="url(#missing)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Sample the rect center — should be the canvas background
	// (white). Tolerance is wider here because the canvas is a clear
	// 255/255/255/255 RGBA, with no anti-aliasing concerns.
	sampleColorClose(t, img, 25, 25, 255, 255, 255, 4, "no paint")
}

// TestSVG_GradientStopOpacity: a stop with stop-opacity=0.5 produces
// a semi-transparent ramp at that offset. With a white canvas
// underneath, the midpoint of a red→red 50%-opacity ramp should be
// approximately rgb(255, 127, 127) (red blended with white at 50%
// coverage).
func TestSVG_GradientStopOpacity(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<linearGradient id="lg">` +
		`<stop offset="0" stop-color="red" stop-opacity="1"/>` +
		`<stop offset="1" stop-color="red" stop-opacity="0"/>` +
		`</linearGradient>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#lg)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Left edge: fully opaque red.
	sampleColorClose(t, img, 2, 50, 255, 0, 0, 8, "left opaque red")
	// Midpoint: ~50% red over white background → (255, 127, 127).
	sampleColorClose(t, img, 50, 50, 255, 127, 127, 12, "midpoint")
	// Right edge: fully transparent → background (white).
	sampleColorClose(t, img, 97, 50, 255, 255, 255, 8, "right transparent")
}

// TestSVG_SpreadMethodPad: spreadMethod=pad (the default) clamps the
// gradient at its endpoints. With a gradient line covering only the
// middle half of the rect (x1=25%, x2=75%) the left third stays at the
// first stop's color and the right third at the last stop's color.
func TestSVG_SpreadMethodPad(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<linearGradient id="lg" x1="0.25" x2="0.75" spreadMethod="pad">` +
		`<stop offset="0" stop-color="black"/>` +
		`<stop offset="1" stop-color="white"/>` +
		`</linearGradient>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#lg)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Left edge: still black (clamped at first stop).
	sampleColorClose(t, img, 5, 50, 5, 5, 5, 8, "left pad clamp")
	// Right edge: white (clamped at last stop).
	sampleColorClose(t, img, 95, 50, 250, 250, 250, 8, "right pad clamp")
	// Midpoint of gradient line: mid-gray.
	sampleColorClose(t, img, 50, 50, 127, 127, 127, 12, "gradient midpoint")
}

// TestSVG_SpreadMethodReflect: outside the [0,1] range the gradient
// mirrors. With a gradient covering only the first 50% of the rect,
// reflect mirrors the second 50% so the rightmost edge shows the
// FIRST stop's color (the reflection brings t back to ~0).
func TestSVG_SpreadMethodReflect(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<linearGradient id="lg" x1="0" x2="0.5" spreadMethod="reflect">` +
		`<stop offset="0" stop-color="black"/>` +
		`<stop offset="1" stop-color="white"/>` +
		`</linearGradient>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#lg)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Far right edge: t=2 → reflects to t=0 → first-stop color (black).
	sampleColorClose(t, img, 97, 50, 5, 5, 5, 12, "right reflect to black")
	// At 50% (gradient end): white.
	sampleColorClose(t, img, 48, 50, 250, 250, 250, 12, "gradient end white")
	// At 75% (reflection midpoint): mid-gray.
	sampleColorClose(t, img, 75, 50, 127, 127, 127, 18, "reflect midpoint")
}

// TestSVG_SpreadMethodRepeat: outside the [0,1] range the gradient
// tiles. With a gradient covering the first 50%, repeat makes the
// second 50% an identical copy: t=0.75 wraps to t=0.5 → still
// midway → mid-gray. The far right edge wraps to t=1 → white.
func TestSVG_SpreadMethodRepeat(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<linearGradient id="lg" x1="0" x2="0.5" spreadMethod="repeat">` +
		`<stop offset="0" stop-color="black"/>` +
		`<stop offset="1" stop-color="white"/>` +
		`</linearGradient>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#lg)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// At x=25 (t=0.5 → mid-gray).
	sampleColorClose(t, img, 25, 50, 127, 127, 127, 12, "tile-1 midpoint")
	// At x=75 (t=1.5 → mod 1 = 0.5 → mid-gray again).
	sampleColorClose(t, img, 75, 50, 127, 127, 127, 12, "tile-2 midpoint")
	// At x=2 (t≈0.04 → near black).
	sampleColorClose(t, img, 2, 50, 10, 10, 10, 12, "tile-1 start")
	// At x=52 (t≈1.04 → mod 1 ≈ 0.04 → near black again — seam).
	sampleColorClose(t, img, 52, 50, 10, 10, 10, 18, "tile-2 start")
}

// TestSVG_Pattern: a simple 10×10 pattern containing a 10×10 black
// rectangle, applied to a 100×100 white-background rect. The pattern
// uses patternUnits=userSpaceOnUse so coordinates are in user units.
// Sample any point inside the 100×100 rect — should be black (the
// entire tile is filled, so every tile pixel is black).
func TestSVG_Pattern(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<pattern id="p" x="0" y="0" width="10" height="10" patternUnits="userSpaceOnUse">` +
		`<rect width="10" height="10" fill="black"/>` +
		`</pattern>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#p)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Sample a few points — all should be black because every tile
	// pixel is black.
	sampleColorClose(t, img, 5, 5, 5, 5, 5, 12, "tile (0,0)")
	sampleColorClose(t, img, 15, 25, 5, 5, 5, 12, "tile (10,20)")
	sampleColorClose(t, img, 95, 95, 5, 5, 5, 12, "tile (90,90)")
}

// ---------------------------------------------------------------------------
// Phase 5 — <clipPath> + <mask> hand-check tests
// ---------------------------------------------------------------------------
//
// These exercise the SVG clip / mask resource pipeline added in Phase 5:
//   - SVGResourceClipper.AsPath fast path (shape-based clippers).
//   - clipPathUnits userSpaceOnUse + objectBoundingBox.
//   - SVGResourceMasker.Rasterize with mask-type luminance / alpha.
//
// Each test renders a minimal SVG/HTML snippet and samples pixels
// inside / outside the clip or mask region. Tolerances allow for
// edge-anti-aliasing rounding noise.

// TestSVG_ClipPathShapeRect — `<clipPath><rect/></clipPath>` on a
// 100×100 red rect. The clip's child rect at (20,20,60,60) restricts
// the visible region to that inner sub-rect; pixels inside the clip
// stay red, pixels outside are clipped away (canvas white).
func TestSVG_ClipPathShapeRect(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<clipPath id="c">` +
		`<rect x="20" y="20" width="60" height="60"/>` +
		`</clipPath>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" clip-path="url(#c)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Inside the clip — visible red.
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 8, "clip interior (50,50)")
	// Outside the clip rect (top-left corner of element, outside clip).
	sampleColorClose(t, img, 10, 10, 255, 255, 255, 8, "outside clip (10,10)")
	// Outside the clip rect (bottom-right corner).
	sampleColorClose(t, img, 90, 90, 255, 255, 255, 8, "outside clip (90,90)")
}

// TestSVG_ClipPathCircle — `<clipPath><circle/></clipPath>` on a
// 100×100 red rect. Inside the circle stays red; outside is clipped.
func TestSVG_ClipPathCircle(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<clipPath id="c">` +
		`<circle cx="50" cy="50" r="30"/>` +
		`</clipPath>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" clip-path="url(#c)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Center — inside the circle.
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 8, "circle center (50,50)")
	// Just inside the bottom of the circle (radius 30, sample at y=79).
	sampleColorClose(t, img, 50, 78, 255, 0, 0, 30, "circle near edge (50,78)")
	// Just outside the top of the circle (radius 30 → top at y=20;
	// sample at y=15).
	sampleColorClose(t, img, 50, 15, 255, 255, 255, 8, "outside top (50,15)")
	// Corner — well outside.
	sampleColorClose(t, img, 5, 5, 255, 255, 255, 8, "corner (5,5)")
}

// TestSVG_ClipPathUserSpaceOnUse — explicit `clipPathUnits=userSpaceOnUse`
// produces the same result as the default (which is userSpaceOnUse).
// This is a sanity check that the units attribute is parsed without
// breaking the default path.
func TestSVG_ClipPathUserSpaceOnUse(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<clipPath id="c" clipPathUnits="userSpaceOnUse">` +
		`<rect x="20" y="20" width="60" height="60"/>` +
		`</clipPath>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" clip-path="url(#c)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 8, "clip interior (50,50)")
	sampleColorClose(t, img, 10, 10, 255, 255, 255, 8, "outside clip (10,10)")
}

// TestSVG_ClipPathObjectBBox — `clipPathUnits=objectBoundingBox`
// makes the child rect coords fractions of the referencing element's
// bbox. Children `<rect x="0.25" y="0.25" width="0.5" height="0.5"/>`
// over a 100×100 element produces a clip at user-coords (25..75,
// 25..75) — same final geometry as the userSpaceOnUse case at
// (25,25,50,50).
func TestSVG_ClipPathObjectBBox(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<clipPath id="c" clipPathUnits="objectBoundingBox">` +
		`<rect x="0.25" y="0.25" width="0.5" height="0.5"/>` +
		`</clipPath>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" clip-path="url(#c)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 8, "bbox clip center (50,50)")
	sampleColorClose(t, img, 10, 50, 255, 255, 255, 8, "bbox clip left-of (10,50)")
	sampleColorClose(t, img, 90, 50, 255, 255, 255, 8, "bbox clip right-of (90,50)")
}

// TestSVG_MaskLuminance — `<mask><rect fill="white"/></mask>` on a
// red rect. White luminance = full mask alpha = element shows
// unmodified. Sample the center: red.
func TestSVG_MaskLuminance(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<mask id="m" maskContentUnits="userSpaceOnUse">` +
		`<rect width="100" height="100" fill="white"/>` +
		`</mask>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" mask="url(#m)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 12, "white mask (50,50)")
}

// TestSVG_MaskLuminanceBlack — `<mask><rect fill="black"/></mask>` on
// a red rect. Black luminance = 0 mask alpha = element fully masked.
// Sample the center: canvas white (since the red is gated out).
func TestSVG_MaskLuminanceBlack(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<mask id="m" maskContentUnits="userSpaceOnUse">` +
		`<rect width="100" height="100" fill="black"/>` +
		`</mask>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" mask="url(#m)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	sampleColorClose(t, img, 50, 50, 255, 255, 255, 8, "black mask (50,50)")
}

// TestSVG_MaskAlpha — `mask-type=alpha` uses the rendered mask
// subtree's alpha as the mask value. A half-transparent white inside
// the mask should pass through the red rect at 50% alpha:
// rgba(255*0.5, 0, 0, 0.5) over white = (128+127, 127, 127) ≈
// (255, 127, 127).
func TestSVG_MaskAlpha(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<mask id="m" mask-type="alpha" maskContentUnits="userSpaceOnUse">` +
		`<rect width="100" height="100" fill="white" fill-opacity="0.5"/>` +
		`</mask>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" mask="url(#m)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// 50% red over white: r = 255*0.5 + 255*0.5 = 255; g = 0*0.5 +
	// 255*0.5 = 127; b = 0*0.5 + 255*0.5 = 127.
	sampleColorClose(t, img, 50, 50, 255, 127, 127, 14, "alpha mask (50,50)")
}

// TestSVG_CyclicMaskReference — Phase 6 cycle detection. A `<mask>`
// whose subtree references itself (mask="url(#m1)") must NOT cause the
// painter to recurse forever. The cycle-flagged mask is treated as
// `none`, so the referencing red rect paints as if unmasked.
func TestSVG_CyclicMaskReference(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<mask id="m1" maskContentUnits="userSpaceOnUse">` +
		`<rect width="100" height="100" fill="white" mask="url(#m1)"/>` +
		`</mask>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" mask="url(#m1)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Cycle → mask dropped → red rect paints unmasked. Center is red.
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 12, "self-cyclic mask (50,50)")
}

// TestSVG_MutualMaskCycle — Phase 6 cycle detection. Two masks each
// reference the other through their subtree's `mask` attribute; both
// must be flagged HasCycle and treated as `none`. The referencing red
// rect paints unmasked.
func TestSVG_MutualMaskCycle(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<mask id="ma" maskContentUnits="userSpaceOnUse">` +
		`<rect width="100" height="100" fill="white" mask="url(#mb)"/>` +
		`</mask>` +
		`<mask id="mb" maskContentUnits="userSpaceOnUse">` +
		`<rect width="100" height="100" fill="white" mask="url(#ma)"/>` +
		`</mask>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" mask="url(#ma)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 12, "mutual mask cycle (50,50)")
}

// TestSVG_CyclicClipPath — Phase 6 cycle detection. A `<clipPath>`
// whose child carries `clip-path="url(#c1)"` self-references; the
// painter must not infinite-loop. The cycle-flagged clipper is
// treated as `none` (no clipping applied), so the referencing rect
// paints fully.
func TestSVG_CyclicClipPath(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<clipPath id="c1" clipPathUnits="userSpaceOnUse">` +
		`<rect width="100" height="100" clip-path="url(#c1)"/>` +
		`</clipPath>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" clip-path="url(#c1)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Cycle → no clipping → red rect paints fully.
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 12, "self-cyclic clip-path (50,50)")
}

// TestSVG_ResourceLookupAfterRegistryMerge — sanity check that the
// Phase 6 umbrella-registry refactor still resolves a `<linearGradient>`
// declared in the same inline `<svg>` and referenced by a sibling shape
// via `fill="url(#g)"`. Verifies the single-map registry's
// LookupAsPaintServer path is wired correctly.
func TestSVG_ResourceLookupAfterRegistryMerge(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<linearGradient id="g" gradientUnits="objectBoundingBox" x1="0" y1="0" x2="1" y2="0">` +
		`<stop offset="0" stop-color="black"/>` +
		`<stop offset="1" stop-color="white"/>` +
		`</linearGradient>` +
		`</defs>` +
		`<rect width="100" height="100" fill="url(#g)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Left edge ≈ black, right edge ≈ white (same expectation as the
	// pre-Phase-6 LinearGradientObjectBBox test, just routed through
	// the umbrella Lookup).
	sampleColorClose(t, img, 5, 50, 0, 0, 0, 18, "gradient left (5,50)")
	sampleColorClose(t, img, 95, 50, 255, 255, 255, 18, "gradient right (95,50)")
}

// TestSVG_FilterFeFlood — Phase 7 gate: a `<feFlood flood-color="green"/>`
// inside a filter overrides the rect's red fill with green pixels in
// the filter region. Mirrors the spec-canonical scenario:
//
//	<filter id="f"><feFlood flood-color="green"/></filter>
//	<rect fill="red" filter="url(#f)"/>
//
// Sample interior should be green, with no red surviving the filter.
// TestSVG_FilterFloodOnEmptyHiddenForeignObject mirrors the WPT reftest
// svg-empty-hidden-foreignobject-with-filter-001: a foreignObject with
// zero size and visibility:hidden carries a flood filter that has its
// own explicit userSpaceOnUse region. Per CSS Filter Effects 1 §7 the
// filter applies regardless of the source bbox or visibility — the
// flood output must render at the filter's region.
func TestSVG_FilterFloodOnEmptyHiddenForeignObject(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="200" height="200">` +
		`<defs>` +
		`<filter id="f" x="0" y="0" width="100" height="100" filterUnits="userSpaceOnUse">` +
		`<feFlood flood-color="green" flood-opacity="1"/>` +
		`</filter>` +
		`</defs>` +
		`<foreignObject style="visibility: hidden;" x="0" y="0" width="0" height="0" filter="url(#f)"></foreignObject>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Filter region is (0, 0, 100, 100). Center of that region must be green.
	sampleColorClose(t, img, 50, 50, 0, 128, 0, 12, "flood center")
	sampleColorClose(t, img, 10, 10, 0, 128, 0, 12, "flood corner")
	// Outside the filter region the canvas should be white (the SVG
	// viewport background).
	sampleColorClose(t, img, 150, 150, 255, 255, 255, 2, "outside flood region")
}

func TestSVG_FilterFeFlood(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<filter id="f" x="0" y="0" width="1" height="1">` +
		`<feFlood flood-color="green"/>` +
		`</filter>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" filter="url(#f)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Center should be green (0,128,0 = CSS "green"), not red.
	sampleColorClose(t, img, 50, 50, 0, 128, 0, 12, "feFlood center")
	// Corner inside filter region should also be green.
	sampleColorClose(t, img, 10, 10, 0, 128, 0, 12, "feFlood corner")
}

// TestSVG_FilterFeFloodWithOpacity — `<feFlood flood-color="red"
// flood-opacity="0.5"/>` produces a half-transparent red flood. The
// filter's output is composited onto the page via the louis14
// straight-alpha-into-premultiplied-container convention (same shim
// the SVG mask path uses); on a white background this produces a
// mid-gray rather than the "correct" premultiplied-math (255,127,127).
// Mirrors the well-known louis14 color-convention quirk — exhibits
// pixel parity with the corresponding CSS-rendered ref.
func TestSVG_FilterFeFloodWithOpacity(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<filter id="f" x="0" y="0" width="1" height="1">` +
		`<feFlood flood-color="red" flood-opacity="0.5"/>` +
		`</filter>` +
		`</defs>` +
		`<rect width="100" height="100" fill="blue" filter="url(#f)"/>` +
		`</svg></body></html>`
	// The reference rect uses the same color convention — `rgba(255,
	// 0, 0, 0.5)` rendered via the gg pattern path. Sample the test
	// against an equivalent CSS-rendered reference.
	const refHTML = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<rect width="100" height="100" fill="red" fill-opacity="0.5"/>` +
		`</svg></body></html>`
	testImg := renderToImage(t, htmlContent, 200, 200)
	refImg := renderToImage(t, refHTML, 200, 200)
	// Match the CSS-path pixel value at the sample point — confirms
	// the filter path uses the same straight-alpha convention.
	rR, rG, rB, _ := refImg.At(50, 50).RGBA()
	sampleColorClose(t, testImg, 50, 50,
		uint8(rR>>8), uint8(rG>>8), uint8(rB>>8), 5,
		"feFlood 50% pixel-matches CSS rgba(255,0,0,0.5)")
}

// TestSVG_FilterFeGaussianBlur — a simple Gaussian blur softens a
// sharp-edged shape. Interior pixels stay near saturated; edge pixels
// fall to a mid-value.
func TestSVG_FilterFeGaussianBlur(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="200" height="200">` +
		`<defs>` +
		`<filter id="b">` +
		`<feGaussianBlur stdDeviation="4"/>` +
		`</filter>` +
		`</defs>` +
		`<rect x="50" y="50" width="100" height="100" fill="red" filter="url(#b)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 300, 300)
	// Center of the rect: should be approximately solid red after
	// blurring (the interior pixels' neighbourhoods are all red).
	sampleColorClose(t, img, 100, 100, 255, 0, 0, 60, "blur interior")
	// A pixel just outside the original edge: should be neither
	// fully red nor fully white — the blur halo. The page background
	// is white, so a fully-saturated red blur would composite to
	// pure (255, 0, 0) and a no-filter pass would leave pure
	// (255, 255, 255). A halo lands somewhere in between, so the
	// GREEN channel is the right discriminator: pure red → G=0,
	// pure white → G=255, partial halo → G mid-range.
	_, gE, _, _ := img.At(155, 100).RGBA()
	gotE := int(gE >> 8)
	if gotE < 20 || gotE > 240 {
		t.Errorf("blur edge halo at (155,100): green=%d, expected mid range (blur halo present)", gotE)
	}
}

// TestSVG_FilterCycleReference — two filters that reference each
// other form a cycle; the cycle solver flags both as having a cycle,
// and the painter treats `filter: url(#f1)` as `none`. Confirms no
// infinite loop and that the rect renders unfiltered.
//
// This test arranges the cycle via two filter elements whose own
// styles contain `filter: url(#other)` — the only style-level edge
// outgoingReferences currently walks for filters.
func TestSVG_FilterCycleReference(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<defs>` +
		`<filter id="f1" style="filter:url(#f2)"><feFlood flood-color="green"/></filter>` +
		`<filter id="f2" style="filter:url(#f1)"><feFlood flood-color="blue"/></filter>` +
		`</defs>` +
		`<rect width="100" height="100" fill="red" filter="url(#f1)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Cycle → filter treated as none → the rect renders as plain red.
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 12, "cycle fallback (red)")
}

// TestSVG_FilterPositioningWithP — Same as Simple but with a `<p>`
// element before the SVG, matching the structure of the
// svg-feflood-001 reftest. This reproduces the layout shift seen in
// the WPT gate.
func TestSVG_FilterPositioningWithP(t *testing.T) {
	const tHTML = `<!DOCTYPE html><html><head><style>svg{width:500px;height:500px}</style></head><body>` +
		`<p>X</p>` +
		`<svg>` +
		`<defs><filter id="f" x="0" y="0" width="1" height="1"><feFlood flood-color="black"/></filter></defs>` +
		`<rect width="300" height="300" fill="red" filter="url(#f)"/>` +
		`</svg></body></html>`
	const rHTML = `<!DOCTYPE html><html><head><style>svg{width:500px;height:500px}</style></head><body>` +
		`<p>X</p>` +
		`<svg>` +
		`<rect width="300" height="300" fill="black"/>` +
		`</svg></body></html>`
	testImg := renderToImage(t, tHTML, 800, 600)
	refImg := renderToImage(t, rHTML, 800, 600)
	// Sample at several rect positions. Both should produce black.
	for _, p := range []struct{ x, y int }{
		{50, 50},
		{100, 100},
		{200, 200},
		{250, 100},
	} {
		tR, tG, tB, _ := testImg.At(p.x, p.y).RGBA()
		rR, rG, rB, _ := refImg.At(p.x, p.y).RGBA()
		if (tR>>8 != rR>>8) || (tG>>8 != rG>>8) || (tB>>8 != rB>>8) {
			t.Errorf("(%d,%d): test=(%d,%d,%d) ref=(%d,%d,%d)",
				p.x, p.y, tR>>8, tG>>8, tB>>8, rR>>8, rG>>8, rB>>8)
		}
	}
}

// TestSVG_FilterPositioningSimple — Compare a filter-applied rect at
// (50,50,100,100) with the same rect with no filter. They should
// land at the same x,y.
func TestSVG_FilterPositioningSimple(t *testing.T) {
	const tHTML = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="200" height="200">` +
		`<defs><filter id="f" x="0" y="0" width="1" height="1"><feFlood flood-color="green"/></filter></defs>` +
		`<rect x="50" y="50" width="100" height="100" fill="red" filter="url(#f)"/>` +
		`</svg></body></html>`
	const rHTML = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="200" height="200">` +
		`<rect x="50" y="50" width="100" height="100" fill="green"/>` +
		`</svg></body></html>`
	testImg := renderToImage(t, tHTML, 300, 300)
	refImg := renderToImage(t, rHTML, 300, 300)
	// Sample at the rect corners and center.
	for _, p := range []struct{ x, y int }{
		{60, 60},
		{100, 100},
		{140, 140},
	} {
		tR, tG, tB, _ := testImg.At(p.x, p.y).RGBA()
		rR, rG, rB, _ := refImg.At(p.x, p.y).RGBA()
		if (tR>>8 != rR>>8) || (tG>>8 != rG>>8) || (tB>>8 != rB>>8) {
			t.Errorf("(%d,%d): test=(%d,%d,%d) ref=(%d,%d,%d)",
				p.x, p.y, tR>>8, tG>>8, tB>>8, rR>>8, rG>>8, rB>>8)
		}
	}
}

// TestSVG_FilterPositioningParity — A `<rect>` with filter applied
// should render at the same position as a plain `<rect>` of the same
// geometry (no filter). This is the parity check for the
// svg-feflood-001 WPT reftest, which compares a filtered red rect
// against a plain black rect at the same position. Sample the corners.
func TestSVG_FilterPositioningParity(t *testing.T) {
	const testHTML = `<!DOCTYPE html><html><head><style>svg{width:500px;height:500px}</style></head><body>` +
		`<p>x</p>` +
		`<svg>` +
		`<defs>` +
		`<filter id="f" x="0" y="0" width="1" height="1">` +
		`<feFlood flood-color="black"/>` +
		`</filter>` +
		`</defs>` +
		`<rect width="300" height="300" fill="red" filter="url(#f)"/>` +
		`</svg></body></html>`
	const refHTML = `<!DOCTYPE html><html><head><style>svg{width:500px;height:500px}</style></head><body>` +
		`<p>x</p>` +
		`<svg>` +
		`<rect width="300" height="300" fill="black"/>` +
		`</svg></body></html>`
	testImg := renderToImage(t, testHTML, 800, 600)
	refImg := renderToImage(t, refHTML, 800, 600)

	// Sample at a few interior points and compare. The test must
	// produce black where the ref produces black, and vice versa.
	for _, p := range []struct{ x, y int }{
		{50, 50},
		{100, 100},
		{200, 200},
		{250, 250},
		{350, 350},
		{50, 350},
		{350, 50},
	} {
		tR, tG, tB, _ := testImg.At(p.x, p.y).RGBA()
		rR, rG, rB, _ := refImg.At(p.x, p.y).RGBA()
		if (tR>>8 != rR>>8) || (tG>>8 != rG>>8) || (tB>>8 != rB>>8) {
			t.Errorf("(%d,%d): test=(%d,%d,%d) ref=(%d,%d,%d)",
				p.x, p.y, tR>>8, tG>>8, tB>>8, rR>>8, rG>>8, rB>>8)
		}
	}
}

// TestSVG_FilterFeBlendDoesNotMakeBackgroundBlack — Phase 7 reproduces
// the filter-subregion-01 scenario in miniature: a rect with a feBlend
// filter applied. The page area OUTSIDE the rect (where the source
// buffer is transparent) should NOT become black; the white page
// background should remain. Regression guard for the composite-back
// path.
func TestSVG_FilterFeBlendDoesNotMakeBackgroundBlack(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="200" height="200">` +
		`<defs>` +
		`<filter id="b">` +
		`<feBlend in2="SourceGraphic" mode="multiply"/>` +
		`</filter>` +
		`</defs>` +
		`<rect x="50" y="50" width="60" height="60" fill="green" filter="url(#b)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 300, 300)
	// Inside the rect: should be green-ish.
	sampleColorClose(t, img, 80, 80, 0, 60, 0, 50, "feBlend interior (green)")
	// OUTSIDE the rect's bbox (well outside any reasonable filter region): white.
	sampleColorClose(t, img, 180, 180, 255, 255, 255, 12, "page background (outside SVG content)")
	// Just outside the rect but POSSIBLY inside the filter region (-10% to 120% of rect bbox):
	// rect bbox (50,50,60,60), filter region (-10%, -10%, 120%, 120%) = (44, 44, 116, 116).
	// Sample at (115, 80): just outside filter region — must still be white (page bg).
	sampleColorClose(t, img, 130, 80, 255, 255, 255, 12, "outside filter region")
}

// TestSVG_FilterFloodOpacity_ConfirmPremultiplied — Replicates the
// upper-left section of filter-subregion-01: a filter region equal
// to the shape bbox, with an feFlood at primitive subregion 25-75%
// (objectBoundingBox primitiveUnits), flood-color="green" and
// flood-opacity="0.75". Compare to the WPT-ref equivalent: a plain
// rect at the subregion coords with fill="green" fill-opacity="0.75".
// Both should produce identical pixels per spec.
func TestSVG_FilterFloodOpacity_ConfirmPremultiplied(t *testing.T) {
	const tHTML = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="200" height="200">` +
		`<defs>` +
		`<filter id="f" x="0" y="0" width="100%" height="100%" primitiveUnits="objectBoundingBox">` +
		`<feFlood x="25%" y="25%" width="50%" height="50%" flood-color="green" flood-opacity="0.75"/>` +
		`</filter>` +
		`</defs>` +
		`<rect x="20" y="20" width="160" height="160" fill="green" filter="url(#f)"/>` +
		`</svg></body></html>`
	const rHTML = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="200" height="200">` +
		`<rect x="60" y="60" width="80" height="80" fill="green" fill-opacity="0.75"/>` +
		`</svg></body></html>`
	testImg := renderToImage(t, tHTML, 300, 300)
	refImg := renderToImage(t, rHTML, 300, 300)
	// Sample the flood square center.
	for _, p := range []struct{ x, y int }{
		{80, 80},
		{100, 100},
		{120, 120},
	} {
		tR, tG, tB, tA := testImg.At(p.x, p.y).RGBA()
		rR, rG, rB, rA := refImg.At(p.x, p.y).RGBA()
		diffR := int(tR>>8) - int(rR>>8)
		diffG := int(tG>>8) - int(rG>>8)
		diffB := int(tB>>8) - int(rB>>8)
		if abs(diffR) > 5 || abs(diffG) > 5 || abs(diffB) > 5 {
			t.Errorf("(%d,%d): test=(%d,%d,%d,%d) ref=(%d,%d,%d,%d) diff RGB (%d,%d,%d)",
				p.x, p.y, tR>>8, tG>>8, tB>>8, tA>>8,
				rR>>8, rG>>8, rB>>8, rA>>8,
				diffR, diffG, diffB)
		}
	}
}

// TestSVG_FilterFallbackOnUnknownID — `filter="url(#missing)"`
// references a non-existent filter. Per SVG 1.1 §6.4, an
// unresolvable filter reference treats the element's `filter`
// property as `none` — the element renders unfiltered. Mirrors
// Blink's SVGResources::FindFilter returning nullptr → no filter
// applied (LayoutObject::PaintWithFilter falls through to normal
// paint).
func TestSVG_FilterFallbackOnUnknownID(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100">` +
		`<rect width="100" height="100" fill="red" filter="url(#missing)"/>` +
		`</svg></body></html>`
	img := renderToImage(t, htmlContent, 200, 200)
	// Unknown filter → rect renders as plain red.
	sampleColorClose(t, img, 50, 50, 255, 0, 0, 12, "unknown filter fallback (red)")
}

// ---------------------------------------------------------------------------
// LOU-206: SVG root per-axis overflow clip (ComputeOverflowClipAxes)
//
// Mirrors Blink's LayoutSVGRoot::ComputeOverflowClipAxes at
// layout_svg_root.cc (Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
// When overflow-x: clip and overflow-y: visible (or vice-versa), the SVG
// root must clip only the specified axis and let the other overflow freely.
// ---------------------------------------------------------------------------

// TestSVG_OverflowClipXVisibleY — an inline <svg width=100 height=100>
// contains a <rect width=150 height=150>. CSS: overflow-x:clip;
// overflow-y:visible. The expected result is a 100×150 green rectangle
// (clipped to 100 on X, overflows to 150 on Y).
//
// Sample grid:
//
//	(50, 50)   — inside both axes, must be green
//	(50, 120)  — inside X clip, beyond SVG height on Y (should overflow → green)
//	(120, 50)  — beyond SVG width on X (should be clipped → white)
//	(120, 120) — beyond both axes (both clipped in X, visible in Y) → white
func TestSVG_OverflowClipXVisibleY(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100" style="overflow-x:clip;overflow-y:visible">` +
		`<rect width="150" height="150" fill="green"/>` +
		`</svg></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)

	// Inside the SVG box: green.
	sampleColorClose(t, img, 50, 50, 0, 128, 0, 12, "inside svg box (50,50)")
	// Beyond height but within width — Y is visible so must be green.
	sampleColorClose(t, img, 50, 120, 0, 128, 0, 12, "overflow-y (50,120)")
	// Beyond width — X is clipped so must be white (canvas).
	sampleColorClose(t, img, 120, 50, 255, 255, 255, 12, "clipped-x (120,50)")
	// Beyond both — X clipped, canvas white.
	sampleColorClose(t, img, 120, 120, 255, 255, 255, 12, "clipped-x beyond-y (120,120)")
}

// TestSVG_OverflowClipYVisibleX — mirror: overflow-x:visible; overflow-y:clip.
// Expected: a 150×100 green rectangle (overflows to 150 on X, clipped to 100 on Y).
//
// Sample grid:
//
//	(50, 50)   — inside both axes, must be green
//	(120, 50)  — beyond SVG width on X (should overflow → green)
//	(50, 120)  — beyond SVG height on Y (should be clipped → white)
//	(120, 120) — beyond both axes (visible in X, clipped in Y) → white
func TestSVG_OverflowClipYVisibleX(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="100" height="100" style="overflow-x:visible;overflow-y:clip">` +
		`<rect width="150" height="150" fill="green"/>` +
		`</svg></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)

	// Inside the SVG box: green.
	sampleColorClose(t, img, 50, 50, 0, 128, 0, 12, "inside svg box (50,50)")
	// Beyond width but within height — X is visible so must be green.
	sampleColorClose(t, img, 120, 50, 0, 128, 0, 12, "overflow-x (120,50)")
	// Beyond height — Y is clipped so must be white (canvas).
	sampleColorClose(t, img, 50, 120, 255, 255, 255, 12, "clipped-y (50,120)")
	// Beyond both — Y clipped, canvas white.
	sampleColorClose(t, img, 120, 120, 255, 255, 255, 12, "clipped-y beyond-x (120,120)")
}
