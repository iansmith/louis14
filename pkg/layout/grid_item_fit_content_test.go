package layout

import "testing"

// A grid item that does not stretch inline is laid out in its grid area
// with a fit-content inline auto behaviour: its border-box inline size is
// its max-content (border and padding included), clamped to the area less
// its margins but never below its min-content. Blink passes the item's
// column_auto_behavior into its constraint space and fixes no inline size
// (GridLayoutAlgorithm::CreateConstraintSpace, grid/grid_layout_algorithm.cc:
// 1358-1409); the item's ComputeInlineSizeForFragment shrinks to fit
// (length_utils.cc:559-568 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Each item has a 2px border, 3px/10px padding, a 5px start margin and a
// 20x10 child; want is its offset and size in a 100x60 area, from Chrome.
func TestGridItemFitContentInlineSizeIncludesBorderPadding(t *testing.T) {
	const child = `<div style="width:20px;height:10px"></div>`
	cases := []struct {
		name, self, content string
		x, y, w, h          float64
	}{
		{"start", "justify-self:start;align-self:start", child, 5, 0, 44, 20},
		{"center", "justify-self:center;align-self:start", child, 30.5, 0, 44, 20},
		{"end", "justify-self:end;align-self:start", child, 56, 0, 44, 20},
		{"start, stretched in the block axis (re-layout)", "justify-self:start", child, 5, 0, 44, 60},
		{"max-content wider than the area", "justify-self:start;align-self:start",
			`<div style="width:120px;height:5px"></div>`, 5, 0, 144, 15},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div id="g" style="display:grid;grid-template-columns:100px;grid-template-rows:60px;width:100px">`+
				`<div id="i" style="border:2px solid;padding:3px 10px;margin-left:5px;`+c.self+`">`+c.content+`</div></div>`)
			g, i := findBoxByID(r, "g"), findBoxByID(r, "i")
			if g == nil || i == nil {
				t.Fatal("no #g or #i box")
			}
			if x, y := i.X-g.X, i.Y-g.Y; x != c.x || y != c.y || i.Width != c.w || i.Height != c.h {
				t.Errorf("item at %v,%v size %vx%v, want %v,%v size %vx%v", x, y, i.Width, i.Height, c.x, c.y, c.w, c.h)
			}
		})
	}
}
