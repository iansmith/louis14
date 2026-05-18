package svg

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// SVGResourceClipper is the layout node for a `<clipPath>` resource
// element. Mirrors Blink's LayoutSVGResourceClipper
// (core/layout/svg/layout_svg_resource_clipper.{h,cc}).
//
// Per SVG 1.1 §14.3 a `<clipPath>` contains shape children whose
// geometry forms a clipping region for any element that references it
// via `clip-path: url(#id)`. The clipping rule is the unioned
// silhouette of the children's filled paths (the clip's `clip-rule`
// or each child's `clip-rule` decides nonzero-vs-evenodd).
//
// Two evaluation modes:
//
//  1. Shape-based fast path: exactly one shape child whose geometry is
//     a single path (rect/circle/ellipse/path/etc.). AsPath() returns
//     the child's path, transformed to the target's reference space if
//     `clipPathUnits="objectBoundingBox"`. This is the path that gets
//     pushed onto the DrawContext via Clip() — mazarin's
//     clip-as-mask now supports non-rect paths.
//
//  2. Content-based fallback: multiple children, or content that the
//     fast path can't express as a single path. The painter rasterizes
//     the child subtree into an alpha buffer and intersects with the
//     current clip. Mirrors Blink's LayoutSVGResourceClipper::
//     CreatePaintRecord. Phase 5 stores the children on the clipper;
//     the render-side svg_clip_painter walks them.
type SVGResourceClipper struct {
	// ResourceID is the clipper's `id` attribute. Empty for clippers
	// without an id (which can't be referenced and are hence inert —
	// the registry rejects them).
	ResourceID string

	// ClipPathUnits is the coordinate-space mode for the clipper's
	// child geometry. Default `userSpaceOnUse` per SVG 1.1 §14.3.5.
	ClipPathUnits SVGUnitType

	// Transform is the parsed `transform` attribute on the `<clipPath>`
	// element itself (SVG 2 lifts the SVG 1.1 restriction that only
	// `<g>` and shapes accept transform). Identity when absent.
	// Composed onto the clip geometry's user-space mapping.
	Transform geometry.AffineTransform

	// Children are the SVG children of the `<clipPath>` element in DOM
	// order. Used for the content-based fallback. May contain shapes,
	// `<use>` (Phase 6+), or nested `<g>` containers. Phase 5 paints
	// shapes only.
	Children []SVGNode

	// elementStyle is the clipper element's resolved style. Carries
	// the `clip-rule` property for the unioned-children fill rule.
	elementStyle *css.Style

	// cycleState carries the cycle-detection state machine; flipped
	// by the SVGResourcesCycleSolver DFS once per paint frame.
	cycleState SVGCycleState
}

// ID implements the SVG resource interface (registry lookup key).
func (c *SVGResourceClipper) ID() string { return c.ResourceID }

// Kind implements SVGResource — Phase 6.
func (c *SVGResourceClipper) Kind() SVGResourceKind { return SVGResourceKindClipper }

// CycleState implements SVGResource — Phase 6.
func (c *SVGResourceClipper) CycleState() SVGCycleState { return c.cycleState }

// SetCycleState implements SVGResource — Phase 6.
func (c *SVGResourceClipper) SetCycleState(s SVGCycleState) { c.cycleState = s }

// ObjectBoundingBox returns an empty bbox — clipper resource elements
// don't contribute to the SVG tree's paint bounds. Mirrors Blink's
// LayoutSVGHiddenContainer::ObjectBoundingBox.
func (c *SVGResourceClipper) ObjectBoundingBox() geometry.RectF { return geometry.RectF{} }

// LocalTransform returns identity — clipper elements are never in the
// paint tree, so their LocalTransform doesn't compose into ancestor
// paint transforms.
func (c *SVGResourceClipper) LocalTransform() geometry.AffineTransform { return geometry.Identity() }

// UpdateSVGLayout is a no-op for clipper resource elements.
func (c *SVGResourceClipper) UpdateSVGLayout(SVGLayoutInfo) SVGLayoutResult {
	return SVGLayoutResult{}
}

// Paint is a no-op — the render-side svg_clip_painter walks the
// clipper's children directly when a `clip-path: url(#id)` reference
// resolves to it. Phase 3 established the convention: SVGNode.Paint is
// a no-op everywhere; render dispatches the painter directly.
func (c *SVGResourceClipper) Paint(*SVGPaintContext) {}

// Style returns the clipper element's resolved style (or nil). Used by
// the render-side painter to read `clip-rule`.
func (c *SVGResourceClipper) Style() *css.Style { return c.elementStyle }

// AsPath returns the clipper's geometry as a single user-space Path
// when the shape-based fast path is available. Returns (path, true)
// when there is exactly one shape child and the clipper has no
// transform; returns (_, false) otherwise — callers must use the
// content-based rasterization fallback.
//
// `target` is the referencing element's user-space reference rect
// (typically its FillBoundingBox). When ClipPathUnits is
// objectBoundingBox, the child path's coords are fractions of `target`
// and we transform them into the target's user-space rect by
// translate(target.x, target.y) ∘ scale(target.w, target.h).
//
// Mirrors Blink's LayoutSVGResourceClipper::AsPath: the fast path
// that lets a simple `<clipPath><rect/></clipPath>` push a single
// path-clip rather than spinning up an alpha buffer.
func (c *SVGResourceClipper) AsPath(target geometry.RectF) (Path, bool) {
	if c == nil {
		return Path{}, false
	}
	if len(c.Children) != 1 {
		return Path{}, false
	}
	shape, ok := c.Children[0].(*SVGShape)
	if !ok {
		return Path{}, false
	}
	if shape.Path.Empty() {
		return Path{}, false
	}
	// Build the transformed path. For userSpaceOnUse the path is in
	// the same user-space as the referencing element — no extra
	// mapping needed (the painter pushes the clip in user-space
	// alongside the element it clips). For objectBoundingBox, map
	// fractions onto the target rect via translate ∘ scale.
	t := geometry.Identity()
	if c.ClipPathUnits == SVGUnitObjectBoundingBox {
		t = geometry.Translate(target.X(), target.Y()).Compose(
			geometry.Scale(target.Width(), target.Height()),
		)
	}
	// Compose `transform` on the clipper itself AFTER the units
	// mapping — same composition order as paint servers (units first,
	// then resource-element transform).
	if !c.Transform.IsIdentity() {
		t = t.Compose(c.Transform)
	}
	if t.IsIdentity() {
		// Return a copy so callers can't mutate the cached child.
		return clonePath(&shape.Path), true
	}
	return transformPath(&shape.Path, t), true
}

// HasContent reports whether the clipper has any rasterizable
// children. Used by the painter to short-circuit a fully-empty clipper
// (which clips everything away per SVG 1.1 §14.3.5).
func (c *SVGResourceClipper) HasContent() bool {
	return c != nil && len(c.Children) > 0
}

// NewSVGResourceClipper builds an SVGResourceClipper from a `<clipPath>`
// ElementAdapter. Parses clipPathUnits + transform attributes and
// recursively builds the child subtree against `parentCtx` (the
// referencing scope's length context — `<clipPath>` children resolve
// percentages against the same viewport the clipper was declared in
// when clipPathUnits=userSpaceOnUse).
//
// Mirrors Blink's LayoutSVGResourceClipper::UpdateSVGLayout: parse
// attributes, populate child list.
func NewSVGResourceClipper(elt ElementAdapter, parentCtx SVGLengthContext, styleResolver StyleResolver) *SVGResourceClipper {
	if elt == nil {
		return nil
	}
	c := &SVGResourceClipper{
		ClipPathUnits: SVGUnitUserSpaceOnUse, // SVG 1.1 §14.3.5 default
		Transform:     geometry.Identity(),
	}
	if id, ok := elt.Attribute("id"); ok {
		c.ResourceID = strings.TrimSpace(id)
	}
	// clipPathUnits arrives in either spelling — HTML tokenizer
	// lowercases SVG attribute names, but the canonical SVG form is
	// camelCase and some adapters may preserve it.
	if v, ok := elt.Attribute("clipPathUnits"); ok {
		c.ClipPathUnits = ParseGradientUnits(v, SVGUnitUserSpaceOnUse)
	} else if v, ok := elt.Attribute("clippathunits"); ok {
		c.ClipPathUnits = ParseGradientUnits(v, SVGUnitUserSpaceOnUse)
	}
	if v, ok := elt.Attribute("transform"); ok {
		c.Transform = ParseSVGTransform(v)
	}
	if styleResolver != nil {
		c.elementStyle = styleResolver(elt)
	}
	// Build child subtree against the same length context the clipper
	// was declared in. Resource elements inside the clipper (e.g.
	// a nested gradient — uncommon but legal) are not registered into
	// the outer registry here; the resource walk happens at the
	// SVGRoot level via buildSVGTreeWithResources. Phase 5 keeps
	// children plain shapes/containers — using BuildSVGTree (not the
	// resource variant) is the right call since clipPath content is
	// rendering geometry, not new resource declarations.
	for _, child := range elt.SVGChildren() {
		if node := BuildSVGTree(child, parentCtx, styleResolver); node != nil {
			c.Children = append(c.Children, node)
		}
	}
	return c
}

// clonePath returns a deep copy of `src` so callers can hand it to
// downstream mutators (e.g. the painter that builds it onto the DC)
// without aliasing the cached resource path.
func clonePath(src *Path) Path {
	if src == nil || len(src.Segments) == 0 {
		return Path{}
	}
	dst := Path{Segments: make([]PathSegment, len(src.Segments))}
	copy(dst.Segments, src.Segments)
	return dst
}

// transformPath returns a copy of `src` with every coord mapped
// through `t`. Used by AsPath to bake the units-mode mapping (and the
// `<clipPath>` element's own transform) into the path returned to the
// caller.
func transformPath(src *Path, t geometry.AffineTransform) Path {
	if src == nil || len(src.Segments) == 0 {
		return Path{}
	}
	dst := Path{Segments: make([]PathSegment, 0, len(src.Segments))}
	mapPt := func(p geometry.PointF) geometry.PointF {
		x := t.A*p.X + t.C*p.Y + t.E
		y := t.B*p.X + t.D*p.Y + t.F
		return geometry.PointF{X: x, Y: y}
	}
	for _, seg := range src.Segments {
		ns := seg
		switch seg.Kind {
		case SegMove, SegLine:
			ns.End = mapPt(seg.End)
		case SegQuad:
			ns.C1 = mapPt(seg.C1)
			ns.End = mapPt(seg.End)
		case SegCubic:
			ns.C1 = mapPt(seg.C1)
			ns.C2 = mapPt(seg.C2)
			ns.End = mapPt(seg.End)
		case SegClose:
			// no coords
		}
		dst.Segments = append(dst.Segments, ns)
	}
	return dst
}
