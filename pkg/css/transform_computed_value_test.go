package css

import (
	"testing"

	"louis14/pkg/geometry"
)

// The transform and transform-origin properties compute relative lengths to
// absolute ones, and `inherit` copies that computed value: Blink converts both
// at style-build time (StyleBuilderConverter::ConvertTransformOperations /
// ConvertTransformOrigin, core/css/resolver/style_builder_converter.cc:3269,
// :3276 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b, via
// TransformBuilder::CreateTransformOperations) and the inherit path copies
// ComputedStyle::Transform(). WPT css-transforms/transform-inherit-001,
// transform-inherit-origin-001, transform-propagate-inherit-boolean-001.

func TestTransformComputedValue_EmResolvesPerElement(t *testing.T) {
	styles := cascadeHTML(t, `<!DOCTYPE html><style>
		p { font-size: 20px; transform: translateX(4em); }
		#b { font-size: 40px; }
	</style><p id=a>One</p><p id=b>Two</p>`, nil)
	for _, tc := range []struct {
		id   string
		want float64
	}{{"a", 80}, {"b", 160}} {
		if x, _ := transformTranslation(styleByID(t, styles, tc.id)); x != tc.want {
			t.Errorf("#%s transform translates x by %v, want %vpx", tc.id, x, tc.want)
		}
	}
}

func TestTransformComputedValue_InheritCopiesAbsoluteLengths(t *testing.T) {
	styles := cascadeHTML(t, `<!DOCTYPE html><style>
		#p { font-size: 25px; transform: translate(1em, 1em); }
		#c { font-size: 100px; transform: inherit; }
	</style><div id=p><div id=c></div></div>`, nil)
	if x, y := transformTranslation(styleByID(t, styles, "c")); x != 25 || y != 25 {
		t.Errorf("inherited transform translates by (%v, %v), want (25, 25)", x, y)
	}
}

func TestTransformComputedValue_InheritTransformOrigin(t *testing.T) {
	styles := cascadeHTML(t, `<!DOCTYPE html><style>
		#p { font-size: 10px; transform-origin: 5em 10em; }
		#c { font-size: 20px; transform-origin: inherit; }
	</style><div id=p><div id=c></div></div>`, nil)
	x, y := styleByID(t, styles, "c").ResolveTransformOriginPx(100, 100)
	if x != 50 || y != 100 {
		t.Errorf("inherited transform-origin = (%v, %v), want (50, 100)", x, y)
	}
}

// transformTranslation returns the translation of s's transform list for a
// 100×100 box, without transform-origin.
func transformTranslation(s *Style) (float64, float64) {
	var m geometry.Transform
	s.ApplyTransform(&m, geometry.NewRectF(0, 0, 100, 100), IncludeTransformOperations, ExcludeTransformOrigin, ExcludeIndependentTransformProperties)
	return m.Rc(0, 3), m.Rc(1, 3)
}

// TestCreateTransformOperations covers TransformBuilder's per-function
// conversions (transform_builder.cc:96-350 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestCreateTransformOperations(t *testing.T) {
	ctx := initialConversionData() // 16px font
	ops := CreateTransformOperations("translate(1em, 50%) translateZ(2em) rotateZ(90deg) scale(2) skewY(10deg) perspective(none) perspective(0px)", ctx)
	if len(ops.Operations) != 7 {
		t.Fatalf("got %d operations, want 7", len(ops.Operations))
	}
	tr := ops.Operations[0].(*TranslateTransformOperation)
	if tr.Type() != TransformOperationTranslate || tr.X.Fixed != 16 || tr.Y.Percent != 50 {
		t.Errorf("translate = %+v", tr)
	}
	if z := ops.Operations[1].(*TranslateTransformOperation); z.Type() != TransformOperationTranslateZ || z.Z != 32 {
		t.Errorf("translateZ = %+v", z)
	}
	if r := ops.Operations[2].(*RotateTransformOperation); r.Type() != TransformOperationRotateZ || r.Z != 1 || r.Angle != 90 {
		t.Errorf("rotateZ = %+v", r)
	}
	if s := ops.Operations[3].(*ScaleTransformOperation); s.X != 2 || s.Y != 2 || s.Z != 1 {
		t.Errorf("scale = %+v", s)
	}
	if k := ops.Operations[4].(*SkewTransformOperation); k.Type() != TransformOperationSkewY || k.AngleY != 10 {
		t.Errorf("skewY = %+v", k)
	}
	if p := ops.Operations[5].(*PerspectiveTransformOperation); p.HasPerspective {
		t.Errorf("perspective(none) = %+v", p)
	}
	if p := ops.Operations[6].(*PerspectiveTransformOperation); !p.HasPerspective || p.UsedPerspective() != 1 {
		t.Errorf("perspective(0px) used = %v, want 1", p.UsedPerspective())
	}

	// translateZ takes no percentage; unitless non-zero lengths are invalid.
	if got := CreateTransformOperations("translateZ(10%) translateX(10)", ctx); !got.IsEmpty() {
		t.Errorf("invalid functions kept: %+v", got.Operations)
	}

	// A calc() mixing % and em resolves the percentage against the box.
	calc := CreateTransformOperations("translateX(calc(50% + 1em))", ctx)
	var m geometry.Transform
	calc.Apply(geometry.SizeF{Width: 200, Height: 100}, &m)
	if m.Rc(0, 3) != 116 {
		t.Errorf("calc translate = %v, want 116", m.Rc(0, 3))
	}
}

// TestTransformComputedValue_TextChangeAfterCascade: a computed transform is
// tied to the property text it came from, so a post-cascade rewrite (the
// transition sampler writes Properties directly) is not masked by the
// cascade-time value.
func TestTransformComputedValue_TextChangeAfterCascade(t *testing.T) {
	styles := cascadeHTML(t, `<!DOCTYPE html><style>#d { transform: translateX(10px); }</style><div id=d></div>`, nil)
	s := styleByID(t, styles, "d")
	s.Properties["transform"] = "translateX(30px)"
	if x, _ := transformTranslation(s); x != 30 {
		t.Errorf("transform after text change translates x by %v, want 30", x)
	}
}
