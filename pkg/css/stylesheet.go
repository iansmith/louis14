package css

import (
	"fmt"
	"strings"
)

// Phase 3: CSS stylesheet structures

// Selector represents a CSS selector
type Selector struct {
	Raw        string       // Original selector string
	Type       SelectorType // Type of selector
	Value      string       // The actual value (element name, class name, or id)
	Specificity int         // Specificity score for cascade
}

type SelectorType int

const (
	ElementSelector SelectorType = iota // div, p, span
	ClassSelector                       // .classname
	IDSelector                          // #idname
)

// Rule represents a CSS rule (selector + declarations)
type Rule struct {
	Selector     Selector
	Declarations map[string]string // property -> value
}

// Stylesheet represents a parsed CSS stylesheet
type Stylesheet struct {
	Rules []Rule
}

// ParseStylesheet parses CSS stylesheet content into rules
func ParseStylesheet(css string) (*Stylesheet, error) {
	stylesheet := &Stylesheet{
		Rules: make([]Rule, 0),
	}

	// Simple parser: split by } to get rules
	css = strings.TrimSpace(css)
	if css == "" {
		return stylesheet, nil
	}

	// Find each rule (selector { declarations })
	rules := splitRules(css)

	for _, ruleStr := range rules {
		rule, err := parseRule(ruleStr)
		if err != nil {
			// Skip malformed rules
			continue
		}
		stylesheet.Rules = append(stylesheet.Rules, rule)
	}

	return stylesheet, nil
}

// splitRules splits CSS into individual rules
func splitRules(css string) []string {
	rules := make([]string, 0)
	depth := 0
	start := 0

	for i, ch := range css {
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				// Found complete rule
				ruleStr := css[start : i+1]
				if strings.TrimSpace(ruleStr) != "" {
					rules = append(rules, ruleStr)
				}
				start = i + 1
			}
		}
	}

	return rules
}

// parseRule parses a single CSS rule
func parseRule(ruleStr string) (Rule, error) {
	// Find the opening brace
	bracePos := strings.Index(ruleStr, "{")
	if bracePos == -1 {
		return Rule{}, fmt.Errorf("no opening brace found")
	}

	// Extract selector
	selectorStr := strings.TrimSpace(ruleStr[:bracePos])
	selector := parseSelector(selectorStr)

	// Extract declarations (between { and })
	declStart := bracePos + 1
	declEnd := strings.LastIndex(ruleStr, "}")
	if declEnd == -1 {
		declEnd = len(ruleStr)
	}

	declStr := ruleStr[declStart:declEnd]
	declarations := parseDeclarations(declStr)

	return Rule{
		Selector:     selector,
		Declarations: declarations,
	}, nil
}

// parseSelector parses a selector string and determines its type
func parseSelector(selectorStr string) Selector {
	selectorStr = strings.TrimSpace(selectorStr)

	if selectorStr == "" {
		return Selector{Type: ElementSelector, Value: "", Raw: ""}
	}

	// Check for ID selector (#id)
	if strings.HasPrefix(selectorStr, "#") {
		return Selector{
			Type:        IDSelector,
			Value:       selectorStr[1:], // Remove #
			Raw:         selectorStr,
			Specificity: 100, // ID has high specificity
		}
	}

	// Check for class selector (.class)
	if strings.HasPrefix(selectorStr, ".") {
		return Selector{
			Type:        ClassSelector,
			Value:       selectorStr[1:], // Remove .
			Raw:         selectorStr,
			Specificity: 10, // Class has medium specificity
		}
	}

	// Element selector (div, p, etc.)
	return Selector{
		Type:        ElementSelector,
		Value:       selectorStr,
		Raw:         selectorStr,
		Specificity: 1, // Element has low specificity
	}
}

// parseDeclarations parses CSS declarations into a map
func parseDeclarations(declStr string) map[string]string {
	declarations := make(map[string]string)

	// Split by semicolon
	parts := strings.Split(declStr, ";")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split property: value
		colonPos := strings.Index(part, ":")
		if colonPos == -1 {
			continue
		}

		property := strings.TrimSpace(part[:colonPos])
		value := strings.TrimSpace(part[colonPos+1:])

		if property != "" && value != "" {
			// Expand shorthand properties (reuse from Phase 2)
			style := NewStyle()
			expandShorthand(style, property, value)

			// Copy all expanded properties to declarations
			for k, v := range style.Properties {
				declarations[k] = v
			}
		}
	}

	return declarations
}
