package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"math"
	"sort"
)

func (le *LayoutEngine) layoutFlex(flexBox *Box, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style) {
	direction := flexBox.Style.GetFlexDirection()
	wrap := flexBox.Style.GetFlexWrap()
	justifyContent := flexBox.Style.GetJustifyContent()
	alignItems := flexBox.Style.GetAlignItems()
	alignContent := flexBox.Style.GetAlignContent()

	isRow := direction == css.FlexDirectionRow || direction == css.FlexDirectionRowReverse
	isReverse := direction == css.FlexDirectionRowReverse || direction == css.FlexDirectionColumnReverse
	isWrapReverse := wrap == css.FlexWrapWrapReverse

	// Container content box dimensions (inside padding+border)
	contentBoxWidth := flexBox.Width - flexBox.Padding.Left - flexBox.Padding.Right - flexBox.Border.Left - flexBox.Border.Right
	contentBoxHeight := flexBox.Height - flexBox.Padding.Top - flexBox.Padding.Bottom - flexBox.Border.Top - flexBox.Border.Bottom

	// Determine main axis available size
	var mainSize, crossSize float64
	hasDefiniteCross := false
	if isRow {
		mainSize = contentBoxWidth
		if contentBoxHeight > 0 {
			crossSize = contentBoxHeight
			hasDefiniteCross = true
		}
	} else {
		if contentBoxHeight > 0 {
			mainSize = contentBoxHeight
		} else {
			mainSize = math.MaxFloat64 // indefinite
		}
		// Only treat cross size as definite if there's an explicit width
		if _, hasExplicitWidth := flexBox.Style.GetLength("width"); hasExplicitWidth {
			crossSize = contentBoxWidth
			hasDefiniteCross = true
		} else if _, hasExplicitPctWidth := flexBox.Style.GetPercentage("width"); hasExplicitPctWidth {
			crossSize = contentBoxWidth
			hasDefiniteCross = true
		} else if contentBoxWidth > 0 {
			crossSize = contentBoxWidth
			hasDefiniteCross = true
		}
	}

	// Get gap values
	rowGap := 0.0
	colGap := 0.0
	if val, ok := flexBox.Style.Get("row-gap"); ok {
		if g, ok := css.ParseLengthWithFontSize(val, flexBox.Style.GetFontSize()); ok {
			rowGap = g
		}
	}
	if val, ok := flexBox.Style.Get("column-gap"); ok {
		if pct, ok := css.ParsePercentage(val); ok {
			// column-gap percentages always resolve against the inline size (width)
			colGap = contentBoxWidth * pct / 100
		} else if g, ok := css.ParseLengthWithFontSize(val, flexBox.Style.GetFontSize()); ok {
			colGap = g
		}
	}
	// For flex, column-gap is the main-axis gap (row direction), row-gap is cross-axis gap
	var mainGap, crossGap float64
	if isRow {
		mainGap = colGap
		crossGap = rowGap
	} else {
		mainGap = rowGap
		crossGap = colGap
	}

	// Step 1: Create flex items by laying out children to get intrinsic sizes
	contentStartX := flexBox.X + flexBox.Border.Left + flexBox.Padding.Left
	contentStartY := flexBox.Y + flexBox.Border.Top + flexBox.Padding.Top
	items := le.createFlexItemsProper(flexBox, contentStartX, contentStartY, contentBoxWidth, computedStyles, isRow)

	// Step 2: Sort by order property
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})

	// Step 3: Determine flex base size and hypothetical main size for each item
	for _, item := range items {
		basisVal := item.Box.Style.GetFlexBasisValue()
		if basisVal.IsAuto {
			// flex-basis: auto → use the item's main size property
			if isRow {
				if w, ok := item.Box.Style.GetLength("width"); ok {
					item.FlexBasis = w
				} else {
					// Use content size (already laid out)
					item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
				}
			} else {
				if h, ok := item.Box.Style.GetLength("height"); ok {
					item.FlexBasis = h
				} else {
					item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
				}
			}
		} else if basisVal.IsPercent {
			item.FlexBasis = mainSize * basisVal.Percentage / 100
		} else {
			item.FlexBasis = basisVal.Length
		}

		// Hypothetical main size = flex base size clamped by min/max
		item.HypotheticalMainSize = item.FlexBasis
		// Clamp by min-width: auto
		if item.HypotheticalMainSize < item.AutoMinMain {
			item.HypotheticalMainSize = item.AutoMinMain
		}
		if item.HypotheticalMainSize < 0 {
			item.HypotheticalMainSize = 0
		}
	}

	// Step 3b: For shrink-to-fit flex containers (float, inline-flex, abs pos without
	// explicit width/height), compute the ideal main size from items.
	// CSS Flexbox §9.2: The flex container's main size is its max-content size if
	// the main size property would be auto.
	if isRow {
		if _, hasExplicitWidth := flexBox.Style.GetLength("width"); !hasExplicitWidth {
			if _, hasExplicitPctWidth := flexBox.Style.GetPercentage("width"); !hasExplicitPctWidth {
				// Compute max-content width: sum of all item outer hypothetical main sizes + gaps
				maxContentWidth := 0.0
				for i, item := range items {
					maxContentWidth += item.HypotheticalMainSize + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
					if i > 0 {
						maxContentWidth += mainGap
					}
				}
				// Apply min/max-width constraints
				if minW, ok := flexBox.Style.GetLength("min-width"); ok && maxContentWidth < minW {
					maxContentWidth = minW
				}
				if maxW, ok := flexBox.Style.GetLength("max-width"); ok && maxContentWidth > maxW {
					maxContentWidth = maxW
				}
				// Update container and main size
				contentBoxWidth = maxContentWidth
				flexBox.Width = maxContentWidth + flexBox.Padding.Left + flexBox.Padding.Right + flexBox.Border.Left + flexBox.Border.Right
				mainSize = contentBoxWidth
			}
		}
	}

	// Step 4: Collect items into flex lines
	lines := collectFlexLines(items, mainSize, mainGap, wrap, isRow)

	// Step 5: Resolve flexible lengths for each line
	for _, line := range lines {
		resolveFlexibleLengths(line, mainSize, mainGap, isRow)
	}

	// Step 6: Determine cross sizes
	for _, line := range lines {
		for _, item := range line.Items {
			if isRow {
				// Cross size = height of item
				item.CrossSize = item.outerCrossSize(isRow)
			} else {
				item.CrossSize = item.outerCrossSize(isRow)
			}
		}
		// Line cross size = max item cross size
		maxCross := 0.0
		for _, item := range line.Items {
			if item.CrossSize > maxCross {
				maxCross = item.CrossSize
			}
		}
		line.CrossSize = maxCross
	}

	// Single-line container with definite cross size: use container's cross size
	if wrap == css.FlexWrapNowrap && hasDefiniteCross && len(lines) == 1 {
		lines[0].CrossSize = crossSize
	}

	// Step 7: Handle align-content: stretch for multi-line
	totalLinesCross := 0.0
	for i, line := range lines {
		totalLinesCross += line.CrossSize
		if i > 0 {
			totalLinesCross += crossGap
		}
	}
	if hasDefiniteCross && alignContent == css.AlignContentStretch && wrap != css.FlexWrapNowrap {
		freeSpace := crossSize - totalLinesCross
		if freeSpace > 0 {
			extra := freeSpace / float64(len(lines))
			for _, line := range lines {
				line.CrossSize += extra
			}
			totalLinesCross = crossSize
		}
	}

	// Step 8: Handle align-items: stretch for each item
	// CSS Flexbox §8.2: Only stretch if the item's cross-size property is auto
	for _, line := range lines {
		for _, item := range line.Items {
			alignment := resolveAlignment(alignItems, item.Box.Style.GetAlignSelf())
			if alignment == css.AlignItemsStretch {
				// Check if item has explicit cross-size (stretch only applies to auto)
				hasExplicitCrossSize := false
				if isRow {
					if _, ok := item.Box.Style.GetLength("height"); ok {
						hasExplicitCrossSize = true
					}
				} else {
					if _, ok := item.Box.Style.GetLength("width"); ok {
						hasExplicitCrossSize = true
					}
				}
				if hasExplicitCrossSize {
					continue
				}
				outerCross := item.outerCrossSize(isRow)
				if outerCross < line.CrossSize {
					// Stretch item to fill line's cross size
					crossMargin := 0.0
					if isRow {
						crossMargin = item.Box.Margin.Top + item.Box.Margin.Bottom
						newHeight := line.CrossSize - crossMargin
						item.Box.Height = newHeight
					} else {
						crossMargin = item.Box.Margin.Left + item.Box.Margin.Right
						newWidth := line.CrossSize - crossMargin
						item.Box.Width = newWidth
					}
					item.CrossSize = line.CrossSize
				}
			}
		}
	}

	// Step 8b: Resolve auto margins on the main axis (CSS Flexbox §8.1)
	// Auto margins absorb remaining free space BEFORE justify-content.
	for _, line := range lines {
		totalItemsMain := 0.0
		for i, item := range line.Items {
			totalItemsMain += item.outerMainSize(isRow)
			if i > 0 {
				totalItemsMain += mainGap
			}
		}
		freeSpace := mainSize - totalItemsMain

		// Preserve original free space for justify-content fallback detection
		originalFreeSpace := freeSpace
		if freeSpace < 0 {
			freeSpace = 0
		}

		// Count auto margins on main axis
		autoMarginCount := 0
		for _, item := range line.Items {
			margin := item.Box.Style.GetMargin()
			if isRow {
				if margin.AutoLeft {
					autoMarginCount++
				}
				if margin.AutoRight {
					autoMarginCount++
				}
			} else {
				if margin.AutoTop {
					autoMarginCount++
				}
				if margin.AutoBottom {
					autoMarginCount++
				}
			}
		}

		if autoMarginCount > 0 && freeSpace > 0 {
			// Distribute free space to auto margins
			autoMarginSize := freeSpace / float64(autoMarginCount)
			for _, item := range line.Items {
				margin := item.Box.Style.GetMargin()
				if isRow {
					if margin.AutoLeft {
						item.Box.Margin.Left = autoMarginSize
					}
					if margin.AutoRight {
						item.Box.Margin.Right = autoMarginSize
					}
				} else {
					if margin.AutoTop {
						item.Box.Margin.Top = autoMarginSize
					}
					if margin.AutoBottom {
						item.Box.Margin.Bottom = autoMarginSize
					}
				}
			}
			// Recalculate freeSpace (should be 0 now)
			freeSpace = 0
		}

		// Step 9: Main-axis alignment (justify-content)
		// CSS Flexbox spec: space-between/space-around/space-evenly fall back to flex-start
		// when free space is negative (overflow) or there's only one item.
		var initialOffset, spacing float64
		switch justifyContent {
		case css.JustifyContentFlexStart:
			initialOffset = 0
		case css.JustifyContentFlexEnd:
			initialOffset = freeSpace
		case css.JustifyContentCenter:
			initialOffset = freeSpace / 2
		case css.JustifyContentSpaceBetween:
			// Fall back to flex-start if overflow or single item
			if originalFreeSpace < 0 || len(line.Items) == 1 {
				initialOffset = 0 // flex-start
			} else if len(line.Items) > 1 {
				spacing = freeSpace / float64(len(line.Items)-1)
			}
		case css.JustifyContentSpaceAround:
			// Fall back to flex-start if overflow, center if single item
			if originalFreeSpace < 0 {
				initialOffset = 0 // flex-start
			} else if len(line.Items) == 1 {
				initialOffset = freeSpace / 2 // center
			} else if len(line.Items) > 0 {
				spacing = freeSpace / float64(len(line.Items))
				initialOffset = spacing / 2
			}
		case css.JustifyContentSpaceEvenly:
			// Fall back to flex-start if overflow, center if single item
			if originalFreeSpace < 0 {
				initialOffset = 0 // flex-start
			} else if len(line.Items) == 1 {
				initialOffset = freeSpace / 2 // center
			} else if len(line.Items) > 0 {
				spacing = freeSpace / float64(len(line.Items)+1)
				initialOffset = spacing
			}
		}

		currentPos := initialOffset
		for i, item := range line.Items {
			if isRow {
				item.MainPos = currentPos + item.Box.Margin.Left
			} else {
				item.MainPos = currentPos + item.Box.Margin.Top
			}
			currentPos += item.outerMainSize(isRow) + spacing
			if i < len(line.Items)-1 {
				currentPos += mainGap
			}
		}
	}

	// Step 10: Cross-axis alignment
	currentCrossPos := 0.0

	// Align content (distribute lines along cross axis)
	if hasDefiniteCross && (len(lines) > 1 || wrap != css.FlexWrapNowrap) {
		freeSpace := crossSize - totalLinesCross
		if freeSpace < 0 {
			freeSpace = 0
		}
		var lineOffsets []float64
		switch alignContent {
		case css.AlignContentFlexStart, css.AlignContentStretch:
			pos := 0.0
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		case css.AlignContentFlexEnd:
			pos := freeSpace
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		case css.AlignContentCenter:
			pos := freeSpace / 2
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		case css.AlignContentSpaceBetween:
			lineSpacing := 0.0
			if len(lines) > 1 {
				lineSpacing = freeSpace / float64(len(lines)-1)
			}
			pos := 0.0
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize + lineSpacing
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		case css.AlignContentSpaceAround:
			lineSpacing := 0.0
			if len(lines) > 0 {
				lineSpacing = freeSpace / float64(len(lines))
			}
			pos := lineSpacing / 2
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize + lineSpacing
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		}

		// Position items within lines using computed offsets
		for lineIdx, line := range lines {
			crossPos := 0.0
			if lineIdx < len(lineOffsets) {
				crossPos = lineOffsets[lineIdx]
			}
			positionItemsCrossAxis(line, crossPos, alignItems, isRow)
		}
	} else {
		// Single-line or no definite cross size
		for i, line := range lines {
			positionItemsCrossAxis(line, currentCrossPos, alignItems, isRow)
			currentCrossPos += line.CrossSize
			if i < len(lines)-1 {
				currentCrossPos += crossGap
			}
		}
	}

	// Step 11: Reverse if needed
	if isReverse {
		// For indefinite main size (e.g., column-reverse with auto height),
		// compute the actual used main size from item positions
		effectiveMainSize := mainSize
		if effectiveMainSize == math.MaxFloat64 {
			effectiveMainSize = 0
			for _, line := range lines {
				for _, item := range line.Items {
					var itemEnd float64
					if isRow {
						itemEnd = item.MainPos + item.Box.Width + item.Box.Margin.Right
					} else {
						itemEnd = item.MainPos + item.Box.Height + item.Box.Margin.Bottom
					}
					if itemEnd > effectiveMainSize {
						effectiveMainSize = itemEnd
					}
				}
			}
		}
		for _, line := range lines {
			for _, item := range line.Items {
				// Mirror main-axis position
				outerMain := item.outerMainSize(isRow)
				if isRow {
					item.MainPos = effectiveMainSize - item.MainPos - (outerMain - item.Box.Margin.Left - item.Box.Margin.Right)
				} else {
					item.MainPos = effectiveMainSize - item.MainPos - (outerMain - item.Box.Margin.Top - item.Box.Margin.Bottom)
				}
			}
		}
	}
	if isWrapReverse && len(lines) > 1 {
		// Reverse line order along cross axis
		totalCross := 0.0
		for i, line := range lines {
			totalCross += line.CrossSize
			if i > 0 {
				totalCross += crossGap
			}
		}
		for _, line := range lines {
			for _, item := range line.Items {
				item.CrossPos = totalCross - item.CrossPos - item.CrossSize
			}
		}
	}

	// Step 12: Set final box positions
	flexBox.Children = flexBox.Children[:0]
	for _, line := range lines {
		for _, item := range line.Items {
			oldX := item.Box.X
			oldY := item.Box.Y
			if isRow {
				item.Box.X = contentStartX + item.MainPos
				item.Box.Y = contentStartY + item.CrossPos
			} else {
				item.Box.X = contentStartX + item.CrossPos
				item.Box.Y = contentStartY + item.MainPos
			}
			// Re-position children relative to new box position
			deltaX := item.Box.X - oldX
			deltaY := item.Box.Y - oldY
			le.repositionFlexItemChildren(item.Box, deltaX, deltaY)
			flexBox.Children = append(flexBox.Children, item.Box)
		}
	}

	// Step 13: Update container auto width for column direction
	if !isRow && !hasDefiniteCross {
		totalCrossSize := 0.0
		for i, line := range lines {
			totalCrossSize += line.CrossSize
			if i > 0 {
				totalCrossSize += crossGap
			}
		}
		flexBox.Width = totalCrossSize + flexBox.Padding.Left + flexBox.Padding.Right + flexBox.Border.Left + flexBox.Border.Right
	}

	// Step 14: Update container auto height
	if !hasDefiniteCross || (isRow && contentBoxHeight == 0) {
		maxBottom := 0.0
		for _, child := range flexBox.Children {
			childBottom := child.Y + child.Height + child.Margin.Bottom - contentStartY
			if childBottom > maxBottom {
				maxBottom = childBottom
			}
		}
		if isRow {
			flexBox.Height = maxBottom + flexBox.Padding.Top + flexBox.Padding.Bottom + flexBox.Border.Top + flexBox.Border.Bottom
		}
	}
	if !isRow && mainSize == math.MaxFloat64 {
		maxBottom := 0.0
		for _, child := range flexBox.Children {
			childBottom := child.Y + child.Height + child.Margin.Bottom - contentStartY
			if childBottom > maxBottom {
				maxBottom = childBottom
			}
		}
		flexBox.Height = maxBottom + flexBox.Padding.Top + flexBox.Padding.Bottom + flexBox.Border.Top + flexBox.Border.Bottom
	}
}

// createFlexItemsProper creates flex items by laying out each child to get proper dimensions.
func (le *LayoutEngine) createFlexItemsProper(flexBox *Box, startX, startY, availableWidth float64, computedStyles map[*html.Node]*css.Style, isRow bool) []*FlexItem {
	items := make([]*FlexItem, 0)

	for _, child := range flexBox.Node.Children {
		if child.Type == html.TextNode {
			// Anonymous flex items for text (skip whitespace-only)
			text := child.Text
			trimmed := ""
			for _, c := range text {
				if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
					trimmed += string(c)
				}
			}
			if trimmed == "" {
				continue
			}
			// TODO: handle anonymous text flex items
			continue
		}
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
			computedStyles[child] = childStyle
		}

		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		// CSS Flexbox §4: Blockification of flex items
		// Children of a flex container have their display value blockified:
		// inline → block, inline-block → block, inline-flex → flex
		display := childStyle.GetDisplay()
		if display == css.DisplayInline || display == css.DisplayInlineBlock {
			childStyle.Set("display", "block")
		} else if display == css.DisplayInlineFlex {
			childStyle.Set("display", "flex")
		}

		// Layout the child to get its intrinsic dimensions
		childBox := le.layoutNode(child, startX, startY, availableWidth, computedStyles, flexBox)

		item := &FlexItem{
			Box:        childBox,
			FlexGrow:   childStyle.GetFlexGrow(),
			FlexShrink: childStyle.GetFlexShrink(),
			Order:      childStyle.GetOrder(),
		}

		// Compute min-width: auto (CSS Flexbox §4.5)
		// For flex items with overflow: visible, min-width/min-height: auto
		// computes to the content-based minimum size
		overflow := "visible"
		if v, ok := childStyle.Get("overflow"); ok {
			overflow = v
		}
		hasExplicitMin := false
		if isRow {
			if _, ok := childStyle.GetLength("min-width"); ok {
				hasExplicitMin = true
			}
		} else {
			if _, ok := childStyle.GetLength("min-height"); ok {
				hasExplicitMin = true
			}
		}
		if !hasExplicitMin && overflow == "visible" {
			item.AutoMinMain = le.computeFlexItemAutoMinMain(child, childStyle, childBox, isRow)
		}

		items = append(items, item)
	}

	return items
}

// collectFlexLines collects flex items into lines based on wrapping rules.
func collectFlexLines(items []*FlexItem, mainSize, mainGap float64, wrap css.FlexWrap, isRow bool) []*FlexLine {
	if wrap == css.FlexWrapNowrap || len(items) == 0 {
		return []*FlexLine{{Items: items}}
	}

	var lines []*FlexLine
	currentLine := &FlexLine{Items: make([]*FlexItem, 0)}
	lineMainSize := 0.0

	for _, item := range items {
		itemMain := item.HypotheticalOuterMain(isRow)
		gapSize := 0.0
		if len(currentLine.Items) > 0 {
			gapSize = mainGap
		}
		if lineMainSize+gapSize+itemMain > mainSize && len(currentLine.Items) > 0 {
			lines = append(lines, currentLine)
			currentLine = &FlexLine{Items: make([]*FlexItem, 0)}
			lineMainSize = 0
			gapSize = 0
		}
		currentLine.Items = append(currentLine.Items, item)
		lineMainSize += gapSize + itemMain
	}
	if len(currentLine.Items) > 0 {
		lines = append(lines, currentLine)
	}
	return lines
}

// resolveFlexibleLengths implements CSS Flexbox spec Section 9.7.
func resolveFlexibleLengths(line *FlexLine, availableMain, mainGap float64, isRow bool) {
	if len(line.Items) == 0 {
		return
	}

	// Account for gaps between items
	totalGaps := mainGap * float64(len(line.Items)-1)
	effectiveAvailable := availableMain - totalGaps

	// Calculate sum of outer hypothetical main sizes
	sumHypothetical := 0.0
	for _, item := range line.Items {
		sumHypothetical += item.HypotheticalMainSize + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
	}

	// Determine whether we're growing or shrinking
	growing := sumHypothetical < effectiveAvailable

	// Freeze inflexible items
	type flexState struct {
		frozen    bool
		targetMain float64
	}
	states := make([]flexState, len(line.Items))
	for i, item := range line.Items {
		if growing && item.FlexGrow == 0 {
			states[i].frozen = true
			states[i].targetMain = item.HypotheticalMainSize
		} else if !growing && item.FlexShrink == 0 {
			states[i].frozen = true
			states[i].targetMain = item.HypotheticalMainSize
		} else {
			states[i].targetMain = item.HypotheticalMainSize
		}
	}

	// Iterative resolution loop
	for iteration := 0; iteration < 10; iteration++ {
		// Check if all frozen
		allFrozen := true
		for _, s := range states {
			if !s.frozen {
				allFrozen = false
				break
			}
		}
		if allFrozen {
			break
		}

		// Calculate remaining free space
		usedSpace := 0.0
		for i, item := range line.Items {
			if states[i].frozen {
				usedSpace += states[i].targetMain + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
			} else {
				usedSpace += item.FlexBasis + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
			}
		}
		freeSpace := effectiveAvailable - usedSpace

		// Distribute space
		if growing {
			totalGrowFactor := 0.0
			for i, item := range line.Items {
				if !states[i].frozen {
					totalGrowFactor += item.FlexGrow
				}
			}
			if totalGrowFactor > 0 {
				for i, item := range line.Items {
					if !states[i].frozen {
						states[i].targetMain = item.FlexBasis + freeSpace*(item.FlexGrow/totalGrowFactor)
					}
				}
			}
		} else {
			// Shrink: weighted by flex-shrink * flex-basis
			totalScaledShrink := 0.0
			for i, item := range line.Items {
				if !states[i].frozen {
					totalScaledShrink += item.FlexShrink * item.FlexBasis
				}
			}
			if totalScaledShrink > 0 {
				for i, item := range line.Items {
					if !states[i].frozen {
						scaledFactor := item.FlexShrink * item.FlexBasis / totalScaledShrink
						states[i].targetMain = item.FlexBasis + freeSpace*scaledFactor
					}
				}
			}
		}

		// Clamp by min/max and detect violations
		totalViolation := 0.0
		for i, item := range line.Items {
			if states[i].frozen {
				continue
			}
			clamped := states[i].targetMain
			// Clamp by min-width: auto (content-based minimum)
			if clamped < item.AutoMinMain {
				clamped = item.AutoMinMain
			}
			if clamped < 0 {
				clamped = 0
			}
			totalViolation += clamped - states[i].targetMain
			states[i].targetMain = clamped
		}

		// Freeze violating items
		if totalViolation == 0 {
			// Freeze all
			for i := range states {
				states[i].frozen = true
			}
		} else if totalViolation > 0 {
			// Freeze items that hit minimum
			for i := range states {
				if !states[i].frozen && states[i].targetMain <= 0 {
					states[i].frozen = true
				}
			}
		} else {
			// Freeze items that hit maximum
			for i := range states {
				if !states[i].frozen {
					states[i].frozen = true // simplified: freeze all on negative violation
				}
			}
		}
	}

	// Apply resolved main sizes to items
	for i, item := range line.Items {
		item.MainSize = states[i].targetMain
		// Update the box's main dimension
		if isRow {
			item.Box.Width = item.MainSize + item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
		} else {
			item.Box.Height = item.MainSize + item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
		}
	}
}

// positionItemsCrossAxis positions items within a line along the cross axis.
func positionItemsCrossAxis(line *FlexLine, crossStart float64, alignItems css.AlignItems, isRow bool) {
	for _, item := range line.Items {
		// CSS Flexbox §8.1: Cross-axis auto margins override align-self
		margin := item.Box.Style.GetMargin()
		hasAutoCrossStart := false
		hasAutoCrossEnd := false
		if isRow {
			hasAutoCrossStart = margin.AutoTop
			hasAutoCrossEnd = margin.AutoBottom
		} else {
			hasAutoCrossStart = margin.AutoLeft
			hasAutoCrossEnd = margin.AutoRight
		}

		if hasAutoCrossStart || hasAutoCrossEnd {
			outerCross := item.outerCrossSize(isRow)
			freeSpace := line.CrossSize - outerCross
			if freeSpace < 0 {
				freeSpace = 0
			}

			crossMarginStart := 0.0
			if isRow {
				crossMarginStart = item.Box.Margin.Top
			} else {
				crossMarginStart = item.Box.Margin.Left
			}

			if hasAutoCrossStart && hasAutoCrossEnd {
				// Both auto: center the item
				autoMargin := freeSpace / 2
				if isRow {
					item.Box.Margin.Top = autoMargin
					item.Box.Margin.Bottom = autoMargin
				} else {
					item.Box.Margin.Left = autoMargin
					item.Box.Margin.Right = autoMargin
				}
				item.CrossPos = crossStart + autoMargin
			} else if hasAutoCrossStart {
				// Only start auto: push to end
				if isRow {
					item.Box.Margin.Top = freeSpace
				} else {
					item.Box.Margin.Left = freeSpace
				}
				item.CrossPos = crossStart + freeSpace
			} else {
				// Only end auto: stay at start
				if isRow {
					item.Box.Margin.Bottom = freeSpace
				} else {
					item.Box.Margin.Right = freeSpace
				}
				item.CrossPos = crossStart + crossMarginStart
			}
			continue
		}

		alignment := resolveAlignment(alignItems, item.Box.Style.GetAlignSelf())
		outerCross := item.outerCrossSize(isRow)
		crossMarginStart := 0.0
		if isRow {
			crossMarginStart = item.Box.Margin.Top
		} else {
			crossMarginStart = item.Box.Margin.Left
		}

		switch alignment {
		case css.AlignItemsFlexStart:
			item.CrossPos = crossStart + crossMarginStart
		case css.AlignItemsFlexEnd:
			item.CrossPos = crossStart + line.CrossSize - outerCross + crossMarginStart
		case css.AlignItemsCenter:
			item.CrossPos = crossStart + (line.CrossSize-outerCross)/2 + crossMarginStart
		case css.AlignItemsStretch:
			item.CrossPos = crossStart + crossMarginStart
		case css.AlignItemsBaseline:
			item.CrossPos = crossStart + crossMarginStart
		}
	}
}

// resolveAlignment resolves align-self: auto to the container's align-items.
func resolveAlignment(alignItems css.AlignItems, alignSelf css.AlignSelf) css.AlignItems {
	switch alignSelf {
	case css.AlignSelfFlexStart:
		return css.AlignItemsFlexStart
	case css.AlignSelfFlexEnd:
		return css.AlignItemsFlexEnd
	case css.AlignSelfCenter:
		return css.AlignItemsCenter
	case css.AlignSelfStretch:
		return css.AlignItemsStretch
	case css.AlignSelfBaseline:
		return css.AlignItemsBaseline
	default: // auto
		return alignItems
	}
}

// repositionFlexItemChildren adjusts children positions after a flex item is moved.
// deltaX and deltaY are the difference between the new and original box position.
func (le *LayoutEngine) repositionFlexItemChildren(box *Box, deltaX, deltaY float64) {
	if deltaX == 0 && deltaY == 0 {
		return
	}
	for _, child := range box.Children {
		child.X += deltaX
		child.Y += deltaY
		// Recursively shift grandchildren
		le.repositionFlexItemChildren(child, deltaX, deltaY)
	}
	// Also shift line boxes if any
	for _, lb := range box.LineBoxes {
		lb.Y += deltaY
		for _, lbBox := range lb.Boxes {
			lbBox.X += deltaX
			lbBox.Y += deltaY
		}
	}
}

// computeFlexItemAutoMinMain computes the content-based minimum main size for a flex item.
// Per CSS Flexbox §4.5, this is the smaller of the content size suggestion and specified size suggestion.
func (le *LayoutEngine) computeFlexItemAutoMinMain(node *html.Node, style *css.Style, box *Box, isRow bool) float64 {
	if isRow {
		// Row direction: min-width: auto → content-based minimum WIDTH
		contentMinSize := 0.0
		for _, child := range node.Children {
			childStyle := css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
			if childStyle == nil {
				childStyle = style
			}
			constraint := &ConstraintSpace{AvailableSize: Size{Width: le.viewport.width}}
			childMinMax := le.ComputeMinMaxSizes(child, constraint, childStyle)
			if childMinMax.MinContentSize > contentMinSize {
				contentMinSize = childMinMax.MinContentSize
			}
		}

		// Specified size suggestion: the item's computed width, if definite
		if w, ok := style.GetLength("width"); ok {
			if contentMinSize > w {
				return w
			}
		}
		return contentMinSize
	}

	// Column direction: min-height: auto → content-based minimum HEIGHT
	// The content height is the item's already-computed height from layoutNode
	contentMinHeight := box.Height - box.Padding.Top - box.Padding.Bottom - box.Border.Top - box.Border.Bottom
	if contentMinHeight < 0 {
		contentMinHeight = 0
	}

	// Specified size suggestion: the item's computed height, if definite
	if h, ok := style.GetLength("height"); ok {
		if contentMinHeight > h {
			return h
		}
	}
	return contentMinHeight
}

// Helper methods on FlexItem

// HypotheticalOuterMain returns the outer hypothetical main size (main size + margins + padding + border).
func (item *FlexItem) HypotheticalOuterMain(isRow bool) float64 {
	return item.HypotheticalMainSize + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
}

func (item *FlexItem) mainMargins(isRow bool) float64 {
	if isRow {
		return item.Box.Margin.Left + item.Box.Margin.Right
	}
	return item.Box.Margin.Top + item.Box.Margin.Bottom
}

func (item *FlexItem) mainPaddingBorder(isRow bool) float64 {
	if isRow {
		return item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
	}
	return item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
}

func (item *FlexItem) outerMainSize(isRow bool) float64 {
	if isRow {
		return item.Box.Width + item.Box.Margin.Left + item.Box.Margin.Right
	}
	return item.Box.Height + item.Box.Margin.Top + item.Box.Margin.Bottom
}

func (item *FlexItem) outerCrossSize(isRow bool) float64 {
	if isRow {
		return item.Box.Height + item.Box.Margin.Top + item.Box.Margin.Bottom
	}
	return item.Box.Width + item.Box.Margin.Left + item.Box.Margin.Right
}
