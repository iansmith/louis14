package layout

// ShouldBlockContainerChildStretchAutoInlineSize reports whether a block
// container stretches child's auto inline-size (rather than giving it
// fit-content). Only called for a child in a writing mode parallel with its
// container. Not stretched: replaced elements, tables, and the form controls
// <button>, <input>, <select> and <textarea>. Mirrors Blink's
// ShouldBlockContainerChildStretchAutoInlineSize (space_utils.cc:82-104 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// louis14's IsReplacedElement covers the form controls too, standing in for
// Blink's separate form-control check (:89-102). That check exempts a
// control with a supported base appearance; louis14 does not parse
// `appearance`, so no control is exempt.
func ShouldBlockContainerChildStretchAutoInlineSize(child *LayoutInputNode) bool {
	if child.DOMNode != nil && IsReplacedElement(child.DOMNode) {
		return false
	}
	if style := child.Style(); style != nil && isNonStretchableDisplay(style) {
		return false
	}
	return true
}
