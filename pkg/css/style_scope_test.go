package css

import (
	"testing"

	"louis14/pkg/html"
)

// colorOf is the color of the element with the given id.
func colorOf(t *testing.T, styles map[*html.Node]*Style, id string) string {
	t.Helper()
	c, _ := styleByID(t, styles, id).Get("color")
	return c
}

// backgroundOf is the background-color of the element with the given id.
func backgroundOf(t *testing.T, styles map[*html.Node]*Style, id string) string {
	t.Helper()
	c, _ := styleByID(t, styles, id).Get("background-color")
	return c
}

// @scope (<scope-start>) to (<scope-end>): a style rule in the scope's body
// matches only elements inside a scoping root and not at or below one of
// its scoping limits (SelectorChecker::CalculateActivations /
// ElementIsScopingLimit, selector_checker.cc:3967-4100 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestScopeRootAndLimit(t *testing.T) {
	styles := cascadeHTML(t, `<style>
		.t { color: blue }
		@scope (.root) to (.limit) { .t { color: green } }
	</style>
	<div class="t" id="outside"></div>
	<div class="root">
		<div class="t" id="inside"></div>
		<div class="limit t" id="limit"><div class="t" id="below-limit"></div></div>
	</div>`, nil)
	for id, want := range map[string]string{"outside": "blue", "inside": "green", "limit": "blue", "below-limit": "blue"} {
		if got := colorOf(t, styles, id); got != want {
			t.Errorf("#%s color = %q, want %q", id, got, want)
		}
	}
}

// :scope inside @scope matches the scoping root, not the document root
// (SelectorChecker::CheckPseudoScope, selector_checker.cc:3650).
func TestScopePseudoMatchesScopingRoot(t *testing.T) {
	styles := cascadeHTML(t, `<html id="root"><style>
		@scope (#r) { :scope { color: green } }
	</style><div id="r"></div></html>`, nil)
	if got := colorOf(t, styles, "r"); got != "green" {
		t.Errorf("#r color = %q, want green", got)
	}
	if got := colorOf(t, styles, "root"); got == "green" {
		t.Errorf("<html> matched :scope of @scope (#r)")
	}
}

// Scope proximity: of two equally specific scoped declarations, the one
// whose scoping root is nearer wins, regardless of source order; an
// unscoped declaration of equal specificity loses to a scoped one
// (ElementRuleCollector::SortMatchedRules sorts {layer, specificity,
// -proximity, ...}; cascade_priority.h:66-70 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestScopeProximity(t *testing.T) {
	styles := cascadeHTML(t, `<style>
		@scope (.a) { .t { color: green } }
		@scope (.b) { .t { color: red } }
		@scope (.b) { .v { color: red } }
		@scope (.a) { .v { color: green } }
		@scope (.a) { .u { color: green } }
		.u { color: red }
	</style>
	<div class="b"><div class="a"><div class="t" id="t"></div><div class="v" id="v"></div><div class="u" id="u"></div></div></div>`, nil)
	for _, id := range []string{"t", "v"} {
		if got := colorOf(t, styles, id); got != "green" {
			t.Errorf("#%s color = %q, want green (nearer scoping root wins in either source order)", id, got)
		}
	}
	if got := colorOf(t, styles, "u"); got != "green" {
		t.Errorf("#u color = %q, want green (scoped beats unscoped)", got)
	}
}

// A scoped selector that does not contain :scope gets an implicit
// `:scope` anchor with a descendant combinator, and the anchor adds no
// specificity (CreateImplicitAnchor / MaybeCreateImplicitDescendantAnchor,
// css_selector_parser.cc:786-823 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// `&` in the body is :where(:scope) (CSSScopeifiedParentPseudoClass,
// stable; style_scope.cc:142-147), so it adds none either.
func TestScopeImplicitAnchorAddsNoSpecificity(t *testing.T) {
	styles := cascadeHTML(t, `<style>
		@scope (#r) { .t { color: red } & .n { color: red } }
		div.t, div.n { color: green }
	</style>
	<div id="r"><div class="t" id="t"></div><div class="n" id="n"></div></div>`, nil)
	for _, id := range []string{"t", "n"} {
		if got := colorOf(t, styles, id); got != "green" {
			t.Errorf("#%s color = %q, want green", id, got)
		}
	}
	// The anchor is a descendant combinator: the root itself is out.
	styles = cascadeHTML(t, `<style>
		@scope (.t) { .t { color: red } }
	</style><div class="t" id="t"><div class="t" id="inner"></div></div>`, nil)
	if got := colorOf(t, styles, "t"); got == "red" {
		t.Errorf("scoping root matched its own scope's .t rule")
	}
	if got := colorOf(t, styles, "inner"); got != "red" {
		t.Errorf("#inner color = %q, want red (a descendant of the root)", got)
	}
}

// A nested @scope is scoped within the outer scope's roots (StyleScope
// Parent, RuleSet::AddChildRules rule_set.cc:1057-1064 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestNestedScope(t *testing.T) {
	styles := cascadeHTML(t, `<style>
		@scope (.a) { @scope (.b) { .t { color: green } } }
	</style>
	<div class="b"><div class="t" id="b-only"></div></div>
	<div class="a"><div class="b"><div class="t" id="a-b"></div></div></div>`, nil)
	if got := colorOf(t, styles, "a-b"); got != "green" {
		t.Errorf("#a-b color = %q, want green", got)
	}
	if got := colorOf(t, styles, "b-only"); got == "green" {
		t.Errorf("#b-only matched a scope nested in @scope (.a)")
	}
}

// A style rule in a grouping rule inside @scope keeps the scope and the
// implicit :where(:scope) anchor at any depth: neither an element outside
// the scope nor the root itself matches.
func TestScopeAnchorSurvivesGroupingRules(t *testing.T) {
	for _, group := range []struct{ open, close string }{
		{"@media all {", "}"},
		{"@supports (color: red) {", "}"},
		{"@layer x {", "}"},
		{"@media all { @supports (color: red) {", "} }"},
	} {
		t.Run(group.open, func(t *testing.T) {
			styles := cascadeHTML(t, `<style>
				@scope (.a) { `+group.open+` .t { color: green } `+group.close+` }
			</style>
			<div class="t" id="out"></div><div class="a"><div class="t" id="in"></div></div>
			<div class="a t" id="root"></div>`, nil)
			if got := colorOf(t, styles, "in"); got != "green" {
				t.Errorf("#in color = %q, want green", got)
			}
			for _, id := range []string{"out", "root"} {
				if got := colorOf(t, styles, id); got == "green" {
					t.Errorf("#%s matched a rule scoped to .a", id)
				}
			}
		})
	}
}

// Pseudo-element rules in @scope go through the same scope activation
// (scope-pseudo-element.html).
func TestScopePseudoElement(t *testing.T) {
	doc, err := html.Parse(`<style>
		@scope (#r > main) { :scope::before { content: "B" } }
	</style><div id="r"><main id="m"></main></div><main id="other"></main>`)
	if err != nil {
		t.Fatal(err)
	}
	sheets := ParseDocumentStylesheets(doc)
	content := func(id string) string {
		st := ComputePseudoElementStyle(doc.Root.ElementByID(id), "before", sheets, 800, 600)
		c, _ := st.Get("content")
		return c
	}
	if got := content("m"); got != `"B"` {
		t.Errorf("#m::before content = %q, want \"B\"", got)
	}
	if got := content("other"); got == `"B"` {
		t.Errorf("#other::before took content from a rule scoped to #r > main")
	}
}

// A prelude-less @scope is implicit: its root is the parent element of the
// stylesheet's owner node (ImplicitScopeTrigger,
// scoped_style_resolver.cc:444-454 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// including an <svg> or <head> parent, and each owner gives its sheet its
// own root. A `to (...)`-only prelude is implicit too, with limits.
func TestImplicitScopeRoot(t *testing.T) {
	const scoped = `<style>@scope { .a { color: green } }</style>`
	styles := cascadeHTML(t, `<head>`+scoped+`</head><body>
	<div id="root" class="a"><style>
		@scope { .a { color: green } :scope { background-color: blue } }
	</style><div class="a" id="in"></div></div>
	<div class="a" id="out"></div>
	<svg id="svg">`+scoped+`<rect class="a" id="rect"/></svg>
	<section>`+scoped+`<div class="a" id="first"></div></section>
	<section>`+scoped+`<div class="a" id="second"></div></section>
	<div><style>@scope to (.lim) { .a { color: green } }</style>
		<div class="a" id="above-limit"></div>
		<div class="lim"><div class="a" id="below-limit"></div></div></div>
	</body>`, nil)
	for _, tc := range []struct{ id, want string }{
		{"in", "green"}, {"rect", "green"}, {"first", "green"}, {"second", "green"},
		{"above-limit", "green"},
	} {
		if got := colorOf(t, styles, tc.id); got != tc.want {
			t.Errorf("#%s color = %q, want %q", tc.id, got, tc.want)
		}
	}
	// Outside every implicit root (a <head> sheet's root is <head>), the
	// root itself (the anchor is a descendant combinator), and below a limit.
	for _, id := range []string{"out", "root", "below-limit"} {
		if got := colorOf(t, styles, id); got == "green" {
			t.Errorf("#%s matched an implicit scope's .a rule", id)
		}
	}
	if got := backgroundOf(t, styles, "root"); got != "blue" {
		t.Errorf(":scope of the implicit scope: #root background-color = %q, want blue", got)
	}
}

// <scope-end> and a nested <scope-start> are parsed relative to the scope
// (CSSNestingType::kScope): without :scope they get the implicit
// :where(:scope) descendant anchor, so a root is never its own limit and a
// nested scope's root lies strictly inside the outer root
// (StyleScope::Consume, style_scope.cc:86-139;
// MaybeCreateImplicitDescendantAnchor, css_selector_parser.cc:778-824 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestScopeBoundariesAreRelativeToTheScope(t *testing.T) {
	styles := cascadeHTML(t, `<style>
		@scope (.x) to (.x) { :scope { color: green } .t { color: green } }
		@scope (.a) { @scope (.b) { :scope { background-color: green } } }
	</style>
	<div class="x" id="x"><div class="t" id="t"></div></div>
	<div class="a b" id="ab"></div>
	<div class="a"><div class="b" id="b"></div></div>`, nil)
	for _, id := range []string{"x", "t"} {
		if got := colorOf(t, styles, id); got != "green" {
			t.Errorf("#%s color = %q, want green (a root is not its own limit)", id, got)
		}
	}
	if got := backgroundOf(t, styles, "b"); got != "green" {
		t.Errorf("#b background-color = %q, want green", got)
	}
	if got := backgroundOf(t, styles, "ab"); got == "green" {
		t.Errorf("#ab, the outer root itself, matched the nested scope's root")
	}
}

// @scope nested in a style rule: its <scope-start> is relative to the
// parent rule (an implicit `&` anchor), and its body rules are scoped,
// with `&` as :where(:scope) (CSSParserImpl::ConsumeScopeRule,
// css_parser_impl.cc:2135-2172; StyleScope::RuleForNesting,
// style_scope.cc:142-147). Declarations directly in an @scope body apply
// to the scoping root, as `:where(:scope)`.
func TestScopeNestedInStyleRule(t *testing.T) {
	styles := cascadeHTML(t, `<style>
		.p { @scope (.x) { .y { color: green } & > .z { color: green } } }
		@scope (.r) { background-color: green; }
	</style>
	<div class="p"><div class="x"><div class="y" id="in"></div><div class="z" id="z"></div></div></div>
	<div class="x"><div class="y" id="out"></div></div>
	<div class="p x"><div class="y" id="px"></div></div>
	<div class="r" id="r"><div id="r-child"></div></div>`, nil)
	// & in the body is :where(:scope), the .x root, not the parent .p.
	for _, id := range []string{"in", "z"} {
		if got := colorOf(t, styles, id); got != "green" {
			t.Errorf("#%s color = %q, want green", id, got)
		}
	}
	// The prelude is relative to .p: a .x outside .p, or the .p itself, is
	// no root.
	for _, id := range []string{"out", "px"} {
		if got := colorOf(t, styles, id); got == "green" {
			t.Errorf("#%s matched the @scope nested in .p", id)
		}
	}
	if got := backgroundOf(t, styles, "r"); got != "green" {
		t.Errorf("#r background-color = %q, want green (a declaration in the @scope body)", got)
	}
	if got := backgroundOf(t, styles, "r-child"); got == "green" {
		t.Errorf("#r-child took the @scope body's declaration, which applies to the root only")
	}
}

// An implicit scope's root is the owner's parent when the style is
// computed (ImplicitScopeTrigger reads the sheet's owner,
// scoped_style_resolver.cc:444-454): moving the <style> moves the root.
func TestImplicitScopeRootFollowsTheOwner(t *testing.T) {
	doc, err := html.Parse(`<div id="first"><style>@scope { .a { color: green } }</style><div class="a" id="a1"></div></div>
	<div id="second"><div class="a" id="a2"></div></div>`)
	if err != nil {
		t.Fatal(err)
	}
	ApplyStylesToDocument(doc, 800, 600)
	first := doc.Root.ElementByID("first")
	doc.Root.ElementByID("second").AddChild(first.RemoveChild(first.Children[0]))
	doc.UpdateActiveStyleSheets()
	styles := ApplyStylesToDocument(doc, 800, 600)
	if got := colorOf(t, styles, "a2"); got != "green" {
		t.Errorf("#a2 color = %q, want green (the moved <style>'s new parent is the root)", got)
	}
	if got := colorOf(t, styles, "a1"); got == "green" {
		t.Errorf("#a1 still matched the scope of the <style> moved away")
	}
}

// The implicit :scope anchor goes on a style rule of an @scope body,
// directly or in a grouping rule, before CSS nesting expands it: Blink adds
// it when it parses the rule, so `&` in its nested rules carries it
// (MaybeCreateImplicitDescendantAnchor, css_selector_parser.cc:800-823,
// called at :914-930 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). `.q &` lets .q sit outside
// the scope, and a nested `:scope .b` does not stand in for the outer
// rule's anchor.
func TestScopeAnchorBeforeNesting(t *testing.T) {
	for _, tc := range []struct {
		name, body, html string
		green            bool
	}{
		{"& in a nested rule", `.p { .q & { color: green } }`,
			`<div class="q"><div class="r"><div class="p" id="t"></div></div></div>`, true},
		{"& in a grouping rule", `@media all { .p { .q & { color: green } } }`,
			`<div class="q"><div class="r"><div class="p" id="t"></div></div></div>`, true},
		{"& in a nested grouping rule", `@supports (color: red) { @layer x { .p { .q & { color: green } } } }`,
			`<div class="q"><div class="r"><div class="p" id="t"></div></div></div>`, true},
		// `:is(:scope .d) :scope .e`: the in-scope .d would have to sit
		// above the root, so this never matches; without the anchor on .d,
		// a .d above the root would wrongly satisfy it.
		{":scope in a nested rule, its parent outside the scope", `@media all { .d { :scope .e { color: green } } }`,
			`<div class="d"><div class="r"><div class="e" id="t"></div></div></div>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := cascadeHTML(t, `<style>@scope (.r) { `+tc.body+` }</style>`+tc.html, nil)
			if got := colorOf(t, styles, "t") == "green"; got != tc.green {
				t.Errorf("#t green = %v, want %v", got, tc.green)
			}
		})
	}
}

// The scope's <scope-start> and <scope-end> take the sheet's default
// namespace, as its other selectors do (Blink parses them with the sheet's
// parser context, StyleScope::Consume).
func TestScopeSelectorsTakeTheDefaultNamespace(t *testing.T) {
	styles := cascadeHTML(t, `<style>`+svgDefaultNamespaceRule+`
		@scope (g) { *|* { color: green } }
	</style>
	<div><g><span id="html-g-child"></span></g></div>
	<svg><g><rect id="svg-g-child"/></g></svg>`, nil)
	if got := colorOf(t, styles, "svg-g-child"); got != "green" {
		t.Errorf("#svg-g-child color = %q, want green", got)
	}
	if got := colorOf(t, styles, "html-g-child"); got == "green" {
		t.Errorf("#html-g-child matched: an HTML <g> is not a root of @scope (g) under the SVG default namespace")
	}
	// <scope-end> too: an HTML <g> is no limit of `to (g)`.
	styles = cascadeHTML(t, `<style>`+svgDefaultNamespaceRule+`
		@scope (*|*.r) to (g) { *|* { color: green } }
	</style>
	<div class="r"><g><span id="below-html-g"></span></g></div>`, nil)
	if got := colorOf(t, styles, "below-html-g"); got != "green" {
		t.Errorf("#below-html-g color = %q, want green: an HTML <g> is not a limit of to (g) under the SVG default namespace", got)
	}
}

// The implicit :scope anchor is not a written compound, so it takes no
// default namespace (CreateImplicitAnchor builds a bare :scope,
// css_selector_parser.cc:786-797 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// under an SVG default namespace an HTML root still scopes its rules.
func TestScopeAnchorTakesNoDefaultNamespace(t *testing.T) {
	styles := cascadeHTML(t, `<style>`+svgDefaultNamespaceRule+`
		@scope (*|*.x) { *|* { color: green } }
	</style>
	<div class="x"><span id="child"></span></div>`, nil)
	if got := colorOf(t, styles, "child"); got != "green" {
		t.Errorf("#child color = %q, want green", got)
	}
}
