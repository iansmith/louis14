package graphics

import (
	"image"
	"image/color"
	"math"

	"louis14/pkg/geometry"
)

// This file holds the raster operations a canvas 2D context performs on its
// bitmap: the slice of Skia's SkCanvas (drawRect / drawImageRect with
// source-over and anti-aliasing) that Blink's Canvas2DRecorderContext
// records (modules/canvas/canvas2d/canvas_2d_recorder_context.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Pixel coverage of an
// axis-aligned rect is its exact area overlap with the pixel square.

// FillRect fills r with c, source-over, anti-aliased by area coverage.
func FillRect(dst *image.NRGBA, r geometry.RectF, c color.NRGBA) {
	forEachCoveredPixel(dst, r, func(x, y int, cov float64) {
		blendSourceOver(dst, x, y, c, cov)
	})
}

// DrawImageRect draws the srcRect part of src into dstRect, source-over:
// each destination pixel centre is mapped back into the source rect and
// sampled bilinearly (smooth) or nearest-neighbour, clamped to the source
// rect, as SkCanvas::drawImageRect with kStrict_SrcRectConstraint does.
// Edge pixels are anti-aliased by their coverage of dstRect.
func DrawImageRect(dst *image.NRGBA, src image.Image, srcRect, dstRect geometry.RectF, smooth bool) {
	if srcRect.Width() <= 0 || srcRect.Height() <= 0 || dstRect.Width() <= 0 || dstRect.Height() <= 0 {
		return
	}
	// The integer source pixels the clamp keeps samples within.
	minX, minY, maxX, maxY := pixelSpan(src.Bounds(), srcRect)
	maxX, maxY = maxX-1, maxY-1
	if maxX < minX || maxY < minY {
		return
	}
	sx, sy := srcRect.Width()/dstRect.Width(), srcRect.Height()/dstRect.Height()
	forEachCoveredPixel(dst, dstRect, func(x, y int, cov float64) {
		u := srcRect.X() + (float64(x)+0.5-dstRect.X())*sx
		v := srcRect.Y() + (float64(y)+0.5-dstRect.Y())*sy
		var c color.NRGBA
		if smooth {
			c = SampleBilinear(src, u, v, minX, minY, maxX, maxY)
		} else {
			r, g, b, a := premultipliedAt(src, clampInt(int(math.Floor(u)), minX, maxX), clampInt(int(math.Floor(v)), minY, maxY))
			c = unpremultiply(float64(r), float64(g), float64(b), float64(a))
		}
		blendSourceOver(dst, x, y, c, cov)
	})
}

// pixelSpan is the integer pixel range [x0, x1) × [y0, y1) that r touches,
// clipped to b.
func pixelSpan(b image.Rectangle, r geometry.RectF) (x0, y0, x1, y1 int) {
	return max(b.Min.X, int(math.Floor(r.X()))), max(b.Min.Y, int(math.Floor(r.Y()))),
		min(b.Max.X, int(math.Ceil(r.Right()))), min(b.Max.Y, int(math.Ceil(r.Bottom())))
}

// forEachCoveredPixel calls f for each pixel of dst that r covers, with the
// fraction of the pixel's area it covers.
func forEachCoveredPixel(dst *image.NRGBA, r geometry.RectF, f func(x, y int, cov float64)) {
	x0, y0, x1, y1 := pixelSpan(dst.Bounds(), r)
	for y := y0; y < y1; y++ {
		cy := overlap(float64(y), r.Y(), r.Bottom())
		for x := x0; x < x1; x++ {
			if cov := cy * overlap(float64(x), r.X(), r.Right()); cov > 0 {
				f(x, y, cov)
			}
		}
	}
}

// overlap is the length of [p, p+1) ∩ [lo, hi).
func overlap(p, lo, hi float64) float64 {
	return max(0, min(p+1, hi)-max(p, lo))
}

func clampInt(v, lo, hi int) int { return max(lo, min(hi, v)) }

// premultipliedAt returns src's pixel at (x, y) premultiplied, 16-bit per
// channel, reading the common bitmap types' Pix directly.
func premultipliedAt(src image.Image, x, y int) (r, g, b, a uint32) {
	switch s := src.(type) {
	case *image.NRGBA:
		p := s.Pix[s.PixOffset(x, y):]
		a = uint32(p[3]) * 0x101
		return uint32(p[0]) * a / 0xff, uint32(p[1]) * a / 0xff, uint32(p[2]) * a / 0xff, a
	case *image.RGBA:
		p := s.Pix[s.PixOffset(x, y):]
		return uint32(p[0]) * 0x101, uint32(p[1]) * 0x101, uint32(p[2]) * 0x101, uint32(p[3]) * 0x101
	}
	return src.At(x, y).RGBA()
}

// unpremultiply converts 16-bit premultiplied channels to an 8-bit
// non-premultiplied color.
func unpremultiply(r, g, b, a float64) color.NRGBA {
	if a == 0 {
		return color.NRGBA{}
	}
	return color.NRGBA{
		R: uint8(math.Round(r / a * 255)),
		G: uint8(math.Round(g / a * 255)),
		B: uint8(math.Round(b / a * 255)),
		A: uint8(math.Round(a / 0xffff * 255)),
	}
}

// SampleBilinear samples src at (u, v) (pixel centres at +0.5), clamping
// taps to [minX, maxX] × [minY, maxY]; it interpolates premultiplied.
func SampleBilinear(src image.Image, u, v float64, minX, minY, maxX, maxY int) color.NRGBA {
	r, g, b, a := sampleBilinearPremultiplied(src, u, v, minX, minY, maxX, maxY)
	return unpremultiply(r, g, b, a)
}

// SampleBilinearPremultiplied is SampleBilinear returning the premultiplied
// colour, as an image.RGBA stores it.
func SampleBilinearPremultiplied(src image.Image, u, v float64, minX, minY, maxX, maxY int) color.RGBA {
	r, g, b, a := sampleBilinearPremultiplied(src, u, v, minX, minY, maxX, maxY)
	return color.RGBA{uint8(math.Round(r / 0x101)), uint8(math.Round(g / 0x101)), uint8(math.Round(b / 0x101)), uint8(math.Round(a / 0x101))}
}

// sampleBilinearPremultiplied interpolates src's four premultiplied 16-bit
// taps around (u, v).
func sampleBilinearPremultiplied(src image.Image, u, v float64, minX, minY, maxX, maxY int) (r, g, b, a float64) {
	fx, fy := u-0.5, v-0.5
	ix, iy := int(math.Floor(fx)), int(math.Floor(fy))
	tx, ty := fx-float64(ix), fy-float64(iy)
	for _, tap := range [4]struct {
		dx, dy int
		w      float64
	}{{0, 0, (1 - tx) * (1 - ty)}, {1, 0, tx * (1 - ty)}, {0, 1, (1 - tx) * ty}, {1, 1, tx * ty}} {
		if tap.w == 0 {
			continue
		}
		pr, pg, pb, pa := premultipliedAt(src, clampInt(ix+tap.dx, minX, maxX), clampInt(iy+tap.dy, minY, maxY))
		r += float64(pr) * tap.w
		g += float64(pg) * tap.w
		b += float64(pb) * tap.w
		a += float64(pa) * tap.w
	}
	return r, g, b, a
}

// blendSourceOver composites c, scaled by coverage, over dst's pixel.
func blendSourceOver(dst *image.NRGBA, x, y int, c color.NRGBA, coverage float64) {
	sa := float64(c.A) / 255 * coverage
	if sa <= 0 {
		return
	}
	i := dst.PixOffset(x, y)
	d := dst.Pix[i : i+4 : i+4]
	da := float64(d[3]) / 255
	switch {
	case sa >= 1:
		d[0], d[1], d[2], d[3] = c.R, c.G, c.B, 255
		return
	case da == 0:
		d[0], d[1], d[2], d[3] = c.R, c.G, c.B, uint8(math.Round(sa*255))
		return
	}
	oa := sa + da*(1-sa)
	mix := func(s, dc uint8) uint8 {
		return uint8(math.Round((float64(s)*sa + float64(dc)*da*(1-sa)) / oa))
	}
	d[0], d[1], d[2], d[3] = mix(c.R, d[0]), mix(c.G, d[1]), mix(c.B, d[2]), uint8(math.Round(oa*255))
}
