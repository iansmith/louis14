package css

import (
	"louis14/pkg/html"
	"sort"
)

// Phase 3: CSS Cascade - computing final styles for a node

// Phase 17: applyUserAgentStyles applies default browser styles based on element type
func applyUserAgentStyles(node *html.Node, style *Style) {
	if node.Type != html.ElementNode {
		return
	}

	// Default styles for <a> (anchor/link) elements
	if node.TagName == "a" {
		style.Set("color", "#0645ad")           // Standard link blue
		style.Set("text-decoration", "underline")
	}
}

// ComputeStyle computes the final style for a node by applying the cascade
// Phase 22: Added viewport dimensions for media query evaluation
func ComputeStyle(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) *Style {
	finalStyle := NewStyle()

	// Phase 17: Apply user agent (default browser) styles first
	applyUserAgentStyles(node, finalStyle)

	// Collect all matching rules from all stylesheets
	allRules := make([]Rule, 0)

	for _, stylesheet := range stylesheets {
		matches := FindMatchingRules(node, stylesheet, viewportWidth, viewportHeight)
		allRules = append(allRules, matches...)
	}

	// Sort rules by specificity (lowest first)
	sort.Slice(allRules, func(i, j int) bool {
		return allRules[i].Selector.Specificity < allRules[j].Selector.Specificity
	})

	// Apply rules in order (lower specificity first, higher specificity overwrites)
	for _, rule := range allRules {
		for property, value := range rule.Declarations {
			finalStyle.Set(property, value)
		}
	}

	// Inline styles have highest specificity (specificity = 1000)
	if styleAttr, ok := node.GetAttribute("style"); ok {
		inlineStyle := ParseInlineStyle(styleAttr)
		for property, value := range inlineStyle.Properties {
			finalStyle.Set(property, value)
		}
	}

	return finalStyle
}

// ApplyStylesToDocument applies stylesheets to all nodes in the document
// Phase 22: Added viewport dimensions for media query evaluation
func ApplyStylesToDocument(doc *html.Document, viewportWidth, viewportHeight float64) map[*html.Node]*Style {
	styles := make(map[*html.Node]*Style)

	// Parse all stylesheets
	stylesheets := make([]*Stylesheet, 0)
	for _, cssText := range doc.Stylesheets {
		stylesheet, err := ParseStylesheet(cssText)
		if err == nil {
			stylesheets = append(stylesheets, stylesheet)
		}
	}

	// Recursively apply styles to all nodes
	applyStylesToNode(doc.Root, stylesheets, styles, viewportWidth, viewportHeight)

	return styles
}

// Phase 11: ComputePseudoElementStyle computes the style for a pseudo-element
// Phase 22: Added viewport dimensions for media query evaluation
func ComputePseudoElementStyle(node *html.Node, pseudoElement string, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) *Style {
	finalStyle := NewStyle()

	// Collect all matching rules for this pseudo-element
	allRules := make([]Rule, 0)

	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			// Phase 22: Check media query
			if !EvaluateMediaQuery(rule.MediaQuery, viewportWidth, viewportHeight) {
				continue
			}

			// Check if this rule's selector matches the node AND has the right pseudo-element
			if rule.Selector.PseudoElement == pseudoElement {
				// Check if the base selector matches
				if MatchesSelector(node, rule.Selector) {
					allRules = append(allRules, rule)
				}
			}
		}
	}

	// Sort rules by specificity
	sort.Slice(allRules, func(i, j int) bool {
		return allRules[i].Selector.Specificity < allRules[j].Selector.Specificity
	})

	// Apply rules in order
	for _, rule := range allRules {
		for property, value := range rule.Declarations {
			finalStyle.Set(property, value)
		}
	}

	return finalStyle
}

// applyStylesToNode recursively applies styles to a node and its children
func applyStylesToNode(node *html.Node, stylesheets []*Stylesheet, styles map[*html.Node]*Style, viewportWidth, viewportHeight float64) {
	if node.Type == html.ElementNode && node.TagName != "document" {
		styles[node] = ComputeStyle(node, stylesheets, viewportWidth, viewportHeight)
	}

	// Always traverse children
	for _, child := range node.Children {
		applyStylesToNode(child, stylesheets, styles, viewportWidth, viewportHeight)
	}
}
