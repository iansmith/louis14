package layout

import "testing"

// A grid's intrinsic inline size runs auto-placement first, so the implicit
// columns grid-auto-flow:column creates each take their own items'
// contributions (GridLayoutAlgorithm::ComputeMinMaxSizes builds the grid
// sizing tree, and with it the placement, before sizing columns:
// layout/grid/grid_layout_algorithm.cc:279-284 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Mirrors WPT
// css-contain/contain-layout-baseline-003.
func TestInlineGridCountsImplicitColumns(t *testing.T) {
	for _, tc := range []struct {
		name, grid, secondWidth string
		wantW, wantBX           float64 // wantBX < 0: position not checked
	}{
		{"auto-flow column", `grid-auto-flow:column`, "50px", 100, 50},
		{"auto-flow column, gap", `grid-auto-flow:column; column-gap:10px`, "50px", 110, 60},
		// Each column takes its own item's contribution, not the widest
		// item's. (Where the second item lands then depends on the column
		// pass sizing each auto track from its items, which is LOU-691.)
		{"auto-flow column, unequal items", `grid-auto-flow:column`, "80px", 130, -1},
		{"grid-auto-columns:auto, unequal items", `grid-auto-flow:column; grid-auto-columns:auto`, "80px", 130, -1},
		{"auto auto template, unequal items", `grid-template-columns:auto auto`, "80px", 130, -1},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div id=g style="display:inline-grid; `+tc.grid+`">`+
			`<div style="width:50px;height:20px"></div><div id=b style="width:`+tc.secondWidth+`;height:20px"></div></div></body>`, nil)
		g, b := findBox(fl.root, byID("g")), findBox(fl.root, byID("b"))
		if g == nil || b == nil || g.Width != tc.wantW || (tc.wantBX >= 0 && b.X != tc.wantBX) {
			t.Errorf("%s: grid %s, #b %s; want grid width %v, #b at x=%v", tc.name, boxDims(g), boxDims(b), tc.wantW, tc.wantBX)
		}
	}
}
