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

	// containerPercentResolutionBlockSize is the multicol container's explicit
	// content-box block-size for use as the percentage-resolution base for
	// percentage-height children (CSS Multicol §6, Blink
	// ColumnLayoutAlgorithm::CreateConstraintSpaceForFragmentainer:
	// container_builder_.ExplicitBlockSize().value_or(kIndefiniteSize)).
	// Set in Layout() once, reused by createConstraintSpaceForColumn and
	// layoutSpanner. Indefinite when the multicol has auto block-size.
	containerPercentResolutionBlockSize float64

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

	// --- Track A: GapGeometry state (Phase 12h.6) ---
	// Mirrors Blink's ColumnLayoutAlgorithm private fields for gap decoration.

	// crossGaps accumulates one entry per inter-column gap (column-rule position).
	// Populated by addCrossGap during the per-column layout loop.
	crossGaps []CrossGap

	// mainGaps accumulates one entry per inter-row gap or spanner boundary.
	// Populated by addMainGap during spanner commit and (future) row-wrap.
	mainGaps []MainGap

	// columnsPerRow records the number of columns placed in each row, with
	// -1 used as a sentinel (kNotFound) for spanner "rows".
	// Mirrors Blink's optional<Vector<wtf_size_t>> columns_per_row_.
	columnsPerRow    []int
	hasColumnsPerRow bool

	// firstColumnOffset is the logical offset of the first column placed,
	// recorded once. Mirrors Blink's optional<LogicalOffset> first_column_offset_.
	firstColumnOffset    LogicalOffset
	firstColumnOffsetSet bool

	// columnGapSize is the resolved column-gap (same value as `gap` in Layout).
	columnGapSize float64

	// maxColumnsInRow is the maximum number of columns placed in any single row
	// (used when padding cross_gaps_ to the declared column-count).
	maxColumnsInRow int

	// usedColWidth is the computed per-column inline-size (resolved once in
	// Layout, stored for use by drawColumnRules when padding cross_gaps_).
	usedColWidth float64

	// lastMeasuredTallestUnbreakable carries the tallest unbreakable
	// block-size accumulated during resolveColumnAutoBlockSize's measure
	// pass, so MLA.Layout can forward it to its outer container_builder
	// when this multicol is itself inside an initial column-balancing
	// pass (nested column balancing). Mirrors Blink's
	// container_builder_.PropagateTallestUnbreakableBlockSize call at
	// column_layout_algorithm.cc:1706-1712. v2 Path X port.
	lastMeasuredTallestUnbreakable float64
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

	// Phase 25 Cmt-3: an outer (non-nested) multicol IS the block-fragmentation
	// context root. The flag gates `OutOfFlowLayoutPart.HandleFragmentation`'s
	// drain pass. When this multicol is itself nested in another fragmentation
	// context (`mla.space.HasBlockFragmentation`), an ANCESTOR multicol is the
	// root; this builder must not drain. Mirrors Blink's
	// `column_layout_algorithm.cc:391-414` / `out_of_flow_layout_part.cc:692`.
	if !mla.space.HasBlockFragmentation {
		builder.SetIsBlockFragmentationContextRoot()
	}

	// Callsite 1: seed the unpositioned list marker from the node + break token.
	// Mirrors Blink cla.cc:240–253 (constructor body).
	// ListMarkerBlockNodeIfListItem returns nil until the layout-time marker
	// path is enabled — this makes the protocol a no-op in the current engine.
	if markerNode := mla.node.ListMarkerBlockNodeIfListItem(); markerNode != nil {
		if !markerNode.ListMarkerOccupiesWholeLine() {
			incoming := mla.space.BreakToken
			if incoming == nil || incoming.HasUnpositionedListMarker {
				builder.SetUnpositionedListMarker(&UnpositionedListMarker{
					MarkerNode: markerNode,
				})
			}
		}
	}

	contentInlineSize := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if contentInlineSize < 0 {
		contentInlineSize = 0
	}

	// Resolve column parameters.
	colCount := mla.style.GetColumnCount()
	colWidth := mla.style.GetColumnWidth()
	gap := mla.style.GetColumnGapMulticolWithBase(contentInlineSize)
	mla.columnGapSize = gap
	numCols, usedColWidth := resolveColumnCount(contentInlineSize, colCount, colWidth, gap)
	mla.usedColWidth = usedColWidth

	// Container explicit block-size (from CSS height).
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
		}
	}

	// Percentage-resolution base for percentage-height children: the multicol
	// container's explicit content-box block-size, or Indefinite when auto.
	// Mirrors Blink's container_builder_.ExplicitBlockSize().value_or(kIndefiniteSize).
	mla.containerPercentResolutionBlockSize = Indefinite
	if hasExplicitBlock {
		mla.containerPercentResolutionBlockSize = explicitBlockSize
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
			mla.remainingContentBlockSize -= mla.space.BreakToken.ConsumedBlockSize.Float64()
			if mla.remainingContentBlockSize < 0 {
				mla.remainingContentBlockSize = 0
			}
		}
	}
	// MulticolBreakTokenData row phase carry across outer fragmentainers.
	// Mirrors Blink's OffsetInCurrentRow read site (cla.cc:2122-2139)
	// which adds GetBreakToken()->TokenData()->consumed_row_block_size to
	// the line offset before the modulo against row_stride. Phase 16.e+18
	// step 1 (scaffold): the carrier field exists on BlockBreakToken
	// (`MulticolData *MulticolBreakTokenData`); the WRITE site lands in
	// step 4. Until then MulticolData is always nil and behavior is
	// unchanged.
	mla.consumedRowBlockSize = 0
	if mla.space.BreakToken != nil && mla.space.BreakToken.MulticolData != nil {
		mla.consumedRowBlockSize = mla.space.BreakToken.MulticolData.ConsumedRowBlockSize
	}

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
			effectiveMaxBlockSize = maxBS.Float64()
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
	// Use cached grouped children so that anonymous block wrappers have the
	// same pointer identity across multiple Layout() calls on the same node.
	// Break tokens store node pointers for resume; without this cache a fresh
	// groupInlineChildrenForMulticol call would create new anonymous wrappers
	// whose pointers don't match the stored ones, causing resumeChildIdx=-1.
	if mla.node.groupedChildrenCache == nil {
		mla.node.groupedChildrenCache = groupInlineChildrenForMulticol(mla.node.Children(), mla.style)
	}
	// Phase 16.e+18 v2 B0 (contentNode pointer stability — Step 0 finding):
	// MulticolPartWalker dispatches by `child.Node == multicolContainer`
	// pointer equality. Allocating a fresh anonymous wrapper on each
	// Layout() call breaks resume from outer-column 2 onward — every
	// column-content child token references the previous outer column's
	// pointer, and the walker mis-dispatches it as a spanner. Caching
	// contentNode on mla.node mirrors the LayoutObject pointer stability
	// Blink's column-box assumes.
	if mla.node.contentNodeCache == nil {
		mla.node.contentNodeCache = &LayoutInputNode{
			style:       css.NewAnonymousBlockStyle(mla.style),
			children:    mla.node.groupedChildrenCache,
			isAnonymous: true,
		}
	}
	contentNode := mla.node.contentNodeCache

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

	// Phase 14b: defer entire multicol when its required column block-size
	// can't fit in the remaining outer fragmentainer. Under column-fill:auto
	// + explicit height, the inner column block-size is the explicit height;
	// if that exceeds the outer remaining space, fragmenting in place would
	// produce sub-columns much smaller than intended (and leak content into
	// the outer overflow area — see multicol-nested-010). Returning a 0-height
	// fragment with BlockSizeForFragmentation=explicitBlockSize makes the
	// parent's BreakBeforeChildIfNeeded push the entire multicol to the next
	// outer fragmentainer, where it has full space to lay out at its declared
	// height. Mirrors Blink's BlockSizeForFragmentation hook for nested
	// fragmentation. Only fires when starting fresh (no incoming BreakToken),
	// not balanced, has explicit height, and there is container separation
	// (the parent's break-before would be meaningful).
	if hasOuterFrag && hasExplicitBlock && mla.space.BreakToken == nil &&
		columnFill == "auto" && outerAvailable < explicitBlockSize {
		builder.SetSize(LogicalSize{
			InlineSize: contentInlineSize + geom.InlineBorderPadding(),
			BlockSize:  0,
		})
		builder.SetIntrinsicBlockSize(0)
		builder.SetLayoutNode(mla.node)
		physBorder := ToPhysicalEdges(geom.Border, wdm)
		physPadding := ToPhysicalEdges(geom.Padding, wdm)
		physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize.Float64()), wdm)
		builder.SetBoxData(&PhysicalBoxData{
			Margin:  physMargin,
			Border:  physBorder,
			Padding: physPadding,
		})
		result := builder.Build()
		result.BlockSizeForFragmentation = explicitBlockSize + geom.BlockBorderPadding()
		return result
	}

	// Phase 16.e (commit 2): walker dispatch. The 3-slot positional parser
	// (nextColToken / partialSpannerToken / colRowsResumeToken) and pure-
	// nested-resume promotion are replaced with a MulticolPartWalker that
	// iterates the parts of this multicol container in document order. Each
	// walker entry is either column-content (DescendantNode == nil; BreakToken
	// carries the resume point) or a spanner (DescendantNode == leaf spanner;
	// BreakToken carries resume info if mid-spanner). Mirrors Blink's
	// LayoutChildren loop at column_layout_algorithm.cc:605-714.
	//
	// Commit 2 of the bundled phase: only the READ site is switched. The
	// WRITE site (buildOuterBreakResult below + partialSpanner construction
	// in the spanner branch) still emits the 3-slot positional encoding, so
	// the walker reinterprets [col, partialSpanner, colRows] as a flat list
	// — this introduces semantic mismatches (spanner resume info lost when
	// MoveToSpanner clobbers walker state, pure-nested-resume tokens with
	// nil padding generate empty entries) that commit 3 fixes by flattening
	// the WRITE side.
	walker := NewMulticolPartWalker(contentNode, mla.space.BreakToken)

	// outBuilder accumulates outgoing child break tokens in document order
	// for this multicol's outgoing BlockBreakToken. Mirrors Blink's
	// container_builder_.AddBreakToken / AddBreakBeforeChild calls in
	// column_layout_algorithm.cc.
	var outBuilder MulticolBreakTokenBuilder

	// outgoingMulticolData carries Phase 18 row-phase info on the outgoing
	// outer BlockBreakToken when this multicol breaks mid-row across an outer
	// fragmentainer. Picked up by buildOuterBreakResult below. Mirrors Blink
	// cla.cc:822-833 setting MulticolBreakTokenData on the container builder
	// before ToBoxFragment.
	var outgoingMulticolData *MulticolBreakTokenData

	// pendingContentOverflow is set when a spanner's box fit in this outer
	// column but its children overflowed the fragmentainer boundary. We
	// continue placing subsequent content (other spanners, column rows)
	// before generating the outer break result at the end of the loop. The
	// spanner's content-resume token has already been pushed to outBuilder
	// at the point this flag is set; this flag merely signals that the
	// post-loop handler must emit a break result rather than completing
	// the multicol normally.
	var pendingContentOverflow bool

	// hasForcedBreakAfter is set when a spanner's break-after:column fires
	// and there is no content after the spanner to resume — the break propagates
	// to the parent context via HasForcedBreak=true on the final result.
	var hasForcedBreakAfter bool

	// buildOuterBreakResult finalises the in-progress builder and returns a
	// layout result with a BreakToken whose ChildBreakTokens are the
	// document-order flat entries accumulated in outBuilder. Mirrors
	// Blink's container_builder_.ToBoxFragment after the LayoutChildren
	// loop in column_layout_algorithm.cc:605-714.
	buildOuterBreakResult := func() *LayoutResult {
		builder.SetSize(LogicalSize{
			InlineSize: contentInlineSize + geom.InlineBorderPadding(),
			BlockSize:  blockCursor + geom.BlockBorderPadding(),
		})
		builder.SetIntrinsicBlockSize(blockCursor)
		builder.SetLayoutNode(mla.node)
		physBorder := ToPhysicalEdges(geom.Border, wdm)
		physPadding := ToPhysicalEdges(geom.Padding, wdm)
		physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize.Float64()), wdm)
		builder.SetBoxData(&PhysicalBoxData{
			Margin:  physMargin,
			Border:  physBorder,
			Padding: physPadding,
		})
		result := builder.Build()
		prevConsumed := layoutunit.LayoutUnit{}
		seqNum := 0
		if mla.space.BreakToken != nil {
			prevConsumed = mla.space.BreakToken.ConsumedBlockSize
			seqNum = mla.space.BreakToken.SequenceNumber + 1
		}
		result.BreakToken = &BlockBreakToken{
			Node:              mla.node,
			ChildBreakTokens:  outBuilder.Children(),
			MulticolData:      outgoingMulticolData,
			ConsumedBlockSize: prevConsumed.Add(layoutunit.FromFloat64Round(blockCursor)),
			SequenceNumber:    seqNum,
		}
		// Cmt-5b: when all in-flow column content has been consumed but the
		// multicol's declared block-size is not yet satisfied (blockCursor <
		// remainingContentBlockSize), outBuilder is empty — no child resume
		// tokens are needed. Set HasSeenAllChildren so the resumed walker
		// (NewMulticolPartWalker) exits immediately without re-running content.
		// Mirrors Blink's BlockBreakToken::has_seen_all_children_ semantics:
		// the resumed fragment is an empty completion fragment covering the
		// remaining declared height without re-placing any in-flow descendants.
		if outBuilder.IsEmpty() {
			result.BreakToken.HasSeenAllChildren = true
		}
		// Forward unplaced marker to the break token so the resumed fragment
		// re-seeds it. Mirrors Blink's FinishFragmentation marker plumbing.
		if builder.GetUnpositionedListMarker().IsValid() {
			result.BreakToken.HasUnpositionedListMarker = true
		}
		if len(builder.outOfFlowCandidates) > 0 {
			result.PropagatedOOFCandidates = builder.outOfFlowCandidates
		}
		if result.Fragment != nil {
			result.Fragment.RenderedColumnCount = totalColumnsRendered
		}
		return result
	}

	// flushWalker pushes any remaining walker entries to outBuilder so they
	// survive into the next outer fragmentainer. Mirrors Blink's cleanup
	// loop at column_layout_algorithm.cc:733-738 which runs after the main
	// LayoutChildren loop exits and forwards unfinished entries via
	// container_builder_.AddBreakToken / AddBreakBeforeChild. Called at
	// every site that returns a break result mid-loop, so the post-spanner
	// column-content driver token (set by walker.MoveToSpanner before the
	// spanner branch ran) and any further parent-token entries propagate
	// to the next outer column.
	flushWalker := func() {
		for !walker.IsFinished() {
			e := walker.Current()
			if e.BreakToken != nil {
				outBuilder.AddBreakToken(e.BreakToken)
			} else if e.DescendantNode != nil {
				outBuilder.AddBreakBeforeChild(e.DescendantNode, false)
			}
			walker.Next()
		}
	}

	// MulticolPartWalker dispatch loop: iterate parts (column content, spanners)
	// in document order. Each iteration handles ONE walker entry — either a
	// column-content layoutLine call OR a spanner placement. Mirrors Blink's
	// LayoutChildren loop (cla.cc:605-714).
	//
	// isFirstRow tracks the first row within a column-run (the run between
	// spanners, or before the first spanner). Phase 12f row-wrap advances
	// line_offset only after the first row in a run. Reset to true when a new
	// column-run starts after a spanner placement.
	isFirstRow := true
	for !walker.IsFinished() {
		entry := walker.Current()

		if entry.DescendantNode == nil {
			// === Column-content branch (Blink cla.cc:614-654). ===
			nextColToken := entry.BreakToken

			// Blink cla.cc:795-797 row-advance guard. Fires whenever
			// !isFirstRow OR we find ourselves past the start of the current
			// row (after a spanner that didn't quite align to a row boundary
			// even after the pre-commit snap, or in the initial position when
			// `column-height: 0` makes every offset a row-start).
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
					// No room for another row in the outer fragmentainer —
					// stop now and let the outer context resume any remaining
					// content in the next outer column.
					if nextColToken != nil {
						// walker.Current() still points to this column-
						// content entry (we never called walker.Next).
						// flushWalker pushes it (and any remaining
						// parent-token entries) in document order.
						flushWalker()
						return buildOuterBreakResult()
					}
					break
				}
			}

			rowStart := blockCursor
			rowBlockAdvance, columnsPlaced, spannerPath, remainingToken := mla.layoutLine(
				contentNode, wdm, usedColWidth, numCols, gap,
				balanceColumns, hasExplicitBlock, explicitBlockSize,
				effectiveMaxBlockSize,
				blockCursor, nextColToken, builder,
				Indefinite,
			)
			blockCursor += rowBlockAdvance
			totalColumnsRendered += columnsPlaced

			walker.Next()

			if spannerPath == nil {
				// Phase 18 WRITE site (Blink cla.cc:822-833). When the first
				// row of a column-run is truncated by the outer fragmentainer
				// (rowHeight > outerAvailable - rowStart) and column content is
				// still pending, defer to outer with a ConsumedRowBlockSize
				// carrier so the next outer fragment continues the same row at
				// the right phase. Without this, the next outer fragment would
				// start at row offset 0 and re-paint the already-consumed slice.
				if remainingToken != nil && isFirstRow && hasOuterFrag &&
					mla.shouldWrapColumns() && mla.hasRowHeight() {
					painted := outerAvailable - rowStart
					if mla.rowHeight() > painted && painted > 0 {
						outgoingMulticolData = &MulticolBreakTokenData{
							ConsumedRowBlockSize: painted,
						}
						outBuilder.AddBreakToken(remainingToken)
						flushWalker()
						return buildOuterBreakResult()
					}
				}
				// Phase 12f (Blink cla.cc:835): row-wrap — when wrap is in
				// effect and a column row exited with content still pending,
				// continue the same column-run by starting another row below
				// the one we just placed. Driver: column-height-001.html.
				// AddNextColumnBreakToken installs remainingToken as the next
				// walker entry (mirrors Blink's
				// MulticolPartWalker::AddNextColumnBreakToken).
				if remainingToken != nil && mla.shouldWrapColumns() && mla.hasRowHeight() {
					walker.AddNextColumnBreakToken(remainingToken)
					isFirstRow = false
					continue
				}
				// Phase 12c: nested multicol hit the outer fragmentainer
				// boundary with content still pending. Emit an outer break
				// result carrying the column-rows continuation so the outer
				// fragmentation context can resume this multicol in its next
				// outer column.
				if remainingToken != nil && hasOuterFrag {
					outBuilder.AddBreakToken(remainingToken)
					flushWalker()
					return buildOuterBreakResult()
				}
				break // all column content done (no outer fragmentation)
			}

			// Spanner discovered via ColumnSpannerPath. Queue it for the next
			// iteration via MoveToSpanner; remainingToken (post-spanner column
			// content, if any) is stored as the walker's nextColumnToken so it
			// becomes the iteration AFTER the spanner.
			//
			// NB: in commit 2 with the still-positional WRITE site, walker may
			// have had additional parent-token entries pending (slot[1] partial
			// spanner with resume info, slot[2] colRows). MoveToSpanner resets
			// the walker, dropping those. Commit 3 changes the WRITE site so
			// that the spanner's resume info appears as the NEXT walker entry
			// directly (no re-detect col_token at slot[0]) — at which point
			// MoveToSpanner is only called for genuinely-fresh discoveries.
			spanner := spannerLeafNode(spannerPath)
			walker.MoveToSpanner(spanner, remainingToken)
			continue
		}

		// === Spanner branch (Blink cla.cc:656-684 LayoutSpanner). ===
		spanner := entry.DescendantNode

		// Derive resume state from entry.BreakToken. With the WRITE side
		// flattened in Commit 3, each piece is a standalone flat walker
		// entry (no nested ChildBreakTokens layering for clip vs content-
		// overflow on the same spanner). entry.BreakToken IS the spanner's
		// break token directly:
		//   ConsumedBlockSize > 0 (and no children)         → clip-resume
		//   ChildBreakTokens non-empty (and Consumed == 0)  → content-overflow
		//                                                     resume; pass the
		//                                                     entry's break
		//                                                     token straight
		//                                                     to layoutSpanner-
		//                                                     InFrag so it
		//                                                     resumes from the
		//                                                     spanner's own
		//                                                     children.
		// Combined-clip cases (one spanner content-overflow followed by a
		// later spanner clipping) surface as TWO consecutive walker entries
		// in document order — no [0]/[1] index peek required.
		var spannerConsumed float64
		hasSpannerResume := false
		var spannerContentBreakToken *BlockBreakToken
		if entry.BreakToken != nil {
			hasSpannerResume = true
			spannerConsumed = entry.BreakToken.ConsumedBlockSize.Float64()
			if len(entry.BreakToken.ChildBreakTokens) > 0 {
				spannerContentBreakToken = entry.BreakToken
			}
		}

		// Forced break-before:column on spanner: propagate to the parent
		// column context. Only on fresh layout (not resumed) so the break
		// fires once.
		if hasOuterFrag && mla.space.BreakToken == nil &&
			spanner.Style() != nil && spanner.Style().GetBreakBefore() == "column" {
			var result *LayoutResult
			if blockCursor == 0 {
				// No column content was placed yet — produce a zero-height
				// fragment so borders are not painted in this outer column
				// (they belong to the resumed fragment in the next outer
				// column).
				builder.SetSize(LogicalSize{
					InlineSize: contentInlineSize + geom.InlineBorderPadding(),
					BlockSize:  0,
				})
				builder.SetIntrinsicBlockSize(0)
				builder.SetLayoutNode(mla.node)
				physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize.Float64()), wdm)
				builder.SetBoxData(&PhysicalBoxData{Margin: physMargin})
				result = builder.Build()
				result.BreakToken = &BlockBreakToken{
					Node: mla.node,
					ChildBreakTokens: []*BlockBreakToken{{
						Node:          spanner,
						IsBreakBefore: true,
						IsForcedBreak: true,
					}},
				}
			} else {
				// Advance the walker past this spanner entry so the
				// outgoing break token doesn't re-break-before it; then
				// emit a synthetic break-before-spanner and flush any
				// remaining walker entries (e.g., MoveToSpanner-stored
				// post-spanner column-content).
				walker.Next()
				outBuilder.AddBreakBeforeChild(spanner, true)
				flushWalker()
				result = buildOuterBreakResult()
			}
			result.HasForcedBreak = true
			return result
		}

		// Outer fragmentation: no room for any of this spanner — break
		// before this spanner.
		if hasOuterFrag && blockCursor >= outerAvailable {
			walker.Next()
			outBuilder.AddBreakBeforeChild(spanner, false)
			flushWalker()
			return buildOuterBreakResult()
		}

		// Compute the spanner's full block size, accounting for mid-spanner
		// resumption.
		var spanFrag *PhysicalFragment
		var spanHeight float64
		var spanResult *LayoutResult // for PropagateBaselineFromChild (Track B)
		var didContentOverflowResume bool
		if hasSpannerResume {
			if spannerContentBreakToken != nil {
				// Content-overflow resume: the spanner's box was fully placed
				// in the previous outer column but its children overflowed.
				// Re-layout the spanner from the child break point in the new
				// outer column.
				fragOff := mla.space.FragmentainerOffset + blockCursor
				resumeResult := mla.layoutSpannerInFrag(spanner, contentInlineSize, wdm,
					mla.space.FragmentainerBlockSize, fragOff, spannerContentBreakToken)
				if resumeResult != nil && resumeResult.Fragment != nil {
					spanFrag = resumeResult.Fragment
					spanHeight = NewLogicalFragment(wdm, resumeResult.Fragment).BlockSize()
					spanResult = resumeResult
				}
				didContentOverflowResume = true
			} else {
				// Clip resume: the spanner was visually truncated in the
				// previous outer column. Layout at full size and take the
				// remaining portion.
				fullResult := mla.layoutSpanner(spanner, contentInlineSize, wdm)
				if fullResult != nil && fullResult.Fragment != nil {
					fullHeight := NewLogicalFragment(wdm, fullResult.Fragment).BlockSize()
					spanHeight = fullHeight - spannerConsumed
					if spanHeight < 0 {
						spanHeight = 0
					}
					frag := fullResult.Fragment
					if wdm.IsHorizontal() {
						frag.Size.Height = layoutunit.FromFloat64Round(spanHeight)
					} else {
						frag.Size.Width = layoutunit.FromFloat64Round(spanHeight)
					}
					spanFrag = frag
					spanResult = fullResult
				}
			}
		} else {
			var fullResult *LayoutResult
			if hasOuterFrag {
				fragOff := mla.space.FragmentainerOffset + blockCursor
				fullResult = mla.layoutSpannerInFrag(spanner, contentInlineSize, wdm,
					mla.space.FragmentainerBlockSize, fragOff, nil)
			} else {
				fullResult = mla.layoutSpanner(spanner, contentInlineSize, wdm)
			}
			if fullResult != nil && fullResult.Fragment != nil {
				spanFrag = fullResult.Fragment
				spanHeight = NewLogicalFragment(wdm, fullResult.Fragment).BlockSize()
				spanResult = fullResult
				// Content overflow: spanner box fits physically but its
				// children overflow the outer fragmentainer boundary. Push
				// the spanner's OWN block-layout break token directly to
				// outBuilder (Node is already the spanner; ChildBreakTokens
				// are the spanner's content-resume children;
				// HasSeenAllChildren / SequenceNumber / ConsumedBlockSize
				// are preserved for BLA's resume bookkeeping). Mirrors
				// Blink's container_builder_.AddBreakToken(child_token)
				// which forwards the spanner's break token directly without
				// a wrapper. Continue placing subsequent content in this
				// outer column before generating the outer break result;
				// the post-loop pendingContentOverflow handler emits the
				// final result.
				if hasOuterFrag && fullResult.BreakToken != nil && blockCursor+spanHeight <= outerAvailable {
					pendingContentOverflow = true
					outBuilder.AddBreakToken(fullResult.BreakToken)
				}
			}
		}

		if spanFrag != nil {
			// Blink cla.cc:1427-1459 (pre-commit row snap). Snap blockCursor
			// to the next row-stride boundary if we're past the start of the
			// current column row, so the spanner lands on a row boundary.
			if mla.shouldWrapColumns() && mla.hasRowHeight() && mla.rowHeight() > 0 &&
				mla.offsetInCurrentRow(blockCursor) > 0 {
				blockCursor += mla.offsetToNextRow(blockCursor)
			}
			// Outer fragmentation: spanner doesn't fit — clip and break.
			if hasOuterFrag && blockCursor+spanHeight > outerAvailable {
				available := outerAvailable - blockCursor
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
				// Advance the walker past this spanner entry; the clip
				// token below replaces it for the next outer column.
				walker.Next()
				if pendingContentOverflow {
					// Combined content-overflow + clip: a previous spanner's
					// content-overflow entry is already in outBuilder; push
					// THIS spanner's clip token as a separate flat entry so
					// the resumed outer column walks both in document order.
					outBuilder.AddBreakToken(&BlockBreakToken{
						Node:              spanner,
						ConsumedBlockSize: layoutunit.FromFloat64Round(available),
					})
					// Fall through to post-loop pendingContentOverflow
					// handler, which calls flushWalker before emitting.
					break
				}
				outBuilder.AddBreakToken(&BlockBreakToken{
					Node:              spanner,
					ConsumedBlockSize: layoutunit.FromFloat64Round(available),
				})
				// Flush any remaining walker entries (e.g., MoveToSpanner-
				// stored post-spanner column-content driver) so the next
				// outer column resumes with both the clipped spanner and
				// the un-enumerated post-spanner content.
				flushWalker()
				return buildOuterBreakResult()
			}

			// Apply spanner margin-block-start. Skip when resuming a
			// content-overflow spanner — margins were consumed in OC1.
			if !didContentOverflowResume && spanner.Style() != nil {
				spannerMargins := ResolveMargins(spanner.Style(), wdm, mla.space.AvailableSize.InlineSize.Float64())
				blockCursor += spannerMargins.BlockStart
			}
			mla.addMainGap(blockCursor, SpannerGapStart)
			mla.addNumberOfColumnsForCurrentRow(-1)
			builder.AddChild(spanFrag, LogicalOffset{
				InlineOffset: 0,
				BlockOffset:  blockCursor,
			})
			if spanResult != nil {
				mla.propagateBaselineFromChild(spanResult, builder, blockCursor)
			}
			if spanFrag != nil && spanResult != nil {
				mla.attemptToPositionListMarker(spanFrag, spanResult, builder, blockCursor)
			}
			blockCursor += spanHeight
			if !didContentOverflowResume && spanner.Style() != nil {
				spannerMargins := ResolveMargins(spanner.Style(), wdm, mla.space.AvailableSize.InlineSize.Float64())
				blockCursor += spannerMargins.BlockEnd
			}
			mla.addMainGap(blockCursor, SpannerGapEnd)
		}

		// Advance the walker past this spanner entry. After Next(), the
		// walker either points to the next part (post-spanner column content
		// queued by MoveToSpanner, or a subsequent parent-token entry) or is
		// finished.
		walker.Next()

		// Forced break-after:column on spanner: propagate to the parent
		// column context. Only on fresh layout (not resumed) so the break
		// fires once.
		if hasOuterFrag && spanFrag != nil && !didContentOverflowResume &&
			mla.space.BreakToken == nil && !hasSpannerResume &&
			spanner.Style() != nil && spanner.Style().GetBreakAfter() == "column" {
			if !walker.IsFinished() {
				// walker.Next was already called above; current is the
				// post-spanner walker entry (column-content from
				// MoveToSpanner-stored next_column_token, or further
				// parent-token entries). Flush them all so the next outer
				// column resumes correctly.
				flushWalker()
				result := buildOuterBreakResult()
				result.HasForcedBreak = true
				return result
			}
			// Nothing follows — propagate break-after to the parent context.
			hasForcedBreakAfter = true
			break
		}

		// Reset isFirstRow so the next iteration's column-content branch
		// (the post-spanner column run) doesn't apply the row-advance guard
		// before placing its first row.
		isFirstRow = true
	}

	// If a spanner's children overflowed the outer fragmentainer boundary, emit
	// a break token so the outer multicol resumes the spanner's content in the
	// next outer column. This is checked after the loop so that all content for
	// this outer column (spanners, column rows following the overflowing spanner)
	// is placed before the break is recorded. The spanner's content-resume
	// token (and any combined-clip clip token) is already in outBuilder; we
	// only need to flush any leftover walker entries (e.g., MoveToSpanner-
	// stored post-spanner column-content driver) and finalize.
	if pendingContentOverflow {
		flushWalker()
		return buildOuterBreakResult()
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

	// Phase 25 Cmt-2: when a nested multicol finishes with deferred OOF
	// fragmentainer descendants, register a pending-multicol entry on its
	// outgoing fragment so the outer fragmentation root knows to revisit
	// this multicol via HandleMulticolsWithPendingOOFs. The descendants
	// themselves stay on builder.oofPositionedFragmentainerDescendants and
	// are emitted via FragmentedOofData at Build() time. Mirrors Blink's
	// column_layout_algorithm.cc:391-414 (`InvolvedInBlockFragmentation`
	// branch).
	//
	// Cmt-2 is behaviorally inert: HasOutOfFlowFragmentainerDescendants()
	// never returns true until Cmt-3 lands the OOF-promotion logic in
	// OutOfFlowLayoutPart.HandleFragmentation. MulticolOffset is left zero
	// here; PropagateOOFFragmentainerDescendants accumulates the offset
	// incrementally as the entry walks up through ancestor BLAs.
	nestedDeferredOOFs := mla.space.HasBlockFragmentation && builder.HasOutOfFlowFragmentainerDescendants()
	if nestedDeferredOOFs {
		builder.AddMulticolWithPendingOOFs(mla.node, &MulticolWithPendingOOFs{})
	}

	// Handle out-of-flow candidates. When nestedDeferredOOFs is set the
	// complete-path LayoutCandidates is skipped — the outer fragmentation
	// root resolves them per fragmentainer in Cmt-3.
	var propagatedOOF []OutOfFlowCandidate
	if len(builder.outOfFlowCandidates) > 0 {
		isPositioned := mla.style != nil && mla.style.GetPosition() != css.PositionStatic
		if isPositioned && !nestedDeferredOOFs {
			var absoluteCandidates, fixedCandidates []OutOfFlowCandidate
			for _, cand := range builder.outOfFlowCandidates {
				if cand.IsFixedPosition {
					fixedCandidates = append(fixedCandidates, cand)
				} else {
					absoluteCandidates = append(absoluteCandidates, cand)
				}
			}
			// Phase 25 Cmt-3: when this positioned multicol is itself nested
			// inside another fragmentation context, abspos descendants whose
			// CB is this multicol must wait for the outer drain. Promote them
			// to fragmentainer descendants. Mirrors Blink's
			// `out_of_flow_layout_part.cc:1158-1170`. After promotion,
			// nestedDeferredOOFs becomes true (via HasOutOfFlowFragmentainerDescendants)
			// so the AddMulticolWithPendingOOFs registration below also fires.
			if mla.space.HasBlockFragmentation && len(absoluteCandidates) > 0 {
				for _, cand := range absoluteCandidates {
					builder.AddOutOfFlowFragmentainerDescendant(LogicalOofNodeForFragmentation{
						Candidate: cand,
					})
				}
				absoluteCandidates = nil
				if !nestedDeferredOOFs && builder.HasOutOfFlowFragmentainerDescendants() {
					builder.AddMulticolWithPendingOOFs(mla.node, &MulticolWithPendingOOFs{})
					nestedDeferredOOFs = true
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

	// Phase 25 Cmt-4 (was Phase 22 Cmt-B): outer fragmentainer exhausted
	// but explicit content block-size remains. Mirrors Blink's
	// post-LayoutChildren remaining_content_block_size_ break check
	// (column_layout_algorithm.cc). Placed after the OOF promotion +
	// pending-multicol registration above so the outgoing break fragment
	// carries the FragmentedOofData (descendants + pending-multicol entry)
	// the outer drain consumes — Cmt-3's pipeline. Without this guard the
	// inner exits "complete" with no BreakToken when content was clamped
	// to outerAvailable, and the outer reruns it from scratch in the next
	// column.
	if hasOuterFrag && hasExplicitBlock && blockCursor >= outerAvailable &&
		blockCursor < mla.remainingContentBlockSize {
		return buildOuterBreakResult()
	}

	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})
	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize.Float64()), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:  physMargin,
		Border:  physBorder,
		Padding: physPadding,
	})
	builder.SetIntrinsicBlockSize(blockCursor)

	// Callsite 2: place any unclaimed list marker before building.
	// Mirrors Blink cla.cc:383 PositionAnyUnclaimedListMarker() call.
	mla.positionAnyUnclaimedListMarker(builder, &blockCursor)

	// GapGeometry (Track A): build and attach gap geometry before Build().
	// Mirrors Blink cla.cc:420–485.
	mla.buildGapGeometry(builder, contentInlineSize, finalBlockSize, geom)

	// Phase 25 Cmt-3: drain pending fragmentainer descendants and
	// MulticolsWithPendingOOFs at the outermost fragmentation context root.
	// Mirrors Blink's `OutOfFlowLayoutPart::HandleFragmentation` invoked by
	// `OutOfFlowLayoutPart::Run` (out_of_flow_layout_part.cc:589-695).
	// No-op when this builder is not the root or has no pending entries.
	mla.HandleOofFragmentation(builder)

	result := builder.Build()
	result.PropagatedOOFCandidates = propagatedOOF
	if hasForcedBreakAfter {
		result.HasForcedBreak = true
	}
	if result.Fragment != nil {
		result.Fragment.RenderedColumnCount = totalColumnsRendered
		// Phase 20 P20.5: tag the multicol container so the paint layer
		// can apply Blink's structural overflow clip. Mirrors the
		// HasNonVisibleOverflow()=true condition that Blink's
		// LayoutBox sets for any multicol fragmentation context — the
		// paint-property tree then installs an OverflowClip at
		// LayoutBox::OverflowClipRect, which for a non-scroll
		// container is border-box contracted by border outsets, i.e.
		// the padding-box (verified against blink/renderer/core/
		// layout/layout_box.cc::OverflowClipRect).
		//
		// Replaces the 3389efe7 broad ClipContentToBorderBox flag.
		// The structural change is twofold:
		//   1. Rect choice: padding-box (P20.5 painter) instead of
		//      border-box (3389efe7).
		//   2. Layered model: multicol clip is no longer a special
		//      flag piggybacked on table-cell semantics; multicol
		//      and CSS Tables 3 §5.4.1 cell clipping live on
		//      separate flags with separate rect choices.
		//
		// Gate retained at finalBlockSize > 0 so zero-height multicol
		// containers (multicol-zero-height-002 — content is explicitly
		// expected to overflow visibly) don't get clipped. Closing
		// remaining content-overflow regressions (inline-block-and-
		// column-span-all etc.) is the job of P20.6 (TallestUnbreakable
		// for atomic inlines), which makes the multicol grow to its
		// real content extent so the clip stops cutting legitimate
		// content.
		if finalBlockSize > 0 {
			result.Fragment.IsMulticolContainer = true
		}
	}
	// v2 Path X (Blink cla.cc:1706-1712): nested column balancing — when
	// our outer fragmentation context is in its initial balancing pass,
	// forward the accumulated tallest-unbreakable so the outer's column
	// auto-block-size is at least that tall. Without this, an outer
	// multicol balancing an inner multicol that contains a monolithic
	// descendant won't grow its columns to fit.
	if mla.space.IsInitialColumnBalancingPass && mla.lastMeasuredTallestUnbreakable > 0 {
		result.TallestUnbreakableBlockSize = max(
			result.TallestUnbreakableBlockSize,
			mla.lastMeasuredTallestUnbreakable)
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
	minimumColumnBlockSize float64,
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
		colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
	case balanceColumns:
		colBlockSize = mla.resolveColumnAutoBlockSize(contentNode, wdm, usedColWidth, numCols, gap, nextColToken, mla.containerPercentResolutionBlockSize)
		colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
	case hasExplicitBlock:
		colBlockSize = mla.constrainColumnBlockSize(explicitBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
	case maxBlockSize != Indefinite:
		colBlockSize = mla.constrainColumnBlockSize(maxBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
	default:
		if minimumColumnBlockSize != Indefinite {
			colBlockSize = mla.constrainColumnBlockSize(minimumColumnBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
		} else {
			colBlockSize = Indefinite
		}
	}

	var maxColHeight float64
	var finalColumns []struct {
		fragment       *PhysicalFragment
		offset         LogicalOffset
		intrinsicBlock float64
		propagatedOOF  []OutOfFlowCandidate
		result         *LayoutResult // for PropagateBaselineFromChild
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
			result         *LayoutResult // for PropagateBaselineFromChild
		}
		minSpaceShortage := math.MaxFloat64
		hasShortage := false
		actualColumnCount := 0
		forcedBreakCount := 0
		hasViolatingBreak := false
		rowHasDeferredOOFs := false

		// Reset break token to the incoming token at the start of each iteration.
		colBreakToken := nextColToken
		inlineOffset := 0.0

		// Inner per-column loop.
		for col := 0; col < numCols || colBlockSize == Indefinite; col++ {
			// IsInsideBalancedColumns is true only when the balance algorithm
			// actually determined the column height (not when column-height is
			// explicit). Prevents fragment background extension (Change 3) from
			// firing in explicit column-height contexts (CSS multicol-2).
			colSpace := mla.createConstraintSpaceForColumn(wdm, usedColWidth, colBlockSize, mla.containerPercentResolutionBlockSize, colBreakToken, balanceColumns && mla.hasAutoColumnHeight(), true)
			result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, colSpace).Layout()
			if result == nil || result.Fragment == nil {
				break
			}

			colFrag := result.Fragment
			// Tag the column fragmentainer so the painter (P20.3) and
			// column-rule painter (P20.4) can recognise it. Mirrors
			// Blink column_layout_algorithm.cc:1620 SetBoxType(kColumnBox)
			// on the per-column BlockLayoutAlgorithm.
			colFrag.BoxType = BoxTypeColumn
			colHeight := NewLogicalFragment(wdm, colFrag).BlockSize()
			// Blink does not set any per-column paint clip (BoxFragmentPainter::PaintBlockChild
			// fragmentainer branch). Phase 12h F2 partial added ClipBlockAxisOnly here to handle
			// nested-multicol leaf overflow, but that was the wrong level of abstraction — it
			// clipped monolithic pre-spanner content that legitimately overflows the balance-
			// estimate column height. Removed in Phase 16.b.3; row advance now uses
			// BlockSizeForFragmentation (Phase 16.b.2) so overflow is placed correctly.

			columns = append(columns, struct {
				fragment       *PhysicalFragment
				offset         LogicalOffset
				intrinsicBlock float64
				propagatedOOF  []OutOfFlowCandidate
				result         *LayoutResult
			}{
				fragment:       colFrag,
				offset:         LogicalOffset{InlineOffset: inlineOffset, BlockOffset: lineOffset},
				intrinsicBlock: result.IntrinsicBlockSize,
				propagatedOOF:  result.PropagatedOOFCandidates,
				result:         result,
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
			// Phase 25 Cmt-5c: track whether any column saw deferred OOF
			// descendants this row, so we keep emitting empty trailing
			// columns the outer drain needs as fragmentainers. Mirrors the
			// flat `limited_multicol_container_builder.Children()` behavior
			// (out_of_flow_layout_part.cc:1320-1373) — all inner column
			// fragmentainers must exist regardless of in-flow content,
			// otherwise OOF pieces past the last content-bearing column are
			// dropped (multicol-nested-032's GREEN-RED-GREEN-RED stripes).
			if result.Fragment != nil && result.Fragment.FragmentedOofData != nil {
				rowHasDeferredOOFs = true
			}
			if colBreakToken == nil {
				if mla.space.HasBlockFragmentation && rowHasDeferredOOFs {
					inlineOffset += usedColWidth + gap
					if col+1 >= numCols {
						break
					}
					continue
				}
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
				colBlockSize = mla.resolveColumnAutoBlockSize(contentNode, wdm, usedColWidth, numCols, gap, nextColToken, mla.containerPercentResolutionBlockSize)
				colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
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
		newColBlockSize = mla.constrainColumnBlockSize(newColBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
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

	// Phase 16.c.1: column regrowth (Blink cla.cc:1099-1124).
	//
	// When the multicol container is itself a child of an outer column
	// fragmentainer (nested multicol) and one of our columns produced more
	// block-axis content than fits in the outer fragmentainer, re-run
	// LayoutLine with the columns forced taller so they encompass the
	// overflow. Without this, a tall inner column gets visually clipped to
	// the outer fragmentainer's space — the role Phase 12h F2 partial
	// (ClipBlockAxisOnly) was filling, which has no Blink analog.
	//
	// Carrier: BlockSizeForFragmentation (set by Phase 16.b.1) — the
	// "block-axis space the column's content actually consumed including
	// monolithic overflow", which corresponds to Blink's
	// LogicalBoxFragment(...).BlockEndScrollableOverflow() in this context.
	if minimumColumnBlockSize == Indefinite &&
		mla.space.HasBlockFragmentation &&
		mla.space.BlockFragmentationType == FragmentColumn &&
		colBlockSize != Indefinite {
		// Only fire regrowth when true monolithic overflow exists in some
		// column — BSFF exceeds the column's fragment block size AND the
		// column did not break (BreakToken == nil). When content broke at
		// the column boundary, BSFF includes the trailing content that
		// fragmented into later columns — that is NOT monolithic overflow
		// and must not trigger regrowth (otherwise tests like
		// multicol-nested-030/031, where break-inside:avoid is violated
		// and content fragments into multiple columns, would be forced
		// into a single oversized column).
		var maxOverflow float64
		for _, col := range finalColumns {
			if col.result == nil || col.fragment == nil || col.result.BreakToken != nil {
				continue
			}
			fragH := NewLogicalFragment(wdm, col.fragment).BlockSize()
			if col.result.BlockSizeForFragmentation > fragH &&
				col.result.BlockSizeForFragmentation > maxOverflow {
				maxOverflow = col.result.BlockSizeForFragmentation
			}
		}
		if maxOverflow > outerRemaining && maxOverflow > colBlockSize {
			return mla.layoutLine(
				contentNode, wdm, usedColWidth, numCols, gap,
				balanceColumns, hasExplicitBlock, explicitBlockSize,
				maxBlockSize, lineOffset, nextColToken, builder,
				maxOverflow,
			)
		}
	}

	// Commit final columns to the parent builder.
	// columnsPlaced reports how many columns actually received content so the
	// column-rule painter (CSS Multicol L1 §5: rules only between columns that
	// both have content) can skip painting rules adjacent to empty columns.
	maxColHeight = 0.0
	columnsPlaced = 0
	seenOOF := map[*LayoutInputNode]bool{}
	isFirstCol := true
	for _, col := range finalColumns {
		// Phase 16.e+18 v2 B3: ClipBlockAxisOnly setter removed. Phase 12h F2
		// partial added it as a workaround for monolithic content overflow in
		// nested-multicol scenarios, before 16.d.1's per-fragment clamp + B0's
		// contentNode pointer cache + B2's TallestUnbreakableBlockSize carrier
		// closed the upstream gap. Blink has no per-column paint clip
		// (third_party/blink/renderer/core/paint/box_fragment_painter.cc:1080-
		// 1114; see findings.md § "Phase 16.d Blink research"). The field is
		// also removed by this commit; engine.go propagation + paint_layer.go
		// branch are removed in lockstep.
		builder.AddChild(col.fragment, col.offset)
		// Callsite 1 (Track B): propagate baseline from each column child.
		// Mirrors Blink cla.cc:1336 PropagateBaselineFromChild loop.
		if col.result != nil {
			mla.propagateBaselineFromChild(col.result, builder, col.offset.BlockOffset)
		}
		// Callsite 3 (Track C): attempt marker placement on first column only.
		// Mirrors Blink cla.cc:1302: "Only the first column in a line may attempt
		// to place any unpositioned list-item."
		if isFirstCol && col.result != nil {
			mla.attemptToPositionListMarker(col.fragment, col.result, builder, col.offset.BlockOffset)
			isFirstCol = false
		}
		h := NewLogicalFragment(wdm, col.fragment).BlockSize()
		// Phase 16.b.2: use BSFF for row advance — Blink parity (cla.cc:1300-1370).
		// BSFF = monolithic-overflow-aware content size, so the row advance reflects
		// actual content needs and the spanner is placed below full pre-spanner
		// content (not the pinned 6px column fragment).
		if col.result != nil && col.result.BlockSizeForFragmentation > h {
			h = col.result.BlockSizeForFragmentation
		}
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
		// Phase 25 Cmt-3: forward any deferred OOF fragmentainer descendants
		// the column carries on its content fragment (an abspos in the column
		// flow whose CB is fragmented — promoted to a fragmentainer descendant
		// at the CB's BLA). The column's offset within the multicol becomes
		// the parent-side translation for ContainingBlock.Offset accumulation.
		// StaticPosition is preserved CB-relative (see PropagateOOFFragmentainerDescendants).
		if col.fragment != nil && col.fragment.FragmentedOofData != nil {
			builder.PropagateOOFFragmentainerDescendants(
				col.fragment,
				col.offset,
				LogicalOffset{},
				layoutunit.LayoutUnit{},
				nil,
				nil,
			)
		}
	}
	// GapGeometry (Track A): record first column offset and cross gaps.
	// Mirrors Blink cla.cc:1650–1665 AddCrossGap / AddNumberOfColumnsForCurrentRow.
	hasSpannerDetector := lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil
	// Restore the row-advance cap for rows that do NOT end with a spanner.
	// Pre-spanner rows use BSFF-based maxColHeight (set above via ColumnSpannerPath
	// condition) and must NOT be capped. All other rows (normal balance, column-fill:auto,
	// explicit column-height) must cap to colBlockSize to prevent oversized containers.
	if colBlockSize != Indefinite && maxColHeight > colBlockSize && !hasSpannerDetector {
		maxColHeight = colBlockSize
	}
	if len(finalColumns) > 0 && !mla.firstColumnOffsetSet {
		mla.firstColumnOffset = finalColumns[0].offset
		mla.firstColumnOffsetSet = true
	}
	// The last finalColumn is the spanner-detector (empty) when a spanner was hit;
	// exclude it from cross gaps and from the per-row column count.
	crossGapEnd := len(finalColumns)
	if hasSpannerDetector && crossGapEnd > 0 {
		crossGapEnd--
	}
	for i := 1; i < crossGapEnd; i++ {
		// Only add a cross gap between two adjacent columns that both have content.
		// CSS spec: column rules are drawn only between columns that both have content.
		// An empty continuation column (intrinsicBlock==0) must not produce a rule.
		if finalColumns[i-1].intrinsicBlock > 0 && finalColumns[i].intrinsicBlock > 0 {
			mla.addCrossGap(finalColumns[i].offset.InlineOffset)
		}
	}
	if crossGapEnd > 0 {
		mla.addNumberOfColumnsForCurrentRow(crossGapEnd)
		if crossGapEnd > mla.maxColumnsInRow {
			mla.maxColumnsInRow = crossGapEnd
		}
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
	// AvailableSize.BlockSize is set to the container's explicit height (when
	// defined) so that percentage-height spanners (e.g. height:50%) can resolve
	// via !IsBlockSizeIndefinite() in ResolveBlockSize. Auto-height spanners are
	// not constrained: they still grow to their content size because hasExplicitBlock
	// is determined by the spanner's own CSS, not by AvailableSize.
	spannerAvailBlock := mla.containerPercentResolutionBlockSize // Indefinite when auto
	spannerWDM := NewWritingDirectionMode(spanner.Style())
	spannerSpace := NewConstraintSpaceBuilder(wdm, spannerWDM, true).
		SetAvailableSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  spannerAvailBlock,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  mla.containerPercentResolutionBlockSize,
		}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		SetIsInsideColumnSpanner(true).
		Build()
	result := layoutElement(mla.ctx, spanner, spannerSpace)
	mla.markSpannerMonolithicIfOverflowed(result, wdm)
	return result
}

// markSpannerMonolithicIfOverflowed sets PhysicalFragment.IsMonolithic on a
// spanner result when the spanner's measured intrinsic block-size exceeds
// the laid-out fragment's block-size — i.e., the spanner has an explicit
// height that's shorter than its content. The spanner's box won't grow,
// but its content paints past the box (overflow:visible default). For
// column-balance purposes we treat the spanner as monolithic of size
// IntrinsicBlockSize so the column block-size can grow to accommodate
// the visible content area.
//
// Phase 16.e+18 v2 B2.5: this is louis14's mechanism for "spanner with
// content > declared height", paired with extended
// CalculateUnbreakableBlockSize that prefers IntrinsicBlockSize over
// fragment block-size when IsMonolithic is set on a spanner-shaped
// fragment.
func (mla *MulticolLayoutAlgorithm) markSpannerMonolithicIfOverflowed(result *LayoutResult, wdm WritingDirectionMode) {
	if result == nil || result.Fragment == nil {
		return
	}
	fragBlock := NewLogicalFragment(wdm, result.Fragment).BlockSize()
	if result.IntrinsicBlockSize > fragBlock {
		result.Fragment.IsMonolithic = true
	}
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
	spannerWDM := NewWritingDirectionMode(spanner.Style())
	b := NewConstraintSpaceBuilder(wdm, spannerWDM, true).
		SetAvailableSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  Indefinite,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  mla.containerPercentResolutionBlockSize,
		}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(fragBS).
		SetFragmentainerOffset(fragOff).
		SetBlockFragmentationType(FragmentColumn).
		SetIsInsideColumnSpanner(true)
	if breakToken != nil {
		b = b.SetBreakToken(breakToken)
	}
	result := layoutElement(mla.ctx, spanner, b.Build())
	mla.markSpannerMonolithicIfOverflowed(result, wdm)
	return result
}

// resolveColumnAutoBlockSize estimates the balanced column height by doing an
// unconstrained layout of the content to get total height, then dividing by
// the number of columns. Mirrors Blink's ResolveColumnAutoBlockSizeInternal().
//
// nextColToken, if non-nil, is threaded into the measurement space so the
// unconstrained pass starts from the right position in the content stream
// (e.g., after a spanner in a multi-row layout).
// contentRun mirrors Blink's ContentRun struct (column_layout_algorithm.cc).
// Each run represents the content height between two forced column breaks.
type contentRun struct {
	contentBlockSize      float64
	implicitBreaksAssumed int
}

func (r contentRun) columnBlockSize() float64 {
	return math.Ceil(r.contentBlockSize / float64(r.implicitBreaksAssumed+1))
}

// distributeImplicitBreaks bumps implicitBreaksAssumed on the tallest run
// until len(runs)+Σimplicit >= target. Mirrors Blink's
// ContentRuns::DistributeImplicitBreaks (column_layout_algorithm.cc).
func distributeImplicitBreaks(runs []contentRun, target int) {
	for columnsFound := len(runs); columnsFound < target; columnsFound++ {
		idx := 0
		for i := 1; i < len(runs); i++ {
			if runs[i].columnBlockSize() > runs[idx].columnBlockSize() {
				idx = i
			}
		}
		runs[idx].implicitBreaksAssumed++
	}
}

func tallestColumnBlockSize(runs []contentRun) float64 {
	tallest := 0.0
	for _, r := range runs {
		if c := r.columnBlockSize(); c > tallest {
			tallest = c
		}
	}
	return tallest
}

func tallestContentBlockSize(runs []contentRun) float64 {
	tallest := 0.0
	for _, r := range runs {
		if r.contentBlockSize > tallest {
			tallest = r.contentBlockSize
		}
	}
	return tallest
}

func (mla *MulticolLayoutAlgorithm) resolveColumnAutoBlockSize(
	contentNode *LayoutInputNode,
	wdm WritingDirectionMode,
	colWidth float64,
	numCols int,
	gap float64,
	nextColToken *BlockBreakToken,
	containerPercentResolutionBlockSize float64,
) float64 {
	// When the multicol container has an explicit height, use it as the
	// available block-size for the measurement pass so that percentage-height
	// children (e.g. height:50%) can resolve against the container height.
	// Without this, AvailableSize=Indefinite causes ResolveBlockSize to return
	// auto for all percentage heights, making the balance estimate too small.
	// IsBlockSizeOverride+IsFixedBlockSize activates the CalculateInitialFragmentGeometry
	// override branch so the anonymous content block gets hasExplicitBlock=true,
	// making childAvailableBlock definite and enabling !IsBlockSizeIndefinite().
	// IsContentSuggestionLayout is skipped in this case since IsBlockSizeOverride
	// must take effect (IsContentSuggestionLayout is checked first in the
	// geometry function and would suppress the override).
	// FragmentainerBlockSize stays Indefinite so no actual fragmentation occurs
	// during measurement — we only want content heights, not column splits.
	hasExplicitContainerHeight := containerPercentResolutionBlockSize >= 0

	// buildSpace constructs a ConstraintSpace for one measure-pass iteration.
	// Called once per loop iteration with the current break-token.
	buildSpace := func(breakToken *BlockBreakToken) ConstraintSpace {
		measureAvailBlock := Indefinite
		b := NewConstraintSpaceBuilder(wdm, wdm, true)
		if hasExplicitContainerHeight {
			measureAvailBlock = containerPercentResolutionBlockSize
			b = b.SetAvailableSize(LogicalSize{
				InlineSize: colWidth,
				BlockSize:  measureAvailBlock,
			}).
				SetPercentageResolutionSize(LogicalSize{
					InlineSize: colWidth,
					BlockSize:  containerPercentResolutionBlockSize,
				}).
				SetPercentageResolutionInlineSize(colWidth).
				SetIsFixedBlockSize(true).
				SetIsBlockSizeOverride(true)
		} else {
			b = b.SetAvailableSize(LogicalSize{
				InlineSize: colWidth,
				BlockSize:  Indefinite,
			}).
				SetPercentageResolutionSize(LogicalSize{
					InlineSize: colWidth,
					BlockSize:  containerPercentResolutionBlockSize,
				}).
				SetPercentageResolutionInlineSize(colWidth).
				SetIsContentSuggestionLayout(true)
		}
		b = b.SetIsInitialColumnBalancingPass(true).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(Indefinite).
			SetBlockFragmentationType(FragmentColumn)
		if breakToken != nil {
			b = b.SetBreakToken(breakToken)
		}
		return b.Build()
	}

	// Measure-pass loop: mirrors Blink's ResolveColumnAutoBlockSizeInternal
	// (column_layout_algorithm.cc:1734-1934). Each iteration measures one
	// "tall strip" with no soft breaks allowed, advancing through the content
	// via break-tokens. One contentRun is recorded per inter-forced-break segment.
	var runs []contentRun
	breakToken := nextColToken
	forcedBreaks := 0
	considerAllColumns := false // becomes true once forcedBreaks >= numCols (mirrors ColumnsOverflowInInlineDirection)
	// Phase 16.d.2/3 (v2 B2) accumulator: max unbreakable block-size observed
	// across all measure-pass iterations. Floors the column block-size at line
	// 1639 below so columns are at least as tall as the tallest break-inside:
	// avoid (or eventually monolithic) descendant. Mirrors Blink's
	// tallest_unbreakable_block_size_ in column_layout_algorithm.cc:1879-1948.
	var tallestUnbreakable float64

	const maxIterations = 1000 // safety valve against non-advancing break-token bugs
	for i := 0; i < maxIterations; i++ {
		result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, buildSpace(breakToken)).Layout()
		if result == nil {
			break
		}

		if forcedBreaks < numCols || considerAllColumns {
			colBSize := result.BlockSizeForFragmentation
			if result.Fragment != nil {
				if fragBSize := NewLogicalFragment(wdm, result.Fragment).BlockSize(); fragBSize > colBSize {
					colBSize = fragBSize
				}
			}
			// When the fragment is forced to a fixed size (IsBlockSizeOverride),
			// fragment.BlockSize underestimates the actual content; use
			// IntrinsicBlockSize as the true content height for balance estimation.
			if result.IntrinsicBlockSize > colBSize {
				colBSize = result.IntrinsicBlockSize
			}
			runs = append(runs, contentRun{contentBlockSize: colBSize})
		}

		// Phase 16.d.2/3 (v2 B2): accumulate the carrier from each measure-
		// pass iteration. result.TallestUnbreakableBlockSize is populated by
		// BLA's child loop + BreakBeforeChildIfNeeded propagation when any
		// descendant has break-inside:avoid (or is monolithic, once that's
		// detectable). Mirrors Blink cla.cc:1879-1948.
		if result.TallestUnbreakableBlockSize > tallestUnbreakable {
			tallestUnbreakable = result.TallestUnbreakableBlockSize
		}

		if result.ColumnSpannerPath != nil {
			// Spanner detected during measure pass. Mirrors Blink
			// cla.cc:1697 — break out and let the main layout pass place
			// the spanner. The Blink recursion case (cla.cc:1690-1696,
			// forced_break_count && !knew_about_spanner) is deferred —
			// lower priority.
			//
			// Earlier attempt: also measured the spanner's content here
			// to contribute to tallestUnbreakable. Reverted — caused
			// regressions on spanner-fragmentation-000/002/010 (the extra
			// layout call had side effects on the spanner's resume state).
			// The spanner-content-overflow residuals (-004/-006) need a
			// different mechanism, possibly at the spanner-placement
			// layer rather than the measure-pass layer.
			break
		}

		if result.HasForcedBreak {
			forcedBreaks++
			// When forced breaks reach or exceed the column count, extra segments
			// will overflow inline (column count expands). Switch to considerAllColumns
			// so subsequent runs — which may be the tallest — are still recorded.
			// Mirrors Blink's ColumnsOverflowInInlineDirection(false) guard.
			if forcedBreaks >= numCols {
				considerAllColumns = true
			}
		}
		if result.BreakToken == nil {
			break
		}
		breakToken = result.BreakToken
	}

	// v2 Path X (Blink cla.cc:1706-1712): record the accumulated unbreakable
	// for outer-balancing forwarding. Read by MLA.Layout's exit path.
	mla.lastMeasuredTallestUnbreakable = tallestUnbreakable

	if len(runs) == 0 {
		return 0
	}

	// Phase 16.d.2/3 (v2 B2): floor the resolved column block-size at the
	// tallest unbreakable descendant. When the tallest unbreakable already
	// exceeds the natural per-run estimate, return it directly — no point
	// distributing implicit breaks below this floor. Mirrors Blink's
	// ResolveColumnAutoBlockSizeInternal final clamp.
	if tallestUnbreakable >= tallestContentBlockSize(runs) {
		return tallestUnbreakable
	}

	distributeImplicitBreaks(runs, numCols)

	estimate := tallestColumnBlockSize(runs)
	if estimate < 1 {
		estimate = 1
	}
	return estimate
}

// constrainColumnBlockSize clamps the candidate column height to the
// container's explicit height (if any), max-height (if set), the outer
// fragmentainer's remaining space, the row-height at this offset (Phase 12f),
// and ensures it is non-negative. Mirrors Blink's ConstrainColumnBlockSize()
// (cla.cc:1936-1968 — clamp by RemainingRowHeightAtOffset(line_offset)).
// maxBlockSize == Indefinite means the container has no max-height constraint.
// outerRemaining > 0 means columns cannot exceed this outer limit.
//
// minimumColumnBlockSize is a floor applied LAST (Blink: "Don't make the
// column smaller than what we should."). Used by the column-regrowth path
// (cla.cc:1099-1124) so a re-run of LayoutLine forces columns tall enough to
// encompass overflow detected on the previous pass — the floor overrides
// upper bounds so columns can legitimately exceed outerRemaining /
// max-height when content needs the room. Indefinite => no floor.
func (mla *MulticolLayoutAlgorithm) constrainColumnBlockSize(
	size float64,
	hasExplicitBlock bool,
	explicitBlockSize float64,
	maxBlockSize float64,
	outerRemaining float64,
	lineOffset float64,
	minimumColumnBlockSize float64,
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
	if minimumColumnBlockSize != Indefinite && size < minimumColumnBlockSize {
		size = minimumColumnBlockSize
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
	containerPercentResolutionBlockSize float64,
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
			BlockSize:  containerPercentResolutionBlockSize,
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

// PropagateBaselineFromChild propagates baseline values from a column or
// spanner child result to the multicol container builder.
//
// Mirrors Blink's ColumnLayoutAlgorithm::PropagateBaselineFromChild
// (cla.cc:1655–1677). Called at two sites:
//   1. After each column is committed in layoutLine.
//   2. After the spanner is committed in the spanner-placement block of Layout.
//
// min semantics for first baseline: the earliest column wins.
// max semantics for last baseline: the latest column wins.
// SetUseLastBaselineForInlineBaseline is called unconditionally (Blink invariant).
func (mla *MulticolLayoutAlgorithm) propagateBaselineFromChild(
	result *LayoutResult, builder *BoxFragmentBuilder, blockOffset float64) {

	if result.HasBaseline {
		cur, has := builder.FirstBaseline()
		if !has {
			cur = math.MaxFloat64
		}
		bl := math.Min(blockOffset+result.Baseline, cur)
		builder.SetFirstBaseline(bl)
	}

	if result.LastBaseline > 0 {
		bl := math.Max(blockOffset+result.LastBaseline, builder.lastBaseline)
		builder.SetLastBaseline(bl)
	}

	builder.SetUseLastBaselineForInlineBaseline()
}

// --- Track A: GapGeometry helpers (Phase 12h.6) ---
// These mirror Blink's cla.cc:1640–1763.

// addMainGap records a row-axis gap or spanner boundary in mainGaps.
// For normal row gaps (SpannerGapNone) the block offset is shifted to mid-gap.
// Mirrors Blink's ColumnLayoutAlgorithm::AddMainGap (cla.cc:1640).
func (mla *MulticolLayoutAlgorithm) addMainGap(blockOffset float64, gapType SpannerMainGapType) {
	if gapType == SpannerGapNone {
		blockOffset += mla.rowGapSize / 2
	}
	mla.mainGaps = append(mla.mainGaps, MainGap{GapOffset: blockOffset, SpannerType: gapType})
}

// spannerLeafNode traverses a ColumnSpannerPath linked list and returns the
// Box at the leaf (where Child == nil). This is the actual column-span:all
// element, which may be a grandchild or deeper descendant of the multicol
// flow thread. Mirrors Blink's ColumnSpannerPath::SpannerBox() (column_spanner_path.h).
func spannerLeafNode(path *ColumnSpannerPath) *LayoutInputNode {
	if path.Child == nil {
		return path.Box
	}
	return spannerLeafNode(path.Child)
}

// addCrossGap records an inter-column gap position in crossGaps.
// columnInlineStartOffset is the inline start of the column AFTER the gap.
// Mirrors Blink's ColumnLayoutAlgorithm::AddCrossGap (cla.cc:1650).
func (mla *MulticolLayoutAlgorithm) addCrossGap(columnInlineStartOffset float64) {
	gapCenter := columnInlineStartOffset - (mla.columnGapSize / 2)
	blockStart := 0.0
	if mla.firstColumnOffsetSet {
		blockStart = mla.firstColumnOffset.BlockOffset
	}
	mla.crossGaps = append(mla.crossGaps, CrossGap{
		GapInlineOffset: gapCenter,
		GapBlockOffset:  blockStart,
	})
}

// addNumberOfColumnsForCurrentRow records how many columns were placed in the
// current row (or -1 for a spanner row).
// Mirrors Blink's ColumnLayoutAlgorithm::AddNumberOfColumnsForCurrentRow (cla.cc:1659).
func (mla *MulticolLayoutAlgorithm) addNumberOfColumnsForCurrentRow(colsInRow int) {
	mla.columnsPerRow = append(mla.columnsPerRow, colsInRow)
	mla.hasColumnsPerRow = true
}

// updateCrossGapSegmentStates mutates crossGaps in place to flag segments
// that are blocked by a spanner (cols_in_row == -1 sentinel).
// Mirrors Blink's ColumnLayoutAlgorithm::UpdateCrossGapSegmentStates (cla.cc:1714).
func (mla *MulticolLayoutAlgorithm) updateCrossGapSegmentStates() {
	if !mla.hasColumnsPerRow {
		return
	}
	colCount := mla.style.GetColumnCount()
	hasAutoColCount := colCount == 0

	for crossIdx := range mla.crossGaps {
		for _, colsInRow := range mla.columnsPerRow {
			if colsInRow != -1 && crossIdx+1 < colsInRow {
				continue
			}
			isOutsideSpecified := !hasAutoColCount && crossIdx+1 >= colCount
			if colsInRow == -1 && !isOutsideSpecified {
				// Spanner row: block this cross-gap segment.
				if mla.crossGaps[crossIdx].EdgeState == EdgeNone {
					mla.crossGaps[crossIdx].EdgeState = EdgeStart
				} else {
					mla.crossGaps[crossIdx].EdgeState = EdgeBoth
				}
			}
		}
	}
}

// buildGapGeometry constructs a *GapGeometry from the accumulated gap state
// and attaches it to the builder. Mirrors Blink cla.cc:420–485.
// Called at the end of Layout() before builder.Build().
func (mla *MulticolLayoutAlgorithm) buildGapGeometry(
	builder *BoxFragmentBuilder,
	contentInlineSize float64,
	finalBlockSize float64,
	geom FragmentGeometry,
) {
	if len(mla.crossGaps) == 0 && len(mla.mainGaps) == 0 {
		return
	}
	if !mla.firstColumnOffsetSet {
		return
	}
	if !mla.style.HasColumnRule() {
		return
	}

	gg := &GapGeometry{
		ContainerType: GapContainerMulticol,
		MainDirection: GapForRows,
	}

	// Pad cross_gaps_ to the declared column count when layout produced fewer.
	// Mirrors Blink cla.cc:1722-1735.
	// Only pad when there is at least one actual cross gap: padding extends
	// existing gaps rightward. If there are no actual cross gaps (all rows had
	// only one content column, so no inter-column boundaries), padding would
	// invent gap positions in empty space and paint spurious column rules.
	// Use usedColWidth (computed column width) as stride so auto-width columns
	// get correct positions instead of falling back to CSS column-width: auto (0).
	declaredColCount := mla.style.GetColumnCount()
	if declaredColCount > 0 && len(mla.crossGaps) > 0 {
		for i := mla.maxColumnsInRow; i < declaredColCount; i++ {
			last := mla.crossGaps[len(mla.crossGaps)-1]
			// Stride = usedColWidth + columnGapSize.
			inlineOffset := last.GapInlineOffset + mla.columnGapSize/2 + mla.usedColWidth + mla.columnGapSize
			mla.addCrossGap(inlineOffset)
		}
	}

	// Build content inline extents.
	contentInlineEnd := contentInlineSize + geom.InlineBorderPadding() - geom.Border.InlineEnd - geom.Padding.InlineEnd
	if len(mla.crossGaps) > 0 {
		last := mla.crossGaps[len(mla.crossGaps)-1]
		if last.GapInlineOffset > contentInlineEnd {
			contentInlineEnd = last.GapInlineOffset
		}
		if mla.hasColumnsPerRow {
			mla.updateCrossGapSegmentStates()
		}
		gg.CrossGaps = mla.crossGaps
		gg.InlineGapSize = mla.columnGapSize
	}

	// Build content block extents.
	contentBlockEnd := finalBlockSize + geom.BlockBorderPadding() - geom.Border.BlockEnd - geom.Padding.BlockEnd
	if len(mla.mainGaps) > 0 {
		last := mla.mainGaps[len(mla.mainGaps)-1]
		if last.GapOffset > contentBlockEnd {
			contentBlockEnd = last.GapOffset
		}
		gg.MainGaps = mla.mainGaps
		gg.BlockGapSize = mla.rowGapSize
	}

	gg.ContentInlineStart = mla.firstColumnOffset.InlineOffset
	gg.ContentInlineEnd = contentInlineEnd
	gg.ContentBlockStart = mla.firstColumnOffset.BlockOffset
	gg.ContentBlockEnd = contentBlockEnd

	builder.SetGapGeometry(gg)
}

// attemptToPositionListMarker tries to align and place any pending list-item
// marker against the given child fragment.
//
// Mirrors Blink's ColumnLayoutAlgorithm::AttemptToPositionListMarker
// (cla.cc:1299–1311 for first-column, cla.cc:1492–1502 for spanner).
//
// The marker is placed only when the child has a baseline to align to.
// After placement, the builder's unpositioned marker is cleared.
func (mla *MulticolLayoutAlgorithm) attemptToPositionListMarker(
	child *PhysicalFragment, childResult *LayoutResult,
	builder *BoxFragmentBuilder, blockOffset float64) {

	marker := builder.GetUnpositionedListMarker()
	if !marker.IsValid() {
		return
	}
	baseline, ok := marker.ContentAlignmentBaseline(child, childResult)
	if !ok {
		return
	}
	marker.AddToBox(child, childResult, baseline, blockOffset, builder)
	builder.ClearUnpositionedListMarker()
}

// positionAnyUnclaimedListMarker places the marker at the container origin
// when no column or spanner provided a baseline to align against.
//
// Mirrors Blink's ColumnLayoutAlgorithm::PositionAnyUnclaimedListMarker
// (cla.cc:383). Called before builder.Build() at the end of Layout().
func (mla *MulticolLayoutAlgorithm) positionAnyUnclaimedListMarker(
	builder *BoxFragmentBuilder, intrinsicBlockSize *float64) {

	marker := builder.GetUnpositionedListMarker()
	if !marker.IsValid() {
		return
	}
	marker.AddToBoxWithoutLineBoxes(builder, intrinsicBlockSize)
	builder.ClearUnpositionedListMarker()
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
