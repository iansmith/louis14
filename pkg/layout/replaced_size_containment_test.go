package layout

import "testing"

// imageFetcher serves a size x size PNG for any URL.
func imageFetcher(t *testing.T, size int) func(string) ([]byte, error) {
	data := onePNG(t, size, size)
	return func(string) ([]byte, error) { return data, nil }
}

// Under size containment a replaced element's natural ratio is ignored, but
// the width/height attributes' `aspect-ratio: auto w / h` hint still applies
// (BlockNode::GetReplacedAspectRatio, block_node.cc:1329-1354 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a 100px-wide size-contained
// <img width=60 height=60> of a 200x200 image is 100px tall. Mirrors WPT
// css-contain/contain-size-replaced-007.
func TestSizeContainedImageUsesAttributeAspectRatio(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><img id=i src="sq.png" width="60" height="30" style="width:100px; height:auto; contain:size; display:block"></body>`, imageFetcher(t, 200))
	if b := findBox(fl.root, byID("i")); b == nil || b.Width != 100 || b.Height != 50 {
		t.Errorf("size-contained img with width=60 height=30: %s, want 100x50", boxDims(b))
	}
}

// Without size containment `auto && <ratio>` yields to the natural ratio
// wherever the ratio is used: the attributes' 2:1 hint must not override a
// square image's 1:1 (css-sizing-4 aspect-ratio `auto && <ratio>`;
// BlockNode::GetReplacedAspectRatio, block_node.cc:1329-1354). Flex and grid
// items, and a video sized from its poster, all keep the natural ratio.
func TestAttributeAspectRatioYieldsToNaturalRatio(t *testing.T) {
	cases := []struct {
		name, body string
		w, h       float64
	}{
		{"flex item", `<div style="display:flex; align-items:flex-start"><img id=i src="sq.png" width="100" height="50" style="height:auto"></div>`, 100, 100},
		{"column flex item", `<div style="display:flex; flex-direction:column; align-items:flex-start"><img id=i src="sq.png" width="100" height="50" style="height:auto"></div>`, 100, 100},
		{"grid item", `<div style="display:grid; align-items:start; justify-items:start"><img id=i src="sq.png" width="100" height="50" style="height:auto"></div>`, 100, 100},
		{"block", `<img id=i src="sq.png" width="100" height="50" style="height:auto; display:block">`, 100, 100},
		{"video poster", `<video id=i poster="sq.png" width="320" height="180" style="width:320px; height:auto; display:block"></video>`, 320, 320},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">`+tc.body+`</body>`, imageFetcher(t, 200))
			if b := findBox(fl.root, byID("i")); b == nil || b.Width != tc.w || b.Height != tc.h {
				t.Errorf("%s: %s, want %vx%v", tc.name, boxDims(b), tc.w, tc.h)
			}
		})
	}
}

// A flex container's intrinsic inline size transfers a stretched row item's
// definite cross size through the item's ratio; for a replaced item that is
// its natural 1:1, not the attributes' 2:1 hint (`auto && <ratio>`), so the
// container is 100px wide, not 200px.
func TestFlexIntrinsicWidthUsesNaturalRatioOverAttributeHint(t *testing.T) {
	for _, container := range []string{
		`display:inline-flex; height:100px`,
		`display:flex; height:100px; width:min-content`,
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div id=c style="`+container+`"><img id=i src="sq.png" width="100" height="50" style="width:auto; height:auto"></div></body>`, imageFetcher(t, 200))
		if c, i := findBox(fl.root, byID("c")), findBox(fl.root, byID("i")); c == nil || i == nil || c.Width != 100 || i.Width != 100 || i.Height != 100 {
			t.Errorf("%s: container %s, img %s; want 100 wide, img 100x100", container, boxDims(c), boxDims(i))
		}
	}
}
