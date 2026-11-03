package css

import "testing"

// transform-box's initial value is view-box (css_properties.json5:6596-6604
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestGetTransformBox_InitialIsViewBox(t *testing.T) {
	if got := NewStyle().GetTransformBox(); got != TransformBoxViewBox {
		t.Errorf("initial transform-box = %v, want view-box", got)
	}
}

func TestGetTransformBox_Keywords(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  TransformBox
	}{
		{"content-box", TransformBoxContentBox},
		{"border-box", TransformBoxBorderBox},
		{"fill-box", TransformBoxFillBox},
		{"stroke-box", TransformBoxStrokeBox},
		{"view-box", TransformBoxViewBox},
	} {
		s := NewStyle()
		s.Set("transform-box", tc.value)
		if got := s.GetTransformBox(); got != tc.want {
			t.Errorf("transform-box: %s gave %v, want %v", tc.value, got, tc.want)
		}
		if got := tc.want.String(); got != tc.value {
			t.Errorf("%v.String() = %q, want %q", tc.want, got, tc.value)
		}
	}
}

// UsedTransformBox maps each specified value onto the boxes the element
// has: SVG elements without a CSS layout box use fill-box for content-box
// and stroke-box for border-box; CSS boxes use content-box for fill-box and
// border-box for stroke-box and view-box (ComputedStyle::UsedTransformBox,
// computed_style.cc:1377-1420 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestUsedTransformBox(t *testing.T) {
	for _, tc := range []struct {
		value          string
		layoutBox, svg TransformBox
	}{
		{"content-box", TransformBoxContentBox, TransformBoxFillBox},
		{"border-box", TransformBoxBorderBox, TransformBoxStrokeBox},
		{"fill-box", TransformBoxContentBox, TransformBoxFillBox},
		{"stroke-box", TransformBoxBorderBox, TransformBoxStrokeBox},
		{"view-box", TransformBoxBorderBox, TransformBoxViewBox},
	} {
		s := NewStyle()
		s.Set("transform-box", tc.value)
		if got := s.UsedTransformBox(TransformBoxContextLayoutBox); got != tc.layoutBox {
			t.Errorf("%s on a CSS box: used %v, want %v", tc.value, got, tc.layoutBox)
		}
		if got := s.UsedTransformBox(TransformBoxContextSvg); got != tc.svg {
			t.Errorf("%s on an SVG element: used %v, want %v", tc.value, got, tc.svg)
		}
	}
}

// CSS keywords are ASCII case-insensitive, and a value can carry stray
// whitespace (e.g. after var() substitution).
func TestGetTransformBox_CaseInsensitiveTrimmed(t *testing.T) {
	for _, v := range []string{"Fill-Box", "FILL-BOX", " fill-box ", "\tfill-box\n"} {
		s := NewStyle()
		s.Set("transform-box", v)
		if got := s.GetTransformBox(); got != TransformBoxFillBox {
			t.Errorf("transform-box: %q gave %v, want fill-box", v, got)
		}
	}
}
