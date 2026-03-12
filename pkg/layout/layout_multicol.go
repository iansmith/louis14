package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// layoutMulticolumn implements CSS Multi-column Layout (CSS Multicol Level 1).
// It distributes child content across evenly-sized columns and supports
// column-span: all elements that span across all columns.
func (le *LayoutEngine) layoutMulticolumn(
	box *Box, x, y, availableWidth float64,
	style *css.Style,
	dir Dir,
	computedStyles map[*html.Node]*css.Style,
) {
	columnCount := style.GetColumnCount()
	columnWidth := style.GetColumnWidth()
	columnGap := style.GetColumnGapMulticol()

	// Content width available for columns (inside padding+border)
	contentWidth := box.Width - box.Padding.Left - box.Padding.Right -
		box.Border.Left - box.Border.Right
	if contentWidth <= 0 {
		contentWidth = availableWidth - box.Padding.Left - box.Padding.Right -
			box.Border.Left - box.Border.Right
	}

	// Resolve column count and width per CSS Multicol spec §3.4
	var numCols int
	if columnCount > 0 && columnWidth > 0 {
		maxCols := int((contentWidth + columnGap) / (columnWidth + columnGap))
		if maxCols < 1 {
			maxCols = 1
		}
		numCols = columnCount
		if maxCols < numCols {
			numCols = maxCols
		}
	} else if columnCount > 0 {
		numCols = columnCount
	} else if columnWidth > 0 {
		numCols = int((contentWidth + columnGap) / (columnWidth + columnGap))
		if numCols < 1 {
			numCols = 1
		}
	} else {
		numCols = 1
	}

	colWidth := (contentWidth - float64(numCols-1)*columnGap) / float64(numCols)
	if colWidth < 0 {
		colWidth = 0
	}

	// Collect flow children
	children := multicolCollectFlowChildren(box.Node, computedStyles)
	if len(children) == 0 {
		// No element children — may have inline/text content directly
		le.layoutMulticolumnInline(box, numCols, colWidth, columnGap, computedStyles)
		return
	}

	// --- column-span: all support ---
	// Split children into segments: each segment is either a list of normal
	// children (laid out in columns) or a single column-span:all child laid
	// out at full content width.
	type segment struct {
		spanAll  bool        // true → single spanning child
		node     *html.Node  // set when spanAll=true
		children []*html.Node // set when spanAll=false
	}

	var segments []segment
	var cur segment
	for _, child := range children {
		childStyle := computedStyles[child]
		if childStyle != nil && childStyle.GetColumnSpan() == "all" {
			// Close current normal group (even if empty) and add span segment
			segments = append(segments, cur)
			cur = segment{}
			segments = append(segments, segment{spanAll: true, node: child})
		} else {
			cur.children = append(cur.children, child)
		}
	}
	segments = append(segments, cur)

	// Check whether any spanning children exist
	hasSpans := false
	for _, seg := range segments {
		if seg.spanAll {
			hasSpans = true
			break
		}
	}

	if !hasSpans {
		// Fast path: no column-span:all — use original algorithm
		le.layoutMulticolumnSegment(box, numCols, colWidth, columnGap, dir, children, computedStyles)
		return
	}

	// Slow path: handle spans
	box.Children = nil
	startX := box.X + box.Border.Left + box.Padding.Left
	startY := box.Y + box.Border.Top + box.Padding.Top
	curY := startY

	for _, seg := range segments {
		if seg.spanAll {
			// Lay out spanning child at full content width
			spanBox := le.layoutNodeHTB(seg.node, startX, curY, contentWidth, computedStyles, box)
			if spanBox != nil {
				spanH := spanBox.Height +
					spanBox.Margin.Top + spanBox.Margin.Bottom +
					spanBox.Padding.Top + spanBox.Padding.Bottom +
					spanBox.Border.Top + spanBox.Border.Bottom
				// Reposition: spanning child uses margin area origin
				dx := startX + spanBox.Margin.Left - spanBox.X
				dy := curY + spanBox.Margin.Top - spanBox.Y
				multicolRepositionBox(spanBox, dx, dy)
				box.Children = append(box.Children, spanBox)
				spanBox.Parent = box
				curY += spanH
			}
		} else if len(seg.children) > 0 {
			// Lay out this group in columns; collect the column height
			groupH := le.layoutMulticolumnSegmentAt(box, numCols, colWidth, columnGap,
				startX, curY, dir, seg.children, computedStyles)
			curY += groupH
		}
	}

	// Set total box height
	innerH := curY - startY
	box.Height = innerH + box.Padding.Top + box.Padding.Bottom +
		box.Border.Top + box.Border.Bottom
}

// layoutMulticolumnSegment distributes a list of children across columns and
// appends results to box.Children. Used by the no-span fast path.
func (le *LayoutEngine) layoutMulticolumnSegment(
	box *Box,
	numCols int, colWidth, columnGap float64,
	dir Dir,
	children []*html.Node,
	computedStyles map[*html.Node]*css.Style,
) {
	startX := box.X + box.Border.Left + box.Padding.Left
	startY := box.Y + box.Border.Top + box.Padding.Top
	maxH := le.layoutMulticolumnSegmentAt(box, numCols, colWidth, columnGap,
		startX, startY, dir, children, computedStyles)
	box.Height = maxH + box.Padding.Top + box.Padding.Bottom +
		box.Border.Top + box.Border.Bottom
}

// layoutMulticolumnSegmentAt lays out children in columns starting at (startX,
// startY), appends result boxes to box.Children, and returns the height of the
// tallest column.
func (le *LayoutEngine) layoutMulticolumnSegmentAt(
	box *Box,
	numCols int, colWidth, columnGap float64,
	startX, startY float64,
	dir Dir,
	children []*html.Node,
	computedStyles map[*html.Node]*css.Style,
) float64 {
	type childLayout struct {
		node   *html.Node
		box    *Box
		height float64
	}

	var laid []childLayout
	totalChildHeight := 0.0
	tempY := 0.0

	for _, child := range children {
		childBox := le.layoutNodeHTB(child, 0, tempY, colWidth, computedStyles, box)
		if childBox == nil {
			continue
		}
		h := childBox.Height + childBox.Margin.Top + childBox.Margin.Bottom +
			childBox.Padding.Top + childBox.Padding.Bottom +
			childBox.Border.Top + childBox.Border.Bottom
		laid = append(laid, childLayout{node: child, box: childBox, height: h})
		totalChildHeight += h
		tempY += h
	}

	if len(laid) == 0 {
		return 0
	}

	// Distribute children across columns
	targetHeight := totalChildHeight / float64(numCols)
	if targetHeight < 1 {
		targetHeight = 1
	}

	type column struct {
		items  []childLayout
		height float64
	}
	columns := make([]column, numCols)
	colIdx := 0

	for _, item := range laid {
		if colIdx < numCols-1 && len(columns[colIdx].items) > 0 &&
			columns[colIdx].height+item.height > targetHeight*1.1 {
			colIdx++
		}
		columns[colIdx].items = append(columns[colIdx].items, item)
		columns[colIdx].height += item.height
	}

	// Position children in their columns
	maxColumnHeight := 0.0
	for i, col := range columns {
		colX := startX + float64(i)*(colWidth+columnGap)
		colY := startY

		for _, item := range col.items {
			childBox := item.box
			dx := colX + childBox.Margin.Left - childBox.X
			dy := colY + childBox.Margin.Top - childBox.Y
			multicolRepositionBox(childBox, dx, dy)

			box.Children = append(box.Children, childBox)
			childBox.Parent = box
			colY += item.height
		}
		if col.height > maxColumnHeight {
			maxColumnHeight = col.height
		}
	}

	return maxColumnHeight
}

// multicolCollectFlowChildren returns the in-flow element children of a node.
func multicolCollectFlowChildren(node *html.Node, computedStyles map[*html.Node]*css.Style) []*html.Node {
	var children []*html.Node
	for _, child := range node.Children {
		if child.Type != html.ElementNode {
			continue
		}
		if s, ok := computedStyles[child]; ok {
			if s.GetDisplay() == css.DisplayNone {
				continue
			}
			pos := s.GetPosition()
			if pos == css.PositionAbsolute || pos == css.PositionFixed {
				continue
			}
		}
		children = append(children, child)
	}
	return children
}

// multicolRepositionBox moves a box and all its descendants by (dx, dy).
func multicolRepositionBox(box *Box, dx, dy float64) {
	box.X += dx
	box.Y += dy
	for _, child := range box.Children {
		multicolRepositionBox(child, dx, dy)
	}
}

// layoutMulticolumnInline handles multicol containers whose content is
// inline/text nodes (no element children). It lays out the content at
// colWidth, then distributes the resulting line boxes across columns.
func (le *LayoutEngine) layoutMulticolumnInline(
	box *Box,
	numCols int,
	colWidth, columnGap float64,
	computedStyles map[*html.Node]*css.Style,
) {
	if box.Node == nil || len(box.Node.Children) == 0 {
		return
	}

	startX := box.X + box.Border.Left + box.Padding.Left
	startY := box.Y + box.Border.Top + box.Padding.Top

	// Use inline layout at colWidth to get line-box fragments
	result := le.LayoutInlineContentToBoxes(
		box.Node.Children,
		box,
		colWidth,
		startY,
		computedStyles,
		nil,
		NewDir(HorizontalTB),
	)
	if result == nil || len(result.ChildBoxes) == 0 {
		return
	}

	// Group boxes by their Y coordinate (each distinct Y = one line)
	type lineGroup struct {
		y    float64
		h    float64
		boxes []*Box
	}
	var lines []lineGroup

	for _, b := range result.ChildBoxes {
		placed := false
		for i := range lines {
			if lines[i].y == b.Y {
				lines[i].boxes = append(lines[i].boxes, b)
				h := b.Height
				if h > lines[i].h {
					lines[i].h = h
				}
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, lineGroup{
				y:     b.Y,
				h:     b.Height,
				boxes: []*Box{b},
			})
		}
	}

	if len(lines) == 0 {
		return
	}

	// Distribute lines across columns
	linesPerCol := (len(lines) + numCols - 1) / numCols

	box.Children = nil
	maxColHeight := 0.0

	for colIdx := 0; colIdx < numCols; colIdx++ {
		colX := startX + float64(colIdx)*(colWidth+columnGap)
		firstLine := colIdx * linesPerCol
		lastLine := firstLine + linesPerCol
		if lastLine > len(lines) {
			lastLine = len(lines)
		}
		if firstLine >= len(lines) {
			break
		}

		colHeight := 0.0
		for lineIdx := firstLine; lineIdx < lastLine; lineIdx++ {
			line := lines[lineIdx]
			// Compute where this line should go within the column
			targetY := startY + float64(lineIdx-firstLine)*line.h
			for _, b := range line.boxes {
				dx := colX - startX
				dy := targetY - line.y
				multicolRepositionBox(b, dx, dy)
				box.Children = append(box.Children, b)
				b.Parent = box
			}
			colHeight += line.h
		}
		if colHeight > maxColHeight {
			maxColHeight = colHeight
		}
	}

	// Update box height
	box.Height = maxColHeight + box.Padding.Top + box.Padding.Bottom +
		box.Border.Top + box.Border.Bottom
}
