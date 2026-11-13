package layout

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
)

// AxisEdge is a grid item's alignment edge in one axis. Mirrors Blink's
// AxisEdge (core/layout/grid/grid_item.h:17 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type AxisEdge uint8

const (
	AxisEdgeStart AxisEdge = iota
	AxisEdgeCenter
	AxisEdgeEnd
	AxisEdgeFirstBaseline
	AxisEdgeLastBaseline
)

// gridItemAxisData is a grid item's alignment in one axis (columns: the
// grid's inline axis; rows: its block axis): Blink GridItemData's
// {column,row}_alignment, _auto_behavior, is_overflow_safe_for_,
// _baseline_writing_mode and _baseline_group (grid_item.h:278-301 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type gridItemAxisData struct {
	alignment           AxisEdge
	autoBehavior        AutoSizeBehavior
	isOverflowSafe      bool
	baselineWritingMode WritingMode
	baselineGroup       BaselineGroup
	// hasOwnBaselines: the baseline writing mode is the item's own, so its
	// fragment's baselines can be read (LogicalBoxFragment::
	// IsWritingModeEqual).
	hasOwnBaselines bool
	// spansIntrinsicTrack / spansFlexibleTrack: the item spans a track
	// with an intrinsic (flexible) sizing function in this axis
	// (GridItemData::IsSpanningIntrinsicTrack / IsSpanningFlexibleTrack),
	// set when the axis's sizing items are built.
	spansIntrinsicTrack, spansFlexibleTrack bool
	// fallback is the alignment a baseline-aligned item falls back to when
	// it cannot take part in baseline alignment (hasFallback;
	// {column,row}_fallback_alignment).
	fallback    AxisEdge
	hasFallback bool
}

// newGridItemAxisData resolves an item's alignment in the columns
// (forColumns) or rows as GridItemData's constructor does
// (grid_item.cc:150-260 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// louis14 has no subgrids, so the parent and root grids are the container.
func newGridItemAxisData(forColumns bool, container *css.Style, containerWDM WritingDirectionMode, item *css.Style, itemWDM WritingDirectionMode, isReplaced bool) gridItemAxisData {
	var d gridItemAxisData
	d.alignment, d.autoBehavior, d.isOverflowSafe = axisEdgeFromItemPosition(forColumns, isReplaced, container, containerWDM, item, itemWDM)
	// The rows are a parallel alignment context, the columns are not
	// (grid_item.cc:170-176).
	d.baselineWritingMode = DetermineBaselineWritingMode(containerWDM, itemWDM.WM, !forColumns)
	d.hasOwnBaselines = d.baselineWritingMode == itemWDM.WM
	d.baselineGroup = DetermineBaselineGroup(containerWDM, d.baselineWritingMode, !forColumns,
		d.alignment == AxisEdgeLastBaseline, false)
	return d
}

// stretches reports whether the item's size in this axis stretches when it
// is auto (any auto behaviour but fit-content).
func (d gridItemAxisData) stretches() bool {
	return d.autoBehavior != AutoSizeBehaviorFitContent
}

// axisEdgeFromItemPosition is an in-flow item's alignment edge in the
// columns (justify-self) or rows (align-self), the auto behaviour of its
// size there, and whether its alignment is overflow-safe. Auto margins take
// precedence and are always safe; `stretch` stretches explicitly; `normal`
// stretches implicitly unless the item is replaced; everything else is
// fit-content. Mirrors Blink's AxisEdgeFromItemPosition
// (grid/grid_item.cc:18-147 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// louis14 has no subgrids or grid-lanes, so those branches are omitted.
func axisEdgeFromItemPosition(forColumns, isReplaced bool, container *css.Style, containerWDM WritingDirectionMode, item *css.Style, itemWDM WritingDirectionMode) (edge AxisEdge, autoBehavior AutoSizeBehavior, isOverflowSafe bool) {
	position, safe := resolvedSelfAlignment(forColumns, container, item)
	autoBehavior, isOverflowSafe = AutoSizeBehaviorFitContent, safe

	// Auto margins take precedence over any alignment property
	// (grid_item.cc:66-86).
	autoInlineStart, autoInlineEnd, autoBlockStart, autoBlockEnd := PhysicalAutoMarginsToLogical(item.GetMargin(), containerWDM)
	startAuto, endAuto := autoBlockStart, autoBlockEnd
	if forColumns {
		startAuto, endAuto = autoInlineStart, autoInlineEnd
	}
	switch {
	case startAuto && endAuto:
		return AxisEdgeCenter, autoBehavior, true
	case startAuto:
		return AxisEdgeEnd, autoBehavior, true
	case endAuto:
		return AxisEdgeStart, autoBehavior, true
	}

	switch position {
	case "self-start", "self-end":
		edge = selfAxisEdge(forColumns, position == "self-end", itemWDM, containerWDM)
	case "anchor-center", "center":
		edge = AxisEdgeCenter
	case "flex-end", "end":
		edge = AxisEdgeEnd
	case "stretch":
		autoBehavior = AutoSizeBehaviorStretchExplicit
	case "baseline", "first baseline":
		edge = AxisEdgeFirstBaseline
	case "last baseline":
		edge = AxisEdgeLastBaseline
	case "left", "right":
		// Only justify-self takes left/right (the columns).
		if (position == "left") != containerWDM.IsLTR() {
			edge = AxisEdgeEnd
		}
	case "normal":
		if !isReplaced {
			autoBehavior = AutoSizeBehaviorStretchImplicit
		}
	}
	return edge, autoBehavior, isOverflowSafe
}

// selfAxisEdge is self-start's (or self-end's) edge in the container's
// inline axis (a grid's columns, a column flex's cross axis) or block axis
// (a grid's rows, a row flex's cross axis): the item's own start (end)
// edge, mapped through its writing direction onto the container's. Grid,
// flex and out-of-flow static-position edges and inset alignment
// (GetAlignmentInsetBias) share it. Mirrors the LogicalToPhysical / PhysicalToLogical
// pair in AxisEdgeFromItemPosition (grid/grid_item.cc:90-111) and the
// LogicalToLogical in FlexLayoutAlgorithm::ResolvedAlignSelf
// (flex/flex_layout_algorithm.cc:317-326 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func selfAxisEdge(inlineAxis, isEnd bool, itemWDM, containerWDM WritingDirectionMode) AxisEdge {
	// 1 marks the item's start edges.
	logical := LogicalToLogical(LogicalEdges{InlineStart: 1, BlockStart: 1}, itemWDM, containerWDM)
	startIsItemStart := logical.BlockStart == 1
	if inlineAxis {
		startIsItemStart = logical.InlineStart == 1
	}
	if startIsItemStart != isEnd {
		return AxisEdgeStart
	}
	return AxisEdgeEnd
}

// resolvedSelfAlignment is an item's justify-self (columns) or align-self
// (rows) position keyword, `auto` resolved to the container's
// justify-items / align-items (a `legacy` keyword dropped), and whether it
// is `safe`. Mirrors ComputedStyle::ResolvedJustifySelf / ResolvedAlignSelf
// with a `normal` default, as AxisEdgeFromItemPosition calls them (a nil
// container, as for BlockStaticPositionEdge, leaves `auto` as `normal`)
// (grid_item.cc:40-46 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func resolvedSelfAlignment(forColumns bool, container, item *css.Style) (position string, safe bool) {
	self, items := "align-self", "align-items"
	if forColumns {
		self, items = "justify-self", "justify-items"
	}
	v, _ := item.Get(self)
	if v = strings.TrimSpace(v); (v == "" || v == "auto") && container != nil {
		v, _ = container.Get(items)
		var words []string
		for _, w := range strings.Fields(v) {
			if w != "legacy" {
				words = append(words, w)
			}
		}
		v = strings.Join(words, " ")
	}
	safe = hasOverflowSafe(v)
	if v = stripOverflowKeyword(v); v == "" || v == "auto" {
		v = "normal"
	}
	return v, safe
}

// AlignmentOffset is an item's offset in its grid area along one axis: its
// start margin, plus the free space for end (half of it for center), with
// the free space clamped at 0 when the alignment is overflow-safe; or its
// baseline offset. Mirrors Blink's AlignmentOffset (layout_utils.cc:640-664
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), in LayoutUnits.
func AlignmentOffset(containerSize, size, marginStart, marginEnd, baselineOffset float64, edge AxisEdge, isOverflowSafe bool) float64 {
	start := layoutunit.FromFloat64Trunc(marginStart)
	free := layoutunit.FromFloat64Trunc(containerSize).Sub(layoutunit.FromFloat64Trunc(size)).
		Sub(start).Sub(layoutunit.FromFloat64Trunc(marginEnd))
	if isOverflowSafe {
		free = layoutunit.Max(free, layoutunit.LayoutUnit{})
	}
	switch edge {
	case AxisEdgeCenter:
		return start.Add(free.DivInt(2)).Float64()
	case AxisEdgeEnd:
		return start.Add(free).Float64()
	case AxisEdgeFirstBaseline, AxisEdgeLastBaseline:
		return baselineOffset
	}
	return start.Float64()
}

// alignmentEdge is the item's alignment in this axis, its fallback when it
// has one. Mirrors GridItemData::Alignment (grid_item.h:55-59 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (d gridItemAxisData) alignmentEdge() AxisEdge {
	if d.hasFallback {
		return d.fallback
	}
	return d.alignment
}

// overflowSafe reports whether the item's alignment is overflow-safe: a
// fallback always is (GridItemData::IsOverflowSafe, grid_item.h:60-64).
func (d gridItemAxisData) overflowSafe() bool {
	return d.hasFallback || d.isOverflowSafe
}

// isBaselineSpecified reports whether the item specifies baseline alignment
// in this axis (GridItemData::IsBaselineSpecified, grid_item.h:73-78).
func (d gridItemAxisData) isBaselineSpecified() bool {
	return d.alignment == AxisEdgeFirstBaseline || d.alignment == AxisEdgeLastBaseline
}

// isBaselineAligned reports whether the item aligns to a shared baseline in
// this axis: it specifies baseline alignment and has not fallen back.
// Mirrors GridItemData::IsBaselineAligned (grid_item.h:67-71).
func (d gridItemAxisData) isBaselineAligned() bool {
	e := d.alignmentEdge()
	return e == AxisEdgeFirstBaseline || e == AxisEdgeLastBaseline
}

// setAlignmentFallback decides whether a baseline-specified item can take
// part in baseline alignment: not when it has only a synthesized baseline,
// spans an intrinsic or flexible track, and its size in the baseline axis
// (or its min or max) is a percentage or stretch, since its size then
// depends on the track it sizes. It then falls back to start (the major
// group) or end (the minor group). Mirrors GridItemData::
// SetAlignmentFallback (grid_item.cc:262-322 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (item *gridItem) setAlignmentFallback(forColumns, hasSynthesizedBaseline bool) {
	d := item.axisPtr(forColumns)
	if !d.isBaselineSpecified() {
		return
	}
	d.hasFallback = false
	if !hasSynthesizedBaseline || (!d.spansIntrinsicTrack && !d.spansFlexibleTrack) {
		return
	}
	itemWDM := item.itemWDM
	// The baseline axis is the item's block axis when it is parallel to
	// the grid and the axis is the rows, or orthogonal and the columns.
	prop := logicalInlineSizeProperty(itemWDM)
	if item.wdm.IsOrthogonalTo(itemWDM) == forColumns {
		prop = logicalBlockSizeProperty(itemWDM)
	}
	// Length::HasPercentOrStretch: the parser maps stretch keywords to
	// 100%, so a percentage test covers both.
	if gridItemHasPercentageSize(item.style, prop) {
		d.fallback, d.hasFallback = AxisEdgeStart, true
		if d.baselineGroup == BaselineGroupMinor {
			d.fallback = AxisEdgeEnd
		}
	}
}

// setTrackSpanProperties records, for each item, whether it spans an
// intrinsic or flexible track of sets in this axis. Mirrors
// GridItemData::SetTrackSpanProperties' use in the sizing collections.
func setTrackSpanProperties(items []*gridItem, sets []gridSet, forColumns bool) {
	a := &gridTrackSizingAlgorithm{sets: sets}
	for _, item := range items {
		start, end := item.span(forColumns)
		d := item.axisPtr(forColumns)
		d.spansIntrinsicTrack, d.spansFlexibleTrack = false, false
		if it := (&gridSizingItem{start: start, end: min(end, len(sets))}); it.start >= 0 && it.end > it.start {
			d.spansIntrinsicTrack, d.spansFlexibleTrack = a.spansIntrinsic(it), a.spansFlexible(it)
		}
	}
}

// span is the item's [start, end) tracks in the columns (forColumns) or
// rows.
func (item *gridItem) span(forColumns bool) (start, end int) {
	if forColumns {
		return item.colStart, item.colEnd
	}
	return item.rowStart, item.rowEnd
}

// axisPtr is axis, for updating.
func (item *gridItem) axisPtr(forColumns bool) *gridItemAxisData {
	if forColumns {
		return &item.columns
	}
	return &item.rows
}

// axis is the item's alignment data in the columns (forColumns) or rows.
func (item *gridItem) axis(forColumns bool) gridItemAxisData {
	if forColumns {
		return item.columns
	}
	return item.rows
}
