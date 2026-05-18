package filters

import "image"

// SourceGraphic is the filter graph's built-in input: the rendered element
// subtree. Its image is supplied externally (the painter renders the element
// into a buffer the size of the filter region) and stored on the Filter; the
// effect just hands that buffer through. Mirrors Blink's SourceGraphic.
type SourceGraphic struct {
	baseEffect
	// image is set by Filter.SetSourceImage before evaluation.
	image *image.RGBA
}

// NewSourceGraphic creates a SourceGraphic operating in the given space.
func NewSourceGraphic(space InterpolationSpace) *SourceGraphic {
	return &SourceGraphic{baseEffect: baseEffect{space: space}}
}

// ApplyEffect returns the externally supplied source image, sized to region.
func (s *SourceGraphic) ApplyEffect(_ []*image.RGBA, region image.Rectangle) *image.RGBA {
	if s.image != nil {
		return s.image
	}
	return newRGBA(region)
}

// SourceAlpha is the built-in input that takes SourceGraphic and zeroes its
// colour channels, keeping only the alpha — the input to a drop-shadow's blur.
// Mirrors Blink's SourceAlpha.
type SourceAlpha struct {
	baseEffect
}

// NewSourceAlpha creates a SourceAlpha fed by the given SourceGraphic.
func NewSourceAlpha(src *SourceGraphic, space InterpolationSpace) *SourceAlpha {
	return &SourceAlpha{baseEffect: baseEffect{inputs: []FilterEffect{src}, space: space}}
}

// ApplyEffect produces a buffer with the input's alpha and black RGB. Because
// image.RGBA is premultiplied, black-with-alpha-a is simply {0,0,0,a}.
func (s *SourceAlpha) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	in := firstInput(inputs, region)
	out := newRGBA(region)
	for i := 0; i+3 < len(in.Pix); i += 4 {
		out.Pix[i+3] = in.Pix[i+3]
	}
	return out
}
