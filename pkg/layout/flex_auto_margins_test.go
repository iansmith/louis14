package layout

import (
	"testing"

	"louis14/pkg/css"
)

// getItemAutoMargins maps a flex item's physical margin:auto flags onto
// its main and cross axes through its writing direction, for every writing
// direction, auto-margin combination and flex direction. The oracle maps
// the flags with ToLogicalEdges (1 marks auto), then picks the axes: the
// inline axis is main when isRow, else the block axis.
func TestFlexItemAutoMarginsAllWritingDirections(t *testing.T) {
	modes := []WritingMode{WritingModeHorizontalTB, WritingModeVerticalRL, WritingModeVerticalLR, WritingModeSidewaysRL, WritingModeSidewaysLR}
	sides := []string{"margin-top", "margin-right", "margin-bottom", "margin-left"}
	for _, wm := range modes {
		for _, dir := range []Direction{DirectionLTR, DirectionRTL} {
			wdm := WritingDirectionMode{WM: wm, Dir: dir}
			for mask := 0; mask < 16; mask++ {
				style := css.NewStyle()
				for i, side := range sides {
					if mask&(1<<i) != 0 {
						style.Set(side, "auto")
					}
				}
				on := func(bit int) float64 {
					if mask&(1<<bit) != 0 {
						return 1
					}
					return 0
				}
				l := ToLogicalEdges(PhysicalEdges{Top: on(0), Right: on(1), Bottom: on(2), Left: on(3)}, wdm)
				inline := [2]bool{l.InlineStart == 1, l.InlineEnd == 1}
				block := [2]bool{l.BlockStart == 1, l.BlockEnd == 1}
				for _, isRow := range []bool{false, true} {
					want := [4]bool{block[0], block[1], inline[0], inline[1]}
					if isRow {
						want = [4]bool{inline[0], inline[1], block[0], block[1]}
					}
					ms, me, cs, ce := getItemAutoMargins(style, wdm, isRow)
					if got := [4]bool{ms, me, cs, ce}; got != want {
						t.Errorf("%v mask %04b row %v: main/cross auto %v, want %v", wdm, mask, isRow, got, want)
					}
				}
			}
		}
	}
}
