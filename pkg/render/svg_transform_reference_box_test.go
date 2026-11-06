package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// TestSVGTransform_PercentagesResolveAgainstViewBox: an SVG element's CSS
// transform resolves percentages against its reference box, which for the
// default transform-box (view-box) is the nearest SVG viewport
// (TransformHelper::ComputeReferenceBox, core/layout/svg/transform_helper.cc:
// 93-117 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). translate(50%, 50%)
// in a 200×200 viewport moves the group by 100px each way.
func TestSVGTransform_PercentagesResolveAgainstViewBox(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0"><svg width="200" height="200" style="position:absolute;top:0;left:0">` +
		`<g style="transform:translate(50%, 50%)"><rect x="-100" y="-100" width="100" height="100" style="fill:lime"></rect></g>` +
		`</svg></body></html>`
	img := render.RenderPage(t, src, 300, 300)
	if c := img.RGBAAt(50, 50); c.G < 200 || c.R > 60 {
		t.Errorf("pixel (50,50) = %v, want the group moved by half the viewport", c)
	}
}

// TestSVGTransform_OriginDefaultsToViewBoxOrigin: the UA stylesheet gives
// SVG elements transform-origin: 0 0, so a rotation pivots on the
// viewport origin, while an author transform-origin resolves against the
// reference box (TransformHelper::ComputeTransformOrigin, :173-184).
func TestSVGTransform_OriginDefaultsToViewBoxOrigin(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0"><svg width="200" height="200" style="position:absolute;top:0;left:0">` +
		`<rect x="0" y="0" width="100" height="100" style="fill:lime;transform:rotate(180deg);transform-origin:50% 50%"></rect>` +
		`</svg></body></html>`
	img := render.RenderPage(t, src, 300, 300)
	// rotate(180deg) about the viewport center (100,100) moves the
	// rect to 100..200.
	if c := img.RGBAAt(150, 150); c.G < 200 || c.R > 60 {
		t.Errorf("pixel (150,150) = %v, want the rect rotated about the viewport center", c)
	}
}
