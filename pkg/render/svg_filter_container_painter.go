package render

import (
	"louis14/pkg/css"
	"louis14/pkg/layout/svg"
)

// trySVGFilterDispatch paints an SVG container or <image> through its
// `filter` when it has one that resolves: a lone url() to a `<filter>`, CSS
// filter functions, or a mix, built in the element's user space, its own
// transform included, which pctx's draw context is already in
// (buildSVGElementFilter). paintContent paints what the filter's source
// graphic holds: a container's children; nil for an <image>, whose image is
// not drawn yet (LOU-797). Mirrors Blink's SVGContainerPainter, whose filter
// effect node is installed when the container has a filter
// (SVGElementResourceClient::UpdateFilterData → FilterEffectBuilder::
// BuildFilterOperations, svg_resources.cc:371-422 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with the element's fill box as
// the reference box (ReferenceBoxForEffects) and its stroke bounding box as
// the paint extent (SVGContentContainer::ComputeStrokeBoundingBox). An empty
// fill box is allowed: the box only resolves objectBoundingBox units.
//
// Returns true when the filter path took ownership of the paint (the caller
// MUST skip its normal content paint), false when the element paints
// unfiltered.
func trySVGFilterDispatch(pctx *svgPaintContext, node svg.SVGNode, style *css.Style, paintContent func(*svgPaintContext)) bool {
	if style == nil || pctx.clipPathContent {
		return false
	}
	builder := newSVGFilterEffectBuilder(pctx, node.ObjectBoundingBox(), style.GetColor())
	f, handled := buildSVGElementFilter(pctx, builder, builder.resolveStages(style.GetFilter()), node.StrokeBoundingBox())
	if !handled {
		return false
	}
	if f != nil {
		if paintContent == nil {
			paintContent = func(*svgPaintContext) {}
		}
		f.paint(pctx, paintContent, nil)
	}
	return true
}
