package layout

import (
	"strings"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/text"
)

func (le *LayoutEngine) ComputeMinMaxSizes(
	node *html.Node,
	constraint *ConstraintSpace,
	style *css.Style,
) MinMaxSizes {
	// Handle nil cases
	if node == nil || style == nil {
		return MinMaxSizes{0, 0}
	}

	// Text nodes: measure text width
	if node.Type == html.TextNode {
		return le.computeTextMinMax(node.Text, style)
	}

	// Element nodes: depends on display type
	display := style.GetDisplay()

	switch display {
	case css.DisplayInline:
		return le.computeInlineMinMax(node, constraint, style)

	case css.DisplayBlock, css.DisplayListItem:
		return le.computeBlockMinMax(node, constraint, style)

	case css.DisplayInlineBlock:
		return le.computeInlineBlockMinMax(node, constraint, style)

	case css.DisplayNone:
		return MinMaxSizes{0, 0}

	default:
		// For unknown display types, use block behavior
		return le.computeBlockMinMax(node, constraint, style)
	}
}

// computeTextMinMax calculates min/max sizes for text content.
// Min size: width of longest word (won't wrap within words)
// Max size: width of full text (preferred width without wrapping)
func (le *LayoutEngine) computeTextMinMax(textContent string, style *css.Style) MinMaxSizes {
	fontSize := style.GetFontSize()
	isBold := style.GetFontWeight() == css.FontWeightBold

	// Max size: full text width
	maxWidth, _ := text.MeasureTextWithWeight(textContent, fontSize, isBold)

	// Min size: width of longest word
	// Split text into words and measure each
	words := strings.Fields(textContent)
	minWidth := 0.0

	for _, word := range words {
		wordWidth, _ := text.MeasureTextWithWeight(word, fontSize, isBold)
		if wordWidth > minWidth {
			minWidth = wordWidth
		}
	}

	// If no words (whitespace only), min = max = 0
	if len(words) == 0 {
		minWidth = 0
		maxWidth = 0
	}

	return MinMaxSizes{
		MinContentSize: minWidth,
		MaxContentSize: maxWidth,
	}
}

// computeInlineMinMax calculates min/max sizes for inline elements.
// Inline elements with inline children: sum child widths (horizontal flow)
// Inline elements with block children: max child widths (stacking)
func (le *LayoutEngine) computeInlineMinMax(
	node *html.Node,
	constraint *ConstraintSpace,
	style *css.Style,
) MinMaxSizes {
	// Check for explicit width (important for floated inline elements)
	if width, ok := style.GetLength("width"); ok && width > 0 {
		// Explicit width: both min and max are the same
		// Add padding and border
		padding := style.GetPadding()
		border := style.GetBorderWidth()
		totalWidth := width + padding.Left + padding.Right + border.Left + border.Right

		return MinMaxSizes{
			MinContentSize: totalWidth,
			MaxContentSize: totalWidth,
		}
	}

	// Get computed styles for all children
	computedStyles := le.computeStylesForTree(node)

	// Check if children are inline or block
	hasBlockChild := false
	for _, child := range node.Children {
		childStyle := computedStyles[child]
		if childStyle != nil {
			childDisplay := childStyle.GetDisplay()
			if childDisplay == css.DisplayBlock || childDisplay == css.DisplayListItem {
				hasBlockChild = true
				break
			}
		}
	}

	// Recursively compute children sizes
	var minContent, maxContent float64

	if hasBlockChild {
		// Block children: use max of child sizes (stacking vertically)
		for _, child := range node.Children {
			childStyle := computedStyles[child]
			if childStyle == nil || childStyle.GetDisplay() == css.DisplayNone {
				continue
			}

			childSizes := le.ComputeMinMaxSizes(child, constraint, childStyle)
			if childSizes.MinContentSize > minContent {
				minContent = childSizes.MinContentSize
			}
			if childSizes.MaxContentSize > maxContent {
				maxContent = childSizes.MaxContentSize
			}
		}
	} else {
		// Inline children: sum child sizes (horizontal flow)
		for _, child := range node.Children {
			childStyle := computedStyles[child]
			if childStyle == nil || childStyle.GetDisplay() == css.DisplayNone {
				continue
			}

			childSizes := le.ComputeMinMaxSizes(child, constraint, childStyle)
			minContent += childSizes.MinContentSize
			maxContent += childSizes.MaxContentSize
		}
	}

	// Add padding and border (no margin for inline)
	padding := style.GetPadding()
	border := style.GetBorderWidth()

	minContent += padding.Left + padding.Right + border.Left + border.Right
	maxContent += padding.Left + padding.Right + border.Left + border.Right

	return MinMaxSizes{
		MinContentSize: minContent,
		MaxContentSize: maxContent,
	}
}

// computeBlockMinMax calculates min/max sizes for block elements.
// For blocks: min/max based on children (blocks stack vertically)
func (le *LayoutEngine) computeBlockMinMax(
	node *html.Node,
	constraint *ConstraintSpace,
	style *css.Style,
) MinMaxSizes {
	// Check for explicit width
	if width, ok := style.GetLength("width"); ok && width > 0 {
		// Explicit width: both min and max are the same
		return MinMaxSizes{
			MinContentSize: width,
			MaxContentSize: width,
		}
	}

	// Auto width: compute from children
	computedStyles := le.computeStylesForTree(node)

	var minContent, maxContent float64

	// For block elements, take max of children (they stack vertically)
	for _, child := range node.Children {
		childStyle := computedStyles[child]
		if childStyle == nil || childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		childSizes := le.ComputeMinMaxSizes(child, constraint, childStyle)
		if childSizes.MinContentSize > minContent {
			minContent = childSizes.MinContentSize
		}
		if childSizes.MaxContentSize > maxContent {
			maxContent = childSizes.MaxContentSize
		}
	}

	// Add padding and border
	padding := style.GetPadding()
	border := style.GetBorderWidth()

	minContent += padding.Left + padding.Right + border.Left + border.Right
	maxContent += padding.Left + padding.Right + border.Left + border.Right

	return MinMaxSizes{
		MinContentSize: minContent,
		MaxContentSize: maxContent,
	}
}

// computeInlineBlockMinMax calculates min/max sizes for inline-block elements.
// Inline-blocks are sized like blocks but participate in inline layout.
func (le *LayoutEngine) computeInlineBlockMinMax(
	node *html.Node,
	constraint *ConstraintSpace,
	style *css.Style,
) MinMaxSizes {
	// For now, treat inline-blocks like blocks for sizing
	return le.computeBlockMinMax(node, constraint, style)
}

// computeStylesForTree computes styles for a node and all its descendants.
// This is a helper to avoid recomputing styles multiple times.
func (le *LayoutEngine) computeStylesForTree(root *html.Node) map[*html.Node]*css.Style {
	styles := make(map[*html.Node]*css.Style)

	var traverse func(*html.Node)
	traverse = func(node *html.Node) {
		if node == nil {
			return
		}

		// Compute style for this node using the full ComputeStyle API
		// Use viewport dimensions from layout engine
		styles[node] = css.ComputeStyle(node, le.stylesheets, le.viewport.width, le.viewport.height)

		// Recursively traverse children
		for _, child := range node.Children {
			traverse(child)
		}
	}

	traverse(root)
	return styles
}

func (le *LayoutEngine) ComputeIntrinsicSizes(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	if node == nil {
		return IntrinsicSizes{}
	}

	// Text nodes: measure with and without wrapping
	if node.Type == html.TextNode {
		return le.computeTextIntrinsicSizes(node.Text, style)
	}

	// Element nodes
	if node.Type != html.ElementNode {
		return IntrinsicSizes{}
	}

	// Images have intrinsic dimensions
	if node.TagName == "img" {
		return le.computeImageIntrinsicSizes(node, style)
	}

	display := style.GetDisplay()

	// Replaced elements (images, etc.) use their natural size
	if display == css.DisplayNone {
		return IntrinsicSizes{}
	}

	// Get box model values
	padding := style.GetPadding()
	border := style.GetBorderWidth()
	horizontalExtra := padding.Left + padding.Right + border.Left + border.Right

	// For inline elements, sum up children's intrinsic sizes
	if display == css.DisplayInline {
		return le.computeInlineIntrinsicSizes(node, style, computedStyles, horizontalExtra)
	}

	// For block/inline-block, compute based on children
	return le.computeBlockIntrinsicSizes(node, style, computedStyles, horizontalExtra)
}

// computeTextIntrinsicSizes computes intrinsic sizes for text content
func (le *LayoutEngine) computeTextIntrinsicSizes(textContent string, style *css.Style) IntrinsicSizes {
	if textContent == "" {
		return IntrinsicSizes{}
	}

	fontSize := style.GetFontSize()
	fontWeight := style.GetFontWeight()
	bold := fontWeight == css.FontWeightBold

	// Max-content: width without any wrapping
	maxContent, _ := text.MeasureTextWithWeight(textContent, fontSize, bold)

	// Min-content: width of longest word (break at spaces)
	minContent := 0.0
	words := strings.Fields(textContent)
	for _, word := range words {
		wordWidth, _ := text.MeasureTextWithWeight(word, fontSize, bold)
		if wordWidth > minContent {
			minContent = wordWidth
		}
	}

	return IntrinsicSizes{
		MinContent: minContent,
		MaxContent: maxContent,
		Preferred:  maxContent,
	}
}

// computeImageIntrinsicSizes computes intrinsic sizes for images
func (le *LayoutEngine) computeImageIntrinsicSizes(node *html.Node, style *css.Style) IntrinsicSizes {
	src, _ := node.GetAttribute("src")
	if src == "" {
		return IntrinsicSizes{}
	}

	// Try to get image dimensions
	var imgWidth float64
	if w, _, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil {
		imgWidth = float64(w)
	}

	// CSS width overrides natural width
	if cssW, ok := style.GetLength("width"); ok && cssW > 0 {
		imgWidth = cssW
	}

	return IntrinsicSizes{
		MinContent: imgWidth,
		MaxContent: imgWidth,
		Preferred:  imgWidth,
	}
}

// computeInlineIntrinsicSizes computes intrinsic sizes for inline elements
func (le *LayoutEngine) computeInlineIntrinsicSizes(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style, horizontalExtra float64) IntrinsicSizes {
	var minContent, maxContent float64

	for _, child := range node.Children {
		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = style // Inherit parent style for text
		}

		childSizes := le.ComputeIntrinsicSizes(child, childStyle, computedStyles)

		// For inline, children are laid out horizontally
		// Min-content: largest child min-content (can wrap between children)
		if childSizes.MinContent > minContent {
			minContent = childSizes.MinContent
		}
		// Max-content: sum of all children (no wrapping)
		maxContent += childSizes.MaxContent
	}

	return IntrinsicSizes{
		MinContent: minContent + horizontalExtra,
		MaxContent: maxContent + horizontalExtra,
		Preferred:  maxContent + horizontalExtra,
	}
}

// computeBlockIntrinsicSizes computes intrinsic sizes for block/inline-block elements
func (le *LayoutEngine) computeBlockIntrinsicSizes(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style, horizontalExtra float64) IntrinsicSizes {
	var minContent, maxContent float64

	// Check for explicit width
	if width, ok := style.GetLength("width"); ok && width > 0 {
		return IntrinsicSizes{
			MinContent: width + horizontalExtra,
			MaxContent: width + horizontalExtra,
			Preferred:  width + horizontalExtra,
		}
	}

	// Track current inline run for block containers
	var inlineMinContent, inlineMaxContent float64

	for _, child := range node.Children {
		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
		}

		childSizes := le.ComputeIntrinsicSizes(child, childStyle, computedStyles)
		childDisplay := childStyle.GetDisplay()

		if childDisplay == css.DisplayBlock || childDisplay == css.DisplayListItem {
			// Block child: flush inline run, then take max of block widths
			if inlineMaxContent > maxContent {
				maxContent = inlineMaxContent
			}
			if inlineMinContent > minContent {
				minContent = inlineMinContent
			}
			inlineMinContent = 0
			inlineMaxContent = 0

			if childSizes.MinContent > minContent {
				minContent = childSizes.MinContent
			}
			if childSizes.MaxContent > maxContent {
				maxContent = childSizes.MaxContent
			}
		} else {
			// Inline child: accumulate in current run
			if childSizes.MinContent > inlineMinContent {
				inlineMinContent = childSizes.MinContent
			}
			inlineMaxContent += childSizes.MaxContent
		}
	}

	// Flush final inline run
	if inlineMaxContent > maxContent {
		maxContent = inlineMaxContent
	}
	if inlineMinContent > minContent {
		minContent = inlineMinContent
	}

	return IntrinsicSizes{
		MinContent: minContent + horizontalExtra,
		MaxContent: maxContent + horizontalExtra,
		Preferred:  maxContent + horizontalExtra,
	}
}

// ============================================================================
// Layout Mode Implementations
// ============================================================================

// ComputeIntrinsicSizes for BlockLayoutMode
func (m *BlockLayoutMode) ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	return le.ComputeIntrinsicSizes(node, style, computedStyles)
}

// LayoutChildren for BlockLayoutMode - to be implemented as refactor progresses
func (m *BlockLayoutMode) LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box {
	// This will be filled in as we refactor layoutNode
	return nil
}

// ComputeIntrinsicSizes for InlineLayoutMode
