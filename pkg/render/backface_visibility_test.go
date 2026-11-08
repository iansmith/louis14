package render_test

import (
	"fmt"
	"testing"

	"louis14/pkg/render"
)

// TestBackface_ParentPerspectiveRevealsBackFace: whether a flat element's
// back face is visible is decided on its transform to the screen, which
// includes its parent's perspective (cc IsTransformToRootOf3DRenderingContext
// BackFaceVisible walks to the root through nodes of rendering context 0,
// cc/trees/draw_property_utils.cc:432-463; the perspective node's children
// do not flatten it, core/paint/paint_property_tree_builder.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). rotateY(-60deg) at the far left
// of a perspective:100px parent shows the viewer its back face.
func TestBackface_ParentPerspectiveRevealsBackFace(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:400px;height:100px;perspective:100px">` +
		`<div style="width:50px;height:50px;background:red;transform:rotateY(-60deg);backface-visibility:%s"></div>` +
		`</div></body></html>`
	if n := countPixels(render.RenderPage(t, fmt.Sprintf(page, "visible"), 400, 100), isRed); n == 0 {
		t.Fatal("control: the backface-visibility:visible element painted nothing")
	}
	if n := countPixels(render.RenderPage(t, fmt.Sprintf(page, "hidden"), 400, 100), isRed); n != 0 {
		t.Errorf("%d red pixels: the hidden back face painted", n)
	}
}

// TestBackface_VisibleNodeUnderHiddenParentPaints: backface-visibility is
// inherited along transform nodes only while the parent allows it; a child
// transform node in its parent's preserve-3d context resets it to visible
// (UpdateTransform's backface_visibility, paint_property_tree_builder.cc:
// 1487-1508, can_inherit_backface_visibility :4126-4130 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so it paints although its
// parent's back face is hidden. Mirrors WPT
// css-transforms/composited-under-rotateY-180deg-preserve-3d.
func TestBackface_VisibleNodeUnderHiddenParentPaints(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="transform:rotateY(180deg);width:100px;backface-visibility:hidden;transform-style:preserve-3d">` +
		`<div style="width:100px;height:100px;background:red;will-change:transform"></div>` +
		`</div></body></html>`
	if n := countPixels(render.RenderPage(t, src, 200, 200), isRed); n == 0 {
		t.Error("the child with its own transform node did not paint")
	}
}

// TestBackface_HiddenInlineInRotatedContext: an inline box with
// backface-visibility:hidden gets its own transform node (NeedsTransform
// returns true for a hidden backface before its IsBox check,
// paint_property_tree_builder.cc:1330-1350 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so in a rotateY(180deg)
// preserve-3d context it shows its back face and is not painted.
func TestBackface_HiddenInlineInRotatedContext(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="transform:rotateY(180deg);transform-style:preserve-3d;width:100px">` +
		`<span style="backface-visibility:hidden;background:red">XXXX</span>` +
		`</div></body></html>`
	if n := countPixels(render.RenderPage(t, src, 200, 100), isRed); n != 0 {
		t.Errorf("%d red pixels: the inline's hidden back face painted", n)
	}
}

// TestBackface_HiddenInlineTextInRotatedContext: an inline's text, and the
// content of inline elements nested in it, paints in the inline's
// LocalBorderBoxProperties (Blink paints text in its LayoutInline's
// contents state), so a hidden inline back face culls its text too, not
// only its background fragments.
func TestBackface_HiddenInlineTextInRotatedContext(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="transform:rotateY(180deg);transform-style:preserve-3d;width:100px;font-size:20px">` +
		`<span style="backface-visibility:hidden;color:red">XXXX <b>YYYY</b></span>` +
		`</div></body></html>`
	if n := countPixels(render.RenderPage(t, src, 200, 100), isRed); n != 0 {
		t.Errorf("%d red pixels: the hidden inline's text painted", n)
	}
}
