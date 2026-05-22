package layout

// RubyItemIndexes pinpoints the four item-stream landmarks of a ruby
// column: the column-open, the base/annotation boundary, the start of
// annotation content, and the column-close. The LineBreaker uses
// these ranges to build base and annotation sub-LineInfos when
// processing a column (see LineBreaker.handleRuby in line_breaker.go).
//
// Mirrors Blink's `RubyItemIndexes` in
// `core/layout/inline/ruby_utils.cc:147-176`. Vetted against Chromium
// main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
type RubyItemIndexes struct {
	// ColumnStart is the index of the InlineItemOpenRubyColumn that
	// opens this column.
	ColumnStart int
	// BaseEnd is the index just past the last base item in the
	// column. For a column with an annotation, BaseEnd ==
	// AnnotationStart and points at the InlineItemRubyLinePlaceholder
	// that opens the annotation sub-line. For a column without an
	// annotation, BaseEnd == ColumnEnd.
	BaseEnd int
	// AnnotationStart is the index of the InlineItemRubyLinePlaceholder
	// that marks the start of the annotation sub-line, or equals
	// ColumnEnd if the column has no annotation.
	AnnotationStart int
	// ColumnEnd is the index of the InlineItemCloseRubyColumn that
	// closes this column.
	ColumnEnd int
}

// HasAnnotation reports whether the column has an annotation
// sub-line — i.e. one or more items between the annotation
// placeholder and the column close. A bare `<ruby>X</ruby>` with no
// `<rt>` parses with AnnotationStart == ColumnEnd, leaving the
// annotation sub-LineInfo empty in handleRuby.
func (r RubyItemIndexes) HasAnnotation() bool {
	return r.AnnotationStart < r.ColumnEnd
}

// ParseRubyInInlineItems walks the item stream starting at `start`
// (which must point at an InlineItemOpenRubyColumn) and returns the
// four landmark indexes that delimit the column's base and annotation
// content. The walk recurses across the OpenTag/CloseTag pairs of
// `<rb>`/`<rbc>`/`<rtc>` and the `<rt>` element itself; it never
// looks past the matching CloseRubyColumn.
//
// Phase 2 expects exactly one base sub-line and at most one
// annotation sub-line per column (the multi-`<rt>` and nested-ruby
// cases land in later phases). The parser is structured to recurse
// into nested columns for forward compatibility — see Blink's own
// recursive case at `ruby_utils.cc:169-172` — but Phase 2 callers
// will never trigger it because collectInlinesRecursive doesn't yet
// emit nested columns.
//
// Mirrors Blink's `ParseRubyInInlineItems(items, start)` at
// `core/layout/inline/ruby_utils.cc:147-176`. Vetted against Chromium
// main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func ParseRubyInInlineItems(items []*InlineItem, start int) RubyItemIndexes {
	if start < 0 || start >= len(items) ||
		items[start].Type != InlineItemOpenRubyColumn {
		// Defensive: not a column open. Return a zero range so
		// downstream code skips this without further item access.
		return RubyItemIndexes{
			ColumnStart:     start,
			BaseEnd:         start,
			AnnotationStart: start,
			ColumnEnd:       start,
		}
	}

	idx := RubyItemIndexes{ColumnStart: start}
	// The first placeholder (immediately after the column open) marks
	// the start of the base sub-line. We've already consumed it by
	// starting the scan at start+2 below.
	sawAnnotationPlaceholder := false
	depth := 1
	i := start + 1
	for i < len(items) {
		switch items[i].Type {
		case InlineItemOpenRubyColumn:
			// Nested column — recurse to find its close, then resume
			// after it. Phase 2 doesn't produce these, but the parser
			// handles them so Phase 3+ work doesn't need to retouch
			// this file. Mirrors `ruby_utils.cc:169-172`.
			inner := ParseRubyInInlineItems(items, i)
			i = inner.ColumnEnd + 1
			continue
		case InlineItemCloseRubyColumn:
			depth--
			if depth == 0 {
				idx.ColumnEnd = i
				if !sawAnnotationPlaceholder {
					// No annotation in this column — base ends at the
					// column close, and AnnotationStart collapses to
					// the same index so HasAnnotation() == false.
					idx.BaseEnd = i
					idx.AnnotationStart = i
				}
				return idx
			}
		case InlineItemRubyLinePlaceholder:
			// The first placeholder (right after the column open) is
			// the base anchor; the second is the annotation anchor.
			// Skipping start+1 in the loop's initial step already
			// consumed the first one, so any placeholder we see here
			// at depth 1 is the annotation boundary.
			if !sawAnnotationPlaceholder && i > start+1 {
				idx.BaseEnd = i
				idx.AnnotationStart = i
				sawAnnotationPlaceholder = true
			}
		}
		i++
	}

	// Unclosed column (malformed item stream). Return what we have
	// with ColumnEnd pinned to len(items) so callers can advance past
	// it without infinite-looping.
	idx.ColumnEnd = len(items)
	if !sawAnnotationPlaceholder {
		idx.BaseEnd = idx.ColumnEnd
		idx.AnnotationStart = idx.ColumnEnd
	}
	return idx
}
