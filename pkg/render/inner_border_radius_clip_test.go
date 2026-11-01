package render_test

import "testing"

// TestInnerBorderRadiusClip_UsesInnerRadii: overflow:hidden on a box with
// border:10px and border-radius:60px clips its content to the padding box
// rounded by the INNER radii (60 - 10 = 50), not the border-box radii.
// Blink installs InnerBorderRadiusClip from
// ContouredBorderGeometry::PixelSnappedContouredInnerBorder
// (paint_property_tree_builder.cc:2937-2976 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), then the rectangular
// OverflowClip. Mirrors css-backgrounds/border-radius-clipping-with-transform-001.
//
// Padding box 20..220, inner corner circle centre (70,70) r=50: pixel
// (44,30) is ~47px from the centre, inside the inner curve, so the blue
// child paints there. Border-box radii on the padding box (centre (80,80),
// r=60) put it ~61px out, clipping it to white.
func TestInnerBorderRadiusClip_UsesInnerRadii(t *testing.T) {
	const src = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="border:10px solid #000;border-radius:60px;width:200px;height:200px;overflow:hidden;position:absolute;top:10px;left:10px">` +
		`<div style="width:100%;height:100%;background:blue"></div></div></body></html>`
	img := renderToImage(t, src, 300, 300)
	sampleColorClose(t, img, 44, 30, 0, 0, 255, 0, "inside inner radius (44,30)")
	sampleColorClose(t, img, 120, 120, 0, 0, 255, 0, "centre (120,120)")
	// Just outside the inner curve (centre (70,70), r=50): ~53px away.
	sampleColorClose(t, img, 32, 35, 0, 0, 0, 0, "border outside inner curve (32,35)")
}

// TestInnerBorderRadiusClip_ClipMarginBorderBoxZero: overflow-clip-margin:
// border-box 0 clips to the border box rounded like the border edge, via
// AdjustRoundedClipForOverflowClipMargin (paint_property_tree_builder.cc:
// 2909-2935 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) — not the
// padding-box inner-radius clip. Mirrors
// css-overflow/overflow-clip-margin-border-radius-002 (last box).
//
// Border box 0..100 with radius 40 (centre (40,40)); pixel (20,15) is
// ~32px from the centre — inside the border edge, in the border area
// outside the padding box — so the black overflowing child shows there.
func TestInnerBorderRadiusClip_ClipMarginBorderBoxZero(t *testing.T) {
	const src = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:20px;height:20px;padding:20px;border:20px solid transparent;border-radius:40px;overflow:clip;overflow-clip-margin:border-box 0px">` +
		`<div style="width:180px;height:180px;margin:-80px 0 0 -80px;background:black"></div></div></body></html>`
	img := renderToImage(t, src, 200, 200)
	sampleColorClose(t, img, 20, 15, 0, 0, 0, 0, "border area inside border edge (20,15)")
	sampleColorClose(t, img, 3, 3, 255, 255, 255, 0, "outside border-edge corner (3,3)")
}
