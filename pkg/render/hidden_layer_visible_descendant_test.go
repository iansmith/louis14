package render_test

import (
	"image/color"
	"testing"
)

// LOU-725: visibility is per box, not per layer. Blink paints a hidden
// layer's child layers and gates only its own content on HasVisibleContent
// (PaintLayerPainter::PaintLayerContents, paint_layer_painter.cc:362-366;
// PaintLayer::UpdateHasVisibleContent, paint_layer.cc:511-537 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); each painter then checks its own
// object's visibility. So a visibility:visible descendant of a hidden box
// paints, and the hidden box's own background does not.
func TestHiddenLayer_PaintsVisibleDescendants(t *testing.T) {
	for _, tc := range []struct{ name, outer string }{
		{"stacking context", "position:relative;z-index:0"},
		{"filtered", "filter:hue-rotate(0deg)"},
		{"flow block", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div style="`+tc.outer+`;visibility:hidden;background:red;padding-left:50px;width:50px;height:50px">
<div style="visibility:visible;background:green;width:50px;height:50px"></div></div>`, 200, 100)
			wantPixel(t, img, 75, 25, canvasGreen, "the visible child")
			wantPixel(t, img, 25, 25, canvasWhite, "the hidden box's own background")
		})
	}
}

// A hidden frame owner paints none of its frame's document: ReplacedPainter
// bails for an invisible replaced box before EmbeddedContentPainter paints
// the child frame (ReplacedPainter::ShouldPaint, replaced_painter.cc:270-276 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The nested document is styled
// on its own, so its root is visible.
func TestHiddenLayer_FrameOwnerHidesItsDocument(t *testing.T) {
	for _, tc := range []struct {
		visibility string
		want       color.RGBA
	}{
		{"visible", canvasGreen}, // control: the frame's document renders
		{"hidden", canvasWhite},
	} {
		img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+
			`<iframe style="visibility:`+tc.visibility+`;border:0;width:100px;height:80px" srcdoc="`+
			`<html style='background:green'><body style='margin:0'></body></html>"></iframe>`, 100, 100)
		wantPixel(t, img, 50, 40, tc.want, tc.visibility+" iframe's document")
	}
}

// A hidden box paints no mask image: PaintLayerPainter paints the kMask phase
// only for a layer with visible content (paint_layer_painter.cc:525-531) and
// BoxFragmentPainter::PaintObject paints the mask only for a visible box
// (box_fragment_painter.cc:876 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// so nothing masks its visible descendants.
func TestHiddenLayer_MaskOfHiddenBoxDoesNotApply(t *testing.T) {
	for _, tc := range []struct {
		visibility string
		want       color.RGBA
	}{
		{"visible", canvasWhite}, // control: the transparent mask applies
		{"hidden", canvasGreen},
	} {
		img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div style="visibility:`+tc.visibility+`;mask-image:linear-gradient(transparent,transparent);width:50px;height:50px">
<div style="visibility:visible;background:green;width:50px;height:50px"></div></div>`, 100, 100)
		wantPixel(t, img, 25, 25, tc.want, "the visible child of a "+tc.visibility+" masked box")
	}
}

// A hidden <svg> still walks its content: ReplacedPainter::ShouldPaint lets
// an SVG root through (replaced_painter.cc:270-273 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and each SVG element checks its
// own visibility.
func TestHiddenLayer_HiddenSVGPaintsVisibleChild(t *testing.T) {
	for _, tc := range []struct {
		rect string
		want color.RGBA
	}{
		{`visibility="visible"`, canvasGreen},
		{"", canvasWhite}, // control: the rect inherits hidden
	} {
		img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg style="visibility:hidden;display:block" width="100" height="100"><rect `+tc.rect+` width="50" height="50" fill="green"/></svg>`, 100, 100)
		wantPixel(t, img, 25, 25, tc.want, "a hidden svg's rect "+tc.rect)
	}
}
