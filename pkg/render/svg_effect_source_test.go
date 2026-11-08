package render

import (
	"image"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// effectSourceUnder makes the effect buffer at device x 50..150 of a paint
// context whose user space maps to device by dfu, drawn under an ancestor
// translate of 40px (the masked-shape case), with the given region clip.
func effectSourceUnder(dfu geometry.AffineTransform, regionClip *image.Rectangle) svgPaintContext {
	r := NewRendererForImage(image.NewRGBA(image.Rect(0, 0, 200, 100)))
	pctx := &svgPaintContext{
		dc:              r.dc,
		contentToDevice: dfu,
		viewport:        geometry.NewRectF(0, 0, 100, 100),
		DeviceFromUser:  dfu,
		Renderer:        r,
		regionClip:      regionClip,
	}
	ancestor := geometry.Translate(40, 0)
	_, child := newSVGEffectSource(pctx, image.Rect(50, 0, 150, 100), ancestor.Compose(dfu).ToTransform(), ancestor.ToTransform())
	return child
}

// An effect buffer painted through the draw context's whole matrix (a
// masked shape's) carries the viewport clip in that matrix's frame: with the
// ancestor translate on top of the <svg>'s DeviceFromUser (10px), the 100px
// viewport at user (0, 0) is at device x 50..150, buffer x 0..100.
func TestNewSVGEffectSource_ViewportClipUnderAncestorTransform(t *testing.T) {
	child := effectSourceUnder(geometry.Translate(10, 0), nil)
	if got, want := child.viewportDeviceClip(), image.Rect(0, 0, 100, 100); got != want {
		t.Errorf("buffer viewport clip %v, want %v", got, want)
	}
}

// The ancestor matrix is given, not recovered by inverting DeviceFromUser,
// so a singular DeviceFromUser (a scale(0, 1)) still moves the clips: the
// region clip at device x 10..60 is at x 50..100 under the ancestor, buffer
// x 0..50.
func TestNewSVGEffectSource_SingularDeviceFromUser(t *testing.T) {
	clip := image.Rect(10, 0, 60, 100)
	child := effectSourceUnder(geometry.Scale(0, 1), &clip)
	if got, want := *child.regionClip, image.Rect(0, 0, 50, 100); got != want {
		t.Errorf("buffer region clip %v, want %v", got, want)
	}
}

// A filter chain on a paint context without a renderer builds nothing
// rather than dereferencing one.
func TestBuildSVGFilterChain_NilRenderer(t *testing.T) {
	pctx := &svgPaintContext{viewport: geometry.NewRectF(0, 0, 100, 100), DeviceFromUser: geometry.Identity()}
	style := css.NewStyle()
	style.Set("filter", "url(a.svg#f) blur(1px)")
	ops := style.GetFilter()
	if _, _, ok := buildSVGFilterChain(pctx, ops, geometry.NewRectF(0, 0, 10, 10), image.Rectangle{}, geometry.PointF{}, css.Color{}); ok {
		t.Error("built a chain without a renderer")
	}
}

// The paint pass's external SVG cache builds each external filter once:
// the same document, fragment and viewport give the same filter.
func TestExternalSVGCache_BuildsFilterOnce(t *testing.T) {
	fetches := 0
	c := newExternalSVGCache(func(string) ([]byte, error) {
		fetches++
		return []byte(`<svg xmlns="http://www.w3.org/2000/svg"><filter id="f"><feFlood/></filter></svg>`), nil
	})
	lc := svg.NewSVGLengthContext(geometry.SizeF{Width: 100, Height: 100})
	a, b := c.filter("a.svg", "f", lc), c.filter("a.svg", "f", lc)
	if a == nil || a != b || fetches != 1 {
		t.Errorf("filters %p, %p after %d fetches, want one filter from one fetch", a, b, fetches)
	}
}
