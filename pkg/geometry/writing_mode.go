package geometry

// WritingMode mirrors CSS Writing Modes 3 writing-mode values. Numeric
// ordering matches Blink's WritingMode enum and louis14's
// pkg/layout.WritingMode so a future migration of pkg/layout is mechanical.
type WritingMode uint8

const (
	WritingModeHorizontalTB WritingMode = iota // default: inline=LR, block=TB
	WritingModeVerticalRL                      // inline=TB, block=RL
	WritingModeVerticalLR                      // inline=TB, block=LR
	WritingModeSidewaysRL                      // inline=TB, block=RL (same shape as VRL for layout)
	WritingModeSidewaysLR                      // inline=BT, block=LR
)

// Direction mirrors CSS direction values.
type Direction uint8

const (
	DirectionLTR Direction = iota
	DirectionRTL
)

// WritingDirectionMode pairs a writing-mode with a direction. The
// combination fully determines the logical-to-physical coordinate mapping.
// Mirrors Blink's WritingDirectionMode (platform/text/writing_direction_mode.h).
type WritingDirectionMode struct {
	WM  WritingMode
	Dir Direction
}

// IsHorizontal reports whether block progression is vertical (htb).
func (w WritingDirectionMode) IsHorizontal() bool {
	return w.WM == WritingModeHorizontalTB
}

// IsVertical reports whether block progression is horizontal.
func (w WritingDirectionMode) IsVertical() bool { return !w.IsHorizontal() }

// IsFlippedBlocks reports whether block progression goes right-to-left
// (vertical-rl / sideways-rl).
func (w WritingDirectionMode) IsFlippedBlocks() bool {
	return w.WM == WritingModeVerticalRL || w.WM == WritingModeSidewaysRL
}

// IsLTR reports whether direction is LTR.
func (w WritingDirectionMode) IsLTR() bool { return w.Dir == DirectionLTR }

// IsRTL reports whether direction is RTL.
func (w WritingDirectionMode) IsRTL() bool { return w.Dir == DirectionRTL }
