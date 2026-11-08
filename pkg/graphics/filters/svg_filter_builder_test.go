package filters

import (
	"image"
	"math"
	"testing"

	"louis14/pkg/geometry"
)

// mockSVGFilterElement is a minimal stub of SVGFilterElement for unit
// testing resolvePrimitiveSubregion. It captures only the data the
// resolver reads — filter region, reference box, user-space origin,
// and the primitiveUnits flag.
type mockSVGFilterElement struct {
	region           image.Rectangle
	refBox           geometry.RectF
	subregionVp      geometry.RectF
	userSpaceOrigin  geometry.PointF
	primUnitsObjBBox bool
	mapping          Mapping
}

func (m *mockSVGFilterElement) FilterRegion() image.Rectangle { return m.region }

// ParamFilterRegion is m.region mapped back into parameter space.
func (m *mockSVGFilterElement) ParamFilterRegion() geometry.RectF {
	r := geometry.NewRectF(float64(m.region.Min.X), float64(m.region.Min.Y), float64(m.region.Dx()), float64(m.region.Dy()))
	p, _ := MapRectAsM33(m.mapping.ParamToLayer.InverseOrIdentity(), r)
	return p
}
func (m *mockSVGFilterElement) SubregionViewport() geometry.RectF      { return m.subregionVp }
func (m *mockSVGFilterElement) ReferenceBox() geometry.RectF           { return m.refBox }
func (m *mockSVGFilterElement) PrimitiveUnitsObjectBoundingBox() bool  { return m.primUnitsObjBBox }
func (m *mockSVGFilterElement) UserSpaceOrigin() geometry.PointF       { return m.userSpaceOrigin }
func (m *mockSVGFilterElement) Mapping() Mapping                       { return m.mapping }
func (m *mockSVGFilterElement) InterpolationSpace() InterpolationSpace { return InterpolationSpaceSRGB }
func (m *mockSVGFilterElement) PrimitiveInterpolationSpace(SVGFilterPrimitive) InterpolationSpace {
	return InterpolationSpaceSRGB
}
func (m *mockSVGFilterElement) Primitives() []SVGFilterPrimitive { return nil }
func (m *mockSVGFilterElement) FillPaintEffect() FilterEffect    { return nil }
func (m *mockSVGFilterElement) StrokePaintEffect() FilterEffect  { return nil }

// mockSVGFilterPrimitive is a minimal SVGFilterPrimitive stub holding
// raw attribute strings. resolvePrimitiveSubregion only calls
// Attribute() on the primitive.
type mockSVGFilterPrimitive struct {
	tag   string
	attrs map[string]string
	light LightSource
	// imageSourceCalls counts ResolveImageSource calls.
	imageSourceCalls int
}

func (p *mockSVGFilterPrimitive) TagName() string { return p.tag }
func (p *mockSVGFilterPrimitive) Attribute(name string) (string, bool) {
	v, ok := p.attrs[name]
	return v, ok
}
func (p *mockSVGFilterPrimitive) Children() []SVGFilterPrimitive             { return nil }
func (p *mockSVGFilterPrimitive) FloodColor() (uint8, uint8, uint8, float64) { return 0, 0, 0, 1 }
func (p *mockSVGFilterPrimitive) TaintsOrigin() bool                         { return false }
func (p *mockSVGFilterPrimitive) LightingColor() (uint8, uint8, uint8, float64) {
	return 255, 255, 255, 1
}
func (p *mockSVGFilterPrimitive) SurfaceScale() float64                { return 1 }
func (p *mockSVGFilterPrimitive) DiffuseConstant() float64             { return 1 }
func (p *mockSVGFilterPrimitive) SpecularConstant() float64            { return 1 }
func (p *mockSVGFilterPrimitive) SpecularExponent() float64            { return 1 }
func (p *mockSVGFilterPrimitive) KernelUnitLength() (float64, float64) { return 1, 1 }
func (p *mockSVGFilterPrimitive) LightSource() LightSource             { return p.light }
func (p *mockSVGFilterPrimitive) ResolveImageSource(filterRegion, subregion image.Rectangle) (*image.RGBA, image.Rectangle, bool) {
	p.imageSourceCalls++
	return nil, image.Rectangle{}, false
}

// TestResolvePrimitiveSubregion_UserSpaceOnUseAnchoredAtUserOrigin
// covers the canonical bucket-J failure: an SVG inline in HTML body
// (so its user-space origin in device coords is (8, 8) due to the body
// margin), a filter using filterUnits="userSpaceOnUse" with the spec
// default 10% expansion (region.Min = (-2, -2)), and a primitive
// declaring x="0" y="0" width="100" height="100".
//
// Per SVG Filter Effects 1 §15.4, x="0" in userSpaceOnUse maps to
// user-space coordinate 0, which is device pixel 8 — NOT region.Min.X
// = -2 (which includes the 10% filter-region expansion). The correct
// anchor is UserSpaceOrigin, not FilterRegion.Min.
func TestResolvePrimitiveSubregion_UserSpaceOnUseAnchoredAtUserOrigin(t *testing.T) {
	elt := &mockSVGFilterElement{
		region:          image.Rect(-2, -2, 110, 110),
		refBox:          geometry.NewRectF(8, 8, 100, 100),
		userSpaceOrigin: geometry.PointF{X: 8, Y: 8},
	}
	prim := &mockSVGFilterPrimitive{
		tag: "fedisplacementmap",
		attrs: map[string]string{
			"x":      "0",
			"y":      "0",
			"width":  "100",
			"height": "100",
		},
	}
	got, _ := resolvePrimitiveSubregion(elt, prim)
	want := image.Rect(8, 8, 108, 108)
	if got != want {
		t.Errorf("resolvePrimitiveSubregion = %v, want %v", got, want)
	}
}

// TestResolvePrimitiveSubregion_UserSpaceOnUseNonOrigin checks the
// generalization: arbitrary user-space origin coordinates anchor the
// primitive subregion, not the filter-region origin.
func TestResolvePrimitiveSubregion_UserSpaceOnUseNonOrigin(t *testing.T) {
	elt := &mockSVGFilterElement{
		region:          image.Rect(40, 40, 150, 150),
		refBox:          geometry.NewRectF(50, 50, 100, 100),
		userSpaceOrigin: geometry.PointF{X: 50, Y: 50},
	}
	prim := &mockSVGFilterPrimitive{
		tag: "fedisplacementmap",
		attrs: map[string]string{
			"x":      "0",
			"y":      "0",
			"width":  "100",
			"height": "100",
		},
	}
	got, _ := resolvePrimitiveSubregion(elt, prim)
	want := image.Rect(50, 50, 150, 150)
	if got != want {
		t.Errorf("resolvePrimitiveSubregion = %v, want %v", got, want)
	}
}

// TestResolvePrimitiveSubregion_UserSpaceOnUsePercentWidth covers the
// `<feFlood width="100%">` shape where the filter region carries the
// default 10% expansion (region 120×120) but the subregion viewport is
// the reference box (100×100). Per SVG Filter Effects 1 §7.11 a percent
// subregion length in userSpaceOnUse resolves against the nearest
// VIEWPORT, not the filter region — verified against Blink's
// LayoutSVGResourceContainer::ResolveRectangle, whose userSpaceOnUse
// branch resolves against a viewport size, and against the WPT reftest
// svg-filter-primitive-units-user-space (HTML box: green flood is exactly
// 100×100 = the border box, not 120×120 = the expanded region).
func TestResolvePrimitiveSubregion_UserSpaceOnUsePercentWidth(t *testing.T) {
	elt := &mockSVGFilterElement{
		region:          image.Rect(-10, -10, 110, 110),
		refBox:          geometry.NewRectF(0, 0, 100, 100),
		subregionVp:     geometry.NewRectF(0, 0, 100, 100),
		userSpaceOrigin: geometry.PointF{X: 0, Y: 0},
	}
	prim := &mockSVGFilterPrimitive{
		tag:   "feflood",
		attrs: map[string]string{"width": "100%"},
	}
	got, _ := resolvePrimitiveSubregion(elt, prim)
	// width="100%" → 100% × viewport.Dx() (100) = 100.
	// x is unspecified → x defaults to region.Min.X = -10, so the rect
	// spans -10..90.
	// y/height are unspecified → default to full region (-10..110).
	want := image.Rect(-10, -10, 90, 110)
	if got != want {
		t.Errorf("resolvePrimitiveSubregion = %v, want %v", got, want)
	}
}

// TestResolvePrimitiveSubregion_UserSpaceOnUsePercentAllAxes covers
// percentage values on every axis simultaneously — verifies x/y % bases
// use width/height respectively (not transposed) and that x/y still get
// offset by UserSpaceOrigin.
func TestResolvePrimitiveSubregion_UserSpaceOnUsePercentAllAxes(t *testing.T) {
	elt := &mockSVGFilterElement{
		region:          image.Rect(8, 8, 208, 208),
		refBox:          geometry.NewRectF(8, 8, 200, 200),
		userSpaceOrigin: geometry.PointF{X: 8, Y: 8},
	}
	prim := &mockSVGFilterPrimitive{
		tag: "fegaussianblur",
		attrs: map[string]string{
			"x":      "25%",
			"y":      "50%",
			"width":  "50%",
			"height": "25%",
		},
	}
	// regionW = 200, regionH = 200.
	// x = userOrigin.X (8) + 25% × 200 = 8 + 50 = 58.
	// y = userOrigin.Y (8) + 50% × 200 = 8 + 100 = 108.
	// w = 50% × 200 = 100.
	// h = 25% × 200 = 50.
	got, _ := resolvePrimitiveSubregion(elt, prim)
	want := image.Rect(58, 108, 158, 158)
	if got != want {
		t.Errorf("resolvePrimitiveSubregion = %v, want %v", got, want)
	}
}

// TestResolvePrimitiveSubregion_UserSpaceOnUseExplicitOffset checks
// non-zero x/y values: they add to the user-space origin, not the
// filter-region origin.
func TestResolvePrimitiveSubregion_UserSpaceOnUseExplicitOffset(t *testing.T) {
	elt := &mockSVGFilterElement{
		region:          image.Rect(-2, -2, 110, 110),
		refBox:          geometry.NewRectF(8, 8, 100, 100),
		userSpaceOrigin: geometry.PointF{X: 8, Y: 8},
	}
	prim := &mockSVGFilterPrimitive{
		tag: "fedisplacementmap",
		attrs: map[string]string{
			"x":      "20",
			"y":      "30",
			"width":  "50",
			"height": "40",
		},
	}
	got, _ := resolvePrimitiveSubregion(elt, prim)
	want := image.Rect(28, 38, 78, 78)
	if got != want {
		t.Errorf("resolvePrimitiveSubregion = %v, want %v", got, want)
	}
}

// refBoxElement is a filter element with a 100×50 reference box at (10, 10),
// in objectBoundingBox primitive units when obb is set.
func refBoxElement(obb bool) *mockSVGFilterElement {
	return &mockSVGFilterElement{region: image.Rect(0, 0, 200, 200), refBox: geometry.NewRectF(10, 10, 100, 50), primUnitsObjBBox: obb}
}

// buildPrimitive builds one primitive of elt's filter.
func buildPrimitive(elt SVGFilterElement, prim *mockSVGFilterPrimitive) FilterEffect {
	return NewSVGFilterBuilder(InterpolationSpaceSRGB).buildOnePrimitive(elt, prim)
}

// With primitiveUnits="objectBoundingBox" a primitive's lengths are fractions
// of the reference box, scaled by its width (x lengths) and height (y
// lengths): Filter::ApplyHorizontalScale / ApplyVerticalScale (filter.cc:
// 65-75 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), applied by FEOffset
// (fe_offset.cc:55-65), FEGaussianBlur (fe_gaussian_blur.cc:74-83),
// FEDropShadow (fe_drop_shadow.cc:59-72) and FEDisplacementMap's scale
// (fe_displacement_map.cc:126, horizontally). The reference box here is
// 100×50.
func TestBuildPrimitive_ObjectBoundingBoxUnitsScaleLengths(t *testing.T) {
	elt := refBoxElement(true)
	build := func(tag string, attrs map[string]string) FilterEffect {
		return buildPrimitive(elt, &mockSVGFilterPrimitive{tag: tag, attrs: attrs})
	}
	if e, ok := build("feoffset", map[string]string{"dx": "0.2", "dy": "0.3"}).(*FEOffset); !ok || e.DX != 20 || e.DY != 15 {
		t.Errorf("feOffset = %+v, want dx 20, dy 15", e)
	}
	if e, ok := build("fegaussianblur", map[string]string{"stdDeviation": "0.1 0.2"}).(*FEGaussianBlur); !ok || e.StdDevX != 10 || e.StdDevY != 10 {
		t.Errorf("feGaussianBlur = %+v, want stdDeviation 10 10", e)
	}
	if e, ok := build("fedropshadow", map[string]string{"dx": "0.1", "dy": "0.1", "stdDeviation": "0.02"}).(*FEDropShadow); !ok || e.DX != 10 || e.DY != 5 || e.StdDevX != 2 || e.StdDevY != 1 {
		t.Errorf("feDropShadow = %+v, want dx 10, dy 5, stdDeviation 2 1", e)
	}
	if e, ok := build("fedisplacementmap", map[string]string{"scale": "0.1"}).(*FEDisplacementMap); !ok || e.Scale != 10 {
		t.Errorf("feDisplacementMap = %+v, want scale 10", e)
	}
}

// feDropShadow's stdDeviation defaults to 2, like its dx and dy
// (SVGFEDropShadowElement, svg_fe_drop_shadow_element.cc:40-45 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and scales by the reference box
// under objectBoundingBox units.
func TestBuildPrimitive_DropShadowStdDeviationDefault(t *testing.T) {
	for _, c := range []struct {
		obb          bool
		wantX, wantY float64
	}{{false, 2, 2}, {true, 200, 100}} {
		if e, ok := buildPrimitive(refBoxElement(c.obb), &mockSVGFilterPrimitive{tag: "fedropshadow"}).(*FEDropShadow); !ok || e.StdDevX != c.wantX || e.StdDevY != c.wantY {
			t.Errorf("objectBoundingBox=%v: feDropShadow = %+v, want stdDeviation %v %v", c.obb, e, c.wantX, c.wantY)
		}
	}
}

// Under objectBoundingBox units, light positions and spot-light targets are
// fractions of the reference box: x*w+ref.x, y*h+ref.y and
// z*sqrt((w²+h²)/2) (Filter::Resolve3dPoint, filter.cc:77-87, called from
// SVGFEPointLightElement / SVGFESpotLightElement::GetLightSource,
// svg_fe_point_light_element.cc:33 and svg_fe_spot_light_element.cc:33-34 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBuildPrimitive_ObjectBoundingBoxUnitsResolveLightPositions(t *testing.T) {
	elt := refBoxElement(true)
	z := 0.1 * math.Sqrt((100*100+50*50)/2.0)

	point := &mockSVGFilterPrimitive{tag: "fediffuselighting", light: NewPointLightSource(0.5, 0.2, 0.1)}
	if f := buildPrimitive(elt, point); f == nil {
		t.Errorf("fediffuselighting built nothing")
	} else if d, ok := f.(*FEDiffuseLighting); !ok {
		t.Errorf("fediffuselighting built %T", f)
	} else if p, ok := d.LightSource.(*PointLightSource); !ok || !approxEq(p.X, 60) || !approxEq(p.Y, 20) || !approxEq(p.Z, z) {
		t.Errorf("point light = %+v, want (60, 20, %v)", d.LightSource, z)
	}

	spot := &mockSVGFilterPrimitive{tag: "fespecularlighting", light: NewSpotLightSource(0.5, 0.2, 0.1, 1, 1, 0, 1, 90)}
	if f := buildPrimitive(elt, spot); f == nil {
		t.Errorf("fespecularlighting built nothing")
	} else if s, ok := f.(*FESpecularLighting); !ok {
		t.Errorf("fespecularlighting built %T", f)
	} else if l, ok := s.LightSource.(*SpotLightSource); !ok || !approxEq(l.X, 60) || !approxEq(l.Y, 20) || !approxEq(l.Z, z) ||
		!approxEq(l.PointsAtX, 110) || !approxEq(l.PointsAtY, 60) || l.PointsAtZ != 0 {
		t.Errorf("spot light = %+v, want (60, 20, %v) pointing at (110, 60, 0)", s.LightSource, z)
	}
}

// A malformed stdDeviation (not one or two numbers) resets to the
// primitive's initial value: SVGNumberOptionalNumber::SetValueAsString
// reports a parse error (svg_number_optional_number.cc:64-75), and the
// animated property then calls SetInitial (svg_animated_property.h:177-179
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBuildPrimitive_MalformedStdDeviationResetsToInitial(t *testing.T) {
	elt := refBoxElement(false)
	for _, v := range []string{"abc", "3 x", "1 2 3"} {
		build := func(tag string) FilterEffect {
			return buildPrimitive(elt, &mockSVGFilterPrimitive{tag: tag, attrs: map[string]string{"stdDeviation": v}})
		}
		if e, ok := build("fedropshadow").(*FEDropShadow); !ok || e.StdDevX != 2 || e.StdDevY != 2 {
			t.Errorf("feDropShadow stdDeviation=%q: %+v, want 2 2", v, e)
		}
		if e, ok := build("fegaussianblur").(*FEGaussianBlur); !ok || e.StdDevX != 0 || e.StdDevY != 0 {
			t.Errorf("feGaussianBlur stdDeviation=%q: %+v, want 0 0", v, e)
		}
	}
}

// FEDisplacementMap::MapEffect pads each axis by half the scale scaled for
// that axis (fe_displacement_map.cc:43-48 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), though the kernel applies the
// horizontal scale to both: 0.2 of a 100×50 box pads 10px across and 5px
// down.
func TestBuildPrimitive_DisplacementMapPadsEachAxis(t *testing.T) {
	f := buildPrimitive(refBoxElement(true), &mockSVGFilterPrimitive{tag: "fedisplacementmap", attrs: map[string]string{"scale": "0.2"}})
	e, ok := f.(*FEDisplacementMap)
	if !ok {
		t.Fatalf("fedisplacementmap built %T", f)
	}
	if got, want := e.MapRect(image.Rect(0, 0, 10, 10)), image.Rect(-10, -5, 20, 15); got != want {
		t.Errorf("MapRect = %v, want %v", got, want)
	}
}

// Number-optional-number attributes parse with the SVG number grammar:
// no inf, NaN or hex floats, and one optional comma between the numbers
// (GenericParseNumber and ParseNumberOptionalNumber,
// svg_parser_utilities.cc:42-207 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// a trailing delimiter after the last number is skipped
// (SkipOptionalSVGSpacesOrDelimiter, svg_parser_utilities.h:71-86). A parse
// error resets the attribute to its initial value.
func TestBuildPrimitive_NumberOptionalNumberGrammar(t *testing.T) {
	elt := refBoxElement(false)
	build := func(tag, attr, v string) FilterEffect {
		return buildPrimitive(elt, &mockSVGFilterPrimitive{tag: tag, attrs: map[string]string{attr: v}})
	}
	for _, v := range []string{"inf", "NaN", "0x1p1", "1,,2", ",3", "1e"} {
		if e, ok := build("fedropshadow", "stdDeviation", v).(*FEDropShadow); !ok || e.StdDevX != 2 || e.StdDevY != 2 {
			t.Errorf("feDropShadow stdDeviation=%q: %+v, want 2 2", v, e)
		}
		if e, ok := build("femorphology", "radius", v).(*FEMorphology); !ok || e.RadiusX != 0 || e.RadiusY != 0 {
			t.Errorf("feMorphology radius=%q: %+v, want 0 0", v, e)
		}
		if e, ok := build("feturbulence", "baseFrequency", v).(*FETurbulence); !ok || e.BaseFreqX != 0 || e.BaseFreqY != 0 {
			t.Errorf("feTurbulence baseFrequency=%q: %+v, want 0 0", v, e)
		}
	}
	for _, c := range []struct {
		v    string
		x, y float64
	}{{"1,2", 1, 2}, {" 1 , 2 ", 1, 2}, {"3", 3, 3}, {"1e1 .5", 10, 0.5}, {"1 2,", 1, 2}} {
		if e, ok := build("fedropshadow", "stdDeviation", c.v).(*FEDropShadow); !ok || e.StdDevX != c.x || e.StdDevY != c.y {
			t.Errorf("feDropShadow stdDeviation=%q: %+v, want %v %v", c.v, e, c.x, c.y)
		}
	}
}

// A negative baseFrequency still builds a turbulence primitive, which runs at
// the initial "0 0": SVGFETurbulenceElement::Build always builds one
// (svg_fe_turbulence_element.cc:138-146 @ a9f50e52), and FETurbulence::CreateImageFilter
// treats a negative frequency as unspecified (fe_turbulence.cc:116-124 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBuildPrimitive_TurbulenceNegativeBaseFrequency(t *testing.T) {
	f := buildPrimitive(refBoxElement(false), &mockSVGFilterPrimitive{tag: "feturbulence", attrs: map[string]string{"baseFrequency": "-0.1 0.2"}})
	if e, ok := f.(*FETurbulence); !ok || e.BaseFreqX != 0 || e.BaseFreqY != 0 {
		t.Errorf("feTurbulence baseFrequency=\"-0.1 0.2\" built %#v, want a turbulence at 0 0", f)
	}
}
