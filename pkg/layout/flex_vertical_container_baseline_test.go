package layout

import "testing"

// A vertical flex container with no baseline-aligned items still exports
// a baseline: its first item's synthesized one, at the item's center for a
// central font baseline, else its block-end, or block-start in flipped
// lines. Mirrors BaselineAccumulator (flex/flex_layout_algorithm.cc:80-153,
// constructed with Style().GetFontBaseline()) and
// LogicalBoxFragment::SynthesizedBaseline (logical_box_fragment.h:33-40 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). want is #c's offset from the
// line's left edge, from Chrome headless.
func TestFlexVerticalContainerSynthesizedBaseline(t *testing.T) {
	cases := []struct {
		name, mode string
		x          float64
	}{
		{"vertical-rl central", "writing-mode:vertical-rl", 0},
		{"vertical-rl alphabetic", "writing-mode:vertical-rl;text-orientation:sideways", 0},
		{"vertical-lr central", "writing-mode:vertical-lr", 4.5},
		{"vertical-lr alphabetic, flipped lines", "writing-mode:vertical-lr;text-orientation:sideways", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, cb := layoutBaselineProbe(t, "height:100px;"+c.mode, "column",
				`<div style="block-size:20px;border-block-end:1px solid"></div><div style="block-size:19px"></div>`)
			if got := cb.X - w.X; got != c.x {
				t.Errorf("#c at %v, want %v", got, c.x)
			}
		})
	}
}
