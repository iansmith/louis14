package css

import "testing"

// TestSupportsCondition covers the boolean evaluation of @supports conditions
// per CSS Conditional Rules 3 §6.4 (Supports Syntax), mirroring Blink's
// CSSSupportsParser::ConsumeSupportsCondition() in
// third_party/blink/renderer/core/css/parser/css_supports_parser.cc
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// These cases drive the WPT at-supports-* reftests in
// pkg/visualtest/testdata/wpt-css3/css-conditional/.
func TestSupportsCondition(t *testing.T) {
	cases := []struct {
		name string
		cond string
		want bool
	}{
		// Simple declarations
		{"known property", "(margin: 0)", true},
		{"unknown property", "(foo: 15em)", false},
		{"empty value invalid", "(margin: )", false},
		{"empty parens invalid", "()", false},
		{"trailing semi invalid", "(margin: 0;)", false},

		// at-supports-006 — extra parens
		{"double-wrapped", "((margin: 0))", true},
		{"triple-wrapped", "(((visibility:hidden)))", true},

		// at-supports-007 — AND with !important
		{"and with important", "((margin: 0) and (display:inline-block !important))", true},

		// at-supports-008 — OR
		{"or known-unknown", "((margin: 0) or (foo: 15em))", true},
		{"or unknown-known", "((foo: 15em) or (margin: 0))", true},
		{"or both unknown", "((foo: 15em) or (bar: 12em))", false},

		// at-supports-009 — not with unknown function inside value
		{"not unknown function value", "(not (yadayada: x, calc( 2/3 )))", true},

		// at-supports-010 — combined
		{"combined disj/conj/neg",
			"((yada: 2kg !trivial) or ((not (foo:bar)) and (((visibility:hidden)))))", true},

		// at-supports-012 — chained AND with > 2 terms
		{"3-term and", "((margin: 0) and (background: blue) and (padding:inherit))", true},

		// at-supports-013 — chained OR with > 2 terms
		{"3-term or with one passing", "((yoyo: yaya) or (margin: 0) or (answer: 42))", true},

		// at-supports-014 — `not(` without space is INVALID per CSS Syntax;
		// "not(" is a function-token, not the not-keyword, so it's a
		// <general-enclosed> which evaluates to false. The bare-form
		// outer condition is invalid; the entire at-rule is therefore false.
		{"bare not without space invalid", "not(foo: baz)", false},

		// at-supports-015 — `not (...)` with weird-but-tokenizable value
		{"not with weird value", "(not (foo: baz !unrelated $ ,/[&]))", true},

		// at-supports-018 — function with leading space in args (out of W8.19
		// in-scope set; expectation enforced via the reftest once full per-
		// property value validation lands).

		// at-supports-019 — empty value declaration is invalid
		{"empty-value decl is false", "(margin: )", false},

		// at-supports-039 — semicolon inside parens is invalid
		{"semicolon-terminated decl is false", "(margin: 0;)", false},

		// at-supports-044 — custom property is always supported
		{"custom property", "(--foo: whatever)", true},
		{"not custom property", "(not (--foo: whatever))", false},

		// at-supports-034/035 — operator outside parens is invalid syntax.
		// These need per-property value grammar enforcement (e.g. margin's
		// value parser would reject `0 or padding: 0`). Tracked but out of
		// scope for W8.19's bring-up. Reftest is the source of truth.

		// Bare `not` outside parens — required by spec
		{"bare not unknown", "not (foo: bar)", true},
		{"bare not known", "not (margin: 0)", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateSupportsCondition(tc.cond)
			if got != tc.want {
				t.Errorf("evaluateSupportsCondition(%q) = %v; want %v", tc.cond, got, tc.want)
			}
		})
	}
}
