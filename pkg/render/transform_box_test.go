package render

import (
	"math"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout"
)

// The paint transform resolves transform-origin against the box's
// reference box (PaintPropertyTreeBuilder's GetTransformOrigin over
// ComputeReferenceBox, paint_property_tree_builder.cc:1255-1270,1427 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): for content-box, the
// content box after a 50px left border. WPT
// css-transforms/transform-box/cssbox-content-box-001's geometry.
func TestLocalTransformMatrix_ContentBoxOrigin(t *testing.T) {
	style := css.NewStyle()
	style.Set("transform", "rotate(90deg)")
	style.Set("transform-origin", "-50px 0")
	style.Set("transform-box", "content-box")
	box := &layout.Box{
		Style: style, X: 308, Y: 100, Width: 200, Height: 200,
		Border: css.BoxEdge{Left: 50},
	}
	// Origin: content-box x 358 - 50 = 308, y 100. (508, 100) rotates
	// about it to (308, 300); the border-box origin (258, 100) would give
	// (258, 350).
	got := localTransformMatrix(box).MapPoint(geometry.PointF{X: 508, Y: 100})
	if math.Abs(got.X-308) > 1e-9 || math.Abs(got.Y-300) > 1e-9 {
		t.Errorf("(508, 100) mapped to %v, want (308, 300)", got)
	}
}
