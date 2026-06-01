package css

import (
	"louis14/pkg/html"
	"testing"
)

func TestComputeStyle_ElementSelector(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`div { color: red; }`, nil)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
	}

	style := ComputeStyle(node, stylesheets, 800, 600, nil)

	if color, ok := style.Get("color"); !ok || color != "red" {
		t.Errorf("expected color='red', got '%s'", color)
	}
}

func TestComputeStyle_SpecificityOverride(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; }
		.highlight { color: blue; }
	`, nil)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
		},
	}

	style := ComputeStyle(node, stylesheets, 800, 600, nil)

	// Class selector (.highlight) should override element selector (div)
	if color, ok := style.Get("color"); !ok || color != "blue" {
		t.Errorf("expected color='blue' (class overrides element), got '%s'", color)
	}
}

func TestComputeStyle_IDHasHighestSpecificity(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; }
		.highlight { color: blue; }
		#header { color: green; }
	`, nil)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
			"id":    "header",
		},
	}

	style := ComputeStyle(node, stylesheets, 800, 600, nil)

	// ID selector should override both class and element
	if color, ok := style.Get("color"); !ok || color != "green" {
		t.Errorf("expected color='green' (ID has highest specificity), got '%s'", color)
	}
}

func TestComputeStyle_InlineStyleOverridesAll(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; }
		.highlight { color: blue; }
		#header { color: green; }
	`, nil)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
			"id":    "header",
			"style": "color: purple",
		},
	}

	style := ComputeStyle(node, stylesheets, 800, 600, nil)

	// Inline style should override everything
	if color, ok := style.Get("color"); !ok || color != "purple" {
		t.Errorf("expected color='purple' (inline style overrides all), got '%s'", color)
	}
}

func TestComputeStyle_MultipleProperties(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; width: 100px; }
		.highlight { color: blue; height: 50px; }
	`, nil)
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
		},
	}

	style := ComputeStyle(node, stylesheets, 800, 600, nil)

	// Should have color from .highlight (overrides div)
	if color, ok := style.Get("color"); !ok || color != "blue" {
		t.Errorf("expected color='blue', got '%s'", color)
	}

	// Should have width from div (no override)
	if width, ok := style.Get("width"); !ok || width != "100px" {
		t.Errorf("expected width='100px', got '%s'", width)
	}

	// Should have height from .highlight
	if height, ok := style.Get("height"); !ok || height != "50px" {
		t.Errorf("expected height='50px', got '%s'", height)
	}
}

func TestApplyStylesToDocument(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			div { color: red; }
			.special { color: blue; }
		</style>
		<div></div>
		<div class="special"></div>
	`)

	// Phase 22: Pass viewport dimensions for media query evaluation
	styles := ApplyStylesToDocument(doc, 800, 600)

	// The parser creates implicit <html> and <body> wrappers per HTML5 §8.2.6.4.
	// All 4 elements (html, body, 2 divs) should be styled.
	// Only the 2 divs should have color set (from the CSS rules).
	elementCount := 0
	divCount := 0
	for node, style := range styles {
		if node.Type == html.ElementNode {
			elementCount++
			if node.TagName == "div" {
				divCount++
				if _, ok := style.Get("color"); !ok {
					t.Errorf("expected color to be set on div (class=%q)", node.Attributes["class"])
				}
			}
		}
		_ = style
	}

	if elementCount != 4 {
		t.Errorf("expected 4 styled elements (html, body, 2 divs), got %d", elementCount)
	}
	if divCount != 2 {
		t.Errorf("expected 2 divs, got %d", divCount)
	}
}

func TestInheritKeyword_FloatInherit(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.parent { float: left; }
			.child { float: inherit; }
		</style>
		<div class="parent"><div class="child"></div></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	// Find child node
	for node, style := range styles {
		if node.TagName == "div" {
			if cls, _ := node.GetAttribute("class"); cls == "child" {
				if val, ok := style.Get("float"); !ok || val != "left" {
					t.Errorf("expected float='left' (inherited from parent), got '%s' ok=%v", val, ok)
				}
			}
		}
	}
}

func TestInheritKeyword_ColorInherit(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.parent { color: red; }
			.child { color: inherit; }
		</style>
		<div class="parent"><span class="child"></span></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			if val, ok := style.Get("color"); !ok || val != "red" {
				t.Errorf("expected color='red' (inherited), got '%s'", val)
			}
		}
	}
}

func TestInheritKeyword_NoParentValue(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.child { float: inherit; }
		</style>
		<div class="parent"><div class="child"></div></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			// "inherit" should be resolved away (deleted), not left as literal "inherit"
			if val, ok := style.Get("float"); ok {
				t.Errorf("expected float to be unset (no parent value), got '%s'", val)
			}
		}
	}
}

func TestInheritKeyword_DeepNesting(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.grandparent { display: inline; }
			.parent { display: inherit; }
			.child { display: inherit; }
		</style>
		<div class="grandparent"><div class="parent"><div class="child"></div></div></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			if val, ok := style.Get("display"); !ok || val != "inline" {
				t.Errorf("expected display='inline' (inherited through chain), got '%s'", val)
			}
		}
	}
}

func TestInheritKeyword_InlineStyle(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			.parent { color: blue; }
		</style>
		<div class="parent"><div style="color: inherit"></div></div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if node.Parent != nil {
			if cls, _ := node.Parent.GetAttribute("class"); cls == "parent" && node.TagName == "div" {
				if val, ok := style.Get("color"); !ok || val != "blue" {
					t.Errorf("expected color='blue' (inherited via inline style), got '%s'", val)
				}
			}
		}
	}
}

// TestComputeStyle_RubyUADisplays verifies the Phase 1 ruby UA stylesheet
// mirrors Blink html.css exactly (vetted at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
//
//   - <ruby>            → display: ruby (inline-level)
//   - <ruby><rt>        → display: ruby-text, font-size: 50%, text-align: start
//   - <rt> outside ruby → no UA display (defaults to block)
//   - <rb>, <rbc>, <rtc> → NO UA display override (plain inline boxes;
//     EDisplay in modern Blink has no kRubyBase / kRubyBaseContainer /
//     kRubyTextContainer).
//   - <rp>              → display: none unconditionally (html.css:972-975
//     flat element-only rule, not parent-scoped).
//
// rb is hidden in HTML because <span>/<em>/<strong>/etc. default to
// display:inline; <rb>/<rbc>/<rtc> are not in louis14's inline tag list
// either, so they fall through to the default `block` display. That
// matches Blink (no UA rule for these tags).
func TestComputeStyle_RubyUADisplays(t *testing.T) {
	// Build a ruby tree by hand: <ruby><rb>base</rb><rt>anno</rt><rp>(</rp></ruby>
	// plus a standalone <rt> outside any ruby, plus <rbc>/<rtc>.
	ruby := &html.Node{Type: html.ElementNode, TagName: "ruby"}
	rb := &html.Node{Type: html.ElementNode, TagName: "rb", Parent: ruby}
	rt := &html.Node{Type: html.ElementNode, TagName: "rt", Parent: ruby}
	rp := &html.Node{Type: html.ElementNode, TagName: "rp", Parent: ruby}
	rbc := &html.Node{Type: html.ElementNode, TagName: "rbc", Parent: ruby}
	rtc := &html.Node{Type: html.ElementNode, TagName: "rtc", Parent: ruby}
	ruby.Children = []*html.Node{rb, rt, rp, rbc, rtc}

	// Standalone <rt> outside a ruby parent.
	body := &html.Node{Type: html.ElementNode, TagName: "body"}
	rtBare := &html.Node{Type: html.ElementNode, TagName: "rt", Parent: body}
	body.Children = []*html.Node{rtBare}

	cases := []struct {
		node      *html.Node
		wantDisp  DisplayType
		wantFontS string // "" = unset
		wantTAlig string // "" = unset
	}{
		{ruby, DisplayRuby, "", ""},
		{rt, DisplayRubyText, "50%", "start"},
		{rp, DisplayNone, "", ""},
		// rb / rbc / rtc must NOT receive a ruby-specific display.
		// They have no tag-default inline entry either, so they fall
		// through to GetDisplay's default of DisplayBlock.
		{rb, DisplayBlock, "", ""},
		{rbc, DisplayBlock, "", ""},
		{rtc, DisplayBlock, "", ""},
		// Standalone <rt> (parent is body, not ruby) gets no UA display
		// (matches Blink: `ruby > rt` is parent-scoped); defaults to block.
		{rtBare, DisplayBlock, "", ""},
	}

	for _, c := range cases {
		style := ComputeStyle(c.node, nil, 800, 600, nil)
		gotDisp := style.GetDisplay()
		if gotDisp != c.wantDisp {
			t.Errorf("<%s>: display = %q, want %q", c.node.TagName, gotDisp, c.wantDisp)
		}
		gotFS, _ := style.Get("font-size")
		if gotFS != c.wantFontS {
			t.Errorf("<%s>: font-size = %q, want %q", c.node.TagName, gotFS, c.wantFontS)
		}
		gotTA, _ := style.Get("text-align")
		if gotTA != c.wantTAlig {
			t.Errorf("<%s>: text-align = %q, want %q", c.node.TagName, gotTA, c.wantTAlig)
		}
	}
}

// TestGetDisplay_TwoKeywordRuby verifies that GetDisplay accepts the
// CSS Display L3 two-keyword forms `block ruby` and `inline ruby`,
// and that `ruby-base` (deleted in Phase 1, since modern Blink has no
// kRubyBase) is no longer recognized.
func TestGetDisplay_TwoKeywordRuby(t *testing.T) {
	cases := []struct {
		input string
		want  DisplayType
	}{
		{"ruby", DisplayRuby},
		{"inline ruby", DisplayRuby},
		{"block ruby", DisplayBlockRuby},
		// Extra whitespace normalizes to the canonical form.
		{"  block   ruby ", DisplayBlockRuby},
		{"ruby-text", DisplayRubyText},
		// ruby-base is no longer a recognized keyword — falls through
		// to the default DisplayBlock.
		{"ruby-base", DisplayBlock},
	}
	for _, c := range cases {
		s := NewStyle()
		s.Set("display", c.input)
		if got := s.GetDisplay(); got != c.want {
			t.Errorf("GetDisplay(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestEquivalentInlineDisplay covers the Phase 1 inlinification mapping
// used by normalizeRubySubtrees / inlinifyRubyChildren. Mirrors Blink
// `core/css/resolver/style_adjuster.cc:303-356`.
func TestEquivalentInlineDisplay(t *testing.T) {
	cases := []struct {
		in, want DisplayType
	}{
		{DisplayBlock, DisplayInlineBlock},
		{DisplayFlowRoot, DisplayInlineBlock},
		{DisplayListItem, DisplayInlineBlock},
		{DisplayFlex, DisplayInlineFlex},
		{DisplayGrid, DisplayInlineGrid},
		{DisplayTable, DisplayInlineTable},
		{DisplayBlockRuby, DisplayRuby},
		// Already inline — identity.
		{DisplayInline, DisplayInline},
		{DisplayInlineBlock, DisplayInlineBlock},
		{DisplayRuby, DisplayRuby},
		{DisplayRubyText, DisplayRubyText},
	}
	for _, c := range cases {
		if got := EquivalentInlineDisplay(c.in); got != c.want {
			t.Errorf("EquivalentInlineDisplay(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// CSS Cascade 5 §6.2 + §6.1.3: each anonymous @layer block is its own
// distinct layer, and revert-layer rolls back to the value the property
// held at the start of the current layer.
func TestComputeStyle_RevertLayer_AnonymousLayers(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		@layer { #target { background-color: green; } }
		@layer { #target { background-color: red; background-color: revert-layer; } }
	`, nil)
	if len(stylesheet.LayerOrder) != 2 {
		t.Fatalf("expected 2 anonymous layers in LayerOrder, got %d: %v",
			len(stylesheet.LayerOrder), stylesheet.LayerOrder)
	}
	stylesheets := []*Stylesheet{stylesheet}
	node := &html.Node{
		Type:       html.ElementNode,
		TagName:    "div",
		Attributes: map[string]string{"id": "target"},
	}
	style := ComputeStyle(node, stylesheets, 800, 600, nil)
	if bg, _ := style.Get("background-color"); bg != "green" {
		t.Errorf("expected background-color=green (revert-layer to first @layer), got %q", bg)
	}
}

// CSS Cascade 5 §6.1.3 + §6.4.1: !important revert-layer also rolls back to
// the declaration-order predecessor layer's value (revert-layer-005). The
// !important layer ordering is reversed for cascade precedence, but the
// revert-layer snapshot is still the declaration-order one.
func TestComputeStyle_RevertLayer_ImportantPreservesDeclarationOrder(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		@layer { #target { background-color: green; } }
		@layer { #target {
			background-color: red;
			background-color: red !important;
			background-color: revert-layer !important;
		} }
		@layer { #target {
			background-color: red;
			background-color: red !important;
		} }
	`, nil)
	stylesheets := []*Stylesheet{stylesheet}
	node := &html.Node{
		Type:       html.ElementNode,
		TagName:    "div",
		Attributes: map[string]string{"id": "target"},
	}
	style := ComputeStyle(node, stylesheets, 800, 600, nil)
	if bg, _ := style.Get("background-color"); bg != "green" {
		t.Errorf("expected background-color=green (important revert-layer should revert to first @layer), got %q", bg)
	}
}

// CSS Cascade 5 §6.1.3: revert-layer inside an inline `style` attribute
// rolls back to the pre-inline cascaded value (revert-layer-009).
func TestComputeStyle_RevertLayer_InlineStyle(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		#target { background-color: green; }
	`, nil)
	stylesheets := []*Stylesheet{stylesheet}
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"id":    "target",
			"style": "background-color: red; background-color: revert-layer",
		},
	}
	style := ComputeStyle(node, stylesheets, 800, 600, nil)
	if bg, _ := style.Get("background-color"); bg != "green" {
		t.Errorf("expected background-color=green (revert-layer in inline style), got %q", bg)
	}
}

// CSS Cascade 5 §6.1.3: chained revert-layer across multiple layers must
// resolve transitively — each layer's revert-layer should restore the
// previous layer's RESOLVED value, not the literal sentinel
// (revert-layer-007).
func TestComputeStyle_RevertLayer_ChainedAcrossLayers(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		@layer { #target { background-color: green; } }
		@layer { #target { background-color: red; background-color: revert-layer; } }
		@layer { #target { background-color: red; background-color: revert-layer; } }
		@layer { #target { background-color: red; background-color: revert-layer; } }
	`, nil)
	stylesheets := []*Stylesheet{stylesheet}
	node := &html.Node{
		Type:       html.ElementNode,
		TagName:    "div",
		Attributes: map[string]string{"id": "target"},
	}
	style := ComputeStyle(node, stylesheets, 800, 600, nil)
	if bg, _ := style.Get("background-color"); bg != "green" {
		t.Errorf("expected background-color=green (chained revert-layer), got %q", bg)
	}
}

// TestAppliedTextDecorations_PropagationBoundaries exercises the CSS
// Text Decor 4 §1.3 propagation boundary rules wired into
// ResolveAppliedTextDecorations: atomic inlines, replaced elements,
// out-of-flow descendants reset the inherited vector; display:contents
// passes the vector through but contributes nothing of its own.
func TestAppliedTextDecorations_PropagationBoundaries(t *testing.T) {
	cases := []struct {
		name     string
		html     string
		findTag  string // first element matching this tag receives the assertion
		findAttr string // optional class= attribute filter on the matched element
		wantLen  int    // expected len(AppliedTextDecorations) on that element
	}{
		{
			name:    "plain inline span inherits parent decoration",
			html:    `<style>.outer{text-decoration:underline}</style><div class="outer"><span class="t">hi</span></div>`,
			findTag: "span",
			wantLen: 1,
		},
		{
			name:    "atomic inline-block resets parent decoration",
			html:    `<style>.outer{text-decoration:underline}.t{display:inline-block}</style><div class="outer"><span class="t">hi</span></div>`,
			findTag: "span",
			wantLen: 0,
		},
		{
			name:    "replaced svg resets parent decoration",
			html:    `<style>.outer{text-decoration:underline}</style><span class="outer"><svg></svg></span>`,
			findTag: "svg",
			wantLen: 0,
		},
		{
			name:     "absolute-positioned descendant resets parent decoration",
			html:     `<style>.outer{text-decoration:underline}.t{position:absolute}</style><div class="outer"><div class="t">hi</div></div>`,
			findTag:  "div",
			findAttr: "t",
			wantLen:  0,
		},
		{
			name:     "float descendant resets parent decoration",
			html:     `<style>.outer{text-decoration:underline}.t{float:left}</style><div class="outer"><div class="t">hi</div></div>`,
			findTag:  "div",
			findAttr: "t",
			wantLen:  0,
		},
		{
			name: "display:contents skips own contribution, passes parent through",
			// Parent overline; contents-span declares underline but contributes nothing;
			// inner span inherits only overline.
			html:     `<style>p{text-decoration:overline}.c{text-decoration:underline;display:contents}</style><p><span class="c"><span class="t">hi</span></span></p>`,
			findTag:  "span",
			findAttr: "t",
			wantLen:  1,
		},
		{
			name:     "nested block-level decorations DO accumulate (in-flow, not atomic)",
			html:     `<style>.outer{text-decoration:underline}.inner{text-decoration:overline}</style><div class="outer"><div class="inner">hi</div></div>`,
			findTag:  "div",
			findAttr: "inner",
			wantLen:  2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, _ := html.Parse(c.html)
			styles := ApplyStylesToDocument(doc, 800, 600)

			var match *html.Node
			var walk func(n *html.Node)
			walk = func(n *html.Node) {
				if match != nil {
					return
				}
				if n.Type == html.ElementNode && n.TagName == c.findTag {
					if c.findAttr == "" || n.Attributes["class"] == c.findAttr {
						match = n
						return
					}
				}
				for _, child := range n.Children {
					walk(child)
				}
			}
			walk(doc.Root)
			if match == nil {
				t.Fatalf("element %q (class=%q) not found in DOM", c.findTag, c.findAttr)
			}
			style := styles[match]
			if style == nil {
				t.Fatalf("no style computed for %q (class=%q)", c.findTag, c.findAttr)
			}
			got := len(style.GetAppliedTextDecorations())
			if got != c.wantLen {
				t.Errorf("AppliedTextDecorations length: got %d, want %d", got, c.wantLen)
			}
		})
	}
}

// TestFontSizeZeroEmMarginResolvesToZero is the Phase 0 / LOU-204 indicator test.
//
// CSS spec: em margins resolve against the element's own computed font-size
// (Blink: StyleRef().FontSize() in ComputedCSSValueFor / ResolveToLength,
// verified at Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
// When font-size:0, 1em == 0px — the computed block margins must be 0, not 16px.
//
// The WPT test css-fonts/font-size-zero-1.html exercises exactly this:
//
//	p { margin: 1em 0 }; p.zero { font-size: 0 }
//
// → .zero's top and bottom margins must be 0 (not 16px).
func TestFontSizeZeroEmMarginResolvesToZero(t *testing.T) {
	doc, _ := html.Parse(`<!DOCTYPE html>
<style>
p { margin: 1em 0 }
p.zero { font-size: 0 }
</style>
<p class="zero">zero</p>`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		if node.TagName != "p" {
			continue
		}
		cls, _ := node.GetAttribute("class")
		if cls != "zero" {
			continue
		}
		// Element's own font-size must be 0.
		if fs := style.GetFontSize(); fs != 0 {
			t.Errorf("p.zero GetFontSize() = %v, want 0", fs)
		}
		// em margins must resolve to 0 * 1 = 0, not parent's 16px * 1 = 16px.
		margins := style.GetAllMarginsForWidth(800)
		if margins.Top != 0 {
			t.Errorf("p.zero margin-top = %v, want 0 (1em with font-size:0 must be 0px)", margins.Top)
		}
		if margins.Bottom != 0 {
			t.Errorf("p.zero margin-bottom = %v, want 0 (1em with font-size:0 must be 0px)", margins.Bottom)
		}
		return
	}
	t.Error("p.zero element not found in styles map")
}
