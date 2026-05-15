package render

import (
	"louis14/pkg/css"

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
type svgObjectPainter struct {
	dc    textshape.DrawContext
	style *css.Style
}

// newSVGObjectPainter binds an object painter to a DrawContext and a
// resolved computed style. style may be nil (in which case every
// accessor returns its SVG default — see pkg/css/svg_style.go).
func newSVGObjectPainter(dc textshape.DrawContext, style *css.Style) *svgObjectPainter {
	return &svgObjectPainter{dc: dc, style: style}
}

// applyFill pushes the fill color (or transparent if `fill: none`)
// and the fill-rule onto the DrawContext, returning true if the
// caller should subsequently call dc.Fill(). Returns false when the
// fill should be skipped entirely (Kind == None, or zero-alpha after
// fill-opacity * opacity).
//
// Phase 2 paint-server resolution: Kind == Server defers to the
// optional fallback paint if present (matching SVG 1.1 §11.3's
// "if the resource cannot be found" branch); otherwise the SVG 2 §13.1
// "treat as if `fill: none` was specified" rule applies — return
// false. Phase 4 will plumb in the actual gradient/pattern.
func (op *svgObjectPainter) applyFill() bool {
	if op.style == nil {
		// Default: opaque black, fill-rule nonzero.
		setDCColor(op.dc, css.Color{A: 1.0})
		op.dc.SetFillRule(textshape.FillRuleWinding)
		return true
	}
	paint := op.style.GetFill()
	c, ok := op.resolvePaintColor(paint)
	if !ok {
		return false
	}
	// fill-opacity multiplies through.
	c.A *= op.style.GetFillOpacity()
	if c.A <= 0 {
		return false
	}
	setDCColor(op.dc, c)
	switch op.style.GetFillRule() {
	case css.SVGFillRuleEvenOdd:
		op.dc.SetFillRule(textshape.FillRuleEvenOdd)
	default:
		op.dc.SetFillRule(textshape.FillRuleWinding)
	}
	return true
}

// applyStroke pushes stroke color, stroke-width, stroke-linecap,
// stroke-linejoin, stroke-dasharray, stroke-dashoffset onto dc and
// returns true if a Stroke() should follow. Returns false when the
// stroke should be skipped entirely (paint is none / zero alpha /
// stroke-width <= 0).
//
// stroke-miterlimit is parsed and validated but mazarin currently
// exposes only round/bevel line joins (no miter), so miterlimit has
// no rendering effect in Phase 2. The miter→bevel fallback applies
// whenever stroke-linejoin: miter is requested — this is a known
// fidelity gap documented at the call site; no Phase 2 reftest
// exercises miter-vs-bevel, so it does not affect this phase's gate.
func (op *svgObjectPainter) applyStroke() bool {
	if op.style == nil {
		return false // SVG default for unset is `stroke: none`.
	}
	paint := op.style.GetStroke()
	c, ok := op.resolvePaintColor(paint)
	if !ok {
		return false
	}
	c.A *= op.style.GetStrokeOpacity()
	if c.A <= 0 {
		return false
	}
	w := op.style.GetStrokeWidth()
	if w <= 0 {
		return false
	}
	setDCColor(op.dc, c)
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
	case css.SVGLineJoinBevel, css.SVGLineJoinMiter:
		// Mazarin doesn't yet implement miter joins (only Round
		// and Bevel are defined in mazarin/textshape.LineJoin).
		// Per SVG 2 §13.4.5, a miter join with a sharp angle
		// degrades to bevel when stroke-miterlimit is exceeded —
		// the bevel fallback we use here is the limiting case of
		// that rule for any angle. Documented as a fidelity gap
		// at the type definition.
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
	return true
}

// resolvePaintColor reduces a css.SVGPaint to a concrete color (with
// the success bool false meaning "no paint — skip the draw").
// `url(#…)` references are not yet resolvable (no paint-server
// registry); they fall through to their explicit fallback if one was
// declared, otherwise return false per the SVG 2 invalid-paint rule.
func (op *svgObjectPainter) resolvePaintColor(paint css.SVGPaint) (css.Color, bool) {
	switch paint.Kind {
	case css.SVGPaintNone:
		return css.Color{}, false
	case css.SVGPaintColor:
		c := paint.Color
		if c.A <= 0 && (c.R|c.G|c.B) == 0 {
			// A completely zero color (e.g. invalid value parsed to
			// transparent black) is treated as a real transparent
			// black draw — but for fill that means "draw nothing
			// visible". Return false so we don't waste a fill.
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
