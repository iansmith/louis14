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
// css_selector_parser.cc:778-824 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
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

// A conditional rule inside @scope keeps the scope.
func TestScopeWrapsMedia(t *testing.T) {
	styles := cascadeHTML(t, `<style>
		@scope (.a) { @media all { .t { color: green } } }
	</style>
	<div class="t" id="out"></div><div class="a"><div class="t" id="in"></div></div>`, nil)
	if got := colorOf(t, styles, "in"); got != "green" {
		t.Errorf("#in color = %q, want green", got)
	}
	if got := colorOf(t, styles, "out"); got == "green" {
		t.Errorf("#out matched a rule scoped to .a")
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
	if got, _ := styleByID(t, styles, "root").Get("background-color"); got != "blue" {
		t.Errorf(":scope of the implicit scope: #root background-color = %q, want blue", got)
	}
}
