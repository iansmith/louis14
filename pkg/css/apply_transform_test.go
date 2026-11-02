package css

import (
	"math"
	"testing"

	"louis14/pkg/geometry"
)

// TestApplyTransform locks ComputedStyle::ApplyTransform's composition
// (core/style/computed_style.cc:1434-1496 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): origin, then the translate,
// rotate and scale properties, then the transform list, then the origin
// back out, with percentages against the reference box.
func TestApplyTransform(t *testing.T) {
	s := NewStyle()
	s.Properties["translate"] = "10px 50%"
	s.Properties["rotate"] = "90deg"
	s.Properties["scale"] = "2"
	s.Properties["transform"] = "translateX(25%)"
	s.Properties["transform-origin"] = "0 0"
	box := geometry.NewRectF(0, 0, 200, 100)

	var m geometry.Transform
	s.ApplyTransform(&m, box, IncludeTransformOperations, IncludeTransformOrigin, IncludeIndependentTransformProperties)
	// translate(10px, 50px) rotate(90deg) scale(2) translateX(50px):
	// the point (0,0) maps to (10, 50) + R90·S2·(50, 0) = (10, 150).
	p := m.MapPoint(geometry.PointF{})
	if math.Abs(p.X-10) > 1e-9 || math.Abs(p.Y-150) > 1e-9 {
		t.Errorf("origin maps to %v, want (10, 150)", p)
	}

	// The transform list alone, about the default 50% 50% origin.
	s2 := NewStyle()
	s2.Properties["transform"] = "scale(2)"
	var m2 geometry.Transform
	s2.ApplyTransform(&m2, box, IncludeTransformOperations, IncludeTransformOrigin, ExcludeIndependentTransformProperties)
	if p := m2.MapPoint(geometry.PointF{X: 100, Y: 50}); p.X != 100 || p.Y != 50 {
		t.Errorf("center moved to %v; the origin must be fixed", p)
	}
	var m3 geometry.Transform
	s2.ApplyTransform(&m3, box, IncludeTransformOperations, ExcludeTransformOrigin, ExcludeIndependentTransformProperties)
	if p := m3.MapPoint(geometry.PointF{X: 100, Y: 50}); p.X != 200 || p.Y != 100 {
		t.Errorf("without origin (100,50) maps to %v, want (200,100)", p)
	}
	if !s.HasTransform() || s2.HasTransform() == false || NewStyle().HasTransform() {
		t.Error("HasTransform")
	}
}

// TestTransformOperationsBlend covers TransformOperations::Blend for the
// ported cases (transform_operations.cc:179-215, the per-operation Blend @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestTransformOperationsBlend(t *testing.T) {
	ctx := initialConversionData()
	for _, tc := range []struct{ from, to, want string }{
		// Matching translates; px and % blend into one Length.
		{"translateX(0px)", "translateX(50%)", "translateX(25%)"},
		{"translateX(10px)", "translateX(50%)", "translateX(calc(5px + 25%))"},
		// translate vs translate3d: common primitive kTranslate3D.
		{"translate(10px, 0px)", "translate3d(30px, 0px, 20px)", "translate3d(20px, 0px, 10px)"},
		// Identity padding of the shorter list.
		{"translateX(0px)", "translateX(100px) scale(3)", "translateX(50px) scale(2, 2)"},
		// A zero rotation shares the other rotation's axis.
		{"rotateX(0deg)", "rotateY(90deg)", "rotate3d(0, 1, 0, 45deg)"},
	} {
		from := CreateTransformOperations(tc.from, ctx)
		to := CreateTransformOperations(tc.to, ctx)
		got, ok := to.Blend(from, 0.5)
		if !ok || got.CSSText() != tc.want {
			t.Errorf("Blend(%q → %q, 0.5) = %q, %v; want %q", tc.from, tc.to, got.CSSText(), ok, tc.want)
		}
	}
	// Different functions need matrix interpolation, which is not ported.
	if _, ok := CreateTransformOperations("scale(2)", ctx).Blend(CreateTransformOperations("rotate(10deg)", ctx), 0.5); ok {
		t.Error("mismatched lists blended")
	}
}
