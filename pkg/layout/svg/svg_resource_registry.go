package svg

// SVGResourceRegistry is the `id` → resource map for `url(#…)`
// references. Mirrors Blink's per-document SVGTreeScopeResources
// (core/svg/svg_resource.{h,cc}, core/svg/svg_tree_scope_resources.{h,cc}).
//
// Phase 6 unifies the per-kind maps used in Phases 4-5 into a single
// `id`-keyed map of `SVGResource` (the umbrella interface). The single
// namespace matches the SVG 2 §3.1 rule that all resource elements
// share the document's fragment id space — a `<linearGradient id="x">`
// and a `<clipPath id="x">` cannot coexist (first-wins per registration
// order, which the registry preserves through pre-order document walk).
//
// Per-kind lookup helpers (LookupAsPaintServer, LookupClipper,
// LookupMasker) wrap the umbrella Lookup with a type assertion so
// callsites don't have to switch on the concrete type. Each helper
// returns (zero, false) for an unknown id OR for an id resolved to the
// wrong resource kind (matches Blink's SVGTreeScopeResources::ResourceForId
// which returns nullptr for a wrong-typed cast).
type SVGResourceRegistry struct {
	resources map[string]SVGResource
}

// NewSVGResourceRegistry builds an empty registry.
func NewSVGResourceRegistry() *SVGResourceRegistry {
	return &SVGResourceRegistry{
		resources: make(map[string]SVGResource),
	}
}

// Register attaches a resource by ID. Empty ID is silently rejected
// (per SVG 2 §3.1 an id is required for `url(#…)` reachability).
// Duplicate IDs follow first-wins (Blink's SVGTreeScopeResources rejects
// re-registration without raising an error — matches our behavior).
func (r *SVGResourceRegistry) Register(res SVGResource) {
	if r == nil || res == nil {
		return
	}
	id := res.ID()
	if id == "" {
		return
	}
	if _, exists := r.resources[id]; exists {
		return
	}
	r.resources[id] = res
}

// RegisterPaintServer attaches a paint-server resource (gradient or
// pattern) by ID. Thin compatibility wrapper kept so Phase 4/5 callsites
// in svg_root.go don't need to drop their typed signatures.
func (r *SVGResourceRegistry) RegisterPaintServer(server SVGResourcePaintServer) {
	if server == nil {
		return
	}
	r.Register(server)
}

// RegisterClipper attaches a `<clipPath>` resource by ID.
func (r *SVGResourceRegistry) RegisterClipper(c *SVGResourceClipper) {
	if c == nil {
		return
	}
	r.Register(c)
}

// RegisterMasker attaches a `<mask>` resource by ID.
func (r *SVGResourceRegistry) RegisterMasker(m *SVGResourceMasker) {
	if m == nil {
		return
	}
	r.Register(m)
}

// RegisterFilter attaches a `<filter>` resource by ID.
func (r *SVGResourceRegistry) RegisterFilter(f *SVGResourceFilter) {
	if f == nil {
		return
	}
	r.Register(f)
}

// Lookup returns the resource registered under `id`. Returns (nil,
// false) for an unknown id. The id is matched case-sensitively per the
// HTML/SVG id-attribute model. Callers that want a kind-typed result
// use the per-kind Lookup* wrappers below.
func (r *SVGResourceRegistry) Lookup(id string) (SVGResource, bool) {
	if r == nil {
		return nil, false
	}
	res, ok := r.resources[id]
	return res, ok
}

// LookupAsPaintServer returns the paint-server resource (gradient or
// pattern) registered under `id`. Returns (nil, false) for an unknown
// id, or for an id registered as a non-paint-server kind (e.g. a
// `<clipPath id="x">` reached by `fill: url(#x)`).
func (r *SVGResourceRegistry) LookupAsPaintServer(id string) (SVGResourcePaintServer, bool) {
	res, ok := r.Lookup(id)
	if !ok {
		return nil, false
	}
	if ps, ok := res.(SVGResourcePaintServer); ok {
		return ps, true
	}
	return nil, false
}

// LookupClipper returns the `<clipPath>` resource registered under
// `id`. Returns (nil, false) when the id is unknown or registered as
// a different kind.
func (r *SVGResourceRegistry) LookupClipper(id string) (*SVGResourceClipper, bool) {
	res, ok := r.Lookup(id)
	if !ok {
		return nil, false
	}
	c, ok := res.(*SVGResourceClipper)
	if !ok {
		return nil, false
	}
	return c, true
}

// LookupMasker returns the `<mask>` resource registered under `id`.
// Returns (nil, false) when the id is unknown or registered as a
// different kind.
func (r *SVGResourceRegistry) LookupMasker(id string) (*SVGResourceMasker, bool) {
	res, ok := r.Lookup(id)
	if !ok {
		return nil, false
	}
	m, ok := res.(*SVGResourceMasker)
	if !ok {
		return nil, false
	}
	return m, true
}

// LookupAsFilter returns the `<filter>` resource registered under
// `id`. Returns (nil, false) when the id is unknown or registered as
// a different kind.
func (r *SVGResourceRegistry) LookupAsFilter(id string) (*SVGResourceFilter, bool) {
	res, ok := r.Lookup(id)
	if !ok {
		return nil, false
	}
	f, ok := res.(*SVGResourceFilter)
	if !ok {
		return nil, false
	}
	return f, true
}

// ForEach invokes `fn` for each registered resource in map-iteration
// order (unspecified). Used by the renderer to merge per-SVGRoot
// registries into a single page-wide registry and by the cycle solver.
func (r *SVGResourceRegistry) ForEach(fn func(SVGResource)) {
	if r == nil || fn == nil {
		return
	}
	for _, res := range r.resources {
		fn(res)
	}
}

// ForEachPaintServer invokes `fn` for each registered paint server.
// Retained for Phase 4/5 callsites; new code should use ForEach +
// type assertion, or LookupAsPaintServer at the resolution site.
func (r *SVGResourceRegistry) ForEachPaintServer(fn func(SVGResourcePaintServer)) {
	if r == nil || fn == nil {
		return
	}
	for _, res := range r.resources {
		if ps, ok := res.(SVGResourcePaintServer); ok {
			fn(ps)
		}
	}
}

// ForEachClipper invokes `fn` for each registered `<clipPath>`.
func (r *SVGResourceRegistry) ForEachClipper(fn func(*SVGResourceClipper)) {
	if r == nil || fn == nil {
		return
	}
	for _, res := range r.resources {
		if c, ok := res.(*SVGResourceClipper); ok {
			fn(c)
		}
	}
}

// ForEachMasker invokes `fn` for each registered `<mask>`.
func (r *SVGResourceRegistry) ForEachMasker(fn func(*SVGResourceMasker)) {
	if r == nil || fn == nil {
		return
	}
	for _, res := range r.resources {
		if m, ok := res.(*SVGResourceMasker); ok {
			fn(m)
		}
	}
}

// ForEachFilter invokes `fn` for each registered `<filter>`.
func (r *SVGResourceRegistry) ForEachFilter(fn func(*SVGResourceFilter)) {
	if r == nil || fn == nil {
		return
	}
	for _, res := range r.resources {
		if f, ok := res.(*SVGResourceFilter); ok {
			fn(f)
		}
	}
}

// Count returns the number of registered resources. Used by the
// cycle-solver tests to verify registration counts.
func (r *SVGResourceRegistry) Count() int {
	if r == nil {
		return 0
	}
	return len(r.resources)
}
