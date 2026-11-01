package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
)

// BoxFragmentBuilder is a mutable accumulator used during layout to build
// a PhysicalFragment. Layout algorithms add children at logical offsets,
// then call Build() to produce an immutable PhysicalFragment with all
// coordinates converted to physical.
//
// Ported from Blink's BoxFragmentBuilder / FragmentBuilder.
type BoxFragmentBuilder struct {
	wdm WritingDirectionMode

	// size is the fragment's border-box size in logical coordinates.
	size LogicalSize

	// children are accumulated during layout at logical offsets.
	children []logicalChildLink

	// boxData stores the physical box model edges.
	boxData *PhysicalBoxData

	// node is the DOM node that produced this fragment.
	node *html.Node

	// style is the computed style for the fragment's node.
	style *css.Style

	// layoutNode is the LayoutInputNode that produced this fragment.
	layoutNode *LayoutInputNode

	// intrinsicBlockSize is the content's natural block-size.
	intrinsicBlockSize float64

	// endMarginStrut for margin collapsing propagation.
	endMarginStrut MarginStrut

	// baseline position (first baseline) — the single first-baseline source.
	// Set by SetBaseline (block/table/flex directly, multicol via
	// PropagateBaselineFromChild) and read back by ancestors as
	// LayoutResult.Baseline. Mirrors Blink's BoxFragmentBuilder::first_baseline_.
	baseline    float64
	hasBaseline bool

	// gapGeometry carries the column-rule/row-rule gap descriptors for multicol
	// containers. Set by SetGapGeometry; forwarded to PhysicalFragment in Build().
	// Mirrors Blink's BoxFragmentBuilder::gap_geometry_ / SetGapGeometry.
	gapGeometry *GapGeometry

	// unpositionedListMarker is the list-item marker awaiting placement by
	// AttemptToPositionListMarker or PositionAnyUnclaimedListMarker.
	// Mirrors Blink's FragmentBuilder::unpositioned_list_marker_.
	unpositionedListMarker *UnpositionedListMarker

	// lastBaseline position (last baseline, for inline-block alignment).
	lastBaseline float64

	// useLastBaselineForInlineBaseline mirrors Blink's
	// BoxFragmentBuilder::use_last_baseline_for_inline_baseline_. Set
	// unconditionally by PropagateBaselineFromChild after any column or spanner
	// child is processed.
	useLastBaselineForInlineBaseline bool

	// exclusionSpace after layout.
	exclusionSpace *ExclusionSpace

	// outOfFlowCandidates collects abs-pos/fixed children for deferred layout.
	outOfFlowCandidates []OutOfFlowCandidate

	// oofPositionedFragmentainerDescendants accumulates deferred OOFs whose
	// CB is itself fragmented across a multicol's outer fragmentainers.
	// Drained at the fragmentation-context root (the outermost multicol).
	// Distinct from outOfFlowCandidates which targets non-fragmented CBs.
	// Phase 25 scaffolding — wiring in subsequent commits. Mirrors Blink's
	// `FragmentBuilder::oof_positioned_fragmentainer_descendants_`
	// (fragment_builder.h:254-260; in Blink this lives on the parent class
	// of BoxFragmentBuilder).
	oofPositionedFragmentainerDescendants []LogicalOofNodeForFragmentation

	// multicolsWithPendingOOFs records inner-multicol nodes nested in this
	// builder that themselves have deferred OOF fragmentainer descendants.
	// Drained at the fragmentation root via
	// `OutOfFlowLayoutPart.HandleMulticolsWithPendingOOFs`. Mirrors Blink's
	// `FragmentBuilder::multicols_with_pending_oofs_` (fragment_builder.h:51-55).
	// Keyed by *LayoutInputNode (louis14's persistent layout-tree identity;
	// where Blink keys by *LayoutBox — see pkg/layout/layout_input_node.go).
	multicolsWithPendingOOFs map[*LayoutInputNode]*MulticolWithPendingOOFs

	// isBlockFragmentationContextRoot marks this builder as a block
	// fragmentation context root (every multicol). Only the outermost one
	// lays out fragmentainer descendants (OutOfFlowLayoutPart.
	// HandleFragmentation returns early for a nested root). Mirrors Blink's
	// `FragmentBuilder::IsBlockFragmentationContextRoot()`
	// (fragment_builder.h:422-428 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	isBlockFragmentationContextRoot bool

	// childAvailableSize is the content-box size of this fragment — the
	// containing block for children's position:relative percentage
	// resolution. Mirrors Blink's BoxFragmentBuilder::child_available_size_.
	// Set by the enclosing layout algorithm via SetChildAvailableSize before
	// adding children; consumed by AddChild to compute a child's relative
	// offset.
	childAvailableSize    LogicalSize
	hasChildAvailableSize bool

	// minimalSpaceShortage tracks the smallest block-size overflow that
	// caused an unforced break in any descendant column during a nested
	// balancing pass. Mirrors Blink's
	// FragmentBuilder::minimal_space_shortage_ (fragment_builder.h). Read
	// into LayoutResult.MinSpaceShortage at Build().
	minimalSpaceShortage    float64
	hasMinimalSpaceShortage bool

	// earlyBreak is the most appealing unforced breakpoint found so far in
	// this fragment, to go back to if layout later runs out of space at a
	// less appealing place. Mirrors Blink's FragmentBuilder::early_break_
	// (fragment_builder.h:641 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	earlyBreak *EarlyBreak

	// blockSizeForFragmentation is the furthest block-end of the in-flow
	// children propagated through PropagateChildBlockSizeForFragmentation,
	// in this builder's content-relative logical coordinates: the extent
	// the content reaches, overflow included, which may exceed the box's
	// own block-size. Build finalizes it (see there). Mirrors Blink's
	// BoxFragmentBuilder::block_size_for_fragmentation_ and its
	// accumulation in PropagateBreakInfo (box_fragment_builder.cc:479-489
	// @ a9f50e522efa):
	//
	//	LayoutUnit block_end_in_container =
	//	    offset.block_offset
	//	    - child_layout_result.AnnotationBlockOffsetAdjustment()
	//	    + BlockSizeForFragmentation(child_layout_result, writing_direction_);
	//	block_size_for_fragmentation_ = std::max(
	//	    block_size_for_fragmentation_, block_end_in_container);
	//
	// louis14 omits the annotation-adjustment term — no ruby/annotation
	// layout exists yet — and the clamped flag (:491-494).
	blockSizeForFragmentation    float64
	hasBlockSizeForFragmentation bool

	// tallestUnbreakableBlockSize tracks the largest unbreakable block-size
	// observed (or propagated up from a child) during the initial column-
	// balancing pass. Read into LayoutResult.TallestUnbreakableBlockSize at
	// Build(). Only meaningful when space.IsInitialColumnBalancingPass.
	// Mirrors Blink's BoxFragmentBuilder::tallest_unbreakable_block_size_
	// + PropagateTallestUnbreakableBlockSize (box_fragment_builder.cc:566-569).
	// Phase 16.d.2/3 (v2 B1+B2): field + accessor in B1; propagation
	// callsites in fragmentation_utils.go + consumer at multicol_layout.go:1601
	// in B2.
	tallestUnbreakableBlockSize    float64
	hasTallestUnbreakableBlockSize bool

	// isMonolithic marks the produced fragment as unbreakable for column-
	// fragmentation purposes — its block-size is fixed and its content does
	// not fragment across column or page boundaries. Read into
	// PhysicalFragment.IsMonolithic at Build(). Phase 16.e+18 v2 B2.5.
	// Mirrors Blink's PhysicalFragment::IsMonolithic.
	isMonolithic bool

	// notFirstForNode is the inverse of Blink's is_first_for_node_
	// (box_fragment_builder.h:790 @ a9f50e522efa), so the zero value means
	// first-for-node. Read into PhysicalFragment at Build().
	notFirstForNode bool

	// initialFragmentGeometry is the fragment geometry the algorithm
	// resolved for this box, read through BorderScrollbarPadding. It points
	// at the algorithm's own geometry, which louis14 clears in place
	// (FragmentGeometry.ClearBorderScrollbarPadding*): the one copy serves
	// as both Blink's initial_fragment_geometry_ and its
	// border_scrollbar_padding_ (box_fragment_builder.h:72-98, :772-774 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so after a clear it holds
	// the edges this fragment carries, not the declared ones.
	initialFragmentGeometry *FragmentGeometry

	// shouldCloneBoxEndDecorations: this fragment repeats its block-end
	// border/padding (box-decoration-break:clone, not known to be the last
	// fragment). Mirrors Blink's
	// BoxFragmentBuilder::should_clone_box_end_decorations_
	// (box_fragment_builder.h:791 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	shouldCloneBoxEndDecorations bool

	// requiresContentBeforeBreaking: no child content added yet, and the
	// node must place some here before it may break (the space requires it,
	// or the node must stay in the current fragmentainer). Mirrors Blink's
	// FragmentBuilder::requires_content_before_breaking_ (fragment_builder.h:
	// 459-468, :677 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	requiresContentBeforeBreaking bool

	// isKnownToFitInFragmentainer: what is left of the node is known, before
	// layout, to fit in the current fragmentainer. Mirrors Blink's
	// BoxFragmentBuilder::is_known_to_fit_in_fragmentainer_
	// (box_fragment_builder.h:787 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	isKnownToFitInFragmentainer bool

	// isBlockSizeForFragmentationClamped: a child that did not fit was kept
	// in this fragmentainer because content was required, so child overflow
	// is ignored for the block-size for fragmentation. Mirrors Blink's
	// BoxFragmentBuilder::is_block_size_for_fragmentation_clamped_
	// (box_fragment_builder.h:397-399, :788 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	isBlockSizeForFragmentationClamped bool

	// previousBreakAfter is the break-after value of the most recently added
	// in-flow child, joined with the value that child propagated
	// (PropagateChildBreakValues). JoinedBreakBetweenValue joins it with the
	// next child's break-before to compute the effective break-between, and
	// Build copies it into LayoutResult.FinalBreakAfter. Mirrors Blink's
	// BoxFragmentBuilder::previous_break_after_ + JoinedBreakBetweenValue.
	previousBreakAfter string

	// initialBreakBefore is the joined break-before value of the first
	// in-flow child added, set once (hasInitialBreakBefore) even when it is
	// auto, so a later child's break-before never replaces it. Copied into
	// LayoutResult.InitialBreakBefore at Build(). Mirrors Blink's
	// BoxFragmentBuilder::initial_break_before_, a std::optional
	// (box_fragment_builder.h:822 @ a9f50e522efa).
	initialBreakBefore    string
	hasInitialBreakBefore bool

	// breakAppeal is the appeal of the break that produced this fragment;
	// BreakAppealPerfect when no break was inserted. Read into
	// LayoutResult.BreakAppeal at Build(). Mirrors Blink's
	// FragmentBuilder::break_appeal_.
	breakAppeal    BreakAppeal
	hasBreakAppeal bool

	// lineCount is the number of non-empty line boxes added to the builder,
	// counted only under block fragmentation (orphans / widows). Mirrors
	// Blink's FragmentBuilder::line_count_ (fragment_builder.h:654-656,
	// incremented in PropagateFromFragment, fragment_builder.cc:466-494 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	lineCount int

	// columnSpannerPath is the path to a column spanner found inside this
	// box; hasColumnSpanner is set with it, or alone when a spanner was
	// found but is not to be laid out yet (a parallel flow broke before it).
	// isEmptySpannerParent marks that nothing in this fragment precedes the
	// spanner. Mirrors Blink's FragmentBuilder::column_spanner_path_,
	// has_column_spanner_ and is_empty_spanner_parent_ (fragment_builder.h:
	// 639, :674-675 @ a9f50e522efa).
	columnSpannerPath    *ColumnSpannerPath
	hasColumnSpanner     bool
	isEmptySpannerParent bool

	// shouldForceSameFragmentationFlow keeps this box's breaks in its
	// parent's flow: it contains a column spanner, so content after it must
	// not be laid out in a parallel flow before the spanner. Mirrors Blink's
	// BoxFragmentBuilder::SetShouldForceSameFragmentationFlow
	// (block_layout_algorithm.cc:820-827 @ a9f50e522efa).
	shouldForceSameFragmentationFlow bool

	// childBreakTokens are the break tokens of the children that broke
	// before or inside, in the order they were added; they become the child
	// break tokens of this fragment's outgoing break token.
	// hasInflowChildBreakInside: one of them broke in this box's own flow
	// (not a float, an out-of-flow box or a child past its block end), so
	// this box breaks inside and lays out no further siblings.
	// hasForcedBreak: a forced break was inserted, here or inside a child.
	// hasSeenAllChildren: every child has been started or finished. Mirrors
	// Blink's FragmentBuilder::child_break_tokens_ (fragment_builder.h:628)
	// and BoxFragmentBuilder::has_inflow_child_break_inside_,
	// has_forced_break_ and has_seen_all_children_ (box_fragment_builder.h:
	// 794-796 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	childBreakTokens          []*BlockBreakToken
	hasInflowChildBreakInside bool
	hasForcedBreak            bool
	hasSeenAllChildren        bool

	// space is the constraint space the fragment is laid out in, when the
	// algorithm sets it: BlockSizeForFragmentation is accumulated and
	// recorded only when it has block fragmentation. Mirrors Blink's
	// FragmentBuilder::space_ (GetConstraintSpace).
	space *ConstraintSpace

	// boxType is the type of box the fragment is (BoxTypeColumn for a
	// fragmentainer), copied to PhysicalFragment.BoxType at Build. Mirrors
	// Blink's FragmentBuilder::box_type_ (SetBoxType).
	boxType BoxType
}

// SetConstraintSpace records the space the fragment is laid out in. Mirrors
// the space Blink's FragmentBuilder is constructed with.
func (b *BoxFragmentBuilder) SetConstraintSpace(space *ConstraintSpace) {
	b.space = space
}

// GetBoxType is the fragment's box type: the one set, else computed from
// the node. Mirrors Blink's FragmentBuilder::GetBoxType
// (fragment_builder.cc:81-96 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// only the rendered-legend inference is ported.
func (b *BoxFragmentBuilder) GetBoxType() BoxType {
	if b.boxType != BoxTypeNormal {
		return b.boxType
	}
	if b.layoutNode.IsRenderedLegend() {
		return BoxTypeRenderedLegend
	}
	return BoxTypeNormal
}

// SetBoxType sets the type of box the fragment is. Mirrors Blink's
// FragmentBuilder::SetBoxType.
func (b *BoxFragmentBuilder) SetBoxType(t BoxType) {
	b.boxType = t
}

type logicalChildLink struct {
	offset   LogicalOffset
	fragment *PhysicalFragment
}

// NewBoxFragmentBuilder creates a builder for the given writing direction.
func NewBoxFragmentBuilder(wdm WritingDirectionMode) *BoxFragmentBuilder {
	return &BoxFragmentBuilder{wdm: wdm}
}

// SetInlineSize sets the fragment's inline-size.
func (b *BoxFragmentBuilder) SetInlineSize(v float64) {
	b.size.InlineSize = v
}

// SetBlockSize sets the fragment's block-size.
func (b *BoxFragmentBuilder) SetBlockSize(v float64) {
	b.size.BlockSize = v
}

// SetSize sets both inline and block size.
func (b *BoxFragmentBuilder) SetSize(size LogicalSize) {
	b.size = size
}

// SetBoxData sets the box model edges (margins, borders, padding).
func (b *BoxFragmentBuilder) SetBoxData(data *PhysicalBoxData) {
	b.boxData = data
}

// SetIntrinsicBlockSize sets the intrinsic (pre-constraint) block-size.
func (b *BoxFragmentBuilder) SetIntrinsicBlockSize(v float64) {
	b.intrinsicBlockSize = v
}

// SetEndMarginStrut sets the margin strut at the block-end.
func (b *BoxFragmentBuilder) SetEndMarginStrut(ms MarginStrut) {
	b.endMarginStrut = ms
}

// SetBaseline sets the first baseline position. This is the single
// first-baseline source ancestors read back via LayoutResult.Baseline
// (block, table, flex, and — via PropagateBaselineFromChild — multicol).
// Mirrors Blink's BoxFragmentBuilder::SetFirstBaseline writing first_baseline_.
func (b *BoxFragmentBuilder) SetBaseline(v float64) {
	b.baseline = v
	b.hasBaseline = true
}

// Baseline returns the current first-baseline value and whether it has been
// set. Used by PropagateBaselineFromChild to apply min semantics across
// columns. Mirrors reading Blink's BoxFragmentBuilder::first_baseline_.
func (b *BoxFragmentBuilder) Baseline() (float64, bool) {
	return b.baseline, b.hasBaseline
}

// SetLastBaseline sets the last baseline position.
func (b *BoxFragmentBuilder) SetLastBaseline(v float64) {
	b.lastBaseline = v
}

// SetUseLastBaselineForInlineBaseline records that this builder's last baseline
// should be used for inline-baseline alignment, as opposed to the first.
// Called unconditionally by PropagateBaselineFromChild. Mirrors Blink's
// BoxFragmentBuilder::SetUseLastBaselineForInlineBaseline().
func (b *BoxFragmentBuilder) SetUseLastBaselineForInlineBaseline() {
	b.useLastBaselineForInlineBaseline = true
}

// SetGapGeometry attaches the column-rule geometry descriptor to this builder.
// Mirrors Blink's BoxFragmentBuilder::SetGapGeometry.
func (b *BoxFragmentBuilder) SetGapGeometry(gg *GapGeometry) {
	b.gapGeometry = gg
}

// SetUnpositionedListMarker records a list-item marker awaiting placement.
// Mirrors Blink's FragmentBuilder::SetUnpositionedListMarker.
func (b *BoxFragmentBuilder) SetUnpositionedListMarker(m *UnpositionedListMarker) {
	b.unpositionedListMarker = m
}

// GetUnpositionedListMarker returns the current unplaced marker, or nil.
// Mirrors Blink's FragmentBuilder::GetUnpositionedListMarker.
func (b *BoxFragmentBuilder) GetUnpositionedListMarker() *UnpositionedListMarker {
	return b.unpositionedListMarker
}

// ClearUnpositionedListMarker removes the stored marker after it has been placed.
// Mirrors Blink's FragmentBuilder::ClearUnpositionedListMarker.
func (b *BoxFragmentBuilder) ClearUnpositionedListMarker() {
	b.unpositionedListMarker = nil
}

// SetIsMonolithic marks the produced fragment as unbreakable for column-
// fragmentation purposes. Mirrors Blink's PhysicalFragment::IsMonolithic.
// Phase 16.e+18 v2 B2.5: set when style.HasSizeContainment() (CSS
// Containment 2 §2.6) or when a spanner's content overflows its declared
// height (implicit monolithic for column-balance).
func (b *BoxFragmentBuilder) SetIsMonolithic(v bool) {
	b.isMonolithic = v
}

// SetIsFirstForNode records whether the fragment being built is the first
// one for its node. Mirrors Blink's BoxFragmentBuilder::SetIsFirstForNode
// (box_fragment_builder.h:416 @ a9f50e522efa), called by
// SetupFragmentBuilderForFragmentation when resuming after a break inside
// the node (fragmentation_utils.cc:426-430).
func (b *BoxFragmentBuilder) SetIsFirstForNode(isFirst bool) {
	b.notFirstForNode = !isFirst
}

// SetInitialFragmentGeometry points the builder at the fragment geometry
// its algorithm resolved (see initialFragmentGeometry). Mirrors Blink's
// BoxFragmentBuilder::SetInitialFragmentGeometry (box_fragment_builder.h:
// 72-98 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which the algorithm
// calls before SetupFragmentBuilderForFragmentation.
func (b *BoxFragmentBuilder) SetInitialFragmentGeometry(geom *FragmentGeometry) {
	b.initialFragmentGeometry = geom
}

// InitialFragmentGeometry returns the geometry set by
// SetInitialFragmentGeometry, with the edges this fragment carries. Mirrors
// Blink's BoxFragmentBuilder::InitialFragmentGeometry
// (box_fragment_builder.h:100-103 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) InitialFragmentGeometry() *FragmentGeometry {
	return b.initialFragmentGeometry
}

// BorderScrollbarPadding is the border + scrollbar + padding this fragment
// carries: a block edge it does not carry has been cleared. Zero when no
// geometry was set. Mirrors Blink's BoxFragmentBuilder::
// BorderScrollbarPadding (box_fragment_builder.h:298-301 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) BorderScrollbarPadding() LogicalEdges {
	if b.initialFragmentGeometry == nil {
		return LogicalEdges{}
	}
	return b.initialFragmentGeometry.BorderBoxPadding()
}

// ClearBorderScrollbarPaddingBlockStart drops the block-start edge, for a
// fragment that does not carry it. Mirrors Blink's BoxFragmentBuilder::
// ClearBorderScrollbarPaddingBlockStart (box_fragment_builder.h:307-309 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) ClearBorderScrollbarPaddingBlockStart() {
	b.initialFragmentGeometry.ClearBorderScrollbarPaddingBlockStart()
}

// ClearBorderScrollbarPaddingBlockEnd drops the block-end edge, for a
// fragment that does not carry it. Mirrors Blink's BoxFragmentBuilder::
// ClearBorderScrollbarPaddingBlockEnd (box_fragment_builder.h:310-312 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) ClearBorderScrollbarPaddingBlockEnd() {
	b.initialFragmentGeometry.ClearBorderScrollbarPaddingBlockEnd()
}

// SetRequiresContentBeforeBreaking sets whether some child content must be
// placed before this node may break. Mirrors Blink's
// FragmentBuilder::SetRequiresContentBeforeBreaking (fragment_builder.h:
// 463-465 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetRequiresContentBeforeBreaking(v bool) {
	b.requiresContentBeforeBreaking = v
}

// RequiresContentBeforeBreaking mirrors Blink's
// FragmentBuilder::RequiresContentBeforeBreaking (fragment_builder.h:466-468
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) RequiresContentBeforeBreaking() bool {
	return b.requiresContentBeforeBreaking
}

// SetIsKnownToFitInFragmentainer records whether the rest of the node is
// known to fit in the current fragmentainer. Mirrors BoxFragmentBuilder::
// SetIsKnownToFitInFragmentainer (box_fragment_builder.h:390-392 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetIsKnownToFitInFragmentainer(v bool) {
	b.isKnownToFitInFragmentainer = v
}

// IsKnownToFitInFragmentainer mirrors BoxFragmentBuilder::
// IsKnownToFitInFragmentainer (box_fragment_builder.h:393-395).
func (b *BoxFragmentBuilder) IsKnownToFitInFragmentainer() bool { return b.isKnownToFitInFragmentainer }

// MustStayInCurrentFragmentainer reports whether the node, known to fit,
// must keep its first fragment (and its first piece of content) in the
// current fragmentainer. Mirrors BoxFragmentBuilder::
// MustStayInCurrentFragmentainer (box_fragment_builder.h:401-413 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) MustStayInCurrentFragmentainer() bool {
	return b.isKnownToFitInFragmentainer && !b.notFirstForNode
}

// SetIsBlockSizeForFragmentationClamped records that a child was forced to
// stay in this fragmentainer although it did not fit. Mirrors Blink's
// BoxFragmentBuilder::SetIsBlockSizeForFragmentationClamped
// (box_fragment_builder.h:397-399 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetIsBlockSizeForFragmentationClamped() {
	b.isBlockSizeForFragmentationClamped = true
}

// ShouldCloneBoxEndDecorations reports whether this fragment repeats its
// block-end border/padding. Mirrors Blink's BoxFragmentBuilder::
// ShouldCloneBoxEndDecorations (box_fragment_builder.h:418-420 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) ShouldCloneBoxEndDecorations() bool {
	return b.shouldCloneBoxEndDecorations
}

// SetShouldCloneBoxEndDecorations is set by the algorithm's
// SetupFragmentBuilderForFragmentation port. Mirrors Blink's
// BoxFragmentBuilder::SetShouldCloneBoxEndDecorations
// (box_fragment_builder.h:421-423 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetShouldCloneBoxEndDecorations(v bool) {
	b.shouldCloneBoxEndDecorations = v
}

// Style is the computed style of the fragment's node (nil if none was
// set). Mirrors Blink's FragmentBuilder::Style (fragment_builder.h:57-60 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) Style() *css.Style {
	return b.style
}

// GetConstraintSpace is the space the fragment is laid out in (nil if the
// algorithm set none). Mirrors Blink's FragmentBuilder::GetConstraintSpace
// (fragment_builder.h:65 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) GetConstraintSpace() *ConstraintSpace {
	return b.space
}

// PreviousBreakToken is the incoming break token of the fragment being
// built. Blink hands it to the builder separately
// (BoxFragmentBuilder::PreviousBreakToken, box_fragment_builder.h:151-153 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14's constraint space
// carries it, so it is read from there (nil without a space).
func (b *BoxFragmentBuilder) PreviousBreakToken() *BlockBreakToken {
	if b.space == nil {
		return nil
	}
	return b.space.BreakToken
}

// SetExclusionSpace sets the updated float exclusion state.
func (b *BoxFragmentBuilder) SetExclusionSpace(es *ExclusionSpace) {
	b.exclusionSpace = es
}

// GetExclusionSpace returns the float exclusion state last set. Mirrors
// Blink's BoxFragmentBuilder::GetExclusionSpace (box_fragment_builder.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) GetExclusionSpace() *ExclusionSpace {
	return b.exclusionSpace
}

// SetNode sets the DOM node that produced this fragment.
func (b *BoxFragmentBuilder) SetNode(node *html.Node) {
	b.node = node
}

// SetStyle sets the computed style for this fragment.
func (b *BoxFragmentBuilder) SetStyle(style *css.Style) {
	b.style = style
}

// SetLayoutNode sets both the DOM node and style from a LayoutInputNode,
// and stores the LayoutInputNode itself for fragment→LayoutInputNode bridging.
func (b *BoxFragmentBuilder) SetLayoutNode(lin *LayoutInputNode) {
	b.node = lin.DOMNode
	b.style = lin.Style()
	b.layoutNode = lin
}

// AddOutOfFlowCandidate records an absolutely/fixed positioned child
// for deferred layout by OutOfFlowLayoutPart.
// PropagateSpaceShortage records a minimum space shortage: a line or child
// that did not fit, or an inner nested multicol whose balancing pass gave
// up. The outer balancing
// loop consumes LayoutResult.MinSpaceShortage to decide its next stretch.
// Mirrors Blink's FragmentBuilder::PropagateSpaceShortage (cla.cc:1241).
func (b *BoxFragmentBuilder) PropagateSpaceShortage(shortage float64) {
	if shortage <= 0 {
		return
	}
	if !b.hasMinimalSpaceShortage || shortage < b.minimalSpaceShortage {
		b.minimalSpaceShortage = shortage
		b.hasMinimalSpaceShortage = true
	}
}

// PropagateTallestUnbreakableBlockSize records an unbreakable block-size
// observed during the initial column-balancing pass and keeps the maximum
// across multiple calls. Used by the column-layout algorithm to floor the
// auto column block-size at the tallest piece of monolithic content or
// break-inside:avoid block in any descendant — so columns are tall enough
// to hold the largest unbreakable child without visual overflow.
//
// Mirrors Blink's BoxFragmentBuilder::PropagateTallestUnbreakableBlockSize
// (box_fragment_builder.cc:566-569). Callers (in v2 B2): the fragmentation
// hook in BreakBeforeChildIfNeeded for break-inside:avoid children
// (fragmentation_utils.cc:1105-1113), the SetupFragmentation border/padding
// contribution (fragmentation_utils.cc:510-514), and the parent-side
// child-result propagation (box_fragment_builder.cc:566-569).
//
// Negative or zero values are ignored to keep callsites loose; only the
// non-trivial floors are kept. Mirrors Blink's std::max idiom.
func (b *BoxFragmentBuilder) PropagateTallestUnbreakableBlockSize(blockSize float64) {
	if blockSize <= 0 {
		return
	}
	if !b.hasTallestUnbreakableBlockSize || blockSize > b.tallestUnbreakableBlockSize {
		b.tallestUnbreakableBlockSize = blockSize
		b.hasTallestUnbreakableBlockSize = true
	}
}

// TallestUnbreakableBlockSize returns the current accumulated unbreakable
// block-size (or 0 if none has been propagated). Used by callers that
// need to forward the value to an outer fragmentation context (e.g.,
// nested multicol propagating its own tallest unbreakable up to the
// outer multicol's initial balancing pass; mirrors cla.cc:1879-1948).
func (b *BoxFragmentBuilder) TallestUnbreakableBlockSize() float64 {
	if !b.hasTallestUnbreakableBlockSize {
		return 0
	}
	return b.tallestUnbreakableBlockSize
}

// PropagateChildBlockSizeForFragmentation max-merges an in-flow child's
// block-end for fragmentation — offset + BlockSizeForFragmentation(child) —
// into this builder's running value, when the fragment is laid out with
// block fragmentation. Mirrors the BlockSizeForFragmentation part of Blink's
// BoxFragmentBuilder::PropagateBreakInfo (box_fragment_builder.cc:479-489 @
// a9f50e522efa), which AddResult runs only under block fragmentation
// (:227-229). No-op if the child is nil or carries no fragment.
func (b *BoxFragmentBuilder) PropagateChildBlockSizeForFragmentation(
	child *LayoutResult, offset LogicalOffset,
) {
	if b.space == nil || !b.space.HasBlockFragmentation || child == nil || child.Fragment == nil {
		return
	}
	blockEnd := offset.BlockOffset + BlockSizeForFragmentation(b.wdm, child)
	if !b.hasBlockSizeForFragmentation || blockEnd > b.blockSizeForFragmentation {
		b.blockSizeForFragmentation = blockEnd
		b.hasBlockSizeForFragmentation = true
	}
	// A child clamped while content was required here clamps this node too
	// (box_fragment_builder.cc:493-496 @ a9f50e522efa).
	if b.space.RequiresContentBeforeBreaking && child.IsBlockSizeForFragmentationClamped {
		b.isBlockSizeForFragmentationClamped = true
	}
}

// SetBlockSizeForFragmentation overwrites the builder's running BSFF with
// the given value (clamped non-negative). Mirrors Blink's
// BoxFragmentBuilder::SetBlockSizeForFragmentation (box_fragment_builder.h).
func (b *BoxFragmentBuilder) SetBlockSizeForFragmentation(v float64) {
	if v < 0 {
		v = 0
	}
	b.blockSizeForFragmentation = v
	b.hasBlockSizeForFragmentation = true
}

// BlockSizeForFragmentation returns the current accumulated BSFF (content
// relative, before Build's finalization) and whether any was recorded.
func (b *BoxFragmentBuilder) BlockSizeForFragmentation() (float64, bool) {
	return b.blockSizeForFragmentation, b.hasBlockSizeForFragmentation
}

// SetInitialBreakBefore records the break-before value to propagate to the
// parent. Mirrors Blink's BoxFragmentBuilder::SetInitialBreakBefore
// (box_fragment_builder.h:514-516 @ a9f50e522efa).
func (b *BoxFragmentBuilder) SetInitialBreakBefore(v string) {
	b.initialBreakBefore = v
	b.hasInitialBreakBefore = true
}

// SetInitialBreakBeforeIfNeeded records v only when no value was recorded
// yet, so the first in-flow child wins even when its value is auto. Mirrors
// Blink's BoxFragmentBuilder::SetInitialBreakBeforeIfNeeded
// (box_fragment_builder.h:518-521 @ a9f50e522efa).
func (b *BoxFragmentBuilder) SetInitialBreakBeforeIfNeeded(v string) {
	if !b.hasInitialBreakBefore {
		b.SetInitialBreakBefore(v)
	}
}

// PreviousBreakAfter returns the break-after value recorded for the most
// recently added in-flow child ("auto" when none). Mirrors Blink's
// BoxFragmentBuilder::PreviousBreakAfter (box_fragment_builder.h:523 @
// a9f50e522efa).
func (b *BoxFragmentBuilder) PreviousBreakAfter() string {
	if b.previousBreakAfter == "" {
		return "auto"
	}
	return b.previousBreakAfter
}

// PropagateChildBreakValues propagates an in-flow child's break values: the
// first child's break-before (joined with the value it propagated) becomes
// this box's initial break-before, and every child's break-after (joined
// with the value it propagated) overwrites the previous break-after, so the
// last child's becomes this box's final break-after. Inline, column-box,
// floating and out-of-flow fragments do not propagate. Callers gate it on
// ConstraintSpace.ShouldPropagateChildBreakValues. Mirrors Blink's
// BoxFragmentBuilder::PropagateChildBreakValues (box_fragment_builder.cc:
// 618-657 @ a9f50e522efa; exclusions :627-630); page names are not ported
// (louis14 does not paginate).
func (b *BoxFragmentBuilder) PropagateChildBreakValues(childResult *LayoutResult) {
	if childResult == nil {
		return
	}
	frag := childResult.Fragment
	if frag == nil || frag.Type != FragmentBox || frag.IsColumnBox() || isOutOfFlowFragment(frag) {
		return
	}
	breakBefore, breakAfter := "auto", "auto"
	if s := frag.Style; s != nil {
		if s.GetFloat() != css.FloatNone {
			return
		}
		breakBefore, breakAfter = s.GetBreakBefore(), s.GetBreakAfter()
	}
	b.SetInitialBreakBeforeIfNeeded(JoinFragmentainerBreakValues(childResult.InitialBreakBefore, breakBefore))
	b.SetPreviousBreakAfter(JoinFragmentainerBreakValues(childResult.FinalBreakAfter, breakAfter))
}

// AddResult adds a child's fragment at offset (shifted by relativeOffset)
// and takes over what its layout result carries for this box: under block
// fragmentation its break token and break info (PropagateBreakInfo), and,
// when the space asks for it, its break values. Mirrors Blink's
// BoxFragmentBuilder::AddResult (box_fragment_builder.cc:180-233 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the child's break token is
// collected as AddChild → PropagateFromFragment does (fragment_builder.cc:
// 455-464). louis14 resolves margins and the relative offset at the call
// site and has no block-in-inline line result to unwrap (:189-210).
func (b *BoxFragmentBuilder) AddResult(child *LayoutInputNode, childResult *LayoutResult, offset, relativeOffset LogicalOffset) {
	b.AddChildWithRelativeOffset(childResult.Fragment, offset, relativeOffset)
	if b.space == nil {
		return
	}
	if b.space.HasBlockFragmentation {
		if token := childResult.BreakToken; token != nil && !childResult.Fragment.IsFragmentainerBox() {
			b.childBreakTokens = append(b.childBreakTokens, token)
		}
		b.PropagateBreakInfo(child, childResult, offset)
	}
	if b.space.ShouldPropagateChildBreakValues() {
		b.PropagateChildBreakValues(childResult)
	}
}

// PropagateBreakInfo takes over the fragmentation info of a child laid out
// at offset: its block-end for fragmentation; a break inside it, which in
// this box's own flow means this box breaks inside, and lowers the appeal
// of breaking inside this box to that of breaking inside the child; its
// tallest unbreakable block-size in the initial column balancing pass; a
// forced break inside it, or else its space shortage; and a column spanner
// found inside it, whose path this box wraps so that the multicol reaches
// the spanner through it. Mirrors Blink's BoxFragmentBuilder::
// PropagateBreakInfo (box_fragment_builder.cc:479-617 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Not ported: the annotation
// (ruby) block-offset adjustment (:486-487), paginated monolithic overflow
// (:512-564, louis14 does not paginate) and the out-of-flow-in-fragmentainer
// subtree bit (:610-615). IsInColumnBfc has no louis14 flag; a column
// fragmentation space stands in. A louis14 spanner path omits Blink's head
// Node(): a result's path starts at the child, so {Box: child, Child: path}
// is Blink's ColumnSpannerPath(Node(), child_path), and the space-side
// follow checks path.Box == child.
func (b *BoxFragmentBuilder) PropagateBreakInfo(child *LayoutInputNode, childResult *LayoutResult, offset LogicalOffset) {
	if childResult == nil || childResult.Fragment == nil {
		return
	}
	space := b.space
	b.PropagateChildBlockSizeForFragmentation(childResult, offset)

	token := childResult.BreakToken
	// A break inside an out-of-flow box, a float, or a child past its
	// block end (a parallel flow) is not in this box's flow — unless the
	// child holds a column spanner, which forces its breaks into this flow
	// (:504-510).
	isInSameFlow := ((!token.IsBreakInside() || !token.IsAtBlockEnd) &&
		!child.IsFloatingOrOutOfFlowPositioned()) ||
		childResult.ShouldForceSameFragmentationFlow
	if token.IsBreakInside() {
		if isInSameFlow {
			b.hasInflowChildBreakInside = true
		}
		b.ClampBreakAppeal(CalculateBreakAppealInside(*space, childResult))
	}
	if space.IsInitialColumnBalancingPass {
		b.PropagateTallestUnbreakableBlockSize(childResult.TallestUnbreakableBlockSize)
	}
	if childResult.HasForcedBreak {
		b.SetHasForcedBreak()
	} else if !space.IsInitialColumnBalancingPass {
		b.PropagateSpaceShortage(childResult.MinSpaceShortage)
	}

	if childResult.Fragment.Type != FragmentBox {
		return
	}
	if space.BlockFragmentationType == FragmentColumn && childResult.ColumnSpannerPath != nil {
		b.SetColumnSpannerPath(&ColumnSpannerPath{Box: child, Child: childResult.ColumnSpannerPath})
		b.SetIsEmptySpannerParent(childResult.IsEmptySpannerParent)
	}
}

// AddBreakBeforeChild breaks before child: its break-before token joins the
// child break tokens, and unless child is a float or an out-of-flow box,
// this box breaks inside in its own flow. A forced break sets the
// forced-break flag; otherwise appeal, when given, lowers the break appeal.
// Mirrors Blink's BoxFragmentBuilder::AddBreakBeforeChild
// (box_fragment_builder.cc:121-178 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the inline-node branch
// (:142-175), which keeps the last inline break token, is not ported: a
// louis14 line break hands its own token to AddBreakToken.
func (b *BoxFragmentBuilder) AddBreakBeforeChild(child *LayoutInputNode, appeal *BreakAppeal, isForcedBreak bool) {
	if isForcedBreak {
		b.SetHasForcedBreak()
	} else if appeal != nil {
		b.ClampBreakAppeal(*appeal)
	}
	if !b.hasInflowChildBreakInside {
		b.hasInflowChildBreakInside = !child.IsFloatingOrOutOfFlowPositioned()
	}
	b.childBreakTokens = append(b.childBreakTokens, CreateBreakBefore(child, isForcedBreak))
}

// AddBreakToken adds a child break token that a child's layout result does
// not carry (a line break, a float resumed in a parallel flow). Unless it
// is in a parallel flow, this box breaks inside. Mirrors Blink's
// BoxFragmentBuilder::AddBreakToken (box_fragment_builder.cc:381-389 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) AddBreakToken(token *BlockBreakToken, isInParallelFlow bool) {
	b.childBreakTokens = append(b.childBreakTokens, token)
	b.hasInflowChildBreakInside = b.hasInflowChildBreakInside || !isInParallelFlow
}

// ChildBreakTokens returns the child break tokens added so far.
func (b *BoxFragmentBuilder) ChildBreakTokens() []*BlockBreakToken { return b.childBreakTokens }

// LastChildBreakToken returns the child break token added last. Mirrors
// BoxFragmentBuilder::LastChildBreakToken (box_fragment_builder.h:741-744 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) LastChildBreakToken() *BlockBreakToken {
	return b.childBreakTokens[len(b.childBreakTokens)-1]
}

// HasInsertedChildBreak reports whether a child break was inserted, in
// this box's flow or a parallel one. Mirrors BoxFragmentBuilder::
// HasInsertedChildBreak (box_fragment_builder.h:466-479 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 has no repeated
// content, so every token counts.
func (b *BoxFragmentBuilder) HasInsertedChildBreak() bool { return len(b.childBreakTokens) > 0 }

// ShouldBreakInside reports whether this box needs a break token for its
// children, the way things look at the end of layout. Mirrors
// BoxFragmentBuilder::ShouldBreakInside (box_fragment_builder.h:485-505 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for its HasInsertedChildBreak
// term: a louis14 line break is a child break token (no
// last_inline_break_token_), monolithic overflow is decided by
// FinishFragmentation at the call site, and has_subsequent_children_ (grid,
// fragmentainer OOF layout) is not ported.
func (b *BoxFragmentBuilder) ShouldBreakInside() bool { return b.HasInsertedChildBreak() }

// HasInflowChildBreakInside reports whether a child broke before or inside
// in this box's own flow, so this box breaks inside. Mirrors
// BoxFragmentBuilder::HasInflowChildBreakInside (box_fragment_builder.h:
// 507-512 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) HasInflowChildBreakInside() bool { return b.hasInflowChildBreakInside }

// SetHasForcedBreak records a forced break; a space shortage is meaningless
// past one, so the minimal space shortage is dropped. Mirrors
// BoxFragmentBuilder::SetHasForcedBreak (box_fragment_builder.h:734-737 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetHasForcedBreak() {
	b.hasForcedBreak = true
	b.minimalSpaceShortage = 0
	b.hasMinimalSpaceShortage = false
}

// HasForcedBreak mirrors BoxFragmentBuilder::HasForcedBreak
// (box_fragment_builder.h:739).
func (b *BoxFragmentBuilder) HasForcedBreak() bool { return b.hasForcedBreak }

// SetHasSeenAllChildren records that every child has been started or
// finished, so a continuation lays out none from scratch. Mirrors
// BoxFragmentBuilder::SetHasSeenAllChildren (box_fragment_builder.h:547 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetHasSeenAllChildren() { b.hasSeenAllChildren = true }

// HasSeenAllChildren mirrors BoxFragmentBuilder::HasSeenAllChildren
// (box_fragment_builder.h:548).
func (b *BoxFragmentBuilder) HasSeenAllChildren() bool { return b.hasSeenAllChildren }

// SetPreviousBreakAfter records the break-after value of the most recently
// added in-flow child (PropagateChildBreakValues), or resets it (the
// multicol after a non-empty column line). JoinedBreakBetweenValue uses it to
// compute the effective break-between value when a subsequent child is
// added. Mirrors Blink's BoxFragmentBuilder::SetPreviousBreakAfter
// (box_fragment_builder.h:524 @ a9f50e522efa).
func (b *BoxFragmentBuilder) SetPreviousBreakAfter(v string) {
	b.previousBreakAfter = v
}

// JoinedBreakBetweenValue returns the join of the previous child's break-after
// with the new child's break-before, picking the dominant value. Mirrors
// Blink's BoxFragmentBuilder::JoinedBreakBetweenValue.
func (b *BoxFragmentBuilder) JoinedBreakBetweenValue(childBreakBefore string) string {
	return JoinFragmentainerBreakValues(b.previousBreakAfter, childBreakBefore)
}

// SetBreakAppeal records the appeal of the break that produced this fragment.
// BreakAppealPerfect when no soft break was inserted; lower values when a
// break-avoid value or orphans/widows constraint was violated. Mirrors Blink's
// FragmentBuilder::SetBreakAppeal.
func (b *BoxFragmentBuilder) SetBreakAppeal(appeal BreakAppeal) {
	b.breakAppeal = appeal
	b.hasBreakAppeal = true
}

// SetEarlyBreak mirrors Blink's BoxFragmentBuilder::SetEarlyBreak
// (box_fragment_builder.h:573-575 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetEarlyBreak(breakpoint *EarlyBreak) { b.earlyBreak = breakpoint }

// HasEarlyBreak mirrors BoxFragmentBuilder::HasEarlyBreak
// (box_fragment_builder.h:576).
func (b *BoxFragmentBuilder) HasEarlyBreak() bool { return b.earlyBreak != nil }

// GetEarlyBreak mirrors BoxFragmentBuilder::GetEarlyBreak
// (box_fragment_builder.h:577-580).
func (b *BoxFragmentBuilder) GetEarlyBreak() *EarlyBreak { return b.earlyBreak }

// LineCount mirrors Blink's BoxFragmentBuilder::LineCount
// (box_fragment_builder.h:541 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) LineCount() int { return b.lineCount }

// SetLineCount mirrors Blink's BoxFragmentBuilder::SetLineCount
// (box_fragment_builder.h:542 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetLineCount(lineCount int) { b.lineCount = lineCount }

// ClampBreakAppeal lowers the recorded break appeal to `appeal` when that is
// worse than what is recorded (an unset appeal counts as Perfect). Used to
// fold a child's or a column's sub-perfect appeal into the container's
// result. Mirrors Blink's FragmentBuilder::ClampBreakAppeal.
func (b *BoxFragmentBuilder) ClampBreakAppeal(appeal BreakAppeal) {
	if !b.hasBreakAppeal || appeal < b.breakAppeal {
		b.SetBreakAppeal(appeal)
	}
}

func (b *BoxFragmentBuilder) AddOutOfFlowCandidate(c OutOfFlowCandidate) {
	b.outOfFlowCandidates = append(b.outOfFlowCandidates, c)
}

// AddOutOfFlowFragmentainerDescendant appends an OOF whose CB is itself
// fragmented across a multicol's outer fragmentainers. Drained at the
// fragmentation-context root by `OutOfFlowLayoutPart.HandleFragmentation`.
// Mirrors Blink's `FragmentBuilder::AddOutOfFlowFragmentainerDescendant`
// (fragment_builder.h:373-380; fragment_builder.cc:589-603 for the
// propagating variant).
func (b *BoxFragmentBuilder) AddOutOfFlowFragmentainerDescendant(d LogicalOofNodeForFragmentation) {
	b.oofPositionedFragmentainerDescendants = append(b.oofPositionedFragmentainerDescendants, d)
}

// AdjustFragmentainerDescendant moves a fragmentainer descendant deferred at
// this containing block into stitched-CB coordinates: the containing-block
// offset becomes the offset of the CB's first fragment, and the static
// position is made relative to it. When this box is the descendant's
// fixedpos containing block (a fixed container) its fixedpos
// containing-block offset is moved the same way. onlyFixedposContainingBlock
// adjusts only the latter. previousConsumedBlockSize is the CB's incoming
// break token's (border-box) ConsumedBlockSize. Mirrors Blink's
// BoxFragmentBuilder::AdjustFragmentainerDescendant
// (box_fragment_builder.cc:781-809 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), called from
// OutOfFlowLayoutPart::LayoutCandidates (out_of_flow_layout_part.cc:1190-1200).
//
// louis14 static positions are content-box relative, Blink's border-box
// relative (plan RC-8), so the static position takes the content block-size
// the CB's earlier fragments consumed (previousConsumedContentBlockSize)
// while the containing-block offsets take the border-box one.
func (b *BoxFragmentBuilder) AdjustFragmentainerDescendant(d *LogicalOofNodeForFragmentation, onlyFixedposContainingBlock bool, previousConsumedBlockSize, previousConsumedContentBlockSize float64) {
	if !onlyFixedposContainingBlock && d.ContainingBlock.Fragment == nil {
		d.ContainingBlock.Offset.BlockOffset -= previousConsumedBlockSize
		d.Candidate.StaticPosition.Offset.BlockOffset += previousConsumedContentBlockSize
	}
	if d.FixedposContainingBlock.Fragment == nil &&
		(b.isFixedContainer() || d.FixedposInlineContainer.Container != nil) {
		d.FixedposContainingBlock.Offset.BlockOffset -= previousConsumedBlockSize
	}
}

// AdjustFixedposContainingBlockForFragmentainerDescendants applies the
// fixedpos half of AdjustFragmentainerDescendant to every fragmentainer
// descendant collected here. Mirrors Blink's
// BoxFragmentBuilder::AdjustFixedposContainingBlockForFragmentainerDescendants
// (box_fragment_builder.cc:811-821 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) AdjustFixedposContainingBlockForFragmentainerDescendants(previousConsumedBlockSize float64) {
	for i := range b.oofPositionedFragmentainerDescendants {
		b.AdjustFragmentainerDescendant(&b.oofPositionedFragmentainerDescendants[i], true, previousConsumedBlockSize, 0)
	}
}

// AdjustFixedposContainingBlockForInnerMulticols makes the pending inner
// multicols whose fixedpos containing block is this box (a fixed container,
// its fragment not recorded yet) relative to its first fragment: the
// fixedpos containing-block offset loses, and the multicol offset gains, the
// block-size the box's earlier fragments consumed. Mirrors Blink's
// BoxFragmentBuilder::AdjustFixedposContainingBlockForInnerMulticols
// (box_fragment_builder.cc:823-845 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the multicol offset is
// content-box relative in louis14, so it gains the consumed content
// block-size.
func (b *BoxFragmentBuilder) AdjustFixedposContainingBlockForInnerMulticols(previousConsumedBlockSize, previousConsumedContentBlockSize float64) {
	if previousConsumedBlockSize == 0 {
		return
	}
	for _, multicol := range b.multicolsWithPendingOOFs {
		if multicol.FixedposContainingBlock.Fragment == nil &&
			(b.isFixedContainer() || multicol.FixedposInlineContainer.Container != nil) {
			multicol.FixedposContainingBlock.Offset.BlockOffset -= previousConsumedBlockSize
			multicol.MulticolOffset.BlockOffset += previousConsumedContentBlockSize
		}
	}
}

// isFixedContainer reports whether the box this builder lays out is a fixed
// container (Blink's node_.IsFixedContainer()).
func (b *BoxFragmentBuilder) isFixedContainer() bool {
	return b.layoutNode != nil && b.layoutNode.IsFixedContainer()
}

// AdjustFixedposContainerInfo records boxFragment, a child being propagated
// into this builder, as the fixedpos containing block of an OOF (or pending
// multicol) that has none yet when it can contain fixed-position boxes.
// Mirrors Blink's FragmentBuilder::AdjustFixedposContainerInfo
// (fragment_builder.cc:1108-1143 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for box fragments; louis14
// inline boxes are not fragments that carry OOF data, so the
// inline-container branches are not ported.
func (b *BoxFragmentBuilder) AdjustFixedposContainerInfo(boxFragment *PhysicalFragment, fixedposContainingBlockFragment **PhysicalFragment) {
	if boxFragment == nil || *fixedposContainingBlockFragment != nil || boxFragment.LayoutNode == nil {
		return
	}
	if boxFragment.LayoutNode.IsFixedContainer() {
		*fixedposContainingBlockFragment = boxFragment
	}
}

// AddMulticolWithPendingOOFs records an inner-multicol descendant whose own
// OOF fragmentainer descendants haven't been laid out yet. The outer
// fragmentation root drains the entry in
// `OutOfFlowLayoutPart.HandleMulticolsWithPendingOOFs`.
//
// Idempotent: first-write-wins. Mirrors Blink's
// `FragmentBuilder::AddMulticolWithPendingOOFs` early-return on duplicate-key
// insert (fragment_builder.cc:651-652). Lazy-initializes the map on first call.
func (b *BoxFragmentBuilder) AddMulticolWithPendingOOFs(node *LayoutInputNode, info *MulticolWithPendingOOFs) {
	if b.multicolsWithPendingOOFs == nil {
		b.multicolsWithPendingOOFs = make(map[*LayoutInputNode]*MulticolWithPendingOOFs)
	}
	if _, exists := b.multicolsWithPendingOOFs[node]; exists {
		return
	}
	b.multicolsWithPendingOOFs[node] = info
}

// TransferOutOfFlowCandidates moves this builder's out-of-flow candidates to
// destination, adding additionalOffset to their static positions. multicol,
// when set, is the inner multicol the candidates surfaced in: a fixed
// candidate whose fixedpos containing block was found inside it becomes a
// fragmentainer descendant of destination instead. Mirrors Blink's
// FragmentBuilder::TransferOutOfFlowCandidates (fragment_builder.cc:682-710
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The pending multicol's
// fixedpos containing block is recorded while it propagates
// (AdjustFixedposContainerInfo), its multicol offset then relative to it.
func (b *BoxFragmentBuilder) TransferOutOfFlowCandidates(destination *BoxFragmentBuilder, additionalOffset LogicalOffset, multicol *MulticolWithPendingOOFs) {
	for _, candidate := range b.outOfFlowCandidates {
		candidate.StaticPosition.Offset.InlineOffset += additionalOffset.InlineOffset
		candidate.StaticPosition.Offset.BlockOffset += additionalOffset.BlockOffset
		if multicol != nil && multicol.FixedposContainingBlock.Fragment != nil && candidate.IsFixedPosition {
			destination.AddOutOfFlowFragmentainerDescendant(LogicalOofNodeForFragmentation{
				Candidate:               candidate,
				ContainingBlock:         multicol.FixedposContainingBlock,
				FixedposContainingBlock: multicol.FixedposContainingBlock,
				FixedposInlineContainer: multicol.FixedposInlineContainer,
			})
			continue
		}
		destination.AddOutOfFlowCandidate(candidate)
	}
	b.outOfFlowCandidates = nil
}

// NeedsOutOfFlowLayoutPart reports whether the container this builder lays
// out has out-of-flow work for its OutOfFlowLayoutPart: candidates,
// fragmentainer descendants or inner multicols with pending OOFs (the
// condition of the early return in Blink's OutOfFlowLayoutPart constructor,
// out_of_flow_layout_part.cc:551-556 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) NeedsOutOfFlowLayoutPart() bool {
	return len(b.outOfFlowCandidates) > 0 || b.HasOutOfFlowFragmentainerDescendants() || b.HasMulticolsWithPendingOOFs()
}

// HasOutOfFlowFragmentainerDescendants reports whether any deferred OOFs are
// queued. Mirrors Blink's `FragmentBuilder::HasOutOfFlowFragmentainerDescendants`.
func (b *BoxFragmentBuilder) HasOutOfFlowFragmentainerDescendants() bool {
	return len(b.oofPositionedFragmentainerDescendants) > 0
}

// HasMulticolsWithPendingOOFs reports whether any inner-multicol entries are
// pending drain. Mirrors Blink's
// `FragmentBuilder::HasMulticolsWithPendingOOFs`.
func (b *BoxFragmentBuilder) HasMulticolsWithPendingOOFs() bool {
	return len(b.multicolsWithPendingOOFs) > 0
}

// SwapOutOfFlowFragmentainerDescendants exchanges the builder's deferred-OOF
// list with the caller's `out` slice. Caller ends up holding what the builder
// had; builder's slice becomes whatever the caller passed in (typically a nil
// slice that resets the builder). Mirrors Blink's
// `FragmentBuilder::SwapOutOfFlowFragmentainerDescendants`.
func (b *BoxFragmentBuilder) SwapOutOfFlowFragmentainerDescendants(out *[]LogicalOofNodeForFragmentation) {
	*out, b.oofPositionedFragmentainerDescendants = b.oofPositionedFragmentainerDescendants, *out
}

// SwapMulticolsWithPendingOOFs exchanges the builder's pending-multicol map
// with the caller's `out` map. Caller ends up holding what the builder had;
// builder's map becomes whatever the caller passed in. Mirrors Blink's
// `FragmentBuilder::SwapMulticolsWithPendingOOFs`.
func (b *BoxFragmentBuilder) SwapMulticolsWithPendingOOFs(out *map[*LayoutInputNode]*MulticolWithPendingOOFs) {
	*out, b.multicolsWithPendingOOFs = b.multicolsWithPendingOOFs, *out
}

// IsBlockFragmentationContextRoot reports whether this builder is a block
// fragmentation context root (a multicol). Mirrors Blink's
// `FragmentBuilder::IsBlockFragmentationContextRoot`
// (fragment_builder.h:426-428 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) IsBlockFragmentationContextRoot() bool {
	return b.isBlockFragmentationContextRoot
}

// SetIsBlockFragmentationContextRoot marks this builder as a block
// fragmentation context root. Mirrors Blink's
// `FragmentBuilder::SetIsBlockFragmentationContextRoot`.
func (b *BoxFragmentBuilder) SetIsBlockFragmentationContextRoot() {
	b.isBlockFragmentationContextRoot = true
}

// PropagateOOFPositionedInfo adopts the out-of-flow candidates and
// fragmentainer descendants of childResult, a child added at offset (its
// border-box offset in this builder's content box) and shifted by
// relativeOffset. Candidates' static positions move from the child's
// content box into this builder's: the child's offset, border+padding and
// relative offset are added, through a physical round trip when the child's
// writing direction differs. Fragmentainer descendants keep their
// CB-relative static positions (PropagateOOFFragmentainerDescendants).
// Every algorithm calls it right after adding the child. Mirrors Blink's
// FragmentBuilder::PropagateOOFPositionedInfo (fragment_builder.cc:746-923
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which Blink's AddResult /
// AddChild reach through PropagateFromFragment (:357-405). louis14's static
// positions are content-box relative (Blink: border-box), hence the
// border+padding term.
func (b *BoxFragmentBuilder) PropagateOOFPositionedInfo(childResult *LayoutResult, childStyle *css.Style, offset, relativeOffset LogicalOffset) {
	if childResult == nil {
		return
	}
	hasFragmentedOofData := childResult.Fragment != nil && childResult.Fragment.FragmentedOofData != nil
	if len(childResult.PropagatedOOFCandidates) == 0 && !hasFragmentedOofData {
		return
	}
	parentWDM := b.wdm
	// The child's border+padding in its own logical axes, converted to
	// this builder's through physical edges.
	childWDM := NewWritingDirectionMode(childStyle)
	childGeom := ComputeFragmentGeometry(childStyle, childWDM)
	physBP := ToPhysicalEdges(LogicalEdges{
		InlineStart: childGeom.Border.InlineStart + childGeom.Padding.InlineStart,
		InlineEnd:   childGeom.Border.InlineEnd + childGeom.Padding.InlineEnd,
		BlockStart:  childGeom.Border.BlockStart + childGeom.Padding.BlockStart,
		BlockEnd:    childGeom.Border.BlockEnd + childGeom.Padding.BlockEnd,
	}, childWDM)
	parentBP := ToLogicalEdges(physBP, parentWDM)
	// adjusted_offset = offset + relative_offset (fragment_builder.cc:762).
	adjusted := LogicalOffset{
		InlineOffset: offset.InlineOffset + parentBP.InlineStart + relativeOffset.InlineOffset,
		BlockOffset:  offset.BlockOffset + parentBP.BlockStart + relativeOffset.BlockOffset,
	}

	needsConversion := childWDM.WM != parentWDM.WM || childWDM.Dir != parentWDM.Dir
	var childContentPhys PhysicalSize
	if needsConversion && childResult.Fragment != nil {
		childContentPhys = PhysicalSize{
			Width:  max(0, childResult.Fragment.Size.WidthF64()-physBP.Left-physBP.Right),
			Height: max(0, childResult.Fragment.Size.HeightF64()-physBP.Top-physBP.Bottom),
		}
	}
	for _, cand := range childResult.PropagatedOOFCandidates {
		if needsConversion {
			// child-logical → physical → parent-logical, within the child's
			// content box.
			cand.StaticPosition = cand.StaticPosition.ConvertToPhysical(childWDM, childContentPhys).ConvertToLogical(parentWDM, childContentPhys)
		}
		cand.StaticPosition.Offset.InlineOffset += adjusted.InlineOffset
		cand.StaticPosition.Offset.BlockOffset += adjusted.BlockOffset
		b.AddOutOfFlowCandidate(cand)
	}

	if hasFragmentedOofData {
		b.PropagateOOFFragmentainerDescendants(childResult.Fragment, adjusted, LogicalOffset{}, layoutunit.LayoutUnit{}, nil, nil, nil)
	}
}

// PropagateOOFFragmentainerDescendants walks any deferred OOF fragmentainer
// descendants stashed on `childFragment` (via FragmentedOofData) and appends
// them to this builder's own deferred list. Mirrors Blink's
// `FragmentBuilder::PropagateOOFFragmentainerDescendants`
// (fragment_builder.cc:926-1100).
//
// Coordinate semantics (Blink-aligned, source-verified at fb.cc:945-986):
//   - The descendant's StaticPosition stays in CONTAINING-BLOCK content-box
//     coordinates throughout the walk. Do NOT shift by parent offsets — the
//     position was captured at the deferral site relative to its CB and the
//     drain pipeline expects it to remain so.
//   - The descendant's `ContainingBlock.Offset` is the offset from the CB's
//     fragment to the propagating fragment (this builder's fragment). It DOES
//     accumulate `offset + relativeOffset` at each propagation step, plus
//     `cbAdjustment` (the block-offset adjustment when a CB resumes in a
//     later outer fragmentainer).
//   - When a descendant's `ContainingBlock.Fragment` is nil, this means the
//     descendant was promoted at THIS child's level (the child IS the CB).
//     Assign `childFragment` as the CB. Mirrors fb.cc:952-954.
//
// Pending-multicol entries are forwarded as well so the outer fragmentation
// root sees both the OOFs and the multicols that need revisiting. When out
// is non-nil the descendants go there instead of this builder, and pending
// multicols are not forwarded (Blink's
// out_list, used by column balancing, fragment_builder.cc:925-1106 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) PropagateOOFFragmentainerDescendants(
	childFragment *PhysicalFragment,
	offset LogicalOffset,
	relativeOffset LogicalOffset,
	cbAdjustment layoutunit.LayoutUnit,
	containingBlock *LogicalOofContainingBlock,
	fixedposCB *LogicalOofContainingBlock,
	out *[]LogicalOofNodeForFragmentation,
) {
	toBuilder := out == nil
	if toBuilder {
		out = &b.oofPositionedFragmentainerDescendants
	}
	if childFragment == nil || childFragment.FragmentedOofData == nil {
		return
	}
	data := childFragment.FragmentedOofData
	totalInline := offset.InlineOffset + relativeOffset.InlineOffset
	totalBlock := offset.BlockOffset + relativeOffset.BlockOffset
	cbAdjBlock := cbAdjustment.Float64()
	// A descendant propagated out of a column spanner is tagged, so the
	// inner multicol's pending-OOF layout leaves it to the next
	// fragmentation context (fragment_builder.cc:974-975 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Blink's IsColumnSpanAll
	// also requires tree validity; louis14 checks the style half
	// (isColumnSpanAll), which only matters for a column-span:all box that
	// is not a real spanner and carries fragmentainer descendants.
	isColumnSpanner := childFragment.LayoutNode != nil && isColumnSpanAll(childFragment.LayoutNode)

	if len(data.OofPositionedFragmentainerDescendants) > 0 {
		for _, d := range data.OofPositionedFragmentainerDescendants {
			// An inner multicol keeps the fragmentainer descendants whose
			// containing block is inside it: they are laid out in its
			// columns once the outermost fragmentation context root is
			// reached (LayoutOOFsInMulticol). Only one whose containing
			// block is inside a column spanner it contains goes on, to the
			// next fragmentation context root, with its spanner tag reset
			// (fragment_builder.cc:952-972 @
			// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
			if childFragment.IsFragmentationContextRoot && d.ContainingBlock.Fragment != nil {
				if !d.ContainingBlock.IsInsideColumnSpanner {
					continue
				}
				d.ContainingBlock.IsInsideColumnSpanner = false
				d.FixedposContainingBlock.IsInsideColumnSpanner = false
			}
			if isColumnSpanner {
				d.ContainingBlock.IsInsideColumnSpanner = true
			}
			// CB fragment fallback: when missing, the propagating child IS the
			// CB (descendant was promoted at child's level).
			if d.ContainingBlock.Fragment == nil {
				if containingBlock != nil && containingBlock.Fragment != nil {
					d.ContainingBlock = *containingBlock
				} else {
					d.ContainingBlock.Fragment = childFragment
				}
			}
			// StaticPosition stays CB-relative — do NOT shift by parent offsets.
			// Only the CB offsets accumulate.
			d.ContainingBlock.Offset.InlineOffset += totalInline
			d.ContainingBlock.Offset.BlockOffset += totalBlock + cbAdjBlock
			// The fixedpos containing block: found here when the child is a
			// fixed container, then accumulating offsets like the
			// containing block; else the one passed in
			// (fragment_builder.cc:1042-1072).
			fixedposFragment := d.FixedposContainingBlock.Fragment
			b.AdjustFixedposContainerInfo(childFragment, &fixedposFragment)
			if fixedposFragment != nil {
				d.FixedposContainingBlock.Fragment = fixedposFragment
				d.FixedposContainingBlock.Offset.InlineOffset += totalInline
				d.FixedposContainingBlock.Offset.BlockOffset += totalBlock + cbAdjBlock
				if isColumnSpanner {
					d.FixedposContainingBlock.IsInsideColumnSpanner = true
				}
			} else if fixedposCB != nil {
				d.FixedposContainingBlock = *fixedposCB
			}
			*out = append(*out, d)
		}
	}

	if !toBuilder {
		return
	}
	for node, info := range data.MulticolsWithPendingOOFs {
		// Once the multicol's fixedpos containing block is found (here, when
		// the child is a fixed container, or further down) its offset stays
		// relative to it and the containing block's offset accumulates;
		// until then the multicol offset is translated into this builder's
		// frame (fragment_builder.cc:842-917 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		propagated := *info
		fixedposFragment := propagated.FixedposContainingBlock.Fragment
		b.AdjustFixedposContainerInfo(childFragment, &fixedposFragment)
		if fixedposFragment != nil {
			propagated.FixedposContainingBlock.Fragment = fixedposFragment
			propagated.FixedposContainingBlock.RelativeOffset.InlineOffset += relativeOffset.InlineOffset
			propagated.FixedposContainingBlock.RelativeOffset.BlockOffset += relativeOffset.BlockOffset
			if !childFragment.IsFragmentainerBox() {
				propagated.FixedposContainingBlock.Offset.InlineOffset += offset.InlineOffset
				propagated.FixedposContainingBlock.Offset.BlockOffset += offset.BlockOffset
			}
			propagated.FixedposContainingBlock.Offset.BlockOffset += cbAdjBlock
			if isColumnSpanner {
				propagated.FixedposContainingBlock.IsInsideColumnSpanner = true
			}
		} else {
			propagated.MulticolOffset.InlineOffset += totalInline
			propagated.MulticolOffset.BlockOffset += totalBlock
		}
		b.AddMulticolWithPendingOOFs(node, &propagated)
	}
}

// SetChildAvailableSize records the content-box size that serves as the
// containing block for children's position:relative percentage resolution.
// Layout algorithms call this once before adding children. Mirrors Blink's
// BoxFragmentBuilder::child_available_size_, which its constructor sets from
// CalculateChildAvailableSize (box_fragment_builder.h:93-95 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) SetChildAvailableSize(size LogicalSize) {
	b.childAvailableSize = size
	b.hasChildAvailableSize = true
}

// ChildAvailableSize returns the content-box size set by
// SetChildAvailableSize (Blink's BoxFragmentBuilder::ChildAvailableSize,
// box_fragment_builder.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) ChildAvailableSize() LogicalSize {
	return b.childAvailableSize
}

// AddChild adds a child fragment at the given logical offset (relative to
// this fragment's content box), applying the child's position:relative
// offset: a CSS box child (not a line box, text run or fragmentainer) with
// position:relative is shifted by ComputeRelativeOffsetForBoxFragment
// against this builder's child available size. Mirrors Blink's
// BoxFragmentBuilder::AddChild with no relative_offset supplied
// (box_fragment_builder.cc:255-267, :372-375 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A builder whose algorithm never
// sets its child available size (a louis14 line box, whose inline-level
// children are shifted by the inline layout, as Blink's line items are by
// InlineLayoutStateStack::ApplyRelativePositioning) applies no offset.
//
// position:sticky emits zero layout-time offset — per Blink, the sticky
// offset is a scroll-time update via StickyPositionScrollingConstraints,
// not baked into layout fragments.
func (b *BoxFragmentBuilder) AddChild(fragment *PhysicalFragment, offset LogicalOffset) {
	b.AddChildWithRelativeOffset(fragment, offset, b.relativeOffsetFor(fragment))
}

// relativeOffsetFor returns the relative offset AddChild applies to
// fragment, in this builder's logical coordinates (box_fragment_builder.cc:
// 255-267 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) relativeOffsetFor(fragment *PhysicalFragment) LogicalOffset {
	if !b.hasChildAvailableSize || fragment == nil || !fragment.IsCSSBox() {
		return LogicalOffset{}
	}
	return ComputeRelativeOffsetForBoxFragment(fragment, b.wdm, b.childAvailableSize)
}

// AddChildWithRelativeOffset adds a child at offset shifted by a relative
// offset the caller resolved itself — against a containing block other than
// this builder's child available size (a grid area, a table row) or, for
// inline-level content, in line-logical coordinates. Mirrors Blink's
// BoxFragmentBuilder::AddChild with a supplied relative_offset, which skips
// ComputeRelativeOffsetForBoxFragment and calls AddChildInternal(&child,
// child_offset + *relative_offset) (box_fragment_builder.cc:255, :372-375 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) AddChildWithRelativeOffset(fragment *PhysicalFragment, offset, relativeOffset LogicalOffset) {
	b.children = append(b.children, logicalChildLink{
		offset: LogicalOffset{
			InlineOffset: offset.InlineOffset + relativeOffset.InlineOffset,
			BlockOffset:  offset.BlockOffset + relativeOffset.BlockOffset,
		},
		fragment: fragment,
	})
	// Some content is here now, so normal breaking rules apply from now on
	// (box_fragment_builder.cc:376-377 @ a9f50e522efa).
	b.requiresContentBeforeBreaking = false
}

// MoveChildrenInBlockDirection moves every child, the first and last
// baselines and the out-of-flow candidates' static positions by offset in
// the block direction. Mirrors BoxFragmentBuilder::MoveChildrenInDirection
// with is_block_direction (box_fragment_builder.cc:396-477 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 has no inflow bounds
// or fragment items builder to move, and fragmentainer descendants are not
// moved.
func (b *BoxFragmentBuilder) MoveChildrenInBlockDirection(offset float64) {
	if offset == 0 {
		return
	}
	if b.hasBaseline {
		b.baseline += offset
	}
	if b.lastBaseline != 0 {
		b.lastBaseline += offset
	}
	for i := range b.children {
		b.children[i].offset.BlockOffset += offset
	}
	for i := range b.outOfFlowCandidates {
		b.outOfFlowCandidates[i].StaticPosition.Offset.BlockOffset += offset
	}
}

// ShiftChildBlockOffset adds delta to the block offset of the most recently
// added child link whose fragment pointer matches. Used by the list-item
// block layout to push content down when an OUTSIDE marker's ascent is taller
// than the content's (Blink's UnpositionedListMarker::AddToBox content-push
// branch: *block_offset -= baseline_adjust). The content child has already
// been added to the builder, so it is re-offset in place.
func (b *BoxFragmentBuilder) ShiftChildBlockOffset(fragment *PhysicalFragment, delta float64) {
	for i := len(b.children) - 1; i >= 0; i-- {
		if b.children[i].fragment == fragment {
			b.children[i].offset.BlockOffset += delta
			return
		}
	}
}

// SetHasColumnSpanner records whether a column spanner was found inside this
// box. Mirrors Blink's FragmentBuilder::SetHasColumnSpanner
// (fragment_builder.h:435-437 @ a9f50e522efa).
func (b *BoxFragmentBuilder) SetHasColumnSpanner(v bool) {
	b.hasColumnSpanner = v
}

// SetColumnSpannerPath sets the path to the column spanner found inside this
// box, and records that one was found. Mirrors Blink's
// FragmentBuilder::SetColumnSpannerPath (fragment_builder.h:438-441 @
// a9f50e522efa).
func (b *BoxFragmentBuilder) SetColumnSpannerPath(path *ColumnSpannerPath) {
	b.columnSpannerPath = path
	b.SetHasColumnSpanner(path != nil)
}

// FoundColumnSpanner reports whether a column spanner was found inside this
// box, with or without a path. Mirrors Blink's
// FragmentBuilder::FoundColumnSpanner (fragment_builder.h:442-445 @
// a9f50e522efa).
func (b *BoxFragmentBuilder) FoundColumnSpanner() bool {
	return b.hasColumnSpanner
}

// SetShouldForceSameFragmentationFlow marks that this box's breaks stay in
// its parent's flow (see shouldForceSameFragmentationFlow).
func (b *BoxFragmentBuilder) SetShouldForceSameFragmentationFlow() {
	b.shouldForceSameFragmentationFlow = true
}

// SetIsEmptySpannerParent marks that this fragment holds nothing before the
// spanner it was interrupted by. Mirrors Blink's
// FragmentBuilder::SetIsEmptySpannerParent (fragment_builder.h:446-449 @
// a9f50e522efa).
func (b *BoxFragmentBuilder) SetIsEmptySpannerParent(v bool) {
	b.isEmptySpannerParent = v
}

// IsEmptySpannerParent reports whether this fragment holds nothing before the
// spanner it was interrupted by. Mirrors Blink's
// FragmentBuilder::IsEmptySpannerParent (fragment_builder.h:450 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *BoxFragmentBuilder) IsEmptySpannerParent() bool {
	return b.isEmptySpannerParent
}

// finalBlockSizeForFragmentation is the block-size for fragmentation the
// built fragment records: the children's furthest block-end, made border-box
// relative by the block-start border and padding (louis14 child offsets are
// content-box relative; zero on a slice continuation, whose boxData omits
// it); for a box other than a fragmentainer at least its own block-size, or
// only its own block-size when it clips block-axis overflow. A fragmentainer
// (column) keeps the children-only value. Mirrors BoxFragmentBuilder::
// ToBoxFragment (box_fragment_builder.cc:743-761 @ a9f50e522efa), which
// also ignores child overflow when the block-size for fragmentation was
// clamped (:750-751).
func (b *BoxFragmentBuilder) finalBlockSizeForFragmentation() float64 {
	bsff := 0.0
	if b.hasBlockSizeForFragmentation {
		bsff = b.blockSizeForFragmentation
		if b.boxData != nil {
			border := ToLogicalEdges(b.boxData.Border, b.wdm)
			padding := ToLogicalEdges(b.boxData.Padding, b.wdm)
			bsff += border.BlockStart + padding.BlockStart
		}
	}
	if b.boxType == BoxTypeColumn {
		return bsff
	}
	if clipsBlockAxisOverflow(b.style, b.node, b.wdm) || b.isBlockSizeForFragmentationClamped {
		return b.size.BlockSize
	}
	return max(bsff, b.size.BlockSize)
}

// Build converts all logical coordinates to physical and returns the
// immutable PhysicalFragment and LayoutResult.
//
// This is the single point where logical→physical conversion happens.
func (b *BoxFragmentBuilder) Build() *LayoutResult {
	physSize := ToPhysicalSize(b.size, b.wdm.WM)

	// Children are stored with content-relative logical offsets.
	// The converter's outer size must be the CONTENT-BOX physical size so that
	// "outerW - block - innerW" type formulas (used by RTL, vertical, sideways
	// modes) give content-relative physical offsets that fragmentToBox can add
	// to the content origin.  For boxes without borders/padding (line boxes,
	// anonymous boxes), boxData is nil and physSize is already the content size.
	convSize := physSize
	if b.boxData != nil {
		bd := b.boxData
		w := physSize.Width - bd.Border.Left - bd.Border.Right - bd.Padding.Left - bd.Padding.Right
		h := physSize.Height - bd.Border.Top - bd.Border.Bottom - bd.Padding.Top - bd.Padding.Bottom
		if w < 0 {
			w = 0
		}
		if h < 0 {
			h = 0
		}
		convSize = PhysicalSize{Width: w, Height: h}
	}
	conv := NewConverter(b.wdm, convSize)
	physChildren := make([]ChildLink, len(b.children))
	for i, child := range b.children {
		childPhysSize := geomSizeToOld(child.fragment.Size)
		physOff := conv.ToPhysicalOffset(child.offset, childPhysSize)
		physChildren[i] = ChildLink{
			Offset:   geometry.PhysicalOffsetFromF64Round(physOff.X, physOff.Y),
			Fragment: child.fragment,
		}
	}

	fragment := &PhysicalFragment{
		Size:                       oldSizeToGeom(physSize),
		Children:                   physChildren,
		WritingDirection:           b.wdm,
		BoxData:                    b.boxData,
		Node:                       b.node,
		Style:                      b.style,
		LayoutNode:                 b.layoutNode,
		GapGeometry:                b.gapGeometry,
		IsMonolithic:               b.isMonolithic,
		IsFragmentationContextRoot: b.isBlockFragmentationContextRoot,
		notFirstForNode:            b.notFirstForNode,
		BoxType:                    b.GetBoxType(),
	}

	fragment.updateScrollableOverflow()

	// Phase 25 Cmt-2: stash any deferred OOF fragmentainer descendants and
	// pending-multicol entries on the outgoing fragment so ancestors can
	// continue propagating them to the outer fragmentation root. Mirrors
	// Blink's PhysicalFragment::OofData carried via FragmentedOofData
	// (oof_positioned_node.h:366-408).
	if len(b.oofPositionedFragmentainerDescendants) > 0 || len(b.multicolsWithPendingOOFs) > 0 {
		fragment.FragmentedOofData = &FragmentedOofData{
			OofPositionedFragmentainerDescendants: b.oofPositionedFragmentainerDescendants,
			MulticolsWithPendingOOFs:              b.multicolsWithPendingOOFs,
		}
	}

	// Layout containment suppresses the box's baselines: parents synthesize
	// from the border-box edge instead (existing HasBaseline==false /
	// LastBaseline==0 fallbacks). Table cells are exempt. Mirrors Blink
	// physical_box_fragment.cc:442 @ ea697c8865fc20dc39c19d49845a184f6d0ab24c:
	//   const bool allow_baseline =
	//       !layout_object_->ShouldApplyLayoutContainment() ||
	//       layout_object_->IsTableCell();
	allowBaseline := b.style == nil ||
		!b.style.ShouldApplyLayoutContainment() ||
		b.style.GetDisplay() == css.DisplayTableCell

	result := &LayoutResult{
		Fragment:               fragment,
		IntrinsicBlockSize:     b.intrinsicBlockSize,
		UnpositionedListMarker: b.unpositionedListMarker,
		EndMarginStrut:         b.endMarginStrut,
		ExclusionSpace:         b.exclusionSpace,
		BreakAppeal:            BreakAppealPerfect,
		ColumnSpannerPath:      b.columnSpannerPath,
		IsEmptySpannerParent:   b.isEmptySpannerParent,

		ShouldForceSameFragmentationFlow: b.shouldForceSameFragmentationFlow,

		InitialBreakBefore: "auto",
		FinalBreakAfter:    "auto",
	}
	// Mirrors Blink's LayoutResult constructor (layout_result.cc:89-95 @
	// a9f50e522efa): propagated break values stop at a box with layout
	// containment. The ShouldPropagateChildBreakValues gate is applied where
	// PropagateChildBreakValues is called; without it nothing is recorded.
	if b.style == nil || !b.style.ShouldApplyLayoutContainment() {
		if b.hasInitialBreakBefore {
			result.InitialBreakBefore = b.initialBreakBefore
		}
		result.FinalBreakAfter = b.PreviousBreakAfter()
	}
	if allowBaseline {
		result.Baseline = b.baseline
		result.HasBaseline = b.hasBaseline
		result.LastBaseline = b.lastBaseline
		result.UseLastBaselineForInlineBaseline = b.useLastBaselineForInlineBaseline
	}
	if b.hasMinimalSpaceShortage {
		result.MinSpaceShortage = b.minimalSpaceShortage
	}
	result.HasForcedBreak = b.hasForcedBreak
	if b.space != nil && b.space.HasBlockFragmentation {
		result.BlockSizeForFragmentation = b.finalBlockSizeForFragmentation()
		result.HasBlockSizeForFragmentation = true
		result.IsBlockSizeForFragmentationClamped = b.isBlockSizeForFragmentationClamped
	}
	if b.hasTallestUnbreakableBlockSize {
		result.TallestUnbreakableBlockSize = b.tallestUnbreakableBlockSize
	}
	if b.hasBreakAppeal {
		result.BreakAppeal = b.breakAppeal
	}
	return result
}

// AdjustSpaceShortageForBreakAvoidRoot replaces the space shortage of a
// break-inside:avoid root that broke inside before its block end during
// column balancing: what its content propagated would only stretch the
// columns a little at a time, so it is dropped, and the root's first
// fragment reports instead by how much its unbroken block-size, from a
// layout without fragmentation, exceeds the space left. Mirrors
// BoxFragmentBuilder::ToBoxFragment (box_fragment_builder.cc:712-740 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 builds the outgoing
// break token after Build, so the algorithms call this once result carries
// it. Blink's DisableLayoutSideEffectsScope has no louis14 counterpart;
// IsFragmentainerBoxType is a column box type here (louis14 has no pages).
func (b *BoxFragmentBuilder) AdjustSpaceShortageForBreakAvoidRoot(ctx *LayoutContext, result *LayoutResult) {
	space := b.space
	if space == nil || result.BreakToken == nil || result.BreakToken.IsAtBlockEnd ||
		!space.IsInsideBalancedColumns || b.GetBoxType() == BoxTypeColumn || b.style == nil ||
		!IsAvoidBreakValue(*space, b.style.GetBreakInside()) || space.IsInsideBreakAvoid ||
		!space.HasKnownFragmentainerBlockSize() {
		return
	}
	result.MinSpaceShortage = 0
	if b.PreviousBreakToken().IsBreakInside() {
		return
	}
	measure := layoutElement(ctx, b.layoutNode, space.CloneWithoutFragmentation())
	if measure == nil || measure.Status != LayoutResultSuccess || measure.Fragment == nil {
		return
	}
	shortage := NewLogicalFragment(space.WritingDirection, measure.Fragment).BlockSize() - FragmentainerSpaceLeft(b, false)
	result.MinSpaceShortage = max(shortage, 0)
}
