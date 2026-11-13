package layout

import "testing"

// Whether the line may wrap is the style's own (LineBreaker::auto_wrap_,
// set by SetCurrentStyleForce from ComputedStyle::ShouldWrapLine,
// inline/line_breaker.cc:419-424, :4549-4553): nowrap text is never broken
// inside (HandleText adds it whole, :1478-1495), a break opportunity is one
// only where the style in effect wraps (ComputeCanBreakAfter with
// auto_wrap_, HandleOpenTag :3987-3996), and an atomic inline in nowrap
// content has none after it (CanBreakAfterAtomicInline, :1168-1173). All @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestLineBreakerAutoWrapPerStyle(t *testing.T) {
	const nw = `<span style="white-space:nowrap">`
	cases := []struct {
		name, div, body string
		x, y, w         float64
	}{
		// "Some " breaks; the nowrap run then overflows its own line.
		{"nowrap span mid-paragraph", "", `Some ` + nw + `text that <span id="n">overflows</span> my parent.</span>`, 100, 10, 90},
		// The space is inside the nowrap span: no opportunity after "Some".
		{"nowrap space glues to the word before", "", `Some` + nw + ` text <span id="n">that</span></span>`, 100, 0, 40},
		// No opportunity before the inline-block inside nowrap content.
		{"atomic inline in nowrap", "", `Some ` + nw + `texttext<span id="n" style="display:inline-block;width:5ch;height:1ch"></span></span>`, 80, 10, 50},
		// Guard: a nowrap block stays one line.
		{"nowrap block", "white-space:nowrap;", `aaaa <span id="n">bbbb</span> cccc dddd`, 50, 0, 40},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="`+c.div+`width:10ch;font:10px/1 Ahem">`+c.body+`</div>`)
			n := r["n"]
			if len(n) != 1 || n[0].x != c.x || n[0].y != c.y || n[0].w != c.w {
				t.Errorf("#n %+v; want one fragment at (%v,%v) width %v", n, c.x, c.y, c.w)
			}
		})
	}
	t.Run("min-content keeps a nowrap run whole", func(t *testing.T) {
		c := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div id="c" style="width:min-content;font:10px/1 Ahem">aa `+
			nw+`bb cc</span> dd</div>`), "c")
		if c == nil {
			t.Fatal("#c not found")
		}
		if c.Width != 50 {
			t.Errorf("min-content = %v, want 50 (bb cc)", c.Width)
		}
	})
}

// A line that overflows the opportunity beside floats moves to the next
// opportunity, below them, only when the container wraps
// (InlineLayoutAlgorithm::Layout's check of Node().Style().ShouldWrapLine(),
// inline/inline_layout_algorithm.cc:1330-1336 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a nowrap or pre line stays
// beside them and overflows.
func TestNoWrapLineStaysBesideFloats(t *testing.T) {
	cases := []struct{ name, body string }{
		// css-ui/text-overflow-006: a float leading the pre line.
		{"leading float", `<div style="white-space:pre;width:10ch;font:10px/1 Ahem"><span style="float:right;width:4ch;height:2ch"></span><span id="n">PASS</span> FAIL</div>`},
		// A float from before the block narrows the line.
		{"earlier float", `<div style="float:left;width:4ch;height:2ch;font:10px/1 Ahem"></div><div style="white-space:nowrap;width:10ch;font:10px/1 Ahem"><span id="n">PASS</span> FAIL</div>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+c.body)["n"]
			if len(n) != 1 || n[0].y != 0 {
				t.Errorf("#n %+v; want it on the first line, y 0", n)
			}
		})
	}
}
