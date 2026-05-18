package filters

import "image"

// BlendMode selects an feBlend blending mode, mirroring Blink's
// BlendMode used by platform/graphics/filters/fe_blend.cc / the
// SVGFEBlendElement enum.
type BlendMode int

const (
	// BlendModeNormal: cr = (1-qa)*cb + ca.
	BlendModeNormal BlendMode = iota
	// BlendModeMultiply: cr = (1-qa)*cb + (1-qb)*ca + ca*cb.
	BlendModeMultiply
	// BlendModeScreen: cr = cb + ca - ca*cb.
	BlendModeScreen
	// BlendModeDarken: cr = min((1-qa)*cb + ca, (1-qb)*ca + cb).
	BlendModeDarken
	// BlendModeLighten: cr = max((1-qa)*cb + ca, (1-qb)*ca + cb).
	BlendModeLighten
)

// FEBlend implements SVG Filter Effects 1 §feBlend. It composites two
// inputs (in over in2) per the selected blend mode. inputs[0] is `in`
// (top layer); inputs[1] is `in2` (bottom layer).
//
// Output alpha (all modes): qr = 1 - (1-qa)*(1-qb).
//
// All input pixels are premultiplied image.RGBA. The blend math is
// performed on premultiplied floats per the SVG spec.
//
// Mirrors platform/graphics/filters/fe_blend.cc.
type FEBlend struct {
	baseEffect
	Mode BlendMode
}

// NewFEBlend creates a blend effect with the two given inputs (in, in2)
// in argument order matching SVG feBlend semantics: in is the top
// layer, in2 is the bottom layer.
func NewFEBlend(in, in2 FilterEffect, space InterpolationSpace, mode BlendMode) *FEBlend {
	return &FEBlend{
		baseEffect: baseEffect{inputs: []FilterEffect{in, in2}, space: space},
		Mode:       mode,
	}
}

// ApplyEffect composites in over in2 per the blend mode.
func (e *FEBlend) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	var ina, inb *image.RGBA
	if len(inputs) > 0 {
		ina = inputs[0]
	}
	if len(inputs) > 1 {
		inb = inputs[1]
	}
	if ina == nil {
		ina = newRGBA(region)
	}
	if inb == nil {
		inb = newRGBA(region)
	}
	out := newRGBA(region)
	pa := ina.Pix
	pb := inb.Pix
	po := out.Pix
	n := len(po)
	if len(pa) < n {
		n = len(pa)
	}
	if len(pb) < n {
		n = len(pb)
	}
	for i := 0; i+3 < n; i += 4 {
		// Premultiplied source values as floats in [0,1].
		ca := [3]float64{
			float64(pa[i+0]) / 255.0,
			float64(pa[i+1]) / 255.0,
			float64(pa[i+2]) / 255.0,
		}
		qa := float64(pa[i+3]) / 255.0
		cb := [3]float64{
			float64(pb[i+0]) / 255.0,
			float64(pb[i+1]) / 255.0,
			float64(pb[i+2]) / 255.0,
		}
		qb := float64(pb[i+3]) / 255.0
		// Output alpha: qr = 1 - (1-qa)*(1-qb).
		qr := 1.0 - (1.0-qa)*(1.0-qb)
		var cr [3]float64
		switch e.Mode {
		case BlendModeNormal:
			// cr = (1-qa)*cb + ca.
			for k := 0; k < 3; k++ {
				cr[k] = (1.0-qa)*cb[k] + ca[k]
			}
		case BlendModeMultiply:
			// cr = (1-qa)*cb + (1-qb)*ca + ca*cb.
			for k := 0; k < 3; k++ {
				cr[k] = (1.0-qa)*cb[k] + (1.0-qb)*ca[k] + ca[k]*cb[k]
			}
		case BlendModeScreen:
			// cr = cb + ca - ca*cb. (premultiplied)
			for k := 0; k < 3; k++ {
				cr[k] = cb[k] + ca[k] - ca[k]*cb[k]
			}
		case BlendModeDarken:
			for k := 0; k < 3; k++ {
				a := (1.0-qa)*cb[k] + ca[k]
				b := (1.0-qb)*ca[k] + cb[k]
				if a < b {
					cr[k] = a
				} else {
					cr[k] = b
				}
			}
		case BlendModeLighten:
			for k := 0; k < 3; k++ {
				a := (1.0-qa)*cb[k] + ca[k]
				b := (1.0-qb)*ca[k] + cb[k]
				if a > b {
					cr[k] = a
				} else {
					cr[k] = b
				}
			}
		}
		// Clamp + write back as 8-bit premultiplied bytes.
		clamp := func(v float64) uint8 {
			if v < 0 {
				return 0
			}
			if v > 1 {
				return 255
			}
			return uint8(v*255.0 + 0.5)
		}
		po[i+0] = clamp(cr[0])
		po[i+1] = clamp(cr[1])
		po[i+2] = clamp(cr[2])
		po[i+3] = clamp(qr)
	}
	return out
}
