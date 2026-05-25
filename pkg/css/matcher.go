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
		if node.Parent == nil || node.Parent.TagName != "document" {
			return false
		}
		for _, child := range node.Parent.Children {
			if child.Type == html.ElementNode {
				return child == node
			}
		}
		return false
	case pc == "empty":
		return len(node.Children) == 0
	case strings.HasPrefix(pc, "nth-child("):
		arg := pc[len("nth-child(") : len(pc)-1] // strip "nth-child(" and ")"
		return matchesNthChild(node, arg)
	case strings.HasPrefix(pc, "not("):
		arg := pc[len("not(") : len(pc)-1] // strip "not(" and ")"
		// Split by comma (paren-aware) to support selector lists: :not(A, B, C)
		// Element must NOT match ANY of the selectors in the list
		selectors := splitSelectorGroup(strings.TrimSpace(arg))
		for _, sel := range selectors {
			sel = strings.TrimSpace(sel)
			if sel == "" {
				continue
			}
			innerSel := ParseSelector(sel)
			if len(innerSel.Parts) > 0 && matchesSelectorPart(node, innerSel.Parts[len(innerSel.Parts)-1]) {
				return false // Element matches one of the :not() selectors → fail
			}
		}
		return true // Element doesn't match any → passes :not()
	case pc == "hover", pc == "focus", pc == "active":
		// Dynamic pseudo-classes never match in a static renderer
		return false
	case pc == "visited":
		// :visited reflects browsing history, which a static renderer has no
		// general model for — so an arbitrary link is treated as unvisited.
		// But a hyperlink with an *empty* href is defined to resolve to the
		// current document's own URL, which is unconditionally in history
		// whenever that document is being rendered. So `<a href="">` always
		// matches :visited; this is exact, not a heuristic. A fragment href
		// ("#name") resolves to current-URL-plus-fragment — a distinct URL
		// that a reftest run did not navigate to — so it stays unvisited.
		switch node.TagName {
		case "a", "area", "link":
			href, ok := node.GetAttribute("href")
			if !ok {
				return false
			}
			return strings.TrimSpace(href) == ""
		}
		return false
	case pc == "focus-visible", pc == "focus-within":
		// Focus pseudo-classes: no focus state in static renderer
		return false
	case pc == "target":
		// :target matches element with matching URL fragment — not available in static renderer
		return false
	case pc == "placeholder-shown":
		// :placeholder-shown: inputs don't show placeholders in static render
		return false
	case pc == "autofill", pc == "-webkit-autofill":
		// Autofill state: never applies in static renderer
		return false
	case pc == "paused", pc == "playing":
		// Animation state pseudo-classes: not applicable in static renderer
		return false
	case pc == "muted", pc == "volume-locked":
		// Media element state: not applicable in static renderer
		return false
	case pc == "local-link":
		// :local-link matches anchors linking to current page — can't determine in static render
		return false
	case pc == "any-link", pc == "-webkit-any-link":
		// :any-link matches <a>, <area>, <link> elements that have an href attribute
		if node.Type != html.ElementNode {
			return false
		}
		tag := node.TagName
		if tag != "a" && tag != "area" && tag != "link" {
			return false
		}
		_, hasHref := node.GetAttribute("href")
		return hasHref
	case pc == "scope":
		// :scope matches the element serving as the scope reference.
		// In a static document context, approximate as :root (first element child of document).
		if node.Parent == nil || node.Parent.TagName != "document" {
			return false
		}
		for _, child := range node.Parent.Children {
			if child.Type == html.ElementNode {
				return child == node
			}
		}
		return false
	case pc == "link":
		return node.TagName == "a"
	case strings.HasPrefix(pc, "is("):
		// :is() matches if ANY selector in the comma-separated list matches
		arg := pc[len("is(") : len(pc)-1]
		selectors := splitSelectorGroup(strings.TrimSpace(arg))
		for _, sel := range selectors {
			innerSel := ParseSelector(strings.TrimSpace(sel))
			if len(innerSel.Parts) > 0 {
				if matchesSelectorPart(node, innerSel.Parts[len(innerSel.Parts)-1]) {
					return true
				}
			}
		}
		return false
	case strings.HasPrefix(pc, "where("):
		// :where() is identical to :is() but with zero specificity
		arg := pc[len("where(") : len(pc)-1]
		selectors := splitSelectorGroup(strings.TrimSpace(arg))
		for _, sel := range selectors {
			innerSel := ParseSelector(strings.TrimSpace(sel))
			if len(innerSel.Parts) > 0 {
				if matchesSelectorPart(node, innerSel.Parts[len(innerSel.Parts)-1]) {
					return true
				}
			}
		}
		return false
	case strings.HasPrefix(pc, "has("):
		arg := pc[len("has(") : len(pc)-1]
		return matchesHas(node, strings.TrimSpace(arg))
	case pc == "first-of-type":
		return nthOfTypeIndex(node) == 1
	case pc == "last-of-type":
		return nthOfTypeIndex(node) == countOfType(node)
	case pc == "only-of-type":
		return countOfType(node) == 1
	case strings.HasPrefix(pc, "nth-of-type("):
		arg := pc[len("nth-of-type(") : len(pc)-1]
		return matchesAnPlusB(nthOfTypeIndex(node), arg)
	case strings.HasPrefix(pc, "nth-last-of-type("):
		arg := pc[len("nth-last-of-type(") : len(pc)-1]
		posFromEnd := countOfType(node) - nthOfTypeIndex(node) + 1
		return matchesAnPlusB(posFromEnd, arg)
	case strings.HasPrefix(pc, "nth-last-child("):
		arg := pc[len("nth-last-child(") : len(pc)-1]
		posFromEnd := totalElementChildren(node) - nthChildIndex(node) + 1
		return matchesAnPlusB(posFromEnd, arg)
	case pc == "enabled":
		return isFormElement(node) && !hasAttribute(node, "disabled")
	case pc == "disabled":
		return isFormElement(node) && hasAttribute(node, "disabled")
	case pc == "checked":
		return isCheckableElement(node) && hasAttribute(node, "checked")
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

// matchesHas implements the :has() pseudo-class
func matchesHas(node *html.Node, selectorStr string) bool {
	selectors := splitSelectorGroup(strings.TrimSpace(selectorStr))
	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}
		if strings.HasPrefix(sel, ">") {
			// Direct child combinator
			innerSel := strings.TrimSpace(sel[1:])
			parsed := ParseSelector(innerSel)
			if len(parsed.Parts) > 0 {
				for _, child := range node.Children {
					if child.Type == html.ElementNode {
						if matchesSelectorPart(child, parsed.Parts[len(parsed.Parts)-1]) {
							return true
						}
					}
				}
			}
		} else if strings.HasPrefix(sel, "+") {
			// Adjacent sibling combinator
			innerSel := strings.TrimSpace(sel[1:])
			parsed := ParseSelector(innerSel)
			if len(parsed.Parts) > 0 {
				next := getNextElementSibling(node)
				if next != nil && matchesSelectorPart(next, parsed.Parts[len(parsed.Parts)-1]) {
					return true
				}
			}
		} else if strings.HasPrefix(sel, "~") {
			// General sibling combinator
			innerSel := strings.TrimSpace(sel[1:])
			parsed := ParseSelector(innerSel)
			if len(parsed.Parts) > 0 {
				for _, sib := range getSubsequentSiblings(node) {
					if matchesSelectorPart(sib, parsed.Parts[len(parsed.Parts)-1]) {
						return true
					}
				}
			}
		} else {
			// Default: descendant combinator
			parsed := ParseSelector(sel)
			if len(parsed.Parts) > 0 {
				if hasMatchingDescendant(node, parsed.Parts[len(parsed.Parts)-1]) {
					return true
				}
			}
		}
	}
	return false
}

func getNextElementSibling(node *html.Node) *html.Node {
	if node.Parent == nil {
		return nil
	}
	found := false
	for _, child := range node.Parent.Children {
		if found && child.Type == html.ElementNode {
			return child
		}
		if child == node {
			found = true
		}
	}
	return nil
}

func getSubsequentSiblings(node *html.Node) []*html.Node {
	if node.Parent == nil {
		return nil
	}
	var result []*html.Node
	found := false
	for _, child := range node.Parent.Children {
		if found && child.Type == html.ElementNode {
			result = append(result, child)
		}
		if child == node {
			found = true
		}
	}
	return result
}

func hasMatchingDescendant(node *html.Node, part SelectorPart) bool {
	for _, child := range node.Children {
		if child.Type == html.ElementNode {
			if matchesSelectorPart(child, part) {
				return true
			}
			if hasMatchingDescendant(child, part) {
				return true
			}
		}
	}
	return false
}

// nthOfTypeIndex returns the 1-based index among siblings with the same tag name
func nthOfTypeIndex(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode && c.TagName == node.TagName {
			count++
			if c == node {
				return count
			}
		}
	}
	return 0
}

// countOfType returns total siblings with same tag name
func countOfType(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode && c.TagName == node.TagName {
			count++
		}
	}
	return count
}

// totalElementChildren returns total element children of parent
func totalElementChildren(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode {
			count++
		}
	}
	return count
}

// matchesAnPlusB checks if position matches An+B formula
func matchesAnPlusB(position int, arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "odd" {
		return position%2 == 1
	}
	if arg == "even" {
		return position%2 == 0
	}
	a, b := parseAnPlusB(arg)
	if a == 0 {
		return position == b
	}
	diff := position - b
	if a > 0 {
		return diff >= 0 && diff%a == 0
	}
	return diff <= 0 && diff%a == 0
}

func isFormElement(node *html.Node) bool {
	switch node.TagName {
	case "input", "select", "textarea", "button":
		return true
	}
	return false
}

func isCheckableElement(node *html.Node) bool {
	if node.TagName != "input" {
		return false
	}
	inputType, _ := node.GetAttribute("type")
	return inputType == "checkbox" || inputType == "radio" || inputType == ""
}

func hasAttribute(node *html.Node, name string) bool {
	_, exists := node.GetAttribute(name)
	return exists
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

		// Skip container query rules (evaluated separately in applyContainerQueryRules)
		if rule.ContainerQuery != nil {
			continue
		}

		// Phase 22: Check media query first (Kleene 3-valued — apply only when
		// the result is definitely true; Unknown does not apply).
		if EvaluateMediaQuery(rule.MediaQuery, viewportWidth, viewportHeight) != MediaQueryTrue {
			continue
		}

		if MatchesSelector(node, rule.Selector) {
			matches = append(matches, rule)
		}
	}

	return matches
}
