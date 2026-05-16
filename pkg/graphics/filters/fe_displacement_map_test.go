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
