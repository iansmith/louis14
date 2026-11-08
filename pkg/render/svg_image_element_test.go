package render_test

import (
	"image"
	"testing"

	"louis14/pkg/render"
)

// LOU-797: an SVG <image> is a LayoutSVGImage leaf.

// renderSVGImagePage renders a 120×120 page with an <svg> holding body and
// two filters: #flood floods red over a userSpaceOnUse region (0,0)-(100,100),
// #obb floods green over the default objectBoundingBox region.
func renderSVGImagePage(t *testing.T, body string) *image.RGBA {
	t.Helper()
	return render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}svg{display:block}</style>
<svg width="120" height="120"><defs>
<filter id="flood" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100"><feFlood flood-color="red"/></filter>
<filter id="obb"><feFlood flood-color="green"/></filter></defs>`+body+`</svg>`, 120, 120)
}

// An <image> with no image (no href) paints nothing, its filter included:
// SVGImagePainter::Paint returns first when the image resource has no image
// (svg_image_painter.cc:41-44 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b;
// LayoutImageResource::HasImage, layout_image_resource.h:55), as for
// svg-empty-element-with-filter-001's <image height="0">.
func TestSVGImageElement_NoImagePaintsNothing(t *testing.T) {
	for _, el := range []string{
		`<image x="10" y="10" width="10" height="0" filter="url(#flood)"/>`,
		`<image x="10" y="10" width="40" height="40" filter="url(#flood)"/>`,
	} {
		img := renderSVGImagePage(t, el)
		wantPixel(t, img, 5, 5, canvasWhite, el+": no image, no flood")
		wantPixel(t, img, 90, 90, canvasWhite, el+": no image, no flood")
	}
}

// An <image> with an image (an href, here one that does not load: the
// resource exists with an error) runs its filter even when its viewport is
// empty: the paint state and its filter are set up before PaintForeground's
// empty-viewport return (svg_image_painter.cc:47-86, 95-98 @ a9f50e52).
func TestSVGImageElement_ImageRunsItsFilter(t *testing.T) {
	img := renderSVGImagePage(t, `<image href="missing.png" x="10" y="10" width="10" height="0" filter="url(#flood)"/>`)
	wantPixel(t, img, 5, 5, canvasRed, "the userSpaceOnUse flood")
	wantPixel(t, img, 90, 90, canvasRed, "the userSpaceOnUse flood")
}

// An <image>'s box is its x, y, width and height (LayoutSVGImage::
// UpdateBoundingBox, layout_svg_image.cc:140-153 @ a9f50e52), which a
// parent's bounding box unites (HasValidBoundingBoxForContainer,
// svg_content_container.cc:102-104): a <g> under an objectBoundingBox
// filter floods around its image.
func TestSVGImageElement_BoxPropagatesToGroup(t *testing.T) {
	img := renderSVGImagePage(t, `<g filter="url(#obb)"><image x="10" y="10" width="40" height="20"/></g>`)
	wantPixel(t, img, 30, 20, canvasGreen, "the group's flood over the image's box")
	wantPixel(t, img, 5, 5, canvasWhite, "outside the group's region")
	wantPixel(t, img, 90, 90, canvasWhite, "outside the group's region")
}
