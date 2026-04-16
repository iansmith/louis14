package layout

import (
	"strconv"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// Box represents a CSS box in the fragment tree.
// This is the output of layout — an immutable positioned fragment
// with physical coordinates for painting.
type Box struct {
	Node   *html.Node
	Style  *css.Style
	X, Y   float64
	Width  float64
	Height float64

	Margin  css.BoxEdge
	Padding css.BoxEdge
	Border  css.BoxEdge

	Children []*Box
	Parent   *Box

	Position css.PositionType
	ZIndex   int

	// Content for replaced elements and pseudo-elements.
	ImagePath     string
	PseudoContent string

	// MarkerStyle is the computed ::marker pseudo-element style, if any.
	// Set during layout tree building for display:list-item elements.
	MarkerStyle   *css.Style
	MarkerContent string // Resolved ::marker content text (may include counter values)

	// Text holds the rendered text content for inline text boxes.
	Text string

	// IsVerticalText is true when text should be rendered vertically
	// (writing-mode is vertical-rl, vertical-lr, sideways-rl, or sideways-lr).
	IsVerticalText bool

	// IsSidewaysLR is true for sideways-lr writing mode, where inline direction
	// is bottom-to-top (characters drawn from bottom to top within the fragment).
	IsSidewaysLR bool

	// IsSidewaysRL is true for sideways-rl writing mode, where the entire text
	// line is rotated 90° CW (like horizontal text read by tilting head right).
	IsSidewaysRL bool

	// Inline fragment tracking.
	IsFirstFragment bool
	IsLastFragment  bool

	// Line boxes for inline formatting contexts.
	LineBoxes []*LineBox

	// LayoutNode is the LayoutInputNode that produced this box.
	// Nil for anonymous boxes, text fragments, and line boxes.
	// Provides the Box→LayoutInputNode direction of the bidirectional link
	// used by BuildPaintTree to walk children in DOM tree order.
	LayoutNode *LayoutInputNode

	// DOMIndex is a pre-order index in the DOM tree, used to ensure correct
	// paint ordering when out-of-flow children propagate to ancestor boxes.
	DOMIndex int

	// ClipContentToBorderBox forces the paint layer to clip children to
	// this box's border-box regardless of the computed `overflow` style.
	// Carried from the producing PhysicalFragment (see layout_result.go).
	// Used by CSS Tables 3 §5.4.1 rowspan-over-collapsed-row clipping.
	ClipContentToBorderBox bool
}

// CreatesStackingContext returns true if this box establishes a new
// stacking context per CSS 2.1 Appendix E / CSS Compositing §2.
//
// Stacking contexts are created by:
// - Positioned elements with an explicit integer z-index (not auto)
// - Elements with opacity < 1
func (b *Box) CreatesStackingContext() bool {
	if b.Style == nil {
		return false
	}
	// Positioned + explicit z-index.
	if b.Position != css.PositionStatic && b.Style.HasExplicitZIndex() {
		return true
	}
	// CSS Flexbox §4.3: flex items with explicit z-index create stacking contexts
	// even when position is static.
	if b.Style.HasExplicitZIndex() && b.Parent != nil && b.Parent.Style != nil {
		parentDisplay := b.Parent.Style.GetDisplay()
		if parentDisplay == css.DisplayFlex || parentDisplay == css.DisplayInlineFlex {
			return true
		}
	}
	// opacity < 1.
	if opacity, ok := b.Style.Get("opacity"); ok {
		if o, err := strconv.ParseFloat(opacity, 64); err == nil && o < 1.0 {
			return true
		}
	}
	// CSS Transforms: elements with a transform create a stacking context.
	if transforms := b.Style.GetTransforms(); len(transforms) > 0 {
		return true
	}
	// CSS Filters: elements with a filter create a stacking context.
	if filters := b.Style.GetFilter(); len(filters) > 0 {
		return true
	}
	// CSS backdrop-filter: elements with a backdrop-filter create a stacking context.
	if filters := b.Style.GetBackdropFilter(); len(filters) > 0 {
		return true
	}
	// CSS mix-blend-mode: elements with non-normal blend mode create a stacking context.
	if bm := b.Style.GetMixBlendMode(); bm != css.MixBlendModeNormal && bm != "" {
		return true
	}
	// CSS Containment: layout and paint containment create a stacking context.
	if b.Style.HasLayoutContainment() || b.Style.HasPaintContainment() {
		return true
	}
	// CSS Compositing Level 1 §2: isolation: isolate creates a stacking context.
	if b.Style.GetIsolation() == "isolate" {
		return true
	}
	// CSS Will Change Level 1 §2.2: will-change of certain properties creates
	// a stacking context (same properties that create one when actually set).
	for _, prop := range b.Style.GetWillChange() {
		switch prop {
		case "transform", "opacity", "filter", "backdrop-filter",
			"clip-path", "mask", "mix-blend-mode", "isolation",
			"perspective", "offset-path":
			return true
		}
	}
	// CSS Flexbox §4.3: A flex item with an explicit z-index creates a
	// stacking context even if position is static.
	if b.IsFlexItem() && b.Style.HasExplicitZIndex() {
		return true
	}
	return false
}

// IsFlexItem returns true if this box is a child of a flex container.
func (b *Box) IsFlexItem() bool {
	if b.Parent == nil || b.Parent.Style == nil {
		return false
	}
	d := b.Parent.Style.GetDisplay()
	return d == css.DisplayFlex || d == css.DisplayInlineFlex
}

// LineBox represents a line in an inline formatting context.
type LineBox struct {
	Y      float64
	Height float64
	Boxes  []*Box
}
