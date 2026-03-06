package layout

import (
	"fmt"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
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

		// Skip caption elements (handled separately in layoutTable)
		if child.TagName == "caption" || childDisplay == css.DisplayTableCaption {
			continue
		}

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
	} else if _, ok := tableBox.Style.GetPercentage("width"); ok {
		// Percentage width was already resolved in layoutNode → use the content width
		explicitTableWidth = tableBox.Width - tableBox.Border.Left - tableBox.Border.Right - tableBox.Padding.Left - tableBox.Padding.Right
	}
	if tableBox.Style.GetTableLayout() == css.TableLayoutFixed && explicitTableWidth > 0 {
		tableInfo.ColumnWidths = le.calculateColumnWidthsFixed(cellGrid, tableInfo, explicitTableWidth)
	} else {
		tableInfo.ColumnWidths = le.calculateColumnWidths(cellGrid, availableWidth, tableInfo, explicitTableWidth, computedStyles)
	}

	// Set table width from column widths if not explicitly set
	_, hasExplicitWidth := tableBox.Style.GetLength("width")
	_, hasPercentWidth := tableBox.Style.GetPercentage("width")
	if !hasExplicitWidth && !hasPercentWidth {
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
	explicitTableHeight, hasExplicitHeight := tableBox.Style.GetLength("height")
	if !hasExplicitHeight {
		// Check if percentage height was already resolved by layoutNode
		preComputedContent := tableBox.Height - tableBox.Border.Top - tableBox.Border.Bottom -
			tableBox.Padding.Top - tableBox.Padding.Bottom
		if preComputedContent > 0 {
			explicitTableHeight = preComputedContent
			hasExplicitHeight = true
		}
	}
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
	} else {
		// Distribute explicit height to rows if it exceeds content-based row heights
		borderSpacing := tableInfo.BorderSpacing
		if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
			borderSpacing = 0
		}
		totalRowH := 0.0
		for _, rh := range tableInfo.RowHeights {
			totalRowH += rh
		}
		spacingH := borderSpacing * float64(len(tableInfo.RowHeights)+1)
		contentH := explicitTableHeight - tableBox.Border.Top - tableBox.Border.Bottom -
			tableBox.Padding.Top - tableBox.Padding.Bottom - spacingH
		if contentH > totalRowH && len(tableInfo.RowHeights) > 0 {
			extra := contentH - totalRowH
			// First, distribute extra space to rows WITHOUT explicit heights
			nonExplicitCount := 0
			for i := range tableInfo.RowHeights {
				if !tableInfo.RowHasExplicitHeight[i] {
					nonExplicitCount++
				}
			}
			if nonExplicitCount > 0 {
				perRow := extra / float64(nonExplicitCount)
				for i := range tableInfo.RowHeights {
					if !tableInfo.RowHasExplicitHeight[i] {
						tableInfo.RowHeights[i] += perRow
					}
				}
			} else {
				// All rows have explicit heights — distribute proportionally
				perRow := extra / float64(len(tableInfo.RowHeights))
				for i := range tableInfo.RowHeights {
					tableInfo.RowHeights[i] += perRow
				}
			}
		}
	}

	// Handle captions: find top and bottom captions, position them around the table body
	type captionEntry struct {
		node  *html.Node
		style *css.Style
		side  string // "top" or "bottom"
	}
	var topCaptions, bottomCaptions []captionEntry
	for _, child := range tableBox.Node.Children {
		if child.Type != html.ElementNode {
			continue
		}
		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
		}
		if child.TagName != "caption" && childStyle.GetDisplay() != css.DisplayTableCaption {
			continue
		}
		side := childStyle.GetCaptionSide()
		if side == "bottom" {
			bottomCaptions = append(bottomCaptions, captionEntry{child, childStyle, "bottom"})
		} else {
			topCaptions = append(topCaptions, captionEntry{child, childStyle, "top"})
		}
	}

	// Layout top captions first; accumulate their height to push table body down
	topCaptionHeight := 0.0
	for _, cap := range topCaptions {
		capBox := le.layoutNodeHTB(cap.node, x, y+topCaptionHeight, tableBox.Width, computedStyles, tableBox)
		if capBox != nil {
			tableBox.Children = append(tableBox.Children, capBox)
			topCaptionHeight += capBox.Height
		}
	}

	// Position cells below top captions
	tableBodyY := y + topCaptionHeight
	le.positionTableCells(tableBox, cellGrid, tableInfo, x, tableBodyY, computedStyles)

	// After positioning cells, tableBox.Height reflects body content (from positionTableCells)
	// Adjust it to include top captions
	tableBox.Height += topCaptionHeight

	// Layout bottom captions below table body
	for _, cap := range bottomCaptions {
		capY := y + tableBox.Height
		capBox := le.layoutNodeHTB(cap.node, x, capY, tableBox.Width, computedStyles, tableBox)
		if capBox != nil {
			tableBox.Children = append(tableBox.Children, capBox)
			tableBox.Height += capBox.Height
		}
	}
}

// Phase 9: processTableRows recursively processes rows and row groups
func (le *LayoutEngine) processTableRows(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style, rowIdx *int, cellGrid *[][]*TableCell, tableInfo *TableInfo) {
	display := style.GetDisplay()

	// Skip caption elements (handled separately by layoutTable)
	if node.TagName == "caption" || display == css.DisplayTableCaption {
		return
	}

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
func (le *LayoutEngine) calculateColumnWidths(cellGrid [][]*TableCell, availableWidth float64, tableInfo *TableInfo, tableWidth float64, computedStyles map[*html.Node]*css.Style) []float64 {
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
				cw := le.measureCellContentWidth(cell, computedStyles)
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
		if tableWidth > 0 {
			remaining = tableWidth - usedWidth
		}
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

// calculateColumnWidthsFixed implements CSS 2.1 §17.5.2.1 fixed table layout.
// Only the first row is examined to determine column widths.
func (le *LayoutEngine) calculateColumnWidthsFixed(cellGrid [][]*TableCell, tableInfo *TableInfo, tableWidth float64) []float64 {
	numCols := tableInfo.NumCols
	if numCols == 0 {
		return []float64{}
	}

	columnWidths := make([]float64, numCols)
	hasExplicit := make([]bool, numCols)

	// Step 1: Examine ONLY the first row for width hints
	if len(cellGrid) > 0 {
		firstRow := cellGrid[0]
		for colIdx, cell := range firstRow {
			if cell == nil || cell.Box == nil || cell.Box.Style == nil || cell.ColIdx != colIdx {
				continue
			}
			if w, ok := cell.Box.Style.GetLength("width"); ok && w > 0 {
				if cell.ColSpan > 1 {
					perCol := w / float64(cell.ColSpan)
					for c := 0; c < cell.ColSpan && colIdx+c < numCols; c++ {
						if !hasExplicit[colIdx+c] {
							columnWidths[colIdx+c] = perCol
							hasExplicit[colIdx+c] = true
						}
					}
				} else {
					columnWidths[colIdx] = w
					hasExplicit[colIdx] = true
				}
			}
		}
	}

	// Step 2: Calculate total spacing
	var totalSpacing float64
	if tableInfo.BorderCollapse == css.BorderCollapseSeparate {
		totalSpacing = tableInfo.BorderSpacing * float64(numCols+1)
	}

	// Step 3: Distribute remaining width evenly among unset columns
	usedWidth := totalSpacing
	unsetCols := 0
	for i := 0; i < numCols; i++ {
		usedWidth += columnWidths[i]
		if !hasExplicit[i] {
			unsetCols++
		}
	}

	if unsetCols > 0 {
		remaining := tableWidth - usedWidth
		if remaining > 0 {
			perCol := remaining / float64(unsetCols)
			for i := 0; i < numCols; i++ {
				if !hasExplicit[i] {
					columnWidths[i] = perCol
				}
			}
		} else {
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
func (le *LayoutEngine) measureCellContentWidth(cell *TableCell, computedStyles map[*html.Node]*css.Style) float64 {
	if cell == nil || cell.Box == nil || cell.Box.Node == nil {
		return 0
	}
	fontSize := 16.0
	isBold := false
	if cell.Box.Style != nil {
		fontSize = cell.Box.Style.GetFontSize()
		isBold = cell.Box.Style.GetFontWeight() == css.FontWeightBold
	}
	// Save counter state — measurement may process counter-reset/increment
	// for pseudo-elements, but these shouldn't affect the actual layout pass
	savedCounters := le.saveCounterState()
	totalWidth := le.measureTextContentRecursive(cell.Box.Node, fontSize, isBold, computedStyles)
	le.restoreCounterState(savedCounters)
	// Add cell padding and border
	if cell.Box.Style != nil {
		padding := cell.Box.Style.GetPadding()
		border := cell.Box.Style.GetBorderWidth()
		totalWidth += padding.Left + padding.Right + border.Left + border.Right
	}
	return totalWidth
}

// measureTextContentRecursive recursively measures all text content in a node's subtree,
// also accounting for elements with explicit CSS width (e.g., <div style="width:10px">).
// Block-level children start new lines, so their widths are compared with MAX rather than
// summed (CSS preferred width = width of the widest single line).
// Also accounts for ::before/::after pseudo-element content.
func (le *LayoutEngine) measureTextContentRecursive(node *html.Node, fontSize float64, isBold bool, computedStyles map[*html.Node]*css.Style) float64 {
	currentLineWidth := 0.0
	maxWidth := 0.0

	// Process counter-reset on this node (needed for accurate pseudo-element counter values)
	if node.Type == html.ElementNode && computedStyles != nil {
		if nodeStyle := computedStyles[node]; nodeStyle != nil {
			if resetVal, ok := nodeStyle.Get("counter-reset"); ok {
				resets := parseCounterReset(resetVal)
				for name, value := range resets {
					le.counterReset(name, value)
				}
			}
		}
	}

	// Measure ::before pseudo-element content
	beforeWidth := le.measurePseudoContentWidth(node, "before", fontSize, isBold, computedStyles)

	// Measure ::after pseudo-element content
	afterWidth := le.measurePseudoContentWidth(node, "after", fontSize, isBold, computedStyles)

	// Determine if pseudo-elements are block-level
	beforeIsBlock := false
	afterIsBlock := false
	if le != nil {
		parentStyle := computedStyles[node]
		if beforeWidth > 0 {
			beforeStyle := css.ComputePseudoElementStyle(node, "before", le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
			if beforeStyle != nil {
				switch beforeStyle.GetDisplay() {
				case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayFlex, css.DisplayGrid,
					css.DisplayTable, css.DisplayListItem:
					beforeIsBlock = true
				}
			}
		}
		if afterWidth > 0 {
			afterStyle := css.ComputePseudoElementStyle(node, "after", le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
			if afterStyle != nil {
				switch afterStyle.GetDisplay() {
				case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayFlex, css.DisplayGrid,
					css.DisplayTable, css.DisplayListItem:
					afterIsBlock = true
				}
			}
		}
	}

	// Add ::before content
	if beforeWidth > 0 {
		if beforeIsBlock {
			if beforeWidth > maxWidth {
				maxWidth = beforeWidth
			}
		} else {
			currentLineWidth += beforeWidth
		}
	}

	for _, child := range node.Children {
		if child.Type == html.TextNode {
			if strings.TrimSpace(child.Text) == "" {
				continue
			}
			w, _ := text.MeasureTextWithWeight(child.Text, fontSize, isBold)
			currentLineWidth += w
		} else if child.Type == html.ElementNode {
			// Determine if this element is block-level
			isBlock := false
			if computedStyles != nil {
				if childStyle := computedStyles[child]; childStyle != nil {
					switch childStyle.GetDisplay() {
					case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayFlex, css.DisplayGrid,
						css.DisplayTable, css.DisplayListItem:
						isBlock = true
					}
				}
			}

			// Check computed style for explicit width (e.g., .votearrow { width: 10px })
			if computedStyles != nil {
				if childStyle := computedStyles[child]; childStyle != nil {
					if w, ok := childStyle.GetLength("width"); ok && w > 0 {
						// Include element margins
						margin := childStyle.GetMargin()
						childWidth := w + margin.Left + margin.Right
						if isBlock {
							// Block with explicit width: finalize current line, compare
							if currentLineWidth > maxWidth {
								maxWidth = currentLineWidth
							}
							currentLineWidth = 0
							if childWidth > maxWidth {
								maxWidth = childWidth
							}
						} else {
							currentLineWidth += childWidth
						}
						continue
					}
				}
			}
			// Check HTML width attribute (e.g., <img width="18">)
			if w, ok := child.GetAttribute("width"); ok {
				if pw, ok := css.ParseLength(w + "px"); ok && pw > 0 {
					if isBlock {
						if currentLineWidth > maxWidth {
							maxWidth = currentLineWidth
						}
						currentLineWidth = 0
						if pw > maxWidth {
							maxWidth = pw
						}
					} else {
						currentLineWidth += pw
					}
					continue
				}
			}
			// Check img natural dimensions (replaced elements with no explicit width)
			if child.TagName == "img" {
				if src, ok := child.GetAttribute("src"); ok {
					if w, _, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil && w > 0 {
						imgW := float64(w)
						if isBlock {
							if currentLineWidth > maxWidth {
								maxWidth = currentLineWidth
							}
							currentLineWidth = 0
							if imgW > maxWidth {
								maxWidth = imgW
							}
						} else {
							currentLineWidth += imgW
						}
						continue
					}
				}
			}
			childWidth := le.measureTextContentRecursive(child, fontSize, isBold, computedStyles)
			if isBlock {
				// Block children start new lines
				if currentLineWidth > maxWidth {
					maxWidth = currentLineWidth
				}
				currentLineWidth = 0
				if childWidth > maxWidth {
					maxWidth = childWidth
				}
			} else {
				currentLineWidth += childWidth
			}
		}
	}

	// Add ::after content
	if afterWidth > 0 {
		if afterIsBlock {
			// Finalize current line before block after
			if currentLineWidth > maxWidth {
				maxWidth = currentLineWidth
			}
			currentLineWidth = 0
			if afterWidth > maxWidth {
				maxWidth = afterWidth
			}
		} else {
			currentLineWidth += afterWidth
		}
	}

	// Finalize last line
	if currentLineWidth > maxWidth {
		maxWidth = currentLineWidth
	}
	return maxWidth
}

// measurePseudoContentWidth measures the text content width of a ::before or ::after
// pseudo-element. Returns 0 if no pseudo-element content exists.
func (le *LayoutEngine) measurePseudoContentWidth(node *html.Node, pseudoType string, fontSize float64, isBold bool, computedStyles map[*html.Node]*css.Style) float64 {
	if le == nil || node.Type != html.ElementNode {
		return 0
	}
	parentStyle := computedStyles[node]
	pseudoStyle := css.ComputePseudoElementStyle(node, pseudoType, le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
	if pseudoStyle == nil {
		return 0
	}
	contentValues, hasContent := pseudoStyle.GetContentValues()
	if !hasContent || len(contentValues) == 0 {
		return 0
	}

	// Process counter-increment (same as createPseudoElementNode does)
	if incVal, ok := pseudoStyle.Get("counter-increment"); ok {
		increments := parseCounterIncrement(incVal)
		for name, value := range increments {
			le.counterIncrement(name, value)
		}
	}

	// Get quotes from parent style
	quotes := []string{"\"", "\"", "'", "'"}
	if parentStyle != nil {
		if q, ok := parentStyle.Get("quotes"); ok {
			quotes = parseQuotes(q)
		}
	}

	// Measure text content and image widths
	totalWidth := 0.0
	quoteDepth := 0
	var textBuf string

	for _, cv := range contentValues {
		switch cv.Type {
		case "text":
			textBuf += cv.Value
		case "url":
			// Flush accumulated text
			if textBuf != "" {
				w, _ := text.MeasureTextWithWeight(textBuf, fontSize, isBold)
				totalWidth += w
				textBuf = ""
			}
			// Add image intrinsic width
			if w, _, err := images.GetImageDimensionsWithFetcher(cv.Value, le.imageFetcher); err == nil {
				totalWidth += float64(w)
			}
		case "counter":
			counterValue := le.counterValue(cv.Value)
			textBuf += fmt.Sprintf("%d", counterValue)
		case "attr":
			if val, ok := node.GetAttribute(cv.Value); ok && val != "" {
				textBuf += val
			}
		case "open-quote":
			if quoteDepth*2 < len(quotes) {
				textBuf += quotes[quoteDepth*2]
			}
			quoteDepth++
		case "close-quote":
			if quoteDepth > 0 {
				quoteDepth--
			}
			if quoteDepth*2+1 < len(quotes) {
				textBuf += quotes[quoteDepth*2+1]
			}
		}
	}
	// Flush remaining text
	if textBuf != "" {
		w, _ := text.MeasureTextWithWeight(textBuf, fontSize, isBold)
		totalWidth += w
	}
	return totalWidth
}

// Phase 9: calculateRowHeights determines row heights
// Returns row heights and a boolean slice indicating which rows have explicit heights
func (le *LayoutEngine) calculateRowHeights(cellGrid [][]*TableCell, tableInfo *TableInfo) []float64 {
	numRows := len(cellGrid)
	rowHeights := make([]float64, numRows)
	tableInfo.RowHasExplicitHeight = make([]bool, numRows)

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
					tableInfo.RowHasExplicitHeight[i] = true
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
					if child.Type == html.TextNode && strings.TrimSpace(child.Text) != "" {
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
func (le *LayoutEngine) positionTableCells(tableBox *Box, cellGrid [][]*TableCell, tableInfo *TableInfo, x, y float64, computedStyles map[*html.Node]*css.Style) {
	borderSpacing := tableInfo.BorderSpacing
	if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
		borderSpacing = 0
	}

	// Single-pass: lay out cells row by row, updating row heights from actual content
	currentY := y + tableBox.Border.Top + tableBox.Padding.Top + borderSpacing
	processedCells := make(map[*TableCell]bool)

	for rowIdx, row := range cellGrid {
		currentX := x + tableBox.Border.Left + tableBox.Padding.Left + borderSpacing
		rowHeight := tableInfo.RowHeights[rowIdx]

		type cellEntry struct {
			cell      *TableCell
			cellWidth float64
		}
		var rowCells []cellEntry

		for colIdx, cell := range row {
			if cell == nil || processedCells[cell] {
				if cell == nil {
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

			// In border-collapse: collapse, merge borders from row/row-group/col/table
			// into the cell's computed style (wider border wins per CSS 2.1 §17.6.2)
			if tableInfo.BorderCollapse == css.BorderCollapseCollapse && cell.Box.Node != nil {
				mergeCollapsedBorders(cell.Box.Node, tableBox, colIdx, computedStyles)
			}

			// Lay out cell content using the full layout engine
			if cell.Box.PseudoContent != "" && cell.Box.Node == nil {
				// Pseudo-element cell: manual text box
				cell.Box.Margin = cell.Box.Style.GetMargin()
				cell.Box.Padding = cell.Box.Style.GetPadding()
				cell.Box.Border = cell.Box.Style.GetBorderWidth()
				cell.Box.X = currentX
				cell.Box.Y = currentY

				fontSize := cell.Box.Style.GetFontSize()
				fontWeight := cell.Box.Style.GetFontWeight()
				bold := fontWeight == css.FontWeightBold
				textWidth, textHeight := text.MeasureTextWithWeight(cell.Box.PseudoContent, fontSize, bold)
				textBox := &Box{
					Style:         cell.Box.Style,
					X:             currentX + cell.Box.Border.Left + cell.Box.Padding.Left,
					Y:             currentY + cell.Box.Border.Top + cell.Box.Padding.Top,
					Width:         textWidth,
					Height:        textHeight,
					Parent:        cell.Box,
					PseudoContent: cell.Box.PseudoContent,
				}
				cell.Box.Children = append(cell.Box.Children, textBox)
			} else if cell.Box.Node != nil {
				// table-layout:fixed cells clip overflow to enforce column widths
				if tableBox.Style != nil && tableBox.Style.GetTableLayout() == css.TableLayoutFixed {
					if cellStyle, ok := computedStyles[cell.Box.Node]; ok && cellStyle != nil {
						cellStyle.Set("overflow", "hidden")
					}
				}
				// Use layoutNode to handle all content (text, inline elements, nested tables)
				cellBox := le.layoutNodeHTB(cell.Box.Node, currentX, currentY, cellWidth, computedStyles, tableBox)
				if cellBox != nil {
					cell.Box = cellBox
				}
			}

			// Update row height if actual content is taller (single-row cells only)
			if cell.RowSpan == 1 && cell.Box.Height > rowHeight {
				rowHeight = cell.Box.Height
			}

			// empty-cells: hide — mark empty cells so renderer skips their background/border
			if tableBox.Style != nil && tableBox.Style.GetEmptyCells() == "hide" && cell.Box.Node != nil {
				if isCellNodeEmpty(cell.Box.Node) {
					cell.Box.HideBackground = true
				}
			}

			rowCells = append(rowCells, cellEntry{cell: cell, cellWidth: cellWidth})
			processedCells[cell] = true
			currentX += cellWidth + borderSpacing
		}

		// Finalize row height and set cell dimensions
		tableInfo.RowHeights[rowIdx] = rowHeight
		for _, ce := range rowCells {
			// Calculate cell height from spanned rows
			cellHeight := 0.0
			for r := 0; r < ce.cell.RowSpan; r++ {
				if rowIdx+r < len(tableInfo.RowHeights) {
					cellHeight += tableInfo.RowHeights[rowIdx+r]
					if r > 0 {
						cellHeight += borderSpacing
					}
				}
			}

			// Save natural content height before overriding with row height
			naturalContentH := ce.cell.Box.Height - ce.cell.Box.Padding.Top - ce.cell.Box.Padding.Bottom - ce.cell.Box.Border.Top - ce.cell.Box.Border.Bottom

			ce.cell.Box.Width = ce.cellWidth
			ce.cell.Box.Height = cellHeight
			if ce.cell.Box.Width < 0 {
				ce.cell.Box.Width = 0
			}
			if ce.cell.Box.Height < 0 {
				ce.cell.Box.Height = 0
			}

			// CSS 2.1 §17.5.3: Apply vertical-align to table cells.
			// When the cell is taller than its content, shift children vertically.
			if ce.cell.Box.Node != nil && ce.cell.Box.Style != nil {
				vertAlign := ce.cell.Box.Style.GetVerticalAlign()
				cellContentH := cellHeight - ce.cell.Box.Padding.Top - ce.cell.Box.Padding.Bottom - ce.cell.Box.Border.Top - ce.cell.Box.Border.Bottom
				if naturalContentH < cellContentH {
					var yOffset float64
					switch vertAlign {
					case css.VerticalAlignMiddle:
						yOffset = (cellContentH - naturalContentH) / 2
					case css.VerticalAlignBottom:
						yOffset = cellContentH - naturalContentH
					default: // top/baseline: no offset (already at top)
						yOffset = 0
					}
					if yOffset != 0 {
						for _, child := range ce.cell.Box.Children {
							child.Y += yOffset
						}
					}
				}
			}

			tableBox.Children = append(tableBox.Children, ce.cell.Box)
		}

		currentY += rowHeight + borderSpacing
	}

	// Update table box height based on content
	if len(cellGrid) > 0 {
		tableBox.Height = currentY - y + tableBox.Border.Bottom + tableBox.Padding.Bottom
	}
}

// isCellNodeEmpty returns true if the cell node has no visible content.
// Per CSS 2.1 §17.6.1.1: whitespace-only text nodes are not "visible content".
func isCellNodeEmpty(node *html.Node) bool {
	for _, child := range node.Children {
		if child.Type == html.TextNode {
			if strings.TrimSpace(child.Text) != "" {
				return false
			}
		} else if child.Type == html.ElementNode {
			return false
		}
	}
	return true
}

// mergeCollapsedBorders implements CSS 2.1 §17.6.2 collapsed border model.
// For each side of a cell, the winning border is the widest border among
// the cell, row, row-group, column, column-group, and table (with hidden/none handling).
// This modifies the cell's computed style so that subsequent layout uses
// the merged borders.
func mergeCollapsedBorders(cellNode *html.Node, tableBox *Box, colIdx int, computedStyles map[*html.Node]*css.Style) {
	cellStyle := computedStyles[cellNode]
	if cellStyle == nil {
		return
	}

	// Collect ancestor styles: row (tr), row-group (tbody/thead/tfoot), table
	var rowStyle, rowGroupStyle *css.Style
	if cellNode.Parent != nil {
		rowNode := cellNode.Parent
		rowStyle = computedStyles[rowNode]
		if rowNode.Parent != nil {
			rgNode := rowNode.Parent
			// Check if it's a row group (tbody/thead/tfoot) or the table itself
			if rgNode.TagName == "tbody" || rgNode.TagName == "thead" || rgNode.TagName == "tfoot" {
				rowGroupStyle = computedStyles[rgNode]
			}
		}
	}

	// Find col/colgroup elements for this column index
	var colStyle, colGroupStyle *css.Style
	if tableBox.Node != nil {
		colCount := 0
		for _, child := range tableBox.Node.Children {
			if child.Type != html.ElementNode {
				continue
			}
			if child.TagName == "colgroup" {
				cgStyle := computedStyles[child]
				startCol := colCount
				cgSpan := 0
				for _, gc := range child.Children {
					if gc.Type == html.ElementNode && gc.TagName == "col" {
						span := 1
						if s, ok := gc.GetAttribute("span"); ok {
							if n, err := fmt.Sscanf(s, "%d", &span); n == 0 || err != nil {
								span = 1
							}
						}
						if colIdx >= colCount && colIdx < colCount+span {
							colStyle = computedStyles[gc]
							colGroupStyle = cgStyle
						}
						colCount += span
						cgSpan += span
					}
				}
				// If colgroup had no col children, it represents cgSpan columns itself
				if cgSpan == 0 {
					span := 1
					if s, ok := child.GetAttribute("span"); ok {
						if n, err := fmt.Sscanf(s, "%d", &span); n == 0 || err != nil {
							span = 1
						}
					}
					if colIdx >= startCol && colIdx < startCol+span {
						colGroupStyle = cgStyle
					}
					colCount += span
				}
			} else if child.TagName == "col" {
				span := 1
				if s, ok := child.GetAttribute("span"); ok {
					if n, err := fmt.Sscanf(s, "%d", &span); n == 0 || err != nil {
						span = 1
					}
				}
				if colIdx >= colCount && colIdx < colCount+span {
					colStyle = computedStyles[child]
				}
				colCount += span
			}
		}
	}

	// For each side (top, right, bottom, left), pick the widest border
	// among cell, row, row-group, column, column-group, and table.
	// Priority at equal width: cell > row > row-group > column > column-group > table.
	sides := []string{"top", "right", "bottom", "left"}
	ancestorStyles := []*css.Style{rowStyle, rowGroupStyle, colStyle, colGroupStyle, tableBox.Style}

	for _, side := range sides {
		widthProp := "border-" + side + "-width"
		styleProp := "border-" + side + "-style"
		colorProp := "border-" + side + "-color"

		// Get cell's own border
		cellWidthStr, _ := cellStyle.Get(widthProp)
		cellBorderStyle, _ := cellStyle.Get(styleProp)
		cellWidth, _ := css.ParseLength(cellWidthStr)
		if cellBorderStyle == "none" || cellBorderStyle == "" {
			cellWidth = 0
		}

		// Check ancestors for wider borders
		bestWidth := cellWidth
		bestStyleProp := cellBorderStyle
		bestColorProp, _ := cellStyle.Get(colorProp)
		bestWidthStr := cellWidthStr

		for _, ancestorStyle := range ancestorStyles {
			if ancestorStyle == nil {
				continue
			}
			aWidthStr, _ := ancestorStyle.Get(widthProp)
			aBorderStyle, _ := ancestorStyle.Get(styleProp)
			aWidth, _ := css.ParseLength(aWidthStr)
			if aBorderStyle == "none" || aBorderStyle == "" {
				continue
			}
			if aBorderStyle == "hidden" {
				// hidden wins: force no border
				bestWidth = 0
				bestStyleProp = "hidden"
				bestColorProp = ""
				bestWidthStr = "0"
				break
			}
			if aWidth > bestWidth {
				bestWidth = aWidth
				bestStyleProp = aBorderStyle
				aColor, _ := ancestorStyle.Get(colorProp)
				bestColorProp = aColor
				bestWidthStr = aWidthStr
			}
		}

		// Apply the winning border to the cell's computed style
		if bestWidth != cellWidth || bestStyleProp != cellBorderStyle {
			if bestWidthStr != "" {
				cellStyle.Set(widthProp, bestWidthStr)
			}
			if bestStyleProp != "" {
				cellStyle.Set(styleProp, bestStyleProp)
			}
			if bestColorProp != "" {
				cellStyle.Set(colorProp, bestColorProp)
			}
		}
	}
}

