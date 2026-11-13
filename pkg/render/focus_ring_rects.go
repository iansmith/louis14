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
// rects[:own] are the focused element's own rects. With prune, a
// descendant rect inside one of them is skipped: it leaves the union, and
// so the ring, unchanged when outline-offset outsets every rect alike (a
// negative offset is clamped per rect by AdjustedOutlineOffset, so then
// every rect counts).
type focusRingRectCollector struct {
	rects []geometry.RectF
	own   int
	prune bool
}

// focusRingOutlineRects returns the outline rects of an outline-style: auto
// layer: for a block (or atomic inline) its self outline rects
// (AddSelfOutlineRects, :1220-1239), for an inline box the rects of every
// non-empty fragment plus each fragment's descendants
// (AddOutlineRectsForInlineBox, :1306-1386). Not ported: an <a>'s
// out-of-flow descendants (kIncludeBlockInkOverflowForAnchor, :1232-1235),
// form controls' control clip (HasControlClip), and transforms on layered
// descendants (Blink maps them through LocalToAncestorRects).
func focusRingOutlineRects(l *PaintLayer) []geometry.RectF {
	c := focusRingRectCollector{prune: l.OutlineOffset >= 0}
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
// items through the cursor walk, any other child as a descendant.
func (c *focusRingRectCollector) addNormalChildren(b *layout.Box) {
	if b.HasNonVisibleOverflow() {
		return
	}
	for _, child := range b.Children {
		if child == nil {
			continue
		}
		if child.IsLineBox {
			c.addLineItems(child.Children, nil)
			continue
		}
		c.addDescendant(child)
	}
}

// addLineItems is AddOutlineRectsForCursor (:1419-1477) over one line's
// items. With inline nil the cursor is a block's: a text item counts unless
// it lies inside an inline box, whose descendants Blink reaches through
// the legacy LayoutInline::AddOutlineRectsForNormalChildren that skips text
// (layout_box_model_object.cc:386-429). With inline set the cursor is that
// focused inline's descendants: only items of its subtree count, and of its
// text only its own.
func (c *focusRingRectCollector) addLineItems(items []*layout.Box, inline *html.Node) {
	for _, item := range items {
		if item == nil || (inline != nil && !isInclusiveAncestor(inline, item.Node)) {
			continue
		}
		if !isTextFragment(item) {
			c.addDescendant(item)
			continue
		}
		own := item.Node == inline
		if inline == nil {
			own = !isNonAtomicInline(item.Style, item.Node)
		}
		if own {
			c.add(boxRectF(item))
		}
	}
}

// addDescendant is AddOutlineRectsForDescendant (:1479-1532) behind
// AddOutlineRectsForNormalChildren's out-of-flow skip (:1409-1413): list
// markers add nothing; an inline box adds only its non-text descendants,
// which sit on the line beside it and the line walk reaches; any other box
// adds its own rect and its children's.
func (c *focusRingRectCollector) addDescendant(d *layout.Box) {
	if d.Position == css.PositionAbsolute || d.Position == css.PositionFixed ||
		(d.LayoutNode != nil && d.LayoutNode.IsMarkerNode()) || isInlineBoxFragment(d) {
		return
	}
	c.addOwnRect(d)
	c.addNormalChildren(d)
}

func (c *focusRingRectCollector) add(r geometry.RectF) {
	if c.prune {
		for _, o := range c.rects[:c.own] {
			if o.Union(r) == o {
				return
			}
		}
	}
	c.rects = append(c.rects, r)
}

// isTextFragment reports whether b is a text fragment's box: no layout node
// of its own, and text.
func isTextFragment(b *layout.Box) bool {
	return b.LayoutNode == nil && b.Text != ""
}

// isInlineBoxFragment reports whether b is a fragment of a non-atomic inline
// box (Blink's LayoutInline): an element box that is a non-atomic inline.
func isInlineBoxFragment(b *layout.Box) bool {
	return b != nil && !b.IsLineBox && !isTextFragment(b) && b.Node != nil && isNonAtomicInline(b.Style, b.Node)
}

// isNonAtomicInline is layout's PhysicalFragment::IsInlineBox test for a
// style and its node: display inline, ruby or ruby-text on an element that
// is neither replaced nor a form control, or an inline list-item.
func isNonAtomicInline(s *css.Style, n *html.Node) bool {
	return s != nil && (!layout.IsTransformableBox(s, n) || s.IsInlineBoxListItem())
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
