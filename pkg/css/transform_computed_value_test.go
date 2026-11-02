package css

import "testing"

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
		ops := styleByID(t, styles, tc.id).ResolvedTransformOperations(100, 100)
		if len(ops) != 1 || ops[0].Values[0] != tc.want {
			t.Errorf("#%s transform = %+v, want translate %vpx", tc.id, ops, tc.want)
		}
	}
}

func TestTransformComputedValue_InheritCopiesAbsoluteLengths(t *testing.T) {
	styles := cascadeHTML(t, `<!DOCTYPE html><style>
		#p { font-size: 25px; transform: translate(1em, 1em); }
		#c { font-size: 100px; transform: inherit; }
	</style><div id=p><div id=c></div></div>`, nil)
	ops := styleByID(t, styles, "c").ResolvedTransformOperations(100, 100)
	if len(ops) != 1 || ops[0].Values[0] != 25 || ops[0].Values[1] != 25 {
		t.Errorf("inherited transform = %+v, want translate(25px, 25px)", ops)
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
