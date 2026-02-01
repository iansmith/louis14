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

	selector := Selector{Type: ElementSelector, Value: "div"}

	if !MatchesSelector(node, selector) {
		t.Error("div should match selector 'div'")
	}

	selectorP := Selector{Type: ElementSelector, Value: "p"}
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

	selector := Selector{Type: ClassSelector, Value: "highlight"}

	if !MatchesSelector(node, selector) {
		t.Error("div with class='highlight' should match selector '.highlight'")
	}

	selectorOther := Selector{Type: ClassSelector, Value: "other"}
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

	selector := Selector{Type: IDSelector, Value: "header"}

	if !MatchesSelector(node, selector) {
		t.Error("div with id='header' should match selector '#header'")
	}

	selectorOther := Selector{Type: IDSelector, Value: "footer"}
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

	matches := FindMatchingRules(node, stylesheet)

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
