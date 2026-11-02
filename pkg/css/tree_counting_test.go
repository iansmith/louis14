package css

import (
	"testing"

	"louis14/pkg/html"
)

// sibling-index() / sibling-count() resolve for the element whose style is
// computed: its 1-based index among its element siblings, and their count
// (CSSMathExpressionSiblingFunction::ComputeDouble,
// core/css/css_math_expression_node.cc:5580-5600 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestTreeCountingFunctions(t *testing.T) {
	doc, err := html.Parse(`<style>.t { width: calc(10px * sibling-index()); height: calc(1px * SIBLING-COUNT( )); }</style>
		<div>text<p></p><!-- c --><p class="t" id="a"></p><p></p><p class="t" id="b"></p></div>`)
	if err != nil {
		t.Fatal(err)
	}
	styles := ApplyStylesToDocument(doc, 800, 600)
	for id, want := range map[string][2]float64{"a": {20, 4}, "b": {40, 4}} {
		var node *html.Node
		var find func(n *html.Node)
		find = func(n *html.Node) {
			if v, _ := n.GetAttribute("id"); v == id {
				node = n
			}
			for _, c := range n.Children {
				find(c)
			}
		}
		find(doc.Root)
		s := styles[node]
		w, _ := s.GetLength("width")
		h, _ := s.GetLength("height")
		if w != want[0] || h != want[1] {
			t.Errorf("#%s: width %v height %v, want %v %v", id, w, h, want[0], want[1])
		}
	}
}
