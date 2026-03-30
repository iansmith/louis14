package layout

import (
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

	// Inline fragment tracking.
	IsFirstFragment bool
	IsLastFragment  bool

	// Line boxes for inline formatting contexts.
	LineBoxes []*LineBox
}

// LineBox represents a line in an inline formatting context.
type LineBox struct {
	Y      float64
	Height float64
	Boxes  []*Box
}
