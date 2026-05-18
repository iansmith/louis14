package layout

import (
	"path"
	"regexp"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/text"
)

// cssURLDoubleQuote matches url("...") with double quotes.
var cssURLDoubleQuote = regexp.MustCompile(`(?i)url\(\s*"([^"]+)"\s*\)`)

// cssURLSingleQuote matches url('...') with single quotes.
var cssURLSingleQuote = regexp.MustCompile(`(?i)url\(\s*'([^']+)'\s*\)`)

// cssURLNoQuote matches url(...) without quotes (no parens/quotes inside).
var cssURLNoQuote = regexp.MustCompile(`(?i)url\(\s*([^)"'\s][^)]*?)\s*\)`)

// resolveURLInCSSMatch resolves a CSS url() match, prepending baseDir for relative URIs.
func resolveURLInCSSMatch(match, uri, quote, baseDir string) string {
	if strings.HasPrefix(uri, "/") || strings.HasPrefix(uri, "http://") ||
		strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "data:") {
		return match
	}
	resolved := path.Join(baseDir, uri)
	return "url(" + quote + resolved + quote + ")"
}

// rewriteCSSURLs walks each url(...) reference in cssText across the three
// quoting variants (double, single, none) and replaces each with the result
// of fn(uri, quote). fn returns the new full match text (e.g. "url("+q+uri'+q+")").
func rewriteCSSURLs(cssText string, fn func(match, uri, quote string) string) string {
	cssText = cssURLDoubleQuote.ReplaceAllStringFunc(cssText, func(match string) string {
		groups := cssURLDoubleQuote.FindStringSubmatch(match)
		if groups == nil {
			return match
		}
		return fn(match, groups[1], `"`)
	})
	cssText = cssURLSingleQuote.ReplaceAllStringFunc(cssText, func(match string) string {
		groups := cssURLSingleQuote.FindStringSubmatch(match)
		if groups == nil {
			return match
		}
		return fn(match, groups[1], `'`)
	})
	cssText = cssURLNoQuote.ReplaceAllStringFunc(cssText, func(match string) string {
		groups := cssURLNoQuote.FindStringSubmatch(match)
		if groups == nil {
			return match
		}
		return fn(match, groups[1], ``)
	})
	return cssText
}

// ResolveRelativeURLsInCSS rewrites relative CSS url() references to be
// relative to baseDir. Absolute URLs (data:, http://, https://, /) are left unchanged.
func ResolveRelativeURLsInCSS(cssText, baseDir string) string {
	if baseDir == "" || baseDir == "." {
		return cssText
	}
	return rewriteCSSURLs(cssText, func(match, uri, quote string) string {
		return resolveURLInCSSMatch(match, uri, quote, baseDir)
	})
}

// stripBaseURLsInCSS reverses ResolveRelativeURLsInCSS for a single baseDir.
// Used by JS cross-document adoptNode to un-bake URLs that were pre-resolved
// against the iframe document at parse time, so they re-resolve against the
// outer document at paint time.
func stripBaseURLsInCSS(cssText, baseDir string) string {
	if baseDir == "" || baseDir == "." {
		return cssText
	}
	prefix := baseDir + "/"
	return rewriteCSSURLs(cssText, func(match, uri, quote string) string {
		if !strings.HasPrefix(uri, prefix) {
			return match
		}
		return "url(" + quote + strings.TrimPrefix(uri, prefix) + quote + ")"
	})
}

// tagIframeBase recursively sets IframeBase on every ElementNode in the
// subtree. TextNodes carry no style attribute so the marker is irrelevant
// there. Called by layoutNestedDocument so JS cross-document moves can find
// the pre-baked prefix to reverse.
func tagIframeBase(n *html.Node, base string) {
	if n == nil {
		return
	}
	if n.Type == html.ElementNode {
		n.IframeBase = base
	}
	for _, c := range n.Children {
		tagIframeBase(c, base)
	}
}

// AdoptNodeFromIframe walks n (the node being moved cross-document) and any
// descendants, reversing the iframe-base URL pre-baking on each node's inline
// style attribute and clearing IframeBase. Mirrors Blink's
// Document::adoptNode: when a node is adopted into a new owner document, its
// CSS URLs re-resolve against the new owner. louis14 stores the pre-baked
// prefix on the node so we can strip it; clearing IframeBase makes subsequent
// re-adoption a no-op.
func AdoptNodeFromIframe(n *html.Node) {
	if n == nil || n.IframeBase == "" {
		return
	}
	// Fast-path: regex passes are pure overhead for the typical inline style
	// (color/dimension/transform with no url() reference). A substring test
	// avoids three regex scans per node when nothing needs rewriting.
	if style, ok := n.Attributes["style"]; ok && strings.Contains(style, "url(") {
		n.Attributes["style"] = stripBaseURLsInCSS(style, n.IframeBase)
	}
	n.IframeBase = ""
	for _, c := range n.Children {
		AdoptNodeFromIframe(c)
	}
}

// ResolveRelativeURLsInHTML rewrites relative URL references in a complete
// HTML document string (inline styles and <style> blocks) so that they are
// rooted at baseDir. This is used when loading nested documents (iframes)
// so that sub-resources (background images, etc.) resolve correctly when
// painted by the outer renderer.
func ResolveRelativeURLsInHTML(htmlContent, baseDir string) string {
	if baseDir == "" || baseDir == "." {
		return htmlContent
	}
	// Rewrite url() references in <style>...</style> blocks.
	styleBlockPattern := regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)
	htmlContent = styleBlockPattern.ReplaceAllStringFunc(htmlContent, func(block string) string {
		// Find the content between <style> tags.
		inner := styleBlockPattern.FindStringSubmatch(block)
		if inner == nil {
			return block
		}
		rewritten := ResolveRelativeURLsInCSS(inner[1], baseDir)
		return strings.Replace(block, inner[1], rewritten, 1)
	})
	// Rewrite url() references in style="" attributes.
	styleAttrPattern := regexp.MustCompile(`(?i)style="([^"]*)"`)
	htmlContent = styleAttrPattern.ReplaceAllStringFunc(htmlContent, func(attr string) string {
		inner := styleAttrPattern.FindStringSubmatch(attr)
		if inner == nil {
			return attr
		}
		rewritten := ResolveRelativeURLsInCSS(inner[1], baseDir)
		return `style="` + rewritten + `"`
	})
	return htmlContent
}

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
