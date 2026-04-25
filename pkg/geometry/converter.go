package geometry

// WritingModeConverter converts offsets between logical and physical
// coordinate systems. Mirrors Blink's WritingModeConverter
// (core/layout/geometry/writing_mode_converter.{h,cc}).
//
// To convert an offset, the caller needs:
//   - WDM (writing-mode + direction)
//   - OuterSize (the container's physical size)
//   - InnerSize (the box's physical size)
//
// The InnerSize is needed because physical offsets are top-left-anchored,
// but flipped writing modes (vertical-rl, RTL) anchor the logical origin
// at a different physical corner. The formula `outer - offset - inner`
// accounts for this.
//
// Size conversions don't need outer/inner sizes — vertical modes just
// swap (width, height) ↔ (block, inline).
type WritingModeConverter struct {
	WDM       WritingDirectionMode
	OuterSize PhysicalSize
}

// NewWritingModeConverter creates a converter for a given WDM + container
// physical size.
func NewWritingModeConverter(wdm WritingDirectionMode, outer PhysicalSize) WritingModeConverter {
	return WritingModeConverter{WDM: wdm, OuterSize: outer}
}

// --- Size conversions (direction-independent) ---

// LogicalSizeFromPhysical converts a physical size to logical, swapping
// (width, height) for vertical writing modes.
func LogicalSizeFromPhysical(size PhysicalSize, wm WritingMode) LogicalSize {
	if wm == WritingModeHorizontalTB {
		return LogicalSize{InlineSize: size.Width, BlockSize: size.Height}
	}
	return LogicalSize{InlineSize: size.Height, BlockSize: size.Width}
}

// PhysicalSizeFromLogical is the inverse of LogicalSizeFromPhysical.
func PhysicalSizeFromLogical(size LogicalSize, wm WritingMode) PhysicalSize {
	if wm == WritingModeHorizontalTB {
		return PhysicalSize{Width: size.InlineSize, Height: size.BlockSize}
	}
	return PhysicalSize{Width: size.BlockSize, Height: size.InlineSize}
}

// --- Offset conversions ---

// ToPhysicalOffset converts a logical offset to physical. Mirrors Blink's
// WritingModeConverter::SlowToPhysical (writing_mode_converter.cc:30-65).
//
// inner is the physical size of the box being placed.
func (c WritingModeConverter) ToPhysicalOffset(offset LogicalOffset, inner PhysicalSize) PhysicalOffset {
	// Fast path: horizontal-tb + LTR is identity.
	if c.WDM.WM == WritingModeHorizontalTB && c.WDM.Dir == DirectionLTR {
		return PhysicalOffset{Left: offset.InlineOffset, Top: offset.BlockOffset}
	}
	return c.toPhysicalOffsetSlow(offset, inner)
}

func (c WritingModeConverter) toPhysicalOffsetSlow(offset LogicalOffset, inner PhysicalSize) PhysicalOffset {
	outerW := c.OuterSize.Width
	outerH := c.OuterSize.Height
	innerW := inner.Width
	innerH := inner.Height
	inline := offset.InlineOffset
	block := offset.BlockOffset

	switch c.WDM.WM {
	case WritingModeHorizontalTB:
		// RTL.
		return PhysicalOffset{
			Left: outerW.Sub(inline).Sub(innerW),
			Top:  block,
		}

	case WritingModeVerticalRL, WritingModeSidewaysRL:
		if c.WDM.Dir == DirectionLTR {
			return PhysicalOffset{
				Left: outerW.Sub(block).Sub(innerW),
				Top:  inline,
			}
		}
		// RTL.
		return PhysicalOffset{
			Left: outerW.Sub(block).Sub(innerW),
			Top:  outerH.Sub(inline).Sub(innerH),
		}

	case WritingModeVerticalLR:
		if c.WDM.Dir == DirectionLTR {
			return PhysicalOffset{
				Left: block,
				Top:  inline,
			}
		}
		// RTL.
		return PhysicalOffset{
			Left: block,
			Top:  outerH.Sub(inline).Sub(innerH),
		}

	case WritingModeSidewaysLR:
		if c.WDM.Dir == DirectionLTR {
			return PhysicalOffset{
				Left: block,
				Top:  outerH.Sub(inline).Sub(innerH),
			}
		}
		// RTL.
		return PhysicalOffset{
			Left: block,
			Top:  inline,
		}
	}
	return PhysicalOffset{Left: offset.InlineOffset, Top: offset.BlockOffset}
}

// ToLogicalOffset converts a physical offset to logical. Inverse of
// ToPhysicalOffset; mirrors Blink's WritingModeConverter::SlowToLogical.
func (c WritingModeConverter) ToLogicalOffset(offset PhysicalOffset, inner PhysicalSize) LogicalOffset {
	if c.WDM.WM == WritingModeHorizontalTB && c.WDM.Dir == DirectionLTR {
		return LogicalOffset{InlineOffset: offset.Left, BlockOffset: offset.Top}
	}
	return c.toLogicalOffsetSlow(offset, inner)
}

func (c WritingModeConverter) toLogicalOffsetSlow(offset PhysicalOffset, inner PhysicalSize) LogicalOffset {
	outerW := c.OuterSize.Width
	outerH := c.OuterSize.Height
	innerW := inner.Width
	innerH := inner.Height
	x := offset.Left
	y := offset.Top

	switch c.WDM.WM {
	case WritingModeHorizontalTB:
		// RTL.
		return LogicalOffset{
			InlineOffset: outerW.Sub(x).Sub(innerW),
			BlockOffset:  y,
		}

	case WritingModeVerticalRL, WritingModeSidewaysRL:
		if c.WDM.Dir == DirectionLTR {
			return LogicalOffset{
				InlineOffset: y,
				BlockOffset:  outerW.Sub(x).Sub(innerW),
			}
		}
		// RTL.
		return LogicalOffset{
			InlineOffset: outerH.Sub(y).Sub(innerH),
			BlockOffset:  outerW.Sub(x).Sub(innerW),
		}

	case WritingModeVerticalLR:
		if c.WDM.Dir == DirectionLTR {
			return LogicalOffset{
				InlineOffset: y,
				BlockOffset:  x,
			}
		}
		// RTL.
		return LogicalOffset{
			InlineOffset: outerH.Sub(y).Sub(innerH),
			BlockOffset:  x,
		}

	case WritingModeSidewaysLR:
		if c.WDM.Dir == DirectionLTR {
			return LogicalOffset{
				InlineOffset: outerH.Sub(y).Sub(innerH),
				BlockOffset:  x,
			}
		}
		// RTL.
		return LogicalOffset{
			InlineOffset: y,
			BlockOffset:  x,
		}
	}
	return LogicalOffset{InlineOffset: offset.Left, BlockOffset: offset.Top}
}

// --- Rect conversions ---

// ToPhysicalRect converts a logical rect to physical. Uses the rect's own
// size as the inner size for the offset conversion.
func (c WritingModeConverter) ToPhysicalRect(r LogicalRect) PhysicalRect {
	innerSize := PhysicalSizeFromLogical(r.Size, c.WDM.WM)
	return PhysicalRect{
		Offset: c.ToPhysicalOffset(r.Offset, innerSize),
		Size:   innerSize,
	}
}

// ToLogicalRect converts a physical rect to logical.
func (c WritingModeConverter) ToLogicalRect(r PhysicalRect) LogicalRect {
	return LogicalRect{
		Offset: c.ToLogicalOffset(r.Offset, r.Size),
		Size:   LogicalSizeFromPhysical(r.Size, c.WDM.WM),
	}
}
