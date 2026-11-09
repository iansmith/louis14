package render

import (
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
//   - viewport: the user-space rectangle the SVG content paints into.
//     Currently equals (0, 0, containerWidth, containerHeight) but
//     Phase 3's nested <svg> will push a smaller viewport here.
type svgPaintContext struct {
	dc       textshape.DrawContext
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
	// pixel coordinate system (in an effect buffer, the buffer's pixels:
	// newSVGEffectSource). Equal to the composed transform stack
	// the DrawContext has pushed:
	//
	//   contentToDevice · root.LocalToBorderBoxTransform ·
	//     CurrentTransform
	//
	// The first two factors are Blink's
	// SVGRootPainter::TransformToPixelSnappedBorderBox
	// (core/paint/svg_root_painter.cc:28-67 @ Chromium
	// 5992657f556bcd9d207a4aca187488462f027cab). louis14's
	// LocalToBorderBoxTransform is content-box-relative, unlike Blink's,
	// so contentToDevice carries the border+padding offset instead.
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

	// clipPathContent is set while painting <clipPath> content, which
	// clips with its geometry only: its filters, masks and other
	// non-geometric effects are not applied
	// (ScopedSVGPaintState::ApplyEffects, scoped_svg_paint_state.cc:
	// 117-125 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	clipPathContent bool

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

// setDCColor sets the draw context's color from a css.Color, premultiplied like
// the renderer-wide setColor. Centralized here so the SVG painter cluster
// doesn't reach into Renderer methods. Blink: SVGObjectPainter::PreparePaint,
// core/paint/svg_object_painter.cc:180-181 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b (flags.setColor of the colour with
// fill/stroke opacity folded in; Skia premultiplies it).
func setDCColor(dc textshape.DrawContext, c css.Color) {
	dc.SetColor(premultipliedRGBA(c))
}

// newBufferPaintContext returns the paint context for resource content
// painted into tmpR's buffer, whose content is the viewport (user units)
// scaled onto the whole buffer: <mask> and <clipPath> children (their
// viewport is the buffer, in pixels) and <pattern> tile content.
// <clipPath> content sets clipPathContent, as it paints its geometry without
// effects.
func newBufferPaintContext(tmpR *Renderer, viewport geometry.SizeF, deviceFromUser geometry.AffineTransform, resources *svg.SVGResourceRegistry) *svgPaintContext {
	return &svgPaintContext{
		dc:               tmpR.dc,
		viewport:         geometry.NewRectF(0, 0, viewport.Width, viewport.Height),
		CurrentTransform: geometry.Identity(),
		DeviceFromUser:   deviceFromUser,
		Resources:        resources,
		Renderer:         tmpR,
	}
}
