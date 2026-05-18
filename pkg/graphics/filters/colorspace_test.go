package filters

import (
	"image"
	"math"
	"testing"
)

// TestSRGBLinearRoundTrip verifies the sRGB<->linear transfer functions are
// mutual inverses to within floating-point precision.
func TestSRGBLinearRoundTrip(t *testing.T) {
	for i := 0; i <= 1000; i++ {
		c := float64(i) / 1000.0
		got := linearToSRGB(srgbToLinear(c))
		if math.Abs(got-c) > 1e-9 {
			t.Errorf("srgb->linear->srgb(%v) = %v, want %v", c, got, c)
		}
		got2 := srgbToLinear(linearToSRGB(c))
		if math.Abs(got2-c) > 1e-9 {
			t.Errorf("linear->srgb->linear(%v) = %v, want %v", c, got2, c)
		}
	}
}

// TestSRGBLinearKnownValues checks anchor points of the transfer functions.
func TestSRGBLinearKnownValues(t *testing.T) {
	if srgbToLinear(0) != 0 {
		t.Errorf("srgbToLinear(0) = %v, want 0", srgbToLinear(0))
	}
	if math.Abs(srgbToLinear(1)-1) > 1e-12 {
		t.Errorf("srgbToLinear(1) = %v, want 1", srgbToLinear(1))
	}
	// 0.5 sRGB is well below midpoint in linear light.
	lin := srgbToLinear(0.5)
	if lin < 0.21 || lin > 0.22 {
		t.Errorf("srgbToLinear(0.5) = %v, want ~0.214", lin)
	}
}

// TestPremultiplyRoundTrip verifies premultiply/unpremultiply round-trip to
// within 1 ULP of 8-bit precision. Inputs are valid premultiplied pixels
// (every colour channel <= alpha), the only kind that occur in image.RGBA.
func TestPremultiplyRoundTrip(t *testing.T) {
	cases := [][4]uint8{
		{255, 128, 0, 255},
		{0, 255, 0, 255},
		{100, 50, 25, 128},
		{1, 1, 1, 1},
		{0, 0, 0, 0},
		{64, 32, 16, 200},
	}
	for _, px := range cases {
		r, g, b, a := unpremultiply(px)
		out := premultiply(r, g, b, a)
		for i := 0; i < 4; i++ {
			d := int(out[i]) - int(px[i])
			if d < -1 || d > 1 {
				t.Errorf("premultiply round-trip %v -> %v differs at %d by %d", px, out, i, d)
			}
		}
	}
}

// TestConvertImageSpaceNoOp verifies same-space conversion leaves pixels
// unchanged.
func TestConvertImageSpaceNoOp(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	orig := make([]uint8, len(img.Pix))
	copy(orig, img.Pix)
	convertImageSpace(img, InterpolationSpaceSRGB, InterpolationSpaceSRGB)
	for i := range img.Pix {
		if img.Pix[i] != orig[i] {
			t.Fatalf("no-op conversion changed pixel %d: %d -> %d", i, orig[i], img.Pix[i])
		}
	}
}

// TestConvertImageSpaceRoundTrip verifies sRGB->linear->sRGB on an image
// returns close to the original (within 8-bit quantization noise).
func TestConvertImageSpaceRoundTrip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i] = uint8((i * 3) % 256)
		img.Pix[i+1] = uint8((i * 5) % 256)
		img.Pix[i+2] = uint8((i * 7) % 256)
		img.Pix[i+3] = 255
	}
	orig := make([]uint8, len(img.Pix))
	copy(orig, img.Pix)
	convertImageSpace(img, InterpolationSpaceSRGB, InterpolationSpaceLinearRGB)
	convertImageSpace(img, InterpolationSpaceLinearRGB, InterpolationSpaceSRGB)
	// The intermediate linearRGB image is stored at 8-bit depth, so dark
	// colours lose precision (sRGB's gamma curve crowds many sRGB codes into
	// a few linear codes near black). A few LSBs of error is expected and
	// matches what Blink/Skia produce with 8-bit intermediates.
	for i := range img.Pix {
		d := int(img.Pix[i]) - int(orig[i])
		if d < -6 || d > 6 {
			t.Errorf("round-trip pixel %d: %d -> %d (delta %d)", i, orig[i], img.Pix[i], d)
		}
	}
}
