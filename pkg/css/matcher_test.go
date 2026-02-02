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
	`)

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
	stylesheet, err := ParseStylesheet(`[class~="bar"] { color: red; }`)
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
	stylesheet, err := ParseStylesheet(`[lang|="en"] { color: red; }`)
	if err != nil {
		t.Fatal(err)
	}

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{"lang": "en-US"},
	}
	matches := FindMatchingRules(node, stylesheet, 800, 600)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'en-US', got %d", len(matches))
	}

	node2 := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
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
			stylesheet, err := ParseStylesheet(tt.css)
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
			stylesheet, err := ParseStylesheet("a:" + pc + " { color: red; }")
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
	`)
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
