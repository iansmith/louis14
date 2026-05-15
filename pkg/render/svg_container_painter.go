package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"

	"mazarin/textshape"
)

// scopedSVGTransformState pushes a draw-context state scope and applies
// an affine transform on entry; popping the scope on Close()
// removes the transform plus any rect-clip pushed alongside it. The
// DrawContext.Push/Pop pair models the same stack Blink's
// GraphicsContext maintains; pushing a fresh scope is the cheapest
// correct way to compose SVG transforms because every nested SVG
// container needs a fresh "save+restore" boundary.
//
// Mirrors Blink's ScopedSVGTransformState
// (core/paint/scoped_svg_transform_state.{h,cc}): an RAII-style guard
// that ensures the GraphicsContext returns to its outer state after
// the container's children paint.
type scopedSVGTransformState struct {
	dc        textshape.DrawContext
	pushed    bool
	hadClip   bool
	prevClip  geometry.RectF
	pctx      *svgPaintContext
	prevXfrm  geometry.AffineTransform
	prevDev   geometry.AffineTransform
	hadActive bool
}

// pushScopedSVGTransform begins a new transform scope. Always pushes
// the DrawContext stack — even an identity transform — so the matching
// Close() always pops the same number of frames. Returns a guard
// callers must Close() (the canonical idiom is `defer guard.Close()`).
//
// If extraClip is non-empty, a rect-clip is also applied within the
// scope; that is used by PaintViewportContainer to enforce the
// nested-<svg> viewport clip (SVG 2 §3.5 default overflow: hidden).
// The clip rect is interpreted in the coordinate space ESTABLISHED by
// `transform` (i.e. clip is applied after the transform), matching
// what Blink does in SVGContainerPainter::PaintObjectContainer.
func pushScopedSVGTransform(pctx *svgPaintContext, transform geometry.AffineTransform, extraClip geometry.RectF) *scopedSVGTransformState {
	s := &scopedSVGTransformState{
		dc:       pctx.dc,
		pctx:     pctx,
		prevXfrm: pctx.CurrentTransform,
		prevDev:  pctx.DeviceFromUser,
	}
	s.dc.Push()
	s.pushed = true
	if !transform.IsIdentity() {
		s.dc.MultiplyMatrix(
			transform.A, transform.B,
			transform.C, transform.D,
			transform.E, transform.F,
		)
	}
	// Update the paint context's running transform. Compose: the
	// accumulated transform from root to this scope is
	// prevXfrm · transform. Mirrors how Blink updates
	// SVGPaintContext's current_transform during recursion.
	s.pctx.CurrentTransform = s.prevXfrm.Compose(transform)
	s.pctx.DeviceFromUser = s.prevDev.Compose(transform)
	s.hadActive = true

	if !extraClip.IsEmpty() {
		// The clip rect lives in the current coordinate space (after
		// the transform above). DrawRectangle + Clip is the same
		// idiom paintLayerContent uses for CSS clip.
		s.dc.DrawRectangle(
			extraClip.X(), extraClip.Y(),
			extraClip.Width(), extraClip.Height(),
		)
		s.dc.Clip()
		s.hadClip = true
		s.prevClip = extraClip
	}
	return s
}

// Close pops the DrawContext scope, undoing the transform (and any
// rect-clip pushed at construction). The running CurrentTransform on
// the paint context is restored to its pre-Push value.
func (s *scopedSVGTransformState) Close() {
	if s == nil {
		return
	}
	if s.pushed {
		s.dc.Pop()
		s.pushed = false
	}
	if s.hadActive {
		s.pctx.CurrentTransform = s.prevXfrm
		s.pctx.DeviceFromUser = s.prevDev
		s.hadActive = false
	}
	_ = s.hadClip
	_ = s.prevClip
}

// paintSVGContainer paints an SVGContainer (typically a <g>): push
// the container's effective local transform (SVG transform composed
// with CSS transform per SVG 2 §7.5), then recurse into children in
// tree order. Mirrors Blink's SVGContainerPainter::Paint with its
// ScopedSVGTransformState scope and SVGObjectPainter recursion.
func paintSVGContainer(pctx *svgPaintContext, c *svg.SVGContainer) {
	if c == nil {
		return
	}
	if !svgIsVisible(c.Style) {
		// visibility: hidden inhibits paint of this element and its
		// descendants (no painted output, but the subtree is still
		// laid out — relevant for future hit-testing and bbox use,
		// which already happened at layout time).
		return
	}
	// Effective transform = cssTransform ∘ svgTransform. SVG 2 §7.5:
	// CSS `transform` is applied AFTER the SVG `transform`
	// attribute. With the SVGRoot painter pushing transforms onto a
	// DrawContext stack, "applied after" means composed on the LEFT
	// (outer) side, so a point p goes through svgT first, then cssT.
	cssT := svg.ParseCSSTransformForSVG(c.Style)
	effective := cssT.Compose(c.SVGAttributeTransform())

	guard := pushScopedSVGTransform(pctx, effective, geometry.RectF{})
	defer guard.Close()

	for _, child := range c.Children {
		paintSVGNode(pctx, child)
	}
}

// paintSVGViewportContainer paints a nested-<svg> (SVGViewportContainer).
// Mirrors Blink's SVGContainerPainter::Paint specialized for
// LayoutSVGViewportContainer. The transform/clip layering matches
// Blink's split between:
//
//	(outer)  cssTransform on the nested <svg>'s element  (SVG 2 §7.5)
//	         translate(x, y)                              (positioning)
//	         clip to (0, 0, width, height)                (overflow:hidden, SVG 2 §3.5)
//	(inner)  viewBox→viewport transform                   (intrinsic scaling)
//	         children paint
//
// The clip MUST be established AFTER translate(x, y) but BEFORE the
// viewBox transform, because the viewport rect is in the
// translated-but-not-yet-scaled coordinate space — descendants paint
// in viewBox space which the inner transform scales into the (0..w,
// 0..h) box. Pushing the clip after the inner transform would push it
// in viewBox units (typically 0..1 or 0..viewBox.Width) — wrong size.
func paintSVGViewportContainer(pctx *svgPaintContext, vc *svg.SVGViewportContainer) {
	if vc == nil {
		return
	}
	if !svgIsVisible(vc.Style) {
		return
	}
	if vc.Width <= 0 || vc.Height <= 0 {
		// An empty nested viewport produces no paint per SVG 2 §3.5.
		return
	}

	// Outer transform = cssTransform ∘ translate(x, y). Clip the
	// nested viewport rect (in this translated coordinate space)
	// before applying the viewBox→viewport scaling, so the clip rect
	// is in the same coordinate space the viewport's width/height
	// refer to.
	cssT := svg.ParseCSSTransformForSVG(vc.Style)
	outer := cssT.Compose(geometry.Translate(vc.X, vc.Y))
	clipRect := geometry.NewRectF(0, 0, vc.Width, vc.Height)

	outerGuard := pushScopedSVGTransform(pctx, outer, clipRect)
	defer outerGuard.Close()

	// Inner transform = viewBox→viewport. Applies inside the clip;
	// scales viewBox coords into the (0..width, 0..height) viewport
	// rect already established by the outer guard's clip.
	inner := vc.LocalToBorderBoxTransform
	innerGuard := pushScopedSVGTransform(pctx, inner, geometry.RectF{})
	defer innerGuard.Close()

	for _, child := range vc.Children {
		paintSVGNode(pctx, child)
	}
}

// svgIsVisible reports whether an SVG element's `visibility` allows
// it to paint. Mirrors Blink's StyleSVGResource visibility check.
// A nil style is treated as visible (the SVG default).
func svgIsVisible(style *css.Style) bool {
	if style == nil {
		return true
	}
	v, ok := style.Get("visibility")
	if !ok {
		return true
	}
	return v != "hidden" && v != "collapse"
}
