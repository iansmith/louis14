package text

import (
	"louis14/pkg/geometry/layoutunit"
)

// shapeWidthSnap is the Blink ShapeResult::SnappedWidth analog. Snaps a
// float64 width (HarfBuzz output, post-letter/word-spacing pad if any) to a
// LayoutUnit using CEIL on the raw quantum.
//
// CEIL is the conservative direction for line-fit decisions: over-reporting
// is safe (caller may break early), under-reporting is unsafe (caller may
// overflow the line box). See third_party/blink/renderer/platform/fonts/
// shaping/shape_result.h:
//
//	LayoutUnit SnappedWidth() const {
//	  return LayoutUnit::FromFloatCeil(width_);
//	}
func shapeWidthSnap(width float64) layoutunit.LayoutUnit {
	return layoutunit.FromFloat64Ceil(width)
}

// shapeAdvancePairSnap takes a float64 cumulative-positions slice and returns
// the Blink Snapped{Start,End}PositionForOffset pair. Start uses FLOOR, End
// uses CEIL — see shape_result.h:
//
//	LayoutUnit SnappedStartPositionForOffset(unsigned offset) const {
//	  return LayoutUnit::FromFloatFloor(PositionForOffset(offset));
//	}
//	LayoutUnit SnappedEndPositionForOffset(unsigned offset) const {
//	  return LayoutUnit::FromFloatCeil(PositionForOffset(offset));
//	}
//
// The pair-of-positions difference End[e].Sub(Start[s]) is the snapped width
// of the byte range [s, e]. It is an upper bound on the unsnapped width by at
// most 2 raw quanta total — independent of how many cluster offsets are
// between s and e. Summing per-cluster snapped widths instead would
// accumulate up to N quanta over N clusters, which is the class of
// rounding-drift bug 13f closes.
//
// At HarfBuzz cluster boundaries the input is exact-1/64-px (XAdvance is
// int32 26.6 fixed-point, divided by 64.0), so Start[i] and End[i] agree
// bit-exactly there. The pair-of-snaps form is required only when the cum
// slice carries non-quantum values (e.g. future inter-cluster sub-quantum
// pad); we adopt it unconditionally for Blink-parity and future-proofing.
func shapeAdvancePairSnap(cumFloat []float64) (start, end []layoutunit.LayoutUnit) {
	n := len(cumFloat)
	start = make([]layoutunit.LayoutUnit, n)
	end = make([]layoutunit.LayoutUnit, n)
	for i, v := range cumFloat {
		start[i] = layoutunit.FromFloat64Floor(v)
		end[i] = layoutunit.FromFloat64Ceil(v)
	}
	return start, end
}
