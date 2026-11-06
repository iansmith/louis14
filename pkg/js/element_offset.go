package js

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// CSSOM View's offset attributes (HTMLElement.offsetParent, offsetLeft,
// offsetTop, offsetWidth, offsetHeight). Each brings layout up to date, as
// Element::OffsetLeft … OffsetHeight do with
// EnsurePaintLocationDataValidForNode (dom/element.cc:2477-2530 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and reads 0 / null for an
// element without a layout box.
//
// louis14 lays boxes out in absolute (canvas) coordinates, so the container
// chain Blink sums up to the offsetParent (AdjustedPositionRelativeTo) is the
// difference of two absolute positions (relative and sticky offsets are in
// the boxes, as in Blink's locations), with the scroll offsets a scroller
// translated its contents by added back. Not ported: an inline element's
// offsets (its line boxes have no entry in the node→box index, so it reads
// 0 and null), an inline offsetParent (its first line box's top left), and
// the effective-zoom stop and zoom adjustment.

// offsetLayout brings layout up to date and returns the node→box index and
// the element's box; box is nil without a layout updater or a box.
func (e *elementAccessor) offsetLayout() (map[*html.Node]*layout.Box, *layout.Box) {
	if e.ctx.updateLayout == nil {
		return nil, nil
	}
	boxes := e.ctx.updateLayout()
	return boxes, boxes[e.node]
}

// offsetParent mirrors LayoutObject::OffsetParent (layout/layout_object.cc:
// 4626-4697 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for an element with
// box: null for the document element and body; otherwise the nearest layout
// ancestor that can contain absolutely positioned objects — fixed ones, for
// a fixed element, with null when none does — or body, or for a
// non-positioned element a table or table cell.
func offsetParent(box *layout.Box) *html.Node {
	n := box.LayoutNode
	if n == nil || n.IsDocumentElement() || isBodyElement(box.Node) {
		return nil
	}
	inFixed := box.Position == css.PositionFixed
	positioned := box.Position != css.PositionStatic
	for anc := n.Parent(); anc != nil; anc = anc.Parent() {
		node := anc.DOMNode
		if anc.IsAnonymous() || node == nil || node.Type != html.ElementNode {
			continue
		}
		if inFixed {
			if anc.IsFixedContainer() {
				return node
			}
		} else {
			if anc.IsAbsoluteContainer() || isBodyElement(node) {
				return node
			}
			if !positioned && (node.TagName == "table" || node.TagName == "td" || node.TagName == "th") {
				return node
			}
		}
	}
	// Blink's walk ends at the LayoutView, whose node is the document, not an
	// element; louis14's layout tree ends at the document element.
	return nil
}

// offsetPoint mirrors LayoutBoxModelObject::AdjustedPositionRelativeTo
// (layout/layout_box_model_object.cc:754-812 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): box's border-box position
// relative to parent's padding edge; relative to the canvas when parent is
// null, and to the document element's border box when parent is a
// non-positioned body (the chain stops at body and adds its location).
// Blink's locations are unscrolled: the scroll offsets of the scrollers from
// box's container up to and including parent are added back.
func (e *elementAccessor) offsetPoint(boxes map[*html.Node]*layout.Box, box *layout.Box, parent *html.Node) (x, y float64) {
	// Only body and the LayoutView (louis14 has none) read zero; the
	// document element, with no offsetParent, reads its location.
	if isBodyElement(box.Node) {
		return 0, 0
	}
	pbox := boxes[parent]
	x, y = box.X, box.Y
	for c := box.Parent; c != nil && c.Parent != nil; c = c.Parent {
		dx, dy := layout.ScrolledContentOffset(c.ScrollOffset)
		x, y = x+dx, y+dy
		if c == pbox {
			break
		}
	}
	switch {
	case pbox == nil:
		return x, y
	case isBodyElement(parent) && pbox.Position == css.PositionStatic:
		if root := boxes[documentElement(e.ctx.doc)]; root != nil {
			x, y = x-root.X, y-root.Y
		}
		return x, y
	}
	return x - pbox.X - pbox.Border.Left, y - pbox.Y - pbox.Border.Top
}

// offsetParentNode is HTMLElement.offsetParent's node, or nil.
func (e *elementAccessor) offsetParentNode() *html.Node {
	if _, box := e.offsetLayout(); box != nil {
		return offsetParent(box)
	}
	return nil
}

// offsetInt is HTMLElement.offsetLeft, offsetTop, offsetWidth or
// offsetHeight: the LayoutUnit value rounded as LayoutUnit::Round does
// (Element::OffsetLeft … OffsetHeight, dom/element.cc:2477-2522 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The size is not snapped against
// the position: OffsetWidth/OffsetHeight are the bounding box size
// (layout_box_model_object.h:147-154 @ a9f50e52).
func (e *elementAccessor) offsetInt(name string) int {
	boxes, box := e.offsetLayout()
	if box == nil {
		return 0
	}
	var v float64
	switch name {
	case "offsetWidth":
		v = box.Width
	case "offsetHeight":
		v = box.Height
	default:
		x, y := e.offsetPoint(boxes, box, offsetParent(box))
		v = y
		if name == "offsetLeft" {
			v = x
		}
	}
	return int(layoutunit.FromFloat64Round(v).Round())
}

func isBodyElement(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && n.TagName == "body"
}
