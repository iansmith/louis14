package filters

import "image"

// CompositeOperator selects the per-pixel composite operation, mirroring
// Blink's CompositeOperationType enum (fe_composite.h:38). Names use
// louis14's Go-idiomatic style; values map 1-1 to Blink's
// FECOMPOSITE_OPERATOR_OVER / IN / OUT / ATOP / XOR / ARITHMETIC.
//
// Per-pixel formulas, with i1 = primary input (`in`) and i2 = secondary
// (`in2`), both premultiplied, channels in [0, 1]:
//
//	over:       out = i1 + i2 * (1 - Sa)
//	in:         out = i1 * Da
//	out:        out = i1 * (1 - Da)
//	atop:       out = i1 * Da + i2 * (1 - Sa)
//	xor:        out = i1 * (1 - Da) + i2 * (1 - Sa)
//	arithmetic: outC = k1*C1*C2 + k2*C1 + k3*C2 + k4  (unpremultiplied)
//	            outA = k1*A1*A2 + k2*A1 + k3*A2 + k4
//
// Output is clamped to [0, 1] before re-premultiplication.
type CompositeOperator int

const (
	// CompositeOver: source over destination. Default per SVG spec.
	CompositeOver CompositeOperator = iota
	// CompositeIn: source restricted to destination's alpha extent.
	CompositeIn
	// CompositeOut: source outside destination's alpha extent.
	CompositeOut
	// CompositeAtop: source on top, restricted to destination extent.
	CompositeAtop
	// CompositeXor: source XOR destination — both vanish where they
	// overlap, both visible where they don't.
	CompositeXor
	// CompositeArithmetic: linear combination per spec formula.
	CompositeArithmetic
)

// FEComposite composites two inputs per the selected operator. Mirrors
// platform/graphics/filters/fe_composite.{h,cc}. inputs[0] is the
// primary (`in`); inputs[1] is the secondary (`in2`).
//
// All Porter-Duff operators run on premultiplied bytes directly — this
// is safe because the spec formulas are linear in premultiplied space
// for over/in/out/atop/xor (Skia's XfermodePaintFilter takes the same
// fast path).
//
// The arithmetic operator follows Blink's
// ArithmeticPaintFilter / Skia SkArithmeticImageFilter behaviour: the
// formula is applied to non-premultiplied channels, then the result is
// re-premultiplied. With `pm_color_validation = true` (Skia's default
// for filter chains feeding back into Porter-Duff stages) the output is
// clamped so RGB ≤ A — the same clamp Blink achieves via
// `MayProduceInvalidPreMultipliedPixels = true`.
type FEComposite struct {
	baseEffect
	Operator       CompositeOperator
	K1, K2, K3, K4 float64
}

// NewFEComposite builds an FEComposite from its two inputs, operator
// and arithmetic constants. Mirrors FEComposite::FEComposite
// (fe_composite.cc:36-40).
func NewFEComposite(in1, in2 FilterEffect, space InterpolationSpace,
	op CompositeOperator, k1, k2, k3, k4 float64) *FEComposite {
	return &FEComposite{
		baseEffect: baseEffect{inputs: []FilterEffect{in1, in2}, space: space},
		Operator:   op,
		K1:         k1, K2: k2, K3: k3, K4: k4,
	}
}

// ApplyEffect runs the per-pixel composite.
func (e *FEComposite) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	var i1, i2 *image.RGBA
	if len(inputs) > 0 {
		i1 = inputs[0]
	}
	if len(inputs) > 1 {
		i2 = inputs[1]
	}
	if i1 == nil {
		i1 = newRGBA(region)
	}
	if i2 == nil {
		i2 = newRGBA(region)
	}
	out := newRGBA(region)
	p1 := i1.Pix
	p2 := i2.Pix
	po := out.Pix
	n := len(po)
	if len(p1) < n {
		n = len(p1)
	}
	if len(p2) < n {
		n = len(p2)
	}

	if e.Operator == CompositeArithmetic {
		e.applyArithmetic(p1, p2, po, n)
		return out
	}
	e.applyPorterDuff(p1, p2, po, n)
	return out
}

// applyPorterDuff runs the operator on premultiplied byte channels. The
// integer-domain formulas mirror Skia's SkXfermodes for the 5 Porter-Duff
// operators feComposite exposes. All math is in 0–255 space using the
// fast premultiplied-byte multiplier `(a*b + 127) / 255` (round-half-up).
//
// Per-pixel sums are guaranteed to stay within 0..255 mathematically;
// the explicit u8 cast at the end clamps any rounding overshoot.
func (e *FEComposite) applyPorterDuff(p1, p2, po []uint8, n int) {
	// mul: round-half-up premultiplied-byte product = round(a*b/255).
	mul := func(a, b uint32) uint32 {
		return (a*b + 127) / 255
	}
	clampU8 := func(v uint32) uint8 {
		if v > 255 {
			return 255
		}
		return uint8(v)
	}
	for i := 0; i+3 < n; i += 4 {
		s0 := uint32(p1[i])
		s1 := uint32(p1[i+1])
		s2 := uint32(p1[i+2])
		sa := uint32(p1[i+3])
		d0 := uint32(p2[i])
		d1 := uint32(p2[i+1])
		d2 := uint32(p2[i+2])
		da := uint32(p2[i+3])
		switch e.Operator {
		case CompositeOver:
			// out = i1 + i2 * (1 - Sa).
			inv := 255 - sa
			po[i] = clampU8(s0 + mul(d0, inv))
			po[i+1] = clampU8(s1 + mul(d1, inv))
			po[i+2] = clampU8(s2 + mul(d2, inv))
			po[i+3] = clampU8(sa + mul(da, inv))
		case CompositeIn:
			// out = i1 * Da.
			po[i] = clampU8(mul(s0, da))
			po[i+1] = clampU8(mul(s1, da))
			po[i+2] = clampU8(mul(s2, da))
			po[i+3] = clampU8(mul(sa, da))
		case CompositeOut:
			// out = i1 * (1 - Da).
			inv := 255 - da
			po[i] = clampU8(mul(s0, inv))
			po[i+1] = clampU8(mul(s1, inv))
			po[i+2] = clampU8(mul(s2, inv))
			po[i+3] = clampU8(mul(sa, inv))
		case CompositeAtop:
			// out = i1*Da + i2*(1 - Sa).
			invS := 255 - sa
			po[i] = clampU8(mul(s0, da) + mul(d0, invS))
			po[i+1] = clampU8(mul(s1, da) + mul(d1, invS))
			po[i+2] = clampU8(mul(s2, da) + mul(d2, invS))
			po[i+3] = clampU8(mul(sa, da) + mul(da, invS))
		case CompositeXor:
			// out = i1*(1 - Da) + i2*(1 - Sa).
			invS := 255 - sa
			invD := 255 - da
			po[i] = clampU8(mul(s0, invD) + mul(d0, invS))
			po[i+1] = clampU8(mul(s1, invD) + mul(d1, invS))
			po[i+2] = clampU8(mul(s2, invD) + mul(d2, invS))
			po[i+3] = clampU8(mul(sa, invD) + mul(da, invS))
		default:
			// Unhandled — pass-through i1 as a safe default.
			po[i] = p1[i]
			po[i+1] = p1[i+1]
			po[i+2] = p1[i+2]
			po[i+3] = p1[i+3]
		}
	}
}

// applyArithmetic runs the SVG arithmetic operator. Per spec the formula
//
//	result = k1 * i1 * i2 + k2 * i1 + k3 * i2 + k4
//
// operates on non-premultiplied channels in [0, 1]. We un-premultiply
// both inputs (alpha-aware divide), apply the formula per channel
// including alpha, clamp to [0, 1], and re-premultiply for storage.
//
// Mirrors Skia ArithmeticImageFilter / SkArithmeticImageFilter — which
// is the implementation Blink hands off to via ArithmeticPaintFilter.
// In particular, the per-channel formula treats alpha as just another
// channel (i.e. there is no "alpha is special" carve-out), and the
// output is clamped before re-premultiplication so RGB ≤ A in the
// stored bytes.
func (e *FEComposite) applyArithmetic(p1, p2, po []uint8, n int) {
	k1, k2, k3, k4 := e.K1, e.K2, e.K3, e.K4
	clamp01 := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	to8 := func(v float64) uint8 { return uint8(v*255.0 + 0.5) }

	for i := 0; i+3 < n; i += 4 {
		r1, g1, b1, a1 := unpremultiply([4]uint8{p1[i], p1[i+1], p1[i+2], p1[i+3]})
		r2, g2, b2, a2 := unpremultiply([4]uint8{p2[i], p2[i+1], p2[i+2], p2[i+3]})
		nr := k1*r1*r2 + k2*r1 + k3*r2 + k4
		ng := k1*g1*g2 + k2*g1 + k3*g2 + k4
		nb := k1*b1*b2 + k2*b1 + k3*b2 + k4
		na := k1*a1*a2 + k2*a1 + k3*a2 + k4
		na = clamp01(na)
		nr = clamp01(nr)
		ng = clamp01(ng)
		nb = clamp01(nb)
		// Re-premultiply for storage. Clamp RGB ≤ A so the stored
		// bytes remain valid premultiplied form (Skia's
		// pm_color_validation step).
		if nr > na {
			nr = na
		}
		if ng > na {
			ng = na
		}
		if nb > na {
			nb = na
		}
		po[i] = to8(nr * na)
		po[i+1] = to8(ng * na)
		po[i+2] = to8(nb * na)
		po[i+3] = to8(na)
	}
}
