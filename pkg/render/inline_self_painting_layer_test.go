package render_test

import (
	"image/color"
	"testing"
)

// LOU-718 (filter-effects worklist D2): a non-atomic inline gets its own
// self-painting layer for every reason in LayoutInline::LayerTypeRequired
// (layout_inline.cc:759-765 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// relative or sticky position, or CreatesGroup() (opacity, filter, clip-path,
// mask, blend mode, isolation, backdrop-filter). The layer owns the inline's
// fragments, text and floats, so the inline's effects apply once to all of
// them.

// The float is inside the filtered inline's layer
// (LayoutObject::PaintingLayer), so the inline's filter applies to it: blue
// inverts to yellow.
func TestInlineLayer_FilterAppliesToFloat(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div><span style="filter: invert(1)"><div style="float:left;width:100px;height:100px;background:blue"></div></span></div>`, 200, 150)
	wantPixel(t, img, 50, 50, canvasYellow, "float under the inverted inline")
}

// An inline's clip-path reference box is its first box fragment
// (ClipPathClipper's BorderBoxRect, clip_path_clipper.cc:97-107 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the clip applies once to the
// inline's layer: inset(0) keeps the first line and cuts away the second.
func TestInlineLayer_ClipPathReferenceBoxIsFirstFragment(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0;font-size:20px;line-height:40px}</style>
<div style="width:100px"><span style="clip-path: inset(0); background: red; color: red">aaaaaaa aaaaaaa aaaaaaa</span></div>`, 200, 150)
	wantPixel(t, img, 5, 20, canvasRed, "first line, inside the reference box")
	wantPixel(t, img, 5, 60, canvasWhite, "second line, outside the reference box")
}

// A relatively positioned inline paints at Appendix E step 6 in tree order,
// as a positioned layer: after an earlier absolutely positioned sibling.
func TestInlineLayer_RelativeInlinePaintsAfterEarlierPositioned(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div style="position:absolute;left:0;top:0;width:200px;height:100px;background:red"></div>
<div><span style="position:relative;background:green;padding:0 100px;font-size:50px">x</span></div>`, 250, 150)
	wantPixel(t, img, 20, 20, canvasGreen, "relative inline over the earlier abspos box")
}

// Not inline-specific; backdrop-filter-inline-positioning needs it. A
// backdrop-filter's bounds are its reference box pixel-snapped
// (PaintLayer::BackdropFilterBounds, paint_layer.cc:2072-2078 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): at y=10.6 the box starts at row
// 11, as its background would.
func TestBackdropFilter_BoundsArePixelSnapped(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0;background:orange}</style>
<div style="position:absolute;left:10px;top:10.6px;width:50px;height:50px;backdrop-filter:invert(1)"></div>`, 100, 100)
	wantPixel(t, img, 30, 10, canvasOrange, "row above the snapped backdrop bounds")
	wantPixel(t, img, 30, 11, color.RGBA{0, 90, 255, 255}, "first row inside: orange inverted")
}

// An absolutely positioned backdrop-filter inside a clip-path inline samples
// the inline's layer, its backdrop root, not the page behind it
// (backdrop-filter-backdrop-root-clip-path-2): the inline's magenta inverts
// to lime, the yellow outside it is left alone. The abspos layer joins the
// inline's layer after the paint-tree walk (reparentZOrderByAncestorSC).
func TestInlineLayer_BackdropRootForReparentedAbspos(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div style="background:yellow;padding:100px">
<div style="position:relative;clip-path:inset(-50px);background:rgb(255,0,255);padding:100px;display:inline">
<div style="backdrop-filter:invert(1);position:absolute;top:-50px;left:-50px;width:400px;height:400px"></div>
</div></div>`, 400, 300)
	wantPixel(t, img, 150, 100, color.RGBA{0, 255, 0, 255}, "the inline's magenta, inverted")
	wantPixel(t, img, 70, 100, canvasYellow, "outside the backdrop root's content, inside the clip")
}
