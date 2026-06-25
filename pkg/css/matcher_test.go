package css

import (
	"louis14/pkg/html"
	"testing"
)

func TestMatchesSelector_ElementSelector(t *testing.T) {
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
	}

	selector := Selector{Parts: []SelectorPart{{Element: "div"}}}

	if !MatchesSelector(node, selector) {
		t.Error("div should match selector 'div'")
	}

	selectorP := Selector{Parts: []SelectorPart{{Element: "p"}}}
	if MatchesSelector(node, selectorP) {
		t.Error("div should not match selector 'p'")
	}
}

func TestMatchesSelector_ClassSelector(t *testing.T) {
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
		},
	}

	selector := Selector{Parts: []SelectorPart{{Classes: []string{"highlight"}}}}

	if !MatchesSelector(node, selector) {
		t.Error("div with class='highlight' should match selector '.highlight'")
	}

	selectorOther := Selector{Parts: []SelectorPart{{Classes: []string{"other"}}}}
	if MatchesSelector(node, selectorOther) {
		t.Error("div with class='highlight' should not match selector '.other'")
	}
}

func TestMatchesSelector_IDSelector(t *testing.T) {
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"id": "header",
		},
	}

	selector := Selector{Parts: []SelectorPart{{ID: "header"}}}

	if !MatchesSelector(node, selector) {
		t.Error("div with id='header' should match selector '#header'")
	}

	selectorOther := Selector{Parts: []SelectorPart{{ID: "footer"}}}
	if MatchesSelector(node, selectorOther) {
		t.Error("div with id='header' should not match selector '#footer'")
	}
}

func TestFindMatchingRules(t *testing.T) {
	stylesheet, _ := ParseStylesheet(`
		div { color: red; }
		.highlight { background-color: yellow; }
		#header { width: 100px; }
	`, nil)

	// Create a div with class and id
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "highlight",
			"id":    "header",
		},
	}

	matches := FindMatchingRules(node, stylesheet, 800, 600)

	// Should match all three rules
	if len(matches) != 3 {
		t.Fatalf("expected 3 matching rules, got %d", len(matches))
	}

	// Check that we got the right rules
	foundElement := false
	foundClass := false
	foundID := false

	for _, rule := range matches {
		switch rule.Selector.Type {
		case ElementSelector:
			foundElement = true
		case ClassSelector:
			foundClass = true
		case IDSelector:
			foundID = true
		}
	}

	if !foundElement || !foundClass || !foundID {
		t.Error("not all expected rules were matched")
	}
}

func TestMatchesAttributeSelector_WordMatch(t *testing.T) {
	// ~= matches when the attribute is a whitespace-separated list and one word exactly equals the value
	tests := []struct {
		name     string
		attrVal  string
		selVal   string
		expected bool
	}{
		{"exact single word", "foo", "foo", true},
		{"word in list", "foo bar baz", "bar", true},
		{"first word", "foo bar", "foo", true},
		{"last word", "foo bar", "bar", true},
		{"no match substring", "foobar", "foo", false},
		{"no match partial", "foo-bar", "foo", false},
		{"empty attribute", "", "foo", false},
		{"multiple spaces", "foo  bar  baz", "bar", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &html.Node{
				Type:    html.ElementNode,
				TagName: "div",
				Attributes: map[string]string{
					"class": tt.attrVal,
				},
			}
			attr := AttributeSelector{Name: "class", Operator: "~=", Value: tt.selVal}
			got := matchesAttributeSelector(node, attr)
			if got != tt.expected {
				t.Errorf("[class~=%q] on %q: got %v, want %v", tt.selVal, tt.attrVal, got, tt.expected)
			}
		})
	}
}

func TestMatchesAttributeSelector_HyphenMatch(t *testing.T) {
	// |= matches when attribute value equals the value or starts with value followed by "-"
	tests := []struct {
		name     string
		attrVal  string
		selVal   string
		expected bool
	}{
		{"exact match", "en", "en", true},
		{"hyphen prefix", "en-US", "en", true},
		{"no match different", "fr", "en", false},
		{"no match longer", "enx", "en", false},
		{"no match substring", "ben-US", "en", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &html.Node{
				Type:    html.ElementNode,
				TagName: "div",
				Attributes: map[string]string{
					"lang": tt.attrVal,
				},
			}
			attr := AttributeSelector{Name: "lang", Operator: "|=", Value: tt.selVal}
			got := matchesAttributeSelector(node, attr)
			if got != tt.expected {
				t.Errorf("[lang|=%q] on %q: got %v, want %v", tt.selVal, tt.attrVal, got, tt.expected)
			}
		})
	}
}

func TestParseAndMatch_TildeEquals(t *testing.T) {
	// End-to-end: parse a stylesheet with ~= and match against a node
	stylesheet, err := ParseStylesheet(`[class~="bar"] { color: red; }`, nil)
	if err != nil {
		t.Fatal(err)
	}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "foo bar baz",
		},
	}
	matches := FindMatchingRules(node, stylesheet, 800, 600)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	// Should NOT match when no word matches
	node2 := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"class": "foobar",
		},
	}
	matches2 := FindMatchingRules(node2, stylesheet, 800, 600)
	if len(matches2) != 0 {
		t.Fatalf("expected 0 matches for 'foobar', got %d", len(matches2))
	}
}

func TestParseAndMatch_PipeEquals(t *testing.T) {
	stylesheet, err := ParseStylesheet(`[lang|="en"] { color: red; }`, nil)
	if err != nil {
		t.Fatal(err)
	}

	node := &html.Node{
		Type:       html.ElementNode,
		TagName:    "div",
		Attributes: map[string]string{"lang": "en-US"},
	}
	matches := FindMatchingRules(node, stylesheet, 800, 600)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'en-US', got %d", len(matches))
	}

	node2 := &html.Node{
		Type:       html.ElementNode,
		TagName:    "div",
		Attributes: map[string]string{"lang": "fr"},
	}
	matches2 := FindMatchingRules(node2, stylesheet, 800, 600)
	if len(matches2) != 0 {
		t.Fatalf("expected 0 matches for 'fr', got %d", len(matches2))
	}
}

func TestMatchesSelector_NoMatchTextNode(t *testing.T) {
	node := &html.Node{
		Type: html.TextNode,
		Text: "Hello",
	}

	selector := Selector{Type: ElementSelector, Value: "div"}

	if MatchesSelector(node, selector) {
		t.Error("text nodes should not match selectors")
	}
}

func TestPseudoClass_ParsesWithoutError(t *testing.T) {
	// :hover, :focus, :active, :visited rules must parse without error
	tests := []struct {
		name string
		css  string
	}{
		{"hover", `a:hover { color: red; }`},
		{"focus", `input:focus { border: 1px solid blue; }`},
		{"active", `button:active { background: gray; }`},
		{"visited", `a:visited { color: purple; }`},
		{"hover with class", `.link:hover { color: green; }`},
		{"hover with id", `#nav:hover { opacity: 1; }`},
		{"multiple pseudo", `a:hover:focus { color: red; }`},
		{"descendant with hover", `div a:hover { color: red; }`},
		{"child with hover", `ul > li:hover { color: blue; }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stylesheet, err := ParseStylesheet(tt.css, nil)
			if err != nil {
				t.Fatalf("ParseStylesheet returned error: %v", err)
			}
			if len(stylesheet.Rules) != 1 {
				t.Fatalf("expected 1 rule, got %d", len(stylesheet.Rules))
			}
		})
	}
}

func TestPseudoClass_NeverMatches(t *testing.T) {
	// :hover and friends should never match any element in a static renderer
	pseudoClasses := []string{"hover", "focus", "active", "visited"}

	for _, pc := range pseudoClasses {
		t.Run(pc, func(t *testing.T) {
			stylesheet, err := ParseStylesheet("a:"+pc+" { color: red; }", nil)
			if err != nil {
				t.Fatal(err)
			}

			node := &html.Node{
				Type:    html.ElementNode,
				TagName: "a",
				Attributes: map[string]string{
					"href": "http://example.com",
				},
			}

			matches := FindMatchingRules(node, stylesheet, 800, 600)
			if len(matches) != 0 {
				t.Errorf(":%s should never match in static renderer, got %d matches", pc, len(matches))
			}
		})
	}
}

func TestPseudoClass_NonHoverRulesStillMatch(t *testing.T) {
	// Rules without :hover in the same stylesheet should still work
	stylesheet, err := ParseStylesheet(`
		a { color: blue; }
		a:hover { color: red; }
	`, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(stylesheet.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(stylesheet.Rules))
	}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "a",
	}

	matches := FindMatchingRules(node, stylesheet, 800, 600)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (non-hover rule only), got %d", len(matches))
	}
}

func TestPseudoClass_SelectorParsing(t *testing.T) {
	// Verify the parsed selector structure
	sel := parseSelector("a:hover")
	if len(sel.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(sel.Parts))
	}
	part := sel.Parts[0]
	if part.Element != "a" {
		t.Errorf("expected element 'a', got %q", part.Element)
	}
	if len(part.PseudoClasses) != 1 || part.PseudoClasses[0] != "hover" {
		t.Errorf("expected PseudoClasses=[hover], got %v", part.PseudoClasses)
	}
}

func TestPseudoClass_Specificity(t *testing.T) {
	// Pseudo-classes should contribute to specificity (10 each, like classes)
	sel := parseSelector("a:hover")
	// a = 1 (element) + hover = 10 (pseudo-class) = 11
	if sel.Specificity != 11 {
		t.Errorf("expected specificity 11 for 'a:hover', got %d", sel.Specificity)
	}
}

// TestDirPseudoClass covers :dir(ltr)/:dir(rtl) for the static cases
// (explicit dir=ltr/rtl, inheritance, invalid values, default LTR).
// Dynamic dir-attribute changes and dir=auto first-strong-Unicode detection
// are out of scope for this matcher.
func TestDirPseudoClass(t *testing.T) {
	mkDiv := func(parent *html.Node, dir string) *html.Node {
		n := &html.Node{Type: html.ElementNode, TagName: "div"}
		if dir != "" {
			n.Attributes = map[string]string{"dir": dir}
		}
		if parent != nil {
			n.Parent = parent
			parent.Children = append(parent.Children, n)
		}
		return n
	}

	// Root <html dir="rtl">
	root := mkDiv(nil, "rtl")
	root.TagName = "html"

	// <div dir="ltr"> (under html dir=rtl): directionality ltr
	ltrDiv := mkDiv(root, "ltr")

	// <div> (under <html dir="rtl">) — inherits rtl
	inheritDiv := mkDiv(root, "")

	// <div dir="foopy"> — invalid value, inherits from parent (rtl)
	invalidDiv := mkDiv(root, "foopy")

	// <div dir="auto"> — out-of-scope first-strong; falls through to inherit rtl
	autoDiv := mkDiv(root, "auto")

	// Orphan element with no dir, no parent → default ltr
	orphan := &html.Node{Type: html.ElementNode, TagName: "div"}

	cases := []struct {
		name     string
		node     *html.Node
		ltrMatch bool
		rtlMatch bool
	}{
		{"explicit-rtl-root", root, false, true},
		{"explicit-ltr-child-of-rtl", ltrDiv, true, false},
		{"inherit-from-rtl-parent", inheritDiv, false, true},
		{"invalid-dir-inherits-rtl", invalidDiv, false, true},
		{"auto-inherits-rtl", autoDiv, false, true},
		{"orphan-default-ltr", orphan, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesPseudoClass(tc.node, "dir(ltr)", matchContext{}); got != tc.ltrMatch {
				t.Errorf(":dir(ltr) match = %v, want %v", got, tc.ltrMatch)
			}
			if got := matchesPseudoClass(tc.node, "dir(rtl)", matchContext{}); got != tc.rtlMatch {
				t.Errorf(":dir(rtl) match = %v, want %v", got, tc.rtlMatch)
			}
			// Unknown argument never matches.
			if matchesPseudoClass(tc.node, "dir(foopy)", matchContext{}) {
				t.Errorf(":dir(foopy) should never match")
			}
		})
	}
}

// TestDirPseudoClass_Auto covers the HTML "auto directionality" algorithm:
// `dir=auto` walks descendant text and uses the first character with a
// strong Unicode bidi class (L → ltr; R or AL → rtl). Descendant subtrees
// rooted at <bdi>/<script>/<style>/<textarea> or at any element with its
// own dir attribute are excluded from the scan.
func TestDirPseudoClass_Auto(t *testing.T) {
	mkElem := func(parent *html.Node, tag string, attrs map[string]string) *html.Node {
		n := &html.Node{Type: html.ElementNode, TagName: tag}
		if attrs != nil {
			n.Attributes = map[string]string{}
			for k, v := range attrs {
				n.Attributes[k] = v
			}
		}
		if parent != nil {
			n.Parent = parent
			parent.Children = append(parent.Children, n)
		}
		return n
	}
	mkText := func(parent *html.Node, text string) *html.Node {
		n := &html.Node{Type: html.TextNode, Text: text, Parent: parent}
		if parent != nil {
			parent.Children = append(parent.Children, n)
		}
		return n
	}

	// div1 dir=auto > "a"  → ltr (L strong character)
	div1 := mkElem(nil, "div", map[string]string{"dir": "auto"})
	mkText(div1, "a")
	// div2 dir=auto > "ת" (Hebrew tav, RTL)  → rtl
	div2 := mkElem(nil, "div", map[string]string{"dir": "auto"})
	mkText(div2, "ת")
	// div3 dir=auto > [div3_1 dir=rtl > "ת"] [text "a"]  → ltr
	// (div3_1 has its own dir, so it's skipped; "a" provides the strong L)
	div3 := mkElem(nil, "div", map[string]string{"dir": "auto"})
	d31 := mkElem(div3, "div", map[string]string{"dir": "rtl"})
	mkText(d31, "ת")
	mkText(div3, "a")
	// div4 dir=auto > <bdi>"a"</bdi> > "ת"  → rtl
	// (the <bdi> subtree is skipped, the trailing tav is the first strong)
	div4 := mkElem(nil, "div", map[string]string{"dir": "auto"})
	bdi := mkElem(div4, "bdi", nil)
	mkText(bdi, "a")
	mkText(div4, "ת")
	// div5 dir=auto with only whitespace → falls back to parent (none) → ltr
	div5 := mkElem(nil, "div", map[string]string{"dir": "auto"})
	mkText(div5, "   \n\t")

	cases := []struct {
		name string
		node *html.Node
		want Direction
	}{
		{"auto-strong-L", div1, DirectionLTR},
		{"auto-strong-R", div2, DirectionRTL},
		{"auto-skip-dir-subtree", div3, DirectionLTR},
		{"auto-skip-bdi-subtree", div4, DirectionRTL},
		{"auto-whitespace-only", div5, DirectionLTR},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := elementDirectionality(tc.node); got != tc.want {
				t.Errorf("elementDirectionality = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMatchesIsWithPseudoElement guards the contextually-invalid rule from
// CSS Selectors 4 + CSS Nesting: an :is() or :where() that contains a pseudo-
// element argument must not match real elements. This is what makes the WPT
// contextually-invalid-selectors-{001,002,003} tests pass — without it the
// nested rule `*, ::before { & * { color: red } }` expands to
// `:is(*, ::before) *` and would wrongly paint <div>'s color red.
func TestMatchesIsWithPseudoElement(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, TagName: "div"}

	// Sanity: :is() with only normal selectors still matches.
	if !matchesPseudoClass(node, "is(div, span)", matchContext{}) {
		t.Error(":is(div, span) should match <div>")
	}
	if !matchesPseudoClass(node, "where(div, span)", matchContext{}) {
		t.Error(":where(div, span) should match <div>")
	}

	// Pseudo-element inside :is() makes the whole :is() contextually invalid
	// for element matching, even if another branch (the * universal) would
	// otherwise match.
	if matchesPseudoClass(node, "is(*, ::before)", matchContext{}) {
		t.Error(":is(*, ::before) must not match a real element (contextually invalid)")
	}
	if matchesPseudoClass(node, "where(*, ::before)", matchContext{}) {
		t.Error(":where(*, ::before) must not match a real element (contextually invalid)")
	}

	// Legacy single-colon pseudo-element forms also trigger invalidation.
	if matchesPseudoClass(node, "is(div, :before)", matchContext{}) {
		t.Error(":is(div, :before) must not match (legacy pseudo-element form)")
	}

	// Pseudo-element nested deeper inside a paren scope (e.g. a :not() arg)
	// is NOT a top-level pseudo-element of the :is() argument, so the :is()
	// is still valid. This keeps the paren-aware scanner consistent with the
	// selector parser's findTopLevelPseudoElement.
	if !matchesPseudoClass(node, "is(:not(span))", matchContext{}) {
		t.Error(":is(:not(span)) should match <div> (no top-level pseudo-element)")
	}
}

// TestFormStatePseudos covers :required / :optional / :read-write /
// :read-only / :placeholder-shown (CSS Selectors 4 §6) for the static reftest
// shape we care about. Each case states an authored markup snippet and the
// expected matching answer for the predicate. Mirrors Blink core/html/forms/
// html_input_element.cc::IsRequired / IsPlaceholderVisible at
// 4883d11fef4a — louis14's matcher reads the parsed initial state, not the
// element-level invalidation graph.
func TestFormStatePseudos(t *testing.T) {
	mkInput := func(attrs map[string]string) *html.Node {
		if attrs == nil {
			attrs = map[string]string{}
		}
		return &html.Node{
			Type:       html.ElementNode,
			TagName:    "input",
			Attributes: attrs,
		}
	}

	// :required and :optional
	requiredCases := []struct {
		name     string
		node     *html.Node
		required bool
		optional bool
	}{
		{"input no required", mkInput(nil), false, true},
		{"input required", mkInput(map[string]string{"required": ""}), true, false},
		{"input type=hidden required (not eligible)", mkInput(map[string]string{"type": "hidden", "required": ""}), false, false},
		{"input type=submit required (not eligible)", mkInput(map[string]string{"type": "submit", "required": ""}), false, false},
		{"select required", &html.Node{Type: html.ElementNode, TagName: "select", Attributes: map[string]string{"required": ""}}, true, false},
		{"textarea no required", &html.Node{Type: html.ElementNode, TagName: "textarea"}, false, true},
		{"div not a form control", &html.Node{Type: html.ElementNode, TagName: "div"}, false, false},
	}
	for _, tc := range requiredCases {
		t.Run("required/"+tc.name, func(t *testing.T) {
			if matchesPseudoClass(tc.node, "required", matchContext{}) != tc.required {
				t.Errorf(":required expected %v", tc.required)
			}
			if matchesPseudoClass(tc.node, "optional", matchContext{}) != tc.optional {
				t.Errorf(":optional expected %v", tc.optional)
			}
		})
	}

	// :read-write / :read-only
	rwCases := []struct {
		name      string
		node      *html.Node
		readWrite bool
	}{
		{"plain input", mkInput(nil), true},
		{"input readonly", mkInput(map[string]string{"readonly": ""}), false},
		{"input disabled", mkInput(map[string]string{"disabled": ""}), false},
		{"input type=button", mkInput(map[string]string{"type": "button"}), false},
		{"input type=checkbox", mkInput(map[string]string{"type": "checkbox"}), false},
		{"textarea editable", &html.Node{Type: html.ElementNode, TagName: "textarea"}, true},
		{"textarea readonly", &html.Node{Type: html.ElementNode, TagName: "textarea", Attributes: map[string]string{"readonly": ""}}, false},
		{"div without contenteditable", &html.Node{Type: html.ElementNode, TagName: "div"}, false},
		{"div contenteditable=true", &html.Node{Type: html.ElementNode, TagName: "div", Attributes: map[string]string{"contenteditable": "true"}}, true},
		{"div contenteditable=false", &html.Node{Type: html.ElementNode, TagName: "div", Attributes: map[string]string{"contenteditable": "false"}}, false},
	}
	for _, tc := range rwCases {
		t.Run("read-write/"+tc.name, func(t *testing.T) {
			if got := matchesPseudoClass(tc.node, "read-write", matchContext{}); got != tc.readWrite {
				t.Errorf(":read-write expected %v got %v", tc.readWrite, got)
			}
			if got := matchesPseudoClass(tc.node, "read-only", matchContext{}); got == tc.readWrite {
				t.Errorf(":read-only must be the negation of :read-write (case %q)", tc.name)
			}
		})
	}

	// :placeholder-shown
	psCases := []struct {
		name string
		node *html.Node
		want bool
	}{
		{"input with placeholder, empty value", mkInput(map[string]string{"placeholder": "type here"}), true},
		{"input with placeholder, non-empty value",
			mkInput(map[string]string{"placeholder": "type here", "value": "hi"}), false},
		{"input no placeholder", mkInput(nil), false},
		{"input type=hidden with placeholder", mkInput(map[string]string{"type": "hidden", "placeholder": "x"}), false},
		{"input type=checkbox with placeholder", mkInput(map[string]string{"type": "checkbox", "placeholder": "x"}), false},
	}
	for _, tc := range psCases {
		t.Run("placeholder-shown/"+tc.name, func(t *testing.T) {
			if got := matchesPseudoClass(tc.node, "placeholder-shown", matchContext{}); got != tc.want {
				t.Errorf(":placeholder-shown expected %v got %v", tc.want, got)
			}
		})
	}
}

// TestParseAnPlusBValid documents the strict An+B grammar per CSS Syntax §3
// / Selectors 4 §6.6.1. The pre-existing permissive parser accepted garbage
// like "n of" → (1, 0) which caused `:nth-child(n of)` (an invalid selector)
// to match every element. Mirrors Blink's CSSSelectorParser::ConsumeANPlusB
// @ 4883d11fef4a, which rejects the same forms.
func TestParseAnPlusBValid(t *testing.T) {
	cases := []struct {
		in    string
		a, b  int
		valid bool
	}{
		// Valid forms
		{"1", 0, 1, true},
		{"+1", 0, 1, true},
		{"-2", 0, -2, true},
		{"n", 1, 0, true},
		{"-n", -1, 0, true},
		{"+n", 1, 0, true},
		{"2n", 2, 0, true},
		{"2n+1", 2, 1, true},
		{"2n + 1", 2, 1, true},
		{"-n+3", -1, 3, true},
		{"-n + 3", -1, 3, true},
		// Invalid: whitespace inside the dimension
		{"1 n", 0, 0, false},
		{"2 n + 1", 0, 0, false},
		// Invalid: trailing garbage in the of-form (handled upstream by
		// splitNthChildArg, but parser must still reject when invoked alone)
		{"1 of", 0, 0, false},
		{"n of", 0, 0, false},
		{"even of", 0, 0, false},
		{"n + 1of", 0, 0, false},
		// Invalid: empty
		{"", 0, 0, false},
		// Invalid: pure-B form with extra after
		{"1 foo", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			a, b, ok := parseAnPlusBValid(tc.in)
			if ok != tc.valid {
				t.Fatalf("validity: want %v got %v", tc.valid, ok)
			}
			if !ok {
				return
			}
			if a != tc.a || b != tc.b {
				t.Errorf("(a,b): want (%d,%d) got (%d,%d)", tc.a, tc.b, a, b)
			}
		})
	}
}

// TestMatchesIsLikeFullComplex documents that :is() / :where() must walk the
// full complex-selector chain inside each branch, not just the rightmost
// compound. The pre-fix `matchesIsLike` checked only the last part, so
// `:is(#d + div, #d ~ #h)` matched every `div` instead of only the one
// adjacent to `#d`. Mirrors Blink's SelectorChecker dispatch through
// MatchSelector for each :is() branch.
// TestLinkVisitedMutualExclusion verifies CSS Selectors 4 §6.6.1: in general
// matching, :link matches only an UNVISITED hyperlink and :visited matches
// only a VISITED one (they are mutually exclusive). louis14 treats <a href="">
// as the sole visited link (its href resolves to the current document URL).
// Mirrors Blink SelectorChecker::CheckPseudoClass's LINK/VISITED arms gated by
// MatchesLink/MatchesVisited @ 4883d11fef4a.
func TestLinkVisitedMutualExclusion(t *testing.T) {
	visited := &html.Node{Type: html.ElementNode, TagName: "a", Attributes: map[string]string{"href": ""}}
	unvisited := &html.Node{Type: html.ElementNode, TagName: "a", Attributes: map[string]string{"href": "unvisited"}}

	linkSel := ParseSelector("a:link")
	visitedSel := ParseSelector("a:visited")

	if MatchesSelector(visited, linkSel) {
		t.Error(`a:link must NOT match a visited link (href="")`)
	}
	if !MatchesSelector(unvisited, linkSel) {
		t.Error(`a:link must match an unvisited link (href="unvisited")`)
	}
	if !MatchesSelector(visited, visitedSel) {
		t.Error(`a:visited must match a visited link (href="")`)
	}
	if MatchesSelector(unvisited, visitedSel) {
		t.Error(`a:visited must NOT match an unvisited link`)
	}
}

// TestLinkVisitedInsideHas verifies the CSS Selectors 4 §6.6.1.1 privacy
// carve-out: inside :has(), :link matches ANY hyperlink and :visited matches
// NONE (to avoid a history side channel). Mirrors Blink's
// SelectorChecker::CheckPseudoHas history-leak guard @ 4883d11fef4a.
func TestLinkVisitedInsideHas(t *testing.T) {
	// <div id=parent1><a href=""></a></div>  (contains a visited link)
	parent1 := &html.Node{Type: html.ElementNode, TagName: "div", Attributes: map[string]string{"id": "parent1"}}
	vlink := &html.Node{Type: html.ElementNode, TagName: "a", Attributes: map[string]string{"href": ""}, Parent: parent1}
	parent1.Children = []*html.Node{vlink}

	// <div id=parent2><a href="unvisited"></a></div>  (only an unvisited link)
	parent2 := &html.Node{Type: html.ElementNode, TagName: "div", Attributes: map[string]string{"id": "parent2"}}
	ulink := &html.Node{Type: html.ElementNode, TagName: "a", Attributes: map[string]string{"href": "unvisited"}, Parent: parent2}
	parent2.Children = []*html.Node{ulink}

	hasLink := ParseSelector("#parent1:has(:link)")
	if !MatchesSelector(parent1, hasLink) {
		t.Error(":has(:link) must match a parent containing a visited link (inside :has, :link matches any link)")
	}

	hasVisited := ParseSelector("#parent2:has(:visited)")
	if MatchesSelector(parent2, hasVisited) {
		t.Error(":has(:visited) must NOT match (inside :has, :visited never matches — privacy)")
	}
}

func TestMatchesIsLikeFullComplex(t *testing.T) {
	// Build: <main><div id=a></div><div id=b></div><div id=d></div><div id=e></div></main>
	main := &html.Node{Type: html.ElementNode, TagName: "main"}
	a := &html.Node{Type: html.ElementNode, TagName: "div", Attributes: map[string]string{"id": "a"}, Parent: main}
	b := &html.Node{Type: html.ElementNode, TagName: "div", Attributes: map[string]string{"id": "b"}, Parent: main}
	d := &html.Node{Type: html.ElementNode, TagName: "div", Attributes: map[string]string{"id": "d"}, Parent: main}
	e := &html.Node{Type: html.ElementNode, TagName: "div", Attributes: map[string]string{"id": "e"}, Parent: main}
	main.Children = []*html.Node{a, b, d, e}

	// :is(#d + div, #d ~ #h) — only #e is "div adjacent to #d"; #h doesn't exist.
	if !matchesIsLike(e, "#d + div, #d ~ #h", matchContext{}) {
		t.Error("#e should match :is(#d + div, ...) as the adjacent sibling of #d")
	}
	if matchesIsLike(a, "#d + div, #d ~ #h", matchContext{}) {
		t.Error("#a must NOT match :is(#d + div, #d ~ #h) — only the rightmost compound was checked under the old impl")
	}
	if matchesIsLike(b, "#d + div, #d ~ #h", matchContext{}) {
		t.Error("#b must NOT match :is(#d + div, #d ~ #h)")
	}
}
