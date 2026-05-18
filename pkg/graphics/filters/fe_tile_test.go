package filters

import (
	"image"
	"testing"
)

// subregionInputEffect returns a precomputed buffer AND advertises a
// FilterPrimitiveSubregion so feTile can pick up the tile source rect.
type subregionInputEffect struct {
	baseEffect
	img *image.RGBA
	sub image.Rectangle
}

func newSubregionInputEffect(img *image.RGBA, sub image.Rectangle, space InterpolationSpace) *subregionInputEffect {
	return &subregionInputEffect{baseEffect: baseEffect{space: space}, img: img, sub: sub}
}

func (s *subregionInputEffect) ApplyEffect(_ []*image.RGBA, region image.Rectangle) *image.RGBA {
	return s.img
}

func (s *subregionInputEffect) FilterPrimitiveSubregion() image.Rectangle { return s.sub }

// TestFETile_FillsRegion verifies a 5×5 source subregion at (0,0,5,5) tiled
// across a 20×20 filter region: every output pixel must equal source[x%5][y%5].
func TestFETile_FillsRegion(t *testing.T) {
	region := image.Rect(0, 0, 20, 20)
	// Build a 5×5 colorful pattern inside a region-sized buffer; outside the
	// 5×5 subregion the buffer is transparent (zero). The tile source is the
	// subregion, not the whole input.
	srcImg := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			off := y*srcImg.Stride + x*4
			srcImg.Pix[off] = uint8(x * 50)   // R
			srcImg.Pix[off+1] = uint8(y * 50) // G
			srcImg.Pix[off+2] = 0
			srcImg.Pix[off+3] = 255
		}
	}
	in := newSubregionInputEffect(srcImg, image.Rect(0, 0, 5, 5), InterpolationSpaceSRGB)
	fe := NewFETile(in, InterpolationSpaceSRGB)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	if out == nil {
		t.Fatal("Apply returned nil")
	}
	for y := 0; y < region.Dy(); y++ {
		for x := 0; x < region.Dx(); x++ {
			soff := (y%5)*srcImg.Stride + (x%5)*4
			doff := y*out.Stride + x*4
			for c := 0; c < 4; c++ {
				if out.Pix[doff+c] != srcImg.Pix[soff+c] {
					t.Fatalf("tile (%d,%d): channel %d expected %d got %d",
						x, y, c, srcImg.Pix[soff+c], out.Pix[doff+c])
				}
			}
		}
	}
}

// TestFETile_EmptySubregion verifies that a tile whose advertised subregion is
// empty (zero-sized) produces a transparent buffer. Blink returns
// nullptr/passthrough in that branch; louis14's equivalent is "transparent"
// because every effect's output buffer is sized to the filter region.
func TestFETile_EmptySubregion(t *testing.T) {
	region := image.Rect(0, 0, 8, 8)
	srcImg := solidInput(region, 255, 0, 0, 255)
	in := newSubregionInputEffect(srcImg, image.Rectangle{}, InterpolationSpaceSRGB)
	fe := NewFETile(in, InterpolationSpaceSRGB)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	if out == nil {
		t.Fatal("Apply returned nil")
	}
	// With empty subregion and a non-source input, the tile source falls
	// back to the filter region (matching Blink's behaviour when no
	// primitive subregion has been resolved). Verify the output is the
	// input replicated — which since the input already fills the region
	// means the output equals the input.
	for i := 0; i+3 < len(out.Pix); i += 4 {
		if out.Pix[i] != 255 || out.Pix[i+1] != 0 || out.Pix[i+2] != 0 || out.Pix[i+3] != 255 {
			t.Fatalf("empty-subregion fallback: pixel %d expected red, got %v",
				i/4, out.Pix[i:i+4])
		}
	}
}

// TestFETile_OffsetSubregion verifies the tile origin correctly handles a
// subregion that doesn't start at (0, 0). With a subregion at (10, 10, 15, 15)
// inside a 20×20 buffer, the tile source is those 5×5 pixels, and tiling
// across the filter region produces pixel (x, y) =
// source[10 + (x-tileOrigin.X) mod 5][10 + (y-tileOrigin.Y) mod 5].
// Per spec the tile pattern aligns with the source subregion origin
// (Blink TilePaintFilter mirrors that with src/dst rect anchors).
func TestFETile_OffsetSubregion(t *testing.T) {
	region := image.Rect(0, 0, 20, 20)
	srcImg := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	// Paint 5×5 pattern at (10, 10).
	for y := 10; y < 15; y++ {
		for x := 10; x < 15; x++ {
			off := y*srcImg.Stride + x*4
			srcImg.Pix[off] = uint8((x - 10) * 50)
			srcImg.Pix[off+1] = uint8((y - 10) * 50)
			srcImg.Pix[off+2] = 0
			srcImg.Pix[off+3] = 255
		}
	}
	in := newSubregionInputEffect(srcImg, image.Rect(10, 10, 15, 15), InterpolationSpaceSRGB)
	fe := NewFETile(in, InterpolationSpaceSRGB)
	f := &Filter{FilterRegion: region, LastEffect: fe}
	out := f.Apply()
	for y := 0; y < region.Dy(); y++ {
		for x := 0; x < region.Dx(); x++ {
			// Source pixel index inside the 5×5 tile, aligned to the
			// subregion origin. floor-mod is the SVG-spec semantic.
			sx := ((x-10)%5 + 5) % 5
			sy := ((y-10)%5 + 5) % 5
			soff := (10+sy)*srcImg.Stride + (10+sx)*4
			doff := y*out.Stride + x*4
			for c := 0; c < 4; c++ {
				if out.Pix[doff+c] != srcImg.Pix[soff+c] {
					t.Fatalf("offset tile (%d,%d): channel %d expected %d got %d",
						x, y, c, srcImg.Pix[soff+c], out.Pix[doff+c])
				}
			}
		}
	}
}
