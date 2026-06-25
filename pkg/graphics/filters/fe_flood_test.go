package filters

import (
	"image"
	"testing"
)

// TestFEFlood_SRGB_StoresRawColor verifies that in an sRGB filter the flood
// color is stored unchanged (no gamma conversion): green (0,128,0) opaque
// remains (0,128,0,255).
func TestFEFlood_SRGB_StoresRawColor(t *testing.T) {
	region := image.Rect(0, 0, 2, 2)
	fe := NewFEFlood(InterpolationSpaceSRGB, 0, 128, 0, 1.0)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 0 || out.Pix[i+1] != 128 || out.Pix[i+2] != 0 || out.Pix[i+3] != 255 {
			t.Fatalf("sRGB flood: pixel %d expected (0,128,0,255) got %v", i/4, out.Pix[i:i+4])
		}
	}
}

// TestFEFlood_LinearRGB_ConvertsColor verifies that in a linearRGB filter the
// flood color is converted sRGB → linearRGB before storage, so that the final
// linearRGB → sRGB output conversion round-trips back to the original sRGB
// value. The decisive end-to-end check: after ApplyEffect (which stores the
// converted value) ApplyToSRGB must return the original sRGB green (0,128,0).
func TestFEFlood_LinearRGB_ConvertsColor(t *testing.T) {
	region := image.Rect(0, 0, 2, 2)
	fe := NewFEFlood(InterpolationSpaceLinearRGB, 0, 128, 0, 1.0)
	f := &Filter{FilterRegion: region, LastEffect: fe}

	// The stored (linearRGB) green must be DARKER than the sRGB byte: the
	// linear value of 0.502 is ~0.216 → byte ~55. If the conversion were
	// missing it would store 128 and the round-trip below would over-brighten.
	stored := f.Apply()
	if g := stored.Pix[1]; g < 50 || g > 60 {
		t.Fatalf("linearRGB flood: stored green expected ~55 (linear), got %d", g)
	}

	// Round-trip back to sRGB must recover the original (0,128,0,255) ±1.
	srgb := f.ApplyToSRGB()
	for i := 0; i+3 < len(srgb.Pix); i += 4 {
		g := int(srgb.Pix[i+1])
		if srgb.Pix[i] != 0 || g < 127 || g > 129 || srgb.Pix[i+2] != 0 || srgb.Pix[i+3] != 255 {
			t.Fatalf("linearRGB flood round-trip: pixel %d expected ~(0,128,0,255) got %v", i/4, srgb.Pix[i:i+4])
		}
	}
}
