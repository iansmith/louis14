package css

import (
	"strings"

	"louis14/pkg/html"
)

// Phase 3: Selector matching

// Phase 17: MatchesSelector returns true if the node matches the complex selector
func MatchesSelector(node *html.Node, selector Selector) bool {
	if node.Type != html.ElementNode {
		return false
	}

	// Handle complex selector (with multiple parts and combinators)
	if len(selector.Parts) == 0 {
		return false
	}

	// Start matching from the rightmost part (the target element)
	return matchesCompoundSelector(node, selector, len(selector.Parts)-1)
}

// matchesCompoundSelector checks if the node matches the selector at the given part index
// and all ancestor requirements
func matchesCompoundSelector(node *html.Node, selector Selector, partIndex int) bool {
	// Match the current part against the node
	if !matchesSelectorPart(node, selector.Parts[partIndex]) {
		return false
	}

	// If this is the first part, we're done
	if partIndex == 0 {
		return true
	}

	// Check the combinator with the previous part
	combinator := selector.Combinators[partIndex-1]
	prevPartIndex := partIndex - 1

	switch combinator {
	case DescendantCombinator:
		// Match any ancestor
		return matchesAncestor(node, selector, prevPartIndex)

	case ChildCombinator:
		// Match direct parent only (skip synthetic document node)
		if node.Parent != nil && node.Parent.TagName != "document" {
			return matchesCompoundSelector(node.Parent, selector, prevPartIndex)
		}
		return false

	case AdjacentSiblingCombinator:
		// Match immediate previous sibling
		prevSibling := getPreviousSibling(node)
		if prevSibling != nil {
			return matchesCompoundSelector(prevSibling, selector, prevPartIndex)
		}
		return false

	case GeneralSiblingCombinator:
		// Match any previous sibling
		return matchesPreviousSibling(node, selector, prevPartIndex)
	}

	return false
}

// matchesSelectorPart checks if a node matches a single selector part
func matchesSelectorPart(node *html.Node, part SelectorPart) bool {
	// Match element
	if part.Element != "" && part.Element != "*" {
		if node.TagName != part.Element {
			return false
		}
	}

	// Match ID
	if part.ID != "" {
		if id, ok := node.GetAttribute("id"); !ok || id != part.ID {
			return false
		}
	}

	// Match classes
	if len(part.Classes) > 0 {
		classAttr, ok := node.GetAttribute("class")
		if !ok {
			return false
		}
		nodeClasses := strings.Split(classAttr, " ")
		for _, requiredClass := range part.Classes {
			found := false
			for _, nodeClass := range nodeClasses {
				if strings.TrimSpace(nodeClass) == requiredClass {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	// Match attributes
	for _, attrSel := range part.Attributes {
		if !matchesAttributeSelector(node, attrSel) {
			return false
		}
	}

	// Pseudo-classes
	for _, pc := range part.PseudoClasses {
		if !matchesPseudoClass(node, pc) {
			return false
		}
	}

	return true
}

// matchesAttributeSelector checks if a node matches an attribute selector
func matchesAttributeSelector(node *html.Node, attr AttributeSelector) bool {
	value, ok := node.GetAttribute(attr.Name)
	if !ok {
		return false
	}

	// If no operator, just check existence
	if attr.Operator == "" {
		return true
	}

	switch attr.Operator {
	case "=":
		// Exact match
		return value == attr.Value
	case "^=":
		// Starts with
		return strings.HasPrefix(value, attr.Value)
	case "$=":
		// Ends with
		return strings.HasSuffix(value, attr.Value)
	case "*=":
		// Contains
		return strings.Contains(value, attr.Value)
	case "~=":
		// Word match (whitespace-separated)
		words := strings.Fields(value)
		for _, word := range words {
			if word == attr.Value {
				return true
			}
		}
		return false
	case "|=":
		// Language prefix (starts with value or value-)
		return value == attr.Value || strings.HasPrefix(value, attr.Value+"-")
	}

	return false
}

// matchesAncestor checks if any ancestor matches the selector part
func matchesAncestor(node *html.Node, selector Selector, partIndex int) bool {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type == html.ElementNode && ancestor.TagName != "document" {
			if matchesCompoundSelector(ancestor, selector, partIndex) {
				return true
			}
		}
	}
	return false
}

// matchesPreviousSibling checks if any previous sibling matches the selector part
func matchesPreviousSibling(node *html.Node, selector Selector, partIndex int) bool {
	for sibling := getPreviousSibling(node); sibling != nil; sibling = getPreviousSibling(sibling) {
		if matchesCompoundSelector(sibling, selector, partIndex) {
			return true
		}
	}
	return false
}

// getPreviousSibling returns the previous element sibling of a node
func getPreviousSibling(node *html.Node) *html.Node {
	if node.Parent == nil {
		return nil
	}

	foundCurrent := false
	var prevElement *html.Node

	for _, sibling := range node.Parent.Children {
		if sibling == node {
			foundCurrent = true
			break
		}
		if sibling.Type == html.ElementNode {
			prevElement = sibling
		}
	}

	if foundCurrent {
		return prevElement
	}
	return nil
}

// matchesPseudoClass checks if a node matches a given pseudo-class.
func matchesPseudoClass(node *html.Node, pc string) bool {
	switch {
	case pc == "first-child":
		return isNthChild(node, 1)
	case pc == "last-child":
		return isLastChild(node)
	case pc == "only-child":
		return isNthChild(node, 1) && isLastChild(node)
	case pc == "root":
		return node.Parent != nil && node.Parent.TagName == "document"
	case pc == "empty":
		return len(node.Children) == 0
	case strings.HasPrefix(pc, "nth-child("):
		arg := pc[len("nth-child(") : len(pc)-1] // strip "nth-child(" and ")"
		return matchesNthChild(node, arg)
	case strings.HasPrefix(pc, "not("):
		arg := pc[len("not(") : len(pc)-1] // strip "not(" and ")"
		// Parse the inner selector and check if it does NOT match
		innerSel := ParseSelector(strings.TrimSpace(arg))
		return !matchesSelectorPart(node, innerSel.Parts[len(innerSel.Parts)-1])
	case pc == "hover", pc == "focus", pc == "active", pc == "visited":
		// Dynamic pseudo-classes never match in a static renderer
		return false
	case pc == "link":
		return node.TagName == "a"
	default:
		return false
	}
}

// isNthChild returns true if the node is the nth element child (1-based).
func isNthChild(node *html.Node, n int) bool {
	if node.Parent == nil {
		return n == 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode {
			count++
			if c == node {
				return count == n
			}
		}
	}
	return false
}

// isLastChild returns true if the node is the last element child.
func isLastChild(node *html.Node) bool {
	if node.Parent == nil {
		return true
	}
	for i := len(node.Parent.Children) - 1; i >= 0; i-- {
		c := node.Parent.Children[i]
		if c.Type == html.ElementNode {
			return c == node
		}
	}
	return false
}

// matchesNthChild checks the An+B formula.
func matchesNthChild(node *html.Node, arg string) bool {
	arg = strings.TrimSpace(arg)

	if arg == "odd" {
		return nthChildIndex(node)%2 == 1
	}
	if arg == "even" {
		return nthChildIndex(node)%2 == 0
	}

	// Parse An+B
	a, b := parseAnPlusB(arg)
	idx := nthChildIndex(node)
	if a == 0 {
		return idx == b
	}
	// Check if (idx - b) is divisible by a and non-negative
	diff := idx - b
	if a > 0 {
		return diff >= 0 && diff%a == 0
	}
	// a < 0: match when diff <= 0 and divisible
	return diff <= 0 && diff%a == 0
}

// nthChildIndex returns the 1-based index of node among element siblings.
func nthChildIndex(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode {
			count++
			if c == node {
				return count
			}
		}
	}
	return 0
}

// parseAnPlusB parses an An+B expression like "2n+1", "3n", "5", "-n+3".
func parseAnPlusB(s string) (a, b int) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")

	nIdx := strings.IndexByte(s, 'n')
	if nIdx < 0 {
		// Just B
		b, _ = parseInt(s)
		return 0, b
	}

	// Parse A
	aStr := s[:nIdx]
	switch aStr {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		a, _ = parseInt(aStr)
	}

	// Parse B
	rest := s[nIdx+1:]
	if rest == "" {
		return a, 0
	}
	b, _ = parseInt(rest)
	return a, b
}

func parseInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	if s[0] == '+' {
		s = s[1:]
	} else if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

// FindMatchingRules returns all rules that match the given node
// Phase 22: Added viewport dimensions for media query evaluation
func FindMatchingRules(node *html.Node, stylesheet *Stylesheet, viewportWidth, viewportHeight float64) []Rule {
	matches := make([]Rule, 0)

	for _, rule := range stylesheet.Rules {
		// Skip pseudo-element rules (they are applied via ComputePseudoElementStyle)
		if rule.Selector.PseudoElement != "" {
			continue
		}

		// Phase 22: Check media query first
		if !EvaluateMediaQuery(rule.MediaQuery, viewportWidth, viewportHeight) {
			continue
		}

		if MatchesSelector(node, rule.Selector) {
			matches = append(matches, rule)
		}
	}

	return matches
}
