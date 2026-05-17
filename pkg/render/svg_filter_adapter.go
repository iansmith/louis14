package render

import (
	"image"
	"strconv"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/graphics/filters"
	"louis14/pkg/layout/svg"
)

// svgFilterElementAdapter is the render-side bridge between
// svg.SVGResourceFilter (the parsed `<filter>` element) and the
// filters.SVGFilterElement interface consumed by SVGFilterBuilder.
// Mirrors how Blink's SVGFilterBuilder receives a SVGFilterElement
// reference + a separately-resolved viewport + reference box.
//
// The adapter is stateless — it carries pre-computed coordinates
// (filter region + reference box in device pixels) and the resolved
// interpolation space; everything else routes back to the
// SVGResourceFilter and its primitive ElementAdapter children.
type svgFilterElementAdapter struct {
	filter       *svg.SVGResourceFilter
	filterRegion image.Rectangle
	referenceBox image.Rectangle
	space        filters.InterpolationSpace
	// resolveStyle is called for each filter-primitive ElementAdapter
	// so the adapter can read presentational attributes (flood-color,
	// flood-opacity) through the cascade. May be nil — in that case
	// the adapter falls back to raw attribute lookup + SVG defaults.
	resolveStyle func(svg.ElementAdapter) *css.Style
}

// FilterRegion implements filters.SVGFilterElement.
func (a *svgFilterElementAdapter) FilterRegion() image.Rectangle { return a.filterRegion }

// ReferenceBox implements filters.SVGFilterElement.
func (a *svgFilterElementAdapter) ReferenceBox() image.Rectangle { return a.referenceBox }

// PrimitiveUnitsObjectBoundingBox implements filters.SVGFilterElement.
func (a *svgFilterElementAdapter) PrimitiveUnitsObjectBoundingBox() bool {
	return a.filter != nil && a.filter.PrimitiveUnits == svg.SVGUnitObjectBoundingBox
}

// InterpolationSpace implements filters.SVGFilterElement.
func (a *svgFilterElementAdapter) InterpolationSpace() filters.InterpolationSpace {
	return a.space
}

// FillPaintEffect implements filters.SVGFilterElement. Returns the
// referencing element's resolved fill paint as a PaintFilterEffect, or
// nil when the element has no fill (`fill="none"` or no fill at all).
//
// Phase 1 ships the interface point but always returns nil — no
// bucket-J test exercises FillPaint directly, so the adapter does not
// yet plumb the referencing element's resolved fill from the layout
// tree. Phase 8 wires the actual resolution. Until then,
// SVGFilterBuilder falls back to the default SourceGraphic alias for
// the FillPaint builtin.
func (a *svgFilterElementAdapter) FillPaintEffect() filters.FilterEffect { return nil }

// StrokePaintEffect implements filters.SVGFilterElement. Counterpart
// to FillPaintEffect for the element's stroke paint. Phase 1 returns
// nil for the same reason FillPaintEffect does — see its doc.
func (a *svgFilterElementAdapter) StrokePaintEffect() filters.FilterEffect { return nil }

// Primitives implements filters.SVGFilterElement. Wraps each filter
// primitive ElementAdapter in an svgFilterPrimitiveAdapter so the
// builder can read its attributes through the
// filters.SVGFilterPrimitive interface.
func (a *svgFilterElementAdapter) Primitives() []filters.SVGFilterPrimitive {
	if a.filter == nil {
		return nil
	}
	out := make([]filters.SVGFilterPrimitive, 0, len(a.filter.FePrimitives))
	for _, p := range a.filter.FePrimitives {
		out = append(out, &svgFilterPrimitiveAdapter{
			elt:          p,
			resolveStyle: a.resolveStyle,
		})
	}
	return out
}

// svgFilterPrimitiveAdapter wraps an svg.ElementAdapter (the
// underlying DOM element view) as a filters.SVGFilterPrimitive so the
// SVGFilterBuilder can consume it without importing pkg/layout/svg.
type svgFilterPrimitiveAdapter struct {
	elt          svg.ElementAdapter
	resolveStyle func(svg.ElementAdapter) *css.Style
}

// TagName implements filters.SVGFilterPrimitive.
func (p *svgFilterPrimitiveAdapter) TagName() string {
	if p.elt == nil {
		return ""
	}
	return p.elt.TagName()
}

// Attribute implements filters.SVGFilterPrimitive.
func (p *svgFilterPrimitiveAdapter) Attribute(name string) (string, bool) {
	if p.elt == nil {
		return "", false
	}
	return p.elt.Attribute(name)
}

// Children implements filters.SVGFilterPrimitive.
func (p *svgFilterPrimitiveAdapter) Children() []filters.SVGFilterPrimitive {
	if p.elt == nil {
		return nil
	}
	kids := p.elt.SVGChildren()
	out := make([]filters.SVGFilterPrimitive, 0, len(kids))
	for _, c := range kids {
		out = append(out, &svgFilterPrimitiveAdapter{
			elt:          c,
			resolveStyle: p.resolveStyle,
		})
	}
	return out
}

// TaintsOrigin implements filters.SVGFilterPrimitive. Reports whether
// this primitive originates origin-tainting (the bit then propagates
// through the filter graph via SVGFilterBuilder.BuildGraph, and the
// only consumer is feDisplacementMap which becomes a pass-through —
// see FEDisplacementMap.ApplyEffect).
//
// Tainting sources currently recognised:
//
//   - `<feFlood flood-color="currentcolor">`: reads the element's CSS
//     `color` property, which can be set by `:visited` link styling — a
//     known browser-history fingerprinting side channel. Marking the
//     primitive tainted causes a downstream feDisplacementMap to skip
//     the displacement (returning in1 unchanged) so the tainted colour
//     value is never read in a way that affects observable pixels.
//
//   - `<feImage>` with cross-origin href without CORS: handled by a
//     separate adapter override when feImage lands (Phase 4.2, out of
//     scope for this phase).
//
// Comparison is case-insensitive on both the tag name and the
// `currentcolor` keyword (CSS keywords are ASCII case-insensitive).
// Style lookup is preferred over raw attribute since the cascade may
// have overridden the attribute; raw attribute is the fallback when
// no style resolver is wired (e.g. unit tests).
func (p *svgFilterPrimitiveAdapter) TaintsOrigin() bool {
	if p.elt == nil {
		return false
	}
	if !strings.EqualFold(p.elt.TagName(), "feflood") {
		return false
	}
	// flood-color from style first, then raw attribute.
	var v string
	if p.resolveStyle != nil {
		if s := p.resolveStyle(p.elt); s != nil {
			if sv, ok := s.Get("flood-color"); ok {
				v = sv
			}
		}
	}
	if v == "" {
		if av, ok := p.elt.Attribute("flood-color"); ok {
			v = av
		}
	}
	return strings.EqualFold(strings.TrimSpace(v), "currentcolor")
}

// FloodColor implements filters.SVGFilterPrimitive. Reads
// flood-color (default black) and flood-opacity (default 1) from the
// primitive's resolved style if available, falling back to raw
// attribute lookup, then to SVG defaults.
//
// Per SVG Filter Effects 1 §15.7.4 / §15.7.16:
//   - flood-color: <color> — default rgb(0,0,0) (opaque black).
//   - flood-opacity: <number-or-percentage> — default 1.
func (p *svgFilterPrimitiveAdapter) FloodColor() (uint8, uint8, uint8, float64) {
	// SVG defaults.
	r, g, b := uint8(0), uint8(0), uint8(0)
	opacity := 1.0

	var style *css.Style
	if p.resolveStyle != nil && p.elt != nil {
		style = p.resolveStyle(p.elt)
	}

	// Style lookup (style wins, then attribute, then default — but
	// since applyPresentationalAttributes folds attributes into the
	// style before user CSS, the style usually has both).
	readColor := func(val string) bool {
		c, ok := css.ParseColor(val)
		if !ok {
			return false
		}
		r, g, b = c.R, c.G, c.B
		// ParseColor returns A in 0..1; carry it forward as a
		// pre-multiplier on flood-opacity (CSS treats them as
		// compounding).
		opacity *= c.A
		return true
	}
	readOpacity := func(val string) bool {
		o, ok := parseFloodOpacityValue(val)
		if !ok {
			return false
		}
		opacity = o
		return true
	}

	// Read opacity first so a flood-color with alpha can override.
	if style != nil {
		if v, ok := style.Get("flood-opacity"); ok {
			readOpacity(v)
		}
	}
	if p.elt != nil {
		if v, ok := p.elt.Attribute("flood-opacity"); ok {
			if style == nil {
				readOpacity(v)
			}
		}
	}
	if style != nil {
		if v, ok := style.Get("flood-color"); ok {
			readColor(v)
		}
	}
	if p.elt != nil {
		if v, ok := p.elt.Attribute("flood-color"); ok {
			// Only if style didn't supply one (style wins).
			if style == nil {
				readColor(v)
			}
		}
	}
	// Clamp opacity to [0, 1].
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}
	return r, g, b, opacity
}

// parseFloodOpacityValue parses a CSS <number-or-percentage> as used
// by `flood-opacity`. Returns (value, true) on success or (1, false)
// on parse failure. Mirrors the same parser CSS uses for opacity:
// numbers in [0, 1] and percentages in [0%, 100%].
func parseFloodOpacityValue(val string) (float64, bool) {
	val = strings.TrimSpace(val)
	if val == "" {
		return 1, false
	}
	if strings.HasSuffix(val, "%") {
		num, err := strconv.ParseFloat(strings.TrimSuffix(val, "%"), 64)
		if err != nil {
			return 1, false
		}
		return num / 100.0, true
	}
	num, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 1, false
	}
	return num, true
}
