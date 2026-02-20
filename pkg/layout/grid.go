package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"math"
)

// GridCell represents a single cell in the grid
type GridCell struct {
	Row        int
	Column     int
	RowSpan    int
	ColumnSpan int
	Box        *Box
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
	justifyContent := style.GetJustifyContent()
	alignContent := style.GetAlignContent()
	templateAreas := style.GetGridTemplateAreas()

	// Get box model properties
	margin := style.GetMargin()
	padding := style.GetPadding()
	border := style.GetBorderWidth()

	// Calculate container content width
	var containerWidth float64
	hasExplicitWidth := false
	isInlineGrid := style.GetDisplay() == css.DisplayInlineGrid

	if w, ok := style.GetLength("width"); ok {
		containerWidth = w
		hasExplicitWidth = true
	} else if pct, ok := style.GetPercentage("width"); ok && pct > 0 {
		if parent != nil && parent.Width > 0 {
			parentContentWidth := parent.Width - parent.Padding.Left - parent.Padding.Right -
				parent.Border.Left - parent.Border.Right
			containerWidth = pct * parentContentWidth / 100
			hasExplicitWidth = true
		}
	} else if isInlineGrid {
		// Inline-grid: intrinsically sized, don't use available width as definite
		containerWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
		hasExplicitWidth = false
	} else {
		containerWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
		hasExplicitWidth = true // block-level grid fills available width
	}

	// Apply max-width constraint
	if maxWidth, hasMaxWidth := style.GetMaxWidth(); hasMaxWidth {
		if containerWidth > maxWidth {
			containerWidth = maxWidth
		}
	}

	// Handle margin: auto for horizontal centering
	actualX := x
	if margin.AutoLeft && margin.AutoRight {
		totalWidth := containerWidth + padding.Left + padding.Right + border.Left + border.Right
		if totalWidth < availableWidth {
			centerOffset := (availableWidth - totalWidth) / 2
			actualX = x + centerOffset
		}
	}

	// Calculate container content height
	var containerHeight float64
	hasExplicitHeight := false
	if h, ok := style.GetLength("height"); ok {
		containerHeight = h
		hasExplicitHeight = true
	} else if pct, ok := style.GetPercentage("height"); ok && pct > 0 {
		// Resolve percentage height against parent's content height
		if parent != nil && parent.Height > 0 {
			parentContentHeight := parent.Height - parent.Padding.Top - parent.Padding.Bottom -
				parent.Border.Top - parent.Border.Bottom
			containerHeight = pct * parentContentHeight / 100
			hasExplicitHeight = true
		}
	}

	// Get positioning information
	position := style.GetPosition()
	zindex := style.GetZIndex()

	// Collect grid items and determine placement
	type gridItemInfo struct {
		child      *html.Node
		childStyle *css.Style
		row, col   int
		rowSpan    int
		colSpan    int
	}
	items := make([]gridItemInfo, 0)
	maxRow := 0
	maxCol := 0

	currentRow := 0
	currentCol := 0
	numColTracks := len(columnTracks)
	numRowTracks := len(rowTracks)
	autoFlow := style.GetGridAutoFlow() // "row" or "column"

	// Expand display:contents children so their children participate as direct grid items
	gridChildren := le.flattenContentsChildren(node, computedStyles)

	for _, child := range gridChildren {
		if child.Type != html.ElementNode {
			continue
		}
		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
			computedStyles[child] = childStyle
		}
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		// CSS Grid §6.2: Blockify inline grid items
		switch childStyle.GetDisplay() {
		case css.DisplayInline:
			childStyle.Set("display", "block")
		case css.DisplayInlineBlock:
			childStyle.Set("display", "block")
		case css.DisplayInlineFlex:
			childStyle.Set("display", "flex")
		case css.DisplayInlineGrid:
			childStyle.Set("display", "grid")
		}

		gridColumn := childStyle.GetGridColumn()
		gridRow := childStyle.GetGridRow()
		gridAreaName := childStyle.GetGridArea()

		var cellRow, cellCol, rowSpan, colSpan int

		// Check if grid-area references a named template area
		if gridAreaName != "" && templateAreas != nil {
			if areaInfo, ok := templateAreas[gridAreaName]; ok {
				cellCol = areaInfo.ColStart - 1
				colSpan = areaInfo.ColEnd - areaInfo.ColStart
				cellRow = areaInfo.RowStart - 1
				rowSpan = areaInfo.RowEnd - areaInfo.RowStart
			} else {
				// Fallback: auto-placement
				cellCol = currentCol
				colSpan = 1
				cellRow = currentRow
				rowSpan = 1
			}
		} else {
			if gridColumn != nil {
				cellCol = gridColumn.Start - 1
				colSpan = gridColumn.End - gridColumn.Start
			} else {
				cellCol = currentCol
				colSpan = 1
			}
			if gridRow != nil {
				cellRow = gridRow.Start - 1
				rowSpan = gridRow.End - gridRow.Start
			} else {
				cellRow = currentRow
				rowSpan = 1
			}
		}

		items = append(items, gridItemInfo{
			child: child, childStyle: childStyle,
			row: cellRow, col: cellCol,
			rowSpan: rowSpan, colSpan: colSpan,
		})

		if cellCol+colSpan > maxCol {
			maxCol = cellCol + colSpan
		}
		if cellRow+rowSpan > maxRow {
			maxRow = cellRow + rowSpan
		}

		// Advance auto-placement cursor (only when no explicit placement via grid-column or grid-area name)
		namedAreaPlaced := false
		if gridAreaName != "" && templateAreas != nil {
			if _, ok := templateAreas[gridAreaName]; ok {
				namedAreaPlaced = true
			}
		}
		if gridColumn == nil && !namedAreaPlaced {
			if autoFlow == "column" {
				// Column-first: advance rows, wrap to next column
				currentRow += rowSpan
				if numRowTracks > 0 && currentRow >= numRowTracks {
					currentRow = 0
					currentCol++
				}
			} else {
				// Row-first (default): advance columns, wrap to next row
				currentCol += colSpan
				if numColTracks > 0 && currentCol >= numColTracks {
					currentCol = 0
					currentRow++
				}
			}
		}
	}

	// Ensure we have enough tracks (create implicit tracks if needed)
	// Use grid-auto-columns / grid-auto-rows for sizing implicit tracks
	autoColTrack := style.GetGridAutoColumns()
	autoRowTrack := style.GetGridAutoRows()
	for len(columnTracks) < maxCol {
		if autoColTrack != nil {
			columnTracks = append(columnTracks, *autoColTrack)
		} else {
			columnTracks = append(columnTracks, css.GridTrack{Auto: true})
		}
	}
	for len(rowTracks) < maxRow {
		if autoRowTrack != nil {
			rowTracks = append(rowTracks, *autoRowTrack)
		} else {
			rowTracks = append(rowTracks, css.GridTrack{Auto: true})
		}
	}

	// Phase 1: Layout items to determine auto track sizes
	// First pass with 0 width for auto columns to get intrinsic sizes
	itemBoxes := make([]*Box, len(items))
	autoColSizes := make([]float64, len(columnTracks))
	autoRowSizes := make([]float64, len(rowTracks))

	for i, item := range items {
		// Absolutely positioned grid items are out-of-flow; skip for intrinsic sizing
		if pos := item.childStyle.GetPosition(); pos == css.PositionAbsolute || pos == css.PositionFixed {
			continue
		}

		// Use a preliminary width for the child layout
		prelimWidth := 0.0
		hasFixedTrack := false
		for c := 0; c < item.colSpan && item.col+c < len(columnTracks); c++ {
			t := columnTracks[item.col+c]
			if !t.Auto && t.Fr == 0 && !(t.IsMinMax && t.MaxFr > 0) {
				prelimWidth += t.Size
				hasFixedTrack = true
			}
		}
		// Only fall back to containerWidth for fixed tracks;
		// for auto/fr tracks, keep 0 to get min-content size
		if hasFixedTrack && prelimWidth == 0 {
			prelimWidth = containerWidth
		}

		childBox := le.layoutNode(item.child, 0, 0, prelimWidth, computedStyles, nil)
		itemBoxes[i] = childBox
		if childBox == nil {
			continue
		}

		// Update auto column sizes from content
		// Include auto, fr, and minmax tracks — all need content-based sizing for intrinsic/indefinite paths
		if item.colSpan == 1 && item.col < len(columnTracks) {
			t := columnTracks[item.col]
			if t.Auto || t.Fr > 0 || (t.IsMinMax && t.MaxFr > 0) || t.MinContent || t.MaxContent {
				totalW := childBox.Width + childBox.Margin.Left + childBox.Margin.Right
				if totalW > autoColSizes[item.col] {
					autoColSizes[item.col] = totalW
				}
			}
		}
		// Update auto row sizes from content
		if item.rowSpan == 1 && item.row < len(rowTracks) {
			t := rowTracks[item.row]
			if t.Auto || t.Fr > 0 || (t.IsMinMax && t.MaxFr > 0) || t.MinContent || t.MaxContent {
				totalH := childBox.Height + childBox.Margin.Top + childBox.Margin.Bottom +
					childBox.Padding.Top + childBox.Padding.Bottom +
					childBox.Border.Top + childBox.Border.Bottom
				if totalH > autoRowSizes[item.row] {
					autoRowSizes[item.row] = totalH
				}
			}
		}
	}

	// Phase 2: Resolve track sizes
	resolvedColSizes := resolveTrackSizes(columnTracks, autoColSizes, containerWidth, columnGap, hasExplicitWidth, justifyContent == css.JustifyContentStretch)
	resolvedRowSizes := resolveTrackSizes(rowTracks, autoRowSizes, containerHeight, rowGap, hasExplicitHeight, alignContent == css.AlignContentStretch)

	// Calculate actual content dimensions from resolved tracks
	actualContentWidth := sumTracks(resolvedColSizes, columnGap)
	actualContentHeight := sumTracks(resolvedRowSizes, rowGap)

	// For inline-grid without explicit width, shrink-wrap to content
	if isInlineGrid && !hasExplicitWidth {
		containerWidth = actualContentWidth
	}

	if !hasExplicitHeight {
		containerHeight = actualContentHeight
	}

	// Phase 3: Compute content distribution offsets
	colOffsets := computeContentDistribution(resolvedColSizes, containerWidth, columnGap, justifyContent)
	rowOffsets := computeContentDistribution(resolvedRowSizes, containerHeight, rowGap, alignContent)

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

	// Content area origin
	contentX := actualX + padding.Left + border.Left
	contentY := y + padding.Top + border.Top

	// Phase 4: Re-layout and position items with final track sizes
	for i, item := range items {
		// Calculate cell dimensions from resolved tracks
		cellWidth := 0.0
		for c := 0; c < item.colSpan && item.col+c < len(resolvedColSizes); c++ {
			cellWidth += resolvedColSizes[item.col+c]
			if c > 0 {
				cellWidth += columnGap
			}
		}
		cellHeight := 0.0
		for r := 0; r < item.rowSpan && item.row+r < len(resolvedRowSizes); r++ {
			cellHeight += resolvedRowSizes[item.row+r]
			if r > 0 {
				cellHeight += rowGap
			}
		}

		// Create a temporary cell parent so percentage heights resolve against cell size
		cellParent := &Box{
			Width:  cellWidth,
			Height: cellHeight,
			Style:  style,
			Parent: box,
		}
		// Re-layout child with correct cell width
		childBox := le.layoutNode(item.child, 0, 0, cellWidth, computedStyles, cellParent)
		if childBox == nil {
			continue
		}
		itemBoxes[i] = childBox

		// Grid items default to stretch (CSS Grid §6.2)
		itemJustify := justifyItems
		itemAlign := alignItems

		// Apply item alignment
		childTotalWidth := childBox.Width + childBox.Margin.Left + childBox.Margin.Right
		if childTotalWidth < cellWidth {
			switch itemJustify {
			case css.JustifyItemsCenter:
				childBox.X = (cellWidth - childTotalWidth) / 2
			case css.JustifyItemsEnd:
				childBox.X = cellWidth - childTotalWidth
			default: // start or stretch
				// For stretch, the item should fill the cell
				childBox.Width = cellWidth - childBox.Margin.Left - childBox.Margin.Right
				childBox.X = 0
			}
		}

		childTotalHeight := childBox.Height + childBox.Margin.Top + childBox.Margin.Bottom +
			childBox.Padding.Top + childBox.Padding.Bottom +
			childBox.Border.Top + childBox.Border.Bottom
		if childTotalHeight < cellHeight {
			switch itemAlign {
			case css.AlignItemsCenter:
				childBox.Y = (cellHeight - childTotalHeight) / 2
			case css.AlignItemsFlexEnd:
				childBox.Y = cellHeight - childTotalHeight
			default: // stretch
				childBox.Height = cellHeight - childBox.Margin.Top - childBox.Margin.Bottom -
					childBox.Padding.Top - childBox.Padding.Bottom -
					childBox.Border.Top - childBox.Border.Bottom
				childBox.Y = 0
			}
		}

		// Position in cell
		cellX := contentX + colOffsets[item.col]
		cellY := contentY + rowOffsets[item.row]

		deltaX := cellX - childBox.X
		deltaY := cellY - childBox.Y
		childBox.X = cellX
		childBox.Y = cellY
		le.repositionFlexItemChildren(childBox, deltaX, deltaY)
		childBox.Parent = box

		box.Children = append(box.Children, childBox)
	}

	// Update container height if not explicit
	if !hasExplicitHeight {
		box.Height = actualContentHeight
	}

	return box
}

// resolveTrackSizes resolves auto and fr tracks to pixel sizes.
func resolveTrackSizes(tracks []css.GridTrack, autoSizes []float64, containerSize, gap float64, hasDefiniteSize, stretch bool) []float64 {
	sizes := make([]float64, len(tracks))
	totalGap := float64(len(tracks)-1) * gap
	if len(tracks) <= 1 {
		totalGap = 0
	}

	// First pass: assign fixed sizes and count flexible space
	usedSpace := totalGap
	totalFr := 0.0
	autoCount := 0

	for i, t := range tracks {
		if t.IsMinMax {
			if t.MaxFr > 0 {
				if hasDefiniteSize {
					// minmax(X, Nfr) — acts like fr track with a minimum
					sizes[i] = t.MinSize // start at minimum
					usedSpace += sizes[i]
					totalFr += t.MaxFr
				} else {
					// Indefinite container: use auto content size, clamped to min
					sizes[i] = autoSizes[i]
					if sizes[i] < t.MinSize {
						sizes[i] = t.MinSize
					}
					usedSpace += sizes[i]
				}
			} else if t.MaxAuto {
				// minmax(X, auto) — use auto sizing
				sizes[i] = autoSizes[i]
				if sizes[i] < t.MinSize {
					sizes[i] = t.MinSize
				}
				usedSpace += sizes[i]
				autoCount++
			} else {
				// minmax(X, Ypx) — use max as fixed size, clamp to min
				size := t.MaxSize
				if size < t.MinSize {
					size = t.MinSize
				}
				sizes[i] = size
				usedSpace += sizes[i]
			}
		} else if t.Percent > 0 {
			// Percentage track: resolve against container size
			if hasDefiniteSize {
				sizes[i] = t.Percent * containerSize / 100
			}
			usedSpace += sizes[i]
		} else if t.Fr > 0 {
			if hasDefiniteSize {
				totalFr += t.Fr
			} else {
				// Indefinite: fr track uses auto content size
				sizes[i] = autoSizes[i]
				usedSpace += sizes[i]
			}
		} else if t.Auto || t.MinContent || t.MaxContent {
			sizes[i] = autoSizes[i]
			usedSpace += sizes[i]
			autoCount++
		} else {
			sizes[i] = t.Size
			usedSpace += sizes[i]
		}
	}

	// Distribute remaining space to fr tracks
	remaining := containerSize - usedSpace
	if remaining < 0 {
		remaining = 0
	}

	if totalFr > 0 && hasDefiniteSize {
		frSize := remaining / totalFr
		for i, t := range tracks {
			if t.Fr > 0 {
				sizes[i] = t.Fr * frSize
			} else if t.IsMinMax && t.MaxFr > 0 {
				// minmax with fr max: start from minimum, grow with fr share
				sizes[i] = t.MinSize + t.MaxFr*frSize
			}
		}
	}

	// Stretch: distribute remaining space to auto tracks
	if stretch && autoCount > 0 && hasDefiniteSize {
		// Recalculate remaining after fr distribution
		used := totalGap
		for _, s := range sizes {
			used += s
		}
		extra := containerSize - used
		if extra > 0 {
			perTrack := extra / float64(autoCount)
			for i, t := range tracks {
				if t.Auto {
					sizes[i] += perTrack
				}
			}
		}
	}

	return sizes
}

// sumTracks returns the total size of tracks plus gaps.
func sumTracks(sizes []float64, gap float64) float64 {
	total := 0.0
	for i, s := range sizes {
		total += s
		if i > 0 {
			total += gap
		}
	}
	return total
}

// computeContentDistribution computes the X/Y offset for each track
// based on justify-content / align-content.
func computeContentDistribution(trackSizes []float64, containerSize, gap float64, alignment interface{}) []float64 {
	n := len(trackSizes)
	offsets := make([]float64, n)
	if n == 0 {
		return offsets
	}

	totalTrackSize := sumTracks(trackSizes, gap)
	freeSpace := containerSize - totalTrackSize
	if freeSpace < 0 {
		freeSpace = 0
	}

	// Determine alignment type from either JustifyContent or AlignContent
	alignStr := ""
	switch v := alignment.(type) {
	case css.JustifyContent:
		alignStr = string(v)
	case css.AlignContent:
		alignStr = string(v)
	}

	switch alignStr {
	case "space-between":
		if n <= 1 {
			offsets[0] = 0
		} else {
			spacing := freeSpace / float64(n-1)
			offset := 0.0
			for i := range trackSizes {
				offsets[i] = offset
				offset += trackSizes[i] + gap + spacing
			}
			return offsets
		}
	case "space-around":
		spacing := freeSpace / float64(n)
		offset := spacing / 2
		for i := range trackSizes {
			offsets[i] = offset
			offset += trackSizes[i] + gap + spacing
		}
		return offsets
	case "space-evenly":
		spacing := freeSpace / float64(n+1)
		offset := spacing
		for i := range trackSizes {
			offsets[i] = offset
			offset += trackSizes[i] + gap + spacing
		}
		return offsets
	case "center":
		offset := freeSpace / 2
		for i := range trackSizes {
			offsets[i] = offset
			offset += trackSizes[i] + gap
		}
		return offsets
	case "flex-end", "end":
		offset := freeSpace
		for i := range trackSizes {
			offsets[i] = offset
			offset += trackSizes[i] + gap
		}
		return offsets
	}

	// Default: start / stretch (stretch already applied to track sizes)
	offset := 0.0
	for i := range trackSizes {
		offsets[i] = offset
		offset += trackSizes[i] + gap
	}
	return offsets
}

// Ensure math import is used
var _ = math.Max
