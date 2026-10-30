package css

import (
	"strings"
	"testing"

	"louis14/pkg/html"
)

// fakeFontSelector is a deterministic stand-in for text.CSSFontSelector. Its
// ratios differ from every fallback (ex 0.5em, ch 0.5em, ic 1em, cap 0.7em) so
// a value that came from the font cannot be mistaken for a fallback.
// font-family "b" has a 0.3em x-height; "noic" has no 水 glyph; "zeroic" has
// a 水 glyph with a zero advance.
type fakeFontSelector struct{}

func (fakeFontSelector) CreateFont(s *Style) Font {
	fs := s.GetFontSize()
	f := Font{AdjustedSize: fs, ZeroWidth: 0.6 * fs, XHeight: 0.4 * fs, CapHeight: 0.8 * fs,
		IdeographicInlineSize: 0.9 * fs, HasIdeographicInlineSize: true}
	switch fam, _ := s.Get("font-family"); strings.TrimSpace(fam) {
	case "b":
		f.XHeight = 0.3 * fs
	case "noic":
		f.IdeographicInlineSize, f.HasIdeographicInlineSize = 0, false
	case "zeroic":
		f.IdeographicInlineSize = 0
	}
	return f
}

// styleByTag returns the style of the first element named tag.
func styleByTag(t *testing.T, styles map[*html.Node]*Style, tag string) *Style {
	t.Helper()
	for node, style := range styles {
		if node.TagName == tag {
			return style
		}
	}
	t.Fatalf("no <%s> style", tag)
	return nil
}

// TestLineHeight_FontMetricUnitsInheritComputedLength (LOU-428): a line-height
// using ex / ch / ic / cap computes to px against the element's own font, so a
// 40px child inherits the 10px parent's length instead of re-resolving it.
// Blink's ConvertLineHeight turns every length into Length::Fixed
// (core/css/resolver/style_builder_converter.cc:2212) with FontSizes::Ex / Ch /
// Ic / Cap read from the element's font (core/css/css_to_length_conversion_data.cc
// :95/117/139/169), which StyleCascade::Apply updates just before line-height
// (core/css/resolver/style_cascade.cc:310-315), all @
// 5992657f556bcd9d207a4aca187488462f027cab.
func TestLineHeight_FontMetricUnitsInheritComputedLength(t *testing.T) {
	for _, tc := range []struct {
		family, lineHeight string
		want               float64 // div is 10px with the fake font's metrics
	}{
		{"a", "calc(1em + 1ex)", 14},  // x-height 0.4em
		{"a", "calc(1em + 1ch)", 16},  // "0" advance 0.6em
		{"a", "calc(1em + 1ic)", 19},  // 水 advance 0.9em
		{"a", "calc(1em + 1cap)", 18}, // cap-height 0.8em
		{"a", "2ex", 8},
		{"noic", "calc(1em + 1ic)", 20},   // FontSizes::Ic falls back to 1em
		{"zeroic", "calc(1em + 1ic)", 10}, // a present zero 水 advance gives ic = 0
	} {
		doc, _ := html.Parse(`<style>div { font-family: ` + tc.family + `; font-size: 10px; line-height: ` +
			tc.lineHeight + `; } span { font-size: 40px; }</style><div><span>x</span></div>`)
		styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
		div, span := styleByTag(t, styles, "div"), styleByTag(t, styles, "span")
		if got := div.GetLineHeight(); got != tc.want {
			t.Errorf("%s line-height %s: div = %v, want %v", tc.family, tc.lineHeight, got, tc.want)
		}
		if got := span.GetLineHeight(); got != tc.want {
			t.Errorf("%s line-height %s: span = %v, want the div's %v", tc.family, tc.lineHeight, got, tc.want)
		}
		if raw := div.Properties["line-height"]; !strings.HasSuffix(raw, "px") {
			t.Errorf("%s line-height %s: div computed value %q, want a px length", tc.family, tc.lineHeight, raw)
		}
	}
}

// TestLineHeight_FontMetricUnitsUseOwnFont (LOU-428): each element builds its
// own font, so a child with a different family uses that family's x-height,
// not the parent's (Blink FontBuilder::CreateFont per element,
// core/css/resolver/font_builder.cc:656 @ 5992657f556bcd9d207a4aca187488462f027cab).
func TestLineHeight_FontMetricUnitsUseOwnFont(t *testing.T) {
	doc, _ := html.Parse(`<style>div { font-size: 10px; }
		span { font-family: b; font-size: 10px; line-height: 2ex; }</style><div><span>x</span></div>`)
	styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
	if got := styleByTag(t, styles, "span").GetLineHeight(); got != 6 {
		t.Errorf("span line-height = %v, want 6 (2 × family b's 0.3em x-height at 10px)", got)
	}
}

// TestLineHeight_PseudoElementFontMetricUnits (LOU-428): a pseudo-element
// builds its font through the same cascade step as an element, from the
// originating element's font selector.
func TestLineHeight_PseudoElementFontMetricUnits(t *testing.T) {
	doc, _ := html.Parse(`<style>div { font-size: 10px; }
		div::before { content: "x"; font-size: 20px; line-height: 2ex; }</style><div>y</div>`)
	styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
	var divNode *html.Node
	for node := range styles {
		if node.TagName == "div" {
			divNode = node
		}
	}
	before := ComputePseudoElementStyle(divNode, "before", ParseDocumentStylesheets(doc), 800, 600, styles[divNode])
	if got := before.GetLineHeight(); got != 16 {
		t.Errorf("::before line-height = %v, want 16 (2 × 0.4em x-height at 20px)", got)
	}
	if before.XHeight != 8 || !before.HasZeroWidth || before.LhSize != 16 {
		t.Errorf("::before metrics XHeight=%v HasZeroWidth=%v LhSize=%v, want 8 true 16",
			before.XHeight, before.HasZeroWidth, before.LhSize)
	}
}

// TestLineHeight_ContainerRuleReresolvesFont (LOU-428): an @container rule that
// changes font-size re-builds the font before line-height is absolutized again.
func TestLineHeight_ContainerRuleReresolvesFont(t *testing.T) {
	doc, _ := html.Parse(`<style>@container (min-width: 100px) { p { font-size: 20px; line-height: 2ex; } }</style>
		<div style="container-type: inline-size; width: 500px; font-size: 10px"><p>x</p></div>`)
	styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
	p := styleByTag(t, styles, "p")
	if got := p.GetFontSize(); got != 20 {
		t.Fatalf("p font-size = %v, want 20 (the @container rule must match)", got)
	}
	if got := p.GetLineHeight(); got != 16 {
		t.Errorf("p line-height = %v, want 16 (2 × 0.4em x-height at 20px)", got)
	}
}

// TestLineHeight_ContainerRuleFontOnlyReresolvesLineHeight (LOU-428): an
// @container rule that changes only font-size still re-derives a line-height
// declared in an ordinary rule, since Blink applies @container rules inside the
// one cascade before UpdateFont and line-height (style_cascade.cc:310-315 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestLineHeight_ContainerRuleFontOnlyReresolvesLineHeight(t *testing.T) {
	doc, _ := html.Parse(`<style>p { line-height: 2ex; } @container (min-width: 100px) { p { font-size: 20px; } }</style>
		<div style="container-type: inline-size; width: 500px; font-size: 10px"><p>x</p></div>`)
	styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
	p := styleByTag(t, styles, "p")
	if got := p.GetFontSize(); got != 20 {
		t.Fatalf("p font-size = %v, want 20 (the @container rule must match)", got)
	}
	if got := p.GetLineHeight(); got != 16 {
		t.Errorf("p line-height = %v, want 16 (2 × 0.4em x-height at the @container font's 20px)", got)
	}
}

// TestLineHeight_SyntheticRootFontMetricUnits (LOU-428): in a fragment parse,
// :root's ex line-height is computed against :root's font before it is copied
// onto the synthetic doc.Root style, which gets its own font metrics too.
func TestLineHeight_SyntheticRootFontMetricUnits(t *testing.T) {
	doc := html.NewDocument()
	doc.Stylesheets = append(doc.Stylesheets, html.StylesheetSource{
		Text: ":root { font-size: 10px; line-height: calc(1em + 1ex); } p { font-size: 40px; }"})
	first := &html.Node{Type: html.ElementNode, TagName: "div", Parent: doc.Root}
	p := &html.Node{Type: html.ElementNode, TagName: "p", Parent: doc.Root}
	doc.Root.Children = append(doc.Root.Children, first, p)

	styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
	if got := styles[p].GetLineHeight(); got != 14 {
		t.Errorf("p line-height = %v, want 14 (:root's 10px + 0.4em x-height)", got)
	}
	if root := styles[doc.Root]; root == nil || root.XHeight != 4 {
		t.Errorf("synthetic root style %+v, want XHeight 4", root)
	}
}

// TestTransitionsFirstFrame_ReresolvesFont (LOU-428): a first-frame font-size
// transition rewrites font-size after the cascade, so the element and the
// descendants it propagates to must re-build their fonts.
func TestTransitionsFirstFrame_ReresolvesFont(t *testing.T) {
	doc, _ := html.Parse(`<style>div { font-size: 20px; transition: font-size 1s; }</style><div><span>x</span></div>`)
	styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
	prev := make(map[*html.Node]*Style, len(styles))
	for node, style := range styles {
		c := style.Clone()
		if node.TagName == "div" || node.TagName == "span" {
			c.Properties["font-size"] = "10px"
		}
		prev[node] = c
	}
	ApplyTransitionsFirstFrame(doc.Root, styles, prev)
	div, span := styleByTag(t, styles, "div"), styleByTag(t, styles, "span")
	if got := div.GetFontSize(); got != 10 {
		t.Fatalf("div font-size = %v, want the transition's start value 10", got)
	}
	if div.XHeight != 4 || div.UsedFontSize != 10 {
		t.Errorf("div XHeight=%v UsedFontSize=%v, want 4 10 (the transitioned font)", div.XHeight, div.UsedFontSize)
	}
	if span.XHeight != 4 {
		t.Errorf("span XHeight = %v, want 4 (inherits the transitioned font-size)", span.XHeight)
	}
}

// TestTextDecorationInset_ChResolvedAtCascade (LOU-428): with the font built
// during the cascade, a ch text-decoration-inset resolves against the font's
// "0" advance when ResolveAppliedTextDecorations records it.
func TestTextDecorationInset_ChResolvedAtCascade(t *testing.T) {
	doc, _ := html.Parse(`<style>div { font-size: 10px; text-decoration: underline; text-decoration-inset: 2ch; }</style><div>x</div>`)
	styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
	div := styleByTag(t, styles, "div")
	if len(div.AppliedTextDecorations) == 0 {
		t.Fatal("div has no applied text decoration")
	}
	if got := div.AppliedTextDecorations[0].Inset; got != (TextDecorationInset{InlineStart: 12, InlineEnd: 12}) {
		t.Errorf("inset = %+v, want 12px each side (2 × 0.6em \"0\" advance at 10px)", got)
	}
}
