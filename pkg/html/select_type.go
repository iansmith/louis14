package html

import "math"

// <select> flat-tree slotting. Blink gives <select> a UA shadow root whose
// slots are manually assigned (SelectType::ManuallyAssignSlots); a light-DOM
// child that is not assigned to a slot is not in the flat tree and so gets
// no layout object. louis14 has no shadow DOM, so the layout tree builder
// applies the same assignment predicate directly when collecting a select's
// children.

// CanAssignToSelectSlot reports whether n is an <option>, <optgroup> or <hr>
// — the children every select type slots. Mirrors CanAssignToSelectSlot
// (core/html/forms/select_type.cc:114-121 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); Node::HasTagName matches the
// HTML namespace only.
func CanAssignToSelectSlot(n *Node) bool {
	if n.Type != ElementNode || n.NamespaceURI() != XHTMLNamespaceURI {
		return false
	}
	switch n.TagName {
	case "option", "optgroup", "hr":
		return true
	}
	return false
}

// CanAssignToCustomizableSelectSlot reports whether n is valid in <select>'s
// customizable-select content model: option, optgroup, hr, span or div.
// Mirrors CanAssignToCustomizableSelectSlot (select_type.cc:123-129 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func CanAssignToCustomizableSelectSlot(n *Node) bool {
	if n.Type != ElementNode || n.NamespaceURI() != XHTMLNamespaceURI {
		return false
	}
	switch n.TagName {
	case "option", "optgroup", "hr", "span", "div":
		return true
	}
	return false
}

// UsesMenuList reports whether the <select> element n renders as a menulist
// (popup) rather than an in-page list box. Mirrors
// HTMLSelectElement::UpdateUsesMenuList (html_select_element.cc:219-235 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a multiple select is a menulist
// only with an explicit size of 1; otherwise a select is a menulist when its
// size is <= 1. An unparsable size is 0 (ParseAttribute, :424-430).
func (n *Node) UsesMenuList() bool {
	sizeAttr, hasSize := n.GetAttribute("size")
	size, _ := parseHTMLNonNegativeInteger(sizeAttr) // 0 on failure
	if _, multiple := n.GetAttribute("multiple"); multiple {
		return hasSize && size == 1
	}
	return size <= 1
}

// parseHTMLNonNegativeInteger implements the HTML "rules for parsing
// non-negative integers" (HTML §2.3.4.1): skip leading ASCII whitespace,
// accept an optional sign, then one or more ASCII digits; trailing garbage is
// ignored. A negative value fails unless it is zero ("-0"), and a value that
// overflows an unsigned 32-bit int fails. Mirrors Blink's
// ParseHTMLNonNegativeInteger (core/html/parser/html_parser_idioms.cc:175-222
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func parseHTMLNonNegativeInteger(s string) (uint32, bool) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\f' || s[i] == '\r') {
		i++
	}
	negative := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		negative = s[i] == '-'
		i++
	}
	start := i
	var v uint64
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		v = v*10 + uint64(s[i]-'0')
		if v > math.MaxUint32 {
			return 0, false
		}
		i++
	}
	if i == start || (negative && v != 0) {
		return 0, false
	}
	return uint32(v), true
}
