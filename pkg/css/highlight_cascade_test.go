package css

// Tests for LOU-352's ComputeHighlightPseudoElementStyle: CSS Pseudo-4
// §highlight-cascade inheritance, mirroring Blink's highlight_parent model
// (Element::StyleForHighlightPseudoElement → StyleResolver::InitStyle's
// CreateNewClonedStyle(*parent_style) + the monotonic
// HasAuthorHighlightColors flag gating UseDefaultHighlightColors — all
// verified @ Chromium main 72259ecfb56eacf259e21783dd2b31608ac951a3; see
// Style.HasAuthorHighlightColors' doc comment for the exact citations).

import (
	"louis14/pkg/html"
	"testing"
)

func mustColorProp(t *testing.T, style *Style, property string) Color {
	t.Helper()
	val, ok := style.Get(property)
	if !ok {
		t.Fatalf("style has no %q", property)
	}
	c, ok := ParseColor(val)
	if !ok {
		t.Fatalf("style %q = %q, not a parseable color", property, val)
	}
	return c
}

// TestComputeHighlightPseudoElementStyle_InheritsAllPropertiesFromParent:
// with author colors on the parent highlight style and NO rules matching
// the child, the child style carries the parent's full property set
// (including the normally-non-inherited background-color) and the
// HasAuthorHighlightColors flag.
func TestComputeHighlightPseudoElementStyle_InheritsAllPropertiesFromParent(t *testing.T) {
	stylesheet, err := ParseStylesheet(`
		p::target-text { color: darkgrey; background-color: orange; }
	`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}
	p := &html.Node{Type: html.ElementNode, TagName: "p"}
	span := &html.Node{Type: html.ElementNode, TagName: "span", Parent: p}

	parent := ComputeHighlightPseudoElementStyle(p, "target-text", stylesheets, 800, 600, nil)
	if !parent.HasAuthorHighlightColors {
		t.Fatalf("parent.HasAuthorHighlightColors = false, want true (author set both colors)")
	}

	child := ComputeHighlightPseudoElementStyle(span, "target-text", stylesheets, 800, 600, parent)
	if got, want := mustColorProp(t, child, "color"), (Color{R: 169, G: 169, B: 169, A: 1}); got != want {
		t.Errorf("child color = %+v, want inherited darkgrey %+v", got, want)
	}
	if got, want := mustColorProp(t, child, "background-color"), (Color{R: 255, G: 165, B: 0, A: 1}); got != want {
		t.Errorf("child background-color = %+v, want inherited orange %+v", got, want)
	}
	if !child.HasAuthorHighlightColors {
		t.Errorf("child.HasAuthorHighlightColors = false, want true (monotonic through the highlight cascade)")
	}
}

// TestComputeHighlightPseudoElementStyle_BakedUADefaultsDoNotInherit: a
// parent highlight style WITHOUT author colors carries louis14's baked UA
// default pair — the child clone must exclude it (Blink never bakes:
// unauthored highlight colors stay unset until paint), so a child authoring
// only `color` gets the paired-cascade INITIAL background (transparent),
// not the parent's baked UA purple. Non-color properties (text-shadow)
// still inherit through the clone.
func TestComputeHighlightPseudoElementStyle_BakedUADefaultsDoNotInherit(t *testing.T) {
	stylesheet, err := ParseStylesheet(`
		p::target-text { text-shadow: 1px 1px red; }
		span::target-text { color: blue; }
	`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}
	p := &html.Node{Type: html.ElementNode, TagName: "p"}
	span := &html.Node{Type: html.ElementNode, TagName: "span", Parent: p}

	parent := ComputeHighlightPseudoElementStyle(p, "target-text", stylesheets, 800, 600, nil)
	if parent.HasAuthorHighlightColors {
		t.Fatalf("parent.HasAuthorHighlightColors = true, want false (no author colors, UA pair baked)")
	}

	child := ComputeHighlightPseudoElementStyle(span, "target-text", stylesheets, 800, 600, parent)
	if got, want := mustColorProp(t, child, "color"), (Color{R: 0, G: 0, B: 255, A: 1}); got != want {
		t.Errorf("child color = %+v, want its own authored blue %+v", got, want)
	}
	if got, want := mustColorProp(t, child, "background-color"), (Color{R: 0, G: 0, B: 0, A: 0}); got != want {
		t.Errorf("child background-color = %+v, want paired-cascade initial transparent %+v (NOT the parent's baked UA pair)", got, want)
	}
	if got, ok := child.Get("text-shadow"); !ok || got != "1px 1px red" {
		t.Errorf("child text-shadow = %q (ok=%v), want inherited %q", got, ok, "1px 1px red")
	}
	if !child.HasAuthorHighlightColors {
		t.Errorf("child.HasAuthorHighlightColors = false, want true (own authored color)")
	}
}
