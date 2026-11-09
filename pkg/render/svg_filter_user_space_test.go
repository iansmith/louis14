package render_test

import (
	"image"
	"testing"

	"louis14/pkg/render"
)

// An SVG element's filter is built in its local user space, inside its own
// transform: UpdateTransformForSVGChild runs before UpdateFilter, whose
// local_transform_space is the current transform
// (FragmentPaintPropertyTreeBuilder, core/paint/paint_property_tree_builder.cc:
// 4066-4082, 2447-2453 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Its
// parameters are in user units, and Skia maps them into the layer the graph
// runs in (skif::Mapping::paramToLayer, src/core/SkImageFilterTypes.h @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e). LOU-774.

// renderUserSpacePage renders a 120×120 page holding an <svg> (styled
// svgStyle) at (0, 0) with body and an feOffset filter #off (dx=10 user
// units, userSpaceOnUse region).
func renderUserSpacePage(t *testing.T, svgStyle, body string) *image.RGBA {
	t.Helper()
	return render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}svg{display:block}</style>
<svg width="120" height="120" style="`+svgStyle+`"><defs>
<filter id="off" filterUnits="userSpaceOnUse" x="-100" y="-100" width="300" height="300">
<feOffset dx="10" dy="0"/></filter></defs>`+body+`</svg>`, 120, 120)
}

// TestSVGFilter_ShapeOwnTransform: a filtered shape paints inside its own
// transform (svg-mutation-object-transform-changed, statically).
func TestSVGFilter_ShapeOwnTransform(t *testing.T) {
	img := renderUserSpacePage(t, "", `<rect width="100" height="100" fill="red"/>
<rect transform="translate(50, 0)" width="50" height="100" fill="green" filter="drop-shadow(-50px 0 green)"/>`)
	wantPixel(t, img, 25, 50, canvasGreen, "the shadow, cast from the translated rect")
	wantPixel(t, img, 75, 50, canvasGreen, "the rect, translated by its transform attribute")
}

// TestSVGFilter_UserSpaceOffsetScales: a userSpaceOnUse feOffset under a
// scale(2,1) moves 20 device pixels, not 10.
func TestSVGFilter_UserSpaceOffsetScales(t *testing.T) {
	img := renderUserSpacePage(t, "",
		`<g transform="scale(2,1)"><rect width="20" height="20" fill="green" filter="url(#off)"/></g>`)
	wantPixel(t, img, 10, 10, canvasWhite, "the rect moved 20px right, off its first 20 columns")
	wantPixel(t, img, 50, 10, canvasGreen, "the rect spans 20..60 after a 10-unit offset at scale 2")
}

// TestSVGFilter_UserSpaceOffsetRotates: under rotate(90) an x offset moves
// along device y.
func TestSVGFilter_UserSpaceOffsetRotates(t *testing.T) {
	img := renderUserSpacePage(t, "",
		`<g transform="translate(60,0) rotate(90)"><rect width="20" height="20" fill="green" filter="url(#off)"/></g>`)
	wantPixel(t, img, 50, 5, canvasWhite, "the offset moved the rect down, off rows 0..10")
	wantPixel(t, img, 50, 25, canvasGreen, "the rect spans rows 10..30")
	wantPixel(t, img, 65, 15, canvasWhite, "no offset along device x")
}

// TestSVGFilter_ShorthandScales: drop-shadow lengths are user units too.
func TestSVGFilter_ShorthandScales(t *testing.T) {
	img := renderUserSpacePage(t, "",
		`<g transform="scale(2,1)"><rect width="20" height="20" fill="blue" filter="drop-shadow(10px 0 green)"/></g>`)
	wantPixel(t, img, 50, 10, canvasGreen, "the shadow spans 20..60 at scale 2")
}

// TestSVGFilter_ContainerUserSpaceOffsetScales: a filtered <g> builds its
// filter inside its own transform as a shape does.
func TestSVGFilter_ContainerUserSpaceOffsetScales(t *testing.T) {
	img := renderUserSpacePage(t, "",
		`<g transform="scale(2,1)" filter="url(#off)"><rect width="20" height="20" fill="green"/></g>`)
	wantPixel(t, img, 10, 10, canvasWhite, "the group moved 20px right")
	wantPixel(t, img, 50, 10, canvasGreen, "the group spans 20..60")
}

// TestSVGFilter_AncestorCSSTransform: a filtered shape in a transformed
// inline <svg> paints under that transform (LOU-788).
func TestSVGFilter_AncestorCSSTransform(t *testing.T) {
	img := renderUserSpacePage(t, "transform:scale(2);transform-origin:0 0",
		`<rect width="20" height="20" fill="green" filter="url(#off)"/>`)
	wantPixel(t, img, 10, 10, canvasWhite, "the rect moved 20px right")
	wantPixel(t, img, 50, 30, canvasGreen, "the rect spans 20..60 × 0..40 under scale(2)")
}
