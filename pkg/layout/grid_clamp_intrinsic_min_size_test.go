package layout

import "testing"

// A grid item's automatic minimum (its content-based minimum) is clamped to
// its spanned tracks' fixed maximum, but not below its margins, border and
// padding (ContributionSizeForGridItem's kForIntrinsicMinimums,
// grid/grid_layout_algorithm.cc:732-803; ClampIntrinsicMinSize,
// grid/grid_layout_utils.cc:603-616 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An item with a fixed size has
// no automatic minimum and is not clamped.
func TestGridClampIntrinsicMinSize(t *testing.T) {
	cases := []struct {
		name, grid, item string
		w, h             float64
	}{
		{"column clamped to its fixed maximum", "grid-template-columns:minmax(auto,20px);width:max-content",
			`<div style="font:10px/1 Ahem">xxxxx</div>`, 20, 10},
		{"never below border and padding", "grid-template-columns:minmax(auto,20px);width:max-content",
			`<div style="font:10px/1 Ahem;border:15px solid">xxxxx</div>`, 30, 40},
		{"row clamped to its fixed maximum", "grid-template-rows:minmax(auto,20px);grid-template-columns:100px",
			`<div><div style="height:50px"></div></div>`, 800, 20},
		// Spanning two 20px tracks and the 10px gap between them: 50.
		{"spanned tracks and gutter", "grid-template-columns:repeat(2,minmax(auto,20px));column-gap:10px;width:max-content",
			`<div style="font:10px/1 Ahem;grid-column:span 2">xxxxxxxx</div>`, 50, 10},
		// Guard: a fixed width is not an automatic minimum.
		{"fixed size not clamped", "grid-template-columns:minmax(auto,20px);width:max-content",
			`<div style="width:50px;height:5px"></div>`, 50, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div id="g" style="display:grid;`+c.grid+`">`+c.item+`</div>`)
			if g := findBoxByID(r, "g"); g == nil || g.Width != c.w || g.Height != c.h {
				t.Errorf("grid %vx%v, want %vx%v", g.Width, g.Height, c.w, c.h)
			}
		})
	}
}
