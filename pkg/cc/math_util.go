// Package cc holds ports of Chromium's compositor (cc/) helpers that
// louis14's renderer uses.
package cc

import (
	"math"

	"louis14/pkg/geometry"
)

// kInfiniteCoordinate is HomogeneousCoordinate::kInfiniteCoordinate
// (cc/base/math_util.h:55 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
const kInfiniteCoordinate = 1000000.0

// kStationaryPointEpsilon is the tolerance for an eyepoint-aligned edge
// (math_util.cc:129).
const kStationaryPointEpsilon = 0.00001

// floatEpsilon is std::numeric_limits<float>::epsilon().
const floatEpsilon = 1.1920928955078125e-07

// homogeneousCoordinate mirrors cc::HomogeneousCoordinate
// (math_util.h:48-102): a point in 4D homogeneous space.
type homogeneousCoordinate struct {
	Vec [4]float64
}

func (h homogeneousCoordinate) x() float64 { return h.Vec[0] }
func (h homogeneousCoordinate) y() float64 { return h.Vec[1] }
func (h homogeneousCoordinate) z() float64 { return h.Vec[2] }
func (h homogeneousCoordinate) w() float64 { return h.Vec[3] }

// ShouldBeClipped mirrors homogeneousCoordinate::ShouldBeClipped: the point
// is at or behind the eye (w <= 0).
func (h homogeneousCoordinate) ShouldBeClipped() bool { return h.w() <= 0 }

// CartesianPoint3dUnclamped mirrors
// homogeneousCoordinate::CartesianPoint3dUnclamped.
// It computes in float (SkScalar), like cc.
func (h homogeneousCoordinate) CartesianPoint3dUnclamped() geometry.Point3F {
	x, y, z, w := float32(h.x()), float32(h.y()), float32(h.z()), float32(h.w())
	if w == 1 {
		return geometry.Point3F{X: float64(x), Y: float64(y), Z: float64(z)}
	}
	invW := 1 / w
	return geometry.Point3F{X: float64(x * invW), Y: float64(y * invW), Z: float64(z * invW)}
}

// mapHomogeneousPoint mirrors MapHomogeneousPoint (math_util.cc:67-73).
func mapHomogeneousPoint(t geometry.Transform, p geometry.PointF) homogeneousCoordinate {
	h := homogeneousCoordinate{Vec: [4]float64{p.X, p.Y, 0, 1}}
	t.TransformVector4(&h.Vec)
	return h
}

// homogeneousLimitNearZero mirrors homogeneousLimitNearZero
// (math_util.cc:135-152), in float as cc computes it.
func homogeneousLimitNearZero(a1, w1, a2, w2, t float32) float64 {
	if abs32(float32(a1*w2)/w1/a2-1) > kStationaryPointEpsilon {
		// t has been computed so that w is near but not at zero.
		return float64((float32((1-t)*a1) + float32(t*a2)) / (float32((1-t)*w1) + float32(t*w2)))
	}
	return float64(a1 / w1) // (== a2 / w2)
}

// computeClippedCartesianPoint3dForEdge mirrors
// ComputeClippedCartesianPoint3dForEdge (math_util.cc:154-214): the point
// where edge h1-h2 crosses w = 0, pulled back to the positive-w side far
// enough that its largest coordinate is about kInfiniteCoordinate. It
// computes in float, like cc, which the limit's magnitude depends on.
func computeClippedCartesianPoint3dForEdge(h1, h2 homogeneousCoordinate) geometry.Point3F {
	var a, b [4]float32
	for i := range a {
		a[i], b[i] = float32(h1.Vec[i]), float32(h2.Vec[i])
	}
	wDiff := a[3] - b[3]
	t := a[3] / wDiff
	lerp := func(i int) float32 { return float32((1-t)*a[i]) + float32(t*b[i]) }
	maxNumerator := max(abs32(lerp(0)), abs32(lerp(1)), abs32(lerp(2)))
	tShift := maxNumerator / wDiff / kInfiniteCoordinate
	const halfEpsilon = floatEpsilon / 2
	if wDiff > 0 {
		t = max(0, t-(tShift+halfEpsilon))
	} else {
		t = min(1, t-(tShift-halfEpsilon))
	}
	return geometry.Point3F{
		X: homogeneousLimitNearZero(a[0], a[3], b[0], b[3], t),
		Y: homogeneousLimitNearZero(a[1], a[3], b[1], b[3], t),
		Z: homogeneousLimitNearZero(a[2], a[3], b[2], b[3], t),
	}
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

// isNearlyTheSame mirrors IsNearlyTheSame(float, float)
// (math_util.cc:226-234), in float.
func isNearlyTheSame(f, g float32) bool {
	const epsilonScale = 0.00001
	return abs32(f-g) < float32(epsilonScale*max(abs32(f), abs32(g), epsilonScale))
}

// isNearlyTheSamePoint3F mirrors IsNearlyTheSame(Point3F, Point3F)
// (math_util.cc:241-245).
func isNearlyTheSamePoint3F(a, b geometry.Point3F) bool {
	return isNearlyTheSame(float32(a.X), float32(b.X)) && isNearlyTheSame(float32(a.Y), float32(b.Y)) &&
		isNearlyTheSame(float32(a.Z), float32(b.Z))
}

// clippedQuad3d accumulates the vertices of a clipped quad
// (AddVertexToClippedQuad3d, math_util.cc:248-273).
type clippedQuad3d struct {
	points      []geometry.Point3F
	needToClamp bool
}

func (q *clippedQuad3d) add(v geometry.Point3F) {
	if n := len(q.points); n > 0 && isNearlyTheSamePoint3F(q.points[n-1], v) {
		return
	}
	q.points = append(q.points, v)
	if math.Abs(v.X) > kInfiniteCoordinate || math.Abs(v.Y) > kInfiniteCoordinate || math.Abs(v.Z) > kInfiniteCoordinate {
		q.needToClamp = true
	}
}

// MapClippedQuad3d maps quad through t, clipping it against w = 0, and
// returns the vertices of the clipped polygon (at most six) in 3D, in the
// quad's winding order. ok reports at least four vertices. Mirrors
// MathUtil::MapClippedQuad3d (math_util.cc:380-597 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), including its coplanar clamp
// of coordinates past kInfiniteCoordinate.
func MapClippedQuad3d(t geometry.Transform, quad geometry.QuadF) (points []geometry.Point3F, ok bool) {
	h := [4]homogeneousCoordinate{
		mapHomogeneousPoint(t, quad.P1),
		mapHomogeneousPoint(t, quad.P2),
		mapHomogeneousPoint(t, quad.P3),
		mapHomogeneousPoint(t, quad.P4),
	}
	// The order of adding the vertices is chosen so that clockwise /
	// counter-clockwise orientation is retained.
	q := clippedQuad3d{points: make([]geometry.Point3F, 0, 6)}
	for i := range h {
		next := h[(i+1)%4]
		if !h[i].ShouldBeClipped() {
			q.add(h[i].CartesianPoint3dUnclamped())
		}
		if h[i].ShouldBeClipped() != next.ShouldBeClipped() {
			q.add(computeClippedCartesianPoint3dForEdge(h[i], next))
		}
	}
	if n := len(q.points); n > 2 && isNearlyTheSamePoint3F(q.points[0], q.points[n-1]) {
		q.points = q.points[:n-1]
	}
	if q.needToClamp {
		clampCoplanar(q.points)
	}
	return q.points, len(q.points) >= 4
}

// clampCoplanar clamps the clipped quad's coordinates to
// kInfiniteCoordinate while keeping the points coplanar
// (math_util.cc:461-594).
func clampCoplanar(points []geometry.Point3F) {
	// Build a normal to the plane by averaging the cross products of
	// adjacent edges.
	var normal geometry.Vector3dF
	if n := len(points); n > 2 {
		loop := points[0].Sub(points[n-1])
		prev := loop
		for i := 1; i < n; i++ {
			cur := points[i].Sub(points[i-1])
			normal = normal.Add(geometry.CrossProduct(prev, cur))
			prev = cur
		}
		normal = normal.Add(geometry.CrossProduct(prev, loop))
	}

	// The scalar arithmetic is float, as in cc.
	clampByPoints := false
	if length := normal.Length(); geometry.IsNormalFloat(length) {
		normal.InvScale(length)
		// The vector to the point in the plane closest to (0,0,0).
		shortest := normal
		shortest.Scale(geometry.DotProduct(normal, points[0].Sub(geometry.Point3F{})))
		// The point in the plane at x=0 and y=0.
		var zAtXYZero float32
		switch {
		case shortest.X == 0 && shortest.Y == 0:
			zAtXYZero = float32(shortest.Z)
		case shortest.Z != 0:
			// v points from the shortest point to the point with x=0 and
			// y=0.
			v := geometry.CrossProduct(normal, geometry.CrossProduct(geometry.Vector3dF{Z: 1}, normal))
			var r float32
			if math.Abs(v.X) > math.Abs(v.Y) {
				r = float32(shortest.X) / float32(v.X)
			} else {
				r = float32(shortest.Y) / float32(v.Y)
			}
			zAtXYZero = float32(shortest.Z) - float32(float32(v.Z)*r)
		default:
			// The plane is parallel to the z axis, so it is not visible.
			clampByPoints = true
		}

		if !clampByPoints {
			// Keep z_at_xy_zero within 3/4 of kInfiniteCoordinate so the
			// clamp does not cut away the likely visible area.
			const maxDistance = 0.75 * kInfiniteCoordinate
			if abs32(zAtXYZero) > maxDistance {
				zDelta := -maxDistance - zAtXYZero
				if zAtXYZero > 0 {
					zDelta = maxDistance - zAtXYZero
				}
				for i := range points {
					points[i].Z = float64(float32(points[i].Z) + zDelta)
				}
				zAtXYZero += zDelta
			}
			// Move every point towards (0, 0, z_at_xy_zero) until all its
			// coordinates are within kInfiniteCoordinate.
			for i := range points {
				x, y, z := float32(points[i].X), float32(points[i].Y), float32(points[i].Z)
				t := float32(1)
				if a := abs32(x); a > kInfiniteCoordinate {
					t = min(t, kInfiniteCoordinate/a)
				}
				if a := abs32(y); a > kInfiniteCoordinate {
					t = min(t, kInfiniteCoordinate/a)
				}
				if abs32(z) > kInfiniteCoordinate {
					zClamped := min(kInfiniteCoordinate, max(-kInfiniteCoordinate, zAtXYZero))
					zOffset := z - zClamped
					zSpace := -kInfiniteCoordinate - zClamped
					if z > 0 {
						zSpace = kInfiniteCoordinate - zClamped
					}
					t = min(t, zSpace/zOffset)
				}
				if t != 1 {
					points[i] = geometry.Point3F{X: float64(t * x), Y: float64(t * y), Z: float64(float32((1-t)*zAtXYZero) + float32(t*z))}
				}
			}
		}
	} else {
		// The points are colinear, so there is no plane to keep.
		clampByPoints = true
	}

	if clampByPoints {
		for i := range points {
			points[i] = geometry.Point3F{
				X: clamp(points[i].X), Y: clamp(points[i].Y), Z: clamp(points[i].Z),
			}
		}
	}
}

func clamp(v float64) float64 {
	return math.Min(kInfiniteCoordinate, math.Max(-kInfiniteCoordinate, v))
}
