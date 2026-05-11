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

	// baseline position (first baseline).
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

	// firstBaseline is the first baseline position across all column/spanner
	// children, updated via SetFirstBaseline during PropagateBaselineFromChild.
	// Mirrors Blink's BoxFragmentBuilder::first_baseline_ (std::optional).
	firstBaseline    float64
	hasFirstBaseline bool

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
	// `OutOfFlowLayoutPart.HandleMulticolsWithPendingOOFs`. Phase 25
	// scaffolding — wiring in subsequent commits. Mirrors Blink's
	// `FragmentBuilder::multicols_with_pending_oofs_` (fragment_builder.h:51-55).
	// Keyed by *LayoutInputNode (louis14's persistent layout-tree identity;
	// where Blink keys by *LayoutBox — see pkg/layout/layout_input_node.go).
	multicolsWithPendingOOFs map[*LayoutInputNode]*MulticolWithPendingOOFs

	// isBlockFragmentationContextRoot marks this builder as the outermost
	// multicol (or paginated root) — the only place
	// `OutOfFlowLayoutPart.HandleFragmentation` actually drains the
	// descendants list. Phase 25 scaffolding — gate-setter wiring in a
	// subsequent commit. Mirrors Blink's
	// `FragmentBuilder::IsBlockFragmentationContextRoot()`
	// (fragment_builder.h:415-423).
	isBlockFragmentationContextRoot bool

	// childAvailableSize is the content-box size of this fragment — the
	// containing block for children's position:relative/sticky percentage
	// resolution. Mirrors Blink's FragmentBuilder::child_available_size_.
	// Set by the enclosing layout algorithm via SetChildAvailableSize before
	// adding children; consumed by AddChild to compute a child's RelativeOffset.
	childAvailableSize    LogicalSize
	hasChildAvailableSize bool

	// minimalSpaceShortage tracks the smallest block-size overflow that
	// caused an unforced break in any descendant column during a nested
	// balancing pass. Mirrors Blink's
	// FragmentBuilder::minimal_space_shortage_ (fragment_builder.h). Read
	// into LayoutResult.MinSpaceShortage at Build().
	minimalSpaceShortage    float64
	hasMinimalSpaceShortage bool

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

	// hasClonedBoxDecorations is set when the element has
	// box-decoration-break:clone. When true, each fragment behaves as
	// an independent decorated box — block-start AND block-end borders
	// appear on every fragment. Mirrors Blink's
	// BoxFragmentBuilder::clone_box_start_decorations_ /
	// clone_box_end_decorations_.
	hasClonedBoxDecorations bool

	// previousBreakAfter is the break-after value of the most recently added
	// in-flow child. JoinedBreakBetweenValue joins it with the next child's
	// break-before to compute the effective break-between. Mirrors Blink's
	// BoxFragmentBuilder::previous_break_after_ + JoinedBreakBetweenValue.
	previousBreakAfter string

	// breakAppeal is the appeal of the break that produced this fragment;
	// BreakAppealPerfect when no break was inserted. Read into
	// LayoutResult.BreakAppeal at Build(). Mirrors Blink's
	// FragmentBuilder::break_appeal_.
	breakAppeal    BreakAppeal
	hasBreakAppeal bool
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

// SetBaseline sets the first baseline position.
func (b *BoxFragmentBuilder) SetBaseline(v float64) {
	b.baseline = v
	b.hasBaseline = true
}

// SetFirstBaseline sets the first baseline, keeping the minimum value across
// multiple calls (earlier column wins). Mirrors Blink's
// BoxFragmentBuilder::SetFirstBaseline used by PropagateBaselineFromChild.
func (b *BoxFragmentBuilder) SetFirstBaseline(v float64) {
	b.firstBaseline = v
	b.hasFirstBaseline = true
}

// FirstBaseline returns the current first-baseline value and whether it has
// been set. Used by PropagateBaselineFromChild to apply min semantics.
func (b *BoxFragmentBuilder) FirstBaseline() (float64, bool) {
	return b.firstBaseline, b.hasFirstBaseline
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

// SetHasClonedBoxDecorations marks this fragment as having cloned box
// decorations (box-decoration-break:clone). When true, each fragment
// behaves as an independent decorated box — block-start and block-end
// borders appear on every fragment. Mirrors Blink's
// SetupFragmentBuilderForFragmentation setting clone_box_start_decorations_
// and clone_box_end_decorations_.
func (b *BoxFragmentBuilder) SetHasClonedBoxDecorations(v bool) {
	b.hasClonedBoxDecorations = v
}

// HasClonedBoxDecorations reports whether this fragment has cloned box
// decorations (box-decoration-break:clone).
func (b *BoxFragmentBuilder) HasClonedBoxDecorations() bool {
	return b.hasClonedBoxDecorations
}

// SetExclusionSpace sets the updated float exclusion state.
func (b *BoxFragmentBuilder) SetExclusionSpace(es *ExclusionSpace) {
	b.exclusionSpace = es
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
// Also sets HasClonedBoxDecorations when the element has box-decoration-break:clone.
func (b *BoxFragmentBuilder) SetLayoutNode(lin *LayoutInputNode) {
	b.node = lin.DOMNode
	b.style = lin.Style()
	b.layoutNode = lin
	if s := lin.Style(); s != nil && s.GetBoxDecorationBreak() == css.BoxDecorationBreakClone {
		b.hasClonedBoxDecorations = true
	}
}

// AddOutOfFlowCandidate records an absolutely/fixed positioned child
// for deferred layout by OutOfFlowLayoutPart.
// PropagateSpaceShortage records a minimum space shortage reported by an
// inner nested multicol whose balancing pass gave up. The outer balancing
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

// SetPreviousBreakAfter records the break-after value of the most recently
// added in-flow child. JoinedBreakBetweenValue uses it to compute the
// effective break-between value when a subsequent child is added.
// Mirrors Blink's BoxFragmentBuilder::SetPreviousBreakAfter.
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

// IsBlockFragmentationContextRoot reports whether this builder is the
// outermost fragmentation root (the only place
// `OutOfFlowLayoutPart.HandleFragmentation` actually drains the descendants
// list). Mirrors Blink's `FragmentBuilder::IsBlockFragmentationContextRoot`
// (fragment_builder.h:415-423).
func (b *BoxFragmentBuilder) IsBlockFragmentationContextRoot() bool {
	return b.isBlockFragmentationContextRoot
}

// SetIsBlockFragmentationContextRoot marks this builder as the outermost
// fragmentation root. Mirrors Blink's
// `FragmentBuilder::SetIsBlockFragmentationContextRoot`.
func (b *BoxFragmentBuilder) SetIsBlockFragmentationContextRoot() {
	b.isBlockFragmentationContextRoot = true
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
// root sees both the OOFs and the multicols that need revisiting.
func (b *BoxFragmentBuilder) PropagateOOFFragmentainerDescendants(
	childFragment *PhysicalFragment,
	offset LogicalOffset,
	relativeOffset LogicalOffset,
	cbAdjustment layoutunit.LayoutUnit,
	containingBlock *LogicalOofContainingBlock,
	fixedposCB *LogicalOofContainingBlock,
) {
	if childFragment == nil || childFragment.FragmentedOofData == nil {
		return
	}
	data := childFragment.FragmentedOofData

	if len(data.OofPositionedFragmentainerDescendants) > 0 {
		totalInline := offset.InlineOffset + relativeOffset.InlineOffset
		totalBlock := offset.BlockOffset + relativeOffset.BlockOffset
		cbAdjBlock := cbAdjustment.Float64()
		for _, d := range data.OofPositionedFragmentainerDescendants {
			// CB fragment fallback: when missing, the propagating child IS the
			// CB (descendant was promoted at child's level).
			if d.ContainingBlock.Fragment == nil {
				if containingBlock != nil && containingBlock.Fragment != nil {
					d.ContainingBlock = *containingBlock
				} else {
					d.ContainingBlock.Fragment = childFragment
				}
			}
			if d.FixedposContainingBlock.Fragment == nil && fixedposCB != nil {
				d.FixedposContainingBlock = *fixedposCB
			}
			// StaticPosition stays CB-relative — do NOT shift by parent offsets.
			// Only the CB offsets accumulate.
			d.ContainingBlock.Offset.InlineOffset += totalInline
			d.ContainingBlock.Offset.BlockOffset += totalBlock + cbAdjBlock
			d.FixedposContainingBlock.Offset.InlineOffset += totalInline
			d.FixedposContainingBlock.Offset.BlockOffset += totalBlock + cbAdjBlock
			b.oofPositionedFragmentainerDescendants = append(b.oofPositionedFragmentainerDescendants, d)
		}
	}

	for node, info := range data.MulticolsWithPendingOOFs {
		// Translate the inner multicol's offset into this builder's frame so
		// the outer drain site can position OOFs against the right column flow.
		propagated := *info
		propagated.MulticolOffset.InlineOffset += offset.InlineOffset + relativeOffset.InlineOffset
		propagated.MulticolOffset.BlockOffset += offset.BlockOffset + relativeOffset.BlockOffset
		b.AddMulticolWithPendingOOFs(node, &propagated)
	}
}

// SetChildAvailableSize records the content-box size that serves as the
// containing block for children's position:relative/sticky percentage
// resolution. Layout algorithms call this once before adding children.
// Mirrors Blink's FragmentBuilder::SetAvailableSize.
func (b *BoxFragmentBuilder) SetChildAvailableSize(size LogicalSize) {
	b.childAvailableSize = size
	b.hasChildAvailableSize = true
}

// AddChild adds a child fragment at the given logical offset.
// The offset is relative to this fragment's content box origin.
//
// CSS 2.1 §9.4.3: if the child is position:relative and its RelativeOffset
// has not already been computed by the layout algorithm, compute it here
// from the child's style and the parent's childAvailableSize. Mirrors
// Blink's BoxFragmentBuilder::AddChild, which unconditionally calls
// ComputeRelativeOffsetForBoxFragment at add-time; the RelativeOffset == 0
// guard lets existing per-algorithm tail blocks short-circuit the work.
//
// position:sticky emits zero layout-time offset — per Blink, the sticky
// offset is a scroll-time update via StickyPositionScrollingConstraints,
// not baked into layout fragments. Scroll-time wiring is deferred until
// scroll-based sticky tests arrive.
func (b *BoxFragmentBuilder) AddChild(fragment *PhysicalFragment, offset LogicalOffset) {
	if fragment != nil && fragment.Style != nil && fragment.RelativeOffset == (geometry.PhysicalOffset{}) && b.hasChildAvailableSize {
		pos := fragment.Style.GetPosition()
		if pos == css.PositionRelative {
			cbBlock := b.childAvailableSize.BlockSize
			if cbBlock == Indefinite {
				cbBlock = 0 // auto CB height → percentages compute to 0
			}
			physCB := ToPhysicalSize(LogicalSize{
				InlineSize: b.childAvailableSize.InlineSize,
				BlockSize:  cbBlock,
			}, b.wdm.WM)
			off := fragment.Style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
			fragment.RelativeOffset = computeRelativeOffset(off, b.wdm)
		}
	}
	b.children = append(b.children, logicalChildLink{
		offset:   offset,
		fragment: fragment,
	})
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
		Size:             oldSizeToGeom(physSize),
		Children:         physChildren,
		WritingDirection: b.wdm,
		BoxData:          b.boxData,
		Node:             b.node,
		Style:            b.style,
		LayoutNode:       b.layoutNode,
		GapGeometry:      b.gapGeometry,
		IsMonolithic:     b.isMonolithic,
	}

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

	result := &LayoutResult{
		Fragment:                         fragment,
		IntrinsicBlockSize:               b.intrinsicBlockSize,
		Baseline:                         b.baseline,
		HasBaseline:                      b.hasBaseline,
		FirstBaseline:                    b.firstBaseline,
		HasFirstBaseline:                 b.hasFirstBaseline,
		LastBaseline:                     b.lastBaseline,
		UseLastBaselineForInlineBaseline: b.useLastBaselineForInlineBaseline,
		UnpositionedListMarker:           b.unpositionedListMarker,
		EndMarginStrut:                   b.endMarginStrut,
		ExclusionSpace:                   b.exclusionSpace,
		BreakAppeal:                      BreakAppealPerfect,
	}
	if b.hasMinimalSpaceShortage {
		result.MinSpaceShortage = b.minimalSpaceShortage
	}
	if b.hasTallestUnbreakableBlockSize {
		result.TallestUnbreakableBlockSize = b.tallestUnbreakableBlockSize
	}
	if b.hasBreakAppeal {
		result.BreakAppeal = b.breakAppeal
	}
	return result
}
