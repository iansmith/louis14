package filters

import (
	"image"
	"testing"
)

// TestFETurbulence_ZeroFrequency_FractalNoise verifies that at baseFrequency 0
// the fractalNoise generator produces a constant 0.5 per channel. This is the
// behaviour an invalid (negative) baseFrequency falls back to per crbug/1068863
// and is what makes filter-turbulence-invalid-001 render as the reference green.
func TestFETurbulence_ZeroFrequency_FractalNoise(t *testing.T) {
	region := image.Rect(0, 0, 8, 8)
	fe := NewFETurbulence(InterpolationSpaceSRGB, TurbulenceFractalNoise, 0, 0, 1, 0, false)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	// fractalNoise at freq 0 → noise sum 0 → (0+1)/2 = 0.5 per channel.
	// Stored premultiplied: with A=0.5 (~128), RGB premultiplied = 0.5*0.5 = 0.25 (~64).
	for i := 0; i+3 < len(out.Pix); i += 4 {
		a := int(out.Pix[i+3])
		if a < 127 || a > 129 {
			t.Fatalf("fractalNoise freq0: pixel %d alpha expected ~128 got %d", i/4, a)
		}
		// Unpremultiplied colour ~0.5 → premultiplied ~64.
		for c := 0; c < 3; c++ {
			v := int(out.Pix[i+c])
			if v < 63 || v > 65 {
				t.Fatalf("fractalNoise freq0: pixel %d ch %d expected ~64 got %d", i/4, c, v)
			}
		}
	}
}

// TestFETurbulence_ZeroFrequency_Turbulence verifies that at baseFrequency 0
// the turbulence generator produces 0 per channel (|0| = 0) → transparent black.
func TestFETurbulence_ZeroFrequency_Turbulence(t *testing.T) {
	region := image.Rect(0, 0, 8, 8)
	fe := NewFETurbulence(InterpolationSpaceSRGB, TurbulenceTurbulence, 0, 0, 1, 0, false)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 0 || out.Pix[i+1] != 0 || out.Pix[i+2] != 0 || out.Pix[i+3] != 0 {
			t.Fatalf("turbulence freq0: pixel %d expected transparent black got %v", i/4, out.Pix[i:i+4])
		}
	}
}

// TestFETurbulence_NonZeroFrequency_Varies verifies that a real, positive
// baseFrequency produces a non-constant image (proves the Perlin generator is
// not a stub returning a constant).
func TestFETurbulence_NonZeroFrequency_Varies(t *testing.T) {
	region := image.Rect(0, 0, 64, 64)
	fe := NewFETurbulence(InterpolationSpaceSRGB, TurbulenceFractalNoise, 0.1, 0.1, 3, 0, false)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	first := out.Pix[3]
	varied := false
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i+3] != first {
			varied = true
			break
		}
	}
	if !varied {
		t.Fatal("turbulence freq 0.1: expected a varying noise field, got a constant image")
	}
}
