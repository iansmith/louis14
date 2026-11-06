package render_test

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/render"
)

// LOU-718 (filter-effects worklist D2): a non-atomic inline gets its own
// self-painting layer for every reason in LayoutInline::LayerTypeRequired
// (layout_inline.cc:759-765 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// relative or sticky position, or CreatesGroup() (opacity, filter, clip-path,
// mask, blend mode, isolation, backdrop-filter). The layer owns the inline's
// fragments, text and floats, so the inline's effects apply once to all of
// them.

var (
	inlineLayerYellow = color.RGBA{255, 255, 0, 255}
	inlineLayerOrange = color.RGBA{255, 165, 0, 255}
	// Orange inverted.
	inlineLayerAzure = color.RGBA{0, 90, 255, 255}
	// Magenta inverted.
	inlineLayerLime = color.RGBA{0, 255, 0, 255}
)

// The float is inside the filtered inline's layer
// (LayoutObject::PaintingLayer), so the inline's filter applies to it: blue
// inverts to yellow.
func TestInlineLayer_FilterAppliesToFloat(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div><span style="filter: invert(1)"><div style="float:left;width:100px;height:100px;background:blue"></div></span></div>`, 200, 150)
	wantPixel(t, img, 50, 50, inlineLayerYellow, "float under the inverted inline")
}

// An inline's clip-path reference box is its first box fragment
// (ClipPathClipper's BorderBoxRect, clip_path_clipper.cc:97-107 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the clip applies once to the
// inline's layer: inset(0) keeps the first line and cuts away the second.
func TestInlineLayer_ClipPathReferenceBoxIsFirstFragment(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0;font-size:20px;line-height:40px}</style>
<div style="width:100px"><span style="clip-path: inset(0); background: red; color: red">aaaaaaa aaaaaaa aaaaaaa</span></div>`, 200, 150)
	wantPixel(t, img, 5, 20, canvasRed, "first line, inside the reference box")
	wantPixel(t, img, 5, 60, canvasWhite, "second line, outside the reference box")
}

// A relatively positioned inline paints at Appendix E step 6 in tree order,
// as a positioned layer: after an earlier absolutely positioned sibling.
func TestInlineLayer_RelativeInlinePaintsAfterEarlierPositioned(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
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
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0;background:orange}</style>
<div style="position:absolute;left:10px;top:10.6px;width:50px;height:50px;backdrop-filter:invert(1)"></div>`, 100, 100)
	wantPixel(t, img, 30, 10, inlineLayerOrange, "row above the snapped backdrop bounds")
	wantPixel(t, img, 30, 11, inlineLayerAzure, "first row inside: orange inverted")
}

// An absolutely positioned backdrop-filter inside a clip-path inline samples
// the inline's layer, its backdrop root, not the page behind it
// (backdrop-filter-backdrop-root-clip-path-2): the inline's magenta inverts
// to lime, the yellow outside it is left alone. The abspos layer joins the
// inline's layer after the paint-tree walk (reparentZOrderByAncestorSC).
func TestInlineLayer_BackdropRootForReparentedAbspos(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div style="background:yellow;padding:100px">
<div style="position:relative;clip-path:inset(-50px);background:rgb(255,0,255);padding:100px;display:inline">
<div style="backdrop-filter:invert(1);position:absolute;top:-50px;left:-50px;width:400px;height:400px"></div>
</div></div>`, 400, 300)
	wantPixel(t, img, 150, 100, inlineLayerLime, "the inline's magenta, inverted")
	wantPixel(t, img, 70, 100, inlineLayerYellow, "outside the backdrop root's content, inside the clip")
}

// The inline's layer has no box of its own: its box fragments paint the
// inline's decorations and outline (InlineBoxFragmentPainter), so nothing
// paints at the synthesized layer box's origin.
func TestInlineLayer_PaintsNoDecorationsOfItsOwn(t *testing.T) {
	for _, decl := range []string{"outline:10px solid red", "box-shadow:0 0 0 10px red"} {
		t.Run(decl, func(t *testing.T) {
			img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div style="padding:50px"><span style="position:relative;`+decl+`">x</span></div>`, 200, 150)
			wantPixel(t, img, 0, 0, canvasWhite, "nothing at the layer box origin")
			wantPixel(t, img, 45, 58, canvasRed, "the fragment's own ring, left of the span")
		})
	}
}

// firstLinePage puts member, an inline-block, on a ::first-line whose
// opacity (0.999) makes it a synthetic paint group; the opacity moves a
// channel by at most one level.
func firstLinePage(member, content string) string {
	return `<!DOCTYPE html><style>body{margin:0} p{margin:0} p::first-line{opacity:.999}</style>
<p><span style="display:inline-block;width:50px;height:50px;` + member + `">` + content + `</span></p>`
}

func wantPixelWithinOne(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, why string) {
	t.Helper()
	if got := img.RGBAAt(x, y); !colorWithin(got, want, 1) {
		t.Errorf("pixel (%d,%d) = %v, want %v within one level: %s", x, y, got, want, why)
	}
}

// A synthetic ::first-line paint group takes over only the ::first-line
// opacity from its members: an inline-block's own filter still applies
// (blue inverts to yellow).
func TestInlineLayer_FirstLineGroupKeepsAtomicMemberEffects(t *testing.T) {
	img := render.RenderPage(t, firstLinePage("background:blue;filter:invert(1)", ""), 200, 150)
	wantPixelWithinOne(t, img, 25, 25, inlineLayerYellow, "inline-block's own filter inside a ::first-line group")
}

// A ::first-line member that is a stacking context collects its own
// stacking descendants: its z-index:-1 child paints above its background.
func TestInlineLayer_FirstLineStackingMemberKeepsItsDescendants(t *testing.T) {
	img := render.RenderPage(t, firstLinePage("background:green;filter:hue-rotate(0deg)",
		`<span style="display:block;position:relative;z-index:-1;width:50px;height:50px;background:red"></span>`), 200, 150)
	wantPixelWithinOne(t, img, 25, 25, canvasRed, "the member's z-index:-1 child above its background")
}

// The clip-path reference box is the inline's first line's fragment, also
// when a block-in-inline splits the inline.
func TestInlineLayer_ClipPathReferenceBoxFirstLineWithBlockInInline(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0;font-size:20px;line-height:40px}</style>
<div style="width:200px"><span style="clip-path: inset(0); background: red; color: red">aaa<div>bbb</div>ccc</span></div>`, 200, 150)
	wantPixel(t, img, 5, 20, canvasRed, "first line, inside the reference box")
	wantPixel(t, img, 5, 100, canvasWhite, "last line, outside the reference box")
}

// minChannel returns the darkest channel value in the rect, so a test can
// tell opaque dark text (0) from text at half opacity (about 127) or no
// text at all (255).
func minChannel(img *image.RGBA, r image.Rectangle) uint8 {
	m := uint8(255)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			m = min(m, c.R, c.G, c.B)
		}
	}
	return m
}

// z-index applies to a positioned box only (Blink's AllowsZIndex,
// style_adjuster.cc:1245-1254 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a
// static inline's layer stays in z-order auto even with a z-index, so its
// background is not hidden under its parent's.
func TestInlineLayer_StaticInlineIgnoresZIndex(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div style="background:white"><span style="opacity:.5;z-index:-1;background:black;padding:0 50px">x</span></div>`, 200, 100)
	if m := minChannel(img, image.Rect(0, 0, 40, 20)); m > 200 {
		t.Errorf("the inline's background is hidden under its parent's (darkest channel %d)", m)
	}
}

// ::first-line opacity still applies to first-line text inside a relatively
// positioned span (master's routing, LOU-310): the text is at most half
// dark.
func TestInlineLayer_FirstLineOpacityInsideRelativeSpan(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0} p{margin:0;font-size:40px} p::first-line{opacity:.5}</style>
<p><span style="position:relative;color:black">WWWW</span></p>`, 200, 100)
	switch m := minChannel(img, image.Rect(0, 0, 200, 50)); {
	case m < 100:
		t.Errorf("first-line text painted opaque (darkest channel %d), want ::first-line opacity 0.5", m)
	case m > 200:
		t.Errorf("no first-line text painted (darkest channel %d)", m)
	}
}

// Visibility is per box, not per layer: a visible descendant of a hidden
// inline with a layer still paints.
func TestInlineLayer_HiddenInlinePaintsVisibleDescendants(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0;font-size:40px}</style>
<span style="position:relative;visibility:hidden">a <b style="visibility:visible;color:black">WWW</b></span>`, 200, 100)
	if m := minChannel(img, image.Rect(0, 0, 200, 50)); m > 50 {
		t.Errorf("the visible descendant did not paint (darkest channel %d)", m)
	}
}
