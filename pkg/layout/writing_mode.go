package layout

import "louis14/pkg/css"

// WritingMode represents CSS writing-mode values.
// These match Blink's WritingMode enum ordering.
type WritingMode uint8

const (
	WritingModeHorizontalTB WritingMode = iota // default: inline=LR, block=TB
	WritingModeVerticalRL                      // inline=TB, block=RL
	WritingModeVerticalLR                      // inline=TB, block=LR
	WritingModeSidewaysRL                      // inline=TB, block=RL (same as VRL for layout)
	WritingModeSidewaysLR                      // inline=BT, block=LR
)

// Direction represents CSS direction values (ltr/rtl).
type Direction uint8

const (
	DirectionLTR Direction = iota
	DirectionRTL
)

// WritingDirectionMode combines WritingMode and Direction into a single
// value that fully determines the logical-to-physical coordinate mapping.
// This is the Go equivalent of Blink's WritingDirectionMode.
type WritingDirectionMode struct {
	WM  WritingMode
	Dir Direction
}

// NewWritingDirectionMode creates a WritingDirectionMode from a CSS style.
func NewWritingDirectionMode(style *css.Style) WritingDirectionMode {
	wdm := WritingDirectionMode{
		WM:  WritingModeHorizontalTB,
		Dir: DirectionLTR,
	}
	if style == nil {
		return wdm
	}
	if wm, ok := style.Get("writing-mode"); ok {
		switch wm {
		case "vertical-rl":
			wdm.WM = WritingModeVerticalRL
		case "vertical-lr":
			wdm.WM = WritingModeVerticalLR
		case "sideways-rl":
			wdm.WM = WritingModeSidewaysRL
		case "sideways-lr":
			wdm.WM = WritingModeSidewaysLR
		}
	}
	if dir, ok := style.Get("direction"); ok && dir == "rtl" {
		wdm.Dir = DirectionRTL
	}
	return wdm
}

// IsHorizontal returns true for horizontal-tb.
func (wdm WritingDirectionMode) IsHorizontal() bool {
	return wdm.WM == WritingModeHorizontalTB
}

// IsVertical returns true for any vertical or sideways writing mode.
func (wdm WritingDirectionMode) IsVertical() bool {
	return !wdm.IsHorizontal()
}

// IsFlippedBlocks returns true when block progression goes right-to-left
// (vertical-rl and sideways-rl).
func (wdm WritingDirectionMode) IsFlippedBlocks() bool {
	return wdm.WM == WritingModeVerticalRL || wdm.WM == WritingModeSidewaysRL
}

// IsLTR returns true for direction: ltr.
func (wdm WritingDirectionMode) IsLTR() bool {
	return wdm.Dir == DirectionLTR
}

// IsRTL returns true for direction: rtl.
func (wdm WritingDirectionMode) IsRTL() bool {
	return wdm.Dir == DirectionRTL
}

// IsOrthogonalTo returns true if the other mode has a different inline axis.
func (wdm WritingDirectionMode) IsOrthogonalTo(other WritingDirectionMode) bool {
	return wdm.IsHorizontal() != other.IsHorizontal()
}

// UsesCentralBaseline returns true if the writing mode uses the central
// baseline as the dominant baseline for inline alignment. Per CSS Writing
// Modes 3 §4.3, vertical-rl and vertical-lr use the central baseline
// (with text-orientation: mixed or upright), while horizontal-tb,
// sideways-rl, sideways-lr, and vertical modes with text-orientation:
// sideways use the alphabetic baseline.
func (wdm WritingDirectionMode) UsesCentralBaseline() bool {
	return wdm.WM == WritingModeVerticalRL || wdm.WM == WritingModeVerticalLR
}

// UsesCentralBaselineWithStyle returns true if the combination of writing
// mode and text-orientation results in the central baseline being dominant.
// text-orientation: sideways causes vertical modes to use alphabetic baseline.
func (wdm WritingDirectionMode) UsesCentralBaselineWithStyle(style *css.Style) bool {
	if wdm.WM != WritingModeVerticalRL && wdm.WM != WritingModeVerticalLR {
		return false
	}
	if style != nil {
		if to, ok := style.Get("text-orientation"); ok && to == "sideways" {
			return false
		}
	}
	return true
}
