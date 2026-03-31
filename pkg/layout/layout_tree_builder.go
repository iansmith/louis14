package layout

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LayoutTreeBuilder constructs the layout tree from a DOM tree and
// computed styles. It wraps each DOM node in a LayoutInputNode, pre-filters
// display:none children, and (in Phase 1) generates anonymous block boxes.
//
// Mirrors Blink's LayoutTreeBuilderForNG.
type LayoutTreeBuilder struct {
	styles          map[*html.Node]*css.Style
	stylesheets     []*css.Stylesheet
	viewportWidth   float64
	viewportHeight  float64
}

// BuildLayoutTree creates the layout tree rooted at the given DOM node.
func (b *LayoutTreeBuilder) BuildLayoutTree(root *html.Node) *LayoutInputNode {
	return b.buildNode(root)
}

// buildNode recursively wraps a DOM node and its children.
func (b *LayoutTreeBuilder) buildNode(node *html.Node) *LayoutInputNode {
	style := b.styles[node]

	lin := &LayoutInputNode{
		DOMNode: node,
		style:   style,
	}

	// Text nodes are leaf nodes — no children to process.
	if node.Type == html.TextNode {
		return lin
	}

	// Build layout children, filtering out display:none and non-layout nodes.
	var rawChildren []*LayoutInputNode

	// CSS 2.1 §12.1: Generate ::before pseudo-element.
	if beforeNode := b.createPseudoElement(node, style, "before"); beforeNode != nil {
		rawChildren = append(rawChildren, beforeNode)
	}

	for _, child := range node.Children {
		switch child.Type {
		case html.TextNode:
			// Text nodes always participate in layout (whitespace handled later).
			rawChildren = append(rawChildren, &LayoutInputNode{
				DOMNode: child,
				style:   style, // text inherits parent's style
			})

		case html.ElementNode:
			childStyle := b.styles[child]
			if childStyle == nil {
				continue
			}
			if childStyle.GetDisplay() == css.DisplayNone {
				continue
			}
			rawChildren = append(rawChildren, b.buildNode(child))

		default:
			// Skip comment nodes, doctype, etc.
			continue
		}
	}

	// CSS 2.1 §12.1: Generate ::after pseudo-element.
	if afterNode := b.createPseudoElement(node, style, "after"); afterNode != nil {
		rawChildren = append(rawChildren, afterNode)
	}

	// CSS 2.1 §9.2.1.1: If an inline element contains a block-level box,
	// split the inline around the block (block-in-inline splitting).
	rawChildren = b.expandInlineWithBlockChildren(rawChildren, style)

	// CSS 2.1 §12.2: Apply ::first-letter pseudo-element styling.
	rawChildren = b.applyFirstLetterSplit(rawChildren, node, style)

	// CSS 2.1 §9.2.1.1: If a block container has both inline-level and
	// block-level children, generate anonymous block boxes around consecutive
	// runs of inline-level children.
	lin.children = b.maybeWrapAnonymousBlocks(rawChildren, style)

	return lin
}

// isBlockLevel returns true if the child is a block-level box.
// Floats and abs-pos are excluded from this classification — they go into
// whichever group they're adjacent to.
func isBlockLevel(child *LayoutInputNode) bool {
	if child.IsText() {
		return false
	}
	s := child.Style()
	if s == nil {
		return false
	}
	// Floats and abs/fixed positioned elements are not block-level for
	// the purpose of anonymous block generation (CSS 2.1 §9.2.1.1).
	if s.GetFloat() != css.FloatNone {
		return false
	}
	pos := s.GetPosition()
	if pos == css.PositionAbsolute || pos == css.PositionFixed {
		return false
	}
	d := s.GetDisplay()
	switch d {
	case css.DisplayBlock, css.DisplayFlex, css.DisplayTable,
		css.DisplayListItem, css.DisplayFlowRoot, css.DisplayGrid:
		return true
	}
	return false
}

// isOutOfFlowOrFloat returns true for children that are out-of-flow
// (absolute/fixed positioned) or floated. These are neutral for the
// purpose of inline/block content classification.
func isOutOfFlowOrFloat(child *LayoutInputNode) bool {
	if child.IsText() {
		return false
	}
	s := child.Style()
	if s == nil {
		return false
	}
	if s.GetFloat() != css.FloatNone {
		return true
	}
	pos := s.GetPosition()
	return pos == css.PositionAbsolute || pos == css.PositionFixed
}

// maybeWrapAnonymousBlocks checks if children contain mixed inline/block
// content and wraps inline runs in anonymous block boxes if needed.
func (b *LayoutTreeBuilder) maybeWrapAnonymousBlocks(children []*LayoutInputNode, parentStyle *css.Style) []*LayoutInputNode {
	if parentStyle == nil || len(children) == 0 {
		return children
	}

	// Don't generate anonymous blocks inside inline containers or table internals.
	// Only block containers do this.
	parentDisplay := parentStyle.GetDisplay()
	if !isBlockContainer(parentDisplay) {
		return children
	}

	// Check if there are any block-level and inline-level children.
	// Floats and abs-pos are neutral — they don't trigger wrapping.
	hasBlock := false
	hasInline := false
	for _, child := range children {
		if isOutOfFlowOrFloat(child) {
			continue // neutral for classification
		}
		if isBlockLevel(child) {
			hasBlock = true
		} else if child.IsText() {
			if strings.TrimSpace(child.TextContent()) != "" {
				hasInline = true
			}
		} else {
			hasInline = true
		}
		if hasBlock && hasInline {
			break
		}
	}

	// No mixed content — no wrapping needed.
	if !hasBlock || !hasInline {
		return children
	}

	// Mixed content: wrap consecutive inline runs in anonymous blocks.
	// Floats and abs-pos children go into whichever group they're adjacent to.
	var result []*LayoutInputNode
	var inlineRun []*LayoutInputNode

	flushInlineRun := func() {
		if len(inlineRun) == 0 {
			return
		}
		// CSS 2.1 §9.2.2.1: If an anonymous block box contains only
		// whitespace (and possibly floats/abs-pos), discard the whitespace.
		if isWhitespaceOnly(inlineRun) {
			// Move floats/abs-pos out of the discarded run — they still
			// need to participate in layout.
			for _, c := range inlineRun {
				if isOutOfFlowOrFloat(c) {
					result = append(result, c)
				}
			}
			inlineRun = nil
			return
		}
		anonStyle := css.NewAnonymousBlockStyle(parentStyle)
		// CSS 2.1 §9.2.1.1: If the inline run contains block-in-inline
		// continuation nodes, copy properties from the inline element.
		for _, c := range inlineRun {
			if c.isContinuation {
				if c.Style().GetPosition() == css.PositionRelative {
					// Positioned inline: propagate the relative offset so the
					// anonymous block shifts with the rest of the split inline.
					// Do NOT copy background-color: for positioned inlines the
					// span background fragment (inline-width) gives the correct
					// visual. Copying it would paint full block-width background.
					anonStyle.Set("position", "relative")
					for _, prop := range []string{"top", "left", "right", "bottom"} {
						if v, ok := c.Style().Get(prop); ok {
							anonStyle.Set(prop, v)
						}
					}
				} else {
					// Non-positioned inline: copy background-color so the anonymous
					// block fills the full block width with the inline's background.
					// CSS 2.1 §9.2.1.1: background of anonymous blocks comes from
					// the inline element that generated them.
					if bg, ok := c.Style().Get("background-color"); ok {
						anonStyle.Set("background-color", bg)
					}
				}
				break
			}
		}
		anonBlock := &LayoutInputNode{
			style:       anonStyle,
			children:    inlineRun,
			isAnonymous: true,
		}
		result = append(result, anonBlock)
		inlineRun = nil
	}

	for _, child := range children {
		if isOutOfFlowOrFloat(child) {
			// Neutral children: accumulate with current inline run if one
			// is building, otherwise attach to block level.
			if len(inlineRun) > 0 {
				inlineRun = append(inlineRun, child)
			} else {
				// Between blocks or at the start: let them pass through.
				result = append(result, child)
			}
		} else if isBlockLevel(child) {
			flushInlineRun()
			result = append(result, child)
		} else {
			inlineRun = append(inlineRun, child)
		}
	}
	flushInlineRun()

	return result
}

// createPseudoElement creates a LayoutInputNode for a ::before or ::after
// pseudo-element if the element has matching CSS rules with content.
// Returns nil if no pseudo-element should be generated.
func (b *LayoutTreeBuilder) createPseudoElement(
	node *html.Node, parentStyle *css.Style, pseudoType string,
) *LayoutInputNode {
	if node.Type != html.ElementNode || parentStyle == nil {
		return nil
	}
	if len(b.stylesheets) == 0 {
		return nil
	}

	// Compute the pseudo-element's style.
	pseudoStyle := css.ComputePseudoElementStyle(
		node, pseudoType, b.stylesheets,
		b.viewportWidth, b.viewportHeight, parentStyle,
	)

	// Check if the pseudo-element has content.
	content, hasContent := pseudoStyle.GetContent()
	if !hasContent {
		return nil
	}
	if pseudoStyle.GetDisplay() == css.DisplayNone {
		return nil
	}

	// Create a synthetic DOM node for the pseudo-element.
	pseudoNode := &html.Node{
		Type:    html.ElementNode,
		TagName: "::" + pseudoType,
	}
	pseudoNode.Parent = node

	// Store the pseudo style in the styles map so it's accessible.
	b.styles[pseudoNode] = pseudoStyle

	// Build the LayoutInputNode.
	display := pseudoStyle.GetDisplay()

	if display == css.DisplayBlock || display == css.DisplayFlowRoot {
		// Block-level pseudo-element.
		lin := &LayoutInputNode{
			DOMNode: pseudoNode,
			style:   pseudoStyle,
		}
		// If there's text content, add it as a child text node.
		if content != "" {
			textNode := &html.Node{
				Type: html.TextNode,
				Text: content,
			}
			textNode.Parent = pseudoNode
			lin.children = []*LayoutInputNode{{
				DOMNode: textNode,
				style:   pseudoStyle,
			}}
		}
		return lin
	}

	// Inline pseudo-element (default).
	lin := &LayoutInputNode{
		DOMNode: pseudoNode,
		style:   pseudoStyle,
	}
	if content != "" {
		textNode := &html.Node{
			Type: html.TextNode,
			Text: content,
		}
		textNode.Parent = pseudoNode
		lin.children = []*LayoutInputNode{{
			DOMNode: textNode,
			style:   pseudoStyle,
		}}
	}
	return lin
}

// isBlockContainer returns true for display types that are block containers
// (can have block-level or inline-level children that trigger anonymous blocks).
func isBlockContainer(d css.DisplayType) bool {
	switch d {
	case css.DisplayBlock, css.DisplayListItem, css.DisplayFlowRoot,
		css.DisplayTableCell, css.DisplayTableCaption:
		return true
	}
	return false
}

// isWhitespaceOnly returns true if the run contains only whitespace text nodes
// (and possibly floats/abs-pos). CSS 2.1 §9.2.2.1: whitespace-only anonymous
// blocks between block-level boxes are not rendered.
func isWhitespaceOnly(run []*LayoutInputNode) bool {
	for _, child := range run {
		if isOutOfFlowOrFloat(child) {
			continue // neutral — ignore for whitespace check
		}
		if !child.IsText() {
			return false
		}
		if strings.TrimSpace(child.TextContent()) != "" {
			return false
		}
	}
	return true
}

// isInlineWithBlockChildren returns true if the node is display:inline and
// has at least one block-level direct child.
func isInlineWithBlockChildren(node *LayoutInputNode) bool {
	if node.IsText() || node.IsAnonymous() {
		return false
	}
	s := node.Style()
	if s == nil || s.GetDisplay() != css.DisplayInline {
		return false
	}
	for _, child := range node.Children() {
		if isBlockLevel(child) {
			return true
		}
	}
	return false
}

// expandInlineWithBlockChildren performs CSS 2.1 §9.2.1.1 block-in-inline
// splitting. When a display:inline element directly contains block-level
// children, it is split into inline continuations separated by the blocks.
// This pre-pass runs before maybeWrapAnonymousBlocks so the resulting
// mixed inline/block children trigger anonymous block generation correctly.
func (b *LayoutTreeBuilder) expandInlineWithBlockChildren(
	children []*LayoutInputNode, parentStyle *css.Style,
) []*LayoutInputNode {
	if parentStyle == nil || !isBlockContainer(parentStyle.GetDisplay()) {
		return children
	}
	// Quick check: any inline-with-block present?
	needsExpansion := false
	for _, child := range children {
		if isInlineWithBlockChildren(child) {
			needsExpansion = true
			break
		}
	}
	if !needsExpansion {
		return children
	}

	var result []*LayoutInputNode
	for _, child := range children {
		if !isInlineWithBlockChildren(child) {
			result = append(result, child)
			continue
		}
		// Split this inline element around its block children.
		// Each segment of inline children becomes a continuation of the
		// original inline element (same DOMNode + style).
		// CSS 2.1 §9.2.1.1: empty/whitespace-only continuations are suppressed.
		var segment []*LayoutInputNode
		var blockParts []*LayoutInputNode
		// continuations tracks all continuation nodes in order so we can
		// mark the first and last after collecting them all.
		var continuations []*LayoutInputNode
		hasCont := false
		for _, grandchild := range child.Children() {
			if isBlockLevel(grandchild) {
				// Flush the inline segment as a continuation (skip if whitespace-only).
				if len(segment) > 0 && !isWhitespaceOnly(segment) {
					cont := &LayoutInputNode{
						DOMNode:        child.DOMNode,
						style:          child.Style(),
						children:       segment,
						isContinuation: true,
					}
					result = append(result, cont)
					continuations = append(continuations, cont)
					hasCont = true
				}
				segment = nil
				blockParts = append(blockParts, grandchild)
				// CSS 2.1 §9.2.1.1: If the parent inline has position:relative,
				// wrap the block part in a transparent anonymous positioned block
				// so the block part shifts together with the inline's continuations.
				blockPart := grandchild
				if child.Style().GetPosition() == css.PositionRelative {
					wrapStyle := css.NewAnonymousBlockStyle(parentStyle)
					wrapStyle.Set("position", "relative")
					for _, prop := range []string{"top", "left", "right", "bottom"} {
						if v, ok := child.Style().Get(prop); ok {
							wrapStyle.Set(prop, v)
						}
					}
					blockPart = &LayoutInputNode{
						style:       wrapStyle,
						children:    []*LayoutInputNode{grandchild},
						isAnonymous: true,
					}
				}
				result = append(result, blockPart)
			} else {
				segment = append(segment, grandchild)
			}
		}
		if len(segment) > 0 && !isWhitespaceOnly(segment) {
			cont := &LayoutInputNode{
				DOMNode:        child.DOMNode,
				style:          child.Style(),
				children:       segment,
				isContinuation: true,
			}
			result = append(result, cont)
			continuations = append(continuations, cont)
			hasCont = true
		}
		// Mark first and last continuations so the inline layout pipeline can
		// suppress inline-start/inline-end borders on non-first/non-last fragments.
		// CSS 2.1 §9.2.1.1: the first fragment gets inline-start border/padding;
		// the last fragment gets inline-end border/padding.
		if len(continuations) > 0 {
			continuations[0].isFirstContinuation = true
			continuations[len(continuations)-1].isLastContinuation = true
		}
		// If the inline had only block children (no non-whitespace inline content),
		// wrap all blocks in an anonymous block that preserves the inline's visual
		// properties (e.g. opacity). This keeps the stacking context intact.
		if !hasCont && len(blockParts) > 0 {
			// Undo the individual block additions.
			result = result[:len(result)-len(blockParts)]
			wrapStyle := css.NewBlockifiedStyle(child.Style())
			wrapper := &LayoutInputNode{
				DOMNode:     child.DOMNode,
				style:       wrapStyle,
				children:    blockParts,
				isAnonymous: true,
			}
			result = append(result, wrapper)
		}
	}
	return result
}

// applyFirstLetterSplit implements CSS 2.1 §12.2 ::first-letter pseudo-element.
// If a block container has matching ::first-letter rules, the first letter of
// the first inline text is wrapped in a synthetic inline span with that style.
func (b *LayoutTreeBuilder) applyFirstLetterSplit(
	children []*LayoutInputNode, node *html.Node, parentStyle *css.Style,
) []*LayoutInputNode {
	if node == nil || node.Type != html.ElementNode {
		return children
	}
	if parentStyle == nil || !isBlockContainer(parentStyle.GetDisplay()) {
		return children
	}
	if len(b.stylesheets) == 0 {
		return children
	}
	if !css.HasFirstLetterRules(node, b.stylesheets, b.viewportWidth, b.viewportHeight) {
		return children
	}

	// Compute the ::first-letter style.
	flStyle := css.ComputePseudoElementStyle(
		node, "first-letter", b.stylesheets,
		b.viewportWidth, b.viewportHeight, parentStyle,
	)

	// Find and split the first non-whitespace character in the inline content.
	// We only look at direct text children or text inside inline children.
	return b.splitFirstLetter(children, node, flStyle)
}

// splitFirstLetter walks children to find the first text character and wraps
// it in a synthetic inline span with the first-letter style.
func (b *LayoutTreeBuilder) splitFirstLetter(
	children []*LayoutInputNode, parentNode *html.Node, flStyle *css.Style,
) []*LayoutInputNode {
	for i, child := range children {
		if !child.IsText() {
			continue
		}
		text := child.TextContent()
		// Find first non-whitespace character.
		idx := strings.IndexFunc(text, func(r rune) bool {
			return r != ' ' && r != '\t' && r != '\n' && r != '\r'
		})
		if idx < 0 {
			continue
		}
		// Find where the first letter ends (just take the first rune).
		letterEnd := idx
		for _, r := range text[idx:] {
			_ = r
			letterEnd += len(string(r))
			break
		}

		letter := text[idx:letterEnd]
		rest := text[letterEnd:]

		// Create a synthetic DOM node and LayoutInputNode for the first letter.
		letterDOMNode := &html.Node{Type: html.TextNode, Text: letter}
		letterDOMNode.Parent = parentNode
		flSpanDOM := &html.Node{Type: html.ElementNode, TagName: "::first-letter"}
		flSpanDOM.Parent = parentNode
		b.styles[flSpanDOM] = flStyle

		flSpan := &LayoutInputNode{
			DOMNode: flSpanDOM,
			style:   flStyle,
			children: []*LayoutInputNode{{
				DOMNode: letterDOMNode,
				style:   flStyle,
			}},
		}

		// Build the result: children before, [leading-ws if any, flSpan, rest-text, ...children after]
		var result []*LayoutInputNode
		result = append(result, children[:i]...)

		// Add leading whitespace as a separate text node if needed.
		if idx > 0 {
			wsDOMNode := &html.Node{Type: html.TextNode, Text: text[:idx]}
			wsDOMNode.Parent = parentNode
			result = append(result, &LayoutInputNode{
				DOMNode: wsDOMNode,
				style:   child.style,
			})
		}

		result = append(result, flSpan)

		if rest != "" {
			restDOMNode := &html.Node{Type: html.TextNode, Text: rest}
			restDOMNode.Parent = parentNode
			result = append(result, &LayoutInputNode{
				DOMNode: restDOMNode,
				style:   child.style,
			})
		}

		result = append(result, children[i+1:]...)
		return result
	}
	return children
}
