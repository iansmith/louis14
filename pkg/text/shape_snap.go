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

// ShapeCumulative carries the per-byte cumulative-position pair returned by
// ShapeAdvances and ShapeAdvancesMixed. Mirrors Blink ShapeResult's
// SnappedStart/EndPositionForOffset (shape_result.h):
//
//	Start[i] = LayoutUnit::FromFloatFloor(PositionForOffset(i))
//	End[i]   = LayoutUnit::FromFloatCeil (PositionForOffset(i))
//
// Both slices have length len(text)+1; index i holds the snapped position at
// which text[i:] begins. Index 0 is always Zero; the final index equals the
// snapped MeasureText advance.
//
// Use SnappedWidth(s, e) for the pair-of-positions read pattern that mirrors
// Blink's CachedWidth(start, end).
type ShapeCumulative struct {
	Start []layoutunit.LayoutUnit
	End   []layoutunit.LayoutUnit
}

// SnappedWidth returns the snapped advance from byte offset s to byte offset
// e. Mirrors Blink ShapeResult::CachedWidth(start, end). The result is an
// upper bound on the unsnapped width by at most 2 raw quanta total —
// independent of how many cluster offsets are between s and e. This is the
// load-bearing 13f invariant: pair-of-positions caps rounding error at a
// constant; summing per-cluster snapped widths would accumulate up to N
// quanta over N clusters.
func (c ShapeCumulative) SnappedWidth(s, e int) layoutunit.LayoutUnit {
	return c.End[e].Sub(c.Start[s])
}

// shapeAdvancePairSnap takes a float64 cumulative-positions slice and returns
// the Blink Snapped{Start,End}PositionForOffset pair packaged as a
// ShapeCumulative. Start uses FLOOR, End uses CEIL — see shape_result.h:
//
//	LayoutUnit SnappedStartPositionForOffset(unsigned offset) const {
//	  return LayoutUnit::FromFloatFloor(PositionForOffset(offset));
//	}
//	LayoutUnit SnappedEndPositionForOffset(unsigned offset) const {
//	  return LayoutUnit::FromFloatCeil(PositionForOffset(offset));
//	}
//
// At HarfBuzz cluster boundaries the input is exact-1/64-px (XAdvance is
// int32 26.6 fixed-point, divided by 64.0), so Start[i] and End[i] agree
// bit-exactly there. The pair-of-snaps form is required only when the cum
// slice carries non-quantum values (e.g. future inter-cluster sub-quantum
// pad); we adopt it unconditionally for Blink-parity and future-proofing.
func shapeAdvancePairSnap(cumFloat []float64) ShapeCumulative {
	n := len(cumFloat)
	start := make([]layoutunit.LayoutUnit, n)
	end := make([]layoutunit.LayoutUnit, n)
	for i, v := range cumFloat {
		start[i] = layoutunit.FromFloat64Floor(v)
		end[i] = layoutunit.FromFloat64Ceil(v)
	}
	return ShapeCumulative{Start: start, End: end}
}
