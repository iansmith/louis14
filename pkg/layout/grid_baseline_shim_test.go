package layout

import "testing"

// A baseline-aligned item's row contribution includes its baseline shim:
// the distance its baseline sits below the row's shared baseline, so an
// auto row grows to hold the shifted item. The shim also raises the
// ClampIntrinsicMinSize floor (min_clamp_size = margins + shim + border and
// padding). Mirrors CalculateBaselineShim and its uses in
// ContributionSizeForGridItem (grid/grid_layout_algorithm.cc:606-623,
// :651-661, :705-714, :776-799 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// The second item ("X<br>X<br>X", baseline 8) is shifted 20px down to meet
// the first (baseline 28). Want heights from Chrome headless.
func TestGridBaselineShimGrowsAutoRows(t *testing.T) {
	const shifted = `<div style="padding-top:20px">X</div><div>X<br>X<br>X</div>`
	cases := []struct {
		name, grid, items string
		height            float64
	}{
		{"first baseline", "align-items:baseline", shifted, 50},
		{"last baseline", "align-items:last baseline", `<div style="padding-bottom:20px">X</div><div>X<br>X<br>X</div>`, 50},
		{"orthogonal item's synthesized baseline", "align-items:baseline",
			`<div style="writing-mode:vertical-lr;width:10px">XXX</div><div>X<br>X<br>X<br>X</div>`, 62},
		{"second row unaffected", "align-items:baseline;grid-template-rows:auto auto", shifted + `<div>Y</div>`, 60},
		{"shim in the clamp floor", "align-items:baseline;grid-template-rows:minmax(auto,0px)",
			`<div style="padding-top:20px">X</div><div style="border-bottom:5px solid">X<br>X<br>X</div>`, 25},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font:10px/10px Ahem"><div id="g" style="display:grid;grid-template-columns:50px 50px;width:100px;`+
				c.grid+`">`+c.items+`</div>`)
			g := findBoxByID(r, "g")
			if g == nil {
				t.Fatal("no #g box")
			}
			if g.Height != c.height {
				t.Errorf("grid height %v, want %v", g.Height, c.height)
			}
		})
	}
}
