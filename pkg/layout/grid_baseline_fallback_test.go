package layout

import "testing"

// A baseline-aligned item with only a synthesized baseline whose size in
// the baseline axis depends on an intrinsic (or flexible) track it spans (a
// percentage or stretch size) does not take part in baseline alignment: it
// uses its fallback alignment, start for the major group and end for the
// minor one, overflow-safe. Mirrors GridItemData::SetAlignmentFallback
// (grid/grid_item.cc:262-322 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// called from StoreItemBaseline / ComputeGridItemBaselines. Want offsets and
// sizes (relative to the grid) from Chrome headless.
func TestGridBaselineAlignmentFallback(t *testing.T) {
	cases := []struct {
		name, grid, items string
		y, h, gridH       float64 // #a's offset and height, and the grid's height
	}{
		{"last baseline, percentage height falls back to end", "align-items:last baseline",
			`<div id="a" style="height:100%"></div><div style="height:100px">X</div>`, 0, 100, 100},
		{"replaced percentage height does not grow the row", "align-items:baseline",
			`<canvas id="a" style="width:100%;height:100%"></canvas><div style="height:100px"><br></div>`, 0, 100, 100},
		// Guards: a fixed height takes part; a fixed row is not intrinsic.
		{"fixed height takes part", "align-items:baseline",
			`<div id="a" style="height:30px"></div><div style="padding-top:5px">X</div>`, 0, 30, 32},
		{"fixed row is not intrinsic", "align-items:baseline;grid-template-rows:60px",
			`<div id="a" style="height:100%"></div><div style="padding-top:5px">X</div>`, 0, 60, 60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font:10px/10px Ahem"><div id="g" style="display:grid;grid-template-columns:50px 50px;width:100px;`+
				c.grid+`">`+c.items+`</div>`)
			g, a := findBoxByID(r, "g"), findBoxByID(r, "a")
			if g == nil || a == nil {
				t.Fatal("no #g or #a box")
			}
			if y := a.Y - g.Y; y != c.y || a.Height != c.h || g.Height != c.gridH {
				t.Errorf("#a at %v height %v, grid height %v; want %v, %v, %v", y, a.Height, g.Height, c.y, c.h, c.gridH)
			}
		})
	}
}
