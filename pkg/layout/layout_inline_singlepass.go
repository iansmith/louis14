package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

func (le *LayoutEngine) layoutInlineChildren(
	node *html.Node,
	box *Box,
	display css.DisplayType,
	style *css.Style,
	border, padding css.BoxEdge,
	x, childY float64,
	childAvailableWidth float64,
	contentWidth float64,
	isObjectImage bool,
	computedStyles map[*html.Node]*css.Style,
	prevBlockChild **Box,
	pendingMargins *[]float64,
	algorithm InlineLayoutAlgorithm,
) *InlineLayoutResult {
	// Initialize inline context
	inlineCtx := &InlineContext{
		LineX:      le.initializeLineX(box, border, padding, childY),
		LineY:      childY,
		LineHeight: 0,
		LineBoxes:  make([]*Box, 0),
	}

	// Result container
	result := &InlineLayoutResult{
		ChildBoxes:     make([]*Box, 0),
		FinalInlineCtx: inlineCtx,
		UsedMultiPass:  false,
	}

	// Decide which algorithm to actually use
	effectiveAlgorithm := algorithm

	if effectiveAlgorithm == InlineLayoutSinglePass {
		// Use the current single-pass algorithm
		result.ChildBoxes = le.layoutInlineChildrenSinglePass(
			node, box, display, style, border, padding, x, childY, childAvailableWidth,
			contentWidth, isObjectImage, computedStyles, inlineCtx, prevBlockChild, pendingMargins,
		)
		result.FinalInlineCtx = inlineCtx
		result.UsedMultiPass = false
	} else {
		// Use multi-pass algorithm (Blink LayoutNG-style)
		// This uses the existing LayoutInlineBatch infrastructure
		childBoxes := le.LayoutInlineBatch(
			node.Children, box, childAvailableWidth, childY, border, padding, computedStyles,
		)
		result.ChildBoxes = childBoxes

		// Update inline context to end of batched content
		if len(childBoxes) > 0 {
			lastBox := childBoxes[len(childBoxes)-1]
			inlineCtx.LineX = lastBox.X + le.getTotalWidth(lastBox)
			inlineCtx.LineY = lastBox.Y
			if lastBox.Height > inlineCtx.LineHeight {
				inlineCtx.LineHeight = lastBox.Height
			}
			// Populate LineBoxes so parent height calculation works (line 2875 condition)
			inlineCtx.LineBoxes = childBoxes
		}
		result.FinalInlineCtx = inlineCtx
		result.UsedMultiPass = true
	}

	return result
}

// layoutInlineChildrenSinglePass implements the original single-pass inline layout algorithm.
// This is extracted from layoutNode to enable testing and to provide a clean baseline
// before adding multi-pass support.
//
// This function contains the exact code from layoutNode for:
// - ::before generation
// - Child loop (elements and text nodes)
// - ::after generation
// - Block-in-inline fragment finalization
// - text-align application
//
// Additional parameters needed from layoutNode:
// - style: The container's computed style (for line-height, white-space, text-align)
// - x: The container's X position (for list markers)
// - prevBlockChild: Previous block sibling for margin collapsing (modified in-place)
// - pendingMargins: Pending margins from collapse-through elements (modified in-place)
// - contentWidth: Available content width for text-align
func (le *LayoutEngine) layoutInlineChildrenSinglePass(
	node *html.Node,
	box *Box,
	display css.DisplayType,
	style *css.Style,
	border, padding css.BoxEdge,
	x, childY float64,
	childAvailableWidth float64,
	contentWidth float64,
	isObjectImage bool,
	computedStyles map[*html.Node]*css.Style,
	inlineCtx *InlineContext,
	prevBlockChild **Box,
	pendingMargins *[]float64,
) []*Box {
	childBoxes := make([]*Box, 0)

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
			childBoxes = append(childBoxes, beforeBox)
		} else {
			childBoxes = append(childBoxes, beforeBox)
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
			childBoxes = append(childBoxes, markerBox)
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

	// Local copy of childY for tracking vertical position within this function
	localChildY := childY

	for _, child := range node.Children {
		if skipChildren {
			break
		}
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

					childBoxes = append(childBoxes, childBox)

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
						localChildY = inlineCtx.LineY + inlineCtx.LineHeight
						inlineCtx.LineBoxes = make([]*Box, 0)
						inlineCtx.LineHeight = 0
					} else {
						localChildY = inlineCtx.LineY
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
						newY := localChildY + childBox.Margin.Top + relativeOffsetY

						// Shift children by the position delta (important for block-in-inline)
						dx := newX - childBox.X
						dy := newY - childBox.Y
						if dx != 0 || dy != 0 {
							le.shiftChildren(childBox, dx, dy)
						}
						childBox.X = newX
						childBox.Y = newY
					}

					childBoxes = append(childBoxes, childBox)

					// Advance Y for block elements
					childFloatType := childBox.Style.GetFloat()
					if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && childFloatType == css.FloatNone {
						// Margin-collapse-through: collect margins from collapse-through elements
						// and combine them with the next non-collapse-through sibling's margins.
						if isCollapseThrough(childBox) {
							// Add this element's margins (and children's) to pending list
							*pendingMargins = append(*pendingMargins, childBox.Margin.Top, childBox.Margin.Bottom)
							collectCollapseThroughChildMargins(childBox, pendingMargins)
							// Position at localChildY (zero-height, no visual impact)
							childBox.Y = localChildY
							// Don't advance localChildY, don't set prevBlockChild
						} else {
							// Normal margin collapsing between adjacent block siblings
							if *prevBlockChild != nil && shouldCollapseMargins(*prevBlockChild) && shouldCollapseMargins(childBox) {
								// Collect all margins: prev bottom, any pending from collapse-through, current top
								allMargins := []float64{(*prevBlockChild).Margin.Bottom}
								allMargins = append(allMargins, *pendingMargins...)
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
								totalUsed := (*prevBlockChild).Margin.Bottom + childBox.Margin.Top
								adjustment := totalUsed - collapsed
								childBox.Y -= adjustment
								le.adjustChildrenY(childBox, -adjustment)
							} else if len(*pendingMargins) > 0 && shouldCollapseMargins(childBox) {
								// No prev sibling but pending margins from collapse-through
								allMargins := append(*pendingMargins, childBox.Margin.Top)
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
							*pendingMargins = nil
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
							localChildY = childBox.Y + childBox.Border.Top + childBox.Padding.Top + childBox.Height + childBox.Padding.Bottom + childBox.Border.Bottom + childBox.Margin.Bottom
							*prevBlockChild = childBox
						}
					}

					// Reset inline context for next line
					inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
					inlineCtx.LineY = localChildY

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
				childBoxes = append(childBoxes, textBox)

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
		childBoxes = append(childBoxes, afterBox)
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
			// CRITICAL FIX: Apply text-align to childBoxes (which will be added to box.Children later)
			// NOT to box.Children directly (which is still empty at this point)
			le.applyTextAlignToBoxes(childBoxes, box, textAlign, contentWidth)
		}
	}

	return childBoxes
}

// ============================================================================
// Multi-pass Inline Layout (Blink-style three-phase approach)
// ============================================================================

// LayoutInlineContent is the main entry point for multi-pass inline layout.
// LayoutInlineBatch processes a specific batch of inline children (not all children of node).
// This is used during layoutNode to process consecutive inline/text children in one pass.
func (le *LayoutEngine) LayoutInlineBatch(
	children []*html.Node,
	box *Box,
	availableWidth float64,
	startY float64,
	border, padding css.BoxEdge,
	computedStyles map[*html.Node]*css.Style,
) []*Box {
	const maxRetries = 3

	// Calculate float base index ONCE before retry loop
	// This ensures we reset to the same point on each retry
	floatBaseIndex := len(le.floats)

	for attempt := 0; attempt < maxRetries; attempt++ {
		// DO NOT reset floats - they must persist between retries so line breaking can account for them
		// Each retry needs to see the floats added in previous attempts to converge

		// Create state for this batch
		state := &InlineLayoutState{
			Items:          []*InlineItem{},
			Lines:          []*LineBreakResult{},
			ContainerBox:   box,
			ContainerStyle: box.Style,
			AvailableWidth: availableWidth,
			StartY:         startY,
			Border:         border,
			Padding:        padding,
			FloatList:      le.floats,
			FloatBaseIndex: floatBaseIndex,
		}

		// Phase 1: Collect items from the batch of children
		for _, child := range children {
			le.CollectInlineItems(child, state, computedStyles)
		}

		// If no items collected, return empty
		if len(state.Items) == 0 {
			return []*Box{}
		}

		// Phase 2: Break lines
		success := le.breakLinesWIP(state)
		if !success {
			return []*Box{}
		}

		// Phase 3: Construct boxes with retry detection
		boxes, retryNeeded := le.constructLineBoxesWithRetry(state, box, computedStyles)

		if !retryNeeded {
			return boxes
		}

		// Retry: floats added during construction are kept so line breaking can account for them
		// Don't reset le.floats - we want the next iteration to see the floats we just added
	}

	// Max retries exceeded - do final construction with full support
	state := &InlineLayoutState{
		Items:          []*InlineItem{},
		Lines:          []*LineBreakResult{},
		ContainerBox:   box,
		ContainerStyle: box.Style,
		AvailableWidth: availableWidth,
		StartY:         startY,
		Border:         border,
		Padding:        padding,
		FloatList:      le.floats,
		FloatBaseIndex: len(le.floats),
	}
	for _, child := range children {
		le.CollectInlineItems(child, state, computedStyles)
	}
	le.breakLinesWIP(state)
	// Use full construction method that handles block children and floats
	boxes, _ := le.constructLineBoxesWithRetry(state, box, computedStyles)

	// Apply text-align to inline children
	if box.Style != nil {
		display := box.Style.GetDisplay()
		if display != css.DisplayInline && display != css.DisplayInlineBlock {
			if textAlign, ok := box.Style.Get("text-align"); ok && textAlign != "left" && textAlign != "" {
				contentWidth := box.Width // box.Width is already the content width
				le.applyTextAlignToBoxes(boxes, box, textAlign, contentWidth)
			}
		}
	}

	return boxes
}

// It orchestrates all three phases and returns the resulting boxes.
//
// NOTE: This is the OLD WIP implementation. New code should use LayoutInlineContent() instead.
