package render

import (
	"image"
	"math"
	"testing"

	"louis14/pkg/geometry"
)

// LOU-769: deviceTransformAt reads back a projective draw context matrix at
// the probe rect, and reports a degenerate one (an edge-on projection, which
// Blink does not paint) as not ok. A legitimately tiny projective scale is
// not degenerate: the threshold is relative.
func TestDeviceTransformAt(t *testing.T) {
	probe := geometry.NewRectF(0, 0, 10, 10)
	for _, c := range []struct {
		name string
		h    [9]float64
		ok   bool
	}{
		{"projective", [9]float64{2, 0.5, 3, 0.25, 1.5, 4, 0.001, 0.002, 1}, true},
		{"tiny scale", [9]float64{1e-4, 0, 0, 0, 1e-4, 0, 1e-7, 0, 1}, true},
		{"edge-on", [9]float64{1, 0, 0, 0, 0, 5, 0.001, 0, 1}, false},
	} {
		r := NewRendererForImage(image.NewRGBA(image.Rect(0, 0, 4, 4)))
		r.dc.MultiplyProjectiveMatrix(c.h[0], c.h[1], c.h[2], c.h[3], c.h[4], c.h[5], c.h[6], c.h[7], c.h[8])
		m, affine, ok := deviceTransformAt(r.dc, probe)
		if ok != c.ok || affine {
			t.Errorf("%s: ok=%v affine=%v, want ok=%v affine=false", c.name, ok, affine, c.ok)
			continue
		}
		if !ok {
			continue
		}
		near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }
		for _, p := range [][2]float64{{3, 7}, {9, 1}, {5, 5}} {
			wx, wy := r.dc.TransformPoint(p[0], p[1])
			q, front := mapRectToLayer(m, geometry.NewRectF(p[0], p[1], 0, 0))
			if !front || !near(q.X(), wx) || !near(q.Y(), wy) {
				t.Errorf("%s: maps %v to %v (in front %v), the draw context to (%v, %v)", c.name, p, q, front, wx, wy)
			}
		}
	}
}
