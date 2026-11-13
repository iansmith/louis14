package css

import (
	"slices"
	"testing"
)

// insertRule finds a rule's block as the parser does: a `{` inside a
// string does not open it (CSSParserImpl::ConsumeStyleRule reads the
// prelude to the first `{` token, css_parser_impl.cc:2926-2946 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestInsertRuleBraceInSelectorString(t *testing.T) {
	c := NewStyleSheetContents(`p { color: blue }`, NewParserContext(""))
	if n, err := c.InsertRule(`a[title="{"] { color: red }`, 0); err != nil || n != 0 {
		t.Fatalf(`InsertRule(a[title="{"] {…}, 0) = %d, %v, want inserted at 0`, n, err)
	}
	if got := selectorsOf(t, c); !slices.Equal(got, []string{`a[title="{"]`, "p"}) {
		t.Errorf("rules after insert = %q, want a[title=\"{\"] p", got)
	}
}
