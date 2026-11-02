package geometry

import "math"

// Matrix44 is a 4×4 double matrix in column-major storage, the backing store
// of Transform. Port of gfx::Matrix44 (ui/gfx/geometry/matrix44.h/.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), limited to the operations
// Transform uses. The zero value is the zero matrix; use NewMatrix44 for
// identity.
type Matrix44 struct {
	// matrix is indexed by [col][row], as in Chromium.
	matrix [4][4]float64
}

// NewMatrix44 returns the identity matrix (gfx::Matrix44()).
func NewMatrix44() Matrix44 {
	return Matrix44{matrix: [4][4]float64{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
	}}
}

// Matrix44ColMajor builds a matrix from parameters in column-major order
// (the gfx::Matrix44 16-argument constructor).
func Matrix44ColMajor(r0c0, r1c0, r2c0, r3c0,
	r0c1, r1c1, r2c1, r3c1,
	r0c2, r1c2, r2c2, r3c2,
	r0c3, r1c3, r2c3, r3c3 float64) Matrix44 {
	return Matrix44{matrix: [4][4]float64{
		{r0c0, r1c0, r2c0, r3c0},
		{r0c1, r1c1, r2c1, r3c1},
		{r0c2, r1c2, r2c2, r3c2},
		{r0c3, r1c3, r2c3, r3c3},
	}}
}

func (m *Matrix44) col(i int) double4       { return double4(m.matrix[i]) }
func (m *Matrix44) setCol(i int, v double4) { m.matrix[i] = v }

// Equal mirrors gfx::Matrix44::operator==.
func (m *Matrix44) Equal(o *Matrix44) bool { return m.matrix == o.matrix }

// IsIdentity mirrors gfx::Matrix44::IsIdentity.
func (m *Matrix44) IsIdentity() bool { return m.matrix == NewMatrix44().matrix }

// IsIdentityOrTranslation mirrors gfx::Matrix44::IsIdentityOrTranslation.
func (m *Matrix44) IsIdentityOrTranslation() bool {
	return m.col(0) == double4{1, 0, 0, 0} &&
		m.col(1) == double4{0, 1, 0, 0} &&
		m.col(2) == double4{0, 0, 1, 0} && m.matrix[3][3] == 1
}

// IsScaleOrTranslation mirrors gfx::Matrix44::IsScaleOrTranslation.
func (m *Matrix44) IsScaleOrTranslation() bool {
	return (double4{m.matrix[0][1], m.matrix[0][2], m.matrix[0][3], m.matrix[1][0]}) == double4{} &&
		(double4{m.matrix[1][2], m.matrix[1][3], m.matrix[2][0], m.matrix[2][1]}) == double4{} &&
		m.matrix[2][3] == 0 && m.matrix[3][3] == 1
}

// IsScale mirrors gfx::Matrix44::IsScale.
func (m *Matrix44) IsScale() bool {
	return m.IsScaleOrTranslation() && m.col(3) == double4{0, 0, 0, 1}
}

// IsFlat mirrors gfx::Matrix44::IsFlat: the third row and column are
// (0, 0, 1, 0).
func (m *Matrix44) IsFlat() bool {
	return m.col(2) == double4{0, 0, 1, 0} &&
		(double4{m.matrix[0][2], m.matrix[1][2], 0, m.matrix[3][2]}) == double4{}
}

// HasPerspective mirrors gfx::Matrix44::HasPerspective: the fourth row is
// not (0, 0, 0, 1).
func (m *Matrix44) HasPerspective() bool {
	return (double4{m.matrix[0][3], m.matrix[1][3], m.matrix[2][3], m.matrix[3][3]}) != double4{0, 0, 0, 1}
}

// Is2dTransform mirrors gfx::Matrix44::Is2dTransform.
func (m *Matrix44) Is2dTransform() bool { return m.IsFlat() && !m.HasPerspective() }

// Rc returns the value at row, col (gfx::Matrix44::rc).
func (m *Matrix44) Rc(row, col int) float64 { return m.matrix[col][row] }

// SetRc sets the value at row, col (gfx::Matrix44::set_rc).
func (m *Matrix44) SetRc(row, col int, v float64) { m.matrix[col][row] = v }

// PreTranslate: this = this * translation (matrix44.cc).
func (m *Matrix44) PreTranslate(dx, dy float64) {
	m.setCol(3, m.col(0).scale(dx).add(m.col(1).scale(dy)).add(m.col(3)))
}

// PreTranslate3d: this = this * translation.
func (m *Matrix44) PreTranslate3d(dx, dy, dz float64) {
	if dx == 0 && dy == 0 && dz == 0 {
		return
	}
	m.setCol(3, m.col(0).scale(dx).add(m.col(1).scale(dy)).add(m.col(2).scale(dz)).add(m.col(3)))
}

// PostTranslate: this = translation * this.
func (m *Matrix44) PostTranslate(dx, dy float64) {
	if !m.HasPerspective() {
		m.matrix[3][0] += dx
		m.matrix[3][1] += dy
		return
	}
	if dx != 0 {
		m.matrix[0][0] += m.matrix[0][3] * dx
		m.matrix[1][0] += m.matrix[1][3] * dx
		m.matrix[2][0] += m.matrix[2][3] * dx
		m.matrix[3][0] += m.matrix[3][3] * dx
	}
	if dy != 0 {
		m.matrix[0][1] += m.matrix[0][3] * dy
		m.matrix[1][1] += m.matrix[1][3] * dy
		m.matrix[2][1] += m.matrix[2][3] * dy
		m.matrix[3][1] += m.matrix[3][3] * dy
	}
}

// PostTranslate3d: this = translation * this.
func (m *Matrix44) PostTranslate3d(dx, dy, dz float64) {
	t := double4{dx, dy, dz, 0}
	if t == (double4{}) {
		return
	}
	if !m.HasPerspective() {
		m.setCol(3, m.col(3).add(t))
		return
	}
	for i := 0; i < 4; i++ {
		m.setCol(i, m.col(i).add(t.scale(m.matrix[i][3])))
	}
}

// PreScale: this = this * scale.
func (m *Matrix44) PreScale(sx, sy float64) {
	m.setCol(0, m.col(0).scale(sx))
	m.setCol(1, m.col(1).scale(sy))
}

// PreScale3d: this = this * scale.
func (m *Matrix44) PreScale3d(sx, sy, sz float64) {
	if sx == 1 && sy == 1 && sz == 1 {
		return
	}
	m.setCol(0, m.col(0).scale(sx))
	m.setCol(1, m.col(1).scale(sy))
	m.setCol(2, m.col(2).scale(sz))
}

// PostScale: this = scale * this.
func (m *Matrix44) PostScale(sx, sy float64) {
	if sx != 1 {
		for c := 0; c < 4; c++ {
			m.matrix[c][0] *= sx
		}
	}
	if sy != 1 {
		for c := 0; c < 4; c++ {
			m.matrix[c][1] *= sy
		}
	}
}

// PostScale3d: this = scale * this.
func (m *Matrix44) PostScale3d(sx, sy, sz float64) {
	if sx == 1 && sy == 1 && sz == 1 {
		return
	}
	s := double4{sx, sy, sz, 1}
	for i := 0; i < 4; i++ {
		m.setCol(i, m.col(i).mul(s))
	}
}

// RotateUnitSinCos rotates about the unit-length axis (x, y, z):
// this = this * rotation.
func (m *Matrix44) RotateUnitSinCos(x, y, z, sinAngle, cosAngle float64) {
	// Major-axis fast paths; the axis is already normalized.
	if z == 1 {
		m.RotateAboutZAxisSinCos(sinAngle, cosAngle)
		return
	}
	if y == 1 {
		m.RotateAboutYAxisSinCos(sinAngle, cosAngle)
		return
	}
	if x == 1 {
		m.RotateAboutXAxisSinCos(sinAngle, cosAngle)
		return
	}
	c := cosAngle
	s := sinAngle
	C := 1 - c
	xs, ys, zs := x*s, y*s, z*s
	xC, yC, zC := x*C, y*C, z*C
	xyC, yzC, zxC := x*yC, y*zC, z*xC
	rot := Matrix44ColMajor(x*xC+c, xyC+zs, zxC-ys, 0, // col 0
		xyC-zs, y*yC+c, yzC+xs, 0, // col 1
		zxC+ys, yzC-xs, z*zC+c, 0, // col 2
		0, 0, 0, 1) // col 3
	m.PreConcat(&rot)
}

// RotateAboutXAxisSinCos: this = this * rotateX.
func (m *Matrix44) RotateAboutXAxisSinCos(sinAngle, cosAngle float64) {
	c1, c2 := m.col(1), m.col(2)
	m.setCol(1, c1.scale(cosAngle).add(c2.scale(sinAngle)))
	m.setCol(2, c2.scale(cosAngle).sub(c1.scale(sinAngle)))
}

// RotateAboutYAxisSinCos: this = this * rotateY.
func (m *Matrix44) RotateAboutYAxisSinCos(sinAngle, cosAngle float64) {
	c0, c2 := m.col(0), m.col(2)
	m.setCol(0, c0.scale(cosAngle).sub(c2.scale(sinAngle)))
	m.setCol(2, c2.scale(cosAngle).add(c0.scale(sinAngle)))
}

// RotateAboutZAxisSinCos: this = this * rotateZ.
func (m *Matrix44) RotateAboutZAxisSinCos(sinAngle, cosAngle float64) {
	c0, c1 := m.col(0), m.col(1)
	m.setCol(0, c0.scale(cosAngle).add(c1.scale(sinAngle)))
	m.setCol(1, c1.scale(cosAngle).sub(c0.scale(sinAngle)))
}

// Skew: this = this * skew.
func (m *Matrix44) Skew(tanSkewX, tanSkewY float64) {
	c0, c1 := m.col(0), m.col(1)
	m.setCol(0, c0.add(c1.scale(tanSkewY)))
	m.setCol(1, c1.add(c0.scale(tanSkewX)))
}

// ApplyPerspectiveDepth: this = this * perspective. perspective must be
// non-zero.
func (m *Matrix44) ApplyPerspectiveDepth(perspective float64) {
	m.setCol(2, m.col(2).add(m.col(3).scale(-1/perspective)))
}

// PreConcat: this = this * o.
func (m *Matrix44) PreConcat(o *Matrix44) { m.SetConcat(m, o) }

// PostConcat: this = o * this.
func (m *Matrix44) PostConcat(o *Matrix44) { m.SetConcat(o, m) }

// SetConcat: this = x * y. x or y may alias m.
func (m *Matrix44) SetConcat(x, y *Matrix44) {
	if x.Is2dTransform() && y.Is2dTransform() {
		a, b := x.matrix[0][0], x.matrix[0][1]
		c, d := x.matrix[1][0], x.matrix[1][1]
		e, f := x.matrix[3][0], x.matrix[3][1]
		ya, yb := y.matrix[0][0], y.matrix[0][1]
		yc, yd := y.matrix[1][0], y.matrix[1][1]
		ye, yf := y.matrix[3][0], y.matrix[3][1]
		*m = Matrix44ColMajor(a*ya+c*yb, b*ya+d*yb, 0, 0, // col 0
			a*yc+c*yd, b*yc+d*yd, 0, 0, // col 1
			0, 0, 1, 0, // col 2
			a*ye+c*yf+e, b*ye+d*yf+f, 0, 1) // col 3
		return
	}
	// result col j = sum_k x.col(k) * y[j][k]; computed into a temporary so
	// aliasing is safe.
	var r [4][4]float64
	for j := 0; j < 4; j++ {
		yc := &y.matrix[j]
		for row := 0; row < 4; row++ {
			r[j][row] = x.matrix[0][row]*yc[0] + x.matrix[1][row]*yc[1] +
				x.matrix[2][row]*yc[2] + x.matrix[3][row]*yc[3]
		}
	}
	m.matrix = r
}

// isNormalFloat mirrors std::isnormal(static_cast<float>(v)): v is finite,
// non-zero and not subnormal at float precision.
func isNormalFloat(v float64) bool {
	f := float64(float32(v))
	if math.IsNaN(f) || math.IsInf(f, 0) || f == 0 {
		return false
	}
	return math.Abs(f) >= math.SmallestNonzeroFloat32*(1<<23)
}

// inverseWithDouble4Cols inverts the matrix held in the four columns in
// place, reporting false (and leaving garbage) when it is singular. Port of
// the anonymous InverseWithDouble4Cols in matrix44.cc (Intel AP-928).
func inverseWithDouble4Cols(c0, c1, c2, c3 *double4) bool {
	// Note that r1 and r3 have components 2/3 and 0/1 swapped.
	r0 := double4{c0[0], c1[0], c2[0], c3[0]}
	r1 := double4{c2[1], c3[1], c0[1], c1[1]}
	r2 := double4{c0[2], c1[2], c2[2], c3[2]}
	r3 := double4{c2[3], c3[3], c0[3], c1[3]}

	t := r2.mul(r3).swapInPairs()
	*c0 = r1.mul(t)
	*c1 = r0.mul(t)

	t = t.swapHighLow()
	*c0 = r1.mul(t).sub(*c0)
	*c1 = r0.mul(t).sub(*c1).swapHighLow()

	t = r1.mul(r2).swapInPairs()
	*c0 = c0.add(r3.mul(t))
	*c3 = r0.mul(t)

	t = t.swapHighLow()
	*c0 = c0.sub(r3.mul(t))
	*c3 = r0.mul(t).sub(*c3).swapHighLow()

	t = r1.swapHighLow().mul(r3).swapInPairs()
	r2 = r2.swapHighLow()
	*c0 = c0.add(r2.mul(t))
	*c2 = r0.mul(t)

	t = t.swapHighLow()
	*c0 = c0.sub(r2.mul(t))

	det := r0.mul(*c0).sum()
	if !isNormalFloat(det) {
		return false
	}

	*c2 = r0.mul(t).sub(*c2).swapHighLow()

	t = r0.mul(r1).swapInPairs()
	*c2 = r3.mul(t).add(*c2)
	*c3 = r2.mul(t).sub(*c3)

	t = t.swapHighLow()
	*c2 = r3.mul(t).sub(*c2)
	*c3 = c3.sub(r2.mul(t))

	t = r0.mul(r3).swapInPairs()
	*c1 = c1.sub(r2.mul(t))
	*c2 = r1.mul(t).add(*c2)

	t = t.swapHighLow()
	*c1 = r2.mul(t).add(*c1)
	*c2 = c2.sub(r1.mul(t))

	t = r0.mul(r2).swapInPairs()
	*c1 = r3.mul(t).add(*c1)
	*c3 = c3.sub(r1.mul(t))

	t = t.swapHighLow()
	*c1 = c1.sub(r3.mul(t))
	*c3 = r1.mul(t).add(*c3)

	det = 1.0 / det
	*c0 = c0.scale(det)
	*c1 = c1.scale(det)
	*c2 = c2.scale(det)
	*c3 = c3.scale(det)
	return true
}

// GetInverse sets inverse to the inverted matrix and returns true when this
// matrix is invertible; otherwise returns false and leaves inverse
// unchanged.
func (m *Matrix44) GetInverse(inverse *Matrix44) bool {
	if m.Is2dTransform() {
		determinant := m.Determinant()
		if !isNormalFloat(determinant) {
			return false
		}
		invDet := 1.0 / determinant
		a, b := m.matrix[0][0], m.matrix[0][1]
		c, d := m.matrix[1][0], m.matrix[1][1]
		e, f := m.matrix[3][0], m.matrix[3][1]
		*inverse = Matrix44ColMajor(d*invDet, -b*invDet, 0, 0, // col 0
			-c*invDet, a*invDet, 0, 0, // col 1
			0, 0, 1, 0, // col 2
			(c*f-d*e)*invDet, (b*e-a*f)*invDet, 0, 1) // col 3
		return true
	}
	c0, c1, c2, c3 := m.col(0), m.col(1), m.col(2), m.col(3)
	if !inverseWithDouble4Cols(&c0, &c1, &c2, &c3) {
		return false
	}
	inverse.setCol(0, c0)
	inverse.setCol(1, c1)
	inverse.setCol(2, c2)
	inverse.setCol(3, c3)
	return true
}

// IsInvertible mirrors gfx::Matrix44::IsInvertible.
func (m *Matrix44) IsInvertible() bool { return isNormalFloat(m.Determinant()) }

// Determinant mirrors gfx::Matrix44::Determinant (a reduced
// InverseWithDouble4Cols).
func (m *Matrix44) Determinant() float64 {
	if m.Is2dTransform() {
		return m.matrix[0][0]*m.matrix[1][1] - m.matrix[0][1]*m.matrix[1][0]
	}
	c0, c1, c2, c3 := m.col(0), m.col(1), m.col(2), m.col(3)
	r0 := double4{c0[0], c1[0], c2[0], c3[0]}
	r1 := double4{c2[1], c3[1], c0[1], c1[1]}
	r2 := double4{c0[2], c1[2], c2[2], c3[2]}
	r3 := double4{c2[3], c3[3], c0[3], c1[3]}

	t := r2.mul(r3).swapInPairs()
	c0 = r1.mul(t)
	t = t.swapHighLow()
	c0 = r1.mul(t).sub(c0)
	t = r1.mul(r2).swapInPairs()
	c0 = c0.add(r3.mul(t))
	t = t.swapHighLow()
	c0 = c0.sub(r3.mul(t))
	t = r1.swapHighLow().mul(r3).swapInPairs()
	r2 = r2.swapHighLow()
	c0 = c0.add(r2.mul(t))
	t = t.swapHighLow()
	c0 = c0.sub(r2.mul(t))
	return r0.mul(c0).sum()
}

// Transpose transposes the matrix in place.
func (m *Matrix44) Transpose() {
	for c := 0; c < 4; c++ {
		for r := c + 1; r < 4; r++ {
			m.matrix[c][r], m.matrix[r][c] = m.matrix[r][c], m.matrix[c][r]
		}
	}
}

// MapVector4 applies the matrix to vec in place.
func (m *Matrix44) MapVector4(vec *[4]float64) {
	v := double4(*vec)
	r0 := double4{m.matrix[0][0], m.matrix[1][0], m.matrix[2][0], m.matrix[3][0]}
	r1 := double4{m.matrix[0][1], m.matrix[1][1], m.matrix[2][1], m.matrix[3][1]}
	r2 := double4{m.matrix[0][2], m.matrix[1][2], m.matrix[2][2], m.matrix[3][2]}
	r3 := double4{m.matrix[0][3], m.matrix[1][3], m.matrix[2][3], m.matrix[3][3]}
	*vec = [4]float64{r0.mul(v).sum(), r1.mul(v).sum(), r2.mul(v).sum(), r3.mul(v).sum()}
}

// MapVector2 maps (vec[0], vec[1], 0, 1), stores x and y back into vec and
// returns w.
func (m *Matrix44) MapVector2(vec *[2]float64) float64 {
	v0, v1 := vec[0], vec[1]
	x := v0*m.matrix[0][0] + v1*m.matrix[1][0] + m.matrix[3][0]
	y := v0*m.matrix[0][1] + v1*m.matrix[1][1] + m.matrix[3][1]
	w := v0*m.matrix[0][3] + v1*m.matrix[1][3] + m.matrix[3][3]
	vec[0], vec[1] = x, y
	return w
}

// Flatten zeroes the third row and column except rc(2,2) = 1.
func (m *Matrix44) Flatten() {
	m.matrix[0][2] = 0
	m.matrix[1][2] = 0
	m.matrix[3][2] = 0
	m.setCol(2, double4{0, 0, 1, 0})
}
