package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
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
	Children      []*Box           // Phase 2: Nested boxes
	Parent        *Box             // Phase 4: Parent box for containing block
	Position      css.PositionType // Phase 4: Position type
	ZIndex        int              // Phase 4: Stacking order
	ImagePath     string           // Phase 8: Image source path for img elements
	PseudoContent string           // Phase 11: Content for pseudo-elements

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
	scrollY        float64             // Scroll offset for fixed positioning (viewport-relative)
	absoluteBoxes  []*Box              // Phase 4: Track absolutely positioned boxes
	floats         []FloatInfo         // Phase 5: Track floated elements
	floatBaseStack []int               // Stack of float base indices for BFC boundaries
	floatBase      int                 // Current BFC float base index
	stylesheets    []*css.Stylesheet   // Phase 11: Store stylesheets for pseudo-elements
	imageFetcher   images.ImageFetcher // Optional fetcher for network images

	// CSS Counters support
	counters map[string][]int // Counter name -> stack of values (for nested scopes)

	// NEW ARCHITECTURE: Flag to enable clean multi-pass inline layout
	// When true, uses LayoutInlineContentToBoxes instead of old single-pass
	useMultiPass bool
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
//
// NEW ARCHITECTURE: Immutable data structures for correct multi-pass layout
// Based on Blink LayoutNG principles - see docs/MULTIPASS-REDESIGN.md

// Rect represents a rectangular region
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// Exclusion represents a float that affects inline layout.
// Immutable - created once with correct dimensions.
type Exclusion struct {
	Rect Rect         // Position and size of the float
	Side css.FloatType // FloatLeft or FloatRight
}

// ExclusionSpace tracks all floats affecting inline layout.
// IMMUTABLE - Add() returns a NEW ExclusionSpace instead of modifying the original.
// This prevents float accumulation bugs during retry iterations.
type ExclusionSpace struct {
	exclusions []Exclusion // List of active float exclusions
}

// Size represents dimensions (width and height)
type Size struct {
	Width  float64
	Height float64
}

// Position represents a 2D coordinate
type Position struct {
	X float64
	Y float64
}

// ConstraintSpace packages all constraints for laying out a subtree.
// IMMUTABLE - create modified copies using helper methods instead of mutation.
// This prevents stale constraint bugs during retry iterations.
type ConstraintSpace struct {
	AvailableSize  Size             // Available width and height for content
	ExclusionSpace *ExclusionSpace  // Floats affecting inline layout
	TextAlign      css.TextAlign    // Text alignment for inline content
	NoWrap         bool             // white-space: nowrap - prevent line breaking
	// TODO: Add more constraints as needed:
	// - WritingMode
	// - IsNewFormattingContext
	// - Baseline offsets
}

// FragmentType represents the type of a fragment (output of layout).
type FragmentType int

const (
	FragmentText       FragmentType = iota // Text node
	FragmentInline                         // Inline box (span, em, etc.)
	FragmentBlock                          // Block box (div, p, etc.)
	FragmentFloat                          // Floated box
	FragmentAtomic                         // Atomic inline (inline-block, img, etc.)
	FragmentBlockChild                     // Block child (requires recursive layout)
)

// Fragment represents the immutable output of layout.
// Unlike Box (which is mutable and repositioned), Fragment is created with
// the correct position from the start and never modified.
//
// This is the key to preventing positioning bugs:
// - No position deltas or adjustments
// - No recursive repositioning
// - Position is correct when created
//
// For now, Fragment is only used in the new multi-pass inline layout.
// Eventually, it may replace Box entirely (full LayoutNG-style architecture).
type Fragment struct {
	Node     *html.Node   // Source DOM node (can be nil for anonymous fragments)
	Style    *css.Style   // Computed style
	Position Position     // Correct final position (not relative!)
	Size     Size         // Content size
	Children []*Fragment  // Child fragments (owned by this fragment)
	Type     FragmentType // Type of fragment

	// For text fragments
	Text string // Text content (for FragmentText)

	// For image fragments
	ImagePath string // Image source path for img elements

	// For fragments that correspond to Box tree (temporary bridge)
	Box *Box // Link to Box tree (for converting back to Box)
}

// MinMaxSizes represents the intrinsic sizing information for an element.
// These are the "content-based" sizes (CSS Sizing Level 3):
// - MinContentSize: minimum size without overflow (narrowest the content can be)
// - MaxContentSize: preferred size without wrapping (widest the content wants to be)
//
// For text: min = longest word, max = full text width
// For inline boxes with inline children: min = max child min, max = sum of child max
// For inline boxes with block children: min = max child min, max = max child max
type MinMaxSizes struct {
	MinContentSize float64 // Minimum content size (narrowest without overflow)
	MaxContentSize float64 // Maximum content size (preferred width without wrapping)
}

// InlineItemType represents the type of an inline item
type InlineItemType int

const (
	InlineItemText       InlineItemType = iota // Text content
	InlineItemOpenTag                          // Opening tag of inline element
	InlineItemCloseTag                         // Closing tag of inline element
	InlineItemAtomic                           // Atomic inline (inline-block, replaced element)
	InlineItemFloat                            // Floated element
	InlineItemControl                          // Control element (br, etc.)
	InlineItemBlockChild                       // Block-level child (requires recursive layout)
)

// InlineItem represents a piece of inline content in the flattened item list.
// This is Phase 1 (CollectInlineItems) output - a sequential representation of all inline content.
type InlineItem struct {
	Type InlineItemType
	Node *html.Node // Source DOM node

	// For text items
	Text        string // Text content
	StartOffset int    // Start offset in original text
	EndOffset   int    // End offset in original text

	// For all items
	Style *css.Style // Computed style

	// Cached measurements (computed during collection)
	Width  float64 // Intrinsic width (for atomic items, measured text width)
	Height float64 // Intrinsic height
}

// LineInfo represents a single line in the new multi-pass architecture.
// This is the cleaner Phase 2 (BreakLines) output that uses ConstraintSpace.
type LineInfo struct {
	Y          float64          // Y position of this line
	Items      []*InlineItem    // Items on this line
	Constraint *ConstraintSpace // Constraint space for THIS line (includes floats)
	Height     float64          // Computed line height
}

// LineBreakResult represents the result of line breaking for a single line.
// This is Phase 2 (BreakLines) output - what items go on each line.
// NOTE: This is the old structure. New code should use LineInfo instead.
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
	ContainerBox   *Box
	ContainerStyle *css.Style
	AvailableWidth float64
	StartY         float64
	Border         css.BoxEdge
	Padding        css.BoxEdge

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
	Rows           []*TableRow
	NumCols        int
	ColumnWidths   []float64
	RowHeights     []float64
	BorderSpacing  float64
	BorderCollapse css.BorderCollapse
}

// FlexItem tracks a flex item during flex layout
type FlexItem struct {
	Box                  *Box
	FlexGrow             float64
	FlexShrink           float64
	FlexBasis            float64
	HypotheticalMainSize float64 // flex base size clamped by min/max
	MainSize             float64 // Size along main axis (after flex resolution)
	CrossSize            float64 // Size along cross axis
	MainPos              float64 // Position along main axis
	CrossPos             float64 // Position along cross axis
	Order                int
	AutoMinMain          float64 // min-width/min-height: auto value (content-based minimum)
}

// FlexLine tracks a line of flex items (for wrapping)
type FlexLine struct {
	Items     []*FlexItem
	MainSize  float64 // Total size of items along main axis
	CrossSize float64 // Maximum cross size in this line
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

// BoxFragment represents a visual fragment of a box
// When an inline box is split by a block element, it renders as multiple fragments
type BoxFragment struct {
	X, Y, Width, Height float64
	Borders             BorderEdgeFlags
}

// LineBox represents a line of inline content
// This is an explicit representation of CSS line boxes for proper inline layout
type LineBox struct {
	Y         float64 // Y position of the line box
	Height    float64 // Height of the line box
	Boxes     []*Box  // Inline-level boxes on this line
	BaselineY float64 // Y position of the alphabetic baseline (relative to line top)
	LeftEdge  float64 // Left edge of available space (accounting for floats)
	RightEdge float64 // Right edge of available space (accounting for floats)
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

// InlineLayoutAlgorithm represents different inline layout implementations
type InlineLayoutAlgorithm int

const (
	InlineLayoutSinglePass InlineLayoutAlgorithm = iota
	InlineLayoutMultiPass
)

// InlineLayoutResult holds the result of inline layout
type InlineLayoutResult struct {
	// ChildBoxes contains all the laid-out child boxes (including pseudo-elements)
	ChildBoxes []*Box
	// FinalInlineCtx contains the final state of the inline context after layout
	FinalInlineCtx *InlineContext
	// UsedMultiPass indicates whether multi-pass mode was actually used
	UsedMultiPass bool
	// Legacy fields (may not be used in all paths)
	Boxes         []*Box
	Height        float64 // Total height of all lines
	LastBaselineY float64 // Y position of last line's baseline
}
