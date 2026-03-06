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

// isCollapseThrough returns true if a box's block-start and block-end margins collapse through it.
// This happens when: block size is 0, no block-start/end border or padding, no in-flow content,
// and the box participates in normal margin collapsing.
// Uses HorizontalTB direction (block-start=top, block-end=bottom).
func isCollapseThrough(box *Box) bool {
	return isCollapseThroughDir(box, NewDir(HorizontalTB))
}

// isCollapseThroughDir is the Dir-aware version of isCollapseThrough.
// It checks block-start/end border and padding using the given Dir.
func isCollapseThroughDir(box *Box, dir Dir) bool {
	if !shouldCollapseMargins(box) {
		return false
	}
	if dir.BlockStartEdge(box.Border) > 0 || dir.BlockEndEdge(box.Border) > 0 {
		return false
	}
	if dir.BlockStartEdge(box.Padding) > 0 || dir.BlockEndEdge(box.Padding) > 0 {
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
		if !isCollapseThroughDir(child, dir) {
			return false
		}
	}
	return true
}

// getCollapseThroughMargin collects all margins from a collapse-through element
// (its own block-start/end plus recursively from collapse-through children)
// and returns the single collapsed result.
func getCollapseThroughMargin(box *Box) float64 {
	return getCollapseThroughMarginDir(box, NewDir(HorizontalTB))
}

// getCollapseThroughMarginDir is the Dir-aware version of getCollapseThroughMargin.
func getCollapseThroughMarginDir(box *Box, dir Dir) float64 {
	margins := []float64{dir.BlockStartEdge(box.Margin), dir.BlockEndEdge(box.Margin)}
	for _, child := range box.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			continue
		}
		if child.Style != nil && child.Style.GetFloat() != css.FloatNone {
			continue
		}
		if isCollapseThroughDir(child, dir) {
			margins = append(margins, dir.BlockStartEdge(child.Margin), dir.BlockEndEdge(child.Margin))
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

// collectCollapseThroughChildMargins adds block-direction margins from collapse-through children to the list.
func collectCollapseThroughChildMargins(box *Box, margins *[]float64) {
	collectCollapseThroughChildMarginsDir(box, margins, NewDir(HorizontalTB))
}

// collectCollapseThroughChildMarginsDir is the Dir-aware version.
func collectCollapseThroughChildMarginsDir(box *Box, margins *[]float64, dir Dir) {
	for _, child := range box.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			continue
		}
		if child.Style != nil && child.Style.GetFloat() != css.FloatNone {
			continue
		}
		if isCollapseThroughDir(child, dir) {
			*margins = append(*margins, dir.BlockStartEdge(child.Margin), dir.BlockEndEdge(child.Margin))
			collectCollapseThroughChildMarginsDir(child, margins, dir)
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
	// Note: Flex containers' margins DO collapse with sibling margins (CSS Flexbox §3).
	// They only prevent margin collapsing between the container and its children,
	// which is handled by the fact that layoutFlex returns before parent-child
	// collapsing code is reached in layoutBlock.
	// CSS Flexbox §9: margins of flex items do not collapse
	if box.Parent != nil && box.Parent.Style != nil {
		parentDisplay := box.Parent.Style.GetDisplay()
		if parentDisplay == css.DisplayFlex || parentDisplay == css.DisplayInlineFlex {
			return false
		}
	}
	overflow := box.Style.GetOverflow()
	if overflow != css.OverflowVisible {
		return false
	}
	return true
}

// parentCanCollapseTopMargin returns true if the parent has no block-start border or padding
// separating it from its first child's block-start margin.
func parentCanCollapseTopMargin(parent *Box) bool {
	return parentCanCollapseBlockStartMargin(parent, NewDir(HorizontalTB))
}

// parentCanCollapseBlockStartMargin is the Dir-aware version.
func parentCanCollapseBlockStartMargin(parent *Box, dir Dir) bool {
	if dir.BlockStartEdge(parent.Border) > 0 || dir.BlockStartEdge(parent.Padding) > 0 {
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
		// display: flow-root creates a BFC — parent-child margin collapsing is blocked
		if display == css.DisplayFlowRoot {
			return false
		}
		floatType := parent.Style.GetFloat()
		if floatType != css.FloatNone {
			return false
		}
	}
	return true
}

// parentCanCollapseBottomMargin returns true if the parent has no block-end border or padding
// separating it from its last child's block-end margin.
func parentCanCollapseBottomMargin(parent *Box) bool {
	return parentCanCollapseBlockEndMargin(parent, NewDir(HorizontalTB))
}

// parentCanCollapseBlockEndMargin is the Dir-aware version.
func parentCanCollapseBlockEndMargin(parent *Box, dir Dir) bool {
	if dir.BlockEndEdge(parent.Border) > 0 || dir.BlockEndEdge(parent.Padding) > 0 {
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
		// display: flow-root creates a BFC — parent-child margin collapsing is blocked
		if display == css.DisplayFlowRoot {
			return false
		}
		floatType := parent.Style.GetFloat()
		if floatType != css.FloatNone {
			return false
		}
	}
	return true
}
