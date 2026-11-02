// Package viz holds ports of Chromium's display compositor
// (components/viz/service/display) used to draw 3D rendering contexts.
package viz

import (
	"louis14/pkg/cc"
	"louis14/pkg/geometry"
)

// splitThreshold is how "thick" a plane is: a point within it is on the
// plane for splitting (draw_polygon.cc:22 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
const splitThreshold = 0.05

// DrawPolygon is a convex planar polygon in target space with its normal,
// the quad it was made from, and its draw order. Mirrors viz::DrawPolygon
// (components/viz/service/display/draw_polygon.{h,cc} @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type DrawPolygon struct {
	points []geometry.Point3F
	// normal is normalized; distance and coplanarity tests need that.
	normal geometry.Vector3dF
	// orderIndex is the document order, the tie-break for coplanar
	// surfaces.
	orderIndex int
	// originalRef is the quad this polygon draws (a DrawQuad in viz).
	originalRef any
	isSplit     bool
}

// NewDrawPolygon makes a split polygon from points that already lie in
// target space, keeping normal (the DrawPolygon(original, points, normal,
// index) constructor, draw_polygon.cc:40-56).
func NewDrawPolygon(originalRef any, points []geometry.Point3F, normal geometry.Vector3dF, orderIndex int) *DrawPolygon {
	return &DrawPolygon{
		points:      append([]geometry.Point3F(nil), points...),
		normal:      normal,
		orderIndex:  orderIndex,
		originalRef: originalRef,
		isSplit:     true,
	}
}

// NewDrawPolygonFromRect maps visibleLayerRect through transform into
// target space, clipped against w = 0 (MathUtil::MapClippedQuad3d), and
// computes its normal (the DrawPolygon(original, rect, transform, index)
// constructor, draw_polygon.cc:61-84).
func NewDrawPolygonFromRect(originalRef any, visibleLayerRect geometry.RectF, transform geometry.Transform, orderIndex int) *DrawPolygon {
	p := &DrawPolygon{orderIndex: orderIndex, originalRef: originalRef}
	p.points, _ = cc.MapClippedQuad3d(transform, geometry.QuadFFromRect(visibleLayerRect))
	p.normal = transform.MapVector(geometry.Vector3dF{Z: 1})
	p.ConstructNormal()
	return p
}

// Points returns the polygon's vertices.
func (p *DrawPolygon) Points() []geometry.Point3F { return p.points }

// Normal returns the polygon's unit normal.
func (p *DrawPolygon) Normal() geometry.Vector3dF { return p.normal }

// OriginalRef returns the quad the polygon draws.
func (p *DrawPolygon) OriginalRef() any { return p.originalRef }

// OrderIndex returns the polygon's draw order.
func (p *DrawPolygon) OrderIndex() int { return p.orderIndex }

// IsSplit reports whether the polygon is a piece of a split.
func (p *DrawPolygon) IsSplit() bool { return p.isSplit }

// ConstructNormal recomputes the normal from a few near-diagonal cross
// products, keeping the sense of the current normal so winding-reversing
// transforms are handled (DrawPolygon::ConstructNormal,
// draw_polygon.cc:108-130).
func (p *DrawPolygon) ConstructNormal() {
	var n geometry.Vector3dF
	delta := len(p.points) / 2
	for i := 1; i+delta < len(p.points); i++ {
		n = n.Add(geometry.CrossProduct(p.points[i].Sub(p.points[0]), p.points[i+delta].Sub(p.points[0])))
	}
	magnitude := n.Length()
	if geometry.DotProduct(p.normal, n) < 0 {
		magnitude = -magnitude
	}
	if magnitude != 0 && magnitude != 1 {
		n.Scale(1 / magnitude)
	}
	p.normal = n
}

// SignedPointDistance mirrors DrawPolygon::SignedPointDistance: the
// distance of point from the polygon's plane along its normal.
func (p *DrawPolygon) SignedPointDistance(point geometry.Point3F) float64 {
	return geometry.DotProduct(point.Sub(p.points[0]), p.normal)
}

// applyTransform mirrors DrawPolygon::ApplyTransform.
func (p *DrawPolygon) applyTransform(t geometry.Transform) {
	for i, pt := range p.points {
		p.points[i] = t.MapPoint3F(pt)
	}
}

// TransformToScreenSpace moves the polygon and its normal from layer space
// into 3D target space (DrawPolygon::TransformToScreenSpace).
func (p *DrawPolygon) TransformToScreenSpace(t geometry.Transform) {
	p.applyTransform(t)
	p.normal = t.MapVector(p.normal)
	p.ConstructNormal()
}

// TransformToLayerSpace moves the polygon back into layer space with the
// inverse of its quad's transform; the normal resets to the layer's
// (DrawPolygon::TransformToLayerSpace).
func (p *DrawPolygon) TransformToLayerSpace(inverse geometry.Transform) {
	p.applyTransform(inverse)
	p.normal = geometry.Vector3dF{Z: -1}
}

// SplitPolygon splits polygon by p's plane into front and back pieces. A
// polygon that does not cross the plane goes whole to front or back; a
// coplanar one (isCoplanar) goes to front when it is later in document
// order than p, else to back. Mirrors DrawPolygon::SplitPolygon
// (draw_polygon.cc:177-314 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (p *DrawPolygon) SplitPolygon(polygon *DrawPolygon) (front, back *DrawPolygon, isCoplanar bool) {
	n := len(polygon.points)
	next := func(i int) int { return (i + 1) % n }
	prev := func(i int) int { return (i + n - 1) % n }

	// Plane distances of the vertices, snapped to 0 within the thick plane.
	distance := make([]float64, n)
	posCount, negCount := 0, 0
	for i, pt := range polygon.points {
		d := p.SignedPointDistance(pt)
		switch {
		case d < -splitThreshold:
			negCount++
		case d > splitThreshold:
			posCount++
		default:
			d = 0
		}
		distance[i] = d
	}

	switch {
	case posCount == 0 && negCount == 0:
		if polygon.orderIndex >= p.orderIndex {
			return polygon, nil, true
		}
		return nil, polygon, true
	case negCount == 0:
		return polygon, nil, false
	case posCount == 0:
		return nil, polygon, false
	}

	// The first vertex of the front piece, and of the back piece.
	frontBegin := 0
	for distance[frontBegin] <= 0 {
		frontBegin++
	}
	preFrontBegin := prev(frontBegin)
	for distance[preFrontBegin] > 0 {
		frontBegin = preFrontBegin
		preFrontBegin = prev(frontBegin)
	}
	backBegin := 0
	for distance[backBegin] >= 0 {
		backBegin++
	}
	preBackBegin := prev(backBegin)
	for distance[preBackBegin] < 0 {
		backBegin = preBackBegin
		preBackBegin = prev(backBegin)
	}

	// The intersections use the vertex distances, not fresh dot products:
	// they are what separated front from back, so the denominator is
	// nonzero. A "pre" vertex on the thick plane is its own intersection.
	prePosIntersection := pointInterpolate(polygon.points[preFrontBegin], polygon.points[frontBegin],
		floatRatio(distance[preFrontBegin], distance[frontBegin]))
	preNegIntersection := pointInterpolate(polygon.points[preBackBegin], polygon.points[backBegin],
		floatRatio(distance[preBackBegin], distance[backBegin]))

	out := make([]geometry.Point3F, 0, n+2)
	out = append(out, prePosIntersection)
	for i := frontBegin; i != backBegin; i = next(i) {
		out = append(out, polygon.points[i])
	}
	if out[len(out)-1] != preNegIntersection {
		out = append(out, preNegIntersection)
	}
	front = polygon.splitPiece(out)

	out = make([]geometry.Point3F, 0, n+2)
	out = append(out, preNegIntersection)
	for i := backBegin; i != frontBegin; i = next(i) {
		out = append(out, polygon.points[i])
	}
	if out[len(out)-1] != prePosIntersection {
		out = append(out, prePosIntersection)
	}
	back = polygon.splitPiece(out)
	return front, back, false
}

// floatRatio is pre / (pre - cur) over SplitPolygon's float distances.
func floatRatio(pre, cur float64) float64 {
	return float64(float32(pre) / (float32(pre) - float32(cur)))
}

// splitPiece makes a split polygon of p from points, which it takes.
func (p *DrawPolygon) splitPiece(points []geometry.Point3F) *DrawPolygon {
	return &DrawPolygon{points: points, normal: p.normal, orderIndex: p.orderIndex, originalRef: p.originalRef, isSplit: true}
}

// pointInterpolate mirrors PointInterpolate (draw_polygon.cc:26-33): float
// coordinates, a double delta, stored back as float.
func pointInterpolate(from, to geometry.Point3F, delta float64) geometry.Point3F {
	lerp := func(a, b float64) float64 {
		fa := float32(a)
		return float64(float32(float64(fa) + float64(float64(float32(b)-fa)*delta)))
	}
	return geometry.Point3F{X: lerp(from.X, to.X), Y: lerp(from.Y, to.Y), Z: lerp(from.Z, to.Z)}
}

// ToQuads2D fans the polygon into 2D quads from its first vertex, the last
// one a degenerate quad for a leftover triangle (DrawPolygon::ToQuads2D,
// draw_polygon.cc:327-352).
func (p *DrawPolygon) ToQuads2D() []geometry.QuadF {
	if len(p.points) <= 2 {
		return nil
	}
	pt := func(i int) geometry.PointF { return geometry.PointF{X: p.points[i].X, Y: p.points[i].Y} }
	var quads []geometry.QuadF
	for offset := 1; offset < len(p.points)-1; {
		op1, op2 := offset+1, offset+2
		if op2 >= len(p.points) {
			op2 = op1 // a degenerate quad: a triangle
		}
		quads = append(quads, geometry.QuadF{P1: pt(0), P2: pt(offset), P3: pt(op1), P4: pt(op2)})
		offset = op2
	}
	return quads
}
