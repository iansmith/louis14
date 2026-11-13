package layout

import "testing"

// A shrink-to-fit scroll container's intrinsic inline size lays its
// orthogonal atomic out against its content block size less the auto
// horizontal scrollbar its layout found it needs: Blink's
// BlockNode::ComputeMinMaxSizes takes the container's geometry from
// CalculateInitialFragmentGeometry(is_intrinsic=true), whose ComputeScrollbars
// reads the box's PaintLayerScrollableArea (length_utils.cc:1543-1549,
// layout_box.cc:1577-1630 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// #c is 90px tall; the absolutely positioned #s overflows it
// horizontally, so it gets a 17px horizontal scrollbar and a 73px content
// block size. "aa bb cc dd ee" in 10px Ahem then sets two words per 73px
// column (three would need 80px): three columns, 30px wide — not the two
// 20px columns a 90px content block size gives. Chrome (17px classic
// scrollbars) gives #f and #c 30px, #o 30x73.
func TestOrthogonalIntrinsicSizeExcludesAutoScrollbar(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html>
<style>
body { margin: 0; font: 10px/10px Ahem; }
#f { float: left; }
#c { overflow: auto; height: 90px; position: relative; }
#o { display: inline-block; writing-mode: vertical-rl; vertical-align: top; }
#s { position: absolute; left: 200px; top: 0; width: 1px; height: 1px; }
</style>
<div id="f"><div id="c"><div id="o">aa bb cc dd ee</div><div id="s"></div></div></div>`)
	c, o := findBoxByID(root, "c"), findBoxByID(root, "o")
	if c == nil || o == nil {
		t.Fatal("no #c or #o box")
	}
	if o.Width != 30 {
		t.Errorf("#o width = %v, want 30 (three 73px columns)", o.Width)
	}
	if c.Width != 30 {
		t.Errorf("#c width = %v, want 30 (#o's three columns)", c.Width)
	}
}
