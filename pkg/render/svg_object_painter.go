package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"

	"mazarin/textshape"
)

// svgObjectPainter resolves the SVG paint properties (fill, stroke,
// fill-opacity, stroke-opacity, stroke-width, stroke-linecap,
// stroke-linejoin, stroke-miterlimit, stroke-dasharray,
// stroke-dashoffset, fill-rule) for a single SVG node and applies
// them to the DrawContext as the painter is about to fill or stroke.
// Mirrors Blink's SVGObjectPainter::PreparePaint
// (core/paint/svg_object_painter.{h,cc}).
//
// The painter does NOT track its own state — it reads from style on
// each Apply* call and pushes the result onto dc. This matches Blink's
// "paint server applies on each draw" model and avoids cache-staleness
// bugs when style changes between phases.
//
// Phase 4 adds Resources/ReferenceBox/LengthContext — paint-server
// resolution needs the registry + the shape's bbox to evaluate
// objectBoundingBox-units geometry, plus a length context for
// userSpaceOnUse percent resolution. nil Resources falls back to
// "treat all `url(#…)` paints as unresolvable" — Phase 1-3 callers
// still work via the SVGPaintServer Fallback branch.
type svgObjectPainter struct {
	dc        textshape.DrawContext
	style     *css.Style
	resources *svg.SVGResourceRegistry
	refBox    geometry.RectF
	lengthCtx svg.SVGLengthContext
}

// newSVGObjectPainter binds an object painter to a DrawContext and a
// resolved computed style. style may be nil (in which case every
// accessor returns its SVG default — see pkg/css/svg_style.go).
//
// `resources` / `refBox` / `lengthCtx` are needed for Phase 4
// paint-server resolution; pre-Phase-4 callers can pass nil/zero
// values and the painter degrades to "no paint server resolution" —
// `url(#…)` fills then fall back to their `Fallback` color (if any)
// or skip the draw (SVG 2 §13.1 invalid-paint rule).
func newSVGObjectPainter(dc textshape.DrawContext, style *css.Style) *svgObjectPainter {
	return &svgObjectPainter{dc: dc, style: style}
}

// withResources extends the object painter with paint-server context.
// Called by the shape painter just before applyFill/applyStroke once
// the shape's reference bbox is known.
func (op *svgObjectPainter) withResources(resources *svg.SVGResourceRegistry, refBox geometry.RectF, lengthCtx svg.SVGLengthContext) *svgObjectPainter {
	op.resources = resources
	op.refBox = refBox
	op.lengthCtx = lengthCtx
	return op
}

// svgPaintMode enumerates how a single fill/stroke pass should
// rasterize. Phase 4 expands the binary "draw or skip" of Phase 2 to
// also cover paint-server fills.
type svgPaintMode int

const (
	// svgPaintSkip means the caller should not run the fill/stroke
	// pass for this style — either the paint is `none`, the alpha
	// resolved to 0, or the paint-server reference couldn't be
	// resolved and no fallback was supplied.
	svgPaintSkip svgPaintMode = iota
	// svgPaintSolid means the DrawContext has been configured with
	// a solid color and the caller should run dc.Fill()/dc.Stroke().
	svgPaintSolid
	// svgPaintShader means the caller should NOT call dc.Fill()/
	// dc.Stroke(); instead it should call the renderer's
	// fillPathWithShader / strokePathWithShader path with the
	// returned shader. The DC color is left untouched.
	svgPaintShader
)

// svgPaintResolution is the result of resolving a fill/stroke style
// into a concrete draw plan. Mirrors what Blink's
// SVGObjectPainter::PreparePaint returns to the SVGShapePainter:
// either a configured PaintFlags with a solid color, or a PaintFlags
// holding a Skia shader produced by the paint server's ApplyShader.
type svgPaintResolution struct {
	mode   svgPaintMode
	shader paintShader
}

// applyFill pushes the fill color (or transparent if `fill: none`)
// and the fill-rule onto the DrawContext, returning a resolution that
// tells the caller whether to call dc.Fill() (solid path), invoke the
// paint-server shader pipeline (shader path), or skip.
//
// Phase 4 paint-server resolution: Kind == Server looks up the server
// in op.resources. On hit, builds the shader (gradient/pattern) and
// returns svgPaintShader. On miss, falls back to paint.Fallback if
// supplied, else applies the SVG 2 §13.1 "treat as none" rule
// (svgPaintSkip).
func (op *svgObjectPainter) applyFill() svgPaintResolution {
	if op.style == nil {
		// Default: opaque black, fill-rule nonzero.
		setDCColor(op.dc, css.Color{A: 1.0})
		op.dc.SetFillRule(textshape.FillRuleWinding)
		return svgPaintResolution{mode: svgPaintSolid}
	}
	paint := op.style.GetFill()
	opacity := op.style.GetFillOpacity()
	// fill-rule applies regardless of which paint we end up using.
	switch op.style.GetFillRule() {
	case css.SVGFillRuleEvenOdd:
		op.dc.SetFillRule(textshape.FillRuleEvenOdd)
	default:
		op.dc.SetFillRule(textshape.FillRuleWinding)
	}
	return op.resolvePaint(paint, opacity, true)
}

// applyStroke pushes stroke geometry parameters (stroke-width,
// stroke-linecap, stroke-linejoin, stroke-dasharray, stroke-dashoffset)
// onto dc and returns a resolution describing the paint to apply. If
// the resolution mode is svgPaintShader the caller is expected to
// route the stroke through fillPathWithShader (yes, fill — the
// stroke-as-fill conversion is the standard technique: convert the
// stroke geometry into a filled outline path, then fill that with the
// shader). For Phase 4 we approximate this by stroking with a
// sentinel color into a coverage buffer; the path/stroke-width
// geometry already lives on the DC.
func (op *svgObjectPainter) applyStroke() svgPaintResolution {
	if op.style == nil {
		return svgPaintResolution{mode: svgPaintSkip} // SVG default for unset is `stroke: none`.
	}
	paint := op.style.GetStroke()
	w := op.style.GetStrokeWidth()
	if w <= 0 {
		return svgPaintResolution{mode: svgPaintSkip}
	}
	op.dc.SetLineWidth(w)

	switch op.style.GetStrokeLinecap() {
	case css.SVGLineCapRound:
		op.dc.SetLineCap(textshape.LineCapRound)
	case css.SVGLineCapSquare:
		op.dc.SetLineCap(textshape.LineCapSquare)
	default:
		op.dc.SetLineCap(textshape.LineCapButt)
	}

	switch op.style.GetStrokeLinejoin() {
	case css.SVGLineJoinRound:
		op.dc.SetLineJoin(textshape.LineJoinRound)
	case css.SVGLineJoinMiter:
		op.dc.SetLineJoin(textshape.LineJoinMiter)
	case css.SVGLineJoinBevel:
		op.dc.SetLineJoin(textshape.LineJoinBevel)
	}

	dashes := op.style.GetStrokeDashArray()
	if len(dashes) > 0 {
		// SVG 2 §13.4.4: an odd-element list is duplicated to
		// make the pattern even.
		if len(dashes)%2 == 1 {
			dashes = append(append([]float64(nil), dashes...), dashes...)
		}
		op.dc.SetDash(dashes...)
	} else {
		// Reset any prior dash.
		op.dc.SetDash()
	}

	// stroke-dashoffset is not yet plumbed through DrawContext;
	// mazarin's SetDash does not currently take an offset. Phase 4+
	// will follow up if a reftest in scope exercises a non-zero
	// dashoffset.
	_ = op.style.GetStrokeDashOffset()

	opacity := op.style.GetStrokeOpacity()
	return op.resolvePaint(paint, opacity, false)
}

// resolvePaint reduces a parsed css.SVGPaint to an svgPaintResolution.
// `opacity` is the per-property opacity (fill-opacity or
// stroke-opacity) that multiplies into the final alpha. `isFill`
// distinguishes the fill from the stroke pass for any future
// fill-specific resolution decisions; currently both follow the same
// SVG-2 invalid-paint rule.
func (op *svgObjectPainter) resolvePaint(paint css.SVGPaint, opacity float64, isFill bool) svgPaintResolution {
	switch paint.Kind {
	case css.SVGPaintNone:
		return svgPaintResolution{mode: svgPaintSkip}
	case css.SVGPaintColor:
		c := paint.Color
		if c.A <= 0 && (c.R|c.G|c.B) == 0 {
			return svgPaintResolution{mode: svgPaintSkip}
		}
		c.A *= opacity
		if c.A <= 0 {
			return svgPaintResolution{mode: svgPaintSkip}
		}
		setDCColor(op.dc, c)
		return svgPaintResolution{mode: svgPaintSolid}
	case css.SVGPaintServer:
		// Phase 4: look up the server in the registry. On hit, build
		// the per-pixel shader. On miss, fall back to paint.Fallback
		// if supplied (SVG 2 `url(#id) color` syntax), else apply the
		// "treat as none" rule.
		if op.resources != nil {
			if server, ok := op.resources.LookupAsPaintServer(paint.ServerID); ok {
				// Phase 6: a paint server in a reference cycle is
				// treated as `none`; fall through to the fallback
				// color (if any) or skip the draw.
				if server.CycleState() != svg.SVGCycleHasCycle {
					if shader, built := buildPaintShader(server, op.refBox, op.lengthCtx, opacity, op.resources); built {
						return svgPaintResolution{mode: svgPaintShader, shader: shader}
					}
				}
			}
		}
		if paint.Fallback != nil {
			return op.resolvePaint(*paint.Fallback, opacity, isFill)
		}
		return svgPaintResolution{mode: svgPaintSkip}
	}
	return svgPaintResolution{mode: svgPaintSkip}
}

// resolvePaintColor is retained for places that still want the
// solid-color reduction. Phase 4 paint-server resolution lives in
// resolvePaint instead; this thin wrapper preserves the Phase 2-era
// caller surface (currently none — all callers now go through
// applyFill/applyStroke).
func (op *svgObjectPainter) resolvePaintColor(paint css.SVGPaint) (css.Color, bool) {
	switch paint.Kind {
	case css.SVGPaintNone:
		return css.Color{}, false
	case css.SVGPaintColor:
		c := paint.Color
		if c.A <= 0 && (c.R|c.G|c.B) == 0 {
			return c, false
		}
		return c, true
	case css.SVGPaintServer:
		if paint.Fallback != nil {
			return op.resolvePaintColor(*paint.Fallback)
		}
		return css.Color{}, false
	}
	return css.Color{}, false
}
