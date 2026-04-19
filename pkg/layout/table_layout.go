package layout

import (
	"fmt"
	"strings"

	"louis14/pkg/css"
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
	node        *LayoutInputNode
	style       *css.Style
	colIndex    int
	colSpan     int
	rowSpan     int
	isAnonymous bool // true for non-table-structural children wrapped in anonymous cell boxes
}

// tableRow tracks a row during table layout.
type tableRow struct {
	node       *LayoutInputNode
	style      *css.Style
	groupStyle *css.Style // style of the containing row group (thead/tbody/tfoot), nil if none
	cells      []tableCell
}

// tableColWidth tracks an explicit column width from a col/colgroup element.
// Per CSS Tables, col/colgroup width properties establish column widths.
type tableColWidth struct {
	colIndex int
	span     int     // number of columns covered
	width    float64 // resolved width in pixels (0 = not set)
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
	// In border-collapse mode, resolve border conflicts and adjust cell styles
	// BEFORE computing column widths and laying out cells.
	// CSS 2.1 §17.6.2.1: each shared edge uses half the winning border's width.
	var collapsedStyles map[string]*css.Style
	if borderCollapse {
		grid := newCellBorderGrid(rows, numCols)
		// Collect row and row-group styles for element-type precedence.
		rowStyles := make([]*css.Style, len(rows))
		groupStyles := make([]*css.Style, len(rows))
		for i, row := range rows {
			rowStyles[i] = row.style
			groupStyles[i] = row.groupStyle
		}
		// Build per-column style slices for col/colgroup border resolution.
		colStyles := make([]*css.Style, numCols)
		colGroupStyles := make([]*css.Style, numCols)
		for _, cs := range colStyleSpecs {
			for i := 0; i < cs.span; i++ {
				idx := cs.colIndex + i
				if idx >= numCols {
					break
				}
				if cs.isGroup {
					if colGroupStyles[idx] == nil {
						colGroupStyles[idx] = cs.style
					}
				} else {
					if colStyles[idx] == nil {
						colStyles[idx] = cs.style
					}
				}
			}
		}
		collapsedStyles = grid.resolveCollapsedBorders(wdm, rowStyles, groupStyles, colStyles, colGroupStyles, tla.style)

		// Swap cell node styles to the collapsed versions so that
		// column width computation and cell layout use half-border widths.
		for rowIdx, row := range rows {
			for cellIdx := range row.cells {
				key := fmt.Sprintf("%d,%d", rowIdx, row.cells[cellIdx].colIndex)
				if cloned, ok := collapsedStyles[key]; ok {
					rows[rowIdx].cells[cellIdx].style = cloned
					if rows[rowIdx].cells[cellIdx].node != nil {
						rows[rowIdx].cells[cellIdx].node.style = cloned
					}
				}
			}
		}
	}

	// Choose column width algorithm: fixed (CSS 2.1 §17.5.2.1) or auto (§17.5.2.2).
	var colWidths []float64
	if tla.style.GetTableLayout() == css.TableLayoutFixed && hasExplicitTableWidth {
		colWidths = tla.computeColumnWidthsFixed(rows, numCols, availableForCols, spacingForCols, colWidthSpecs)
	} else {
		colWidths = tla.computeColumnWidths(rows, numCols, availableForCols, borderCollapse, hasExplicitTableWidth, colWidthSpecs)
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

	// Separate top and bottom captions.
	var topCaptions, bottomCaptions []tableCaption
	for _, cap := range captions {
		if cap.side == "bottom" {
			bottomCaptions = append(bottomCaptions, cap)
		} else {
			topCaptions = append(topCaptions, cap)
		}
	}

	// Layout top (block-start) captions.
	blockOffset := 0.0
	for _, cap := range topCaptions {
		capWDM := wdm
		if cap.style != nil {
			capWDM = NewWritingDirectionMode(cap.style)
		}
		capSpace := NewConstraintSpaceBuilder(wdm, capWDM, true).
			SetOrthogonalFallbackInlineSize(
				orthogonalFallbackSize(capWDM, tla.ctx)).
			SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
			SetAvailableSize(LogicalSize{
				InlineSize: availableInline,
				BlockSize:  Indefinite,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: availableInline,
				BlockSize:  0,
			}).
			SetPercentageResolutionInlineSize(availableInline).
			Build()

		capResult := layoutElement(tla.ctx, cap.node, capSpace)
		capLogical := NewLogicalFragment(wdm, capResult.Fragment)

		builder.AddChild(capResult.Fragment, LogicalOffset{
			InlineOffset: 0,
			BlockOffset:  blockOffset,
		})
		blockOffset += capLogical.BlockSize()
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
	//             stretching cells and applying §17.5.4 vertical-align
	//             inline (row block-size is known at cell-placement time).
	//
	// Rows and sections are structural-only (no NGLayoutResult of their own
	// in Blink); OOF candidates from cell content are collected directly at
	// the table builder at their final block-offsets.

	// Add block-start spacing before first row (if border-spacing is non-zero).
	if len(rows) > 0 && blockSpacing > 0 {
		blockOffset += blockSpacing
	}

	// Per-cell measurement data captured during Phase 1 and consumed by
	// Phase 3. contentBlockSize is the size used by the §17.5.4 check
	// (cell's intrinsic block-size for real cells; the row's intrinsic
	// height for anonymous-cell wrappers — their block margins fill the
	// row, so VA on the wrapped child is suppressed at the intrinsic size).
	type cellMeasurement struct {
		result           *LayoutResult
		inlineOffset     float64
		cell             tableCell
		contentBlockSize float64
		cellBlockOffset  float64 // block offset of the child inside an anon-cell wrapper
		// intrinsicBlockSize is the cell's own minimum block-size — the
		// value that contributes to row intrinsic for rowSpan == 1 cells,
		// and that DistributeRowspanCellToRows distributes across spanned
		// rows for rowSpan > 1 cells. Includes anon-cell child margins.
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

			// For anonymous cell wrappers, the child's inline margins reduce
			// the available width for content (the anonymous cell acts as a
			// block container whose content area = column width - child margins).
			// The child is then offset by its inline-start margin within the row.
			layoutWidth := cellWidth
			if cell.isAnonymous && cell.style != nil {
				childMargins := ResolveMargins(cell.style, wdm, cellWidth)
				layoutWidth -= childMargins.InlineStart + childMargins.InlineEnd
				if layoutWidth < 0 {
					layoutWidth = 0
				}
				cellInlineOffset += childMargins.InlineStart
			}

			// The column width is a fixed inline-size constraint from the table's
			// perspective. For parallel (same-axis) cells this becomes a fixed
			// InlineSize; for orthogonal cells the builder swaps it to
			// IsFixedBlockSize so the cell fills the column width in its block
			// direction (= the table's physical column width).
			//
			// The TableSectionData pointer threads the (soon-to-be-final)
			// row sizing vector into the cell sub-space, mirroring Blink's
			// TableSectionConstraintSpaceBuilder chain.
			cellSpace := NewConstraintSpaceBuilder(wdm, cellWDM, true).
				SetOrthogonalFallbackInlineSize(
					orthogonalFallbackSize(cellWDM, tla.ctx)).
				SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
				SetAvailableSize(LogicalSize{
					InlineSize: layoutWidth,
					BlockSize:  Indefinite,
				}).
				SetIsFixedInlineSize(true).
				SetPercentageResolutionSize(LogicalSize{
					InlineSize: layoutWidth,
					BlockSize:  0,
				}).
				SetPercentageResolutionInlineSize(layoutWidth).
				SetTableSectionData(tableData).
				SetTableSectionIndex(0).
				Build()

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
			cellBlockForRow := cellLogical.BlockSize() // table's block direction = physical height
			if isOrthogonalCell && colHasExplicit[colIdx] {
				// Use the cell's physical WIDTH (= column width) as the row height.
				// This produces square cells when col width is explicitly specified.
				cellBlockForRow = cellResult.Fragment.Size.Width
			}
			// For anonymous cell wrappers (non-table-structural children wrapped per
			// CSS Tables §2.1), the child's block margins must be included in the row
			// height. Real table-cell boxes suppress margins (CSS 2.1 §17.5.3), but
			// anonymous cell wrappers act as block containers — the child's margin-box
			// determines the cell's content height.
			if cell.isAnonymous && cell.style != nil {
				childMargins := ResolveMargins(cell.style, wdm, cellWidth)
				cellBlockForRow += childMargins.BlockStart + childMargins.BlockEnd
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

			// contentBlockSize: for real cells, the cell fragment's own
			// block-size; for anonymous wrappers, the row's intrinsic
			// height (assigned below after the row's max cell settles).
			// cellBlockOffset positions the wrapped child within the anon
			// cell to honor the child's block-start margin.
			contentBlockSize := cellLogical.BlockSize()
			cellBlockOffset := 0.0
			if cell.isAnonymous && cell.style != nil {
				childMargins := ResolveMargins(cell.style, wdm, cellLogical.InlineSize())
				cellBlockOffset = childMargins.BlockStart
			}

			cellLayouts = append(cellLayouts, cellMeasurement{
				result:             cellResult,
				inlineOffset:       cellInlineOffset,
				cell:               cell,
				contentBlockSize:   contentBlockSize,
				cellBlockOffset:    cellBlockOffset,
				intrinsicBlockSize: cellBlockForRow,
				rowIndex:           rowIdx,
			})
		}

		// Anonymous-cell wrappers: their contentBlockSize is the row's
		// intrinsic height, so the §17.5.4 pass does not shift the wrapped
		// child when the row does not grow past its intrinsic size. Matches
		// the legacy "no VA for anon at intrinsic height" behavior.
		for ci := range cellLayouts {
			if cellLayouts[ci].cell.isAnonymous && cellLayouts[ci].cell.style != nil {
				cellLayouts[ci].contentBlockSize = rowHeight
			}
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
	if geom.BorderBoxSize.BlockSize != Indefinite && len(rows) > 0 {
		explicitContentBlock := geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
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

	firstRowHeight := 0.0
	firstRowBaseline := 0.0  // max cell baseline in first VISIBLE row
	lastRowBaseline := 0.0   // max cell baseline in last VISIBLE row
	lastRowBlockOffset := 0.0 // block offset of last VISIBLE row
	emittedRows := 0          // count of visible (non-collapsed) rows emitted
	for rowIdx, row := range rows {
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

		// Add inter-row spacing only between VISIBLE rows: the
		// collapsed row's spacing is removed along with the row.
		if emittedRows > 0 && blockSpacing > 0 {
			blockOffset += blockSpacing
		}

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
				// after the row loop, with their fragment stretched
				// to the total spanned block-size.
				rowspanPending = append(rowspanPending, pendingRowspan{
					cl:          cl,
					blockOffset: blockOffset,
				})
				continue
			}

			cellFrag := cl.result.Fragment
			contentBlockSize := cl.contentBlockSize

			// §17.5.4: stretch the cell fragment to the final row
			// block-size and apply vertical-align. For anon-cell wrappers
			// contentBlockSize equals the row's intrinsic size, so this
			// only fires when distribution grew the row — matching the
			// legacy post-append VA sweep semantics.
			if contentBlockSize < rowHeight {
				cellLogical := NewLogicalFragment(wdm, cellFrag)
				cellFrag.Size = ToPhysicalSize(LogicalSize{
					InlineSize: cellLogical.InlineSize(),
					BlockSize:  rowHeight,
				}, wdm.WM)

				va := css.VerticalAlignMiddle // default for td/th
				if cl.cell.style != nil {
					va = cl.cell.style.GetVerticalAlign()
				}
				var blockShift float64
				switch va {
				case css.VerticalAlignMiddle:
					blockShift = (rowHeight - contentBlockSize) / 2
				case css.VerticalAlignBottom:
					blockShift = rowHeight - contentBlockSize
				}
				if blockShift > 0 {
					conv := NewConverter(wdm, cellFrag.Size)
					physShift := conv.ToPhysicalOffset(LogicalOffset{
						InlineOffset: 0,
						BlockOffset:  blockShift,
					}, PhysicalSize{})
					for cci := range cellFrag.Children {
						cellFrag.Children[cci].Offset.X += physShift.X
						cellFrag.Children[cci].Offset.Y += physShift.Y
					}
				}
			}

			rowBuilder.AddChild(cellFrag, LogicalOffset{
				InlineOffset: cl.inlineOffset,
				BlockOffset:  cl.cellBlockOffset,
			})

			// Collect OOF candidates from cell directly into the table builder.
			// blockOffset here is the row's FINAL block-offset (single-pass),
			// so OOF static positions reference the row's final location.
			if len(cl.result.PropagatedOOFCandidates) > 0 && cl.cell.style != nil {
				cellGeom := ComputeFragmentGeometry(cl.cell.style, wdm)
				inlineAdj := cl.inlineOffset + cellGeom.Border.InlineStart + cellGeom.Padding.InlineStart
				blockAdj := blockOffset + cellGeom.Border.BlockStart + cellGeom.Padding.BlockStart
				for _, cand := range cl.result.PropagatedOOFCandidates {
					adj := cand
					adj.StaticPosition.Offset.InlineOffset += inlineAdj
					adj.StaticPosition.Offset.BlockOffset += blockAdj
					builder.AddOutOfFlowCandidate(adj)
				}
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
		if row.style != nil {
			rowPhysBorder := ToPhysicalEdges(ComputeFragmentGeometry(row.style, wdm).Border, wdm)
			if borderCollapse {
				// In border-collapse mode, row borders participate in the
				// collapsed model — the row fragment must not paint them.
				rowPhysBorder = PhysicalEdges{}
			}
			physPadding := ToPhysicalEdges(ComputeFragmentGeometry(row.style, wdm).Padding, wdm)
			rowBuilder.SetBoxData(&PhysicalBoxData{
				Border:  rowPhysBorder,
				Padding: physPadding,
			})
		}

		rowResult := rowBuilder.Build()
		builder.AddChild(rowResult.Fragment, LogicalOffset{
			InlineOffset: 0,
			BlockOffset:  blockOffset,
		})

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

	// --- Phase 3b: place rowspan cells across spanned rows ---
	//
	// Each deferred rowspan cell stretches to
	//   Σ rows[R..R+s-1].BlockSize + (s-1) * blockSpacing
	// (mirrors Blink's TableSectionLayoutAlgorithm row-loop cell offset
	// calculation in core/layout/table/table_section_layout_algorithm.cc)
	// and attaches at the originating row's block-offset, just like a
	// rowSpan == 1 cell would. Because Rowspan B grew spRows so that
	// the spanned span ≥ cell intrinsic block-size, the stretch grows
	// the cell, never shrinks it.
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
		// are skipped; the cell fragment is unconditionally sized to
		// that reduced extent, and any child content that overflows is
		// clipped at paint time via ClipContentToBorderBox.
		spannedBlock := 0.0
		visibleSpanCount := 0
		spansCollapsedRow := false
		for i := rowIdx; i < end; i++ {
			if spRows[i].IsCollapsed {
				spansCollapsedRow = true
				continue
			}
			spannedBlock += spRows[i].BlockSize
			visibleSpanCount++
		}
		if visibleSpanCount > 1 && blockSpacing > 0 {
			spannedBlock += float64(visibleSpanCount-1) * blockSpacing
		}

		cellFrag := cl.result.Fragment
		cellLogical := NewLogicalFragment(wdm, cellFrag)
		contentBlockSize := cellLogical.BlockSize()

		// §17.5.4 + §5.4.1: always resize the cell fragment to
		// spannedBlock. When spannedBlock >= content (normal case),
		// apply vertical-align to position content within the row
		// extent. When spannedBlock < content (collapsed-row case),
		// shrink the fragment so its border-box matches the visible
		// spanned extent, anchor content at the block-start edge
		// (§5.4.1 "the top left content of the cell will continue to
		// show"), and flag the fragment for border-box clipping so
		// overflowing content is hidden.
		cellFrag.Size = ToPhysicalSize(LogicalSize{
			InlineSize: cellLogical.InlineSize(),
			BlockSize:  spannedBlock,
		}, wdm.WM)

		if contentBlockSize < spannedBlock {
			va := css.VerticalAlignMiddle // default for td/th
			if cl.cell.style != nil {
				va = cl.cell.style.GetVerticalAlign()
			}
			var blockShift float64
			switch va {
			case css.VerticalAlignMiddle:
				blockShift = (spannedBlock - contentBlockSize) / 2
			case css.VerticalAlignBottom:
				blockShift = spannedBlock - contentBlockSize
			}
			if blockShift > 0 {
				conv := NewConverter(wdm, cellFrag.Size)
				physShift := conv.ToPhysicalOffset(LogicalOffset{
					InlineOffset: 0,
					BlockOffset:  blockShift,
				}, PhysicalSize{})
				for cci := range cellFrag.Children {
					cellFrag.Children[cci].Offset.X += physShift.X
					cellFrag.Children[cci].Offset.Y += physShift.Y
				}
			}
		}
		if spansCollapsedRow {
			cellFrag.ClipContentToBorderBox = true
		}

		// Attach directly to the table builder at the originating
		// row's block-offset.
		builder.AddChild(cellFrag, LogicalOffset{
			InlineOffset: cl.inlineOffset,
			BlockOffset:  p.blockOffset + cl.cellBlockOffset,
		})

		// OOF candidates from rowspan cell content reference the
		// originating row's final block-offset (same convention as
		// non-rowspan cells in the row loop above).
		if len(cl.result.PropagatedOOFCandidates) > 0 && cl.cell.style != nil {
			cellGeom := ComputeFragmentGeometry(cl.cell.style, wdm)
			inlineAdj := cl.inlineOffset + cellGeom.Border.InlineStart + cellGeom.Padding.InlineStart
			blockAdj := p.blockOffset + cellGeom.Border.BlockStart + cellGeom.Padding.BlockStart
			for _, cand := range cl.result.PropagatedOOFCandidates {
				adj := cand
				adj.StaticPosition.Offset.InlineOffset += inlineAdj
				adj.StaticPosition.Offset.BlockOffset += blockAdj
				builder.AddOutOfFlowCandidate(adj)
			}
		}
	}

	// Add block-end spacing after last row.
	if len(rows) > 0 && blockSpacing > 0 {
		blockOffset += blockSpacing
	}

	// Layout bottom (block-end) captions.
	for _, cap := range bottomCaptions {
		capWDM := wdm
		if cap.style != nil {
			capWDM = NewWritingDirectionMode(cap.style)
		}
		capSpace := NewConstraintSpaceBuilder(wdm, capWDM, true).
			SetOrthogonalFallbackInlineSize(
				orthogonalFallbackSize(capWDM, tla.ctx)).
			SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
			SetAvailableSize(LogicalSize{
				InlineSize: availableInline,
				BlockSize:  Indefinite,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: availableInline,
				BlockSize:  0,
			}).
			SetPercentageResolutionInlineSize(availableInline).
			Build()

		capResult := layoutElement(tla.ctx, cap.node, capSpace)
		capLogical := NewLogicalFragment(wdm, capResult.Fragment)

		builder.AddChild(capResult.Fragment, LogicalOffset{
			InlineOffset: 0,
			BlockOffset:  blockOffset,
		})
		blockOffset += capLogical.BlockSize()
	}

	// Compute table size.
	contentInlineSize := 0.0
	for _, w := range colWidths {
		contentInlineSize += w
	}
	contentInlineSize += spacingForCols // include inline spacing in total
	finalBlockSize := blockOffset

	// CSS 2.1 §17.5.3: honor an explicit table block-size as a floor, even
	// when the table has no rows (or fewer rows than the specified height
	// requires). The per-row distribution above (line 489) only runs when
	// len(rows) > 0; for empty tables we still need to apply the specified
	// height so `<table style="height:30px"></table>` renders 30px tall.
	borderBoxBlock := finalBlockSize + geom.BlockBorderPadding()
	if geom.BorderBoxSize.BlockSize != Indefinite &&
		geom.BorderBoxSize.BlockSize > borderBoxBlock {
		borderBoxBlock = geom.BorderBoxSize.BlockSize
	}

	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
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
		} else if firstRowHeight > 0 {
			builder.SetLastBaseline(borderPaddingStart + firstRowHeight/2)
		}
	}

	physBorder := ToPhysicalEdges(geom.Border, wdm) // already zeroed for border-collapse
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(tla.style, wdm, tla.space.AvailableSize.InlineSize), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:  physMargin,
		Border:  physBorder,
		Padding: physPadding,
	})

	// Mirrors Blink's single NGOutOfFlowLayoutPart::Run() at the end of
	// NGTableLayoutAlgorithm::Layout(). Positioned tables resolve absolute
	// candidates but propagate fixed candidates toward the ICB.
	var propagatedOOF []OutOfFlowCandidate
	if len(builder.outOfFlowCandidates) > 0 {
		isPositioned := tla.style != nil && tla.style.GetPosition() != css.PositionStatic

		if isPositioned {
			var absoluteCandidates, fixedCandidates []OutOfFlowCandidate
			for _, cand := range builder.outOfFlowCandidates {
				if cand.IsFixedPosition {
					fixedCandidates = append(fixedCandidates, cand)
				} else {
					absoluteCandidates = append(absoluteCandidates, cand)
				}
			}
			if len(absoluteCandidates) > 0 {
				oofPart := &OutOfFlowLayoutPart{
					ctx:                 tla.ctx,
					containingBlockWDM:  wdm,
					containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
					geom:                geom,
				}
				oofPart.LayoutCandidates(absoluteCandidates, builder)
			}
			propagatedOOF = fixedCandidates
		} else {
			propagatedOOF = builder.outOfFlowCandidates
		}
	}

	result := builder.Build()

	if len(propagatedOOF) > 0 {
		result.PropagatedOOFCandidates = propagatedOOF
	}

	return result
}

// collectRowsAndCaptions extracts table rows, captions, and col/colgroup width specs
// from the table's children, handling row-groups (thead, tbody, tfoot).
// CSS 2.1 §17.5: rendering order is thead, tbodies (in source order), tfoot.
// Also returns column width specifications and border styles from col/colgroup elements.
func (tla *TableLayoutAlgorithm) collectRowsAndCaptions() ([]tableRow, []tableCaption, []tableColWidth, []tableColStyle) {
	var headerRows, bodyRows, footerRows []tableRow
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
			if w, ok := childStyle.GetLength("width"); ok && w > 0 {
				colWidths = append(colWidths, tableColWidth{
					colIndex: colIdx,
					span:     span,
					width:    w,
				})
			}
			// Collect style for border-collapse resolution (CSS 2.1 §17.6.2.1).
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
					if w, ok := colChildStyle.GetLength("width"); ok && w > 0 {
						colWidths = append(colWidths, tableColWidth{
							colIndex: colChildIdx,
							span:     colChildSpan,
							width:    w,
						})
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
					groupRows = append(groupRows, r)
				}
			}
			switch display {
			case css.DisplayTableHeaderGroup:
				headerRows = append(headerRows, groupRows...)
			case css.DisplayTableFooterGroup:
				footerRows = append(footerRows, groupRows...)
			default:
				bodyRows = append(bodyRows, groupRows...)
			}
		}
	}

	// Render order: thead → tbody sections → tfoot.
	rows := make([]tableRow, 0, len(headerRows)+len(bodyRows)+len(footerRows))
	rows = append(rows, headerRows...)
	rows = append(rows, bodyRows...)
	rows = append(rows, footerRows...)
	return rows, captions, colWidths, colStyleSpecs
}


// buildRow extracts cells from a table-row element. After tree-build
// normalization (LayoutTreeBuilder.wrapAnonymousTableBoxes), every row
// child is a table-cell — either authored or an anonymous wrapper around
// stray content. Anonymous and authored cells are both layout-time cells;
// only the isAnonymous flag disambiguates for the row-height / margin
// accounting in Layout().
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

		// For anonymous cell wrappers holding a single block element,
		// expose that inner block directly as the cell's node/style.
		// This preserves the pre-refactor semantics where a bare block
		// inside a <table> contributed its margins to the row height
		// via the special cell.isAnonymous path (lines ~288/340 in
		// Layout()). Without this unwrap, cell.style would be the
		// anon-cell's (margin-free) style and those margins would be
		// lost.
		cellNode := child
		cellStyleUsed := childStyle
		isAnon := child.IsAnonymous()
		if isAnon {
			if inner := singleBlockInnerChild(child); inner != nil {
				cellNode = inner
				cellStyleUsed = inner.Style()
			}
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
			node:        cellNode,
			style:       cellStyleUsed,
			colIndex:    colIdx, // provisional; assignColumnIndices overwrites
			colSpan:     colSpan,
			rowSpan:     rowSpan,
			isAnonymous: isAnon,
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

// singleBlockInnerChild returns the sole non-text, non-whitespace child of
// an anonymous cell wrapper if exactly one exists and it is a block-level
// element — otherwise nil. Used by buildRow to preserve margin-propagation
// semantics for bare blocks wrapped at tree-build time.
func singleBlockInnerChild(wrapper *LayoutInputNode) *LayoutInputNode {
	var found *LayoutInputNode
	for _, c := range wrapper.Children() {
		if c.IsText() {
			if strings.TrimSpace(c.TextContent()) == "" {
				continue
			}
			return nil // non-whitespace text ⇒ inline content, keep wrapper
		}
		s := c.Style()
		if s == nil {
			return nil
		}
		// Inline-level children stay inside the wrapper (multi-inline runs
		// or mixed inline/block can't be collapsed to a single content box).
		d := s.GetDisplay()
		switch d {
		case css.DisplayBlock, css.DisplayFlex, css.DisplayFlowRoot,
			css.DisplayGrid, css.DisplayTable, css.DisplayListItem:
			// ok
		default:
			return nil
		}
		if found != nil {
			return nil // more than one block child
		}
		found = c
	}
	return found
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
		if cws.width > 0 {
			for s := 0; s < cws.span; s++ {
				ci := cws.colIndex + s
				if ci < numCols && !hasExplicit[ci] {
					colWidths[ci] = cws.width
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

// computeColumnWidths computes column widths using the auto table layout
// algorithm (CSS 2.1 §17.5.2).
func (tla *TableLayoutAlgorithm) computeColumnWidths(
	rows []tableRow, numCols int, availableInline float64, borderCollapse bool, hasExplicitWidth bool,
	colWidthSpecs []tableColWidth,
) []float64 {
	colWidths := make([]float64, numCols)

	// First pass: compute each column's min/max content width.
	colMin := make([]float64, numCols)
	colMax := make([]float64, numCols)

	// Track which columns have explicit col/colgroup widths.
	// When a column has an explicit col width, that width is authoritative:
	// intrinsic cell sizing (which may return the cell's logical inline-size
	// instead of the table's column width for orthogonal cells) is skipped.
	colHasExplicit := make([]bool, numCols)

	// Apply col/colgroup explicit widths first (CSS Tables §9.1).
	// These establish the column widths from the column model.
	for _, cws := range colWidthSpecs {
		for s := 0; s < cws.span; s++ {
			ci := cws.colIndex + s
			if ci >= numCols {
				break
			}
			if cws.width > colMin[ci] {
				colMin[ci] = cws.width
			}
			if cws.width > colMax[ci] {
				colMax[ci] = cws.width
			}
			colHasExplicit[ci] = true
		}
	}

	for _, row := range rows {
		for _, cell := range row.cells {
			colIdx := cell.colIndex
			if colIdx >= numCols {
				continue
			}

			// Check for explicit inline-size on the cell from the TABLE's perspective.
			// Column widths are the table's inline dimension:
			//   HTB table: inline=horizontal → use CSS "width"
			//   VLR/VRL table: inline=vertical → use CSS "height"
			// For orthogonal cells (cell WM differs from table WM), the cell's physical
			// "width" equals the table's inline extent regardless of writing mode.
			explicitW := 0.0
			hasExplicit := false
			if cell.style != nil {
				inlineProp := "width"
				if tla.space.WritingDirection.IsVertical() {
					inlineProp = "height"
				}
				if w, ok := cell.style.GetLength(inlineProp); ok && w > 0 {
					explicitW = w
					hasExplicit = true
				}
			}

			if cell.colSpan == 1 {
				if hasExplicit {
					// explicitW is a content-box width; column sizing uses border-box.
					// If the cell has box-sizing:border-box the value already includes BP;
					// otherwise add the cell's inline border+padding (from the table's axis).
					explicitBB := explicitW
					if cell.style == nil || cell.style.GetBoxSizing() != "border-box" {
						cellGeom := ComputeFragmentGeometry(cell.style, tla.space.WritingDirection)
						explicitBB += cellGeom.InlineBorderPadding()
					}
					if explicitBB > colMin[colIdx] {
						colMin[colIdx] = explicitBB
					}
					if explicitBB > colMax[colIdx] {
						colMax[colIdx] = explicitBB
					}
				} else {
					// Compute intrinsic size.
					// For orthogonal cells (e.g. vertical-rl cell in horizontal table),
					// ComputeMinMaxSizes returns the cell's LOGICAL inline sizes (from the
					// cell's own WDM perspective), which equals the TABLE's BLOCK direction
					// (not the column width direction). However, this "accidental" behavior
					// produces correct column widths when no explicit col width is given,
					// because the cell's logical inline-size (= physical height for vertical-rl)
					// represents the cell's contribution to column sizing in those cases.
					//
					// When an explicit col/colgroup width IS provided, it takes priority
					// and we skip intrinsic sizing for orthogonal cells to avoid overriding it.
					childWDM := tla.space.WritingDirection
					if cell.style != nil {
						childWDM = NewWritingDirectionMode(cell.style)
					}
					isOrthogonal := tla.space.WritingDirection.IsVertical() != childWDM.IsVertical()
					skipIntrinsic := isOrthogonal && colHasExplicit[colIdx]
					if !skipIntrinsic {
						childSpace := NewConstraintSpaceBuilder(tla.space.WritingDirection, childWDM, true).
							SetOrthogonalFallbackInlineSize(
								orthogonalFallbackSize(childWDM, tla.ctx)).
							SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
							SetAvailableSize(LogicalSize{InlineSize: availableInline, BlockSize: Indefinite}).
							SetPercentageResolutionInlineSize(tla.space.PercentageResolutionInlineSize).
							Build()
						mm := ComputeMinMaxSizes(tla.ctx, cell.node, childSpace)
						// Convert content-box to border-box for column sizing.
						cellGeom := ComputeFragmentGeometry(cell.style, childWDM)
						cellBP := cellGeom.InlineBorderPadding()
						cellMin := mm.MinContent + cellBP
						cellMax := mm.MaxContent + cellBP
						if cellMin > colMin[colIdx] {
							colMin[colIdx] = cellMin
						}
						if cellMax > colMax[colIdx] {
							colMax[colIdx] = cellMax
						}
					}
				}
			}
		}
	}

	// Distribute available width.
	totalMin := 0.0
	for _, w := range colMin {
		totalMin += w
	}

	if totalMin >= availableInline {
		// Not enough room — use min widths.
		copy(colWidths, colMin)
	} else if !hasExplicitWidth {
		// CSS 2.1 §17.5.2: Auto-width tables shrink to fit.
		// Use max-content widths, capped at available inline size.
		totalMax := 0.0
		for _, w := range colMax {
			totalMax += w
		}
		if totalMax <= availableInline {
			copy(colWidths, colMax)
		} else {
			// Scale down proportionally.
			extra := availableInline - totalMin
			maxExtra := totalMax - totalMin
			for i := 0; i < numCols; i++ {
				share := 0.0
				if maxExtra > 0 {
					share = (colMax[i] - colMin[i]) / maxExtra * extra
				}
				colWidths[i] = colMin[i] + share
			}
		}
	} else {
		// Explicit width: distribute available space.
		totalMax := 0.0
		for _, w := range colMax {
			totalMax += w
		}
		if totalMax <= availableInline {
			// Everything fits at max — distribute extra evenly.
			copy(colWidths, colMax)
			remaining := availableInline - totalMax
			if remaining > 0 && numCols > 0 {
				each := remaining / float64(numCols)
				for i := range colWidths {
					colWidths[i] += each
				}
			}
		} else {
			// Between min and max — distribute proportionally.
			extra := availableInline - totalMin
			maxExtra := totalMax - totalMin
			for i := 0; i < numCols; i++ {
				share := 0.0
				if maxExtra > 0 {
					share = (colMax[i] - colMin[i]) / maxExtra * extra
				}
				colWidths[i] = colMin[i] + share
			}
		}
	}

	return colWidths
}

// tableCaption tracks a caption element during table layout.
type tableCaption struct {
	node  *LayoutInputNode
	style *css.Style
	side  string // "top" or "bottom"
}

// logicalBorderSpacing returns border spacing mapped to table logical coordinates.
// CSS border-spacing first value = horizontal = inline spacing (between columns).
// CSS border-spacing second value = vertical = block spacing (between rows).
// Per CSS Writing Modes 3 §7.2, the first value maps to the inline dimension and
// the second to the block dimension across all writing modes.
//
// We resolve em/rem units using the table's computed font-size rather than
// relying on GetBorderSpacing() which uses a default 16px.
func (tla *TableLayoutAlgorithm) logicalBorderSpacing() (inlineSpacing, blockSpacing float64) {
	val, ok := tla.style.Get("border-spacing")
	if !ok {
		return 0, 0
	}
	fontSize := tla.style.GetFontSize()
	parts := strings.Fields(val)
	if len(parts) >= 1 {
		if v, ok := css.ParseLengthWithFontSize(parts[0], fontSize); ok {
			inlineSpacing = v
		}
	}
	if len(parts) >= 2 {
		if v, ok := css.ParseLengthWithFontSize(parts[1], fontSize); ok {
			blockSpacing = v
		}
	} else {
		blockSpacing = inlineSpacing // single value applies to both
	}
	return
}

// borderEdgeInfo holds a single border edge's resolved properties.
type borderEdgeInfo struct {
	width float64
	style css.BorderStyle
	color css.Color
}

// physicalSideNames maps logical edges to physical CSS property side names
// based on writing mode and direction.
// Returns (inlineStart, inlineEnd, blockStart, blockEnd) as "top"/"right"/"bottom"/"left".
func physicalSideNames(wdm WritingDirectionMode) (inlineStart, inlineEnd, blockStart, blockEnd string) {
	switch wdm.WM {
	case WritingModeHorizontalTB:
		if wdm.Dir == DirectionLTR {
			return "left", "right", "top", "bottom"
		}
		return "right", "left", "top", "bottom"
	case WritingModeVerticalLR, WritingModeSidewaysLR:
		if wdm.Dir == DirectionLTR {
			return "top", "bottom", "left", "right"
		}
		return "bottom", "top", "left", "right"
	case WritingModeVerticalRL, WritingModeSidewaysRL:
		if wdm.Dir == DirectionLTR {
			return "top", "bottom", "right", "left"
		}
		return "bottom", "top", "right", "left"
	}
	return "left", "right", "top", "bottom"
}

// readBorderEdge reads a border edge (width, style, color) from a CSS style
// for a given physical side ("top", "right", "bottom", "left").
func readBorderEdge(s *css.Style, side string) borderEdgeInfo {
	if s == nil {
		return borderEdgeInfo{}
	}
	styleProp := "border-" + side + "-style"
	widthProp := "border-" + side + "-width"
	colorProp := "border-" + side + "-color"

	var info borderEdgeInfo
	info.style = css.BorderStyleNone
	if sv, ok := s.Get(styleProp); ok {
		switch sv {
		case "solid":
			info.style = css.BorderStyleSolid
		case "double":
			info.style = css.BorderStyleDouble
		case "dashed":
			info.style = css.BorderStyleDashed
		case "dotted":
			info.style = css.BorderStyleDotted
		case "none", "hidden":
			info.style = css.BorderStyle(sv)
		default:
			info.style = css.BorderStyleSolid
		}
	}

	if info.style == css.BorderStyleNone || info.style == "hidden" {
		info.width = 0
	} else if wv, ok := s.Get(widthProp); ok {
		if v, ok := css.ParseLengthWithFontSize(wv, s.GetFontSize()); ok {
			info.width = v
		}
	}

	// Read color from longhand (e.g., border-top-color), fall back to currentColor.
	currentColor := css.Color{R: 0, G: 0, B: 0, A: 1.0}
	if cv, ok := s.Get("color"); ok {
		if c, ok := css.ParseColor(cv); ok {
			currentColor = c
		}
	}

	info.color = currentColor // default
	if cv, ok := s.Get(colorProp); ok {
		if c, ok := css.ParseColor(cv); ok {
			info.color = c
		}
	}

	return info
}

// borderStylePrecedence returns a numeric precedence for border style
// per CSS 2.1 §17.6.2.1: double > solid > dashed > dotted > ridge > outset > groove > inset > none.
func borderStylePrecedence(s css.BorderStyle) int {
	switch s {
	case "hidden":
		return 100 // hidden always wins
	case css.BorderStyleDouble:
		return 9
	case css.BorderStyleSolid:
		return 8
	case css.BorderStyleDashed:
		return 7
	case css.BorderStyleDotted:
		return 6
	case "ridge":
		return 5
	case "outset":
		return 4
	case "groove":
		return 3
	case "inset":
		return 2
	case css.BorderStyleNone:
		return 0
	}
	return 1
}

// resolveBorderConflict applies CSS 2.1 §17.6.2.1 to determine which border wins.
// startCellWins is true if cellA is the inline-start or block-start cell.
// Returns the winning border.
func resolveBorderConflict(a, b borderEdgeInfo, startCellWins bool) borderEdgeInfo {
	// Rule 1: hidden wins (becomes no border).
	if a.style == "hidden" || b.style == "hidden" {
		return borderEdgeInfo{style: css.BorderStyleNone, width: 0}
	}

	// Rule 2: none loses.
	aIsNone := a.style == css.BorderStyleNone || a.width == 0
	bIsNone := b.style == css.BorderStyleNone || b.width == 0
	if aIsNone && bIsNone {
		return borderEdgeInfo{style: css.BorderStyleNone, width: 0}
	}
	if aIsNone {
		return b
	}
	if bIsNone {
		return a
	}

	// Rule 3: wider wins.
	if a.width > b.width {
		return a
	}
	if b.width > a.width {
		return b
	}

	// Rule 4: style precedence (same width).
	aPrecedence := borderStylePrecedence(a.style)
	bPrecedence := borderStylePrecedence(b.style)
	if aPrecedence > bPrecedence {
		return a
	}
	if bPrecedence > aPrecedence {
		return b
	}

	// Rule 5: same type — start cell wins.
	// (We only handle cell-to-cell conflicts; no row/column/table precedence yet.)
	if startCellWins {
		return a
	}
	return b
}

// cellBorderGrid stores border info for each cell in the grid for conflict resolution.
type cellBorderGrid struct {
	numRows int
	numCols int
	// grid[row][col] stores the cell's style, or nil if empty.
	styles [][]*css.Style
}

func newCellBorderGrid(rows []tableRow, numCols int) *cellBorderGrid {
	g := &cellBorderGrid{
		numRows: len(rows),
		numCols: numCols,
		styles:  make([][]*css.Style, len(rows)),
	}
	for rowIdx, row := range rows {
		g.styles[rowIdx] = make([]*css.Style, numCols)
		for _, cell := range row.cells {
			if cell.colIndex < numCols {
				g.styles[rowIdx][cell.colIndex] = cell.style
			}
		}
	}
	return g
}

// resolveCollapsedBorders computes half-border widths and winning colors for
// all cells in the grid. Returns a map from "row,col" to a cloned style with
// adjusted border properties. Only cells with modified borders are included.
//
// CSS 2.1 §17.6.2.1 precedence (highest first): cell > row > row-group > col > col-group > table.
func (g *cellBorderGrid) resolveCollapsedBorders(wdm WritingDirectionMode, rowStyles, groupStyles, colStyles, colGroupStyles []*css.Style, tableStyle *css.Style) map[string]*css.Style {
	iStart, iEnd, bStart, bEnd := physicalSideNames(wdm)
	result := make(map[string]*css.Style)

	// Helper to ensure a cloned style exists for a cell.
	getClone := func(row, col int) *css.Style {
		key := fmt.Sprintf("%d,%d", row, col)
		if s, ok := result[key]; ok {
			return s
		}
		orig := g.styles[row][col]
		if orig == nil {
			return nil
		}
		clone := orig.Clone()
		result[key] = clone
		return clone
	}

	// Resolve inline-direction edges (between cells in the same row).
	for row := 0; row < g.numRows; row++ {
		for col := 0; col < g.numCols-1; col++ {
			sA := g.styles[row][col]
			sB := g.styles[row][col+1]
			if sA == nil && sB == nil {
				continue
			}

			// Cell A's inline-end border vs Cell B's inline-start border.
			edgeA := readBorderEdge(sA, iEnd)
			edgeB := readBorderEdge(sB, iStart)

			if edgeA.width == 0 && edgeB.width == 0 {
				continue
			}

			// CSS Writing Modes 3 §6.2 + CSS 2.1 §17.6.2.1: the inline-start
			// cell always wins the tiebreaker. For LTR the tiebreaker is
			// "line-left" and for RTL it's "line-right"; in both cases,
			// inline-start is where cell A (lower index) lives.
			aIsStart := true

			winner := resolveBorderConflict(edgeA, edgeB, aIsStart)
			halfWidth := winner.width / 2

			// Apply to cell A's inline-end.
			if sA != nil {
				clone := getClone(row, col)
				if clone != nil {
					setBorderEdge(clone, iEnd, halfWidth, winner.style, winner.color)
				}
			}
			// Apply to cell B's inline-start.
			if sB != nil {
				clone := getClone(row, col+1)
				if clone != nil {
					setBorderEdge(clone, iStart, halfWidth, winner.style, winner.color)
				}
			}
		}
	}

	// Resolve block-direction edges (between cells in adjacent rows).
	for row := 0; row < g.numRows-1; row++ {
		for col := 0; col < g.numCols; col++ {
			sA := g.styles[row][col]
			sB := g.styles[row+1][col]
			if sA == nil && sB == nil {
				continue
			}

			// Cell A's block-end border vs Cell B's block-start border.
			edgeA := readBorderEdge(sA, bEnd)
			edgeB := readBorderEdge(sB, bStart)

			if edgeA.width == 0 && edgeB.width == 0 {
				continue
			}

			// CSS 2.1 §17.6.2.1: block-start cell always wins the tiebreaker.
			// "Further to the top" in horizontal-tb maps to "further to block-start"
			// in all writing modes. A (lower row index) is block-start.
			winner := resolveBorderConflict(edgeA, edgeB, true)
			halfWidth := winner.width / 2

			if sA != nil {
				clone := getClone(row, col)
				if clone != nil {
					setBorderEdge(clone, bEnd, halfWidth, winner.style, winner.color)
				}
			}
			if sB != nil {
				clone := getClone(row+1, col)
				if clone != nil {
					setBorderEdge(clone, bStart, halfWidth, winner.style, winner.color)
				}
			}
		}
	}

	// Outer edges (edges at the table boundary) keep their full width —
	// they have no adjacent cell to share with. Only the border color
	// needs to be set on the clone (the width stays as-is from the
	// original computed style). No halving is needed for outer borders.

	// CSS 2.1 §17.6.2.1 element-type precedence: merge borders from
	// parent elements (table → rowgroup → row) into cell borders.
	// Processed in reverse precedence order; cell borders were written
	// first, so they win ties via "first writer wins" in the conflict
	// resolver (startCellWins=true means the existing cell border wins).
	allSides := [4]string{iStart, iEnd, bStart, bEnd}

	// mergeElementBorder resolves a single edge of a cell against an
	// element (row/rowgroup/table) border. The cell's existing border
	// wins on equal width+style (higher CSS precedence).
	mergeElementBorder := func(row, col int, side string, elemEdge borderEdgeInfo) {
		if elemEdge.width == 0 && elemEdge.style == css.BorderStyleNone {
			return
		}
		cellStyle := g.styles[row][col]
		if cellStyle == nil {
			return
		}
		// Read the cell's current (possibly already resolved) border.
		clone := getClone(row, col)
		if clone == nil {
			return
		}
		cellEdge := readBorderEdge(clone, side)

		// Cell (A) wins ties; element (B) only wins if strictly wider
		// or more prominent style.
		winner := resolveBorderConflict(cellEdge, elemEdge, true)

		// Only update if the element border actually won.
		if winner.color != cellEdge.color || winner.width != cellEdge.width || winner.style != cellEdge.style {
			// For outer edges, keep full width; for inner edges, the width
			// was already halved during cell-to-cell resolution.
			setBorderEdge(clone, side, winner.width, winner.style, winner.color)
		}
	}

	// 1. Merge row (tr) borders — lower precedence than cell.
	for row := 0; row < g.numRows; row++ {
		rs := rowStyles[row]
		if rs == nil {
			continue
		}
		for _, side := range allSides {
			elemEdge := readBorderEdge(rs, side)
			if elemEdge.width == 0 && elemEdge.style == css.BorderStyleNone {
				continue
			}
			for col := 0; col < g.numCols; col++ {
				mergeElementBorder(row, col, side, elemEdge)
			}
		}
	}

	// 2. Merge row group (thead/tbody/tfoot) borders — lower than row.
	for row := 0; row < g.numRows; row++ {
		gs := groupStyles[row]
		if gs == nil {
			continue
		}
		for _, side := range allSides {
			elemEdge := readBorderEdge(gs, side)
			if elemEdge.width == 0 && elemEdge.style == css.BorderStyleNone {
				continue
			}
			for col := 0; col < g.numCols; col++ {
				mergeElementBorder(row, col, side, elemEdge)
			}
		}
	}

	// 3. Merge column (col) borders — lower precedence than row-group.
	for col := 0; col < g.numCols; col++ {
		cs := colStyles[col]
		if cs == nil {
			continue
		}
		for _, side := range allSides {
			elemEdge := readBorderEdge(cs, side)
			if elemEdge.width == 0 && elemEdge.style == css.BorderStyleNone {
				continue
			}
			for row := 0; row < g.numRows; row++ {
				mergeElementBorder(row, col, side, elemEdge)
			}
		}
	}

	// 4. Merge column-group (colgroup) borders — lower precedence than col.
	for col := 0; col < g.numCols; col++ {
		cgs := colGroupStyles[col]
		if cgs == nil {
			continue
		}
		for _, side := range allSides {
			elemEdge := readBorderEdge(cgs, side)
			if elemEdge.width == 0 && elemEdge.style == css.BorderStyleNone {
				continue
			}
			for row := 0; row < g.numRows; row++ {
				mergeElementBorder(row, col, side, elemEdge)
			}
		}
	}

	// 5. Merge table borders — lowest precedence.
	if tableStyle != nil {
		for _, side := range allSides {
			elemEdge := readBorderEdge(tableStyle, side)
			if elemEdge.width == 0 && elemEdge.style == css.BorderStyleNone {
				continue
			}
			for row := 0; row < g.numRows; row++ {
				for col := 0; col < g.numCols; col++ {
					mergeElementBorder(row, col, side, elemEdge)
				}
			}
		}
	}

	return result
}

// setBorderEdge sets a specific physical border edge on a cloned style.
func setBorderEdge(s *css.Style, side string, width float64, style css.BorderStyle, color css.Color) {
	s.Properties["border-"+side+"-width"] = fmt.Sprintf("%.6gpx", width)
	s.Properties["border-"+side+"-style"] = string(style)
	s.Properties["border-"+side+"-color"] = fmt.Sprintf("rgb(%d,%d,%d)", color.R, color.G, color.B)
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
			prop := "height"
			if wdm.IsVertical() {
				prop = "width"
			}
			if pct, ok := s.GetPercentage(prop); ok && pct > 0 {
				row.Percent = pct
			}
			if _, ok := s.GetLength(prop); ok {
				row.IsConstrained = true
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
//                assignColumnIndices's slot grid; we re-clamp defensively
//                so this helper is safe to call directly).
//   - cellBlock: the cell's intrinsic block-size (the value that would
//                otherwise have been folded into the originating row's
//                intrinsic max in the rowSpan == 1 path).
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

