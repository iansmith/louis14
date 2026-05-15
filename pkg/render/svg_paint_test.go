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
