package filters

import "image"

// FEDiffuseLighting mirrors Blink's FEDiffuseLighting
// (platform/graphics/filters/fe_diffuse_lighting.{h,cc}).
//
// One-input primitive. inputs[0] is the height-field "alpha bump":
// each pixel's alpha channel encodes the surface elevation z =
// SurfaceScale * alpha / 255. The Sobel-derived normal N at that
// pixel + the LightSource's direction L feed Lambert's law:
//
//	I = DiffuseConstant * max(N · L, 0) * Visibility
//	R_out = clamp(I * LightingColor.R, 0, 255)   (same for G, B)
//	A_out = LightingColor.A * 255
//
// Per SVG Filter Effects 1 §"feDiffuseLighting": the diffuse BRDF is
// pure Lambert (no Fresnel, no specular component), the output alpha
// is the lighting-color alpha (NOT max(R,G,B) — that's specular's
// rule). The colour-interpolation-filters CSS property selects the
// space the BRDF runs in; the graph evaluator (Filter.evaluate) has
// already converted inputs to OperatingSpace, so the BRDF math here
// is space-agnostic.
//
// Field defaults per SVG Filter Effects 1:
//   - DiffuseConstant: 1
//   - SurfaceScale:    1 (on the embedded feLighting)
//   - KernelUnitLength: 1, 1
//   - LightingColor:   white (1, 1, 1, 1)
type FEDiffuseLighting struct {
	feLighting
	// DiffuseConstant is the Lambert reflectance term k_d. Spec
	// default: 1. Negative values are non-spec but the spec leaves it
	// to the implementation to clamp; Blink clamps to >= 0 implicitly
	// via the BRDF (negative k_d * positive N·L = negative I → clamped
	// at the byte cast). louis14 follows Blink.
	DiffuseConstant float64
}

// NewFEDiffuseLighting constructs a diffuse-lighting effect.
func NewFEDiffuseLighting(in FilterEffect, space InterpolationSpace,
	lightingColorR, lightingColorG, lightingColorB uint8, lightingColorA float64,
	surfaceScale, diffuseConstant, kernelUnitLengthX, kernelUnitLengthY float64,
	lightSource LightSource) *FEDiffuseLighting {
	return &FEDiffuseLighting{
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
		DiffuseConstant: diffuseConstant,
	}
}

// ApplyEffect computes the diffuse-lighting output. Implements the
// per-pixel Lambert BRDF described on the type.
func (e *FEDiffuseLighting) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	out := newRGBA(region)
	if e.LightSource == nil {
		// Spec: no light source → no lighting → transparent black
		// output. newRGBA already zeroed; return as-is.
		return out
	}
	src := firstInput(inputs, region)
	w, h := region.Dx(), region.Dy()
	if w <= 0 || h <= 0 {
		return out
	}

	sample := alphaSampler(src.Pix, src.Stride)

	// Pre-scale lighting colour from byte to [0, 1] floats for the
	// BRDF multiply.
	lcR := float64(e.LightingColorR) / 255.0
	lcG := float64(e.LightingColorG) / 255.0
	lcB := float64(e.LightingColorB) / 255.0

	// Output alpha is LightingColorA per spec — constant across all
	// output pixels for diffuse.
	outA := e.LightingColorA
	if outA < 0 {
		outA = 0
	} else if outA > 1 {
		outA = 1
	}
	outAByte := uint8(outA*255 + 0.5)

	for y := 0; y < h; y++ {
		outRow := y * out.Stride
		for x := 0; x < w; x++ {
			// Surface point in absolute device pixels with z lifted
			// by alpha-bump (per spec). The (+0.5, +0.5) shifts to
			// the pixel centre, the same convention used by Skia's
			// shaders.
			z := e.SurfaceScale * sample(x, y)
			Nx, Ny, Nz := e.computeNormal(sample, x, y, w, h)
			Lx, Ly, Lz, vis := e.LightSource.Direction(
				float64(region.Min.X)+float64(x)+0.5,
				float64(region.Min.Y)+float64(y)+0.5,
				z,
			)

			NdotL := Nx*Lx + Ny*Ly + Nz*Lz
			if NdotL < 0 {
				NdotL = 0
			}
			I := e.DiffuseConstant * NdotL * vis

			r := I * lcR
			g := I * lcG
			b := I * lcB
			// Premultiply: out_alpha is constant, so R_out_premul =
			// clamp(I*lc, 0, 1) * outA. Skia writes premultiplied
			// buffers; image.RGBA storage is premultiplied.
			out.Pix[outRow+x*4+0] = clampByte(r * outA)
			out.Pix[outRow+x*4+1] = clampByte(g * outA)
			out.Pix[outRow+x*4+2] = clampByte(b * outA)
			out.Pix[outRow+x*4+3] = outAByte
		}
	}
	return out
}

// clampByte clamps a float in [0, 1] and rounds to a uint8.
func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}
