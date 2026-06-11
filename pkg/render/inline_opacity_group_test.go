package render

import (
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// layoutAndBuildPaintTree runs the production parse -> layout -> BuildPaintTree
// pipeline and returns the root PaintLayer alongside the root box.
func layoutAndBuildPaintTree(t *testing.T, content string) (*PaintLayer, *layout.Box) {
	t.Helper()
	doc, err := html.ParseWithFetcher(content, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eng := layout.NewLayoutEngine(800, 600)
	boxes := eng.Layout(doc)
	if len(boxes) == 0 {
		t.Fatal("layout produced no boxes")
	}
	return BuildPaintTree(boxes[0]), boxes[0]
}

// forEachLayer visits every layer in the paint tree, passing the chain of
// ancestor layers (root first) for each visited layer.
func forEachLayer(l *PaintLayer, ancestors []*PaintLayer, visit func(layer *PaintLayer, ancestors []*PaintLayer)) {
	visit(l, ancestors)
	chain := append(append([]*PaintLayer{}, ancestors...), l)
	for _, list := range [][]*PaintLayer{l.NegativeZ, l.FlowChildren, l.FloatChildren, l.AutoZero, l.PositiveZ} {
		for _, c := range list {
			forEachLayer(c, chain, visit)
		}
	}
}

// CSS Color 3 §3.2 / Blink LayoutObject::PaintingLayer (layout_object.cc:1218
// @ 4883d11f): a float whose nearest self-painting ancestor is an inline with
// opacity must paint inside that inline's opacity group. With opacity:0 the
// float must be fully suppressed, so its layer needs an ancestor layer with
// Opacity == 0 in the paint tree.
func TestPaintTree_FloatInsideOpacityInline_PaintsInsideGroup(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div style="width: 100px; height: 100px; background: green">
  <span style="opacity: 0">
    <div style="width: 100px; height: 100px; float: left; background: red"></div>
  </span>
</div>`)

	foundFloat := false
	forEachLayer(root, nil, func(l *PaintLayer, ancestors []*PaintLayer) {
		if l.Box == nil || !isFloat(l.Box) {
			return
		}
		foundFloat = true
		for _, a := range ancestors {
			if a.Opacity == 0 {
				return // correctly grouped
			}
		}
		t.Errorf("float layer has no Opacity==0 ancestor: it escapes the inline's opacity group (ancestor opacities: %v)", opacities(ancestors))
	})
	if !foundFloat {
		t.Fatal("no float layer found in paint tree")
	}
}

func opacities(layers []*PaintLayer) []float64 {
	out := make([]float64, len(layers))
	for i, l := range layers {
		out[i] = l.Opacity
	}
	return out
}

// CSS Color 3 §3.2: opacity is GROUP opacity over the element — all fragments
// of a multi-line inline (per-line background fragments and text runs) must
// composite as ONE group with a single alpha application. Blink: one PaintLayer
// per LayoutBoxModelObject, painting all its fragments together. Exactly one
// layer in the whole paint tree may carry the span's 0.4 opacity.
func TestPaintTree_MultiLineInlineOpacity_SingleGroup(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div style="line-height: 0.5em; font-family: monospace"><span style="opacity: 0.4; background: blue; color: blue">XXXXX<br/>XXXXX</span></div>`)

	var groups int
	forEachLayer(root, nil, func(l *PaintLayer, ancestors []*PaintLayer) {
		if l.Opacity < 1.0 {
			groups++
		}
	})
	if groups != 1 {
		t.Errorf("expected exactly 1 opacity group layer for the span element, got %d", groups)
	}
}

// CSS 2.1 §10.6.1: the content area of a non-replaced inline is based on the
// FONT (em box), not on line-height. Blink: InlineBoxState::ComputeTextMetrics
// (inline_box_state.cc:105-132 @ 4883d11f) takes ascent/descent from
// GetFontHeight; line-height only affects line box stacking. With
// line-height:0 the span's background fragment must still be em-box tall,
// vertically coincident with the text fragment's em box.
func TestInlineBackgroundFragment_HeightFromFontMetrics(t *testing.T) {
	_, root := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div style="line-height: 0; font-family: monospace"><span style="background: blue; color: blue">XXXXX</span></div>`)

	var bgBox, textBox *layout.Box
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b.Node != nil && b.Node.TagName == "span" && b.Style != nil {
			if b.Text != "" {
				textBox = b
			} else {
				bgBox = b
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)

	if bgBox == nil || textBox == nil {
		t.Fatalf("missing span boxes: bg=%v text=%v", bgBox != nil, textBox != nil)
	}
	if bgBox.Height != textBox.Height {
		t.Errorf("span background height %v != text em-box height %v (must come from font metrics, not line-height)", bgBox.Height, textBox.Height)
	}
	if bgBox.Y != textBox.Y {
		t.Errorf("span background top %v != text em-box top %v", bgBox.Y, textBox.Y)
	}
}
