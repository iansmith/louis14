package svg

// SVGResourceRegistry is a simple `id` → resource lookup attached to
// an SVGRoot. Mirrors Blink's per-document SVGResourceRegistry
// (core/svg/svg_resource.{h,cc}) but scoped to a single outer SVG
// subtree — louis14's resource scope is the outermost `<svg>`, which
// matches the way `url(#…)` references resolve against the same
// document fragment.
//
// Phase 4 surface is intentionally minimal: only paint servers
// (linear gradient, radial gradient, pattern) are registered; Phase 5
// adds <clipPath>/<mask>, Phase 6 promotes this to the full
// `SVGResource` abstraction with cycle detection across all resource
// kinds. Paint-server lookup at paint time is `Lookup(id)` returning
// the registered interface value.
type SVGResourceRegistry struct {
	paintServers map[string]SVGResourcePaintServer
}

// NewSVGResourceRegistry builds an empty registry. Returned by-value
// so callers can store it inline on SVGRoot without allocating an
// extra heap object.
func NewSVGResourceRegistry() *SVGResourceRegistry {
	return &SVGResourceRegistry{
		paintServers: make(map[string]SVGResourcePaintServer),
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
