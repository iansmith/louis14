package render_test

import (
	"fmt"
	"image/color"
	"testing"

	"louis14/pkg/render"
)

// LOU-905: overflow-clip-margin applies only when the box clips by
// overflow:clip on both axes (or paint containment). A replaced element
// uses clip for every overflow value except visible, so for it the margin
// applies when neither axis is visible (LayoutObject::ShouldApplyOverflowClipMargin,
// core/layout/layout_object.h:588-617 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestOverflowClipMargin_AppliesPerBlinkRule(t *testing.T) {
	// A green 100x100 image drawn at its natural size, centred in a 50x50
	// <img>: it overflows the content box by 25px on every side.
	img := `<img src="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='100' height='100'><rect width='100' height='100' fill='green'/></svg>" style="display:block; margin:30px; width:50px; height:50px; object-fit:none; %s">`
	for _, tc := range []struct {
		name, html string
		x, y       int
		want       color.RGBA
	}{
		// Non-replaced, clip on one axis only: no margin, so the child
		// is clipped at the padding edge.
		{"one-axis clip has no margin",
			`<div style="width:50px; height:50px; overflow-x:clip; overflow-y:visible; overflow-clip-margin:20px"><div style="width:60px; height:10px; background:green"></div></div>`,
			55, 5, canvasWhite},
		// Control: clip on both axes applies the margin.
		{"two-axis clip has the margin",
			`<div style="width:50px; height:50px; overflow:clip; overflow-clip-margin:20px"><div style="width:60px; height:10px; background:green"></div></div>`,
			55, 5, canvasGreen},
		// Replaced, overflow:hidden: used value clip on both axes, so the
		// margin applies and the image shows 10px past the box.
		{"img overflow hidden has the margin",
			fmt.Sprintf(img, "overflow:hidden; overflow-clip-margin:10px"), 25, 55, canvasGreen},
		{"img overflow visible on one axis has no margin",
			fmt.Sprintf(img, "overflow-x:visible; overflow-y:clip; overflow-clip-margin:10px"), 55, 25, canvasWhite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+tc.html, 200, 200)
			wantPixel(t, got, tc.x, tc.y, tc.want, tc.name)
		})
	}
}
