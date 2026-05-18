// paint_filter_effect.go mirrors Blink's
// platform/graphics/filters/paint_filter_effect.{h,cc}. Blink's class:
//
//   class PaintFilterEffect : public FilterEffect {
//    public:
//     PaintFilterEffect(Filter*, const cc::PaintFlags&);
//    private:
//     cc::PaintFlags flags_;
//   };
//
// PaintFilterEffect is the FilterEffect counterpart of the SVG
// `FillPaint` / `StrokePaint` builtin inputs (Filter Effects 1 §15.7).
// When applied, it paints the referencing element's resolved fill or
// stroke paint into the primitive's reference rectangle — supplying
// `<feMerge>` / `<feComposite>` chains with the element's fill/stroke
// as a filter input.
//
// Phase 1 ships the minimum viable surface: a solid RGBA color + a
// reference box. The wire-up from the referencing element's resolved
// paint server is part of Phase 8 (the bucket-J tests that exercise
// FillPaint/StrokePaint directly); Phase 1 only needs to (a) replace
// the LOU-128 alias-to-SourceGraphic so the type exists and (b) keep
// build/vet clean. A gradient/pattern paint server can later embed the
// rasterized server's RGBA buffer into the same struct, reusing the
// existing paint pipeline — same pattern Blink uses where its
// `cc::PaintFlags` can carry a shader.

package filters

import "image"

// PaintFilterEffect renders a resolved fill or stroke paint into the
// effect's reference rectangle. Mirrors Blink PaintFilterEffect
// (platform/graphics/filters/paint_filter_effect.h:13-27). It has no
// input effects — the paint comes from the referencing element, not
// from upstream pipeline output.
type PaintFilterEffect struct {
	baseEffect
	// R, G, B are the resolved paint color components (0–255,
	// non-premultiplied). For a paint server that's not a flat color
	// (gradient, pattern), Phase 8 will extend this struct to also
	// hold a pre-rasterized server buffer; for now flat color covers
	// the bucket-J primitives that read FillPaint/StrokePaint.
	R, G, B uint8
	// A is the resolved paint opacity in [0, 1]
	// (fill-opacity * (color-A from CSS Color L4)).
	A float64
	// RefBox is the referencing element's reference rectangle in
	// absolute device-pixel coordinates. The paint fills this rect
	// inside the filter region; pixels outside RefBox stay
	// transparent. Mirrors how Blink's PaintFilterEffect paints
	// within the primitive subregion (or the reference box when no
	// subregion is set).
	RefBox image.Rectangle
}

// NewPaintFilterEffect constructs a PaintFilterEffect with the given
// resolved color, opacity, reference box, and operating interpolation
// space. The effect has no upstream inputs.
func NewPaintFilterEffect(space InterpolationSpace, r, g, b uint8, a float64, refBox image.Rectangle) *PaintFilterEffect {
	return &PaintFilterEffect{
		baseEffect: baseEffect{space: space},
		R:          r,
		G:          g,
		B:          b,
		A:          a,
		RefBox:     refBox,
	}
}

// ApplyEffect paints the resolved color into the reference box within
// the filter region. Pixels outside RefBox stay transparent — matching
// Blink's behavior where the FillPaint/StrokePaint effect is bounded
// by the element's paint geometry.
func (e *PaintFilterEffect) ApplyEffect(_ []*image.RGBA, region image.Rectangle) *image.RGBA {
	out := newRGBA(region)
	if e.RefBox.Empty() {
		return out
	}
	fill := region.Intersect(e.RefBox)
	if fill.Empty() {
		return out
	}
	px := premultiply(float64(e.R)/255, float64(e.G)/255, float64(e.B)/255, e.A)
	// Convert fill (absolute) to buffer-local coordinates.
	x0 := fill.Min.X - region.Min.X
	y0 := fill.Min.Y - region.Min.Y
	x1 := fill.Max.X - region.Min.X
	y1 := fill.Max.Y - region.Min.Y
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
		row := y * out.Stride
		for x := x0; x < x1; x++ {
			i := row + x*4
			out.Pix[i] = px[0]
			out.Pix[i+1] = px[1]
			out.Pix[i+2] = px[2]
			out.Pix[i+3] = px[3]
		}
	}
	return out
}
