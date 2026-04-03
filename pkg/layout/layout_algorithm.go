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

// DocumentFetcher returns HTML content for a given URI. Used to load
// nested documents for iframe/object elements during layout.
type DocumentFetcher func(uri string) (string, error)

// LayoutContext carries shared state needed by all layout algorithms.
// Algorithms receive this context rather than being methods on a god object.
// Style access is through LayoutInputNode.Style(), not a map lookup.
type LayoutContext struct {
	// Viewport dimensions for resolving viewport-relative units.
	ViewportWidth  float64
	ViewportHeight float64

	// ImageFetcher for loading images during layout (intrinsic sizing).
	ImageFetcher images.ImageFetcher

	// DocumentFetcher for loading nested documents (iframe, object).
	DocumentFetcher DocumentFetcher

	// FontConfig provides font paths for text measurement, including @font-face fonts.
	// If zero-value, DefaultFontConfig() is used.
	FontConfig text.FontConfig

	// OrthogonalLayoutCache caches layout results for orthogonal children
	// during min/max sizing to avoid redundant layouts and detect cycles.
	// Keyed by LayoutInputNode pointer. Mirrors Blink's NGLayoutCacheStatus.
	OrthogonalLayoutCache map[*LayoutInputNode]*orthogonalCacheEntry
}

// orthogonalCacheEntry stores a cached layout result or a "computing" sentinel.
type orthogonalCacheEntry struct {
	Computing bool          // true while layout is in progress (cycle detection)
	Result    *LayoutResult // cached result, nil if still computing
}

// GetOrthogonalLayout returns a cached layout result for an orthogonal child,
// or nil if not cached. Returns (nil, true) if a cycle is detected.
func (ctx *LayoutContext) GetOrthogonalLayout(node *LayoutInputNode) (*LayoutResult, bool) {
	if ctx.OrthogonalLayoutCache == nil {
		return nil, false
	}
	entry, ok := ctx.OrthogonalLayoutCache[node]
	if !ok {
		return nil, false
	}
	if entry.Computing {
		return nil, true // cycle detected
	}
	return entry.Result, false
}

// SetOrthogonalComputing marks a node as being computed (cycle detection sentinel).
func (ctx *LayoutContext) SetOrthogonalComputing(node *LayoutInputNode) {
	if ctx.OrthogonalLayoutCache == nil {
		ctx.OrthogonalLayoutCache = make(map[*LayoutInputNode]*orthogonalCacheEntry)
	}
	ctx.OrthogonalLayoutCache[node] = &orthogonalCacheEntry{Computing: true}
}

// SetOrthogonalResult stores a cached layout result for an orthogonal child.
func (ctx *LayoutContext) SetOrthogonalResult(node *LayoutInputNode, result *LayoutResult) {
	if ctx.OrthogonalLayoutCache == nil {
		ctx.OrthogonalLayoutCache = make(map[*LayoutInputNode]*orthogonalCacheEntry)
	}
	ctx.OrthogonalLayoutCache[node] = &orthogonalCacheEntry{Result: result}
}
