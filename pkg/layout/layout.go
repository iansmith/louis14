package layout

import (
	"fmt"     // Phase 23: For list marker formatting
	"strings" // For url() parsing in pseudo-element content

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/text"
)

type Box struct {
	Node          *html.Node
	Style         *css.Style
	X             float64
	Y             float64
	Width         float64 // Content width
	Height        float64 // Content height
	Margin        css.BoxEdge
	Padding       css.BoxEdge
	Border        css.BoxEdge
	Children      []*Box          // Phase 2: Nested boxes
	Parent        *Box            // Phase 4: Parent box for containing block
	Position      css.PositionType // Phase 4: Position type
	ZIndex        int             // Phase 4: Stacking order
	ImagePath     string          // Phase 8: Image source path for img elements
	PseudoContent string          // Phase 11: Content for pseudo-elements
}

type LayoutEngine struct {
	viewport struct {
		width  float64
		height float64
	}
	scrollY        float64    // Scroll offset for fixed positioning (viewport-relative)
	absoluteBoxes  []*Box     // Phase 4: Track absolutely positioned boxes
	floats         []FloatInfo // Phase 5: Track floated elements
	floatBaseStack []int       // Stack of float base indices for BFC boundaries
	floatBase      int         // Current BFC float base index
	stylesheets    []*css.Stylesheet // Phase 11: Store stylesheets for pseudo-elements
	imageFetcher   images.ImageFetcher // Optional fetcher for network images
}

// Phase 5: FloatInfo tracks information about floated elements
type FloatInfo struct {
	Box  *Box
	Side css.FloatType
	Y    float64 // Y position where float starts
}

// Phase 7: InlineContext tracks the current inline layout state
type InlineContext struct {
	LineX      float64 // Current X position on the line
	LineY      float64 // Current line Y position
	LineHeight float64 // Height of current line
	LineBoxes  []*Box  // Boxes on current line
}

// Phase 9: TableCell tracks a cell in a table
type TableCell struct {
	Box     *Box
	RowSpan int
	ColSpan int
	RowIdx  int
	ColIdx  int
}

// Phase 9: TableRow tracks a row in a table
type TableRow struct {
	Box   *Box
	Cells []*TableCell
}

// Phase 9: TableInfo tracks table layout information
type TableInfo struct {
	Rows          []*TableRow
	NumCols       int
	ColumnWidths  []float64
	RowHeights    []float64
	BorderSpacing float64
	BorderCollapse css.BorderCollapse
}

// Phase 10: FlexItem tracks a flex item
type FlexItem struct {
	Box            *Box
	FlexGrow       float64
	FlexShrink     float64
	FlexBasis      float64
	MainSize       float64  // Size along main axis
	CrossSize      float64  // Size along cross axis
	MainPos        float64  // Position along main axis
	CrossPos       float64  // Position along cross axis
	Order          int
}

// Phase 10: FlexLine tracks a line of flex items (for wrapping)
type FlexLine struct {
	Items          []*FlexItem
	MainSize       float64  // Total size of items along main axis
	CrossSize      float64  // Maximum cross size in this line
}

// Phase 10: FlexContainer tracks flex container layout information
type FlexContainer struct {
	Lines          []*FlexLine
	Direction      css.FlexDirection
	Wrap           css.FlexWrap
	JustifyContent css.JustifyContent
	AlignItems     css.AlignItems
	AlignContent   css.AlignContent
	MainAxisSize   float64
	CrossAxisSize  float64
	IsRow          bool  // true if direction is row/row-reverse
}

func NewLayoutEngine(viewportWidth, viewportHeight float64) *LayoutEngine {
	le := &LayoutEngine{}
	le.viewport.width = viewportWidth
	le.viewport.height = viewportHeight
	return le
}

// SetScrollY sets the vertical scroll offset for fixed positioning.
// Fixed elements are positioned relative to viewport + scrollY.
func (le *LayoutEngine) SetScrollY(scrollY float64) {
	le.scrollY = scrollY
}

// SetImageFetcher sets the image fetcher used to load network images during layout.
func (le *LayoutEngine) SetImageFetcher(fetcher images.ImageFetcher) {
	le.imageFetcher = fetcher
}

// GetScrollY returns the current vertical scroll offset.
func (le *LayoutEngine) GetScrollY() float64 {
	return le.scrollY
}

func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
	// Phase 3: Compute styles from stylesheets
	// Phase 22: Pass viewport dimensions for media query evaluation
	computedStyles := css.ApplyStylesToDocument(doc, le.viewport.width, le.viewport.height)

	// Phase 11: Parse and store stylesheets for pseudo-element styling
	le.stylesheets = make([]*css.Stylesheet, 0)
	for _, cssText := range doc.Stylesheets {
		if stylesheet, err := css.ParseStylesheet(cssText); err == nil {
			le.stylesheets = append(le.stylesheets, stylesheet)
		}
	}

	// Phase 2: Recursively layout the tree starting from root's children
	boxes := make([]*Box, 0)
	y := 0.0

	// Phase 4: Track absolutely positioned boxes separately
	le.absoluteBoxes = make([]*Box, 0)

	// Phase 5: Initialize floats tracking
	le.floats = make([]FloatInfo, 0)

	var prevBox *Box // Track previous sibling for margin collapsing
	for _, node := range doc.Root.Children {
		if node.Type == html.ElementNode {
			box := le.layoutNode(node, 0, y, le.viewport.width, computedStyles, nil)
			// Phase 7: Skip elements with display: none (layoutNode returns nil)
			if box == nil {
				continue
			}
			boxes = append(boxes, box)

			// Phase 4 & 5: Only advance Y if element is in normal flow (not absolutely positioned or floated)
			floatType := box.Style.GetFloat()
			if box.Position != css.PositionAbsolute && box.Position != css.PositionFixed && floatType == css.FloatNone {
				// Margin collapsing between adjacent siblings
				if prevBox != nil && shouldCollapseMargins(prevBox) && shouldCollapseMargins(box) {
					collapsed := collapseMargins(prevBox.Margin.Bottom, box.Margin.Top)
					// We already advanced by prevBox's full total height (including prevBox.Margin.Bottom)
					// and layoutNode already added box.Margin.Top to box.Y.
					// We need to pull back by the non-collapsed portion.
					adjustment := prevBox.Margin.Bottom + box.Margin.Top - collapsed
					box.Y -= adjustment
					le.adjustChildrenY(box, -adjustment)
				}
				y = box.Y + box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom + box.Margin.Bottom
				prevBox = box
			}
		}
	}

	// Phase 4: Absolutely positioned boxes are already in the tree as children
	// of their containing blocks, so no need to add them separately.

	return boxes
}

// layoutNode recursively layouts a node and its children
func (le *LayoutEngine) layoutNode(node *html.Node, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style, parent *Box) *Box {
	// Phase 3: Use computed styles from cascade
	style := computedStyles[node]
	if style == nil {
		style = css.NewStyle()
	}

	// Phase 7: Check display mode early
	display := style.GetDisplay()
	if display == css.DisplayNone {
		return nil
	}

	// Phase 8: Check if this is an img element
	isImage := node.TagName == "img"
	// Phase 24: Check if this is an object element with a loadable image
	isObjectImage := false
	if node.TagName == "object" {
		if data, ok := node.GetAttribute("data"); ok {
			if _, _, err := images.GetImageDimensionsWithFetcher(data, le.imageFetcher); err == nil {
				isObjectImage = true
			}
		}
	}
	var imageWidth, imageHeight int
	var imagePath string
	if isImage {
		// Get image source
		if src, ok := node.GetAttribute("src"); ok {
			imagePath = src
			// Try to load image to get natural dimensions
			if w, h, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil {
				imageWidth = w
				imageHeight = h
			}
		}
		// Images default to inline-block display
		if display == css.DisplayBlock {
			display = css.DisplayInlineBlock
		}
	} else if isObjectImage {
		// Object element with loadable image - treat like img
		if data, ok := node.GetAttribute("data"); ok {
			imagePath = data
			if w, h, err := images.GetImageDimensionsWithFetcher(data, le.imageFetcher); err == nil {
				imageWidth = w
				imageHeight = h
			}
		}
		isImage = true
		if display == css.DisplayBlock {
			display = css.DisplayInlineBlock
		}
	}

	// Phase 5: Check for float early to determine width calculation
	floatType := style.GetFloat()

	// CSS 2.1 §9.7: Relationships between display, position, and float
	// Floated or absolutely positioned inline elements compute to block display
	if display == css.DisplayInline {
		pos := style.GetPosition()
		if floatType != css.FloatNone || pos == css.PositionAbsolute || pos == css.PositionFixed {
			display = css.DisplayBlock
		}
	}

	// Get box model values
	margin := style.GetMargin()
	padding := style.GetPadding()
	border := style.GetBorderWidth()

	// Phase 7 Enhancement: Inline elements ignore vertical margins and padding
	if display == css.DisplayInline {
		margin.Top = 0
		margin.Bottom = 0
		padding.Top = 0
		padding.Bottom = 0
	}

	// Apply margin offset
	x += margin.Left
	y += margin.Top

	// Calculate content width
	var contentWidth float64
	hasExplicitWidth := false

	// Phase 8: Images use image dimensions or explicit dimensions
	if isImage {
		if w, ok := style.GetLength("width"); ok {
			contentWidth = w
			hasExplicitWidth = true
		} else if widthAttr, ok := node.GetAttribute("width"); ok {
			// Parse width attribute
			if w, ok := css.ParseLength(widthAttr); ok {
				contentWidth = w
				hasExplicitWidth = true
			}
		} else if imageWidth > 0 {
			// Use natural image width
			contentWidth = float64(imageWidth)
			hasExplicitWidth = true
		} else {
			// Fallback for missing/broken images
			contentWidth = 100
			hasExplicitWidth = true
		}
	} else if display == css.DisplayInline {
		// Phase 7 Enhancement: Inline elements always shrink-wrap (ignore width property)
		contentWidth = 0
		hasExplicitWidth = false
	} else if w, ok := style.GetLength("width"); ok {
		contentWidth = w
		hasExplicitWidth = true
	} else if pct, ok := style.GetPercentage("width"); ok {
		// Percentage width resolved against containing block
		cbWidth := availableWidth
		if style.GetPosition() == css.PositionFixed {
			cbWidth = le.viewport.width
		}
		contentWidth = cbWidth * pct / 100
		hasExplicitWidth = true
	} else if style.GetPosition() == css.PositionAbsolute || style.GetPosition() == css.PositionFixed {
		// Absolutely positioned elements without explicit width shrink-wrap
		contentWidth = 0
	} else if floatType != css.FloatNone {
		// CSS 2.1 §10.3.5: Floated elements without explicit width use shrink-to-fit
		contentWidth = 0
	} else if display == css.DisplayTable {
		// CSS 2.1 §17.5.2: Tables without explicit width use shrink-to-fit
		contentWidth = 0
	} else {
		// Default to available width minus horizontal margin, padding, border
		contentWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
	}

	// Calculate content height
	var contentHeight float64
	// Phase 8: Images use image dimensions or explicit dimensions
	if isImage {
		if h, ok := style.GetLength("height"); ok {
			contentHeight = h
		} else if heightAttr, ok := node.GetAttribute("height"); ok {
			// Parse height attribute
			if h, ok := css.ParseLength(heightAttr); ok {
				contentHeight = h
			}
		} else if imageHeight > 0 {
			// Use natural image height, maintaining aspect ratio if width was specified
			if hasExplicitWidth && imageWidth > 0 {
				// Scale height to maintain aspect ratio
				contentHeight = contentWidth * float64(imageHeight) / float64(imageWidth)
			} else {
				contentHeight = float64(imageHeight)
			}
		} else {
			// Fallback for missing/broken images
			contentHeight = 100
		}
	} else if display == css.DisplayInline {
		// Phase 7 Enhancement: Inline elements always shrink-wrap (ignore height property)
		contentHeight = 0
	} else if h, ok := style.GetLength("height"); ok {
		contentHeight = h
	} else {
		contentHeight = 0 // Auto height - will be calculated from children
	}

	// Apply min/max width constraints
	if minWidth, ok := style.GetLength("min-width"); ok {
		if contentWidth < minWidth {
			contentWidth = minWidth
		}
	}
	if maxWidth, ok := style.GetLength("max-width"); ok {
		if contentWidth > maxWidth {
			contentWidth = maxWidth
		}
	}

	// Apply min/max height constraints (min-height overrides max-height per CSS 2.1 10.7)
	if maxHeight, ok := style.GetLength("max-height"); ok {
		if contentHeight > maxHeight {
			contentHeight = maxHeight
		}
	}
	if minHeight, ok := style.GetLength("min-height"); ok {
		if contentHeight < minHeight {
			contentHeight = minHeight
		}
	}

	// Phase 13: Handle margin: auto for horizontal centering
	// Only center if both left and right margins are auto
	if margin.AutoLeft && margin.AutoRight {
		// For block-level elements with auto margins, center them
		// Calculate total width including padding and border
		totalWidth := contentWidth + padding.Left + padding.Right + border.Left + border.Right
		// Center within available width
		if totalWidth < availableWidth {
			centerOffset := (availableWidth - totalWidth) / 2
			x = x + centerOffset
		}
	}

	// Phase 4: Get positioning information
	position := style.GetPosition()
	zindex := style.GetZIndex()

	// Phase 5: Check for clear property
	clearType := style.GetClear()

	// Phase 5: Handle clear property - move Y down past floats
	if clearType != css.ClearNone {
		y = le.getClearY(clearType, y)
	}

	box := &Box{
		Node:      node,
		Style:     style,
		X:         x,
		Y:         y,
		Width:     contentWidth,
		Height:    contentHeight,
		Margin:    margin,
		Padding:   padding,
		Border:    border,
		Children:  make([]*Box, 0),
		Position:  position,
		ZIndex:    zindex,
		Parent:    parent,
		ImagePath: imagePath, // Phase 8: Store image path for rendering
	}

	// Phase 5: Float positioning will be done AFTER children are laid out
	// (to support shrink-wrapping and float drop)

	// Phase 4: Handle positioning
	if position == css.PositionAbsolute || position == css.PositionFixed {
		// Absolutely positioned elements - positioning applied after children layout
		le.absoluteBoxes = append(le.absoluteBoxes, box)
	} else if position == css.PositionRelative {
		// Relative positioning: offset from normal position
		offset := style.GetPositionOffset()
		if offset.HasTop {
			box.Y += offset.Top
		} else if offset.HasBottom {
			box.Y -= offset.Bottom
		}
		if offset.HasLeft {
			box.X += offset.Left
		} else if offset.HasRight {
			box.X -= offset.Right
		}
	}

	// Phase 9: Handle table layout specially
	if display == css.DisplayTable {
		le.layoutTable(box, x, y, availableWidth, computedStyles)
		return box
	}

	// Phase 10: Handle flexbox layout specially
	if display == css.DisplayFlex || display == css.DisplayInlineFlex {
		le.layoutFlex(box, x, y, availableWidth, computedStyles)
		return box
	}

	// Phase 15: Handle grid layout specially
	if display == css.DisplayGrid || display == css.DisplayInlineGrid {
		return le.layoutGridContainer(node, x, y, availableWidth, style, computedStyles, parent)
	}

	// Check if this element creates a new block formatting context (BFC)
	createsBFC := false
	if style.GetOverflow() != css.OverflowVisible || floatType != css.FloatNone ||
		position == css.PositionAbsolute || position == css.PositionFixed ||
		display == css.DisplayInlineBlock {
		createsBFC = true
	}
	if createsBFC {
		le.floatBaseStack = append(le.floatBaseStack, le.floatBase)
		le.floatBase = len(le.floats)
	}

	// Phase 2: Recursively layout children
	// Use box.X/Y which include relative positioning offset
	childY := box.Y + border.Top + padding.Top
	childAvailableWidth := contentWidth

	// Track previous block child for margin collapsing between siblings
	var prevBlockChild *Box
	var pendingMargins []float64 // margins from collapse-through elements

	// Phase 7: Track inline layout context
	inlineCtx := &InlineContext{
		LineX:      box.X + border.Left + padding.Left,
		LineY:      childY,
		LineHeight: 0,
		LineBoxes:  make([]*Box, 0),
	}

	// Phase 11: Generate ::before pseudo-element if it has content
	beforeBox := le.generatePseudoElement(node, "before", inlineCtx.LineX, inlineCtx.LineY, childAvailableWidth, computedStyles, box)
	if beforeBox != nil {
		box.Children = append(box.Children, beforeBox)
		// Update inline context for subsequent children
		beforeDisplay := beforeBox.Style.GetDisplay()
		if beforeDisplay == css.DisplayBlock {
			inlineCtx.LineY += le.getTotalHeight(beforeBox)
			inlineCtx.LineX = box.X + border.Left + padding.Left
		} else {
			inlineCtx.LineX += le.getTotalWidth(beforeBox)
			if beforeBox.Height > inlineCtx.LineHeight {
				inlineCtx.LineHeight = beforeBox.Height
			}
		}
	}

	// Phase 23: Generate list marker for list-item elements
	if display == css.DisplayListItem {
		markerBox := le.generateListMarker(node, style, x, inlineCtx.LineY, box)
		if markerBox != nil {
			box.Children = append(box.Children, markerBox)
		}
	}

	// Phase 24: Skip children for object elements that successfully loaded an image
	skipChildren := isObjectImage

	for _, child := range node.Children {
		if skipChildren {
			break
		}
		if child.Type == html.ElementNode {
			// Get child's computed style to check display mode
			childStyle := computedStyles[child]
			if childStyle == nil {
				childStyle = css.NewStyle()
			}
			childDisplay := childStyle.GetDisplay()

			// Layout the child
			childBox := le.layoutNode(
				child,
				inlineCtx.LineX,
				inlineCtx.LineY,
				childAvailableWidth,
				computedStyles,
				box,  // Phase 4: Pass parent
			)

			// Phase 7: Skip elements with display: none (layoutNode returns nil)
			if childBox != nil {
				// Phase 7: Handle inline and inline-block elements
				// Skip inline positioning for floated elements (they are positioned by float logic)
				childIsFloated := childStyle != nil && childStyle.GetFloat() != css.FloatNone
				if (childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock) && childBox.Position == css.PositionStatic && !childIsFloated {
					childTotalWidth := le.getTotalWidth(childBox)

					// Check if child fits on current line
					if inlineCtx.LineX + childTotalWidth > box.X + border.Left + padding.Left + childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to next line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = box.X + border.Left + padding.Left
						inlineCtx.LineHeight = 0
						inlineCtx.LineBoxes = make([]*Box, 0)

						// Reposition child at start of new line
						childBox.X = inlineCtx.LineX
						childBox.Y = inlineCtx.LineY
					}

					// Add to current line
					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, childBox)
					childHeight := le.getTotalHeight(childBox)
					if childHeight > inlineCtx.LineHeight {
						inlineCtx.LineHeight = childHeight
					}
					// CSS 2.1 §10.8.1: The "strut" ensures line box height is at least
					// the block container's line-height
					strutHeight := style.GetLineHeight()
					if strutHeight > inlineCtx.LineHeight {
						inlineCtx.LineHeight = strutHeight
					}

					// Advance X for next inline-block element
					inlineCtx.LineX += childTotalWidth

					box.Children = append(box.Children, childBox)

					// Phase 7 Enhancement: Apply vertical-align to inline element
					le.applyVerticalAlign(childBox, inlineCtx.LineY, inlineCtx.LineHeight)
				} else {
					// Block element or other display mode
					// Finish current inline line (apply strut for line box height)
					if len(inlineCtx.LineBoxes) > 0 {
						strutHeight := style.GetLineHeight()
						if strutHeight > inlineCtx.LineHeight {
							inlineCtx.LineHeight = strutHeight
						}
						childY = inlineCtx.LineY + inlineCtx.LineHeight
						inlineCtx.LineBoxes = make([]*Box, 0)
						inlineCtx.LineHeight = 0
					} else {
						childY = inlineCtx.LineY
					}

					// Update child position for block element (skip absolute/fixed - positioned later, skip floats - positioned by float logic)
					childFloatTypePos := css.FloatNone
					if childStyle != nil { childFloatTypePos = childStyle.GetFloat() }
					if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && childFloatTypePos == css.FloatNone {
						// For position:relative, preserve the offset that was already applied
						relativeOffsetY := 0.0
						if childBox.Position == css.PositionRelative && childStyle != nil {
							offset := childStyle.GetPositionOffset()
							if offset.HasTop {
								relativeOffsetY = offset.Top
							} else if offset.HasBottom {
								relativeOffsetY = -offset.Bottom
							}
						}
						if childBox.Margin.AutoLeft && childBox.Margin.AutoRight {
							childTotalW := childBox.Width + childBox.Padding.Left + childBox.Padding.Right + childBox.Border.Left + childBox.Border.Right
							parentContentStart := box.X + border.Left + padding.Left
							centerOff := (childAvailableWidth - childTotalW) / 2
							if centerOff < 0 { centerOff = 0 }
							childBox.X = parentContentStart + centerOff
						} else {
							childBox.X = box.X + border.Left + padding.Left + childBox.Margin.Left
						}
						childBox.Y = childY + childBox.Margin.Top + relativeOffsetY
					}

					box.Children = append(box.Children, childBox)

					// Advance Y for block elements
					childFloatType := childBox.Style.GetFloat()
					if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && childFloatType == css.FloatNone {
						// Margin-collapse-through: collect margins from collapse-through elements
						// and combine them with the next non-collapse-through sibling's margins.
						if isCollapseThrough(childBox) {
							// Add this element's margins (and children's) to pending list
							pendingMargins = append(pendingMargins, childBox.Margin.Top, childBox.Margin.Bottom)
							collectCollapseThroughChildMargins(childBox, &pendingMargins)
							// Position at childY (zero-height, no visual impact)
							childBox.Y = childY
							// Don't advance childY, don't set prevBlockChild
						} else {
							// Normal margin collapsing between adjacent block siblings
							if prevBlockChild != nil && shouldCollapseMargins(prevBlockChild) && shouldCollapseMargins(childBox) {
								// Collect all margins: prev bottom, any pending from collapse-through, current top
								allMargins := []float64{prevBlockChild.Margin.Bottom}
								allMargins = append(allMargins, pendingMargins...)
								allMargins = append(allMargins, childBox.Margin.Top)
								// Collapse all together
								var maxPos, minNeg float64
								for _, m := range allMargins {
									if m > maxPos { maxPos = m }
									if m < minNeg { minNeg = m }
								}
								collapsed := maxPos + minNeg
								// Only real margins used space; pending margins were from zero-height elements
								totalUsed := prevBlockChild.Margin.Bottom + childBox.Margin.Top
								adjustment := totalUsed - collapsed
								childBox.Y -= adjustment
								le.adjustChildrenY(childBox, -adjustment)
							} else if len(pendingMargins) > 0 && shouldCollapseMargins(childBox) {
								// No prev sibling but pending margins from collapse-through
								allMargins := append(pendingMargins, childBox.Margin.Top)
								var maxPos, minNeg float64
								for _, m := range allMargins {
									if m > maxPos { maxPos = m }
									if m < minNeg { minNeg = m }
								}
								collapsed := maxPos + minNeg
								totalUsed := childBox.Margin.Top
								adjustment := totalUsed - collapsed
								childBox.Y -= adjustment
								le.adjustChildrenY(childBox, -adjustment)
							}
							pendingMargins = nil
							// Apply clear property after margin collapsing
							if childBox.Style != nil {
								childClear := childBox.Style.GetClear()
								if childClear != css.ClearNone {
									clearY := le.getClearY(childClear, childBox.Y)
									if clearY > childBox.Y {
										delta := clearY - childBox.Y
										childBox.Y = clearY
										le.adjustChildrenY(childBox, delta)
									}
								}
							}
							childY = childBox.Y + childBox.Border.Top + childBox.Padding.Top + childBox.Height + childBox.Padding.Bottom + childBox.Border.Bottom + childBox.Margin.Bottom
							prevBlockChild = childBox
						}
					}

					// Reset inline context for next line
					inlineCtx.LineX = box.X + border.Left + padding.Left
					inlineCtx.LineY = childY
				}
			}
		} else if child.Type == html.TextNode {
			// Phase 6: Layout text nodes
			// Always use inline flow so text nodes participate in the inline
			// formatting context together with sibling inline elements (e.g. <em>).
			textBox := le.layoutTextNode(
				child,
				inlineCtx.LineX,
				inlineCtx.LineY,
				box.X+border.Left+padding.Left+childAvailableWidth-inlineCtx.LineX,
				style, // Text inherits parent's style
				box,
			)
			if textBox != nil {
				box.Children = append(box.Children, textBox)

				// For multi-line text containers, the inline context should
				// continue after the LAST line, not after the full container width.
				if len(textBox.Children) > 0 {
					// Multi-line text: advance to end of last line
					lastLine := textBox.Children[len(textBox.Children)-1]
					inlineCtx.LineY = lastLine.Y
					inlineCtx.LineX = lastLine.X + le.getTotalWidth(lastLine)
					inlineCtx.LineHeight = le.getTotalHeight(lastLine)
					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, textBox)
				} else {
					// Single-line text
					textWidth := le.getTotalWidth(textBox)
					textHeight := le.getTotalHeight(textBox)

					// Check if text fits on current line
					if inlineCtx.LineX+textWidth > box.X+border.Left+padding.Left+childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to new line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = box.X + border.Left + padding.Left
						inlineCtx.LineHeight = textHeight
						textBox.X = inlineCtx.LineX
						textBox.Y = inlineCtx.LineY
						inlineCtx.LineX += textWidth
					} else {
						// Fits on current line (or is the first item on the line)
						inlineCtx.LineX += textWidth
						if textHeight > inlineCtx.LineHeight {
							inlineCtx.LineHeight = textHeight
						}
					}

					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, textBox)
				}
			}
		}
	}

	// Phase 11: Generate ::after pseudo-element if it has content
	afterBox := le.generatePseudoElement(node, "after", inlineCtx.LineX, inlineCtx.LineY, childAvailableWidth, computedStyles, box)
	if afterBox != nil {
		box.Children = append(box.Children, afterBox)
	}

	// Apply text-align to inline children (only for block containers, not inline elements)
	if display != css.DisplayInline && display != css.DisplayInlineBlock {
		if textAlign, ok := style.Get("text-align"); ok && textAlign != "left" && textAlign != "" {
			le.applyTextAlign(box, textAlign, contentWidth)
		}
	}

	// Parent-child top margin collapsing
	// If parent has no border-top/padding-top, collapse with first block child's top margin
	if parentCanCollapseTopMargin(box) && shouldCollapseMargins(box) {
		// Find first in-flow block child
		var firstBlockChild *Box
		for _, ch := range box.Children {
			if ch.Style != nil && ch.Style.GetFloat() != css.FloatNone {
				continue
			}
			if ch.Position == css.PositionAbsolute || ch.Position == css.PositionFixed {
				continue
			}
			if ch.Style != nil {
				d := ch.Style.GetDisplay()
				if d == css.DisplayInline || d == css.DisplayInlineBlock {
					break // inline content separates margins
				}
			}
			firstBlockChild = ch
			break
		}
		if firstBlockChild != nil && shouldCollapseMargins(firstBlockChild) && firstBlockChild.Margin.Top > 0 {
			childMarginTop := firstBlockChild.Margin.Top
			// Pull all children up by the first child's top margin
			for _, ch := range box.Children {
				ch.Y -= childMarginTop
				le.adjustChildrenY(ch, -childMarginTop)
			}
			// Compute collapsed margin
			collapsed := collapseMargins(margin.Top, childMarginTop)
			marginDiff := collapsed - margin.Top
			box.Margin.Top = collapsed
			if marginDiff != 0 {
				box.Y += marginDiff
				for _, ch := range box.Children {
					ch.Y += marginDiff
					le.adjustChildrenY(ch, marginDiff)
				}
			}
		}
	}

	// If height is auto and we have children, adjust height to fit content
	if _, ok := style.GetLength("height"); !ok && len(box.Children) > 0 {
		// Calculate height based on maximum bottom edge of children (not sum)
		// This correctly handles overlapping children (like floats with blocks)
		parentContentTop := box.Y + box.Border.Top + box.Padding.Top
		maxBottom := 0.0

		// CSS 2.1 §8.3.1 / §10.6.3: Parent-child bottom margin collapsing.
		// When parent has no bottom border and no bottom padding (and auto height),
		// the last in-flow child's bottom margin collapses with the parent's bottom
		// margin, so it should NOT be included in the auto-height calculation.
		// Note: Margin collapsing does NOT apply to absolutely positioned elements,
		// which establish a new block formatting context (CSS 2.1 §9.4.1).
		parentChildBottomCollapse := box.Border.Bottom == 0 && box.Padding.Bottom == 0 &&
			position != css.PositionAbsolute && position != css.PositionFixed
		var lastInFlowChild *Box
		if parentChildBottomCollapse {
			for _, child := range box.Children {
				if child.Position != css.PositionAbsolute && child.Position != css.PositionFixed {
					lastInFlowChild = child
				}
			}
		}

		for _, child := range box.Children {
			if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
				continue
			}
			// Calculate child's bottom edge relative to parent content area
			// For position:relative children, use their normal flow position
			// (CSS 2.1 §10.6.3: relative offset doesn't affect parent height)
			childY := child.Y
			if child.Position == css.PositionRelative && child.Style != nil {
				offset := child.Style.GetPositionOffset()
				if offset.HasTop {
					childY -= offset.Top
				} else if offset.HasBottom {
					childY += offset.Bottom
				}
			}
			childRelativeY := childY - parentContentTop
			// Use height from child's border-top edge (child.Y) downward:
			// border + padding + content + padding + border + margin-bottom.
			// Don't include margin-top since child.Y already accounts for it.
			childMarginBottom := child.Margin.Bottom
			if parentChildBottomCollapse && child == lastInFlowChild {
				// Last child's margin-bottom collapses through the parent
				childMarginBottom = 0
			}
			childHeight := child.Border.Top + child.Padding.Top + child.Height +
				child.Padding.Bottom + child.Border.Bottom + childMarginBottom
			childBottom := childRelativeY + childHeight
			if childBottom > maxBottom {
				maxBottom = childBottom
			}
		}
		// CSS 2.1 §10.8.1: Account for trailing inline line box height (including strut)
		if len(inlineCtx.LineBoxes) > 0 {
			strutHeight := style.GetLineHeight()
			lineBoxHeight := inlineCtx.LineHeight
			if strutHeight > lineBoxHeight {
				lineBoxHeight = strutHeight
			}
			lineBottom := (inlineCtx.LineY - parentContentTop) + lineBoxHeight
			if lineBottom > maxBottom {
				maxBottom = lineBottom
			}
		}
		if maxBottom < 0 {
			maxBottom = 0
		}
		box.Height = maxBottom

		// CSS 2.1 §8.3.1: When parent-child bottom margin collapsing applies,
		// propagate the last child's bottom margin to the parent's bottom margin.
		// The collapsed margin is the combination of parent's and child's margins.
		if parentChildBottomCollapse && lastInFlowChild != nil && lastInFlowChild.Margin.Bottom != 0 {
			parentMB := box.Margin.Bottom
			childMB := lastInFlowChild.Margin.Bottom
			if parentMB >= 0 && childMB >= 0 {
				if childMB > parentMB {
					box.Margin.Bottom = childMB
				}
			} else if parentMB < 0 && childMB < 0 {
				if childMB < parentMB {
					box.Margin.Bottom = childMB
				}
			} else {
				box.Margin.Bottom = parentMB + childMB
			}
		}
	}

	// Re-apply min/max height constraints after auto-height calculation
	if maxHeight, ok := style.GetLength("max-height"); ok {
		if box.Height > maxHeight {
			box.Height = maxHeight
		}
	}
	if minHeight, ok := style.GetLength("min-height"); ok {
		if box.Height < minHeight {
			box.Height = minHeight
		}
	}

	// Phase 7 Enhancement: Inline elements always shrink-wrap to children
	if display == css.DisplayInline && len(box.Children) > 0 {
		// Calculate width from children (horizontal sum for inline flow)
		maxChildWidth := 0.0
		totalChildHeight := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			if childWidth > maxChildWidth {
				maxChildWidth = childWidth
			}
			childHeight := le.getTotalHeight(child)
			if childHeight > totalChildHeight {
				totalChildHeight = childHeight
			}
		}
		box.Width = maxChildWidth
		box.Height = totalChildHeight
	}

	// Phase 5 Enhancement: Float shrink-wrapping
	// If this is a float without explicit width, shrink-wrap to content
	if floatType != css.FloatNone && !hasExplicitWidth && len(box.Children) > 0 {
		maxChildWidth := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			if childWidth > maxChildWidth {
				maxChildWidth = childWidth
			}
		}
		if maxChildWidth > 0 {
			box.Width = maxChildWidth
		}
	}

	// Shrink-wrap absolutely positioned elements without explicit width
	if (position == css.PositionAbsolute || position == css.PositionFixed) && !hasExplicitWidth && len(box.Children) > 0 {
		maxChildWidth := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			if childWidth > maxChildWidth {
				maxChildWidth = childWidth
			}
		}
		if maxChildWidth > 0 {
			box.Width = maxChildWidth
		}
		// After shrink-wrap, update block children with auto width to use the new parent width
		for _, child := range box.Children {
			if child.Style != nil {
				childDisplay := child.Style.GetDisplay()
				if _, hasW := child.Style.GetLength("width"); !hasW && childDisplay != css.DisplayInline &&
					child.Style.GetFloat() == css.FloatNone &&
					child.Style.GetPosition() != css.PositionAbsolute && child.Style.GetPosition() != css.PositionFixed {
					child.Width = box.Width - child.Border.Left - child.Padding.Left - child.Padding.Right - child.Border.Right -
						child.Margin.Left - child.Margin.Right
					if child.Width < 0 {
						child.Width = 0
					}
					// Re-apply text-align with the updated width
					if child.Style != nil {
						if ta, ok := child.Style.Get("text-align"); ok && ta != "left" && ta != "" {
							le.applyTextAlign(child, ta, child.Width)
						}
					}
				}
			}
		}
	}

	// Phase 4: Apply absolute positioning AFTER children layout and height finalization
	if position == css.PositionAbsolute || position == css.PositionFixed {
		oldX, oldY := box.X, box.Y
		le.applyAbsolutePositioning(box)
		// Shift all children by the position delta
		dx, dy := box.X-oldX, box.Y-oldY
		if dx != 0 || dy != 0 {
			le.shiftChildren(box, dx, dy)
		}
	}

	// Phase 5: Handle float positioning AFTER children layout and shrink-wrapping
	var floatY float64
	if floatType != css.FloatNone && position == css.PositionStatic {
		oldX, oldY := box.X, box.Y
		floatTotalWidth := le.getTotalWidth(box)

		// Phase 5 Enhancement: Check if float fits, apply drop if needed
		// margin.Top was already applied to y at line 276 (y += margin.Top) and is
		// included in box.Y, so don't add it again here
		floatY = le.getFloatDropY(floatType, floatTotalWidth, box.Y, availableWidth)
		box.Y = floatY

		// Position float horizontally
		if floatType == css.FloatLeft {
			// Position at left edge (accounting for existing left floats)
			leftOffset, _ := le.getFloatOffsets(floatY)
			box.X = x + leftOffset
		} else if floatType == css.FloatRight {
			// Position at right edge (accounting for existing right floats)
			_, rightOffset := le.getFloatOffsets(floatY)
			box.X = x + availableWidth - floatTotalWidth - rightOffset
		}

		// Shift children by the position delta
		dx, dy := box.X-oldX, box.Y-oldY
		if dx != 0 || dy != 0 {
			le.shiftChildren(box, dx, dy)
		}
	}

	// Restore BFC float context - remove floats added inside this BFC
	if createsBFC {
		le.floats = le.floats[:le.floatBase]
		le.floatBase = le.floatBaseStack[len(le.floatBaseStack)-1]
		le.floatBaseStack = le.floatBaseStack[:len(le.floatBaseStack)-1]
	}

	// Add to float tracking (after BFC pop so float is in parent context)
	if floatType != css.FloatNone && position == css.PositionStatic {
		le.addFloat(box, floatType, floatY)
	}

	// After all positioning is done, fix float:right children that were
	// positioned before the parent width was finalized (shrink-to-fit containers)
	if !hasExplicitWidth && box.Width > 0 {
		le.repositionFloatRightChildren(box)
	}

	return box
}

// repositionFloatRightChildren fixes float:right children that were positioned
// before the parent's shrink-to-fit width was finalized.
func (le *LayoutEngine) repositionFloatRightChildren(box *Box) {
	contentLeft := box.X + box.Border.Left + box.Padding.Left
	contentRight := contentLeft + box.Width
	for _, child := range box.Children {
		if child.Style != nil && child.Style.GetFloat() == css.FloatRight {
			childTotalWidth := le.getTotalWidth(child)
			// Float:right: right edge of child aligns with right edge of parent content
			newX := contentRight - childTotalWidth
			dx := newX - child.X
			if dx != 0 {
				child.X = newX
				le.shiftChildren(child, dx, 0)
				// After shifting, recursively fix any float:right grandchildren
				le.repositionFloatRightChildren(child)
			}
		}
	}
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

// adjustChildrenY recursively adjusts Y positions of all children by delta
func (le *LayoutEngine) adjustChildrenY(box *Box, delta float64) {
	for _, child := range box.Children {
		child.Y += delta
		le.adjustChildrenY(child, delta)
	}
}

func (le *LayoutEngine) shiftChildren(box *Box, dx, dy float64) {
	for _, child := range box.Children {
		child.X += dx
		child.Y += dy
		le.shiftChildren(child, dx, dy)
	}
}

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

// getTotalWidth returns the total width including margin, border, padding
func (le *LayoutEngine) getTotalWidth(box *Box) float64 {
	return box.Margin.Left + box.Border.Left + box.Padding.Left +
		box.Width +
		box.Padding.Right + box.Border.Right + box.Margin.Right
}

// isFirstTextInBlock checks if this text node is the first text content
// in its block-level ancestor (for ::first-letter styling)
func (le *LayoutEngine) isFirstTextInBlock(node *html.Node, parent *Box) bool {
	if parent == nil || parent.Node == nil {
		return false
	}
	// Check if any text or element children came before this node
	for _, sibling := range parent.Node.Children {
		if sibling == node {
			return true // We reached ourselves first
		}
		if sibling.Type == html.TextNode && strings.TrimSpace(sibling.Text) != "" {
			return false // Another text node with content came first
		}
		if sibling.Type == html.ElementNode {
			return false // An element came first
		}
	}
	return false
}

// extractFirstLetter extracts the first letter from text (handling punctuation per CSS spec)
func extractFirstLetter(text string) (string, string) {
	text = strings.TrimLeft(text, " \t\n\r")
	if len(text) == 0 {
		return "", ""
	}
	// Get first rune
	runes := []rune(text)
	return string(runes[0]), string(runes[1:])
}

// Phase 6: layoutTextNode creates a layout box for a text node
func (le *LayoutEngine) layoutTextNode(node *html.Node, x, y, availableWidth float64, parentStyle *css.Style, parent *Box) *Box {
	// Skip empty text nodes
	if node.Text == "" {
		return nil
	}

	// CSS 2.1 §16.6.1: Strip spaces at the beginning/end of a line in block containers.
	// When this text node is the first/last content child of a block-level parent,
	// trim leading/trailing whitespace respectively.
	// IMPORTANT: This only applies to block-level containers, not inline elements.
	// Inline elements preserve trailing whitespace because they flow horizontally.
	if parent != nil && parent.Node != nil && parent.Style != nil {
		parentDisplay := parent.Style.GetDisplay()
		// Only apply trimming for block-level containers (block, table-cell, etc.)
		// Do NOT trim for inline or inline-block parents - they flow horizontally
		isBlockContainer := parentDisplay == css.DisplayBlock ||
			parentDisplay == css.DisplayTableCell ||
			parentDisplay == css.DisplayListItem

		if isBlockContainer {
			isFirstContent := true
			isLastContent := true
			for _, sibling := range parent.Node.Children {
				if sibling == node {
					break
				}
				if sibling.Type == html.TextNode && strings.TrimSpace(sibling.Text) != "" {
					isFirstContent = false
				} else if sibling.Type == html.ElementNode {
					isFirstContent = false
				}
			}
			foundSelf := false
			for _, sibling := range parent.Node.Children {
				if sibling == node {
					foundSelf = true
					continue
				}
				if foundSelf {
					if sibling.Type == html.TextNode && strings.TrimSpace(sibling.Text) != "" {
						isLastContent = false
					} else if sibling.Type == html.ElementNode {
						isLastContent = false
					}
				}
			}
			if isFirstContent {
				node.Text = strings.TrimLeft(node.Text, " \t\n\r")
			}
			if isLastContent {
				node.Text = strings.TrimRight(node.Text, " \t\n\r")
			}
			if node.Text == "" {
				return nil
			}
		}
	}

	// Check for ::first-letter pseudo-element styling
	// This applies to the first letter of the first text in a block container
	var firstLetterBox *Box
	if parent != nil && parent.Node != nil && le.isFirstTextInBlock(node, parent) {
		// First check if there are any actual ::first-letter rules targeting this element
		// (not just inherited properties from parent)
		hasFirstLetterRules := false
		for _, stylesheet := range le.stylesheets {
			for _, rule := range stylesheet.Rules {
				if rule.Selector.PseudoElement == "first-letter" {
					if css.MatchesSelector(parent.Node, rule.Selector) {
						hasFirstLetterRules = true
						break
					}
				}
			}
			if hasFirstLetterRules {
				break
			}
		}

		if hasFirstLetterRules {
			// Get the computed first-letter style
			firstLetterStyle := css.ComputePseudoElementStyle(parent.Node, "first-letter", le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
			firstLetter, remaining := extractFirstLetter(node.Text)
			if firstLetter != "" {
				// Create a box for the first letter with the special styling
				flFontSize := firstLetterStyle.GetFontSize()
				flFontWeight := firstLetterStyle.GetFontWeight()
				flBold := flFontWeight == css.FontWeightBold
				flWidth, flHeight := text.MeasureTextWithWeight(firstLetter, flFontSize, flBold)

				firstLetterBox = &Box{
					Node:          node,
					Style:         firstLetterStyle,
					X:             x,
					Y:             y,
					Width:         flWidth,
					Height:        flHeight,
					Margin:        css.BoxEdge{},
					Padding:       css.BoxEdge{},
					Border:        css.BoxEdge{},
					Children:      make([]*Box, 0),
					Parent:        parent,
					PseudoContent: firstLetter,
				}

				// Advance x for the remaining text
				x += flWidth
				availableWidth -= flWidth
				// Update the node text to exclude the first letter
				node.Text = remaining
				if node.Text == "" {
					// Only the first letter, return just that box
					return firstLetterBox
				}
			}
		}
	}

	// Get font properties from parent style
	fontSize := parentStyle.GetFontSize()
	fontWeight := parentStyle.GetFontWeight()
	lineHeight := parentStyle.GetLineHeight() // Phase 7 Enhancement

	// Phase 5: Adjust position and width for floats
	adjustedX := x
	adjustedWidth := availableWidth

	// Get available space accounting for floats
	leftOffset, rightOffset := le.getFloatOffsets(y)
	adjustedX += leftOffset
	adjustedWidth -= (leftOffset + rightOffset)

	// Phase 6 Enhancement: Measure the text with correct font weight
	isBold := fontWeight == css.FontWeightBold
	width, _ := text.MeasureTextWithWeight(node.Text, fontSize, isBold)
	height := lineHeight // Phase 7 Enhancement: Use line-height for box height

	// Compute parent's content-area left edge and full width for wrapped lines.
	// The first line uses the remaining space (adjustedWidth), but subsequent
	// lines start at the parent's left edge and use the full content width.
	parentContentLeft := adjustedX
	parentContentWidth := adjustedWidth
	if parent != nil {
		parentContentLeft = parent.X + parent.Border.Left + parent.Padding.Left
		parentContentWidth = parent.Width
	}

	// Phase 6 Enhancement: Check if text needs line breaking
	if width > adjustedWidth && adjustedWidth > 0 {
		// Break text into multiple lines, using remaining space for first line
		// and full parent width for subsequent lines.
		lines := text.BreakTextIntoLinesWithWrap(node.Text, fontSize, isBold, adjustedWidth, parentContentWidth)

		if len(lines) > 1 {
			// Create a container box for multi-line text
			containerBox := &Box{
				Node:     node,
				Style:    parentStyle,
				X:        adjustedX,
				Y:        y,
				Width:    parentContentWidth,
				Height:   float64(len(lines)) * height,
				Margin:   css.BoxEdge{},
				Padding:  css.BoxEdge{},
				Border:   css.BoxEdge{},
				Children: make([]*Box, 0),
				Position: css.PositionStatic,
				ZIndex:   0,
				Parent:   parent,
			}

			// Create a box for each line
			currentY := y
			for i, line := range lines {
				lineWidth, _ := text.MeasureTextWithWeight(line, fontSize, isBold)
				lineNode := &html.Node{
					Type: html.TextNode,
					Text: line,
				}

				// First line starts at current X; subsequent lines at parent left edge
				lineX := adjustedX
				if i > 0 {
					lineX = parentContentLeft
				}

				lineBox := &Box{
					Node:     lineNode,
					Style:    parentStyle,
					X:        lineX,
					Y:        currentY,
					Width:    lineWidth,
					Height:   lineHeight, // Phase 7: Use line-height consistently
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Children: make([]*Box, 0),
					Position: css.PositionStatic,
					ZIndex:   0,
					Parent:   containerBox,
				}

				containerBox.Children = append(containerBox.Children, lineBox)
				currentY += lineHeight
			}

			return containerBox
		}
	}

	// Create a box for single-line text
	box := &Box{
		Node:     node,
		Style:    parentStyle, // Text inherits parent's style
		X:        adjustedX,
		Y:        y,
		Width:    width,
		Height:   height,
		Margin:   css.BoxEdge{},   // No margin for text
		Padding:  css.BoxEdge{},   // No padding for text
		Border:   css.BoxEdge{},   // No border for text
		Children: make([]*Box, 0), // Text nodes have no children
		Position: css.PositionStatic,
		ZIndex:   0,
		Parent:   parent,
	}

	return box
}

// Phase 5: Float layout helper methods

// addFloat adds a floated element to the tracking list
func (le *LayoutEngine) addFloat(box *Box, side css.FloatType, y float64) {
	le.floats = append(le.floats, FloatInfo{
		Box:  box,
		Side: side,
		Y:    y,
	})
}

// getFloatOffsets returns the left and right offsets caused by floats at a given Y position
func (le *LayoutEngine) getFloatOffsets(y float64) (leftOffset, rightOffset float64) {
	leftOffset = 0
	rightOffset = 0

	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		// Check if this float affects the current Y position
		floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)
		if y >= floatInfo.Y && y < floatBottom {
			if floatInfo.Side == css.FloatLeft {
				floatWidth := le.getTotalWidth(floatInfo.Box)
				if floatWidth > leftOffset {
					leftOffset = floatWidth
				}
			} else if floatInfo.Side == css.FloatRight {
				floatWidth := le.getTotalWidth(floatInfo.Box)
				if floatWidth > rightOffset {
					rightOffset = floatWidth
				}
			}
		}
	}

	return leftOffset, rightOffset
}

// getClearY returns the Y position after clearing floats
func (le *LayoutEngine) getClearY(clearType css.ClearType, currentY float64) float64 {
	if clearType == css.ClearNone {
		return currentY
	}

	maxY := currentY

	for i := le.floatBase; i < len(le.floats); i++ {
		floatInfo := le.floats[i]
		b := floatInfo.Box
		// CSS 2.1 §9.5.2: clearance uses the float's "bottom outer edge" (margin edge),
		// which includes margin-bottom even when negative.
		floatBottom := floatInfo.Y + b.Border.Top + b.Padding.Top + b.Height + b.Padding.Bottom + b.Border.Bottom + b.Margin.Bottom

		shouldClear := false
		switch clearType {
		case css.ClearLeft:
			shouldClear = floatInfo.Side == css.FloatLeft
		case css.ClearRight:
			shouldClear = floatInfo.Side == css.FloatRight
		case css.ClearBoth:
			shouldClear = true
		}

		if shouldClear && floatBottom > maxY {
			maxY = floatBottom
		}
	}

	return maxY
}

// Phase 5 Enhancement: getFloatDropY finds Y position where float of given width will fit
func (le *LayoutEngine) getFloatDropY(floatType css.FloatType, floatWidth float64, startY float64, availableWidth float64) float64 {
	// If available width is 0 (shrink-to-fit parent), skip drop logic
	if availableWidth <= 0 {
		return startY
	}
	currentY := startY
	maxAttempts := 100 // Prevent infinite loops

	for attempt := 0; attempt < maxAttempts; attempt++ {
		leftOffset, rightOffset := le.getFloatOffsets(currentY)
		remainingWidth := availableWidth - leftOffset - rightOffset

		// Check if float fits at current Y
		if floatWidth <= remainingWidth {
			return currentY
		}

		// Find the next Y position where a float ends
		nextY := currentY + 1 // Default small increment
		for _, floatInfo := range le.floats {
			floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)
			// Look for the nearest float bottom that's below current Y
			if floatBottom > currentY && (nextY == currentY+1 || floatBottom < nextY) {
				nextY = floatBottom
			}
		}

		currentY = nextY

		// If we've moved way down, just return current position
		if currentY > startY+1000 {
			return startY
		}
	}

	return currentY
}

// Phase 7 Enhancement: applyVerticalAlign adjusts element Y position based on vertical-align
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

// Phase 9: buildTableInfo analyzes table structure and creates TableInfo
func (le *LayoutEngine) buildTableInfo(tableBox *Box, computedStyles map[*html.Node]*css.Style) *TableInfo {
	tableInfo := &TableInfo{
		Rows:           make([]*TableRow, 0),
		BorderSpacing:  tableBox.Style.GetBorderSpacing(),
		BorderCollapse: tableBox.Style.GetBorderCollapse(),
	}

	// Scan children for rows (tr elements or display: table-row)
	// Also handle anonymous row generation for direct table-cell children
	var anonRow *TableRow
	for _, child := range tableBox.Node.Children {
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
		}

		childDisplay := childStyle.GetDisplay()

		// Check if this is a row (tr tag or display: table-row)
		isRow := child.TagName == "tr" || childDisplay == css.DisplayTableRow

		// Also check for tbody, thead, tfoot which contain rows
		isRowGroup := child.TagName == "tbody" || child.TagName == "thead" || child.TagName == "tfoot" ||
			childDisplay == css.DisplayTableRowGroup ||
			childDisplay == css.DisplayTableHeaderGroup ||
			childDisplay == css.DisplayTableFooterGroup

		// Check if this is a table-cell (or will be wrapped as one)
		isCell := child.TagName == "td" || child.TagName == "th" || childDisplay == css.DisplayTableCell

		if isRow {
			anonRow = nil // explicit row breaks anonymous row
			tableInfo.Rows = append(tableInfo.Rows, &TableRow{Cells: make([]*TableCell, 0)})
		} else if isRowGroup {
			anonRow = nil
			// Process rows within the group
			for _, groupChild := range child.Children {
				if groupChild.Type != html.ElementNode {
					continue
				}
				groupChildStyle := computedStyles[groupChild]
				if groupChildStyle == nil {
					groupChildStyle = css.NewStyle()
				}
				if groupChild.TagName == "tr" || groupChildStyle.GetDisplay() == css.DisplayTableRow {
					tableInfo.Rows = append(tableInfo.Rows, &TableRow{Cells: make([]*TableCell, 0)})
				}
			}
		} else if isCell {
			// CSS 2.1 §17.2.1: Generate anonymous table-row for consecutive table-cells
			if anonRow == nil {
				anonRow = &TableRow{Cells: make([]*TableCell, 0)}
				tableInfo.Rows = append(tableInfo.Rows, anonRow)
			}
		} else {
			// Non-table child: wrap in anonymous cell within the anonymous row
			if anonRow == nil {
				anonRow = &TableRow{Cells: make([]*TableCell, 0)}
				tableInfo.Rows = append(tableInfo.Rows, anonRow)
			}
		}
	}

	return tableInfo
}

// Phase 9: getColspan returns the colspan attribute value (default 1)
func getColspan(node *html.Node) int {
	if colspan, ok := node.GetAttribute("colspan"); ok {
		if c, ok := css.ParseLength(colspan); ok {
			col := int(c)
			if col > 0 {
				return col
			}
		}
	}
	return 1
}

// Phase 9: getRowspan returns the rowspan attribute value (default 1)
func getRowspan(node *html.Node) int {
	if rowspan, ok := node.GetAttribute("rowspan"); ok {
		if r, ok := css.ParseLength(rowspan); ok {
			row := int(r)
			if row > 0 {
				return row
			}
		}
	}
	return 1
}

// Phase 9: layoutTable performs table layout
func (le *LayoutEngine) layoutTable(tableBox *Box, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style) {
	tableInfo := le.buildTableInfo(tableBox, computedStyles)

	// Build cell grid accounting for rowspan/colspan
	rowIdx := 0
	cellGrid := make([][]*TableCell, 0)

	// Process table structure
	for _, child := range tableBox.Node.Children {
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
		}

		le.processTableRows(child, childStyle, computedStyles, &rowIdx, &cellGrid, tableInfo)
	}

	// Determine number of columns
	numCols := 0
	for _, row := range cellGrid {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	tableInfo.NumCols = numCols

	// Calculate column widths
	tableInfo.ColumnWidths = le.calculateColumnWidths(cellGrid, availableWidth, tableInfo, tableBox.Width)

	// Set table width from column widths if not explicitly set
	if tableBox.Width == 0 {
		totalW := 0.0
		for _, cw := range tableInfo.ColumnWidths {
			totalW += cw
		}
		borderSpacing := tableInfo.BorderSpacing
		if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
			borderSpacing = 0
		}
		totalW += borderSpacing * float64(numCols+1)
		tableBox.Width = totalW
	}

	// Calculate row heights
	tableInfo.RowHeights = le.calculateRowHeights(cellGrid, tableInfo)

	// Set table height from row heights if not explicitly set
	if tableBox.Height == 0 {
		totalH := 0.0
		for _, rh := range tableInfo.RowHeights {
			totalH += rh
		}
		borderSpacing := tableInfo.BorderSpacing
		if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
			borderSpacing = 0
		}
		totalH += borderSpacing * float64(len(tableInfo.RowHeights)+1)
		tableBox.Height = totalH
	}

	// Position cells
	le.positionTableCells(tableBox, cellGrid, tableInfo, x, y)
}

// Phase 9: processTableRows recursively processes rows and row groups
func (le *LayoutEngine) processTableRows(node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style, rowIdx *int, cellGrid *[][]*TableCell, tableInfo *TableInfo) {
	display := style.GetDisplay()
	isRow := node.TagName == "tr" || display == css.DisplayTableRow
	isRowGroup := node.TagName == "tbody" || node.TagName == "thead" || node.TagName == "tfoot" ||
		display == css.DisplayTableRowGroup ||
		display == css.DisplayTableHeaderGroup ||
		display == css.DisplayTableFooterGroup

	if isRow {
		// Ensure we have enough rows in the grid
		for len(*cellGrid) <= *rowIdx {
			*cellGrid = append(*cellGrid, make([]*TableCell, 0))
		}

		colIdx := 0
		for _, cellNode := range node.Children {
			if cellNode.Type != html.ElementNode {
				continue
			}

			cellStyle := computedStyles[cellNode]
			if cellStyle == nil {
				cellStyle = css.NewStyle()
			}

			isCell := cellNode.TagName == "td" || cellNode.TagName == "th" ||
				cellStyle.GetDisplay() == css.DisplayTableCell

			if !isCell {
				continue
			}

			// Skip columns occupied by rowspan from previous rows
			for colIdx < len((*cellGrid)[*rowIdx]) && (*cellGrid)[*rowIdx][colIdx] != nil {
				colIdx++
			}

			colspan := getColspan(cellNode)
			rowspan := getRowspan(cellNode)

			cell := &TableCell{
				Box:     &Box{Node: cellNode, Style: cellStyle},
				RowSpan: rowspan,
				ColSpan: colspan,
				RowIdx:  *rowIdx,
				ColIdx:  colIdx,
			}

			// Mark cells in grid for this cell and its span
			for r := 0; r < rowspan; r++ {
				for len(*cellGrid) <= *rowIdx+r {
					*cellGrid = append(*cellGrid, make([]*TableCell, 0))
				}
				for c := 0; c < colspan; c++ {
					for len((*cellGrid)[*rowIdx+r]) <= colIdx+c {
						(*cellGrid)[*rowIdx+r] = append((*cellGrid)[*rowIdx+r], nil)
					}
					(*cellGrid)[*rowIdx+r][colIdx+c] = cell
				}
			}

			colIdx += colspan
		}

		*rowIdx++
	} else if isRowGroup {
		// Process rows within the group
		for _, child := range node.Children {
			if child.Type != html.ElementNode {
				continue
			}
			childStyle := computedStyles[child]
			if childStyle == nil {
				childStyle = css.NewStyle()
			}
			le.processTableRows(child, childStyle, computedStyles, rowIdx, cellGrid, tableInfo)
		}
	} else if display == css.DisplayTableCell || display == css.DisplayTable {
		// CSS 2.1 §17.2.1: Direct table-cell children generate an anonymous row
		// Also handle nested display:table elements as anonymous cells
		for len(*cellGrid) <= *rowIdx {
			*cellGrid = append(*cellGrid, make([]*TableCell, 0))
		}
		colIdx := len((*cellGrid)[*rowIdx])
		cell := &TableCell{
			Box:     &Box{Node: node, Style: style},
			RowSpan: 1,
			ColSpan: 1,
			RowIdx:  *rowIdx,
			ColIdx:  colIdx,
		}
		for len((*cellGrid)[*rowIdx]) <= colIdx {
			(*cellGrid)[*rowIdx] = append((*cellGrid)[*rowIdx], nil)
		}
		(*cellGrid)[*rowIdx][colIdx] = cell
	} else {
		// Non-table child: wrap in anonymous table-cell within the current anonymous row
		for len(*cellGrid) <= *rowIdx {
			*cellGrid = append(*cellGrid, make([]*TableCell, 0))
		}
		colIdx := len((*cellGrid)[*rowIdx])
		cell := &TableCell{
			Box:     &Box{Node: node, Style: style},
			RowSpan: 1,
			ColSpan: 1,
			RowIdx:  *rowIdx,
			ColIdx:  colIdx,
		}
		for len((*cellGrid)[*rowIdx]) <= colIdx {
			(*cellGrid)[*rowIdx] = append((*cellGrid)[*rowIdx], nil)
		}
		(*cellGrid)[*rowIdx][colIdx] = cell
	}
}

// Phase 9: calculateColumnWidths determines column widths
// tableWidth is the explicit table width (0 for shrink-to-fit tables)
func (le *LayoutEngine) calculateColumnWidths(cellGrid [][]*TableCell, availableWidth float64, tableInfo *TableInfo, tableWidth float64) []float64 {
	numCols := tableInfo.NumCols
	if numCols == 0 {
		return []float64{}
	}

	// Account for border spacing
	var totalSpacing float64
	if tableInfo.BorderCollapse == css.BorderCollapseSeparate {
		totalSpacing = tableInfo.BorderSpacing * float64(numCols+1)
	}

	// First pass: determine column widths from cell explicit widths
	columnWidths := make([]float64, numCols)
	hasExplicit := make([]bool, numCols)
	contentWidths := make([]float64, numCols) // content-based widths
	for _, row := range cellGrid {
		for colIdx, cell := range row {
			if cell == nil || cell.Box == nil || cell.Box.Style == nil || cell.ColIdx != colIdx {
				continue
			}
			if w, ok := cell.Box.Style.GetLength("width"); ok && w > 0 {
				if w > columnWidths[colIdx] {
					columnWidths[colIdx] = w
					hasExplicit[colIdx] = true
				}
			}
			// Measure content width for auto-sizing
			if !hasExplicit[colIdx] {
				cw := le.measureCellContentWidth(cell)
				if cw > contentWidths[colIdx] {
					contentWidths[colIdx] = cw
				}
			}
		}
	}

	// Distribute remaining width to columns without explicit widths
	// Use content-based sizing: give each column its content width,
	// then distribute any leftover space proportionally.
	usedWidth := totalSpacing
	unsetCols := 0
	totalContentWidth := 0.0
	for i := 0; i < numCols; i++ {
		usedWidth += columnWidths[i]
		if !hasExplicit[i] {
			unsetCols++
			totalContentWidth += contentWidths[i]
		}
	}
	if unsetCols > 0 {
		remaining := availableWidth - usedWidth
		if remaining > 0 {
			if tableWidth == 0 && totalContentWidth > 0 {
				// Shrink-to-fit table: use content widths directly, no extra space distribution
				for i := 0; i < numCols; i++ {
					if !hasExplicit[i] {
						columnWidths[i] = contentWidths[i]
					}
				}
			} else if totalContentWidth > 0 && totalContentWidth <= remaining {
				// Content fits: use content widths, distribute extra space proportionally
				extraSpace := remaining - totalContentWidth
				for i := 0; i < numCols; i++ {
					if !hasExplicit[i] {
						columnWidths[i] = contentWidths[i] + extraSpace*contentWidths[i]/totalContentWidth
					}
				}
			} else if totalContentWidth > remaining {
				// Content doesn't fit: distribute proportionally based on content
				for i := 0; i < numCols; i++ {
					if !hasExplicit[i] {
						columnWidths[i] = remaining * contentWidths[i] / totalContentWidth
					}
				}
			} else {
				// No content measured: distribute evenly
				perCol := remaining / float64(unsetCols)
				for i := 0; i < numCols; i++ {
					if !hasExplicit[i] {
						columnWidths[i] = perCol
					}
				}
			}
		} else {
			// No remaining space; use minimum width
			for i := 0; i < numCols; i++ {
				if !hasExplicit[i] {
					columnWidths[i] = 10
				}
			}
		}
	}

	return columnWidths
}

// measureCellContentWidth measures the preferred content width of a table cell
func (le *LayoutEngine) measureCellContentWidth(cell *TableCell) float64 {
	if cell == nil || cell.Box == nil || cell.Box.Node == nil {
		return 0
	}
	totalWidth := 0.0
	fontSize := 16.0
	isBold := false
	if cell.Box.Style != nil {
		fontSize = cell.Box.Style.GetFontSize()
		isBold = cell.Box.Style.GetFontWeight() == css.FontWeightBold
	}
	for _, child := range cell.Box.Node.Children {
		if child.Type == html.TextNode {
			w, _ := text.MeasureTextWithWeight(child.Text, fontSize, isBold)
			totalWidth += w
		}
	}
	// Add cell padding and border
	if cell.Box.Style != nil {
		padding := cell.Box.Style.GetPadding()
		border := cell.Box.Style.GetBorderWidth()
		totalWidth += padding.Left + padding.Right + border.Left + border.Right
	}
	return totalWidth
}

// Phase 9: calculateRowHeights determines row heights
func (le *LayoutEngine) calculateRowHeights(cellGrid [][]*TableCell, tableInfo *TableInfo) []float64 {
	numRows := len(cellGrid)
	rowHeights := make([]float64, numRows)

	// Calculate row heights from cell content and explicit heights
	for i := 0; i < numRows; i++ {
		maxHeight := 0.0
		for _, cell := range cellGrid[i] {
			if cell == nil || cell.Box == nil {
				continue
			}
			// Check for explicit height from style
			if cell.Box.Style != nil {
				if h, ok := cell.Box.Style.GetLength("height"); ok && h > maxHeight {
					maxHeight = h
				}
			}
			// Get padding and border from style since box values may not be set yet
			var paddingTop, paddingBottom, borderTop, borderBottom float64
			if cell.Box.Style != nil {
				padding := cell.Box.Style.GetPadding()
				paddingTop = padding.Top
				paddingBottom = padding.Bottom
				border := cell.Box.Style.GetBorderWidth()
				borderTop = border.Top
				borderBottom = border.Bottom
			}
			cellHeight := cell.Box.Height + paddingTop + paddingBottom + borderTop + borderBottom
			if cellHeight > maxHeight {
				maxHeight = cellHeight
			}
			// Estimate height from text content if cell hasn't been laid out yet
			if cell.Box.Height == 0 && cell.Box.Node != nil {
				lineHeight := 19.2 // default line height for 16px font
				if cell.Box.Style != nil {
					lineHeight = cell.Box.Style.GetLineHeight()
				}
				for _, child := range cell.Box.Node.Children {
					if child.Type == html.TextNode && child.Text != "" {
						textHeight := lineHeight + paddingTop + paddingBottom + borderTop + borderBottom
						if textHeight > maxHeight {
							maxHeight = textHeight
						}
					}
				}
			}
		}
		rowHeights[i] = maxHeight
	}

	return rowHeights
}

// Phase 9: positionTableCells positions cells in the table
func (le *LayoutEngine) positionTableCells(tableBox *Box, cellGrid [][]*TableCell, tableInfo *TableInfo, x, y float64) {
	borderSpacing := tableInfo.BorderSpacing
	if tableInfo.BorderCollapse == css.BorderCollapseCollapse {
		borderSpacing = 0
	}

	// Position cells
	currentY := y + tableBox.Border.Top + tableBox.Padding.Top + borderSpacing
	processedCells := make(map[*TableCell]bool)

	for rowIdx, row := range cellGrid {
		currentX := x + tableBox.Border.Left + tableBox.Padding.Left + borderSpacing
		rowHeight := tableInfo.RowHeights[rowIdx]

		for colIdx, cell := range row {
			if cell == nil || processedCells[cell] {
				// Skip empty cells or already processed cells
				if cell == nil {
					// Still advance X for empty cell
					currentX += tableInfo.ColumnWidths[colIdx] + borderSpacing
				}
				continue
			}

			// Calculate cell width (sum of spanned columns)
			cellWidth := 0.0
			for c := 0; c < cell.ColSpan; c++ {
				if colIdx+c < tableInfo.NumCols {
					cellWidth += tableInfo.ColumnWidths[colIdx+c]
					if c > 0 {
						cellWidth += borderSpacing
					}
				}
			}

			// Calculate cell height (sum of spanned rows)
			cellHeight := 0.0
			for r := 0; r < cell.RowSpan; r++ {
				if rowIdx+r < len(tableInfo.RowHeights) {
					cellHeight += tableInfo.RowHeights[rowIdx+r]
					if r > 0 {
						cellHeight += borderSpacing
					}
				}
			}

			// Set cell box dimensions and position
			// Note: cellWidth/cellHeight from row/column calculations include padding+border,
			// but box.Width/Height should be content dimensions only
			cell.Box.Margin = cell.Box.Style.GetMargin()
			cell.Box.Padding = cell.Box.Style.GetPadding()
			cell.Box.Border = cell.Box.Style.GetBorderWidth()
			cell.Box.X = currentX
			cell.Box.Y = currentY
			cell.Box.Width = cellWidth - cell.Box.Padding.Left - cell.Box.Padding.Right - cell.Box.Border.Left - cell.Box.Border.Right
			cell.Box.Height = cellHeight - cell.Box.Padding.Top - cell.Box.Padding.Bottom - cell.Box.Border.Top - cell.Box.Border.Bottom
			if cell.Box.Width < 0 {
				cell.Box.Width = 0
			}
			if cell.Box.Height < 0 {
				cell.Box.Height = 0
			}

			// Layout cell content (children)
			childY := currentY + cell.Box.Border.Top + cell.Box.Padding.Top
			childX := currentX + cell.Box.Border.Left + cell.Box.Padding.Left
			childAvailableWidth := cellWidth - cell.Box.Padding.Left - cell.Box.Padding.Right

			for _, childNode := range cell.Box.Node.Children {
				if childNode.Type == html.TextNode {
					// Handle text in cell
					textBox := le.layoutTextNode(childNode, childX, childY, childAvailableWidth, cell.Box.Style, cell.Box)
					if textBox != nil {
						cell.Box.Children = append(cell.Box.Children, textBox)
						childY += le.getTotalHeight(textBox)
					}
				}
			}

			// Add cell box to table's children
			tableBox.Children = append(tableBox.Children, cell.Box)
			processedCells[cell] = true

			currentX += cellWidth + borderSpacing
		}

		currentY += rowHeight + borderSpacing
	}

	// Update table box height based on content
	if len(cellGrid) > 0 {
		tableBox.Height = currentY - y - tableBox.Border.Top - tableBox.Padding.Top
	}
}

// Phase 10: layoutFlex performs flexbox layout
func (le *LayoutEngine) layoutFlex(flexBox *Box, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style) {
	direction := flexBox.Style.GetFlexDirection()
	wrap := flexBox.Style.GetFlexWrap()
	justifyContent := flexBox.Style.GetJustifyContent()
	alignItems := flexBox.Style.GetAlignItems()
	alignContent := flexBox.Style.GetAlignContent()

	isRow := direction == css.FlexDirectionRow || direction == css.FlexDirectionRowReverse

	container := &FlexContainer{
		Direction:      direction,
		Wrap:           wrap,
		JustifyContent: justifyContent,
		AlignItems:     alignItems,
		AlignContent:   alignContent,
		IsRow:          isRow,
		Lines:          make([]*FlexLine, 0),
	}

	// Determine container size
	if isRow {
		container.MainAxisSize = flexBox.Width
		container.CrossAxisSize = flexBox.Height
	} else {
		container.MainAxisSize = flexBox.Height
		container.CrossAxisSize = flexBox.Width
	}

	// Create flex items from children
	items := le.createFlexItems(flexBox, computedStyles)

	// Sort items by order property
	le.sortFlexItemsByOrder(items)

	// Create flex lines (handle wrapping)
	le.createFlexLines(container, items)

	// Resolve flexible lengths (flex-grow, flex-shrink)
	le.resolveFlexibleLengths(container)

	// Position items along main axis
	le.distributeMainAxis(container, flexBox)

	// Position items along cross axis
	le.alignCrossAxis(container, flexBox)

	// Update flex box children from positioned items
	for _, line := range container.Lines {
		for _, item := range line.Items {
			flexBox.Children = append(flexBox.Children, item.Box)
		}
	}

	// Update flex box height based on content if not explicitly set
	if container.IsRow {
		totalCrossSize := 0.0
		for _, line := range container.Lines {
			totalCrossSize += line.CrossSize
		}
		if flexBox.Height < totalCrossSize {
			flexBox.Height = totalCrossSize
		}
	} else {
		totalMainSize := 0.0
		for _, line := range container.Lines {
			totalMainSize += line.MainSize
		}
		if flexBox.Width < totalMainSize {
			flexBox.Width = totalMainSize
		}
	}
}

// Phase 10: createFlexItems creates flex items from children
func (le *LayoutEngine) createFlexItems(flexBox *Box, computedStyles map[*html.Node]*css.Style) []*FlexItem {
	items := make([]*FlexItem, 0)

	for _, child := range flexBox.Node.Children {
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.NewStyle()
		}

		// Skip display:none elements
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		// Create a box for the flex item (simplified - just get dimensions)
		margin := childStyle.GetMargin()
		padding := childStyle.GetPadding()
		border := childStyle.GetBorderWidth()

		var width, height float64
		if w, ok := childStyle.GetLength("width"); ok {
			width = w
		} else {
			width = 100 // Default width
		}
		if h, ok := childStyle.GetLength("height"); ok {
			height = h
		} else {
			height = 50 // Default height
		}

		box := &Box{
			Node:    child,
			Style:   childStyle,
			Width:   width,
			Height:  height,
			Margin:  margin,
			Padding: padding,
			Border:  border,
			Children: make([]*Box, 0),
		}

		flexBasis := childStyle.GetFlexBasis()
		if flexBasis == -1 { // auto
			// Use width or height depending on flex direction
			flexBasis = width
		}

		item := &FlexItem{
			Box:        box,
			FlexGrow:   childStyle.GetFlexGrow(),
			FlexShrink: childStyle.GetFlexShrink(),
			FlexBasis:  flexBasis,
			Order:      childStyle.GetOrder(),
		}

		items = append(items, item)
	}

	return items
}

// Phase 10: sortFlexItemsByOrder sorts items by their order property
func (le *LayoutEngine) sortFlexItemsByOrder(items []*FlexItem) {
	// Simple bubble sort (good enough for small lists)
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Order > items[j].Order {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

// Phase 10: createFlexLines creates flex lines (handles wrapping)
func (le *LayoutEngine) createFlexLines(container *FlexContainer, items []*FlexItem) {
	if container.Wrap == css.FlexWrapNowrap {
		// Single line
		line := &FlexLine{Items: items, MainSize: 0, CrossSize: 0}
		container.Lines = append(container.Lines, line)
	} else {
		// Multiple lines (wrapping)
		currentLine := &FlexLine{Items: make([]*FlexItem, 0), MainSize: 0, CrossSize: 0}

		for _, item := range items {
			itemMainSize := item.FlexBasis
			if container.IsRow {
				itemMainSize = item.Box.Width
			} else {
				itemMainSize = item.Box.Height
			}

			// Check if item fits in current line
			if currentLine.MainSize+itemMainSize > container.MainAxisSize && len(currentLine.Items) > 0 {
				// Start new line
				container.Lines = append(container.Lines, currentLine)
				currentLine = &FlexLine{Items: make([]*FlexItem, 0), MainSize: 0, CrossSize: 0}
			}

			currentLine.Items = append(currentLine.Items, item)
			currentLine.MainSize += itemMainSize
		}

		// Add last line
		if len(currentLine.Items) > 0 {
			container.Lines = append(container.Lines, currentLine)
		}
	}
}

// Phase 10: resolveFlexibleLengths calculates final item sizes
func (le *LayoutEngine) resolveFlexibleLengths(container *FlexContainer) {
	for _, line := range container.Lines {
		// Calculate total flex basis
		totalFlexBasis := 0.0
		totalFlexGrow := 0.0
		totalFlexShrink := 0.0

		for _, item := range line.Items {
			if container.IsRow {
				item.MainSize = item.FlexBasis
			} else {
				item.MainSize = item.FlexBasis
			}
			totalFlexBasis += item.MainSize
			totalFlexGrow += item.FlexGrow
			totalFlexShrink += item.FlexShrink
		}

		// Calculate free space
		freeSpace := container.MainAxisSize - totalFlexBasis

		if freeSpace > 0 && totalFlexGrow > 0 {
			// Distribute extra space based on flex-grow
			for _, item := range line.Items {
				if item.FlexGrow > 0 {
					item.MainSize += freeSpace * (item.FlexGrow / totalFlexGrow)
				}
			}
		} else if freeSpace < 0 && totalFlexShrink > 0 {
			// Shrink items based on flex-shrink
			for _, item := range line.Items {
				if item.FlexShrink > 0 {
					shrinkAmount := -freeSpace * (item.FlexShrink / totalFlexShrink)
					item.MainSize -= shrinkAmount
					if item.MainSize < 0 {
						item.MainSize = 0
					}
				}
			}
		}

		// Set cross size
		for _, item := range line.Items {
			if container.IsRow {
				item.CrossSize = item.Box.Height
				item.Box.Width = item.MainSize
			} else {
				item.CrossSize = item.Box.Width
				item.Box.Height = item.MainSize
			}

			// Update line cross size
			if item.CrossSize > line.CrossSize {
				line.CrossSize = item.CrossSize
			}
		}
	}
}

// Phase 10: distributeMainAxis positions items along main axis
func (le *LayoutEngine) distributeMainAxis(container *FlexContainer, flexBox *Box) {
	for _, line := range container.Lines {
		// Calculate total main size of items
		totalMainSize := 0.0
		for _, item := range line.Items {
			totalMainSize += item.MainSize
		}

		freeSpace := container.MainAxisSize - totalMainSize
		currentPos := 0.0
		spacing := 0.0
		initialOffset := 0.0

		switch container.JustifyContent {
		case css.JustifyContentFlexStart:
			initialOffset = 0
		case css.JustifyContentFlexEnd:
			initialOffset = freeSpace
		case css.JustifyContentCenter:
			initialOffset = freeSpace / 2
		case css.JustifyContentSpaceBetween:
			if len(line.Items) > 1 {
				spacing = freeSpace / float64(len(line.Items)-1)
			}
		case css.JustifyContentSpaceAround:
			spacing = freeSpace / float64(len(line.Items))
			initialOffset = spacing / 2
		case css.JustifyContentSpaceEvenly:
			spacing = freeSpace / float64(len(line.Items)+1)
			initialOffset = spacing
		}

		currentPos = initialOffset

		for _, item := range line.Items {
			item.MainPos = currentPos
			currentPos += item.MainSize + spacing
		}
	}
}

// Phase 10: alignCrossAxis positions items along cross axis
func (le *LayoutEngine) alignCrossAxis(container *FlexContainer, flexBox *Box) {
	currentCrossPos := 0.0

	for _, line := range container.Lines {
		for _, item := range line.Items {
			// Determine alignment (use align-self if set, otherwise align-items)
			alignSelf := item.Box.Style.GetAlignSelf()
			alignment := container.AlignItems

			if alignSelf != css.AlignSelfAuto {
				// Map AlignSelf to AlignItems
				switch alignSelf {
				case css.AlignSelfFlexStart:
					alignment = css.AlignItemsFlexStart
				case css.AlignSelfFlexEnd:
					alignment = css.AlignItemsFlexEnd
				case css.AlignSelfCenter:
					alignment = css.AlignItemsCenter
				case css.AlignSelfStretch:
					alignment = css.AlignItemsStretch
				case css.AlignSelfBaseline:
					alignment = css.AlignItemsBaseline
				}
			}

			switch alignment {
			case css.AlignItemsFlexStart:
				item.CrossPos = currentCrossPos
			case css.AlignItemsFlexEnd:
				item.CrossPos = currentCrossPos + line.CrossSize - item.CrossSize
			case css.AlignItemsCenter:
				item.CrossPos = currentCrossPos + (line.CrossSize-item.CrossSize)/2
			case css.AlignItemsStretch:
				item.CrossPos = currentCrossPos
				item.CrossSize = line.CrossSize
				if container.IsRow {
					item.Box.Height = line.CrossSize
				} else {
					item.Box.Width = line.CrossSize
				}
			case css.AlignItemsBaseline:
				// Simplified - treat as flex-start
				item.CrossPos = currentCrossPos
			}

			// Set final box position
			if container.IsRow {
				item.Box.X = flexBox.X + flexBox.Border.Left + flexBox.Padding.Left + item.MainPos
				item.Box.Y = flexBox.Y + flexBox.Border.Top + flexBox.Padding.Top + item.CrossPos
			} else {
				item.Box.X = flexBox.X + flexBox.Border.Left + flexBox.Padding.Left + item.CrossPos
				item.Box.Y = flexBox.Y + flexBox.Border.Top + flexBox.Padding.Top + item.MainPos
			}
		}

		currentCrossPos += line.CrossSize
	}
}

// parseURLValue extracts the URL from a CSS url(...) value.
// Returns the URL and true if the value is a url() function, or empty string and false otherwise.
func parseURLValue(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "url(") || !strings.HasSuffix(trimmed, ")") {
		return "", false
	}
	inner := trimmed[4 : len(trimmed)-1]
	inner = strings.TrimSpace(inner)
	// Remove optional quotes
	if len(inner) >= 2 && ((inner[0] == '\'' && inner[len(inner)-1] == '\'') || (inner[0] == '"' && inner[len(inner)-1] == '"')) {
		inner = inner[1 : len(inner)-1]
	}
	return inner, true
}

// Phase 11: generatePseudoElement generates a pseudo-element box if it has content
func (le *LayoutEngine) generatePseudoElement(node *html.Node, pseudoType string, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style, parent *Box) *Box {
	// Compute pseudo-element style using stored stylesheets
	// Phase 22: Pass viewport dimensions for media query evaluation
	parentStyle := computedStyles[node]
	pseudoStyle := css.ComputePseudoElementStyle(node, pseudoType, le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)

	// Get content from pseudo-element style
	content, hasContent := pseudoStyle.GetContent()
	if !hasContent {
		return nil
	}

	// Create box model values
	margin := pseudoStyle.GetMargin()
	padding := pseudoStyle.GetPadding()
	border := pseudoStyle.GetBorderWidth()

	// Check if content is a url() value (image pseudo-element)
	if urlPath, isURL := parseURLValue(content); isURL {
		x += margin.Left

		// Try to get image dimensions for natural sizing
		var imgWidth, imgHeight float64
		if w, h, err := images.GetImageDimensionsWithFetcher(urlPath, le.imageFetcher); err == nil {
			imgWidth = float64(w)
			imgHeight = float64(h)
		}

		// Allow CSS width/height to override natural dimensions
		if cssW, ok := pseudoStyle.GetLength("width"); ok && cssW > 0 {
			imgWidth = cssW
		}
		if cssH, ok := pseudoStyle.GetLength("height"); ok && cssH > 0 {
			imgHeight = cssH
		}

		box := &Box{
			Node:      node,
			Style:     pseudoStyle,
			X:         x,
			Y:         y + margin.Top,
			Width:     imgWidth,
			Height:    imgHeight,
			Margin:    margin,
			Padding:   padding,
			Border:    border,
			Children:  make([]*Box, 0),
			Parent:    parent,
			ImagePath: urlPath,
		}
		return box
	}

	// Determine dimensions based on display type
	var boxWidth, boxHeight float64
	display := pseudoStyle.GetDisplay()

	if content != "" {
		// Text content: measure text
		fontSize := pseudoStyle.GetFontSize()
		fontWeight := pseudoStyle.GetFontWeight()
		bold := fontWeight == css.FontWeightBold
		boxWidth, boxHeight = text.MeasureTextWithWeight(content, fontSize, bold)
	}

	// Block-level pseudo-elements: only take available width if they have content
	// CSS triangles use empty content with borders, so they need width: 0
	if display == css.DisplayBlock && content != "" {
		boxWidth = availableWidth - margin.Left - margin.Right -
			padding.Left - padding.Right - border.Left - border.Right
	}

	// Apply explicit height
	if h, ok := pseudoStyle.GetLength("height"); ok {
		boxHeight = h
	}

	// Apply horizontal margin offset (border is inside the box, not added to position)
	x += margin.Left

	box := &Box{
		Node:          node, // Reference the parent node
		Style:         pseudoStyle,
		X:             x,
		Y:             y + margin.Top, // Y is border-box top (margin outside, border inside)
		Width:         boxWidth,
		Height:        boxHeight,
		Margin:        margin,
		Padding:       padding,
		Border:        border,
		Children:      make([]*Box, 0),
		Parent:        parent,
		PseudoContent: content, // Store the pseudo-element content
	}

	return box
}

// Phase 23: generateListMarker creates a marker box for list items
func (le *LayoutEngine) generateListMarker(node *html.Node, style *css.Style, x, y float64, parent *Box) *Box {
	listStyleType := style.GetListStyleType()
	if listStyleType == css.ListStyleTypeNone {
		return nil
	}

	// Generate marker text based on list-style-type
	var markerText string
	switch listStyleType {
	case css.ListStyleTypeDisc:
		markerText = "•"
	case css.ListStyleTypeCircle:
		markerText = "○"
	case css.ListStyleTypeSquare:
		markerText = "■"
	case css.ListStyleTypeDecimal:
		// Count preceding <li> siblings to determine number
		itemNumber := le.getListItemNumber(node)
		markerText = fmt.Sprintf("%d.", itemNumber)
	default:
		markerText = "•"
	}

	// Measure marker text
	fontSize := style.GetFontSize()
	fontWeight := style.GetFontWeight()
	bold := fontWeight == css.FontWeightBold
	textWidth, textHeight := text.MeasureTextWithWeight(markerText, fontSize, bold)

	// Position marker to the left of the content (outside the content box)
	markerX := x - 20 // 20px to the left of content edge
	markerY := y

	markerBox := &Box{
		Node:          node,
		Style:         style,
		X:             markerX,
		Y:             markerY,
		Width:         textWidth,
		Height:        textHeight,
		Margin:        css.BoxEdge{},
		Padding:       css.BoxEdge{},
		Border:        css.BoxEdge{},
		Children:      make([]*Box, 0),
		Parent:        parent,
		PseudoContent: markerText, // Store marker text for rendering
	}

	return markerBox
}

// Phase 23: getListItemNumber returns the 1-based index of a list item among its siblings
func (le *LayoutEngine) getListItemNumber(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}

	itemNumber := 1
	for _, sibling := range node.Parent.Children {
		if sibling == node {
			break
		}
		if sibling.Type == html.ElementNode && sibling.TagName == "li" {
			itemNumber++
		}
	}

	return itemNumber
}
