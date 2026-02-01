package css

import "louis14/pkg/html"

// Phase 3: Selector matching

// MatchesSelector returns true if the node matches the selector
func MatchesSelector(node *html.Node, selector Selector) bool {
	if node.Type != html.ElementNode {
		return false
	}

	switch selector.Type {
	case ElementSelector:
		// Match element name (e.g., "div" matches <div>)
		return node.TagName == selector.Value

	case ClassSelector:
		// Match class attribute (e.g., ".highlight" matches class="highlight")
		if class, ok := node.GetAttribute("class"); ok {
			// Simple class matching (doesn't handle multiple classes yet)
			return class == selector.Value
		}
		return false

	case IDSelector:
		// Match id attribute (e.g., "#header" matches id="header")
		if id, ok := node.GetAttribute("id"); ok {
			return id == selector.Value
		}
		return false
	}

	return false
}

// FindMatchingRules returns all rules that match the given node
func FindMatchingRules(node *html.Node, stylesheet *Stylesheet) []Rule {
	matches := make([]Rule, 0)

	for _, rule := range stylesheet.Rules {
		if MatchesSelector(node, rule.Selector) {
			matches = append(matches, rule)
		}
	}

	return matches
}
