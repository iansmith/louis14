package layout

// Phase 4: Absolute positioning logic

// applyAbsolutePositioning positions an absolutely positioned box
// following CSS 2.1 §10.3.7 (horizontal) and §10.6.4 (vertical)
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
		cbWidth = containingBlock.Width + containingBlock.Padding.Left + containingBlock.Padding.Right
		cbHeight = containingBlock.Height + containingBlock.Padding.Top + containingBlock.Padding.Bottom
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

	// CSS 2.1 §10.3.7: Horizontal positioning for absolutely positioned elements
	// When left, right, and width are all non-auto, and both margins are auto,
	// the margins should be equal (centering the element horizontally)
	if offset.HasLeft && offset.HasRight && marginLeftAuto && marginRightAuto {
		// Calculate available space for margins
		usedWidth := box.Border.Left + box.Padding.Left + box.Width +
			box.Padding.Right + box.Border.Right
		availableForMargins := cbWidth - offset.Left - offset.Right - usedWidth

		if availableForMargins >= 0 {
			// Center horizontally
			box.Margin.Left = availableForMargins / 2
			box.Margin.Right = availableForMargins / 2
		} else {
			// Over-constrained: set margins to 0
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
		box.X = cbX + box.Margin.Left
	}

	// CSS 2.1 §10.6.4: Vertical positioning for absolutely positioned elements
	// When top, bottom, and height are all non-auto, and both margins are auto,
	// the margins should be equal (centering the element vertically)
	if offset.HasTop && offset.HasBottom && marginTopAuto && marginBottomAuto {
		// Calculate available space for margins
		usedHeight := box.Border.Top + box.Padding.Top + box.Height +
			box.Padding.Bottom + box.Border.Bottom
		availableForMargins := cbHeight - offset.Top - offset.Bottom - usedHeight

		if availableForMargins >= 0 {
			// Center vertically
			box.Margin.Top = availableForMargins / 2
			box.Margin.Bottom = availableForMargins / 2
		} else {
			// Over-constrained: set margins to 0
			box.Margin.Top = 0
			box.Margin.Bottom = 0
		}
		box.Y = cbY + offset.Top + box.Margin.Top
	} else if offset.HasTop {
		box.Y = cbY + offset.Top + box.Margin.Top
	} else if offset.HasBottom {
		box.Y = cbY + cbHeight - offset.Bottom - box.Margin.Bottom - box.Height -
			box.Padding.Top - box.Padding.Bottom - box.Border.Top - box.Border.Bottom
	} else {
		box.Y = cbY + box.Margin.Top
	}
}
