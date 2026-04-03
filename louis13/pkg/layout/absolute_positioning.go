package layout

import "louis14/pkg/css"

// Phase 4: Absolute positioning logic

// isVerticalWM returns true if the writing-mode is vertical
// (vertical-rl, vertical-lr, sideways-rl, sideways-lr).
func isVerticalWM(wm string) bool {
	return wm == "vertical-rl" || wm == "vertical-lr" ||
		wm == "sideways-rl" || wm == "sideways-lr"
}

// getContainingBlockWritingMode returns the writing-mode of the containing block.
func getContainingBlockWritingMode(box *Box) string {
	cb := box.FindContainingBlock()
	if cb != nil && cb.Style != nil {
		if wm, ok := cb.Style.Get("writing-mode"); ok {
			return wm
		}
	}
	return "horizontal-tb"
}

// applyAbsolutePositioning positions an absolutely positioned box
// following CSS 2.1 §10.3.7 (horizontal) and §10.6.4 (vertical)
//
// CSS Writing Modes §7.1: In vertical writing modes, the constraint equations swap:
//   - The §10.3.7 "horizontal" equation applies to the VERTICAL dimension (top/bottom/height)
//   - The §10.6.4 "vertical" equation applies to the HORIZONTAL dimension (left/right/width)
//
// Architecture note: When the containing block has a vertical writing mode, this engine
// first lays out children in horizontal mode, then transformToVerticalRL transforms
// coordinates. The transform maps pre-X → final-Y and pre-Y-group → final-X-column.
// For vertical containing blocks, we compute the desired final positions and encode them
// into the pre-transform coordinates.
func (le *LayoutEngine) applyAbsolutePositioning(box *Box) {
	// Find containing block
	containingBlock := box.FindContainingBlock()

	// Get position offsets
	offset := box.Style.GetPositionOffset()

	// Determine containing block bounds
	var cbX, cbY, cbWidth, cbHeight float64

	if containingBlock == nil {
		// Positioned relative to viewport (initial containing block)
		cbX = 0
		cbY = 0
		cbWidth = le.viewport.width
		cbHeight = le.viewport.height
	} else {
		// Positioned relative to containing block's padding edge
		cbX = containingBlock.X + containingBlock.Border.Left
		cbY = containingBlock.Y + containingBlock.Border.Top
		// FIXME: Box.Width currently includes padding+border, but CSS spec says it should be content-only
		// For now, subtract borders since containingBlock.Width includes them
		cbWidth = containingBlock.Width - containingBlock.Border.Left - containingBlock.Border.Right
		cbHeight = containingBlock.Height - containingBlock.Border.Top - containingBlock.Border.Bottom
	}

	// Resolve percentage offsets against containing block dimensions
	// GetPositionOffset only returns absolute lengths, so we need to check for percentages separately
	if box.Style != nil {
		if !offset.HasLeft {
			if pct, ok := box.Style.GetPercentage("left"); ok {
				offset.Left = cbWidth * (pct / 100.0)
				offset.HasLeft = true
			}
		}
		if !offset.HasRight {
			if pct, ok := box.Style.GetPercentage("right"); ok {
				offset.Right = cbWidth * (pct / 100.0)
				offset.HasRight = true
			}
		}
		if !offset.HasTop {
			if pct, ok := box.Style.GetPercentage("top"); ok {
				offset.Top = cbHeight * (pct / 100.0)
				offset.HasTop = true
			}
		}
		if !offset.HasBottom {
			if pct, ok := box.Style.GetPercentage("bottom"); ok {
				offset.Bottom = cbHeight * (pct / 100.0)
				offset.HasBottom = true
			}
		}
	}

	// Check if margins are auto
	marginTopAuto := false
	marginBottomAuto := false
	marginLeftAuto := false
	marginRightAuto := false

	if box.Style != nil {
		if mt, ok := box.Style.Get("margin-top"); ok && mt == "auto" {
			marginTopAuto = true
		}
		if mb, ok := box.Style.Get("margin-bottom"); ok && mb == "auto" {
			marginBottomAuto = true
		}
		if ml, ok := box.Style.Get("margin-left"); ok && ml == "auto" {
			marginLeftAuto = true
		}
		if mr, ok := box.Style.Get("margin-right"); ok && mr == "auto" {
			marginRightAuto = true
		}
	}

	// Detect vertical writing mode on the containing block.
	// The vertical override only applies when transformToVerticalRL will run
	// (block/inline-block containers). Flex and grid containers handle vertical
	// writing modes natively, so skip the vertical abs-pos override for them.
	cbWM := getContainingBlockWritingMode(box)
	isVertical := isVerticalWM(cbWM)
	if isVertical {
		cb := box.FindContainingBlock()
		if cb != nil && cb.Style != nil {
			cbDisplay := cb.Style.GetDisplay()
			if cbDisplay == css.DisplayFlex || cbDisplay == css.DisplayInlineFlex ||
				cbDisplay == css.DisplayGrid || cbDisplay == css.DisplayInlineGrid {
				isVertical = false
			}
		}
	}

	// Detect if width/height are auto (not explicitly specified in CSS).
	// Needed for §10.3.7 Case 5 and §10.6.4 Case 5 dimension solving.
	widthIsAuto := true
	heightIsAuto := true
	if box.Style != nil {
		if _, ok := box.Style.GetLength("width"); ok {
			widthIsAuto = false
		} else if _, ok := box.Style.GetPercentage("width"); ok {
			widthIsAuto = false
		} else if v, ok := box.Style.Get("width"); ok && v != "auto" && v != "" {
			widthIsAuto = false
		}
		if _, ok := box.Style.GetLength("height"); ok {
			heightIsAuto = false
		} else if _, ok := box.Style.GetPercentage("height"); ok {
			heightIsAuto = false
		} else if v, ok := box.Style.Get("height"); ok && v != "auto" && v != "" {
			heightIsAuto = false
		}
	}

	// §10.3.7 Case 5: Solve for width when left+right specified and width is auto.
	// Per spec, auto margins are set to 0 before solving.
	if offset.HasLeft && offset.HasRight && widthIsAuto {
		if marginLeftAuto {
			box.Margin.Left = 0
		}
		if marginRightAuto {
			box.Margin.Right = 0
		}
		solvedWidth := cbWidth - offset.Left - offset.Right -
			box.Margin.Left - box.Margin.Right -
			box.Border.Left - box.Padding.Left -
			box.Padding.Right - box.Border.Right
		if solvedWidth >= 0 {
			box.Width = solvedWidth
		}
	}

	// §10.6.4 Case 5: Solve for height when top+bottom specified and height is auto.
	// Only in horizontal-tb mode; vertical WM height solving is in the vertical section below.
	if !isVertical && offset.HasTop && offset.HasBottom && heightIsAuto {
		if marginTopAuto {
			box.Margin.Top = 0
		}
		if marginBottomAuto {
			box.Margin.Bottom = 0
		}
		solvedHeight := cbHeight - offset.Top - offset.Bottom -
			box.Margin.Top - box.Margin.Bottom -
			box.Border.Top - box.Padding.Top -
			box.Padding.Bottom - box.Border.Bottom
		if solvedHeight >= 0 {
			box.Height = solvedHeight
		}
	}

	// CSS 2.1 §10.3.7: Horizontal positioning for absolutely positioned elements
	// Always run this — in horizontal-tb it's the primary horizontal equation;
	// in vertical modes it provides a baseline box.X that may be overridden below.
	if offset.HasLeft && offset.HasRight && marginLeftAuto && marginRightAuto && !widthIsAuto {
		usedWidth := box.Border.Left + box.Padding.Left + box.Width +
			box.Padding.Right + box.Border.Right
		availableForMargins := cbWidth - offset.Left - offset.Right - usedWidth

		if availableForMargins >= 0 {
			box.Margin.Left = availableForMargins / 2
			box.Margin.Right = availableForMargins / 2
		} else {
			box.Margin.Left = 0
			box.Margin.Right = 0
		}
		box.X = cbX + offset.Left + box.Margin.Left
	} else if offset.HasLeft {
		box.X = cbX + offset.Left + box.Margin.Left
	} else if offset.HasRight {
		box.X = cbX + cbWidth - offset.Right - box.Margin.Right - box.Width -
			box.Padding.Left - box.Padding.Right - box.Border.Left - box.Border.Right
	} else {
		// CSS 2.1 §10.3.7: When left and right are auto, use the static position.
		// In LTR, box.X already reflects the static position (set during normal flow).
		// In RTL, spec §10.3.7 says use the static position for 'right' and solve for 'left'.
		//
		// The inline layout engine sets box.X = staticX = prevSibling.X + prevSibling.Width,
		// which is the right edge of the element's hypothetical normal-flow box in RTL
		// (since the element would appear to the LEFT of the preceding RTL content).
		//
		// rightStatic = distance from CB right edge to the hypothetical right edge of the element:
		//   rightStatic = (cbX + cbWidth) - (box.X - outerWidth)
		//               = (cbX + cbWidth) - box.X + outerWidth
		//
		// Solving for left:
		//   box.X = cbX + cbWidth - rightStatic - outerWidth
		//         = cbX + cbWidth - ((cbX + cbWidth) - box.X + outerWidth) - outerWidth
		//         = box.X - 2 * outerWidth
		if !isVertical && containingBlock != nil {
			cbDir := css.DirectionLTR
			if containingBlock.Style != nil {
				cbDir = containingBlock.Style.GetDirection()
			}
			if cbDir == css.DirectionRTL {
				outerWidth := box.Margin.Left + box.Border.Left + box.Padding.Left +
					box.Width + box.Padding.Right + box.Border.Right + box.Margin.Right
				box.X = box.X - 2*outerWidth
			}
		}
	}

	// CSS 2.1 §10.6.4: Vertical positioning for absolutely positioned elements
	// In vertical writing modes, top/bottom is the inline axis (handled below via box.X override),
	// so we skip the standard §10.6.4 for top/bottom when vertical — setting box.Y from
	// top/bottom would put the element in the wrong column after transformToVerticalRL.
	if !(isVertical && (offset.HasTop || offset.HasBottom)) {
		if offset.HasTop && offset.HasBottom && marginTopAuto && marginBottomAuto && !heightIsAuto {
			usedHeight := box.Border.Top + box.Padding.Top + box.Height +
				box.Padding.Bottom + box.Border.Bottom
			availableForMargins := cbHeight - offset.Top - offset.Bottom - usedHeight

			if availableForMargins >= 0 {
				box.Margin.Top = availableForMargins / 2
				box.Margin.Bottom = availableForMargins / 2
			} else {
				box.Margin.Top = 0
				box.Margin.Bottom = 0
			}
			box.Y = cbY + offset.Top + box.Margin.Top
		} else if offset.HasTop && offset.HasBottom && !marginTopAuto && !marginBottomAuto {
			box.Y = cbY + offset.Top + box.Margin.Top
		} else if offset.HasTop {
			box.Y = cbY + offset.Top + box.Margin.Top
		} else if offset.HasBottom {
			box.Y = cbY + cbHeight - offset.Bottom - box.Margin.Bottom - box.Height -
				box.Padding.Top - box.Padding.Bottom - box.Border.Top - box.Border.Bottom
		} else {
			// CSS 2.1 §10.6.4: When top and bottom are auto, use the static position.
		}
	}

	// CSS Writing Modes §7.1: In vertical writing modes, the inline axis is vertical
	// (top/bottom/height). The transformToVerticalRL function maps pre-transform X → final Y.
	// When top or bottom is explicitly specified, we override box.X to encode the
	// desired inline position into the pre-transform coordinate that becomes final Y.
	//
	// The block axis (left/right/width → final X) is NOT overridden here because
	// transformToVerticalRL determines box.X from Y-group column assignment, overwriting
	// whatever we set. The standard §10.3.7 result for left/right is left in box.X as
	// a fallback (it will be overwritten by the transform if needed).
	if isVertical && (offset.HasTop || offset.HasBottom) {
		// §10.3.7 Case 5 mapped to vertical inline axis: solve for height when auto.
		if offset.HasTop && offset.HasBottom && heightIsAuto {
			if marginTopAuto {
				box.Margin.Top = 0
			}
			if marginBottomAuto {
				box.Margin.Bottom = 0
			}
			solvedHeight := cbHeight - offset.Top - offset.Bottom -
				box.Margin.Top - box.Margin.Bottom -
				box.Border.Top - box.Padding.Top -
				box.Padding.Bottom - box.Border.Bottom
			if solvedHeight >= 0 {
				box.Height = solvedHeight
			}
		}

		usedInlineSize := box.Height + box.Border.Top + box.Padding.Top + box.Padding.Bottom + box.Border.Bottom

		if offset.HasTop && offset.HasBottom && marginTopAuto && marginBottomAuto && !heightIsAuto {
			avail := cbHeight - offset.Top - offset.Bottom - usedInlineSize
			if avail >= 0 {
				box.Margin.Top = avail / 2
				box.Margin.Bottom = avail / 2
			} else {
				box.Margin.Top = 0
				box.Margin.Bottom = 0
			}
			box.X = cbX + offset.Top + box.Margin.Top
		} else if offset.HasTop {
			box.X = cbX + offset.Top + box.Margin.Top
		} else if offset.HasBottom {
			box.X = cbX + cbHeight - offset.Bottom - box.Margin.Bottom - usedInlineSize
		}
	}
}

// repositionAbsPosAfterVerticalTransform re-positions absolutely positioned children
// of a vertical writing-mode container AFTER transformToVerticalRL has converted the
// container's children from horizontal to vertical physical coordinates.
//
// CSS Writing Modes §7.1: In vertical writing modes, the constraint equations
// from CSS 2.1 §10.3.7 and §10.6.4 are swapped:
//   - §10.3.7 ("horizontal" equation) applies to top/bottom/height (inline axis)
//   - §10.6.4 ("vertical" equation) applies to left/right/width (block axis)
//
// Parameters:
//   - box: the containing block with vertical writing mode (post-transform)
//   - cbWM: "vertical-rl" or "vertical-lr"
//   - preTransformWidth, preTransformHeight: the CB's border-box dimensions BEFORE
//     the transform (CSS-specified dimensions)
func repositionAbsPosAfterVerticalTransform(box *Box, cbWM string, preTransformWidth, preTransformHeight float64) {
	// Recursively walk all descendants and reposition abs-pos elements.
	// Each abs-pos element uses its actual containing block for positioning.
	repositionAbsPosRecursive(box, box, cbWM, preTransformWidth, preTransformHeight)
}

// repositionAbsPosInCB repositions absolutely positioned direct children of `cb`
// using the vertical writing mode constraint equations.
// Parameters:
//   - cb: the containing block (position:relative/absolute/fixed ancestor)
//   - cbWM: writing mode of the containing block
//   - cbPadWidth, cbPadHeight: padding-box dimensions of the CB (pre-transform for the
//     outermost transformed box, post-transform for nested CBs with explicit dims)
func repositionAbsPosInCB(cb *Box, cbWM string, cbPadWidth, cbPadHeight float64) {
	cbX := cb.X + cb.Border.Left
	cbY := cb.Y + cb.Border.Top

	// Get direction from the containing block's style
	cbDir := css.DirectionLTR
	if cb.Style != nil {
		cbDir = cb.Style.GetDirection()
	}

	for idx, child := range cb.Children {
		if child.Style == nil {
			continue
		}
		// Use the box's Position field rather than child.Style.GetPosition().
		// Text fragment boxes inherit their parent element's style (which may have
		// position:absolute), but they are not CSS positioned elements — their
		// box.Position is set to PositionStatic during inline layout creation.
		// Using child.Position avoids incorrectly repositioning text fragments.
		if child.Position != css.PositionAbsolute && child.Position != css.PositionFixed {
			continue
		}
		// Skip CSS floats: float boxes have box.Position = PositionAbsolute as an
		// internal rendering marker (set in layout_inline_multipass.go), but they are
		// NOT CSS absolutely positioned elements. transformToVerticalRL already placed
		// them correctly; repositioning them here would override the correct position.
		if child.Style.GetFloat() != css.FloatNone {
			continue
		}

		// Save old position to compute delta for shifting descendants
		oldX, oldY := child.X, child.Y

		// Get the child's CSS position offsets
		offset := child.Style.GetPositionOffset()

		// Resolve percentage offsets against CB dimensions
		if !offset.HasTop {
			if pct, ok := child.Style.GetPercentage("top"); ok {
				offset.Top = cbPadHeight * (pct / 100.0)
				offset.HasTop = true
			}
		}
		if !offset.HasBottom {
			if pct, ok := child.Style.GetPercentage("bottom"); ok {
				offset.Bottom = cbPadHeight * (pct / 100.0)
				offset.HasBottom = true
			}
		}
		if !offset.HasLeft {
			if pct, ok := child.Style.GetPercentage("left"); ok {
				offset.Left = cbPadWidth * (pct / 100.0)
				offset.HasLeft = true
			}
		}
		if !offset.HasRight {
			if pct, ok := child.Style.GetPercentage("right"); ok {
				offset.Right = cbPadWidth * (pct / 100.0)
				offset.HasRight = true
			}
		}

		// Check auto margins
		marginTopAuto := false
		marginBottomAuto := false
		marginLeftAuto := false
		marginRightAuto := false
		if mt, ok := child.Style.Get("margin-top"); ok && mt == "auto" {
			marginTopAuto = true
		}
		if mb, ok := child.Style.Get("margin-bottom"); ok && mb == "auto" {
			marginBottomAuto = true
		}
		if ml, ok := child.Style.Get("margin-left"); ok && ml == "auto" {
			marginLeftAuto = true
		}
		if mr, ok := child.Style.Get("margin-right"); ok && mr == "auto" {
			marginRightAuto = true
		}

		// Compute the static position in the vertical writing mode's coordinate system.
		// The transform placed abs-pos children incorrectly (using horizontal static position).
		// We need to find the correct static position based on the last in-flow sibling.
		staticInline := 0.0 // distance from inline-start (top for ltr, bottom for rtl)
		staticBlock := 0.0  // distance from block-start (left for vlr, right for vrl)

		// Find the last in-flow sibling box before this abs-pos child.
		var prevInFlow *Box
		for j := idx - 1; j >= 0; j-- {
			sib := cb.Children[j]
			if sib.Style != nil {
				sibPos := sib.Style.GetPosition()
				if sibPos == css.PositionAbsolute || sibPos == css.PositionFixed {
					continue
				}
			}
			prevInFlow = sib
			break
		}

		if prevInFlow != nil {
			prevRelY := prevInFlow.Y - cbY

			if cbDir == css.DirectionLTR {
				// LTR: inline flows top-to-bottom. Static position is after the
				// previous sibling's inline extent.
				staticInline = prevRelY + prevInFlow.Width
			} else {
				// RTL: inline flows bottom-to-top.
				staticInline = cbPadHeight - prevRelY
			}

			prevRelX := prevInFlow.X - cbX
			staticBlock = prevRelX

			// For text fragment siblings, use line-height as column width
			if cb.Style != nil && (prevInFlow.Node == nil || prevInFlow.Node.TagName == "") {
				lineHeight := cb.Style.GetLineHeight()
				if lineHeight > 0 {
					columnXPositions := []float64{}
					for _, sib := range cb.Children {
						if sib.Style != nil {
							sp := sib.Style.GetPosition()
							if sp == css.PositionAbsolute || sp == css.PositionFixed {
								continue
							}
						}
						sibRelX := sib.X - cbX
						found := false
						for _, cx := range columnXPositions {
							if cx == sibRelX || (cx-sibRelX < 1 && sibRelX-cx < 1) {
								found = true
								break
							}
						}
						if !found {
							columnXPositions = append(columnXPositions, sibRelX)
						}
					}

					colIdx := 0
					for ci, cx := range columnXPositions {
						if cx == prevRelX || (cx-prevRelX < 1 && prevRelX-cx < 1) {
							colIdx = ci
							break
						}
					}

					if cbWM == "vertical-rl" {
						staticBlock = cbPadWidth - float64(colIdx+1)*lineHeight
					} else {
						staticBlock = float64(colIdx) * lineHeight
					}
				}
			}
		}

		// ========================================================================
		// INLINE AXIS: top/bottom/height (physical Y in vertical modes)
		// Uses §10.3.7 rules with axis swap.
		// ========================================================================
		if offset.HasTop && offset.HasBottom && marginTopAuto && marginBottomAuto {
			usedHeight := child.Border.Top + child.Padding.Top + child.Height +
				child.Padding.Bottom + child.Border.Bottom
			avail := cbPadHeight - offset.Top - offset.Bottom - usedHeight
			if avail >= 0 {
				child.Margin.Top = avail / 2
				child.Margin.Bottom = avail / 2
			} else {
				child.Margin.Top = 0
				child.Margin.Bottom = 0
			}
			child.Y = cbY + offset.Top + child.Margin.Top
		} else if offset.HasTop && offset.HasBottom {
			child.Y = cbY + offset.Top + child.Margin.Top
		} else if offset.HasTop {
			child.Y = cbY + offset.Top + child.Margin.Top
		} else if offset.HasBottom {
			child.Y = cbY + cbPadHeight - offset.Bottom - child.Margin.Bottom -
				child.Border.Bottom - child.Padding.Bottom - child.Height -
				child.Padding.Top - child.Border.Top
		} else {
			// Neither top nor bottom specified → use static position
			if cbDir == css.DirectionLTR {
				child.Y = cbY + staticInline
			} else {
				outerH := child.Margin.Top + child.Border.Top + child.Padding.Top +
					child.Height + child.Padding.Bottom + child.Border.Bottom + child.Margin.Bottom
				child.Y = cbY + cbPadHeight - staticInline - outerH
			}
		}

		// ========================================================================
		// BLOCK AXIS: left/right/width (physical X in vertical modes)
		// Uses §10.6.4 rules with axis swap.
		// ========================================================================
		if offset.HasLeft && offset.HasRight && marginLeftAuto && marginRightAuto {
			usedWidth := child.Border.Left + child.Padding.Left + child.Width +
				child.Padding.Right + child.Border.Right
			avail := cbPadWidth - offset.Left - offset.Right - usedWidth
			if avail >= 0 {
				child.Margin.Left = avail / 2
				child.Margin.Right = avail / 2
			} else {
				child.Margin.Left = 0
				child.Margin.Right = 0
			}
			child.X = cbX + offset.Left + child.Margin.Left
		} else if offset.HasLeft && offset.HasRight {
			// Over-constrained block axis: spec says ignore the "end" side and solve from "start".
			// In vertical-rl: block-start = right, block-end = left → ignore left, use right.
			// In vertical-lr: block-start = left, block-end = right → ignore right, use left.
			if cbWM == "vertical-rl" || cbWM == "sideways-rl" {
				child.X = cbX + cbPadWidth - offset.Right - child.Margin.Right -
					child.Border.Right - child.Padding.Right - child.Width -
					child.Padding.Left - child.Border.Left
			} else {
				child.X = cbX + offset.Left + child.Margin.Left
			}
		} else if offset.HasLeft {
			child.X = cbX + offset.Left + child.Margin.Left
		} else if offset.HasRight {
			child.X = cbX + cbPadWidth - offset.Right - child.Margin.Right -
				child.Border.Right - child.Padding.Right - child.Width -
				child.Padding.Left - child.Border.Left
		} else {
			// Neither left nor right specified → use static block position
			child.X = cbX + staticBlock
		}

		// Shift descendants to match the position change
		dx, dy := child.X-oldX, child.Y-oldY
		if dx != 0 || dy != 0 {
			shiftAllDescendants(child, dx, dy)
		}
	}
}

// repositionAbsPosRecursive walks the tree rooted at `current`, finding abs-pos
// descendants and repositioning them according to their containing block's
// vertical writing-mode constraint equations.
//
// Parameters:
//   - transformRoot: the outermost box that was transformed (for pre-transform dims)
//   - current: the current box being walked
//   - cbWM: writing mode of the containing block context
//   - preTransformWidth, preTransformHeight: border-box dimensions of transformRoot
//     BEFORE the transform was applied
func repositionAbsPosRecursive(current *Box, transformRoot *Box, cbWM string, preTransformWidth, preTransformHeight float64) {
	// Determine if `current` is a containing block for abs-pos elements
	isCB := false
	if current.Style != nil {
		pos := current.Style.GetPosition()
		if pos == css.PositionRelative || pos == css.PositionAbsolute || pos == css.PositionFixed || pos == css.PositionSticky {
			isCB = true
		}
		// Also check for transform, will-change, contain, etc. (simplified)
		display := current.Style.GetDisplay()
		if display == css.DisplayFlex || display == css.DisplayInlineFlex ||
			display == css.DisplayGrid || display == css.DisplayInlineGrid {
			isCB = true
		}
	}

	// If current is the transform root OR a containing block, handle its direct abs-pos children
	if current == transformRoot || isCB {
		// Determine CB padding-box dimensions
		var cbPadWidth, cbPadHeight float64
		if current == transformRoot {
			// Use pre-transform dimensions for the outermost transformed box
			cbPadWidth = preTransformWidth - current.Border.Left - current.Border.Right
			cbPadHeight = preTransformHeight - current.Border.Top - current.Border.Bottom
		} else {
			// For nested containing blocks, use current (post-transform) dimensions.
			// If the CB has explicit CSS width/height, those dimensions are preserved
			// through the transform. Otherwise, use the current box dimensions.
			cbPadWidth = current.Width - current.Border.Left - current.Border.Right
			cbPadHeight = current.Height - current.Border.Top - current.Border.Bottom
		}

		// Determine the writing mode for this CB (may differ from outer)
		localWM := cbWM
		if current != transformRoot && current.Style != nil {
			if wm, ok := current.Style.Get("writing-mode"); ok && isVerticalWM(wm) {
				localWM = wm
			}
		}

		repositionAbsPosInCB(current, localWM, cbPadWidth, cbPadHeight)
	}

	// Recurse into non-abs-pos children to find deeper containing blocks
	for _, child := range current.Children {
		if child.Style != nil {
			childPos := child.Style.GetPosition()
			if childPos == css.PositionAbsolute || childPos == css.PositionFixed {
				// Don't recurse into abs-pos children — they've already been repositioned
				// by their containing block's repositionAbsPosInCB call
				continue
			}
		}
		repositionAbsPosRecursive(child, transformRoot, cbWM, preTransformWidth, preTransformHeight)
	}
}

// shiftAllDescendants recursively shifts all children of a box by dx, dy.
func shiftAllDescendants(box *Box, dx, dy float64) {
	for _, c := range box.Children {
		c.X += dx
		c.Y += dy
		shiftAllDescendants(c, dx, dy)
	}
}
