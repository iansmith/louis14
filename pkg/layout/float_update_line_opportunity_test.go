package layout

import "testing"

// A float positioned while its line is broken narrows the rest of that
// line: LineBreaker::HandleFloat positions it and UpdateLineOpportunity
// recomputes the available width from the exclusion space
// (inline/line_breaker.cc:3727-3857); a rewind past it restores the
// exclusion space and the width (RewindFloats, :3860-3890). Both @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestFloatNarrowsTheRestOfItsLine(t *testing.T) {
	const float = `<span id="f" style="float:right;width:5ch;height:5ch"></span>`
	cases := []struct {
		name, body     string
		fx, fy, nx, ny float64
	}{
		// "aaaa " then the 5ch float fits in 10ch; " b" no longer fits
		// in the 5ch left, so b wraps beside the float.
		{"text after the float wraps", `aaaa ` + float + ` <span id="n">b</span>`, 50, 0, 0, 10},
		// Text that fits in what the float leaves stays on the line.
		{"text after the float fits", `aaa ` + float + `<span id="n">b</span>`, 50, 0, 40, 0},
		// "aaaaa " and the 5ch float are 11ch, but the space collapses
		// when the line breaks at the float, so it fits in 10ch
		// (ShouldPushFloatAfterLine's retry without the trailing space).
		{"trailing space collapses", `<span id="n">aaaaa</span> ` + float, 50, 0, 0, 0},
		// The 3ch float fits after "xx aa" and leaves 7ch; "bbb" is glued
		// to "aa" and no longer fits, so the line rewinds to after "xx ",
		// past the float, whose exclusion is undone; "aa", the float and
		// "bbb" go to the next line.
		{"rewind past the float", `xx aa<span id="f" style="float:right;width:3ch;height:3ch"></span><span id="n">bbb</span>`, 70, 10, 20, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="width:10ch;font:10px/1 Ahem">`+c.body+`</div>`)
			f, n := r["f"], r["n"]
			if len(f) != 1 || len(n) != 1 || f[0].x != c.fx || f[0].y != c.fy || n[0].x != c.nx || n[0].y != c.ny {
				t.Errorf("#f %+v, #n %+v; want #f at (%v,%v), #n at (%v,%v)", f, n, c.fx, c.fy, c.nx, c.ny)
			}
		})
	}
}
