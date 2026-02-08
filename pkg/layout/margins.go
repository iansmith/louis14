package layout

import (
	"louis14/pkg/css"
)

// collapseMargins returns the collapsed margin value for two adjoining vertical margins.
// Per CSS 2.1: both positive => max, both negative => most negative, mixed => sum.
func collapseMargins(margin1, margin2 float64) float64 {
	if margin1 >= 0 && margin2 >= 0 {
		if margin1 > margin2 {
			return margin1
		}
		return margin2
	}
	if margin1 < 0 && margin2 < 0 {
		if margin1 < margin2 {
			return margin1
		}
		return margin2
	}
	// Mixed: one positive, one negative
	return margin1 + margin2
}

// isCollapseThrough returns true if a box's top and bottom margins collapse through it.
// This happens when: height is 0, no border-top/bottom, no padding-top/bottom, no in-flow content,
// and the box participates in normal margin collapsing.
func isCollapseThrough(box *Box) bool {
	if !shouldCollapseMargins(box) {
		return false
	}
	if box.Border.Top > 0 || box.Border.Bottom > 0 {
		return false
	}
	if box.Padding.Top > 0 || box.Padding.Bottom > 0 {
		return false
	}
	if box.Height > 0 {
		return false
	}
	// Check for in-flow content that would prevent collapse-through
	for _, child := range box.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			continue
		}
		if child.Style != nil && child.Style.GetFloat() != css.FloatNone {
			continue
		}
		if !isCollapseThrough(child) {
			return false
		}
	}
	return true
}

// getCollapseThroughMargin collects all margins from a collapse-through element
// (its own top/bottom plus recursively from collapse-through children)
// and returns the single collapsed result.
func getCollapseThroughMargin(box *Box) float64 {
	margins := []float64{box.Margin.Top, box.Margin.Bottom}
	for _, child := range box.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			continue
		}
		if child.Style != nil && child.Style.GetFloat() != css.FloatNone {
			continue
		}
		if isCollapseThrough(child) {
			margins = append(margins, child.Margin.Top, child.Margin.Bottom)
		}
	}
	// Collapse all: max of positives + min of negatives
	var maxPos, minNeg float64
	for _, m := range margins {
		if m > maxPos {
			maxPos = m
		}
		if m < minNeg {
			minNeg = m
		}
	}
	return maxPos + minNeg
}

// collectCollapseThroughChildMargins adds margins from collapse-through children to the list.
func collectCollapseThroughChildMargins(box *Box, margins *[]float64) {
	for _, child := range box.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			continue
		}
		if child.Style != nil && child.Style.GetFloat() != css.FloatNone {
			continue
		}
		if isCollapseThrough(child) {
			*margins = append(*margins, child.Margin.Top, child.Margin.Bottom)
			collectCollapseThroughChildMargins(child, margins)
		}
	}
}

// shouldCollapseMargins returns true if the box participates in normal margin collapsing.
// Floated, absolutely/fixed positioned, inline-block, and overflow!=visible elements do not collapse.
func shouldCollapseMargins(box *Box) bool {
	if box.Style == nil {
		return true
	}
	// CRITICAL FIX: In standards mode, <body> elements never participate in margin collapsing
	// They are considered "magical" per CSS spec and quirks mode documentation
	// See: https://developer.mozilla.org/en-US/docs/Mozilla/Mozilla_quirks_mode_behavior
	if box.Node != nil && box.Node.TagName == "body" {
		return false
	}
	floatType := box.Style.GetFloat()
	if floatType != css.FloatNone {
		return false
	}
	if box.Position == css.PositionAbsolute || box.Position == css.PositionFixed {
		return false
	}
	display := box.Style.GetDisplay()
	if display == css.DisplayInlineBlock || display == css.DisplayInline {
		return false
	}
	if display == css.DisplayFlex || display == css.DisplayInlineFlex {
		return false
	}
	overflow := box.Style.GetOverflow()
	if overflow != css.OverflowVisible {
		return false
	}
	return true
}

// parentCanCollapseTopMargin returns true if the parent has no border-top or padding-top
// separating it from its first child's top margin.
func parentCanCollapseTopMargin(parent *Box) bool {
	if parent.Border.Top > 0 || parent.Padding.Top > 0 {
		return false
	}
	if parent.Style != nil {
		overflow := parent.Style.GetOverflow()
		if overflow != css.OverflowVisible {
			return false
		}
		display := parent.Style.GetDisplay()
		if display == css.DisplayInlineBlock || display == css.DisplayFlex || display == css.DisplayInlineFlex {
			return false
		}
		floatType := parent.Style.GetFloat()
		if floatType != css.FloatNone {
			return false
		}
	}
	return true
}

// parentCanCollapseBottomMargin returns true if the parent has no border-bottom or padding-bottom
// separating it from its last child's bottom margin.
func parentCanCollapseBottomMargin(parent *Box) bool {
	if parent.Border.Bottom > 0 || parent.Padding.Bottom > 0 {
		return false
	}
	if parent.Style != nil {
		overflow := parent.Style.GetOverflow()
		if overflow != css.OverflowVisible {
			return false
		}
		display := parent.Style.GetDisplay()
		if display == css.DisplayInlineBlock || display == css.DisplayFlex || display == css.DisplayInlineFlex {
			return false
		}
		floatType := parent.Style.GetFloat()
		if floatType != css.FloatNone {
			return false
		}
	}
	return true
}
