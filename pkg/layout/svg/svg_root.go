package svg

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// ElementAdapter is the abstract DOM-element view consumed by the SVG
// tree builder. It exists so pkg/layout/svg avoids importing pkg/layout
// (which would create an import cycle: layout → layout/svg → layout).
// pkg/layout wraps its LayoutInputNode in a tiny adapter that implements
// this interface.
//
// Phase 1 surface is minimal: tag name, attribute lookup, and SVG-child
// iteration. The adapter only needs to expose SVG children (not arbitrary
// LayoutInputNodes) because text nodes and anonymous boxes have no
// meaning in SVG layout.
type ElementAdapter interface {
	// TagName returns the SVG element tag in lowercase (HTML tokenizer
	// canonical form), e.g. "svg", "g", "rect".
	TagName() string

	// Attribute returns the named attribute value and whether it was set.
	// HTML's tokenizer lowercases attribute names, so SVG's camelCase
	// names (viewBox, preserveAspectRatio) are stored lowercased.
	// Callers should try both forms when the camelCase spelling matters.
	Attribute(name string) (string, bool)

	// SVGChildren returns the element's children as ElementAdapters in
	// DOM order, skipping text nodes and other non-element content.
	SVGChildren() []ElementAdapter
}

// SVGRoot is the root of an inline <svg>'s SVG layout subtree. It owns
// the SVG coordinate system: the parsed viewBox, the preserveAspectRatio
// directive, the resolved local-to-border-box transform, and the SVG
// child subtree. Mirrors Blink's LayoutSVGRoot
// (core/layout/svg/layout_svg_root.{h,cc}).
//
// SVGRoot is NOT a LayoutInputNode and NOT in the CSS box tree — its
// outer presence in HTML flow is the <svg> replaced-element box, sized
// the same way as before (via getInlineSVGIntrinsicInfo and
// ComputeReplacedSize). SVGRoot only governs what is inside that box.
type SVGRoot struct {
	// ContainerSize is the SVG content-box physical size (CSS px). This
	// is the viewport the SVG content is rendered into. Equal to the
	// <svg> box's CSS content-box size (post border/padding subtraction).
	// Mirrors Blink's LayoutSVGRoot::container_size_.
	ContainerSize geometry.SizeF

	// ViewBox is the parsed `viewBox` attribute. Invalid/missing leaves
	// Valid=false; the transform below is then the identity (user units
	// == CSS px).
	ViewBox ViewBox

	// PreserveAspectRatio is the parsed `preserveAspectRatio` attribute.
	// Defaults to xMidYMid meet per SVG 2 §8.2.4.
	PreserveAspectRatio PreserveAspectRatio

	// LocalToBorderBoxTransform maps a point in viewBox/user-units space
	// to a point in the <svg> content-box's CSS px coordinate system.
	// Built by BuildViewBoxToViewportTransform from the parsed viewBox
	// and preserveAspectRatio against ContainerSize. Mirrors Blink's
	// LayoutSVGRoot::local_to_border_box_transform_.
	LocalToBorderBoxTransform geometry.AffineTransform

	// Children are the structural SVG children of the root in DOM order
	// (each one already routed through the BuildSVGTree dispatch — Phase
	// 1: SVGContainer stubs for everything that isn't <svg> itself).
	Children []SVGNode

	// bbox accumulated by UpdateSVGLayout. Phase 1: always empty.
	bbox geometry.RectF
}

// ObjectBoundingBox returns the union of children's bounding boxes (in
// viewBox / user-unit coordinates). Phase 1: empty.
func (r *SVGRoot) ObjectBoundingBox() geometry.RectF { return r.bbox }

// LocalTransform returns the viewBox→viewport mapping. Phase 1: this is
// the only non-identity transform any node may carry; descendant
// containers/shapes return identity until Phase 3.
func (r *SVGRoot) LocalTransform() geometry.AffineTransform {
	return r.LocalToBorderBoxTransform
}

// UpdateSVGLayout runs the SVG layout pass over the subtree. Phase 1:
// recursive no-op — every child is a stub. Mirrors Blink's
// LayoutSVGRoot::LayoutRoot but with no real geometry yet.
func (r *SVGRoot) UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult {
	var combined SVGLayoutResult
	for _, child := range r.Children {
		res := child.UpdateSVGLayout(info)
		if res.BoundsChanged {
			combined.BoundsChanged = true
		}
		if res.HasViewportDependence {
			combined.HasViewportDependence = true
		}
	}
	r.bbox = geometry.RectF{} // Phase 1.
	return combined
}

// Paint is a no-op in Phase 1 — children are all stubs and the painter
// hook isn't wired yet. Mirrors Blink's SVGRootPainter::PaintReplaced
// signature for forward-compat: Phase 2 will apply
// LocalToBorderBoxTransform, set up the viewport clip, and recurse.
func (r *SVGRoot) Paint(ctx *SVGPaintContext) {
	// Phase 1: no-op. SVG content still renders blank.
}

// StyleResolver maps an SVG ElementAdapter to the element's resolved
// computed style. Supplied by the caller (pkg/layout's tree builder
// has the per-DOM-node styles map populated by css.ComputeStyle). May
// return nil for elements that didn't get cascade attention — the
// painter falls back to SVG property defaults in that case.
type StyleResolver func(ElementAdapter) *css.Style

// BuildSVGRoot constructs an SVGRoot from an <svg> ElementAdapter and the
// physical content-box size of its CSS box. Performs the viewBox /
// preserveAspectRatio parse and computes the local-to-border-box
// transform. The SVG subtree is built by BuildSVGTree.
//
// The styleResolver callback is invoked for every element built into
// the subtree; the resolved *css.Style is attached to the SVGShape /
// SVGContainer for the painter to read at paint time. nil is
// tolerated (e.g. for tests that only exercise geometry); SVG
// default values then apply.
//
// Phase 2 adds shape dispatch; Phase 3 will add <g>/nested-<svg>.
func BuildSVGRoot(svgElement ElementAdapter, containerSize geometry.SizeF, styleResolver StyleResolver) *SVGRoot {
	if svgElement == nil {
		return &SVGRoot{
			ContainerSize:             containerSize,
			LocalToBorderBoxTransform: geometry.Identity(),
			PreserveAspectRatio:       DefaultPreserveAspectRatio(),
		}
	}

	root := &SVGRoot{
		ContainerSize:       containerSize,
		PreserveAspectRatio: DefaultPreserveAspectRatio(),
	}

	// Parse viewBox. The HTML tokenizer lowercases attribute names, so
	// SVG's camelCase "viewBox" is stored as "viewbox". Try both for
	// robustness against XHTML / future case-preserving paths.
	vb, ok := svgElement.Attribute("viewBox")
	if !ok {
		vb, ok = svgElement.Attribute("viewbox")
	}
	if ok {
		if x, y, w, h, parsed := ParseViewBox(vb); parsed {
			root.ViewBox = ViewBox{X: x, Y: y, Width: w, Height: h, Valid: true}
		}
	}

	// Parse preserveAspectRatio.
	if par, ok := svgElement.Attribute("preserveAspectRatio"); ok {
		root.PreserveAspectRatio = ParsePreserveAspectRatio(par)
	} else if par, ok := svgElement.Attribute("preserveaspectratio"); ok {
		root.PreserveAspectRatio = ParsePreserveAspectRatio(par)
	}

	// Build the local-to-border-box transform: viewBox → viewport rect.
	// The viewport rect is the SVG content box; its origin is (0, 0) in
	// the <svg> box's content-box coordinate system.
	viewport := geometry.NewRectF(0, 0, containerSize.Width, containerSize.Height)
	root.LocalToBorderBoxTransform = BuildViewBoxToViewportTransform(
		root.ViewBox, viewport, root.PreserveAspectRatio,
	)

	// Build the SVG layout subtree from the <svg>'s children.
	lengthCtx := NewSVGLengthContext(containerSize)
	for _, child := range svgElement.SVGChildren() {
		node := BuildSVGTree(child, lengthCtx, styleResolver)
		if node != nil {
			root.Children = append(root.Children, node)
		}
	}

	return root
}

// BuildSVGTree dispatches an ElementAdapter to the right SVGNode
// constructor based on its tag name. Phase 2 recognizes the seven
// shape tags (<rect>, <circle>, <ellipse>, <line>, <polyline>,
// <polygon>, <path>) and routes them to SVGShape. All other tags fall
// through to SVGContainer, the recursive structural shell from
// Phase 1 — Phase 3 promotes <g> to SVGTransformableContainer and
// nested <svg> to SVGViewportContainer; Phase 6 routes
// <mask>/<clipPath>/<filter>/<linearGradient>/<radialGradient>/<pattern>
// to resource containers.
//
// Mirrors the dispatch in Blink's LayoutTreeBuilder for SVG element
// kinds (core/layout/svg/svg_resources.cc and the per-element
// CreateLayoutObject impl on SVGGeometryElement subclasses).
func BuildSVGTree(elt ElementAdapter, lengthCtx SVGLengthContext, styleResolver StyleResolver) SVGNode {
	if elt == nil {
		return nil
	}
	tag := elt.TagName()
	switch tag {
	case "rect", "circle", "ellipse", "line", "polyline", "polygon", "path":
		// Shape leaves do not have SVG-element children (text content
		// inside a <rect> is ignored per SVG 1.1 §5.6). Skip the
		// recursive child walk.
		shape := NewSVGShape(elt, lengthCtx)
		if shape != nil && styleResolver != nil {
			shape.Style = styleResolver(elt)
		}
		return shape
	case "svg":
		// Nested <svg>: establishes a new viewport + viewBox + length
		// context for its descendants. Mirrors Blink's
		// LayoutSVGViewportContainer.
		vc := NewSVGViewportContainer(elt, lengthCtx)
		if vc == nil {
			return nil
		}
		if styleResolver != nil {
			vc.Style = styleResolver(elt)
		}
		childCtx := vc.ChildLengthContext()
		for _, child := range elt.SVGChildren() {
			if c := BuildSVGTree(child, childCtx, styleResolver); c != nil {
				vc.AppendChild(c)
			}
		}
		return vc
	}
	// Default: a plain SVG container (covers <g>, and falls back to
	// the structural shell for any tag we don't yet recognize). The
	// `transform` attribute is parsed unconditionally — only `<g>`
	// (and a handful of other transformable elements) accept it per
	// SVG 1.1 §7.6, but parsing it on unrecognized tags is harmless
	// (the value is the identity if absent) and keeps the dispatch
	// flat. Mirrors how Blink's LayoutSVGTransformableContainer reads
	// the attribute via SVGTransformList::Parse without filtering by
	// element type.
	container := NewSVGContainer(tag)
	if styleResolver != nil {
		container.Style = styleResolver(elt)
	}
	if v, ok := elt.Attribute("transform"); ok {
		container.SetSVGTransform(ParseSVGTransform(v))
	}
	for _, child := range elt.SVGChildren() {
		if c := BuildSVGTree(child, lengthCtx, styleResolver); c != nil {
			container.AppendChild(c)
		}
	}
	return container
}
