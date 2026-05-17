package filters

import (
	"image"
	"math"
)

// FEGaussianBlur approximates a Gaussian blur with three successive box
// blurs, exactly as the SVG filter spec prescribes and Blink's
// platform/graphics/filters/fe_gaussian_blur.cc implements (via Skia's
// SkBlurImageFilter, which itself uses the 3-box approximation for the CPU
// path). Operates on premultiplied pixels — blurring premultiplied colour is
// what keeps edges against transparency correct.
type FEGaussianBlur struct {
	baseEffect
	StdDevX float64
	StdDevY float64
}

// NewFEGaussianBlur creates a blur effect. stdDev is the Gaussian standard
// deviation (CSS blur(r) maps r directly to stdDev — note σ = r, not r/2).
func NewFEGaussianBlur(input FilterEffect, space InterpolationSpace, stdDevX, stdDevY float64) *FEGaussianBlur {
	return &FEGaussianBlur{
		baseEffect: baseEffect{inputs: []FilterEffect{input}, space: space},
		StdDevX:    stdDevX,
		StdDevY:    stdDevY,
	}
}

// boxSizesForStdDev returns the three box-blur diameters for a given σ along
// one axis, per SVG Filter Effects §15.17 "Gaussian blur":
//
//	d = floor(s * 3 * sqrt(2*PI) / 4 + 0.5)
//
// then if d is odd, three boxes of size d centred; if even, two boxes of size
// d (offset by ±1 to stay centred overall) and one of size d+1.
func boxSizesForStdDev(s float64) (d1, d2, d3 int) {
	if s <= 0 {
		return 0, 0, 0
	}
	d := int(math.Floor(s*3.0*math.Sqrt(2.0*math.Pi)/4.0 + 0.5))
	if d%2 == 1 {
		return d, d, d
	}
	return d, d, d + 1
}

// blurExtentForStdDev returns how many pixels a blur of stdDev s spreads its
// input — the sum of the three box half-widths. Used for MapRect.
//
// A sigma of 0 (or negative) means no blur and therefore zero spread.
// Mirrors Blink's FEGaussianBlur::MapRect (fe_gaussian_blur.cc), which
// returns the input rect unchanged in that case.
func blurExtentForStdDev(s float64) int {
	if s <= 0 {
		return 0
	}
	d1, d2, d3 := boxSizesForStdDev(s)
	// Each box of diameter d spreads by d/2; total spread is the sum.
	// The +2 padding accounts for the asymmetric biasing of even-diameter
	// passes (see boxBlurAxis) and the half-pixel rasterization edge.
	return d1/2 + d2/2 + d3/2 + 2
}

// MapRect inflates the rect by the blur's spread on each axis.
func (e *FEGaussianBlur) MapRect(r image.Rectangle) image.Rectangle {
	ex := blurExtentForStdDev(e.StdDevX)
	ey := blurExtentForStdDev(e.StdDevY)
	return image.Rect(r.Min.X-ex, r.Min.Y-ey, r.Max.X+ex, r.Max.Y+ey)
}

// ApplyEffect runs the three-pass box blur on a copy of the input.
func (e *FEGaussianBlur) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	out := image.NewRGBA(in.Bounds())
	copy(out.Pix, in.Pix)
	if e.StdDevX <= 0 && e.StdDevY <= 0 {
		return out
	}
	dx1, dx2, dx3 := boxSizesForStdDev(e.StdDevX)
	dy1, dy2, dy3 := boxSizesForStdDev(e.StdDevY)
	// Horizontal passes.
	boxBlurAxis(out, dx1, true, 0)
	boxBlurAxis(out, dx2, true, 0)
	boxBlurAxis(out, dx3, true, 0)
	// Vertical passes.
	boxBlurAxis(out, dy1, false, 0)
	boxBlurAxis(out, dy2, false, 0)
	boxBlurAxis(out, dy3, false, 0)
	return out
}

// boxBlurAxis runs one box blur of the given diameter along one axis
// (horizontal when horiz is true). A diameter <= 1 is a no-op. The kernel is
// centred; for an even diameter the window is biased so three even passes
// still produce an overall-centred result (handled by the caller using a
// d / d / d+1 triple). Operates on premultiplied bytes in place.
func boxBlurAxis(img *image.RGBA, diameter int, horiz bool, _ int) {
	if diameter <= 1 {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return
	}
	stride := img.Stride
	left := diameter / 2
	right := diameter - 1 - left
	src := make([]uint8, len(img.Pix))
	copy(src, img.Pix)

	if horiz {
		for y := 0; y < h; y++ {
			rowOff := y * stride
			for x := 0; x < w; x++ {
				var rs, gs, bs, as, count uint32
				for k := -left; k <= right; k++ {
					sx := x + k
					if sx < 0 || sx >= w {
						count++ // treat off-edge as transparent black
						continue
					}
					o := rowOff + sx*4
					rs += uint32(src[o])
					gs += uint32(src[o+1])
					bs += uint32(src[o+2])
					as += uint32(src[o+3])
					count++
				}
				o := rowOff + x*4
				img.Pix[o] = uint8(rs / count)
				img.Pix[o+1] = uint8(gs / count)
				img.Pix[o+2] = uint8(bs / count)
				img.Pix[o+3] = uint8(as / count)
			}
		}
		return
	}
	for x := 0; x < w; x++ {
		colOff := x * 4
		for y := 0; y < h; y++ {
			var rs, gs, bs, as, count uint32
			for k := -left; k <= right; k++ {
				sy := y + k
				if sy < 0 || sy >= h {
					count++
					continue
				}
				o := sy*stride + colOff
				rs += uint32(src[o])
				gs += uint32(src[o+1])
				bs += uint32(src[o+2])
				as += uint32(src[o+3])
				count++
			}
			o := y*stride + colOff
			img.Pix[o] = uint8(rs / count)
			img.Pix[o+1] = uint8(gs / count)
			img.Pix[o+2] = uint8(bs / count)
			img.Pix[o+3] = uint8(as / count)
		}
	}
}
