package render

import (
	"image"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/graphics/filters"
	"louis14/pkg/layout/svg"
)

// FilterEffectBuilder turns a CSS filter-function list into a filters.Filter
// graph. It mirrors Blink's core/paint/FilterEffectBuilder: BuildFilterEffect
// switches on each FilterOperation's type and emits the equivalent fe*
// primitive, chaining each effect onto the previous one's output.
//
// CSS shorthand filter functions all run in the sRGB interpolation space and
// do not clip to a primitive subregion (Filter Effects 1 §13, and Blink's
// filter_effect_builder.cc).
type FilterEffectBuilder struct {
	// ReferenceBox, FilterViewport and UserSpaceOrigin are in parameter
	// space, the local space the filter's parameters are in, which Mapping
	// maps into the layer the graph runs in: Blink builds the filter in the
	// element's local space (Filter::ReferenceBox, a gfx::RectF) and Skia maps
	// it (skif::Mapping, src/core/SkImageFilterTypes.h @ skia
	// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e). LOU-774.
	//
	// ReferenceBox is the filter reference box, unsnapped: the border box, or for a filter that
	// moves pixels the layer's bounding box including its self-painting
	// descendants (UpdateFilterReferenceBox). A backdrop filter's is the
	// pixel-snapped border box (BackdropFilterBounds).
	ReferenceBox geometry.RectF
	// FilterViewport is a layer's pixel-snapped
	// border box or an SVG element's <svg> viewport (unsnapped, as Blink
	// resolves it in floats): the viewport userSpaceOnUse percentages
	// resolve against (filter_effect_builder.cc:528-537 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). For a layer its size is
	// Blink's FilterViewport (ComputeFilterViewport, core/paint/
	// paint_layer.cc:164-174 @ a9f50e52), and its origin is the layer's local
	// origin, which userSpaceOnUse coordinates are relative to; an SVG shape
	// sets UserSpaceOrigin instead (SVGViewportResolver, svg_resources.cc:
	// 375-381 @ a9f50e52). It may be empty: a 0x0 box keeps its 0x0
	// viewport.
	FilterViewport geometry.RectF
	// UserSpaceOrigin, when set, is the origin userSpaceOnUse coordinates
	// are relative to in place of FilterViewport's origin: an SVG element's
	// user-space origin, whose viewport is its <svg>'s.
	UserSpaceOrigin *geometry.PointF
	// CurrentColor resolves currentColor for drop-shadow.
	CurrentColor css.Color
	// Resources, when non-nil, supplies the SVG resource registry so
	// `filter: url(#id)` references can resolve to an
	// SVGResourceFilter. Phase 7 wires this up from the renderer's
	// per-frame registry.
	Resources *svg.SVGResourceRegistry
	// ResolveStyle resolves a filter-primitive element's computed
	// style (for `flood-color`/`flood-opacity` and
	// `color-interpolation-filters`). May be nil — the builder then
	// falls back to raw attribute lookup.
	ResolveStyle func(svg.ElementAdapter) *css.Style
	// ExternalSVG resolves `filter: url(doc#id)` where doc is another
	// document (data:, file://, relative, http(s)), sharing the paint
	// pass's fetched documents and built filters (externalSVGCache). Nil
	// means external references produce no filter (Filter Effects 1 §3.1
	// null filter).
	ExternalSVG *externalSVGCache
	// ImageFetcher resolves `<feImage href="path.png">` external image
	// references. May be nil — feImage falls back to LoadImage which
	// handles data: URIs and absolute filesystem paths without a
	// fetcher. Same fetcher the renderer's `<img>`/background-image
	// path uses; see Renderer.imageFetcher.
	ImageFetcher func(uri string) ([]byte, error)
	// Mapping maps parameter space (the boxes above, blur deviations, shadow
	// offsets, an SVG <filter>'s primitives) into the layer the graph is
	// evaluated in, as Skia's filters map theirs through skif::Mapping. The
	// zero value is the identity.
	Mapping filters.Mapping
}

// viewportLengthContext is the SVGLengthContext userSpaceOnUse percentages
// resolve against: FilterViewport's size (filter_effect_builder.cc:528-537 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *FilterEffectBuilder) viewportLengthContext() svg.SVGLengthContext {
	return svg.NewSVGLengthContext(geometry.SizeF{
		Width: b.FilterViewport.Width(), Height: b.FilterViewport.Height(),
	})
}

// userSpaceOrigin is the origin userSpaceOnUse coordinates are relative
// to: UserSpaceOrigin, else the layer's local origin.
func (b *FilterEffectBuilder) userSpaceOrigin() geometry.PointF {
	if b.UserSpaceOrigin != nil {
		return *b.UserSpaceOrigin
	}
	return geometry.PointF{X: b.FilterViewport.X(), Y: b.FilterViewport.Y()}
}

// LayerReferenceBox is ReferenceBox in layer pixels
// (Mapping.ParamToLayerPixels); it is empty when the box maps behind the eye.
func (b *FilterEffectBuilder) LayerReferenceBox() image.Rectangle {
	r, _ := b.Mapping.ParamToLayerPixels(b.ReferenceBox)
	return r
}

// BuildFilterEffect builds a filters.Filter for a chained CSS filter list.
// Returns nil when the list is empty or contains only unsupported entries.
//
// A single `url(#id)` is built from its <filter> alone (buildStageReference) so the
// reference filter's own region / interpolation space is preserved
// (single-url tests rely on this). A mixed list walks the ops in order,
// each `url()` becoming a sub-graph whose SourceGraphic is rewired to
// the running chain output (Blink's FilterEffectBuilder::Build*
// FilterChain composition); each shorthand entry is built with the
// running output as its source.
func (b *FilterEffectBuilder) BuildFilterEffect(ops []css.FilterFunction) *filters.Filter {
	return b.buildStages(b.resolveStages(ops))
}

// filterStage is one function of a filter list, with a url() stage's
// <filter> resolved (lookupReferenceFilter) once per paint, for both the
// capability (filterStagesCapability) and the build (buildStages).
type filterStage struct {
	op css.FilterFunction
	// reference is a url() stage's <filter>, nil when it does not resolve;
	// external reports one from another document.
	reference *svg.SVGResourceFilter
	external  bool
}

// resolveStages resolves each url() stage of ops to its <filter>.
func (b *FilterEffectBuilder) resolveStages(ops []css.FilterFunction) []filterStage {
	stages := make([]filterStage, len(ops))
	for i, op := range ops {
		stages[i].op = op
		if op.Name == "url" {
			stages[i].reference, stages[i].external = b.lookupReferenceFilter(op.URL.Absolute)
		}
	}
	return stages
}

// buildStages is BuildFilterEffect over resolved stages.
func (b *FilterEffectBuilder) buildStages(stages []filterStage) *filters.Filter {
	if len(stages) == 0 {
		return nil
	}
	if len(stages) == 1 && stages[0].op.Name == "url" {
		return b.buildStageReference(stages[0], nil)
	}
	const space = filters.InterpolationSpaceSRGB
	src := filters.NewSourceGraphic(space)
	in := cssFilterInputs{
		prev:         src,
		srcAlpha:     filters.NewSourceAlpha(src, space),
		space:        space,
		currentColor: b.CurrentColor,
		mapping:      b.Mapping,
	}
	for _, st := range stages {
		if st.op.Name == "url" {
			sub := b.buildStageReference(st, in.prev)
			if sub != nil && sub.LastEffect != nil {
				in.prev = sub.LastEffect
			}
			// An unresolvable url() (missing fragment, cycle, fetch
			// fail) drops the entry per Filter Effects 1 §3.1 — the
			// chain continues with the same `prev`.
			continue
		}
		if fn, ok := cssFilterFunctions[st.op.Name]; ok {
			in.prev = fn.build(st.op, in)
		}
	}
	if in.prev == filters.FilterEffect(src) {
		return nil
	}

	f := &filters.Filter{
		Source:      src,
		SourceAlpha: in.srcAlpha,
		LastEffect:  in.prev,
	}
	// A shorthand-only chain has no region; the caller sizes its buffer
	// (filters.Filter.EnsureRegion). A chain with a url() stage starts from
	// the reference box mapped through the chain, with no per-stage crop
	// (LOU-704 ports the exact sizing). An SVG shape's chain then unites its
	// paint extent into it (buildSVGElementFilter); the HTML layer path keeps
	// it as is (LOU-780).
	if hasReferenceStage(stages) {
		f.FilterRegion = f.MapRect(b.LayerReferenceBox())
	}
	return f
}

// hasReferenceStage reports whether a filter list has a url() stage.
func hasReferenceStage(stages []filterStage) bool {
	for _, st := range stages {
		if st.op.Name == "url" {
			return true
		}
	}
	return false
}

// BuildReferenceFilter resolves a `filter: url(#id)` reference
// against the SVG resource registry and builds the corresponding
// FilterEffect graph via SVGFilterBuilder. Returns nil when:
//   - the id is empty,
//   - no registry is available,
//   - the id doesn't resolve to an SVGResourceFilter,
//   - the resolved filter is flagged as part of a reference cycle (per
//     the Phase 6 cycle solver) — Blink's "cycle = drop reference"
//     rule (LayoutSVGResourceContainer::FindCycle).
//
// Mirrors Blink's FilterEffectBuilder::BuildReferenceFilter +
// SVGFilterBuilder::BuildGraph dispatch.
func (b *FilterEffectBuilder) BuildReferenceFilter(id string) *filters.Filter {
	filter, external := b.lookupReferenceFilter(id)
	return b.buildStageReference(filterStage{reference: filter, external: external}, nil)
}

// buildStageReference builds a url() stage's resolved <filter> (nil when it
// did not resolve), with an optional chain-source override: when prev is
// non-nil, "SourceGraphic" references inside the filter resolve to prev (the
// running output of an outer CSS filter-value-list chain), mirroring Blink's
// FilterEffectBuilder::Build* pattern of swapping SourceGraphic per stage.
func (b *FilterEffectBuilder) buildStageReference(st filterStage, prev filters.FilterEffect) *filters.Filter {
	if st.reference == nil {
		return nil
	}
	// External documents don't share the host page's cascade: their
	// primitives read their presentation attributes.
	resolveStyle := b.ResolveStyle
	if st.external {
		resolveStyle = nil
	}
	return b.buildResourceFilter(st.reference, resolveStyle, prev)
}

// lookupReferenceFilter resolves a `filter: url(...)` value (FilterFunction.
// URL: "#frag" in this document, or "doc#frag" for a data:, file:, http(s):
// or relative document) to its <filter>: same-document through Resources,
// external through the paint pass's cache (ExternalSVG), which fetches and
// parses the document once. external reports the latter. nil when the
// fragment is empty, the lookup or fetch misses, or the filter is part of a
// reference cycle (Blink drops the reference: LayoutSVGResourceContainer::
// FindCycle); Filter Effects 1 §3.1 then drops the stage.
func (b *FilterEffectBuilder) lookupReferenceFilter(id string) (filter *svg.SVGResourceFilter, external bool) {
	docURL, fragID := splitFilterRef(id)
	if docURL != "" {
		return b.ExternalSVG.filter(docURL, fragID, b.viewportLengthContext()), true
	}
	if b.Resources == nil || fragID == "" {
		return nil, false
	}
	filter, ok := b.Resources.LookupAsFilter(fragID)
	if !ok || filter == nil || filter.CycleState() == svg.SVGCycleHasCycle {
		return nil, false
	}
	return filter, false
}

// hasUserSpaceFilterRegion reports whether a url() stage resolves to a
// <filter> whose region is userSpaceOnUse, independent of the element's box.
func hasUserSpaceFilterRegion(stages []filterStage) bool {
	for _, st := range stages {
		if st.reference != nil && st.reference.FilterUnits == svg.SVGUnitUserSpaceOnUse {
			return true
		}
	}
	return false
}

// filterStagesCapability is the most complex layer matrix the graph of stages can run
// in: the minimum over its nodes (SkImageFilter_Base::getCTMCapability,
// src/core/SkImageFilter.cpp:337-345 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e). It is read from the list, not a
// built graph, so the graph is built once, in its layer
// (Renderer.layerSpaceFilter):
//   - a CSS filter function takes its Skia filter's capability
//     (cssFilterFunctions) and does not clip to bounds
//     (filter_effect_builder.cc:355-362 @
//     a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
//   - every primitive of a url() stage clips to its subregion
//     (FilterEffect::clips_to_bounds_, filter_effect.cc:37; GetCropRect,
//     filter_effect.cc:144-163 @ a9f50e52), which Skia applies as a crop
//     filter (SkColorFilterImageFilter.cpp:92-93), kScaleTranslate: a
//     rotated flood runs unrotated and is rotated when drawn.
//
// A url() stage that does not resolve is dropped from the chain, and an
// unknown function builds nothing; builds is false when no stage builds.
func filterStagesCapability(stages []filterStage) (capability filters.MatrixCapability, builds bool) {
	capability = filters.MatrixCapabilityComplex
	for _, st := range stages {
		if st.op.Name == "url" {
			if st.reference == nil {
				continue
			}
			capability = min(capability, filters.MatrixCapabilityScaleTranslate)
		} else if fn, ok := cssFilterFunctions[st.op.Name]; ok {
			capability = min(capability, fn.capability)
		} else {
			continue
		}
		builds = true
	}
	return capability, builds
}

// filterInterpolationSpace is the operating space of an SVG <filter>'s
// graph: its color-interpolation-filters, linearRGB by default (SVG 1.1
// §4.2). A filter with no layout object takes its presentation attribute,
// else auto, which runs in sRGB (ColorInterpolationForElement and
// ResolveInterpolationSpace, svg_filter_builder.cc:136-155 and 157-162 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func filterInterpolationSpace(filter *svg.SVGResourceFilter) filters.InterpolationSpace {
	if filter.NoLayoutObject {
		if v, ok := filter.Attribute("color-interpolation-filters"); ok {
			if space, valid := filters.ResolveInterpolationSpace(v); valid {
				return space
			}
		}
		return filters.InterpolationSpaceSRGB
	}
	// An invalid declaration is dropped, leaving the initial linearRGB.
	if st := filter.Style(); st != nil {
		if v, ok := st.Get("color-interpolation-filters"); ok {
			if space, valid := filters.ResolveInterpolationSpace(v); valid {
				return space
			}
		}
	}
	return filters.InterpolationSpaceLinearRGB
}

// buildResourceFilter builds filter's graph: its region resolved from
// filterUnits and the reference box in parameter space, then mapped into the
// layer (userSpaceOnUse x/y relative to userSpaceOrigin), its primitives'
// subregion percentages against FilterViewport, not the filter region, which
// can carry the default 10% expansion. resolveStyle resolves primitive
// styles (nil for an external document, whose primitives read their
// presentation attributes). A non-nil chainSource replaces SourceGraphic
// (CSS filter-value-list chain composition).
func (b *FilterEffectBuilder) buildResourceFilter(filter *svg.SVGResourceFilter, resolveStyle func(svg.ElementAdapter) *css.Style, chainSource filters.FilterEffect) *filters.Filter {
	space := filterInterpolationSpace(filter)
	paramRegion := filter.ResourceBoundingBox(b.ReferenceBox, b.userSpaceOrigin(), b.viewportLengthContext())
	layerRegion, _ := b.Mapping.ParamToLayerPixels(paramRegion)
	adapter := &svgFilterElementAdapter{
		filter:            filter,
		filterRegion:      layerRegion,
		paramRegion:       paramRegion,
		referenceBox:      b.ReferenceBox,
		subregionViewport: b.FilterViewport,
		userSpaceOrigin:   b.userSpaceOrigin(),
		mapping:           b.Mapping,
		space:             space,
		resolveStyle:      resolveStyle,
		resources:         b.Resources,
		imageFetcher:      b.ImageFetcher,
	}
	builder := filters.NewSVGFilterBuilder(space)
	builder.SetSourceOverride(chainSource)
	return builder.BuildGraph(adapter)
}

// externalSVGFilter fetches docURL, parses it and builds its
// <filter id="fragID">: nil on any failure. External documents don't share
// the host page's computed-style cascade, so the filter and its primitives
// read their presentation attributes (a nil style resolver). The fetched
// document is never laid out, so the filter has no layout object
// (ColorInterpolationForElement's "used by external SVG files",
// svg_filter_builder.cc:136-155 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Shared by the CSS filter path and an SVG shape's filter attribute.
func externalSVGFilter(fetch func(string) ([]byte, error), docURL, fragID string, lengthCtx svg.SVGLengthContext) *svg.SVGResourceFilter {
	data, err := fetch(docURL)
	if err != nil || len(data) == 0 {
		return nil
	}
	root, err := svg.ParseExternalSVG(data)
	if err != nil {
		return nil
	}
	target, ok := svg.FindElementByID(root, fragID)
	if !ok || target.TagName() != "filter" {
		return nil
	}
	filter := svg.NewSVGResourceFilter(target, lengthCtx, nil)
	if filter != nil {
		filter.NoLayoutObject = true
	}
	return filter
}

// splitFilterRef parses a `filter: url(...)` inner value into its
// document and fragment parts. Handles:
//
//	"#frag"               → ("", "frag")
//	"foo.svg#frag"        → ("foo.svg", "frag")
//	"data:...,...#frag"   → ("data:...,...", "frag")
//	`"#frag"` / `'#frag'` → strips surrounding quotes
//	" #frag "             → trims whitespace
//	""                    → ("", "")
//
// Mirrors Blink's KURL fragment-identifier parsing for SVGResource
// URLs (svg_resource.cc:151-176 @ bf955d02 — `ParseTargetReference`).
func splitFilterRef(raw string) (docURL, fragID string) {
	v := strings.TrimSpace(raw)
	if len(v) >= 2 {
		first, last := v[0], v[len(v)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			v = v[1 : len(v)-1]
		}
	}
	return splitURLFragment(v)
}

// splitURLFragment splits a URL at its fragment: the document part (empty
// for a same-document reference) and the fragment id (empty when there is
// none, which no filter reference resolves).
func splitURLFragment(v string) (docURL, fragID string) {
	if v == "" {
		return "", ""
	}
	hashIdx := strings.IndexByte(v, '#')
	if hashIdx < 0 {
		return v, ""
	}
	return v[:hashIdx], v[hashIdx+1:]
}

// cssFilterInputs is what a CSS filter function's effect is built from: its
// input (the running chain output), the graph's SourceAlpha, the operating
// space, currentColor, and the mapping of its parameters (blur deviations,
// shadow offsets, in CSS px) into the layer.
type cssFilterInputs struct {
	prev         filters.FilterEffect
	srcAlpha     *filters.SourceAlpha
	space        filters.InterpolationSpace
	currentColor css.Color
	mapping      filters.Mapping
}

// cssFilterFunction is how a CSS filter function builds: the capability of
// the Skia filter it is (filterStagesCapability), and its fe* equivalent.
type cssFilterFunction struct {
	capability filters.MatrixCapability
	build      func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect
}

// cssFilterFunctions is every CSS filter function the builder builds (url()
// stages are built from their <filter>). A colour function is an
// SkColorFilterImageFilter, kComplex (SkColorFilterImageFilter.cpp:53 @
// skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e); blur and drop-shadow keep
// the default kScaleTranslate (SkImageFilter_Base.h:225-227).
var cssFilterFunctions = map[string]cssFilterFunction{
	// grayscale(a): blend each row of the luminance basis toward identity
	// by (1-a). Filter Effects 1 §13.
	"grayscale": {filters.MatrixCapabilityComplex, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		return filters.NewFEColorMatrix(in.prev, in.space, filters.ColorMatrixTypeMatrix, grayscaleMatrix(clampFilter01(op.Value)))
	}},
	"sepia": {filters.MatrixCapabilityComplex, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		return filters.NewFEColorMatrix(in.prev, in.space, filters.ColorMatrixTypeMatrix, sepiaMatrix(clampFilter01(op.Value)))
	}},
	// saturate accepts <number> or <percentage>, clamped at 0 minimum.
	"saturate": {filters.MatrixCapabilityComplex, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		return filters.NewFEColorMatrix(in.prev, in.space, filters.ColorMatrixTypeSaturate, []float64{max(op.Value, 0)})
	}},
	"hue-rotate": {filters.MatrixCapabilityComplex, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		return filters.NewFEColorMatrix(in.prev, in.space, filters.ColorMatrixTypeHueRotate, []float64{op.Value})
	}},
	// invert(a): table [a, 1-a] on R,G,B; alpha untouched.
	"invert": {filters.MatrixCapabilityComplex, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		a := clampFilter01(op.Value)
		tbl := filters.TransferFunc{Type: filters.TransferTable, TableValues: []float64{a, 1 - a}}
		return filters.NewFEComponentTransfer(in.prev, in.space, [4]filters.TransferFunc{tbl, tbl, tbl, identityTransfer})
	}},
	// opacity(a): table [0, a] on A only.
	"opacity": {filters.MatrixCapabilityComplex, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		aFn := filters.TransferFunc{Type: filters.TransferTable, TableValues: []float64{0, clampFilter01(op.Value)}}
		return filters.NewFEComponentTransfer(in.prev, in.space, [4]filters.TransferFunc{identityTransfer, identityTransfer, identityTransfer, aFn})
	}},
	// brightness(a): linear slope=a intercept=0 on R,G,B. a may exceed 1.
	"brightness": {filters.MatrixCapabilityComplex, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		lin := filters.TransferFunc{Type: filters.TransferLinear, Slope: max(op.Value, 0)}
		return filters.NewFEComponentTransfer(in.prev, in.space, [4]filters.TransferFunc{lin, lin, lin, identityTransfer})
	}},
	// contrast(a): linear slope=a intercept=-(0.5*a)+0.5 on R,G,B.
	"contrast": {filters.MatrixCapabilityComplex, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		s := max(op.Value, 0)
		lin := filters.TransferFunc{Type: filters.TransferLinear, Slope: s, Intercept: -(0.5 * s) + 0.5}
		return filters.NewFEComponentTransfer(in.prev, in.space, [4]filters.TransferFunc{lin, lin, lin, identityTransfer})
	}},
	// blur(r): FEGaussianBlur with stdDeviation = r (NOT r/2).
	"blur": {filters.MatrixCapabilityScaleTranslate, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		r := max(op.Value, 0)
		sx, sy := in.mapping.ParamToLayerSize(r, r)
		return filters.NewFEGaussianBlur(in.prev, in.space, sx, sy)
	}},
	"drop-shadow": {filters.MatrixCapabilityScaleTranslate, func(op css.FilterFunction, in cssFilterInputs) filters.FilterEffect {
		shadowColor := op.ShadowColor
		if op.ShadowUseCurrentColor {
			shadowColor = in.currentColor
		}
		dx, dy := in.mapping.ParamToLayerVector(op.ShadowOffsetX, op.ShadowOffsetY)
		sx, sy := in.mapping.ParamToLayerSize(op.ShadowBlur, op.ShadowBlur)
		return filters.NewFEDropShadow(in.prev, in.srcAlpha, in.space,
			dx, dy, sx, sy,
			shadowColor.R, shadowColor.G, shadowColor.B, shadowColor.A)
	}},
}

// identityTransfer leaves a channel unchanged.
var identityTransfer = filters.TransferFunc{Type: filters.TransferIdentity}

// grayscaleMatrix builds the 5x4 matrix for grayscale(a): each row is the
// luminance vector (0.2126, 0.7152, 0.0722) blended toward identity by (1-a).
func grayscaleMatrix(a float64) []float64 {
	lr, lg, lb := 0.2126, 0.7152, 0.0722
	inv := 1 - a
	return []float64{
		lr + inv*(1-lr), lg - inv*lg, lb - inv*lb, 0, 0,
		lr - inv*lr, lg + inv*(1-lg), lb - inv*lb, 0, 0,
		lr - inv*lr, lg - inv*lg, lb + inv*(1-lb), 0, 0,
		0, 0, 0, 1, 0,
	}
}

// sepiaMatrix builds the 5x4 matrix for sepia(a): the fixed sepia basis
// blended toward identity by (1-a). Filter Effects 1 §13.
func sepiaMatrix(a float64) []float64 {
	// Sepia basis rows.
	s := [3][3]float64{
		{0.393, 0.769, 0.189},
		{0.349, 0.686, 0.168},
		{0.272, 0.534, 0.131},
	}
	inv := 1 - a
	row := func(i int) [3]float64 {
		var r [3]float64
		for j := 0; j < 3; j++ {
			id := 0.0
			if i == j {
				id = 1.0
			}
			r[j] = s[i][j] + inv*(id-s[i][j])
		}
		return r
	}
	r0, r1, r2 := row(0), row(1), row(2)
	return []float64{
		r0[0], r0[1], r0[2], 0, 0,
		r1[0], r1[1], r1[2], 0, 0,
		r2[0], r2[1], r2[2], 0, 0,
		0, 0, 0, 1, 0,
	}
}
