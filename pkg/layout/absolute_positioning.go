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

// repositionAbsPosAfterVerticalTransform corrects the position of an absolutely
// positioned child after transformToVerticalRL has run on the containing block.
// The transform repositions all children (including abspos) as if they were in-flow,
// but abspos children should be positioned using the CSS constraint equations in
// physical coordinates. This function re-applies the correct physical positioning.
//
// cbPadWidth/cbPadHeight are the CB's pre-transform padding-box dimensions,
// which is what CSS uses for the constraint equations.
func repositionAbsPosAfterVerticalTransform(child *Box, cb *Box, cbPadWidth, cbPadHeight float64) {
	if child.Style == nil {
		return
	}

	cbX := cb.X + cb.Border.Left
	cbY := cb.Y + cb.Border.Top

	offset := child.Style.GetPositionOffset()

	// Resolve percentage offsets against CB padding-box dimensions
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

	// Detect auto dimensions
	widthIsAuto := true
	heightIsAuto := true
	if _, ok := child.Style.GetLength("width"); ok {
		widthIsAuto = false
	} else if _, ok := child.Style.GetPercentage("width"); ok {
		widthIsAuto = false
	} else if v, ok := child.Style.Get("width"); ok && v != "auto" && v != "" {
		widthIsAuto = false
	}
	if _, ok := child.Style.GetLength("height"); ok {
		heightIsAuto = false
	} else if _, ok := child.Style.GetPercentage("height"); ok {
		heightIsAuto = false
	} else if v, ok := child.Style.Get("height"); ok && v != "auto" && v != "" {
		heightIsAuto = false
	}

	// Solve for width (§10.3.7/§10.6.4 Case 5 in physical coords)
	if offset.HasLeft && offset.HasRight && widthIsAuto {
		solved := cbPadWidth - offset.Left - offset.Right -
			child.Margin.Left - child.Margin.Right -
			child.Border.Left - child.Padding.Left -
			child.Padding.Right - child.Border.Right
		if solved >= 0 {
			child.Width = solved
		}
	}

	// Solve for height (§10.3.7/§10.6.4 Case 5 in physical coords)
	if offset.HasTop && offset.HasBottom && heightIsAuto {
		solved := cbPadHeight - offset.Top - offset.Bottom -
			child.Margin.Top - child.Margin.Bottom -
			child.Border.Top - child.Padding.Top -
			child.Padding.Bottom - child.Border.Bottom
		if solved >= 0 {
			child.Height = solved
		}
	}

	oldX, oldY := child.X, child.Y

	// Horizontal positioning
	if offset.HasLeft && offset.HasRight {
		// Both specified: position from left
		child.X = cbX + offset.Left + child.Margin.Left
	} else if offset.HasLeft {
		child.X = cbX + offset.Left + child.Margin.Left
	} else if offset.HasRight {
		usedWidth := child.Border.Left + child.Padding.Left + child.Width +
			child.Padding.Right + child.Border.Right
		child.X = cbX + cbPadWidth - offset.Right - child.Margin.Right - usedWidth
	}
	// else: auto left/right → keep transform result (approximate static position)

	// Vertical positioning
	if offset.HasTop && offset.HasBottom {
		child.Y = cbY + offset.Top + child.Margin.Top
	} else if offset.HasTop {
		child.Y = cbY + offset.Top + child.Margin.Top
	} else if offset.HasBottom {
		usedHeight := child.Border.Top + child.Padding.Top + child.Height +
			child.Padding.Bottom + child.Border.Bottom
		child.Y = cbY + cbPadHeight - offset.Bottom - child.Margin.Bottom - usedHeight
	}
	// else: auto top/bottom → keep transform result (approximate static position)

	// Shift children by position delta
	dx, dy := child.X-oldX, child.Y-oldY
	if dx != 0 || dy != 0 {
		shiftAllDescendants(child, dx, dy)
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
