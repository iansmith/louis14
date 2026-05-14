package filters

import (
	"image"
	"math"
)

// ColorMatrixType selects how FEColorMatrix builds its 5x4 matrix, mirroring
// Blink's ColorMatrixType (fe_color_matrix.h).
type ColorMatrixType int

const (
	// ColorMatrixTypeMatrix uses the 20 Values directly.
	ColorMatrixTypeMatrix ColorMatrixType = iota
	// ColorMatrixTypeSaturate builds the saturation matrix from Values[0].
	ColorMatrixTypeSaturate
	// ColorMatrixTypeHueRotate builds the hue-rotation matrix from Values[0]
	// (the angle in degrees).
	ColorMatrixTypeHueRotate
	// ColorMatrixTypeLuminanceToAlpha maps luminance to alpha, RGB to 0.
	ColorMatrixTypeLuminanceToAlpha
)

// FEColorMatrix applies a 5x4 colour matrix to non-premultiplied RGBA. It
// backs the grayscale/sepia (MATRIX), saturate (SATURATE) and hue-rotate
// (HUEROTATE) CSS shorthand functions and the SVG feColorMatrix primitive.
// Mirrors platform/graphics/filters/fe_color_matrix.cc.
type FEColorMatrix struct {
	baseEffect
	Type   ColorMatrixType
	Values []float64 // 20 entries for MATRIX; 1 (amount/angle) for SATURATE/HUEROTATE
}

// NewFEColorMatrix creates a colour-matrix effect with the given input.
func NewFEColorMatrix(input FilterEffect, space InterpolationSpace, t ColorMatrixType, values []float64) *FEColorMatrix {
	return &FEColorMatrix{
		baseEffect: baseEffect{inputs: []FilterEffect{input}, space: space},
		Type:       t,
		Values:     values,
	}
}

// resolveMatrix returns the concrete 5x4 matrix (20 values, row-major: each
// row is r,g,b,a,bias) for this effect's type, following SVG Filter Effects
// §feColorMatrix and Blink's FEColorMatrix matrix builders.
func (e *FEColorMatrix) resolveMatrix() [20]float64 {
	var m [20]float64
	switch e.Type {
	case ColorMatrixTypeMatrix:
		for i := 0; i < 20 && i < len(e.Values); i++ {
			m[i] = e.Values[i]
		}
	case ColorMatrixTypeSaturate:
		s := 0.0
		if len(e.Values) > 0 {
			s = e.Values[0]
		}
		// Coefficients per fe_color_matrix.cc / SVG spec.
		m = [20]float64{
			0.213 + 0.787*s, 0.715 - 0.715*s, 0.072 - 0.072*s, 0, 0,
			0.213 - 0.213*s, 0.715 + 0.285*s, 0.072 - 0.072*s, 0, 0,
			0.213 - 0.213*s, 0.715 - 0.715*s, 0.072 + 0.928*s, 0, 0,
			0, 0, 0, 1, 0,
		}
	case ColorMatrixTypeHueRotate:
		deg := 0.0
		if len(e.Values) > 0 {
			deg = e.Values[0]
		}
		rad := deg * math.Pi / 180.0
		c := math.Cos(rad)
		s := math.Sin(rad)
		m = [20]float64{
			0.213 + c*0.787 - s*0.213, 0.715 - c*0.715 - s*0.715, 0.072 - c*0.072 + s*0.928, 0, 0,
			0.213 - c*0.213 + s*0.143, 0.715 + c*0.285 + s*0.140, 0.072 - c*0.072 - s*0.283, 0, 0,
			0.213 - c*0.213 - s*0.787, 0.715 - c*0.715 + s*0.715, 0.072 + c*0.928 + s*0.072, 0, 0,
			0, 0, 0, 1, 0,
		}
	case ColorMatrixTypeLuminanceToAlpha:
		m = [20]float64{
			0, 0, 0, 0, 0,
			0, 0, 0, 0, 0,
			0, 0, 0, 0, 0,
			0.2125, 0.7154, 0.0721, 0, 0,
		}
	}
	return m
}

// ApplyEffect transforms each pixel's non-premultiplied RGBA by the matrix.
func (e *FEColorMatrix) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	m := e.resolveMatrix()
	out := newRGBA(region)
	pix := in.Pix
	op := out.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		if pix[i+3] == 0 {
			// Fully transparent input: a matrix with no bias keeps it
			// transparent. Honour any constant bias terms anyway.
			r := m[4]
			g := m[9]
			b := m[14]
			a := m[19]
			res := premultiply(r, g, b, a)
			op[i], op[i+1], op[i+2], op[i+3] = res[0], res[1], res[2], res[3]
			continue
		}
		r, g, b, a := unpremultiply([4]uint8{pix[i], pix[i+1], pix[i+2], pix[i+3]})
		nr := m[0]*r + m[1]*g + m[2]*b + m[3]*a + m[4]
		ng := m[5]*r + m[6]*g + m[7]*b + m[8]*a + m[9]
		nb := m[10]*r + m[11]*g + m[12]*b + m[13]*a + m[14]
		na := m[15]*r + m[16]*g + m[17]*b + m[18]*a + m[19]
		res := premultiply(nr, ng, nb, na)
		op[i], op[i+1], op[i+2], op[i+3] = res[0], res[1], res[2], res[3]
	}
	return out
}
