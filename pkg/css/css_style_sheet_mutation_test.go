package css

import (
	"errors"
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
	if got := selectorsOf(t, c); len(got) != 3 || got[0] != "div" || got[1] != "p" || got[2] != "span" {
		t.Errorf("rules after inserts = %q, want div p span", got)
	}
	if err := c.DeleteRule(1); err != nil {
		t.Fatal(err)
	}
	if got := selectorsOf(t, c); len(got) != 2 || got[0] != "div" || got[1] != "span" {
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
		{"p {}", "div {", 0, "SyntaxError"},
		{"p {}", "a {} b {}", 0, "SyntaxError"},
		{"p {}", "", 0, "SyntaxError"},
		// @import only before every rule but @import / leading @layer.
		{"p {}", "@import url(a.css);", 1, "HierarchyRequestError"},
		{"@import url(a.css);", "p {}", 0, "HierarchyRequestError"},
	} {
		c := NewStyleSheetContents(tt.sheet, NewParserContext(""))
		_, err := c.InsertRule(tt.rule, tt.index)
		var me *RuleMutationError
		if !errors.As(err, &me) || me.Name != tt.want {
			t.Errorf("sheet %q: InsertRule(%q, %d) error = %v, want %s", tt.sheet, tt.rule, tt.index, err, tt.want)
		}
	}
	c := NewStyleSheetContents("p {}", NewParserContext(""))
	var me *RuleMutationError
	if err := c.DeleteRule(1); !errors.As(err, &me) || me.Name != "IndexSizeError" {
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
	if got := selectorsOf(t, c); len(got) != 2 || got[0] != "div" {
		t.Fatalf("rules with the import = %q, want div then :root", got)
	}
	sheet, _ := c.ParsedStylesheet()
	if got := sheet.Rules[1].Declarations["background-image"]; !strings.Contains(got, "http://x/images/green.png") {
		t.Errorf("background-image = %q, want the url resolved against the sheet base", got)
	}
	if err := c.DeleteRule(0); err != nil {
		t.Fatal(err)
	}
	if got := selectorsOf(t, c); len(got) != 1 || got[0] != ":root" {
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
	if got := selectorsOf(t, c); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "p" {
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
		var me *RuleMutationError
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("sheet %q: InsertRule(%q, %d) = %v, want inserted", tt.sheet, tt.rule, tt.index, err)
		case tt.want != "" && (!errors.As(err, &me) || me.Name != tt.want):
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
