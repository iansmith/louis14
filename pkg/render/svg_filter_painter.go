package render

import (
	"image"
	"image/color"
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

	referenceBox := sp.shapeDeviceReferenceBox()
	if referenceBox.IsEmpty() {
		return
	}
	willFill, willStroke := svgPaints(sp.shape.Style)

	graph, region, ok := buildSVGFilterElementGraph(sp.ctx, filter, referenceBox, sp.ctx.userSpaceOriginDevice())
	if !ok {
		return
	}

	// Render the shape's fill/stroke into a source buffer sized to the
	// filter region. The shared helper centralises this for both the
	// single-url and CSS-chain (FilterEffectBuilder) filter paths.
	srcBuf := sp.paintSVGShapeFilterSource(region, willFill, willStroke, style)
	graph.SetSourceImage(srcBuf)
	// The sRGB transform is the end of the filter; clip and mask are outside
	// it (core/paint/filter_effect_builder.cc:500-505 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	out := graph.ApplyToSRGB()
	// CSS Masking 1 §3.7 rendering order is filter → clip → mask →
	// composite: the filter output is the *input* to the clip-path and
	// mask stages, not a sibling of them. Apply them to the filtered
	// buffer here, since the dispatch routed straight to the filter path
	// and would otherwise drop both effects. Mirrors Blink's
	// ScopedSVGPaintState, which nests the clip + mask recorders OUTSIDE
	// the filter recorder so the filtered result is clipped and masked.
	if out != nil {
		sp.applyClipMaskToFilterOutput(out, region)
	}
	// Viewport clip is applied inside the composite helper: the composite
	// path bypasses the DC's clip stack, and SVG 2 §3.5 requires the
	// default 10% filter-region expansion to stay inside the host SVG
	// element.
	compositeSVGFilterResult(sp.ctx, srcBuf, out, region)
}

// svgShapeHasClipOrMask reports whether a shape's style carries a
// clip-path: url(#id) reference or a mask: url(#id). Used to skip the
// post-filter clip/mask stage entirely for the common case of a filtered
// shape with neither effect.
func svgShapeHasClipOrMask(style *css.Style) bool {
	if style == nil {
		return false
	}
	if cp := style.GetClipPath(); cp != nil && cp.Type == css.ClipPathReference {
		return true
	}
	if maskVal := style.GetMaskImage(); maskVal != "" && maskVal != "none" {
		return true
	}
	return false
}

// applyClipMaskToFilterOutput modulates the alpha of a filtered output
// buffer `out` (sized to `region`, buffer pixel (0,0) == region.Min in
// device space) by the shape's `clip-path` and/or `mask`, in that
// order. Per CSS Masking 1 §3.7 the filtered content is what gets
// clipped and masked — mirrors Blink ScopedSVGPaintState nesting the
// clip/mask recorders around the filter recorder.
//
// Both effects reduce to a per-pixel alpha coverage applied to the
// premultiplied buffer (kDstIn): clip-path produces a binary 0/255
// coverage from its geometry; mask produces luminance/alpha coverage
// from the rasterized `<mask>` children.
func (sp *svgShapePainter) applyClipMaskToFilterOutput(out *image.RGBA, region image.Rectangle) {
	style := sp.shape.Style
	if !svgShapeHasClipOrMask(style) {
		// Common case: a filtered shape with neither clip-path nor mask
		// pays nothing beyond this guard (no buffer allocation, no
		// rasterization).
		return
	}
	// clip-path: url(#id). The clipper geometry rasterized into a
	// region-sized coverage buffer becomes a binary alpha mask on the
	// filtered output.
	if cp := style.GetClipPath(); cp != nil && cp.Type == css.ClipPathReference {
		if cov := sp.rasterizeClipCoverageForRegion(cp.ReferenceID, region); cov != nil {
			applySVGMaskToBuffer(out, cov, svg.SVGMaskTypeAlpha)
		}
	}
	// mask: url(#id). Rasterize the masker into a region-sized buffer
	// and apply it as a kDstIn luminance/alpha modulator.
	if maskVal := style.GetMaskImage(); maskVal != "" && maskVal != "none" {
		if id, ok := css.ParseURLReference(maskVal); ok && sp.ctx.Resources != nil {
			if masker, found := sp.ctx.Resources.LookupMasker(id); found &&
				masker != nil && masker.CycleState() != svg.SVGCycleHasCycle {
				maskBuf := sp.ctx.Renderer.rasterizeSVGMaskAtDeviceBBox(
					masker, sp.shape.FillBoundingBox,
					region.Dx(), region.Dy(), region.Min.X, region.Min.Y,
					sp.ctx.DeviceFromUser,
				)
				if maskBuf != nil {
					applySVGMaskToBuffer(out, maskBuf, masker.MaskType)
				}
			}
		}
	}
}

// rasterizeClipCoverageForRegion rasterizes the referenced `<clipPath>`
// geometry into an opaque-white-on-transparent coverage buffer sized to
// `region` (pixel (0,0) == region.Min in device space). The buffer's
// alpha channel is the clip coverage: 255 inside the clip geometry, 0
// outside. Returns nil when the clipper is missing, empty, or part of a
// reference cycle (the caller then leaves the filtered output unclipped,
// matching SVG 2 §15.3 invalid-reference resilience).
//
// Mirrors how Blink's SVGClipPainter rasterizes a content clip into an
// alpha layer that then masks the painted (here: filtered) content.
func (sp *svgShapePainter) rasterizeClipCoverageForRegion(clipPathReferenceID string, region image.Rectangle) *image.RGBA {
	if sp.ctx.Resources == nil || clipPathReferenceID == "" {
		return nil
	}
	clipper, ok := sp.ctx.Resources.LookupClipper(clipPathReferenceID)
	if !ok || clipper == nil || !clipper.HasContent() ||
		clipper.CycleState() == svg.SVGCycleHasCycle {
		return nil
	}
	bw, bh := region.Dx(), region.Dy()
	if bw <= 0 || bh <= 0 {
		return nil
	}
	cov := image.NewRGBA(image.Rect(0, 0, bw, bh))
	tmpR := NewRendererForImage(cov)
	// Buffer pixel (0,0) == device pixel region.Min; compose that
	// translate with the SVG-root user→device map so the clip geometry
	// rasterizes at the same scale the shape did.
	tmpR.dc.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	if t := sp.ctx.DeviceFromUser; !t.IsIdentity() {
		tmpR.dc.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}
	// Shape-based clipper fast path: replay the path filled opaque
	// white. The shape's user-space FillBoundingBox is the reference
	// box for objectBoundingBox-unit clip geometry.
	if path, ok := clipper.AsPath(sp.shape.FillBoundingBox); ok {
		if !buildSVGPathOnDC(tmpR.dc, &path) {
			return nil
		}
		tmpR.dc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
		tmpR.dc.Fill()
		return cov
	}
	// Content-based clippers are not yet exercised on the filter+clip
	// reftests; leave the output unclipped rather than mis-clip.
	return nil
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
// `buf` is premultiplied sRGB and reaches the target through source-over.
func compositeFilterOutputOntoTarget(r *Renderer, buf *image.RGBA, dx, dy int, clipRect image.Rectangle) {
	if r == nil || r.target == nil || buf == nil {
		return
	}
	sb := buf.Bounds()
	offset := image.Pt(dx, dy).Sub(sb.Min)
	dest := sb.Add(offset).Intersect(clipRect).Intersect(r.target.Bounds())
	if dest.Empty() {
		return
	}
	rasterPipelineSpriteBlit(r.target, dest, buf, dest.Min.Sub(offset), 1)
}

// shapeDeviceReferenceBox maps the shape's filter reference box, its fill
// box (SVGResources::ReferenceBoxForEffects defaults to GeometryBox::kFillBox,
// the ObjectBoundingBox: svg_resources.h:65-68, svg_resources.cc:53-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), through DeviceFromUser,
// unsnapped (Blink's reference box is a gfx::RectF). It is empty for a
// shape with an empty fill box, such as a horizontal <line>.
func (sp *svgShapePainter) shapeDeviceReferenceBox() geometry.RectF {
	return sp.ctx.DeviceFromUser.MapRect(sp.shape.FillBoundingBox)
}

// svgPaints reports whether a shape with style fills and whether it
// strokes; a nil style takes the defaults, fill black and stroke none.
func svgPaints(style *css.Style) (willFill, willStroke bool) {
	return style == nil || style.GetFill().Kind != css.SVGPaintNone,
		style != nil && style.GetStroke().Kind != css.SVGPaintNone
}

// shapeDevicePaintBox is the shape's paint extent in device pixels: its
// stroke bounding box, miter joins and square caps included
// (LayoutSVGShape::StrokeBoundingBox via svg.SVGShape.StrokeBoundingBox).
func (sp *svgShapePainter) shapeDevicePaintBox() geometry.RectF {
	return sp.ctx.DeviceFromUser.MapRect(sp.shape.StrokeBoundingBox())
}

// userSpaceOriginDevice is the user-space origin in device pixels, which
// userSpaceOnUse filter coordinates are relative to (SVG Filter Effects 1
// §6.5).
func (c *svgPaintContext) userSpaceOriginDevice() geometry.PointF {
	return c.DeviceFromUser.MapPoint(geometry.PointF{X: 0, Y: 0})
}

// roundPoint rounds p to the nearest device pixel.
func roundPoint(p geometry.PointF) image.Point {
	return image.Pt(int(math.Round(p.X)), int(math.Round(p.Y)))
}

// compositeSVGFilterResult writes either the filter output (out) or the
// unfiltered source (srcBuf, used when ApplyToSRGB returned nil) onto the
// page DC's target at region, clipped to the SVG viewport. The viewport clip
// must be applied explicitly because the composite path bypasses the
// DC's transform/clip stack — see compositeFilterOutputOntoTarget.
func compositeSVGFilterResult(pctx *svgPaintContext, srcBuf, out *image.RGBA, region image.Rectangle) {
	buf := out
	if buf == nil {
		// ApplyToSRGB returned nil (degenerate filter): composite the raw source.
		buf = srcBuf
	}
	compositeFilterOutputOntoTarget(pctx.Renderer, buf, region.Min.X, region.Min.Y, pctx.viewportDeviceClip())
}

// newSVGFilterSource returns a source buffer covering region (device
// pixels) and a copy of pctx painting into it: the buffer's pixel (0, 0) is
// region.Min. Shared by the shape and container filter paths.
func newSVGFilterSource(pctx *svgPaintContext, region image.Rectangle) (*image.RGBA, svgPaintContext) {
	srcBuf := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	tmpR := NewRendererForImage(srcBuf)
	tmpR.dc.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	t := pctx.DeviceFromUser
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
	//
	// The viewport and mask-region clips a nested filtered child composites
	// under (viewportDeviceClip) are in device pixels too, so they move into
	// the buffer with it.
	toBuffer := geometry.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	childCtx := *pctx
	childCtx.dc = tmpR.dc
	childCtx.Renderer = tmpR
	childCtx.DeviceFromUser = toBuffer.Compose(pctx.DeviceFromUser)
	childCtx.contentToDevice = toBuffer.Compose(pctx.contentToDevice)
	if !pctx.regionClip.Empty() {
		childCtx.regionClip = pctx.regionClip.Sub(region.Min)
	}
	return srcBuf, childCtx
}

// buildSVGFilterElementGraph resolves a lone url() <filter>'s region
// against an SVG element's device-space reference box and builds its graph
// (the caller paints the source into the region and sets it). The region
// resolves filterUnits against referenceBox, userSpaceOnUse x/y relative to
// userOrigin (SVG Filter Effects 1 §6.5), and userSpaceOnUse percentages
// against the <svg>'s viewport, not the reference box (Blink's
// LayoutSVGResourceContainer::ResolveRectangle). Primitive subregions in
// userSpaceOnUse anchor at userOrigin too, not at region.Min, which can
// carry the default 10% expansion (SVG Filter Effects 1 §15.4). ok is false
// when the region is not a usable buffer. Shared by SVG shapes and
// containers.
func buildSVGFilterElementGraph(pctx *svgPaintContext, filter *svg.SVGResourceFilter, referenceBox geometry.RectF, userOrigin geometry.PointF) (graph *filters.Filter, region image.Rectangle, ok bool) {
	viewportDeviceSize := pctx.DeviceFromUser.MapRect(pctx.viewport).Size
	region = snapRectFToDevice(filter.ResourceBoundingBox(referenceBox, userOrigin, svg.NewSVGLengthContext(viewportDeviceSize)))
	if !effectBufferUsable(region.Dx(), region.Dy()) {
		return nil, image.Rectangle{}, false
	}
	space := filterInterpolationSpace(filter)
	adapter := &svgFilterElementAdapter{
		filter:             filter,
		filterRegion:       region,
		referenceBox:       referenceBox,
		userSpaceOrigin:    roundPoint(userOrigin),
		space:              space,
		resources:          pctx.Resources,
		imageFetcher:       pctx.Renderer.imageFetcher,
		externalSVGFetcher: pctx.Renderer.externalSVGFetcher,
	}
	graph = filters.NewSVGFilterBuilder(space).BuildGraph(adapter)
	return graph, region, graph != nil
}

// buildSVGFilterChain builds an SVG element's CSS filter list that is not a
// lone url() (shorthand functions, or url() mixed with them) and its region:
// the element's paint extent paintBox mapped through the chain, under the
// buffer cap. A url() stage starts the region from the reference box mapped
// through the chain; the paint extent is united in, so paint outside the
// fill box stays (Blink crops only a url() stage, to its own filter region).
// As for a lone url(), userSpaceOnUse coordinates are relative to
// userOrigin and percentages to the <svg>'s viewport (SVGViewportResolver,
// svg_resources.cc:375-381 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// currentColor resolves `drop-shadow(... currentcolor)` (the element's
// ComputedStyle::VisitedDependentColor(GetCSSPropertyColor())). ok is false
// when no graph is produced, so the caller paints unfiltered.
func buildSVGFilterChain(pctx *svgPaintContext, ops []css.FilterFunction, referenceBox geometry.RectF, paintBox image.Rectangle, userOrigin geometry.PointF, currentColor css.Color) (graph *filters.Filter, region image.Rectangle, ok bool) {
	// A lone url() is painted through the <filter> element's own region
	// (buildSVGFilterElementGraph); here it is one that did not resolve.
	if len(ops) == 1 && ops[0].Name == "url" {
		return nil, image.Rectangle{}, false
	}
	builder := &FilterEffectBuilder{
		ReferenceBox:       referenceBox,
		FilterViewport:     snapRectFToDevice(pctx.DeviceFromUser.MapRect(pctx.viewport)),
		UserSpaceOrigin:    &userOrigin,
		CurrentColor:       currentColor,
		Resources:          pctx.Resources,
		ExternalSVGFetcher: pctx.Renderer.externalSVGFetcher,
		ImageFetcher:       pctx.Renderer.imageFetcher,
	}
	graph = builder.BuildFilterEffect(ops)
	if graph == nil {
		return nil, image.Rectangle{}, false
	}
	if !graph.FilterRegion.Empty() {
		graph.FilterRegion = graph.FilterRegion.Union(graph.MapRect(paintBox))
	}
	region = graph.EnsureRegion(paintBox, false)
	if !effectBufferUsable(region.Dx(), region.Dy()) {
		return nil, image.Rectangle{}, false
	}
	return graph, region, true
}

// paintSVGShapeFilterSource rasterises the shape's fill/stroke into an
// RGBA buffer sized to the filter region (newSVGFilterSource). Shared
// between paintWithSVGFilter (single url() reference) and
// paintWithFilterChain (CSS filter-value-list with multiple ops).
func (sp *svgShapePainter) paintSVGShapeFilterSource(region image.Rectangle, willFill, willStroke bool, style *css.Style) *image.RGBA {
	srcBuf, childCtx := newSVGFilterSource(sp.ctx, region)
	tmpR := childCtx.Renderer
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
	var currentColor css.Color
	if sp.shape.Style != nil {
		currentColor = sp.shape.Style.GetColor()
	}
	// A shorthand stage needs no reference box, so only a url() stage
	// rejects a shape whose fill box is empty (a horizontal <line>), as its
	// lone url() does; whether even that should render is LOU-777. A
	// container's chain paints with it, as its lone url() does.
	referenceBox := sp.shapeDeviceReferenceBox()
	if referenceBox.IsEmpty() && hasReferenceFilter(ops) {
		return false
	}
	graph, region, ok := buildSVGFilterChain(sp.ctx, ops, referenceBox,
		snapRectFToDevice(sp.shapeDevicePaintBox()), sp.ctx.userSpaceOriginDevice(), currentColor)
	if !ok {
		return false
	}
	willFill, willStroke := svgPaints(sp.shape.Style)
	srcBuf := sp.paintSVGShapeFilterSource(region, willFill, willStroke, sp.shape.Style)
	graph.SetSourceImage(srcBuf)
	out := graph.ApplyToSRGB()
	// CSS Masking 1 §3.7 filter → clip → mask: the filtered result is
	// clipped and masked before compositing. The shorthand-filter path
	// (e.g. `filter="blur(20px)"` on an SVG `<rect>`) shares the same
	// post-filter clip/mask requirement as the url(#id) path above.
	if out != nil {
		sp.applyClipMaskToFilterOutput(out, region)
	}
	compositeSVGFilterResult(sp.ctx, srcBuf, out, region)
	return true
}
