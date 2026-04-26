package layout

// UnpositionedListMarker is a value type that carries a reference to a list
// marker node that has not yet been placed in the fragment tree.
//
// Mirrors Blink's UnpositionedListMarker (core/layout/list/unpositioned_list_marker.h).
// In Louis14 the current list-marker path is paint-time only (pkg/render/render.go
// drawListMarker). Track C seeds the layout-time protocol so that future work
// can route markers through the fragment tree. Until ListMarkerBlockNodeIfListItem
// returns a non-nil node the protocol is a no-op.
type UnpositionedListMarker struct {
	// MarkerNode is the LayoutInputNode for the ::marker pseudo-element.
	// Nil when the marker was not found (IsValid returns false).
	MarkerNode *LayoutInputNode
}

// IsValid returns true when the marker node is non-nil — i.e., there is a
// marker to be placed. Mirrors Blink's operator bool().
func (m *UnpositionedListMarker) IsValid() bool {
	return m != nil && m.MarkerNode != nil
}

// ContentAlignmentBaseline returns the baseline of the given child fragment
// that the marker should align to.
//
// For a block fragment (column): returns the fragment's first baseline if set.
// Mirrors Blink's UnpositionedListMarker::ContentAlignmentBaseline
// (unpositioned_list_marker.cc).
func (m *UnpositionedListMarker) ContentAlignmentBaseline(
	child *PhysicalFragment, childResult *LayoutResult) (float64, bool) {
	if child == nil || childResult == nil {
		return 0, false
	}
	if childResult.HasBaseline {
		return childResult.Baseline, true
	}
	return 0, false
}

// AddToBox positions the marker alongside content that has a baseline.
// Stub implementation — no-op until the layout-time marker path is enabled.
// Mirrors Blink's UnpositionedListMarker::AddToBox.
func (m *UnpositionedListMarker) AddToBox(
	child *PhysicalFragment, childResult *LayoutResult,
	contentBaseline float64, blockOffset float64,
	builder *BoxFragmentBuilder) {
	// No-op: marker placement is handled at paint time in render.go.
	// Future work: lay out the marker node and add it as a child.
}

// AddToBoxWithoutLineBoxes positions the marker when the list-item has no
// line boxes. Stub — no-op until the layout-time path is enabled.
// Mirrors Blink's UnpositionedListMarker::AddToBoxWithoutLineBoxes.
func (m *UnpositionedListMarker) AddToBoxWithoutLineBoxes(
	builder *BoxFragmentBuilder, intrinsicBlockSize *float64) {
	// No-op.
}

// Layout lays out the marker node and returns the result.
// Returns nil until the layout-time path is enabled.
// Mirrors Blink's UnpositionedListMarker::Layout.
func (m *UnpositionedListMarker) Layout(
	ctx *LayoutContext, space ConstraintSpace) *LayoutResult {
	// No-op: marker is drawn at paint time.
	return nil
}

// InlineOffset returns the inline offset of the marker relative to the
// content-box start. Mirrors Blink's UnpositionedListMarker::InlineOffset.
func (m *UnpositionedListMarker) InlineOffset(markerInlineSize float64) float64 {
	return -markerInlineSize
}
