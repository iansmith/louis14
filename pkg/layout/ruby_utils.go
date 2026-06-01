package layout

import "louis14/pkg/text"

// baseFontAscentFromSubLine returns the maximum natural font ascent across
// all text items in a sub-line, EXCLUDING the line-height half-leading.
//
// Sister to `computeLineMetricsEx` in `inline_layout.go`: same first four
// arg positions, drops the strut `parentStyle` because the strut doesn't
// apply to ruby sub-lines.
//
// Mirrors Blink's `ComputeLogicalLineEmHeight(base_line_items,
// BaseIndexList())` used at `core/layout/inline/ruby_utils.cc:981 @
// 574216cbb0c2b86a39c1d41ad85b2891a050b44c` — the base-font reference
// against which ruby annotation block-axis position is measured.
//
// Unlike `computeLineMetricsEx`, this does NOT include line-height
// leading. That distinction is load-bearing for ruby annotation Y under
// tall line-heights: the annotation sits adjacent to the base text's
// natural glyph cell, NOT adjacent to the line-height-inflated bbox.
// Using sub-line ascent (with leading) here would compound with the
// pre-LOU-161 `+= annoAsc` line-growth pattern to pin annotations at
// line-box top. See LOU-161 findings.md "Ruby annotation Y-positioning
// bug" section.
func baseFontAscentFromSubLine(
	line *LineInfo,
	wdm WritingDirectionMode,
	fonts text.FontConfig,
	centralBaseline bool,
) float64 {
	if line == nil {
		return 0
	}
	sidewaysVLR := needsSidewaysVLRBaselineSwap(wdm, centralBaseline)
	var maxAscent float64
	for _, r := range line.Results {
		if r.Item == nil || r.Item.Type != InlineItemText || r.Item.Style == nil {
			continue
		}
		// CSS Writing Modes 3 §9.1.1: text-combine-upright: all in a vertical
		// writing mode renders the combined text as a 1em upright unit. It
		// behaves like an orthogonal atomic inline from the ruby block-axis
		// perspective — it does not contribute to the base font ascent used
		// to place over-side annotations. Mirrors the zero contribution of a
		// LayoutResult=nil atomic inline in computeLineMetricsEx at
		// core/layout/inline/inline_layout_algorithm.cc @
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		if wdm.IsVertical() && r.Item.Style.GetTextCombineUpright() {
			continue
		}
		fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
		var asc float64
		if centralBaseline {
			asc = fontSize / 2
		} else {
			fontPath := resolveFontPath(r.Item.Style, fonts)
			// CSS Fonts 4 §6.1: base font ascent for annotation placement uses the
			// native (non-overridden) ascent. The override grows the line box's
			// strut ascent (maxAscent), and annotationBlockTop is computed relative
			// to that grown maxAscent. Using overridden baseFontAsc here would
			// double-count the override delta and push the annotation into the base.
			asc = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, nil)
		}
		if asc > maxAscent {
			maxAscent = asc
		}
	}
	return maxAscent
}

// annotationEmHeightFromSubLine returns the maximum font typo ascent and
// descent across all text items in a ruby annotation sub-line, EXCLUDING
// CSS line-height half-leading and EXCLUDING the integer rounding that
// `computeLineMetricsEx` applies via `alignmentAscentFromFont`.
//
// Mirrors Blink's `ComputeEmHeight(line_item)` at
// `core/layout/inline/ruby_utils.cc:78-126 @
// 574216cbb0c2b86a39c1d41ad85b2891a050b44c` — the em-height-only
// metric Blink uses to size annotation sub-line contributions and to
// position the annotation baseline relative to the line's block origin.
//
// Why this is distinct from `computeLineMetricsEx`:
//  1. No half-leading. computeLineMetricsEx adds `(lineHt − (asc+desc))/2`
//     to both ascent and descent for `line-height: normal`. Blink's
//     annotation positioning uses font em-height only; the leading is
//     a property of the OUTER line's block metrics.
//  2. No integer rounding. computeLineMetricsEx → alignmentAscentFromFont
//     → FontAscentFromFont applies `math.Round`. Blink's annotation
//     positioning uses sub-pixel `FixedAscent`/`FixedDescent`; the
//     rounding to integer ascent (`int_ascent_` via `lroundf`) happens
//     once at PAINT time. Layout uses the sub-pixel value.
//
// Both differences contribute to the LOU-161 emphasis/annotation
// alignment gap; see findings.md "Subpixel rounding investigation".
func annotationEmHeightFromSubLine(
	line *LineInfo,
	wdm WritingDirectionMode,
	fonts text.FontConfig,
	centralBaseline bool,
) (ascent, descent float64) {
	if line == nil {
		return 0, 0
	}
	sidewaysVLR := needsSidewaysVLRBaselineSwap(wdm, centralBaseline)
	for _, r := range line.Results {
		if r.Item == nil || r.Item.Type != InlineItemText || r.Item.Style == nil {
			continue
		}
		// CSS Writing Modes 3 §9.1.1: text-combine-upright: all in a vertical
		// writing mode renders as an orthogonal 1em upright unit. It does not
		// contribute to the annotation em-height used for over-side block
		// positioning; its block contribution is like a nil LayoutResult
		// atomic inline (zero). Mirrors the zero contribution of
		// LayoutResult=nil atomic inlines in ruby_utils.cc ComputeEmHeight @
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		if wdm.IsVertical() && r.Item.Style.GetTextCombineUpright() {
			continue
		}
		fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
		var a, d float64
		if centralBaseline {
			a = fontSize / 2
			d = fontSize / 2
		} else {
			fontPath := resolveFontPath(r.Item.Style, fonts)
			a = text.FontTypoAscentFromFont(fontSize, fontPath, fonts.Registry)
			d = text.FontTypoDescentFromFont(fontSize, fontPath, fonts.Registry)
			if sidewaysVLR {
				a, d = d, a
			}
		}
		if a > ascent {
			ascent = a
		}
		if d > descent {
			descent = d
		}
	}
	return ascent, descent
}

// RubyBlockPositionCalculator stacks annotation sub-lines above/below
// the base baseline of a ruby column on a line. Phase 2 supports the
// single-level case only — at most one annotation per column, all
// annotations on the "over" side (above the base). The full
// multi-level RubyLevel path model (`[1,-1]`, `[-2]`, ...) is Phase 11.
//
// Mirrors Blink's `RubyBlockPositionCalculator` in
// `core/layout/inline/ruby_utils.{h,cc}` (header `:128-244`, impls
// `:775-1037` @ 4883d11fef).
type RubyBlockPositionCalculator struct {
	// annotationAscent is the maximum block-axis height contributed
	// by any annotation sub-line on the line. Computed by PlaceLines;
	// returned by AnnotationMetrics so the outer line's height grows
	// to contain all annotations (mirrors
	// inline_layout_algorithm.cc:396-418 SetAnnotationBlockStartAdjustment).
	annotationAscent float64
}

// PlaceLines computes the block-axis offset of each annotation
// sub-line relative to the base baseline, given the outer line's
// font config and writing-direction context. Phase 2: all annotations
// stack ABOVE the base (default `ruby-position: over`); the
// per-annotation offset is the negative of the annotation's own
// height (sub-line ascent + descent), so the annotation sits with
// its baseline above the base baseline by its descent and its top
// extends further by its ascent.
//
// The maximum annotation height across the line is recorded for
// AnnotationMetrics so the outer line can grow its ascent
// accordingly.
//
// Phase 11 will replace `columns` with a `RubyLevel` partition built
// from Blink's GroupLines pre-pass; for Phase 2 we always treat the
// whole line as one over-level group.
func (rbpc *RubyBlockPositionCalculator) PlaceLines(
	columns []*InlineItemResultRubyColumn,
	wdm WritingDirectionMode,
	fonts text.FontConfig,
	centralBaseline bool,
) {
	var maxAnno float64
	for _, col := range columns {
		for _, anno := range col.AnnotationLines {
			a, d := annotationEmHeightFromSubLine(anno, wdm, fonts, centralBaseline)
			if h := a + d; h > maxAnno {
				maxAnno = h
			}
		}
	}
	rbpc.annotationAscent = maxAnno
}

// AnnotationMetrics returns the additional ascent (above the base
// baseline) and descent (below) that the line box needs to contain
// all annotations on the line. Phase 2 always returns the over-side
// contribution as ascent and zero descent — `ruby-position: under`
// support is Phase 11.
//
// Mirrors Blink's `AnnotationMetrics` accessor on
// `RubyBlockPositionCalculator`.
func (rbpc *RubyBlockPositionCalculator) AnnotationMetrics() (ascent, descent float64) {
	return rbpc.annotationAscent, 0
}

// UpdateRubyColumnInlinePositions resolves each ruby column's
// inline-axis position on the outer line (the position where the
// base sub-line and annotation sub-lines will be laid out).
//
// Phase 2 keeps this simple: the inline position is the running sum
// of preceding non-ruby-column results' InlineSize on the same line,
// computed up front so M6's createLineBoxEx can pick up per-column
// offsets without re-traversing. Returned slice is parallel to
// line.RubyColumns.
//
// Mirrors Blink's `UpdateRubyColumnInlinePositions` at
// `core/layout/inline/ruby_utils.cc:720-748` @ 4883d11fef. Phase 2's
// version is a straight running-sum walk; Blink's version is the
// same shape (the more elaborate overhang adjustments belong to
// Phase 5's `ruby-align`/`ruby-overhang` work).
func UpdateRubyColumnInlinePositions(line *LineInfo) []float64 {
	if len(line.RubyColumns) == 0 {
		return nil
	}
	positions := make([]float64, 0, len(line.RubyColumns))
	var inlinePos float64
	for i := range line.Results {
		r := &line.Results[i]
		if r.RubyColumn != nil {
			positions = append(positions, inlinePos)
		}
		inlinePos += r.InlineSize
	}
	return positions
}

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

// HasAnnotation reports whether the column has an annotation sub-line.
//
// Sentinel encoding: `AnnotationStart == ColumnEnd` is the "no annotation"
// sentinel; when an annotation exists, `AnnotationStart` is set to the
// index of the annotation placeholder and is strictly less than
// `ColumnEnd`. A bare `<ruby>X</ruby>` with no `<rt>` parses with
// AnnotationStart == ColumnEnd, leaving the annotation sub-LineInfo
// empty in handleRuby.
//
// Mirrors Blink's sentinel-encoding pattern for
// RubyItemIndexes.annotation_start at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Blink initialises the
// field to `kNotFound` at core/layout/inline/ruby_utils.cc:151
// (`RubyItemIndexes indexes = {start_item_index, kNotFound,
// kNotFound, kNotFound};` inside ParseRubyInInlineItems at
// :147-:176) and assigns it only when an annotation kOpenTag is
// encountered at :169 inside the `IsInlineRubyText()` branch. Blink's
// RubyItemIndexes has no `HasAnnotation` accessor; the equivalent
// "no annotation" test is the type check `Items()[base_end_index]->
// Type() == InlineItem::kCloseRubyColumn` at core/layout/inline/
// line_breaker.cc:3292 (pinned SHA). Blink uses `kNotFound` as the
// sentinel; louis14 uses `ColumnEnd`; both encode "placeholder was
// not emitted".
//
// IMPORTANT: this predicate tests whether the annotation EXISTS
// (placeholder emitted), NOT whether the annotation has CONTENT.
// A degenerate `[Open, BasePH, AnnoPH, Close]` shape — annotation
// placeholder immediately before the column close — returns true
// (the annotation exists; it just has zero content). Do NOT tighten
// to `AnnotationStart+1 < ColumnEnd` — that would silently diverge
// from Blink's `annotation_start != kNotFound` semantics. See
// ruby_utils_test.go::TestHasAnnotation_AdjacentPlaceholderBeforeClose_HasAnnotationTrue
// for the invariant lock.
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
