package layout

// WritingModeConverter converts between logical and physical coordinate systems.
//
// Ported from Blink's WritingModeConverter (writing_mode_converter.h/.cc).
//
// To convert an offset, you need:
//   - The writing direction mode (writing-mode + direction)
//   - The outer size (the container's physical size)
//   - The inner size (the child's physical size)
//
// The inner size is needed because physical offsets reference the top-left
// corner, but in flipped modes (RTL, vertical-rl) the logical origin is at
// a different corner. The formula outer - offset - inner accounts for this.
type WritingModeConverter struct {
	WDM       WritingDirectionMode
	OuterSize PhysicalSize
}

// NewConverter creates a WritingModeConverter for a given writing direction
// and outer (container) physical size.
func NewConverter(wdm WritingDirectionMode, outerSize PhysicalSize) WritingModeConverter {
	return WritingModeConverter{WDM: wdm, OuterSize: outerSize}
}

// --- Size conversions (direction-independent) ---

// ToLogicalSize converts a physical size to logical.
// All vertical modes swap width/height; horizontal is identity.
func ToLogicalSize(size PhysicalSize, wm WritingMode) LogicalSize {
	if wm == WritingModeHorizontalTB {
		return LogicalSize{InlineSize: size.Width, BlockSize: size.Height}
	}
	return LogicalSize{InlineSize: size.Height, BlockSize: size.Width}
}

// ToPhysicalSize converts a logical size to physical.
func ToPhysicalSize(size LogicalSize, wm WritingMode) PhysicalSize {
	if wm == WritingModeHorizontalTB {
		return PhysicalSize{Width: size.InlineSize, Height: size.BlockSize}
	}
	return PhysicalSize{Width: size.BlockSize, Height: size.InlineSize}
}

// --- Offset conversions (require outer size + inner size) ---

// ToLogicalOffset converts a physical offset to logical coordinates.
//
// innerSize is the physical size of the child whose offset is being converted.
// The converter's OuterSize is the physical size of the container.
func (c WritingModeConverter) ToLogicalOffset(offset PhysicalOffset, innerSize PhysicalSize) LogicalOffset {
	// Fast path: horizontal-tb + LTR is identity.
	if c.WDM.WM == WritingModeHorizontalTB && c.WDM.Dir == DirectionLTR {
		return LogicalOffset{InlineOffset: offset.X, BlockOffset: offset.Y}
	}
	return c.toLogicalOffsetSlow(offset, innerSize)
}

func (c WritingModeConverter) toLogicalOffsetSlow(offset PhysicalOffset, innerSize PhysicalSize) LogicalOffset {
	outerW := c.OuterSize.Width
	outerH := c.OuterSize.Height
	innerW := innerSize.Width
	innerH := innerSize.Height
	x := offset.X
	y := offset.Y

	switch c.WDM.WM {
	case WritingModeHorizontalTB:
		// RTL (LTR handled by fast path)
		return LogicalOffset{
			InlineOffset: outerW - x - innerW,
			BlockOffset:  y,
		}

	case WritingModeVerticalRL, WritingModeSidewaysRL:
		if c.WDM.Dir == DirectionLTR {
			return LogicalOffset{
				InlineOffset: y,
				BlockOffset:  outerW - x - innerW,
			}
		}
		// RTL
		return LogicalOffset{
			InlineOffset: outerH - y - innerH,
			BlockOffset:  outerW - x - innerW,
		}

	case WritingModeVerticalLR:
		if c.WDM.Dir == DirectionLTR {
			return LogicalOffset{
				InlineOffset: y,
				BlockOffset:  x,
			}
		}
		// RTL
		return LogicalOffset{
			InlineOffset: outerH - y - innerH,
			BlockOffset:  x,
		}

	case WritingModeSidewaysLR:
		if c.WDM.Dir == DirectionLTR {
			return LogicalOffset{
				InlineOffset: outerH - y - innerH,
				BlockOffset:  x,
			}
		}
		// RTL
		return LogicalOffset{
			InlineOffset: y,
			BlockOffset:  x,
		}
	}
	return LogicalOffset{InlineOffset: offset.X, BlockOffset: offset.Y}
}

// ToPhysicalOffset converts a logical offset to physical coordinates.
//
// innerSize is the physical size of the child whose offset is being converted.
func (c WritingModeConverter) ToPhysicalOffset(offset LogicalOffset, innerSize PhysicalSize) PhysicalOffset {
	// Fast path: horizontal-tb + LTR is identity.
	if c.WDM.WM == WritingModeHorizontalTB && c.WDM.Dir == DirectionLTR {
		return PhysicalOffset{X: offset.InlineOffset, Y: offset.BlockOffset}
	}
	return c.toPhysicalOffsetSlow(offset, innerSize)
}

func (c WritingModeConverter) toPhysicalOffsetSlow(offset LogicalOffset, innerSize PhysicalSize) PhysicalOffset {
	outerW := c.OuterSize.Width
	outerH := c.OuterSize.Height
	innerW := innerSize.Width
	innerH := innerSize.Height
	inline := offset.InlineOffset
	block := offset.BlockOffset

	switch c.WDM.WM {
	case WritingModeHorizontalTB:
		// RTL
		return PhysicalOffset{
			X: outerW - inline - innerW,
			Y: block,
		}

	case WritingModeVerticalRL, WritingModeSidewaysRL:
		if c.WDM.Dir == DirectionLTR {
			return PhysicalOffset{
				X: outerW - block - innerW,
				Y: inline,
			}
		}
		// RTL
		return PhysicalOffset{
			X: outerW - block - innerW,
			Y: outerH - inline - innerH,
		}

	case WritingModeVerticalLR:
		if c.WDM.Dir == DirectionLTR {
			return PhysicalOffset{
				X: block,
				Y: inline,
			}
		}
		// RTL
		return PhysicalOffset{
			X: block,
			Y: outerH - inline - innerH,
		}

	case WritingModeSidewaysLR:
		if c.WDM.Dir == DirectionLTR {
			return PhysicalOffset{
				X: block,
				Y: outerH - inline - innerH,
			}
		}
		// RTL
		return PhysicalOffset{
			X: block,
			Y: inline,
		}
	}
	return PhysicalOffset{X: offset.InlineOffset, Y: offset.BlockOffset}
}

// --- Edge conversions (margins, borders, padding) ---

// ToLogicalEdges converts physical edges (top/right/bottom/left) to logical
// edges (inline-start/inline-end/block-start/block-end).
func ToLogicalEdges(edges PhysicalEdges, wdm WritingDirectionMode) LogicalEdges {
	var result LogicalEdges

	// First map by writing mode (assuming LTR).
	switch wdm.WM {
	case WritingModeHorizontalTB:
		result = LogicalEdges{
			InlineStart: edges.Left,
			InlineEnd:   edges.Right,
			BlockStart:  edges.Top,
			BlockEnd:    edges.Bottom,
		}
	case WritingModeVerticalRL, WritingModeSidewaysRL:
		result = LogicalEdges{
			InlineStart: edges.Top,
			InlineEnd:   edges.Bottom,
			BlockStart:  edges.Right,
			BlockEnd:    edges.Left,
		}
	case WritingModeVerticalLR:
		result = LogicalEdges{
			InlineStart: edges.Top,
			InlineEnd:   edges.Bottom,
			BlockStart:  edges.Left,
			BlockEnd:    edges.Right,
		}
	case WritingModeSidewaysLR:
		result = LogicalEdges{
			InlineStart: edges.Bottom,
			InlineEnd:   edges.Top,
			BlockStart:  edges.Left,
			BlockEnd:    edges.Right,
		}
	}

	// If RTL, swap inline-start and inline-end.
	if wdm.Dir == DirectionRTL {
		result.InlineStart, result.InlineEnd = result.InlineEnd, result.InlineStart
	}

	return result
}

// ToPhysicalEdges converts logical edges to physical edges.
func ToPhysicalEdges(edges LogicalEdges, wdm WritingDirectionMode) PhysicalEdges {
	// Apply direction: resolve inline-start/end to direction-start/end.
	dirStart := edges.InlineStart
	dirEnd := edges.InlineEnd
	if wdm.Dir == DirectionRTL {
		dirStart, dirEnd = dirEnd, dirStart
	}

	switch wdm.WM {
	case WritingModeHorizontalTB:
		return PhysicalEdges{
			Top:    edges.BlockStart,
			Right:  dirEnd,
			Bottom: edges.BlockEnd,
			Left:   dirStart,
		}
	case WritingModeVerticalRL, WritingModeSidewaysRL:
		return PhysicalEdges{
			Top:    dirStart,
			Right:  edges.BlockStart,
			Bottom: dirEnd,
			Left:   edges.BlockEnd,
		}
	case WritingModeVerticalLR:
		return PhysicalEdges{
			Top:    dirStart,
			Right:  edges.BlockEnd,
			Bottom: dirEnd,
			Left:   edges.BlockStart,
		}
	case WritingModeSidewaysLR:
		return PhysicalEdges{
			Top:    dirEnd,
			Right:  edges.BlockEnd,
			Bottom: dirStart,
			Left:   edges.BlockStart,
		}
	}
	return PhysicalEdges{}
}
