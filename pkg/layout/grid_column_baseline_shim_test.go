package layout

import "testing"

// Column-axis baseline alignment (justify-self: baseline) grows an auto
// column by each item's baseline shim, as the rows do: before the columns
// are sized, the items' column baselines come from their measure layout
// (an orthogonal item's own baselines; a parallel item with a definite
// inline size, its synthesized one), and the shim is added to the column
// contributions and the ClampIntrinsicMinSize floor (ComputeBaselineAlignment
// before CompleteTrackSizingAlgorithm(kForColumns); CalculateBaselineShim in
// ContributionSizeForGridItem, grid/grid_layout_algorithm.cc:606-714,
// :776-799, :951-1037, :1263-1281 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Each grid has two 50px rows and one auto column; want is the grid's
// width and #b's offset, from Chrome headless.
func TestGridColumnBaselineShimGrowsAutoColumns(t *testing.T) {
	cases := []struct {
		name, grid, items string
		width, bx         float64
	}{
		{"orthogonal items, first baseline", "justify-items:baseline",
			`<div class="v" style="padding-left:20px">X</div><div class="v" id="b">X<br>X<br>X</div>`, 50, 20},
		{"orthogonal items, last baseline", "justify-items:last baseline",
			`<div class="v" style="padding-right:20px">X</div><div class="v" id="b">X<br>X<br>X</div>`, 50, 0},
		{"parallel items with definite widths", "justify-items:baseline",
			`<div style="width:20px;height:10px;margin-left:20px"></div><div id="b" style="width:40px;height:10px"></div>`, 60, 20},
		{"shim in the clamp floor", "justify-items:baseline;grid-template-columns:minmax(auto,0px)",
			`<div class="v" style="padding-left:20px">X</div><div class="v" id="b" style="border-right:5px solid">X X X</div>`, 25, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}.v{writing-mode:vertical-lr}</style>`+
				`<div id="g" style="display:inline-grid;grid-template-rows:50px 50px;vertical-align:top;`+c.grid+`">`+c.items+`</div>`)
			g, b := findBoxByID(r, "g"), findBoxByID(r, "b")
			if g == nil || b == nil {
				t.Fatal("no #g or #b box")
			}
			if g.Width != c.width || b.X-g.X != c.bx {
				t.Errorf("grid width %v, #b at %v; want %v, %v", g.Width, b.X-g.X, c.width, c.bx)
			}
		})
	}
}
