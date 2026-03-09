package layout

import "louis14/pkg/css"

// WritingMode represents CSS writing-mode values.
type WritingMode int

const (
	HorizontalTB WritingMode = iota // default: inline=LR, block=TB
	VerticalRL                      // inline=TB, block=RL
	VerticalLR                      // inline=TB, block=LR
	SidewaysLR                      // inline=BT (reversed), block=LR
)

// WritingModeFromStyle extracts WritingMode from a css.Style.
func WritingModeFromStyle(style *css.Style) WritingMode {
	if style == nil {
		return HorizontalTB
	}
	wm, _ := style.Get("writing-mode")
	switch wm {
	case "vertical-rl", "sideways-rl":
		return VerticalRL
	case "vertical-lr":
		return VerticalLR
	case "sideways-lr":
		return VerticalLR
	default:
		return HorizontalTB
	}
}

// Dir maps logical directions (inline/block) to physical coordinates (X/Y).
type Dir struct {
	WM WritingMode
}

func NewDir(wm WritingMode) Dir {
	return Dir{WM: wm}
}

func (d Dir) IsVertical() bool {
	return d.WM == VerticalRL || d.WM == VerticalLR || d.WM == SidewaysLR
}

func (d Dir) InlineSize(box *Box) float64 {
	if d.IsVertical() {
		return box.Height
	}
	return box.Width
}

func (d Dir) SetInlineSize(box *Box, v float64) {
	if d.IsVertical() {
		box.Height = v
	} else {
		box.Width = v
	}
}

func (d Dir) BlockSize(box *Box) float64 {
	if d.IsVertical() {
		return box.Width
	}
	return box.Height
}

func (d Dir) SetBlockSize(box *Box, v float64) {
	if d.IsVertical() {
		box.Width = v
	} else {
		box.Height = v
	}
}

func (d Dir) InlinePos(box *Box) float64 {
	if d.IsVertical() {
		return box.Y
	}
	return box.X
}

func (d Dir) SetInlinePos(box *Box, v float64) {
	if d.IsVertical() {
		box.Y = v
	} else {
		box.X = v
	}
}

func (d Dir) BlockPos(box *Box) float64 {
	if d.IsVertical() {
		return box.X
	}
	return box.Y
}

func (d Dir) SetBlockPos(box *Box, v float64) {
	if d.IsVertical() {
		box.X = v
	} else {
		box.Y = v
	}
}

// BoxEdge accessors.
// Mapping: h-tb InlineStart=Left,InlineEnd=Right,BlockStart=Top,BlockEnd=Bottom
//          v-rl InlineStart=Top,InlineEnd=Bottom,BlockStart=Right,BlockEnd=Left
//          v-lr InlineStart=Top,InlineEnd=Bottom,BlockStart=Left,BlockEnd=Right

func (d Dir) InlineStartEdge(e css.BoxEdge) float64 {
	if d.IsVertical() {
		return e.Top
	}
	return e.Left
}

func (d Dir) InlineEndEdge(e css.BoxEdge) float64 {
	if d.IsVertical() {
		return e.Bottom
	}
	return e.Right
}

func (d Dir) BlockStartEdge(e css.BoxEdge) float64 {
	switch d.WM {
	case VerticalRL:
		return e.Right
	case VerticalLR, SidewaysLR:
		return e.Left
	default:
		return e.Top
	}
}

func (d Dir) BlockEndEdge(e css.BoxEdge) float64 {
	switch d.WM {
	case VerticalRL:
		return e.Left
	case VerticalLR, SidewaysLR:
		return e.Right
	default:
		return e.Bottom
	}
}

func (d Dir) AutoInlineStart(e css.BoxEdge) bool {
	if d.IsVertical() {
		return e.AutoTop
	}
	return e.AutoLeft
}

func (d Dir) AutoInlineEnd(e css.BoxEdge) bool {
	if d.IsVertical() {
		return e.AutoBottom
	}
	return e.AutoRight
}

func (d Dir) AutoBlockStart(e css.BoxEdge) bool {
	switch d.WM {
	case VerticalRL:
		return e.AutoRight
	case VerticalLR, SidewaysLR:
		return e.AutoLeft
	default:
		return e.AutoTop
	}
}

func (d Dir) AutoBlockEnd(e css.BoxEdge) bool {
	switch d.WM {
	case VerticalRL:
		return e.AutoLeft
	case VerticalLR, SidewaysLR:
		return e.AutoRight
	default:
		return e.AutoBottom
	}
}

func (d Dir) SetInlineStartEdge(e *css.BoxEdge, v float64) {
	if d.IsVertical() {
		e.Top = v
	} else {
		e.Left = v
	}
}

func (d Dir) SetInlineEndEdge(e *css.BoxEdge, v float64) {
	if d.IsVertical() {
		e.Bottom = v
	} else {
		e.Right = v
	}
}

func (d Dir) SetBlockStartEdge(e *css.BoxEdge, v float64) {
	switch d.WM {
	case VerticalRL:
		e.Right = v
	case VerticalLR, SidewaysLR:
		e.Left = v
	default:
		e.Top = v
	}
}

func (d Dir) SetBlockEndEdge(e *css.BoxEdge, v float64) {
	switch d.WM {
	case VerticalRL:
		e.Left = v
	case VerticalLR, SidewaysLR:
		e.Right = v
	default:
		e.Bottom = v
	}
}

// CSS property name accessors.

func (d Dir) InlineSizeProp() string {
	if d.IsVertical() {
		return "height"
	}
	return "width"
}

func (d Dir) BlockSizeProp() string {
	if d.IsVertical() {
		return "width"
	}
	return "height"
}

func (d Dir) MinInlineSizeProp() string {
	if d.IsVertical() {
		return "min-height"
	}
	return "min-width"
}

func (d Dir) MaxInlineSizeProp() string {
	if d.IsVertical() {
		return "max-height"
	}
	return "max-width"
}

func (d Dir) MinBlockSizeProp() string {
	if d.IsVertical() {
		return "min-width"
	}
	return "min-height"
}

func (d Dir) MaxBlockSizeProp() string {
	if d.IsVertical() {
		return "max-width"
	}
	return "max-height"
}

// Compound helpers.

func (d Dir) ContentInlineSize(box *Box) float64 {
	return d.InlineSize(box) - d.InlineStartEdge(box.Padding) - d.InlineEndEdge(box.Padding) -
		d.InlineStartEdge(box.Border) - d.InlineEndEdge(box.Border)
}

func (d Dir) ContentBlockSize(box *Box) float64 {
	return d.BlockSize(box) - d.BlockStartEdge(box.Padding) - d.BlockEndEdge(box.Padding) -
		d.BlockStartEdge(box.Border) - d.BlockEndEdge(box.Border)
}

func (d Dir) InlineBorderBox(padding, border css.BoxEdge) float64 {
	return d.InlineStartEdge(padding) + d.InlineEndEdge(padding) +
		d.InlineStartEdge(border) + d.InlineEndEdge(border)
}

func (d Dir) BlockBorderBox(padding, border css.BoxEdge) float64 {
	return d.BlockStartEdge(padding) + d.BlockEndEdge(padding) +
		d.BlockStartEdge(border) + d.BlockEndEdge(border)
}

func (d Dir) ContentStartInlinePos(box *Box) float64 {
	return d.InlinePos(box) + d.InlineStartEdge(box.Border) + d.InlineStartEdge(box.Padding)
}

func (d Dir) ContentStartBlockPos(box *Box) float64 {
	return d.BlockPos(box) + d.BlockStartEdge(box.Border) + d.BlockStartEdge(box.Padding)
}

// BlockOffsetToPhysical converts a logical block offset to physical coordinate.
// For v-rl, block flows right-to-left: physX = contentEnd - offset - childBlockSize.
func (d Dir) BlockOffsetToPhysical(parentContentStart, parentContentEnd, blockOffset, childBlockSize float64) float64 {
	if d.WM == VerticalRL {
		return parentContentEnd - blockOffset - childBlockSize
	}
	return parentContentStart + blockOffset
}

func (d Dir) ExtractInline(x, y float64) float64 {
	if d.IsVertical() {
		return y
	}
	return x
}

func (d Dir) ExtractBlock(x, y float64) float64 {
	if d.IsVertical() {
		return x
	}
	return y
}

func (d Dir) MakePhysical(inlinePos, blockPos float64) (x, y float64) {
	if d.IsVertical() {
		return blockPos, inlinePos
	}
	return inlinePos, blockPos
}

func (d Dir) ViewportInlineSize(le *LayoutEngine) float64 {
	if d.IsVertical() {
		return le.viewport.height
	}
	return le.viewport.width
}

func (d Dir) ViewportBlockSize(le *LayoutEngine) float64 {
	if d.IsVertical() {
		return le.viewport.width
	}
	return le.viewport.height
}

// ZeroBlockMarginsAndPadding zeros block-direction margins/padding for inline elements.
func (d Dir) ZeroBlockMarginsAndPadding(margin *css.BoxEdge, padding *css.BoxEdge) {
	d.SetBlockStartEdge(margin, 0)
	d.SetBlockEndEdge(margin, 0)
	d.SetBlockStartEdge(padding, 0)
	d.SetBlockEndEdge(padding, 0)
}
