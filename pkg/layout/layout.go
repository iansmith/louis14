package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

type Box struct {
	Node     *html.Node
	Style    *css.Style
	X        float64
	Y        float64
	Width    float64  // Content width
	Height   float64  // Content height
	Margin   css.BoxEdge
	Padding  css.BoxEdge
	Border   css.BoxEdge
	Children []*Box   // Phase 2: Nested boxes
}

type LayoutEngine struct {
	viewport struct {
		width  float64
		height float64
	}
}

func NewLayoutEngine(viewportWidth, viewportHeight float64) *LayoutEngine {
	le := &LayoutEngine{}
	le.viewport.width = viewportWidth
	le.viewport.height = viewportHeight
	return le
}

func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
	// Phase 3: Compute styles from stylesheets
	computedStyles := css.ApplyStylesToDocument(doc)

	// Phase 2: Recursively layout the tree starting from root's children
	boxes := make([]*Box, 0)
	y := 0.0

	for _, node := range doc.Root.Children {
		if node.Type == html.ElementNode {
			box := le.layoutNode(node, 0, y, le.viewport.width, computedStyles)
			boxes = append(boxes, box)
			// Advance Y by the total height of this box (including margin)
			y += le.getTotalHeight(box)
		}
	}

	return boxes
}

// layoutNode recursively layouts a node and its children
func (le *LayoutEngine) layoutNode(node *html.Node, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style) *Box {
	// Phase 3: Use computed styles from cascade
	style := computedStyles[node]
	if style == nil {
		style = css.NewStyle()
	}

	// Get box model values
	margin := style.GetMargin()
	padding := style.GetPadding()
	border := style.GetBorderWidth()

	// Apply margin offset
	x += margin.Left
	y += margin.Top

	// Calculate content width
	var contentWidth float64
	if w, ok := style.GetLength("width"); ok {
		contentWidth = w
	} else {
		// Default to available width minus horizontal margin, padding, border
		contentWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
	}

	// Calculate content height
	var contentHeight float64
	if h, ok := style.GetLength("height"); ok {
		contentHeight = h
	} else {
		contentHeight = 50 // Default height
	}

	box := &Box{
		Node:    node,
		Style:   style,
		X:       x,
		Y:       y,
		Width:   contentWidth,
		Height:  contentHeight,
		Margin:  margin,
		Padding: padding,
		Border:  border,
		Children: make([]*Box, 0),
	}

	// Phase 2: Recursively layout children
	childY := y + border.Top + padding.Top
	childAvailableWidth := contentWidth - padding.Left - padding.Right

	for _, child := range node.Children {
		if child.Type == html.ElementNode {
			childBox := le.layoutNode(
				child,
				x + border.Left + padding.Left,
				childY,
				childAvailableWidth,
				computedStyles,
			)
			box.Children = append(box.Children, childBox)
			childY += le.getTotalHeight(childBox)
		}
	}

	// If height is auto and we have children, adjust height to fit content
	if _, ok := style.GetLength("height"); !ok && len(box.Children) > 0 {
		// Calculate total height of children
		totalChildHeight := 0.0
		for _, child := range box.Children {
			totalChildHeight += le.getTotalHeight(child)
		}
		box.Height = totalChildHeight
	}

	return box
}

// getStyle returns the computed style for a node
func (le *LayoutEngine) getStyle(node *html.Node) *css.Style {
	if styleAttr, ok := node.GetAttribute("style"); ok {
		return css.ParseInlineStyle(styleAttr)
	}
	return css.NewStyle()
}

// getTotalHeight returns the total height including margin, border, padding
func (le *LayoutEngine) getTotalHeight(box *Box) float64 {
	return box.Margin.Top + box.Border.Top + box.Padding.Top +
		box.Height +
		box.Padding.Bottom + box.Border.Bottom + box.Margin.Bottom
}

// getTotalWidth returns the total width including margin, border, padding
func (le *LayoutEngine) getTotalWidth(box *Box) float64 {
	return box.Margin.Left + box.Border.Left + box.Padding.Left +
		box.Width +
		box.Padding.Right + box.Border.Right + box.Margin.Right
}
