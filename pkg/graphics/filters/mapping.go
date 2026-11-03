package filters

import (
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

// CTMCapability returns the capability of the graph rooted at e: the minimum
// of e's own capability and its inputs'. Mirrors
// SkImageFilter_Base::getCTMCapability (src/core/SkImageFilter.cpp:337-346).
//
// Per-effect capabilities follow the Skia filter each effect is built as:
// color filters (SkColorFilterImageFilter.cpp:53 — Blink's FEColorMatrix,
// FEComponentTransfer and SourceAlpha), shaders (SkShaderImageFilter.cpp:51 —
// FEFlood, PaintFilterEffect), merges (SkMergeImageFilter.cpp:39) and blends
// (SkBlendImageFilter.cpp:69 — FEBlend, FEComposite) are kComplex; every other
// effect keeps the kScaleTranslate default (SkImageFilter_Base.h:225-227).
// SourceGraphic is Skia's null input, which constrains nothing.
func CTMCapability(e FilterEffect) MatrixCapability {
	if e == nil {
		return MatrixCapabilityComplex
	}
	result := MatrixCapabilityScaleTranslate
	switch e.(type) {
	case *SourceGraphic, *SourceAlpha, *FEColorMatrix, *FEComponentTransfer,
		*FEFlood, *PaintFilterEffect, *FEMerge, *FEBlend, *FEComposite:
		result = MatrixCapabilityComplex
	}
	for _, in := range e.InputEffects() {
		result = min(result, CTMCapability(in))
	}
	return result
}

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
