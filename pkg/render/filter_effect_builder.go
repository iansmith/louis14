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
	// ReferenceBox is the element's border box in absolute device pixels.
	ReferenceBox image.Rectangle
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
		return b.BuildReferenceFilter(ops[0].URL)
	}
	const space = filters.InterpolationSpaceSRGB
	src := filters.NewSourceGraphic(space)
	srcAlpha := filters.NewSourceAlpha(src, space)

	var prev filters.FilterEffect = src
	for _, op := range ops {
		if op.Name == "url" {
			sub := b.buildReferenceFilterWithSource(op.URL, prev)
			if sub != nil && sub.LastEffect != nil {
				prev = sub.LastEffect
			}
			// An unresolvable url() (missing fragment, cycle, fetch
			// fail) drops the entry per Filter Effects 1 §3.1 — the
			// chain continues with the same `prev`.
			continue
		}
		next := buildOneEffect(op, prev, src, srcAlpha, space, b.CurrentColor)
		if next != nil {
			prev = next
		}
	}
	if prev == src {
		return nil
	}

	f := &filters.Filter{
		ReferenceBox: b.ReferenceBox,
		Source:       src,
		SourceAlpha:  srcAlpha,
		LastEffect:   prev,
	}
	f.FilterRegion = f.MapRect(b.ReferenceBox)
	return f
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

	// Resolve the SVG operating space from
	// color-interpolation-filters on the filter element. Default is
	// linearRGB per SVG 1.1 §4.2.
	space := filters.InterpolationSpaceLinearRGB
	if st := filter.Style(); st != nil {
		if v, ok := st.Get("color-interpolation-filters"); ok {
			space = filters.ResolveInterpolationSpace(v)
		}
	}

	// Resolve the filter region from filterUnits + the reference box.
	// Both inputs are in absolute device pixels. The
	// SVGResourceFilter.ResourceBoundingBox operates on float rects
	// so wrap b.ReferenceBox into a geometry.RectF, run resolution,
	// and convert back.
	refBoxF := geometry.NewRectF(
		float64(b.ReferenceBox.Min.X),
		float64(b.ReferenceBox.Min.Y),
		float64(b.ReferenceBox.Dx()),
		float64(b.ReferenceBox.Dy()),
	)
	// CSS `filter: url(...)` has no SVG transform stack — the
	// "user-space origin" equivalent for this path is the reference
	// box origin (where the filtered element lives in absolute device
	// coords). Used by ResourceBoundingBox to anchor explicit
	// userSpaceOnUse x/y values.
	userOriginF := geometry.PointF{X: refBoxF.X(), Y: refBoxF.Y()}
	regionF := filter.ResourceBoundingBox(refBoxF, userOriginF, svg.NewSVGLengthContext(refBoxF.Size))
	region := image.Rect(
		int(regionF.X()),
		int(regionF.Y()),
		int(regionF.X()+regionF.Width()),
		int(regionF.Y()+regionF.Height()),
	)

	// userSpaceOrigin for the CSS `filter: url(...)` path is the
	// reference box origin: this path has no SVG transform stack
	// because the FilterEffectBuilder is constructed with
	// b.ReferenceBox already in absolute device coords. The CSS
	// filter reference operates on the element's reference box, so
	// its "user-space origin" equivalent is the reference box origin.
	adapter := &svgFilterElementAdapter{
		filter:          filter,
		filterRegion:    region,
		referenceBox:    b.ReferenceBox,
		userSpaceOrigin: b.ReferenceBox.Min,
		space:           space,
		resolveStyle:    b.ResolveStyle,
	}
	builder := filters.NewSVGFilterBuilder(space)
	builder.SetSourceOverride(chainSource)
	return builder.BuildGraph(adapter)
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
	if fragID == "" || b.ExternalSVGFetcher == nil {
		return nil
	}
	data, err := b.ExternalSVGFetcher(docURL)
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

	// Build the SVGResourceFilter from the external `<filter>` element.
	// External documents don't share the host page's computed-style
	// cascade, so styleResolver is nil — filter primitives fall back to
	// raw attribute lookup, which is what the external-resource tests
	// exercise (`flood-color="green"`, `type="hueRotate" values="..."`).
	resource := svg.NewSVGResourceFilter(target, svg.NewSVGLengthContext(geometry.SizeF{
		Width:  float64(b.ReferenceBox.Dx()),
		Height: float64(b.ReferenceBox.Dy()),
	}), nil)
	if resource == nil {
		return nil
	}

	// color-interpolation-filters defaults to linearRGB per SVG 1.1
	// §4.2. For external filters we read it as an attribute (no
	// cascade in the fetched document).
	space := filters.InterpolationSpaceLinearRGB
	if v, ok := target.Attribute("color-interpolation-filters"); ok {
		space = filters.ResolveInterpolationSpace(v)
	}

	// Resolve the filter region — same logic as the same-doc path.
	refBoxF := geometry.NewRectF(
		float64(b.ReferenceBox.Min.X),
		float64(b.ReferenceBox.Min.Y),
		float64(b.ReferenceBox.Dx()),
		float64(b.ReferenceBox.Dy()),
	)
	userOriginF := geometry.PointF{X: refBoxF.X(), Y: refBoxF.Y()}
	regionF := resource.ResourceBoundingBox(refBoxF, userOriginF, svg.NewSVGLengthContext(refBoxF.Size))
	region := image.Rect(
		int(regionF.X()),
		int(regionF.Y()),
		int(regionF.X()+regionF.Width()),
		int(regionF.Y()+regionF.Height()),
	)

	adapter := &svgFilterElementAdapter{
		filter:          resource,
		filterRegion:    region,
		referenceBox:    b.ReferenceBox,
		userSpaceOrigin: b.ReferenceBox.Min,
		space:           space,
		// No ResolveStyle for external documents — filter primitives
		// read presentational attributes directly.
		resolveStyle: nil,
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
	space filters.InterpolationSpace, currentColor css.Color) filters.FilterEffect {

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
		return filters.NewFEGaussianBlur(prev, space, r, r)

	case "drop-shadow":
		shadowColor := op.ShadowColor
		if op.ShadowUseCurrentColor {
			shadowColor = currentColor
		}
		return filters.NewFEDropShadow(prev, srcAlpha, space,
			op.ShadowOffsetX, op.ShadowOffsetY, op.ShadowBlur,
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
