package layout

import (
	"fmt"
	"strconv"
	"strings"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/text"
)

func (le *LayoutEngine) generatePseudoElement(node *html.Node, pseudoType string, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style, parent *Box) *Box {
	// Compute pseudo-element style using stored stylesheets
	// Phase 22: Pass viewport dimensions for media query evaluation
	parentStyle := computedStyles[node]
	pseudoStyle := css.ComputePseudoElementStyle(node, pseudoType, le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)

	// Get parsed content values from pseudo-element style
	contentValues, hasContent := pseudoStyle.GetContentValues()
	if !hasContent || len(contentValues) == 0 {
		return nil
	}

	// CSS Counter support: Process counter-increment BEFORE evaluating content
	// This ensures counter() returns the incremented value
	if incVal, ok := pseudoStyle.Get("counter-increment"); ok {
		increments := parseCounterIncrement(incVal)
		for name, value := range increments {
			le.counterIncrement(name, value)
		}
	}

	// Create box model values
	margin := pseudoStyle.GetMargin()
	padding := pseudoStyle.GetPadding()
	border := pseudoStyle.GetBorderWidth()
	display := pseudoStyle.GetDisplay()
	fontSize := pseudoStyle.GetFontSize()
	fontWeight := pseudoStyle.GetFontWeight()
	bold := fontWeight == css.FontWeightBold

	// Get quotes from parent style (for open-quote/close-quote)
	quotes := []string{"\"", "\"", "'", "'"}
	if parentStyle != nil {
		if q, ok := parentStyle.Get("quotes"); ok {
			quotes = parseQuotes(q)
		}
	}

	// Build combined text content and collect images
	// Track pre-image text (before first image) and post-image text (after all images)
	var preImageText string
	var postImageText string
	var imageBoxes []*Box
	currentX := x + margin.Left + border.Left + padding.Left
	quoteDepth := 0
	seenImage := false

	for _, cv := range contentValues {
		switch cv.Type {
		case "text":
			if seenImage {
				postImageText += cv.Value
			} else {
				preImageText += cv.Value
			}
		case "url":
			seenImage = true
			// Create an image box for this URL
			var imgWidth, imgHeight float64
			if w, h, err := images.GetImageDimensionsWithFetcher(cv.Value, le.imageFetcher); err == nil {
				imgWidth = float64(w)
				imgHeight = float64(h)
			}
			// If dimensions fail to load, imgWidth and imgHeight remain 0 (placeholder)

			// Create style for image box (inline-block, not block)
			imgStyle := css.NewStyle()
			imgStyle.Set("display", "inline-block")

			imgBox := &Box{
				Node:      node,
				Style:     imgStyle,
				X:         currentX,
				Y:         y + margin.Top + border.Top + padding.Top,
				Width:     imgWidth,
				Height:    imgHeight,
				ImagePath: cv.Value, // Fetcher will resolve relative paths during rendering
			}
			imageBoxes = append(imageBoxes, imgBox)
			currentX += imgWidth
		case "counter":
			// Get the current value of the specified counter
			counterValue := le.counterValue(cv.Value)
			if seenImage {
				postImageText += strconv.Itoa(counterValue)
			} else {
				preImageText += strconv.Itoa(counterValue)
			}
		case "attr":
			// Get attribute value from the node
			if val, ok := node.GetAttribute(cv.Value); ok && val != "" {
				if seenImage {
					postImageText += val
				} else {
					preImageText += val
				}
			}
		case "open-quote":
			if quoteDepth*2 < len(quotes) {
				if seenImage {
					postImageText += quotes[quoteDepth*2]
				} else {
					preImageText += quotes[quoteDepth*2]
				}
			}
			quoteDepth++
		case "close-quote":
			if quoteDepth > 0 {
				quoteDepth--
			}
			if quoteDepth*2+1 < len(quotes) {
				if seenImage {
					postImageText += quotes[quoteDepth*2+1]
				} else {
					preImageText += quotes[quoteDepth*2+1]
				}
			}
		}
	}

	// Combine for total text content (used for non-wrapped layouts)
	textContent := preImageText + postImageText

	// Measure text dimensions
	var boxWidth, boxHeight float64
	var textWidth, textHeight float64
	if textContent != "" {
		textWidth, textHeight = text.MeasureTextWithWeight(textContent, fontSize, bold)
		boxWidth = textWidth
		boxHeight = textHeight
	}

	// Calculate total image width and max image height
	var imageWidth, maxImageHeight float64
	for _, imgBox := range imageBoxes {
		imageWidth += imgBox.Width
		if imgBox.Height > maxImageHeight {
			maxImageHeight = imgBox.Height
		}
	}
	boxWidth += imageWidth
	if maxImageHeight > boxHeight {
		boxHeight = maxImageHeight
	}

	// Check if this is a floated pseudo-element
	floatVal := pseudoStyle.GetFloat()

	// For floated elements, apply shrink-to-fit sizing with text wrapping (CSS 2.1 §10.3.5)
	// shrink-to-fit width = min(max(preferred minimum width, available width), preferred width)
	// For floats, browsers typically use min-content sizing when it produces a narrower result
	var wrappedPostLines []string
	var shrinkToFitWidth float64 // Declared at higher scope for use in child positioning
	// Use CSS default line-height: normal (approximately 1.2x font size)
	lineHeight := fontSize * 1.2

	// Track pre-image and post-image text widths for layout
	var preImageWidth, postImageWidth float64
	if preImageText != "" {
		preImageWidth, _ = text.MeasureTextWithWeight(preImageText, fontSize, bold)
	}
	if postImageText != "" {
		postImageWidth, _ = text.MeasureTextWithWeight(postImageText, fontSize, bold)
	}

	if floatVal != css.FloatNone && (textContent != "" || len(imageBoxes) > 0) {
		// Calculate preferred minimum width (min-content): the longest unbreakable unit
		// This includes the image width and the longest word in post-image text
		minContentWidth := preImageWidth + imageWidth
		if postImageText != "" {
			words := strings.Fields(postImageText)
			for _, word := range words {
				wordWidth, _ := text.MeasureTextWithWeight(word, fontSize, bold)
				if wordWidth > minContentWidth {
					minContentWidth = wordWidth
				}
			}
		}

		// Calculate preferred width (max-content): all content on one line
		// Use sum of individual text measurements to match actual child box widths
		maxContentWidth := preImageWidth + imageWidth + postImageWidth

		// Calculate available width
		maxAvailable := availableWidth - margin.Left - margin.Right - padding.Left - padding.Right - border.Left - border.Right

		// CSS 2.1 §10.3.5: shrink-to-fit = min(max(min-content, available), max-content)
		// For floated pseudo-elements, prefer keeping all content on one line
		shrinkToFitWidth = minContentWidth
		if maxAvailable > minContentWidth {
			shrinkToFitWidth = maxAvailable
		}
		if shrinkToFitWidth > maxContentWidth {
			shrinkToFitWidth = maxContentWidth
		}

		// For floated elements, always use shrink-to-fit width
		boxWidth = shrinkToFitWidth

		// Wrap postImageText only if content exceeds shrink-to-fit width
		if postImageText != "" && maxContentWidth > shrinkToFitWidth {
			// Calculate remaining space on first line after preImageText and images
			firstLineMax := shrinkToFitWidth - preImageWidth - imageWidth
			if firstLineMax < 0 {
				firstLineMax = 0
			}

			// Wrap only the post-image text
			wrappedPostLines = text.BreakTextIntoLinesWithWrap(postImageText, fontSize, bold, firstLineMax, shrinkToFitWidth)

			// Calculate height needed for all content
			numTextLines := len(wrappedPostLines)
			if numTextLines == 0 {
				// No wrapped text - height is just the first line (max of line height and image height)
				boxHeight = lineHeight
				if maxImageHeight > lineHeight {
					boxHeight = maxImageHeight
				}
			} else {
				// First line contains images + possibly first text line
				// Additional lines contain wrapped text
				firstLineHeight := lineHeight
				if maxImageHeight > firstLineHeight {
					firstLineHeight = maxImageHeight
				}
				additionalLines := numTextLines - 1
				if additionalLines < 0 {
					additionalLines = 0
				}
				boxHeight = firstLineHeight + float64(additionalLines)*lineHeight
			}
		}
	}

	// Block-level pseudo-elements: take available width unless floated
	if display == css.DisplayBlock && floatVal == css.FloatNone && (textContent != "" || len(imageBoxes) > 0) {
		totalWidth := availableWidth - margin.Left - margin.Right
		boxWidth = totalWidth - padding.Left - padding.Right - border.Left - border.Right
	}

	// Apply explicit height
	if h, ok := pseudoStyle.GetLength("height"); ok {
		boxHeight = h
	}

	// Create the pseudo-element box
	box := &Box{
		Node:          node,
		Style:         pseudoStyle,
		X:             x + margin.Left,
		Y:             y + margin.Top,
		Width:         boxWidth,
		Height:        boxHeight,
		Margin:        margin,
		Padding:       padding,
		Border:        border,
		Children:      make([]*Box, 0),
		Parent:        parent,
		PseudoContent: textContent,
	}

	// Add image boxes as children
	for _, imgBox := range imageBoxes {
		imgBox.Parent = box
		box.Children = append(box.Children, imgBox)
	}

	// For content with images, create child text boxes for proper inline layout and paint order
	// This prevents the parent's full text from being drawn and then covered by image children
	if len(imageBoxes) > 0 {
		// Clear PseudoContent from container so renderer draws children instead
		box.PseudoContent = ""

		contentX := x + margin.Left + border.Left + padding.Left
		contentY := y + margin.Top + border.Top + padding.Top

		// Line 1: preImageText (before images), then images (already positioned), then first wrapped line
		currentLineX := contentX

		// Add preImageText as a child box on line 1
		if preImageText != "" {
			// Create a minimal style for the text box (inline content)
			textStyle := css.NewStyle()
			textStyle.Set("display", "inline")
			// Inherit font properties from pseudo-element style
			if val, ok := pseudoStyle.Get("font-size"); ok {
				textStyle.Set("font-size", val)
			}
			if val, ok := pseudoStyle.Get("font-weight"); ok {
				textStyle.Set("font-weight", val)
			}
			if val, ok := pseudoStyle.Get("color"); ok {
				textStyle.Set("color", val)
			}

			preBox := &Box{
				Node:          node,
				Style:         textStyle,
				X:             currentLineX,
				Y:             contentY,
				Width:         preImageWidth,
				Height:        lineHeight,
				Margin:        css.BoxEdge{},
				Padding:       css.BoxEdge{},
				Border:        css.BoxEdge{},
				Children:      make([]*Box, 0),
				Parent:        box,
				PseudoContent: preImageText,
			}
			box.Children = append(box.Children, preBox)
			currentLineX += preImageWidth
		}

		// Images are already added as children - update their X positions
		for _, imgBox := range imageBoxes {
			imgBox.X = currentLineX
			currentLineX += imgBox.Width
		}

		// Add post-image text (either wrapped lines or single unwrapped line)
		if len(wrappedPostLines) > 0 {
			// Text wraps - add each wrapped line as a child box
			// Text continues on the same line after images if there's space
			// Wrapped lines start below the image if image is taller than line height
			firstLineBaseY := contentY
			wrappedLinesStartY := contentY + maxImageHeight
			if wrappedLinesStartY < contentY+lineHeight {
				wrappedLinesStartY = contentY + lineHeight
			}

			for i, line := range wrappedPostLines {
				lineWidth, _ := text.MeasureTextWithWeight(line, fontSize, bold)

				var lineX, lineY float64
				if i == 0 {
					// First line continues after preImageText and images
					lineX = currentLineX
					lineY = firstLineBaseY
				} else {
					// Subsequent lines wrap below the image
					lineX = contentX
					lineY = wrappedLinesStartY + float64(i-1)*lineHeight
				}

				// Create a minimal style for the text box (inline content)
				textStyle := css.NewStyle()
				textStyle.Set("display", "inline")
				// Inherit font properties from pseudo-element style
				if val, ok := pseudoStyle.Get("font-size"); ok {
					textStyle.Set("font-size", val)
				}
				if val, ok := pseudoStyle.Get("font-weight"); ok {
					textStyle.Set("font-weight", val)
				}
				if val, ok := pseudoStyle.Get("color"); ok {
					textStyle.Set("color", val)
				}

				// Create a pseudo-text child box for this line
				lineBox := &Box{
					Node:          node,
					Style:         textStyle,
					X:             lineX,
					Y:             lineY,
					Width:         lineWidth,
					Height:        lineHeight,
					Margin:        css.BoxEdge{},
					Padding:       css.BoxEdge{},
					Border:        css.BoxEdge{},
					Children:      make([]*Box, 0),
					Parent:        box,
					PseudoContent: line,
				}
				box.Children = append(box.Children, lineBox)
			}
		} else if postImageText != "" {
			// Text doesn't wrap - add unwrapped postImageText as single child box
			// postImageWidth was already measured earlier for shrink-to-fit calculation

			// Create a minimal style for the text box (inline content)
			textStyle := css.NewStyle()
			textStyle.Set("display", "inline")
			// Inherit font properties from pseudo-element style
			if val, ok := pseudoStyle.Get("font-size"); ok {
				textStyle.Set("font-size", val)
			}
			if val, ok := pseudoStyle.Get("font-weight"); ok {
				textStyle.Set("font-weight", val)
			}
			if val, ok := pseudoStyle.Get("color"); ok {
				textStyle.Set("color", val)
			}

			postBox := &Box{
				Node:          node,
				Style:         textStyle,
				X:             currentLineX,
				Y:             contentY,
				Width:         postImageWidth,
				Height:        lineHeight,
				Margin:        css.BoxEdge{},
				Padding:       css.BoxEdge{},
				Border:        css.BoxEdge{},
				Children:      make([]*Box, 0),
				Parent:        box,
				PseudoContent: postImageText,
			}
			box.Children = append(box.Children, postBox)
		}
	}

	// Update box dimensions to include all content
	if len(imageBoxes) > 0 || textContent != "" {
		box.Width = boxWidth
		box.Height = boxHeight
	}

	return box
}

// createPseudoElementNode creates a synthetic html.Node for a pseudo-element.
// Instead of generating Box objects (like generatePseudoElement), this creates
// DOM nodes that can be processed by the multi-pass inline layout pipeline,
// ensuring pseudo-elements get identical sizing and positioning to real elements.
//
// Returns the synthetic node and its computed style, or (nil, nil) if no content.
func (le *LayoutEngine) createPseudoElementNode(node *html.Node, pseudoType string, computedStyles map[*html.Node]*css.Style) (*html.Node, *css.Style) {
	parentStyle := computedStyles[node]
	pseudoStyle := css.ComputePseudoElementStyle(node, pseudoType, le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)

	contentValues, hasContent := pseudoStyle.GetContentValues()
	if !hasContent || len(contentValues) == 0 {
		return nil, nil
	}

	// CSS Counter support: Process counter-increment BEFORE evaluating content
	if incVal, ok := pseudoStyle.Get("counter-increment"); ok {
		increments := parseCounterIncrement(incVal)
		for name, value := range increments {
			le.counterIncrement(name, value)
		}
	}

	// Get quotes from parent style (for open-quote/close-quote)
	quotes := []string{"\"", "\"", "'", "'"}
	if parentStyle != nil {
		if q, ok := parentStyle.Get("quotes"); ok {
			quotes = parseQuotes(q)
		}
	}

	// Create the synthetic span node
	syntheticNode := &html.Node{
		Type:       html.ElementNode,
		TagName:    "span",
		Attributes: map[string]string{},
		Children:   make([]*html.Node, 0),
		Parent:     node,
	}

	// Resolve content values into child nodes
	var currentText string
	quoteDepth := 0

	flushText := func() {
		if currentText != "" {
			textNode := &html.Node{
				Type:   html.TextNode,
				Text:   currentText,
				Parent: syntheticNode,
			}
			syntheticNode.Children = append(syntheticNode.Children, textNode)
			currentText = ""
		}
	}

	for _, cv := range contentValues {
		switch cv.Type {
		case "text":
			currentText += cv.Value
		case "url":
			flushText()
			imgNode := &html.Node{
				Type:       html.ElementNode,
				TagName:    "img",
				Attributes: map[string]string{"src": cv.Value},
				Children:   make([]*html.Node, 0),
				Parent:     syntheticNode,
			}
			syntheticNode.Children = append(syntheticNode.Children, imgNode)
		case "counter":
			counterValue := le.counterValue(cv.Value)
			currentText += strconv.Itoa(counterValue)
		case "attr":
			if val, ok := node.GetAttribute(cv.Value); ok && val != "" {
				currentText += val
			}
		case "open-quote":
			if quoteDepth*2 < len(quotes) {
				currentText += quotes[quoteDepth*2]
			}
			quoteDepth++
		case "close-quote":
			if quoteDepth > 0 {
				quoteDepth--
			}
			if quoteDepth*2+1 < len(quotes) {
				currentText += quotes[quoteDepth*2+1]
			}
		}
	}
	flushText()

	// If no children were created, return nil
	if len(syntheticNode.Children) == 0 {
		return nil, nil
	}

	return syntheticNode, pseudoStyle
}

// parseQuotes parses the CSS quotes property value

// unescapeUnicode converts CSS Unicode escapes like \0022 to actual characters

// Phase 23: generateListMarker creates a marker box for list items
func (le *LayoutEngine) generateListMarker(node *html.Node, style *css.Style, x, y float64, parent *Box) *Box {
	listStyleType := style.GetListStyleType()
	if listStyleType == css.ListStyleTypeNone {
		return nil
	}

	// Generate marker text based on list-style-type
	var markerText string
	switch listStyleType {
	case css.ListStyleTypeDisc:
		markerText = "•"
	case css.ListStyleTypeCircle:
		markerText = "○"
	case css.ListStyleTypeSquare:
		markerText = "■"
	case css.ListStyleTypeDecimal:
		// Count preceding <li> siblings to determine number
		itemNumber := le.getListItemNumber(node)
		markerText = fmt.Sprintf("%d.", itemNumber)
	default:
		// Use custom marker string (e.g., from list-style-type: "\2022")
		if string(listStyleType) != "" {
			markerText = string(listStyleType)
		} else {
			markerText = "•"
		}
	}

	// Measure marker text
	fontSize := style.GetFontSize()
	fontWeight := style.GetFontWeight()
	bold := fontWeight == css.FontWeightBold
	textWidth, textHeight := text.MeasureTextWithWeight(markerText, fontSize, bold)

	// Position marker to the left of the content (outside the content box)
	// CSS 2.1 §12.5.1: marker box is placed outside the principal box
	// Use 0.5em spacing between marker and content (typical browser behavior)
	markerSpacing := fontSize * 0.5
	markerX := x - textWidth - markerSpacing
	markerY := y

	markerBox := &Box{
		Node:          node,
		Style:         style,
		X:             markerX,
		Y:             markerY,
		Width:         textWidth,
		Height:        textHeight,
		Margin:        css.BoxEdge{},
		Padding:       css.BoxEdge{},
		Border:        css.BoxEdge{},
		Children:      make([]*Box, 0),
		Parent:        parent,
		PseudoContent: markerText, // Store marker text for rendering
	}

	return markerBox
}

func (le *LayoutEngine) hasPseudoElements(node *html.Node, computedStyles map[*html.Node]*css.Style) bool {
	parentStyle := computedStyles[node]

	// Check ::before
	beforeStyle := css.ComputePseudoElementStyle(node, "before", le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
	if contentValues, hasContent := beforeStyle.GetContentValues(); hasContent && len(contentValues) > 0 {
		return true
	}

	// Check ::after
	afterStyle := css.ComputePseudoElementStyle(node, "after", le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
	if contentValues, hasContent := afterStyle.GetContentValues(); hasContent && len(contentValues) > 0 {
		return true
	}

	return false
}

