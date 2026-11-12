package css

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// Rule mutation on a sheet's contents: CSSStyleSheet::insertRule /
// deleteRule (core/css/css_style_sheet.cc:423-497) through
// StyleSheetContents::WrapperInsertRule / WrapperDeleteRule
// (core/css/style_sheet_contents.cc:278-400) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b (LOU-866).

func selectorsOf(t *testing.T, c *StyleSheetContents) []string {
	t.Helper()
	sheet, err := c.ParsedStylesheet()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range sheet.Rules {
		out = append(out, r.Selector.Raw)
	}
	return out
}

// mutationErrName is the DOMException name of a rule mutation's error, ""
// for another error or none.
func mutationErrName(err error) string {
	var me *RuleMutationError
	if errors.As(err, &me) {
		return me.Name
	}
	return ""
}

func TestInsertAndDeleteRule(t *testing.T) {
	c := NewStyleSheetContents("p { color: blue }", NewParserContext(""))
	if n, err := c.InsertRule("div { color: red }", 0); err != nil || n != 0 {
		t.Fatalf("InsertRule at 0 = %d, %v", n, err)
	}
	if n, err := c.InsertRule("span { color: green }", 2); err != nil || n != 2 {
		t.Fatalf("InsertRule at the end = %d, %v", n, err)
	}
	if c.RuleCount() != 3 {
		t.Fatalf("RuleCount = %d, want 3", c.RuleCount())
	}
	if got := selectorsOf(t, c); !slices.Equal(got, []string{"div", "p", "span"}) {
		t.Errorf("rules after inserts = %q, want div p span", got)
	}
	if err := c.DeleteRule(1); err != nil {
		t.Fatal(err)
	}
	if got := selectorsOf(t, c); !slices.Equal(got, []string{"div", "span"}) {
		t.Errorf("rules after delete = %q, want div span", got)
	}
}

func TestInsertRuleErrors(t *testing.T) {
	for _, tt := range []struct {
		sheet, rule string
		index       int
		want        string
	}{
		{"p {}", "div {}", 2, "IndexSizeError"},
		{"p {}", "a {} b {}", 0, "SyntaxError"},
		{"p {}", "div", 0, "SyntaxError"}, // EOF before the block
		{"", "@import;", 0, "SyntaxError"}, // no URI: ConsumeImportRule's null rule
		{"p {}", "", 0, "SyntaxError"},
		// @import only before every rule but @import / leading @layer.
		{"p {}", "@import url(a.css);", 1, "HierarchyRequestError"},
		{"@import url(a.css);", "p {}", 0, "HierarchyRequestError"},
	} {
		c := NewStyleSheetContents(tt.sheet, NewParserContext(""))
		_, err := c.InsertRule(tt.rule, tt.index)
		if mutationErrName(err) != tt.want {
			t.Errorf("sheet %q: InsertRule(%q, %d) error = %v, want %s", tt.sheet, tt.rule, tt.index, err, tt.want)
		}
	}
	c := NewStyleSheetContents("p {}", NewParserContext(""))
	if err := c.DeleteRule(1); mutationErrName(err) != "IndexSizeError" {
		t.Errorf("DeleteRule(1) of 1 rule = %v, want IndexSizeError", err)
	}
}

// An inserted @import loads, and deleting it drops the imported rules; an
// inserted rule's url() resolves against the sheet's base.
func TestInsertRuleImportAndBase(t *testing.T) {
	ctx := NewParserContext("http://x/images/")
	ctx.Fetcher = func(uri string) (string, error) { return "div { color: red !important }", nil }
	c := NewStyleSheetContents("", ctx)
	if _, err := c.InsertRule("@import url('a.css');", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InsertRule(":root { background-image: url(green.png) }", 1); err != nil {
		t.Fatal(err)
	}
	if got := selectorsOf(t, c); !slices.Equal(got, []string{"div", ":root"}) {
		t.Fatalf("rules with the import = %q, want div then :root", got)
	}
	sheet, _ := c.ParsedStylesheet()
	if got := sheet.Rules[1].Declarations["background-image"]; !strings.Contains(got, "http://x/images/green.png") {
		t.Errorf("background-image = %q, want the url resolved against the sheet base", got)
	}
	if err := c.DeleteRule(0); err != nil {
		t.Fatal(err)
	}
	if got := selectorsOf(t, c); !slices.Equal(got, []string{":root"}) {
		t.Errorf("rules after deleting the import = %q, want :root", got)
	}
}

// A sheet parsed before a mutation re-parses after it; rules count as
// top-level rules (CSSRuleList), not the selectors louis14 flattens a
// selector list into.
func TestInsertRuleAfterParseAndTopLevelCount(t *testing.T) {
	c := NewStyleSheetContents("p { color: blue }", NewParserContext(""))
	if got := selectorsOf(t, c); len(got) != 1 {
		t.Fatalf("precondition: %q", got)
	}
	if _, err := c.InsertRule("a, b { color: red }", 0); err != nil {
		t.Fatal(err)
	}
	if c.RuleCount() != 2 {
		t.Errorf("RuleCount = %d, want 2 top-level rules", c.RuleCount())
	}
	if got := selectorsOf(t, c); !slices.Equal(got, []string{"a", "b", "p"}) {
		t.Errorf("parsed rules after insert = %q, want a b p", got)
	}
}

// WrapperInsertRule's ordering (style_sheet_contents.cc:287-370): leading
// @layer statements, then @import, then @namespace, then the rest.
func TestInsertRuleOrdering(t *testing.T) {
	for _, tt := range []struct {
		sheet, rule string
		index       int
		want        string // "" = inserted
	}{
		// With no @import or @namespace, a leading @layer statement is an
		// ordinary rule (:291-295): an @import may go only before it.
		{"@layer a; p {}", "@import url(x.css);", 1, "HierarchyRequestError"},
		{"@layer a; p {}", "@import url(x.css);", 0, ""},
		{"@import url(x.css);", "@layer a;", 0, ""},
		{"p {}", "@namespace svg url(http://www.w3.org/2000/svg);", 0, "InvalidStateError"},
		{"@import url(x.css); p {}", "@namespace svg url(http://www.w3.org/2000/svg);", 1, "InvalidStateError"},
		{"", "@namespace svg url(http://www.w3.org/2000/svg);", 0, ""},
	} {
		c := NewStyleSheetContents(tt.sheet, NewParserContext(""))
		_, err := c.InsertRule(tt.rule, tt.index)
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("sheet %q: InsertRule(%q, %d) = %v, want inserted", tt.sheet, tt.rule, tt.index, err)
		case tt.want != "" && mutationErrName(err) != tt.want:
			t.Errorf("sheet %q: InsertRule(%q, %d) = %v, want %s", tt.sheet, tt.rule, tt.index, err, tt.want)
		}
	}
}

// The rule list holds the rules Blink keeps: @charset and a rule with an
// invalid selector take no place in it, and a nested rule's style is its
// own declarations, not a nested rule's.
func TestRuleListIsBlinksRules(t *testing.T) {
	c := NewStyleSheetContents(`@charset "utf-8"; a[ { color: red } p { color: blue }`, NewParserContext(""))
	if c.RuleCount() != 1 {
		t.Errorf("RuleCount = %d, want 1 (only p)", c.RuleCount())
	}
	c = NewStyleSheetContents("p { & span { color: red } }", NewParserContext(""))
	if d := c.RuleDeclarations(c.Rules()[0]); d != nil {
		t.Errorf("p's declarations = %v, want none (its only declaration is the nested span's)", d.Declarations)
	}
}

// Recording the rule list must not change the parse: an unterminated
// declaration ending in a bad string (css-variables/variable-reference-32)
// stays invalid, so the earlier color wins.
func TestRuleListKeepsTheParse(t *testing.T) {
	sheet, err := ParseStylesheet("p {\n  color: green;\n  --a: red;\n  color: var(--a, \"\n", NewParserContext(""))
	if err != nil || len(sheet.Rules) != 1 {
		t.Fatalf("parse = %v, %v", sheet, err)
	}
	if got := sheet.Rules[0].Declarations["color"]; got != "green" {
		t.Errorf("color = %q, want green (the bad-string declaration is invalid)", got)
	}
}

// CSSParserImpl::ParseRule accepts a block left open at EOF
// (css_parser_impl.cc:457-485 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestInsertRuleClosesBlockAtEOF(t *testing.T) {
	c := NewStyleSheetContents("p { color: blue }", NewParserContext(""))
	if _, err := c.InsertRule("div { color: red", 0); err != nil {
		t.Fatalf("InsertRule of an open block = %v, want inserted", err)
	}
	if got := selectorsOf(t, c); !slices.Equal(got, []string{"div", "p"}) {
		t.Fatalf("rules = %q, want div then p", got)
	}
	sheet, _ := c.ParsedStylesheet()
	if got := sheet.Rules[0].Declarations["color"]; got != "red" {
		t.Errorf("inserted rule color = %q, want red", got)
	}
}

// A mutation re-parses the sheet's rule texts, whose url()s are already
// resolved: they must not be resolved again. A relative base shows it,
// since resolving twice prefixes the base twice.
func TestMutationResolvesURLsOnce(t *testing.T) {
	c := NewStyleSheetContents("a { background-image: url(img.png) }", NewParserContext("support"))
	if _, err := c.InsertRule("b { background-image: url(x.png) }", 1); err != nil {
		t.Fatal(err)
	}
	sheet, err := c.ParsedStylesheet()
	if err != nil || len(sheet.Rules) != 2 {
		t.Fatalf("parsed sheet = %v, %v; want 2 rules", sheet, err)
	}
	for i, want := range []string{"url(support/img.png)", "url(support/x.png)"} {
		if got := sheet.Rules[i].Declarations["background-image"]; got != want {
			t.Errorf("rule %d background-image = %q, want %q (resolved once)", i, got, want)
		}
	}
}

// One top-level rule outside the rule list (here a stray `;` makes its
// selector invalid) does not unmap the rules around it.
func TestOneDroppedRuleKeepsTheOthersMapped(t *testing.T) {
	c := NewStyleSheetContents("a{width:1px} ;@media screen{a{width:2px} b{width:2px}} c{width:3px}", NewParserContext(""))
	rules := c.Rules()
	if len(rules) != 2 {
		t.Fatalf("rule count = %d, want 2 (a and c)", len(rules))
	}
	for i, want := range []string{"1px", "3px"} {
		d := c.RuleDeclarations(rules[i])
		if d == nil {
			t.Fatalf("rule %d has no declarations", i)
		}
		if got := d.Declarations["width"]; got != want {
			t.Errorf("rule %d width = %q, want %q", i, got, want)
		}
	}
}

// deleteRule of an @namespace rule fails while the sheet has rules after
// its @import / @namespace rules (StyleSheetContents::WrapperDeleteRule, style_sheet_contents.cc:397-406;
// CSSStyleSheet::deleteRule throws InvalidStateError, css_style_sheet.cc:510-513
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestDeleteNamespaceRule(t *testing.T) {
	c := NewStyleSheetContents("@namespace svg url(http://www.w3.org/2000/svg); a { color: red }", NewParserContext(""))
	if err := c.DeleteRule(0); mutationErrName(err) != "InvalidStateError" {
		t.Errorf("DeleteRule(@namespace) with other rules = %v, want InvalidStateError", err)
	}
	if c.RuleCount() != 2 {
		t.Errorf("RuleCount after the failed delete = %d, want 2", c.RuleCount())
	}
	if err := c.DeleteRule(1); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteRule(0); err != nil {
		t.Errorf("DeleteRule(@namespace) alone = %v, want success", err)
	}
	// Another @namespace rule does not block the delete.
	c = NewStyleSheetContents("@namespace a url(x); @namespace b url(y);", NewParserContext(""))
	if err := c.DeleteRule(0); err != nil || c.RuleCount() != 1 {
		t.Errorf("DeleteRule(@namespace) beside another @namespace = %v (%d rules), want success", err, c.RuleCount())
	}
}

// A statement at-rule may end at EOF with no `;`:
// CSSParserImpl::ConsumeEndOfPreludeForAtRuleWithoutBlock returns true when
// the input is exhausted (css_parser_impl.cc:797-807 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), for insertRule and for a
// sheet's last rule alike.
func TestStatementAtRuleEndsAtEOF(t *testing.T) {
	for _, rule := range []string{"@layer z", `@namespace url(http://a)`, "@import url(x.css)"} {
		c := NewStyleSheetContents("", NewParserContext(""))
		if _, err := c.InsertRule(rule, 0); err != nil || c.RuleCount() != 1 {
			t.Errorf("InsertRule(%q) = %v with %d rules, want the one rule inserted", rule, err, c.RuleCount())
		}
	}
	sheet := mustParseSheet(t, `@namespace url(http://a)`, nil)
	if sheet.DefaultNamespace != "http://a" {
		t.Errorf("DefaultNamespace = %q, want http://a from a last @namespace with no ;", sheet.DefaultNamespace)
	}
	sheet = mustParseSheet(t, `p { color: blue } @layer z`, nil)
	if !slices.Contains(sheet.LayerOrder, "z") {
		t.Errorf("LayerOrder = %q, want z from a last @layer with no ;", sheet.LayerOrder)
	}
	ctx := NewParserContext("")
	ctx.Fetcher = func(string) (string, error) { return "div { color: red }", nil }
	sheet = mustParseSheet(t, `@import url(x.css)`, ctx)
	if got := len(sheet.Rules); got != 1 {
		t.Errorf("%d rules, want the imported div rule from a last @import with no ;", got)
	}
}
