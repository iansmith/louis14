package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"math"
	"strings"
)

// GridCell represents a single cell in the grid
type GridCell struct {
	Row        int
	Column     int
	RowSpan    int
	ColumnSpan int
	Box        *Box
}

// expandAutoFillTracks replaces auto-fill/auto-fit sentinel tracks with
// the correct number of repeated template tracks for the given container size.
// For auto-fill: fills the container as many times as fits (at least once).
// For auto-fit: same as auto-fill but collapses empty tracks to 0.
func expandAutoFillTracks(tracks []css.GridTrack, containerSize, gap float64) []css.GridTrack {
	// Check if any track is an auto-fill or auto-fit sentinel
	hasSentinel := false
	for _, t := range tracks {
		if t.AutoFill || t.AutoFit {
			hasSentinel = true
			break
		}
	}
	if !hasSentinel {
		return tracks
	}

	var result []css.GridTrack
	for _, t := range tracks {
		if !t.AutoFill && !t.AutoFit {
			result = append(result, t)
			continue
		}
		// Compute the minimum width of one template repetition
		template := t.AutoTemplate
		if len(template) == 0 {
			// Empty template — treat as a single auto track
			result = append(result, css.GridTrack{Auto: true})
			continue
		}
		templateMinWidth := 0.0
		for _, tt := range template {
			if tt.Size > 0 {
				templateMinWidth += tt.Size
			} else if tt.IsMinMax && tt.MinSize > 0 {
				templateMinWidth += tt.MinSize
			} else if tt.Fr == 0 {
				// auto/min-content/max-content: use a nominal 0 (will be sized by content)
				// but for counting purposes, treat as minimal non-zero contribution
				templateMinWidth += 0
			}
			// fr tracks: counted as 0 minimum for auto-fill/fit calculation
		}

		// Compute count = max(1, floor((containerSize + gap) / (templateMinWidth + gap)))
		// but only if templateMinWidth > 0
		count := 1
		if templateMinWidth > 0 && containerSize > 0 {
			// Number of repetitions that fit: each repetition takes templateMinWidth + one gap
			// except the last one which doesn't need a trailing gap
			// count = floor((containerSize + gap) / (templateMinWidth + gap))
			unitSize := templateMinWidth + gap
			computed := int((containerSize + gap) / unitSize)
			if computed < 1 {
				computed = 1
			}
			count = computed
		}

		for i := 0; i < count; i++ {
			result = append(result, template...)
		}
	}
	return result
}

// layoutGridContainer handles CSS Grid layout.
// establishedHeight > 0 means the parent (e.g. flex) has already determined a
// definite height for this grid; use it as the container height even when no
// explicit "height" CSS property is present.
func (le *LayoutEngine) layoutGridContainer(
	node *html.Node,
	x, y, availableWidth float64,
	establishedHeight float64,
	style *css.Style,
	dir Dir,
	computedStyles map[*html.Node]*css.Style,
	parent *Box,
) *Box {
	// Get grid properties (with named line maps for named line placement resolution)
	columnTracks, colLineNames := style.GetGridTemplateColumnsWithNames()
	rowTracks, rowLineNames := style.GetGridTemplateRowsWithNames()

	// Handle subgrid: when grid-template-columns/rows is "subgrid", the child grid
	// inherits its tracks from the parent. As a simplified fallback, we treat it as
	// a single auto-sized track (fills available width), which prevents crashes and
	// produces reasonable layout when the parent tracks aren't directly accessible.
	if style.GetGridTemplateColumnsIsSubgrid() {
		// Replace the subgrid sentinel with a single auto track
		// This is the simplified approach: treat as auto-sized
		columnTracks = []css.GridTrack{{Auto: true}}
	}
	if style.GetGridTemplateRowsIsSubgrid() {
		rowTracks = []css.GridTrack{{Auto: true}}
	}

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
	// CSS Intrinsic Sizing: detect min-content/max-content/fit-content keywords
	intrinsicWidthKeyword := ""
	if widthVal, ok := style.Get("width"); ok {
		if widthVal == "min-content" || widthVal == "max-content" || widthVal == "fit-content" {
			intrinsicWidthKeyword = widthVal
		}
	}

	if intrinsicWidthKeyword != "" {
		// Intrinsic keyword: lay out tracks without a definite container width;
		// afterwards, set containerWidth from actualContentWidth.
		containerWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
		hasExplicitWidth = false
	} else if w, ok := style.GetLength("width"); ok {
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
	} else if establishedHeight > 0 {
		// Height established by parent context (e.g. flex layout assigned a definite height)
		containerHeight = establishedHeight
		hasExplicitHeight = true
	}

	// Expand auto-fill and auto-fit repeat() tracks now that container size is known
	columnTracks = expandAutoFillTracks(columnTracks, containerWidth, columnGap)
	rowTracks = expandAutoFillTracks(rowTracks, containerHeight, rowGap)

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

	numColTracks := len(columnTracks)
	numRowTracks := len(rowTracks)
	autoFlow := style.GetGridAutoFlow() // "row" or "column"

	// Expand display:contents children so their children participate as direct grid items
	gridChildren := le.flattenContentsChildren(node, computedStyles)

	// Categorize children for proper CSS Grid auto-placement ordering
	type pendingItem struct {
		child      *html.Node
		childStyle *css.Style
		// Explicit placements (0 = auto)
		explicitRow, explicitCol     int
		rowSpan, colSpan             int
		hasExplicitRow, hasExplicitCol bool
	}
	var pendingItems []pendingItem

	for _, child := range gridChildren {
		// CSS Grid §6: Non-whitespace text nodes become anonymous block grid items.
		if child.Type == html.TextNode {
			if strings.TrimSpace(child.Text) == "" {
				continue
			}
			// Wrap in a synthetic block element so it participates in grid placement.
			anonStyle := css.NewStyle()
			anonStyle.Set("display", "block")
			// Inherit font/color from grid container
			for _, prop := range []string{"font-size", "font-weight", "font-family", "font-style", "color"} {
				if v, ok := style.Get(prop); ok {
					anonStyle.Set(prop, v)
				}
			}
			anonNode := &html.Node{
				Type:     html.ElementNode,
				TagName:  "div",
				Children: []*html.Node{child},
				Parent:   node,
			}
			computedStyles[anonNode] = anonStyle
			le.syntheticStyles[anonNode] = anonStyle
			pendingItems = append(pendingItems, pendingItem{
				child: anonNode, childStyle: anonStyle,
				rowSpan: 1, colSpan: 1,
			})
			continue
		}
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

		gridColumn := childStyle.GetGridColumnWithNames(colLineNames)
		gridRow := childStyle.GetGridRowWithNames(rowLineNames)
		gridAreaName := childStyle.GetGridArea()

		pi := pendingItem{child: child, childStyle: childStyle, rowSpan: 1, colSpan: 1}

		// Check if grid-area references a named template area
		if gridAreaName != "" && templateAreas != nil {
			if areaInfo, ok := templateAreas[gridAreaName]; ok {
				pi.explicitCol = areaInfo.ColStart
				pi.colSpan = areaInfo.ColEnd - areaInfo.ColStart
				pi.explicitRow = areaInfo.RowStart
				pi.rowSpan = areaInfo.RowEnd - areaInfo.RowStart
				pi.hasExplicitCol = true
				pi.hasExplicitRow = true
			}
		}

		if !pi.hasExplicitCol && !pi.hasExplicitRow {
			if gridColumn != nil {
				if gridColumn.IsSpan {
					pi.colSpan = gridColumn.SpanCount
					if pi.colSpan < 1 {
						pi.colSpan = 1
					}
					// no explicit col start
				} else {
					pi.explicitCol = gridColumn.Start
					pi.colSpan = gridColumn.End - gridColumn.Start
					if pi.colSpan < 1 {
						pi.colSpan = 1
					}
					pi.hasExplicitCol = true
				}
			}
			if gridRow != nil {
				if gridRow.IsSpan {
					pi.rowSpan = gridRow.SpanCount
					if pi.rowSpan < 1 {
						pi.rowSpan = 1
					}
					// no explicit row start
				} else {
					pi.explicitRow = gridRow.Start
					pi.rowSpan = gridRow.End - gridRow.Start
					if pi.rowSpan < 1 {
						pi.rowSpan = 1
					}
					pi.hasExplicitRow = true
				}
			}
		}

		pendingItems = append(pendingItems, pi)
	}

	// occupancy tracks which cells are taken; grows dynamically
	type occKey struct{ row, col int }
	occupied := make(map[occKey]bool)

	markOccupied := func(row, col, rowSpan, colSpan int) {
		for r := row; r < row+rowSpan; r++ {
			for c := col; c < col+colSpan; c++ {
				occupied[occKey{r, c}] = true
			}
		}
	}

	isFree := func(row, col, rowSpan, colSpan int) bool {
		for r := row; r < row+rowSpan; r++ {
			for c := col; c < col+colSpan; c++ {
				if occupied[occKey{r, c}] {
					return false
				}
			}
		}
		return true
	}

	// Auto-placement cursor
	cursorRow := 0
	cursorCol := 0

	// findNextFreeCell finds the next free cell for an item starting at/after (cursorRow, cursorCol)
	// in row-flow mode, searching across columns then rows
	findNextFreeRowFlow := func(rowSpan, colSpan int) (row, col int) {
		r := cursorRow
		c := cursorCol
		nCols := numColTracks
		if nCols == 0 {
			nCols = 1
		}
		for {
			if c+colSpan > nCols {
				c = 0
				r++
			}
			if isFree(r, c, rowSpan, colSpan) {
				return r, c
			}
			c++
			if c+colSpan > nCols {
				c = 0
				r++
			}
		}
	}

	findNextFreeColFlow := func(rowSpan, colSpan int) (row, col int) {
		r := cursorRow
		c := cursorCol
		nRows := numRowTracks
		if nRows == 0 {
			nRows = 1
		}
		for {
			if r+rowSpan > nRows {
				r = 0
				c++
			}
			if isFree(r, c, rowSpan, colSpan) {
				return r, c
			}
			r++
			if r+rowSpan > nRows {
				r = 0
				c++
			}
		}
	}

	// Process items in CSS Grid order:
	// 1. Items with explicit row AND column
	// 2. Items with explicit row only (need auto-column)
	// 3. Items with explicit column only (need auto-row) - simple cursor per column
	// 4. Fully auto items

	// Pass 1: Place items with explicit row AND column
	for idx := range pendingItems {
		pi := &pendingItems[idx]
		if !pi.hasExplicitRow || !pi.hasExplicitCol {
			continue
		}
		cellRow := pi.explicitRow - 1
		cellCol := pi.explicitCol - 1
		items = append(items, gridItemInfo{
			child: pi.child, childStyle: pi.childStyle,
			row: cellRow, col: cellCol,
			rowSpan: pi.rowSpan, colSpan: pi.colSpan,
		})
		markOccupied(cellRow, cellCol, pi.rowSpan, pi.colSpan)
		if cellCol+pi.colSpan > maxCol {
			maxCol = cellCol + pi.colSpan
		}
		if cellRow+pi.rowSpan > maxRow {
			maxRow = cellRow + pi.rowSpan
		}
		pi.explicitRow = -1 // mark as placed
	}

	// Pass 2: Place items with explicit row only (auto-column, row-flow)
	// For each such item, find the first free column in the given row(s)
	for idx := range pendingItems {
		pi := &pendingItems[idx]
		if pi.explicitRow == -1 {
			continue // already placed
		}
		if !pi.hasExplicitRow || pi.hasExplicitCol {
			continue
		}
		cellRow := pi.explicitRow - 1
		// Find free column in this row
		c := 0
		nCols := numColTracks
		if nCols == 0 {
			nCols = 1
		}
		for !isFree(cellRow, c, pi.rowSpan, pi.colSpan) {
			c++
		}
		cellCol := c
		items = append(items, gridItemInfo{
			child: pi.child, childStyle: pi.childStyle,
			row: cellRow, col: cellCol,
			rowSpan: pi.rowSpan, colSpan: pi.colSpan,
		})
		markOccupied(cellRow, cellCol, pi.rowSpan, pi.colSpan)
		if cellCol+pi.colSpan > maxCol {
			maxCol = cellCol + pi.colSpan
		}
		if cellRow+pi.rowSpan > maxRow {
			maxRow = cellRow + pi.rowSpan
		}
		pi.explicitRow = -1 // mark as placed
	}

	// Pass 3 & 4: Place remaining items (explicit col only or fully auto)
	// Use cursor-based placement with occupancy tracking
	for idx := range pendingItems {
		pi := &pendingItems[idx]
		if pi.explicitRow == -1 {
			continue // already placed
		}

		var cellRow, cellCol int

		if pi.hasExplicitCol {
			// Explicit col only: auto-row per column
			cellCol = pi.explicitCol - 1
			// Find first free row in this column
			r := 0
			for !isFree(r, cellCol, pi.rowSpan, pi.colSpan) {
				r++
			}
			cellRow = r
		} else {
			// Fully auto: use cursor
			if autoFlow == "column" {
				cellRow, cellCol = findNextFreeColFlow(pi.rowSpan, pi.colSpan)
			} else {
				cellRow, cellCol = findNextFreeRowFlow(pi.rowSpan, pi.colSpan)
			}
			// Advance cursor past this item
			if autoFlow == "column" {
				cursorRow = cellRow + pi.rowSpan
				cursorCol = cellCol
				nRows := numRowTracks
				if nRows == 0 {
					nRows = 1
				}
				if cursorRow >= nRows {
					cursorRow = 0
					cursorCol = cellCol + pi.colSpan
				}
			} else {
				cursorCol = cellCol + pi.colSpan
				cursorRow = cellRow
				nCols := numColTracks
				if nCols == 0 {
					nCols = 1
				}
				if cursorCol >= nCols {
					cursorCol = 0
					cursorRow = cellRow + 1
				}
			}
		}

		items = append(items, gridItemInfo{
			child: pi.child, childStyle: pi.childStyle,
			row: cellRow, col: cellCol,
			rowSpan: pi.rowSpan, colSpan: pi.colSpan,
		})
		markOccupied(cellRow, cellCol, pi.rowSpan, pi.colSpan)
		if cellCol+pi.colSpan > maxCol {
			maxCol = cellCol + pi.colSpan
		}
		if cellRow+pi.rowSpan > maxRow {
			maxRow = cellRow + pi.rowSpan
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

	// items have been placed with proper CSS Grid auto-placement algorithm

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

		childBox := le.layoutNode(item.child, 0, 0, prelimWidth, dir, computedStyles, nil)
		itemBoxes[i] = childBox
		if childBox == nil {
			continue
		}

		// Update auto column sizes from content (columns = inline-axis tracks)
		// Include auto, fr, and minmax tracks — all need content-based sizing for intrinsic/indefinite paths
		if item.colSpan == 1 && item.col < len(columnTracks) {
			t := columnTracks[item.col]
			if t.Auto || t.Fr > 0 || (t.IsMinMax && t.MaxFr > 0) || t.MinContent || t.MaxContent || t.IsFitContent {
				totalW := dir.InlineSize(childBox) + dir.InlineStartEdge(childBox.Margin) + dir.InlineEndEdge(childBox.Margin)
				// CSS Sizing: when an item has an intrinsic max-width/width keyword
				// (max-content, min-content, fit-content), layoutNode with 0 available
				// width gives an incorrect contribution (block fills 0 → only padding).
				// Use ComputeMinMaxSizes to get the true max-content contribution.
				if mwVal, hasMW := item.childStyle.Get("max-width"); hasMW &&
					(mwVal == "max-content" || mwVal == "min-content" || mwVal == "fit-content") {
					sizes := le.ComputeMinMaxSizes(item.child, NewConstraintSpace(containerWidth, -1), item.childStyle)
					intrinsicW := sizes.MaxContentSize + dir.InlineStartEdge(childBox.Margin) + dir.InlineEndEdge(childBox.Margin)
					if intrinsicW > totalW {
						totalW = intrinsicW
					}
				}
				if totalW > autoColSizes[item.col] {
					autoColSizes[item.col] = totalW
				}
			}
		}
		// Update auto row sizes from content (rows = block-axis tracks)
		if item.rowSpan == 1 && item.row < len(rowTracks) {
			t := rowTracks[item.row]
			if t.Auto || t.Fr > 0 || (t.IsMinMax && t.MaxFr > 0) || t.MinContent || t.MaxContent {
				// Use Dir-aware block-size (border-box) + block-axis margins for track sizing
				totalH := dir.BlockSize(childBox) + dir.BlockStartEdge(childBox.Margin) + dir.BlockEndEdge(childBox.Margin)
				if totalH > autoRowSizes[item.row] {
					autoRowSizes[item.row] = totalH
				}
			}
		}
		// Handle column-spanning items: distribute content to auto/fr tracks.
		// CSS Grid §12.5: spanning items contribute their size to spanned tracks
		// after subtracting the fixed-size tracks.
		if item.colSpan > 1 {
			totalW := dir.InlineSize(childBox) + dir.InlineStartEdge(childBox.Margin) + dir.InlineEndEdge(childBox.Margin)
			// Sum of fixed (non-auto, non-fr) track sizes in the span
			fixedW := 0.0
			autoFrCount := 0
			for c := 0; c < item.colSpan && item.col+c < len(columnTracks); c++ {
				t := columnTracks[item.col+c]
				if !t.Auto && t.Fr == 0 && !(t.IsMinMax && t.MaxFr > 0) && !t.MinContent && !t.MaxContent && t.Percent == 0 {
					fixedW += t.Size
				} else {
					autoFrCount++
				}
			}
			// Account for column gaps within the span
			fixedW += columnGap * float64(item.colSpan-1)
			remainder := totalW - fixedW
			if remainder > 0 && autoFrCount > 0 {
				perTrack := remainder / float64(autoFrCount)
				for c := 0; c < item.colSpan && item.col+c < len(columnTracks); c++ {
					t := columnTracks[item.col+c]
					if t.Auto || t.Fr > 0 || (t.IsMinMax && t.MaxFr > 0) || t.MinContent || t.MaxContent {
						if perTrack > autoColSizes[item.col+c] {
							autoColSizes[item.col+c] = perTrack
						}
					}
				}
			}
		}
	}

	// Phase 2: Resolve track sizes
	// CSS Grid §12: justify-content:normal behaves as stretch for auto tracks (maximize tracks step).
	// Only skip auto-track stretch when justify-content is explicitly set to a spacing/positioning value.
	_, hasExplicitJustify := style.Get("justify-content")
	colStretch := !hasExplicitJustify || justifyContent == css.JustifyContentStretch
	resolvedColSizes := resolveTrackSizes(columnTracks, autoColSizes, containerWidth, columnGap, hasExplicitWidth, colStretch)
	// align-content default is already AlignContentStretch, so rows stretch by default.
	_, hasExplicitAlign := style.Get("align-content")
	rowStretch := !hasExplicitAlign || alignContent == css.AlignContentStretch
	resolvedRowSizes := resolveTrackSizes(rowTracks, autoRowSizes, containerHeight, rowGap, hasExplicitHeight, rowStretch)

	// Calculate actual content dimensions from resolved tracks
	actualContentWidth := sumTracks(resolvedColSizes, columnGap)
	actualContentHeight := sumTracks(resolvedRowSizes, rowGap)

	// For inline-grid without explicit width, shrink-wrap to content
	if isInlineGrid && !hasExplicitWidth {
		containerWidth = actualContentWidth
	}
	// For intrinsic keyword widths (min-content/max-content/fit-content), use actual content width
	if intrinsicWidthKeyword != "" {
		containerWidth = actualContentWidth
		hasExplicitWidth = true
	}

	if !hasExplicitHeight {
		containerHeight = actualContentHeight
	}

	// Phase 3: Compute content distribution offsets
	colOffsets := computeContentDistribution(resolvedColSizes, containerWidth, columnGap, justifyContent)
	rowOffsets := computeContentDistribution(resolvedRowSizes, containerHeight, rowGap, alignContent)

	// Create container box. Width and Height are border-box (content + padding + border),
	// consistent with block layout (layout_block.go line 513) and render.go expectations
	// (render.go line 1913: bgWidth := box.Width // Border-box dimensions).
	box := &Box{
		Node:     node,
		Style:    style,
		X:        actualX,
		Y:        y,
		Width:    containerWidth + padding.Left + padding.Right + border.Left + border.Right,
		Height:   containerHeight + padding.Top + padding.Bottom + border.Top + border.Bottom,
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

		// For non-stretch alignment (start/end/center), use fit-content width:
		// fit-content = min(max-content, cellWidth). This ensures items with
		// negative inline margins (e.g. margin-inline-end:-1px) wrap correctly.
		layoutWidth := cellWidth
		if justifyItems == css.JustifyItemsStart || justifyItems == css.JustifyItemsEnd || justifyItems == css.JustifyItemsCenter {
			maxContent := le.computeGridItemMaxContentWidth(item.child, item.childStyle, computedStyles)
			if maxContent > 0 && maxContent < cellWidth {
				layoutWidth = maxContent
			}
		}
		// Create a temporary cell parent so percentage heights resolve against cell size
		cellParent := &Box{
			Width:  layoutWidth,
			Height: cellHeight,
			Style:  style,
			Parent: box,
		}
		// Re-layout child with correct cell width (or fit-content for non-stretch)
		childBox := le.layoutNode(item.child, 0, 0, layoutWidth, dir, computedStyles, cellParent)
		if childBox == nil {
			continue
		}
		itemBoxes[i] = childBox

		// Grid items default to stretch (CSS Grid §6.2)
		itemJustify := justifyItems
		itemAlign := alignItems

		// Apply item alignment using Dir-aware dimensions
		// Columns = inline-axis, Rows = block-axis
		childTotalInline := dir.InlineSize(childBox) + dir.InlineStartEdge(childBox.Margin) + dir.InlineEndEdge(childBox.Margin)
		if childTotalInline < cellWidth {
			switch itemJustify {
			case css.JustifyItemsCenter:
				childBox.X = (cellWidth - childTotalInline) / 2
			case css.JustifyItemsEnd:
				childBox.X = cellWidth - childTotalInline
			case css.JustifyItemsStart:
				// start: position at inline-start edge, do not stretch
				childBox.X = 0
			default: // stretch
				// CSS Grid §6.2: stretch only applies when inline-size is auto.
				// An explicit width/height (including max-content, min-content) prevents stretching.
				inlineSizeProp := dir.InlineSizeProp()
				sizeVal, hasSizeVal := item.childStyle.Get(inlineSizeProp)
				inlineSizeIsAuto := !hasSizeVal || sizeVal == "auto"
				if inlineSizeIsAuto {
					dir.SetInlineSize(childBox, cellWidth-dir.InlineStartEdge(childBox.Margin)-dir.InlineEndEdge(childBox.Margin))
				}
				childBox.X = 0
			}
		}

		// Block-axis alignment (rows = block-axis)
		childTotalBlock := dir.BlockSize(childBox) + dir.BlockStartEdge(childBox.Margin) + dir.BlockEndEdge(childBox.Margin)
		if childTotalBlock < cellHeight {
			switch itemAlign {
			case css.AlignItemsCenter:
				childBox.Y = (cellHeight - childTotalBlock) / 2
			case css.AlignItemsFlexEnd:
				childBox.Y = cellHeight - childTotalBlock
			default: // stretch — CSS Grid §6.2: only stretch when block size is auto
				blockSizeProp := dir.BlockSizeProp()
				sizeVal, hasSizeVal := item.childStyle.Get(blockSizeProp)
				blockSizeIsAuto := !hasSizeVal || sizeVal == "auto"
				if blockSizeIsAuto {
					dir.SetBlockSize(childBox, cellHeight-dir.BlockStartEdge(childBox.Margin)-dir.BlockEndEdge(childBox.Margin))
				}
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

	// Update container height if not explicit (border-box: content + padding + border)
	if !hasExplicitHeight {
		box.Height = actualContentHeight + padding.Top + padding.Bottom + border.Top + border.Bottom
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
		} else if t.IsFitContent {
			// fit-content(X) = min(max-content, max(min-content, X))
			// autoSizes[i] holds min-content contribution from Phase 1.
			// Approximation: use max(autoSizes[i], FitContentMax) bounded by FitContentMax
			// when autoSizes[i] > FitContentMax, the min-content exceeds the limit → use autoSizes[i]
			if autoSizes[i] > t.FitContentMax {
				sizes[i] = autoSizes[i]
			} else {
				sizes[i] = t.FitContentMax
			}
			usedSpace += sizes[i]
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
