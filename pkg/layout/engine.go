package layout

import (
	"louis14/pkg/html"
	"louis14/pkg/images"
)

// LayoutEngine performs CSS layout, producing a tree of positioned Box fragments.
type LayoutEngine struct {
	viewport     viewport
	imageFetcher images.ImageFetcher
	scrollY      float64
}

type viewport struct {
	width  float64
	height float64
}

// NewLayoutEngine creates a new layout engine with the given viewport dimensions.
func NewLayoutEngine(viewportWidth, viewportHeight float64) *LayoutEngine {
	return &LayoutEngine{
		viewport: viewport{width: viewportWidth, height: viewportHeight},
	}
}

// SetImageFetcher sets the image fetcher for loading network images during layout.
func (le *LayoutEngine) SetImageFetcher(fetcher images.ImageFetcher) {
	le.imageFetcher = fetcher
}

// SetScrollY sets the vertical scroll offset for fixed positioning.
func (le *LayoutEngine) SetScrollY(scrollY float64) {
	le.scrollY = scrollY
}

// GetScrollY returns the current vertical scroll offset.
func (le *LayoutEngine) GetScrollY() float64 {
	return le.scrollY
}

// Layout performs CSS layout on the document and returns a tree of positioned boxes.
//
// STUB: Returns a single root box covering the viewport.
// This will be replaced with the real layout algorithm.
func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
	root := &Box{
		X:      0,
		Y:      0,
		Width:  le.viewport.width,
		Height: le.viewport.height,
	}
	return []*Box{root}
}
