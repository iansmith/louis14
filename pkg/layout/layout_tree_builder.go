package layout

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LayoutTreeBuilder constructs the layout tree from a DOM tree and
// computed styles. It wraps each DOM node in a LayoutInputNode, pre-filters
// display:none children, and (in Phase 1) generates anonymous block boxes.
//
// Mirrors Blink's LayoutTreeBuilderForNG.
type LayoutTreeBuilder struct {
	styles         map[*html.Node]*css.Style
	stylesheets    []*css.Stylesheet
	viewportWidth  float64
	viewportHeight float64

	// counterCtx is the Blink-faithful CountersAttachmentContext used
	// to resolve counter() / counters() in content and ::marker text.
	// One context is threaded through the single pre-order traversal
	// in buildNode, with EnterObject called after style resolution and
	// LeaveObject after recursing into children. See
	// pkg/css/counters_attachment_context.go.
	counterCtx *css.CountersAttachmentContext

	quoteDepth int // nesting depth for open-quote/close-quote

	// quoteDepthSaved is the stack of saved depths used to implement
	// CSS Contain 1 §3.3 style-containment for quotes. On entering a
	// style-contained element we push the current quoteDepth; on
	// leaving we restore it. This matches Blink's effect (the
	// contained subtree's quote nesting cannot escape the boundary)
	// without porting the full StyleContainmentScopeTree from
	// third_party/blink/renderer/core/css/style_containment_scope.cc
	// @ 4883d11fef — that tree exists for dynamic invalidation; for
	// our static layout pass, save/restore is sufficient because we
	// process quotes in document order in a single traversal.
	quoteDepthSaved []int
}

// BuildLayoutTree creates the layout tree rooted at the given DOM node.
func (b *LayoutTreeBuilder) BuildLayoutTree(root *html.Node) *LayoutInputNode {
	if b.counterCtx == nil {
		b.counterCtx = css.NewCountersAttachmentContext()
	}
	tree := b.buildNode(root)
	b.normalizeTableSubtrees(tree)
	b.normalizeRubySubtrees(tree)
	assignDOMIndices(tree)
	return tree
}

// normalizeTableSubtrees walks the built layout tree and applies CSS 2.1
// §17.2.1 anonymous-table-box generation inside every real table /
// inline-table subtree. Scoping the normalization to table roots (rather
// than running it from buildNode at every level) matches what Blink does:
// standalone display:table-row-group / table-row / table-cell boxes whose
// ancestor is not a table (e.g. a pseudo-element
// `div:before { display: table-row-group }` on a non-table div) are left
// alone — louis14 has not yet implemented the reverse §17.2.1 rule that
// would generate an anonymous containing table around them, and existing
// reftests rely on the legacy fall-through-to-block layout.
func (b *LayoutTreeBuilder) normalizeTableSubtrees(node *LayoutInputNode) {
	if node == nil {
		return
	}
	if s := node.Style(); s != nil {
		switch s.GetDisplay() {
		case css.DisplayTable, css.DisplayInlineTable:
			node.children = b.wrapAnonymousTableBoxes(node.children, s)
			// wrapAnonymousTableBoxes recurses through the proper-table
			// subtree for us, so no further walk is needed here.
			return
		}
	}
	for _, c := range node.children {
		b.normalizeTableSubtrees(c)
	}
}

// normalizeRubySubtrees walks the built layout tree and applies the
// CSS Ruby §2.2 box-fixup rules that operate purely on the box tree.
//
// Two responsibilities:
//  1. For `display: block ruby` elements, generate the
//     `LayoutRubyAsBlock` two-box structure: the principal box stays
//     block-level (its display is rewritten in place to `block`) and
//     its original children move under a single anonymous child whose
//     display is `ruby`. Mirrors Blink
//     `core/layout/layout_ruby_as_block.{h,cc}` and the
//     `EquivalentBlockDisplay` / `EquivalentInlineDisplay` pair at
//     `core/css/resolver/style_adjuster.cc:247-356`.
//  2. Inlinification (`#anon-gen-inlinize`): when a node's layout
//     parent is a `display: ruby` or `display: ruby-text` box, set
//     each in-flow child's used display to its
//     `EquivalentInlineDisplay` and force `float: none`. Mirrors
//     Blink `AdjustStyleForDisplay` at
//     `core/css/resolver/style_adjuster.cc:788-805`. The rule is
//     recursive (csswg-drafts #1341) — a block inside a block inside
//     a ruby is still inlinified.
func (b *LayoutTreeBuilder) normalizeRubySubtrees(node *LayoutInputNode) {
	if node == nil {
		return
	}
	if s := node.Style(); s != nil {
		switch s.GetDisplay() {
		case css.DisplayBlockRuby:
			b.wrapBlockRubyAsTwoBox(node)
			// The principal box's only child is now an anonymous
			// inline `display:ruby` box; recurse into it so its
			// in-flow children get inlinified.
			for _, c := range node.children {
				b.normalizeRubySubtrees(c)
			}
			return
		case css.DisplayRuby, css.DisplayRubyText:
			// Inlinify in-flow children, then recurse so descendants
			// (which are themselves inside a ruby/ruby-text layout
			// parent after inlinification) also get processed.
			b.inlinifyRubyChildren(node)
			for _, c := range node.children {
				b.normalizeRubySubtrees(c)
			}
			return
		}
	}
	for _, c := range node.children {
		b.normalizeRubySubtrees(c)
	}
}

// wrapBlockRubyAsTwoBox rewrites a `display: block ruby` node into
// Blink's `LayoutRubyAsBlock` two-box model: the principal box keeps
// the element identity but is now `display: block`, and the original
// in-flow children move under a single new anonymous child whose
// display is `ruby`. Border/padding/margin remain on the principal
// block; the anonymous inline ruby inherits only inheritable
// properties (text-align, font, etc.) via NewAnonymousInlineRubyStyle.
func (b *LayoutTreeBuilder) wrapBlockRubyAsTwoBox(node *LayoutInputNode) {
	if node == nil || node.style == nil {
		return
	}
	// Rewrite the principal box's display to `block`. Cloning avoids
	// mutating a Style that other layout-tree branches might share
	// (e.g. when the same DOM element is referenced from a
	// pseudo-element style cache).
	principal := node.style.Clone()
	principal.Set("display", "block")
	node.style = principal

	// Original children become the children of a single anonymous
	// inline `display:ruby` box.
	anon := &LayoutInputNode{
		style:       css.NewAnonymousInlineRubyStyle(node.style),
		children:    node.children,
		isAnonymous: true,
	}
	node.children = []*LayoutInputNode{anon}
}

// inlinifyRubyChildren applies the CSS Ruby §2.2 `#anon-gen-inlinize`
// rule to the direct children of a `display:ruby` or `display:ruby-text`
// node: each in-flow child has its used display rewritten to its
// EquivalentInlineDisplay and any float is dropped. OOF (absolute /
// fixed) children are left alone per Blink
// `style_adjuster.cc:788-805` (`!builder.HasOutOfFlowPosition()`).
//
// Recursion into descendants happens in normalizeRubySubtrees: after
// inlinification, the child's layout parent in the recursive call is
// the same ruby/ruby-text, so the rule fires again at each level.
func (b *LayoutTreeBuilder) inlinifyRubyChildren(node *LayoutInputNode) {
	if node == nil {
		return
	}
	for _, child := range node.children {
		if child == nil || child.style == nil {
			continue
		}
		// OOF children are exempt from inlinification.
		pos := child.style.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			continue
		}
		d := child.style.GetDisplay()
		eq := css.EquivalentInlineDisplay(d)
		isFloated := child.style.GetFloat() != css.FloatNone
		if eq == d && !isFloated {
			continue
		}
		// Mutate via a clone so we don't disturb shared style cells.
		clone := child.style.Clone()
		if eq != d {
			clone.Set("display", string(eq))
		}
		if isFloated {
			clone.Set("float", "none")
		}
		child.style = clone
	}
}

// assignDOMIndices assigns a monotonically increasing pre-order index to each
// LayoutInputNode. This ensures correct paint ordering when out-of-flow
// children propagate to ancestor boxes (CSS 2.1 Appendix E tree order).
func assignDOMIndices(root *LayoutInputNode) {
	index := 0
	var walk func(n *LayoutInputNode)
	walk = func(n *LayoutInputNode) {
		n.DOMIndex = index
		index++
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(root)
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

	// CSS Lists 3 §counter-properties: enter the counter scope for
	// this element. EnterObject resolves the element's counter-reset
	// and counter-increment directives via the Blink-faithful
	// CountersAttachmentContext (see pkg/css/counters_attachment_context.go).
	// Pre-order traversal: must run BEFORE recursing into children so
	// the children see this element's counter directives in scope.
	// LeaveObject is invoked after children are built (below).
	//
	// CSS Contain 1 §3.3 style containment: the contained element's
	// own counter directives apply to the OUTER scope (so ::before /
	// ::after can read them), then a sentinel is pushed before
	// descending into real descendants. Quote nesting depth is
	// save/restored across the boundary so changes inside don't leak
	// out. Counter sentinel push/pop lives in EnterObject /
	// LeaveObject; quote save/restore lives here because quotes are
	// emitted by the layout-tree builder (open-quote / close-quote
	// resolution in createPseudoElement), not by the counter context.
	if style != nil {
		b.counterCtx.EnterObject(node, style)
		defer b.counterCtx.LeaveObject(node, style)
		if style.HasStyleContainment() {
			b.quoteDepthSaved = append(b.quoteDepthSaved, b.quoteDepth)
			defer func() {
				n := len(b.quoteDepthSaved)
				b.quoteDepth = b.quoteDepthSaved[n-1]
				b.quoteDepthSaved = b.quoteDepthSaved[:n-1]
			}()
		}
	}

	// CSS Pseudo-4 §4.2: Compute ::marker style for list items,
	// but only if there are actual ::marker rules matching.
	if style != nil && style.GetDisplay() == css.DisplayListItem && len(b.stylesheets) > 0 {
		if css.HasPseudoElementRules(node, "marker", b.stylesheets, b.viewportWidth, b.viewportHeight) {
			markerStyle := css.ComputePseudoElementStyle(
				node, "marker", b.stylesheets,
				b.viewportWidth, b.viewportHeight, style,
			)
			// CSS Pseudo-4 §3: UA default for ::marker is unicode-bidi: isolate.
			if _, hasBidi := markerStyle.Get("unicode-bidi"); !hasBidi {
				clone := markerStyle.Clone()
				clone.Set("unicode-bidi", "isolate")
				markerStyle = clone
			}
			lin.MarkerStyle = markerStyle
			// Extract content from ::marker { content: } for layout-time use.
			// This path is the "outside" marker — list-style-position
			// defaults to outside and we cache content here for the
			// paint-time renderer (Phase 6 will move outside markers to
			// real boxes). Counter lookups use `node` as the origin
			// because this path does NOT push a separate counter scope
			// for ::marker; that scope only exists when we actually
			// emit a marker box via createMarkerPseudoElement.
			if cv, ok := markerStyle.GetContentValues(); ok && len(cv) > 0 {
				lin.MarkerContent = b.resolveContentText(cv, node, node)
			}
		}
		// For list-style-type: <string> without ::marker rules, use the string
		// as marker content when list-style-position: inside.
		if lin.MarkerContent == "" && style.GetListStylePosition() == "inside" {
			lst := style.GetListStyleType()
			if lst != "" {
				s := string(lst)
				if !isBuiltinListStyleType(lst) {
					lin.MarkerContent = s
				}
			}
		}
	}

	// Build layout children, filtering out display:none and non-layout nodes.
	var rawChildren []*LayoutInputNode

	// CSS Pseudo-4 §4.2: Insert ::marker pseudo-element as first child
	// of display:list-item elements when list-style-position is inside.
	// The marker comes before ::before per CSS Pseudo-4 pseudo-element
	// ordering (marker, before, children, after).
	if style != nil && style.GetDisplay() == css.DisplayListItem {
		if markerNode := b.createMarkerPseudoElement(node, style); markerNode != nil {
			rawChildren = append(rawChildren, markerNode)
		}
	}

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
			// CSS Display 3 §3.2 `display: contents`: the element generates
			// no box; its children participate in layout as if they were
			// direct children of its parent. Inherited properties on the
			// contents element still propagate to children — that already
			// happened during the style cascade (children's computed styles
			// were resolved with the contents element as DOM parent). Here
			// we simply expand the contents element's own children into the
			// current layout-child list, recursing through nested contents.
			// Out of scope (per W2.6 plan): ::before/::after on contents,
			// ::first-letter / ::first-line, form controls, shadow DOM.
			// Mirrors Blink's LayoutObjectIsNeeded() returning false for
			// display:contents (third_party/blink/renderer/core/dom/element.cc).
			if childStyle.GetDisplay() == css.DisplayContents {
				rawChildren = append(rawChildren, b.expandContentsChildren(child)...)
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

// expandContentsChildren returns the layout-tree children produced by a
// `display: contents` element. Per CSS Display 3 §3.2 the contents element
// itself generates no box; its DOM children participate in layout as if
// they were direct children of the contents element's parent.
//
// Inherited properties on the contents element still propagate: text
// children use the contents element's computed style as their style
// source (just as they would if a box existed for it), and element
// children already had their computed styles resolved against the
// contents element as their DOM parent during the cascade — so
// `b.buildNode(elementChild)` produces the right computed style without
// any extra plumbing.
//
// Nested `display: contents` elements expand recursively. Display:none
// children are filtered out, matching the main buildNode loop.
//
// Mirrors Blink's behavior in `LayoutTreeBuilderForElement` where a
// display:contents element is not given a LayoutObject; its children
// are attached to the grandparent. See
// third_party/blink/renderer/core/dom/layout_tree_builder.cc and
// third_party/blink/renderer/core/dom/element.cc::LayoutObjectIsNeeded.
func (b *LayoutTreeBuilder) expandContentsChildren(contents *html.Node) []*LayoutInputNode {
	contentsStyle := b.styles[contents]
	var out []*LayoutInputNode
	for _, child := range contents.Children {
		switch child.Type {
		case html.TextNode:
			out = append(out, &LayoutInputNode{
				DOMNode: child,
				style:   contentsStyle, // text inherits contents element's style
			})
		case html.ElementNode:
			childStyle := b.styles[child]
			if childStyle == nil {
				continue
			}
			if childStyle.GetDisplay() == css.DisplayNone {
				continue
			}
			if childStyle.GetDisplay() == css.DisplayContents {
				out = append(out, b.expandContentsChildren(child)...)
				continue
			}
			out = append(out, b.buildNode(child))
		default:
			// Skip comment nodes, doctype, etc.
			continue
		}
	}
	return out
}

// isProperTableChild reports whether a child is a proper table child per
// CSS 2.1 §17.2.1: table-caption, table-row-group variants, or (because
// louis14 does not yet map <col>/<colgroup> to dedicated display values)
// the col/colgroup HTML elements.
func isProperTableChild(c *LayoutInputNode) bool {
	s := c.Style()
	if s == nil {
		return false
	}
	switch s.GetDisplay() {
	case css.DisplayTableCaption, css.DisplayTableRowGroup,
		css.DisplayTableHeaderGroup, css.DisplayTableFooterGroup:
		return true
	}
	if c.DOMNode != nil {
		switch c.DOMNode.TagName {
		case "col", "colgroup":
			return true
		}
	}
	return false
}

// isProperRowGroupChild reports whether a child is a proper row-group child
// per CSS 2.1 §17.2.1 (only table-row).
func isProperRowGroupChild(c *LayoutInputNode) bool {
	s := c.Style()
	if s == nil {
		return false
	}
	return s.GetDisplay() == css.DisplayTableRow
}

// isProperRowChild reports whether a child is a proper row child per
// CSS 2.1 §17.2.1 (only table-cell).
func isProperRowChild(c *LayoutInputNode) bool {
	s := c.Style()
	if s == nil {
		return false
	}
	return s.GetDisplay() == css.DisplayTableCell
}

// wrapAnonymousTableBoxes applies CSS 2.1 §17.2.1 anonymous table-box
// generation to a parent's children. Mirrors Blink's
// LayoutTable::AddChild / LayoutTableSection::AddChild /
// LayoutTableRow::AddChild: runs of stray children collapse into a single
// anonymous wrapper (one wrapper per run, not one per child), and the
// wrapper's children are recursively wrapped for the next table level so
// e.g. a bare block directly inside a <table> becomes
// anon-row-group → anon-row → anon-cell → <block>.
//
// Whitespace-only stray runs are discarded: text between table-internal
// boxes is not content (CSS 2.1 §17.2.1 "Anonymous table objects" note).
func (b *LayoutTreeBuilder) wrapAnonymousTableBoxes(
	children []*LayoutInputNode, parentStyle *css.Style,
) []*LayoutInputNode {
	if parentStyle == nil || len(children) == 0 {
		return children
	}

	var accepts func(*LayoutInputNode) bool
	var newWrapperStyle func(*css.Style) *css.Style
	switch parentStyle.GetDisplay() {
	case css.DisplayTable, css.DisplayInlineTable:
		accepts = isProperTableChild
		newWrapperStyle = css.NewAnonymousTableRowGroupStyle
	case css.DisplayTableRowGroup, css.DisplayTableHeaderGroup,
		css.DisplayTableFooterGroup:
		accepts = isProperRowGroupChild
		newWrapperStyle = css.NewAnonymousTableRowStyle
	case css.DisplayTableRow:
		accepts = isProperRowChild
		newWrapperStyle = css.NewAnonymousTableCellStyle
	default:
		return children
	}

	var result []*LayoutInputNode
	var stray []*LayoutInputNode

	flush := func() {
		if len(stray) == 0 {
			return
		}
		// Drop whitespace-only stray runs (non-content text between table
		// elements). Non-text or non-whitespace content must generate boxes.
		allWS := true
		for _, c := range stray {
			if !c.IsText() || strings.TrimSpace(c.TextContent()) != "" {
				allWS = false
				break
			}
		}
		if allWS {
			stray = nil
			return
		}
		wrapperStyle := newWrapperStyle(parentStyle)
		// Recurse so the wrapper's own level is normalized too.
		wrapperChildren := b.wrapAnonymousTableBoxes(stray, wrapperStyle)
		result = append(result, &LayoutInputNode{
			style:       wrapperStyle,
			children:    wrapperChildren,
			isAnonymous: true,
		})
		stray = nil
	}

	for _, child := range children {
		if accepts(child) {
			flush()
			// Recurse through authored descendants too, so a real
			// <tbody> gets its <tr>s normalized and real <tr>s get
			// their cells normalized. Without this the post-pass
			// would only traverse into synthesized wrappers.
			if cs := child.Style(); cs != nil {
				child.children = b.wrapAnonymousTableBoxes(child.children, cs)
			}
			result = append(result, child)
		} else {
			stray = append(stray, child)
		}
	}
	flush()
	return result
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
		css.DisplayListItem, css.DisplayFlowRoot, css.DisplayGrid,
		css.DisplayBlockRuby:
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
		// CSS Writing Modes §2.2: unicode-bidi does not inherit, but a
		// plaintext/bidi-override/isolate-override block's inline content
		// forms its paragraphs. When that inline content is wrapped into
		// an anonymous block (because sibling blocks break up the inline
		// run), the anonymous block must continue to apply the parent's
		// unicode-bidi behaviour — otherwise each inline-run chunk loses
		// its auto paragraph-direction resolution.
		if bidiVal, ok := parentStyle.Get("unicode-bidi"); ok {
			switch bidiVal {
			case "plaintext", "bidi-override", "isolate-override":
				anonStyle.Set("unicode-bidi", bidiVal)
			}
		}
		// CSS 2.1 §9.2.1.1: If the inline run contains block-in-inline
		// continuation nodes, copy background-color from the inline element
		// so the anonymous block fills the full block width with the inline's
		// background.
		//
		// Do NOT propagate position:relative/sticky to this anon block: the
		// inline continuation's own fragments already carry the offset (see
		// inline_layout.go RelativeOffset handling). Copying it here would
		// double-offset the inline paint position (CSS 2.1 §9.4.3). Also skip
		// background-color for positioned inlines — their span background
		// fragment renders the inline-width background; copying here would
		// paint a full block-width bar that the spec doesn't call for.
		for _, c := range inlineRun {
			if c.isContinuation {
				pos := c.Style().GetPosition()
				if pos != css.PositionRelative && pos != css.PositionSticky {
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

	// CSS Lists 3: pseudo-elements participate in the counter scope.
	// We enter the pseudo's scope before resolving its content so any
	// counter() / counters() reads see the pseudo's own
	// counter-reset / counter-increment directives, and we leave the
	// scope afterward so resets on the pseudo don't leak to siblings.
	// We need to push EnterObject on a synthetic node that is the
	// SAME node we'll attach to the layout tree (so RemoveStaleCounters
	// can compare ancestry correctly via Parent links). The pseudoNode
	// is allocated below — defer the call until after pseudoNode exists.

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

	// Enter the pseudo-element's counter scope. The pseudo is treated
	// as a child of `node` in document order, so its counter-reset /
	// counter-increment apply to its own ::before / content reads and
	// then drop out of scope when we leave below. We pair this with
	// LeaveObject before returning the LayoutInputNode.
	b.counterCtx.EnterObject(pseudoNode, pseudoStyle)
	defer b.counterCtx.LeaveObject(pseudoNode, pseudoStyle)

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
			// counter(name [, style]): innermost in-scope value.
			// Phase 1 always renders as decimal; Phase 5 will route
			// cv.Style through CounterStyle.
			vals := b.counterCtx.GetCounterValues(pseudoNode, cv.Value, true)
			pendingText.WriteString(formatCounterValues(vals, "", cv.Style))
		case "counters":
			// counters(name, sep [, style]): outermost..innermost values
			// joined by sep. If the counter is not in scope, CSS Lists 3
			// §counter-functions resolves it as if `counter-reset: <name> 0`
			// were set at the root, so GetCounterValues returns []int{0}
			// (LOU-144). The result is "0" formatted via cv.Style.
			vals := b.counterCtx.GetCounterValues(pseudoNode, cv.Value, false)
			pendingText.WriteString(formatCounterValues(vals, cv.Separator, cv.Style))
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

// resolveContentText resolves CSS content values (text, counter, counters)
// to a plain string. Used for ::marker content resolution during layout
// tree building. `node` is the marker-bearing element (used to look up
// the list-item DOM-sibling fallback until Phase 3 makes list-item a
// real counter). `markerNode` is the pseudo-element node currently in
// scope for counter lookups (so the marker reads its own scope, not the
// real element's).
func (b *LayoutTreeBuilder) resolveContentText(contentVals []css.ContentValue, node *html.Node, markerNode *html.Node) string {
	var buf strings.Builder
	if markerNode == nil {
		markerNode = node
	}
	for _, cv := range contentVals {
		switch cv.Type {
		case "text":
			buf.WriteString(cv.Value)
		case "counter":
			// Phase 3 TODO: plumb list-item into the counter context. Until
			// then, list-item uses DOM-sibling counting (matches paint-time
			// computeListItemIndex). Check first — GetCounterValues now
			// returns []int{0} for any counter not in scope (LOU-144), which
			// would otherwise mask the fallback.
			var vals []int
			if cv.Value == "list-item" {
				vals = []int{b.getListItemCounterValue(node)}
			} else {
				vals = b.counterCtx.GetCounterValues(markerNode, cv.Value, true)
			}
			buf.WriteString(formatCounterValues(vals, "", cv.Style))
		case "counters":
			var vals []int
			if cv.Value == "list-item" {
				vals = []int{b.getListItemCounterValue(node)}
			} else {
				vals = b.counterCtx.GetCounterValues(markerNode, cv.Value, false)
			}
			buf.WriteString(formatCounterValues(vals, cv.Separator, cv.Style))
		}
	}
	return buf.String()
}

// formatCounterValues renders a slice of counter values (outermost-first
// for counters(), single-entry for counter()) using the given separator
// and CSS counter style. Phase 1 implements decimal and
// decimal-leading-zero only — those are the styles exercised by the B1
// test bucket. Phase 5 will route through CounterStyle for the full
// predefined / @counter-style set.
func formatCounterValues(values []int, sep, style string) string {
	if len(values) == 0 {
		return ""
	}
	if sep == "" && len(values) > 1 {
		// counter() with the inner counter that returned multiple
		// (shouldn't happen — onlyLast=true returns at most one), but
		// guard anyway.
		values = values[len(values)-1:]
	}
	var b strings.Builder
	for i, v := range values {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(formatCounterValue(v, style))
	}
	return b.String()
}

// formatCounterValue renders a single counter integer according to the
// CSS counter style identifier. Unknown styles fall through to decimal,
// matching CSS Lists 3 §counter-style-fallback (the predefined style
// table is delivered in Phase 5).
func formatCounterValue(v int, style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "decimal-leading-zero":
		if v >= 0 && v < 10 {
			return "0" + strconv.Itoa(v)
		}
		if v <= -1 && v > -10 {
			return "-0" + strconv.Itoa(-v)
		}
		return strconv.Itoa(v)
	case "lower-alpha", "lower-latin":
		return css.ToAlpha(v)
	case "upper-alpha", "upper-latin":
		return strings.ToUpper(css.ToAlpha(v))
	case "lower-roman":
		return strings.ToLower(css.ToRoman(v))
	case "upper-roman":
		return css.ToRoman(v)
	case "lower-greek":
		return css.ToGreek(v)
	default:
		return strconv.Itoa(v)
	}
}

// getListItemCounterValue returns the counter value for a list item by
// counting preceding list-item sibling elements in the DOM tree. Mirrors
// the paint-time computeListItemIndex approach. Does NOT use the CSS
// counter stack (b.counters) — that stack is managed independently for
// counter() values in content and must not be modified by marker resolution.
func (b *LayoutTreeBuilder) getListItemCounterValue(node *html.Node) int {
	if node == nil || node.Parent == nil {
		return 1
	}
	start := 1
	if val, ok := node.Parent.GetAttribute("start"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			start = n
		}
	}
	idx := start
	for _, sibling := range node.Parent.Children {
		if sibling == node {
			break
		}
		if s := b.styles[sibling]; s != nil && s.GetDisplay() == css.DisplayListItem {
			idx++
		}
	}
	return idx
}

// createMarkerPseudoElement creates a LayoutInputNode for a ::marker
// pseudo-element when the element is display:list-item with
// list-style-position: inside. Returns nil if no marker should be generated.
//
// Mirrors Blink's LayoutInsideListMarker creation in LayoutObject::CreateObject
// and MarkerText resolution in ListMarker::MarkerText.
func (b *LayoutTreeBuilder) createMarkerPseudoElement(node *html.Node, style *css.Style) *LayoutInputNode {
	if node == nil || style == nil {
		return nil
	}

	// Guard: only for display:list-item with list-style-position: inside.
	if style.GetDisplay() != css.DisplayListItem {
		return nil
	}
	if style.GetListStylePosition() != "inside" {
		return nil
	}

	// Step 1: Resolve marker content.
	var markerContent string
	var markerStyle *css.Style
	hasMarkerStyle := false
	hasContentProperty := false
	var markerContentValues []css.ContentValue

	if len(b.stylesheets) > 0 && css.HasPseudoElementRules(node, "marker", b.stylesheets, b.viewportWidth, b.viewportHeight) {
		// Case 2a: ::marker rules exist — compute ::marker style and extract content.
		markerStyle = css.ComputePseudoElementStyle(
			node, "marker", b.stylesheets,
			b.viewportWidth, b.viewportHeight, style,
		)
		hasMarkerStyle = true

		// CSS Pseudo-4 §3: UA default for ::marker is unicode-bidi: isolate.
		if _, hasBidi := markerStyle.Get("unicode-bidi"); !hasBidi {
			clone := markerStyle.Clone()
			clone.Set("unicode-bidi", "isolate")
			markerStyle = clone
		}

		// Capture the content values; the actual resolution happens
		// after we've entered the marker's counter scope below so
		// counter() reads use the right origin node.
		if cv, ok := markerStyle.GetContentValues(); ok {
			hasContentProperty = true
			markerContentValues = cv
		}
	}

	// Step 2: Compute the effective marker style if no ::marker rules.
	if !hasMarkerStyle {
		// No ::marker rules; create a default marker style inheriting from
		// parent with unicode-bidi: isolate (CSS Pseudo-4 §3) and display: inline.
		markerStyle = style.Clone()
		markerStyle.Set("unicode-bidi", "isolate")
		markerStyle.Set("display", "inline")
	}

	// Step 3: Create a synthetic ::marker DOM node so we can use it as
	// the origin for the counter scope.
	markerNode := &html.Node{
		Type:    html.ElementNode,
		TagName: "::marker",
		Parent:  node,
	}

	// Store the marker style in the styles map.
	b.styles[markerNode] = markerStyle

	// Enter the marker's counter scope before resolving its content.
	// Per CSS Pseudo-4 pseudo-element ordering the ::marker comes
	// before ::before, before children, before ::after — and Blink
	// runs EnterObject for the marker as part of that traversal. We
	// pair this with LeaveObject before returning.
	b.counterCtx.EnterObject(markerNode, markerStyle)
	defer b.counterCtx.LeaveObject(markerNode, markerStyle)

	// Now that the marker's counter scope is live, resolve content.
	if len(markerContentValues) > 0 {
		markerContent = b.resolveContentText(markerContentValues, node, markerNode)
	}

	// Case 2b: If no ::marker content resolved, fall back to list-style-type.
	// Skip fallback when ::marker explicitly set the content property (e.g. content:none).
	if markerContent == "" && !hasContentProperty {
		lst := style.GetListStyleType()
		if lst == css.ListStyleTypeNone {
			return nil
		}
		if lst != "" {
			if isBuiltinListStyleType(lst) {
				markerContent = b.resolveListStyleType(lst, node)
			} else {
				// Custom <string> value (e.g., list-style-type: "§").
				s := string(lst)
				// Strip surrounding quotes if present.
				if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
					s = s[1 : len(s)-1]
				}
				markerContent = s
			}
		}
	}

	// If no content was resolved, no marker to generate.
	if markerContent == "" {
		return nil
	}

	// Step 4: Create a text node child with the resolved marker content.
	textNode := &html.Node{
		Type:   html.TextNode,
		Text:   markerContent,
		Parent: markerNode,
	}

	// Step 5: Return the LayoutInputNode with the marker and its text child.
	return &LayoutInputNode{
		DOMNode: markerNode,
		style:   markerStyle,
		children: []*LayoutInputNode{{
			DOMNode: textNode,
			style:   markerStyle,
		}},
	}
}

// resolveListStyleType converts a built-in list-style-type value to its
// string representation with appropriate suffix, matching the paint-time
// formatListMarker output.
func (b *LayoutTreeBuilder) resolveListStyleType(lst css.ListStyleType, node *html.Node) string {
	if lst == css.ListStyleTypeNone {
		return ""
	}

	// Get the counter value (1-based).
	value := b.getListItemCounterValue(node)

	switch lst {
	case css.ListStyleTypeDisc:
		return "•" // bullet
	case css.ListStyleTypeCircle:
		return "◦" // white bullet
	case css.ListStyleTypeSquare:
		return "▪" // black small square
	case css.ListStyleTypeDecimal:
		return strconv.Itoa(value) + "."
	case css.ListStyleTypeDecimalLeadingZero:
		return fmt.Sprintf("%02d.", value)
	case css.ListStyleTypeLowerAlpha, css.ListStyleTypeLowerLatin:
		return css.ToAlpha(value) + "."
	case css.ListStyleTypeUpperAlpha, css.ListStyleTypeUpperLatin:
		return strings.ToUpper(css.ToAlpha(value)) + "."
	case css.ListStyleTypeLowerRoman:
		return strings.ToLower(css.ToRoman(value)) + "."
	case css.ListStyleTypeUpperRoman:
		return css.ToRoman(value) + "."
	case css.ListStyleTypeLowerGreek:
		return css.ToGreek(value) + "."
	case css.ListStyleTypeDisclosureOpen:
		return "▼" // ▼ down-pointing triangle (details expanded)
	case css.ListStyleTypeDisclosureClosed:
		return "▶" // ▶ right-pointing triangle (details collapsed)
	default:
		return ""
	}
}

// isBuiltinListStyleType returns true for the predefined list-style-type values.
// (Moved from inline_item.go — kept here for use by createMarkerPseudoElement.)
func isBuiltinListStyleType(lst css.ListStyleType) bool {
	switch lst {
	case css.ListStyleTypeDisc, css.ListStyleTypeCircle, css.ListStyleTypeSquare,
		css.ListStyleTypeDecimal, css.ListStyleTypeNone,
		css.ListStyleTypeDecimalLeadingZero,
		css.ListStyleTypeLowerAlpha, css.ListStyleTypeUpperAlpha,
		css.ListStyleTypeLowerLatin, css.ListStyleTypeUpperLatin,
		css.ListStyleTypeLowerRoman, css.ListStyleTypeUpperRoman,
		css.ListStyleTypeLowerGreek,
		css.ListStyleTypeDisclosureOpen, css.ListStyleTypeDisclosureClosed:
		return true
	}
	return false
}

// isBlockContainer returns true for display types that are block containers
// (can have block-level or inline-level children that trigger anonymous blocks).
func isBlockContainer(d css.DisplayType) bool {
	switch d {
	case css.DisplayBlock, css.DisplayListItem, css.DisplayFlowRoot,
		css.DisplayInlineBlock,
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
		// CSS 2.1 §9.2.1.1: empty/whitespace-only continuations are suppressed
		// for non-positioned inlines. Positioned inlines (relative/sticky)
		// emit an empty LEADING continuation when the inline starts with a
		// block child AND has later in-flow inline content, so the inline
		// containing block (CSS Position 3 §def-cb) spans from the span's
		// start position rather than from the first text after the block.
		// Without this, abspos descendants that anchor to the CB's start
		// would land on the post-block segment instead of the pre-block
		// (empty) segment. When the inline has ONLY block children and no
		// trailing inline content, fall through to the !hasCont &&
		// len(blockParts)>0 blockified-wrapper path below, which preserves
		// the span's stacking-context/visual-properties around the blocks.
		isPositionedInline := child.Style() != nil &&
			child.Style().GetPosition() != css.PositionStatic
		hasTrailingInlineContent := false
		if isPositionedInline {
			seenFirstBlock := false
			for _, gc := range child.Children() {
				if isBlockLevel(gc) {
					seenFirstBlock = true
					continue
				}
				if seenFirstBlock && !isWhitespaceOnly([]*LayoutInputNode{gc}) {
					hasTrailingInlineContent = true
					break
				}
			}
		}
		var segment []*LayoutInputNode
		var blockParts []*LayoutInputNode
		// continuations tracks all continuation nodes in order so we can
		// mark the first and last after collecting them all.
		var continuations []*LayoutInputNode
		hasCont := false
		for _, grandchild := range child.Children() {
			if isBlockLevel(grandchild) {
				// Flush the inline segment as a continuation (skip if whitespace-only,
				// unless this is an empty LEADING segment for a positioned inline).
				hasSegment := len(segment) > 0 && !isWhitespaceOnly(segment)
				emitEmptyLeading := isPositionedInline && hasTrailingInlineContent && !hasCont && !hasSegment
				if hasSegment || emitEmptyLeading {
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

// isFirstLetterPunctuation reports whether r is a punctuation character that
// should be included in the ::first-letter pseudo-element per CSS Pseudo-4 §3.2.
// Mirrors Blink's IsPunctuationForFirstLetter (Ps/Pe/Pi/Pf/Po). Go's
// unicode.IsPunct also returns true for Pd (Punctuation Dash) and Pc
// (Punctuation Connector), which matches the broader spec wording "P*" and
// is consistent with WPT tests such as first-letter-punctuation-and-space
// (leading em-dash). Blink reference: third_party/blink/renderer/core/dom/
// first_letter_pseudo_element.cc @ Chromium 4883d11fef.
func isFirstLetterPunctuation(r rune) bool {
	return unicode.IsPunct(r)
}

// isFirstLetterSpace reports whether r is whitespace skipped by the
// ::first-letter algorithm. Mirrors Blink's IsSpaceForFirstLetter (ASCII
// space/tab/newline/CR + U+00A0 NO-BREAK SPACE).
func isFirstLetterSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', ' ':
		return true
	}
	return false
}

// firstLetterGraphemeLen returns the byte length of the grapheme cluster
// starting at text[0]: one base rune followed by zero or more combining marks
// (Unicode category M*). This is the minimal cluster definition used by
// Blink's LengthOfGraphemeCluster for the ::first-letter algorithm — enough
// to keep base + combining marks together in tests like first-letter-004.
func firstLetterGraphemeLen(text string) int {
	if len(text) == 0 {
		return 0
	}
	_, n := utf8.DecodeRuneInString(text)
	for n < len(text) {
		r, sz := utf8.DecodeRuneInString(text[n:])
		if !unicode.IsMark(r) {
			break
		}
		n += sz
	}
	return n
}

// firstLetterLength returns the byte length of the leading typographic letter
// unit in text per CSS Pseudo-4 §3.2 — leading whitespace, then optional
// leading punctuation grapheme clusters, then one non-punctuation grapheme
// cluster (the letter), then trailing punctuation grapheme clusters. Returns
// (skipped, length) where skipped is the byte count of leading whitespace and
// length is the byte count of the first-letter unit starting at skipped.
// Returns (0, 0) if no first-letter unit is found in text.
//
// Mirrors Blink's FirstLetterPseudoElement::FirstLetterLength at Chromium
// 4883d11fef. The cross-text-node punctuation accumulation (Punctuation::kSeen)
// is omitted: we operate on one text node at a time per the louis14 layout
// builder.
func firstLetterLength(text string) (skipped, length int) {
	// Skip leading whitespace.
	for skipped < len(text) {
		r, sz := utf8.DecodeRuneInString(text[skipped:])
		if !isFirstLetterSpace(r) {
			break
		}
		skipped += sz
	}
	if skipped == len(text) {
		return 0, 0
	}

	// Consume leading punctuation grapheme clusters.
	pos := skipped
	for pos < len(text) {
		r, _ := utf8.DecodeRuneInString(text[pos:])
		if !isFirstLetterPunctuation(r) {
			break
		}
		pos += firstLetterGraphemeLen(text[pos:])
	}

	// If only punctuation was found, this text node has no first-letter unit.
	// Blink would set Punctuation::kSeen and continue scanning the next text
	// node; we don't model that yet, so abort.
	if pos == len(text) {
		return 0, 0
	}

	// After leading punctuation, whitespace breaks the first-letter unit.
	r, _ := utf8.DecodeRuneInString(text[pos:])
	if isFirstLetterSpace(r) {
		return 0, 0
	}

	// Consume one grapheme cluster as the letter.
	pos += firstLetterGraphemeLen(text[pos:])

	// Consume trailing punctuation grapheme clusters.
	for pos < len(text) {
		r, _ := utf8.DecodeRuneInString(text[pos:])
		if !isFirstLetterPunctuation(r) {
			break
		}
		pos += firstLetterGraphemeLen(text[pos:])
	}

	return skipped, pos - skipped
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
		idx, letterLen := firstLetterLength(text)
		if letterLen == 0 {
			continue
		}
		letterEnd := idx + letterLen

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

// (Phase 1, LOU-css-lists): the ad-hoc counter-reset / counter-increment
// stack handling (processCounterReset / processCounterIncrement /
// getCounterValue / b.counters) has been replaced by the Blink-faithful
// css.CountersAttachmentContext threaded through buildNode. See
// pkg/css/counters_attachment_context.go.

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
