package filters

import (
	"image"
	"math"

	"louis14/pkg/geometry"
)

// Skia citations in this file are pinned to skia @
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e, the `skia_revision` in Chromium
// DEPS @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.

// MatrixCapability is how complex a layer-space matrix a filter graph can be
// evaluated in. Mirrors skif::MatrixCapability (src/core/SkImageFilterTypes.h:
// 552-556); the order matters, a graph's capability is the minimum over its
// effects.
type MatrixCapability int

const (
	// MatrixCapabilityTranslate: the graph only handles an integer
	// translation; the whole CTM is applied after filtering (kTranslate).
	MatrixCapabilityTranslate MatrixCapability = iota
	// MatrixCapabilityScaleTranslate: the graph handles a scale + translate
	// layer matrix; rotation, skew and perspective are applied after
	// filtering (kScaleTranslate, SkImageFilter_Base's default).
	MatrixCapabilityScaleTranslate
	// MatrixCapabilityComplex: the graph handles any layer matrix, so the
	// layer is device space (kComplex).
	MatrixCapabilityComplex
)

// Mapping splits a filter's total CTM into the matrix that maps the filter's
// parameter (local) space into the layer the graph is evaluated in, and the
// matrix that draws that layer into device space. Mirrors skif::Mapping
// (src/core/SkImageFilterTypes.h:564).
type Mapping struct {
	// ParamToLayer maps local (parameter) space into layer space
	// (fParamToLayerMatrix).
	ParamToLayer geometry.Transform
	// LayerToDev maps the filtered layer into device space
	// (fLayerToDevMatrix).
	LayerToDev geometry.Transform
}

// DecomposeCTM splits ctm into ParamToLayer and LayerToDev for a graph of the
// given capability. Mirrors skif::Mapping::decomposeCTM
// (src/core/SkImageFilterTypes.cpp:259-298):
//
//   - kTranslate: the layer is parameter space; the whole CTM is applied when
//     the layer is drawn.
//   - a scale + translate CTM, or a kComplex graph: the layer is device
//     space.
//   - otherwise (rotation, skew, perspective): the layer keeps only the CTM's
//     scale (decompose_transform), and the rest is applied when the layer is
//     drawn.
//
// representativePt is the parameter-space point a perspective CTM's scale is
// measured at (compute_decomposition_center, src/core/SkCanvas.cpp:531-546).
// It reports false when the remainder is not invertible, in which case the
// layer is not drawn.
func (m *Mapping) DecomposeCTM(ctm geometry.Transform, capability MatrixCapability, representativePt geometry.PointF) bool {
	var layer, remainder geometry.Transform
	switch {
	case capability == MatrixCapabilityTranslate:
		remainder = ctm
	case isScaleTranslateAsM33(ctm) || capability == MatrixCapabilityComplex:
		layer = ctm
	default:
		sx, sy := decomposeTransformScale(ctm, representativePt)
		layer = geometry.MakeScale(sx, sy)
		remainder = ctm
		remainder.Scale(1/sx, 1/sy)
	}
	if !remainder.IsInvertible() {
		return false
	}
	m.ParamToLayer = layer
	m.LayerToDev = remainder
	return true
}

// FoldIntegerTranslation moves a ParamToLayer that is an integer
// translation into LayerToDev, so the graph is evaluated in parameter space
// and not rebuilt for the common translate-only CTM. Every primitive's
// geometry moves with the boxes it is built from, by the same integer, so
// the output is unchanged — save feTurbulence, whose noise is sampled at
// layer coordinates: it is now sampled at parameter coordinates, as Skia's
// SkPerlinNoiseShader evaluates noise in its local (parameter) space.
//
// no Skia analog: Skia rebuilds nothing, as its graph is built once and
// mapped at evaluation; louis14 builds its graph in the layer space.
func (m *Mapping) FoldIntegerTranslation() {
	if !m.ParamToLayer.IsIdentityOrIntegerTranslation() {
		return
	}
	m.LayerToDev = m.LayerToDev.Mul(m.ParamToLayer)
	m.ParamToLayer = geometry.Transform{}
}

// scaleTranslateEpsilon absorbs the rounding noise of a CTM recomposed from
// the draw context's float matrix; Skia's IsScaleTranslateAsM33 compares its
// float entries exactly.
const scaleTranslateEpsilon = 1e-9

// isScaleTranslateAsM33 mirrors SkMatrixPriv::IsScaleTranslateAsM33
// (src/core/SkMatrixPriv.h:190-195): no skew and no perspective in the 3×3
// that acts on the z = 0 plane.
func isScaleTranslateAsM33(m geometry.Transform) bool {
	near := func(v, want float64) bool { return math.Abs(v-want) < scaleTranslateEpsilon }
	return near(m.Rc(1, 0), 0) && near(m.Rc(3, 0), 0) &&
		near(m.Rc(0, 1), 0) && near(m.Rc(3, 1), 0) &&
		near(m.Rc(3, 3), 1)
}

// skScalarNearlyZero is SK_ScalarNearlyZero (1 / 4096).
const skScalarNearlyZero = 1.0 / (1 << 12)

// decomposeTransformScale returns the layer scale decompose_transform
// (src/core/SkImageFilterTypes.cpp:93-116) factors out of m's 3×3: the
// lengths of its x and y columns (SkMatrix::decomposeScale,
// src/core/SkMatrix.cpp:1479-1499), or — under perspective, or when a column
// is degenerate — one uniform scale: the square root of the differential area
// scale at p, or 1 when p maps behind w = 0.
func decomposeTransformScale(m geometry.Transform, p geometry.PointF) (sx, sy float64) {
	if !hasPerspectiveAsM33(m) {
		sx = math.Hypot(m.Rc(0, 0), m.Rc(1, 0))
		sy = math.Hypot(m.Rc(0, 1), m.Rc(1, 1))
		if isFinite(sx) && isFinite(sy) && sx > skScalarNearlyZero && sy > skScalarNearlyZero {
			return sx, sy
		}
	}
	s := differentialAreaScale(m, p)
	if isFinite(s) && math.Abs(s) > skScalarNearlyZero {
		s = math.Sqrt(s)
	} else {
		s = 1
	}
	return s, s
}

// hasPerspectiveAsM33 reports whether m's 3×3 on the z = 0 plane has a
// perspective row (SkMatrix::hasPerspective).
func hasPerspectiveAsM33(m geometry.Transform) bool {
	return m.Rc(3, 0) != 0 || m.Rc(3, 1) != 0 || m.Rc(3, 3) != 1
}

// differentialAreaScale is the factor by which m's 3×3 scales area at local
// point p: |det J'| / w³ with J' = [x y w; m00 m10 m20; m01 m11 m21]. Mirrors
// SkMatrixPriv::DifferentialAreaScale (src/core/SkMatrix.cpp:1706-1737).
func differentialAreaScale(m geometry.Transform, p geometry.PointF) float64 {
	m00, m01, m02 := m.Rc(0, 0), m.Rc(0, 1), m.Rc(0, 3)
	m10, m11, m12 := m.Rc(1, 0), m.Rc(1, 1), m.Rc(1, 3)
	m20, m21, m22 := m.Rc(3, 0), m.Rc(3, 1), m.Rc(3, 3)
	x := m00*p.X + m01*p.Y + m02
	y := m10*p.X + m11*p.Y + m12
	w := m20*p.X + m21*p.Y + m22
	if w < skScalarNearlyZero {
		return math.Inf(1)
	}
	det := x*(m10*m21-m20*m11) - y*(m00*m21-m20*m01) + w*(m00*m11-m10*m01)
	return math.Abs(det / (w * w * w))
}

func isFinite(v float64) bool { return !math.IsInf(v, 0) && !math.IsNaN(v) }

// ParamToLayerVector maps a parameter-space vector (an offset) into layer
// space: the difference of the mapped point and the mapped origin. Mirrors
// Mapping::map<Vector> (src/core/SkImageFilterTypes.cpp:361-364) via
// SkMatrix::mapVectors, which subtracts the mapped origin under perspective.
func (m Mapping) ParamToLayerVector(x, y float64) (float64, float64) {
	px, py := mapPointAsM33(m.ParamToLayer, x, y)
	ox, oy := mapPointAsM33(m.ParamToLayer, 0, 0)
	return px - ox, py - oy
}

// ParamToLayerSize maps a parameter-space size (a blur's standard deviations)
// into layer space: the lengths of the mapped x and y basis vectors. Mirrors
// Mapping::map<SkSize> (src/core/SkImageFilterTypes.cpp:372-386).
func (m Mapping) ParamToLayerSize(w, h float64) (float64, float64) {
	xx, xy := m.ParamToLayerVector(w, 0)
	yx, yy := m.ParamToLayerVector(0, h)
	return math.Hypot(xx, xy), math.Hypot(yx, yy)
}

// mapPointAsM33 maps (x, y, 0, 1) through t with the perspective divide.
func mapPointAsM33(t geometry.Transform, x, y float64) (float64, float64) {
	w := t.Rc(3, 0)*x + t.Rc(3, 1)*y + t.Rc(3, 3)
	return (t.Rc(0, 0)*x + t.Rc(0, 1)*y + t.Rc(0, 3)) / w,
		(t.Rc(1, 0)*x + t.Rc(1, 1)*y + t.Rc(1, 3)) / w
}

// ParamToLayerPoint maps a parameter-space point (a light position) into
// layer space. Mirrors Mapping::map<SkPoint>
// (src/core/SkImageFilterTypes.cpp @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e).
func (m Mapping) ParamToLayerPoint(x, y float64) (float64, float64) {
	return mapPointAsM33(m.ParamToLayer, x, y)
}

// ParamToLayerZ maps a parameter-space depth (a light's z, a lighting
// surface scale) into layer space: the midpoint of the vector (z, z) mapped,
// as LayerSpace<ZValue>::Map does (src/effects/imagefilters/
// SkLightingImageFilter.cpp:63-67 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e).
func (m Mapping) ParamToLayerZ(z float64) float64 {
	x, y := m.ParamToLayerVector(z, z)
	return (x + y) / 2
}

// ParamToLayerPixels maps a parameter-space rect into the layer, as
// Mapping::map<SkRect> (src/core/SkImageFilterTypes.cpp @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e) does through SkMatrix::mapRect,
// and rounds it out to the layer pixels covering it (SnapRectOut). ok is
// false, with an empty rect, when a corner maps behind the eye (w <= 0).
func (m Mapping) ParamToLayerPixels(r geometry.RectF) (image.Rectangle, bool) {
	l, ok := MapRectAsM33(m.ParamToLayer, r)
	if !ok {
		return image.Rectangle{}, false
	}
	return SnapRectOut(l), true
}

// MapRectAsM33 maps r's corners through t with the perspective divide and
// returns their bounds; ok is false when a corner maps to w <= 0.
func MapRectAsM33(t geometry.Transform, r geometry.RectF) (geometry.RectF, bool) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range [4]geometry.PointF{
		{X: r.X(), Y: r.Y()}, {X: r.Right(), Y: r.Y()},
		{X: r.X(), Y: r.Bottom()}, {X: r.Right(), Y: r.Bottom()},
	} {
		if t.Rc(3, 0)*p.X+t.Rc(3, 1)*p.Y+t.Rc(3, 3) <= 0 {
			return geometry.RectF{}, false
		}
		x, y := mapPointAsM33(t, p.X, p.Y)
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	return geometry.NewRectF(minX, minY, maxX-minX, maxY-minY), true
}

// SnapRectOut converts a float rect into the integer pixel rect covering it. Edges are first nudged to the nearest integer
// when within a small epsilon (objectBoundingBox math introduces float noise:
// e.g. an exact -10% of a 100px box computes to 7.9999999998, not 8.0), then
// floor/ceil out so the integer region fully covers the float region. Without
// the epsilon snap, int()/floor() of 7.9999999998 truncates to 7 — a 1px
// offset on the filter region top, visible as a 1px seam on the filter output.
// gfx::ToEnclosingRectIgnoringError (ui/gfx/geometry/rect_conversions.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) with a fixed error.
func SnapRectOut(r geometry.RectF) image.Rectangle {
	const eps = 1e-6
	snap := func(v float64) float64 {
		if n := math.Round(v); math.Abs(v-n) < eps {
			return n
		}
		return v
	}
	x0 := snap(r.X())
	y0 := snap(r.Y())
	x1 := snap(r.X() + r.Width())
	y1 := snap(r.Y() + r.Height())
	return image.Rect(
		int(math.Floor(x0)), int(math.Floor(y0)),
		int(math.Ceil(x1)), int(math.Ceil(y1)),
	)
}
