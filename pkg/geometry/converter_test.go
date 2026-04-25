package geometry

import (
	"testing"

	"louis14/pkg/geometry/layoutunit"
)

// Reference frame used across the tests:
//
//	outer (container) physical size: 200 × 100 (W × H)
//	inner (child) physical size:      40 ×  30
//	logical offset:                   inline=10, block=20
//
// Hand-traced expected physical offsets (mirrors writing_mode_converter.cc
// SlowToPhysical) for each (WM, Dir) combo:
//
// |  WM             | Dir | physical Left              | physical Top                |
// |-----------------|-----|----------------------------|----------------------------|
// | horizontal-tb   | LTR | inline = 10                | block  = 20                |
// | horizontal-tb   | RTL | outerW - inline - innerW   | block                      |
// |                 |     |   = 200 - 10 - 40 = 150    |   = 20                     |
// | vertical-rl     | LTR | outerW - block - innerW    | inline                     |
// |                 |     |   = 200 - 20 - 40 = 140    |   = 10                     |
// | vertical-rl     | RTL | outerW - block - innerW    | outerH - inline - innerH   |
// |                 |     |   = 140                    |   = 100 - 10 - 30 = 60     |
// | vertical-lr     | LTR | block = 20                 | inline = 10                |
// | vertical-lr     | RTL | block = 20                 | outerH - inline - innerH   |
// |                 |     |                            |   = 60                     |
// | sideways-rl     | LTR | (same as vrl-LTR)          | (same as vrl-LTR)          |
// | sideways-rl     | RTL | (same as vrl-RTL)          | (same as vrl-RTL)          |
// | sideways-lr     | LTR | block = 20                 | outerH - inline - innerH   |
// |                 |     |                            |   = 60                     |
// | sideways-lr     | RTL | block = 20                 | inline = 10                |

var (
	outer = NewPhysicalSize(200, 100)
	inner = NewPhysicalSize(40, 30)
	lo    = NewLogicalOffset(10, 20)
)

func TestToPhysicalOffset_AllWMDir(t *testing.T) {
	cases := []struct {
		name        string
		wm          WritingMode
		dir         Direction
		wantX, wantY int
	}{
		{"htb-ltr", WritingModeHorizontalTB, DirectionLTR, 10, 20},
		{"htb-rtl", WritingModeHorizontalTB, DirectionRTL, 150, 20},
		{"vrl-ltr", WritingModeVerticalRL, DirectionLTR, 140, 10},
		{"vrl-rtl", WritingModeVerticalRL, DirectionRTL, 140, 60},
		{"vlr-ltr", WritingModeVerticalLR, DirectionLTR, 20, 10},
		{"vlr-rtl", WritingModeVerticalLR, DirectionRTL, 20, 60},
		{"srl-ltr", WritingModeSidewaysRL, DirectionLTR, 140, 10},
		{"srl-rtl", WritingModeSidewaysRL, DirectionRTL, 140, 60},
		{"slr-ltr", WritingModeSidewaysLR, DirectionLTR, 20, 60},
		{"slr-rtl", WritingModeSidewaysLR, DirectionRTL, 20, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conv := NewWritingModeConverter(WritingDirectionMode{WM: c.wm, Dir: c.dir}, outer)
			got := conv.ToPhysicalOffset(lo, inner)
			wantLeft := layoutunit.New(c.wantX)
			wantTop := layoutunit.New(c.wantY)
			if got.Left != wantLeft || got.Top != wantTop {
				t.Errorf("ToPhysicalOffset(%s) = (%v, %v) px, want (%d, %d) px",
					c.name, got.LeftF64(), got.TopF64(), c.wantX, c.wantY)
			}
		})
	}
}

func TestToLogicalOffset_RoundTrip(t *testing.T) {
	// For every (WM, Dir), ToLogical(ToPhysical(lo)) should reproduce lo.
	// Verifies that the two conversion functions are inverses.
	wms := []WritingMode{
		WritingModeHorizontalTB, WritingModeVerticalRL, WritingModeVerticalLR,
		WritingModeSidewaysRL, WritingModeSidewaysLR,
	}
	dirs := []Direction{DirectionLTR, DirectionRTL}
	for _, wm := range wms {
		for _, dir := range dirs {
			conv := NewWritingModeConverter(WritingDirectionMode{WM: wm, Dir: dir}, outer)
			po := conv.ToPhysicalOffset(lo, inner)
			back := conv.ToLogicalOffset(po, inner)
			if back != lo {
				t.Errorf("round-trip wm=%d dir=%d: lo=%v → po=%v → lo=%v, mismatch",
					wm, dir, lo, po, back)
			}
		}
	}
}

func TestSizeConversions(t *testing.T) {
	ps := NewPhysicalSize(200, 100)

	// Horizontal-tb: width→inline, height→block.
	if ls := LogicalSizeFromPhysical(ps, WritingModeHorizontalTB); ls.InlineSize != layoutunit.New(200) || ls.BlockSize != layoutunit.New(100) {
		t.Errorf("htb LogicalSizeFromPhysical: got (%v, %v), want (200, 100)", ls.InlineF64(), ls.BlockF64())
	}

	// Vertical: width→block, height→inline.
	for _, wm := range []WritingMode{WritingModeVerticalRL, WritingModeVerticalLR, WritingModeSidewaysRL, WritingModeSidewaysLR} {
		ls := LogicalSizeFromPhysical(ps, wm)
		if ls.InlineSize != layoutunit.New(100) || ls.BlockSize != layoutunit.New(200) {
			t.Errorf("wm=%d LogicalSizeFromPhysical: got (%v, %v), want inline=100 block=200", wm, ls.InlineF64(), ls.BlockF64())
		}

		// Inverse.
		back := PhysicalSizeFromLogical(ls, wm)
		if back != ps {
			t.Errorf("wm=%d round-trip size: %v → %v → %v, mismatch", wm, ps, ls, back)
		}
	}
}

func TestToPhysicalRect(t *testing.T) {
	// Logical rect inline-start=10, block-start=20, inline-size=30, block-size=40
	// in vertical-rl (inline = vertical y, block = horizontal x flipped).
	lr := NewLogicalRect(10, 20, 30, 40)
	conv := NewWritingModeConverter(WritingDirectionMode{WM: WritingModeVerticalRL, Dir: DirectionLTR}, outer)
	pr := conv.ToPhysicalRect(lr)

	// Size: vertical mode swaps inline/block — physical width=block-size=40, height=inline-size=30.
	if pr.Size.Width != layoutunit.New(40) || pr.Size.Height != layoutunit.New(30) {
		t.Errorf("ToPhysicalRect size = (%v, %v), want (40, 30)", pr.Size.Width.Float64(), pr.Size.Height.Float64())
	}

	// Offset: vrl-LTR. innerW = 40 (block-size).
	// Left = outerW - block - innerW = 200 - 20 - 40 = 140.
	// Top = inline = 10.
	if pr.Offset.Left != layoutunit.New(140) || pr.Offset.Top != layoutunit.New(10) {
		t.Errorf("ToPhysicalRect offset = (%v, %v), want (140, 10)", pr.Offset.LeftF64(), pr.Offset.TopF64())
	}

	// Round-trip.
	back := conv.ToLogicalRect(pr)
	if back != lr {
		t.Errorf("rect round-trip: %v → %v → %v, mismatch", lr, pr, back)
	}
}

func TestSubPixelOffsetSurvivesConversion(t *testing.T) {
	// Pick a sub-pixel logical offset (raw fractional), ensure the physical
	// result stays sub-pixel exact and round-trips cleanly. Uses Blink's
	// fundamental discipline: bit-exact through the conversion (no float
	// associativity drift).
	loSub := LogicalOffset{
		InlineOffset: layoutunit.FromFloat64Round(10.5),
		BlockOffset:  layoutunit.FromFloat64Round(20.25),
	}
	conv := NewWritingModeConverter(WritingDirectionMode{WM: WritingModeVerticalRL, Dir: DirectionRTL}, outer)
	po := conv.ToPhysicalOffset(loSub, inner)
	back := conv.ToLogicalOffset(po, inner)
	if back.InlineOffset != loSub.InlineOffset || back.BlockOffset != loSub.BlockOffset {
		t.Errorf("sub-pixel round-trip: %v → %v → %v, mismatch", loSub, po, back)
	}
	// And the raw values stay sub-pixel: InlineOffset=10.5 → raw 672.
	if loSub.InlineOffset.Raw() != 672 || loSub.BlockOffset.Raw() != 1296 {
		t.Errorf("expected sub-pixel raws (672, 1296) for (10.5, 20.25); got (%d, %d)",
			loSub.InlineOffset.Raw(), loSub.BlockOffset.Raw())
	}
}

func TestF64Accessors(t *testing.T) {
	off := LogicalOffsetFromF64Round(1.5, 2.5)
	if got := off.InlineF64(); got != 1.5 {
		t.Errorf("InlineF64 = %v, want 1.5", got)
	}
	if got := off.BlockF64(); got != 2.5 {
		t.Errorf("BlockF64 = %v, want 2.5", got)
	}

	sz := PhysicalSizeFromF64Round(100.0, 50.5)
	if got := sz.WidthF64(); got != 100.0 {
		t.Errorf("WidthF64 = %v, want 100", got)
	}
	if got := sz.HeightF64(); got != 50.5 {
		t.Errorf("HeightF64 = %v, want 50.5", got)
	}
}

func TestCeilFloorEntry(t *testing.T) {
	// Ceil enters at the next-higher quantum.
	if r := PhysicalSizeFromF64Ceil(0.01, 0.0).Width.Raw(); r != 1 {
		t.Errorf("PhysicalSizeFromF64Ceil(0.01).Width.Raw() = %d, want 1", r)
	}
	// Floor enters at the next-lower quantum.
	if r := PhysicalSizeFromF64Floor(0.01, 0.0).Width.Raw(); r != 0 {
		t.Errorf("PhysicalSizeFromF64Floor(0.01).Width.Raw() = %d, want 0", r)
	}
	// Round = nearest.
	if r := PhysicalSizeFromF64Round(0.01, 0.0).Width.Raw(); r != 1 {
		t.Errorf("PhysicalSizeFromF64Round(0.01).Width.Raw() = %d, want 1", r)
	}
}

func TestLogicalRectAccessors(t *testing.T) {
	r := NewLogicalRect(10, 20, 30, 40)
	if got := r.InlineEnd(); got != layoutunit.New(40) {
		t.Errorf("InlineEnd = %v, want 40", got.Float64())
	}
	if got := r.BlockEnd(); got != layoutunit.New(60) {
		t.Errorf("BlockEnd = %v, want 60", got.Float64())
	}
}

func TestPhysicalRectAccessors(t *testing.T) {
	r := NewPhysicalRect(10, 20, 30, 40)
	if got := r.Right(); got != layoutunit.New(40) {
		t.Errorf("Right = %v, want 40", got.Float64())
	}
	if got := r.Bottom(); got != layoutunit.New(60) {
		t.Errorf("Bottom = %v, want 60", got.Float64())
	}
}

func TestOffsetAddSub(t *testing.T) {
	a := NewLogicalOffset(10, 20)
	b := NewLogicalOffset(3, 4)
	if got := a.Add(b); got != NewLogicalOffset(13, 24) {
		t.Errorf("LogicalOffset.Add: got %v, want (13, 24)", got)
	}
	if got := a.Sub(b); got != NewLogicalOffset(7, 16) {
		t.Errorf("LogicalOffset.Sub: got %v, want (7, 16)", got)
	}

	pa := NewPhysicalOffset(10, 20)
	pb := NewPhysicalOffset(3, 4)
	if got := pa.Add(pb); got != NewPhysicalOffset(13, 24) {
		t.Errorf("PhysicalOffset.Add: got %v, want (13, 24)", got)
	}
}

func TestWritingDirectionMode_Predicates(t *testing.T) {
	htb := WritingDirectionMode{WM: WritingModeHorizontalTB, Dir: DirectionLTR}
	vrl := WritingDirectionMode{WM: WritingModeVerticalRL, Dir: DirectionRTL}
	srl := WritingDirectionMode{WM: WritingModeSidewaysRL, Dir: DirectionLTR}
	if !htb.IsHorizontal() || htb.IsVertical() || htb.IsFlippedBlocks() || !htb.IsLTR() || htb.IsRTL() {
		t.Error("htb predicates wrong")
	}
	if vrl.IsHorizontal() || !vrl.IsVertical() || !vrl.IsFlippedBlocks() || vrl.IsLTR() || !vrl.IsRTL() {
		t.Error("vrl predicates wrong")
	}
	if !srl.IsFlippedBlocks() {
		t.Error("sideways-rl should be flipped-blocks")
	}
}
