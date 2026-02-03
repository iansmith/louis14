package css

import (
	"fmt"
	"strconv"
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
	Element       string              // Element name ("div", "p", "*" for universal, "" for none)
	Classes       []string            // Class names (without the .)
	ID            string              // ID (without the #)
	Attributes    []AttributeSelector // Attribute selectors
	PseudoClasses []string            // Pseudo-classes (e.g., "hover", "focus", "active", "visited")
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

// stripCSSComments removes all /* ... */ comments from CSS source,
// while preserving string literals (comments inside strings are not stripped).
func stripCSSComments(css string) string {
	var b strings.Builder
	b.Grow(len(css))
	i := 0
	inString := byte(0)
	for i < len(css) {
		// Handle string literals
		if inString != 0 {
			b.WriteByte(css[i])
			if css[i] == '\\' && i+1 < len(css) {
				i++
				b.WriteByte(css[i])
			} else if css[i] == inString {
				inString = 0
			}
			i++
			continue
		}
		if css[i] == '"' || css[i] == '\'' {
			inString = css[i]
			b.WriteByte(css[i])
			i++
			continue
		}
		if i+1 < len(css) && css[i] == '/' && css[i+1] == '*' {
			// Skip until */
			i += 2
			for i < len(css) {
				if i+1 < len(css) && css[i] == '*' && css[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			// If we reached end of input, the comment was unterminated — just stop
		} else {
			b.WriteByte(css[i])
			i++
		}
	}
	return b.String()
}

// ParseStylesheet parses CSS stylesheet content into rules
func ParseStylesheet(css string) (*Stylesheet, error) {
	stylesheet := &Stylesheet{
		Rules: make([]Rule, 0),
	}

	// Strip comments before parsing
	css = stripCSSComments(css)

	// Simple parser: split by } to get rules
	css = strings.TrimSpace(css)
	if css == "" {
		return stylesheet, nil
	}

	// Find each rule (selector { declarations })
	rules := splitRules(css)

	for _, ruleStr := range rules {
		trimmed := strings.TrimSpace(ruleStr)
		if strings.HasPrefix(trimmed, "@") {
			// Phase 22: Handle @media; skip all other at-rules
			if strings.HasPrefix(trimmed, "@media") {
				mediaRules := parseMediaRule(ruleStr)
				stylesheet.Rules = append(stylesheet.Rules, mediaRules...)
			}
			// Unknown at-rules (@three-dee, @import, etc.) are silently skipped
			continue
		}

		rule, err := parseRule(ruleStr)
		if err != nil {
			// Skip malformed rules
			continue
		}
		stylesheet.Rules = append(stylesheet.Rules, rule)
	}

	return stylesheet, nil
}

// splitRules splits CSS into individual rules, with robust error recovery
// for unclosed blocks, strings, and mismatched braces.
func splitRules(css string) []string {
	rules := make([]string, 0)
	depth := 0
	start := 0
	inString := byte(0) // 0 = not in string, '"' or '\'' = in that string

	for i := 0; i < len(css); i++ {
		ch := css[i]

		// Handle string literals — skip their contents
		if inString != 0 {
			if ch == '\\' && i+1 < len(css) {
				i++ // skip escaped character
			} else if ch == inString {
				inString = 0
			}
			continue
		}

		// Handle backslash escapes outside strings (e.g., \} in property values)
		if ch == '\\' && i+1 < len(css) {
			i++ // skip escaped character
			continue
		}

		if ch == '"' || ch == '\'' {
			inString = ch
			continue
		}

		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth <= 0 {
				// Found complete rule (or recovered from negative depth)
				depth = 0
				ruleStr := css[start : i+1]
				if strings.TrimSpace(ruleStr) != "" {
					rules = append(rules, ruleStr)
				}
				start = i + 1
			}
		} else if depth == 0 && ch == ';' {
			// Skip stray semicolons between rules (e.g., "};")
			start = i + 1
		}
	}

	// Any trailing content without a closing brace is discarded (error recovery)
	return rules
}

// isValidSelector checks if a selector string looks valid enough to parse.
// Returns false for clearly malformed selectors that should cause the rule to be skipped.
func isValidSelector(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Selector must not start with } or ; or {
	if s[0] == '}' || s[0] == ';' || s[0] == '{' {
		return false
	}
	// Check for unbalanced brackets
	bracketDepth := 0
	for _, ch := range s {
		switch ch {
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
			if bracketDepth < 0 {
				return false
			}
		case '{', '}':
			// Braces inside selector text are invalid
			return false
		}
	}
	if bracketDepth != 0 {
		return false
	}
	return true
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

	// Validate selector — skip entire rule if invalid
	if !isValidSelector(selectorStr) {
		return Rule{}, fmt.Errorf("invalid selector: %q", selectorStr)
	}

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

	// Phase 11: Check for pseudo-element (::before/::after or CSS 2.1 :before/:after)
	// If there's a space before the pseudo-element (e.g., ".foo :after"), it applies to
	// descendants only, not the element matched by the selector itself.
	pseudoElement := ""
	pseudoElementForDescendants := false
	if strings.Contains(selectorStr, "::before") {
		pseudoElement = "before"
		// Check if space before pseudo-element
		idx := strings.Index(selectorStr, "::before")
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = strings.Replace(selectorStr, "::before", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	} else if strings.Contains(selectorStr, "::after") {
		pseudoElement = "after"
		idx := strings.Index(selectorStr, "::after")
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = strings.Replace(selectorStr, "::after", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	} else if strings.Contains(selectorStr, ":before") {
		pseudoElement = "before"
		idx := strings.Index(selectorStr, ":before")
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = strings.Replace(selectorStr, ":before", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	} else if strings.Contains(selectorStr, ":after") {
		pseudoElement = "after"
		idx := strings.Index(selectorStr, ":after")
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = strings.Replace(selectorStr, ":after", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	}
	// If pseudo-element is for descendants only, clear it from direct matching
	// but record it somehow (we'll use a convention: if PseudoElement starts with "descendant:",
	// it means the element must be a descendant of the selector match)
	if pseudoElementForDescendants && pseudoElement != "" {
		pseudoElement = "descendant:" + pseudoElement
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
		case ">", "+", "~":
			if currentPart != "" {
				parts = append(parts, parseSelectorPart(currentPart))
				currentPart = ""
			}
			// If last combinator was a space (descendant), replace it with the explicit combinator
			// This handles "A > B" being tokenized as ["A", " ", ">", " ", "B"]
			var comb CombinatorType
			switch token {
			case ">":
				comb = ChildCombinator
			case "+":
				comb = AdjacentSiblingCombinator
			case "~":
				comb = GeneralSiblingCombinator
			}
			if len(combinators) > 0 && len(combinators) == len(parts) {
				// Replace trailing space combinator
				combinators[len(combinators)-1] = comb
			} else {
				combinators = append(combinators, comb)
			}
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
		specificity += len(part.PseudoClasses) * 10
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
		Classes:       make([]string, 0),
		Attributes:    make([]AttributeSelector, 0),
		PseudoClasses: make([]string, 0),
	}

	s = strings.TrimSpace(s)
	if s == "" {
		return part
	}

	// Parse element, classes, ID, and attributes
	i := 0

	// Check for element (must come first)
	if s[i] != '.' && s[i] != '#' && s[i] != '[' && s[i] != ':' {
		// Read element name until we hit a special character
		j := i
		for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' && s[j] != ':' {
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
			for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' && s[j] != ':' {
				j++
			}
			part.Classes = append(part.Classes, s[i:j])
			i = j
		} else if s[i] == '#' {
			// ID
			i++
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' && s[j] != ':' {
				j++
			}
			part.ID = s[i:j]
			i = j
		} else if s[i] == ':' {
			// Pseudo-class (skip :: pseudo-elements, handled separately)
			if i+1 < len(s) && s[i+1] == ':' {
				// This is a pseudo-element, stop parsing
				break
			}
			i++ // skip the ':'
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' && s[j] != ':' {
				j++
			}
			if j > i {
				part.PseudoClasses = append(part.PseudoClasses, s[i:j])
			}
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
			// Handle CSS escape sequences (e.g., second\ two → second two)
			value = strings.ReplaceAll(value, `\ `, " ")
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

// splitDeclarationParts splits a declaration block by semicolons,
// respecting string literals so semicolons inside strings are not split on.
func splitDeclarationParts(declStr string) []string {
	var parts []string
	start := 0
	inString := byte(0)
	parenDepth := 0

	for i := 0; i < len(declStr); i++ {
		ch := declStr[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(declStr) {
				i++ // skip escaped char
			} else if ch == inString {
				inString = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = ch
			continue
		}
		if ch == '(' {
			parenDepth++
			continue
		}
		if ch == ')' && parenDepth > 0 {
			parenDepth--
			continue
		}
		if ch == ';' && parenDepth == 0 {
			parts = append(parts, declStr[start:i])
			start = i + 1
		}
	}
	// Last segment (after final semicolon or if no semicolon)
	if start < len(declStr) {
		parts = append(parts, declStr[start:])
	}
	return parts
}

// parseDeclarations parses CSS declarations into a map.
// Invalid declarations are silently skipped (error recovery).
func parseDeclarations(declStr string) map[string]string {
	declarations := make(map[string]string)

	parts := splitDeclarationParts(declStr)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split property: value at first colon
		colonPos := strings.Index(part, ":")
		if colonPos == -1 {
			// No colon — invalid declaration, skip
			continue
		}

		property := strings.TrimSpace(part[:colonPos])
		value := strings.TrimSpace(part[colonPos+1:])

		// Skip declarations with empty property or value
		if property == "" || value == "" {
			continue
		}

		// Skip properties that start with invalid characters
		// (valid CSS properties start with a letter or hyphen)
		if property[0] != '-' && (property[0] < 'a' || property[0] > 'z') && (property[0] < 'A' || property[0] > 'Z') {
			continue
		}

		// Handle !important: strip it if valid, reject if malformed
		if strings.Contains(value, "!") {
			bangIdx := strings.Index(value, "!")
			afterBang := strings.TrimSpace(value[bangIdx+1:])
			if strings.EqualFold(afterBang, "important") {
				value = strings.TrimSpace(value[:bangIdx])
			} else {
				// Invalid use of ! (e.g., "red ! error") — reject entire declaration
				continue
			}
		}

		// CSS 2.1: Reject bare non-zero numbers for length properties (must have units)
		if isLengthProperty(property) && isInvalidBareNumber(value) {
			continue
		}

		// Expand shorthand properties (reuse from Phase 2)
		style := NewStyle()
		expandShorthand(style, property, value)

		// Copy all expanded properties to declarations
		for k, v := range style.Properties {
			declarations[k] = v
		}
	}

	return declarations
}

// isLengthProperty returns true for CSS properties that expect length values
func isLengthProperty(prop string) bool {
	switch prop {
	case "width", "height", "min-width", "min-height", "max-width", "max-height",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border-width", "border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
		"top", "right", "bottom", "left",
		"font-size", "line-height", "letter-spacing", "word-spacing",
		"text-indent", "vertical-align":
		return true
	}
	return false
}

// isInvalidBareNumber returns true if value is a non-zero number with no unit
func isInvalidBareNumber(value string) bool {
	num, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false // not a bare number
	}
	return num != 0 // zero without units is valid
}

