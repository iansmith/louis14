package layout

import (
	"louis14/pkg/css"
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
	// relative to the fragment's block-start edge. Used for alignment.
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

	// HasForcedBreak is true if this layout emitted a forced column/page break
	// (break-before:column, break-after:column, etc.). Mirrors Blink's
	// LayoutResult::HasForcedBreak() used by ColumnLayoutAlgorithm to count
	// forced breaks and decide when no soft-break opportunities remain.
	HasForcedBreak bool

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

// PhysicalFragment is an immutable positioned box in physical coordinates.
// This is the output of layout — the fragment tree that the renderer walks.
//
// Ported from Blink's PhysicalFragment (physical_fragment.h).
type PhysicalFragment struct {
	// Size is the border-box size in physical coordinates.
	Size PhysicalSize

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

	// TextContent holds the rendered text for text fragments.
	TextContent string

	// BidiLevel is the resolved UAX#9 bidi embedding level for text fragments.
	// 0 = LTR, 1 = RTL. Used by the renderer to draw RTL text in visual order.
	BidiLevel int

	// RelativeOffset is the CSS position:relative offset for this fragment.
	// Computed during layout, applied at paint time (not baked into
	// fragment tree positions). Zero for non-relative elements.
	// Mirrors Blink's approach where relative offsets are paint properties.
	RelativeOffset PhysicalOffset

	// ClipContentToBorderBox forces the renderer to clip this fragment's
	// children to its border-box, independent of the CSS `overflow`
	// property. Set by the table layout algorithm on rowspan cells that
	// span a visibility:collapse row (CSS Tables 3 §5.4.1: "clip the
	// content to the table-cell's border-box"). Default is false;
	// normal overflow semantics apply.
	ClipContentToBorderBox bool
}

// ChildLink is a positioned child within a parent fragment.
type ChildLink struct {
	Offset   PhysicalOffset
	Fragment *PhysicalFragment
}

// PhysicalBoxData stores the physical box model edges.
type PhysicalBoxData struct {
	Margin  PhysicalEdges
	Border  PhysicalEdges
	Padding PhysicalEdges
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
	ls := ToLogicalSize(lf.fragment.Size, lf.wdm.WM)
	return ls.InlineSize
}

// BlockSize returns the fragment's block-size (height in HTB, width in vertical).
func (lf LogicalFragment) BlockSize() float64 {
	ls := ToLogicalSize(lf.fragment.Size, lf.wdm.WM)
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
