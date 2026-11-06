package render_test

import (
	"fmt"
	"image/color"
	"testing"

	"louis14/pkg/render"
)

func isRed(c color.RGBA) bool { return c.R > 200 && c.G < 60 }

// TestClipNode_EscapingLayersInheritAncestorClips: every layer paints under
// the clip of its LocalBorderBoxProperties, the contents clip its
// containing-block chain builds, whichever stacking context's list paints
// it (FragmentPaintPropertyTreeBuilder::UpdateOverflowClip sets
// context_.current.clip for descendants, UpdateLocalBorderBoxContext records
// it, core/paint/paint_property_tree_builder.cc:3137-3138, :2756-2779 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The clip is in the parent's
// transform space, so a transformed child is clipped before its transform.
// LOU-612.
func TestClipNode_EscapingLayersInheritAncestorClips(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:50px;height:50px;overflow:hidden;margin-top:60px">%s</div></body></html>`
	const red = `width:50px;height:50px;background:red`
	for _, tc := range []struct{ name, child string }{
		{"flex item with z-index", `<div style="display:flex"><div style="z-index:1;margin-top:-40px;` + red + `"></div></div>`},
		{"transformed relpos z-index", `<div style="position:relative;z-index:1;transform:translateY(-40px);` + red + `"></div>`},
		{"masked relpos z-index", `<div style="position:relative;z-index:1;top:-40px;mask-image:linear-gradient(black,black);` + red + `"></div>`},
		{"filtered relpos z-index", `<div style="position:relative;z-index:1;top:-40px;filter:brightness(1);` + red + `"></div>`},
		{"isolated relpos z-index", `<div style="position:relative;z-index:1;top:-40px;` + red + `"><div style="mix-blend-mode:multiply;width:10px;height:10px"></div></div>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := render.RenderPage(t, fmt.Sprintf(page, tc.child), 100, 150)
			// (10, 40) is above the clipping box (which starts at y=60).
			if c := img.RGBAAt(10, 40); isRed(c) {
				t.Errorf("pixel (10,40) = %v: the child escaped the ancestor overflow clip", c)
			}
			if c := img.RGBAAt(30, 62); !isRed(c) {
				t.Errorf("pixel (30,62) = %v, want the red child inside the clip", c)
			}
		})
	}
}

// TestClipNode_InlineOpacityGroupIsClipped: the paint layer of an inline
// with group opacity inherits the clip of the block that contains it.
func TestClipNode_InlineOpacityGroupIsClipped(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:50px;height:50px;overflow:hidden"><span style="opacity:0.99">` +
		`<span style="display:inline-block;width:100px;height:20px;background:red"></span>` +
		`</span></div></body></html>`
	img := render.RenderPage(t, src, 150, 100)
	if c := img.RGBAAt(70, 10); isRed(c) {
		t.Errorf("pixel (70,10) = %v: the opacity group escaped the overflow clip", c)
	}
	if c := img.RGBAAt(30, 10); !isRed(c) {
		t.Errorf("pixel (30,10) = %v, want the red inline-block inside the clip", c)
	}
}

// TestClipNode_RelposAutoPaintsAtStep6InsideClip: a relatively positioned
// z-index:auto box paints at CSS 2.1 Appendix E step 6, above a later
// in-flow block's background, even when an overflow clip contains it —
// Blink collects it into the stacking context's z-order list
// (PaintLayerPaintOrderIterator) and clips it through its clip node, not by
// painting it in DOM order.
func TestClipNode_RelposAutoPaintsAtStep6InsideClip(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:50px;height:100px;overflow:hidden">` +
		`<div style="position:relative;width:50px;height:50px;background:red"></div>` +
		`<div style="margin-top:-50px;width:50px;height:50px;background:lime"></div>` +
		`</div></body></html>`
	img := render.RenderPage(t, src, 100, 150)
	if c := img.RGBAAt(10, 10); !isRed(c) {
		t.Errorf("pixel (10,10) = %v, want the relpos box painted over the later block", c)
	}
}
