package svg

// SVGResourceRegistry is a simple `id` → resource lookup attached to
// an SVGRoot. Mirrors Blink's per-document SVGResourceRegistry
// (core/svg/svg_resource.{h,cc}) but scoped to a single outer SVG
// subtree — louis14's resource scope is the outermost `<svg>`, which
// matches the way `url(#…)` references resolve against the same
// document fragment.
//
// Phase 5 widens this to carry clip-paths and masks alongside paint
// servers. The registry keeps separate per-kind maps because the
// resolution paths are different: paint servers feed the paint-server
// shader pipeline (svg_paint_server.go), clippers feed the clip path
// pipeline (svg_clip_painter.go), and maskers feed the mask painter
// (svg_mask_painter.go). Phase 6 will promote this to a single
// `SVGResource` umbrella interface with cycle detection across all
// resource kinds (mirrors Blink's LayoutSVGResourceContainer).
type SVGResourceRegistry struct {
	paintServers map[string]SVGResourcePaintServer
	clippers     map[string]*SVGResourceClipper
	maskers      map[string]*SVGResourceMasker
}

// NewSVGResourceRegistry builds an empty registry. Returned by-value
// so callers can store it inline on SVGRoot without allocating an
// extra heap object.
func NewSVGResourceRegistry() *SVGResourceRegistry {
	return &SVGResourceRegistry{
		paintServers: make(map[string]SVGResourcePaintServer),
		clippers:     make(map[string]*SVGResourceClipper),
		maskers:      make(map[string]*SVGResourceMasker),
	}
}

// RegisterPaintServer attaches a paint-server resource by ID. An
// empty ID is silently rejected (the spec requires an `id` attribute
// for `url(#…)` reachability per SVG 2 §3.1). Duplicate IDs follow
// the spec's "first wins" rule: subsequent registrations with the
// same ID are ignored. Mirrors how Blink's SVGTreeScopeResources
// rejects re-registration without raising an error.
func (r *SVGResourceRegistry) RegisterPaintServer(server SVGResourcePaintServer) {
	if r == nil || server == nil {
		return
	}
	id := server.ID()
	if id == "" {
		return
	}
	if _, exists := r.paintServers[id]; exists {
		return
	}
	r.paintServers[id] = server
}

// RegisterClipper attaches a `<clipPath>` resource by ID. Same id
// rules as RegisterPaintServer (empty id rejected, first-write wins).
// `<clipPath>` and paint-server IDs share the same `url(#id)`
// namespace per SVG 2 §3.1 — but louis14 keys them by resolution
// path, so a `<linearGradient id="x">` and `<clipPath id="x">` can
// coexist (Blink's behavior is to refuse the second registration with
// the same id; if a real reftest exercises that pathology we'll add
// a cross-kind dedup. For Phase 5 the per-kind isolation is fine.)
func (r *SVGResourceRegistry) RegisterClipper(c *SVGResourceClipper) {
	if r == nil || c == nil {
		return
	}
	id := c.ID()
	if id == "" {
		return
	}
	if _, exists := r.clippers[id]; exists {
		return
	}
	r.clippers[id] = c
}

// RegisterMasker attaches a `<mask>` resource by ID.
func (r *SVGResourceRegistry) RegisterMasker(m *SVGResourceMasker) {
	if r == nil || m == nil {
		return
	}
	id := m.ID()
	if id == "" {
		return
	}
	if _, exists := r.maskers[id]; exists {
		return
	}
	r.maskers[id] = m
}

// Lookup returns the paint-server resource registered under `id`.
// Returns (nil, false) for an unknown id. The id is matched case-
// sensitively, matching the SVG/HTML id-attribute model.
func (r *SVGResourceRegistry) Lookup(id string) (SVGResourcePaintServer, bool) {
	if r == nil {
		return nil, false
	}
	s, ok := r.paintServers[id]
	return s, ok
}

// LookupClipper returns the `<clipPath>` resource registered under
// `id`. Returns (nil, false) for an unknown id.
func (r *SVGResourceRegistry) LookupClipper(id string) (*SVGResourceClipper, bool) {
	if r == nil {
		return nil, false
	}
	c, ok := r.clippers[id]
	return c, ok
}

// LookupMasker returns the `<mask>` resource registered under `id`.
func (r *SVGResourceRegistry) LookupMasker(id string) (*SVGResourceMasker, bool) {
	if r == nil {
		return nil, false
	}
	m, ok := r.maskers[id]
	return m, ok
}

// ForEachPaintServer invokes `fn` for each registered paint server.
// Used by the renderer to merge per-SVGRoot registries into a single
// page-wide registry (collectSVGResources in pkg/render). Iteration
// order is map-iteration order (unspecified) — callers that need a
// stable order must sort externally. Phase 5 only uses this for
// merging; the merge target's first-wins rule is order-insensitive.
func (r *SVGResourceRegistry) ForEachPaintServer(fn func(SVGResourcePaintServer)) {
	if r == nil || fn == nil {
		return
	}
	for _, s := range r.paintServers {
		fn(s)
	}
}

// ForEachClipper invokes `fn` for each registered `<clipPath>`.
func (r *SVGResourceRegistry) ForEachClipper(fn func(*SVGResourceClipper)) {
	if r == nil || fn == nil {
		return
	}
	for _, c := range r.clippers {
		fn(c)
	}
}

// ForEachMasker invokes `fn` for each registered `<mask>`.
func (r *SVGResourceRegistry) ForEachMasker(fn func(*SVGResourceMasker)) {
	if r == nil || fn == nil {
		return
	}
	for _, m := range r.maskers {
		fn(m)
	}
}
