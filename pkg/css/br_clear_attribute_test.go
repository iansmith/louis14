package css

import (
	"testing"

	"louis14/pkg/html"
)

// LOU-470: <br clear> maps to the CSS clear property as a presentational
// hint: "all" is "both", an empty value adds nothing, any other value is
// the clear keyword itself. Mirrors Blink's HTMLBRElement::
// CollectStyleForPresentationAttribute (html/html_br_element.cc:41-60 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBRClearAttributeMapsToClear(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  ClearType
	}{
		{"all", ClearBoth},
		{"ALL", ClearBoth},
		{"both", ClearBoth},
		{"left", ClearLeft},
		{"Right", ClearRight},
		{"", ClearNone},
		{"bogus", ClearNone},
	} {
		node := &html.Node{
			Type:       html.ElementNode,
			TagName:    "br",
			Attributes: map[string]string{"clear": tc.value},
		}
		style := ComputeStyle(node, nil, 800, 600, nil)
		if got := style.GetClear(); got != tc.want {
			t.Errorf("<br clear=%q>: clear = %v, want %v", tc.value, got, tc.want)
		}
	}

	// The attribute is the BR element's only: a div ignores it.
	div := &html.Node{
		Type:       html.ElementNode,
		TagName:    "div",
		Attributes: map[string]string{"clear": "all"},
	}
	if got := ComputeStyle(div, nil, 800, 600, nil).GetClear(); got != ClearNone {
		t.Errorf("<div clear=all>: clear = %v, want none", got)
	}
}
