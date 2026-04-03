package layout

import (
	"fmt"
	"testing"
)

// All 10 writing direction modes (5 writing modes x 2 directions).
var allModes = []WritingDirectionMode{
	{WritingModeHorizontalTB, DirectionLTR},
	{WritingModeHorizontalTB, DirectionRTL},
	{WritingModeVerticalRL, DirectionLTR},
	{WritingModeVerticalRL, DirectionRTL},
	{WritingModeVerticalLR, DirectionLTR},
	{WritingModeVerticalLR, DirectionRTL},
	{WritingModeSidewaysRL, DirectionLTR},
	{WritingModeSidewaysRL, DirectionRTL},
	{WritingModeSidewaysLR, DirectionLTR},
	{WritingModeSidewaysLR, DirectionRTL},
}

func modeName(wdm WritingDirectionMode) string {
	wmNames := map[WritingMode]string{
		WritingModeHorizontalTB: "HTB",
		WritingModeVerticalRL:   "VRL",
		WritingModeVerticalLR:   "VLR",
		WritingModeSidewaysRL:   "SRL",
		WritingModeSidewaysLR:   "SLR",
	}
	dirNames := map[Direction]string{
		DirectionLTR: "LTR",
		DirectionRTL: "RTL",
	}
	return fmt.Sprintf("%s-%s", wmNames[wdm.WM], dirNames[wdm.Dir])
}

// --- Size conversion tests ---

func TestToLogicalSize_HorizontalIsIdentity(t *testing.T) {
	phys := PhysicalSize{Width: 800, Height: 600}
	logical := ToLogicalSize(phys, WritingModeHorizontalTB)
	if logical.InlineSize != 800 || logical.BlockSize != 600 {
		t.Errorf("HTB: got inline=%v block=%v, want 800,600", logical.InlineSize, logical.BlockSize)
	}
}

func TestToLogicalSize_VerticalSwaps(t *testing.T) {
	phys := PhysicalSize{Width: 800, Height: 600}
	for _, wm := range []WritingMode{WritingModeVerticalRL, WritingModeVerticalLR, WritingModeSidewaysRL, WritingModeSidewaysLR} {
		logical := ToLogicalSize(phys, wm)
		if logical.InlineSize != 600 || logical.BlockSize != 800 {
			t.Errorf("%d: got inline=%v block=%v, want 600,800", wm, logical.InlineSize, logical.BlockSize)
		}
	}
}

func TestSizeRoundTrip(t *testing.T) {
	phys := PhysicalSize{Width: 800, Height: 600}
	for _, wm := range []WritingMode{WritingModeHorizontalTB, WritingModeVerticalRL, WritingModeVerticalLR, WritingModeSidewaysRL, WritingModeSidewaysLR} {
		logical := ToLogicalSize(phys, wm)
		back := ToPhysicalSize(logical, wm)
		if back != phys {
			t.Errorf("WM %d: round trip failed: %v -> %v -> %v", wm, phys, logical, back)
		}
	}
}

// --- Offset conversion tests ---

func TestOffsetRoundTrip(t *testing.T) {
	// A 100x50 child at offset (30, 20) inside a 800x600 container.
	outerSize := PhysicalSize{Width: 800, Height: 600}
	innerSize := PhysicalSize{Width: 100, Height: 50}
	physOffset := PhysicalOffset{X: 30, Y: 20}

	for _, wdm := range allModes {
		t.Run(modeName(wdm), func(t *testing.T) {
			conv := NewConverter(wdm, outerSize)
			logical := conv.ToLogicalOffset(physOffset, innerSize)
			back := conv.ToPhysicalOffset(logical, innerSize)
			if back.X != physOffset.X || back.Y != physOffset.Y {
				t.Errorf("round trip failed: phys=%v -> logical=%v -> phys=%v",
					physOffset, logical, back)
			}
		})
	}
}

func TestOffset_HTB_LTR(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	conv := NewConverter(WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}, outer)

	// Identity: inline=x, block=y
	logical := conv.ToLogicalOffset(PhysicalOffset{X: 30, Y: 20}, inner)
	if logical.InlineOffset != 30 || logical.BlockOffset != 20 {
		t.Errorf("got %v, want inline=30 block=20", logical)
	}
}

func TestOffset_HTB_RTL(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	conv := NewConverter(WritingDirectionMode{WritingModeHorizontalTB, DirectionRTL}, outer)

	// inline = outerW - x - innerW = 800 - 30 - 100 = 670
	logical := conv.ToLogicalOffset(PhysicalOffset{X: 30, Y: 20}, inner)
	if logical.InlineOffset != 670 || logical.BlockOffset != 20 {
		t.Errorf("got %v, want inline=670 block=20", logical)
	}
}

func TestOffset_VRL_LTR(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	conv := NewConverter(WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}, outer)

	// inline=y=20, block=outerW-x-innerW=800-30-100=670
	logical := conv.ToLogicalOffset(PhysicalOffset{X: 30, Y: 20}, inner)
	if logical.InlineOffset != 20 || logical.BlockOffset != 670 {
		t.Errorf("got %v, want inline=20 block=670", logical)
	}
}

func TestOffset_VRL_RTL(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	conv := NewConverter(WritingDirectionMode{WritingModeVerticalRL, DirectionRTL}, outer)

	// inline=outerH-y-innerH=600-20-50=530, block=outerW-x-innerW=670
	logical := conv.ToLogicalOffset(PhysicalOffset{X: 30, Y: 20}, inner)
	if logical.InlineOffset != 530 || logical.BlockOffset != 670 {
		t.Errorf("got %v, want inline=530 block=670", logical)
	}
}

func TestOffset_VLR_LTR(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	conv := NewConverter(WritingDirectionMode{WritingModeVerticalLR, DirectionLTR}, outer)

	// inline=y=20, block=x=30
	logical := conv.ToLogicalOffset(PhysicalOffset{X: 30, Y: 20}, inner)
	if logical.InlineOffset != 20 || logical.BlockOffset != 30 {
		t.Errorf("got %v, want inline=20 block=30", logical)
	}
}

func TestOffset_VLR_RTL(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	conv := NewConverter(WritingDirectionMode{WritingModeVerticalLR, DirectionRTL}, outer)

	// inline=outerH-y-innerH=530, block=x=30
	logical := conv.ToLogicalOffset(PhysicalOffset{X: 30, Y: 20}, inner)
	if logical.InlineOffset != 530 || logical.BlockOffset != 30 {
		t.Errorf("got %v, want inline=530 block=30", logical)
	}
}

func TestOffset_SLR_LTR(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	conv := NewConverter(WritingDirectionMode{WritingModeSidewaysLR, DirectionLTR}, outer)

	// SLR+LTR: inline=outerH-y-innerH=530, block=x=30
	logical := conv.ToLogicalOffset(PhysicalOffset{X: 30, Y: 20}, inner)
	if logical.InlineOffset != 530 || logical.BlockOffset != 30 {
		t.Errorf("got %v, want inline=530 block=30", logical)
	}
}

func TestOffset_SLR_RTL(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	conv := NewConverter(WritingDirectionMode{WritingModeSidewaysLR, DirectionRTL}, outer)

	// SLR+RTL: inline=y=20, block=x=30
	logical := conv.ToLogicalOffset(PhysicalOffset{X: 30, Y: 20}, inner)
	if logical.InlineOffset != 20 || logical.BlockOffset != 30 {
		t.Errorf("got %v, want inline=20 block=30", logical)
	}
}

// Verify VRL and SRL produce identical results (Blink treats them identically for layout).
func TestOffset_VRL_SRL_Identical(t *testing.T) {
	outer := PhysicalSize{Width: 800, Height: 600}
	inner := PhysicalSize{Width: 100, Height: 50}
	phys := PhysicalOffset{X: 30, Y: 20}

	for _, dir := range []Direction{DirectionLTR, DirectionRTL} {
		vrl := NewConverter(WritingDirectionMode{WritingModeVerticalRL, dir}, outer)
		srl := NewConverter(WritingDirectionMode{WritingModeSidewaysRL, dir}, outer)

		logVRL := vrl.ToLogicalOffset(phys, inner)
		logSRL := srl.ToLogicalOffset(phys, inner)
		if logVRL != logSRL {
			t.Errorf("dir=%v: VRL=%v != SRL=%v", dir, logVRL, logSRL)
		}
	}
}

// --- Edge conversion tests ---

func TestEdges_HTB_LTR(t *testing.T) {
	phys := PhysicalEdges{Top: 10, Right: 20, Bottom: 30, Left: 40}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	logical := ToLogicalEdges(phys, wdm)

	expect := LogicalEdges{InlineStart: 40, InlineEnd: 20, BlockStart: 10, BlockEnd: 30}
	if logical != expect {
		t.Errorf("got %v, want %v", logical, expect)
	}
}

func TestEdges_HTB_RTL(t *testing.T) {
	phys := PhysicalEdges{Top: 10, Right: 20, Bottom: 30, Left: 40}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionRTL}
	logical := ToLogicalEdges(phys, wdm)

	// RTL swaps inline-start/end: inline-start=right=20, inline-end=left=40
	expect := LogicalEdges{InlineStart: 20, InlineEnd: 40, BlockStart: 10, BlockEnd: 30}
	if logical != expect {
		t.Errorf("got %v, want %v", logical, expect)
	}
}

func TestEdges_VRL_LTR(t *testing.T) {
	phys := PhysicalEdges{Top: 10, Right: 20, Bottom: 30, Left: 40}
	wdm := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	logical := ToLogicalEdges(phys, wdm)

	expect := LogicalEdges{InlineStart: 10, InlineEnd: 30, BlockStart: 20, BlockEnd: 40}
	if logical != expect {
		t.Errorf("got %v, want %v", logical, expect)
	}
}

func TestEdges_SLR_LTR(t *testing.T) {
	phys := PhysicalEdges{Top: 10, Right: 20, Bottom: 30, Left: 40}
	wdm := WritingDirectionMode{WritingModeSidewaysLR, DirectionLTR}
	logical := ToLogicalEdges(phys, wdm)

	// SLR+LTR: inline-start=bottom=30, inline-end=top=10
	expect := LogicalEdges{InlineStart: 30, InlineEnd: 10, BlockStart: 40, BlockEnd: 20}
	if logical != expect {
		t.Errorf("got %v, want %v", logical, expect)
	}
}

func TestEdgesRoundTrip(t *testing.T) {
	phys := PhysicalEdges{Top: 10, Right: 20, Bottom: 30, Left: 40}

	for _, wdm := range allModes {
		t.Run(modeName(wdm), func(t *testing.T) {
			logical := ToLogicalEdges(phys, wdm)
			back := ToPhysicalEdges(logical, wdm)
			if back != phys {
				t.Errorf("round trip failed: %v -> %v -> %v", phys, logical, back)
			}
		})
	}
}

// --- WritingDirectionMode predicate tests ---

func TestIsHorizontal(t *testing.T) {
	htb := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	if !htb.IsHorizontal() {
		t.Error("HTB should be horizontal")
	}
	vrl := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	if vrl.IsHorizontal() {
		t.Error("VRL should not be horizontal")
	}
}

func TestIsOrthogonal(t *testing.T) {
	htb := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	vrl := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	vlr := WritingDirectionMode{WritingModeVerticalLR, DirectionRTL}

	if !htb.IsOrthogonalTo(vrl) {
		t.Error("HTB and VRL should be orthogonal")
	}
	if htb.IsOrthogonalTo(htb) {
		t.Error("HTB and HTB should not be orthogonal")
	}
	if vrl.IsOrthogonalTo(vlr) {
		t.Error("VRL and VLR should not be orthogonal")
	}
}

func TestIsFlippedBlocks(t *testing.T) {
	tests := []struct {
		wm   WritingMode
		want bool
	}{
		{WritingModeHorizontalTB, false},
		{WritingModeVerticalRL, true},
		{WritingModeVerticalLR, false},
		{WritingModeSidewaysRL, true},
		{WritingModeSidewaysLR, false},
	}
	for _, tt := range tests {
		wdm := WritingDirectionMode{tt.wm, DirectionLTR}
		if got := wdm.IsFlippedBlocks(); got != tt.want {
			t.Errorf("WM %d: IsFlippedBlocks()=%v, want %v", tt.wm, got, tt.want)
		}
	}
}

// --- Stress test: every mode, verify ToPhysical(ToLogical(x)) == x ---

func TestOffsetRoundTrip_AllPositions(t *testing.T) {
	outerSize := PhysicalSize{Width: 800, Height: 600}

	// Test a grid of positions and child sizes.
	positions := []PhysicalOffset{
		{0, 0}, {100, 200}, {700, 550}, {400, 300},
	}
	childSizes := []PhysicalSize{
		{50, 30}, {100, 100}, {200, 50}, {1, 1},
	}

	for _, wdm := range allModes {
		conv := NewConverter(wdm, outerSize)
		for _, pos := range positions {
			for _, cs := range childSizes {
				logical := conv.ToLogicalOffset(pos, cs)
				back := conv.ToPhysicalOffset(logical, cs)
				if back.X != pos.X || back.Y != pos.Y {
					t.Errorf("%s pos=%v child=%v: logical=%v back=%v",
						modeName(wdm), pos, cs, logical, back)
				}
			}
		}
	}
}
