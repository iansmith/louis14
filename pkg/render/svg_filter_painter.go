package render

import (
	"image"
	"image/color"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/graphics/filters"
	"louis14/pkg/layout/svg"
)

// svgElementFilter is an SVG element's filter graph, built in the element's
// user space inside its own transform, and the mapping that carries that
// space into the layer the graph runs in. Blink builds the filter in the
// element's local space: UpdateTransformForSVGChild runs before UpdateFilter,
// whose local_transform_space is the current transform
// (FragmentPaintPropertyTreeBuilder, core/paint/
// paint_property_tree_builder.cc:4066-4082, 2447-2453 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and Skia splits the CTM into the
// layer and the remainder it is drawn with (skif::Mapping::decomposeCTM, as
// for a CSS layer: Renderer.layerSpaceFilter). LOU-774.
type svgElementFilter struct {
	// graph's FilterRegion is the source and output buffer, in layer
	// pixels.
	graph   *filters.Filter
	mapping filters.Mapping
}

// buildSVGElementFilter builds an SVG element's resolved filter stages with
// builder (newSVGFilterEffectBuilder: its reference box is the element's
// fill box, SVGResources::ReferenceBoxForEffects defaults to
// GeometryBox::kFillBox, svg_resources.cc:53-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) in the user space pctx's draw
// context paints in, the element's own transform included. paintBox is its
// paint extent (its stroke bounding box) in user space.
//
// A lone url() takes its <filter>'s region. Any other list (shorthand
// functions, or url() mixed with them) takes the paint extent mapped
// through the chain; a url() stage starts it from the reference box mapped
// through the chain, and the paint extent is united in (Blink crops only a
// url() stage, to its own filter region).
//
// handled is false when the element paints unfiltered: there is no
// renderer, no graph is built (no resolvable stage), the buffer is over the
// cap, or an element with no
// paint extent (a group of only <text>: LOU-795) has no url() stage whose
// region is its own (userSpaceOnUse). A handled nil filter paints nothing:
// the CTM does not decompose, or the region is empty.
func buildSVGElementFilter(pctx *svgPaintContext, builder *FilterEffectBuilder, stages []filterStage, paintBox geometry.RectF) (f *svgElementFilter, handled bool) {
	r := pctx.Renderer
	if r == nil || len(stages) == 0 {
		return nil, false
	}
	lone := len(stages) == 1 && stages[0].op.Name == "url"
	if !lone && paintBox.IsEmpty() && !hasUserSpaceFilterRegion(stages) {
		return nil, false
	}
	graph, mapping, ok := r.layerSpaceFilter(drawContextTransform(pctx.dc), builder, stages, nil)
	switch {
	case !ok:
		return nil, true
	case graph == nil:
		return nil, false
	}
	region := graph.FilterRegion
	if !lone {
		paintLayer, _ := builder.Mapping.ParamToLayerPixels(paintBox)
		if !graph.FilterRegion.Empty() {
			graph.FilterRegion = graph.FilterRegion.Union(graph.MapRect(paintLayer))
		}
		region = graph.EnsureRegion(paintLayer, false)
	}
	// The region is not clipped to the draw context's clip before the cap:
	// Skia bounds the layer by the filter's input bounds (the reverse
	// mapping), which louis14 does not have (LOU-704); a forward-MapRect
	// reach would tile, light and wrap at the clip edge.
	graph.FilterRegion = region
	switch {
	case region.Empty():
		return nil, true
	case !effectBufferUsable(region.Dx(), region.Dy()):
		return nil, false
	}
	return &svgElementFilter{graph: graph, mapping: mapping}, true
}

// newSVGFilterEffectBuilder returns the builder for an SVG element's filter
// in its user space: referenceBox is its fill box, userSpaceOnUse
// coordinates are relative to the user-space origin and percentages to the
// <svg>'s viewport (SVGViewportResolver, svg_resources.cc:375-381 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and currentColor resolves
// `drop-shadow(... currentcolor)`. Without a renderer it resolves
// same-document filters only.
func newSVGFilterEffectBuilder(pctx *svgPaintContext, referenceBox geometry.RectF, currentColor css.Color) *FilterEffectBuilder {
	b := &FilterEffectBuilder{
		ReferenceBox:    referenceBox,
		FilterViewport:  pctx.viewport,
		UserSpaceOrigin: &geometry.PointF{},
		CurrentColor:    currentColor,
		Resources:       pctx.Resources,
	}
	if r := pctx.Renderer; r != nil {
		b.ExternalSVG = r.externalSVGs()
		b.ImageFetcher = r.imageFetcher
	}
	return b
}

// paint paints an SVG element through f: paintSource paints the element
// into the source buffer through the given paint context (the element's user
// space mapped into the layer), the graph runs over it, afterFilter (when
// non-nil) modulates the filtered output, and the output is drawn through
// pctx's draw context with the layer's remainder (fLayerToDevMatrix), so
// ancestor transforms and the draw context's clips (the <svg> viewport's
// overflow clip) apply to it.
func (f *svgElementFilter) paint(pctx *svgPaintContext, paintSource func(*svgPaintContext), afterFilter func(out *image.RGBA)) {
	toLayer := f.mapping.ParamToLayer
	region := f.graph.FilterRegion
	srcBuf, childCtx := newSVGEffectSource(pctx, region, toLayer)
	paintSource(&childCtx)
	f.graph.SetSourceImage(srcBuf)
	// The sRGB transform is the end of the filter; clip and mask are outside
	// it (core/paint/filter_effect_builder.cc:500-505 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	out := f.graph.ApplyToSRGB()
	if out == nil {
		// A degenerate graph: draw the unfiltered source.
		out = srcBuf
	} else if afterFilter != nil {
		afterFilter(out)
	}
	pctx.dc.ClearPath()
	compositeEffectBufferOnto(pctx.dc, toLayer, out, region.Min.X, region.Min.Y)
}

// paintWithFilter paints the shape through its filter list ops (a lone url()
// to a <filter>, CSS filter functions, or a mix), in its user space, the
// shape's own transform included (buildSVGElementFilter). Per CSS Masking 1
// §3.7 the rendering order is filter → clip → mask: the filtered output is
// clipped and masked before it composites (ScopedSVGPaintState nests the clip
// and mask recorders outside the filter's). It reports false when the shape
// paints unfiltered.
//
// A url() stage needs a reference box: a shape whose fill box is empty (a
// horizontal <line>) paints nothing through a lone url() that resolves, and
// unfiltered otherwise; whether it should render is LOU-777. A shorthand
// stage needs none.
func (sp *svgShapePainter) paintWithFilter(ops []css.FilterFunction) bool {
	style := sp.shape.Style
	var currentColor css.Color
	if style != nil {
		currentColor = style.GetColor()
	}
	builder := newSVGFilterEffectBuilder(sp.ctx, sp.shape.FillBoundingBox, currentColor)
	stages := builder.resolveStages(ops)
	if sp.shape.FillBoundingBox.IsEmpty() && hasReferenceStage(stages) {
		return len(stages) == 1 && stages[0].reference != nil
	}
	f, handled := buildSVGElementFilter(sp.ctx, builder, stages, sp.shape.StrokeBoundingBox())
	if !handled {
		return false
	}
	if f == nil {
		return true
	}
	willFill, willStroke := svgPaints(style)
	f.paint(sp.ctx, func(childCtx *svgPaintContext) {
		childOp := newSVGObjectPainter(childCtx.dc, style).withResources(
			sp.ctx.Resources, sp.shape.FillBoundingBox,
			svg.NewSVGLengthContext(sp.ctx.viewport.Size),
		)
		sp.rasterizeShape(childCtx, childOp, willFill, willStroke, style)
	}, func(out *image.RGBA) {
		sp.applyClipMaskToFilterOutput(out, f.graph.FilterRegion, f.mapping.ParamToLayer)
	})
	return true
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
// layer pixels, which toLayer maps user space into) by the shape's
// `clip-path` and/or `mask`, in that order. Per CSS Masking 1 §3.7 the filtered content is what gets
// clipped and masked — mirrors Blink ScopedSVGPaintState nesting the
// clip/mask recorders around the filter recorder.
//
// Both effects reduce to a per-pixel alpha coverage applied to the
// premultiplied buffer (kDstIn): clip-path produces a binary 0/255
// coverage from its geometry; mask produces luminance/alpha coverage
// from the rasterized `<mask>` children.
func (sp *svgShapePainter) applyClipMaskToFilterOutput(out *image.RGBA, region image.Rectangle, toLayer geometry.Transform) {
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
		if cov := sp.rasterizeClipCoverageForRegion(cp.ReferenceID, region, toLayer); cov != nil {
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
					toLayer,
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
// `region` (pixel (0,0) == region.Min in layer pixels). The buffer's
// alpha channel is the clip coverage: 255 inside the clip geometry, 0
// outside. Returns nil when the clipper is missing, empty, or part of a
// reference cycle (the caller then leaves the filtered output unclipped,
// matching SVG 2 §15.3 invalid-reference resilience).
//
// Mirrors how Blink's SVGClipPainter rasterizes a content clip into an
// alpha layer that then masks the painted (here: filtered) content.
func (sp *svgShapePainter) rasterizeClipCoverageForRegion(clipPathReferenceID string, region image.Rectangle, toLayer geometry.Transform) *image.RGBA {
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
	tmpR := sp.ctx.Renderer.newChildRenderer(cov)
	// Buffer pixel (0,0) == layer pixel region.Min; compose that translate
	// with the user→layer map so the clip geometry rasterizes where the
	// shape did.
	tmpR.dc.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	multiplyDCHomography(tmpR.dc, projectiveHomography(toLayer))
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

// svgPaints reports whether a shape with style fills and whether it
// strokes; a nil style takes the defaults, fill black and stroke none.
func svgPaints(style *css.Style) (willFill, willStroke bool) {
	return style == nil || style.GetFill().Kind != css.SVGPaintNone,
		style != nil && style.GetStroke().Kind != css.SVGPaintNone
}

// newSVGEffectSource returns an effect buffer covering region and a copy of
// pctx painting into it through toFrame, which maps user space into the
// frame region is in: the buffer's pixel (0, 0) is region.Min. toFrame may be
// projective (a masked shape rasterizes through the draw context's whole
// matrix, ancestor ∘ DeviceFromUser); the paint context's DeviceFromUser is
// its affine part (exact only for an affine matrix: LOU-778). A filter's is
// the user space's map into its layer (svgElementFilter.paint).
func newSVGEffectSource(pctx *svgPaintContext, region image.Rectangle, toFrame geometry.Transform) (*image.RGBA, svgPaintContext) {
	srcBuf := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	tmpR := pctx.Renderer.newChildRenderer(srcBuf)
	tmpR.dc.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	multiplyDCHomography(tmpR.dc, projectiveHomography(toFrame))

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
	toBuffer := geometry.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	childCtx := *pctx
	childCtx.dc = tmpR.dc
	childCtx.Renderer = tmpR
	childCtx.DeviceFromUser = toBuffer.Compose(geometry.AffineTransformFromTransform(toFrame))
	return srcBuf, childCtx
}

// rasterizeShape fills and strokes the shape onto pctx's draw context with
// childOp's paints, running paint-server fills and strokes through pctx. It
// reports false when the shape has no path. Shared by the filter source and
// the masked-shape buffer.
func (sp *svgShapePainter) rasterizeShape(pctx *svgPaintContext, childOp *svgObjectPainter, willFill, willStroke bool, style *css.Style) bool {
	tmpR := pctx.Renderer
	tmpR.dc.SetAntiAlias(shouldAntiAliasSVGShape(style))
	evenOdd := style != nil && style.GetFillRule() == css.SVGFillRuleEvenOdd
	if !buildPathOnDC(tmpR.dc, &sp.shape.Path) {
		return false
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
			tmpR.fillPathWithShader(pctx, &sp.shape.Path, fillRes.shader, evenOdd)
			tmpR.dc.ClearPath()
			if willStroke {
				buildPathOnDC(tmpR.dc, &sp.shape.Path)
			}
		case svgPaintSkip:
			if willStroke {
				tmpR.dc.ClearPath()
				buildPathOnDC(tmpR.dc, &sp.shape.Path)
			}
		}
	}
	if willStroke {
		strokeRes := childOp.applyStroke()
		switch strokeRes.mode {
		case svgPaintSolid:
			tmpR.dc.Stroke()
		case svgPaintShader:
			tmpR.strokePathWithShader(pctx, &sp.shape.Path, strokeRes.shader, style)
			tmpR.dc.ClearPath()
		case svgPaintSkip:
			tmpR.dc.ClearPath()
		}
	}
	return true
}
