package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// requireParentLinks fails unless every node under n is the parent of its
// children, i.e. the parent links agree with the children lists.
func requireParentLinks(t *testing.T, n *LayoutInputNode) {
	t.Helper()
	for i, c := range n.Children() {
		if c.parent != n {
			t.Fatalf("child %d (%q) of %q: parent = %p, want %p", i, c.TagName(), n.TagName(), c.parent, n)
		}
		requireParentLinks(t, c)
	}
}

// TestBuildLayoutTree_LinksParents: after BuildLayoutTree every node's
// parent link names the node whose children list holds it, including the
// anonymous block wrappers generated for inline runs among block children.
func TestBuildLayoutTree_LinksParents(t *testing.T) {
	styles := map[*html.Node]*css.Style{}
	a := makeNode("div")
	styles[a] = makeStyle("display", "block")
	span := makeNode("span", makeTextNode("x"))
	styles[span] = makeStyle("display", "inline")
	b := makeNode("div", makeNode("p"))
	styles[b] = makeStyle("display", "block")
	styles[b.Children[0]] = makeStyle("display", "block")
	root := makeNode("div", a, span, b)
	styles[root] = makeStyle("display", "block")

	tree := buildTestTree(root, styles)
	if tree.parent != nil {
		t.Errorf("root parent = %p, want nil", tree.parent)
	}
	requireParentLinks(t, tree)
}

// TestNextSibling: NextSibling returns the following non-text sibling and
// nil past the last child or for a parentless node, mirroring Blink's
// BlockNode::NextBlockSibling (block_node.cc:1120-1143 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which skips the text
// LayoutObjects left among block children.
func TestNextSibling(t *testing.T) {
	block := func() *LayoutInputNode { return &LayoutInputNode{style: makeStyle("display", "block")} }
	text := &LayoutInputNode{DOMNode: makeTextNode(" ")}
	first, second, last := block(), block(), block()
	parent := &LayoutInputNode{children: []*LayoutInputNode{first, text, second, last}}
	linkLayoutParents(parent)

	if got := first.NextSibling(); got != second {
		t.Errorf("first.NextSibling() = %p, want second %p (text skipped)", got, second)
	}
	if got := second.NextSibling(); got != last {
		t.Errorf("second.NextSibling() = %p, want last %p", got, last)
	}
	if got := last.NextSibling(); got != nil {
		t.Errorf("last.NextSibling() = %p, want nil", got)
	}
	if got := parent.NextSibling(); got != nil {
		t.Errorf("parentless NextSibling() = %p, want nil", got)
	}
}

// TestMulticol_LayoutKeepsParentLinks: multicol layout leaves the
// multicol the parent of its children, so a spanner finds its sibling
// spanners through the multicol's own children list. Blink has no flow
// thread at a9f50e522efa9005e6ec765a9a785c74f5c2c86b: ColumnLayoutAlgorithm
// lays column content out on the multicol node itself
// (column_layout_algorithm.cc:940-945) and walks spanners with
// NextSibling (:207-209), so nothing re-parents the children.
func TestMulticol_LayoutKeepsParentLinks(t *testing.T) {
	styles := map[*html.Node]*css.Style{}
	s1 := makeNode("div")
	styles[s1] = makeStyle("display", "block", "column-span", "all", "height", "10px")
	s2 := makeNode("div")
	styles[s2] = makeStyle("display", "block", "column-span", "all", "height", "10px")
	mc := makeNode("div", s1, makeTextNode("x"), s2)
	styles[mc] = makeStyle("display", "block", "column-count", "2")

	mcNode := buildTestTree(mc, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		Build()
	NewMulticolLayoutAlgorithm(testContext(), mcNode, space).Layout()

	requireParentLinks(t, mcNode)
}
