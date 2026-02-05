package layout

import (
	"fmt"     // Phase 23: For list marker formatting
	"strconv" // For counter value parsing
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

	// Block-in-inline fragment tracking (CSS 2.1 §9.2.1.1)
	// When a block element breaks an inline element, the inline's border is split
	IsFirstFragment bool // First part of split inline - has left border, no right border
	IsLastFragment  bool // Last part of split inline - has right border, no left border

	// New architecture: Fragments for split inline boxes
	// When non-empty, this box renders as multiple visual regions
	Fragments []BoxFragment

	// Cached intrinsic sizes (computed on demand)
	intrinsicSizes *IntrinsicSizes

	// Line boxes for block containers with inline content
	LineBoxes []*LineBox
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

	// CSS Counters support
	counters      map[string][]int // Counter name -> stack of values (for nested scopes)
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

// Multi-pass inline layout data structures (Blink-style)

// InlineItemType represents the type of an inline item
type InlineItemType int

const (
	InlineItemText InlineItemType = iota // Text content
	InlineItemOpenTag                     // Opening tag of inline element
	InlineItemCloseTag                    // Closing tag of inline element
	InlineItemAtomic                      // Atomic inline (inline-block, replaced element)
	InlineItemFloat                       // Floated element
	InlineItemControl                     // Control element (br, etc.)
)

// InlineItem represents a piece of inline content in the flattened item list.
// This is Phase 1 (CollectInlineItems) output - a sequential representation of all inline content.
type InlineItem struct {
	Type InlineItemType
	Node *html.Node // Source DOM node

	// For text items
	Text       string // Text content
	StartOffset int   // Start offset in original text
	EndOffset   int   // End offset in original text

	// For all items
	Style *css.Style // Computed style

	// Cached measurements (computed during collection)
	Width  float64 // Intrinsic width (for atomic items, measured text width)
	Height float64 // Intrinsic height
}

// LineBreakResult represents the result of line breaking for a single line.
// This is Phase 2 (BreakLines) output - what items go on each line.
type LineBreakResult struct {
	Items      []*InlineItem // Items on this line (references to InlineItem from main list)
	StartIndex int           // Index of first item in main item list
	EndIndex   int           // Index after last item in main item list

	// Constraints for this line
	Y              float64 // Y position of this line
	AvailableWidth float64 // Width available for this line (accounting for floats)
	LineHeight     float64 // Computed height of this line

	// Text breaking info (for items that span multiple lines)
	TextBreaks map[*InlineItem]struct { // For text items that break across lines
		StartOffset int
		EndOffset   int
	}
}

// InlineLayoutState holds the state for multi-pass inline layout.
// This coordinates all three phases of inline layout.
type InlineLayoutState struct {
	// Phase 1: Collected items (flattened inline content)
	Items []*InlineItem

	// Phase 2: Line breaking results
	Lines []*LineBreakResult

	// Context from parent container
	ContainerBox       *Box
	ContainerStyle     *css.Style
	AvailableWidth     float64
	StartY             float64
	Border             css.BoxEdge
	Padding            css.BoxEdge

	// Float tracking (for line breaking retry)
	FloatList      []FloatInfo // Active floats that affect line breaking
	FloatBaseIndex int         // Index into LayoutEngine.floats where this context starts
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

// ============================================================================
// New Architecture: Intrinsic Sizing, Line Boxes, and Fragments
// ============================================================================

// Axis represents a layout axis (horizontal or vertical)
// Used for flexbox main/cross axis and future grid support
type Axis int

const (
	AxisHorizontal Axis = iota
	AxisVertical
)

// IntrinsicSizes holds the computed intrinsic size information for a box
// These are used for shrink-to-fit width, flexbox, and table layout
type IntrinsicSizes struct {
	MinContent float64 // Width when all soft wrap opportunities are taken
	MaxContent float64 // Width when no soft wrapping occurs
	Preferred  float64 // Preferred width (for flex-basis: auto)
}

// Alignment represents alignment values for flex/grid layout
type Alignment int

const (
	AlignStart Alignment = iota
	AlignEnd
	AlignCenter
	AlignStretch
	AlignBaseline
	AlignSpaceBetween
	AlignSpaceAround
	AlignSpaceEvenly
)

// BorderEdgeFlags indicates which border edges should be drawn
// Used for fragmented inline boxes (block-in-inline splitting)
type BorderEdgeFlags struct {
	Left   bool
	Right  bool
	Top    bool
	Bottom bool
}

// AllBorders returns BorderEdgeFlags with all edges enabled
func AllBorders() BorderEdgeFlags {
	return BorderEdgeFlags{Left: true, Right: true, Top: true, Bottom: true}
}

// BoxFragment represents a visual fragment of a box
// When an inline box is split by a block element, it renders as multiple fragments
type BoxFragment struct {
	X, Y, Width, Height float64
	Borders             BorderEdgeFlags
}

// LineBox represents a line of inline content
// This is an explicit representation of CSS line boxes for proper inline layout
type LineBox struct {
	Y         float64   // Y position of the line box
	Height    float64   // Height of the line box
	Boxes     []*Box    // Inline-level boxes on this line
	BaselineY float64   // Y position of the alphabetic baseline (relative to line top)
	LeftEdge  float64   // Left edge of available space (accounting for floats)
	RightEdge float64   // Right edge of available space (accounting for floats)
}

// LayoutMode defines the interface for different layout algorithms
// This abstraction allows clean separation of block, inline, flex, grid layout
type LayoutMode interface {
	// ComputeIntrinsicSizes calculates min-content and max-content widths
	ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes

	// LayoutChildren positions child boxes within the container
	LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box
}

// BlockLayoutMode implements block formatting context layout
type BlockLayoutMode struct{}

// InlineLayoutMode implements inline formatting context layout
type InlineLayoutMode struct{}

// FlexLayoutMode implements flexbox layout (to be implemented)
type FlexLayoutMode struct{}

func NewLayoutEngine(viewportWidth, viewportHeight float64) *LayoutEngine {
	le := &LayoutEngine{}
	le.viewport.width = viewportWidth
	le.viewport.height = viewportHeight
	le.counters = make(map[string][]int)
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

	// CSS Counter support: Process counter-reset on this element
	var counterResets map[string]int
	if resetVal, ok := style.Get("counter-reset"); ok {
		counterResets = parseCounterReset(resetVal)
		for name, value := range counterResets {
			le.counterReset(name, value)
		}
	}

	// Phase 2: Recursively layout children
	// Use box.X/Y which include relative positioning offset
	childY := box.Y + border.Top + padding.Top
	childAvailableWidth := contentWidth

	// For shrink-to-fit elements (floats, abs pos without explicit width), pass the parent's
	// available width to children so they can lay out naturally, then we'll shrink-wrap around them
	if contentWidth == 0 && (floatType != css.FloatNone || position == css.PositionAbsolute || position == css.PositionFixed) {
		childAvailableWidth = availableWidth - padding.Left - padding.Right - border.Left - border.Right
	}

	// Track previous block child for margin collapsing between siblings
	var prevBlockChild *Box
	var pendingMargins []float64 // margins from collapse-through elements

	// Phase 7: Track inline layout context
	inlineCtx := &InlineContext{
		LineX:      le.initializeLineX(box, border, padding, childY),
		LineY:      childY,
		LineHeight: 0,
		LineBoxes:  make([]*Box, 0),
	}

	// Phase 11: Generate ::before pseudo-element if it has content
	beforeBox := le.generatePseudoElement(node, "before", inlineCtx.LineX, inlineCtx.LineY, childAvailableWidth, computedStyles, box)
	if beforeBox != nil {
		beforeFloat := beforeBox.Style.GetFloat()
		if beforeFloat != css.FloatNone {
			// Position floated ::before pseudo-element
			floatWidth := le.getTotalWidth(beforeBox)
			// Pseudo-element floats position inline at current LineY, allowing overflow
			// rather than dropping to a new line like block-level floats
			floatY := inlineCtx.LineY
			leftOffset, rightOffset := le.getFloatOffsets(floatY)
			// Calculate new position
			var newX float64
			if beforeFloat == css.FloatLeft {
				// For left floats, position must clear both other floats (leftOffset) AND inline content (LineX)
				baseX := box.X + border.Left + padding.Left
				floatClearX := baseX + leftOffset + beforeBox.Margin.Left
				inlineEndX := inlineCtx.LineX + beforeBox.Margin.Left
				if inlineEndX > floatClearX {
					newX = inlineEndX
				} else {
					newX = floatClearX
				}
			} else {
				newX = box.X + border.Left + padding.Left + childAvailableWidth - rightOffset - floatWidth + beforeBox.Margin.Left
			}
			newY := floatY + beforeBox.Margin.Top

			// Calculate position delta to reposition children
			deltaX := newX - beforeBox.X
			deltaY := newY - beforeBox.Y

			// Reposition child boxes (e.g., images inside the pseudo-element)
			for _, child := range beforeBox.Children {
				child.X += deltaX
				child.Y += deltaY
			}

			beforeBox.X = newX
			beforeBox.Y = newY
			le.addFloat(beforeBox, beforeFloat, floatY)
			box.Children = append(box.Children, beforeBox)
		} else {
			box.Children = append(box.Children, beforeBox)
			// Update inline context for subsequent children
			beforeDisplay := beforeBox.Style.GetDisplay()
			if beforeDisplay == css.DisplayBlock {
				inlineCtx.LineY += le.getTotalHeight(beforeBox)
				inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
			} else {
				inlineCtx.LineX += le.getTotalWidth(beforeBox)
				if beforeBox.Height > inlineCtx.LineHeight {
					inlineCtx.LineHeight = beforeBox.Height
				}
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

	// Track block-in-inline for fragment splitting (CSS 2.1 §9.2.1.1)
	// When a block element is inside an inline element, the inline's borders are split
	isInlineParent := display == css.DisplayInline
	hasSeenBlockChild := false
	hasInlineContentBeforeBlock := false

	// Fragment tracking for block-in-inline
	// We track the bounding region of inline content to create fragments
	type fragmentRegion struct {
		startX, startY float64
		maxX, maxY     float64
		hasContent     bool
	}
	currentFragment := fragmentRegion{
		startX: box.X + border.Left + padding.Left,
		startY: box.Y + border.Top + padding.Top,
	}
	var completedFragments []fragmentRegion

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
				// Handle <br> elements - force a line break
				if child.TagName == "br" {
					// Move to next line
					if inlineCtx.LineHeight == 0 {
						inlineCtx.LineHeight = style.GetLineHeight()
					}
					inlineCtx.LineY += inlineCtx.LineHeight
					inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
					inlineCtx.LineHeight = 0
					inlineCtx.LineBoxes = make([]*Box, 0)
					// Don't add <br> to children - it's just a control element
					continue
				}

				// Phase 7: Handle inline and inline-block elements
				// Skip inline positioning for floated elements (they are positioned by float logic)
				childIsFloated := childStyle != nil && childStyle.GetFloat() != css.FloatNone
				if (childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock) && childBox.Position == css.PositionStatic && !childIsFloated {
					// Block-in-inline: mark inline content after a block as last fragment
					if isInlineParent && hasSeenBlockChild {
						childBox.IsLastFragment = true
					}
					if isInlineParent && !hasSeenBlockChild {
						hasInlineContentBeforeBlock = true
					}

					// Update fragment region with this inline child's bounds
					if isInlineParent {
						childRight := childBox.X + le.getTotalWidth(childBox)
						childBottom := childBox.Y + le.getTotalHeight(childBox)
						if childRight > currentFragment.maxX {
							currentFragment.maxX = childRight
						}
						if childBottom > currentFragment.maxY {
							currentFragment.maxY = childBottom
						}
						currentFragment.hasContent = true
					}

					childTotalWidth := le.getTotalWidth(childBox)

					// Check if child fits on current line (skip wrapping if white-space: nowrap)
					allowWrap := style.GetWhiteSpace() != css.WhiteSpaceNowrap
					if allowWrap && inlineCtx.LineX + childTotalWidth > box.X + border.Left + padding.Left + childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to next line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
						inlineCtx.LineHeight = 0
						inlineCtx.LineBoxes = make([]*Box, 0)

						// Reposition child at start of new line
						childBox.X = inlineCtx.LineX
						childBox.Y = inlineCtx.LineY
					} else {
						// Fits on current line - position it at the current LineX
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
					// Block-in-inline: when a block is inside an inline parent, mark fragments
					if isInlineParent && hasInlineContentBeforeBlock {
						// Complete the current fragment (content before the block)
						if currentFragment.hasContent {
							completedFragments = append(completedFragments, currentFragment)
						}
						// Start a new fragment for content after the block
						// (will be positioned after block layout is done)
						hasSeenBlockChild = true
						// Mark legacy flags for backward compatibility
						box.IsFirstFragment = true
					}

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
						// Calculate new position
					var newX float64
					if childBox.Margin.AutoLeft && childBox.Margin.AutoRight {
							childTotalW := childBox.Width + childBox.Padding.Left + childBox.Padding.Right + childBox.Border.Left + childBox.Border.Right
							parentContentStart := box.X + border.Left + padding.Left
							centerOff := (childAvailableWidth - childTotalW) / 2
							if centerOff < 0 { centerOff = 0 }
							newX = parentContentStart + centerOff
						} else {
							newX = box.X + border.Left + padding.Left + childBox.Margin.Left
						}
						newY := childY + childBox.Margin.Top + relativeOffsetY

						// Shift children by the position delta (important for block-in-inline)
						dx := newX - childBox.X
						dy := newY - childBox.Y
						if dx != 0 || dy != 0 {
							le.shiftChildren(childBox, dx, dy)
						}
						childBox.X = newX
						childBox.Y = newY
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
					inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
					inlineCtx.LineY = childY

					// Reset fragment tracking for next fragment (content after this block)
					if isInlineParent {
						currentFragment = fragmentRegion{
							startX: inlineCtx.LineX,
							startY: inlineCtx.LineY,
						}
					}
				}
			}
		} else if child.Type == html.TextNode {
			// Phase 6: Layout text nodes
			// Always use inline flow so text nodes participate in the inline
			// formatting context together with sibling inline elements (e.g. <em>).
			// layoutTextNode already handles float offsets internally, so pass the
			// original position and let it adjust for floats
			// Ensure LineX accounts for any floats that were added (e.g., floated ::before)
			le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
			textBox := le.layoutTextNode(
				child,
				inlineCtx.LineX,
				inlineCtx.LineY,
				box.X+border.Left+padding.Left+childAvailableWidth-inlineCtx.LineX,
				style, // Text inherits parent's style
				box,
			)
			if textBox != nil {
				// Block-in-inline: track and mark text fragments
				if isInlineParent {
					if hasSeenBlockChild {
						textBox.IsLastFragment = true
					} else {
						hasInlineContentBeforeBlock = true
					}
					// Update fragment region with this text's bounds
					textRight := textBox.X + le.getTotalWidth(textBox)
					textBottom := textBox.Y + le.getTotalHeight(textBox)
					if textRight > currentFragment.maxX {
						currentFragment.maxX = textRight
					}
					if textBottom > currentFragment.maxY {
						currentFragment.maxY = textBottom
					}
					currentFragment.hasContent = true
				}
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

					// Check if text fits on current line (skip wrapping if white-space: nowrap)
					allowWrap := style.GetWhiteSpace() != css.WhiteSpaceNowrap
					if allowWrap && inlineCtx.LineX+textWidth > box.X+border.Left+padding.Left+childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to new line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
						inlineCtx.LineHeight = textHeight
						textBox.X = inlineCtx.LineX
						textBox.Y = inlineCtx.LineY
						inlineCtx.LineX += textWidth
						le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
					} else {
						// Fits on current line (or is the first item on the line)
						inlineCtx.LineX += textWidth
						le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
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
		afterFloat := afterBox.Style.GetFloat()
		if afterFloat != css.FloatNone {
			// Position floated ::after pseudo-element
			floatWidth := le.getTotalWidth(afterBox)
			// Pseudo-element floats position inline at current LineY, allowing overflow
			// rather than dropping to a new line like block-level floats
			floatY := inlineCtx.LineY
			leftOffset, rightOffset := le.getFloatOffsets(floatY)

			// Calculate new position
			var newX float64
			if afterFloat == css.FloatLeft {
				// For left floats, position must clear both other floats (leftOffset) AND inline content (LineX)
				baseX := box.X + border.Left + padding.Left
				floatClearX := baseX + leftOffset + afterBox.Margin.Left
				inlineEndX := inlineCtx.LineX + afterBox.Margin.Left
				if inlineEndX > floatClearX {
					newX = inlineEndX
				} else {
					newX = floatClearX
				}
			} else {
				newX = box.X + border.Left + padding.Left + childAvailableWidth - rightOffset - floatWidth + afterBox.Margin.Left
			}
			newY := floatY + afterBox.Margin.Top

			// Calculate position delta to reposition children
			deltaX := newX - afterBox.X
			deltaY := newY - afterBox.Y

			// Reposition child boxes (e.g., images inside the pseudo-element)
			for _, child := range afterBox.Children {
				child.X += deltaX
				child.Y += deltaY
			}

			afterBox.X = newX
			afterBox.Y = newY
			le.addFloat(afterBox, afterFloat, floatY)
		}
		box.Children = append(box.Children, afterBox)
	}

	// Finalize block-in-inline fragments
	// If we're an inline parent that was split by block children, create the fragment boxes
	if isInlineParent && hasSeenBlockChild {
		// Complete the final fragment (content after the last block)
		if currentFragment.hasContent {
			completedFragments = append(completedFragments, currentFragment)
		}

		// Create BoxFragment objects for rendering
		for i, frag := range completedFragments {
			if !frag.hasContent {
				continue
			}

			// Determine which borders this fragment should have
			borders := AllBorders()
			if i == 0 {
				// First fragment: has left border, no right border
				borders.Right = false
			}
			if i == len(completedFragments)-1 {
				// Last fragment: has right border, no left border
				borders.Left = false
			}

			// Calculate fragment dimensions including padding/border
			fragWidth := frag.maxX - frag.startX + border.Left + border.Right + padding.Left + padding.Right
			fragHeight := frag.maxY - frag.startY + border.Top + border.Bottom + padding.Top + padding.Bottom

			box.AddFragment(
				frag.startX-border.Left-padding.Left,
				frag.startY-border.Top-padding.Top,
				fragWidth,
				fragHeight,
				borders,
			)
		}
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
		// Calculate width from children
		// For inline formatting context, children flow horizontally so we SUM their widths
		totalChildWidth := 0.0
		maxChildHeight := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			totalChildWidth += childWidth
			childHeight := le.getTotalHeight(child)
			if childHeight > maxChildHeight {
				maxChildHeight = childHeight
			}
		}
		box.Width = totalChildWidth
		box.Height = maxChildHeight
	}

	// Phase 5 Enhancement: Float shrink-wrapping
	// If this is a float without explicit width, shrink-wrap to content
	if floatType != css.FloatNone && !hasExplicitWidth && len(box.Children) > 0 {
		// For inline formatting context (inline children), sum widths horizontally
		// For block formatting context (block children), take max width (vertical stacking)
		allInline := true
		for _, child := range box.Children {
			if child.Style != nil {
				childDisplay := child.Style.GetDisplay()
				if childDisplay != css.DisplayInline && childDisplay != css.DisplayInlineBlock && child.Node != nil && child.Node.Type != html.TextNode {
					allInline = false
					break
				}
			}
		}

		if allInline {
			// Inline formatting context: sum widths
			totalWidth := 0.0
			for _, child := range box.Children {
				totalWidth += le.getTotalWidth(child)
			}
			if totalWidth > 0 {
				box.Width = totalWidth
			}
		} else {
			// Block formatting context: take max width
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

	// CSS Counter support: Pop counter scopes that were reset on this element
	if counterResets != nil {
		for name := range counterResets {
			le.counterPop(name)
		}
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

			// If parent box already has children (e.g., ::before pseudo-element),
			// then this text is not the first content
			if len(parent.Children) > 0 {
				isFirstContent = false
			}

			// If parent element will have ::after pseudo-element, text is not last content
			// Check by computing ::after style and seeing if it has content
			if parent.Style != nil && parent.Node != nil {
				afterStyle := css.ComputePseudoElementStyle(parent.Node, "after", le.stylesheets, le.viewport.width, le.viewport.height, parent.Style)
				if _, hasAfterContent := afterStyle.GetContentValues(); hasAfterContent {
					isLastContent = false
				}
			}

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
	adjustedY := y
	adjustedWidth := availableWidth

	// Get available space accounting for floats
	// NOTE: X position is now handled by the inline context, so we only adjust width
	leftOffset, rightOffset := le.getFloatOffsets(adjustedY)
	adjustedWidth -= (leftOffset + rightOffset)

	// Phase 6 Enhancement: Measure the text with correct font weight
	isBold := fontWeight == css.FontWeightBold
	width, _ := text.MeasureTextWithWeight(node.Text, fontSize, isBold)
	height := lineHeight // Phase 7 Enhancement: Use line-height for box height

	// Compute parent's content-area left edge and full width for wrapped lines.
	// The first line uses the remaining space (adjustedWidth), but subsequent
	// lines start at the parent's left edge and use the full content width.
	parentContentLeft := x
	parentContentWidth := availableWidth
	if parent != nil {
		parentContentLeft = parent.X + parent.Border.Left + parent.Padding.Left
		parentContentWidth = parent.Width
	}

	// CSS 2.1 §9.5: If a shortened line box is too small to contain any content,
	// then the line box is shifted downward until either some content fits or
	// there are no more floats present.
	if adjustedWidth < parentContentWidth && adjustedWidth > 0 {
		// Get the first word to check if it fits beside floats
		firstWord := text.GetFirstWord(node.Text)
		if firstWord != "" {
			firstWordWidth, _ := text.MeasureTextWithWeight(firstWord, fontSize, isBold)
			if firstWordWidth > adjustedWidth {
				// First word doesn't fit beside floats - drop below them
				newY := le.getClearY(css.ClearBoth, adjustedY)
				if newY > adjustedY {
					adjustedY = newY
					// Recalculate float offsets at new Y position
					leftOffset, rightOffset = le.getFloatOffsets(adjustedY)
					adjustedX = x + leftOffset
					adjustedWidth = availableWidth - (leftOffset + rightOffset)
				}
			}
		}
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
				Y:        adjustedY,
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
			currentY := adjustedY
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
		Y:        adjustedY,
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

// initializeLineX returns the starting X position for inline content in a box at the given Y position,
// accounting for left floats. This should be called when starting a new line or after the Y position changes.
func (le *LayoutEngine) initializeLineX(box *Box, border, padding css.BoxEdge, y float64) float64 {
	leftOffset, _ := le.getFloatOffsets(y)
	return box.X + border.Left + padding.Left + leftOffset
}

// ensureLineXClearsFloats updates the inline context's LineX to ensure it clears any left floats
// at the current Y position. This should be called after advancing LineX to verify constraints.
func (le *LayoutEngine) ensureLineXClearsFloats(inlineCtx *InlineContext, box *Box, border, padding css.BoxEdge) {
	minX := le.initializeLineX(box, border, padding, inlineCtx.LineY)
	if inlineCtx.LineX < minX {
		inlineCtx.LineX = minX
	}
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

		// Check for ::before pseudo-element with display: table-cell
		beforeStyle := css.ComputePseudoElementStyle(node, "before", le.stylesheets, le.viewport.width, le.viewport.height, style)
		if beforeStyle != nil && beforeStyle.GetDisplay() == css.DisplayTableCell {
			content, _ := beforeStyle.Get("content")
			if content != "" && content != "none" {
				// Strip quotes from content
				if len(content) >= 2 && ((content[0] == '"' && content[len(content)-1] == '"') ||
					(content[0] == '\'' && content[len(content)-1] == '\'')) {
					content = content[1 : len(content)-1]
				}
				// Create pseudo-element cell
				pseudoCell := &TableCell{
					Box:     &Box{Style: beforeStyle, PseudoContent: content},
					RowSpan: 1,
					ColSpan: 1,
					RowIdx:  *rowIdx,
					ColIdx:  colIdx,
				}
				for len((*cellGrid)[*rowIdx]) <= colIdx {
					(*cellGrid)[*rowIdx] = append((*cellGrid)[*rowIdx], nil)
				}
				(*cellGrid)[*rowIdx][colIdx] = pseudoCell
				colIdx++
			}
		}

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

		// Check for ::after pseudo-element with display: table-cell
		afterStyle := css.ComputePseudoElementStyle(node, "after", le.stylesheets, le.viewport.width, le.viewport.height, style)
		if afterStyle != nil && afterStyle.GetDisplay() == css.DisplayTableCell {
			content, _ := afterStyle.Get("content")
			if content != "" && content != "none" {
				// Strip quotes from content
				if len(content) >= 2 && ((content[0] == '"' && content[len(content)-1] == '"') ||
					(content[0] == '\'' && content[len(content)-1] == '\'')) {
					content = content[1 : len(content)-1]
				}
				// Create pseudo-element cell
				pseudoCell := &TableCell{
					Box:     &Box{Style: afterStyle, PseudoContent: content},
					RowSpan: 1,
					ColSpan: 1,
					RowIdx:  *rowIdx,
					ColIdx:  colIdx,
				}
				for len((*cellGrid)[*rowIdx]) <= colIdx {
					(*cellGrid)[*rowIdx] = append((*cellGrid)[*rowIdx], nil)
				}
				(*cellGrid)[*rowIdx][colIdx] = pseudoCell
				colIdx++
			}
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

			// Handle pseudo-element cells (have content but no DOM node)
			if cell.Box.Node == nil && cell.Box.PseudoContent != "" {
				// Measure and create text box for pseudo-content
				fontSize := cell.Box.Style.GetFontSize()
				fontWeight := cell.Box.Style.GetFontWeight()
				bold := fontWeight == css.FontWeightBold
				textWidth, textHeight := text.MeasureTextWithWeight(cell.Box.PseudoContent, fontSize, bold)
				textBox := &Box{
					Style:         cell.Box.Style,
					X:             childX,
					Y:             childY,
					Width:         textWidth,
					Height:        textHeight,
					Parent:        cell.Box,
					PseudoContent: cell.Box.PseudoContent,
				}
				cell.Box.Children = append(cell.Box.Children, textBox)
			} else if cell.Box.Node != nil {
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

	// Get parsed content values from pseudo-element style
	contentValues, hasContent := pseudoStyle.GetContentValues()
	if !hasContent || len(contentValues) == 0 {
		return nil
	}

	// CSS Counter support: Process counter-increment BEFORE evaluating content
	// This ensures counter() returns the incremented value
	if incVal, ok := pseudoStyle.Get("counter-increment"); ok {
		increments := parseCounterIncrement(incVal)
		for name, value := range increments {
			le.counterIncrement(name, value)
		}
	}

	// Create box model values
	margin := pseudoStyle.GetMargin()
	padding := pseudoStyle.GetPadding()
	border := pseudoStyle.GetBorderWidth()
	display := pseudoStyle.GetDisplay()
	fontSize := pseudoStyle.GetFontSize()
	fontWeight := pseudoStyle.GetFontWeight()
	bold := fontWeight == css.FontWeightBold

	// Get quotes from parent style (for open-quote/close-quote)
	quotes := []string{"\"", "\"", "'", "'"}
	if parentStyle != nil {
		if q, ok := parentStyle.Get("quotes"); ok {
			quotes = parseQuotes(q)
		}
	}

	// Build combined text content and collect images
	// Track pre-image text (before first image) and post-image text (after all images)
	var preImageText string
	var postImageText string
	var imageBoxes []*Box
	currentX := x + margin.Left + border.Left + padding.Left
	quoteDepth := 0
	seenImage := false

	for _, cv := range contentValues {
		switch cv.Type {
		case "text":
			if seenImage {
				postImageText += cv.Value
			} else {
				preImageText += cv.Value
			}
		case "url":
			seenImage = true
			// Create an image box for this URL
			var imgWidth, imgHeight float64
			if w, h, err := images.GetImageDimensionsWithFetcher(cv.Value, le.imageFetcher); err == nil {
				imgWidth = float64(w)
				imgHeight = float64(h)
			}
			// If dimensions fail to load, imgWidth and imgHeight remain 0 (placeholder)

			// Create style for image box (inline-block, not block)
			imgStyle := css.NewStyle()
			imgStyle.Set("display", "inline-block")

			imgBox := &Box{
				Node:      node,
				Style:     imgStyle,
				X:         currentX,
				Y:         y + margin.Top + border.Top + padding.Top,
				Width:     imgWidth,
				Height:    imgHeight,
				ImagePath: cv.Value, // Fetcher will resolve relative paths during rendering
			}
			imageBoxes = append(imageBoxes, imgBox)
			currentX += imgWidth
		case "counter":
			// Get the current value of the specified counter
			counterValue := le.counterValue(cv.Value)
			if seenImage {
				postImageText += strconv.Itoa(counterValue)
			} else {
				preImageText += strconv.Itoa(counterValue)
			}
		case "attr":
			// Get attribute value from the node
			if val, ok := node.GetAttribute(cv.Value); ok && val != "" {
				if seenImage {
					postImageText += val
				} else {
					preImageText += val
				}
			}
		case "open-quote":
			if quoteDepth*2 < len(quotes) {
				if seenImage {
					postImageText += quotes[quoteDepth*2]
				} else {
					preImageText += quotes[quoteDepth*2]
				}
			}
			quoteDepth++
		case "close-quote":
			if quoteDepth > 0 {
				quoteDepth--
			}
			if quoteDepth*2+1 < len(quotes) {
				if seenImage {
					postImageText += quotes[quoteDepth*2+1]
				} else {
					preImageText += quotes[quoteDepth*2+1]
				}
			}
		}
	}

	// Combine for total text content (used for non-wrapped layouts)
	textContent := preImageText + postImageText

	// Measure text dimensions
	var boxWidth, boxHeight float64
	var textWidth, textHeight float64
	if textContent != "" {
		textWidth, textHeight = text.MeasureTextWithWeight(textContent, fontSize, bold)
		boxWidth = textWidth
		boxHeight = textHeight
	}

	// Calculate total image width and max image height
	var imageWidth, maxImageHeight float64
	for _, imgBox := range imageBoxes {
		imageWidth += imgBox.Width
		if imgBox.Height > maxImageHeight {
			maxImageHeight = imgBox.Height
		}
	}
	boxWidth += imageWidth
	if maxImageHeight > boxHeight {
		boxHeight = maxImageHeight
	}

	// Check if this is a floated pseudo-element
	floatVal := pseudoStyle.GetFloat()

	// For floated elements, apply shrink-to-fit sizing with text wrapping (CSS 2.1 §10.3.5)
	// shrink-to-fit width = min(max(preferred minimum width, available width), preferred width)
	// For floats, browsers typically use min-content sizing when it produces a narrower result
	var wrappedPostLines []string
	var shrinkToFitWidth float64 // Declared at higher scope for use in child positioning
	// Use CSS default line-height: normal (approximately 1.2x font size)
	lineHeight := fontSize * 1.2

	// Track pre-image and post-image text widths for layout
	var preImageWidth, postImageWidth float64
	if preImageText != "" {
		preImageWidth, _ = text.MeasureTextWithWeight(preImageText, fontSize, bold)
	}
	if postImageText != "" {
		postImageWidth, _ = text.MeasureTextWithWeight(postImageText, fontSize, bold)
	}

	if floatVal != css.FloatNone && (textContent != "" || len(imageBoxes) > 0) {
		// Calculate preferred minimum width (min-content): the longest unbreakable unit
		// This includes the image width and the longest word in post-image text
		minContentWidth := preImageWidth + imageWidth
		if postImageText != "" {
			words := strings.Fields(postImageText)
			for _, word := range words {
				wordWidth, _ := text.MeasureTextWithWeight(word, fontSize, bold)
				if wordWidth > minContentWidth {
					minContentWidth = wordWidth
				}
			}
		}

		// Calculate preferred width (max-content): all content on one line
		// Use sum of individual text measurements to match actual child box widths
		maxContentWidth := preImageWidth + imageWidth + postImageWidth

		// Calculate available width
		maxAvailable := availableWidth - margin.Left - margin.Right - padding.Left - padding.Right - border.Left - border.Right

		// CSS 2.1 §10.3.5: shrink-to-fit = min(max(min-content, available), max-content)
		// For floated pseudo-elements, prefer keeping all content on one line
		shrinkToFitWidth = minContentWidth
		if maxAvailable > minContentWidth {
			shrinkToFitWidth = maxAvailable
		}
		if shrinkToFitWidth > maxContentWidth {
			shrinkToFitWidth = maxContentWidth
		}

		// For floated elements, always use shrink-to-fit width
		boxWidth = shrinkToFitWidth

		// Wrap postImageText only if content exceeds shrink-to-fit width
		if postImageText != "" && maxContentWidth > shrinkToFitWidth {
			// Calculate remaining space on first line after preImageText and images
			firstLineMax := shrinkToFitWidth - preImageWidth - imageWidth
			if firstLineMax < 0 {
				firstLineMax = 0
			}

			// Wrap only the post-image text
			wrappedPostLines = text.BreakTextIntoLinesWithWrap(postImageText, fontSize, bold, firstLineMax, shrinkToFitWidth)

			// Calculate height needed for all content
			numTextLines := len(wrappedPostLines)
			if numTextLines == 0 {
				// No wrapped text - height is just the first line (max of line height and image height)
				boxHeight = lineHeight
				if maxImageHeight > lineHeight {
					boxHeight = maxImageHeight
				}
			} else {
				// First line contains images + possibly first text line
				// Additional lines contain wrapped text
				firstLineHeight := lineHeight
				if maxImageHeight > firstLineHeight {
					firstLineHeight = maxImageHeight
				}
				additionalLines := numTextLines - 1
				if additionalLines < 0 {
					additionalLines = 0
				}
				boxHeight = firstLineHeight + float64(additionalLines)*lineHeight
			}
		}
	}

	// Block-level pseudo-elements: take available width unless floated
	if display == css.DisplayBlock && floatVal == css.FloatNone && (textContent != "" || len(imageBoxes) > 0) {
		totalWidth := availableWidth - margin.Left - margin.Right
		boxWidth = totalWidth - padding.Left - padding.Right - border.Left - border.Right
	}

	// Apply explicit height
	if h, ok := pseudoStyle.GetLength("height"); ok {
		boxHeight = h
	}

	// Create the pseudo-element box
	box := &Box{
		Node:          node,
		Style:         pseudoStyle,
		X:             x + margin.Left,
		Y:             y + margin.Top,
		Width:         boxWidth,
		Height:        boxHeight,
		Margin:        margin,
		Padding:       padding,
		Border:        border,
		Children:      make([]*Box, 0),
		Parent:        parent,
		PseudoContent: textContent,
	}

	// Add image boxes as children
	for _, imgBox := range imageBoxes {
		imgBox.Parent = box
		box.Children = append(box.Children, imgBox)
	}

	// For content with images, create child text boxes for proper inline layout and paint order
	// This prevents the parent's full text from being drawn and then covered by image children
	if len(imageBoxes) > 0 {
		// Clear PseudoContent from container so renderer draws children instead
		box.PseudoContent = ""

		contentX := x + margin.Left + border.Left + padding.Left
		contentY := y + margin.Top + border.Top + padding.Top

		// Line 1: preImageText (before images), then images (already positioned), then first wrapped line
		currentLineX := contentX

		// Add preImageText as a child box on line 1
		if preImageText != "" {
			// Create a minimal style for the text box (inline content)
			textStyle := css.NewStyle()
			textStyle.Set("display", "inline")
			// Inherit font properties from pseudo-element style
			if val, ok := pseudoStyle.Get("font-size"); ok {
				textStyle.Set("font-size", val)
			}
			if val, ok := pseudoStyle.Get("font-weight"); ok {
				textStyle.Set("font-weight", val)
			}
			if val, ok := pseudoStyle.Get("color"); ok {
				textStyle.Set("color", val)
			}

			preBox := &Box{
				Node:          node,
				Style:         textStyle,
				X:             currentLineX,
				Y:             contentY,
				Width:         preImageWidth,
				Height:        lineHeight,
				Margin:        css.BoxEdge{},
				Padding:       css.BoxEdge{},
				Border:        css.BoxEdge{},
				Children:      make([]*Box, 0),
				Parent:        box,
				PseudoContent: preImageText,
			}
			box.Children = append(box.Children, preBox)
			currentLineX += preImageWidth
		}

		// Images are already added as children - update their X positions
		for _, imgBox := range imageBoxes {
			imgBox.X = currentLineX
			currentLineX += imgBox.Width
		}

		// Add post-image text (either wrapped lines or single unwrapped line)
		if len(wrappedPostLines) > 0 {
			// Text wraps - add each wrapped line as a child box
			// Text continues on the same line after images if there's space
			// Wrapped lines start below the image if image is taller than line height
			firstLineBaseY := contentY
			wrappedLinesStartY := contentY + maxImageHeight
			if wrappedLinesStartY < contentY + lineHeight {
				wrappedLinesStartY = contentY + lineHeight
			}

			for i, line := range wrappedPostLines {
				lineWidth, _ := text.MeasureTextWithWeight(line, fontSize, bold)

				var lineX, lineY float64
				if i == 0 {
					// First line continues after preImageText and images
					lineX = currentLineX
					lineY = firstLineBaseY
				} else {
					// Subsequent lines wrap below the image
					lineX = contentX
					lineY = wrappedLinesStartY + float64(i-1)*lineHeight
				}

				// Create a minimal style for the text box (inline content)
				textStyle := css.NewStyle()
				textStyle.Set("display", "inline")
				// Inherit font properties from pseudo-element style
				if val, ok := pseudoStyle.Get("font-size"); ok {
					textStyle.Set("font-size", val)
				}
				if val, ok := pseudoStyle.Get("font-weight"); ok {
					textStyle.Set("font-weight", val)
				}
				if val, ok := pseudoStyle.Get("color"); ok {
					textStyle.Set("color", val)
				}

				// Create a pseudo-text child box for this line
				lineBox := &Box{
					Node:          node,
					Style:         textStyle,
					X:             lineX,
					Y:             lineY,
					Width:         lineWidth,
					Height:        lineHeight,
					Margin:        css.BoxEdge{},
					Padding:       css.BoxEdge{},
					Border:        css.BoxEdge{},
					Children:      make([]*Box, 0),
					Parent:        box,
					PseudoContent: line,
				}
				box.Children = append(box.Children, lineBox)
			}
		} else if postImageText != "" {
			// Text doesn't wrap - add unwrapped postImageText as single child box
			// postImageWidth was already measured earlier for shrink-to-fit calculation

			// Create a minimal style for the text box (inline content)
			textStyle := css.NewStyle()
			textStyle.Set("display", "inline")
			// Inherit font properties from pseudo-element style
			if val, ok := pseudoStyle.Get("font-size"); ok {
				textStyle.Set("font-size", val)
			}
			if val, ok := pseudoStyle.Get("font-weight"); ok {
				textStyle.Set("font-weight", val)
			}
			if val, ok := pseudoStyle.Get("color"); ok {
				textStyle.Set("color", val)
			}

			postBox := &Box{
				Node:          node,
				Style:         textStyle,
				X:             currentLineX,
				Y:             contentY,
				Width:         postImageWidth,
				Height:        lineHeight,
				Margin:        css.BoxEdge{},
				Padding:       css.BoxEdge{},
				Border:        css.BoxEdge{},
				Children:      make([]*Box, 0),
				Parent:        box,
				PseudoContent: postImageText,
			}
			box.Children = append(box.Children, postBox)
		}
	}

	// Update box dimensions to include all content
	if len(imageBoxes) > 0 || textContent != "" {
		box.Width = boxWidth
		box.Height = boxHeight
	}

	return box
}

// parseQuotes parses the CSS quotes property value
func parseQuotes(q string) []string {
	var quotes []string
	q = strings.TrimSpace(q)

	for len(q) > 0 {
		q = strings.TrimSpace(q)
		if len(q) == 0 {
			break
		}

		// Handle quoted strings
		if q[0] == '"' || q[0] == '\'' {
			quote := q[0]
			end := 1
			for end < len(q) && q[end] != quote {
				if q[end] == '\\' && end+1 < len(q) {
					end += 2
				} else {
					end++
				}
			}
			if end < len(q) {
				val := q[1:end]
				// Unescape Unicode escapes like \0022
				val = unescapeUnicode(val)
				quotes = append(quotes, val)
				q = q[end+1:]
			} else {
				break
			}
		} else {
			// Skip non-quoted content
			if idx := strings.IndexAny(q, " \t\"'"); idx > 0 {
				q = q[idx:]
			} else {
				break
			}
		}
	}

	return quotes
}

// unescapeUnicode converts CSS Unicode escapes like \0022 to actual characters
func unescapeUnicode(s string) string {
	result := s
	// Handle common escapes
	result = strings.ReplaceAll(result, "\\0022", "\"")
	result = strings.ReplaceAll(result, "\\0027", "'")
	result = strings.ReplaceAll(result, "\\00AB", "«")
	result = strings.ReplaceAll(result, "\\00BB", "»")
	return result
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
		// Use custom marker string (e.g., from list-style-type: "\2022")
		if string(listStyleType) != "" {
			markerText = string(listStyleType)
		} else {
			markerText = "•"
		}
	}

	// Measure marker text
	fontSize := style.GetFontSize()
	fontWeight := style.GetFontWeight()
	bold := fontWeight == css.FontWeightBold
	textWidth, textHeight := text.MeasureTextWithWeight(markerText, fontSize, bold)

	// Position marker to the left of the content (outside the content box)
	// CSS 2.1 §12.5.1: marker box is placed outside the principal box
	// Use 0.5em spacing between marker and content (typical browser behavior)
	markerSpacing := fontSize * 0.5
	markerX := x - textWidth - markerSpacing
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

// ============================================================================
// Intrinsic Size Computation
// ============================================================================

// ComputeIntrinsicSizes calculates the min-content and max-content widths for a node.
// These are fundamental for:
// - Shrink-to-fit width calculation (floats, inline-blocks, abs pos)
// - Flexbox flex-basis: auto
// - Table cell width calculation
// - CSS min-content/max-content width values
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
func (m *InlineLayoutMode) ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	return le.ComputeIntrinsicSizes(node, style, computedStyles)
}

// LayoutChildren for InlineLayoutMode - to be implemented as refactor progresses
func (m *InlineLayoutMode) LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box {
	// This will be filled in as we refactor layoutNode
	return nil
}

// ComputeIntrinsicSizes for FlexLayoutMode
func (m *FlexLayoutMode) ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	// Flex intrinsic sizing follows CSS Flexible Box Layout Module Level 1 §9.9
	// For now, delegate to block computation
	return le.ComputeIntrinsicSizes(node, style, computedStyles)
}

// LayoutChildren for FlexLayoutMode - to be implemented
func (m *FlexLayoutMode) LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box {
	// This will implement the full flex layout algorithm
	return nil
}

// ============================================================================
// Helper Functions for Fragment Rendering
// ============================================================================

// AddFragment adds a visual fragment to a box
func (b *Box) AddFragment(x, y, width, height float64, borders BorderEdgeFlags) {
	b.Fragments = append(b.Fragments, BoxFragment{
		X:       x,
		Y:       y,
		Width:   width,
		Height:  height,
		Borders: borders,
	})
}

// HasFragments returns true if this box should render as fragments
func (b *Box) HasFragments() bool {
	return len(b.Fragments) > 0
}

// GetBorderFlags returns which borders should be drawn based on fragment state
func (b *Box) GetBorderFlags() BorderEdgeFlags {
	// If we have explicit fragments, use the main box position only for the first
	if b.HasFragments() {
		return BorderEdgeFlags{} // Don't draw borders for main box, use fragments
	}

	// Legacy fragment flags for backward compatibility
	flags := AllBorders()
	if b.IsFirstFragment {
		flags.Right = false
	}
	if b.IsLastFragment {
		flags.Left = false
	}
	return flags
}

// CSS Counter support functions

// counterReset resets a counter to the specified value (default 0)
// This creates a new scope for the counter.
func (le *LayoutEngine) counterReset(name string, value int) {
	if le.counters == nil {
		le.counters = make(map[string][]int)
	}
	// Push a new value onto the counter's stack
	le.counters[name] = append(le.counters[name], value)
}

// counterIncrement increments a counter by the specified value (default 1)
func (le *LayoutEngine) counterIncrement(name string, value int) {
	if le.counters == nil {
		return
	}
	stack := le.counters[name]
	if len(stack) == 0 {
		// Counter wasn't reset - implicitly create it at 0
		le.counters[name] = []int{value}
	} else {
		// Increment the top of the stack
		le.counters[name][len(stack)-1] += value
	}
}

// counterValue returns the current value of a counter
func (le *LayoutEngine) counterValue(name string) int {
	if le.counters == nil {
		return 0
	}
	stack := le.counters[name]
	if len(stack) == 0 {
		return 0
	}
	return stack[len(stack)-1]
}

// counterPop removes the topmost scope of a counter (called when leaving an element that reset it)
func (le *LayoutEngine) counterPop(name string) {
	if le.counters == nil {
		return
	}
	stack := le.counters[name]
	if len(stack) > 0 {
		le.counters[name] = stack[:len(stack)-1]
	}
}

// parseCounterReset parses the counter-reset property value
// Format: "name [value] [name2 [value2] ...]" or "none"
func parseCounterReset(value string) map[string]int {
	result := make(map[string]int)
	value = strings.TrimSpace(value)
	if value == "" || value == "none" {
		return result
	}

	parts := strings.Fields(value)
	i := 0
	for i < len(parts) {
		name := parts[i]
		resetValue := 0
		if i+1 < len(parts) {
			// Check if next part is a number
			if v, err := strconv.Atoi(parts[i+1]); err == nil {
				resetValue = v
				i++
			}
		}
		result[name] = resetValue
		i++
	}
	return result
}

// parseCounterIncrement parses the counter-increment property value
// Format: "name [value] [name2 [value2] ...]" or "none"
func parseCounterIncrement(value string) map[string]int {
	result := make(map[string]int)
	value = strings.TrimSpace(value)
	if value == "" || value == "none" {
		return result
	}

	parts := strings.Fields(value)
	i := 0
	for i < len(parts) {
		name := parts[i]
		incValue := 1 // Default increment is 1
		if i+1 < len(parts) {
			// Check if next part is a number
			if v, err := strconv.Atoi(parts[i+1]); err == nil {
				incValue = v
				i++
			}
		}
		result[name] = incValue
		i++
	}
	return result
}

// ============================================================================
// Multi-pass Inline Layout (Blink-style three-phase approach)
// ============================================================================

// LayoutInlineContent is the main entry point for multi-pass inline layout.
// It orchestrates all three phases and returns the resulting boxes.
//
// This should be called instead of the old single-pass inline layout logic.
func (le *LayoutEngine) LayoutInlineContent(
	node *html.Node,
	box *Box,
	availableWidth float64,
	startY float64,
	border, padding css.BoxEdge,
	computedStyles map[*html.Node]*css.Style,
) []*Box {
	// Initialize state
	state := &InlineLayoutState{
		Items:              []*InlineItem{},
		Lines:              []*LineBreakResult{},
		ContainerBox:       box,
		ContainerStyle:     box.Style,
		AvailableWidth:     availableWidth,
		StartY:             startY,
		Border:             border,
		Padding:            padding,
		FloatList:          []FloatInfo{},
		FloatBaseIndex:     le.floatBase,
	}

	// Phase 1: Collect inline items
	for _, child := range node.Children {
		le.CollectInlineItems(child, state, computedStyles)
	}

	// Phase 2 & 3: Line breaking with retry when floats change available width
	// This implements the Gecko-style retry mechanism (RedoMoreFloats)
	const maxRetries = 3 // Prevent infinite loops
	var boxes []*Box

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Phase 2: Break into lines with current float state
		success := le.BreakLines(state)
		if !success {
			return []*Box{} // Line breaking failed
		}

		// Phase 3: Construct line boxes and layout floats
		// Returns boxes and whether retry is needed
		boxes, retryNeeded := le.constructLineBoxesWithRetry(state, box, computedStyles)

		if !retryNeeded {
			// Success - no floats changed available width
			return boxes
		}

		// Retry needed - a float changed available width
		// Phase 2 will be re-run with updated float list on next iteration
	}

	// Max retries exceeded - return what we have
	return boxes
}

// Phase 1: CollectInlineItems flattens the DOM tree into a sequential list of inline items.
// This converts the hierarchical structure into a flat array that's easier to process for line breaking.
//
// Example:
//   <p>Hello <em>world</em>!</p>
// Becomes:
//   [Text("Hello "), OpenTag(<em>), Text("world"), CloseTag(</em>), Text("!")]
func (le *LayoutEngine) CollectInlineItems(node *html.Node, state *InlineLayoutState, computedStyles map[*html.Node]*css.Style) {
	if node == nil {
		return
	}

	// Handle text nodes
	if node.Type == html.TextNode {
		if node.Text == "" {
			return
		}

		// Get parent style for text measurements
		parentStyle := state.ContainerStyle
		if node.Parent != nil {
			if style := computedStyles[node.Parent]; style != nil {
				parentStyle = style
			}
		}

		// Measure the text
		fontSize := parentStyle.GetFontSize()
		bold := parentStyle.GetFontWeight() == css.FontWeightBold
		width, height := text.MeasureTextWithWeight(node.Text, fontSize, bold)

		item := &InlineItem{
			Type:        InlineItemText,
			Node:        node,
			Text:        node.Text,
			StartOffset: 0,
			EndOffset:   len(node.Text),
			Style:       parentStyle,
			Width:       width,
			Height:      height,
		}
		state.Items = append(state.Items, item)
		return
	}

	// Handle element nodes
	if node.Type == html.ElementNode {
		style := computedStyles[node]
		if style == nil {
			style = css.NewStyle()
		}

		display := style.GetDisplay()

		// Skip display:none elements
		if display == css.DisplayNone {
			return
		}

		// Handle different display types
		switch display {
		case css.DisplayBlock, css.DisplayTable, css.DisplayListItem:
			// Block elements don't become inline items - they're handled separately
			// This shouldn't happen in a pure inline formatting context
			return

		case css.DisplayInline:
			// Check for floats
			if style.GetFloat() != css.FloatNone {
				// Floated inline elements become atomic items
				item := &InlineItem{
					Type:  InlineItemFloat,
					Node:  node,
					Style: style,
					// Width/height will be computed during layout
				}
				state.Items = append(state.Items, item)
				// Don't process children - they're part of the float box
				return
			}

			// Regular inline element - add open tag
			openItem := &InlineItem{
				Type:  InlineItemOpenTag,
				Node:  node,
				Style: style,
			}
			state.Items = append(state.Items, openItem)

			// Process children recursively
			for _, child := range node.Children {
				le.CollectInlineItems(child, state, computedStyles)
			}

			// Add close tag
			closeItem := &InlineItem{
				Type:  InlineItemCloseTag,
				Node:  node,
				Style: style,
			}
			state.Items = append(state.Items, closeItem)

		case css.DisplayInlineBlock:
			// Atomic inline element
			item := &InlineItem{
				Type:  InlineItemAtomic,
				Node:  node,
				Style: style,
				// Width/height will be computed during layout
			}
			state.Items = append(state.Items, item)
			// Don't process children - they're part of the atomic box

		default:
			// Other display types - treat as atomic for now
			item := &InlineItem{
				Type:  InlineItemAtomic,
				Node:  node,
				Style: style,
			}
			state.Items = append(state.Items, item)
		}
	}
}

// Phase 2: BreakLines determines what items go on each line, accounting for floats.
// This is where retry happens - if floats change available width, we re-break affected lines.
//
// Returns true if line breaking succeeded, false if retry is needed.
func (le *LayoutEngine) BreakLines(state *InlineLayoutState) bool {
	if len(state.Items) == 0 {
		return true // Nothing to break
	}

	state.Lines = nil // Clear any previous line breaking results
	currentY := state.StartY
	itemIndex := 0

	for itemIndex < len(state.Items) {
		// Start a new line
		line := &LineBreakResult{
			Y:          currentY,
			Items:      []*InlineItem{},
			StartIndex: itemIndex,
			TextBreaks: make(map[*InlineItem]struct {
				StartOffset int
				EndOffset   int
			}),
		}

		// Calculate available width for this line (accounting for floats)
		leftOffset, rightOffset := le.getFloatOffsets(currentY)
		line.AvailableWidth = state.AvailableWidth - leftOffset - rightOffset

		// Accumulate items on this line
		lineX := 0.0
		lineHeight := 0.0

		for itemIndex < len(state.Items) {
			item := state.Items[itemIndex]

			// Calculate item width
			itemWidth := 0.0
			itemHeight := 0.0

			switch item.Type {
			case InlineItemText:
				// For text, we might need to break it
				itemWidth = item.Width
				itemHeight = item.Height

				// Check if text fits on current line
				if lineX+itemWidth > line.AvailableWidth && len(line.Items) > 0 {
					// Text doesn't fit - need to break
					// For now, simple algorithm: break entire text to next line
					// TODO: Implement proper word breaking within text
					goto finishLine
				}

			case InlineItemOpenTag, InlineItemCloseTag:
				// Tags don't take space themselves
				itemWidth = 0
				itemHeight = 0

			case InlineItemAtomic, InlineItemFloat:
				// Atomic items have their own width/height
				itemWidth = item.Width
				itemHeight = item.Height

				if lineX+itemWidth > line.AvailableWidth && len(line.Items) > 0 {
					// Atomic item doesn't fit
					goto finishLine
				}

			case InlineItemControl:
				// Control items (like <br>) force a line break
				itemIndex++
				goto finishLine
			}

			// Add item to line
			line.Items = append(line.Items, item)
			lineX += itemWidth
			if itemHeight > lineHeight {
				lineHeight = itemHeight
			}

			itemIndex++
		}

	finishLine:
		// Finalize this line
		line.EndIndex = itemIndex
		line.LineHeight = lineHeight
		if line.LineHeight == 0 {
			// Use container's line-height as minimum
			line.LineHeight = state.ContainerStyle.GetLineHeight()
		}

		state.Lines = append(state.Lines, line)

		// Move to next line
		currentY += line.LineHeight

		// If we didn't make progress, force at least one item
		if itemIndex == line.StartIndex && itemIndex < len(state.Items) {
			// Force include at least one item to avoid infinite loop
			item := state.Items[itemIndex]
			line.Items = append(line.Items, item)
			line.EndIndex = itemIndex + 1
			itemIndex++
		}
	}

	return true // Line breaking succeeded
}

// Phase 3: ConstructLineBoxes creates actual positioned Box fragments from line breaking results.
// This is the final phase that produces the output fragment tree.
func (le *LayoutEngine) ConstructLineBoxes(state *InlineLayoutState, parent *Box) []*Box {
	boxes := []*Box{}

	for _, line := range state.Lines {
		// Calculate starting X for this line (accounting for floats)
		leftOffset, _ := le.getFloatOffsets(line.Y)
		currentX := state.ContainerBox.X + state.Border.Left + state.Padding.Left + leftOffset

		// Track open inline elements (for nested inline styling)
		type inlineContext struct {
			node  *html.Node
			style *css.Style
			box   *Box
		}
		openInlines := []inlineContext{}

		// Process each item on this line
		for _, item := range line.Items {
			switch item.Type {
			case InlineItemText:
				// Create a text box
				textBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, textBox)
				currentX += item.Width

			case InlineItemOpenTag:
				// Start tracking this inline element
				// Create a box for it (will be sized after seeing all children)
				inlineBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    0, // Will be computed from children
					Height:   line.LineHeight,
					Margin:   css.BoxEdge{},
					Padding:  item.Style.GetPadding(),
					Border:   item.Style.GetBorderWidth(),
					Position: css.PositionStatic,
					Parent:   parent,
				}
				openInlines = append(openInlines, inlineContext{
					node:  item.Node,
					style: item.Style,
					box:   inlineBox,
				})

			case InlineItemCloseTag:
				// Close the most recent inline element
				if len(openInlines) > 0 {
					ctx := openInlines[len(openInlines)-1]
					openInlines = openInlines[:len(openInlines)-1]

					// Compute width from current X - start X
					ctx.box.Width = currentX - ctx.box.X
					boxes = append(boxes, ctx.box)
				}

			case InlineItemAtomic:
				// Atomic inline element - it has its own dimensions
				atomicBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, atomicBox)
				currentX += item.Width

			case InlineItemFloat:
				// Floats are positioned separately by float logic
				// We don't position them here
				// TODO: Integrate with existing float positioning
			}
		}
	}

	return boxes
}

// constructLineBoxesWithRetry is like ConstructLineBoxes but also detects when floats
// change available width and signals that retry is needed.
// Returns (boxes, retryNeeded)
func (le *LayoutEngine) constructLineBoxesWithRetry(
	state *InlineLayoutState,
	parent *Box,
	computedStyles map[*html.Node]*css.Style,
) ([]*Box, bool) {
	boxes := []*Box{}
	retryNeeded := false

	for _, line := range state.Lines {
		// Calculate starting X for this line (accounting for floats)
		leftOffsetBefore, _ := le.getFloatOffsets(line.Y)
		currentX := state.ContainerBox.X + state.Border.Left + state.Padding.Left + leftOffsetBefore

		// Track open inline elements
		type inlineContext struct {
			node  *html.Node
			style *css.Style
			box   *Box
		}
		openInlines := []inlineContext{}

		// Process each item on this line
		for _, item := range line.Items {
			switch item.Type {
			case InlineItemText:
				textBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, textBox)
				currentX += item.Width

			case InlineItemOpenTag:
				inlineBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    0,
					Height:   line.LineHeight,
					Margin:   css.BoxEdge{},
					Padding:  item.Style.GetPadding(),
					Border:   item.Style.GetBorderWidth(),
					Position: css.PositionStatic,
					Parent:   parent,
				}
				openInlines = append(openInlines, inlineContext{
					node:  item.Node,
					style: item.Style,
					box:   inlineBox,
				})

			case InlineItemCloseTag:
				if len(openInlines) > 0 {
					ctx := openInlines[len(openInlines)-1]
					openInlines = openInlines[:len(openInlines)-1]
					ctx.box.Width = currentX - ctx.box.X
					boxes = append(boxes, ctx.box)
				}

			case InlineItemAtomic:
				atomicBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, atomicBox)
				currentX += item.Width

			case InlineItemFloat:
				// Layout the float to get its dimensions
				floatBox := le.layoutNode(
					item.Node,
					state.ContainerBox.X+state.Border.Left+state.Padding.Left,
					line.Y,
					state.AvailableWidth,
					computedStyles,
					parent,
				)

				if floatBox != nil {
					boxes = append(boxes, floatBox)

					// Add float to engine's float list
					floatType := item.Style.GetFloat()
					le.addFloat(floatBox, floatType, line.Y)

					// Check if this float changes available width for this line
					leftOffsetAfter, _ := le.getFloatOffsets(line.Y)
					if leftOffsetAfter != leftOffsetBefore {
						// Float changed available width - retry needed
						retryNeeded = true
					}
				}
			}
		}
	}

	return boxes, retryNeeded
}
