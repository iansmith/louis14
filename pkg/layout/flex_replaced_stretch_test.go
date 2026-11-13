package layout

import "testing"

// A stretched replaced flex item keeps its flexed main size and takes the
// line's cross size: its layout space fixes the main size and stretches the
// cross axis (BuildSpaceForLayout with main_axis_final_size and
// line_cross_size, flex/flex_layout_algorithm.cc:699-797), and
// ComputeReplacedSize honours both, so the ratio does not re-derive the main
// size; an image with only an aspect ratio stretches like any other
// (DoesItemStretch :648-654; StretchFit, length_utils.cc:1173-1196). All @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestFlexStretchedReplacedItemKeepsItsMainSize(t *testing.T) {
	const sized = `data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"><rect width="20" height="10"/></svg>`
	for _, c := range []struct{ name, src string }{
		{"natural size", sized},
		{"ratio only", ratioOnlySVG},
	} {
		t.Run(c.name, func(t *testing.T) {
			html := `<!DOCTYPE html><body style="margin:0"><div style="display:flex;width:100px;height:100px"><img id="i" style="flex:1 1 0px;min-width:0" src='` + c.src + `'></div>`
			assertBoxGeometry(t, "#i", findBoxByID(layoutRootTestHTML(t, html), "i"), 0, 0, 100, 100)
		})
	}
}
