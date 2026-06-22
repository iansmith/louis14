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

// TestInitialKeyword_TextAlignResetsNotInherits is the Phase-0 red test for
// LOU-315 cluster 5. CSS Cascade §7.3: `text-align: initial` resolves to the
// property's INITIAL value (`start`), NOT the inherited parent value. text-align
// is an inherited property, so without an explicit nonDefaultInitialValues entry
// the `initial`-induced deletion in resolveCSSWideKeywords is clobbered by the
// later ApplyInheritedProperties pass re-filling the slot from the parent.
// Regression: marker-text-align-001, where `li > div { text-align: initial }`
// must compute `start` even under an `end`-aligned `li`.
func TestInitialKeyword_TextAlignResetsNotInherits(t *testing.T) {
	doc, err := html.Parse(`
		<style>
			.parent { text-align: end; }
			.child { text-align: initial; }
		</style>
		<div class="parent"><div class="child"></div></div>
	`)
	if err != nil {
		t.Fatalf("failed to parse test HTML: %v", err)
	}

	styles := ApplyStylesToDocument(doc, 800, 600)

	foundChild := false
	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			foundChild = true
			if val, ok := style.Get("text-align"); !ok || val != "start" {
				t.Errorf("expected text-align='start' (initial value, not inherited 'end'), got '%s' (ok=%v)", val, ok)
			}
		}
	}
	if !foundChild {
		t.Fatal("test fixture missing .child node — assertion never ran")
	}
}

// TestInherit_TextShadowInherits is the Phase-0 red test for LOU-315 cluster 4a.
// CSS Text Decor 4: `text-shadow` is an inherited property. A child with no
// text-shadow of its own must inherit the parent's. Regression:
// css-pseudo/marker-text-shadow — the `inherit` column and the reference both
// rely on text-shadow inheriting to descendant text. Before the fix, text-shadow
// was absent from inheritableProperties so it never inherited.
func TestInherit_TextShadowInherits(t *testing.T) {
	doc, err := html.Parse(`
		<style>
			.parent { text-shadow: green 1px 2px 3px; }
		</style>
		<div class="parent"><span class="child"></span></div>
	`)
	if err != nil {
		t.Fatalf("failed to parse test HTML: %v", err)
	}

	styles := ApplyStylesToDocument(doc, 800, 600)

	foundChild := false
	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			foundChild = true
			if val, ok := style.Get("text-shadow"); !ok || val == "" {
				t.Errorf("expected child to inherit text-shadow from parent, got '%s' (ok=%v)", val, ok)
			}
		}
	}
	if !foundChild {
		t.Fatal("test fixture missing .child node — assertion never ran")
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
		// rb / rbc / rtc default to inline (CSS Display L3 initial value).
		// When inside <ruby>, box-fixup rules in normalizeRubySubtrees apply,
		// but those are layout-time, not UA stylesheet. Mirrors Blink html.css
		// `rb,rbc,rtc{display:inline}` @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		{rb, DisplayInline, "", ""},
		{rbc, DisplayInline, "", ""},
		{rtc, DisplayInline, "", ""},
		// Standalone <rt> (parent is body, not ruby) gets no UA display
		// (matches Blink: `ruby > rt` is parent-scoped); defaults to inline.
		{rtBare, DisplayInline, "", ""},
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

// TestFontSizeZeroEmMarginResolvesToZero verifies the style layer for LOU-204.
//
// CSS spec: em margins resolve against the element's own computed font-size
// (Blink: StyleRef().FontSize() in ComputedCSSValueFor / ResolveToLength,
// verified at Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
// When font-size:0, 1em == 0px — the computed block margins must be 0, not 16px.
// The layout-level indicator is TestBlockLayout_FontSizeZeroIFCHeight.
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

// TestApplyMarkerUADefaults_NoAuthorRules verifies that the no-author-rule fast
// path in createMarkerPseudoElement applies UA ::marker defaults so that an li
// with text-transform:uppercase still gets text-transform:none on its marker.
// This is the indicator test for LOU-209.
func TestApplyMarkerUADefaults_NoAuthorRules(t *testing.T) {
	// Simulate the state of a cloned list-item style that has text-transform:uppercase
	// inherited (as happens when the li itself has that property).
	liStyle := NewStyle()
	liStyle.Set("text-transform", "uppercase")

	markerStyle := liStyle.Clone()
	// Simulate what the no-author fast path does: also sets unicode-bidi and display.
	markerStyle.Set("unicode-bidi", "isolate")
	markerStyle.Set("display", "inline")

	// Before the fix, applyMarkerCascade is NOT called here, so text-transform
	// remains "uppercase" (inherited from li).  After the fix, calling
	// ApplyMarkerUADefaults resets it to "none".
	ApplyMarkerUADefaults(markerStyle)

	if v, ok := markerStyle.Get("text-transform"); !ok || v != "none" {
		t.Errorf("marker text-transform = %q, want %q (UA default must override inherited value)", v, "none")
	}
	if v, ok := markerStyle.Get("white-space"); !ok || v != "pre" {
		t.Errorf("marker white-space = %q, want %q (UA default must be set)", v, "pre")
	}
	if v, ok := markerStyle.Get("font-variant-numeric"); !ok || v != "tabular-nums" {
		t.Errorf("marker font-variant-numeric = %q, want %q (UA default must be set)", v, "tabular-nums")
	}
}

// TestWideKeywordFallback verifies that a custom property whose var() fallback
// is a CSS-wide keyword collapses that keyword at the declaring element, so the
// computed value (not the raw token list) is inherited. This tests WI-1 of
// LOU-266: --color: var(--foo, unset) should compute to the inherited green
// (because unset→inherit for custom properties), not to black (initial color).
func TestWideKeywordFallback(t *testing.T) {
	// Parse HTML with a three-element tree:
	//   .outer1 { --color: green; }          [parent of outer2]
	//   .outer2 { --color: var(--foo, unset); }  [--foo undefined, fallback is unset]
	//   .inner { color: var(--color); }       [child of outer2]
	//
	// Per CSS Custom Properties §2.2, custom properties are inherited by default.
	// For outer2, unset on a custom property behaves as inherit, so --color should
	// resolve to the parent's (outer1's) computed value, green.
	// Then, inner's color: var(--color) should inherit green from outer2.

	doc, _ := html.Parse(`
		<style>
			.outer1 { --color: green; }
			.outer2 { --color: var(--foo, unset); }
			.inner { color: var(--color); }
		</style>
		<div class="outer1">
			<div class="outer2">
				<div class="inner"></div>
			</div>
		</div>
	`)

	styles := ApplyStylesToDocument(doc, 800, 600)

	var outer1Style, outer2Style, innerStyle *Style

	// Find the three nodes and their computed styles
	for node, style := range styles {
		if node.Type != html.ElementNode || node.TagName != "div" {
			continue
		}
		cls, _ := node.GetAttribute("class")
		switch cls {
		case "outer1":
			outer1Style = style
		case "outer2":
			outer2Style = style
		case "inner":
			innerStyle = style
		}
	}

	if outer1Style == nil || outer2Style == nil || innerStyle == nil {
		t.Fatal("failed to find nodes outer1, outer2, or inner in parsed document")
	}

	// Verify outer2's computed --color is the inherited green (not the raw var token list).
	// After WI-1, the var()-substituted unset is rewritten to inherit, which copies outer1's green.
	if outer2Comp, ok := outer2Style.Get("--color"); !ok || outer2Comp != "green" {
		t.Errorf("outer2 computed --color = %q, want %q (var(--foo, unset) → unset → inherit → green)", outer2Comp, "green")
	}

	// Verify inner's color is green (inherited from outer2's --color).
	// If outer2's --color were still the raw var token list, inner would resolve
	// var(--color) to var(--foo, unset) → unset → initial → black.
	if innerColor, ok := innerStyle.Get("color"); !ok || innerColor != "green" {
		t.Errorf("inner color = %q, want %q (inherited from outer2's computed --color)", innerColor, "green")
	}
}

// HTML §4.5.1 / Blink UA stylesheet (html.css:1683-1687 @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f): link color and underline apply
// via `a:-webkit-any-link` — an <a> WITH an href. A bare <a> (no href) is
// not a link and gets neither.
func TestUAStyles_AnchorOnlyStyledWhenLink(t *testing.T) {
	link := &html.Node{Type: html.ElementNode, TagName: "a", Attributes: map[string]string{"href": "https://example.com/"}}
	linkStyle := NewStyle()
	applyUserAgentStyles(link, linkStyle)
	if v, ok := linkStyle.Get("text-decoration-line"); !ok || v != "underline" {
		t.Errorf("a[href]: expected UA underline, got %q (ok=%v)", v, ok)
	}
	if _, ok := linkStyle.Get("color"); !ok {
		t.Error("a[href]: expected UA link color")
	}

	plain := &html.Node{Type: html.ElementNode, TagName: "a"}
	plainStyle := NewStyle()
	applyUserAgentStyles(plain, plainStyle)
	if v, ok := plainStyle.Get("text-decoration-line"); ok {
		t.Errorf("bare <a> (no href): must not get UA underline, got %q", v)
	}
	if v, ok := plainStyle.Get("color"); ok {
		t.Errorf("bare <a> (no href): must not get UA link color, got %q", v)
	}
}
