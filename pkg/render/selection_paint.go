package render

// selection_paint.go implements ::selection highlight painting (LOU-344).
// Conceptually mirrors Blink's HighlightPainter / SelectionPaintState
// (third_party/blink/renderer/core/paint/highlight_painter.h @ blob
// 96737c206898cbd27aed2cbeb07c455a3fa3d2dd), simplified to a single
// highlight type (no multi-layer PaintCase dispatch — see LOU-344's
// "Theory of root cause" for why that scope cut is deliberate).
//
// Architecture note: pkg/render previously had NO access to the DOM
// Selection or to stylesheets (it only consumed the pre-built Box tree —
// see Renderer.SetSelectionContext's doc comment). Rather than threading
// selection state through pkg/layout's fragment-emission machinery
// (inline_layout.go's emitTextFragment, deep inside per-line text
// shaping), this resolves the selected sub-range of each text fragment
// AT PAINT TIME using only data already on Box/PaintLayer: box.Node (the
// fragment's originating ELEMENT — see fragmentToBox/emitTextFragment,
// which set frag.Node to the parent element, not the DOM text node) and
// box.Text (the fragment's own rendered string). selectionTextConsumed
// recovers the node-local offset a fragment covers by tracking, per DOM
// text node, how many code units have already been consumed by earlier
// fragments painted in this Render call — DOM text nodes are walked by
// drawText in paint order, which is document order for the common case
// (no bidi reordering across fragment boundaries within one node).

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// selectionOverlapForTextNode returns the [start,end) sub-range of a text
// fragment — expressed in FRAGMENT-LOCAL offsets (i.e. already shifted so
// 0 means the start of THIS fragment's own text, not the node's) — that
// falls inside rng, given the fragment covers node-local offsets
// [fragStart, fragEnd) of node's text content. ok is false when rng is nil
// or doesn't touch this node at all, or the computed range is empty.
//
// Mirrors the DOM Range "partially contained node" containment check
// (https://dom.spec.whatwg.org/#concept-range-partially-contained), with
// node-local offsets just used directly since both StartContainer and
// EndContainer here are always the same text node (the simplified single-
// Range model this engine supports — see html.Range's doc comment).
func selectionOverlapForTextNode(rng *html.Range, node *html.Node, fragStart, fragEnd int) (start, end int, ok bool) {
	if rng == nil || node == nil {
		return 0, 0, false
	}

	// nodeSelStart/nodeSelEnd: the selected range expressed in node-local
	// offsets, clamped to [0, len(node.Text)]. A boundary container that
	// is an ancestor ELEMENT (e.g. selectNodeContents(div) where div has
	// one text-node child) selects the text node fully when the text
	// node's index-among-siblings falls within [StartOffset, EndOffset) —
	// the common case in every LOU-344 target test is "select all of the
	// element's children" via selectNodeContents, so the text node is
	// either fully in or fully out for an element-container boundary.
	nodeSelStart, hasStart := nodeLocalBoundary(rng.StartContainer, rng.StartOffset, node, true)
	nodeSelEnd, hasEnd := nodeLocalBoundary(rng.EndContainer, rng.EndOffset, node, false)
	if !hasStart && !hasEnd {
		return 0, 0, false
	}
	if !hasStart {
		nodeSelStart = 0
	}
	if !hasEnd {
		nodeSelEnd = len(node.Text)
	}
	if nodeSelStart < 0 {
		nodeSelStart = 0
	}
	if nodeSelEnd > len(node.Text) {
		nodeSelEnd = len(node.Text)
	}
	if nodeSelStart >= nodeSelEnd {
		return 0, 0, false
	}

	// Intersect [nodeSelStart, nodeSelEnd) with this fragment's own
	// [fragStart, fragEnd) node-local span, then shift to fragment-local.
	overlapStart := nodeSelStart
	if fragStart > overlapStart {
		overlapStart = fragStart
	}
	overlapEnd := nodeSelEnd
	if fragEnd < overlapEnd {
		overlapEnd = fragEnd
	}
	if overlapStart >= overlapEnd {
		return 0, 0, false
	}
	return overlapStart - fragStart, overlapEnd - fragStart, true
}

// nodeLocalBoundary resolves a single DOM Range boundary point (container,
// offset) against a specific text node, returning the node-local character
// offset that boundary implies for that node, or ok=false if the boundary
// doesn't constrain this node's selected range at all (e.g. the boundary's
// container is an unrelated subtree).
//
// isStart selects which extreme to assume when container is an ancestor
// ELEMENT of node rather than node itself: per DOM Range semantics, a
// child-index offset into an element container selects whole child
// subtrees [offset_start, offset_end) — since this engine doesn't track
// each text node's sibling index against the container relationship in
// general, the supported case is the common one every LOU-344 test uses:
// container IS node itself (a setStart/setEnd(textNode, charOffset) call),
// OR container is node's direct parent (selectNodeContents semantics,
// where node is treated as fully selected — isStart picks offset 0 / the
// full length appropriately).
func nodeLocalBoundary(container *html.Node, offset int, node *html.Node, isStart bool) (int, bool) {
	if container == nil {
		return 0, false
	}
	if container == node {
		return offset, true
	}
	if container == node.Parent {
		// Element-container boundary (selectNodeContents / setStart(div,0)
		// style). Determine node's index among its parent's children to
		// decide whether the boundary's child-index offset includes node.
		idx := -1
		for i, c := range container.Children {
			if c == node {
				idx = i
				break
			}
		}
		if idx < 0 {
			return 0, false
		}
		if isStart {
			if offset <= idx {
				return 0, true // node starts at-or-after the start boundary: fully included from 0
			}
			return 0, false // start boundary is past this node: node not included
		}
		if offset > idx {
			return len(node.Text), true // node ends at-or-before the end boundary: fully included to the end
		}
		return 0, false // end boundary is at-or-before this node: not included
	}
	return 0, false
}

// resolveSelectionPseudoStyle returns the computed ::selection style for
// originating (the originating element of a text fragment), or nil if no
// selection context is configured or originating is nil. Memoized per
// Render call in r.selectionStyleCache since multiple fragments commonly
// share one originating element (line-wrapped runs of the same <p>).
func (r *Renderer) resolveSelectionPseudoStyle(originating *html.Node) *css.Style {
	if originating == nil || len(r.selectionStylesheets) == 0 {
		return nil
	}
	if r.selectionStyleCache == nil {
		r.selectionStyleCache = make(map[*html.Node]*css.Style)
	}
	if cached, ok := r.selectionStyleCache[originating]; ok {
		return cached
	}
	// originating.Style isn't tracked on *html.Node directly (style lives
	// on layout.Box/css cascade results, not the DOM tree) — but
	// ComputePseudoElementStyle only needs the parent style for
	// INHERITANCE (font-size, etc.), which the originating element's own
	// already-resolved Box carries. We don't have a node→Box index handy
	// here without another lookup, so pass no parentStyles: ::selection's
	// own UA-default-or-author color/background-color (the properties
	// this paint phase reads) never depend on inherited font metrics, and
	// selectionAllowedProperty already restricts the rule to a property
	// set that doesn't need font-relative resolution for color values.
	style := css.ComputePseudoElementStyle(originating, "selection", r.selectionStylesheets, r.selectionViewportW, r.selectionViewportH)
	r.selectionStyleCache[originating] = style
	return style
}

// findOriginatingTextNode finds the DOM text-node child of parent whose
// content, when fragments are consumed in document order, covers the
// given fragment text — returning the text node plus the node-local
// [fragStart, fragEnd) range this fragment's box.Text occupies. Uses
// r.selectionTextConsumed to track how much of each text node's content
// has already been claimed by earlier fragments painted this Render call.
//
// fragmentLen runes... actually bytes: box.Text length in bytes (Go string
// indexing), matching html.Range's StartOffset/EndOffset byte-offset
// convention (mirrors the rest of this engine's UTF-8 byte-offset
// handling — see html.Range's doc comment).
func (r *Renderer) findOriginatingTextNode(parent *html.Node, fragmentText string) (*html.Node, int, int) {
	if parent == nil {
		return nil, 0, 0
	}
	if r.selectionTextConsumed == nil {
		r.selectionTextConsumed = make(map[*html.Node]int)
	}
	fragLen := len(fragmentText)
	for _, child := range parent.Children {
		if child.Type != html.TextNode {
			continue
		}
		consumed := r.selectionTextConsumed[child]
		remaining := len(child.Text) - consumed
		if remaining <= 0 {
			continue
		}
		if fragLen <= remaining {
			fragStart := consumed
			fragEnd := consumed + fragLen
			r.selectionTextConsumed[child] = fragEnd
			return child, fragStart, fragEnd
		}
	}
	// No exact-fit text-node child found (text-transform changed the
	// length, or the fragment spans a generated/pseudo text node not
	// present in the DOM, e.g. ::before content, or a non-text-only
	// child mix this lookup doesn't model). Selection painting is
	// skipped for this fragment — drawText's caller falls back to the
	// single-color path, which is correct-but-unhighlighted rather than
	// wrong.
	return nil, 0, 0
}

// selectionSegment describes one paint segment of a split text run: a
// [start,end) byte range of the original fragment text, plus the resolved
// color overrides to use for that segment (nil overrides mean "use the
// layer's existing values unchanged" — the pre-/post-selection segments).
type selectionSegment struct {
	start, end      int
	selected        bool
	backgroundColor *css.Color // nil = no background rect painted
	textColor       *css.Color // nil = use layer.TextColor
	decorationColor *css.Color // nil = use layer.TextDecorationColor
}

// computeSelectionSegments splits a text fragment into 1-3 segments
// (pre-selection / selected / post-selection) based on the configured
// selection range and the fragment's originating text node. Returns a
// single unselected segment spanning the whole text when there's no
// selection context, no DOM Selection, or no overlap — the common
// fast-path callers should check via len(segs) == 1 && !segs[0].selected
// to skip the splitting machinery entirely.
func (r *Renderer) computeSelectionSegments(box *layout.Box, text string) []selectionSegment {
	whole := []selectionSegment{{start: 0, end: len(text)}}
	if r.selectionRange == nil || box == nil || box.Node == nil || text == "" {
		return whole
	}
	textNode, fragStart, fragEnd := r.findOriginatingTextNode(box.Node, text)
	if textNode == nil {
		return whole
	}
	selStart, selEnd, ok := selectionOverlapForTextNode(r.selectionRange, textNode, fragStart, fragEnd)
	if !ok {
		return whole
	}

	pseudoStyle := r.resolveSelectionPseudoStyle(box.Node)
	var bgColor, fgColor, decColor *css.Color
	if pseudoStyle != nil {
		if bg, ok := pseudoStyle.Get("background-color"); ok {
			if c, ok := css.ParseColorWithCurrentColor(bg, pseudoStyle.GetColor()); ok {
				bgColor = &c
			}
		}
		if cv, ok := pseudoStyle.Get("color"); ok {
			if c, ok := css.ParseColor(cv); ok {
				fgColor = &c
			}
		}
		if dc, hasDC := pseudoStyle.GetTextDecorationColor(); hasDC {
			decColor = &dc
		} else {
			decColor = fgColor
		}
	}

	var segs []selectionSegment
	if selStart > 0 {
		segs = append(segs, selectionSegment{start: 0, end: selStart})
	}
	segs = append(segs, selectionSegment{
		start: selStart, end: selEnd, selected: true,
		backgroundColor: bgColor, textColor: fgColor, decorationColor: decColor,
	})
	if selEnd < len(text) {
		segs = append(segs, selectionSegment{start: selEnd, end: len(text)})
	}
	return segs
}
