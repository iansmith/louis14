package filters

import "image"

// MorphologyOperator selects erode (min) or dilate (max), mirroring Blink's
// MorphologyOperatorType enum (fe_morphology.h). Values map 1-1 to Blink's
// FEMORPHOLOGY_OPERATOR_ERODE / FEMORPHOLOGY_OPERATOR_DILATE.
type MorphologyOperator int

const (
	// MorphologyErode takes the per-channel minimum over the kernel window.
	MorphologyErode MorphologyOperator = iota
	// MorphologyDilate takes the per-channel maximum over the kernel window.
	MorphologyDilate
)

// FEMorphology fattens (dilate) or thins (erode) its input by sliding a
// (2*radiusX+1) x (2*radiusY+1) window over the premultiplied pixels and
// writing, per channel, the window's maximum (dilate) or minimum (erode).
// Mirrors platform/graphics/filters/fe_morphology.{h,cc}, which delegates the
// per-pixel work to Skia's MorphologyPaintFilter.
//
// The operation runs directly on premultiplied RGBA bytes — this is exactly
// what Skia's morphology does (it operates on the premultiplied buffer), and
// it is correct because min/max are monotone on each channel independently.
//
// Per SVG Filter Effects 1 §feMorphology:
//   - A negative radius is an error that disables the primitive (Blink clamps
//     to >= 0 via std::max(0, radius)); the builder treats a negative radius as
//     a disabled effect.
//   - A radius of zero disables the effect: "the result is a transparent black
//     image." louis14 mirrors this (RadiusX <= 0 || RadiusY <= 0 → transparent
//     black), matching Blink which produces an empty result for a zero radius.
type FEMorphology struct {
	baseEffect
	Operator MorphologyOperator
	// RadiusX / RadiusY are the half-window extents in device pixels, already
	// clamped to >= 0 by the constructor (Blink's std::max(0, radius)).
	RadiusX int
	RadiusY int
}

// NewFEMorphology constructs a morphology effect. Negative radii are clamped to
// zero, mirroring FEMorphology's std::max(0.0f, radius) in Blink.
func NewFEMorphology(input FilterEffect, space InterpolationSpace,
	op MorphologyOperator, radiusX, radiusY int) *FEMorphology {
	if radiusX < 0 {
		radiusX = 0
	}
	if radiusY < 0 {
		radiusY = 0
	}
	return &FEMorphology{
		baseEffect: baseEffect{inputs: []FilterEffect{input}, space: space},
		Operator:   op,
		RadiusX:    radiusX,
		RadiusY:    radiusY,
	}
}

// MapRect inflates the rect by (RadiusX, RadiusY) on every side — dilate
// spreads opaque coverage outward by the radius, so an effect's reach grows by
// the radius in each direction. Mirrors FEMorphology::MapEffect, which offsets
// the rect by ApplyHorizontalScale(radius_x_) / ApplyVerticalScale(radius_y_).
func (e *FEMorphology) MapRect(r image.Rectangle) image.Rectangle {
	return image.Rect(
		r.Min.X-e.RadiusX, r.Min.Y-e.RadiusY,
		r.Max.X+e.RadiusX, r.Max.Y+e.RadiusY,
	)
}

// ApplyEffect runs the sliding-window min/max over the input buffer.
func (e *FEMorphology) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	out := newRGBA(region)
	// Spec: a zero (or negative, clamped to zero) radius disables the effect —
	// the result is a transparent black image. newRGBA is already zeroed.
	if e.RadiusX <= 0 || e.RadiusY <= 0 {
		return out
	}
	in := firstInput(inputs, region)
	w, h := region.Dx(), region.Dy()
	if w <= 0 || h <= 0 {
		return out
	}

	rx, ry := e.RadiusX, e.RadiusY
	for y := 0; y < h; y++ {
		y0 := y - ry
		if y0 < 0 {
			y0 = 0
		}
		y1 := y + ry
		if y1 >= h {
			y1 = h - 1
		}
		doffRow := y * out.Stride
		for x := 0; x < w; x++ {
			x0 := x - rx
			if x0 < 0 {
				x0 = 0
			}
			x1 := x + rx
			if x1 >= w {
				x1 = w - 1
			}

			// Window extrema per channel. Pixels outside the input data
			// (when the window reaches past the buffer edge before clamping)
			// are transparent black: for dilate the running max ignores them
			// (max with 0 is a no-op), and for erode they would pull the min
			// to 0. Skia's morphology treats out-of-bounds as transparent
			// black, so erode at the boundary correctly shrinks. We model
			// that by seeding the erode min from the implicit transparent
			// area whenever the window extends past an edge.
			var r, g, b, a uint8
			windowClipped := (x-rx < 0) || (x+rx >= w) || (y-ry < 0) || (y+ry >= h)
			if e.Operator == MorphologyErode {
				if windowClipped {
					// Min already 0 on every channel — the clipped window
					// reaches transparent black outside the data.
					out.Pix[doffRow+x*4+3] = 0
					continue
				}
				r, g, b, a = 255, 255, 255, 255
			}
			for sy := y0; sy <= y1; sy++ {
				row := sy * in.Stride
				for sx := x0; sx <= x1; sx++ {
					si := row + sx*4
					if e.Operator == MorphologyDilate {
						if in.Pix[si] > r {
							r = in.Pix[si]
						}
						if in.Pix[si+1] > g {
							g = in.Pix[si+1]
						}
						if in.Pix[si+2] > b {
							b = in.Pix[si+2]
						}
						if in.Pix[si+3] > a {
							a = in.Pix[si+3]
						}
					} else { // erode
						if in.Pix[si] < r {
							r = in.Pix[si]
						}
						if in.Pix[si+1] < g {
							g = in.Pix[si+1]
						}
						if in.Pix[si+2] < b {
							b = in.Pix[si+2]
						}
						if in.Pix[si+3] < a {
							a = in.Pix[si+3]
						}
					}
				}
			}
			di := doffRow + x*4
			out.Pix[di], out.Pix[di+1], out.Pix[di+2], out.Pix[di+3] = r, g, b, a
		}
	}
	return out
}
