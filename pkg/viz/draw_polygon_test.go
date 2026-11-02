package viz

import (
	"math"
	"testing"

	"louis14/pkg/geometry"
)

// Ported from components/viz/service/display/draw_polygon_unittest.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.

type p3 = geometry.Point3F
type v3 = geometry.Vector3dF

const floatEpsilon = 1.1920928955078125e-07

func sign(v float64) int {
	const epsilon = 0.00001
	switch {
	case v > epsilon:
		return 1
	case v < -epsilon:
		return -1
	}
	return 0
}

func isPlanarForTesting(p *DrawPolygon) bool {
	const epsilon = 0.00001
	for i := 1; i < len(p.points); i++ {
		if geometry.DotProduct(p.points[i].Sub(p.points[0]), p.normal) > epsilon {
			return false
		}
	}
	return true
}

func isConvexForTesting(p *DrawPolygon) bool {
	n := len(p.points)
	if n < 3 {
		return true
	}
	prev := p.points[n-1].Sub(p.points[n-2])
	next := p.points[0].Sub(p.points[n-1])
	ccw := sign(geometry.DotProduct(geometry.CrossProduct(prev, next), p.normal))
	for i := 1; i < n; i++ {
		prev = next
		next = p.points[i].Sub(p.points[i-1])
		s := sign(geometry.DotProduct(geometry.CrossProduct(prev, next), p.normal))
		if ccw == 0 {
			ccw = s
		}
		if s != 0 && s != ccw {
			return false
		}
	}
	return true
}

// forwardPolygon is CREATE_TEST_DRAW_FORWARD_POLYGON: normal (0,0,1), then
// RecomputeNormalForTesting.
func forwardPolygon(points []p3, id int) *DrawPolygon {
	p := NewDrawPolygon(nil, points, v3{Z: 1}, id)
	p.ConstructNormal()
	return p
}

func expectNormal(t *testing.T, p *DrawPolygon, x, y, z float64) {
	t.Helper()
	n := p.Normal()
	if math.Abs(n.X-x) >= floatEpsilon || math.Abs(n.Y-y) >= floatEpsilon || math.Abs(n.Z-z) >= floatEpsilon {
		t.Errorf("normal = %v, want (%v, %v, %v)", n, x, y, z)
	}
}

// floatEq is EXPECT_FLOAT_EQ: within 4 float ULPs.
func floatEq(a, b float64) bool {
	fa, fb := float32(a), float32(b)
	if fa == fb {
		return true
	}
	ulp := math.Nextafter32(float32(math.Abs(float64(fb))), float32(math.Inf(1))) - float32(math.Abs(float64(fb)))
	return math.Abs(float64(fa-fb)) <= 4*float64(ulp)
}

func validatePoints(t *testing.T, p *DrawPolygon, want []p3) {
	t.Helper()
	if len(p.Points()) != len(want) {
		t.Fatalf("points = %v, want %v", p.Points(), want)
	}
	for i, w := range want {
		g := p.Points()[i]
		if !floatEq(g.X, w.X) || !floatEq(g.Y, w.Y) || !floatEq(g.Z, w.Z) {
			t.Errorf("point %d = %v, want %v", i, g, w)
		}
	}
}

func validatePointsWithinDeltaOf(t *testing.T, p *DrawPolygon, want []p3, delta float64) {
	t.Helper()
	if len(p.Points()) != len(want) {
		t.Fatalf("points = %v, want %v", p.Points(), want)
	}
	for i, w := range want {
		if d := p.Points()[i].Sub(w).Length(); d > delta {
			t.Errorf("point %d = %v, want %v (±%v)", i, p.Points()[i], w, delta)
		}
	}
}

func contains(points []p3, v p3) bool {
	for _, p := range points {
		if p == v {
			return true
		}
	}
	return false
}

func rowMajor(m ...float64) geometry.Transform {
	return geometry.TransformRowMajor(m[0], m[1], m[2], m[3], m[4], m[5], m[6], m[7],
		m[8], m[9], m[10], m[11], m[12], m[13], m[14], m[15])
}

func TestDrawPolygonConstruction_NormalNormal(t *testing.T) {
	expectNormal(t, NewDrawPolygonFromRect(nil, geometry.NewRectF(0, 0, 10, 10), geometry.Transform{}, 1), 0, 0, 1)
}

func TestDrawPolygonConstruction_TestNormal(t *testing.T) {
	expectNormal(t, forwardPolygon([]p3{{X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}}, 1), 0, 0, 1)
}

func TestDrawPolygonConstruction_ClippedNormal(t *testing.T) {
	expectNormal(t, forwardPolygon([]p3{{X: 0.1, Y: 10, Z: 0}, {X: 0, Y: 9.9, Z: 0}, {X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}}, 1), 0, 0, 1)
}

func TestDrawPolygonConstruction_SlimTriangleNormal(t *testing.T) {
	expectNormal(t, forwardPolygon([]p3{{X: 0, Y: 0, Z: 0}, {X: 5000, Y: 0, Z: 0}, {X: 10000, Y: 1, Z: 0}}, 2), 0, 0, 1)
}

func TestDrawPolygonConstruction_ManyVertexNormal(t *testing.T) {
	var c, d []p3
	for i := 0; i < 100; i++ {
		step := float64(i) * math.Pi / 50
		c = append(c, p3{X: math.Cos(step), Y: math.Sin(step)})
		d = append(d, p3{X: math.Cos(step) + 99, Y: math.Sin(step) + 99, Z: 100})
	}
	expectNormal(t, forwardPolygon(c, 3), 0, 0, 1)
	expectNormal(t, forwardPolygon(d, 4), 0, 0, 1)
}

func TestDrawPolygonConstruction_TransformedRects(t *testing.T) {
	src := geometry.NewRectF(-0.1, -10, 0.2, 20)
	for _, tc := range []struct {
		name    string
		tr      geometry.Transform
		x, y, z float64
	}{
		{"SimpleNormal", geometry.Transform{}, 0, 0, 1},
		{"NormalInvertXY", rowMajor(0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1), 0, 0, 1},
		{"NormalInvertXZ", rowMajor(0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1), 1, 0, 0},
		{"NormalInvertYZ", rowMajor(1, 0, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1), 0, 1, 0},
		{"NormalRotate90", rowMajor(0, -1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1), 0, 0, 1},
		{"InvertXNormal", rowMajor(-1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1), 0, 0, 1},
		{"InvertYNormal", rowMajor(1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1), 0, 0, 1},
		{"InvertZNormal", rowMajor(1, 0, 0, 0, 0, 1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1), 0, 0, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectNormal(t, NewDrawPolygonFromRect(nil, src, tc.tr, 1), tc.x, tc.y, tc.z)
		})
	}
}

func TestDrawPolygonSplit_NearlyTouchingOrder(t *testing.T) {
	a := NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}}, v3{Z: 1}, 0)
	b := NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: -1}, {X: 0, Y: 0, Z: -1}, {X: 10, Y: 0, Z: -1}, {X: 10, Y: 10, Z: -1}}, v3{Z: 1}, 1)
	front, back, coplanar := a.SplitPolygon(b)
	if coplanar || front != nil || back == nil {
		t.Errorf("coplanar=%v front=%v back=%v, want back only", coplanar, front, back)
	}
}

func normalOf(v []p3) v3 {
	n := geometry.CrossProduct(v[1].Sub(v[0]), v[1].Sub(v[2]))
	n.InvScale(n.Length())
	return n
}

func TestDrawPolygonSplit_NotClearlyInFront(t *testing.T) {
	va := []p3{{X: 87.2, Y: 1185, Z: 0.9}, {X: 288.3, Y: 1185, Z: -0.7}, {X: 288.3, Y: 1196, Z: -0.7}, {X: 87.2, Y: 1196, Z: 0.9}}
	vb := []p3{{X: 62.1, Y: 1034.7, Z: 1}, {X: 313.4, Y: 1035.3, Z: -1}, {X: 313.4, Y: 1196, Z: -1}, {X: 62.1, Y: 1196, Z: 1}}
	a := NewDrawPolygon(nil, va, normalOf(va), 0)
	b := NewDrawPolygon(nil, vb, normalOf(vb), 1)
	front, back, coplanar := a.SplitPolygon(b)
	if coplanar || front == nil || back != nil {
		t.Errorf("coplanar=%v front=%v back=%v, want front only", coplanar, front, back)
	}
}

func TestDrawPolygonSplit_NotTouchingNoSplit(t *testing.T) {
	a := NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}}, v3{Z: 1}, 0)
	b := NewDrawPolygon(nil, []p3{{X: 5, Y: 10, Z: 5}, {X: 5, Y: 10, Z: 15}, {X: 5, Y: 0, Z: 15}, {X: 5, Y: 0, Z: 5}}, v3{X: -1}, 1)
	front, back, coplanar := a.SplitPolygon(b)
	if coplanar || front == nil || back != nil {
		t.Errorf("coplanar=%v front=%v back=%v, want front only", coplanar, front, back)
	}
}

func TestDrawPolygonSplit_BarelyTouchingNoSplit(t *testing.T) {
	a := NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}}, v3{Z: 1}, 0)
	b := NewDrawPolygon(nil, []p3{{X: 5, Y: 10, Z: 0}, {X: 5, Y: 10, Z: -10}, {X: 5, Y: 0, Z: -10}, {X: 5, Y: 0, Z: 0}}, v3{X: -1}, 1)
	front, back, coplanar := a.SplitPolygon(b)
	if coplanar || front != nil || back == nil {
		t.Errorf("coplanar=%v front=%v back=%v, want back only", coplanar, front, back)
	}
}

func TestDrawPolygonSplit_SlimClip(t *testing.T) {
	a := NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}}, v3{Z: 1}, 0)
	b := NewDrawPolygon(nil, []p3{{X: 9, Y: 9, Z: 5}, {X: 1, Y: 1, Z: 0.001}, {X: 1, Y: 1, Z: 0}, {X: 1.002, Y: 1.002, Z: -0.005}, {X: 9, Y: 9, Z: -4}},
		v3{X: math.Sqrt2 / 2, Y: -math.Sqrt2 / 2}, 1)
	for name, p := range map[string]*DrawPolygon{"a": a, "b": b} {
		if !isPlanarForTesting(p) || !isConvexForTesting(p) {
			t.Errorf("polygon %s is not planar and convex", name)
		}
	}
	front, back, coplanar := a.SplitPolygon(b)
	if coplanar || front == nil || back == nil {
		t.Errorf("coplanar=%v front=%v back=%v, want a split", coplanar, front, back)
	}
}

func TestDrawPolygonSplit_BasicSplit(t *testing.T) {
	a := NewDrawPolygon(nil, []p3{{X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}}, v3{Z: 1}, 0)
	b := NewDrawPolygon(nil, []p3{{X: 5, Y: 10, Z: -5}, {X: 5, Y: 0, Z: -5}, {X: 5, Y: 0, Z: 5}, {X: 5, Y: 10, Z: 5}}, v3{X: -1}, 1)
	front, back, coplanar := a.SplitPolygon(b)
	if coplanar || front == nil || back == nil {
		t.Fatalf("coplanar=%v front=%v back=%v, want a split", coplanar, front, back)
	}
	validatePoints(t, front, []p3{{X: 5, Y: 0, Z: 0}, {X: 5, Y: 0, Z: 5}, {X: 5, Y: 10, Z: 5}, {X: 5, Y: 10, Z: 0}})
	validatePoints(t, back, []p3{{X: 5, Y: 10, Z: 0}, {X: 5, Y: 10, Z: -5}, {X: 5, Y: 0, Z: -5}, {X: 5, Y: 0, Z: 0}})
}

func TestDrawPolygonSplit_AngledSplit(t *testing.T) {
	a := NewDrawPolygon(nil, []p3{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 0}}, v3{Y: 1}, 0)
	b := NewDrawPolygon(nil, []p3{{X: 2, Y: 5, Z: 1}, {X: 2, Y: -5, Z: 1}, {X: -1, Y: -5, Z: -2}, {X: -1, Y: 5, Z: -2}}, v3{X: 0.707107, Z: -0.707107}, 1)
	front, back, coplanar := b.SplitPolygon(a)
	if coplanar || front == nil || back == nil {
		t.Fatalf("coplanar=%v front=%v back=%v, want a split", coplanar, front, back)
	}
	validatePointsWithinDeltaOf(t, front, []p3{{X: 10, Y: 0, Z: 9}, {X: 10, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}}, 1e-6)
	validatePointsWithinDeltaOf(t, back, []p3{{X: 1, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 9}}, 1e-6)
}

// AlmostCoplanarSplit (crbug.com/693826): every vertex of the split
// polygon survives in one piece.
func TestDrawPolygonSplit_AlmostCoplanarSplit(t *testing.T) {
	va := []p3{
		{X: 723.814758300781250, Y: 552.810119628906250, Z: -206.656036376953125},
		{X: 797.634155273437500, Y: 549.095703125000000, Z: -209.802902221679688},
		{X: 799.264648437500000, Y: 490.325805664062500, Z: -172.261627197265625},
		{X: 720.732421875000000, Y: 493.944458007812500, Z: -168.700469970703125},
	}
	vb := []p3{
		{X: 720.631286621093750, Y: 487.595977783203125, Z: -164.681198120117188},
		{X: 799.672851562500000, Y: 484.059020996093750, Z: -168.219161987304688},
		{X: 801.565490722656250, Y: 416.416809082031250, Z: -125.007690429687500},
		{X: 717.096801757812500, Y: 419.792327880859375, Z: -120.967689514160156},
	}
	splitting := NewDrawPolygon(nil, va, v3{X: -0.062916249036789, Y: -0.538499474525452, Z: -0.840273618698120}, 0)
	split := NewDrawPolygon(nil, vb, v3{X: -0.061713, Y: -0.538550, Z: -0.840330}, 1)
	front, back, coplanar := splitting.SplitPolygon(split)
	if coplanar || front == nil || back == nil {
		t.Fatalf("coplanar=%v front=%v back=%v, want a split", coplanar, front, back)
	}
	for _, v := range vb {
		if !contains(front.Points(), v) && !contains(back.Points(), v) {
			t.Errorf("vertex %v lost", v)
		}
	}
}

func TestDrawPolygonSplit_DoubleSplit(t *testing.T) {
	a := NewDrawPolygon(nil, []p3{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 0}}, v3{Y: 1}, 0)
	b := NewDrawPolygon(nil, []p3{{X: 2, Y: 5, Z: 1}, {X: 2, Y: -5, Z: 1}, {X: -1, Y: -5, Z: -2}, {X: -1, Y: 5, Z: -2}}, v3{X: math.Sqrt2 / 2, Z: -math.Sqrt2 / 2}, 1)
	front, back, coplanar := b.SplitPolygon(a)
	if coplanar || front == nil || back == nil {
		t.Fatalf("coplanar=%v front=%v back=%v, want a split", coplanar, front, back)
	}
	if len(front.Points()) != 3 || len(back.Points()) != 5 {
		t.Fatalf("pieces have %d and %d points, want 3 and 5", len(front.Points()), len(back.Points()))
	}
	saved := append([]p3(nil), back.Points()...)

	c := NewDrawPolygon(nil, []p3{{X: 0, Y: 0, Z: 10}, {X: 1, Y: -0.05, Z: 0}, {X: 10, Y: 0.05, Z: 9}}, v3{X: 0.005555, Y: -0.99997, Z: 0.005555}, 0)
	c.ConstructNormal()
	front2, back2, coplanar := c.SplitPolygon(back)
	if coplanar || front2 == nil || back2 == nil {
		t.Fatalf("second split: coplanar=%v front=%v back=%v", coplanar, front2, back2)
	}
	if len(front2.Points()) != 4 || len(back2.Points()) != 3 {
		t.Errorf("second pieces have %d and %d points, want 4 and 3", len(front2.Points()), len(back2.Points()))
	}
	for _, v := range saved {
		if !contains(front2.Points(), v) && !contains(back2.Points(), v) {
			t.Errorf("vertex %v lost", v)
		}
	}
}

func allFinite(t *testing.T, p *DrawPolygon) {
	t.Helper()
	for _, pt := range p.Points() {
		for _, c := range []float64{pt.X, pt.Y, pt.Z} {
			if math.IsNaN(c) || math.IsInf(c, 0) {
				t.Errorf("non-finite point %v", pt)
			}
		}
	}
}

// f32 rounds points to float, as the upstream test's float literals are.
func f32(points []p3) []p3 {
	out := make([]p3, len(points))
	for i, p := range points {
		out[i] = p3{X: float64(float32(p.X)), Y: float64(float32(p.Y)), Z: float64(float32(p.Z))}
	}
	return out
}

// SplitNoNaNs (crbug.com/1264787).
func TestDrawPolygonSplit_SplitNoNaNs(t *testing.T) {
	splitting := NewDrawPolygon(nil, f32([]p3{
		{X: -438777.031, Y: -999211.938, Z: 782768.312}, {X: 733.815186, Y: 168.424362, Z: -133.606277},
		{X: 829.194641, Y: 173.089539, Z: -80.6844864}, {X: -237207.141, Y: -999021.0, Z: 904761.812},
	}), v3{}, 0)
	split := NewDrawPolygon(nil, f32([]p3{
		{X: 1068.28625, Y: 184.783997, Z: 51.9764748}, {X: 733.815247, Y: 168.424393, Z: -133.606277},
		{X: 567.472534, Y: 456.538422, Z: -133.606277}, {X: 817.688904, Y: 618.831238, Z: 51.9764748},
	}), v3{}, 1)
	splitting.ConstructNormal()
	split.ConstructNormal()
	front, back, coplanar := splitting.SplitPolygon(split)
	if coplanar || front == nil || back == nil {
		t.Fatalf("coplanar=%v front=%v back=%v, want a split", coplanar, front, back)
	}
	allFinite(t, front)
	allFinite(t, back)
}

// SplitNoInfs (crbug.com/1264787). Upstream notes that with better
// gfx::Transform precision the near-plane vertex lands behind, so there is
// no front piece; that hinges on float rounding near a vertex clamped to
// ~1e6, which is why the gfx/cc/viz ports compute in float without fused
// multiply-adds.
func TestDrawPolygonSplit_SplitNoInfs(t *testing.T) {
	splitting := NewDrawPolygonFromRect(nil, geometry.NewRectF(0, 0, 1140, 418), rowMajor(
		1.39069271, -0.131110176, 0.5, -503.545319,
		1.00984585, -0.273445308, -0.866025388, -828.169128,
		-0.722961724, 0.690888107, 4.23046966e-17, 570.098511,
		0.00144592347, -0.0013817763, -8.46093968e-20, -0.140197217), 0)
	split := NewDrawPolygonFromRect(nil, geometry.NewRectF(430, 0, 560, 454), rowMajor(
		0.131110176, -0.5, 1.39069271, 952.04541,
		0.273445308, 0.866025388, 1.00984585, 52.3442955,
		-0.690888107, 0, -0.722961724, 429.901398,
		0.0013817763, 0, 0.00144592347, 0.140197009), 1)
	front, back, coplanar := splitting.SplitPolygon(split)
	if coplanar || front != nil || back == nil {
		t.Fatalf("coplanar=%v front=%v back=%v, want back only", coplanar, front, back)
	}
	allFinite(t, back)
}

func TestDrawPolygonTransform_TransformNormal(t *testing.T) {
	a := NewDrawPolygon(nil, []p3{{X: 1, Y: 0, Z: 1}, {X: -1, Y: 0, Z: -1}, {X: 0, Y: 1, Z: 0}}, v3{X: math.Sqrt2 / 2, Z: -math.Sqrt2 / 2}, 0)
	expectNormal(t, a, math.Sqrt2/2, 0, -math.Sqrt2/2)
	var tr geometry.Transform
	tr.RotateAboutYAxis(45)
	a.TransformToScreenSpace(tr)
	expectNormal(t, a, 0, 0, -1)
}

func TestDrawPolygon_ToQuads2D(t *testing.T) {
	// A pentagon fans into a quad and a degenerate triangle quad.
	p := NewDrawPolygon(nil, []p3{{X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 15, Y: 5, Z: 0}, {X: 10, Y: 10, Z: 0}, {X: 0, Y: 10, Z: 0}}, v3{Z: 1}, 0)
	q := p.ToQuads2D()
	want := []geometry.QuadF{
		{P1: geometry.PointF{X: 0, Y: 0}, P2: geometry.PointF{X: 10, Y: 0}, P3: geometry.PointF{X: 15, Y: 5}, P4: geometry.PointF{X: 10, Y: 10}},
		{P1: geometry.PointF{X: 0, Y: 0}, P2: geometry.PointF{X: 10, Y: 10}, P3: geometry.PointF{X: 0, Y: 10}, P4: geometry.PointF{X: 0, Y: 10}},
	}
	if len(q) != len(want) {
		t.Fatalf("quads = %v, want %v", q, want)
	}
	for i := range want {
		if q[i] != want[i] {
			t.Errorf("quad %d = %v, want %v", i, q[i], want[i])
		}
	}
}
