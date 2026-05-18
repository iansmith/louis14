package svg

// SVGResource is the umbrella interface every `url(#id)`-addressable
// SVG resource element implements. Mirrors Blink's SVGResource +
// LayoutSVGResourceContainer abstract bases
// (core/svg/svg_resource.{h,cc}, core/layout/svg/layout_svg_resource_container.{h,cc}).
//
// Concrete implementations:
//
//   - *SVGLinearGradient, *SVGRadialGradient, *SVGPattern — paint
//     servers (also satisfy SVGResourcePaintServer).
//   - *SVGResourceClipper — `<clipPath>` resource.
//   - *SVGResourceMasker — `<mask>` resource.
//   - (Phase 7) *SVGResourceFilter — `<filter>` resource.
//
// The interface is intentionally narrow: ID() for registry lookup,
// Kind() for runtime dispatch and printable diagnostics, and the
// CycleState accessors used by the cycle-detection DFS. Per-kind
// resolution methods (gradient/pattern shader building, AsPath on
// clippers, Rasterize on maskers) live on the concrete types — the
// render package switches on the concrete type rather than going
// through this interface, matching the per-kind shader/raster pipeline
// established in Phases 4-5.
type SVGResource interface {
	// ID returns the resource's `id` attribute (used as the registry
	// key for `url(#id)` resolution). Returns "" for an unidentified
	// resource — the registry refuses to register those because they
	// are unreachable per SVG 2 §3.1.
	ID() string

	// Kind classifies the resource for runtime dispatch and for
	// printable diagnostics during cycle reporting.
	Kind() SVGResourceKind

	// CycleState returns the resource's current cycle-detection state
	// (see SVGCycleState). Initialized to SVGCycleNeedCheck.
	CycleState() SVGCycleState

	// SetCycleState updates the resource's cycle-detection state. The
	// cycle solver flips through Need→Performing→{HasCycle,NoCycle}
	// during its DFS.
	SetCycleState(SVGCycleState)
}

// SVGResourceKind enumerates the resource kinds known to the registry.
// Mirrors Blink's LayoutSVGResourceType (core/layout/svg/layout_svg_resource_container.h).
type SVGResourceKind int

const (
	// SVGResourceKindUnknown is the zero value; never used by a
	// registered resource (every concrete type returns a specific kind).
	SVGResourceKindUnknown SVGResourceKind = iota
	SVGResourceKindLinearGradient
	SVGResourceKindRadialGradient
	SVGResourceKindPattern
	SVGResourceKindClipper
	SVGResourceKindMasker
	// SVGResourceKindFilter is reserved for Phase 7.
	SVGResourceKindFilter
	// SVGResourceKindMarker is reserved for the marker-foundation work.
	SVGResourceKindMarker
)

// String returns a printable identifier for the kind. Used in cycle
// diagnostics (the cycle solver doesn't emit logs in production but
// the kind name is the natural label).
func (k SVGResourceKind) String() string {
	switch k {
	case SVGResourceKindLinearGradient:
		return "linearGradient"
	case SVGResourceKindRadialGradient:
		return "radialGradient"
	case SVGResourceKindPattern:
		return "pattern"
	case SVGResourceKindClipper:
		return "clipPath"
	case SVGResourceKindMasker:
		return "mask"
	case SVGResourceKindFilter:
		return "filter"
	case SVGResourceKindMarker:
		return "marker"
	}
	return "unknown"
}

// SVGCycleState is the per-resource cycle-detection state machine used
// by the SVGResourcesCycleSolver DFS. Mirrors Blink's
// LayoutSVGResourceContainer::FindCycle's local enum
// (kNeedCheck/kPerformingCheck/kHasCycle/kNoCycle) — see
// core/layout/svg/layout_svg_resource_container.{h,cc}.
//
// State transitions:
//
//	NeedCheck → PerformingCheck (when DFS enters the node)
//	PerformingCheck → HasCycle  (when re-entered during its own check)
//	PerformingCheck → NoCycle   (when DFS exits without re-entry)
//	HasCycle stays HasCycle (consumers treat as `none` at paint time)
//
// Resources marked HasCycle are treated as `none` at paint time:
// `clip-path: url(#cyclic)` clips nothing; `mask: url(#cyclic)`
// applies no mask; `fill: url(#cyclic)` falls back per SVG 2 §13.1.
type SVGCycleState int

const (
	// SVGCycleNeedCheck is the initial state; a fresh DFS pass must
	// visit this resource to decide if it participates in a cycle.
	SVGCycleNeedCheck SVGCycleState = iota
	// SVGCyclePerformingCheck means the DFS is currently inside this
	// resource's subtree. Re-entering a node in PerformingCheck state
	// flags a cycle through this node.
	SVGCyclePerformingCheck
	// SVGCycleHasCycle means a cycle was detected through this
	// resource. Consumers MUST treat references to it as `none`.
	SVGCycleHasCycle
	// SVGCycleNoCycle means the DFS verified the resource's subtree
	// is acyclic. Safe to consume normally.
	SVGCycleNoCycle
)

// SVGResourceReference is a parsed `url(#id)` reference at a callsite.
// Holds the unresolved id and (after resolution) the resolved
// SVGResource pointer. Mirrors Blink's SVGResource + LocalSVGResource
// pair: a single reference object carries both the textual id and the
// resolved target.
//
// Phase 6 introduces the type but most callsites still parse + resolve
// inline at paint time (Renderer.lookupSVGResources().Lookup(id)). The
// reference type is the API surface for future caching — Blink caches
// the resolved target on the reference object and invalidates on DOM
// mutation. louis14 doesn't yet have DOM mutation so the cache is
// trivial; the type exists to keep the call-site shape Blink-compatible.
type SVGResourceReference struct {
	// ID is the fragment id (without the leading `#`). Empty for a
	// reference that couldn't be parsed.
	ID string
	// Resolved is the registry-resolved target; nil until the
	// reference is resolved against a registry. A nil Resolved with
	// non-empty ID means the id wasn't registered.
	Resolved SVGResource
}

// IsValid reports whether the reference has a non-empty id.
func (ref *SVGResourceReference) IsValid() bool {
	return ref != nil && ref.ID != ""
}

// Resolve looks up the reference's id in `reg` and stashes the
// resolved resource on the reference. Returns the resolved resource
// (nil on miss). Callers can call Resolve at paint time so the
// registry walks once per reference; subsequent paints reuse the
// cached pointer.
func (ref *SVGResourceReference) Resolve(reg *SVGResourceRegistry) SVGResource {
	if ref == nil || ref.ID == "" || reg == nil {
		return nil
	}
	if r, ok := reg.Lookup(ref.ID); ok {
		ref.Resolved = r
		return r
	}
	return nil
}
