package svg

import "louis14/pkg/geometry"

// SVGPaintContext is the state threaded through SVG paint. Phase 1 leaves
// it almost empty — Phase 2 grows it with the DrawContext, accumulated
// AffineTransform, current viewport, and (Phase 6) the SVGResourceRegistry.
// Mirrors the inputs Blink's SVG painters carry as PaintInfo extensions.
type SVGPaintContext struct {
	// CurrentTransform is the accumulated affine transform from the SVG
	// root to the current node. Phase 1: not yet consumed.
	CurrentTransform geometry.AffineTransform
}

// SVGNode is the interface every SVG layout node implements. Mirrors the
// shared surface of Blink's LayoutSVGRoot / LayoutSVGContainer /
// LayoutSVGShape: object-bounding-box accounting, the local transform,
// the recursive layout pass, and paint.
//
// Phase 1: every implementation is a stub. Phase 2 fills in shapes;
// Phase 3 fills in container transforms.
type SVGNode interface {
	// ObjectBoundingBox returns the node's tight bounding box in its
	// local coordinate system, excluding stroke. Mirrors Blink's
	// LayoutSVGModelObject::ObjectBoundingBox. Used by gradient/pattern
	// resolution with objectBoundingBox units, by mask/clip-path units,
	// and by parents accumulating their own bounding box.
	ObjectBoundingBox() geometry.RectF

	// LocalTransform returns the affine transform this node applies to
	// its own children. For shapes the default is identity (shapes don't
	// have their own transform). For <g> it is the parsed
	// `transform="…"` attribute. Mirrors Blink's
	// LayoutSVGModelObject::LocalSVGTransform.
	LocalTransform() geometry.AffineTransform

	// UpdateSVGLayout runs the per-node layout pass: re-resolve geometry
	// attributes, rebuild paths, propagate the bounding box up. Mirrors
	// Blink's LayoutSVGContainer::UpdateSVGLayout (which dispatches to
	// concrete subclasses' UpdateLayoutInternal).
	UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult

	// Paint renders this node and its descendants into ctx. Phase 1
	// stubs return without touching the DrawContext — SVG content still
	// renders blank, exactly as before. Phase 2 wires real paint.
	Paint(ctx *SVGPaintContext)
}

// SVGContainer and SVGViewportContainer live in svg_container.go (split
// out in Phase 3 when both gained transform parsing, child-bbox
// accumulation, and computed-style storage). svg_node.go is now the
// home of the SVGNode interface + SVGPaintContext only.
