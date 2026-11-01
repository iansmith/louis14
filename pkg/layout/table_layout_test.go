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

// relativeOffsetByID returns the relative offset (x, y) of the single
// fragment generated by id: its border-box origin minus normalX/normalY, its
// normal-flow origin. The parent bakes the relative offset into the child's
// link offset (BoxFragmentBuilder::AddChild, box_fragment_builder.cc:372-375
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Fails the test if id produced
// no fragment.
func relativeOffsetByID(t *testing.T, frag *PhysicalFragment, ids map[*html.Node]string, id string, normalX, normalY float64) (float64, float64) {
	t.Helper()
	found := false
	var x, y float64
	walkFragmentsByID(frag, ids, func(fid string, fx, fy float64, _ *PhysicalFragment) {
		if fid == id && !found {
			found, x, y = true, fx, fy
		}
	})
	if !found {
		t.Fatalf("no fragment for %q", id)
	}
	return x - normalX, y - normalY
}

// checkBlockSizes reports every id whose first fragment's border-box height
// differs from want.
func checkBlockSizes(t *testing.T, got map[string][]fragRect, want map[string]float64) {
	t.Helper()
	for id, h := range want {
		if len(got[id]) == 0 {
			t.Errorf("%s: no fragment", id)
			continue
		}
		if got[id][0].h != h {
			t.Errorf("%s border-box height = %v, want %v", id, got[id][0].h, h)
		}
	}
}

// TestTableLayout_PercentHeightAnonymousCellChildIsAuto pins the DOM shape of
// WPT css-tables/percent-height-table-cell-child.html: a block with
// height:50% as the only child of an auto-height display:table, so it sits in
// an anonymous cell. Blink gives every cell a percentage-resolution block
// size of kIndefiniteSize (SetupTableCellConstraintSpaceBuilder,
// third_party/blink/renderer/core/layout/table/table_layout_utils.cc:1411-1412
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520), and the row never grows past
// the cell's measured size, so the cell's initial block size stays
// indefinite (ComputeCellBlockSize, table_layout_utils.cc:1352-1358 @ the
// same SHA). The 50% is therefore auto and g is its content's 100px.
// Chrome 159 and Edge 158 pass the reftest (a 100x100 green square).
func TestTableLayout_PercentHeightAnonymousCellChildIsAuto(t *testing.T) {
	frag, ids := layoutInFlowRoot(t,
		ms("t", props("display", "table", "width", "100px", "height", "100%"),
			ms("g", props("height", "50%"),
				ms("i", props("height", "100px")))))
	checkBlockSizes(t, borderBoxRectsByID(frag, ids), map[string]float64{
		"t": 100, "g": 100, "i": 100,
	})
}

// TestTableLayout_PercentHeightAuthoredCellChildIsAuto is the authored-cell
// form of the case above: a height:50% block inside an auto-height
// display:table-cell. Blink hands a table-cell's children the cell's
// child_available_size as their percentage base (CalculateChildPercentageSize,
// third_party/blink/renderer/core/layout/length_utils.cc:1823
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520), which is indefinite because
// the measure pass lays the cell out with an indefinite initial block size
// (table_layout_utils.cc:222-230 @ the same SHA). So 50% is auto: g and c
// are 100px tall.
func TestTableLayout_PercentHeightAuthoredCellChildIsAuto(t *testing.T) {
	frag, ids := layoutInFlowRoot(t,
		ms("t", props("display", "table", "width", "100px"),
			ms("r", props("display", "table-row"),
				ms("c", props("display", "table-cell", "padding", "0"),
					ms("g", props("height", "50%"),
						ms("i", props("height", "100px")))))))
	checkBlockSizes(t, borderBoxRectsByID(frag, ids), map[string]float64{
		"c": 100, "g": 100,
	})
}

// relativeCellTable is a border-spacing:0 table with one row (row props
// rowProps) holding one position:relative cell c (extra props cellProps)
// that contains a 100x100 block.
func relativeCellTable(rowProps, cellProps []string) marginSpec {
	return ms("t", props("display", "table", "border-spacing", "0"),
		ms("r", append([]string{"display", "table-row"}, rowProps...),
			ms("c", append([]string{"display", "table-cell", "padding", "0", "position", "relative"}, cellProps...),
				ms("i", props("width", "100px", "height", "100px")))))
}

// TestTableLayout_RelativeCellPercentInsetsResolveAgainstRowSpecifiedBlockSize
// pins the chromium bug 1227884 basis (WPT css-position/position-relative-013:
// "top: 100% ... resolves against the specified parent height"): a relative
// cell's percentage insets resolve against its row's specified block size.
// Blink computes the offset when the row adds the cell
// (TableRowLayoutAlgorithm, table_row_layout_algorithm.cc:167 →
// BoxFragmentBuilder::AddResult, box_fragment_builder.cc:262-267 →
// ComputeRelativeOffsetForBoxFragment, relative_utils.cc:92, all
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
func TestTableLayout_RelativeCellPercentInsetsResolveAgainstRowSpecifiedBlockSize(t *testing.T) {
	cases := []struct {
		name                string
		rowProps, cellProps []string
		wantX, wantY        float64
	}{
		{"percent top, sized row", props("height", "10px"), props("top", "100%"), 0, 10},
		{"px left + percent top, sized row", props("height", "10px"), props("left", "5px", "top", "100%"), 5, 10},
		{"percent top, auto row", nil, props("top", "100%"), 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutInFlowRoot(t, relativeCellTable(tc.rowProps, tc.cellProps))
			x, y := relativeOffsetByID(t, frag, ids, "c", 0, 0)
			if x != tc.wantX || y != tc.wantY {
				t.Errorf("c relative offset = (%v, %v), want (%v, %v)", x, y, tc.wantX, tc.wantY)
			}
		})
	}
}

// TestTableLayout_RelativeCellPercentInlineInsetResolvesAgainstRowInlineSize
// pins the inline basis of a relative cell's percentage inset: the row's
// child available inline size (the section's inline size, set at
// table_section_layout_algorithm.cc:38-39 and :76-77), not the cell's own
// width — Blink's ComputeRelativeOffset (relative_utils.cc:18) runs in the
// row's AddResult against the row's child_available_size
// (box_fragment_builder.cc:262-267), all
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520. The row is 200px wide, so
// left:10% is 20px. Blink-source derived, not Chrome-measured.
func TestTableLayout_RelativeCellPercentInlineInsetResolvesAgainstRowInlineSize(t *testing.T) {
	frag, ids := layoutInFlowRoot(t,
		ms("t", props("display", "table", "width", "200px", "border-spacing", "0"),
			ms("r", props("display", "table-row"),
				ms("a", props("display", "table-cell", "width", "100px", "padding", "0")),
				ms("c", props("display", "table-cell", "width", "100px", "padding", "0",
					"position", "relative", "left", "10%")))))
	if x, y := relativeOffsetByID(t, frag, ids, "c", 100, 0); x != 20 || y != 0 {
		t.Errorf("c relative offset = (%v, %v), want (20, 0)", x, y)
	}
}

// TestTableLayout_RelativeRowspanCellPercentInsetUsesOriginatingRow pins the
// rowspan path: a rowspan cell attaches to the table rather than to a row
// fragment, but its percentage insets still resolve against its originating
// row's specified block size, as Blink adds every cell through the row it
// originates in (table_row_layout_algorithm.cc:167
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520). An auto-height originating
// row has an indefinite block size, so top:100% is auto (offset 0) even when
// the table itself has a definite height: the table's child available size
// must not re-resolve the inset.
func TestTableLayout_RelativeRowspanCellPercentInsetUsesOriginatingRow(t *testing.T) {
	for _, tc := range []struct {
		name               string
		tableProps, r1Prop []string
		wantY              float64
	}{
		{"sized row", props("display", "table", "border-spacing", "0"),
			props("display", "table-row", "height", "10px"), 10},
		{"auto row in sized table", props("display", "table", "border-spacing", "0", "height", "300px"),
			props("display", "table-row"), 0},
	} {
		cell := ms("c", props("display", "table-cell", "padding", "0", "position", "relative", "top", "100%"),
			ms("i", props("width", "100px", "height", "100px")))
		cell.attrs = map[string]string{"rowspan": "2"}
		frag, ids := layoutInFlowRoot(t,
			ms("t", tc.tableProps,
				ms("r1", tc.r1Prop, cell),
				ms("r2", props("display", "table-row"))))
		if x, y := relativeOffsetByID(t, frag, ids, "c", 0, 0); x != 0 || y != tc.wantY {
			t.Errorf("%s: c relative offset = (%v, %v), want (0, %v)", tc.name, x, y, tc.wantY)
		}
	}
}

// TestTableLayout_RowSpecifiedBlockSizeIsRowMinimum pins Blink's "Apply row's
// CSS block size" step in ComputeMinimumRowBlockSize
// (third_party/blink/renderer/core/layout/table/table_layout_utils.cc:305-315
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520): a row's fixed specified block
// size is a floor on its block size, whatever its cells measure. The
// height:100% cell is the WPT css-tables/table-as-item-cell-percentage-001
// shape: its percentage is indefinite in the measure pass (the cell space's
// kIndefiniteSize base, table_layout_utils.cc:1411-1412), so the 50px row
// height must come from the row itself.
func TestTableLayout_RowSpecifiedBlockSizeIsRowMinimum(t *testing.T) {
	for _, cellProps := range [][]string{
		props("display", "table-cell"),
		props("display", "table-cell", "height", "100%"),
	} {
		frag, ids := layoutInFlowRoot(t,
			ms("t", props("display", "table", "width", "100px", "border-spacing", "0"),
				ms("r", props("display", "table-row", "height", "50px"),
					ms("c", cellProps))))
		got := borderBoxRectsByID(frag, ids)
		for _, id := range []string{"t", "r", "c"} {
			if len(got[id]) == 0 || got[id][0].h != 50 {
				t.Errorf("cell %v: %s rects = %v, want height 50", cellProps, id, got[id])
			}
		}
	}
}

// TestTableLayout_RelativeAnonymousCellChildPercentInsetIsAuto pins the other
// side of the row basis: a relative block placed directly in a sized row is
// wrapped in an anonymous cell, so its containing block is that cell, not the
// row. The cell's initial block size is indefinite (the row does not grow:
// ComputeCellBlockSize, table_layout_utils.cc:1352-1358), so the cell's
// child_available_size block size is indefinite (CalculateChildAvailableSize,
// length_utils.cc:1774; ComputeInitialBlockSizeForFragment :936) and Blink's
// ResolveInset treats top:100% as auto (relative_utils.cc:29-35), all
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520: no offset.
func TestTableLayout_RelativeAnonymousCellChildPercentInsetIsAuto(t *testing.T) {
	frag, ids := layoutInFlowRoot(t,
		ms("t", props("display", "table", "border-spacing", "0"),
			ms("r", props("display", "table-row", "height", "10px"),
				ms("g", props("position", "relative", "top", "100%", "width", "100px", "height", "100px")))))
	if x, y := relativeOffsetByID(t, frag, ids, "g", 0, 0); x != 0 || y != 0 {
		t.Errorf("g relative offset = (%v, %v), want (0, 0)", x, y)
	}
}
