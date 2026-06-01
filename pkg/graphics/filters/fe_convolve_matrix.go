package filters

import "image"

// FEConvolveMatrix applies a 2D kernel convolution to its input image,
// per SVG Filter Effects 1 §feConvolveMatrix. Mirrors
// platform/graphics/filters/fe_convolve_matrix.cc.
type FEConvolveMatrix struct {
	baseEffect
	Order             int
	KernelMatrix      []float64
	Divisor           float64
	Bias              float64
	TargetX           int
	TargetY           int
	EdgeMode          string // "duplicate" (default) | "wrap" | "none"
	KernelUnitLengthX float64
	KernelUnitLengthY float64
	PreserveAlpha     bool
}

// NewFEConvolveMatrix creates a convolution-matrix effect.
func NewFEConvolveMatrix(
	input FilterEffect,
	space InterpolationSpace,
	order int,
	kernelMatrix []float64,
	divisor float64,
	bias float64,
	targetX, targetY int,
	edgeMode string,
	kernelUnitLengthX, kernelUnitLengthY float64,
	preserveAlpha bool,
) *FEConvolveMatrix {
	// Default divisor is the sum of the kernel elements (spec §feConvolveMatrix).
	// Falls back to 1 if the sum is zero (all-zero kernel is a no-op anyway).
	if divisor == 0 {
		for _, v := range kernelMatrix {
			divisor += v
		}
		if divisor == 0 {
			divisor = 1
		}
	}

	return &FEConvolveMatrix{
		baseEffect:        baseEffect{inputs: []FilterEffect{input}, space: space},
		Order:             order,
		KernelMatrix:      kernelMatrix,
		Divisor:           divisor,
		Bias:              bias,
		TargetX:           targetX,
		TargetY:           targetY,
		EdgeMode:          edgeMode,
		KernelUnitLengthX: kernelUnitLengthX,
		KernelUnitLengthY: kernelUnitLengthY,
		PreserveAlpha:     preserveAlpha,
	}
}

// MapRect inflates the rectangle by the kernel reach in each direction.
// A kernel of order N can affect pixels up to ~(N-1)/2 away from the center.
func (e *FEConvolveMatrix) MapRect(r image.Rectangle) image.Rectangle {
	// Spread is roughly (order-1)/2 pixels in each direction.
	spread := (e.Order - 1) / 2
	return r.Inset(-spread)
}

// ApplyEffect applies the convolution kernel to the input image.
// All arithmetic is in [0,1] float64; per SVG Filter Effects §feConvolveMatrix
// the formula is result[c] = Σ(src[c]*kernel) / divisor + bias, with values
// in [0,1] normalized space. The input is already in the declared operating
// space (premultiplied); bias is added after the sum/divisor step, matching
// Skia's MatrixConvolutionPaintFilter gain+bias model.
func (e *FEConvolveMatrix) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	out := newRGBA(region)

	w, h := region.Dx(), region.Dy()
	order := e.Order
	if len(e.KernelMatrix) != order*order || order < 1 {
		copy(out.Pix, in.Pix)
		return out
	}

	const inv = 1.0 / 255.0
	divisor := e.Divisor
	bias := e.Bias

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sumR, sumG, sumB, sumA float64

			for ky := 0; ky < order; ky++ {
				for kx := 0; kx < order; kx++ {
					sx := x - e.TargetX + kx
					sy := y - e.TargetY + ky

					if sx < 0 || sx >= w || sy < 0 || sy >= h {
						switch e.EdgeMode {
						case "wrap":
							sx = ((sx % w) + w) % w
							sy = ((sy % h) + h) % h
						case "none":
							// Transparent black contributes 0 to all channels.
							continue
						default: // "duplicate": clamp to nearest edge pixel (Skia kClamp).
							if sx < 0 {
								sx = 0
							} else if sx >= w {
								sx = w - 1
							}
							if sy < 0 {
								sy = 0
							} else if sy >= h {
								sy = h - 1
							}
						}
					}

					kv := e.KernelMatrix[ky*order+kx]
					si := sy*in.Stride + sx*4
					sumR += float64(in.Pix[si]) * inv * kv
					sumG += float64(in.Pix[si+1]) * inv * kv
					sumB += float64(in.Pix[si+2]) * inv * kv
					sumA += float64(in.Pix[si+3]) * inv * kv
				}
			}

			rOut := clamp01(sumR/divisor + bias)
			gOut := clamp01(sumG/divisor + bias)
			bOut := clamp01(sumB/divisor + bias)

			var aOut float64
			if e.PreserveAlpha {
				si := y*in.Stride + x*4
				aOut = float64(in.Pix[si+3]) * inv
			} else {
				aOut = clamp01(sumA/divisor + bias)
			}

			di := y*out.Stride + x*4
			out.Pix[di] = uint8(rOut*255 + 0.5)
			out.Pix[di+1] = uint8(gOut*255 + 0.5)
			out.Pix[di+2] = uint8(bOut*255 + 0.5)
			out.Pix[di+3] = uint8(aOut*255 + 0.5)
		}
	}

	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
