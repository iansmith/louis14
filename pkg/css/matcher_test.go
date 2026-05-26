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
			if got := matchesPseudoClass(tc.node, "dir(ltr)"); got != tc.ltrMatch {
				t.Errorf(":dir(ltr) match = %v, want %v", got, tc.ltrMatch)
			}
			if got := matchesPseudoClass(tc.node, "dir(rtl)"); got != tc.rtlMatch {
				t.Errorf(":dir(rtl) match = %v, want %v", got, tc.rtlMatch)
			}
			// Unknown argument never matches.
			if matchesPseudoClass(tc.node, "dir(foopy)") {
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
	if !matchesPseudoClass(node, "is(div, span)") {
		t.Error(":is(div, span) should match <div>")
	}
	if !matchesPseudoClass(node, "where(div, span)") {
		t.Error(":where(div, span) should match <div>")
	}

	// Pseudo-element inside :is() makes the whole :is() contextually invalid
	// for element matching, even if another branch (the * universal) would
	// otherwise match.
	if matchesPseudoClass(node, "is(*, ::before)") {
		t.Error(":is(*, ::before) must not match a real element (contextually invalid)")
	}
	if matchesPseudoClass(node, "where(*, ::before)") {
		t.Error(":where(*, ::before) must not match a real element (contextually invalid)")
	}

	// Legacy single-colon pseudo-element forms also trigger invalidation.
	if matchesPseudoClass(node, "is(div, :before)") {
		t.Error(":is(div, :before) must not match (legacy pseudo-element form)")
	}

	// Pseudo-element nested deeper inside a paren scope (e.g. a :not() arg)
	// is NOT a top-level pseudo-element of the :is() argument, so the :is()
	// is still valid. This keeps the paren-aware scanner consistent with the
	// selector parser's findTopLevelPseudoElement.
	if !matchesPseudoClass(node, "is(:not(span))") {
		t.Error(":is(:not(span)) should match <div> (no top-level pseudo-element)")
	}
}
