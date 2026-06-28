package filters

import "image"

// FEFlood fills its subregion with a constant colour. With no Subregion set it
// fills the whole filter region. Mirrors platform/graphics/filters/fe_flood.cc.
// The colour is stored as non-premultiplied 8-bit RGB plus [0,1] alpha.
type FEFlood struct {
	baseEffect
	R, G, B uint8
	A       float64
	// Subregion, when non-empty, limits the flood to that rect (absolute
	// device coordinates) — the SVG primitive subregion. Empty means the
	// whole filter region.
	Subregion image.Rectangle
}

// NewFEFlood creates a flood effect with the given non-premultiplied colour.
// The space argument is accepted for symmetry with the other effect
// constructors but ignored: feFlood is exempt from color-interpolation-filters
// and always operates in sRGB (Blink treats SetOperatingInterpolationSpace as a
// no-op for feFlood), emitting its colour raw. The effect is therefore always
// constructed in InterpolationSpaceSRGB so its stored space matches the sRGB
// pixels ApplyEffect writes — the graph converts sRGB→linearRGB at the edge
// when a linearRGB primitive consumes the flood.
func NewFEFlood(_ InterpolationSpace, r, g, b uint8, a float64) *FEFlood {
	return &FEFlood{
		baseEffect: baseEffect{space: InterpolationSpaceSRGB},
		R:          r, G: g, B: b, A: a,
	}
}

// MapRect overrides baseEffect.MapRect. FEFlood is a generator: it fills
// its Subregion with a constant colour, ignoring any input image. The
// output therefore covers Subregion regardless of the input rect `r`, so
// the forward bounds must include Subregion. Mirrors Blink's
// FEFlood::MapRect (platform/graphics/filters/fe_flood.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f): returns the union of the
// input rect and the flood's output area (Subregion) so the filter graph's
// canvas (filter.FilterRegion) is sized to cover the flood.
func (e *FEFlood) MapRect(r image.Rectangle) image.Rectangle {
	if !e.Subregion.Empty() {
		return r.Union(e.Subregion)
	}
	return r
}

// ApplyEffect fills the region (or the subregion) with the flood colour.
func (e *FEFlood) ApplyEffect(_ []*image.RGBA, region image.Rectangle) *image.RGBA {
	out := newRGBA(region)
	// feFlood is exempt from color-interpolation-filters: the flood colour is
	// emitted directly in sRGB regardless of the filter's operating space, so it
	// is stored raw with no sRGB→operating-space conversion. FEFlood always
	// operates in sRGB — the sole production constructor passes
	// InterpolationSpaceSRGB (see svg_filter_builder.buildOnePrimitive), and the
	// space is never mutated afterwards. Mirrors Blink, where FEFlood overrides
	// SetOperatingInterpolationSpace as a no-op so the flood is never adapted to
	// linearRGB (fe_flood.cc).
	r, g, b := float64(e.R)/255, float64(e.G)/255, float64(e.B)/255
	px := premultiply(r, g, b, e.A)
	fill := region
	if !e.Subregion.Empty() {
		fill = region.Intersect(e.Subregion)
	}
	// Convert fill (absolute) to buffer-local coordinates.
	x0 := fill.Min.X - region.Min.X
	y0 := fill.Min.Y - region.Min.Y
	x1 := fill.Max.X - region.Min.X
	y1 := fill.Max.Y - region.Min.Y
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > region.Dx() {
		x1 = region.Dx()
	}
	if y1 > region.Dy() {
		y1 = region.Dy()
	}
	for y := y0; y < y1; y++ {
		row := y * out.Stride
		for x := x0; x < x1; x++ {
			i := row + x*4
			out.Pix[i] = px[0]
			out.Pix[i+1] = px[1]
			out.Pix[i+2] = px[2]
			out.Pix[i+3] = px[3]
		}
	}
	return out
}
