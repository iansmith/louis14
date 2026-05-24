package layout

// Sub-line breaking and the LineBreaker.handleRuby column handler.
// Together these implement Phase 2 of plan-css-ruby.md.
//
// Vetted against Chromium main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.

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

	column := &InlineItemResultRubyColumn{
		BaseLine:        baseLine,
		AnnotationLines: annotationLines,
		StartIndex:      lb.currentItemIndex,
		InlineSize:      rubySize,
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

