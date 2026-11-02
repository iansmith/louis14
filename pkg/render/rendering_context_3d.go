package render

import (
	"math"

	"louis14/pkg/geometry"
	"louis14/pkg/layout"
	"louis14/pkg/viz"
)

// contextUnit is one quad of a 3D rendering context: a self-painting layer
// (or part of one), the rect its polygon is made from, and the projections
// to the context base of its quad and of the plane it is drawn under. A
// layer that preserves 3D in the context (the root or a preserve-3d
// participant) is two units, its background and the rest of its own
// content, and applies its own projection; any other layer paints whole,
// flattened into the plane of its 3D scene leaf — the ancestor-or-self
// element whose parent preserves 3D while it does not.
type contextUnit struct {
	layer      *PaintLayer
	part       memberPart
	bounds     geometry.RectF
	projection geometry.Transform
	plane      *geometry.Transform // nil for a member, which applies its own
}

// memberPart is the part of a 3D-preserving layer's own content a unit
// draws: its background (painted before its negative z-index children) or
// the rest. Any other unit draws its layer whole.
type memberPart int

const (
	wholeLayer memberPart = iota
	memberBackground
	memberContent
)

// renderingContext3D is the 3D rendering context being drawn: the layers
// its units draw, each painted only by its unit, and the unit being drawn.
type renderingContext3D struct {
	units   map[*PaintLayer]bool
	drawing contextUnit
}

// partOf returns the part of layer's own content to paint: all of it
// unless layer is the member being drawn as one of its parts.
func (c *renderingContext3D) partOf(layer *PaintLayer) memberPart {
	if c == nil || layer != c.drawing.layer {
		return wholeLayer
	}
	return c.drawing.part
}

// paintsFromContextBase reports whether layer is the participant of a 3D
// rendering context being drawn from the context base, applying its whole
// projection to the base (applyTransforms).
func (r *Renderer) paintsFromContextBase(layer *PaintLayer) bool {
	c := r.context3D
	return c != nil && layer == c.drawing.layer && c.drawing.plane == nil &&
		r.transformTree.node(layer.Box).participatesInRenderingContext()
}

// paintRenderingContext3D paints the 3D rendering context rooted at root:
// every unit becomes a polygon in the context base, and the polygons are
// depth-sorted and split by a BspTree and drawn back to front (draw order
// breaks ties between coplanar ones). Mirrors how Blink and viz draw a
// sorting context: a 3D scene leaf gets an effect and its own render pass
// (CompositingReasonsFor3DSceneLeaf, compositing_reason_finder.cc:179-212),
// so every quad in the context carries its sorting_context_id; those quads
// become DrawPolygons in target space, polygons without a normal are
// dropped, and FlushPolygons walks a BspTree with BspWalkActionDrawPolygon
// (DirectRenderer::DrawRenderPassAndExecuteCopyRequests and FlushPolygons,
// components/viz/service/display/direct_renderer.cc:806-847, :591-603 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The target space is the
// root's parent space, where the draw context is on entry.
func (r *Renderer) paintRenderingContext3D(root *PaintLayer, node *TransformPaintPropertyNode) {
	units := r.collectContextUnits(root, node)
	ctx := &renderingContext3D{units: make(map[*PaintLayer]bool, len(units))}
	polygons := make([]*viz.DrawPolygon, 0, len(units))
	for i, u := range units {
		ctx.units[u.layer] = true
		p := viz.NewDrawPolygonFromRect(u, u.bounds, u.projection, i)
		// A polygon entirely behind the camera clips to fewer than three
		// vertices, and a degenerate one has no normal; neither draws, and
		// the split math needs a vertex and a unit normal. An edge-on one
		// covers no area on screen: cc culls its layer, whose visible rect
		// is empty, so it makes no quad and splits nothing.
		if len(p.Points()) >= 3 && p.Normal().LengthSquared() > 0 && !zeroScreenArea(p) {
			polygons = append(polygons, p)
		}
	}
	outer := r.context3D
	r.context3D = ctx
	viz.NewBspTree(polygons).TraverseWithActionHandler(r.drawPolygon)
	r.context3D = outer
}

// collectContextUnits returns the units of the context rooted at root in
// paint order: a layer preserving 3D in the context paints its background,
// its negative z-index children, the rest of its own content and then its
// other layers; a non-member layer paints its whole subtree. A leaf without
// a self-painting layer paints its own content in its parent's unit.
func (r *Renderer) collectContextUnits(root *PaintLayer, node *TransformPaintPropertyNode) []contextUnit {
	var units []contextUnit
	var walk func(layer *PaintLayer)
	var visit func(c *PaintLayer, selfPainting bool)
	member := func(l *PaintLayer, n *TransformPaintPropertyNode) {
		u := contextUnit{layer: l, part: memberBackground, bounds: boxPaintBounds(l.Box), projection: projectionToRenderingContextBase(n)}
		units = append(units, u)
		for _, c := range l.NegativeZ {
			visit(c, true)
		}
		u.part = memberContent
		units = append(units, u)
		walk(l)
	}
	visit = func(c *PaintLayer, selfPainting bool) {
		if !selfPainting || c.Box == nil {
			walk(c)
			return
		}
		if n := r.transformTree.node(c.Box); n.participatesInRenderingContext() && c.Box.Preserves3D() {
			member(c, n)
			return
		}
		units = append(units, r.leafUnit(c, root.Box))
	}
	walk = func(layer *PaintLayer) {
		for _, c := range layer.FlowChildren {
			visit(c, isSelfPaintingFlowChild(c))
		}
		for _, list := range [][]*PaintLayer{layer.FloatChildren, layer.AutoZero, layer.PositiveZ} {
			for _, c := range list {
				visit(c, true)
			}
		}
	}
	member(root, node)
	return units
}

// leafUnit makes the unit of a non-member layer: drawn under the plane of
// its 3D scene leaf, its quad that plane with the layer's own transform
// flattened into it when the layer is not the leaf.
func (r *Renderer) leafUnit(l *PaintLayer, root *layout.Box) contextUnit {
	plane := projectionToRenderingContextBase(r.transformTree.localBorderBoxTransform(r.sceneLeaf(l.Box, root)))
	u := contextUnit{layer: l, bounds: boxPaintBounds(l.Box), projection: plane, plane: &plane}
	switch n := r.transformTree.node(l.Box); {
	case n.participatesInRenderingContext():
		// The leaf itself: plane is its own node's projection, which it
		// applies when drawn.
		u.plane = nil
	case n != nil:
		own := n.Matrix
		own.Flatten()
		u.projection = plane.Mul(own)
	}
	return u
}

// sceneLeaf returns the 3D scene leaf box belongs to: the ancestor-or-self
// whose parent element preserves 3D in the context rooted at root
// (CompositingReasonsFor3DSceneLeaf's NearestAncestorForElement test). It
// follows transformContextParent, the DOM-parent chain the 3D context
// follows, and returns box itself when no leaf is found.
func (r *Renderer) sceneLeaf(box, root *layout.Box) *layout.Box {
	for a := box; a != nil; {
		p := r.transformTree.transformContextParent(a)
		if p == nil {
			break
		}
		if n := r.transformTree.node(p); n != nil && p.Preserves3D() && (p == root || n.RenderingContextRoot == root) {
			return a
		}
		a = p
	}
	return box
}

// isSelfPaintingFlowChild reports whether paintDescendantPhase paints the
// flow child c through paintLayer.
func isSelfPaintingFlowChild(c *PaintLayer) bool {
	return (c.Box != nil && c.Box.CreatesStackingContext()) || isAtomicInlineForPaint(c) || isOutOfFlowForPaint(c)
}

// zeroScreenArea reports whether p projects to no area in the target plane
// (its vertices are perspective-divided, so x and y are screen positions).
func zeroScreenArea(p *viz.DrawPolygon) bool {
	pts := p.Points()
	area := 0.0
	for i, a := range pts {
		b := pts[(i+1)%len(pts)]
		area += a.X*b.Y - b.X*a.Y
	}
	return math.Abs(area) < 1e-6
}

// boxPaintBounds is the rect in a unit's local (absolute) coordinates its
// polygon is made from, the quad's visible rect in viz: the border boxes of
// box and its descendants.
func boxPaintBounds(box *layout.Box) geometry.RectF {
	b := subtreeBorderBoxBounds(box)
	return rectFFromInts(b.Min.X, b.Min.Y, b.Dx(), b.Dy())
}

// drawPolygon draws one sorted polygon: its unit drawn from the context
// base, clipped to the polygon when it is a piece of a split. Mirrors
// BspWalkActionDrawPolygon and DirectRenderer::DoDrawPolygon
// (bsp_walk_action.cc:22-38, direct_renderer.cc:566-583): an unsplit
// polygon draws its whole quad; a split one draws the quad restricted to
// the piece, which louis14 expresses as a clip to the piece's outline in
// the target space (its vertices are already perspective-divided there).
// A member applies its own projection to the base (paintsFromContextBase);
// any other unit is drawn under its plane's.
func (r *Renderer) drawPolygon(p *viz.DrawPolygon) {
	r.dc.Push()
	defer r.dc.Pop()
	if p.IsSplit() {
		r.clipToPolygon(p.Points())
	}
	u := p.OriginalRef().(contextUnit)
	if u.plane != nil {
		r.multiplyHomography(projectiveHomography(*u.plane))
	}
	outer := r.context3D.drawing
	r.context3D.drawing = u
	r.paintLayerContents(u.layer)
	r.context3D.drawing = outer
}

// clipToPolygon clips the draw context to a split piece's outline. An
// axis-aligned rectangular piece is pixel-snapped, as louis14 snaps every
// rectangular clip, so a pixel whose center it covers is drawn (the draw
// context's rectangle clip would otherwise truncate a fractional edge).
func (r *Renderer) clipToPolygon(points []geometry.Point3F) {
	if x0, y0, x1, y1, ok := axisAlignedRect(points); ok {
		r.dc.DrawRectangle(pixelSnap(x0, y0, x1-x0, y1-y0))
	} else {
		outline := make([]geometry.PointF, len(points))
		for i, pt := range points {
			outline[i] = geometry.PointF{X: pt.X, Y: pt.Y}
		}
		addPolygonPath(r.dc, outline)
	}
	r.dc.Clip()
}

// axisAlignedRect returns the bounds of points when they outline an
// axis-aligned rectangle in x and y.
func axisAlignedRect(points []geometry.Point3F) (x0, y0, x1, y1 float64, ok bool) {
	if len(points) != 4 {
		return 0, 0, 0, 0, false
	}
	x0, y0, x1, y1 = points[0].X, points[0].Y, points[0].X, points[0].Y
	for _, p := range points[1:] {
		x0, y0, x1, y1 = math.Min(x0, p.X), math.Min(y0, p.Y), math.Max(x1, p.X), math.Max(y1, p.Y)
	}
	for _, p := range points {
		if (p.X != x0 && p.X != x1) || (p.Y != y0 && p.Y != y1) {
			return 0, 0, 0, 0, false
		}
	}
	return x0, y0, x1, y1, true
}
