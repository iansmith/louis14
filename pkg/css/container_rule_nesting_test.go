package css

import "testing"

// containerHost is a 400px inline-size container around .r > .t#t.
const containerHost = `<div style="container-type: inline-size; width: 400px"><div class="r"><div class="t" id="t"></div></div></div>`

// containerColor cascades css (after `.t { color: green }`) over
// containerHost and returns #t's color.
func containerColor(t *testing.T, css string) string {
	t.Helper()
	styles := cascadeHTML(t, `<style>.t { color: green } `+css+`</style>`+containerHost, nil)
	return colorOf(t, styles, "t")
}

// An @container body is a rule list allowing what @media's does: Blink reads
// it with ConsumeRuleList(kRegularRules) (CSSParserImpl::ConsumeContainerRule,
// css_parser_impl.cc:2208-2249, :3215-3221 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so its nested at-rules apply.
// (A nested @container is TestContainerChainAllGate's.)
func TestContainerBodyNestedAtRules(t *testing.T) {
	const yes = "(min-width: 0)"
	for _, css := range []string{
		`@container ` + yes + ` { @scope (.r) { .t { color: blue } } }`,
		`@container ` + yes + ` { @media all { .t { color: blue } } }`,
		`@container ` + yes + ` { @supports (color: red) { .t { color: blue } } }`,
		// !important: a layered normal rule loses to containerColor's
		// unlayered green; a layered important one beats it.
		`@container ` + yes + ` { @layer l { .t { color: blue !important } } }`,
	} {
		t.Run(css, func(t *testing.T) {
			if got := containerColor(t, css); got != "blue" {
				t.Errorf("#t color = %q, want blue", got)
			}
		})
	}
}

// Side-effect at-rules in an @container body register on the sheet.
func TestContainerBodyRegistersAtRules(t *testing.T) {
	ss := mustParseSheet(t, `@container (min-width: 0) {
		@font-face { font-family: f; src: url(f.woff) }
		@keyframes k { from { opacity: 0 } }
		@counter-style c { system: cyclic; symbols: a }
	}`, nil)
	if len(ss.FontFaces) != 1 {
		t.Errorf("%d @font-face rules, want 1", len(ss.FontFaces))
	}
	if _, ok := ss.Keyframes["k"]; !ok {
		t.Error("@keyframes k not registered")
	}
	if len(ss.CounterStyles) != 1 {
		t.Errorf("%d @counter-style rules, want 1", len(ss.CounterStyles))
	}
}

// Every level of an @container chain must match: RuleSet::AddChildRules
// gives a nested @container's set the outer one as parent
// (ContainerQuerySet::CopyWithParent, rule_set.cc:1036-1045,
// container_query_set.cc:12-16), and EvaluateAndAddContainerQueries walks
// that chain (element_rule_collector.cc:288-316 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An @media around or inside an
// @container gates its rules too.
func TestContainerChainAllGate(t *testing.T) {
	const yes, no = "(min-width: 0)", "(min-width: 5000px)"
	for _, tc := range []struct{ css, want string }{
		{`@container ` + no + ` { @container ` + yes + ` { .t { color: red } } }`, "green"},
		{`@container ` + yes + ` { @container ` + no + ` { .t { color: red } } }`, "green"},
		{`@container ` + yes + ` { @container ` + yes + ` { .t { color: blue } } }`, "blue"},
		{`@container ` + yes + ` { @media ` + no + ` { .t { color: red } } }`, "green"},
		{`@media ` + no + ` { @container ` + yes + ` { .t { color: red } } }`, "green"},
		{`@media all { @container ` + yes + ` { .t { color: blue } } }`, "blue"},
	} {
		t.Run(tc.css, func(t *testing.T) {
			if got := containerColor(t, tc.css); got != tc.want {
				t.Errorf("#t color = %q, want %s", got, tc.want)
			}
		})
	}
}

// A matching @container rule cascades with every other rule, by layer,
// specificity and source order: ElementRuleCollector evaluates a rule's
// container queries while it collects (CollectMatchingRulesForListInternal,
// core/css/element_rule_collector.cc:562, container block :662-699) and adds
// it through DidMatchRule with its layer order (:701-702, SeekLayerOrder
// :345-354 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestContainerRulesCascadeInOrder(t *testing.T) {
	const yes = "(min-width: 0)"
	for _, tc := range []struct{ name, css, want string }{
		{"unlayered beats layered", `@layer a { @container ` + yes + ` { .t { color: red } } } .t { color: green }`, "green"},
		{"specificity", `@container ` + yes + ` { .t { color: red } } #t { color: green }`, "green"},
		{"specificity, container later", `#t { color: green } @container ` + yes + ` { .t { color: red } }`, "green"},
		{"source order", `@container ` + yes + ` { .t { color: red } } .t { color: green }`, "green"},
		// The @container rule wins where the cascade says it does.
		{"later container rule wins", `.t { color: red } @container ` + yes + ` { .t { color: green } }`, "green"},
		{"earlier container rule wins on specificity", `@container ` + yes + ` { #t { color: green } } .t { color: red }`, "green"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := containerColor(t, tc.css); got != tc.want {
				t.Errorf("#t color = %q, want %s", got, tc.want)
			}
		})
	}
}
