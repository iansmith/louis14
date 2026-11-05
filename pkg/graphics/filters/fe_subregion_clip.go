package filters

import "image"

// subregionClippedEffect wraps another FilterEffect and clips its
// output to a primitive subregion rect. Mirrors Blink's
// FilterEffect::subregion_ + the Skia clip applied around each
// primitive's evaluation in fe_*.cc.
//
// The wrapped effect is evaluated normally, and the result image is
// then masked: pixels OUTSIDE the subregion are zeroed. This is
// equivalent to running the primitive through a Skia clipRect. MapRect
// passes the rect through: the reach a buffer is sized by must not
// shrink to the crop (LOU-704 ports the reverse mapping).
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
	e := &subregionClippedEffect{
		baseEffect: baseEffect{
			inputs: []FilterEffect{inner},
			space:  inner.OperatingSpace(),
		},
		inner:     inner,
		subregion: subregion,
	}
	// The crop is a thin per-pixel clip; the output is as tainted as the
	// input, so feDisplacementMap still refuses it.
	if inner.OriginTainted() {
		e.SetOriginTainted()
	}
	return e
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
