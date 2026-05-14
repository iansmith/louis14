package filters

import (
	"image"
	"math"
)

// FEDropShadow implements the drop-shadow() shorthand and the SVG feDropShadow
// primitive. Per Filter Effects 1 it is equivalent to the sub-graph:
//
//	shadow = FEFlood(color) IN FEOffset(dx,dy, FEGaussianBlur(stdDev, SourceAlpha))
//	result = FEMerge{ shadow, SourceGraphic }
//
// Mirrors platform/graphics/filters/fe_drop_shadow.cc, which builds exactly
// this composite. inputs[0] is the effect being shadowed (the chain's current
// output, which is also what the shadow merges under); inputs[1] is the
// SourceAlpha the shadow shape is derived from.
type FEDropShadow struct {
	baseEffect
	DX, DY  float64
	StdDev  float64
	R, G, B uint8
	A       float64
}

// NewFEDropShadow creates a drop-shadow effect. graphic is the chain output to
// shadow; srcAlpha supplies the shadow shape.
func NewFEDropShadow(graphic FilterEffect, srcAlpha FilterEffect, space InterpolationSpace,
	dx, dy, stdDev float64, r, g, b uint8, a float64) *FEDropShadow {
	return &FEDropShadow{
		baseEffect: baseEffect{inputs: []FilterEffect{graphic, srcAlpha}, space: space},
		DX:         dx, DY: dy, StdDev: stdDev,
		R: r, G: g, B: b, A: a,
	}
}

// MapRect: the output covers the input rect unioned with the offset+blurred
// shadow rect — the reason a drop-shadow buffer must not be clipped to the box.
func (e *FEDropShadow) MapRect(r image.Rectangle) image.Rectangle {
	ext := blurExtentForStdDev(e.StdDev)
	dx := int(math.Round(e.DX))
	dy := int(math.Round(e.DY))
	shadow := image.Rect(
		r.Min.X-ext+dx, r.Min.Y-ext+dy,
		r.Max.X+ext+dx, r.Max.Y+ext+dy,
	)
	return r.Union(shadow)
}

// ApplyEffect builds the shadow from inputs[1] (SourceAlpha): blur it, offset
// it, recolour it with the shadow colour, then composite the original
// inputs[0] on top.
func (e *FEDropShadow) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	var graphic, srcAlpha *image.RGBA
	if len(inputs) > 0 {
		graphic = inputs[0]
	}
	if len(inputs) > 1 {
		srcAlpha = inputs[1]
	}
	if graphic == nil {
		graphic = newRGBA(region)
	}
	if srcAlpha == nil {
		srcAlpha = newRGBA(region)
	}

	// 1. Blur the alpha shape.
	blur := NewFEGaussianBlur(nil, e.space, e.StdDev, e.StdDev)
	blurred := blur.ApplyEffect([]*image.RGBA{srcAlpha}, region)

	// 2. Offset it.
	offset := NewFEOffset(nil, e.space, e.DX, e.DY)
	offsetShadow := offset.ApplyEffect([]*image.RGBA{blurred}, region)

	// 3. Recolour: the shadow keeps the offset+blurred alpha but takes the
	//    shadow colour. This is FEFlood(color) composited IN the shadow
	//    alpha: result = floodColor * shadowAlpha (premultiplied).
	shadow := newRGBA(region)
	cr := float64(e.R) / 255.0
	cg := float64(e.G) / 255.0
	cb := float64(e.B) / 255.0
	for i := 0; i+3 < len(offsetShadow.Pix); i += 4 {
		sa := float64(offsetShadow.Pix[i+3]) / 255.0
		if sa == 0 {
			continue
		}
		outA := sa * e.A
		px := premultiply(cr, cg, cb, outA)
		shadow.Pix[i] = px[0]
		shadow.Pix[i+1] = px[1]
		shadow.Pix[i+2] = px[2]
		shadow.Pix[i+3] = px[3]
	}

	// 4. Merge: shadow under the original graphic.
	out := newRGBA(region)
	compositeSrcOver(out, shadow)
	compositeSrcOver(out, graphic)
	return out
}
