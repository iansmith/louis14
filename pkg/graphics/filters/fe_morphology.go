package filters

import (
	"image"
	"image/draw"
	"math"
)

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
// A zero or negative radius disables the effect: "the result is the filter
// input image" (SVGFEMorphologyElement::Build, svg_fe_morphology_element.cc:
// 118-121 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Skia rounds each
// radius to whole pixels (layerRadii); both zero is the input itself, and a
// zero on one axis skips that axis's pass (SkMorphologyImageFilter.cpp:
// 72-79, 113-118, 187-220 @ skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e).
type FEMorphology struct {
	baseEffect
	Operator MorphologyOperator
	// RadiusX / RadiusY are the half-window extents in pixels, already
	// clamped to >= 0 by the constructor (Blink's std::max(0.0f, radius),
	// fe_morphology.cc:40-41).
	RadiusX float64
	RadiusY float64
}

// NewFEMorphology constructs a morphology effect. Negative radii are clamped to
// zero, mirroring FEMorphology's std::max(0.0f, radius) in Blink.
func NewFEMorphology(input FilterEffect, space InterpolationSpace,
	op MorphologyOperator, radiusX, radiusY float64) *FEMorphology {
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

// maxMorphologyRadius caps a layer radius (SkMorphologyImageFilter::radii's
// kMaxRadii, crbug.com/1123035).
const maxMorphologyRadius = 256

// layerRadii returns the radii in whole pixels: rounded half up, as Skia's
// LayerSpace<SkISize>::round, and capped at maxMorphologyRadius
// (SkMorphologyImageFilter::radii, SkMorphologyImageFilter.cpp:72-79 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e).
// The radius is clamped as a float first, so the conversion is defined on
// every platform: NaN is no radius (std::max(0.0f, NaN) is 0 in Blink's
// FEMorphology), and +Inf or a huge value is the cap.
func (e *FEMorphology) layerRadii() (rx, ry int) {
	round := func(r float64) int {
		if math.IsNaN(r) {
			return 0
		}
		return int(math.Floor(math.Min(r, maxMorphologyRadius) + 0.5))
	}
	return round(e.RadiusX), round(e.RadiusY)
}

// MapRect inflates the rect by the radii on every side — dilate spreads
// opaque coverage outward by the radius, so an effect's reach grows by the
// radius in each direction. Mirrors FEMorphology::MapEffect, which offsets
// the rect by ApplyHorizontalScale(radius_x_) / ApplyVerticalScale(radius_y_).
func (e *FEMorphology) MapRect(r image.Rectangle) image.Rectangle {
	rx, ry := e.layerRadii()
	return image.Rect(r.Min.X-rx, r.Min.Y-ry, r.Max.X+rx, r.Max.Y+ry)
}

// ApplyEffect runs the min/max as two one-dimensional passes, X then Y, as
// Skia does (SkMorphologyImageFilter::onFilterImage, morphology_pass,
// SkMorphologyImageFilter.cpp:187-220, 260-275 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e): a zero radius skips its pass,
// and both zero is the input itself. Like every primitive it returns a
// 0-origin buffer the size of region.
func (e *FEMorphology) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	out := newRGBA(region)
	draw.Draw(out, out.Bounds(), in, in.Bounds().Min, draw.Src)
	rx, ry := e.layerRadii()
	if rx > 0 {
		out = e.pass(out, rx, axisX)
	}
	if ry > 0 {
		out = e.pass(out, ry, axisY)
	}
	return out
}

// morphologyAxis is the axis a morphology pass runs along.
type morphologyAxis int

const (
	axisX morphologyAxis = iota
	axisY
)

// pass runs the min (erode) or max (dilate) over a window of radius pixels
// either side along axis, on the premultiplied bytes — min/max are monotone
// per channel. Samples outside the buffer are transparent black: they leave
// a dilate's max unchanged and pull an erode's min to zero. src and the
// result are 0-origin.
func (e *FEMorphology) pass(src *image.RGBA, radius int, axis morphologyAxis) *image.RGBA {
	out := image.NewRGBA(src.Rect)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	n, step := w, 4 // pixels along the axis, bytes between them
	if axis == axisY {
		n, step = h, src.Stride
	}
	erode := e.Operator == MorphologyErode
	pick, seed := maxByte, uint8(0)
	if erode {
		pick, seed = minByte, 255
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pos := x
			if axis == axisY {
				pos = y
			}
			lo, hi := pos-radius, pos+radius
			if erode && (lo < 0 || hi >= n) {
				continue // the window reaches transparent black
			}
			lo, hi = max(lo, 0), min(hi, n-1)
			base := y*src.Stride + x*4 - pos*step
			acc := [4]uint8{seed, seed, seed, seed}
			for k := lo; k <= hi; k++ {
				si := base + k*step
				for c := range acc {
					acc[c] = pick(acc[c], src.Pix[si+c])
				}
			}
			di := y*out.Stride + x*4
			copy(out.Pix[di:di+4], acc[:])
		}
	}
	return out
}

// minByte and maxByte are the per-channel erode and dilate operations.
func minByte(a, b uint8) uint8 { return min(a, b) }
func maxByte(a, b uint8) uint8 { return max(a, b) }
