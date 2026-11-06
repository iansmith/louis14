package render_test

import "testing"

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
