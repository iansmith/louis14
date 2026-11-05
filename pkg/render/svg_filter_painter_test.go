package render

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/layout/svg"
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
	compositeFilterOutputOntoTarget(r, src, 0, 0, clip)

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

// TestApplyClipCoverageToFilterOutput asserts the kDstIn alpha-coverage
// step that applyClipMaskToFilterOutput relies on for SVG
// `filter` + `clip-path` / `mask` interaction (svg-filter-vs-clip-path,
// svg-filter-vs-mask). Per CSS Masking 1 §3.7 the FILTERED result is
// clipped: a clip-path becomes a binary 0/255 alpha coverage applied to
// the (region-sized) filtered buffer, so the blur halo that extends past
// the clip geometry is erased before compositing.
//
// Setup: a 10x10 filtered buffer of opaque red (modelling the post-blur
// output that has spread to fill the region). A coverage buffer with a
// 6x6 opaque square in the top-left and transparent elsewhere models a
// clip-path covering only part of the region. After applySVGMaskToBuffer
// with SVGMaskTypeAlpha (clip coverage), pixels inside the square keep
// full alpha; pixels outside are zeroed.
func TestApplyClipCoverageToFilterOutput(t *testing.T) {
	const W, H = 10, 10
	const clipW, clipH = 6, 6

	out := image.NewRGBA(image.Rect(0, 0, W, H))
	red := color.RGBA{255, 0, 0, 255}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			out.SetRGBA(x, y, red)
		}
	}

	// Clip coverage: opaque white inside the clipW x clipH square,
	// transparent outside. applySVGMaskToBuffer reads the alpha channel
	// for SVGMaskTypeAlpha.
	cov := image.NewRGBA(image.Rect(0, 0, W, H))
	for y := 0; y < clipH; y++ {
		for x := 0; x < clipW; x++ {
			cov.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}

	applySVGMaskToBuffer(out, cov, svg.SVGMaskTypeAlpha)

	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			got := out.RGBAAt(x, y)
			inClip := x < clipW && y < clipH
			if inClip {
				if got != red {
					t.Errorf("inside-clip pixel (%d,%d): want %v, got %v", x, y, red, got)
				}
			} else if got.A != 0 {
				t.Errorf("outside-clip pixel (%d,%d): want alpha 0, got %v", x, y, got)
			}
		}
	}
}

// solidPremultipliedBuffer returns a 4x4 buffer of one premultiplied colour.
func solidPremultipliedBuffer(c color.RGBA) *image.RGBA {
	buf := image.NewRGBA(image.Rect(0, 0, 4, 4))
	fillRGBA(buf, c)
	return buf
}

// TestCompositeFilterOutputOntoTarget_PremultipliedSourceOver — the filter
// output is premultiplied, and reaches the page through SkBlendMode::kSrcOver:
// out = src + dst*(1-srcA), rounded (rasterPipelineSpriteBlit). Green (0,128,0) at
// .75 over white is (64,160,64); the former straight-alpha shim gave (64,192,64).
//
// A source pixel with RGB > A saturates as Skia's to_unorm does
// (src/opts/SkRasterPipeline_opts.h:2269-2272 @
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e).
func TestCompositeFilterOutputOntoTarget_PremultipliedSourceOver(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  color.RGBA
		dst  color.RGBA
		want color.RGBA
	}{
		{
			name: "green 75% over white",
			src:  color.RGBA{0, 96, 0, 191},
			dst:  color.RGBA{255, 255, 255, 255},
			want: color.RGBA{64, 160, 64, 255},
		},
		{
			name: "red 50% over transparent",
			src:  color.RGBA{128, 0, 0, 128},
			dst:  color.RGBA{},
			want: color.RGBA{128, 0, 0, 128},
		},
		{
			name: "RGB above alpha saturates",
			src:  color.RGBA{255, 0, 0, 128},
			dst:  color.RGBA{255, 255, 255, 255},
			want: color.RGBA{255, 127, 127, 255},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := solidPremultipliedBuffer(tc.dst)
			r := NewRendererForImage(target)
			src := solidPremultipliedBuffer(tc.src)
			compositeFilterOutputOntoTarget(r, src, 0, 0, target.Bounds())
			if got := target.RGBAAt(1, 1); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCompositeBufferWithOpacityOnto_PremultipliedSourceOver pins Skia's float
// pipeline (rasterPipelineSpriteBlit): out = round(s*alpha + d*(1 - sa*alpha)).
// Opaque green (0,128,0) at .5 over white: R = 0 + 127.5 -> 128,
// G = 64 + 127.5 = 191.5 -> 192, A = 255. Layer opacity reaches it through
// SaveLayerAlphaOp, platform/graphics/compositing/paint_chunks_to_cc_layer.cc:734
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestCompositeBufferWithOpacityOnto_PremultipliedSourceOver(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}
	for _, tc := range []struct {
		name    string
		src     color.RGBA
		opacity float64
		want    color.RGBA
	}{
		{"opaque green at .5", color.RGBA{0, 128, 0, 255}, 0.5, color.RGBA{128, 192, 128, 255}},
		{"premultiplied red .5 at 1", color.RGBA{128, 0, 0, 128}, 1, color.RGBA{255, 127, 127, 255}},
		{"premultiplied red .5 at .5", color.RGBA{128, 0, 0, 128}, 0.5, color.RGBA{255, 191, 191, 255}},
		{"RGB above alpha saturates", color.RGBA{255, 0, 0, 128}, 1, color.RGBA{255, 127, 127, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := solidPremultipliedBuffer(white)
			compositeBufferWithOpacityOnto(target, solidPremultipliedBuffer(tc.src), 0, 0, tc.opacity)
			if got := target.RGBAAt(1, 1); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

var placementRed = color.RGBA{255, 0, 0, 255}

// compositePlacementCase describes a 2x2 source placed on a target. The target
// is white except where the source lands; `want` lists exactly where that is.
type compositePlacementCase struct {
	name      string
	targetBox image.Rectangle
	srcBox    image.Rectangle // bounds of the source buffer
	subImage  bool            // source is a sub-image, so Stride != 4*width
	dx, dy    int
	clip      image.Rectangle // filter composite only; empty means the target
	want      image.Rectangle // target pixels the source must cover
}

// newPlacementSource returns an opaque red buffer with bounds box, optionally a
// sub-image of a larger, opaque blue buffer.
func newPlacementSource(box image.Rectangle, subImage bool) *image.RGBA {
	if !subImage {
		src := image.NewRGBA(box)
		fillRGBA(src, placementRed)
		return src
	}
	parent := image.NewRGBA(box.Inset(-3))
	fillRGBA(parent, color.RGBA{0, 0, 255, 255})
	sub := parent.SubImage(box).(*image.RGBA)
	fillRGBA(sub, placementRed)
	return sub
}

func fillRGBA(img *image.RGBA, c color.RGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// checkPlacement asserts target is `inside` over want and white elsewhere.
func checkPlacement(t *testing.T, target *image.RGBA, want image.Rectangle, inside color.RGBA) {
	t.Helper()
	white := color.RGBA{255, 255, 255, 255}
	b := target.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			exp := white
			if image.Pt(x, y).In(want) {
				exp = inside
			}
			if got := target.RGBAAt(x, y); got != exp {
				t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, exp)
			}
		}
	}
}

// TestComposite_OriginsOffsetsAndClipping — both composites place a buffer whose
// bounds need not start at (0,0) onto a target whose bounds need not either.
// compositeFilterOutputOntoTarget maps the buffer's Min to (dx,dy);
// compositeBufferWithOpacityOnto adds (dx,dy) to the buffer's coordinates,
// because paintLayerIsolated passes a buffer already addressed in target space.
func TestComposite_OriginsOffsetsAndClipping(t *testing.T) {
	target8 := image.Rect(0, 0, 8, 8)
	targetFar := image.Rect(100, 100, 108, 108)

	t.Run("filter", func(t *testing.T) {
		for _, tc := range []compositePlacementCase{
			{name: "zero origin", targetBox: target8, srcBox: image.Rect(0, 0, 2, 2), dx: 3, dy: 4, want: image.Rect(3, 4, 5, 6)},
			{name: "buffer origin is ignored", targetBox: target8, srcBox: image.Rect(50, 60, 52, 62), dx: 3, dy: 4, want: image.Rect(3, 4, 5, 6)},
			{name: "target origin", targetBox: targetFar, srcBox: image.Rect(0, 0, 2, 2), dx: 103, dy: 104, want: image.Rect(103, 104, 105, 106)},
			{name: "clipped", targetBox: target8, srcBox: image.Rect(50, 60, 52, 62), dx: 3, dy: 4, clip: image.Rect(4, 0, 8, 5), want: image.Rect(4, 4, 5, 5)},
			{name: "off the target", targetBox: target8, srcBox: image.Rect(0, 0, 2, 2), dx: 7, dy: -1, want: image.Rect(7, 0, 8, 1)},
			{name: "outside the clip", targetBox: target8, srcBox: image.Rect(0, 0, 2, 2), dx: 3, dy: 4, clip: image.Rect(0, 0, 2, 2), want: image.Rectangle{}},
			{name: "sub-image", targetBox: target8, srcBox: image.Rect(10, 10, 12, 12), subImage: true, dx: 3, dy: 4, want: image.Rect(3, 4, 5, 6)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				target := image.NewRGBA(tc.targetBox)
				fillRGBA(target, color.RGBA{255, 255, 255, 255})
				clip := tc.clip
				if clip.Empty() {
					clip = tc.targetBox
				}
				r := NewRendererForImage(target)
				compositeFilterOutputOntoTarget(r, newPlacementSource(tc.srcBox, tc.subImage), tc.dx, tc.dy, clip)
				checkPlacement(t, target, tc.want, placementRed)
			})
		}
	})

	t.Run("opacity", func(t *testing.T) {
		for _, tc := range []compositePlacementCase{
			{name: "zero origin plus offset", targetBox: target8, srcBox: image.Rect(0, 0, 2, 2), dx: 3, dy: 4, want: image.Rect(3, 4, 5, 6)},
			{name: "buffer in target space", targetBox: target8, srcBox: image.Rect(3, 4, 5, 6), want: image.Rect(3, 4, 5, 6)},
			{name: "target origin", targetBox: targetFar, srcBox: image.Rect(103, 104, 105, 106), want: image.Rect(103, 104, 105, 106)},
			{name: "off the target", targetBox: target8, srcBox: image.Rect(0, 0, 2, 2), dx: 7, dy: -1, want: image.Rect(7, 0, 8, 1)},
			{name: "sub-image", targetBox: target8, srcBox: image.Rect(3, 4, 5, 6), subImage: true, want: image.Rect(3, 4, 5, 6)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				target := image.NewRGBA(tc.targetBox)
				fillRGBA(target, color.RGBA{255, 255, 255, 255})
				compositeBufferWithOpacityOnto(target, newPlacementSource(tc.srcBox, tc.subImage), tc.dx, tc.dy, 1)
				checkPlacement(t, target, tc.want, placementRed)
			})
		}
	})
}
