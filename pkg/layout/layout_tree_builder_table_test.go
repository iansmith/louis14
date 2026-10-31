package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"slices"
	"testing"
)

// Tests for CSS 2.1 §17.2.1 rule 3 ("generate missing parents"): a table
// part whose parent is not the matching table level gets an anonymous
// table, which is then filled down to row-group / row. Expected shapes
// follow Blink LayoutObject::AddChild + RequiresAnonymousTableWrappers
// (third_party/blink/renderer/core/layout/layout_object.cc:600-621,
// 715-728 @ d4ecdfed88f962439247c2ad36b8fe47805b1520) and the
// LayoutTable / LayoutTableSection / LayoutTableRow AddChild chain.

// requireAnon fails unless n is an anonymous box with the given display,
// and returns its children.
func requireAnon(t *testing.T, what string, n *LayoutInputNode, want css.DisplayType) []*LayoutInputNode {
	t.Helper()
	if n == nil {
		t.Fatalf("%s: got nil node, want anonymous display %v", what, want)
	}
	if !n.IsAnonymous() || n.DOMNode != nil {
		t.Fatalf("%s: got non-anonymous node (DOM %v), want anonymous display %v", what, n.TagName(), want)
	}
	if got := n.Style().GetDisplay(); got != want {
		t.Fatalf("%s: anonymous display = %v, want %v", what, got, want)
	}
	return n.Children()
}

// requireOnlyChild fails unless children has exactly one node.
func requireOnlyChild(t *testing.T, what string, children []*LayoutInputNode) *LayoutInputNode {
	t.Helper()
	if len(children) != 1 {
		t.Fatalf("%s: got %d children, want 1", what, len(children))
	}
	return children[0]
}

// requireTableRowGroupRow asserts table (anonymous, with tableDisplay) >
// anon row-group > anon row > cells (the given DOM nodes, in order).
func requireTableRowGroupRow(t *testing.T, table *LayoutInputNode, tableDisplay css.DisplayType, cells ...*html.Node) {
	t.Helper()
	rg := requireOnlyChild(t, "table children", requireAnon(t, "table", table, tableDisplay))
	row := requireOnlyChild(t, "row-group children", requireAnon(t, "row-group", rg, css.DisplayTableRowGroup))
	got := requireAnon(t, "row", row, css.DisplayTableRow)
	if len(got) != len(cells) {
		t.Fatalf("row has %d children, want %d cells", len(got), len(cells))
	}
	for i, c := range cells {
		if got[i].DOMNode != c {
			t.Fatalf("row child %d is DOM %q, want the authored cell", i, got[i].TagName())
		}
	}
}

func TestGenerateMissingTableParents_OrphanCell(t *testing.T) {
	cell := makeNode("div")
	div := makeNode("div", cell)
	styles := map[*html.Node]*css.Style{
		div:  makeStyle("display", "block"),
		cell: makeStyle("display", "table-cell"),
	}
	root := buildTestTree(div, styles)
	table := requireOnlyChild(t, "div children", root.Children())
	requireTableRowGroupRow(t, table, css.DisplayTable, cell)
}

func TestGenerateMissingTableParents_OrphanRowRowGroupCaption(t *testing.T) {
	cases := []struct {
		display string
		// wantPath lists the anonymous displays between the anon table
		// and the authored part.
		wantPath []css.DisplayType
	}{
		{"table-row", []css.DisplayType{css.DisplayTableRowGroup}},
		{"table-row-group", nil},
		{"table-header-group", nil},
		{"table-footer-group", nil},
		{"table-caption", nil},
	}
	for _, tc := range cases {
		t.Run(tc.display, func(t *testing.T) {
			part := makeNode("div")
			div := makeNode("div", part)
			styles := map[*html.Node]*css.Style{
				div:  makeStyle("display", "block"),
				part: makeStyle("display", tc.display),
			}
			root := buildTestTree(div, styles)
			n := requireOnlyChild(t, "div children", root.Children())
			children := requireAnon(t, "anon table", n, css.DisplayTable)
			for _, d := range tc.wantPath {
				children = requireAnon(t, "anon wrapper", requireOnlyChild(t, "wrapper children", children), d)
			}
			if got := requireOnlyChild(t, "innermost", children); got.DOMNode != part {
				t.Fatalf("innermost child is DOM %q, want the authored %s", got.TagName(), tc.display)
			}
		})
	}
}

func TestGenerateMissingTableParents_ConsecutiveOrphansShareOneTable(t *testing.T) {
	t.Run("collapsible whitespace is dropped", func(t *testing.T) {
		c1, c2 := makeNode("div"), makeNode("div")
		div := makeNode("div", c1, makeTextNode("\n  "), c2)
		styles := map[*html.Node]*css.Style{
			div: makeStyle("display", "block"),
			c1:  makeStyle("display", "table-cell"),
			c2:  makeStyle("display", "table-cell"),
		}
		root := buildTestTree(div, styles)
		table := requireOnlyChild(t, "div children", root.Children())
		requireTableRowGroupRow(t, table, css.DisplayTable, c1, c2)
	})
	t.Run("white-space pre keeps the text and splits the run", func(t *testing.T) {
		c1, c2 := makeNode("div"), makeNode("div")
		ws := makeTextNode("\n")
		div := makeNode("div", c1, ws, c2)
		styles := map[*html.Node]*css.Style{
			div: makeStyle("display", "block", "white-space", "pre"),
			c1:  makeStyle("display", "table-cell"),
			c2:  makeStyle("display", "table-cell"),
		}
		root := buildTestTree(div, styles)
		kids := root.Children()
		if len(kids) != 3 {
			t.Fatalf("div has %d children, want 3 (table, text, table)", len(kids))
		}
		requireTableRowGroupRow(t, kids[0], css.DisplayTable, c1)
		requireTableRowGroupRow(t, kids[2], css.DisplayTable, c2)
		if findLayoutNode(kids[1], ws) == nil {
			t.Fatalf("middle child does not hold the preserved whitespace text")
		}
	})
	// The drop decision reads the text's own style, as Blink's
	// Text::TextLayoutObjectIsNeeded tests style.ShouldPreserveBreaks() on
	// the style of the text's flat-tree parent (core/dom/text.cc:307, 339-348
	// @ d4ecdfed88f962439247c2ad36b8fe47805b1520). Text under a
	// display:contents element takes that element's style, not the box
	// parent's.
	t.Run("display:contents white-space pre keeps the text and splits the run", func(t *testing.T) {
		c1, c2 := makeNode("div"), makeNode("div")
		ws := makeTextNode("\n")
		contents := makeNode("span", ws)
		div := makeNode("div", c1, contents, c2)
		styles := map[*html.Node]*css.Style{
			div:      makeStyle("display", "block"),
			contents: makeStyle("display", "contents", "white-space", "pre"),
			c1:       makeStyle("display", "table-cell"),
			c2:       makeStyle("display", "table-cell"),
		}
		root := buildTestTree(div, styles)
		kids := root.Children()
		if len(kids) != 3 {
			t.Fatalf("div has %d children, want 3 (table, text, table)", len(kids))
		}
		requireTableRowGroupRow(t, kids[0], css.DisplayTable, c1)
		requireTableRowGroupRow(t, kids[2], css.DisplayTable, c2)
		if findLayoutNode(kids[1], ws) == nil {
			t.Fatalf("middle child does not hold the preserved whitespace text")
		}
	})
	t.Run("display:contents white-space normal drops the text under a pre parent", func(t *testing.T) {
		c1, c2 := makeNode("div"), makeNode("div")
		contents := makeNode("span", makeTextNode("\n"))
		div := makeNode("div", c1, contents, c2)
		styles := map[*html.Node]*css.Style{
			div:      makeStyle("display", "block", "white-space", "pre"),
			contents: makeStyle("display", "contents", "white-space", "normal"),
			c1:       makeStyle("display", "table-cell"),
			c2:       makeStyle("display", "table-cell"),
		}
		root := buildTestTree(div, styles)
		table := requireOnlyChild(t, "div children", root.Children())
		requireTableRowGroupRow(t, table, css.DisplayTable, c1, c2)
	})
}

func TestGenerateMissingTableParents_SiblingSplitsRun(t *testing.T) {
	cases := []struct {
		name    string
		between *html.Node
		style   *css.Style
	}{
		{"abspos box", makeNode("div"), makeStyle("display", "block", "position", "absolute")},
		{"non-whitespace text", makeTextNode("x"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c1, c2 := makeNode("div"), makeNode("div")
			div := makeNode("div", c1, tc.between, c2)
			styles := map[*html.Node]*css.Style{
				div: makeStyle("display", "block"),
				c1:  makeStyle("display", "table-cell"),
				c2:  makeStyle("display", "table-cell"),
			}
			if tc.style != nil {
				styles[tc.between] = tc.style
			}
			root := buildTestTree(div, styles)
			kids := root.Children()
			if len(kids) != 3 {
				t.Fatalf("div has %d children, want 3 (table, sibling, table)", len(kids))
			}
			requireTableRowGroupRow(t, kids[0], css.DisplayTable, c1)
			requireTableRowGroupRow(t, kids[2], css.DisplayTable, c2)
			if findLayoutNode(kids[1], tc.between) == nil {
				t.Fatalf("middle child does not hold the separating sibling")
			}
		})
	}
}

func TestGenerateMissingTableParents_InlineParentGetsInlineTable(t *testing.T) {
	for _, display := range []string{"inline", "ruby", "inline list-item"} {
		t.Run("display "+display, func(t *testing.T) {
			cell := makeNode("div")
			span := makeNode("span", cell)
			div := makeNode("div", span)
			styles := map[*html.Node]*css.Style{
				div:  makeStyle("display", "block"),
				span: makeStyle("display", display),
				cell: makeStyle("display", "table-cell"),
			}
			root := buildTestTree(div, styles)
			// The inline-table is inline-level, so the inline parent is not
			// split by block-in-inline and no anonymous block appears.
			s := requireOnlyChild(t, "div children", root.Children())
			if s.DOMNode != span || s.isContinuation {
				t.Fatalf("div child is DOM %q (continuation=%v), want the unsplit %s box",
					s.TagName(), s.isContinuation, display)
			}
			table := ancestorOf(root, cell, 3)
			if !slices.Contains(s.Children(), table) {
				t.Fatalf("anonymous table is not a direct child of the %s box", display)
			}
			requireTableRowGroupRow(t, table, css.DisplayInlineTable, cell)
		})
	}
}

// A `display: block ruby` box's children move under an anonymous inline
// `display: ruby` box (Blink LayoutRubyAsBlock), so an orphan cell among them
// gets an inline-table, and sibling text is not wrapped in an anonymous block.
func TestGenerateMissingTableParents_BlockRubyGetsInlineTable(t *testing.T) {
	cell := makeNode("div")
	txt := makeTextNode("x")
	ruby := makeNode("div", txt, cell)
	styles := map[*html.Node]*css.Style{
		ruby: makeStyle("display", "block ruby"),
		cell: makeStyle("display", "table-cell"),
	}
	root := buildTestTree(ruby, styles)
	inner := requireAnon(t, "block-ruby child", requireOnlyChild(t, "block ruby children", root.Children()), css.DisplayRuby)
	if len(inner) != 2 || inner[0].DOMNode != txt {
		t.Fatalf("anonymous ruby has %d children (first anonymous=%v), want [text, inline-table]",
			len(inner), len(inner) > 0 && inner[0].IsAnonymous())
	}
	requireTableRowGroupRow(t, inner[1], css.DisplayInlineTable, cell)
}

func TestGenerateMissingTableParents_BlockifiedAndTableParentsExempt(t *testing.T) {
	cases := []struct {
		name        string
		parentStyle *css.Style
		cellStyle   *css.Style
	}{
		{"abspos cell", makeStyle("display", "block"), makeStyle("display", "table-cell", "position", "absolute")},
		{"floated cell", makeStyle("display", "block"), makeStyle("display", "table-cell", "float", "left")},
		{"flex item cell", makeStyle("display", "flex"), makeStyle("display", "table-cell")},
		{"grid item cell", makeStyle("display", "grid"), makeStyle("display", "table-cell")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cell := makeNode("div")
			div := makeNode("div", cell)
			root := buildTestTree(div, map[*html.Node]*css.Style{div: tc.parentStyle, cell: tc.cellStyle})
			if got := requireOnlyChild(t, "parent children", root.Children()); got.DOMNode != cell {
				t.Fatalf("parent child is anonymous=%v display=%v, want the cell itself (no anonymous table)",
					got.IsAnonymous(), got.Style().GetDisplay())
			}
		})
	}
	t.Run("cell directly in a table gets no nested table", func(t *testing.T) {
		cell := makeNode("div")
		table := makeNode("div", cell)
		root := buildTestTree(table, map[*html.Node]*css.Style{
			table: makeStyle("display", "table"),
			cell:  makeStyle("display", "table-cell"),
		})
		rg := requireOnlyChild(t, "table children", root.Children())
		row := requireOnlyChild(t, "row-group children", requireAnon(t, "row-group", rg, css.DisplayTableRowGroup))
		got := requireOnlyChild(t, "row children", requireAnon(t, "row", row, css.DisplayTableRow))
		if got.DOMNode != cell {
			t.Fatalf("row child is anonymous=%v, want the authored cell", got.IsAnonymous())
		}
	})
}

func TestGenerateMissingTableParents_AnonymousTableInheritsOnlyInherited(t *testing.T) {
	cell := makeNode("div")
	div := makeNode("div", cell)
	styles := map[*html.Node]*css.Style{
		div: makeStyle("display", "block", "color", "rgb(1, 2, 3)",
			"border-top-width", "5px", "border-top-style", "solid", "margin-left", "7px"),
		cell: makeStyle("display", "table-cell"),
	}
	root := buildTestTree(div, styles)
	table := requireOnlyChild(t, "div children", root.Children())
	requireAnon(t, "table", table, css.DisplayTable)
	props := table.Style().Properties
	if got := props["color"]; got != "rgb(1, 2, 3)" {
		t.Errorf("anonymous table color = %q, want the parent's inherited rgb(1, 2, 3)", got)
	}
	for _, p := range []string{"border-top-width", "border-top-style", "margin-left"} {
		if v, ok := props[p]; ok {
			t.Errorf("anonymous table carries non-inherited %s = %q from its parent", p, v)
		}
	}
}

func TestGenerateMissingTableParents_InsideTableCellNormalized(t *testing.T) {
	row := makeNode("div")
	inner := makeNode("div", row)
	td := makeNode("td", inner)
	tr := makeNode("tr", td)
	table := makeNode("table", tr)
	root := buildTestTree(table, map[*html.Node]*css.Style{
		table: makeStyle("display", "table"),
		tr:    makeStyle("display", "table-row"),
		td:    makeStyle("display", "table-cell"),
		inner: makeStyle("display", "block"),
		row:   makeStyle("display", "table-row"),
	})
	innerNode := findLayoutNode(root, inner)
	if innerNode == nil {
		t.Fatal("inner div not found in the layout tree")
	}
	anonTable := requireOnlyChild(t, "inner div children", innerNode.Children())
	rg := requireOnlyChild(t, "anon table children", requireAnon(t, "anon table", anonTable, css.DisplayTable))
	got := requireOnlyChild(t, "row-group children", requireAnon(t, "row-group", rg, css.DisplayTableRowGroup))
	if got.DOMNode != row {
		t.Fatalf("row-group child is anonymous=%v, want the authored row", got.IsAnonymous())
	}
}

// TestLayout_OrphanTableCellSizedByAnonymousTable pins the layout effect of
// the anonymous table around an orphan cell (WPT
// css-tables/dynamic-table-cell-height.html): the auto-width table shrinks to
// its content (CSS 2.1 §17.5.2), and the cell's `height: 50px` is a minimum,
// so the row grows to the 100px child (CSS 2.1 §17.5.3). Chrome renders the
// cell 100x100 (the WPT reference is a 100px green square).
func TestLayout_OrphanTableCellSizedByAnonymousTable(t *testing.T) {
	child := makeNode("div")
	cell := makeNode("div", child)
	root := makeNode("div", cell)
	styles := map[*html.Node]*css.Style{
		root:  makeStyle("display", "block", "width", "800px"),
		cell:  makeStyle("display", "table-cell", "height", "50px"),
		child: makeStyle("display", "block", "width", "100px", "height", "100px"),
	}
	tree := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()
	result := NewBlockLayoutAlgorithm(testContext(), tree, space).Layout()

	cellFrag := findFragmentByNode(result.Fragment, cell)
	if cellFrag == nil {
		t.Fatal("cell fragment not found")
	}
	if w, h := cellFrag.Size.WidthF64(), cellFrag.Size.HeightF64(); w != 100 || h != 100 {
		t.Errorf("orphan cell border box = %vx%v, want 100x100", w, h)
	}
	if len(result.Fragment.Children) != 1 {
		t.Fatalf("root has %d child fragments, want 1 (the anonymous table)", len(result.Fragment.Children))
	}
	tableFrag := result.Fragment.Children[0].Fragment
	if tableFrag.LayoutNode == nil || !tableFrag.LayoutNode.IsAnonymous() ||
		tableFrag.LayoutNode.Style().GetDisplay() != css.DisplayTable {
		t.Fatalf("root child fragment is not the anonymous table")
	}
	if w := tableFrag.Size.WidthF64(); w != 100 {
		t.Errorf("anonymous table width = %v, want 100 (shrink-to-fit)", w)
	}
}

// findLayoutNode returns the first layout node in n's subtree built for dom.
func findLayoutNode(n *LayoutInputNode, dom *html.Node) *LayoutInputNode {
	if n == nil {
		return nil
	}
	if n.DOMNode == dom {
		return n
	}
	for _, c := range n.Children() {
		if f := findLayoutNode(c, dom); f != nil {
			return f
		}
	}
	return nil
}

// ancestorOf returns the layout node `levels` levels above dom's layout
// node, or nil.
func ancestorOf(root *LayoutInputNode, dom *html.Node, levels int) *LayoutInputNode {
	var path []*LayoutInputNode
	var walk func(n *LayoutInputNode) bool
	walk = func(n *LayoutInputNode) bool {
		path = append(path, n)
		if n.DOMNode == dom {
			return true
		}
		for _, c := range n.Children() {
			if walk(c) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}
	if !walk(root) || len(path) <= levels {
		return nil
	}
	return path[len(path)-1-levels]
}
