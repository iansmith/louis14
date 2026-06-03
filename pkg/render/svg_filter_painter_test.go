package render

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/graphics/filters"
)

// TestCompositeFilterOutputOntoTarget_ClipRespected demonstrates that
// compositeFilterOutputOntoTarget must honour the supplied clipRect.
// Filter output (e.g. the 10% default expansion of the filter region in
// SVG Filter Effects) routinely extends outside the host SVG element's
// viewport, and the SVG UA stylesheet sets overflow:hidden so that
// content must NOT escape onto the page background.
//
// Setup: a 20x20 white target; a 10x10 source buffer of opaque red; the
// composite is positioned with dx=0, dy=0 so the source buffer would
// otherwise land at target (0,0)-(10,10). A clipRect at target (5,5)-(15,15)
// must constrain the write — pixels in the buffer that fall outside the
// clipRect (i.e. the source's top-left quadrant at target (0..4, 0..4))
// must NOT modify the target.
//
// Pre-fix: compositeFilterOutputOntoTarget bypassed the DC's clip stack
// and wrote every in-bounds source pixel into r.target.Pix, so the
// top-left of the target picked up red where it should have stayed
// white. That's the bucket-J tainting failure: filter overflow leaks
// past the SVG viewport clip.
func TestCompositeFilterOutputOntoTarget_ClipRespected(t *testing.T) {
	const W, H = 20, 20

	// White target.
	target := image.NewRGBA(image.Rect(0, 0, W, H))
	white := color.RGBA{255, 255, 255, 255}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			target.SetRGBA(x, y, white)
		}
	}

	// 10x10 source buffer, fully opaque red. The composite path
	// expects premultiplied bytes from the filter graph; opaque red
	// has identical straight-alpha and premultiplied bytes.
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	red := color.RGBA{255, 0, 0, 255}
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			src.SetRGBA(x, y, red)
		}
	}

	r := NewRendererForImage(target)

	// Clip rect (5,5)-(15,15). Composite at offset (0, 0) — without
	// the clip the source would write to target (0,0)-(10,10) which
	// overlaps both the clipped region (5..10, 5..10) and the
	// outside-clip top-left quadrant (0..5, 0..10) + (0..10, 0..5).
	clip := image.Rect(5, 5, 15, 15)
	compositeFilterOutputOntoTarget(r, src, 0, 0, clip, filters.InterpolationSpaceSRGB)

	// Inside-clip pixels must be red; outside-clip pixels must remain white.
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			got := target.RGBAAt(x, y)
			inClip := x >= clip.Min.X && x < clip.Max.X && y >= clip.Min.Y && y < clip.Max.Y
			inSrcArea := x < 10 && y < 10
			var want color.RGBA
			if inClip && inSrcArea {
				want = red
			} else {
				want = white
			}
			if got != want {
				t.Errorf("pixel (%d,%d): want %v, got %v (inClip=%v inSrcArea=%v)",
					x, y, want, got, inClip, inSrcArea)
			}
		}
	}
}
