package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
)

// ScrollableOverflowCalculator determines a fragment's scrollable overflow
// (also known as layout overflow), CSS Overflow 3 §2.2
// (https://drafts.csswg.org/css-overflow-3/#scrollable), in the fragment's
// border-box physical coordinate space. Mirrors Blink's
// ScrollableOverflowCalculator (core/layout/scrollable_overflow_calculator.h:
// 22-100, .cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); BoxFragmentBuilder.
// Build runs it (updateScrollableOverflow) and stores the result on the
// fragment, as PhysicalBoxFragment::Create does (physical_box_fragment.cc:
// 213-240 @ a9f50e522efa).
//
// Not ported (louis14 has no model for them yet):
//   - inflow bounds (BoxFragmentBuilder::inflow_bounds_,
//     box_fragment_builder.cc:283-369 @ a9f50e522efa): a scroll
//     container's in-flow children plus its end padding do not extend
//     the overflow, so Result takes no inflow bounds.
//   - is_view_ (fixed-position children of the LayoutView): louis14 builds
//     no view fragment.
//   - IsOverscrollAreaParent (scrolls_all_directions_), the
//     LayoutFlexibleBox HasTopOverflow/HasLeftOverflow overrides
//     (flex/layout_flexible_box.cc:48-54 @ a9f50e522efa), line-box hanging
//     (AdjustOverflowForHanging; louis14 has no hanging-punctuation),
//     LayoutTextCombine, IsHiddenForPaint, ::view-transition children,
//     AddTableSelfRect (collapsed borders), and the out-of-flow clip clamp
//     under block fragmentation (scrollable_overflow_calculator.cc:302-333
//     @ a9f50e522efa), which walks the containing-block chain louis14
//     fragments do not carry.
type ScrollableOverflowCalculator struct {
	writingDirection  WritingDirectionMode
	isScrollContainer bool
	hasLeftOverflow   bool
	hasTopOverflow    bool

	paddingRect        geometry.PhysicalRect
	scrollableOverflow geometry.PhysicalRect
}

// NewScrollableOverflowCalculator starts the calculation for a fragment of
// node with the given physical borders, scrollbar gutters, padding and
// border-box size; the overflow starts as the padding rect. isCSSBox is
// false for a fragmentainer, which is never a scroll container. Mirrors
// ScrollableOverflowCalculator's constructor (scrollable_overflow_
// calculator.cc:71-101 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func NewScrollableOverflowCalculator(node *LayoutInputNode, isCSSBox bool, borders, scrollbar PhysicalEdges, size geometry.PhysicalSize, writingDirection WritingDirectionMode) ScrollableOverflowCalculator {
	c := ScrollableOverflowCalculator{writingDirection: writingDirection}
	if isCSSBox && node != nil {
		c.isScrollContainer = isScrollContainerBox(node.Style(), node.DOMNode)
		c.hasLeftOverflow = hasLeftOverflow(writingDirection)
		c.hasTopOverflow = hasTopOverflow(writingDirection)
	}
	left := borders.Left + scrollbar.Left
	top := borders.Top + scrollbar.Top
	c.paddingRect = geometry.PhysicalRect{
		Offset: geometry.PhysicalOffsetFromF64Round(left, top),
		Size: geometry.PhysicalSizeFromF64Round(
			max(0, size.WidthF64()-left-borders.Right-scrollbar.Right),
			max(0, size.HeightF64()-top-borders.Bottom-scrollbar.Bottom)),
	}
	c.scrollableOverflow = c.paddingRect
	return c
}

// Result returns the scrollable overflow gathered so far. Mirrors
// ScrollableOverflowCalculator::Result (scrollable_overflow_calculator.cc:
// 103-120 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) without inflow
// bounds (see the type doc).
func (c *ScrollableOverflowCalculator) Result() geometry.PhysicalRect {
	return c.scrollableOverflow
}

// AddChild adds the scrollable overflow of child, placed at offset from this
// fragment's border-box origin. A line box contributes as Blink's fragment
// items do (louis14 keeps line boxes and their runs as child fragments
// rather than a flat item list). Mirrors ScrollableOverflowCalculator::
// AddChild (scrollable_overflow_calculator.h:44-52 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (c *ScrollableOverflowCalculator) AddChild(child *PhysicalFragment, offset geometry.PhysicalOffset) {
	switch child.Type {
	case FragmentLineBox:
		c.addLineBoxItems(child, offset)
	case FragmentText:
		c.addOverflow(geometry.PhysicalRect{Offset: offset, Size: child.Size}, false)
	default:
		overflow := c.scrollableOverflowForPropagation(child)
		overflow.Offset = overflow.Offset.Add(offset)
		c.addOverflow(overflow, child.IsFragmentainerBox())
	}
}

// addLineBoxItems adds a line box placed at offset: its own rect (not
// adjusted for the scroll origin), then each run in it — text by its rect,
// boxes by their propagated overflow — at its position in this fragment,
// moved by its relative offset. Mirrors ScrollableOverflowCalculator::
// AddItemsInternal (scrollable_overflow_calculator.cc:126-182 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), whose items carry container-
// relative offsets with relative positioning applied.
func (c *ScrollableOverflowCalculator) addLineBoxItems(line *PhysicalFragment, offset geometry.PhysicalOffset) {
	if lineRect := (geometry.PhysicalRect{Offset: offset, Size: line.Size}); !lineRect.IsEmpty() {
		c.scrollableOverflow = c.scrollableOverflow.UniteEvenIfEmpty(lineRect)
	}
	for _, item := range line.Children {
		if item.Fragment == nil {
			continue
		}
		c.AddChild(item.Fragment, offset.Add(item.Offset).Add(item.Fragment.RelativeOffset))
	}
}

// addOverflow unites a child's overflow, first clamped to the scroll origin
// when this is a scroll container. An empty overflow is skipped unless it
// is a fragmentainer's (a column gap can overflow on its own). Mirrors
// ScrollableOverflowCalculator::AddOverflow (scrollable_overflow_
// calculator.h:72-81 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (c *ScrollableOverflowCalculator) addOverflow(childOverflow geometry.PhysicalRect, childIsFragmentainer bool) {
	if c.isScrollContainer {
		childOverflow = c.adjustOverflowForScrollOrigin(childOverflow)
	}
	if !childOverflow.IsEmpty() || childIsFragmentainer {
		c.scrollableOverflow = c.scrollableOverflow.UniteEvenIfEmpty(childOverflow)
	}
}

// adjustOverflowForScrollOrigin clips overflow that lies before the scroll
// origin: a scroll container cannot scroll to content left of its padding
// box unless it has left overflow (RTL / vertical-rl), nor above it unless
// it has top overflow. Mirrors ScrollableOverflowCalculator::
// AdjustOverflowForScrollOrigin (scrollable_overflow_calculator.cc:214-238
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), scrolls_all_directions_
// false.
func (c *ScrollableOverflowCalculator) adjustOverflowForScrollOrigin(overflow geometry.PhysicalRect) geometry.PhysicalRect {
	pr := c.paddingRect
	var left, right, top, bottom layoutunit.LayoutUnit
	if c.hasLeftOverflow {
		left = layoutunit.Min(pr.Right(), overflow.Offset.Left)
		right = layoutunit.Min(pr.Right(), overflow.Right())
	} else {
		left = layoutunit.Max(pr.Offset.Left, overflow.Offset.Left)
		right = layoutunit.Max(pr.Offset.Left, overflow.Right())
	}
	if c.hasTopOverflow {
		top = layoutunit.Min(pr.Bottom(), overflow.Offset.Top)
		bottom = layoutunit.Min(pr.Bottom(), overflow.Bottom())
	} else {
		top = layoutunit.Max(pr.Offset.Top, overflow.Offset.Top)
		bottom = layoutunit.Max(pr.Offset.Top, overflow.Bottom())
	}
	return geometry.PhysicalRect{
		Offset: geometry.PhysicalOffset{Left: left, Top: top},
		Size:   geometry.PhysicalSize{Width: right.Sub(left), Height: bottom.Sub(top)},
	}
}

// scrollableOverflowForPropagation is the overflow child contributes to its
// container, in child's border-box space: a non-CSS box (fragmentainer)
// passes its own scrollable overflow through; a CSS box contributes its
// border box, united with its own scrollable overflow unless it has layout
// containment, is an inline box, or clips both axes without
// overflow-clip-margin — clipped to its border box per clipped axis, or to
// the overflow-clip-margin rect — then mapped through its transform.
// Mirrors ScrollableOverflowCalculator::ScrollableOverflowForPropagation
// (scrollable_overflow_calculator.cc:240-300 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (c *ScrollableOverflowCalculator) scrollableOverflowForPropagation(child *PhysicalFragment) geometry.PhysicalRect {
	if !child.IsCSSBox() {
		return child.ScrollableOverflow()
	}
	overflow := geometry.PhysicalRect{Size: child.Size}
	// For a CSS box, a non-empty clip-axis set is exactly
	// HasNonVisibleOverflow, and both axes is ShouldClipOverflowAlongBothAxis.
	axes := child.GetOverflowClipAxes()
	applyClipMargin := child.ShouldApplyOverflowClipMargin()
	ignoreScrollableOverflow := child.ShouldApplyLayoutContainment() ||
		child.IsInlineBox() ||
		(axes == OverflowClipBothAxis && !applyClipMargin)
	if !ignoreScrollableOverflow {
		childOverflow := child.ScrollableOverflow()
		if axes != NoOverflowClip {
			if applyClipMargin {
				o := child.OverflowClipMarginOutsets()
				clipRect := geometry.PhysicalRect{Size: child.Size}.ExpandEdges(
					layoutunit.FromFloat64Round(o.Top), layoutunit.FromFloat64Round(o.Right),
					layoutunit.FromFloat64Round(o.Bottom), layoutunit.FromFloat64Round(o.Left))
				childOverflow = childOverflow.Intersect(clipRect)
			} else {
				if axes&OverflowClipX != 0 {
					childOverflow.Offset.Left = layoutunit.Zero
					childOverflow.Size.Width = child.Size.Width
				}
				if axes&OverflowClipY != 0 {
					childOverflow.Offset.Top = layoutunit.Zero
					childOverflow.Size.Height = child.Size.Height
				}
			}
		}
		overflow = overflow.UniteEvenIfEmpty(childOverflow)
	}
	if transform, ok := transformForChildFragment(child); ok {
		overflow = geometry.PhysicalRectEnclosingRect(transform.MapRect(overflow.ToRectF()))
	}
	return overflow
}

// transformForChildFragment returns the transform child's overflow is
// mapped through into its container: its transform operations about its
// transform-origin, relative to its border box. Mirrors
// GetTransformForChildFragment (core/layout/transform_utils.cc:51-84 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for a container without
// `perspective`; the container's perspective, and the perspective and z
// components of 3D operations, are not applied (css.ComposeAffineProjection
// flattens to 2D, as the paint path's flat branch does).
func transformForChildFragment(child *PhysicalFragment) (geometry.AffineTransform, bool) {
	style := child.Style
	if style == nil || !IsTransformableBox(style, child.Node) {
		return geometry.AffineTransform{}, false
	}
	w, h := child.Size.WidthF64(), child.Size.HeightF64()
	ops := style.ResolvedTransformOperations(w, h)
	if len(ops) == 0 {
		return geometry.AffineTransform{}, false
	}
	ox, oy := style.ResolveTransformOriginPx(w, h)
	a, b, cc, d, e, f := css.ComposeAffineProjection(ops)
	return geometry.Translate(ox, oy).
		Compose(geometry.NewAffineTransform(a, b, cc, d, e, f)).
		Compose(geometry.Translate(-ox, -oy)), true
}

// hasTopOverflow reports whether a box in writingDirection scrolls upward
// from its scroll origin. Mirrors LayoutBox::HasTopOverflow
// (layout_box.cc:3222-3238 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func hasTopOverflow(wdm WritingDirectionMode) bool {
	switch wdm.WM {
	case WritingModeSidewaysLR:
		return wdm.IsLTR()
	case WritingModeVerticalLR, WritingModeVerticalRL, WritingModeSidewaysRL:
		return !wdm.IsLTR()
	}
	return false
}

// hasLeftOverflow reports whether a box in writingDirection scrolls leftward
// from its scroll origin. Mirrors LayoutBox::HasLeftOverflow
// (layout_box.cc:3240-3256 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func hasLeftOverflow(wdm WritingDirectionMode) bool {
	if wdm.IsHorizontal() {
		return wdm.IsRTL()
	}
	return wdm.IsFlippedBlocks()
}

// updateScrollableOverflow computes and records the fragment's scrollable
// overflow from its children, when it is a fragment of a node (not a line
// box or run) that is not replaced content (BoxFragmentBuilder::
// ShouldCalculateScrollableOverflow, box_fragment_builder.h:749-757 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; louis14 has no paginated
// root). louis14 child offsets are content-box relative and exclude
// relative positioning; Blink's are border-box relative with it applied
// (box_fragment_builder.cc:262-267 @ a9f50e522efa), so both are added back.
// Mirrors PhysicalBoxFragment::Create (physical_box_fragment.cc:213-240 @
// a9f50e522efa). Table layout calls it again after resizing a cell and
// shifting its content for vertical-align once built (no Blink analog:
// Blink's cell algorithm builds the final geometry once).
func (f *PhysicalFragment) updateScrollableOverflow() {
	if f.LayoutNode == nil || (f.Node != nil && IsReplacedElement(f.Node)) {
		return
	}
	var borders, padding, scrollbar PhysicalEdges
	if bd := f.BoxData; bd != nil {
		borders, padding, scrollbar = bd.Border, bd.Padding, bd.Scrollbar
	}
	calculator := NewScrollableOverflowCalculator(f.LayoutNode, !f.IsFragmentainerBox(), borders, scrollbar, f.Size, f.WritingDirection)
	contentOrigin := geometry.PhysicalOffsetFromF64Round(borders.Left+padding.Left, borders.Top+padding.Top)
	for _, child := range f.Children {
		if child.Fragment == nil {
			continue
		}
		calculator.AddChild(child.Fragment, contentOrigin.Add(child.Offset).Add(child.Fragment.RelativeOffset))
	}
	f.setScrollableOverflow(calculator.Result())
}
