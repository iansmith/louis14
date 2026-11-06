package render

import (
	"image"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/graphics/filters"
	"louis14/pkg/layout/svg"
)

// paintSVGContainerWithFilter paints an SVGContainer (typically a <g>)
// through its referenced `<filter>` element. Mirrors Blink's
// SVGContainerPainter routing through SVGFilterPainter when a container
// carries a `filter` reference (svg/svg_container_painter.cc). It parallels
// paintWithSVGFilter in svg_filter_painter.go, with the children's painted
// output as the source graphic (paintSVGContainerFilterSource). effective is
// the container's own transform (ElementTransform).
//
// Returns true if the filter was actually dispatched (caller should
// skip the normal child-paint loop). Returns false when the filter
// resolves to nothing usable, so the caller falls back to unfiltered
// child paint.
func paintSVGContainerWithFilter(pctx *svgPaintContext, c *svg.SVGContainer, effective geometry.AffineTransform, filter *svg.SVGResourceFilter) bool {
	if filter == nil || c == nil {
		return false
	}

	// The container's bounding boxes are in its local space, inside its own
	// transform, where its children paint; deviceFromLocal maps them to
	// device pixels, and userSpaceOnUse coordinates are relative to its
	// origin (the referencing element's user space includes its transform).
	//
	// Empty bbox is allowed. Blink's SVG paint dispatch is paint-property
	// based: `ScopedSVGPaintState::ApplyPaintPropertyState`
	// (core/paint/scoped_svg_paint_state.cc:140-182 @ chromium-main
	// bf955d02bf0b0c67868b2e62359c0af199af9acc) installs the filter
	// effect node unconditionally when the element has a filter; the
	// source bbox only affects objectBoundingBox-unit resolution inside
	// the filter region computation, not dispatch. The downstream
	// `bw <= 0 || bh <= 0` guard on the resolved region is the
	// authoritative "nothing to render" exit here.
	deviceFromLocal := pctx.DeviceFromUser.Compose(effective)
	referenceBox := snapRectFToDevice(deviceFromLocal.MapRect(c.ObjectBoundingBox()))
	refBoxF := rectFFromInts(referenceBox.Min.X, referenceBox.Min.Y, referenceBox.Dx(), referenceBox.Dy())
	userOriginF := deviceFromLocal.MapPoint(geometry.PointF{X: 0, Y: 0})
	// Per Blink's LayoutSVGResourceContainer::ResolveRectangle the
	// userSpaceOnUse percent base is the SVG VIEWPORT, NOT the reference
	// box: the <svg>'s viewport, which the container's own transform does
	// not move, mapped to device pixels via DeviceFromUser. (Mirrors the
	// same fix in svg_filter_painter.go.)
	viewportDeviceSize := pctx.DeviceFromUser.MapRect(pctx.viewport).Size
	regionF := filter.ResourceBoundingBox(refBoxF, userOriginF, svg.NewSVGLengthContext(viewportDeviceSize))
	region := snapRectFToDevice(regionF)
	bw := region.Dx()
	bh := region.Dy()
	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		return false
	}

	space := filterInterpolationSpace(filter)
	srcBuf := paintSVGContainerFilterSource(pctx, c, effective, region)

	// Build + apply the filter graph. Mirrors paintWithSVGFilter's
	// post-source-render steps.
	adapter := &svgFilterElementAdapter{
		filter:             filter,
		filterRegion:       region,
		referenceBox:       referenceBox,
		userSpaceOrigin:    image.Point{X: int(math.Round(userOriginF.X)), Y: int(math.Round(userOriginF.Y))},
		space:              space,
		resources:          pctx.Resources,
		imageFetcher:       pctx.Renderer.imageFetcher,
		externalSVGFetcher: pctx.Renderer.externalSVGFetcher,
	}
	builder := filters.NewSVGFilterBuilder(space)
	graph := builder.BuildGraph(adapter)
	toComposite := srcBuf
	if graph != nil {
		graph.SetSourceImage(srcBuf)
		if out := graph.ApplyToSRGB(); out != nil {
			toComposite = out
		}
	}
	compositeFilterOutputOntoTarget(pctx.Renderer, toComposite, region.Min.X, region.Min.Y, pctx.viewportDeviceClip())
	return true
}

// paintSVGContainerWithFilterChain paints a container through a CSS filter
// list that is not a lone url(): shorthand functions, or url() mixed with
// them. Blink builds a container's filter operations for both alike
// (SVGElementResourceClient::UpdateFilterData → FilterEffectBuilder::
// BuildFilterOperations, svg_resources.cc:371-422 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with the container's fill box
// as the reference box (ReferenceBoxForEffects). Returns false when no graph
// is produced, so the caller paints the children unfiltered.
func paintSVGContainerWithFilterChain(pctx *svgPaintContext, c *svg.SVGContainer, effective geometry.AffineTransform, ops []css.FilterFunction) bool {
	if len(ops) == 1 && ops[0].Name == "url" {
		return false // a lone url() that did not resolve: paint unfiltered
	}
	deviceFromLocal := pctx.DeviceFromUser.Compose(effective)
	referenceBox := snapRectFToDevice(deviceFromLocal.MapRect(c.ObjectBoundingBox()))
	if referenceBox.Empty() && hasReferenceFilter(ops) {
		return false
	}
	// The chain's source extent is the children's paint: their stroke
	// bounding boxes (SVGContentContainer::ComputeStrokeBoundingBox).
	paintBox := snapRectFToDevice(deviceFromLocal.MapRect(c.StrokeBoundingBox()))
	if paintBox.Empty() {
		return false
	}
	builder := &FilterEffectBuilder{
		ReferenceBox:       referenceBox,
		FilterViewport:     referenceBox,
		CurrentColor:       c.Style.GetColor(),
		Resources:          pctx.Resources,
		ExternalSVGFetcher: pctx.Renderer.externalSVGFetcher,
		ImageFetcher:       pctx.Renderer.imageFetcher,
	}
	graph := builder.BuildFilterEffect(ops)
	if graph == nil {
		return false
	}
	// A url() stage's region is the reference box mapped through the
	// chain; the paint extent is united in, so the children's paint
	// outside the fill box stays.
	if !graph.FilterRegion.Empty() {
		graph.FilterRegion = graph.FilterRegion.Union(graph.MapRect(paintBox))
	}
	region := graph.EnsureRegion(paintBox, false)
	if bw, bh := region.Dx(), region.Dy(); bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		return false
	}
	srcBuf := paintSVGContainerFilterSource(pctx, c, effective, region)
	graph.SetSourceImage(srcBuf)
	toComposite := srcBuf
	if out := graph.ApplyToSRGB(); out != nil {
		toComposite = out
	}
	compositeFilterOutputOntoTarget(pctx.Renderer, toComposite, region.Min.X, region.Min.Y, pctx.viewportDeviceClip())
	return true
}

// paintSVGContainerFilterSource paints the container's children, under its
// own transform effective, into a buffer covering region (device pixels):
// the filter's SourceGraphic, the union of its children's painted output.
func paintSVGContainerFilterSource(pctx *svgPaintContext, c *svg.SVGContainer, effective geometry.AffineTransform, region image.Rectangle) *image.RGBA {
	// The Translate(-region.Min) shift makes the buffer-local (0, 0)
	// correspond to region.Min in device space.
	srcBuf := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	tmpR := NewRendererForImage(srcBuf)
	tmpR.dc.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	t := pctx.DeviceFromUser
	if !t.IsIdentity() {
		tmpR.dc.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}

	// Recurse into the container's children with a copy of the paint
	// context retargeted at the offscreen buffer, with the same
	// DeviceFromUser-plus-region-translate composition the shape-filter
	// path uses.
	childCtx := *pctx
	childCtx.dc = tmpR.dc
	childCtx.Renderer = tmpR
	childCtx.DeviceFromUser = geometry.Translate(
		float64(-region.Min.X), float64(-region.Min.Y),
	).Compose(pctx.DeviceFromUser)

	// Apply the container's own transform (ElementTransform) inside the
	// buffer. Mirrors paintSVGContainer's pushScopedSVGTransform — the
	// container's transform stack participates in the source-graphic
	// painting just like in the un-filtered path.
	guard := pushScopedSVGTransform(&childCtx, effective, geometry.RectF{})
	for _, child := range c.Children {
		paintSVGNode(&childCtx, child)
	}
	guard.Close()
	return srcBuf
}

// trySVGContainerFilterDispatch paints the container through its `filter`
// property when it has one that resolves: a lone url() to a usable
// `<filter>` (paintSVGContainerWithFilter), else its CSS filter functions or
// a url() chain (paintSVGContainerWithFilterChain), as for an SVG shape
// (svgShapePainter.paint). Returns true when the filter path took ownership
// of the paint (caller MUST skip its normal child loop), false otherwise.
func trySVGContainerFilterDispatch(pctx *svgPaintContext, c *svg.SVGContainer, effective geometry.AffineTransform) bool {
	if c == nil || c.Style == nil || pctx.clipPathContent {
		return false
	}
	filterVal, ok := c.Style.Get("filter")
	if !ok || filterVal == "" || filterVal == "none" {
		return false
	}
	if id, ok := css.ParseURLReference(filterVal); ok && pctx.Resources != nil {
		if filter, found := pctx.Resources.LookupAsFilter(id); found && filter != nil &&
			filter.CycleState() != svg.SVGCycleHasCycle {
			return paintSVGContainerWithFilter(pctx, c, effective, filter)
		}
	}
	// Anything else — CSS filter functions, or url() mixed with them —
	// goes through the filter chain, as for an SVG shape.
	ops := c.Style.GetFilter()
	return len(ops) > 0 && paintSVGContainerWithFilterChain(pctx, c, effective, ops)
}
