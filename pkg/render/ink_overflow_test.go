package render_test

import (
	"image/color"
	"testing"

	"louis14/pkg/render"
)

// A filter's source covers the layer's visual overflow, ink overflow
// included (PaintLayer::LocalBoundingBox, core/paint/paint_layer.cc:
// 2172-2179 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A box's self ink
// overflow is its border box grown by its BoxDecorationOutsets (box-shadow
// and border-image outsets) and its outline inflated by OutlineOutsetExtent
// (PhysicalBoxFragment::ComputeSelfInkOverflow, core/layout/
// physical_box_fragment.cc:1109-1180), and a child's ink overflow joins its
// parent's. So with blur(0), which moves no pixel, each of these paints
// outside the filtered box as it would without the filter. Pixels from
// headless Chrome 157 2026-11-13. The filtered element's own outline is
// TestFocusRing_FilteredInline's.
func TestInkOverflow_FilterSourceCoversDescendantInk(t *testing.T) {
	blue := color.RGBA{0, 0, 255, 255}
	img := render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="position:absolute;left:20px;top:20px;font:20px/20px Ahem;color:transparent;filter:blur(0)">X<a href="#" style="outline:auto 1px blue">XX</a></div>`+
		`<div style="position:absolute;left:20px;top:80px;width:40px;height:20px;filter:blur(0)"><div style="width:40px;height:20px;box-shadow:0 -10px 0 blue"></div></div>`+
		`<div style="position:absolute;left:20px;top:140px;width:40px;height:20px;filter:blur(0)"><div style="width:40px;height:20px;border:2px solid;border-image:linear-gradient(blue,blue) 1;border-image-outset:8px;box-sizing:border-box"></div></div>`+
		`<div style="position:absolute;left:100px;top:80px;width:40px;height:20px;filter:blur(0);box-shadow:0 -10px 0 blue"></div>`+
		`</body></html>`, 200, 200)
	assertPixel(t, img, 60, 19, blue, "a child anchor's focus ring, above the filtered div")
	assertPixel(t, img, 40, 75, blue, "a child's box-shadow, above the filtered div")
	assertPixel(t, img, 40, 133, blue, "a child's border-image outset, above the filtered div")
	assertPixel(t, img, 120, 75, blue, "the filtered div's own box-shadow, above it")
	assertPixel(t, img, 40, 85, white, "guard: no shadow under the child's own box")
}
