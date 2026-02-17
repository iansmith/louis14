package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// layoutMulticolumn implements CSS Multi-column Layout (CSS Multicol Level 1).
// It distributes child content across evenly-sized columns.
func (le *LayoutEngine) layoutMulticolumn(
	box *Box, x, y, availableWidth float64,
	style *css.Style,
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
		return
	}

	// Lay out each child in a temporary position with column width
	type childLayout struct {
		node   *html.Node
		box    *Box
		height float64
	}

	var laid []childLayout
	totalChildHeight := 0.0
	tempY := 0.0

	for _, child := range children {
		childBox := le.layoutNode(child, 0, tempY, colWidth, computedStyles, box)
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
		return
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
	box.Children = nil
	startX := box.X + box.Border.Left + box.Padding.Left
	startY := box.Y + box.Border.Top + box.Padding.Top
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

	// Set box height to tallest column
	box.Height = maxColumnHeight + box.Padding.Top + box.Padding.Bottom +
		box.Border.Top + box.Border.Bottom
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
