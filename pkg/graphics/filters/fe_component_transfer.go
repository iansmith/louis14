package filters

import (
	"image"
	"math"
)

// TransferType selects a component-transfer function shape, mirroring Blink's
// ComponentTransferType (fe_component_transfer.h) and SVG's feFunc* element.
type TransferType int

const (
	// TransferIdentity passes the component through unchanged.
	TransferIdentity TransferType = iota
	// TransferTable interpolates linearly through TableValues.
	TransferTable
	// TransferDiscrete steps through TableValues.
	TransferDiscrete
	// TransferLinear computes slope*c + intercept.
	TransferLinear
	// TransferGamma computes amplitude*c^exponent + offset.
	TransferGamma
)

// TransferFunc is one channel's transfer function (the SVG feFuncR/G/B/A
// element). Mirrors Blink's ComponentTransferFunction.
type TransferFunc struct {
	Type        TransferType
	TableValues []float64
	Slope       float64
	Intercept   float64
	Amplitude   float64
	Exponent    float64
	Offset      float64
}

// eval applies the function to a [0,1] component value.
func (t TransferFunc) eval(c float64) float64 {
	switch t.Type {
	case TransferTable:
		n := len(t.TableValues)
		if n == 0 {
			return c
		}
		if n == 1 {
			return t.TableValues[0]
		}
		if c >= 1 {
			return t.TableValues[n-1]
		}
		if c < 0 {
			c = 0
		}
		k := int(c * float64(n-1))
		if k >= n-1 {
			k = n - 2
		}
		// v_k + (c - k/(n-1)) * (n-1) * (v_{k+1} - v_k)
		frac := c*float64(n-1) - float64(k)
		return t.TableValues[k] + frac*(t.TableValues[k+1]-t.TableValues[k])
	case TransferDiscrete:
		n := len(t.TableValues)
		if n == 0 {
			return c
		}
		if c >= 1 {
			return t.TableValues[n-1]
		}
		if c < 0 {
			c = 0
		}
		k := int(c * float64(n))
		if k >= n {
			k = n - 1
		}
		return t.TableValues[k]
	case TransferLinear:
		return t.Slope*c + t.Intercept
	case TransferGamma:
		return t.Amplitude*math.Pow(c, t.Exponent) + t.Offset
	default: // TransferIdentity
		return c
	}
}

// FEComponentTransfer applies a per-channel transfer function to
// non-premultiplied RGBA. It backs the invert/opacity/brightness/contrast CSS
// shorthand functions and the SVG feComponentTransfer primitive.
// Mirrors platform/graphics/filters/fe_component_transfer.cc.
type FEComponentTransfer struct {
	baseEffect
	Funcs [4]TransferFunc // R, G, B, A
}

// NewFEComponentTransfer creates a component-transfer effect.
func NewFEComponentTransfer(input FilterEffect, space InterpolationSpace, funcs [4]TransferFunc) *FEComponentTransfer {
	return &FEComponentTransfer{
		baseEffect: baseEffect{inputs: []FilterEffect{input}, space: space},
		Funcs:      funcs,
	}
}

// ApplyEffect runs each channel through its transfer function.
func (e *FEComponentTransfer) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	out := newRGBA(region)
	pix := in.Pix
	op := out.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		var r, g, b, a float64
		if pix[i+3] == 0 {
			// feComponentTransfer is defined on non-premultiplied colour;
			// a transparent pixel has colour 0 but its transfer functions
			// can still raise alpha (e.g. invert/brightness on A).
			r, g, b, a = 0, 0, 0, 0
		} else {
			r, g, b, a = unpremultiply([4]uint8{pix[i], pix[i+1], pix[i+2], pix[i+3]})
		}
		nr := e.Funcs[0].eval(r)
		ng := e.Funcs[1].eval(g)
		nb := e.Funcs[2].eval(b)
		na := e.Funcs[3].eval(a)
		res := premultiply(nr, ng, nb, na)
		op[i], op[i+1], op[i+2], op[i+3] = res[0], res[1], res[2], res[3]
	}
	return out
}
