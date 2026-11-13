package layout

import "testing"

// When a span's end edge makes the line overflow and there is no break
// opportunity between the span's text and what follows (xx</span>b), the
// line breaks at the last opportunity before them: HandleOverflow walks the
// results back to the last one that can break after and fits
// (inline/line_breaker.cc:4069 ff.; can_break_after moves through a close
// tag only when it was set before it, HandleCloseTag :3999-4060), both @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestLineBreakerOverflowRewindsToBreakOpportunity(t *testing.T) {
	const span = `<span id="s" style="padding-right:20px">`
	cases := []struct {
		name, width, body string
		sx, sy, bx, by    float64
	}{
		// "aa xx" plus the 20px padding is 70 in 60: no opportunity
		// between "xx" and "b", so the line breaks after "aa ".
		{"no opportunity before the close tag", "60px", `aa ` + span + `xx</span><span id="b">b</span>`, 0, 10, 40, 10},
		// The opportunity is inside the text before b ("aa |xx"): that
		// text is broken again there, and "xx" moves down with b.
		{"opportunity inside the preceding text", "50px", `<span id="s">aa </span>xx<span id="b">b</span>`, 0, 0, 20, 10},
		// Guard: with a space before the close tag the opportunity moves
		// after it, and the 80px line ends there.
		{"opportunity before the close tag", "80px", `aa ` + span + `xx </span><span id="b">b</span>`, 30, 0, 0, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="width:`+c.width+`;font:10px/1 Ahem">`+c.body+`</div>`)
			s, b := r["s"], r["b"]
			if len(s) != 1 || len(b) != 1 || s[0].x != c.sx || s[0].y != c.sy || b[0].x != c.bx || b[0].y != c.by {
				t.Errorf("#s %+v, #b %+v; want #s at (%v,%v), #b at (%v,%v)", s, b, c.sx, c.sy, c.bx, c.by)
			}
		})
	}
	t.Run("min-content keeps xx</span>b together", func(t *testing.T) {
		c := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div id="c" style="width:min-content;font:10px/1 Ahem">aa `+
			span+`xx</span><span id="b">b</span></div>`), "c")
		if c == nil || c.Width != 50 {
			t.Errorf("min-content = %v, want 50 (xx + 20 + b)", c)
		}
	})
}
