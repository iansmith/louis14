package layout

import "testing"

// The flex basis of a replaced item whose main axis is its inline axis is
// its max-content inline size, which for a replaced box is
// ComputeReplacedSize with the inline lengths ignored, in the item's layout
// space (MinMaxSizesFunc → BlockNode::ComputeMinMaxSizes,
// flex/flex_layout_algorithm.cc:875-880, block_node.cc:1023-1026). With only
// an aspect ratio and no block size it stretches to the available inline
// size (StretchFit, length_utils.cc:1173-1196), not the 300x150 default. All
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestFlexReplacedRowBasis(t *testing.T) {
	cases := []struct {
		name, style string
		x, y, w, h  float64
	}{
		// 400 wide, so 200 tall, centred in the 400px cross size.
		{"wide container, centered", "width:400px;height:400px;align-items:center", 0, 100, 400, 200},
		// The basis is 100, not 300: it does not overflow when it cannot shrink.
		{"no shrink", "width:100px;height:100px;align-items:center", 0, 25, 100, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := `<!DOCTYPE html><body style="margin:0"><div style="display:flex;` + c.style + `"><img id="i" style="flex-shrink:0" src='` + ratioOnlySVG + `'></div>`
			assertBoxGeometry(t, "#i", findBoxByID(layoutRootTestHTML(t, html), "i"), c.x, c.y, c.w, c.h)
		})
	}
}
