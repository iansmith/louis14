package css

import (
	"fmt"
	"strings"
)

// Phase 3: CSS stylesheet structures

// Phase 17: Enhanced selector system for complex selectors

// Selector represents a CSS selector which may be compound (multiple parts with combinators)
type Selector struct {
	Raw           string             // Original selector string
	Parts         []SelectorPart     // Parts of a compound selector
	Combinators   []CombinatorType   // Combinators between parts (len = len(Parts)-1)
	Specificity   int                // Specificity score for cascade
	PseudoElement string             // Phase 11: Pseudo-element (::before, ::after)

	// Legacy fields for backward compatibility with simple selectors
	Type  SelectorType // Deprecated: use Parts instead
	Value string       // Deprecated: use Parts instead
}

// SelectorPart represents a single part of a compound selector (e.g., "div.class1.class2")
type SelectorPart struct {
	Element    string   // Element name ("div", "p", "*" for universal, "" for none)
	Classes    []string // Class names (without the .)
	ID         string   // ID (without the #)
	Attributes []AttributeSelector // Attribute selectors
}

// AttributeSelector represents an attribute selector like [type="text"]
type AttributeSelector struct {
	Name     string // Attribute name
	Operator string // =, ^=, $=, *=, ~=, |=
	Value    string // Attribute value
}

// CombinatorType represents the type of combinator between selector parts
type CombinatorType int

const (
	DescendantCombinator CombinatorType = iota // space: .parent .child
	ChildCombinator                            // >: .parent > .child
	AdjacentSiblingCombinator                  // +: .box + .box
	GeneralSiblingCombinator                   // ~: .box ~ .box
)

// Legacy: keep for backward compatibility with simple selectors
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
	MediaQuery   *MediaQuery       // Phase 22: Optional media query wrapper
}

// Phase 22: MediaQuery represents a @media rule condition
type MediaQuery struct {
	MediaType  string            // "screen", "print", "all", etc.
	Conditions []MediaCondition  // min-width, max-width, etc.
}

// Phase 22: MediaCondition represents a single media query condition
type MediaCondition struct {
	Feature string  // "min-width", "max-width", "orientation", etc.
	Value   string  // "768px", "landscape", etc.
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
		// Phase 22: Check if this is a @media rule
		if strings.HasPrefix(strings.TrimSpace(ruleStr), "@media") {
			mediaRules := parseMediaRule(ruleStr)
			stylesheet.Rules = append(stylesheet.Rules, mediaRules...)
		} else {
			rule, err := parseRule(ruleStr)
			if err != nil {
				// Skip malformed rules
				continue
			}
			stylesheet.Rules = append(stylesheet.Rules, rule)
		}
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

// Phase 22: parseMediaRule parses a @media rule and returns its inner rules
func parseMediaRule(ruleStr string) []Rule {
	rules := make([]Rule, 0)

	// Find the opening brace
	bracePos := strings.Index(ruleStr, "{")
	if bracePos == -1 {
		return rules
	}

	// Extract media query string: @media (conditions)
	mediaStr := strings.TrimSpace(ruleStr[:bracePos])
	mediaQuery := parseMediaQuery(mediaStr)

	// Extract inner CSS (between outermost { and })
	innerStart := bracePos + 1
	innerEnd := strings.LastIndex(ruleStr, "}")
	if innerEnd == -1 || innerEnd <= innerStart {
		return rules
	}

	innerCSS := ruleStr[innerStart:innerEnd]

	// Parse inner rules
	innerRules := splitRules(innerCSS)

	for _, innerRuleStr := range innerRules {
		rule, err := parseRule(innerRuleStr)
		if err != nil {
			continue
		}
		// Attach media query to this rule
		rule.MediaQuery = mediaQuery
		rules = append(rules, rule)
	}

	return rules
}

// Phase 22: parseMediaQuery parses a media query string like "@media screen and (min-width: 768px)"
func parseMediaQuery(mediaStr string) *MediaQuery {
	// Remove @media prefix
	mediaStr = strings.TrimPrefix(mediaStr, "@media")
	mediaStr = strings.TrimSpace(mediaStr)

	mq := &MediaQuery{
		MediaType:  "all",
		Conditions: make([]MediaCondition, 0),
	}

	// Check for media type (screen, print, all, etc.)
	if strings.HasPrefix(mediaStr, "screen") {
		mq.MediaType = "screen"
		mediaStr = strings.TrimPrefix(mediaStr, "screen")
		mediaStr = strings.TrimSpace(mediaStr)
	} else if strings.HasPrefix(mediaStr, "print") {
		mq.MediaType = "print"
		mediaStr = strings.TrimPrefix(mediaStr, "print")
		mediaStr = strings.TrimSpace(mediaStr)
	}

	// Remove "and" keyword
	mediaStr = strings.TrimPrefix(mediaStr, "and")
	mediaStr = strings.TrimSpace(mediaStr)

	// Parse conditions: (min-width: 768px) and (max-width: 1024px)
	// Simple approach: split by "and" and extract each condition
	conditionStrs := strings.Split(mediaStr, "and")

	for _, condStr := range conditionStrs {
		condStr = strings.TrimSpace(condStr)
		if condStr == "" {
			continue
		}

		// Remove parentheses
		condStr = strings.Trim(condStr, "()")
		condStr = strings.TrimSpace(condStr)

		// Split by : to get feature and value
		parts := strings.SplitN(condStr, ":", 2)
		if len(parts) == 2 {
			feature := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			mq.Conditions = append(mq.Conditions, MediaCondition{
				Feature: feature,
				Value:   value,
			})
		}
	}

	return mq
}

// Phase 17: parseSelector parses a complex CSS selector
func parseSelector(selectorStr string) Selector {
	selectorStr = strings.TrimSpace(selectorStr)

	if selectorStr == "" {
		return Selector{Raw: "", Parts: []SelectorPart{}, Specificity: 0}
	}

	// Phase 11: Check for pseudo-element (::before, ::after)
	pseudoElement := ""
	if strings.Contains(selectorStr, "::before") {
		pseudoElement = "before"
		selectorStr = strings.Replace(selectorStr, "::before", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	} else if strings.Contains(selectorStr, "::after") {
		pseudoElement = "after"
		selectorStr = strings.Replace(selectorStr, "::after", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	}

	// Split by combinators while preserving them
	parts := make([]SelectorPart, 0)
	combinators := make([]CombinatorType, 0)

	// Tokenize the selector
	tokens := tokenizeSelector(selectorStr)

	// Build selector parts and combinators
	currentPart := ""
	for _, token := range tokens {
		switch token {
		case ">":
			if currentPart != "" {
				parts = append(parts, parseSelectorPart(currentPart))
				currentPart = ""
			}
			combinators = append(combinators, ChildCombinator)
		case "+":
			if currentPart != "" {
				parts = append(parts, parseSelectorPart(currentPart))
				currentPart = ""
			}
			combinators = append(combinators, AdjacentSiblingCombinator)
		case "~":
			if currentPart != "" {
				parts = append(parts, parseSelectorPart(currentPart))
				currentPart = ""
			}
			combinators = append(combinators, GeneralSiblingCombinator)
		case " ":
			// Descendant combinator (space)
			if currentPart != "" {
				parts = append(parts, parseSelectorPart(currentPart))
				currentPart = ""
				combinators = append(combinators, DescendantCombinator)
			}
		default:
			currentPart += token
		}
	}

	// Add final part
	if currentPart != "" {
		parts = append(parts, parseSelectorPart(currentPart))
	}

	// Calculate specificity: count IDs (100), classes (10), elements (1)
	specificity := 0
	for _, part := range parts {
		if part.ID != "" {
			specificity += 100
		}
		specificity += len(part.Classes) * 10
		specificity += len(part.Attributes) * 10
		if part.Element != "" && part.Element != "*" {
			specificity += 1
		}
	}

	// Set legacy fields for backward compatibility (simple selectors only)
	legacyType := ElementSelector
	legacyValue := ""
	if len(parts) == 1 && len(combinators) == 0 {
		part := parts[0]
		if part.ID != "" {
			legacyType = IDSelector
			legacyValue = part.ID
		} else if len(part.Classes) == 1 && part.Element == "" {
			legacyType = ClassSelector
			legacyValue = part.Classes[0]
		} else if part.Element != "" && part.ID == "" && len(part.Classes) == 0 {
			legacyType = ElementSelector
			legacyValue = part.Element
		}
	}

	return Selector{
		Raw:           selectorStr,
		Parts:         parts,
		Combinators:   combinators,
		Specificity:   specificity,
		PseudoElement: pseudoElement,
		Type:          legacyType,
		Value:         legacyValue,
	}
}

// tokenizeSelector splits a selector into tokens (handling combinators)
func tokenizeSelector(s string) []string {
	tokens := make([]string, 0)
	current := ""
	inBracket := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '[' {
			inBracket = true
			current += string(ch)
		} else if ch == ']' {
			inBracket = false
			current += string(ch)
		} else if !inBracket && (ch == '>' || ch == '+' || ch == '~' || ch == ' ') {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
			if ch != ' ' || (ch == ' ' && len(tokens) > 0) {
				// Only add space if it's a meaningful separator
				if ch == ' ' {
					// Check if last token was a combinator
					if len(tokens) > 0 {
						lastToken := tokens[len(tokens)-1]
						if lastToken != ">" && lastToken != "+" && lastToken != "~" && lastToken != " " {
							tokens = append(tokens, " ")
						}
					}
				} else {
					tokens = append(tokens, string(ch))
				}
			}
		} else {
			current += string(ch)
		}
	}

	if current != "" {
		tokens = append(tokens, current)
	}

	return tokens
}

// parseSelectorPart parses a single selector part like "div.class1.class2#id[attr=value]"
func parseSelectorPart(s string) SelectorPart {
	part := SelectorPart{
		Classes:    make([]string, 0),
		Attributes: make([]AttributeSelector, 0),
	}

	s = strings.TrimSpace(s)
	if s == "" {
		return part
	}

	// Parse element, classes, ID, and attributes
	i := 0

	// Check for element (must come first)
	if s[i] != '.' && s[i] != '#' && s[i] != '[' {
		// Read element name until we hit a special character
		j := i
		for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' {
			j++
		}
		part.Element = s[i:j]
		i = j
	}

	// Parse classes, ID, and attributes
	for i < len(s) {
		if s[i] == '.' {
			// Class
			i++
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' {
				j++
			}
			part.Classes = append(part.Classes, s[i:j])
			i = j
		} else if s[i] == '#' {
			// ID
			i++
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' {
				j++
			}
			part.ID = s[i:j]
			i = j
		} else if s[i] == '[' {
			// Attribute
			j := i + 1
			for j < len(s) && s[j] != ']' {
				j++
			}
			if j < len(s) {
				attrStr := s[i+1 : j]
				attr := parseAttributeSelector(attrStr)
				part.Attributes = append(part.Attributes, attr)
				i = j + 1
			} else {
				break
			}
		} else {
			i++
		}
	}

	return part
}

// parseAttributeSelector parses an attribute selector like "type=text" or "href^=https"
func parseAttributeSelector(s string) AttributeSelector {
	// Find the operator
	operators := []string{"^=", "$=", "*=", "~=", "|=", "="}

	for _, op := range operators {
		if idx := strings.Index(s, op); idx != -1 {
			name := strings.TrimSpace(s[:idx])
			value := strings.TrimSpace(s[idx+len(op):])
			// Remove quotes from value
			value = strings.Trim(value, `"'`)
			return AttributeSelector{
				Name:     name,
				Operator: op,
				Value:    value,
			}
		}
	}

	// No operator, just attribute name (existence check)
	return AttributeSelector{
		Name:     strings.TrimSpace(s),
		Operator: "",
		Value:    "",
	}
}

// Phase 22: EvaluateMediaQuery checks if a media query matches the given viewport dimensions
func EvaluateMediaQuery(mq *MediaQuery, viewportWidth, viewportHeight float64) bool {
	if mq == nil {
		// No media query = always matches
		return true
	}

	// Check media type (we only support "all" and "screen" for now)
	if mq.MediaType != "all" && mq.MediaType != "screen" {
		return false
	}

	// Check all conditions
	for _, cond := range mq.Conditions {
		if !evaluateMediaCondition(cond, viewportWidth, viewportHeight) {
			return false
		}
	}

	return true
}

// Phase 22: evaluateMediaCondition checks if a single media condition matches
func evaluateMediaCondition(cond MediaCondition, viewportWidth, viewportHeight float64) bool {
	// Parse the value to get numeric value and unit
	value, unit := parseMediaLength(cond.Value)

	// For simplicity, we only support px units
	if unit != "px" {
		return true // Unknown units = assume match
	}

	switch cond.Feature {
	case "min-width":
		return viewportWidth >= value
	case "max-width":
		return viewportWidth <= value
	case "min-height":
		return viewportHeight >= value
	case "max-height":
		return viewportHeight <= value
	default:
		return true // Unknown feature = assume match
	}
}

// Phase 22: parseMediaLength parses a length value and returns value and unit
func parseMediaLength(val string) (float64, string) {
	val = strings.TrimSpace(val)

	// Check for px suffix
	if strings.HasSuffix(val, "px") {
		numStr := strings.TrimSuffix(val, "px")
		if num, err := fmt.Sscanf(numStr, "%f", new(float64)); err == nil && num == 1 {
			var value float64
			fmt.Sscanf(numStr, "%f", &value)
			return value, "px"
		}
	}

	// Try to parse as plain number (assume px)
	var value float64
	if _, err := fmt.Sscanf(val, "%f", &value); err == nil {
		return value, "px"
	}

	return 0, ""
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
