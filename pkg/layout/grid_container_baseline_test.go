package layout

import "testing"

// A grid container's first (last) baseline is its first (last) row's
// shared baseline when that row has baseline-aligned items, else the
// first-baseline-or-synthesized (last) of its start-most (end-most) item.
// Mirrors GridBaselineAccumulator (grid/grid_baseline_accumulator.h) as
// PlaceGridItems uses it (grid/grid_layout_algorithm.cc:1804, :1900-1946 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each case baseline-aligns a
// one-line "X" (#y) with a nested grid in an outer grid; want is #y's
// offset, from Chrome headless.
func TestGridContainerBaselineFromItems(t *testing.T) {
	cases := []struct {
		name, outer, inner string
		y                  float64
	}{
		{"first baseline synthesized from the first item", "align-items:baseline",
			`<div class="in"><div style="height:20px"></div><div>X</div></div>`, 12},
		{"last baseline from the last item", "align-items:last baseline",
			`<div class="in"><div>X</div><div style="height:20px"></div></div>`, 22},
		{"last baseline synthesized from the last item", "align-items:last baseline",
			`<div class="in"><div style="height:20px"></div><div>X</div></div>`, 20},
		{"first row's shared baseline", "align-items:baseline",
			`<div class="in" style="grid-template-columns:20px 20px;align-items:baseline"><div style="padding-top:15px">X</div><div>X</div><div>Z</div></div>`, 15},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}.in{display:grid}</style>`+
				`<div id="g" style="display:grid;grid-template-columns:50px 50px;width:100px;`+c.outer+`"><div id="y">X</div>`+c.inner+`</div>`)
			g, y := findBoxByID(r, "g"), findBoxByID(r, "y")
			if g == nil || y == nil {
				t.Fatal("no #g or #y box")
			}
			if got := y.Y - g.Y; got != c.y {
				t.Errorf("#y at %v, want %v", got, c.y)
			}
		})
	}
}
