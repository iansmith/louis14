package render_test

import "testing"

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
