package layout

import "louis14/pkg/css"

// InlineItemResultRubyColumn is the per-column payload attached to the
// single InlineItemResult that the LineBreaker emits for a ruby column.
// It carries the base sub-LineInfo and one annotation sub-LineInfo per
// `<rt>` so the inline layout algorithm can lay out the base on the
// main baseline and stack annotations above/below.
//
// Ported from Blink's InlineItemResult::RubyColumn — declared in its
// own header `core/layout/inline/inline_item_result_ruby_column.h`
// rather than alongside the other ruby utilities in `ruby_utils.h`.
// Vetted against Chromium main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Phase 2 supports the single-level case (`[]` base, `[1]` over /
// `[-1]` under). The multi-level RubyLevel path model (`[1,-1]`,
// `[-2]`, …) is Phase 11.
type InlineItemResultRubyColumn struct {
	// BaseLine is the sub-LineInfo for the column's base content.
	BaseLine *LineInfo

	// AnnotationLines holds one sub-LineInfo per `<rt>` within the
	// column. Empty for a placeholder-only column (a `<ruby>` with no
	// `<rt>`), which the LineBreaker dispatch skips outright (mirrors
	// `line_breaker.cc:1082-1093`).
	AnnotationLines []*LineInfo

	// StartIndex is the index into InlineItemsData.Items of the
	// InlineItemOpenRubyColumn that opened this column. Used by
	// downstream consumers (UpdateRubyColumnInlinePositions,
	// RubyBlockPositionCalculator) to reconstruct column ordering.
	StartIndex int

	// InlineSize is the column's inline contribution to the line —
	// `max(BaseLine.Width, AnnotationLines[i].Width...)` (mirrors
	// `ruby_size = MaxLineWidth(base, annotations)` in
	// `line_breaker.cc:3278-3449` HandleRuby).
	InlineSize float64

	// RubyAlign is the effective ruby-align value for this column.
	// CSS Ruby 1 §7: ruby-align is inherited, so it may be set on the
	// <ruby> element (the column-open item's style) or on the <rb>/<rt>
	// elements within the column. handleRuby resolves this by checking
	// the column-open item style first (the <ruby> parent), then falling
	// back to the first explicit ruby-align found in the base sub-line's
	// item styles (the <rb> case).
	//
	// Mirrors Blink's ApplyRubyAlign path in ruby_utils.cc @ 4883d11fef.
	RubyAlign css.RubyAlign
}
