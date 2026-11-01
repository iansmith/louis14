package layout

import "louis14/pkg/html"

// The view-background predicates of Blink's LayoutBoxModelObject
// (core/layout/layout_box_model_object.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), kept on louis14's
// LayoutInputNode, which stands in for the layout object (see
// layout_input_node.go).

// IsDocumentElement reports whether n is the layout object of the
// document element: the root of the persistent layout tree. Mirrors
// LayoutObject::IsDocumentElement (layout_object.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Per-layout anonymous wrappers
// and outside ::marker nodes also have no layout parent, so they are
// excluded explicitly.
func (n *LayoutInputNode) IsDocumentElement() bool {
	return n != nil && n.parent == nil && n.IsElement() && !n.isAnonymous && !n.isMarkerNode
}

// DocumentElement returns the layout object of n's document element (the
// root of n's layout tree): GetDocument().documentElement()->
// GetLayoutObject().
func (n *LayoutInputNode) DocumentElement() *LayoutInputNode {
	root := n
	for root != nil && root.parent != nil {
		root = root.parent
	}
	if !root.IsDocumentElement() {
		return nil
	}
	return root
}

// FirstBodyElement returns the layout object of the document's first
// <body> child of the <html> document element, or nil when there is none or
// it generates no layout object (display:none). Mirrors
// Document::FirstBodyElement (core/dom/document.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) followed by GetLayoutObject().
func (n *LayoutInputNode) FirstBodyElement() *LayoutInputNode {
	root := n.DocumentElement()
	if root == nil || root.TagName() != "html" {
		return nil
	}
	var body *html.Node
	for _, c := range root.DOMNode.Children {
		if c != nil && c.Type == html.ElementNode && c.TagName == "body" {
			body = c
			break
		}
	}
	if body == nil {
		return nil
	}
	return root.layoutObjectFor(body)
}

// layoutObjectFor returns the node in n's subtree whose DOM node is dom.
func (n *LayoutInputNode) layoutObjectFor(dom *html.Node) *LayoutInputNode {
	if n.DOMNode == dom {
		return n
	}
	for _, c := range n.children {
		if found := c.layoutObjectFor(dom); found != nil {
			return found
		}
	}
	return nil
}

// ShouldStopBodyPropagation reports whether the html or body element n
// stops the body's background (and the other viewport-propagated
// properties) from propagating: it has no layout object, or containment
// applies to it. Mirrors StyleResolver::ShouldStopBodyPropagation
// (core/css/resolver/style_resolver.cc:3394-3414 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), placed here because it reads
// the layout object.
func ShouldStopBodyPropagation(n *LayoutInputNode) bool {
	if n == nil {
		return true
	}
	style := n.Style()
	return style != nil && style.HasAnyContainment()
}

// BackgroundTransfersToView reports whether n's background is painted by
// the view instead of by n: always for the document element, and for the
// first <body> of an <html> document element that has no background of its
// own, when containment applies to neither. The box painters skip such a
// background (BoxFragmentPainter::PaintBackground,
// paint/box_fragment_painter.cc:1923). Mirrors
// LayoutBoxModelObject::BackgroundTransfersToView
// (core/layout/layout_box_model_object.cc:1095-1125 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (n *LayoutInputNode) BackgroundTransfersToView() bool {
	if n.IsDocumentElement() {
		return true
	}
	// FirstBodyElement is nil unless the document element is <html>.
	if n == nil || n != n.FirstBodyElement() {
		return false
	}
	root := n.DocumentElement()
	return !root.Style().HasBackground() && !ShouldStopBodyPropagation(root) && !ShouldStopBodyPropagation(n)
}
