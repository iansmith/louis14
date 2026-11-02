package cc

import (
	"math"
	"testing"

	"louis14/pkg/geometry"
)

// Ported from cc/base/math_util_unittest.cc:645-1031 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b (the MapClippedQuad3d tests).
// EXPECT_EQ on a float compares float32 values.

func pt(x, y float64) geometry.PointF { return geometry.PointF{X: x, Y: y} }

func feq(t *testing.T, what string, got, want float64) {
	t.Helper()
	if float32(got) != float32(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func between(t *testing.T, what string, lo, got, hi float64, loInclusive, hiInclusive bool) {
	t.Helper()
	g := float64(float32(got))
	okLo := g > lo || (loInclusive && g == lo)
	okHi := g < hi || (hiInclusive && g == hi)
	if !okLo || !okHi {
		t.Errorf("%s = %v, want in %v..%v", what, got, lo, hi)
	}
}

func TestMapClippedQuadDuplicateTriangle(t *testing.T) {
	var tr geometry.Transform
	tr.ApplyPerspectiveDepth(50)
	tr.RotateAboutYAxis(89)
	pts, _ := MapClippedQuad3d(tr, geometry.QuadF{P1: pt(0, -50), P2: pt(0, -100), P3: pt(-99, -300), P4: pt(-99, -100)})
	for i, p := range pts {
		if p.Y > 0 {
			t.Errorf("point %d y = %v: w<0 space leaked in", i, p.Y)
		}
	}
	if len(pts) != 4 {
		t.Errorf("%d vertices, want 4", len(pts))
	}
}

func TestMapClippedQuadDuplicatePoints(t *testing.T) {
	var tr geometry.Transform
	tr.RotateAboutYAxis(45)
	pts, _ := MapClippedQuad3d(tr, geometry.QuadF{P1: pt(-99, -50), P2: pt(-99, -50), P3: pt(0, 100), P4: pt(0, -100)})
	if len(pts) != 3 {
		t.Errorf("%d vertices, want 3", len(pts))
	}
}

func TestMapClippedQuadDuplicatePointsWrapped(t *testing.T) {
	var tr geometry.Transform
	tr.RotateAboutYAxis(45)
	pts, _ := MapClippedQuad3d(tr, geometry.QuadF{P1: pt(-99, -50), P2: pt(0, 100), P3: pt(0, -100), P4: pt(-99, -50)})
	if len(pts) != 3 {
		t.Errorf("%d vertices, want 3", len(pts))
	}
}

func TestMapClippedQuadDuplicateQuad(t *testing.T) {
	var tr geometry.Transform
	tr.ApplyPerspectiveDepth(50)
	tr.RotateAboutYAxis(89)
	pts, _ := MapClippedQuad3d(tr, geometry.QuadF{P1: pt(0, -50), P2: pt(400, -50), P3: pt(0, -100), P4: pt(-99, -300)})
	for i, p := range pts {
		if p.Y > 0 {
			t.Errorf("point %d y = %v: w<0 space leaked in", i, p.Y)
		}
	}
	if len(pts) != 5 {
		t.Errorf("%d vertices, want 5", len(pts))
	}
}

func TestMapClippedQuadInfiniteInSomeDimensions(t *testing.T) {
	var tr geometry.Transform
	tr.ApplyPerspectiveDepth(50)
	tr.RotateAboutXAxis(89)
	q, _ := MapClippedQuad3d(tr, geometry.QuadF{P1: pt(0, 0), P2: pt(0, 100), P3: pt(100, 100), P4: pt(100, 0)})
	if len(q) != 4 {
		t.Fatalf("%d vertices, want 4", len(q))
	}
	feq(t, "q0.x", q[0].X, 0)
	feq(t, "q0.y", q[0].Y, 0)
	feq(t, "q0.z", q[0].Z, 0)
	feq(t, "q1.x", q[1].X, 0)
	between(t, "q1.y", 17000, q[1].Y, 18000, false, false)
	between(t, "q1.z", 998000, q[1].Z, 1000000, false, true)
	between(t, "q2.x", 998000, q[2].X, 1000000, false, true)
	between(t, "q2.y", 8500, q[2].Y, 9000, false, false)
	between(t, "q2.z", 499000, q[2].Z, 500000, false, true)
	feq(t, "q3.x", q[3].X, 100)
	feq(t, "q3.y", q[3].Y, 0)
	feq(t, "q3.z", q[3].Z, 0)
}

func TestMapClippedQuadInfiniteInSomeDimensionsNonZero(t *testing.T) {
	var tr geometry.Transform
	tr.Translate(100, 0)
	tr.ApplyPerspectiveDepth(50)
	tr.RotateAboutXAxis(89)
	tr.Translate(-100, 0)
	q, _ := MapClippedQuad3d(tr, geometry.QuadF{P1: pt(0, 0), P2: pt(0, 100), P3: pt(100, 100), P4: pt(100, 0)})
	if len(q) != 4 {
		t.Fatalf("%d vertices, want 4", len(q))
	}
	feq(t, "q0.x", q[0].X, 0)
	feq(t, "q0.y", q[0].Y, 0)
	feq(t, "q0.z", q[0].Z, 0)
	between(t, "q1.x", -1000000, q[1].X, -998000, true, false)
	between(t, "q1.y", 8500, q[1].Y, 9000, false, false)
	between(t, "q1.z", 499000, q[1].Z, 500000, false, true)
	feq(t, "q2.x", q[2].X, 100)
	between(t, "q2.y", 17000, q[2].Y, 18000, false, false)
	between(t, "q2.z", 996000, q[2].Z, 1000000, false, true)
	feq(t, "q3.x", q[3].X, 100)
	feq(t, "q3.y", q[3].Y, 0)
	feq(t, "q3.z", q[3].Z, 0)
}

func TestMapClippedQuadClampInvisiblePlane(t *testing.T) {
	src := geometry.QuadF{P1: pt(0, 0), P2: pt(0, 1000), P3: pt(1000, 1000), P4: pt(1000, 0)}
	check := func(name string, tr geometry.Transform, want [4][3]float64) {
		t.Helper()
		q, _ := MapClippedQuad3d(tr, src)
		if len(q) != 4 {
			t.Fatalf("%s: %d vertices, want 4", name, len(q))
		}
		for i, w := range want {
			feq(t, name+" x", q[i].X, w[0])
			feq(t, name+" y", q[i].Y, w[1])
			feq(t, name+" z", q[i].Z, w[2])
		}
	}
	var a geometry.Transform
	a.Translate(100, 0)
	a.RotateAboutYAxis(90)
	a.Scale(10000, 10000)
	check("rotateY", a, [4][3]float64{{100, 0, 0}, {100, 1000000, 0}, {100, 1000000, -1000000}, {100, 0, -1000000}})

	var b geometry.Transform
	b.Translate(0, -50)
	b.RotateAboutXAxis(-90)
	b.Scale(10000, 10000)
	check("rotateX", b, [4][3]float64{{0, -50, 0}, {0, -50, -1000000}, {1000000, -50, -1000000}, {1000000, -50, 0}})

	var c geometry.Transform
	c.Translate(10, 10)
	c.Rotate(30)
	c.RotateAboutXAxis(90)
	c.Scale(10000, 10000)
	check("rotate+rotateX", c, [4][3]float64{{10, 10, 0}, {10, 10, 1000000}, {1000000, 1000000, 1000000}, {1000000, 1000000, 0}})
}

func TestMapClippedQuadClampWholePlane(t *testing.T) {
	var tr geometry.Transform
	tr.Scale3d(1000, 1000, 1000)
	tr.Translate3d(0, 0, 10000)
	tr.RotateAboutXAxis(-45)
	q, _ := MapClippedQuad3d(tr, geometry.QuadF{P1: pt(0, 0), P2: pt(0, 10000), P3: pt(100, 10000), P4: pt(100, -10000)})
	if len(q) != 4 {
		t.Fatalf("%d vertices, want 4", len(q))
	}
	feq(t, "q0.x", q[0].X, 0)
	feq(t, "q0.y", q[0].Y, 0)
	between(t, "q0.z", 750000, q[0].Z, 750001, true, true)
	feq(t, "q1.x", q[1].X, 0)
	between(t, "q1.y", 999999, q[1].Y, 1000000, true, true)
	between(t, "q1.z", -250001, q[1].Z, -249999, true, true)
	between(t, "q2.x", 14100, q[2].X, 14200, true, true)
	between(t, "q2.y", 999999, q[2].Y, 1000000, true, true)
	between(t, "q2.z", -250001, q[2].Z, -249999, true, true)
	between(t, "q3.x", 3500, q[3].X, 3600, true, true)
	between(t, "q3.y", -250001, q[3].Y, -249999, true, true)
	feq(t, "q3.z", q[3].Z, 1000000)
}

func TestMapClippedQuadClampWholePlaneBelow(t *testing.T) {
	var tr geometry.Transform
	tr.Scale3d(1000, 1000, 1000)
	tr.Translate3d(0, 0, -5000)
	tr.RotateAboutYAxis(30)
	q, _ := MapClippedQuad3d(tr, geometry.QuadF{P1: pt(0, 0), P2: pt(-10000, 100), P3: pt(10000, 100), P4: pt(10000, 0)})
	if len(q) != 4 {
		t.Fatalf("%d vertices, want 4", len(q))
	}
	feq(t, "q0.x", q[0].X, 0)
	feq(t, "q0.y", q[0].Y, 0)
	between(t, "q0.z", -750001, q[0].Z, -750000, true, true)
	feq(t, "q1.x", q[1].X, -1000000)
	between(t, "q1.y", 11540, q[1].Y, 11550, true, true)
	between(t, "q1.z", -172660, q[1].Z, -172640, true, true)
	between(t, "q2.x", 433000, q[2].X, 433025, true, true)
	between(t, "q2.y", 4999.9, q[2].Y, 5000.1, false, false)
	feq(t, "q2.z", q[2].Z, -1000000)
	between(t, "q3.x", 433000, q[3].X, 433025, true, true)
	feq(t, "q3.y", q[3].Y, 0)
	feq(t, "q3.z", q[3].Z, -1000000)
}

func TestMapClippedQuadInfiniteMatrix(t *testing.T) {
	tr := geometry.TransformRowMajor(
		1, 0, 0, 0,
		0, -100, 0, math.Inf(1),
		0, 0, 1, 0,
		0, 0, 0, 1)
	// Nothing to check but that it does not panic.
	MapClippedQuad3d(tr, geometry.QuadF{P1: pt(0, 1), P2: pt(1, 1), P3: pt(1, 2), P4: pt(0, 2)})
}
