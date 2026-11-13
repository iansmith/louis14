package layout

import "testing"

// A replaced flex item's automatic minimum size is the smaller of its
// specified and content size suggestions, and its content size suggestion
// is its min-content main size from ComputeReplacedSize in its layout space,
// where a stretched definite cross size transfers through the ratio
// (MinMaxSizesFunc(kContent), flex/flex_layout_algorithm.cc:1121-1145 with
// :875-880; block_node.cc:1023-1026 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 2:1 image stretched to 100
// tall cannot shrink below 200.
func TestFlexReplacedItemAutoMinSize(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"natural size", naturalSizeSVG},
		{"ratio only", ratioOnlySVG},
	} {
		t.Run(c.name, func(t *testing.T) {
			html := `<!DOCTYPE html><body style="margin:0"><div style="display:flex;width:100px;height:100px"><img id="i" src='` + c.src + `'></div>`
			assertBoxGeometry(t, "#i", findBoxByID(layoutRootTestHTML(t, html), "i"), 0, 0, 200, 100)
		})
	}
}
