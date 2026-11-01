package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout"
)

// ViewPainter paints the view's background: the canvas background, taken
// from the document element's style or propagated from the body's
// (css.PropagateStyleToViewport). Mirrors Blink's ViewPainter
// (core/paint/view_painter.{h,cc} @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// louis14 has no LayoutView object or paint property trees: the view
// background is painted from inside the root element's layer paint, at the
// point where the root's effects (filter / mask / blend isolation buffers)
// are active but its transform is not yet applied — Blink paints it in the
// root element's LocalBorderBoxProperties with the transform replaced by
// its PreTransform (view_painter.cc:183-201). Nested documents (iframes)
// have no view of their own yet, so their document element keeps painting
// its own background.
type ViewPainter struct {
	// rootLayer is the paint layer of the document element; its box is
	// the view's positioning box (BoxBackgroundPaintContext::
	// positioning_box_ = RootBox()).
	rootLayer *PaintLayer
	// transferringBody is the first body's layout object when its
	// background transfers to the view (BackgroundTransfersToView), else
	// nil. Resolved once per paint: drawBackground asks for every box.
	transferringBody *layout.LayoutInputNode
	// style is the view's propagated background.
	style css.ViewportStyle
	// backgroundRect is the view's background rect in document space
	// (ViewPainter::BackgroundRect, view_painter.cc:454-459): the canvas,
	// or the embedder's viewport rect.
	backgroundRect geometry.PhysicalRect
}

// newViewPainter builds the view painter for the document element rootBox
// painted through rootLayer, running the background half of
// StyleResolver::PropagateStyleToViewport (style_resolver.cc:3416-3499 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Returns nil when the root box
// has no layout object (an empty document).
func newViewPainter(r *Renderer, rootBox *layout.Box, rootLayer *PaintLayer) *ViewPainter {
	root := rootBox.LayoutNode
	if root == nil || !root.IsDocumentElement() {
		return nil
	}
	body := root.FirstBodyElement()
	var bodyStyle *css.Style
	if !layout.ShouldStopBodyPropagation(root) && !layout.ShouldStopBodyPropagation(body) {
		bodyStyle = body.Style()
	}
	if !body.BackgroundTransfersToView() {
		body = nil
	}
	rect := geometry.PhysicalRectFromRect(r.target.Bounds())
	if r.embeddedViewport.W > 0 && r.embeddedViewport.H > 0 {
		rect = toLayoutRect(0, 0, r.embeddedViewport.W, r.embeddedViewport.H)
	}
	return &ViewPainter{
		rootLayer:        rootLayer,
		transferringBody: body,
		style:            css.PropagateStyleToViewport(rootBox.Style, bodyStyle),
		backgroundRect:   rect,
	}
}

// backgroundTransfersToView reports whether layer's box leaves its
// background to the view (LayoutBoxModelObject::BackgroundTransfersToView,
// layout/layout_box_model_object.go): the document element and the
// transferring body of the document this renderer's view paints.
func (r *Renderer) backgroundTransfersToView(layer *PaintLayer) bool {
	if r.view == nil || layer.Box == nil || layer.Box.LayoutNode == nil {
		return false
	}
	n := layer.Box.LayoutNode
	return n == r.view.rootLayer.Box.LayoutNode || n == r.view.transferringBody
}

// paintViewBackground paints the view background when layer is the
// document element's layer; callers invoke it where the root's effects are
// active and before its transform is applied.
func (r *Renderer) paintViewBackground(layer *PaintLayer) {
	if r.view != nil && layer == r.view.rootLayer {
		r.view.PaintRootElementGroup(r)
	}
}

// PaintRootElementGroup paints the view background: the propagated
// background color over the whole background rect, then each fill layer,
// bottom to top, positioned against the root element's border box and
// clipped to the background rect (background-clip is ignored). Mirrors
// ViewPainter::PaintRootElementGroup (view_painter.cc:264-450 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the base background (white)
// is filled by Render / RenderEmbedded before painting starts, and the
// root color is blended over it source-over (combined_background_color,
// :416-431).
func (v *ViewPainter) PaintRootElementGroup(r *Renderer) {
	paintRect := v.backgroundRect
	px := geometry.ToPixelSnappedRect(paintRect)
	if v.style.BackgroundColor.A > 0 {
		r.dc.DrawRectangle(float64(px.Min.X), float64(px.Min.Y), float64(px.Dx()), float64(px.Dy()))
		r.setColor(v.style.BackgroundColor)
		r.dc.Fill()
	}
	if v.style.BackgroundLayers == nil {
		return
	}
	// The image offset maps the view's paint rect onto the root element's
	// border box (view_painter.cc:346-347: −root paint offset + paint rect
	// origin), so tiling starts at the root's origin.
	ctx := v.newViewBoxBackgroundPaintContext(paintRect)
	r.dc.Push()
	defer r.dc.Pop()
	r.dc.DrawRectangle(float64(px.Min.X), float64(px.Min.Y), float64(px.Dx()), float64(px.Dy()))
	r.dc.Clip()
	rootStyle := v.rootLayer.Box.Style
	v.style.BackgroundLayers.IterateReverse(func(bg *css.FillLayer) {
		if img := r.loadFillLayerImage(bg); bg.Gradient != "" || !img.isNone() {
			r.paintFillLayerImage(ctx, paintRect, bg, img, v.style.ImageRendering, rootStyle)
		}
	})
}

// newViewBoxBackgroundPaintContext builds the view's
// BoxBackgroundPaintContext (box_background_paint_context.cc:92-115 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): positioning_box_ is the root
// box, the positioning area is the root's border-box size
// (positioning_size_override_ = RootBox().StitchedSize()), and the element
// positioning offset maps paintRect onto the root's border box.
// has_background_fixed_to_viewport_ is LayoutView::
// IsBackgroundAttachmentFixedObject: the view's style has a fixed image
// layer (layout_box_model_object.cc:480-482). The fixed positioning area is
// the viewport, the view's background rect.
func (v *ViewPainter) newViewBoxBackgroundPaintContext(paintRect geometry.PhysicalRect) *BoxBackgroundPaintContext {
	rootBox := v.rootLayer.Box
	rootRect := backgroundPaintRect(borderBoxRect(rootBox))
	return &BoxBackgroundPaintContext{
		paintingView:                   true,
		hasBackgroundFixedToViewport:   v.style.BackgroundLayers.AnyLayerHasFixedAttachmentImage(),
		positioningSizeOverride:        rootRect.Size,
		elementPositioningAreaOffset:   paintRect.Offset.Sub(rootRect.Offset),
		borderOutsets:                  toLayoutStrut(rootBox.Border),
		paddingOutsets:                 toLayoutStrut(rootBox.Padding),
		fixedAttachmentPositioningArea: v.backgroundRect,
	}
}
