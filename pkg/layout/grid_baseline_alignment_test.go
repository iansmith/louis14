package layout

import "testing"

// Baseline-aligned grid items share their track's baseline: each item's
// baseline (synthesized from its border box when it has none in its
// baseline writing mode) plus its start margin sets the track's major
// baseline, its end-side distance the minor one, and the item is offset so
// its baseline meets the shared one. Mirrors ComputeGridItemBaselines,
// StoreItemBaseline and ComputeBaselineOffset (grid/grid_layout_algorithm.cc:
// 951-1037, grid/grid_layout_utils.cc:26-113 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Fixed tracks, so no shim is
// needed. Want offsets (relative to the grid) from Chrome headless.
func TestGridBaselineAlignmentPlacesItemsOnTheSharedBaseline(t *testing.T) {
	const twoCols = "grid-template-columns:50px 50px;grid-template-rows:100px;width:100px"
	cases := []struct {
		name, grid, items string
		x, y              float64 // #b's offset
	}{
		{"row, first baseline", twoCols + ";align-items:baseline",
			`<div style="padding-top:20px">X</div><div id="b">X</div>`, 50, 20},
		{"row, last baseline", twoCols + ";align-items:last baseline",
			`<div style="padding-bottom:20px">X</div><div id="b">X</div>`, 50, 70},
		{"column, synthesized baseline", "grid-template-columns:100px;grid-template-rows:50px 50px;width:100px;justify-items:baseline",
			`<div style="margin-left:20px;width:10px">X</div><div id="b" style="width:30px">X</div>`, 20, 50},
		{"row, orthogonal item's synthesized baseline", twoCols + ";align-items:baseline",
			`<div style="writing-mode:vertical-lr;height:30px;width:10px"></div><div id="b" style="height:10px">X</div>`, 50, 22},
		{"row, spanning item aligns in its start-most row", "grid-template-columns:50px 50px;grid-template-rows:50px 50px;width:100px;align-items:baseline",
			`<div style="grid-row:span 2;padding-top:20px">X</div><div id="b">X</div>`, 50, 20},
		{"row, margins and font sizes", twoCols + ";align-items:baseline",
			`<div style="margin-top:15px">X</div><div id="b" style="font-size:20px;line-height:20px">X</div>`, 50, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font:10px/10px Ahem"><div id="g" style="display:grid;`+
				c.grid+`">`+c.items+`</div>`)
			g, b := findBoxByID(r, "g"), findBoxByID(r, "b")
			if g == nil || b == nil {
				t.Fatal("no #g or #b box")
			}
			if x, y := b.X-g.X, b.Y-g.Y; x != c.x || y != c.y {
				t.Errorf("#b at %v,%v, want %v,%v", x, y, c.x, c.y)
			}
		})
	}
}
