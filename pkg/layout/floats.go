package layout

import (
	"louis14/pkg/css"
)

// bfcChildTooWideForFloats returns true if a new block formatting context element
// cannot fit beside the current floats and should be pushed below them.
// Per CSS 2.1 §9.5: a BFC element whose border-box would overlap a float's margin-box
// must be repositioned — either by narrowing (width:auto) or by pushing below floats.
// This function returns true (push below) only when the element has an explicit width
// that exceeds the remaining space, or when remaining space is zero or negative.
// Elements with width:auto are left to be narrowed instead (return false).
func bfcChildTooWideForFloats(style *css.Style, remainingWidth, totalWidth float64) bool {
	if remainingWidth <= 0 {
		return true
	}
	// Explicit length width: push below if it doesn't fit
	if w, ok := style.GetLength("width"); ok {
		padding := style.GetPadding()
		border := style.GetBorderWidth()
		requiredWidth := w + padding.Left + padding.Right + border.Left + border.Right
		return requiredWidth > remainingWidth
	}
	// width:auto — narrow beside floats (don't push below)
	return false
}

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

	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		// Check if this float affects the current Y position
		floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)
		if y >= floatInfo.Y && y < floatBottom {
			// box.Width is border-box (content + padding + borders), so margin-box = margins + box.Width
			b := floatInfo.Box
			if floatInfo.Side == css.FloatLeft {
				floatWidth := b.Margin.Left + b.Width + b.Margin.Right
				// Sum left float widths: left floats stack left-to-right
				leftOffset += floatWidth
			} else if floatInfo.Side == css.FloatRight {
				floatWidth := b.Margin.Left + b.Width + b.Margin.Right
				// Sum right float widths: right floats stack right-to-left
				rightOffset += floatWidth
			}
		}
	}

	return leftOffset, rightOffset
}

// getLeftFloatAbsoluteEdge returns the absolute X coordinate of the right edge of all active
// left floats at the given Y position. Returns 0 if there are no active left floats.
// Unlike getFloatOffsets (which returns float widths relative to content-left), this returns
// the absolute position, correctly handling floats inside nested bordered containers.
func (le *LayoutEngine) getLeftFloatAbsoluteEdge(y float64) float64 {
	rightEdge := 0.0
	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)
		if y >= floatInfo.Y && y < floatBottom && floatInfo.Side == css.FloatLeft {
			b := floatInfo.Box
			// Right margin edge of left float = border-box right + margin-right
			edge := b.X + b.Width + b.Margin.Right
			if edge > rightEdge {
				rightEdge = edge
			}
		}
	}
	return rightEdge
}

// initializeLineX returns the starting X position for inline content in a box at the given Y position,
// accounting for left floats. This should be called when starting a new line or after the Y position changes.
func (le *LayoutEngine) initializeLineX(box *Box, border, padding css.BoxEdge, y float64) float64 {
	contentLeft := box.X + border.Left + padding.Left
	leftEdge := le.getLeftFloatAbsoluteEdge(y)
	if leftEdge > contentLeft {
		return leftEdge
	}
	return contentLeft
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
		// b.Height is border-box (includes padding+border), so only add margin.
		floatBottom := floatInfo.Y + b.Height + b.Margin.Bottom

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
// CSS 2.1 §9.5.1: Floats must be placed as high as possible (Rule 6). A float only needs
// to drop when it conflicts with opposite-side floats. Same-side floats stack horizontally
// and can extend past the container edge.
func (le *LayoutEngine) getFloatDropY(floatType css.FloatType, floatWidth float64, startY float64, availableWidth float64) float64 {
	// If available width is 0 (shrink-to-fit parent), skip drop logic
	if availableWidth <= 0 {
		return startY
	}
	currentY := startY
	maxAttempts := 100 // Prevent infinite loops

	for attempt := 0; attempt < maxAttempts; attempt++ {
		leftOffset, rightOffset := le.getFloatOffsets(currentY)

		// CSS 2.1 §9.5.1: A float is placed as high as possible. If there isn't
		// enough horizontal room, it's shifted downward until it fits or clears
		// all existing floats (same-side or opposite-side).
		if floatWidth <= availableWidth-leftOffset-rightOffset {
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

