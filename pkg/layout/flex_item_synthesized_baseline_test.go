package layout

import "testing"

// A baseline-aligned flex item with no baseline in its baseline writing
// mode gets a synthesized one in any container, in the container's font
// baseline: its center for a central baseline, else its block-end, or its
// block-start in flipped lines (of the item's baseline writing mode). The
// baseline writing mode is DetermineBaselineWritingMode's, so a column
// flex's items synthesize in the writing mode orthogonal to theirs. Mirrors
// FlexLayoutAlgorithm::BaselineAscent / SynthesizedBaselineAscent
// (flex/flex_layout_algorithm.cc:375-420, :1190 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) with LogicalBoxFragment::
// SynthesizedBaseline. Each container holds a baseline-aligned item and an
// empty one (#e); want is #e's offset from the container, from Chrome
// headless.
func TestFlexItemSynthesizedBaseline(t *testing.T) {
	const row = `<div style="block-size:30px;padding-block-start:7px">X</div>` +
		`<div id="e" style="block-size:16px;inline-size:10px;border-block-end:2px solid"></div>`
	const column = `<div style="inline-size:30px;block-size:10px"></div><div id="e" style="inline-size:16px;block-size:10px"></div>`
	cases := []struct {
		name, style, items string
		dx, dy             float64
	}{
		{"vertical-rl row, central", "writing-mode:vertical-rl;height:60px;align-items:baseline", row, 16, 10},
		{"vertical-rl row, alphabetic", "writing-mode:vertical-rl;text-orientation:sideways;height:60px;align-items:baseline", row, 22, 10},
		{"vertical-lr row, central", "writing-mode:vertical-lr;height:60px;align-items:baseline", row, 3, 10},
		{"vertical-lr row, alphabetic, flipped lines", "writing-mode:vertical-lr;text-orientation:sideways;height:60px;align-items:baseline", row, 9, 10},
		{"vertical-rl row, last baseline", "writing-mode:vertical-rl;height:60px;align-items:last baseline", row, 16, 10},
		{"vertical-lr row, alphabetic, last baseline", "writing-mode:vertical-lr;text-orientation:sideways;height:60px;align-items:last baseline", row, 9, 10},
		{"horizontal column, last baseline", "flex-direction:column;width:60px;align-items:last baseline", column, 30, 10},
		{"horizontal column, first baseline", "flex-direction:column;width:60px;align-items:baseline", column, 0, 10},
		{"vertical-rl column, central", "writing-mode:vertical-rl;flex-direction:column;height:60px;align-items:baseline", column, 0, 7},
		{"vertical-rl column, alphabetic", "writing-mode:vertical-rl;text-orientation:sideways;flex-direction:column;height:60px;align-items:baseline", column, 0, 14},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}</style>`+
				`<div id="c" style="display:flex;float:left;`+c.style+`">`+c.items+`</div>`)
			cb, e := findBoxByID(r, "c"), findBoxByID(r, "e")
			if cb == nil || e == nil {
				t.Fatal("no #c or #e box")
			}
			if dx, dy := e.X-cb.X, e.Y-cb.Y; dx != c.dx || dy != c.dy {
				t.Errorf("#e at (%v, %v), want (%v, %v)", dx, dy, c.dx, c.dy)
			}
		})
	}
}
