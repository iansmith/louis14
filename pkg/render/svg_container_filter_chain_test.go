package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// LOU-785 (filter-effects worklist C2): an SVG container's CSS filter
// functions apply, as Blink builds the container's filter operations for
// shorthands and url() alike (SVGElementResourceClient::UpdateFilterData →
// FilterEffectBuilder::BuildFilterOperations, svg_resources.cc:371-422 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each page is the end state of
// an svg-mutation-group-* reftest: a 50×100 green rect and its green drop
// shadow cover the red 100×100 square.
func TestSVGContainer_ShorthandFilter(t *testing.T) {
	for _, c := range []struct{ name, group, rect string }{
		{"position", `filter="drop-shadow(-50px 0 green)"`, `x="50" width="50" height="100"`},
		{"size", `filter="drop-shadow(50px 0 green)"`, `width="50" height="100"`},
		{"transform", `transform="translate(50, 0)" filter="drop-shadow(-50px 0 green)"`, `width="50" height="100"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg><rect width="100" height="100" fill="red"/>
<g `+c.group+`><rect `+c.rect+` fill="green"/></g></svg>`, 120, 120)
			for _, x := range []int{25, 75} {
				sampleColorClose(t, img, x, 50, 0, 128, 0, 2, "the rect and its shadow")
			}
		})
	}
}

// A container's url() filter region is resolved in the container's own
// transformed frame, where its children paint: under translate(50, 0) the
// group's rect at user x 0..50 is at device x 50..100, inside the region.
func TestSVGContainer_URLFilterUnderGroupTransform(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg><filter id="f" color-interpolation-filters="sRGB"><feOffset dx="0" dy="0"/></filter>
<rect x="50" width="50" height="100" fill="red"/>
<g transform="translate(50, 0)" filter="url(#f)"><rect width="50" height="100" fill="green"/></g></svg>`, 120, 120)
	sampleColorClose(t, img, 75, 50, 0, 128, 0, 2, "the translated group's rect")
}

// A url() stage of a container's chain resolves userSpaceOnUse coordinates
// in the container's user space and percentages against its <svg>'s
// viewport, as a lone url() and a shape's chain do (SVGViewportResolver,
// svg_resources.cc:375-381 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the
// flood's (0, 0, 10, 10) subregion is at the <svg>'s origin, not at the
// group's reference box.
func TestSVGContainer_ChainURLUserSpaceOnUseAnchorsAtUserOrigin(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100" color-interpolation-filters="sRGB"><feFlood flood-color="green" x="0" y="0" width="10" height="10"/></filter>
<g style="filter:url(#f) blur(0px)"><rect x="50" y="50" width="20" height="20" fill="red"/></g></svg>`, 100, 100)
	wantPixel(t, img, 5, 5, canvasGreen, "the flood at the user-space origin")
	wantPixel(t, img, 55, 55, canvasWhite, "not at the group's reference box")
}

// A filtered child inside a filtered group composites in the group's source
// buffer, so its viewport clip is translated with the buffer: with the <svg>
// at (8, 200) on the page, the child's rect still shows.
func TestSVGContainer_NestedFilterOffPageOrigin(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><div style="height:192px"></div>
<svg width="100" height="100" style="display:block">
<filter id="f" color-interpolation-filters="sRGB"><feOffset dx="0" dy="0"/></filter>
<g style="filter:opacity(1)"><rect width="100" height="100" fill="green" filter="url(#f)"/></g></svg>`, 140, 320)
	wantPixel(t, img, 58, 250, canvasGreen, "the nested filtered rect")
}

// A shape whose fill box is empty still has a stroke bounding box, and a
// container counts it (HasValidBoundingBoxForContainer is !IsShapeEmpty,
// svg_content_container.cc:88-106, in ComputeStrokeBoundingBox, :254-266 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a group's drop-shadow of a
// horizontal <line> paints the shadow.
func TestSVGContainer_ChainOnHorizontalLine(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><g style="filter:drop-shadow(0 20px 0 green)">
<line x1="10" y1="20" x2="90" y2="20" stroke="green" stroke-width="10"/></g></svg>`, 100, 100)
	wantPixel(t, img, 50, 20, canvasGreen, "the line")
	wantPixel(t, img, 50, 40, canvasGreen, "its shadow")
}

// A container's chain with a url() stage paints with an empty fill box, as
// its lone url() does (Blink installs the filter whatever the bbox):
// the userSpaceOnUse flood covers the filter region around a horizontal
// <line>.
func TestSVGContainer_ChainURLWithEmptyFillBox(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter>
<g style="filter:url(#f) blur(0px)"><line x1="10" y1="20" x2="90" y2="20" stroke="red" stroke-width="10"/></g></svg>`, 100, 100)
	wantPixel(t, img, 50, 60, canvasGreen, "the flood")
}

// A container's lone url() keeps its document part: url(other.svg#f)
// resolves in other.svg even when the page has its own #f, as an SVG
// shape's does (resolveSVGFilterValue).
func TestSVGContainer_ExternalURLFilter(t *testing.T) {
	ext := `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter></svg>`
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<filter id="f" color-interpolation-filters="sRGB"><feFlood flood-color="red"/></filter>
<g filter="url(other.svg#f)"><rect x="20" y="20" width="60" height="60" fill="blue"/></g></svg>`, 100, 100,
		render.WithExternalSVGFetcher(func(string) ([]byte, error) { return []byte(ext), nil }))
	wantPixel(t, img, 50, 50, canvasGreen, "the external filter's flood")
}
