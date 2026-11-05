package render_test

import (
	"fmt"
	"testing"
)

// TestFilterReferenceBox_IncludesSelfPaintingDescendants — a filter that moves
// pixels takes its reference box from the layer's bounding box including its
// self-painting descendants, mapped through their transforms: Blink's
// PaintLayer::UpdateFilterReferenceBox and
// LocalBoundingBoxIncludingSelfPaintingDescendants (core/paint/paint_layer.cc:
// 2021-2044, 2181-2249 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// The child is 50x50 scaled 2x about its centre and moved by (25,25), so it
// paints (0,0)-(100,100). The filter region is x 25%..75% of the reference box:
// (25..75) of that bounding box, where the border box alone gives (12.5..37.5).
// Geometry from WPT filter-effects/filter-region-transformed-child-001.
func TestFilterReferenceBox_IncludesSelfPaintingDescendants(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="0" height="0" style="position:absolute"><defs>` +
		`<filter id="f" x="25%" y="0%" width="50%" height="100%">` +
		`<feComponentTransfer><feFuncG type="linear" slope="0"/><feFuncB type="linear" slope="0"/></feComponentTransfer>` +
		`</filter></defs></svg>` +
		`<div style="display:inline-block;filter:url(#f)">` +
		`<div style="width:50px;height:50px;background:rgb(128,128,128);transform:translate(25px,25px) scale(2)"></div>` +
		`</div></body></html>`
	img := renderToImage(t, page, 200, 200)
	sampleColorClose(t, img, 60, 50, 128, 0, 0, 1, "inside the region from the descendants' bounds")
	sampleColorClose(t, img, 30, 25, 128, 0, 0, 1, "inside both regions")
	sampleColorClose(t, img, 15, 50, 255, 255, 255, 0, "left of the region")
	sampleColorClose(t, img, 85, 50, 255, 255, 255, 0, "right of the region")
}

// TestFilterReferenceBox_NegativelyPositionedChild — an absolutely positioned
// child above its filtered parent widens the reference box, so the default
// filter region covers it. Geometry from WPT
// filter-effects/filter-region-negative-positioned-child-001.
func TestFilterReferenceBox_NegativelyPositionedChild(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="0" height="0" style="position:absolute"><defs>` +
		`<filter id="inv" color-interpolation-filters="sRGB">` +
		`<feColorMatrix type="matrix" values="-1 0 0 0 1  0 -1 0 0 1  0 0 -1 0 1  0 0 0 1 0"/>` +
		`</filter></defs></svg>` +
		`<div style="filter:url(#inv);position:relative;top:100px;width:100px;height:100px;background:white">` +
		`<div style="position:absolute;top:-100px;width:100px;height:100px;background:rgb(0,128,0)"></div>` +
		`</div></body></html>`
	img := renderToImage(t, page, 200, 250)
	// The default region (-10%..120%) of the 100x100 border box only reaches
	// 10px above it; with the child included it covers the child.
	sampleColorClose(t, img, 50, 50, 255, 127, 255, 1, "child, inverted")
	sampleColorClose(t, img, 50, 150, 0, 0, 0, 0, "parent, inverted")
}

// TestFilterViewport_EmptyBorderBox — userSpaceOnUse coordinates are relative
// to the layer's origin and its border box is the viewport
// (ComputeFilterViewport, core/paint/paint_layer.cc:164-174 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), even when the border box is 0x0
// and the reference box, from a child, starts elsewhere.
func TestFilterViewport_EmptyBorderBox(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="0" height="0" style="position:absolute"><defs>` +
		`<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="20" height="20">` +
		`<feFlood flood-color="green"/></filter></defs></svg>` +
		`<div style="margin:50px;width:0;height:0;filter:url(#f)">` +
		`<div style="position:relative;left:-50px;top:-50px;width:100px;height:100px;background:red"></div>` +
		`</div></body></html>`
	img := renderToImage(t, page, 200, 200)
	sampleColorClose(t, img, 60, 60, 0, 128, 0, 0, "region at the 0x0 box's origin")
	sampleColorClose(t, img, 10, 10, 255, 255, 255, 0, "outside the region")
}

// TestFilterReferenceBox_ChildOwnTransform — a child is mapped through its own
// Transform(), not its parent's perspective, and a corner with w < 0 divides by
// w, as gfx::Transform::MapRect does (core/paint/paint_layer.cc:2213-2216 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; ui/gfx/geometry/transform.cc:
// 775-786, 1022-1036 @ a9f50e52). The flood fills the reference box.
func TestFilterReferenceBox_ChildOwnTransform(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="0" height="0" style="position:absolute"><defs>` +
		`<filter id="f" x="0" y="0" width="1" height="1"><feFlood flood-color="green"/></filter></defs></svg>` +
		`<div style="position:absolute;left:200px;top:200px;width:100px;height:100px;perspective:100px;filter:url(#f)">` +
		`<div style="width:100px;height:100px;transform:%s"></div></div></body></html>`
	// translateZ under the parent's perspective would double the box about
	// (250,250) to (150,150)-(350,350); its own transform leaves it in place.
	img := renderToImage(t, fmt.Sprintf(page, "translateZ(50px)"), 400, 400)
	sampleColorClose(t, img, 250, 250, 0, 128, 0, 0, "translateZ: inside the box")
	sampleColorClose(t, img, 160, 250, 255, 255, 255, 0, "translateZ: outside the box")
	// w = -1 about origin (200,200): x maps to 400 - x, so the child lands on
	// (100,100)-(200,200) and the reference box is (100,100)-(300,300).
	img = renderToImage(t, fmt.Sprintf(page, "matrix3d(1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,-1);transform-origin:0 0"), 400, 400)
	sampleColorClose(t, img, 150, 150, 0, 128, 0, 0, "w < 0: mapped child")
}

// TestShorthandFilter_NotCroppedToReferenceBox — a CSS shorthand filter is a
// SaveLayerOp with no bounds (ConversionContext::StartEffect,
// platform/graphics/compositing/paint_chunks_to_cc_layer.cc:737-743 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a descendant painted outside the
// border box is filtered, not cropped. Geometry from WPT
// filter-effects/backdrop-filter-backdrop-root-filter.
func TestShorthandFilter_NotCroppedToReferenceBox(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:50px;height:50px;background:green;filter:invert(0.1)">` +
		`<div style="position:relative;left:50px;width:50px;height:50px;border:1px solid black;box-sizing:border-box"></div>` +
		`</div></body></html>`
	img := renderToImage(t, page, 200, 100)
	// Black inverted by 0.1 is 26.
	sampleColorClose(t, img, 50, 20, 26, 26, 26, 1, "child's left border")
	sampleColorClose(t, img, 99, 20, 26, 26, 26, 1, "child's right border")
}

// TestShorthandFilter_OffscreenDescendant — a skip link far off-screen inside a
// filtered box must not switch the filter off or size the buffer to it: Skia
// clips a layer's bounds to the device clip (get_layer_mapping_and_bounds,
// src/core/SkCanvas.cpp:570-600 @ d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e).
func TestShorthandFilter_OffscreenDescendant(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="position:relative;width:200px;height:200px;background:rgb(255,0,0);filter:grayscale(1)">` +
		`<a href="#main" style="position:absolute;left:-9999px">Skip to content</a>` +
		`</div></body></html>`
	img := renderToImage(t, page, 300, 300)
	// grayscale(1) of red: 0.2126 * 255.
	sampleColorClose(t, img, 100, 100, 54, 54, 54, 2, "filtered box")
}

// TestMixedFilterChain_ReferenceStageCropped — in `url(#off) grayscale(1)` the
// reference stage's output is cropped to its SVG filter region before the
// next stage, as it is when the url() stands alone
// (FilterEffectBuilder::BuildReferenceFilter, core/paint/filter_effect_builder.cc:
// 534-550; FilterEffect::GetCropRect, platform/graphics/filters/
// filter_effect.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestMixedFilterChain_ReferenceStageCropped(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<svg width="0" height="0" style="position:absolute"><defs>` +
		`<filter id="off" x="0" y="0" width="1" height="1"><feOffset dx="50" dy="0"/></filter>` +
		`</defs></svg>` +
		`<div style="width:100px;height:100px;background:rgb(255,0,0);filter:url(#off) grayscale(1)"></div>` +
		`</body></html>`
	img := renderToImage(t, page, 300, 200)
	sampleColorClose(t, img, 75, 50, 54, 54, 54, 2, "offset content inside the region")
	sampleColorClose(t, img, 125, 50, 255, 255, 255, 0, "offset content past the region is cropped")
}

// TestShorthandFilter_TransformedEdgeRowSurvives — a scaled and a rotated
// filtered box keep their border-box edge rows: the buffer covers the paint
// extent mapped into the filter layer.
func TestShorthandFilter_TransformedEdgeRowSurvives(t *testing.T) {
	const scaled = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="position:absolute;left:50px;top:50px;width:100px;height:100px;background:rgb(255,0,0);` +
		`transform:scale(1.5);transform-origin:0 0;filter:grayscale(1)"></div></body></html>`
	img := renderToImage(t, scaled, 300, 300)
	for _, p := range [][2]int{{50, 50}, {199, 50}, {50, 199}, {199, 199}} {
		sampleColorClose(t, img, p[0], p[1], 54, 54, 54, 2, "scaled box edge")
	}
	const rotated = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="position:absolute;left:100px;top:100px;width:100px;height:100px;background:rgb(255,0,0);` +
		`transform:rotate(90deg);filter:grayscale(1)"></div></body></html>`
	img = renderToImage(t, rotated, 300, 300)
	for _, p := range [][2]int{{100, 100}, {199, 100}, {100, 199}, {199, 199}} {
		sampleColorClose(t, img, p[0], p[1], 54, 54, 54, 2, "rotated box edge")
	}
}
