package layout

import (
	"testing"

	"louis14/pkg/html"
)

// TestAbsposWithoutPositionedAncestor_InitialContainingBlock pins that an
// absolutely positioned box with no positioned ancestor is placed against
// the initial containing block — the LayoutView, whose rect is the viewport
// at the canvas origin — not the root element's content or padding box.
// Blink: LayoutObject::ContainerForAbsolutePosition walks up to the nearest
// CanContainAbsolutePositionObjects ancestor, which for such a box is the
// LayoutView (core/layout/layout_object.cc:1752 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the LayoutView's
// OutOfFlowLayoutPart uses InitialContainingBlockFixedSize
// (core/layout/out_of_flow_layout_part.cc:531 @ a9f50e52).
// LOU-560 (worklist C2): background-origin-006 has html{padding-left:170px}
// and an abspos div with left:0, which Chrome puts at x=0.
func TestAbsposWithoutPositionedAncestor_InitialContainingBlock(t *testing.T) {
	src := `<!DOCTYPE html><html style="padding-left:170px;padding-top:30px;border-left:5px solid">` +
		`<body><div id=abs style="position:absolute;left:0;top:0;width:60px;height:60px"></div></body>`
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	var abs *Box
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Node != nil {
			if id, _ := b.Node.GetAttribute("id"); id == "abs" {
				abs = b
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
		walk(b)
	}
	if abs == nil {
		t.Fatal("no abspos box")
	}
	if abs.X != 0 || abs.Y != 0 {
		t.Errorf("abspos border-box origin = (%v, %v), want (0, 0) — the initial containing block", abs.X, abs.Y)
	}
}
