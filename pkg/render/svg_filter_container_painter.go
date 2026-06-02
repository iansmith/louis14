package render

import (
	"image"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/graphics/filters"
	"louis14/pkg/layout/svg"
)

// paintSVGContainerWithFilter dispatches an SVGContainer (typically a
// <g>) through its referenced `<filter>` element. Mirrors Blink's
// SVGContainerPainter routing through SVGFilterPainter when a container
// carries a `filter` reference (svg/svg_container_painter.cc).
//
// The structure parallels paintWithSVGFilter in svg_filter_painter.go,
// but the source-buffer step recurses into children via paintSVGNode
// rather than running fill+stroke directly — a container's "source
// graphic" is the union of its children's painted output.
//
// Returns true if the filter was actually dispatched (caller should
// skip the normal child-paint loop). Returns false when the filter
// resolves to nothing usable, so the caller falls back to unfiltered
// child paint.
func paintSVGContainerWithFilter(pctx *svgPaintContext, c *svg.SVGContainer, filter *svg.SVGResourceFilter) bool {
	if filter == nil || c == nil {
		return false
	}

	// SVGContainer.ObjectBoundingBox() folds in the container's own SVG
	// `transform`, so the returned rect is in the caller's coord space
	// (before we push the container's own transform).
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
	userBBox := c.ObjectBoundingBox()
	deviceBBox := pctx.DeviceFromUser.MapRect(userBBox)
	referenceBox := image.Rect(
		int(math.Floor(deviceBBox.X())),
		int(math.Floor(deviceBBox.Y())),
		int(math.Ceil(deviceBBox.Right())),
		int(math.Ceil(deviceBBox.Bottom())),
	)

	refBoxF := geometry.NewRectF(
		float64(referenceBox.Min.X),
		float64(referenceBox.Min.Y),
		float64(referenceBox.Dx()),
		float64(referenceBox.Dy()),
	)
	userOriginF := pctx.DeviceFromUser.MapPoint(geometry.PointF{X: 0, Y: 0})
	// Per Blink's LayoutSVGResourceContainer::ResolveRectangle the
	// userSpaceOnUse percent base is the SVG VIEWPORT, NOT the reference
	// box. Map the user-space viewport to device pixels via
	// DeviceFromUser. (Mirrors the same fix in svg_filter_painter.go.)
	viewportDeviceSize := pctx.DeviceFromUser.MapRect(pctx.viewport).Size
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
		return false
	}

	// Resolve operating space from color-interpolation-filters.
	space := filters.InterpolationSpaceLinearRGB
	if st := filter.Style(); st != nil {
		if v, ok := st.Get("color-interpolation-filters"); ok {
			space = filters.ResolveInterpolationSpace(v)
		}
	}

	// Allocate the source buffer + a child renderer whose target is
	// that buffer. The Translate(-region.Min) shift makes the
	// buffer-local (0, 0) correspond to region.Min in device space.
	srcBuf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	tmpR := NewRendererForImage(srcBuf)
	tmpR.dc.Translate(float64(-region.Min.X), float64(-region.Min.Y))
	t := pctx.DeviceFromUser
	if !t.IsIdentity() {
		tmpR.dc.MultiplyMatrix(t.A, t.B, t.C, t.D, t.E, t.F)
	}

	// Recurse into the container's children with a copy of the paint
	// context retargeted at the offscreen buffer. The child context
	// uses the same DeviceFromUser-plus-region-translate composition
	// the shape-filter path uses (see svg_filter_painter.go:132-137 for
	// the rationale).
	childCtx := *pctx
	childCtx.dc = tmpR.dc
	childCtx.Renderer = tmpR
	childCtx.DeviceFromUser = geometry.Translate(
		float64(-region.Min.X), float64(-region.Min.Y),
	).Compose(pctx.DeviceFromUser)

	// Apply the container's own SVG + CSS transform inside the buffer.
	// Mirrors paintSVGContainer's pushScopedSVGTransform — the
	// container's transform stack participates in the source-graphic
	// painting just like in the un-filtered path.
	cssT := svg.ParseCSSTransformForSVG(c.Style)
	effective := cssT.Compose(c.SVGAttributeTransform())
	guard := pushScopedSVGTransform(&childCtx, effective, geometry.RectF{})
	for _, child := range c.Children {
		paintSVGNode(&childCtx, child)
	}
	guard.Close()

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
	viewportClip := image.Rect(
		int(pctx.originX),
		int(pctx.originY),
		int(pctx.originX+pctx.viewport.Width()),
		int(pctx.originY+pctx.viewport.Height()),
	)
	toComposite := srcBuf
	compositeSpace := space
	if graph != nil {
		graph.SetSourceImage(srcBuf)
		if out := graph.Apply(); out != nil {
			toComposite = out
			compositeSpace = graph.LastEffect.OperatingSpace()
		}
	}
	compositeFilterOutputOntoTarget(pctx.Renderer, toComposite, region.Min.X, region.Min.Y, viewportClip, compositeSpace)
	return true
}

// trySVGContainerFilterDispatch consults the container's `filter`
// presentation property and dispatches through paintSVGContainerWithFilter
// if a usable `<filter>` resource is found. Returns true when the
// filter path took ownership of the paint (caller MUST skip its normal
// child loop), false otherwise.
//
// Mirrors the gate in svgShapePainter.paint() that routes filtered
// shapes through paintWithSVGFilter — same lookup chain
// (ParseURLReference → registry.LookupAsFilter → cycle-state check).
func trySVGContainerFilterDispatch(pctx *svgPaintContext, c *svg.SVGContainer) bool {
	if c == nil || c.Style == nil {
		return false
	}
	filterVal, ok := c.Style.Get("filter")
	if !ok || filterVal == "" || filterVal == "none" {
		return false
	}
	id, ok := css.ParseURLReference(filterVal)
	if !ok {
		return false
	}
	if pctx.Resources == nil {
		return false
	}
	filter, found := pctx.Resources.LookupAsFilter(id)
	if !found || filter == nil {
		return false
	}
	if filter.CycleState() == svg.SVGCycleHasCycle {
		return false
	}
	return paintSVGContainerWithFilter(pctx, c, filter)
}
