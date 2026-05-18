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

// TestResourceBoundingBox_UserSpaceOnUseMissingXYUsesViewportPercentBase
// captures the LOU-129 Bug 1 root cause: in userSpaceOnUse mode the
// spec-default -10%/-10%/120%/120% percentages must resolve against
// the SVG VIEWPORT (lengthCtx's viewport), NOT against the target's
// own bounding box. Mirrors Blink's
// LayoutSVGResourceContainer::ResolveRectangle
// (third_party/blink/renderer/core/layout/svg/layout_svg_resource_container.cc:99-146)
// which in the userSpaceOnUse branch uses
// `viewport_resolver.ResolveViewport()` (the SVG viewport) for percent
// resolution, while the objectBoundingBox branch uses the reference box.
//
// Failing case: tainting-fedropshadow-001.html has a 100×100 rect inside
// an <svg> with no width/height (default viewport 300×150). The filter
// has filterUnits="userSpaceOnUse" with no explicit x/y/w/h. Per Blink
// the filter region is then (-30, -15, 360, 180) — wide enough to hold
// the chain's intermediate shadow at (100..200, 0..100). Before this
// fix the region was (-10, -10, 110, 110), clipping the shadow at x=110
// and corrupting the downstream feOffset + feDisplacementMap output.
func TestResourceBoundingBox_UserSpaceOnUseMissingXYUsesViewportPercentBase(t *testing.T) {
	f := &SVGResourceFilter{
		FilterUnits: SVGUnitUserSpaceOnUse,
		// X, Y, Width, Height all HasValue=false → use defaults.
	}
	// 100×100 rect at user-space (0, 0) → device (8, 8) (body margin).
	// SVG viewport is 300×150 (the default for an <svg> with no
	// width/height in HTML).
	target := geometry.NewRectF(8, 8, 100, 100)
	userOrigin := geometry.PointF{X: 8, Y: 8}
	viewport := geometry.SizeF{Width: 300, Height: 150}
	got := f.ResourceBoundingBox(target, userOrigin, NewSVGLengthContext(viewport))
	// Per Blink: x = userOrigin.X + (-0.10)*300 = -22
	//           y = userOrigin.Y + (-0.10)*150 = -7
	//           w = 1.20*300 = 360
	//           h = 1.20*150 = 180
	const eps = 1e-9
	if absDiff(got.X(), -22) > eps ||
		absDiff(got.Y(), -7) > eps ||
		absDiff(got.Width(), 360) > eps ||
		absDiff(got.Height(), 180) > eps {
		t.Errorf("ResourceBoundingBox = %v, want approx (-22, -7, 360, 180)", got)
	}
}

// TestResourceBoundingBox_UserSpaceOnUseMissingXYBackwardsCompat retains
// the original behaviour when caller passes the target size AS the
// viewport (the pre-fix shape). Confirms the layout-level fix is purely
// driven by the lengthCtx the caller supplies.
func TestResourceBoundingBox_UserSpaceOnUseMissingXYBackwardsCompat(t *testing.T) {
	f := &SVGResourceFilter{
		FilterUnits: SVGUnitUserSpaceOnUse,
	}
	target := geometry.NewRectF(8, 8, 100, 100)
	userOrigin := geometry.PointF{X: 8, Y: 8}
	// When the caller passes target.Size as viewport the math reproduces
	// the pre-fix behavior: x=8+(-10), y=8+(-10), w=120, h=120.
	got := f.ResourceBoundingBox(target, userOrigin, NewSVGLengthContext(target.Size))
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

// TestResourceBoundingBox_PercentDefaults_ObjectBoundingBox is the LOU-130
// Phase 1 falsification test: with filterUnits=objectBoundingBox and no
// explicit x/y/w/h on the `<filter>` element, the SVG Filter Effects 1
// §6.5 defaults must produce a filter region of (-10, -10, 120, 120) over
// a 100x100 target box. Per Blink's
// LayoutSVGResourceFilter::ResourceBoundingBox + ResolveRectangle
// (objectBoundingBox branch) the result is target + (-10%, -10%, 120%, 120%)
// scaled by target dimensions.
func TestResourceBoundingBox_PercentDefaults_ObjectBoundingBox(t *testing.T) {
	f := &SVGResourceFilter{
		FilterUnits: SVGUnitObjectBoundingBox,
		// X, Y, Width, Height all HasValue=false → use defaults.
	}
	target := geometry.NewRectF(0, 0, 100, 100)
	userOrigin := geometry.PointF{X: 0, Y: 0}
	got := f.ResourceBoundingBox(target, userOrigin, NewSVGLengthContext(target.Size))
	const eps = 1e-9
	if absDiff(got.X(), -10) > eps ||
		absDiff(got.Y(), -10) > eps ||
		absDiff(got.Width(), 120) > eps ||
		absDiff(got.Height(), 120) > eps {
		t.Errorf("ResourceBoundingBox = %v, want (-10, -10, 120, 120)", got)
	}
}

// TestResourceBoundingBox_PercentDefaults_UserSpaceOnUse is the userSpace
// counterpart to the above. With filterUnits=userSpaceOnUse and no
// explicit x/y/w/h, the -10%/-10%/120%/120% defaults must resolve against
// the SVG viewport (per Blink's ResolveRectangle userSpaceOnUse branch).
// Here the viewport is 100x100 so the resolved region is (-10, -10, 120, 120)
// in user-space, then shifted by userSpaceOrigin (here (0, 0)).
func TestResourceBoundingBox_PercentDefaults_UserSpaceOnUse(t *testing.T) {
	f := &SVGResourceFilter{
		FilterUnits: SVGUnitUserSpaceOnUse,
		// X, Y, Width, Height all HasValue=false → use defaults.
	}
	target := geometry.NewRectF(0, 0, 100, 100)
	userOrigin := geometry.PointF{X: 0, Y: 0}
	viewport := geometry.SizeF{Width: 100, Height: 100}
	got := f.ResourceBoundingBox(target, userOrigin, NewSVGLengthContext(viewport))
	const eps = 1e-9
	if absDiff(got.X(), -10) > eps ||
		absDiff(got.Y(), -10) > eps ||
		absDiff(got.Width(), 120) > eps ||
		absDiff(got.Height(), 120) > eps {
		t.Errorf("ResourceBoundingBox = %v, want (-10, -10, 120, 120)", got)
	}
}

// TestResourceBoundingBox_ExplicitOverridesDefault asserts that when the
// `<filter>` carries explicit x/y/w/h percent attrs the spec defaults are
// NOT applied — the explicit values fully drive resolution. Mirrors
// Blink's ResolveRectangle, which calls value resolution per axis and
// only falls back to the SVG-spec default when the SVGLength field is
// the spec sentinel.
func TestResourceBoundingBox_ExplicitOverridesDefault(t *testing.T) {
	f := &SVGResourceFilter{
		FilterUnits: SVGUnitObjectBoundingBox,
		X:           SVGGradientLength{Value: -0.05, IsPercent: true, HasValue: true, Raw: "-5%"},
		Y:           SVGGradientLength{Value: -0.05, IsPercent: true, HasValue: true, Raw: "-5%"},
		Width:       SVGGradientLength{Value: 1.10, IsPercent: true, HasValue: true, Raw: "110%"},
		Height:      SVGGradientLength{Value: 1.10, IsPercent: true, HasValue: true, Raw: "110%"},
	}
	target := geometry.NewRectF(0, 0, 100, 100)
	userOrigin := geometry.PointF{X: 0, Y: 0}
	got := f.ResourceBoundingBox(target, userOrigin, NewSVGLengthContext(target.Size))
	const eps = 1e-9
	// Explicit -5%/-5%/110%/110% over a 100x100 target → (-5, -5, 110, 110).
	if absDiff(got.X(), -5) > eps ||
		absDiff(got.Y(), -5) > eps ||
		absDiff(got.Width(), 110) > eps ||
		absDiff(got.Height(), 110) > eps {
		t.Errorf("ResourceBoundingBox = %v, want (-5, -5, 110, 110); defaults must not override explicit attrs", got)
	}
}
