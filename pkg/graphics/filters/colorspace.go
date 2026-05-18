// Package filters implements the FilterEffect graph that backs CSS filter
// functions, backdrop-filter, and SVG <filter> references. It mirrors Blink's
// platform/graphics/filters/ subsystem: a Filter owns a graph of FilterEffect
// nodes (one per fe* primitive / CSS shorthand equivalent), each of which
// declares the interpolation space it operates in and produces an image from
// its input effects.
package filters

import (
	"image"
	"math"
)

// InterpolationSpace is the color space a FilterEffect operates in. CSS
// shorthand filter functions run in sRGB; SVG <filter> primitives default to
// linearRGB (the SVG color-interpolation-filters initial value). This mirrors
// Blink's FilterEffect::operating_interpolation_space_.
type InterpolationSpace int

const (
	// InterpolationSpaceSRGB is the gamma-encoded sRGB space.
	InterpolationSpaceSRGB InterpolationSpace = iota
	// InterpolationSpaceLinearRGB is the linear-light RGB space.
	InterpolationSpaceLinearRGB
)

// sRGB <-> linearRGB transfer functions. These are the exact piecewise
// transfers used by Blink (platform/graphics/color.cc): the 0.04045 / 0.0031308
// thresholds and the 2.4 gamma.

// srgbToLinear converts a single [0,1] sRGB component to linear light.
func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// linearToSRGB converts a single [0,1] linear-light component to sRGB.
func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// 256-entry lookup tables keyed by the 8-bit encoded component value. The
// forward table (srgbToLinearLUT) is exact for byte input; the inverse table
// is sampled at byte granularity and used only where a fast approximate path
// is acceptable — the per-pixel conversion below uses the precise float path.
var (
	srgbToLinearLUT [256]float64
	linearToSRGBLUT [256]float64
)

func init() {
	for i := 0; i < 256; i++ {
		c := float64(i) / 255.0
		srgbToLinearLUT[i] = srgbToLinear(c)
		linearToSRGBLUT[i] = linearToSRGB(c)
	}
}

// unpremultiply converts a premultiplied 8-bit RGBA pixel (Go's image.RGBA
// storage convention) to non-premultiplied [0,1] float components. Mirrors the
// un-premultiply step every Skia color filter performs before transforming.
func unpremultiply(px [4]uint8) (r, g, b, a float64) {
	a = float64(px[3]) / 255.0
	if a == 0 {
		return 0, 0, 0, 0
	}
	r = float64(px[0]) / 255.0 / a
	g = float64(px[1]) / 255.0 / a
	b = float64(px[2]) / 255.0 / a
	return r, g, b, a
}

// premultiply converts non-premultiplied [0,1] float components back to a
// premultiplied 8-bit RGBA pixel, clamping to the valid range.
func premultiply(r, g, b, a float64) [4]uint8 {
	clamp01 := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	a = clamp01(a)
	r = clamp01(r)
	g = clamp01(g)
	b = clamp01(b)
	to8 := func(v float64) uint8 { return uint8(v*255.0 + 0.5) }
	return [4]uint8{
		to8(r * a),
		to8(g * a),
		to8(b * a),
		to8(a),
	}
}

// convertImageSpace converts an image in place between interpolation spaces.
// The conversion un-premultiplies, applies the transfer function to the colour
// channels (alpha is space-independent), and re-premultiplies. It is a no-op
// when from == to. This establishes the invariant that every filter primitive
// works in its declared space with the graph edges doing the conversions.
func convertImageSpace(img *image.RGBA, from, to InterpolationSpace) {
	if from == to || img == nil {
		return
	}
	pix := img.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		a := pix[i+3]
		if a == 0 {
			continue
		}
		r, g, b, af := unpremultiply([4]uint8{pix[i], pix[i+1], pix[i+2], a})
		switch {
		case from == InterpolationSpaceSRGB && to == InterpolationSpaceLinearRGB:
			r = srgbToLinear(r)
			g = srgbToLinear(g)
			b = srgbToLinear(b)
		case from == InterpolationSpaceLinearRGB && to == InterpolationSpaceSRGB:
			r = linearToSRGB(r)
			g = linearToSRGB(g)
			b = linearToSRGB(b)
		}
		out := premultiply(r, g, b, af)
		pix[i], pix[i+1], pix[i+2], pix[i+3] = out[0], out[1], out[2], out[3]
	}
}
