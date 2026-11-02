package graphics

import (
	"testing"

	"louis14/pkg/geometry"
)

// ImageOrientation (image_orientation.h:48-78, .cc:35-60 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the transform maps the stored
// image's corners onto the oriented box.
func TestImageOrientationTransformFromDefault(t *testing.T) {
	// Oriented box 2×4 (a 4×2 image rotated 90° clockwise, orientation 6):
	// stored top-left (0,0) lands top-right (2,0); stored top-right (4,0)
	// lands bottom-right (2,4).
	tr := NewImageOrientation(OriginRightTop).TransformFromDefault(2, 4)
	for _, c := range []struct{ in, want geometry.PointF }{
		{geometry.PointF{X: 0, Y: 0}, geometry.PointF{X: 2, Y: 0}},
		{geometry.PointF{X: 4, Y: 0}, geometry.PointF{X: 2, Y: 4}},
		{geometry.PointF{X: 0, Y: 2}, geometry.PointF{X: 0, Y: 0}},
	} {
		if got := tr.MapPoint(c.in); got != c.want {
			t.Errorf("orientation 6: %v → %v, want %v", c.in, got, c.want)
		}
	}
	if !NewImageOrientation(OriginLeftBottom).UsesWidthAsHeight() || NewImageOrientation(OriginBottomRight).UsesWidthAsHeight() {
		t.Error("UsesWidthAsHeight should be true exactly for 5-8")
	}
	if (ImageOrientation{}).Orientation() != OriginTopLeft || !NewImageOrientation(OriginTopLeft).TransformFromDefault(3, 3).IsIdentity() {
		t.Error("zero value / top-left should be the identity default")
	}
}
