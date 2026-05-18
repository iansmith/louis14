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

	// CSS 2.1 §15.3: initial value of font-family is UA-dependent.
	// Browsers default to serif; set on the root element so all descendants
	// inherit it. Author/inline styles override via normal cascade rules.
	if node.TagName == "html" {
		if _, ok := style.Get("font-family"); !ok {
			style.Set("font-family", "serif")
		}
	}

	// Default styles for <a> (anchor/link) elements
	if node.TagName == "a" {
		style.Set("color", "#0645ad") // Standard link blue
		style.Set("text-decoration", "underline")
	}

	// Default margin for <body> element (CSS 2.1 §8.3.1).
	// Physical: 8px on all sides, same in every writing mode.
	if node.TagName == "body" {
		style.Set("margin-top", "8px")
		style.Set("margin-right", "8px")
		style.Set("margin-bottom", "8px")
		style.Set("margin-left", "8px")
	}

	// Default margin for block elements: use logical markers so that
	// resolveLogicalBoxProperties maps to the correct physical side in
	// vertical/sideways writing modes (HTML spec §15.3.3).
	if node.TagName == "p" || node.TagName == "dl" ||
		node.TagName == "hr" {
		style.Set("margin-top", "1em")
		style.Set("margin-bottom", "1em")
		style.Set("_margin-block-start", "1em")
		style.Set("_margin-block-end", "1em")
	}

	// Headings: logical block margins per HTML spec.
	switch node.TagName {
	case "h1":
		style.Set("margin-top", "0.67em")
		style.Set("margin-bottom", "0.67em")
		style.Set("_margin-block-start", "0.67em")
		style.Set("_margin-block-end", "0.67em")
	case "h2":
		style.Set("margin-top", "0.83em")
		style.Set("margin-bottom", "0.83em")
		style.Set("_margin-block-start", "0.83em")
		style.Set("_margin-block-end", "0.83em")
	case "h3":
		style.Set("margin-top", "1em")
		style.Set("margin-bottom", "1em")
		style.Set("_margin-block-start", "1em")
		style.Set("_margin-block-end", "1em")
	case "h4":
		style.Set("margin-top", "1.33em")
		style.Set("margin-bottom", "1.33em")
		style.Set("_margin-block-start", "1.33em")
		style.Set("_margin-block-end", "1.33em")
	case "h5":
		style.Set("margin-top", "1.67em")
		style.Set("margin-bottom", "1.67em")
		style.Set("_margin-block-start", "1.67em")
		style.Set("_margin-block-end", "1.67em")
	case "h6":
		style.Set("margin-top", "2.33em")
		style.Set("margin-bottom", "2.33em")
		style.Set("_margin-block-start", "2.33em")
		style.Set("_margin-block-end", "2.33em")
	}

	// Lists and blockquotes: logical block margins.
	if node.TagName == "ul" || node.TagName == "ol" || node.TagName == "menu" ||
		node.TagName == "dir" || node.TagName == "blockquote" || node.TagName == "figure" {
		style.Set("margin-top", "1em")
		style.Set("margin-bottom", "1em")
		style.Set("_margin-block-start", "1em")
		style.Set("_margin-block-end", "1em")
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

	// HTML5 semantic elements default to display: block
	switch node.TagName {
	case "main", "nav", "header", "footer", "section", "article", "aside",
		"figure", "figcaption", "details", "summary", "hgroup":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "block")
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

	// Default white-space: pre for preformatted elements
	switch node.TagName {
	case "pre", "xmp", "listing":
		style.Set("white-space", "pre")
	}

	// Default inline display for inline HTML elements
	switch node.TagName {
	case "span", "em", "strong", "b", "i", "u", "s", "a", "abbr", "cite",
		"code", "dfn", "kbd", "mark", "q", "samp", "small", "sub", "sup",
		"var", "time", "label", "br", "wbr", "img", "object":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline")
		}
	}

	// Replaced inline elements: behave as inline-block for layout purposes
	// (they are replaced elements that honor width/height attributes)
	switch node.TagName {
	case "canvas", "video", "embed", "iframe":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-block")
		}
	}

	// Ruby UA stylesheet. Mirrors Blink html.css:1701-1720 exactly
	// (vetted at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). In
	// modern Blink only `ruby` and `rt` have ruby-specific UA display
	// values; `rb`, `rbc`, and `rtc` receive NO display override and
	// remain plain inline boxes (Blink's EDisplay has no kRubyBase /
	// kRubyBaseContainer / kRubyTextContainer). `rp` is hidden by the
	// separate flat element-only rule at html.css:972-975
	// (base, basefont, datalist, head, link, meta, noembed, noframes,
	// param, rp, script, style, template, title { display: none }),
	// not by a parent-scoped `ruby > rp` selector — so rp is hidden
	// regardless of parent.
	//
	// The rt UA rule has two layers in Blink: the unconditional `rt {
	// line-height: normal; text-emphasis: none }` applies always, while
	// `ruby > rt { display: ruby-text; font-size: 50%; text-align: start }`
	// only applies when rt's parent is a ruby box.
	switch node.TagName {
	case "ruby":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "ruby")
		}
		// html.css:1701-1704 — `ruby, rt { text-indent: 0 }`.
		if _, ok := style.Get("text-indent"); !ok {
			style.Set("text-indent", "0")
		}
	case "rt":
		// Unconditional rt rules.
		if _, ok := style.Get("line-height"); !ok {
			style.Set("line-height", "normal")
		}
		if _, ok := style.Get("text-indent"); !ok {
			style.Set("text-indent", "0")
		}
		// `ruby > rt` rules — only when parent is a ruby element.
		if node.Parent != nil && node.Parent.TagName == "ruby" {
			if _, ok := style.Get("display"); !ok {
				style.Set("display", "ruby-text")
			}
			if _, ok := style.Get("font-size"); !ok {
				// Blink uses 50%, not 0.5em — ruby-rt-fontsize-001
				// expects exactly half the parent's font-size after
				// inheritance, which 50% guarantees and 0.5em does
				// not (`em` resolves against the inherited size
				// before this UA rule, but `%` does the same and is
				// the spec-exact value Blink ships).
				style.Set("font-size", "50%")
			}
			if _, ok := style.Get("text-align"); !ok {
				style.Set("text-align", "start")
			}
		}
	case "rp":
		// Hidden unconditionally per Blink html.css:972-975 (the flat
		// element-only display:none rule). Not parent-scoped.
		style.Set("display", "none")
	}

	// Default styles for form elements — rendered as simple boxes.
	// Note: must use individual properties (not shorthands like "border" or "padding")
	// because style.Set() does not expand shorthands.
	switch node.TagName {
	case "input":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-block")
		}
		inputType, _ := node.GetAttribute("type")
		if inputType == "" {
			inputType = "text"
		}
		switch inputType {
		case "checkbox", "radio":
			if _, ok := style.Get("width"); !ok {
				style.Set("width", "13px")
			}
			if _, ok := style.Get("height"); !ok {
				style.Set("height", "13px")
			}
			setFormBorder(style, "1px", "solid", "#767676")
			if _, ok := style.Get("background-color"); !ok {
				style.Set("background-color", "white")
			}
		default:
			// text, password, email, number, search, etc.
			// UA uses border-box sizing (matches real browser UA stylesheets)
			style.Set("box-sizing", "border-box")
			if _, ok := style.Get("width"); !ok {
				style.Set("width", "173px")
			}
			if _, ok := style.Get("height"); !ok {
				style.Set("height", "19px")
			}
			setFormPadding(style, "1px", "2px", "1px", "2px")
			setFormBorder(style, "2px", "solid", "#767676")
			if _, ok := style.Get("background-color"); !ok {
				style.Set("background-color", "white")
			}
			if _, ok := style.Get("font-size"); !ok {
				style.Set("font-size", "13.3333px")
			}
			style.Set("overflow", "hidden")
		}
	case "textarea":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-block")
		}
		// Blink/Firefox/Safari: textarea defaults to content-box (border-box is
		// gated behind the experimental AppearanceBase feature). Default width
		// 173px and height 54px are border-box-ish approximations of 20 cols × 2 rows
		// but we express them as content-box to match the standard box model.
		if _, ok := style.Get("width"); !ok {
			style.Set("width", "167px")
		}
		if _, ok := style.Get("height"); !ok {
			style.Set("height", "48px")
		}
		setFormPadding(style, "2px", "2px", "2px", "2px")
		setFormBorder(style, "1px", "solid", "#767676")
		if _, ok := style.Get("background-color"); !ok {
			style.Set("background-color", "white")
		}
		if _, ok := style.Get("font-size"); !ok {
			style.Set("font-size", "13.3333px")
		}
		style.Set("font-family", "monospace")
		style.Set("overflow", "hidden")
	case "select":
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-block")
		}
		// UA uses border-box sizing (matches real browser UA stylesheets)
		style.Set("box-sizing", "border-box")
		if _, ok := style.Get("width"); !ok {
			style.Set("width", "173px")
		}
		if _, ok := style.Get("height"); !ok {
			style.Set("height", "19px")
		}
		setFormPadding(style, "1px", "2px", "1px", "2px")
		setFormBorder(style, "1px", "solid", "#767676")
		if _, ok := style.Get("background-color"); !ok {
			style.Set("background-color", "white")
		}
		if _, ok := style.Get("font-size"); !ok {
			style.Set("font-size", "13.3333px")
		}
		style.Set("overflow", "hidden")
	case "button":
		// Blink's <button> is an inline flex container with cross-axis centering
		// (align-items:center) and start-aligned main-axis content
		// (justify-content defaults to flex-start). Horizontal centering of text
		// content is handled separately by text-align:center below.
		// Mirrors third_party/blink/renderer/core/html/forms/html_button_element.cc
		// and the UA defaults in third_party/blink/renderer/core/html/resources/html.css.
		if _, ok := style.Get("display"); !ok {
			style.Set("display", "inline-flex")
		}
		if _, ok := style.Get("align-items"); !ok {
			style.Set("align-items", "center")
		}
		style.Set("box-sizing", "border-box")
		setFormPadding(style, "1px", "6px", "1px", "6px")
		setFormBorder(style, "2px", "solid", "#767676")
		if _, ok := style.Get("background-color"); !ok {
			style.Set("background-color", "#efefef")
		}
		if _, ok := style.Get("font-size"); !ok {
			style.Set("font-size", "13.3333px")
		}
		if _, ok := style.Get("text-align"); !ok {
			style.Set("text-align", "center")
		}
	}

	// Phase 23: Default styles for table elements
	switch node.TagName {
	case "table":
		style.Set("display", "table")
		style.Set("border-collapse", "separate")
		style.Set("border-spacing", "2px")
		// CSS Tables: table 'width' effectively specifies border-box width.
		// Matches browser behavior where table width includes padding/border.
		style.Set("box-sizing", "border-box")
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
		style.Set("text-align", "left")
		style.Set("vertical-align", "middle")
	case "th":
		style.Set("display", "table-cell")
		style.Set("padding", "1px")
		style.Set("font-weight", "bold")
		style.Set("vertical-align", "middle")
		style.Set("text-align", "center")
	case "caption":
		style.Set("display", "table-caption")
		style.Set("text-align", "center")

	// Phase 23: Default styles for list elements
	// Per HTML spec default stylesheet, list-style-type is set on ul/ol (not li),
	// so author "list-style: none" on ul/ol overrides it and li inherits "none".
	case "ul":
		style.Set("display", "block")
		style.Set("margin-top", "16px")
		style.Set("margin-bottom", "16px")
		expandShorthand(style, "padding-inline-start", "40px")
		style.Set("list-style-type", "disc")
	case "ol":
		style.Set("display", "block")
		style.Set("margin-top", "16px")
		style.Set("margin-bottom", "16px")
		expandShorthand(style, "padding-inline-start", "40px")
		style.Set("list-style-type", "decimal")
	case "li":
		style.Set("display", "list-item")
	}
}

// buildLayerOrder builds a combined layer order from all stylesheets.
// Earlier layers in the order have lower cascade priority (they lose to later layers).
// The order comes from @layer declaration statements and @layer block rules.
func buildLayerOrder(stylesheets []*Stylesheet) []string {
	seen := make(map[string]bool)
	var order []string
	for _, ss := range stylesheets {
		for _, name := range ss.LayerOrder {
			if !seen[name] {
				seen[name] = true
				order = append(order, name)
			}
		}
	}
	return order
}

// layerPriority returns the cascade priority of a rule based on its layer name.
// Rules in earlier layers get lower priority values (they lose to later layers).
// Unlayered rules (layerName == "") get priority = len(layerOrder) (highest priority).
// Anonymous layer rules ("") treated as unlayered for simplicity.
func layerPriority(layerName string, layerOrder []string) int {
	if layerName == "" {
		// Unlayered rules win over any layered rule
		return len(layerOrder)
	}
	for i, name := range layerOrder {
		if name == layerName {
			return i
		}
	}
	// Unknown layer name: treat as last declared layer (just before unlayered)
	return len(layerOrder) - 1
}

// ComputeStyle computes the final style for a node by applying the cascade
// Phase 22: Added viewport dimensions for media query evaluation
func ComputeStyle(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) *Style {
	finalStyle := NewStyle()

	// Phase 17: Apply user agent (default browser) styles first
	applyUserAgentStyles(node, finalStyle)

	// Map HTML presentational attributes to CSS (lower priority than stylesheets)
	applyPresentationalAttributes(node, finalStyle)

	// Collect all matching rules from all stylesheets
	allRules := make([]Rule, 0)

	for _, stylesheet := range stylesheets {
		matches := FindMatchingRules(node, stylesheet, viewportWidth, viewportHeight)
		allRules = append(allRules, matches...)
	}

	// Sort rules by cascade order: layer priority (lowest first), then specificity
	layerOrder := buildLayerOrder(stylesheets)
	sort.SliceStable(allRules, func(i, j int) bool {
		pi := layerPriority(allRules[i].LayerName, layerOrder)
		pj := layerPriority(allRules[j].LayerName, layerOrder)
		if pi != pj {
			return pi < pj
		}
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
			expandShorthand(finalStyle, property, value)
		}
	}

	// Apply !important declarations (second pass)
	for _, rule := range allRules {
		if rule.Important == nil {
			continue
		}
		for property, value := range rule.Declarations {
			if rule.Important[property] {
				expandShorthand(finalStyle, property, value)
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

	// Store viewport dimensions for viewport unit resolution (vw, vh, vmin, vmax)
	finalStyle.ViewportWidth = viewportWidth
	finalStyle.ViewportHeight = viewportHeight

	return finalStyle
}

// ApplyStylesToDocument applies stylesheets to all nodes in the document
// Phase 22: Added viewport dimensions for media query evaluation
func ApplyStylesToDocument(doc *html.Document, viewportWidth, viewportHeight float64) map[*html.Node]*Style {
	styles := make(map[*html.Node]*Style)

	// Parse all stylesheets
	stylesheets := ParseDocumentStylesheets(doc)

	// Recursively apply styles to all nodes
	applyStylesToNode(doc.Root, stylesheets, styles, viewportWidth, viewportHeight)

	return styles
}

// ParseDocumentStylesheets parses all stylesheets from a document.
// Exported so the layout tree builder can use them for pseudo-element generation.
func ParseDocumentStylesheets(doc *html.Document) []*Stylesheet {
	stylesheets := make([]*Stylesheet, 0)
	for _, cssText := range doc.Stylesheets {
		stylesheet, err := ParseStylesheet(cssText)
		if err == nil {
			stylesheets = append(stylesheets, stylesheet)
		}
	}
	return stylesheets
}

// Phase 11: ComputePseudoElementStyle computes the style for a pseudo-element
// Phase 22: Added viewport dimensions for media query evaluation
func ComputePseudoElementStyle(node *html.Node, pseudoElement string, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64, parentStyles ...*Style) *Style {
	finalStyle := NewStyle()

	// Inherit inheritable properties from parent element
	if len(parentStyles) > 0 && parentStyles[0] != nil {
		parent := parentStyles[0]
		inheritableProps := []string{"font-size", "font-family", "font-weight", "font-style",
			"color", "line-height", "text-align", "white-space", "visibility",
			"letter-spacing", "word-spacing", "text-indent", "text-transform"}
		for _, prop := range inheritableProps {
			if val, ok := parent.Get(prop); ok {
				finalStyle.Set(prop, val)
			}
		}
		// CSS Custom Properties for Cascading Variables §3: Custom properties
		// inherit by default. Pseudo-elements inherit custom properties from
		// their originating element so that var() references in the pseudo-
		// element's declarations resolve correctly.
		for prop, val := range parent.Properties {
			if strings.HasPrefix(prop, "--") {
				if _, ok := finalStyle.Get(prop); !ok {
					finalStyle.Set(prop, val)
				}
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

	// Sort rules by cascade order: layer priority (lowest first), then specificity
	layerOrder2 := buildLayerOrder(stylesheets)
	sort.SliceStable(allRules, func(i, j int) bool {
		pi := layerPriority(allRules[i].LayerName, layerOrder2)
		pj := layerPriority(allRules[j].LayerName, layerOrder2)
		if pi != pj {
			return pi < pj
		}
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

	// CSS 2.1 §12.1: Pseudo-elements default to display:inline unless
	// explicitly set by a rule. GetDisplay() defaults to DisplayBlock for
	// regular elements, but pseudo-elements are inline by default.
	if _, ok := finalStyle.Get("display"); !ok {
		finalStyle.Set("display", "inline")
	}

	// Store viewport dimensions for viewport unit resolution
	finalStyle.ViewportWidth = viewportWidth
	finalStyle.ViewportHeight = viewportHeight

	return finalStyle
}

// HasFirstLetterRules returns true if any stylesheet rules with ::first-letter
// pseudo-element match the given node.
func HasFirstLetterRules(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) bool {
	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			if rule.Selector.PseudoElement != "first-letter" {
				continue
			}
			if !EvaluateMediaQuery(rule.MediaQuery, viewportWidth, viewportHeight) {
				continue
			}
			if MatchesSelector(node, rule.Selector) {
				return true
			}
		}
	}
	return false
}

// HasFirstLineRules returns true if any stylesheet rules with ::first-line
// pseudo-element match the given node, indicating first-line styling is needed.
func HasFirstLineRules(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) bool {
	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			if rule.Selector.PseudoElement != "first-line" {
				continue
			}
			if !EvaluateMediaQuery(rule.MediaQuery, viewportWidth, viewportHeight) {
				continue
			}
			if MatchesSelector(node, rule.Selector) {
				return true
			}
		}
	}
	return false
}

// HasPseudoElementRules returns true if any stylesheet rules with the given
// pseudo-element match the given node.
func HasPseudoElementRules(node *html.Node, pseudoElement string, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) bool {
	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			pseudo := rule.Selector.PseudoElement
			if pseudo != pseudoElement {
				// Also check for "descendant:" prefixed pseudo-elements.
				if !strings.HasPrefix(pseudo, "descendant:") || strings.TrimPrefix(pseudo, "descendant:") != pseudoElement {
					continue
				}
			}
			if !EvaluateMediaQuery(rule.MediaQuery, viewportWidth, viewportHeight) {
				continue
			}
			if pseudo == pseudoElement {
				if MatchesSelector(node, rule.Selector) {
					return true
				}
			} else {
				// Descendant pseudo-element: check if node is a descendant.
				ancestor := node.Parent
				for ancestor != nil {
					if MatchesSelector(ancestor, rule.Selector) {
						return true
					}
					ancestor = ancestor.Parent
				}
			}
		}
	}
	return false
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
	"font-feature-settings": true,
	"line-height":           true, "text-align": true, "text-decoration": true,
	"text-transform": true, "text-indent": true, "white-space": true,
	"visibility": true, "list-style-type": true, "list-style-position": true, "list-style-image": true,
	"direction": true, "letter-spacing": true, "word-spacing": true,
	"cursor": true, "writing-mode": true, "text-orientation": true,
	"empty-cells": true,
	// CSS Text 3 inherited properties:
	"word-break": true, "overflow-wrap": true, "hyphens": true,
	"line-break": true, "tab-size": true,
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

	// Resolve font-size em and percentage values using parent's font-size.
	// Parent has already been processed (top-down cascade), so its font-size
	// is resolved to an absolute px value. This propagates through to
	// children so 1em and 100% both resolve correctly per CSS 2.1 §15.7.
	if fsVal, hasFontSize := style.Get("font-size"); hasFontSize {
		trimmed := strings.TrimSpace(fsVal)
		parentFS := 16.0
		if parentStyle != nil {
			parentFS = parentStyle.GetFontSize()
		}
		if strings.HasSuffix(trimmed, "%") {
			if pct, ok := ParsePercentage(trimmed); ok {
				style.Set("font-size", fmt.Sprintf("%.6gpx", pct/100.0*parentFS))
			}
		} else if strings.HasSuffix(trimmed, "em") && !strings.HasSuffix(trimmed, "rem") {
			if resolved, ok := ParseLengthWithFontSize(fsVal, parentFS); ok {
				style.Set("font-size", fmt.Sprintf("%.6gpx", resolved))
			}
		}
	}

	for prop := range inheritableProperties {
		if _, hasOwn := style.Get(prop); !hasOwn {
			if parentVal, ok := parentStyle.Get(prop); ok {
				style.Set(prop, parentVal)
				// Track that writing-mode was inherited, not explicitly set.
				// This allows resolveLogicalSizeProperties to skip the
				// vertical-mode resolution for inherited values, since the
				// layout engine's transformToVerticalRL handles positioning
				// as a post-pass and expects children to use horizontal dimensions.
				if prop == "writing-mode" {
					style.Set("_writing-mode-inherited", "true")
				}
			}
		}
	}

	// CSS Custom Properties (--*) inherit by default (CSS Custom Properties §2.2)
	for prop, val := range parentStyle.Properties {
		if strings.HasPrefix(prop, "--") {
			if _, hasOwn := style.Properties[prop]; !hasOwn {
				style.Properties[prop] = val
			}
		}
	}
}

// ApplyInheritedFrom copies inheritable CSS properties from a parent style to
// a child style when the child hasn't explicitly set them. This is used during
// intrinsic sizing when css.ComputeStyle is called standalone (without the full
// document style tree) and inherited properties like font-family are missing.
func ApplyInheritedFrom(child, parent *Style) {
	if child == nil || parent == nil {
		return
	}
	for prop := range inheritableProperties {
		if _, hasOwn := child.Get(prop); !hasOwn {
			if parentVal, ok := parent.Get(prop); ok {
				child.Set(prop, parentVal)
			}
		}
	}
}

// NewBlockifiedStyle creates a minimal anonymous block wrapper style for
// block-in-inline splitting. When all inline continuations are suppressed
// (whitespace-only), the extracted block children are wrapped in this style
// so that non-inheritable stacking context properties (opacity, transform,
// filter) from the inline are preserved. Backgrounds, borders, padding, and
// margins are NOT copied — they belong only to the inline's own box edges.
//
// position:relative/sticky + top/right/bottom/left are also preserved so the
// blocks inside a positioned inline shift with the inline (CSS 2.1 §9.4.3).
func NewBlockifiedStyle(inline *Style) *Style {
	s := NewAnonymousBlockStyle(inline)
	// Preserve stacking-context-creating non-inheritable properties.
	for _, prop := range []string{"opacity", "transform", "filter", "isolation", "will-change", "position", "top", "right", "bottom", "left"} {
		if val, ok := inline.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	return s
}

// NewAnonymousBlockStyle creates a style for an anonymous block box
// (CSS 2.1 §9.2.1.1). Anonymous boxes inherit all inheritable properties
// from the parent and have display:block with zero margin/border/padding.
func NewAnonymousBlockStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.Set("display", "block")
	// Copy all inheritable properties from the parent.
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	return s
}

// NewAnonymousTableCellStyle creates a style for an anonymous table-cell box
// that inherits from the parent row style. Per CSS Tables §2.1, when a
// non-table-cell child appears inside a table-row, it must be wrapped in
// an anonymous table-cell box.
func NewAnonymousTableCellStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.Set("display", "table-cell")
	// Copy all inheritable properties from the parent.
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	return s
}

// NewAnonymousTableRowStyle creates a style for an anonymous table-row box.
// Per CSS 2.1 §17.2.1, non-row children of a table-row-group (and bare
// non-row siblings of rows in a table) are wrapped in anonymous table-row
// boxes. Mirrors Blink's LayoutTableRow::CreateAnonymousWithParent.
func NewAnonymousTableRowStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.Set("display", "table-row")
	// Copy all inheritable properties from the parent.
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	return s
}

// NewAnonymousInlineRubyStyle creates a style for the single anonymous
// inline `display: ruby` box that wraps the children of a block-level
// `display: block ruby` element (CSS Display L3 + Blink's
// LayoutRubyAsBlock two-box model). The principal box is the
// block-flow generated from the block-ruby element itself; this style
// is for the inline-ruby child that actually carries the ruby column
// items.
//
// Per Blink layout_ruby_as_block.cc, the inline ruby child inherits
// all inheritable properties from the principal box; non-inheritable
// box properties (margin/border/padding) live on the principal block
// box, not on the inline ruby. Mirrors NewAnonymousBlockStyle but with
// display:ruby.
func NewAnonymousInlineRubyStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.Set("display", "ruby")
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	return s
}

// NewAnonymousTableRowGroupStyle creates a style for an anonymous
// table-row-group box. Per CSS 2.1 §17.2.1, a run of non-proper-table
// children of a table/inline-table is wrapped in an anonymous
// table-row-group. Mirrors Blink's LayoutTableSection::CreateAnonymousWithParent.
func NewAnonymousTableRowGroupStyle(parent *Style) *Style {
	s := NewStyle()
	s.ViewportWidth = parent.ViewportWidth
	s.ViewportHeight = parent.ViewportHeight
	s.ChWidth = parent.ChWidth
	s.Set("display", "table-row-group")
	// Copy all inheritable properties from the parent.
	for prop := range inheritableProperties {
		if val, ok := parent.Get(prop); ok {
			s.Set(prop, val)
		}
	}
	return s
}

// resolveOrthogonalDisplay implements CSS Writing Modes §2.1:
// If an inline-level box has a different writing-mode axis than its containing
// block, its display value computes to inline-block. This causes inline spans
// with orthogonal writing-mode to honor width/height properties.
func resolveOrthogonalDisplay(node *html.Node, style *Style, styles map[*html.Node]*Style) {
	if node == nil || node.Parent == nil {
		return
	}
	// Only apply to inline-level elements
	display, hasDisplay := style.Get("display")
	if !hasDisplay || display != "inline" {
		return
	}
	// Get this element's writing-mode axis
	wm, _ := style.Get("writing-mode")
	isVertical := wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr"
	// Get parent's writing-mode axis
	parentStyle, ok := styles[node.Parent]
	if !ok || parentStyle == nil {
		// No parent style: assume horizontal-tb (the default)
		// If this element is vertical, it's orthogonal → promote to inline-block
		if isVertical {
			style.Set("display", "inline-block")
		}
		return
	}
	parentWM, _ := parentStyle.Get("writing-mode")
	parentIsVertical := parentWM == "vertical-rl" || parentWM == "vertical-lr" || parentWM == "sideways-rl" || parentWM == "sideways-lr"
	// If the axes differ, promote inline to inline-block
	if isVertical != parentIsVertical {
		style.Set("display", "inline-block")
	}
}

// resolveLogicalSizeProperties remaps logical size properties (inline-size,
// block-size, and their min/max variants) to the correct physical properties
// based on the element's computed writing-mode.
//
// During shorthand expansion, inline-size is mapped to width (the default for
// horizontal-tb). For vertical writing modes (vertical-rl, vertical-lr,
// sideways-rl, sideways-lr), the inline axis is vertical, so inline-size must
// map to height instead. This function fixes the mapping after the writing-mode
// has been fully resolved (including inheritance).
func resolveLogicalSizeProperties(style *Style) {
	wm, _ := style.Get("writing-mode")
	isVertical := wm == "vertical-rl" || wm == "vertical-lr" ||
		wm == "sideways-rl" || wm == "sideways-lr"
	if !isVertical {
		return // horizontal-tb: default mapping is correct
	}
	// Remap for both explicitly-set and inherited writing-mode — logical
	// axes are always computed from the element's used writing-mode.

	// For vertical writing modes:
	//   inline-size -> height (not width)
	//   block-size  -> width  (not height)
	// The expandShorthand stored the logical values under _inline-size / _block-size
	// markers and mapped them to width/height assuming horizontal-tb. We now need
	// to swap them.

	inlineVal, hasInline := style.Get("_inline-size")
	blockVal, hasBlock := style.Get("_block-size")

	if hasInline && hasBlock {
		// Both set: swap width <-> height
		style.Set("height", inlineVal)
		style.Set("width", blockVal)
	} else if hasInline {
		// Only inline-size was set: move from width to height.
		// Clear width if it was set only by inline-size (check if there's
		// an independent width declaration by comparing values).
		style.Set("height", inlineVal)
		if curW, ok := style.Get("width"); ok && curW == inlineVal {
			delete(style.Properties, "width")
		}
	} else if hasBlock {
		// Only block-size was set: move from height to width.
		style.Set("width", blockVal)
		if curH, ok := style.Get("height"); ok && curH == blockVal {
			delete(style.Properties, "height")
		}
	}

	// Same for min-inline-size / min-block-size
	minInlineVal, hasMinInline := style.Get("_min-inline-size")
	minBlockVal, hasMinBlock := style.Get("_min-block-size")

	if hasMinInline && hasMinBlock {
		style.Set("min-height", minInlineVal)
		style.Set("min-width", minBlockVal)
	} else if hasMinInline {
		style.Set("min-height", minInlineVal)
		if curMW, ok := style.Get("min-width"); ok && curMW == minInlineVal {
			delete(style.Properties, "min-width")
		}
	} else if hasMinBlock {
		style.Set("min-width", minBlockVal)
		if curMH, ok := style.Get("min-height"); ok && curMH == minBlockVal {
			delete(style.Properties, "min-height")
		}
	}

	// Same for max-inline-size / max-block-size
	maxInlineVal, hasMaxInline := style.Get("_max-inline-size")
	maxBlockVal, hasMaxBlock := style.Get("_max-block-size")

	if hasMaxInline && hasMaxBlock {
		style.Set("max-height", maxInlineVal)
		style.Set("max-width", maxBlockVal)
	} else if hasMaxInline {
		style.Set("max-height", maxInlineVal)
		if curMW, ok := style.Get("max-width"); ok && curMW == maxInlineVal {
			delete(style.Properties, "max-width")
		}
	} else if hasMaxBlock {
		style.Set("max-width", maxBlockVal)
		if curMH, ok := style.Get("max-height"); ok && curMH == maxBlockVal {
			delete(style.Properties, "max-height")
		}
	}
}

// resolveLogicalBoxProperties resolves logical margin/padding/border properties
// (e.g. border-inline-start, margin-block-end) to physical properties based on
// the element's computed writing-mode. Must be called after inheritance.
//
// Mapping for horizontal-tb (default):
//
//	inline-start=left, inline-end=right, block-start=top, block-end=bottom
//
// For vertical-rl / vertical-lr:
//
//	inline-start=top, inline-end=bottom, block-start=right(rl)/left(lr), block-end=left(rl)/right(lr)
func resolveLogicalBoxProperties(style *Style) {
	wm, _ := style.Get("writing-mode")
	isVertical := wm == "vertical-rl" || wm == "vertical-lr" ||
		wm == "sideways-rl" || wm == "sideways-lr"
	// CSS Logical Properties Level 1: logical properties resolve based on
	// the element's computed writing-mode, regardless of whether writing-mode
	// was inherited or explicitly set. Previously we forced HTB mapping for
	// inherited writing-mode, but that incorrectly maps UA logical properties
	// (e.g. padding-inline-start on <ul>) to the wrong physical side in
	// vertical writing modes.

	dir, _ := style.Get("direction")
	isRTL := dir == "rtl"

	// Check if any logical markers exist; if not, nothing to resolve
	hasMarkers := false
	for key := range style.Properties {
		if len(key) > 0 && key[0] == '_' {
			hasMarkers = true
			break
		}
	}
	if !hasMarkers {
		return
	}

	// Determine the correct physical mapping for each logical direction.
	// CSS Logical Properties: https://www.w3.org/TR/css-logical-1/#box
	//
	// horizontal-tb + LTR: inline-start=left, inline-end=right, block-start=top, block-end=bottom
	// horizontal-tb + RTL: inline-start=right, inline-end=left, block-start=top, block-end=bottom
	// vertical-rl + LTR: inline-start=top, inline-end=bottom, block-start=right, block-end=left
	// vertical-rl + RTL: inline-start=bottom, inline-end=top, block-start=right, block-end=left
	// vertical-lr + LTR: inline-start=top, inline-end=bottom, block-start=left, block-end=right
	// vertical-lr + RTL: inline-start=bottom, inline-end=top, block-start=left, block-end=right
	// sideways-lr + LTR: inline-start=bottom, inline-end=top, block-start=left, block-end=right
	// sideways-lr + RTL: inline-start=top, inline-end=bottom, block-start=left, block-end=right

	var inlineStartSide, inlineEndSide, blockStartSide, blockEndSide string

	switch {
	case wm == "sideways-lr" && !isRTL:
		inlineStartSide = "bottom"
		inlineEndSide = "top"
		blockStartSide = "left"
		blockEndSide = "right"
	case wm == "sideways-lr" && isRTL:
		inlineStartSide = "top"
		inlineEndSide = "bottom"
		blockStartSide = "left"
		blockEndSide = "right"
	case isVertical && !isRTL:
		inlineStartSide = "top"
		inlineEndSide = "bottom"
		if wm == "vertical-rl" || wm == "sideways-rl" {
			blockStartSide = "right"
			blockEndSide = "left"
		} else {
			blockStartSide = "left"
			blockEndSide = "right"
		}
	case isVertical && isRTL:
		inlineStartSide = "bottom"
		inlineEndSide = "top"
		if wm == "vertical-rl" || wm == "sideways-rl" {
			blockStartSide = "right"
			blockEndSide = "left"
		} else {
			blockStartSide = "left"
			blockEndSide = "right"
		}
	case !isVertical && isRTL:
		// horizontal-tb + RTL
		inlineStartSide = "right"
		inlineEndSide = "left"
		blockStartSide = "top"
		blockEndSide = "bottom"
	default:
		// horizontal-tb + LTR (default)
		inlineStartSide = "left"
		inlineEndSide = "right"
		blockStartSide = "top"
		blockEndSide = "bottom"
	}

	// The expandShorthand always maps to horizontal-tb LTR physical properties:
	//   inline-start -> left, inline-end -> right
	//   block-start -> top, block-end -> bottom
	type logicalMapping struct {
		markerPrefix string // e.g., "_margin-inline-start"
		wrongSide    string // what expandShorthand used (e.g., "left")
		correctSide  string // what it should be (e.g., "top")
	}

	mappings := []logicalMapping{
		{"_margin-inline-start", "left", inlineStartSide},
		{"_margin-inline-end", "right", inlineEndSide},
		{"_margin-block-start", "top", blockStartSide},
		{"_margin-block-end", "bottom", blockEndSide},
		{"_padding-inline-start", "left", inlineStartSide},
		{"_padding-inline-end", "right", inlineEndSide},
		{"_padding-block-start", "top", blockStartSide},
		{"_padding-block-end", "bottom", blockEndSide},
	}

	for _, m := range mappings {
		if val, ok := style.Get(m.markerPrefix); ok {
			propType := strings.TrimPrefix(m.markerPrefix, "_")
			propType = propType[:strings.Index(propType, "-")]
			wrongProp := propType + "-" + m.wrongSide
			correctProp := propType + "-" + m.correctSide

			// Only set the resolved physical property if it wasn't explicitly
			// set by a higher-cascade-priority source (e.g., author stylesheet
			// setting padding-top should not be overridden by UA's logical
			// padding-inline-start resolving to the same physical side).
			if _, alreadySet := style.Get(correctProp); !alreadySet {
				style.Set(correctProp, val)
			}
			if m.wrongSide != m.correctSide {
				if curVal, curOk := style.Get(wrongProp); curOk && curVal == val {
					delete(style.Properties, wrongProp)
				}
			}
			delete(style.Properties, m.markerPrefix)
		}
	}

	// Border properties: same pattern but with -width/-style/-color suffixes
	borderMappings := []logicalMapping{
		{"_border-inline-start", "left", inlineStartSide},
		{"_border-inline-end", "right", inlineEndSide},
		{"_border-block-start", "top", blockStartSide},
		{"_border-block-end", "bottom", blockEndSide},
	}

	for _, m := range borderMappings {
		for _, suffix := range []string{"-width", "-style", "-color"} {
			marker := m.markerPrefix + suffix
			if val, ok := style.Get(marker); ok {
				correctProp := "border-" + m.correctSide + suffix

				if _, alreadySet := style.Get(correctProp); !alreadySet {
					style.Set(correctProp, val)
				}
				delete(style.Properties, marker)
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
		// CSS Writing Modes §2.1: If an inline box has a different writing-mode
		// than its containing block, its display computes to inline-block.
		resolveOrthogonalDisplay(node, style, styles)
		// Resolve logical size properties (inline-size, block-size) based on the
		// element's computed writing-mode. Must happen after inheritance so that
		// inherited writing-mode is available.
		resolveLogicalSizeProperties(style)
		// Resolve logical box properties (margin/padding/border-inline/block)
		resolveLogicalBoxProperties(style)
		// Apply container query rules after base style (needs ancestor styles resolved)
		applyContainerQueryRules(node, stylesheets, styles, style)
		styles[node] = style
	}

	// When the HTML parser doesn't create implicit <html>/<body> elements (common for
	// fragment-style HTML and WPT test files), elements appear as direct children of
	// doc.Root. In this case, CSS inheritance from :root rules cannot propagate because
	// doc.Root has no style entry. Fix: synthesize a root style from the :root-matching
	// element (the first element child of doc.Root) and store it as styles[doc.Root].
	// This allows ApplyInheritedProperties to correctly propagate font-size, font-family,
	// etc. to all children of doc.Root, so em units resolve against the correct font-size.
	if node.TagName == "document" && styles[node] == nil {
		syntheticRootStyle := NewStyle()
		syntheticRootStyle.ViewportWidth = viewportWidth
		syntheticRootStyle.ViewportHeight = viewportHeight
		for _, child := range node.Children {
			if child.Type == html.ElementNode {
				// First element child matches :root — compute its style to capture
				// :root rules, then extract inheritable properties for doc.Root.
				rootChildStyle := ComputeStyle(child, stylesheets, viewportWidth, viewportHeight)
				for prop := range inheritableProperties {
					// CSS Writing Modes §7.1: writing-mode's initial value is horizontal-tb.
					// Don't propagate from :root to synthetic doc root — otherwise every
					// descendant inherits it and the parentIsVertical guard blocks transforms.
					if prop == "writing-mode" {
						continue
					}
					if val, ok := rootChildStyle.Get(prop); ok {
						syntheticRootStyle.Set(prop, val)
					}
				}
				// Also propagate CSS custom properties (--*) which inherit by default
				for prop, val := range rootChildStyle.Properties {
					if strings.HasPrefix(prop, "--") {
						syntheticRootStyle.Set(prop, val)
					}
				}
				break
			}
		}
		if len(syntheticRootStyle.Properties) > 0 {
			styles[node] = syntheticRootStyle
		}
	}

	// Always traverse children (parent is already computed, so top-down order is maintained)
	for _, child := range node.Children {
		applyStylesToNode(child, stylesheets, styles, viewportWidth, viewportHeight)
	}
}

// applyContainerQueryRules evaluates @container rules for a node by checking
// ancestor containers' computed sizes. Called after base style computation.
func applyContainerQueryRules(node *html.Node, stylesheets []*Stylesheet, styles map[*html.Node]*Style, style *Style) {
	for _, stylesheet := range stylesheets {
		for _, rule := range stylesheet.Rules {
			if rule.ContainerQuery == nil {
				continue
			}
			// Skip pseudo-element rules
			if rule.Selector.PseudoElement != "" {
				continue
			}
			// Check if selector matches this node
			if !MatchesSelector(node, rule.Selector) {
				continue
			}
			// Evaluate the container query against ancestors
			if !evaluateContainerQuery(node, rule.ContainerQuery, styles) {
				continue
			}
			// Apply declarations
			for property, value := range rule.Declarations {
				expandShorthand(style, property, value)
			}
		}
	}
}

// evaluateContainerQuery walks up the DOM to find a matching container and evaluates size conditions.
func evaluateContainerQuery(node *html.Node, cq *ContainerQuery, styles map[*html.Node]*Style) bool {
	// Walk up ancestors to find a container
	ancestor := node.Parent
	for ancestor != nil {
		ancestorStyle, ok := styles[ancestor]
		if !ok {
			ancestor = ancestor.Parent
			continue
		}

		containerType, _ := ancestorStyle.Get("container-type")
		if containerType != "inline-size" && containerType != "size" {
			ancestor = ancestor.Parent
			continue
		}

		// Check container name if specified
		if cq.Name != "" {
			containerName, _ := ancestorStyle.Get("container-name")
			if containerName != cq.Name {
				ancestor = ancestor.Parent
				continue
			}
		}

		// Found a matching container — evaluate conditions against its width
		widthStr, _ := ancestorStyle.Get("width")
		containerWidth, _ := ParseLength(widthStr)

		for _, cond := range cq.Conditions {
			condValue, _ := ParseLength(cond.Value)
			switch cond.Feature {
			case "min-width":
				if containerWidth < condValue {
					return false
				}
			case "max-width":
				if containerWidth > condValue {
					return false
				}
			case "width":
				if containerWidth != condValue {
					return false
				}
			}
		}
		return true
	}
	return false
}

// setFormBorder sets individual border properties for form element UA styles.
// style.Set() doesn't expand shorthands, so we must set each property individually.
func setFormBorder(style *Style, width, borderStyle, color string) {
	for _, side := range []string{"top", "right", "bottom", "left"} {
		if _, ok := style.Get("border-" + side + "-width"); !ok {
			style.Set("border-"+side+"-width", width)
		}
		if _, ok := style.Get("border-" + side + "-style"); !ok {
			style.Set("border-"+side+"-style", borderStyle)
		}
		if _, ok := style.Get("border-" + side + "-color"); !ok {
			style.Set("border-"+side+"-color", color)
		}
	}
}

// setFormPadding sets individual padding properties for form element UA styles.
func setFormPadding(style *Style, top, right, bottom, left string) {
	if _, ok := style.Get("padding-top"); !ok {
		style.Set("padding-top", top)
	}
	if _, ok := style.Get("padding-right"); !ok {
		style.Set("padding-right", right)
	}
	if _, ok := style.Get("padding-bottom"); !ok {
		style.Set("padding-bottom", bottom)
	}
	if _, ok := style.Get("padding-left"); !ok {
		style.Set("padding-left", left)
	}
}

// pxValue appends "px" to s, stripping any existing "px" suffix first so that
// attribute values like "100px" don't become "100pxpx".
func pxValue(s string) string {
	return strings.TrimSuffix(s, "px") + "px"
}

// applyPresentationalAttributes maps HTML presentational attributes to CSS properties.
// These have lower priority than author CSS — they're applied before stylesheet rules
// and inline styles, so CSS can override them.
func applyPresentationalAttributes(node *html.Node, style *Style) {
	if node.Type != html.ElementNode {
		return
	}

	// bgcolor → background-color (on table, tr, td, th, body)
	if val, ok := node.GetAttribute("bgcolor"); ok {
		style.Set("background-color", val)
	}

	// width attribute → CSS width (on table, td, th, img, etc.)
	// Per HTML spec (2022+), canvas and video width/height attributes set
	// intrinsic dimensions and aspect-ratio, NOT CSS width/height. This lets
	// CSS height override the attribute height and have the width transfer
	// through the aspect ratio correctly.
	if val, ok := node.GetAttribute("width"); ok {
		switch node.TagName {
		case "table", "td", "th", "col", "colgroup", "img", "input", "object", "embed", "hr":
			if strings.HasSuffix(val, "%") {
				style.Set("width", val)
			} else {
				// Numeric value = pixels
				style.Set("width", pxValue(val))
			}
		}
	}

	// height attribute → CSS height.
	// Per HTML spec (2024), canvas and video height attributes do NOT map to CSS
	// height directly — they define the element's intrinsic coordinate space height
	// and establish a CSS aspect-ratio when paired with the width attribute.
	// Excluding canvas/video from the height→CSS height mapping ensures that
	// CSS Containment (contain:size) can correctly use the intrinsic aspect ratio
	// to determine block-size from an author-set inline-size.
	if val, ok := node.GetAttribute("height"); ok {
		switch node.TagName {
		case "table", "td", "th", "tr", "img", "input", "object", "embed":
			if strings.HasSuffix(val, "%") {
				style.Set("height", val)
			} else {
				style.Set("height", pxValue(val))
			}
		}
	}

	// canvas and video: width+height attributes → CSS aspect-ratio (per HTML spec 2024).
	// When both numeric attributes are present, they establish the element's
	// CSS aspect-ratio as a presentational hint. The width attribute continues
	// to map to CSS width (above) for backward-compatibility and flex sizing.
	switch node.TagName {
	case "canvas", "video":
		wVal, wOk := node.GetAttribute("width")
		hVal, hOk := node.GetAttribute("height")
		if wOk && hOk && !strings.HasSuffix(wVal, "%") && !strings.HasSuffix(hVal, "%") {
			// Both numeric attributes present → set aspect-ratio presentational hint.
			style.Set("aspect-ratio", wVal+"/"+hVal)
		}
	}

	// align → text-align (on td, th, tr, div, p, etc.)
	if val, ok := node.GetAttribute("align"); ok {
		switch val {
		case "left", "center", "right", "justify":
			switch node.TagName {
			case "td", "th", "tr", "div", "p", "h1", "h2", "h3", "h4", "h5", "h6", "table":
				style.Set("text-align", val)
			}
		}
		// table/img align="center" → auto margins
		if val == "center" && (node.TagName == "table" || node.TagName == "img") {
			style.Set("margin-left", "auto")
			style.Set("margin-right", "auto")
		}
	}

	// valign → vertical-align (on td, th, tr)
	if val, ok := node.GetAttribute("valign"); ok {
		switch node.TagName {
		case "td", "th", "tr":
			style.Set("vertical-align", val)
		}
	}

	// border attribute on table → border-width on cells
	if val, ok := node.GetAttribute("border"); ok {
		if node.TagName == "table" {
			style.Set("border-width", pxValue(val))
			style.Set("border-style", "outset")
		}
	}

	// cellpadding on table → stored for table layout to apply to cells
	// cellspacing on table → border-spacing
	if val, ok := node.GetAttribute("cellspacing"); ok {
		if node.TagName == "table" {
			style.Set("border-spacing", pxValue(val))
		}
	}

	// <center> element → text-align: center + auto margins for block children
	if node.TagName == "center" {
		style.Set("text-align", "center")
	}

	// cellpadding on ancestor <table> → padding on td/th
	if node.TagName == "td" || node.TagName == "th" {
		// Walk up to find containing table
		for p := node.Parent; p != nil; p = p.Parent {
			if p.TagName == "table" {
				if cp, ok := p.GetAttribute("cellpadding"); ok {
					style.Set("padding", pxValue(cp))
				}
				break
			}
		}
	}

	// colspan attribute — stored for table layout (not a CSS property,
	// but we note it here so table layout can read it from the style)
	if val, ok := node.GetAttribute("colspan"); ok {
		if node.TagName == "td" || node.TagName == "th" {
			style.Set("-x-colspan", val)
		}
	}

	// color attribute → color (on font, hr, etc.)
	if val, ok := node.GetAttribute("color"); ok {
		style.Set("color", val)
	}

	// <font size="N"> → font-size
	if node.TagName == "font" {
		if val, ok := node.GetAttribute("size"); ok {
			if fontSize := fontSizeFromHTMLSize(val); fontSize != "" {
				style.Set("font-size", fontSize)
			}
		}
		if val, ok := node.GetAttribute("face"); ok {
			style.Set("font-family", val)
		}
	}

	// <img> border attribute
	if node.TagName == "img" {
		if val, ok := node.GetAttribute("border"); ok {
			style.Set("border-width", pxValue(val))
			style.Set("border-style", "solid")
		}
	}

	// dir attribute → direction + unicode-bidi (HTML presentational hint).
	// Per HTML spec §14.3.5, the dir attribute maps to:
	//   direction: ltr/rtl
	//   unicode-bidi: isolate (or isolate-override for <bdo>)
	// As a presentational hint, author CSS can override these values.
	if dirAttr, ok := node.GetAttribute("dir"); ok {
		dirAttr = strings.ToLower(strings.TrimSpace(dirAttr))
		switch dirAttr {
		case "rtl":
			style.Set("direction", "rtl")
		case "ltr":
			style.Set("direction", "ltr")
		case "auto":
			// TODO: implement first-strong heuristic (UAX#9 P2/P3)
			style.Set("direction", "ltr")
		}
		if dirAttr == "rtl" || dirAttr == "ltr" || dirAttr == "auto" {
			if node.TagName == "bdo" {
				style.Set("unicode-bidi", "isolate-override")
			} else {
				style.Set("unicode-bidi", "isolate")
			}
		}
	}
}

// fontSizeFromHTMLSize converts HTML <font size="N"> to CSS font-size.
func fontSizeFromHTMLSize(size string) string {
	switch size {
	case "1":
		return "x-small"
	case "2":
		return "small"
	case "3":
		return "medium"
	case "4":
		return "large"
	case "5":
		return "x-large"
	case "6":
		return "xx-large"
	case "7":
		return "xxx-large"
	}
	return ""
}
