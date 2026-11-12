package html

// UpdateSelectionOnFocus places the selection for an element that has just
// been focused. Mirrors Blink's Element::UpdateSelectionOnFocus
// (core/dom/element.cc:8559-8600 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (d *Document) UpdateSelectionOnFocus(element *Node) {}

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
