package layout

import "testing"

// A block container's first baseline is its first line box with content;
// an empty line box (one that only places a float) gives none, so the next
// line's baseline is the block's. Mirrors PropagateBaselineFromLineBox
// (block_layout_algorithm.cc:3700-3730 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// which skips IsEmptyLineBox. The block is the only item of the column
// inline-flex #c, which exports its item's first baseline (an inline-block
// would export its last) and follows an "X" and a 30px inline-block, so #c
// sits 30 - baseline below the line top; want is from Chrome headless.
func TestBlockFirstLineBaseline(t *testing.T) {
	cases := []struct {
		name, item string
		y          float64
	}{
		{"a full-width float pushes the text below it",
			`<span style="float:left;width:40px;height:5px"></span>X`, 17},
		{"a full-width float, a space, then text",
			`<span style="float:left;width:40px;height:5px"></span> X`, 17},
		{"a line holding only a <br> has a baseline",
			`<span></span><br>X`, 22},
		{"control: one line", `X`, 22},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// #c is 40px wide, so a 40px float leaves no room beside it.
			w, cb := layoutBaselineProbe(t, "", "column", `<div>`+c.item+`</div>`)
			if got := cb.Y - w.Y; got != c.y {
				t.Errorf("#c at %v, want %v", got, c.y)
			}
		})
	}
}
