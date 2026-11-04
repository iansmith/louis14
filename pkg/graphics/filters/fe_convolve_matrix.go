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
// For an asymmetric target the reach differs per side: left=TargetX,
// right=Order-1-TargetX, top=TargetY, bottom=Order-1-TargetY.
func (e *FEConvolveMatrix) MapRect(r image.Rectangle) image.Rectangle {
	left := e.TargetX
	right := e.Order - 1 - e.TargetX
	top := e.TargetY
	bottom := e.Order - 1 - e.TargetY
	return image.Rect(r.Min.X-left, r.Min.Y-top, r.Max.X+right, r.Max.Y+bottom)
}

// ApplyEffect applies the convolution kernel to the input image, as Skia's
// matrix convolution shader does (make_matrix_conv_shader,
// src/core/SkKnownRuntimeEffects.cpp:166-196 @
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e), with Blink's parameters
// (platform/graphics/filters/fe_convolve_matrix.cc:148-152 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b: gain = 1/divisor,
// convolve_alpha = !preserve_alpha_).
//
//	color = sum*gain + bias
//	preserveAlpha: accumulate unpremultiplied; color = (color.rgb*a, a), where a
//	               is the alpha of the sample under the target
//	otherwise:     accumulate premultiplied; color.a = saturate(color.a)
//	color.rgb = clamp(color.rgb, 0, color.a)
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
					px := [4]uint8{in.Pix[si], in.Pix[si+1], in.Pix[si+2], in.Pix[si+3]}
					var r, g, b float64
					if e.PreserveAlpha {
						r, g, b, _ = unpremultiply(px)
					} else {
						r, g, b = float64(px[0])*inv, float64(px[1])*inv, float64(px[2])*inv
						sumA += float64(px[3]) * inv * kv
					}
					sumR += r * kv
					sumG += g * kv
					sumB += b * kv
				}
			}

			rOut := sumR/divisor + bias
			gOut := sumG/divisor + bias
			bOut := sumB/divisor + bias

			var aOut float64
			if e.PreserveAlpha {
				// The kernel position under the target samples (x, y) itself.
				aOut = float64(in.Pix[y*in.Stride+x*4+3]) * inv
				rOut *= aOut
				gOut *= aOut
				bOut *= aOut
			} else {
				aOut = clamp01(sumA/divisor + bias)
			}

			di := y*out.Stride + x*4
			out.Pix[di] = clampByte(min(rOut, aOut))
			out.Pix[di+1] = clampByte(min(gOut, aOut))
			out.Pix[di+2] = clampByte(min(bOut, aOut))
			out.Pix[di+3] = clampByte(aOut)
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
