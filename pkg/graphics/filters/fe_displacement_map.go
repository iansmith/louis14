package filters

import (
	"image"
	"math"
)

// ChannelSelector mirrors Blink's ChannelSelectorType
// (platform/graphics/filters/fe_displacement_map.h:30-36). It is the
// enum used by `<feDisplacementMap>`'s `xChannelSelector` /
// `yChannelSelector` attributes to pick which channel of the
// displacement-map input drives each axis.
//
// Values match Blink:
//
//	CHANNEL_UNKNOWN = 0   // attribute missing or unrecognized; SVG default is A
//	CHANNEL_R       = 1
//	CHANNEL_G       = 2
//	CHANNEL_B       = 3
//	CHANNEL_A       = 4
type ChannelSelector uint8

const (
	// ChannelUnknown is the unset/unrecognized slot (Blink CHANNEL_UNKNOWN).
	ChannelUnknown ChannelSelector = iota
	// ChannelR selects the red channel.
	ChannelR
	// ChannelG selects the green channel.
	ChannelG
	// ChannelB selects the blue channel.
	ChannelB
	// ChannelA selects the alpha channel.
	ChannelA
)

// FEDisplacementMap mirrors Blink's FEDisplacementMap
// (platform/graphics/filters/fe_displacement_map.{h,cc}).
//
// Two-input primitive. inputs[0] supplies the source pixels (the image
// being displaced); inputs[1] supplies the displacement map (the image
// whose two selected channels per pixel encode the per-pixel offset).
// For each output pixel (x, y):
//
//	dx = (sample(map, x, y).chan_x / 255 - 0.5) * scale
//	dy = (sample(map, x, y).chan_y / 255 - 0.5) * scale
//	output[x, y] = sample(source, x + dx, y + dy)
//
// Per SVG Filter Effects 1 §"feDisplacementMap":
//   - Single `scale` value — NOT split X/Y (mirrors Blink's
//     single `float scale_` member; the spec only exposes one `scale`
//     attribute that applies to both axes).
//   - Out-of-bounds source sampling returns transparent black (NOT
//     clamp, NOT wrap, NOT mirror — matches Skia's `SkTileMode::kDecal`
//     used by `SkImageFilters::DisplacementMap`).
//   - The displacement map (input 2) is read with nearest-neighbour
//     sampling (it's data, not a renderable image).
//   - The source (input 1) is read with bilinear sampling so sub-pixel
//     displacements look smooth.
//   - Channel value: read the non-premultiplied byte at the selected
//     channel offset, convert to [0, 1] by dividing by 255, subtract
//     0.5, multiply by scale. Mirrors Skia's displacement map which
//     un-premultiplies before reading channels.
type FEDisplacementMap struct {
	baseEffect
	XChannel ChannelSelector
	YChannel ChannelSelector
	Scale    float64
}

// NewFEDisplacementMap creates a displacement-map effect.
// in1 is the source (the image to be displaced); in2 is the map (the
// image whose channels drive the displacement). xChan / yChan select
// which channel of `in2` drives each axis. SVG's spec default when an
// attribute is missing is `A`; the builder applies that — this ctor
// records whatever the caller passes.
func NewFEDisplacementMap(in1, in2 FilterEffect, space InterpolationSpace,
	xChan, yChan ChannelSelector, scale float64) *FEDisplacementMap {
	return &FEDisplacementMap{
		baseEffect: baseEffect{inputs: []FilterEffect{in1, in2}, space: space},
		XChannel:   xChan,
		YChannel:   yChan,
		Scale:      scale,
	}
}

// MapRect overrides the default pass-through. Because displacement can
// pull pixels from up to ±(|scale|/2) away on each axis, the output
// rect inflates by that amount. Mirrors Blink's
// FEDisplacementMap::MapEffect (fe_displacement_map.cc), which adds
// `scale * 0.5` to each side. `MapInputs` differs (only input 1
// inflates), but our graph's MapRect already pre-unions inputs through
// `mapRectThrough`, so the symmetric inflation is correct for the
// downstream bounds calculation.
func (e *FEDisplacementMap) MapRect(r image.Rectangle) image.Rectangle {
	half := math.Abs(e.Scale) * 0.5
	pad := int(math.Ceil(half))
	if pad <= 0 {
		return r
	}
	return image.Rect(r.Min.X-pad, r.Min.Y-pad, r.Max.X+pad, r.Max.Y+pad)
}

// channelByteOffset returns the byte offset within a 4-byte RGBA pixel
// for a given channel selector. The SVG spec says an unset attribute
// defaults to `A`; the builder is expected to convert ChannelUnknown
// to ChannelA before constructing the effect, but we also defend
// against it here (Unknown → A) for safety.
func channelByteOffset(c ChannelSelector) int {
	switch c {
	case ChannelR:
		return 0
	case ChannelG:
		return 1
	case ChannelB:
		return 2
	case ChannelA:
		return 3
	default:
		// ChannelUnknown → A per SVG spec default.
		return 3
	}
}

// ApplyEffect produces the displaced source. Both inputs are sized to
// `region` (every effect in the graph produces a region-sized buffer);
// the map is sampled at integer (x, y) and the source is sampled
// bilinearly at (x + dx, y + dy). Per the spec, out-of-bounds source
// samples return (0, 0, 0, 0).
//
// The displacement-map sample is un-premultiplied before reading the
// chosen channel byte, mirroring Skia's `SkDisplacementMapEffect`
// (which always un-premultiplies before sampling).
func (e *FEDisplacementMap) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	source := firstInput(inputs, region)
	var mapImg *image.RGBA
	if len(inputs) > 1 && inputs[1] != nil {
		mapImg = inputs[1]
	} else {
		// Spec: a missing in2 falls back to in1 (per Filter Effects 1
		// §"Inputs to a filter primitive"). Match by using source.
		mapImg = source
	}

	out := newRGBA(region)
	w, h := region.Dx(), region.Dy()
	if w <= 0 || h <= 0 {
		return out
	}

	xOff := channelByteOffset(e.XChannel)
	yOff := channelByteOffset(e.YChannel)
	scale := e.Scale
	srcStride := source.Stride
	mapStride := mapImg.Stride
	outStride := out.Stride
	srcW := source.Bounds().Dx()
	srcH := source.Bounds().Dy()
	mapW := mapImg.Bounds().Dx()
	mapH := mapImg.Bounds().Dy()

	for y := 0; y < h; y++ {
		mapRow := y * mapStride
		outRow := y * outStride
		for x := 0; x < w; x++ {
			// Read the displacement-map sample at integer (x, y).
			// Map and source buffers are both sized to `region`. Defend
			// against shorter buffers — out-of-bounds map samples
			// behave as zero, which yields a -0.5 * scale displacement.
			var xByte, yByte uint8
			if x < mapW && y < mapH {
				mapI := mapRow + x*4
				a := mapImg.Pix[mapI+3]
				if a == 0 {
					xByte, yByte = 0, 0
				} else if a == 255 {
					xByte = mapImg.Pix[mapI+xOff]
					yByte = mapImg.Pix[mapI+yOff]
				} else {
					// Un-premultiply colour channels; alpha stays as-is.
					af := float64(a)
					rb := float64(mapImg.Pix[mapI+0]) * 255.0 / af
					gb := float64(mapImg.Pix[mapI+1]) * 255.0 / af
					bb := float64(mapImg.Pix[mapI+2]) * 255.0 / af
					ab := float64(a)
					readBy := func(off int) uint8 {
						var v float64
						switch off {
						case 0:
							v = rb
						case 1:
							v = gb
						case 2:
							v = bb
						default:
							v = ab
						}
						if v < 0 {
							v = 0
						}
						if v > 255 {
							v = 255
						}
						return uint8(v + 0.5)
					}
					xByte = readBy(xOff)
					yByte = readBy(yOff)
				}
			}

			// Compute fractional displacement in pixels.
			dx := (float64(xByte)/255.0 - 0.5) * scale
			dy := (float64(yByte)/255.0 - 0.5) * scale

			// Sample source at (x + dx, y + dy) with bilinear
			// interpolation. Out-of-bounds returns transparent black.
			sxF := float64(x) + dx
			syF := float64(y) + dy
			r, g, bl, a := bilinearSampleRGBA(source, sxF, syF, srcW, srcH, srcStride)

			outI := outRow + x*4
			out.Pix[outI+0] = r
			out.Pix[outI+1] = g
			out.Pix[outI+2] = bl
			out.Pix[outI+3] = a
		}
	}
	return out
}

// bilinearSampleRGBA reads a sub-pixel sample from a premultiplied
// image.RGBA at (sxF, syF) with bilinear interpolation and the
// "transparent black outside" tile mode (Skia's `SkTileMode::kDecal`).
// Operates on premultiplied bytes — bilinear interpolation of
// premultiplied alpha is the only correct way to handle edge pixels
// against transparency (un-premultiplied bilinear produces colour
// fringing). Mirrors Skia's default sampling for image filters.
func bilinearSampleRGBA(img *image.RGBA, sxF, syF float64, w, h, stride int) (uint8, uint8, uint8, uint8) {
	// Pixel-centre convention: a sample at integer (i, j) targets the
	// centre of pixel [i, j]. Bilinear uses sxF - 0.5 / syF - 0.5 so
	// that an integer displacement falls cleanly on one pixel.
	fx := sxF - 0.5
	fy := syF - 0.5
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	x1 := x0 + 1
	y1 := y0 + 1
	tx := fx - float64(x0)
	ty := fy - float64(y0)

	// readPx returns the premultiplied bytes at (x, y), or
	// (0, 0, 0, 0) when out of bounds (decal tile mode).
	readPx := func(x, y int) (float64, float64, float64, float64) {
		if x < 0 || y < 0 || x >= w || y >= h {
			return 0, 0, 0, 0
		}
		i := y*stride + x*4
		return float64(img.Pix[i+0]), float64(img.Pix[i+1]), float64(img.Pix[i+2]), float64(img.Pix[i+3])
	}

	r00, g00, b00, a00 := readPx(x0, y0)
	r10, g10, b10, a10 := readPx(x1, y0)
	r01, g01, b01, a01 := readPx(x0, y1)
	r11, g11, b11, a11 := readPx(x1, y1)

	w00 := (1 - tx) * (1 - ty)
	w10 := tx * (1 - ty)
	w01 := (1 - tx) * ty
	w11 := tx * ty

	r := r00*w00 + r10*w10 + r01*w01 + r11*w11
	g := g00*w00 + g10*w10 + g01*w01 + g11*w11
	b := b00*w00 + b10*w10 + b01*w01 + b11*w11
	a := a00*w00 + a10*w10 + a01*w01 + a11*w11

	clamp := func(v float64) uint8 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v + 0.5)
	}
	return clamp(r), clamp(g), clamp(b), clamp(a)
}
