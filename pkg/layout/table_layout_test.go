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
