package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestLhUnit_LineHeightResolvesAgainstParent: lh in line-height resolves
// against the parent's computed line-height (CSS Values 4 §6.1.1), as in
// font-size. Blink builds CSSToLengthConversionData's LineHeightSize from the
// parent style and only switches to the element's own after applying
// line-height (StyleCascade::Apply → StyleResolverState::UpdateLineHeight,
// style_cascade.cc:309-315 @ 5992657f556bcd9d207a4aca187488462f027cab).
// WPT css-values/lh-unit-001.html.
func TestLhUnit_LineHeightResolvesAgainstParent(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			div { font-size: 50px; line-height: 1; }
			aside { font-size: 42px; line-height: 2lh; }
			span { font-size: 10px; }
		</style>
		<div><aside><span>x</span></aside></div>
	`)
	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		switch node.TagName {
		case "aside", "span":
			// span inherits aside's computed (absolute) line-height.
			if got := style.GetLineHeight(); got != 100 {
				t.Errorf("<%s> line-height = %v, want 100 (2 × parent's 50px)", node.TagName, got)
			}
		}
	}
}

// TestLineHeight_InheritsComputedLength: a length, percentage or calc()
// line-height computes to an absolute length, which descendants inherit;
// only a bare number inherits as a multiplier (CSS 2.1 §10.8.1). Mirrors
// Blink's StyleBuilderConverter::ConvertLineHeight
// (style_builder_converter.cc:2212 @ 5992657f556bcd9d207a4aca187488462f027cab).
func TestLineHeight_InheritsComputedLength(t *testing.T) {
	for _, tc := range []struct {
		lineHeight string
		want       float64 // span's line-height; div is 10px, span 40px
	}{
		{"2em", 20},
		{"200%", 20},
		{"calc(1em + 10px)", 20},
		{"calc(50% + 15px)", 20},
		{"2", 80},
		{"25px", 25},
	} {
		doc, _ := html.Parse(`<style>div { font-size: 10px; line-height: ` + tc.lineHeight +
			`; } span { font-size: 40px; }</style><div><span>x</span></div>`)
		for node, style := range ApplyStylesToDocument(doc, 800, 600) {
			if node.TagName == "span" {
				if got := style.GetLineHeight(); got != tc.want {
					t.Errorf("line-height %s: span = %v, want %v", tc.lineHeight, got, tc.want)
				}
			}
		}
	}
}

// TestLineHeight_VarFontShorthandInheritsComputedLength: a line-height set by
// a var()-deferred `font` shorthand (expanded after the main cascade, in
// reexpandVarShorthands) is absolutized too.
func TestLineHeight_VarFontShorthandInheritsComputedLength(t *testing.T) {
	doc, _ := html.Parse(`<style>div { --f: 10px/2em serif; font: var(--f); }
		span { font-size: 40px; }</style><div><span>x</span></div>`)
	for node, style := range ApplyStylesToDocument(doc, 800, 600) {
		if node.TagName == "span" {
			if got := style.GetLineHeight(); got != 20 {
				t.Errorf("span line-height = %v, want 20 (div's 2em at 10px)", got)
			}
		}
	}
}

// TestLineHeight_PseudoElementLhResolvesAgainstOriginating: lh in a
// pseudo-element's line-height resolves against the originating element's
// line-height, its inheritance parent.
func TestLineHeight_PseudoElementLhResolvesAgainstOriginating(t *testing.T) {
	stylesheet, err := ParseStylesheet(`
		p { font-size: 10px; line-height: 30px; }
		p::before { content: "x"; font-size: 40px; line-height: 2lh; }
	`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}
	node := &html.Node{Type: html.ElementNode, TagName: "p"}
	originating := ComputeStyle(node, stylesheets, 800, 600, nil)
	before := ComputePseudoElementStyle(node, "before", stylesheets, 800, 600, originating)
	if got := before.GetLineHeight(); got != 60 {
		t.Errorf("::before line-height = %v, want 60 (2 × p's 30px)", got)
	}
}

// TestLineHeight_SyntheticRootInheritsComputedLength: in a fragment parse
// (no <html>), :root's line-height reaches doc.Root's other children through
// the synthetic root style, which must carry the computed length too.
func TestLineHeight_SyntheticRootInheritsComputedLength(t *testing.T) {
	for _, tc := range []struct {
		rootFontSize string
		want         float64 // p's line-height: :root's 2em
	}{
		{"10px", 20},
		{"62.5%", 20}, // of the initial 16px
	} {
		doc := html.NewDocument()
		doc.Stylesheets = append(doc.Stylesheets, html.StylesheetSource{
			Text: ":root { font-size: " + tc.rootFontSize + "; line-height: 2em; } p { font-size: 40px; }"})
		first := &html.Node{Type: html.ElementNode, TagName: "div", Parent: doc.Root}
		p := &html.Node{Type: html.ElementNode, TagName: "p", Parent: doc.Root}
		doc.Root.Children = append(doc.Root.Children, first, p)

		style := ApplyStylesToDocument(doc, 800, 600)[p]
		if got := style.GetLineHeight(); got != tc.want {
			t.Errorf(":root font-size %s: p line-height = %v, want %v", tc.rootFontSize, got, tc.want)
		}
	}
}
