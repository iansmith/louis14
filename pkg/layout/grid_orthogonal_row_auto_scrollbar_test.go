package layout

import "testing"

// An orthogonal grid item's row contribution is its border-box min/max
// inline size, counting the auto scrollbar its layout at its column found:
// Blink's MinContentSize / MaxContentSize take the item's ComputeMinMaxSizes,
// whose geometry adds ComputeScrollbars, which reads the box's
// PaintLayerScrollableArea (grid/grid_layout_algorithm.cc:577-640,
// length_utils.cc:1543-1549 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// #i's 80px-wide child overflows its 50px grid column, so it has a 17px
// horizontal scrollbar: the auto row is its 20px max-content inline size
// (the child's height) plus 17. Chrome (17px classic scrollbars): #g and #i 37px tall.
func TestGridOrthogonalRowContributionCountsAutoScrollbar(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html>
<style>
body { margin: 0; font: 10px/10px Ahem; }
#g { display: grid; grid-template-columns: 50px; align-items: start; }
#i { writing-mode: vertical-rl; overflow: auto; }
</style>
<div id="g"><div id="i"><div style="width:80px;height:20px"></div></div></div>`)
	g, i := findBoxByID(root, "g"), findBoxByID(root, "i")
	if g == nil || i == nil {
		t.Fatal("no #g or #i box")
	}
	if g.Height != 37 || i.Height != 37 {
		t.Errorf("#g height %v, #i height %v, want 37 (20 + 17px scrollbar)", g.Height, i.Height)
	}
}
