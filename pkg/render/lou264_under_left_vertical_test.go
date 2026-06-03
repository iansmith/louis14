package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/layout"
)

// LOU-264: text-underline-position: under left in vertical-rl (central-baseline,
// upright/mixed) text must NOT apply the alphabetic-baseline descent shift that
// `under` applies in horizontal text. Per Blink's ResolveUnderlinePosition() /
// TextDecorationOffset::ComputeUnderlineOffset (text_decoration_offset.cc /
// text_decoration_info.cc:13-47 @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f),
// the `under` content-edge push is gated on the run using the ALPHABETIC baseline;
// central-baseline (upright/mixed vertical) runs select the side via left/right/auto
// and the line sits at the central-baseline under-edge — `under` resolves like `auto`.
//
// On current (buggy) code computeUnderlineZeroOffset(under) returns ascent+descent
// for BOTH axes, which shifts the vertical underline by `descent` (~4px) and breaks
// css-text-decor/text-underline-position-vertical-ja.html by ~100 pixels.

// TestUnderlineZeroOffset_UnderInVerticalIsAuto is the RED test: in vertical
// (central-baseline) axis, `under` must yield the same zero-offset as `auto` (0),
// not ascent+descent.
func TestUnderlineZeroOffset_UnderInVerticalIsAuto(t *testing.T) {
	box := &layout.Box{X: 100, Y: 0, Width: 20}
	// Ahem-like metrics at font-size 20: ascent=16, descent=4.
	vinfo := newVerticalTextDecorationInfo(box, 80, 20, 16, 4, 0, blockUnderLeft)

	autoOff := vinfo.computeUnderlineZeroOffset(css.TextUnderlinePositionAuto)
	underOff := vinfo.computeUnderlineZeroOffset(css.TextUnderlinePositionUnder)

	if underOff != autoOff {
		t.Errorf("vertical (central-baseline) underline zero-offset: under=%v auto=%v; "+
			"want equal (under must not apply the descent shift in central-baseline text)",
			underOff, autoOff)
	}
}

// TestUnderlineZeroOffset_UnderInHorizontalStillShifts guards that the fix is
// scoped to the vertical/central-baseline axis only: horizontal `under` must
// still push by ascent+descent (the alphabetic-baseline content-edge semantics).
func TestUnderlineZeroOffset_UnderInHorizontalStillShifts(t *testing.T) {
	box := &layout.Box{Y: 0}
	hinfo := newTextDecorationInfo(box, 80, 20, 16, 4, 0)

	autoOff := hinfo.computeUnderlineZeroOffset(css.TextUnderlinePositionAuto)
	underOff := hinfo.computeUnderlineZeroOffset(css.TextUnderlinePositionUnder)

	if autoOff != 0 {
		t.Errorf("horizontal auto zero-offset = %v; want 0", autoOff)
	}
	if underOff != 16+4 {
		t.Errorf("horizontal under zero-offset = %v; want ascent+descent=20", underOff)
	}
}
