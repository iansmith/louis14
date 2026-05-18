package render

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// linearGradientShader evaluates a linear gradient at each (userX, userY)
// in the shape's user-space coordinate system. Mirrors Blink's
// LayoutSVGResourceLinearGradient::BuildGradient → cc::PaintShader::MakeLinearGradient.
//
// The gradient is defined by a line from (x1, y1) to (x2, y2) in the
// gradient's INTERNAL coordinate system (objectBBox fractions or
// userSpaceOnUse user units). At each evaluated point P, the parameter
// t is the perpendicular projection of P onto that line, divided by
// the line's length. The stop ramp is then sampled at t.
//
// Pre-baked at shader-build time:
//   - p1, p2 are the gradient endpoints already mapped into USER space
//     (via resolveGradientPaintTransform).
//   - dirX/dirY is (p2 - p1).
//   - lineLenSq is |p2 - p1|² — pre-computed so SampleAt is one dot
//     product instead of a normalization.
//
// At sample time:
//
//	t = dot(P - p1, p2 - p1) / lineLenSq
//
// followed by applySpreadMethod(t) and sampleGradientStops.
type linearGradientShader struct {
	p1x, p1y   float64
	dirX, dirY float64
	lineLenSq  float64
	stops      []svg.SVGGradientStop
	spread     svg.SVGSpreadMethod
	opacity    float64
}

func (s *linearGradientShader) SampleAt(userX, userY float64) css.Color {
	if s.lineLenSq == 0 {
		// Degenerate: p1 == p2. SVG 1.1 §13.2.2 says paint with the
		// last stop's color. (Blink picks the last stop, not "skip
		// the paint" — confirmed by SkLinearGradient's degenerate
		// case.)
		if len(s.stops) == 0 {
			return css.Color{}
		}
		c := s.stops[len(s.stops)-1].Color
		c.A *= s.opacity
		return c
	}
	dx := userX - s.p1x
	dy := userY - s.p1y
	t := (dx*s.dirX + dy*s.dirY) / s.lineLenSq
	t = applySpreadMethod(t, s.spread)
	c := sampleGradientStops(s.stops, t)
	c.A *= s.opacity
	return c
}

// buildLinearGradientShader is the layout→render bridge for
// `<linearGradient>`. Resolves the endpoint attributes against the
// shape's reference box (objectBoundingBox mode) or the current length
// context (userSpaceOnUse mode), composes the gradientTransform on
// top, and returns a per-pixel shader.
//
// Spec defaults per SVG 1.1 §13.2.2: x1=0%, y1=0%, x2=100%, y2=0%.
// Empty stop lists return (nil, false) — SVG 1.1 says "the gradient
// has no effect", which the caller treats as a SKIPPED PAINT (so the
// fallback color, if any, applies via PreparePaint's normal path).
func buildLinearGradientShader(g *svg.SVGLinearGradient, referenceBox geometry.RectF, lengthCtx svg.SVGLengthContext, opacity float64) (paintShader, bool) {
	if g == nil || len(g.Stops) == 0 {
		return nil, false
	}
	// Spec defaults.
	x1 := g.X1.ResolveAgainst(g.GradientUnits, svg.LengthModeWidth, lengthCtx, defaultLinearX1(g.GradientUnits, lengthCtx))
	y1 := g.Y1.ResolveAgainst(g.GradientUnits, svg.LengthModeHeight, lengthCtx, defaultLinearY1(g.GradientUnits, lengthCtx))
	x2 := g.X2.ResolveAgainst(g.GradientUnits, svg.LengthModeWidth, lengthCtx, defaultLinearX2(g.GradientUnits, lengthCtx))
	y2 := g.Y2.ResolveAgainst(g.GradientUnits, svg.LengthModeHeight, lengthCtx, defaultLinearY2(g.GradientUnits, lengthCtx))

	// Compose into user-space. For objectBBox the (x1,y1)/(x2,y2)
	// values are fractions of bbox; for userSpaceOnUse they are user
	// units. resolveGradientPaintTransform builds the right
	// composition for each mode.
	userT := resolveGradientPaintTransform(g.GradientUnits, referenceBox, g.GradientTransform)
	p1 := userT.MapPoint(geometry.PointF{X: x1, Y: y1})
	p2 := userT.MapPoint(geometry.PointF{X: x2, Y: y2})

	dx := p2.X - p1.X
	dy := p2.Y - p1.Y
	return &linearGradientShader{
		p1x:       p1.X,
		p1y:       p1.Y,
		dirX:      dx,
		dirY:      dy,
		lineLenSq: dx*dx + dy*dy,
		stops:     g.Stops,
		spread:    g.SpreadMethod,
		opacity:   opacity,
	}, true
}

// defaultLinearX1/Y1/X2/Y2 return the spec defaults for
// `<linearGradient>` endpoints. For objectBoundingBox mode they are
// fractions (0, 0, 1, 0). For userSpaceOnUse, percent attributes
// declared without an attribute treat the missing value as the same
// percent — but with no attribute supplied we use the bbox-style
// fractions as if they were user units (length resolution mode `width`
// against the viewport reduces percentages to absolute units). Per SVG
// 1.1 §13.2.2 the defaults in userSpaceOnUse are 0/0/100%/0 — which
// when resolved against the viewport WIDTH gives us 0 / 0 /
// viewport.W / 0, matching the visual intent.
func defaultLinearX1(units svg.SVGUnitType, ctx svg.SVGLengthContext) float64 {
	return 0
}
func defaultLinearY1(units svg.SVGUnitType, ctx svg.SVGLengthContext) float64 {
	return 0
}
func defaultLinearX2(units svg.SVGUnitType, ctx svg.SVGLengthContext) float64 {
	switch units {
	case svg.SVGUnitObjectBoundingBox:
		return 1
	case svg.SVGUnitUserSpaceOnUse:
		// Spec default is 100% along x — resolve against viewport width.
		return ctx.ViewportSize().Width
	}
	return 1
}
func defaultLinearY2(units svg.SVGUnitType, ctx svg.SVGLengthContext) float64 {
	return 0
}

// radialGradientShader implements the SVG 2 §13.4.3 radial gradient
// model: a focal circle (fx, fy, fr) projected outward to a larger
// enclosing circle (cx, cy, r). At each point P the gradient parameter
// is the relative position of P along the line between the focal
// circle and the enclosing circle.
//
// SVG 1.1 was simpler — a focal POINT (fr=0). SVG 2 generalizes to a
// focal circle. The math below implements the SVG 2 form; setting fr=0
// recovers SVG 1.1 exactly.
//
// All geometry is in USER space (mapped through resolveGradientPaintTransform).
//
// Algorithm (matches Skia's SkTwoPointConicalGradient):
//
//	Solve a quadratic in `t` for the line through the focal at angle θ
//	from P, where t = 0 is the focal circle and t = 1 is the enclosing
//	circle. The point P is at parameter t. Then map t through spread
//	and stops.
//
// For the focal == center degenerate case (a "radial" gradient with
// no focal offset), the simpler `t = distance(P, center) / radius`
// applies — branch on it early to keep the fast path cheap.
type radialGradientShader struct {
	// Center / focal in user space.
	cx, cy float64
	r      float64
	fx, fy float64
	fr     float64

	// Pre-computed line vector cd = (cx-fx, cy-fy, r-fr); used by
	// the two-point conical eval.
	cdx, cdy float64
	cdr      float64
	a        float64
	inva     float64

	stops   []svg.SVGGradientStop
	spread  svg.SVGSpreadMethod
	opacity float64
}

func (s *radialGradientShader) SampleAt(userX, userY float64) css.Color {
	if len(s.stops) == 0 {
		return css.Color{}
	}
	// Two-point conical formulation (matches Pixman / Skia /
	// fogleman/gg/gradient.go).
	dx := userX - s.fx
	dy := userY - s.fy
	b := dx*s.cdx + dy*s.cdy + s.fr*s.cdr
	c := dx*dx + dy*dy - s.fr*s.fr

	var t float64
	if s.a == 0 {
		if b == 0 {
			return css.Color{}
		}
		t = 0.5 * c / b
	} else {
		discr := b*b - s.a*c
		if discr < 0 {
			// Outside the cone of influence — SVG 2 §13.4.3 says
			// "no paint" (transparent black) for pad/repeat/reflect
			// when the point isn't reachable.
			return css.Color{}
		}
		sqrtDiscr := math.Sqrt(discr)
		t0 := (b + sqrtDiscr) * s.inva
		t1 := (b - sqrtDiscr) * s.inva
		// Pick the larger valid t — the one where the ray from the
		// focal still progresses outward (per Pixman's convention).
		if t0 >= 0 {
			t = t0
		} else if t1 >= 0 {
			t = t1
		} else {
			return css.Color{}
		}
	}
	t = applySpreadMethod(t, s.spread)
	c2 := sampleGradientStops(s.stops, t)
	c2.A *= s.opacity
	return c2
}

// buildRadialGradientShader is the layout→render bridge for
// `<radialGradient>`. Resolves geometry attributes against the
// reference box (objectBBox mode) or length context (userSpaceOnUse),
// then composes gradientTransform.
//
// Spec defaults per SVG 1.1 §13.2.3 + SVG 2 §13.4.3:
//   - cx, cy: 50%
//   - r: 50%
//   - fx, fy: cx, cy (when absent)
//   - fr: 0
//
// Per SVG 1.1, an `r <= 0` makes the gradient degenerate to the
// last-stop color everywhere; we return (nil, false) so the painter
// uses the last stop's color via a separate fast path. But for
// simplicity and Blink parity we still build a shader that returns
// the last stop's color — Skia's degenerate case path.
func buildRadialGradientShader(g *svg.SVGRadialGradient, referenceBox geometry.RectF, lengthCtx svg.SVGLengthContext, opacity float64) (paintShader, bool) {
	if g == nil || len(g.Stops) == 0 {
		return nil, false
	}
	// Defaults: 0.5 in objectBBox mode (50% of bbox); 50% of viewport
	// in userSpaceOnUse mode.
	cx := g.Cx.ResolveAgainst(g.GradientUnits, svg.LengthModeWidth, lengthCtx, defaultRadialCx(g.GradientUnits, lengthCtx))
	cy := g.Cy.ResolveAgainst(g.GradientUnits, svg.LengthModeHeight, lengthCtx, defaultRadialCy(g.GradientUnits, lengthCtx))
	r := g.R.ResolveAgainst(g.GradientUnits, svg.LengthModeOther, lengthCtx, defaultRadialR(g.GradientUnits, lengthCtx))
	fx := cx
	fy := cy
	if g.HasFx {
		fx = g.Fx.ResolveAgainst(g.GradientUnits, svg.LengthModeWidth, lengthCtx, cx)
	}
	if g.HasFy {
		fy = g.Fy.ResolveAgainst(g.GradientUnits, svg.LengthModeHeight, lengthCtx, cy)
	}
	fr := g.Fr.ResolveAgainst(g.GradientUnits, svg.LengthModeOther, lengthCtx, 0)

	if r <= 0 {
		return nil, false
	}

	// Map (cx, cy, fx, fy) into user space using the units-aware
	// transform. Radii get scaled by the linear part of the
	// transform when in objectBBox mode (the bbox.w, bbox.h scale).
	// For userSpaceOnUse we apply gradientTransform's linear part.
	userT := resolveGradientPaintTransform(g.GradientUnits, referenceBox, g.GradientTransform)
	cpt := userT.MapPoint(geometry.PointF{X: cx, Y: cy})
	fpt := userT.MapPoint(geometry.PointF{X: fx, Y: fy})

	// Radius scaling: in objectBBox mode the radius is a fraction of
	// the bbox diagonal-ish (sqrt(w*h) is Blink's choice).
	// Mirrors LayoutSVGResourceRadialGradient::ResolveRadius.
	rScale := 1.0
	switch g.GradientUnits {
	case svg.SVGUnitObjectBoundingBox:
		// SVG 1.1 §7.11 (and Blink's LayoutSVGResourceRadialGradient)
		// scales objectBBox radii by sqrt(w*h) as the "diagonal"
		// scale factor.
		rScale = math.Sqrt(referenceBox.Width() * referenceBox.Height())
	case svg.SVGUnitUserSpaceOnUse:
		// Radii are already in user units; only the linear part of
		// gradientTransform scales them. For a uniform scale this is
		// sqrt(|det|).
		gt := g.GradientTransform
		det := math.Abs(gt.A*gt.D - gt.B*gt.C)
		if det != 0 {
			rScale = math.Sqrt(det)
		}
	}
	rUser := r * rScale
	frUser := fr * rScale

	// Two-point conical pre-computes.
	cdx := cpt.X - fpt.X
	cdy := cpt.Y - fpt.Y
	cdr := rUser - frUser
	a := cdx*cdx + cdy*cdy - cdr*cdr
	var inva float64
	if a != 0 {
		inva = 1.0 / a
	}

	return &radialGradientShader{
		cx:      cpt.X,
		cy:      cpt.Y,
		r:       rUser,
		fx:      fpt.X,
		fy:      fpt.Y,
		fr:      frUser,
		cdx:     cdx,
		cdy:     cdy,
		cdr:     cdr,
		a:       a,
		inva:    inva,
		stops:   g.Stops,
		spread:  g.SpreadMethod,
		opacity: opacity,
	}, true
}

func defaultRadialCx(units svg.SVGUnitType, ctx svg.SVGLengthContext) float64 {
	switch units {
	case svg.SVGUnitObjectBoundingBox:
		return 0.5
	case svg.SVGUnitUserSpaceOnUse:
		return ctx.ViewportSize().Width * 0.5
	}
	return 0.5
}
func defaultRadialCy(units svg.SVGUnitType, ctx svg.SVGLengthContext) float64 {
	switch units {
	case svg.SVGUnitObjectBoundingBox:
		return 0.5
	case svg.SVGUnitUserSpaceOnUse:
		return ctx.ViewportSize().Height * 0.5
	}
	return 0.5
}
func defaultRadialR(units svg.SVGUnitType, ctx svg.SVGLengthContext) float64 {
	switch units {
	case svg.SVGUnitObjectBoundingBox:
		return 0.5
	case svg.SVGUnitUserSpaceOnUse:
		// 50% of the normalized diagonal per SVG 1.1 §4.5.10.
		w, h := ctx.ViewportSize().Width, ctx.ViewportSize().Height
		return 0.5 * math.Sqrt((w*w+h*h)/2.0)
	}
	return 0.5
}
