package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// A list-box <select> (size > 1, or multiple without size) only puts its
// option / optgroup / hr / span / div children in the flat tree: its UA
// shadow root has one manually-assigned slot, and
// ListBoxSelectType::ManuallyAssignSlots assigns only children passing
// CanAssignToSelectSlot or CanAssignToCustomizableSelectSlot
// (core/html/forms/select_type.cc:114-129, :2002-2033 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Any other child (text, <p>,
// ...) is unslotted, so it gets no layout object. WPT
// css-display/select-4-option-optgroup-display-none.html
// (`<select size=4>select</select>` must paint no text).
func TestLayoutTree_ListBoxSelectSkipsUnslottableChildren(t *testing.T) {
	text := makeTextNode("select")
	option := makeNode("option", makeTextNode("o"))
	optgroup := makeNode("optgroup")
	hr := makeNode("hr")
	span := makeNode("span")
	div := makeNode("div")
	p := makeNode("p")
	sel := makeNode("select", text, option, optgroup, hr, span, div, p)
	sel.Attributes = map[string]string{"size": "4"}
	root := makeNode("div", sel)

	block := makeStyle("display", "block")
	styles := map[*html.Node]*css.Style{
		root:     block,
		sel:      makeStyle("display", "inline-block"),
		option:   block,
		optgroup: block,
		hr:       block,
		span:     makeStyle("display", "inline"),
		div:      block,
		p:        block,
	}
	tree := buildTestTree(root, styles)

	selNode := findLayoutNode(tree, sel)
	if selNode == nil {
		t.Fatal("select layout node not found")
	}
	if findLayoutNode(selNode, text) != nil {
		t.Errorf("text child of list-box <select> got a layout node; want none (not slotted)")
	}
	if findLayoutNode(selNode, p) != nil {
		t.Errorf("<p> child of list-box <select> got a layout node; want none (not slotted)")
	}
	for _, c := range []*html.Node{option, optgroup, hr, span, div} {
		if findLayoutNode(selNode, c) == nil {
			t.Errorf("<%s> child of list-box <select> has no layout node; want one (slotted)", c.TagName)
		}
	}
}
