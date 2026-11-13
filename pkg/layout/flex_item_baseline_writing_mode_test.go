package layout

import "testing"

// A flex container's fallback baseline uses an item's own baseline only
// when the item's writing mode equals the container's; otherwise the item
// has none in the container's writing mode and one is synthesized, even
// when both writing modes are vertical. Mirrors LogicalBoxFragment::
// FirstBaseline / LastBaseline, which return nullopt unless
// IsWritingModeEqual (logical_box_fragment.h:28-31, :42-70 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). want is #c's offset from the
// line's left edge and the line's width, from Chrome headless.
func TestFlexItemBaselineNeedsEqualWritingMode(t *testing.T) {
	cases := []struct {
		name, outer, item string
		x, width          float64
	}{
		{"vertical-lr item in vertical-rl", "writing-mode:vertical-rl", "writing-mode:vertical-lr", 0, 45},
		{"vertical-rl item in vertical-rl", "writing-mode:vertical-rl", "writing-mode:vertical-rl", 0, 50},
		{"vertical-lr item in sideways vertical-rl", "writing-mode:vertical-rl;text-orientation:sideways", "writing-mode:vertical-lr", 0, 50},
		{"vertical-rl item in sideways vertical-rl", "writing-mode:vertical-rl;text-orientation:sideways", "writing-mode:vertical-rl", 0, 62},
		{"vertical-rl item in vertical-lr", "writing-mode:vertical-lr", "writing-mode:vertical-rl", 5, 45},
		{"vertical-lr item in vertical-lr", "writing-mode:vertical-lr", "writing-mode:vertical-lr", 10, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, cb := layoutBaselineProbe(t, "height:100px;"+c.outer, "column",
				`<div style="block-size:20px;`+c.item+`">X</div><div style="block-size:20px"></div>`)
			if got := cb.X - w.X; got != c.x {
				t.Errorf("#c at %v, want %v", got, c.x)
			}
			if w.Width != c.width {
				t.Errorf("line width %v, want %v", w.Width, c.width)
			}
		})
	}
}
