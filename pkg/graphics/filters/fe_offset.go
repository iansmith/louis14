package filters

import (
	"image"
	"math"
)

// FEOffset translates its input by (DX, DY) device pixels. Mirrors
// platform/graphics/filters/fe_offset.cc.
type FEOffset struct {
	baseEffect
	DX float64
	DY float64
}

// NewFEOffset creates an offset effect.
func NewFEOffset(input FilterEffect, space InterpolationSpace, dx, dy float64) *FEOffset {
	return &FEOffset{
		baseEffect: baseEffect{inputs: []FilterEffect{input}, space: space},
		DX:         dx,
		DY:         dy,
	}
}

// MapRect translates the rect.
func (e *FEOffset) MapRect(r image.Rectangle) image.Rectangle {
	dx := int(math.Round(e.DX))
	dy := int(math.Round(e.DY))
	return r.Add(image.Pt(dx, dy))
}

// ApplyEffect copies the input shifted by (DX, DY) within the region buffer.
func (e *FEOffset) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	out := newRGBA(region)
	dx := int(math.Round(e.DX))
	dy := int(math.Round(e.DY))
	w, h := region.Dx(), region.Dy()
	for y := 0; y < h; y++ {
		sy := y - dy
		if sy < 0 || sy >= h {
			continue
		}
		for x := 0; x < w; x++ {
			sx := x - dx
			if sx < 0 || sx >= w {
				continue
			}
			si := sy*in.Stride + sx*4
			di := y*out.Stride + x*4
			out.Pix[di] = in.Pix[si]
			out.Pix[di+1] = in.Pix[si+1]
			out.Pix[di+2] = in.Pix[si+2]
			out.Pix[di+3] = in.Pix[si+3]
		}
	}
	return out
}
