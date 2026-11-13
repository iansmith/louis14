package layout

import "testing"

// A column flex container with no baseline-aligned items exports its first
// item's first-baseline-or-synthesized as its own baseline. The synthesized
// baseline sits at the item's border-box block-end in the container's
// writing mode, which in a column flex is the item's main size, not its
// cross (inline) size. Mirrors BaselineAccumulator::AccumulateItem
// (flex/flex_layout_algorithm.cc:91-101) via
// LogicalBoxFragment::FirstBaselineOrSynthesize
// (logical_box_fragment.h:53-59 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// #c sits 30 - baseline below the line top; want is from Chrome headless.
func TestFlexColumnContainerSynthesizedBaseline(t *testing.T) {
	cases := []struct {
		name, flow, first string
		y                 float64
	}{
		{"parallel first item", "column", `<div style="height:20px;border-bottom:1px solid"></div>`, 9},
		{"orthogonal first item", "column", `<div style="writing-mode:vertical-rl;width:40px;height:20px;border-bottom:1px solid"></div>`, 9},
		{"column-reverse starts at the last item", "column-reverse", `<div style="height:20px;border-bottom:1px solid"></div>`, 11},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, cb := layoutBaselineProbe(t, "", c.flow, c.first+`<div style="height:19px"></div>`)
			if got := cb.Y - w.Y; got != c.y {
				t.Errorf("#c at %v, want %v", got, c.y)
			}
		})
	}
}

// layoutBaselineProbe lays out a 40x40 inline-flex #c (flex-flow flow,
// children items) on a line of wrapper #w (wStyle appended to its style),
// after an Ahem "X" and a 30px inline-block. The inline-block sets the
// line's extent on both sides of the baseline, so #c's position on the
// line, and the line's size, depend on the baseline #c exports.
func layoutBaselineProbe(t *testing.T, wStyle, flow, items string) (w, c *Box) {
	t.Helper()
	r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}</style>`+
		`<div id="w" style="`+wStyle+`">X<span style="display:inline-block;block-size:30px"></span>`+
		`<div id="c" style="display:inline-flex;flex-flow:`+flow+`;inline-size:40px;block-size:40px">`+
		items+`</div></div>`)
	w, c = findBoxByID(r, "w"), findBoxByID(r, "c")
	if w == nil || c == nil {
		t.Fatal("no #w or #c box")
	}
	return w, c
}
