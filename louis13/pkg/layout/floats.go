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
	padding := style.GetPadding()
	border := style.GetBorderWidth()
	pbWidth := padding.Left + padding.Right + border.Left + border.Right
	// Explicit length width: push below if it doesn't fit
	if w, ok := style.GetLength("width"); ok {
		return w+pbWidth > remainingWidth
	}
	// Percentage width: resolve against containing block and check fit
	if pct, ok := style.GetPercentage("width"); ok && totalWidth > 0 {
		w := totalWidth * pct / 100
		return w+pbWidth > remainingWidth
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

// positionFloatDir positions a float in the multi-pass inline layout pipeline,
// accounting for writing direction.
//
// For horizontal (dir=DirHorizontal): identical to positionFloat.
// For vertical (dir=DirVerticalRL/LR):
//   - lineBlockPos is the X position (block axis) of the current line
//   - float:left goes to inline-start (top), float:right goes to inline-end (bottom)
//   - Fragment Position.X = block position, Position.Y = inline position
//   - Exclusion Rect uses block axis for X/Width and inline axis for Y/Height
func (le *LayoutEngine) positionFloatDir(
	item *InlineItem,
	lineBlockPos float64,
	constraint *ConstraintSpace,
	dir Dir,
) (*Fragment, *ConstraintSpace) {
	if !dir.IsVertical() {
		return le.positionFloat(item, lineBlockPos, constraint)
	}

	floatType := item.Style.GetFloat()
	floatMargin := item.Style.GetMargin()

	// In vertical mode:
	// - Block dimension uses Left/Right margins (physical X axis)
	// - Inline dimension uses Top/Bottom margins (physical Y axis)
	marginBoxBlockSize := floatMargin.Left + item.Width + floatMargin.Right   // block = width
	marginBoxInlineSize := floatMargin.Top + item.Height + floatMargin.Bottom // inline = height

	// Calculate float inline position based on type
	var floatInlinePos float64

	if floatType == css.FloatLeft {
		// float:left = inline-start = top
		startOffset, _ := constraint.ExclusionSpace.AvailableInlineSizeDir(lineBlockPos, marginBoxBlockSize, dir)
		floatInlinePos = startOffset
	} else if floatType == css.FloatRight {
		// float:right = inline-end = bottom
		_, endOffset := constraint.ExclusionSpace.AvailableInlineSizeDir(lineBlockPos, marginBoxBlockSize, dir)
		floatInlinePos = constraint.AvailableSize.Height - endOffset - marginBoxInlineSize
	}

	// Create fragment — position uses physical coordinates
	// In vertical mode: X = block position, Y = inline position
	frag := &Fragment{
		Type:     FragmentFloat,
		Node:     item.Node,
		Style:    item.Style,
		Position: Position{X: lineBlockPos, Y: floatInlinePos},
		Size:     Size{Width: item.Width, Height: item.Height},
	}

	// Create exclusion using block/inline dimensions mapped to Rect
	// For vertical: Rect.X = block position, Rect.Width = block size,
	//               Rect.Y = inline position, Rect.Height = inline size
	exclusion := Exclusion{
		Rect: Rect{
			X:      lineBlockPos,
			Y:      floatInlinePos,
			Width:  marginBoxBlockSize,
			Height: marginBoxInlineSize,
		},
		Side: floatType,
	}

	newConstraint := constraint.WithExclusion(exclusion)
	return frag, newConstraint
}

// ============================================================================
// Dir-parameterized float functions for vertical writing modes
// ============================================================================
//
// In horizontal-tb (default): block axis = Y, inline axis = X
//   float:left = inline-start (left), float:right = inline-end (right)
//   Float exclusions affect Y ranges, clearance is in Y direction
//
// In vertical-rl/lr: block axis = X, inline axis = Y
//   float:left = inline-start (top), float:right = inline-end (bottom)
//   Float exclusions affect X ranges, clearance is in X direction
//
// These functions accept a Dir parameter and swap axes accordingly.
// For DirHorizontal they delegate to the existing functions (zero overhead).
// FloatInfo.Y is reinterpreted as "block-start position" — it stores the
// Y coordinate for horizontal modes and the X coordinate for vertical modes.
// The caller (layout_block.go) is responsible for passing the correct coordinate.

// addFloatDir adds a float with a block-start position.
// For horizontal: blockPos is the Y coordinate.
// For vertical: blockPos is the X coordinate.
// The function stores blockPos in FloatInfo.Y regardless of direction.
func (le *LayoutEngine) addFloatDir(box *Box, side css.FloatType, blockPos float64, dir Dir) {
	le.floats = append(le.floats, FloatInfo{
		Box:  box,
		Side: side,
		Y:    blockPos,
	})
}

// getFloatBlockSize returns the total size of a float box along the block axis.
// For horizontal: total height (margin-top + border-top + padding-top + height + padding-bottom + border-bottom + margin-bottom)
// For vertical: total width (margin-left + border-left + padding-left + width + padding-right + border-right + margin-right)
func (le *LayoutEngine) getFloatBlockSize(box *Box, dir Dir) float64 {
	if dir.IsVertical() {
		return box.Margin.Left + box.Border.Left + box.Padding.Left +
			box.Width +
			box.Padding.Right + box.Border.Right + box.Margin.Right
	}
	return le.getTotalHeight(box)
}

// getFloatInlineSize returns the total margin-box size of a float box along the inline axis.
// For horizontal: margin-left + width + margin-right
// For vertical: margin-top + height + margin-bottom
func (le *LayoutEngine) getFloatInlineSize(box *Box, dir Dir) float64 {
	if dir.IsVertical() {
		return box.Margin.Top + box.Height + box.Margin.Bottom
	}
	return box.Margin.Left + box.Width + box.Margin.Right
}

// getFloatOffsetsDir returns the start and end offsets along the inline axis
// caused by floats at the given block-axis position.
//
// For horizontal (dir=DirHorizontal):
//   blockPos = Y coordinate, returns (leftOffset, rightOffset) — identical to getFloatOffsets
//
// For vertical (dir=DirVerticalRL/LR):
//   blockPos = X coordinate, returns (topOffset, bottomOffset)
//   Float exclusions are checked against X ranges (block axis),
//   and offsets are measured along Y (inline axis).
func (le *LayoutEngine) getFloatOffsetsDir(blockPos float64, dir Dir) (startOffset, endOffset float64) {
	if !dir.IsVertical() {
		return le.getFloatOffsets(blockPos)
	}

	// Vertical mode: block axis is X, inline axis is Y
	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		// FloatInfo.Y stores the block-start position (X coordinate in vertical mode)
		floatBlockEnd := floatInfo.Y + le.getFloatBlockSize(floatInfo.Box, dir)
		if blockPos >= floatInfo.Y && blockPos < floatBlockEnd {
			b := floatInfo.Box
			// Inline size in vertical mode = total height (margin-top + height + margin-bottom)
			floatInlineSize := b.Margin.Top + b.Height + b.Margin.Bottom
			if floatInfo.Side == css.FloatLeft {
				// float:left = inline-start = top offset
				startOffset += floatInlineSize
			} else if floatInfo.Side == css.FloatRight {
				// float:right = inline-end = bottom offset
				endOffset += floatInlineSize
			}
		}
	}

	return startOffset, endOffset
}

// getClearDir returns the block-axis position after clearing floats.
//
// For horizontal (dir=DirHorizontal):
//   Returns Y position after clearing — identical to getClearY.
//
// For vertical (dir=DirVerticalRL/LR):
//   Returns X position after clearing.
//   "clear: left" clears float:left (inline-start / top-side floats),
//   "clear: right" clears float:right (inline-end / bottom-side floats).
//   Clearance moves in the block direction (X axis).
func (le *LayoutEngine) getClearDir(clearType css.ClearType, currentBlockPos float64, dir Dir) float64 {
	if !dir.IsVertical() {
		return le.getClearY(clearType, currentBlockPos)
	}

	if clearType == css.ClearNone {
		return currentBlockPos
	}

	maxBlockPos := currentBlockPos

	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		// In vertical mode, the "bottom" of a float in block direction is the
		// block-start + total width (since block axis = X).
		b := floatInfo.Box
		floatBlockEnd := floatInfo.Y + b.Border.Left + b.Padding.Left + b.Width + b.Padding.Right + b.Border.Right + b.Margin.Right

		shouldClear := false
		switch clearType {
		case css.ClearLeft:
			shouldClear = floatInfo.Side == css.FloatLeft
		case css.ClearRight:
			shouldClear = floatInfo.Side == css.FloatRight
		case css.ClearBoth:
			shouldClear = true
		}

		if shouldClear && floatBlockEnd > maxBlockPos {
			maxBlockPos = floatBlockEnd
		}
	}

	return maxBlockPos
}

// getFloatDropDir finds the block-axis position where a float of the given inline size will fit.
//
// For horizontal (dir=DirHorizontal):
//   Finds Y position — identical to getFloatDropY.
//
// For vertical (dir=DirVerticalRL/LR):
//   Finds X position where the float (measured by its inline size = height) fits
//   within the available inline size (= available height) at that X position.
func (le *LayoutEngine) getFloatDropDir(floatType css.FloatType, floatInlineSize float64, startBlockPos float64, availableInlineSize float64, dir Dir) float64 {
	if !dir.IsVertical() {
		return le.getFloatDropY(floatType, floatInlineSize, startBlockPos, availableInlineSize)
	}

	// Vertical mode: block axis is X, inline axis is Y
	if availableInlineSize <= 0 {
		return startBlockPos
	}
	currentBlockPos := startBlockPos
	maxAttempts := 100

	for attempt := 0; attempt < maxAttempts; attempt++ {
		startOff, endOff := le.getFloatOffsetsDir(currentBlockPos, dir)

		if floatInlineSize <= availableInlineSize-startOff-endOff {
			return currentBlockPos
		}

		// Find the next block position where a float ends
		nextBlockPos := currentBlockPos + 1
		for i := le.floatBase; i < len(le.floats); i++ {
			floatInfo := le.floats[i]
			floatBlockEnd := floatInfo.Y + le.getFloatBlockSize(floatInfo.Box, dir)
			if floatBlockEnd > currentBlockPos && (nextBlockPos == currentBlockPos+1 || floatBlockEnd < nextBlockPos) {
				nextBlockPos = floatBlockEnd
			}
		}

		currentBlockPos = nextBlockPos

		if currentBlockPos > startBlockPos+1000 {
			return startBlockPos
		}
	}

	return currentBlockPos
}

// getStartFloatAbsoluteEdgeDir returns the absolute coordinate of the far edge of all active
// start-side (float:left) floats at the given block-axis position.
//
// For horizontal: returns the absolute X coordinate of the right edge of left floats — same as getLeftFloatAbsoluteEdge.
// For vertical: returns the absolute Y coordinate of the bottom edge of top floats (float:left = inline-start = top).
func (le *LayoutEngine) getStartFloatAbsoluteEdgeDir(blockPos float64, dir Dir) float64 {
	if !dir.IsVertical() {
		return le.getLeftFloatAbsoluteEdge(blockPos)
	}

	// Vertical mode: float:left = inline-start = top
	// We want the absolute Y coordinate of the bottom edge of these floats
	bottomEdge := 0.0
	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		floatBlockEnd := floatInfo.Y + le.getFloatBlockSize(floatInfo.Box, dir)
		if blockPos >= floatInfo.Y && blockPos < floatBlockEnd && floatInfo.Side == css.FloatLeft {
			b := floatInfo.Box
			// Bottom margin edge of top-side float = Y + Height + margin-bottom
			edge := b.Y + b.Height + b.Margin.Bottom
			if edge > bottomEdge {
				bottomEdge = edge
			}
		}
	}
	return bottomEdge
}

// initializeLineDir returns the starting inline position for content in a box at the given
// block-axis position, accounting for start-side (float:left) floats.
//
// For horizontal: returns starting X position — same as initializeLineX.
// For vertical: returns starting Y position, accounting for top-side floats.
func (le *LayoutEngine) initializeLineDir(box *Box, border, padding css.BoxEdge, blockPos float64, dir Dir) float64 {
	if !dir.IsVertical() {
		return le.initializeLineX(box, border, padding, blockPos)
	}

	// Vertical mode: inline-start = top edge of content area
	contentTop := box.Y + border.Top + padding.Top
	topEdge := le.getStartFloatAbsoluteEdgeDir(blockPos, dir)
	if topEdge > contentTop {
		return topEdge
	}
	return contentTop
}

