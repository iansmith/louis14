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
