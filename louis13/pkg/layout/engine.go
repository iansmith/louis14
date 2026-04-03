package layout

import (
	"louis14/pkg/images"
)

func NewLayoutEngine(viewportWidth, viewportHeight float64) *LayoutEngine {
	le := &LayoutEngine{}
	le.viewport.width = viewportWidth
	le.viewport.height = viewportHeight
	le.counters = make(map[string][]int)
	le.useMultiPass = true // Multi-pass is now the default (investigating block-in-inline-003 regression)
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

// SetUseMultiPass enables the new clean multi-pass inline layout architecture.
// When enabled, inline content uses LayoutInlineContentToBoxes (Phase 1-2-3 pipeline)
// instead of the old single-pass algorithm.
//
// This is used for selective testing: enable for specific test files to measure
// improvement before full rollout.
func (le *LayoutEngine) SetUseMultiPass(enabled bool) {
	le.useMultiPass = enabled
}

// GetScrollY returns the current vertical scroll offset.
func (le *LayoutEngine) GetScrollY() float64 {
	return le.scrollY
}
