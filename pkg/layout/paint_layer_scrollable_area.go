package layout

import (
	"math"

	"louis14/pkg/geometry"
	"louis14/pkg/html"
)

// PaintLayerScrollableArea is the scroll range of a scroll container
// fragment: its scroll origin and the minimum/maximum scroll offsets derived
// from its scrollable overflow. Mirrors the parts of Blink's
// PaintLayerScrollableArea (core/paint/paint_layer_scrollable_area.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) that position scrolled
// contents.
//
// Blink keeps the area on the LayoutBox's PaintLayer and lets it outlive
// layout; louis14 keeps the persistent scroll offset on the element
// (html.Node.ScrollOffset) and builds the area from the fragment each time
// fragmentToBox converts it, which re-clamps the offset against the new
// overflow as ClampScrollOffsetAfterOverflowChange does (paint_layer_
// scrollable_area.cc:1209-1245 @ a9f50e522efa). It lives in pkg/layout,
// not pkg/render (louis14's core/paint), because the scroll translation is
// applied while fragments become the absolutely positioned box tree, which
// precedes sticky offsetting and paint.
//
// The LayoutView's area (newLayoutViewScrollableArea) has no fragment:
// louis14 builds no view fragment, so the view's scrollable overflow and
// client size are given, and its scroll offset is kept on the Document
// node (html.Document.Root), the LayoutView's node.
//
// Not ported: scrollbars as widgets (including the viewport's), snapping,
// scroll anchoring and the root scroller.
type PaintLayerScrollableArea struct {
	// fragment is the scroll container's fragment; nil for the LayoutView.
	fragment *PhysicalFragment
	// overflowRect is overflow_rect_: the scrollable overflow in border-box
	// space, grown to at least the client size.
	overflowRect geometry.PhysicalRect
	// scrollOriginX/Y is scroll_origin_: the position of the initial scroll
	// offset (0,0) within the scrollable overflow, measured from its
	// left/top edge — nonzero when content overflows leftward or upward.
	scrollOriginX, scrollOriginY float64
	clientWidth, clientHeight    float64
}

// NewPaintLayerScrollableArea returns the scrollable area of fragment, or
// nil if fragment is not a scroll container (only a scroll container has
// one, LayoutBox::GetScrollableArea).
func NewPaintLayerScrollableArea(fragment *PhysicalFragment) *PaintLayerScrollableArea {
	if fragment == nil || !fragment.IsCSSBox() || fragment.Style == nil ||
		!isScrollContainerBox(fragment.Style, fragment.Node) {
		return nil
	}
	a := &PaintLayerScrollableArea{fragment: fragment}
	a.updateScrollDimensions()
	return a
}

// borderScrollbarOutsets returns the physical border plus scrollbar gutter
// on each side (BorderOutsets() + ComputeScrollbars()).
func (a *PaintLayerScrollableArea) borderScrollbarOutsets() (border, scrollbar PhysicalEdges) {
	if bd := a.fragment.BoxData; bd != nil {
		return bd.Border, bd.Scrollbar
	}
	return PhysicalEdges{}, PhysicalEdges{}
}

// updateScrollDimensions mirrors PaintLayerScrollableArea::
// UpdateScrollDimensions (paint_layer_scrollable_area.cc:940-954 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the overflow rect is the
// box's scrollable overflow united with the client size at the overflow's
// offset, then the scroll origin follows from it.
func (a *PaintLayerScrollableArea) updateScrollDimensions() {
	border, scrollbar := a.borderScrollbarOutsets()
	size := a.fragment.Size
	// LayoutContentRect(kExcludeScrollbars), paint_layer_scrollable_area.cc:
	// 653-667 @ a9f50e522efa.
	a.clientWidth = max(0, size.WidthF64()-border.Left-border.Right-scrollbar.Left-scrollbar.Right)
	a.clientHeight = max(0, size.HeightF64()-border.Top-border.Bottom-scrollbar.Top-scrollbar.Bottom)
	a.setScrollableOverflow(a.fragment.ScrollableOverflow(), border, scrollbar)
}

// setScrollableOverflow is the tail of UpdateScrollDimensions: the overflow
// rect is overflow united with the client size at the overflow's offset,
// then the scroll origin follows from it.
func (a *PaintLayerScrollableArea) setScrollableOverflow(overflow geometry.PhysicalRect, border, scrollbar PhysicalEdges) {
	client := geometry.PhysicalRect{
		Offset: overflow.Offset,
		Size:   geometry.PhysicalSizeFromF64Round(a.clientWidth, a.clientHeight),
	}
	a.overflowRect = overflow.UniteEvenIfEmpty(client)
	a.updateScrollOrigin(border, scrollbar)
}

// newLayoutViewScrollableArea returns the LayoutView's scrollable area: a
// viewport of width x height (the view has no border, and louis14 paints
// no viewport scrollbars) over the view's scrollable overflow — louis14's
// stand-in for UpdateScrollDimensions on the view's own fragment
// (paint_layer_scrollable_area.cc:940-954 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), whose ScrollableOverflowRect
// is LayoutView::DocumentRect (layout/layout_view.cc:801-804).
func newLayoutViewScrollableArea(width, height float64, overflow geometry.PhysicalRect) *PaintLayerScrollableArea {
	a := &PaintLayerScrollableArea{clientWidth: width, clientHeight: height}
	a.setScrollableOverflow(overflow, PhysicalEdges{}, PhysicalEdges{})
	return a
}

// updateScrollOrigin mirrors PaintLayerScrollableArea::UpdateScrollOrigin
// (paint_layer_scrollable_area.cc:927-938 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the floored distance from the
// overflow rect's left/top to the padding box, plus the left/top scrollbar
// (LayoutBox::OriginAdjustmentForScrollbars, layout_box.cc:1766-1773 @
// a9f50e522efa).
func (a *PaintLayerScrollableArea) updateScrollOrigin(border, scrollbar PhysicalEdges) {
	left := a.overflowRect.Offset.LeftF64() - border.Left
	top := a.overflowRect.Offset.TopF64() - border.Top
	a.scrollOriginX = math.Floor(-left) + math.Trunc(scrollbar.Left)
	a.scrollOriginY = math.Floor(-top) + math.Trunc(scrollbar.Top)
}

// scrollableAxes mirrors PaintLayerScrollableArea::ScrollableAxes
// (paint_layer_scrollable_area.cc:2484-2508 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the LayoutView always
// scrolls both axes; another box scrolls an axis whose overflow value is
// scrollable.
func (a *PaintLayerScrollableArea) scrollableAxes() (horizontal, vertical bool) {
	if a.fragment == nil {
		return true, true
	}
	return a.fragment.Style.IsOverflowValueScrollableX(), a.fragment.Style.IsOverflowValueScrollableY()
}

// clampNonScrollableAxesOffsets zeroes the offset on an axis that cannot
// scroll. Mirrors PaintLayerScrollableArea::ClampNonScrollableAxesOffsets
// (paint_layer_scrollable_area.cc:3750-3760 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (a *PaintLayerScrollableArea) clampNonScrollableAxesOffsets(x, y float64) (float64, float64) {
	horizontal, vertical := a.scrollableAxes()
	if !horizontal {
		x = 0
	}
	if !vertical {
		y = 0
	}
	return x, y
}

// computeScrollableSize mirrors PaintLayerScrollableArea::
// ComputeScrollableSize (paint_layer_scrollable_area.cc:3714-3748 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the pixel-snapped contents
// size (at least the visible size) minus the visible (client) size.
func (a *PaintLayerScrollableArea) computeScrollableSize() (float64, float64) {
	visibleW, visibleH := math.Round(a.clientWidth), math.Round(a.clientHeight)
	contentsW := max(math.Round(a.overflowRect.Size.WidthF64()), visibleW)
	contentsH := max(math.Round(a.overflowRect.Size.HeightF64()), visibleH)
	return contentsW - visibleW, contentsH - visibleH
}

// MinimumScrollOffset mirrors PaintLayerScrollableArea::
// MinimumScrollOffsetInt (paint_layer_scrollable_area.cc:640-642 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (a *PaintLayerScrollableArea) MinimumScrollOffset() html.ScrollOffset {
	x, y := a.clampNonScrollableAxesOffsets(-a.scrollOriginX, -a.scrollOriginY)
	return html.ScrollOffset{X: x, Y: y}
}

// MaximumScrollOffset mirrors PaintLayerScrollableArea::
// MaximumScrollOffsetInt (paint_layer_scrollable_area.cc:644-647 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (a *PaintLayerScrollableArea) MaximumScrollOffset() html.ScrollOffset {
	w, h := a.computeScrollableSize()
	x, y := a.clampNonScrollableAxesOffsets(-a.scrollOriginX+w, -a.scrollOriginY+h)
	return html.ScrollOffset{X: x, Y: y}
}

// ClampScrollOffset clamps offset into [MinimumScrollOffset,
// MaximumScrollOffset]. Mirrors ScrollableArea::ClampScrollOffset
// (scroll/scrollable_area.cc:1029-1035 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (a *PaintLayerScrollableArea) ClampScrollOffset(offset html.ScrollOffset) html.ScrollOffset {
	lo, hi := a.MinimumScrollOffset(), a.MaximumScrollOffset()
	return html.ScrollOffset{
		X: max(min(offset.X, hi.X), lo.X),
		Y: max(min(offset.Y, hi.Y), lo.Y),
	}
}

// scrolledContentOffset returns how far contents are translated by
// scrolling at the clamped scroll offset: the offset floored. Mirrors
// LayoutBox::ScrolledContentOffset (layout_box.cc:1781-1787 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); fragmentToBox subtracts it
// from the contents' paint offset, the effect of the ScrollTranslation
// paint property node (-ScrollPosition) combined with the ScrollOrigin
// already in the contents' paint offset.
func scrolledContentOffset(offset html.ScrollOffset) (dx, dy float64) {
	return math.Floor(offset.X), math.Floor(offset.Y)
}

// applyScrollableArea records fragment's scroll state on box — the
// element's requested scroll offset clamped to the scroll range, and the
// range itself (the scrollbar painter positions the thumb from them) — and
// returns the scrolled content offset. Zero for a non-scroll-container.
func applyScrollableArea(fragment *PhysicalFragment, box *Box) (dx, dy float64) {
	area := NewPaintLayerScrollableArea(fragment)
	if area == nil {
		return 0, 0
	}
	var requested html.ScrollOffset
	if fragment.Node != nil {
		requested = fragment.Node.ScrollOffset
	}
	return area.applyTo(box, requested)
}

// applyTo records the area's scroll range on box with the requested offset
// clamped into it, and returns the scrolled content offset.
func (a *PaintLayerScrollableArea) applyTo(box *Box, requested html.ScrollOffset) (dx, dy float64) {
	box.ScrollSize = PhysicalSize{
		Width:  a.overflowRect.Size.WidthF64(),
		Height: a.overflowRect.Size.HeightF64(),
	}
	box.MinimumScrollOffset = a.MinimumScrollOffset()
	box.MaximumScrollOffset = a.MaximumScrollOffset()
	box.ScrollOffset = a.ClampScrollOffset(requested)
	return scrolledContentOffset(box.ScrollOffset)
}

// ScrollPosition mirrors ScrollableArea::ScrollPosition
// (ScrollOffsetToPosition, scroll/scrollable_area.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the scroll offset measured
// from the overflow rect's left/top edge, ScrollOffset + ScrollOrigin.
// MinimumScrollOffset is −ScrollOrigin on every scrollable axis, and a
// scroll container never has a non-scrollable (overflow:clip) axis, so
// this is ScrollOffset − MinimumScrollOffset. Zero for a box that is not
// a scroll container.
func (b *Box) ScrollPosition() html.ScrollOffset {
	return html.ScrollOffset{
		X: b.ScrollOffset.X - b.MinimumScrollOffset.X,
		Y: b.ScrollOffset.Y - b.MinimumScrollOffset.Y,
	}
}
