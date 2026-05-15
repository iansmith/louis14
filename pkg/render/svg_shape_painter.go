package render

import (
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

	willFill := op.style != nil && op.style.GetFill().Kind != css.SVGPaintNone
	willStroke := op.style != nil && op.style.GetStroke().Kind != css.SVGPaintNone
	// Style was nil → defaults apply: fill: black, stroke: none.
	if op.style == nil {
		willFill = true
	}
	if !willFill && !willStroke {
		return
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
