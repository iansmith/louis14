package css

import "testing"

// TestMediaQueryKleene covers the 3-valued (True / False / Unknown) semantics
// of @media query evaluation per CSS Media Queries 4 §3.1, mirroring Blink's
// MediaQueryEvaluator::Eval() + ApplyRestrictor() in
// third_party/blink/renderer/core/css/media_query_evaluator.cc @ 4883d11f.
//
// These cases drive the WPT at-media-001 / 002 / 003 reftests in
// pkg/visualtest/testdata/wpt-css3/css-conditional/.
func TestMediaQueryKleene(t *testing.T) {
	cases := []struct {
		name string
		mq   string
		want MediaQueryResult
	}{
		// at-media-001 cases
		{"@media all", "@media all", MediaQueryTrue},
		{"@media not unknown", "@media not unknown", MediaQueryTrue},

		// at-media-002 cases
		{"@media not all", "@media not all", MediaQueryFalse},
		{"@media unknown", "@media unknown", MediaQueryFalse},
		{"@media (unknown)", "@media (unknown)", MediaQueryUnknown},
		{"@media not (unknown)", "@media not (unknown)", MediaQueryUnknown},

		// at-media-003 sanity: empty @media (just `@media { ... }`) matches.
		{"@media (empty)", "@media", MediaQueryTrue},

		// Misc supported types
		{"@media screen", "@media screen", MediaQueryTrue},
		{"@media print", "@media print", MediaQueryFalse},
		{"@media not print", "@media not print", MediaQueryTrue},
		{"@media not screen", "@media not screen", MediaQueryFalse},
		{"@media only screen", "@media only screen", MediaQueryTrue},
	}

	const vw, vh = 800.0, 600.0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mq := parseMediaQuery(c.mq)
			got := EvaluateMediaQuery(mq, vw, vh)
			if got != c.want {
				t.Fatalf("EvaluateMediaQuery(%q) = %d, want %d (parsed: restrictor=%d type=%q conds=%+v)",
					c.mq, got, c.want, mq.Restrictor, mq.MediaType, mq.Conditions)
			}
		})
	}
}

// TestMediaQueryFeatureKleene covers the Kleene combination of feature
// conditions with `and` plus a media type prefix.
func TestMediaQueryFeatureKleene(t *testing.T) {
	cases := []struct {
		name string
		mq   string
		want MediaQueryResult
	}{
		// True ∧ Unknown = Unknown
		{"screen and (min-width:1px) and (unknown)", "@media screen and (min-width:1px) and (unknown)", MediaQueryUnknown},
		// False ∧ Unknown = False (False absorbs)
		{"screen and (min-width:99999px) and (unknown)", "@media screen and (min-width:99999px) and (unknown)", MediaQueryFalse},
		// True ∧ True = True
		{"screen and (min-width:1px)", "@media screen and (min-width:1px)", MediaQueryTrue},
		// not (True ∧ Unknown) = not Unknown = Unknown
		{"not screen and (min-width:1px) and (unknown)", "@media not screen and (min-width:1px) and (unknown)", MediaQueryUnknown},
	}

	const vw, vh = 800.0, 600.0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mq := parseMediaQuery(c.mq)
			got := EvaluateMediaQuery(mq, vw, vh)
			if got != c.want {
				t.Fatalf("EvaluateMediaQuery(%q) = %d, want %d (parsed: restrictor=%d type=%q conds=%+v)",
					c.mq, got, c.want, mq.Restrictor, mq.MediaType, mq.Conditions)
			}
		})
	}
}
