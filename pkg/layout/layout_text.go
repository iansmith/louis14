package layout

import (
	"strings"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

func (le *LayoutEngine) layoutTextNode(node *html.Node, x, y, availableWidth float64, parentStyle *css.Style, parent *Box) *Box {
	// Skip empty text nodes
	if node.Text == "" {
		return nil
	}

	// CSS 2.1 §16.6.1: Strip spaces at the beginning/end of a line in block containers.
	// When this text node is the first/last content child of a block-level parent,
	// trim leading/trailing whitespace respectively.
	// IMPORTANT: This only applies to block-level containers, not inline elements.
	// Inline elements preserve trailing whitespace because they flow horizontally.
	if parent != nil && parent.Node != nil && parent.Style != nil {
		parentDisplay := parent.Style.GetDisplay()
		// Only apply trimming for block-level containers (block, table-cell, etc.)
		// Do NOT trim for inline or inline-block parents - they flow horizontally
		isBlockContainer := parentDisplay == css.DisplayBlock ||
			parentDisplay == css.DisplayTableCell ||
			parentDisplay == css.DisplayListItem

		if isBlockContainer {
			isFirstContent := true
			isLastContent := true

			// If parent box already has children (e.g., ::before pseudo-element),
			// then this text is not the first content
			if len(parent.Children) > 0 {
				isFirstContent = false
			}

			// If parent element will have ::after pseudo-element, text is not last content
			// Check by computing ::after style and seeing if it has content
			if parent.Style != nil && parent.Node != nil {
				afterStyle := css.ComputePseudoElementStyle(parent.Node, "after", le.stylesheets, le.viewport.width, le.viewport.height, parent.Style)
				if _, hasAfterContent := afterStyle.GetContentValues(); hasAfterContent {
					isLastContent = false
				}
			}

			for _, sibling := range parent.Node.Children {
				if sibling == node {
					break
				}
				if sibling.Type == html.TextNode && strings.TrimSpace(sibling.Text) != "" {
					isFirstContent = false
				} else if sibling.Type == html.ElementNode {
					isFirstContent = false
				}
			}
			foundSelf := false
			for _, sibling := range parent.Node.Children {
				if sibling == node {
					foundSelf = true
					continue
				}
				if foundSelf {
					if sibling.Type == html.TextNode && strings.TrimSpace(sibling.Text) != "" {
						isLastContent = false
					} else if sibling.Type == html.ElementNode {
						isLastContent = false
					}
				}
			}
			if isFirstContent {
				node.Text = strings.TrimLeft(node.Text, " \t\n\r")
			}
			if isLastContent {
				node.Text = strings.TrimRight(node.Text, " \t\n\r")
			}
			if node.Text == "" {
				return nil
			}
		}
	}

	// Check for ::first-letter pseudo-element styling
	// This applies to the first letter of the first text in a block container
	var firstLetterBox *Box
	if parent != nil && parent.Node != nil && le.isFirstTextInBlock(node, parent) {
		// First check if there are any actual ::first-letter rules targeting this element
		// (not just inherited properties from parent)
		hasFirstLetterRules := false
		for _, stylesheet := range le.stylesheets {
			for _, rule := range stylesheet.Rules {
				if rule.Selector.PseudoElement == "first-letter" {
					if css.MatchesSelector(parent.Node, rule.Selector) {
						hasFirstLetterRules = true
						break
					}
				}
			}
			if hasFirstLetterRules {
				break
			}
		}

		if hasFirstLetterRules {
			// Get the computed first-letter style
			firstLetterStyle := css.ComputePseudoElementStyle(parent.Node, "first-letter", le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
			firstLetter, remaining := extractFirstLetter(node.Text)
			if firstLetter != "" {
				// Create a box for the first letter with the special styling
				flFontSize := firstLetterStyle.GetFontSize()
				flFontWeight := firstLetterStyle.GetFontWeight()
				flBold := flFontWeight == css.FontWeightBold
				flWidth, flHeight := text.MeasureTextWithWeight(firstLetter, flFontSize, flBold)

				firstLetterBox = &Box{
					Node:          node,
					Style:         firstLetterStyle,
					X:             x,
					Y:             y,
					Width:         flWidth,
					Height:        flHeight,
					Margin:        css.BoxEdge{},
					Padding:       css.BoxEdge{},
					Border:        css.BoxEdge{},
					Children:      make([]*Box, 0),
					Parent:        parent,
					PseudoContent: firstLetter,
				}

				// Advance x for the remaining text
				x += flWidth
				availableWidth -= flWidth
				// Update the node text to exclude the first letter
				node.Text = remaining
				if node.Text == "" {
					// Only the first letter, return just that box
					return firstLetterBox
				}
			}
		}
	}

	// Get font properties from parent style
	fontSize := parentStyle.GetFontSize()
	fontWeight := parentStyle.GetFontWeight()
	lineHeight := parentStyle.GetLineHeight() // Phase 7 Enhancement

	// Phase 5: Adjust position and width for floats
	adjustedX := x
	adjustedY := y
	adjustedWidth := availableWidth

	// Get available space accounting for floats
	// NOTE: X position is now handled by the inline context, so we only adjust width
	leftOffset, rightOffset := le.getFloatOffsets(adjustedY)
	adjustedWidth -= (leftOffset + rightOffset)

	// Phase 6 Enhancement: Measure the text with correct font weight
	isBold := fontWeight == css.FontWeightBold
	width, _ := text.MeasureTextWithWeight(node.Text, fontSize, isBold)
	height := lineHeight // Phase 7 Enhancement: Use line-height for box height

	// Compute parent's content-area left edge and full width for wrapped lines.
	// The first line uses the remaining space (adjustedWidth), but subsequent
	// lines start at the parent's left edge and use the full content width.
	parentContentLeft := x
	parentContentWidth := availableWidth
	if parent != nil {
		parentContentLeft = parent.X + parent.Border.Left + parent.Padding.Left
		parentContentWidth = parent.Width
	}

	// CSS 2.1 §9.5: If a shortened line box is too small to contain any content,
	// then the line box is shifted downward until either some content fits or
	// there are no more floats present.
	if adjustedWidth < parentContentWidth && adjustedWidth > 0 {
		// Get the first word to check if it fits beside floats
		firstWord := text.GetFirstWord(node.Text)
		if firstWord != "" {
			firstWordWidth, _ := text.MeasureTextWithWeight(firstWord, fontSize, isBold)
			if firstWordWidth > adjustedWidth {
				// First word doesn't fit beside floats - drop below them
				newY := le.getClearY(css.ClearBoth, adjustedY)
				if newY > adjustedY {
					adjustedY = newY
					// Recalculate float offsets at new Y position
					leftOffset, rightOffset = le.getFloatOffsets(adjustedY)
					adjustedX = x + leftOffset
					adjustedWidth = availableWidth - (leftOffset + rightOffset)
				}
			}
		}
	}

	// Phase 6 Enhancement: Check if text needs line breaking
	if width > adjustedWidth && adjustedWidth > 0 {
		// Break text into multiple lines, using remaining space for first line
		// and full parent width for subsequent lines.
		lines := text.BreakTextIntoLinesWithWrap(node.Text, fontSize, isBold, adjustedWidth, parentContentWidth)

		if len(lines) > 1 {
			// Create a container box for multi-line text
			containerBox := &Box{
				Node:     node,
				Style:    parentStyle,
				X:        adjustedX,
				Y:        adjustedY,
				Width:    parentContentWidth,
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
			currentY := adjustedY
			for i, line := range lines {
				lineWidth, _ := text.MeasureTextWithWeight(line, fontSize, isBold)
				lineNode := &html.Node{
					Type: html.TextNode,
					Text: line,
				}

				// First line starts at current X; subsequent lines at parent left edge
				lineX := adjustedX
				if i > 0 {
					lineX = parentContentLeft
				}

				lineBox := &Box{
					Node:     lineNode,
					Style:    parentStyle,
					X:        lineX,
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
		Y:        adjustedY,
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

