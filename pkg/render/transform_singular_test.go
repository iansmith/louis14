package render

import (
	"testing"

	"louis14/pkg/geometry"
)

// TestIsSingularProjection locks the contract for the edge-on (singular)
// flattened-transform cull (LOU-182). A flattened 2D projection that maps the
// box to a zero-area quad must be reported singular so the transform paint
// scope skips painting the subtree; legitimately-visible transforms must NOT
// be reported singular or visible content would be blanked.
func TestIsSingularProjection(t *testing.T) {
	build := func(steps ...func(*geometry.Transform)) geometry.Transform {
		var m geometry.Transform
		for _, step := range steps {
			step(&m)
		}
		return m
	}
	rotateX := func(deg float64) func(*geometry.Transform) {
		return func(m *geometry.Transform) { m.RotateAboutXAxis(deg) }
	}

	cases := []struct {
		name       string
		transforms geometry.Transform
		want       bool
	}{
		{
			// rotateX(90deg): yy = cos(90°) = 0 (exact, gfx::SinCosDegrees).
			name:       "rotateX(90deg) is edge-on",
			transforms: build(rotateX(90)),
			want:       true,
		},
		{
			// rotateY(90deg): xx = 0, edge-on, zero area.
			name:       "rotateY(90deg) is edge-on",
			transforms: build(func(m *geometry.Transform) { m.RotateAboutYAxis(90) }),
			want:       true,
		},
		{
			name:       "scale(1,0) collapses Y to zero",
			transforms: geometry.MakeScale(1, 0),
			want:       true,
		},
		{
			name:       "identity is not singular",
			transforms: geometry.Transform{},
			want:       false,
		},
		{
			name:       "rotate(45deg) is not singular",
			transforms: build(func(m *geometry.Transform) { m.Rotate(45) }),
			want:       false,
		},
		{
			// Aggressive uniform scale-down: det = 0.001*0.001 = 1e-6, far above
			// the singular epsilon — must stay visible.
			name:       "scale(0.001) is not singular",
			transforms: geometry.MakeScale(0.001, 0.001),
			want:       false,
		},
		{
			// Two rotateX(90deg) composed (preserve-3d 180°) → scaleY(-1): det = -1,
			// content reappears mirrored, must NOT be culled.
			name:       "double rotateX(90deg) composes to non-singular",
			transforms: build(rotateX(90), rotateX(90)),
			want:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := geometry.AffineTransformFromTransform(tc.transforms)
			xx, yx, xy, yy := a.A, a.B, a.C, a.D
			got := isSingularProjection(xx, yx, xy, yy)
			if got != tc.want {
				t.Errorf("isSingularProjection(%v) = %v, want %v (xx=%g yx=%g xy=%g yy=%g)",
					tc.transforms, got, tc.want, xx, yx, xy, yy)
			}
		})
	}
}
