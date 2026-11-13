package layout

import "testing"

// A vertical flex container with no baseline-aligned items still exports
// a baseline: its first item's synthesized one, at the item's center for a
// central font baseline, else its block-end, or block-start in flipped
// lines. Mirrors BaselineAccumulator (flex/flex_layout_algorithm.cc:80-153,
// constructed with Style().GetFontBaseline()) and
// LogicalBoxFragment::SynthesizedBaseline (logical_box_fragment.h:33-40 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 30px inline-block sets the
// line's extent on both sides of the baseline, so #c's offset from the
// line's left edge depends on the exported baseline; want is from Chrome
// headless. The flipped-lines case (a synthesized baseline of 0) is
// LOU-1015: the line layout takes an atomic inline's baseline of 0 for none.
func TestFlexVerticalContainerSynthesizedBaseline(t *testing.T) {
	cases := []struct {
		name, mode string
		x          float64
	}{
		{"vertical-rl central", "writing-mode:vertical-rl", 0},
		{"vertical-rl alphabetic", "writing-mode:vertical-rl;text-orientation:sideways", 0},
		{"vertical-lr central", "writing-mode:vertical-lr", 4.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}</style>`+
				`<div id="w" style="height:100px;`+c.mode+`">X<span style="display:inline-block;block-size:30px"></span>`+
				`<div id="c" style="display:inline-flex;flex-flow:column;inline-size:40px;block-size:40px">`+
				`<div style="block-size:20px;border-block-end:1px solid"></div><div style="block-size:19px"></div></div></div>`)
			w, cb := findBoxByID(r, "w"), findBoxByID(r, "c")
			if w == nil || cb == nil {
				t.Fatal("no #w or #c box")
			}
			if got := cb.X - w.X; got != c.x {
				t.Errorf("#c at %v, want %v", got, c.x)
			}
		})
	}
}
