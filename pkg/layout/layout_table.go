package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

func (le *LayoutEngine) buildTableInfo(tableBox *Box, computedStyles map[*html.Node]*css.Style) *TableInfo {
	tableInfo := &TableInfo{
		Rows:           make([]*TableRow, 0),
		BorderSpacing:  tableBox.Style.GetBorderSpacing(),
		BorderCollapse: tableBox.Style.GetBorderCollapse(),
	}

	// Scan children for rows (tr elements or display: table-row)
	// Also handle anonymous row generation for direct table-cell children
	var anonRow *TableRow
	for _, child := range tableBox.Node.Children {
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
		}

		childDisplay := childStyle.GetDisplay()

		// Check if this is a row (tr tag or display: table-row)
		isRow := child.TagName == "tr" || childDisplay == css.DisplayTableRow

		// Also check for tbody, thead, tfoot which contain rows
		isRowGroup := child.TagName == "tbody" || child.TagName == "thead" || child.TagName == "tfoot" ||
			childDisplay == css.DisplayTableRowGroup ||
			childDisplay == css.DisplayTableHeaderGroup ||
			childDisplay == css.DisplayTableFooterGroup

		// Check if this is a table-cell (or will be wrapped as one)
		isCell := child.TagName == "td" || child.TagName == "th" || childDisplay == css.DisplayTableCell

		if isRow {
			anonRow = nil // explicit row breaks anonymous row
			tableInfo.Rows = append(tableInfo.Rows, &TableRow{Cells: make([]*TableCell, 0)})
		} else if isRowGroup {
			anonRow = nil
			// Process rows within the group
			for _, groupChild := range child.Children {
				if groupChild.Type != html.ElementNode {
					continue
				}
				groupChildStyle := computedStyles[groupChild]
				if groupChildStyle == nil {
					groupChildStyle = css.NewStyle()
				}
				if groupChild.TagName == "tr" || groupChildStyle.GetDisplay() == css.DisplayTableRow {
					tableInfo.Rows = append(tableInfo.Rows, &TableRow{Cells: make([]*TableCell, 0)})
				}
			}
		} else if isCell {
			// CSS 2.1 §17.2.1: Generate anonymous table-row for consecutive table-cells
			if anonRow == nil {
				anonRow = &TableRow{Cells: make([]*TableCell, 0)}
				tableInfo.Rows = append(tableInfo.Rows, anonRow)
			}
		} else {
			// Non-table child: wrap in anonymous cell within the anonymous row
			if anonRow == nil {
				anonRow = &TableRow{Cells: make([]*TableCell, 0)}
				tableInfo.Rows = append(tableInfo.Rows, anonRow)
			}
		}
	}

	return tableInfo
}

// Phase 9: getColspan returns the colspan attribute value (default 1)

// Phase 9: getRowspan returns the rowspan attribute value (default 1)

// Phase 9: layoutTable performs table layout
func (le *LayoutEngine) layoutTable(tableBox *Box, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style) {
	tableInfo := le.buildTableInfo(tableBox, computedStyles)

	// Build cell grid accounting for rowspan/colspan
	rowIdx := 0
	cellGrid := make([][]*TableCell, 0)

	// Process table structure
	for _, child := range tableBox.Node.Children {
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
		}

		le.processTableRows(child, childStyle, computedStyles, &rowIdx, &cellGrid, tableInfo)
	}

	// Determine number of columns
	numCols := 0
	for _, row := range cellGrid {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	tableInfo.NumCols = numCols

	// Calculate column widths
	// Pass 0 for tableWidth when the table has no explicit width (shrink-to-fit)
	explicitTableWidth := 0.0
	if w, ok := tableBox.Style.GetLength("width"); ok {
		explicitTableWidth = w
	}
	tableInfo.ColumnWidths = le.calculateColumnWidths(cellGrid, availableWidth, tableInfo, explicitTableWidth)

	// Set table width from column widths if not explicitly set
	// Check the style for an explicit width, not tableBox.Width which includes borders
	_, hasExplicitWidth := tableBox.Style.GetLength("width")
	if !hasExplicitWidth {
		totalW := 0.0
		for _, cw := range tableInfo.ColumnWidths {
			totalW += cw
		}
		borderSpacing := tableInfo.BorderSpacing
		if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
			borderSpacing = 0
		}
		spacingWidth := borderSpacing * float64(numCols+1)
		totalW += spacingWidth
		tableBox.Width = totalW + tableBox.Border.Left + tableBox.Border.Right +
			tableBox.Padding.Left + tableBox.Padding.Right
	}

	// Calculate row heights
	tableInfo.RowHeights = le.calculateRowHeights(cellGrid, tableInfo)

	// Set table height from row heights if not explicitly set
	_, hasExplicitHeight := tableBox.Style.GetLength("height")
	if !hasExplicitHeight {
		totalH := 0.0
		for _, rh := range tableInfo.RowHeights {
			totalH += rh
		}
		borderSpacing := tableInfo.BorderSpacing
		if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
			borderSpacing = 0
		}
		totalH += borderSpacing * float64(len(tableInfo.RowHeights)+1)
		tableBox.Height = totalH + tableBox.Border.Top + tableBox.Border.Bottom +
			tableBox.Padding.Top + tableBox.Padding.Bottom
	}

	// Position cells
	le.positionTableCells(tableBox, cellGrid, tableInfo, x, y)
}

// Phase 9: processTableRows recursively processes rows and row groups
func (le *LayoutEngine) processTableRows(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style, rowIdx *int, cellGrid *[][]*TableCell, tableInfo *TableInfo) {
	display := style.GetDisplay()
	isRow := node.TagName == "tr" || display == css.DisplayTableRow
	isRowGroup := node.TagName == "tbody" || node.TagName == "thead" || node.TagName == "tfoot" ||
		display == css.DisplayTableRowGroup ||
		display == css.DisplayTableHeaderGroup ||
		display == css.DisplayTableFooterGroup

	if isRow {
		// Ensure we have enough rows in the grid
		for len(*cellGrid) <= *rowIdx {
			*cellGrid = append(*cellGrid, make([]*TableCell, 0))
		}

		colIdx := 0

		// Check for ::before pseudo-element with display: table-cell
		beforeStyle := css.ComputePseudoElementStyle(node, "before", le.stylesheets, le.viewport.width, le.viewport.height, style)
		if beforeStyle != nil && beforeStyle.GetDisplay() == css.DisplayTableCell {
			content, _ := beforeStyle.Get("content")
			if content != "" && content != "none" {
				// Strip quotes from content
				if len(content) >= 2 && ((content[0] == '"' && content[len(content)-1] == '"') ||
					(content[0] == '\'' && content[len(content)-1] == '\'')) {
					content = content[1 : len(content)-1]
				}
				// Create pseudo-element cell
				pseudoCell := &TableCell{
					Box:     &Box{Style: beforeStyle, PseudoContent: content},
					RowSpan: 1,
					ColSpan: 1,
					RowIdx:  *rowIdx,
					ColIdx:  colIdx,
				}
				for len((*cellGrid)[*rowIdx]) <= colIdx {
					(*cellGrid)[*rowIdx] = append((*cellGrid)[*rowIdx], nil)
				}
				(*cellGrid)[*rowIdx][colIdx] = pseudoCell
				colIdx++
			}
		}

		for _, cellNode := range node.Children {
			if cellNode.Type != html.ElementNode {
				continue
			}

			cellStyle := computedStyles[cellNode]
			if cellStyle == nil {
				cellStyle = css.NewStyle()
			}

			isCell := cellNode.TagName == "td" || cellNode.TagName == "th" ||
				cellStyle.GetDisplay() == css.DisplayTableCell

			if !isCell {
				continue
			}

			// Skip columns occupied by rowspan from previous rows
			for colIdx < len((*cellGrid)[*rowIdx]) && (*cellGrid)[*rowIdx][colIdx] != nil {
				colIdx++
			}

			colspan := getColspan(cellNode)
			rowspan := getRowspan(cellNode)

			cell := &TableCell{
				Box:     &Box{Node: cellNode, Style: cellStyle},
				RowSpan: rowspan,
				ColSpan: colspan,
				RowIdx:  *rowIdx,
				ColIdx:  colIdx,
			}

			// Mark cells in grid for this cell and its span
			for r := 0; r < rowspan; r++ {
				for len(*cellGrid) <= *rowIdx+r {
					*cellGrid = append(*cellGrid, make([]*TableCell, 0))
				}
				for c := 0; c < colspan; c++ {
					for len((*cellGrid)[*rowIdx+r]) <= colIdx+c {
						(*cellGrid)[*rowIdx+r] = append((*cellGrid)[*rowIdx+r], nil)
					}
					(*cellGrid)[*rowIdx+r][colIdx+c] = cell
				}
			}

			colIdx += colspan
		}

		// Check for ::after pseudo-element with display: table-cell
		afterStyle := css.ComputePseudoElementStyle(node, "after", le.stylesheets, le.viewport.width, le.viewport.height, style)
		if afterStyle != nil && afterStyle.GetDisplay() == css.DisplayTableCell {
			content, _ := afterStyle.Get("content")
			if content != "" && content != "none" {
				// Strip quotes from content
				if len(content) >= 2 && ((content[0] == '"' && content[len(content)-1] == '"') ||
					(content[0] == '\'' && content[len(content)-1] == '\'')) {
					content = content[1 : len(content)-1]
				}
				// Create pseudo-element cell
				pseudoCell := &TableCell{
					Box:     &Box{Style: afterStyle, PseudoContent: content},
					RowSpan: 1,
					ColSpan: 1,
					RowIdx:  *rowIdx,
					ColIdx:  colIdx,
				}
				for len((*cellGrid)[*rowIdx]) <= colIdx {
					(*cellGrid)[*rowIdx] = append((*cellGrid)[*rowIdx], nil)
				}
				(*cellGrid)[*rowIdx][colIdx] = pseudoCell
				colIdx++
			}
		}

		*rowIdx++
	} else if isRowGroup {
		// Process rows within the group
		for _, child := range node.Children {
			if child.Type != html.ElementNode {
				continue
			}
			childStyle := computedStyles[child]
			if childStyle == nil {
				childStyle = css.NewStyle()
			}
			le.processTableRows(child, childStyle, computedStyles, rowIdx, cellGrid, tableInfo)
		}
	} else if display == css.DisplayTableCell || display == css.DisplayTable {
		// CSS 2.1 §17.2.1: Direct table-cell children generate an anonymous row
		// Also handle nested display:table elements as anonymous cells
		for len(*cellGrid) <= *rowIdx {
			*cellGrid = append(*cellGrid, make([]*TableCell, 0))
		}
		colIdx := len((*cellGrid)[*rowIdx])
		cell := &TableCell{
			Box:     &Box{Node: node, Style: style},
			RowSpan: 1,
			ColSpan: 1,
			RowIdx:  *rowIdx,
			ColIdx:  colIdx,
		}
		for len((*cellGrid)[*rowIdx]) <= colIdx {
			(*cellGrid)[*rowIdx] = append((*cellGrid)[*rowIdx], nil)
		}
		(*cellGrid)[*rowIdx][colIdx] = cell
	} else {
		// Non-table child: wrap in anonymous table-cell within the current anonymous row
		for len(*cellGrid) <= *rowIdx {
			*cellGrid = append(*cellGrid, make([]*TableCell, 0))
		}
		colIdx := len((*cellGrid)[*rowIdx])
		cell := &TableCell{
			Box:     &Box{Node: node, Style: style},
			RowSpan: 1,
			ColSpan: 1,
			RowIdx:  *rowIdx,
			ColIdx:  colIdx,
		}
		for len((*cellGrid)[*rowIdx]) <= colIdx {
			(*cellGrid)[*rowIdx] = append((*cellGrid)[*rowIdx], nil)
		}
		(*cellGrid)[*rowIdx][colIdx] = cell
	}
}

// Phase 9: calculateColumnWidths determines column widths
// tableWidth is the explicit table width (0 for shrink-to-fit tables)
func (le *LayoutEngine) calculateColumnWidths(cellGrid [][]*TableCell, availableWidth float64, tableInfo *TableInfo, tableWidth float64) []float64 {
	numCols := tableInfo.NumCols
	if numCols == 0 {
		return []float64{}
	}

	// Account for border spacing
	var totalSpacing float64
	if tableInfo.BorderCollapse == css.BorderCollapseSeparate {
		totalSpacing = tableInfo.BorderSpacing * float64(numCols+1)
	}

	// First pass: determine column widths from cell explicit widths
	columnWidths := make([]float64, numCols)
	hasExplicit := make([]bool, numCols)
	contentWidths := make([]float64, numCols) // content-based widths
	for _, row := range cellGrid {
		for colIdx, cell := range row {
			if cell == nil || cell.Box == nil || cell.Box.Style == nil || cell.ColIdx != colIdx {
				continue
			}
			if w, ok := cell.Box.Style.GetLength("width"); ok && w > 0 {
				if w > columnWidths[colIdx] {
					columnWidths[colIdx] = w
					hasExplicit[colIdx] = true
				}
			}
			// Measure content width for auto-sizing
			if !hasExplicit[colIdx] {
				cw := le.measureCellContentWidth(cell)
				if cw > contentWidths[colIdx] {
					contentWidths[colIdx] = cw
				}
			}
		}
	}

	// Distribute remaining width to columns without explicit widths
	// Use content-based sizing: give each column its content width,
	// then distribute any leftover space proportionally.
	usedWidth := totalSpacing
	unsetCols := 0
	totalContentWidth := 0.0
	for i := 0; i < numCols; i++ {
		usedWidth += columnWidths[i]
		if !hasExplicit[i] {
			unsetCols++
			totalContentWidth += contentWidths[i]
		}
	}
	if unsetCols > 0 {
		remaining := availableWidth - usedWidth
		if remaining > 0 {
			if tableWidth == 0 && totalContentWidth > 0 {
				// Shrink-to-fit table: use content widths directly, no extra space distribution
				for i := 0; i < numCols; i++ {
					if !hasExplicit[i] {
						columnWidths[i] = contentWidths[i]
					}
				}
			} else if totalContentWidth > 0 && totalContentWidth <= remaining {
				// Content fits: use content widths, distribute extra space proportionally
				extraSpace := remaining - totalContentWidth
				for i := 0; i < numCols; i++ {
					if !hasExplicit[i] {
						columnWidths[i] = contentWidths[i] + extraSpace*contentWidths[i]/totalContentWidth
					}
				}
			} else if totalContentWidth > remaining {
				// Content doesn't fit: distribute proportionally based on content
				for i := 0; i < numCols; i++ {
					if !hasExplicit[i] {
						columnWidths[i] = remaining * contentWidths[i] / totalContentWidth
					}
				}
			} else {
				// No content measured: distribute evenly
				perCol := remaining / float64(unsetCols)
				for i := 0; i < numCols; i++ {
					if !hasExplicit[i] {
						columnWidths[i] = perCol
					}
				}
			}
		} else {
			// No remaining space; use minimum width
			for i := 0; i < numCols; i++ {
				if !hasExplicit[i] {
					columnWidths[i] = 10
				}
			}
		}
	}

	return columnWidths
}

// measureCellContentWidth measures the preferred content width of a table cell
func (le *LayoutEngine) measureCellContentWidth(cell *TableCell) float64 {
	if cell == nil || cell.Box == nil || cell.Box.Node == nil {
		return 0
	}
	totalWidth := 0.0
	fontSize := 16.0
	isBold := false
	if cell.Box.Style != nil {
		fontSize = cell.Box.Style.GetFontSize()
		isBold = cell.Box.Style.GetFontWeight() == css.FontWeightBold
	}
	for _, child := range cell.Box.Node.Children {
		if child.Type == html.TextNode {
			w, _ := text.MeasureTextWithWeight(child.Text, fontSize, isBold)
			totalWidth += w
		}
	}
	// Add cell padding and border
	if cell.Box.Style != nil {
		padding := cell.Box.Style.GetPadding()
		border := cell.Box.Style.GetBorderWidth()
		totalWidth += padding.Left + padding.Right + border.Left + border.Right
	}
	return totalWidth
}

// Phase 9: calculateRowHeights determines row heights
func (le *LayoutEngine) calculateRowHeights(cellGrid [][]*TableCell, tableInfo *TableInfo) []float64 {
	numRows := len(cellGrid)
	rowHeights := make([]float64, numRows)

	// Calculate row heights from cell content and explicit heights
	for i := 0; i < numRows; i++ {
		maxHeight := 0.0
		for _, cell := range cellGrid[i] {
			if cell == nil || cell.Box == nil {
				continue
			}
			// Check for explicit height from style
			if cell.Box.Style != nil {
				if h, ok := cell.Box.Style.GetLength("height"); ok && h > maxHeight {
					maxHeight = h
				}
			}
			// Get padding and border from style since box values may not be set yet
			var paddingTop, paddingBottom, borderTop, borderBottom float64
			if cell.Box.Style != nil {
				padding := cell.Box.Style.GetPadding()
				paddingTop = padding.Top
				paddingBottom = padding.Bottom
				border := cell.Box.Style.GetBorderWidth()
				borderTop = border.Top
				borderBottom = border.Bottom
			}
			cellHeight := cell.Box.Height + paddingTop + paddingBottom + borderTop + borderBottom
			if cellHeight > maxHeight {
				maxHeight = cellHeight
			}
			// Estimate height from text content if cell hasn't been laid out yet
			if cell.Box.Height == 0 && cell.Box.Node != nil {
				lineHeight := 19.2 // default line height for 16px font
				if cell.Box.Style != nil {
					lineHeight = cell.Box.Style.GetLineHeight()
				}
				for _, child := range cell.Box.Node.Children {
					if child.Type == html.TextNode && child.Text != "" {
						textHeight := lineHeight + paddingTop + paddingBottom + borderTop + borderBottom
						if textHeight > maxHeight {
							maxHeight = textHeight
						}
					}
				}
			}
		}
		rowHeights[i] = maxHeight
	}

	return rowHeights
}

// Phase 9: positionTableCells positions cells in the table
func (le *LayoutEngine) positionTableCells(tableBox *Box, cellGrid [][]*TableCell, tableInfo *TableInfo, x, y float64) {
	borderSpacing := tableInfo.BorderSpacing
	if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
		borderSpacing = 0
	}

	// Position cells
	currentY := y + tableBox.Border.Top + tableBox.Padding.Top + borderSpacing
	processedCells := make(map[*TableCell]bool)

	for rowIdx, row := range cellGrid {
		currentX := x + tableBox.Border.Left + tableBox.Padding.Left + borderSpacing
		rowHeight := tableInfo.RowHeights[rowIdx]

		for colIdx, cell := range row {
			if cell == nil || processedCells[cell] {
				// Skip empty cells or already processed cells
				if cell == nil {
					// Still advance X for empty cell
					currentX += tableInfo.ColumnWidths[colIdx] + borderSpacing
				}
				continue
			}

			// Calculate cell width (sum of spanned columns)
			cellWidth := 0.0
			for c := 0; c < cell.ColSpan; c++ {
				if colIdx+c < tableInfo.NumCols {
					cellWidth += tableInfo.ColumnWidths[colIdx+c]
					if c > 0 {
						cellWidth += borderSpacing
					}
				}
			}

			// Calculate cell height (sum of spanned rows)
			cellHeight := 0.0
			for r := 0; r < cell.RowSpan; r++ {
				if rowIdx+r < len(tableInfo.RowHeights) {
					cellHeight += tableInfo.RowHeights[rowIdx+r]
					if r > 0 {
						cellHeight += borderSpacing
					}
				}
			}

			// Set cell box dimensions and position
			// Note: cellWidth/cellHeight from row/column calculations include padding+border,
			// but box.Width/Height should be content dimensions only
			cell.Box.Margin = cell.Box.Style.GetMargin()
			cell.Box.Padding = cell.Box.Style.GetPadding()
			cell.Box.Border = cell.Box.Style.GetBorderWidth()
			cell.Box.X = currentX
			cell.Box.Y = currentY
			// box.Width/Height should be border-box dimensions (for rendering)
			cell.Box.Width = cellWidth
			cell.Box.Height = cellHeight
			if cell.Box.Width < 0 {
				cell.Box.Width = 0
			}
			if cell.Box.Height < 0 {
				cell.Box.Height = 0
			}

			// Layout cell content (children)
			childY := currentY + cell.Box.Border.Top + cell.Box.Padding.Top
			childX := currentX + cell.Box.Border.Left + cell.Box.Padding.Left
			childAvailableWidth := cellWidth - cell.Box.Padding.Left - cell.Box.Padding.Right

			// Handle pseudo-element cells (have content but no DOM node)
			if cell.Box.Node == nil && cell.Box.PseudoContent != "" {
				// Measure and create text box for pseudo-content
				fontSize := cell.Box.Style.GetFontSize()
				fontWeight := cell.Box.Style.GetFontWeight()
				bold := fontWeight == css.FontWeightBold
				textWidth, textHeight := text.MeasureTextWithWeight(cell.Box.PseudoContent, fontSize, bold)
				textBox := &Box{
					Style:         cell.Box.Style,
					X:             childX,
					Y:             childY,
					Width:         textWidth,
					Height:        textHeight,
					Parent:        cell.Box,
					PseudoContent: cell.Box.PseudoContent,
				}
				cell.Box.Children = append(cell.Box.Children, textBox)
			} else if cell.Box.Node != nil {
				for _, childNode := range cell.Box.Node.Children {
					if childNode.Type == html.TextNode {
						// Handle text in cell
						textBox := le.layoutTextNode(childNode, childX, childY, childAvailableWidth, cell.Box.Style, cell.Box)
						if textBox != nil {
							cell.Box.Children = append(cell.Box.Children, textBox)
							childY += le.getTotalHeight(textBox)
						}
					}
				}
			}

			// Add cell box to table's children
			tableBox.Children = append(tableBox.Children, cell.Box)
			processedCells[cell] = true

			currentX += cellWidth + borderSpacing
		}

		currentY += rowHeight + borderSpacing
	}

	// Update table box height based on content (border-box = content area + borders + padding)
	if len(cellGrid) > 0 {
		tableBox.Height = currentY - y + tableBox.Border.Bottom + tableBox.Padding.Bottom
	}
}

