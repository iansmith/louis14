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

	// BlockSizeForFragmentation is the size the parent should use when
	// deciding whether to break before this child in a fragmentation
	// context, IF this value is > 0. Defaults to 0 (use the fragment's
	// own block-size). Mirrors Blink's LayoutResult::BlockSizeForFragmentation
	// — a multicol container with column-fill:auto + explicit height that
	// can't fit its required column block-size in the remaining outer
	// fragmentainer space sets this to its declared height, signaling the
	// parent's BreakBeforeChildIfNeeded to push the entire multicol to the
	// next outer fragmentainer instead of producing a clipped fragment.
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

	// ColumnSpannerPath is non-nil when a column-span:all element was
	// encountered and the algorithm returned early without laying it out.
	// The multicol algorithm extracts the spanner via this path and lays it
	// out at full container width. Mirrors Blink's ColumnSpannerPath
	// (column_spanner_path.h) and LayoutResult::GetColumnSpannerPath().
	ColumnSpannerPath *ColumnSpannerPath
}

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

// IsFragmentainerBox reports whether this fragment is a fragmentainer
// (currently only a multicol column box; pagination would extend this
// to kPageArea). Mirrors Blink PhysicalFragment::IsFragmentainerBox /
// IsFragmentainerBoxType.
func (f *PhysicalFragment) IsFragmentainerBox() bool {
	return f.IsColumnBox()
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
	//   - Spanners whose explicit declared height is less than their
	//     measured content height (implicit monolithic at the column-
	//     balancing level — the spanner's box won't grow to fit content,
	//     but the column box must accommodate the content so the
	//     overflow doesn't visually cross column boundaries).
	//   - (Future) replaced elements (img, video, canvas, iframe).
	//
	// Currently consumed only by `ShouldAvoidBreakInside`
	// (fragmentation_utils.go) for `TallestUnbreakableBlockSize`
	// propagation during the initial column-balancing pass.
	IsMonolithic bool

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

// MarginStrut tracks pending margins for CSS 2.1 §8.3.1 margin collapsing.
//
// Margins don't resolve immediately — they accumulate as a "strut" that
// collapses with adjacent margins. The strut carries the largest positive
// and most negative margin seen so far.
type MarginStrut struct {
	PositiveMargin float64
	NegativeMargin float64 // stored as a negative value (e.g., -10)
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
	return ms.PositiveMargin + ms.NegativeMargin
}

// IsEmpty returns true if no margins have been appended.
func (ms MarginStrut) IsEmpty() bool {
	return ms.PositiveMargin == 0 && ms.NegativeMargin == 0
}

// AppendStrut merges another strut into this one, keeping the max positive
// and most negative margins from both.
func (ms *MarginStrut) AppendStrut(other MarginStrut) {
	if other.PositiveMargin > ms.PositiveMargin {
		ms.PositiveMargin = other.PositiveMargin
	}
	if other.NegativeMargin < ms.NegativeMargin {
		ms.NegativeMargin = other.NegativeMargin
	}
}
