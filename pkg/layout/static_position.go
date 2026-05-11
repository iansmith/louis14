package layout

// StaticPositionEdge identifies which edge of the alignment container
// a static position refers to. Blink calls these InlineEdge and BlockEdge.
//
// For block-level OOF children, the static position is at the start edge
// by default. For inline-level OOF children found inside line boxes, the
// position may refer to different edges depending on alignment.
type StaticPositionEdge int

const (
	StaticEdgeStart  StaticPositionEdge = iota // default for block-level OOF
	StaticEdgeCenter                           // for align-self: center
	StaticEdgeEnd                              // for align-self: end / flex-end
)

// LogicalStaticPosition is the static position of an out-of-flow element,
// expressed in the containing block's logical coordinate system.
//
// Unlike a plain LogicalOffset, this carries edge annotations that indicate
// which edge of the alignment container the offset refers to. This matters
// when resolving auto insets: the offset is measured from the indicated edge.
//
// Mirrors Blink's LogicalStaticPosition (static_position.h).
type LogicalStaticPosition struct {
	Offset     LogicalOffset
	InlineEdge StaticPositionEdge
	BlockEdge  StaticPositionEdge
}

// PhysicalStaticPosition is the physical-coordinate form of a static position.
// Used as an intermediate during cross-writing-mode conversion.
//
// Mirrors Blink's PhysicalStaticPosition.
type PhysicalStaticPosition struct {
	Offset         PhysicalOffset
	HorizontalEdge PhysicalStaticEdge
	VerticalEdge   PhysicalStaticEdge
}

// PhysicalStaticEdge identifies which physical edge a static position refers to.
type PhysicalStaticEdge int

const (
	PhysicalEdgeLeft PhysicalStaticEdge = iota
	PhysicalEdgeRight
	PhysicalEdgeTop
	PhysicalEdgeBottom
	PhysicalEdgeCenter // for centered alignment
)

// ConvertToPhysical converts a logical static position to physical coordinates,
// given the writing direction and the physical size of the container's content box.
//
// Mirrors Blink's LogicalStaticPosition::ConvertToPhysical().
func (lsp LogicalStaticPosition) ConvertToPhysical(wdm WritingDirectionMode, containerSize PhysicalSize) PhysicalStaticPosition {
	// Convert the offset using the standard converter.
	// The "inner size" for static position is zero — the position is a point,
	// not a box. Blink uses the same approach.
	conv := NewConverter(wdm, containerSize)
	physOffset := conv.ToPhysicalOffset(lsp.Offset, PhysicalSize{})

	// Convert edge annotations from logical to physical.
	var hEdge PhysicalStaticEdge
	var vEdge PhysicalStaticEdge

	switch wdm.WM {
	case WritingModeHorizontalTB:
		// Inline = horizontal, Block = vertical
		hEdge = logicalInlineToPhysicalH(lsp.InlineEdge, wdm)
		vEdge = logicalBlockToPhysicalV(lsp.BlockEdge, wdm)

	case WritingModeVerticalRL, WritingModeSidewaysRL:
		// Inline = vertical (top-to-bottom for LTR), Block = horizontal (right-to-left)
		vEdge = logicalInlineToPhysicalV(lsp.InlineEdge, wdm)
		hEdge = logicalBlockToPhysicalH(lsp.BlockEdge, wdm)

	case WritingModeVerticalLR:
		// Inline = vertical (top-to-bottom for LTR), Block = horizontal (left-to-right)
		vEdge = logicalInlineToPhysicalV(lsp.InlineEdge, wdm)
		hEdge = logicalBlockToPhysicalH(lsp.BlockEdge, wdm)

	case WritingModeSidewaysLR:
		// Inline = vertical (bottom-to-top for LTR), Block = horizontal (left-to-right)
		vEdge = logicalInlineToPhysicalVSidewaysLR(lsp.InlineEdge, wdm)
		hEdge = logicalBlockToPhysicalH(lsp.BlockEdge, wdm)
	}

	return PhysicalStaticPosition{
		Offset:         physOffset,
		HorizontalEdge: hEdge,
		VerticalEdge:   vEdge,
	}
}

// ConvertToLogical converts a physical static position to logical coordinates
// in the given writing direction, using the container's physical content size.
//
// Mirrors Blink's PhysicalStaticPosition::ConvertToLogical().
func (psp PhysicalStaticPosition) ConvertToLogical(wdm WritingDirectionMode, containerSize PhysicalSize) LogicalStaticPosition {
	conv := NewConverter(wdm, containerSize)
	logOffset := conv.ToLogicalOffset(psp.Offset, PhysicalSize{})

	var inlineEdge StaticPositionEdge
	var blockEdge StaticPositionEdge

	switch wdm.WM {
	case WritingModeHorizontalTB:
		inlineEdge = physicalHToLogicalInline(psp.HorizontalEdge, wdm)
		blockEdge = physicalVToLogicalBlock(psp.VerticalEdge, wdm)

	case WritingModeVerticalRL, WritingModeSidewaysRL:
		inlineEdge = physicalVToLogicalInline(psp.VerticalEdge, wdm)
		blockEdge = physicalHToLogicalBlock(psp.HorizontalEdge, wdm)

	case WritingModeVerticalLR:
		inlineEdge = physicalVToLogicalInline(psp.VerticalEdge, wdm)
		blockEdge = physicalHToLogicalBlock(psp.HorizontalEdge, wdm)

	case WritingModeSidewaysLR:
		inlineEdge = physicalVToLogicalInlineSidewaysLR(psp.VerticalEdge, wdm)
		blockEdge = physicalHToLogicalBlock(psp.HorizontalEdge, wdm)
	}

	return LogicalStaticPosition{
		Offset:     logOffset,
		InlineEdge: inlineEdge,
		BlockEdge:  blockEdge,
	}
}

// --- Edge conversion helpers ---

// logicalInlineToPhysicalH: inline edge → horizontal physical edge for HTB.
func logicalInlineToPhysicalH(e StaticPositionEdge, wdm WritingDirectionMode) PhysicalStaticEdge {
	if e == StaticEdgeCenter {
		return PhysicalEdgeCenter
	}
	if wdm.IsLTR() {
		if e == StaticEdgeStart {
			return PhysicalEdgeLeft
		}
		return PhysicalEdgeRight
	}
	// RTL
	if e == StaticEdgeStart {
		return PhysicalEdgeRight
	}
	return PhysicalEdgeLeft
}

// logicalBlockToPhysicalV: block edge → vertical physical edge for HTB.
func logicalBlockToPhysicalV(e StaticPositionEdge, _ WritingDirectionMode) PhysicalStaticEdge {
	switch e {
	case StaticEdgeStart:
		return PhysicalEdgeTop
	case StaticEdgeEnd:
		return PhysicalEdgeBottom
	}
	return PhysicalEdgeCenter
}

// logicalInlineToPhysicalV: inline edge → vertical physical edge for vertical modes.
func logicalInlineToPhysicalV(e StaticPositionEdge, wdm WritingDirectionMode) PhysicalStaticEdge {
	if e == StaticEdgeCenter {
		return PhysicalEdgeCenter
	}
	if wdm.IsLTR() {
		if e == StaticEdgeStart {
			return PhysicalEdgeTop
		}
		return PhysicalEdgeBottom
	}
	// RTL
	if e == StaticEdgeStart {
		return PhysicalEdgeBottom
	}
	return PhysicalEdgeTop
}

// logicalBlockToPhysicalH: block edge → horizontal physical edge for vertical modes.
func logicalBlockToPhysicalH(e StaticPositionEdge, wdm WritingDirectionMode) PhysicalStaticEdge {
	if e == StaticEdgeCenter {
		return PhysicalEdgeCenter
	}
	if wdm.IsFlippedBlocks() {
		// VRL/SRL: block-start = right
		if e == StaticEdgeStart {
			return PhysicalEdgeRight
		}
		return PhysicalEdgeLeft
	}
	// VLR/SLR: block-start = left
	if e == StaticEdgeStart {
		return PhysicalEdgeLeft
	}
	return PhysicalEdgeRight
}

// logicalInlineToPhysicalVSidewaysLR: inline edge → vertical for sideways-lr.
// Sideways-lr has inline direction bottom-to-top for LTR.
func logicalInlineToPhysicalVSidewaysLR(e StaticPositionEdge, wdm WritingDirectionMode) PhysicalStaticEdge {
	if e == StaticEdgeCenter {
		return PhysicalEdgeCenter
	}
	if wdm.IsLTR() {
		if e == StaticEdgeStart {
			return PhysicalEdgeBottom
		}
		return PhysicalEdgeTop
	}
	// RTL
	if e == StaticEdgeStart {
		return PhysicalEdgeTop
	}
	return PhysicalEdgeBottom
}

// physicalHToLogicalInline: horizontal physical edge → inline edge for HTB.
func physicalHToLogicalInline(e PhysicalStaticEdge, wdm WritingDirectionMode) StaticPositionEdge {
	if e == PhysicalEdgeCenter {
		return StaticEdgeCenter
	}
	if wdm.IsLTR() {
		if e == PhysicalEdgeLeft {
			return StaticEdgeStart
		}
		return StaticEdgeEnd
	}
	if e == PhysicalEdgeRight {
		return StaticEdgeStart
	}
	return StaticEdgeEnd
}

// physicalVToLogicalBlock: vertical physical edge → block edge for HTB.
func physicalVToLogicalBlock(e PhysicalStaticEdge, _ WritingDirectionMode) StaticPositionEdge {
	switch e {
	case PhysicalEdgeTop:
		return StaticEdgeStart
	case PhysicalEdgeBottom:
		return StaticEdgeEnd
	}
	return StaticEdgeCenter
}

// physicalVToLogicalInline: vertical physical edge → inline edge for vertical modes.
func physicalVToLogicalInline(e PhysicalStaticEdge, wdm WritingDirectionMode) StaticPositionEdge {
	if e == PhysicalEdgeCenter {
		return StaticEdgeCenter
	}
	if wdm.IsLTR() {
		if e == PhysicalEdgeTop {
			return StaticEdgeStart
		}
		return StaticEdgeEnd
	}
	if e == PhysicalEdgeBottom {
		return StaticEdgeStart
	}
	return StaticEdgeEnd
}

// physicalHToLogicalBlock: horizontal physical edge → block edge for vertical modes.
func physicalHToLogicalBlock(e PhysicalStaticEdge, wdm WritingDirectionMode) StaticPositionEdge {
	if e == PhysicalEdgeCenter {
		return StaticEdgeCenter
	}
	if wdm.IsFlippedBlocks() {
		if e == PhysicalEdgeRight {
			return StaticEdgeStart
		}
		return StaticEdgeEnd
	}
	if e == PhysicalEdgeLeft {
		return StaticEdgeStart
	}
	return StaticEdgeEnd
}

// physicalVToLogicalInlineSidewaysLR: vertical physical edge → inline for sideways-lr.
func physicalVToLogicalInlineSidewaysLR(e PhysicalStaticEdge, wdm WritingDirectionMode) StaticPositionEdge {
	if e == PhysicalEdgeCenter {
		return StaticEdgeCenter
	}
	if wdm.IsLTR() {
		if e == PhysicalEdgeBottom {
			return StaticEdgeStart
		}
		return StaticEdgeEnd
	}
	if e == PhysicalEdgeTop {
		return StaticEdgeStart
	}
	return StaticEdgeEnd
}
