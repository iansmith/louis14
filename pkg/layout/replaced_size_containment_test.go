package layout

import "testing"

// Under size containment a replaced element's natural ratio is ignored, but
// the width/height attributes' `aspect-ratio: auto w / h` hint still applies
// (BlockNode::GetReplacedAspectRatio, block_node.cc:1329-1354 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a 100px-wide size-contained
// <img width=60 height=60> is 100px tall. Mirrors WPT
// css-contain/contain-size-replaced-007.
func TestSizeContainedImageUsesAttributeAspectRatio(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><img id=i src="x.png" width="60" height="60" style="width:100px; height:auto; contain:size; display:block"></body>`, nil)
	b := findBox(fl.root, byID("i"))
	if b == nil {
		t.Fatal("no box for the img")
	}
	if b.Width != 100 || b.Height != 100 {
		t.Errorf("size-contained img with width=60 height=60: %vx%v, want 100x100", b.Width, b.Height)
	}
}
