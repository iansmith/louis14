package layout

import (
	"louis14/pkg/images"
	"louis14/pkg/text"
)

// LayoutAlgorithm is the interface for all formatting context algorithms.
// Each display type (block, flex, table, grid) implements this interface.
//
// Ported from Blink's LayoutAlgorithm pattern.
type LayoutAlgorithm interface {
	// Layout performs layout and returns the result.
	Layout() *LayoutResult
}

// LayoutContext carries shared state needed by all layout algorithms.
// Algorithms receive this context rather than being methods on a god object.
// Style access is through LayoutInputNode.Style(), not a map lookup.
type LayoutContext struct {
	// Viewport dimensions for resolving viewport-relative units.
	ViewportWidth  float64
	ViewportHeight float64

	// ImageFetcher for loading images during layout (intrinsic sizing).
	ImageFetcher images.ImageFetcher

	// FontConfig provides font paths for text measurement, including @font-face fonts.
	// If zero-value, DefaultFontConfig() is used.
	FontConfig text.FontConfig
}
