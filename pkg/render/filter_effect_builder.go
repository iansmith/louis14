package render

import (
	"image"
	"math"
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
	// ReferenceBox is the filter reference box in absolute device pixels,
	// unsnapped (Blink's gfx::RectF): the border box, or for a filter that
	// moves pixels the layer's bounding box including its self-painting
	// descendants (UpdateFilterReferenceBox). A backdrop filter's is the
	// pixel-snapped border box (BackdropFilterBounds).
	ReferenceBox geometry.RectF
	// FilterViewport is, in absolute device pixels, a layer's pixel-snapped
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
	// are relative to in place of FilterViewport's origin: an SVG shape's user
	// space origin in device pixels, whose viewport is its <svg>'s.
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
	// ExternalSVGFetcher resolves `filter: url(doc#id)` where doc is a
	// data: / file:// / http(s):// URL pointing at an external SVG
	// document. Returns the raw document bytes. May be nil — in that
	// case external references fall back to a null filter (caller
	// paints unfiltered, matching Blink's tainted-resource behavior
	// per Filter Effects 1 §3.1). LOU-130 Phase 2 wiring.
	ExternalSVGFetcher func(uri string) ([]byte, error)
	// ImageFetcher resolves `<feImage href="path.png">` external image
	// references. May be nil — feImage falls back to LoadImage which
	// handles data: URIs and absolute filesystem paths without a
	// fetcher. Same fetcher the renderer's `<img>`/background-image
	// path uses; see Renderer.imageFetcher.
	ImageFetcher func(uri string) ([]byte, error)
	// Mapping maps the filter functions' parameters (blur deviations,
	// shadow offsets, in CSS px) into the layer the graph is evaluated
	// in, as Skia's filters map theirs through skif::Mapping. The zero
	// value is the identity.
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

// userSpaceOriginPixel is userSpaceOrigin rounded to a device pixel, as the
// filter element adapter takes it.
func (b *FilterEffectBuilder) userSpaceOriginPixel() image.Point {
	return roundPoint(b.userSpaceOrigin())
}

// BuildFilterEffect builds a filters.Filter for a chained CSS filter list.
// Returns nil when the list is empty or contains only unsupported entries.
//
// A single `url(#id)` is dispatched to BuildReferenceFilter so the
// reference filter's own region / interpolation space is preserved
// (single-url tests rely on this). A mixed list walks the ops in order,
// each `url()` becoming a sub-graph whose SourceGraphic is rewired to
// the running chain output (Blink's FilterEffectBuilder::Build*
// FilterChain composition); each shorthand entry is built with the
// running output as its source.
func (b *FilterEffectBuilder) BuildFilterEffect(ops []css.FilterFunction) *filters.Filter {
	if len(ops) == 0 {
		return nil
	}
	if len(ops) == 1 && ops[0].Name == "url" {
		return b.BuildReferenceFilter(ops[0].URL.Absolute)
	}
	const space = filters.InterpolationSpaceSRGB
	src := filters.NewSourceGraphic(space)
	srcAlpha := filters.NewSourceAlpha(src, space)

	var prev filters.FilterEffect = src
	for _, op := range ops {
		if op.Name == "url" {
			sub := b.buildReferenceFilterWithSource(op.URL.Absolute, prev)
			if sub != nil && sub.LastEffect != nil {
				prev = sub.LastEffect
			}
			// An unresolvable url() (missing fragment, cycle, fetch
			// fail) drops the entry per Filter Effects 1 §3.1 — the
			// chain continues with the same `prev`.
			continue
		}
		next := buildOneEffect(op, prev, src, srcAlpha, space, b.CurrentColor, b.Mapping)
		if next != nil {
			prev = next
		}
	}
	if prev == src {
		return nil
	}

	f := &filters.Filter{
		Source:      src,
		SourceAlpha: srcAlpha,
		LastEffect:  prev,
	}
	// A shorthand-only chain has no region; the caller sizes its buffer
	// (filters.Filter.EnsureRegion). A chain with a url() stage starts from
	// the reference box mapped through the chain, with no per-stage crop
	// (LOU-704 ports the exact sizing). An SVG shape's chain then unites its
	// paint extent into it (paintWithFilterChain); the HTML layer path keeps
	// it as is (LOU-780).
	if hasReferenceFilter(ops) {
		f.FilterRegion = f.MapRect(snapRectFToDevice(b.ReferenceBox))
	}
	return f
}

// hasReferenceFilter reports whether a filter list has a url() stage.
func hasReferenceFilter(ops []css.FilterFunction) bool {
	for _, op := range ops {
		if op.Name == "url" {
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
	return b.buildReferenceFilterWithSource(id, nil)
}

// buildReferenceFilterWithSource is BuildReferenceFilter with an optional
// chain-source override: when prev is non-nil, "SourceGraphic" references
// inside the resolved filter resolve to prev (the running output of an
// outer CSS filter-value-list chain), mirroring Blink's
// FilterEffectBuilder::Build* pattern of swapping SourceGraphic per stage.
func (b *FilterEffectBuilder) buildReferenceFilterWithSource(id string, prev filters.FilterEffect) *filters.Filter {
	if id == "" {
		return nil
	}

	// FilterFunction.URL is the raw inside-of-`url(...)` content. It may
	// be one of:
	//   - "#frag"               → same-document, look up frag in Resources
	//   - "data:...,...#frag"   → external data URL
	//   - "file:///path#frag"   → external file URL
	//   - "http(s)://host/p#f"  → external network URL
	//   - "path/file.svg#frag"  → relative external URL (test harness
	//                              resolves against document base)
	//
	// Same-document fast path: leading `#` (after trim/quote strip).
	docURL, fragID := splitFilterRef(id)
	if docURL == "" {
		return b.buildSameDocReferenceFilter(fragID, prev)
	}
	return b.buildExternalReferenceFilter(docURL, fragID, prev)
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

// buildSameDocReferenceFilter resolves a `filter: url(#id)` reference
// against the in-page SVGResourceRegistry. The original (pre-LOU-130)
// behavior for bare-fragment references. When chainSource is non-nil
// the resolved filter's SourceGraphic references are rewired to
// chainSource (CSS filter-value-list chain composition).
func (b *FilterEffectBuilder) buildSameDocReferenceFilter(id string, chainSource filters.FilterEffect) *filters.Filter {
	if b.Resources == nil {
		return nil
	}
	filter, ok := b.Resources.LookupAsFilter(id)
	if !ok || filter == nil {
		return nil
	}
	if filter.CycleState() == svg.SVGCycleHasCycle {
		return nil
	}

	space := filterInterpolationSpace(filter)

	// Resolve the filter region from filterUnits + the reference box, both
	// in absolute device pixels. userSpaceOnUse x/y are relative to the
	// layer's local origin, FilterViewport's origin.
	regionF := filter.ResourceBoundingBox(b.ReferenceBox, b.userSpaceOrigin(), b.viewportLengthContext())
	region := snapRectFToDevice(regionF)

	adapter := &svgFilterElementAdapter{
		filter:       filter,
		filterRegion: region,
		referenceBox: b.ReferenceBox,
		// The primitive subregion percentage viewport is FilterViewport,
		// not the filter region, which can carry the default 10% expansion.
		subregionViewport:  b.FilterViewport,
		userSpaceOrigin:    b.userSpaceOriginPixel(),
		space:              space,
		resolveStyle:       b.ResolveStyle,
		resources:          b.Resources,
		imageFetcher:       b.ImageFetcher,
		externalSVGFetcher: b.ExternalSVGFetcher,
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
	if fragID == "" || fetch == nil {
		return nil
	}
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

// buildExternalReferenceFilter handles `filter: url(doc#id)` where doc
// is a cross-document URL (data:, file://, http(s)://, or a relative
// path resolved by the harness). Fetches the document via
// ExternalSVGFetcher, parses it, finds `<filter id="fragID">`, and
// builds the FilterEffect graph. Returns nil on any failure — Blink's
// behavior for tainted/unreachable external filters is a null filter
// (caller paints source unfiltered) per Filter Effects 1 §3.1. When
// chainSource is non-nil, SourceGraphic references inside the fetched
// filter are rewired to it (CSS filter-value-list chain composition).
func (b *FilterEffectBuilder) buildExternalReferenceFilter(docURL, fragID string, chainSource filters.FilterEffect) *filters.Filter {
	resource := externalSVGFilter(b.ExternalSVGFetcher, docURL, fragID, b.viewportLengthContext())
	if resource == nil {
		return nil
	}
	space := filterInterpolationSpace(resource)

	// Resolve the filter region — same logic as the same-doc path.
	regionF := resource.ResourceBoundingBox(b.ReferenceBox, b.userSpaceOrigin(), b.viewportLengthContext())
	region := snapRectFToDevice(regionF)

	adapter := &svgFilterElementAdapter{
		filter:            resource,
		filterRegion:      region,
		referenceBox:      b.ReferenceBox,
		subregionViewport: b.FilterViewport,
		userSpaceOrigin:   b.userSpaceOriginPixel(),
		space:             space,
		// No ResolveStyle for external documents — filter primitives
		// read presentational attributes directly.
		resolveStyle:       nil,
		resources:          b.Resources,
		imageFetcher:       b.ImageFetcher,
		externalSVGFetcher: b.ExternalSVGFetcher,
	}
	builder := filters.NewSVGFilterBuilder(space)
	builder.SetSourceOverride(chainSource)
	return builder.BuildGraph(adapter)
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
	if v == "" {
		return "", ""
	}
	hashIdx := strings.IndexByte(v, '#')
	if hashIdx < 0 {
		// No fragment — for filter references, the fragment is required;
		// caller will fail to find a `<filter>` element and return nil.
		return v, ""
	}
	return v[:hashIdx], v[hashIdx+1:]
}

// buildOneEffect maps a single CSS filter function to its fe* equivalent,
// fed by prev. src / srcAlpha are the graph builtins (drop-shadow needs them).
func buildOneEffect(op css.FilterFunction, prev filters.FilterEffect,
	src *filters.SourceGraphic, srcAlpha *filters.SourceAlpha,
	space filters.InterpolationSpace, currentColor css.Color, mapping filters.Mapping) filters.FilterEffect {

	switch op.Name {
	case "grayscale":
		a := clampFilter01(op.Value)
		// grayscale(a): blend each row of the luminance basis toward identity
		// by (1-a). Filter Effects 1 §13.
		m := grayscaleMatrix(a)
		return filters.NewFEColorMatrix(prev, space, filters.ColorMatrixTypeMatrix, m)

	case "sepia":
		a := clampFilter01(op.Value)
		m := sepiaMatrix(a)
		return filters.NewFEColorMatrix(prev, space, filters.ColorMatrixTypeMatrix, m)

	case "saturate":
		// saturate accepts <number> or <percentage>, clamped at 0 minimum.
		s := op.Value
		if s < 0 {
			s = 0
		}
		return filters.NewFEColorMatrix(prev, space, filters.ColorMatrixTypeSaturate, []float64{s})

	case "hue-rotate":
		return filters.NewFEColorMatrix(prev, space, filters.ColorMatrixTypeHueRotate, []float64{op.Value})

	case "invert":
		a := clampFilter01(op.Value)
		// invert(a): table [a, 1-a] on R,G,B; alpha untouched.
		tbl := filters.TransferFunc{Type: filters.TransferTable, TableValues: []float64{a, 1 - a}}
		ident := filters.TransferFunc{Type: filters.TransferIdentity}
		return filters.NewFEComponentTransfer(prev, space,
			[4]filters.TransferFunc{tbl, tbl, tbl, ident})

	case "opacity":
		a := clampFilter01(op.Value)
		// opacity(a): table [0, a] on A only.
		ident := filters.TransferFunc{Type: filters.TransferIdentity}
		aFn := filters.TransferFunc{Type: filters.TransferTable, TableValues: []float64{0, a}}
		return filters.NewFEComponentTransfer(prev, space,
			[4]filters.TransferFunc{ident, ident, ident, aFn})

	case "brightness":
		// brightness(a): linear slope=a intercept=0 on R,G,B. a may exceed 1.
		s := op.Value
		if s < 0 {
			s = 0
		}
		lin := filters.TransferFunc{Type: filters.TransferLinear, Slope: s, Intercept: 0}
		ident := filters.TransferFunc{Type: filters.TransferIdentity}
		return filters.NewFEComponentTransfer(prev, space,
			[4]filters.TransferFunc{lin, lin, lin, ident})

	case "contrast":
		// contrast(a): linear slope=a intercept=-(0.5*a)+0.5 on R,G,B.
		s := op.Value
		if s < 0 {
			s = 0
		}
		lin := filters.TransferFunc{Type: filters.TransferLinear, Slope: s, Intercept: -(0.5 * s) + 0.5}
		ident := filters.TransferFunc{Type: filters.TransferIdentity}
		return filters.NewFEComponentTransfer(prev, space,
			[4]filters.TransferFunc{lin, lin, lin, ident})

	case "blur":
		// blur(r): FEGaussianBlur with stdDeviation = r (NOT r/2).
		r := op.Value
		if r < 0 {
			r = 0
		}
		sx, sy := mapping.ParamToLayerSize(r, r)
		return filters.NewFEGaussianBlur(prev, space, sx, sy)

	case "drop-shadow":
		shadowColor := op.ShadowColor
		if op.ShadowUseCurrentColor {
			shadowColor = currentColor
		}
		dx, dy := mapping.ParamToLayerVector(op.ShadowOffsetX, op.ShadowOffsetY)
		sx, sy := mapping.ParamToLayerSize(op.ShadowBlur, op.ShadowBlur)
		return filters.NewFEDropShadow(prev, srcAlpha, space,
			dx, dy, sx, sy,
			shadowColor.R, shadowColor.G, shadowColor.B, shadowColor.A)

	default:
		// url() references and unknown functions are handled elsewhere.
		return nil
	}
}

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

// snapRectFToDevice converts a float user-space filter-region rect into an
// integer device-pixel rect. Edges are first nudged to the nearest integer
// when within a small epsilon (objectBoundingBox math introduces float noise:
// e.g. an exact -10% of a 100px box computes to 7.9999999998, not 8.0), then
// floor/ceil out so the integer region fully covers the float region. Without
// the epsilon snap, int()/floor() of 7.9999999998 truncates to 7 — a 1px
// offset on the filter region top, visible as a 1px seam on the filter output.
func snapRectFToDevice(r geometry.RectF) image.Rectangle {
	const eps = 1e-6
	snap := func(v float64) float64 {
		if n := math.Round(v); math.Abs(v-n) < eps {
			return n
		}
		return v
	}
	x0 := snap(r.X())
	y0 := snap(r.Y())
	x1 := snap(r.X() + r.Width())
	y1 := snap(r.Y() + r.Height())
	return image.Rect(
		int(math.Floor(x0)), int(math.Floor(y0)),
		int(math.Ceil(x1)), int(math.Ceil(y1)),
	)
}
