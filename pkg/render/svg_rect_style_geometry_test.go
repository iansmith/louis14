package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// LOU-877: an SVG <rect> takes its x, y, width and height from its computed
// style (LayoutSVGRect::UpdateShapeFromElement, core/layout/svg/
// layout_svg_rect.cc:70-95 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), to
// which the attributes map as presentation attributes (svg_rect_element.cc:
// 46-57, :81-95). So CSS sizes a rect with no attributes, and author CSS
// beats the attributes.
func TestSVGRect_GeometryFromStyle(t *testing.T) {
	for _, tc := range []struct{ name, rect string }{
		{"css only", `<rect style="x:20px; y:10px; width:40px; height:30px" fill="green"/>`},
		{"css beats attributes", `<rect x="0" y="0" width="5" height="5" style="x:20px; y:10px; width:40px; height:30px" fill="green"/>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+
				`<svg width="100" height="100" style="display:block">`+tc.rect+`</svg>`, 100, 100)
			wantPixel(t, img, 40, 25, canvasGreen, "inside the rect")
			wantPixel(t, img, 2, 2, canvasWhite, "above-left of the rect")
			wantPixel(t, img, 70, 25, canvasWhite, "right of the rect")
		})
	}
}
