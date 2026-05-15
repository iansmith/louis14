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

// SVGContainer is the Phase-1 fallback node for any tag we don't yet
// recognize specifically. It is a recursive structural shell: it carries
// children, accumulates their bounding boxes (Phase 2+ — empty for now),
// and forwards Paint to each child. No transform, no clipping, no
// per-element behaviour — just a placeholder so BuildSVGTree can produce
// a well-formed subtree.
//
// Mirrors the role Blink's LayoutSVGContainer plays for plain <g> minus
// transforms — and is the base shape every later node (SVGShape,
// SVGTransformableContainer, SVGViewportContainer, SVGResourceContainer)
// will replace as the foundation grows.
type SVGContainer struct {
	// TagName is the SVG element tag this node represents, for debugging.
	TagName string

	// Children are the structural SVG children in tree order.
	Children []SVGNode

	// bbox is the accumulated object bounding box. Phase 1: always empty.
	bbox geometry.RectF
}

// NewSVGContainer constructs an empty container for the given tag.
func NewSVGContainer(tagName string) *SVGContainer {
	return &SVGContainer{TagName: tagName}
}

// AppendChild adds a child node to the container, in DOM order.
func (c *SVGContainer) AppendChild(child SVGNode) {
	if child == nil {
		return
	}
	c.Children = append(c.Children, child)
}

// ObjectBoundingBox returns the accumulated bbox.
func (c *SVGContainer) ObjectBoundingBox() geometry.RectF { return c.bbox }

// LocalTransform returns identity — plain containers add no transform.
func (c *SVGContainer) LocalTransform() geometry.AffineTransform { return geometry.Identity() }

// UpdateSVGLayout walks children, returning the union of their bounds.
// Phase 1: every child returns an empty result, so the union is empty.
func (c *SVGContainer) UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult {
	var combined SVGLayoutResult
	for _, child := range c.Children {
		r := child.UpdateSVGLayout(info)
		if r.BoundsChanged {
			combined.BoundsChanged = true
		}
		if r.HasViewportDependence {
			combined.HasViewportDependence = true
		}
	}
	c.bbox = geometry.RectF{} // Phase 1: nothing paints, bbox is empty.
	return combined
}

// Paint forwards Paint to each child in tree order. Phase 1: every child
// is a stub, so this still paints nothing.
func (c *SVGContainer) Paint(ctx *SVGPaintContext) {
	for _, child := range c.Children {
		child.Paint(ctx)
	}
}
