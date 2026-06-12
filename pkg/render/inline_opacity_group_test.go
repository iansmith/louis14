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

// countOpacityGroups counts the layers in the whole paint tree carrying a
// group opacity (Opacity < 1) — one per opacity inline.
func countOpacityGroups(root *PaintLayer) int {
	groups := 0
	forEachLayer(root, nil, func(l *PaintLayer, _ []*PaintLayer) {
		if l.Opacity < 1.0 {
			groups++
		}
	})
	return groups
}

// CSS Color 3 §3.2: opacity is GROUP opacity over the element — all fragments
// of a multi-line inline (per-line background fragments and text runs) must
// composite as ONE group with a single alpha application. Blink: one PaintLayer
// per LayoutBoxModelObject, painting all its fragments together. Exactly one
// layer in the whole paint tree may carry the span's 0.4 opacity.
func TestPaintTree_MultiLineInlineOpacity_SingleGroup(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div style="line-height: 0.5em; font-family: monospace"><span style="opacity: 0.4; background: blue; color: blue">XXXXX<br/>XXXXX</span></div>`)

	if groups := countOpacityGroups(root); groups != 1 {
		t.Errorf("expected exactly 1 opacity group layer for the span element, got %d", groups)
	}
}

// Comprehensive group-membership contract with FRACTIONAL opacity (0.4), so
// no skip-paint shortcut applies: one span carrying background + text + a
// float child. Exactly one group layer exists, it carries the span's alpha,
// and the span's bg fragments, text runs, and float are all Opacity-1
// DESCENDANTS of it (float via the group's FloatChildren, pinning Appendix E
// step-4-below-text order inside the group). No non-float div (the containing
// block, sibling content) may be dimmed by the group.
func TestPaintTree_InlineOpacityGroup_Membership(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div style="line-height: 0.5em; font-family: monospace"><span style="opacity: 0.4; background: blue; color: blue"><div style="width: 20px; height: 20px; float: left; background: red"></div>XXXXX<br/>XXXXX</span></div>
<div style="width: 100px; height: 100px; background: green"></div>`)

	var group *PaintLayer
	groups := 0
	forEachLayer(root, nil, func(l *PaintLayer, ancestors []*PaintLayer) {
		if l.Opacity < 1.0 {
			groups++
			group = l
		}
	})
	if groups != 1 {
		t.Fatalf("expected exactly 1 opacity group layer, got %d", groups)
	}
	if group.Opacity != 0.4 {
		t.Errorf("group opacity = %v, want 0.4", group.Opacity)
	}

	hasAncestor := func(ancestors []*PaintLayer, target *PaintLayer) bool {
		for _, a := range ancestors {
			if a == target {
				return true
			}
		}
		return false
	}

	var floatLayers, textLayers, bgLayers int
	forEachLayer(root, nil, func(l *PaintLayer, ancestors []*PaintLayer) {
		if l == group {
			return
		}
		switch {
		case l.Box != nil && isFloat(l.Box):
			floatLayers++
			if !hasAncestor(ancestors, group) {
				t.Errorf("float layer is not a descendant of the opacity group")
			}
		case l.Box != nil && l.Box.Text == "XXXXX":
			textLayers++
			if !hasAncestor(ancestors, group) {
				t.Errorf("text run layer is not a descendant of the opacity group")
			}
		case l.Box != nil && l.Box.Node != nil && l.Box.Node.TagName == "span" && l.Box.Text == "":
			bgLayers++
			if !hasAncestor(ancestors, group) {
				t.Errorf("span background fragment layer is not a descendant of the opacity group")
			}
		case l.Box != nil && l.Box.Node != nil && l.Box.Node.TagName == "div":
			if hasAncestor(ancestors, group) {
				t.Errorf("non-span div layer is wrongly dimmed by the opacity group")
			}
		}
	})
	if floatLayers != 1 {
		t.Errorf("expected 1 float layer, got %d", floatLayers)
	}
	if textLayers != 2 {
		t.Errorf("expected 2 text run layers, got %d", textLayers)
	}
	inFloatList := false
	for _, c := range group.FloatChildren {
		if c.Box != nil && isFloat(c.Box) {
			inFloatList = true
		}
	}
	if !inFloatList {
		t.Errorf("float must sit in the group's FloatChildren (Appendix E step 4, below the span's text)")
	}
}

// Nested opacity inlines: the inner group must be a DESCENDANT of the outer
// group so alphas multiply (0.5 * 0.25). Sibling groups hung flat off the
// block would composite the inner content at 0.25 instead of 0.125.
func TestPaintTree_NestedOpacityInlines_GroupsNest(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div><span style="opacity: 0.5">AA<span style="opacity: 0.25">BB</span>CC</span></div>`)

	var outer, inner *PaintLayer
	var innerAncestors []*PaintLayer
	forEachLayer(root, nil, func(l *PaintLayer, ancestors []*PaintLayer) {
		switch l.Opacity {
		case 0.5:
			outer = l
		case 0.25:
			inner = l
			innerAncestors = append([]*PaintLayer{}, ancestors...)
		}
	})
	if outer == nil || inner == nil {
		t.Fatalf("expected one 0.5 group and one 0.25 group (outer=%v inner=%v)", outer != nil, inner != nil)
	}
	found := false
	for _, a := range innerAncestors {
		if a == outer {
			found = true
		}
	}
	if !found {
		t.Error("inner opacity group is not nested inside the outer group — alphas will not multiply")
	}
}

// position:relative on the opacity span: emit() clones the span's style to
// reset position for the bg fragment, so group identity must be keyed on the
// NODE, not the style pointer. Still exactly one group containing bg + text.
func TestPaintTree_RelativeOpacitySpan_SingleGroup(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div><span style="opacity: 0.4; position: relative; background: blue">XX</span></div>`)

	if groups := countOpacityGroups(root); groups != 1 {
		t.Errorf("expected exactly 1 opacity group for a relative opacity span, got %d", groups)
	}
}

// Two sibling opacity spans must produce two independent groups, not share a
// singleton cache.
func TestPaintTree_SiblingOpacitySpans_TwoGroups(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div><span style="opacity: 0.4">AA</span> <span style="opacity: 0.4">BB</span></div>`)

	if groups := countOpacityGroups(root); groups != 2 {
		t.Errorf("expected 2 independent opacity groups for 2 sibling spans, got %d", groups)
	}
}

// CSS 2.1 §10.6.1: the content area of a non-replaced inline is based on the
// FONT (em box), not on line-height. Blink: InlineBoxState::ComputeTextMetrics
// (inline_box_state.cc:105-132 @ 4883d11f) takes ascent/descent from
// GetFontHeight; line-height only affects line box stacking. With
// line-height:0 the span's background fragment must still be em-box tall,
// vertically coincident with the text fragment's em box.
func TestInlineBackgroundFragment_HeightFromFontMetrics(t *testing.T) {
	// max(em box, line box). line-height:0 collapses the line box below the
	// em box, so the band must be the em box, coincident with the text. A
	// large line-height grows the band to the line box, still covering the
	// text em box vertically.
	collapsed := func(b *layout.Box) (bg, text *layout.Box) {
		var walk func(b *layout.Box)
		walk = func(b *layout.Box) {
			if b.Node != nil && b.Node.TagName == "span" && b.Style != nil {
				if b.Text != "" {
					text = b
				} else {
					bg = b
				}
			}
			for _, c := range b.Children {
				walk(c)
			}
		}
		walk(b)
		return
	}

	t.Run("line-height:0 collapses to em box", func(t *testing.T) {
		_, root := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div style="line-height: 0; font-family: monospace"><span style="background: blue; color: blue">XXXXX</span></div>`)
		bgBox, textBox := collapsed(root)
		if bgBox == nil || textBox == nil {
			t.Fatalf("missing span boxes: bg=%v text=%v", bgBox != nil, textBox != nil)
		}
		if textBox.Height <= 0 {
			t.Fatalf("text em-box height %v must be positive", textBox.Height)
		}
		if bgBox.Height != textBox.Height {
			t.Errorf("with line-height:0 the band must be the em box %v, got %v", textBox.Height, bgBox.Height)
		}
		if bgBox.Y != textBox.Y {
			t.Errorf("span background top %v != text em-box top %v", bgBox.Y, textBox.Y)
		}
	})

	t.Run("large line-height grows to line box and covers the text", func(t *testing.T) {
		_, root := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div style="line-height: 2em; font-family: monospace"><span style="background: blue; color: blue">XXXXX</span></div>`)
		bgBox, textBox := collapsed(root)
		if bgBox == nil || textBox == nil {
			t.Fatalf("missing span boxes: bg=%v text=%v", bgBox != nil, textBox != nil)
		}
		if bgBox.Height <= textBox.Height {
			t.Errorf("with line-height:2em the band must fill the line box (> em box %v), got %v", textBox.Height, bgBox.Height)
		}
		if bgBox.Y > textBox.Y || bgBox.Y+bgBox.Height < textBox.Y+textBox.Height {
			t.Errorf("band [%v,%v] must cover the text em box [%v,%v]", bgBox.Y, bgBox.Y+bgBox.Height, textBox.Y, textBox.Y+textBox.Height)
		}
	})
}

// CSS Compositing 1 §8: a mix-blend-mode child inside an inline opacity group
// must isolate against the GROUP (its nearest stacking context), not some
// outer SC. Regression guard for the paint-group branch running the same
// blend/backdrop bookkeeping as the ordinary descendant walk.
func TestPaintTree_BlendInsideOpacityGroup_IsolatesGroup(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<div><span style="opacity: 0.5">A<span style="mix-blend-mode: multiply; display: inline-block; width: 10px; height: 10px; background: red"></span></span></div>`)

	var group *PaintLayer
	forEachLayer(root, nil, func(l *PaintLayer, _ []*PaintLayer) {
		if l.Opacity == 0.5 {
			group = l
		}
	})
	if group == nil {
		t.Fatal("no opacity group layer found")
	}
	if !group.HasBlendingDescendant {
		t.Error("opacity group must be marked HasBlendingDescendant for its blended member (CSS Compositing §8)")
	}
}
