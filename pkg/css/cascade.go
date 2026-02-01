package css

import (
	"louis14/pkg/html"
	"sort"
)

// Phase 3: CSS Cascade - computing final styles for a node

// ComputeStyle computes the final style for a node by applying the cascade
func ComputeStyle(node *html.Node, stylesheets []*Stylesheet) *Style {
	finalStyle := NewStyle()

	// Collect all matching rules from all stylesheets
	allRules := make([]Rule, 0)

	for _, stylesheet := range stylesheets {
		matches := FindMatchingRules(node, stylesheet)
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
func ApplyStylesToDocument(doc *html.Document) map[*html.Node]*Style {
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
	applyStylesToNode(doc.Root, stylesheets, styles)

	return styles
}

// applyStylesToNode recursively applies styles to a node and its children
func applyStylesToNode(node *html.Node, stylesheets []*Stylesheet, styles map[*html.Node]*Style) {
	if node.Type == html.ElementNode && node.TagName != "document" {
		styles[node] = ComputeStyle(node, stylesheets)
	}

	// Always traverse children
	for _, child := range node.Children {
		applyStylesToNode(child, stylesheets, styles)
	}
}
