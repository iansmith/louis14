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

	// Content inline-size available for columns (inside padding+border)
	contentInline := dir.ContentInlineSize(box)
	if contentInline <= 0 {
		contentInline = availableWidth - dir.InlineBorderBox(box.Padding, box.Border)
	}

	// Resolve column count and width per CSS Multicol spec §3.4
	var numCols int
	if columnCount > 0 && columnWidth > 0 {
		maxCols := int((contentInline + columnGap) / (columnWidth + columnGap))
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
		numCols = int((contentInline + columnGap) / (columnWidth + columnGap))
		if numCols < 1 {
			numCols = 1
		}
	} else {
		numCols = 1
	}

	colInline := (contentInline - float64(numCols-1)*columnGap) / float64(numCols)
	if colInline < 0 {
		colInline = 0
	}

	// Collect flow children
	children := multicolCollectFlowChildren(box.Node, computedStyles)
	if len(children) == 0 {
		// No element children — may have inline/text content directly
		le.layoutMulticolumnInline(box, numCols, colInline, columnGap, dir, computedStyles)
		return
	}

	// --- column-span: all support ---
	// Split children into segments: each segment is either a list of normal
	// children (laid out in columns) or a single column-span:all child laid
	// out at full content inline-size.
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
		le.layoutMulticolumnSegment(box, numCols, colInline, columnGap, dir, children, computedStyles)
		return
	}

	// Slow path: handle spans
	box.Children = nil
	startInline := dir.ContentStartInlinePos(box)
	startBlock := dir.ContentStartBlockPos(box)

	curBlockOffset := 0.0 // logical offset from block-start

	for _, seg := range segments {
		if seg.spanAll {
			// Lay out spanning child at full content inline-size
			spanBlock := startBlock + curBlockOffset
			if dir.WM == VerticalRL {
				spanBlock = startBlock - curBlockOffset
			}
			spanX, spanY := dir.MakePhysical(startInline, spanBlock)
			spanBox := le.layoutNode(seg.node, spanX, spanY, contentInline, dir, computedStyles, box)
			if spanBox != nil {
				spanBlockExt := dir.BlockSize(spanBox) +
					dir.BlockStartEdge(spanBox.Margin) + dir.BlockEndEdge(spanBox.Margin) +
					dir.BlockStartEdge(spanBox.Padding) + dir.BlockEndEdge(spanBox.Padding) +
					dir.BlockStartEdge(spanBox.Border) + dir.BlockEndEdge(spanBox.Border)
				// Reposition: spanning child uses margin area origin
				targetInline := startInline + dir.InlineStartEdge(spanBox.Margin)
				spanMarginBlock := curBlockOffset + dir.BlockStartEdge(spanBox.Margin)
				targetBlock := startBlock + spanMarginBlock
				if dir.WM == VerticalRL {
					targetBlock = startBlock - spanMarginBlock - dir.BlockSize(spanBox)
				}
				targetX, targetY := dir.MakePhysical(targetInline, targetBlock)
				dx := targetX - spanBox.X
				dy := targetY - spanBox.Y
				multicolRepositionBox(spanBox, dx, dy)
				box.Children = append(box.Children, spanBox)
				spanBox.Parent = box
				curBlockOffset += spanBlockExt
			}
		} else if len(seg.children) > 0 {
			// Lay out this group in columns; collect the column block extent
			segBlock := startBlock + curBlockOffset
			if dir.WM == VerticalRL {
				segBlock = startBlock - curBlockOffset
			}
			segX, segY := dir.MakePhysical(startInline, segBlock)
			groupBlockExt := le.layoutMulticolumnSegmentAt(box, numCols, colInline, columnGap,
				segX, segY, dir, seg.children, computedStyles)
			curBlockOffset += groupBlockExt
		}
	}

	// Set total box block-size
	dir.SetBlockSize(box, curBlockOffset+dir.BlockBorderBox(box.Padding, box.Border))
}

// layoutMulticolumnSegment distributes a list of children across columns and
// appends results to box.Children. Used by the no-span fast path.
func (le *LayoutEngine) layoutMulticolumnSegment(
	box *Box,
	numCols int, colInline, columnGap float64,
	dir Dir,
	children []*html.Node,
	computedStyles map[*html.Node]*css.Style,
) {
	startInline := dir.ContentStartInlinePos(box)
	startBlock := dir.ContentStartBlockPos(box)
	startX, startY := dir.MakePhysical(startInline, startBlock)
	maxBlockExt := le.layoutMulticolumnSegmentAt(box, numCols, colInline, columnGap,
		startX, startY, dir, children, computedStyles)
	dir.SetBlockSize(box, maxBlockExt+dir.BlockBorderBox(box.Padding, box.Border))
}

// layoutMulticolumnSegmentAt lays out children in columns starting at physical
// (startX, startY), appends result boxes to box.Children, and returns the
// block extent of the tallest column.
func (le *LayoutEngine) layoutMulticolumnSegmentAt(
	box *Box,
	numCols int, colInline, columnGap float64,
	startX, startY float64,
	dir Dir,
	children []*html.Node,
	computedStyles map[*html.Node]*css.Style,
) float64 {
	type childLayout struct {
		node     *html.Node
		box      *Box
		blockExt float64 // block extent including margins+padding+border
	}

	// Derive segment block base from the physical start position.
	// This is the physical coordinate of block-start for this segment.
	segBlockBase := dir.ExtractBlock(startX, startY)

	var laid []childLayout
	totalBlockExt := 0.0
	tempBlockOffset := 0.0

	for _, child := range children {
		// Lay out at temporary position for measurement.
		// Use segment-relative block positions.
		tempBlock := segBlockBase + tempBlockOffset
		if dir.WM == VerticalRL {
			tempBlock = segBlockBase - tempBlockOffset
		}
		tempX, tempY := dir.MakePhysical(0, tempBlock)
		childBox := le.layoutNode(child, tempX, tempY, colInline, dir, computedStyles, box)
		if childBox == nil {
			continue
		}
		blockExt := dir.BlockSize(childBox) +
			dir.BlockStartEdge(childBox.Margin) + dir.BlockEndEdge(childBox.Margin) +
			dir.BlockStartEdge(childBox.Padding) + dir.BlockEndEdge(childBox.Padding) +
			dir.BlockStartEdge(childBox.Border) + dir.BlockEndEdge(childBox.Border)
		laid = append(laid, childLayout{node: child, box: childBox, blockExt: blockExt})
		totalBlockExt += blockExt
		tempBlockOffset += blockExt
	}

	if len(laid) == 0 {
		return 0
	}

	// Distribute children across columns
	targetBlockExt := totalBlockExt / float64(numCols)
	if targetBlockExt < 1 {
		targetBlockExt = 1
	}

	type column struct {
		items    []childLayout
		blockExt float64
	}
	columns := make([]column, numCols)
	colIdx := 0

	for _, item := range laid {
		if colIdx < numCols-1 && len(columns[colIdx].items) > 0 &&
			columns[colIdx].blockExt+item.blockExt > targetBlockExt*1.1 {
			colIdx++
		}
		columns[colIdx].items = append(columns[colIdx].items, item)
		columns[colIdx].blockExt += item.blockExt
	}

	// Position children in their columns
	startInline := dir.ExtractInline(startX, startY)
	maxColumnBlockExt := 0.0
	for i, col := range columns {
		colInlinePos := startInline + float64(i)*(colInline+columnGap)
		colBlockOffset := 0.0 // logical offset from block-start within this column

		for _, item := range col.items {
			childBox := item.box
			// Target physical positions — relative to segment start (segBlockBase)
			targetInline := colInlinePos + dir.InlineStartEdge(childBox.Margin)
			marginBlock := colBlockOffset + dir.BlockStartEdge(childBox.Margin)
			targetBlock := segBlockBase + marginBlock
			if dir.WM == VerticalRL {
				targetBlock = segBlockBase - marginBlock - dir.BlockSize(childBox)
			}
			targetX, targetY := dir.MakePhysical(targetInline, targetBlock)
			dx := targetX - childBox.X
			dy := targetY - childBox.Y
			multicolRepositionBox(childBox, dx, dy)

			box.Children = append(box.Children, childBox)
			childBox.Parent = box
			colBlockOffset += item.blockExt
		}
		if col.blockExt > maxColumnBlockExt {
			maxColumnBlockExt = col.blockExt
		}
	}

	return maxColumnBlockExt
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
// colInline width, then distributes the resulting line boxes across columns.
func (le *LayoutEngine) layoutMulticolumnInline(
	box *Box,
	numCols int,
	colInline, columnGap float64,
	dir Dir,
	computedStyles map[*html.Node]*css.Style,
) {
	if box.Node == nil || len(box.Node.Children) == 0 {
		return
	}

	startX := box.X + box.Border.Left + box.Padding.Left
	startY := box.Y + box.Border.Top + box.Padding.Top

	// Use inline layout at colInline to get line-box fragments.
	// Inline text layout remains HTB for now — Dir-aware inline text in
	// multicol requires further work in the inline layout pipeline.
	result := le.LayoutInlineContentToBoxes(
		box.Node.Children,
		box,
		colInline,
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
		y     float64
		h     float64
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
		colX := startX + float64(colIdx)*(colInline+columnGap)
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
