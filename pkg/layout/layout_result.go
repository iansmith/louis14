package layout

import "louis14/pkg/html"

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

	// EndMarginStrut is the margin strut at the block-end of this fragment,
	// for margin collapsing propagation to the next sibling or parent.
	EndMarginStrut MarginStrut

	// ExclusionSpace is the updated float exclusion state after this
	// fragment's layout (including any floats it added).
	ExclusionSpace *ExclusionSpace
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

	// Type distinguishes box, line-box, and text fragments.
	Type FragmentType

	// TextContent holds the rendered text for text fragments.
	TextContent string
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
