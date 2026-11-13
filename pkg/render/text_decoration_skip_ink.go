package render

import (
	"sort"
	"unicode"
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

// decorationBandCrossesGlyphs reports whether a decoration line whose
// bounds run from boundsTop to boundsBottom across the line crosses glyph
// ink running from glyphTop to glyphBottom there, so skip-ink applies.
func decorationBandCrossesGlyphs(boundsTop, boundsBottom, glyphTop, glyphBottom float64) bool {
	return boundsBottom > glyphTop && boundsTop < glyphBottom
}

// kDecorationClipMaxDilation caps skip-ink's dilation of a glyph intercept
// (core/paint/text_painter.cc:46 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
const kDecorationClipMaxDilation = 13

// skipInkSegments returns the sub-segments of the decoration line that
// REMAIN after skip-ink removes the glyph extents.
//
// Inputs:
//   - start, end:  the decoration line's inline-axis range (start < end).
//   - thickness:   the decoration line's stroke thickness; each glyph extent
//     is dilated by it, at most kDecorationClipMaxDilation, on
//     both sides. Pass 0 for no dilation.
//   - glyphs:      glyph extents on the same inline axis. May be empty or
//     in any order.
//
// Returns the list of [start, end) sub-segments in increasing order.
//
// Mirrors TextPainter::ClipDecorationLine (core/paint/text_painter.cc:
// 574-622 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): each glyph intercept
// is dilated by min(thickness, kDecorationClipMaxDilation) on both sides
// and clipped out, so the line keeps the complement of the merged
// intervals.
func skipInkSegments(start, end, thickness float64, glyphs []glyphExtent) []inkSegment {
	if end <= start {
		return nil
	}
	if len(glyphs) == 0 {
		return []inkSegment{{Start: start, End: end}}
	}

	// Dilate each glyph extent on both sides, clamp to the decoration
	// line's range, drop empty or out-of-range intervals.
	inflate := min(thickness, kDecorationClipMaxDilation)
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
func (r *Renderer) buildGlyphExtentsHorizontal(text string, fontID int32, features fontFeatureInputs, startX, letterSpacing, wordSpacing float64) []glyphExtent {
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
