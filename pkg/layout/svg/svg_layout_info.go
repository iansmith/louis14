// Package svg implements the SVG layout subtree mirroring Blink's
// core/layout/svg/. The SVG layout pass is not CSS box flow — it is a
// recursive geometry / transform / bounding-box update rooted at an
// SVGRoot under an inline <svg> element.
//
// See docs/plan-svg-foundation.md.
package svg

// SVGLayoutInfo flows DOWN the SVG layout pass: from the SVGRoot through
// each container to each leaf shape. Mirrors Blink's SVGLayoutInfo
// (core/layout/svg/svg_layout_info.h).
//
// Phase 1: the chassis carries these flags but every Update consumer is a
// stub that returns an empty SVGLayoutResult. Real geometry consumers land
// in Phase 2+.
type SVGLayoutInfo struct {
	// ForceLayout requests that every node re-run UpdateSVGLayout even if
	// its inputs (geometry attrs, viewport, scale) appear unchanged. Mirrors
	// Blink's force_layout. Phase 1: always true at root, propagated as-is.
	ForceLayout bool

	// ScaleChanged is set when the effective scale of the SVG content has
	// changed (e.g. an ancestor's transform scale, page zoom, or viewBox
	// fit changed). Stroke geometry and dashed-stroke patterns may depend
	// on this. Mirrors Blink's scale_factor_changed.
	ScaleChanged bool

	// ViewportChanged is set when the nearest viewport's size changed,
	// invalidating percentage-resolved <length>s within it. Mirrors Blink's
	// viewport_changed.
	ViewportChanged bool
}

// SVGLayoutResult flows UP the SVG layout pass: from each leaf back to
// the SVGRoot. Mirrors Blink's SVGLayoutResult
// (core/layout/svg/svg_layout_info.h).
type SVGLayoutResult struct {
	// BoundsChanged reports whether the node's ObjectBoundingBox differs
	// from its previous value, so the parent can decide to invalidate or
	// repaint. Mirrors Blink's bounds_changed.
	BoundsChanged bool

	// HasViewportDependence reports whether any descendant resolved a
	// percentage <length> against the viewport — i.e. a viewport-size
	// change would require relayout below this point. Mirrors Blink's
	// has_viewport_dependence.
	HasViewportDependence bool
}
