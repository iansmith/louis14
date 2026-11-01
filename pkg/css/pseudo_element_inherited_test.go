package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestPseudoElementInheritsEveryInheritedProperty pins that a ::before /
// ::after pseudo-element inherits the same property set as an ordinary child
// element: Blink resolves a pseudo-element's style with its originating
// element as the parent (StyleResolver::ResolveStyle with a
// PseudoElement's StyleRequest), so every `inherited: true` property in
// css_properties.json5 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b inherits —
// there is no separate pseudo-element list. louis14's computePseudoElementStyle
// kept its own 33-property list (LOU-509), so these inherited properties
// (all `inherited: true` in Blink) were dropped on pseudo-elements.
func TestPseudoElementInheritsEveryInheritedProperty(t *testing.T) {
	parent := NewStyle()
	want := map[string]string{
		"cursor":                      "pointer",
		"quotes":                      `"<" ">"`,
		"empty-cells":                 "hide",
		"border-collapse":             "collapse",
		"font-kerning":                "none",
		"font-variant-ligatures":      "no-common-ligatures",
		"text-decoration-skip-ink":    "none",
		"color-interpolation-filters": "srgb",
	}
	for prop, val := range want {
		parent.Set(prop, val)
	}
	doc, _ := html.Parse(`<body><div class="o"></div></body>`)
	var node *html.Node
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if cls, _ := n.GetAttribute("class"); cls == "o" {
			node = n
		}
		for _, c := range n.Children {
			find(c)
		}
	}
	find(doc.Root)
	if node == nil {
		t.Fatal("originating element not found")
	}
	pseudo := computePseudoElementStyle(node, "before", nil, 800, 600, nil, parent)
	for prop, val := range want {
		if got, _ := pseudo.Get(prop); got != val {
			t.Errorf("::before %s = %q, want %q (inherited from originating element)", prop, got, val)
		}
	}
}

// TestInheritedSetFollowsBlink pins properties Blink marks `inherited: true`
// in css_properties.json5 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b
// (caption-side :2994, fill :3496, stroke-width :6072, text-align-last
// :6142, font-variant-caps :1608, image-rendering :4025) that louis14's
// hand-kept inheritableProperties map omitted, so a child fell back to the
// initial value instead of its parent's.
func TestInheritedSetFollowsBlink(t *testing.T) {
	want := map[string]string{
		"caption-side":      "bottom",
		"fill":              "red",
		"stroke-width":      "4px",
		"text-align-last":   "center",
		"font-variant-caps": "small-caps",
		"image-rendering":   "pixelated",
	}
	decls := ""
	for prop, val := range want {
		decls += prop + ": " + val + "; "
	}
	doc, _ := html.Parse(`<style>.parent { ` + decls + ` }</style><body><div class="parent"><div class="child"></div></div></body>`)
	styles := ApplyStylesToDocument(doc, 800, 600)
	var child *Style
	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			child = style
		}
	}
	if child == nil {
		t.Fatal("child not styled")
	}
	for prop, val := range want {
		if got, _ := child.Get(prop); got != val {
			t.Errorf("child %s = %q, want %q (inherited)", prop, got, val)
		}
	}
}

// TestInitialOnNewlyInheritedProperty pins that `initial` on a property that
// became inherited with the Blink inherited set computes the property's
// initial value rather than re-inheriting the parent's (the
// delete-then-inherit hazard nonDefaultInitialValues documents). WPT
// css-pseudo/marker-text-align-001's reference sets `li { text-align-last:
// end }` and `li > div { text-align-last: initial }`; the div must align its
// last line by text-align (start), not end.
func TestInitialOnNewlyInheritedProperty(t *testing.T) {
	parentVals := map[string]string{
		"caption-side":            "bottom",
		"fill":                    "red",
		"fill-rule":               "evenodd",
		"stroke":                  "blue",
		"stroke-dasharray":        "2 3",
		"stroke-dashoffset":       "5",
		"stroke-linecap":          "round",
		"stroke-linejoin":         "bevel",
		"stroke-miterlimit":       "9",
		"stroke-width":            "4px",
		"image-rendering":         "pixelated",
		"scrollbar-color":         "red blue",
		"text-align-last":         "end",
		"text-underline-offset":   "3px",
		"text-underline-position": "under",
		"font-optical-sizing":     "none",
		"font-size-adjust":        "0.5",
		"font-variant-caps":       "small-caps",
		"text-wrap":               "balance",
		"word-wrap":               "break-word",
	}
	parentDecls, childDecls := "", ""
	for prop, val := range parentVals {
		parentDecls += prop + ": " + val + "; "
		childDecls += prop + ": initial; "
	}
	doc, _ := html.Parse(`<style>.parent { ` + parentDecls + ` } .child { ` + childDecls + ` }</style><body><div class="parent"><div class="child"></div></div></body>`)
	styles := ApplyStylesToDocument(doc, 800, 600)
	var child *Style
	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			child = style
		}
	}
	if child == nil {
		t.Fatal("child not styled")
	}
	for prop, parentVal := range parentVals {
		if got, _ := child.Get(prop); got == parentVal {
			t.Errorf("child %s: initial = %q, the parent's value; want the initial value", prop, got)
		}
	}
}
