package render

import (
	"louis14/pkg/layout"
)

// Gap-decorations painting: column rules drawn from the multicol fragment's
// GapGeometry, from intersection to intersection. Port of Blink's
// BoxFragmentPainter::PaintGapDecorations (box_fragment_painter.cc:1569-1644
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and GapDecorationsPainter::Paint
// (gap_decorations_painter.cc:211-478 @ a9f50e522efa). CSSGapDecoration is
// "stable" at that SHA (runtime_enabled_features.json5:1673-1676), so this is
// Blink's live column-rule path; the legacy BoxFragmentPainter::
// PaintColumnRules returns early (box_fragment_painter.cc:1777-1781).
//
// Scope, from the CSS louis14 supports:
//   - Only kForColumns is painted. Blink also paints kForRows, but the
//     row-rule-* properties are not implemented (row-rule-style computes to
//     none, so DrawBoxSide paints nothing there).
//   - column-rule-break and column-rule-visibility-items are not
//     implemented; their initial value `normal` resolves for multicol to
//     kIntersection and kBetween (css_gap_decoration_property_utils.cc:190-229
//     @ a9f50e522efa).
//   - column-rule-inset-* are not implemented; their initial value is 0
//     (css_properties.json5:7138-7219 @ a9f50e522efa), so every inset is 0
//     and IsCapIntersection / GetMaxInsetWidth / overlap-join have no
//     effect. A single rule color/style/width (no gap data lists), and no
//     row-gap fragmentation stitching (HasRowGapFragmentation is false for
//     multicol, gap_geometry.cc:18-32).

// ruleBreak mirrors Blink's RuleBreak.
type ruleBreak int

const (
	ruleBreakNone ruleBreak = iota
	ruleBreakNormal
	ruleBreakIntersection
)

// ruleVisibilityItems mirrors Blink's RuleVisibilityItems.
type ruleVisibilityItems int

const (
	ruleVisibilityAll ruleVisibilityItems = iota
	ruleVisibilityAround
	ruleVisibilityBetween
)

// resolveRuleBreakValue mirrors CSSGapDecorationUtils::ResolveRuleBreakValue
// (css_gap_decoration_property_utils.cc:190-206) for the computed value
// `normal` (louis14 does not parse column-rule-break / row-rule-break).
func resolveRuleBreakValue(direction layout.GapDirection, containerType layout.GapContainerType) ruleBreak {
	if containerType == layout.GapContainerMulticol {
		if direction == layout.GapForColumns {
			return ruleBreakIntersection
		}
		return ruleBreakNone
	}
	return ruleBreakNormal
}

// resolveRuleVisibilityItemsValue mirrors
// CSSGapDecorationUtils::ResolveRuleVisibilityItemsValue (:208-229) for the
// computed value `normal`.
func resolveRuleVisibilityItemsValue(containerType layout.GapContainerType) ruleVisibilityItems {
	if containerType == layout.GapContainerGrid {
		return ruleVisibilityAll
	}
	return ruleVisibilityBetween
}

// isRuleSegmentVisible mirrors CSSGapDecorationUtils::IsRuleSegmentVisible
// (:231-256).
func isRuleSegmentVisible(state layout.GapSegmentState, visibility ruleVisibilityItems) bool {
	switch visibility {
	case ruleVisibilityAround:
		return !state.IsEmpty()
	case ruleVisibilityBetween:
		return !state.HasEmptyStatus()
	}
	return true
}

// shouldMoveIntersectionStartForward mirrors the anonymous-namespace helper
// of the same name (gap_decorations_painter.cc:30-73), without the flex
// overlap-window arm.
func shouldMoveIntersectionStartForward(start int, rb ruleBreak, visibility ruleVisibilityItems, intersections []layout.GapIntersection) bool {
	visible := isRuleSegmentVisible(intersections[start].SegmentState, visibility)
	if rb == ruleBreakNone {
		return !visible
	}
	return layout.BlockedStatusFromGapStates(intersections, start).HasBlockedStatus(layout.BlockedAfter) || !visible
}

// shouldMoveIntersectionEndForward mirrors the helper of the same name
// (gap_decorations_painter.cc:75-162), without the flex arm. Under
// kIntersection a multicol rule breaks at every intersection; only grid
// continues across a cross intersection flanked by spanners (:146-161), and
// louis14 builds no grid gap geometry.
func shouldMoveIntersectionEndForward(end int, rb ruleBreak, visibility ruleVisibilityItems, intersections []layout.GapIntersection) bool {
	if !isRuleSegmentVisible(intersections[end].SegmentState, visibility) {
		return false
	}
	switch rb {
	case ruleBreakNone:
		return true
	case ruleBreakNormal:
		return !layout.BlockedStatusFromGapStates(intersections, end).HasBlockedStatus(layout.BlockedAfter)
	}
	return false
}

// adjustIntersectionIndexPair mirrors AdjustIntersectionIndexPair
// (gap_decorations_painter.cc:164-205): advance start to the first paintable
// segment, then end as far as the segment may extend. Returns end == start
// when nothing is left to paint.
func adjustIntersectionIndexPair(start int, rb ruleBreak, visibility ruleVisibilityItems, intersections []layout.GapIntersection) (int, int) {
	last := len(intersections) - 1
	for start < last && shouldMoveIntersectionStartForward(start, rb, visibility, intersections) {
		start++
	}
	if start == last {
		return start, start
	}
	end := start + 1
	for end < last && shouldMoveIntersectionEndForward(end, rb, visibility, intersections) {
		end++
	}
	return start, end
}

// paintGapDecorations paints the layer's column rules from its GapGeometry.
// Mirrors BoxFragmentPainter::PaintGapDecorations: with the default
// rule-overlap (row-over-column) columns paint first, then rows (not
// supported, see the file comment).
func (r *Renderer) paintGapDecorations(layer *PaintLayer) {
	if layer.GapGeometry == nil {
		return
	}
	r.paintGapDecorationsForDirection(layer, layout.GapForColumns, layer.GapGeometry)
}

// paintGapDecorationsForDirection mirrors GapDecorationsPainter::Paint
// (gap_decorations_painter.cc:211-478). Geometry offsets are relative to
// the content box (see layout.GapGeometry), so the WritingModeConverter runs
// over the content-box size and the result is offset by the content origin.
func (r *Renderer) paintGapDecorationsForDirection(layer *PaintLayer, direction layout.GapDirection, gg *layout.GapGeometry) {
	box := layer.Box
	// DrawBoxSide paints nothing for none/hidden or a zero thickness.
	ruleThickness := layer.ColumnRuleWidth
	if layer.ColumnRuleStyle == "none" || layer.ColumnRuleStyle == "hidden" || ruleThickness <= 0 {
		return
	}
	isMain := gg.IsMainDirection(direction)
	gapCount := len(gg.CrossGaps)
	if isMain {
		gapCount = len(gg.MainGaps)
	}
	if gapCount == 0 {
		return
	}
	r.setColor(layer.ColumnRuleColor)

	isColumnGap := direction == layout.GapForColumns
	rb := resolveRuleBreakValue(direction, gg.ContainerType)
	visibility := resolveRuleVisibilityItemsValue(gg.ContainerType)

	wdm := layout.NewWritingDirectionMode(box.Style)
	// The content-box size BoxFragmentBuilder.Build converts children over.
	contentSize := layout.PhysicalSize{
		Width:  max(0, box.Width-box.Border.Left-box.Border.Right-box.Padding.Left-box.Padding.Right),
		Height: max(0, box.Height-box.Border.Top-box.Border.Bottom-box.Padding.Top-box.Padding.Bottom),
	}
	conv := layout.NewConverter(wdm, contentSize)
	originX := box.X + box.Border.Left + box.Padding.Left
	originY := box.Y + box.Border.Top + box.Padding.Top

	var intersections []layout.GapIntersection
	gg.InitPaintState()
	for gapIndex := 0; gapIndex < gapCount; gapIndex++ {
		// Spanner main gaps only produce cross-gap intersections.
		if gg.IsMultiColSpanner(gapIndex, direction) {
			continue
		}
		center := gg.GetGapCenterOffset(direction, gapIndex)
		intersections = gg.GenerateIntersectionListForGap(direction, gapIndex, intersections)
		if len(intersections) < 2 {
			continue
		}
		last := len(intersections) - 1
		start := 0
		for start < last {
			var end int
			start, end = adjustIntersectionIndexPair(start, rb, visibility, intersections)
			if start >= end {
				break
			}
			// Insets are 0 (see the file comment); the decoration starts
			// half the crossing gap past each intersection.
			startCrossWidth := gg.GetCrossWidthForIntersection(direction, gapIndex, start, isMain, intersections)
			endCrossWidth := gg.GetCrossWidthForIntersection(direction, gapIndex, end, isMain, intersections)
			primaryStart := center - ruleThickness/2
			secondaryStart := intersections[start].Offset + startCrossWidth/2
			secondarySize := intersections[end].Offset - secondaryStart - endCrossWidth/2

			logical := layout.LogicalOffset{InlineOffset: secondaryStart, BlockOffset: primaryStart}
			logicalSize := layout.LogicalSize{InlineSize: secondarySize, BlockSize: ruleThickness}
			if isColumnGap {
				logical = layout.LogicalOffset{InlineOffset: primaryStart, BlockOffset: secondaryStart}
				logicalSize = layout.LogicalSize{InlineSize: ruleThickness, BlockSize: secondarySize}
			}
			physSize := layout.ToPhysicalSize(logicalSize, wdm.WM)
			physOff := conv.ToPhysicalOffset(logical, physSize)
			x, y, w, h := pixelSnap(originX+physOff.X, originY+physOff.Y, physSize.Width, physSize.Height)
			// BoxSideFromDirection: a column rule is drawn as the logical
			// inline-start side, physically horizontal in vertical modes.
			r.drawColumnRuleRect(layer.ColumnRuleStyle, layer.ColumnRuleColor, x, y, w, h, isColumnGap != wdm.IsHorizontal())

			start = end
		}
	}
}
