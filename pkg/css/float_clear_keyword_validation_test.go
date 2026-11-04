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
// parser drops is unsupported, with the property name in any case and env() as
// a pending substitution.
func TestFloatClear_SupportsRejectsInvalidKeyword(t *testing.T) {
	cases := []struct {
		property, value string
		want            bool
	}{
		{"clear", "inline", false},
		{"clear", "inline-start", true},
		{"float", "both", false},
		{"float", "inline-end", true},
		{"clear", "env(a,left)", true},
		{"float", "ENV(a,left)", true},
		{"CLEAR", "inline", false},
		{"Float", "both", false},
		{"CLEAR", "left", true},
	}
	for _, c := range cases {
		if got := isSupportedDeclaration(c.property, c.value); got != c.want {
			t.Errorf("isSupportedDeclaration(%q, %q) = %v, want %v", c.property, c.value, got, c.want)
		}
	}
}

// TestParseInlineStyle_InvalidKeywordDropsDeclaration pins the style attribute
// path: it reaches the same check as a rule, since
// CSSParserImpl::ParseInlineStyleDeclaration consumes its declarations through
// ConsumeDeclarationValue too (parser/css_parser_impl.cc:371, :3470 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestParseInlineStyle_InvalidKeywordDropsDeclaration(t *testing.T) {
	cases := []struct {
		name, attr, property, want string
	}{
		{"clear: inline", "clear:left;clear:inline", "clear", "left"},
		{"clear: bogus", "clear:left;clear:bogus", "clear", "left"},
		{"float: both", "float:left;float:both", "float", "left"},
		{"!important on an invalid value", "clear:left;clear:inline !important", "clear", "left"},
		{"!important on a valid value", "clear:none;clear:left !important", "clear", "left"},
		{"upper-case property name", "clear:left;CLEAR:inline", "clear", "left"},
		{"a valid value still replaces", "clear:left;clear:both", "clear", "both"},
		{"env() is a pending substitution", "float:left;float:env(x,right)", "float", "right"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, _ := ParseInlineStyle(c.attr, nil).Get(c.property); got != c.want {
				t.Errorf("%s = %q, want %q", c.property, got, c.want)
			}
		})
	}
}

// TestDeclaration_EnvIsPendingSubstitution pins that any env() reference makes
// the declaration a pending substitution, not checked against the property's
// grammar at parse time: ConsumeEnvVariableReference sets kHasReferences
// (parser/css_variable_parser.cc:582-587 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). This holds for every validator on
// isCSSWideOrSubstitutedValue; each property's getter reads through Style.Get,
// which substitutes env().
func TestDeclaration_EnvIsPendingSubstitution(t *testing.T) {
	cases := []struct {
		property, value, want string
	}{
		{"float", "env(x,right)", "right"},
		{"float", "ENV(x,right)", "right"},
		{"float", "env(x, right)", "right"},
		{"clear", "env(x,left)", "left"},
		{"image-orientation", "env(x,none)", "none"},
		{"object-position", "env(x,10px)", "10px"},
		{"background-size", "env(x,10px)", "10px"},
		{"object-view-box", "env(x,none)", "none"},
	}
	for _, c := range cases {
		t.Run(c.property+":"+c.value, func(t *testing.T) {
			decls := parseDeclarations(c.property + ":" + c.value).Declarations
			if decls[c.property] != c.value {
				t.Fatalf("declaration = %q, want it kept as %q", decls[c.property], c.value)
			}
			s := NewStyle()
			s.Set(c.property, decls[c.property])
			if got, _ := s.Get(c.property); got != c.want {
				t.Errorf("substituted = %q, want %q", got, c.want)
			}
		})
	}
	// An identifier that merely ends in "env" is not an env() reference.
	if got := parseDeclarations("float:left;float:xenv(a,right)").Declarations["float"]; got != "left" {
		t.Errorf("float:xenv() = %q, want the earlier left", got)
	}
}
