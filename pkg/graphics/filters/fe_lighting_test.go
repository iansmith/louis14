package filters

import (
	"math"
	"testing"
)

// TestFELighting_ComputeNormalEdgeReplication verifies that the Sobel
// kernel reads out-of-bounds neighbours by clamping to the nearest
// in-bounds pixel (replicate-edge sampling), NOT by treating
// out-of-bounds samples as zero alpha. The WPT `lighting-region.html`
// reftest catches this: with zero-padding the alpha gradient at the
// filter-region boundary produces a false "edge highlight"; with
// replicate-edge sampling the boundary continues the interior surface
// and no highlight appears.
//
// Mirrors Blink's fe_lighting.cc::SetPixel which uses implicit edge
// clamping via the texture sampler.
//
// The test constructs a 3x3 alpha field that is uniform 1.0 INSIDE
// (column 1, row 1) and 0 elsewhere — but really, the more decisive
// check is: a uniform-alpha buffer must produce N = (0, 0, 1) at the
// corner pixels too. With zero-padding the Sobel sums at a corner
// would be non-zero, tilting the normal. With replicate-edge sampling
// every neighbour samples in-bounds (all 1.0), Sobel sums are zero,
// N = (0, 0, 1).
func TestFELighting_ComputeNormalEdgeReplication(t *testing.T) {
	// Tiny uniform-alpha buffer.
	const w, h = 4, 4
	alpha := make([]float64, w*h)
	for i := range alpha {
		alpha[i] = 1.0 // fully opaque.
	}
	alphaAt := func(x, y int) float64 {
		return alpha[y*w+x]
	}

	e := &feLighting{
		SurfaceScale:      1,
		KernelUnitLengthX: 1,
		KernelUnitLengthY: 1,
	}

	// Probe every pixel — uniform alpha must produce N = (0, 0, 1)
	// EVERYWHERE, including the four corners and four edges. If
	// computeNormal used zero-padded sampling, corner pixels would
	// see a (1,1,1,1,0,1,0,1,1)-style asymmetric neighbourhood and
	// the Sobel sums would tilt the normal away from straight up.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nx, ny, nz := e.computeNormal(alphaAt, x, y, w, h)
			if !approxEq(nx, 0) || !approxEq(ny, 0) || !approxEq(nz, 1) {
				t.Errorf("computeNormal(%d, %d) = (%g, %g, %g); want (0, 0, 1) — Sobel must use replicate-edge sampling",
					x, y, nx, ny, nz)
			}
		}
	}

	// And one direct probe of the sampler closure inside the kernel:
	// at the top-left corner the kernel reads (x-1, y-1) etc. With
	// replicate-edge, sample(-1, -1) must equal sample(0, 0).
	// Construct a buffer with a unique corner alpha to make the
	// expectation observable via the kernel output.
	alpha2 := make([]float64, w*h)
	for i := range alpha2 {
		alpha2[i] = 0
	}
	alpha2[0] = 1.0 // only top-left corner non-zero.
	alphaAt2 := func(x, y int) float64 {
		return alpha2[y*w+x]
	}
	// At pixel (0, 0): with replicate-edge, the 3x3 neighbourhood
	// would be sample(-1,-1)=sample(0,0)=1, sample(0,-1)=sample(0,0)=1,
	// sample(1,-1)=sample(1,0)=0, sample(-1,0)=sample(0,0)=1,
	// sample(1,0)=0, sample(-1,1)=sample(0,1)=0, sample(0,1)=0, sample(1,1)=0.
	// Sobel sumX = -1+0 + -2+0 + 0+0 = -3. sumY = -1+-2+0 + 0+0+0 = -3.
	// Nx = -1/4 * -3 = 0.75. Ny = -1/4 * -3 = 0.75. Nz = 1.
	// Magnitude = sqrt(0.5625 + 0.5625 + 1) = sqrt(2.125) ≈ 1.4577.
	nx, ny, nz := e.computeNormal(alphaAt2, 0, 0, w, h)
	wantMag := math.Sqrt(0.75*0.75 + 0.75*0.75 + 1)
	wantNx := 0.75 / wantMag
	wantNy := 0.75 / wantMag
	wantNz := 1.0 / wantMag
	if !approxEq(nx, wantNx) || !approxEq(ny, wantNy) || !approxEq(nz, wantNz) {
		t.Errorf("computeNormal(0,0) with single-pixel corner = (%g, %g, %g); want (%g, %g, %g) under replicate-edge",
			nx, ny, nz, wantNx, wantNy, wantNz)
	}
}

// TestFELighting_NegativeKernelUnitLength verifies that a negative or zero
// kernelUnitLength defaults to 1, per SVG Filter Effects 1: "A negative or
// zero value is an error (see Error processing). A value of zero disables the
// effect of the given filter primitive" — Blink/Gecko treat both as the
// default 1. With kernelUnitLength="0 -1" the normal must equal the result
// computed with kernelUnitLength 1,1 (not a flipped/garbage normal from the
// negative denominator).
func TestFELighting_NegativeKernelUnitLength(t *testing.T) {
	const w, h = 4, 4
	// A non-uniform alpha field so the kernelUnitLength denominator matters.
	alpha := make([]float64, w*h)
	for i := range alpha {
		alpha[i] = float64(i) / float64(w*h)
	}
	alphaAt := func(x, y int) float64 { return alpha[y*w+x] }

	neg := &feLighting{SurfaceScale: 1, KernelUnitLengthX: 0, KernelUnitLengthY: -1}
	def := &feLighting{SurfaceScale: 1, KernelUnitLengthX: 1, KernelUnitLengthY: 1}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nx, ny, nz := neg.computeNormal(alphaAt, x, y, w, h)
			dx, dy, dz := def.computeNormal(alphaAt, x, y, w, h)
			if !approxEq(nx, dx) || !approxEq(ny, dy) || !approxEq(nz, dz) {
				t.Fatalf("computeNormal(%d,%d) with kUL=(0,-1) = (%g,%g,%g); want default-1 result (%g,%g,%g)",
					x, y, nx, ny, nz, dx, dy, dz)
			}
		}
	}
}
