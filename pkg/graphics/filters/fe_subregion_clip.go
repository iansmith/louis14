package filters

import "image"

// subregionClippedEffect wraps another FilterEffect and clips its
// output (and inputs) to a primitive subregion rect. Mirrors Blink's
// FilterEffect::subregion_ + the Skia clip applied around each
// primitive's evaluation in fe_*.cc — the primitive subregion both
// clips the output and is used to compute the primitive's evaluation
// rect.
//
// The wrapped effect is evaluated normally, and the result image is
// then masked: pixels OUTSIDE the subregion are zeroed. This is
// equivalent to running the primitive through a Skia clipRect.
type subregionClippedEffect struct {
	baseEffect
	inner     FilterEffect
	subregion image.Rectangle
}

// NewSubregionClippedEffect wraps `inner` so its output is clipped to
// `subregion`. The wrapper's operating space matches the inner's.
func NewSubregionClippedEffect(inner FilterEffect, subregion image.Rectangle) FilterEffect {
	if inner == nil || subregion.Empty() {
		return inner
	}
	return &subregionClippedEffect{
		baseEffect: baseEffect{
			inputs: []FilterEffect{inner},
			space:  inner.OperatingSpace(),
		},
		inner:     inner,
		subregion: subregion,
	}
}

// ApplyEffect copies inner's output into a fresh buffer with
// everything outside the subregion zeroed.
func (e *subregionClippedEffect) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	out := newRGBA(region)
	// The buffer is in "region" coordinates: pixel (0, 0) = region.Min
	// in device space.
	clip := region.Intersect(e.subregion)
	if clip.Empty() {
		return out // entirely transparent
	}
	// Compute buffer-local rect.
	x0 := clip.Min.X - region.Min.X
	y0 := clip.Min.Y - region.Min.Y
	x1 := clip.Max.X - region.Min.X
	y1 := clip.Max.Y - region.Min.Y
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > region.Dx() {
		x1 = region.Dx()
	}
	if y1 > region.Dy() {
		y1 = region.Dy()
	}
	for y := y0; y < y1; y++ {
		soff := y*in.Stride + x0*4
		doff := y*out.Stride + x0*4
		copy(out.Pix[doff:doff+(x1-x0)*4], in.Pix[soff:soff+(x1-x0)*4])
	}
	return out
}
