package filters

import (
	"image"
	"math"
	"testing"

	"louis14/pkg/geometry"
)

// A primitive's attributes are in parameter space, and the builder maps them
// into the layer the graph runs in, as Skia maps each image filter's
// parameters through skif::Mapping::paramToLayer (src/core/
// SkImageFilterTypes.h @ skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e).
// LOU-774.

// scaledElement is refBoxElement with its parameter space scaled by (sx, sy)
// and translated by (tx, ty) into the layer.
func scaledElement(obb bool, sx, sy, tx, ty float64) *mockSVGFilterElement {
	elt := refBoxElement(obb)
	m := geometry.MakeScale(sx, sy)
	m.PostTranslate(tx, ty)
	elt.mapping = Mapping{ParamToLayer: m}
	return elt
}

func TestSVGFilterBuilder_MapsOffsetIntoLayer(t *testing.T) {
	prim := &mockSVGFilterPrimitive{tag: "feoffset", attrs: map[string]string{"dx": "10", "dy": "-3"}}
	off := buildPrimitive(scaledElement(false, 2, 3, 7, 7), prim).(*FEOffset)
	if off.DX != 20 || off.DY != -9 {
		t.Errorf("feOffset (10, -3) under scale(2, 3) = (%v, %v), want (20, -9): a vector ignores the translation", off.DX, off.DY)
	}
	// objectBoundingBox: dx is a fraction of the 100-wide box, then mapped.
	prim = &mockSVGFilterPrimitive{tag: "feoffset", attrs: map[string]string{"dx": "0.1"}}
	if off := buildPrimitive(scaledElement(true, 2, 1, 0, 0), prim).(*FEOffset); off.DX != 20 {
		t.Errorf("objectBoundingBox feOffset 0.1 of 100 under scale 2 = %v, want 20", off.DX)
	}
}

func TestSVGFilterBuilder_MapsLengthsKeepingSign(t *testing.T) {
	prim := &mockSVGFilterPrimitive{tag: "fegaussianblur", attrs: map[string]string{"stdDeviation": "4 -1"}}
	blur := buildPrimitive(scaledElement(false, 2, 3, 0, 0), prim).(*FEGaussianBlur)
	if blur.StdDevX != 8 || blur.StdDevY != -3 {
		t.Errorf("stdDeviation (4, -1) under scale(2, 3) = (%v, %v), want (8, -3): a negative deviation stays invalid", blur.StdDevX, blur.StdDevY)
	}
}

func TestSVGFilterBuilder_MapsLightIntoLayer(t *testing.T) {
	prim := &mockSVGFilterPrimitive{tag: "fediffuselighting", light: NewPointLightSource(10, 20, 30)}
	lit := buildPrimitive(scaledElement(false, 2, 4, 5, 6), prim).(*FEDiffuseLighting)
	p := lit.LightSource.(*PointLightSource)
	// x, y map as a point; z as the midpoint of (30, 30) mapped, (60+120)/2.
	if p.X != 25 || p.Y != 86 || p.Z != 90 {
		t.Errorf("point light (10, 20, 30) = (%v, %v, %v), want (25, 86, 90)", p.X, p.Y, p.Z)
	}
	// The surface scale (1) is a depth too: (2+4)/2.
	if lit.SurfaceScale != 3 {
		t.Errorf("surfaceScale 1 = %v, want 3", lit.SurfaceScale)
	}
}

// A distant light's direction maps as a vector, so a non-uniform scale turns
// it: azimuth 45° under scale(1, 3) points along (1, 3).
func TestSVGFilterBuilder_MapsDistantLightIntoLayer(t *testing.T) {
	prim := &mockSVGFilterPrimitive{tag: "fediffuselighting", light: NewDistantLightSource(45, 0)}
	l := buildPrimitive(scaledElement(false, 1, 3, 0, 0), prim).(*FEDiffuseLighting).LightSource.(*DistantLightSource)
	if want := math.Atan2(3, 1) * 180 / math.Pi; math.Abs(l.AzimuthDeg-want) > 1e-9 || math.Abs(l.ElevationDeg) > 1e-9 {
		t.Errorf("distant light (45°, 0°) under scale(1, 3) = (%v°, %v°), want (%v°, 0°)", l.AzimuthDeg, l.ElevationDeg, want)
	}
}

func TestSVGFilterBuilder_MapsSubregionIntoLayer(t *testing.T) {
	elt := scaledElement(false, 2, 1, 4, 0)
	elt.userSpaceOrigin = geometry.PointF{X: 1, Y: 0}
	prim := &mockSVGFilterPrimitive{tag: "feoffset", attrs: map[string]string{"x": "10", "width": "5"}}
	// x: origin 1 + 10 = 11, width 5 → param [11, 16] → layer [26, 36]. y and
	// height take the region's, mapped back and forth: [0, 200].
	if got, _ := resolvePrimitiveSubregion(elt, prim); got != image.Rect(26, 0, 36, 200) {
		t.Errorf("subregion = %v, want %v", got, image.Rect(26, 0, 36, 200))
	}
}

// The noise is sampled in parameter space: under scale(3) the layer pixel
// whose centre is 3k+1.5 samples parameter point k+0.5, the centre of
// parameter pixel k.
func TestFETurbulence_SamplesInParameterSpace(t *testing.T) {
	plain := NewFETurbulence(InterpolationSpaceSRGB, TurbulenceTurbulence, 0.05, 0.05, 2, 3, false)
	scaled := NewFETurbulence(InterpolationSpaceSRGB, TurbulenceTurbulence, 0.05, 0.05, 2, 3, false)
	scaled.Mapping = Mapping{ParamToLayer: geometry.MakeScale(3, 3)}
	a := plain.ApplyEffect(nil, image.Rect(0, 0, 10, 10))
	b := scaled.ApplyEffect(nil, image.Rect(0, 0, 30, 30))
	for k := 0; k < 10; k++ {
		if got, want := b.RGBAAt(3*k+1, 3*k+1), a.RGBAAt(k, k); got != want {
			t.Errorf("scaled noise at layer (%d, %d) = %v, want parameter pixel (%d, %d)'s %v", 3*k+1, 3*k+1, got, k, k, want)
		}
	}
}

// A primitive subregion that maps behind the eye (w <= 0) is empty, not
// absent: the primitive's output is clipped away. Treating it as "no
// subregion" would flood the whole filter region.
func TestSVGFilterBuilder_SubregionBehindEyeIsEmpty(t *testing.T) {
	elt := refBoxElement(false)
	// w = 1 - x/50: parameter x >= 50 is at or behind the eye.
	elt.mapping = Mapping{ParamToLayer: geometry.TransformRowMajor(
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		-1.0/50, 0, 0, 1,
	)}
	prim := &mockSVGFilterPrimitive{tag: "feflood", attrs: map[string]string{"x": "60", "y": "0", "width": "10", "height": "10"}}
	out := buildPrimitive(elt, prim).ApplyEffect(nil, elt.region)
	for i := 3; i < len(out.Pix); i += 4 {
		if out.Pix[i] != 0 {
			t.Fatalf("flood painted pixel %d (alpha %d): want nothing for a subregion behind the eye", i/4, out.Pix[i])
		}
	}
}
