package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// Under primitiveUnits="objectBoundingBox" a length scales by the float
// reference box (Filter::ApplyHorizontalScale, filter.cc:65-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), not its pixel-snapped
// enclosing rect: a shape spanning x 0.25..20.75 (width 20.5) offset by
// dx="2" moves 41px, to 41.25..61.75. The snapped width 21 would move it to
// 42.25..62.75. (effect-reference-obb-dimensions covers the integral box.)
func TestFEOffset_ObjectBoundingBoxUnitsUseFloatReferenceBox(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="40">
<filter id="o" primitiveUnits="objectBoundingBox" x="0" y="0" width="4" height="1" color-interpolation-filters="sRGB"><feOffset dx="2" dy="0"/></filter>
<rect x="0.25" y="0" width="20.5" height="20" fill="green" filter="url(#o)"/></svg>`, 100, 40)
	assertNotWhite(t, img, 41, 10, "the shape's left edge (it starts at x 41.25)")
	wantPixel(t, img, 62, 10, canvasWhite, "past the shape's right edge at x 61.75")
}

// An SVG shape's filter reference box is its fill box, not its stroke box:
// SVGResources::ReferenceBoxForEffects defaults to GeometryBox::kFillBox,
// the ObjectBoundingBox (svg_resources.h:65-68, svg_resources.cc:53-75; the
// filter path calls it with the default at :375 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 100px-wide rect with a 20px
// stroke spans x 10..130; dx="0.1" moves it 10px (fill box), to 20..140, not
// 12px (stroke box), to 22..142.
func TestFEOffset_ObjectBoundingBoxUnitsUseFillBox(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="160" height="80">
<filter id="o" primitiveUnits="objectBoundingBox" x="-1" y="-1" width="3" height="3" color-interpolation-filters="sRGB"><feOffset dx="0.1" dy="0"/></filter>
<rect x="20" y="20" width="100" height="40" fill="green" stroke="green" stroke-width="20" filter="url(#o)"/></svg>`, 160, 80)
	wantPixel(t, img, 20, 40, canvasGreen, "the stroke's moved left edge at x 20")
	wantPixel(t, img, 140, 40, canvasWhite, "past the moved right edge at x 140")
}

// A url() + shorthand chain on a stroked shape keeps its whole stroke: the
// chain's source is the shape's paint (stroke included), not its fill-box
// reference box. The rect's stroke spans x 10..90; its left half-stroke
// (x 10..20) lies outside the fill box (Blink crops a url() stage to its
// own filter region only: FilterEffectBuilder::BuildReferenceFilter,
// filter_effect_builder.cc:514 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFilterChain_URLAndShorthandKeepsStroke(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<filter id="f" color-interpolation-filters="sRGB"><feOffset dx="0" dy="0"/></filter>
<rect x="20" y="20" width="60" height="60" fill="green" stroke="green" stroke-width="20" style="filter:url(#f) blur(1px)"/></svg>`, 100, 100)
	wantPixel(t, img, 14, 50, canvasGreen, "the stroke's outer half, outside the fill box")
}

// A shorthand chain's source extent is the shape's stroke bounding box,
// miter joins included (LayoutSVGShape::StrokeBoundingBox, layout_svg_shape.cc:
// 238-262 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the triangle's apex
// miter reaches y 8.8, above the half-width outset of its fill box (y 15).
func TestFilterChain_ShorthandKeepsMiterJoin(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<path d="M20,80 L50,20 L80,80 Z" fill="green" stroke="green" stroke-width="10" style="filter:blur(0.5px)"/></svg>`, 100, 100)
	assertNotWhite(t, img, 50, 12, "the apex miter (it reaches y 8.8)")
}

// A shorthand-only chain needs no reference box, so a shape with an empty
// fill box (a horizontal <line>) still filters its stroke: blur(2px) spreads
// it past y 45..55.
func TestFilterChain_ShorthandOnEmptyFillBox(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<line x1="10" y1="50" x2="90" y2="50" stroke="green" stroke-width="10" style="filter:blur(2px)"/></svg>`, 100, 100)
	assertNotWhite(t, img, 50, 43, "the blurred stroke past its edge at y 45")
}

// A url() stage of a chain on an SVG shape resolves userSpaceOnUse
// coordinates in the shape's user space, as a lone url() filter does: the
// flood's (0, 0, 10, 10) subregion is at the <svg>'s origin, not at the
// shape's reference box.
func TestFilterChain_URLUserSpaceOnUseAnchorsAtUserOrigin(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100" color-interpolation-filters="sRGB"><feFlood flood-color="green" x="0" y="0" width="10" height="10"/></filter>
<rect x="50" y="50" width="20" height="20" fill="red" style="filter:url(#f) blur(0px)"/></svg>`, 100, 100)
	wantPixel(t, img, 5, 5, canvasGreen, "the flood at the user-space origin")
	wantPixel(t, img, 55, 55, canvasWhite, "not at the shape's reference box")
}
