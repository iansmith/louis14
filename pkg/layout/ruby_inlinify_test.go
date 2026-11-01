package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestRubyInlinifiesRecursivelyThroughInlines pins Blink's recursive
// inlinification: an inline box inlinified by a `display: ruby` (or
// `ruby-text`) layout parent inlinifies its own in-flow children in turn
// (ComputedStyle::InlinifiesChildren, style/computed_style.h:1805-1817, and
// StyleAdjuster::AdjustStyleForDisplay, css/resolver/style_adjuster.cc:789-806
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), but an inline-block does not.
// A block inside display:ruby is therefore an atomic inline-block, never a
// block-in-inline.
func TestRubyInlinifiesRecursivelyThroughInlines(t *testing.T) {
	for _, tc := range []struct {
		name      string
		container string // display of the ruby-like container
		middle    string // display of the box between container and leaf
		want      css.DisplayType
	}{
		{"ruby>inline>block", "ruby", "inline", css.DisplayInlineBlock},
		{"ruby-text>inline>block", "ruby-text", "inline", css.DisplayInlineBlock},
		{"ruby>inline-list-item>block", "ruby", "inline list-item", css.DisplayInlineBlock},
		{"ruby>inline-block>block", "ruby", "inline-block", css.DisplayBlock},
		{"ruby>block>block", "ruby", "block", css.DisplayBlock},
		{"block ruby>inline>block", "block ruby", "inline", css.DisplayInlineBlock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := makeNode("div", &html.Node{Type: html.TextNode, Text: "a"})
			middle := makeNode("span", leaf)
			container := makeNode("span", middle)
			root := makeNode("div", container)
			tree := buildTestTree(root, map[*html.Node]*css.Style{
				root:      makeStyle("display", "block"),
				container: makeStyle("display", tc.container),
				middle:    makeStyle("display", tc.middle),
				leaf:      makeStyle("display", "block"),
			})
			var got *LayoutInputNode
			var walk func(n *LayoutInputNode)
			walk = func(n *LayoutInputNode) {
				if n.DOMNode == leaf {
					got = n
				}
				for _, c := range n.Children() {
					walk(c)
				}
			}
			walk(tree)
			if got == nil {
				t.Fatal("leaf not in layout tree")
			}
			if d := got.Style().GetDisplay(); d != tc.want {
				t.Errorf("leaf display = %q, want %q", d, tc.want)
			}
		})
	}
}
