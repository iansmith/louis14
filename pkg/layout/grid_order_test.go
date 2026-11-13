package layout

import "testing"

// Grid items are placed in order-modified document order: when any child has
// a non-initial order, the items are stable-sorted by it before placement.
// Mirrors GridNode::ConstructGridItems and GridItems::SortByOrderProperty
// (grid/grid_node.cc:28-112, grid/grid_item.cc:470-478 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Want offsets (relative to the
// grid) from Chrome headless.
func TestGridItemsPlacedInOrderModifiedDocumentOrder(t *testing.T) {
	type pos struct {
		id   string
		x, y float64
	}
	cases := []struct {
		name, columns, items string
		want                 []pos
	}{
		{"reversed by order", "repeat(4,auto)",
			`<div id="a" style="width:10px;order:99"></div><div id="b" style="width:20px;order:98"></div><div id="c" style="width:30px;order:97"></div>`,
			[]pos{{"a", 50, 0}, {"b", 30, 0}, {"c", 0, 0}}},
		{"negative and positive order", "repeat(4,auto)",
			`<div id="a" style="width:10px"></div><div id="b" style="width:20px;order:-1"></div><div id="c" style="width:30px"></div><div id="d" style="width:40px;order:1"></div>`,
			[]pos{{"a", 20, 0}, {"b", 0, 0}, {"c", 30, 0}, {"d", 60, 0}}},
		{"equal orders keep document order", "repeat(2,auto)",
			`<div id="a" style="width:10px;order:1"></div><div id="b" style="width:20px;order:1"></div><div id="c" style="width:30px"></div>`,
			[]pos{{"a", 30, 0}, {"b", 0, 5}, {"c", 0, 0}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0}#g>div{height:5px}</style><div id="g" style="display:grid;grid:auto/`+
				c.columns+`;width:max-content">`+c.items+`</div>`)
			g := findBoxByID(r, "g")
			if g == nil {
				t.Fatal("no #g box")
			}
			for _, p := range c.want {
				b := findBoxByID(r, p.id)
				if b == nil {
					t.Fatalf("no #%s box", p.id)
				}
				if x, y := b.X-g.X, b.Y-g.Y; x != p.x || y != p.y {
					t.Errorf("#%s at %v,%v, want %v,%v", p.id, x, y, p.x, p.y)
				}
			}
		})
	}
}
