package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// focusRingRectCollector gathers a focus ring's outline rects the way
// Blink's PhysicalBoxFragment::AddOutlineRects family does for
// OutlineType::kIncludeBlockInkOverflow (core/layout/
// physical_box_fragment.cc:1249-1532 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14's line boxes hold
// inline content flat — an inline box's fragment, its text and its atomic
// inlines are siblings on the line — so the cursor walks filter line items
// by DOM node instead of descending into the inline box's children.
//
// With anchor (OutlineType::kIncludeBlockInkOverflowForAnchor, a focused
// <a>, :1232-1235) out-of-flow descendants count too: a block's own
// (:1290-1300) and, for an inline anchor, its container's whose containing
// block it is (:1372-1386).
//
// rects[:own] are the focused element's own rects. With prune, a
// descendant rect inside one of them is skipped: it leaves the union, and
// so the ring, unchanged when outline-offset outsets every rect alike (a
// negative offset is clamped per rect by AdjustedOutlineOffset, so then
// every rect counts).
type focusRingRectCollector struct {
	r      *Renderer
	rects  []geometry.RectF
	own    int
	prune  bool
	anchor bool
	// toFocused maps rects of the descendant being walked to the focused
	// element's space: the composed transforms of the transformed boxes
	// between them (nil when there are none).
	toFocused *geometry.Transform
}

// focusRingOutlineRects returns the outline rects of an outline-style: auto
// layer: for a block (or atomic inline) its self outline rects
// (AddSelfOutlineRects, :1220-1239), for an inline box the rects of every
// non-empty fragment plus each fragment's descendants
// (AddOutlineRectsForInlineBox, :1306-1386). HasControlClip needs no port: with SelectUsesUAClip stable
// (runtime_enabled_features.json5:5533-5535) it is always false.
func (r *Renderer) focusRingOutlineRects(l *PaintLayer) []geometry.RectF {
	c := focusRingRectCollector{r: r, prune: l.OutlineOffset >= 0, anchor: l.Box.Node.IsHTMLAnchorElement()}
	if !isInlineBoxFragment(l.Box) {
		c.addOwnRect(l.Box)
		c.own = len(c.rects)
		c.addNormalChildren(l.Box)
		return c.rects
	}
	frags := l.outlineFragments()
	for _, f := range frags {
		if f.Width != 0 || f.Height != 0 {
			c.rects = append(c.rects, boxRectF(f))
		}
	}
	c.own = len(c.rects)
	walked := map[*layout.Box]bool{}
	for _, f := range frags {
		if line := f.Parent; line != nil && line.IsLineBox && !walked[line] {
			walked[line] = true
			c.addLineItems(line.Children, l.Box.Node)
		}
	}
	// AddOutlineRectsForInlineBox returns before the anchor's out-of-flow
	// pass when the inline collected no rects (:1362-1364).
	if c.anchor && len(c.rects) > 0 && !l.Box.HasNonVisibleOverflow() {
		containers := map[*layout.Box]bool{}
		for _, f := range frags {
			if f.Parent == nil || f.Parent.Parent == nil || containers[f.Parent.Parent] {
				continue
			}
			container := f.Parent.Parent
			containers[container] = true
			for _, oof := range container.Children {
				if oof != nil && isOutOfFlow(oof) && r.absoluteContainer(oof.Node) == l.Box.Node {
					c.addDescendantBox(oof)
				}
			}
		}
	}
	return c.rects
}

// addOwnRect adds b's rect unless b is an anonymous block, whose children
// add rects instead (PhysicalBoxFragment::AddOutlineRects, :1263-1272).
func (c *focusRingRectCollector) addOwnRect(b *layout.Box) {
	if b.Node != nil {
		c.add(boxRectF(b))
	}
}

// addNormalChildren adds b's children's rects unless b clips its overflow
// (:1275-1276; AddOutlineRectsForNormalChildren, :1388-1417): a line box's
// items through the cursor walk, any other in-flow child as a descendant,
// and for an anchor the out-of-flow children too (:1290-1300).
func (c *focusRingRectCollector) addNormalChildren(b *layout.Box) {
	if b.HasNonVisibleOverflow() {
		return
	}
	for _, child := range b.Children {
		switch {
		case child == nil:
		case child.IsLineBox:
			c.addLineItems(child.Children, nil)
		case isOutOfFlow(child):
			if c.anchor {
				c.addDescendantBox(child)
			}
		default:
			c.addDescendant(child)
		}
	}
}

// addLineItems is AddOutlineRectsForCursor (:1419-1477) over one line's
// items. A text item counts unless an inline box fragment on the line
// encloses it: Blink reaches such an inline box's descendants through the
// legacy LayoutInline::AddOutlineRectsForNormalChildren, which skips text
// (layout_box_model_object.cc:386-429), while a culled inline box (no box
// fragment, LayoutInline::ComputeInitialShouldCreateBoxFragment,
// layout_inline.cc:179-227) leaves its text items on the line. With inline
// set the cursor is that focused inline's descendants: only items of its
// subtree count, and only inline boxes inside it enclose.
func (c *focusRingRectCollector) addLineItems(items []*layout.Box, inline *html.Node) {
	boxes := map[*html.Node]bool{}
	for _, item := range items {
		if item != nil && isInlineBoxFragment(item) {
			boxes[item.Node] = true
		}
	}
	for _, item := range items {
		if item == nil || (inline != nil && !isInclusiveAncestor(inline, item.Node)) {
			continue
		}
		if !isTextFragment(item) {
			c.addDescendant(item)
			continue
		}
		if !enclosedByInlineBox(item.Node, inline, boxes) {
			c.add(boxRectF(item))
		}
	}
}

// enclosedByInlineBox reports whether one of boxes is n or an ancestor of n
// below stop (the focused inline, nil for a block's cursor).
func enclosedByInlineBox(n, stop *html.Node, boxes map[*html.Node]bool) bool {
	for ; n != nil && n != stop; n = n.Parent {
		if boxes[n] {
			return true
		}
	}
	return false
}

// addDescendant is AddOutlineRectsForDescendant (:1479-1532) behind
// AddOutlineRectsForNormalChildren's out-of-flow skip (:1409-1413).
func (c *focusRingRectCollector) addDescendant(d *layout.Box) {
	if !isOutOfFlow(d) {
		c.addDescendantBox(d)
	}
}

// addDescendantBox is AddOutlineRectsForDescendant: list markers add
// nothing; an inline box adds only its non-text descendants, which sit on
// the line beside it and the line walk reaches; any other box adds its own
// rect and its children's.
func (c *focusRingRectCollector) addDescendantBox(d *layout.Box) {
	if (d.LayoutNode != nil && d.LayoutNode.IsMarkerNode()) || isInlineBoxFragment(d) {
		return
	}
	if d.Style == nil || !d.Style.HasTransform() {
		c.addOwnRect(d)
		c.addNormalChildren(d)
		return
	}
	// A transformed descendant (its layer's HasLayer branch, :1499-1510)
	// collects its rects in its own space and maps each to the focused
	// element's (VectorOutlineRectCollector::Combine → LocalToAncestorRect,
	// core/layout/outline_rect_collector.cc:49-61), which composes every
	// transform on the way before taking one bounding box. Box geometry is
	// untransformed and localTransformMatrix works in those coordinates.
	outer := c.toFocused
	m := localTransformMatrix(d)
	if outer != nil {
		m = outer.Mul(m)
	}
	c.toFocused = &m
	c.addOwnRect(d)
	c.addNormalChildren(d)
	c.toFocused = outer
}

func (c *focusRingRectCollector) add(r geometry.RectF) {
	if c.toFocused != nil {
		r = c.toFocused.MapRect(r)
	}
	if c.prune {
		for _, o := range c.rects[:c.own] {
			if o.Union(r) == o {
				return
			}
		}
	}
	c.rects = append(c.rects, r)
}

// isOutOfFlow reports whether b is absolutely or fixed positioned.
func isOutOfFlow(b *layout.Box) bool {
	return b.Position == css.PositionAbsolute || b.Position == css.PositionFixed
}

// absoluteContainer approximates LayoutObject::ContainerForAbsolutePosition
// (core/layout/layout_object.cc:1752-1758 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for n: its nearest ancestor
// whose computed style is positioned. Transforms, filters, backdrop
// filters, will-change and containment also make a container; layout
// resolves the real one (OutOfFlowCandidate's container) but does not carry
// it onto the out-of-flow box, so they are not considered here.
func (r *Renderer) absoluteContainer(n *html.Node) *html.Node {
	for a := n; a != nil && a.Parent != nil; {
		a = a.Parent
		if s := elementStyle(r.nodeBoxIndex[a]); s != nil && s.GetPosition() != css.PositionStatic {
			return a
		}
	}
	return nil
}

// elementStyle is the computed style of b's element. A positioned inline's
// box fragments paint with an in-flow clone of it (position static,
// CloneInFlowFragment); its paint group item keeps the element's own.
func elementStyle(b *layout.Box) *css.Style {
	if b == nil {
		return nil
	}
	if b.IsOwnPaintGroupFragment() {
		return b.PaintGroup.Style
	}
	return b.Style
}

// isTextFragment reports whether b is a text fragment's box: no layout node
// of its own, and text.
func isTextFragment(b *layout.Box) bool {
	return b.LayoutNode == nil && b.Text != ""
}

// isInlineBoxFragment reports whether b is a fragment of a non-atomic inline
// box (Blink's LayoutInline): an element box that is a non-atomic inline.
func isInlineBoxFragment(b *layout.Box) bool {
	return b != nil && !b.IsLineBox && !isTextFragment(b) && b.Node != nil && layout.IsLayoutInline(b.Style, b.Node)
}

// isInclusiveAncestor reports whether a is n or one of its ancestors (an
// upward walk; html.Node.Contains searches a's subtree instead).
func isInclusiveAncestor(a, n *html.Node) bool {
	for ; n != nil; n = n.Parent {
		if n == a {
			return true
		}
	}
	return false
}
