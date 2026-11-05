package render_test

import "testing"

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
