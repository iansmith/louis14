package layout

import "testing"

// A grid item's minimum contribution in a minmax(auto, 0px) track counts its
// border and padding but not its scrollbars, in both of Blink's branches:
//   - a scroll container in the track's axis has no content-based minimum;
//     its min size resolves against ComputeBorders + ComputePadding
//     (CalculateIntrinsicMinimumContribution, grid/grid_layout_utils.cc:
//     525-574);
//   - otherwise its content-based minimum is clamped to the spanned tracks'
//     0px, but not below min_clamp_size = margins + ComputeBorders +
//     ComputePadding (grid/grid_layout_algorithm.cc:776-799).
//
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b. Each item has a 2px border, 3px
// padding and a 17px scrollbar along the track, so its minimum is 10px.
// Chrome (17px classic scrollbars) makes every track 10px.
func TestGridScrollContainerMinimumExcludesScrollbars(t *testing.T) {
	const rows = "grid-template-rows:minmax(auto,0px);grid-template-columns:100px"
	const columns = "grid-template-columns:minmax(auto,0px);width:max-content"
	cases := []struct {
		name, grid, item string
		row              bool // the track is a row: check the grid's height, else its width
	}{
		{"row, parallel scroll container", rows, "overflow:scroll", true},
		{"row, orthogonal scroll container", rows, "overflow:scroll;writing-mode:vertical-rl", true},
		{"row, parallel clamped", rows, "overflow-x:scroll;overflow-y:hidden", true},
		{"row, orthogonal clamped", rows, "overflow-x:scroll;overflow-y:hidden;writing-mode:vertical-rl", true},
		{"column, orthogonal scroll container", columns, "overflow:scroll;writing-mode:vertical-rl", false},
		{"column, orthogonal clamped", columns, "overflow-y:scroll;overflow-x:hidden;writing-mode:vertical-rl", false},
		// Guards: the parallel column path already excludes them.
		{"column, parallel scroll container", columns, "overflow:scroll", false},
		{"column, parallel clamped", columns, "overflow-y:scroll;overflow-x:hidden", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div id="g" style="display:grid;`+c.grid+`">`+
				`<div style="border:2px solid;padding:3px;font:10px/10px Ahem;`+c.item+`">xx xx xx xx</div></div>`)
			g := findBoxByID(r, "g")
			if g == nil {
				t.Fatal("no #g box")
			}
			track := g.Width
			if c.row {
				track = g.Height
			}
			if track != 10 {
				t.Errorf("track = %v, want 10 (border + padding, no scrollbar)", track)
			}
		})
	}
}
