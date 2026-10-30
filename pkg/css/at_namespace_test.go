package css

import (
	"testing"

	"louis14/pkg/html"
)

// svgDefaultNamespaceRule declares the SVG namespace as the sheet's default
// namespace, exactly as selectors/is-default-ns-001.html does.
const svgDefaultNamespaceRule = `@namespace url("http://www.w3.org/2000/svg");`

// elementsByTag returns the elements of doc named tag, in document order.
func elementsByTag(doc *html.Document, tag string) []*html.Node {
	var out []*html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.TagName == tag {
			out = append(out, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	return out
}

// firstElement returns the first element of the parsed htmlSrc named tag.
func firstElement(t *testing.T, htmlSrc, tag string) *html.Node {
	t.Helper()
	doc, err := html.Parse(htmlSrc)
	if err != nil {
		t.Fatalf("html.Parse(%q): %v", htmlSrc, err)
	}
	els := elementsByTag(doc, tag)
	if len(els) == 0 {
		t.Fatalf("no <%s> in %q", tag, htmlSrc)
	}
	return els[0]
}

// mustParseSheet parses css with ctx and fails the test on error.
func mustParseSheet(t *testing.T, cssText string, ctx *ParserContext) *Stylesheet {
	t.Helper()
	ss, err := ParseStylesheet(cssText, ctx)
	if err != nil {
		t.Fatalf("ParseStylesheet(%q): %v", cssText, err)
	}
	return ss
}

// sheetSelector parses `<prelude> <sel> {color:red}` and returns the selector of
// its single style rule.
func sheetSelector(t *testing.T, prelude, sel string) Selector {
	t.Helper()
	ss := mustParseSheet(t, prelude+" "+sel+" {color:red}", nil)
	if len(ss.Rules) != 1 {
		t.Fatalf("sheet %q: got %d rules, want 1", prelude+" "+sel+" {color:red}", len(ss.Rules))
	}
	return ss.Rules[0].Selector
}

// TestParseStylesheet_NamespaceRuleRecordsDefaultNamespace pins Blink's
// ConsumeNamespaceRule + StyleSheetContents::ParserAddNamespace: a
// prefix-less top-level @namespace sets the sheet's default namespace,
// verbatim (no base-URL resolution), and does not swallow the following
// style rule; a prefixed one does not set it; a nested one is dropped.
func TestParseStylesheet_NamespaceRuleRecordsDefaultNamespace(t *testing.T) {
	cases := []struct {
		name      string
		css       string
		want      string
		wantRules int
	}{
		{"url form, verbatim under a BaseDir", svgDefaultNamespaceRule + ` div{color:red}`, html.SVGNamespaceURI, 1},
		{"string form", `@namespace "http://www.w3.org/1999/xhtml"; div{color:red}`, html.XHTMLNamespaceURI, 1},
		{"prefixed rule is not the default", `@namespace x "u"; div{color:red}`, "", 1},
		{"nested in @media is dropped", `@media all { @namespace url(svg); }`, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ss := mustParseSheet(t, tc.css, &ParserContext{BaseDir: "/base/dir"})
			if ss.DefaultNamespace != tc.want {
				t.Errorf("DefaultNamespace = %q, want %q", ss.DefaultNamespace, tc.want)
			}
			if len(ss.Rules) != tc.wantRules {
				t.Errorf("got %d rules, want %d", len(ss.Rules), tc.wantRules)
			}
		})
	}
}

// TestParseStylesheet_NamespaceRuleOrdering pins Blink's
// ComputeNewAllowedRules (css_parser_impl.cc:696-745 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): @namespace is valid only
// before every regular rule; @layer statements before the first
// @import/@namespace keep it valid; @import keeps it valid.
func TestParseStylesheet_NamespaceRuleOrdering(t *testing.T) {
	cases := []struct {
		name string
		css  string
		want string
	}{
		{"after a style rule", `div{color:red} @namespace url(svg);`, ""},
		{"after a block at-rule", `@media all{} @namespace url(svg);`, ""},
		{"after an early @layer statement", `@layer a; @namespace url(svg);`, "svg"},
		{"after a late @layer statement", `@namespace "hello"; @layer feh; @namespace "world";`, "hello"},
		{"after @import", `@import "a.css"; @namespace "x";`, "x"},
		{"two in a row, last wins", `@namespace "a"; @namespace "b";`, "b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ss := mustParseSheet(t, tc.css, nil)
			if ss.DefaultNamespace != tc.want {
				t.Errorf("DefaultNamespace = %q, want %q", ss.DefaultNamespace, tc.want)
			}
		})
	}
}

// TestConsumeNamespaceRule pins the @namespace prelude grammar
// `@namespace <namespace-prefix>? [ <string> | <url> ] ;` (Blink
// CSSParserImpl::ConsumeNamespaceRule, css_parser_impl.cc:1209-1230 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func TestConsumeNamespaceRule(t *testing.T) {
	cases := []struct {
		rule       string
		wantPrefix string
		wantURI    string
		wantOK     bool
	}{
		{`@namespace url("x");`, "", "x", true},
		{`@namespace url(x);`, "", "x", true},
		{`@namespace "x";`, "", "x", true},
		{`@namespace 'x';`, "", "x", true},
		{`@namespace pfx "x";`, "pfx", "x", true},
		{`@namespace pfx url(x);`, "pfx", "x", true},
		{`@namespace "x" y;`, "", "", false},
		{`@namespace pfx;`, "", "", false},
		{`@namespace;`, "", "", false},
		{`@namespaces "x";`, "", "", false}, // a different at-keyword
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			prefix, uri, ok := consumeNamespaceRule(tc.rule)
			if ok != tc.wantOK || prefix != tc.wantPrefix || uri != tc.wantURI {
				t.Errorf("consumeNamespaceRule(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.rule, prefix, uri, ok, tc.wantPrefix, tc.wantURI, tc.wantOK)
			}
		})
	}
}

// TestMatchesSelector_DefaultNamespaceTypeSelector pins Blink's
// MatchesTagName / MatchesUniversalTagName (selector_checker.cc:147-177 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): under an SVG default
// namespace an unprefixed type or universal selector is svg|div / svg|*
// and does not match an HTML element, including in a rule nested in
// @media; `*|` still matches any namespace.
func TestMatchesSelector_DefaultNamespaceTypeSelector(t *testing.T) {
	ss := mustParseSheet(t, svgDefaultNamespaceRule+` div{color:red} *{color:red} *|div{color:red} *|*{color:red} @media all { div{color:red} }`, nil)
	div := firstElement(t, "<div></div>", "div")
	want := []struct {
		label string
		match bool
	}{
		{"div", false},
		{"*", false},
		{"*|div", true},
		{"*|*", true},
		{"@media div", false},
	}
	if len(ss.Rules) != len(want) {
		t.Fatalf("got %d rules, want %d", len(ss.Rules), len(want))
	}
	for i, w := range want {
		if got := MatchesSelector(div, ss.Rules[i].Selector); got != w.match {
			t.Errorf("%s: MatchesSelector(HTML div) = %v, want %v", w.label, got, w.match)
		}
	}
}

// TestMatchesSelector_DefaultNamespaceInFunctionalPseudo pins that the
// sheet's default namespace reaches every functional pseudo-class argument
// the matcher re-parses: a type selector there is svg|div and does not
// match an HTML <div> (is-default-ns-001). One row per re-parse site; the
// no-@namespace column is the control.
func TestMatchesSelector_DefaultNamespaceInFunctionalPseudo(t *testing.T) {
	const doc = `<section><p></p><div></div></section>`
	cases := []struct {
		sel     string
		subject string // tag of the element the selector is matched against
		withNS  bool   // want under the svg default namespace
	}{
		{"*|*:is(div)", "div", false},
		{"*|*:where(div)", "div", false},
		{"*|*:not(div)", "div", true},
		{"*|*:has(div)", "section", false},
		{"*|*:has(> div)", "section", false},
		{"*|*:has(+ div)", "p", false},
		{"*|*:has(~ div)", "p", false},
		{"*|*:nth-child(1 of div)", "div", false},
	}
	for _, tc := range cases {
		t.Run(tc.sel, func(t *testing.T) {
			node := firstElement(t, doc, tc.subject)
			if got := MatchesSelector(node, sheetSelector(t, svgDefaultNamespaceRule, tc.sel)); got != tc.withNS {
				t.Errorf("with svg default namespace: matches <%s> = %v, want %v", tc.subject, got, tc.withNS)
			}
			if got := MatchesSelector(node, sheetSelector(t, "", tc.sel)); got != !tc.withNS {
				t.Errorf("control, no @namespace: matches <%s> = %v, want %v", tc.subject, got, !tc.withNS)
			}
		})
	}
}

// TestMatchesSelector_TypelessCompoundIgnoresDefaultNamespace pins that a
// compound without a type selector inside :is/:where/:not stays
// namespace-unconstrained, as Blink's ignore_default_namespace_ makes the
// subject compound (css_selector_parser.cc:1318-1321 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520); is-default-ns-002 and
// not-default-ns-002 depend on it. Regression guard: green before and
// after the fix, red if resolution reaches type-less compounds.
func TestMatchesSelector_TypelessCompoundIgnoresDefaultNamespace(t *testing.T) {
	input := firstElement(t, "<input disabled>", "input")
	cases := []struct {
		sel  string
		want bool
	}{
		{"*|*:is(:disabled)", true},
		{"*|*:where(:disabled)", true},
		{"*|input:not(:disabled)", false},
	}
	for _, tc := range cases {
		t.Run(tc.sel, func(t *testing.T) {
			if got := MatchesSelector(input, sheetSelector(t, svgDefaultNamespaceRule, tc.sel)); got != tc.want {
				t.Errorf("matches <input disabled> = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMatchesSelector_NoDefaultNamespaceIsAny pins that with no @namespace
// (Blink: DefaultNamespace() == g_star_atom, css_selector_parser.cc:2244 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520) and for context-free
// ParseSelector (pkg/js querySelector) type selectors match elements in
// any namespace, HTML and SVG alike. Regression guard: green before and
// after the fix, red if "" stops meaning "any namespace".
func TestMatchesSelector_NoDefaultNamespaceIsAny(t *testing.T) {
	doc, err := html.Parse(`<div></div><svg><rect></rect></svg>`)
	if err != nil {
		t.Fatal(err)
	}
	div := elementsByTag(doc, "div")[0]
	rect := elementsByTag(doc, "rect")[0]
	cases := []struct {
		name string
		sel  Selector
		node *html.Node
	}{
		{"sheet div", sheetSelector(t, "", "div"), div},
		{"sheet *|div", sheetSelector(t, "", "*|div"), div},
		{"sheet |div", sheetSelector(t, "", "|div"), div},
		{"sheet x|div", sheetSelector(t, "", "x|div"), div},
		{"sheet *|*:is(div)", sheetSelector(t, "", "*|*:is(div)"), div},
		{"sheet rect", sheetSelector(t, "", "rect"), rect},
		{"ParseSelector div", ParseSelector("div"), div},
		{"ParseSelector rect", ParseSelector("rect"), rect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !MatchesSelector(tc.node, tc.sel) {
				t.Errorf("does not match <%s>, want match", tc.node.TagName)
			}
		})
	}
}

// TestMatchesSelector_DefaultNamespaceScopedPerSheet pins that a default
// namespace applies only to its own sheet (Blink keeps default_namespace_
// per StyleSheetContents, style_sheet_contents.h:303 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), including across @import,
// which louis14 folds into the importing Stylesheet. The import rows
// guard against introduced leaks (resolving the merged rule list in one
// pass, or copying DefaultNamespace in resolveAtImport).
func TestMatchesSelector_DefaultNamespaceScopedPerSheet(t *testing.T) {
	div := firstElement(t, "<div></div>", "div")

	t.Run("separate sheets", func(t *testing.T) {
		a := mustParseSheet(t, svgDefaultNamespaceRule+` div{color:red}`, nil)
		b := mustParseSheet(t, `div{color:red}`, nil)
		if MatchesSelector(div, a.Rules[0].Selector) {
			t.Error("svg-default sheet's div matches an HTML div")
		}
		if !MatchesSelector(div, b.Rules[0].Selector) {
			t.Error("plain sheet's div does not match an HTML div")
		}
	})

	importCases := []struct {
		name          string
		parent, child string
		importedWant  bool // rule 0: from the imported sheet
		importerWant  bool // rule 1: from the importing sheet
	}{
		{"namespace in importer", `@import "c.css"; ` + svgDefaultNamespaceRule + ` div{color:red}`, `div{color:red}`, true, false},
		{"namespace in imported", `@import "c.css"; div{color:red}`, svgDefaultNamespaceRule + ` div{color:red}`, false, true},
	}
	for _, tc := range importCases {
		t.Run(tc.name, func(t *testing.T) {
			child := tc.child
			ctx := &ParserContext{Fetcher: func(string) (string, error) { return child, nil }}
			ss := mustParseSheet(t, tc.parent, ctx)
			if len(ss.Rules) != 2 {
				t.Fatalf("got %d rules, want 2", len(ss.Rules))
			}
			if got := MatchesSelector(div, ss.Rules[0].Selector); got != tc.importedWant {
				t.Errorf("imported div matches = %v, want %v", got, tc.importedWant)
			}
			if got := MatchesSelector(div, ss.Rules[1].Selector); got != tc.importerWant {
				t.Errorf("importer div matches = %v, want %v", got, tc.importerWant)
			}
		})
	}
}
