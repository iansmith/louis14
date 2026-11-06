package layout

import "testing"

// An orthogonal atomic inline contributes its block size to its line's
// min/max-content: LineBreaker measures it with
// ComputeMinAndMaxContentContribution (inline/line_breaker.cc:3182-3194 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which lays an orthogonal child
// out. So a horizontal 200x100 img inline in a vertical-lr inline-block
// gives the inline-block an inline size (height) of 100 + 2 border.
// Mirrors WPT css-writing-modes/img-intrinsic-size-contribution-002.
func TestOrthogonalAtomicInlineContributesBlockSize(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><html style="writing-mode: vertical-lr;"><body style="margin:0">`+
		`<div id=i style="display:inline-block; border: 1px solid orange;"><img src="x.png" style="writing-mode: horizontal-tb; width:200px; height:100px"></div></body></html>`, nil)
	if i := findBox(fl.root, byID("i")); i == nil || i.Height != 102 {
		t.Errorf("inline-block %s, want height 102", boxDims(i))
	}
}
