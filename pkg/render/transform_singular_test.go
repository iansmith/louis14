package render

import (
	"testing"

	"louis14/pkg/css"
)

// TestIsSingularProjection locks the contract for the edge-on (singular)
// flattened-transform cull (LOU-182). A flattened 2D projection that maps the
// box to a zero-area quad must be reported singular so the transform paint
// scope skips painting the subtree; legitimately-visible transforms must NOT
// be reported singular or visible content would be blanked.
func TestIsSingularProjection(t *testing.T) {
	proj := func(transforms ...css.Transform) (xx, yx, xy, yy float64) {
		x, y, a, b, _, _ := css.ComposeAffineProjection(transforms)
		return x, y, a, b
	}

	cases := []struct {
		name       string
		transforms []css.Transform
		want       bool
	}{
		{
			// rotateX(90deg): yy = cos(90°) ≈ 6.12e-17, edge-on, zero area.
			name:       "rotateX(90deg) is edge-on",
			transforms: []css.Transform{{Type: "rotateX", Values: []float64{90}}},
			want:       true,
		},
		{
			// rotateY(90deg): xx ≈ 6.12e-17, edge-on, zero area.
			name:       "rotateY(90deg) is edge-on",
			transforms: []css.Transform{{Type: "rotateY", Values: []float64{90}}},
			want:       true,
		},
		{
			name:       "scale(1,0) collapses Y to zero",
			transforms: []css.Transform{{Type: "scale", Values: []float64{1, 0}}},
			want:       true,
		},
		{
			name:       "identity is not singular",
			transforms: nil,
			want:       false,
		},
		{
			name:       "rotate(45deg) is not singular",
			transforms: []css.Transform{{Type: "rotate", Values: []float64{45}}},
			want:       false,
		},
		{
			// Aggressive uniform scale-down: det = 0.001*0.001 = 1e-6, far above
			// the singular epsilon — must stay visible.
			name:       "scale(0.001) is not singular",
			transforms: []css.Transform{{Type: "scale", Values: []float64{0.001, 0.001}}},
			want:       false,
		},
		{
			// Two rotateX(90deg) composed (preserve-3d 180°) → scaleY(-1): det = -1,
			// content reappears mirrored, must NOT be culled.
			name: "double rotateX(90deg) composes to non-singular",
			transforms: []css.Transform{
				{Type: "rotateX", Values: []float64{90}},
				{Type: "rotateX", Values: []float64{90}},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			xx, yx, xy, yy := proj(tc.transforms...)
			got := isSingularProjection(xx, yx, xy, yy)
			if got != tc.want {
				t.Errorf("isSingularProjection(%v) = %v, want %v (xx=%g yx=%g xy=%g yy=%g)",
					tc.transforms, got, tc.want, xx, yx, xy, yy)
			}
		})
	}
}
