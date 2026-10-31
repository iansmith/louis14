package render

import (
	"image"
	"image/color"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"

	"mazarin/textshape"
)

// svgPaintContext is the per-paint-pass state shared between
// SVGRootPainter, SVGContainerPainter (Phase 3), SVGShapePainter, and
// SVGObjectPainter. Mirrors the bundle Blink threads through
// SVGPaintContext + the inputs SVGPainter classes pull out of
// PaintInfo (graphics_context, transform_state, current viewport).
//
// Phase 2 carries:
//   - dc: the DrawContext to paint into.
//   - originX/Y: the CSS-px offset of the SVG content origin within
//     the page coordinate system: the pixel-snapped border-box origin
//     plus the unsnapped border+padding offset (see paintSVGRoot). The
//     viewBox→viewport transform is applied AFTER this translation, so
//     a shape at user-units (10, 20) inside <svg> whose content origin
//     is page (50, 100) paints at page-space (60, 120) (modulo the
//     viewBox map).
//   - viewport: the user-space rectangle the SVG content paints into.
//     Currently equals (0, 0, containerWidth, containerHeight) but
//     Phase 3's nested <svg> will push a smaller viewport here.
type svgPaintContext struct {
	dc       textshape.DrawContext
	originX  float64
	originY  float64
	viewport geometry.RectF

	// CurrentTransform is the accumulated affine transform from the
	// SVG root down to the current paint scope (NOT including the
	// outer SVG-root viewBox→viewport transform, which is applied
	// directly to the DrawContext at SVGRootPainter entry and lives
	// outside the painter's transform-stack abstraction). Each
	// scopedSVGTransformState updates this on push and restores it on
	// Close, mirroring SVGPaintContext::PushTransform /
	// PopTransform's bookkeeping in Blink. Phase 3 maintains this for
	// forward-compat with paint server / mask coordinate computations
	// (Phase 4+).
	CurrentTransform geometry.AffineTransform

	// DeviceFromUser maps a point in the current node's local
	// user-space coordinate system to the destination image's device
	// pixel coordinate system. Equal to the composed transform stack
	// the DrawContext has pushed:
	//
	//   translate(originX, originY) · root.LocalToBorderBoxTransform ·
	//     CurrentTransform
	//
	// The first two factors are Blink's
	// SVGRootPainter::TransformToPixelSnappedBorderBox
	// (core/paint/svg_root_painter.cc:28-67 @ Chromium
	// 5992657f556bcd9d207a4aca187488462f027cab). louis14's
	// LocalToBorderBoxTransform is content-box-relative, unlike Blink's,
	// so originX/originY carry the border+padding offset instead.
	//
	// Paint servers need this to map a shape's reference bbox (which
	// is in user-space) into the device-pixel rect they need to
	// iterate. SVG 2 §13.4: gradient/pattern coordinates are
	// evaluated in the shape's user-space coordinate system, then the
	// resulting paint is rasterized at the device-pixel level —
	// equivalent to inverting this transform per-pixel.
	DeviceFromUser geometry.AffineTransform

	// Resources is the outermost SVGRoot's resource registry, used by
	// paint servers to resolve `url(#id)` references. nil before Phase
	// 4 (and harmless in Phase 1-3 because no painter dereferences it).
	Resources *svg.SVGResourceRegistry

	// Renderer is the host Renderer used by paint-server fills to
	// composite onto the same *image.RGBA the rest of the renderer
	// writes to. The DC doesn't expose pattern fills, so paint
	// servers rasterize the shape's path through a child DC into a
	// coverage buffer, then composite the per-pixel paint color
	// against that coverage onto Renderer.target. Mirrors how the
	// existing CSS gradient code (gradient.go) reaches the target
	// image directly.
	Renderer *Renderer
}

// setDCColor sets the draw context's color from a css.Color,
// mirroring the renderer-wide setColor helper. Centralized here so
// the SVG painter cluster doesn't reach into Renderer methods.
func setDCColor(dc textshape.DrawContext, c css.Color) {
	dc.SetColor(color.RGBA{
		R: c.R,
		G: c.G,
		B: c.B,
		A: uint8(c.A * 255),
	})
}

// viewportDeviceClip returns the SVG root viewport in device pixels, for the
// filter composite paths that write straight to the target image and so
// bypass the DrawContext clip stack. The edges are int()-truncated, while
// paintSVGRoot's fallback viewport clip rounds via pixelSnap, so any edge can
// differ by 1px from it whenever originX/Y is fractional (fractional border or
// padding) or the viewport size is fractional (LOU-426 follow-up).
//
// no Blink analog: Blink composites filter output under the replaced
// content clip property node; louis14's filter composite bypasses the DC.
func (c *svgPaintContext) viewportDeviceClip() image.Rectangle {
	return image.Rect(
		int(c.originX),
		int(c.originY),
		int(c.originX+c.viewport.Width()),
		int(c.originY+c.viewport.Height()),
	)
}
