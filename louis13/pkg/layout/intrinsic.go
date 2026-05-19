package layout

import (
	"strconv"
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

	case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayListItem:
		return le.computeBlockMinMax(node, constraint, style)

	case css.DisplayInlineBlock:
		return le.computeInlineBlockMinMax(node, constraint, style)

	case css.DisplayFlex, css.DisplayInlineFlex:
		return le.computeFlexMinMax(node, constraint, style)

	case css.DisplayNone:
		return MinMaxSizes{0, 0}

	default:
		// For unknown display types, use block behavior
		return le.computeBlockMinMax(node, constraint, style)
	}
}

// computeTextMinMax calculates min/max sizes for text content.
func (le *LayoutEngine) computeTextMinMax(textContent string, style *css.Style) MinMaxSizes {
	fontSize := style.GetFontSize()
	isBold := style.GetFontWeight() == css.FontWeightBold
	isItalic := style.GetFontStyle() == css.FontStyleItalic
	isMono := style.IsMonospaceFamily()
	isAhem := style.IsAhemFamily()

	maxWidth, _ := text.MeasureTextWithStyle(textContent, fontSize, isBold, isItalic, isMono, isAhem)

	words := strings.Fields(textContent)
	minWidth := 0.0

	for _, word := range words {
		wordWidth, _ := text.MeasureTextWithStyle(word, fontSize, isBold, isItalic, isMono, isAhem)
		if wordWidth > minWidth {
			minWidth = wordWidth
		}
	}

	if len(words) == 0 {
		minWidth = 0
		maxWidth = 0
	}

	return MinMaxSizes{
		MinContentSize: minWidth,
		MaxContentSize: maxWidth,
	}
}

func (le *LayoutEngine) computeInlineMinMax(
	node *html.Node,
	constraint *ConstraintSpace,
	style *css.Style,
) MinMaxSizes {
	if width, ok := style.GetLength("width"); ok && width > 0 {
		padding := style.GetPadding()
		border := style.GetBorderWidth()
		totalWidth := width + padding.Left + padding.Right + border.Left + border.Right
		return MinMaxSizes{
			MinContentSize: totalWidth,
			MaxContentSize: totalWidth,
		}
	}

	computedStyles := le.computeStylesForTreeWithParent(node, style)

	hasBlockChild := false
	for _, child := range node.Children {
		childStyle := computedStyles[child]
		if childStyle != nil {
			childDisplay := childStyle.GetDisplay()
			if childDisplay == css.DisplayBlock || childDisplay == css.DisplayFlowRoot || childDisplay == css.DisplayListItem {
				hasBlockChild = true
				break
			}
		}
	}

	var minContent, maxContent float64

	if hasBlockChild {
		for _, child := range node.Children {
			var childStyle *css.Style
			if child.Type == html.TextNode {
				childStyle = style
			} else {
				childStyle = computedStyles[child]
			}
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
		for _, child := range node.Children {
			var childStyle *css.Style
			if child.Type == html.TextNode {
				childStyle = style
			} else {
				childStyle = computedStyles[child]
			}
			if childStyle == nil || childStyle.GetDisplay() == css.DisplayNone {
				continue
			}
			childSizes := le.ComputeMinMaxSizes(child, constraint, childStyle)
			minContent += childSizes.MinContentSize
			maxContent += childSizes.MaxContentSize
		}
	}

	padding := style.GetPadding()
	border := style.GetBorderWidth()
	minContent += padding.Left + padding.Right + border.Left + border.Right
	maxContent += padding.Left + padding.Right + border.Left + border.Right

	// Apply CSS min-width/max-width constraints with intrinsic sizing keywords.
	// When min-width: max-content, the element's minimum size is its max-content size.
	if minWStr, ok := style.Get("min-width"); ok {
		minWStr = strings.TrimSpace(minWStr)
		switch minWStr {
		case "max-content":
			if maxContent > minContent {
				minContent = maxContent
			}
		case "min-content":
			// Already the min-content — no change needed
		}
	}
	if maxWStr, ok := style.Get("max-width"); ok {
		maxWStr = strings.TrimSpace(maxWStr)
		switch maxWStr {
		case "min-content":
			if minContent < maxContent {
				maxContent = minContent
			}
		case "max-content":
			// Already the max-content — no change needed
		}
	}

	return MinMaxSizes{
		MinContentSize: minContent,
		MaxContentSize: maxContent,
	}
}

// computeBlockMinMax calculates min/max sizes for block elements.
func (le *LayoutEngine) computeBlockMinMax(
	node *html.Node,
	constraint *ConstraintSpace,
	style *css.Style,
) MinMaxSizes {
	if width, ok := style.GetLength("width"); ok && width > 0 {
		padding := style.GetPadding()
		border := style.GetBorderWidth()
		totalWidth := width + padding.Left + padding.Right + border.Left + border.Right
		return MinMaxSizes{
			MinContentSize: totalWidth,
			MaxContentSize: totalWidth,
		}
	}

	computedStyles := le.computeStylesForTreeWithParent(node, style)

	var minContent, maxContent float64

	// CSS Sizing Level 3 section 4.1: Block containers compute intrinsic widths
	// IMPORTANT: For text nodes, computeStylesForTree won't produce entries.
	// Override: use the node's own style (which has inherited properties).
	// from their children. Block-level children stack vertically (take max),
	// while inline-level children flow horizontally (sum for max-content).
	var inlineMaxContent float64
	hasInlineChildren := false

	for _, child := range node.Children {
		var childStyle *css.Style
		if child.Type == html.TextNode {
			childStyle = style
		} else {
			childStyle = computedStyles[child]
		}
		if childStyle == nil || childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		childSizes := le.ComputeMinMaxSizes(child, constraint, childStyle)

		if childSizes.MinContentSize > minContent {
			minContent = childSizes.MinContentSize
		}

		childDisplay := css.DisplayBlock
		if child.Type == html.TextNode {
			childDisplay = css.DisplayInline
		} else if childStyle != nil {
			childDisplay = childStyle.GetDisplay()
		}
		isFloat := childStyle != nil && childStyle.GetFloat() != css.FloatNone
		isInlineLevel := childDisplay == css.DisplayInline ||
			childDisplay == css.DisplayInlineBlock ||
			childDisplay == css.DisplayInlineFlex ||
			childDisplay == css.DisplayInlineGrid ||
			child.Type == html.TextNode ||
			isFloat

		if isInlineLevel {
			inlineMaxContent += childSizes.MaxContentSize
			hasInlineChildren = true
		} else {
			if childSizes.MaxContentSize > maxContent {
				maxContent = childSizes.MaxContentSize
			}
		}
	}
	if hasInlineChildren && inlineMaxContent > maxContent {
		maxContent = inlineMaxContent
	}

	if minWidthVal, ok := style.Get("min-width"); ok {
		switch minWidthVal {
		case "max-content":
			if minContent < maxContent {
				minContent = maxContent
			}
		case "min-content":
			// no-op
		default:
			if minW, ok2 := style.GetLength("min-width"); ok2 && minW > 0 {
				if minContent < minW {
					minContent = minW
				}
				if maxContent < minW {
					maxContent = minW
				}
			}
		}
	}
	if maxWidthVal, ok := style.Get("max-width"); ok {
		switch maxWidthVal {
		case "min-content":
			if maxContent > minContent {
				maxContent = minContent
			}
		case "max-content":
			// no-op
		default:
			if maxW, ok2 := style.GetLength("max-width"); ok2 && maxW > 0 {
				if minContent > maxW {
					minContent = maxW
				}
				if maxContent > maxW {
					maxContent = maxW
				}
			}
		}
	}

	padding := style.GetPadding()
	border := style.GetBorderWidth()
	minContent += padding.Left + padding.Right + border.Left + border.Right
	maxContent += padding.Left + padding.Right + border.Left + border.Right

	// Apply intrinsic size keyword constraints for min-width and max-width.
	if minWVal, ok := style.Get("min-width"); ok {
		switch strings.TrimSpace(minWVal) {
		case "max-content":
			if minContent < maxContent {
				minContent = maxContent
			}
		}
	}
	if maxWVal, ok := style.Get("max-width"); ok {
		switch strings.TrimSpace(maxWVal) {
		case "min-content":
			if maxContent > minContent {
				maxContent = minContent
			}
		}
	}

	return MinMaxSizes{
		MinContentSize: minContent,
		MaxContentSize: maxContent,
	}
}

func (le *LayoutEngine) computeInlineBlockMinMax(
	node *html.Node,
	constraint *ConstraintSpace,
	style *css.Style,
) MinMaxSizes {
	return le.computeBlockMinMax(node, constraint, style)
}

// computeFlexMinMax calculates min/max content sizes for flex containers.
// Per CSS Flexbox §9.9.1 (min-content) and §9.9.2 (max-content):
// Row direction:
//   - Min-content width: max of each item's min-content contribution
//   - Max-content width: sum of each item's max-content contribution
// Column direction:
//   - Min/max-content width: max of items' min/max-content widths (cross-axis)
func (le *LayoutEngine) computeFlexMinMax(
	node *html.Node,
	constraint *ConstraintSpace,
	style *css.Style,
) MinMaxSizes {
	if width, ok := style.GetLength("width"); ok && width >= 0 {
		padding := style.GetPadding()
		border := style.GetBorderWidth()
		totalWidth := width + padding.Left + padding.Right + border.Left + border.Right
		return MinMaxSizes{
			MinContentSize: totalWidth,
			MaxContentSize: totalWidth,
		}
	}

	flexDir := style.GetFlexDirection()
	isRow := flexDir == css.FlexDirectionRow || flexDir == css.FlexDirectionRowReverse

	computedStyles := le.computeStylesForTree(node)

	var minContent, maxContent float64

	for _, child := range node.Children {
		var childStyle *css.Style
		if child.Type == html.TextNode {
			childStyle = style
		} else {
			childStyle = computedStyles[child]
		}
		if childStyle == nil || childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		childSizes := le.ComputeMinMaxSizes(child, constraint, childStyle)

		if isRow {
			if childSizes.MinContentSize > minContent {
				minContent = childSizes.MinContentSize
			}
			maxContent += childSizes.MaxContentSize
		} else {
			if childSizes.MinContentSize > minContent {
				minContent = childSizes.MinContentSize
			}
			if childSizes.MaxContentSize > maxContent {
				maxContent = childSizes.MaxContentSize
			}
		}
	}

	padding := style.GetPadding()
	border := style.GetBorderWidth()
	minContent += padding.Left + padding.Right + border.Left + border.Right
	maxContent += padding.Left + padding.Right + border.Left + border.Right

	return MinMaxSizes{
		MinContentSize: minContent,
		MaxContentSize: maxContent,
	}
}

func (le *LayoutEngine) computeStylesForTree(root *html.Node) map[*html.Node]*css.Style {
	return le.computeStylesForTreeWithParent(root, nil)
}

// computeStylesForTreeWithParent computes styles for a subtree, propagating
// inherited CSS properties (font-family, font-size, etc.) from parentStyle
// down through the tree. Without this, standalone ComputeStyle calls miss
// inherited properties because they don't walk the DOM parent chain.
func (le *LayoutEngine) computeStylesForTreeWithParent(root *html.Node, parentStyle *css.Style) map[*html.Node]*css.Style {
	styles := make(map[*html.Node]*css.Style)

	var traverse func(*html.Node, *css.Style)
	traverse = func(node *html.Node, inherited *css.Style) {
		if node == nil {
			return
		}
		style := css.ComputeStyle(node, le.stylesheets, le.viewport.width, le.viewport.height, nil)
		if inherited != nil {
			css.ApplyInheritedFrom(style, inherited)
		}
		styles[node] = style
		for _, child := range node.Children {
			traverse(child, style)
		}
	}

	traverse(root, parentStyle)
	return styles
}

// ComputeIntrinsicSizes computes intrinsic sizes for a node.
func (le *LayoutEngine) ComputeIntrinsicSizes(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	if node == nil {
		return IntrinsicSizes{}
	}

	if node.Type == html.TextNode {
		// For white-space: pre/pre-wrap, use RawText to preserve whitespace
		textContent := node.Text
		ws := style.GetWhiteSpace()
		if (ws == css.WhiteSpacePre || ws == css.WhiteSpacePreWrap) && node.RawText != "" {
			textContent = node.RawText
		}
		return le.computeTextIntrinsicSizes(textContent, style)
	}

	if node.Type != html.ElementNode {
		return IntrinsicSizes{}
	}

	if node.TagName == "img" {
		return le.computeImageIntrinsicSizes(node, style)
	}

	// Replaced elements (canvas, video, etc.) use HTML width/height attributes
	if isReplacedElementTag(node.TagName) {
		return le.computeReplacedIntrinsicSizes(node, style)
	}

	display := style.GetDisplay()

	if display == css.DisplayNone {
		return IntrinsicSizes{}
	}

	padding := style.GetPadding()
	border := style.GetBorderWidth()
	horizontalExtra := padding.Left + padding.Right + border.Left + border.Right

	// Flex containers: compute intrinsic sizes from flex items
	if display == css.DisplayFlex || display == css.DisplayInlineFlex {
		return le.computeFlexIntrinsicSizes(node, style, computedStyles, horizontalExtra)
	}

	if display == css.DisplayInline {
		return le.computeInlineIntrinsicSizes(node, style, computedStyles, horizontalExtra)
	}

	return le.computeBlockIntrinsicSizes(node, style, computedStyles, horizontalExtra)
}

// isReplacedElementTag returns true for replaced elements (not img).
func isReplacedElementTag(tagName string) bool {
	switch tagName {
	case "canvas", "video", "iframe", "embed", "object":
		return true
	}
	return false
}

// computeReplacedIntrinsicSizes computes intrinsic sizes for replaced elements.
func (le *LayoutEngine) computeReplacedIntrinsicSizes(node *html.Node, style *css.Style) IntrinsicSizes {
	if cssW, ok := style.GetLength("width"); ok && cssW > 0 {
		padding := style.GetPadding()
		border := style.GetBorderWidth()
		total := cssW + padding.Left + padding.Right + border.Left + border.Right
		return IntrinsicSizes{
			MinContent: total,
			MaxContent: total,
			Preferred:  total,
		}
	}

	var width float64
	if widthAttr, ok := node.GetAttribute("width"); ok {
		if w, err := strconv.ParseFloat(widthAttr, 64); err == nil {
			width = w
		}
	}

	padding := style.GetPadding()
	border := style.GetBorderWidth()
	total := width + padding.Left + padding.Right + border.Left + border.Right

	return IntrinsicSizes{
		MinContent: total,
		MaxContent: total,
		Preferred:  total,
	}
}

func (le *LayoutEngine) computeTextIntrinsicSizes(textContent string, style *css.Style) IntrinsicSizes {
	if textContent == "" {
		return IntrinsicSizes{}
	}

	fontSize := style.GetFontSize()
	isBold := style.GetFontWeight() == css.FontWeightBold
	isItalic := style.GetFontStyle() == css.FontStyleItalic
	isMono := style.IsMonospaceFamily()
	isAhem := style.IsAhemFamily()

	// For white-space: pre/pre-wrap, whitespace is preserved and affects sizing.
	ws := style.GetWhiteSpace()
	if ws == css.WhiteSpacePre || ws == css.WhiteSpacePreWrap {
		maxContent, _ := text.MeasureTextWithStyle(textContent, fontSize, isBold, isItalic, isMono, isAhem)
		return IntrinsicSizes{
			MinContent: maxContent,
			MaxContent: maxContent,
			Preferred:  maxContent,
		}
	}

	words := strings.Fields(textContent)
	collapsed := strings.Join(words, " ")
	maxContent, _ := text.MeasureTextWithStyle(collapsed, fontSize, isBold, isItalic, isMono, isAhem)

	minContent := 0.0
	for _, word := range words {
		wordWidth, _ := text.MeasureTextWithStyle(word, fontSize, isBold, isItalic, isMono, isAhem)
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

func (le *LayoutEngine) computeImageIntrinsicSizes(node *html.Node, style *css.Style) IntrinsicSizes {
	src, _ := node.GetAttribute("src")
	if src == "" {
		return IntrinsicSizes{}
	}

	var imgWidth float64
	if w, _, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil {
		imgWidth = float64(w)
	}

	if cssW, ok := style.GetLength("width"); ok && cssW > 0 {
		imgWidth = cssW
	}

	return IntrinsicSizes{
		MinContent: imgWidth,
		MaxContent: imgWidth,
		Preferred:  imgWidth,
	}
}

func (le *LayoutEngine) computeInlineIntrinsicSizes(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style, horizontalExtra float64) IntrinsicSizes {
	var minContent, maxContent float64

	for _, child := range node.Children {
		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = style
		}

		childSizes := le.ComputeIntrinsicSizes(child, childStyle, computedStyles)

		if childSizes.MinContent > minContent {
			minContent = childSizes.MinContent
		}
		maxContent += childSizes.MaxContent
	}

	return IntrinsicSizes{
		MinContent: minContent + horizontalExtra,
		MaxContent: maxContent + horizontalExtra,
		Preferred:  maxContent + horizontalExtra,
	}
}

// computeFlexIntrinsicSizes computes intrinsic sizes for flex containers.
func (le *LayoutEngine) computeFlexIntrinsicSizes(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style, horizontalExtra float64) IntrinsicSizes {
	if width, ok := style.GetLength("width"); ok && width > 0 {
		return IntrinsicSizes{
			MinContent: width + horizontalExtra,
			MaxContent: width + horizontalExtra,
			Preferred:  width + horizontalExtra,
		}
	}

	direction := style.GetFlexDirection()
	isRow := direction == css.FlexDirectionRow || direction == css.FlexDirectionRowReverse

	var minContent, maxContent float64

	gap := 0.0
	if gapVal, ok := style.Get("gap"); ok {
		if g, ok2 := css.ParseLength(gapVal); ok2 {
			gap = g
		}
	}
	if gapVal, ok := style.Get("column-gap"); ok && isRow {
		if g, ok2 := css.ParseLength(gapVal); ok2 {
			gap = g
		}
	}
	if gapVal, ok := style.Get("row-gap"); ok && !isRow {
		if g, ok2 := css.ParseLength(gapVal); ok2 {
			gap = g
		}
	}

	childCount := 0
	for _, child := range node.Children {
		childStyle := computedStyles[child]
		if childStyle == nil {
			if child.Type == html.TextNode {
				childStyle = style
			} else {
				childStyle = css.NewStyle()
			}
		}
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}
		if child.Type == html.TextNode && strings.TrimSpace(child.Text) == "" {
			continue
		}

		childSizes := le.ComputeIntrinsicSizes(child, childStyle, computedStyles)

		childMargin := childStyle.GetMargin()
		childOuter := childSizes.MaxContent + childMargin.Left + childMargin.Right
		childMinOuter := childSizes.MinContent + childMargin.Left + childMargin.Right

		if isRow {
			maxContent += childOuter
			if childMinOuter > minContent {
				minContent = childMinOuter
			}
		} else {
			if childOuter > maxContent {
				maxContent = childOuter
			}
			if childMinOuter > minContent {
				minContent = childMinOuter
			}
		}
		childCount++
	}

	if childCount > 1 && gap > 0 {
		if isRow {
			maxContent += gap * float64(childCount-1)
		}
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

	if width, ok := style.GetLength("width"); ok && width > 0 {
		return IntrinsicSizes{
			MinContent: width + horizontalExtra,
			MaxContent: width + horizontalExtra,
			Preferred:  width + horizontalExtra,
		}
	}

	var inlineMinContent, inlineMaxContent float64

	for _, child := range node.Children {
		childStyle := computedStyles[child]
		if childStyle == nil {
			if child.Type == html.TextNode {
				// Text nodes inherit style from their parent element
				childStyle = style
			} else {
				childStyle = css.NewStyle()
			}
		}

		childSizes := le.ComputeIntrinsicSizes(child, childStyle, computedStyles)
		childDisplay := childStyle.GetDisplay()

		// Text nodes are always inline-level (never block-level)
		if child.Type == html.TextNode {
			childDisplay = css.DisplayInline
		}

		// Floated children participate in inline flow for max-content sizing
		// (CSS Sizing Level 3 section 4.1: floats are laid out side by side).
		isFloat := childStyle.GetFloat() != css.FloatNone
		isBlockLevel := (childDisplay == css.DisplayBlock || childDisplay == css.DisplayFlowRoot || childDisplay == css.DisplayListItem) && !isFloat

		if isBlockLevel {
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
			// Inline child (including floats): accumulate in current run
			if childSizes.MinContent > inlineMinContent {
				inlineMinContent = childSizes.MinContent
			}
			inlineMaxContent += childSizes.MaxContent
		}
	}

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

func (le *LayoutEngine) computeGridItemMaxContentWidth(node *html.Node, nodeStyle *css.Style, computedStyles map[*html.Node]*css.Style) float64 {
	if node == nil || nodeStyle == nil {
		return 0
	}
	return le.sumInlineMaxContentAdvance(node.Children, nodeStyle, computedStyles)
}

func (le *LayoutEngine) sumInlineMaxContentAdvance(children []*html.Node, parentStyle *css.Style, computedStyles map[*html.Node]*css.Style) float64 {
	type contribInfo struct {
		width      float64
		whitespace bool
	}
	var contribs []contribInfo
	seenContent := false

	for _, child := range children {
		if child.Type == html.TextNode {
			textContent := child.Text
			if textContent == "" {
				continue
			}
			isAllWS := strings.TrimSpace(textContent) == ""
			if isAllWS && !seenContent {
				continue
			}
			if !seenContent {
				textContent = strings.TrimLeft(textContent, " \t\n\r")
			}
			if textContent == "" {
				continue
			}
			fontSize := parentStyle.GetFontSize()
			isBold := parentStyle.GetFontWeight() == css.FontWeightBold
			isItalic := parentStyle.GetFontStyle() == css.FontStyleItalic
			isMono := parentStyle.IsMonospaceFamily()
			isAhem := parentStyle.IsAhemFamily()
			w, _ := text.MeasureTextWithStyle(textContent, fontSize, isBold, isItalic, isMono, isAhem)
			isWSContrib := strings.TrimSpace(textContent) == ""
			contribs = append(contribs, contribInfo{w, isWSContrib})
			if !isWSContrib {
				seenContent = true
			}
		} else if child.Type == html.ElementNode {
			childStyle := computedStyles[child]
			if childStyle == nil {
				continue
			}
			if childStyle.GetDisplay() == css.DisplayNone {
				continue
			}
			margin := childStyle.GetMargin()
			padding := childStyle.GetPadding()
			border := childStyle.GetBorderWidth()
			childContent := le.sumInlineMaxContentAdvance(child.Children, childStyle, computedStyles)
			w := margin.Left + padding.Left + border.Left +
				childContent +
				border.Right + padding.Right + margin.Right
			contribs = append(contribs, contribInfo{w, false})
			seenContent = true
		}
	}

	for len(contribs) > 0 && contribs[len(contribs)-1].whitespace {
		contribs = contribs[:len(contribs)-1]
	}

	var total float64
	for _, c := range contribs {
		total += c.width
	}
	return total
}

// ============================================================================
// Layout Mode Implementations
// ============================================================================

// ComputeIntrinsicSizes for BlockLayoutMode
func (m *BlockLayoutMode) ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	return le.ComputeIntrinsicSizes(node, style, computedStyles)
}

// LayoutChildren for BlockLayoutMode
func (m *BlockLayoutMode) LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box {
	return nil
}

// ComputeIntrinsicSizes for InlineLayoutMode
