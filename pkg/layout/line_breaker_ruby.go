package layout

import "louis14/pkg/css"

// Sub-line breaking and the LineBreaker.handleRuby column handler.
// Together these implement Phase 2 of plan-css-ruby.md.
//
// Vetted against Chromium main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.

// resolveRubyAlign returns the effective ruby-align for a ruby column.
//
// CSS Ruby 1 §7: ruby-align is an inherited property. It may be set on
// the <ruby> element (carried by columnOpenItem.Style) or on the <rb>
// child (visible as OpenTag items in the base sub-line). The column-open
// item's style takes priority; if it carries the initial value
// (space-around), we scan the base sub-line for an OpenTag whose style
// has an explicit ruby-align.
//
// Mirrors the lookup Blink performs in ApplyRubyAlign
// (core/layout/inline/ruby_utils.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func resolveRubyAlign(columnOpenItem *InlineItem, baseLine *LineInfo) css.RubyAlign {
	if columnOpenItem != nil && columnOpenItem.Style != nil {
		ra := columnOpenItem.Style.GetRubyAlign()
		if ra != css.RubyAlignSpaceAround {
			return ra
		}
	}
	// Column-open item has the initial value — check base sub-line items
	// for a <rb> element carrying an explicit ruby-align.
	if baseLine != nil {
		for _, r := range baseLine.Results {
			if r.Item == nil || r.Item.Type != InlineItemOpenTag || r.Item.Style == nil {
				continue
			}
			ra := r.Item.Style.GetRubyAlign()
			if ra != css.RubyAlignSpaceAround {
				return ra
			}
		}
	}
	return css.RubyAlignSpaceAround
}

// CreateSubLineInfo runs line breaking over a sub-range of items in
// LineBreakerMaxContent mode and returns a freshly-built LineInfo for
// that range. Used by handleRuby to build the base and annotation
// sub-LineInfos of a ruby column without affecting the outer
// LineBreaker's state.
//
// The sub-range is `items[startIdx:endIdx]`; the same TextContent /
// RuneLevels / ParagraphLevels are shared (so existing offsets in the
// items remain valid). The sub-LineBreaker uses MaxContent mode so
// sub-lines never wrap — ruby columns are atomic inlines from the
// outer line's perspective.
//
// Mirrors Blink's `CreateSubLineInfo` near `LineBreaker::HandleRuby`
// in `core/layout/inline/line_breaker.cc`.
func (lb *LineBreaker) CreateSubLineInfo(startIdx, endIdx int) *LineInfo {
	if startIdx < 0 || endIdx > len(lb.itemsData.Items) || startIdx >= endIdx {
		return &LineInfo{}
	}
	subData := &InlineItemsData{
		TextContent:     lb.itemsData.TextContent,
		Items:           lb.itemsData.Items[startIdx:endIdx],
		RuneLevels:      lb.itemsData.RuneLevels,
		ParagraphLevels: lb.itemsData.ParagraphLevels,
	}
	subLB := NewLineBreaker(subData, lb.ctx, lb.space, lb.fonts, LineBreakerMaxContent)
	sub := &LineInfo{}
	subLB.NextLine(sub)
	return sub
}

// capTextCombineSubLineWidth caps the width of a sub-line to 1em if it
// contains any items with text-combine-upright: all in a vertical writing mode.
// Mirrors Blink's inline_layout_algorithm.cc:~1270 constraint.
func capTextCombineSubLineWidth(subLine *LineInfo, columnOpenStyle *css.Style, isVerticalMode bool) {
	if !isVerticalMode || subLine == nil {
		return
	}
	// Check if any result item has text-combine-upright: all.
	hasTextCombine := false
	for _, r := range subLine.Results {
		if r.Item != nil && r.Item.Style != nil && r.Item.Style.GetTextCombineUpright() {
			hasTextCombine = true
			break
		}
	}
	if !hasTextCombine {
		return
	}
	// Cap the width to 1em (parent font-size).
	oneEm := columnOpenStyle.GetFontSize()
	if subLine.Width > oneEm {
		subLine.Width = oneEm
	}
}

// handleRuby processes the InlineItemOpenRubyColumn currently at
// lb.currentItemIndex. It builds base + annotation sub-LineInfos,
// computes the column's inline size (= max of base/annotation
// widths), appends a single InlineItemResult of kind RubyColumn to
// the outer line, and advances currentItemIndex past the matching
// InlineItemCloseRubyColumn.
//
// Returns true if the column doesn't fit and the line should end
// before it (then the column is re-processed on the next line).
//
// Mirrors Blink's `LineBreaker::HandleRuby` at
// `core/layout/inline/line_breaker.cc:3278-3449`. Phase 2 implements
// the fits-in-line path; the proportional-break + `RubyBreakTokenData`
// branch at `:3372-3438` is deferred to Phase 7.
func (lb *LineBreaker) handleRuby(item *InlineItem, line *LineInfo) bool {
	idx := ParseRubyInInlineItems(lb.itemsData.Items, lb.currentItemIndex)

	// Build the base sub-LineInfo over (column-open, base-end).
	// The leading InlineItemRubyLinePlaceholder is silently skipped
	// by the sub-LineBreaker (no dispatch case = no-op + advance).
	baseLine := lb.CreateSubLineInfo(idx.ColumnStart+1, idx.BaseEnd)
	baseLine.IsRubyBase = true

	var annotationLines []*LineInfo
	if idx.HasAnnotation() {
		annoLine := lb.CreateSubLineInfo(idx.AnnotationStart+1, idx.ColumnEnd)
		annoLine.IsRubyText = true
		annotationLines = append(annotationLines, annoLine)
	}

	// Cap text-combine sub-line widths to 1em in vertical mode.
	// Mirrors Blink's inline_layout_algorithm.cc:~1270.
	isVerticalMode := lb.space.WritingDirection.UsesCentralBaseline()
	if item.Style != nil {
		capTextCombineSubLineWidth(baseLine, item.Style, isVerticalMode)
		for _, a := range annotationLines {
			capTextCombineSubLineWidth(a, item.Style, isVerticalMode)
		}
	}

	// rubySize = max(base.Width, annotations[i].Width...).
	rubySize := baseLine.Width
	for _, a := range annotationLines {
		if a.Width > rubySize {
			rubySize = a.Width
		}
	}

	// Check fit and break-before if needed. Mirrors the atomic-inline
	// fit check in handleAtomicInline. Phase 2 doesn't yet attempt
	// proportional-break inside the column (Phase 7). The
	// `mode != LineBreakerMaxContent` guard is for the OUTER mode:
	// the sub-LineBreakers built by CreateSubLineInfo always run in
	// MaxContent so they never reach this branch — they're a separate
	// LineBreaker instance, not this one.
	remaining := lb.availableWidth - lb.position
	if rubySize > remaining && len(line.Results) > 0 && lb.mode != LineBreakerMaxContent {
		return true
	}

	// Resolve ruby-align: check the column-open item style (the <ruby>
	// element) first, then scan the base sub-line items for a <rb> element
	// that carries an explicit ruby-align. CSS Ruby §7: the property is
	// inherited, so it may live on <ruby> or on the <rb> child directly.
	//
	// Mirrors Blink's ApplyRubyAlign lookup path in ruby_utils.cc
	// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	rubyAlign := resolveRubyAlign(item, baseLine)

	column := &InlineItemResultRubyColumn{
		BaseLine:        baseLine,
		AnnotationLines: annotationLines,
		StartIndex:      lb.currentItemIndex,
		InlineSize:      rubySize,
		RubyAlign:       rubyAlign,
	}

	line.Results = append(line.Results, InlineItemResult{
		Item:          item,
		ItemIndex:     lb.currentItemIndex,
		InlineSize:    rubySize,
		RubyColumn:    column,
		CanBreakAfter: true,
	})
	line.RubyColumns = append(line.RubyColumns, column)
	lb.position += rubySize
	line.Width = lb.position

	// Advance currentItemIndex to the CloseRubyColumn. The outer
	// NextLine loop will increment past it.
	lb.currentItemIndex = idx.ColumnEnd
	return false
}
