package render

import (
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// paintSVGRoot is the entry point for SVG paint. Mirrors Blink's
// SVGRootPainter::PaintReplaced (core/paint/svg_root_painter.{h,cc}):
//
//  1. Translate the DrawContext to the <svg> content-box origin.
//  2. Apply LocalToBorderBoxTransform (the viewBox→viewport mapping).
//  3. Recurse into the SVGRoot's children in tree order.
//  4. Pop the transform stack.
//
// This is hooked into paintSelfForeground (the CSS 2.1 Appendix E
// step-5 inline-foreground phase) — replaced-element content paints in
// the same slot as <img>. The replaced <svg> still gets the normal
// outer treatment (background, border, outline, stacking-context
// promotion); only the *content* paint is replaced.
func (r *Renderer) paintSVGRoot(layer *PaintLayer) {
	if layer == nil || layer.Box == nil || layer.Box.LayoutNode == nil {
		return
	}
	root, ok := layer.Box.LayoutNode.SVGRoot.(*svg.SVGRoot)
	if !ok || root == nil {
		return
	}

	box := layer.Box
	// Content-box origin in page coordinates. CSS 2.1 §10.7: the
	// content-box origin is (box.X + border-left + padding-left,
	// box.Y + border-top + padding-top). Pixel-snap is unnecessary
	// here because the transform stack will sub-pixel position the
	// path rasterizer correctly — Blink doesn't snap the SVG content
	// origin either, only the outer <svg> border-box.
	originX := box.X + box.Border.Left + box.Padding.Left
	originY := box.Y + box.Border.Top + box.Padding.Top

	// DeviceFromUser at the root paint scope is the composed
	// translate(originX, originY) · LocalToBorderBoxTransform: maps a
	// point in the outermost SVG's user-space coords into device
	// (page-pixel) coords. Each scopedSVGTransformState composes its
	// own transform on top as the paint walk descends. Paint servers
	// use this when fill is `url(#…)` to know the device rect they
	// need to iterate.
	deviceFromUser := geometry.Translate(originX, originY).Compose(root.LocalToBorderBoxTransform)

	pctx := &svgPaintContext{
		dc:      r.dc,
		originX: originX,
		originY: originY,
		viewport: geometry.NewRectF(0, 0,
			root.ContainerSize.Width, root.ContainerSize.Height),
		CurrentTransform: geometry.Identity(),
		DeviceFromUser:   deviceFromUser,
		Resources:        root.Resources,
		Renderer:         r,
	}

	// Push a scope so the translation + viewBox transform pop cleanly.
	r.dc.Push()
	defer r.dc.Pop()

	// SVG 2 §3.5: the outermost <svg> is always clipped to its viewport
	// (the UA stylesheet sets overflow:hidden on the SVG element).
	// When `overflow-clip-margin` is set, the standard CSS-overflow
	// paint path in `paintLayer` is enabled for SVG roots and that path
	// installs the expanded clip — we must NOT install a second
	// viewport clip here (it would clip back to the SVG content rect
	// and silently undo the outward margin). When no margin is set,
	// the CSS-overflow path is bypassed (the SVG outer clip is then
	// pixel-snapped relative to the SVG's box origin, which can drift
	// 1px from the SVG content paint origin) and we install the
	// SVG-side viewport clip in unsnapped coords to match the content
	// paint.
	if !layer.HasClipMargin {
		r.dc.DrawRectangle(originX, originY,
			root.ContainerSize.Width, root.ContainerSize.Height)
		r.dc.Clip()
	}

	r.dc.Translate(originX, originY)
	t := root.LocalToBorderBoxTransform
	if !t.IsIdentity() {
		r.dc.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}

	// Recurse children.
	for _, child := range root.Children {
		paintSVGNode(pctx, child)
	}
}

// paintSVGNode dispatches a single SVGNode to its appropriate
// painter. Phase 3 supports SVGShape, SVGContainer (with transform
// scope), and SVGViewportContainer (with transform scope + viewport
// clip).
func paintSVGNode(ctx *svgPaintContext, node svg.SVGNode) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *svg.SVGShape:
		sp := &svgShapePainter{ctx: ctx, shape: n}
		sp.paint()
	case *svg.SVGContainer:
		paintSVGContainer(ctx, n)
	case *svg.SVGViewportContainer:
		paintSVGViewportContainer(ctx, n)
	default:
		// Unknown SVGNode kind — Phase 6 (resource elements) and
		// later phases add cases here. The default keeps the painter
		// crash-free if a new SVGNode kind appears before its
		// dispatch lands.
		_ = n
	}
}

// isInlineSVGLayer reports whether a paint layer is an inline <svg>
// element with a resolved SVGRoot. Used by paintSelfForeground to
// route the layer through paintSVGRoot instead of drawImage. Mirrors
// the dispatch condition Blink applies in PaintReplacedContents.
func isInlineSVGLayer(layer *PaintLayer) bool {
	if layer == nil || layer.Box == nil || layer.Box.Node == nil {
		return false
	}
	if layer.Box.Node.TagName != "svg" {
		return false
	}
	if layer.Box.LayoutNode == nil {
		return false
	}
	root, ok := layer.Box.LayoutNode.SVGRoot.(*svg.SVGRoot)
	return ok && root != nil
}
