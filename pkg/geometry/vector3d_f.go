package geometry

import "math"

// Vector3dF is a 3D vector, gfx::Vector3dF (ui/gfx/geometry/vector3d_f.h,
// vector3d_f.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). gfx keeps the
// components in float and computes in float; louis14 stores float64 values
// that the operations round to float, as Point3F results are, so geometry
// that depends on float rounding (viz's plane splitting) matches. Every
// product is converted explicitly so the compiler cannot fuse it into a
// multiply-add (the Go spec allows fusing an unconverted a*b+c), which
// would round differently from gfx.
type Vector3dF struct {
	X, Y, Z float64
}

// f32s returns v's components as float.
func (v Vector3dF) f32s() (x, y, z float32) { return float32(v.X), float32(v.Y), float32(v.Z) }

func vec32(x, y, z float32) Vector3dF { return Vector3dF{float64(x), float64(y), float64(z)} }

// Add mirrors operator+(Vector3dF, Vector3dF).
func (v Vector3dF) Add(o Vector3dF) Vector3dF {
	x, y, z := v.f32s()
	ox, oy, oz := o.f32s()
	return vec32(x+ox, y+oy, z+oz)
}

// LengthSquared mirrors Vector3dF::LengthSquared, computed in double.
func (v Vector3dF) LengthSquared() float64 {
	x, y, z := toFloat(v.X), toFloat(v.Y), toFloat(v.Z)
	return float64(x*x) + float64(y*y) + float64(z*z)
}

// Length mirrors Vector3dF::Length.
func (v Vector3dF) Length() float64 { return toFloat(math.Sqrt(v.LengthSquared())) }

// Scale mirrors Vector3dF::Scale(float).
func (v *Vector3dF) Scale(s float64) {
	x, y, z := v.f32s()
	k := float32(s)
	*v = vec32(x*k, y*k, z*k)
}

// InvScale mirrors Vector3dF::InvScale(float).
func (v *Vector3dF) InvScale(s float64) {
	x, y, z := v.f32s()
	k := float32(s)
	*v = vec32(x/k, y/k, z/k)
}

// CrossProduct mirrors gfx::CrossProduct(Vector3dF, Vector3dF), which
// computes in double and stores float (Vector3dF::Cross).
func CrossProduct(a, b Vector3dF) Vector3dF {
	ax, ay, az := toFloat(a.X), toFloat(a.Y), toFloat(a.Z)
	bx, by, bz := toFloat(b.X), toFloat(b.Y), toFloat(b.Z)
	return vec32(
		float32(float64(ay*bz)-float64(az*by)),
		float32(float64(az*bx)-float64(ax*bz)),
		float32(float64(ax*by)-float64(ay*bx)),
	)
}

// DotProduct mirrors gfx::DotProduct(Vector3dF, Vector3dF), computed in
// float.
func DotProduct(a, b Vector3dF) float64 {
	ax, ay, az := a.f32s()
	bx, by, bz := b.f32s()
	return float64(float32(ax*bx) + float32(ay*by) + float32(az*bz))
}

// Sub mirrors operator-(Point3F, Point3F): the vector from q to p.
func (p Point3F) Sub(q Point3F) Vector3dF {
	return vec32(float32(p.X)-float32(q.X), float32(p.Y)-float32(q.Y), float32(p.Z)-float32(q.Z))
}
