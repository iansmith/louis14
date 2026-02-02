package css

import "testing"

// TestErrorRecovery_InvalidSelectors verifies that rules with invalid selectors
// are silently skipped while valid rules are still parsed.
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
			expectedRules: 1,
			description:   "rule with [} selector skipped, p rule kept",
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
			expectedRules: 2,
			description:   "body and h1 rules kept, invalid one skipped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := ParseStylesheet(tt.css)
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
			ss, err := ParseStylesheet(tt.css)
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
			ss, err := ParseStylesheet(tt.css)
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
// gracefully — trailing content without a closing brace is discarded.
func TestErrorRecovery_UnclosedBlocks(t *testing.T) {
	tests := []struct {
		name          string
		css           string
		expectedRules int
	}{
		{
			name:          "unclosed block at end",
			css:           `p { color: red; } h1 { font-size: 20px`,
			expectedRules: 1,
		},
		{
			name:          "all blocks properly closed",
			css:           `p { color: red; } h1 { font-size: 20px; }`,
			expectedRules: 2,
		},
		{
			name:          "extra closing brace recovers",
			css:           `} p { color: red; }`,
			expectedRules: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := ParseStylesheet(tt.css)
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
			ss, err := ParseStylesheet(tt.css)
			if err != nil {
				t.Fatalf("ParseStylesheet returned error: %v", err)
			}
			// We don't care about the exact result, just that it didn't crash
			_ = ss
		})
	}
}

// TestErrorRecovery_Acid2Patterns tests specific patterns from the Acid2 test
// that must be silently ignored.
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

	ss, err := ParseStylesheet(acid2CSS)
	if err != nil {
		t.Fatalf("ParseStylesheet returned error: %v", err)
	}

	// We expect exactly 3 valid rules: .eyes, .nose, .mouth
	if len(ss.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(ss.Rules))
		for i, r := range ss.Rules {
			t.Logf("  rule %d: selector=%q declarations=%v", i, r.Selector.Raw, r.Declarations)
		}
	}
}

// TestErrorRecovery_StringsInComments verifies that comment-like sequences
// inside string literals are preserved (not stripped).
func TestErrorRecovery_StringsInComments(t *testing.T) {
	css := `p { content: "/* not a comment */"; color: red; }`
	result := stripCSSComments(css)
	if result != css {
		t.Errorf("stripCSSComments incorrectly stripped inside string literal\n  got:  %q\n  want: %q", result, css)
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
		{"[}", false},       // unbalanced
		{"[attr", false},    // unclosed bracket
		{"div { }", false},  // braces in selector
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
