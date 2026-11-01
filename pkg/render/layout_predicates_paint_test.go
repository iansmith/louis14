package render

import (
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// LOU-514: paint reads the layout predicates Blink's painters read off the
// layout object — LayoutObject::GetOverflowClipAxes (layout_object.h:
// 2058-2069) and IsMulticolContainer (:3438-3441 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) — forwarded from the layout
// fragments, instead of re-deriving them from style.

func layoutForPaintPredicateTest(t *testing.T, src string) *layout.Box {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	boxes := layout.NewLayoutEngine(200, 200).Layout(doc)
	if len(boxes) == 0 {
		t.Fatal("no boxes")
	}
	return boxes[0]
}

func findBoxByID(b *layout.Box, id string) *layout.Box {
	if b == nil {
		return nil
	}
	if b.Node != nil && b.Style != nil && b.Text == "" {
		if got, _ := b.Node.GetAttribute("id"); got == id {
			return b
		}
	}
	for _, c := range b.Children {
		if f := findBoxByID(c, id); f != nil {
			return f
		}
	}
	return nil
}

// A non-atomic inline does not respect overflow (LayoutObject::
// RespectsCSSOverflow is false for LayoutInline, layout_object.h:583-586),
// so overflow:hidden on a span clips nothing.
func TestPaint_InlineBoxOverflowDoesNotClip(t *testing.T) {
	root := layoutForPaintPredicateTest(t, `<!DOCTYPE html><body style="margin:0">`+
		`<span id="s" style="overflow:hidden; background:green">xx</span></body>`)
	span := findBoxByID(root, "s")
	if span == nil {
		t.Fatal("no span box")
	}
	if span.OverflowClipAxes != layout.NoOverflowClip {
		t.Errorf("span box OverflowClipAxes = %v, want none", span.OverflowClipAxes)
	}
	if hasOverflowClipping(span) {
		t.Error("hasOverflowClipping(span) = true, want false")
	}
	if newPaintLayer(span).HasClip {
		t.Error("span paint layer HasClip = true, want false")
	}
}

// A block clips along the axes layout computed: overflow-x:clip with
// overflow-y:visible clips only X.
func TestPaint_BlockOverflowClipAxesFromLayout(t *testing.T) {
	root := layoutForPaintPredicateTest(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div id="d" style="overflow-x:clip; width:50px; height:50px"></div></body>`)
	d := findBoxByID(root, "d")
	if d == nil {
		t.Fatal("no div box")
	}
	if d.OverflowClipAxes != layout.OverflowClipX {
		t.Errorf("div OverflowClipAxes = %v, want X only", d.OverflowClipAxes)
	}
	if l := newPaintLayer(d); !l.HasClip || !l.ClipX || l.ClipY {
		t.Errorf("div paint layer clip = (has %v, x %v, y %v), want X only", l.HasClip, l.ClipX, l.ClipY)
	}
}

// IsMulticolContainer is structural: a zero-height multicol container is
// one too, and its paint layer is a multicol layer.
func TestPaint_MulticolContainerFromLayout(t *testing.T) {
	root := layoutForPaintPredicateTest(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div id="mc" style="columns:2; height:0; column-rule:2px solid blue"></div></body>`)
	mc := findBoxByID(root, "mc")
	if mc == nil {
		t.Fatal("no multicol box")
	}
	if !mc.IsMulticolContainer {
		t.Error("zero-height multicol box IsMulticolContainer = false, want true")
	}
	if !newPaintLayer(mc).IsMulticol {
		t.Error("multicol paint layer IsMulticol = false, want true")
	}
}
