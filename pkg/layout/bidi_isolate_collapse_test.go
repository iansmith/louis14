package layout

import "testing"

// The bidi controls an element's unicode-bidi injects (LRI/RLI/FSI ... PDI,
// LRI+LRO ... PDF+PDI) are opaque to whitespace collapsing: a collapsible
// space next to them still collapses with one on the other side, and one
// ending the text before a block-in-inline is still removed. A control the
// author wrote is text and stops collapsing, and a space white-space
// preserves (pre-wrap) is not collapsible. Blink appends its controls with
// AppendOpaque, marking them kOpaqueToCollapsing (:1356-1380), and
// LastItemToCollapseWith skips such items (:216-226), both for
// AppendCollapseWhitespace (:844-848) and for AppendBlockInInline's
// RemoveTrailingCollapsibleSpaceIfExists (:1311, :1383-1391); a preserved
// space's item keeps the default kNotCollapsible end (inline_item.h:307).
// All in core/layout/inline/ @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
// want is #t's x within its 300px parent and its width, from Chrome 157
// headless, Ahem.
func TestBidiControlsOpaqueToCollapsing(t *testing.T) {
	for _, c := range []struct {
		name, content string
		x, w          float64
	}{
		{"isolates around inter-element whitespace",
			"\n  <div class=i>AB </div>\n  <div class=i>CD </div>\n  <div class=i id=t>EF</div>\n", 60, 20},
		{"space, PDI, space", `<span class=i>AB </span> <span class=i id=t>CD</span>`, 30, 20},
		{"PDI, space, LRI, space", `<span class=i>AB</span> <span class=i id=t> CD</span>`, 30, 20},
		{"space, LRI, space", `<span>AB </span><span class=i id=t> CD</span>`, 30, 20},
		{"rtl isolate", `<span class=i dir=rtl>a </span> <span id=t>b</span>`, 20, 10},
		{"isolate-override: PDF then PDI", `<span style="unicode-bidi:isolate-override">a </span> <span id=t>b</span>`, 20, 10},
		{"plaintext: FSI ... PDI", `<span style="unicode-bidi:plaintext">a </span> <span id=t>b</span>`, 20, 10},
		{"author-written PDI between spaces", `<span>a &#x2069;</span><span> <span id=t>b</span></span>`, 30, 10},
		{"pre-wrap space, then a collapsible space", `<span style="white-space:pre-wrap">a </span> <span id=t>b</span>`, 30, 10},
		{"pre-wrap space in an isolate, then a collapsible space", `<span class=i style="white-space:pre-wrap">a </span> <span id=t>b</span>`, 30, 10},
		{"collapsible space, then a pre-wrap one in an isolate", `a <span class=i style="white-space:pre-wrap"> <span id=t>b</span></span>`, 30, 10},
		{"trailing space before a block-in-inline, then PDI",
			`<span><span class=i id=t>a </span><div style="height:5px"></div></span>`, 0, 10},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><meta charset=utf-8><style>body{margin:0;font:10px/10px Ahem}`+
				`.p{width:300px}.i{display:inline;unicode-bidi:isolate}</style><div class=p>`+c.content+`</div>`)
			b := findBoxByID(r, "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.X != c.x || b.Width != c.w {
				t.Errorf("#t x=%v w=%v, want x=%v w=%v", b.X, b.Width, c.x, c.w)
			}
		})
	}
}
