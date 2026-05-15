package svg

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// SVGResourceFilter is the layout node for a `<filter>` resource
// element. Mirrors Blink's LayoutSVGResourceFilter
// (core/layout/svg/layout_svg_resource_filter.{h,cc}).
//
// Per SVG Filter Effects 1 a `<filter>` element declares a region (its
// filter region: x, y, width, height) and a sequence of filter
// primitive children (`<feFlood>`, `<feGaussianBlur>`, …) whose
// outputs feed each other to produce the filtered rendering of any
// element that references the filter via `filter: url(#id)`.
//
// Two coordinate spaces:
//
//   - FilterUnits (default objectBoundingBox per SVG Filter Effects 1
//     §6.5) decides how x/y/width/height resolve: fractions of the
//     referencing element's bbox (objectBoundingBox) or absolute
//     user-space coords (userSpaceOnUse).
//   - PrimitiveUnits (default userSpaceOnUse — REVERSE of FilterUnits,
//     same reversal as MaskUnits/MaskContentUnits) decides how each
//     filter primitive's own x/y/width/height (primitive subregion)
//     resolve.
//
// Defaults for x/y/width/height: -10%, -10%, 120%, 120% (SVG Filter
// Effects 1 §6.5).
//
// FePrimitives holds the filter primitive child elements as
// ElementAdapters so the render-side SVGFilterBuilder can re-parse
// attributes when wiring the FilterEffect graph against a target's
// reference box. This mirrors Blink's approach: the LayoutSVGResourceFilter
// holds the parsed `<filter>` element, and SVGFilterBuilder::BuildGraph
// walks the child SVGFilterPrimitiveStandardAttributes elements at
// build time.
type SVGResourceFilter struct {
	// ResourceID is the filter's `id` attribute.
	ResourceID string

	// FilterUnits is the coordinate-space mode for x/y/width/height.
	// Default objectBoundingBox per SVG Filter Effects 1 §6.5.
	FilterUnits SVGUnitType

	// PrimitiveUnits is the coordinate-space mode for primitive
	// subregion attributes on each filter primitive child. Default
	// userSpaceOnUse per SVG Filter Effects 1 §6.5.
	PrimitiveUnits SVGUnitType

	// X, Y, Width, Height define the filter region bounding box. Stored
	// unresolved because their resolution depends on FilterUnits and the
	// referencing element's bbox.
	X, Y, Width, Height SVGGradientLength

	// FePrimitives are the filter primitive child elements
	// (`<feFlood>`, `<feGaussianBlur>`, …) in DOM order. Kept as
	// ElementAdapters so the SVGFilterBuilder can read primitive
	// attributes at build time. Mirrors Blink's storing of child
	// SVGFilterPrimitiveStandardAttributes elements on the parent
	// SVGFilterElement.
	FePrimitives []ElementAdapter

	// elementStyle is the filter element's resolved style. Carries
	// `color-interpolation-filters` (linearRGB by default per SVG 1.1
	// §4.2).
	elementStyle *css.Style

	// cycleState carries the cycle-detection state machine; flipped
	// by the SVGResourcesCycleSolver DFS once per paint frame.
	cycleState SVGCycleState
}

// ID implements the SVG resource interface (registry lookup key).
func (f *SVGResourceFilter) ID() string { return f.ResourceID }

// Kind implements SVGResource.
func (f *SVGResourceFilter) Kind() SVGResourceKind { return SVGResourceKindFilter }

// CycleState implements SVGResource.
func (f *SVGResourceFilter) CycleState() SVGCycleState { return f.cycleState }

// SetCycleState implements SVGResource.
func (f *SVGResourceFilter) SetCycleState(s SVGCycleState) { f.cycleState = s }

// ObjectBoundingBox returns an empty bbox — filter resource elements
// don't contribute to the SVG tree's paint bounds. Mirrors Blink's
// LayoutSVGHiddenContainer::ObjectBoundingBox.
func (f *SVGResourceFilter) ObjectBoundingBox() geometry.RectF { return geometry.RectF{} }

// LocalTransform returns identity — filter elements are never in the
// paint tree, so their LocalTransform doesn't compose into ancestor
// paint transforms.
func (f *SVGResourceFilter) LocalTransform() geometry.AffineTransform { return geometry.Identity() }

// UpdateSVGLayout is a no-op for filter resource elements.
func (f *SVGResourceFilter) UpdateSVGLayout(SVGLayoutInfo) SVGLayoutResult {
	return SVGLayoutResult{}
}

// Paint is a no-op — render-side svg_filter_painter walks the
// primitives directly when a `filter: url(#id)` reference resolves
// to it. Mirrors the SVGNode convention established in Phase 3: the
// painter is dispatched at the consumer site, not by the resource
// element itself.
func (f *SVGResourceFilter) Paint(*SVGPaintContext) {}

// Style returns the filter element's resolved style (or nil). Used by
// the render-side builder to read `color-interpolation-filters`.
func (f *SVGResourceFilter) Style() *css.Style { return f.elementStyle }

// ResourceBoundingBox returns the filter region's user-space rect for
// the given referencing target. Mirrors Blink's
// LayoutSVGResourceFilter::ResourceBoundingBox.
//
// Resolution rule (SVG Filter Effects 1 §6.5):
//   - In objectBoundingBox mode: x/y/width/height are fractions of
//     `target`; default -10%/-10%/120%/120% if attributes are absent.
//     The returned rect is in `target`'s user-space.
//   - In userSpaceOnUse mode: attributes are user-space lengths; if
//     missing they fall back to the same -10%/-10%/120%/120% defaults
//     projected onto `target` (Blink's compatibility convention).
//
// `lengthCtx` is the length context active in the filter's declaration
// scope — used for percent resolution in userSpaceOnUse mode.
func (f *SVGResourceFilter) ResourceBoundingBox(target geometry.RectF, lengthCtx SVGLengthContext) geometry.RectF {
	// Defaults per SVG Filter Effects 1 §6.5.
	defX := -0.10
	defY := -0.10
	defW := 1.20
	defH := 1.20

	switch f.FilterUnits {
	case SVGUnitObjectBoundingBox:
		x := f.X.ResolveAgainst(SVGUnitObjectBoundingBox, LengthModeWidth, lengthCtx, defX)
		y := f.Y.ResolveAgainst(SVGUnitObjectBoundingBox, LengthModeHeight, lengthCtx, defY)
		w := f.Width.ResolveAgainst(SVGUnitObjectBoundingBox, LengthModeWidth, lengthCtx, defW)
		h := f.Height.ResolveAgainst(SVGUnitObjectBoundingBox, LengthModeHeight, lengthCtx, defH)
		return geometry.NewRectF(
			target.X()+x*target.Width(),
			target.Y()+y*target.Height(),
			w*target.Width(),
			h*target.Height(),
		)
	case SVGUnitUserSpaceOnUse:
		x := target.X() + defX*target.Width()
		y := target.Y() + defY*target.Height()
		w := defW * target.Width()
		h := defH * target.Height()
		if f.X.HasValue {
			x = f.X.ResolveAgainst(SVGUnitUserSpaceOnUse, LengthModeWidth, lengthCtx, x)
		}
		if f.Y.HasValue {
			y = f.Y.ResolveAgainst(SVGUnitUserSpaceOnUse, LengthModeHeight, lengthCtx, y)
		}
		if f.Width.HasValue {
			w = f.Width.ResolveAgainst(SVGUnitUserSpaceOnUse, LengthModeWidth, lengthCtx, w)
		}
		if f.Height.HasValue {
			h = f.Height.ResolveAgainst(SVGUnitUserSpaceOnUse, LengthModeHeight, lengthCtx, h)
		}
		return geometry.NewRectF(x, y, w, h)
	}
	return target
}

// NewSVGResourceFilter builds an SVGResourceFilter from a `<filter>`
// ElementAdapter. Parses filterUnits/primitiveUnits/x/y/width/height
// and collects the filter primitive children as ElementAdapters for
// later graph building. Mirrors Blink's
// LayoutSVGResourceFilter::UpdateSVGLayout — parse attributes,
// populate child list.
//
// The styleResolver callback (if non-nil) resolves the `<filter>`
// element's own computed style — this is where `color-interpolation-filters`
// arrives. Filter primitive children get their styles resolved by the
// render-side builder when it visits them (each one is an
// ElementAdapter wrapping its DOM node).
func NewSVGResourceFilter(elt ElementAdapter, parentCtx SVGLengthContext, styleResolver StyleResolver) *SVGResourceFilter {
	if elt == nil {
		return nil
	}
	_ = parentCtx
	f := &SVGResourceFilter{
		FilterUnits:    SVGUnitObjectBoundingBox, // SVG Filter Effects 1 §6.5 default
		PrimitiveUnits: SVGUnitUserSpaceOnUse,    // SVG Filter Effects 1 §6.5 default
	}
	if id, ok := elt.Attribute("id"); ok {
		f.ResourceID = strings.TrimSpace(id)
	}
	if v, ok := elt.Attribute("x"); ok {
		f.X = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("y"); ok {
		f.Y = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("width"); ok {
		f.Width = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("height"); ok {
		f.Height = parseGradientLength(v)
	}
	// filterUnits / primitiveUnits arrive in either spelling — HTML
	// tokenizer lowercases SVG attribute names, but the canonical SVG
	// form is camelCase and some adapters may preserve it.
	if v, ok := elt.Attribute("filterUnits"); ok {
		f.FilterUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	} else if v, ok := elt.Attribute("filterunits"); ok {
		f.FilterUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	}
	if v, ok := elt.Attribute("primitiveUnits"); ok {
		f.PrimitiveUnits = ParseGradientUnits(v, SVGUnitUserSpaceOnUse)
	} else if v, ok := elt.Attribute("primitiveunits"); ok {
		f.PrimitiveUnits = ParseGradientUnits(v, SVGUnitUserSpaceOnUse)
	}
	if styleResolver != nil {
		f.elementStyle = styleResolver(elt)
	}
	for _, child := range elt.SVGChildren() {
		// Only known filter primitive tags become children. Anything
		// else inside `<filter>` is dropped per SVG Filter Effects 1
		// §6.4 (the content model is `feBlend | feColorMatrix |
		// feComponentTransfer | feComposite | feConvolveMatrix |
		// feDiffuseLighting | feDisplacementMap | feDistantLight |
		// feDropShadow | feFlood | feFuncA | feFuncB | feFuncG |
		// feFuncR | feGaussianBlur | feImage | feMerge | feMergeNode |
		// feMorphology | feOffset | fePointLight | feSpecularLighting |
		// feSpotLight | feTile | feTurbulence`). We keep the full set
		// in the child list and let the builder decide which it can
		// realize.
		if isFePrimitiveTag(child.TagName()) {
			f.FePrimitives = append(f.FePrimitives, child)
		}
	}
	return f
}

// isFePrimitiveTag reports whether `tag` is a filter-primitive element
// recognized by SVG Filter Effects 1. Names are matched in the HTML
// tokenizer's lowercased form (`<feGaussianBlur>` → `fegaussianblur`).
func isFePrimitiveTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "feblend",
		"fecolormatrix",
		"fecomponenttransfer",
		"fecomposite",
		"feconvolvematrix",
		"fediffuselighting",
		"fedisplacementmap",
		"fedistantlight",
		"fedropshadow",
		"feflood",
		"fefunca",
		"fefuncb",
		"fefuncg",
		"fefuncr",
		"fegaussianblur",
		"feimage",
		"femerge",
		"femergenode",
		"femorphology",
		"feoffset",
		"fepointlight",
		"fespecularlighting",
		"fespotlight",
		"fetile",
		"feturbulence":
		return true
	}
	return false
}
