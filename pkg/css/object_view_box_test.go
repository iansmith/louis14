package css

import (
	"testing"

	"louis14/pkg/geometry"
)

// object-view-box parse + compute (ObjectViewBox::ParseSingleValue,
// longhands_custom.cc:7339-7357; ConvertObjectViewBox → BasicShapeForValue,
// basic_shape_functions.cc:698-760; BasicShapeInset::GetPath, basic_shapes.cc:
// 349-380 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), checked through the
// bounding rect each value computes to in a 100×200 natural box.
func TestObjectViewBox_ComputedInset(t *testing.T) {
	box := geometry.NewRectF(0, 0, 100, 200)
	for _, tc := range []struct {
		value string
		want  geometry.RectF
	}{
		{"inset(10px)", geometry.NewRectF(10, 10, 80, 180)},
		{"inset(50px 0px 0px 0px)", geometry.NewRectF(0, 50, 100, 150)},
		{"inset(0px -50px 0px 0px)", geometry.NewRectF(0, 0, 150, 200)},
		{"inset(10% 20%)", geometry.NewRectF(20, 20, 60, 160)},
		{"inset(10px round 5px)", geometry.NewRectF(10, 10, 80, 180)},
		{"rect(50px 50px 100px 25px)", geometry.NewRectF(25, 50, 25, 50)},
		{"rect(auto auto auto auto)", geometry.NewRectF(0, 0, 100, 200)},
		{"rect(50% 100% 100% 50%)", geometry.NewRectF(50, 100, 50, 100)},
		{"xywh(25px 50px 25px 50px)", geometry.NewRectF(25, 50, 25, 50)},
		{"xywh(50% 50% 50% 50%)", geometry.NewRectF(50, 100, 50, 100)},
		{"inset(150px 0 0 0)", geometry.NewRectF(0, 150, 100, 50)},
		{"inset(0 120px 0 0)", geometry.NewRectF(0, 0, 0, 200)}, // width clamps at 0
	} {
		s := NewStyle()
		s.Set("object-view-box", tc.value)
		vb := s.ObjectViewBox()
		if vb == nil {
			t.Errorf("%s: nil view box", tc.value)
			continue
		}
		if got := vb.PathBoundingRect(box); got != tc.want {
			t.Errorf("%s: bounding rect %+v, want %+v", tc.value, got, tc.want)
		}
	}
	if s := NewStyle(); s.ObjectViewBox() != nil {
		t.Error("initial object-view-box should be none")
	}
}

func TestObjectViewBox_InvalidKeepsEarlier(t *testing.T) {
	for _, bad := range []string{"circle(10px)", "inset()", "rect(1px 2px 3px)", "xywh(0 0 -1px 2px)", "inset(1px round)", "foo"} {
		ss, err := ParseStylesheet("img { object-view-box: inset(1px); object-view-box: "+bad+"; }", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := ss.Rules[0].Declarations["object-view-box"]; got != "inset(1px)" {
			t.Errorf("object-view-box: %s should be invalid; got %q", bad, got)
		}
	}
	ss, _ := ParseStylesheet("img { object-view-box: inset(1px); object-view-box: none; }", nil)
	if got := ss.Rules[0].Declarations["object-view-box"]; got != "none" {
		t.Errorf("none should be valid; got %q", got)
	}
}
