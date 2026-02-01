package layout

// Phase 4: Absolute positioning logic

// applyAbsolutePositioning positions an absolutely positioned box
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

	// Apply positioning
	// Priority: top/left over bottom/right

	if offset.HasLeft {
		box.X = cbX + offset.Left
	} else if offset.HasRight {
		box.X = cbX + cbWidth - offset.Right - box.Width
	} else {
		box.X = cbX
	}

	if offset.HasTop {
		box.Y = cbY + offset.Top
	} else if offset.HasBottom {
		box.Y = cbY + cbHeight - offset.Bottom - box.Height
	} else {
		box.Y = cbY
	}
}
