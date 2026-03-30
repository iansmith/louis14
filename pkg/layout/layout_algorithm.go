package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
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
type LayoutContext struct {
	// ComputedStyles maps DOM nodes to their computed CSS styles.
	ComputedStyles map[*html.Node]*css.Style

	// Viewport dimensions for resolving viewport-relative units.
	ViewportWidth  float64
	ViewportHeight float64

	// ImageFetcher for loading images during layout (intrinsic sizing).
	ImageFetcher images.ImageFetcher
}
