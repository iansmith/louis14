package css

import "testing"

// TestMediaConditionNesting covers the recursive media-condition tree required
// by CSS Media Queries 4 §3.1 and WPT mediaqueries/negation-001.html.
//
// The negation-001 test uses these specific patterns:
//   - @media not (monochrome)       → condition-level not (not type-level)
//   - @media (not (monochrome))     → condition-level not inside parens
//   - @media not (not (color))      → double negation
//   - @media not ((unknown) or (monochrome)) → or + outer not
//
// The current flat Conditions slice cannot represent any of these; it treats
// "not (monochrome)" as a literal Feature string → Unknown → rule doesn't
// apply → div stays red.
//
// These tests are Phase-0 RED: they all fail on the current code and should
// turn green after the recursive condition tree is implemented.
// Mirrors Blink's MediaCondition / MediaQueryExp recursive tree in
// third_party/blink/renderer/core/css/parser/media_query_parser.cc @ 4883d11f.
func TestMediaConditionNesting(t *testing.T) {
	const vw, vh = 800.0, 600.0

	cases := []struct {
		name string
		mq   string
		want MediaQueryResult
	}{
		// negation-001 .test2: @media not (monochrome)
		// Our renderer is not monochrome → monochrome = False → not False = True.
		// Current code: parses "not (monochrome)" as literal Feature → Unknown.
		{
			name: "not (monochrome) → True (non-monochrome renderer)",
			mq:   "@media not (monochrome)",
			want: MediaQueryTrue,
		},

		// negation-001 .test3: @media (not (monochrome))
		// Same semantics as above but the not is wrapped in an extra paren level.
		{
			name: "(not (monochrome)) → True",
			mq:   "@media (not (monochrome))",
			want: MediaQueryTrue,
		},

		// negation-001 .test4: @media not (not (color))
		// Our renderer has color → color = True → not True = False → not False = True.
		{
			name: "not (not (color)) → True (double negation of True)",
			mq:   "@media not (not (color))",
			want: MediaQueryTrue,
		},

		// negation-001 .test5 gating: @media not ((unknown) or (monochrome))
		// unknown = Unknown; monochrome = False.
		// (unknown) or (monochrome) → Unknown ∨ False = Unknown.
		// not Unknown = Unknown → rule does NOT apply → .test5 stays green.
		// (The CSS sets .test5 green first, then gates red on this query.)
		{
			name: "not ((unknown) or (monochrome)) → Unknown",
			mq:   "@media not ((unknown) or (monochrome))",
			want: MediaQueryUnknown,
		},

		// Extra: pure or in conditions
		// (min-width:0px) or (color) → True ∨ True = True
		{
			name: "(min-width:0px) or (color) → True",
			mq:   "@media (min-width:0px) or (color)",
			want: MediaQueryTrue,
		},

		// Extra: not (color) → not True = False
		{
			name: "not (color) → False",
			mq:   "@media not (color)",
			want: MediaQueryFalse,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mq := parseMediaQuery(c.mq)
			got := EvaluateMediaQuery(mq, vw, vh)
			if got != c.want {
				t.Fatalf("EvaluateMediaQuery(%q) = %v, want %v",
					c.mq, got, c.want)
			}
		})
	}
}
