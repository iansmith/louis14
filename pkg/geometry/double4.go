package geometry

// double4 is a 4-lane double vector, the Go stand-in for gfx::Double4
// (ui/gfx/geometry/double4.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// which Chromium implements with clang vector extensions. Operators are
// element-wise, as in Chromium.
type double4 [4]float64

func (a double4) add(b double4) double4 {
	return double4{a[0] + b[0], a[1] + b[1], a[2] + b[2], a[3] + b[3]}
}

func (a double4) sub(b double4) double4 {
	return double4{a[0] - b[0], a[1] - b[1], a[2] - b[2], a[3] - b[3]}
}

func (a double4) mul(b double4) double4 {
	return double4{a[0] * b[0], a[1] * b[1], a[2] * b[2], a[3] * b[3]}
}

func (a double4) scale(s float64) double4 {
	return double4{a[0] * s, a[1] * s, a[2] * s, a[3] * s}
}

// sum mirrors gfx::Sum(Double4).
func (a double4) sum() float64 {
	return a[0] + a[1] + a[2] + a[3]
}

// swapHighLow mirrors the anonymous SwapHighLow in matrix44.cc.
func (a double4) swapHighLow() double4 {
	return double4{a[2], a[3], a[0], a[1]}
}

// swapInPairs mirrors the anonymous SwapInPairs in matrix44.cc.
func (a double4) swapInPairs() double4 {
	return double4{a[1], a[0], a[3], a[2]}
}
