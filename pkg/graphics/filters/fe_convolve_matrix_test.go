package filters

import (
	"image"
	"image/color"
	"testing"
)

// TestFEConvolveMatrix_ValidPremultipliedOutput pins the footer of Skia's
// matrix convolution shader. With preserveAlpha it accumulates unpremultiplied
// colour and multiplies by the original alpha; otherwise it saturates the
// convolved alpha; either way RGB is clamped to [0, alpha].
//
// Skia: make_matrix_conv_shader, src/core/SkKnownRuntimeEffects.cpp:166-196 @
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e. Blink passes
// convolve_alpha = !preserve_alpha_ and gain = 1/divisor,
// platform/graphics/filters/fe_convolve_matrix.cc:148-152 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. The kernels here are symmetric.
func TestFEConvolveMatrix_ValidPremultipliedOutput(t *testing.T) {
	identityTimes2 := []float64{0, 0, 0, 0, 2, 0, 0, 0, 0}
	identity := []float64{0, 0, 0, 0, 1, 0, 0, 0, 0}
	sharpen := []float64{0, -1, 0, -1, 5, -1, 0, -1, 0}
	row := []float64{0, 0, 0, 1, 1, 1, 0, 0, 0}
	red50 := color.RGBA{128, 0, 0, 128}
	red25 := color.RGBA{64, 0, 0, 64}
	blue := color.RGBA{0, 0, 255, 255}
	black := color.RGBA{0, 0, 0, 255}

	for _, tc := range []struct {
		name          string
		center        color.RGBA
		neighbor      color.RGBA
		kernel        []float64
		divisor, bias float64
		preserveAlpha bool
		want          color.RGBA
	}{
		{"preserveAlpha clamps colour to the original alpha", red50, red50, identityTimes2, 1, 0, true, red50},
		{"preserveAlpha with bias keeps transparent black", color.RGBA{}, color.RGBA{}, identity, 1, 0.5, true, color.RGBA{}},
		{"preserveAlpha accumulates unpremultiplied", black, red25, row, 3, 0, true, color.RGBA{170, 0, 0, 255}},
		{"convolved alpha with bias paints transparent black", color.RGBA{}, color.RGBA{}, identity, 1, 0.5, false, color.RGBA{128, 128, 128, 128}},
		{"negative weights clamp colour to convolved alpha", red50, blue, sharpen, 1, 0, false, color.RGBA{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region := image.Rect(0, 0, 3, 3)
			in := image.NewRGBA(region)
			for y := 0; y < 3; y++ {
				for x := 0; x < 3; x++ {
					in.SetRGBA(x, y, tc.neighbor)
				}
			}
			in.SetRGBA(1, 1, tc.center)
			e := NewFEConvolveMatrix(nil, InterpolationSpaceSRGB, 3, tc.kernel, tc.divisor, tc.bias,
				1, 1, "duplicate", 0, 0, tc.preserveAlpha)
			out := e.ApplyEffect([]*image.RGBA{in}, region)
			if got := out.RGBAAt(1, 1); got != tc.want {
				t.Errorf("center = %v, want %v", got, tc.want)
			}
		})
	}
}
