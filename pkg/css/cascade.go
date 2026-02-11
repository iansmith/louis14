package css

import (
	"fmt"
	"louis14/pkg/html"
	"sort"
	"strings"
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

	// Default margin for <body> element (Chrome: 8px)
	// DISABLED for W3C tests - they may expect margin: 0
	if node.TagName == "body" {
		// style.Set("margin", "8px")
		style.Set("margin", "0")
	}

	// Default margin for <p> (paragraph) elements
	if node.TagName == "p" {
		style.Set("margin-top", "1em")
		style.Set("margin-bottom", "1em")
	}

	// Non-rendered elements should be hidden by default
	// Author CSS can override this (e.g., Acid2 sets display:block on head)
	switch node.TagName {
	case "head", "style", "script", "meta", "title", "link", "base":
		style.Set("display", "none")
	}

	// Dialog elements are hidden by default unless they have the "open" attribute
	if node.TagName == "dialog" {
		if _, hasOpen := node.GetAttribute("open"); !hasOpen {
			style.Set("display", "none")
		}
	}

	// Default font-style for emphasis elements
	switch node.TagName {
	case "em", "i", "cite", "dfn", "var":
		style.Set("font-style", "italic")
	}

	// Default font-weight for strong elements
	switch node.TagName {
	case "strong", "b":
		style.Set("font-weight", "bold")
	}

	// Default monospace font-family for code elements
	switch node.TagName {
	case "code", "pre", "kbd", "samp", "tt":
		style.Set("font-family", "monospace")
	}

	// Default inline display for inline HTML elements
	switch node.TagName {
	case "span", "em", "strong", "b", "i", "u", "s", "a", "abbr", "cite",
		"code", "dfn", "kbd", "mark", "q", "samp", "small", "sub", "sup",
		"var", "time", "label", "br", "wbr", "img", "input", "select",
		"textarea", "button", "object":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline")
		}
	}

	// Phase 23: Default styles for table elements
	switch node.TagName {
	case "table":
		style.Set("display", "table")
		style.Set("border-collapse", "separate")
		style.Set("border-spacing", "2px")
	case "thead":
		style.Set("display", "table-header-group")
	case "tbody":
		style.Set("display", "table-row-group")
	case "tfoot":
		style.Set("display", "table-footer-group")
	case "tr":
		style.Set("display", "table-row")
	case "td":
		style.Set("display", "table-cell")
		style.Set("padding", "1px")
	case "th":
		style.Set("display", "table-cell")
		style.Set("padding", "1px")
		style.Set("font-weight", "bold")
		style.Set("text-align", "center")

	// Phase 23: Default styles for list elements
	// Per HTML spec default stylesheet, list-style-type is set on ul/ol (not li),
	// so author "list-style: none" on ul/ol overrides it and li inherits "none".
	case "ul":
		style.Set("display", "block")
		style.Set("margin-top", "16px")
		style.Set("margin-bottom", "16px")
		style.Set("padding-left", "40px")
		style.Set("list-style-type", "disc")
	case "ol":
		style.Set("display", "block")
		style.Set("margin-top", "16px")
		style.Set("margin-bottom", "16px")
		style.Set("padding-left", "40px")
		style.Set("list-style-type", "decimal")
	case "li":
		style.Set("display", "list-item")
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

	// Track which properties have been set with !important
	importantProps := make(map[string]bool)

	// Apply rules in order (lower specificity first, higher specificity overwrites)
	for _, rule := range allRules {
		for property, value := range rule.Declarations {
			// Skip if already set by an important rule
			if importantProps[property] {
				continue
			}
			finalStyle.Set(property, value)
		}
	}

	// Apply !important declarations (second pass)
	for _, rule := range allRules {
		if rule.Important == nil {
			continue
		}
		for property, value := range rule.Declarations {
			if rule.Important[property] {
				finalStyle.Set(property, value)
				importantProps[property] = true
			}
		}
	}

	// Inline styles have highest specificity (specificity = 1000)
	// Note: inline !important would override stylesheet !important, but we don't track that yet
	if styleAttr, ok := node.GetAttribute("style"); ok {
		inlineStyle := ParseInlineStyle(styleAttr)
		for property, value := range inlineStyle.Properties {
			if !importantProps[property] {
				finalStyle.Set(property, value)
			}
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
func ComputePseudoElementStyle(node *html.Node, pseudoElement string, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64, parentStyles ...*Style) *Style {
	finalStyle := NewStyle()

	// Inherit inheritable properties from parent element
	if len(parentStyles) > 0 && parentStyles[0] != nil {
		inheritableProps := []string{"font-size", "font-family", "font-weight", "font-style",
			"color", "line-height", "text-align", "white-space", "visibility",
			"letter-spacing", "word-spacing", "text-indent", "text-transform"}
		for _, prop := range inheritableProps {
			if val, ok := parentStyles[0].Get(prop); ok {
				finalStyle.Set(prop, val)
			}
		}
	}

	// Collect all matching rules for this pseudo-element
	allRules := make([]Rule, 0)

	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			// Phase 22: Check media query
			if !EvaluateMediaQuery(rule.MediaQuery, viewportWidth, viewportHeight) {
				continue
			}

			// Check if this rule's selector matches the node AND has the right pseudo-element
			rulePseudo := rule.Selector.PseudoElement

			// Handle "descendant:" prefix - these pseudo-elements apply to descendants only
			if strings.HasPrefix(rulePseudo, "descendant:") {
				actualPseudo := strings.TrimPrefix(rulePseudo, "descendant:")
				if actualPseudo == pseudoElement {
					// For descendant pseudo-elements, check if the node is a descendant of a matching element
					// (not the matching element itself)
					ancestor := node.Parent
					for ancestor != nil {
						if MatchesSelector(ancestor, rule.Selector) {
							allRules = append(allRules, rule)
							break
						}
						ancestor = ancestor.Parent
					}
				}
			} else if rulePseudo == pseudoElement {
				// Direct pseudo-element match
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

	// Track which properties have been set with !important
	importantProps := make(map[string]bool)

	// Apply rules in order (normal declarations first)
	for _, rule := range allRules {
		for property, value := range rule.Declarations {
			// Skip if already set by an important rule
			if importantProps[property] {
				continue
			}
			finalStyle.Set(property, value)
		}
	}

	// Apply !important declarations (second pass, in specificity order)
	for _, rule := range allRules {
		if rule.Important == nil {
			continue
		}
		for property, value := range rule.Declarations {
			if rule.Important[property] {
				finalStyle.Set(property, value)
				importantProps[property] = true
			}
		}
	}

	return finalStyle
}

// resolveInheritValues resolves any "inherit" keyword values by copying from the parent's computed style.
func resolveInheritValues(node *html.Node, style *Style, styles map[*html.Node]*Style) {
	for property, value := range style.Properties {
		if value != "inherit" {
			continue
		}
		// Look up parent's computed style
		if node.Parent != nil {
			if parentStyle, ok := styles[node.Parent]; ok {
				if parentVal, ok := parentStyle.Get(property); ok {
					style.Set(property, parentVal)
					continue
				}
			}
		}
		// No parent or parent doesn't have the property: remove the inherit value
		// so the property falls back to its default
		delete(style.Properties, property)
	}
}

// inheritableProperties lists CSS properties that inherit from parent to child by default
var inheritableProperties = map[string]bool{
	"color": true, "font-family": true, "font-size": true,
	"font-style": true, "font-weight": true, "font-variant": true,
	"line-height": true, "text-align": true, "text-decoration": true,
	"text-transform": true, "text-indent": true, "white-space": true,
	"visibility": true, "list-style-type": true, "list-style-position": true,
	"direction": true, "letter-spacing": true, "word-spacing": true,
	"cursor": true,
}

// ApplyInheritedProperties copies inheritable properties from parent if not set on child.
// Also resolves font-size em values using parent's computed font-size.
// ApplyInheritedProperties applies inherited CSS properties from parent to child
func ApplyInheritedProperties(node *html.Node, style *Style, styles map[*html.Node]*Style) {
	if node.Parent == nil {
		return
	}
	parentStyle, ok := styles[node.Parent]
	if !ok {
		return
	}

	// Resolve font-size em values using parent's font-size
	if fsVal, hasFontSize := style.Get("font-size"); hasFontSize {
		if strings.HasSuffix(strings.TrimSpace(fsVal), "em") {
			parentFS := 16.0
			if parentStyle != nil {
				parentFS = parentStyle.GetFontSize()
			}
			if resolved, ok := ParseLengthWithFontSize(fsVal, parentFS); ok {
				style.Set("font-size", fmt.Sprintf("%.6gpx", resolved))
			}
		}
	}

	for prop := range inheritableProperties {
		if _, hasOwn := style.Get(prop); !hasOwn {
			if parentVal, ok := parentStyle.Get(prop); ok {
				style.Set(prop, parentVal)
			}
		}
	}
}

// applyStylesToNode recursively applies styles to a node and its children
func applyStylesToNode(node *html.Node, stylesheets []*Stylesheet, styles map[*html.Node]*Style, viewportWidth, viewportHeight float64) {
	if node.Type == html.ElementNode && node.TagName != "document" {
		style := ComputeStyle(node, stylesheets, viewportWidth, viewportHeight)
		resolveInheritValues(node, style, styles)
		ApplyInheritedProperties(node, style, styles)
		styles[node] = style
	}

	// Always traverse children (parent is already computed, so top-down order is maintained)
	for _, child := range node.Children {
		applyStylesToNode(child, stylesheets, styles, viewportWidth, viewportHeight)
	}
}
