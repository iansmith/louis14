package render_test

import "testing"

// TestOutOfFlowEscapesStackingContextOverflowClip — an absolutely positioned
// box whose containing block is outside an opacity layer's overflow clip is
// still in the opacity group but not under that clip: its clip comes from its
// containing block (FragmentPaintPropertyTreeBuilder::UpdateOutOfFlowContext and
// SwitchToOOFContext, core/paint/paint_property_tree_builder.cc:3617-3622,
// 3750-3754 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). WPT
// css-position/position-absolute-under-non-containing-stacking-context,
// without the scroll.
func TestOutOfFlowEscapesStackingContextOverflowClip(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="position:relative;width:200px;height:200px">` +
		`<div style="height:0;overflow:hidden;opacity:0.5;margin:50px">` +
		`<div style="width:100px;height:100px;position:absolute;background:rgb(0,128,0)"></div>` +
		`</div></div></body></html>`
	img := renderToImage(t, page, 300, 300)
	sampleColorClose(t, img, 100, 100, 128, 192, 128, 2, "absolute box at half opacity")
}
