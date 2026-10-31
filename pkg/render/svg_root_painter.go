package render

import (
	"louis14/pkg/geometry"
	"louis14/pkg/layout"
	"louis14/pkg/layout/svg"
)

// paintSVGRoot is the entry point for SVG paint. Mirrors Blink's
// SVGRootPainter::PaintReplaced (core/paint/svg_root_painter.{h,cc}):
//
//  1. Translate the DrawContext to the pixel-snapped border-box origin
//     plus the (unsnapped) border+padding offset.
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
	// Content origin in page coordinates. Mirrors Blink's
	// SVGRootPainter::TransformToPixelSnappedBorderBox
	// (core/paint/svg_root_painter.cc:28-67 @ Chromium
	// 5992657f556bcd9d207a4aca187488462f027cab): content paints under
	// Translation(ToPixelSnappedRect(border box).origin) (:30-32), the same
	// snap the <svg> background gets, PreConcat'd with
	// LocalToBorderBoxTransform (:64-65). Blink's LocalToBorderBoxTransform
	// carries the border+padding content offset unsnapped
	// (LayoutSVGRoot::BuildLocalToBorderBoxTransform,
	// core/layout/svg/layout_svg_root.cc:426-445 @ 5992657f556b); louis14's
	// svg.SVGRoot.LocalToBorderBoxTransform is content-box-relative, so the
	// unsnapped border+padding is added here instead.
	//
	// Blink's inline-root scale-to-fit when snapping shrinks the box
	// (svg_root_painter.cc:41-61) is not ported: it is gated by
	// SvgInlineRootPixelSnappingScaleAdjustment, status "test"
	// (platform/runtime_enabled_features.json5:6672-6673 @ 5992657f556b),
	// so shipping Chrome does not apply it. The document-element scale
	// (:37-40) has no caller here: paintSVGRoot paints inline <svg> only.
	snappedX, snappedY, _, _ := pixelSnap(box.X, box.Y, box.Width, box.Height)
	contentOffsetX := box.Border.Left + box.Padding.Left
	contentOffsetY := box.Border.Top + box.Padding.Top
	originX := snappedX + contentOffsetX
	originY := snappedY + contentOffsetY

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

	// Viewport clip. When layer.HasClip is set (overflow-x/overflow-y
	// clip on some axis, or overflow-clip-margin; HasClipMargin implies
	// HasClip, both are set in the same `if clipX || clipY` block in
	// paint_layer.go), the CSS-overflow paint path in `paintLayer` has
	// already installed the pixel-snapped per-axis clip, extending any
	// unclipped axis; a second clip here would override those per-axis
	// semantics.
	//
	// Otherwise this fallback stands in for Blink's default inline-svg
	// clip: the UA sheet gives svg:not(:root) overflow:hidden and
	// overflow-clip-margin:content-box (core/css/svg.css:48-54 @ 5992657f556b),
	// which UpdateOverflowClip turns into ToSnappedClipRect of the content
	// box for a replaced element
	// (core/paint/paint_property_tree_builder.cc:3409-3419, helper
	// :2822-2824 @ 5992657f556b). louis14 has no such UA rule, so the
	// painter installs it. The content box is snapped on its own, as
	// Blink snaps the clip rect, not derived from the snapped origin: the
	// two differ when border or padding is fractional.
	if !layer.HasClip {
		cx, cy, cw, ch := pixelSnap(box.X+contentOffsetX, box.Y+contentOffsetY,
			root.ContainerSize.Width, root.ContainerSize.Height)
		r.dc.DrawRectangle(cx, cy, cw, ch)
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
	case *svg.SVGText:
		tp := &svgTextPainter{ctx: ctx, text: n}
		tp.paint()
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

// isSVGContentBox reports whether the given box represents an SVG content
// element nested inside an inline <svg> root (i.e. a <rect>, <g>, <text>,
// <feImage>, …). The <svg> root itself returns false — it is an HTML
// replaced element and participates in normal CSS layer dispatch.
//
// Walks Box.Parent → Box.Parent.Parent → … looking for a `<svg>` ancestor.
// Cheap (SVG nesting is typically shallow) and avoids reaching into the
// layout tree's SVGRoot model. Used by paint_layer.go to suppress
// CSS-layer-side filter / mask / mix-blend dispatch on SVG content,
// because the SVG paint walk (svg_shape_painter.go,
// svg_container_painter.go) is the authoritative consumer for those
// properties on SVG elements. Mirrors Blink's LayoutSVGShape/
// LayoutSVGContainer paint dispatch — those types route through
// SVGObjectPainter rather than the LayoutBox PaintLayer pipeline.
func isSVGContentBox(box *layout.Box) bool {
	if box == nil || box.Node == nil {
		return false
	}
	// The <svg> root itself is not "SVG content" — its paint dispatch
	// uses the standard CSS-replaced-element path until paintSelfForeground
	// hands off to paintSVGRoot.
	if box.Node.TagName == "svg" {
		return false
	}
	for p := box.Parent; p != nil; p = p.Parent {
		if p.Node != nil && p.Node.TagName == "svg" {
			return true
		}
	}
	return false
}
