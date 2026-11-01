package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/html"
)

// LayoutResult is the immutable output of a layout algorithm.
// It contains the fragment (positioned box with physical coordinates)
// and metadata needed by the parent for margin collapsing, float
// propagation, and baseline alignment.
//
// Ported from Blink's LayoutResult (layout_result.h).
type LayoutResult struct {
	// Fragment is the physical fragment produced by layout.
	// Contains the box's physical size and positioned children.
	Fragment *PhysicalFragment

	// IntrinsicBlockSize is the block-size of the content before
	// min/max constraints are applied. Used by the parent to compute
	// the "auto" block-size.
	IntrinsicBlockSize float64

	// Baseline is the first baseline position in the block direction,
	// relative to the fragment's block-start edge. Used for alignment. For
	// multicol containers this is the baseline propagated across columns and
	// spanners by PropagateBaselineFromChild. Mirrors Blink's
	// BoxFragmentBuilder::first_baseline_, read back by ancestors (notably
	// FlexLayoutAlgorithm for align-items:baseline).
	Baseline float64

	// HasBaseline indicates whether the Baseline value was explicitly set
	// during layout (true) vs defaulting to zero because no baseline source
	// existed (false). This distinction is needed for baseline synthesis:
	// a layout result with Baseline=0 and HasBaseline=true has a real baseline
	// at position 0 (e.g. line-height:0), while HasBaseline=false means no
	// baseline was found and the parent should synthesize one.
	HasBaseline bool

	// LastBaseline is the last baseline position in the block direction,
	// relative to the fragment's block-start edge. Used for inline-block
	// alignment per CSS 2.1 §10.8.1 (baseline of the last line box).
	LastBaseline float64

	// UseLastBaselineForInlineBaseline mirrors Blink's
	// use_last_baseline_for_inline_baseline_ — set by PropagateBaselineFromChild
	// to indicate the last-baseline should be preferred for inline alignment.
	UseLastBaselineForInlineBaseline bool

	// UnpositionedListMarker is a list-item marker that was not placed during
	// layout. Mirrors Blink's LayoutResult carrying the unpositioned marker so
	// the parent algorithm can set HasUnpositionedListMarker on the outgoing
	// BlockBreakToken.
	UnpositionedListMarker *UnpositionedListMarker

	// EndMarginStrut is the margin strut at the block-end of this fragment,
	// for margin collapsing propagation to the next sibling or parent.
	EndMarginStrut MarginStrut

	// ExclusionSpace is the updated float exclusion state after this
	// fragment's layout (including any floats it added).
	ExclusionSpace *ExclusionSpace

	// PropagatedTopMargin carries the first child's unresolved margin
	// for CSS 2.1 §8.3.1 parent-child top margin collapsing. When a
	// block has no block-start border or padding (and isn't a new BFC),
	// the first child's margin propagates upward.
	PropagatedTopMargin MarginStrut

	// PropagatedOOFCandidates are absolutely/fixed positioned children
	// that this non-positioned element cannot resolve. They propagate
	// upward to the nearest positioned ancestor or the root (ICB).
	// Mirrors Blink's approach where OOF candidates bubble up the tree
	// until they reach their actual containing block.
	PropagatedOOFCandidates []OutOfFlowCandidate

	// --- Fragmentation ---

	// BreakToken is non-nil if there is more content to lay out in
	// subsequent fragmentainers. Nil means all content fit.
	BreakToken *BlockBreakToken

	// MinSpaceShortage is the smallest overflow that caused a break.
	// Used by column balancing to determine how much to stretch.
	MinSpaceShortage float64

	// BlockSizeForFragmentation is the block-size the parent uses when
	// fitting this child into a fragmentainer: the furthest block-end of
	// the fragment's content, overflow included, and at least its own
	// block-size — a fragmentainer (column) records its children's extent
	// only. Recorded by BoxFragmentBuilder.Build when the fragment was laid
	// out with block fragmentation; 0 otherwise (Blink's kIndefiniteSize:
	// read the fragment's own block-size, see the free
	// BlockSizeForFragmentation). Mirrors Blink's
	// LayoutResult::BlockSizeForFragmentation (layout_result.cc:75-79 @
	// a9f50e522efa). One louis14 site overwrites it after Build (the
	// nested-spanner early return).
	BlockSizeForFragmentation float64

	// HasForcedBreak is true if this layout emitted a forced column/page break
	// (break-before:column, break-after:column, etc.). Mirrors Blink's
	// LayoutResult::HasForcedBreak() used by ColumnLayoutAlgorithm to count
	// forced breaks and decide when no soft-break opportunities remain.
	HasForcedBreak bool

	// DidBreakSelf is true when the block clamped its own border-box to fit
	// the fragmentainer's remaining space and emitted a continuation
	// BreakToken so the next fragmentainer resumes the same block. Mirrors
	// Blink's BoxFragmentBuilder::SetDidBreakSelf (fragmentation_utils.cc
	// FinishFragmentation): when a block's desired size exceeds space_left
	// inside a fragmentation context, the fragment is sized to space_left,
	// DidBreakSelf is set, and a BlockBreakToken with updated
	// ConsumedBlockSize is emitted — even if no inner child broke. The block
	// itself needs to be resumed.
	DidBreakSelf bool

	// TallestUnbreakableBlockSize is the maximum unbreakable block-size
	// observed during this layout pass — the tallest piece of monolithic
	// content or block with break-inside:avoid that this result placed (or
	// transitively contains via PropagateTallestUnbreakableBlockSize). The
	// outer multicol's initial column-balancing pass uses it as a floor when
	// determining the auto column block-size: columns must be at least as
	// tall as the tallest unbreakable child so that monolithic content
	// doesn't overflow visually. Mirrors Blink's
	// LayoutResult::tallest_unbreakable_block_size_ + the consumer in
	// ColumnLayoutAlgorithm at column_layout_algorithm.cc:1879-1948. Only
	// populated during space.IsInitialColumnBalancingPass; ignored
	// otherwise. Phase 16.d.2/3 (v2 B1+B2): the field lands in B1 as a
	// scaffold; B2 wires the propagation hooks in fragmentation_utils.go +
	// box_fragment_builder.go and the consumer at multicol_layout.go:1601.
	TallestUnbreakableBlockSize float64

	// BreakAppeal scores how appealing the break that produced this result
	// is. The multicol stretch loop demotes acceptance when any column's
	// result has a non-Perfect appeal (cla.cc:1019 / cla.cc:1034). Default
	// (zero value) is BreakAppealLastResort; layouts that didn't break set
	// this to BreakAppealPerfect. Mirrors Blink's LayoutResult::GetBreakAppeal.
	BreakAppeal BreakAppeal

	// ColumnSpannerPath is non-nil when layout of this box stopped at a
	// column-span:all element: its own child, or one inside an in-flow child
	// whose path it wraps (BoxFragmentBuilder.PropagateBreakInfo,
	// box_fragment_builder.cc:594-608 @ a9f50e522efa). The multicol
	// algorithm extracts the spanner via this path and lays it out at full
	// container width. Mirrors Blink's ColumnSpannerPath
	// (column_spanner_path.h) and LayoutResult::GetColumnSpannerPath().
	ColumnSpannerPath *ColumnSpannerPath

	// IsEmptySpannerParent is set when nothing in this fragment precedes the
	// spanner on ColumnSpannerPath (the spanner's parent resumed with no
	// children placed), so the column line holding it counts as empty and a
	// preceding spanner's margin collapses through it. Mirrors Blink's
	// LayoutResult::IsEmptySpannerParent (layout_result.h:219 @
	// a9f50e522efa).
	IsEmptySpannerParent bool

	// ShouldForceSameFragmentationFlow is set when this box contains a
	// column spanner: a break inside it stays in its parent's flow even when
	// the box itself is at its block end, so no parallel flow opens before
	// the spanner. Mirrors Blink's LayoutResult::
	// ShouldForceSameFragmentationFlow (layout_result.h:368-370 @
	// a9f50e522efa).
	ShouldForceSameFragmentationFlow bool

	// InitialBreakBefore is the break-before value propagated up from this
	// box's first in-flow child (joined with that child's own propagated
	// value), for the parent to join into the break-between value before
	// this box. "auto" when none was propagated. Mirrors Blink's
	// LayoutResult::InitialBreakBefore (layout_result.h:414 @ a9f50e522efa).
	InitialBreakBefore string

	// FinalBreakAfter is the break-after value propagated up from this box's
	// last in-flow child, for the parent to join into the break-between
	// value after this box. "auto" when none was propagated. Mirrors Blink's
	// LayoutResult::FinalBreakAfter (layout_result.h:420 @ a9f50e522efa).
	FinalBreakAfter string

	// Status is the outcome of the layout pass that produced this result.
	// Anything but LayoutResultSuccess means layout was aborted and there is
	// no fragment. Zero value is LayoutResultSuccess. Mirrors Blink's
	// LayoutResult::Status() (layout_result.h:239 @ a9f50e522efa).
	Status EStatus
}

// EStatus is the outcome of a layout pass. Mirrors Blink's
// LayoutResult::EStatus (layout_result.h:48-64 @ a9f50e522efa); values keep
// Blink's names with the owning class as prefix (LayoutResult::kSuccess →
// LayoutResultSuccess), as InlineItem::Type values are InlineItem*.
//
// louis14 produces only LayoutResultSuccess today. Blink's sole producer of
// LayoutResultOutOfFragmentainerSpace is InlineLayoutAlgorithm aborting a
// line whose <br clear> needs clearance past the fragmentainer
// (inline_layout_algorithm.cc:1501-1503, AddAnyClearanceAfterLine
// :1058-1061 @ a9f50e522efa); louis14 has no
// ExclusionSpace::NeedsClearancePastFragmentainer and does not route line
// boxes through BreakBeforeChildIfNeeded. LayoutResultNeedsEarlierBreak
// needs block-level early breaks, not ported (an inline formatting
// context's line early break relays out directly, relayoutAndBreakEarlier).
// The consumers of a non-success status (BreakBeforeChildIfNeeded,
// CalculateBreakAppealBefore) are ported so a producer can be added.
type EStatus int

const (
	LayoutResultSuccess EStatus = iota
	LayoutResultBfcBlockOffsetResolved
	LayoutResultNeedsEarlierBreak
	LayoutResultOutOfFragmentainerSpace
	LayoutResultNeedsLineClampRelayout
	LayoutResultDisableFragmentation
	LayoutResultNeedsRelayoutWithNoChildScrollbarChanges
	LayoutResultTextBoxTrimEndDidNotApply
	// LayoutResultAlgorithmSpecific1 is shared by mutually exclusive
	// algorithm-specific statuses (table rows, last table box, margin-trim),
	// exactly as Blink shares the value; louis14 has none of their
	// producers, so only the shared value is declared.
	LayoutResultAlgorithmSpecific1
)

// ColumnSpannerPath is a linked list identifying the path from the current
// node down to the first column-span:all element encountered during column
// layout. Mirrors Blink's ColumnSpannerPath (column_spanner_path.h).
type ColumnSpannerPath struct {
	// Box is the node at this level of the path — either the spanner itself
	// (for a direct column child) or an intermediate container.
	Box *LayoutInputNode
	// Child is the next level of the path toward the spanner (nil when Box
	// is the spanner itself).
	Child *ColumnSpannerPath
}

// FragmentType distinguishes box, line-box, and text fragments.
// Ported from Blink's PhysicalFragment::Type enum.
type FragmentType int

const (
	// FragmentBox is a CSS box (block, inline-block, etc.).
	FragmentBox FragmentType = iota
	// FragmentLineBox is a line box in an inline formatting context.
	FragmentLineBox
	// FragmentText is a text run within a line box.
	FragmentText
)

// BoxType distinguishes the structural role of a box fragment within
// the fragment tree. Mirrors Blink's PhysicalFragment::BoxType
// (physical_fragment.h). louis14 currently only models the subset
// needed for multicol fragmentation; additional values (kPageArea,
// kAtomicInline, etc.) can be added when pagination / replaced-inline
// work lands.
type BoxType int

const (
	// BoxTypeNormal is the default for ordinary block / inline-block
	// boxes. Mirrors Blink's kNormalBox.
	BoxTypeNormal BoxType = iota
	// BoxTypeColumn marks a fragmentainer fragment produced by
	// MulticolLayoutAlgorithm for one column of a multicol container.
	// Mirrors Blink's kColumnBox.
	BoxTypeColumn
)

// IsColumnBox reports whether this fragment is a multicol column
// fragmentainer. Mirrors Blink PhysicalFragment::IsColumnBox.
func (f *PhysicalFragment) IsColumnBox() bool {
	return f != nil && f.Type == FragmentBox && f.BoxType == BoxTypeColumn
}

// IsFirstForNode reports whether this is the first fragment generated for
// its node; false for a fragment resumed after a break inside the node. A
// nil fragment (aborted layout) counts as first. Mirrors Blink's
// PhysicalBoxFragment::IsFirstForNode (physical_box_fragment.h:439 @
// a9f50e522efa), copied from the builder (physical_box_fragment.cc:427).
func (f *PhysicalFragment) IsFirstForNode() bool {
	return f == nil || !f.notFirstForNode
}

// IsFragmentainerBox reports whether this fragment is a fragmentainer
// (currently only a multicol column box; pagination would extend this
// to kPageArea). Mirrors Blink PhysicalFragment::IsFragmentainerBox /
// IsFragmentainerBoxType.
func (f *PhysicalFragment) IsFragmentainerBox() bool {
	return f.IsColumnBox()
}

// IsCSSBox reports whether this fragment is a CSS box: not a line box, text
// run or fragmentainer. Mirrors Blink PhysicalFragment::IsCSSBox
// (physical_fragment.h:247 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// louis14 text runs are fragments where Blink's are fragment items.
func (f *PhysicalFragment) IsCSSBox() bool {
	return f.Type == FragmentBox && !f.IsFragmentainerBox()
}

// IsInlineBox reports whether this fragment is a non-atomic inline box
// (a span's box), which louis14 marks by its style rather than a box type.
// Mirrors Blink PhysicalFragment::IsInlineBox (physical_fragment.h:162-164
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (f *PhysicalFragment) IsInlineBox() bool {
	return f.Type == FragmentBox && isNonAtomicInline(f.Style, f.Node)
}

// isNonAtomicInline reports whether a box with style, generated by node, is
// a non-replaced inline box (display inline, ruby, ruby-text, or an inline
// list-item, Blink's LayoutInlineListItem).
func isNonAtomicInline(style *css.Style, node *html.Node) bool {
	return style != nil && (!IsTransformableBox(style, node) || style.IsInlineBoxListItem())
}

// ShouldApplyLayoutContainment mirrors Blink
// PhysicalFragment::ShouldApplyLayoutContainment (physical_fragment.h:
// 427-429 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (f *PhysicalFragment) ShouldApplyLayoutContainment() bool {
	return f.IsCSSBox() && f.Style != nil && f.Style.ShouldApplyLayoutContainment()
}

// OverflowClipAxes is the set of physical axes along which a box clips its
// overflow. Mirrors Blink's OverflowClipAxes
// (core/layout/overflow_model.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type OverflowClipAxes uint8

const (
	NoOverflowClip       OverflowClipAxes = 0
	OverflowClipX        OverflowClipAxes = 1 << 0
	OverflowClipY        OverflowClipAxes = 1 << 1
	OverflowClipBothAxis                  = OverflowClipX | OverflowClipY
)

// respectsCSSOverflow reports whether a box honors its overflow property:
// block containers and replaced boxes do, non-atomic inlines do not.
// Mirrors LayoutObject/LayoutBlock::RespectsCSSOverflow (layout_object.h:
// 583-586, layout_block.cc:166-170 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the viewport-defining
// element's propagated overflow is not modelled.
func respectsCSSOverflow(style *css.Style, node *html.Node) bool {
	return style != nil && !isNonAtomicInline(style, node)
}

// hasNonVisibleOverflow mirrors LayoutObject::HasNonVisibleOverflow as set
// by LayoutBox::UpdateFromStyle: some axis is not visible, or paint
// containment applies, on a box that respects overflow.
func hasNonVisibleOverflow(style *css.Style, node *html.Node) bool {
	if !respectsCSSOverflow(style, node) {
		return false
	}
	return style.GetOverflowX() != css.OverflowVisible ||
		style.GetOverflowY() != css.OverflowVisible ||
		style.ShouldApplyPaintContainment()
}

// isScrollContainerBox mirrors LayoutObject::IsScrollContainer
// (layout_object.h:1550-1561 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// replaced boxes never scroll.
func isScrollContainerBox(style *css.Style, node *html.Node) bool {
	if node != nil && IsReplacedElement(node) {
		return false
	}
	return hasNonVisibleOverflow(style, node) && style.IsScrollContainer()
}

// computeOverflowClipAxes returns the axes a box with style, generated by
// node, clips: both under paint containment or as a scroll container,
// otherwise each non-visible axis. Mirrors LayoutBox::ComputeOverflowClipAxes
// (layout_box.cc:3948-3962 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// HasControlClip is not modelled.
func computeOverflowClipAxes(style *css.Style, node *html.Node) OverflowClipAxes {
	if style == nil {
		return NoOverflowClip
	}
	if style.ShouldApplyPaintContainment() && respectsCSSOverflow(style, node) {
		return OverflowClipBothAxis
	}
	if !hasNonVisibleOverflow(style, node) {
		return NoOverflowClip
	}
	if isScrollContainerBox(style, node) {
		return OverflowClipBothAxis
	}
	axes := NoOverflowClip
	if style.GetOverflowX() != css.OverflowVisible {
		axes |= OverflowClipX
	}
	if style.GetOverflowY() != css.OverflowVisible {
		axes |= OverflowClipY
	}
	return axes
}

// GetOverflowClipAxes mirrors Blink PhysicalFragment::GetOverflowClipAxes
// (physical_fragment.h:389-394 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (f *PhysicalFragment) GetOverflowClipAxes() OverflowClipAxes {
	if !f.IsCSSBox() {
		return NoOverflowClip
	}
	return computeOverflowClipAxes(f.Style, f.Node)
}

// HasNonVisibleOverflow mirrors Blink PhysicalFragment::
// HasNonVisibleOverflow (a CSS box whose layout object has non-visible
// overflow).
func (f *PhysicalFragment) HasNonVisibleOverflow() bool {
	return f.IsCSSBox() && hasNonVisibleOverflow(f.Style, f.Node)
}

// ShouldClipOverflowAlongBothAxis mirrors Blink PhysicalFragment::
// ShouldClipOverflowAlongBothAxis (physical_fragment.h:435-437 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (f *PhysicalFragment) ShouldClipOverflowAlongBothAxis() bool {
	return f.GetOverflowClipAxes() == OverflowClipBothAxis
}

// ShouldApplyOverflowClipMargin reports whether overflow-clip-margin
// extends this box's clip: not a scroll container, a margin that has an
// effect, and overflow:clip on both axes (any non-visible value for a
// replaced box) or paint containment. Mirrors LayoutObject::
// ShouldApplyOverflowClipMargin (layout_object.h:588-618) and
// ComputedStyle::OverflowClipMarginHasAnEffect (computed_style.h:2642-2647
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (f *PhysicalFragment) ShouldApplyOverflowClipMargin() bool {
	style := f.Style
	if !f.IsCSSBox() || style == nil || isScrollContainerBox(style, f.Node) {
		return false
	}
	margin, ok := style.GetOverflowClipMargin()
	if !ok || (margin.Box == css.OverflowClipMarginPaddingBox && margin.Length == 0) {
		return false
	}
	x, y := style.GetOverflowX(), style.GetOverflowY()
	var isOverflowClip bool
	if f.Node != nil && IsReplacedElement(f.Node) {
		isOverflowClip = x != css.OverflowVisible && y != css.OverflowVisible
	} else {
		isOverflowClip = x == css.OverflowClip && y == css.OverflowClip
	}
	return (isOverflowClip || style.ShouldApplyPaintContainment()) && respectsCSSOverflow(style, f.Node)
}

// OverflowClipMarginOutsets returns the per-side outsets from the border
// box to the overflow-clip-margin clip edge: inset to the reference box,
// then out by the margin. Mirrors PhysicalBoxFragment::
// OverflowClipMarginOutsets (physical_box_fragment.cc:1764-1793 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); TruncateSides for fragmented
// boxes is not applied.
func (f *PhysicalFragment) OverflowClipMarginOutsets() PhysicalEdges {
	margin, _ := f.Style.GetOverflowClipMargin()
	var outsets PhysicalEdges
	if bd := f.BoxData; bd != nil {
		switch margin.Box {
		case css.OverflowClipMarginPaddingBox:
			outsets = PhysicalEdges{Top: -bd.Border.Top, Right: -bd.Border.Right, Bottom: -bd.Border.Bottom, Left: -bd.Border.Left}
		case css.OverflowClipMarginContentBox:
			outsets = PhysicalEdges{
				Top:    -bd.Border.Top - bd.Padding.Top,
				Right:  -bd.Border.Right - bd.Padding.Right,
				Bottom: -bd.Border.Bottom - bd.Padding.Bottom,
				Left:   -bd.Border.Left - bd.Padding.Left,
			}
		}
	}
	outsets.Top += margin.Length
	outsets.Right += margin.Length
	outsets.Bottom += margin.Length
	outsets.Left += margin.Length
	return outsets
}

// ScrollableOverflow returns this fragment's scrollable overflow in its
// border-box space: what ScrollableOverflowCalculator recorded, or the
// border box when none was recorded. Mirrors Blink PhysicalBoxFragment::
// ScrollableOverflow (physical_box_fragment.h:234-240 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (f *PhysicalFragment) ScrollableOverflow() geometry.PhysicalRect {
	if f.scrollableOverflow != nil {
		return *f.scrollableOverflow
	}
	return geometry.PhysicalRect{Size: f.Size}
}

// setScrollableOverflow records overflow, stored only when it differs from
// the border box as Blink stores it only then (physical_box_fragment.cc:
// 243-246 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (f *PhysicalFragment) setScrollableOverflow(overflow geometry.PhysicalRect) {
	if overflow == (geometry.PhysicalRect{Size: f.Size}) {
		f.scrollableOverflow = nil
		return
	}
	stored := overflow
	f.scrollableOverflow = &stored
}

// PhysicalFragment is an immutable positioned box in physical coordinates.
// This is the output of layout — the fragment tree that the renderer walks.
//
// Ported from Blink's PhysicalFragment (physical_fragment.h).
type PhysicalFragment struct {
	// Size is the border-box size in physical coordinates.
	Size geometry.PhysicalSize

	// Children are the positioned child fragments.
	Children []ChildLink

	// WritingDirection is the writing mode that produced this fragment.
	// Needed to read logical properties from the physical fragment.
	WritingDirection WritingDirectionMode

	// BoxData contains CSS box model data (margins, borders, padding).
	// Nil for line-box and text fragments.
	BoxData *PhysicalBoxData

	// Node is the DOM node that produced this fragment.
	// Set by the layout algorithm for the fragment→box bridge.
	// For text fragments, this is the parent element (for style lookup).
	Node *html.Node

	// Style is the computed style for this fragment's node.
	// Carried on the fragment so fragmentToBox doesn't need a style map.
	Style *css.Style

	// LayoutNode is the LayoutInputNode that produced this fragment.
	// Set for element-level boxes (not text, not anonymous).
	// Used by fragmentToBox to connect LayoutInputNode ↔ Box for DOM-ordered paint.
	LayoutNode *LayoutInputNode

	// PaintGroup identifies the inline paint group this fragment must
	// composite inside: the OpenTag InlineItem of the nearest enclosing
	// opacity-group inline (or the fragment's OWN inline, for a group span's
	// background fragments). Nil for fragments outside any group. Stamped by
	// inline layout (where the open-span stack is live) and copied to
	// Box.PaintGroup; pkg/render's buildPaintSubtree routes annotated boxes
	// into one PaintLayer per group node. See InlineItem.EnclosingPaintGroup.
	PaintGroup *InlineItem

	// Type distinguishes box, line-box, and text fragments.
	Type FragmentType

	// BoxType distinguishes the structural role of box fragments
	// (BoxTypeNormal, BoxTypeColumn). Only meaningful when Type ==
	// FragmentBox; ignored on line-box / text fragments. Default
	// (BoxTypeNormal) preserves prior behaviour. Mirrors Blink
	// PhysicalFragment::box_type_.
	BoxType BoxType

	// TextContent holds the rendered text for text fragments.
	TextContent string

	// BidiLevel is the resolved UAX#9 bidi embedding level for text fragments.
	// 0 = LTR, 1 = RTL. Used by the renderer to draw RTL text in visual order.
	BidiLevel int

	// RelativeOffset is the CSS position:relative offset for this fragment.
	// Computed during layout, applied at paint time (not baked into
	// fragment tree positions). Zero for non-relative elements.
	// Mirrors Blink's approach where relative offsets are paint properties.
	RelativeOffset geometry.PhysicalOffset

	// ClipContentToBorderBox forces the renderer to clip this fragment's
	// children to its border-box, independent of the CSS `overflow`
	// property. Set by the table layout algorithm on rowspan cells that
	// span a visibility:collapse row (CSS Tables 3 §5.4.1: "clip the
	// content to the table-cell's border-box"). Default is false;
	// normal overflow semantics apply.
	ClipContentToBorderBox bool

	// IsMulticolContainer marks this fragment as a multicol container
	// (CSS Multicol L1 §2). Mirrors the structural condition that makes
	// Blink's LayoutBox::HasNonVisibleOverflow() return true for
	// multicol — the fragmentation context establishes an overflow clip
	// even when the computed `overflow` is `visible`. Consumed by the
	// paint layer (P20.5) to set up an overflow clip with rect =
	// padding-box (Blink LayoutBox::OverflowClipRect default for
	// non-scroll-container: border-box contracted by border outsets).
	// Replaces the ad-hoc 3389efe7 mechanism that repurposed
	// ClipContentToBorderBox for multicol; that flag now retains its
	// CSS-Tables-3 §5.4.1 semantics only.
	IsMulticolContainer bool

	// IsMonolithic marks this fragment as unbreakable for column-layout
	// purposes — its block-size is fixed and its content does not
	// fragment across column or page boundaries. Mirrors Blink's
	// PhysicalFragment::IsMonolithic (consumed by ShouldAvoidBreakInside
	// in fragmentation_utils.h alongside the style-level
	// break-inside:avoid check).
	//
	// Phase 16.e+18 v2 B2.5 sources:
	//   - `contain: size` (CSS Containment 2 §2.6: a size-contained box
	//     has no intrinsic size from its descendants, so they're treated
	//     as monolithic for fragmentation).
	//   - (Future) replaced elements (img, video, canvas, iframe).
	//
	// Currently consumed only by `ShouldAvoidBreakInside`
	// (fragmentation_utils.go) for `TallestUnbreakableBlockSize`
	// propagation during the initial column-balancing pass.
	IsMonolithic bool

	// notFirstForNode is set when this box fragment resumes its node after a
	// break inside it (not its first fragment). Inverted so that every
	// fragment not built by a fragmenting algorithm (line boxes, text,
	// struct literals) reads as first-for-node, matching Blink's default
	// (is_first_for_node_ = true, box_fragment_builder.h:790 @
	// a9f50e522efa). Read through IsFirstForNode.
	notFirstForNode bool

	// RenderedColumnCount is the number of column fragments actually placed
	// in this multicol fragment (column-fill:auto may render fewer columns
	// than column-count when content fits in a subset). Zero for non-multicol
	// fragments. Used by the column-rule painter to honor the spec rule that
	// rules are only drawn between columns that both have content.
	RenderedColumnCount int

	// GapGeometry carries column-rule / row-rule geometry for multicol
	// containers. Nil for non-multicol fragments or when no gap rules apply.
	// Consumed by drawColumnRules in pkg/render/render.go.
	// Mirrors Blink's PhysicalBoxFragment::gap_geometry_ field.
	GapGeometry *GapGeometry

	// FragmentedOofData carries OOF-fragmentation state when this fragment
	// is a multicol container or other fragmentation-context root. Nil for
	// non-multicol fragments. Drained by `OutOfFlowLayoutPart` at the
	// outermost fragmentation root (a future Phase 25 commit). Phase 25
	// scaffolding — population in a subsequent commit. Mirrors Blink's
	// `PhysicalFragment::OofData` carried via `FragmentedOofData`
	// (oof_positioned_node.h:366-408).
	FragmentedOofData *FragmentedOofData

	// AppliedTextDecorations is an optional per-fragment override of the
	// style-carried decoration vector. Populated only for FragmentText runs
	// that participate in a multi-fragment decorating box (LOU-149 Phase 4),
	// so the painter can draw a continuous line across bidi-split, line-
	// wrapped, or nested-inline fragments. When nil, paint_layer falls back
	// to Style.GetAppliedTextDecorations() — preserving LOU-142 behavior for
	// single-fragment inlines. Mirrors Blink's InlinePaintContext per-paint
	// decoration list (inline_paint_context.h:20-26 @ SHA 4883d11f).
	AppliedTextDecorations []css.AppliedTextDecoration

	// CollapsedBorderOutwardExtension is set on border-collapse:collapse
	// table-cell fragments whose neighbor cell on a given physical side is
	// missing from the grid (cell removed, or no element-level border to
	// share at the table outer edge). For each side [top, right, bottom,
	// left] the value is the px of the collapsed border's "outside half"
	// — the spec-mandated paint that extends beyond the cell's border-box
	// per CSS 2.1 §17.6.3 / CSS Tables 3 §4.2.
	//
	// The cell's own drawBorders paints the inside half (half the winning
	// width, stored on the cloned cell style). When this field is
	// non-zero on side s, paintCollapsedTableCellBorder additionally
	// paints an outward strip of width [s] just outside the cell's
	// border-box on that side, so the combined visual is the full
	// collapsed border centered on the cell-edge grid line.
	//
	// Blink reference: TablePainter::PaintCollapsedBorders @
	// table_painters.cc:356-362 (Chromium SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Where louis14 differs —
	// Blink paints all collapsed borders at the table level from a grid;
	// louis14 paints them per cell — we recover the same painted extent
	// via this additive outward strip.
	//
	// Indexing matches drawBorders: [0]=top, [1]=right, [2]=bottom,
	// [3]=left. All zero on the common case (every neighbor exists or
	// border-collapse:separate).
	CollapsedBorderOutwardExtension [4]float64

	// scrollableOverflow is the scrollable overflow ScrollableOverflowCalculator
	// computed when the fragment was built, in its border-box space; nil
	// when it equals the border box or was not computed. Read through
	// ScrollableOverflow. Mirrors Blink PhysicalBoxFragment's
	// kScrollableOverflow rare field (physical_box_fragment.h:234-243 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	scrollableOverflow *geometry.PhysicalRect
}

// ChildLink is a positioned child within a parent fragment.
type ChildLink struct {
	Offset   geometry.PhysicalOffset
	Fragment *PhysicalFragment
}

// PhysicalBoxData stores the physical box model edges.
type PhysicalBoxData struct {
	Margin    PhysicalEdges
	Border    PhysicalEdges
	Padding   PhysicalEdges
	Scrollbar PhysicalEdges
}

// LogicalFragment is a read-only wrapper that presents a PhysicalFragment's
// data in logical coordinates. Layout algorithms use this to read child
// results in logical terms without knowing the physical axis.
//
// Ported from Blink's LogicalFragment (logical_fragment.h).
type LogicalFragment struct {
	fragment *PhysicalFragment
	wdm      WritingDirectionMode
}

// NewLogicalFragment wraps a physical fragment for logical access.
// The wdm should be the writing direction mode of the context reading
// the fragment (typically the parent's mode).
func NewLogicalFragment(wdm WritingDirectionMode, fragment *PhysicalFragment) LogicalFragment {
	return LogicalFragment{fragment: fragment, wdm: wdm}
}

// InlineSize returns the fragment's inline-size (width in HTB, height in vertical).
func (lf LogicalFragment) InlineSize() float64 {
	ls := ToLogicalSize(geomSizeToOld(lf.fragment.Size), lf.wdm.WM)
	return ls.InlineSize
}

// BlockSize returns the fragment's block-size (height in HTB, width in vertical).
func (lf LogicalFragment) BlockSize() float64 {
	ls := ToLogicalSize(geomSizeToOld(lf.fragment.Size), lf.wdm.WM)
	return ls.BlockSize
}

// BlockEndScrollableOverflow returns the block-end edge of the fragment's
// scrollable overflow, read in this wrapper's writing direction. Mirrors
// Blink LogicalBoxFragment::BlockEndScrollableOverflow
// (logical_box_fragment.cc:86-91 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (lf LogicalFragment) BlockEndScrollableOverflow() float64 {
	overflow := lf.fragment.ScrollableOverflow()
	size := geomSizeToOld(overflow.Size)
	conv := NewConverter(lf.wdm, geomSizeToOld(lf.fragment.Size))
	offset := conv.ToLogicalOffset(PhysicalOffset{X: overflow.Offset.LeftF64(), Y: overflow.Offset.TopF64()}, size)
	return offset.BlockOffset + ToLogicalSize(size, lf.wdm.WM).BlockSize
}

// MarginStrut tracks pending margins for CSS 2.1 §8.3.1 margin collapsing.
//
// Margins don't resolve immediately — they accumulate as a "strut" that
// collapses with adjacent margins. The strut carries the largest positive
// and most negative margin seen so far.
type MarginStrut struct {
	PositiveMargin float64
	NegativeMargin float64 // stored as a negative value (e.g., -10)

	// DiscardMargins discards every margin adjoining this strut: Resolve()
	// returns 0 and IsEmpty() reports true whatever was appended. Set when a
	// container resumes after an unforced fragmentainer break, where margins
	// adjoining the fragmentainer start are truncated. Mirrors Blink's
	// MarginStrut::discard_margins (geometry/margin_strut.h:29-30, Sum()
	// :40-44, IsEmpty() margin_strut.cc:32-36 @ a9f50e522efa).
	DiscardMargins bool
}

// Append adds a margin value to the strut.
func (ms *MarginStrut) Append(margin float64) {
	if margin > 0 {
		if margin > ms.PositiveMargin {
			ms.PositiveMargin = margin
		}
	} else if margin < 0 {
		if margin < ms.NegativeMargin {
			ms.NegativeMargin = margin
		}
	}
}

// Resolve returns the collapsed margin value.
// CSS 2.1 §8.3.1: collapsed margin = max(positives) + min(negatives).
func (ms MarginStrut) Resolve() float64 {
	if ms.DiscardMargins {
		return 0
	}
	return ms.PositiveMargin + ms.NegativeMargin
}

// IsEmpty returns true if no margins have been appended.
func (ms MarginStrut) IsEmpty() bool {
	if ms.DiscardMargins {
		return true
	}
	return ms.PositiveMargin == 0 && ms.NegativeMargin == 0
}

// AppendStrut merges another strut into this one, keeping the max positive
// and most negative margins from both; a discarding strut makes the merge
// discard too.
func (ms *MarginStrut) AppendStrut(other MarginStrut) {
	ms.DiscardMargins = ms.DiscardMargins || other.DiscardMargins
	if other.PositiveMargin > ms.PositiveMargin {
		ms.PositiveMargin = other.PositiveMargin
	}
	if other.NegativeMargin < ms.NegativeMargin {
		ms.NegativeMargin = other.NegativeMargin
	}
}
