package html

// IsHTMLAnchorElement reports whether n is an HTML <a> element
// (IsA<HTMLAnchorElement>, core/html/html_anchor_element.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); an SVG <a> is not one.
func (n *Node) IsHTMLAnchorElement() bool {
	return n != nil && n.Type == ElementNode && n.TagName == "a" && n.NamespaceURI() == XHTMLNamespaceURI
}
