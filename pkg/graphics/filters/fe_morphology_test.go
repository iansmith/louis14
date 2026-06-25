package filters

import (
	"image"
	"testing"
)

// TestFEMorphology_Dilate_ExpandsOpaqueBlock verifies that dilate grows an
// opaque block by radius pixels in every direction: a single opaque pixel at
// the centre with radiusX=radiusY=1 fills the surrounding 3x3 window.
func TestFEMorphology_Dilate_ExpandsOpaqueBlock(t *testing.T) {
	region := image.Rect(0, 0, 5, 5)
	in := image.NewRGBA(image.Rect(0, 0, 5, 5))
	// Single opaque white pixel at (2,2).
	set := func(x, y int, r, g, b, a uint8) {
		i := y*in.Stride + x*4
		in.Pix[i], in.Pix[i+1], in.Pix[i+2], in.Pix[i+3] = r, g, b, a
	}
	set(2, 2, 255, 255, 255, 255)

	src := newPixelInputEffect(in, InterpolationSpaceSRGB)
	fe := NewFEMorphology(src, InterpolationSpaceSRGB, MorphologyDilate, 1, 1)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()

	// After dilate radius 1, the 3x3 window centred on (2,2) — i.e. x,y in
	// [1,3] — must be opaque white; everything else transparent.
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			i := y*out.Stride + x*4
			inWindow := x >= 1 && x <= 3 && y >= 1 && y <= 3
			if inWindow {
				if out.Pix[i+3] != 255 || out.Pix[i] != 255 {
					t.Fatalf("dilate: (%d,%d) expected opaque white, got %v", x, y, out.Pix[i:i+4])
				}
			} else if out.Pix[i+3] != 0 {
				t.Fatalf("dilate: (%d,%d) expected transparent, got %v", x, y, out.Pix[i:i+4])
			}
		}
	}
}

// TestFEMorphology_Erode_ShrinksOpaqueBlock verifies erode takes the min over
// the window: a 5x5 opaque field eroded by radius 1 stays opaque only where the
// full window is opaque (the interior); the 1px border becomes transparent
// because its window reaches the implicit transparent area outside the data.
func TestFEMorphology_Erode_ShrinksOpaqueBlock(t *testing.T) {
	region := image.Rect(0, 0, 5, 5)
	in := image.NewRGBA(image.Rect(0, 0, 5, 5))
	for i := 0; i+3 < len(in.Pix); i += 4 {
		in.Pix[i], in.Pix[i+1], in.Pix[i+2], in.Pix[i+3] = 255, 0, 0, 255
	}
	src := newPixelInputEffect(in, InterpolationSpaceSRGB)
	fe := NewFEMorphology(src, InterpolationSpaceSRGB, MorphologyErode, 1, 1)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()

	// Only the interior 3x3 (x,y in [1,3]) has a fully-opaque window.
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			i := y*out.Stride + x*4
			interior := x >= 1 && x <= 3 && y >= 1 && y <= 3
			if interior {
				if out.Pix[i+3] != 255 || out.Pix[i] != 255 {
					t.Fatalf("erode: (%d,%d) expected opaque red, got %v", x, y, out.Pix[i:i+4])
				}
			} else if out.Pix[i+3] != 0 {
				t.Fatalf("erode: (%d,%d) expected transparent, got %v", x, y, out.Pix[i:i+4])
			}
		}
	}
}

// TestFEMorphology_ZeroRadius_TransparentBlack verifies the spec rule that a
// zero radius disables the effect, producing a transparent black image.
func TestFEMorphology_ZeroRadius_TransparentBlack(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)
	in := solidInput(region, 0, 255, 0, 255) // opaque green
	src := newPixelInputEffect(in, InterpolationSpaceSRGB)
	fe := NewFEMorphology(src, InterpolationSpaceSRGB, MorphologyDilate, 0, 0)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 0 || out.Pix[i+1] != 0 || out.Pix[i+2] != 0 || out.Pix[i+3] != 0 {
			t.Fatalf("zero radius: pixel %d expected transparent black, got %v", i/4, out.Pix[i:i+4])
		}
	}
}

// TestFEMorphology_MapRect_InflatesByRadius verifies MapRect grows the rect by
// (radiusX, radiusY) on each side for dilate-style spread accounting.
func TestFEMorphology_MapRect_InflatesByRadius(t *testing.T) {
	src := newPixelInputEffect(nil, InterpolationSpaceSRGB)
	fe := NewFEMorphology(src, InterpolationSpaceSRGB, MorphologyDilate, 3, 2)
	got := fe.MapRect(image.Rect(10, 10, 20, 20))
	want := image.Rect(7, 8, 23, 22)
	if got != want {
		t.Fatalf("MapRect: got %v want %v", got, want)
	}
}
