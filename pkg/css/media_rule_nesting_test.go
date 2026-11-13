package css

import "testing"

// A rule nested in several @media rules applies only when every one of them
// matches: RuleSet::AddChildRules recurses into an @media's child rules only
// when MatchMediaForAddRules holds for it, at each level of the chain, through
// @supports, @scope and further @media rules alike (rule_set.cc:999-1004 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestNestedMediaRulesAllGate(t *testing.T) {
	const never, always = "(min-width: 5000px)", "(min-width: 0)"
	for _, tc := range []struct{ css, want string }{
		// A deeper @media keeps its own condition.
		{`@media all { @scope (.r) { @media ` + never + ` { .t { color: red } } } }`, "green"},
		{`@media all { @supports (color: red) { @media ` + never + ` { .t { color: red } } } }`, "green"},
		// An outer @media still gates an inner one.
		{`@media ` + never + ` { @media all { .t { color: red } } }`, "green"},
		{`@media ` + never + ` { @scope (.r) { @media all { .t { color: red } } } }`, "green"},
		// A matching inner level does not rescue a failing middle one, nor
		// an outer list with no matching branch.
		{`@media all { @media ` + never + ` { @media ` + always + ` { .t { color: red } } } }`, "green"},
		{`@media ` + never + `, (min-width: 6000px) { @media ` + always + ` { .t { color: red } } }`, "green"},
		// Every level matching applies the rule.
		{`@media all { @scope (.r) { @media ` + always + ` { .t { color: blue } } } }`, "blue"},
		{`@media all { @supports (color: red) { @media ` + always + ` { .t { color: blue } } } }`, "blue"},
		{`@media ` + always + ` { @media all { .t { color: blue } } }`, "blue"},
		// One branch of an outer list matching is enough.
		{`@media ` + never + `, all { @media ` + always + ` { .t { color: blue } } }`, "blue"},
	} {
		t.Run(tc.css, func(t *testing.T) {
			styles := cascadeHTML(t, `<style>.t { color: green } `+tc.css+`</style>
			<div class="r"><div class="t" id="t"></div></div>`, nil)
			if got := colorOf(t, styles, "t"); got != tc.want {
				t.Errorf("#t color = %q, want %s", got, tc.want)
			}
		})
	}
}
