package svg

import (
	"math"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

const transformEpsilon = 1e-9

func nearAffine(a, b geometry.AffineTransform) bool {
	return math.Abs(a.A-b.A) < transformEpsilon &&
		math.Abs(a.B-b.B) < transformEpsilon &&
		math.Abs(a.C-b.C) < transformEpsilon &&
		math.Abs(a.D-b.D) < transformEpsilon &&
		math.Abs(a.E-b.E) < transformEpsilon &&
		math.Abs(a.F-b.F) < transformEpsilon
}

func TestParseSVGTransform_AllFunctions(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want geometry.AffineTransform
	}{
		// translate
		{"translate-1arg", "translate(10)", geometry.Translate(10, 0)},
		{"translate-2arg", "translate(10, 20)", geometry.Translate(10, 20)},
		{"translate-spaces", "translate(  -3.5  4e1  )", geometry.Translate(-3.5, 40)},
		{"translate-negative", "translate(-10,-20)", geometry.Translate(-10, -20)},

		// scale
		{"scale-1arg", "scale(2)", geometry.Scale(2, 2)},
		{"scale-2arg", "scale(2, 3)", geometry.Scale(2, 3)},

		// rotate
		{"rotate-1arg", "rotate(90)", geometry.Rotate(90)},
		{"rotate-3arg", "rotate(45, 10, 10)",
			geometry.Translate(10, 10).
				Compose(geometry.Rotate(45)).
				Compose(geometry.Translate(-10, -10))},

		// skew
		{"skewX", "skewX(30)", geometry.SkewX(30)},
		{"skewY", "skewY(15)", geometry.SkewY(15)},

		// matrix
		{"matrix", "matrix(1 0 0 1 5 7)", geometry.NewAffineTransform(1, 0, 0, 1, 5, 7)},
		{"matrix-commas", "matrix(2,0,0,3,1,2)", geometry.NewAffineTransform(2, 0, 0, 3, 1, 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseSVGTransform(tc.in)
			if !nearAffine(got, tc.want) {
				t.Errorf("ParseSVGTransform(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseSVGTransform_Composition(t *testing.T) {
	// translate(10,0) rotate(45) — applied to a point p means
	// translate(rotate(p)). At p=(1,0), rotate(45) gives
	// (cos45, sin45) ≈ (0.7071, 0.7071); translate(10,0) gives
	// (10.7071, 0.7071).
	got := ParseSVGTransform("translate(10,0) rotate(45)")
	want := geometry.Translate(10, 0).Compose(geometry.Rotate(45))
	if !nearAffine(got, want) {
		t.Errorf("composition: got %+v want %+v", got, want)
	}
	p := got.MapPoint(geometry.PointF{X: 1, Y: 0})
	wantX := 10 + math.Cos(math.Pi/4)
	wantY := math.Sin(math.Pi / 4)
	if math.Abs(p.X-wantX) > 1e-9 || math.Abs(p.Y-wantY) > 1e-9 {
		t.Errorf("composition mapped (1,0) to (%v,%v), want (%v,%v)", p.X, p.Y, wantX, wantY)
	}
}

func TestParseSVGTransform_Empty(t *testing.T) {
	cases := []string{"", "   ", "\t\n"}
	for _, in := range cases {
		got := ParseSVGTransform(in)
		if !got.IsIdentity() {
			t.Errorf("ParseSVGTransform(%q) = %+v, want identity", in, got)
		}
	}
}

func TestParseSVGTransform_BadInputStopsEarly(t *testing.T) {
	// Good translate followed by an unknown function — the parser
	// should keep the translate and abandon the rest.
	got := ParseSVGTransform("translate(5,5) badness(1,2)")
	want := geometry.Translate(5, 5)
	if !nearAffine(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseSVGTransform_ScientificAndSigned(t *testing.T) {
	got := ParseSVGTransform("translate(1.5e1,-2E0)")
	want := geometry.Translate(15, -2)
	if !nearAffine(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseCSSTransformForSVG_Nil(t *testing.T) {
	if !ParseCSSTransformForSVG(nil).IsIdentity() {
		t.Errorf("nil style should give identity")
	}
}

func TestParseCSSTransformForSVG_Translate(t *testing.T) {
	s := css.NewStyle()
	s.Set("transform", "translate(10px, 20px)")
	got := ParseCSSTransformForSVG(s)
	want := geometry.Translate(10, 20)
	if !nearAffine(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseCSSTransformForSVG_Composition(t *testing.T) {
	s := css.NewStyle()
	s.Set("transform", "translate(10px, 0px) rotate(45deg)")
	got := ParseCSSTransformForSVG(s)
	want := geometry.Translate(10, 0).Compose(geometry.Rotate(45))
	if !nearAffine(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
