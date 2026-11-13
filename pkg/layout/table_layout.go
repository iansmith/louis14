package layout

import (
	"sort"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
)

// TableLayoutAlgorithm implements CSS 2.1 §17 table layout.
// This is a basic implementation handling display:table, table-row-group,
// table-header-group, table-footer-group, table-row, and table-cell.
//
// Mirrors Blink's TableLayoutAlgorithm.
type TableLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace
}

// tableInternalOOFSpace is the constraint space the OutOfFlowLayoutPart of
// a table row or section runs in: louis14 lays table rows and sections out
// unfragmented, so their out-of-flow descendants are never deferred to a
// fragmentation context root. No Blink analog: Blink's
// TableRowLayoutAlgorithm / TableSectionLayoutAlgorithm run it in their own
// (possibly fragmented) space.
func tableInternalOOFSpace(wdm WritingDirectionMode) ConstraintSpace {
	return ConstraintSpace{WritingDirection: wdm}
}

// NewTableLayoutAlgorithm creates a table layout algorithm.
func NewTableLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *TableLayoutAlgorithm {
	return &TableLayoutAlgorithm{
		ctx:   ctx,
		node:  node,
		style: node.Style(),
		space: space,
	}
}

// tableCell tracks a cell during table layout.
type tableCell struct {
	node     *LayoutInputNode
	style    *css.Style
	colIndex int
	colSpan  int
	rowSpan  int
}

// tableRow tracks a row during table layout.
type tableRow struct {
	node       *LayoutInputNode
	style      *css.Style
	groupStyle *css.Style       // style of the containing row group (thead/tbody/tfoot), nil if none
	groupNode  *LayoutInputNode // LayoutInputNode of the containing row group, nil if none
	cells      []tableCell
}

// tableColWidth tracks an explicit column width from a col/colgroup element.
// Per CSS Tables, col/colgroup width properties establish column widths.
type tableColWidth struct {
	colIndex int
	span     int     // number of columns covered
	width    float64 // resolved width in pixels (0 = not set)
	minWidth float64 // resolved min-width in pixels; hasMin false means the value was absent, auto, or a percentage/calc-% (which GetLength rejects)
	hasMin   bool    // true if min-width is a concrete px length
	maxWidth float64 // resolved max-width in pixels; hasMax false means the value was absent, auto, or a percentage/calc-% (which GetLength rejects)
	hasMax   bool    // true if max-width is a concrete px length
}

// extractColWidthSpec reads width/min-width/max-width constraints from a
// col/colgroup computed style. Returns the populated spec and true if any
// concrete constraint is present; (zero, false) otherwise.
func extractColWidthSpec(style *css.Style, colIndex, span int) (tableColWidth, bool) {
	w, okWidth := style.GetLength("width")
	mw, okMin := style.GetLength("min-width")
	xw, okMax := style.GetLength("max-width")
	// Include a spec when ANY of width>0, okMin, or okMax holds — a
	// min-width-only col must still produce a spec (col-definite-min-size row 1).
	if (okWidth && w > 0) || okMin || okMax {
		return tableColWidth{
			colIndex: colIndex,
			span:     span,
			width:    w,
			minWidth: mw,
			hasMin:   okMin,
			maxWidth: xw,
			hasMax:   okMax,
		}, true
	}
	return tableColWidth{}, false
}

// tableColStyle carries a col/colgroup element's computed style for use in
// border-collapse resolution (CSS 2.1 §17.6.2.1).
type tableColStyle struct {
	colIndex int
	span     int
	style    *css.Style
	isGroup  bool // true = colgroup (lower priority than col)
}

// Layout performs table layout and returns the result.
func (tla *TableLayoutAlgorithm) Layout() *LayoutResult {
	wdm := tla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(tla.ctx, tla.node, tla.style, wdm, tla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(tla.node)

	// Collect rows, captions, col/colgroup widths, and col/colgroup border styles.
	rows, captions, colWidthSpecs, colStyleSpecs := tla.collectRowsAndCaptions()

	// Assign final cell colIndex using a slot grid that honors rowspan
	// reservations from prior rows. Returns the grid's max column count.
	// Mirrors Blink's LayoutTableSection slot tracking.
	numCols := assignColumnIndices(rows)
	if numCols == 0 {
		numCols = 1
	}

	borderCollapse := tla.style.GetBorderCollapse() == css.BorderCollapseCollapse

	// In border-collapse mode, the table's own border is folded into the
	// cell borders. Zero it out in geom so it doesn't affect sizing.
	if borderCollapse {
		geom.Border = LogicalEdges{}
	}

	// Resolve available inline size from the centralized border-box size.
	availableInline := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if availableInline < 0 {
		availableInline = 0
	}

	// CSS Tables §11.5 / §11.5.3 "Computing the table height":
	// the table's used block-size is max(intrinsic content, explicit height,
	// min-height) clamped by max-height. Tables get `box-sizing: border-box`
	// from the UA cascade (element-name gated in cascade.go), so all resolved
	// sizes are border-box inclusive of borders and padding.
	//
	// CalculateInitialFragmentGeometry only applies min/max-block-size when
	// the resolved block-size is definite (fragment_geometry.go ~L879). For
	// tables where height is auto but min-height is set (e.g.
	// css-tables/min-height-table.html), that bypasses min-height entirely.
	// Resolve a separate explicitTableBorderBoxBlock that combines the
	// definite-height path with min/max-block-size so both the row excess
	// distribution (CSS 2.1 §17.5.3) and the final-size floor below see the
	// floored value.
	//
	// Mirrors Blink's TableLayoutAlgorithm::ComputeBlockSize at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f which always honors
	// min/max-block-size on tables regardless of whether height is auto.
	explicitTableBorderBoxBlock := geom.BorderBoxSize.BlockSize
	minBlockContent := ResolveMinBlockSize(tla.style, wdm, tla.space, geom).Float64()
	maxBlockContentLU, hasMaxBlock := ResolveMaxBlockSize(tla.style, wdm, tla.space, geom)
	contentToBorderBox := func(content float64) float64 {
		return content + geom.BlockBorderPadding()
	}
	// CSS Sizing §1.2: apply max first, then min. min wins when min > max.
	if hasMaxBlock {
		maxBorderBox := contentToBorderBox(maxBlockContentLU.Float64())
		if explicitTableBorderBoxBlock != Indefinite && explicitTableBorderBoxBlock > maxBorderBox {
			explicitTableBorderBoxBlock = maxBorderBox
		}
	}
	if minBlockContent > 0 {
		minBorderBox := contentToBorderBox(minBlockContent)
		if explicitTableBorderBoxBlock == Indefinite || minBorderBox > explicitTableBorderBoxBlock {
			explicitTableBorderBoxBlock = minBorderBox
		}
	}

	// The table's block size is specified (not auto), which lets a cell
	// that grew have a definite initial block size (TableLayoutAlgorithm::
	// Layout, table/table_layout_algorithm.cc:313 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	isTableBlockSizeSpecified := !isAutoSizeInDirection(tla.style, wdm, false)

	// Record the table's content-box size so row / section / cell fragments
	// added via builder.AddChild get their position:relative offset resolved
	// against the correct CB (CSS 2.1 §9.4.3). Mirrors Blink's
	// FragmentBuilder::SetAvailableSize usage in table_layout_algorithm.cc.
	tableContentBlock := Indefinite
	if explicitTableBorderBoxBlock != Indefinite {
		tableContentBlock = explicitTableBorderBoxBlock - geom.BlockBorderPadding()
		if tableContentBlock < 0 {
			tableContentBlock = 0
		}
	}
	builder.SetChildAvailableSize(LogicalSize{
		InlineSize: availableInline,
		BlockSize:  tableContentBlock,
	})

	// Compute logical border spacing (CSS 2.1 §17.6.1).
	// In collapsed mode, spacing is always zero.
	inlineSpacing, blockSpacing := 0.0, 0.0
	if !borderCollapse {
		inlineSpacing, blockSpacing = tla.logicalBorderSpacing()
	}
	// Check if the table has an explicit inline-size (width/height).
	_, hasExplicitTableWidth := ResolveInlineSize(tla.style, wdm, tla.space, geom)

	// Subtract inline spacing from available width for column sizing.
	spacingForCols := 0.0
	if numCols > 0 {
		spacingForCols = inlineSpacing * float64(numCols+1) // before first + between + after last
	}
	availableForCols := availableInline - spacingForCols
	if availableForCols < 0 {
		availableForCols = 0
	}
	// In border-collapse mode, resolve the collapsed borders BEFORE
	// computing column widths and laying out cells: Blink computes
	// TableBorders up front and threads each cell's share through its
	// constraint space (TableBorders::ComputeTableBorders /
	// TableBorders::CellBorder, table_borders.cc:115-248, :400-410 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	var tableBorders *TableBorders
	if borderCollapse {
		tableBorders = resolveCollapsedCellBorders(rows, colStyleSpecs, tla.style, wdm, numCols)
	}

	// Choose the column width algorithm: fixed or auto table layout.
	var colWidths []float64
	if tla.style.GetTableLayout() == css.TableLayoutFixed && hasExplicitTableWidth {
		colWidths = tla.computeColumnWidthsFixed(rows, numCols, availableForCols, spacingForCols, colWidthSpecs)
	} else {
		colWidths = tla.computeColumnWidths(rows, numCols, availableForCols, borderCollapse, hasExplicitTableWidth, geom, captions, colWidthSpecs)
	}

	// Track which columns have explicit col/colgroup widths (used for orthogonal cell row height).
	colHasExplicit := make([]bool, numCols)
	for _, cws := range colWidthSpecs {
		for s := 0; s < cws.span; s++ {
			ci := cws.colIndex + s
			if ci < numCols {
				colHasExplicit[ci] = true
			}
		}
	}

	// Compute table size.
	contentInlineSize := 0.0
	for _, w := range colWidths {
		contentInlineSize += w
	}
	contentInlineSize += spacingForCols // include inline spacing in total

	// When a parent algorithm fixed the table's inline size, an auto-layout
	// table's columns already fill it (computeColumnWidths). The fixed-layout
	// column widths do not yet spread excess width over explicit columns
	// (Blink SynchronizeAssignableTableInlineSizeAndColumnsFixed, table/
	// table_layout_utils.cc:776 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so
	// the fixed size still floors the border box here.
	borderBoxInline := contentInlineSize + geom.InlineBorderPadding()
	if tla.space.IsFixedInlineSize && geom.BorderBoxSize.InlineSize > borderBoxInline {
		borderBoxInline = geom.BorderBoxSize.InlineSize
	}

	// Separate top and bottom captions.
	var topCaptions, bottomCaptions []tableCaption
	for _, cap := range captions {
		if cap.side == "bottom" {
			bottomCaptions = append(bottomCaptions, cap)
		} else {
			topCaptions = append(topCaptions, cap)
		}
	}

	// Lay out and add the top (block-start) captions. Mirrors Blink
	// GenerateFragment's top-caption loop (table_layout_algorithm.cc:989-993
	// @ d4ecdfed88f9).
	tableContentInline := borderBoxInline - geom.InlineBorderPadding()
	blockOffset := 0.0
	for _, cap := range topCaptions {
		tla.addCaptionResult(builder, wdm,
			tla.layoutCaption(cap, wdm, tableContentInline), &blockOffset)
	}

	// Layout each row — single-pass pipeline (refactor-b ckpt 3).
	//
	// Mirrors Blink's NGTableLayoutAlgorithm::Layout (table_layout_algorithm.cc
	// ~L2285) in three phases:
	//
	//   Phase 1 — measure: lay out every cell once to collect intrinsic
	//             row heights, baselines, cell alignment data. No row
	//             fragments are appended to the table builder yet.
	//
	//   Phase 2 — size: build TableTypesRows via computeRows, then run
	//             distributeTableBlockSizeToRows (5-priority distribution
	//             matching table_layout_utils.cc DistributeExcessBlockSize
	//             ToRows). Finalized row block-sizes are then read by the
	//             assembly phase — no post-append mutation, no reposition
	//             sweep.
	//
	//   Phase 3 — assemble: build each row fragment at its final block-size,
	//             laying each cell out again at its final block-size, which
	//             applies its §17.5.4 vertical-align.
	//
	// Rows and sections are structural-only (no NGLayoutResult of their own
	// in Blink); OOF candidates from cell content are collected directly at
	// the table builder at their final block-offsets.

	// Add block-start spacing before first row (if border-spacing is non-zero).
	if len(rows) > 0 && blockSpacing > 0 {
		blockOffset += blockSpacing
	}

	// Per-cell measurement data captured during Phase 1 and consumed by
	// Phase 3.
	type cellMeasurement struct {
		result       *LayoutResult
		inlineOffset float64
		cell         tableCell
		// layoutInlineSize and cellWDM rebuild the cell's space for its
		// final layout (Phase 3).
		layoutInlineSize float64
		cellWDM          WritingDirectionMode
		// intrinsicBlockSize is the cell's own minimum block-size — the
		// value that contributes to row intrinsic for rowSpan == 1 cells,
		// and that DistributeRowspanCellToRows distributes across spanned
		// rows for rowSpan > 1 cells.
		intrinsicBlockSize float64
		rowIndex           int // originating row for rowspan cells
	}
	type rowMeasurement struct {
		intrinsic       float64
		hasExplicit     bool
		baseline        float64
		hasRowspanStart bool
		cells           []cellMeasurement
	}
	measured := make([]rowMeasurement, len(rows))

	// Build the immutable TableConstraintSpaceData pointer that is threaded
	// through each cell's sub-space (and in the future, row/section
	// sub-spaces). Mirrors Blink's
	// TableLayoutAlgorithm::CreateConstraintSpaceData
	// (table_layout_algorithm.cc ~L979-1044).
	//
	// In louis14 today cell layout does not read from the section data —
	// but the pointer is shared by every sub-space so that future features
	// (percentage resolution against section block-size, rowspan
	// distribution) can publish final sizes via one in-place update after
	// distribution.
	tableData := &TableConstraintSpaceData{
		Rows:              make(TableTypesRows, len(rows)),
		ColumnInlineSizes: append([]float64(nil), colWidths...),
	}
	if len(rows) > 0 {
		tableData.Sections = []TableTypesSection{{
			StartRowIndex: 0,
			RowCount:      len(rows),
		}}
	}

	// --- Phase 1: measure ---
	for rowIdx, row := range rows {
		rowHeight := 0.0
		cellLayouts := make([]cellMeasurement, 0, len(row.cells))

		for _, cell := range row.cells {
			// cell.colIndex was assigned by assignColumnIndices and
			// honors rowspan reservations from earlier rows.
			colIdx := cell.colIndex

			// Compute cell width from column widths.
			cellWidth := 0.0
			for c := colIdx; c < colIdx+cell.colSpan && c < numCols; c++ {
				cellWidth += colWidths[c]
			}

			// Compute inline offset for this cell within the row, including inline spacing.
			cellInlineOffset := inlineSpacing // start with spacing before first column
			for c := 0; c < colIdx && c < numCols; c++ {
				cellInlineOffset += colWidths[c] + inlineSpacing
			}

			// Layout the cell's content via block layout algorithm.
			cellWDM := wdm
			if cell.style != nil {
				cellWDM = NewWritingDirectionMode(cell.style)
			}

			layoutWidth := cellWidth

			// The column width is a fixed inline-size constraint from the table's
			// perspective. For parallel (same-axis) cells this becomes a fixed
			// InlineSize; for orthogonal cells the builder swaps it to
			// IsFixedBlockSize so the cell fills the column width in its block
			// direction (= the table's physical column width).
			//
			// The TableSectionData pointer threads the (soon-to-be-final)
			// row sizing vector into the cell sub-space, mirroring Blink's
			// TableSectionConstraintSpaceBuilder chain.
			//
			// The percentage-resolution block size is Indefinite, as in
			// Blink's SetupTableCellConstraintSpaceBuilder
			// (third_party/blink/renderer/core/layout/table/table_layout_utils.cc:1363,
			// SetPercentageResolutionSize({percentage_inline_size,
			// kIndefiniteSize}) at :1411-1412, quoting css-tables-3
			// #computing-the-table-height at :1409-1410), so a %-height
			// cell child is auto. Blink's measure pass also lays the cell
			// out with an indefinite initial block size (:222-230); its
			// layout pass makes it definite only when ComputeCellBlockSize
			// says so (:1333-1361: a fixed cell height, or a row that grew in
			// a table with a specified block size), which Phase 3 does.
			// All @ d4ecdfed88f962439247c2ad36b8fe47805b1520. A relative
			// cell's percentage insets resolve against its row instead
			// (Phase 3, relativeCellOffset).
			cellSpace := tla.tableCellConstraintSpace(cellWDM, layoutWidth, Indefinite, true, tableData)

			cellResult := layoutElement(tla.ctx, cell.node, cellSpace)
			cellLogical := NewLogicalFragment(wdm, cellResult.Fragment)

			// For orthogonal cells (e.g. vertical-rl in horizontal table):
			// The cell's physical height (table block direction) comes from the cell's
			// logical INLINE size in the cell's own WDM. However, for orthogonal cells
			// where a col/colgroup provides an explicit column width, the row height is
			// constrained to the column width (making the cell square). This matches
			// browser behavior where the col's block-size (= table inline) also
			// determines the row height for orthogonal cells.
			isOrthogonalCell := wdm.IsVertical() != cellWDM.IsVertical()
			// The measured cell is its intrinsic size, border-box
			// (block_layout.go's table-cell branch); the row also takes the
			// cell's fixed block size (cell_css_block_size), ignoring its
			// min/max block sizes (ComputeMinimumRowBlockSize, table/
			// table_layout_utils.cc:265-292 @
			// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
			cellBlockForRow := cellLogical.BlockSize() // table's block direction = physical height
			if size, ok := cellCSSBlockSize(cell.style, wdm, cellResult.Fragment); ok {
				cellBlockForRow = max(cellBlockForRow, size)
			}
			if isOrthogonalCell && colHasExplicit[colIdx] {
				// Use the cell's physical WIDTH (= column width) as the row height.
				// This produces square cells when col width is explicitly specified.
				cellBlockForRow = cellResult.Fragment.Size.WidthF64()
			}
			// Rowspan cells contribute to MULTIPLE rows' intrinsic
			// block-sizes via DistributeRowspanCellToRows after Phase 1
			// — their block-size is *not* attributed to the originating
			// row's intrinsic. Mirrors Blink's
			// ComputeMinimumRowBlockSize, which excludes rowspan-
			// originator cells from the per-row max and runs
			// DistributeRowspanCellToRows separately
			// (table_layout_utils.cc ~L1606).
			if cell.rowSpan == 1 {
				if cellBlockForRow > rowHeight {
					rowHeight = cellBlockForRow
				}
			}

			// Track if any cell has an explicit height (for distribution).
			if cell.style != nil {
				if _, ok := ResolveBlockSize(cell.style, wdm, ConstraintSpace{}, ComputeFragmentGeometry(cell.style, wdm)); ok {
					measured[rowIdx].hasExplicit = true
				}
			}

			cellLayouts = append(cellLayouts, cellMeasurement{
				result:             cellResult,
				inlineOffset:       cellInlineOffset,
				cell:               cell,
				layoutInlineSize:   layoutWidth,
				cellWDM:            cellWDM,
				intrinsicBlockSize: cellBlockForRow,
				rowIndex:           rowIdx,
			})
		}

		// Track cell baselines for this row (CSS 2.1 §17.5.2).
		// The row baseline is the max cell first-baseline across all cells
		// that have a baseline, mirroring Blink's RowBaselineTabulator.
		rowBaseline := 0.0
		for _, cl := range cellLayouts {
			if cl.result.Baseline > 0 && cl.result.Baseline > rowBaseline {
				rowBaseline = cl.result.Baseline
			}
		}

		measured[rowIdx].intrinsic = rowHeight
		measured[rowIdx].baseline = rowBaseline
		measured[rowIdx].cells = cellLayouts
		for _, cl := range cellLayouts {
			if cl.cell.rowSpan > 1 {
				measured[rowIdx].hasRowspanStart = true
				break
			}
		}
	}

	// --- Phase 2: compute final row block-sizes ---
	// Build TableTypesRows from measured intrinsics, then apply the
	// 5-priority distribution when the table has an explicit block-size
	// larger than the intrinsic content. Mirrors Blink's ComputeRows +
	// DistributeTableBlockSizeToSections pair.
	intrinsics := make([]tableRowIntrinsic, len(rows))
	flat := 0
	for i := range rows {
		intrinsics[i] = tableRowIntrinsic{
			intrinsicHeight: measured[i].intrinsic,
			hasExplicitCell: measured[i].hasExplicit,
			cellCount:       len(measured[i].cells),
			startCellIndex:  flat,
			hasRowspanStart: measured[i].hasRowspanStart,
			baseline:        measured[i].baseline,
			row:             &rows[i],
		}
		flat += len(measured[i].cells)
	}
	spRows := computeRows(intrinsics, wdm)

	// Distribute rowspan-originator cells' minimum block-sizes across
	// the rows they span. Mirrors Blink's DistributeRowspanCellToRows
	// (table_layout_utils.cc ~L1606): runs after computeRows and before
	// the table-block-size excess distribution so that, by the time
	// excess distribution sees the row vector, each row already
	// reflects the contribution of every rowspan cell it participates
	// in. This is what makes a rowspan cell "pull up" the rows below
	// its originating row even when those rows would otherwise have
	// smaller intrinsics.
	for ri := range measured {
		for _, cl := range measured[ri].cells {
			if cl.cell.rowSpan > 1 {
				distributeRowspanCellToRows(spRows, cl.rowIndex, cl.cell.rowSpan,
					cl.intrinsicBlockSize, blockSpacing)
			}
		}
	}

	// CSS 2.1 §17.5.3: if the table has an explicit block-size larger
	// than its intrinsic content, distribute the excess across the rows.
	// `explicitTableBorderBoxBlock` is geom.BorderBoxSize.BlockSize floored
	// by min-block-size and clamped by max-block-size — see the comment at
	// the top of Layout(). Reading from it (not directly from geom) ensures
	// min-height triggers the distribution even when CSS height is auto.
	if explicitTableBorderBoxBlock != Indefinite && len(rows) > 0 {
		explicitContentBlock := explicitTableBorderBoxBlock - geom.BlockBorderPadding()
		if explicitContentBlock < 0 {
			explicitContentBlock = 0
		}

		// Intrinsic rows-block total (spacings + post-rowspan-distribution
		// row block-sizes). Reads from spRows[i].BlockSize so the rowspan
		// pre-pass's contributions are included in the "what's already
		// claimed" count. Collapsed rows are not emitted (§3.5 / §5.4.1)
		// so they must be excluded from both the row block-sum and the
		// inter-row spacing contribution — otherwise the table thinks it
		// is already using the space that belongs to a non-displayed row.
		intrinsicTotal := blockOffset
		emittedCount := 0
		for i := range rows {
			if spRows[i].IsCollapsed {
				continue
			}
			if emittedCount > 0 && blockSpacing > 0 {
				intrinsicTotal += blockSpacing
			}
			intrinsicTotal += spRows[i].BlockSize
			emittedCount++
		}
		if emittedCount > 0 && blockSpacing > 0 {
			intrinsicTotal += blockSpacing // block-end border spacing
		}

		if explicitContentBlock > intrinsicTotal {
			excess := explicitContentBlock - intrinsicTotal
			// Build the rowspan-cell vector for priority-2 preferential
			// distribution: each entry is (originating row, rowSpan,
			// minimum block-size). Mirrors the list Blink carries inside
			// TableTypes across ComputeRows →
			// DistributeTableBlockSizeToSections.
			var rowspanCells []rowspanCellInfo
			for ri := range measured {
				for _, cl := range measured[ri].cells {
					if cl.cell.rowSpan > 1 {
						rowspanCells = append(rowspanCells, rowspanCellInfo{
							rowIndex: cl.rowIndex,
							rowSpan:  cl.cell.rowSpan,
							minBlock: cl.intrinsicBlockSize,
						})
					}
				}
			}
			distributeTableBlockSizeToRows(spRows, excess, rowspanCells, blockSpacing)
		}
	}

	// Publish finalized sizes into the shared constraint-space data.
	// Cells laid out in Phase 1 hold a pointer to tableData; updating in
	// place here is the single-pass analogue of Blink's
	// CreateConstraintSpaceData-after-distribution pattern. Cell layout
	// today does not read from it, but future rowspan / percentage code
	// will.
	copy(tableData.Rows, spRows)
	if len(tableData.Sections) > 0 {
		sectionBlock := 0.0
		for i := range spRows {
			sectionBlock += spRows[i].BlockSize
		}
		tableData.Sections[0].BlockSize = sectionBlock
	}

	// --- Phase 3: assemble row fragments at final sizes ---
	//
	// Rowspan cells are deferred and emitted as direct children of the
	// table builder *after* every row fragment has been added. Mirrors
	// Blink's painting-order rule for rowspan cells: row backgrounds
	// (drawn from row fragments) sit beneath cell content, and a
	// rowspan cell visually overlaps the rows it spans. Adding rowspan
	// cells last means their content paints on top of the row
	// backgrounds for rows R+1..R+s-1.
	type pendingRowspan struct {
		cl          cellMeasurement
		blockOffset float64 // originating row's final block-offset
	}
	var rowspanPending []pendingRowspan

	// Total inline-size of a row fragment — sum of column widths plus
	// inline-axis border-spacing. Identical across rows; lifted out of
	// the row loop so the section-fragment path can reuse it.
	totalInlineForRows := 0.0
	for _, w := range colWidths {
		totalInlineForRows += w
	}
	totalInlineForRows += spacingForCols

	// Section fragment state. When a row group (thead/tbody/tfoot) has
	// position:relative or position:sticky its rows are wrapped in a
	// section PhysicalBoxFragment so the group's relative offset can
	// shift its rows together. Non-positioned groups still
	// flatten into the table builder (unchanged behaviour).
	//
	// Mirrors Blink's NGTableSectionLayoutAlgorithm output — sections
	// are structural fragments carrying their group's style.
	var sectionBuilder *BoxFragmentBuilder
	var sectionStartBlock float64
	var lastGroupNode *LayoutInputNode
	flushSection := func(endBlock float64) {
		if sectionBuilder == nil {
			return
		}
		sectionBlockSize := endBlock - sectionStartBlock
		if sectionBlockSize < 0 {
			sectionBlockSize = 0
		}
		sectionBuilder.SetSize(LogicalSize{
			InlineSize: totalInlineForRows,
			BlockSize:  sectionBlockSize,
		})

		// The section is an absolute container (sectionBuilder is created
		// for position:relative/sticky groups AND for transform/filter/etc
		// groups that are fixed containers, see the row loop below): its
		// OutOfFlowLayoutPart resolves the candidates it contains against
		// its padding box and the rest propagate to the table. Mirrors
		// Blink's OutOfFlowLayoutPart::Run at the end of
		// TableSectionLayoutAlgorithm (BoxFragmentBuilder::
		// HandleOofsAndSpecialDescendants, box_fragment_builder.cc:660-671 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		if len(sectionBuilder.outOfFlowCandidates) > 0 {
			secGeom := FragmentGeometry{}
			if lastGroupNode != nil && lastGroupNode.Style() != nil {
				secGeom = ComputeFragmentGeometry(lastGroupNode.Style(), wdm)
			}
			propagated := NewOutOfFlowLayoutPart(tla.ctx, sectionBuilder, tableInternalOOFSpace(wdm),
				LogicalSize{InlineSize: totalInlineForRows, BlockSize: sectionBlockSize}, secGeom.Padding, 0).Run()
			// Bubble the rest to the table. Section block-origin within
			// table content-box is sectionStartBlock; inline-origin is 0.
			for _, c := range propagated {
				c.StaticPosition.Offset.BlockOffset += sectionStartBlock
				builder.AddOutOfFlowCandidate(c)
			}
		}

		sectionResult := sectionBuilder.Build()
		builder.AddChild(sectionResult.Fragment, LogicalOffset{
			InlineOffset: 0,
			BlockOffset:  sectionStartBlock,
		})
		sectionBuilder = nil
	}

	firstRowHeight := 0.0
	firstRowBaseline := 0.0   // max cell baseline in first VISIBLE row
	lastRowBaseline := 0.0    // max cell baseline in last VISIBLE row
	lastRowBlockOffset := 0.0 // block offset of last VISIBLE row
	emittedRows := 0          // count of visible (non-collapsed) rows emitted
	// rowStarts[i] is row i's block offset in the content box (a collapsed
	// row starts, and ends, where the next row starts), rowStarts[len(rows)]
	// the end of the last row: the collapsed-borders row geometry.
	rowStarts := make([]float64, len(rows)+1)
	for rowIdx, row := range rows {
		rowStarts[rowIdx] = blockOffset
		// CSS Tables 3 §3.5 (visibility:collapse): a collapsed row is
		// removed from layout. Skip it entirely — no row fragment,
		// no inter-row spacing, no blockOffset advance, no cells
		// emitted. Cells originating here (rowSpan==1 or rowSpan>1
		// originators) are not displayed, propagating the row's
		// collapse to its cells. Mirrors Blink's
		// TableSectionLayoutAlgorithm row loop which skips collapsed
		// rows for fragment construction.
		if spRows[rowIdx].IsCollapsed {
			continue
		}

		// Group boundary: crossing from one row group into another
		// (or between a grouped and ungrouped row) closes any open
		// positioned section at the bottom of the previous row. The
		// section's block-extent ends at the current blockOffset,
		// before inter-row spacing is added — spacing sits BETWEEN
		// sections, not inside them.
		isGroupBoundary := row.groupNode != lastGroupNode
		if isGroupBoundary && sectionBuilder != nil {
			flushSection(blockOffset)
		}

		// Add inter-row spacing only between VISIBLE rows: the
		// collapsed row's spacing is removed along with the row.
		if emittedRows > 0 && blockSpacing > 0 {
			blockOffset += blockSpacing
		}

		// Entering a row group that is an absolute container (relative or
		// sticky position, or a fixed container: transform, filter,
		// will-change...): open a section BoxFragmentBuilder to hold the
		// group's rows. The section's relative offset is applied by the
		// main table builder's AddChild when flushSection emits the section
		// fragment. Other groups continue to flatten into the table builder
		// (legacy behaviour). A fixed-container group's fixed descendants
		// resolve against the section and are wrapped in the section's
		// transform at paint (LayoutObject::ComputeIsFixedContainer,
		// layout_object.cc:1849-1900 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		if isGroupBoundary && row.groupNode != nil {
			if row.groupNode.IsAbsoluteContainer() {
				sectionBuilder = NewBoxFragmentBuilder(wdm)
				sectionBuilder.SetLayoutNode(row.groupNode)
				sectionStartBlock = blockOffset
				// Nested position:relative/sticky children inside the
				// section resolve percentages against the section's
				// content area. Match the table-level convention of
				// Indefinite block-size (percentages → 0).
				sectionBuilder.SetChildAvailableSize(LogicalSize{
					InlineSize: totalInlineForRows,
					BlockSize:  Indefinite,
				})
			}
		}
		lastGroupNode = row.groupNode

		rowHeight := spRows[rowIdx].BlockSize

		// Create synthetic row fragment (structural only, no OOF role).
		// row.node is always set after CSS 2.1 §17.2.1 anonymous-box
		// normalization at tree-build time — anonymous and authored rows
		// are indistinguishable here.
		rowBuilder := NewBoxFragmentBuilder(wdm)
		rowBuilder.SetLayoutNode(row.node)

		for _, cl := range measured[rowIdx].cells {
			if cl.cell.rowSpan > 1 {
				// Defer rowspan cells: they are NOT children of any
				// single row fragment; they attach directly to the
				// table builder at the originating row's block-offset
				// after the row loop, laid out again at the total
				// spanned block-size.
				rowspanPending = append(rowspanPending, pendingRowspan{
					cl:          cl,
					blockOffset: blockOffset,
				})
				continue
			}

			// Lay the cell out again at its final block size, which also
			// applies its vertical-align (TableRowLayoutAlgorithm::Layout →
			// PlaceCells, table/table_row_layout_algorithm.cc:37-200 @
			// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
			cellBlockSize, initialIndefinite := computeCellBlockSize(spRows, rowIdx, 1, blockSpacing,
				cl.intrinsicBlockSize, cellHasSpecifiedBlockSize(cl.cell.style, wdm), isTableBlockSizeSpecified)
			result := tla.layoutCellAtFinalSize(cl.cell.node, cl.cellWDM, cl.layoutInlineSize, cl.result, cellBlockSize, initialIndefinite, tableData)
			cellFrag := result.Fragment
			cellFrag.HasCollapsedBorders = borderCollapse

			cellOffset := LogicalOffset{InlineOffset: cl.inlineOffset}
			cellRelativeOffset := relativeCellOffset(cl.cell, row.style, wdm, totalInlineForRows)
			rowBuilder.AddChildWithRelativeOffset(cellFrag, cellOffset, cellRelativeOffset)
			// The cell's OOF candidates, in row-content-box coordinates.
			if cl.cell.style != nil {
				rowBuilder.PropagateOOFPositionedInfo(&LayoutResult{Fragment: cellFrag, PropagatedOOFCandidates: result.PropagatedOOFCandidates},
					cl.cell.style, cellOffset, cellRelativeOffset)
			}
		}

		// Set row size and build synthetic fragment.
		totalInline := 0.0
		for _, w := range colWidths {
			totalInline += w
		}
		totalInline += spacingForCols // include inline spacing in row width
		rowBuilder.SetSize(LogicalSize{
			InlineSize: totalInline,
			BlockSize:  rowHeight,
		})

		// Copy row style for background/border rendering. Anonymous row
		// wrappers have zero border/padding per CSS 2.1 §17.2.1, so this
		// block safely degrades to a no-op for them.
		//
		// CSS 2.1 §17.2 + CSS Tables 3 §11.3: In BOTH border models, rows
		// have no border (in separate mode borders are not defined on rows;
		// in collapse mode rows participate in the collapsed model rather
		// than painting their own). Per CSS Tables 3 row padding also has
		// no effect (cell padding is the relevant property). Mirrors
		// Blink's TableRowLayoutAlgorithm which zeroes row geometry in
		// both border models.
		if row.style != nil {
			rowBuilder.SetBoxData(&PhysicalBoxData{
				Border:    PhysicalEdges{},
				Padding:   PhysicalEdges{},
				Scrollbar: PhysicalEdges{},
			})
		}

		// Route cell OOFs to their containing block. A row that is an
		// absolute container (CSS 2.1 §10.1, §9.4.3; a fixed container
		// contains fixed descendants too) runs its OutOfFlowLayoutPart;
		// the rest bubble to a positioned section or to the table. Mirrors
		// Blink's OutOfFlowLayoutPart::Run at the end of
		// TableRowLayoutAlgorithm (box_fragment_builder.cc:660-671 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		// The cells' candidates are on rowBuilder, in row-content-box
		// coordinates.
		var bubbleOOFs []OutOfFlowCandidate
		if len(rowBuilder.outOfFlowCandidates) > 0 && row.node.IsAbsoluteContainer() {
			// Row CB: padding-box. Row's geometry has been applied to
			// rowBuilder via SetBoxData above; CB inline/block size
			// includes row padding.
			bubbleOOFs = NewOutOfFlowLayoutPart(tla.ctx, rowBuilder, tableInternalOOFSpace(wdm),
				LogicalSize{InlineSize: totalInline, BlockSize: rowHeight}, ComputeFragmentGeometry(row.style, wdm).Padding, 0).Run()
		} else {
			bubbleOOFs, rowBuilder.outOfFlowCandidates = rowBuilder.outOfFlowCandidates, nil
		}
		// Bubble remaining OOFs to enclosing CB (section if open, else
		// table). Translate row-content-box coords to the enclosing
		// content-box coords: row inline origin == enclosing inline origin,
		// so only the block axis shifts.
		for _, c := range bubbleOOFs {
			if sectionBuilder != nil {
				c.StaticPosition.Offset.BlockOffset += blockOffset - sectionStartBlock
				sectionBuilder.AddOutOfFlowCandidate(c)
			} else {
				c.StaticPosition.Offset.BlockOffset += blockOffset
				builder.AddOutOfFlowCandidate(c)
			}
		}

		rowResult := rowBuilder.Build()

		// CSS Tables + chromium bug 1227884: percent insets on a
		// position:relative table-row resolve against the row GROUP's
		// SPECIFIED block-size (tbody/thead/tfoot height), not the
		// used/distributed size. Resolve the row's relative offset here
		// instead of with the AddChild default (which would use the
		// table's available block-size). Mirrors Blink's
		// table_section_layout path for position:relative rows.
		rowRelativeOffset := ComputeRelativeOffset(row.style, wdm, LogicalSize{
			InlineSize: totalInlineForRows,
			BlockSize:  specifiedBlockSize(row.groupStyle, wdm),
		})

		if sectionBuilder != nil {
			// Row is inside a positioned row group: attach to the
			// section with offset relative to the section's origin.
			// The section fragment itself is added to the main table
			// builder by flushSection, carrying its own block-offset.
			sectionBuilder.AddChildWithRelativeOffset(rowResult.Fragment, LogicalOffset{
				InlineOffset: 0,
				BlockOffset:  blockOffset - sectionStartBlock,
			}, rowRelativeOffset)
		} else {
			builder.AddChildWithRelativeOffset(rowResult.Fragment, LogicalOffset{
				InlineOffset: 0,
				BlockOffset:  blockOffset,
			}, rowRelativeOffset)
		}

		rowBaseline := measured[rowIdx].baseline
		if emittedRows == 0 {
			firstRowHeight = rowHeight
			firstRowBaseline = rowBaseline
		}
		lastRowBaseline = rowBaseline
		lastRowBlockOffset = blockOffset
		blockOffset += rowHeight
		emittedRows++
	}

	// Close any still-open positioned section at the bottom of the
	// last emitted row.
	flushSection(blockOffset)
	rowStarts[len(rows)] = blockOffset

	// --- Phase 3b: place rowspan cells across spanned rows ---
	//
	// Each deferred rowspan cell is laid out again at the block size of
	// the rows it spans (computeCellBlockSize) and attaches at the
	// originating row's block-offset, just like a rowSpan == 1 cell
	// would.
	for _, p := range rowspanPending {
		cl := p.cl
		rowIdx := cl.rowIndex
		end := rowIdx + cl.cell.rowSpan
		if end > len(spRows) {
			end = len(spRows)
		}
		// CSS Tables 3 §5.4.1: "If the table-cell is spanning more than
		// one table-track, and at least one of those table-track is set
		// to visibility: collapse then clip the content to the
		// table-cell's border-box." The cell's border-box block-extent
		// is the sum of the NON-collapsed spanned rows, plus inter-row
		// spacing between visible pairs. Mirrors Blink's
		// ComputeCellBlockSize (table_layout_utils.cc): collapsed rows
		// are skipped; the cell is laid out at that reduced extent, and
		// any child content that overflows is clipped at paint time via
		// ClipContentToBorderBox.
		spansCollapsedRow := false
		for i := rowIdx; i < end; i++ {
			spansCollapsedRow = spansCollapsedRow || spRows[i].IsCollapsed
		}

		// The cell is laid out again at the spanned extent; when that is
		// smaller than its content (a collapsed spanned row), the content
		// stays at the block-start edge (§5.4.1 "the top left content of
		// the cell will continue to show") and is clipped to the border box.
		spannedBlock, initialIndefinite := computeCellBlockSize(spRows, rowIdx, cl.cell.rowSpan, blockSpacing,
			cl.intrinsicBlockSize, cellHasSpecifiedBlockSize(cl.cell.style, wdm), isTableBlockSizeSpecified)
		result := tla.layoutCellAtFinalSize(cl.cell.node, cl.cellWDM, cl.layoutInlineSize, cl.result, spannedBlock, initialIndefinite, tableData)
		cellFrag := result.Fragment
		cellFrag.HasCollapsedBorders = borderCollapse
		if spansCollapsedRow {
			cellFrag.ClipContentToBorderBox = true
		}

		// Attach directly to the table builder at the originating
		// row's block-offset. Its relative offset still resolves against
		// the originating row, the row Blink adds it through, not the
		// table's child available size.
		cellOffset := LogicalOffset{InlineOffset: cl.inlineOffset, BlockOffset: p.blockOffset}
		cellRelativeOffset := relativeCellOffset(cl.cell, rows[rowIdx].style, wdm, totalInlineForRows)
		builder.AddChildWithRelativeOffset(cellFrag, cellOffset, cellRelativeOffset)
		if cl.cell.style != nil {
			builder.PropagateOOFPositionedInfo(&LayoutResult{Fragment: cellFrag, PropagatedOOFCandidates: result.PropagatedOOFCandidates},
				cl.cell.style, cellOffset, cellRelativeOffset)
		}
	}

	// Add block-end spacing after last row.
	if len(rows) > 0 && blockSpacing > 0 {
		blockOffset += blockSpacing
	}

	// Lay out and add the bottom (block-end) captions. Mirrors Blink
	// GenerateFragment's bottom-caption loop (table_layout_algorithm.cc:1585-1589
	// @ d4ecdfed88f9).
	for _, cap := range bottomCaptions {
		tla.addCaptionResult(builder, wdm,
			tla.layoutCaption(cap, wdm, tableContentInline), &blockOffset)
	}

	// Negative caption margins can pull blockOffset below 0; Blink clamps
	// (child_block_offset.ClampNegativeToZero(), table_layout_algorithm.cc:1592
	// @ d4ecdfed88f9).
	finalBlockSize := max(0, blockOffset)

	// CSS 2.1 §17.5.3 + CSS Tables 3 §11.5.3 "Computing the table height":
	// honor an explicit table block-size as a floor, even when the table has
	// no rows (or fewer rows than the specified height requires).
	// `explicitTableBorderBoxBlock` is min/max-height-resolved — see the top
	// of Layout().
	//
	// CSS Tables 3 §11.5.3 specifies that the used table block-size is the
	// MAX of intrinsic content and the explicit/min/max-resolved value —
	// i.e. max-height cannot shrink the table below its intrinsic content
	// (`max-height: 0` on a table with rows still renders content). This
	// differs from the standard CSS Sizing model and matches Blink/Gecko
	// behavior (css-tables/max-height-table.html).
	borderBoxBlock := finalBlockSize + geom.BlockBorderPadding()
	if explicitTableBorderBoxBlock != Indefinite &&
		explicitTableBorderBoxBlock > borderBoxBlock {
		borderBoxBlock = explicitTableBorderBoxBlock
	}

	builder.SetSize(LogicalSize{
		InlineSize: borderBoxInline,
		BlockSize:  borderBoxBlock,
	})

	// CSS 2.1 §17.5.2: "The baseline of an inline-table is the baseline of
	// the first row." Blink computes this via RowBaselineTabulator: the row
	// baseline is the max first-baseline of all baseline-aligned cells.
	// We propagate the first row's baseline as the table's first baseline,
	// and the last row's baseline as the table's last baseline.
	if len(rows) > 0 {
		borderPaddingStart := geom.Border.BlockStart + geom.Padding.BlockStart
		if firstRowBaseline > 0 {
			// First baseline: distance from table border-box block-start
			// to the first row's text baseline.
			builder.SetBaseline(borderPaddingStart + firstRowBaseline)
		} else if firstRowHeight > 0 {
			// Fallback: center of first row (no cells had a text baseline).
			builder.SetBaseline(borderPaddingStart + firstRowHeight/2)
		}
		if lastRowBaseline > 0 {
			builder.SetLastBaseline(borderPaddingStart + lastRowBlockOffset + lastRowBaseline)
		}
		// When no row has a text-alignment baseline, leave LastBaseline
		// unset. Blink's LayoutBox::LastBaselineForInlineBlock returns
		// nullopt in this case; the fallback to the bottom margin edge
		// (CSS 2.1 §10.8.1) happens at the enclosing inline-block's
		// atomic-inline placement, not here. Synthesizing a content-box
		// baseline here would propagate through block wrappers and
		// incorrectly shift the inline-block's baseline.
	}

	physBorder := ToPhysicalEdges(geom.Border, wdm) // already zeroed for border-collapse
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physScrollbar := ToPhysicalEdges(geom.Scrollbar, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(tla.style, wdm, tla.space.AvailableSize.InlineSize.Float64()), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:    physMargin,
		Border:    physBorder,
		Padding:   physPadding,
		Scrollbar: physScrollbar,
	})

	// Mirrors Blink's single OutOfFlowLayoutPart::Run() at the end of
	// TableLayoutAlgorithm::Layout (BoxFragmentBuilder::
	// HandleOofsAndSpecialDescendants, box_fragment_builder.cc:660-671 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), against the padding box.
	var propagatedOOF []OutOfFlowCandidate
	if builder.NeedsOutOfFlowLayoutPart() {
		propagatedOOF = NewOutOfFlowLayoutPart(tla.ctx, builder, tla.space, LogicalSize{
			InlineSize: contentInlineSize + geom.Padding.InlineSum(),
			BlockSize:  finalBlockSize + geom.Padding.BlockSum(),
		}, geom.Padding,
			previousConsumedContentBlockSize(tla.space, geom.BorderBoxPadding().BlockStart)).Run()
	}

	result := builder.Build()
	result.Fragment.HasCollapsedBorders = borderCollapse
	if tableBorders != nil && tableBorders.EdgeCount() > 0 {
		result.Fragment.TableCollapsedBorders = tableBorders
		result.Fragment.TableCollapsedBordersGeometry = collapsedTableBordersGeometry(
			tableBorders, wdm, geom, colWidths, rowStarts)
	}

	if len(propagatedOOF) > 0 {
		result.PropagatedOOFCandidates = propagatedOOF
	}

	return result
}

// resolveCollapsedCellBorders resolves a collapsed table's borders and
// gives each cell its share, as Blink computes TableBorders up front
// (TableBorders::ComputeTableBorders, table/table_borders.cc:115-248 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func resolveCollapsedCellBorders(rows []tableRow, colStyleSpecs []tableColStyle, tableStyle *css.Style, wdm WritingDirectionMode, numCols int) *TableBorders {
	restoreCollapsedCellSourceStyles(rows)
	tableBorders := ComputeTableBorders(rows, colStyleSpecs, tableStyle, wdm, numCols)
	applyCollapsedCellBorders(rows, tableBorders, numCols, wdm)
	return tableBorders
}

// ComputeMinMaxSizes returns the table's content-box min/max-content
// inline-sizes, for an auto-layout table: the column constraints summed by
// ComputeGridInlineMinMax with the border spacing as undistributable space,
// encompassing the captions' min-content contributions. Mirrors Blink's
// TableLayoutAlgorithm::ComputeMinMaxSizes (table/table_layout_algorithm.cc:
// 701-732), ComputeCaptionConstraint (:38-58) and
// ComputeUndistributableTableSpace (:173-189), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. Blink's sizes include the
// table's border and padding; louis14's callers add those. louis14 has no
// mergeable columns, so the spacing counts every column, as Layout does.
func (tla *TableLayoutAlgorithm) ComputeMinMaxSizes() MinMaxSizes {
	wdm := tla.space.WritingDirection
	rows, captions, colWidthSpecs, colStyleSpecs := tla.collectRowsAndCaptions()
	numCols := assignColumnIndices(rows)
	borderCollapse := tla.style.GetBorderCollapse() == css.BorderCollapseCollapse
	if borderCollapse {
		resolveCollapsedCellBorders(rows, colStyleSpecs, tla.style, wdm, numCols)
	}
	inlineSpacing, undistributableSpace := 0.0, 0.0
	if !borderCollapse {
		inlineSpacing, _ = tla.logicalBorderSpacing()
		if numCols > 0 {
			undistributableSpace = inlineSpacing * float64(numCols+1)
		}
	}
	columns := tla.computeColumnConstraints(rows, numCols, Indefinite, inlineSpacing, colWidthSpecs)
	minMax := ComputeGridInlineMinMax(tla.node, columns, undistributableSpace, false, false)
	if borderCollapse {
		// The edge cells hold the collapsed outer borders, as Layout's
		// zeroed table border reflects (Blink splits them between the
		// table's TableBorders and the cells); the caller adds the
		// table's own border, so take it out here.
		tableBorder := ComputeFragmentGeometry(tla.style, wdm).Border
		minMax.MinContent = max(0, minMax.MinContent-tableBorder.InlineStart-tableBorder.InlineEnd)
		minMax.MaxContent = max(0, minMax.MaxContent-tableBorder.InlineStart-tableBorder.InlineEnd)
	}

	// The captions' min-content contributions, against the table's content
	// box.
	tableBP := ComputeFragmentGeometry(tla.style, wdm).InlineBorderPadding()
	minMax.Encompass(tla.computeCaptionConstraint(captions).MinContent - tableBP)
	return minMax
}

// computeCaptionConstraint returns the captions' min/max-content
// contributions, border-box with their inline margins, the largest of
// each. Mirrors Blink's ComputeCaptionConstraint (table/
// table_layout_algorithm.cc:38-58 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (tla *TableLayoutAlgorithm) computeCaptionConstraint(captions []tableCaption) MinMaxSizes {
	wdm := tla.space.WritingDirection
	var constraint MinMaxSizes
	for _, caption := range captions {
		captionWDM := NewWritingDirectionMode(caption.style)
		space := NewConstraintSpaceBuilder(wdm, captionWDM, true).
			SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(captionWDM, tla.ctx)).
			SetAvailableSize(LogicalSize{InlineSize: Indefinite, BlockSize: Indefinite}).
			Build()
		sizes := ComputeMinMaxSizes(tla.ctx, caption.node, space)
		extra := ComputeFragmentGeometry(caption.style, captionWDM).InlineBorderPadding() +
			ResolveMargins(caption.style, wdm, 0).InlineSum()
		constraint.MinContent = max(constraint.MinContent, sizes.MinContent+extra)
		constraint.MaxContent = max(constraint.MaxContent, sizes.MaxContent+extra)
	}
	return constraint
}

// collectRowsAndCaptions extracts table rows, captions, and col/colgroup width specs
// from the table's children, handling row-groups (thead, tbody, tfoot).
// Rendering order is the header, the bodies in source order, then the
// footer. The header is the first table-header-group and the footer the
// first table-footer-group; later ones are bodies (TableGroupedChildren,
// table/table_layout_algorithm_types.cc:296-330 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Also returns column width
// specifications and border styles from col/colgroup elements.
func (tla *TableLayoutAlgorithm) collectRowsAndCaptions() ([]tableRow, []tableCaption, []tableColWidth, []tableColStyle) {
	var headerRows, bodyRows, footerRows []tableRow
	hasHeader, hasFooter := false, false // an empty group still takes the slot
	var captions []tableCaption
	var colWidths []tableColWidth
	var colStyleSpecs []tableColStyle
	colIdx := 0 // tracks current column index as col/colgroup are encountered

	// Per CSS 2.1 §17.2.1 the layout tree is already normalized at
	// tree-build time (see LayoutTreeBuilder.wrapAnonymousTableBoxes).
	// Every direct child of a table is a caption, a row-group, or a
	// col/colgroup — stray text/inlines/bare rows/bare cells/bare blocks
	// have all been wrapped in anonymous row-groups upstream. Matches
	// Blink: by the time the table layout algorithm runs, anonymous and
	// authored row-groups are indistinguishable.
	for _, child := range tla.node.Children() {
		if child.IsText() {
			// Defensive: whitespace text is dropped at tree-build.
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		display := childStyle.GetDisplay()

		// col/colgroup — affect column widths per CSS Tables §9.1.
		// These have display:block in louis14 (no dedicated display value),
		// so detect by tag name.
		if child.DOMNode != nil && (child.DOMNode.TagName == "col" || child.DOMNode.TagName == "colgroup") {
			span := 1
			if spanAttr, ok := child.DOMNode.GetAttribute("span"); ok {
				if n := parseIntAttr(spanAttr); n > 0 {
					span = n
				}
			}
			if spec, ok := extractColWidthSpec(childStyle, colIdx, span); ok {
				colWidths = append(colWidths, spec)
			}
			// Collect style for border-collapse resolution (CSS 2.1 §17.6.2.1).
			groupSpecIdx := len(colStyleSpecs)
			colStyleSpecs = append(colStyleSpecs, tableColStyle{
				colIndex: colIdx,
				span:     span,
				style:    childStyle,
				isGroup:  child.DOMNode.TagName == "colgroup",
			})
			if child.DOMNode.TagName == "colgroup" {
				// Check child col elements for per-column widths and styles.
				colChildIdx := colIdx
				for _, colChild := range child.Children() {
					if colChild.IsText() || colChild.DOMNode == nil || colChild.DOMNode.TagName != "col" {
						continue
					}
					colChildStyle := colChild.Style()
					if colChildStyle == nil {
						colChildIdx++
						continue
					}
					colChildSpan := 1
					if spanAttr, ok := colChild.DOMNode.GetAttribute("span"); ok {
						if n := parseIntAttr(spanAttr); n > 0 {
							colChildSpan = n
						}
					}
					if spec, ok := extractColWidthSpec(colChildStyle, colChildIdx, colChildSpan); ok {
						colWidths = append(colWidths, spec)
					}
					colStyleSpecs = append(colStyleSpecs, tableColStyle{
						colIndex: colChildIdx,
						span:     colChildSpan,
						style:    colChildStyle,
						isGroup:  false,
					})
					colChildIdx += colChildSpan
				}
				if colChildIdx > colIdx {
					// A colgroup with col children spans exactly them
					// (VisitLayoutTableColumn, layout_table_column_visitor.h
					// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
					colStyleSpecs[groupSpecIdx].span = colChildIdx - colIdx
					colIdx = colChildIdx
					continue
				}
			}
			colIdx += span
			continue
		}

		switch display {
		case css.DisplayTableCaption:
			// caption-side is inherited but may not be in the cascade's
			// inherited list. Check the caption's own style first, then
			// fall back to the table's style.
			side := "top"
			if childStyle.GetCaptionSide() == "bottom" {
				side = "bottom"
			} else if tla.style.GetCaptionSide() == "bottom" {
				side = "bottom"
			}
			captions = append(captions, tableCaption{
				node:  child,
				style: childStyle,
				side:  side,
			})

		case css.DisplayTableHeaderGroup, css.DisplayTableRowGroup, css.DisplayTableFooterGroup:
			// Collect rows from this group, then append to the correct bucket.
			var groupRows []tableRow
			for _, grandchild := range child.Children() {
				if grandchild.IsText() {
					continue
				}
				gcStyle := grandchild.Style()
				if gcStyle == nil {
					continue
				}
				if gcStyle.GetDisplay() == css.DisplayTableRow {
					r := tla.buildRow(grandchild, gcStyle)
					r.groupStyle = childStyle // tag with row group's style
					r.groupNode = child       // tag with row group's LayoutInputNode (for section fragments)
					groupRows = append(groupRows, r)
				}
			}
			switch {
			case display == css.DisplayTableHeaderGroup && !hasHeader:
				headerRows, hasHeader = groupRows, true
			case display == css.DisplayTableFooterGroup && !hasFooter:
				footerRows, hasFooter = groupRows, true
			default:
				bodyRows = append(bodyRows, groupRows...)
			}

		case css.DisplayTableCell:
			// Bare cell without a row — wrap in an anonymous row
			// (CSS 2.1 §17.2.1).
			anonRowStyle := css.CreateAnonymousStyleWithDisplay(tla.style, string(css.DisplayTableRow))
			anonRowNode := &LayoutInputNode{
				style:       anonRowStyle,
				isAnonymous: true,
			}
			bodyRows = append(bodyRows, tableRow{
				node:  anonRowNode,
				style: anonRowStyle,
				cells: []tableCell{{
					node:    child,
					style:   childStyle,
					colSpan: 1,
					rowSpan: 1,
				}},
			})

		default:
			// Non-table-structural children (display:block, display:inline, etc.)
			// must be wrapped in an anonymous table-row + anonymous table-cell per
			// CSS 2.1 §17.2.1. Each such child becomes the sole cell of an
			// anonymous row. The cell's style derives from the row's, as Blink's
			// LayoutTableCell::CreateAnonymousWithParent(row)
			// (core/layout/table/layout_table_cell.cc:27-35 @
			// 5992657f556bcd9d207a4aca187488462f027cab).
			anonRowStyle := css.CreateAnonymousStyleWithDisplay(tla.style, string(css.DisplayTableRow))
			anonCellStyle := css.CreateAnonymousStyleWithDisplay(anonRowStyle, string(css.DisplayTableCell))
			anonCell := &LayoutInputNode{
				style:       anonCellStyle,
				children:    []*LayoutInputNode{child},
				isAnonymous: true,
			}
			anonRowNode := &LayoutInputNode{
				style:       anonRowStyle,
				children:    []*LayoutInputNode{anonCell},
				isAnonymous: true,
			}
			bodyRows = append(bodyRows, tableRow{
				node:  anonRowNode,
				style: anonRowStyle,
				cells: []tableCell{{
					node:    anonCell,
					style:   anonCellStyle,
					colSpan: 1,
					rowSpan: 1,
				}},
			})
		}
	}

	// Render order: header → bodies → footer.
	rows := make([]tableRow, 0, len(headerRows)+len(bodyRows)+len(footerRows))
	rows = append(rows, headerRows...)
	rows = append(rows, bodyRows...)
	rows = append(rows, footerRows...)
	return rows, captions, colWidths, colStyleSpecs
}

// buildRow extracts cells from a table-row element. After tree-build
// normalization (LayoutTreeBuilder.wrapAnonymousTableBoxes), every row
// child is a table-cell — either authored or an anonymous wrapper around
// stray content. An anonymous cell is an ordinary cell: its content is
// laid out inside it, as Blink's LayoutTableRow::AddChild wraps every
// non-cell child (table/layout_table_row.cc:87-115 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (tla *TableLayoutAlgorithm) buildRow(node *LayoutInputNode, style *css.Style) tableRow {
	row := tableRow{node: node, style: style}
	colIdx := 0

	for _, child := range node.Children() {
		if child.IsText() {
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		if childStyle.GetDisplay() != css.DisplayTableCell {
			// Defensive — tree-build should have wrapped this.
			continue
		}

		colSpan := 1
		rowSpan := 1
		if child.DOMNode != nil {
			if cs, ok := child.DOMNode.GetAttribute("colspan"); ok {
				if v := parseIntAttr(cs); v > 0 {
					colSpan = v
				}
			}
			// WHATWG HTML §4.9.11: rowspan is a non-negative integer.
			// Per spec, rowspan="0" means "span all remaining rows in
			// the row group." Blink implements this in
			// HTMLTableCellElement::rowSpan by returning kMaxRowIndex
			// (65534) and letting the table section layout clamp down.
			// We do the same: encode 0 as 65534 and let the slot grid
			// in assignColumnIndices clamp rowSpan so it cannot extend
			// past the last row. Non-zero values above 65534 are also
			// clamped (WHATWG HTML parser's limit).
			if rs, ok := child.DOMNode.GetAttribute("rowspan"); ok {
				if v, ok := parseIntAttrWithZero(rs); ok {
					if v == 0 || v > 65534 {
						v = 65534
					}
					rowSpan = v
				}
			}
		}
		row.cells = append(row.cells, tableCell{
			node:     child,
			style:    childStyle,
			colIndex: colIdx, // provisional; assignColumnIndices overwrites
			colSpan:  colSpan,
			rowSpan:  rowSpan,
		})
		colIdx += colSpan
	}

	return row
}

// assignColumnIndices walks `rows` in order and fills in each cell's
// final colIndex using a slot grid. Mirrors Blink's slot tracking in
// LayoutTableSection::AddChild / LayoutTableSection::AppendCell
// (core/layout/table/layout_table_section.{h,cc}).
//
// Rules:
//   - A cell at (row R, column C) with span (r, c) reserves the
//     r×c rectangle of slots [R..R+r-1] × [C..C+c-1].
//   - Placing a cell in row R starts at the first unoccupied column
//     at or after the previous cell's end column.
//   - rowSpan is clamped so it never extends past the last row — a
//     cell at row N-1 with rowSpan=10 ends up spanning only row N-1.
//     Matches CSS 2.1 §17.6.1 / HTML5 behavior (rowspan clamps to
//     end of the table; cross-section handling is a future concern).
//
// Returns the total number of columns (max reserved slot + 1).
func assignColumnIndices(rows []tableRow) int {
	if len(rows) == 0 {
		return 0
	}
	occupied := make([][]bool, len(rows))
	for i := range occupied {
		occupied[i] = make([]bool, 0, 8)
	}
	ensureCols := func(rowIdx, upto int) {
		for len(occupied[rowIdx]) < upto {
			occupied[rowIdx] = append(occupied[rowIdx], false)
		}
	}
	maxCol := 0
	for rowIdx := range rows {
		col := 0
		for ci := range rows[rowIdx].cells {
			cell := &rows[rowIdx].cells[ci]
			// Skip past slots already reserved by rowspan cells
			// from earlier rows.
			for col < len(occupied[rowIdx]) && occupied[rowIdx][col] {
				col++
			}
			cell.colIndex = col
			// Clamp rowSpan so it cannot extend past the end of rows.
			spanEnd := rowIdx + cell.rowSpan
			if spanEnd > len(rows) {
				spanEnd = len(rows)
			}
			for r := rowIdx; r < spanEnd; r++ {
				ensureCols(r, col+cell.colSpan)
				for c := col; c < col+cell.colSpan; c++ {
					occupied[r][c] = true
				}
			}
			col += cell.colSpan
			if col > maxCol {
				maxCol = col
			}
		}
	}
	return maxCol
}

// specifiedBlockSize returns style's specified block-axis length in the
// writing mode wdm (height, or width when vertical), or Indefinite when style
// is nil or the block size is not a length. No Blink analog: louis14
// synthesizes row and section fragments without a row/section algorithm whose
// ChildAvailableSize (CalculateChildAvailableSize, length_utils.cc) would carry
// this; it is the basis for the chromium bug 1227884 relative insets.
func specifiedBlockSize(style *css.Style, wdm WritingDirectionMode) float64 {
	if style == nil {
		return Indefinite
	}
	if v, ok := style.GetLength(logicalBlockSizeProperty(wdm)); ok {
		return v
	}
	return Indefinite
}

// tableCellConstraintSpace is a table cell's constraint space: its fixed
// inline size, its block size (Indefinite in the measure pass, fixed in the
// final layout, with an indefinite initial block size unless
// computeCellBlockSize says it is definite) and an indefinite percentage
// block size. Mirrors Blink's SetupTableCellConstraintSpaceBuilder
// (table/table_layout_utils.cc:1363-1425 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14's
// IsFixedBlockSizeIndefinite is Blink's IsInitialBlockSizeIndefinite. Not
// ported: the alignment baseline (LOU-839 T8b) and
// IsRestrictedBlockSizeTableCell.
func (tla *TableLayoutAlgorithm) tableCellConstraintSpace(cellWDM WritingDirectionMode, inlineSize, blockSize float64, isInitialBlockSizeIndefinite bool, tableData *TableConstraintSpaceData) ConstraintSpace {
	wdm := tla.space.WritingDirection
	b := NewConstraintSpaceBuilder(wdm, cellWDM, true).
		SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(cellWDM, tla.ctx)).
		SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
		SetAvailableSize(LogicalSize{InlineSize: inlineSize, BlockSize: blockSize}).
		SetIsFixedInlineSize(true).
		SetPercentageResolutionSize(LogicalSize{InlineSize: inlineSize, BlockSize: Indefinite}).
		SetPercentageResolutionInlineSize(inlineSize).
		SetTableSectionData(tableData).
		SetTableSectionIndex(0)
	if blockSize != Indefinite {
		b.SetIsFixedBlockSize(true)
		if wdm.IsVertical() == cellWDM.IsVertical() {
			// The cell's own CSS block size does not size it: the row
			// does (ComputeBlockSizeForFragment's fixed branch,
			// length_utils.cc:906-916).
			b.SetIsBlockSizeOverride(true).
				SetIsFixedBlockSizeIndefinite(isInitialBlockSizeIndefinite)
		}
	}
	return b.Build()
}

// layoutCellAtFinalSize lays a cell out again at its final block size,
// unless that is the block size it measured at: then the final space lays
// it out as the measure space did (its children see the same percentage
// base, an indefinite one or the cell's own fixed block size, and there is
// no free space to align), so the measure result stands. Blink gets the
// same reuse from its layout cache (BlockNode::Layout → CachedLayoutResult,
// layout_result_caching.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// without it each level of nested tables lays its cells out twice, which
// is exponential in the nesting depth.
func (tla *TableLayoutAlgorithm) layoutCellAtFinalSize(cell *LayoutInputNode, cellWDM WritingDirectionMode, inlineSize float64, measure *LayoutResult, cellBlockSize float64, initialIndefinite bool, tableData *TableConstraintSpaceData) *LayoutResult {
	measured := NewLogicalFragment(tla.space.WritingDirection, measure.Fragment).BlockSize()
	if layoutunit.FromFloat64Round(measured) == layoutunit.FromFloat64Round(cellBlockSize) {
		return measure
	}
	return layoutElement(tla.ctx, cell,
		tla.tableCellConstraintSpace(cellWDM, inlineSize, cellBlockSize, initialIndefinite, tableData))
}

// computeCellBlockSize returns a table cell's final block size, the sum of
// the non-collapsed rows it spans plus the spacing between them, and
// whether its initial block size is indefinite: it is definite only when
// the cell is constrained (a specified block size) or it grew past its
// minBlockSize in a table with a specified block size. Mirrors Blink's
// ComputeCellBlockSize (table/table_layout_utils.cc:1333-1361 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func computeCellBlockSize(rows TableTypesRows, rowIndex, rowSpan int, blockSpacing, minBlockSize float64, isConstrained, isTableBlockSizeSpecified bool) (float64, bool) {
	cellBlockSize := 0.0
	if !rows[rowIndex].IsCollapsed {
		for i := 0; i < rowSpan && rowIndex+i < len(rows); i++ {
			if rows[rowIndex+i].IsCollapsed {
				continue
			}
			cellBlockSize += rows[rowIndex+i].BlockSize
			if i != 0 {
				cellBlockSize += blockSpacing
			}
		}
	}
	hasGrown := cellBlockSize > minBlockSize
	isInitialBlockSizeDefinite := isConstrained || (hasGrown && isTableBlockSizeSpecified)
	return cellBlockSize, !isInitialBlockSizeDefinite
}

// cellHasSpecifiedBlockSize reports whether a cell's block size is a fixed
// length, which constrains it: CellBlockConstraint::is_constrained is
// cell_specified_block_length.IsFixed() (table/table_layout_utils.cc:
// 248-253 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a percentage only
// constrains its row (:264-289).
func cellHasSpecifiedBlockSize(style *css.Style, wdm WritingDirectionMode) bool {
	if style == nil {
		return false
	}
	_, ok := cellFixedLength(style, logicalBlockSizeProperty(wdm))
	return ok
}

// cellCSSBlockSize returns a cell's fixed block size as a border-box size,
// the row's minimum for it; its min/max block sizes are ignored. Mirrors
// cell_css_block_size in ComputeMinimumRowBlockSize (table/
// table_layout_utils.cc:265-292 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// with the cell's border and padding as the measured fragment resolved
// them (collapsed borders included). louis14 has no quirks mode, so the
// table-cell-height box-sizing quirk does not apply.
func cellCSSBlockSize(style *css.Style, wdm WritingDirectionMode, frag *PhysicalFragment) (float64, bool) {
	if style == nil || frag == nil {
		return 0, false
	}
	size, ok := cellFixedLength(style, logicalBlockSizeProperty(wdm))
	if !ok {
		return 0, false
	}
	borderPadding := 0.0
	if frag.BoxData != nil {
		borderPadding = ToLogicalEdges(frag.BoxData.Border, wdm).BlockSum() +
			ToLogicalEdges(frag.BoxData.Padding, wdm).BlockSum()
	}
	// Snap the length to a LayoutUnit, as ResolveBlockSize does.
	size = layoutunit.FromFloat64Round(size).Float64()
	if style.GetBoxSizing() == "border-box" {
		return max(borderPadding, size), true
	}
	return borderPadding + size, true
}

// relativeCellOffset returns a table cell's relative offset in the table's
// logical coordinates. A cell resolves against its row: the
// row's inline size and specified block size (chromium bug 1227884, WPT
// css-position/position-relative-010/013). Mirrors Blink, where
// TableRowLayoutAlgorithm adds each cell (table_row_layout_algorithm.cc:167)
// and BoxFragmentBuilder::AddChild resolves the offset against the row's
// child_available_size (box_fragment_builder.cc:262-267); the section gives
// the row an indefinite available block size
// (table_section_layout_algorithm.cc:38-39, :76-77), all @
// d4ecdfed88f962439247c2ad36b8fe47805b1520.
func relativeCellOffset(cell tableCell, rowStyle *css.Style, wdm WritingDirectionMode, rowInlineSize float64) LogicalOffset {
	return ComputeRelativeOffset(cell.style, wdm, LogicalSize{
		InlineSize: rowInlineSize,
		BlockSize:  specifiedBlockSize(rowStyle, wdm),
	})
}

// computeColumnWidthsFixed computes column widths using the fixed table layout
// algorithm (CSS 2.1 §17.5.2.1). Only the first row is examined for sizing.
// This is faster and more predictable than the auto algorithm.
func (tla *TableLayoutAlgorithm) computeColumnWidthsFixed(
	rows []tableRow, numCols int, availableForCols float64, spacingForCols float64,
	colWidthSpecs []tableColWidth,
) []float64 {
	if numCols == 0 {
		return []float64{}
	}

	colWidths := make([]float64, numCols)
	hasExplicit := make([]bool, numCols)

	// Step 1a: Apply <col> element widths (CSS 2.1 §17.5.2.1 step 1).
	// "A column element with a value other than 'auto' for the 'width'
	// property sets the width for that column."
	for _, cws := range colWidthSpecs {
		// Compute the effective per-column width: start from cws.width,
		// apply max then min clamp (CSS used-value order: min wins over smaller max).
		eff := cws.width
		if cws.hasMax {
			if eff > cws.maxWidth {
				eff = cws.maxWidth
			}
		}
		if cws.hasMin {
			if eff < cws.minWidth {
				eff = cws.minWidth
			}
		}
		// Assign eff to columns only when the column has an effective constraint:
		// width>0 OR hasMin (a min-raised column is constrained).
		// A column with hasMax only and width==0 stays unconstrained (max-width alone is inert).
		// Use cws.width (declared width), not eff (clamped), so that width:Xpx; max-width:0
		// still treats the column as constrained (it gets eff=0, not auto).
		if cws.width > 0 || cws.hasMin {
			for s := 0; s < cws.span; s++ {
				ci := cws.colIndex + s
				if ci < numCols && !hasExplicit[ci] {
					colWidths[ci] = eff
					hasExplicit[ci] = true
				}
			}
		}
	}

	// Step 1b: Check first-row cells for explicit widths.
	// "A cell in the first row with a value other than 'auto' for the 'width'
	// property determines the width for that column."
	if len(rows) > 0 {
		for _, cell := range rows[0].cells {
			colIdx := cell.colIndex
			if colIdx >= numCols {
				continue
			}
			if cell.style != nil {
				inlineProp := "width"
				if tla.space.WritingDirection.IsVertical() {
					inlineProp = "height"
				}
				if w, ok := cell.style.GetLength(inlineProp); ok && w > 0 {
					// Convert content-box to border-box for column sizing.
					wBB := w
					if cell.style.GetBoxSizing() != "border-box" {
						cellGeom := ComputeFragmentGeometry(cell.style, tla.space.WritingDirection)
						wBB += cellGeom.InlineBorderPadding()
					}
					if cell.colSpan > 1 {
						// Distribute explicit width equally across spanned columns.
						perCol := wBB / float64(cell.colSpan)
						for c := 0; c < cell.colSpan && colIdx+c < numCols; c++ {
							if !hasExplicit[colIdx+c] {
								colWidths[colIdx+c] = perCol
								hasExplicit[colIdx+c] = true
							}
						}
					} else {
						colWidths[colIdx] = wBB
						hasExplicit[colIdx] = true
					}
				}
			}
		}
	}

	// Step 2: Distribute remaining width equally among columns without
	// explicit widths (CSS 2.1 §17.5.2.1 step 3).
	usedWidth := 0.0
	unsetCols := 0
	for i := 0; i < numCols; i++ {
		usedWidth += colWidths[i]
		if !hasExplicit[i] {
			unsetCols++
		}
	}

	if unsetCols > 0 {
		remaining := availableForCols - usedWidth
		if remaining > 0 {
			perCol := remaining / float64(unsetCols)
			for i := 0; i < numCols; i++ {
				if !hasExplicit[i] {
					colWidths[i] = perCol
				}
			}
		} else {
			// Fallback: give unset columns a minimal width.
			for i := 0; i < numCols; i++ {
				if !hasExplicit[i] {
					colWidths[i] = 10
				}
			}
		}
	}

	return colWidths
}

// cellExplicitInlineSize returns the cell's specified inline-size measured along
// the TABLE's inline axis (CSS "width" for a horizontal table, "height" for a
// vertical one), and whether one is specified. For an orthogonal cell the cell's
// physical width still equals the table's inline extent, so the same property
// applies. Mirrors Blink CellInlineConstraint.is_constrained — "true if this cell
// has a specified inline-size" (ng_table_layout_algorithm_types.h @ Chromium SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func cellExplicitInlineSize(cell tableCell, tableWD WritingDirectionMode) (float64, bool) {
	if cell.style == nil {
		return 0, false
	}
	if w, ok := cellFixedLength(cell.style, logicalInlineSizeProperty(tableWD)); ok && w > 0 {
		return w, true
	}
	return 0, false
}

// cellFixedLength returns a cell's prop when it is a fixed length (Blink's
// Length::IsFixed), the only sizes the table reads as lengths
// (InlineSizesFromStyle, table/table_layout_algorithm_types.cc:24-75;
// ComputeMinimumRowBlockSize, table/table_layout_utils.cc:265-292 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A calculation with a
// percentage is not one, though GetLength resolves it against 0: the
// table reads it as a percentage at most (cellPercentageInlineSize), else
// as auto.
func cellFixedLength(style *css.Style, prop string) (float64, bool) {
	v, ok := style.Get(prop)
	if !ok || css.IsMathFunctionWithPercent(v) {
		return 0, false
	}
	return style.GetLengthForVal(v)
}

// computeColumnConstraints returns the table's column constraints: each
// column's border-box min/max inline-size, percentage and whether it is
// constrained, from the <col>/<colgroup> widths and the single-column
// cells. Mirrors Blink's ComputeColumnConstraints
// (table/table_layout_utils.cc:1450-1485), with
// ApplyCellConstraintsToColumnConstraints's total-percentage clamp
// (:144-160) and Column::Encompass's percentage rule
// (table/table_layout_algorithm_types.cc:290-293), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. Not ported: colspan cells'
// distribution to columns (DistributeColspanCellsToColumns), mergeable
// columns, and <col> percentages. availableInline is the cells' available
// inline-size (Indefinite while sizing the table intrinsically);
// inlineSpacing is the table's inline border spacing (0 when collapsed).
func (tla *TableLayoutAlgorithm) computeColumnConstraints(rows []tableRow, numCols int, availableInline, inlineSpacing float64, colWidthSpecs []tableColWidth) *TableTypesColumns {
	// Each column's min/max content width.
	colMin := make([]float64, numCols)
	colMax := make([]float64, numCols)
	colPercent := make([]float64, numCols)
	colHasPercent := make([]bool, numCols)
	var colspanCells []TableTypesColspanCell

	// colConstrained is the order-independent "this column has a specified width"
	// set — Blink's Column.is_constrained ("true if any cell for this column is
	// constrained"; ng_table_layout_algorithm_types.h @ Chromium SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). When a column is constrained, that
	// width is authoritative: space above every column's max goes to the auto
	// columns first (DistributeInlineSizeToComputedInlineSizeAuto's above-max
	// case), and an orthogonal cell's unreliable intrinsic sizing is skipped. The
	// set is populated in FULL before the intrinsic-sizing loop — col/colgroup widths
	// just below, cell widths in the pre-pass — so both readers are independent of
	// the order cells/rows are visited.
	colConstrained := make([]bool, numCols)

	// Apply col/colgroup explicit widths first (CSS Tables §9.1).
	// These establish the column widths from the column model.
	for _, cws := range colWidthSpecs {
		for s := 0; s < cws.span; s++ {
			ci := cws.colIndex + s
			if ci >= numCols {
				break
			}
			// Compute the effective per-column width: start from cws.width,
			// apply max then min clamp (CSS used-value order: min wins over smaller max).
			eff := cws.width
			if cws.hasMax {
				if eff > cws.maxWidth {
					eff = cws.maxWidth
				}
			}
			if cws.hasMin {
				if eff < cws.minWidth {
					eff = cws.minWidth
				}
			}
			// Fold eff into colMin/colMax exactly as cws.width was (only raise).
			if eff > colMin[ci] {
				colMin[ci] = eff
			}
			if eff > colMax[ci] {
				colMax[ci] = eff
			}
			// Set colConstrained only when the column has an effective constraint:
			// width>0 OR hasMin (a min-raised column is constrained).
			// A column with hasMax only and width==0 stays unconstrained (max-width alone is inert).
			if cws.width > 0 || cws.hasMin {
				colConstrained[ci] = true
			}
		}
	}

	// Pre-pass: fold cell-level specified widths into colConstrained before the
	// intrinsic-sizing loop below reads it. A colSpan==1 cell with a specified
	// inline-size constrains its column (Blink CellInlineConstraint.is_constrained →
	// Column.is_constrained). Computing this up-front is what makes the orthogonal-
	// cell skipIntrinsic decision independent of cell/row visitation order.
	for _, row := range rows {
		for _, cell := range row.cells {
			if cell.colIndex >= numCols || cell.colSpan != 1 {
				continue
			}
			if _, ok := cellExplicitInlineSize(cell, tla.space.WritingDirection); ok {
				colConstrained[cell.colIndex] = true
			}
		}
	}

	for _, row := range rows {
		for _, cell := range row.cells {
			colIdx := cell.colIndex
			if colIdx >= numCols {
				continue
			}

			// Explicit inline-size on the cell, from the table's axis. Shared with the
			// colConstrained pre-pass above via cellExplicitInlineSize.
			explicitW, hasExplicit := cellExplicitInlineSize(cell, tla.space.WritingDirection)

			// The cell's own constraint (CreateCellInlineConstraint):
			// hasCellSize is false when an orthogonal cell's intrinsic
			// sizing is skipped.
			cellMin, cellMax, hasCellSize := 0.0, 0.0, true
			pct, hasPct := cellPercentageInlineSize(cell.style, tla.space.WritingDirection)
			if hasExplicit {
				// explicitW is a content-box width; column sizing uses border-box.
				// If the cell has box-sizing:border-box the value already includes BP;
				// otherwise add the cell's inline border+padding (from the table's axis).
				explicitBB := explicitW
				if cell.style == nil || cell.style.GetBoxSizing() != "border-box" {
					cellGeom := ComputeFragmentGeometry(cell.style, tla.space.WritingDirection)
					explicitBB += cellGeom.InlineBorderPadding()
				}
				cellMin, cellMax = explicitBB, explicitBB
				// (colConstrained for this column was set in the pre-pass above.)
			} else {
				// Compute intrinsic size.
				// For an orthogonal cell (e.g. vertical-rl cell in a
				// horizontal table) the contribution is the inline size,
				// in the table's writing mode, of the cell laid out: Blink
				// BlockNode::ComputeMinMaxSizes runs layout for an
				// orthogonal flow root and returns min = max = that size
				// (core/layout/block_node.cc:968-998 @
				// d4ecdfed88f962439247c2ad36b8fe47805b1520), which
				// TableTypes::CreateCellInlineConstraint calls
				// (table/table_layout_algorithm_types.cc:138-163). The
				// laid-out fragment is border-box, so no border+padding is
				// added.
				//
				// When the column is constrained by a specified width (col/colgroup
				// OR any cell — Blink Column.is_constrained), that width is
				// authoritative, so we skip the orthogonal cell's intrinsic
				// sizing rather than let it override the constraint. colConstrained is
				// fully populated before this loop, so the decision does not depend on
				// the order cells/rows are visited.
				childWDM := tla.space.WritingDirection
				if cell.style != nil {
					childWDM = NewWritingDirectionMode(cell.style)
				}
				isOrthogonal := tla.space.WritingDirection.IsVertical() != childWDM.IsVertical()
				skipIntrinsic := isOrthogonal && cell.colSpan == 1 && colConstrained[colIdx]
				hasCellSize = !skipIntrinsic
				if !skipIntrinsic {
					childSpace := NewConstraintSpaceBuilder(tla.space.WritingDirection, childWDM, true).
						SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
						SetOrthogonalFallbackInlineSize(
							orthogonalFallbackSize(childWDM, tla.ctx)).
						SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
						SetAvailableSize(LogicalSize{InlineSize: availableInline, BlockSize: Indefinite}).
						SetPercentageResolutionInlineSize(tla.space.PercentageResolutionInlineSize).
						Build()
					if isOrthogonal {
						res := layoutElement(tla.ctx, cell.node, childSpace)
						cellMin = NewLogicalFragment(tla.space.WritingDirection, res.Fragment).InlineSize()
						cellMax = cellMin
					} else {
						mm := ComputeMinMaxSizes(tla.ctx, cell.node, childSpace)
						// Convert content-box to border-box for column sizing.
						cellBP := ComputeFragmentGeometry(cell.style, childWDM).InlineBorderPadding()
						cellMin = mm.MinContent + cellBP
						cellMax = mm.MaxContent + cellBP
					}
				}
			}
			if cell.colSpan > 1 {
				colspanCells = append(colspanCells, TableTypesColspanCell{
					CellInlineConstraint: TableTypesCellInlineConstraint{
						MinInlineSize: cellMin,
						MaxInlineSize: max(cellMin, cellMax),
						Percent:       pct,
						HasPercent:    hasPct,
						IsConstrained: hasExplicit,
					},
					StartColumn: colIdx,
					Span:        cell.colSpan,
				})
				continue
			}
			// Column::Encompass keeps the largest cell percentage.
			if hasPct && (!colHasPercent[colIdx] || pct > colPercent[colIdx]) {
				colPercent[colIdx], colHasPercent[colIdx] = pct, true
			}
			if hasCellSize {
				colMin[colIdx] = max(colMin[colIdx], cellMin)
				colMax[colIdx] = max(colMax[colIdx], cellMax)
			}
		}
	}

	columns := &TableTypesColumns{Data: make([]TableTypesColumn, numCols)}
	for i := range columns.Data {
		columns.Data[i] = TableTypesColumn{
			MinInlineSize: colMin[i],
			MaxInlineSize: colMax[i],
			Percent:       colPercent[i],
			HasPercent:    colHasPercent[i],
			IsConstrained: colConstrained[i],
		}
	}

	// Colspan cells, shortest span first then by start column, spread
	// across the columns they span (ApplyCellConstraintsToColumn-
	// Constraints, table/table_layout_utils.cc:131-142).
	sort.SliceStable(colspanCells, func(i, j int) bool {
		if colspanCells[i].Span == colspanCells[j].Span {
			return colspanCells[i].StartColumn < colspanCells[j].StartColumn
		}
		return colspanCells[i].Span < colspanCells[j].Span
	})
	for _, colspanCell := range colspanCells {
		DistributeColspanCellToColumnsAuto(colspanCell, inlineSpacing, columns)
	}

	// Column total percentage is clamped to 100%: max(0, 100% minus the
	// percentages of the prior columns) (auto layout, :144-160).
	totalPercentage := 0.0
	for i := range columns.Data {
		if columns.Data[i].HasPercent {
			columns.Data[i].Percent = min(columns.Data[i].Percent, 100-totalPercentage)
			totalPercentage += columns.Data[i].Percent
		}
	}
	return columns
}

// computeColumnWidths returns an auto-layout table's column widths: the
// assignable table inline size, distributed to the column constraints.
func (tla *TableLayoutAlgorithm) computeColumnWidths(
	rows []tableRow, numCols int, availableInline float64, borderCollapse bool, hasExplicitWidth bool,
	geom FragmentGeometry, captions []tableCaption, colWidthSpecs []tableColWidth,
) []float64 {
	inlineSpacing := 0.0
	if !borderCollapse {
		inlineSpacing, _ = tla.logicalBorderSpacing()
	}
	columns := tla.computeColumnConstraints(rows, numCols, availableInline, inlineSpacing, colWidthSpecs)

	// The assignable table inline size (ComputeAssignableTableInlineSize,
	// table/table_layout_algorithm.cc:223-258 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): when the parent fixed the
	// table's inline size (a stretched grid item, a flex item), it is that
	// size less the border spacing, already out of availableInline
	// (:231-234). Otherwise it is the used width less the border spacing.
	// The used width (ComputeUsedInlineSizeForTableFragment,
	// length_utils.cc:527-598, :618-649) is the specified width
	// (availableInline), or for an auto width fit-content over the
	// layout-pass grid min/max, where percentage columns count; clamped to
	// the table's min/max inline sizes, which never go below the grid min
	// (ComputeMinMaxInlineSizes, :758-799), then floored at the captions'
	// min-content (table_layout_algorithm.cc:248-250). The columns share the
	// assignable size by the width distribution algorithm
	// (SynchronizeAssignableTableInlineSizeAndColumns, table/
	// table_layout_utils.cc:1849-1864).
	if tla.space.IsFixedInlineSize {
		return DistributeInlineSizeToComputedInlineSizeAuto(availableInline, columns.Data, true)
	}
	grid := ComputeGridInlineMinMax(tla.node, columns, 0, false, true)
	extent := availableInline
	if !hasExplicitWidth {
		extent = min(max(extent, grid.MinContent), grid.MaxContent)
	}
	// The min/max inline sizes are content-box; the columns' space is less
	// the border spacing.
	spacing := inlineSpacing * float64(numCols+1)
	wdm := tla.space.WritingDirection
	minProp, maxProp := "min-width", "max-width"
	if wdm.IsVertical() {
		minProp, maxProp = "min-height", "max-height"
	}
	content := func() MinMaxSizes {
		return MinMaxSizes{MinContent: grid.MinContent + spacing, MaxContent: grid.MaxContent + spacing}
	}
	sizes := computeMinMaxInlineSizes(tla.style, wdm, tla.space, geom, minProp, maxProp, content, fitContentModeNormal, true)
	used := sizes.ClampSizeToMinAndMax(extent + spacing)
	// Never below the captions' min-content contributions (border-box),
	// taken against the table's content box (:248-250).
	used = max(used, tla.computeCaptionConstraint(captions).MinContent-geom.InlineBorderPadding())
	assignable := used - spacing
	return DistributeInlineSizeToComputedInlineSizeAuto(assignable, columns.Data, true)
}

// tableCaption tracks a caption element during table layout.
type tableCaption struct {
	node  *LayoutInputNode
	style *css.Style
	side  string // "top" or "bottom"
}

// captionResult is a laid-out caption with its resolved margins. Mirrors
// Blink TableLayoutAlgorithm::CaptionResult (table_layout_algorithm.h:43 @
// d4ecdfed88f9) minus its node, which louis14 reads from the fragment.
type captionResult struct {
	result  *LayoutResult
	margins LogicalEdges // in the table's writing direction
}

// layoutCaption lays out one caption and resolves its margins. Mirrors Blink
// ComputeCaptionMargins (table_layout_algorithm.cc:118-128),
// CreateCaptionConstraintSpace (:61-90) and LayoutCaption (:92-111), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. Margins are resolved in the
// table's writing direction, as Blink passes the table's to
// ComputeMarginsFor.
//
// The caption lays out at the table's final inline size: its available
// and percentage-resolution inline size, and the base its margins resolve
// against, are tableInline (GenerateFragment, :1230-1243). Divergence from Blink:
// tableInline is the table's content box, where Blink uses its border box,
// because louis14 places captions inside the table's border and padding
// (addCaptionResult; LOU-573). They agree when the table has no inline
// border or padding.
//
// An auto-width caption stretches to the available size minus its inline
// margins: Blink's kStretchImplicit (:73) feeds the kStretch inline size,
// which subtracts the box's own margins (length_utils.cc:46-62).
func (tla *TableLayoutAlgorithm) layoutCaption(cap tableCaption, wdm WritingDirectionMode, tableInline float64) captionResult {
	margins := ResolveMargins(cap.style, wdm, tableInline)
	capWDM := wdm
	if cap.style != nil {
		capWDM = NewWritingDirectionMode(cap.style)
	}
	capSpace := NewConstraintSpaceBuilder(wdm, capWDM, true).
		SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
		SetOrthogonalFallbackInlineSize(
			orthogonalFallbackSize(capWDM, tla.ctx)).
		SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
		SetAvailableSize(LogicalSize{
			InlineSize: max(0, tableInline-margins.InlineSum()),
			BlockSize:  Indefinite,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: tableInline,
			BlockSize:  0,
		}).
		SetPercentageResolutionInlineSize(tableInline).
		Build()

	result := layoutElement(tla.ctx, cap.node, capSpace)
	margins = resolveInlineAutoMargins(cap.style, wdm, tableInline,
		NewLogicalFragment(wdm, result.Fragment).InlineSize(), margins)
	return captionResult{result: result, margins: margins}
}

// addCaptionResult adds a caption at its resolved margins and advances
// *blockOffset past its margin box. Mirrors Blink's AddCaptionResult lambda
// (table_layout_algorithm.cc:966-979 @ d4ecdfed88f9). Blink's AddResult also
// records the margins on the child (SetMargins); louis14 has no such builder
// field, and the caption's own layout records BoxData.Margin.
//
// The offset is relative to the table's content box, so captions sit inside
// the table's border and padding; Blink places them in the table wrapper,
// outside the table border. AddChild shifts the caption by its
// position:relative offset (box_fragment_builder.cc:255-267 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (tla *TableLayoutAlgorithm) addCaptionResult(builder *BoxFragmentBuilder, wdm WritingDirectionMode, cr captionResult, blockOffset *float64) {
	*blockOffset += cr.margins.BlockStart
	builder.AddChild(cr.result.Fragment, LogicalOffset{
		InlineOffset: cr.margins.InlineStart,
		BlockOffset:  *blockOffset,
	})
	*blockOffset += NewLogicalFragment(wdm, cr.result.Fragment).BlockSize() + cr.margins.BlockEnd
}

// logicalBorderSpacing returns border spacing mapped to table logical coordinates.
// CSS border-spacing first value = horizontal = inline spacing (between columns).
// CSS border-spacing second value = vertical = block spacing (between rows).
// Per CSS Writing Modes 3 §7.2, the first value maps to the inline dimension and
// the second to the block dimension across all writing modes.
//
// Lengths resolve through the table style's conversion data (its font for
// em / ch / ex, etc.).
func (tla *TableLayoutAlgorithm) logicalBorderSpacing() (inlineSpacing, blockSpacing float64) {
	val, ok := tla.style.Get("border-spacing")
	if !ok {
		return 0, 0
	}
	parts := strings.Fields(val)
	if len(parts) >= 1 {
		if v, ok := tla.style.GetLengthForVal(parts[0]); ok {
			inlineSpacing = v
		}
	}
	if len(parts) >= 2 {
		if v, ok := tla.style.GetLengthForVal(parts[1]); ok {
			blockSpacing = v
		}
	} else {
		blockSpacing = inlineSpacing // single value applies to both
	}
	return
}

// parseIntAttr parses an integer from an HTML attribute value.
func parseIntAttr(s string) int {
	v := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			v = v*10 + int(c-'0')
		} else {
			break
		}
	}
	return v
}

// parseIntAttrWithZero is like parseIntAttr but reports whether any
// digits were consumed, distinguishing an explicit "0" from a missing
// / non-numeric value. Used by rowspan parsing where 0 has a distinct
// meaning ("span to end of section" per WHATWG HTML §4.9.11).
func parseIntAttrWithZero(s string) (int, bool) {
	v := 0
	seen := false
	for _, c := range s {
		if c >= '0' && c <= '9' {
			v = v*10 + int(c-'0')
			seen = true
		} else {
			break
		}
	}
	return v, seen
}

// --- Single-pass row sizing helpers ---
//
// The types, helpers, and distribution algorithm below mirror Blink's
// single-pass table sizing pipeline in
// third_party/blink/renderer/core/layout/table/. Layout() calls these
// in three phases (measure → size → assemble) as the sole row-sizing
// path. The legacy build-then-mutate-then-reposition flow was removed
// in refactor-(b) ckpt 3.

// tableRowIntrinsic holds the per-row data needed to build a
// TableTypesRow. It is populated during the cell-layout sweep inside
// Layout() — the same sweep the legacy path uses. This is the
// "already-collected rows/cells" input computeRows walks.
//
// Fields mirror the inputs to Blink's ComputeMinimumRowBlockSize
// (table_layout_utils.cc ~L315) after cell layout has produced
// intrinsic block-sizes for each cell.
type tableRowIntrinsic struct {
	// intrinsicHeight is the row's max cell min-content block-size
	// (the legacy path's rowInfos[i].height pre-redistribution).
	intrinsicHeight float64

	// hasExplicitCell is true if any cell in the row has an explicit
	// CSS block-size (height or width on orthogonal cells). Maps to
	// Blink's per-cell IsConstrained contribution to the row.
	hasExplicitCell bool

	// cellCount is the number of cells originating in this row.
	cellCount int

	// startCellIndex is the flat cell index at which this row's cells
	// begin. Mirrors Blink's TableTypes::Row::start_cell_index.
	startCellIndex int

	// hasRowspanStart is true if any originating cell has rowspan > 1.
	// We do not currently support rowspan sizing, so this is a carry-over
	// for the 5-priority distribution's step 2; see the rowspan note in
	// the commit message.
	hasRowspanStart bool

	// baseline is the max cell first-baseline across baseline-aligned
	// cells in this row (Blink's RowBaselineTabulator result).
	baseline float64

	// row points to the underlying tableRow (for style access when
	// deriving percent / collapsed / constrained flags).
	row *tableRow
}

// computeRows builds a TableTypesRows vector from the already-collected
// per-row intrinsic data. Mirrors Blink's
// TableLayoutAlgorithm::ComputeRows (table_layout_algorithm.cc ~L2073)
// which aggregates per-row minimums via ComputeMinimumRowBlockSize
// (table_layout_utils.cc ~L315).
//
// We intentionally do NOT re-layout cells here — the legacy cell-layout
// sweep has already produced the intrinsic block-sizes we need.
// Extracting the aggregation logic (not the cell layout itself) is the
// "extract, don't invent" contract called out in the ckpt 2 prompt.
func computeRows(intrinsics []tableRowIntrinsic, wdm WritingDirectionMode) TableTypesRows {
	result := make(TableTypesRows, len(intrinsics))
	for i, in := range intrinsics {
		row := &result[i]
		row.BlockSize = in.intrinsicHeight
		row.StartCellIndex = in.startCellIndex
		row.CellCount = in.cellCount
		row.Baseline = in.baseline
		row.Percent = TableTypesRowAbsentPercent
		row.IsConstrained = in.hasExplicitCell
		row.HasRowspanStart = in.hasRowspanStart
		row.IsCollapsed = false

		// Pull row-level modifiers from the row's style (percent
		// block-size, explicit block-size, visibility:collapse) if
		// available. Note: Blink reads these in
		// ComputeMinimumRowBlockSize and propagates them onto each
		// TableTypes::Row before distribution.
		if in.row != nil && in.row.style != nil {
			s := in.row.style
			// Row percentage block-size: CSS 2.1 §17.5.3 allows <tr>
			// to carry a percentage height that resolves against the
			// table's definite height. We record it on the row
			// regardless — the distribution pass will ignore it when
			// the table has no definite height (Percent path goes
			// through priority 1 only when Percent > 0 and the table
			// block size is known).
			prop := logicalBlockSizeProperty(wdm)
			if pct, ok := s.GetPercentage(prop); ok && pct > 0 {
				row.Percent = pct
			}
			// A fixed specified block size is a floor on the row's
			// block size, whatever its cells measure. Mirrors Blink's
			// "Apply row's CSS block size" step in
			// ComputeMinimumRowBlockSize
			// (third_party/blink/renderer/core/layout/table/table_layout_utils.cc:305-315
			// @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
			if v, ok := s.GetLength(prop); ok {
				row.IsConstrained = true
				row.BlockSize = max(row.BlockSize, v)
			}
			if s.GetVisibility() == "collapse" {
				row.IsCollapsed = true
			}
		}

		// CSS Tables 3 §3.5 (visibility:collapse on a row): the row is
		// not displayed, but its intrinsic block-size MUST be retained
		// during rowspan-cell distribution so the rowspan cell's minimum
		// is split proportionally across ALL spanned rows (collapsed or
		// not) — otherwise the non-collapsed spanned rows absorb the
		// collapsed row's share and over-grow, hiding the clipping
		// behavior §5.4.1 mandates for cells spanning collapsed rows.
		//
		// Collapsed rows are removed from the table at EMISSION time
		// (Phase 3): no fragment, no block-offset advance, no
		// inter-row spacing. Cell fragment sizing (§5.4.1 "clip the
		// content to the table-cell's border-box") is handled in
		// Phase 3b by summing only non-collapsed spanned rows in
		// spannedBlock and forcing the cell fragment to that extent.
		// Mirrors Blink's flow: is_collapsed flag set here, row
		// block-size kept (see table_layout_utils.cc), row emission
		// skipped in table_section_layout_algorithm.cc, cell block-size
		// reduced in ComputeCellBlockSize.
	}
	return result
}

// rowspanCellInfo is the triple (originating row, rowspan, minimum
// block-size) the priority-2 distribution step needs to compute each
// rowspan cell's unmet deficit. Mirrors the per-cell loop in Blink's
// DistributeExcessBlockSizeToRows (table_layout_utils.cc ~L1201) which
// reads the same data off the rowspan-cell bookkeeping it built during
// ComputeRows.
type rowspanCellInfo struct {
	rowIndex int
	rowSpan  int
	minBlock float64
}

// distributeTableBlockSizeToRows distributes `excess` block-size across
// `rows` in place, matching Blink's 5-priority distribution in
// DistributeExcessBlockSizeToRows (table_layout_utils.cc ~L1095).
//
// The five priorities (applied in order; later priorities fire on any
// excess that the earlier buckets did not absorb):
//
//  1. Percent rows: rows with Percent != TableTypesRowAbsentPercent
//     receive a share proportional to their percentage, capped at what
//     the table actually has to give. Blink: line ~1127.
//  2. Rowspan-originator rows (preferential): for each rowspan cell,
//     compute its unmet deficit (min block-size minus current spanned
//     block-size) and attribute a per-row share. Distribute
//     min(remaining, totalDeficit) proportionally to the accumulated
//     per-row deficit. Priority 2 does NOT consume surplus excess —
//     any remainder flows to priority 3. Blink: ~L1201.
//  3. Unconstrained non-empty rows: rows with !IsConstrained and a
//     non-zero BlockSize absorb remaining excess evenly. Blink: ~L1253.
//  4. Empty rows (BlockSize == 0): if no rows in priorities 1-3 were
//     eligible, distribute evenly across empty rows. Blink: ~L1286.
//  5. Proportional fallback: all rows participate, distributed in
//     proportion to their current BlockSize (or evenly if all zero).
//     Blink: ~L1318.
//
// Collapsed rows (IsCollapsed) never participate — their block-size
// stays zero. Same constraint as Blink.
// distributeRowspanCellToRows enforces a rowspan cell's minimum
// block-size by growing the rows it spans when their combined
// block-size (plus the row spacings between them) is less than the
// cell's own intrinsic block-size. Mirrors Blink's
// DistributeRowspanCellToRows in
// third_party/blink/renderer/core/layout/table/table_layout_utils.cc
// (~L1606).
//
// Inputs:
//   - rows:      the row vector to mutate in place.
//   - rowIdx:    the rowspan cell's originating row index.
//   - rowSpan:   the rowspan attribute (already clamped by
//     assignColumnIndices's slot grid; we re-clamp defensively
//     so this helper is safe to call directly).
//   - cellBlock: the cell's intrinsic block-size (the value that would
//     otherwise have been folded into the originating row's
//     intrinsic max in the rowSpan == 1 path).
//   - rowSpacing: CSS 2.1 §17.6.1 border-spacing in the block axis.
//
// Distribution rules (matches Blink):
//
//   - Compute `current` = Σ rows[R..R+s-1].BlockSize +
//     (s-1) * rowSpacing. This is the space the rowspan cell already
//     occupies given prior intrinsic sizing.
//   - If cellBlock > current, the cell has a deficit of
//     `cellBlock - current` that must be absorbed by growing the
//     spanned rows.
//   - If at least one spanned row has BlockSize > 0, the deficit is
//     distributed proportionally to each row's existing BlockSize
//     (matching Blink's "scale by current row block-size" rule).
//   - If every spanned row has BlockSize == 0, the deficit is
//     distributed evenly.
//   - Distribution uses last-row remainder accounting so the
//     post-distribution sum is exact (no float drift).
func distributeRowspanCellToRows(
	rows TableTypesRows,
	rowIdx, rowSpan int,
	cellBlock, rowSpacing float64,
) {
	if rowSpan <= 1 || cellBlock <= 0 || len(rows) == 0 {
		return
	}
	if rowIdx < 0 || rowIdx >= len(rows) {
		return
	}
	end := rowIdx + rowSpan
	if end > len(rows) {
		end = len(rows)
	}
	span := end - rowIdx
	if span <= 1 {
		// Cell does not actually span past the originating row (clamped
		// by table size). Treat its block-size as a contribution to the
		// originating row's intrinsic instead.
		if rows[rowIdx].BlockSize < cellBlock {
			rows[rowIdx].BlockSize = cellBlock
		}
		return
	}

	current := 0.0
	for i := rowIdx; i < end; i++ {
		current += rows[i].BlockSize
	}
	current += float64(span-1) * rowSpacing

	if cellBlock <= current {
		return
	}

	deficit := cellBlock - current

	// Total of current row block-sizes (excluding the spacings) — this
	// is what the proportional split sees.
	totalRowBlock := 0.0
	for i := rowIdx; i < end; i++ {
		totalRowBlock += rows[i].BlockSize
	}

	accum := 0.0
	if totalRowBlock > 0 {
		// Proportional distribution.
		for k := 0; k < span; k++ {
			i := rowIdx + k
			var share float64
			if k == span-1 {
				share = deficit - accum
			} else {
				share = deficit * rows[i].BlockSize / totalRowBlock
				accum += share
			}
			rows[i].BlockSize += share
		}
	} else {
		// All-zero spanned rows — distribute evenly.
		per := deficit / float64(span)
		for k := 0; k < span; k++ {
			i := rowIdx + k
			var share float64
			if k == span-1 {
				share = deficit - accum
			} else {
				share = per
				accum += share
			}
			rows[i].BlockSize += share
		}
	}
}

func distributeTableBlockSizeToRows(
	rows TableTypesRows,
	excess float64,
	rowspanCells []rowspanCellInfo,
	rowSpacing float64,
) {
	if len(rows) == 0 || excess <= 0 {
		return
	}

	// Build the participating-row index sets for each priority.
	// Priority 2 reads rowspan data from `rowspanCells` directly, so
	// no rowspan-originator index is built here (HasRowspanStart still
	// lives on TableTypesRow for Blink-struct parity, but is no longer
	// consulted by this function).
	var percentIdx, unconstrainedIdx, emptyIdx, fallbackIdx []int
	for i := range rows {
		if rows[i].IsCollapsed {
			continue
		}
		fallbackIdx = append(fallbackIdx, i)
		if rows[i].Percent != TableTypesRowAbsentPercent && rows[i].Percent > 0 {
			percentIdx = append(percentIdx, i)
		}
		if !rows[i].IsConstrained && rows[i].BlockSize > 0 {
			unconstrainedIdx = append(unconstrainedIdx, i)
		}
		if rows[i].BlockSize <= 0 && !rows[i].IsConstrained {
			emptyIdx = append(emptyIdx, i)
		}
	}

	remaining := excess

	// Priority 1 — percent rows.
	// Blink (table_layout_utils.cc ~L1127): scale each percent row's
	// block-size up to its percent-derived target, bounded by the
	// available excess. We compute the total percent demand and hand
	// each row its proportional slice.
	if remaining > 0 && len(percentIdx) > 0 {
		totalPct := 0.0
		for _, i := range percentIdx {
			totalPct += rows[i].Percent
		}
		if totalPct > 0 {
			// Proportional share of the excess — not a "grow to
			// percent-of-table" target, matching Blink's behavior
			// when the excess is the authoritative budget.
			distribute := remaining
			if totalPct > 100 {
				// Over-specified percents: scale back so the total
				// share cannot exceed 100% of the excess.
				totalPct = 100
			}
			portion := distribute * totalPct / 100
			if portion > remaining {
				portion = remaining
			}
			// Split `portion` proportionally among percent rows.
			accum := 0.0
			for k, i := range percentIdx {
				var share float64
				if k == len(percentIdx)-1 {
					share = portion - accum
				} else {
					share = portion * rows[i].Percent / totalPct
					accum += share
				}
				rows[i].BlockSize += share
			}
			remaining -= portion
		}
	}

	// Priority 2 — rowspan originators (Blink-style preferential).
	// Blink (~L1201): each rowspan cell wants Σ spanned rows + (s-1) *
	// rowSpacing ≥ its minimum block-size. Compute the per-row
	// contribution to each cell's unmet deficit (proportional to row
	// block-size when any spanned row is non-zero, else even-split —
	// same rule as distributeRowspanCellToRows), then distribute
	// min(remaining, totalDeficit) in proportion to the accumulated
	// per-row deficit. Surplus flows to priority 3.
	if remaining > 0 && len(rowspanCells) > 0 {
		rowDeficit := make([]float64, len(rows))
		totalDeficit := 0.0
		for _, rc := range rowspanCells {
			if rc.rowIndex < 0 || rc.rowIndex >= len(rows) {
				continue
			}
			end := rc.rowIndex + rc.rowSpan
			if end > len(rows) {
				end = len(rows)
			}
			span := end - rc.rowIndex
			if span <= 1 {
				continue
			}
			current := 0.0
			for i := rc.rowIndex; i < end; i++ {
				current += rows[i].BlockSize
			}
			current += float64(span-1) * rowSpacing
			if rc.minBlock <= current {
				continue
			}
			cellDeficit := rc.minBlock - current
			totalRowBlock := 0.0
			for i := rc.rowIndex; i < end; i++ {
				totalRowBlock += rows[i].BlockSize
			}
			if totalRowBlock > 0 {
				for i := rc.rowIndex; i < end; i++ {
					share := cellDeficit * rows[i].BlockSize / totalRowBlock
					rowDeficit[i] += share
					totalDeficit += share
				}
			} else {
				per := cellDeficit / float64(span)
				for i := rc.rowIndex; i < end; i++ {
					rowDeficit[i] += per
					totalDeficit += per
				}
			}
		}

		if totalDeficit > 0 {
			toDistribute := remaining
			if toDistribute > totalDeficit {
				toDistribute = totalDeficit
			}
			// Last row with a non-zero deficit absorbs the rounding
			// remainder; a zero-deficit row must not pick it up.
			lastIdx := -1
			for i := range rows {
				if rowDeficit[i] > 0 {
					lastIdx = i
				}
			}
			accum := 0.0
			for i := range rows {
				if rowDeficit[i] <= 0 {
					continue
				}
				var share float64
				if i == lastIdx {
					share = toDistribute - accum
				} else {
					share = toDistribute * rowDeficit[i] / totalDeficit
					accum += share
				}
				rows[i].BlockSize += share
			}
			remaining -= toDistribute
		}
	}

	// Priority 3 — unconstrained non-empty rows.
	// Blink (~L1253): evenly distribute across rows with !IsConstrained
	// and BlockSize > 0. This is the case the legacy louis14 path
	// already handled ("auto-height rows").
	if remaining > 0 && len(unconstrainedIdx) > 0 {
		per := remaining / float64(len(unconstrainedIdx))
		accum := 0.0
		for k, i := range unconstrainedIdx {
			var share float64
			if k == len(unconstrainedIdx)-1 {
				share = remaining - accum
			} else {
				share = per
				accum += share
			}
			rows[i].BlockSize += share
		}
		remaining = 0
	}

	// Priority 4 — empty rows.
	// Blink (~L1286): when no unconstrained non-empty rows existed,
	// empty rows absorb the excess evenly.
	if remaining > 0 && len(emptyIdx) > 0 {
		per := remaining / float64(len(emptyIdx))
		accum := 0.0
		for k, i := range emptyIdx {
			var share float64
			if k == len(emptyIdx)-1 {
				share = remaining - accum
			} else {
				share = per
				accum += share
			}
			rows[i].BlockSize += share
		}
		remaining = 0
	}

	// Priority 5 — proportional fallback.
	// Blink (~L1318): every non-collapsed row gets a share proportional
	// to its current BlockSize, or evenly if all sizes are zero.
	if remaining > 0 && len(fallbackIdx) > 0 {
		totalSize := 0.0
		for _, i := range fallbackIdx {
			totalSize += rows[i].BlockSize
		}
		accum := 0.0
		if totalSize > 0 {
			for k, i := range fallbackIdx {
				var share float64
				if k == len(fallbackIdx)-1 {
					share = remaining - accum
				} else {
					share = remaining * rows[i].BlockSize / totalSize
					accum += share
				}
				rows[i].BlockSize += share
			}
		} else {
			per := remaining / float64(len(fallbackIdx))
			for k, i := range fallbackIdx {
				var share float64
				if k == len(fallbackIdx)-1 {
					share = remaining - accum
				} else {
					share = per
					accum += share
				}
				rows[i].BlockSize += share
			}
		}
	}
}
