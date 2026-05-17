package filters

import (
	"image"
	"testing"
)

// TestFEDiffuseLighting_FlatBumpDistantLight90 verifies the Lambert
// BRDF on the simplest, most-discriminating case: a uniform alpha=255
// input (flat surface, normal = (0, 0, 1) everywhere) with a distant
// light directly overhead (azimuth=0, elevation=90 → L = (0, 0, 1)).
// Lambert reduces to N·L = 1, so output_RGB = LightingColor.RGB and
// output_alpha = LightingColor.A.
//
// This is the EXACT setup of WPT tainting-fediffuselighting-001 /-002
// reduced to the FE level: a `<feFlood/>` (alpha=255, RGB=0) into
// feDiffuseLighting with lighting-color=rgb(0, 255, 128) and
// `<feDistantLight elevation="90"/>` must emit (0, 255, 128, 255)
// everywhere.
//
// Pre-implementation: FEDiffuseLighting type doesn't exist → test
// won't compile. Post-implementation: ApplyEffect returns the
// expected flat colour at every pixel.
func TestFEDiffuseLighting_FlatBumpDistantLight90(t *testing.T) {
	region := image.Rect(0, 0, 10, 10)

	// Build the input: alpha=255 everywhere, RGB=0 (the feFlood
	// default-black-opaque output).
	src := image.NewRGBA(region)
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			i := y*src.Stride + x*4
			src.Pix[i+3] = 255
		}
	}

	srcEffect := NewSourceGraphic(InterpolationSpaceSRGB)
	srcEffect.image = src

	fe := NewFEDiffuseLighting(
		srcEffect,
		InterpolationSpaceSRGB,
		/*R*/ 0 /*G*/, 255 /*B*/, 128 /*A*/, 1.0,
		/*surfaceScale*/ 1.0,
		/*diffuseConstant*/ 1.0,
		/*kernelUnitLengthX*/ 1.0,
		/*kernelUnitLengthY*/ 1.0,
		NewDistantLightSource(0, 90),
	)

	out := fe.ApplyEffect([]*image.RGBA{src}, region)
	if out == nil {
		t.Fatal("ApplyEffect returned nil")
	}

	// Every pixel must be (0, 255, 128, 255) within ±1 (premultiplied
	// rounding). For a uniform alpha=255 input and N·L=1, output =
	// LightingColor verbatim.
	at := func(x, y int) (uint8, uint8, uint8, uint8) {
		i := y*out.Stride + x*4
		return out.Pix[i+0], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3]
	}
	withinPM1 := func(a, b uint8) bool {
		if a > b {
			return a-b <= 1
		}
		return b-a <= 1
	}
	// Test interior (1, 1) and (8, 8); corner (0, 0); edge (0, 5).
	for _, pt := range [][2]int{{0, 0}, {0, 5}, {5, 5}, {8, 8}} {
		r, g, b, a := at(pt[0], pt[1])
		if !withinPM1(r, 0) || !withinPM1(g, 255) || !withinPM1(b, 128) || !withinPM1(a, 255) {
			t.Errorf("output(%d, %d) = (%d, %d, %d, %d); want (0, 255, 128, 255)",
				pt[0], pt[1], r, g, b, a)
		}
	}
}

// TestFEDiffuseLighting_NoLightSourceProducesTransparent verifies the
// spec behaviour when no <feDistantLight>/<fePointLight>/<feSpotLight>
// child is supplied: the primitive produces a transparent-black
// buffer per SVG Filter Effects 1 §"Lighting elements with no light
// source".
func TestFEDiffuseLighting_NoLightSourceProducesTransparent(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	src := image.NewRGBA(region)
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	srcEffect := NewSourceGraphic(InterpolationSpaceSRGB)
	srcEffect.image = src

	fe := NewFEDiffuseLighting(
		srcEffect,
		InterpolationSpaceSRGB,
		255, 255, 255, 1.0,
		1.0, 1.0, 1.0, 1.0,
		nil, // no light source
	)
	out := fe.ApplyEffect([]*image.RGBA{src}, region)
	for i := 0; i < len(out.Pix); i += 4 {
		if out.Pix[i+0] != 0 || out.Pix[i+1] != 0 || out.Pix[i+2] != 0 || out.Pix[i+3] != 0 {
			t.Errorf("pixel %d = (%d, %d, %d, %d); want transparent black",
				i/4, out.Pix[i+0], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3])
		}
	}
}
