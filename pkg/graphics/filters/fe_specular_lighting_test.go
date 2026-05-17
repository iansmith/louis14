package filters

import (
	"image"
	"testing"
)

// TestFESpecularLighting_FlatBumpDistantLight90 verifies the Phong
// specular BRDF on the same flat-surface case used by
// TestFEDiffuseLighting_FlatBumpDistantLight90.
//
// Setup: alpha=255 input (flat surface, N = (0, 0, 1) everywhere) +
// `<feDistantLight elevation="90"/>` (L = (0, 0, 1)). V = (0, 0, 1)
// always for a viewer at infinity. H = normalize(L + V) =
// normalize((0, 0, 2)) = (0, 0, 1). N · H = 1. (N · H)^1 = 1.
//
// With SpecularExponent=1, SpecularConstant=1, LightingColor =
// rgb(0, 255, 128):
//
//	I = 1 * 1 * 1 = 1
//	R_out = 1 * 0 = 0
//	G_out = 1 * 255 = 255
//	B_out = 1 * 128 = 128
//	A_out = max(0, 255, 128) = 255   (specular spec rule)
//
// The output alpha is max(R, G, B), NOT LightingColor.A — this is
// the key distinguishing feature from diffuse and is asserted here.
//
// Pre-implementation: FESpecularLighting type doesn't exist → test
// won't compile. Post-implementation: ApplyEffect returns the
// expected flat colour with alpha = max(RGB) at every pixel.
func TestFESpecularLighting_FlatBumpDistantLight90(t *testing.T) {
	region := image.Rect(0, 0, 10, 10)

	src := image.NewRGBA(region)
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			i := y*src.Stride + x*4
			src.Pix[i+3] = 255
		}
	}

	srcEffect := NewSourceGraphic(InterpolationSpaceSRGB)
	srcEffect.image = src

	fe := NewFESpecularLighting(
		srcEffect,
		InterpolationSpaceSRGB,
		/*R*/ 0 /*G*/, 255 /*B*/, 128 /*A*/, 1.0,
		/*surfaceScale*/ 1.0,
		/*specularConstant*/ 1.0,
		/*specularExponent*/ 1.0,
		/*kernelUnitLengthX*/ 1.0,
		/*kernelUnitLengthY*/ 1.0,
		NewDistantLightSource(0, 90),
	)
	out := fe.ApplyEffect([]*image.RGBA{src}, region)
	if out == nil {
		t.Fatal("ApplyEffect returned nil")
	}
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
	for _, pt := range [][2]int{{0, 0}, {0, 5}, {5, 5}, {8, 8}, {9, 9}} {
		r, g, b, a := at(pt[0], pt[1])
		// Alpha must equal max(R, G, B) = max(0, 255, 128) = 255.
		// Premultiplied storage with alpha=255 → R/G/B bytes are the
		// straight (non-premultiplied) channel values.
		if !withinPM1(r, 0) || !withinPM1(g, 255) || !withinPM1(b, 128) || !withinPM1(a, 255) {
			t.Errorf("output(%d, %d) = (%d, %d, %d, %d); want (0, 255, 128, 255) with A = max(R,G,B)",
				pt[0], pt[1], r, g, b, a)
		}
	}
}

// TestFESpecularLighting_DarkLightingColorAlphaMatchesMax verifies the
// specular alpha rule when LightingColor's max channel is < 255. With
// LightingColor=rgb(0, 100, 50) → R=0 G=100 B=50, max = 100. With
// I=1 the output (premultiplied) channels are
// (0*100/255 ≈ 0, 100*100/255 ≈ 39, 50*100/255 ≈ 20, alpha=100).
//
// This is the second discriminator vs diffuse: diffuse would emit
// alpha=255 (LightingColor.A) regardless of RGB.
func TestFESpecularLighting_DarkLightingColorAlphaMatchesMax(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	src := image.NewRGBA(region)
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	srcEffect := NewSourceGraphic(InterpolationSpaceSRGB)
	srcEffect.image = src

	fe := NewFESpecularLighting(
		srcEffect,
		InterpolationSpaceSRGB,
		0, 100, 50, 1.0,
		1.0, 1.0, 1.0, 1.0, 1.0,
		NewDistantLightSource(0, 90),
	)
	out := fe.ApplyEffect([]*image.RGBA{src}, region)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			i := y*out.Stride + x*4
			a := out.Pix[i+3]
			// Alpha must be exactly 100 (max of R=0, G=100, B=50).
			if a != 100 {
				t.Errorf("output(%d, %d) alpha = %d; want 100 (= max(R, G, B))", x, y, a)
			}
		}
	}
}
