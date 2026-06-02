package filters

import "image"

// Filter owns a filter graph and the geometry it is evaluated against. It is
// the louis14 analogue of Blink's platform/graphics/filters/Filter: it holds
// the reference box, the filter region, the SourceGraphic / SourceAlpha
// builtins, and the last effect (the graph root).
//
// Coordinates are absolute device pixels. ReferenceBox is the element's
// border box; FilterRegion is the rectangle the graph is evaluated over and
// the size of every intermediate buffer.
type Filter struct {
	ReferenceBox image.Rectangle
	FilterRegion image.Rectangle

	Source      *SourceGraphic
	SourceAlpha *SourceAlpha

	// LastEffect is the graph root — the effect whose output is the filter
	// result. For a chained CSS filter list it is the final function.
	LastEffect FilterEffect
}

// SetSourceImage installs the rendered element buffer as the SourceGraphic's
// output. img must be sized to FilterRegion.
func (f *Filter) SetSourceImage(img *image.RGBA) {
	if f.Source != nil {
		f.Source.image = img
	}
}

// Apply evaluates the graph and returns the filter result in the last effect's
// operating space, sized to FilterRegion.
func (f *Filter) Apply() *image.RGBA {
	if f.LastEffect == nil || f.FilterRegion.Empty() {
		return nil
	}
	memo := make(map[FilterEffect]*image.RGBA)
	return f.evaluate(f.LastEffect, memo)
}

// ApplyToSRGB evaluates the graph and converts the result to premultiplied
// sRGB, ready for compositing with the page. SVG filters default to linearRGB
// (color-interpolation-filters:linearRGB); the final output must be converted
// back to sRGB before DrawImage composites it over the page content.
// Mirrors Blink's FilterPainter::PaintFilteredContent post-conversion step.
func (f *Filter) ApplyToSRGB() *image.RGBA {
	out := f.Apply()
	if out == nil || f.LastEffect == nil {
		return out
	}
	if f.LastEffect.OperatingSpace() == InterpolationSpaceSRGB {
		return out
	}
	converted := image.NewRGBA(out.Bounds())
	copy(converted.Pix, out.Pix)
	convertImageSpace(converted, f.LastEffect.OperatingSpace(), InterpolationSpaceSRGB)
	return converted
}

func (f *Filter) evaluate(e FilterEffect, memo map[FilterEffect]*image.RGBA) *image.RGBA {
	if cached, ok := memo[e]; ok {
		return cached
	}
	// Evaluate inputs, then convert each into this effect's operating space.
	deps := e.InputEffects()
	inputs := make([]*image.RGBA, len(deps))
	for i, dep := range deps {
		depImg := f.evaluate(dep, memo)
		if depImg != nil && dep.OperatingSpace() != e.OperatingSpace() {
			// Copy before converting so a shared dependency image is not
			// mutated for another consumer in a different space.
			cp := image.NewRGBA(depImg.Bounds())
			copy(cp.Pix, depImg.Pix)
			convertImageSpace(cp, dep.OperatingSpace(), e.OperatingSpace())
			depImg = cp
		}
		inputs[i] = depImg
	}
	out := e.ApplyEffect(inputs, f.FilterRegion)
	// Tainting is metadata only — propagated by
	// SVGFilterBuilder.BuildGraph (mirrors svg_filter_builder.cc:191-192).
	// The ONLY primitive whose OUTPUT changes when tainted is
	// feDisplacementMap, which becomes a pass-through; see
	// FEDisplacementMap.ApplyEffect (mirrors Blink
	// fe_displacement_map.cc::CreateImageFilter's early return on
	// `InputEffect(1)->OriginTainted()`). All other primitives produce
	// normal output regardless of the bit.
	memo[e] = out
	return out
}

// MapRect runs the graph's forward bounds mapping: starting from r (typically
// the reference box) it walks the effect chain applying each effect's MapRect
// so the caller can size the offscreen buffer to cover everything the filter
// will draw. For a DAG it unions the mapped rects of all paths.
func (f *Filter) MapRect(r image.Rectangle) image.Rectangle {
	if f.LastEffect == nil {
		return r
	}
	return mapRectThrough(f.LastEffect, r)
}

func mapRectThrough(e FilterEffect, r image.Rectangle) image.Rectangle {
	deps := e.InputEffects()
	if len(deps) == 0 {
		// A source effect: its output is r itself (SourceGraphic/SourceAlpha)
		// or, for generators like feFlood with no input, the effect decides.
		return e.MapRect(r)
	}
	var acc image.Rectangle
	first := true
	for _, dep := range deps {
		dr := mapRectThrough(dep, r)
		if first {
			acc = dr
			first = false
		} else {
			acc = acc.Union(dr)
		}
	}
	return e.MapRect(acc)
}
