package render

import (
	"image"
	"math"
	"testing"

	"mazarin/textshape"
)

// wantTrackedMatches checks that the tracked matrix maps points as the draw
// context does, at every probe point in front of the camera (w > 0).
func wantTrackedMatches(t *testing.T, dc textshape.DrawContext, step string) {
	t.Helper()
	h := projectiveHomography(drawContextTransform(dc))
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-6*math.Max(1, math.Abs(b)) }
	for _, p := range [][2]float64{{0, 0}, {13, -7}, {-40, 25}, {100, 60}, {3.5, 250}} {
		w := h[6]*p[0] + h[7]*p[1] + h[8]
		if w <= 1e-6 {
			continue // behind the camera, where TransformPoint is not a projection
		}
		gx, gy := (h[0]*p[0]+h[1]*p[1]+h[2])/w, (h[3]*p[0]+h[4]*p[1]+h[5])/w
		wx, wy := dc.TransformPoint(p[0], p[1])
		if !near(gx, wx) || !near(gy, wy) {
			t.Errorf("%s: tracked matrix maps %v to (%v, %v), the draw context to (%v, %v)", step, p, gx, gy, wx, wy)
		}
	}
}

// LOU-769: the renderer's draw context tracks the matrix it is given, in
// mazzy's composition order (each transform applies to local points first:
// draw_impl.go:492-552), across Push/Pop and in a child context, which
// starts at the identity (draw_impl.go:162-172).
func TestTrackingDrawContext_MatchesDrawContext(t *testing.T) {
	r := NewRendererForImage(image.NewRGBA(image.Rect(0, 0, 8, 8)))
	dc := r.dc
	wantTrackedMatches(t, dc, "initial")
	dc.Translate(10, 20)
	dc.Scale(2, 0.5)
	dc.Rotate(0.3)
	dc.RotateAbout(-0.7, 5, 6)
	dc.MultiplyMatrix(1, 0.2, -0.3, 1.1, 4, -2)
	wantTrackedMatches(t, dc, "affine")
	dc.Push()
	dc.MultiplyProjectiveMatrix(1, 0.1, 3, 0.05, 1, -2, 0.002, -0.001, 1)
	dc.Translate(-3, 4)
	dc.Rotate(0.2)
	dc.Scale(1.5, 1.5)
	dc.MultiplyMatrix(0.9, 0.1, 0, 1, 2, 2)
	wantTrackedMatches(t, dc, "projective")
	if _, affine := drawContextAffine(dc); affine {
		t.Error("projective: reported affine")
	}
	dc.Pop()
	wantTrackedMatches(t, dc, "after Pop")
	if _, affine := drawContextAffine(dc); !affine {
		t.Error("after Pop: reported projective")
	}
	child := dc.NewChildContext(image.NewRGBA(image.Rect(0, 0, 4, 4)))
	wantTrackedMatches(t, child, "child")
	child.MultiplyProjectiveMatrix(2, 0, 1, 0, 2, 1, 0.01, 0, 1)
	wantTrackedMatches(t, child, "child projective")
	wantTrackedMatches(t, dc, "parent after child")
}

// An external draw context's starting matrix is read once, with an affine
// probe: it must not be projective.
func TestTrackingDrawContext_ExternalStartingMatrix(t *testing.T) {
	ext := textshape.NewDrawContextForImage(image.NewRGBA(image.Rect(0, 0, 8, 8)), newProvider())
	ext.Translate(7, -3)
	ext.Scale(2, 3)
	ext.Rotate(0.4)
	r := NewRendererForDrawContext(ext)
	wantTrackedMatches(t, r.dc, "external")
	r.dc.MultiplyProjectiveMatrix(1, 0, 0, 0, 1, 0, 0.003, 0.001, 1)
	wantTrackedMatches(t, r.dc, "external projective")
}
