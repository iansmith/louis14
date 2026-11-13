package layout

import "testing"

// A float that does not fit beside the content before it on the line is
// positioned after the line, and the line goes on past it:
// ShouldPushFloatAfterLine (inline/line_breaker.cc:3673-3712) leaves it
// unpositioned and InlineLayoutAlgorithm::PlaceFloatingObjects
// (inline/inline_layout_algorithm.cc:852-906) places it at the line's
// block-end; a later float on the line follows it (HasUnpositionedFloats,
// line_breaker.cc:240-243). Whether it fits is measured against the line
// opportunity (AvailableFloatInlineSize), which nowrap does not widen. All @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestFloatPushedAfterLineKeepsTheLine(t *testing.T) {
	const float = `<span id="f" style="float:right;width:5ch;height:5ch"></span>`
	cases := []struct {
		name, div, body string
		fx, fy, nx, ny  float64
	}{
		// "aaaaaaaa " is 9ch: the 5ch float does not fit in 10ch, but
		// "aaaaaaaa b" does, so b stays on the line.
		{"wrapping line", "", `aaaaaaaa ` + float + ` <span id="n">b</span>`, 50, 10, 90, 0},
		// The nowrap line is unbounded for its text, not for the float.
		{"nowrap line", "white-space:nowrap;", `text that overflows ` + float + ` my <span id="n">parent.</span>`, 50, 10, 230, 0},
		// A second float that would fit still follows the pushed one.
		{"later float follows", "", `aaaaaaaa <span style="float:right;width:5ch;height:5ch"></span>` +
			`<span id="f" style="float:left;width:1ch;height:1ch"></span> <span id="n">b</span>`, 0, 10, 90, 0},
		// Guard: a float that fits is placed on the line ("aaaa b" fits
		// beside the 4ch float).
		{"float fits", "", `aaaa <span id="f" style="float:right;width:4ch;height:4ch"></span> <span id="n">b</span>`, 60, 0, 50, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="`+c.div+`width:10ch;font:10px/1 Ahem">`+c.body+`</div>`)
			f, n := r["f"], r["n"]
			if len(f) != 1 || len(n) != 1 || f[0].x != c.fx || f[0].y != c.fy || n[0].x != c.nx || n[0].y != c.ny {
				t.Errorf("#f %+v, #n %+v; want #f at (%v,%v), #n at (%v,%v)", f, n, c.fx, c.fy, c.nx, c.ny)
			}
		})
	}
}
