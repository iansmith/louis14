package css

import "testing"

// TestFloatClear_InvalidKeywordDropsDeclaration pins that an invalid float or
// clear value drops the whole declaration, so an earlier valid one survives.
// Mirrors CSSParserFastPaths::IsValidKeywordPropertyAndValue
// (parser/css_parser_fast_paths.cc:1195-1199, :1240-1244 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and ParseKeywordValue returning
// nullptr (:1976-1983 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFloatClear_InvalidKeywordDropsDeclaration(t *testing.T) {
	cases := []struct {
		name, decls, property, want string
	}{
		{"clear: inline is not a keyword", "clear:left; clear:inline", "clear", "left"},
		{"clear: block is not a keyword", "clear:left; clear:block", "clear", "left"},
		{"clear: bogus", "clear:left; clear:bogus", "clear", "left"},
		{"clear: two keywords", "clear:left; clear:left right", "clear", "left"},
		{"float: both is clear's keyword", "float:left; float:both", "float", "left"},
		{"float: bogus", "float:left; float:bogus", "float", "left"},
		{"float: two keywords", "float:left; float:left right", "float", "left"},
		{"clear: none", "clear:left; clear:none", "clear", "none"},
		{"clear: both", "clear:left; clear:both", "clear", "both"},
		{"clear: inline-start", "clear:left; clear:inline-start", "clear", "inline-start"},
		{"clear: inline-end", "clear:left; clear:inline-end", "clear", "inline-end"},
		{"float: none", "float:left; float:none", "float", "none"},
		{"float: inline-start", "float:left; float:inline-start", "float", "inline-start"},
		{"float: inline-end", "float:left; float:inline-end", "float", "inline-end"},
		{"clear: CSS-wide keyword", "clear:left; clear:inherit", "clear", "inherit"},
		{"float: CSS-wide keyword", "float:left; float:unset", "float", "unset"},
		{"clear: var() is checked at computed time", "clear:left; clear:var(--a)", "clear", "var(--a)"},
		{"float: var() is checked at computed time", "float:left; float:var(--a)", "float", "var(--a)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseDeclarations(c.decls).Declarations[c.property]; got != c.want {
				t.Errorf("%s = %q, want %q", c.property, got, c.want)
			}
		})
	}
}

// TestFloatClear_KeywordsMatchCaseInsensitively pins that the keywords match
// ASCII case-insensitively, as CssValueKeywordID does (ParseKeywordValue,
// parser/css_parser_fast_paths.cc:1959 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), through to the used value.
func TestFloatClear_KeywordsMatchCaseInsensitively(t *testing.T) {
	s := NewStyle()
	for property, value := range parseDeclarations("float:INLINE-End; clear:LEFT").Declarations {
		s.Set(property, value)
	}
	if got := s.Floating(DirectionLTR); got != FloatRight {
		t.Errorf("float:INLINE-End = %v, want right", got)
	}
	if got := s.Clear(DirectionLTR); got != ClearLeft {
		t.Errorf("clear:LEFT = %v, want left", got)
	}
}

// TestFloatClear_SupportsRejectsInvalidKeyword pins that @supports sees the
// same grammar: CSS.supports parses through CSSPropertyParser, so a value the
// parser drops is unsupported.
func TestFloatClear_SupportsRejectsInvalidKeyword(t *testing.T) {
	cases := []struct {
		property, value string
		want            bool
	}{
		{"clear", "inline", false},
		{"clear", "inline-start", true},
		{"float", "both", false},
		{"float", "inline-end", true},
	}
	for _, c := range cases {
		if got := isSupportedDeclaration(c.property, c.value); got != c.want {
			t.Errorf("isSupportedDeclaration(%q, %q) = %v, want %v", c.property, c.value, got, c.want)
		}
	}
}
