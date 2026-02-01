package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

type Box struct {
	Node     *html.Node
	Style    *css.Style
	X        float64
	Y        float64
	Width    float64  // Content width
	Height   float64  // Content height
	Margin   css.BoxEdge
	Padding  css.BoxEdge
	Border   css.BoxEdge
	Children []*Box   // Phase 2: Nested boxes
	Parent   *Box     // Phase 4: Parent box for containing block
	Position css.PositionType  // Phase 4: Position type
	ZIndex   int      // Phase 4: Stacking order
}

type LayoutEngine struct {
	viewport struct {
		width  float64
		height float64
	}
	absoluteBoxes []*Box     // Phase 4: Track absolutely positioned boxes
	floats        []FloatInfo // Phase 5: Track floated elements
}

// Phase 5: FloatInfo tracks information about floated elements
type FloatInfo struct {
	Box  *Box
	Side css.FloatType
	Y    float64 // Y position where float starts
}

// Phase 7: InlineContext tracks the current inline layout state
type InlineContext struct {
	LineX      float64 // Current X position on the line
	LineY      float64 // Current line Y position
	LineHeight float64 // Height of current line
	LineBoxes  []*Box  // Boxes on current line
}

func NewLayoutEngine(viewportWidth, viewportHeight float64) *LayoutEngine {
	le := &LayoutEngine{}
	le.viewport.width = viewportWidth
	le.viewport.height = viewportHeight
	return le
}

func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
	// Phase 3: Compute styles from stylesheets
	computedStyles := css.ApplyStylesToDocument(doc)

	// Phase 2: Recursively layout the tree starting from root's children
	boxes := make([]*Box, 0)
	y := 0.0

	// Phase 4: Track absolutely positioned boxes separately
	le.absoluteBoxes = make([]*Box, 0)

	// Phase 5: Initialize floats tracking
	le.floats = make([]FloatInfo, 0)

	for _, node := range doc.Root.Children {
		if node.Type == html.ElementNode {
			box := le.layoutNode(node, 0, y, le.viewport.width, computedStyles, nil)
			// Phase 7: Skip elements with display: none (layoutNode returns nil)
			if box == nil {
				continue
			}
			boxes = append(boxes, box)

			// Phase 4 & 5: Only advance Y if element is in normal flow (not absolutely positioned or floated)
			floatType := box.Style.GetFloat()
			if box.Position != css.PositionAbsolute && box.Position != css.PositionFixed && floatType == css.FloatNone {
				y += le.getTotalHeight(box)
			}
		}
	}

	// Phase 4: Add absolutely positioned boxes to result
	boxes = append(boxes, le.absoluteBoxes...)

	return boxes
}

// layoutNode recursively layouts a node and its children
func (le *LayoutEngine) layoutNode(node *html.Node, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style, parent *Box) *Box {
	// Phase 3: Use computed styles from cascade
	style := computedStyles[node]
	if style == nil {
		style = css.NewStyle()
	}

	// Phase 7: Check display mode early
	display := style.GetDisplay()
	if display == css.DisplayNone {
		return nil
	}

	// Get box model values
	margin := style.GetMargin()
	padding := style.GetPadding()
	border := style.GetBorderWidth()

	// Phase 7 Enhancement: Inline elements ignore vertical margins and padding
	if display == css.DisplayInline {
		margin.Top = 0
		margin.Bottom = 0
		padding.Top = 0
		padding.Bottom = 0
	}

	// Apply margin offset
	x += margin.Left
	y += margin.Top

	// Phase 5: Check for float early to determine width calculation
	floatType := style.GetFloat()

	// Calculate content width
	var contentWidth float64
	hasExplicitWidth := false

	// Phase 7 Enhancement: Inline elements always shrink-wrap (ignore width property)
	if display == css.DisplayInline {
		// Will be calculated from children later
		contentWidth = 0
		hasExplicitWidth = false
	} else if w, ok := style.GetLength("width"); ok {
		contentWidth = w
		hasExplicitWidth = true
	} else {
		// Default to available width minus horizontal margin, padding, border
		contentWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
	}

	// Calculate content height
	var contentHeight float64
	// Phase 7 Enhancement: Inline elements always shrink-wrap (ignore height property)
	if display == css.DisplayInline {
		// Will be calculated from children later
		contentHeight = 0
	} else if h, ok := style.GetLength("height"); ok {
		contentHeight = h
	} else {
		contentHeight = 50 // Default height
	}

	// Phase 4: Get positioning information
	position := style.GetPosition()
	zindex := style.GetZIndex()

	// Phase 5: Check for clear property
	clearType := style.GetClear()

	// Phase 5: Handle clear property - move Y down past floats
	if clearType != css.ClearNone {
		y = le.getClearY(clearType, y)
	}

	box := &Box{
		Node:     node,
		Style:    style,
		X:        x,
		Y:        y,
		Width:    contentWidth,
		Height:   contentHeight,
		Margin:   margin,
		Padding:  padding,
		Border:   border,
		Children: make([]*Box, 0),
		Position: position,
		ZIndex:   zindex,
		Parent:   parent,
	}

	// Phase 5: Float positioning will be done AFTER children are laid out
	// (to support shrink-wrapping and float drop)

	// Phase 4: Handle positioning
	if position == css.PositionAbsolute || position == css.PositionFixed {
		// Absolutely positioned elements
		le.applyAbsolutePositioning(box)
		le.absoluteBoxes = append(le.absoluteBoxes, box)
	} else if position == css.PositionRelative {
		// Relative positioning: offset from normal position
		offset := style.GetPositionOffset()
		if offset.HasTop {
			box.Y += offset.Top
		}
		if offset.HasLeft {
			box.X += offset.Left
		}
	}

	// Phase 2: Recursively layout children
	childY := y + border.Top + padding.Top
	childAvailableWidth := contentWidth - padding.Left - padding.Right

	// Phase 7: Track inline layout context
	inlineCtx := &InlineContext{
		LineX:      x + border.Left + padding.Left,
		LineY:      childY,
		LineHeight: 0,
		LineBoxes:  make([]*Box, 0),
	}

	for _, child := range node.Children {
		if child.Type == html.ElementNode {
			// Get child's computed style to check display mode
			childStyle := computedStyles[child]
			if childStyle == nil {
				childStyle = css.NewStyle()
			}
			childDisplay := childStyle.GetDisplay()

			// Layout the child
			childBox := le.layoutNode(
				child,
				inlineCtx.LineX,
				inlineCtx.LineY,
				childAvailableWidth,
				computedStyles,
				box,  // Phase 4: Pass parent
			)

			// Phase 7: Skip elements with display: none (layoutNode returns nil)
			if childBox != nil {
				// Phase 7: Handle inline and inline-block elements
				if (childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock) && childBox.Position == css.PositionStatic {
					childTotalWidth := le.getTotalWidth(childBox)

					// Check if child fits on current line
					if inlineCtx.LineX + childTotalWidth > x + border.Left + padding.Left + childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to next line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = x + border.Left + padding.Left
						inlineCtx.LineHeight = 0
						inlineCtx.LineBoxes = make([]*Box, 0)

						// Reposition child at start of new line
						childBox.X = inlineCtx.LineX
						childBox.Y = inlineCtx.LineY
					}

					// Add to current line
					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, childBox)
					childHeight := le.getTotalHeight(childBox)
					if childHeight > inlineCtx.LineHeight {
						inlineCtx.LineHeight = childHeight
					}

					// Advance X for next inline-block element
					inlineCtx.LineX += childTotalWidth

					box.Children = append(box.Children, childBox)

					// Phase 7 Enhancement: Apply vertical-align to inline element
					le.applyVerticalAlign(childBox, inlineCtx.LineY, inlineCtx.LineHeight)
				} else {
					// Block element or other display mode
					// Finish current inline line
					if len(inlineCtx.LineBoxes) > 0 {
						childY = inlineCtx.LineY + inlineCtx.LineHeight
						inlineCtx.LineBoxes = make([]*Box, 0)
						inlineCtx.LineHeight = 0
					} else {
						childY = inlineCtx.LineY
					}

					// Update child position for block element
					childBox.X = x + border.Left + padding.Left
					childBox.Y = childY

					box.Children = append(box.Children, childBox)

					// Advance Y for block elements
					childFloatType := childBox.Style.GetFloat()
					if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && childFloatType == css.FloatNone {
						childY += le.getTotalHeight(childBox)
					}

					// Reset inline context for next line
					inlineCtx.LineX = x + border.Left + padding.Left
					inlineCtx.LineY = childY
				}
			}
		} else if child.Type == html.TextNode {
			// Phase 6: Layout text nodes
			// Phase 7 Enhancement: Text flows inline if there are inline elements
			if len(inlineCtx.LineBoxes) > 0 {
				// Continue on current inline line
				textBox := le.layoutTextNode(
					child,
					inlineCtx.LineX,
					inlineCtx.LineY,
					x + border.Left + padding.Left + childAvailableWidth - inlineCtx.LineX,
					style,  // Text inherits parent's style
					box,
				)
				if textBox != nil {
					box.Children = append(box.Children, textBox)

					// Add text to inline flow
					textWidth := le.getTotalWidth(textBox)
					textHeight := le.getTotalHeight(textBox)

					// Check if text fits on current line
					if inlineCtx.LineX + textWidth > x + border.Left + padding.Left + childAvailableWidth {
						// Wrap to new line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = x + border.Left + padding.Left
						inlineCtx.LineHeight = textHeight
						textBox.X = inlineCtx.LineX
						textBox.Y = inlineCtx.LineY
						inlineCtx.LineX += textWidth
					} else {
						// Fits on current line
						inlineCtx.LineX += textWidth
						if textHeight > inlineCtx.LineHeight {
							inlineCtx.LineHeight = textHeight
						}
					}

					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, textBox)
				}
			} else {
				// No inline elements, text starts on new line
				textBox := le.layoutTextNode(
					child,
					x + border.Left + padding.Left,
					childY,
					childAvailableWidth,
					style,  // Text inherits parent's style
					box,
				)
				if textBox != nil {
					box.Children = append(box.Children, textBox)
					childY += le.getTotalHeight(textBox)
					inlineCtx.LineY = childY
					inlineCtx.LineX = x + border.Left + padding.Left
				}
			}
		}
	}

	// If height is auto and we have children, adjust height to fit content
	if _, ok := style.GetLength("height"); !ok && len(box.Children) > 0 {
		// Calculate total height of children
		totalChildHeight := 0.0
		for _, child := range box.Children {
			totalChildHeight += le.getTotalHeight(child)
		}
		box.Height = totalChildHeight
	}

	// Phase 7 Enhancement: Inline elements always shrink-wrap to children
	if display == css.DisplayInline && len(box.Children) > 0 {
		// Calculate width from children (horizontal sum for inline flow)
		maxChildWidth := 0.0
		totalChildHeight := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			if childWidth > maxChildWidth {
				maxChildWidth = childWidth
			}
			childHeight := le.getTotalHeight(child)
			if childHeight > totalChildHeight {
				totalChildHeight = childHeight
			}
		}
		box.Width = maxChildWidth
		box.Height = totalChildHeight
	}

	// Phase 5 Enhancement: Float shrink-wrapping
	// If this is a float without explicit width, shrink-wrap to content
	if floatType != css.FloatNone && !hasExplicitWidth && len(box.Children) > 0 {
		maxChildWidth := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			if childWidth > maxChildWidth {
				maxChildWidth = childWidth
			}
		}
		// Set width to fit children (but don't exceed available width)
		if maxChildWidth > 0 && maxChildWidth < box.Width {
			box.Width = maxChildWidth
		}
	}

	// Phase 5: Handle float positioning AFTER children layout and shrink-wrapping
	if floatType != css.FloatNone && position == css.PositionStatic {
		floatTotalWidth := le.getTotalWidth(box)

		// Phase 5 Enhancement: Check if float fits, apply drop if needed
		floatY := le.getFloatDropY(floatType, floatTotalWidth, box.Y, availableWidth)
		box.Y = floatY

		// Position float horizontally
		if floatType == css.FloatLeft {
			// Position at left edge (accounting for existing left floats)
			leftOffset, _ := le.getFloatOffsets(floatY)
			box.X = x + leftOffset
		} else if floatType == css.FloatRight {
			// Position at right edge (accounting for existing right floats)
			_, rightOffset := le.getFloatOffsets(floatY)
			box.X = x + availableWidth - floatTotalWidth - rightOffset
		}

		// Add to float tracking
		le.addFloat(box, floatType, floatY)
	}

	return box
}

// getStyle returns the computed style for a node
func (le *LayoutEngine) getStyle(node *html.Node) *css.Style {
	if styleAttr, ok := node.GetAttribute("style"); ok {
		return css.ParseInlineStyle(styleAttr)
	}
	return css.NewStyle()
}

// getTotalHeight returns the total height including margin, border, padding
func (le *LayoutEngine) getTotalHeight(box *Box) float64 {
	return box.Margin.Top + box.Border.Top + box.Padding.Top +
		box.Height +
		box.Padding.Bottom + box.Border.Bottom + box.Margin.Bottom
}

// getTotalWidth returns the total width including margin, border, padding
func (le *LayoutEngine) getTotalWidth(box *Box) float64 {
	return box.Margin.Left + box.Border.Left + box.Padding.Left +
		box.Width +
		box.Padding.Right + box.Border.Right + box.Margin.Right
}

// Phase 6: layoutTextNode creates a layout box for a text node
func (le *LayoutEngine) layoutTextNode(node *html.Node, x, y, availableWidth float64, parentStyle *css.Style, parent *Box) *Box {
	// Skip empty text nodes
	if node.Text == "" {
		return nil
	}

	// Get font properties from parent style
	fontSize := parentStyle.GetFontSize()
	fontWeight := parentStyle.GetFontWeight()
	lineHeight := parentStyle.GetLineHeight() // Phase 7 Enhancement

	// Phase 5: Adjust position and width for floats
	adjustedX := x
	adjustedWidth := availableWidth

	// Get available space accounting for floats
	leftOffset, rightOffset := le.getFloatOffsets(y)
	adjustedX += leftOffset
	adjustedWidth -= (leftOffset + rightOffset)

	// Phase 6 Enhancement: Measure the text with correct font weight
	isBold := fontWeight == css.FontWeightBold
	width, _ := text.MeasureTextWithWeight(node.Text, fontSize, isBold)
	height := lineHeight // Phase 7 Enhancement: Use line-height for box height

	// Phase 6 Enhancement: Check if text needs line breaking
	if width > adjustedWidth && adjustedWidth > 0 {
		// Break text into multiple lines
		lines := text.BreakTextIntoLines(node.Text, fontSize, isBold, adjustedWidth)

		if len(lines) > 1 {
			// Create a container box for multi-line text
			containerBox := &Box{
				Node:     node,
				Style:    parentStyle,
				X:        adjustedX,
				Y:        y,
				Width:    adjustedWidth,
				Height:   float64(len(lines)) * height,
				Margin:   css.BoxEdge{},
				Padding:  css.BoxEdge{},
				Border:   css.BoxEdge{},
				Children: make([]*Box, 0),
				Position: css.PositionStatic,
				ZIndex:   0,
				Parent:   parent,
			}

			// Create a box for each line
			currentY := y
			for _, line := range lines {
				lineWidth, _ := text.MeasureTextWithWeight(line, fontSize, isBold)
				lineNode := &html.Node{
					Type: html.TextNode,
					Text: line,
				}

				lineBox := &Box{
					Node:     lineNode,
					Style:    parentStyle,
					X:        adjustedX,
					Y:        currentY,
					Width:    lineWidth,
					Height:   lineHeight, // Phase 7: Use line-height consistently
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Children: make([]*Box, 0),
					Position: css.PositionStatic,
					ZIndex:   0,
					Parent:   containerBox,
				}

				containerBox.Children = append(containerBox.Children, lineBox)
				currentY += lineHeight
			}

			return containerBox
		}
	}

	// Create a box for single-line text
	box := &Box{
		Node:     node,
		Style:    parentStyle, // Text inherits parent's style
		X:        adjustedX,
		Y:        y,
		Width:    width,
		Height:   height,
		Margin:   css.BoxEdge{},   // No margin for text
		Padding:  css.BoxEdge{},   // No padding for text
		Border:   css.BoxEdge{},   // No border for text
		Children: make([]*Box, 0), // Text nodes have no children
		Position: css.PositionStatic,
		ZIndex:   0,
		Parent:   parent,
	}

	return box
}

// Phase 5: Float layout helper methods

// addFloat adds a floated element to the tracking list
func (le *LayoutEngine) addFloat(box *Box, side css.FloatType, y float64) {
	le.floats = append(le.floats, FloatInfo{
		Box:  box,
		Side: side,
		Y:    y,
	})
}

// getFloatOffsets returns the left and right offsets caused by floats at a given Y position
func (le *LayoutEngine) getFloatOffsets(y float64) (leftOffset, rightOffset float64) {
	leftOffset = 0
	rightOffset = 0

	for _, floatInfo := range le.floats {
		// Check if this float affects the current Y position
		floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)
		if y >= floatInfo.Y && y < floatBottom {
			if floatInfo.Side == css.FloatLeft {
				floatWidth := le.getTotalWidth(floatInfo.Box)
				if floatWidth > leftOffset {
					leftOffset = floatWidth
				}
			} else if floatInfo.Side == css.FloatRight {
				floatWidth := le.getTotalWidth(floatInfo.Box)
				if floatWidth > rightOffset {
					rightOffset = floatWidth
				}
			}
		}
	}

	return leftOffset, rightOffset
}

// getClearY returns the Y position after clearing floats
func (le *LayoutEngine) getClearY(clearType css.ClearType, currentY float64) float64 {
	if clearType == css.ClearNone {
		return currentY
	}

	maxY := currentY

	for _, floatInfo := range le.floats {
		floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)

		shouldClear := false
		switch clearType {
		case css.ClearLeft:
			shouldClear = floatInfo.Side == css.FloatLeft
		case css.ClearRight:
			shouldClear = floatInfo.Side == css.FloatRight
		case css.ClearBoth:
			shouldClear = true
		}

		if shouldClear && floatBottom > maxY {
			maxY = floatBottom
		}
	}

	return maxY
}

// Phase 5 Enhancement: getFloatDropY finds Y position where float of given width will fit
func (le *LayoutEngine) getFloatDropY(floatType css.FloatType, floatWidth float64, startY float64, availableWidth float64) float64 {
	currentY := startY
	maxAttempts := 100 // Prevent infinite loops

	for attempt := 0; attempt < maxAttempts; attempt++ {
		leftOffset, rightOffset := le.getFloatOffsets(currentY)
		remainingWidth := availableWidth - leftOffset - rightOffset

		// Check if float fits at current Y
		if floatWidth <= remainingWidth {
			return currentY
		}

		// Find the next Y position where a float ends
		nextY := currentY + 1 // Default small increment
		for _, floatInfo := range le.floats {
			floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)
			// Look for the nearest float bottom that's below current Y
			if floatBottom > currentY && (nextY == currentY+1 || floatBottom < nextY) {
				nextY = floatBottom
			}
		}

		currentY = nextY

		// If we've moved way down, just return current position
		if currentY > startY+1000 {
			return startY
		}
	}

	return currentY
}

// Phase 7 Enhancement: applyVerticalAlign adjusts element Y position based on vertical-align
func (le *LayoutEngine) applyVerticalAlign(box *Box, lineY float64, lineHeight float64) {
	valign := box.Style.GetVerticalAlign()
	boxHeight := le.getTotalHeight(box)

	switch valign {
	case css.VerticalAlignTop:
		// Align top of box with top of line
		box.Y = lineY
	case css.VerticalAlignMiddle:
		// Center box vertically in line
		box.Y = lineY + (lineHeight-boxHeight)/2
	case css.VerticalAlignBottom:
		// Align bottom of box with bottom of line
		box.Y = lineY + lineHeight - boxHeight
	case css.VerticalAlignBaseline:
		// Default - already positioned at baseline (lineY)
		// Could be enhanced with true baseline alignment in the future
		box.Y = lineY
	}
}
