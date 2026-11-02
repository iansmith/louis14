package geometry

import "math"

// Transform is a 4×4 transformation matrix, the port of gfx::Transform
// (ui/gfx/geometry/transform.h/.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b)
// that Blink's TransformPaintPropertyNode, ComputedStyle::ApplyTransform and
// the compositor use.
//
// Like gfx::Transform, the zero value is the identity, and a full Matrix44 is
// materialised only once a mutator needs one (EnsureFullMatrix). gfx keeps a
// scale+translation fast path (AxisTransform2d, float storage) in the
// non-full state; louis14 keeps only its identity case, so every
// non-identity transform is a double Matrix44. Chains of only scales and
// translations therefore accumulate in double here and in float in gfx — a
// rounding-level difference. As in gfx, the scale/translate entry points take
// float arguments (narrowed with toFloat).
type Transform struct {
	fullMatrix bool
	matrix     Matrix44
}

// TransformRowMajor builds a transform from its entries in row-major order
// (gfx::Transform::RowMajor).
func TransformRowMajor(r0c0, r0c1, r0c2, r0c3,
	r1c0, r1c1, r1c2, r1c3,
	r2c0, r2c1, r2c2, r2c3,
	r3c0, r3c1, r3c2, r3c3 float64) Transform {
	return Transform{fullMatrix: true, matrix: Matrix44ColMajor(
		r0c0, r1c0, r2c0, r3c0,
		r0c1, r1c1, r2c1, r3c1,
		r0c2, r1c2, r2c2, r3c2,
		r0c3, r1c3, r2c3, r3c3)}
}

// TransformColMajor builds a transform from its entries in column-major order
// (gfx::Transform::ColMajor; CSS matrix3d() argument order).
func TransformColMajor(r0c0, r1c0, r2c0, r3c0,
	r0c1, r1c1, r2c1, r3c1,
	r0c2, r1c2, r2c2, r3c2,
	r0c3, r1c3, r2c3, r3c3 float64) Transform {
	return Transform{fullMatrix: true, matrix: Matrix44ColMajor(
		r0c0, r1c0, r2c0, r3c0,
		r0c1, r1c1, r2c1, r3c1,
		r0c2, r1c2, r2c2, r3c2,
		r0c3, r1c3, r2c3, r3c3)}
}

// TransformAffine builds the 2D affine transform [a c e; b d f]
// (gfx::Transform::Affine; CSS matrix() argument order).
func TransformAffine(a, b, c, d, e, f float64) Transform {
	return TransformColMajor(a, b, 0, 0, c, d, 0, 0, 0, 0, 1, 0, e, f, 0, 1)
}

// MakeTranslation mirrors gfx::Transform::MakeTranslation.
func MakeTranslation(tx, ty float64) Transform {
	var t Transform
	t.Translate(tx, ty)
	return t
}

// MakeScale mirrors gfx::Transform::MakeScale(sx, sy).
func MakeScale(sx, sy float64) Transform {
	var t Transform
	t.Scale(sx, sy)
	return t
}

// getFullMatrix mirrors gfx::Transform::GetFullMatrix.
func (t Transform) getFullMatrix() Matrix44 {
	if !t.fullMatrix {
		return NewMatrix44()
	}
	return t.matrix
}

// ensureFullMatrix mirrors gfx::Transform::EnsureFullMatrix.
func (t *Transform) ensureFullMatrix() *Matrix44 {
	if !t.fullMatrix {
		t.fullMatrix = true
		t.matrix = NewMatrix44()
	}
	return &t.matrix
}

// MakeIdentity resets to the identity.
func (t *Transform) MakeIdentity() { *t = Transform{} }

// Equal mirrors gfx::Transform::operator==.
func (t Transform) Equal(o Transform) bool {
	a, b := t.getFullMatrix(), o.getFullMatrix()
	return a.Equal(&b)
}

// Rc returns the entry at row, col (gfx::Transform::rc).
func (t Transform) Rc(row, col int) float64 {
	if !t.fullMatrix {
		if row == col {
			return 1
		}
		return 0
	}
	return t.matrix.Rc(row, col)
}

// SetRc sets the entry at row, col (gfx::Transform::set_rc).
func (t *Transform) SetRc(row, col int, v float64) { t.ensureFullMatrix().SetRc(row, col, v) }

// PreConcat: this = this * o.
func (t *Transform) PreConcat(o Transform) {
	if !o.fullMatrix {
		return
	}
	if !t.fullMatrix {
		*t = o
		return
	}
	t.matrix.PreConcat(&o.matrix)
}

// PostConcat: this = o * this.
func (t *Transform) PostConcat(o Transform) {
	if !o.fullMatrix {
		return
	}
	if !t.fullMatrix {
		*t = o
		return
	}
	t.matrix.PostConcat(&o.matrix)
}

// Mul returns t * o (gfx::Transform::operator*): o is applied first.
func (t Transform) Mul(o Transform) Transform {
	r := t
	r.PreConcat(o)
	return r
}

// Scale: this = this * scale(x, y).
func (t *Transform) Scale(x, y float64) {
	x, y = toFloat(x), toFloat(y)
	if x == 1 && y == 1 {
		return
	}
	t.ensureFullMatrix().PreScale(x, y)
}

// Scale3d: this = this * scale3d(x, y, z).
func (t *Transform) Scale3d(x, y, z float64) {
	x, y, z = toFloat(x), toFloat(y), toFloat(z)
	if z == 1 {
		t.Scale(x, y)
		return
	}
	t.ensureFullMatrix().PreScale3d(x, y, z)
}

// PostScale: this = scale(x, y) * this.
func (t *Transform) PostScale(x, y float64) {
	x, y = toFloat(x), toFloat(y)
	if x == 1 && y == 1 {
		return
	}
	t.ensureFullMatrix().PostScale(x, y)
}

// PostScale3d: this = scale3d(x, y, z) * this.
func (t *Transform) PostScale3d(x, y, z float64) {
	x, y, z = toFloat(x), toFloat(y), toFloat(z)
	if z == 1 {
		t.PostScale(x, y)
		return
	}
	t.ensureFullMatrix().PostScale3d(x, y, z)
}

// Translate: this = this * translate(x, y).
func (t *Transform) Translate(x, y float64) {
	x, y = toFloat(x), toFloat(y)
	if x == 0 && y == 0 {
		return
	}
	t.ensureFullMatrix().PreTranslate(x, y)
}

// Translate3d: this = this * translate3d(x, y, z).
func (t *Transform) Translate3d(x, y, z float64) {
	x, y, z = toFloat(x), toFloat(y), toFloat(z)
	if z == 0 {
		t.Translate(x, y)
		return
	}
	t.ensureFullMatrix().PreTranslate3d(x, y, z)
}

// PostTranslate: this = translate(x, y) * this.
func (t *Transform) PostTranslate(x, y float64) {
	x, y = toFloat(x), toFloat(y)
	if x == 0 && y == 0 {
		return
	}
	t.ensureFullMatrix().PostTranslate(x, y)
}

// PostTranslate3d: this = translate3d(x, y, z) * this.
func (t *Transform) PostTranslate3d(x, y, z float64) {
	x, y, z = toFloat(x), toFloat(y), toFloat(z)
	if z == 0 {
		t.PostTranslate(x, y)
		return
	}
	t.ensureFullMatrix().PostTranslate3d(x, y, z)
}

// Rotate rotates about the z axis (gfx::Transform::Rotate).
func (t *Transform) Rotate(degrees float64) { t.RotateAboutZAxis(degrees) }

// RotateAboutXAxis: this = this * rotateX(degrees).
func (t *Transform) RotateAboutXAxis(degrees float64) {
	sc := SinCosDegrees(degrees)
	if sc.IsZeroAngle() {
		return
	}
	t.ensureFullMatrix().RotateAboutXAxisSinCos(sc.Sin, sc.Cos)
}

// RotateAboutYAxis: this = this * rotateY(degrees).
func (t *Transform) RotateAboutYAxis(degrees float64) {
	sc := SinCosDegrees(degrees)
	if sc.IsZeroAngle() {
		return
	}
	t.ensureFullMatrix().RotateAboutYAxisSinCos(sc.Sin, sc.Cos)
}

// RotateAboutZAxis: this = this * rotateZ(degrees).
func (t *Transform) RotateAboutZAxis(degrees float64) {
	sc := SinCosDegrees(degrees)
	if sc.IsZeroAngle() {
		return
	}
	t.ensureFullMatrix().RotateAboutZAxisSinCos(sc.Sin, sc.Cos)
}

// RotateAbout rotates about the axis (x, y, z), normalised first; a zero
// axis is a no-op.
func (t *Transform) RotateAbout(x, y, z, degrees float64) {
	sc := SinCosDegrees(degrees)
	if sc.IsZeroAngle() {
		return
	}
	squareLength := x*x + y*y + z*z
	if squareLength == 0 {
		return
	}
	if squareLength != 1 {
		scale := 1.0 / math.Sqrt(squareLength)
		x *= scale
		y *= scale
		z *= scale
	}
	t.ensureFullMatrix().RotateUnitSinCos(x, y, z, sc.Sin, sc.Cos)
}

// Skew: this = this * skew(degreesX, degreesY).
func (t *Transform) Skew(degreesX, degreesY float64) {
	if degreesX == 0 && degreesY == 0 {
		return
	}
	t.ensureFullMatrix().Skew(tanDegrees(degreesX), tanDegrees(degreesY))
}

// SkewX mirrors gfx::Transform::SkewX.
func (t *Transform) SkewX(degrees float64) { t.Skew(degrees, 0) }

// SkewY mirrors gfx::Transform::SkewY.
func (t *Transform) SkewY(degrees float64) { t.Skew(0, degrees) }

func tanDegrees(degrees float64) float64 { return math.Tan(degrees * math.Pi / 180) }

// ApplyPerspectiveDepth: this = this * perspective(depth); a zero depth is
// a no-op.
func (t *Transform) ApplyPerspectiveDepth(depth float64) {
	if depth == 0 {
		return
	}
	t.ensureFullMatrix().ApplyPerspectiveDepth(depth)
}

// IsIdentity mirrors gfx::Transform::IsIdentity.
func (t Transform) IsIdentity() bool { return !t.fullMatrix || t.matrix.IsIdentity() }

// IsIdentityOrTranslation mirrors gfx::Transform::IsIdentityOrTranslation.
func (t Transform) IsIdentityOrTranslation() bool {
	return !t.fullMatrix || t.matrix.IsIdentityOrTranslation()
}

// IsScaleOrTranslation mirrors gfx::Transform::IsScaleOrTranslation.
func (t Transform) IsScaleOrTranslation() bool {
	return !t.fullMatrix || t.matrix.IsScaleOrTranslation()
}

// IsScale2d mirrors gfx::Transform::IsScale2d.
func (t Transform) IsScale2d() bool {
	return !t.fullMatrix || (t.matrix.IsScale() && t.matrix.Rc(2, 2) == 1)
}

// Creates3d reports whether the transform moves points out of the z=0 plane
// (gfx::Transform::Creates3d).
func (t Transform) Creates3d() bool {
	if !t.fullMatrix {
		return false
	}
	return t.matrix.Rc(2, 0) != 0 || t.matrix.Rc(2, 1) != 0 || t.matrix.Rc(2, 3) != 0
}

// HasPerspective mirrors gfx::Transform::HasPerspective.
func (t Transform) HasPerspective() bool { return t.fullMatrix && t.matrix.HasPerspective() }

// IsInvertible mirrors gfx::Transform::IsInvertible: the full 4×4
// determinant is a normal float.
func (t Transform) IsInvertible() bool { return !t.fullMatrix || t.matrix.IsInvertible() }

// Determinant mirrors gfx::Transform::Determinant.
func (t Transform) Determinant() float64 {
	if !t.fullMatrix {
		return 1
	}
	return t.matrix.Determinant()
}

// GetInverse sets *inverse to the inverse and returns true when invertible;
// otherwise sets *inverse to the identity and returns false. inverse may
// point at t.
func (t Transform) GetInverse(inverse *Transform) bool {
	if !t.fullMatrix {
		*inverse = Transform{}
		return true
	}
	var m Matrix44
	if t.matrix.GetInverse(&m) {
		*inverse = Transform{fullMatrix: true, matrix: m}
		return true
	}
	*inverse = Transform{}
	return false
}

// InverseOrIdentity mirrors gfx::Transform::InverseOrIdentity.
func (t Transform) InverseOrIdentity() Transform {
	var inverse Transform
	t.GetInverse(&inverse)
	return inverse
}

// kEpsilon is transform.cc's std::numeric_limits<float>::epsilon().
const kEpsilon = 1.1920928955078125e-07

// IsBackFaceVisible reports whether a layer with forward normal (0, 0, 1, 0)
// shows its back face after this transform: the sign of the (3,3) entry of
// the inverse-transpose, from its cofactor and the determinant
// (gfx::Transform::IsBackFaceVisible, transform.cc:424-477).
func (t Transform) IsBackFaceVisible() bool {
	if !t.fullMatrix {
		return false
	}
	m := &t.matrix
	determinant := m.Determinant()
	// A non-invertible matrix is treated as front-facing.
	if determinant == 0 {
		return false
	}
	cofactor33 := m.Rc(0, 0)*m.Rc(1, 1)*m.Rc(3, 3) +
		m.Rc(0, 1)*m.Rc(1, 3)*m.Rc(3, 0) +
		m.Rc(0, 3)*m.Rc(1, 0)*m.Rc(3, 1) -
		m.Rc(0, 0)*m.Rc(1, 3)*m.Rc(3, 1) -
		m.Rc(0, 1)*m.Rc(1, 0)*m.Rc(3, 3) -
		m.Rc(0, 3)*m.Rc(1, 1)*m.Rc(3, 0)
	return cofactor33*determinant < -kEpsilon
}

// Transpose mirrors gfx::Transform::Transpose.
func (t *Transform) Transpose() {
	if !t.IsScale2d() {
		t.ensureFullMatrix().Transpose()
	}
}

// ApplyTransformOrigin makes (x, y, z) the fixed point of the transform:
// this = translate(x, y, z) * this * translate(-x, -y, -z).
func (t *Transform) ApplyTransformOrigin(x, y, z float64) {
	x, y, z = toFloat(x), toFloat(y), toFloat(z)
	t.PostTranslate3d(x, y, z)
	t.Translate3d(-x, -y, -z)
}

// Flatten drops the z row and column (gfx::Transform::Flatten).
func (t *Transform) Flatten() {
	if t.fullMatrix {
		t.matrix.Flatten()
	}
}

// IsFlat mirrors gfx::Transform::IsFlat.
func (t Transform) IsFlat() bool { return !t.fullMatrix || t.matrix.IsFlat() }

// Is2dTransform mirrors gfx::Transform::Is2dTransform: flat and without
// perspective.
func (t Transform) Is2dTransform() bool { return !t.fullMatrix || t.matrix.Is2dTransform() }

// MapPoint maps a point on the z=0 plane, dividing by w when w is a normal
// non-1 value (gfx::Transform::MapPoint(PointF), MapPointInternal).
func (t Transform) MapPoint(p PointF) PointF {
	p = PointF{toFloat(p.X), toFloat(p.Y)} // gfx::PointF is float
	if !t.fullMatrix {
		return PointF{clampFloatGeometry(p.X), clampFloatGeometry(p.Y)}
	}
	v := [2]float64{p.X, p.Y}
	w := t.matrix.MapVector2(&v)
	if w != 1 && isNormal(w) {
		wInverse := 1.0 / w
		return PointF{clampFloatGeometry(v[0] * wInverse), clampFloatGeometry(v[1] * wInverse)}
	}
	return PointF{clampFloatGeometry(v[0]), clampFloatGeometry(v[1])}
}

// MapPoint3F maps a 3D point (gfx::Transform::MapPoint(Point3F)).
func (t Transform) MapPoint3F(p Point3F) Point3F {
	p = Point3F{toFloat(p.X), toFloat(p.Y), toFloat(p.Z)} // gfx::Point3F is float
	if !t.fullMatrix {
		return Point3F{clampFloatGeometry(p.X), clampFloatGeometry(p.Y), clampFloatGeometry(p.Z)}
	}
	v := [4]float64{p.X, p.Y, p.Z, 1}
	t.matrix.MapVector4(&v)
	if v[3] != 1 && isNormal(v[3]) {
		wInverse := 1.0 / v[3]
		return Point3F{clampFloatGeometry(v[0] * wInverse), clampFloatGeometry(v[1] * wInverse), clampFloatGeometry(v[2] * wInverse)}
	}
	return Point3F{clampFloatGeometry(v[0]), clampFloatGeometry(v[1]), clampFloatGeometry(v[2])}
}

// MapVector maps a direction (w = 0), gfx::Transform::MapVector
// (transform.cc:714-724 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (t Transform) MapVector(v Vector3dF) Vector3dF {
	v = Vector3dF{toFloat(v.X), toFloat(v.Y), toFloat(v.Z)} // gfx::Vector3dF is float
	if !t.fullMatrix {
		return Vector3dF{clampFloatGeometry(v.X), clampFloatGeometry(v.Y), clampFloatGeometry(v.Z)}
	}
	p := [4]float64{v.X, v.Y, v.Z, 0}
	t.matrix.MapVector4(&p)
	return Vector3dF{clampFloatGeometry(p[0]), clampFloatGeometry(p[1]), clampFloatGeometry(p[2])}
}

// TransformVector4 maps a homogeneous vector in place,
// gfx::Transform::TransformVector4 (transform.cc:726-741): float in,
// double math, float out.
func (t Transform) TransformVector4(v *[4]float64) {
	for i := range v {
		v[i] = toFloat(v[i]) // the vector is float
	}
	if t.fullMatrix {
		t.matrix.MapVector4(v)
	}
	for i := range v {
		v[i] = clampFloatGeometry(v[i])
	}
}

// kProjectionClampedBigNumber is the coordinate ProjectPoint returns for a
// point behind the eye: 1 << (numeric_limits<float>::digits - 1).
const kProjectionClampedBigNumber = float64(1 << 23)

// ProjectPoint casts a ray parallel to the z axis through p (in the
// destination plane) and returns where it meets the transformed z=0 plane,
// mapped back to source space; clamped reports that the ray missed the
// plane or met it behind the eye (gfx::Transform::ProjectPoint,
// transform.cc:847-905).
func (t Transform) ProjectPoint(p PointF) (projected PointF, clamped bool) {
	p = PointF{toFloat(p.X), toFloat(p.Y)} // gfx::PointF is float
	if !t.fullMatrix {
		return PointF{clampFloatGeometry(p.X), clampFloatGeometry(p.Y)}, false
	}
	m := &t.matrix
	if !isNormal(m.Rc(2, 2)) {
		// The projection plane is parallel to the ray.
		return PointF{}, true
	}
	x, y := p.X, p.Y
	z := -(m.Rc(2, 0)*x + m.Rc(2, 1)*y + m.Rc(2, 3)) / m.Rc(2, 2)
	if math.IsInf(z, 0) || math.IsNaN(z) {
		return PointF{}, true
	}
	v := [4]float64{x, y, z, 1}
	m.MapVector4(&v)
	if v[3] <= 0 {
		return PointF{math.Copysign(kProjectionClampedBigNumber, v[0]), math.Copysign(kProjectionClampedBigNumber, v[1])}, true
	}
	if v[3] != 1 {
		v[0] /= v[3]
		v[1] /= v[3]
	}
	return PointF{clampFloatGeometry(v[0]), clampFloatGeometry(v[1])}, false
}

// toFloat narrows v to float precision, as passing a double to one of
// gfx::Transform's float parameters does.
func toFloat(v float64) float64 { return float64(float32(v)) }

// isNormal mirrors std::isnormal on a double.
func isNormal(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) || v == 0 {
		return false
	}
	return math.Abs(v) >= 0x1p-1022
}

// floatGeometryMax is FloatGeometrySaturationHandler::max():
// numeric_limits<float>::max() / 1e6.
const floatGeometryMax = math.MaxFloat32 / 1e6

// clampFloatGeometry mirrors gfx::ClampFloatGeometry
// (ui/gfx/geometry/clamp_float_geometry.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): saturate to ±max/1e6, NaN → 0,
// then round to float.
func clampFloatGeometry(v float64) float64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v > floatGeometryMax:
		return float64(float32(floatGeometryMax))
	case v < -floatGeometryMax:
		return float64(float32(-floatGeometryMax))
	}
	return float64(float32(v))
}
