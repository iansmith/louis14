package render

import (
	"image/color"

	"louis14/pkg/css"
	"louis14/pkg/geometry"

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
//   - originX/Y: the CSS-px offset of the SVG content-box origin
//     within the page coordinate system. The viewBox→viewport
//     transform is applied AFTER this translation, so a shape at
//     user-units (10, 20) inside <svg> at page (50, 100) ultimately
//     paints at page-space (60, 120) (modulo the viewBox map).
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
