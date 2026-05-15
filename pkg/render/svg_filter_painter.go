package render

import (
	"image"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/graphics/filters"
	"louis14/pkg/layout/svg"
)

// paintWithSVGFilter dispatches an SVG shape through its referenced
// `<filter>` element. Mirrors Blink's SVGShapePainter routing through
// SVGFilterPainter when the shape carries a `filter` reference.
//
// Algorithm (matches Blink's FilterPainter::Paint):
//  1. Resolve the filter region from the shape's bounding box +
//     filterUnits. (Both inputs in device pixels.)
//  2. Allocate an RGBA buffer sized to the filter region.
//  3. Repaint the shape's fill/stroke into the buffer, with a
//     transform that aligns the shape's user-space origin to the
//     buffer's pixel origin (filterRegion.Min in device pixels).
//  4. Build the FilterEffect graph via SVGFilterBuilder.
//  5. Apply the graph; composite the result back onto the page DC.
//
// The fill/stroke painting is the same code path the normal shape
// painter runs, just retargeted to the offscreen buffer — that's
// what makes feFlood-only filters not actually consume the shape's
// pixels (they just write their own).
func (sp *svgShapePainter) paintWithSVGFilter(filter *svg.SVGResourceFilter) {
	if filter == nil {
		return
	}
	style := sp.shape.Style

	// Compute the shape's user-space bbox + map to device space for
	// the reference box. The reference box is the shape's
	// FillBoundingBox in user-space, mapped through the SVG paint
	// context's DeviceFromUser transform.
	userBBox := sp.shape.FillBoundingBox
	if userBBox.IsEmpty() {
		return
	}
	// Expand for stroke if present.
	willFill := style == nil || style.GetFill().Kind != css.SVGPaintNone
	willStroke := style != nil && style.GetStroke().Kind != css.SVGPaintNone
	if willStroke && style != nil {
		half := style.GetStrokeWidth() / 2
		userBBox = geometry.NewRectF(
			userBBox.X()-half, userBBox.Y()-half,
			userBBox.Width()+2*half, userBBox.Height()+2*half,
		)
	}
	deviceBBox := sp.ctx.DeviceFromUser.MapRect(userBBox)
	if deviceBBox.IsEmpty() {
		return
	}
	referenceBox := image.Rect(
		int(math.Floor(deviceBBox.X())),
		int(math.Floor(deviceBBox.Y())),
		int(math.Ceil(deviceBBox.Right())),
		int(math.Ceil(deviceBBox.Bottom())),
	)

	// Resolve the filter region against the device-space reference
	// box. ResourceBoundingBox returns a user-space rect; with the
	// reference box in device pixels, the result is in device
	// pixels too (filterUnits=objectBoundingBox treats x/y/w/h as
	// fractions of the reference box).
	refBoxF := geometry.NewRectF(
		float64(referenceBox.Min.X),
		float64(referenceBox.Min.Y),
		float64(referenceBox.Dx()),
		float64(referenceBox.Dy()),
	)
	regionF := filter.ResourceBoundingBox(refBoxF, svg.NewSVGLengthContext(refBoxF.Size))
	region := image.Rect(
		int(math.Floor(regionF.X())),
		int(math.Floor(regionF.Y())),
		int(math.Ceil(regionF.X()+regionF.Width())),
		int(math.Ceil(regionF.Y()+regionF.Height())),
	)
	bw := region.Dx()
	bh := region.Dy()
	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		return
	}

	// Resolve operating space from color-interpolation-filters.
	space := filters.InterpolationSpaceLinearRGB
	if st := filter.Style(); st != nil {
		if v, ok := st.Get("color-interpolation-filters"); ok {
			space = filters.ResolveInterpolationSpace(v)
		}
	}

	// Allocate the source buffer and a renderer targeting it. The
	// buffer's pixel (0,0) corresponds to region.Min in device space.
	srcBuf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	tmpR := NewRendererForImage(srcBuf)
	tmpR.dc.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	t := sp.ctx.DeviceFromUser
	if !t.IsIdentity() {
		tmpR.dc.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}

	// Render the shape's fill/stroke directly into the buffer using
	// the same primitives as the mask painter. This avoids recursing
	// into paintShape — the filter dispatch already consumed the
	// `filter:` decision and we want only fill+stroke here.
	childCtx := *sp.ctx
	childCtx.dc = tmpR.dc
	childCtx.Renderer = tmpR
	childOp := newSVGObjectPainter(tmpR.dc, style).withResources(
		sp.ctx.Resources, sp.shape.FillBoundingBox,
		svg.NewSVGLengthContext(sp.ctx.viewport.Size),
	)

	evenOdd := style != nil && style.GetFillRule() == css.SVGFillRuleEvenOdd
	if buildPathOnDC(tmpR.dc, &sp.shape.Path) {
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
				tmpR.strokePathWithShader(&childCtx, &sp.shape.Path, strokeRes.shader, style)
				tmpR.dc.ClearPath()
			case svgPaintSkip:
				tmpR.dc.ClearPath()
			}
		}
	}

	// Build the FilterEffect graph for this filter element.
	adapter := &svgFilterElementAdapter{
		filter:       filter,
		filterRegion: region,
		referenceBox: referenceBox,
		space:        space,
	}
	builder := filters.NewSVGFilterBuilder(space)
	graph := builder.BuildGraph(adapter)
	if graph == nil {
		return
	}
	graph.SetSourceImage(srcBuf)
	out := graph.Apply()
	if out == nil {
		// Filter produced nothing — draw unfiltered source as fallback.
		compositeFilterOutputOntoTarget(sp.ctx.Renderer, srcBuf, region.Min.X, region.Min.Y)
		return
	}
	compositeFilterOutputOntoTarget(sp.ctx.Renderer, out, region.Min.X, region.Min.Y)
}

// compositeFilterOutputOntoTarget composites `buf` onto the
// renderer's target image at the given absolute device-pixel offset.
// Bypasses the DC's transform stack — the filter region is already
// in device pixels and the SVG paint walk has pushed user→device
// transforms onto the DC that would otherwise double-translate the
// buffer.
//
// Uses the louis14 "straight-alpha-into-premultiplied-container"
// composite convention so the output pixel-matches CSS-rendered
// references (which all flow through mazarin's gg-style setColor
// with rgba(r,g,b,a) values stuffed straight into a color.RGBA).
// The filter graph internally works on premultiplied buffers (per
// SVG Filter Effects); we un-premultiply at the composite edge so
// the page-level math matches the project-wide convention. Mirrors
// the same shim svg_mask_painter.go applies in
// compositeBufferWithOpacityOnto.
func compositeFilterOutputOntoTarget(r *Renderer, buf *image.RGBA, dx, dy int) {
	if r == nil || r.target == nil || buf == nil {
		return
	}
	tb := r.target.Bounds()
	sb := buf.Bounds()
	const m = 1<<16 - 1
	for sy := sb.Min.Y; sy < sb.Max.Y; sy++ {
		ty := sy - sb.Min.Y + dy
		if ty < tb.Min.Y || ty >= tb.Max.Y {
			continue
		}
		for sx := sb.Min.X; sx < sb.Max.X; sx++ {
			tx := sx - sb.Min.X + dx
			if tx < tb.Min.X || tx >= tb.Max.X {
				continue
			}
			si := buf.PixOffset(sx, sy)
			ti := r.target.PixOffset(tx, ty)
			sa := buf.Pix[si+3]
			if sa == 0 {
				continue
			}
			// Un-premultiply to recover straight-alpha RGB. The filter
			// graph outputs premultiplied bytes per
			// FilterEffect.ApplyEffect's contract; the page composite
			// works in the louis14 straight-alpha convention.
			var sR, sG, sB uint8
			if sa == 255 {
				sR, sG, sB = buf.Pix[si+0], buf.Pix[si+1], buf.Pix[si+2]
			} else {
				af := float64(sa) / 255.0
				sR = clampU8Filter(float64(buf.Pix[si+0]) / af)
				sG = clampU8Filter(float64(buf.Pix[si+1]) / af)
				sB = clampU8Filter(float64(buf.Pix[si+2]) / af)
			}
			// Same blend formula as compositeBufferWithOpacityOnto in
			// svg_mask_painter.go (the gg-pattern math), with the SOURCE
			// pixel's straight-alpha RGB and a 16-bit expansion. This is
			// what makes the filter path pixel-match the CSS path the
			// reftest references go through.
			cr := uint32(sR) * 0x101
			cg := uint32(sG) * 0x101
			cb := uint32(sB) * 0x101
			ca := uint32(sa) * 0x101
			ma := uint32(m) // full coverage
			dr := uint32(r.target.Pix[ti+0])
			dg := uint32(r.target.Pix[ti+1])
			db := uint32(r.target.Pix[ti+2])
			da := uint32(r.target.Pix[ti+3])
			a := (m - ca*ma/m) * 0x101
			r.target.Pix[ti+0] = uint8(((dr*a + cr*ma) / m) >> 8)
			r.target.Pix[ti+1] = uint8(((dg*a + cg*ma) / m) >> 8)
			r.target.Pix[ti+2] = uint8(((db*a + cb*ma) / m) >> 8)
			r.target.Pix[ti+3] = uint8(((da*a + ca*ma) / m) >> 8)
		}
	}
}

// clampU8Filter clamps a float to [0, 255] and rounds to uint8.
func clampU8Filter(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}
