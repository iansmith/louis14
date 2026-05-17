package filters

import (
	"image"
	"math"
)

// FESpecularLighting mirrors Blink's FESpecularLighting
// (platform/graphics/filters/fe_specular_lighting.{h,cc}).
//
// One-input primitive. Like FEDiffuseLighting it consumes the
// height-field "alpha bump" and a LightSource, but uses the Phong
// specular BRDF instead of Lambert. The halfway vector H is the unit
// bisector of the light direction L and the eye direction V (V is
// (0, 0, 1) — viewer at infinity above the surface, per SVG Filter
// Effects 1):
//
//	H = normalize(L + V) where V = (0, 0, 1)
//	I = SpecularConstant * max(N · H, 0)^SpecularExponent * Visibility
//	R_out = clamp(I * LightingColor.R, 0, 255)   (same for G, B)
//	A_out = max(R_out, G_out, B_out)             (spec rule!)
//
// IMPORTANT: per SVG Filter Effects 1 §"feSpecularLighting" the output
// alpha is `max(R_out, G_out, B_out)` — NOT LightingColor.A. This is
// the key behavioural difference from FEDiffuseLighting and is asserted
// by the SVG spec to make specular highlights compose correctly when
// the result is composited over other content.
//
// Field defaults per SVG Filter Effects 1:
//   - SpecularConstant: 1
//   - SpecularExponent: 1
//   - SurfaceScale:     1 (on the embedded feLighting)
//   - KernelUnitLength: 1, 1
//   - LightingColor:    white (1, 1, 1, 1)
type FESpecularLighting struct {
	feLighting
	// SpecularConstant is the Phong reflectance term k_s. Spec default
	// 1. Negative values are clamped at the byte cast (mirrors Blink).
	SpecularConstant float64
	// SpecularExponent controls highlight sharpness. Spec default 1.
	// Range per spec: [1, 128]; values outside are accepted but the
	// clamp is the implementation's choice. Blink clamps to >= 1.0
	// (any value < 1.0 is set to 1.0 — see Blink fe_specular_lighting.cc).
	SpecularExponent float64
}

// NewFESpecularLighting constructs a specular-lighting effect.
func NewFESpecularLighting(in FilterEffect, space InterpolationSpace,
	lightingColorR, lightingColorG, lightingColorB uint8, lightingColorA float64,
	surfaceScale, specularConstant, specularExponent, kernelUnitLengthX, kernelUnitLengthY float64,
	lightSource LightSource) *FESpecularLighting {
	// Blink clamps SpecularExponent below at 1.0 — replicate.
	if specularExponent < 1.0 {
		specularExponent = 1.0
	}
	return &FESpecularLighting{
		feLighting: feLighting{
			baseEffect:        baseEffect{inputs: []FilterEffect{in}, space: space},
			LightingColorR:    lightingColorR,
			LightingColorG:    lightingColorG,
			LightingColorB:    lightingColorB,
			LightingColorA:    lightingColorA,
			SurfaceScale:      surfaceScale,
			KernelUnitLengthX: kernelUnitLengthX,
			KernelUnitLengthY: kernelUnitLengthY,
			LightSource:       lightSource,
		},
		SpecularConstant: specularConstant,
		SpecularExponent: specularExponent,
	}
}

// ApplyEffect computes the specular-lighting output. Implements the
// per-pixel Phong BRDF described on the type.
func (e *FESpecularLighting) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	out := newRGBA(region)
	if e.LightSource == nil {
		return out
	}
	src := firstInput(inputs, region)
	w, h := region.Dx(), region.Dy()
	if w <= 0 || h <= 0 {
		return out
	}

	sample := alphaSampler(src.Pix, src.Stride)

	lcR := float64(e.LightingColorR) / 255.0
	lcG := float64(e.LightingColorG) / 255.0
	lcB := float64(e.LightingColorB) / 255.0

	for y := 0; y < h; y++ {
		outRow := y * out.Stride
		for x := 0; x < w; x++ {
			z := e.SurfaceScale * sample(x, y)
			Nx, Ny, Nz := e.computeNormal(sample, x, y, w, h)
			Lx, Ly, Lz, vis := e.LightSource.Direction(
				float64(region.Min.X)+float64(x)+0.5,
				float64(region.Min.Y)+float64(y)+0.5,
				z,
			)

			// Halfway vector H = normalize(L + V) where V = (0, 0, 1).
			Hx := Lx
			Hy := Ly
			Hz := Lz + 1
			mag := math.Sqrt(Hx*Hx + Hy*Hy + Hz*Hz)
			if mag != 0 {
				inv := 1.0 / mag
				Hx, Hy, Hz = Hx*inv, Hy*inv, Hz*inv
			}

			NdotH := Nx*Hx + Ny*Hy + Nz*Hz
			if NdotH < 0 {
				NdotH = 0
			}
			// Avoid math.Pow when exponent == 1 (the bucket-J case).
			var coeff float64
			if e.SpecularExponent == 1.0 {
				coeff = NdotH
			} else {
				coeff = math.Pow(NdotH, e.SpecularExponent)
			}
			I := e.SpecularConstant * coeff * vis

			r := I * lcR
			g := I * lcG
			b := I * lcB
			// Clamp colour first.
			rByte := clampByte(r)
			gByte := clampByte(g)
			bByte := clampByte(b)
			// Specular alpha rule: A_out = max(R_out, G_out, B_out).
			// Then premultiply colours by alpha for the buffer's
			// premultiplied-RGBA storage convention. Because the
			// max-of-RGB equals at least one channel, the
			// premultiplied bytes equal the originals for that
			// channel; for the others they shrink to <= the alpha.
			aByte := rByte
			if gByte > aByte {
				aByte = gByte
			}
			if bByte > aByte {
				aByte = bByte
			}
			// Premultiply (Skia + image.RGBA store premultiplied).
			if aByte == 0 {
				out.Pix[outRow+x*4+0] = 0
				out.Pix[outRow+x*4+1] = 0
				out.Pix[outRow+x*4+2] = 0
				out.Pix[outRow+x*4+3] = 0
			} else if aByte == 255 {
				out.Pix[outRow+x*4+0] = rByte
				out.Pix[outRow+x*4+1] = gByte
				out.Pix[outRow+x*4+2] = bByte
				out.Pix[outRow+x*4+3] = 255
			} else {
				af := float64(aByte) / 255.0
				out.Pix[outRow+x*4+0] = uint8(float64(rByte)*af + 0.5)
				out.Pix[outRow+x*4+1] = uint8(float64(gByte)*af + 0.5)
				out.Pix[outRow+x*4+2] = uint8(float64(bByte)*af + 0.5)
				out.Pix[outRow+x*4+3] = aByte
			}
		}
	}
	return out
}
