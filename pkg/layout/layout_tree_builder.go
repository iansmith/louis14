package layout

import (
	"strconv"
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
	counters        map[string][]int // CSS counter stacks (name → stack of values)
	quoteDepth      int              // nesting depth for open-quote/close-quote
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

	// CSS 2.1 §12.5: Process counter-reset before content evaluation.
	if style != nil {
		b.processCounterReset(style)
	}

	// CSS Pseudo-4 §4.2: Compute ::marker style for list items,
	// but only if there are actual ::marker rules matching.
	if style != nil && style.GetDisplay() == css.DisplayListItem && len(b.stylesheets) > 0 {
		if css.HasPseudoElementRules(node, "marker", b.stylesheets, b.viewportWidth, b.viewportHeight) {
			markerStyle := css.ComputePseudoElementStyle(
				node, "marker", b.stylesheets,
				b.viewportWidth, b.viewportHeight, style,
			)
			lin.MarkerStyle = markerStyle
		}
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

	// CSS Pseudo-Elements §3: Compute ::first-line pseudo-element style.
	b.computeFirstLineStyle(lin, node, style)

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
				if c.Style().GetPosition() == css.PositionRelative || c.Style().GetPosition() == css.PositionSticky {
					// Positioned inline: propagate the relative offset so the
					// anonymous block shifts with the rest of the split inline.
					// Do NOT copy background-color: for positioned inlines the
					// span background fragment (inline-width) gives the correct
					// visual. Copying it would paint full block-width background.
					anonStyle.Set("position", string(c.Style().GetPosition()))
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
//
// Handles CSS 2.1 §12.2 content values: text strings, url() images,
// counter(), attr(), open-quote/close-quote. Also implements CSS 2.1 §9.7
// display blockification for floated pseudo-elements.
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

	// Check if the pseudo-element has content using the rich parser.
	contentValues, hasContent := pseudoStyle.GetContentValues()
	if !hasContent || len(contentValues) == 0 {
		return nil
	}
	if pseudoStyle.GetDisplay() == css.DisplayNone {
		return nil
	}

	// CSS 2.1 §12.5: Process counter-increment on pseudo-elements.
	b.processCounterIncrement(pseudoStyle)

	// CSS 2.1 §9.7: Blockify floated pseudo-elements.
	// When float is set, display is forced to block.
	display := pseudoStyle.GetDisplay()
	if pseudoStyle.GetFloat() != css.FloatNone {
		if display != css.DisplayBlock && display != css.DisplayTable {
			pseudoStyle.Set("display", "block")
			display = css.DisplayBlock
		}
	}

	// Create a synthetic DOM node for the pseudo-element.
	pseudoNode := &html.Node{
		Type:    html.ElementNode,
		TagName: "::" + pseudoType,
	}
	pseudoNode.Parent = node

	// Store the pseudo style in the styles map so it's accessible.
	b.styles[pseudoNode] = pseudoStyle

	// Build child nodes from content values.
	// Collect quote strings from parent style.
	quotes := []string{"\"", "\"", "'", "'"}
	if parentStyle != nil {
		if q, ok := parentStyle.Get("quotes"); ok {
			quotes = b.parseQuotes(q)
		}
	}

	// Build child nodes from content values.
	// Adjacent text-producing values (text, counter, attr, quote) are merged
	// into a single text node to match how real DOM elements concatenate text.
	// Only url() (replaced elements) forces a text flush.
	var children []*LayoutInputNode
	var pendingText strings.Builder

	flushText := func() {
		if pendingText.Len() > 0 {
			textNode := &html.Node{Type: html.TextNode, Text: pendingText.String()}
			textNode.Parent = pseudoNode
			children = append(children, &LayoutInputNode{
				DOMNode: textNode,
				style:   pseudoStyle,
			})
			pendingText.Reset()
		}
	}

	for _, cv := range contentValues {
		switch cv.Type {
		case "text":
			pendingText.WriteString(cv.Value)
		case "url":
			// Flush any accumulated text before the image.
			flushText()
			// Create a synthetic <img> element for url() content.
			imgNode := &html.Node{
				Type:    html.ElementNode,
				TagName: "img",
				Attributes: map[string]string{
					"src": cv.Value,
				},
			}
			imgNode.Parent = pseudoNode
			imgStyle := css.NewStyle()
			imgStyle.Set("display", "inline")
			imgStyle.ViewportWidth = b.viewportWidth
			imgStyle.ViewportHeight = b.viewportHeight
			b.styles[imgNode] = imgStyle
			children = append(children, &LayoutInputNode{
				DOMNode: imgNode,
				style:   imgStyle,
			})
		case "counter":
			val := b.getCounterValue(cv.Value)
			pendingText.WriteString(strconv.Itoa(val))
		case "attr":
			if node.Attributes != nil {
				if attrVal, ok := node.Attributes[cv.Value]; ok {
					pendingText.WriteString(attrVal)
				}
			}
		case "open-quote":
			idx := b.quoteDepth * 2
			if idx < len(quotes) {
				pendingText.WriteString(quotes[idx])
			}
			b.quoteDepth++
		case "close-quote":
			if b.quoteDepth > 0 {
				b.quoteDepth--
			}
			idx := b.quoteDepth*2 + 1
			if idx < len(quotes) {
				pendingText.WriteString(quotes[idx])
			}
		}
	}
	// Flush any remaining text.
	flushText()

	// Build the LayoutInputNode with the generated children.
	lin := &LayoutInputNode{
		DOMNode:  pseudoNode,
		style:    pseudoStyle,
		children: children,
	}
	return lin
}

// resolveContentText resolves CSS content values (text, counters) to a plain string.
// Used for ::marker content resolution during layout tree building.
func (b *LayoutTreeBuilder) resolveContentText(contentVals []css.ContentValue) string {
	var buf strings.Builder
	for _, cv := range contentVals {
		switch cv.Type {
		case "text":
			buf.WriteString(cv.Value)
		case "counter":
			val := b.getCounterValue(cv.Value)
			buf.WriteString(strconv.Itoa(val))
		case "counters":
			// counters(name, separator) — resolve to the counter value.
			// cv.Value is the counter name, cv.Separator is the join string.
			val := b.getCounterValue(cv.Value)
			buf.WriteString(strconv.Itoa(val))
		}
	}
	return buf.String()
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
				if child.Style().GetPosition() == css.PositionRelative || child.Style().GetPosition() == css.PositionSticky {
					wrapStyle := css.NewAnonymousBlockStyle(parentStyle)
					wrapStyle.Set("position", string(child.Style().GetPosition()))
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

// computeFirstLineStyle checks if a block container has matching ::first-line
// rules and, if so, computes the pseudo-element style and stores it on the
// LayoutInputNode. The style is later applied to inline items on the first
// formatted line during inline layout.
func (b *LayoutTreeBuilder) computeFirstLineStyle(lin *LayoutInputNode, node *html.Node, parentStyle *css.Style) {
	if node == nil || node.Type != html.ElementNode {
		return
	}
	if parentStyle == nil || !isBlockContainer(parentStyle.GetDisplay()) {
		return
	}
	if len(b.stylesheets) == 0 {
		return
	}
	if !css.HasFirstLineRules(node, b.stylesheets, b.viewportWidth, b.viewportHeight) {
		return
	}

	// Compute the ::first-line style.
	flStyle := css.ComputePseudoElementStyle(
		node, "first-line", b.stylesheets,
		b.viewportWidth, b.viewportHeight, parentStyle,
	)
	if flStyle != nil {
		lin.FirstLineStyle = flStyle
	}
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

// processCounterReset handles the counter-reset CSS property.
// CSS 2.1 §12.5.1: counter-reset creates or resets one or more counters.
func (b *LayoutTreeBuilder) processCounterReset(style *css.Style) {
	val, ok := style.Get("counter-reset")
	if !ok || val == "none" || val == "" {
		return
	}
	if b.counters == nil {
		b.counters = make(map[string][]int)
	}
	parts := strings.Fields(val)
	for i := 0; i < len(parts); i++ {
		name := parts[i]
		if name == "none" {
			continue
		}
		value := 0
		if i+1 < len(parts) {
			if v, err := strconv.Atoi(parts[i+1]); err == nil {
				value = v
				i++
			}
		}
		// Push a new counter scope.
		b.counters[name] = append(b.counters[name], value)
	}
}

// processCounterIncrement handles the counter-increment CSS property.
// CSS 2.1 §12.5.2: counter-increment increments an existing counter.
func (b *LayoutTreeBuilder) processCounterIncrement(style *css.Style) {
	val, ok := style.Get("counter-increment")
	if !ok || val == "none" || val == "" {
		return
	}
	if b.counters == nil {
		b.counters = make(map[string][]int)
	}
	parts := strings.Fields(val)
	for i := 0; i < len(parts); i++ {
		name := parts[i]
		if name == "none" {
			continue
		}
		increment := 1
		if i+1 < len(parts) {
			if v, err := strconv.Atoi(parts[i+1]); err == nil {
				increment = v
				i++
			}
		}
		stack := b.counters[name]
		if len(stack) == 0 {
			// Auto-instantiate counter at the root scope.
			b.counters[name] = []int{increment}
		} else {
			stack[len(stack)-1] += increment
		}
	}
}

// getCounterValue returns the current value of a named counter.
func (b *LayoutTreeBuilder) getCounterValue(name string) int {
	if b.counters == nil {
		return 0
	}
	stack := b.counters[name]
	if len(stack) == 0 {
		return 0
	}
	return stack[len(stack)-1]
}

// parseQuotes parses the CSS quotes property value into a list of quote strings.
// Format: "open1" "close1" "open2" "close2" ...
func (b *LayoutTreeBuilder) parseQuotes(val string) []string {
	var quotes []string
	val = strings.TrimSpace(val)
	for len(val) > 0 {
		val = strings.TrimSpace(val)
		if len(val) == 0 {
			break
		}
		if val[0] == '"' || val[0] == '\'' {
			quote := val[0]
			end := 1
			for end < len(val) && val[end] != quote {
				if val[end] == '\\' && end+1 < len(val) {
					end += 2
				} else {
					end++
				}
			}
			if end < len(val) {
				text := val[1:end]
				// Handle CSS escape sequences for quote characters.
				text = b.unescapeQuoteText(text)
				quotes = append(quotes, text)
				val = val[end+1:]
			} else {
				break
			}
		} else {
			// Skip non-quote tokens.
			idx := strings.IndexByte(val, ' ')
			if idx < 0 {
				break
			}
			val = val[idx:]
		}
	}
	return quotes
}

// unescapeQuoteText handles common CSS escape sequences in quote strings.
func (b *LayoutTreeBuilder) unescapeQuoteText(text string) string {
	if !strings.Contains(text, "\\") {
		return text
	}
	var result strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			// Try to parse a hex escape: \XXXX
			hexStart := i + 1
			hexEnd := hexStart
			for hexEnd < len(text) && hexEnd < hexStart+6 &&
				((text[hexEnd] >= '0' && text[hexEnd] <= '9') ||
					(text[hexEnd] >= 'a' && text[hexEnd] <= 'f') ||
					(text[hexEnd] >= 'A' && text[hexEnd] <= 'F')) {
				hexEnd++
			}
			if hexEnd > hexStart {
				if codepoint, err := strconv.ParseInt(text[hexStart:hexEnd], 16, 32); err == nil {
					result.WriteRune(rune(codepoint))
					i = hexEnd - 1
					// Skip optional trailing space after hex escape.
					if i+1 < len(text) && text[i+1] == ' ' {
						i++
					}
					continue
				}
			}
			// Simple escape: skip the backslash.
			i++
			result.WriteByte(text[i])
		} else {
			result.WriteByte(text[i])
		}
	}
	return result.String()
}
