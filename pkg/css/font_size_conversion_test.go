package css

import (
	"strings"
	"testing"

	"louis14/pkg/html"
)

// styleByID returns the style of the element whose id attribute is id.
func styleByID(t *testing.T, styles map[*html.Node]*Style, id string) *Style {
	t.Helper()
	for node, style := range styles {
		if got, _ := node.GetAttribute("id"); got == id {
			return style
		}
	}
	t.Fatalf("no element with id %q", id)
	return nil
}

// cascadeHTML parses src and cascades it at an 800x600 viewport with fs
// (nil = no fonts).
func cascadeHTML(t *testing.T, src string, fs FontSelector) map[*html.Node]*Style {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	return ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fs)
}

// lengthOf returns s's property resolved as a length, failing if it does not
// resolve.
func lengthOf(t *testing.T, s *Style, property string) float64 {
	t.Helper()
	v, ok := s.GetLength(property)
	if !ok {
		raw, _ := s.Get(property)
		t.Fatalf("%s %q does not resolve to a length", property, raw)
	}
	return v
}

// TestFontSize_InitialResetsToMedium (D1): `font-size: initial` is medium
// (16px), not the inherited size. Blink's FontBuilder::InitialSize is
// FontSizeFunctions::InitialKeywordSize() (core/css/resolver/font_builder.h
// :119-122, core/css/font_size_functions.h:75 @
// 5992657f556bcd9d207a4aca187488462f027cab). WPT css-values/calc-rem-lang.
func TestFontSize_InitialResetsToMedium(t *testing.T) {
	styles := cascadeHTML(t, `<style>#p { font-size: 32px; } #c { font-size: initial; } #g { width: 1em; }</style>
		<div id="p"><div id="c"><div id="g"></div></div></div>`, nil)
	if got := styleByID(t, styles, "c").GetFontSize(); got != 16 {
		t.Errorf("font-size: initial under 32px = %v, want 16 (medium)", got)
	}
	if got := lengthOf(t, styleByID(t, styles, "g"), "width"); got != 16 {
		t.Errorf("grandchild width 1em = %v, want 16", got)
	}
}

// TestFontSize_ViewportUnitsComputedAtCascade (D2, D10): vw / vh font-sizes
// resolve against the real viewport at cascade time and are stored as px, so
// descendants inherit px. The computed size is clamped at Blink's
// kMaximumAllowedFontSize (10000), not louis14's former 500. WPT
// css-values/vh-em-inherit.
func TestFontSize_ViewportUnitsComputedAtCascade(t *testing.T) {
	styles := cascadeHTML(t, `<style>html { font-size: 100vw; }
		#t { font-size: 100vh; width: 1rem; height: 1em; }</style>
		<div id="t"><span id="g">x</span></div>`, nil)
	if got := styleByTag(t, styles, "html").GetFontSize(); got != 800 {
		t.Errorf("html font-size 100vw = %v, want 800", got)
	}
	target := styleByID(t, styles, "t")
	if got := target.GetFontSize(); got != 600 {
		t.Errorf("#t font-size 100vh = %v, want 600", got)
	}
	if got := lengthOf(t, target, "width"); got != 800 {
		t.Errorf("#t width 1rem = %v, want 800", got)
	}
	if got := lengthOf(t, target, "height"); got != 600 {
		t.Errorf("#t height 1em = %v, want 600", got)
	}
	g := styleByID(t, styles, "g")
	if got := g.GetFontSize(); got != 600 {
		t.Errorf("grandchild inherited font-size = %v, want 600", got)
	}
	if raw := g.Properties["font-size"]; raw != "600px" {
		t.Errorf("grandchild computed font-size %q, want 600px", raw)
	}
}

// TestFontSize_ComputedClampAndRasterGuard: the computed size is clamped at
// Blink's kMaximumAllowedFontSize = 10000 (core/style/computed_style_constants.h
// :487, applied in FontBuilder::SetSize, core/css/resolver/font_builder.cc:284 @
// 5992657f556bcd9d207a4aca187488462f027cab); louis14's glyph-raster guard
// keeps the used size at 500 or below.
func TestFontSize_ComputedClampAndRasterGuard(t *testing.T) {
	s := NewStyle()
	s.Set("font-size", "20000px")
	if got := s.GetFontSize(); got != 10000 {
		t.Errorf("font-size 20000px = %v, want 10000", got)
	}
	s.Set("font-size", "800px")
	if got := s.GetFontSize(); got != 800 {
		t.Errorf("font-size 800px = %v, want 800", got)
	}
	if got := s.GetUsedFontSize(); got != 500 {
		t.Errorf("used font-size of 800px without a font = %v, want 500", got)
	}
	styles := cascadeHTML(t, `<div id="d" style="font-size: 800px">x</div>`, fakeFontSelector{})
	d := styleByID(t, styles, "d")
	if !d.UsedFontSizeSet {
		t.Fatal("UsedFontSizeSet is false; the font selector did not run")
	}
	if got := d.GetUsedFontSize(); got != 500 {
		t.Errorf("used font-size of 800px with a font = %v, want 500", got)
	}
}

// TestRem_ResolvesAgainstRootComputedFontSize (D3): rem is the root element's
// computed font-size in ordinary lengths and calc() tokens
// (CSSToLengthConversionData::FontSizes::rem_,
// core/css/css_to_length_conversion_data.cc:84-93 @
// 5992657f556bcd9d207a4aca187488462f027cab). WPT css-values/percentage-rem-low.
func TestRem_ResolvesAgainstRootComputedFontSize(t *testing.T) {
	styles := cascadeHTML(t, `<style>:root { font-size: 25%; } #r { width: 25rem; height: calc(1rem + 1px); }</style>
		<div><div id="r"></div></div>`, nil)
	r := styleByID(t, styles, "r")
	if got := lengthOf(t, r, "width"); got != 100 {
		t.Errorf("width 25rem under a 4px root = %v, want 100", got)
	}
	if got := lengthOf(t, r, "height"); got != 5 {
		t.Errorf("height calc(1rem + 1px) under a 4px root = %v, want 5", got)
	}
}

// TestRem_RootFontSizeUsesInitialRem (D3): the root's own font-size resolves
// rem against the initial size, since Blink leaves root_element_style_ null
// for documentElement (core/css/resolver/element_resolve_context.cc:109-113 @
// 5992657f556bcd9d207a4aca187488462f027cab); its other properties use its own
// computed size.
func TestRem_RootFontSizeUsesInitialRem(t *testing.T) {
	styles := cascadeHTML(t, `<style>html { font-size: calc(1rem + 1em); }</style><p>x</p>`, nil)
	if got := styleByTag(t, styles, "html").GetFontSize(); got != 32 {
		t.Errorf("html font-size calc(1rem + 1em) = %v, want 32", got)
	}
	styles = cascadeHTML(t, `<style>:root { font-size: 2rem; } #d { width: 10rem; }</style><div id="d"></div>`, nil)
	if got := styleByTag(t, styles, "html").GetFontSize(); got != 32 {
		t.Errorf(":root font-size 2rem = %v, want 32", got)
	}
	if got := lengthOf(t, styleByID(t, styles, "d"), "width"); got != 320 {
		t.Errorf("width 10rem under a 2rem root = %v, want 320", got)
	}
	styles = cascadeHTML(t, `<style>html { font-size: 10px; width: 5rem; }</style><p>x</p>`, nil)
	if got := lengthOf(t, styleByTag(t, styles, "html"), "width"); got != 50 {
		t.Errorf("html width 5rem with font-size 10px = %v, want 50", got)
	}
}

// TestRem_ZeroRootIsPresentZero (D3): a root font-size of 0 makes rem 0 — a
// measured zero, not an absent value that falls back to 16.
func TestRem_ZeroRootIsPresentZero(t *testing.T) {
	styles := cascadeHTML(t, `<style>:root { font-size: 0px; } #d { font-size: 10px; width: 10rem; }</style>
		<div id="d"></div>`, nil)
	if got := lengthOf(t, styleByID(t, styles, "d"), "width"); got != 0 {
		t.Errorf("width 10rem under a 0px root = %v, want 0", got)
	}
}

// TestRem_SyntheticRoot (D3): in a fragment parse (no <html>), rem resolves
// against :root's font-size through the synthetic doc.Root style. No Blink
// analog: Blink always has a documentElement.
func TestRem_SyntheticRoot(t *testing.T) {
	doc := html.NewDocument()
	doc.Stylesheets = append(doc.Stylesheets, html.StylesheetSource{
		Text: ":root { font-size: 10px; } p { width: 3rem; }"})
	first := &html.Node{Type: html.ElementNode, TagName: "div", Parent: doc.Root}
	p := &html.Node{Type: html.ElementNode, TagName: "p", Parent: doc.Root}
	doc.Root.Children = append(doc.Root.Children, first, p)

	if got := lengthOf(t, ApplyStylesToDocument(doc, 800, 600)[p], "width"); got != 30 {
		t.Errorf("p width 3rem under a 10px :root = %v, want 30", got)
	}
}

// TestFontSize_PseudoAndLateRulesComputed: the font-size conversion also runs
// for pseudo-elements, @container rules and var()-deferred font shorthands —
// every path that reaches the font step (StyleCascade::Apply converts
// font-size in ApplyHighPriority before UpdateFont,
// core/css/resolver/style_cascade.cc:309-310 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestFontSize_PseudoAndLateRulesComputed(t *testing.T) {
	for _, tc := range []struct{ decl, want string }{
		{"font-size: 2rem", "20"},
		{"font-size: 10vw", "80"},
	} {
		src := `<style>:root { font-size: 10px; } #d::before { content: "x"; ` + tc.decl + `; }</style><div id="d">y</div>`
		doc, _ := html.Parse(src)
		styles := ApplyStylesToDocument(doc, 800, 600)
		var dNode *html.Node
		for node := range styles {
			if id, _ := node.GetAttribute("id"); id == "d" {
				dNode = node
			}
		}
		before := ComputePseudoElementStyle(dNode, "before", ParseDocumentStylesheets(doc), 800, 600, styles[dNode])
		if got := before.Properties["font-size"]; got != tc.want+"px" {
			t.Errorf("::before %s = %q, want %spx", tc.decl, got, tc.want)
		}
	}

	styles := cascadeHTML(t, `<style>:root { font-size: 10px; }
		@container (min-width: 100px) { #p { font-size: 2rem; } }</style>
		<div style="container-type: inline-size; width: 500px"><p id="p"><span id="s">x</span></p></div>`, nil)
	if got := styleByID(t, styles, "p").GetFontSize(); got != 20 {
		t.Errorf("@container font-size 2rem = %v, want 20", got)
	}
	if got := styleByID(t, styles, "s").GetFontSize(); got != 20 {
		t.Errorf("child of @container 2rem = %v, want 20", got)
	}

	styles = cascadeHTML(t, `<style>#d { --f: 3vw serif; font: var(--f); }</style><div id="d"><span id="s">x</span></div>`, nil)
	if got := styleByID(t, styles, "s").GetFontSize(); got != 24 {
		t.Errorf("child of font: var(--f) with --f 3vw = %v, want 24", got)
	}
}

// TestFontSize_ExChUseParentMeasuredMetrics (D4): ex and ch in font-size use
// the PARENT's font (StyleResolverState::FontSizeConversionData builds the
// conversion data from ParentStyle()->GetFontSizeStyle(),
// core/css/resolver/style_resolver_state.cc:264-266; FontSizes::Ex / Ch
// :95-104 / :117-126 of core/css/css_to_length_conversion_data.cc, all @
// 5992657f556bcd9d207a4aca187488462f027cab). The parent is 20px family b:
// x-height 0.3em = 6, "0" advance 0.6em = 12. WPT css-values/calc-ch-ex-lang.
func TestFontSize_ExChUseParentMeasuredMetrics(t *testing.T) {
	src := `<style>#p { font-family: b; font-size: 20px; } #c { font-size: calc(1ex + 1ch + 1em); }
		#x { font-size: 1ex; } #h { font-size: 1ch; }</style>
		<div id="p"><div id="c"><span id="g">x</span></div><div id="x"></div><div id="h"></div></div>`
	styles := cascadeHTML(t, src, fakeFontSelector{})
	for _, tc := range []struct {
		id   string
		want float64
	}{{"c", 38}, {"g", 38}, {"x", 6}, {"h", 12}} {
		if got := styleByID(t, styles, tc.id).GetFontSize(); got != tc.want {
			t.Errorf("#%s font-size = %v, want %v", tc.id, got, tc.want)
		}
	}
	if raw := styleByID(t, styles, "c").Properties["font-size"]; !strings.HasSuffix(raw, "px") {
		t.Errorf("#c computed font-size %q, want a px length", raw)
	}
	styles = cascadeHTML(t, src, nil)
	if got := styleByID(t, styles, "x").GetFontSize(); got != 10 {
		t.Errorf("#x font-size 1ex with no font = %v, want 10 (the 0.5em fallback)", got)
	}
}

// TestFontSize_MathResultClampedNonNegative: a math function in font-size is
// clamped to the property's non-negative range (CSS Values 4 §10.12).
func TestFontSize_MathResultClampedNonNegative(t *testing.T) {
	styles := cascadeHTML(t, `<style>#p { font-size: 20px; } #c { font-size: calc(1em - 100px); }</style>
		<div id="p"><div id="c"></div></div>`, nil)
	if got := styleByID(t, styles, "c").GetFontSize(); got != 0 {
		t.Errorf("font-size calc(1em - 100px) under 20px = %v, want 0", got)
	}
}

// TestMediaQueryRem_IgnoresRootFontSize (D7): a media query's rem is the
// initial font-size (Media Queries 4 §1.3), not the root's.
func TestMediaQueryRem_IgnoresRootFontSize(t *testing.T) {
	styles := cascadeHTML(t, `<style>:root { font-size: 100px; }
		@media (min-width: 50rem) { #d { width: 7px; } }</style><div id="d"></div>`, nil)
	if got, ok := styleByID(t, styles, "d").GetLength("width"); !ok || got != 7 {
		t.Errorf("@media (min-width: 50rem) at 800px: width = %v (ok=%v), want 7", got, ok)
	}
}

// TestRem_SyntheticRootOwnFontSizeUsesInitialRem: in a fragment parse (no
// <html>) the first element is :root, so its own rem font-size resolves
// against the initial size even though it is re-cascaded under the synthetic
// doc.Root style that carries its computed size — Blink leaves
// root_element_style_ null for documentElement
// (core/css/resolver/element_resolve_context.cc:109-113 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestRem_SyntheticRootOwnFontSizeUsesInitialRem(t *testing.T) {
	for _, decl := range []string{"2rem", "2em", "200%"} {
		doc := html.NewDocument()
		doc.Stylesheets = append(doc.Stylesheets, html.StylesheetSource{
			Text: ":root { font-size: " + decl + "; } p { width: 1rem; }"})
		first := &html.Node{Type: html.ElementNode, TagName: "div", Parent: doc.Root}
		p := &html.Node{Type: html.ElementNode, TagName: "p", Parent: doc.Root}
		doc.Root.Children = append(doc.Root.Children, first, p)

		styles := ApplyStylesToDocument(doc, 800, 600)
		if got := styles[first].GetFontSize(); got != 32 {
			t.Errorf(":root font-size %s = %v, want 32", decl, got)
		}
		if got := lengthOf(t, styles[p], "width"); got != 32 {
			t.Errorf(":root font-size %s: sibling width 1rem = %v, want 32", decl, got)
		}
	}
}

// TestComputedLengths_InheritedFontRelative: text-indent,
// text-underline-offset and border-spacing are inherited and compute their
// lengths to absolute values, so a 30px child inherits the 10px parent's em /
// ex lengths rather than re-resolving them against its own font. Blink:
// TextIndent::ApplyValue → ConvertToLength(CssToLengthConversionData())
// (core/css/properties/longhands/longhands_custom.cc:10267-10292),
// ConvertTextUnderlineOffset → ConvertLengthOrAuto
// (core/css/resolver/style_builder_converter.cc:3259-3263), and
// -webkit-border-{horizontal,vertical}-spacing → ConvertComputedLength
// (core/css/css_properties.json5:7061-7097, style_builder_converter.h:478-482),
// all @ 5992657f556bcd9d207a4aca187488462f027cab. Percentages stay
// percentages (text-indent of the containing block, text-underline-offset of
// the element's own 1em). text-underline-offset and border-spacing inherit
// through an explicit `inherit` here: louis14 does not yet list them in
// inheritableProperties.
func TestComputedLengths_InheritedFontRelative(t *testing.T) {
	styles := cascadeHTML(t, `<style>
		#p { font-size: 10px; text-indent: 2em; text-underline-offset: 1ex; border-spacing: 1em 2em; }
		#q { font-size: 10px; text-indent: 10%; text-underline-offset: 10%; }
		.c { font-size: 30px; text-underline-offset: inherit; border-spacing: inherit; }</style>
		<div id="p"><div id="c" class="c"></div></div><div id="q"><div id="d" class="c"></div></div>`, nil)
	c := styleByID(t, styles, "c")
	if got, pct := c.GetTextIndent(); got != 20 || pct {
		t.Errorf("inherited text-indent 2em from 10px = %v (pct=%v), want 20px", got, pct)
	}
	if got := c.GetTextUnderlineOffset(); got != 5 {
		t.Errorf("inherited text-underline-offset 1ex from 10px = %v, want 5", got)
	}
	if h, v := c.GetBorderSpacing(), c.GetBorderSpacingV(); h != 10 || v != 20 {
		t.Errorf("inherited border-spacing 1em 2em from 10px = %v %v, want 10 20", h, v)
	}
	d := styleByID(t, styles, "d")
	if got, pct := d.GetTextIndent(); got != 0.1 || !pct {
		t.Errorf("inherited text-indent 10%% = %v (pct=%v), want the 0.1 fraction", got, pct)
	}
	if got := d.GetTextUnderlineOffset(); got != 3 {
		t.Errorf("inherited text-underline-offset 10%% at 30px = %v, want 3", got)
	}
}

// TestTabSize_UnitlessMathIsSpaceCount: a unitless math function in tab-size
// is a <number> — a space count — not a length (CSS Text 3 §4.2;
// ConvertLengthOrTabSpaces keeps a number as TabSize spaces,
// core/css/resolver/style_builder_converter.cc:2182-2194 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestTabSize_UnitlessMathIsSpaceCount(t *testing.T) {
	styles := cascadeHTML(t, `<style>#d { tab-size: calc(2*4); }</style><div id="d"></div>`, nil)
	if got, isLen := styleByID(t, styles, "d").GetTabSize(); got != 8 || isLen {
		t.Errorf("tab-size calc(2*4) = %v (length=%v), want 8 spaces", got, isLen)
	}
}

// TestOutline_PxLiteralRounded: px outline lengths take the same Blink
// rounding as other units — ConvertOutlineOffset and ConvertBorderWidth →
// ClampLineWidth compute every <length> with no px special case
// (core/css/resolver/style_builder_converter.cc:1965-2015 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestOutline_PxLiteralRounded(t *testing.T) {
	styles := cascadeHTML(t, `<style>#d { outline: solid 2.5px; outline-offset: 0.5px; }
		#e { outline: solid 0.25em; outline-offset: -2.5px; font-size: 10px; }</style>
		<div id="d"></div><div id="e"></div>`, nil)
	d, e := styleByID(t, styles, "d"), styleByID(t, styles, "e")
	if got := d.GetOutlineOffset(); got != 1 {
		t.Errorf("outline-offset 0.5px = %v, want 1", got)
	}
	if got := d.GetOutlineWidth(); got != 2 {
		t.Errorf("outline-width 2.5px = %v, want 2", got)
	}
	if got := e.GetOutlineOffset(); got != -2 {
		t.Errorf("outline-offset -2.5px = %v, want -2", got)
	}
}

// TestPseudoElement_ComputedLengths: a pseudo-element's outline-offset is
// computed (rounded by ConvertOutlineOffset) like an element's — Blink builds
// pseudo-element styles through the same StyleBuilder converters
// (core/css/resolver/style_builder_converter.cc:2004-2015 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestPseudoElement_ComputedLengths(t *testing.T) {
	doc, _ := html.Parse(`<style>#d { font-size: 10px; } #d::before { content: "x"; outline: solid 1px; outline-offset: 0.25em; tab-size: calc(1em + 1px); }</style><div id="d">y</div>`)
	styles := ApplyStylesToDocument(doc, 800, 600)
	var dNode *html.Node
	for node := range styles {
		if id, _ := node.GetAttribute("id"); id == "d" {
			dNode = node
		}
	}
	before := ComputePseudoElementStyle(dNode, "before", ParseDocumentStylesheets(doc), 800, 600, styles[dNode])
	if got := before.GetOutlineOffset(); got != 2 {
		t.Errorf("::before outline-offset 0.25em at 10px = %v, want 2", got)
	}
	if got := before.Properties["tab-size"]; got != "11px" {
		t.Errorf("::before tab-size calc(1em + 1px) = %q, want computed 11px", got)
	}
}

// TestComputedLengths_SyntheticRootInheritsComputed: in a fragment parse (no
// <html>), :root's inherited computed lengths reach doc.Root's other children
// through the synthetic root style, so they must be computed against :root's
// font before the copy: a 10px sibling inherits :root's 2em text-indent as
// 40px, not re-resolved against its own font. No Blink analog for the
// synthetic root (Blink always has a documentElement); the computed value is
// TextIndent::ApplyValue's (core/css/properties/longhands/longhands_custom.cc
// :10267-10292 @ 5992657f556bcd9d207a4aca187488462f027cab).
func TestComputedLengths_SyntheticRootInheritsComputed(t *testing.T) {
	doc := html.NewDocument()
	doc.Stylesheets = append(doc.Stylesheets, html.StylesheetSource{
		Text: ":root { font-size: 20px; text-indent: 2em; } p { font-size: 10px; }"})
	first := &html.Node{Type: html.ElementNode, TagName: "div", Parent: doc.Root}
	p := &html.Node{Type: html.ElementNode, TagName: "p", Parent: doc.Root}
	doc.Root.Children = append(doc.Root.Children, first, p)

	if got, pct := ApplyStylesToDocument(doc, 800, 600)[p].GetTextIndent(); got != 40 || pct {
		t.Errorf("sibling text-indent from :root 2em at 20px = %v (pct=%v), want 40px", got, pct)
	}
}

// TestComputedLengths_MixedCalcKeepsPercentOnly: a math function mixing a
// percentage with font-relative lengths computes the length terms and keeps
// the percentage (Blink's ConvertToLength turns calc(10% + 2em) into
// calc(10% + 20px) at a 10px font — CSSMathExpressionNode lengths resolve
// through CssToLengthConversionData, TextIndent::ApplyValue,
// core/css/properties/longhands/longhands_custom.cc:10267-10292 @
// 5992657f556bcd9d207a4aca187488462f027cab), so a 30px child inherits 20px of
// length, not 60px.
func TestComputedLengths_MixedCalcKeepsPercentOnly(t *testing.T) {
	styles := cascadeHTML(t, `<style>#p { font-size: 10px; text-indent: calc(10% + 2em); }
		#c { font-size: 30px; }</style><div id="p"><div id="c"></div></div>`, nil)
	c := styleByID(t, styles, "c")
	if got := c.ResolveTextIndent(100); got != 30 {
		t.Errorf("inherited text-indent calc(10%% + 2em) from 10px at a 100px block = %v, want 30", got)
	}
	if got := styleByID(t, styles, "p").ResolveTextIndent(100); got != 30 {
		t.Errorf("text-indent calc(10%% + 2em) at 10px in a 100px block = %v, want 30", got)
	}
}

// TestTabSize_NegativeMathClampedToZero: tab-size is parsed as a
// non-negative number (TabSize::ParseSingleValue, kNonNegative,
// core/css/properties/longhands/longhands_custom.cc:9892-9905), and a math
// function's result is clamped to that range
// (CSSMathFunctionValue::ClampToPermittedRange,
// core/css/css_math_function_value.cc:190-202), both @
// 5992657f556bcd9d207a4aca187488462f027cab.
func TestTabSize_NegativeMathClampedToZero(t *testing.T) {
	styles := cascadeHTML(t, `<style>#d { tab-size: calc(-2); }</style><div id="d"></div>`, nil)
	if got, isLen := styleByID(t, styles, "d").GetTabSize(); got != 0 || isLen {
		t.Errorf("tab-size calc(-2) = %v (length=%v), want 0 spaces", got, isLen)
	}
}
