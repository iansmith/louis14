package svg

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// SVGViewportContainer is the layout node for a nested <svg> element.
// Mirrors Blink's LayoutSVGViewportContainer
// (core/layout/svg/layout_svg_viewport_container.{h,cc}):
//
//   - The nested <svg> positions a new viewport rect inside its parent
//     SVG coordinate system (x, y, width, height — all SVG lengths
//     resolved against the parent's length context).
//   - It establishes a new viewBox → viewport mapping via
//     BuildViewBoxToViewportTransform; the combined local transform is
//     translate(x, y) · viewBox→viewport (mirrors Blink's
//     LayoutSVGViewportContainer::CalculateLocalTransform).
//   - It defaults to overflow: hidden (SVG 2 §3.5) — the painter clips
//     descendants to the viewport rect.
//   - Establishes a new SVGLengthContext for descendants (their
//     percentage lengths resolve against the nested viewport, not the
//     outer SVG's container size).
//
// Like SVGContainer, the layout node only carries data; the painter in
// pkg/render/svg_container_painter.go owns the DrawContext and runs
// the actual clip + transform stack.
type SVGViewportContainer struct {
	// TagName is "svg" (lowercased by the HTML tokenizer).
	TagName string

	// Style is the resolved computed style for the nested <svg>'s
	// element. The painter reads `overflow` here (Phase 3 hardcodes
	// the SVG 2 default of `hidden` because nested <svg>'s UA style
	// is overflow: hidden — that's the spec default per §3.5, not a
	// per-element override) and `transform` (CSS transform on the
	// nested <svg>) via ParseCSSTransformForSVG.
	Style *css.Style

	// X, Y position the nested viewport within the parent SVG
	// coordinate system, in user units. Default 0, 0.
	X, Y float64

	// Width, Height are the size of the nested viewport rect in the
	// parent SVG coordinate system, in user units. A 0/negative
	// width or height produces an empty viewport (no descendant
	// paint), matching SVG 2 §8.2.3.
	Width, Height float64

	// ViewBox is the parsed viewBox attribute on the nested <svg>.
	// Invalid/missing leaves ViewBox.Valid = false (descendants then
	// use the nested viewport's user-unit space directly).
	ViewBox ViewBox

	// PreserveAspectRatio is the parsed preserveAspectRatio attribute.
	// Defaults to xMidYMid meet per SVG 2 §8.2.4.
	PreserveAspectRatio PreserveAspectRatio

	// LocalToBorderBoxTransform is the viewBox → viewport mapping
	// (NOT including the translate(x, y) that positions the nested
	// viewport in its parent). Mirrors the same name on SVGRoot for
	// symmetry. Composed with Translate(X, Y) at paint time.
	LocalToBorderBoxTransform geometry.AffineTransform

	// Children are the SVG children of the nested <svg> in DOM order.
	Children []SVGNode

	// bbox is the accumulated bbox of children mapped through the
	// nested viewport's local transform.
	bbox geometry.RectF
}

// NewSVGViewportContainer constructs a nested-<svg> container by
// reading x/y/width/height/viewBox/preserveAspectRatio off the
// element adapter and computing the viewBox→viewport mapping. The
// children are NOT built here — BuildSVGTree handles that with a
// fresh SVGLengthContext.
func NewSVGViewportContainer(elt ElementAdapter, parentCtx SVGLengthContext) *SVGViewportContainer {
	if elt == nil {
		return nil
	}
	vc := &SVGViewportContainer{
		TagName:                   elt.TagName(),
		PreserveAspectRatio:       DefaultPreserveAspectRatio(),
		LocalToBorderBoxTransform: geometry.Identity(),
	}

	// x, y default to 0; resolved against the parent's length
	// context (the nested viewport's own context isn't built until
	// width/height are known).
	vc.X = getLength(elt, "x", LengthModeWidth, parentCtx, 0)
	vc.Y = getLength(elt, "y", LengthModeHeight, parentCtx, 0)
	// width, height default to 100% per SVG 2 §8.2.3 — but only on
	// the outermost <svg>. For a nested <svg>, the spec defaults to
	// 100% as well (it inherits the same defaulting from the
	// SVGSVGElement spec). We honour that.
	if v, ok := elt.Attribute("width"); ok {
		vc.Width = parentCtx.Resolve(v, LengthModeWidth)
	} else {
		vc.Width = parentCtx.ViewportSize().Width
	}
	if v, ok := elt.Attribute("height"); ok {
		vc.Height = parentCtx.Resolve(v, LengthModeHeight)
	} else {
		vc.Height = parentCtx.ViewportSize().Height
	}
	if vc.Width < 0 {
		vc.Width = 0
	}
	if vc.Height < 0 {
		vc.Height = 0
	}

	// Parse viewBox.
	vb, ok := elt.Attribute("viewBox")
	if !ok {
		vb, ok = elt.Attribute("viewbox")
	}
	if ok {
		if x, y, w, h, parsed := ParseViewBox(vb); parsed {
			vc.ViewBox = ViewBox{X: x, Y: y, Width: w, Height: h, Valid: true}
		}
	}
	// Parse preserveAspectRatio.
	if par, ok := elt.Attribute("preserveAspectRatio"); ok {
		vc.PreserveAspectRatio = ParsePreserveAspectRatio(par)
	} else if par, ok := elt.Attribute("preserveaspectratio"); ok {
		vc.PreserveAspectRatio = ParsePreserveAspectRatio(par)
	}

	// viewBox → viewport mapping. The viewport rect for the mapping
	// has origin (0, 0) — the translate(X, Y) is composed
	// separately so the viewBox→viewport mapping is reusable as the
	// nested viewport's "intrinsic" coordinate transform (mirrors
	// Blink's split: LocalToParent = translate(viewport.origin) ·
	// viewBoxToViewport, with viewBoxToViewport based at (0,0)).
	innerViewport := geometry.NewRectF(0, 0, vc.Width, vc.Height)
	vc.LocalToBorderBoxTransform = BuildViewBoxToViewportTransform(
		vc.ViewBox, innerViewport, vc.PreserveAspectRatio,
	)
	return vc
}

// AppendChild appends a child to the nested-<svg> in DOM order.
func (v *SVGViewportContainer) AppendChild(child SVGNode) {
	if child == nil {
		return
	}
	v.Children = append(v.Children, child)
}

// ChildLengthContext returns the SVGLengthContext that the nested
// <svg>'s children should use for resolving percentage lengths.
// Mirrors the way Blink's SVGSVGElement (and LayoutSVGViewportContainer)
// installs a new SVGLengthContext keyed on the nested viewport size.
func (v *SVGViewportContainer) ChildLengthContext() SVGLengthContext {
	return NewSVGLengthContext(geometry.SizeF{Width: v.Width, Height: v.Height})
}

// ObjectBoundingBox returns the union of children's bounding boxes
// mapped through the nested viewport's local transform. Mirrors
// Blink's LayoutSVGContainer::ObjectBoundingBox (which is the union
// of children, transformed by each child's local transform — for a
// viewport container, the viewBox transform applies between the
// child-space union and the parent-space bbox).
func (v *SVGViewportContainer) ObjectBoundingBox() geometry.RectF {
	return v.bbox
}

// LocalTransform returns the transform that maps a point in this
// nested viewport's local (viewBox / user-unit) space into its
// parent's SVG coordinate system: translate(X, Y) · viewBox→viewport.
//
// SVG 2 §7.5 says CSS `transform` on a nested <svg> is applied AFTER
// the SVG-defined transform — composed as `cssT ∘ svgT` at the paint
// stage. To keep the LocalTransform API consistent across SVGNode
// implementations, this returns the SVG-only part; the painter is
// responsible for composing the CSS transform on top of it (mirrors
// Blink's split between `LayoutSVGViewportContainer::LocalToParent`
// for the SVG-only transform and the paint-time application of CSS
// transform via TransformHelper::ComputeTransform).
func (v *SVGViewportContainer) LocalTransform() geometry.AffineTransform {
	if v.X == 0 && v.Y == 0 {
		return v.LocalToBorderBoxTransform
	}
	return geometry.Translate(v.X, v.Y).Compose(v.LocalToBorderBoxTransform)
}

// UpdateSVGLayout walks children and accumulates their bbox under
// this viewport's local transform.
func (v *SVGViewportContainer) UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult {
	var combined SVGLayoutResult
	var unionRect geometry.RectF
	hasAny := false
	for _, child := range v.Children {
		res := child.UpdateSVGLayout(info)
		if res.BoundsChanged {
			combined.BoundsChanged = true
		}
		if res.HasViewportDependence {
			combined.HasViewportDependence = true
		}
		childBBox := child.ObjectBoundingBox()
		if childBBox.IsEmpty() {
			continue
		}
		mapped := child.LocalTransform().MapRect(childBBox)
		if mapped.IsEmpty() {
			continue
		}
		if !hasAny {
			unionRect = mapped
			hasAny = true
		} else {
			unionRect = unionRects(unionRect, mapped)
		}
	}
	if hasAny {
		v.bbox = unionRect
	} else {
		v.bbox = geometry.RectF{}
	}
	return combined
}

// Paint is a no-op; the render-side painter walks the tree directly.
func (v *SVGViewportContainer) Paint(ctx *SVGPaintContext) {}

// SVGContainer is the layout node for an SVG container element. Phase
// 1 used this as a recursive structural stub for any unrecognized tag;
// Phase 3 upgrades it into a real transformable container (mirrors
// Blink's LayoutSVGTransformableContainer for `<g>`) and continues to
// serve as the fallback for SVG elements we don't yet recognize
// specifically (Phase 6 will route resource elements to dedicated
// types).
//
// Mirrors core/layout/svg/layout_svg_container.{h,cc}: children list,
// a parsed local transform, computed style for paint, and an
// ObjectBoundingBox that's the union of children mapped through this
// container's local transform.
type SVGContainer struct {
	// TagName is the SVG tag this node represents.
	TagName string

	// Style is the resolved computed style for the container element.
	// Carries fill/stroke/etc. for child inheritance (the cascade
	// has already handled inheritance — this is the container's own
	// computed values, used by the painter for paint-state setup).
	Style *css.Style

	// transform is the parsed `transform` attribute (SVG-side).
	// CSS `transform` is folded in by the painter via
	// ParseCSSTransformForSVG so that animations of the CSS
	// property reach the rasterizer.
	transform geometry.AffineTransform

	// Children are the SVG children of this container in DOM order.
	Children []SVGNode

	// bbox is the accumulated bbox of children mapped through
	// this.transform.
	bbox geometry.RectF
}

// NewSVGContainer constructs a container with the identity transform.
// Callers (BuildSVGTree) populate the transform separately via
// ParseSVGTransform on the element's `transform` attribute.
func NewSVGContainer(tagName string) *SVGContainer {
	return &SVGContainer{
		TagName:   tagName,
		transform: geometry.Identity(),
	}
}

// SetSVGTransform sets the parsed SVG `transform` attribute on the
// container. BuildSVGTree calls this after construction so a `<g>`'s
// `transform="translate(...)"` is honoured by the painter.
func (c *SVGContainer) SetSVGTransform(t geometry.AffineTransform) {
	c.transform = t
}

// SVGAttributeTransform returns the parsed SVG `transform` attribute
// (NOT including any CSS transform — that's composed at paint time).
// Mirrors Blink's LayoutSVGTransformableContainer::AdditionalTransform.
func (c *SVGContainer) SVGAttributeTransform() geometry.AffineTransform {
	return c.transform
}

// AppendChild adds a child node to the container, in DOM order.
func (c *SVGContainer) AppendChild(child SVGNode) {
	if child == nil {
		return
	}
	c.Children = append(c.Children, child)
}

// ObjectBoundingBox returns the union of children's bounding boxes
// mapped through this container's local transform.
func (c *SVGContainer) ObjectBoundingBox() geometry.RectF {
	return c.bbox
}

// LocalTransform returns the container's SVG `transform` attribute.
// CSS `transform` is composed on top of this at paint time so the
// SVGNode interface stays uniform (callers that walk the tree without
// a painter — e.g. bbox accumulation — see only the SVG-attribute
// transform, which is the stable value the layout pass owns).
func (c *SVGContainer) LocalTransform() geometry.AffineTransform {
	return c.transform
}

// UpdateSVGLayout walks children and unions their bbox under this
// container's local transform.
func (c *SVGContainer) UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult {
	var combined SVGLayoutResult
	var unionRect geometry.RectF
	hasAny := false
	for _, child := range c.Children {
		res := child.UpdateSVGLayout(info)
		if res.BoundsChanged {
			combined.BoundsChanged = true
		}
		if res.HasViewportDependence {
			combined.HasViewportDependence = true
		}
		childBBox := child.ObjectBoundingBox()
		if childBBox.IsEmpty() {
			continue
		}
		mapped := child.LocalTransform().MapRect(childBBox)
		if mapped.IsEmpty() {
			continue
		}
		if !hasAny {
			unionRect = mapped
			hasAny = true
		} else {
			unionRect = unionRects(unionRect, mapped)
		}
	}
	if hasAny {
		c.bbox = unionRect
	} else {
		c.bbox = geometry.RectF{}
	}
	return combined
}

// Paint is a no-op; the render-side painter walks the tree directly.
func (c *SVGContainer) Paint(ctx *SVGPaintContext) {}

// unionRects returns the smallest axis-aligned rect containing both
// inputs. Mirrors gfx::RectF::Union semantics: empty inputs are
// absorbed by non-empty ones; two non-empty inputs produce the
// min-min / max-max envelope.
func unionRects(a, b geometry.RectF) geometry.RectF {
	if a.IsEmpty() {
		return b
	}
	if b.IsEmpty() {
		return a
	}
	minX := a.X()
	if b.X() < minX {
		minX = b.X()
	}
	minY := a.Y()
	if b.Y() < minY {
		minY = b.Y()
	}
	maxX := a.Right()
	if b.Right() > maxX {
		maxX = b.Right()
	}
	maxY := a.Bottom()
	if b.Bottom() > maxY {
		maxY = b.Bottom()
	}
	return geometry.NewRectF(minX, minY, maxX-minX, maxY-minY)
}
