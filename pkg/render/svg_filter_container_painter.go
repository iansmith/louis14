package render

import (
	"image"
	"louis14/pkg/graphics/filters"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
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
	// buffer-size guard on the resolved region is the authoritative
	// "nothing to render" exit here.
	deviceFromLocal := pctx.DeviceFromUser.Compose(effective)
	graph, region, ok := buildSVGFilterElementGraph(pctx, filter,
		deviceFromLocal.MapRect(c.ObjectBoundingBox()), deviceFromLocal.MapPoint(geometry.PointF{X: 0, Y: 0}))
	if !ok {
		return false
	}
	srcBuf := paintSVGContainerFilterSource(pctx, c, effective, region)
	graph.SetSourceImage(srcBuf)
	out := graph.ApplyToSRGB()
	compositeSVGFilterResult(pctx, srcBuf, out, region)
	return true
}

// paintSVGContainerWithFilterChain paints a container through a CSS filter
// list that is not a lone url(): shorthand functions, or url() mixed with
// them. Blink builds a container's filter operations for both alike
// (SVGElementResourceClient::UpdateFilterData → FilterEffectBuilder::
// BuildFilterOperations, svg_resources.cc:371-422 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with the container's fill box
// as the reference box (ReferenceBoxForEffects) and its children's stroke
// bounding boxes as the paint extent (SVGContentContainer::
// ComputeStrokeBoundingBox). Returns false when no graph is produced, so
// the caller paints the children unfiltered.
func paintSVGContainerWithFilterChain(pctx *svgPaintContext, c *svg.SVGContainer, effective geometry.AffineTransform, ops []css.FilterFunction) bool {
	deviceFromLocal := pctx.DeviceFromUser.Compose(effective)
	graph, region, ok := buildSVGFilterChain(pctx, ops,
		deviceFromLocal.MapRect(c.ObjectBoundingBox()),
		filters.SnapRectOut(deviceFromLocal.MapRect(c.StrokeBoundingBox())),
		deviceFromLocal.MapPoint(geometry.PointF{X: 0, Y: 0}), c.Style.GetColor())
	if !ok {
		return false
	}
	srcBuf := paintSVGContainerFilterSource(pctx, c, effective, region)
	graph.SetSourceImage(srcBuf)
	compositeSVGFilterResult(pctx, srcBuf, graph.ApplyToSRGB(), region)
	return true
}

// paintSVGContainerFilterSource paints the container's children, under its
// own transform effective, into a buffer covering region (device pixels):
// the filter's SourceGraphic, the union of its children's painted output.
func paintSVGContainerFilterSource(pctx *svgPaintContext, c *svg.SVGContainer, effective geometry.AffineTransform, region image.Rectangle) *image.RGBA {
	srcBuf, childCtx := newSVGFilterSource(pctx, region)
	// Apply the container's own transform inside the buffer, as
	// paintSVGContainer's pushScopedSVGTransform does unfiltered.
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
	if filter := resolveSVGFilterValue(pctx, filterVal); filter != nil {
		return paintSVGContainerWithFilter(pctx, c, effective, filter)
	}
	// Anything else — CSS filter functions, or url() mixed with them —
	// goes through the filter chain, as for an SVG shape.
	ops := c.Style.GetFilter()
	return len(ops) > 0 && paintSVGContainerWithFilterChain(pctx, c, effective, ops)
}
