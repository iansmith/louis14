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

// shouldAntiAliasSVGShape mirrors SVGShapePainter::PaintShape's
// should_anti_alias (svg_shape_painter.cc:98-102 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): shape-rendering crispEdges
// and optimizeSpeed turn anti-aliasing off for the fill, stroke and
// clip-mask flags (:104-155).
func shouldAntiAliasSVGShape(style *css.Style) bool {
	if style == nil {
		return true
	}
	sr := style.ShapeRendering()
	return sr != css.ShapeRenderingCrispEdges && sr != css.ShapeRenderingOptimizeSpeed
}

// paint executes the fill-then-stroke sequence for the shape. The
// path is added to dc only once and reused via FillPreserve /
// StrokePreserve when both passes are active, so the path
// rasterizer's work is shared.
func (sp *svgShapePainter) paint() {
	// An empty shape paints nothing, its filters included, as
	// SVGShapePainter::Paint returns for IsShapeEmpty (svg_shape_painter.cc:
	// 47 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	if sp.shape == nil || sp.shape.Path.Empty() {
		return
	}

	// The shape's transform (svg.ElementTransform: its CSS transform, or
	// its SVG transform attribute), as for a container. The scope guard wraps
	// the filter, the path build, fill and stroke so rasterization happens in
	// the transformed coordinate space, and the filter is built in it.
	// Mirrors Blink's SVGShapePainter applying LocalSVGTransform via a
	// ScopedSVGTransformState, and the filter's local transform space
	// (buildSVGElementFilter).
	style := sp.shape.Style
	effective := svg.ElementTransform(sp.shape, style, sp.shape.Viewport)
	if !effective.IsIdentity() {
		guard := pushScopedSVGTransform(sp.ctx, effective, geometry.RectF{})
		defer guard.Close()
	}

	// SVG-level filter: url(#id), url(doc#id), CSS filter functions, or a
	// mix. Per CSS Masking 1 §3.7 rendering order is filter → clip → mask →
	// composite, so the filter path paints the shape into an offscreen
	// buffer the graph consumes and clips and masks the output. A filter
	// that does not resolve falls through to the unfiltered paint below,
	// the long-standing `filter="url(#missing)"` fallback.
	if style != nil && !sp.ctx.clipPathContent {
		if ops := style.GetFilter(); len(ops) > 0 && sp.paintWithFilter(ops) {
			return
		}
	}

	dc := sp.ctx.dc

	// The SVG layout pass attaches *css.Style to each SVGShape via
	// the StyleResolver callback (see svg_root_algorithm.go); the
	// painter reads it directly. A nil style means the shape's
	// element didn't get cascade attention — fall back to SVG
	// property defaults inside the object painter.
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
	if !styleIsVisible(style) {
		return
	}

	// The shape's fill, stroke and clip-mask flags anti-alias unless
	// shape-rendering asks for crisp edges or speed (a GraphicsContext
	// state scope, as GraphicsContextStateSaver).
	dc.Push()
	defer dc.Pop()
	dc.SetAntiAlias(shouldAntiAliasSVGShape(style))

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

	willFill, willStroke := svgPaints(op.style)
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
	if style != nil && !sp.ctx.clipPathContent {
		if maskVal := style.GetMaskImage(); maskVal != "" && maskVal != "none" {
			if id, ok := css.ParseURLReference(maskVal); ok {
				if sp.ctx.Resources != nil {
					if masker, found := sp.ctx.Resources.LookupMasker(id); found && masker != nil && masker.CycleState() != svg.SVGCycleHasCycle {
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
// mask's MaskUnits and MaskContentUnits resolve through
// rasterizeSVGElementMask against the fill box (ReferenceBoxForEffects).
func (sp *svgShapePainter) paintWithSVGMask(masker *svg.SVGResourceMasker, op *svgObjectPainter, willFill, willStroke bool) {
	// Compute the shape's bbox in DEVICE pixels. The shape's
	// FillBoundingBox is in user-space; map it through the draw context's
	// matrix (toDevice, below).
	userBBox := sp.shape.FillBoundingBox
	if !willStroke && userBBox.IsEmpty() {
		return
	}
	// Expand for stroke geometry if needed.
	if willStroke && sp.shape.Style != nil {
		userBBox = userBBox.Outset(sp.shape.Style.GetStrokeWidth())
	}
	// The shape and its mask rasterize in device pixels, through the draw
	// context's whole matrix (an ancestor CSS transform or perspective
	// included), so the buffer composites as is. Its device bbox is the part
	// of the shape in front of the camera (projectRectInFront), under the
	// clip. Paint servers map user space through the matrix's affine part,
	// which is exact only for an affine matrix (LOU-778).
	toDevice := drawContextTransform(sp.ctx.dc)
	deviceBBox, ok := projectRectInFront(toDevice, userBBox)
	if !ok {
		return
	}
	buffer := image.Rect(
		int(math.Floor(deviceBBox.X())), int(math.Floor(deviceBBox.Y())),
		int(math.Ceil(deviceBBox.Right()))+1, int(math.Ceil(deviceBBox.Bottom()))+1,
	).Intersect(sp.ctx.dc.ClipBounds())
	bx, by, bw, bh := buffer.Min.X, buffer.Min.Y, buffer.Dx(), buffer.Dy()
	if !effectBufferUsable(bw, bh) {
		return
	}

	// Render the shape's fill/stroke into a fresh buffer with the
	// SAME transform stack the page DC has, so the rasterized
	// geometry pixel-aligns with what DrawImage will composite back.
	// Paint-server fills and strokes run through the buffer's paint
	// context instead of the page target.
	buf, childCtx := newSVGEffectSource(sp.ctx, buffer, toDevice)
	childOp := newSVGObjectPainter(childCtx.dc, op.style).withResources(
		op.resources, op.refBox, op.lengthCtx,
	)
	if !sp.rasterizeShape(&childCtx, childOp, willFill, willStroke, sp.shape.Style) {
		return
	}

	// Rasterize the mask against the shape's fill box, its reference box
	// for effects, into a buffer aligned with the shape's: the same extent
	// and device-pixel offset (bx, by).
	maskBuf := sp.ctx.Renderer.rasterizeSVGElementMask(
		masker, sp.shape.FillBoundingBox, sp.ctx.viewport.Size, bw, bh, bx, by, toDevice,
	)
	if maskBuf != nil {
		applySVGMaskToBuffer(buf, maskBuf, masker.MaskType)
	}

	// Composite the masked buffer back at (bx, by), the device position
	// it was rasterized at. Clear any in-flight path first.
	sp.ctx.dc.ClearPath()
	compositeEffectBufferOnto(sp.ctx.dc, toDevice, buf, bx, by)
}

// compositeEffectBufferOnto draws buf, rasterized through toBuffer (a map
// from dc's current space into buf's pixels), onto dc at buffer pixel
// (bx, by): toBuffer is undone for the blit. A masked shape's buffer is
// rasterized through dc's whole matrix, so what is left is the identity to
// rounding: an affine one makes the blit a pixel copy (under the clip); under
// a projective dc the blit is still a homography resample, through a
// near-identity one, which matches the unmasked shape to within a unit per
// channel (TestSVG_PerspectiveMaskedShapeUnchanged). A filter's buffer is
// in its layer, so what is left is the layer's remainder (fLayerToDevMatrix:
// svgElementFilter.paint).
//
// no Blink analog: Blink records the masked or filtered content into the
// paint operation stream under the effect; louis14 composites a
// pre-rasterized buffer.
func compositeEffectBufferOnto(dc textshape.DrawContext, toBuffer geometry.Transform, buf *image.RGBA, bx, by int) {
	dc.Push()
	defer dc.Pop()
	multiplyDCHomography(dc, projectiveHomography(homographyTransform(toBuffer).InverseOrIdentity()))
	dc.DrawImage(buf, bx, by)
}

// rasterizeSVGElementMask rasterizes masker for an SVG element into a
// bufW×bufH buffer whose pixel (0, 0) is (bx, by) in the frame toDevice maps
// the element's user space into. referenceBox is the element's reference box
// for effects in user space (SVGResources::ReferenceBoxForEffects), viewport
// the size of its nearest <svg> viewport.
//
// Mirrors Blink's mask on an SVG element: the mask region is resolved by
// LayoutSVGResourceContainer::ResolveRectangle, fractions of the reference
// box for objectBoundingBox and user-space lengths with percentages of the
// viewport for userSpaceOnUse (layout_svg_resource_container.cc:115-137 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the content is drawn in user
// space, translated and scaled to the reference box for objectBoundingBox
// maskContentUnits (MaskToContentTransform, svg_mask_painter.cc:34-47).
func (r *Renderer) rasterizeSVGElementMask(masker *svg.SVGResourceMasker, referenceBox geometry.RectF, viewport geometry.SizeF, bufW, bufH, bx, by int, toDevice geometry.Transform) *image.RGBA {
	if masker == nil || bufW <= 0 || bufH <= 0 {
		return nil
	}
	region := masker.ResourceBoundingBox(referenceBox, svg.NewSVGLengthContext(viewport))

	maskBuf := image.NewRGBA(image.Rect(0, 0, bufW, bufH))
	tmpR := r.newChildRenderer(maskBuf)
	// Buffer pixel (0, 0) is (bx, by); the element's user space maps through
	// toDevice. The mask region clips in user space.
	tmpR.dc.Translate(float64(-bx), float64(-by))
	multiplyDCHomography(tmpR.dc, projectiveHomography(toDevice))
	tmpR.dc.DrawRectangle(region.X(), region.Y(), region.Width(), region.Height())
	tmpR.dc.Clip()

	// The mask content's paint servers map user space through the matrix's
	// affine part (LOU-778 under a projective one).
	dfu := geometry.Translate(-float64(bx), -float64(by)).Compose(geometry.AffineTransformFromTransform(toDevice))
	if masker.MaskContentUnits == svg.SVGUnitObjectBoundingBox {
		content := geometry.Translate(referenceBox.X(), referenceBox.Y()).Compose(
			geometry.Scale(referenceBox.Width(), referenceBox.Height()))
		tmpR.dc.MultiplyMatrix(content.A, content.B, content.C, content.D, content.E, content.F)
		dfu = dfu.Compose(content)
	}

	pctx := newBufferPaintContext(tmpR, geometry.SizeF{Width: float64(bufW), Height: float64(bufH)}, dfu, r.lookupSVGResources())
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
