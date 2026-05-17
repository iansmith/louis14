package svg

import (
	"testing"

	"louis14/pkg/geometry"
)

// TestResourceBoundingBox_UserSpaceOnUseExplicitXYAnchoredAtUserOrigin
// covers the symmetric anchor bug for `<filter x="..." y="...">` in
// userSpaceOnUse mode: the explicit x/y values are user-space coords
// and must be shifted by userSpaceOrigin to produce a result in
// target's coordinate space, NOT used raw.
//
// Test scenario: SVG inline in HTML body (user-space origin in device
// coords = (8, 8) due to the body margin). Filter declares
// `<filter filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">`.
// Per SVG Filter Effects 1 §6.5 x="0" is user-space 0, which maps to
// device pixel 8 — not 0.
func TestResourceBoundingBox_UserSpaceOnUseExplicitXYAnchoredAtUserOrigin(t *testing.T) {
	f := &SVGResourceFilter{
		FilterUnits: SVGUnitUserSpaceOnUse,
		X:           SVGGradientLength{Value: 0, HasValue: true},
		Y:           SVGGradientLength{Value: 0, HasValue: true},
		Width:       SVGGradientLength{Value: 100, HasValue: true},
		Height:      SVGGradientLength{Value: 100, HasValue: true},
	}
	// target is the reference box in device coords (a 0x0 element at
	// device (8, 8) per the body margin).
	target := geometry.NewRectF(8, 8, 0, 0)
	userOrigin := geometry.PointF{X: 8, Y: 8}
	got := f.ResourceBoundingBox(target, userOrigin, NewSVGLengthContext(target.Size))
	want := geometry.NewRectF(8, 8, 100, 100)
	if got != want {
		t.Errorf("ResourceBoundingBox = %v, want %v", got, want)
	}
}

// TestResourceBoundingBox_UserSpaceOnUseExplicitNonZero confirms a
// non-zero explicit x/y is also correctly anchored.
func TestResourceBoundingBox_UserSpaceOnUseExplicitNonZero(t *testing.T) {
	f := &SVGResourceFilter{
		FilterUnits: SVGUnitUserSpaceOnUse,
		X:           SVGGradientLength{Value: 20, HasValue: true},
		Y:           SVGGradientLength{Value: 30, HasValue: true},
		Width:       SVGGradientLength{Value: 50, HasValue: true},
		Height:      SVGGradientLength{Value: 60, HasValue: true},
	}
	target := geometry.NewRectF(50, 50, 100, 100)
	userOrigin := geometry.PointF{X: 50, Y: 50}
	got := f.ResourceBoundingBox(target, userOrigin, NewSVGLengthContext(target.Size))
	want := geometry.NewRectF(70, 80, 50, 60)
	if got != want {
		t.Errorf("ResourceBoundingBox = %v, want %v", got, want)
	}
}

// TestResourceBoundingBox_UserSpaceOnUseMissingXYUsesDefaults
// confirms that when x/y are missing the spec-default -10% projection
// off target is preserved (no anchor shift — the default already
// lives in target's coord space).
func TestResourceBoundingBox_UserSpaceOnUseMissingXYUsesDefaults(t *testing.T) {
	f := &SVGResourceFilter{
		FilterUnits: SVGUnitUserSpaceOnUse,
		// X, Y, Width, Height all HasValue=false → use defaults.
	}
	target := geometry.NewRectF(8, 8, 100, 100)
	userOrigin := geometry.PointF{X: 8, Y: 8}
	got := f.ResourceBoundingBox(target, userOrigin, NewSVGLengthContext(target.Size))
	// Defaults: x=-10%, y=-10%, w=120%, h=120% of target.
	// target.X()=8, target.Y()=8, target.Width()=target.Height()=100.
	// x = 8 + (-0.10)*100 = -2; y = -2; w = 120; h = 120.
	// (Account for float-multiply precision noise with 1e-9 tolerance.)
	const eps = 1e-9
	if absDiff(got.X(), -2) > eps ||
		absDiff(got.Y(), -2) > eps ||
		absDiff(got.Width(), 120) > eps ||
		absDiff(got.Height(), 120) > eps {
		t.Errorf("ResourceBoundingBox = %v, want approx (-2, -2, 120, 120)", got)
	}
}

func absDiff(a, b float64) float64 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}
