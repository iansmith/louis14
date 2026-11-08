package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestAllAppliesAtSourcePosition: within one declaration block, `all`
// applies at its own source position. It replaces the declarations of the
// properties it covers that precede it, and later ones override it
// (declarations kept in source order in CSSPropertyValueSet; All::
// ParseShorthand sets every longhand but direction and unicode-bidi @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An earlier !important
// declaration still beats a later normal `all` (CSS Cascade 4 §6.4).
func TestAllAppliesAtSourcePosition(t *testing.T) {
	for _, tc := range []struct{ decl, prop, want string }{
		{"margin-left:5px; all:initial", "margin-left", ""},
		{"all:initial; margin-left:5px", "margin-left", "5px"},
		{"color:red; all:inherit", "color", "green"},
		{"all:inherit; color:red", "color", "red"},
		{"margin-left:5px !important; all:initial", "margin-left", "5px"},
		{"margin-left:5px; all:initial !important", "margin-left", ""},
		{"margin-left:5px !important; all:initial !important", "margin-left", ""},
		{"direction:rtl; all:initial", "direction", "rtl"},
		{"--x:1; all:initial", "--x", "1"},
	} {
		doc, _ := html.Parse(`<style>.p{color:green} .t{` + tc.decl + `}</style><div class=p><div class=t></div></div>`)
		found := false
		for n, st := range ApplyStylesToDocument(doc, 800, 600) {
			if cls, _ := n.GetAttribute("class"); n.Type == html.ElementNode && cls == "t" {
				found = true
				if got, _ := st.Get(tc.prop); got != tc.want {
					t.Errorf("%s: %s = %q, want %q", tc.decl, tc.prop, got, tc.want)
				}
			}
		}
		if !found {
			t.Fatalf("%s: element not found", tc.decl)
		}
	}
}
