package render

import (
	"louis14/pkg/layout/svg"
)

// trySVGContainerFilterDispatch paints an SVGContainer (typically a <g>)
// through its `filter` when it has one that resolves: a lone url() to a
// `<filter>`, CSS filter functions, or a mix, built in the container's user
// space, its own transform included, which pctx's draw context is already in
// (buildSVGElementFilter). The filter's source graphic is the children's
// painted output. Mirrors Blink's SVGContainerPainter, whose filter effect
// node is installed when the container has a filter
// (SVGElementResourceClient::UpdateFilterData → FilterEffectBuilder::
// BuildFilterOperations, svg_resources.cc:371-422 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with the container's fill box
// as the reference box (ReferenceBoxForEffects) and its children's stroke
// bounding boxes as the paint extent (SVGContentContainer::
// ComputeStrokeBoundingBox). An empty fill box is allowed: the box only
// resolves objectBoundingBox units.
//
// Returns true when the filter path took ownership of the paint (the caller
// MUST skip its normal child loop), false when the container paints
// unfiltered.
func trySVGContainerFilterDispatch(pctx *svgPaintContext, c *svg.SVGContainer) bool {
	if c == nil || c.Style == nil || pctx.clipPathContent {
		return false
	}
	f, handled := buildSVGElementFilter(pctx, c.Style.GetFilter(), c.ObjectBoundingBox(), c.StrokeBoundingBox(), c.Style.GetColor())
	if !handled {
		return false
	}
	if f != nil {
		f.paint(pctx, func(childCtx *svgPaintContext) {
			for _, child := range c.Children {
				paintSVGNode(childCtx, child)
			}
		}, nil)
	}
	return true
}
