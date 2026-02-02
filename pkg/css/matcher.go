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

	// Pseudo-classes: dynamic pseudo-classes never match in a static renderer
	for _, pc := range part.PseudoClasses {
		switch pc {
		case "hover", "focus", "active", "visited":
			return false
		default:
			// Unknown pseudo-class: never match
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

// FindMatchingRules returns all rules that match the given node
// Phase 22: Added viewport dimensions for media query evaluation
func FindMatchingRules(node *html.Node, stylesheet *Stylesheet, viewportWidth, viewportHeight float64) []Rule {
	matches := make([]Rule, 0)

	for _, rule := range stylesheet.Rules {
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
