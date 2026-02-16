package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// applyVerticalAlign applies vertical alignment to a box within a line
func (le *LayoutEngine) applyVerticalAlign(box *Box, lineY float64, lineHeight float64) {
	valign := box.Style.GetVerticalAlign()
	boxHeight := le.getTotalHeight(box)

	switch valign {
	case css.VerticalAlignTop:
		// Align top of box with top of line
		box.Y = lineY
	case css.VerticalAlignMiddle:
		// Center box vertically in line
		box.Y = lineY + (lineHeight-boxHeight)/2
	case css.VerticalAlignBottom:
		// Align bottom of box with bottom of line
		box.Y = lineY + lineHeight - boxHeight
	case css.VerticalAlignBaseline:
		// Default - already positioned at baseline (lineY)
		// Could be enhanced with true baseline alignment in the future
		box.Y = lineY
	}
}

// applyTextAlign shifts inline children according to text-align property
func (le *LayoutEngine) applyTextAlign(box *Box, textAlign string, contentWidth float64) {
	contentLeft := box.X + box.Border.Left + box.Padding.Left

	for _, child := range box.Children {
		if child.Style == nil {
			continue
		}
		childDisplay := child.Style.GetDisplay()
		// Only apply to inline/inline-block children, or text nodes
		isInline := childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock
		if child.Node != nil && child.Node.Type == html.TextNode {
			isInline = true
		}
		if !isInline {
			continue
		}

		childTotalWidth := le.getTotalWidth(child)

		switch textAlign {
		case "right":
			dx := contentLeft + contentWidth - childTotalWidth - child.X
			if dx != 0 {
				child.X += dx
				le.shiftChildren(child, dx, 0)
			}
		case "center":
			dx := contentLeft + (contentWidth-childTotalWidth)/2 - child.X
			if dx != 0 {
				child.X += dx
				le.shiftChildren(child, dx, 0)
			}
		}
	}
}

// applyTextAlignToBoxes applies text-align to a slice of boxes instead of box.Children.
// Groups boxes by line (Y position) and shifts each line as a whole.
func (le *LayoutEngine) applyTextAlignToBoxes(boxes []*Box, parentBox *Box, textAlign string, contentWidth float64) {
	contentLeft := parentBox.X + parentBox.Border.Left + parentBox.Padding.Left
	contentRight := contentLeft + contentWidth

	// Group inline boxes by line (same Y position)
	type lineGroup struct {
		y      float64
		boxes  []*Box
		minX   float64
		maxEnd float64 // rightmost edge
	}

	var lines []lineGroup
	for _, child := range boxes {
		if child == nil || child.Style == nil {
			continue
		}
		// Skip floats — they have physical positioning per CSS 2.1 §9.5
		if child.Style.GetFloat() != css.FloatNone {
			continue
		}
		childDisplay := child.Style.GetDisplay()
		isInline := childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock
		if child.Node != nil && child.Node.Type == html.TextNode {
			isInline = true
		}
		if !isInline {
			continue
		}

		// Find or create line group for this Y
		found := false
		childRight := child.X + le.getTotalWidth(child)
		for i := range lines {
			if lines[i].y == child.Y {
				lines[i].boxes = append(lines[i].boxes, child)
				if child.X < lines[i].minX {
					lines[i].minX = child.X
				}
				if childRight > lines[i].maxEnd {
					lines[i].maxEnd = childRight
				}
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, lineGroup{
				y:      child.Y,
				boxes:  []*Box{child},
				minX:   child.X,
				maxEnd: childRight,
			})
		}
	}

	// Shift each line as a whole
	for _, line := range lines {
		lineWidth := line.maxEnd - line.minX
		var dx float64
		switch textAlign {
		case "right":
			dx = contentRight - line.maxEnd
		case "center":
			dx = contentLeft + (contentWidth-lineWidth)/2 - line.minX
		case "left":
			dx = contentLeft - line.minX
		default:
			continue
		}
		if dx == 0 {
			continue
		}
		for _, child := range line.boxes {
			child.X += dx
			le.shiftChildren(child, dx, 0)
		}
	}
}

// mirrorInlineBoxesRTL mirrors inline box positions for RTL layout.
// For each line (group of boxes at the same Y), each box's X position
// is reflected within the container's content area:
//
//	newX = contentLeft + contentRight - oldX - boxWidth
//
// This makes the first DOM element appear at the right edge and
// subsequent elements flow leftward, as required by CSS direction: rtl.
func (le *LayoutEngine) mirrorInlineBoxesRTL(boxes []*Box, contentLeft, contentRight float64) {
	for _, box := range boxes {
		if box == nil || box.Style == nil {
			continue
		}
		// Skip floats — they have physical positioning per CSS 2.1 §9.5
		if box.Style.GetFloat() != css.FloatNone {
			continue
		}
		childDisplay := box.Style.GetDisplay()
		isInline := childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock
		if box.Node != nil && box.Node.Type == html.TextNode {
			isInline = true
		}
		if !isInline {
			continue
		}

		// Mirror within the float-adjusted available area, not the full content area.
		// Inline content was laid out in [contentLeft+leftOffset, contentRight-rightOffset],
		// so the mirror axis must use those same bounds.
		leftOffset, rightOffset := le.getFloatOffsets(box.Y)
		availLeft := contentLeft + leftOffset
		availRight := contentRight - rightOffset
		oldX := box.X
		boxWidth := box.Width
		newX := availRight - (oldX - availLeft) - boxWidth
		dx := newX - oldX
		if dx != 0 {
			box.X = newX
			le.shiftChildren(box, dx, 0)
		}
	}
}
