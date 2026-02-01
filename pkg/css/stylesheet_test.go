package css

import "testing"

// Phase 3 tests: Stylesheet parsing

func TestParseStylesheet_SingleRule(t *testing.T) {
	css := `div { color: red; }`
	stylesheet, err := ParseStylesheet(css)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stylesheet.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(stylesheet.Rules))
	}

	rule := stylesheet.Rules[0]
	if rule.Selector.Type != ElementSelector {
		t.Errorf("expected ElementSelector, got %v", rule.Selector.Type)
	}

	if rule.Selector.Value != "div" {
		t.Errorf("expected selector 'div', got '%s'", rule.Selector.Value)
	}

	if rule.Declarations["color"] != "red" {
		t.Errorf("expected color='red', got '%s'", rule.Declarations["color"])
	}
}

func TestParseStylesheet_MultipleRules(t *testing.T) {
	css := `
		div { color: red; }
		p { color: blue; }
		span { color: green; }
	`
	stylesheet, err := ParseStylesheet(css)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stylesheet.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(stylesheet.Rules))
	}

	// Check each rule
	expected := []struct {
		selector string
		color    string
	}{
		{"div", "red"},
		{"p", "blue"},
		{"span", "green"},
	}

	for i, exp := range expected {
		if stylesheet.Rules[i].Selector.Value != exp.selector {
			t.Errorf("rule %d: expected selector '%s', got '%s'", i, exp.selector, stylesheet.Rules[i].Selector.Value)
		}
		if stylesheet.Rules[i].Declarations["color"] != exp.color {
			t.Errorf("rule %d: expected color '%s', got '%s'", i, exp.color, stylesheet.Rules[i].Declarations["color"])
		}
	}
}

func TestParseSelector_ElementSelector(t *testing.T) {
	selector := parseSelector("div")

	if selector.Type != ElementSelector {
		t.Errorf("expected ElementSelector, got %v", selector.Type)
	}

	if selector.Value != "div" {
		t.Errorf("expected value 'div', got '%s'", selector.Value)
	}

	if selector.Specificity != 1 {
		t.Errorf("expected specificity 1, got %d", selector.Specificity)
	}
}

func TestParseSelector_ClassSelector(t *testing.T) {
	selector := parseSelector(".myclass")

	if selector.Type != ClassSelector {
		t.Errorf("expected ClassSelector, got %v", selector.Type)
	}

	if selector.Value != "myclass" {
		t.Errorf("expected value 'myclass', got '%s'", selector.Value)
	}

	if selector.Specificity != 10 {
		t.Errorf("expected specificity 10, got %d", selector.Specificity)
	}
}

func TestParseSelector_IDSelector(t *testing.T) {
	selector := parseSelector("#myid")

	if selector.Type != IDSelector {
		t.Errorf("expected IDSelector, got %v", selector.Type)
	}

	if selector.Value != "myid" {
		t.Errorf("expected value 'myid', got '%s'", selector.Value)
	}

	if selector.Specificity != 100 {
		t.Errorf("expected specificity 100, got %d", selector.Specificity)
	}
}

func TestParseStylesheet_ClassSelector(t *testing.T) {
	css := `.highlight { background-color: yellow; }`
	stylesheet, err := ParseStylesheet(css)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stylesheet.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(stylesheet.Rules))
	}

	rule := stylesheet.Rules[0]
	if rule.Selector.Type != ClassSelector {
		t.Errorf("expected ClassSelector, got %v", rule.Selector.Type)
	}

	if rule.Selector.Value != "highlight" {
		t.Errorf("expected selector 'highlight', got '%s'", rule.Selector.Value)
	}
}

func TestParseStylesheet_IDSelector(t *testing.T) {
	css := `#header { width: 100px; }`
	stylesheet, err := ParseStylesheet(css)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stylesheet.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(stylesheet.Rules))
	}

	rule := stylesheet.Rules[0]
	if rule.Selector.Type != IDSelector {
		t.Errorf("expected IDSelector, got %v", rule.Selector.Type)
	}

	if rule.Selector.Value != "header" {
		t.Errorf("expected selector 'header', got '%s'", rule.Selector.Value)
	}
}

func TestParseStylesheet_MultipleProperties(t *testing.T) {
	css := `div { color: red; width: 100px; height: 50px; }`
	stylesheet, err := ParseStylesheet(css)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rule := stylesheet.Rules[0]

	if rule.Declarations["color"] != "red" {
		t.Errorf("expected color='red'")
	}

	if rule.Declarations["width"] != "100px" {
		t.Errorf("expected width='100px'")
	}

	if rule.Declarations["height"] != "50px" {
		t.Errorf("expected height='50px'")
	}
}
