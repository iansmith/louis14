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
	// minBlockSize is the multicol's resolved min-block-size; it may raise
	// the column height cap in constrainColumnBlockSize (Blink
	// cla.cc:1790-1793 ResolveInitialMinBlockLength). Resolved once in
	// Layout() because resolution needs the fragment geometry.
	minBlockSize float64
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

	// LOU-325: spanner detection captured during the most recent
	// resolveColumnAutoBlockSize measure pass. When a definite-height container
	// is fragmented by a column-span, the measure pass lays the resumed
	// container out at indefinite column height, so its content reaches the
	// spanner and BlockLayoutAlgorithm's early-return surfaces the
	// ColumnSpannerPath plus a budget-clamped resume break token
	// (block_layout.go:624-724). The subsequent render pass uses a *bounded*
	// per-column height: when the container's remaining budget is smaller than
	// the next block's content (004b's 150px budget vs block2's 200px), that
	// block fills the render columns and produces an ordinary continuation
	// token, so the render never reaches the spanner. layoutLine then falls
	// back to this captured detection result so the spanner (and the trailing
	// segments) are still placed. Mirrors Blink, where the spanner is found in
	// the flow thread's stitched space (column_layout_algorithm.cc
	// ResolveColumnAutoBlockSizeInternal hitting GetColumnSpannerPath), not the
	// per-column space. Cleared at the top of every resolveColumnAutoBlockSize
	// call; consumed once by the immediately-following render in layoutLine.
	lastMeasuredSpannerPath  *ColumnSpannerPath
	lastMeasuredSpannerToken *BlockBreakToken
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
					Item:       mla.node,
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

	// Blink cla.cc:1790-1793: a specified min-block-size may raise the column
	// height cap in constrainColumnBlockSize (min wins over max per CSS 2.1
	// §10.7). Resolved once here; when the block-size is definite the
	// min/max clamp is already folded into explicitBlockSize by
	// CalculateInitialFragmentGeometry, so this only matters for the
	// max-height (auto block-size) case.
	mla.minBlockSize = ResolveMinBlockSize(mla.style, wdm, mla.space, geom).Float64()

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
	//
	// Cmt-5e: also gated on FragmentainerOffset > 0. When the inner-mc starts
	// at the OUTER fragmentainer's origin (FragmentainerOffset == 0), the
	// outer column has already supplied a clean fresh starting offset — the
	// inner-mc should fragment in place rather than be deferred. Without this
	// narrowing, an inner-mc fresh in outer col-1 with declared > available
	// would be deferred to col-2 (where it's still fresh and would defer
	// again, infinitely). Mirrors Blink's behavior for fragmenting nested
	// multicols where the OUTER context provides the fragmentation boundaries.
	// Closure flip for `multicol-nested-011/032/033` cluster (combined with
	// Cmt-5b's chain semantics; visual closure of `-011` further requires
	// Phase 21 clip ungating).
	if hasOuterFrag && hasExplicitBlock && mla.space.BreakToken == nil &&
		columnFill == "auto" && outerAvailable < explicitBlockSize &&
		mla.space.FragmentainerOffset > 0 {
		builder.SetSize(LogicalSize{
			InlineSize: contentInlineSize + geom.InlineBorderPadding(),
			BlockSize:  0,
		})
		builder.SetIntrinsicBlockSize(0)
		builder.SetLayoutNode(mla.node)
		// Zero-height defer fragment: omit border/padding to
		// prevent top-border paint leaking into the skipped
		// fragment. Only margin is needed for outer layout.
		physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize.Float64()), wdm)
		builder.SetBoxData(&PhysicalBoxData{
			Margin:    physMargin,
			Scrollbar: PhysicalEdges{},
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
		didBreakSelf := true // Content exceeded outer fragmentainer.
		effBP := EffectiveBlockBorderPadding(geom, builder.HasClonedBoxDecorations(), mla.space.BreakToken, didBreakSelf)
		builder.SetSize(LogicalSize{
			InlineSize: contentInlineSize + geom.InlineBorderPadding(),
			BlockSize:  blockCursor + effBP,
		})
		builder.SetIntrinsicBlockSize(blockCursor)
		builder.SetLayoutNode(mla.node)
		border := geom.Border
		padding := geom.Padding
		if !builder.HasClonedBoxDecorations() {
			isContinuation := mla.space.BreakToken != nil && !mla.space.BreakToken.ConsumedBlockSize.IsZero()
			if isContinuation {
				border.BlockStart = 0
				padding.BlockStart = 0
			}
			if didBreakSelf {
				border.BlockEnd = 0
				padding.BlockEnd = 0
			}
		}
		physBorder := ToPhysicalEdges(border, wdm)
		physPadding := ToPhysicalEdges(padding, wdm)
		physScrollbar := ToPhysicalEdges(geom.Scrollbar, wdm)
		physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize.Float64()), wdm)
		builder.SetBoxData(&PhysicalBoxData{
			Margin:    physMargin,
			Border:    physBorder,
			Padding:   physPadding,
			Scrollbar: physScrollbar,
		})
		// LOU-143: emit gap geometry on every outer-break-result path so the
		// column-rule painter has accurate cross-gap positions and last-row
		// stretch info. Without this, broken-out fragments fall through to
		// drawColumnRules's ad-hoc fallback (render.go L3138), which places
		// (numCols-1) rules evenly without spanner skips or last-row stretch.
		// Self-gated: no-op when !HasColumnRule, and emits empty geometry
		// when no columns were placed (firstColumnOffsetSet=false), which
		// the painter handles. Mirrors the post-loop call at the bottom of
		// MulticolLayoutAlgorithm.Layout.
		mla.buildGapGeometry(builder, contentInlineSize, blockCursor, geom)
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
		// Cmt-5b: when outBuilder is empty AND there are no deferred OOF
		// fragmentainer descendants, the inner-mc's main loop has exhausted
		// all in-flow children with no continuation needed, but Cmt-4's
		// post-loop guard fired because declared block-size is not yet
		// satisfied. The emitted BlockBreakToken must signal that all
		// children are seen — otherwise NewMulticolPartWalker on resume
		// finds parent token with no BreakToken entry but
		// HasSeenAllChildren=false and starts iterating in-flow children
		// from scratch, re-painting content already placed (the
		// `multicol-nested-011` symptom under Cmt-A). Gated on the OOF
		// flag so OOF-deferred resume paths in nested OOF tests
		// (e.g. `multicol-nested-032`) are not short-circuited; OOF
		// descendants are placed via `FragmentedOofData` propagation,
		// not the in-flow walker, but their resume timing still depends
		// on HasSeenAllChildren=false. Mirrors Blink's
		// `BlockBreakToken::has_seen_all_children_` semantics
		// (`block_break_token.h:120-160`).
		if outBuilder.IsEmpty() && !builder.HasOutOfFlowFragmentainerDescendants() {
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

	// marginStrut collapses adjacent spanner block margins and lets a
	// spanner's trailing margin collapse through an empty column line.
	// Mirrors the MarginStrut threaded through Blink's LayoutChildren walk
	// (cla.cc:551-708 @ a9f50e522efa). Column content itself never
	// contributes (columns establish a BFC root, cla.cc:554-558 comment).
	var marginStrut MarginStrut
	for !walker.IsFinished() {
		entry := walker.Current()

		if entry.DescendantNode == nil {
			// === Column-content branch (Blink cla.cc:614-654). ===
			nextColToken := entry.BreakToken

			// Blink cla.cc:716-724: the line offset includes any trailing
			// margin from a preceding spanner. The strut is NOT reset yet —
			// if the line turns out to be empty, the margin collapses
			// through it (and as far as the spec is concerned, the line
			// won't even exist).
			lineOffset := blockCursor + marginStrut.Resolve()

			// Blink cla.cc:795-797 row-advance guard. Fires whenever
			// !isFirstRow OR we find ourselves past the start of the current
			// row (after a spanner that didn't quite align to a row boundary
			// even after the pre-commit snap, or in the initial position when
			// `column-height: 0` makes every offset a row-start).
			needsRowAdvance := !isFirstRow
			if !needsRowAdvance && mla.shouldWrapColumns() && mla.hasRowHeight() &&
				mla.rowHeight() > 0 &&
				mla.remainingRowHeightAtOffset(lineOffset) <= 0 {
				needsRowAdvance = true
			}
			if needsRowAdvance {
				if mla.hasRowHeight() {
					lineOffset += mla.offsetToNextRow(lineOffset)
				}
				if hasOuterFrag && mla.hasRowHeight() &&
					mla.rowHeight() > outerAvailable-lineOffset {
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

			rowStart := lineOffset
			rowBlockAdvance, columnsPlaced, spannerPath, remainingToken, lineIsEmpty := mla.layoutLine(
				contentNode, wdm, usedColWidth, numCols, gap,
				balanceColumns, hasExplicitBlock, explicitBlockSize,
				effectiveMaxBlockSize,
				lineOffset, nextColToken, builder,
				Indefinite, EffectiveBlockBorderPadding(geom, builder.HasClonedBoxDecorations(), mla.space.BreakToken, false),
			)
			// Blink cla.cc:1233-1251: only a non-empty line updates the
			// layout position (which already incorporates the strut) and
			// resets the strut. An empty line — e.g. the spanner-detection
			// line between two adjacent spanners — leaves both untouched so
			// the pending margin collapses through.
			if !lineIsEmpty {
				blockCursor = lineOffset + rowBlockAdvance
				marginStrut = MarginStrut{}
			}
			totalColumnsRendered += columnsPlaced

			// column-fill:auto + spanner: once any row encountered a spanner,
			// subsequent rows must also balance — but ONLY when the multicol has
			// auto height (no explicit block-size constraint). For fixed-height
			// multicol + column-fill:auto + spanner, post-spanner content is NOT
			// balanced (per WPT no-balancing-after-column-span); each post-spanner
			// row fills to the remaining row height sequentially.
			// Mirrors Blink's HasKnownFragmentainerBlockSize gate in
			// column_layout_algorithm.cc — when block-size is auto the algorithm
			// must balance to determine row heights even after a spanner.
			if spannerPath != nil && !balanceColumns && !hasExplicitBlock {
				balanceColumns = true
			}

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
					// Blink overflow formula: how much of the row exceeds the outer
					// available space. Only emit a carrier when overflow > 0.
					overflow := mla.remainingRowHeightAtOffset(rowStart) - (outerAvailable - rowStart)
					if overflow > 0 {
						outgoingMulticolData = &MulticolBreakTokenData{
							ConsumedRowBlockSize: mla.rowHeight() - overflow,
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
					// Record the row gap boundary for column-rule painting
					// (Blink: gap_accumulator_->AddMainGap when has_wrapped &&
					// row_gap_size_ > LayoutUnit()). blockCursor is the end of the
					// row just laid out; the gap extends from there by rowGapSize.
					if mla.rowGapSize > 0 {
						mla.addMainGap(blockCursor, SpannerGapNone)
					}
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
		// LOU-111 step 6.6.A: after the unified resume routing below
		// forwards entry.BreakToken straight into layoutSpannerInFrag,
		// the per-case decoding (spannerConsumed / spannerContentBreakToken)
		// is no longer needed at the multicol level — the spanner's BLA
		// reads incomingBreakToken.ConsumedBlockSize and ChildBreakTokens
		// itself and resumes accordingly.
		hasSpannerResume := entry.BreakToken != nil

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
				builder.SetBoxData(&PhysicalBoxData{Margin: physMargin, Scrollbar: PhysicalEdges{}})
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

		// Blink cla.cc:1339-1347 (@ a9f50e522efa): resolve the spanner's
		// margins against the multicol's content-box inline size and collapse
		// its block-start margin into the running strut BEFORE computing the
		// spanner's block offset. A resumed spanner has already consumed its
		// block-start margin, except when resuming from a forced break-before
		// (AdjustMarginsForFragmentation, fragmentation_utils.h:279-289).
		spannerStyle := spanner.Style()
		var spannerMargins LogicalEdges
		if spannerStyle != nil {
			spannerMargins = ResolveMargins(spannerStyle, wdm, contentInlineSize)
		}
		if hasSpannerResume &&
			(!entry.BreakToken.IsBreakBefore || !entry.BreakToken.IsForcedBreak) {
			spannerMargins.BlockStart = 0
		}
		marginStrut.Append(spannerMargins.BlockStart)
		// Blink cla.cc:1354-1356: block_offset = intrinsic_block_size_ +
		// margin_strut->Sum(). This collapsed strut (a preceding spanner's
		// block-end margin plus this spanner's block-start margin) feeds the
		// outer-fragmentation break decision and the spanner's fragmentainer
		// offset (cla.cc:1402-1403: FragmentainerOffsetForChildren() +
		// block_offset) as well as final placement, so it must be resolved
		// before the break-before / fragOff sites below — not left as a bare
		// blockCursor that under-counts by the pending margin.
		pendingMargin := marginStrut.Resolve()

		// Outer fragmentation: no room for any of this spanner (including its
		// collapsed block-start margin) — break before this spanner.
		if hasOuterFrag && blockCursor+pendingMargin >= outerAvailable {
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
		// LOU-111 step 6.6.A — unified spanner dispatch.
		//
		// Pre-6.6.A this branch was three-way:
		//   - hasSpannerResume && spannerContentBreakToken != nil (:822-835):
		//     content-overflow resume via layoutSpannerInFrag with the
		//     entry's break token.
		//   - hasSpannerResume && spannerContentBreakToken == nil
		//     (:836-855): Fab A clip-resume via layoutSpanner (NON-fragmented)
		//     + geometric clip of the resulting fragment.
		//   - !hasSpannerResume (:857-887): fresh layout via
		//     layoutSpannerInFrag (or layoutSpanner if no outer fragmentation).
		//
		// After LOU-111 step 6.5, the spanner's BLA emits a uniform
		// LayoutResult.BreakToken carrying ConsumedBlockSize +
		// optionally ChildBreakTokens for every resume case (S5/S6 in the
		// audit's case table). Forwarding entry.BreakToken straight into
		// layoutSpannerInFrag handles both clip-resume and content-overflow-
		// resume uniformly — the BLA's prev_consumed math and Phase 16.d.1
		// clamp produce a correctly-sized fragment. The geometric clip
		// post-mutation that Fab A used to do is no longer needed; the
		// Fab A clip body at :899-936 is also redundant and goes in 6.6.B.
		var fullResult *LayoutResult
		if hasOuterFrag || hasSpannerResume {
			fragOff := mla.space.FragmentainerOffset + blockCursor + pendingMargin
			fullResult = mla.layoutSpannerInFrag(spanner, contentInlineSize, wdm,
				mla.space.FragmentainerBlockSize, fragOff, entry.BreakToken)
		} else {
			fullResult = mla.layoutSpanner(spanner, contentInlineSize, wdm)
		}
		if fullResult != nil && fullResult.Fragment != nil {
			spanFrag = fullResult.Fragment
			spanHeight = NewLogicalFragment(wdm, fullResult.Fragment).BlockSize()
			spanResult = fullResult
			// Spanner emitted a break-token (box clamp / content-overflow /
			// continuation): push it straight to outBuilder so the next
			// outer fragmentainer resumes the spanner. Mirrors Blink's
			// container_builder_.AddBreakToken(child_token).
			if hasOuterFrag && fullResult.BreakToken != nil && blockCursor+pendingMargin+spanHeight <= outerAvailable {
				pendingContentOverflow = true
				outBuilder.AddBreakToken(fullResult.BreakToken)
			}
		}
		if spanFrag != nil {
			// Blink cla.cc:1364-1374 (pre-commit row snap). Snap blockCursor
			// to the next row-stride boundary only when the spanner (with its
			// pending collapsed margin) doesn't fit in the remaining space of
			// the current row.
			if mla.shouldWrapColumns() && mla.hasRowHeight() && mla.rowHeight() > 0 &&
				mla.offsetInCurrentRow(blockCursor+pendingMargin) > 0 {
				remainingRow := mla.remainingRowHeightAtOffset(blockCursor)
				if pendingMargin+spanHeight > remainingRow {
					blockCursor += mla.offsetToNextRow(blockCursor)
				}
			}
			// Blink cla.cc:1354-1356: the spanner's block offset includes the
			// collapsed strut. cla.cc:1439-1441: the start-spanner gap
			// boundary is the end of the preceding content — margins live
			// inside the gap.
			spannerBlockOffset := blockCursor + pendingMargin
			mla.addMainGap(blockCursor, SpannerGapStart)
			mla.addNumberOfColumnsForCurrentRow(-1)
			spannerOffset := LogicalOffset{
				InlineOffset: resolveSpannerInlineMargins(spannerStyle, wdm, contentInlineSize,
					NewLogicalFragment(wdm, spanFrag).InlineSize(), spannerMargins),
				BlockOffset: spannerBlockOffset,
			}
			builder.AddChild(spanFrag, spannerOffset)
			// LOU-111 step 4: propagate the spanner's BSFF into this
			// multicol's running BSFF so an outer multicol slicing this
			// multicol's fragments sees the true content-overflow extent
			// (analog of column_layout_algorithm.cc:977-982 +
			// fragmentation_utils.cc BlockSizeForFragmentation helper).
			// Without this, a spanner whose content overflows past its
			// declared box reports only its box-size up the chain, and
			// the outer multicol's per-column row-advance picks the wrong
			// slice height.
			if spanResult != nil {
				builder.PropagateChildBlockSizeForFragmentation(spanResult, spannerOffset)
				mla.propagateBaselineFromChild(spanResult, builder, spannerBlockOffset)
			}
			if spanResult != nil {
				mla.attemptToPositionListMarker(spanFrag, spanResult, builder, spannerBlockOffset)
			}
			// Blink cla.cc:1436-1443: advance past the spanner box; reset the
			// strut and seed it with the block-end margin, which collapses
			// with a following spanner or positions the next column line —
			// and is dropped when this fragmentainer breaks inside the
			// spanner (the broke path never flushes the strut).
			blockCursor = spannerBlockOffset + spanHeight
			marginStrut = MarginStrut{}
			marginStrut.Append(spannerMargins.BlockEnd)
			mla.addMainGap(blockCursor+marginStrut.Resolve(), SpannerGapEnd)
		}

		// Advance the walker past this spanner entry. After Next(), the
		// walker either points to the next part (post-spanner column content
		// queued by MoveToSpanner, or a subsequent parent-token entry) or is
		// finished.
		walker.Next()

		// Forced break-after:column on spanner: propagate to the parent
		// column context. Only on fresh layout (not resumed) so the break
		// fires once.
		if hasOuterFrag && spanFrag != nil && !hasSpannerResume &&
			mla.space.BreakToken == nil &&
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

	// Blink cla.cc:687-705: all children seen — the leftover strut (e.g. a
	// trailing spanner's block-end margin) contributes to the intrinsic
	// block-size. Break paths (walker unfinished) drop the strut instead.
	if walker.IsFinished() {
		blockCursor += marginStrut.Resolve()
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
	if hasExplicitBlock && !hasOuterFrag {
		// A definite-height multicol is sized by its CSS height, not its
		// content: Blink's ComputeBlockSizeForFragment resolves the specified
		// block-size regardless of content extent, so content that exceeds it
		// (e.g. an overflowing post-spanner spanner in
		// multicol-span-all-children-height-003) overflows the box rather than
		// stretching it. LOU-370: clamp DOWN to explicitBlockSize as well as up
		// (the prior code only bumped up when content was shorter).
		finalBlockSize = explicitBlockSize
	}
	// CSS Contain 1 §4.2: size containment treats the multicol container as
	// if it had no in-flow content for sizing purposes. With auto block-size
	// the cumulative blockCursor (from column heights) does NOT contribute
	// to the container's block-size — only `min-height` / explicit `height`
	// remains. Mirrors Blink's
	// `length_utils.cc :: CalculateIntrinsicBlockSizeIgnoringChildren`
	// gated on `ShouldApplyBlockSizeContainment()` at SHA 4883d11fef. The
	// downstream min-block-size apply at fragment_geometry.go:730 ensures
	// `min-height` still bumps the result.
	if mla.style != nil && mla.style.ShouldApplySizeContainment() && !hasExplicitBlock {
		finalBlockSize = 0
		minBlock := ResolveMinBlockSize(mla.style, wdm, mla.space, geom).Float64()
		if finalBlockSize < minBlock {
			finalBlockSize = minBlock
		}
	}
	// Phase 25 Cmt-8: when a nested multicol finishes with blockCursor <
	// remainingContentBlockSize (all in-flow content placed but CSS declared
	// height not yet satisfied), absorb the remaining height into this
	// fragment's block-size up to the outer fragmentainer limit. This
	// produces structural fragments that fill the outer column with the
	// inner's background, matching Blink's ComputeBlockSizeForFragment
	// expansion + FinishFragmentation reduction to fragmentainer space.
	if hasExplicitBlock && hasOuterFrag && blockCursor < mla.remainingContentBlockSize {
		maxCapacity := outerAvailable - geom.BlockBorderPadding()
		if maxCapacity < 0 {
			maxCapacity = 0
		}
		want := mla.remainingContentBlockSize
		if want > maxCapacity {
			want = maxCapacity
		}
		if finalBlockSize < want {
			finalBlockSize = want
		}
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
	// Phase 25 Cmt-4b: when balanced columns converged below outerAvailable
	// but Cmt-8 absorption expanded finalBlockSize to fill the outer column,
	// a structural continuation fragment is still required to satisfy
	// remaining explicit CSS block-size. Only fires when blockCursor > 0
	// (actual in-flow content was placed), NOT for pure structural fragments
	// that Cmt-8 already delivered (blockCursor == 0). Mirrors the Blink
	// break check but accounting for the Cmt-8 post-hoc absorption.
	if balanceColumns && hasOuterFrag && hasExplicitBlock && blockCursor > 0 && blockCursor < outerAvailable &&
		blockCursor < mla.remainingContentBlockSize && finalBlockSize > blockCursor &&
		finalBlockSize >= outerAvailable {
		// LOU-143: expand blockCursor to finalBlockSize so the gap geometry
		// emitted inside buildOuterBreakResult stretches the last row's
		// column rules to the content-box block end (Cmt-8 absorption
		// target). Without the expansion, gap geometry would compute its
		// content extents at the pre-absorption cursor and the painter
		// would draw rules short of the box's painted block-end.
		blockCursor = finalBlockSize
		return buildOuterBreakResult()
	}

	didBreakSelf := false // Main path: all remaining content fits.
	effBP := EffectiveBlockBorderPadding(geom, builder.HasClonedBoxDecorations(), mla.space.BreakToken, didBreakSelf)
	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + effBP,
	})
	border := geom.Border
	padding := geom.Padding
	if !builder.HasClonedBoxDecorations() {
		isContinuation := mla.space.BreakToken != nil && !mla.space.BreakToken.ConsumedBlockSize.IsZero()
		if isContinuation {
			border.BlockStart = 0
			padding.BlockStart = 0
		}
		if didBreakSelf {
			border.BlockEnd = 0
			padding.BlockEnd = 0
		}
	}
	physBorder := ToPhysicalEdges(border, wdm)
	physPadding := ToPhysicalEdges(padding, wdm)
	physScrollbar := ToPhysicalEdges(geom.Scrollbar, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize.Float64()), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:    physMargin,
		Border:    physBorder,
		Padding:   physPadding,
		Scrollbar: physScrollbar,
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

	// Phase 25 Cmt-9: emit a break token when content fits in the current
	// outer fragmentainer but explicit content block-size remains
	// (finalBlockSize < remainingContentBlockSize). The barrier above only
	// triggers when blockCursor >= outerAvailable; after Cmt-8 absorption,
	// content may still fit yet leave explicit height unsatiated. Without
	// this token the outer multicol sees no continuation and drops trailing
	// columns. Mirrors Blink's post-LayoutChildren
	// remaining_content_block_size_ break check
	// (column_layout_algorithm.cc).
	if hasOuterFrag && hasExplicitBlock && finalBlockSize > 0 &&
		finalBlockSize < mla.remainingContentBlockSize {
		prevConsumed := layoutunit.LayoutUnit{}
		seqNum := 0
		if mla.space.BreakToken != nil {
			prevConsumed = mla.space.BreakToken.ConsumedBlockSize
			seqNum = mla.space.BreakToken.SequenceNumber + 1
		}
		result.BreakToken = &BlockBreakToken{
			Node:               mla.node,
			ConsumedBlockSize:  prevConsumed.Add(layoutunit.FromFloat64Round(finalBlockSize)),
			SequenceNumber:     seqNum,
			HasSeenAllChildren: true,
		}
	}

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

	// CSS 2.1 §9.4.3: position:relative offset, stored on the fragment for
	// paint-time application (engine.go box conversion). No Blink analog
	// inside column_layout_algorithm.cc — Blink applies relative offsets
	// generically in the parent (relative_utils.cc ComputeRelativeOffset);
	// louis14's established pattern is this per-algorithm tail, copied from
	// block_layout.go:2469-2489 (see there for the PercentageResolutionSize
	// rationale).
	//
	// Known coverage limit: only the main (non-fragmented) return path gets
	// the offset. Fragments returned via buildOuterBreakResult (multicol
	// nested in an outer fragmentation context) skip it — the same gap the
	// BLA pattern has for its own break-result returns.
	if mla.style != nil && mla.style.GetPosition() == css.PositionRelative {
		cbInline := mla.space.PercentageResolutionSize.InlineSize.Float64()
		if cbInline == Indefinite {
			cbInline = 0
		}
		cbBlock := mla.space.PercentageResolutionSize.BlockSize.Float64()
		if cbBlock == Indefinite {
			cbBlock = 0
		}
		physCB := ToPhysicalSize(LogicalSize{
			InlineSize: cbInline,
			BlockSize:  cbBlock,
		}, wdm.WM)
		offset := mla.style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
		result.Fragment.RelativeOffset = computeRelativeOffset(offset, wdm)
	}
	return result
}

// layoutLine implements the outer stretch / inner per-column loop for one
// "column row" — all columns between two spanners (or the full content if no
// spanners). Returns the block-size consumed by this row, the spanner path
// (if a column-span:all element was encountered), the break token for
// resuming after the spanner, and whether the line is empty (Blink's
// is_empty, cla.cc:1218-1231 @ a9f50e522efa) — an empty line must not reset
// the caller's spanner margin strut nor advance the layout position, so a
// preceding spanner's trailing margin can collapse through it.
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
	multicolBlockBorderPadding float64,
) (rowBlockAdvance float64, columnsPlaced int, spannerPath *ColumnSpannerPath, remainingToken *BlockBreakToken, lineIsEmpty bool) {

	// Outer remaining space: cap column height so columns don't exceed what
	// fits in the current outer fragmentainer. Mirrors Blink's
	// is_constrained_by_outer_fragmentation_context_ =
	// HasKnownFragmentainerBlockSize() (cla.cc:311-318 @ a9f50e522efa) and
	// available_outer_space (cla.cc:819-824).
	constrainedByOuter := mla.space.HasBlockFragmentation &&
		mla.space.FragmentainerBlockSize != Indefinite &&
		!mla.space.IsInitialColumnBalancingPass
	outerRemaining := 0.0
	if constrainedByOuter {
		rem := mla.space.FragmentainerBlockSize - mla.space.FragmentainerOffset - lineOffset - multicolBlockBorderPadding
		if rem > 0 {
			outerRemaining = rem
		}
	}

	// Blink cla.cc:805-816: the initial (pre-balance) column block-size — the
	// row's remaining height under non-auto column-height, else the remaining
	// content block-size minus what earlier column lines/spanners in this
	// fragment already consumed (CurrentContentBlockOffset(line_offset)),
	// unless wrapping (each wrapped row gets the full content-box size).
	initialColBlockSize := mla.remainingContentBlockSize
	if !mla.hasAutoColumnHeight() {
		initialColBlockSize = mla.remainingRowHeightAtOffset(lineOffset)
	} else if initialColBlockSize != Indefinite && !mla.shouldWrapColumns() {
		initialColBlockSize -= lineOffset
		if initialColBlockSize < 0 {
			initialColBlockSize = 0
		}
	}

	// Blink cla.cc:818-831 + ColumnsOverflowInInlineDirection
	// (cla.cc:1823-1842): when column-wrap is off, columns may extend past
	// column-count along the inline axis instead of dropping content —
	// always when there is no outer fragmentation context; when nested, only
	// if the column block-size is known to fit in the outer fragmentainer (a
	// shorter inner multicol never reaches the outer fragmentation line, so
	// resuming there wouldn't help).
	//
	// LOU-370: suppress inline overflow when free balancing can grow the
	// columns to fit. When balance controls the auto column-height AND the
	// multicol has no definite content budget (auto height), the outer stretch
	// loop grows the columns until all content fits in `column-count` columns
	// (Blink's balancing do/while), so nothing is left to overflow inline —
	// enabling it would greedily place a stray 3rd column before the stretch
	// loop rebalances (regressed multicol-margin-003). With a DEFINITE budget
	// the row's column height is capped (constrainColumnBlockSize subtracts the
	// consumed offset), so balancing cannot grow columns past the budget and
	// overflow columns are required for the trailing content
	// (children-height-002/003 post-spanner block2).
	freeBalancing := balanceColumns && mla.hasAutoColumnHeight() &&
		mla.remainingContentBlockSize == Indefinite
	overflowInline := false
	if !mla.shouldWrapColumns() && !freeBalancing {
		if constrainedByOuter {
			overflowInline = initialColBlockSize != Indefinite &&
				initialColBlockSize <= outerRemaining
		} else {
			overflowInline = true
		}
	}

	// Determine the used column block-size.
	// Phase 12f (Blink cla.cc:806-809): non-auto column-height fixes the
	// column block-size to the row's remaining height at this line offset,
	// bypassing the balancing / max-height / auto branches. This is the hard
	// upper bound on a single row of columns.
	// Phase 12e (Blink behaviour): column-fill:auto + max-height on an auto-
	// height multicol uses max-height as the column block-size so columns fill
	// sequentially up to that height. Otherwise the column block-size is
	// content-driven (auto), which would collapse the multicol to a single
	// column of indefinite height.
	var colBlockSize float64
	switch {
	case !mla.hasAutoColumnHeight():
		colBlockSize = mla.constrainColumnBlockSize(initialColBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
	case balanceColumns:
		colBlockSize = mla.resolveColumnAutoBlockSize(contentNode, wdm, usedColWidth, numCols, gap, nextColToken, mla.containerPercentResolutionBlockSize)
		colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
	case hasExplicitBlock:
		colBlockSize = mla.constrainColumnBlockSize(initialColBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
	case maxBlockSize != Indefinite:
		colBlockSize = mla.constrainColumnBlockSize(maxBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
	default:
		if minimumColumnBlockSize != Indefinite {
			colBlockSize = mla.constrainColumnBlockSize(minimumColumnBlockSize, hasExplicitBlock, explicitBlockSize, maxBlockSize, outerRemaining, lineOffset, minimumColumnBlockSize)
		} else {
			colBlockSize = Indefinite
		}
	}

	// Blink: when a definite-height multicol's content budget is fully spent by
	// earlier column rows and spanners, a trailing row with content still to
	// place gets 1px column boxes rather than 0-height ones — otherwise the
	// content can never make progress. The WPT reference for
	// multicol-span-all-children-height-003 renders block2's post-spanner
	// columns at height:1px ("the column container creates 1px column boxes
	// for it"). Floor to 1px only in the budget-exhausted case (definite
	// budget, colBlockSize clamped to 0, a break token remaining) — the
	// genuinely-empty balance row keeps its 0 (→ Indefinite) so no ghost row
	// appears before a spanner.
	if colBlockSize == 0 && mla.remainingContentBlockSize != Indefinite &&
		initialColBlockSize == 0 && nextColToken != nil {
		colBlockSize = 1
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

		// CSS Multicol L2 §4.2.6 `column-wrap:nowrap` mode (including the
		// `auto` resolution to `nowrap`): columns may extend past
		// `column-count` along the inline axis (overflowing the multicol
		// box) because no row-wrap is allowed. The inner per-column loop
		// must continue placing columns until content is consumed
		// (break-token goes nil). Each column must have a definite
		// block-size so layout makes progress per column; otherwise the
		// classic L1 balance algorithm constrains content into `column-
		// count` columns and the cap remains. Mirrors Blink's behavior at
		// `4883d11fef` where `ColumnLayoutAlgorithm::LayoutLine` does not
		// cap by `used_column_count` when `!ShouldWrapColumns()` and each
		// column has a known block-size.
		// Balance is conceptually inactive when column-height is non-auto:
		// the switch above takes the `!hasAutoColumnHeight()` branch, which
		// bypasses the balance estimate and uses the explicit column height.
		// So the gate excludes balance only when it is actually controlling
		// the column block-size (balanceColumns && hasAutoColumnHeight).
		balanceControlsColumnHeight := balanceColumns && mla.hasAutoColumnHeight()
		noWrapMode := !mla.shouldWrapColumns() && !balanceControlsColumnHeight && colBlockSize != Indefinite

		// Inner per-column loop. Runs while a break token remains (Blink's
		// do/while at cla.cc:929-1068); the cap at column-count applies only
		// when inline overflow is not permitted (cla.cc:1022-1025).
		//
		// no Blink analog: defensive guard. When overflowInline (or Indefinite
		// column height) removes the column-count cap, a column that makes zero
		// progress — the outgoing break token's consumed block-size does not
		// advance past the previous column's — would spin forever (e.g. content
		// resumed into a 0-height post-spanner column,
		// multicol-span-all-children-height-003). The sequence number always
		// increments (it is a fragment counter), so consumed size is the only
		// true progress signal. Blink guarantees termination via break-token
		// progress invariants; louis14 asserts it explicitly.
		prevTokenConsumed := math.NaN()
		for col := 0; col < numCols || colBlockSize == Indefinite || noWrapMode || overflowInline; col++ {
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
			// no Blink analog: defensive non-advancing-token guard (see the
			// loop header). If this column's outgoing token's consumed size did
			// not advance past the previous column's, no progress was made and
			// continuing would loop forever — stop the row.
			if colBreakToken != nil {
				consumed := colBreakToken.ConsumedBlockSize.Float64()
				if consumed <= prevTokenConsumed {
					colBreakToken = nil
					break
				}
				prevTokenConsumed = consumed
			}
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
					// Blink cla.cc:1022-1025: stop at column-count only when
					// inline overflow is not permitted.
					if !noWrapMode && !overflowInline && col+1 >= numCols {
						break
					}
					continue
				}
				break // all content fit
			}
			inlineOffset += usedColWidth + gap

			// Blink cla.cc:1022-1025: stop at column-count only when inline
			// overflow is not permitted — otherwise keep placing overflow
			// columns until the content is consumed.
			if !noWrapMode && !overflowInline && col+1 >= numCols {
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
		// In nowrap mode, the column-count cap does not apply (CSS Multicol
		// L2 §4.2.6) — columns extend inline as needed.
		lastHasSpanner := lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil
		if !hasViolatingBreak &&
			(noWrapMode || actualColumnCount <= numCols) &&
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
				multicolBlockBorderPadding,
			)
		}
	}

	// Blink cla.cc:1218-1231: a line is "empty" when its single column box has
	// no extent and no children — e.g. the spanner-detection line between two
	// adjacent spanners — OR when its only content is the empty continuation
	// of a resumed block whose next child is the spanner (Blink's
	// is_empty_spanner_parent, bla.cc:1050-1055, propagated up via
	// box_fragment_builder.cc:596-605 @ a9f50e522efa; louis14 has no builder
	// flag, so the equivalent is derived from the spanner path + incoming
	// token below). colBlockSize == Indefinite corresponds to Blink's
	// content-based zero estimate for a contentless column-fill:auto line
	// (Blink resolves the auto block-size to 0 via ResolveColumnAutoBlockSize
	// before the check).
	isEmptySpannerParent := lastInnerResult != nil &&
		lastInnerResult.ColumnSpannerPath != nil &&
		len(finalColumns) == 1 && finalColumns[0].intrinsicBlock == 0 &&
		spannerParentIsResuming(lastInnerResult.ColumnSpannerPath, nextColToken)
	lineIsEmpty = (colBlockSize == 0 || colBlockSize == Indefinite) &&
		len(finalColumns) == 1 &&
		(len(finalColumns[0].fragment.Children) == 0 || isEmptySpannerParent)

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
		// LOU-111 step 4: propagate the column's BSFF so this multicol's
		// own outgoing BSFF reflects the maximum column-row content extent
		// (including any monolithic / parallel-flow overflow inside the
		// column body). An OUTER multicol slicing this multicol's
		// fragments reads the inner BSFF at lines 1631-1632 below to size
		// its column rows correctly. Mirrors Blink's
		// BoxFragmentBuilder::PropagateChildData max-merge
		// (box_fragment_builder.cc:977-982).
		if col.result != nil {
			builder.PropagateChildBlockSizeForFragmentation(col.result, col.offset)
		}
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
		return rowAdvance, columnsPlaced, lastInnerResult.ColumnSpannerPath, lastInnerResult.BreakToken, lineIsEmpty
	}
	// LOU-325: the render did not reach a spanner, but the measure pass did —
	// an oversized block (004b's block2: 200px content vs the container's 150px
	// remaining budget) filled the bounded render columns and produced a plain
	// continuation token, so the render columns never advanced to the spanner.
	// Fall back to the captured detection result: surface the spanner and its
	// budget-clamped resume token so the trailing segments (here: the second
	// spanner + a zero-budget final segment) are still placed. Guarded on a
	// continuation token (finalColBreakToken != nil) — when the render fully
	// consumed the content (token nil), the captured spanner is stale (already
	// represented by the render's own detection) and must not be re-emitted.
	if mla.lastMeasuredSpannerPath != nil && finalColBreakToken != nil {
		capturedPath := mla.lastMeasuredSpannerPath
		capturedToken := mla.lastMeasuredSpannerToken
		mla.lastMeasuredSpannerPath = nil
		mla.lastMeasuredSpannerToken = nil
		return maxColHeight, columnsPlaced, capturedPath, capturedToken, lineIsEmpty
	}

	// Return any remaining column break token so the caller can include it
	// in the outer break result and resume column rows in the next outer column.
	return maxColHeight, columnsPlaced, nil, finalColBreakToken, lineIsEmpty
}

// resolveSpannerInlineMargins returns the spanner's inline-start offset
// within the multicol's content box, resolving `auto` inline margins against
// the free space between the spanner's margin box and the multicol content
// box. Mirrors ResolveInlineAutoMargins (length_utils.cc:1551-1570 @
// a9f50e522efa) and the offset computation at cla.cc:1424-1426. The returned
// offset is content-box relative (border/scrollbar/padding is added at box
// conversion), so only the resolved inline-start margin contributes.
func resolveSpannerInlineMargins(
	spannerStyle *css.Style,
	wdm WritingDirectionMode,
	availableInline float64,
	spannerInline float64,
	margins LogicalEdges,
) float64 {
	if spannerStyle == nil {
		return 0
	}
	autoStart, autoEnd, _, _ := PhysicalAutoMarginsToLogical(spannerStyle.GetMargin(), wdm)
	availableSpace := availableInline - spannerInline - margins.InlineStart - margins.InlineEnd
	if availableSpace < 0 {
		availableSpace = 0
	}
	switch {
	case autoStart && autoEnd:
		margins.InlineStart = availableSpace / 2
	case autoStart:
		margins.InlineStart = availableSpace
	}
	return margins.InlineStart
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
	// LOU-325: reset the captured spanner detection. Set below if a measure-pass
	// iteration reaches a column-span; consumed by the render in layoutLine.
	mla.lastMeasuredSpannerPath = nil
	mla.lastMeasuredSpannerToken = nil

	hasExplicitContainerHeight := containerPercentResolutionBlockSize >= 0

	// buildSpace constructs a ConstraintSpace for one measure-pass iteration.
	// Called once per loop iteration with the current break-token.
	buildSpace := func(breakToken *BlockBreakToken) ConstraintSpace {
		b := NewConstraintSpaceBuilder(wdm, wdm, true)
		if hasExplicitContainerHeight {
			measureAvailBlock := containerPercentResolutionBlockSize
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
			// Use intrinsic content height as the authoritative measure.
			// IsBlockSizeOverride artificially inflates fragment block-size
			// to CSS height; IntrinsicBlockSize reflects true content.
			colBSize := result.IntrinsicBlockSize
			if colBSize <= 0 {
				colBSize = result.BlockSizeForFragmentation
				if result.Fragment != nil {
					if fragBSize := NewLogicalFragment(wdm, result.Fragment).BlockSize(); fragBSize > colBSize {
						colBSize = fragBSize
					}
				}
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
			// LOU-325: capture the spanner path and the budget-clamped resume
			// break token the early-return built (block_layout.go:624-724). The
			// render pass may not reach this spanner when an oversized block
			// fills the bounded render columns (004b); layoutLine falls back to
			// this capture so the spanner and trailing segments are placed.
			mla.lastMeasuredSpannerPath = result.ColumnSpannerPath
			mla.lastMeasuredSpannerToken = result.BreakToken

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
// container's {,min-,max-}block-size cap adjusted for earlier progress, the
// outer fragmentainer's remaining space, and the row-height at this offset.
// Mirrors Blink's ConstrainColumnBlockSize() (cla.cc:1736-1821 @
// a9f50e522efa; louis14 works in content-box units so the border-box
// conversion at :1770-1772/:1813 is a no-op here).
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
	if outerRemaining > 0 && size > outerRemaining {
		size = outerRemaining
	}
	// Blink cla.cc:1774-1793: the cap starts from max-height, a specified
	// height tightens it, and min-height may RAISE it.
	maxSize := math.Inf(1)
	if maxBlockSize != Indefinite {
		maxSize = maxBlockSize
	}
	if hasExplicitBlock && explicitBlockSize < maxSize {
		maxSize = explicitBlockSize
	}
	if mla.minBlockSize > maxSize {
		maxSize = mla.minBlockSize
	}
	// Blink cla.cc:1795-1809: adjust the cap for earlier progress — space
	// consumed in previous outer fragments and earlier column lines/spanners
	// in this fragment — unless column wrapping is on (each wrapped row gets
	// the full content-box size).
	if !math.IsInf(maxSize, 1) && !mla.shouldWrapColumns() {
		if mla.space.BreakToken != nil {
			maxSize -= mla.space.BreakToken.ConsumedBlockSize.Float64()
		}
		maxSize -= lineOffset
		if maxSize < 0 {
			maxSize = 0
		}
	}
	if size > maxSize {
		size = maxSize
	}
	// Blink cla.cc:1815-1818: never become taller than used column-height.
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
	//
	// LOU-370: gate additionally on an auto (Indefinite) content budget. When
	// the multicol has a definite content block-size, a computed 0 is a real
	// budget-exhausted zero (all content consumed by earlier rows/spanners) —
	// Blink passes it straight to CreateConstraintSpaceForFragmentainer
	// (cla.cc:929-933 @ a9f50e522efa: no 0→Indefinite conversion), clipping
	// the trailing block to a zero-height column
	// (multicol-span-all-children-height-003's post-spanner block2). Only the
	// auto-budget empty-balance case needs the ghost-row escape hatch.
	availBlock := colBlockSize
	if colBlockSize == 0 && mla.hasAutoColumnHeight() &&
		mla.remainingContentBlockSize == Indefinite {
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
	} else {
		// Indefinite fragmentainer size: column-fill:auto with auto column-height
		// (no max-height constraint). The child still has block fragmentation
		// (so spanner detection works), but no fragmentainer boundary to break at.
		// FragmentainerBlockSize defaults to 0 on the builder; without an explicit
		// Indefinite set, downstream code (block_layout.go's overflow check at
		// `blockCursor > fragEnd`) treats fragEnd as 0 and breaks every child past
		// the first. Mirrors Blink's column_layout_algorithm.cc:1567 setting
		// fragmentainer_block_size = kIndefiniteSize for the initial column-
		// balancing pass.
		b = b.SetFragmentainerBlockSize(Indefinite).
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
//  1. After each column is committed in layoutLine.
//  2. After the spanner is committed in the spanner-placement block of Layout.
//
// min semantics for first baseline: the earliest column wins.
// max semantics for last baseline: the latest column wins.
// SetUseLastBaselineForInlineBaseline is called unconditionally (Blink invariant).
func (mla *MulticolLayoutAlgorithm) propagateBaselineFromChild(
	result *LayoutResult, builder *BoxFragmentBuilder, blockOffset float64) {

	if result.HasBaseline {
		cur, has := builder.Baseline()
		if !has {
			cur = math.MaxFloat64
		}
		bl := math.Min(blockOffset+result.Baseline, cur)
		builder.SetBaseline(bl)
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

// spannerParentIsResuming reports whether the innermost block directly
// containing the detected spanner is RESUMING (a break-inside continuation)
// rather than starting fresh in this column line. It matches the spanner
// path's intermediate containers against the incoming column break token
// chain: every container on the path must appear with a break-inside token
// (IsBreakBefore false).
//
// This is the multicol-level equivalent of Blink's is_empty_spanner_parent
// input `is_resuming_` (bla.cc:1050-1055 @ a9f50e522efa) — a FRESH parent
// wrapping a spanner starts a box in this line, so a preceding spanner's
// trailing margin must NOT collapse through it (WPT
// multicol-span-all-margin-nested-001), while a resumed parent's empty
// continuation lets the margin collapse (multicol-span-all-margin-003).
func spannerParentIsResuming(path *ColumnSpannerPath, incoming *BlockBreakToken) bool {
	if incoming == nil || incoming.IsBreakBefore {
		return false
	}
	token := incoming
	for p := path; p != nil && p.Child != nil; p = p.Child {
		var next *BlockBreakToken
		for _, ct := range token.ChildBreakTokens {
			if ct.Node == p.Box {
				next = ct
				break
			}
		}
		if next == nil || next.IsBreakBefore {
			return false
		}
		token = next
	}
	return true
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
	if !mla.style.HasColumnRule() {
		return
	}

	gg := &GapGeometry{
		ContainerType: GapContainerMulticol,
		MainDirection: GapForRows,
	}

	// Only build gap content when columns were actually laid out.
	// Structural fragments (Cmt-8) have no columns; they carry an empty
	// GapGeometry so the painter knows there are no rules to draw.
	if mla.firstColumnOffsetSet {
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
			for i := mla.maxColumnsInRow; i < declaredColCount && len(mla.mainGaps) > 0; i++ {
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

		// Compute LastRowCrossGapStart: index of the first CrossGap in
		// the last row. Mirrors Blink's !items_until_last_row gate in
		// PaintColumnRules: only the last row's column rules stretch to
		// the content-box bottom.
		if len(mla.columnsPerRow) > 1 {
			for _, cols := range mla.columnsPerRow[:len(mla.columnsPerRow)-1] {
				if cols > 0 {
					gg.LastRowCrossGapStart += cols - 1
				}
			}
		}
		// Scrollbar block-end for PaintColumnRules stretch alignment.
		gg.ScrollbarBlockEnd = geom.Scrollbar.BlockEnd
	}

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
	// Lay the marker box out for size + first-line baseline, then align it
	// against the column/spanner child's content baseline. Mirrors Blink's
	// ColumnLayoutAlgorithm reusing list_marker.Layout's result in AddToBox.
	markerResult := marker.Layout(mla.ctx, mla.space)
	if markerResult == nil {
		return
	}
	marker.AddToBox(mla.ctx, markerResult, baseline, blockOffset, builder)
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
	markerResult := marker.Layout(mla.ctx, mla.space)
	if markerResult == nil {
		return
	}
	marker.AddToBoxWithoutLineBoxes(mla.ctx, markerResult, builder, intrinsicBlockSize)
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
