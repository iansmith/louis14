package layout

import (
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
	node  *LayoutInputNode
	style *css.Style
	cells []tableCell
}

// Layout performs table layout and returns the result.
func (tla *TableLayoutAlgorithm) Layout() *LayoutResult {
	wdm := tla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(tla.ctx, tla.node, tla.style, wdm, tla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(tla.node)

	// Collect rows and captions from the table's children.
	rows, captions := tla.collectRowsAndCaptions()

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

	// Resolve available inline size from the centralized border-box size.
	availableInline := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if availableInline < 0 {
		availableInline = 0
	}

	borderCollapse := tla.style.GetBorderCollapse() == css.BorderCollapseCollapse

	// Compute logical border spacing (CSS 2.1 §17.6.1).
	// In collapsed mode, spacing is always zero.
	inlineSpacing, blockSpacing := 0.0, 0.0
	if !borderCollapse {
		inlineSpacing, blockSpacing = tla.logicalBorderSpacing()
	}

	// Check if the table has an explicit inline-size (width/height).
	_, hasExplicitTableWidth := ResolveInlineSize(tla.style, wdm, tla.space, geom)

	// Compute column widths via auto table layout (CSS 2.1 §17.5.2).
	// Subtract inline spacing from available width for column sizing.
	spacingForCols := 0.0
	if numCols > 0 {
		spacingForCols = inlineSpacing * float64(numCols+1) // before first + between + after last
	}
	availableForCols := availableInline - spacingForCols
	if availableForCols < 0 {
		availableForCols = 0
	}
	colWidths := tla.computeColumnWidths(rows, numCols, availableForCols, borderCollapse, hasExplicitTableWidth)

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
			// This is where OOF descendants inside the cell are collected.
			cellWDM := wdm
			if cell.style != nil {
				cellWDM = NewWritingDirectionMode(cell.style)
			}
			cellSpace := NewConstraintSpaceBuilder(wdm, cellWDM, true).
				SetOrthogonalFallbackInlineSize(
					orthogonalFallbackSize(cellWDM, tla.ctx)).
				SetOrthogonalFallbackBlockSize(tla.space.OrthogonalFallbackBlockSize).
				SetAvailableSize(LogicalSize{
					InlineSize: cellWidth,
					BlockSize:  Indefinite,
				}).
				SetPercentageResolutionSize(LogicalSize{
					InlineSize: cellWidth,
					BlockSize:  0,
				}).
				SetPercentageResolutionInlineSize(cellWidth).
				Build()

			cellResult := layoutElement(tla.ctx, cell.node, cellSpace)
			cellLogical := NewLogicalFragment(wdm, cellResult.Fragment)

			if cellLogical.BlockSize() > rowHeight {
				rowHeight = cellLogical.BlockSize()
			}

			rowBuilder.AddChild(cellResult.Fragment, LogicalOffset{
				InlineOffset: cellInlineOffset,
				BlockOffset:  0,
			})

			// Collect OOF candidates from cell directly into the table builder.
			// Mirrors Blink: table algorithm centrally collects all OOF descendants,
			// bypassing row/section. Static positions are translated from cell
			// content-box coordinates to table content-box coordinates in one step:
			//   table-relative = cell-relative + cell-border/padding + cell-offset-in-table
			if len(cellResult.PropagatedOOFCandidates) > 0 && cell.style != nil {
				cellGeom := ComputeFragmentGeometry(cell.style, wdm)
				inlineAdj := cellInlineOffset + cellGeom.Border.InlineStart + cellGeom.Padding.InlineStart
				blockAdj := blockOffset + cellGeom.Border.BlockStart + cellGeom.Padding.BlockStart
				for _, cand := range cellResult.PropagatedOOFCandidates {
					adj := cand
					adj.StaticPosition.Offset.InlineOffset += inlineAdj
					adj.StaticPosition.Offset.BlockOffset += blockAdj
					builder.AddOutOfFlowCandidate(adj)
				}
			}

			colIdx += cell.colSpan
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
			physBorder := ToPhysicalEdges(ComputeFragmentGeometry(row.style, wdm).Border, wdm)
			physPadding := ToPhysicalEdges(ComputeFragmentGeometry(row.style, wdm).Padding, wdm)
			rowBuilder.SetBoxData(&PhysicalBoxData{
				Border:  physBorder,
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

	physBorder := ToPhysicalEdges(geom.Border, wdm)
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

// collectRowsAndCaptions extracts table rows and captions from the table's children,
// handling row-groups (thead, tbody, tfoot).
// CSS 2.1 §17.5: rendering order is thead, tbodies (in source order), tfoot.
func (tla *TableLayoutAlgorithm) collectRowsAndCaptions() ([]tableRow, []tableCaption) {
	var headerRows, bodyRows, footerRows []tableRow
	var captions []tableCaption

	for _, child := range tla.node.Children() {
		if child.IsText() {
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		display := childStyle.GetDisplay()

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
					groupRows = append(groupRows, tla.buildRow(grandchild, gcStyle))
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
	return rows, captions
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

// computeColumnWidths computes column widths using the auto table layout
// algorithm (CSS 2.1 §17.5.2).
func (tla *TableLayoutAlgorithm) computeColumnWidths(
	rows []tableRow, numCols int, availableInline float64, borderCollapse bool, hasExplicitWidth bool,
) []float64 {
	colWidths := make([]float64, numCols)

	// First pass: compute each column's min/max content width.
	colMin := make([]float64, numCols)
	colMax := make([]float64, numCols)

	for _, row := range rows {
		colIdx := 0
		for _, cell := range row.cells {
			if colIdx >= numCols {
				break
			}

			// Check for explicit inline-size on the cell.
			// In vertical writing modes, CSS "height" maps to inline-size.
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
					if explicitW > colMin[colIdx] {
						colMin[colIdx] = explicitW
					}
					if explicitW > colMax[colIdx] {
						colMax[colIdx] = explicitW
					}
				} else {
					// Compute intrinsic size.
					childWDM := tla.space.WritingDirection
					if cell.style != nil {
						childWDM = NewWritingDirectionMode(cell.style)
					}
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
