package layout

import (
	"path"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
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

	// BaseDir is the owning document's BaseDir (the value propagated to
	// each computed Style by ApplyStylesToDocument). Nested-document
	// layout (layoutNestedDocument) joins it with the iframe's src
	// directory so URLs inside the nested doc resolve against a
	// fully-qualified-relative-to-outer base.
	BaseDir string

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

	// ComputedStyles is the per-DOM-node style map produced by
	// css.ApplyStylesToDocument and stored on LayoutEngine. SVG
	// descendants of an inline <svg> are not LayoutInputNodes (they
	// live under the SVGRoot subtree, not under the CSS box tree),
	// so the SVGRootAlgorithm needs this map to attach computed
	// style to each SVGShape at build time. Populated by
	// LayoutEngine.Layout before laying out the document root.
	// Nil-tolerant: SVG descendants without a style entry fall back
	// to SVG property defaults.
	ComputedStyles map[*html.Node]*css.Style
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

// ReRootedDocumentFetcher returns a DocumentFetcher that resolves relative URIs
// relative to nestedDocURI. Absolute URIs (starting with "/") are passed through.
// This allows a nested document's sub-resources to be resolved correctly.
func ReRootedDocumentFetcher(outer DocumentFetcher, nestedDocURI string) DocumentFetcher {
	if outer == nil {
		return nil
	}
	// Compute the directory portion of the nested doc URI.
	// For "support/foo.html", base = "support/".
	base := path.Dir(nestedDocURI)
	if base == "." {
		return outer
	}
	return func(uri string) (string, error) {
		if strings.HasPrefix(uri, "/") || strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "data:") {
			return outer(uri)
		}
		return outer(path.Join(base, uri))
	}
}

// ReRootedImageFetcher returns an ImageFetcher that resolves relative URIs
// relative to nestedDocURI. Absolute URIs are passed through unchanged.
func ReRootedImageFetcher(outer images.ImageFetcher, nestedDocURI string) images.ImageFetcher {
	if outer == nil {
		return nil
	}
	base := path.Dir(nestedDocURI)
	if base == "." {
		return outer
	}
	return func(uri string) ([]byte, error) {
		if strings.HasPrefix(uri, "/") || strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "data:") {
			return outer(uri)
		}
		return outer(path.Join(base, uri))
	}
}
