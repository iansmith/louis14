package filters

import "image"

// FilterEffect is one node in the filter graph — the louis14 analogue of
// Blink's platform/graphics/filters/FilterEffect. Each effect consumes the
// outputs of its input effects and produces a new image. Effects declare the
// interpolation space they operate in; the graph evaluator (Filter.Apply)
// converts images across edges so that, e.g., a linearRGB primitive always
// sees linearRGB input regardless of what produced it.
//
// All images flowing through the graph are premultiplied image.RGBA buffers
// whose Bounds() are the filter region in absolute device-pixel coordinates:
// every effect's output covers the same rectangle as the filter region so
// that compositing offsets stay trivial. Effects that conceptually have a
// smaller output (a flood, a shape) simply leave the rest transparent.
type FilterEffect interface {
	// InputEffects returns this effect's upstream dependencies, in argument
	// order (e.g. feComposite's [in, in2]).
	InputEffects() []FilterEffect

	// OperatingSpace is the interpolation space this effect computes in.
	OperatingSpace() InterpolationSpace

	// ApplyEffect computes the effect's output. inputs are the already
	// evaluated and space-converted outputs of InputEffects(), each sized to
	// region. region is the filter region in absolute device coordinates.
	ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA

	// MapRect maps a rectangle from this effect's input space forward to its
	// output space — used to compute how far an effect spreads its input
	// (e.g. a blur inflates the rect by ~3σ). The rect is in absolute
	// device coordinates.
	MapRect(r image.Rectangle) image.Rectangle

	// OriginTainted reports whether this effect's output is tainted by a
	// cross-origin source somewhere upstream. Mirrors Blink
	// FilterEffect::OriginTainted (filter_effect.h:95). Default false;
	// the builder propagates the flag at construction time via
	// SetOriginTainted (see SVGFilterBuilder.BuildGraph).
	OriginTainted() bool

	// SetOriginTainted marks this effect's output as tainted. Mirrors
	// Blink FilterEffect::SetOriginTainted (filter_effect.h:96).
	// Tainting is monotonic — once set, the flag is never cleared. The
	// setter takes no argument by design.
	SetOriginTainted()

	// InputsTaintOrigin returns true when ANY of this effect's
	// InputEffects() reports OriginTainted() true. Mirrors Blink
	// FilterEffect::InputsTaintOrigin (filter_effect.cc:172-177).
	InputsTaintOrigin() bool
}

// baseEffect provides the common InputEffects/OperatingSpace storage that
// every concrete effect embeds. Mirrors FilterEffect's input_effects_ /
// operating_interpolation_space_ / origin_tainted_ members
// (platform/graphics/filters/filter_effect.h:127).
type baseEffect struct {
	inputs []FilterEffect
	space  InterpolationSpace
	// originTainted carries Blink's origin_tainted_ flag. Default
	// false; set monotonically via SetOriginTainted. Propagation
	// happens builder-side in SVGFilterBuilder.BuildGraph (mirrors
	// svg_filter_builder.cc:191-192).
	originTainted bool
}

func (b *baseEffect) InputEffects() []FilterEffect       { return b.inputs }
func (b *baseEffect) OperatingSpace() InterpolationSpace { return b.space }

// MapRect default: an effect that does not spread its input passes the rect
// through unchanged. Effects with spread (blur, offset, drop-shadow, ...)
// override this.
func (b *baseEffect) MapRect(r image.Rectangle) image.Rectangle { return r }

// OriginTainted reports the effect's tainted bit. Mirrors Blink
// FilterEffect::OriginTainted (filter_effect.h:95).
func (b *baseEffect) OriginTainted() bool { return b.originTainted }

// SetOriginTainted marks the effect as tainted; the flag is monotonic —
// once set, it stays set. Mirrors Blink FilterEffect::SetOriginTainted
// (filter_effect.h:96), which is a no-arg setter for the same reason.
func (b *baseEffect) SetOriginTainted() { b.originTainted = true }

// InputsTaintOrigin returns true if any input's OriginTainted() is
// true. Mirrors Blink FilterEffect::InputsTaintOrigin
// (filter_effect.cc:172-177). Because baseEffect cannot see through to
// the embedding type's full input set when methods are overridden, the
// iteration uses the stored inputs slice — which is identical to
// InputEffects() for every concrete FE* in this package (they all set
// inputs at construction time and never override InputEffects).
func (b *baseEffect) InputsTaintOrigin() bool {
	for _, in := range b.inputs {
		if in != nil && in.OriginTainted() {
			return true
		}
	}
	return false
}

// newRGBA allocates a transparent premultiplied buffer covering region.
func newRGBA(region image.Rectangle) *image.RGBA {
	return image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
}

// firstInput returns inputs[0] or a fresh transparent buffer when there is no
// input — the SVG "empty input" fallback.
func firstInput(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	if len(inputs) > 0 && inputs[0] != nil {
		return inputs[0]
	}
	return newRGBA(region)
}
