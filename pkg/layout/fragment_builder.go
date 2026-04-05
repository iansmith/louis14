package layout

import (
	"louis14/pkg/css"
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
	baseline float64

	// lastBaseline position (last baseline, for inline-block alignment).
	lastBaseline float64

	// exclusionSpace after layout.
	exclusionSpace *ExclusionSpace

	// outOfFlowCandidates collects abs-pos/fixed children for deferred layout.
	outOfFlowCandidates []OutOfFlowCandidate
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
}

// SetLastBaseline sets the last baseline position.
func (b *BoxFragmentBuilder) SetLastBaseline(v float64) {
	b.lastBaseline = v
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
func (b *BoxFragmentBuilder) SetLayoutNode(lin *LayoutInputNode) {
	b.node = lin.DOMNode
	b.style = lin.Style()
	b.layoutNode = lin
}

// AddOutOfFlowCandidate records an absolutely/fixed positioned child
// for deferred layout by OutOfFlowLayoutPart.
func (b *BoxFragmentBuilder) AddOutOfFlowCandidate(c OutOfFlowCandidate) {
	b.outOfFlowCandidates = append(b.outOfFlowCandidates, c)
}

// AddChild adds a child fragment at the given logical offset.
// The offset is relative to this fragment's content box origin.
func (b *BoxFragmentBuilder) AddChild(fragment *PhysicalFragment, offset LogicalOffset) {
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
		childPhysSize := child.fragment.Size
		physChildren[i] = ChildLink{
			Offset:   conv.ToPhysicalOffset(child.offset, childPhysSize),
			Fragment: child.fragment,
		}
	}

	fragment := &PhysicalFragment{
		Size:             physSize,
		Children:         physChildren,
		WritingDirection: b.wdm,
		BoxData:          b.boxData,
		Node:             b.node,
		Style:            b.style,
		LayoutNode:       b.layoutNode,
	}

	return &LayoutResult{
		Fragment:           fragment,
		IntrinsicBlockSize: b.intrinsicBlockSize,
		Baseline:           b.baseline,
		LastBaseline:       b.lastBaseline,
		EndMarginStrut:     b.endMarginStrut,
		ExclusionSpace:     b.exclusionSpace,
	}
}
