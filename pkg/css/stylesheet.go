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
	Selector       Selector
	Declarations   map[string]string // property -> value
	Important      map[string]bool   // tracks which properties are !important
	MediaQuery     *MediaQuery       // Phase 22: Optional media query wrapper
	ContainerQuery *ContainerQuery   // Optional @container query wrapper
	LayerName      string            // @layer name (empty = unlayered)
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

// ContainerQuery represents a @container rule condition
type ContainerQuery struct {
	Name       string               // optional container name (empty = any container)
	Conditions []ContainerCondition // size conditions
}

// ContainerCondition represents a single container query condition
type ContainerCondition struct {
	Feature string // "min-width", "max-width", "width"
	Value   string // "200px", etc.
}

// FontFaceRule represents a parsed @font-face rule.
type FontFaceRule struct {
	Family string // font-family (unquoted)
	Src    string // URL from src: url(...)
	Format string // "truetype", "opentype", "woff", "woff2", or ""
	Weight string // font-weight value (e.g. "bold", "400", "700")
	Style  string // font-style value (e.g. "italic", "normal")
}

// KeyframeRule represents a single keyframe stop in a @keyframes rule.
type KeyframeRule struct {
	Stop         string            // "from", "to", "0%", "50%", "100%", etc.
	Declarations map[string]string // CSS declarations at this stop
}

// Stylesheet represents a parsed CSS stylesheet
type Stylesheet struct {
	Rules      []Rule
	FontFaces  []FontFaceRule
	LayerOrder []string                 // @layer declaration order (first declared = lowest priority)
	Keyframes  map[string][]KeyframeRule // animation name → keyframe stops
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
			} else if strings.HasPrefix(trimmed, "@font-face") {
				if ff := parseFontFaceRule(trimmed); ff != nil {
					stylesheet.FontFaces = append(stylesheet.FontFaces, *ff)
				}
			} else if strings.HasPrefix(trimmed, "@supports") {
				supportsRules := parseSupportsRule(ruleStr)
				stylesheet.Rules = append(stylesheet.Rules, supportsRules...)
			} else if strings.HasPrefix(trimmed, "@container") {
				containerRules := parseContainerRule(ruleStr)
				stylesheet.Rules = append(stylesheet.Rules, containerRules...)
			} else if strings.HasPrefix(trimmed, "@layer") {
				layerRules, layerNames := parseLayerRule(ruleStr, stylesheet.LayerOrder)
				for _, name := range layerNames {
					found := false
					for _, existing := range stylesheet.LayerOrder {
						if existing == name {
							found = true
							break
						}
					}
					if !found {
						stylesheet.LayerOrder = append(stylesheet.LayerOrder, name)
					}
				}
				stylesheet.Rules = append(stylesheet.Rules, layerRules...)
			} else if strings.HasPrefix(trimmed, "@keyframes") || strings.HasPrefix(trimmed, "@-webkit-keyframes") {
				// Parse and store keyframes; static renderer uses initial state only
				name, frames := parseKeyframesRule(ruleStr)
				if name != "" {
					if stylesheet.Keyframes == nil {
						stylesheet.Keyframes = make(map[string][]KeyframeRule)
					}
					stylesheet.Keyframes[name] = frames
				}
			}
			// Unknown at-rules (@three-dee, @import, etc.) are silently skipped
			continue
		}

		rules, err := parseRules(ruleStr)
		if err != nil {
			// Skip malformed rules
			continue
		}
		stylesheet.Rules = append(stylesheet.Rules, rules...)
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
			// CSS 2.1 error recovery: a stray ';' after '}' at the top level
			// becomes part of the next rule's text, making its selector invalid.
			// This is tested by Acid2 line 102: ".parser { m\argin: 2em; };"
			// where the ';' should cause the next rule to be skipped.
			// However, ';' that terminates an at-rule (e.g., @import url(...);)
			// should be skipped normally.
			isAfterCloseBrace := false
			for j := i - 1; j >= 0; j-- {
				c := css[j]
				if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
					continue
				}
				if c == '}' {
					isAfterCloseBrace = true
				}
				break
			}
			if !isAfterCloseBrace {
				// At-rule terminator — emit rule text if it's an @layer statement
				ruleText := strings.TrimSpace(css[start : i+1])
				if strings.HasPrefix(ruleText, "@layer") {
					rules = append(rules, ruleText)
				}
				start = i + 1
			}
			// If after '}', leave the ';' in the next rule's text
			// so isValidSelector will reject the selector starting with ';'
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

// parseRules parses a CSS rule string, expanding comma-separated selector
// groups into multiple rules with the same declarations.
// e.g., "h1, h2, h3 { color: red }" → 3 separate rules.
func parseRules(ruleStr string) ([]Rule, error) {
	// Find the opening brace
	bracePos := strings.Index(ruleStr, "{")
	if bracePos == -1 {
		return nil, fmt.Errorf("no opening brace found")
	}

	selectorStr := strings.TrimSpace(ruleStr[:bracePos])

	// Split selector by commas (but not commas inside brackets or parens)
	selectors := splitSelectorGroup(selectorStr)
	if len(selectors) <= 1 {
		// No commas or only one selector — use the original parseRule
		rule, err := parseRule(ruleStr)
		if err != nil {
			return nil, err
		}
		return []Rule{rule}, nil
	}

	// Extract declarations (shared by all selectors)
	declStart := bracePos + 1
	declEnd := strings.LastIndex(ruleStr, "}")
	if declEnd == -1 {
		declEnd = len(ruleStr)
	}
	declStr := ruleStr[declStart:declEnd]
	declResult := parseDeclarations(declStr)

	rules := make([]Rule, 0, len(selectors))
	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" || !isValidSelector(sel) {
			continue
		}
		selector := parseSelector(sel)
		rules = append(rules, Rule{
			Selector:     selector,
			Declarations: declResult.Declarations,
			Important:    declResult.Important,
		})
	}

	if len(rules) == 0 {
		return nil, fmt.Errorf("no valid selectors in group")
	}
	return rules, nil
}

// splitSelectorGroup splits a selector group by commas, respecting brackets
// and parentheses (e.g., attribute selectors like [attr~="a,b"]).
func splitSelectorGroup(s string) []string {
	var parts []string
	depth := 0
	inString := byte(0)
	start := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
			} else if ch == inString {
				inString = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = ch
			continue
		}
		if ch == '(' || ch == '[' {
			depth++
		} else if ch == ')' || ch == ']' {
			depth--
		} else if ch == ',' && depth == 0 {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
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
	declResult := parseDeclarations(declStr)

	return Rule{
		Selector:     selector,
		Declarations: declResult.Declarations,
		Important:    declResult.Important,
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

// parseContainerRule parses a @container rule and returns its inner rules
func parseContainerRule(ruleStr string) []Rule {
	rules := make([]Rule, 0)

	// Find the opening brace
	bracePos := strings.Index(ruleStr, "{")
	if bracePos == -1 {
		return rules
	}

	// Extract container query string: @container [name] (conditions)
	queryStr := strings.TrimSpace(ruleStr[:bracePos])
	containerQuery := parseContainerQuery(queryStr)

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
		rule.ContainerQuery = containerQuery
		rules = append(rules, rule)
	}

	return rules
}

// parseLayerRule parses an @layer rule. Two forms:
//   - Statement form: "@layer reset, base;" — declares layer order, no rules
//   - Block form: "@layer base { selector { ... } }" — rules belonging to a layer
//
// Returns (rules, layerNames) where layerNames are declared/used layer names.
// existingLayerOrder is used to assign LayerName to rules when a block form is used.
func parseLayerRule(ruleStr string, existingLayerOrder []string) ([]Rule, []string) {
	trimmed := strings.TrimSpace(ruleStr)
	rules := make([]Rule, 0)
	layerNames := make([]string, 0)

	// Remove "@layer" prefix
	rest := strings.TrimPrefix(trimmed, "@layer")
	rest = strings.TrimSpace(rest)

	// Statement form: ends with ";" (no block)
	// e.g. "@layer reset, base;" or "@layer utilities;"
	if strings.HasSuffix(rest, ";") {
		// Remove trailing semicolon
		nameList := strings.TrimSuffix(rest, ";")
		nameList = strings.TrimSpace(nameList)
		// Parse comma-separated names
		for _, name := range strings.Split(nameList, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				layerNames = append(layerNames, name)
			}
		}
		// Statement form has no CSS rules; just returns layer names for ordering
		return rules, layerNames
	}

	// Block form: "@layer name { ... }" or "@layer { ... }" (anonymous)
	bracePos := strings.Index(rest, "{")
	if bracePos == -1 {
		return rules, layerNames
	}

	// Extract layer name (may be empty for anonymous layer)
	layerName := strings.TrimSpace(rest[:bracePos])

	// Extract inner CSS (between outermost { and last })
	innerStart := strings.Index(ruleStr, "{") + 1
	innerEnd := strings.LastIndex(ruleStr, "}")
	if innerEnd == -1 || innerEnd <= innerStart {
		return rules, layerNames
	}

	innerCSS := ruleStr[innerStart:innerEnd]

	// Parse inner rules
	innerRules := splitRules(innerCSS)
	for _, innerRuleStr := range innerRules {
		innerTrimmed := strings.TrimSpace(innerRuleStr)
		// Handle nested @media inside @layer
		if strings.HasPrefix(innerTrimmed, "@media") {
			mediaRules := parseMediaRule(innerRuleStr)
			for i := range mediaRules {
				mediaRules[i].LayerName = layerName
			}
			rules = append(rules, mediaRules...)
		} else if strings.HasPrefix(innerTrimmed, "@supports") {
			supportsRules := parseSupportsRule(innerRuleStr)
			for i := range supportsRules {
				supportsRules[i].LayerName = layerName
			}
			rules = append(rules, supportsRules...)
		} else {
			parsed, err := parseRules(innerRuleStr)
			if err != nil {
				continue
			}
			for i := range parsed {
				parsed[i].LayerName = layerName
			}
			rules = append(rules, parsed...)
		}
	}

	// Record this layer name
	if layerName != "" {
		layerNames = append(layerNames, layerName)
	}

	return rules, layerNames
}

// parseContainerQuery parses a container query string like "@container sidebar (min-width: 200px)"
func parseContainerQuery(queryStr string) *ContainerQuery {
	// Remove @container prefix
	queryStr = strings.TrimPrefix(queryStr, "@container")
	queryStr = strings.TrimSpace(queryStr)

	cq := &ContainerQuery{
		Conditions: make([]ContainerCondition, 0),
	}

	// Check if there's a container name before the first (
	parenPos := strings.Index(queryStr, "(")
	if parenPos > 0 {
		namePart := strings.TrimSpace(queryStr[:parenPos])
		if namePart != "" && namePart != "not" {
			cq.Name = namePart
		}
	}

	// Parse conditions: (min-width: 200px) and (max-width: 500px)
	conditionStrs := strings.Split(queryStr, "and")
	for _, condStr := range conditionStrs {
		condStr = strings.TrimSpace(condStr)
		// Skip the name part (non-parenthesized)
		if !strings.Contains(condStr, "(") {
			continue
		}
		// Remove parentheses
		condStr = strings.Trim(condStr, "()")
		condStr = strings.TrimSpace(condStr)

		// Split by : to get feature and value
		colonPos := strings.Index(condStr, ":")
		if colonPos == -1 {
			continue
		}
		feature := strings.TrimSpace(condStr[:colonPos])
		value := strings.TrimSpace(condStr[colonPos+1:])
		cq.Conditions = append(cq.Conditions, ContainerCondition{
			Feature: feature,
			Value:   value,
		})
	}

	return cq
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

	// Handle "not" and "only" modifiers
	// "not screen" means negate the entire type match
	// "only screen" is equivalent to "screen" (older syntax to hide from legacy parsers)
	negate := false
	if strings.HasPrefix(mediaStr, "not ") {
		negate = true
		mediaStr = strings.TrimSpace(mediaStr[4:])
	} else if strings.HasPrefix(mediaStr, "only ") {
		mediaStr = strings.TrimSpace(mediaStr[5:])
	}

	// Check for media type (screen, print, all, etc.)
	if strings.HasPrefix(mediaStr, "screen") && (len(mediaStr) == 6 || mediaStr[6] == ' ' || mediaStr[6] == ',') {
		mq.MediaType = "screen"
		mediaStr = strings.TrimSpace(mediaStr[6:])
	} else if strings.HasPrefix(mediaStr, "print") && (len(mediaStr) == 5 || mediaStr[5] == ' ' || mediaStr[5] == ',') {
		mq.MediaType = "print"
		mediaStr = strings.TrimSpace(mediaStr[5:])
	} else if strings.HasPrefix(mediaStr, "all") && (len(mediaStr) == 3 || mediaStr[3] == ' ' || mediaStr[3] == ',') {
		mq.MediaType = "all"
		mediaStr = strings.TrimSpace(mediaStr[3:])
	}

	// Apply negation: negate the media type by setting it to an impossible value
	if negate {
		switch mq.MediaType {
		case "screen":
			mq.MediaType = "print" // not screen = print (won't match screen renderer)
		case "print":
			mq.MediaType = "screen" // not print = screen (matches)
		case "all":
			mq.MediaType = "none" // not all = never match
		}
	}

	// Remove leading "and" keyword
	mediaStr = strings.TrimPrefix(mediaStr, "and")
	mediaStr = strings.TrimSpace(mediaStr)

	// Parse conditions: (min-width: 768px) and (max-width: 1024px)
	// Split by " and " at paren depth 0 to handle nested parens in values
	conditionStrs := splitMediaConditions(mediaStr)

	for _, condStr := range conditionStrs {
		condStr = strings.TrimSpace(condStr)
		if condStr == "" {
			continue
		}

		// Remove outer parentheses
		if strings.HasPrefix(condStr, "(") && strings.HasSuffix(condStr, ")") {
			condStr = condStr[1 : len(condStr)-1]
		}
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
		} else if len(parts) == 1 && condStr != "" {
			// Boolean media feature like (color) or (hover) — no value
			feature := strings.TrimSpace(parts[0])
			if feature != "" {
				mq.Conditions = append(mq.Conditions, MediaCondition{
					Feature: feature,
					Value:   "", // empty value = boolean true test
				})
			}
		}
	}

	return mq
}

// splitMediaConditions splits a media query condition string by " and " at paren depth 0.
// This handles cases like "(min-width: 100px) and (max-width: 500px)".
func splitMediaConditions(s string) []string {
	var parts []string
	depth := 0
	start := 0
	const sep = " and "

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && i+len(sep) <= len(s) && strings.EqualFold(s[i:i+len(sep)], sep) {
			parts = append(parts, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseSupportsRule parses an @supports rule and returns the inner rules if condition is met
func parseSupportsRule(ruleStr string) []Rule {
	ruleStr = strings.TrimSpace(ruleStr)
	braceIdx := strings.Index(ruleStr, "{")
	if braceIdx < 0 {
		return nil
	}

	conditionStr := strings.TrimSpace(ruleStr[len("@supports"):braceIdx])

	// Find matching closing brace
	innerEnd := strings.LastIndex(ruleStr, "}")
	if innerEnd <= braceIdx {
		return nil
	}
	innerCSS := ruleStr[braceIdx+1 : innerEnd]

	if !evaluateSupportsCondition(conditionStr) {
		return nil
	}

	innerRules := splitRules(innerCSS)
	var rules []Rule
	for _, inner := range innerRules {
		parsed, err := parseRule(inner)
		if err != nil {
			continue
		}
		rules = append(rules, parsed)
	}
	return rules
}

func evaluateSupportsCondition(condition string) bool {
	condition = strings.TrimSpace(condition)

	// Handle "not" prefix
	if strings.HasPrefix(condition, "not ") {
		inner := strings.TrimSpace(condition[4:])
		return !evaluateSupportsCondition(inner)
	}
	if strings.HasPrefix(condition, "not(") {
		inner := strings.TrimSpace(condition[3:])
		return !evaluateSupportsCondition(inner)
	}

	// Handle parenthesized condition
	if strings.HasPrefix(condition, "(") && strings.HasSuffix(condition, ")") {
		inner := condition[1 : len(condition)-1]

		// Check for "and" / "or" compositions
		// Look for ") and (" or ") or (" patterns
		if andParts := splitSupportsOperator(condition, " and "); len(andParts) > 1 {
			for _, part := range andParts {
				if !evaluateSupportsCondition(strings.TrimSpace(part)) {
					return false
				}
			}
			return true
		}
		if orParts := splitSupportsOperator(condition, " or "); len(orParts) > 1 {
			for _, part := range orParts {
				if evaluateSupportsCondition(strings.TrimSpace(part)) {
					return true
				}
			}
			return false
		}

		// Simple property: value check
		colonIdx := strings.Index(inner, ":")
		if colonIdx > 0 {
			property := strings.TrimSpace(inner[:colonIdx])
			value := strings.TrimSpace(inner[colonIdx+1:])
			return isSupportedPropertyValue(property, value)
		}

		// Could be a nested condition
		return evaluateSupportsCondition(inner)
	}

	return false
}

func splitSupportsOperator(s string, op string) []string {
	// Split on operator while respecting parentheses depth
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && i+len(op) <= len(s) && s[i:i+len(op)] == op {
			parts = append(parts, s[start:i])
			start = i + len(op)
			i += len(op) - 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func isSupportedPropertyValue(property, value string) bool {
	// For certain properties, validate the value too
	switch property {
	case "display":
		validDisplay := map[string]bool{
			"block": true, "inline": true, "inline-block": true,
			"flex": true, "inline-flex": true,
			"grid": true, "inline-grid": true,
			"none": true, "contents": true,
			"table": true, "table-row": true, "table-cell": true,
			"table-caption": true, "table-row-group": true,
			"table-header-group": true, "table-footer-group": true,
			"table-column": true, "table-column-group": true,
			"list-item": true,
		}
		return validDisplay[value]
	case "position":
		validPosition := map[string]bool{
			"static": true, "relative": true, "absolute": true,
			"fixed": true, "sticky": true,
		}
		return validPosition[value]
	default:
		// For other properties, just check if the property is known
		return isSupportedCSSProperty(property)
	}
}

func isSupportedCSSProperty(property string) bool {
	supported := map[string]bool{
		"display": true, "position": true, "float": true, "clear": true,
		"width": true, "height": true, "min-width": true, "max-width": true,
		"min-height": true, "max-height": true,
		"margin": true, "margin-top": true, "margin-right": true,
		"margin-bottom": true, "margin-left": true,
		"padding": true, "padding-top": true, "padding-right": true,
		"padding-bottom": true, "padding-left": true,
		"border": true, "border-radius": true,
		"background": true, "background-color": true, "background-image": true,
		"color": true, "font-size": true, "font-family": true, "font-weight": true,
		"flex": true, "flex-direction": true, "flex-wrap": true,
		"grid": true, "grid-template-columns": true, "grid-template-rows": true,
		"transform": true, "opacity": true, "overflow": true,
		"z-index": true, "box-shadow": true, "box-sizing": true,
		"text-align": true, "text-decoration": true, "text-transform": true,
		"vertical-align": true, "line-height": true, "white-space": true,
		"visibility": true, "clip-path": true, "filter": true,
		"aspect-ratio": true, "column-count": true,
		"justify-content": true, "align-items": true,
	}
	return supported[property]
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
	} else if strings.Contains(selectorStr, "::first-letter") {
		pseudoElement = "first-letter"
		idx := strings.Index(selectorStr, "::first-letter")
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = strings.Replace(selectorStr, "::first-letter", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	} else if strings.Contains(selectorStr, ":first-letter") {
		pseudoElement = "first-letter"
		idx := strings.Index(selectorStr, ":first-letter")
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = strings.Replace(selectorStr, ":first-letter", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	} else if strings.Contains(selectorStr, "::first-line") {
		pseudoElement = "first-line"
		idx := strings.Index(selectorStr, "::first-line")
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = strings.Replace(selectorStr, "::first-line", "", 1)
		selectorStr = strings.TrimSpace(selectorStr)
	} else if strings.Contains(selectorStr, ":first-line") {
		pseudoElement = "first-line"
		idx := strings.Index(selectorStr, ":first-line")
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = strings.Replace(selectorStr, ":first-line", "", 1)
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
		for _, pc := range part.PseudoClasses {
			if strings.HasPrefix(pc, "is(") {
				// :is() takes specificity of its most specific argument
				arg := pc[len("is(") : len(pc)-1]
				sels := splitSelectorGroup(strings.TrimSpace(arg))
				maxSpec := 0
				for _, sel := range sels {
					innerSel := parseSelector(strings.TrimSpace(sel))
					if innerSel.Specificity > maxSpec {
						maxSpec = innerSel.Specificity
					}
				}
				specificity += maxSpec
			} else if strings.HasPrefix(pc, "where(") {
				// :where() has zero specificity
			} else {
				specificity += 10
			}
		}
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
	parenDepth := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '(' {
			parenDepth++
			current += string(ch)
		} else if ch == ')' {
			parenDepth--
			current += string(ch)
		} else if ch == '[' {
			inBracket = true
			current += string(ch)
		} else if ch == ']' {
			inBracket = false
			current += string(ch)
		} else if !inBracket && parenDepth == 0 && (ch == '>' || ch == '+' || ch == '~' || ch == ' ') {
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
			depth := 0
			for j < len(s) {
				if s[j] == '(' {
					depth++
				} else if s[j] == ')' {
					depth--
					if depth == 0 {
						j++ // include the closing paren
						break
					}
				} else if depth == 0 && (s[j] == '.' || s[j] == '#' || s[j] == '[' || s[j] == ':') {
					break
				}
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

	// Check media type
	switch mq.MediaType {
	case "all", "screen", "":
		// matches — we render for screen
	case "print":
		return false // Print media: never match in screen renderer
	default:
		return false // Unknown media types: don't match
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
	feature := strings.TrimSpace(strings.ToLower(cond.Feature))
	value := strings.TrimSpace(strings.ToLower(cond.Value))

	// Handle non-numeric media features
	switch feature {
	case "prefers-color-scheme":
		// We render static PNGs in light mode
		return value == "light"
	case "prefers-reduced-motion":
		// Static renderer — no motion, default is no-preference
		return value == "no-preference"
	case "prefers-reduced-transparency":
		return value == "no-preference" || value == ""
	case "prefers-contrast":
		return value == "no-preference" || value == ""

	// Forced colors — no forced colors in desktop static renderer
	case "forced-colors":
		return value == "none"
	case "inverted-colors":
		return value == "none"

	// Interaction media features — desktop defaults (fine pointer/mouse, hover capable)
	case "pointer":
		// Desktop has a fine pointer (mouse); coarse = touch = false; none = false
		return value == "fine" || value == ""
	case "any-pointer":
		// Desktop has fine pointer; also accept "none" as secondary device
		return value == "fine" || value == ""
	case "hover":
		// Desktop supports hover
		return value == "hover" || value == ""
	case "any-hover":
		// Desktop supports hover
		return value == "hover" || value == ""

	// Orientation — based on viewport aspect ratio (800×600 = landscape)
	case "orientation":
		if viewportWidth >= viewportHeight {
			return value == "landscape"
		}
		return value == "portrait"

	// Display mode — we render as a standard browser
	case "display-mode":
		return value == "browser" || value == ""

	// Scripting — we don't execute JS but claim "none" so sites don't show JS-only fallback
	case "scripting":
		return value == "none" || value == ""

	// Color media features
	case "color":
		// boolean feature: true if it has color (our renderer supports color)
		// value is empty for boolean test, or a number for min-color etc.
		return true
	case "color-index":
		// We don't use an indexed color palette
		return value == "0" || value == ""
	case "monochrome":
		// We are not monochrome
		return value == "0" || value == ""

	// Resolution — always match for static renderer
	case "resolution", "min-resolution", "max-resolution":
		return true

	// Legacy device dimension features — approximate as viewport
	case "device-width", "device-height",
		"min-device-width", "min-device-height",
		"max-device-width", "max-device-height":
		return true

	// color-gamut — report srgb support
	case "color-gamut":
		return value == "srgb" || value == ""

	// dynamic-range — report standard
	case "dynamic-range":
		return value == "standard" || value == ""

	// video-dynamic-range
	case "video-dynamic-range":
		return value == "standard" || value == ""

	// overflow-block, overflow-inline
	case "overflow-block":
		return value == "scroll" || value == "optional-paged" || value == ""
	case "overflow-inline":
		return value == "scroll" || value == ""

	// update — we render to a static image (none update)
	case "update":
		return value == "none" || value == ""

	// grid — not a grid device (bitmap display)
	case "grid":
		return value == "0" || value == ""
	}

	// Parse the value to get numeric value and unit for dimension features
	numVal, unit := parseMediaLength(cond.Value)

	// For simplicity, we only support px units
	if unit != "px" {
		return true // Unknown units = assume match
	}

	switch feature {
	case "min-width":
		return viewportWidth >= numVal
	case "max-width":
		return viewportWidth <= numVal
	case "min-height":
		return viewportHeight >= numVal
	case "max-height":
		return viewportHeight <= numVal
	case "width":
		return viewportWidth == numVal
	case "height":
		return viewportHeight == numVal
	default:
		return false // Unknown feature = don't match
	}
}

// Phase 22: parseMediaLength parses a length value and returns value and unit
func parseMediaLength(val string) (float64, string) {
	val = strings.TrimSpace(val)

	// Handle calc() expressions
	if strings.HasPrefix(val, "calc(") && strings.HasSuffix(val, ")") {
		inner := val[5 : len(val)-1]
		// 16px for font-size (media queries use initial font size, not element font-size)
		if result, ok := EvalCalcWithPercent(inner, 16.0, 0); ok {
			return result, "px"
		}
	}

	// Handle rem units (1rem = 16px for media queries per CSS spec)
	if strings.HasSuffix(val, "rem") {
		numStr := strings.TrimSuffix(val, "rem")
		var num float64
		if _, err := fmt.Sscanf(numStr, "%f", &num); err == nil {
			return num * 16.0, "px"
		}
	}

	// Handle em units (1em = 16px for media queries — initial font size)
	if strings.HasSuffix(val, "em") {
		numStr := strings.TrimSuffix(val, "em")
		var num float64
		if _, err := fmt.Sscanf(numStr, "%f", &num); err == nil {
			return num * 16.0, "px"
		}
	}

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

// unescapeCSS decodes CSS escape sequences per CSS 2.1 §4.1.3
// - \X where X is not a hex digit → literal character X (e.g., \r → r)
// - \HHHHHH (1-6 hex digits) → Unicode code point (e.g., \45 → E since 0x45 = 69 = 'E')
// - Trailing whitespace after hex escape is consumed
func unescapeCSS(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '\\' || i+1 >= len(s) {
			result.WriteByte(s[i])
			i++
			continue
		}
		// Found backslash - check what follows
		i++ // skip backslash
		// Check for hex escape (1-6 hex digits)
		hexStart := i
		for i < len(s) && i-hexStart < 6 && isHexDigit(s[i]) {
			i++
		}
		if i > hexStart {
			// Parse hex value
			hexStr := s[hexStart:i]
			codePoint, _ := strconv.ParseInt(hexStr, 16, 32)
			if codePoint > 0 && codePoint <= 0x10FFFF {
				result.WriteRune(rune(codePoint))
			}
			// Consume one trailing whitespace if present
			if i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == '\f') {
				i++
			}
		} else {
			// Single character escape - just output the character
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// isHexDigit returns true if c is a hexadecimal digit
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// DeclarationResult holds parsed declarations and their important flags
type DeclarationResult struct {
	Declarations map[string]string
	Important    map[string]bool
}

// parseDeclarations parses CSS declarations into a map.
// Invalid declarations are silently skipped (error recovery).
func parseDeclarations(declStr string) DeclarationResult {
	result := DeclarationResult{
		Declarations: make(map[string]string),
		Important:    make(map[string]bool),
	}

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

		// Unescape CSS escape sequences in both property and value
		property = unescapeCSS(property)
		// IMPORTANT: Don't unescape the quotes property value here, as it needs special handling
		// in parseQuotes (layout.go) to preserve the quote string structure
		if property != "quotes" {
			value = unescapeCSS(value)
		}

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
		isImportant := false
		if strings.Contains(value, "!") {
			bangIdx := strings.Index(value, "!")
			afterBang := strings.TrimSpace(value[bangIdx+1:])
			if strings.EqualFold(afterBang, "important") {
				value = strings.TrimSpace(value[:bangIdx])
				isImportant = true
			} else {
				// Invalid use of ! (e.g., "red ! error") — reject entire declaration
				continue
			}
		}

		// CSS 2.1: Reject bare non-zero numbers for length properties (must have units)
		if isLengthProperty(property) && isInvalidBareNumber(value) {
			continue
		}

		// Validate color property values before they enter the cascade
		if isColorProperty(property) {
			if !isValidColorValue(value) {
				continue
			}
		}

		// Expand shorthand properties (reuse from Phase 2)
		style := NewStyle()
		expandShorthand(style, property, value)

		// Copy all expanded properties to declarations, validating color values
		for k, v := range style.Properties {
			if isColorProperty(k) {
				if !isValidColorValue(v) {
					continue
				}
			}
			result.Declarations[k] = v
			if isImportant {
				result.Important[k] = true
			}
		}
	}

	return result
}

// isLengthProperty returns true for CSS properties that expect length values
func isLengthProperty(prop string) bool {
	switch prop {
	case "width", "height", "min-width", "min-height", "max-width", "max-height",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border-width", "border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
		"top", "right", "bottom", "left",
		"font-size", "letter-spacing", "word-spacing",
		"text-indent", "vertical-align":
		// NOTE: line-height is NOT included here because CSS allows bare unitless
		// numbers (e.g., "line-height: 2") as multipliers of font-size.
		return true
	}
	return false
}

// isColorProperty returns true for CSS properties that expect color values
func isColorProperty(prop string) bool {
	switch prop {
	case "color", "background-color", "border-color",
		"border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		return true
	}
	return false
}

// isValidColorValue checks if a value is a valid CSS color (parsed color, currentcolor, or inherit)
func isValidColorValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "currentcolor" || lower == "inherit" {
		return true
	}
	// var() references are resolved later — always valid at parse time
	if strings.Contains(value, "var(") {
		return true
	}
	_, ok := ParseColor(value)
	return ok
}

// isInvalidBareNumber returns true if value is a non-zero number with no unit
func isInvalidBareNumber(value string) bool {
	num, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false // not a bare number
	}
	return num != 0 // zero without units is valid
}

// ParseSelector parses a CSS selector string into a Selector struct.
func ParseSelector(selectorStr string) Selector {
	return parseSelector(selectorStr)
}

// SplitSelectorGroup splits a comma-separated selector group into individual selectors.
func SplitSelectorGroup(s string) []string {
	return splitSelectorGroup(s)
}

// parseFontFaceRule parses a @font-face { ... } rule string.
func parseFontFaceRule(ruleStr string) *FontFaceRule {
	// Extract the block between { and }
	start := strings.Index(ruleStr, "{")
	end := strings.LastIndex(ruleStr, "}")
	if start < 0 || end <= start {
		return nil
	}
	block := ruleStr[start+1 : end]

	ff := &FontFaceRule{Weight: "normal", Style: "normal"}

	for _, decl := range strings.Split(block, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			continue
		}
		prop := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch prop {
		case "font-family":
			// Strip quotes
			ff.Family = strings.Trim(val, `"'`)
		case "src":
			// Parse url(...) format(...)
			ff.Src, ff.Format = parseFontFaceSrc(val)
		case "font-weight":
			ff.Weight = val
		case "font-style":
			ff.Style = val
		}
	}

	if ff.Family == "" || ff.Src == "" {
		return nil
	}
	return ff
}

// parseKeyframesRule parses a @keyframes or @-webkit-keyframes rule.
// Returns the animation name and a slice of KeyframeRule stops.
func parseKeyframesRule(ruleStr string) (string, []KeyframeRule) {
	rest := strings.TrimSpace(ruleStr)
	// Strip the at-rule prefix (@keyframes or @-webkit-keyframes)
	for _, prefix := range []string{"@-webkit-keyframes", "@keyframes"} {
		if strings.HasPrefix(rest, prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			break
		}
	}

	// Find the opening brace — name is everything before it
	bracePos := strings.Index(rest, "{")
	if bracePos < 0 {
		return "", nil
	}
	name := strings.TrimSpace(rest[:bracePos])
	if name == "" {
		return "", nil
	}

	// Extract the inner block (between outermost { and last })
	innerEnd := strings.LastIndex(rest, "}")
	if innerEnd <= bracePos {
		return "", nil
	}
	inner := rest[bracePos+1 : innerEnd]

	// Parse each keyframe stop: "from { ... }", "to { ... }", "50% { ... }"
	var frames []KeyframeRule
	for _, part := range splitRules(inner) {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, "{") {
			continue
		}
		bp := strings.Index(part, "{")
		stop := strings.TrimSpace(part[:bp])
		if stop == "" {
			continue
		}
		declEnd := strings.LastIndex(part, "}")
		if declEnd <= bp {
			continue
		}
		declStr := part[bp+1 : declEnd]
		decls := parseDeclarations(declStr)
		frames = append(frames, KeyframeRule{
			Stop:         stop,
			Declarations: decls.Declarations,
		})
	}
	return name, frames
}

// parseFontFaceSrc extracts the URL and format from a src value like:
// url("font.woff") format("woff"), url("font.ttf") format("truetype")
// Returns the first usable URL and its format.
func parseFontFaceSrc(src string) (string, string) {
	// Split by comma for multiple sources
	sources := strings.Split(src, ",")
	for _, source := range sources {
		source = strings.TrimSpace(source)
		// Extract url(...)
		urlStart := strings.Index(source, "url(")
		if urlStart < 0 {
			continue
		}
		urlContent := source[urlStart+4:]
		urlEnd := strings.Index(urlContent, ")")
		if urlEnd < 0 {
			continue
		}
		url := strings.TrimSpace(urlContent[:urlEnd])
		url = strings.Trim(url, `"'`)

		// Extract format(...) if present
		format := ""
		fmtStart := strings.Index(source, "format(")
		if fmtStart >= 0 {
			fmtContent := source[fmtStart+7:]
			fmtEnd := strings.Index(fmtContent, ")")
			if fmtEnd >= 0 {
				format = strings.Trim(strings.TrimSpace(fmtContent[:fmtEnd]), `"'`)
			}
		}

		// Skip WOFF2 (requires Brotli decoder)
		if format == "woff2" {
			continue
		}

		return url, format
	}
	return "", ""
}

