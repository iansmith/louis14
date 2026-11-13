package layout

import "testing"

// An inline box with unicode-bidi: isolate wraps its content in bidi
// controls that are opaque to whitespace collapsing: a collapsible space
// after a box's trailing or before its leading control still collapses with
// a space on the other side. Blink appends the controls with AppendOpaque,
// whose items are kOpaqueToCollapsing, and LastItemToCollapseWith skips them
// (inline/inline_items_builder.cc:216-226, :1356-1380 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). want is each box's x within
// its 300px parent and its width, from Chrome 157 headless.
func TestIsolateBidiControlsOpaqueToCollapsing(t *testing.T) {
	r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}.p{width:300px}`+
		`.i{display:inline;unicode-bidi:isolate}</style>`+
		"<div class=p>\n  <div class=i id=a>AB </div>\n  <div class=i id=b>CD </div>\n  <div class=i id=c>EF</div>\n</div>"+
		`<div class=p><span class=i>AB </span> <span class=i id=d>CD</span></div>`+
		`<div class=p><span class=i>AB</span> <span class=i id=e> CD</span></div>`+
		`<div class=p><span>AB </span><span class=i id=f> CD</span></div>`)
	for _, c := range []struct {
		id   string
		x, w float64
	}{{"a", 0, 30}, {"b", 30, 30}, {"c", 60, 20}, {"d", 30, 20}, {"e", 30, 20}, {"f", 30, 20}} {
		b := findBoxByID(r, c.id)
		if b == nil {
			t.Fatalf("no #%s box", c.id)
		}
		if b.X != c.x || b.Width != c.w {
			t.Errorf("#%s x=%v w=%v, want x=%v w=%v", c.id, b.X, b.Width, c.x, c.w)
		}
	}
}
