package layout

import (
	"math"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
)

// MulticolLayoutAlgorithm implements CSS Multi-column Layout Module Level 1.
// Mirrors Blink's ColumnLayoutAlgorithm (column_layout_algorithm.{h,cc}).
//
// Phase 12a: fragmentation infrastructure — outer stretch / inner column loop,
// MinimalSpaceShortage feedback, inline content fragmentation.
// Phase 12b: ColumnSpannerPath detection + MulticolPartWalker-style walk loop,
// LayoutSpanner, re-balance after spanner, column-fill:auto + spanner flip.
// Phase 12f: column-height / column-wrap (CSS Multicol Level 2 §4.2) — row
// block-size override, row-wrap loop, intrinsic-block-size top-off,
// MulticolBreakTokenData.consumed_row_block_size row-phase carry across outer
// fragmentainers.
type MulticolLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace

	// Phase 12f row-wrap state. Populated in Layout() before the walker loop.
	// remainingContentBlockSize is Blink's remaining_content_block_size_
	// (cla.cc:313): the content-box block size minus any size consumed from an
	// outer fragmentation context, clamped to zero; Indefinite for an auto
	// multicol height. Used by rowHeight() when column-height is auto.
	remainingContentBlockSize float64
	// consumedRowBlockSize carries the MulticolBreakTokenData row phase across
	// an outer fragmentainer boundary (cla.cc:2087). Zero when this is the
	// first outer column of the multicol.
	consumedRowBlockSize float64
	// rowGapSize is the block-axis gap between column rows in a
	// `column-wrap: wrap` multicol (CSS Multicol L2 §4.2.6). Read from
	// the element's `row-gap` property via GetRowGapMulticol. Resolved
	// once in Layout(); `normal` computes to 1em. Matches Blink's
	// row_gap_ in column_layout_algorithm.cc used by
	// RemainingRowHeightAtOffset / row-wrap loop (cla.cc:789–836).
	rowGapSize float64
}

// NewMulticolLayoutAlgorithm creates a multicol layout algorithm.
func NewMulticolLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *MulticolLayoutAlgorithm {
	return &MulticolLayoutAlgorithm{
		ctx:   ctx,
		node:  node,
		style: node.Style(),
		space: space,
	}
}

// isMulticolContainer returns true if the style triggers multi-column layout.
// CSS Multicol L1: column-count or column-width establishes multicol.
// CSS Multicol L2 §4.2: a non-auto column-height ALSO establishes multicol
// (see WPT column-height-012 "Non-auto column-height should turn it into
// a multicol"). Mirrors Blink's ComputedStyle::HasMultiColumn which checks
// all three properties.
func isMulticolContainer(style *css.Style) bool {
	if style == nil {
		return false
	}
	return style.GetColumnCount() > 0 || style.GetColumnWidth() > 0 ||
		style.GetColumnHeight() >= 0
}

// ResolveColumnCountForPaint is a public wrapper around resolveColumnCount
// for use by the paint layer builder.
func ResolveColumnCountForPaint(availableInline float64, colCount int, colWidth float64, gap float64) (int, float64) {
	return resolveColumnCount(availableInline, colCount, colWidth, gap)
}

// resolveColumnCount implements CSS Multicol §3.4 — the pseudo-algorithm
// for resolving column-count and column-width into used values.
// Returns (usedColumnCount, usedColumnWidth).
func resolveColumnCount(availableInline float64, colCount int, colWidth float64, gap float64) (int, float64) {
	var N int
	if colWidth == 0 {
		N = colCount
	} else if colCount == 0 {
		N = int(math.Max(1, math.Floor((availableInline+gap)/(colWidth+gap))))
	} else {
		fromWidth := int(math.Floor((availableInline + gap) / (colWidth + gap)))
		if fromWidth < 1 {
			fromWidth = 1
		}
		N = colCount
		if fromWidth < N {
			N = fromWidth
		}
	}
	if N < 1 {
		N = 1
	}
	W := math.Max(0, (availableInline+gap)/float64(N)-gap)
	return N, W
}

// hasAutoColumnHeight returns true when the CSS column-height is auto (or
// unset). Mirrors Blink's Style().HasAutoColumnHeight().
func (mla *MulticolLayoutAlgorithm) hasAutoColumnHeight() bool {
	return mla.style.GetColumnHeight() < 0
}

// hasRowHeight mirrors Blink's HasRowHeight() (column_layout_algorithm.h:258).
// True when column-height is non-auto, or the multicol has a definite remaining
// content block-size (inherited from a definite block-size on the container).
func (mla *MulticolLayoutAlgorithm) hasRowHeight() bool {
	return !mla.hasAutoColumnHeight() ||
		(mla.remainingContentBlockSize != Indefinite && mla.remainingContentBlockSize > 0)
}

// rowHeight mirrors Blink's RowHeight() (column_layout_algorithm.h:267). When
// column-height is non-auto it returns the CSS value; otherwise the remaining
// content block-size (which the caller guarantees is positive via hasRowHeight).
func (mla *MulticolLayoutAlgorithm) rowHeight() float64 {
	if !mla.hasAutoColumnHeight() {
		return mla.style.GetColumnHeight()
	}
	return mla.remainingContentBlockSize
}

// rowStride is row_height + row_gap_size (cla.cc:2122).
func (mla *MulticolLayoutAlgorithm) rowStride() float64 {
	return mla.rowHeight() + mla.rowGapSize
}

// shouldWrapColumns mirrors Blink's ShouldWrapColumns() (column_layout_algorithm.h:221).
// Wrap is forced by column-wrap:wrap, suppressed by column-wrap:nowrap, and for
// column-wrap:auto follows HasAutoColumnHeight (wrap when column-height is set).
func (mla *MulticolLayoutAlgorithm) shouldWrapColumns() bool {
	switch mla.style.GetColumnWrap() {
	case "wrap":
		return true
	case "nowrap":
		return false
	}
	return !mla.hasAutoColumnHeight()
}

// offsetInCurrentRow mirrors Blink's OffsetInCurrentRow (cla.cc:2122). Returns
// the block-axis offset within the current column row. At exact row boundaries
// the result is zero — so remainingRowHeightAtOffset at such a boundary equals
// the full row height (a fresh row is available).
func (mla *MulticolLayoutAlgorithm) offsetInCurrentRow(lineOffset float64) float64 {
	stride := mla.rowStride()
	if stride <= 0 {
		return 0
	}
	// CurrentContentBlockOffset(line_offset) = line_offset - border_padding.start.
	// lineOffset is already content-box relative in our builder (columns add at
	// content-box offset, border/padding applied separately), so no subtraction.
	cbo := lineOffset + mla.consumedRowBlockSize
	return math.Mod(cbo, stride)
}

// remainingRowHeightAtOffset mirrors Blink (cla.cc:2141). RowHeight() minus the
// offset within the current row.
func (mla *MulticolLayoutAlgorithm) remainingRowHeightAtOffset(lineOffset float64) float64 {
	return mla.rowHeight() - mla.offsetInCurrentRow(lineOffset)
}

// offsetToNextRow mirrors Blink (cla.cc:2146). When hasRowHeight and we are
// past a row start, returns the distance to the row end plus row_gap_size. At
// an exact row boundary (offset_within_row == 0) returns only row_gap_size.
func (mla *MulticolLayoutAlgorithm) offsetToNextRow(lineOffset float64) float64 {
	var offsetToRowEnd float64
	if mla.hasRowHeight() {
		within := mla.offsetInCurrentRow(lineOffset)
		if within > 0 {
			offsetToRowEnd = mla.rowHeight() - within
		}
	}
	return offsetToRowEnd + mla.rowGapSize
}

// Layout performs multi-column layout and returns the result.
// Mirrors Blink's ColumnLayoutAlgorithm::Layout().
func (mla *MulticolLayoutAlgorithm) Layout() *LayoutResult {
	wdm := mla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(mla.ctx, mla.node, mla.style, wdm, mla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(mla.node)

	contentInlineSize := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if contentInlineSize < 0 {
		contentInlineSize = 0
	}

	// Resolve column parameters.
	colCount := mla.style.GetColumnCount()
	colWidth := mla.style.GetColumnWidth()
	gap := mla.style.GetColumnGapMulticol()
	numCols, usedColWidth := resolveColumnCount(contentInlineSize, colCount, colWidth, gap)

	// Container explicit block-size (from CSS height).
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
		}
	}

	// Phase 12f: remaining_content_block_size_ (Blink cla.cc:313). The content
	// block-size minus what the outer fragmentation context has already consumed
	// for us, clamped to zero. Indefinite when the multicol has an auto height.
	// Used by hasRowHeight()/rowHeight() when column-height is auto.
	mla.remainingContentBlockSize = Indefinite
	if hasExplicitBlock {
		mla.remainingContentBlockSize = explicitBlockSize
		if mla.space.BreakToken != nil &&
			mla.space.HasBlockFragmentation &&
			mla.space.FragmentainerBlockSize != Indefinite &&
			!mla.space.IsInitialColumnBalancingPass {
			mla.remainingContentBlockSize -= mla.space.BreakToken.ConsumedBlockSize
			if mla.remainingContentBlockSize < 0 {
				mla.remainingContentBlockSize = 0
			}
		}
	}
	// Phase 12f: MulticolBreakTokenData row phase carry across outer
	// fragmentainers (cla.cc:2087). Default 0 on fresh layout; wired from the
	// break token when 12f.6 lands.
	mla.consumedRowBlockSize = 0

	// F3 (2026-04-24): row-gap between column rows when column-wrap:wrap.
	// Resolve once per layout; `normal` computes to 1em. Matches Blink's
	// row_gap_ in column_layout_algorithm.cc.
	mla.rowGapSize = mla.style.GetRowGapMulticol()

	// Phase 12e: max-height as a column-height constraint when the multicol's
	// own block-size is auto. With column-fill:auto and an auto multicol height,
	// max-height bounds each column box's height (Blink behavior; CSS Multi-
	// column L1 §7.1 "fill columns sequentially"; WPT columnfill-auto-max-height-*
	// + multicol-fill-auto-block-children-003).
	//
	// We only consult max-height when hasExplicitBlock is false. When the
	// multicol HAS an explicit (resolved) height, that height is already clamped
	// by max/min via CalculateInitialFragmentGeometry — applying max-height
	// again here would incorrectly override min-height (which can override
	// max per CSS 2.1 §10.7's "max applies, then min" rule).
	effectiveMaxBlockSize := Indefinite
	if !hasExplicitBlock {
		if maxBS, ok := ResolveMaxBlockSize(mla.style, wdm, mla.space, geom); ok {
			effectiveMaxBlockSize = maxBS
		}
	}

	// column-fill: balance forces balanced column distribution.
	// Also forced when nested inside an outer fragmentation context whose
	// fragmentainer block-size isn't known yet (Blink cla.cc:1025) — this is
	// how the outer's initial balancing pass asks inner multicols to balance.
	// Equivalent to Blink's HasBlockFragmentation() && !HasKnownFragmentainerBlockSize().
	columnFill := mla.style.GetColumnFill()
	balanceColumns := columnFill == "balance" || columnFill == "" ||
		(mla.space.HasBlockFragmentation && mla.space.FragmentainerBlockSize == Indefinite)

	// Anonymous content node wrapping all multicol children. Using an anonymous
	// style (no borders/padding/explicit dimensions) so block layout computes
	// the column content area from the column constraint space, not from the
	// multicol container's own CSS dimensions.
	// Mirrors Blink: BlockLayoutAlgorithm runs on a kColumnBox synthetic node.
	//
	// Group consecutive inline-level and text children into anonymous block
	// nodes so the block layout algorithm's BFC path routes them to IFC.
	// Without this, mixed inline+block content (e.g. spans + h4 + text) would
	// have each span processed as a separate block child, preventing them from
	// forming a shared IFC. Mirrors CSS 2.1 §9.2.1.1 anonymous block generation.
	anonStyle := css.NewAnonymousBlockStyle(mla.style)
	// Use cached grouped children so that anonymous block wrappers have the
	// same pointer identity across multiple Layout() calls on the same node.
	// Break tokens store node pointers for resume; without this cache a fresh
	// groupInlineChildrenForMulticol call would create new anonymous wrappers
	// whose pointers don't match the stored ones, causing resumeChildIdx=-1.
	if mla.node.groupedChildrenCache == nil {
		mla.node.groupedChildrenCache = groupInlineChildrenForMulticol(mla.node.Children(), mla.style)
	}
	contentNode := &LayoutInputNode{
		style:       anonStyle,
		children:    mla.node.groupedChildrenCache,
		isAnonymous: true,
	}

	blockCursor := 0.0
	// Total column fragments placed across all column rows (excludes spanners).
	// Surfaced on the multicol fragment so the column-rule painter can honor
	// "rules between columns that both have content" (CSS Multicol L1 §5).
	totalColumnsRendered := 0

	// Detect outer fragmentation context: we are inside another multicol or
	// paged-media fragmentainer that has a finite block size. When present,
	// the inner multicol must break at the outer column boundary and emit a
	// BreakToken so the outer algorithm can resume us in the next column.
	// Mirrors Blink's ColumnLayoutAlgorithm tracking OuterFragmentainerBlockSize.
	hasOuterFrag := mla.space.HasBlockFragmentation &&
		mla.space.FragmentainerBlockSize != Indefinite &&
		!mla.space.IsInitialColumnBalancingPass
	outerAvailable := mla.space.FragmentainerBlockSize - mla.space.FragmentainerOffset

	// Reconstruct the MulticolPartWalker resumption state from an incoming break
	// token. The break token encodes:
	//   ChildBreakTokens[0]: the nextColToken to pass to layoutLine so it
	//     re-detects the spanner we were processing when the outer column filled.
	//   ChildBreakTokens[1] (optional): a partial-spanner token. Two variants:
	//     Clip resume: ConsumedBlockSize > 0 tells how much visual height was
	//       placed in the previous outer column.
	//     Content-overflow resume: ChildBreakTokens[0] is the spanner's own
	//       content break token (child C started at fragmentainer boundary).
	//   ChildBreakTokens[2] (optional): column rows break token — when OC1
	//     broke mid-column-row (columns hit the outer boundary), this token
	//     resumes the remaining column content in OC2.
	var nextColToken *BlockBreakToken
	var spannerConsumed float64
	hasSpannerResume := false
	var spannerContentBreakToken *BlockBreakToken
	var colRowsResumeToken *BlockBreakToken
	// nextSpannerClipToken: when OC1 had both a content-overflow spanner and a
	// subsequent clipped spanner, this token carries the clip resume info for
	// the next spanner so OC2 can resume it after the content-overflow resume.
	var nextSpannerClipToken *BlockBreakToken
	if mla.space.BreakToken != nil {
		bt := mla.space.BreakToken
		if len(bt.ChildBreakTokens) > 0 {
			nextColToken = bt.ChildBreakTokens[0]
		}
		if len(bt.ChildBreakTokens) > 1 && bt.ChildBreakTokens[1] != nil {
			partialToken := bt.ChildBreakTokens[1]
			spannerConsumed = partialToken.ConsumedBlockSize
			hasSpannerResume = true
			if len(partialToken.ChildBreakTokens) > 0 {
				spannerContentBreakToken = partialToken.ChildBreakTokens[0]
			}
			if len(partialToken.ChildBreakTokens) > 1 {
				nextSpannerClipToken = partialToken.ChildBreakTokens[1]
			}
		}
		if len(bt.ChildBreakTokens) > 2 {
			colRowsResumeToken = bt.ChildBreakTokens[2]
		}
	}

	// Phase 12c: pure nested resume. If the outer break token carries a column-
	// rows continuation with no spanner state (ChildBreakTokens[0] == nil),
	// promote it to nextColToken so the first layoutLine call resumes the
	// remaining content at the start of this outer fragmentainer.
	if nextColToken == nil && colRowsResumeToken != nil && !hasSpannerResume {
		nextColToken = colRowsResumeToken
		colRowsResumeToken = nil
	}

	// Content-overflow pending state: a spanner's box fit in this outer column
	// but its children overflowed the fragmentainer boundary. We continue
	// placing subsequent content (other spanners, column rows) before generating
	// the outer break result at the end of the loop.
	var pendingContentOverflow bool
	var pendingBeforeSpannerToken *BlockBreakToken
	var pendingPartialSpannerToken *BlockBreakToken
	var pendingColRowsBreakToken *BlockBreakToken

	// hasForcedBreakAfter is set when a spanner's break-after:column fires
	// and there is no content after the spanner to resume — the break propagates
	// to the parent context via HasForcedBreak=true on the final result.
	var hasForcedBreakAfter bool

	// buildOuterBreakResult finalises the in-progress builder and returns a
	// layout result with a BreakToken that captures the MulticolPartWalker state
	// needed to resume in the next outer column.
	//   prevNextColToken: the nextColToken used to call layoutLine in this
	//     iteration — when passed back to layoutLine it will re-detect the
	//     same spanner without replaying the pre-spanner column rows.
	//   partialSpannerToken: non-nil if we broke mid-spanner; records how
	//     much of the spanner was placed (ConsumedBlockSize).
	buildOuterBreakResult := func(prevNextColToken, partialSpannerToken *BlockBreakToken) *LayoutResult {
		builder.SetSize(LogicalSize{
			InlineSize: contentInlineSize + geom.InlineBorderPadding(),
			BlockSize:  blockCursor + geom.BlockBorderPadding(),
		})
		builder.SetIntrinsicBlockSize(blockCursor)
		builder.SetLayoutNode(mla.node)
		physBorder := ToPhysicalEdges(geom.Border, wdm)
		physPadding := ToPhysicalEdges(geom.Padding, wdm)
		physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize), wdm)
		builder.SetBoxData(&PhysicalBoxData{
			Margin:  physMargin,
			Border:  physBorder,
			Padding: physPadding,
		})
		result := builder.Build()
		// Token-slot layout (mirrors the parser at the top of Layout):
		//   [0] nextColToken (pre-spanner column rows)
		//   [1] partialSpannerToken (mid-spanner resume state)
		//   [2] pendingColRowsBreakToken (post-spanner column rows resume)
		// Keep the slots fixed so [2] is never misread as [1] — if a col-rows
		// resume exists without a partial spanner, a nil placeholder fills
		// slot [1].
		childTokens := []*BlockBreakToken{prevNextColToken, partialSpannerToken, pendingColRowsBreakToken}
		// Trim trailing nil slots to keep the break-token minimal. Keep [0]
		// at minimum so the parser can still pick up nextColToken.
		for len(childTokens) > 1 && childTokens[len(childTokens)-1] == nil {
			childTokens = childTokens[:len(childTokens)-1]
		}
		result.BreakToken = &BlockBreakToken{
			Node:             mla.node,
			ChildBreakTokens: childTokens,
		}
		if len(builder.outOfFlowCandidates) > 0 {
			result.PropagatedOOFCandidates = builder.outOfFlowCandidates
		}
		if result.Fragment != nil {
			result.Fragment.RenderedColumnCount = totalColumnsRendered
		}
		return result
	}

	// MulticolPartWalker-style loop: LayoutLine until a spanner is detected,
	// lay out the spanner at full width, then resume LayoutLine from after it.
	// Mirrors Blink's ColumnLayoutAlgorithm::LayoutChildren() + LayoutFragmentationContext().
	// isFirstRow tracks the first row within a column-run (the run between
	// spanners, or before the first spanner). Phase 12f row-wrap advances
	// line_offset only after the first row in a run. Reset to true when the
	// walker enters a new column-run after a spanner placement.
	isFirstRow := true
	for {
		// Blink cla.cc:795-797 row-advance guard. Fires whenever !isFirstRow
		// OR (on the first iteration) we find ourselves past the start of
		// the current row — e.g. after a spanner that didn't quite align to
		// a row boundary even after the pre-commit snap, or in the initial
		// position when `column-height: 0` makes every offset a row-start.
		needsRowAdvance := !isFirstRow
		if !needsRowAdvance && mla.shouldWrapColumns() && mla.hasRowHeight() &&
			mla.rowHeight() > 0 &&
			mla.remainingRowHeightAtOffset(blockCursor) <= 0 {
			needsRowAdvance = true
		}
		if needsRowAdvance {
			if mla.hasRowHeight() {
				blockCursor += mla.offsetToNextRow(blockCursor)
			}
			if hasOuterFrag && mla.hasRowHeight() &&
				mla.rowHeight() > outerAvailable-blockCursor {
				// No room for another row in the outer fragmentainer — stop now
				// and let the outer context resume any remaining content in the
				// next outer column.
				if nextColToken != nil {
					pendingColRowsBreakToken = nextColToken
					return buildOuterBreakResult(nil, nil)
				}
				break
			}
		}

		rowBlockAdvance, columnsPlaced, spannerPath, remainingToken := mla.layoutLine(
			contentNode, wdm, usedColWidth, numCols, gap,
			balanceColumns, hasExplicitBlock, explicitBlockSize,
			effectiveMaxBlockSize,
			blockCursor, nextColToken, builder,
		)
		blockCursor += rowBlockAdvance
		totalColumnsRendered += columnsPlaced

		if spannerPath == nil {
			// Phase 12f (Blink cla.cc:835): row-wrap — when wrap is in effect
			// and a column row exited with content still pending, continue the
			// same column-run by starting another row below the one we just
			// placed. Driver: column-height-001.html.
			if remainingToken != nil && mla.shouldWrapColumns() && mla.hasRowHeight() {
				nextColToken = remainingToken
				isFirstRow = false
				continue
			}
			// Phase 12c: nested multicol hit the outer fragmentainer boundary
			// with content still pending. Emit an outer break result carrying the
			// column-rows continuation so the outer fragmentation context can
			// resume this multicol in its next outer column.
			// Mirrors Blink's ColumnLayoutAlgorithm returning with a remaining
			// BreakToken when columns exit before consuming all content inside
			// an outer fragmentation context.
			if remainingToken != nil && hasOuterFrag {
				pendingColRowsBreakToken = remainingToken
				return buildOuterBreakResult(nil, nil)
			}
			break // all column content done (no outer fragmentation context)
		}

		spanner := spannerPath.Box

		// Construct a token that, when passed as nextColToken to layoutLine, will
		// cause block_layout to skip directly to this spanner (by-passing any
		// pre-spanner column rows that were already placed in this outer column).
		beforeSpannerToken := &BlockBreakToken{
			Node: contentNode,
			ChildBreakTokens: []*BlockBreakToken{{
				Node:          spanner,
				IsBreakBefore: true,
			}},
		}

		// Forced break-before:column on spanner: propagate to the parent column
		// context. Only on fresh layout (not resumed) so the break fires once.
		if hasOuterFrag && mla.space.BreakToken == nil &&
			spanner.Style() != nil && spanner.Style().GetBreakBefore() == "column" {
			var result *LayoutResult
			if blockCursor == 0 {
				// No column content was placed yet — produce a zero-height fragment so
				// borders are not painted in this outer column (they belong to the resumed
				// fragment in the next outer column).
				builder.SetSize(LogicalSize{
					InlineSize: contentInlineSize + geom.InlineBorderPadding(),
					BlockSize:  0,
				})
				builder.SetIntrinsicBlockSize(0)
				builder.SetLayoutNode(mla.node)
				// Only emit margin; suppress border+padding so nothing is painted.
				// The resumed fragment will draw full borders.
				physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize), wdm)
				builder.SetBoxData(&PhysicalBoxData{Margin: physMargin})
				result = builder.Build()
				result.BreakToken = &BlockBreakToken{
					Node:             mla.node,
					ChildBreakTokens: []*BlockBreakToken{beforeSpannerToken},
				}
			} else {
				result = buildOuterBreakResult(beforeSpannerToken, nil)
			}
			result.HasForcedBreak = true
			return result
		}

		// Outer fragmentation: check whether there is room for any of this spanner
		// in the current outer column.
		if hasOuterFrag && blockCursor >= outerAvailable {
			// The outer column is completely full — break before this spanner.
			return buildOuterBreakResult(beforeSpannerToken, nil)
		}

		// Compute the spanner's full block size, accounting for mid-spanner resumption.
		var spanFrag *PhysicalFragment
		var spanHeight float64

		var didContentOverflowResume bool
		if hasSpannerResume {
			hasSpannerResume = false
			if spannerContentBreakToken != nil {
				// Content-overflow resume: the spanner's box was fully placed in
				// the previous outer column but its children overflowed. Re-layout
				// the spanner from the child break point in the new outer column.
				fragOff := mla.space.FragmentainerOffset + blockCursor
				resumeResult := mla.layoutSpannerInFrag(spanner, contentInlineSize, wdm,
					mla.space.FragmentainerBlockSize, fragOff, spannerContentBreakToken)
				spannerContentBreakToken = nil
				if resumeResult != nil && resumeResult.Fragment != nil {
					spanFrag = resumeResult.Fragment
					spanHeight = NewLogicalFragment(wdm, resumeResult.Fragment).BlockSize()
				}
				didContentOverflowResume = true
			} else {
				// Clip resume: the spanner was visually truncated in the previous
				// outer column. Layout at full size and take the remaining portion.
				fullResult := mla.layoutSpanner(spanner, contentInlineSize, wdm)
				if fullResult != nil && fullResult.Fragment != nil {
					fullHeight := NewLogicalFragment(wdm, fullResult.Fragment).BlockSize()
					spanHeight = fullHeight - spannerConsumed
					if spanHeight < 0 {
						spanHeight = 0
					}
					// Clip the fragment to the remaining height. For simple leaf
					// spanners (background only, no children) this gives the correct
					// visual result; the background-color fills the clipped height.
					frag := fullResult.Fragment
					if wdm.IsHorizontal() {
						frag.Size.Height = layoutunit.FromFloat64Round(spanHeight)
					} else {
						frag.Size.Width = layoutunit.FromFloat64Round(spanHeight)
					}
					spanFrag = frag
				}
				spannerConsumed = 0
			}
		} else {
			var fullResult *LayoutResult
			if hasOuterFrag {
				// Layout the spanner with the outer fragmentainer constraints so
				// that children overflowing the fragmentainer boundary produce a
				// break token (content-overflow detection).
				fragOff := mla.space.FragmentainerOffset + blockCursor
				fullResult = mla.layoutSpannerInFrag(spanner, contentInlineSize, wdm,
					mla.space.FragmentainerBlockSize, fragOff, nil)
			} else {
				fullResult = mla.layoutSpanner(spanner, contentInlineSize, wdm)
			}
			if fullResult != nil && fullResult.Fragment != nil {
				spanFrag = fullResult.Fragment
				spanHeight = NewLogicalFragment(wdm, fullResult.Fragment).BlockSize()
				// Content overflow: spanner box fits physically but its children
				// overflow the outer fragmentainer boundary. Store the pending
				// break info and continue placing subsequent content in this
				// outer column before generating the outer break result.
				if hasOuterFrag && fullResult.BreakToken != nil && blockCursor+spanHeight <= outerAvailable {
					pendingContentOverflow = true
					pendingBeforeSpannerToken = beforeSpannerToken
					pendingPartialSpannerToken = &BlockBreakToken{
						Node:             spanner,
						ChildBreakTokens: []*BlockBreakToken{fullResult.BreakToken},
					}
				}
			}
		}

		if spanFrag != nil {
			// Blink cla.cc:1427-1459 (pre-commit row snap). Before placing a
			// spanner under column-wrap:wrap, if we're past the start of the
			// current column row (IsPastStartInWrappingRow), snap
			// intrinsicBlockSize forward to the next row-stride boundary so
			// the spanner lands on a row boundary — not mid-row. Without this
			// snap, after a spanner that doesn't start at a row boundary, the
			// next LayoutLine's offsetInCurrentRow math reports negative or
			// tiny remaining-row-space and column rows get placed wrong.
			// Mirror the condition: shouldWrapColumns + hasRowHeight +
			// current blockCursor is past a row start (offsetInCurrentRow > 0).
			if mla.shouldWrapColumns() && mla.hasRowHeight() && mla.rowHeight() > 0 &&
				mla.offsetInCurrentRow(blockCursor) > 0 {
				blockCursor += mla.offsetToNextRow(blockCursor)
			}
			// Outer fragmentation: check whether the spanner fits in the remaining
			// space of the current outer column.
			if hasOuterFrag && blockCursor+spanHeight > outerAvailable {
				available := outerAvailable - blockCursor
				// Clip the spanner to the available space and place it.
				if wdm.IsHorizontal() {
					spanFrag.Size.Height = layoutunit.FromFloat64Round(available)
				} else {
					spanFrag.Size.Width = layoutunit.FromFloat64Round(available)
				}
				builder.AddChild(spanFrag, LogicalOffset{
					InlineOffset: 0,
					BlockOffset:  blockCursor,
				})
				blockCursor += available
				if pendingContentOverflow {
					// Combined content-overflow + clip: encode both in the pending
					// spanner token so the resumed outer column gets both resume points.
					clipToken := &BlockBreakToken{
						Node:              spanner,
						ConsumedBlockSize: available,
					}
					pendingPartialSpannerToken.ChildBreakTokens = append(
						pendingPartialSpannerToken.ChildBreakTokens, clipToken)
					break
				}
				// Record how much of this spanner was consumed so the resumed
				// outer column can place only the remaining portion.
				partialSpannerToken := &BlockBreakToken{
					Node:              spanner,
					ConsumedBlockSize: available,
				}
				return buildOuterBreakResult(beforeSpannerToken, partialSpannerToken)
			}

			builder.AddChild(spanFrag, LogicalOffset{
				InlineOffset: 0,
				BlockOffset:  blockCursor,
			})
			blockCursor += spanHeight
			// Apply spanner margin-block-end (may be negative). Skip when resuming
			// a content-overflow spanner — its margin was already consumed in OC1.
			if !didContentOverflowResume && spanner.Style() != nil {
				spannerMargins := ResolveMargins(spanner.Style(), wdm, mla.space.AvailableSize.InlineSize)
				blockCursor += spannerMargins.BlockEnd
			}
		}

		// After a content-overflow resume, the spanner's overflow children are
		// placed. Continue with any remaining column rows or clipped spanners.
		if didContentOverflowResume {
			if nextSpannerClipToken != nil {
				// A subsequent spanner was clipped in OC1; resume the remaining
				// portion now that the content-overflow spanner is fully placed.
				hasSpannerResume = true
				spannerConsumed = nextSpannerClipToken.ConsumedBlockSize
				nextColToken = remainingToken
				nextSpannerClipToken = nil
				didContentOverflowResume = false
				isFirstRow = true
				continue
			}
			if colRowsResumeToken != nil {
				nextColToken = colRowsResumeToken
				colRowsResumeToken = nil // consume it
				isFirstRow = true
				continue
			}
			break
		}

		// Forced break-after:column on spanner: propagate to the parent column
		// context. Only on fresh layout (not resumed) so the break fires once.
		if hasOuterFrag && spanFrag != nil && !didContentOverflowResume &&
			mla.space.BreakToken == nil && !hasSpannerResume &&
			spanner.Style() != nil && spanner.Style().GetBreakAfter() == "column" {
			if remainingToken != nil {
				// Content follows the spanner — emit break, resume there.
				result := buildOuterBreakResult(remainingToken, nil)
				result.HasForcedBreak = true
				return result
			}
			// Nothing follows — propagate break-after to the parent context.
			hasForcedBreakAfter = true
			break
		}

		nextColToken = remainingToken
		if nextColToken == nil {
			break // nothing after the spanner
		}
		isFirstRow = true
	}

	// If a spanner's children overflowed the outer fragmentainer boundary, emit
	// a break token so the outer multicol resumes the spanner's content in the
	// next outer column. This is checked after the loop so that all content for
	// this outer column (spanners, column rows following the overflowing spanner)
	// is placed before the break is recorded.
	if pendingContentOverflow {
		return buildOuterBreakResult(pendingBeforeSpannerToken, pendingPartialSpannerToken)
	}

	// Phase 12f (Blink cla.cc:342): intrinsic block-size top-off. When column-
	// height is non-auto, pad the cursor up to the current row's end (clamped
	// by the outer fragmentainer's remaining space when nested) — unless the
	// cursor is already at an exact row boundary, in which case the remaining
	// equals the full row height and we must not allocate a new empty row.
	// Protects rows whose alignment was broken by a spanner (blockCursor stops
	// mid-row) and guarantees the multicol container reports row-aligned block
	// size even when forced-fixed column fragments are later removed.
	if !mla.hasAutoColumnHeight() {
		remaining := mla.remainingRowHeightAtOffset(blockCursor)
		if hasOuterFrag {
			outerLeft := outerAvailable - blockCursor
			if remaining > outerLeft {
				remaining = outerLeft
			}
			if remaining < 0 {
				remaining = 0
			}
		}
		if remaining < mla.rowHeight() {
			blockCursor += remaining
		}
	}

	// Final block size.
	finalBlockSize := blockCursor
	if hasExplicitBlock && finalBlockSize < explicitBlockSize {
		finalBlockSize = explicitBlockSize
	}
	// Phase 12e: max-height caps the multicol's content block-size when no
	// explicit height is set. fragment_geometry already applies max-height to
	// BorderBoxSize when the block-size is definite, so this only affects the
	// auto-height case (hasExplicitBlock == false).
	if !hasExplicitBlock && effectiveMaxBlockSize != Indefinite &&
		finalBlockSize > effectiveMaxBlockSize {
		finalBlockSize = effectiveMaxBlockSize
	}

	// Handle out-of-flow candidates.
	var propagatedOOF []OutOfFlowCandidate
	if len(builder.outOfFlowCandidates) > 0 {
		isPositioned := mla.style != nil && mla.style.GetPosition() != css.PositionStatic
		if isPositioned {
			var absoluteCandidates, fixedCandidates []OutOfFlowCandidate
			for _, cand := range builder.outOfFlowCandidates {
				if cand.IsFixedPosition {
					fixedCandidates = append(fixedCandidates, cand)
				} else {
					absoluteCandidates = append(absoluteCandidates, cand)
				}
			}
			if len(absoluteCandidates) > 0 {
				oofPart := &OutOfFlowLayoutPart{
					ctx:                 mla.ctx,
					containingBlockWDM:  wdm,
					containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
					geom:                geom,
				}
				if extra := oofPart.LayoutCandidates(absoluteCandidates, builder); len(extra) > 0 {
					fixedCandidates = append(fixedCandidates, extra...)
				}
			}
			propagatedOOF = fixedCandidates
		} else {
			propagatedOOF = builder.outOfFlowCandidates
		}
	}

	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})
	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:  physMargin,
		Border:  physBorder,
		Padding: physPadding,
	})
	builder.SetIntrinsicBlockSize(blockCursor)

	result := builder.Build()
	result.PropagatedOOFCandidates = propagatedOOF
	if hasForcedBreakAfter {
		result.HasForcedBreak = true
	}
	if result.Fragment != nil {
		result.Fragment.RenderedColumnCount = totalColumnsRendered
	}
	return result
}

// layoutLine implements the outer stretch / inner per-column loop for one
// "column row" — all columns between two spanners (or the full content if no
// spanners). Returns the block-size consumed by this row, the spanner path
// (if a column-span:all element was encountered), and the break token for
// resuming after the spanner.
//
// Mirrors Blink's ColumnLayoutAlgorithm::LayoutLine() (cla.cc:858).
func (mla *MulticolLayoutAlgorithm) layoutLine(
	contentNode *LayoutInputNode,
	wdm WritingDirectionMode,
	usedColWidth float64,
	numCols int,
	gap float64,
	balanceColumns bool,
	hasExplicitBlock bool,
	explicitBlockSize float64,
	maxBlockSize float64,
	lineOffset float64,
	nextColToken *BlockBreakToken,
	builder *BoxFragmentBuilder,
) (rowBlockAdvance float64, columnsPlaced int, spannerPath *ColumnSpannerPath, remainingToken *BlockBreakToken) {

	// Outer remaining space: cap column height so columns don't exceed what
	// fits in the current outer fragmentainer. Mirrors Blink's
	// RemainingRowHeightAtOffset(line_offset) used in ConstrainColumnBlockSize.
	outerRemaining := 0.0
	if mla.space.HasBlockFragmentation &&
		mla.space.FragmentainerBlockSize != Indefinite &&
		!mla.space.IsInitialColumnBalancingPass {
		rem := mla.space.FragmentainerBlockSize - mla.space.FragmentainerOffset - lineOffset
		if rem > 0 {
			outerRemaining = rem
		}
	}

	// Determine initial column block-size.
	// Phase 12f (Blink cla.cc:864): non-auto column-height fixes the column
	// block-size to the row's remaining height at this line offset, bypassing
	// the balancing / max-height / auto branches. This is the hard upper bound
	// on a single row of columns.
	// Phase 12e (Blink behaviour): column-fill:auto + max-height on an auto-
	// height multicol uses max-height as the column block-size so columns fill
	// sequentially up to that height. Otherwise the column block-size is
	// content-driven (auto), which would collapse the multicol to a single
	// column of indefinite height.
	var colBlockSize float64
	switch {
	case !mla.hasAutoColumnHeight():
		colBlockSize = mla.remainingRowHeightAtOffset(lineOffset)
		colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset)
	case balanceColumns:
		colBlockSize = mla.resolveColumnAutoBlockSize(contentNode, wdm, usedColWidth, numCols, gap, nextColToken)
		colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset)
	case hasExplicitBlock:
		colBlockSize = mla.constrainColumnBlockSize(explicitBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset)
	case maxBlockSize != Indefinite:
		colBlockSize = mla.constrainColumnBlockSize(maxBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset)
	default:
		colBlockSize = Indefinite
	}

	var maxColHeight float64
	var finalColumns []struct {
		fragment       *PhysicalFragment
		offset         LogicalOffset
		intrinsicBlock float64
		propagatedOOF  []OutOfFlowCandidate
	}
	var finalColBreakToken *BlockBreakToken
	var lastInnerResult *LayoutResult

	// Outer stretch loop: mirrors Blink's do { ... } while (true) at cla.cc:967.
	for {
		var columns []struct {
			fragment       *PhysicalFragment
			offset         LogicalOffset
			intrinsicBlock float64
			propagatedOOF  []OutOfFlowCandidate
		}
		minSpaceShortage := math.MaxFloat64
		hasShortage := false
		actualColumnCount := 0
		forcedBreakCount := 0
		hasViolatingBreak := false

		// Reset break token to the incoming token at the start of each iteration.
		colBreakToken := nextColToken
		inlineOffset := 0.0

		// Inner per-column loop.
		for col := 0; col < numCols || colBlockSize == Indefinite; col++ {
			colSpace := mla.createConstraintSpaceForColumn(wdm, usedColWidth, colBlockSize, colBreakToken, balanceColumns, true)
			result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, colSpace).Layout()
			if result == nil || result.Fragment == nil {
				break
			}

			colFrag := result.Fragment
			colHeight := NewLogicalFragment(wdm, colFrag).BlockSize()
			// Column fragmentainers clip their content in the BLOCK axis only:
			// a child taller than the column fragment (e.g. our engine's
			// leaf-with-height:40 placed in a 20-tall column) must not paint
			// beyond the column's block extent. But in the INLINE axis, wide
			// children must be allowed to overflow into adjacent columns —
			// this is how CSS multicol expresses a width:200 % leaf inside a
			// narrow sub-column. Blink's BoxFragmentPainter::PaintBlockChild
			// fragmentainer branch pushes no per-column clip at paint time;
			// we approximate by clipping only the block axis, keeping the
			// inline overflow visible. See findings.md "F2 Blink reference".
			//
			// `column-height: 0` (explicit zero) is the CSS Multicol L2 case
			// where a row is 0 tall and each monolithic leaf is placed as
			// "last resort" with overflow visible; clipping would hide
			// everything. Skip the clip only in that specific case.
			// `colBlockSize == 0` under auto-column-height is the
			// balance-yields-0 workaround (see createConstraintSpaceForColumn)
			// and still needs the clip.
			skipBlockClip := colBlockSize == 0 && !mla.hasAutoColumnHeight()
			if colBlockSize != Indefinite && !skipBlockClip {
				colFrag.ClipBlockAxisOnly = true
			}

			columns = append(columns, struct {
				fragment       *PhysicalFragment
				offset         LogicalOffset
				intrinsicBlock float64
				propagatedOOF  []OutOfFlowCandidate
			}{
				fragment:       colFrag,
				offset:         LogicalOffset{InlineOffset: inlineOffset, BlockOffset: lineOffset},
				intrinsicBlock: result.IntrinsicBlockSize,
				propagatedOOF:  result.PropagatedOOFCandidates,
			})

			// Spanner detected: break out immediately (Blink cla.cc:1048).
			if result.ColumnSpannerPath != nil {
				lastInnerResult = result
				actualColumnCount++
				break
			}

			// Track shortage for balancing stretch.
			if result.MinSpaceShortage > 0 {
				if result.MinSpaceShortage < minSpaceShortage {
					minSpaceShortage = result.MinSpaceShortage
					hasShortage = true
				}
			}

			actualColumnCount++
			if result.HasForcedBreak {
				forcedBreakCount++
			}
			if colBlockSize != Indefinite && colHeight > colBlockSize {
				hasViolatingBreak = true
			}
			// Phase 12d (Blink cla.cc:1019): demote acceptance when any column's
			// break appeal is below Perfect (e.g. break-inside:avoid violated).
			if result.BreakAppeal != BreakAppealPerfect {
				hasViolatingBreak = true
			}
			// F5: "terminal shortage in a continuation row" — this column
			// overflowed monolithically (shortage > 0) AND its break token says
			// HasSeenAllChildren with no child break tokens, meaning no
			// subsequent column can absorb the overflow. Restricted to
			// continuation rows (lineOffset > 0, e.g. post-spanner): in that
			// context, monolithic overflow comes from short trailing content
			// (e.g. a single inline line) that the initial balance estimate
			// underestimated, and stretching is the correct response. For
			// lineOffset == 0 (first row), HasSeenAllChildren-with-shortage
			// represents normal "all siblings stacked overflow" that pre-
			// existing acceptance handles via ClipBlockAxisOnly without
			// changing visible layout shape (matches REF rendering for
			// nested-balancing tests). Driver: multicol-list-item-003.
			if lineOffset > 0 &&
				result.MinSpaceShortage > 0 && result.BreakToken != nil &&
				result.BreakToken.HasSeenAllChildren &&
				len(result.BreakToken.ChildBreakTokens) == 0 {
				hasViolatingBreak = true
			}

			colBreakToken = result.BreakToken
			lastInnerResult = result
			if colBreakToken == nil {
				break // all content fit
			}
			inlineOffset += usedColWidth + gap

			if col+1 >= numCols {
				break
			}
		}

		finalColumns = columns
		finalColBreakToken = colBreakToken

		// column-fill:auto + spanner: flip to balanced mode and retry (cla.cc:1130–1140).
		if !balanceColumns {
			if lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil {
				balanceColumns = true
				colBlockSize = mla.resolveColumnAutoBlockSize(contentNode, wdm, usedColWidth, numCols, gap, nextColToken)
				colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset)
				continue
			}
			break
		}

		// Acceptance condition (Blink cla.cc:1034–1037):
		// Accept when no violations, column count within limit, and either
		// content fully fits (no remaining break token) or a spanner was hit
		// (in which case this row is done regardless).
		lastHasSpanner := lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil
		if !hasViolatingBreak &&
			actualColumnCount <= numCols &&
			(colBreakToken == nil || lastHasSpanner) {
			break
		}

		// Cannot make progress: all breaks are forced.
		if numCols <= forcedBreakCount+1 {
			break
		}

		if !hasShortage {
			break
		}

		// Stretch column height by the minimum shortage and retry.
		newColBlockSize := colBlockSize + minSpaceShortage
		newColBlockSize = mla.constrainColumnBlockSize(newColBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset)
		if newColBlockSize <= colBlockSize {
			// Nested balancing: we can't stretch any further in this inner
			// multicol. Report the minimum shortage upward so the outer
			// multicol's stretch loop can widen its own columns on the next
			// iteration, then try us again with more outer space.
			// Mirrors Blink cla.cc:1235-1250.
			if mla.space.IsInsideBalancedColumns &&
				!mla.space.IsInitialColumnBalancingPass &&
				hasShortage {
				builder.PropagateSpaceShortage(minSpaceShortage)
			}
			break
		}
		colBlockSize = newColBlockSize
	}

	// Commit final columns to the parent builder.
	// columnsPlaced reports how many columns actually received content so the
	// column-rule painter (CSS Multicol L1 §5: rules only between columns that
	// both have content) can skip painting rules adjacent to empty columns.
	maxColHeight = 0.0
	columnsPlaced = 0
	seenOOF := map[*LayoutInputNode]bool{}
	for _, col := range finalColumns {
		builder.AddChild(col.fragment, col.offset)
		h := NewLogicalFragment(wdm, col.fragment).BlockSize()
		if h > maxColHeight {
			maxColHeight = h
		}
		// A column counts as non-empty if it has intrinsic content. With
		// IsFixedBlockSize a column fragment can have h>0 even when empty
		// (the forced size); intrinsicBlock is the spec-correct signal.
		if col.intrinsicBlock > 0 {
			columnsPlaced++
		}
		// Propagate abs/fixed descendants of the per-column BlockLayoutAlgorithm
		// result out to the multicol's builder. An abspos whose CB is the
		// multicol (e.g. `multicol { position: relative }` with an abspos child)
		// gets iterated by *every* per-column layout call and emitted on each
		// result; dedupe by Node so we add it once. Static positions come out
		// in the column's local coordinates — translate to the multicol's
		// content-box by adding the column's offset within the multicol.
		for _, cand := range col.propagatedOOF {
			if seenOOF[cand.Node] {
				continue
			}
			seenOOF[cand.Node] = true
			cand.StaticPosition.Offset.InlineOffset += col.offset.InlineOffset
			cand.StaticPosition.Offset.BlockOffset += col.offset.BlockOffset
			builder.AddOutOfFlowCandidate(cand)
		}
	}
	// Cap row advance to colBlockSize when finite: columns placed with
	// fragmentation don't advance the cursor beyond the column height.
	if colBlockSize != Indefinite && maxColHeight > colBlockSize {
		maxColHeight = colBlockSize
	}

	// Determine spanner path and remaining token from the last inner result.
	if lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil {
		// When a spanner is detected and NO pre-spanner in-column content was
		// placed in any column of this row (all intrinsic sizes are 0), commit
		// the spanner at the row origin — not below an empty forced slot
		// (IsFixedBlockSize + column-height can make an empty column fragment
		// report a full row height via BlockSize()). For rows where at least
		// one column placed content before the spanner, keep the existing
		// forced-row-height advance so subsequent rows align correctly.
		anyIntrinsic := false
		for _, col := range finalColumns {
			if col.intrinsicBlock > 0 {
				anyIntrinsic = true
				break
			}
		}
		rowAdvance := maxColHeight
		if !anyIntrinsic {
			rowAdvance = 0
		}
		return rowAdvance, columnsPlaced, lastInnerResult.ColumnSpannerPath, lastInnerResult.BreakToken
	}
	// Return any remaining column break token so the caller can include it
	// in the outer break result and resume column rows in the next outer column.
	return maxColHeight, columnsPlaced, nil, finalColBreakToken
}

// layoutSpanner lays out a column-span:all element at the full multicol
// container width. Mirrors Blink's ColumnLayoutAlgorithm::LayoutSpanner()
// (cla.cc:1397) — creates a new FC constraint space at full container inline
// size and lays out the spanner node.
func (mla *MulticolLayoutAlgorithm) layoutSpanner(
	spanner *LayoutInputNode,
	contentInlineSize float64,
	wdm WritingDirectionMode,
) *LayoutResult {
	spannerSpace := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  Indefinite,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  0,
		}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		Build()
	return layoutElement(mla.ctx, spanner, spannerSpace)
}

// layoutSpannerInFrag lays out a column-span:all element with an outer
// fragmentainer context so that children overflowing the fragmentainer boundary
// produce a break token (content-overflow detection). fragBS/fragOff are the
// outer fragmentainer block-size and the spanner's offset within it.
// breakToken, if non-nil, resumes the spanner's content layout.
func (mla *MulticolLayoutAlgorithm) layoutSpannerInFrag(
	spanner *LayoutInputNode,
	contentInlineSize float64,
	wdm WritingDirectionMode,
	fragBS float64,
	fragOff float64,
	breakToken *BlockBreakToken,
) *LayoutResult {
	b := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  Indefinite,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  0,
		}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(fragBS).
		SetFragmentainerOffset(fragOff).
		SetBlockFragmentationType(FragmentColumn)
	if breakToken != nil {
		b = b.SetBreakToken(breakToken)
	}
	return layoutElement(mla.ctx, spanner, b.Build())
}

// resolveColumnAutoBlockSize estimates the balanced column height by doing an
// unconstrained layout of the content to get total height, then dividing by
// the number of columns. Mirrors Blink's ResolveColumnAutoBlockSizeInternal().
//
// nextColToken, if non-nil, is threaded into the measurement space so the
// unconstrained pass starts from the right position in the content stream
// (e.g., after a spanner in a multi-row layout).
func (mla *MulticolLayoutAlgorithm) resolveColumnAutoBlockSize(
	contentNode *LayoutInputNode,
	wdm WritingDirectionMode,
	colWidth float64,
	numCols int,
	gap float64,
	nextColToken *BlockBreakToken,
) float64 {
	b := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{
			InlineSize: colWidth,
			BlockSize:  Indefinite,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: colWidth,
			BlockSize:  0,
		}).
		SetPercentageResolutionInlineSize(colWidth).
		SetIsContentSuggestionLayout(true).
		SetIsInitialColumnBalancingPass(true).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(Indefinite).
		SetBlockFragmentationType(FragmentColumn)

	if nextColToken != nil {
		b = b.SetBreakToken(nextColToken)
	}

	result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, b.Build()).Layout()
	if result == nil {
		return 0
	}
	totalHeight := result.IntrinsicBlockSize
	if totalHeight <= 0 {
		return 0
	}

	estimate := math.Ceil(totalHeight / float64(numCols))
	if estimate < 1 {
		estimate = 1
	}
	return estimate
}

// constrainColumnBlockSize clamps the candidate column height to the
// container's explicit height (if any), max-height (if set), the outer
// fragmentainer's remaining space, the row-height at this offset (Phase 12f),
// and ensures it is non-negative. Mirrors Blink's ConstrainColumnBlockSize()
// (cla.cc:2017 — clamp by RemainingRowHeightAtOffset(line_offset)).
// maxBlockSize == Indefinite means the container has no max-height constraint.
// outerRemaining > 0 means columns cannot exceed this outer limit.
func (mla *MulticolLayoutAlgorithm) constrainColumnBlockSize(
	size float64,
	hasExplicitBlock bool,
	explicitBlockSize float64,
	maxBlockSize float64,
	outerRemaining float64,
	lineOffset float64,
) float64 {
	if size < 0 {
		size = 0
	}
	if hasExplicitBlock && size > explicitBlockSize {
		size = explicitBlockSize
	}
	if maxBlockSize != Indefinite && size > maxBlockSize {
		size = maxBlockSize
	}
	if outerRemaining > 0 && size > outerRemaining {
		size = outerRemaining
	}
	if mla.hasRowHeight() {
		if rem := mla.remainingRowHeightAtOffset(lineOffset); size > rem {
			size = rem
		}
	}
	return size
}

// createConstraintSpaceForColumn builds the constraint space for a single
// column fragmentainer. Mirrors Blink's CreateConstraintSpaceForFragmentainer()
// (fragmentation_utils.cc).
//
// FragmentainerOffset is always 0: each column fragmentainer starts fresh at
// block position 0. The lineOffset (absolute position of this row within the
// multicol container) is used only for positioning column fragments in the
// parent builder, not for fragmentation overflow calculations.
//
// fixedBlockSize: when true, force the column fragment's block-size to
// colBlockSize via IsFixedBlockSize+IsBlockSizeOverride. Used for
// column-fill:balance (columns are balanced to fill the column row height).
// When false, colBlockSize is only the fragmentainer constraint — content
// fragments at it but the column box itself is intrinsic-sized. Used for
// column-fill:auto with auto block-size so the multicol container's intrinsic
// block-size is content-driven instead of locked to max-height.
func (mla *MulticolLayoutAlgorithm) createConstraintSpaceForColumn(
	wdm WritingDirectionMode,
	colWidth float64,
	colBlockSize float64,
	breakToken *BlockBreakToken,
	balanceColumns bool,
	fixedBlockSize bool,
) ConstraintSpace {
	// A balanced estimate of 0 means the row contains no non-spanner content.
	// Treat it as Indefinite so the column fragment's height is driven purely
	// by its intrinsic content (zero), not by a 1px override that would add
	// a ghost row before every spanner. Only applies when column-height is
	// auto: an explicit `column-height: 0` (CSS Multicol L2) is a real zero
	// that must force every monolith to wrap to the next row, so preserve
	// the literal 0 in that case.
	availBlock := colBlockSize
	if colBlockSize == 0 && mla.hasAutoColumnHeight() {
		availBlock = Indefinite
	}
	isFixed := availBlock != Indefinite && fixedBlockSize

	b := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{
			InlineSize: colWidth,
			BlockSize:  availBlock,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: colWidth,
			BlockSize:  availBlock,
		}).
		SetPercentageResolutionInlineSize(colWidth).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetIsInsideBalancedColumns(balanceColumns)

	if isFixed {
		// IsBlockSizeOverride + IsFixedBlockSize: the column height overrides any
		// CSS block-size on the content node. FragmentainerOffset=0 because each
		// column starts fresh at the top of its fragmentainer.
		b = b.SetIsFixedBlockSize(true).
			SetIsBlockSizeOverride(true).
			SetFragmentainerBlockSize(colBlockSize).
			SetFragmentainerOffset(0)
	} else if availBlock != Indefinite {
		// column-fill:auto: still need fragmentainer info so that block_layout
		// fragments overflowing leaves at the column boundary (Phase 12b leaf
		// fragmentation), even though we don't force the column fragment size.
		b = b.SetFragmentainerBlockSize(colBlockSize).
			SetFragmentainerOffset(0)
	}

	if breakToken != nil {
		b = b.SetBreakToken(breakToken)
	}

	return b.Build()
}

// groupInlineChildrenForMulticol wraps consecutive inline-level and text
// children into anonymous block nodes so that block_layout's BFC path can
// route inline runs to an IFC. Without this, a node with mixed inline+block
// children (e.g. <span>, <span>, <h4 column-span:all>, text) would process
// each span as a separate block child, breaking column fragmentation.
//
// Mirrors CSS 2.1 §9.2.1.1 anonymous block box generation. Block-level
// children (including column-span:all spanners) pass through unchanged.
// Out-of-flow and float children are grouped with the inline run they appear
// in, matching standard anonymous-block generation rules.
func groupInlineChildrenForMulticol(children []*LayoutInputNode, parentStyle *css.Style) []*LayoutInputNode {
	if len(children) == 0 {
		return nil
	}

	// Fast path: check whether any block-level non-spanner child exists.
	// If all children are inline or text, no grouping is needed.
	needsGrouping := false
	for _, child := range children {
		if child.IsText() {
			continue
		}
		s := child.Style()
		if s == nil {
			needsGrouping = true
			break
		}
		pos := s.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			continue
		}
		if s.GetFloat() != css.FloatNone {
			continue
		}
		d := s.GetDisplay()
		if d != css.DisplayInline && d != css.DisplayInlineBlock &&
			d != css.DisplayInlineFlex && d != css.DisplayInlineGrid &&
			d != css.DisplayInlineTable {
			needsGrouping = true
			break
		}
	}
	if !needsGrouping {
		return children
	}

	var result []*LayoutInputNode
	var inlineRun []*LayoutInputNode

	flushRun := func() {
		if len(inlineRun) == 0 {
			return
		}
		// Drop whitespace-only runs: they have no visual effect in a block
		// container (CSS white-space:normal collapses inter-element whitespace)
		// and anonymous wrappers for them cause break-token pointer instability
		// during multicol fragmentation resume.
		allWS := true
		for _, n := range inlineRun {
			if !n.IsText() || strings.TrimSpace(n.TextContent()) != "" {
				allWS = false
				break
			}
		}
		if allWS {
			inlineRun = nil
			return
		}
		anonStyle := css.NewAnonymousBlockStyle(parentStyle)
		result = append(result, &LayoutInputNode{
			style:       anonStyle,
			children:    inlineRun,
			isAnonymous: true,
		})
		inlineRun = nil
	}

	for _, child := range children {
		if child.IsText() {
			inlineRun = append(inlineRun, child)
			continue
		}
		s := child.Style()
		if s == nil {
			flushRun()
			result = append(result, child)
			continue
		}
		pos := s.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			inlineRun = append(inlineRun, child)
			continue
		}
		if s.GetFloat() != css.FloatNone {
			inlineRun = append(inlineRun, child)
			continue
		}
		d := s.GetDisplay()
		if d == css.DisplayInline || d == css.DisplayInlineBlock ||
			d == css.DisplayInlineFlex || d == css.DisplayInlineGrid ||
			d == css.DisplayInlineTable {
			inlineRun = append(inlineRun, child)
		} else {
			flushRun()
			result = append(result, child)
		}
	}
	flushRun()
	return result
}
