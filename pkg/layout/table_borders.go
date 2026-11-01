package layout

import (
	"fmt"

	"louis14/pkg/css"
)

// TableBorders precomputes the collapsed borders of a border-collapse:collapse
// table as a grid of edges, resolved once from every cell, row, section,
// column, column group and the table itself. Mirrors Blink's TableBorders
// (core/layout/table/table_borders.h:62-307, table_borders.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14 builds it only for
// collapsed tables, so Blink's is_collapsed_ flag and the separated-borders
// TableBorder() branch have no counterpart.
//
// Edges live in a 1D slice, EdgesPerRow() per grid row. Each cell slot owns
// two edges: its inline-start (column-axis) edge at an even index and its
// block-start (row-axis) edge at the following odd index; an extra imaginary
// row and column hold the last block-end and inline-end edges
// (table_borders.h:38-60).
type TableBorders struct {
	edges       []TableBordersEdge
	sections    []TableBordersSection
	edgesPerRow int
	tableBorder LogicalEdges
	// lastColumnIndex clamps colspans to the table grid (table_borders.h:303-305).
	lastColumnIndex int
}

// TableBordersEdgeSource mirrors TableBorders::EdgeSource (table_borders.h:76).
type TableBordersEdgeSource int

const (
	TableBordersEdgeSourceNone TableBordersEdgeSource = iota
	TableBordersEdgeSourceCell
	TableBordersEdgeSourceRow
	TableBordersEdgeSourceSection
	TableBordersEdgeSourceColumn
	TableBordersEdgeSourceTable
)

// TableBordersEdgeSide is the physical side of the source style that defines
// an edge, or DoNotFill for an edge inside a spanning cell that no table part
// may fill. Mirrors TableBorders::EdgeSide (table_borders.h:80).
type TableBordersEdgeSide int

const (
	TableBordersEdgeSideTop TableBordersEdgeSide = iota
	TableBordersEdgeSideRight
	TableBordersEdgeSideBottom
	TableBordersEdgeSideLeft
	TableBordersEdgeSideDoNotFill
)

// TableBordersEdge mirrors TableBorders::Edge (table_borders.h:84-95): the
// winning style, which of its sides defines the edge, and the box order (the
// visit index of the source box; lower wins paint ties).
//
// width and borderStyle cache the resolved TableBordersBorderWidth /
// TableBordersBorderStyle of Style's EdgeSide: Blink re-reads them from
// ComputedStyle fields, but louis14's style getters parse property strings.
type TableBordersEdge struct {
	Style       *css.Style
	EdgeSide    TableBordersEdgeSide
	BoxOrder    int
	width       float64
	borderStyle css.BorderStyle
}

// newTableBordersEdge returns the edge style's side defines, with its width
// and style resolved.
func newTableBordersEdge(style *css.Style, side TableBordersEdgeSide, boxOrder int) TableBordersEdge {
	return TableBordersEdge{
		Style: style, EdgeSide: side, BoxOrder: boxOrder,
		width:       TableBordersBorderWidth(style, side),
		borderStyle: TableBordersBorderStyle(style, side),
	}
}

// TableBordersSection mirrors TableBorders::Section (table_borders.h:172-175).
type TableBordersSection struct {
	StartRow int
	RowCount int
}

// EBorderStyleOrder ranks a collapsed border style in Blink's EBorderStyle
// enum order (none, hidden, inset, groove, outset, ridge, dotted, dashed,
// solid, double; core/style/computed_style_constants.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which the conflict resolution
// (IsSourceMoreSpecificThanEdge) and the joint paint precedence compare.
func EBorderStyleOrder(s css.BorderStyle) int {
	switch s {
	case css.BorderStyleHidden:
		return 1
	case "inset":
		return 2
	case "groove":
		return 3
	case "outset":
		return 4
	case "ridge":
		return 5
	case css.BorderStyleDotted:
		return 6
	case css.BorderStyleDashed:
		return 7
	case css.BorderStyleSolid:
		return 8
	case css.BorderStyleDouble:
		return 9
	}
	return 0
}

// tableBordersSideProperty returns the CSS property name prefix of a physical
// edge side ("border-top" etc.).
func tableBordersSideProperty(side TableBordersEdgeSide) string {
	switch side {
	case TableBordersEdgeSideTop:
		return "border-top"
	case TableBordersEdgeSideRight:
		return "border-right"
	case TableBordersEdgeSideBottom:
		return "border-bottom"
	}
	return "border-left"
}

// TableBordersBorderWidth mirrors the static TableBorders::BorderWidth
// (table_borders.h:97-113): the source style's computed border width on
// side — 0 for a none/hidden style, medium when unset — snapped to whole
// pixels as Blink's computed border widths are (ClampLineWidth through
// ConvertBorderWidth, style_builder_converter.cc:1964-1971 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TableBordersBorderWidth(style *css.Style, side TableBordersEdgeSide) float64 {
	if style == nil {
		return 0
	}
	w := style.GetBorderWidth()
	switch side {
	case TableBordersEdgeSideTop:
		return css.ClampLineWidth(w.Top)
	case TableBordersEdgeSideRight:
		return css.ClampLineWidth(w.Right)
	case TableBordersEdgeSideBottom:
		return css.ClampLineWidth(w.Bottom)
	case TableBordersEdgeSideLeft:
		return css.ClampLineWidth(w.Left)
	}
	return 0
}

// TableBordersBorderStyle mirrors the static TableBorders::BorderStyle
// (table_borders.h:115-138): the source side's border style through
// ComputedStyle::CollapsedBorderStyle, which treats outset as groove and
// inset as ridge in the collapsing model (computed_style.h:592-603 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). pkg/css folds the
// three-dimensional styles into solid for painting, so the raw keyword is
// read here to keep their conflict precedence.
func TableBordersBorderStyle(style *css.Style, side TableBordersEdgeSide) css.BorderStyle {
	if style == nil || side == TableBordersEdgeSideDoNotFill {
		return css.BorderStyleNone
	}
	var folded css.BorderStyle
	bs := style.GetBorderStyle()
	switch side {
	case TableBordersEdgeSideTop:
		folded = bs.Top
	case TableBordersEdgeSideRight:
		folded = bs.Right
	case TableBordersEdgeSideBottom:
		folded = bs.Bottom
	default:
		folded = bs.Left
	}
	if folded != css.BorderStyleSolid {
		return folded
	}
	raw, _ := style.Get(tableBordersSideProperty(side) + "-style")
	switch raw {
	case "outset":
		return "groove"
	case "inset":
		return "ridge"
	case "groove", "ridge":
		return css.BorderStyle(raw)
	}
	return css.BorderStyleSolid
}

// TableBordersBorderColor mirrors TableBorders::BorderColor
// (table_borders.cc:254-268 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TableBordersBorderColor(style *css.Style, side TableBordersEdgeSide) css.Color {
	return style.VisitedDependentColor(tableBordersSideProperty(side) + "-color")
}

// isSourceMoreSpecificThanEdge mirrors IsSourceMoreSpecificThanEdge
// (table_borders.cc:24-46 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// css-tables-3 conflict resolution for collapsed borders.
func isSourceMoreSpecificThanEdge(sourceStyle css.BorderStyle, sourceWidth float64, edge TableBordersEdge) bool {
	if edge.EdgeSide == TableBordersEdgeSideDoNotFill {
		return false
	}
	if edge.Style == nil || sourceStyle == css.BorderStyleHidden {
		return true
	}
	edgeStyle := edge.borderStyle
	if edgeStyle == css.BorderStyleHidden {
		return false
	}
	edgeWidth := edge.width
	if sourceWidth < edgeWidth {
		return false
	}
	if sourceWidth > edgeWidth {
		return true
	}
	return EBorderStyleOrder(sourceStyle) > EBorderStyleOrder(edgeStyle)
}

// logicalEdgeSides maps the table's logical sides to the physical edge sides
// that define them: Blink's PhysicalToLogical<EdgeSide>
// (table_borders.cc:486-488 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// computed through ToLogicalEdges so the writing-mode mapping has one home.
func logicalEdgeSides(wdm WritingDirectionMode) (inlineStart, inlineEnd, blockStart, blockEnd TableBordersEdgeSide) {
	l := ToLogicalEdges(PhysicalEdges{
		Top:    float64(TableBordersEdgeSideTop),
		Right:  float64(TableBordersEdgeSideRight),
		Bottom: float64(TableBordersEdgeSideBottom),
		Left:   float64(TableBordersEdgeSideLeft),
	}, wdm)
	return TableBordersEdgeSide(l.InlineStart), TableBordersEdgeSide(l.InlineEnd),
		TableBordersEdgeSide(l.BlockStart), TableBordersEdgeSide(l.BlockEnd)
}

// ComputeTableBorders resolves the collapsed borders of a table from its
// rows (thead, tbodies, tfoot order, cells already placed in grid columns),
// col/colgroup styles and the table style. Mirrors
// TableBorders::ComputeTableBorders (table_borders.cc:115-248 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): cells, then rows, sections,
// cols, colgroups and the table, each later source only winning an edge by
// being strictly more specific. Sections are runs of rows sharing a row
// group (louis14's tableRow carries its group, not a section child list).
func ComputeTableBorders(rows []tableRow, columns []tableColStyle, tableStyle *css.Style, wdm WritingDirectionMode, tableColumnCount int) *TableBorders {
	tb := &TableBorders{lastColumnIndex: int(^uint(0) >> 1)}

	type sectionRun struct {
		start, count int
		style        *css.Style
	}
	var sections []sectionRun
	for i, row := range rows {
		if i == 0 || row.groupNode != rows[i-1].groupNode {
			sections = append(sections, sectionRun{start: i, style: row.groupStyle})
		}
		sections[len(sections)-1].count++
	}

	boxOrder := 0
	// Mark cell borders (:133-168). Rowspans are clamped to their section,
	// which is only known once every row is seen, so a table with a spanning
	// cell redistributes its cell borders in a second pass (:174-198).
	foundMultispanCells := false
	for _, sec := range sections {
		for r := sec.start; r < sec.start+sec.count; r++ {
			for _, cell := range rows[r].cells {
				foundMultispanCells = foundMultispanCells || cell.rowSpan > 1 || cell.colSpan > 1
				if !foundMultispanCells {
					boxOrder++
					tb.mergeBorders(r, cell.colIndex, max(cell.rowSpan, 1), max(cell.colSpan, 1), cell.style, TableBordersEdgeSourceCell, boxOrder, wdm, -1)
				}
			}
		}
		tb.sections = append(tb.sections, TableBordersSection{StartRow: sec.start, RowCount: sec.count})
	}
	tb.lastColumnIndex = tableColumnCount
	tableRowCount := len(rows)
	if foundMultispanCells {
		for si, sec := range sections {
			for r := sec.start; r < sec.start+sec.count; r++ {
				for _, cell := range rows[r].cells {
					boxOrder++
					tb.mergeBorders(r, cell.colIndex, max(cell.rowSpan, 1), max(cell.colSpan, 1), cell.style, TableBordersEdgeSourceCell, boxOrder, wdm, si)
				}
			}
		}
	}

	// Mark row borders (:200-210).
	for r, row := range rows {
		boxOrder++
		tb.mergeBorders(r, 0, 1, tableColumnCount, row.style, TableBordersEdgeSourceRow, boxOrder, wdm, -1)
	}

	// Mark section borders (:212-224).
	for _, sec := range sections {
		boxOrder++
		tb.mergeBorders(sec.start, 0, sec.count, tableColumnCount, sec.style, TableBordersEdgeSourceSection, boxOrder, wdm, -1)
	}

	// Mark column borders: every COL first, then every COLGROUP, each kind
	// sharing one box order (:226-238). A col merges per spanned column
	// (ColBordersMarker::VisitCol, :52-61); a colgroup merges once across its
	// span (ColgroupBordersMarker::LeaveColgroup, :91-98).
	boxOrder++
	for _, col := range columns {
		if col.isGroup {
			continue
		}
		for i := 0; i < col.span && col.colIndex+i < tableColumnCount; i++ {
			tb.mergeBorders(0, col.colIndex+i, tableRowCount, 1, col.style, TableBordersEdgeSourceColumn, boxOrder, wdm, -1)
		}
	}
	boxOrder++
	for _, col := range columns {
		if !col.isGroup || col.colIndex >= tableColumnCount {
			continue
		}
		tb.mergeBorders(0, col.colIndex, tableRowCount, col.span, col.style, TableBordersEdgeSourceColumn, boxOrder, wdm, -1)
	}

	// Mark table borders (:240-243).
	boxOrder++
	tb.mergeBorders(0, 0, tableRowCount, tableColumnCount, tableStyle, TableBordersEdgeSourceTable, boxOrder, wdm, -1)

	tb.updateTableBorder(tableRowCount, tableColumnCount)
	return tb
}

// EdgesPerRow mirrors TableBorders::EdgesPerRow (table_borders.h:181).
func (tb *TableBorders) EdgesPerRow() int { return tb.edgesPerRow }

// EdgeCount mirrors TableBorders::EdgeCount (table_borders.h:226).
func (tb *TableBorders) EdgeCount() int { return len(tb.edges) }

// TableBorder mirrors TableBorders::TableBorder (table_borders.h:183): the
// table's own border, half of each outer collapsed edge.
func (tb *TableBorders) TableBorder() LogicalEdges { return tb.tableBorder }

// BorderWidth mirrors TableBorders::BorderWidth(edge_index) (table_borders.h:151-154).
func (tb *TableBorders) BorderWidth(i int) float64 { return tb.edges[i].width }

// BorderStyle mirrors TableBorders::BorderStyle(edge_index) (table_borders.h:156-159).
func (tb *TableBorders) BorderStyle(i int) css.BorderStyle {
	if tb.edges[i].Style == nil {
		return css.BorderStyleNone
	}
	return tb.edges[i].borderStyle
}

// BorderColor mirrors TableBorders::BorderColor(edge_index) (table_borders.h:161-164).
func (tb *TableBorders) BorderColor(i int) css.Color {
	return TableBordersBorderColor(tb.edges[i].Style, tb.edges[i].EdgeSide)
}

// BoxOrder mirrors TableBorders::BoxOrder (table_borders.h:166-168).
func (tb *TableBorders) BoxOrder(i int) int { return tb.edges[i].BoxOrder }

// Sections returns the table's sections in grid order.
func (tb *TableBorders) Sections() []TableBordersSection { return tb.sections }

// canPaint mirrors TableBorders::CanPaint(edge_index) (table_borders.h:228-238).
func (tb *TableBorders) canPaint(i int) bool {
	if i >= len(tb.edges) || tb.edges[i].Style == nil {
		return false
	}
	s := tb.BorderStyle(i)
	if s == css.BorderStyleNone || s == css.BorderStyleHidden {
		return false
	}
	return tb.BorderWidth(i) != 0
}

// clampColspan mirrors TableBorders::ClampColspan (table_borders.h:254-257).
func (tb *TableBorders) clampColspan(column, colspan int) int {
	return min(colspan, tb.lastColumnIndex-column)
}

// clampRowspan mirrors TableBorders::ClampRowspan (table_borders.h:260-269).
func (tb *TableBorders) clampRowspan(sectionIndex, tableRowIndex, rowspan int) int {
	if rowspan <= 1 {
		return rowspan
	}
	sec := tb.sections[sectionIndex]
	return min(rowspan, sec.RowCount-(tableRowIndex-sec.StartRow))
}

// getCellBorders mirrors TableBorders::GetCellBorders (table_borders.cc:335-387
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): each side is half the widest
// paintable edge along it.
func (tb *TableBorders) getCellBorders(row, column, rowspan, colspan int) LogicalEdges {
	var s LogicalEdges
	if tb.edgesPerRow == 0 {
		return s
	}
	if column*2 >= tb.edgesPerRow || row >= len(tb.edges)/tb.edgesPerRow {
		return s
	}
	width := func(i int) float64 {
		if tb.canPaint(i) {
			return tb.BorderWidth(i)
		}
		return 0
	}
	firstInlineStartEdge := row*tb.edgesPerRow + column*2
	firstInlineEndEdge := firstInlineStartEdge + colspan*2
	for i := 0; i < rowspan; i++ {
		startEdge := firstInlineStartEdge + i*tb.edgesPerRow
		s.InlineStart = max(s.InlineStart, width(startEdge))
		if startEdge >= len(tb.edges) {
			break
		}
		s.InlineEnd = max(s.InlineEnd, width(firstInlineEndEdge+i*tb.edgesPerRow))
	}
	startEdgeColumnIndex := column*2 + 1
	for i := 0; i < colspan; i++ {
		current := startEdgeColumnIndex + i*2
		if current >= tb.edgesPerRow {
			break
		}
		startEdge := row*tb.edgesPerRow + current
		s.BlockStart = max(s.BlockStart, width(startEdge))
		s.BlockEnd = max(s.BlockEnd, width(startEdge+rowspan*tb.edgesPerRow))
	}
	s.BlockStart /= 2
	s.BlockEnd /= 2
	s.InlineStart /= 2
	s.InlineEnd /= 2
	return s
}

// updateTableBorder mirrors TableBorders::UpdateTableBorder
// (table_borders.cc:389-398 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (tb *TableBorders) updateTableBorder(tableRowCount, tableColumnCount int) {
	if tb.edgesPerRow == 0 {
		tb.tableBorder = LogicalEdges{}
		return
	}
	tb.tableBorder = tb.getCellBorders(0, 0, tableRowCount, tableColumnCount)
}

// CellBorder mirrors TableBorders::CellBorder's collapsed branch
// (table_borders.cc:400-410 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the cell's half of each collapsed edge around it.
func (tb *TableBorders) CellBorder(row, column, section, rowspan, colspan int) LogicalEdges {
	return tb.getCellBorders(row, column, tb.clampRowspan(section, row, rowspan), tb.clampColspan(column, colspan))
}

// SectionIndexForRow returns the index of the section holding table row row.
func (tb *TableBorders) SectionIndexForRow(row int) int {
	for i, s := range tb.sections {
		if row >= s.StartRow && row < s.StartRow+s.RowCount {
			return i
		}
	}
	return 0
}

// mergeBorders mirrors TableBorders::MergeBorders (table_borders.cc:433-503 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). sectionIndex is only used to
// clamp a cell's rowspan; -1 leaves it unclamped (Blink's kNotFound).
func (tb *TableBorders) mergeBorders(startRow, startColumn, rowspan, colspan int, source *css.Style, sourceKind TableBordersEdgeSource, boxOrder int, wdm WritingDirectionMode, sectionIndex int) {
	if rowspan == 0 || colspan == 0 || source == nil {
		return
	}
	clampedColspan := tb.clampColspan(startColumn, colspan)
	if clampedColspan <= 0 {
		return
	}
	clampedRowspan := rowspan
	if sourceKind == TableBordersEdgeSourceCell && sectionIndex >= 0 {
		clampedRowspan = tb.clampRowspan(sectionIndex, startRow, rowspan)
	}
	markInnerBorders := sourceKind == TableBordersEdgeSourceCell && (clampedRowspan > 1 || clampedColspan > 1)

	inlineStart, inlineEnd, blockStart, blockEnd := logicalEdgeSides(wdm)
	if markInnerBorders {
		tb.ensureCellColumnFits(startColumn + clampedColspan - 1)
		tb.ensureCellRowFits(startRow + clampedRowspan - 1)
	} else {
		none := func(side TableBordersEdgeSide) bool {
			return TableBordersBorderStyle(source, side) == css.BorderStyleNone
		}
		if none(inlineStart) && none(inlineEnd) && none(blockStart) && none(blockEnd) {
			return
		}
		// Only ensure the edges that will be assigned exist.
		if none(inlineEnd) && none(blockStart) && none(blockEnd) {
			tb.ensureCellColumnFits(startColumn)
		} else {
			tb.ensureCellColumnFits(startColumn + clampedColspan - 1)
		}
		if none(inlineStart) && none(inlineEnd) && none(blockEnd) {
			tb.ensureCellRowFits(startRow)
		} else {
			tb.ensureCellRowFits(startRow + clampedRowspan - 1)
		}
	}

	tb.mergeRowAxisBorder(startRow, startColumn, clampedColspan, source, boxOrder, blockStart)
	tb.mergeRowAxisBorder(startRow+clampedRowspan, startColumn, clampedColspan, source, boxOrder, blockEnd)
	tb.mergeColumnAxisBorder(startRow, startColumn, clampedRowspan, source, boxOrder, inlineStart)
	tb.mergeColumnAxisBorder(startRow, startColumn+clampedColspan, clampedRowspan, source, boxOrder, inlineEnd)
	if markInnerBorders {
		tb.markInnerBordersAsDoNotFill(startRow, startColumn, clampedRowspan, clampedColspan)
	}
}

// mergeRowAxisBorder mirrors TableBorders::MergeRowAxisBorder
// (table_borders.cc:505-527 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (tb *TableBorders) mergeRowAxisBorder(startRow, startColumn, colspan int, source *css.Style, boxOrder int, side TableBordersEdgeSide) {
	sourceStyle := TableBordersBorderStyle(source, side)
	if sourceStyle == css.BorderStyleNone {
		return
	}
	sourceWidth := TableBordersBorderWidth(source, side)
	startEdge := tb.edgesPerRow*startRow + startColumn*2 + 1
	endEdge := startEdge + colspan*2
	for e := startEdge; e < endEdge && e < len(tb.edges); e += 2 {
		if isSourceMoreSpecificThanEdge(sourceStyle, sourceWidth, tb.edges[e]) {
			tb.edges[e] = newTableBordersEdge(source, side, boxOrder)
		}
	}
}

// mergeColumnAxisBorder mirrors TableBorders::MergeColumnAxisBorder
// (table_borders.cc:529-551 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (tb *TableBorders) mergeColumnAxisBorder(startRow, startColumn, rowspan int, source *css.Style, boxOrder int, side TableBordersEdgeSide) {
	sourceStyle := TableBordersBorderStyle(source, side)
	if sourceStyle == css.BorderStyleNone {
		return
	}
	sourceWidth := TableBordersBorderWidth(source, side)
	startEdge := tb.edgesPerRow*startRow + startColumn*2
	endEdge := startEdge + rowspan*tb.edgesPerRow
	for e := startEdge; e < endEdge && e < len(tb.edges); e += tb.edgesPerRow {
		if isSourceMoreSpecificThanEdge(sourceStyle, sourceWidth, tb.edges[e]) {
			tb.edges[e] = newTableBordersEdge(source, side, boxOrder)
		}
	}
}

// markInnerBordersAsDoNotFill mirrors TableBorders::MarkInnerBordersAsDoNotFill
// (table_borders.cc:553-585 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (tb *TableBorders) markInnerBordersAsDoNotFill(startRow, startColumn, rowspan, colspan int) {
	mark := func(e int) {
		if e < len(tb.edges) && tb.edges[e].Style == nil {
			tb.edges[e].EdgeSide = TableBordersEdgeSideDoNotFill
		}
	}
	startEdge := startColumn*2 + 2
	endEdge := startEdge + (colspan-1)*2
	for row := startRow; row < startRow+rowspan && startEdge != endEdge; row++ {
		off := row * tb.edgesPerRow
		for e := off + startEdge; e < off+endEdge; e += 2 {
			mark(e)
		}
	}
	startEdge = startColumn*2 + 1
	endEdge = startEdge + colspan*2
	for row := startRow + 1; row < startRow+rowspan; row++ {
		off := row * tb.edgesPerRow
		for e := off + startEdge; e < off+endEdge; e += 2 {
			mark(e)
		}
	}
}

// ensureCellColumnFits mirrors TableBorders::EnsureCellColumnFits
// (table_borders.cc:587-623 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// re-laying every row out at the wider stride.
func (tb *TableBorders) ensureCellColumnFits(cellColumn int) {
	desired := (cellColumn + 2) * 2
	if desired <= tb.edgesPerRow {
		return
	}
	rowCount := 1
	if tb.edgesPerRow != 0 {
		rowCount = len(tb.edges) / tb.edgesPerRow
	}
	grown := make([]TableBordersEdge, rowCount*desired)
	for r := 0; r < rowCount && tb.edgesPerRow != 0; r++ {
		copy(grown[r*desired:r*desired+tb.edgesPerRow], tb.edges[r*tb.edgesPerRow:(r+1)*tb.edgesPerRow])
	}
	tb.edges = grown
	tb.edgesPerRow = desired
}

// ensureCellRowFits mirrors TableBorders::EnsureCellRowFits
// (table_borders.cc:625-633 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (tb *TableBorders) ensureCellRowFits(cellRow int) {
	current := len(tb.edges) / tb.edgesPerRow
	desired := cellRow + 2
	if desired <= current {
		return
	}
	tb.edges = append(tb.edges, make([]TableBordersEdge, (desired-current)*tb.edgesPerRow)...)
}

// CollapsedTableBordersGeometry carries the grid lines collapsed borders paint
// along: Blink's CollapsedTableBordersGeometry columns (table_layout_algorithm.cc:
// 863-880 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) plus each section's
// row offsets, which Blink stores on the section fragments
// (TableSectionLayoutAlgorithm, table_section_layout_algorithm.cc:47-165 @
// same SHA). louis14 emits section fragments only for positioned row groups,
// so the section geometry rides on the table fragment. All offsets are
// logical, relative to the table's border box.
type CollapsedTableBordersGeometry struct {
	WritingDirection WritingDirectionMode
	Columns          []float64
	Sections         []TableSectionCollapsedBordersGeometry
}

// TableSectionCollapsedBordersGeometry mirrors a section fragment's
// TableSectionStartRowIndex / TableSectionRowOffsets: RowOffsets holds the
// block offset of each of the section's rows plus its end.
type TableSectionCollapsedBordersGeometry struct {
	StartRowIndex int
	RowOffsets    []float64
}

// restoreCollapsedCellSourceStyles puts each cell's own style back where a
// previous layout of the table swapped in its resolved-border clone, so
// borders resolve from the authored styles every time.
func restoreCollapsedCellSourceStyles(rows []tableRow) {
	for r := range rows {
		for c := range rows[r].cells {
			cell := &rows[r].cells[c]
			if cell.node != nil && cell.node.collapsedBorderSourceStyle != nil {
				cell.style = cell.node.collapsedBorderSourceStyle
				cell.node.style = cell.style
			}
		}
	}
}

// applyCollapsedCellBorders gives every cell its collapsed border: Blink's
// TableBorders::CellBorder share, which Blink hands the cell through its
// constraint space (ConstraintSpace::TableCellBorders, read by
// ComputeBorders, length_utils.cc:1491-1493 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14 carries it by swapping
// in a cell style clone whose border widths are the share.
//
// Divergence: Blink gives the table its own border, TableBorder(), half of
// each outer edge (ComputeBorders' table branch, length_utils.cc:1495-1497);
// louis14 lays a collapsed table out with no border, so the table's half of
// each outer edge folds into the outer cells' share. Cell content lands where
// Blink puts it; only the outer cells' border boxes extend to the table edge.
func applyCollapsedCellBorders(rows []tableRow, tb *TableBorders, numCols int, wdm WritingDirectionMode) {
	tableBorder := tb.TableBorder()
	for rowIdx := range rows {
		section := tb.SectionIndexForRow(rowIdx)
		for cellIdx := range rows[rowIdx].cells {
			cell := &rows[rowIdx].cells[cellIdx]
			if cell.style == nil {
				continue
			}
			rowspan, colspan := max(cell.rowSpan, 1), max(cell.colSpan, 1)
			b := tb.CellBorder(rowIdx, cell.colIndex, section, rowspan, colspan)
			if cell.colIndex == 0 {
				b.InlineStart += tableBorder.InlineStart
			}
			if cell.colIndex+colspan >= numCols {
				b.InlineEnd += tableBorder.InlineEnd
			}
			if rowIdx == 0 {
				b.BlockStart += tableBorder.BlockStart
			}
			if rowIdx+rowspan >= len(rows) {
				b.BlockEnd += tableBorder.BlockEnd
			}
			source := cell.style
			clone := source.Clone()
			p := ToPhysicalEdges(b, wdm)
			for _, side := range []struct {
				name  string
				width float64
			}{{"top", p.Top}, {"right", p.Right}, {"bottom", p.Bottom}, {"left", p.Left}} {
				style := css.BorderStyleSolid
				if side.width <= 0 {
					style = css.BorderStyleNone
				}
				clone.Properties["border-"+side.name+"-style"] = string(style)
				clone.Properties["border-"+side.name+"-width"] = fmt.Sprintf("%.6gpx", side.width)
			}
			cell.style = clone
			if cell.node != nil {
				if cell.node.collapsedBorderSourceStyle == nil {
					cell.node.collapsedBorderSourceStyle = source
				}
				cell.node.style = clone
			}
		}
	}
}

// collapsedTableBordersGeometry builds the grid lines a collapsed table's
// borders paint along: the column start offsets plus the grid end (Blink's
// CollapsedTableBordersGeometry, table_layout_algorithm.cc:863-880 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and each section's row offsets
// (table_section_layout_algorithm.cc:47, :122-129 @ same SHA), relative to
// the table border box. rowStarts holds each row's content-box block offset
// and the last row's end.
//
// Because louis14's collapsed table has no border of its own (see
// applyCollapsedCellBorders), its grid starts at the content box; the outer
// grid lines are moved in by the table's half of the outer edges so they sit
// where Blink's do, at the table border's inner edge.
func collapsedTableBordersGeometry(tb *TableBorders, wdm WritingDirectionMode, geom FragmentGeometry, colWidths, rowStarts []float64) *CollapsedTableBordersGeometry {
	tableBorder := tb.TableBorder()
	inlineOrigin := geom.Border.InlineStart + geom.Padding.InlineStart
	blockOrigin := geom.Border.BlockStart + geom.Padding.BlockStart

	columns := make([]float64, len(colWidths)+1)
	offset := inlineOrigin
	for i, w := range colWidths {
		columns[i] = offset
		offset += w
	}
	columns[len(colWidths)] = offset
	columns[0] += tableBorder.InlineStart
	columns[len(colWidths)] -= tableBorder.InlineEnd

	g := &CollapsedTableBordersGeometry{WritingDirection: wdm, Columns: columns}
	lastRow := len(rowStarts) - 1
	for _, sec := range tb.Sections() {
		offsets := make([]float64, sec.RowCount+1)
		for i := range offsets {
			row := sec.StartRow + i
			offsets[i] = blockOrigin + rowStarts[row]
			if row == 0 {
				offsets[i] += tableBorder.BlockStart
			}
			if row == lastRow {
				offsets[i] -= tableBorder.BlockEnd
			}
		}
		g.Sections = append(g.Sections, TableSectionCollapsedBordersGeometry{StartRowIndex: sec.StartRow, RowOffsets: offsets})
	}
	return g
}
