package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

func (le *LayoutEngine) layoutFlex(flexBox *Box, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style) {
	direction := flexBox.Style.GetFlexDirection()
	wrap := flexBox.Style.GetFlexWrap()
	justifyContent := flexBox.Style.GetJustifyContent()
	alignItems := flexBox.Style.GetAlignItems()
	alignContent := flexBox.Style.GetAlignContent()

	isRow := direction == css.FlexDirectionRow || direction == css.FlexDirectionRowReverse

	container := &FlexContainer{
		Direction:      direction,
		Wrap:           wrap,
		JustifyContent: justifyContent,
		AlignItems:     alignItems,
		AlignContent:   alignContent,
		IsRow:          isRow,
		Lines:          make([]*FlexLine, 0),
	}

	// Determine container size
	if isRow {
		container.MainAxisSize = flexBox.Width
		container.CrossAxisSize = flexBox.Height
	} else {
		container.MainAxisSize = flexBox.Height
		container.CrossAxisSize = flexBox.Width
	}

	// Create flex items from children
	items := le.createFlexItems(flexBox, computedStyles)

	// Sort items by order property
	le.sortFlexItemsByOrder(items)

	// Create flex lines (handle wrapping)
	le.createFlexLines(container, items)

	// Resolve flexible lengths (flex-grow, flex-shrink)
	le.resolveFlexibleLengths(container)

	// Position items along main axis
	le.distributeMainAxis(container, flexBox)

	// Position items along cross axis
	le.alignCrossAxis(container, flexBox)

	// Update flex box children from positioned items
	for _, line := range container.Lines {
		for _, item := range line.Items {
			flexBox.Children = append(flexBox.Children, item.Box)
		}
	}

	// Update flex box height based on content if not explicitly set
	if container.IsRow {
		totalCrossSize := 0.0
		for _, line := range container.Lines {
			totalCrossSize += line.CrossSize
		}
		if flexBox.Height < totalCrossSize {
			flexBox.Height = totalCrossSize
		}
	} else {
		totalMainSize := 0.0
		for _, line := range container.Lines {
			totalMainSize += line.MainSize
		}
		if flexBox.Width < totalMainSize {
			flexBox.Width = totalMainSize
		}
	}
}

// Phase 10: createFlexItems creates flex items from children
func (le *LayoutEngine) createFlexItems(flexBox *Box, computedStyles map[*html.Node]*css.Style) []*FlexItem {
	items := make([]*FlexItem, 0)

	for _, child := range flexBox.Node.Children {
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
		}

		// Skip display:none elements
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		// Create a box for the flex item (simplified - just get dimensions)
		margin := childStyle.GetMargin()
		padding := childStyle.GetPadding()
		border := childStyle.GetBorderWidth()

		var width, height float64
		if w, ok := childStyle.GetLength("width"); ok {
			width = w
		} else {
			width = 100 // Default width
		}
		if h, ok := childStyle.GetLength("height"); ok {
			height = h
		} else {
			height = 50 // Default height
		}

		box := &Box{
			Node:     child,
			Style:    childStyle,
			Width:    width,
			Height:   height,
			Margin:   margin,
			Padding:  padding,
			Border:   border,
			Children: make([]*Box, 0),
		}

		flexBasis := childStyle.GetFlexBasis()
		if flexBasis == -1 { // auto
			// Use width or height depending on flex direction
			flexBasis = width
		}

		item := &FlexItem{
			Box:        box,
			FlexGrow:   childStyle.GetFlexGrow(),
			FlexShrink: childStyle.GetFlexShrink(),
			FlexBasis:  flexBasis,
			Order:      childStyle.GetOrder(),
		}

		items = append(items, item)
	}

	return items
}

// Phase 10: sortFlexItemsByOrder sorts items by their order property
func (le *LayoutEngine) sortFlexItemsByOrder(items []*FlexItem) {
	// Simple bubble sort (good enough for small lists)
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Order > items[j].Order {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

// Phase 10: createFlexLines creates flex lines (handles wrapping)
func (le *LayoutEngine) createFlexLines(container *FlexContainer, items []*FlexItem) {
	if container.Wrap == css.FlexWrapNowrap {
		// Single line
		line := &FlexLine{Items: items, MainSize: 0, CrossSize: 0}
		container.Lines = append(container.Lines, line)
	} else {
		// Multiple lines (wrapping)
		currentLine := &FlexLine{Items: make([]*FlexItem, 0), MainSize: 0, CrossSize: 0}

		for _, item := range items {
			itemMainSize := item.FlexBasis
			if container.IsRow {
				itemMainSize = item.Box.Width
			} else {
				itemMainSize = item.Box.Height
			}

			// Check if item fits in current line
			if currentLine.MainSize+itemMainSize > container.MainAxisSize && len(currentLine.Items) > 0 {
				// Start new line
				container.Lines = append(container.Lines, currentLine)
				currentLine = &FlexLine{Items: make([]*FlexItem, 0), MainSize: 0, CrossSize: 0}
			}

			currentLine.Items = append(currentLine.Items, item)
			currentLine.MainSize += itemMainSize
		}

		// Add last line
		if len(currentLine.Items) > 0 {
			container.Lines = append(container.Lines, currentLine)
		}
	}
}

// Phase 10: resolveFlexibleLengths calculates final item sizes
func (le *LayoutEngine) resolveFlexibleLengths(container *FlexContainer) {
	for _, line := range container.Lines {
		// Calculate total flex basis
		totalFlexBasis := 0.0
		totalFlexGrow := 0.0
		totalFlexShrink := 0.0

		for _, item := range line.Items {
			if container.IsRow {
				item.MainSize = item.FlexBasis
			} else {
				item.MainSize = item.FlexBasis
			}
			totalFlexBasis += item.MainSize
			totalFlexGrow += item.FlexGrow
			totalFlexShrink += item.FlexShrink
		}

		// Calculate free space
		freeSpace := container.MainAxisSize - totalFlexBasis

		if freeSpace > 0 && totalFlexGrow > 0 {
			// Distribute extra space based on flex-grow
			for _, item := range line.Items {
				if item.FlexGrow > 0 {
					item.MainSize += freeSpace * (item.FlexGrow / totalFlexGrow)
				}
			}
		} else if freeSpace < 0 && totalFlexShrink > 0 {
			// Shrink items based on flex-shrink
			for _, item := range line.Items {
				if item.FlexShrink > 0 {
					shrinkAmount := -freeSpace * (item.FlexShrink / totalFlexShrink)
					item.MainSize -= shrinkAmount
					if item.MainSize < 0 {
						item.MainSize = 0
					}
				}
			}
		}

		// Set cross size
		for _, item := range line.Items {
			if container.IsRow {
				item.CrossSize = item.Box.Height
				item.Box.Width = item.MainSize
			} else {
				item.CrossSize = item.Box.Width
				item.Box.Height = item.MainSize
			}

			// Update line cross size
			if item.CrossSize > line.CrossSize {
				line.CrossSize = item.CrossSize
			}
		}
	}
}

// Phase 10: distributeMainAxis positions items along main axis
func (le *LayoutEngine) distributeMainAxis(container *FlexContainer, flexBox *Box) {
	for _, line := range container.Lines {
		// Calculate total main size of items
		totalMainSize := 0.0
		for _, item := range line.Items {
			totalMainSize += item.MainSize
		}

		freeSpace := container.MainAxisSize - totalMainSize
		currentPos := 0.0
		spacing := 0.0
		initialOffset := 0.0

		switch container.JustifyContent {
		case css.JustifyContentFlexStart:
			initialOffset = 0
		case css.JustifyContentFlexEnd:
			initialOffset = freeSpace
		case css.JustifyContentCenter:
			initialOffset = freeSpace / 2
		case css.JustifyContentSpaceBetween:
			if len(line.Items) > 1 {
				spacing = freeSpace / float64(len(line.Items)-1)
			}
		case css.JustifyContentSpaceAround:
			spacing = freeSpace / float64(len(line.Items))
			initialOffset = spacing / 2
		case css.JustifyContentSpaceEvenly:
			spacing = freeSpace / float64(len(line.Items)+1)
			initialOffset = spacing
		}

		currentPos = initialOffset

		for _, item := range line.Items {
			item.MainPos = currentPos
			currentPos += item.MainSize + spacing
		}
	}
}

// Phase 10: alignCrossAxis positions items along cross axis
func (le *LayoutEngine) alignCrossAxis(container *FlexContainer, flexBox *Box) {
	currentCrossPos := 0.0

	for _, line := range container.Lines {
		for _, item := range line.Items {
			// Determine alignment (use align-self if set, otherwise align-items)
			alignSelf := item.Box.Style.GetAlignSelf()
			alignment := container.AlignItems

			if alignSelf != css.AlignSelfAuto {
				// Map AlignSelf to AlignItems
				switch alignSelf {
				case css.AlignSelfFlexStart:
					alignment = css.AlignItemsFlexStart
				case css.AlignSelfFlexEnd:
					alignment = css.AlignItemsFlexEnd
				case css.AlignSelfCenter:
					alignment = css.AlignItemsCenter
				case css.AlignSelfStretch:
					alignment = css.AlignItemsStretch
				case css.AlignSelfBaseline:
					alignment = css.AlignItemsBaseline
				}
			}

			switch alignment {
			case css.AlignItemsFlexStart:
				item.CrossPos = currentCrossPos
			case css.AlignItemsFlexEnd:
				item.CrossPos = currentCrossPos + line.CrossSize - item.CrossSize
			case css.AlignItemsCenter:
				item.CrossPos = currentCrossPos + (line.CrossSize-item.CrossSize)/2
			case css.AlignItemsStretch:
				item.CrossPos = currentCrossPos
				item.CrossSize = line.CrossSize
				if container.IsRow {
					item.Box.Height = line.CrossSize
				} else {
					item.Box.Width = line.CrossSize
				}
			case css.AlignItemsBaseline:
				// Simplified - treat as flex-start
				item.CrossPos = currentCrossPos
			}

			// Set final box position
			if container.IsRow {
				item.Box.X = flexBox.X + flexBox.Border.Left + flexBox.Padding.Left + item.MainPos
				item.Box.Y = flexBox.Y + flexBox.Border.Top + flexBox.Padding.Top + item.CrossPos
			} else {
				item.Box.X = flexBox.X + flexBox.Border.Left + flexBox.Padding.Left + item.CrossPos
				item.Box.Y = flexBox.Y + flexBox.Border.Top + flexBox.Padding.Top + item.MainPos
			}
		}

		currentCrossPos += line.CrossSize
	}
}

