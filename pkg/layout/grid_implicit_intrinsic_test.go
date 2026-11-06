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

// The intrinsic width of an inline-grid, for the edge cases of sizing its
// placed tracks: an empty or inline-size-contained grid places no item, so
// it has no implicit tracks (Blink's BuildGridSizingTreeIgnoringChildren,
// grid_layout_algorithm.cc:279-284 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a spanning item's contribution
// excludes the gaps and fixed tracks it crosses; and gutters beside
// collapsed auto-fit tracks collapse too (CSS Grid 2 §7.2.2.1).
func TestInlineGridIntrinsicWidthEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name, grid, items string
		wantW             float64
	}{
		{"empty, grid-auto-columns", `grid-auto-columns:100px`, ``, 0},
		{"inline-size contained, grid-auto-columns", `grid-auto-columns:100px; contain:inline-size`,
			`<div style="width:50px;height:20px"></div>`, 0},
		{"spanner across a gap", `grid-template-columns:auto auto; column-gap:10px`,
			`<div style="grid-column:1/3; width:100px;height:20px"></div>`, 100},
		{"spanner across a fixed track", `grid-template-columns:100px auto`,
			`<div style="grid-column:1/3; width:150px;height:20px"></div>`, 150},
		{"collapsed auto-fit gutters", `grid-template-columns:100px repeat(auto-fit,50px) 100px; column-gap:10px`, ``, 210},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div id=g style="display:inline-grid; `+tc.grid+`">`+
			tc.items+`</div></body>`, nil)
		if g := findBox(fl.root, byID("g")); g == nil || g.Width != tc.wantW {
			t.Errorf("%s: grid %s, want width %v", tc.name, boxDims(g), tc.wantW)
		}
	}
}
