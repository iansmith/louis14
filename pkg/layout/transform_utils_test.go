package layout

import (
	"testing"

	"louis14/pkg/geometry"
)

// ComputeReferenceBox is the border box for border-box (and the view-box
// and stroke-box it stands in for) and the content box for content-box
// (and fill-box), clamped to a non-negative size (ComputeReferenceBoxInternal,
// core/layout/transform_utils.cc:17-34 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestComputeReferenceBox(t *testing.T) {
	data := &PhysicalBoxData{
		Border:  PhysicalEdges{Top: 5, Left: 50},
		Padding: PhysicalEdges{Right: 10, Bottom: 20, Left: 25},
	}
	for _, tc := range []struct {
		transformBox string
		want         geometry.RectF
	}{
		{"", geometry.NewRectF(0, 0, 200, 100)},
		{"view-box", geometry.NewRectF(0, 0, 200, 100)},
		{"border-box", geometry.NewRectF(0, 0, 200, 100)},
		{"stroke-box", geometry.NewRectF(0, 0, 200, 100)},
		{"content-box", geometry.NewRectF(75, 5, 115, 75)},
		{"fill-box", geometry.NewRectF(75, 5, 115, 75)},
	} {
		style := makeStyle()
		if tc.transformBox != "" {
			style = makeStyle("transform-box", tc.transformBox)
		}
		f := &PhysicalFragment{Size: geometry.NewPhysicalSize(200, 100), Style: style, BoxData: data}
		if got := ComputeReferenceBox(f); got != tc.want {
			t.Errorf("transform-box %q: reference box %v, want %v", tc.transformBox, got, tc.want)
		}
	}
}

// A content box thinner than borders plus padding clamps to zero size.
func TestComputeReferenceBox_ContentBoxClampsNegative(t *testing.T) {
	style := makeStyle("transform-box", "content-box")
	f := &PhysicalFragment{
		Size:    geometry.NewPhysicalSize(40, 10),
		Style:   style,
		BoxData: &PhysicalBoxData{Border: PhysicalEdges{Left: 30, Right: 30, Top: 8, Bottom: 8}},
	}
	if got, want := ComputeReferenceBox(f), geometry.NewRectF(30, 8, 0, 0); got != want {
		t.Errorf("reference box %v, want %v", got, want)
	}
}

// The overflow path maps a content-box child through its content-box
// transform-origin: rotate(90deg) about (-50px, 0) of the content box,
// which starts after a 50px left border (WPT
// css-transforms/transform-box/cssbox-content-box-001's geometry).
func TestTransformForChildFragment_ContentBoxOrigin(t *testing.T) {
	style := makeStyle("transform", "rotate(90deg)", "transform-origin", "-50px 0", "transform-box", "content-box")
	child := &PhysicalFragment{
		Size:    geometry.NewPhysicalSize(200, 200),
		Style:   style,
		BoxData: &PhysicalBoxData{Border: PhysicalEdges{Left: 50}},
	}
	m, ok := transformForChildFragment(child)
	if !ok {
		t.Fatal("no transform")
	}
	// The origin is content-box x 50 plus -50px: border-box (0, 0), so
	// (200, 0) rotates to (0, 200). The border-box origin (-50, 0) would
	// give (-50, 250).
	got := m.MapPoint(geometry.PointF{X: 200, Y: 0})
	if !approxEq(got.X, 0) || !approxEq(got.Y, 200) {
		t.Errorf("(200, 0) mapped to %v, want (0, 200)", got)
	}
}
