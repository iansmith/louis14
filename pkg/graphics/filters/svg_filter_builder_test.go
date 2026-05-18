package filters

import (
	"image"
	"testing"
)

// mockSVGFilterElement is a minimal stub of SVGFilterElement for unit
// testing resolvePrimitiveSubregion. It captures only the data the
// resolver reads — filter region, reference box, user-space origin,
// and the primitiveUnits flag.
type mockSVGFilterElement struct {
	region           image.Rectangle
	refBox           image.Rectangle
	userSpaceOrigin  image.Point
	primUnitsObjBBox bool
}

func (m *mockSVGFilterElement) FilterRegion() image.Rectangle          { return m.region }
func (m *mockSVGFilterElement) ReferenceBox() image.Rectangle          { return m.refBox }
func (m *mockSVGFilterElement) PrimitiveUnitsObjectBoundingBox() bool  { return m.primUnitsObjBBox }
func (m *mockSVGFilterElement) UserSpaceOrigin() image.Point           { return m.userSpaceOrigin }
func (m *mockSVGFilterElement) InterpolationSpace() InterpolationSpace { return InterpolationSpaceSRGB }
func (m *mockSVGFilterElement) Primitives() []SVGFilterPrimitive       { return nil }
func (m *mockSVGFilterElement) FillPaintEffect() FilterEffect          { return nil }
func (m *mockSVGFilterElement) StrokePaintEffect() FilterEffect        { return nil }

// mockSVGFilterPrimitive is a minimal SVGFilterPrimitive stub holding
// raw attribute strings. resolvePrimitiveSubregion only calls
// Attribute() on the primitive.
type mockSVGFilterPrimitive struct {
	tag   string
	attrs map[string]string
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
func (p *mockSVGFilterPrimitive) LightSource() LightSource             { return nil }

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
		refBox:          image.Rect(8, 8, 108, 108),
		userSpaceOrigin: image.Point{X: 8, Y: 8},
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
	got := resolvePrimitiveSubregion(elt, prim)
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
		refBox:          image.Rect(50, 50, 150, 150),
		userSpaceOrigin: image.Point{X: 50, Y: 50},
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
	got := resolvePrimitiveSubregion(elt, prim)
	want := image.Rect(50, 50, 150, 150)
	if got != want {
		t.Errorf("resolvePrimitiveSubregion = %v, want %v", got, want)
	}
}

// TestResolvePrimitiveSubregion_UserSpaceOnUsePercentWidth covers the
// `<feDropShadow width="100%">` failure shape. Per SVG Filter Effects 1
// §3.2, percentages on filter primitive subregion attrs resolve against
// the filter region dimensions (Blink mirrors this via an
// SVGLengthContext sized to the filter region). Before the fix,
// parseLength returned 1.0 for "100%" and the resolver ignored its
// pct-suffix signal — collapsing the primitive to a 1-pixel wide rect.
func TestResolvePrimitiveSubregion_UserSpaceOnUsePercentWidth(t *testing.T) {
	elt := &mockSVGFilterElement{
		region:          image.Rect(-10, -10, 110, 110),
		refBox:          image.Rect(0, 0, 100, 100),
		userSpaceOrigin: image.Point{X: 0, Y: 0},
	}
	prim := &mockSVGFilterPrimitive{
		tag:   "fedropshadow",
		attrs: map[string]string{"width": "100%"},
	}
	got := resolvePrimitiveSubregion(elt, prim)
	// width="100%" → 100% × region.Dx() (120) = 120.
	// x is unspecified → x defaults to region.Min.X = -10.
	// y/height are unspecified → default to full region (-10..110).
	want := image.Rect(-10, -10, 110, 110)
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
		refBox:          image.Rect(8, 8, 208, 208),
		userSpaceOrigin: image.Point{X: 8, Y: 8},
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
	got := resolvePrimitiveSubregion(elt, prim)
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
		refBox:          image.Rect(8, 8, 108, 108),
		userSpaceOrigin: image.Point{X: 8, Y: 8},
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
	got := resolvePrimitiveSubregion(elt, prim)
	want := image.Rect(28, 38, 78, 78)
	if got != want {
		t.Errorf("resolvePrimitiveSubregion = %v, want %v", got, want)
	}
}
