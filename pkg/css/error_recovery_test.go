package css

import (
	"slices"
	"strings"
	"testing"
)

// TestErrorRecovery_InvalidSelectors verifies that rules with invalid selectors
// are dropped and the valid rules Blink keeps are still parsed.
//
// Blink drops these as TestStrayCloseBraceJoinsNextPrelude cites: an
// unclosed `[` swallows the rest of the sheet.
func TestErrorRecovery_InvalidSelectors(t *testing.T) {
	tests := []struct {
		name          string
		css           string
		expectedRules int
		description   string
	}{
		{
			name:          "selector starting with closing brace",
			css:           `} { color: red; } p { color: blue; }`,
			expectedRules: 1,
			description:   "rule with } selector skipped, p rule kept",
		},
		{
			name:          "selector starting with semicolon",
			css:           `{; color: red; } p { color: blue; }`,
			expectedRules: 1,
			description:   "rule with {; selector skipped, p rule kept",
		},
		{
			name:          "unbalanced bracket in selector",
			css:           `[} { color: red; } p { color: green; }`,
			expectedRules: 0,
			description:   "the unclosed [ swallows the rest, p rule included",
		},
		{
			name:          "empty selector",
			css:           ` { color: red; } p { color: blue; }`,
			expectedRules: 1,
			description:   "rule with empty selector skipped",
		},
		{
			name:          "valid rules survive among invalid ones",
			css:           `body { color: red; } [} { bad: true; } h1 { font-size: 20px; }`,
			expectedRules: 1,
			description:   "body kept; the unclosed [ swallows the rest, h1 included",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := ParseStylesheet(tt.css, nil)
			if err != nil {
				t.Fatalf("ParseStylesheet returned error: %v", err)
			}
			if len(ss.Rules) != tt.expectedRules {
				t.Errorf("%s: got %d rules, want %d", tt.description, len(ss.Rules), tt.expectedRules)
			}
		})
	}
}

// TestErrorRecovery_UnknownAtRules verifies that unknown at-rules are silently skipped.
func TestErrorRecovery_UnknownAtRules(t *testing.T) {
	tests := []struct {
		name          string
		css           string
		expectedRules int
	}{
		{
			name:          "unknown @three-dee rule",
			css:           `@three-dee { body { color: red; } } p { color: blue; }`,
			expectedRules: 1,
		},
		{
			name:          "unknown @import rule",
			css:           `@import url("foo.css") { } p { color: blue; }`,
			expectedRules: 1,
		},
		{
			name:          "multiple unknown at-rules",
			css:           `@foo { x: y; } @bar { a: b; } div { color: red; }`,
			expectedRules: 1,
		},
		{
			name:          "media rule still works",
			css:           `@media screen { p { color: red; } }`,
			expectedRules: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := ParseStylesheet(tt.css, nil)
			if err != nil {
				t.Fatalf("ParseStylesheet returned error: %v", err)
			}
			if len(ss.Rules) != tt.expectedRules {
				t.Errorf("got %d rules, want %d", len(ss.Rules), tt.expectedRules)
			}
		})
	}
}

// TestErrorRecovery_InvalidDeclarations verifies that invalid declarations
// within a valid rule are skipped while valid declarations are preserved.
func TestErrorRecovery_InvalidDeclarations(t *testing.T) {
	tests := []struct {
		name           string
		css            string
		expectedProps  []string // properties that should exist
		forbiddenProps []string // properties that should NOT exist
	}{
		{
			name:           "declaration without colon is skipped",
			css:            `p { badstuff; color: red; }`,
			expectedProps:  []string{"color"},
			forbiddenProps: []string{"badstuff"},
		},
		{
			name:           "declaration with empty value is skipped",
			css:            `p { bad: ; color: green; }`,
			expectedProps:  []string{"color"},
			forbiddenProps: []string{"bad"},
		},
		{
			name:           "property starting with number is skipped",
			css:            `p { 123abc: red; color: blue; }`,
			expectedProps:  []string{"color"},
			forbiddenProps: []string{"123abc"},
		},
		{
			name:          "valid property with hyphen prefix is kept",
			css:           `p { -webkit-thing: value; color: red; }`,
			expectedProps: []string{"-webkit-thing", "color"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := ParseStylesheet(tt.css, nil)
			if err != nil {
				t.Fatalf("ParseStylesheet returned error: %v", err)
			}
			if len(ss.Rules) != 1 {
				t.Fatalf("expected 1 rule, got %d", len(ss.Rules))
			}
			decls := ss.Rules[0].Declarations
			for _, prop := range tt.expectedProps {
				if _, ok := decls[prop]; !ok {
					t.Errorf("expected property %q to exist, but it does not. Declarations: %v", prop, decls)
				}
			}
			for _, prop := range tt.forbiddenProps {
				if _, ok := decls[prop]; ok {
					t.Errorf("property %q should not exist, but it does", prop)
				}
			}
		})
	}
}

// TestErrorRecovery_UnclosedBlocks verifies that unclosed blocks are handled
// gracefully. Per CSS Syntax Level 3 §5.4.3 (consume a simple block), hitting
// <EOF> inside a `{`-block is a parse error that RETURNS the block (it is
// implicitly closed at EOF) — so a trailing unclosed rule with a valid
// declaration is recovered, not discarded. This matches Blink and every browser.
func TestErrorRecovery_UnclosedBlocks(t *testing.T) {
	tests := []struct {
		name          string
		css           string
		expectedRules int
	}{
		{
			// `h1 { font-size: 20px` is implicitly closed at EOF and recovered as
			// `h1 { font-size: 20px }` (the final declaration needs no trailing
			// semicolon) — 2 rules total (CSS Syntax §5.4.3).
			name:          "unclosed block at end",
			css:           `p { color: red; } h1 { font-size: 20px`,
			expectedRules: 2,
		},
		{
			name:          "all blocks properly closed",
			css:           `p { color: red; } h1 { font-size: 20px; }`,
			expectedRules: 2,
		},
		{
			// The stray `}` joins p's prelude, `} p`, no valid selector
			// (CSSParserImpl::ConsumeRuleList, css_parser_impl.cc:748-791 @
			// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
			name:          "extra closing brace invalidates the next rule",
			css:           `} p { color: red; }`,
			expectedRules: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := ParseStylesheet(tt.css, nil)
			if err != nil {
				t.Fatalf("ParseStylesheet returned error: %v", err)
			}
			if len(ss.Rules) != tt.expectedRules {
				t.Errorf("got %d rules, want %d", len(ss.Rules), tt.expectedRules)
			}
		})
	}
}

// TestErrorRecovery_UnclosedStrings verifies that unclosed strings in CSS
// do not crash the parser.
func TestErrorRecovery_UnclosedStrings(t *testing.T) {
	tests := []struct {
		name string
		css  string
	}{
		{
			name: "unclosed double quote in value",
			css:  `p { content: "unclosed; } h1 { color: red; }`,
		},
		{
			name: "unclosed single quote in value",
			css:  `p { content: 'unclosed; } h1 { color: red; }`,
		},
		{
			name: "unclosed string in selector area",
			css:  `p[attr="unclosed { color: red; }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The key test: this must not panic
			ss, err := ParseStylesheet(tt.css, nil)
			if err != nil {
				t.Fatalf("ParseStylesheet returned error: %v", err)
			}
			// We don't care about the exact result, just that it didn't crash
			_ = ss
		})
	}
}

// TestErrorRecovery_Acid2Patterns tests malformed rules in the style of the
// Acid2 test that must be silently ignored. The `[}` here is not in Acid2
// itself; as in Blink, its unclosed `[` swallows the rest of the sheet.
//
// Blink drops these as TestStrayCloseBraceJoinsNextPrelude cites: an
// unclosed `[` swallows the rest of the sheet.
func TestErrorRecovery_Acid2Patterns(t *testing.T) {
	// This CSS simulates a mix of valid and intentionally malformed rules
	// similar to what Acid2 includes.
	acid2CSS := `
		/* Valid rule */
		.eyes { background: yellow; }

		/* Unknown at-rule — must be skipped */
		@three-dee {
			@background-lighting {
				azimuth: 30deg;
				elevation: 190deg;
			}
			h1 { color: red; }
		}

		/* Invalid selector with unbalanced bracket */
		[} { color: red; }

		/* Valid rule after garbage */
		.nose { width: 0; }

		/* Rule with semicolon-only selector */
		{; color: red; }

		/* Another valid rule */
		.mouth { border: 1px solid black; }
	`

	ss, err := ParseStylesheet(acid2CSS, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet returned error: %v", err)
	}

	// Only .eyes: the unclosed `[` swallows .nose and .mouth.
	if len(ss.Rules) != 1 {
		t.Errorf("expected 1 rule (.eyes), got %d", len(ss.Rules))
		for i, r := range ss.Rules {
			t.Logf("  rule %d: selector=%q declarations=%v", i, r.Selector.Raw, r.Declarations)
		}
	}
}

// TestErrorRecovery_StringsInComments verifies that comment-like sequences
// inside string literals are preserved (not stripped).
func TestErrorRecovery_StringsInComments(t *testing.T) {
	css := `p { content: "/* not a comment */"; color: red; }`
	result := StripComments(css)
	if result != css {
		t.Errorf("StripComments incorrectly stripped inside string literal\n  got:  %q\n  want: %q", result, css)
	}
}

// TestErrorRecovery_BraceMatching verifies that splitRules correctly matches
// nested braces.
func TestErrorRecovery_BraceMatching(t *testing.T) {
	// Nested braces (like @media) should be treated as one rule
	css := `@media screen { p { color: red; } h1 { font-size: 20px; } }`
	rules := splitRules(css)
	if len(rules) != 1 {
		t.Errorf("expected 1 top-level rule for @media block, got %d", len(rules))
	}
}

// TestIsValidSelector tests the selector validation function directly.
func TestIsValidSelector(t *testing.T) {
	tests := []struct {
		selector string
		valid    bool
	}{
		{"p", true},
		{".class", true},
		{"#id", true},
		{"div.class", true},
		{"[attr=val]", true},
		{"", false},
		{"}", false},
		{";", false},
		{"{", false},
		{"[}", false},      // unbalanced
		{"[attr", false},   // unclosed bracket
		{"div { }", false}, // braces in selector
	}

	for _, tt := range tests {
		t.Run(tt.selector, func(t *testing.T) {
			got := isValidSelector(tt.selector)
			if got != tt.valid {
				t.Errorf("isValidSelector(%q) = %v, want %v", tt.selector, got, tt.valid)
			}
		})
	}
}

// TestStrayCloseBraceJoinsNextPrelude: a stray top-level `}` is no rule
// boundary (CSSParserImpl::ConsumeRuleList, css_parser_impl.cc:748-791 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), nor is a `;` outside an
// at-rule: either is part of the next rule's prelude, which is then no valid
// selector (ConsumeStyleRule reads the prelude to its `{`).
//
// Blink @ the same SHA: a `}` inside an open `[` is a plain token, not a block
// end (CSSTokenizer::BlockEnd, css_tokenizer.cc:80-87), so ConsumeStyleRule's
// error recovery (css_parser_impl.cc:2926-2946) skips for a top-level `{`
// (CSSParserTokenStream::SkipUntilPeekedTypeIs, css_parser_token_stream.h:
// 316-356) to EOF: an unclosed `[` swallows the rest of the sheet.
//
// The rules a parse keeps are the CSSOM rule list's (RuleTexts), so a
// mutation's re-parse of that list keeps the same rules (LOU-923).
func TestStrayCloseBraceJoinsNextPrelude(t *testing.T) {
	for _, tc := range []struct {
		name, css string
		want      []string
	}{
		{"stray } then ;", `a{width:1px} };b, c{width:2px} d{width:3px}`, []string{"a", "d"}},
		{"stray } before a list", `a{width:1px} }b, c{width:2px} d{width:3px}`, []string{"a", "d"}},
		{"double }", `a{width:1px}} c{width:2px} d{width:3px}`, []string{"a", "d"}},
		{"stray ;", `a{width:1px} ;b,c{width:2px}`, []string{"a"}},
		{"; inside a prelude", `a{width:1px} b ; c{width:2px} d{width:3px}`, []string{"a", "d"}},
		// A `)` that does not close the innermost `{` is a plain token, so the
		// `(` stays open to EOF (CSSTokenizer::BlockEnd).
		{"{ inside an open (", `a{width:1px} @supports (b: { ) {} c{width:2px}`, []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ss := mustParseSheet(t, tc.css, nil)
			if got := ruleSelectors(ss); !slices.Equal(got, tc.want) {
				t.Errorf("rules = %q, want %q", got, tc.want)
			}
			if d := ruleBySelector(ss, "d"); d != nil && d.Declarations["width"] != "3px" {
				t.Errorf("d width = %q, want 3px", d.Declarations["width"])
			}
			reparsed, err := parseResolvedStylesheet(strings.Join(ss.RuleTexts, "\n"), NewParserContext(""))
			if err != nil {
				t.Fatal(err)
			}
			if got := ruleSelectors(reparsed); !slices.Equal(got, tc.want) {
				t.Errorf("re-parsed rule list = %q, want %q", got, tc.want)
			}
		})
	}
}

func ruleSelectors(ss *Stylesheet) []string {
	var sels []string
	for _, r := range ss.Rules {
		sels = append(sels, r.Selector.Raw)
	}
	return sels
}
