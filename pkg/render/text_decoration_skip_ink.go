package render

import "sort"

// glyphExtent describes one glyph's inline extent in the same frame as the
// decoration line — for horizontal text it is the glyph's [x, x+width]
// projection on the X axis; for vertical text it is the glyph's [y, y+height]
// projection on the Y axis. The skip-ink helper is axis-agnostic; the caller
// builds the extents from whichever axis the decoration line lives on.
type glyphExtent struct {
	Start float64 // inline-axis coord of the glyph's leading edge
	End   float64 // inline-axis coord of the glyph's trailing edge
}

// inkSegment is one painted sub-segment of the decoration line after
// skip-ink culling. Returned segments are disjoint and sorted by Start.
type inkSegment struct {
	Start float64
	End   float64
}

// skipInkSegments returns the sub-segments of the decoration line that
// REMAIN after skip-ink removes the glyph extents.
//
// Inputs:
//   - start, end:  the decoration line's inline-axis range (start < end).
//   - thickness:   the decoration line's stroke thickness; used as the
//                  safety inflation on each side of each glyph extent so
//                  the gap matches the spec's "ink intersection" intent.
//                  Pass 0 for no inflation.
//   - glyphs:      glyph extents on the same inline axis. May be empty or
//                  in any order.
//
// Returns the list of [start, end) sub-segments in increasing order.
//
// Mirrors the algorithm in Blink's `decoration_line_painter.cc` at
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f: each glyph extent is
// inflated by half-thickness on both sides, the inflated intervals are
// merged, and the decoration line is partitioned into the complement.
func skipInkSegments(start, end, thickness float64, glyphs []glyphExtent) []inkSegment {
	if end <= start {
		return nil
	}
	if len(glyphs) == 0 {
		return []inkSegment{{Start: start, End: end}}
	}

	// Inflate each glyph extent by thickness/2 on both sides, clamp to
	// the decoration line's range, drop empty or out-of-range intervals.
	inflate := thickness / 2
	intervals := make([]inkSegment, 0, len(glyphs))
	for _, g := range glyphs {
		s := g.Start - inflate
		e := g.End + inflate
		if e <= start || s >= end {
			continue
		}
		if s < start {
			s = start
		}
		if e > end {
			e = end
		}
		if e <= s {
			continue
		}
		intervals = append(intervals, inkSegment{Start: s, End: e})
	}
	if len(intervals) == 0 {
		return []inkSegment{{Start: start, End: end}}
	}

	// Sort and merge overlapping / touching intervals so the complement
	// is straightforward.
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start < intervals[j].Start })
	merged := intervals[:0]
	cur := intervals[0]
	for _, iv := range intervals[1:] {
		if iv.Start <= cur.End {
			if iv.End > cur.End {
				cur.End = iv.End
			}
			continue
		}
		merged = append(merged, cur)
		cur = iv
	}
	merged = append(merged, cur)

	// Compute the complement on [start, end).
	out := make([]inkSegment, 0, len(merged)+1)
	cursor := start
	for _, iv := range merged {
		if iv.Start > cursor {
			out = append(out, inkSegment{Start: cursor, End: iv.Start})
		}
		if iv.End > cursor {
			cursor = iv.End
		}
	}
	if cursor < end {
		out = append(out, inkSegment{Start: cursor, End: end})
	}
	return out
}
