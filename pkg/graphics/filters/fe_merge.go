package filters

import "image"

// FEMerge stacks its inputs with source-over compositing, the first input at
// the bottom. Mirrors platform/graphics/filters/fe_merge.cc.
type FEMerge struct {
	baseEffect
}

// NewFEMerge creates a merge effect over the given inputs (bottom-first).
func NewFEMerge(inputs []FilterEffect, space InterpolationSpace) *FEMerge {
	return &FEMerge{baseEffect: baseEffect{inputs: inputs, space: space}}
}

// ApplyEffect composites the inputs bottom-to-top.
func (e *FEMerge) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	out := newRGBA(region)
	for _, in := range inputs {
		if in == nil {
			continue
		}
		compositeSrcOver(out, in)
	}
	return out
}

// compositeSrcOver composites src over dst in place. Both are premultiplied
// image.RGBA buffers of the same dimensions.
func compositeSrcOver(dst, src *image.RGBA) {
	dp := dst.Pix
	sp := src.Pix
	n := len(dp)
	if len(sp) < n {
		n = len(sp)
	}
	for i := 0; i+3 < n; i += 4 {
		sa := uint32(sp[i+3])
		if sa == 0 {
			continue
		}
		if sa == 255 {
			dp[i] = sp[i]
			dp[i+1] = sp[i+1]
			dp[i+2] = sp[i+2]
			dp[i+3] = 255
			continue
		}
		ia := 255 - sa
		// premultiplied src-over: out = src + dst*(1-srcAlpha)
		dp[i] = uint8(uint32(sp[i]) + uint32(dp[i])*ia/255)
		dp[i+1] = uint8(uint32(sp[i+1]) + uint32(dp[i+1])*ia/255)
		dp[i+2] = uint8(uint32(sp[i+2]) + uint32(dp[i+2])*ia/255)
		dp[i+3] = uint8(uint32(sp[i+3]) + uint32(dp[i+3])*ia/255)
	}
}
