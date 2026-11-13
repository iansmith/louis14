package layout

import "testing"

// Stretch applies only to an auto size: a grid item whose size in the grid's
// block axis is not auto (a percentage height, which an orthogonal item reads
// as its inline size) keeps that size in a stretching row. Blink lays the
// item out with its row_auto_behavior and fixes no block size
// (GridLayoutAlgorithm::CreateConstraintSpace, grid/grid_layout_algorithm.cc:
// 1358-1409 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each grid is
// 100px tall with two 20px columns; want is #b's size, from Chrome.
func TestGridRowStretchKeepsANonAutoBlockSize(t *testing.T) {
	cases := []struct {
		name, item string
		w, h       float64
	}{
		{"orthogonal item with a percentage height", "writing-mode:vertical-rl;height:20%", 20, 20},
		{"parallel item with a percentage height", "height:20%", 20, 20},
		// Guard: an auto size stretches.
		{"orthogonal item with an auto height", "writing-mode:vertical-rl", 20, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:inline-grid;grid-template-columns:20px 20px;height:100px"><div></div><div id="b" style="`+c.item+`"></div></div>`)
			b := findBoxByID(r, "b")
			if b == nil {
				t.Fatal("no #b box")
			}
			if b.Width != c.w || b.Height != c.h {
				t.Errorf("#b %vx%v, want %vx%v", b.Width, b.Height, c.w, c.h)
			}
		})
	}
}
