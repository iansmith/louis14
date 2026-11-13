package layout

import "testing"

// Baseline-aligned flex items fall into two baseline-sharing groups: the
// major group aligns from the line's cross-start, the minor group (an item
// whose baseline writing mode starts at the cross-end, or the other of
// first/last baseline under wrap-reverse) from its cross-end, each with
// its own shared baseline. Mirrors DetermineBaselineGroup
// (baseline_utils.h:50-89) as FlexLayoutAlgorithm uses it
// (flex/flex_layout_algorithm.cc:375-420, :1190-1195, :1540-1555,
// :1947-1956 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Items a and b
// share the container's writing mode; x's is xMode (the container's in the
// control). want is each item's offset from the container, and its width
// (the cross size both groups' extents set), from Chrome headless.
func TestFlexBaselineGroups(t *testing.T) {
	type pos struct{ x, y float64 }
	cases := []struct {
		name, style, xMode string
		a, b, x            pos
		width              float64
	}{
		{"control: vertical-rl row, one group", "writing-mode:vertical-rl;height:60px;align-items:baseline", "vertical-rl",
			pos{0, 0}, pos{0, 10}, pos{0, 20}, 17},
		{"vertical-rl row, a vertical-lr item", "writing-mode:vertical-rl;height:60px;align-items:baseline", "vertical-lr",
			pos{0, 0}, pos{0, 10}, pos{0, 20}, 17},
		{"vertical-rl row, last baseline", "writing-mode:vertical-rl;height:60px;align-items:last baseline", "vertical-lr",
			pos{0, 0}, pos{0, 10}, pos{2, 20}, 17},
		{"vertical-rl row, wrap-reverse", "writing-mode:vertical-rl;height:60px;align-items:baseline;flex-wrap:wrap-reverse", "vertical-lr",
			pos{0, 0}, pos{0, 10}, pos{2, 20}, 17},
		{"vertical-lr row, a vertical-rl item", "writing-mode:vertical-lr;height:60px;align-items:baseline", "vertical-rl",
			pos{0, 0}, pos{4, 10}, pos{2, 20}, 17},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}</style>`+
				`<div id="c" style="display:flex;float:left;`+c.style+`">`+
				`<div id="a" style="padding-block-start:7px">X</div>`+
				`<div id="b" style="padding-block-start:3px">X</div>`+
				`<div id="x" style="writing-mode:`+c.xMode+`;padding-block-start:5px">X</div></div>`)
			cb := findBoxByID(r, "c")
			if cb == nil {
				t.Fatal("no #c box")
			}
			for _, it := range []struct {
				id   string
				want pos
			}{{"a", c.a}, {"b", c.b}, {"x", c.x}} {
				b := findBoxByID(r, it.id)
				if b == nil {
					t.Fatalf("no #%s box", it.id)
				}
				if got := (pos{b.X - cb.X, b.Y - cb.Y}); got != it.want {
					t.Errorf("#%s at %v, want %v", it.id, got, it.want)
				}
			}
			if cb.Width != c.width {
				t.Errorf("#c width %v, want %v", cb.Width, c.width)
			}
		})
	}
}
