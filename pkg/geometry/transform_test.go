package geometry

import (
	"math"
	"testing"
)

// Tests ported from Chromium's ui/gfx/geometry/transform_unittest.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b (XFormTest.*). Expectations are
// Chromium's, unchanged.

// kErrorThreshold mirrors transform_unittest.cc's tolerance for the 45°
// rotation cases.
const kErrorThreshold = 1e-7

func getTestMatrix1() Transform {
	return TransformColMajor(10, 11, 12, 13,
		14, 15, 16, 17,
		18, 19, 20, 21,
		22, 23, 24, 25)
}

func getTestMatrix2() Transform {
	return TransformRowMajor(30, 34, 38, 42,
		31, 35, 39, 43,
		32, 36, 40, 44,
		33, 37, 41, 45)
}

// expectRow is EXPECT_ROWn_EQ: EXPECT_FLOAT_EQ (4 ULPs in float) on each
// entry of row r.
func expectRow(t *testing.T, tr Transform, r int, want ...float64) {
	t.Helper()
	for c, w := range want {
		if !floatEq(tr.Rc(r, c), w) {
			t.Errorf("rc(%d,%d) = %v, want %v", r, c, tr.Rc(r, c), w)
		}
	}
}

func expectRowNear(t *testing.T, tr Transform, r int, tol float64, want ...float64) {
	t.Helper()
	for c, w := range want {
		if math.Abs(tr.Rc(r, c)-w) > tol {
			t.Errorf("rc(%d,%d) = %v, want %v ±%v", r, c, tr.Rc(r, c), w, tol)
		}
	}
}

func expectIdentityRows(t *testing.T, tr Transform) {
	t.Helper()
	expectRow(t, tr, 0, 1, 0, 0, 0)
	expectRow(t, tr, 1, 0, 1, 0, 0)
	expectRow(t, tr, 2, 0, 0, 1, 0)
	expectRow(t, tr, 3, 0, 0, 0, 1)
}

// floatEq mirrors gtest's EXPECT_FLOAT_EQ: equal as floats within 4 ULPs.
func floatEq(a, b float64) bool {
	fa, fb := float32(a), float32(b)
	if fa == fb {
		return true
	}
	ia, ib := int64(math.Float32bits(fa)), int64(math.Float32bits(fb))
	if (ia < 0) != (ib < 0) {
		return false
	}
	d := ia - ib
	if d < 0 {
		d = -d
	}
	return d <= 4
}

func expectTransformNear(t *testing.T, a, b Transform, tol float64) {
	t.Helper()
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			if math.Abs(a.Rc(r, c)-b.Rc(r, c)) > tol {
				t.Errorf("rc(%d,%d): %v vs %v (tol %v)", r, c, a.Rc(r, c), b.Rc(r, c), tol)
			}
		}
	}
}

func TestTransformDefaultIsIdentity(t *testing.T) {
	var a Transform
	expectIdentityRows(t, a)
	if !a.IsIdentity() {
		t.Error("zero Transform is not identity")
	}
}

func TestTransformPreConcat(t *testing.T) {
	a := getTestMatrix1()
	a.PreConcat(getTestMatrix2())
	expectRow(t, a, 0, 2036, 2292, 2548, 2804)
	expectRow(t, a, 1, 2162, 2434, 2706, 2978)
	expectRow(t, a, 2, 2288, 2576, 2864, 3152)
	expectRow(t, a, 3, 2414, 2718, 3022, 3326)
	if !a.Equal(getTestMatrix1().Mul(getTestMatrix2())) {
		t.Error("PreConcat != operator*")
	}
}

func TestTransformInverse(t *testing.T) {
	var identity, inverseIdentity Transform
	if !identity.GetInverse(&inverseIdentity) || !identity.Equal(inverseIdentity) {
		t.Error("identity inverse")
	}

	var translation Transform
	translation.Translate3d(2, 3, 4)
	var inv Transform
	if !translation.IsInvertible() || !translation.GetInverse(&inv) {
		t.Fatal("translation not invertible")
	}
	expectRow(t, inv, 0, 1, 0, 0, -2)
	expectRow(t, inv, 1, 0, 1, 0, -3)
	expectRow(t, inv, 2, 0, 0, 1, -4)
	expectRow(t, inv, 3, 0, 0, 0, 1)
	if !inv.Equal(translation.InverseOrIdentity()) {
		t.Error("InverseOrIdentity != GetInverse")
	}
	// GetInverse with the parameter pointing to itself.
	if !translation.GetInverse(&translation) || !translation.Equal(inv) {
		t.Error("self GetInverse")
	}

	var scale Transform
	scale.Scale3d(4, 10, 100)
	var invScale Transform
	if !scale.GetInverse(&invScale) {
		t.Fatal("scale not invertible")
	}
	expectRow(t, invScale, 0, 0.25, 0, 0, 0)
	expectRow(t, invScale, 1, 0, 0.1, 0, 0)
	expectRow(t, invScale, 2, 0, 0, 0.01, 0)
	expectRow(t, invScale, 3, 0, 0, 0, 1)

	var m1, m2 Transform
	m1.Translate(10, 20)
	m1.Rotate(30)
	m2.Rotate(-30)
	m2.Translate(-10, -20)
	var im1, im2 Transform
	if !m1.GetInverse(&im1) || !m2.GetInverse(&im2) {
		t.Fatal("2d inverse")
	}
	if !im1.Is2dTransform() || !im2.Is2dTransform() {
		t.Error("2d inverse not 2d")
	}
	expectTransformNear(t, m1, im2, 1e-6)
	expectTransformNear(t, m2, im1, 1e-6)

	var p1, p2 Transform
	p1.RotateAboutZAxis(-30)
	p1.RotateAboutYAxis(10)
	p1.RotateAboutXAxis(20)
	p1.ApplyPerspectiveDepth(100)
	p2.ApplyPerspectiveDepth(-100)
	p2.RotateAboutXAxis(-20)
	p2.RotateAboutYAxis(-10)
	p2.RotateAboutZAxis(30)
	var ip1, ip2 Transform
	if !p1.GetInverse(&ip1) || !p2.GetInverse(&ip2) {
		t.Fatal("3d inverse")
	}
	expectTransformNear(t, p1, ip2, 1e-6)
	expectTransformNear(t, p2, ip1, 1e-6)

	var uninvertible Transform
	uninvertible.SetRc(0, 0, 0)
	uninvertible.SetRc(1, 1, 0)
	uninvertible.SetRc(2, 2, 0)
	uninvertible.SetRc(3, 3, 0)
	if uninvertible.IsInvertible() {
		t.Error("zero matrix invertible")
	}
	var out Transform
	out.Scale3d(4, 10, 100)
	if uninvertible.GetInverse(&out) {
		t.Error("GetInverse of zero matrix succeeded")
	}
	if !out.IsIdentity() {
		t.Error("failed GetInverse must reset the output to identity")
	}
}

func TestTransformBackfaceVisibilityBasicCases(t *testing.T) {
	var tr Transform
	if tr.IsBackFaceVisible() {
		t.Error("identity")
	}
	for _, tc := range []struct {
		deg  float64
		want bool
	}{{80, false}, {100, true}, {90, false}} {
		tr.MakeIdentity()
		tr.RotateAboutYAxis(tc.deg)
		if got := tr.IsBackFaceVisible(); got != tc.want {
			t.Errorf("rotateY(%v): %v, want %v", tc.deg, got, tc.want)
		}
	}
	for _, s := range [][2]float64{{1, 2}, {-1, 2}, {1, -2}, {-1, -2}} {
		if MakeScale(s[0], s[1]).IsBackFaceVisible() {
			t.Errorf("2d scale %v must not show the back face", s)
		}
	}
}

func TestTransformBackfaceVisibilityForPerspective(t *testing.T) {
	var p Transform
	p.ApplyPerspectiveDepth(1)
	p.Translate3d(0, 0, 0)
	p.RotateAboutYAxis(100)
	if !p.IsBackFaceVisible() {
		t.Error("case 1")
	}
	p.MakeIdentity()
	p.ApplyPerspectiveDepth(1)
	p.Translate3d(-10, 0, 0)
	p.RotateAboutYAxis(100)
	if p.IsBackFaceVisible() {
		t.Error("case 2")
	}
	p.RotateAboutYAxis(180)
	if !p.IsBackFaceVisible() {
		t.Error("case 3")
	}
}

func TestTransformRotateAboutXAxis(t *testing.T) {
	sin45 := 0.5 * math.Sqrt(2)
	cos45 := sin45
	var a Transform
	a.RotateAboutXAxis(90)
	// SinCosDegrees makes multiples of 90° exact.
	expectRow(t, a, 0, 1, 0, 0, 0)
	expectRow(t, a, 1, 0, 0, -1, 0)
	expectRow(t, a, 2, 0, 1, 0, 0)
	expectRow(t, a, 3, 0, 0, 0, 1)
	if a.Rc(1, 1) != 0 {
		t.Errorf("rotateX(90) rc(1,1) = %v, want exactly 0", a.Rc(1, 1))
	}

	a.MakeIdentity()
	a.RotateAboutXAxis(45)
	expectRow(t, a, 0, 1, 0, 0, 0)
	expectRowNear(t, a, 1, kErrorThreshold, 0, cos45, -sin45, 0)
	expectRowNear(t, a, 2, kErrorThreshold, 0, sin45, cos45, 0)

	a.MakeIdentity()
	a.Scale3d(6, 7, 8)
	a.RotateAboutXAxis(90)
	expectRow(t, a, 0, 6, 0, 0, 0)
	expectRow(t, a, 1, 0, 0, -7, 0)
	expectRow(t, a, 2, 0, 8, 0, 0)
	expectRow(t, a, 3, 0, 0, 0, 1)
}

func TestTransformRotateAboutArbitraryAxis(t *testing.T) {
	// verifyRotateAboutForArbitraryAxis: rotate 120° about (1,1,1) cycles
	// the axes.
	var a Transform
	a.RotateAbout(1, 1, 1, 120)
	expectRowNear(t, a, 0, kErrorThreshold, 0, 0, 1, 0)
	expectRowNear(t, a, 1, kErrorThreshold, 1, 0, 0, 0)
	expectRowNear(t, a, 2, kErrorThreshold, 0, 1, 0, 0)
	expectRow(t, a, 3, 0, 0, 0, 1)

	// verifyRotateAboutForDegenerateAxis: a zero axis is a no-op.
	b := getTestMatrix1()
	b.RotateAbout(0, 0, 0, 45)
	if !b.Equal(getTestMatrix1()) {
		t.Error("degenerate axis changed the matrix")
	}
}

func TestTransformSkew(t *testing.T) {
	var a Transform
	a.Skew(45, 0)
	expectRow(t, a, 0, 1, 1, 0, 0)
	expectRow(t, a, 1, 0, 1, 0, 0)
	a.MakeIdentity()
	a.Skew(0, 45)
	expectRow(t, a, 0, 1, 0, 0, 0)
	expectRow(t, a, 1, 1, 1, 0, 0)
	a.MakeIdentity()
	a.Scale3d(6, 7, 8)
	a.Skew(45, 0)
	expectRow(t, a, 0, 6, 6, 0, 0)
	expectRow(t, a, 1, 0, 7, 0, 0)
	expectRow(t, a, 2, 0, 0, 8, 0)
}

func TestTransformPerspectiveDepth(t *testing.T) {
	var a Transform
	a.ApplyPerspectiveDepth(1)
	expectRow(t, a, 0, 1, 0, 0, 0)
	expectRow(t, a, 1, 0, 1, 0, 0)
	expectRow(t, a, 2, 0, 0, 1, 0)
	expectRow(t, a, 3, 0, 0, -1, 1)

	a.MakeIdentity()
	a.Translate3d(2, 3, 4)
	a.ApplyPerspectiveDepth(1)
	expectRow(t, a, 0, 1, 0, -2, 2)
	expectRow(t, a, 1, 0, 1, -3, 3)
	expectRow(t, a, 2, 0, 0, -3, 4)
	expectRow(t, a, 3, 0, 0, -1, 1)
}

func TestTransformHasPerspective(t *testing.T) {
	var a Transform
	a.ApplyPerspectiveDepth(1)
	if !a.HasPerspective() {
		t.Error("perspective 1")
	}
	a.MakeIdentity()
	a.ApplyPerspectiveDepth(0)
	if a.HasPerspective() {
		t.Error("perspective 0 is a no-op")
	}
	for _, c := range []struct {
		col int
		v   float64
	}{{0, -1}, {1, -1}, {2, -0.3}, {3, 0.5}, {3, 0}} {
		a.MakeIdentity()
		a.SetRc(3, c.col, c.v)
		if !a.HasPerspective() {
			t.Errorf("rc(3,%d)=%v", c.col, c.v)
		}
	}
}

func TestTransformIsInvertible(t *testing.T) {
	var a Transform
	if !a.IsInvertible() {
		t.Error("identity")
	}
	a.Translate3d(2, 3, 4)
	a.Scale3d(6, 7, 8)
	a.RotateAboutXAxis(10)
	a.RotateAboutYAxis(20)
	a.RotateAboutZAxis(30)
	a.Skew(45, 0)
	if !a.IsInvertible() {
		t.Error("composite")
	}
	a.MakeIdentity()
	a.ApplyPerspectiveDepth(1)
	if !a.IsInvertible() {
		t.Error("perspective")
	}
	a.SetRc(3, 3, 0)
	if a.IsInvertible() {
		t.Error("pure perspective with rc(3,3)=0")
	}
	a.Scale3d(6, 7, 8)
	if a.IsInvertible() {
		t.Error("still singular after scale")
	}
	// A zero z-scale is singular in 4×4 even though its 2D part is not
	// (CSS scale3d(2, 2, 0)).
	var z Transform
	z.Scale3d(2, 2, 0)
	if z.IsInvertible() {
		t.Error("scale3d(2,2,0) invertible")
	}
}

func TestTransformFlattenAndIsFlat(t *testing.T) {
	a := getTestMatrix1()
	if a.IsFlat() {
		t.Error("test matrix 1 is not flat")
	}
	a.Flatten()
	expectRow(t, a, 0, 10, 14, 0, 22)
	expectRow(t, a, 1, 11, 15, 0, 23)
	expectRow(t, a, 2, 0, 0, 1, 0)
	expectRow(t, a, 3, 13, 17, 0, 25)
	if !a.IsFlat() {
		t.Error("flattened matrix not flat")
	}

	b := getTestMatrix1()
	b.SetRc(0, 2, 0)
	b.SetRc(1, 2, 0)
	b.SetRc(2, 2, 1)
	b.SetRc(3, 2, 0)
	if b.IsFlat() {
		t.Error("third column alone does not make it flat")
	}
	b.SetRc(2, 0, 0)
	b.SetRc(2, 1, 0)
	b.SetRc(2, 3, 0)
	if !b.IsFlat() {
		t.Error("third row and column (0,0,1,0) is flat")
	}
}

func TestTransformMapPoint(t *testing.T) {
	var tr Transform
	tr.Translate3d(1.25, 2.75, 3.875)
	tr.Scale3d(3, 4, 5)
	if got := tr.MapPoint(PointF{12.5, 34.5}); got != (PointF{38.75, 140.75}) {
		t.Errorf("MapPoint = %v", got)
	}
	if got := tr.MapPoint3F(Point3F{12.5, 34.5, 56.5}); got != (Point3F{38.75, 140.75, 286.375}) {
		t.Errorf("MapPoint3F = %v", got)
	}

	tr.MakeIdentity()
	tr.SetRc(3, 0, 0.5)
	tr.SetRc(3, 1, 2)
	tr.SetRc(3, 2, 0.75)
	got := tr.MapPoint(PointF{2, 4})
	if !floatEq(got.X, 0.2) || !floatEq(got.Y, 0.4) {
		t.Errorf("perspective MapPoint = %v, want (0.2, 0.4)", got)
	}

	// 0 in all perspectives is ignored.
	tr.MakeIdentity()
	tr.Translate3d(10, 20, 30)
	tr.SetRc(3, 3, 0)
	if got := tr.MapPoint(PointF{2, 4}); got != (PointF{12, 24}) {
		t.Errorf("w=0 MapPoint = %v", got)
	}
	tr.SetRc(3, 3, math.NaN())
	if got := tr.MapPoint(PointF{2, 4}); got != (PointF{12, 24}) {
		t.Errorf("w=NaN MapPoint = %v", got)
	}

	tr = MakeTranslation(10, 20).Mul(MakeScale(3, 4))
	if got := tr.MapPoint(PointF{12.5, 34.5}); got != (PointF{47.5, 158}) {
		t.Errorf("2d MapPoint = %v", got)
	}
}

func TestTransformProjectPoint(t *testing.T) {
	var tr Transform
	p := PointF{1.25, -3.5}
	if got, clamped := tr.ProjectPoint(p); got != p || clamped {
		t.Errorf("identity ProjectPoint = %v %v", got, clamped)
	}

	tr = MakeTranslation(10, 20).Mul(MakeScale(3, 4))
	if got, clamped := tr.ProjectPoint(p); got != (PointF{13.75, 6}) || clamped {
		t.Errorf("2d ProjectPoint = %v %v", got, clamped)
	}

	// Scale z to 0: the plane is parallel to the ray.
	tr.SetRc(2, 2, 0)
	if got, clamped := tr.ProjectPoint(p); got != (PointF{}) || !clamped {
		t.Errorf("z-scale 0 ProjectPoint = %v %v", got, clamped)
	}
	if got := tr.MapPoint(p); got != (PointF{13.75, 6}) {
		t.Errorf("MapPoint unaffected by z scale: %v", got)
	}

	tr.MakeIdentity()
	tr.RotateAboutYAxis(60)
	if got, clamped := tr.ProjectPoint(p); got != (PointF{2.5, -3.5}) || clamped {
		t.Errorf("rotateY(60) ProjectPoint = %v %v", got, clamped)
	}
	if got := tr.MapPoint(p); got != (PointF{0.625, -3.5}) {
		t.Errorf("rotateY(60) MapPoint = %v", got)
	}

	tr.ApplyPerspectiveDepth(10)
	got, clamped := tr.ProjectPoint(p)
	if clamped || math.Abs(got.X-3.19) > 0.01 || math.Abs(got.Y+4.47) > 0.01 {
		t.Errorf("perspective ProjectPoint = %v %v", got, clamped)
	}

	// The ray misses the destination plane.
	tr.ApplyPerspectiveDepth(2)
	big := float64(int64(1) << 23)
	got, clamped = tr.ProjectPoint(p)
	if !clamped || got.X != big || got.Y != -big {
		t.Errorf("clamped ProjectPoint = %v %v, want (±%v) clamped", got, clamped, big)
	}
}

func TestTransformCreates3d(t *testing.T) {
	var id Transform
	if id.Creates3d() || MakeTranslation(1, 2).Creates3d() {
		t.Error("2d")
	}
	var tr Transform
	tr.ApplyPerspectiveDepth(100)
	if tr.Creates3d() {
		t.Error("perspective only")
	}
	tr.Scale3d(2, 3, 4)
	if tr.Creates3d() {
		t.Error("perspective + scale3d")
	}
	tr.Translate3d(1, 2, 3)
	if !tr.Creates3d() {
		t.Error("perspective + translate3d")
	}
	tr.MakeIdentity()
	tr.RotateAboutYAxis(20)
	if !tr.Creates3d() {
		t.Error("rotateY")
	}
}

func TestTransformApplyTransformOrigin(t *testing.T) {
	var tr Transform
	tr.Scale3d(2, 3, 4)
	tr.ApplyTransformOrigin(1, 2, 3)
	if got := tr.MapPoint3F(Point3F{1, 2, 3}); got != (Point3F{1, 2, 3}) {
		t.Errorf("origin not fixed: %v", got)
	}
	if got := tr.MapPoint3F(Point3F{0, 0, 0}); got != (Point3F{-1, -4, -9}) {
		t.Errorf("MapPoint3F(0,0,0) = %v, want (-1,-4,-9)", got)
	}
}

func TestSinCosDegreesExactQuadrants(t *testing.T) {
	for _, tc := range []struct{ deg, sin, cos float64 }{
		{0, 0, 1}, {90, 1, 0}, {180, 0, -1}, {270, -1, 0}, {-90, -1, 0}, {360, 0, 1},
	} {
		sc := SinCosDegrees(tc.deg)
		if sc.Sin != tc.sin || sc.Cos != tc.cos {
			t.Errorf("SinCosDegrees(%v) = %+v, want sin %v cos %v", tc.deg, sc, tc.sin, tc.cos)
		}
	}
	sc := SinCosDegrees(30)
	if math.Abs(sc.Sin-0.5) > 1e-15 || math.Abs(sc.Cos-math.Sqrt(3)/2) > 1e-15 {
		t.Errorf("SinCosDegrees(30) = %+v", sc)
	}
	sc = SinCosDegrees(-120)
	if math.Abs(sc.Sin+math.Sqrt(3)/2) > 1e-15 || math.Abs(sc.Cos+0.5) > 1e-15 {
		t.Errorf("SinCosDegrees(-120) = %+v", sc)
	}
}
