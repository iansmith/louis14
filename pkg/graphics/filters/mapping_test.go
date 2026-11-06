package filters

import (
	"math"
	"testing"

	"louis14/pkg/geometry"
)

func TestCTMCapability(t *testing.T) {
	src := NewSourceGraphic(InterpolationSpaceSRGB)
	color := NewFEColorMatrix(src, InterpolationSpaceSRGB, ColorMatrixTypeSaturate, []float64{0})
	blur := NewFEGaussianBlur(src, InterpolationSpaceSRGB, 2, 2)
	cases := []struct {
		name string
		e    FilterEffect
		want MatrixCapability
	}{
		{"source", src, MatrixCapabilityComplex},
		{"color matrix", color, MatrixCapabilityComplex},
		{"blur", blur, MatrixCapabilityScaleTranslate},
		// The minimum over the graph: a color matrix fed by a blur.
		{"color over blur", NewFEColorMatrix(blur, InterpolationSpaceSRGB, ColorMatrixTypeSaturate, []float64{0}), MatrixCapabilityScaleTranslate},
		{"offset", NewFEOffset(color, InterpolationSpaceSRGB, 1, 1), MatrixCapabilityScaleTranslate},
	}
	for _, c := range cases {
		if got := CTMCapability(c.e); got != c.want {
			t.Errorf("%s: CTMCapability = %v, want %v", c.name, got, c.want)
		}
	}
}

func nearTransform(a, b geometry.Transform) bool {
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			if math.Abs(a.Rc(r, c)-b.Rc(r, c)) > 1e-6 { // gfx::Transform::Scale takes float factors
				return false
			}
		}
	}
	return true
}

func TestMappingDecomposeCTM(t *testing.T) {
	scaleTranslate := geometry.MakeTranslation(10, 20)
	scaleTranslate.Scale(2, 3)

	rotate := geometry.MakeTranslation(5, 7)
	rotate.Rotate(30)
	rotate.Scale(2, 4)

	var perspective geometry.Transform
	perspective.ApplyPerspectiveDepth(500)
	perspective.RotateAboutYAxis(40)

	p := geometry.PointF{X: 50, Y: 50}

	t.Run("scale-translate is the layer", func(t *testing.T) {
		var m Mapping
		if !m.DecomposeCTM(scaleTranslate, MatrixCapabilityScaleTranslate, p) {
			t.Fatal("DecomposeCTM failed")
		}
		if !nearTransform(m.ParamToLayer, scaleTranslate) || !m.LayerToDev.IsIdentity() {
			t.Errorf("layer = %v, remainder = %v; want layer = ctm, remainder = identity", m.ParamToLayer, m.LayerToDev)
		}
	})

	t.Run("complex graph takes any CTM as the layer", func(t *testing.T) {
		var m Mapping
		if !m.DecomposeCTM(rotate, MatrixCapabilityComplex, p) {
			t.Fatal("DecomposeCTM failed")
		}
		if !nearTransform(m.ParamToLayer, rotate) || !m.LayerToDev.IsIdentity() {
			t.Errorf("layer = %v, remainder = %v; want layer = ctm", m.ParamToLayer, m.LayerToDev)
		}
	})

	t.Run("translate-only graph applies the whole CTM after filtering", func(t *testing.T) {
		var m Mapping
		if !m.DecomposeCTM(rotate, MatrixCapabilityTranslate, p) {
			t.Fatal("DecomposeCTM failed")
		}
		if !m.ParamToLayer.IsIdentity() || !nearTransform(m.LayerToDev, rotate) {
			t.Errorf("layer = %v, remainder = %v; want layer = identity, remainder = ctm", m.ParamToLayer, m.LayerToDev)
		}
	})

	t.Run("rotation keeps the column scales in the layer", func(t *testing.T) {
		var m Mapping
		if !m.DecomposeCTM(rotate, MatrixCapabilityScaleTranslate, p) {
			t.Fatal("DecomposeCTM failed")
		}
		if !nearTransform(m.ParamToLayer, geometry.MakeScale(2, 4)) {
			t.Errorf("layer = %v, want scale(2, 4)", m.ParamToLayer)
		}
		if !nearTransform(m.LayerToDev.Mul(m.ParamToLayer), rotate) {
			t.Errorf("remainder ∘ layer = %v, want the CTM %v", m.LayerToDev.Mul(m.ParamToLayer), rotate)
		}
	})

	t.Run("perspective uses the area scale at the representative point", func(t *testing.T) {
		var m Mapping
		if !m.DecomposeCTM(perspective, MatrixCapabilityScaleTranslate, p) {
			t.Fatal("DecomposeCTM failed")
		}
		s := math.Sqrt(differentialAreaScale(perspective, p))
		if !nearTransform(m.ParamToLayer, geometry.MakeScale(s, s)) {
			t.Errorf("layer = %v, want uniform scale %v", m.ParamToLayer, s)
		}
		if !nearTransform(m.LayerToDev.Mul(m.ParamToLayer), perspective) {
			t.Errorf("remainder ∘ layer = %v, want the CTM %v", m.LayerToDev.Mul(m.ParamToLayer), perspective)
		}
	})
}

// differentialAreaScale of an affine matrix is |det| everywhere; under
// perspective it is the Jacobian determinant of the projected map, checked
// here against a finite difference.
func TestDifferentialAreaScale(t *testing.T) {
	affine := geometry.MakeScale(2, 3)
	affine.Rotate(25)
	if got := differentialAreaScale(affine, geometry.PointF{X: 13, Y: -4}); math.Abs(got-6) > 1e-9 {
		t.Errorf("affine area scale = %v, want 6", got)
	}

	var m geometry.Transform
	m.ApplyPerspectiveDepth(300)
	m.RotateAboutXAxis(35)
	p := geometry.PointF{X: 20, Y: 30}
	project := func(x, y float64) (float64, float64) {
		w := m.Rc(3, 0)*x + m.Rc(3, 1)*y + m.Rc(3, 3)
		return (m.Rc(0, 0)*x + m.Rc(0, 1)*y + m.Rc(0, 3)) / w,
			(m.Rc(1, 0)*x + m.Rc(1, 1)*y + m.Rc(1, 3)) / w
	}
	const h = 1e-6
	x0, y0 := project(p.X, p.Y)
	xu, yu := project(p.X+h, p.Y)
	xv, yv := project(p.X, p.Y+h)
	want := math.Abs((xu-x0)*(yv-y0)-(xv-x0)*(yu-y0)) / (h * h)
	if got := differentialAreaScale(m, p); math.Abs(got-want) > 1e-3*want {
		t.Errorf("perspective area scale = %v, want ~%v", got, want)
	}
}

func TestMappingParamToLayer(t *testing.T) {
	m := Mapping{ParamToLayer: geometry.MakeScale(2, 3)}
	if x, y := m.ParamToLayerVector(5, -4); x != 10 || y != -12 {
		t.Errorf("ParamToLayerVector(5,-4) = (%v,%v), want (10,-12)", x, y)
	}
	if w, h := m.ParamToLayerSize(5, 4); w != 10 || h != 12 {
		t.Errorf("ParamToLayerSize(5,4) = (%v,%v), want (10,12)", w, h)
	}
	// A translation does not move vectors or sizes.
	m.ParamToLayer = geometry.MakeTranslation(7, 9)
	if x, y := m.ParamToLayerVector(5, -4); x != 5 || y != -4 {
		t.Errorf("translated ParamToLayerVector = (%v,%v), want (5,-4)", x, y)
	}
	// Sizes are axis lengths under rotation.
	m.ParamToLayer = geometry.Transform{}
	m.ParamToLayer.Rotate(90)
	if w, h := m.ParamToLayerSize(5, 4); math.Abs(w-5) > 1e-6 || math.Abs(h-4) > 1e-6 {
		t.Errorf("rotated ParamToLayerSize = (%v,%v), want (5,4)", w, h)
	}
}

// An integer translation in ParamToLayer moves into LayerToDev, so the graph
// is built once, in parameter space; a fractional one stays.
func TestMapping_FoldIntegerTranslation(t *testing.T) {
	var tr geometry.Transform
	tr.Translate(3, -4)
	m := Mapping{ParamToLayer: tr}
	m.FoldIntegerTranslation()
	if !m.ParamToLayer.IsIdentity() || m.LayerToDev != tr {
		t.Errorf("integer translation: ParamToLayer %v, LayerToDev %v; want identity and the translation", m.ParamToLayer, m.LayerToDev)
	}
	var frac geometry.Transform
	frac.Translate(0.5, 0)
	m = Mapping{ParamToLayer: frac}
	m.FoldIntegerTranslation()
	if m.ParamToLayer != frac {
		t.Errorf("fractional translation folded: ParamToLayer %v", m.ParamToLayer)
	}
}
