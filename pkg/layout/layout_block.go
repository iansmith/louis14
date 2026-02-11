package layout

import (
	"fmt"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
)

func (le *LayoutEngine) layoutNode(node *html.Node, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style, parent *Box) *Box {
	// Debug: Track footer-related elements (removed - see end of function for box width)
	// DEBUG: Track all div elements
	if node != nil && node.TagName == "div" {
		nodeID := "(no id)"
		if node.Attributes != nil {
			if id, ok := node.Attributes["id"]; ok {
				nodeID = id
			}
		}
		fmt.Printf("DEBUG LAYOUT START: layoutNode called for <div id='%s'>\n", nodeID)
	}

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

	// Phase 8: Check if this is an img element
	isImage := node.TagName == "img"
	// Phase 24: Check if this is an object element with a loadable image
	isObjectImage := false
	if node.TagName == "object" {
		if data, ok := node.GetAttribute("data"); ok {
			if _, _, err := images.GetImageDimensionsWithFetcher(data, le.imageFetcher); err == nil {
				isObjectImage = true
			}
		}
	}
	var imageWidth, imageHeight int
	var imagePath string
	if isImage {
		// Get image source
		if src, ok := node.GetAttribute("src"); ok {
			imagePath = src
			// Try to load image to get natural dimensions
			if w, h, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil {
				imageWidth = w
				imageHeight = h
			}
		}
		// Images default to inline-block display
		if display == css.DisplayBlock {
			display = css.DisplayInlineBlock
		}
	} else if isObjectImage {
		// Object element with loadable image - treat like img
		if data, ok := node.GetAttribute("data"); ok {
			imagePath = data
			if w, h, err := images.GetImageDimensionsWithFetcher(data, le.imageFetcher); err == nil {
				imageWidth = w
				imageHeight = h
			}
		}
		isImage = true
		if display == css.DisplayBlock {
			display = css.DisplayInlineBlock
		}
	}

	// Phase 5: Check for float early to determine width calculation
	floatType := style.GetFloat()

	// CSS 2.1 §9.7: Relationships between display, position, and float
	// Floated or absolutely positioned inline elements compute to block display
	if display == css.DisplayInline {
		pos := style.GetPosition()
		if floatType != css.FloatNone || pos == css.PositionAbsolute || pos == css.PositionFixed {
			display = css.DisplayBlock
		}
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

	// TEMP DEBUG: Track Y for float divs
	if floatType != css.FloatNone && node != nil {
		fmt.Printf("DEBUG FLOAT-Y [%s]: initial y=%.1f (after margin.Top=%.1f), padding={T:%.1f R:%.1f B:%.1f L:%.1f}\n",
			node.TagName, y, margin.Top, padding.Top, padding.Right, padding.Bottom, padding.Left)
	}

	// Calculate content width
	var contentWidth float64
	hasExplicitWidth := false

	// Phase 8: Images use image dimensions or explicit dimensions
	if isImage {
		if w, ok := style.GetLength("width"); ok {
			contentWidth = w
			hasExplicitWidth = true
		} else if widthAttr, ok := node.GetAttribute("width"); ok {
			// Parse width attribute
			if w, ok := css.ParseLength(widthAttr); ok {
				contentWidth = w
				hasExplicitWidth = true
			}
		} else if imageWidth > 0 {
			// Use natural image width
			contentWidth = float64(imageWidth)
			hasExplicitWidth = true
		} else {
			// Fallback for missing/broken images
			contentWidth = 100
			hasExplicitWidth = true
		}
	} else if display == css.DisplayInline {
		// Phase 7 Enhancement: Inline elements always shrink-wrap (ignore width property)
		contentWidth = 0
		hasExplicitWidth = false
	} else if w, ok := style.GetLength("width"); ok {
		contentWidth = w
		hasExplicitWidth = true
	} else if pct, ok := style.GetPercentage("width"); ok {
		// Percentage width resolved against containing block
		cbWidth := availableWidth
		if style.GetPosition() == css.PositionFixed {
			cbWidth = le.viewport.width
		}
		contentWidth = cbWidth * pct / 100
		hasExplicitWidth = true
	} else if style.GetPosition() == css.PositionAbsolute || style.GetPosition() == css.PositionFixed {
		// Absolutely positioned elements without explicit width shrink-wrap
		contentWidth = 0
	} else if floatType != css.FloatNone {
		// CSS 2.1 §10.3.5: Floated elements without explicit width use shrink-to-fit
		contentWidth = 0
	} else if display == css.DisplayTable {
		// CSS 2.1 §17.5.2: Tables without explicit width use shrink-to-fit
		contentWidth = 0
	} else {
		// Default to available width minus horizontal margin, padding, border
		contentWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
	}

	// Calculate content height
	var contentHeight float64
	hasExplicitHeight := false
	// Phase 8: Images use image dimensions or explicit dimensions
	if isImage {
		if h, ok := style.GetLength("height"); ok {
			contentHeight = h
		} else if heightAttr, ok := node.GetAttribute("height"); ok {
			// Parse height attribute
			if h, ok := css.ParseLength(heightAttr); ok {
				contentHeight = h
			}
		} else if imageHeight > 0 {
			// Use natural image height, maintaining aspect ratio if width was specified
			if hasExplicitWidth && imageWidth > 0 {
				// Scale height to maintain aspect ratio
				contentHeight = contentWidth * float64(imageHeight) / float64(imageWidth)
			} else {
				contentHeight = float64(imageHeight)
			}
		} else {
			// Fallback for missing/broken images
			contentHeight = 100
		}
	} else if display == css.DisplayInline {
		// Phase 7 Enhancement: Inline elements always shrink-wrap (ignore height property)
		contentHeight = 0
	} else if h, ok := style.GetLength("height"); ok {
		contentHeight = h
		hasExplicitHeight = true
	} else if hPct, ok := style.GetPercentage("height"); ok {
		// CSS 2.1 §10.5: Percentage heights resolve against containing block height
		cbHeight := 0.0
		if node.TagName == "html" {
			// Root element: containing block is initial containing block (viewport)
			cbHeight = le.viewport.height
		} else if style.GetPosition() == css.PositionAbsolute || style.GetPosition() == css.PositionFixed {
			// CSS 2.1 §10.1: For absolutely/fixed positioned elements, the containing
			// block is the nearest positioned ancestor's padding box (or viewport)
			cb := findPositionedAncestorBox(parent)
			if cb == nil || style.GetPosition() == css.PositionFixed {
				cbHeight = le.viewport.height
			} else {
				cbHeight = cb.Height - cb.Border.Top - cb.Border.Bottom
			}
		} else if parent != nil && parent.Style != nil {
			// Non-root: resolve against parent's content height if parent has explicit height
			_, hasLen := parent.Style.GetLength("height")
			_, hasPct := parent.Style.GetPercentage("height")
			if hasLen || hasPct {
				cbHeight = parent.Height - parent.Padding.Top - parent.Padding.Bottom - parent.Border.Top - parent.Border.Bottom
			}
		}
		if cbHeight > 0 {
			contentHeight = cbHeight * hPct / 100
			hasExplicitHeight = true
		}
		// else: containing block height depends on content → treat as auto
	} else {
		contentHeight = 0 // Auto height - will be calculated from children
	}

	// Apply min/max width constraints
	if minWidth, ok := style.GetLength("min-width"); ok {
		if contentWidth < minWidth {
			contentWidth = minWidth
		}
	}
	if maxWidth, ok := style.GetLength("max-width"); ok {
		if contentWidth > maxWidth {
			contentWidth = maxWidth
		}
	}

	// Apply min/max height constraints (min-height overrides max-height per CSS 2.1 10.7)
	maxHeightVal := 0.0
	hasMaxHeight := false
	if mh, ok := style.GetLength("max-height"); ok {
		maxHeightVal = mh
		hasMaxHeight = true
	} else if mhPct, ok := style.GetPercentage("max-height"); ok {
		cbHeight := 0.0
		if node.TagName == "html" {
			cbHeight = le.viewport.height
		} else if parent != nil && parent.Style != nil {
			_, hasLen := parent.Style.GetLength("height")
			_, hasPct := parent.Style.GetPercentage("height")
			if hasLen || hasPct {
				cbHeight = parent.Height - parent.Padding.Top - parent.Padding.Bottom - parent.Border.Top - parent.Border.Bottom
			}
		}
		if cbHeight > 0 {
			maxHeightVal = cbHeight * mhPct / 100
			hasMaxHeight = true
		}
	}
	if hasMaxHeight && contentHeight > maxHeightVal {
		contentHeight = maxHeightVal
	}
	minHeightVal := 0.0
	hasMinHeight := false
	if mh, ok := style.GetLength("min-height"); ok {
		minHeightVal = mh
		hasMinHeight = true
	} else if mhPct, ok := style.GetPercentage("min-height"); ok {
		cbHeight := 0.0
		if node.TagName == "html" {
			cbHeight = le.viewport.height
		} else if parent != nil && parent.Style != nil {
			_, hasLen := parent.Style.GetLength("height")
			_, hasPct := parent.Style.GetPercentage("height")
			if hasLen || hasPct {
				cbHeight = parent.Height - parent.Padding.Top - parent.Padding.Bottom - parent.Border.Top - parent.Border.Bottom
			}
		}
		if cbHeight > 0 {
			minHeightVal = cbHeight * mhPct / 100
			hasMinHeight = true
		}
	}
	if hasMinHeight && contentHeight < minHeightVal {
		contentHeight = minHeightVal
	}

	// Phase 13: Handle margin: auto for horizontal centering
	// Only center if both left and right margins are auto
	if margin.AutoLeft && margin.AutoRight {
		// For block-level elements with auto margins, center them
		// Calculate total width including padding and border
		totalWidth := contentWidth + padding.Left + padding.Right + border.Left + border.Right
		// Center within available width
		if totalWidth < availableWidth {
			centerOffset := (availableWidth - totalWidth) / 2
			x = x + centerOffset
		}
	}

	// Phase 4: Get positioning information
	position := style.GetPosition()
	zindex := style.GetZIndex()

	// DEBUG: Print all div IDs
	if node != nil && node.TagName == "div" {
		id := "(no id)"
		if node.Attributes != nil {
			if nodeID, ok := node.Attributes["id"]; ok {
				id = nodeID
			}
		}
		fmt.Printf("DEBUG CSS: div id='%s' position=%v\n", id, position)
	}

	// Phase 5: Check for clear property
	clearType := style.GetClear()

	// Phase 5: Handle clear property - move Y down past floats
	if clearType != css.ClearNone {
		y = le.getClearY(clearType, y)
	}

	box := &Box{
		Node:      node,
		Style:     style,
		X:         x,
		Y:         y,
		Width:     contentWidth + padding.Left + padding.Right + border.Left + border.Right,
		Height:    contentHeight + padding.Top + padding.Bottom + border.Top + border.Bottom,
		Margin:    margin,
		Padding:   padding,
		Border:    border,
		Children:  make([]*Box, 0),
		Position:  position,
		ZIndex:    zindex,
		Parent:    parent,
		ImagePath: imagePath, // Phase 8: Store image path for rendering
	}

	// Phase 5: Float positioning will be done AFTER children are laid out
	// (to support shrink-wrapping and float drop)

	// Phase 4: Handle positioning
	if node != nil && node.TagName == "div" {
		posName := "static"
		switch position {
		case css.PositionRelative:
			posName = "relative"
		case css.PositionAbsolute:
			posName = "absolute"
		case css.PositionFixed:
			posName = "fixed"
		}
		if position != css.PositionStatic {
			fmt.Printf("DEBUG LAYOUT POS2: div position=%s (%v)\n", posName, position)
		}
	}
	if position == css.PositionRelative {
		// Relative positioning: offset from normal position
		offset := style.GetPositionOffset()
		oldY := box.Y
		oldX := box.X
		if offset.HasTop {
			box.Y += offset.Top
		} else if offset.HasBottom {
			box.Y -= offset.Bottom
		}
		if offset.HasLeft {
			box.X += offset.Left
		} else if offset.HasRight {
			box.X -= offset.Right
		}
		if oldY != box.Y || oldX != box.X {
			tagInfo := "?"
			if node != nil {
				tagInfo = node.TagName
				if node.Attributes != nil {
					if id, ok := node.Attributes["id"]; ok && id != "" {
						tagInfo += "#" + id
					}
				}
			}
			fmt.Printf("DEBUG LAYOUT POS: %s relative offset applied: (%.1f,%.1f) -> (%.1f,%.1f)\n",
				tagInfo, oldX, oldY, box.X, box.Y)
			// Also check background color
			if bgColor, ok := style.Get("background-color"); ok && bgColor != "" && bgColor != "transparent" {
				fmt.Printf("DEBUG LAYOUT POS:   %s has background-color=%s at final Y=%.1f\n", tagInfo, bgColor, box.Y)
			}
		}
	} else if position == css.PositionAbsolute || position == css.PositionFixed {
		// Absolutely positioned elements - positioning applied after children layout
		le.absoluteBoxes = append(le.absoluteBoxes, box)
	}

	// Phase 9: Handle table layout specially
	if display == css.DisplayTable {
		le.layoutTable(box, x, y, availableWidth, computedStyles)
		return box
	}

	// Phase 10: Handle flexbox layout specially
	if display == css.DisplayFlex || display == css.DisplayInlineFlex {
		le.layoutFlex(box, x, y, availableWidth, computedStyles)
		return box
	}

	// Phase 15: Handle grid layout specially
	if display == css.DisplayGrid || display == css.DisplayInlineGrid {
		return le.layoutGridContainer(node, x, y, availableWidth, style, computedStyles, parent)
	}

	// Check if this element creates a new block formatting context (BFC)
	createsBFC := false
	if style.GetOverflow() != css.OverflowVisible || floatType != css.FloatNone ||
		position == css.PositionAbsolute || position == css.PositionFixed ||
		display == css.DisplayInlineBlock {
		createsBFC = true
	}
	if createsBFC {
		le.floatBaseStack = append(le.floatBaseStack, le.floatBase)
		le.floatBase = len(le.floats)
	}

	// CSS Counter support: Process counter-reset on this element
	var counterResets map[string]int
	if resetVal, ok := style.Get("counter-reset"); ok {
		counterResets = parseCounterReset(resetVal)
		for name, value := range counterResets {
			le.counterReset(name, value)
		}
	}

	// Phase 2: Recursively layout children
	// Use box.X/Y which include relative positioning offset
	childY := box.Y + border.Top + padding.Top
	childAvailableWidth := contentWidth

	// For shrink-to-fit elements (floats, abs pos without explicit width), pass the parent's
	// available width to children so they can lay out naturally, then we'll shrink-wrap around them
	if contentWidth == 0 && (floatType != css.FloatNone || position == css.PositionAbsolute || position == css.PositionFixed) {
		childAvailableWidth = availableWidth - padding.Left - padding.Right - border.Left - border.Right
	}

	// Track previous block child for margin collapsing between siblings
	var prevBlockChild *Box
	var pendingMargins []float64 // margins from collapse-through elements

	// Phase 7: Determine which inline layout algorithm to use
	// Use multi-pass only for pure inline formatting contexts (no block children)
	algorithm := InlineLayoutSinglePass

	// Check if we should use multi-pass (only for containers without pseudo-elements)
	// hasPseudo := le.hasPseudoElements(node, computedStyles) // REMOVED: Allow multi-pass with pseudo-elements
	hasFloats := false
	hasBlockChild := false
	hasInlineChild := false
	didAnalyzeChildren := false // Track if we analyzed children

	if (display == css.DisplayBlock || display == css.DisplayInline) {
		didAnalyzeChildren = true
		// Check children to determine if this is a pure inline formatting context

		for _, child := range node.Children {
			if child.Type == html.ElementNode {
				if childStyle := computedStyles[child]; childStyle != nil {
					childDisplay := childStyle.GetDisplay()

					// Check for block-level children
					if childDisplay == css.DisplayBlock || childDisplay == css.DisplayTable ||
					   childDisplay == css.DisplayListItem || childDisplay == css.DisplayFlex ||
					   childDisplay == css.DisplayGrid {
						hasBlockChild = true
					}

					// Check for inline children
					if childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock {
						hasInlineChild = true

						// Check if this inline child is floated
						if childStyle.GetFloat() != css.FloatNone {
							hasFloats = true
						}
					}
				}
			} else if child.Type == html.TextNode {
				hasInlineChild = true
			}
		}

		// Use multi-pass for ALL inline formatting contexts (per user request)
		// Requirements:
		// 1. Has some inline content (block children handled as InlineItemBlockChild)
		// 2. Not an object with image
		// 3. Container is a BLOCK (not inline - inline containers have complex fragment splitting)
		// EXPERIMENTAL: Allow mixed block/inline content - block children handled in multi-pass
		if hasInlineChild && !isObjectImage && display == css.DisplayBlock {
			algorithm = InlineLayoutMultiPass
			// DEBUG: Log when multi-pass is triggered
			if node.TagName != "" {
				fmt.Printf("DEBUG: Multi-pass triggered for <%s> (floats=%v, blockChild=%v, inlineChild=%v)\n",
					node.TagName, hasFloats, hasBlockChild, hasInlineChild)
			}
		}
	}

	// NEW ARCHITECTURE: If multi-pass is enabled AND we have a pure inline formatting context,
	// use clean LayoutInlineContentToBoxes. Multi-pass can only handle inline content, not mixed block/inline.
	var childBoxes []*Box
	var inlineLayoutResult *InlineLayoutResult

	// Check if we can use multi-pass (we analyzed children)
	// Block children are now supported via recursive layoutNode calls
	canUseMultiPass := le.useMultiPass && didAnalyzeChildren

	// DEBUG: Log why we're not using multi-pass
	if node.TagName == "body" {
		fmt.Printf("DEBUG MULTIPASS CHECK: useMultiPass=%v, didAnalyze=%v, canUse=%v\n",
			le.useMultiPass, didAnalyzeChildren, canUseMultiPass)
	}

	if canUseMultiPass {
		// Create synthetic nodes for pseudo-elements so they go through the same
		// multi-pass pipeline as real elements (identical sizing and positioning)
		overrideStyles := make(map[*html.Node]*css.Style)
		extendedChildren := make([]*html.Node, 0, len(node.Children)+2)

		// ::before pseudo-element -> synthetic node
		beforeNode, beforeStyle := le.createPseudoElementNode(node, "before", computedStyles)
		if beforeNode != nil {
			overrideStyles[beforeNode] = beforeStyle
			// Also add override styles for synthetic children (img nodes)
			for _, child := range beforeNode.Children {
				if child.Type == html.ElementNode && child.TagName == "img" {
					imgStyle := css.NewStyle()
					imgStyle.Set("display", "inline-block")
					overrideStyles[child] = imgStyle
				}
			}
			extendedChildren = append(extendedChildren, beforeNode)
		}

		// Real children
		extendedChildren = append(extendedChildren, node.Children...)

		// ::after pseudo-element -> synthetic node
		afterNode, afterStyle := le.createPseudoElementNode(node, "after", computedStyles)
		if afterNode != nil {
			overrideStyles[afterNode] = afterStyle
			// Also add override styles for synthetic children (img nodes)
			for _, child := range afterNode.Children {
				if child.Type == html.ElementNode && child.TagName == "img" {
					imgStyle := css.NewStyle()
					imgStyle.Set("display", "inline-block")
					overrideStyles[child] = imgStyle
				}
			}
			extendedChildren = append(extendedChildren, afterNode)
		}

		// Use new three-phase multi-pass pipeline with extended children
		inlineLayoutResult = le.LayoutInlineContentToBoxes(
			extendedChildren,
			box,
			childAvailableWidth,
			childY,
			computedStyles,
			overrideStyles,
		)
		childBoxes = inlineLayoutResult.ChildBoxes

		// CRITICAL FIX: Apply margin collapsing between adjacent block siblings
		// LayoutInlineContentToBoxes doesn't handle margin collapsing, so we must do it here.
		// Adjustments are cumulative: when box N is moved up, all subsequent boxes must also
		// be moved up by the same amount (since their positions were computed relative to N's
		// pre-collapsing position).
		var prevBox *Box
		cumulativeAdjustment := 0.0
		for _, childBox := range childBoxes {
			if childBox == nil {
				continue
			}

			// Only collapse margins for block-level boxes in normal flow
			floatType := css.FloatNone
			if childBox.Style != nil {
				floatType = childBox.Style.GetFloat()
			}

			if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && floatType == css.FloatNone {
				// Apply cumulative adjustment from previous collapses
				if cumulativeAdjustment != 0 {
					childBox.Y -= cumulativeAdjustment
					le.adjustChildrenY(childBox, -cumulativeAdjustment)
				}

				// Check if both boxes should collapse margins
				if prevBox != nil && shouldCollapseMargins(prevBox) && shouldCollapseMargins(childBox) {
					collapsed := collapseMargins(prevBox.Margin.Bottom, childBox.Margin.Top)
					adjustment := prevBox.Margin.Bottom + childBox.Margin.Top - collapsed

					fmt.Printf("DEBUG MULTIPASS COLLAPSE: prev=%s, curr=%s, prevBottom=%.1f, currTop=%.1f, collapsed=%.1f, adjustment=%.1f, cumulative=%.1f\n",
						prevBox.Node.TagName, childBox.Node.TagName, prevBox.Margin.Bottom, childBox.Margin.Top, collapsed, adjustment, cumulativeAdjustment)

					childBox.Y -= adjustment
					le.adjustChildrenY(childBox, -adjustment)
					cumulativeAdjustment += adjustment
				}
				prevBox = childBox
			}
		}

		// Add all child boxes to the container
		box.Children = append(box.Children, childBoxes...)
	} else {
		// Use existing layout code
		// Layout inline children using detected algorithm
		// This handles ::before, child loop, ::after, and text-align
		inlineLayoutResult = le.layoutInlineChildren(
			node, box, display, style, border, padding, x, childY,
			childAvailableWidth, contentWidth, isObjectImage, computedStyles,
			&prevBlockChild, &pendingMargins, algorithm,
		)

		// Add all child boxes to the container
		box.Children = append(box.Children, inlineLayoutResult.ChildBoxes...)
		childBoxes = inlineLayoutResult.ChildBoxes
	}

	// NOTE: The rest of the old inline layout code (lines 700-1212) has been
	// extracted into layoutInlineChildrenSinglePass() and is now called above.
	// The following line preserves inline context for any code that might use it later.
	var inlineCtx *InlineContext
	if inlineLayoutResult != nil {
		inlineCtx = inlineLayoutResult.FinalInlineCtx
	}
	// Note: Both single-pass and multi-pass now provide inline context for height calculation

	// TEMPORARY: Keep the old inline layout code below commented out for reference
	// until we verify the refactor works correctly. Will be deleted once stable.
	/*
	// Phase 11: Generate ::before pseudo-element if it has content
	beforeBox := le.generatePseudoElement(node, "before", inlineCtx.LineX, inlineCtx.LineY, childAvailableWidth, computedStyles, box)
	if beforeBox != nil {
		beforeFloat := beforeBox.Style.GetFloat()
		if beforeFloat != css.FloatNone {
			// Position floated ::before pseudo-element
			floatWidth := le.getTotalWidth(beforeBox)
			// Pseudo-element floats position inline at current LineY, allowing overflow
			// rather than dropping to a new line like block-level floats
			floatY := inlineCtx.LineY
			leftOffset, rightOffset := le.getFloatOffsets(floatY)
			// Calculate new position
			var newX float64
			if beforeFloat == css.FloatLeft {
				// For left floats, position must clear both other floats (leftOffset) AND inline content (LineX)
				baseX := box.X + border.Left + padding.Left
				floatClearX := baseX + leftOffset + beforeBox.Margin.Left
				inlineEndX := inlineCtx.LineX + beforeBox.Margin.Left
				if inlineEndX > floatClearX {
					newX = inlineEndX
				} else {
					newX = floatClearX
				}
			} else {
				newX = box.X + border.Left + padding.Left + childAvailableWidth - rightOffset - floatWidth + beforeBox.Margin.Left
			}
			newY := floatY + beforeBox.Margin.Top

			// Calculate position delta to reposition children
			deltaX := newX - beforeBox.X
			deltaY := newY - beforeBox.Y

			// Reposition child boxes (e.g., images inside the pseudo-element)
			for _, child := range beforeBox.Children {
				child.X += deltaX
				child.Y += deltaY
			}

			beforeBox.X = newX
			beforeBox.Y = newY
			le.addFloat(beforeBox, beforeFloat, floatY)
			box.Children = append(box.Children, beforeBox)
		} else {
			box.Children = append(box.Children, beforeBox)
			// Update inline context for subsequent children
			beforeDisplay := beforeBox.Style.GetDisplay()
			if beforeDisplay == css.DisplayBlock {
				inlineCtx.LineY += le.getTotalHeight(beforeBox)
				inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
			} else {
				inlineCtx.LineX += le.getTotalWidth(beforeBox)
				if beforeBox.Height > inlineCtx.LineHeight {
					inlineCtx.LineHeight = beforeBox.Height
				}
			}
		}
	}

	// Phase 23: Generate list marker for list-item elements
	if display == css.DisplayListItem {
		markerBox := le.generateListMarker(node, style, x, inlineCtx.LineY, box)
		if markerBox != nil {
			box.Children = append(box.Children, markerBox)
		}
	}

	// Phase 24: Skip children for object elements that successfully loaded an image
	skipChildren := isObjectImage

	// Track block-in-inline for fragment splitting (CSS 2.1 §9.2.1.1)
	// When a block element is inside an inline element, the inline's borders are split
	isInlineParent := display == css.DisplayInline
	hasSeenBlockChild := false
	hasInlineContentBeforeBlock := false

	// Fragment tracking for block-in-inline
	// We track the bounding region of inline content to create fragments
	type fragmentRegion struct {
		startX, startY float64
		maxX, maxY     float64
		hasContent     bool
	}
	currentFragment := fragmentRegion{
		startX: box.X + border.Left + padding.Left,
		startY: box.Y + border.Top + padding.Top,
	}
	var completedFragments []fragmentRegion

	for _, child := range node.Children {
		if skipChildren {
			break
		}
		if child.Type == html.ElementNode {
			// DEBUG: Log which children are being processed
			if node.TagName == "body" {
				childID := "(no id)"
				if child.Attributes != nil {
					if id, ok := child.Attributes["id"]; ok {
						childID = id
					}
				}
				fmt.Printf("DEBUG BODY CHILD: Processing <%s id='%s'>\n", child.TagName, childID)
			}
			// Get child's computed style to check display mode
			childStyle := computedStyles[child]
			if childStyle == nil {
				childStyle = css.NewStyle()
			}
			childDisplay := childStyle.GetDisplay()

			// Determine initial X coordinate for child
			// For inline/inline-block elements, use LineX (accumulates horizontally)
			// For block elements and floats, use parent content area left edge
			childX := inlineCtx.LineX
			childFloat := childStyle.GetFloat()
			if childDisplay == css.DisplayBlock || childDisplay == css.DisplayTable ||
			   childDisplay == css.DisplayListItem || childDisplay == css.DisplayFlex ||
			   childDisplay == css.DisplayGrid || childFloat != css.FloatNone {
				// Block-level or floated: start from parent's left content edge
				childX = box.X + border.Left + padding.Left
			}

			// Layout the child
			childBox := le.layoutNode(
				child,
				childX,
				inlineCtx.LineY,
				childAvailableWidth,
				computedStyles,
				box, // Phase 4: Pass parent
			)

			// Phase 7: Skip elements with display: none (layoutNode returns nil)
			if childBox != nil {
				// Handle <br> elements - force a line break
				if child.TagName == "br" {
					// Move to next line
					if inlineCtx.LineHeight == 0 {
						inlineCtx.LineHeight = style.GetLineHeight()
					}
					inlineCtx.LineY += inlineCtx.LineHeight
					inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
					inlineCtx.LineHeight = 0
					inlineCtx.LineBoxes = make([]*Box, 0)
					// Don't add <br> to children - it's just a control element
					continue
				}

				// Phase 7: Handle inline and inline-block elements
				// Skip inline positioning for floated elements (they are positioned by float logic)
				childIsFloated := childStyle != nil && childStyle.GetFloat() != css.FloatNone
				if (childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock) && childBox.Position == css.PositionStatic && !childIsFloated {
					// Block-in-inline: mark inline content after a block as last fragment
					if isInlineParent && hasSeenBlockChild {
						childBox.IsLastFragment = true
					}
					if isInlineParent && !hasSeenBlockChild {
						hasInlineContentBeforeBlock = true
					}

					// Update fragment region with this inline child's bounds
					if isInlineParent {
						childRight := childBox.X + le.getTotalWidth(childBox)
						childBottom := childBox.Y + le.getTotalHeight(childBox)
						if childRight > currentFragment.maxX {
							currentFragment.maxX = childRight
						}
						if childBottom > currentFragment.maxY {
							currentFragment.maxY = childBottom
						}
						currentFragment.hasContent = true
					}

					childTotalWidth := le.getTotalWidth(childBox)

					// Check if child fits on current line (skip wrapping if white-space: nowrap)
					allowWrap := style.GetWhiteSpace() != css.WhiteSpaceNowrap
					if allowWrap && inlineCtx.LineX+childTotalWidth > box.X+border.Left+padding.Left+childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to next line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
						inlineCtx.LineHeight = 0
						inlineCtx.LineBoxes = make([]*Box, 0)

						// Reposition child at start of new line
						childBox.X = inlineCtx.LineX
						childBox.Y = inlineCtx.LineY
					} else {
						// Fits on current line - position it at the current LineX
						childBox.X = inlineCtx.LineX
						childBox.Y = inlineCtx.LineY
					}

					// Add to current line
					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, childBox)
					childHeight := le.getTotalHeight(childBox)
					if childHeight > inlineCtx.LineHeight {
						inlineCtx.LineHeight = childHeight
					}
					// CSS 2.1 §10.8.1: The "strut" ensures line box height is at least
					// the block container's line-height
					strutHeight := style.GetLineHeight()
					if strutHeight > inlineCtx.LineHeight {
						inlineCtx.LineHeight = strutHeight
					}

					// Advance X for next inline-block element
					inlineCtx.LineX += childTotalWidth

					box.Children = append(box.Children, childBox)

					// Phase 7 Enhancement: Apply vertical-align to inline element
					le.applyVerticalAlign(childBox, inlineCtx.LineY, inlineCtx.LineHeight)
				} else {
					// Block element or other display mode
					// Block-in-inline: when a block is inside an inline parent, mark fragments
					if isInlineParent && hasInlineContentBeforeBlock {
						// Complete the current fragment (content before the block)
						if currentFragment.hasContent {
							completedFragments = append(completedFragments, currentFragment)
						}
						// Start a new fragment for content after the block
						// (will be positioned after block layout is done)
						hasSeenBlockChild = true
						// Mark legacy flags for backward compatibility
						box.IsFirstFragment = true
					}

					// Finish current inline line (apply strut for line box height)
					if len(inlineCtx.LineBoxes) > 0 {
						strutHeight := style.GetLineHeight()
						if strutHeight > inlineCtx.LineHeight {
							inlineCtx.LineHeight = strutHeight
						}
						childY = inlineCtx.LineY + inlineCtx.LineHeight
						inlineCtx.LineBoxes = make([]*Box, 0)
						inlineCtx.LineHeight = 0
					} else {
						childY = inlineCtx.LineY
					}

					// Update child position for block element (skip absolute/fixed - positioned later, skip floats - positioned by float logic)
					childFloatTypePos := css.FloatNone
					if childStyle != nil {
						childFloatTypePos = childStyle.GetFloat()
					}
					if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && childFloatTypePos == css.FloatNone {
						// For position:relative, preserve the offset that was already applied
						relativeOffsetY := 0.0
						if childBox.Position == css.PositionRelative && childStyle != nil {
							offset := childStyle.GetPositionOffset()
							if offset.HasTop {
								relativeOffsetY = offset.Top
							} else if offset.HasBottom {
								relativeOffsetY = -offset.Bottom
							}
						}
						// Calculate new position
						var newX float64
						if childBox.Margin.AutoLeft && childBox.Margin.AutoRight {
							childTotalW := childBox.Width + childBox.Padding.Left + childBox.Padding.Right + childBox.Border.Left + childBox.Border.Right
							parentContentStart := box.X + border.Left + padding.Left
							centerOff := (childAvailableWidth - childTotalW) / 2
							if centerOff < 0 {
								centerOff = 0
							}
							newX = parentContentStart + centerOff
						} else {
							newX = box.X + border.Left + padding.Left + childBox.Margin.Left
						}
						newY := childY + childBox.Margin.Top + relativeOffsetY

						// Shift children by the position delta (important for block-in-inline)
						dx := newX - childBox.X
						dy := newY - childBox.Y
						if dx != 0 || dy != 0 {
							le.shiftChildren(childBox, dx, dy)
						}
						childBox.X = newX
						childBox.Y = newY
					}

					box.Children = append(box.Children, childBox)

					// Advance Y for block elements
					childFloatType := childBox.Style.GetFloat()
					if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && childFloatType == css.FloatNone {
						// Margin-collapse-through: collect margins from collapse-through elements
						// and combine them with the next non-collapse-through sibling's margins.
						if isCollapseThrough(childBox) {
							// Add this element's margins (and children's) to pending list
							pendingMargins = append(pendingMargins, childBox.Margin.Top, childBox.Margin.Bottom)
							collectCollapseThroughChildMargins(childBox, &pendingMargins)
							// Position at childY (zero-height, no visual impact)
							childBox.Y = childY
							// Don't advance childY, don't set prevBlockChild
						} else {
							// Normal margin collapsing between adjacent block siblings
							// DEBUG: Check for div1 specifically
							isDiv1 := childBox.Node != nil && childBox.Node.TagName == "div" && childBox.Node.Attributes != nil
							if isDiv1 {
								if id, ok := childBox.Node.Attributes["id"]; ok && id == "div1" {
									fmt.Printf("DEBUG DIV1: prevBlockChild=%v, shouldCollapse(prev)=%v, shouldCollapse(div1)=%v\n",
										prevBlockChild != nil,
										prevBlockChild != nil && shouldCollapseMargins(prevBlockChild),
										shouldCollapseMargins(childBox))
									if prevBlockChild != nil {
										fmt.Printf("DEBUG DIV1: prevBlockChild.Margin.Bottom=%.1f, childBox.Margin.Top=%.1f\n",
											prevBlockChild.Margin.Bottom, childBox.Margin.Top)
									}
								}
							}
							if prevBlockChild != nil && shouldCollapseMargins(prevBlockChild) && shouldCollapseMargins(childBox) {
								// Collect all margins: prev bottom, any pending from collapse-through, current top
								allMargins := []float64{prevBlockChild.Margin.Bottom}
								allMargins = append(allMargins, pendingMargins...)
								allMargins = append(allMargins, childBox.Margin.Top)
								// Collapse all together
								var maxPos, minNeg float64
								for _, m := range allMargins {
									if m > maxPos {
										maxPos = m
									}
									if m < minNeg {
										minNeg = m
									}
								}
								collapsed := maxPos + minNeg
								// Only real margins used space; pending margins were from zero-height elements
								totalUsed := prevBlockChild.Margin.Bottom + childBox.Margin.Top
								adjustment := totalUsed - collapsed
								childBox.Y -= adjustment
								le.adjustChildrenY(childBox, -adjustment)
							} else if len(pendingMargins) > 0 && shouldCollapseMargins(childBox) {
								// No prev sibling but pending margins from collapse-through
								allMargins := append(pendingMargins, childBox.Margin.Top)
								var maxPos, minNeg float64
								for _, m := range allMargins {
									if m > maxPos {
										maxPos = m
									}
									if m < minNeg {
										minNeg = m
									}
								}
								collapsed := maxPos + minNeg
								totalUsed := childBox.Margin.Top
								adjustment := totalUsed - collapsed
								childBox.Y -= adjustment
								le.adjustChildrenY(childBox, -adjustment)
							}
							pendingMargins = nil
							// Apply clear property after margin collapsing
							if childBox.Style != nil {
								childClear := childBox.Style.GetClear()
								if childClear != css.ClearNone {
									clearY := le.getClearY(childClear, childBox.Y)
									if clearY > childBox.Y {
										delta := clearY - childBox.Y
										childBox.Y = clearY
										le.adjustChildrenY(childBox, delta)
									}
								}
							}
							childY = childBox.Y + childBox.Border.Top + childBox.Padding.Top + childBox.Height + childBox.Padding.Bottom + childBox.Border.Bottom + childBox.Margin.Bottom
							prevBlockChild = childBox
						}
					}

					// Reset inline context for next line
					inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
					inlineCtx.LineY = childY

					// Reset fragment tracking for next fragment (content after this block)
					if isInlineParent {
						currentFragment = fragmentRegion{
							startX: inlineCtx.LineX,
							startY: inlineCtx.LineY,
						}
					}
				}
			}
		} else if child.Type == html.TextNode {
			// Phase 6: Layout text nodes
			// Always use inline flow so text nodes participate in the inline
			// formatting context together with sibling inline elements (e.g. <em>).
			// layoutTextNode already handles float offsets internally, so pass the
			// original position and let it adjust for floats
			// Ensure LineX accounts for any floats that were added (e.g., floated ::before)
			le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
			textBox := le.layoutTextNode(
				child,
				inlineCtx.LineX,
				inlineCtx.LineY,
				box.X+border.Left+padding.Left+childAvailableWidth-inlineCtx.LineX,
				style, // Text inherits parent's style
				box,
			)
			if textBox != nil {
				// Block-in-inline: track and mark text fragments
				if isInlineParent {
					if hasSeenBlockChild {
						textBox.IsLastFragment = true
					} else {
						hasInlineContentBeforeBlock = true
					}
					// Update fragment region with this text's bounds
					textRight := textBox.X + le.getTotalWidth(textBox)
					textBottom := textBox.Y + le.getTotalHeight(textBox)
					if textRight > currentFragment.maxX {
						currentFragment.maxX = textRight
					}
					if textBottom > currentFragment.maxY {
						currentFragment.maxY = textBottom
					}
					currentFragment.hasContent = true
				}
				box.Children = append(box.Children, textBox)

				// For multi-line text containers, the inline context should
				// continue after the LAST line, not after the full container width.
				if len(textBox.Children) > 0 {
					// Multi-line text: advance to end of last line
					lastLine := textBox.Children[len(textBox.Children)-1]
					inlineCtx.LineY = lastLine.Y
					inlineCtx.LineX = lastLine.X + le.getTotalWidth(lastLine)
					inlineCtx.LineHeight = le.getTotalHeight(lastLine)
					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, textBox)
				} else {
					// Single-line text
					textWidth := le.getTotalWidth(textBox)
					textHeight := le.getTotalHeight(textBox)

					// Check if text fits on current line (skip wrapping if white-space: nowrap)
					allowWrap := style.GetWhiteSpace() != css.WhiteSpaceNowrap
					if allowWrap && inlineCtx.LineX+textWidth > box.X+border.Left+padding.Left+childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to new line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
						inlineCtx.LineHeight = textHeight
						textBox.X = inlineCtx.LineX
						textBox.Y = inlineCtx.LineY
						inlineCtx.LineX += textWidth
						le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
					} else {
						// Fits on current line (or is the first item on the line)
						inlineCtx.LineX += textWidth
						le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
						if textHeight > inlineCtx.LineHeight {
							inlineCtx.LineHeight = textHeight
						}
					}

					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, textBox)
				}
			}
		}
	}

	// Phase 11: Generate ::after pseudo-element if it has content
	afterBox := le.generatePseudoElement(node, "after", inlineCtx.LineX, inlineCtx.LineY, childAvailableWidth, computedStyles, box)
	if afterBox != nil {
		afterFloat := afterBox.Style.GetFloat()
		if afterFloat != css.FloatNone {
			// Position floated ::after pseudo-element
			floatWidth := le.getTotalWidth(afterBox)
			// Pseudo-element floats position inline at current LineY, allowing overflow
			// rather than dropping to a new line like block-level floats
			floatY := inlineCtx.LineY
			leftOffset, rightOffset := le.getFloatOffsets(floatY)

			// Calculate new position
			var newX float64
			if afterFloat == css.FloatLeft {
				// For left floats, position must clear both other floats (leftOffset) AND inline content (LineX)
				baseX := box.X + border.Left + padding.Left
				floatClearX := baseX + leftOffset + afterBox.Margin.Left
				inlineEndX := inlineCtx.LineX + afterBox.Margin.Left
				if inlineEndX > floatClearX {
					newX = inlineEndX
				} else {
					newX = floatClearX
				}
			} else {
				newX = box.X + border.Left + padding.Left + childAvailableWidth - rightOffset - floatWidth + afterBox.Margin.Left
			}
			newY := floatY + afterBox.Margin.Top

			// Calculate position delta to reposition children
			deltaX := newX - afterBox.X
			deltaY := newY - afterBox.Y

			// Reposition child boxes (e.g., images inside the pseudo-element)
			for _, child := range afterBox.Children {
				child.X += deltaX
				child.Y += deltaY
			}

			afterBox.X = newX
			afterBox.Y = newY
			le.addFloat(afterBox, afterFloat, floatY)
		}
		box.Children = append(box.Children, afterBox)
	}

	// Finalize block-in-inline fragments
	// If we're an inline parent that was split by block children, create the fragment boxes
	if isInlineParent && hasSeenBlockChild {
		// Complete the final fragment (content after the last block)
		if currentFragment.hasContent {
			completedFragments = append(completedFragments, currentFragment)
		}

		// Create BoxFragment objects for rendering
		for i, frag := range completedFragments {
			if !frag.hasContent {
				continue
			}

			// Determine which borders this fragment should have
			borders := AllBorders()
			if i == 0 {
				// First fragment: has left border, no right border
				borders.Right = false
			}
			if i == len(completedFragments)-1 {
				// Last fragment: has right border, no left border
				borders.Left = false
			}

			// Calculate fragment dimensions including padding/border
			fragWidth := frag.maxX - frag.startX + border.Left + border.Right + padding.Left + padding.Right
			fragHeight := frag.maxY - frag.startY + border.Top + border.Bottom + padding.Top + padding.Bottom

			box.AddFragment(
				frag.startX-border.Left-padding.Left,
				frag.startY-border.Top-padding.Top,
				fragWidth,
				fragHeight,
				borders,
			)
		}
	}

	// Apply text-align to inline children (only for block containers, not inline elements)
	if display != css.DisplayInline && display != css.DisplayInlineBlock {
		if textAlign, ok := style.Get("text-align"); ok && textAlign != "left" && textAlign != "" {
			le.applyTextAlign(box, textAlign, contentWidth)
		}
	}
	*/
	// END OF COMMENTED OLD INLINE LAYOUT CODE - will be removed once refactor is verified

	// Parent-child top margin collapsing
	// If parent has no border-top/padding-top, collapse with first block child's top margin
	if parentCanCollapseTopMargin(box) && shouldCollapseMargins(box) {
		// Find first in-flow block child
		var firstBlockChild *Box
		for _, ch := range box.Children {
			if ch.Style != nil && ch.Style.GetFloat() != css.FloatNone {
				continue
			}
			if ch.Position == css.PositionAbsolute || ch.Position == css.PositionFixed {
				continue
			}
			if ch.Style != nil {
				d := ch.Style.GetDisplay()
				if d == css.DisplayInline || d == css.DisplayInlineBlock {
					break // inline content separates margins
				}
			}
			firstBlockChild = ch
			break
		}
		if firstBlockChild != nil && shouldCollapseMargins(firstBlockChild) && firstBlockChild.Margin.Top > 0 {
			childMarginTop := firstBlockChild.Margin.Top
			// Pull all children up by the first child's top margin
			for _, ch := range box.Children {
				ch.Y -= childMarginTop
				le.adjustChildrenY(ch, -childMarginTop)
			}
			// Compute collapsed margin
			collapsed := collapseMargins(margin.Top, childMarginTop)
			marginDiff := collapsed - margin.Top
			box.Margin.Top = collapsed
			if marginDiff != 0 {
				box.Y += marginDiff
				for _, ch := range box.Children {
					ch.Y += marginDiff
					le.adjustChildrenY(ch, marginDiff)
				}
			}
		}
	}

	// If height is auto and we have children, adjust height to fit content
	if !hasExplicitHeight && len(box.Children) > 0 {
		// Calculate height based on maximum bottom edge of children (not sum)
		// This correctly handles overlapping children (like floats with blocks)
		parentContentTop := box.Y + box.Border.Top + box.Padding.Top
		maxBottom := 0.0

		// CSS 2.1 §8.3.1 / §10.6.3: Parent-child bottom margin collapsing.
		// When parent has no bottom border and no bottom padding (and auto height),
		// the last in-flow child's bottom margin collapses with the parent's bottom
		// margin, so it should NOT be included in the auto-height calculation.
		// Note: Margin collapsing does NOT apply to absolutely positioned elements,
		// which establish a new block formatting context (CSS 2.1 §9.4.1).
		parentChildBottomCollapse := box.Border.Bottom == 0 && box.Padding.Bottom == 0 &&
			position != css.PositionAbsolute && position != css.PositionFixed
		var lastInFlowChild *Box
		if parentChildBottomCollapse {
			for _, child := range box.Children {
				if child.Position != css.PositionAbsolute && child.Position != css.PositionFixed {
					lastInFlowChild = child
				}
			}
		}

		for _, child := range box.Children {
			if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
				continue
			}
			// Calculate child's bottom edge relative to parent content area
			// For position:relative children, use their normal flow position
			// (CSS 2.1 §10.6.3: relative offset doesn't affect parent height)
			childY := child.Y
			if child.Position == css.PositionRelative && child.Style != nil {
				offset := child.Style.GetPositionOffset()
				if offset.HasTop {
					childY -= offset.Top
				} else if offset.HasBottom {
					childY += offset.Bottom
				}
			}
			childRelativeY := childY - parentContentTop
			// Use height from child's border-top edge (child.Y) downward:
			// border + padding + content + padding + border + margin-bottom.
			// Don't include margin-top since child.Y already accounts for it.
			childMarginBottom := child.Margin.Bottom
			if parentChildBottomCollapse && child == lastInFlowChild {
				// Last child's margin-bottom collapses through the parent
				childMarginBottom = 0
			}
			// Box.Height is ALWAYS border-box (content + padding + borders) - set at line 325.
			var childHeight float64
			if child.Style != nil && child.Style.GetDisplay() == css.DisplayInline {
				// IMPORTANT: For inline elements, use LINE BOX height (not wrapper box height)
				// CSS 2.1 §10.8.1: Borders/padding "bleed" outside line box, don't affect container height
				// The wrapper box Height includes borders/padding for rendering, but container should
				// only grow by the line box height. Skip inline wrapper boxes here - they're handled
				// by the inlineCtx.LineBoxes check below
				childHeight = 0  // Don't count inline wrapper box height twice
			} else {
				// Block: Height is already border-box, just add margin-bottom
				childHeight = child.Height + childMarginBottom
			}
			childBottom := childRelativeY + childHeight
			if childBottom > maxBottom {
				maxBottom = childBottom
			}
		}
		// CSS 2.1 §10.8.1: Account for trailing inline line box height (including strut)
		// Only count in-flow boxes — absolutely positioned/fixed elements don't generate line boxes
		hasInFlowLineBoxes := false
		if inlineCtx != nil {
			for _, lb := range inlineCtx.LineBoxes {
				if lb.Position != css.PositionAbsolute && lb.Position != css.PositionFixed {
					hasInFlowLineBoxes = true
					break
				}
			}
		}
		if hasInFlowLineBoxes {
			strutHeight := style.GetLineHeight()
			lineBoxHeight := inlineCtx.LineHeight
			if strutHeight > lineBoxHeight {
				lineBoxHeight = strutHeight
			}
			lineBottom := (inlineCtx.LineY - parentContentTop) + lineBoxHeight

			// DEBUG: Check if this is div2
			nodeID := ""
			if node != nil && node.Attributes != nil {
				if id, ok := node.Attributes["id"]; ok {
					nodeID = id
				}
			}
			if nodeID == "div2" {
				fmt.Printf("DEBUG AUTO-HEIGHT div2: strutHeight=%.1f, lineBoxHeight=%.1f, inlineCtx.LineY=%.1f, parentContentTop=%.1f, lineBottom=%.1f, maxBottom before=%.1f\n",
					strutHeight, lineBoxHeight, inlineCtx.LineY, parentContentTop, lineBottom, maxBottom)
			}

			if lineBottom > maxBottom {
				maxBottom = lineBottom
			}
		}
		if maxBottom < 0 {
			maxBottom = 0
		}
		// Box.Height must be border-box (content + padding + borders)
		// maxBottom is content height, so add padding and borders
		fmt.Printf("DEBUG AUTO-HEIGHT: Final maxBottom=%.1f, setting box.Height=%.1f (maxBottom + padding %.1f + borders %.1f)\n",
			maxBottom, maxBottom + box.Padding.Top + box.Padding.Bottom + box.Border.Top + box.Border.Bottom,
			box.Padding.Top + box.Padding.Bottom, box.Border.Top + box.Border.Bottom)
		box.Height = maxBottom + box.Padding.Top + box.Padding.Bottom + box.Border.Top + box.Border.Bottom

		// CSS 2.1 §8.3.1: When parent-child bottom margin collapsing applies,
		// propagate the last child's bottom margin to the parent's bottom margin.
		// The collapsed margin is the combination of parent's and child's margins.
		if parentChildBottomCollapse && lastInFlowChild != nil && lastInFlowChild.Margin.Bottom != 0 {
			parentMB := box.Margin.Bottom
			childMB := lastInFlowChild.Margin.Bottom
			if parentMB >= 0 && childMB >= 0 {
				if childMB > parentMB {
					box.Margin.Bottom = childMB
				}
			} else if parentMB < 0 && childMB < 0 {
				if childMB < parentMB {
					box.Margin.Bottom = childMB
				}
			} else {
				box.Margin.Bottom = parentMB + childMB
			}
		}
	}

	// Re-apply min/max height constraints after auto-height calculation
	if maxHeight, ok := style.GetLength("max-height"); ok {
		if box.Height > maxHeight {
			box.Height = maxHeight
		}
	}
	if minHeight, ok := style.GetLength("min-height"); ok {
		if box.Height < minHeight {
			box.Height = minHeight
		}
	}

	// Phase 7 Enhancement: Inline elements always shrink-wrap to children
	// DEBUG: Check all inline elements
	if display == css.DisplayInline && box.Node != nil && box.Node.TagName == "span" {
		fmt.Printf("DEBUG INLINE: <span> before shrinkwrap: Height=%.1f, Children=%d\n", box.Height, len(box.Children))
	}

	if display == css.DisplayInline && len(box.Children) > 0 {
		// DEBUG: Track if this is overwriting multi-pass wrapper box height
		originalHeight := box.Height

		// Calculate width from children
		// For inline formatting context, children flow horizontally so we SUM their widths
		totalChildWidth := 0.0
		maxChildHeight := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			totalChildWidth += childWidth
			childHeight := le.getTotalHeight(child)
			if childHeight > maxChildHeight {
				maxChildHeight = childHeight
			}
		}

		if box.Node != nil && box.Node.TagName == "span" && originalHeight != maxChildHeight {
			fmt.Printf("DEBUG SHRINKWRAP: <span> height being overwritten: %.1f → %.1f (diff: %.1f)\n",
				originalHeight, maxChildHeight, originalHeight-maxChildHeight)
		}

		box.Width = totalChildWidth
		box.Height = maxChildHeight
	}

	// Phase 5 Enhancement: Float shrink-wrapping
	// If this is a float without explicit width, shrink-wrap to content
	if floatType != css.FloatNone && !hasExplicitWidth && len(box.Children) > 0 {
		// For inline formatting context (inline children), sum widths horizontally
		// For block formatting context (block children), take max width (vertical stacking)
		allInline := true
		for _, child := range box.Children {
			if child.Style != nil {
				childDisplay := child.Style.GetDisplay()
				if childDisplay != css.DisplayInline && childDisplay != css.DisplayInlineBlock && child.Node != nil && child.Node.Type != html.TextNode {
					allInline = false
					break
				}
			}
		}

		if allInline {
			// Inline formatting context: sum widths
			totalWidth := 0.0
			for _, child := range box.Children {
				totalWidth += le.getTotalWidth(child)
			}
			if totalWidth > 0 {
				box.Width = totalWidth
			}
		} else {
			// Block formatting context: take max width
			maxChildWidth := 0.0
			for _, child := range box.Children {
				childWidth := le.getTotalWidth(child)
				if childWidth > maxChildWidth {
					maxChildWidth = childWidth
				}
			}
			if maxChildWidth > 0 {
				box.Width = maxChildWidth
			}
		}
	}

	// Shrink-wrap absolutely positioned elements without explicit width
	if (position == css.PositionAbsolute || position == css.PositionFixed) && !hasExplicitWidth && len(box.Children) > 0 {
		maxChildWidth := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			if childWidth > maxChildWidth {
				maxChildWidth = childWidth
			}
		}
		if maxChildWidth > 0 {
			box.Width = maxChildWidth
		}
		// After shrink-wrap, update block children with auto width to use the new parent width
		for _, child := range box.Children {
			if child.Style != nil {
				childDisplay := child.Style.GetDisplay()
				if _, hasW := child.Style.GetLength("width"); !hasW && childDisplay != css.DisplayInline &&
					child.Style.GetFloat() == css.FloatNone &&
					child.Style.GetPosition() != css.PositionAbsolute && child.Style.GetPosition() != css.PositionFixed {
					child.Width = box.Width - child.Border.Left - child.Padding.Left - child.Padding.Right - child.Border.Right -
						child.Margin.Left - child.Margin.Right
					if child.Width < 0 {
						child.Width = 0
					}
					// Re-apply text-align with the updated width
					if child.Style != nil {
						if ta, ok := child.Style.Get("text-align"); ok && ta != "left" && ta != "" {
							le.applyTextAlign(child, ta, child.Width)
						}
					}
				}
			}
		}
	}

	// Phase 4: Apply absolute positioning AFTER children layout and height finalization
	if position == css.PositionAbsolute || position == css.PositionFixed {
		oldX, oldY := box.X, box.Y
		le.applyAbsolutePositioning(box)
		// Shift all children by the position delta
		dx, dy := box.X-oldX, box.Y-oldY
		if dx != 0 || dy != 0 {
			le.shiftChildren(box, dx, dy)
		}
	}

	// Phase 5: Handle float positioning AFTER children layout and shrink-wrapping
	var floatY float64
	if floatType != css.FloatNone && position == css.PositionStatic {
		oldX, oldY := box.X, box.Y
		floatTotalWidth := le.getTotalWidth(box)

		// Phase 5 Enhancement: Check if float fits, apply drop if needed
		// margin.Top was already applied to y at line 276 (y += margin.Top) and is
		// included in box.Y, so don't add it again here
		floatY = le.getFloatDropY(floatType, floatTotalWidth, box.Y, availableWidth)
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

		// Shift children by the position delta
		dx, dy := box.X-oldX, box.Y-oldY
		if dx != 0 || dy != 0 {
			le.shiftChildren(box, dx, dy)
		}
	}

	// Restore BFC float context - remove floats added inside this BFC
	if createsBFC {
		le.floats = le.floats[:le.floatBase]
		le.floatBase = le.floatBaseStack[len(le.floatBaseStack)-1]
		le.floatBaseStack = le.floatBaseStack[:len(le.floatBaseStack)-1]
	}

	// CSS Counter support: Pop counter scopes that were reset on this element
	if counterResets != nil {
		for name := range counterResets {
			le.counterPop(name)
		}
	}

	// Add to float tracking (after BFC pop so float is in parent context)
	if floatType != css.FloatNone && position == css.PositionStatic {
		le.addFloat(box, floatType, floatY)
	}

	// After all positioning is done, fix float:right children that were
	// positioned before the parent width was finalized (shrink-to-fit containers)
	if !hasExplicitWidth && box.Width > 0 {
		le.repositionFloatRightChildren(box)
	}

	// DEBUG: Check final box.Y before returning
	if node != nil && node.TagName == "div" && node.Attributes != nil {
		if id, ok := node.Attributes["id"]; ok && id == "div3" {
			fmt.Printf("DEBUG LAYOUT END: div#%s returning with Y=%.1f\n", id, box.Y)
		}
		if bgColor, ok := style.Get("background-color"); ok && bgColor == "red" {
			fmt.Printf("DEBUG LAYOUT END: red div returning with Y=%.1f\n", box.Y)
		}
	}


	return box
}


// findPositionedAncestorBox walks up the Box parent chain to find the nearest
// ancestor with position != static. Returns nil if none found (viewport).
func findPositionedAncestorBox(box *Box) *Box {
	current := box
	for current != nil {
		if current.Position != css.PositionStatic {
			return current
		}
		current = current.Parent
	}
	return nil
}

// LayoutChildren for BlockLayoutMode - to be implemented as refactor progresses

// ComputeIntrinsicSizes for InlineLayoutMode
func (m *InlineLayoutMode) ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	return le.ComputeIntrinsicSizes(node, style, computedStyles)
}

// LayoutChildren for InlineLayoutMode - to be implemented as refactor progresses
func (m *InlineLayoutMode) LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box {
	// This will be filled in as we refactor layoutNode
	return nil
}

// ComputeIntrinsicSizes for FlexLayoutMode
func (m *FlexLayoutMode) ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	// Flex intrinsic sizing follows CSS Flexible Box Layout Module Level 1 §9.9
	// For now, delegate to block computation
	return le.ComputeIntrinsicSizes(node, style, computedStyles)
}

// LayoutChildren for FlexLayoutMode - to be implemented
func (m *FlexLayoutMode) LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box {
	// This will implement the full flex layout algorithm
	return nil
}

// ============================================================================
