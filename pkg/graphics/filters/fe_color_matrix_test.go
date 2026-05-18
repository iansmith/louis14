package filters

import (
	"image"
	"testing"
)

// solidImage builds a region-sized buffer filled with one premultiplied colour.
func solidImage(region image.Rectangle, r, g, b, a uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, a
	}
	return img
}

// TestColorMatrixIdentity verifies an identity MATRIX leaves an opaque pixel
// unchanged — the invariant Phase 1 establishes that makes contrast(100%) etc.
// true identities.
func TestColorMatrixIdentity(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	src := NewSourceGraphic(InterpolationSpaceSRGB)
	src.image = solidImage(region, 0, 128, 0, 255) // opaque green
	identity := []float64{
		1, 0, 0, 0, 0,
		0, 1, 0, 0, 0,
		0, 0, 1, 0, 0,
		0, 0, 0, 1, 0,
	}
	cm := NewFEColorMatrix(src, InterpolationSpaceSRGB, ColorMatrixTypeMatrix, identity)
	f := &Filter{FilterRegion: region, Source: src, LastEffect: cm}
	out := f.Apply()
	if out == nil {
		t.Fatal("Apply returned nil")
	}
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 0 || out.Pix[i+1] != 128 || out.Pix[i+2] != 0 || out.Pix[i+3] != 255 {
			t.Fatalf("identity matrix changed pixel %d: got %v", i/4,
				out.Pix[i:i+4])
		}
	}
}

// TestSaturateIdentity verifies saturate(1) is an identity.
func TestSaturateIdentity(t *testing.T) {
	region := image.Rect(0, 0, 2, 2)
	src := NewSourceGraphic(InterpolationSpaceSRGB)
	src.image = solidImage(region, 40, 90, 160, 255)
	cm := NewFEColorMatrix(src, InterpolationSpaceSRGB, ColorMatrixTypeSaturate, []float64{1})
	f := &Filter{FilterRegion: region, Source: src, LastEffect: cm}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		for c := 0; c < 4; c++ {
			d := int(out.Pix[i+c]) - int(src.image.Pix[i+c])
			if d < -1 || d > 1 {
				t.Fatalf("saturate(1) changed channel %d by %d", c, d)
			}
		}
	}
}

// TestComponentTransferContrastIdentity verifies contrast(1) — slope 1,
// intercept 0 — is an identity, the bug the old premultiplied byte loop had.
func TestComponentTransferContrastIdentity(t *testing.T) {
	region := image.Rect(0, 0, 2, 2)
	src := NewSourceGraphic(InterpolationSpaceSRGB)
	src.image = solidImage(region, 0, 128, 0, 255)
	// contrast(a): slope=a, intercept=-(0.5*a)+0.5. For a=1: slope 1, intercept 0.
	lin := TransferFunc{Type: TransferLinear, Slope: 1, Intercept: 0}
	ident := TransferFunc{Type: TransferIdentity}
	ct := NewFEComponentTransfer(src, InterpolationSpaceSRGB,
		[4]TransferFunc{lin, lin, lin, ident})
	f := &Filter{FilterRegion: region, Source: src, LastEffect: ct}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 0 || out.Pix[i+1] != 128 || out.Pix[i+2] != 0 || out.Pix[i+3] != 255 {
			t.Fatalf("contrast(1) changed pixel %d: got %v", i/4, out.Pix[i:i+4])
		}
	}
}
