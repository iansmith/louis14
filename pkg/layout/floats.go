package layout

import (
	"fmt"
	"louis14/pkg/css"
)

func (le *LayoutEngine) positionFloat(
	item *InlineItem,
	lineY float64,
	constraint *ConstraintSpace,
) (*Fragment, *ConstraintSpace) {
	floatType := item.Style.GetFloat()
	floatMargin := item.Style.GetMargin()

	// CSS 2.1 §9.5: Float exclusions use margin-box dimensions
	marginBoxWidth := floatMargin.Left + item.Width + floatMargin.Right
	marginBoxHeight := floatMargin.Top + item.Height + floatMargin.Bottom

	// Calculate float position based on type
	var floatX float64

	if floatType == css.FloatLeft {
		// Left float: position after existing left floats
		leftOffset, _ := constraint.ExclusionSpace.AvailableInlineSize(lineY, marginBoxHeight)
		floatX = leftOffset
		fmt.Printf("    [positionFloat] Left float: %s, leftOffset=%.1f, floatX=%.1f, width=%.1f (marginBox=%.1f)\n",
			getNodeName(item.Node), leftOffset, floatX, item.Width, marginBoxWidth)
	} else if floatType == css.FloatRight {
		// Right float: position before existing right floats
		_, rightOffset := constraint.ExclusionSpace.AvailableInlineSize(lineY, marginBoxHeight)
		floatX = constraint.AvailableSize.Width - rightOffset - marginBoxWidth
	}

	// Create fragment — position is margin-box position, size is border-box (for layoutNode)
	frag := &Fragment{
		Type:     FragmentFloat,
		Node:     item.Node,
		Style:    item.Style,
		Position: Position{X: floatX, Y: lineY},
		Size:     Size{Width: item.Width, Height: item.Height},
	}

	// Create exclusion using margin-box dimensions
	exclusion := Exclusion{
		Rect: Rect{
			X:      floatX,
			Y:      lineY,
			Width:  marginBoxWidth,
			Height: marginBoxHeight,
		},
		Side: floatType,
	}

	// Return fragment and NEW constraint with float added
	newConstraint := constraint.WithExclusion(exclusion)
	return frag, newConstraint
}

// ConstructFragments creates positioned fragments from line breaking results.
// This is Phase 3 of the multi-pass inline layout pipeline.
//
// For each line:
// 1. Call constructLine to create fragments
// 2. Propagate constraint updates (floats) to next line
// 3. Accumulate all fragments
//
// Returns:
// - fragments: All positioned fragments (flattened from all lines)
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

	if y > 25 && y < 35 && len(le.floats) > le.floatBase {
		fmt.Printf("DEBUG: getFloatOffsets(y=%.1f) checking %d floats (base=%d)\n", y, len(le.floats)-le.floatBase, le.floatBase)
	}
	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		// Check if this float affects the current Y position
		floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)
		if y > 25 && y < 35 {
			fmt.Printf("DEBUG:   Float #%d at Y=%.1f-%.1f, side=%v, width=%.1f\n", i, floatInfo.Y, floatBottom, floatInfo.Side, le.getTotalWidth(floatInfo.Box))
		}
		if y >= floatInfo.Y && y < floatBottom {
			if floatInfo.Side == css.FloatLeft {
				floatWidth := le.getTotalWidth(floatInfo.Box)
				if floatWidth > leftOffset {
					leftOffset = floatWidth
				}
			} else if floatInfo.Side == css.FloatRight {
				floatWidth := le.getTotalWidth(floatInfo.Box)
				if floatWidth > rightOffset {
			if y > 100 && y < 150 {
				fmt.Printf("DEBUG: Float #%d at Y=%.1f-%.1f, side=%v, width=%.1f affects y=%.1f\n", i, floatInfo.Y, floatBottom, floatInfo.Side, le.getTotalWidth(floatInfo.Box), y)
			}
					rightOffset = floatWidth
				}
			}
		}
	}

	return leftOffset, rightOffset
}

// initializeLineX returns the starting X position for inline content in a box at the given Y position,
// accounting for left floats. This should be called when starting a new line or after the Y position changes.
func (le *LayoutEngine) initializeLineX(box *Box, border, padding css.BoxEdge, y float64) float64 {
	leftOffset, _ := le.getFloatOffsets(y)
	return box.X + border.Left + padding.Left + leftOffset
}

// ensureLineXClearsFloats updates the inline context's LineX to ensure it clears any left floats
// at the current Y position. This should be called after advancing LineX to verify constraints.
func (le *LayoutEngine) ensureLineXClearsFloats(inlineCtx *InlineContext, box *Box, border, padding css.BoxEdge) {
	minX := le.initializeLineX(box, border, padding, inlineCtx.LineY)
	if inlineCtx.LineX < minX {
		inlineCtx.LineX = minX
	}
}

// getClearY returns the Y position after clearing floats
func (le *LayoutEngine) getClearY(clearType css.ClearType, currentY float64) float64 {
	if clearType == css.ClearNone {
		return currentY
	}

	maxY := currentY

	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		b := floatInfo.Box
		// CSS 2.1 §9.5.2: clearance uses the float's "bottom outer edge" (margin edge),
		// which includes margin-bottom even when negative.
		floatBottom := floatInfo.Y + b.Border.Top + b.Padding.Top + b.Height + b.Padding.Bottom + b.Border.Bottom + b.Margin.Bottom

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
	// If available width is 0 (shrink-to-fit parent), skip drop logic
	if availableWidth <= 0 {
		return startY
	}
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

