package filters

import (
	"image"
	"testing"
)

// TestFEDisplacementMap_ShiftRight verifies that a uniform displacement
// map carrying G=255, B=128 (the values from the WPT
// tainting-feflood-001 test's feFlood input) produces a +50px X-shift
// and ~0 Y-shift when scale=100. This isolates the FE math from the
// CSS parser's percent-color bug.
func TestFEDisplacementMap_ShiftRight(t *testing.T) {
	region := image.Rect(0, 0, 100, 100)
	// Build the source: red on left half (x=0..49), green on right (x=50..99).
	src := image.NewRGBA(region)
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			i := y*src.Stride + x*4
			if x < 50 {
				src.Pix[i+0] = 255 // R
				src.Pix[i+1] = 0
				src.Pix[i+2] = 0
				src.Pix[i+3] = 255
			} else {
				src.Pix[i+0] = 0
				src.Pix[i+1] = 128 // green (css green)
				src.Pix[i+2] = 0
				src.Pix[i+3] = 255
			}
		}
	}
	// Build the map: uniform (0, 255, 128, 255) per the WPT test's
	// feFlood output (in sRGB space, no linear conversion).
	mp := image.NewRGBA(region)
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			i := y*mp.Stride + x*4
			mp.Pix[i+0] = 0
			mp.Pix[i+1] = 255
			mp.Pix[i+2] = 128
			mp.Pix[i+3] = 255
		}
	}
	fe := NewFEDisplacementMap(nil, nil, InterpolationSpaceSRGB, ChannelG, ChannelB, 100)
	out := fe.ApplyEffect([]*image.RGBA{src, mp}, region)

	// With G=255 → dx = +50, with B=128 → dy ≈ 0.196 (≈ 0).
	// Output(x, y) samples source(x+50, y).
	// Output(0, 50) → source(50, 50) which is GREEN (x>=50).
	// Output(49, 50) → source(99, 50) which is GREEN (last column).
	// Output(50, 50) → source(100, 50) which is OUT OF BOUNDS → transparent.

	at := func(x, y int) (uint8, uint8, uint8, uint8) {
		i := y*out.Stride + x*4
		return out.Pix[i+0], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3]
	}

	// Sample WELL INTO the green region (not at the red/green boundary
	// at output x≈0, where bilinear interpolation blends).
	// output(10, 50) → source(60.0, 50.196) → green (no boundary nearby).
	r, g, b, a := at(10, 50)
	if !(r < 5 && g >= 120 && g <= 130 && b < 5 && a >= 250) {
		t.Errorf("output(10, 50) = (%d, %d, %d, %d); expected green-ish (~0, 128, 0, 255)", r, g, b, a)
	}
	r, g, b, a = at(40, 50)
	if !(r < 5 && g >= 120 && g <= 130 && b < 5 && a >= 250) {
		t.Errorf("output(40, 50) = (%d, %d, %d, %d); expected green-ish", r, g, b, a)
	}
	r, g, b, a = at(60, 50)
	if a > 5 {
		t.Errorf("output(60, 50) = (%d, %d, %d, %d); expected transparent (alpha~0) because source(110, 50) is out-of-bounds", r, g, b, a)
	}
}

// TestFEDisplacementMap_IntegerBoundaryNoBleed asserts that when the
// displacement formula produces an exact-integer source coordinate
// landing on a colour boundary in the source, the output samples the
// destination pixel cleanly with no bleed from the neighbour. This
// captures the WPT tainting-feflood-001 failure mode and mirrors
// Blink/Skia + Firefox behaviour, both of which sample the colour input
// with nearest-neighbour (not bilinear). See:
//   - Skia src/effects/imagefilters/SkDisplacementMapImageFilter.cpp:43
//     `kDisplacementSampling{SkFilterMode::kNearest}`
//   - Firefox gfx/2d/FilterNodeSoftware.cpp:2672-2686
//     `int32_t(scaleOver255 * channel + scaleAdjustment)` + ColorAtPoint
//
// Same setup as TestFEDisplacementMap_ShiftRight (G=255 → dx=+50, source
// is red[0..49] / green[50..99]). For output(0, 50) the formula gives
// source(50, 50.196) — pixel 50 is the first GREEN column. The output
// MUST be green; any bleed of red from source(49, *) means the sampler
// is averaging across the boundary, which Skia explicitly avoids by
// using nearest-neighbour.
func TestFEDisplacementMap_IntegerBoundaryNoBleed(t *testing.T) {
	region := image.Rect(0, 0, 100, 100)
	src := image.NewRGBA(region)
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			i := y*src.Stride + x*4
			if x < 50 {
				src.Pix[i+0] = 255 // pure RED
				src.Pix[i+3] = 255
			} else {
				src.Pix[i+1] = 128 // green
				src.Pix[i+3] = 255
			}
		}
	}
	mp := image.NewRGBA(region)
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			i := y*mp.Stride + x*4
			mp.Pix[i+1] = 255
			mp.Pix[i+2] = 128
			mp.Pix[i+3] = 255
		}
	}
	fe := NewFEDisplacementMap(nil, nil, InterpolationSpaceSRGB, ChannelG, ChannelB, 100)
	out := fe.ApplyEffect([]*image.RGBA{src, mp}, region)

	// Output(0, 50) reads source(50.0, 50.196). Source(50, 50) is green.
	// With nearest-neighbour, the result is pure green (R<5). With
	// half-pixel-centre bilinear sampling at sx=50.0 exactly, the
	// sampler averages source(49)=red and source(50)=green → R≈64-128.
	// We assert R < 5 to capture the bug.
	i := 50*out.Stride + 0*4
	r, g, b, a := out.Pix[i+0], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3]
	if r > 5 {
		t.Errorf("output(0, 50) = (%d, %d, %d, %d); expected pure green (R<5) — RED bleed from source pixel 49 indicates the sampler is blending across the red/green boundary at the exact-integer source coord (50.0). Skia and Firefox both use nearest-neighbour, not bilinear, for this reason.", r, g, b, a)
	}
	_ = g
	_ = b
	_ = a
}
