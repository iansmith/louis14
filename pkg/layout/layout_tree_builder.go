package layout

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
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

	// counterStyleMap lazily indexes @counter-style rules by name for
	// ::marker content resolution (counterStyleRules).
	counterStyleMap map[string]css.CounterStyleRule

	// fontConfig resolves font-family → font path for measuring the scaled
	// initial-letter font (CSS Inline Layout 3 §7.5.1). Zero-value is safe:
	// initial-letter sizing falls back to the cap-unit heuristic when no
	// usable font path / measured metric is available.
	fontConfig text.FontConfig

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
		// For reversed(name) counter-resets without an explicit integer,
		// compute the auto-count initial value before EnterObject so the
		// counter context can use it. Mirrors Blink CounterNode::CountUsage()
		// in counter_node.cc @ 4883d11fef.
		b.precomputeReversedCounterInitials(node, style)
		b.counterCtx.EnterObject(node, style)
		defer b.counterCtx.LeaveObject(node, style)

		// CSS Lists 3 §6.1 implicit list-item decrement for reversed counters.
		// When the innermost list-item counter is reversed and this element is
		// display:list-item, apply an implicit counter-increment: list-item -1
		// UNLESS:
		//   (a) the element has an explicit counter-increment on list-item
		//       (Blink DetermineCounterTypeAndValue: explicit replaces implicit), OR
		//   (b) the element has counter-reset: reversed(list-item) WITHOUT an
		//       explicit integer (auto-count form). In that case, computeReversedCounterInitial
		//       computed initial = -(sum + last) where "last" already encodes the -1
		//       that this element WOULD contribute as a list-item. The auto-initial
		//       is chosen so the element reads the right value directly; applying -1
		//       again would double-count it. Counters with an explicit integer initial
		//       (e.g. reversed(list-item) 10) DO get the -1 applied.
		//
		// The implicit decrement is applied via UpdateCounterValue so it mutates
		// the same entry that EnterObject just created/updated.
		disp := style.GetDisplay()
		if disp == css.DisplayListItem || disp == css.DisplayInlineListItem {
			b.applyImplicitListItemStep(node, style)
		}

		if style.ShouldApplyStyleContainment() {
			b.quoteDepthSaved = append(b.quoteDepthSaved, b.quoteDepth)
			defer func() {
				n := len(b.quoteDepthSaved)
				b.quoteDepth = b.quoteDepthSaved[n-1]
				b.quoteDepthSaved = b.quoteDepthSaved[:n-1]
			}()
		}
	}

	// Build layout children, filtering out display:none and non-layout nodes.
	var rawChildren []*LayoutInputNode

	// CSS Pseudo-4 §4.2: generate the ::marker pseudo-element for every
	// list item. An inside marker is inserted as the first in-flow child
	// (before ::before, per CSS Pseudo-4 ordering: marker, before,
	// children, after). An outside marker is NOT an in-flow child — it is
	// reachable only through lin.markerNode / ListMarkerBlockNodeIfListItem,
	// and the block layout algorithm's UnpositionedListMarker carry/claim
	// protocol owns its placement (Blink BlockNode::
	// ListMarkerBlockNodeIfListItem feeding BlockLayoutAlgorithm,
	// bla.cc:319-327 @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	if style != nil && style.IsListItemDisplay() {
		if markerNode := b.createMarkerPseudoElement(node, style); markerNode != nil {
			lin.markerNode = markerNode
			if !markerNode.MarkerIsOutside {
				rawChildren = append(rawChildren, markerNode)
			}
		}
	}

	// CSS 2.1 §12.1: Generate ::before pseudo-element.
	if beforeNode := b.createPseudoElement(node, style, "before"); beforeNode != nil {
		rawChildren = append(rawChildren, unboxPseudoIfContents(beforeNode)...)
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
			// ::before/::after on the contents element are also generated;
			// see expandContentsChildren. Mirrors Blink's
			// LayoutObjectIsNeeded() returning false for display:contents
			// (third_party/blink/renderer/core/dom/element.cc @
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
			if childStyle.GetDisplay() == css.DisplayContents {
				// CSS Display 3 §3.1 "Unboxing in HTML": replaced /
				// form-control / SVG-only elements ignore
				// display:contents and behave as display:none in
				// louis14's element model.
				if displayContentsBehavesAsNone(child.TagName) {
					continue
				}
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
		rawChildren = append(rawChildren, unboxPseudoIfContents(afterNode)...)
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

	// CSS Pseudo 4 §3.2: a ::first-line pseudo-element on an ancestor block
	// applies to the first formatted line of that block, even when the actual
	// inline content lives inside descendant in-flow blocks. Propagate the
	// stored FirstLineStyle DOWN the first-in-flow-block chain so the
	// innermost block that does inline layout sees it on its own
	// LayoutInputNode. Mirrors Blink's FirstLineStyleIterator
	// (`core/css/first_line_style_iterator.cc` @ 4883d11fef) which walks the
	// ancestor chain at paint time; louis14 pushes the equivalent state down
	// at build time so inline_layout.applyFirstLineStyles can keep operating
	// on the bla.node it already has.
	if lin.FirstLineStyle != nil {
		b.propagateFirstLineToFirstInFlowBlock(lin)
	}

	return lin
}

// propagateFirstLineToFirstInFlowBlock copies parent.FirstLineStyle to the
// first in-flow block-level descendant on the chain, recursively, until we
// reach a block that itself contains inline content. Out-of-flow / floated
// children are skipped (they don't host the first formatted line of the
// ancestor). Stops descending if a descendant already has its own
// FirstLineStyle (more specific match wins via the cascade).
func (b *LayoutTreeBuilder) propagateFirstLineToFirstInFlowBlock(parent *LayoutInputNode) {
	if parent == nil || parent.FirstLineStyle == nil {
		return
	}
	for _, child := range parent.children {
		if child == nil {
			continue
		}
		// Skip out-of-flow / floats — they don't host the first formatted
		// line of the ancestor's block-flow.
		if isOutOfFlowOrFloat(child) {
			continue
		}
		// Whitespace-only text between block siblings is normally collapsed
		// out before paint (CSS 2.1 §9.2.2.1). It does not consume the first
		// formatted line. Skip it for first-line propagation so we keep
		// walking to the actual first in-flow block child.
		if child.IsText() {
			if strings.TrimSpace(child.TextContent()) == "" {
				continue
			}
			// Non-whitespace inline content lives directly in this block:
			// the first line is this block's responsibility, no further
			// propagation needed.
			return
		}
		s := child.Style()
		if s == nil {
			continue
		}
		if !isBlockLevel(child) {
			// Inline-level element in this block-flow: the first line is
			// in this block container's own inline-layout pass.
			return
		}
		// First in-flow block-level descendant. Propagate if it has no
		// FirstLineStyle of its own (cascade specificity: a descendant's
		// own ::first-line rules win and we never overwrite them).
		if child.FirstLineStyle == nil {
			child.FirstLineStyle = parent.FirstLineStyle
			b.propagateFirstLineToFirstInFlowBlock(child)
		}
		// Only the FIRST in-flow block child can carry the ancestor's
		// first formatted line.
		return
	}
}

// unboxPseudoIfContents implements CSS Display 3 §3 for pseudo-elements:
// if a generated ::before / ::after pseudo has computed `display: contents`,
// its own box is suppressed and its content participates in layout as if
// directly attached to the originating element's effective parent. The
// children have already been resolved against the pseudo's style by
// createPseudoElement, so we can simply expose them.
//
// Tested by display-contents-before-after-002.html (the pseudo has
// `display: contents; border: 100px solid red`; the border must NOT be
// rendered) and display-contents-before-after-003.html (flex item
// membership of the pseudo's text content).
func unboxPseudoIfContents(pseudo *LayoutInputNode) []*LayoutInputNode {
	if pseudo == nil {
		return nil
	}
	if pseudo.style != nil && pseudo.style.GetDisplay() == css.DisplayContents {
		return pseudo.children
	}
	return []*LayoutInputNode{pseudo}
}

// displayContentsBehavesAsNone reports whether an HTML element with
// `display: contents` should be treated as `display: none` instead of
// unboxing. Per CSS Display 3 §3.1 "Effect on the box tree" + the
// "Unboxing in HTML" appendix, certain replaced / form-control / SVG
// elements ignore `display: contents`; in louis14 (which does not yet
// host the underlying element-specific renderers) the simplest faithful
// behavior is to treat the contents value as `display: none` so neither
// the element nor its children produce boxes.
//
// The list mirrors Blink at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
// — see `LayoutTreeBuilderTraversal::DisplayContentsAffectsAncestors`
// callers in `core/html/html_element.cc` and
// `core/dom/element.cc::LayoutObjectIsNeeded`. Elements that DO unbox
// (and therefore are NOT in this list) include `<button>`, `<details>`,
// `<summary>`, `<fieldset>`, `<legend>` — these are tested by
// display-contents-button.html, -details.html, -fieldset*.html.
func displayContentsBehavesAsNone(tagName string) bool {
	switch tagName {
	case "br", "wbr",
		"meter", "progress",
		"canvas",
		"embed", "object",
		"audio", "video",
		"iframe", "frame", "frameset",
		"img",
		"input", "textarea", "select",
		"applet":
		return true
	}
	return false
}

// expandContentsChildren returns the layout-tree children produced by a
// `display: contents` element. Per CSS Display 3 §3.2 the contents element
// itself generates no box; its DOM children participate in layout as if
// they were direct children of the contents element's parent. Its
// `::before` and `::after` pseudo-elements DO generate boxes, and they
// appear as the contents element's first and last children in the
// effective layout tree (CSS Display 3 §3 — the contents element is
// "replaced in the box tree by its contents (including pseudo-elements
// such as `::before` and `::after`)").
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
// Counter / quote scopes: the contents element participates in the
// counter scope tree (the CountersAttachmentContext tracks it as a
// "display:contents" node so RemoveStaleCounters can walk through it,
// see pkg/css/counters_attachment_context.go). We Enter/Leave the
// scope around its expansion so any `counter-reset` / `counter-increment`
// on the contents element is honored by its ::before/::after content().
//
// Mirrors Blink's behavior in `LayoutTreeBuilderForElement` where a
// display:contents element is not given a LayoutObject; its children
// (and its pseudo-elements) are attached to the grandparent. See
// third_party/blink/renderer/core/dom/layout_tree_builder.cc and
// third_party/blink/renderer/core/dom/element.cc::LayoutObjectIsNeeded
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func (b *LayoutTreeBuilder) expandContentsChildren(contents *html.Node) []*LayoutInputNode {
	contentsStyle := b.styles[contents]
	var out []*LayoutInputNode

	// Enter the contents element's counter scope before expanding so any
	// counter-reset / counter-increment it declares is visible to its
	// pseudo-elements' content() reads. The scope is left after expansion.
	if contentsStyle != nil {
		b.counterCtx.EnterObject(contents, contentsStyle)
		defer b.counterCtx.LeaveObject(contents, contentsStyle)
	}

	// CSS 2.1 §12.1 / CSS Display 3 §3: generate ::before for the contents
	// element. The pseudo-element belongs to the contents element and lives
	// at the contents element's slot in the effective box tree.
	if beforeNode := b.createPseudoElement(contents, contentsStyle, "before"); beforeNode != nil {
		out = append(out, unboxPseudoIfContents(beforeNode)...)
	}

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
				// CSS Display 3 §3.1 "Unboxing in HTML": certain HTML
				// elements (replaced + most form controls + SVG-only)
				// ignore display:contents and behave as display:none.
				if displayContentsBehavesAsNone(child.TagName) {
					continue
				}
				out = append(out, b.expandContentsChildren(child)...)
				continue
			}
			out = append(out, b.buildNode(child))
		default:
			// Skip comment nodes, doctype, etc.
			continue
		}
	}

	// CSS 2.1 §12.1: generate ::after for the contents element.
	if afterNode := b.createPseudoElement(contents, contentsStyle, "after"); afterNode != nil {
		out = append(out, unboxPseudoIfContents(afterNode)...)
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
	if !isBlockContainerStyle(parentStyle) {
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

	// A display:list-item pseudo-element is a real list item: it takes the
	// implicit list-item counter increment and generates its own ::marker
	// (CSS Lists 3 §declaring-a-list-item; WPT nested-marker.html). Its
	// marker is a nested pseudo-element NOT addressable by author ::marker
	// selectors, so marker creation skips author-rule matching.
	var pseudoMarker *LayoutInputNode
	if pseudoStyle != nil && pseudoStyle.IsListItemDisplay() {
		b.applyImplicitListItemStep(pseudoNode, pseudoStyle)
		pseudoMarker = b.createMarkerPseudoElement(pseudoNode, pseudoStyle)
	}

	// Build child nodes from content values. The pseudo's own computed
	// `quotes` wins (e.g. `::before { quotes: "«" "»" }`); when the pseudo
	// computation carries none, the originating element's inherited value
	// applies — same precedence as ::marker content resolution.
	quotes := b.quotesFor(quoteSourceStyle(pseudoStyle, parentStyle))

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
			children = append(children, b.createSyntheticImage(cv.Value, pseudoNode))
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
			// CSS Values 5 §11.5: read the named attribute; if absent use Fallback.
			// Legacy CSS2.1 form: attr(name) → empty string if absent.
			// New form: attr(name, fallback) → cv.Fallback if absent.
			attrVal, attrPresent := "", false
			if node.Attributes != nil {
				attrVal, attrPresent = node.Attributes[cv.Value]
			}
			if attrPresent {
				pendingText.WriteString(attrVal)
			} else if cv.Fallback != "" {
				pendingText.WriteString(cv.Fallback)
			}
		case "open-quote":
			pendingText.WriteString(b.emitOpenQuote(quotes))
		case "close-quote":
			pendingText.WriteString(b.emitCloseQuote(quotes))
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
	// Attach the list-item pseudo's own ::marker: inside markers become the
	// first in-flow child; outside markers ride the carry/claim protocol
	// via lin.markerNode (same split as buildNode for real list items).
	if pseudoMarker != nil {
		lin.markerNode = pseudoMarker
		if !pseudoMarker.MarkerIsOutside {
			lin.children = append([]*LayoutInputNode{pseudoMarker}, lin.children...)
		}
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
// createSyntheticImage builds the synthetic inline <img> LayoutInputNode
// used for generated image content: ::before/::after content:url() and
// list-style-image markers.
func (b *LayoutTreeBuilder) createSyntheticImage(src string, parent *html.Node) *LayoutInputNode {
	imgNode := &html.Node{
		Type:       html.ElementNode,
		TagName:    "img",
		Attributes: map[string]string{"src": src},
		Parent:     parent,
	}
	imgStyle := css.NewStyle()
	imgStyle.Set("display", "inline")
	imgStyle.ViewportWidth = b.viewportWidth
	imgStyle.ViewportHeight = b.viewportHeight
	b.styles[imgNode] = imgStyle
	return &LayoutInputNode{DOMNode: imgNode, style: imgStyle}
}

// quoteSourceStyle picks the style whose `quotes` value governs generated
// content: the pseudo-element's (or marker's) own computed value wins; when
// it carries none, the originating element's inherited value applies.
func quoteSourceStyle(own, fallback *css.Style) *css.Style {
	if own != nil {
		if _, ok := own.Get("quotes"); ok {
			return own
		}
	}
	return fallback
}

// quotesFor returns the quote-pair strings governing open-quote/close-quote
// for the given style (UA defaults when nil or unset).
func (b *LayoutTreeBuilder) quotesFor(style *css.Style) []string {
	if style != nil {
		if q, ok := style.Get("quotes"); ok {
			return b.parseQuotes(q)
		}
	}
	return []string{"\"", "\"", "'", "'"}
}

// emitOpenQuote / emitCloseQuote step the builder's shared quote-nesting
// depth and return the quote string to append (possibly empty when the
// depth exceeds the available pairs). One state machine shared by
// ::before/::after content and ::marker content resolution.
func (b *LayoutTreeBuilder) emitOpenQuote(quotes []string) string {
	idx := b.quoteDepth * 2
	b.quoteDepth++
	if idx < len(quotes) {
		return quotes[idx]
	}
	return ""
}

func (b *LayoutTreeBuilder) emitCloseQuote(quotes []string) string {
	if b.quoteDepth > 0 {
		b.quoteDepth--
	}
	if idx := b.quoteDepth*2 + 1; idx < len(quotes) {
		return quotes[idx]
	}
	return ""
}

// resolveContentText resolves content values to a plain string; style is the
// one whose `quotes` property governs open-quote/close-quote values (CSS
// Lists 3 §marker-properties: quotes applies in ::marker;
// marker-quotes.html). A nil style keeps the UA default quote pairs.
func (b *LayoutTreeBuilder) resolveContentText(contentVals []css.ContentValue, node *html.Node, markerNode *html.Node, style *css.Style) string {
	var buf strings.Builder
	if markerNode == nil {
		markerNode = node
	}
	quotes := b.quotesFor(style)
	for _, cv := range contentVals {
		switch cv.Type {
		case "text":
			buf.WriteString(cv.Value)
		case "open-quote":
			buf.WriteString(b.emitOpenQuote(quotes))
		case "close-quote":
			buf.WriteString(b.emitCloseQuote(quotes))
		case "counter":
			// Prefer the counter context when list-item has been explicitly
			// established (e.g. counter-reset: reversed(list-item) or
			// counter-reset: list-item N). Fall back to DOM-sibling counting
			// only when list-item is not in scope in the context, so that
			// normal (non-reversed, no explicit reset) lists still produce
			// correct values.
			var vals []int
			if cv.Value == "list-item" && !b.counterCtx.IsCounterInScope(markerNode, "list-item") {
				vals = []int{b.getListItemCounterValue(node)}
			} else {
				vals = b.counterCtx.GetCounterValues(markerNode, cv.Value, true)
			}
			buf.WriteString(formatCounterValues(vals, "", cv.Style))
		case "counters":
			var vals []int
			if cv.Value == "list-item" && !b.counterCtx.IsCounterInScope(markerNode, "list-item") {
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
		// Every list-item display counts toward the ordinal — Blink's
		// ListItemOrdinal walks IsListItem() siblings, which covers inline
		// list items too (list_marker.cc:90-98 @ 4883d11f).
		if s := b.styles[sibling]; s != nil && s.IsListItemDisplay() {
			idx++
		}
	}
	return idx
}

// precomputeReversedCounterInitials inspects node's counter-reset for
// reversed(name) entries without an explicit integer, and for each one
// calls computeReversedCounterInitial to determine the auto-count initial
// value, storing it in the counter context via SetReversedCounterInitial.
// Must be called BEFORE counterCtx.EnterObject(node). Mirrors Blink's
// CounterNode::CountUsage() in counter_node.cc @ 4883d11fef.
func (b *LayoutTreeBuilder) precomputeReversedCounterInitials(node *html.Node, style *css.Style) {
	raw, ok := style.Get("counter-reset")
	if !ok {
		return
	}
	for _, tok := range css.ParseReversedCounterResets(raw) {
		initial := b.computeReversedCounterInitial(node, tok)
		b.counterCtx.SetReversedCounterInitial(node, tok, initial)
	}
}

// computeReversedCounterInitial scans the DOM scope of a reversed(name)
// counter-reset on resetNode and returns the auto-count initial value
// per CSS Lists 3 §4.1 "Instantiating Counters":
//
//	num = 0
//	lastNonZeroIncrementNegated = 0
//	for each el (in tree order) that increments or sets `name` in scope:
//	    incrementNegated = -el.counterIncrement(name)
//	                       or 1 if el is display:list-item and `name`
//	                       is list-item with no explicit counter-increment
//	    if incrementNegated != 0:
//	        lastNonZeroIncrementNegated = incrementNegated
//	    if el sets `name` with counter-set:
//	        num += el.counterSet(name)
//	        break
//	    num += incrementNegated
//	num += lastNonZeroIncrementNegated
//	return num
//
// The scope of resetNode is: resetNode itself (its explicit increments
// and ::before/::after pseudos, but NOT the implicit list-item decrement
// since that's applied to the resetNode at runtime via the auto-reversed
// path in buildNode), then its descendants (skipping subtrees that have
// a nested counter-reset for the same name), then its following siblings
// (with their descendants), stopping at a sibling counter-reset.
//
// Mirrors Blink CountersAttachmentContext::CalculateInitialValueForReversed
// in third_party/blink/renderer/core/css/counters_attachment_context.cc
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, with one louis14 divergence:
// display:list-item (and display:inline-list-item) on any element induces
// the implicit list-item increment for the initial-value scan (Blink uses
// IsA<HTMLLIElement> strictly; louis14 follows the spec letter so
// <span class="li"> style="display:list-item" is counted —
// see counter-reset-reversed-list-item.html).
func (b *LayoutTreeBuilder) computeReversedCounterInitial(resetNode *html.Node, name string) int {
	initialValue := 0
	lastNonZeroIncrementNegated := 0

	// processStyle accumulates one visit's contribution. Returns true if
	// the walk should stop (counter-set for `name` encountered).
	//
	// listItemImplicit indicates whether this style's element induces the
	// implicit list-item +1 (display:list-item + counter `list-item`).
	// Only the element-level visit passes true; pseudo-element styles do
	// not get the implicit list-item bump (Blink: pseudo-elements are not
	// HTMLLIElement; louis14 mirrors by passing false for pseudos).
	processStyle := func(st *css.Style, listItemImplicit bool) bool {
		if st == nil {
			return false
		}
		incNeg := 0
		if v, found := counterIncrementValue(st, name); found {
			incNeg = -v
		} else if name == "list-item" && listItemImplicit {
			// Implicit list-item bump for display:list-item elements
			// without an explicit counter-increment (CSS Lists 3 §6.1).
			incNeg = 1
		}
		if incNeg != 0 {
			lastNonZeroIncrementNegated = incNeg
		}
		if sv, found := counterSetValue(st, name); found {
			initialValue += sv
			return true
		}
		initialValue += incNeg
		return false
	}

	// listItemImplicitOf reports whether `st` induces the list-item
	// implicit increment (display:list-item or display:inline-list-item).
	listItemImplicitOf := func(st *css.Style) bool {
		if st == nil {
			return false
		}
		d := st.GetDisplay()
		return d == css.DisplayListItem || d == css.DisplayInlineListItem
	}

	// visit handles one descendant or sibling element in pre-order: the
	// element itself, then its ::before pseudo, then its DOM children
	// (recursive), then its ::after pseudo. A descendant counter-reset
	// for the same name terminates that branch. Returns true if a hard
	// stop (counter-set) was hit and the entire walk must end.
	var visit func(n *html.Node) bool
	visit = func(n *html.Node) bool {
		if n == nil || n.Type != html.ElementNode {
			return false
		}
		st := b.styles[n]

		// display:none elements generate no box and do not participate
		// in counter scoping (CSS Lists 3 §counters-without-boxes).
		if st != nil && st.GetDisplay() == css.DisplayNone {
			return false
		}

		// A nested counter-reset of the same name terminates this branch
		// (Blink: NextSkippingChildren on IsReset where parent differs).
		if hasCounterReset(st, name) {
			return false
		}

		// Visit the element itself.
		if processStyle(st, listItemImplicitOf(st)) {
			return true
		}

		// ::before pseudo-element (layout-tree child appearing before
		// real children).
		if ps := b.computePseudoStyleForScan(n, st, "before"); ps != nil {
			if processStyle(ps, false) {
				return true
			}
		}

		// Real DOM children, in document order.
		for _, child := range n.Children {
			if child.Type == html.ElementNode {
				if visit(child) {
					return true
				}
			}
		}

		// ::after pseudo-element.
		if ps := b.computePseudoStyleForScan(n, st, "after"); ps != nil {
			if processStyle(ps, false) {
				return true
			}
		}
		return false
	}

	// Phase 0: resetNode's OWN contribution. The CSS Lists 3 §counter-scope
	// rule says the scope "starts at the first element ... that
	// instantiates that counter" — so resetNode is in its own scope.
	// We process resetNode's explicit counter-increment AND counter-set
	// (counter-set on the resetNode itself stops the walk), but NOT the
	// implicit list-item bump (the runtime list-item decrement is applied
	// separately to the resetNode in buildNode and would double-count).
	resetSt := b.styles[resetNode]
	stopped := false
	if resetSt != nil {
		if v, found := counterIncrementValue(resetSt, name); found {
			incNeg := -v
			if incNeg != 0 {
				lastNonZeroIncrementNegated = incNeg
			}
			initialValue += incNeg
		}
		if sv, found := counterSetValue(resetSt, name); found {
			initialValue += sv
			stopped = true
		}
	}
	if !stopped {
		if ps := b.computePseudoStyleForScan(resetNode, resetSt, "before"); ps != nil {
			if processStyle(ps, false) {
				stopped = true
			}
		}
	}

	// Phase 1: resetNode's DOM descendants in document order.
	if !stopped {
		for _, child := range resetNode.Children {
			if child.Type == html.ElementNode {
				if visit(child) {
					stopped = true
					break
				}
			}
		}
	}

	// resetNode's ::after pseudo (after descendants, before following siblings).
	if !stopped {
		if ps := b.computePseudoStyleForScan(resetNode, resetSt, "after"); ps != nil {
			if processStyle(ps, false) {
				stopped = true
			}
		}
	}

	// Phase 2: resetNode's following siblings. A following sibling that
	// has a counter-reset for the same name at the SAME parent ends the
	// entire scope (per CSS Lists 3 §counter-scope: "it does not include
	// any elements in the scope of a counter with the same name created
	// by a counter-reset on a later sibling").
	if !stopped && resetNode.Parent != nil {
		seenReset := false
		for _, sib := range resetNode.Parent.Children {
			if sib == resetNode {
				seenReset = true
				continue
			}
			if !seenReset {
				continue
			}
			if sib.Type != html.ElementNode {
				continue
			}
			sibSt := b.styles[sib]
			if sibSt != nil && sibSt.GetDisplay() == css.DisplayNone {
				continue
			}
			if hasCounterReset(sibSt, name) {
				break
			}
			if visit(sib) {
				stopped = true
				break
			}
		}
	}

	// Final pass per the spec: add lastNonZeroIncrementNegated once.
	initialValue += lastNonZeroIncrementNegated
	return initialValue
}

// counterSetValue returns the set value for counter name from a style's
// counter-set property, and whether it was found. Mirrors Blink's
// CounterDirectives::SetValue accessor.
func counterSetValue(st *css.Style, name string) (int, bool) {
	if st == nil {
		return 0, false
	}
	raw, ok := st.Get("counter-set")
	if !ok {
		return 0, false
	}
	// counter-set default value is 0 when no integer follows the name
	// (CSS Lists 3 §propdef-counter-set).
	for _, tok := range tokenizeCounterSet(raw) {
		if tok.name == name {
			return tok.value, true
		}
	}
	return 0, false
}

// tokenizeCounterSet parses a counter-set value string into (name, value)
// pairs. Default value is 0 when no integer follows (CSS Lists 3
// §propdef-counter-set).
func tokenizeCounterSet(raw string) []counterNameValue {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "none" {
		return nil
	}
	parts := strings.Fields(raw)
	var out []counterNameValue
	for i := 0; i < len(parts); i++ {
		name := parts[i]
		val := 0
		if i+1 < len(parts) {
			if n, err := strconv.Atoi(parts[i+1]); err == nil {
				val = n
				i++
			}
		}
		out = append(out, counterNameValue{name: name, value: val})
	}
	return out
}

// computePseudoStyleForScan returns the computed pseudo-element style for
// n::pseudo if CSS rules match, or nil if no matching rules exist.
// Used by computeReversedCounterInitial to inspect pseudo-element
// directives during the reversed-counter scope scan.
func (b *LayoutTreeBuilder) computePseudoStyleForScan(n *html.Node, parentSt *css.Style, pseudo string) *css.Style {
	if len(b.stylesheets) == 0 || parentSt == nil {
		return nil
	}
	if !css.HasPseudoElementRules(n, pseudo, b.stylesheets, b.viewportWidth, b.viewportHeight) {
		return nil
	}
	return css.ComputePseudoElementStyle(n, pseudo, b.stylesheets, b.viewportWidth, b.viewportHeight, parentSt)
}

// counterIncrementValue returns the increment value for counter name
// from a style, and whether it was found.
func counterIncrementValue(st *css.Style, name string) (int, bool) {
	raw, ok := st.Get("counter-increment")
	if !ok {
		return 0, false
	}
	for _, tok := range tokenizeCounterIncrement(raw) {
		if tok.name == name {
			return tok.value, true
		}
	}
	return 0, false
}

// hasCounterReset reports whether style has a counter-reset for name
// (either reversed or non-reversed).
func hasCounterReset(st *css.Style, name string) bool {
	if st == nil {
		return false
	}
	raw, ok := st.Get("counter-reset")
	if !ok {
		return false
	}
	for _, tok := range tokenizeCounterResets(raw) {
		if tok == name {
			return true
		}
	}
	return false
}

// counterNameValue pairs a counter name with its integer value.
type counterNameValue struct {
	name  string
	value int
}

// tokenizeCounterIncrement parses a counter-increment value string into
// (name, value) pairs. Default value is 1 when no integer follows.
// Matches the tokenization in pkg/css.parseCounterPropertyList.
func tokenizeCounterIncrement(raw string) []counterNameValue {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "none" {
		return nil
	}
	parts := strings.Fields(raw)
	var out []counterNameValue
	for i := 0; i < len(parts); i++ {
		name := parts[i]
		val := 1
		if i+1 < len(parts) {
			if n, err := strconv.Atoi(parts[i+1]); err == nil {
				val = n
				i++
			}
		}
		out = append(out, counterNameValue{name: name, value: val})
	}
	return out
}

// tokenizeCounterResets parses a counter-reset value string and returns
// the counter names (stripping reversed() wrappers). Default value 0 is
// ignored — only the names matter for scope detection.
func tokenizeCounterResets(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "none" {
		return nil
	}
	parts := strings.Fields(raw)
	var out []string
	for i := 0; i < len(parts); i++ {
		tok := parts[i]
		// Strip reversed() wrapper.
		lower := strings.ToLower(tok)
		if strings.HasPrefix(lower, "reversed(") && strings.HasSuffix(tok, ")") {
			tok = strings.TrimSpace(tok[len("reversed(") : len(tok)-1])
		}
		// Skip integer tokens.
		if _, err := strconv.Atoi(tok); err == nil {
			continue
		}
		if tok != "" && tok != "none" {
			out = append(out, tok)
		}
		// Skip optional trailing integer.
		if i+1 < len(parts) {
			if _, err := strconv.Atoi(parts[i+1]); err == nil {
				i++
			}
		}
	}
	return out
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

	// Guard: every list-item display generates a ::marker box (Blink
	// ComputedStyle::IsDisplayListItem feeds ListMarker creation for both
	// LayoutListItem and LayoutInlineListItem, list_marker.cc:72-80 @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Placement — inside as the
	// first in-flow child vs outside via the carry/claim protocol — is
	// decided by MarkerShouldBeInside.
	if !style.IsListItemDisplay() {
		return nil
	}
	markerInside := style.MarkerShouldBeInside()

	// Step 1: Resolve marker content.
	var markerContent string
	var markerStyle *css.Style
	hasMarkerStyle := false
	hasContentProperty := false
	var markerContentValues []css.ContentValue

	// Markers of pseudo-element list items (::before/::after with
	// display:list-item) are nested pseudo-elements and are NOT addressable
	// by author ::marker selectors (css-pseudo-4; WPT nested-marker.html) —
	// they take UA defaults only.
	originIsPseudo := strings.HasPrefix(node.TagName, "::")
	if !originIsPseudo && len(b.stylesheets) > 0 && css.HasPseudoElementRules(node, "marker", b.stylesheets, b.viewportWidth, b.viewportHeight) {
		// Case 2a: ::marker rules exist — compute ::marker style and extract content.
		markerStyle = css.ComputePseudoElementStyle(
			node, "marker", b.stylesheets,
			b.viewportWidth, b.viewportHeight, style,
		)
		hasMarkerStyle = true

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
		// No ::marker rules; create a default marker style carrying ONLY
		// inherited properties from the list item (Blink
		// StyleResolver::CreateAnonymousStyleWithDisplay via
		// ListMarker::UpdateMarkerContentIfNeeded, list_marker.cc:320-323 —
		// a li's border/margin/padding must not leak onto its marker), then
		// stamp UA ::marker defaults on top (text-transform:none,
		// white-space:pre, tabular-nums, unicode-bidi:isolate) per CSS
		// Pseudo-4 §3 + Blink html.css
		// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		markerStyle = css.CreateAnonymousStyleWithDisplay(style, "inline")
		css.ApplyMarkerUADefaults(markerStyle)
	}

	// Marker display is adjusted by position, not authored: inside markers
	// are inline; outside markers become a block container that must not
	// break internally and honors trailing spaces. Mirrors Blink
	// StyleAdjuster::AdjustStyleForMarker (style_adjuster.cc:478-514 @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). markerStyle is freshly
	// built either way (ComputePseudoElementStyle or
	// CreateAnonymousStyleWithDisplay), so mutating it here is safe.
	if markerInside {
		markerStyle.Set("display", "inline")
	} else {
		markerStyle.Set("display", "inline-block")
		markerStyle.Set("white-space", "pre")
	}

	// Letter-spacing / word-spacing must not affect a predefined SYMBOL marker
	// (disc/circle/square/disclosure). Blink draws these as shapes, not text, so
	// a text-spacing property cannot touch them — verified against the WPT
	// css-pseudo/marker-letter-spacing refs, which render the disc plain (the
	// `::marker { letter-spacing }` has no effect on it), while the numeric /
	// string / content markers DO show spacing. louis14 renders the symbol as
	// the bullet glyph text "• "; without this it would pick up a spurious
	// letter-spacing unit on the suffix space. Real-text markers keep spacing.
	if !hasContentProperty && !generatesMarkerImage(style) &&
		isPredefinedSymbolListStyleType(style.GetListStyleType()) {
		markerStyle.Set("letter-spacing", "normal")
		markerStyle.Set("word-spacing", "normal")
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
		markerContent = b.resolveContentText(markerContentValues, node, markerNode, quoteSourceStyle(markerStyle, style))
	}

	// Image marker: list-style-image takes precedence over list-style-type
	// (including `none`). Blink ListMarker::UpdateMarkerContentIfNeeded
	// builds a LayoutListMarkerImage child and sets marker_text_type_ =
	// kNotText (list_marker.cc:277-310 @ 4883d11f); the marker text with
	// prefix/suffix is a single space after the image (MarkerText
	// GeneratesMarkerImage arm, :168-172). Reuses the synthetic-<img>
	// pattern from createPseudoElement's content:url() case.
	if markerContent == "" && !hasContentProperty && generatesMarkerImage(style) {
		raw, _ := style.Get("list-style-image")
		src, ok := css.ParseURLValue(strings.TrimSpace(raw))
		if !ok {
			// Non-url() image values (gradients) are still images: the
			// text fallback stays suppressed (Blink MarkerText returns
			// kNotText for GeneratesMarkerImage). Painting generated
			// images as markers is not implemented yet, so no marker box
			// is produced at all (LOU-303).
			return nil
		}
		spaceNode := &html.Node{Type: html.TextNode, Text: " ", Parent: markerNode}
		return &LayoutInputNode{
			DOMNode:         markerNode,
			style:           markerStyle,
			isMarkerNode:    true,
			MarkerIsOutside: !markerInside,
			MarkerCategory:  CategoryNone,
			children: []*LayoutInputNode{
				b.createSyntheticImage(src, markerNode),
				{DOMNode: spaceNode, style: markerStyle},
			},
		}
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
			} else if cs, ok := b.counterStyleRules()[string(lst)]; ok {
				// @counter-style reference: format the ordinal through the
				// custom style (Blink CounterStyle::GenerateRepresentation
				// WithPrefixAndSuffix).
				markerContent = css.ApplyCounterStyle(b.listItemOrdinal(node), cs, b.counterStyleRules())
			} else {
				// <string> value (e.g., list-style-type: "§"): the string
				// IS the marker text, verbatim, no suffix (Blink
				// kStaticString). GetListStyleType already stripped the
				// quotes and decoded CSS escapes.
				markerContent = string(lst)
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
	// isMarkerNode lets the ::first-letter walk (and other consumers) skip
	// this generated subtree per CSS Pseudo-4 §3.3 — the originating
	// list-item's own marker is not part of first-letter consideration.
	return &LayoutInputNode{
		DOMNode:         markerNode,
		style:           markerStyle,
		isMarkerNode:    true,
		MarkerIsOutside: !markerInside,
		MarkerCategory:  GetListStyleCategory(style),
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

	value := b.listItemOrdinal(node)

	// The predefined symbolic styles carry the CSS Counter Styles 3
	// `suffix: " "` (one space), just like the numeric styles carry ". ".
	// Blink renders marker text via
	// CounterStyle::GenerateRepresentationWithPrefixAndSuffix
	// (ListMarker::MarkerText kSymbol/kLanguage arms, list_marker.cc:180-212
	// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f), so an inside disc marker
	// is "• ", not "•".
	switch lst {
	case css.ListStyleTypeDisc:
		return "• " // bullet
	case css.ListStyleTypeCircle:
		return "◦ " // white bullet
	case css.ListStyleTypeSquare:
		return "▪ " // black small square
	case css.ListStyleTypeDecimal:
		return strconv.Itoa(value) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeDecimalLeadingZero:
		return fmt.Sprintf("%02d", value) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeLowerAlpha, css.ListStyleTypeLowerLatin:
		return css.ToAlpha(value) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeUpperAlpha, css.ListStyleTypeUpperLatin:
		return strings.ToUpper(css.ToAlpha(value)) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeLowerRoman:
		return strings.ToLower(css.ToRoman(value)) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeUpperRoman:
		return css.ToRoman(value) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeLowerGreek:
		return css.ToGreek(value) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeHebrew:
		return css.ToHebrew(value) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeCJKDecimal:
		return css.ToCJKDecimal(value) + "、"
	case css.ListStyleTypeKoreanHangulFormal:
		return css.ToKoreanHangulFormal(value) + ", "
	case css.ListStyleTypeDisclosureOpen:
		return "▼ " // ▼ down-pointing triangle (details expanded)
	case css.ListStyleTypeDisclosureClosed:
		return "▶ " // ▶ right-pointing triangle (details collapsed)
	default:
		return ""
	}
}

// applyImplicitListItemStep applies the implicit list-item counter step for
// a list item (real node or ::before/::after pseudo): +1, or -1 when the
// innermost list-item counter is reversed (CSS Lists 3 §6.1). Suppressed
// when the element has an explicit counter-increment or counter-set on
// list-item — per HTML's ordinal-value algorithm (Blink
// ListItemOrdinal::CalcValue @ 4883d11f) a valued item takes exactly that
// number, and the implicit step runs after EnterObject applied the set, so
// it would otherwise override it. Also suppressed for the auto-count form
// of reversed(list-item), whose computed initial already encodes this
// item's contribution. Stale counter entries are removed first, as Blink
// ProcessCounter does — the implicit path bypasses ProcessCounter, so
// without it a sibling list's exited scope would be mutated.
func (b *LayoutTreeBuilder) applyImplicitListItemStep(node *html.Node, style *css.Style) {
	if _, ok := counterIncrementValue(style, "list-item"); ok {
		return
	}
	if _, ok := counterSetValue(style, "list-item"); ok {
		return
	}
	step := 1
	if b.counterCtx.IsCounterReversed("list-item") {
		if b.counterCtx.HasAutoReversedCounter(node, "list-item") {
			return
		}
		step = -1
	}
	b.counterCtx.RemoveStaleCounters(node, "list-item")
	b.counterCtx.UpdateCounterValue(node, "list-item", css.CounterIncrementType, step)
}

// listItemOrdinal returns the list-item counter value for a marker.
// Prefers the counter context when list-item has been explicitly established
// (e.g. counter-reset: reversed(list-item) or counter-reset: list-item N);
// falls back to DOM-sibling counting otherwise, so normal lists without an
// explicit counter-reset still work.
func (b *LayoutTreeBuilder) listItemOrdinal(node *html.Node) int {
	if b.counterCtx.IsCounterInScope(node, "list-item") {
		return b.counterCtx.GetCounterValues(node, "list-item", true)[0]
	}
	return b.getListItemCounterValue(node)
}

// counterStyleRules lazily indexes the document's @counter-style rules by
// name for ::marker content resolution.
func (b *LayoutTreeBuilder) counterStyleRules() map[string]css.CounterStyleRule {
	if b.counterStyleMap == nil {
		b.counterStyleMap = make(map[string]css.CounterStyleRule)
		for _, sheet := range b.stylesheets {
			for _, cs := range sheet.CounterStyles {
				b.counterStyleMap[cs.Name] = cs
			}
		}
	}
	return b.counterStyleMap
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
		css.ListStyleTypeCJKDecimal, css.ListStyleTypeHebrew, css.ListStyleTypeKoreanHangulFormal,
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

// isBlockContainerStyle is isBlockContainer with the raw display value in
// hand: `inline flow-root list-item` folds into DisplayInlineListItem in
// GetDisplay() but IS a block container (its children get anonymous-block
// treatment), while a pure `inline list-item` is a true inline box and is
// not. Mirrors Blink's kInlineFlowRootListItem vs kInlineListItem split.
func isBlockContainerStyle(st *css.Style) bool {
	if st == nil {
		return false
	}
	d := st.GetDisplay()
	return isBlockContainer(d) || (d == css.DisplayInlineListItem && !st.IsInlineBoxListItem())
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
	if parentStyle == nil || !isBlockContainerStyle(parentStyle) {
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
	if parentStyle == nil || !isBlockContainerStyle(parentStyle) {
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

// isFirstLetterContainer returns true for display types whose first
// formatted line can host a ::first-letter pseudo-element. Per CSS
// Pseudo-4 §3.3 this is "block container" plus the inline-level
// formatting-context-establishing variants (inline-block, inline
// list-item / inline flow-root list-item) — Blink's
// FirstLetterPseudoElement::FirstLetterTextLayoutObject treats these
// identically because each starts its own first-line. Narrower than
// isBlockContainer (which also gates anonymous block generation), so we
// keep it local to the first-letter machinery.
func isFirstLetterContainer(d css.DisplayType) bool {
	if isBlockContainer(d) {
		return true
	}
	return d == css.DisplayInlineListItem
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
	if parentStyle == nil || !isFirstLetterContainer(parentStyle.GetDisplay()) {
		return children
	}
	if len(b.stylesheets) == 0 {
		return children
	}
	if !css.HasFirstLetterRules(node, b.stylesheets, b.viewportWidth, b.viewportHeight) {
		return children
	}

	// preserveBreaks mirrors Blink's ShouldPreserveBreaks: white-space
	// values that preserve newlines (pre / pre-wrap / pre-line / break-spaces)
	// cause a leading LF to terminate first-letter discovery rather than be
	// treated as skippable leading space.
	preserveBreaks := whiteSpacePreservesBreaks(parentStyle.GetWhiteSpace())

	// Find and split the first non-whitespace character in the inline content.
	// We only look at direct text children or text inside inline children.
	// Pass node and parentStyle so walkFirstLetter can compute the correct
	// pseudo-element style based on the actual containing element's style,
	// not just the originating block's style (mirrors Blink's
	// FirstLetterPseudoElement::StyleForFirstLetter).
	return b.splitFirstLetter(children, node, parentStyle, preserveBreaks)
}

// initialLetterBlockStartMargin returns the drop block-start margin for an
// initial-letter float (horizontal text): max(0, lineHeight*size - bigAscent -
// paraLineDescent), where bigAscent is the scaled letter's ascent and
// paraLineDescent is the paragraph font descent plus half-leading.
//
// This is the letter's absolute block position for BOTH drop and `raise`: for
// raise the surrounding text is shifted DOWN and the letter placed UP by the same
// block-start-adjust lineHeight*(size-sink) (handled in inline layout, LOU-289
// part 3), and those two cancel so the letter's own margin is the unchanged drop
// offset. Mirrors Blink ComputeInitialLetterBoxBlockOffset
// (initial_letter_utils.cc:24-105 @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func initialLetterBlockStartMargin(size, lineHeight, bigAscent, paraLineDescent float64) float64 {
	off := lineHeight*size - bigAscent - paraLineDescent
	if off < 0 {
		return 0
	}
	return off
}

// initialLetterTextShift returns the block-start-adjust by which an
// initial-letter float's surrounding paragraph text is shifted down (and the
// letter itself raised to the visual top): lineHeight*(ceil(size) - sink) for
// `raise`, otherwise 0. Only the `raise` keyword is modelled — bare-number / drop
// and explicit-sink produce no shift (matching pre-LOU-289 behavior).
//
// Gating on Raise and using ceil(size) (not the raw float) is required: for a
// bare fractional size like `2.5`, GetInitialLetter sets Sink = floor(size) = 2,
// so a naive lineHeight*(size-sink) would shift a plain drop by half a line.
// Blink ceils size to an integer and treats the bare-number sink as size (no
// shift). Mirrors Blink ComputeInitialLetterBoxBlockOffset's block_start_adjust
// (initial_letter_utils.cc:38-69 @ 4883d11fef).
func initialLetterTextShift(il css.InitialLetterValue, lineHeight float64) float64 {
	if !il.Raise {
		return 0
	}
	shift := lineHeight * (math.Ceil(il.Size) - float64(il.Sink))
	if shift < 0 {
		return 0
	}
	return shift
}

// applyInitialLetter mutates the ::first-letter style `flStyle` into an
// initial-letter box when it declares the `initial-letter` property
// (CSS Inline Layout 3 §7.3). The box is realised as an inline-start float
// whose font is scaled so its cap-height spans <size> line-boxes, and which is
// dropped (its block-start offset) so the surrounding lines flow around it.
//
// This mirrors the two Blink steps, faithfully ported into our model:
//
//   - Font scaling — StyleResolver::ComputeInitialLetterFont
//     (style_resolver.cc:3658-3695 @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
//     desired_cap_height = line_height * (size - 1) + cap_height_of_para
//     adjusted_font_size = desired_cap_height * font_size / cap_height
//     The big and paragraph fonts share a family here, so cap_height scales
//     linearly with size: cap_height/font_size == cap_height_of_para/para_size.
//     We therefore use the paragraph's measured cap-ratio rather than
//     re-measuring the big font, which is exact for a single family.
//
//   - Block-position offset — ComputeInitialLetterBoxBlockOffset
//     (initial_letter_utils.cc:22-105 @ 4883d11fef): for the drop case
//     (size >= sink, horizontal text)
//     block_offset = line_height * size - big_ascent - para_line_descent
//     where para_line_descent is the paragraph font descent plus its half-
//     leading (CalculateLeadingSpace + AddLeading in line_utils.h). The float
//     carries this as a block-start margin.
//
// Reusing CapHeight (pkg/css/style.go) per the project rule: no hardcoded cap
// ratio — the cap-unit 0.7em heuristic is used only when CapHeight is unmeasured.
func (b *LayoutTreeBuilder) applyInitialLetter(flStyle, parentStyle *css.Style) {
	if flStyle == nil || parentStyle == nil {
		return
	}
	il := flStyle.GetInitialLetter()
	if !il.Set {
		return
	}

	paraFontSize := parentStyle.GetFontSize()
	lineHeight := parentStyle.GetLineHeight()
	if paraFontSize <= 0 || lineHeight <= 0 {
		return
	}

	// Paragraph cap-height: prefer the measured OS/2 sCapHeight (Style.CapHeight,
	// set in engine.go from the font metrics pass). Fall back to the spec's
	// 0.7em heuristic only when unmeasured.
	paraCap := parentStyle.CapHeight
	if paraCap <= 0 {
		paraCap = 0.7 * paraFontSize
	}

	// ComputeInitialLetterFont: scale so the big font's cap-height spans
	// (size-1) line-boxes plus one paragraph cap-height.
	desiredCapHeight := lineHeight*(il.Size-1) + paraCap
	capRatio := paraCap / paraFontSize
	adjustedFontSize := desiredCapHeight / capRatio
	if adjustedFontSize <= 0 {
		return
	}

	// The big initial-letter font ascent, measured at the adjusted size.
	bigFontPath := resolveFontPath(flStyle, b.fontConfig)
	bigAscent := text.FontAscentFromFont(adjustedFontSize, bigFontPath, nil)
	if bigAscent <= 0 {
		bigAscent = capRatio * adjustedFontSize
	}

	// Paragraph line descent = font descent + half-leading on the descent side
	// (CalculateLeadingSpace → AddLeading). Half-leading is split from the
	// difference between the computed line-height and the font's em-height.
	paraFontPath := resolveFontPath(parentStyle, b.fontConfig)
	paraAscent := text.FontAscentFromFont(paraFontSize, paraFontPath, nil)
	paraDescent := text.FontDescentFromFont(paraFontSize, paraFontPath, nil)
	leading := lineHeight - (paraAscent + paraDescent)
	paraLineDescent := paraDescent + leading/2

	// Block-start margin (horizontal text) = the drop offset, the letter's
	// absolute block position for both drop and `raise`. For raise, inline layout
	// shifts the surrounding text down and the letter up by equal-and-opposite
	// block-start-adjust (LOU-289 part 3, inline_layout.go), so the letter's own
	// margin stays the drop offset. Mirrors Blink ComputeInitialLetterBoxBlockOffset
	// (initial_letter_utils.cc:24-105 @ 4883d11fef).
	blockOffset := initialLetterBlockStartMargin(il.Size, lineHeight, bigAscent, paraLineDescent)

	// Realise the initial-letter box as an inline-start float. font-size and
	// line-height are overridden (CSS Inline Layout 3 §7.5.1 — the author's
	// font-size/line-height on ::first-letter do not apply to the box); the
	// float hugs the scaled glyph (line-height == 1em of the adjusted font).
	floatSide := "left"
	if parentStyle.GetDirection() == css.DirectionRTL {
		floatSide = "right"
	}
	px := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "px" }
	fontSizePx := px(adjustedFontSize)
	flStyle.Set("float", floatSide)
	flStyle.Set("font-size", fontSizePx)
	flStyle.Set("line-height", fontSizePx)
	flStyle.Set("margin-top", px(blockOffset))
}

// whiteSpacePreservesBreaks reports whether the given white-space value
// preserves newline characters. Mirrors Blink's ShouldPreserveBreaks @
// 4883d11fef which is true for pre, pre-wrap, pre-line and break-spaces.
func whiteSpacePreservesBreaks(ws css.WhiteSpace) bool {
	switch ws {
	case css.WhiteSpacePre, css.WhiteSpacePreWrap, css.WhiteSpacePreLine, css.WhiteSpaceBreakSpaces:
		return true
	}
	return false
}

// isFirstLetterPunctuation reports whether r is a punctuation character that
// should be included in the ::first-letter pseudo-element per CSS Pseudo-4 §3.2.
// Mirrors Blink's IsPunctuationForFirstLetter (Po/Ps/Pe/Pi/Pf only — Pd and Pc
// are deliberately excluded). Blink reference: third_party/blink/renderer/
// core/dom/first_letter_pseudo_element.cc @ Chromium 4883d11fef.
func isFirstLetterPunctuation(r rune) bool {
	return unicode.Is(unicode.Po, r) ||
		unicode.Is(unicode.Ps, r) ||
		unicode.Is(unicode.Pe, r) ||
		unicode.Is(unicode.Pi, r) ||
		unicode.Is(unicode.Pf, r)
}

// firstLetterPunctuationState tracks the cross-text-node accumulation state
// used by Blink's FirstLetterPseudoElement::FirstLetterLength @ 4883d11fef:
//
//   - firstLetterPunctNotSeen:  initial; leading spaces and leading punctuation
//     may both be skipped/consumed.
//   - firstLetterPunctSeen:     only punctuation has been found so far in
//     earlier text nodes; subsequent leading spaces are no longer allowed
//     but more leading punctuation may still be accumulated.
//   - firstLetterPunctDisallow: a typographic letter unit has been found;
//     further leading punctuation in later text nodes is rejected.
type firstLetterPunctuationState int

const (
	firstLetterPunctNotSeen firstLetterPunctuationState = iota
	firstLetterPunctSeen
	firstLetterPunctDisallow
)

// isFirstLetterSpace reports whether r is whitespace skipped by the
// ::first-letter algorithm. Mirrors Blink's IsSpaceForFirstLetter at
// 4883d11fef: ASCII space/tab and U+00A0 NO-BREAK SPACE are always
// treated as skippable spaces; LF/CR are skippable only when not
// preserving line breaks (i.e. with white-space: normal/nowrap, not
// pre/pre-wrap/pre-line). When preserveBreaks is true, leading LF/CR
// is NOT a skippable space and instead terminates first-letter
// discovery via isFirstLetterNewline.
func isFirstLetterSpace(r rune, preserveBreaks bool) bool {
	switch r {
	case ' ', '\t', ' ':
		return true
	case '\n', '\r':
		return !preserveBreaks
	}
	return false
}

// isFirstLetterNewline reports whether r is a line-break character.
// Mirrors Blink's IsNewLine — used to terminate first-letter discovery
// when the leading non-space character is a newline (e.g. white-space:
// pre with a leading "\n").
func isFirstLetterNewline(r rune) bool {
	return r == '\n' || r == '\r'
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

// firstLetterLength returns the byte position and length of the leading
// typographic letter unit in text per CSS Pseudo-4 §3.2 and updates the
// cross-text-node punctuation state. The unit is: optional leading whitespace,
// then optional leading punctuation grapheme clusters, then one
// non-punctuation grapheme cluster (the letter), then trailing punctuation
// grapheme clusters.
//
// state threads Blink's Punctuation across text nodes (see
// firstLetterPunctuationState). preserveBreaks is whether the parent's
// white-space property preserves line breaks (pre / pre-wrap / pre-line /
// break-spaces) — when true, leading LF/CR is NOT skippable and instead
// terminates the search.
//
// Returns (skipped, length, newState):
//
//   - When (length > 0) and newState == firstLetterPunctSeen: only leading
//     punctuation was found in this text node; the walker should remember
//     this node as a candidate "punctuation host" and continue scanning the
//     next text node carrying the new state.
//   - When (length > 0) and newState == firstLetterPunctDisallow: the
//     letter was found in this text node; the walker should wrap here (or
//     wrap the earlier "punctuation host" if one was recorded).
//   - When length == 0 and newState == firstLetterPunctDisallow: the leading
//     non-space char was a newline or post-punctuation space; first-letter
//     does not apply for this originating block.
//   - When length == 0 and newState == state (unchanged): the text node was
//     all whitespace; the walker should continue without recording anything.
//
// Mirrors Blink's FirstLetterPseudoElement::FirstLetterLength at Chromium
// 4883d11fef.
func firstLetterLength(text string, state firstLetterPunctuationState, preserveBreaks bool) (skipped, length int, newState firstLetterPunctuationState) {
	newState = state
	textLen := len(text)
	if textLen == 0 {
		return 0, 0, newState
	}

	pos := 0
	// Account for leading whitespace only when we haven't started accumulating
	// punctuation from a previous text node. Once punctuation has been seen,
	// intervening spaces break the first-letter unit (Blink kSeen semantics).
	if newState == firstLetterPunctNotSeen {
		for pos < textLen {
			r, sz := utf8.DecodeRuneInString(text[pos:])
			if !isFirstLetterSpace(r, preserveBreaks) {
				break
			}
			pos += sz
		}
		if pos == textLen {
			// All whitespace: leave state unchanged so the walker keeps
			// scanning subsequent text nodes.
			return 0, 0, newState
		}
	}
	skipped = pos

	// Accumulate leading punctuation grapheme clusters.
	punctStart := pos
	for pos < textLen {
		r, _ := utf8.DecodeRuneInString(text[pos:])
		if !isFirstLetterPunctuation(r) {
			break
		}
		pos += firstLetterGraphemeLen(text[pos:])
	}

	if pos == textLen {
		if pos > punctStart {
			// Text ends at allowed leading punctuation. Signal continuation;
			// the walker may find the letter in a subsequent text node.
			newState = firstLetterPunctSeen
			return skipped, pos - skipped, newState
		}
		// Only whitespace in the original kNotSeen path is handled above;
		// in the kSeen path we get here only when the text node is empty.
		return 0, 0, newState
	}

	// From here on, no further leading punctuation will be allowed in later
	// text nodes (kDisallow). If the next char is a space or newline, the
	// first-letter unit is broken.
	newState = firstLetterPunctDisallow
	r, _ := utf8.DecodeRuneInString(text[pos:])
	if isFirstLetterSpace(r, preserveBreaks) || isFirstLetterNewline(r) {
		return 0, 0, newState
	}

	// Consume one grapheme cluster as the letter.
	pos += firstLetterGraphemeLen(text[pos:])

	// Consume trailing punctuation grapheme clusters within this text node.
	for pos < textLen {
		r, _ := utf8.DecodeRuneInString(text[pos:])
		if !isFirstLetterPunctuation(r) {
			break
		}
		pos += firstLetterGraphemeLen(text[pos:])
	}

	return skipped, pos - skipped, newState
}

// firstLetterPunctHost records a text node that has accumulated leading
// punctuation but no letter yet — Blink's `punctuation_text` in
// FirstLetterPseudoElement::FirstLetterTextLayoutObject @ 4883d11fef.
// When a subsequent text node provides the letter, the ::first-letter wrap
// is applied to THIS earlier punctuation host (covering its punctuation
// portion only). The parentSlice + index let the walker rewrite the
// host's enclosing children slice; descendChildren records the chain of
// ancestor children-slice owners that need their children replaced when
// the wrap is applied in a nested descendant.
type firstLetterPunctHost struct {
	// child is the text LayoutInputNode that contains leading punctuation.
	child *LayoutInputNode
	// text is child.TextContent() at the time of recording.
	text string
	// skipped is the byte offset of the first punctuation rune.
	skipped int
	// length is the byte count of the leading punctuation grapheme clusters
	// (covering the entire first-letter span on this node).
	length int
	// parentNode is the DOM host for the synthetic ::first-letter wrapper
	// when wrapping at this site.
	parentNode *html.Node
	// parentStyle is the computed style of the element containing this text node.
	// The ::first-letter pseudo-element inherits from this style, not the
	// originating block's style (mirrors Blink FirstLetterPseudoElement::StyleForFirstLetter).
	parentStyle *css.Style
	// owner is the children slice that directly contains `child`; index is
	// child's position within owner. ancestors records the descend chain so
	// the walker can update each parent's children pointer when the wrap
	// rewrites the host's slice. Each entry stores (parent node, the index
	// of the child whose .children needs replacing at unwind time).
	owner     []*LayoutInputNode
	index     int
	ancestors []firstLetterAncestor
}

// firstLetterAncestor records one rung in the descend chain from the
// originating block down to a punctuation host. When the wrap is applied
// to the host, the owner slice is rewritten; each ancestor's child .children
// pointer must be updated to refer to the rewritten owner.
type firstLetterAncestor struct {
	// parent is the LayoutInputNode whose .children should be replaced with
	// `rewritten` once the wrap is applied below.
	parent *LayoutInputNode
}

// splitFirstLetter walks children to find the first letter unit and wraps
// it in a synthetic inline span with the first-letter style.
//
// Mirrors Blink's FirstLetterPseudoElement::FirstLetterTextLayoutObject at
// Chromium 4883d11fef. The walker:
//
//   - Skips ::marker generated content per CSS Pseudo-4 §3.3 — the
//     originating element's own marker AND any descendant element's
//     marker are excluded from first-letter consideration.
//   - Skips out-of-flow (abs/fixed) and floated siblings (they are not
//     part of the first formatted line of this block).
//   - Stops at element children that establish a new formatting context
//     (inline-block, inline flow-root list-item, flex, grid, table-cell,
//     etc.) — the originating block's first-letter neither descends into
//     them nor continues past them.
//   - Recursively descends into all other element children, block- AND
//     inline-level, including empty-inline chains and nested block
//     list-items.
//   - Threads Blink's Punctuation state across text nodes so a leading
//     punctuation in one node (e.g. `<span>"</span>abc`) is the wrap
//     site once the letter is located in a subsequent node.
//
// When the letter is found inside a descendant, the wrap is applied to
// that descendant's child list (mutated in place) — not pulled out to the
// originating block. That matches the spec: the first-letter pseudo styles
// the letter at its lowest containing inline.
//
// Returns the (possibly modified) `children` slice for the originating block.
func (b *LayoutTreeBuilder) splitFirstLetter(
	children []*LayoutInputNode, originatingBlockNode *html.Node, parentStyle *css.Style, preserveBreaks bool,
) []*LayoutInputNode {
	state := firstLetterPunctNotSeen
	var pendingHost *firstLetterPunctHost
	newChildren, _, _ := b.walkFirstLetter(children, originatingBlockNode, originatingBlockNode, parentStyle, preserveBreaks, state, &pendingHost, nil)
	return newChildren
}

// walkFirstLetter implements the recursive tree walk. Returns the
// (possibly modified) children slice, a bool indicating whether the
// first-letter wrap was applied somewhere in the subtree rooted at these
// children, and the updated punctuation state for the caller to continue
// scanning later siblings. Once `applied` flips to true, the walk
// short-circuits and the wrap is committed.
//
// The walk is pre-order over the layout subtree, mirroring Blink's
// FirstLetterPseudoElement::FirstLetterTextLayoutObject which uses
// NextInPreOrder. Block-level descendants are NOT skipped — the first
// letter may sit arbitrarily deep, possibly inside nested block list-items
// (see css-pseudo first-letter-exclude-block-child-marker, where the
// originating `<li>`'s first letter is the 'i' inside its block `<span>`
// list-item child, after that span's own ::marker is skipped).
//
// pendingHost is a pointer into the call chain so a nested descendant can
// see a sibling's earlier kSeen punctuation node and apply the wrap there
// even when the letter is discovered deep in another subtree.
//
// parentStyle is the style of the element at this level in the tree walk.
// When the first letter is found, the ::first-letter pseudo-element inherits
// from the element that actually contains the letter text, not from the
// originating block (mirrors Blink FirstLetterPseudoElement::StyleForFirstLetter).
// originatingBlockNode is the block element on which ::first-letter was declared.
func (b *LayoutTreeBuilder) walkFirstLetter(
	children []*LayoutInputNode, originatingBlockNode *html.Node, parentNode *html.Node, parentStyle *css.Style,
	preserveBreaks bool, state firstLetterPunctuationState,
	pendingHost **firstLetterPunctHost, ancestors []firstLetterAncestor,
) (newChildren []*LayoutInputNode, applied bool, newState firstLetterPunctuationState) {
	for i, child := range children {
		// Skip ::marker generated content per CSS Pseudo-4 §3.3 — the
		// originating element's own marker AND any descendant element's
		// marker are excluded from first-letter consideration.
		if child.IsMarkerNode() {
			continue
		}
		// Skip out-of-flow (abs/fixed) and floated children — they don't
		// participate in the parent block's first formatted line.
		if isOutOfFlowOrFloat(child) {
			continue
		}
		// Stop at element children that establish a new formatting context
		// (inline-block, inline flow-root list-item, flex, grid,
		// table-cell, etc.). Such atomic inline-level boxes occupy the
		// first-letter slot: their content is its own first formatted
		// line, and the originating block's first-letter does NOT
		// descend into them OR continue past them. CSS Pseudo-4 §3.3 /
		// Blink FirstLetterTextLayoutObject — see WPT
		// first-letter-exclude-inline-child-marker groups 3–6 where
		// `<ibi>` / `<ibo>` (display: inline flow-root list-item) block
		// the `<li>`'s ::first-letter from reaching either the inner
		// "item" text or the following "after" text.
		//
		// Text children share the parent's style, so the IsElement()
		// guard is essential: an `<ibi>` parent's text child would
		// otherwise look like a flow-root child of itself and be
		// skipped, defeating ibi::first-letter on its own content.
		if child.IsElement() && child.Style() != nil && child.Style().EstablishesNewFormattingContext() {
			return children, true, firstLetterPunctDisallow // consume the slot without wrapping
		}

		if child.IsText() {
			text := child.TextContent()
			idx, letterLen, ns := firstLetterLength(text, state, preserveBreaks)
			state = ns
			if letterLen == 0 {
				if state == firstLetterPunctDisallow {
					// Leading non-space char was newline / post-punctuation
					// space — no first-letter for this originating block.
					return children, true, state
				}
				continue
			}
			// The ::first-letter pseudo-element inherits from the element
			// that contains the text, not from the originating block.
			// Use the text node's computed style as the inheritance parent.
			textStyle := child.Style()
			if textStyle == nil {
				textStyle = parentStyle
			}
			if state == firstLetterPunctSeen {
				// Only leading punctuation found here; remember as the
				// wrap candidate and continue searching subsequent text
				// nodes for the letter.
				if *pendingHost == nil {
					ancs := make([]firstLetterAncestor, len(ancestors))
					copy(ancs, ancestors)
					*pendingHost = &firstLetterPunctHost{
						child:       child,
						text:        text,
						skipped:     idx,
						length:      letterLen,
						parentNode:  parentNode,
						parentStyle: textStyle,
						owner:       children,
						index:       i,
						ancestors:   ancs,
					}
				}
				continue
			}
			// state == kDisallow with letterLen > 0 — the letter is in
			// THIS text node. If we previously recorded a punctuation
			// host in an earlier text node, wrap there instead so the
			// pseudo styles only the leading punctuation.
			if host := *pendingHost; host != nil {
				newOwner := b.wrapFirstLetterInChildren(host.owner, host.index, host.child, host.text, host.skipped, host.length, host.parentNode, host.parentStyle, originatingBlockNode)
				// Propagate the rewritten owner up through the descend
				// chain so each ancestor's .children pointer matches.
				for _, a := range host.ancestors {
					a.parent.children = newOwner
				}
				_ = newOwner
				return children, true, firstLetterPunctDisallow
			}
			result := b.wrapFirstLetterInChildren(children, i, child, text, idx, letterLen, parentNode, textStyle, originatingBlockNode)
			return result, true, firstLetterPunctDisallow
		}

		// Element or anonymous container — recurse into its children
		// (block-level OR inline-level). If the wrap is applied, the
		// child's children slice has been replaced in place; we return
		// the parent slice unchanged.
		if child.IsElement() || child.IsAnonymous() {
			descAnc := append(ancestors[:len(ancestors):len(ancestors)], firstLetterAncestor{parent: child})
			childStyle := child.Style()
			grandchildren, didApply, ns := b.walkFirstLetter(child.children, originatingBlockNode, b.firstLetterHostNode(child, parentNode), childStyle, preserveBreaks, state, pendingHost, descAnc)
			state = ns
			if didApply {
				child.children = grandchildren
				return children, true, state
			}
		}
	}
	return children, false, state
}

// firstLetterHostNode returns the DOM node that should host the synthetic
// ::first-letter wrapper when the letter is found at this point in the
// walk. For element nodes it is the element itself; for anonymous nodes
// we fall back to the original parent so the synthetic node still has a
// stable parent pointer.
func (b *LayoutTreeBuilder) firstLetterHostNode(child *LayoutInputNode, fallback *html.Node) *html.Node {
	if child != nil && child.DOMNode != nil && child.DOMNode.Type == html.ElementNode {
		return child.DOMNode
	}
	return fallback
}

// wrapFirstLetterInChildren replaces the text child at index i with the
// leading-whitespace prefix (if any), the synthetic ::first-letter span,
// and the trailing text. Returns the new children slice.
// parentStyle is the computed style of the element containing the text.
// originatingBlockNode is the element on which ::first-letter was declared.
// The ::first-letter pseudo inherits from parentStyle (the containing element),
// not from the originating block's style (mirrors Blink).
func (b *LayoutTreeBuilder) wrapFirstLetterInChildren(
	children []*LayoutInputNode, i int, child *LayoutInputNode,
	text string, idx, letterLen int,
	parentNode *html.Node, parentStyle *css.Style, originatingBlockNode *html.Node,
) []*LayoutInputNode {
	// Compute the ::first-letter pseudo-element style using the containing
	// element's style (parentStyle), not the originating block's style.
	// However, we need the originating block's style for initial-letter sizing.
	originatingStyle := b.styles[originatingBlockNode]
	if originatingStyle == nil {
		originatingStyle = parentStyle
	}
	flStyle := css.ComputePseudoElementStyle(
		originatingBlockNode, "first-letter", b.stylesheets,
		b.viewportWidth, b.viewportHeight, parentStyle,
	)

	// Apply initial-letter transformation if needed.
	// Use the originating block's style for line-height/font-size sizing context.
	b.applyInitialLetter(flStyle, originatingStyle)

	letterEnd := idx + letterLen
	letter := text[idx:letterEnd]
	rest := text[letterEnd:]

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

	result := make([]*LayoutInputNode, 0, len(children)+2)
	result = append(result, children[:i]...)

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
