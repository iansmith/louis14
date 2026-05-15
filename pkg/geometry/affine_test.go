package geometry

import (
	"math"
	"testing"
)

const affineEps = 1e-9

func approxEq(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return false
	}
	return math.Abs(a-b) <= affineEps
}

func affineEqual(a, b AffineTransform) bool {
	return approxEq(a.A, b.A) && approxEq(a.B, b.B) && approxEq(a.C, b.C) &&
		approxEq(a.D, b.D) && approxEq(a.E, b.E) && approxEq(a.F, b.F)
}

func TestIdentity(t *testing.T) {
	id := Identity()
	if !id.IsIdentity() {
		t.Fatalf("Identity().IsIdentity() = false")
	}
	if got := id.Determinant(); !approxEq(got, 1) {
		t.Errorf("Identity().Determinant() = %v, want 1", got)
	}
	p := id.MapPoint(PointF{X: 3, Y: 7})
	if p.X != 3 || p.Y != 7 {
		t.Errorf("Identity.MapPoint({3,7}) = %v, want {3,7}", p)
	}
}

func TestTranslate(t *testing.T) {
	tt := Translate(10, -5)
	p := tt.MapPoint(PointF{X: 2, Y: 3})
	if p.X != 12 || p.Y != -2 {
		t.Errorf("Translate(10,-5).MapPoint({2,3}) = %v, want {12,-2}", p)
	}
	if tt.IsIdentity() {
		t.Errorf("non-identity translate reported IsIdentity")
	}
}

func TestScale(t *testing.T) {
	s := Scale(2, 3)
	p := s.MapPoint(PointF{X: 4, Y: 5})
	if p.X != 8 || p.Y != 15 {
		t.Errorf("Scale(2,3).MapPoint({4,5}) = %v, want {8,15}", p)
	}
	if got := s.Determinant(); !approxEq(got, 6) {
		t.Errorf("Scale(2,3).Determinant() = %v, want 6", got)
	}
}

// TestTranslateThenScale checks that Compose applies the right-hand operand
// first. translate.Compose(scale) means: scale first, then translate.
func TestTranslateThenScale(t *testing.T) {
	scale := Scale(2, 3)
	tr := Translate(5, 7)
	// scaleThenTranslate(p) = translate(scale(p)) — scale first, then translate.
	stt := tr.Compose(scale)
	p := stt.MapPoint(PointF{X: 1, Y: 1})
	// scale(1,1) -> (2,3); translate(2,3) by (5,7) -> (7,10).
	if !approxEq(p.X, 7) || !approxEq(p.Y, 10) {
		t.Errorf("(translate∘scale).MapPoint({1,1}) = %v, want {7,10}", p)
	}

	// translateThenScale(p) = scale(translate(p)).
	tts := scale.Compose(tr)
	q := tts.MapPoint(PointF{X: 1, Y: 1})
	// translate(1,1) by (5,7) -> (6,8); scale by (2,3) -> (12,24).
	if !approxEq(q.X, 12) || !approxEq(q.Y, 24) {
		t.Errorf("(scale∘translate).MapPoint({1,1}) = %v, want {12,24}", q)
	}
}

// TestScaleThenRotate checks composition with rotation.
func TestScaleThenRotate(t *testing.T) {
	// Rotate 90° clockwise (SVG convention, y-down): (x,y) -> (-y, x).
	rot := Rotate(90)
	scale := Scale(2, 2)

	// rotate.Compose(scale): scale first, then rotate.
	composed := rot.Compose(scale)
	p := composed.MapPoint(PointF{X: 1, Y: 0})
	// scale: (1,0) -> (2,0). rotate 90° CW: (2,0) -> (0,2).
	if !approxEq(p.X, 0) || !approxEq(p.Y, 2) {
		t.Errorf("(rotate∘scale).MapPoint({1,0}) = %v, want approx {0,2}", p)
	}

	// scale.Compose(rotate): rotate first, then scale (uniform scale, same result here).
	composed2 := scale.Compose(rot)
	q := composed2.MapPoint(PointF{X: 1, Y: 0})
	// rotate: (1,0) -> (0,1). scale: (0,1) -> (0,2).
	if !approxEq(q.X, 0) || !approxEq(q.Y, 2) {
		t.Errorf("(scale∘rotate).MapPoint({1,0}) = %v, want approx {0,2}", q)
	}
}

func TestInvertRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		t    AffineTransform
	}{
		{"translate", Translate(7, -3)},
		{"scale", Scale(2, 0.5)},
		{"rotate45", Rotate(45)},
		{"skewx30", SkewX(30)},
		{"skewy20", SkewY(20)},
		{"composed", Translate(5, 7).Compose(Scale(2, 3)).Compose(Rotate(30))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv, ok := tc.t.Invert()
			if !ok {
				t.Fatalf("Invert() returned ok=false for non-singular transform")
			}
			rt := tc.t.Compose(inv)
			if !affineEqual(rt, Identity()) {
				t.Errorf("t · t^-1 = %+v, want identity", rt)
			}
			rt2 := inv.Compose(tc.t)
			if !affineEqual(rt2, Identity()) {
				t.Errorf("t^-1 · t = %+v, want identity", rt2)
			}
			// MapPoint round-trip.
			p := PointF{X: 11, Y: -7}
			pp := inv.MapPoint(tc.t.MapPoint(p))
			if !approxEq(pp.X, p.X) || !approxEq(pp.Y, p.Y) {
				t.Errorf("inv(t(p)) = %v, want %v", pp, p)
			}
		})
	}
}

func TestInvertSingular(t *testing.T) {
	// A non-uniform scale by zero is singular.
	s := Scale(0, 5)
	_, ok := s.Invert()
	if ok {
		t.Errorf("Invert(singular) returned ok=true")
	}
}

// TestMapRectAxisAligned ensures MapRect is exact for pure translation and
// pure axis-aligned scale.
func TestMapRectAxisAligned(t *testing.T) {
	r := NewRectF(10, 20, 30, 40)

	tr := Translate(5, -7)
	got := tr.MapRect(r)
	want := NewRectF(15, 13, 30, 40)
	if got != want {
		t.Errorf("translate.MapRect = %+v, want %+v", got, want)
	}

	sc := Scale(2, 3)
	got2 := sc.MapRect(r)
	want2 := NewRectF(20, 60, 60, 120)
	if got2 != want2 {
		t.Errorf("scale.MapRect = %+v, want %+v", got2, want2)
	}
}

// TestMapRectRotated ensures MapRect returns the AABB of the rotated quad.
func TestMapRectRotated(t *testing.T) {
	// Rotate a unit square 45°. AABB should have side √2 centred (roughly).
	r := NewRectF(-0.5, -0.5, 1, 1)
	rot := Rotate(45)
	got := rot.MapRect(r)
	// Expected AABB: side = √2; centred at origin.
	side := math.Sqrt2
	if !approxEq(got.Width(), side) || !approxEq(got.Height(), side) {
		t.Errorf("rotate45.MapRect size = (%v,%v), want approx (%v,%v)",
			got.Width(), got.Height(), side, side)
	}
	if !approxEq(got.X(), -side/2) || !approxEq(got.Y(), -side/2) {
		t.Errorf("rotate45.MapRect origin = (%v,%v), want approx (%v,%v)",
			got.X(), got.Y(), -side/2, -side/2)
	}
}

func TestMapRectEmpty(t *testing.T) {
	// Empty rect maps to a zero-size rect at the transformed origin.
	r := RectF{Origin: PointF{X: 3, Y: 4}}
	tr := Translate(10, 10)
	got := tr.MapRect(r)
	if got.X() != 13 || got.Y() != 14 || got.Width() != 0 || got.Height() != 0 {
		t.Errorf("translate.MapRect(empty) = %+v, want origin (13,14) size (0,0)", got)
	}
}

func TestComposeIdentity(t *testing.T) {
	id := Identity()
	tr := Translate(5, 7)
	if !affineEqual(tr.Compose(id), tr) {
		t.Errorf("t.Compose(identity) != t")
	}
	if !affineEqual(id.Compose(tr), tr) {
		t.Errorf("identity.Compose(t) != t")
	}
}

func TestNewAffineTransform(t *testing.T) {
	m := NewAffineTransform(1, 2, 3, 4, 5, 6)
	if m.A != 1 || m.B != 2 || m.C != 3 || m.D != 4 || m.E != 5 || m.F != 6 {
		t.Errorf("NewAffineTransform fields mismatch: %+v", m)
	}
}
