package render

import (
	"image"
	"testing"

	"louis14/pkg/geometry"
)

// An effect buffer painted through the draw context's whole matrix (a
// masked shape's) carries the viewport clip in that matrix's frame: with an
// ancestor translate of 40px on top of the <svg>'s DeviceFromUser, the
// 100px viewport at user (0, 0) is at device x 50..150, buffer x 0..100 for
// a buffer at device x 50.
func TestNewSVGEffectSource_ViewportClipUnderAncestorTransform(t *testing.T) {
	r := NewRendererForImage(image.NewRGBA(image.Rect(0, 0, 200, 100)))
	dfu := geometry.Translate(10, 0)
	pctx := &svgPaintContext{
		dc:              r.dc,
		contentToDevice: dfu,
		viewport:        geometry.NewRectF(0, 0, 100, 100),
		DeviceFromUser:  dfu,
		Renderer:        r,
	}
	toDevice := geometry.Translate(40, 0).Compose(dfu).ToTransform()
	_, child := newSVGEffectSource(pctx, image.Rect(50, 0, 150, 100), toDevice)
	if got, want := child.viewportDeviceClip(), image.Rect(0, 0, 100, 100); got != want {
		t.Errorf("buffer viewport clip %v, want %v", got, want)
	}
}
