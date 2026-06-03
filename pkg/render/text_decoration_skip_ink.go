package render

import (
	"sort"
	"unicode"

	"mazarin/textshape"
)

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
//     safety inflation on each side of each glyph extent so
//     the gap matches the spec's "ink intersection" intent.
//     Pass 0 for no inflation.
//   - glyphs:      glyph extents on the same inline axis. May be empty or
//     in any order.
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

// buildGlyphExtentsVertical returns the inline-axis (Y) extents of each
// non-whitespace glyph in `text` stacked vertically starting at `startY`,
// using fontSize as the cell height and adding `letterSpacing` between
// cells. Mirrors buildGlyphExtentsHorizontal but for the upright-vertical
// (Strategy B) paint path where the inline axis runs along Y.
func buildGlyphExtentsVertical(text string, fontSize, startY, letterSpacing float64) []glyphExtent {
	if text == "" {
		return nil
	}
	out := make([]glyphExtent, 0, len(text))
	y := startY
	for _, ch := range text {
		if !unicode.IsSpace(ch) {
			out = append(out, glyphExtent{Start: y, End: y + fontSize})
		}
		y += fontSize
		if letterSpacing != 0 {
			y += letterSpacing
		}
	}
	return out
}

// buildGlyphExtentsHorizontal returns the inline-axis (X) extents of each
// non-whitespace glyph in `text` rendered at `startX`, using the given
// font + features + letter/word spacing. Whitespace runs are excluded so
// skip-ink does not punch holes in spaces between words — per CSS Text
// Decor 4 §1.1.5 the property only skips over glyphs (visible ink), and
// the spec's separate text-decoration-skip-spaces property covers
// whitespace handling.
//
// This is the louis14 stand-in for Blink's per-glyph ink-bounding-box
// data. For Ahem (1em-square solid glyphs) the per-character advance
// equals the ink extent exactly, so it matches the spec contract for the
// text-decoration-skip-ink-*-002 reftests. For real fonts the advance is
// a superset of the ink extent (in particular for italic / kerning), so
// the approximation is conservative: it may skip slightly more than the
// minimum required.
func (r *Renderer) buildGlyphExtentsHorizontal(text string, fontID int32, features []textshape.FontFeature, startX, letterSpacing, wordSpacing float64) []glyphExtent {
	if text == "" {
		return nil
	}
	out := make([]glyphExtent, 0, len(text))
	x := startX
	for _, ch := range text {
		cw := r.measureTextStr(string(ch), fontID, features)
		if !unicode.IsSpace(ch) {
			out = append(out, glyphExtent{Start: x, End: x + cw})
		}
		x += cw + letterSpacing
		if ch == ' ' {
			x += wordSpacing
		}
	}
	return out
}
