package layout

import (
	"math"
	"strings"

	"louis14/pkg/css"
)

// MulticolLayoutAlgorithm implements CSS Multi-column Layout Module Level 1.
// Mirrors Blink's ColumnLayoutAlgorithm (column_layout_algorithm.{h,cc}).
//
// Phase 12a: fragmentation infrastructure — outer stretch / inner column loop,
// MinimalSpaceShortage feedback, inline content fragmentation.
// Phase 12b: ColumnSpannerPath detection + MulticolPartWalker-style walk loop,
// LayoutSpanner, re-balance after spanner, column-fill:auto + spanner flip.
type MulticolLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace
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
func isMulticolContainer(style *css.Style) bool {
	if style == nil {
		return false
	}
	return style.GetColumnCount() > 0 || style.GetColumnWidth() > 0
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

	// Container explicit block-size (from CSS height/max-height).
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
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
		if len(bt.ChildBreakTokens) > 1 {
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
		childTokens := []*BlockBreakToken{prevNextColToken}
		if partialSpannerToken != nil {
			childTokens = append(childTokens, partialSpannerToken)
		}
		if pendingColRowsBreakToken != nil {
			childTokens = append(childTokens, pendingColRowsBreakToken)
		}
		result.BreakToken = &BlockBreakToken{
			Node:             mla.node,
			ChildBreakTokens: childTokens,
		}
		if len(builder.outOfFlowCandidates) > 0 {
			result.PropagatedOOFCandidates = builder.outOfFlowCandidates
		}
		return result
	}

	// MulticolPartWalker-style loop: LayoutLine until a spanner is detected,
	// lay out the spanner at full width, then resume LayoutLine from after it.
	// Mirrors Blink's ColumnLayoutAlgorithm::LayoutChildren() + LayoutFragmentationContext().
	for {
		rowBlockAdvance, spannerPath, remainingToken := mla.layoutLine(
			contentNode, wdm, usedColWidth, numCols, gap,
			balanceColumns, hasExplicitBlock, explicitBlockSize,
			blockCursor, nextColToken, builder,
		)
		blockCursor += rowBlockAdvance

		if spannerPath == nil {
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
						frag.Size.Height = spanHeight
					} else {
						frag.Size.Width = spanHeight
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
			// Outer fragmentation: check whether the spanner fits in the remaining
			// space of the current outer column.
			if hasOuterFrag && blockCursor+spanHeight > outerAvailable {
				available := outerAvailable - blockCursor
				// Clip the spanner to the available space and place it.
				if wdm.IsHorizontal() {
					spanFrag.Size.Height = available
				} else {
					spanFrag.Size.Width = available
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
				continue
			}
			if colRowsResumeToken != nil {
				nextColToken = colRowsResumeToken
				colRowsResumeToken = nil // consume it
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
	}

	// If a spanner's children overflowed the outer fragmentainer boundary, emit
	// a break token so the outer multicol resumes the spanner's content in the
	// next outer column. This is checked after the loop so that all content for
	// this outer column (spanners, column rows following the overflowing spanner)
	// is placed before the break is recorded.
	if pendingContentOverflow {
		return buildOuterBreakResult(pendingBeforeSpannerToken, pendingPartialSpannerToken)
	}

	// Final block size.
	finalBlockSize := blockCursor
	if hasExplicitBlock && finalBlockSize < explicitBlockSize {
		finalBlockSize = explicitBlockSize
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
	lineOffset float64,
	nextColToken *BlockBreakToken,
	builder *BoxFragmentBuilder,
) (rowBlockAdvance float64, spannerPath *ColumnSpannerPath, remainingToken *BlockBreakToken) {

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
	var colBlockSize float64
	if balanceColumns {
		colBlockSize = mla.resolveColumnAutoBlockSize(contentNode, wdm, usedColWidth, numCols, gap, nextColToken)
		colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, outerRemaining)
	} else if hasExplicitBlock {
		colBlockSize = mla.constrainColumnBlockSize(explicitBlockSize, hasExplicitBlock, explicitBlockSize, outerRemaining)
	} else {
		colBlockSize = Indefinite
	}

	var maxColHeight float64
	var finalColumns []struct {
		fragment *PhysicalFragment
		offset   LogicalOffset
	}
	var finalColBreakToken *BlockBreakToken
	var lastInnerResult *LayoutResult

	// Outer stretch loop: mirrors Blink's do { ... } while (true) at cla.cc:967.
	for {
		var columns []struct {
			fragment *PhysicalFragment
			offset   LogicalOffset
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
			colSpace := mla.createConstraintSpaceForColumn(wdm, usedColWidth, colBlockSize, colBreakToken, balanceColumns)
			result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, colSpace).Layout()
			if result == nil || result.Fragment == nil {
				break
			}

			colFrag := result.Fragment
			colHeight := NewLogicalFragment(wdm, colFrag).BlockSize()
			// Column fragmentainers always clip their content — child fragments
			// may have their full declared size (e.g. a leaf block with height:40px
			// placed in a 20px column), but only the portion within the column
			// should be visible. Mirrors Blink's SetShouldClipFragment on column
			// fragmentainer fragments.
			if colBlockSize != Indefinite {
				colFrag.ClipContentToBorderBox = true
			}

			columns = append(columns, struct {
				fragment *PhysicalFragment
				offset   LogicalOffset
			}{
				fragment: colFrag,
				offset:   LogicalOffset{InlineOffset: inlineOffset, BlockOffset: lineOffset},
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
				colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, outerRemaining)
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
		newColBlockSize = mla.constrainColumnBlockSize(newColBlockSize, hasExplicitBlock, explicitBlockSize, outerRemaining)
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
	maxColHeight = 0.0
	for _, col := range finalColumns {
		builder.AddChild(col.fragment, col.offset)
		h := NewLogicalFragment(wdm, col.fragment).BlockSize()
		if h > maxColHeight {
			maxColHeight = h
		}
	}
	// Cap row advance to colBlockSize when finite: columns placed with
	// fragmentation don't advance the cursor beyond the column height.
	if colBlockSize != Indefinite && maxColHeight > colBlockSize {
		maxColHeight = colBlockSize
	}

	// Determine spanner path and remaining token from the last inner result.
	if lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil {
		return maxColHeight, lastInnerResult.ColumnSpannerPath, lastInnerResult.BreakToken
	}
	// Return any remaining column break token so the caller can include it
	// in the outer break result and resume column rows in the next outer column.
	return maxColHeight, nil, finalColBreakToken
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
// container's explicit height (if any), the outer fragmentainer's remaining
// space, and ensures it is non-negative. Mirrors Blink's
// ConstrainColumnBlockSize() (cla.cc:1974) which clamps by
// RemainingRowHeightAtOffset(line_offset).
// outerRemaining > 0 means columns cannot exceed this outer limit.
func (mla *MulticolLayoutAlgorithm) constrainColumnBlockSize(
	size float64,
	hasExplicitBlock bool,
	explicitBlockSize float64,
	outerRemaining float64,
) float64 {
	if size < 0 {
		size = 0
	}
	if hasExplicitBlock && size > explicitBlockSize {
		size = explicitBlockSize
	}
	if outerRemaining > 0 && size > outerRemaining {
		size = outerRemaining
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
func (mla *MulticolLayoutAlgorithm) createConstraintSpaceForColumn(
	wdm WritingDirectionMode,
	colWidth float64,
	colBlockSize float64,
	breakToken *BlockBreakToken,
	balanceColumns bool,
) ConstraintSpace {
	// A balanced estimate of 0 means the row contains no non-spanner content.
	// Treat it as Indefinite so the column fragment's height is driven purely
	// by its intrinsic content (zero), not by a 1px override that would add
	// a ghost row before every spanner.
	availBlock := colBlockSize
	if colBlockSize == 0 {
		availBlock = Indefinite
	}
	isFixed := availBlock != Indefinite

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
