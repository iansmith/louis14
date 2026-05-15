package geometry

import "math"

// AffineTransform is a 2D affine transformation represented as a 2×3 matrix.
//
// The standard SVG/CSS form is
//
//	[ a c e ]   [ x ]   [ a·x + c·y + e ]
//	[ b d f ] · [ y ] = [ b·x + d·y + f ]
//	[ 0 0 1 ]   [ 1 ]   [        1       ]
//
// Mirrors Blink's gfx::AffineTransform (ui/gfx/geometry/transform.h with the
// 2D subset). Used by the SVG render subsystem to compose the viewBox →
// viewport mapping, nested <g transform="…">, paint-server transforms,
// gradientTransform, etc.
//
// Composition convention: a.Compose(b) returns the transform that, applied
// to a point p, gives a(b(p)) — i.e. b is applied first, then a. This matches
// Blink's gfx::AffineTransform::PreConcat semantics.
type AffineTransform struct {
	A, B, C, D, E, F float64
}

// Identity returns the identity transform.
func Identity() AffineTransform {
	return AffineTransform{A: 1, B: 0, C: 0, D: 1, E: 0, F: 0}
}

// NewAffineTransform constructs an affine transform from its six matrix
// entries (a, b, c, d, e, f), matching the SVG `matrix(a b c d e f)` form.
func NewAffineTransform(a, b, c, d, e, f float64) AffineTransform {
	return AffineTransform{A: a, B: b, C: c, D: d, E: e, F: f}
}

// Translate returns a translation transform.
func Translate(tx, ty float64) AffineTransform {
	return AffineTransform{A: 1, B: 0, C: 0, D: 1, E: tx, F: ty}
}

// Scale returns a non-uniform scaling transform.
func Scale(sx, sy float64) AffineTransform {
	return AffineTransform{A: sx, B: 0, C: 0, D: sy, E: 0, F: 0}
}

// Rotate returns a rotation by the given angle in degrees about the origin.
// SVG rotations are clockwise in the default Y-down coordinate system.
func Rotate(deg float64) AffineTransform {
	r := deg * math.Pi / 180.0
	cs, sn := math.Cos(r), math.Sin(r)
	return AffineTransform{A: cs, B: sn, C: -sn, D: cs, E: 0, F: 0}
}

// SkewX returns a skew transform along the X axis (angle in degrees).
func SkewX(deg float64) AffineTransform {
	t := math.Tan(deg * math.Pi / 180.0)
	return AffineTransform{A: 1, B: 0, C: t, D: 1, E: 0, F: 0}
}

// SkewY returns a skew transform along the Y axis (angle in degrees).
func SkewY(deg float64) AffineTransform {
	t := math.Tan(deg * math.Pi / 180.0)
	return AffineTransform{A: 1, B: t, C: 0, D: 1, E: 0, F: 0}
}

// IsIdentity reports whether this is the identity transform.
func (t AffineTransform) IsIdentity() bool {
	return t.A == 1 && t.B == 0 && t.C == 0 && t.D == 1 && t.E == 0 && t.F == 0
}

// Determinant returns the determinant of the linear (a, b, c, d) part.
// The translation (e, f) does not affect it. Used to detect non-invertible
// (singular) transforms.
func (t AffineTransform) Determinant() float64 {
	return t.A*t.D - t.B*t.C
}

// Compose returns this · other: applying the result to a point p gives
// this(other(p)) — `other` is applied first, then this.
//
// This is the standard matrix product
//
//	[ a₁ c₁ e₁ ]   [ a₂ c₂ e₂ ]
//	[ b₁ d₁ f₁ ] · [ b₂ d₂ f₂ ]
//	[  0  0  1 ]   [  0  0  1 ]
//
// expanded to 2×3 form.
func (t AffineTransform) Compose(other AffineTransform) AffineTransform {
	return AffineTransform{
		A: t.A*other.A + t.C*other.B,
		B: t.B*other.A + t.D*other.B,
		C: t.A*other.C + t.C*other.D,
		D: t.B*other.C + t.D*other.D,
		E: t.A*other.E + t.C*other.F + t.E,
		F: t.B*other.E + t.D*other.F + t.F,
	}
}

// Invert returns the inverse transform. The second return value is false
// when the transform is singular (determinant ≈ 0); the returned transform
// is the identity in that case.
func (t AffineTransform) Invert() (AffineTransform, bool) {
	det := t.Determinant()
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return Identity(), false
	}
	inv := 1.0 / det
	return AffineTransform{
		A: t.D * inv,
		B: -t.B * inv,
		C: -t.C * inv,
		D: t.A * inv,
		E: (t.C*t.F - t.D*t.E) * inv,
		F: (t.B*t.E - t.A*t.F) * inv,
	}, true
}

// PointF is a 2D point in user-space coordinates (float64).
//
// Distinct from geometry.PhysicalOffset, which uses sub-pixel LayoutUnit and
// belongs to the CSS box-tree coordinate system. PointF is for SVG's
// "user units" — a continuous floating-point plane that participates in
// affine transforms.
type PointF struct {
	X, Y float64
}

// SizeF is a 2D extent in user-space coordinates (float64).
type SizeF struct {
	Width, Height float64
}

// RectF is an axis-aligned rectangle in user-space coordinates (float64),
// represented as an origin (top-left) plus a non-negative extent.
type RectF struct {
	Origin PointF
	Size   SizeF
}

// NewRectF constructs a RectF from x, y, width, height.
func NewRectF(x, y, w, h float64) RectF {
	return RectF{Origin: PointF{X: x, Y: y}, Size: SizeF{Width: w, Height: h}}
}

// X returns the rect's left edge.
func (r RectF) X() float64 { return r.Origin.X }

// Y returns the rect's top edge.
func (r RectF) Y() float64 { return r.Origin.Y }

// Width returns the rect's width.
func (r RectF) Width() float64 { return r.Size.Width }

// Height returns the rect's height.
func (r RectF) Height() float64 { return r.Size.Height }

// Right returns r.X() + r.Width().
func (r RectF) Right() float64 { return r.Origin.X + r.Size.Width }

// Bottom returns r.Y() + r.Height().
func (r RectF) Bottom() float64 { return r.Origin.Y + r.Size.Height }

// IsEmpty reports whether the rect has zero or negative width or height.
func (r RectF) IsEmpty() bool {
	return r.Size.Width <= 0 || r.Size.Height <= 0
}

// MapPoint returns t applied to p.
func (t AffineTransform) MapPoint(p PointF) PointF {
	return PointF{
		X: t.A*p.X + t.C*p.Y + t.E,
		Y: t.B*p.X + t.D*p.Y + t.F,
	}
}

// MapRect returns the axis-aligned bounding box of t applied to the four
// corners of r. For pure translations and axis-aligned scales the result
// equals the exact image; for rotations / skews it is the conservative AABB
// of the transformed quadrilateral — matching Blink's
// gfx::AffineTransform::MapRect semantics.
func (t AffineTransform) MapRect(r RectF) RectF {
	if r.IsEmpty() {
		// Preserve the empty/zero rect's origin under the transform but
		// keep zero extent. Mirrors Blink's treatment of empty rects.
		p := t.MapPoint(r.Origin)
		return RectF{Origin: p}
	}
	p0 := t.MapPoint(PointF{X: r.X(), Y: r.Y()})
	p1 := t.MapPoint(PointF{X: r.Right(), Y: r.Y()})
	p2 := t.MapPoint(PointF{X: r.Right(), Y: r.Bottom()})
	p3 := t.MapPoint(PointF{X: r.X(), Y: r.Bottom()})

	minX := math.Min(math.Min(p0.X, p1.X), math.Min(p2.X, p3.X))
	maxX := math.Max(math.Max(p0.X, p1.X), math.Max(p2.X, p3.X))
	minY := math.Min(math.Min(p0.Y, p1.Y), math.Min(p2.Y, p3.Y))
	maxY := math.Max(math.Max(p0.Y, p1.Y), math.Max(p2.Y, p3.Y))

	return NewRectF(minX, minY, maxX-minX, maxY-minY)
}
