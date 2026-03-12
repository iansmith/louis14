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
		BorderSpacingV: tableBox.Style.GetBorderSpacingV(),
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
func (le *LayoutEngine) layoutTable(tableBox *Box, x, y, availableWidth float64, dir Dir, computedStyles map[*html.Node]*css.Style) {
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

	// Map border-spacing to logical axes: inline spacing for columns, block spacing for rows
	inlineSpacing := tableInfo.BorderSpacing
	blockSpacing := tableInfo.BorderSpacingV
	if dir.IsVertical() {
		inlineSpacing = tableInfo.BorderSpacingV
		blockSpacing = tableInfo.BorderSpacing
	}
	if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
		inlineSpacing = 0
		blockSpacing = 0
	}

	// Calculate column widths (= inline-axis track sizes)
	// Pass 0 for tableInline when the table has no explicit inline-size (shrink-to-fit)
	explicitTableInline := 0.0
	if w, ok := tableBox.Style.GetLength(dir.InlineSizeProp()); ok {
		explicitTableInline = w
	} else if _, ok := tableBox.Style.GetPercentage(dir.InlineSizeProp()); ok {
		// Percentage inline-size was already resolved in layoutNode → use the content inline-size
		explicitTableInline = dir.ContentInlineSize(tableBox)
	}
	if tableBox.Style.GetTableLayout() == css.TableLayoutFixed && explicitTableInline > 0 {
		tableInfo.ColumnWidths = le.calculateColumnWidthsFixed(cellGrid, tableInfo, explicitTableInline)
	} else {
		tableInfo.ColumnWidths = le.calculateColumnWidths(cellGrid, availableWidth, tableInfo, explicitTableInline, computedStyles)
	}

	// Set table inline-size from column widths if not explicitly set
	_, hasExplicitInline := tableBox.Style.GetLength(dir.InlineSizeProp())
	_, hasPercentInline := tableBox.Style.GetPercentage(dir.InlineSizeProp())
	if !hasExplicitInline && !hasPercentInline {
		totalInline := 0.0
		for _, cw := range tableInfo.ColumnWidths {
			totalInline += cw
		}
		spacingInline := inlineSpacing * float64(numCols+1)
		totalInline += spacingInline
		dir.SetInlineSize(tableBox, totalInline+dir.InlineBorderBox(tableBox.Padding, tableBox.Border))
	}

	// Calculate row heights (= block-axis track sizes)
	tableInfo.RowHeights = le.calculateRowHeights(cellGrid, tableInfo)

	// Set table block-size from row heights if not explicitly set
	explicitTableBlock, hasExplicitBlock := tableBox.Style.GetLength(dir.BlockSizeProp())
	if !hasExplicitBlock {
		// Check if percentage block-size was already resolved by layoutNode
		preComputedContent := dir.BlockSize(tableBox) - dir.BlockBorderBox(tableBox.Padding, tableBox.Border)
		if preComputedContent > 0 {
			explicitTableBlock = preComputedContent
			hasExplicitBlock = true
		}
	}
	if !hasExplicitBlock {
		totalBlock := 0.0
		for _, rh := range tableInfo.RowHeights {
			totalBlock += rh
		}
		totalBlock += blockSpacing * float64(len(tableInfo.RowHeights)+1)
		dir.SetBlockSize(tableBox, totalBlock+dir.BlockBorderBox(tableBox.Padding, tableBox.Border))
	} else {
		// Distribute explicit block-size to rows if it exceeds content-based row heights
		totalRowBlock := 0.0
		for _, rh := range tableInfo.RowHeights {
			totalRowBlock += rh
		}
		spacingBlock := blockSpacing * float64(len(tableInfo.RowHeights)+1)
		contentBlock := explicitTableBlock - dir.BlockBorderBox(tableBox.Padding, tableBox.Border) - spacingBlock
		if contentBlock > totalRowBlock && len(tableInfo.RowHeights) > 0 {
			extra := contentBlock - totalRowBlock
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

	// Layout top captions first; accumulate their block extent to push table body down
	topCaptionBlockExt := 0.0
	for _, cap := range topCaptions {
		capBlock := dir.BlockPos(tableBox) + dir.BlockStartEdge(tableBox.Border) + dir.BlockStartEdge(tableBox.Padding) + topCaptionBlockExt
		if dir.WM == VerticalRL {
			capBlock = dir.BlockPos(tableBox) + dir.BlockSize(tableBox) - dir.BlockEndEdge(tableBox.Border) - dir.BlockEndEdge(tableBox.Padding) - topCaptionBlockExt
		}
		capInline := dir.InlinePos(tableBox)
		capX, capY := dir.MakePhysical(capInline, capBlock)
		capBox := le.layoutNode(cap.node, capX, capY, dir.InlineSize(tableBox), dir, computedStyles, tableBox)
		if capBox != nil {
			tableBox.Children = append(tableBox.Children, capBox)
			topCaptionBlockExt += dir.BlockSize(capBox)
		}
	}

	// Position cells below top captions
	tableBodyBlock := dir.BlockPos(tableBox) + dir.BlockStartEdge(tableBox.Border) + dir.BlockStartEdge(tableBox.Padding) + topCaptionBlockExt
	if dir.WM == VerticalRL {
		tableBodyBlock = dir.BlockPos(tableBox) + dir.BlockSize(tableBox) - dir.BlockEndEdge(tableBox.Border) - dir.BlockEndEdge(tableBox.Padding) - topCaptionBlockExt
	}
	tableBodyX, tableBodyY := dir.MakePhysical(dir.InlinePos(tableBox), tableBodyBlock)
	le.positionTableCells(tableBox, cellGrid, tableInfo, tableBodyX, tableBodyY, dir, computedStyles)

	// After positioning cells, tableBox block-size reflects body content (from positionTableCells)
	// Adjust it to include top captions
	dir.SetBlockSize(tableBox, dir.BlockSize(tableBox)+topCaptionBlockExt)

	// Layout bottom captions below table body
	for _, cap := range bottomCaptions {
		capBlock := dir.BlockPos(tableBox) + dir.BlockSize(tableBox)
		if dir.WM == VerticalRL {
			capBlock = dir.BlockPos(tableBox) - (dir.BlockSize(tableBox) - dir.BlockStartEdge(tableBox.Border))
		}
		capInline := dir.InlinePos(tableBox)
		capX, capY := dir.MakePhysical(capInline, capBlock)
		capBox := le.layoutNode(cap.node, capX, capY, dir.InlineSize(tableBox), dir, computedStyles, tableBox)
		if capBox != nil {
			tableBox.Children = append(tableBox.Children, capBox)
			dir.SetBlockSize(tableBox, dir.BlockSize(tableBox)+dir.BlockSize(capBox))
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
	isItalic := false
	isMono := false
	isAhem := false
	if cell.Box.Style != nil {
		fontSize = cell.Box.Style.GetFontSize()
		isBold = cell.Box.Style.GetFontWeight() == css.FontWeightBold
		isItalic = cell.Box.Style.GetFontStyle() == css.FontStyleItalic
		isMono = cell.Box.Style.IsMonospaceFamily()
		isAhem = cell.Box.Style.IsAhemFamily()
	}
	// Save counter state — measurement may process counter-reset/increment
	// for pseudo-elements, but these shouldn't affect the actual layout pass
	savedCounters := le.saveCounterState()
	totalWidth := le.measureTextContentRecursive(cell.Box.Node, fontSize, isBold, isItalic, isMono, isAhem, computedStyles)
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
func (le *LayoutEngine) measureTextContentRecursive(node *html.Node, fontSize float64, isBold, isItalic, isMono, isAhem bool, computedStyles map[*html.Node]*css.Style) float64 {
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
	beforeWidth := le.measurePseudoContentWidth(node, "before", fontSize, isBold, isItalic, isMono, isAhem, computedStyles)

	// Measure ::after pseudo-element content
	afterWidth := le.measurePseudoContentWidth(node, "after", fontSize, isBold, isItalic, isMono, isAhem, computedStyles)

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
			w, _ := text.MeasureTextWithStyle(child.Text, fontSize, isBold, isItalic, isMono, isAhem)
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
			// Check if child element overrides font properties
			childBold, childItalic, childMono, childAhem := isBold, isItalic, isMono, isAhem
			childFontSize := fontSize
			if computedStyles != nil {
				if childStyle := computedStyles[child]; childStyle != nil {
					childFontSize = childStyle.GetFontSize()
					childBold = childStyle.GetFontWeight() == css.FontWeightBold
					childItalic = childStyle.GetFontStyle() == css.FontStyleItalic
					childMono = childStyle.IsMonospaceFamily()
					childAhem = childStyle.IsAhemFamily()
				}
			}
			childWidth := le.measureTextContentRecursive(child, childFontSize, childBold, childItalic, childMono, childAhem, computedStyles)
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
func (le *LayoutEngine) measurePseudoContentWidth(node *html.Node, pseudoType string, fontSize float64, isBold, isItalic, isMono, isAhem bool, computedStyles map[*html.Node]*css.Style) float64 {
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
				w, _ := text.MeasureTextWithStyle(textBuf, fontSize, isBold, isItalic, isMono, isAhem)
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
		w, _ := text.MeasureTextWithStyle(textBuf, fontSize, isBold, isItalic, isMono, isAhem)
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
func (le *LayoutEngine) positionTableCells(tableBox *Box, cellGrid [][]*TableCell, tableInfo *TableInfo, x, y float64, dir Dir, computedStyles map[*html.Node]*css.Style) {
	// Map border-spacing to logical axes
	inlineSpacing := tableInfo.BorderSpacing
	blockSpacing := tableInfo.BorderSpacingV
	if dir.IsVertical() {
		inlineSpacing = tableInfo.BorderSpacingV
		blockSpacing = tableInfo.BorderSpacing
	}
	if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
		inlineSpacing = 0
		blockSpacing = 0
	}

	// Extract logical starting positions from physical (x, y)
	startInline := dir.ExtractInline(x, y)
	startBlock := dir.ExtractBlock(x, y)

	// Single-pass: lay out cells row by row, updating row block-sizes from actual content
	currentBlock := startBlock + dir.BlockStartEdge(tableBox.Border) + dir.BlockStartEdge(tableBox.Padding) + blockSpacing
	if dir.WM == VerticalRL {
		// VRL: block flows right-to-left, start from the right edge
		currentBlock = startBlock - dir.BlockStartEdge(tableBox.Border) - dir.BlockStartEdge(tableBox.Padding) - blockSpacing
	}
	processedCells := make(map[*TableCell]bool)

	for rowIdx, row := range cellGrid {
		currentInline := startInline + dir.InlineStartEdge(tableBox.Border) + dir.InlineStartEdge(tableBox.Padding) + inlineSpacing
		rowBlockSize := tableInfo.RowHeights[rowIdx]

		type cellEntry struct {
			cell          *TableCell
			cellInlineSize float64
		}
		var rowCells []cellEntry

		for colIdx, cell := range row {
			if cell == nil || processedCells[cell] {
				if cell == nil {
					currentInline += tableInfo.ColumnWidths[colIdx] + inlineSpacing
				}
				continue
			}

			// Calculate cell inline-size (sum of spanned columns)
			cellInlineSize := 0.0
			for c := 0; c < cell.ColSpan; c++ {
				if colIdx+c < tableInfo.NumCols {
					cellInlineSize += tableInfo.ColumnWidths[colIdx+c]
					if c > 0 {
						cellInlineSize += inlineSpacing
					}
				}
			}

			// In border-collapse: collapse, merge borders from row/row-group/col/table
			// into the cell's computed style (wider border wins per CSS 2.1 §17.6.2)
			if tableInfo.BorderCollapse == css.BorderCollapseCollapse && cell.Box.Node != nil {
				mergeCollapsedBorders(cell.Box.Node, tableBox, colIdx, computedStyles)
			}

			// Convert logical positions to physical for layout
			cellBlock := currentBlock
			if dir.WM == VerticalRL {
				cellBlock = currentBlock - rowBlockSize
			}
			cellX, cellY := dir.MakePhysical(currentInline, cellBlock)

			// Lay out cell content using the full layout engine
			if cell.Box.PseudoContent != "" && cell.Box.Node == nil {
				// Pseudo-element cell: manual text box
				cell.Box.Margin = cell.Box.Style.GetMargin()
				cell.Box.Padding = cell.Box.Style.GetPadding()
				cell.Box.Border = cell.Box.Style.GetBorderWidth()
				cell.Box.X = cellX
				cell.Box.Y = cellY

				fontSize := cell.Box.Style.GetFontSize()
				fontWeight := cell.Box.Style.GetFontWeight()
				bold := fontWeight == css.FontWeightBold
				textWidth, textHeight := text.MeasureTextWithWeight(cell.Box.PseudoContent, fontSize, bold)
				textInline := currentInline + dir.InlineStartEdge(cell.Box.Border) + dir.InlineStartEdge(cell.Box.Padding)
				textBlockBase := cellBlock
				if dir.WM != VerticalRL {
					textBlockBase += dir.BlockStartEdge(cell.Box.Border) + dir.BlockStartEdge(cell.Box.Padding)
				} else {
					textBlockBase = currentBlock - dir.BlockStartEdge(cell.Box.Border) - dir.BlockStartEdge(cell.Box.Padding)
				}
				textX, textY := dir.MakePhysical(textInline, textBlockBase)
				textBox := &Box{
					Style:         cell.Box.Style,
					X:             textX,
					Y:             textY,
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
				cellBox := le.layoutNode(cell.Box.Node, cellX, cellY, cellInlineSize, dir, computedStyles, tableBox)
				if cellBox != nil {
					cell.Box = cellBox
				}
			}

			// Update row block-size if actual content is larger (single-row cells only)
			if cell.RowSpan == 1 && dir.BlockSize(cell.Box) > rowBlockSize {
				rowBlockSize = dir.BlockSize(cell.Box)
			}

			// empty-cells: hide — mark empty cells so renderer skips their background/border
			if tableBox.Style != nil && tableBox.Style.GetEmptyCells() == "hide" && cell.Box.Node != nil {
				if isCellNodeEmpty(cell.Box.Node) {
					cell.Box.HideBackground = true
				}
			}

			rowCells = append(rowCells, cellEntry{cell: cell, cellInlineSize: cellInlineSize})
			processedCells[cell] = true
			currentInline += cellInlineSize + inlineSpacing
		}

		// Finalize row block-size and set cell dimensions
		tableInfo.RowHeights[rowIdx] = rowBlockSize
		for _, ce := range rowCells {
			// Calculate cell block-size from spanned rows
			cellBlockSize := 0.0
			for r := 0; r < ce.cell.RowSpan; r++ {
				if rowIdx+r < len(tableInfo.RowHeights) {
					cellBlockSize += tableInfo.RowHeights[rowIdx+r]
					if r > 0 {
						cellBlockSize += blockSpacing
					}
				}
			}

			// Save natural content block-size before overriding with row block-size
			naturalContentBlock := dir.BlockSize(ce.cell.Box) -
				dir.BlockStartEdge(ce.cell.Box.Padding) - dir.BlockEndEdge(ce.cell.Box.Padding) -
				dir.BlockStartEdge(ce.cell.Box.Border) - dir.BlockEndEdge(ce.cell.Box.Border)

			dir.SetInlineSize(ce.cell.Box, ce.cellInlineSize)
			dir.SetBlockSize(ce.cell.Box, cellBlockSize)
			if dir.InlineSize(ce.cell.Box) < 0 {
				dir.SetInlineSize(ce.cell.Box, 0)
			}
			if dir.BlockSize(ce.cell.Box) < 0 {
				dir.SetBlockSize(ce.cell.Box, 0)
			}

			// CSS 2.1 §17.5.3: Apply vertical-align to table cells.
			// When the cell block-size exceeds its content, shift children in block direction.
			if ce.cell.Box.Node != nil && ce.cell.Box.Style != nil {
				vertAlign := ce.cell.Box.Style.GetVerticalAlign()
				cellContentBlock := cellBlockSize -
					dir.BlockStartEdge(ce.cell.Box.Padding) - dir.BlockEndEdge(ce.cell.Box.Padding) -
					dir.BlockStartEdge(ce.cell.Box.Border) - dir.BlockEndEdge(ce.cell.Box.Border)
				if naturalContentBlock < cellContentBlock {
					var blockOffset float64
					switch vertAlign {
					case css.VerticalAlignMiddle:
						blockOffset = (cellContentBlock - naturalContentBlock) / 2
					case css.VerticalAlignBottom:
						blockOffset = cellContentBlock - naturalContentBlock
					default: // top/baseline: no offset (already at block-start)
						blockOffset = 0
					}
					if blockOffset != 0 {
						// Apply block-direction offset to children
						for _, child := range ce.cell.Box.Children {
							if dir.IsVertical() {
								child.X += blockOffset
								if dir.WM == VerticalRL {
									child.X -= blockOffset * 2 // reverse direction for VRL
								}
							} else {
								child.Y += blockOffset
							}
						}
					}
				}
			}

			tableBox.Children = append(tableBox.Children, ce.cell.Box)
		}

		if dir.WM == VerticalRL {
			currentBlock -= rowBlockSize + blockSpacing
		} else {
			currentBlock += rowBlockSize + blockSpacing
		}
	}

	// Update table box block-size based on content
	if len(cellGrid) > 0 {
		var contentBlock float64
		if dir.WM == VerticalRL {
			contentBlock = startBlock - currentBlock + dir.BlockEndEdge(tableBox.Border) + dir.BlockEndEdge(tableBox.Padding)
		} else {
			contentBlock = currentBlock - startBlock + dir.BlockEndEdge(tableBox.Border) + dir.BlockEndEdge(tableBox.Padding)
		}
		dir.SetBlockSize(tableBox, contentBlock)
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

