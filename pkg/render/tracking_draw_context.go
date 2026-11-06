package render

import (
	"image"
	"math"

	"mazarin/textshape"

	"louis14/pkg/geometry"
)

// trackingDrawContext is the renderer's draw context: it forwards every call
// to the textshape.DrawContext it wraps and keeps the current transform it
// composes, so paint code reads the exact device matrix rather than
// reconstructing it. Blink carries the full transform the same way, through
// its property trees (GeometryMapper::SourceToDestinationProjection), and
// never reads it back from the canvas.
//
// m is the 3×3 row-major homography mazzy holds (its gs.matrix promoted to
// gs.proj in projective mode), composed in mazzy's order: each transform
// applies to local points first, M ← M·t (DrawContextImpl.Translate through
// MultiplyProjectiveMatrix, draw_impl.go:492-552 in ~/mazzy/mazarin/
// textshape). Push and Pop save and restore it with the rest of the
// graphics state (draw_impl.go:218-231).
//
// no Blink analog: louis14 paints through an immediate-mode draw context
// whose matrix it cannot read, so it shadows it.
type trackingDrawContext struct {
	textshape.DrawContext
	m     [9]float64
	stack [][9]float64
}

var identityHomography = [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}

// trackDrawContext wraps dc, starting from its current matrix, which is
// read once with an affine probe. dc must not already be projective: a
// fourth probe that the affine fit does not map panics, as a wrong tracked
// matrix would mis-paint silently.
func trackDrawContext(dc textshape.DrawContext) *trackingDrawContext {
	if t, ok := dc.(*trackingDrawContext); ok {
		return t
	}
	ox, oy := dc.TransformPoint(0, 0)
	xx, yx := dc.TransformPoint(1, 0)
	xy, yy := dc.TransformPoint(0, 1)
	// probeTolerance is relative: the affine fit and the draw context's own
	// map of (1, 1) differ only by rounding when the matrix is affine.
	const probeTolerance = 1e-9
	near := func(a, b float64) bool { return math.Abs(a-b) <= probeTolerance*math.Max(1, math.Abs(b)) }
	px, py := dc.TransformPoint(1, 1)
	if !near(px, xx+xy-ox) || !near(py, yx+yy-oy) {
		panic("render: the host draw context's matrix is projective; it must be affine")
	}
	return &trackingDrawContext{DrawContext: dc, m: [9]float64{xx - ox, xy - ox, ox, yx - oy, yy - oy, oy, 0, 0, 1}}
}

func (t *trackingDrawContext) compose(n [9]float64) { t.m = mul3x3(t.m, n) }

func (t *trackingDrawContext) Push() {
	t.stack = append(t.stack, t.m)
	t.DrawContext.Push()
}

// Pop restores the renderer's last Push. With none, it does nothing: the
// draw context's own stack may hold its host's state, which the tracked
// matrix does not, and the two must stay in lockstep.
func (t *trackingDrawContext) Pop() {
	n := len(t.stack)
	if n == 0 {
		return
	}
	t.m, t.stack = t.stack[n-1], t.stack[:n-1]
	t.DrawContext.Pop()
}

func (t *trackingDrawContext) Translate(x, y float64) {
	t.compose([9]float64{1, 0, x, 0, 1, y, 0, 0, 1})
	t.DrawContext.Translate(x, y)
}

func (t *trackingDrawContext) Scale(x, y float64) {
	t.compose([9]float64{x, 0, 0, 0, y, 0, 0, 0, 1})
	t.DrawContext.Scale(x, y)
}

func (t *trackingDrawContext) Rotate(angle float64) {
	c, s := math.Cos(angle), math.Sin(angle)
	t.compose([9]float64{c, -s, 0, s, c, 0, 0, 0, 1})
	t.DrawContext.Rotate(angle)
}

// RotateAbout is Translate, Rotate, Translate back, as mazzy composes it.
func (t *trackingDrawContext) RotateAbout(angle, x, y float64) {
	t.Translate(x, y)
	t.Rotate(angle)
	t.Translate(-x, -y)
}

func (t *trackingDrawContext) MultiplyMatrix(xx, yx, xy, yy, x0, y0 float64) {
	t.compose([9]float64{xx, xy, x0, yx, yy, y0, 0, 0, 1})
	t.DrawContext.MultiplyMatrix(xx, yx, xy, yy, x0, y0)
}

func (t *trackingDrawContext) MultiplyProjectiveMatrix(m00, m01, m02, m10, m11, m12, m20, m21, m22 float64) {
	t.compose([9]float64{m00, m01, m02, m10, m11, m12, m20, m21, m22})
	t.DrawContext.MultiplyProjectiveMatrix(m00, m01, m02, m10, m11, m12, m20, m21, m22)
}

// NewChildContext's child starts at the identity (draw_impl.go:162-172).
func (t *trackingDrawContext) NewChildContext(target *image.RGBA) textshape.DrawContext {
	return &trackingDrawContext{DrawContext: t.DrawContext.NewChildContext(target), m: identityHomography}
}

func mul3x3(a, b [9]float64) [9]float64 {
	var r [9]float64
	for i := range 3 {
		for j := range 3 {
			r[3*i+j] = a[3*i]*b[j] + a[3*i+1]*b[3+j] + a[3*i+2]*b[6+j]
		}
	}
	return r
}

// drawContextTransform is dc's current device matrix, as its z=0 homography
// (transformFromHomography's layout). Every draw context the renderer paints
// through is tracking: NewRendererForDrawContext wraps it and NewChildContext
// keeps it wrapped; another is a programming error, and panics.
func drawContextTransform(dc textshape.DrawContext) geometry.Transform {
	return transformFromHomography(dc.(*trackingDrawContext).m)
}

// drawContextAffine is dc's device matrix when it is affine, and whether it
// is. A homography whose w is the same everywhere (h6 = h7 = 0, h8 > 0: a
// translateZ under perspective) is affine up to that scale; it is reported
// affine, normalized by h8. A projective one returns the zero transform.
func drawContextAffine(dc textshape.DrawContext) (geometry.Transform, bool) {
	// constantWTolerance bounds h6 and h7 relative to h8: composed affine
	// steps leave them exactly zero, so only rounding is allowed.
	const constantWTolerance = 1e-12
	h := dc.(*trackingDrawContext).m
	if h[8] <= 0 || math.Abs(h[6]) > constantWTolerance*h[8] || math.Abs(h[7]) > constantWTolerance*h[8] {
		return geometry.Transform{}, false
	}
	w := h[8]
	return transformFromHomography([9]float64{h[0] / w, h[1] / w, h[2] / w, h[3] / w, h[4] / w, h[5] / w, 0, 0, 1}), true
}

// projectNearEpsilon is the w plane the near-plane clip cuts at: mazzy's
// projNearEpsilon (draw_impl.go:594 in ~/mazzy/mazarin/textshape), the
// plane it clips the paths it fills against.
const projectNearEpsilon = 1.0 / (1 << 14)

// projectRectInFront maps r through t's homography and returns the bounds of
// the part of it in front of the camera (w > projectNearEpsilon), clipping
// the quad against that plane in homogeneous space as mazzy clips the paths
// it fills (Skia's PerspectiveClip). ok is false when none of r is in front.
// It is the one place the renderer decides what of a rect a projective
// matrix shows.
func projectRectInFront(t geometry.Transform, r geometry.RectF) (geometry.RectF, bool) {
	h := projectiveHomography(t)
	type hp struct{ x, y, w float64 }
	at := func(x, y float64) hp {
		return hp{h[0]*x + h[1]*y + h[2], h[3]*x + h[4]*y + h[5], h[6]*x + h[7]*y + h[8]}
	}
	quad := []hp{at(r.X(), r.Y()), at(r.Right(), r.Y()), at(r.Right(), r.Bottom()), at(r.X(), r.Bottom())}
	const eps = projectNearEpsilon
	var in []hp
	for i, p := range quad {
		q := quad[(i+1)%len(quad)]
		if p.w > eps {
			in = append(in, p)
		}
		if (p.w > eps) != (q.w > eps) {
			s := (eps - p.w) / (q.w - p.w)
			in = append(in, hp{p.x + s*(q.x-p.x), p.y + s*(q.y-p.y), eps})
		}
	}
	if len(in) == 0 {
		return geometry.RectF{}, false
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range in {
		x, y := p.x/p.w, p.y/p.w
		minX, minY = math.Min(minX, x), math.Min(minY, y)
		maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
	}
	return geometry.NewRectF(minX, minY, maxX-minX, maxY-minY), true
}
