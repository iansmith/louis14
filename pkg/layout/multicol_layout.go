package layout

import (
	"math"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
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
	// CreateConstraintSpaceForSpanner. Indefinite when the multicol has auto
	// block-size.
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
	// declaredBorderScrollbarPadding is the multicol's border + scrollbar +
	// padding as declared, before Layout clears an edge this fragment does
	// not carry (the builder's BorderScrollbarPadding is the carried one).
	// louis14 resolves the content block-size, the consumed content and the
	// fragments' total block-size from it; Blink resolves those from the
	// uncleared BoxFragmentBuilder::BorderPadding (border_padding_,
	// box_fragment_builder.h:773, scaled for cloned fragments by
	// UpdateBorderPaddingForClonedBoxDecorations, box_fragment_builder.cc:
	// 88-99) and from InitialBorderBoxSize (column_layout_algorithm.cc:266,
	// :320-331, :380-381), all @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
	// Resolved once in Layout(), before the block-start clear.
	declaredBorderScrollbarPadding LogicalEdges
	// rowGapSize is the block-axis gap between column rows in a
	// `column-wrap: wrap` multicol (CSS Multicol L2 §4.2.6). Read from
	// the element's `row-gap` property via GetRowGapMulticol. Resolved
	// once in Layout(); `normal` computes to 1em. Matches Blink's
	// row_gap_ in column_layout_algorithm.cc used by
	// RemainingRowHeightAtOffset / row-wrap loop (cla.cc:789–836).
	rowGapSize float64

	// gapAccumulator accumulates the column-rule gap geometry while lines are
	// laid out; nil when the style has no gap rule. Mirrors Blink's
	// ColumnLayoutAlgorithm::gap_accumulator_ (column_layout_algorithm.h:292-294
	// @ a9f50e522efa).
	gapAccumulator *ColumnGapAccumulator

	// columnGapSize is the resolved column-gap, resolved once in Layout()
	// against the content-box inline size.
	columnGapSize float64

	// usedColWidth is the computed per-column inline-size (resolved once in
	// Layout; Blink's ColumnInlineSize()).
	usedColWidth float64

	// lastMeasuredTallestUnbreakable carries the tallest unbreakable
	// block-size accumulated during resolveColumnAutoBlockSize's measure
	// pass, so MLA.Layout can forward it to its outer container_builder
	// when this multicol is itself inside an initial column-balancing
	// pass (nested column balancing). Mirrors Blink's
	// container_builder_.PropagateTallestUnbreakableBlockSize call at
	// column_layout_algorithm.cc:1706-1712. v2 Path X port.
	lastMeasuredTallestUnbreakable float64

	// containerBuilder is the builder of the fragment being laid out,
	// created in Layout(). Mirrors Blink's LayoutAlgorithm::
	// container_builder_ (layout_algorithm.h:299 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which Blink's
	// FragmentainerCapacity / FragmentainerOffset / FragmentainerSpaceLeft
	// read (fragmentation_utils.h:153-257). The only access path to the
	// builder: Layout and its helpers read the field. Outside Layout the
	// geometry and edges are read through the builder's accessors; Layout
	// reads its local geom, the object the builder points at.
	containerBuilder *BoxFragmentBuilder

	// spannerPath is the path to the spanner that ends the current line of
	// columns, found by the balancing pass and handed to every column
	// layout of the line, so blocks on it keep their breaks in the same
	// flow. Reset before balancing and when the spanner is laid out.
	// Mirrors Blink's ColumnLayoutAlgorithm::spanner_path_
	// (column_layout_algorithm.cc:942, :1338, :1526, :1637-1694 @
	// a9f50e522efa).
	spannerPath *ColumnSpannerPath

	// --- Per-layout invariants, resolved once in Layout() before
	// LayoutChildren runs and read by the methods it calls. ---

	// contentNode is the anonymous column-content node wrapping the
	// multicol's children (Blink lays columns out on the multicol node
	// itself, column_layout_algorithm.cc:944-945 @ a9f50e522efa). It is
	// read from mla.node.contentNodeCache so its pointer is stable across
	// Layout() calls (break tokens resume by node identity).
	contentNode *LayoutInputNode
	// contentInlineSize is the multicol's content-box inline size
	// (Blink ChildAvailableSize().inline_size).
	contentInlineSize float64
	// usedColumnCount is the used column-count. Mirrors Blink's
	// used_column_count_ (column_layout_algorithm.h:264 @ a9f50e522efa).
	usedColumnCount int
	// effectiveMaxBlockSize is the resolved max-block-size when the
	// block-size is auto, else Indefinite (Blink resolves it inside
	// ConstrainColumnBlockSize, column_layout_algorithm.cc:1774-1775 @
	// a9f50e522efa).
	effectiveMaxBlockSize float64

	// --- Loop state of LayoutChildren and the methods it calls.
	// Initialised by Layout() before the walker loop. ---

	// blockCursor is the content-relative block offset reached so far: Blink's
	// CurrentContentBlockOffset(intrinsic_block_size_)
	// (column_layout_algorithm.h:171-174, :276 @ a9f50e522efa). It is Blink's
	// intrinsic_block_size_ minus BorderScrollbarPadding().block_start, which
	// Blink starts from (column_layout_algorithm.cc:335); mla.blockStartBorderPadding()
	// converts between the two.
	blockCursor float64
	// hasProcessedFirstChild mirrors Blink's has_processed_first_child_
	// (column_layout_algorithm.h:290 @ a9f50e522efa): a non-empty line or a
	// spanner has been placed in this fragment, so a spanner after it has
	// container separation.
	hasProcessedFirstChild bool
	// outBuilder accumulates outgoing child break tokens in document order
	// for this multicol's outgoing BlockBreakToken. Stands in for Blink's
	// container_builder_.AddBreakToken / AddBreakBeforeChild
	// (column_layout_algorithm.cc:662-686 @ a9f50e522efa).
	outBuilder MulticolBreakTokenBuilder
	// outgoingMulticolData carries the row phase on the outgoing break token
	// when the first row of a column run is truncated by the outer
	// fragmentainer. Mirrors Blink's container_builder_.SetBreakTokenData
	// (column_layout_algorithm.cc:763-774 @ a9f50e522efa).
	outgoingMulticolData *MulticolBreakTokenData
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

// AllowsColumns reports whether the element may become a multicol container
// when its style specifies columns. An <img> never does: its block flow
// exists only to hold the alt text of a broken image. Mirrors
// LayoutBlockFlow::AllowsColumns (layout_block_flow.cc:621-647, the
// HTMLImageElement check at :641-645 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). Blink's other exclusions (ruby,
// display:layout(), MathML) are not ported.
func AllowsColumns(node *html.Node) bool {
	return node.TagName != "img"
}

// shouldBeMulticol reports whether node establishes a multicol container:
// its style specifies columns and the element allows them. Mirrors
// LayoutBlockFlow::UpdateForMulticol's ShouldBeMulticol
// (layout_block_flow.cc:650-653 @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
// Anonymous boxes (no DOM node) allow columns.
func shouldBeMulticol(node *LayoutInputNode, style *css.Style) bool {
	return isMulticolContainer(style) && (node.DOMNode == nil || AllowsColumns(node.DOMNode))
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

// isConstrainedByOuterFragmentation reports whether this multicol sits inside
// another fragmentation context (multicol / paged media) with a known
// fragmentainer block-size — so its columns must cap at the outer boundary and
// emit a break token for the outer algorithm to resume. Mirrors Blink's
// is_constrained_by_outer_fragmentation_context_ = HasKnownFragmentainerBlockSize()
// (column_layout_algorithm.cc:311-318 @ a9f50e522efa). The measurement pass is
// excluded because it has no committed fragmentainer geometry to cap against.
func (mla *MulticolLayoutAlgorithm) isConstrainedByOuterFragmentation() bool {
	return mla.space.HasKnownFragmentainerBlockSize()
}

// fragmentainerCapacityForChildren is the outer fragmentainer block-size
// available to this multicol's content, or Indefinite while it is unknown
// (e.g. the outer's initial balancing pass). Mirrors Blink's
// LayoutAlgorithm::FragmentainerCapacityForChildren() (layout_algorithm.h:
// 160-162 @ a9f50e522efa).
func (mla *MulticolLayoutAlgorithm) fragmentainerCapacityForChildren() float64 {
	if !mla.isConstrainedByOuterFragmentation() {
		return Indefinite
	}
	return FragmentainerCapacity(mla.containerBuilder, true)
}

// fragmentainerOffsetForChildren is this multicol's offset from the outer
// fragmentainer start as seen by its children: with cloned decorations a
// child at the block-start content edge counts as offset 0. Mirrors Blink's
// LayoutAlgorithm::FragmentainerOffsetForChildren() (layout_algorithm.h:
// 164-166 @ a9f50e522efa) — the border-box-edge offset, unlike
// BlockLayoutAlgorithm.fragmentainerOffsetForChildren, which already adds
// the block-start edge for that algorithm's content-relative cursor.
func (mla *MulticolLayoutAlgorithm) fragmentainerOffsetForChildren() float64 {
	return FragmentainerOffset(mla.containerBuilder, true)
}

// fragmentainerSpaceLeftForChildren is the outer fragmentainer space left
// from this multicol's border-box block-start edge, clamped to zero, or
// Indefinite when the outer fragmentainer block-size is unknown. Mirrors
// Blink's LayoutAlgorithm::FragmentainerSpaceLeftForChildren()
// (layout_algorithm.h:168-170 @ a9f50e522efa). Blink's line_offset is
// measured from the same border-box edge (intrinsic_block_size_ starts at
// the block-start border/padding, cla.cc:335); louis14's line offsets are
// content-box relative, so callers subtract blockStartBorderPadding() (see
// spaceLeftForContent).
func (mla *MulticolLayoutAlgorithm) fragmentainerSpaceLeftForChildren() float64 {
	if !mla.isConstrainedByOuterFragmentation() {
		return Indefinite
	}
	return FragmentainerSpaceLeft(mla.containerBuilder, true)
}

// spaceLeftForContent is fragmentainerSpaceLeftForChildren() measured from
// the content-box block-start edge, i.e. the space left at content offset 0
// (may be negative when the block-start edge itself overflows), or
// Indefinite when not constrained by an outer fragmentainer.
func (mla *MulticolLayoutAlgorithm) spaceLeftForContent() float64 {
	spaceLeft := mla.fragmentainerSpaceLeftForChildren()
	if spaceLeft == Indefinite {
		return Indefinite
	}
	return spaceLeft - mla.blockStartBorderPadding()
}

// breakTokenTreeContainsNode reports whether node resumes somewhere in the
// break-token tree rooted at token (a break inside it, not a break before
// it): its last fragment so far has a break token. no Blink analog: Blink
// reads the box's last physical fragment's break token
// (PhysicalFragments().back().GetBreakToken(),
// out_of_flow_layout_part.cc:1617 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 fragments carry no
// break token (plan G20).
func breakTokenTreeContainsNode(token *BlockBreakToken, node *LayoutInputNode) bool {
	if token == nil {
		return false
	}
	if token.Node == node && !token.IsBreakBefore {
		return true
	}
	for _, child := range token.ChildBreakTokens {
		if breakTokenTreeContainsNode(child, node) {
			return true
		}
	}
	return false
}

// CreateEmptyColumn creates an empty column fragmentainer after
// previousColumn, of the same size, for out-of-flow content that runs past
// the last column. Mirrors Blink's ColumnLayoutAlgorithm::CreateEmptyColumn
// (column_layout_algorithm.cc:500-519 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which resumes the column
// content from previousColumn's break token in a SimplifiedOofLayoutAlgorithm
// that produces no in-flow children; louis14 builds that empty fragment
// directly.
func CreateEmptyColumn(wdm WritingDirectionMode, previousColumn *PhysicalFragment) *PhysicalFragment {
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetBoxType(BoxTypeColumn)
	builder.SetNode(previousColumn.Node)
	builder.SetStyle(previousColumn.Style)
	builder.SetLayoutNode(previousColumn.LayoutNode)
	builder.SetIsFirstForNode(false)
	builder.SetSize(ToLogicalSize(geomSizeToOld(previousColumn.Size), wdm.WM))
	return builder.Build().Fragment
}

// hasExplicitBlock reports whether the multicol has a definite block-size:
// Blink's InitialBorderBoxSize().block_size != kIndefiniteSize
// (column_layout_algorithm.cc:266, :320-331 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (mla *MulticolLayoutAlgorithm) hasExplicitBlock() bool {
	return mla.containerBuilder.InitialFragmentGeometry().BorderBoxSize.BlockSize != Indefinite
}

// explicitBlockSize is the multicol's definite content block-size (0 when
// auto): Blink's ShrinkLogicalSize(border_box_size,
// BorderScrollbarPadding()).block_size, clamped to zero
// (column_layout_algorithm.cc:320-331 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), over the declared edges —
// louis14 takes earlier fragments' edges off the consumed size instead
// (previouslyConsumedContentBlockSize).
func (mla *MulticolLayoutAlgorithm) explicitBlockSize() float64 {
	if !mla.hasExplicitBlock() {
		return 0
	}
	return math.Max(0, mla.containerBuilder.InitialFragmentGeometry().BorderBoxSize.BlockSize-mla.declaredBorderScrollbarPadding.BlockSum())
}

// blockStartBorderPadding is this fragment's block-start border +
// scrollbar + padding, zero once Layout has cleared it for a continuation
// (ShouldIncludeBlockStartBorderPadding): Blink's initial
// intrinsic_block_size_ (BorderScrollbarPadding().block_start, cla.cc:335
// @ a9f50e522efa). It is the progress that opens the may_have_more_space
// gate in LayoutLine before any line or spanner has been placed, and the
// offset from the border-box edge (Blink's line_offset origin) to
// louis14's content-relative cursors.
func (mla *MulticolLayoutAlgorithm) blockStartBorderPadding() float64 {
	return mla.containerBuilder.BorderScrollbarPadding().BlockStart
}

// finishNestedFragment sizes this fragment of a multicol nested in an outer
// fragmentation context and decides its break state, the way Blink's
// ColumnLayoutAlgorithm::Layout tail does (column_layout_algorithm.cc:345-393
// @ a9f50e522efa): the intrinsic block-size runs from the block-start edge
// through the content to the block-end edge (ClampIntrinsicBlockSize ignores
// the content under size containment), ComputeBlockSizeForFragment resolves
// height / min-height / max-height over all fragments, and FinishFragmentation
// fits the result into the outer fragmentainer. The content size is the
// content-relative cursor; shouldBreakInside / hasInflowChildBreakInside
// describe the outgoing child break tokens.
func (mla *MulticolLayoutAlgorithm) finishNestedFragment(shouldBreakInside, hasInflowChildBreakInside bool) FinishFragmentationResult {
	contentBlockSize := mla.blockCursor
	hasExplicitBlock, maxBlockSize := mla.hasExplicitBlock(), mla.effectiveMaxBlockSize
	bsp := mla.declaredBorderScrollbarPadding
	if mla.style != nil && mla.style.ShouldApplySizeContainment() && !hasExplicitBlock {
		contentBlockSize = 0
	}
	intrinsic := mla.blockStartBorderPadding() + contentBlockSize + bsp.BlockEnd
	previouslyConsumed := 0.0
	if tok := mla.space.BreakToken; tok != nil && !tok.IsBreakBefore {
		previouslyConsumed = tok.ConsumedBlockSize.Float64()
	}
	// ComputeBlockSizeForFragment (border-box, all fragments).
	total := mla.containerBuilder.InitialFragmentGeometry().BorderBoxSize.BlockSize
	if hasExplicitBlock && hasCloneBoxDecorationBreak(mla.containerBuilder.Style()) {
		total += mla.clonedEdgesOfPreviousFragments()
	}
	if !hasExplicitBlock {
		total = previouslyConsumed + intrinsic
		if maxBlockSize != Indefinite {
			total = math.Min(total, maxBlockSize+bsp.BlockSum())
		}
		total = math.Max(total, mla.minBlockSize+bsp.BlockSum())
	}
	// FragmentainerSpaceLeft(builder, is_for_children=false)
	// (fragmentation_utils.cc:550-551 @ a9f50e522efa).
	spaceLeft := Indefinite
	if mla.isConstrainedByOuterFragmentation() {
		spaceLeft = FragmentainerSpaceLeft(mla.containerBuilder, false)
	}
	desired := math.Max(0, total-previouslyConsumed)
	return FinishFragmentation(FinishFragmentationInput{
		SpaceLeft:                             spaceLeft,
		FragmentainerCapacity:                 FragmentainerCapacity(mla.containerBuilder, false),
		HasBlockFragmentation:                 mla.space.HasBlockFragmentation,
		IsColumnFragmentation:                 mla.space.BlockFragmentationType == FragmentColumn,
		IsInitialColumnBalancingPass:          mla.space.IsInitialColumnBalancingPass,
		PreviousBreakToken:                    mla.space.BreakToken,
		FragmentsTotalBlockSize:               total,
		IntrinsicBlockSize:                    intrinsic,
		TrailingBorderPadding:                 bsp.BlockEnd,
		BlockStartBorderPadding:               bsp.BlockStart,
		PreventBreakBeforeBlockEndDecorations: mla.containerBuilder.ShouldCloneBoxEndDecorations(),
		ShouldBreakInside:                     shouldBreakInside,
		HasInflowChildBreakInside:             hasInflowChildBreakInside,
		IsKnownToFitInFragmentainer:           hasExplicitBlock && spaceLeft != Indefinite && desired <= spaceLeft,
		ShouldCloneBoxEndDecorations:          mla.containerBuilder.ShouldCloneBoxEndDecorations(),
	})
}

// previouslyConsumedContentBlockSize is the content block-size earlier
// fragments consumed: the incoming token's (border-box) consumed block-size
// less the block edges those fragments carried — the first fragment's
// block-start edge, or with cloned decorations both edges of every fragment.
// Blink subtracts ConsumedBlockSize from a content size resolved with
// UpdateBorderPaddingForClonedBoxDecorations' accumulated edges
// (column_layout_algorithm.cc:320-331, fragmentation_utils.cc:440-449 @
// a9f50e522efa); louis14 resolves the content size once, so the edges come
// off the consumed size instead.
func (mla *MulticolLayoutAlgorithm) previouslyConsumedContentBlockSize() float64 {
	tok := mla.space.BreakToken
	if tok == nil || tok.IsBreakBefore {
		return 0
	}
	edges := mla.declaredBorderScrollbarPadding.BlockStart
	if hasCloneBoxDecorationBreak(mla.containerBuilder.Style()) {
		edges = mla.clonedEdgesOfPreviousFragments()
	}
	return math.Max(0, tok.ConsumedBlockSize.Float64()-edges)
}

// clonedEdgesOfPreviousFragments is the block border/scrollbar/padding the
// earlier fragments repeated under box-decoration-break:clone (Blink
// UpdateBorderPaddingForClonedBoxDecorations, fragmentation_utils.cc:440-449
// @ a9f50e522efa).
func (mla *MulticolLayoutAlgorithm) clonedEdgesOfPreviousFragments() float64 {
	tok := mla.space.BreakToken
	if tok == nil || tok.IsBreakBefore {
		return 0
	}
	return float64(tok.SequenceNumber+1) * mla.declaredBorderScrollbarPadding.BlockSum()
}

// clearBlockEndExcludedBy drops the builder's block-end edge when a nested
// fragment does not carry it per FinishFragmentation. The block-start edge
// was settled once in Layout (SetupFragmentBuilderForFragmentation).
func (mla *MulticolLayoutAlgorithm) clearBlockEndExcludedBy(res FinishFragmentationResult) {
	if !res.IncludeBlockEnd {
		mla.containerBuilder.ClearBorderScrollbarPaddingBlockEnd()
	}
}

// applyNestedFinish reports FinishFragmentation's space shortage and break
// appeal on the builder.
func applyNestedFinish(builder *BoxFragmentBuilder, res FinishFragmentationResult) {
	builder.PropagateSpaceShortage(res.SpaceShortage)
	if res.ClampAppealToLastResort {
		builder.ClampBreakAppeal(BreakAppealLastResort)
	}
}

// columnsOverflowInInlineDirection reports whether columns past
// column-count are laid out beside the others (overflowing the multicol in
// the inline direction) rather than in a new row or the next outer
// fragmentainer: never when columns wrap; under an outer fragmentation
// context only when the column block-size is known to fit in the outer
// fragmentainer; otherwise always. Mirrors Blink's
// ColumnLayoutAlgorithm::ColumnsOverflowInInlineDirection
// (column_layout_algorithm.cc:1823-1842 @ a9f50e522efa).
func (mla *MulticolLayoutAlgorithm) columnsOverflowInInlineDirection(columnKnownToFitInOuterFragmentainer bool) bool {
	if mla.shouldWrapColumns() {
		return false
	}
	if mla.isConstrainedByOuterFragmentation() {
		return columnKnownToFitInOuterFragmentainer
	}
	return true
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

// Layout performs multi-column layout and returns the result. Mirrors
// Blink's ColumnLayoutAlgorithm::Layout (column_layout_algorithm.cc:265-431
// @ a9f50e522efa): resolve the per-layout state, lay the parts out in
// LayoutChildren (:337), then size and finish the fragment.
func (mla *MulticolLayoutAlgorithm) Layout() *LayoutResult {
	wdm := mla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(mla.ctx, mla.node, mla.style, wdm, mla.space)
	mla.containerBuilder = NewBoxFragmentBuilder(wdm)
	mla.containerBuilder.SetLayoutNode(mla.node)
	mla.containerBuilder.SetConstraintSpace(&mla.space)
	mla.containerBuilder.SetInitialFragmentGeometry(&geom)
	mla.containerBuilder.SetChildAvailableSize(CalculateChildAvailableSize(mla.space, mla.node, geom))
	// SetupFragmentBuilderForFragmentation (fragmentation_utils.cc:426-430
	// @ a9f50e522efa): resuming after a break inside this node.
	mla.containerBuilder.SetIsFirstForNode(!mla.space.BreakToken.IsBreakInside())

	// Every multicol establishes a block fragmentation context
	// (column_layout_algorithm.cc:333 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a nested one leaves its
	// OOFs to the outermost (HandleOofFragmentation).
	mla.containerBuilder.SetIsBlockFragmentationContextRoot()

	// Callsite 1: seed the unpositioned list marker from the node + break token.
	// Mirrors Blink cla.cc:240–253 (constructor body).
	// ListMarkerBlockNodeIfListItem returns only an outside marker (nil for
	// inside markers and non-list-items).
	if markerNode := mla.node.ListMarkerBlockNodeIfListItem(); markerNode != nil {
		if !markerNode.ListMarkerOccupiesWholeLine() {
			incoming := mla.space.BreakToken
			if incoming == nil || incoming.HasUnpositionedListMarker {
				mla.containerBuilder.SetUnpositionedListMarker(&UnpositionedListMarker{
					MarkerNode: markerNode,
					Item:       mla.node,
				})
			}
		}
	}

	mla.declaredBorderScrollbarPadding = geom.BorderBoxPadding()
	// louis14 clones the block-end decorations along with the block-start
	// ones whenever the style clones (as finishNestedFragment does), where
	// Blink's SetupFragmentBuilderForFragmentation sets it only for a
	// fragmented box not known to fit (fragmentation_utils.cc:440-498 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	mla.containerBuilder.SetShouldCloneBoxEndDecorations(hasCloneBoxDecorationBreak(mla.style))

	mla.contentInlineSize = geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if mla.contentInlineSize < 0 {
		mla.contentInlineSize = 0
	}

	// Resolve column parameters.
	colCount := mla.style.GetColumnCount()
	colWidth := mla.style.GetColumnWidth()
	mla.columnGapSize = mla.style.GetColumnGapMulticolWithBase(mla.contentInlineSize)
	mla.usedColumnCount, mla.usedColWidth = resolveColumnCount(mla.contentInlineSize, colCount, colWidth, mla.columnGapSize)

	// Percentage-resolution base for percentage-height children: the multicol
	// container's explicit content-box block-size, or Indefinite when auto.
	// Mirrors Blink's container_builder_.ExplicitBlockSize().value_or(kIndefiniteSize).
	mla.containerPercentResolutionBlockSize = Indefinite
	if mla.hasExplicitBlock() {
		mla.containerPercentResolutionBlockSize = mla.explicitBlockSize()
	}

	// Phase 12f: remaining_content_block_size_ (Blink cla.cc:313). The content
	// block-size minus what the outer fragmentation context has already consumed
	// for us, clamped to zero. Indefinite when the multicol has an auto height.
	// Used by hasRowHeight()/rowHeight() when column-height is auto.
	mla.remainingContentBlockSize = Indefinite
	if mla.hasExplicitBlock() {
		mla.remainingContentBlockSize = mla.explicitBlockSize()
		if mla.space.BreakToken != nil && mla.space.HasKnownFragmentainerBlockSize() {
			mla.remainingContentBlockSize -= mla.previouslyConsumedContentBlockSize()
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

	// Blink cla.cc:289-294 @ a9f50e522efa: accumulate gap geometry only when
	// the style has a gap rule (HasGapRule; louis14 has no row-rule
	// properties, so that is HasColumnRule). GetColumnCount is 0 for auto.
	mla.gapAccumulator = nil
	if mla.style.HasColumnRule() {
		mla.gapAccumulator = NewColumnGapAccumulator(mla.columnGapSize, mla.rowGapSize,
			mla.style.GetColumnCount(), mla.style.GetColumnCount() == 0)
	}

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
	mla.effectiveMaxBlockSize = Indefinite
	if !mla.hasExplicitBlock() {
		if maxBS, ok := ResolveMaxBlockSize(mla.style, wdm, mla.space, geom); ok {
			mla.effectiveMaxBlockSize = maxBS.Float64()
		}
	}

	// Blink cla.cc:1790-1793: a specified min-block-size may raise the column
	// height cap in constrainColumnBlockSize (min wins over max per CSS 2.1
	// §10.7). Resolved once here; when the block-size is definite the
	// min/max clamp is already folded into explicitBlockSize by
	// CalculateInitialFragmentGeometry, so this only matters for the
	// max-height (auto block-size) case.
	mla.minBlockSize = ResolveMinBlockSize(mla.style, wdm, mla.space, geom).Float64()
	// A continuation that neither starts at a break before the box nor
	// clones its decorations (slice mode, or any fragment past the block
	// end) carries no block-start border/scrollbar/padding, so the builder
	// drops it once here, after the declared edges
	// (declaredBorderScrollbarPadding, min/max) are resolved.
	// Mirrors Blink's ClearBorderScrollbarPaddingBlockStart in
	// SetupFragmentBuilderForFragmentation (fragmentation_utils.cc:504-510
	// @ a9f50e522efa), as BlockLayoutAlgorithm.Layout does.
	if !ShouldIncludeBlockStartBorderPadding(mla.containerBuilder) {
		mla.containerBuilder.ClearBorderScrollbarPaddingBlockStart()
	}

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
			style:       css.CreateAnonymousStyleWithDisplay(mla.style, string(css.DisplayBlock)),
			children:    mla.node.groupedChildrenCache,
			isAnonymous: true,
		}
		mla.node.contentNodeCache.adoptChildren()
	}
	mla.contentNode = mla.node.contentNodeCache

	mla.blockCursor = 0

	mla.outBuilder = MulticolBreakTokenBuilder{}
	mla.outgoingMulticolData = nil
	mla.hasProcessedFirstChild = false
	if result := mla.LayoutChildren(); result != nil {
		return result
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
		remaining := mla.remainingRowHeightAtOffset(mla.blockCursor)
		if mla.isConstrainedByOuterFragmentation() {
			outerLeft := mla.spaceLeftForContent() - mla.blockCursor
			if remaining > outerLeft {
				remaining = outerLeft
			}
			if remaining < 0 {
				remaining = 0
			}
		}
		if remaining < mla.rowHeight() {
			mla.blockCursor += remaining
		}
	}

	// Final block size.
	finalBlockSize := mla.blockCursor
	if mla.hasExplicitBlock() && !mla.isConstrainedByOuterFragmentation() {
		// A definite-height multicol is sized by its CSS height, not its
		// content: Blink's ComputeBlockSizeForFragment resolves the specified
		// block-size regardless of content extent, so content that exceeds it
		// (e.g. an overflowing post-spanner spanner in
		// multicol-span-all-children-height-003) overflows the box rather than
		// stretching it. LOU-370: clamp DOWN to explicitBlockSize as well as up
		// (the prior code only bumped up when content was shorter).
		finalBlockSize = mla.explicitBlockSize()
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
	if mla.style != nil && mla.style.ShouldApplySizeContainment() && !mla.hasExplicitBlock() {
		finalBlockSize = 0
		if finalBlockSize < mla.minBlockSize {
			finalBlockSize = mla.minBlockSize
		}
	}
	// A nested multicol (involved in block fragmentation) is sized by
	// ComputeBlockSizeForFragment + FinishFragmentation (cla.cc:376-393 @
	// a9f50e522efa); declared height left over for later fragments makes it
	// break itself.
	var nestedFinish FinishFragmentationResult
	if mla.isConstrainedByOuterFragmentation() {
		nestedFinish = mla.finishNestedFragment(false, false)
		mla.clearBlockEndExcludedBy(nestedFinish)
		finalBlockSize = math.Max(0, nestedFinish.FragmentBlockSize-geom.BlockBorderPadding())
	}

	// Phase 12e: max-height caps the multicol's content block-size when no
	// explicit height is set. fragment_geometry already applies max-height to
	// BorderBoxSize when the block-size is definite, so this only affects the
	// auto-height case (hasExplicitBlock == false).
	if !mla.isConstrainedByOuterFragmentation() && !mla.hasExplicitBlock() && mla.effectiveMaxBlockSize != Indefinite &&
		finalBlockSize > mla.effectiveMaxBlockSize {
		finalBlockSize = mla.effectiveMaxBlockSize
	}

	mla.addMulticolWithPendingOOFsIfNeeded()

	// Lay out the fragmentainer descendants at the outermost fragmentation
	// context root before the candidates: OutOfFlowLayoutPart::Run calls
	// HandleFragmentation before LayoutCandidates
	// (out_of_flow_layout_part.cc:615-687 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the candidates the
	// laid-out OOFs surface (a fixed child of a fragmented abspos) are
	// handled with the others.
	mla.HandleOofFragmentation(nil)

	var propagatedOOF []OutOfFlowCandidate
	// Candidates whose containing block is an inline in the column content
	// (the anonymous column content propagates them unresolved) resolve
	// here against the inline's fragments in the columns. Blink computes the
	// inline's geometry across fragmentainers
	// (ComputeInlineContainingBlocksForFragmentainer,
	// out_of_flow_layout_part.cc:943-1011 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 unions the inline's
	// fragments as placed in the columns, which matches for an inline
	// within one column.
	if len(mla.containerBuilder.outOfFlowCandidates) > 0 {
		inlineCBCandidates, regularCandidates := splitInlineContainingBlockCandidates(mla.containerBuilder.outOfFlowCandidates)
		if len(inlineCBCandidates) > 0 {
			unresolved, extra := layoutInlineContainingBlockCandidates(mla.ctx, mla.containerBuilder, wdm,
				LogicalSize{InlineSize: mla.contentInlineSize, BlockSize: finalBlockSize}, geom, inlineCBCandidates)
			mla.containerBuilder.outOfFlowCandidates = append(append(regularCandidates, unresolved...), extra...)
		}
	}
	if len(mla.containerBuilder.outOfFlowCandidates) > 0 {
		// The multicol contains its absolute descendants when it is an
		// absolute container, and its fixed ones too when it is a fixed
		// container (transform, filter, containment: Blink's
		// IsAbsoluteContainer / IsFixedContainer, read by the multicol's
		// OutOfFlowLayoutPart, out_of_flow_layout_part.cc:544-545 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		isFixedContainer := mla.node.IsFixedContainer()
		if mla.node.IsAbsoluteContainer() {
			// absoluteCandidates are the candidates this multicol contains.
			var absoluteCandidates, fixedCandidates []OutOfFlowCandidate
			for _, cand := range mla.containerBuilder.outOfFlowCandidates {
				if cand.IsFixedPosition && !isFixedContainer {
					fixedCandidates = append(fixedCandidates, cand)
				} else {
					absoluteCandidates = append(absoluteCandidates, cand)
				}
			}
			// A multicol involved in block fragmentation defers the
			// candidates it contains to the outermost fragmentation context
			// root (should_add_outer_fragmentainer_children_,
			// out_of_flow_layout_part.cc:546-548, :1190-1203 @
			// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Their containing
			// block is this multicol, which is not one of its own columns,
			// so they bubble past it and are laid out in the outer columns.
			if mla.space.HasBlockFragmentation && len(absoluteCandidates) > 0 {
				previouslyConsumed := previouslyConsumedBlockSize(mla.space.BreakToken)
				previouslyConsumedContent := mla.previouslyConsumedContentBlockSize()
				for _, cand := range absoluteCandidates {
					d := LogicalOofNodeForFragmentation{Candidate: cand}
					mla.containerBuilder.AdjustFragmentainerDescendant(&d, previouslyConsumed, previouslyConsumedContent)
					mla.containerBuilder.AddOutOfFlowFragmentainerDescendant(d)
				}
				absoluteCandidates = nil
			}
			if len(absoluteCandidates) > 0 {
				oofPart := &OutOfFlowLayoutPart{
					ctx:                 mla.ctx,
					containingBlockWDM:  wdm,
					containingBlockSize: LogicalSize{InlineSize: mla.contentInlineSize, BlockSize: finalBlockSize},
					geom:                geom,
					resolvesFixed:       isFixedContainer,
				}
				if extra := oofPart.LayoutCandidates(absoluteCandidates, mla.containerBuilder); len(extra) > 0 {
					fixedCandidates = append(fixedCandidates, extra...)
				}
			}
			propagatedOOF = fixedCandidates
		} else {
			propagatedOOF = mla.containerBuilder.outOfFlowCandidates
		}
	}

	if mla.isConstrainedByOuterFragmentation() {
		applyNestedFinish(mla.containerBuilder, nestedFinish)
	} else {
		// Not nested: all remaining content fits (no fragmentation), so
		// only the past-the-block-end clause can drop the block-end edge
		// and the remaining flags are never read.
		const shouldBreakInside = false
		if !ShouldIncludeBlockEndBorderPadding(mla.space.BreakToken, shouldBreakInside, false, false, false, false) {
			mla.containerBuilder.ClearBorderScrollbarPaddingBlockEnd()
		}
	}
	mla.containerBuilder.SetSize(LogicalSize{
		InlineSize: mla.contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})
	mla.setPhysicalBoxData()
	mla.containerBuilder.SetIntrinsicBlockSize(mla.blockCursor)

	// Callsite 2: place any unclaimed list marker before building.
	// Mirrors Blink cla.cc:383 PositionAnyUnclaimedListMarker() call.
	mla.positionAnyUnclaimedListMarker(&mla.blockCursor)

	mla.setGapGeometryIfNeeded()

	result := mla.containerBuilder.Build()

	// FinishFragmentation broke this nested multicol itself: declared
	// block-size remains for the next outer fragmentainer, all children
	// having been seen, so the fragments it resumes in have no columns
	// (column_layout_algorithm.cc:687-694 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Monolithic overflow still
	// to steer clear of also needs a token (ShouldBreakInside,
	// box_fragment_builder.h:494-497 @ a9f50e522efa).
	if mla.isConstrainedByOuterFragmentation() &&
		(nestedFinish.DidBreakSelf || nestedFinish.MonolithicOverflow > 0) {
		result.BreakToken = mla.createBreakToken(nestedFinish)
		result.BreakToken.HasSeenAllChildren = true
	}

	result.PropagatedOOFCandidates = propagatedOOF
	if result.Fragment != nil {
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
		// column-span-all etc.) is the job of the line-box
		// TallestUnbreakable floor (layoutInlineChildren), which makes
		// the multicol grow to its real content extent so the clip stops
		// cutting legitimate content.
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

// addMulticolWithPendingOOFsIfNeeded registers this nested multicol with
// the outermost fragmentation context root when it holds fragmentainer
// descendants: OOFs inside a nested fragmentation context are laid out in
// its columns once the outermost context is reached (LayoutOOFsInMulticol).
// Called after the fragment is finished (FinishFragmentation). Mirrors
// Blink's ColumnLayoutAlgorithm::Layout (column_layout_algorithm.cc:390-400
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (mla *MulticolLayoutAlgorithm) addMulticolWithPendingOOFsIfNeeded() {
	if mla.space.HasBlockFragmentation && mla.containerBuilder.HasOutOfFlowFragmentainerDescendants() {
		mla.containerBuilder.AddMulticolWithPendingOOFs(mla.node, &MulticolWithPendingOOFs{})
	}
}

// HandleOofFragmentation lays out the fragmentainer descendants and the
// pending inner multicols' OOFs when this multicol is the outermost block
// fragmentation context root; a multicol nested in another fragmentation
// context leaves them to it. isContainingBlockUnfinished (nil once layout is
// complete) reports a containing block that continues past this point; its
// descendants are delayed. Mirrors the head of Blink's
// OutOfFlowLayoutPart::HandleFragmentation (out_of_flow_layout_part.cc:
// 717-721, should_add_outer_fragmentainer_children_ :546-548 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), run before LayoutCandidates
// and before each spanner (column_layout_algorithm.cc:640-643).
func (mla *MulticolLayoutAlgorithm) HandleOofFragmentation(isContainingBlockUnfinished func(*LayoutInputNode) bool) {
	if mla.space.HasBlockFragmentation ||
		(!mla.containerBuilder.HasOutOfFlowFragmentainerDescendants() && !mla.containerBuilder.HasMulticolsWithPendingOOFs()) {
		return
	}
	part := mla.newFragmentationContextOOFPart()
	part.isContainingBlockUnfinished = isContainingBlockUnfinished
	part.HandleFragmentation(mla.fragmentainerProgression())
}

// newFragmentationContextOOFPart returns the OutOfFlowLayoutPart that lays
// out fragmentainer descendants in this multicol's columns.
func (mla *MulticolLayoutAlgorithm) newFragmentationContextOOFPart() *fragmentationContextOOFPart {
	return &fragmentationContextOOFPart{
		OutOfFlowLayoutPart: OutOfFlowLayoutPart{ctx: mla.ctx, containingBlockWDM: mla.space.WritingDirection},
		containerBuilder:    mla.containerBuilder,
	}
}

// fragmentainerProgression is the offset from one column to the next.
// Mirrors Blink's GetFragmentainerProgression for kFragmentColumn
// (fragmentation_utils.cc:310-319 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which calls
// ColumnInlineProgression.
func (mla *MulticolLayoutAlgorithm) fragmentainerProgression() LogicalOffset {
	return LogicalOffset{InlineOffset: ColumnInlineProgression(mla.style, mla.contentInlineSize)}
}

// LayoutChildren walks the multicol's parts (column content and spanners) in
// document order and lays them out. Mirrors Blink's
// ColumnLayoutAlgorithm::LayoutChildren (column_layout_algorithm.h:100,
// column_layout_algorithm.cc:551-708 @ a9f50e522efa). It returns the broken
// outer fragment when this multicol breaks inside an outer fragmentation
// context, or nil when Layout() should run its tail. Blink returns a
// BreakStatus and finishes both cases through one FinishFragmentation;
// louis14 still has two finish paths (buildOuterBreakResult and the Layout
// tail) and no RelayoutAndBreakEarlier, so the break result is built here.
func (mla *MulticolLayoutAlgorithm) LayoutChildren() *LayoutResult {
	// Each walker entry is either column content (DescendantNode == nil;
	// BreakToken carries the resume point) or a spanner (DescendantNode ==
	// the leaf spanner; BreakToken carries its resume info if it broke
	// inside). Blink's walker is a LayoutChildren local too (cla.cc:562).
	walker := NewMulticolPartWalker(mla.contentNode, mla.space.BreakToken)

	// marginStrut collapses adjacent spanner block margins and lets a
	// spanner's trailing margin collapse through an empty column line.
	// Mirrors Blink's margin_strut local, passed by pointer to
	// LayoutFragmentationContext and LayoutSpanner (cla.cc:552 @
	// a9f50e522efa). Column content itself never contributes (columns
	// establish a BFC root, cla.cc:554-558 comment).
	var marginStrut MarginStrut
	for !walker.IsFinished() {
		entry := walker.Current()

		if entry.DescendantNode == nil {
			// Column content (Blink cla.cc:574-614).
			result, stop := mla.LayoutFragmentationContext(entry.BreakToken, &marginStrut, walker)
			if result != nil {
				return result
			}
			if stop {
				break
			}
			continue
		}

		// A spanner (Blink cla.cc:627-659). Handle the fragmentainer
		// descendants found before it in the columns before it (cla.cc:
		// 640-643 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); those whose
		// containing block continues after the spanner wait.
		mla.HandleOofFragmentation(func(cb *LayoutInputNode) bool {
			return breakTokenTreeContainsNode(walker.nextColumnToken, cb)
		})
		status := mla.LayoutSpanner(entry.DescendantNode, entry.BreakToken, &marginStrut)

		// Advance the walker past this spanner entry (Blink cla.cc:651).
		// After Next(), the walker either points to the next part
		// (post-spanner column content queued by MoveToSpanner, or a
		// subsequent parent-token entry) or is finished.
		walker.Next()
		if status.status == BreakStatusBrokeBefore {
			// The spanner moves to the next outer fragmentainer;
			// LayoutSpanner already added its break-before token.
			result := mla.breakOuter(walker)
			result.HasForcedBreak = result.HasForcedBreak || status.isForcedBreak
			return result
		}
		// Blink cla.cc:656-660 @ a9f50e522efa: once the spanner broke
		// inside, this fragmentainer is done — content after it resumes in
		// the next one.
		if status.brokeInside {
			return mla.breakOuter(walker)
		}
	}

	// A spanner that continues in a parallel flow (its box fit; only its
	// overflowing content resumes) left its token in outBuilder: this
	// multicol still breaks, carrying it to the next outer fragmentainer
	// (Blink: the builder's child break tokens make FinishFragmentation emit
	// a break token).
	if !mla.outBuilder.IsEmpty() {
		return mla.breakOuter(walker)
	}

	// Blink cla.cc:687-705: all children seen — the leftover strut (e.g. a
	// trailing spanner's block-end margin) contributes to the intrinsic
	// block-size. Break paths (walker unfinished) drop the strut instead.
	if walker.IsFinished() {
		mla.blockCursor += marginStrut.Resolve()
	}
	return nil
}

// LayoutFragmentationContext lays out column content from the walker's
// current column-content entry, one line per row, until the content is done,
// a spanner ends a line (walker.MoveToSpanner), or the outer fragmentainer
// breaks. It returns the broken outer fragment, or stop=true when
// LayoutChildren should leave the walker loop. Mirrors Blink's
// ColumnLayoutAlgorithm::LayoutFragmentationContext
// (column_layout_algorithm.h:105-107, column_layout_algorithm.cc:710-780 @
// a9f50e522efa), including its do/while over wrapped rows (:714-777).
//
// louis14 deviations, kept as they were before the split:
//   - The walker is a parameter: the walker bookkeeping Blink does in
//     LayoutChildren after this returns (cla.cc:583-611) happens here, and
//     each wrapped row's token is installed with
//     walker.AddNextColumnBreakToken as the row starts, where Blink installs
//     the remaining token once, after the loop (cla.cc:610-611).
//   - A first row truncated by the outer fragmentainer breaks right away
//     with its MulticolBreakTokenData; Blink sets the data and keeps looping
//     (cla.cc:763-777).
func (mla *MulticolLayoutAlgorithm) LayoutFragmentationContext(nextColumnToken *BlockBreakToken, marginStrut *MarginStrut, walker *MulticolPartWalker) (result *LayoutResult, stop bool) {
	// isFirstRow: the line is the first row of this column run (the run
	// between spanners, or before the first spanner); row wrap advances the
	// line offset only after the first row. Blink's is_first_row
	// (column_layout_algorithm.cc:715 @ a9f50e522efa).
	isFirstRow := true
	for {
		// Blink cla.cc:717-724: the line offset includes any trailing
		// margin from a preceding spanner. The strut is NOT reset yet —
		// if the line turns out to be empty, the margin collapses
		// through it (and as far as the spec is concerned, the line
		// won't even exist).
		lineOffset := mla.blockCursor + marginStrut.Resolve()

		// Blink cla.cc:726-728 @ a9f50e522efa: a column line after a spanner
		// ends the spanner's main gap.
		if mla.gapAccumulator != nil && isFirstRow {
			mla.gapAccumulator.AddEndSpannerMainGapIfNeeded(lineOffset)
		}

		// Blink cla.cc:730-739 row-advance guard. Fires whenever
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
			// Blink cla.cc:741-746: only after a wrapped row.
			if mla.isConstrainedByOuterFragmentation() && !isFirstRow && mla.hasRowHeight() &&
				mla.rowHeight() > mla.spaceLeftForContent()-lineOffset {
				// No room for another row in the outer fragmentainer —
				// stop now and let the outer context resume any remaining
				// content in the next outer column.
				// nextColumnToken is never nil here: isFirstRow is false
				// only after a wrap, which sets a non-nil token. The nil arm
				// is kept as the pre-split loop had it.
				if nextColumnToken != nil {
					// walker.Current() still points to this column-
					// content entry (we never called walker.Next).
					// flushWalker pushes it (and any remaining
					// parent-token entries) in document order.
					return mla.breakOuter(walker), false
				}
				return nil, true
			}
		}

		rowStart := lineOffset
		line := mla.LayoutLine(nextColumnToken, lineOffset, Indefinite, !isFirstRow, marginStrut)
		if line == nil {
			// Blink cla.cc:575-581, :662-685 @ a9f50e522efa: an outer fragmentainer break
			// was inserted before this line. This multicol takes up the
			// remaining space; the line (walker.Current(), not yet
			// advanced) and everything after it resume in the next outer
			// fragmentainer.
			mla.blockCursor = math.Max(mla.blockCursor, mla.spaceLeftForContent())
			return mla.breakOuter(walker), false
		}

		walker.Next()

		if line.columnSpannerPath == nil {
			// Phase 18 WRITE site (Blink cla.cc:763-774). When the first
			// row of a column-run is truncated by the outer fragmentainer
			// (rowHeight > spaceLeft - rowStart) and column content is
			// still pending, defer to outer with a ConsumedRowBlockSize
			// carrier so the next outer fragment continues the same row at
			// the right phase. Without this, the next outer fragment would
			// start at row offset 0 and re-paint the already-consumed slice.
			if line.breakToken != nil && isFirstRow && mla.isConstrainedByOuterFragmentation() &&
				mla.shouldWrapColumns() && mla.hasRowHeight() {
				// Blink overflow formula: how much of the row exceeds the outer
				// available space. Only emit a carrier when overflow > 0.
				overflow := mla.remainingRowHeightAtOffset(rowStart) - (mla.spaceLeftForContent() - rowStart)
				if overflow > 0 {
					mla.outgoingMulticolData = &MulticolBreakTokenData{
						ConsumedRowBlockSize: mla.rowHeight() - overflow,
					}
					mla.outBuilder.AddBreakToken(line.breakToken)
					return mla.breakOuter(walker), false
				}
			}
			// Phase 12f (Blink cla.cc:776-777): row-wrap — when wrap is in
			// effect and a column row exited with content still pending,
			// continue the same column-run by starting another row below
			// the one we just placed. Driver: column-height-001.html.
			// AddNextColumnBreakToken installs the line's token as the next
			// walker entry (mirrors Blink's
			// MulticolPartWalker::AddNextColumnBreakToken).
			if line.breakToken != nil && mla.shouldWrapColumns() && mla.hasRowHeight() {
				walker.AddNextColumnBreakToken(line.breakToken)
				isFirstRow = false
				nextColumnToken = line.breakToken
				continue
			}
			// Phase 12c: nested multicol hit the outer fragmentainer
			// boundary with content still pending. Emit an outer break
			// result carrying the column-rows continuation so the outer
			// fragmentation context can resume this multicol in its next
			// outer column.
			if line.breakToken != nil && mla.isConstrainedByOuterFragmentation() {
				mla.outBuilder.AddBreakToken(line.breakToken)
				return mla.breakOuter(walker), false
			}
			return nil, true // all column content done (no outer fragmentation)
		}

		// Spanner discovered via ColumnSpannerPath (Blink cla.cc:589-604).
		// Queue it for the next LayoutChildren iteration via MoveToSpanner;
		// the line's break token (post-spanner column content, if any) is
		// stored as the walker's nextColumnToken so it becomes the iteration
		// AFTER the spanner.
		walker.MoveToSpanner(getSpannerFromPath(line.columnSpannerPath), line.breakToken)
		return nil, false
	}
}

// layoutSpannerResult is what LayoutSpanner reports to LayoutChildren.
type layoutSpannerResult struct {
	// status mirrors the BreakStatus Blink's LayoutSpanner returns
	// (column_layout_algorithm.h:125-127 @ a9f50e522efa): BrokeBefore when
	// the spanner is pushed to the next outer fragmentainer.
	status BreakStatus
	// isForcedBreak: the break before the spanner is forced. no Blink
	// analog: Blink records it on the builder's break-before token; louis14
	// ORs it into the outer break result's HasForcedBreak.
	isForcedBreak bool
	// brokeInside: the spanner broke inside (an in-flow break). Stands in
	// for container_builder_.HasInflowChildBreakInside()
	// (column_layout_algorithm.cc:656-657).
	brokeInside bool
}

// LayoutSpanner lays out and places one column spanner. Mirrors Blink's
// ColumnLayoutAlgorithm::LayoutSpanner (column_layout_algorithm.h:125-127,
// column_layout_algorithm.cc:1334-1447 @ a9f50e522efa). Pushing the
// spanner to the next outer fragmentainer adds its break-before to
// mla.outBuilder and returns BreakStatusBrokeBefore; LayoutChildren then
// advances the walker and breaks. Its layout() step (cla.cc:1354-1362) is
// one layoutElement call in the space CreateConstraintSpaceForSpanner
// builds.
func (mla *MulticolLayoutAlgorithm) LayoutSpanner(spanner *LayoutInputNode, breakToken *BlockBreakToken, marginStrut *MarginStrut) layoutSpannerResult {
	wdm := mla.space.WritingDirection
	// The line the spanner ended is done (cla.cc:1338 @ a9f50e522efa).
	mla.spannerPath = nil

	// Blink cla.cc:1339-1347 (@ a9f50e522efa): resolve the spanner's
	// margins against the multicol's content-box inline size and collapse
	// its block-start margin into the running strut BEFORE computing the
	// spanner's block offset. A resumed spanner has already consumed its
	// block-start margin, except when resuming from a forced break-before
	// (AdjustMarginsForFragmentation, fragmentation_utils.h:279-296).
	spannerStyle := spanner.Style()
	var spannerMargins LogicalEdges
	if spannerStyle != nil {
		spannerMargins = ResolveMargins(spannerStyle, wdm, mla.contentInlineSize)
	}
	AdjustMarginsForFragmentation(breakToken, &spannerMargins)
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

	var spanFrag *PhysicalFragment
	var spanHeight float64
	var spanResult *LayoutResult // the spanner's result, for Blink's AddResult propagation
	// Blink cla.cc:1354-1362 @ a9f50e522efa: the layout() step —
	// block_offset = intrinsic_block_size_ + margin_strut->Sum(), measured
	// from the border-box edge, then one spanner space
	// (CreateConstraintSpaceForSpanner) and spanner_node.Layout(space,
	// break_token). A resumed spanner reads its incoming token's
	// ConsumedBlockSize and ChildBreakTokens itself. Run again after a row
	// snap, from the snapped offset.
	var blockOffset float64
	layout := func() *LayoutResult {
		blockOffset = mla.blockStartBorderPadding() + mla.blockCursor + pendingMargin
		return layoutElement(mla.ctx, spanner, mla.CreateConstraintSpaceForSpanner(spanner, blockOffset, breakToken))
	}
	fullResult := layout()
	// Blink cla.cc:1364-1394 @ a9f50e522efa: a spanner that does not fit
	// (with its pending collapsed margin) in the rest of a wrapping row
	// moves to the next row — the row gap at the end of the current row is
	// a main gap — and is laid out again there. When that row does not fit
	// in the outer fragmentainer, the spanner (with the row) is pushed to
	// the next outer fragmentainer.
	if fullResult != nil && fullResult.Fragment != nil &&
		mla.shouldWrapColumns() && mla.hasRowHeight() && mla.rowHeight() > 0 &&
		mla.offsetInCurrentRow(mla.blockCursor+pendingMargin) > 0 &&
		mla.remainingRowHeightAtOffset(mla.blockCursor) < pendingMargin+NewLogicalFragment(wdm, fullResult.Fragment).BlockSize() {
		mla.blockCursor += mla.remainingRowHeightAtOffset(mla.blockCursor)
		if mla.gapAccumulator != nil {
			mla.gapAccumulator.AddMainGap(mla.blockCursor, SpannerGapNone)
		}
		mla.blockCursor += mla.rowGapSize
		if mla.isConstrainedByOuterFragmentation() && mla.rowHeight() > mla.spaceLeftForContent()-mla.blockCursor {
			mla.outBuilder.AddBreakBeforeChild(spanner, false)
			return layoutSpannerResult{status: BreakStatusBrokeBefore}
		}
		fullResult = layout()
	}
	// Blink LayoutSpanner (cla.cc:1396-1411 @ a9f50e522efa): nested in
	// another fragmentation context — also in an outer multicol's initial
	// balancing pass — examine the breakpoint before the spanner like any
	// block child (BreakBeforeChildIfNeeded). A resumed spanner is never
	// broken before (MovePastBreakpoint's IsFirstForNode check,
	// fragmentation_utils.cc:1099-1100 @ a9f50e522efa).
	if mla.space.HasBlockFragmentation && fullResult != nil && breakToken.IsFirstForNode() {
		// Blink cla.cc:1402-1403: FragmentainerOffsetForChildren() +
		// block_offset.
		fragOff := mla.fragmentainerOffsetForChildren() + blockOffset
		status, isForced := BreakBeforeChildIfNeeded(mla.space, spanner, fullResult,
			fragOff, mla.fragmentainerCapacityForChildren(), mla.hasProcessedFirstChild, mla.containerBuilder)
		if status == BreakStatusBrokeBefore {
			// BreakBeforeChild (fragmentation_utils.cc:943-976 @
			// a9f50e522efa): report the space shortage so a balancing outer
			// multicol can stretch its columns.
			if mla.space.HasKnownFragmentainerBlockSize() {
				PropagateSpaceShortage(mla.space, fullResult, fragOff, mla.fragmentainerCapacityForChildren(), mla.containerBuilder)
			}
			mla.outBuilder.AddBreakBeforeChild(spanner, isForced)
			return layoutSpannerResult{status: BreakStatusBrokeBefore, isForcedBreak: isForced}
		}
	}
	// Reported to LayoutChildren, which ends this fragmentainer once the
	// spanner broke inside (Blink cla.cc:656-660 @ a9f50e522efa).
	spannerBrokeInside := false
	if fullResult != nil && fullResult.Fragment != nil {
		spanFrag = fullResult.Fragment
		spanHeight = NewLogicalFragment(wdm, fullResult.Fragment).BlockSize()
		spanResult = fullResult
		// The spanner's own break token resumes it in the next outer
		// fragmentainer: AddResult collects a child's break token whenever
		// the multicol takes part in block fragmentation
		// (FragmentBuilder::PropagateFromFragment, fragment_builder.cc:
		// 455-464 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		if mla.space.HasBlockFragmentation && fullResult.BreakToken != nil {
			// A box that fits with only overflowing content left (its
			// token at block end) continues in a parallel flow; that is
			// not an in-flow break inside (Blink
			// BoxFragmentBuilder::HasInflowChildBreakInside @ a9f50e522efa).
			spannerBrokeInside = !fullResult.BreakToken.IsAtBlockEnd
			mla.outBuilder.AddBreakToken(fullResult.BreakToken)
		}
	}
	if spanFrag != nil {
		// Blink cla.cc:1354-1356: the spanner's block offset includes the
		// collapsed strut. cla.cc:1439-1441: the start-spanner gap
		// boundary is the end of the preceding content — margins live
		// inside the gap.
		spannerBlockOffset := mla.blockCursor + pendingMargin
		if mla.gapAccumulator != nil {
			mla.gapAccumulator.AddStartSpannerMainGapIfNeeded(mla.blockCursor)
		}
		spannerOffset := LogicalOffset{
			// The offset is content-box relative, so only the resolved
			// inline-start margin contributes (cla.cc:1424-1426 @
			// a9f50e522efa).
			InlineOffset: resolveInlineAutoMargins(spannerStyle, wdm, mla.contentInlineSize,
				NewLogicalFragment(wdm, spanFrag).InlineSize(), spannerMargins).InlineStart,
			BlockOffset: spannerBlockOffset,
		}
		mla.containerBuilder.AddChild(spanFrag, spannerOffset)
		// Blink AddResult (cla.cc:1427) → PropagateFromFragment
		// (fragment_builder.cc:357-405 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the spanner's OOF
		// descendants propagate to this multicol. A containing-block search
		// jumps from a spanner straight to the multicol
		// (FindColumnSpannerContainer, layout_object.cc:166-202), which
		// louis14 gets structurally: the spanner is laid out from here, so a
		// positioned ancestor inside the column content never sees them.
		if spanResult != nil {
			PropagateOOFCandidates(spanResult, spannerStyle, wdm, spannerOffset.InlineOffset, spannerOffset.BlockOffset, mla.containerBuilder)
		}
		// Blink AddResult (cla.cc:1427) → PropagateChildBreakValues
		// (box_fragment_builder.cc:226-232, :618-657 @ a9f50e522efa): a
		// leading spanner's break-before becomes this multicol's
		// InitialBreakBefore, so the parent breaks before the multicol
		// when it has container separation; a spanner's break-after
		// joins the break before the next spanner, or becomes the
		// multicol's FinalBreakAfter when nothing follows it.
		if mla.space.ShouldPropagateChildBreakValues() {
			mla.containerBuilder.PropagateChildBreakValues(spanResult)
		}
		// Blink AddResult (cla.cc:1427) → PropagateBreakInfo
		// (box_fragment_builder.cc:479-489 @ a9f50e522efa): the spanner's
		// block-end for fragmentation, its content overflow included,
		// reaches this multicol's.
		if spanResult != nil {
			mla.containerBuilder.PropagateChildBlockSizeForFragmentation(spanResult, spannerOffset)
			mla.propagateBaselineFromChild(spanResult, spannerBlockOffset)
		}
		if spanResult != nil {
			mla.attemptToPositionListMarker(spanFrag, spanResult, spannerBlockOffset)
		}
		// Blink cla.cc:1436-1443: advance past the spanner box; reset the
		// strut and seed it with the block-end margin, which collapses
		// with a following spanner or positions the next column line —
		// and is dropped when this fragmentainer breaks inside the
		// spanner (the broke path never flushes the strut).
		mla.blockCursor = spannerBlockOffset + spanHeight
		*marginStrut = MarginStrut{}
		marginStrut.Append(spannerMargins.BlockEnd)
		mla.hasProcessedFirstChild = true
	}

	return layoutSpannerResult{status: BreakStatusContinue, brokeInside: spannerBrokeInside}
}

// buildOuterBreakResult finalises the in-progress builder as a fragment that
// breaks inside this multicol and returns its layout result, whose break
// token's ChildBreakTokens are the document-order entries accumulated in
// mla.outBuilder. Mirrors Blink's LayoutChildren break tail
// (column_layout_algorithm.cc:662-666 @ a9f50e522efa: take up the remaining
// space) followed by the Layout tail's FinishFragmentation and ToBoxFragment
// (column_layout_algorithm.cc:389-431). louis14 keeps this second finish
// path; the main path's tail in Layout() is the other.
func (mla *MulticolLayoutAlgorithm) buildOuterBreakResult() *LayoutResult {
	// Blink cla.cc:662-666 @ a9f50e522efa: broke in the main flow (an
	// unfinished part, or an in-flow child broke inside) — this multicol
	// takes up the remaining space. Every outgoing token is in-flow
	// (BoxFragmentBuilder::AddBreakToken's default,
	// box_fragment_builder.cc:381-389 @ a9f50e522efa) except a spanner
	// continuing in a parallel flow (its token at block end).
	hasInflowChildBreakInside := false
	for _, tok := range mla.outBuilder.Children() {
		if tok.Node == mla.contentNode || !tok.IsAtBlockEnd {
			hasInflowChildBreakInside = true
		}
	}
	if hasInflowChildBreakInside {
		mla.blockCursor = math.Max(mla.blockCursor, mla.spaceLeftForContent())
	}
	finish := mla.finishNestedFragment(true, hasInflowChildBreakInside)
	applyNestedFinish(mla.containerBuilder, finish)
	mla.clearBlockEndExcludedBy(finish)
	mla.addMulticolWithPendingOOFsIfNeeded()
	mla.containerBuilder.SetSize(LogicalSize{
		InlineSize: mla.contentInlineSize + mla.containerBuilder.InitialFragmentGeometry().InlineBorderPadding(),
		BlockSize:  finish.FragmentBlockSize,
	})
	mla.containerBuilder.SetIntrinsicBlockSize(mla.blockCursor)
	mla.containerBuilder.SetLayoutNode(mla.node)
	mla.setPhysicalBoxData()
	mla.setGapGeometryIfNeeded()
	result := mla.containerBuilder.Build()
	result.BreakToken = mla.createBreakToken(finish)
	result.BreakToken.ChildBreakTokens = mla.outBuilder.Children()
	result.BreakToken.MulticolData = mla.outgoingMulticolData
	// No part left to resume: all children have been seen, so the walker
	// finishes at once in the next fragment instead of laying the content
	// out again (column_layout_algorithm.cc:66-77, :687-694 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; multicol-nested-011).
	if mla.outBuilder.IsEmpty() {
		result.BreakToken.HasSeenAllChildren = true
	}
	// Forward unplaced marker to the break token so the resumed fragment
	// re-seeds it. Mirrors Blink's FinishFragmentation marker plumbing.
	if mla.containerBuilder.GetUnpositionedListMarker().IsValid() {
		result.BreakToken.HasUnpositionedListMarker = true
	}
	if len(mla.containerBuilder.outOfFlowCandidates) > 0 {
		result.PropagatedOOFCandidates = mla.containerBuilder.outOfFlowCandidates
	}
	return result
}

// flushWalker pushes the walker's remaining entries to mla.outBuilder so they
// resume in the next outer fragmentainer. Mirrors Blink's loop over the
// unfinished parts in LayoutChildren (column_layout_algorithm.cc:668-686 @
// a9f50e522efa), which forwards them via container_builder_.AddBreakToken /
// AddBreakBeforeChild. Every mid-loop break result runs it, so the
// post-spanner column-content token installed by MoveToSpanner and any
// further parent-token entries propagate to the next outer column.
func (mla *MulticolLayoutAlgorithm) flushWalker(walker *MulticolPartWalker) {
	for !walker.IsFinished() {
		e := walker.Current()
		if e.BreakToken != nil {
			mla.outBuilder.AddBreakToken(e.BreakToken)
		} else if e.DescendantNode != nil {
			mla.outBuilder.AddBreakBeforeChild(e.DescendantNode, false)
		}
		walker.Next()
	}
}

// breakOuter ends this outer fragment: the remaining walker entries resume
// in the next outer fragmentainer. no Blink analog as a method: it is
// LayoutChildren's break tail (column_layout_algorithm.cc:662-686 @
// a9f50e522efa) followed by the Layout tail, which Blink reaches by
// breaking out of the walker loop.
func (mla *MulticolLayoutAlgorithm) breakOuter(walker *MulticolPartWalker) *LayoutResult {
	mla.flushWalker(walker)
	return mla.buildOuterBreakResult()
}

// createBreakToken builds this multicol's outgoing break token from a
// FinishFragmentation result: the node, the consumed (border-box)
// block-size, the sequence number and IsAtBlockEnd. Each finish path then
// sets what only it carries (child break tokens, MulticolBreakTokenData,
// HasSeenAllChildren, the unpositioned list marker). Mirrors
// BlockBreakToken::Create and its constructor (block_break_token.cc:34-42,
// :65-78 @ a9f50e522efa), which read these fields from the builder.
func (mla *MulticolLayoutAlgorithm) createBreakToken(finish FinishFragmentationResult) *BlockBreakToken {
	return &BlockBreakToken{
		Node:               mla.node,
		ConsumedBlockSize:  layoutunit.FromFloat64Round(finish.ConsumedBlockSize),
		SequenceNumber:     sequenceNumberAfter(mla.space.BreakToken),
		IsAtBlockEnd:       finish.IsAtBlockEnd,
		MonolithicOverflow: finish.MonolithicOverflow,
	}
}

// setPhysicalBoxData records the fragment's physical margin, border,
// padding and scrollbar (the builder's geometry, its excluded block edges cleared) on
// builder. no Blink analog: louis14's PhysicalBoxData (Blink's fragment
// builder carries these itself).
func (mla *MulticolLayoutAlgorithm) setPhysicalBoxData() {
	wdm := mla.space.WritingDirection
	mla.containerBuilder.SetBoxData(&PhysicalBoxData{
		Margin:    ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize.Float64()), wdm),
		Border:    ToPhysicalEdges(mla.containerBuilder.InitialFragmentGeometry().Border, wdm),
		Padding:   ToPhysicalEdges(mla.containerBuilder.InitialFragmentGeometry().Padding, wdm),
		Scrollbar: ToPhysicalEdges(mla.containerBuilder.InitialFragmentGeometry().Scrollbar, wdm),
	})
}

// layoutLineResult is what LayoutLine reports about the line it laid out.
// Blink returns the last column's LayoutResult instead
// (column_layout_algorithm.h:109-119 @ a9f50e522efa); louis14 returns the
// pieces of it the caller reads.
type layoutLineResult struct {
	// columnSpannerPath is the spanner that ended the line, if any: Blink's
	// result->GetColumnSpannerPath() (column_layout_algorithm.cc:589).
	columnSpannerPath *ColumnSpannerPath
	// breakToken is the column-content token to resume at: Blink's
	// result->GetPhysicalFragment().GetBreakToken()
	// (column_layout_algorithm.cc:586-587). On a line without a spanner it is
	// the last column's token.
	breakToken *BlockBreakToken
}

// LayoutLine implements the outer stretch / inner per-column loop for one
// line of columns — all columns between two spanners (or the full content if
// no spanners). It returns nil when an outer fragmentainer break is inserted
// before the line; nothing is committed then. Otherwise a non-empty line
// moves the layout position (blockCursor) past itself and resets
// marginStrut, whose pending margin is already in lineOffset; an empty line
// leaves both, so the margin collapses through it (cla.cc:1218-1251).
// Mirrors Blink's ColumnLayoutAlgorithm::LayoutLine
// (column_layout_algorithm.h:115-119, column_layout_algorithm.cc:799-1331 @
// a9f50e522efa), which returns nullptr in that case (:1214).
//
// Blink's has_wrapped parameter is not ported: louis14 records the row gap
// in LayoutFragmentationContext.
func (mla *MulticolLayoutAlgorithm) LayoutLine(
	nextColumnToken *BlockBreakToken,
	lineOffset float64,
	minimumColumnBlockSize float64,
	hasWrapped bool,
	marginStrut *MarginStrut,
) *layoutLineResult {
	wdm := mla.space.WritingDirection

	// Balance if column-fill is 'balance', and also inside another
	// multicol performing its initial column balancing pass (an unknown
	// fragmentainer block-size), so as not to overstretch the outer columns.
	// Recomputed for every line (cla.cc:840-843 @ a9f50e522efa); a
	// column-fill:auto line that ends at a spanner switches only itself to
	// balancing (:1071-1081).
	columnFill := mla.style.GetColumnFill()
	balanceColumns := columnFill == "balance" || columnFill == "" ||
		(mla.space.HasBlockFragmentation && !mla.space.HasKnownFragmentainerBlockSize())

	// Outer remaining space: cap column height so columns don't exceed what
	// fits in the current outer fragmentainer. Mirrors Blink's
	// is_constrained_by_outer_fragmentation_context_ (cla.cc:311-318 @
	// a9f50e522efa) and available_outer_space (cla.cc:819-824).
	// available_outer_space = FragmentainerSpaceLeftForChildren() −
	// line_offset, with line_offset from the border-box edge: nothing is
	// reserved for the block-end border/padding (it belongs to the last
	// fragment), and 0 is a real limit. Indefinite = not constrained.
	outerRemaining := mla.spaceLeftForContent()
	if mla.isConstrainedByOuterFragmentation() {
		outerRemaining = math.Max(0, mla.spaceLeftForContent()-lineOffset)
		// cla.cc:819-822 @ a9f50e522efa: a regrown line may exceed the space left.
		if minimumColumnBlockSize != Indefinite {
			outerRemaining = math.Max(minimumColumnBlockSize, outerRemaining)
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
	overflowInline := !freeBalancing && mla.columnsOverflowInInlineDirection(
		initialColBlockSize != Indefinite && initialColBlockSize <= outerRemaining)

	// Determine the used column block-size (Blink cla.cc:840-856 @
	// a9f50e522efa). Balanced columns come first, also under column-height:
	// the balanced size is then capped by the row's remaining height
	// (constrainColumnBlockSize, Blink ConstrainColumnBlockSize :1815-1818).
	// Phase 12f (Blink cla.cc:806-809): otherwise a non-auto column-height
	// fixes the column block-size to the row's remaining height at this line
	// offset. This is the hard upper bound on a single row of columns.
	// Phase 12e (Blink behaviour): column-fill:auto + max-height on an auto-
	// height multicol uses max-height as the column block-size so columns fill
	// sequentially up to that height. Otherwise an unconstrained line's
	// column block-size is content-based (has_content_based_block_size).
	var colBlockSize float64
	shrinkToFitColumnBlockSize := false
	switch {
	case balanceColumns:
		colBlockSize = mla.resolveColumnAutoBlockSize(mla.contentNode, wdm, mla.usedColWidth, mla.usedColumnCount, mla.columnGapSize, nextColumnToken, mla.containerPercentResolutionBlockSize, true)
		colBlockSize = mla.constrainColumnBlockSize(colBlockSize, mla.hasExplicitBlock(), mla.explicitBlockSize(), mla.effectiveMaxBlockSize, outerRemaining, lineOffset)
	case !mla.hasAutoColumnHeight():
		colBlockSize = mla.constrainColumnBlockSize(initialColBlockSize, mla.hasExplicitBlock(), mla.explicitBlockSize(), mla.effectiveMaxBlockSize, outerRemaining, lineOffset)
	case mla.hasExplicitBlock():
		colBlockSize = mla.constrainColumnBlockSize(initialColBlockSize, mla.hasExplicitBlock(), mla.explicitBlockSize(), mla.effectiveMaxBlockSize, outerRemaining, lineOffset)
	case mla.effectiveMaxBlockSize != Indefinite:
		colBlockSize = mla.constrainColumnBlockSize(mla.effectiveMaxBlockSize, mla.hasExplicitBlock(), mla.explicitBlockSize(), mla.effectiveMaxBlockSize, outerRemaining, lineOffset)
	default:
		if mla.isConstrainedByOuterFragmentation() {
			// Blink cla.cc:857-872 @ a9f50e522efa: when an auto-height
			// multicol with column-fill:auto has no explicit/max height
			// but is constrained by an outer fragmentainer, use the outer
			// remaining space as the column block-size, not a
			// content-based one (has_content_based_block_size requires
			// !is_constrained_by_outer_fragmentation_context_).
			colBlockSize = mla.constrainColumnBlockSize(outerRemaining, mla.hasExplicitBlock(), mla.explicitBlockSize(), mla.effectiveMaxBlockSize, outerRemaining, lineOffset)
			shrinkToFitColumnBlockSize = true
		} else {
			// An unconstrained column-fill:auto line still gets a
			// content-based column block-size, unbalanced
			// (has_content_based_block_size, cla.cc:845-856 @ a9f50e522efa).
			colBlockSize = mla.resolveColumnAutoBlockSize(mla.contentNode, wdm, mla.usedColWidth, mla.usedColumnCount, mla.columnGapSize, nextColumnToken, mla.containerPercentResolutionBlockSize, false)
			colBlockSize = mla.constrainColumnBlockSize(colBlockSize, mla.hasExplicitBlock(), mla.explicitBlockSize(), mla.effectiveMaxBlockSize, outerRemaining, lineOffset)
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
		initialColBlockSize == 0 && nextColumnToken != nil {
		colBlockSize = 1
	}

	var finalColumns []struct {
		fragment      *PhysicalFragment
		offset        LogicalOffset
		propagatedOOF []OutOfFlowCandidate
		result        *LayoutResult // for PropagateBaselineFromChild
	}
	var finalColBreakToken *BlockBreakToken
	var lastInnerResult *LayoutResult

	// Blink cla.cc:886-902 @ a9f50e522efa: when the multicol is
	// constrained by an outer fragmentation context, is not itself a
	// break-inside continuation (its own incoming token, not the column
	// token: a post-spanner line resumes from a break-inside column token
	// while the multicol is still in its first fragment), columns don't
	// overflow inline, and either prior content has contributed block size
	// (intrinsic_block_size_: block-start border/padding, earlier lines,
	// spanners) or the multicol is not at the start of the outer
	// fragmentainer, a sub-perfect break appeal in any column should
	// propagate to the multicol's own result so the outer's stretch loop
	// can retry with more space. (Narrower than hasViolatingBreak below,
	// which drives this multicol's own stretch loop regardless of the
	// outer context.)
	mayHaveMoreSpace := mla.isConstrainedByOuterFragmentation() &&
		!mla.space.BreakToken.IsBreakInside() &&
		!overflowInline &&
		(lineOffset > 0 || mla.blockStartBorderPadding() > 0 || !mla.space.IsAtFragmentainerStart)
	minBreakAppeal := BreakAppealPerfect

	// Outer stretch loop: mirrors Blink's do { ... } while (true) at cla.cc:967.
	for {
		var columns []struct {
			fragment      *PhysicalFragment
			offset        LogicalOffset
			propagatedOOF []OutOfFlowCandidate
			result        *LayoutResult // for PropagateBaselineFromChild
		}
		minSpaceShortage := math.MaxFloat64
		hasShortage := false
		actualColumnCount := 0
		forcedBreakCount := 0
		hasViolatingBreak := false

		minBreakAppeal = BreakAppealPerfect

		// Reset break token to the incoming token at the start of each iteration.
		colBreakToken := nextColumnToken
		inlineOffset := 0.0

		// Inner per-column loop. Runs while a break token remains (Blink's
		// do/while at cla.cc:929-1068); the cap at column-count applies only
		// when inline overflow is not permitted (cla.cc:1012-1025 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). With column-wrap:nowrap
		// under an outer fragmentation context, that is unless the column is
		// known to fit the outer fragmentainer (ColumnsOverflowInInlineDirection,
		// cla.cc:1823-1842): otherwise the remaining content resumes in the
		// next outer fragmentainer's line.
		for col := 0; col < mla.usedColumnCount || overflowInline; col++ {
			// IsInsideBalancedColumns is set when the line is balanced (also
			// under column-height; Blink passes balance_columns, cla.cc:929-935
			// @ a9f50e522efa).
			// Blink cla.cc:930-935 @ a9f50e522efa: pass the worst appeal seen
			// so far in this row (min_break_appeal, unset → LastResort) into
			// the column's space. Only set once a column has completed under
			// the may_have_more_space gate; it lets a resumed column break
			// before its first child (IsBreakableAtStartOfResumedContainer)
			// to push content to the next outer fragmentainer.
			colMinBreakAppeal := BreakAppealLastResort
			if mayHaveMoreSpace && col > 0 {
				colMinBreakAppeal = minBreakAppeal
			}
			colSpace := mla.createConstraintSpaceForColumn(wdm, mla.usedColWidth, colBlockSize, mla.containerPercentResolutionBlockSize, colBreakToken, balanceColumns, colMinBreakAppeal)
			columnAlgorithm := NewBlockLayoutAlgorithm(mla.ctx, mla.contentNode, colSpace)
			columnAlgorithm.SetBoxType(BoxTypeColumn)
			result := columnAlgorithm.Layout()
			if result == nil || result.Fragment == nil {
				break
			}

			// The column algorithm's builder tagged the fragment
			// BoxTypeColumn (Blink SetBoxType(kColumnBox),
			// column_layout_algorithm.cc:945 @ a9f50e522efa).
			colFrag := result.Fragment
			colHeight := NewLogicalFragment(wdm, colFrag).BlockSize()
			// Blink does not set any per-column paint clip (BoxFragmentPainter::PaintBlockChild
			// fragmentainer branch). Phase 12h F2 partial added ClipBlockAxisOnly here to handle
			// nested-multicol leaf overflow, but that was the wrong level of abstraction — it
			// clipped monolithic pre-spanner content that legitimately overflows the balance-
			// estimate column height. Removed in Phase 16.b.3; row advance now uses
			// BlockSizeForFragmentation (Phase 16.b.2) so overflow is placed correctly.

			columns = append(columns, struct {
				fragment      *PhysicalFragment
				offset        LogicalOffset
				propagatedOOF []OutOfFlowCandidate
				result        *LayoutResult
			}{
				fragment:      colFrag,
				offset:        LogicalOffset{InlineOffset: inlineOffset, BlockOffset: lineOffset},
				propagatedOOF: result.PropagatedOOFCandidates,
				result:        result,
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
			if colHeight > colBlockSize {
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
			// Blink cla.cc:1022-1025: the row ends at column-count when
			// content remains and inline overflow is not permitted.
			rowCapped := colBreakToken != nil && !overflowInline && col+1 >= mla.usedColumnCount
			// Blink cla.cc:1027-1037: track the worst break appeal across
			// columns when the outer fragmentainer may have more space. Blink
			// leaves the column loop on the cap before folding the appeal in,
			// so the column that ends a capped row never contributes.
			if mayHaveMoreSpace && !rowCapped {
				if result.BreakAppeal < minBreakAppeal {
					minBreakAppeal = result.BreakAppeal
				}
				// Blink cla.cc:1038-1066 @ a9f50e522efa: a column overflowed by content
				// taller than the outer fragmentainer has room for. Inside
				// balanced columns, let the outer multicol stretch; and lay
				// the line out once more with columns tall enough to
				// encompass the overflow — whether the taller multicol then
				// stays is the parent's call (MovePastBreakpoint).
				blockEndOverflow := NewLogicalFragment(wdm, colFrag).BlockEndScrollableOverflow()
				if lineOffset+blockEndOverflow > mla.spaceLeftForContent() {
					if mla.space.IsInsideBalancedColumns && !mla.space.IsInitialColumnBalancingPass && hasShortage {
						mla.containerBuilder.PropagateSpaceShortage(minSpaceShortage)
					}
					// A regrown line never regrows again (Blink recurses at
					// most once: !minimum_column_block_size).
					if minimumColumnBlockSize == Indefinite && blockEndOverflow > colBlockSize {
						return mla.LayoutLine(nextColumnToken, lineOffset, blockEndOverflow, hasWrapped, marginStrut)
					}
				}
			}
			if colBreakToken == nil {
				break // all content fit
			}
			inlineOffset += mla.usedColWidth + mla.columnGapSize

			// Stop at column-count only when inline overflow is not
			// permitted — otherwise keep placing overflow columns until the
			// content is consumed.
			if rowCapped {
				break
			}
		}

		finalColumns = columns
		finalColBreakToken = colBreakToken

		// column-fill:auto + spanner: flip to balanced mode and retry (cla.cc:1130–1140).
		if !balanceColumns {
			if lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil {
				balanceColumns = true
				colBlockSize = mla.resolveColumnAutoBlockSize(mla.contentNode, wdm, mla.usedColWidth, mla.usedColumnCount, mla.columnGapSize, nextColumnToken, mla.containerPercentResolutionBlockSize, true)
				colBlockSize = mla.constrainColumnBlockSize(colBlockSize, mla.hasExplicitBlock(), mla.explicitBlockSize(), mla.effectiveMaxBlockSize, outerRemaining, lineOffset)
				continue
			}
			break
		}

		// The line's OOFs get laid out once all columns complete layout, but
		// they affect column balancing: lay the columns' fragmentainer
		// descendants out in a dry run over these columns and fold the new
		// columns, space shortage and violating breaks they would cause into
		// the stretch decision. Mirrors Blink cla.cc:1087-1129 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b (has_oof_fragmentainer_
		// descendants is set only while balancing, cla.cc:973-978). The
		// containing-block adjustment Blink passes is not needed: louis14
		// finds containing blocks in the line's columns (plan G20).
		// As before a spanner, descendants whose containing block continues
		// past this line wait (out_of_flow_layout_part.cc:1611-1621).
		var lineDescendants []LogicalOofNodeForFragmentation
		for _, c := range columns {
			mla.containerBuilder.PropagateOOFFragmentainerDescendants(c.fragment, c.offset, LogicalOffset{}, layoutunit.LayoutUnit{}, nil, nil, &lineDescendants)
		}
		if balanceColumns && len(lineDescendants) > 0 {
			lineColumns := make([]logicalChildLink, 0, len(columns))
			for _, c := range columns {
				lineColumns = append(lineColumns, logicalChildLink{offset: c.offset, fragment: c.fragment})
			}
			var columnBalancingInfo ColumnBalancingInfo
			part := mla.newFragmentationContextOOFPart()
			part.childFragmentStorage = &lineColumns
			part.columnBalancingInfo = &columnBalancingInfo
			// The line's last column's outgoing token (colBreakToken is not
			// advanced past a column that ends at a spanner).
			lineBreakToken := columns[len(columns)-1].result.BreakToken
			part.isContainingBlockUnfinished = func(cb *LayoutInputNode) bool {
				return breakTokenTreeContainsNode(lineBreakToken, cb)
			}
			part.LayoutFragmentainerDescendants(&lineDescendants, mla.fragmentainerProgression())
			actualColumnCount += columnBalancingInfo.NumNewColumns
			if columnBalancingInfo.MinimalSpaceShortage > 0 {
				minSpaceShortage = min(minSpaceShortage, columnBalancingInfo.MinimalSpaceShortage)
				hasShortage = true
			}
			hasViolatingBreak = hasViolatingBreak || columnBalancingInfo.HasViolatingBreak
		}

		// Acceptance condition (Blink cla.cc:1034–1037):
		// Accept when no violations, column count within limit, and either
		// content fully fits (no remaining break token) or a spanner was hit
		// (in which case this row is done regardless).
		lastHasSpanner := lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil
		if !hasViolatingBreak &&
			actualColumnCount <= mla.usedColumnCount &&
			(colBreakToken == nil || lastHasSpanner) {
			break
		}

		// Blink cla.cc:1150-1173 @ a9f50e522efa: stretch the columns by the
		// minimal space shortage and retry. With no soft break opportunity
		// left (forced breaks already use up the columns) no stretch can
		// help — unless this multicol is nested in another fragmentation
		// context: then the columns take up all the space the multicol
		// fragment may use (a box that breaks inside uses all the remaining
		// fragmentainer space, css-break-3 §4.4), which
		// constrainColumnBlockSize gives an unbounded request.
		var newColBlockSize float64
		if mla.usedColumnCount <= forcedBreakCount+1 {
			if !mla.isConstrainedByOuterFragmentation() {
				break
			}
			newColBlockSize = math.Inf(1)
		} else {
			if !hasShortage {
				break
			}
			newColBlockSize = colBlockSize + minSpaceShortage
		}
		newColBlockSize = mla.constrainColumnBlockSize(newColBlockSize, mla.hasExplicitBlock(), mla.explicitBlockSize(), mla.effectiveMaxBlockSize, outerRemaining, lineOffset)
		if newColBlockSize <= colBlockSize {
			// Nested balancing: we can't stretch any further in this inner
			// multicol. Report the minimum shortage upward so the outer
			// multicol's stretch loop can widen its own columns on the next
			// iteration, then try us again with more outer space.
			// Mirrors Blink cla.cc:1235-1250.
			if mla.space.IsInsideBalancedColumns &&
				!mla.space.IsInitialColumnBalancingPass &&
				hasShortage {
				mla.containerBuilder.PropagateSpaceShortage(minSpaceShortage)
			}
			break
		}
		colBlockSize = newColBlockSize
	}

	// Blink cla.cc:1194-1213 @ a9f50e522efa: with container separation (a
	// line offset past the multicol's border-box start), breaking before this
	// line is fine — and better, when the line does not fit nicely in the
	// outer fragmentation context. Nothing is committed; the caller breaks
	// and resumes the line in the next outer fragmentainer. The line's block
	// size for fragmentation is its last column's: the column's content
	// extent (a fragmentainer's BlockSizeForFragmentation, children only).
	// (Blink gates on HasBlockFragmentation; MovePastBreakpoint is a no-op
	// while the outer fragmentainer size is unknown, so skip the work.)
	if mla.isConstrainedByOuterFragmentation() && mla.blockStartBorderPadding()+lineOffset > 0 &&
		lastInnerResult != nil && len(finalColumns) > 0 {
		fragmentainerBlockOffset := mla.fragmentainerOffsetForChildren() + mla.blockStartBorderPadding() + lineOffset
		capacity := mla.fragmentainerCapacityForChildren()
		if !MovePastBreakpoint(mla.space, lastInnerResult, fragmentainerBlockOffset, capacity, BreakAppealPerfect, mla.containerBuilder) {
			// TODO(LOU-388): Blink also adds a last-resort break before the
			// multicol itself when no column token exists yet (cla.cc:1206-
			// 1212 @ a9f50e522efa); louis14's walker resumes from the start.
			return nil
		}
	}

	// Blink cla.cc:1027-1066: propagate the worst column break appeal of the
	// accepted row to the multicol's builder (ClampBreakAppeal) so the outer
	// fragmentainer can retry with a better breakpoint. Build() copies it
	// into the result's BreakAppeal.
	if minBreakAppeal < BreakAppealPerfect {
		mla.containerBuilder.ClampBreakAppeal(minBreakAppeal)
	}

	// Blink cla.cc:1218-1231: a line is "empty" when its single column box has
	// no extent and no children — e.g. the spanner-detection line between two
	// adjacent spanners — OR when its only content is the empty continuation
	// of a resumed block whose next child is the spanner (the column result's
	// IsEmptySpannerParent, cla.cc:990 @ a9f50e522efa).
	isEmptySpannerParent := lastInnerResult != nil && lastInnerResult.IsEmptySpannerParent
	lineIsEmpty := colBlockSize == 0 &&
		len(finalColumns) == 1 &&
		(len(finalColumns[0].fragment.Children) == 0 || isEmptySpannerParent)

	// Blink cla.cc:1262-1268 @ a9f50e522efa: right after a column wrap, now
	// that another line of columns is certain, add the preceding row gap so
	// a row rule can go there and the previous row's column rules end.
	if mla.gapAccumulator != nil && hasWrapped && mla.rowGapSize > 0 {
		mla.gapAccumulator.AddMainGap(lineOffset-mla.rowGapSize, SpannerGapNone)
	}
	// Commit final columns to the parent builder.
	seenOOF := map[*LayoutInputNode]bool{}
	isFirstCol := true
	columnIndexInRow := 0
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
		// Columns are added with AddChild, not AddResult, so their
		// BlockSizeForFragmentation does not reach this multicol's (Blink
		// column_layout_algorithm.cc:1275 @ a9f50e522efa).
		mla.containerBuilder.AddChild(col.fragment, col.offset)
		// Callsite 1 (Track B): propagate baseline from each column child.
		// Mirrors Blink cla.cc:1336 PropagateBaselineFromChild loop.
		if col.result != nil {
			mla.propagateBaselineFromChild(col.result, col.offset.BlockOffset)
		}
		// Callsite 3 (Track C): attempt marker placement on first column only.
		// Mirrors Blink cla.cc:1302: "Only the first column in a line may attempt
		// to place any unpositioned list-item."
		if isFirstCol && col.result != nil {
			mla.attemptToPositionListMarker(col.fragment, col.result, col.offset.BlockOffset)
			isFirstCol = false
		}
		// Blink cla.cc:1295-1311 @ a9f50e522efa: every column after the
		// first in a row has a cross gap before it, recorded once per column
		// position across rows.
		if mla.gapAccumulator != nil {
			if columnIndexInRow > 0 && mla.gapAccumulator.ShouldAddCrossGapAt(columnIndexInRow) {
				mla.gapAccumulator.AddCrossGap(col.offset.InlineOffset)
			}
			mla.gapAccumulator.SetFirstColumnOffsetIfNeeded(col.offset)
		}
		columnIndexInRow++
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
			mla.containerBuilder.AddOutOfFlowCandidate(cand)
		}
		// Phase 25 Cmt-3: forward any deferred OOF fragmentainer descendants
		// the column carries on its content fragment (an abspos in the column
		// flow whose CB is fragmented — promoted to a fragmentainer descendant
		// at the CB's BLA). The column's offset within the multicol becomes
		// the parent-side translation for ContainingBlock.Offset accumulation.
		// StaticPosition is preserved CB-relative (see PropagateOOFFragmentainerDescendants).
		if col.fragment != nil && col.fragment.FragmentedOofData != nil {
			mla.containerBuilder.PropagateOOFFragmentainerDescendants(
				col.fragment,
				col.offset,
				LogicalOffset{},
				layoutunit.LayoutUnit{},
				nil,
				nil,
				nil,
			)
		}
	}
	// Blink cla.cc:1316-1319 @ a9f50e522efa.
	if mla.gapAccumulator != nil {
		mla.gapAccumulator.UpdateMaxColumnsInRow(len(finalColumns))
		mla.gapAccumulator.AddNumberOfColumnsForCurrentRow(len(finalColumns))
	}

	// Blink cla.cc:906, :927, :949-971 @ a9f50e522efa: the line contributes
	// its used column block-size. When that size was derived from the outer
	// fragmentainer (shrink-to-fit), a single-column line contributes only
	// the column's content, overflow included (its BlockSizeForFragmentation),
	// so the multicol shrinks to content rather than consuming all outer
	// remaining space; Blink clears the flag after the first column and
	// resets the contribution to the column block-size for every later one.
	contribution := colBlockSize
	if shrinkToFitColumnBlockSize && len(finalColumns) == 1 {
		contribution = math.Min(colBlockSize, finalColumns[0].result.BlockSizeForFragmentation)
	}
	// Blink cla.cc:1229-1251: a non-empty line moves the layout position
	// past itself (lineOffset already holds the strut) and resets the
	// strut; column content also separates a preceding spanner's
	// break-after from what follows. An empty line — e.g. the
	// spanner-detection line between two adjacent spanners — leaves both, so
	// the pending margin collapses through.
	if !lineIsEmpty {
		mla.hasProcessedFirstChild = true
		mla.containerBuilder.SetPreviousBreakAfter("auto")
		mla.blockCursor = lineOffset + contribution
		*marginStrut = MarginStrut{}
	}

	// The spanner that ended the line and the column-content token to resume
	// at.
	if lastInnerResult != nil && lastInnerResult.ColumnSpannerPath != nil {
		return &layoutLineResult{
			columnSpannerPath: lastInnerResult.ColumnSpannerPath,
			breakToken:        lastInnerResult.BreakToken,
		}
	}
	return &layoutLineResult{
		breakToken: finalColBreakToken,
	}
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
	// Rounded up to a LayoutUnit (1/64px), as Blink's
	// LayoutUnit::FromFloatCeil (column_layout_algorithm.cc:1564-1572 @
	// a9f50e522efa).
	return layoutunit.FromFloat64Ceil(r.contentBlockSize / float64(r.implicitBreaksAssumed+1)).Float64()
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
	balanceColumns bool,
) float64 {
	// The balancing space: available block-size indefinite, percentages
	// against the multicol's content box, no fixed block-size. Mirrors
	// Blink's CreateConstraintSpaceForBalancing (column_layout_algorithm.cc:
	// 1844-1863 @ a9f50e522efa). FragmentainerBlockSize stays Indefinite so
	// no soft break occurs: the pass measures one tall strip.

	// buildSpace constructs a ConstraintSpace for one measure-pass iteration.
	// Called once per loop iteration with the current break-token.
	buildSpace := func(breakToken *BlockBreakToken) ConstraintSpace {
		b := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{
				InlineSize: colWidth,
				BlockSize:  Indefinite,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: colWidth,
				BlockSize:  containerPercentResolutionBlockSize,
			}).
			SetPercentageResolutionInlineSize(colWidth).
			// CreateConstraintSpaceForBalancing sets it
			// (column_layout_algorithm.cc:1852 @ a9f50e522efa).
			SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
			SetIsContentSuggestionLayout(true)
		// SetShouldPropagateChildBreakValues: column_layout_algorithm.cc:1850
		// @ a9f50e522efa.
		b = b.SetIsInitialColumnBalancingPass(true).
			SetHasBlockFragmentation(true).
			SetShouldPropagateChildBreakValues(true).
			SetFragmentainerBlockSize(Indefinite).
			SetBlockFragmentationType(FragmentColumn).
			SetColumnSpannerPath(mla.spannerPath)
		if breakToken != nil {
			b = b.SetBreakToken(breakToken)
		}
		return b.Build()
	}

	// Measure-pass loop: mirrors Blink's ResolveColumnAutoBlockSizeInternal
	// (column_layout_algorithm.cc:1734-1934). Each iteration measures one
	// "tall strip" with no soft breaks allowed, advancing through the content
	// via break-tokens. One contentRun is recorded per inter-forced-break segment.
	mla.spannerPath = nil
measure:
	var runs []contentRun
	breakToken := nextColToken
	forcedBreaks := 0
	// Blink cla.cc:1629-1633 @ a9f50e522efa: when columns overflow in the
	// inline direction (no wrapping, no outer fragmentation), runs after
	// column-count forced breaks still shape the column block-size;
	// otherwise that content ends up in a later row or outer fragmentainer
	// and is ignored.
	considerAllColumns := mla.columnsOverflowInInlineDirection(false)
	// Phase 16.d.2/3 (v2 B2) accumulator: max unbreakable block-size observed
	// across all measure-pass iterations. Floors the column block-size at line
	// 1639 below so columns are at least as tall as the tallest break-inside:
	// avoid (or eventually monolithic) descendant. Mirrors Blink's
	// tallest_unbreakable_block_size_ in column_layout_algorithm.cc:1879-1948.
	var tallestUnbreakable float64

	const maxIterations = 1000 // safety valve against non-advancing break-token bugs
	for i := 0; i < maxIterations; i++ {
		balancingAlgorithm := NewBlockLayoutAlgorithm(mla.ctx, contentNode, buildSpace(breakToken))
		balancingAlgorithm.SetBoxType(BoxTypeColumn)
		result := balancingAlgorithm.Layout()
		if result == nil {
			break
		}

		if forcedBreaks < numCols || considerAllColumns {
			// The run is the strip's BlockSizeForFragmentation, encompassing
			// the fragment's own block-size (trailing margins). Blink
			// column_layout_algorithm.cc:1651-1672 @ a9f50e522efa.
			colBSize := result.BlockSizeForFragmentation
			if result.Fragment != nil {
				colBSize = math.Max(colBSize, NewLogicalFragment(wdm, result.Fragment).BlockSize())
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
			// Spanner detected during measure pass: the line ends there.
			// Record the path for the column layouts of this line; if
			// forced breaks were taken before the spanner was known, they
			// may have opened parallel flows wrongly, so measure again
			// with the path (cla.cc:1680-1694 @ a9f50e522efa).
			knewAboutSpanner := mla.spannerPath != nil
			mla.spannerPath = result.ColumnSpannerPath
			if forcedBreaks > 0 && !knewAboutSpanner {
				goto measure
			}
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

	// Balancing creates as many columns as column-count specifies;
	// otherwise (an unconstrained column-fill:auto line) the content stays
	// in as few columns as it needs (cla.cc:1726-1729 @ a9f50e522efa).
	if balanceColumns {
		distributeImplicitBreaks(runs, numCols)
	}

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
// outerRemaining (unless Indefinite) is the outer fragmentainer limit; 0 is a
// real limit (cla.cc:1743-1746 @ a9f50e522efa). A regrown line's minimum
// column block-size reaches here through outerRemaining (LayoutLine).
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
	if outerRemaining != Indefinite && size > outerRemaining {
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
			maxSize -= mla.previouslyConsumedContentBlockSize()
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
	return size
}

// CreateConstraintSpaceForSpanner builds the constraint space a column
// spanner is laid out in: a new formatting context whose available and
// percentage-resolution size are both the multicol's content-box size
// (ChildAvailableSize; its block-size is Indefinite when the multicol's is
// auto), with the multicol's block-fragmentation context set up at the
// spanner's offset when the multicol takes part in block fragmentation —
// including an outer initial column-balancing pass, where the fragmentainer
// block-size is unknown — and the spanner is not monolithic. blockOffset is
// the spanner's block offset from the multicol's border-box block-start edge
// (Blink's block_offset). breakToken resumes a spanner that broke inside;
// louis14 carries it on the space where Blink passes it to
// spanner_node.Layout. Mirrors
// ColumnLayoutAlgorithm::CreateConstraintSpaceForSpanner
// (column_layout_algorithm.cc:1865-1893 @ a9f50e522efa) and
// SetupSpaceBuilderForFragmentation(container_builder_, ...)
// (fragmentation_utils.cc:379-397).
//
// An orthogonal spanner gets the orthogonal fallback inline size; a
// parallel one stretches an auto inline-size only when
// ShouldBlockContainerChildStretchAutoInlineSize holds, and is fit-content
// otherwise (cla.cc:1871-1876). louis14's fallback is the ICB size, as for
// block and flex children.
//
// Not ported: the baseline algorithm type and text-box-trim (cla.cc:
// 1880-1886); louis14's ConstraintSpace carries neither.
func (mla *MulticolLayoutAlgorithm) CreateConstraintSpaceForSpanner(spanner *LayoutInputNode, blockOffset float64, breakToken *BlockBreakToken) ConstraintSpace {
	wdm := mla.space.WritingDirection
	spannerWDM := NewWritingDirectionMode(spanner.Style())
	childAvailableSize := LogicalSize{
		InlineSize: mla.contentInlineSize,
		BlockSize:  mla.containerPercentResolutionBlockSize,
	}
	b := NewConstraintSpaceBuilder(wdm, spannerWDM, true)
	if wdm.IsOrthogonalTo(spannerWDM) {
		b.SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(spannerWDM, mla.ctx))
	} else if ShouldBlockContainerChildStretchAutoInlineSize(spanner) {
		b.SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit)
	} else {
		b.SetInlineAutoBehavior(AutoSizeBehaviorFitContent)
	}
	b.SetAvailableSize(childAvailableSize).
		SetPercentageResolutionSize(childAvailableSize).
		SetPercentageResolutionInlineSize(mla.contentInlineSize)
	if breakToken != nil {
		b = b.SetBreakToken(breakToken)
	}
	if mla.space.HasBlockFragmentation && !spanner.IsMonolithic(wdm.WM != spannerWDM.WM) {
		SetupSpaceBuilderForFragmentation(mla.space, mla.fragmentainerOffsetForChildren()+blockOffset,
			mla.fragmentainerCapacityForChildren(), b)
	}
	return b.Build()
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
// The space sets neither IsFixedBlockSize nor a block-size override:
// FinishFragmentationForFragmentainer sizes the column box from the
// fragmentainer block-size (fragmentation_utils.cc:823-866 @ a9f50e522efa).
func (mla *MulticolLayoutAlgorithm) createConstraintSpaceForColumn(
	wdm WritingDirectionMode,
	colWidth float64,
	colBlockSize float64,
	containerPercentResolutionBlockSize float64,
	breakToken *BlockBreakToken,
	balanceColumns bool,
	minBreakAppeal BreakAppeal,
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
		mla.remainingContentBlockSize == Indefinite &&
		!mla.isConstrainedByOuterFragmentation() {
		availBlock = Indefinite
	}

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
		// CreateConstraintSpaceForFragmentainer sets it
		// (fragmentation_utils.cc:1389 @ a9f50e522efa).
		SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
		SetHasBlockFragmentation(true).
		// CreateConstraintSpaceForFragmentainer sets the bit
		// (fragmentation_utils.cc:1391 @ a9f50e522efa).
		SetShouldPropagateChildBreakValues(true).
		SetBlockFragmentationType(FragmentColumn).
		SetIsInsideBalancedColumns(balanceColumns).
		SetMinBreakAppeal(minBreakAppeal)

	// An auto-height row with no content leaves the fragmentainer block-size
	// Indefinite: fragmentation stays on (spanner detection) with no boundary.
	b = b.SetFragmentainerBlockSize(availBlock).
		SetFragmentainerOffset(0)

	if breakToken != nil {
		b = b.SetBreakToken(breakToken)
	}
	b = b.SetColumnSpannerPath(mla.spannerPath)

	return b.Build()
}

// PropagateBaselineFromChild propagates baseline values from a column or
// spanner child result to the multicol container builder.
//
// Mirrors Blink's ColumnLayoutAlgorithm::PropagateBaselineFromChild
// (cla.cc:1655–1677). Called at two sites:
//  1. After each column is committed in LayoutLine.
//  2. After the spanner is committed in the spanner-placement block of Layout.
//
// min semantics for first baseline: the earliest column wins.
// max semantics for last baseline: the latest column wins.
// SetUseLastBaselineForInlineBaseline is called unconditionally (Blink invariant).
func (mla *MulticolLayoutAlgorithm) propagateBaselineFromChild(
	result *LayoutResult, blockOffset float64) {

	if result.HasBaseline {
		cur, has := mla.containerBuilder.Baseline()
		if !has {
			cur = math.MaxFloat64
		}
		bl := math.Min(blockOffset+result.Baseline, cur)
		mla.containerBuilder.SetBaseline(bl)
	}

	if result.LastBaseline > 0 {
		bl := math.Max(blockOffset+result.LastBaseline, mla.containerBuilder.lastBaseline)
		mla.containerBuilder.SetLastBaseline(bl)
	}

	mla.containerBuilder.SetUseLastBaselineForInlineBaseline()
}

// getSpannerFromPath traverses a ColumnSpannerPath linked list and returns
// the Box at the leaf (where Child == nil) — the actual column-span:all
// element, which may be a grandchild or deeper descendant of the multicol
// flow thread. Mirrors Blink's GetSpannerFromPath
// (column_layout_algorithm.cc:224-229 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func getSpannerFromPath(path *ColumnSpannerPath) *LayoutInputNode {
	for path.Child != nil {
		path = path.Child
	}
	return path.Box
}

// setGapGeometryIfNeeded attaches the accumulated gap geometry, if any, to
// the fragment. Blink does this once at the end of Layout(), after the
// fragment is sized (cla.cc:423-428 @ a9f50e522efa); louis14 finishes a
// fragment on two paths (Layout's tail and buildOuterBreakResult), so both
// call this after SetSize. The content ends are Blink's FragmentInlineSize()
// - BorderScrollbarPadding().inline_end and FragmentBlockSize() - the
// applicable block-end border, scrollbar and padding
// (column_gap_accumulator.cc:116-131), made relative to the content box like
// the column offsets (the builder's geometry already carries only the applicable edges).
func (mla *MulticolLayoutAlgorithm) setGapGeometryIfNeeded() {
	if mla.gapAccumulator == nil {
		return
	}
	contentStart := LogicalEdges{
		InlineStart: mla.containerBuilder.InitialFragmentGeometry().Border.InlineStart + mla.containerBuilder.InitialFragmentGeometry().Padding.InlineStart,
		BlockStart:  mla.containerBuilder.InitialFragmentGeometry().Border.BlockStart + mla.containerBuilder.InitialFragmentGeometry().Padding.BlockStart,
	}
	bsp := mla.containerBuilder.InitialFragmentGeometry().BorderBoxPadding()
	contentInlineEnd := mla.containerBuilder.size.InlineSize - bsp.InlineEnd - contentStart.InlineStart
	contentBlockEnd := mla.containerBuilder.size.BlockSize - bsp.BlockEnd - contentStart.BlockStart
	if gg := mla.gapAccumulator.BuildGapGeometry(contentInlineEnd, contentBlockEnd, mla.usedColWidth); gg != nil {
		mla.containerBuilder.SetGapGeometry(gg)
	}
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
	child *PhysicalFragment, childResult *LayoutResult, blockOffset float64) {

	marker := mla.containerBuilder.GetUnpositionedListMarker()
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
	marker.AddToBox(mla.ctx, markerResult, baseline, blockOffset, mla.containerBuilder)
	mla.containerBuilder.ClearUnpositionedListMarker()
}

// positionAnyUnclaimedListMarker places the marker at the container origin
// when no column or spanner provided a baseline to align against.
//
// Mirrors Blink's ColumnLayoutAlgorithm::PositionAnyUnclaimedListMarker
// (cla.cc:383). Called before builder.Build() at the end of Layout().
func (mla *MulticolLayoutAlgorithm) positionAnyUnclaimedListMarker(
	intrinsicBlockSize *float64) {

	marker := mla.containerBuilder.GetUnpositionedListMarker()
	if !marker.IsValid() {
		return
	}
	markerResult := marker.Layout(mla.ctx, mla.space)
	if markerResult == nil {
		return
	}
	marker.AddToBoxWithoutLineBoxes(mla.ctx, markerResult, mla.containerBuilder, intrinsicBlockSize)
	mla.containerBuilder.ClearUnpositionedListMarker()
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
		anonStyle := css.CreateAnonymousStyleWithDisplay(parentStyle, string(css.DisplayBlock))
		wrapper := &LayoutInputNode{
			style:       anonStyle,
			children:    inlineRun,
			isAnonymous: true,
		}
		wrapper.adoptChildren()
		result = append(result, wrapper)
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
