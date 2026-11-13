package layout

import "testing"

// An atomic inline exports its own baseline only when its writing mode
// equals the line's, and a baseline of 0 (at its block-start) is a real
// one, not "none". Mirrors LogicalBoxFragment::BaselineMetrics
// (logical_box_fragment.cc:14-83), which reads std::optional FirstBaseline /
// LastBaseline, both nullopt unless IsWritingModeEqual
// (logical_box_fragment.h:28-31, :42-70 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each case puts #c on a line
// after an Ahem "X" and a 30px inline-block in wrapper #w; want is #c's
// offset from #w and #w's size, from Chrome headless. A last baseline of 0
// is real too: BoxFragmentBuilder::last_baseline_ is optional
// (box_fragment_builder.h:620-636), set by every line box with content
// (block_layout_algorithm.cc:3700-3730 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestAtomicInlineBaseline(t *testing.T) {
	cases := []struct {
		name, wStyle, child   string
		dx, dy, width, height float64
	}{
		{"vertical-lr inline-block in a vertical-rl line synthesizes",
			"height:100px;writing-mode:vertical-rl",
			`<div id="c" style="display:inline-block;writing-mode:vertical-lr;inline-size:40px;block-size:20px">X</div>`,
			5, 10, 30, 100},
		{"vertical-rl inline-block in a vertical-rl line keeps its baseline",
			"height:100px;writing-mode:vertical-rl",
			`<div id="c" style="display:inline-block;writing-mode:vertical-rl;inline-size:40px;block-size:20px">X</div>`,
			0, 10, 30, 100},
		{"vertical-rl inline-block in a vertical-lr line synthesizes",
			"height:100px;writing-mode:vertical-lr",
			`<div id="c" style="display:inline-block;writing-mode:vertical-rl;inline-size:40px;block-size:20px">X</div>`,
			5, 10, 30, 100},
		{"horizontal inline-block whose only line puts its last baseline at 0",
			"width:100px",
			`<div id="c" style="display:inline-block;width:40px;height:20px"><div style="font-size:0;line-height:0">X</div></div>`,
			10, 30, 100, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}</style>`+
				`<div id="w" style="`+c.wStyle+`">X<span style="display:inline-block;block-size:30px"></span>`+c.child+`</div>`)
			w, cb := findBoxByID(r, "w"), findBoxByID(r, "c")
			if w == nil || cb == nil {
				t.Fatal("no #w or #c box")
			}
			if dx, dy := cb.X-w.X, cb.Y-w.Y; dx != c.dx || dy != c.dy {
				t.Errorf("#c at (%v, %v), want (%v, %v)", dx, dy, c.dx, c.dy)
			}
			if w.Width != c.width || w.Height != c.height {
				t.Errorf("#w %vx%v, want %vx%v", w.Width, w.Height, c.width, c.height)
			}
		})
	}
}
