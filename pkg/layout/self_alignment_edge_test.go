package layout

import "testing"

// selfAxisEdge resolves self-start / self-end for grid (columns: the
// container's inline axis, rows: its block axis) and flex (cross axis: the
// block axis in a row flex, the inline axis in a column flex) alike, for
// every container and item writing direction. Blink maps the item's edges
// onto the container's: flex through LogicalToLogical
// (writing_mode_utils.h:168-200) in FlexLayoutAlgorithm::ResolvedAlignSelf
// (flex/flex_layout_algorithm.cc:317-326), grid through the equivalent
// LogicalToPhysical / PhysicalToLogical pair in AxisEdgeFromItemPosition
// (grid/grid_item.cc:90-111 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The oracle compares physical
// sides directly: the container's start side in the axis against the
// item's start side on the same physical axis.
func TestSelfAxisEdgeAllWritingDirections(t *testing.T) {
	modes := []WritingMode{WritingModeHorizontalTB, WritingModeVerticalRL, WritingModeVerticalLR, WritingModeSidewaysRL, WritingModeSidewaysLR}
	var wdms []WritingDirectionMode
	for _, wm := range modes {
		for _, dir := range []Direction{DirectionLTR, DirectionRTL} {
			wdms = append(wdms, WritingDirectionMode{WM: wm, Dir: dir})
		}
	}
	for _, container := range wdms {
		for _, item := range wdms {
			for _, inlineAxis := range []bool{false, true} {
				containerStart := oraclePhysicalBlockStart(container)
				if inlineAxis {
					containerStart = oraclePhysicalInlineStart(container)
				}
				vertical := containerStart == oracleTop || containerStart == oracleBottom
				itemStart := oraclePhysicalInlineStart(item)
				if vertical == item.IsHorizontal() {
					itemStart = oraclePhysicalBlockStart(item)
				}
				wantStart, wantEnd := AxisEdgeStart, AxisEdgeEnd
				if containerStart != itemStart {
					wantStart, wantEnd = AxisEdgeEnd, AxisEdgeStart
				}
				if got := selfAxisEdge(inlineAxis, false, item, container); got != wantStart {
					t.Errorf("container %v item %v inline %v: self-start %v, want %v", container, item, inlineAxis, got, wantStart)
				}
				if got := selfAxisEdge(inlineAxis, true, item, container); got != wantEnd {
					t.Errorf("container %v item %v inline %v: self-end %v, want %v", container, item, inlineAxis, got, wantEnd)
				}
			}
		}
	}
}

type oracleSide int

const (
	oracleTop oracleSide = iota
	oracleRight
	oracleBottom
	oracleLeft
)

func oraclePhysicalBlockStart(wdm WritingDirectionMode) oracleSide {
	switch wdm.WM {
	case WritingModeVerticalRL, WritingModeSidewaysRL:
		return oracleRight
	case WritingModeVerticalLR, WritingModeSidewaysLR:
		return oracleLeft
	}
	return oracleTop
}

func oraclePhysicalInlineStart(wdm WritingDirectionMode) oracleSide {
	rtl := wdm.Dir == DirectionRTL
	switch {
	case wdm.IsHorizontal():
		if rtl {
			return oracleRight
		}
		return oracleLeft
	case wdm.WM == WritingModeSidewaysLR:
		if rtl {
			return oracleTop
		}
		return oracleBottom
	case rtl:
		return oracleBottom
	}
	return oracleTop
}
