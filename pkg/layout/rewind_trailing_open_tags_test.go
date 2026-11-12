package layout

import "testing"

// LOU-885. Open tags are trailable while a line breaks, but a line never
// ends with them: LineBreaker::RewindTrailingOpenTags moves trailing open
// tags, and so an inline box's start edge, to the next line, except after a
// forced break (inline/line_breaker.cc:2536-2568 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each edged span is 3px margin
// + 5px border + 6px padding at the start and 10px padding + 8px border +
// 4px margin at the end: 36px around its content.
const rewindEdges = `margin:0 4px 0 3px;border:solid gray;border-width:0 8px 0 5px;padding:0 10px 0 6px`

func TestRewindTrailingOpenTags(t *testing.T) {
	const box = `<span style="display:inline-block;width:20px;height:10px"></span>`

	t.Run("layout: the second span starts the next line with its start edge", func(t *testing.T) {
		src := `<!DOCTYPE html><body style="margin:0"><div style="width:80px;font-size:0">` +
			`<span style="` + rewindEdges + `">` + box + `</span><span style="font-size:16px"> </span>` +
			`<span id="t" style="` + rewindEdges + `">` + box + `</span></div>`
		r := oofPageRects(t, src)["t"]
		if len(r) != 1 || r[0].x != 3 || r[0].w != 49 {
			t.Errorf("second span fragments = %+v, want one at x=3, w=49 (no start-edge fragment left on line 1)", r)
		}
	})

	t.Run("min-content: the start edge goes with the span's content", func(t *testing.T) {
		src := `<!DOCTYPE html><body style="margin:0"><div id="c" style="width:min-content;font:10px/1 Ahem">a ` +
			`<span style="` + rewindEdges + `">` + box + `</span></div>`
		b := findBoxByID(layoutRootTestHTML(t, src), "c")
		if b == nil {
			t.Fatal("no #c box")
		}
		if b.Width != 56 {
			t.Errorf("min-content width = %v, want 56 (the span's 36px of edges + 20px)", b.Width)
		}
	})

	t.Run("guard: after a forced break the open tag stays on the line", func(t *testing.T) {
		src := `<!DOCTYPE html><body style="margin:0"><div style="font:10px/1 Ahem">` +
			`<span style="` + rewindEdges + `">aaa</span><span id="t" style="` + rewindEdges + `"><br>aaa</span></div>`
		r := oofPageRects(t, src)["t"]
		onLine2 := false
		for _, f := range r[1:] {
			onLine2 = onLine2 || f.y == 10
		}
		if len(r) < 2 || r[0].y != 0 || r[0].w != 11 || !onLine2 {
			t.Errorf("second span fragments = %+v, want its 11px start edge on line 1 and its text on line 2", r)
		}
	})
}

// A line that ends where an opening punctuation mark was retracted (LB14,
// retractTrailingOpenPunct) must not end with the open tag before it.
func TestRewindTrailingOpenTagsAfterOpenPunctRetract(t *testing.T) {
	src := `<!DOCTYPE html><body style="margin:0"><div style="width:70px;font:10px/1 Ahem">foo ` +
		`<span id="t" style="` + rewindEdges + `">( bar</span></div>`
	r := oofPageRects(t, src)["t"]
	if len(r) == 0 || r[0].y == 0 {
		t.Errorf("span fragments = %+v, want none on line 1 (its open tag moves with the retracted \"(\")", r)
	}
}

// Rewinding trailing open tags leaves the line's width alone: the open tags
// only advanced the line breaker's position, never the line's width.
func TestRewindTrailingOpenTagsKeepsLineWidth(t *testing.T) {
	const aaa = `<span id="a">aaa</span> <span style="` + rewindEdges + `">bbb</span>`
	cases := []struct {
		align, body string
		wantX       float64
	}{
		// "aaa" (30px) alone on line 1 of a 70px line.
		{"right", aaa, 40},
		{"center", aaa, 20},
		// "a a" (30px) justified to 70px: the second "a" ends the line.
		{"justify", `a <span id="a">a</span> <span style="` + rewindEdges + `">bbbb</span> c`, 60},
	}
	for _, c := range cases {
		t.Run(c.align, func(t *testing.T) {
			src := `<!DOCTYPE html><body style="margin:0"><div style="width:70px;font:10px/1 Ahem;text-align:` + c.align + `">` + c.body + `</div>`
			r := oofPageRects(t, src)["a"]
			if len(r) != 1 || r[0].x != c.wantX {
				t.Errorf("#a fragments = %+v, want one at x=%v", r, c.wantX)
			}
		})
	}

	t.Run("min-content: line 1 is the widest line", func(t *testing.T) {
		src := `<!DOCTYPE html><body style="margin:0"><div id="c" style="width:min-content;font:10px/1 Ahem">aaaaaa ` +
			`<span style="` + rewindEdges + `">b</span></div>`
		b := findBoxByID(layoutRootTestHTML(t, src), "c")
		if b == nil {
			t.Fatal("no #c box")
		}
		if b.Width != 60 {
			t.Errorf("min-content width = %v, want 60 (\"aaaaaa\"; line 2 is 46)", b.Width)
		}
	})
}
