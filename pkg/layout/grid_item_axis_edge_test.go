package layout

import "testing"

// A grid item's alignment in each axis follows Blink's
// AxisEdgeFromItemPosition (grid/grid_item.cc:18-147) and AlignmentOffset
// (layout_utils.cc:640-664 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// auto margins take precedence and size the item fit-content; left/right
// and self-start/self-end resolve against the directions; justify-items:
// legacy <position> applies its position; and alignment is unsafe unless
// `safe` (or an auto margin) makes it safe, so an item larger than its area
// overflows both edges. Each case is a 100x100 grid area; want is the item's
// border-box offset in it and size, from Chrome (headless, a9f50e52-era
// stable).
func TestGridItemAxisEdgeFromItemPosition(t *testing.T) {
	cases := []struct {
		name, grid, item string
		x, y, w, h       float64
	}{
		{"justify-self:right", "", `<div id="i" style="justify-self:right;width:20px;height:20px"></div>`, 80, 0, 20, 20},
		{"rtl justify-self:left", "direction:rtl", `<div id="i" style="justify-self:left;width:20px;height:20px"></div>`, 0, 0, 20, 20},
		{"justify-self:self-end", "", `<div id="i" style="justify-self:self-end;width:20px;height:20px"></div>`, 80, 0, 20, 20},
		{"align-self:self-end, vertical-lr item", "", `<div id="i" style="align-self:self-end;writing-mode:vertical-lr;width:20px;height:20px"></div>`, 0, 80, 20, 20},
		{"margin-left:auto", "", `<div id="i" style="margin-left:auto;width:20px;height:20px"></div>`, 80, 0, 20, 20},
		{"margin-top and bottom auto", "", `<div id="i" style="margin-top:auto;margin-bottom:auto;width:20px;height:20px"></div>`, 0, 40, 20, 20},
		{"auto margin sizes fit-content inline", "", `<div id="i" style="margin-left:auto"><div style="width:16px;height:10px"></div></div>`, 84, 0, 16, 100},
		{"auto margin sizes fit-content block", "", `<div id="i" style="margin-top:auto;width:10px"><div style="height:16px"></div></div>`, 0, 84, 10, 16},
		{"auto margin beats stretch", "", `<div id="i" style="justify-self:stretch;margin-right:auto"><div style="width:16px;height:10px"></div></div>`, 0, 0, 16, 100},
		{"unsafe center overflows both edges", "", `<div id="i" style="justify-self:center;width:141px;height:20px"></div>`, -20.5, 0, 141, 20},
		{"unsafe end overflows the start edge", "", `<div id="i" style="justify-self:end;width:141px;height:20px"></div>`, -41, 0, 141, 20},
		{"justify-items:legacy center", "justify-items:legacy center", `<div id="i" style="width:20px;height:20px"></div>`, 40, 0, 20, 20},
		// Guards: already right.
		{"safe center does not overflow the start edge", "", `<div id="i" style="justify-self:safe center;width:141px;height:20px"></div>`, 0, 0, 141, 20},
		{"auto margins are safe", "", `<div id="i" style="margin-left:auto;width:141px;height:20px"></div>`, 0, 0, 141, 20},
		{"center with a margin", "", `<div id="i" style="justify-self:center;width:21px;height:20px;margin-left:3px"></div>`, 41, 0, 21, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div id="g" style="display:grid;grid-template-columns:100px;grid-template-rows:100px;width:100px;`+
				c.grid+`">`+c.item+`</div>`)
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
