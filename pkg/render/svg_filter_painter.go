package render

import (
	"image"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/graphics/filters"
	"louis14/pkg/layout/svg"
)

// sRGB ↔ linear transfer functions, mirroring filters/colorspace.go.
// Duplicated here to avoid exporting package-private helpers; inlined
// for performance in the per-pixel compositor hot path.

var svgLinearToSRGBLUT [256]uint8 // linearRGB byte value → sRGB byte value

func init() {
	for i := 0; i < 256; i++ {
		c := float64(i) / 255.0
		var s float64
		if c <= 0.0031308 {
			s = c * 12.92
		} else {
			s = 1.055*math.Pow(c, 1.0/2.4) - 0.055
		}
		svgLinearToSRGBLUT[i] = uint8(s*255 + 0.5)
	}
}

func svgSRGBToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearToSRGBf(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

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

	referenceBox, willFill, willStroke, ok := sp.shapeDeviceReferenceBox()
	if !ok {
		return
	}

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
	// SVG element's user-space origin in device coords — needed by
	// ResourceBoundingBox to shift explicit userSpaceOnUse x/y values
	// from user space into target's (device) coord space, per SVG
	// Filter Effects 1 §6.5.
	userOriginF := sp.ctx.DeviceFromUser.MapPoint(geometry.PointF{X: 0, Y: 0})
	// Per Blink's LayoutSVGResourceContainer::ResolveRectangle the
	// userSpaceOnUse percent base is the SVG VIEWPORT (mapped to device
	// pixels), NOT the reference box. The SVG viewport in user-space is
	// sp.ctx.viewport; mapping it through DeviceFromUser gives the
	// device-pixel size to use as percent base.
	viewportDeviceSize := sp.ctx.DeviceFromUser.MapRect(sp.ctx.viewport).Size
	regionF := filter.ResourceBoundingBox(refBoxF, userOriginF, svg.NewSVGLengthContext(viewportDeviceSize))
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

	// Render the shape's fill/stroke into a source buffer sized to the
	// filter region. The shared helper centralises this for both the
	// single-url and CSS-chain (FilterEffectBuilder) filter paths.
	srcBuf := sp.renderShapeIntoFilterBuffer(region, bw, bh, willFill, willStroke, style)

	// Build the FilterEffect graph for this filter element.
	//
	// userSpaceOrigin is the SVG element's user-space (0, 0) in
	// absolute device coords (computed above for ResourceBoundingBox).
	// The userSpaceOnUse subregion anchor for `<feFoo x="..." y="...">`
	// primitives is computed against this, NOT against region.Min
	// (which can include the default 10% filter-region expansion).
	// Per SVG Filter Effects 1 §15.4 primitive subregion attributes
	// in userSpaceOnUse mode are user-coordinate-system lengths.
	adapter := &svgFilterElementAdapter{
		filter:             filter,
		filterRegion:       region,
		referenceBox:       referenceBox,
		userSpaceOrigin:    image.Point{X: int(math.Round(userOriginF.X)), Y: int(math.Round(userOriginF.Y))},
		space:              space,
		resources:          sp.ctx.Resources,
		imageFetcher:       sp.ctx.Renderer.imageFetcher,
		externalSVGFetcher: sp.ctx.Renderer.externalSVGFetcher,
	}
	builder := filters.NewSVGFilterBuilder(space)
	graph := builder.BuildGraph(adapter)
	if graph == nil {
		return
	}
	graph.SetSourceImage(srcBuf)
	out := graph.Apply()
	// The composite must convert from the LAST effect's operating space,
	// not the filter's nominal color-interpolation-filters space: a
	// primitive may produce output in a different space than the filter
	// default (most notably feFlood, which Blink exempts from
	// color-interpolation and keeps in sRGB — fe_flood.cc @ Chromium
	// main). Mirrors Filter.ApplyToSRGB, which converts from
	// LastEffect.OperatingSpace(); the chain path (paintWithFilterChain)
	// already does this.
	outSpace := space
	if graph.LastEffect != nil {
		outSpace = graph.LastEffect.OperatingSpace()
	}
	// Viewport clip is applied inside the composite helper: the composite
	// path bypasses the DC's clip stack, and SVG 2 §3.5 requires the
	// default 10% filter-region expansion to stay inside the host SVG
	// element.
	sp.compositeFilterResultOntoTarget(srcBuf, out, region, outSpace)
}

// compositeFilterOutputOntoTarget composites `buf` onto the
// renderer's target image at the given absolute device-pixel offset.
// Bypasses the DC's transform stack — the filter region is already
// in device pixels and the SVG paint walk has pushed user→device
// transforms onto the DC that would otherwise double-translate the
// buffer.
//
// clipRect constrains the write in target device-pixel space. The DC's
// clip stack is also bypassed (writes go straight to r.target.Pix), so
// callers must pass the active SVG-viewport rect to honour the SVG 2
// §3.5 overflow:hidden default that would otherwise leak filter
// overflow (the default 10% filter-region expansion in SVG Filter
// Effects 1 §4.4) past the host element.
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
func compositeFilterOutputOntoTarget(r *Renderer, buf *image.RGBA, dx, dy int, clipRect image.Rectangle, space filters.InterpolationSpace) {
	if r == nil || r.target == nil || buf == nil {
		return
	}
	// Pre-intersect target bounds with clipRect: any pixel outside
	// `effective` is skipped. Empty intersection means there's nothing
	// to write.
	effective := r.target.Bounds().Intersect(clipRect)
	if effective.Empty() {
		return
	}
	sb := buf.Bounds()
	for sy := sb.Min.Y; sy < sb.Max.Y; sy++ {
		ty := sy - sb.Min.Y + dy
		if ty < effective.Min.Y || ty >= effective.Max.Y {
			continue
		}
		for sx := sb.Min.X; sx < sb.Max.X; sx++ {
			tx := sx - sb.Min.X + dx
			if tx < effective.Min.X || tx >= effective.Max.X {
				continue
			}
			si := buf.PixOffset(sx, sy)
			sa := buf.Pix[si+3]
			if sa == 0 {
				continue
			}
			ti := r.target.PixOffset(tx, ty)
			if space == filters.InterpolationSpaceLinearRGB {
				if sa == 255 {
					// Fully opaque: source replaces destination.
					// Convert premultiplied linearRGB → sRGB via LUT (premul=straight at A=255).
					r.target.Pix[ti+0] = svgLinearToSRGBLUT[buf.Pix[si+0]]
					r.target.Pix[ti+1] = svgLinearToSRGBLUT[buf.Pix[si+1]]
					r.target.Pix[ti+2] = svgLinearToSRGBLUT[buf.Pix[si+2]]
					r.target.Pix[ti+3] = 255
					continue
				}
				// Semi-transparent linearRGB source: composite in linearRGB,
				// convert result to sRGB. Compositing in sRGB causes ~11-unit
				// systematic error in uniform filter regions.
				srcA := float64(sa) / 255
				sR := float64(buf.Pix[si+0]) / 255
				sG := float64(buf.Pix[si+1]) / 255
				sB := float64(buf.Pix[si+2]) / 255
				dA := float64(r.target.Pix[ti+3]) / 255
				// Un-premultiply destination before linearizing: applying
				// sRGBToLinear to a premultiplied byte gives the wrong answer
				// when dA < 255. At dA=0 the destination is fully transparent
				// so its RGB values are irrelevant.
				var dR, dG, dB float64
				if dA > 0 {
					invDA := 1 / dA
					dR = svgSRGBToLinear(float64(r.target.Pix[ti+0])/255*invDA) * dA
					dG = svgSRGBToLinear(float64(r.target.Pix[ti+1])/255*invDA) * dA
					dB = svgSRGBToLinear(float64(r.target.Pix[ti+2])/255*invDA) * dA
				}
				inv1 := 1 - srcA
				oR := sR + dR*inv1
				oG := sG + dG*inv1
				oB := sB + dB*inv1
				oA := srcA + dA*inv1
				if oA > 0 {
					invA := 1 / oA
					r.target.Pix[ti+0] = uint8(math.Min(1, math.Max(0, linearToSRGBf(oR*invA)*oA))*255 + 0.5)
					r.target.Pix[ti+1] = uint8(math.Min(1, math.Max(0, linearToSRGBf(oG*invA)*oA))*255 + 0.5)
					r.target.Pix[ti+2] = uint8(math.Min(1, math.Max(0, linearToSRGBf(oB*invA)*oA))*255 + 0.5)
					r.target.Pix[ti+3] = uint8(oA*255 + 0.5)
				}
			} else {
				// sRGB source: un-premultiply the source RGB and composite it
				// with the louis14 "straight-alpha-into-premultiplied-container"
				// convention (out = srcStraightRGB + dst*(1-srcA)), the same
				// convention mazarin's gg-style setColor uses for a plain
				// rgba(r,g,b,a) fill. This is what makes a semi-transparent
				// feFlood pixel-match the equivalent fill-opacity rect (e.g.
				// green flood-opacity=0.75 over white -> (64,192,64), not the
				// premultiplied-Over (64,160,64)). At sa==255 it reduces to a
				// straight source copy, matching the opaque fast path above.
				srcAf := float64(sa) / 255
				inv1 := 1 - srcAf
				// comp folds one channel: straight source (un-premultiplied)
				// plus the destination attenuated by (1-srcA).
				comp := func(srcPremul, dst uint8) uint8 {
					srcStraight := float64(srcPremul) / 255 / srcAf
					v := (srcStraight + float64(dst)/255*inv1) * 255
					return uint8(math.Min(255, math.Max(0, v)) + 0.5)
				}
				inv := uint32(255 - sa)
				r.target.Pix[ti+0] = comp(buf.Pix[si+0], r.target.Pix[ti+0])
				r.target.Pix[ti+1] = comp(buf.Pix[si+1], r.target.Pix[ti+1])
				r.target.Pix[ti+2] = comp(buf.Pix[si+2], r.target.Pix[ti+2])
				r.target.Pix[ti+3] = uint8((uint32(sa)*255 + uint32(r.target.Pix[ti+3])*inv + 127) / 255)
			}
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

// shapeDeviceReferenceBox computes the shape's stroke-inflated user-space
// bbox, maps it through DeviceFromUser, and snaps it to an integer rect.
// Returns the reference box, willFill, willStroke, and ok=false when the
// shape has no paintable extent (empty user or device bbox). Shared by
// both filter paint paths (single-url and chain).
func (sp *svgShapePainter) shapeDeviceReferenceBox() (image.Rectangle, bool, bool, bool) {
	style := sp.shape.Style
	userBBox := sp.shape.FillBoundingBox
	if userBBox.IsEmpty() {
		return image.Rectangle{}, false, false, false
	}
	willFill := style == nil || style.GetFill().Kind != css.SVGPaintNone
	willStroke := style != nil && style.GetStroke().Kind != css.SVGPaintNone
	if willStroke {
		half := style.GetStrokeWidth() / 2
		userBBox = geometry.NewRectF(
			userBBox.X()-half, userBBox.Y()-half,
			userBBox.Width()+2*half, userBBox.Height()+2*half,
		)
	}
	deviceBBox := sp.ctx.DeviceFromUser.MapRect(userBBox)
	if deviceBBox.IsEmpty() {
		return image.Rectangle{}, false, false, false
	}
	return image.Rect(
		int(math.Floor(deviceBBox.X())),
		int(math.Floor(deviceBBox.Y())),
		int(math.Ceil(deviceBBox.Right())),
		int(math.Ceil(deviceBBox.Bottom())),
	), willFill, willStroke, true
}

// compositeFilterResultOntoTarget writes either the filter output (out)
// or the unfiltered source (srcBuf, used when Apply returned nil) onto
// the page DC's target, clipped to the SVG viewport. The viewport clip
// must be applied explicitly because the composite path bypasses the
// DC's transform/clip stack — see compositeFilterOutputOntoTarget.
func (sp *svgShapePainter) compositeFilterResultOntoTarget(srcBuf, out *image.RGBA, region image.Rectangle, space filters.InterpolationSpace) {
	viewportClip := image.Rect(
		int(sp.ctx.originX),
		int(sp.ctx.originY),
		int(sp.ctx.originX+sp.ctx.viewport.Width()),
		int(sp.ctx.originY+sp.ctx.viewport.Height()),
	)
	buf := out
	if buf == nil {
		// Apply returned nil (degenerate filter): composite the raw source
		// buffer, which is always sRGB device content regardless of the
		// filter's interpolation space.
		buf = srcBuf
		space = filters.InterpolationSpaceSRGB
	}
	compositeFilterOutputOntoTarget(sp.ctx.Renderer, buf, region.Min.X, region.Min.Y, viewportClip, space)
}

// renderShapeIntoFilterBuffer rasterises the shape's fill/stroke into an
// RGBA buffer sized to the filter region. The buffer's pixel (0,0) maps to
// region.Min in device space. Shared between paintWithSVGFilter (single
// url() reference) and paintWithFilterChain (CSS filter-value-list with
// multiple ops). See the inline comment in this function about the
// childCtx.DeviceFromUser translate — that detail is load-bearing for
// shader fills that compute per-pixel write positions independently of
// the DC transform stack.
func (sp *svgShapePainter) renderShapeIntoFilterBuffer(region image.Rectangle, bw, bh int, willFill, willStroke bool, style *css.Style) *image.RGBA {
	srcBuf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	tmpR := NewRendererForImage(srcBuf)
	tmpR.dc.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	t := sp.ctx.DeviceFromUser
	if !t.IsIdentity() {
		tmpR.dc.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}

	// childCtx.DeviceFromUser maps a user-space point to a pixel in the
	// source buffer (not the page). tmpR.dc carries
	// Translate(-region.Min.X, -region.Min.Y) on top of the SVG-root
	// DeviceFromUser so paint ends up at the right buffer pixel;
	// childCtx.DeviceFromUser must include the same extra translate or
	// fillPathWithShader (svg_paint_server.go:75) will compute its
	// per-pixel write positions from the SVG-root DeviceFromUser alone
	// and land the shader output at page-coord pixel positions inside
	// the source buffer — wrong by region.Min, so the filter graph then
	// samples a misplaced source. Visible as a ~3-px shift on the
	// bucket-J tainting tests, because region.Min is rounded down by
	// 1 ulp (float precision on the spec's -10% expansion of an int-pixel
	// reference box) and the source rect's blend with the underlying
	// un-filtered rect leaves a slim mismatched strip.
	childCtx := *sp.ctx
	childCtx.dc = tmpR.dc
	childCtx.Renderer = tmpR
	childCtx.DeviceFromUser = geometry.Translate(
		float64(-region.Min.X), float64(-region.Min.Y),
	).Compose(sp.ctx.DeviceFromUser)
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
						return srcBuf
					}
				} else {
					tmpR.dc.ClearPath()
				}
			case svgPaintSkip:
				if willStroke {
					tmpR.dc.ClearPath()
					if !buildPathOnDC(tmpR.dc, &sp.shape.Path) {
						return srcBuf
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
	return srcBuf
}

// paintWithFilterChain handles a CSS filter-value-list that isn't a single
// url() reference — multiple url() entries, mixed url() + shorthand, or
// shorthand-only on an SVG shape. The chain is built via
// FilterEffectBuilder.BuildFilterEffect (which composes per-stage
// SourceGraphic overrides per Blink's FilterChain).
// Returns false when no graph is produced (e.g. every url() entry misses)
// so the caller can fall through to unfiltered paint, matching the
// long-standing single-url fallback behavior.
func (sp *svgShapePainter) paintWithFilterChain(ops []css.FilterFunction) bool {
	// Callers route a single url() through paintWithSVGFilter first
	// because that path consults the `<filter>` element's own region
	// attributes (filterUnits / x / y / w / h + the spec's -10%/+20%
	// default expansion). The chain path here uses Filter.MapRect, which
	// only unions effect-expanded reference-box rects — so a single-url
	// routed through here would clip incorrectly. Guard against it.
	if len(ops) == 1 && ops[0].Name == "url" {
		return false
	}
	referenceBox, willFill, willStroke, ok := sp.shapeDeviceReferenceBox()
	if !ok {
		return false
	}
	// `drop-shadow(... currentcolor)` resolves to the shape's computed CSS
	// `color` at paint time (Filter Effects 1 §3, mirroring Blink's
	// FEDropShadow currentColor resolution via the element's
	// ComputedStyle::VisitedDependentColor(GetCSSPropertyColor())). The HTML
	// box path populates this from layer.TextColor in paintLayerWithFilter;
	// without the same wiring here, drop-shadow on an SVG shape sees a
	// zero-alpha black and the shadow never appears.
	var currentColor css.Color
	if sp.shape != nil && sp.shape.Style != nil {
		currentColor = sp.shape.Style.GetColor()
	}
	builder := &FilterEffectBuilder{
		ReferenceBox:       referenceBox,
		CurrentColor:       currentColor,
		Resources:          sp.ctx.Resources,
		ExternalSVGFetcher: sp.ctx.Renderer.externalSVGFetcher,
		ImageFetcher:       sp.ctx.Renderer.imageFetcher,
	}
	graph := builder.BuildFilterEffect(ops)
	if graph == nil {
		return false
	}
	space := graph.LastEffect.OperatingSpace()
	region := graph.FilterRegion
	bw, bh := region.Dx(), region.Dy()
	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		return false
	}

	srcBuf := sp.renderShapeIntoFilterBuffer(region, bw, bh, willFill, willStroke, sp.shape.Style)
	graph.SetSourceImage(srcBuf)
	out := graph.Apply()
	sp.compositeFilterResultOntoTarget(srcBuf, out, region, space)
	return true
}
