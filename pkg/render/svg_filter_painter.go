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
	sp.compositeFilterResultOntoTarget(srcBuf, out, region)
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
// or the unfiltered source (srcBuf, used when ApplyToSRGB returned nil) onto
// the page DC's target, clipped to the SVG viewport. The viewport clip
// must be applied explicitly because the composite path bypasses the
// DC's transform/clip stack — see compositeFilterOutputOntoTarget.
func (sp *svgShapePainter) compositeFilterResultOntoTarget(srcBuf, out *image.RGBA, region image.Rectangle) {
	buf := out
	if buf == nil {
		// ApplyToSRGB returned nil (degenerate filter): composite the raw source.
		buf = srcBuf
	}
	compositeFilterOutputOntoTarget(sp.ctx.Renderer, buf, region.Min.X, region.Min.Y, sp.ctx.viewportDeviceClip())
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
	// The shape's chain has no layer: its reference box is also the
	// viewport and origin for userSpaceOnUse lengths, as before
	// FilterViewport existed.
	builder := &FilterEffectBuilder{
		ReferenceBox:       referenceBox,
		FilterViewport:     referenceBox,
		CurrentColor:       currentColor,
		Resources:          sp.ctx.Resources,
		ExternalSVGFetcher: sp.ctx.Renderer.externalSVGFetcher,
		ImageFetcher:       sp.ctx.Renderer.imageFetcher,
	}
	graph := builder.BuildFilterEffect(ops)
	if graph == nil {
		return false
	}
	region := graph.FilterRegion
	bw, bh := region.Dx(), region.Dy()
	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		return false
	}

	srcBuf := sp.renderShapeIntoFilterBuffer(region, bw, bh, willFill, willStroke, sp.shape.Style)
	graph.SetSourceImage(srcBuf)
	out := graph.ApplyToSRGB()
	// CSS Masking 1 §3.7 filter → clip → mask: the filtered result is
	// clipped and masked before compositing. The shorthand-filter path
	// (e.g. `filter="blur(20px)"` on an SVG `<rect>`) shares the same
	// post-filter clip/mask requirement as the url(#id) path above.
	if out != nil {
		sp.applyClipMaskToFilterOutput(out, region)
	}
	sp.compositeFilterResultOntoTarget(srcBuf, out, region)
	return true
}
