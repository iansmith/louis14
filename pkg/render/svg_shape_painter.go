package render

import (
	"image"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"

	"mazarin/textshape"
)

// svgShapePainter paints a single SVGShape. Mirrors Blink's
// SVGShapePainter (core/paint/svg_shape_painter.{h,cc}). The paint
// order per SVG 1.1 §11.4 is strictly:
//
//  1. Fill (if any)
//  2. Stroke (if any)
//  3. Markers (Phase 4+; not implemented here)
//
// SVG does not fill-then-stroke as a single combined draw — the
// stroke must paint above the fill. Doing it the other way around
// makes wide strokes encroach on the filled interior. Blink keeps the
// two passes in separate dc.Fill / dc.Stroke calls; we do the same.
type svgShapePainter struct {
	ctx   *svgPaintContext
	shape *svg.SVGShape
}

// paint executes the fill-then-stroke sequence for the shape. The
// path is added to dc only once and reused via FillPreserve /
// StrokePreserve when both passes are active, so the path
// rasterizer's work is shared.
func (sp *svgShapePainter) paint() {
	if sp.shape == nil {
		return
	}
	dc := sp.ctx.dc

	// The SVG layout pass attaches *css.Style to each SVGShape via
	// the StyleResolver callback (see svg_root_algorithm.go); the
	// painter reads it directly. A nil style means the shape's
	// element didn't get cascade attention — fall back to SVG
	// property defaults inside the object painter.
	style := sp.shape.Style
	op := newSVGObjectPainter(dc, style)
	// Phase 4: wire the paint context's resource registry +
	// shape-relative reference box for `url(#id)` paint resolution.
	// The reference box is the shape's user-space fillBoundingBox
	// — passed by value through withResources so the object painter
	// can map objectBoundingBox-units gradient coords onto it.
	op.withResources(sp.ctx.Resources, sp.shape.FillBoundingBox, svg.NewSVGLengthContext(sp.ctx.viewport.Size))

	// SVG 1.1 §11.3.3 (visibility): visibility:hidden on a shape
	// inhibits paint but its children still hit-test. There are no
	// shape children in Phase 2, so a hidden shape is a no-op.
	if !svgIsVisible(style) {
		return
	}

	// CSS `transform` on an SVG shape (SVG 2 §7.5). Shapes don't
	// have an SVG `transform` attribute (only containers do per SVG
	// 1.1 §7.6), so the effective shape-level transform is just
	// whatever CSS supplies. The same scope guard used by
	// paintSVGContainer wraps the path build + fill + stroke so
	// rasterization happens in the transformed coordinate space.
	// Mirrors Blink's SVGShapePainter applying
	// LocalSVGTransform via a ScopedSVGTransformState.
	cssT := svg.ParseCSSTransformForSVG(style)
	if !cssT.IsIdentity() {
		guard := pushScopedSVGTransform(sp.ctx, cssT, geometry.RectF{})
		defer guard.Close()
	}

	// SVG-level clip-path: url(#id). Per SVG 2 §15.3, a `clip-path`
	// presentation attribute on a shape clips its rendering to the
	// referenced `<clipPath>` element's geometry. Mirrors Blink's
	// SVGShapePainter applying SVGClipPainter at the per-shape scope.
	// The clip is established BEFORE fill/stroke so both passes are
	// clipped — matches Blink's SVGObjectPainter ordering (clip →
	// fill → stroke → markers).
	var clipPushed bool
	if style != nil {
		if cp := style.GetClipPath(); cp != nil && cp.Type == css.ClipPathReference {
			sp.ctx.dc.Push()
			clipPushed = true
			defer func() {
				if clipPushed {
					sp.ctx.dc.Pop()
				}
			}()
			// The shape's user-space FillBoundingBox is the reference
			// box for clip-path resolution (objectBoundingBox units
			// resolve against it). The clip is applied in the SAME
			// user-space coords the shape is rasterized in — the
			// scoped transform state above has already pushed any CSS
			// transform onto the DC.
			sp.ctx.Renderer.applySVGClipPathOnSVGShape(
				sp.ctx, sp.shape.FillBoundingBox, cp.ReferenceID,
			)
		}
	}

	willFill := op.style != nil && op.style.GetFill().Kind != css.SVGPaintNone
	willStroke := op.style != nil && op.style.GetStroke().Kind != css.SVGPaintNone
	// Style was nil → defaults apply: fill: black, stroke: none.
	if op.style == nil {
		willFill = true
	}
	if !willFill && !willStroke {
		return
	}

	// SVG-level mask: url(#id). Per SVG 1.1 §14.4, a `mask`
	// presentation attribute on a shape redirects rendering through
	// an offscreen buffer, applies the referenced `<mask>` element's
	// rasterized children as a per-pixel kDstIn modulator, and
	// composites the result back. Differs from clip-path in that the
	// mask is a continuous alpha buffer, not a binary clip region.
	//
	// For an SVG shape inside an `<svg>`, the shape's paint normally
	// goes directly through sp.ctx.dc (which is the page DC). To
	// apply a mask, we redirect the shape's fill/stroke into a
	// temporary buffer matching the shape's bbox in device space,
	// apply the mask, and DrawImage the buffer back. Mirrors Blink's
	// SVGShapePainter dispatch to SVGMaskPainter when the shape
	// carries a mask.
	if style != nil {
		if maskVal := style.GetMaskImage(); maskVal != "" && maskVal != "none" {
			if id, ok := parseURLFragment(maskVal); ok {
				if sp.ctx.Resources != nil {
					if masker, found := sp.ctx.Resources.LookupMasker(id); found && masker != nil {
						sp.paintWithSVGMask(masker, op, willFill, willStroke)
						return
					}
				}
			}
		}
	}

	// Build the path on the DrawContext once.
	if !buildPathOnDC(dc, &sp.shape.Path) {
		return
	}

	// Determine the fill rule for the shader path. The DC-level
	// fill rule is also set by applyFill below; we read style here
	// for the shader-path fillPathWithShader signature.
	evenOdd := false
	if style != nil && style.GetFillRule() == css.SVGFillRuleEvenOdd {
		evenOdd = true
	}

	// Fill pass.
	if willFill {
		fillRes := op.applyFill()
		switch fillRes.mode {
		case svgPaintSolid:
			if willStroke {
				dc.FillPreserve()
			} else {
				dc.Fill()
			}
		case svgPaintShader:
			// Paint-server fill: rasterize the path into a coverage
			// buffer and composite the shader's per-pixel paint over
			// the target. The path is currently sitting on dc; we
			// leave the path intact for any subsequent stroke pass
			// (the dc.Fill path is the one that consumes it). For
			// the shader path, fillPathWithShader builds its own
			// child-DC path replay against the coverage buffer, so
			// dc's path is undisturbed — but we still must clear it
			// if no stroke follows so a later draw doesn't reuse it.
			sp.ctx.Renderer.fillPathWithShader(sp.ctx, &sp.shape.Path, fillRes.shader, evenOdd)
			if willStroke {
				// Re-build the path for the stroke pass (dc.Fill
				// wasn't called, but dc.FillPreserve also wasn't —
				// the path remains. Still, since we're not 100%
				// sure of state across child DC creation, rebuild
				// defensively.)
				dc.ClearPath()
				if !buildPathOnDC(dc, &sp.shape.Path) {
					return
				}
			} else {
				dc.ClearPath()
			}
		case svgPaintSkip:
			if willStroke {
				// Drop the prepared fill state but keep the path
				// for the stroke pass.
				dc.ClearPath()
				if !buildPathOnDC(dc, &sp.shape.Path) {
					return
				}
			}
		}
	}

	// Stroke pass.
	if willStroke {
		strokeRes := op.applyStroke()
		switch strokeRes.mode {
		case svgPaintSolid:
			dc.Stroke()
		case svgPaintShader:
			// Paint-server stroke: stroke-as-fill via the DC's
			// stroke geometry, but coloured per-pixel by the shader.
			// We invoke a thin helper that rasterizes the stroke
			// path into the coverage buffer (using dc.Stroke against
			// a sentinel color) before compositing the shader over.
			sp.ctx.Renderer.strokePathWithShader(sp.ctx, &sp.shape.Path, strokeRes.shader, style)
			dc.ClearPath()
		case svgPaintSkip:
			dc.ClearPath()
		}
	}
}

// paintWithSVGMask redirects the shape's fill/stroke into an
// offscreen buffer sized to the shape's device-space bbox, applies
// the resolved `<mask>` element's children as a kDstIn alpha
// modulator, and DrawImage's the result back onto the page DC.
//
// Mirrors Blink's SVGMaskPainter::PaintSVGMaskForSVG: rasterize the
// shape into a buffer, rasterize the mask subtree into a parallel
// buffer, modulate, composite. Phase 5 implements the case where the
// shape has only a single shape geometry (the hand-check tests). The
// mask's MaskContentUnits + MaskUnits resolution flows through
// rasterizeSVGMask, identical to the CSS-box mask path.
func (sp *svgShapePainter) paintWithSVGMask(masker *svg.SVGResourceMasker, op *svgObjectPainter, willFill, willStroke bool) {
	// Compute the shape's bbox in DEVICE pixels. The shape's
	// FillBoundingBox is in user-space; map through DeviceFromUser.
	userBBox := sp.shape.FillBoundingBox
	if !willStroke && userBBox.IsEmpty() {
		return
	}
	// Expand for stroke geometry if needed.
	if willStroke && sp.shape.Style != nil {
		half := sp.shape.Style.GetStrokeWidth()
		userBBox = geometry.NewRectF(
			userBBox.X()-half, userBBox.Y()-half,
			userBBox.Width()+2*half, userBBox.Height()+2*half,
		)
	}
	deviceBBox := sp.ctx.DeviceFromUser.MapRect(userBBox)
	if deviceBBox.IsEmpty() {
		return
	}
	bx := int(math.Floor(deviceBBox.X()))
	by := int(math.Floor(deviceBBox.Y()))
	bw := int(math.Ceil(deviceBBox.Right())) - bx + 1
	bh := int(math.Ceil(deviceBBox.Bottom())) - by + 1
	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		return
	}

	// Render the shape's fill/stroke into a fresh buffer with the
	// SAME transform stack the page DC has, so the rasterized
	// geometry pixel-aligns with what DrawImage will composite back.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	tmpR := NewRendererForImage(buf)
	tmpR.dc.Translate(float64(-bx), float64(-by))
	t := sp.ctx.DeviceFromUser
	if !t.IsIdentity() {
		tmpR.dc.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}
	// Run a child paint context targeting the temp DC + renderer so
	// fill-shader / stroke-shader fall back onto the temp buffer
	// instead of the page target.
	childCtx := *sp.ctx // shallow copy
	childCtx.dc = tmpR.dc
	childCtx.Renderer = tmpR
	childOp := newSVGObjectPainter(tmpR.dc, op.style).withResources(
		op.resources, op.refBox, op.lengthCtx,
	)

	evenOdd := sp.shape.Style != nil && sp.shape.Style.GetFillRule() == css.SVGFillRuleEvenOdd
	if !buildPathOnDC(tmpR.dc, &sp.shape.Path) {
		return
	}
	if willFill {
		fillRes := childOp.applyFill()
		switch fillRes.mode {
		case svgPaintSolid:
			if willStroke {
				tmpR.dc.FillPreserve()
			} else {
				tmpR.dc.Fill()
			}
		case svgPaintShader:
			tmpR.fillPathWithShader(&childCtx, &sp.shape.Path, fillRes.shader, evenOdd)
			if willStroke {
				tmpR.dc.ClearPath()
				if !buildPathOnDC(tmpR.dc, &sp.shape.Path) {
					return
				}
			} else {
				tmpR.dc.ClearPath()
			}
		case svgPaintSkip:
			if willStroke {
				tmpR.dc.ClearPath()
				if !buildPathOnDC(tmpR.dc, &sp.shape.Path) {
					return
				}
			}
		}
	}
	if willStroke {
		strokeRes := childOp.applyStroke()
		switch strokeRes.mode {
		case svgPaintSolid:
			tmpR.dc.Stroke()
		case svgPaintShader:
			tmpR.strokePathWithShader(&childCtx, &sp.shape.Path, strokeRes.shader, sp.shape.Style)
			tmpR.dc.ClearPath()
		case svgPaintSkip:
			tmpR.dc.ClearPath()
		}
	}

	// Rasterize the mask. The mask's user-space reference box is the
	// shape's FillBoundingBox in user-space; the rasterizer needs
	// the buffer extent and the page-pixel offset (bx, by) so the
	// buffer alignment is consistent.
	maskBuf := sp.ctx.Renderer.rasterizeSVGMaskAtDeviceBBox(
		masker, userBBox, bw, bh, bx, by, sp.ctx.DeviceFromUser,
	)
	if maskBuf != nil {
		applySVGMaskToBuffer(buf, maskBuf, masker.MaskType)
	}

	// Composite the masked buffer back onto the page DC. The page DC
	// currently has all the ancestor transforms pushed (SVG root's
	// LocalToBorderBoxTransform + any container transforms). The
	// buffer was rasterized in device-pixel space at (bx, by) →
	// (bx+bw, by+bh); we need to draw it back at those absolute
	// device coords, NOT in user-space. Push a temporary identity
	// transform via dc.Push so DrawImage targets device pixels
	// directly.
	//
	// In louis14 the DrawContext.DrawImage takes raw integer pixel
	// coordinates (no transform applied), so we can simply call it
	// — but we still need to make sure no in-flight path lives on
	// the DC. ClearPath defensively.
	sp.ctx.dc.ClearPath()
	sp.ctx.Renderer.compositeMaskedShapeOnto(sp.ctx, buf, bx, by)
}

// compositeMaskedShapeOnto draws a pre-rasterized RGBA buffer
// (already in device pixels) onto the renderer's target image. The
// dc.DrawImage call route is fine, but we need to bypass any
// currently-active SVG transform stack since `buf`'s pixels already
// represent the device-space rasterization.
//
// Mirrors what paintLayerWithMask does at the end of its CSS branch
// (r.dc.DrawImage(buf, bx, by)); separated into a helper so the SVG
// shape mask path can call it without depending on render.go's
// internals.
func (r *Renderer) compositeMaskedShapeOnto(pctx *svgPaintContext, buf *image.RGBA, bx, by int) {
	// DrawImage's pixel offsets are in the target image's
	// coordinate system, which is the device-pixel framebuffer.
	// They're NOT routed through the DC's transform stack
	// (mazarin's DrawImage is a pixel blit). So `bx, by` go in
	// directly. The buffer was sized big enough to absorb sub-
	// pixel rounding; some of its pixels may fall outside the
	// page-DC clip, which the underlying DrawImage handles via
	// alpha-blend (out-of-bounds pixels are ignored).
	pctx.dc.DrawImage(buf, bx, by)
}

// rasterizeSVGMaskAtDeviceBBox is the SVG-shape variant of
// rasterizeSVGMask: same signature except the refBox is in USER
// space (the shape's FillBoundingBox) and we receive the existing
// DeviceFromUser so the mask children rasterize at the same scale
// as the shape did.
//
// Mirrors the CSS-box rasterizeSVGMask but operates in the SVG paint
// walk's user-space coordinate system.
func (r *Renderer) rasterizeSVGMaskAtDeviceBBox(masker *svg.SVGResourceMasker, refBoxUser geometry.RectF, bufW, bufH, bx, by int, deviceFromUser geometry.AffineTransform) *image.RGBA {
	if masker == nil || bufW <= 0 || bufH <= 0 {
		return nil
	}
	lengthCtx := svg.NewSVGLengthContext(geometry.SizeF{
		Width: refBoxUser.Width(), Height: refBoxUser.Height(),
	})
	// For SVG-shape mask references, the mask's coords resolve in
	// the shape's user-space, with the shape at user-space origin
	// (refBoxUser.X, refBoxUser.Y). Mirrors how Blink's mask
	// resolution uses the shape's ObjectBoundingBox in user-space.
	boxLocalRef := geometry.NewRectF(0, 0, refBoxUser.Width(), refBoxUser.Height())
	region := masker.ResourceBoundingBox(boxLocalRef, lengthCtx)

	maskBuf := image.NewRGBA(image.Rect(0, 0, bufW, bufH))
	tmpR := NewRendererForImage(maskBuf)
	// Buffer pixel (0, 0) = device pixel (bx, by). Compose the
	// device-pixel translate with the user→device map so the mask
	// children rasterize at the shape's effective scale.
	tmpR.dc.Translate(float64(-bx), float64(-by))
	if !deviceFromUser.IsIdentity() {
		tmpR.dc.MultiplyMatrix(
			deviceFromUser.A, deviceFromUser.B,
			deviceFromUser.C, deviceFromUser.D,
			deviceFromUser.E, deviceFromUser.F,
		)
	}
	// Box-local frame inside the user-space DC.
	tmpR.dc.Translate(refBoxUser.X(), refBoxUser.Y())

	var dfu geometry.AffineTransform
	pageFromBoxLocal := geometry.Translate(refBoxUser.X(), refBoxUser.Y())
	bufferFromPage := geometry.Translate(-float64(bx), -float64(by))
	switch masker.MaskContentUnits {
	case svg.SVGUnitObjectBoundingBox:
		tmpR.dc.MultiplyMatrix(refBoxUser.Width(), 0, 0, refBoxUser.Height(), 0, 0)
		dfu = bufferFromPage.Compose(deviceFromUser).Compose(pageFromBoxLocal).Compose(
			geometry.Scale(refBoxUser.Width(), refBoxUser.Height()),
		)
	default:
		dfu = bufferFromPage.Compose(deviceFromUser).Compose(pageFromBoxLocal)
	}

	// Clip to mask region (in the DC's current coord system —
	// box-local for userSpaceOnUse content, unit-square for
	// objectBoundingBox content).
	regionLocal := region
	if masker.MaskContentUnits == svg.SVGUnitObjectBoundingBox &&
		refBoxUser.Width() != 0 && refBoxUser.Height() != 0 {
		regionLocal = geometry.NewRectF(
			region.X()/refBoxUser.Width(),
			region.Y()/refBoxUser.Height(),
			region.Width()/refBoxUser.Width(),
			region.Height()/refBoxUser.Height(),
		)
	}
	tmpR.dc.DrawRectangle(regionLocal.X(), regionLocal.Y(), regionLocal.Width(), regionLocal.Height())
	tmpR.dc.Clip()

	pctx := &svgPaintContext{
		dc:               tmpR.dc,
		viewport:         geometry.NewRectF(0, 0, float64(bufW), float64(bufH)),
		CurrentTransform: geometry.Identity(),
		DeviceFromUser:   dfu,
		Resources:        r.lookupSVGResources(),
		Renderer:         tmpR,
	}
	for _, child := range masker.Children {
		paintSVGNode(pctx, child)
	}
	return maskBuf
}

// buildPathOnDC replays a normalized svg.Path onto a DrawContext
// using its primitive path-builder calls (MoveTo, LineTo, QuadraticTo,
// CubicTo, ClosePath). Returns false if the path is empty so the
// caller can skip the Fill/Stroke calls (a zero-segment path on dc
// triggers undefined behavior in some Cairo-style backends).
//
// The dc's current point and path state are clobbered. Callers should
// have already pushed the appropriate transform stack via dc.Push /
// dc.Translate / dc.MultiplyMatrix before invoking this.
func buildPathOnDC(dc textshape.DrawContext, p *svg.Path) bool {
	if p == nil || len(p.Segments) == 0 {
		return false
	}
	dc.ClearPath()
	for _, seg := range p.Segments {
		switch seg.Kind {
		case svg.SegMove:
			dc.MoveTo(seg.End.X, seg.End.Y)
		case svg.SegLine:
			dc.LineTo(seg.End.X, seg.End.Y)
		case svg.SegQuad:
			dc.QuadraticTo(seg.C1.X, seg.C1.Y, seg.End.X, seg.End.Y)
		case svg.SegCubic:
			dc.CubicTo(seg.C1.X, seg.C1.Y, seg.C2.X, seg.C2.Y, seg.End.X, seg.End.Y)
		case svg.SegClose:
			dc.ClosePath()
		}
	}
	return true
}
