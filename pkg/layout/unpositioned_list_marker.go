package layout

// unpositioned_list_marker.go mirrors Blink's
// core/layout/list/unpositioned_list_marker.{h,cc}.
//
// UnpositionedListMarker is a value type carrying a reference to an OUTSIDE
// list marker box (Blink's LayoutOutsideListMarker) that has been laid out for
// size but not yet placed in the fragment tree. The marker propagates up the
// layout result / fragment builder — analogous to out-of-flow propagation —
// until the nearest list-item block layout algorithm (or an intervening
// multicol ColumnLayoutAlgorithm) *claims* it and calls AddToBox against the
// content's first baseline. See core/layout/list/README.md: the deferred
// positioning is the central design choice.

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/text"
)

// UnpositionedListMarker is a value type that carries a reference to a list
// marker node that has not yet been placed in the fragment tree.
//
// Mirrors Blink's UnpositionedListMarker
// (core/layout/list/unpositioned_list_marker.h). The marker_layout_object_ is
// modelled here as MarkerNode (the ::marker pseudo-element LayoutInputNode).
type UnpositionedListMarker struct {
	// MarkerNode is the LayoutInputNode for the ::marker pseudo-element.
	// Nil when the marker was not found (IsValid returns false).
	MarkerNode *LayoutInputNode

	// Item is the originating list-item node. ListMarker::InlineMarginsForOutside
	// reads list-style-type / list-style-image off the ORIGINATING item, never
	// off the ::marker (CSS Pseudo-4 §4.4 / Blink unpositioned_list_marker.cc:31).
	Item *LayoutInputNode

	// layoutResult caches the marker box's layout result so Layout is performed
	// at most once per claim cycle. Mirrors Blink's marker_layout_result reuse
	// between PositionOrPropagateListMarker and AddToBox.
	layoutResult *LayoutResult
}

// IsValid returns true when the marker node is non-nil — i.e., there is a
// marker to be placed. Mirrors Blink's operator bool().
func (m *UnpositionedListMarker) IsValid() bool {
	return m != nil && m.MarkerNode != nil
}

// markerStyle returns the marker box's computed style, or nil.
func (m *UnpositionedListMarker) markerStyle() *css.Style {
	if m == nil || m.MarkerNode == nil {
		return nil
	}
	return m.MarkerNode.style
}

// itemStyle returns the originating list-item's computed style, or nil.
func (m *UnpositionedListMarker) itemStyle() *css.Style {
	if m == nil || m.Item == nil {
		return nil
	}
	return m.Item.style
}

// markerFontAscent resolves the integer font ascent (in px) of the marker
// box's font. Blink reads font_data->GetFontMetrics().Ascent() off the marker
// style; ListMarker::InlineMarginsForOutside / WidthOfSymbol need it for the
// predefined-symbol margin formulas.
func (m *UnpositionedListMarker) markerFontAscent(ctx *LayoutContext) int {
	style := m.markerStyle()
	if style == nil || ctx == nil {
		return 0
	}
	fontPath := resolveFontPath(style, ctx.FontConfig)
	return int(text.FontAscentFromFont(style.GetFontSize(), fontPath, ctx.FontConfig.Registry))
}

// Layout lays the marker box out for size and first-line baseline, caching the
// result. Mirrors Blink's UnpositionedListMarker::Layout
// (unpositioned_list_marker.cc:39-53): Blink calls
// marker_node.LayoutAtomicInline(...) to get the marker's *first-line*
// baseline rather than the atomic-inline baseline.
//
// In louis14 the OUTSIDE marker box carries display:inline-block (set in
// createMarkerPseudoElement) so layoutElement routes it through
// NewBlockLayoutAlgorithm with shrink-to-fit inline sizing and a new
// formatting context — the LayoutBlockFlow-equivalent of
// LayoutOutsideListMarker. block_layout.go sets LayoutResult.Baseline to the
// box's first line-box baseline, which is exactly the first-line baseline
// Blink's LayoutAtomicInline requests.
//
// parentSpace is the list item's constraint space; the marker is given the
// list item's content inline-size as available space so a pathologically wide
// marker can still wrap, while a normal marker shrink-to-fits well within it.
func (m *UnpositionedListMarker) Layout(ctx *LayoutContext, parentSpace ConstraintSpace) *LayoutResult {
	if !m.IsValid() {
		return nil
	}
	if m.layoutResult != nil {
		return m.layoutResult
	}
	markerWDM := NewWritingDirectionMode(m.MarkerNode.style)
	availInline := parentSpace.AvailableSize.InlineSize.Float64()
	if availInline < 0 {
		availInline = 0
	}
	csb := NewConstraintSpaceBuilder(parentSpace.WritingDirection, markerWDM, true).
		SetAvailableSize(LogicalSize{
			InlineSize: availInline,
			BlockSize:  Indefinite,
		}).
		SetPercentageResolutionInlineSize(availInline)
	m.layoutResult = layoutElement(ctx, m.MarkerNode, csb.Build())
	return m.layoutResult
}

// LayoutResult returns the cached marker layout result (nil before Layout).
func (m *UnpositionedListMarker) LayoutResult() *LayoutResult {
	if m == nil {
		return nil
	}
	return m.layoutResult
}

// markerAscent returns the marker box's first-line baseline ascent — the
// distance from the marker fragment's block-start edge to the baseline its
// text sits on. Mirrors marker_fragment.BaselineMetrics(...).ascent in
// Blink's AddToBox. The marker box is anonymous (no border/padding), so
// LayoutResult.Baseline is the first-line baseline.
func (m *UnpositionedListMarker) markerAscent(result *LayoutResult) float64 {
	if result == nil {
		return 0
	}
	if result.HasBaseline {
		return result.Baseline
	}
	// No baseline (e.g. empty marker): align to the marker box's block-end,
	// matching Blink's FontHeight fallback for a box with no line boxes.
	return NewLogicalFragment(NewWritingDirectionMode(m.markerStyle()), result.Fragment).BlockSize()
}

// ContentAlignmentBaseline returns the baseline of the given child fragment
// that the marker should align to.
//
// Mirrors Blink's UnpositionedListMarker::ContentAlignmentBaseline
// (unpositioned_list_marker.cc:55-79): for a line-box child it returns the
// line box's ascent (nullopt for an empty line box that has a break token);
// for non-line-box content it recurses to the child's first baseline.
//
// louis14's block layout does not surface a standalone line-box fragment to
// the claimant — the first in-flow child's LayoutResult already carries the
// propagated first baseline (LayoutResult.Baseline / HasBaseline), computed
// from the child's first line box (inline children) or its first in-flow
// block descendant's baseline. That is the value Blink ends up with after the
// IsLineBox() / FirstBaseline() dispatch, so it is read directly here.
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

// AddToBox positions the marker alongside content that has a baseline and adds
// the marker fragment as a child of the list-item box fragment builder.
//
// Mirrors Blink's UnpositionedListMarker::AddToBox
// (unpositioned_list_marker.cc:81-123):
//   - the marker's inline offset is InlineOffset(markerInlineSize) — a
//     negative offset into the list item's content-box start (the marker sits
//     in the padding/margin gap left of the content);
//   - the block offset starts at blockOffset (the content child's block
//     offset) and is shifted by baseline_adjust = content_baseline -
//     marker_ascent so the marker's text baseline lines up with the content's
//     first baseline;
//   - if baseline_adjust is negative (the marker's ascent is taller than the
//     content's), the *content* is pushed down instead — returned as the
//     contentPush delta so the claimant can move the content child.
//
// markerLayoutResult is the result from Layout(); contentBaseline is from
// ContentAlignmentBaseline(); blockOffset is the content child's block offset
// within the list item's content box. Returns the amount (>= 0) by which the
// claimant must push the content child down.
func (m *UnpositionedListMarker) AddToBox(
	ctx *LayoutContext,
	markerLayoutResult *LayoutResult,
	contentBaseline float64, blockOffset float64,
	builder *BoxFragmentBuilder) float64 {
	if !m.IsValid() || markerLayoutResult == nil || markerLayoutResult.Fragment == nil {
		return 0
	}
	wdm := builder.wdm
	markerLogical := NewLogicalFragment(wdm, markerLayoutResult.Fragment)
	markerInlineSize := markerLogical.InlineSize()

	inlineOff := m.InlineOffset(markerInlineSize, ctx)
	markerOffset := LogicalOffset{
		InlineOffset: inlineOff,
		BlockOffset:  blockOffset,
	}

	// Adjust the block offset to align baselines of the marker and content.
	// Blink: baseline_adjust = content_baseline - marker_metrics.ascent.
	markerAscent := m.markerAscent(markerLayoutResult)
	baselineAdjust := contentBaseline - markerAscent
	var contentPush float64
	if baselineAdjust >= 0 {
		markerOffset.BlockOffset += baselineAdjust
	} else {
		// The marker's ascent is taller than the content's ascent: push the
		// content down instead. Blink: *block_offset -= baseline_adjust.
		contentPush = -baselineAdjust
	}

	// ComputeIntrudedFloatOffset is not ported (louis14 has no equivalent
	// LayoutOpportunity query at this seam yet). In the common case — no float
	// intruding into the marker's inline band — Blink's
	// ComputeIntrudedFloatOffset returns LayoutUnit() (zero), so omitting it is
	// exact there. Documented follow-up: port a minimal exclusion-space query
	// when an intruding-float marker test surfaces.

	builder.AddChild(markerLayoutResult.Fragment, markerOffset)
	return contentPush
}

// AddToBoxWithoutLineBoxes positions the marker when the list item produced no
// line boxes: the marker is top-aligned at block offset 0 and contributes to
// the list item's block size.
//
// Mirrors Blink's UnpositionedListMarker::AddToBoxWithoutLineBoxes
// (unpositioned_list_marker.cc:125-157). intrinsicBlockSize is updated in
// place — the marker's block size becomes a lower bound on the list item's
// intrinsic block size (3 of 4 impls include the marker in the block size,
// per the cited CSSWG issue).
func (m *UnpositionedListMarker) AddToBoxWithoutLineBoxes(
	ctx *LayoutContext,
	markerLayoutResult *LayoutResult,
	builder *BoxFragmentBuilder, intrinsicBlockSize *float64) {
	if !m.IsValid() || markerLayoutResult == nil || markerLayoutResult.Fragment == nil {
		return
	}
	wdm := builder.wdm
	markerLogical := NewLogicalFragment(wdm, markerLayoutResult.Fragment)
	markerInlineSize := markerLogical.InlineSize()
	markerBlockSize := markerLogical.BlockSize()

	offset := LogicalOffset{
		InlineOffset: m.InlineOffset(markerInlineSize, ctx),
		BlockOffset:  0,
	}
	builder.AddChild(markerLayoutResult.Fragment, offset)

	// The marker is always included in the block size when there are no line
	// boxes (Blink unpositioned_list_marker.cc:144-156).
	if intrinsicBlockSize != nil && markerBlockSize > *intrinsicBlockSize {
		*intrinsicBlockSize = markerBlockSize
	}
}

// InlineOffset returns the inline offset of the marker relative to the list
// item's content-box start. Mirrors Blink's UnpositionedListMarker::InlineOffset
// (unpositioned_list_marker.cc:28-37): it calls
// ListMarker::InlineMarginsForOutside and returns the *start* (first) margin —
// a negative offset placing the marker left of the content.
//
// ctx supplies the marker font ascent needed for the predefined-symbol margin
// formula.
func (m *UnpositionedListMarker) InlineOffset(markerInlineSize float64, ctx *LayoutContext) float64 {
	if !m.IsValid() {
		return -markerInlineSize
	}
	cat := m.MarkerNode.MarkerCategory
	ascent := m.markerFontAscent(ctx)
	start, _ := InlineMarginsForOutside(
		m.markerStyle(), m.itemStyle(), cat,
		layoutunit.FromFloat64Round(markerInlineSize), ascent)
	return start.Float64()
}
