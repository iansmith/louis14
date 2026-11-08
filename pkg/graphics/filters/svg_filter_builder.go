// svg_filter_builder.go is the SVG `<filter>` → FilterEffect graph
// builder, mirroring Blink's core/svg/graphics/filters/svg_filter_builder.{h,cc}.
//
// This is the ONE file pkg/graphics/filters/ gains for the SVG
// foundation. The FilterEffect graph types (Filter, FilterEffect,
// FEFlood, FEGaussianBlur, FEColorMatrix, FEComponentTransfer,
// FEMerge, FEOffset, FEDropShadow, FEBlend) live in this package
// already; this builder consumes them. It does NOT import
// pkg/layout/svg (which would create a cycle); instead it operates on
// an abstract `SVGFilterElement` view supplied by the caller. The
// concrete view in pkg/render wraps the *svg.SVGResourceFilter and
// each filter primitive child to provide attribute lookup + style.

package filters

import (
	"image"
	"math"
	"strconv"
	"strings"

	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// SVGFilterPrimitive is the abstract view of a single filter primitive
// element (`<feFlood>`, `<feGaussianBlur>`, …) the SVGFilterBuilder
// consumes. Mirrors the surface the Blink builder reads off
// SVGFilterPrimitiveStandardAttributes.
//
// The lighting-specific methods (LightingColor, SurfaceScale,
// DiffuseConstant, SpecularConstant, SpecularExponent,
// KernelUnitLength, LightSource) are only meaningful for
// `<feDiffuseLighting>` / `<feSpecularLighting>`. Other primitive
// types may return whatever defaults their adapter prefers — the
// builder only reads them inside the `fediffuselighting` /
// `fespecularlighting` cases.
type SVGFilterPrimitive interface {
	// TagName returns the primitive's tag (`"feflood"`, `"feblend"`, …)
	// in the HTML tokenizer's lowercased form.
	TagName() string
	// Attribute returns the named attribute value and whether it was
	// set. HTML's tokenizer lowercases attribute names — callers must
	// already be using the lowercase form (the existing
	// svg.ElementAdapter convention).
	Attribute(name string) (string, bool)
	// Children returns child filter primitives (for `<feMerge>`'s
	// `<feMergeNode>` children, mainly).
	Children() []SVGFilterPrimitive
	// FloodColor returns the resolved flood-color (R, G, B in 0–255
	// and A in [0,1]). Used by `<feFlood>` and `<feDropShadow>`.
	// Default: black, opaque (per SVG Filter Effects 1).
	FloodColor() (r, g, b uint8, a float64)
	// TaintsOrigin reports whether this primitive seeds tainting into
	// the chain — i.e. whether its content originates from a
	// cross-origin source without CORS. Default false; only
	// `<feImage>` overrides to return true when its image source is
	// cross-origin without CORS. Mirrors Blink
	// SVGFEImageElement::TaintsOrigin (svg_fe_image_element.cc).
	//
	// All other primitives inherit the default — they don't introduce
	// new tainting, they only propagate input tainting via
	// FilterEffect.InputsTaintOrigin (handled by the builder; see
	// svg_filter_builder.cc:191-192).
	TaintsOrigin() bool

	// LightingColor returns the resolved lighting-color (R, G, B in
	// 0–255 and A in [0,1]) for `<feDiffuseLighting>` /
	// `<feSpecularLighting>`. Default: white opaque (1, 1, 1, 1) per
	// SVG Filter Effects 1.
	LightingColor() (r, g, b uint8, a float64)
	// SurfaceScale returns the `surfaceScale` attribute (default 1).
	SurfaceScale() float64
	// DiffuseConstant returns the `diffuseConstant` attribute on
	// `<feDiffuseLighting>` (default 1).
	DiffuseConstant() float64
	// SpecularConstant returns the `specularConstant` attribute on
	// `<feSpecularLighting>` (default 1).
	SpecularConstant() float64
	// SpecularExponent returns the `specularExponent` attribute on
	// `<feSpecularLighting>` (default 1).
	SpecularExponent() float64
	// KernelUnitLength returns the `kernelUnitLength` attribute as
	// (X, Y) (default 1, 1).
	KernelUnitLength() (x, y float64)
	// LightSource returns the resolved light source from the first
	// `<feDistantLight>` / `<fePointLight>` / `<feSpotLight>` child,
	// or nil when no light-source child is present (the lighting
	// primitive then produces transparent black per spec).
	LightSource() LightSource

	// ResolveImageSource returns a pre-rasterised premultiplied RGBA
	// for an `<feImage>` primitive given the filter region and the
	// primitive's resolved subregion (both in layer pixels).
	// Returns a nil image for primitives that aren't feImage or for an
	// feImage whose href can't be resolved (matches the spec's
	// "transparent black" fallback for unresolved tspan / use references
	// and missing external resources). Mirrors Blink
	// FEImage::CreateImageFilter's dispatch between
	// CreateImageFilterForLayoutObject (internal `href="#id"`) and the
	// fetched image bitmap path (external href).
	//
	// Only consulted inside the `feimage` builder case; other primitive
	// adapters may return (nil, image.Rectangle{}, false).
	ResolveImageSource(filterRegion, subregion image.Rectangle) (img *image.RGBA, target image.Rectangle, preserveAspectRatioNone bool)
}

// SVGFilterElement is the abstract view of a `<filter>` element the
// SVGFilterBuilder consumes. The caller (pkg/render) wraps the
// concrete svg.SVGResourceFilter behind this interface so this
// package stays decoupled from pkg/layout/svg.
type SVGFilterElement interface {
	// FilterRegion returns the resolved filter region in layer pixels
	// (the rect every fe* primitive's output covers).
	FilterRegion() image.Rectangle
	// ParamFilterRegion returns the filter region in parameter space,
	// unsnapped, as Blink's Filter::FilterRegion (a gfx::RectF in the
	// element's local space): a missing userSpaceOnUse subregion attribute
	// takes its edge.
	ParamFilterRegion() geometry.RectF
	// SubregionViewport returns the parameter-space rect whose width/height
	// are the percentage basis for filter primitive subregion
	// (x/y/width/height) attributes in primitiveUnits=userSpaceOnUse.
	// Per SVG Filter Effects 1 §7.11 these percentages resolve against
	// the nearest viewport (Blink's SVGViewportResolver), NOT the filter
	// region — which differs from the viewport when the region carries
	// the default 10% expansion. For a CSS `filter: url(...)` on an HTML
	// box the viewport is the element's border (reference) box; for an
	// SVG-element filter it is the host SVG viewport. Adapters that don't
	// distinguish the two may return the filter region (the historical
	// behaviour, exact whenever region == viewport); an empty one falls
	// back to it.
	SubregionViewport() geometry.RectF
	// ReferenceBox returns the referencing element's reference box in
	// parameter space, unsnapped, as Blink's Filter::ReferenceBox (a
	// gfx::RectF in the element's local space). For objectBoundingBox
	// primitive units the builder scales primitive subregions, lengths and
	// light positions by it.
	ReferenceBox() geometry.RectF
	// UserSpaceOrigin returns the point userSpaceOnUse primitive
	// coordinates are relative to, in parameter space: an SVG element's
	// user-space (0, 0), or a CSS box's local origin. Not
	// FilterRegion().Min, which can include the filter region's default
	// 10% expansion.
	UserSpaceOrigin() geometry.PointF
	// Mapping maps parameter space (where ReferenceBox, SubregionViewport,
	// UserSpaceOrigin and the primitives' attributes live) into the layer
	// the graph runs in (FilterRegion's pixels), as Skia maps each image
	// filter's parameters through skif::Mapping::paramToLayer
	// (src/core/SkImageFilterTypes.h @ skia
	// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e). LOU-774.
	Mapping() Mapping
	// PrimitiveUnitsObjectBoundingBox reports whether primitiveUnits
	// is "objectBoundingBox" (true) or "userSpaceOnUse" (false).
	PrimitiveUnitsObjectBoundingBox() bool
	// InterpolationSpace returns the effect operating space (linearRGB
	// default per SVG; the wrapper reads color-interpolation-filters).
	InterpolationSpace() InterpolationSpace
	// PrimitiveInterpolationSpace returns the operating space of the
	// primitive prim (Blink's ColorInterpolationForElement for it,
	// svg_filter_builder.cc:136-155, 189-192 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	PrimitiveInterpolationSpace(prim SVGFilterPrimitive) InterpolationSpace
	// Primitives returns the filter primitive children in DOM order.
	Primitives() []SVGFilterPrimitive
	// FillPaintEffect returns a FilterEffect that paints the
	// referencing element's resolved fill paint server into the
	// reference box — the SVG `FillPaint` builtin (Filter Effects 1
	// §15.7). Returns nil when the element has no fill (e.g.
	// `fill="none"`), in which case the builder leaves the FillPaint
	// builtin unset. Mirrors Blink
	// svg_filter_builder.cc:114-119 where FillPaint is registered
	// only when fill_flags is non-null.
	FillPaintEffect() FilterEffect
	// StrokePaintEffect returns the StrokePaint builtin counterpart.
	// nil when the element has no stroke. Mirrors Blink
	// svg_filter_builder.cc:120-125.
	StrokePaintEffect() FilterEffect
}

// SVGFilterBuilder turns a `<filter>` element + its children into a
// `*Filter` graph. Mirrors Blink's SVGFilterBuilder.
//
// Resolution order for `in`/`in2` attribute lookup (Blink
// SVGFilterBuilder::GetEffectById):
//
//  1. Built-in effects: "SourceGraphic", "SourceAlpha", "FillPaint",
//     "StrokePaint", "BackgroundImage", "BackgroundAlpha" (Phase 7
//     implements the two real ones — SourceGraphic, SourceAlpha — and
//     aliases the others to the closest builtin to avoid dropping
//     references).
//  2. Named results from prior primitives (the `result="…"` attribute
//     on each).
//  3. Fall back to the previous primitive's output (the "last effect"
//     — what Blink calls last_effect_).
//
// A primitive with no `in` attribute also falls through to (3): the
// chain wires implicitly through "the previous output".
type SVGFilterBuilder struct {
	source      *SourceGraphic
	sourceAlpha *SourceAlpha
	space       InterpolationSpace
	// builtins maps the SourceGraphic / SourceAlpha / Fill / Stroke /
	// Background names to their FilterEffect.
	builtins map[string]FilterEffect
	// namedResults accumulates `result="…"` names as we walk
	// primitives.
	namedResults map[string]FilterEffect
	// lastEffect is the most-recently-built primitive's output;
	// initialized to source so a chain that starts with no explicit
	// `in` reads from SourceGraphic.
	lastEffect FilterEffect
}

// NewSVGFilterBuilder creates a builder with the given operating
// space. The caller's SourceGraphic is built here so its image can be
// supplied later via Filter.SetSourceImage.
func NewSVGFilterBuilder(space InterpolationSpace) *SVGFilterBuilder {
	// SourceGraphic / SourceAlpha always operate in sRGB: they expose the
	// element's rendered DEVICE content, which is sRGB regardless of the
	// filter's color-interpolation-filters value. The graph then converts
	// sRGB->linearRGB at the edge into the first linearRGB primitive (see
	// Filter.evaluate). Mirrors Blink's SourceGraphic constructor, which
	// calls SetOperatingInterpolationSpace(kInterpolationSpaceSRGB)
	// unconditionally, and SourceAlpha, which inherits its source's space
	// (source_graphic.cc / source_alpha.cc @ Chromium main). Passing the
	// filter's `space` here instead would skip the input conversion while
	// the compositor still converts the output back, brightening colors
	// (green 128 -> ~188).
	src := NewSourceGraphic(InterpolationSpaceSRGB)
	srcAlpha := NewSourceAlpha(src, InterpolationSpaceSRGB)
	b := &SVGFilterBuilder{
		source:       src,
		sourceAlpha:  srcAlpha,
		space:        space,
		builtins:     make(map[string]FilterEffect),
		namedResults: make(map[string]FilterEffect),
		lastEffect:   src,
	}
	// Per SVG Filter Effects 1 §15.7 the standard input names. Match
	// case-insensitively (Blink's SVGFilterBuilder matches the
	// camelCase spellings). We store the canonical names; lookups
	// normalize via strings.ToLower-equivalent.
	b.builtins["SourceGraphic"] = src
	b.builtins["SourceAlpha"] = srcAlpha
	// FillPaint / StrokePaint default seed: SourceGraphic. The real
	// PaintFilterEffect registrations happen in BuildGraph once the
	// SVGFilterElement is in hand (which knows the referencing
	// element's resolved fill/stroke). If the element does not
	// supply a paint effect (no fill / no stroke), the SourceGraphic
	// alias remains — this matches Blink's behavior of falling back
	// to the spec's "transparent black" only when the paint server
	// is genuinely unavailable; with no explicit paint server, SVG
	// defaults to black fill, which SourceGraphic represents.
	b.builtins["FillPaint"] = src
	b.builtins["StrokePaint"] = src
	// BackgroundImage / BackgroundAlpha: modern Blink no longer
	// implements these (removed with the `enable-background` /
	// backdrop-snapshot machinery). Spec keeps them but UAs treat
	// them as transparent black / its alpha (which is the same
	// thing). louis14 mirrors modern Blink by aliasing them to
	// SourceGraphic / SourceAlpha as a defensive fallback — both
	// effectively give the spec's "transparent black" semantic when
	// no backdrop is recorded, since SourceGraphic for an element
	// without backdrop access just renders the element itself, and
	// SourceAlpha zeroes the colour channels.
	b.builtins["BackgroundImage"] = src
	b.builtins["BackgroundAlpha"] = srcAlpha
	return b
}

// SetSourceOverride rewires "SourceGraphic" / FillPaint / StrokePaint /
// BackgroundImage lookups (and the implicit chain head, `b.lastEffect`)
// to `override`. Used by CSS filter-value-list chains so each filter's
// output becomes the next filter's SourceGraphic.
//
// Blink does the same composition with a different shape: it passes the
// previous-stage effect as the `source_graphic` argument to a fresh
// SVGFilterBuilder constructor (`core/svg/graphics/filters/svg_filter_builder.cc:106-114`
// @ chromium-main bf955d02bf0b0c67868b2e62359c0af199af9acc), driven from
// `FilterEffectBuilder::BuildReferenceFilter`
// (`core/paint/filter_effect_builder.cc:511-559, 539-557` at the same
// SHA). louis14 uses in-place override because the builder is already
// constructed before the chain knows what previous-stage effect to feed
// in.
//
// One real divergence: SourceAlpha is NOT rewired here. Blink rebuilds
// SourceAlpha from the override (`svg_filter_builder.cc:113-114` at the
// same SHA: `MakeGarbageCollected<SourceAlpha>(source_graphic)`). louis14
// can't do that because `NewSourceAlpha` takes `*SourceGraphic` rather
// than `FilterEffect`. The gate WPT test svg-multiple-filter-functions
// doesn't read SourceAlpha inside a chained sub-filter, so the gap is
// observably benign there. Any future chained-filter test that
// references SourceAlpha downstream would expose it — the fix is to
// generalise NewSourceAlpha to accept FilterEffect, then rewire here.
//
// Must be called before BuildGraph. Nil override is a no-op.
func (b *SVGFilterBuilder) SetSourceOverride(override FilterEffect) {
	if override == nil {
		return
	}
	b.builtins["SourceGraphic"] = override
	b.builtins["FillPaint"] = override
	b.builtins["StrokePaint"] = override
	b.builtins["BackgroundImage"] = override
	// The implicit chain head also moves to the override so a primitive
	// with no `in` attribute reads from the running chain output.
	b.lastEffect = override
}

// Source returns the builder's SourceGraphic.
func (b *SVGFilterBuilder) Source() *SourceGraphic { return b.source }

// SourceAlpha returns the builder's SourceAlpha.
func (b *SVGFilterBuilder) SourceAlpha() *SourceAlpha { return b.sourceAlpha }

// LastEffect returns the most recently built primitive's output
// (initialized to SourceGraphic). Mirrors Blink's last_effect_.
func (b *SVGFilterBuilder) LastEffect() FilterEffect { return b.lastEffect }

// GetEffectByID resolves a string reference (an `in`/`in2` attribute
// value or a fragment id) following the standard order:
//
//	builtins → namedResults → lastEffect (default).
//
// An empty `id` returns `lastEffect`. An unknown id falls back to
// lastEffect (Blink's SVGFilterBuilder::GetEffectById same behavior:
// unknown ids resolve to the chain default).
func (b *SVGFilterBuilder) GetEffectByID(id string) FilterEffect {
	id = strings.TrimSpace(id)
	if id == "" {
		return b.lastEffect
	}
	if e, ok := b.builtins[id]; ok {
		return e
	}
	if e, ok := b.namedResults[id]; ok {
		return e
	}
	return b.lastEffect
}

// BuildGraph walks the filter primitive children, constructs the
// FilterEffect graph, and returns the assembled `*Filter` with its
// reference box, filter region, and last effect set. Mirrors Blink's
// SVGFilterBuilder::BuildGraph.
//
// The returned Filter is non-nil even when there are zero primitives
// — in that case LastEffect == SourceGraphic, which yields a
// passthrough (the source is the result).
func (b *SVGFilterBuilder) BuildGraph(elt SVGFilterElement) *Filter {
	if elt == nil {
		return nil
	}
	// Register FillPaint / StrokePaint from the referencing element if
	// available. Mirrors Blink svg_filter_builder.cc:114-125: Blink
	// only registers these when fill_flags / stroke_flags are non-null
	// (the element has resolved fill / stroke paint). louis14's
	// equivalent: the SVGFilterElement adapter returns a non-nil
	// FilterEffect (a PaintFilterEffect) when the element has a paint
	// to supply, nil otherwise. When nil, the default SourceGraphic
	// alias from NewSVGFilterBuilder stays in place.
	if fp := elt.FillPaintEffect(); fp != nil {
		b.builtins["FillPaint"] = fp
	}
	if sp := elt.StrokePaintEffect(); sp != nil {
		b.builtins["StrokePaint"] = sp
	}
	filterSpace := b.space
	defer func() { b.space = filterSpace }()
	for _, prim := range elt.Primitives() {
		b.space = elt.PrimitiveInterpolationSpace(prim)
		eff := b.buildOnePrimitive(elt, prim)
		if eff == nil {
			continue
		}
		// Tainted-origin propagation — mirrors Blink
		// svg_filter_builder.cc:191-192. Runs BEFORE subregion
		// wrapping so the SubregionClippedEffect inherits the bit
		// from the wrapped primitive. The two seed sources are: any
		// upstream effect already tainted (InputsTaintOrigin), or
		// this primitive itself originating tainting (TaintsOrigin
		// — only `feImage` returns true on cross-origin sources).
		if eff.InputsTaintOrigin() || prim.TaintsOrigin() {
			eff.SetOriginTainted()
		}
		// Apply the primitive subregion clip if x/y/width/height are
		// set. For FEFlood / FEImage we already pushed Subregion into
		// the effect itself (FEFlood uses it as the fill rect, FEImage
		// as the target rect for the source image), so don't
		// double-wrap. For all other primitives the subregion is a
		// generic post-evaluation clip per SVG Filter Effects 1 §15.7
		// "Filter primitive subregion".
		if prim.TagName() != "feflood" && prim.TagName() != "feimage" {
			if sub, set := resolvePrimitiveSubregion(elt, prim); set {
				wrapped := NewSubregionClippedEffect(eff, sub)
				// Subregion wrapper carries the same tainted bit
				// as the wrapped effect (it's a thin per-pixel
				// clip; the underlying tainting is unchanged).
				if eff.OriginTainted() {
					wrapped.SetOriginTainted()
				}
				eff = wrapped
			}
		}
		// `result="…"` registers the named output.
		if name, ok := prim.Attribute("result"); ok {
			name = strings.TrimSpace(name)
			if name != "" {
				b.namedResults[name] = eff
			}
		}
		b.lastEffect = eff
	}
	return &Filter{
		FilterRegion: elt.FilterRegion(),
		Source:       b.source,
		SourceAlpha:  b.sourceAlpha,
		LastEffect:   b.lastEffect,
	}
}

// buildOnePrimitive dispatches one filter primitive element to the
// corresponding FilterEffect constructor, wiring its `in`/`in2`
// inputs via GetEffectByID. Returns nil if the primitive isn't
// supported by Phase 7's primitive set.
func (b *SVGFilterBuilder) buildOnePrimitive(elt SVGFilterElement, prim SVGFilterPrimitive) FilterEffect {
	switch prim.TagName() {
	case "feflood":
		r, g, bb, a := prim.FloodColor()
		// feFlood is exempt from color-interpolation-filters: its flood
		// colour is emitted directly in the current (sRGB) colour space
		// regardless of the filter's operating space. Mirrors Blink, where
		// FEFlood overrides SetOperatingInterpolationSpace as a no-op so the
		// flood is never adapted to linearRGB (fe_flood.cc @ Chromium main).
		// Constructing it in sRGB makes the graph treat its output as sRGB:
		// when feFlood is the last effect the compositor leaves it alone (so
		// flood-color="green" round-trips to #008000, not #00BB00); when it
		// feeds a linearRGB primitive the graph converts sRGB->linearRGB at
		// that edge.
		fe := NewFEFlood(InterpolationSpaceSRGB, r, g, bb, a)
		sub, set := resolvePrimitiveSubregion(elt, prim)
		if set && sub.Empty() {
			// A subregion that resolves to nothing floods nothing.
			return NewSubregionClippedEffect(fe, sub)
		}
		if !set {
			// No explicit primitive subregion: feFlood covers the full
			// filter region per SVG Filter Effects 1 §15.5. Default
			// Subregion to the filter region so FEFlood.MapRect and
			// paintLayerWithFilter's filter.MapRect(sourceExtent) call
			// see the full flood area — otherwise a flood applied to a
			// CSS-filtered HTML element clips to the smaller source
			// extent and misses pixels the filter region extends into.
			// Mirrors Blink FEFlood which fills filter_region when no
			// primitive subregion is set.
			sub = elt.FilterRegion()
		}
		fe.Subregion = sub
		return fe
	case "feimage":
		// Per Filter Effects 1 §15.18: feImage is a leaf primitive
		// whose output is an image, sourced from `href` (or
		// `xlink:href`). The subregion attributes (x/y/width/height) on
		// feImage define the rect into which the image is rendered
		// inside the filter region; the parent filter's region is the
		// containing canvas.
		//
		// resolvePrimitiveSubregion returns the empty rect when no
		// subregion attributes were supplied — in that case the spec
		// defaults to the full filter region, which FEImage.ApplyEffect
		// already does internally via its TargetRect.Empty() fallback.
		// Mirrors Blink SVGFEImageElement::Build's pass of the resolved
		// subregion through to the FEImage constructor.
		sub, set := resolvePrimitiveSubregion(elt, prim)
		src, target, parNone := prim.ResolveImageSource(elt.FilterRegion(), sub)
		fe := NewFEImage(b.space, src, target, parNone)
		if set && sub.Empty() {
			// A subregion that resolves to nothing draws nothing.
			return NewSubregionClippedEffect(fe, sub)
		}
		return fe
	case "fegaussianblur":
		in := b.resolveInputAttr(prim, "in")
		// FEGaussianBlur scales its stdDeviation (fe_gaussian_blur.cc:74-83
		// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		stdX, stdY := NumberOptionalNumberAttr(prim, "stdDeviation", 0)
		stdX, stdY = layerSize(elt, stdX, stdY)
		return NewFEGaussianBlur(in, b.space, stdX, stdY)
	case "fecolormatrix":
		in := b.resolveInputAttr(prim, "in")
		t, values := parseColorMatrixAttrs(prim)
		return NewFEColorMatrix(in, b.space, t, values)
	case "feoffset":
		in := b.resolveInputAttr(prim, "in")
		dx, _ := parseFloatAttr(prim, "dx", 0)
		dy, _ := parseFloatAttr(prim, "dy", 0)
		// FEOffset scales its offset (fe_offset.cc:55-65 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		dx, dy = layerVector(elt, dx, dy)
		return NewFEOffset(in, b.space, dx, dy)
	case "feblend":
		in := b.resolveInputAttr(prim, "in")
		in2 := b.resolveInputAttr(prim, "in2")
		mode := parseBlendMode(prim)
		return NewFEBlend(in, in2, b.space, mode)
	case "fecomposite":
		// Per SVG Filter Effects 1 §feComposite. `in`/`in2` resolve
		// via the standard chain; operator defaults to "over"; k1-k4
		// default to 0 (the spec value) and are only consulted for
		// `operator="arithmetic"`. Mirrors Blink
		// svg_fe_composite_element.cc and FEComposite ctor.
		in1 := b.resolveInputAttr(prim, "in")
		in2 := b.resolveInputAttr(prim, "in2")
		op := parseCompositeOperator(prim)
		k1, _ := parseFloatAttr(prim, "k1", 0)
		k2, _ := parseFloatAttr(prim, "k2", 0)
		k3, _ := parseFloatAttr(prim, "k3", 0)
		k4, _ := parseFloatAttr(prim, "k4", 0)
		return NewFEComposite(in1, in2, b.space, op, k1, k2, k3, k4)
	case "fetile":
		// Per SVG Filter Effects 1 §feTile. Single input via `in`;
		// tile source rect is derived from the input's primitive
		// subregion at evaluation time. Mirrors
		// svg_fe_tile_element.cc and fe_tile.cc.
		in := b.resolveInputAttr(prim, "in")
		return NewFETile(in, b.space)
	case "femerge":
		// `<feMerge>` has `<feMergeNode>` children, each carrying an
		// `in` attribute. Walk them in DOM order.
		var ins []FilterEffect
		for _, child := range prim.Children() {
			if child.TagName() != "femergenode" {
				continue
			}
			ins = append(ins, b.resolveInputAttr(child, "in"))
		}
		// Spec (SVG Filter Effects 1 §feMerge): an feMerge with no
		// feMergeNode children produces a transparent black image — NOT a
		// pass-through of the chain default. NewFEMerge over an empty input
		// list does exactly that (its ApplyEffect composites nothing onto a
		// zeroed buffer). Mirrors Blink SVGFEMergeElement::Build, which
		// constructs an FEMerge with zero inputs.
		return NewFEMerge(ins, b.space)
	case "fecomponenttransfer":
		in := b.resolveInputAttr(prim, "in")
		funcs := parseComponentTransferFuncs(prim)
		return NewFEComponentTransfer(in, b.space, funcs)
	case "fedropshadow":
		in := b.resolveInputAttr(prim, "in")
		// dx, dy and stdDeviation all default to 2 (SVGFEDropShadowElement,
		// svg_fe_drop_shadow_element.cc:40-45), and FEDropShadow scales them
		// (fe_drop_shadow.cc:59-72 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		dx, _ := parseFloatAttr(prim, "dx", 2)
		dy, _ := parseFloatAttr(prim, "dy", 2)
		dx, dy = layerVector(elt, dx, dy)
		stdX, stdY := NumberOptionalNumberAttr(prim, "stdDeviation", 2)
		stdX, stdY = layerSize(elt, stdX, stdY)
		r, g, bb, a := prim.FloodColor()
		return NewFEDropShadow(in, b.sourceAlpha, b.space, dx, dy, stdX, stdY, r, g, bb, a)
	case "fedisplacementmap":
		// Per SVG Filter Effects 1 §feDisplacementMap. `in` defaults to
		// the chain default (handled by resolveInputAttr). `in2` has no
		// implicit fallback in the spec; in this implementation it
		// also falls through to `lastEffect` when missing, but the FE
		// itself defends against a nil in2 by reusing in1.
		in1 := b.resolveInputAttr(prim, "in")
		in2 := b.resolveInputAttr(prim, "in2")
		xChan := parseChannelSelector(prim, "xChannelSelector")
		yChan := parseChannelSelector(prim, "yChannelSelector")
		scale, _ := parseFloatAttr(prim, "scale", 0)
		// FEDisplacementMap's kernel scales its scale horizontally, for both
		// axes (fe_displacement_map.cc:126); MapEffect pads each axis by
		// its own scaled scale (fe_displacement_map.cc:43-48 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		scaleX, scaleY := layerSize(elt, scale, scale)
		return NewFEDisplacementMap(in1, in2, b.space, xChan, yChan, scaleX, scaleY)
	case "fediffuselighting":
		// Per SVG Filter Effects 1 §feDiffuseLighting. The primitive
		// reads its alpha-bump input (default chain), computes the
		// Sobel surface normal per pixel, and dots it with the
		// LightSource direction to produce the Lambert output. The
		// light source is the first matching child element.
		in := b.resolveInputAttr(prim, "in")
		r, g, bb, a := prim.LightingColor()
		surfaceScale := elt.Mapping().ParamToLayerZ(prim.SurfaceScale())
		diffuseConstant := prim.DiffuseConstant()
		kULx, kULy := prim.KernelUnitLength()
		return NewFEDiffuseLighting(in, b.space, r, g, bb, a,
			surfaceScale, diffuseConstant, kULx, kULy, resolveLightSource(elt, prim.LightSource()))
	case "fespecularlighting":
		// Per SVG Filter Effects 1 §feSpecularLighting. Same setup as
		// diffuse but with the Phong specular BRDF (halfway vector +
		// exponent). Output alpha = max(R, G, B) per spec.
		in := b.resolveInputAttr(prim, "in")
		r, g, bb, a := prim.LightingColor()
		surfaceScale := elt.Mapping().ParamToLayerZ(prim.SurfaceScale())
		specularConstant := prim.SpecularConstant()
		specularExponent := prim.SpecularExponent()
		kULx, kULy := prim.KernelUnitLength()
		return NewFESpecularLighting(in, b.space, r, g, bb, a,
			surfaceScale, specularConstant, specularExponent, kULx, kULy, resolveLightSource(elt, prim.LightSource()))
	case "feconvolvematrix":
		// Per SVG Filter Effects 1 §feConvolveMatrix. Applies a 2D kernel
		// convolution to the input image. The kernel is supplied as a
		// space-separated list of numbers in kernelMatrix attribute, with
		// order × order elements. Mirrors Blink
		// svg_fe_convolve_matrix_element.cc and FEConvolveMatrix.
		in := b.resolveInputAttr(prim, "in")
		order, _ := parseIntAttr(prim, "order", 3)
		kernelMatrix := parseFloatList(prim, "kernelMatrix")
		divisor, _ := parseFloatAttr(prim, "divisor", 0) // 0 means compute from kernel sum
		bias, _ := parseFloatAttr(prim, "bias", 0)
		targetX, _ := parseIntAttr(prim, "targetX", order/2)
		targetY, _ := parseIntAttr(prim, "targetY", order/2)
		edgeMode := parseStringAttr(prim, "edgeMode", "duplicate")
		kULx, kULy := prim.KernelUnitLength()
		preserveAlpha := false
		if s, ok := prim.Attribute("preserveAlpha"); ok {
			preserveAlpha = strings.TrimSpace(s) == "true"
		}
		return NewFEConvolveMatrix(in, b.space, order, kernelMatrix, divisor, bias,
			targetX, targetY, edgeMode, kULx, kULy, preserveAlpha)
	case "femorphology":
		// Per SVG Filter Effects 1 §feMorphology. Erodes (min) or dilates
		// (max) the input over a (2*rx+1)x(2*ry+1) window. `radius` is one
		// or two numbers (rx [ry]); operator defaults to "erode". Mirrors
		// Blink svg_fe_morphology_element.cc and FEMorphology.
		in := b.resolveInputAttr(prim, "in")
		op := MorphologyErode
		if parseStringAttr(prim, "operator", "erode") == "dilate" {
			op = MorphologyDilate
		}
		// `radius` is one or two numbers (rx [ry]); default 0 (disabled).
		// A single value applies to both axes. Mirrors
		// SVGFEMorphologyElement::radiusX/radiusY (default 0, 0).
		rx, ry := NumberOptionalNumberAttr(prim, "radius", 0)
		// FEMorphology scales its radii (fe_morphology.cc:86-97 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		rx, ry = layerSize(elt, rx, ry)
		return NewFEMorphology(in, b.space, op, rx, ry)
	case "feturbulence":
		// Per SVG Filter Effects 1 §feTurbulence. Generates a Perlin-noise
		// image. `in` is ignored (this is a generator). Mirrors Blink
		// svg_fe_turbulence_element.cc and FETurbulence.
		// The noise is in the filter's local space, whose origin is
		// UserSpaceOrigin in parameter space.
		t := newFETurbulenceFromPrimitive(b.space, prim)
		t.Mapping = localMapping(elt)
		return t
	}
	// Unsupported primitive: pass through. Blink's SVGFilterBuilder
	// returns nullptr for unknown / unsupported primitives, which
	// causes the chain to stop there. We instead keep the chain
	// flowing so partially-supported filters still render something
	// reasonable.
	return nil
}

// applyScale scales a primitive's horizontal length x and vertical length y
// by the reference box under primitiveUnits="objectBoundingBox", as
// Filter::ApplyHorizontalScale / ApplyVerticalScale do (filter.cc:65-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; their zoom factor is C1's).
func applyScale(elt SVGFilterElement, x, y float64) (float64, float64) {
	if !elt.PrimitiveUnitsObjectBoundingBox() {
		return x, y
	}
	ref := elt.ReferenceBox()
	return x * ref.Width(), y * ref.Height()
}

// layerSize resolves a primitive's horizontal and vertical lengths (a blur's
// deviations, a morphology radius, a displacement scale: applyScale) and maps
// them into the layer as Skia maps an SkSize (Mapping.ParamToLayerSize). The
// sign survives the mapping, so an invalid negative length stays invalid and
// a negative displacement scale keeps its direction.
func layerSize(elt SVGFilterElement, x, y float64) (float64, float64) {
	x, y = applyScale(elt, x, y)
	lx, ly := elt.Mapping().ParamToLayerSize(x, y)
	return math.Copysign(lx, x), math.Copysign(ly, y)
}

// layerVector resolves a primitive's offset (applyScale) and maps it into the
// layer as Skia maps a Vector (Mapping.ParamToLayerVector).
func layerVector(elt SVGFilterElement, x, y float64) (float64, float64) {
	return elt.Mapping().ParamToLayerVector(applyScale(elt, x, y))
}

// resolve3dPoint maps a light position under primitiveUnits=
// "objectBoundingBox": x and y are fractions of the reference box, and z of
// its diagonal over sqrt(2) (Filter::Resolve3dPoint, filter.cc:77-87 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func resolve3dPoint(elt SVGFilterElement, x, y, z float64) (float64, float64, float64) {
	if !elt.PrimitiveUnitsObjectBoundingBox() {
		return x, y, z
	}
	ref := elt.ReferenceBox()
	w, h := ref.Width(), ref.Height()
	return x*w + ref.X(), y*h + ref.Y(), z * math.Sqrt((w*w+h*h)/2)
}

// layerPoint3 resolves a light position (resolve3dPoint) and maps it into
// the layer: x and y as a point, z as a depth (SkLightingImageFilter::
// onFilterImage, src/effects/imagefilters/SkLightingImageFilter.cpp:527-532
// @ skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e). A spot light's pointsAt
// is mapped as a point, which under the affine layer matrix gives Skia's
// mapped direction vector.
func layerPoint3(elt SVGFilterElement, x, y, z float64) (float64, float64, float64) {
	// objectBoundingBox positions are fractions of the reference box, which
	// is in parameter space; userSpaceOnUse ones are local.
	m := localMapping(elt)
	if elt.PrimitiveUnitsObjectBoundingBox() {
		m = elt.Mapping()
	}
	x, y, z = resolve3dPoint(elt, x, y, z)
	lx, ly := m.ParamToLayerPoint(x, y)
	return lx, ly, m.ParamToLayerZ(z)
}

// localMapping is elt's Mapping from the filter's local space instead of
// parameter space: userSpaceOnUse light positions and the turbulence noise
// are local to the element (Filter::Resolve3dPoint leaves them as they are,
// filter.cc:77-88 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the
// local origin is UserSpaceOrigin in parameter space: an SVG element's user
// space origin, or a box's border-box origin (its filter reference box is in
// the layer's local space, paint_property_tree_builder.cc:2414-2428 @
// a9f50e52).
func localMapping(elt SVGFilterElement) Mapping {
	m := elt.Mapping()
	o := elt.UserSpaceOrigin()
	m.ParamToLayer.Translate(o.X, o.Y)
	return m
}

// resolveLightSource returns ls with its positions resolved
// (resolve3dPoint), as SVGFEPointLightElement and SVGFESpotLightElement
// ::GetLightSource build their sources (svg_fe_point_light_element.cc:33,
// svg_fe_spot_light_element.cc:33-34 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A distant light has no
// position.
func resolveLightSource(elt SVGFilterElement, ls LightSource) LightSource {
	switch l := ls.(type) {
	case *PointLightSource:
		return NewPointLightSource(layerPoint3(elt, l.X, l.Y, l.Z))
	case *SpotLightSource:
		x, y, z := layerPoint3(elt, l.X, l.Y, l.Z)
		px, py, pz := layerPoint3(elt, l.PointsAtX, l.PointsAtY, l.PointsAtZ)
		return NewSpotLightSource(x, y, z, px, py, pz, l.SpecularExponent, l.LimitingConeAngle)
	case *DistantLightSource:
		return layerDistantLight(elt.Mapping(), l)
	}
	return ls
}

// layerDistantLight maps a distant light's direction into the layer: its x
// and y as a vector, its z as a depth (SkLightingImageFilter::onFilterImage,
// src/effects/imagefilters/SkLightingImageFilter.cpp:531-532 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e), kept as an azimuth and
// elevation. An identity mapping returns l as is, so its angles are not
// round-tripped through atan2.
func layerDistantLight(m Mapping, l *DistantLightSource) *DistantLightSource {
	if m.ParamToLayer.IsIdentity() {
		return l
	}
	x, y, z, _ := l.Direction(0, 0, 0)
	x, y = m.ParamToLayerVector(x, y)
	z = m.ParamToLayerZ(z)
	const deg = 180 / math.Pi
	return NewDistantLightSource(math.Atan2(y, x)*deg, math.Atan2(z, math.Hypot(x, y))*deg)
}

// resolveInputAttr reads the named attribute (`in` or `in2`) and
// resolves it through GetEffectByID.
func (b *SVGFilterBuilder) resolveInputAttr(prim SVGFilterPrimitive, name string) FilterEffect {
	if v, ok := prim.Attribute(name); ok {
		return b.GetEffectByID(v)
	}
	return b.lastEffect
}

// resolvePrimitiveSubregion computes a filter primitive's subregion in layer
// pixels from its x/y/width/height attributes. set is false when none is
// set ("use the full filter region"); a set subregion may be empty, when it
// maps to nothing (behind the eye), and then clips the primitive away, as
// Skia's crop to an empty rect does.
//
// The subregion resolves in parameter space, as Blink's
// SVGFilterPrimitiveStandardAttributes resolves it in the element's local
// space, and is then mapped into the layer (Mapping.ParamToLayerPixels) and
// rounded out to whole pixels. Per SVG Filter Effects 1 §15.7:
//   - primitiveUnits="objectBoundingBox": x/y/width/height are fractions
//     of the reference box (missing x/y are 0%, missing width/height 100%).
//   - primitiveUnits="userSpaceOnUse": x/y are user-space coordinates
//     relative to UserSpaceOrigin — not the filter region's origin, which
//     can carry the default 10% expansion — and percentages resolve
//     against SubregionViewport (SVG Filter Effects 1 §7.11, Blink's
//     SVGViewportResolver), not the filter region. A missing attribute
//     takes the filter region's (ParamFilterRegion).
func resolvePrimitiveSubregion(elt SVGFilterElement, prim SVGFilterPrimitive) (sub image.Rectangle, set bool) {
	xStr, hasX := prim.Attribute("x")
	yStr, hasY := prim.Attribute("y")
	wStr, hasW := prim.Attribute("width")
	hStr, hasH := prim.Attribute("height")
	if !hasX && !hasY && !hasW && !hasH {
		return image.Rectangle{}, false
	}
	var x, y, w, h float64
	if elt.PrimitiveUnitsObjectBoundingBox() {
		ref := elt.ReferenceBox()
		x = ref.X() + parseFracPercent(xStr, 0.0)*ref.Width()
		y = ref.Y() + parseFracPercent(yStr, 0.0)*ref.Height()
		w, h = ref.Width(), ref.Height()
		if hasW {
			w = parseFracPercent(wStr, 1.0) * ref.Width()
		}
		if hasH {
			h = parseFracPercent(hStr, 1.0) * ref.Height()
		}
	} else {
		region := elt.ParamFilterRegion()
		origin := elt.UserSpaceOrigin()
		viewport := elt.SubregionViewport()
		if viewport.IsEmpty() {
			// A degenerate viewport falls back to the filter region.
			viewport = region
		}
		resolvePctLen := func(s string, base float64) float64 {
			v, isPct := parseLength(s, 0)
			if isPct {
				return v * base
			}
			return v
		}
		x, y, w, h = region.X(), region.Y(), region.Width(), region.Height()
		if hasX {
			x = origin.X + resolvePctLen(xStr, viewport.Width())
		}
		if hasY {
			y = origin.Y + resolvePctLen(yStr, viewport.Height())
		}
		if hasW {
			w = resolvePctLen(wStr, viewport.Width())
		}
		if hasH {
			h = resolvePctLen(hStr, viewport.Height())
		}
	}
	sub, _ = elt.Mapping().ParamToLayerPixels(geometry.NewRectF(x, y, w, h))
	return sub, true
}

// NumberOptionalNumberAttr reads a number-optional-number attribute
// (stdDeviation, radius, baseFrequency, kernelUnitLength): one number for
// both axes, or two. def is the attribute's initial value for both axes,
// taken when it is missing or does not parse: a parse error resets the
// value to its initial one (SVGNumberOptionalNumber::SetValueAsString,
// svg_number_optional_number.cc:64-75, and SVGAnimatedPropertyBase,
// svg_animated_property.h:177-179 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// The HTML tokenizer may have lowercased the name.
func NumberOptionalNumberAttr(prim interface{ Attribute(string) (string, bool) }, name string, def float64) (float64, float64) {
	v, ok := prim.Attribute(name)
	if !ok {
		v, ok = prim.Attribute(strings.ToLower(name))
	}
	if !ok {
		return def, def
	}
	x, y, ok := svg.ParseNumberOptionalNumber(v)
	if !ok {
		return def, def
	}
	return x, y
}

// parseFloatAttr reads a single float attribute with a default
// fallback. Returns (value, true) on parse success; (def, false) on
// missing/malformed.
func parseFloatAttr(prim SVGFilterPrimitive, name string, def float64) (float64, bool) {
	v, ok := prim.Attribute(name)
	if !ok {
		return def, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def, false
	}
	return f, true
}

// parseIntAttr reads an integer attribute. Returns the parsed value and
// whether it was present; if missing or unparseable, returns the default.
func parseIntAttr(prim SVGFilterPrimitive, name string, def int) (int, bool) {
	v, ok := prim.Attribute(name)
	if !ok {
		return def, false
	}
	i, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def, false
	}
	return i, true
}

// parseStringAttr reads a string attribute. Returns the value and whether
// it was present; if missing, returns the default.
func parseStringAttr(prim SVGFilterPrimitive, name string, def string) string {
	v, ok := prim.Attribute(name)
	if !ok {
		return def
	}
	return strings.TrimSpace(v)
}

// parseChannelSelector reads an feDisplacementMap `xChannelSelector` or
// `yChannelSelector` attribute. Per SVG Filter Effects 1
// §feDisplacementMap, the default is `A` when the attribute is missing
// or unrecognized. Mirrors Blink
// SVGFEDisplacementMapElement::ParseChannelSelector.
//
// HTML's tokenizer lowercases attribute names, so we also try the
// lowercase form — same convention as `parseStdDeviation`.
func parseChannelSelector(prim SVGFilterPrimitive, name string) ChannelSelector {
	v, ok := prim.Attribute(name)
	if !ok {
		v, ok = prim.Attribute(strings.ToLower(name))
	}
	if !ok {
		return ChannelA
	}
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "R":
		return ChannelR
	case "G":
		return ChannelG
	case "B":
		return ChannelB
	case "A":
		return ChannelA
	}
	// Unrecognized value → SVG default A.
	return ChannelA
}

// parseCompositeOperator reads the feComposite `operator` attribute.
// Default per SVG Filter Effects 1 §feComposite: "over". Unknown values
// fall back to the default — mirrors Blink's SVGEnumerationMap behaviour
// where the parser rejects unknown enums and the property retains its
// initial value.
//
// "lighter" is the SVG-1.1 / Filter-Effects-1 enum slot used by Skia's
// kPlus blend mode. The spec lists it but it has no native primitive
// behaviour in louis14's software path; we fall back to "over" for it
// pending a real implementation. None of the bucket-J tests exercise it.
func parseCompositeOperator(prim SVGFilterPrimitive) CompositeOperator {
	v, ok := prim.Attribute("operator")
	if !ok {
		return CompositeOver
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "over":
		return CompositeOver
	case "in":
		return CompositeIn
	case "out":
		return CompositeOut
	case "atop":
		return CompositeAtop
	case "xor":
		return CompositeXor
	case "arithmetic":
		return CompositeArithmetic
	}
	return CompositeOver
}

// parseBlendMode reads an feBlend `mode` attribute.
func parseBlendMode(prim SVGFilterPrimitive) BlendMode {
	v, ok := prim.Attribute("mode")
	if !ok {
		return BlendModeNormal
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "multiply":
		return BlendModeMultiply
	case "screen":
		return BlendModeScreen
	case "darken":
		return BlendModeDarken
	case "lighten":
		return BlendModeLighten
	}
	return BlendModeNormal
}

// parseColorMatrixAttrs reads an feColorMatrix `type` + `values`.
// Default type: matrix. Default values: identity for matrix, 1 for
// saturate, 0 for hueRotate.
func parseColorMatrixAttrs(prim SVGFilterPrimitive) (ColorMatrixType, []float64) {
	tStr := "matrix"
	if v, ok := prim.Attribute("type"); ok {
		tStr = strings.ToLower(strings.TrimSpace(v))
	}
	vStr, _ := prim.Attribute("values")
	parts := splitSVGList(vStr)
	values := make([]float64, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(p, 64)
		if err == nil {
			values = append(values, f)
		}
	}
	switch tStr {
	case "saturate":
		if len(values) == 0 {
			values = []float64{1}
		}
		return ColorMatrixTypeSaturate, values
	case "huerotate":
		if len(values) == 0 {
			values = []float64{0}
		}
		return ColorMatrixTypeHueRotate, values
	case "luminancetoalpha":
		return ColorMatrixTypeLuminanceToAlpha, nil
	}
	// matrix (default): identity if no values supplied.
	if len(values) < 20 {
		identity := []float64{
			1, 0, 0, 0, 0,
			0, 1, 0, 0, 0,
			0, 0, 1, 0, 0,
			0, 0, 0, 1, 0,
		}
		values = identity
	}
	return ColorMatrixTypeMatrix, values
}

// parseComponentTransferFuncs reads the `<feFuncR>` / `<feFuncG>` /
// `<feFuncB>` / `<feFuncA>` children of an `<feComponentTransfer>`
// primitive and builds the 4 transfer functions. Missing children
// default to TransferIdentity.
func parseComponentTransferFuncs(prim SVGFilterPrimitive) [4]TransferFunc {
	out := [4]TransferFunc{
		{Type: TransferIdentity},
		{Type: TransferIdentity},
		{Type: TransferIdentity},
		{Type: TransferIdentity},
	}
	for _, c := range prim.Children() {
		var idx int = -1
		switch c.TagName() {
		case "fefuncr":
			idx = 0
		case "fefuncg":
			idx = 1
		case "fefuncb":
			idx = 2
		case "fefunca":
			idx = 3
		default:
			continue
		}
		out[idx] = parseTransferFunc(c)
	}
	return out
}

// parseTransferFunc reads the attributes of one feFuncR/G/B/A child.
func parseTransferFunc(prim SVGFilterPrimitive) TransferFunc {
	tStr := "identity"
	if v, ok := prim.Attribute("type"); ok {
		tStr = strings.ToLower(strings.TrimSpace(v))
	}
	tf := TransferFunc{}
	switch tStr {
	case "table":
		tf.Type = TransferTable
		tf.TableValues = parseFloatList(prim, "tableValues")
	case "discrete":
		tf.Type = TransferDiscrete
		tf.TableValues = parseFloatList(prim, "tableValues")
	case "linear":
		tf.Type = TransferLinear
		tf.Slope, _ = parseFloatAttr(prim, "slope", 1)
		tf.Intercept, _ = parseFloatAttr(prim, "intercept", 0)
	case "gamma":
		tf.Type = TransferGamma
		tf.Amplitude, _ = parseFloatAttr(prim, "amplitude", 1)
		tf.Exponent, _ = parseFloatAttr(prim, "exponent", 1)
		tf.Offset, _ = parseFloatAttr(prim, "offset", 0)
	default:
		tf.Type = TransferIdentity
	}
	return tf
}

// splitSVGList splits a whitespace/comma-separated SVG attribute value.
func splitSVGList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
}

// parseFloatList reads a whitespace/comma-separated list of floats.
func parseFloatList(prim SVGFilterPrimitive, name string) []float64 {
	v, ok := prim.Attribute(name)
	if !ok {
		// Try lowercase variant (HTML tokenizer convention).
		v, ok = prim.Attribute(strings.ToLower(name))
	}
	if !ok {
		return nil
	}
	parts := splitSVGList(v)
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(p, 64)
		if err == nil {
			out = append(out, f)
		}
	}
	return out
}

// parseFracPercent parses "25%" or "0.25" into 0.25. Used for
// objectBoundingBox primitive subregion coords.
func parseFracPercent(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if strings.HasSuffix(s, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil {
			return def
		}
		return f / 100.0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

// parseLength parses a length value with optional percentage. Returns
// the parsed value and whether it had a percent suffix.
func parseLength(s string, def float64) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, false
	}
	pct := false
	num := s
	if strings.HasSuffix(s, "%") {
		pct = true
		num = strings.TrimSuffix(s, "%")
	}
	// Strip common unit suffixes (px, em, …) defensively.
	num = strings.TrimSuffix(num, "px")
	f, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
	if err != nil {
		return def, pct
	}
	if pct {
		return f / 100.0, true
	}
	return f, false
}

// ResolveInterpolationSpace returns the InterpolationSpace a
// `color-interpolation-filters` value selects: linearRGB for linearRGB,
// sRGB for sRGB and auto. Mirrors Blink's
// SVGFilterBuilder::ResolveInterpolationSpace (svg_filter_builder.cc:157-162
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which maps everything but
// kLinearrgb to sRGB. ok is false for any other value, which Blink's parser
// drops: the caller falls back to what applies without it.
func ResolveInterpolationSpace(value string) (space InterpolationSpace, ok bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "linearrgb":
		return InterpolationSpaceLinearRGB, true
	case "srgb", "auto":
		return InterpolationSpaceSRGB, true
	}
	return InterpolationSpaceLinearRGB, false
}
