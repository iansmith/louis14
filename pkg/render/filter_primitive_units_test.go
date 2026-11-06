package render_test

import "testing"

// effect-reference-obb-dimensions: an feOffset of 0.2 in objectBoundingBox
// primitive units moves a 100px box by 20px (Filter::ApplyHorizontalScale,
// filter.cc:65-75 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFEOffset_ObjectBoundingBoxUnits(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<div style="position:absolute;width:100px;height:100px;background:green;filter:url(#o)"></div>
<svg height="0"><filter id="o" primitiveUnits="objectBoundingBox" x="0" y="0" width="1.25" height="1.25"><feOffset dx="0.2" dy="0.2"/></filter></svg>`, 140, 140)
	wantPixel(t, img, 10, 10, canvasWhite, "the box's old corner, offset away")
	wantPixel(t, img, 115, 115, canvasGreen, "the box offset by 20px")
}
