package render

import (
	"image"
	"image/color"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"

	"mazarin/textshape"
)

// paintShader is a per-pixel paint-color generator. Given a point in
// the shape's USER-space coordinate system (the coordinate system the
// path was built in), returns the paint color to apply at that point.
// Mirrors the abstraction Skia exposes via cc::PaintShader — louis14
// can't push a real shader onto the DrawContext, so we sample one
// pixel at a time inside the path's coverage mask.
//
// `userX`/`userY` are FLOAT user-space coordinates; the shader resolves
// the gradient or pattern at that point and returns a straight-alpha
// color. The returned color may have A == 0 to indicate "no paint at
// this pixel" (e.g. a `pad`-spread radial gradient with the focal
// outside the cone).
type paintShader interface {
	// SampleAt returns the paint color at the given USER-space point.
	// Coordinates are floats; the shader applies the inverse
	// paint-coordinate transform internally before evaluating the
	// gradient ramp or sampling the pattern tile.
	SampleAt(userX, userY float64) css.Color
}

// buildPaintShader constructs a paintShader for the resolved paint
// server. Returns (nil, false) when the server is unsupported or has
// no effect (e.g. a gradient with 0 or 1 stops, per SVG 1.1 §13.2.4).
//
// `referenceBox` is the path's user-space FillBoundingBox — used to
// resolve objectBoundingBox-units coordinates. `referenceCtx` is the
// length context the gradient was declared in — used for
// userSpaceOnUse percent resolution. `opacity` is the multiplicative
// alpha to fold into the sampled color (fill-opacity * opacity for the
// fill path; stroke-opacity * opacity for the stroke path).
func buildPaintShader(server svg.SVGResourcePaintServer, referenceBox geometry.RectF, lengthCtx svg.SVGLengthContext, opacity float64, resources *svg.SVGResourceRegistry) (paintShader, bool) {
	if server == nil {
		return nil, false
	}
	switch g := server.(type) {
	case *svg.SVGLinearGradient:
		return buildLinearGradientShader(g, referenceBox, lengthCtx, opacity)
	case *svg.SVGRadialGradient:
		return buildRadialGradientShader(g, referenceBox, lengthCtx, opacity)
	case *svg.SVGPattern:
		// Phase 6: thread the umbrella registry into the pattern's
		// content rendering so url(#…) refs INSIDE a pattern resolve.
		return buildPatternShader(g, referenceBox, lengthCtx, opacity, resources)
	}
	return nil, false
}

// fillPathWithShader rasterizes the path through a child DC into a
// coverage buffer at the path's device-space bounds, then composites
// the per-pixel paint color (sampled by `shader`) over `target` using
// the coverage as alpha. Mirrors Blink's
// SVGShapePainter::FillShape({…, paint_server.ApplyShader(...) → cc::PaintFlags})
// — except that louis14's DC backend doesn't carry a shader on the
// paint, so we materialize it ourselves.
//
// `pctx.DeviceFromUser` is the device←user transform; its inverse
// gives us back user-space coordinates from device pixels, which the
// shader needs to evaluate its gradient ramp / pattern tile.
//
// `path` is the user-space SVG path; `evenOdd` selects the fill rule.
func (r *Renderer) fillPathWithShader(pctx *svgPaintContext, path *svg.Path, shader paintShader, evenOdd bool) {
	if r == nil || pctx == nil || path == nil || shader == nil {
		return
	}
	target := r.target
	if target == nil {
		return
	}
	// Compute the path's device-space bounds. mapBBoxAffine handles
	// the per-corner mapping needed when the transform has rotation /
	// skew.
	userBBox := pathBoundingBoxUser(path)
	if userBBox.IsEmpty() {
		return
	}
	deviceBBox := pctx.DeviceFromUser.MapRect(userBBox)
	if deviceBBox.IsEmpty() {
		return
	}

	// Round the device bbox out to integer pixel rect. Add a small
	// margin so we don't crop the path's antialiased edges.
	const edgeMargin = 1
	x0 := int(math.Floor(deviceBBox.X())) - edgeMargin
	y0 := int(math.Floor(deviceBBox.Y())) - edgeMargin
	x1 := int(math.Ceil(deviceBBox.Right())) + edgeMargin
	y1 := int(math.Ceil(deviceBBox.Bottom())) + edgeMargin

	// Clip to target bounds.
	tb := target.Bounds()
	if x0 < tb.Min.X {
		x0 = tb.Min.X
	}
	if y0 < tb.Min.Y {
		y0 = tb.Min.Y
	}
	if x1 > tb.Max.X {
		x1 = tb.Max.X
	}
	if y1 > tb.Max.Y {
		y1 = tb.Max.Y
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}

	// Create a coverage buffer big enough to hold the device-space
	// region. The buffer is full-target-sized to keep the offscreen
	// DC's coordinate system identical to the main DC's — that way
	// we don't need to translate the path or re-derive transforms.
	// Mirrors how paintLayerWithMask sizes its offscreen buffer to
	// the element's box rather than to the whole canvas.
	coverage := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	// Build a child DC pointed at the coverage buffer.
	childDC := r.dc.NewChildContext(coverage)
	// Translate so device pixel (x0,y0) maps to coverage pixel (0,0).
	childDC.Translate(float64(-x0), float64(-y0))
	// Reapply the current DC's transform stack onto the child. The
	// child inherits the host DC's matrix in louis14's DrawContext —
	// confirm with a Push so the SVG transform layered on the host is
	// already baked in. The DeviceFromUser composition we're about to
	// apply replicates that explicitly so we don't depend on which
	// transforms the child DC inherited:
	t := pctx.DeviceFromUser
	if !t.IsIdentity() {
		childDC.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}
	// Rasterize the path into the coverage buffer with opaque white.
	childDC.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if evenOdd {
		childDC.SetFillRule(textshape.FillRuleEvenOdd)
	} else {
		childDC.SetFillRule(textshape.FillRuleWinding)
	}
	// The path has already been built on the parent DC by
	// svgShapePainter; the child DC needs its own copy.
	if !buildPathOnDC(childDC, path) {
		return
	}
	childDC.Fill()

	// Invert DeviceFromUser to map device pixel center → user space
	// for shader sampling.
	userFromDevice, ok := pctx.DeviceFromUser.Invert()
	if !ok {
		return
	}

	// Composite shader * coverage over target with source-over.
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			covIdx := coverage.PixOffset(px-x0, py-y0)
			cov := coverage.Pix[covIdx+3]
			if cov == 0 {
				continue
			}
			// Sample the shader at the device-pixel center mapped to
			// user space. Using +0.5 mirrors gg's gradient sampler
			// (pixel centers).
			fx, fy := float64(px)+0.5, float64(py)+0.5
			ux := userFromDevice.A*fx + userFromDevice.C*fy + userFromDevice.E
			uy := userFromDevice.B*fx + userFromDevice.D*fy + userFromDevice.F
			pc := shader.SampleAt(ux, uy)
			if pc.A <= 0 {
				continue
			}
			// Composite: source = pc * (cov/255); dest source-over.
			sa := pc.A * (float64(cov) / 255.0)
			if sa <= 0 {
				continue
			}
			sr := float64(pc.R) * sa
			sg := float64(pc.G) * sa
			sb := float64(pc.B) * sa
			inv := 1.0 - sa
			di := target.PixOffset(px, py)
			dr := float64(target.Pix[di+0])
			dg := float64(target.Pix[di+1])
			db := float64(target.Pix[di+2])
			da := float64(target.Pix[di+3])
			target.Pix[di+0] = uint8(math.Round(sr + dr*inv))
			target.Pix[di+1] = uint8(math.Round(sg + dg*inv))
			target.Pix[di+2] = uint8(math.Round(sb + db*inv))
			target.Pix[di+3] = uint8(math.Round(sa*255 + da*inv))
		}
	}
}

// strokePathWithShader rasterizes the path STROKED (with the style's
// stroke-width / linejoin / linecap / dasharray) into a coverage
// buffer, then composites the shader over `target`. Mirrors
// Blink's SVGShapePainter::StrokeShape with a paint-server shader.
//
// The stroke geometry parameters have already been applied to the
// host DC by applyStroke; we use a child DC that inherits those
// parameters explicitly so the offscreen stroke matches the
// would-be solid-color stroke pixel-for-pixel.
func (r *Renderer) strokePathWithShader(pctx *svgPaintContext, path *svg.Path, shader paintShader, style *css.Style) {
	if r == nil || pctx == nil || path == nil || shader == nil || style == nil {
		return
	}
	target := r.target
	if target == nil {
		return
	}
	userBBox := pathBoundingBoxUser(path)
	if userBBox.IsEmpty() {
		return
	}
	// Expand bbox by stroke half-width to capture stroke pixels
	// outside the fill bbox. Account for joins/miters by adding
	// stroke-width on each side (conservative).
	sw := style.GetStrokeWidth()
	half := sw
	userBBox = geometry.NewRectF(
		userBBox.X()-half, userBBox.Y()-half,
		userBBox.Width()+2*half, userBBox.Height()+2*half,
	)
	deviceBBox := pctx.DeviceFromUser.MapRect(userBBox)
	if deviceBBox.IsEmpty() {
		return
	}

	const edgeMargin = 1
	x0 := int(math.Floor(deviceBBox.X())) - edgeMargin
	y0 := int(math.Floor(deviceBBox.Y())) - edgeMargin
	x1 := int(math.Ceil(deviceBBox.Right())) + edgeMargin
	y1 := int(math.Ceil(deviceBBox.Bottom())) + edgeMargin
	tb := target.Bounds()
	if x0 < tb.Min.X {
		x0 = tb.Min.X
	}
	if y0 < tb.Min.Y {
		y0 = tb.Min.Y
	}
	if x1 > tb.Max.X {
		x1 = tb.Max.X
	}
	if y1 > tb.Max.Y {
		y1 = tb.Max.Y
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}

	coverage := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	childDC := r.dc.NewChildContext(coverage)
	childDC.Translate(float64(-x0), float64(-y0))
	t := pctx.DeviceFromUser
	if !t.IsIdentity() {
		childDC.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}
	childDC.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	childDC.SetLineWidth(sw)
	switch style.GetStrokeLinecap() {
	case css.SVGLineCapRound:
		childDC.SetLineCap(textshape.LineCapRound)
	case css.SVGLineCapSquare:
		childDC.SetLineCap(textshape.LineCapSquare)
	default:
		childDC.SetLineCap(textshape.LineCapButt)
	}
	switch style.GetStrokeLinejoin() {
	case css.SVGLineJoinRound:
		childDC.SetLineJoin(textshape.LineJoinRound)
	case css.SVGLineJoinBevel, css.SVGLineJoinMiter:
		childDC.SetLineJoin(textshape.LineJoinBevel)
	}
	dashes := style.GetStrokeDashArray()
	if len(dashes) > 0 {
		if len(dashes)%2 == 1 {
			dashes = append(append([]float64(nil), dashes...), dashes...)
		}
		childDC.SetDash(dashes...)
	}
	if !buildPathOnDC(childDC, path) {
		return
	}
	childDC.Stroke()

	userFromDevice, ok := pctx.DeviceFromUser.Invert()
	if !ok {
		return
	}
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			covIdx := coverage.PixOffset(px-x0, py-y0)
			cov := coverage.Pix[covIdx+3]
			if cov == 0 {
				continue
			}
			fx, fy := float64(px)+0.5, float64(py)+0.5
			ux := userFromDevice.A*fx + userFromDevice.C*fy + userFromDevice.E
			uy := userFromDevice.B*fx + userFromDevice.D*fy + userFromDevice.F
			pc := shader.SampleAt(ux, uy)
			if pc.A <= 0 {
				continue
			}
			sa := pc.A * (float64(cov) / 255.0)
			if sa <= 0 {
				continue
			}
			sr := float64(pc.R) * sa
			sg := float64(pc.G) * sa
			sb := float64(pc.B) * sa
			inv := 1.0 - sa
			di := target.PixOffset(px, py)
			dr := float64(target.Pix[di+0])
			dg := float64(target.Pix[di+1])
			db := float64(target.Pix[di+2])
			da := float64(target.Pix[di+3])
			target.Pix[di+0] = uint8(math.Round(sr + dr*inv))
			target.Pix[di+1] = uint8(math.Round(sg + dg*inv))
			target.Pix[di+2] = uint8(math.Round(sb + db*inv))
			target.Pix[di+3] = uint8(math.Round(sa*255 + da*inv))
		}
	}
}

// pathBoundingBoxUser returns the AABB of a path's control points in
// user space. Conservative — for cubic/quadratic curves it uses the
// control polygon, which encloses the curve. Mirrors what Blink
// computes as the fill bounding box at this stage (the tighter
// extrema-based bbox would matter for stroke joining, not for
// rasterization scope).
func pathBoundingBoxUser(p *svg.Path) geometry.RectF {
	if p == nil || len(p.Segments) == 0 {
		return geometry.RectF{}
	}
	hasAny := false
	var minX, minY, maxX, maxY float64
	consider := func(x, y float64) {
		if !hasAny {
			minX, minY, maxX, maxY = x, y, x, y
			hasAny = true
			return
		}
		if x < minX {
			minX = x
		}
		if y < minY {
			minY = y
		}
		if x > maxX {
			maxX = x
		}
		if y > maxY {
			maxY = y
		}
	}
	for _, seg := range p.Segments {
		switch seg.Kind {
		case svg.SegMove, svg.SegLine:
			consider(seg.End.X, seg.End.Y)
		case svg.SegQuad:
			consider(seg.C1.X, seg.C1.Y)
			consider(seg.End.X, seg.End.Y)
		case svg.SegCubic:
			consider(seg.C1.X, seg.C1.Y)
			consider(seg.C2.X, seg.C2.Y)
			consider(seg.End.X, seg.End.Y)
		case svg.SegClose:
			// no new point
		}
	}
	if !hasAny {
		return geometry.RectF{}
	}
	return geometry.NewRectF(minX, minY, maxX-minX, maxY-minY)
}

// applySpreadMethod maps a [potentially out-of-range] gradient
// parameter `t` into the [0, 1] range according to the SVG
// spreadMethod, per SVG 1.1 §13.2.2 + §13.2.3:
//
//   - SVGSpreadPad: clamp to [0, 1].
//   - SVGSpreadReflect: fold at every integer boundary (the function
//     |((t mod 2) - 1) - 1| isn't quite right; the standard sawtooth
//     formula is: t' = t mod 2, if t' > 1 then 2 - t', else t').
//   - SVGSpreadRepeat: fract(t).
//
// Negative inputs are normalized to the positive range first.
func applySpreadMethod(t float64, method svg.SVGSpreadMethod) float64 {
	switch method {
	case svg.SVGSpreadPad:
		if t < 0 {
			return 0
		}
		if t > 1 {
			return 1
		}
		return t
	case svg.SVGSpreadReflect:
		// Fold into [0, 2), then mirror the upper half to [0, 1].
		t = math.Mod(t, 2)
		if t < 0 {
			t += 2
		}
		if t > 1 {
			return 2 - t
		}
		return t
	case svg.SVGSpreadRepeat:
		t = math.Mod(t, 1)
		if t < 0 {
			t += 1
		}
		return t
	}
	return t
}

// sampleGradientStops returns the interpolated color at parameter `t`
// in [0, 1] over the supplied sorted stop list. Single-stop lists
// degenerate to the constant color. Empty stop lists return a fully
// transparent black (callers should reject empty-stop gradients
// earlier, but this is a safe default).
//
// Stop colors are interpolated in straight (non-premultiplied) sRGB
// space — matching the existing CSS gradient code in gradient.go and
// Blink's default gradient color space (no `color-interpolation`
// override; Phase 4 doesn't plumb that property).
func sampleGradientStops(stops []svg.SVGGradientStop, t float64) css.Color {
	n := len(stops)
	if n == 0 {
		return css.Color{}
	}
	if n == 1 {
		return stops[0].Color
	}
	if t <= stops[0].Offset {
		return stops[0].Color
	}
	if t >= stops[n-1].Offset {
		return stops[n-1].Color
	}
	for i := 1; i < n; i++ {
		if t <= stops[i].Offset {
			a := stops[i-1]
			b := stops[i]
			span := b.Offset - a.Offset
			if span <= 0 {
				// Hard stop at the same offset — the "after" color
				// applies. Matches CSS hard-stop semantics.
				return b.Color
			}
			f := (t - a.Offset) / span
			return css.Color{
				R: lerpU8(a.Color.R, b.Color.R, f),
				G: lerpU8(a.Color.G, b.Color.G, f),
				B: lerpU8(a.Color.B, b.Color.B, f),
				A: a.Color.A + (b.Color.A-a.Color.A)*f,
			}
		}
	}
	return stops[n-1].Color
}

func lerpU8(a, b uint8, t float64) uint8 {
	v := float64(a) + (float64(b)-float64(a))*t
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(math.Round(v))
}

// resolveGradientPaintTransform builds the affine that maps the
// gradient's INTERNAL coordinate system (the (x1,y1)→(x2,y2) line for
// linear; the (cx,cy)/(fx,fy)/r system for radial) into the shape's
// USER-space coordinate system.
//
// For SVGUnitObjectBoundingBox: gradient internal coords are fractions
// of the reference bbox. Composition is:
//
//	userFromGradient = translate(bbox.x, bbox.y) ·
//	                   scale(bbox.w, bbox.h) ·
//	                   gradientTransform
//
// For SVGUnitUserSpaceOnUse: gradient internal coords ARE user-space
// coords already; only gradientTransform applies.
//
// Mirrors Blink's GradientAttributes::ComputeUserSpaceOnUseAttributes /
// ResolveGradientAttributes splitting in LayoutSVGResourceGradient.
func resolveGradientPaintTransform(units svg.SVGUnitType, bbox geometry.RectF, gradientTransform geometry.AffineTransform) geometry.AffineTransform {
	switch units {
	case svg.SVGUnitObjectBoundingBox:
		// translate(bbox.x, bbox.y) ∘ scale(bbox.w, bbox.h) — same
		// composition order as Compose: outer first.
		bboxT := geometry.Translate(bbox.X(), bbox.Y()).Compose(
			geometry.Scale(bbox.Width(), bbox.Height()),
		)
		return bboxT.Compose(gradientTransform)
	case svg.SVGUnitUserSpaceOnUse:
		return gradientTransform
	}
	return gradientTransform
}
