package render

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout"
)

// TransformPaintPropertyNode is a box's node in the transform tree: its
// transform (with transform-origin, and its parent's perspective) in
// absolute paint coordinates, whether it flattens the transform it inherits,
// and the 3D rendering context it belongs to. Mirrors the subset of
// blink::TransformPaintPropertyNode (platform/graphics/paint/
// transform_paint_property_node.h) that FragmentPaintPropertyTreeBuilder
// fills in UpdateTransform / UpdateIndividualTransform / UpdatePerspective
// (core/paint/paint_property_tree_builder.cc:1388-1651, :3153-3216 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14 folds the parent's
// perspective node into the child's Matrix: the perspective node's
// children never flatten it (:3180).
type TransformPaintPropertyNode struct {
	Box *layout.Box
	// Matrix maps the box's local coordinates to its parent node's space:
	// perspective(parent) · T(origin) · ComputedStyle::ApplyTransform ·
	// T(-origin), the origin on the pixel-snapped border box.
	Matrix geometry.Transform
	// Parent is the nearest ancestor node the box's transform composes
	// with, nil when the nearest relevant ancestor has no transform node.
	Parent *TransformPaintPropertyNode
	// FlattensInheritedTransform mirrors the Blink field: true unless the
	// parent node preserves 3D (UpdateTransform sets
	// should_flatten_inherited_transform = !Preserves3D, :1634-1651).
	FlattensInheritedTransform bool
	// RenderingContextRoot is the box that roots the 3D rendering context
	// this node belongs to (Blink's rendering_context_id, :1458-1466), nil
	// outside any context.
	RenderingContextRoot *layout.Box

	// projection caches projectionToRenderingContextBase (GeometryMapper's
	// TransformCache role).
	projection *geometry.Transform
}

// needsTransformNode mirrors NeedsTransform (paint_property_tree_builder.cc:
// 1330-1350 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for a transformable
// box: a transform (or translate/rotate/scale, whose nodes louis14 folds into
// the transform node), a used transform-style of preserve-3d, or a hidden
// backface.
func needsTransformNode(box *layout.Box) bool {
	if box == nil || box.Style == nil || box.Text != "" || !layout.IsTransformableBox(box.Style, box.Node) {
		return false
	}
	return box.Style.HasTransform() || box.Preserves3D() ||
		box.Style.GetBackfaceVisibility() == css.BackfaceVisibilityHidden
}

// perspectiveLookup returns the perspective matrix the DOM parent of box
// applies to box's transform, if any.
type perspectiveLookup func(box *layout.Box) (geometry.Transform, bool)

// transformTreeBuilder builds and caches TransformPaintPropertyNodes for one
// paint (the FragmentPaintPropertyTreeBuilder walk, done lazily per box).
type transformTreeBuilder struct {
	nodes       map[*layout.Box]*TransformPaintPropertyNode
	perspective perspectiveLookup
	// domParent returns the box of a box's DOM parent: the layout tree
	// parents an absolutely or fixed positioned box under its containing
	// block, while Blink's tree walk (and so the 3D rendering context)
	// follows the layout object's parent, its DOM parent. nil uses the box
	// tree parent.
	domParent func(*layout.Box) *layout.Box
}

func newTransformTreeBuilder(perspective perspectiveLookup, domParent func(*layout.Box) *layout.Box) *transformTreeBuilder {
	return &transformTreeBuilder{nodes: make(map[*layout.Box]*TransformPaintPropertyNode), perspective: perspective, domParent: domParent}
}

// transformContextParent returns the ancestor whose transform state box
// inherits: the nearest styled ancestor, skipping anonymous boxes without a
// transform node (a 3D rendering context follows the DOM ancestor chain and
// Blink resets flattening only for non-anonymous objects, :1646-1651), and
// nil past a block-in-inline wrapper (its layout parent is an inline, which
// is not transformable, so the context does not reach past it).
func (b *transformTreeBuilder) transformContextParent(box *layout.Box) *layout.Box {
	start := box.Parent
	if b.domParent != nil && (box.Position == css.PositionAbsolute || box.Position == css.PositionFixed) {
		if p := b.domParent(box); p != nil {
			start = p
		}
	}
	for a := start; a != nil; a = a.Parent {
		if a.Style == nil {
			continue
		}
		if a.IsBlockInInline {
			return nil
		}
		if a.Node == nil && !needsTransformNode(a) {
			continue
		}
		return a
	}
	return nil
}

// node returns box's TransformPaintPropertyNode, nil when it needs none.
func (b *transformTreeBuilder) node(box *layout.Box) *TransformPaintPropertyNode {
	if n, ok := b.nodes[box]; ok {
		return n
	}
	var n *TransformPaintPropertyNode
	if needsTransformNode(box) {
		n = &TransformPaintPropertyNode{Box: box, FlattensInheritedTransform: true}
		if parent := b.transformContextParent(box); parent != nil {
			if pn := b.node(parent); pn != nil {
				n.Parent = pn
				if parent.Preserves3D() {
					n.FlattensInheritedTransform = false
					n.RenderingContextRoot = pn.RenderingContextRoot
				}
			}
		}
		// A preserve-3d node outside any context establishes one
		// (new_rendering_context, :1460-1466).
		if n.RenderingContextRoot == nil && box.Preserves3D() {
			n.RenderingContextRoot = box
		}
		n.Matrix = localTransformMatrix(box)
		if b.perspective != nil {
			if p, ok := b.perspective(box); ok {
				n.Matrix = p.Mul(n.Matrix)
			}
		}
	}
	b.nodes[box] = n
	return n
}

// localTransformMatrix returns T(origin) · ApplyTransform · T(-origin) for
// box in absolute coordinates, the origin resolved on the pixel-snapped
// border box (TransformPaintPropertyNode::Build adds the origin to the
// snapped border box). Identity when the box has no transform.
func localTransformMatrix(box *layout.Box) geometry.Transform {
	var m geometry.Transform
	s := box.Style
	if !s.HasTransform() {
		return m
	}
	sx, sy, _, _ := pixelSnap(box.X, box.Y, box.Width, box.Height)
	s.ApplyTransform(&m, geometry.NewRectF(0, 0, box.Width, box.Height),
		css.IncludeTransformOperations, css.ExcludeTransformOrigin, css.IncludeIndependentTransformProperties)
	origin := s.GetTransformOrigin()
	m.ApplyTransformOrigin(sx+origin.X.ValueForLength(box.Width), sy+origin.Y.ValueForLength(box.Height), origin.Z)
	return m
}

// participatesInRenderingContext reports whether n shares its parent's 3D
// rendering context (its inherited transform is not flattened).
func (n *TransformPaintPropertyNode) participatesInRenderingContext() bool {
	return n != nil && !n.FlattensInheritedTransform && n.RenderingContextRoot != nil
}

// projectionToRenderingContextBase returns n's transform from its local
// space to the space its 3D rendering context's root is painted into: the
// product of the node matrices from the context root down to n, unflattened
// in between. Mirrors GeometryMapper::SourceToDestinationProjection
// (platform/graphics/paint/geometry_mapper.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) with the destination the root's
// parent space, where nothing on the path flattens. Outside a context it is
// n.Matrix.
func projectionToRenderingContextBase(n *TransformPaintPropertyNode) geometry.Transform {
	if n.projection == nil {
		m := n.Matrix
		if n.participatesInRenderingContext() && n.Parent != nil {
			m = projectionToRenderingContextBase(n.Parent).Mul(m)
		}
		n.projection = &m
	}
	return *n.projection
}

// projectiveHomography returns the 3×3 (row-major) homography of m on the
// z=0 plane: rows 0, 1 and 3, columns 0, 1 and 3.
func projectiveHomography(m geometry.Transform) [9]float64 {
	return [9]float64{
		m.Rc(0, 0), m.Rc(0, 1), m.Rc(0, 3),
		m.Rc(1, 0), m.Rc(1, 1), m.Rc(1, 3),
		m.Rc(3, 0), m.Rc(3, 1), m.Rc(3, 3),
	}
}

// contextScope3D is a preserve-3d layer being painted inside a 3D
// rendering context: the projection it applied relative to the context
// base, and the participants deferred until its paint returns (when that
// projection is singular).
type contextScope3D struct {
	root     *layout.Box
	applied  [9]float64
	deferred []*PaintLayer
}

// identityHomography is the 3×3 identity, row-major.
var identityHomography = [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}

// invertHomography inverts a 3×3 row-major homography, reporting false when
// it is singular (singularProjectionEpsilon on its determinant).
func invertHomography(h [9]float64) ([9]float64, bool) {
	a, b, c := h[0], h[1], h[2]
	d, e, f := h[3], h[4], h[5]
	g, i, k := h[6], h[7], h[8]
	co0 := e*k - f*i
	co1 := f*g - d*k
	co2 := d*i - e*g
	det := a*co0 + b*co1 + c*co2
	if math.Abs(det) < singularProjectionEpsilon {
		return [9]float64{}, false
	}
	inv := 1 / det
	return [9]float64{
		co0 * inv, (c*i - b*k) * inv, (b*f - c*e) * inv,
		co1 * inv, (a*k - c*g) * inv, (c*d - a*f) * inv,
		co2 * inv, (b*g - a*i) * inv, (a*e - b*d) * inv,
	}, true
}

// multiplyHomographies returns a·b for 3×3 row-major homographies.
func multiplyHomographies(a, b [9]float64) [9]float64 {
	var r [9]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			r[i*3+j] = a[i*3]*b[j] + a[i*3+1]*b[3+j] + a[i*3+2]*b[6+j]
		}
	}
	return r
}
