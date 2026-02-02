package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// GridCell represents a single cell in the grid
type GridCell struct {
	Row    int
	Column int
	Box    *Box
}

// layoutGridContainer handles CSS Grid layout
func (le *LayoutEngine) layoutGridContainer(
	node *html.Node,
	x, y, availableWidth float64,
	style *css.Style,
	computedStyles map[*html.Node]*css.Style,
	parent *Box,
) *Box {
	// Get grid properties
	columnTracks := style.GetGridTemplateColumns()
	rowTracks := style.GetGridTemplateRows()
	rowGap, columnGap := style.GetGridGap()
	justifyItems := style.GetJustifyItems()
	alignItems := style.GetAlignItems()

	// Get box model properties
	margin := style.GetMargin()
	padding := style.GetPadding()
	border := style.GetBorderWidth()

	// Calculate container dimensions
	var containerWidth float64
	if w, ok := style.GetLength("width"); ok {
		containerWidth = w
	} else if len(columnTracks) > 0 {
		// Calculate width from grid tracks
		containerWidth = 0
		for _, track := range columnTracks {
			containerWidth += track.Size
		}
		containerWidth += float64(len(columnTracks)-1) * columnGap
	} else {
		containerWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
	}

	// Apply max-width constraint
	if maxWidth, hasMaxWidth := style.GetMaxWidth(); hasMaxWidth {
		if containerWidth > maxWidth {
			containerWidth = maxWidth
		}
	}

	// Phase 13: Handle margin: auto for horizontal centering
	actualX := x
	if margin.AutoLeft && margin.AutoRight {
		totalWidth := containerWidth + padding.Left + padding.Right + border.Left + border.Right
		if totalWidth < availableWidth {
			centerOffset := (availableWidth - totalWidth) / 2
			actualX = x + centerOffset
		}
	}

	// Calculate container height
	var containerHeight float64
	if h, ok := style.GetLength("height"); ok {
		containerHeight = h
	} else if len(rowTracks) > 0 {
		// Calculate height from grid tracks
		containerHeight = 0
		for _, track := range rowTracks {
			containerHeight += track.Size
		}
		containerHeight += float64(len(rowTracks)-1) * rowGap
	}

	// Get positioning information
	position := style.GetPosition()
	zindex := style.GetZIndex()

	// Create container box
	box := &Box{
		Node:     node,
		Style:    style,
		X:        actualX,
		Y:        y,
		Width:    containerWidth,
		Height:   containerHeight,
		Margin:   margin,
		Padding:  padding,
		Border:   border,
		Children: make([]*Box, 0),
		Position: position,
		ZIndex:   zindex,
		Parent:   parent,
	}

	// Content area for grid items (inside padding and border)
	contentX := actualX + padding.Left + border.Left
	contentY := y + padding.Top + border.Top

	// Layout grid items
	gridItems := make([]*GridCell, 0)
	currentRow := 0
	currentColumn := 0

	// First pass: layout each child and determine its grid position
	for _, child := range node.Children {
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
			computedStyles[child] = childStyle
		}

		// Skip if display: none
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		// Check for explicit grid placement
		gridColumn := childStyle.GetGridColumn()
		gridRow := childStyle.GetGridRow()

		var cellRow, cellColumn int
		var rowSpan, columnSpan int

		if gridColumn != nil {
			cellColumn = gridColumn.Start - 1 // Convert to 0-indexed
			columnSpan = gridColumn.End - gridColumn.Start
		} else {
			cellColumn = currentColumn
			columnSpan = 1
		}

		if gridRow != nil {
			cellRow = gridRow.Start - 1 // Convert to 0-indexed
			rowSpan = gridRow.End - gridRow.Start
		} else {
			cellRow = currentRow
			rowSpan = 1
		}

		// Calculate cell dimensions
		cellWidth := 0.0
		for i := 0; i < columnSpan && cellColumn+i < len(columnTracks); i++ {
			cellWidth += columnTracks[cellColumn+i].Size
			if i > 0 {
				cellWidth += columnGap
			}
		}

		cellHeight := 0.0
		for i := 0; i < rowSpan && cellRow+i < len(rowTracks); i++ {
			cellHeight += rowTracks[cellRow+i].Size
			if i > 0 {
				cellHeight += rowGap
			}
		}

		// Layout the child item with the cell dimensions as available width/height
		childBox := le.layoutNode(child, 0, 0, cellWidth, computedStyles, box)
		if childBox != nil {
			// Override dimensions if smaller than cell
			if childBox.Width < cellWidth {
				// Apply justify-items
				switch justifyItems {
				case css.JustifyItemsCenter:
					childBox.X = (cellWidth - childBox.Width) / 2
				case css.JustifyItemsEnd:
					childBox.X = cellWidth - childBox.Width
				default: // start or stretch
					childBox.X = 0
				}
			}

			if childBox.Height < cellHeight {
				// Apply align-items
				switch alignItems {
				case css.AlignItemsCenter:
					childBox.Y = (cellHeight - childBox.Height) / 2
				case css.AlignItemsFlexEnd:
					childBox.Y = cellHeight - childBox.Height
				default: // flex-start or stretch
					childBox.Y = 0
				}
			}

			gridItems = append(gridItems, &GridCell{
				Row:    cellRow,
				Column: cellColumn,
				Box:    childBox,
			})
		}

		// Move to next cell position (for auto-placed items)
		if gridColumn == nil {
			currentColumn += columnSpan
			if currentColumn >= len(columnTracks) {
				currentColumn = 0
				currentRow++
			}
		}
	}

	// Second pass: position grid items in their cells
	for _, cell := range gridItems {
		// Calculate cell position
		cellX := contentX
		for i := 0; i < cell.Column; i++ {
			cellX += columnTracks[i].Size + columnGap
		}

		cellY := contentY
		for i := 0; i < cell.Row; i++ {
			cellY += rowTracks[i].Size + rowGap
		}

		// Position the item within its cell
		cell.Box.X += cellX
		cell.Box.Y += cellY

		box.Children = append(box.Children, cell.Box)
	}

	return box
}
