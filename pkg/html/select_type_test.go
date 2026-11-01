package html

import "testing"

// UsesMenuList mirrors HTMLSelectElement::UpdateUsesMenuList
// (html_select_element.cc:219-235 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestSelect_UsesMenuList(t *testing.T) {
	cases := []struct {
		attrs map[string]string
		want  bool
	}{
		{nil, true},
		{map[string]string{"size": "1"}, true},
		{map[string]string{"size": "0"}, true},
		{map[string]string{"size": "4"}, false},
		{map[string]string{"size": " +4px"}, false},
		{map[string]string{"size": "-4"}, true},
		{map[string]string{"size": "bogus"}, true},
		{map[string]string{"size": "99999999999"}, true},
		{map[string]string{"multiple": ""}, false},
		{map[string]string{"multiple": "", "size": "1"}, true},
		{map[string]string{"multiple": "", "size": "0"}, false},
		{map[string]string{"multiple": "", "size": "4"}, false},
	}
	for _, c := range cases {
		n := &Node{Type: ElementNode, TagName: "select", Attributes: c.attrs}
		if got := n.UsesMenuList(); got != c.want {
			t.Errorf("UsesMenuList(%v) = %v, want %v", c.attrs, got, c.want)
		}
	}
}

func TestSelect_CanAssignToSelectSlot(t *testing.T) {
	for tag, want := range map[string][2]bool{
		"option": {true, true}, "optgroup": {true, true}, "hr": {true, true},
		"span": {false, true}, "div": {false, true}, "p": {false, false},
	} {
		n := &Node{Type: ElementNode, TagName: tag}
		if got := CanAssignToSelectSlot(n); got != want[0] {
			t.Errorf("CanAssignToSelectSlot(<%s>) = %v, want %v", tag, got, want[0])
		}
		if got := CanAssignToCustomizableSelectSlot(n); got != want[1] {
			t.Errorf("CanAssignToCustomizableSelectSlot(<%s>) = %v, want %v", tag, got, want[1])
		}
	}
	text := &Node{Type: TextNode, Text: "option"}
	if CanAssignToSelectSlot(text) || CanAssignToCustomizableSelectSlot(text) {
		t.Error("a text node must not be assignable to a select slot")
	}
}
