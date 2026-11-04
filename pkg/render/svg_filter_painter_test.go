package render

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/graphics/filters"
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
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			buf.SetRGBA(x, y, c)
		}
	}
	return buf
}

func checkRGBAClose(t *testing.T, got, want color.RGBA, tol int) {
	t.Helper()
	far := func(a, b uint8) bool {
		d := int(a) - int(b)
		return d > tol || d < -tol
	}
	if far(got.R, want.R) || far(got.G, want.G) || far(got.B, want.B) || far(got.A, want.A) {
		t.Errorf("got %v, want %v +-%d", got, want, tol)
	}
}

// TestCompositeFilterOutputOntoTarget_PremultipliedSourceOver — the filter
// output is premultiplied, and reaches the page through SkBlendMode::kSrcOver:
// out = src + dst*(1-srcA). Green (0,128,0) at .75 over white is (64,160,64);
// the former straight-alpha shim gave (64,192,64).
//
// A linearRGB result is first converted to sRGB in unpremultiplied space, and
// composited in sRGB: Blink appends a TransformInterpolationSpace to sRGB at the
// end of the filter (core/paint/filter_effect_builder.cc:500-505,
// platform/graphics/filters/paint_filter_builder.cc:92-101
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestCompositeFilterOutputOntoTarget_PremultipliedSourceOver(t *testing.T) {
	for _, tc := range []struct {
		name  string
		src   color.RGBA
		space filters.InterpolationSpace
		dst   color.RGBA
		want  color.RGBA
	}{
		{
			name:  "sRGB green 75% over white",
			src:   color.RGBA{0, 96, 0, 191},
			space: filters.InterpolationSpaceSRGB,
			dst:   color.RGBA{255, 255, 255, 255},
			want:  color.RGBA{64, 160, 64, 255},
		},
		{
			name:  "sRGB red 50% over transparent",
			src:   color.RGBA{128, 0, 0, 128},
			space: filters.InterpolationSpaceSRGB,
			dst:   color.RGBA{},
			want:  color.RGBA{128, 0, 0, 128},
		},
		{
			// Linear 0.5 is sRGB 0.735; at alpha .5 premultiplied 94, plus
			// half of the white destination. Compositing in linear gives 225.
			name:  "linearRGB gray 50% over white composites in sRGB",
			src:   color.RGBA{64, 64, 64, 128},
			space: filters.InterpolationSpaceLinearRGB,
			dst:   color.RGBA{255, 255, 255, 255},
			want:  color.RGBA{221, 221, 221, 255},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := solidPremultipliedBuffer(tc.dst)
			r := NewRendererForImage(target)
			src := solidPremultipliedBuffer(tc.src)
			compositeFilterOutputOntoTarget(r, src, 0, 0, target.Bounds(), tc.space)
			checkRGBAClose(t, target.RGBAAt(1, 1), tc.want, 1)
		})
	}
}

// TestCompositeBufferWithOpacityOnto_PremultipliedSourceOver — the buffer is
// premultiplied, and a layer's opacity scales all four channels before
// source-over (cc SaveLayerAlphaOp). Opaque green (0,128,0) at .5 over white
// is (128,192,128); the former straight-alpha shim overflowed.
//
// Blink: ConversionContext::StartEffect,
// platform/graphics/compositing/paint_chunks_to_cc_layer.cc:734
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := solidPremultipliedBuffer(white)
			compositeBufferWithOpacityOnto(target, solidPremultipliedBuffer(tc.src), 0, 0, tc.opacity)
			checkRGBAClose(t, target.RGBAAt(1, 1), tc.want, 1)
		})
	}
}
