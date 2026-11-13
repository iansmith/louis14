package layout

import "testing"

// A minmax() track with an intrinsic minimum or maximum sizes from its
// items (GridTrackSize::HasIntrinsicMinTrackBreadth /
// HasIntrinsicMaxTrackBreadth, core/style/grid_track_size.h:197-208;
// ResolveIntrinsicTrackSizes in the grid track sizing @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an auto or min-content
// minimum takes the item's 10px border box (border 2 + padding 3) or 27px
// height, a min-content maximum lets the row grow to it.
func TestGridMinMaxIntrinsicBreadths(t *testing.T) {
	const box = `<div id="i" style="border:2px solid;padding:3px"></div>`
	const tall = `<div id="i" style="height:27px"></div>`
	cases := []struct {
		rows, item string
		want       float64
	}{
		{"minmax(auto, 0px)", box, 10},
		{"minmax(min-content, 0px)", tall, 27},
		{"minmax(max-content, 0px)", tall, 27},
		{"minmax(0px, min-content)", tall, 27},
		// Guard: a length minimum and maximum, as before.
		{"minmax(5px, 8px)", tall, 8},
	}
	for _, c := range cases {
		t.Run(c.rows, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div id="g" style="display:grid;grid-template-columns:100px;grid-template-rows:`+c.rows+`">`+c.item+`</div>`)
			if g := findBoxByID(r, "g"); g == nil || g.Height != c.want {
				t.Errorf("grid height %v, want %v", g.Height, c.want)
			}
		})
	}
}
