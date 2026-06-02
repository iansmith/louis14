package css

import "testing"

// Phase 3 tests: Stylesheet parsing

func TestParseStylesheet_SingleRule(t *testing.T) {
	css := `div { color: red; }`
	stylesheet, err := ParseStylesheet(css, nil)

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
	stylesheet, err := ParseStylesheet(css, nil)

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
	stylesheet, err := ParseStylesheet(css, nil)

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
	stylesheet, err := ParseStylesheet(css, nil)

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
	stylesheet, err := ParseStylesheet(css, nil)

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

// TestParseSelector_WebkitScrollbarPseudoElements guards against the
// ::-webkit-scrollbar* selector leak (css-scrollbars). Blink's
// CSSSelectorParser::ConsumePseudo classifies these vendor-prefixed tokens as
// pseudo-elements. If parseSelector leaves PseudoElement empty, the rule's
// Parts list ends up empty, matchesCompoundSelector treats it as a universal
// match, and the declarations leak onto every element. With the tokens
// registered, PseudoElement is non-empty so FindMatchingRules skips the rule
// from the normal element cascade (louis14 paints no scrollbar pseudos).
func TestParseSelector_WebkitScrollbarPseudoElements(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"::-webkit-scrollbar", "-webkit-scrollbar"},
		{"::-webkit-scrollbar-button", "-webkit-scrollbar-button"},
		{"::-webkit-scrollbar-track", "-webkit-scrollbar-track"},
		{"::-webkit-scrollbar-track-piece", "-webkit-scrollbar-track-piece"},
		{"::-webkit-scrollbar-thumb", "-webkit-scrollbar-thumb"},
		{"::-webkit-scrollbar-corner", "-webkit-scrollbar-corner"},
		{"::-webkit-resizer", "-webkit-resizer"},
		// Compound: the pseudo-element token must be peeled off whole, leaving
		// only the originating compound (.container) in the Parts list.
		{".container::-webkit-scrollbar-corner", "-webkit-scrollbar-corner"},
		{":root::-webkit-scrollbar", "-webkit-scrollbar"},
	}
	for _, tc := range cases {
		sel := parseSelector(tc.raw)
		if sel.PseudoElement != tc.want {
			t.Errorf("parseSelector(%q): PseudoElement = %q, want %q",
				tc.raw, sel.PseudoElement, tc.want)
		}
	}
}

// TestParseKeyframesRule_ImportantDropped verifies that a !important declaration
// inside a @keyframes block is dropped per CSS Animations §4.1, so it cannot
// overwrite a prior normal declaration of the same property.
//
// Blink analog: CSSParserImpl::ConsumeKeyframeStyleRule drops !important
// keyframe declarations (third_party/blink/renderer/core/css/parser/css_parser_impl.cc
// @ Chromium 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func TestParseKeyframesRule_ImportantDropped(t *testing.T) {
	// `border-color:green` is a normal declaration; `border-color:red !important`
	// must be ignored. The keyframe must resolve to green.
	css := `@keyframes override {
		from, to {
			border-color: green;
			border-color: red !important;
		}
	}`
	ss, err := ParseStylesheet(css, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet error: %v", err)
	}
	frames, ok := ss.Keyframes["override"]
	if !ok {
		t.Fatal("keyframe 'override' not found")
	}
	if len(frames) == 0 {
		t.Fatal("no keyframe stops parsed")
	}
	// Check each stop — border-color longhands should be green (from the normal
	// declaration), not red (from the dropped !important declaration).
	for _, frame := range frames {
		for _, longhand := range []string{"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"} {
			got := frame.Declarations[longhand]
			if got != "green" {
				t.Errorf("stop %q: %s = %q, want %q (important declaration must be dropped)", frame.Stop, longhand, got, "green")
			}
		}
	}
}
