package render_test

import (
	"fmt"
	"testing"
)

// TestOverflowClip_RelativeAtomicInlineIsClipped: an `overflow: hidden`
// box clips every descendant whose containing-block chain passes through it
// (CSS 2.1 §11.1.1), including a relatively positioned atomic inline that
// paints from the stacking context's z-order lists. In Blink the descendant
// inherits the OverflowClip paint property node through its
// LocalBorderBoxProperties regardless of paint order
// (FragmentPaintPropertyTreeBuilder::UpdateOverflowClip /
// UpdateLocalBorderBoxContext, core/paint/paint_property_tree_builder.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). LOU-609.
func TestOverflowClip_RelativeAtomicInlineIsClipped(t *testing.T) {
	const container = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:50px;height:50px;overflow:hidden;margin-top:60px;display:%s">%s</div></body></html>`
	for _, tc := range []struct {
		name, display, child string
	}{
		{"block relpos div (control)", "block", `<div style="position:relative;top:-40px;width:50px;height:50px;background:red"></div>`},
		{"relpos inline-block in a block", "block", `<span style="position:relative;top:-40px;display:inline-block;width:50px;height:50px;background:red"></span>`},
		{"relpos inline-block in an inline-block", "inline-block", `<span style="position:relative;top:-40px;display:inline-block;width:50px;height:50px;background:red"></span>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf(container, tc.display, tc.child)
			img := renderToImage(t, src, 100, 150)
			// (10, 40) is above the container (which starts at y=60): the
			// child shifted up by 40px must not paint there.
			if c := img.RGBAAt(10, 40); c.R > 200 && c.G < 60 {
				t.Errorf("pixel (10,40) = %v: the relatively positioned child escaped the overflow clip", c)
			}
			// Inside the container the child still paints.
			if c := img.RGBAAt(10, 62); !(c.R > 200 && c.G < 60) {
				t.Errorf("pixel (10,62) = %v, want the red child inside the clip", c)
			}
		})
	}
}
