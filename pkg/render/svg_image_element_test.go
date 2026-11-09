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
// filter floods around its image. They are the computed CSS properties the
// attributes map to (SVGImageElement's x_/y_/width_/height_ are bound to
// CSSPropertyID::kX/kY/kWidth/kHeight, svg_image_element.cc:41-64), so CSS
// sets them too.
func TestSVGImageElement_BoxPropagatesToGroup(t *testing.T) {
	for _, size := range []string{`width="40" height="20"`, `style="width:40px;height:20px"`} {
		img := renderSVGImagePage(t, `<g filter="url(#obb)"><image x="10" y="10" `+size+`/></g>`)
		wantPixel(t, img, 30, 20, canvasGreen, size+": the group's flood over the image's box")
		wantPixel(t, img, 5, 5, canvasWhite, size+": outside the group's region")
		wantPixel(t, img, 90, 90, canvasWhite, size+": outside the group's region")
	}
}

// A filtered element's output is clipped and masked after the filter: the
// clip and mask recorders nest outside the filter's in ScopedSVGPaintState
// (core/paint/scoped_svg_paint_state.cc @ a9f50e52), for an <image> and a
// <g> as for a shape. #c and #m keep only (0,0)-(40,40).
func TestSVGImageElement_FilteredOutputClippedAndMasked(t *testing.T) {
	const clipMaskDefs = `<defs><clipPath id="c"><rect width="40" height="40"/></clipPath>
<mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100"><rect width="40" height="40" fill="white"/></mask></defs>`
	for _, el := range []string{
		`<image href="missing.png" width="10" height="10" filter="url(#flood)" clip-path="url(#c)"/>`,
		`<image href="missing.png" width="10" height="10" filter="url(#flood)" mask="url(#m)"/>`,
		`<g filter="url(#flood)" clip-path="url(#c)"><rect width="10" height="10" fill="blue"/></g>`,
	} {
		img := renderSVGImagePage(t, clipMaskDefs+el)
		wantPixel(t, img, 20, 20, canvasRed, el+": the flood inside the clip or mask")
		wantPixel(t, img, 50, 50, canvasWhite, el+": the flood clipped or masked away")
	}
}

// feImage reads its href as SVGAnimatedHref does: a specified href wins even
// when empty, so href="" with xlink:href="#src" has no image
// (svg_animated_href.cc:71-81 @ a9f50e52).
func TestFEImage_EmptyHrefWinsOverXlinkHref(t *testing.T) {
	img := renderSVGImagePage(t, `<defs><rect id="src" width="50" height="50" fill="green"/>
<filter id="fi" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100"><feImage href="" xlink:href="#src"/></filter></defs>
<rect width="100" height="100" fill="blue" filter="url(#fi)"/>`)
	wantPixel(t, img, 20, 20, canvasWhite, "no image: href is specified and empty")
}
