package svg

import (
	"louis14/pkg/css"
)

// SolveResourceCycles runs the cycle-detection DFS over `reg` and
// marks each resource's CycleState as either SVGCycleHasCycle (it
// participates in a reference cycle) or SVGCycleNoCycle (verified
// acyclic). After the call, render-side consumers MUST check
// CycleState() before consuming a resolved resource — a resource
// flagged HasCycle is treated as `none` (no clip / no mask / paint
// fallback) per Blink's "cycle = drop reference" rule.
//
// Mirrors Blink's SVGResourcesCycleSolver / LayoutSVGResourceContainer::
// FindCycle (core/layout/svg/svg_resources_cycle_solver.{h,cc} +
// core/layout/svg/layout_svg_resource_container.cc).
//
// The DFS visits each resource once. For a given resource it walks the
// resource's own referenced url(#…) targets (a clipper's child shapes
// may carry `fill: url(#x)`, a mask's children may carry `mask: url(#m)`
// or `clip-path: url(#c)`, a pattern's children may carry the full
// suite). Re-entering a resource that is currently in PerformingCheck
// state flags a cycle through that node — all nodes in the
// PerformingCheck frame are then marked HasCycle.
//
// Safe to call on a nil or empty registry (returns immediately).
func SolveResourceCycles(reg *SVGResourceRegistry) {
	if reg == nil || reg.Count() == 0 {
		return
	}
	// Reset all states to NeedCheck — the solver may run multiple
	// times per registry across paint frames (registry merging in
	// pkg/render builds a fresh one per frame, so reset is mostly
	// defensive against repeated calls on the same registry).
	reg.ForEach(func(res SVGResource) {
		res.SetCycleState(SVGCycleNeedCheck)
	})
	for _, res := range orderedResources(reg) {
		if res.CycleState() == SVGCycleNeedCheck {
			findCycleDFS(reg, res)
		}
	}
}

// orderedResources returns a snapshot of the registry's resources.
// Map iteration order is unspecified, but the cycle solver doesn't
// care about visit order — the result is the same set of marked nodes
// regardless. Returning a slice avoids mutating the registry's map
// during the DFS (which adds no entries, but a fresh slice is
// stylistically cleaner than iterating the map twice).
func orderedResources(reg *SVGResourceRegistry) []SVGResource {
	out := make([]SVGResource, 0, reg.Count())
	reg.ForEach(func(res SVGResource) {
		out = append(out, res)
	})
	return out
}

// findCycleDFS implements the recursive DFS step from Blink's
// LayoutSVGResourceContainer::FindCycle.
//
// Algorithm:
//  1. If the node is already SVGCycleHasCycle or SVGCycleNoCycle, it's
//     terminal — propagate to the caller.
//  2. If the node is SVGCyclePerformingCheck, the DFS has re-entered
//     it — a cycle. Mark and return.
//  3. Otherwise mark PerformingCheck, walk all outgoing references,
//     and finalize as either HasCycle (if any outgoing reference
//     re-entered) or NoCycle.
func findCycleDFS(reg *SVGResourceRegistry, res SVGResource) bool {
	switch res.CycleState() {
	case SVGCycleHasCycle:
		return true
	case SVGCycleNoCycle:
		return false
	case SVGCyclePerformingCheck:
		// DFS re-entered the same node along its own descendants —
		// a cycle. Flag this node now; the unwinding stack will pick
		// up the others.
		res.SetCycleState(SVGCycleHasCycle)
		return true
	}
	// NeedCheck → PerformingCheck → walk outgoing.
	res.SetCycleState(SVGCyclePerformingCheck)
	hasCycle := false
	for _, refID := range outgoingReferences(res) {
		if refID == "" {
			continue
		}
		target, ok := reg.Lookup(refID)
		if !ok || target == nil {
			continue
		}
		if findCycleDFS(reg, target) {
			hasCycle = true
		}
	}
	// The PerformingCheck state may have flipped to HasCycle during a
	// re-entry (case 2 above) — preserve that.
	if res.CycleState() == SVGCycleHasCycle {
		return true
	}
	if hasCycle {
		res.SetCycleState(SVGCycleHasCycle)
		return true
	}
	res.SetCycleState(SVGCycleNoCycle)
	return false
}

// outgoingReferences returns the set of `url(#id)` references this
// resource carries — either on the resource element itself
// (clip-path / mask / filter on the resource's own style) or on any
// descendant node's style (a `<rect fill="url(#g)"/>` inside a
// `<mask>` adds a reference from the mask to the gradient `g`).
//
// Mirrors what Blink's LayoutSVGResourceContainer::FindCycle does
// implicitly by traversing children + own style refs.
//
// Phase 6 covers the four reference kinds that exist today:
//   - fill / stroke → paint server
//   - clip-path     → clipper
//   - mask / mask-image → masker
//   - filter        → filter (Phase 7, but the parse path is here)
func outgoingReferences(res SVGResource) []string {
	if res == nil {
		return nil
	}
	var refs []string
	switch r := res.(type) {
	case *SVGResourceClipper:
		// Clipper's own style (clip-path on the <clipPath> element).
		refs = appendStyleRefs(refs, r.elementStyle)
		for _, child := range r.Children {
			refs = appendNodeRefs(refs, child)
		}
	case *SVGResourceMasker:
		refs = appendStyleRefs(refs, r.elementStyle)
		for _, child := range r.Children {
			refs = appendNodeRefs(refs, child)
		}
	case *SVGPattern:
		refs = appendStyleRefs(refs, r.elementStyle)
		for _, child := range r.Children {
			refs = appendNodeRefs(refs, child)
		}
	case *SVGLinearGradient, *SVGRadialGradient:
		// Gradients with `xlink:href` to a template gradient form a
		// chain that can technically cycle. Template hrefs aren't yet
		// implemented in louis14 (Phase 4 only resolves a gradient's
		// own stops), so no outgoing references to walk.
		return nil
	case *SVGResourceFilter:
		// Filter's own style may carry `filter:` (chained filters via
		// the CSS-style `filter` property, equivalent to Blink's
		// FilterChain). The filter primitive children's `in`/`in2`
		// attributes are NOT url() references (they point at builtins
		// like SourceGraphic or at named-result strings); they don't
		// create cross-resource edges. Phase 7 walks the filter
		// element's own style refs only.
		refs = appendStyleRefs(refs, r.elementStyle)
		return refs
	}
	return refs
}

// appendStyleRefs scans a single computed Style for `url(#id)` values
// in fill / stroke / clip-path / mask / mask-image / filter and
// appends each parsed id to `refs`. nil style is a no-op.
func appendStyleRefs(refs []string, style *css.Style) []string {
	if style == nil {
		return refs
	}
	// fill / stroke: paint properties whose Server kind carries
	// ServerID directly.
	if val, ok := style.Get("fill"); ok {
		if id, ok := css.ParseURLReference(val); ok {
			refs = append(refs, id)
		}
	}
	if val, ok := style.Get("stroke"); ok {
		if id, ok := css.ParseURLReference(val); ok {
			refs = append(refs, id)
		}
	}
	if val, ok := style.Get("clip-path"); ok {
		if id, ok := css.ParseURLReference(val); ok {
			refs = append(refs, id)
		}
	}
	if val, ok := style.Get("mask"); ok {
		if id, ok := css.ParseURLReference(val); ok {
			refs = append(refs, id)
		}
	}
	if val, ok := style.Get("mask-image"); ok {
		if id, ok := css.ParseURLReference(val); ok {
			refs = append(refs, id)
		}
	}
	if val, ok := style.Get("filter"); ok {
		if id, ok := css.ParseURLReference(val); ok {
			refs = append(refs, id)
		}
	}
	return refs
}

// appendNodeRefs walks an SVGNode subtree and accumulates references
// from each descendant's style. Container types recurse into their
// Children; leaf types (SVGShape) contribute their own style. Resource
// elements (clipper/masker/pattern/gradient) don't appear as descendants
// of other resources via Children — they live in the registry — so
// the recursion terminates at the existing svg-tree boundary.
func appendNodeRefs(refs []string, node SVGNode) []string {
	if node == nil {
		return refs
	}
	switch n := node.(type) {
	case *SVGShape:
		refs = appendStyleRefs(refs, n.Style)
	case *SVGContainer:
		refs = appendStyleRefs(refs, n.Style)
		for _, child := range n.Children {
			refs = appendNodeRefs(refs, child)
		}
	case *SVGViewportContainer:
		refs = appendStyleRefs(refs, n.Style)
		for _, child := range n.Children {
			refs = appendNodeRefs(refs, child)
		}
	}
	return refs
}
