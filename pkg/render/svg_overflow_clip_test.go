package render

import "testing"

// An outermost inline <svg> respects CSS overflow like other replaced
// elements (Element::IsReplacedElementRespectingCSSOverflow,
// core/dom/element.cc:13749-13762), so it clips along the axes its overflow
// gives (LayoutSVGRoot::ComputeOverflowClipAxes →
// LayoutBox::ComputeOverflowClipAxes, core/layout/svg/layout_svg_root.cc:
// 541-565, core/layout/layout_box.cc:3948-3961), and applies
// overflow-clip-margin only when neither axis is visible
// (LayoutObject::ShouldApplyOverflowClipMargin, core/layout/
// layout_object.h:588-617), all @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
// svg.css gives it overflow: hidden and overflow-clip-margin: content-box
// (LOU-878).
func TestPaint_InlineSVGClipsAlongItsOverflowAxes(t *testing.T) {
	for _, tt := range []struct {
		name, style          string
		clipX, clipY, margin bool
	}{
		{"UA hidden", "", true, true, true},
		{"x clip, y visible", "overflow-x:clip; overflow-y:visible", true, false, false},
		{"y clip, x visible", "overflow-y:clip; overflow-x:visible", false, true, false},
		{"visible", "overflow:visible", false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := layoutForPaintPredicateTest(t, `<!DOCTYPE html><body style="margin:0">`+
				`<svg id="s" style="width:100px; height:100px; `+tt.style+`"><rect width="150" height="150"/></svg></body>`)
			s := findBoxByID(root, "s")
			if s == nil {
				t.Fatal("no svg box")
			}
			l := newPaintLayer(s)
			if l.ClipX != tt.clipX || l.ClipY != tt.clipY {
				t.Errorf("clip axes = (x %v, y %v), want (x %v, y %v)", l.ClipX, l.ClipY, tt.clipX, tt.clipY)
			}
			if l.HasClipMargin != tt.margin {
				t.Errorf("HasClipMargin = %v, want %v", l.HasClipMargin, tt.margin)
			}
		})
	}
}
