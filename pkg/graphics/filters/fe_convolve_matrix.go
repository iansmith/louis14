package filters

import (
	"image"
	"math"
)

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
	// Compute default divisor if not provided: sum of kernel elements.
	// If that sum is 0, divisor defaults to 1.
	if divisor == 0 {
		divisor = 1.0
		for _, v := range kernelMatrix {
			divisor += v
		}
		// Avoid division by zero; if sum is still 0, use 1.
		if divisor == 0 {
			divisor = 1.0
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
func (e *FEConvolveMatrix) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	out := newRGBA(region)

	w, h := region.Dx(), region.Dy()

	// order should equal sqrt(len(kernelMatrix)); validate it's a perfect square.
	expectedLen := e.Order * e.Order
	if len(e.KernelMatrix) != expectedLen {
		// Invalid kernel; return input unchanged as fallback.
		copy(out.Pix, in.Pix)
		return out
	}

	// The kernel center is at (targetX, targetY) within the kernel grid.
	// For a 3x3 kernel with center at (1,1), pixel (x,y) convolves with
	// the patch spanning [x-1, x+1] × [y-1, y+1].

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, b, a float64

			// Walk the kernel matrix.
			for ky := 0; ky < e.Order; ky++ {
				for kx := 0; kx < e.Order; kx++ {
					// Source pixel relative to the target.
					// Kernel is applied with the kernel center at (targetX, targetY),
					// so the kernel top-left corner is at (targetX-targetX, targetY-targetY) = (0,0)
					// in kernel space. To find the source pixel for kernel position (kx, ky),
					// we want the pixel at (x - targetX + kx, y - targetY + ky) in the source.
					sx := x - e.TargetX + kx
					sy := y - e.TargetY + ky

					// Resolve edge mode.
					if sx < 0 || sx >= w || sy < 0 || sy >= h {
						// Out of bounds.
						switch e.EdgeMode {
						case "wrap":
							sx = ((sx % w) + w) % w
							sy = ((sy % h) + h) % h
						case "none":
							// Treat as transparent black; skip this pixel.
							continue
						default: // "duplicate"
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

					// Fetch the source pixel.
					var sr, sg, sb, sa float64
					if sx >= 0 && sx < w && sy >= 0 && sy < h {
						si := sy*in.Stride + sx*4
						sr = float64(in.Pix[si])
						sg = float64(in.Pix[si+1])
						sb = float64(in.Pix[si+2])
						sa = float64(in.Pix[si+3])
					} else {
						// Out of bounds in "none" mode (should be caught above).
						sr, sg, sb, sa = 0, 0, 0, 0
					}

					// Kernel value (without reversing — let's test without the 180° rotation).
					kv := e.KernelMatrix[ky*e.Order+kx]

					// Accumulate.
					r += sr * kv
					g += sg * kv
					b += sb * kv
					// Alpha: only convolve if !preserveAlpha.
					if !e.PreserveAlpha {
						a += sa * kv
					}
				}
			}

			// Normalize and bias. Per spec: OUTPUT = (KERNEL_SUM + BIAS * ORDER * ORDER) / DIVISOR
			kernelOrder := float64(e.Order)
			biasAdjusted := e.Bias * kernelOrder * kernelOrder
			r = (r + biasAdjusted) / e.Divisor
			g = (g + biasAdjusted) / e.Divisor
			b = (b + biasAdjusted) / e.Divisor
			if !e.PreserveAlpha {
				a = (a + biasAdjusted) / e.Divisor
			} else {
				// Preserve input alpha.
				si := y*in.Stride + x*4
				a = float64(in.Pix[si+3])
			}

			// Clamp to valid range: [0, 255] for RGB, [0, 1] for alpha.
			r = maxF(0, minF(r, 255))
			g = maxF(0, minF(g, 255))
			b = maxF(0, minF(b, 255))
			a = maxF(0, minF(a, 1))

			// Write premultiplied RGBA.
			di := y*out.Stride + x*4
			out.Pix[di] = uint8(math.Round(r))
			out.Pix[di+1] = uint8(math.Round(g))
			out.Pix[di+2] = uint8(math.Round(b))
			out.Pix[di+3] = uint8(math.Round(a * 255))
		}
	}

	return out
}

// Helper functions.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
