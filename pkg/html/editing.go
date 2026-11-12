package html

import "strings"

// IsEditable reports whether node is editable content: its nearest
// element, or that element's nearest ancestor, with a contenteditable
// attribute has a value other than "false". Stands in for Blink's
// IsEditable (core/editing/editing_utilities.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which reads the computed
// -webkit-user-modify that html.css derives from the same attribute;
// louis14 has no user-modify property or designMode.
func IsEditable(node *Node) bool {
	for e := node; e != nil; e = e.Parent {
		if e.Type != ElementNode {
			continue
		}
		v, ok := e.GetAttribute("contenteditable")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "true", "plaintext-only":
			return true
		case "false":
			return false
		}
		// An invalid value inherits from the parent.
	}
	return false
}

// RootEditableElementOf returns the outermost editable element containing
// node, or nil when node is not editable. Mirrors RootEditableElement
// (core/editing/editing_utilities.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func RootEditableElementOf(node *Node) *Node {
	var root *Node
	for e := node; e != nil && IsEditable(e); e = e.Parent {
		if e.Type == ElementNode {
			root = e
		}
	}
	return root
}

// IsRootEditableElement reports whether node is an editable element whose
// parent is not editable. Mirrors IsRootEditableElement
// (core/editing/editing_utilities.cc).
func IsRootEditableElement(node *Node) bool {
	return node != nil && node.Type == ElementNode && RootEditableElementOf(node) == node
}

// UpdateSelectionOnFocus places the selection for an element that has just
// been focused: a root editable element gets a caret at its first position,
// unless the selection is already in it. Mirrors Blink's
// Element::UpdateSelectionOnFocus (core/dom/element.cc:8559-8600 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), whose
// FirstPositionInOrBeforeNode is (element, 0) for an element that can hold
// children. The non-editable branch (restoring a text control's selection)
// is not ported.
func (d *Document) UpdateSelectionOnFocus(element *Node) {
	if !IsRootEditableElement(element) {
		return
	}
	if d.Selection != nil && RootEditableElementOf(d.Selection.StartContainer) == element {
		return
	}
	d.Selection = &Range{StartContainer: element, StartOffset: 0, EndContainer: element, EndOffset: 0}
}

// ElementByID returns the first element in n's subtree, n included, in tree
// order whose id is id, or nil. Mirrors TreeScope::getElementById's result
// for a connected tree (core/dom/tree_scope.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (n *Node) ElementByID(id string) *Node {
	if n == nil {
		return nil
	}
	if n.Type == ElementNode {
		if v, ok := n.GetAttribute("id"); ok && v == id {
			return n
		}
	}
	for _, c := range n.Children {
		if f := c.ElementByID(id); f != nil {
			return f
		}
	}
	return nil
}
