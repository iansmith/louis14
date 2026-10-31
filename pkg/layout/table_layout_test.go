package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestComputeColumnWidths_LeftoverToAutoColumns reproduces the WPT
// css-grid/fr-unit.html reference-table bug (campaign lane css-grid).
//
// CSS 2.1 §17.5.2.2: in an auto table-layout with an explicit table width
// larger than the sum of column max-content widths, the leftover space is
// distributed ONLY to columns that do NOT have a specified width. A column
// with a specified (constrained) width must keep that width.
//
// Blink analog: table_layout_utils.cc
// DistributeInlineSizeToComputedInlineSizeAuto, kAboveMax case
// (@ Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f): when
// auto_columns_count > 0, the distributable inline-size grows the auto
// (unconstrained) columns, never the fixed ones.
//
// Setup mirrors the shared .error reference template used by both
// fr-unit.html and fr-unit-with-percentage.html: a 400px-wide table with
// col1 cell width:100px (constrained) and col2 auto (empty content). The
// correct result is [100, 300]; the even-split bug produced ~[234, 165].
func TestComputeColumnWidths_LeftoverToAutoColumns(t *testing.T) {
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	// Two real cell nodes so the auto column goes through intrinsic sizing
	// (empty content → 0 max-content) exactly as the live table path does.
	cell1DOM := makeNode("td")
	cell2DOM := makeNode("td")
	cell1Style := makeStyle("display", "table-cell", "width", "100px")
	cell2Style := makeStyle("display", "table-cell")
	styles := map[*html.Node]*css.Style{
		cell1DOM: cell1Style,
		cell2DOM: cell2Style,
	}
	cell1Node := buildTestTree(cell1DOM, styles)
	cell2Node := buildTestTree(cell2DOM, styles)

	rows := []tableRow{
		{cells: []tableCell{
			{node: cell1Node, style: cell1Style, colIndex: 0, colSpan: 1, rowSpan: 1},
			{node: cell2Node, style: cell2Style, colIndex: 1, colSpan: 1, rowSpan: 1},
		}},
	}

	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: 600}).
		Build()
	tableStyle := makeStyle("display", "table", "width", "400px")
	tableNode := buildTestTree(makeNode("table"), map[*html.Node]*css.Style{})
	tla := &TableLayoutAlgorithm{ctx: ctx, node: tableNode, style: tableStyle, space: space}

	widths := tla.computeColumnWidths(rows, 2, 400, false, true, nil)

	if len(widths) != 2 {
		t.Fatalf("expected 2 column widths, got %d (%v)", len(widths), widths)
	}
	// Constrained col1 keeps its specified 100px; the auto col2 absorbs the
	// full ~300px leftover. The even-split bug yielded col1≈234, col2≈165.
	const eps = 0.5
	if d := widths[0] - 100; d < -eps || d > eps {
		t.Errorf("constrained col1 width = %v, want 100 (must not absorb leftover)", widths[0])
	}
	if d := widths[1] - 300; d < -eps || d > eps {
		t.Errorf("auto col2 width = %v, want 300 (must absorb full leftover)", widths[1])
	}
}

// TestComputeColumnWidths_LeftoverAllAutoEvenSplit guards the fallback
// branch: when every column is unconstrained (the common all-auto table),
// leftover above max-content is still split evenly across all columns, with
// no behavior change from the pre-fix code path.
//
// Blink analog: same kAboveMax case — with no constrained columns the auto
// columns share the distributable inline-size equally.
func TestComputeColumnWidths_LeftoverAllAutoEvenSplit(t *testing.T) {
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	cell1DOM := makeNode("td")
	cell2DOM := makeNode("td")
	cell1Style := makeStyle("display", "table-cell")
	cell2Style := makeStyle("display", "table-cell")
	styles := map[*html.Node]*css.Style{
		cell1DOM: cell1Style,
		cell2DOM: cell2Style,
	}
	cell1Node := buildTestTree(cell1DOM, styles)
	cell2Node := buildTestTree(cell2DOM, styles)

	rows := []tableRow{
		{cells: []tableCell{
			{node: cell1Node, style: cell1Style, colIndex: 0, colSpan: 1, rowSpan: 1},
			{node: cell2Node, style: cell2Style, colIndex: 1, colSpan: 1, rowSpan: 1},
		}},
	}

	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: 600}).
		Build()
	tableStyle := makeStyle("display", "table", "width", "400px")
	tableNode := buildTestTree(makeNode("table"), map[*html.Node]*css.Style{})
	tla := &TableLayoutAlgorithm{ctx: ctx, node: tableNode, style: tableStyle, space: space}

	widths := tla.computeColumnWidths(rows, 2, 400, false, true, nil)

	const eps = 0.5
	if d := widths[0] - 200; d < -eps || d > eps {
		t.Errorf("all-auto col1 width = %v, want 200 (even split)", widths[0])
	}
	if d := widths[1] - 200; d < -eps || d > eps {
		t.Errorf("all-auto col2 width = %v, want 200 (even split)", widths[1])
	}
}

// colMinMaxFixture builds a minimal 2-cell, 2-column auto table with empty cell
// content (so a column's width comes purely from its col/colgroup spec) and
// returns an algorithm ready for computeColumnWidths. Shared by the col
// min-width/max-width clamp tests below (LOU-233).
func colMinMaxFixture(t *testing.T) (*TableLayoutAlgorithm, []tableRow) {
	t.Helper()
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	cell1DOM := makeNode("td")
	cell2DOM := makeNode("td")
	cell1Style := makeStyle("display", "table-cell")
	cell2Style := makeStyle("display", "table-cell")
	styles := map[*html.Node]*css.Style{cell1DOM: cell1Style, cell2DOM: cell2Style}
	cell1Node := buildTestTree(cell1DOM, styles)
	cell2Node := buildTestTree(cell2DOM, styles)

	rows := []tableRow{
		{cells: []tableCell{
			{node: cell1Node, style: cell1Style, colIndex: 0, colSpan: 1, rowSpan: 1},
			{node: cell2Node, style: cell2Style, colIndex: 1, colSpan: 1, rowSpan: 1},
		}},
	}

	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 600, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 600, BlockSize: 600}).
		Build()
	tableStyle := makeStyle("display", "table")
	tableNode := buildTestTree(makeNode("table"), map[*html.Node]*css.Style{})
	tla := &TableLayoutAlgorithm{ctx: ctx, node: tableNode, style: tableStyle, space: space}
	return tla, rows
}

// TestComputeColumnWidths_ColMaxWidthClampsWidthDown reproduces
// css-tables/col-definite-max-size-001.html row 8: a column with
// width:100px and max-width:40px must lay out at 40px — the max-width
// caps the specified width.
//
// Blink analog: table_layout_utils.cc ComputeColumnConstraint
// (@ Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f): the per-column
// constraint resolves the col's inline-size and clamps it by the col's
// resolved min/max-inline-size.
func TestComputeColumnWidths_ColMaxWidthClampsWidthDown(t *testing.T) {
	tla, rows := colMinMaxFixture(t)
	// auto table (hasExplicitWidth=false): empty-content columns size to their
	// col spec. col0 has width:100px clamped by max-width:40px → 40px.
	specs := []tableColWidth{
		{colIndex: 0, span: 1, width: 100, maxWidth: 40, hasMax: true},
	}
	widths := tla.computeColumnWidths(rows, 2, 600, false, false, specs)
	const eps = 0.5
	if d := widths[0] - 40; d < -eps || d > eps {
		t.Errorf("col0 width = %v, want 40 (max-width:40px clamps width:100px)", widths[0])
	}
}

// TestComputeColumnWidths_ColMaxWidthZeroCollapses reproduces row 1:
// width:100px with max-width:0 collapses the column to 0.
func TestComputeColumnWidths_ColMaxWidthZeroCollapses(t *testing.T) {
	tla, rows := colMinMaxFixture(t)
	specs := []tableColWidth{
		{colIndex: 0, span: 1, width: 100, maxWidth: 0, hasMax: true},
	}
	widths := tla.computeColumnWidths(rows, 2, 600, false, false, specs)
	const eps = 0.5
	if d := widths[0]; d < -eps || d > eps {
		t.Errorf("col0 width = %v, want 0 (max-width:0 collapses width:100px)", widths[0])
	}
}

// TestComputeColumnWidths_ColMinWidthRaisesWithoutWidth reproduces
// css-tables/col-definite-min-size-001.html row 1: a column with
// min-width:100px and no width must lay out at >=100px.
func TestComputeColumnWidths_ColMinWidthRaisesWithoutWidth(t *testing.T) {
	tla, rows := colMinMaxFixture(t)
	specs := []tableColWidth{
		{colIndex: 0, span: 1, width: 0, minWidth: 100, hasMin: true},
	}
	widths := tla.computeColumnWidths(rows, 2, 600, false, false, specs)
	const eps = 0.5
	if d := widths[0] - 100; d < -eps || d > eps {
		t.Errorf("col0 width = %v, want 100 (min-width:100px with no width)", widths[0])
	}
}

// TestComputeColumnWidths_ColMinWidthBeatsMaxWidth reproduces row 2:
// max-width:0 with min-width:100px resolves to 100px — min-width has
// priority over max-width per the CSS used-value clamp order
// (used = max(min, min(max, preferred))).
func TestComputeColumnWidths_ColMinWidthBeatsMaxWidth(t *testing.T) {
	tla, rows := colMinMaxFixture(t)
	specs := []tableColWidth{
		{colIndex: 0, span: 1, width: 0, minWidth: 100, hasMin: true, maxWidth: 0, hasMax: true},
	}
	widths := tla.computeColumnWidths(rows, 2, 600, false, false, specs)
	const eps = 0.5
	if d := widths[0] - 100; d < -eps || d > eps {
		t.Errorf("col0 width = %v, want 100 (min-width:100px beats max-width:0)", widths[0])
	}
}

// TestTableCell_ExplicitHeightVerticalAlign pins vertical-align on an in-table
// cell whose explicit height exceeds its content. Blink aligns the content in
// FinalizeTableCellLayout (third_party/blink/renderer/core/layout/table/
// table_layout_utils.cc:1599-1638 @ d4ecdfed88f962439247c2ad36b8fe47805b1520):
// free_space = cell block-size - intrinsic block-size, moved by free_space/2
// for middle and free_space for bottom (CSS 2.1 §17.5.4). Cell 100px tall,
// content 20px: middle puts the content at 40, bottom at 80.
func TestTableCell_ExplicitHeightVerticalAlign(t *testing.T) {
	for _, tc := range []struct {
		va   string
		want float64
	}{
		{"middle", 40},
		{"bottom", 80},
	} {
		t.Run(tc.va, func(t *testing.T) {
			content := makeNode("div")
			td := makeNode("td", content)
			tr := makeNode("tr", td)
			table := makeNode("table", tr)
			styles := map[*html.Node]*css.Style{
				table:   makeStyle("display", "table", "border-spacing", "0"),
				tr:      makeStyle("display", "table-row"),
				td:      makeStyle("display", "table-cell", "height", "100px", "vertical-align", tc.va),
				content: makeStyle("display", "block", "width", "20px", "height", "20px"),
			}
			tree := buildTestTree(table, styles)
			wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
				Build()
			result := NewTableLayoutAlgorithm(testContext(), tree, space).Layout()

			cellFrag := findFragmentByNode(result.Fragment, td)
			if cellFrag == nil {
				t.Fatal("cell fragment not found")
			}
			if h := cellFrag.Size.HeightF64(); h != 100 {
				t.Fatalf("cell height = %v, want 100", h)
			}
			for _, ch := range cellFrag.Children {
				if ch.Fragment.LayoutNode != nil && ch.Fragment.LayoutNode.DOMNode == content {
					if got := ch.Offset.TopF64(); got != tc.want {
						t.Errorf("content block offset in cell = %v, want %v", got, tc.want)
					}
					return
				}
			}
			t.Fatal("content fragment is not a child of the cell fragment")
		})
	}
}

// layoutTableForTest lays out root (a block containing tables) in an
// 800x600 horizontal-tb space.
func layoutTableForTest(root *html.Node, styles map[*html.Node]*css.Style) *LayoutResult {
	tree := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()
	return NewBlockLayoutAlgorithm(testContext(), tree, space).Layout()
}

// TestTableCell_VerticalAlignFreeSpaceCountsFloats: a table cell is a new
// formatting context, so its intrinsic block-size encloses its floats even
// when it has an explicit height (Blink BlockLayoutAlgorithm::FinishLayout,
// block_layout_algorithm.cc:1255-1261 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), and FinalizeTableCellLayout
// moves every child, floats included, by free_space/2 for middle
// (table_layout_utils.cc:1599-1638). Cell 100px tall holding only a 20px
// float: free space 80, float at 40 (not 50, which ignores the float).
func TestTableCell_VerticalAlignFreeSpaceCountsFloats(t *testing.T) {
	float := makeNode("div")
	td := makeNode("td", float)
	tr := makeNode("tr", td)
	table := makeNode("table", tr)
	root := makeNode("div", table)
	result := layoutTableForTest(root, map[*html.Node]*css.Style{
		root:  makeStyle("display", "block"),
		table: makeStyle("display", "table", "border-spacing", "0"),
		tr:    makeStyle("display", "table-row"),
		td:    makeStyle("display", "table-cell", "height", "100px", "vertical-align", "middle"),
		float: makeStyle("display", "block", "float", "left", "width", "20px", "height", "20px"),
	})
	cellFrag := findFragmentByNode(result.Fragment, td)
	if cellFrag == nil {
		t.Fatal("cell fragment not found")
	}
	for _, ch := range cellFrag.Children {
		if ch.Fragment.LayoutNode != nil && ch.Fragment.LayoutNode.DOMNode == float {
			if got := ch.Offset.TopF64(); got != 40 {
				t.Errorf("float block offset in cell = %v, want 40", got)
			}
			return
		}
	}
	t.Fatal("float fragment is not a child of the cell fragment")
}

// TestTableColumn_OrthogonalCellContributesLaidOutInlineSize: an orthogonal
// cell's min/max contribution to its column is the inline size (in the
// table's writing mode) of the cell laid out, as Blink's
// BlockNode::ComputeMinMaxSizes does for an orthogonal flow root
// (core/layout/block_node.cc:968-998 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), which
// TableTypes::CreateCellInlineConstraint calls
// (table/table_layout_algorithm_types.cc:138-163). A vertical-rl cell with
// height 180px (its inline size) stacking three 100px-wide blocks is 300px
// wide, so the auto table is 300px wide.
func TestTableColumn_OrthogonalCellContributesLaidOutInlineSize(t *testing.T) {
	c1, c2, c3 := makeNode("div"), makeNode("div"), makeNode("div")
	td := makeNode("td", c1, c2, c3)
	tr := makeNode("tr", td)
	table := makeNode("table", tr)
	root := makeNode("div", table)
	styles := map[*html.Node]*css.Style{
		root:  makeStyle("display", "block"),
		table: makeStyle("display", "table", "border-spacing", "0"),
		tr:    makeStyle("display", "table-row"),
		td:    makeStyle("display", "table-cell", "height", "180px", "writing-mode", "vertical-rl"),
	}
	for _, c := range []*html.Node{c1, c2, c3} {
		styles[c] = makeStyle("display", "block", "width", "100px")
	}
	result := layoutTableForTest(root, styles)
	cellFrag := findFragmentByNode(result.Fragment, td)
	if cellFrag == nil {
		t.Fatal("cell fragment not found")
	}
	if w, h := cellFrag.Size.WidthF64(), cellFrag.Size.HeightF64(); w != 300 || h != 180 {
		t.Errorf("orthogonal cell = %vx%v, want 300x180", w, h)
	}
	if w := findFragmentByNode(result.Fragment, table).Size.WidthF64(); w != 300 {
		t.Errorf("table width = %v, want 300", w)
	}
}

// TestTable_AbsposInAnonymousCellKeepsOuterContainingBlock: an absolutely
// positioned child of a row-group is wrapped in an anonymous row and cell
// (Blink LayoutTableSection::AddChild / LayoutTableRow::AddChild,
// table/layout_table_section.cc:73-102 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520) but stays out of flow: its
// containing block is the positioned ancestor outside the table, so
// `top: 0; left: 0` puts it at that ancestor's padding-box origin, 25px
// above and left of the content-box origin where the in-flow marker sits
// (WPT css-contain/contain-layout-009..012).
func TestTable_AbsposInAnonymousCellKeepsOuterContainingBlock(t *testing.T) {
	abspos := makeNode("div")
	rg := makeNode("div", makeTextNode("\n  "), abspos, makeTextNode("\n"))
	table := makeNode("div", rg)
	marker := makeNode("div")
	wrapper := makeNode("div", marker, table)
	root := makeNode("div", wrapper)
	result := layoutTableForTest(root, map[*html.Node]*css.Style{
		root: makeStyle("display", "block"),
		wrapper: makeStyle("display", "block", "position", "relative", "width", "50px", "height", "50px",
			"padding-top", "25px", "padding-left", "25px", "padding-right", "25px", "padding-bottom", "25px"),
		marker: makeStyle("display", "block", "height", "1px"),
		table:  makeStyle("display", "table"),
		rg:     makeStyle("display", "table-row-group"),
		abspos: makeStyle("display", "block", "position", "absolute", "top", "0", "left", "0", "width", "100px", "height", "100px"),
	})
	mx, my, ok := physicalOffsetOfNodeWithin(result.Fragment, wrapper, marker)
	if !ok {
		t.Fatal("marker fragment not found under the wrapper")
	}
	x, y, ok := physicalOffsetOfNodeWithin(result.Fragment, wrapper, abspos)
	if !ok {
		t.Fatal("abspos fragment not found under the wrapper")
	}
	if x != mx-25 || y != my-25 {
		t.Errorf("abspos at (%v, %v), want the wrapper padding-box origin (%v, %v)", x, y, mx-25, my-25)
	}
}

// TestTable_FloatInAnonymousCellStaysFloat: a floated child of a row-group
// is wrapped in an anonymous row and cell and stays a float inside that
// cell (Blink LayoutTableSection::AddChild / LayoutTableRow::AddChild,
// table/layout_table_section.cc:73-102, table/layout_table_row.cc:87-116 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). It keeps its own 20x20 size at
// the cell's content origin — the table's top-left, since display:table
// has zero border-spacing and the anonymous cell zero padding — instead of
// being laid out as the 100px-wide cell itself.
func TestTable_FloatInAnonymousCellStaysFloat(t *testing.T) {
	float := makeNode("div")
	rg := makeNode("div", makeTextNode("\n  "), float, makeTextNode("\n"))
	table := makeNode("div", rg)
	root := makeNode("div", table)
	result := layoutTableForTest(root, map[*html.Node]*css.Style{
		root:  makeStyle("display", "block"),
		table: makeStyle("display", "table", "width", "100px"),
		rg:    makeStyle("display", "table-row-group"),
		float: makeStyle("display", "block", "float", "left", "width", "20px", "height", "20px"),
	})
	var cell *PhysicalFragment
	var findParent func(f *PhysicalFragment) bool
	findParent = func(f *PhysicalFragment) bool {
		for _, ch := range f.Children {
			if ch.Fragment.LayoutNode != nil && ch.Fragment.LayoutNode.DOMNode == float {
				cell = f
				return true
			}
			if findParent(ch.Fragment) {
				return true
			}
		}
		return false
	}
	if !findParent(result.Fragment) {
		t.Fatal("float fragment not found as a child fragment")
	}
	if cell.LayoutNode == nil || !cell.LayoutNode.IsAnonymous() ||
		cell.LayoutNode.Style().GetDisplay() != css.DisplayTableCell {
		t.Fatalf("float's parent fragment is not the anonymous table cell")
	}
	if f := findFragmentByNode(result.Fragment, float); f.Size.WidthF64() != 20 || f.Size.HeightF64() != 20 {
		t.Errorf("float size = %vx%v, want 20x20", f.Size.WidthF64(), f.Size.HeightF64())
	}
	if x, y, ok := physicalOffsetOfNodeWithin(result.Fragment, table, float); !ok || x != 0 || y != 0 {
		t.Errorf("float at (%v, %v) within the table (found %v), want (0, 0)", x, y, ok)
	}
}
