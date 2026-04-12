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

// Layout performs table layout and returns the result.
func (tla *TableLayoutAlgorithm) Layout() *LayoutResult {
	wdm := tla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(tla.ctx, tla.node, tla.style, wdm, tla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(tla.node)

	// Collect rows, captions, and col/colgroup widths from the table's children.
	rows, captions, colWidthSpecs := tla.collectRowsAndCaptions()

	// Determine the number of columns.
	numCols := 0
	for _, row := range rows {
		colCount := 0
		for _, cell := range row.cells {
			colCount += cell.colSpan
		}
		if colCount > numCols {
			numCols = colCount
		}
	}
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
		collapsedStyles = grid.resolveCollapsedBorders(wdm, rowStyles, groupStyles, tla.style)

		// Swap cell node styles to the collapsed versions so that
		// column width computation and cell layout use half-border widths.
		for rowIdx, row := range rows {
			colIdx := 0
			for cellIdx := range row.cells {
				key := fmt.Sprintf("%d,%d", rowIdx, colIdx)
				if cloned, ok := collapsedStyles[key]; ok {
					rows[rowIdx].cells[cellIdx].style = cloned
					if rows[rowIdx].cells[cellIdx].node != nil {
						rows[rowIdx].cells[cellIdx].node.style = cloned
					}
				}
				colIdx += row.cells[cellIdx].colSpan
			}
		}
	}

	// Choose column width algorithm: fixed (CSS 2.1 §17.5.2.1) or auto (§17.5.2.2).
	var colWidths []float64
	if tla.style.GetTableLayout() == css.TableLayoutFixed && hasExplicitTableWidth {
		colWidths = tla.computeColumnWidthsFixed(rows, numCols, availableForCols, spacingForCols)
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

	// Layout each row.
	//
	// Mirrors Blink's NGTableLayoutAlgorithm: rows and sections are
	// structural-only — they do NOT participate in OOF propagation.
	// OOF candidates from cell content are collected directly at the
	// table level. Rows produce synthetic fragments for painting but
	// have no NGLayoutResult of their own in Blink.

	// Add block-start spacing before first row (if border-spacing is non-zero).
	if len(rows) > 0 && blockSpacing > 0 {
		blockOffset += blockSpacing
	}

	firstRowHeight := 0.0
	for rowIdx, row := range rows {
		// Add inter-row spacing (block spacing) between rows.
		if rowIdx > 0 && blockSpacing > 0 {
			blockOffset += blockSpacing
		}

		rowHeight := 0.0
		colIdx := 0

		// Create synthetic row fragment (structural only, no OOF role).
		rowBuilder := NewBoxFragmentBuilder(wdm)
		if row.node != nil {
			rowBuilder.SetLayoutNode(row.node)
		}

		// Two-pass cell layout: first lay out all cells to find row height,
		// then stretch cells to fill the row (CSS 2.1 §17.5.3).
		type cellLayoutInfo struct {
			result       *LayoutResult
			inlineOffset float64
			cell         tableCell
		}
		cellLayouts := make([]cellLayoutInfo, 0, len(row.cells))

		for _, cell := range row.cells {
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
			// The column width is a fixed inline-size constraint from the table's
			// perspective. For parallel (same-axis) cells this becomes a fixed
			// InlineSize; for orthogonal cells the builder swaps it to
			// IsFixedBlockSize so the cell fills the column width in its block
			// direction (= the table's physical column width).
			cellSpace := NewConstraintSpaceBuilder(wdm, cellWDM, true).
				SetOrthogonalFallbackInlineSize(
					orthogonalFallbackSize(cellWDM, tla.ctx)).
				SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
				SetAvailableSize(LogicalSize{
					InlineSize: cellWidth,
					BlockSize:  Indefinite,
				}).
				SetIsFixedInlineSize(true).
				SetPercentageResolutionSize(LogicalSize{
					InlineSize: cellWidth,
					BlockSize:  0,
				}).
				SetPercentageResolutionInlineSize(cellWidth).
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
			if cellBlockForRow > rowHeight {
				rowHeight = cellBlockForRow
			}

			cellLayouts = append(cellLayouts, cellLayoutInfo{
				result:       cellResult,
				inlineOffset: cellInlineOffset,
				cell:         cell,
			})

			colIdx += cell.colSpan
		}

		// Second pass: stretch cells to row height and add to rowBuilder.
		for _, cl := range cellLayouts {
			cellLogical := NewLogicalFragment(wdm, cl.result.Fragment)
			if cellLogical.BlockSize() < rowHeight {
				// Stretch cell to fill row height. Convert rowHeight to physical size.
				physSize := ToPhysicalSize(LogicalSize{
					InlineSize: cellLogical.InlineSize(),
					BlockSize:  rowHeight,
				}, wdm.WM)
				cl.result.Fragment.Size = physSize
			}

			rowBuilder.AddChild(cl.result.Fragment, LogicalOffset{
				InlineOffset: cl.inlineOffset,
				BlockOffset:  0,
			})

			// Collect OOF candidates from cell directly into the table builder.
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

		// Copy row style for background/border rendering.
		if row.node != nil && row.style != nil {
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

		if rowIdx == 0 {
			firstRowHeight = rowHeight
		}
		blockOffset += rowHeight
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

	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})

	// CSS 2.1 §17.5.2 / CSS Writing Modes 3 §4.3: Set the table's baseline.
	// The baseline of an inline-table is the baseline of its first row.
	// For central baseline mode, this is the center of the first row.
	// We store the distance from border-box block-start to the first row's
	// center as LastBaseline so inline layout can use it.
	if len(rows) > 0 && firstRowHeight > 0 {
		baseline := geom.Border.BlockStart + geom.Padding.BlockStart + firstRowHeight/2
		builder.SetLastBaseline(baseline)
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
// Also returns column width specifications from col/colgroup elements per CSS Tables §9.1.
func (tla *TableLayoutAlgorithm) collectRowsAndCaptions() ([]tableRow, []tableCaption, []tableColWidth) {
	var headerRows, bodyRows, footerRows []tableRow
	var captions []tableCaption
	var colWidths []tableColWidth
	colIdx := 0 // tracks current column index as col/colgroup are encountered

	for _, child := range tla.node.Children() {
		if child.IsText() {
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		display := childStyle.GetDisplay()


		// Check for col/colgroup elements by tag name.
		// These have display:block in our engine but affect column widths per CSS Tables §9.1.
		if child.DOMNode != nil && (child.DOMNode.TagName == "col" || child.DOMNode.TagName == "colgroup") {
			span := 1
			// <col span="N"> or <colgroup span="N">
			if spanAttr, ok := child.DOMNode.GetAttribute("span"); ok {
				if n := parseIntAttr(spanAttr); n > 0 {
					span = n
				}
			}
			// Resolve width from the col/colgroup style (ch units use the element's own
			// writing-mode and text-orientation, per CSS Values §6.1 and CSS WM §7.5).
			if w, ok := childStyle.GetLength("width"); ok && w > 0 {
				colWidths = append(colWidths, tableColWidth{
					colIndex: colIdx,
					span:     span,
					width:    w,
				})
			} else if child.DOMNode.TagName == "colgroup" {
				// colgroup without width: check child col elements
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

		case css.DisplayTableRow:
			bodyRows = append(bodyRows, tla.buildRow(child, childStyle))

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

		case css.DisplayTableCell:
			// Bare cell without a row — wrap in an anonymous row.
			bodyRows = append(bodyRows, tableRow{
				cells: []tableCell{{
					node:    child,
					style:   childStyle,
					colSpan: 1,
					rowSpan: 1,
				}},
			})
		}
	}

	// Render order: thead → tbody sections → tfoot.
	rows := make([]tableRow, 0, len(headerRows)+len(bodyRows)+len(footerRows))
	rows = append(rows, headerRows...)
	rows = append(rows, bodyRows...)
	rows = append(rows, footerRows...)
	return rows, captions, colWidths
}


// buildRow extracts cells from a table-row element.
// Per CSS Tables §2.1, non-table-cell children of a table-row are wrapped
// in anonymous table-cell boxes: consecutive non-cell siblings share one
// anonymous cell. Whitespace-only text nodes are ignored.
func (tla *TableLayoutAlgorithm) buildRow(node *LayoutInputNode, style *css.Style) tableRow {
	row := tableRow{node: node, style: style}
	colIdx := 0

	var anonChildren []*LayoutInputNode

	flushAnon := func() {
		if len(anonChildren) == 0 {
			return
		}
		anonStyle := css.NewAnonymousTableCellStyle(style)
		anonNode := &LayoutInputNode{
			style:       anonStyle,
			children:    anonChildren,
			isAnonymous: true,
		}
		row.cells = append(row.cells, tableCell{
			node:     anonNode,
			style:    anonStyle,
			colIndex: colIdx,
			colSpan:  1,
			rowSpan:  1,
		})
		colIdx++
		anonChildren = nil
	}

	for _, child := range node.Children() {
		if child.IsText() {
			if strings.TrimSpace(child.TextContent()) == "" {
				continue
			}
			anonChildren = append(anonChildren, child)
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		if childStyle.GetDisplay() == css.DisplayTableCell {
			flushAnon()
			colSpan := 1
			if child.DOMNode != nil {
				if cs, ok := child.DOMNode.GetAttribute("colspan"); ok {
					if v := parseIntAttr(cs); v > 0 {
						colSpan = v
					}
				}
			}
			row.cells = append(row.cells, tableCell{
				node:     child,
				style:    childStyle,
				colIndex: colIdx,
				colSpan:  colSpan,
				rowSpan:  1,
			})
			colIdx += colSpan
		} else {
			anonChildren = append(anonChildren, child)
		}
	}

	flushAnon()

	return row
}

// computeColumnWidthsFixed computes column widths using the fixed table layout
// algorithm (CSS 2.1 §17.5.2.1). Only the first row is examined for sizing.
// This is faster and more predictable than the auto algorithm.
func (tla *TableLayoutAlgorithm) computeColumnWidthsFixed(
	rows []tableRow, numCols int, availableForCols float64, spacingForCols float64,
) []float64 {
	if numCols == 0 {
		return []float64{}
	}

	colWidths := make([]float64, numCols)
	hasExplicit := make([]bool, numCols)

	// Step 1: Check first-row cells for explicit widths.
	// Per CSS 2.1 §17.5.2.1: "a column element with a value other than 'auto'
	// for the 'width' property sets the width for that column" — we don't
	// currently track <col> elements, so skip to:
	// "a cell in the first row with a value other than 'auto' for the 'width'
	// property determines the width for that column."
	if len(rows) > 0 {
		colIdx := 0
		for _, cell := range rows[0].cells {
			if colIdx >= numCols {
				break
			}
			if cell.style != nil {
				inlineProp := "width"
				if tla.space.WritingDirection.IsVertical() {
					inlineProp = "height"
				}
				if w, ok := cell.style.GetLength(inlineProp); ok && w > 0 {
					if cell.colSpan > 1 {
						// Distribute explicit width equally across spanned columns.
						perCol := w / float64(cell.colSpan)
						for c := 0; c < cell.colSpan && colIdx+c < numCols; c++ {
							if !hasExplicit[colIdx+c] {
								colWidths[colIdx+c] = perCol
								hasExplicit[colIdx+c] = true
							}
						}
					} else {
						colWidths[colIdx] = w
						hasExplicit[colIdx] = true
					}
				}
			}
			colIdx += cell.colSpan
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
		colIdx := 0
		for _, cell := range row.cells {
			if colIdx >= numCols {
				break
			}

			// Check for explicit inline-size on the cell from the TABLE's perspective.
			// For a horizontal table, the column width comes from the cell's physical width
			// (= cell logical block-size for orthogonal vertical-rl/lr cells).
			// We use the table's inline property ("width" for horizontal tables),
			// NOT the cell's own logical inline property.
			explicitW := 0.0
			hasExplicit := false
			if cell.style != nil {
				// Always use "width" from the TABLE's perspective (physical column width).
				// For orthogonal cells (vertical writing-mode in horizontal table),
				// the cell's CSS "width" is its physical width = table column width.
				// The cell's CSS "height" is its physical height = table row height (not column width).
				if w, ok := cell.style.GetLength("width"); ok && w > 0 {
					explicitW = w
					hasExplicit = true
				} else if tla.space.WritingDirection.IsVertical() {
					// For a vertical table, column width is in the vertical dimension = "height"
					if w, ok := cell.style.GetLength("height"); ok && w > 0 {
						explicitW = w
						hasExplicit = true
					}
				}
			}

			if cell.colSpan == 1 {
				if hasExplicit {
					if explicitW > colMin[colIdx] {
						colMin[colIdx] = explicitW
					}
					if explicitW > colMax[colIdx] {
						colMax[colIdx] = explicitW
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

			colIdx += cell.colSpan
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
// CSS border-spacing first value = horizontal = between columns (inline spacing).
// CSS border-spacing second value = vertical = between rows (block spacing).
// Per CSS 2.1 §17.6.1, these are always "horizontal between columns" and
// "vertical between rows" regardless of writing mode. The table layout algorithm
// works in logical coordinates where columns are inline and rows are block,
// so the mapping is always: inline=horizontal, block=vertical.
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
		colIdx := 0
		for _, cell := range row.cells {
			if colIdx < numCols {
				g.styles[rowIdx][colIdx] = cell.style
			}
			colIdx += cell.colSpan
		}
	}
	return g
}

// resolveCollapsedBorders computes half-border widths and winning colors for
// all cells in the grid. Returns a map from "row,col" to a cloned style with
// adjusted border properties. Only cells with modified borders are included.
//
// rowStyles/groupStyles/tableStyle provide borders from parent elements
// (tr, thead/tbody/tfoot, table) for CSS 2.1 §17.6.2.1 element-type precedence.
func (g *cellBorderGrid) resolveCollapsedBorders(wdm WritingDirectionMode, rowStyles, groupStyles []*css.Style, tableStyle *css.Style) map[string]*css.Style {
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

	// 3. Merge table borders — lowest precedence.
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
