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
	// subregionViewport is the device-pixel rect whose width/height are
	// the percentage basis for filter primitive subregion attrs in
	// primitiveUnits=userSpaceOnUse (per SVG Filter Effects 1 §7.11,
	// these resolve against the nearest viewport, NOT the filter region).
	// When zero it falls back to filterRegion via SubregionViewport(),
	// preserving the historical behaviour for callers that haven't set
	// it. The CSS `filter: url(...)` HTML-box path sets it to the
	// element's reference (border) box.
	subregionViewport image.Rectangle
	// userSpaceOrigin is the SVG element's user-space (0, 0) point
	// mapped to absolute device pixels. The userSpaceOnUse subregion
	// anchor for filter primitives uses this — NOT filterRegion.Min,
	// which can include the default 10% filter-region expansion.
	// Populated by callers (svg_filter_painter.go via the SVG paint
	// context's DeviceFromUser transform; filter_effect_builder.go
	// via the reference box origin for CSS filter: url(...) — no
	// SVG transform exists in that path).
	userSpaceOrigin image.Point
	space           filters.InterpolationSpace
	// resolveStyle is called for each filter-primitive ElementAdapter
	// so the adapter can read presentational attributes (flood-color,
	// flood-opacity) through the cascade. May be nil — in that case
	// the adapter falls back to raw attribute lookup + SVG defaults.
	resolveStyle func(svg.ElementAdapter) *css.Style

	// resources is the SVG resource registry used by `<feImage>` to
	// resolve internal `href="#id"` references via
	// SVGResourceRegistry.LookupElement (which covers ALL id-bearing
	// elements, not just gradient/filter resources). Mirrors Blink's
	// SVGFEImageElement::ReferencedLayoutObject (svg_fe_image_element.cc)
	// which resolves against the tree scope of the feImage element. May
	// be nil — feImage then falls back to transparent output for
	// internal hrefs.
	resources *svg.SVGResourceRegistry

	// imageFetcher loads external images referenced by `<feImage
	// href="path.png">` or data: URLs. May be nil — feImage then
	// falls back to transparent output for external hrefs.
	imageFetcher func(string) ([]byte, error)

	// externalSVGFetcher loads external SVG documents referenced by
	// `<feImage href="path.svg">` or `<feImage href="path.svg#id">`.
	// May be nil — feImage then falls back to the imageFetcher for
	// those URIs (which may also fail if the content type is SVG and
	// the image decoder doesn't handle it).
	externalSVGFetcher func(string) ([]byte, error)
}

// FilterRegion implements filters.SVGFilterElement.
func (a *svgFilterElementAdapter) FilterRegion() image.Rectangle { return a.filterRegion }

// ReferenceBox implements filters.SVGFilterElement.
func (a *svgFilterElementAdapter) ReferenceBox() image.Rectangle { return a.referenceBox }

// SubregionViewport implements filters.SVGFilterElement. Returns the
// explicit subregion viewport when set, otherwise falls back to the
// filter region (the historical basis, exact whenever region == viewport,
// which holds for the SVG-element filter paths).
func (a *svgFilterElementAdapter) SubregionViewport() image.Rectangle {
	if a.subregionViewport.Empty() {
		return a.filterRegion
	}
	return a.subregionViewport
}

// UserSpaceOrigin implements filters.SVGFilterElement.
func (a *svgFilterElementAdapter) UserSpaceOrigin() image.Point { return a.userSpaceOrigin }

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
			elt:                p,
			resolveStyle:       a.resolveStyle,
			space:              a.space,
			resources:          a.resources,
			imageFetcher:       a.imageFetcher,
			externalSVGFetcher: a.externalSVGFetcher,
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

	// space is the filter's interpolation space (the parent
	// `<filter>`'s color-interpolation-filters, linearRGB by default).
	// Propagated from the svgFilterElementAdapter so `<feImage>` can
	// convert its sourced sRGB image INTO the operating space — Blink's
	// FilterEffect framework runs the same conversion on the FEImage
	// result (TransformResultIfNeeded, svg_fe_image.cc). Without it the
	// raw sRGB source bytes are mislabelled as linearRGB and the final
	// composite double-applies a linear→sRGB transfer.
	space filters.InterpolationSpace

	// resources / imageFetcher / externalSVGFetcher are propagated
	// from the parent svgFilterElementAdapter so the feImage adapter
	// can resolve internal `href="#id"` and external href references
	// without a back-reference to the host renderer. See the parent
	// struct's fields for the full doc.
	resources          *svg.SVGResourceRegistry
	imageFetcher       func(string) ([]byte, error)
	externalSVGFetcher func(string) ([]byte, error)
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
			elt:                c,
			resolveStyle:       p.resolveStyle,
			space:              p.space,
			resources:          p.resources,
			imageFetcher:       p.imageFetcher,
			externalSVGFetcher: p.externalSVGFetcher,
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
//   - `<feDropShadow flood-color="currentcolor">`: per Blink
//     SVGFEDropShadowElement::TaintsOrigin
//     (third_party/blink/renderer/core/svg/svg_fe_drop_shadow_element.cc),
//     which checks style->FloodColor().DependsOnCurrentColor(). Same
//     reasoning as feFlood — flood-color reads CSS `color` which can
//     be set by `:visited`. Without this, tainting-fedropshadow-002.html
//     (currentcolor flood-color) leaks the colour through the displaced
//     output.
//
//   - `<feDiffuseLighting lighting-color="currentcolor">` /
//     `<feSpecularLighting lighting-color="currentcolor">`: same
//     reasoning as feFlood — lighting-color reads CSS `color` which
//     can be set by `:visited`. Spec rule per Filter Effects 1
//     §"tainted filter primitives" + Blink
//     SVGFEDiffuseLightingElement::TaintsOrigin /
//     SVGFESpecularLightingElement::TaintsOrigin.
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
	tag := p.elt.TagName()
	switch {
	case strings.EqualFold(tag, "feflood"),
		strings.EqualFold(tag, "fedropshadow"):
		return p.readColorAttr("flood-color") == "currentcolor"
	case strings.EqualFold(tag, "fediffuselighting"),
		strings.EqualFold(tag, "fespecularlighting"):
		return p.readColorAttr("lighting-color") == "currentcolor"
	case strings.EqualFold(tag, "feimage"):
		// feImage taints when the resolved source is cross-origin
		// without CORS. The bucket-J feimage tests all reference
		// same-origin support files or in-page IDs, so this returns
		// false in practice — but the placeholder is here so a
		// downstream feDisplacementMap consuming a cross-origin
		// feImage degrades to passthrough, matching Blink
		// SVGFEImageElement::TaintsOrigin. A future cross-origin
		// hostile-content test would flip this on.
		return false
	}
	return false
}

// readColorAttr fetches a colour-valued attribute (flood-color,
// lighting-color, …) following the CSS cascade precedence:
//
//  1. Resolved computed style (when a style resolver is wired). In
//     production this is currently nil — FilterEffectBuilder.ResolveStyle
//     is declared but not wired (see render.go) — so we fall through.
//  2. Inline `style` HTML attribute on the primitive. This is the
//     bridge for JS-driven mutations (`element.style.floodColor = ...`)
//     and for hand-authored `style="flood-color: ..."`. Per CSS the
//     inline style cascades above the presentation attribute.
//  3. Presentation attribute (e.g. `flood-color="..."`).
//
// Returns the lowercased+trimmed value or "" when unset. Used by
// TaintsOrigin to detect the `currentcolor` keyword case-insensitively.
func (p *svgFilterPrimitiveAdapter) readColorAttr(name string) string {
	var v string
	if p.resolveStyle != nil {
		if s := p.resolveStyle(p.elt); s != nil {
			if sv, ok := s.Get(name); ok {
				v = sv
			}
		}
	}
	if v == "" {
		if sv, ok := readInlineStyleProperty(p.elt, name); ok {
			v = sv
		}
	}
	if v == "" {
		if av, ok := p.elt.Attribute(name); ok {
			v = av
		}
	}
	return strings.ToLower(strings.TrimSpace(v))
}

// readInlineStyleProperty extracts a CSS declaration value from the
// element's `style` HTML attribute. Returns ("", false) when the
// attribute is unset or the property is not present.
//
// This is a minimal CSS declaration-list parser: declarations are
// split on `;`, each `<name>:<value>` pair is matched
// case-insensitively on the property name. Whitespace around the
// name and value is trimmed. Values containing semicolons (none of
// our colour properties produce that) are not supported — they would
// be split incorrectly, but flood-color / lighting-color / opacity
// values never include `;`.
//
// Mirrors what the CSS cascade does for the `style` attribute in
// real browsers, scoped down to the lookup we need here.
func readInlineStyleProperty(elt svg.ElementAdapter, name string) (string, bool) {
	if elt == nil {
		return "", false
	}
	raw, ok := elt.Attribute("style")
	if !ok || raw == "" {
		return "", false
	}
	for _, decl := range strings.Split(raw, ";") {
		colon := strings.IndexByte(decl, ':')
		if colon < 0 {
			continue
		}
		prop := strings.TrimSpace(decl[:colon])
		if !strings.EqualFold(prop, name) {
			continue
		}
		val := strings.TrimSpace(decl[colon+1:])
		return val, true
	}
	return "", false
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

// ---- Lighting primitive attribute exposure (feDiffuseLighting +
// feSpecularLighting + their light-source children) ----
//
// Phase 6 of LOU-129: only meaningful for the two lighting primitives;
// other primitive tags inherit default values (white lighting-color,
// SurfaceScale=1, …) which are harmless because the builder only
// reads them inside the `fediffuselighting`/`fespecularlighting`
// cases.

// LightingColor implements filters.SVGFilterPrimitive. Reads the
// `lighting-color` property from style first, then from raw
// attribute, then defaults to white per SVG Filter Effects 1
// §"feDiffuseLighting" / §"feSpecularLighting". Returns RGB in
// 0..255 and A in [0, 1].
//
// Pattern mirrors FloodColor() for `<feFlood>`.
func (p *svgFilterPrimitiveAdapter) LightingColor() (uint8, uint8, uint8, float64) {
	// SVG default: white opaque.
	r, g, b := uint8(255), uint8(255), uint8(255)
	a := 1.0
	if p.elt == nil {
		return r, g, b, a
	}
	var style *css.Style
	if p.resolveStyle != nil {
		style = p.resolveStyle(p.elt)
	}
	readColor := func(val string) bool {
		c, ok := css.ParseColor(val)
		if !ok {
			return false
		}
		r, g, b = c.R, c.G, c.B
		a = c.A
		return true
	}
	if style != nil {
		if v, ok := style.Get("lighting-color"); ok {
			readColor(v)
		}
	}
	if style == nil {
		if v, ok := p.elt.Attribute("lighting-color"); ok {
			readColor(v)
		}
	}
	return r, g, b, a
}

// SurfaceScale implements filters.SVGFilterPrimitive. Reads the
// `surfaceScale` attribute; default 1 per SVG Filter Effects 1.
func (p *svgFilterPrimitiveAdapter) SurfaceScale() float64 {
	return p.readFloatAttr("surfaceScale", 1.0)
}

// DiffuseConstant implements filters.SVGFilterPrimitive. Reads the
// `diffuseConstant` attribute on `<feDiffuseLighting>`; default 1
// per SVG Filter Effects 1.
func (p *svgFilterPrimitiveAdapter) DiffuseConstant() float64 {
	return p.readFloatAttr("diffuseConstant", 1.0)
}

// SpecularConstant implements filters.SVGFilterPrimitive. Reads the
// `specularConstant` attribute on `<feSpecularLighting>`; default 1.
func (p *svgFilterPrimitiveAdapter) SpecularConstant() float64 {
	return p.readFloatAttr("specularConstant", 1.0)
}

// SpecularExponent implements filters.SVGFilterPrimitive. Reads the
// `specularExponent` attribute on `<feSpecularLighting>`; default 1.
// FESpecularLighting itself clamps values < 1 up to 1.
func (p *svgFilterPrimitiveAdapter) SpecularExponent() float64 {
	return p.readFloatAttr("specularExponent", 1.0)
}

// KernelUnitLength implements filters.SVGFilterPrimitive. Reads the
// `kernelUnitLength` attribute as one or two numbers (X-only or X-Y).
// Default (1, 1) per SVG Filter Effects 1.
func (p *svgFilterPrimitiveAdapter) KernelUnitLength() (float64, float64) {
	if p.elt == nil {
		return 1, 1
	}
	v, ok := p.elt.Attribute("kernelUnitLength")
	if !ok {
		v, ok = p.elt.Attribute("kernelunitlength")
	}
	if !ok {
		return 1, 1
	}
	parts := strings.FieldsFunc(v, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
	if len(parts) == 0 {
		return 1, 1
	}
	x, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 1, 1
	}
	if len(parts) == 1 {
		return x, x
	}
	y, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return x, x
	}
	return x, y
}

// LightSource implements filters.SVGFilterPrimitive. Walks the
// primitive's children, finds the first `<feDistantLight>`,
// `<fePointLight>`, or `<feSpotLight>` child, parses its attributes,
// and returns the appropriate filters.LightSource. Returns nil when
// no light-source child is found — the lighting primitive then
// produces transparent black per SVG Filter Effects 1 §"Lighting
// elements with no light source" (mirrors Blink
// FELighting::PaintingData fallthrough).
func (p *svgFilterPrimitiveAdapter) LightSource() filters.LightSource {
	if p.elt == nil {
		return nil
	}
	for _, c := range p.elt.SVGChildren() {
		switch strings.ToLower(c.TagName()) {
		case "fedistantlight":
			az := readFloatAttrOn(c, "azimuth", 0)
			el := readFloatAttrOn(c, "elevation", 0)
			return filters.NewDistantLightSource(az, el)
		case "fepointlight":
			x := readFloatAttrOn(c, "x", 0)
			y := readFloatAttrOn(c, "y", 0)
			z := readFloatAttrOn(c, "z", 0)
			return filters.NewPointLightSource(x, y, z)
		case "fespotlight":
			x := readFloatAttrOn(c, "x", 0)
			y := readFloatAttrOn(c, "y", 0)
			z := readFloatAttrOn(c, "z", 0)
			pax := readFloatAttrOn(c, "pointsAtX", 0)
			pay := readFloatAttrOn(c, "pointsAtY", 0)
			paz := readFloatAttrOn(c, "pointsAtZ", 0)
			se := readFloatAttrOn(c, "specularExponent", 1)
			lca := readFloatAttrOn(c, "limitingConeAngle", 90)
			return filters.NewSpotLightSource(x, y, z, pax, pay, paz, se, lca)
		}
	}
	return nil
}

// readFloatAttr fetches a single float attribute from the primitive's
// element with both camelCase and lowercase fallback (HTML tokenizer
// lowercases SVG attribute names; some adapters preserve camelCase).
// Returns def when the attribute is missing or malformed.
func (p *svgFilterPrimitiveAdapter) readFloatAttr(name string, def float64) float64 {
	if p.elt == nil {
		return def
	}
	return readFloatAttrOn(p.elt, name, def)
}

// readFloatAttrOn is the same as readFloatAttr but works on any
// svg.ElementAdapter (used for light-source child elements).
func readFloatAttrOn(elt svg.ElementAdapter, name string, def float64) float64 {
	v, ok := elt.Attribute(name)
	if !ok {
		v, ok = elt.Attribute(strings.ToLower(name))
	}
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return f
}
