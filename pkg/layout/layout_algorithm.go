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

// resolveURLInCSSMatch resolves a CSS url() match via css.ResolveURL —
// the single source of truth shared with inline-style (parse-time)
// resolution. The original `match` is returned unchanged when the URL
// is already absolute, preserving whitespace inside the original
// `url(...)` form.
func resolveURLInCSSMatch(match, uri, quote, baseDir string) string {
	resolved := css.ResolveURL(uri, baseDir)
	if resolved == uri {
		return match
	}
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

// ResolveRelativeURLsInHTML rewrites relative URL references inside
// <style> blocks of a complete HTML document string so that they are
// rooted at baseDir. Used when loading nested documents (iframes) so
// sub-resources (background images, mask images, …) consumed via raw
// string values resolve against the nested doc's directory.
//
// Inline `style=""` attribute URLs are NOT rewritten here — those flow
// through parse-time resolution in pkg/css's `parseFilterList` /
// `Style.BaseDir` path, mirroring Blink's `CSSUrlData` parse-time-resolve
// model (`core/css/css_url_data.cc:82-95` @ chromium-main
// bf955d02bf0b0c67868b2e62359c0af199af9acc). Pre-baking would force the
// cascade to double-resolve when an element moves cross-document.
//
// Migrating the <style>-block path to parse-time resolution is deferred
// — it requires threading BaseDir into the stylesheet parser proper.
// Moved DOM nodes don't take their `<style>` with them, so the cross-
// document-move correctness target the rest of this refactor addresses
// isn't at risk from the deferred <style>-block path.
func ResolveRelativeURLsInHTML(htmlContent, baseDir string) string {
	if baseDir == "" || baseDir == "." {
		return htmlContent
	}
	styleBlockPattern := regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)
	return styleBlockPattern.ReplaceAllStringFunc(htmlContent, func(block string) string {
		inner := styleBlockPattern.FindStringSubmatch(block)
		if inner == nil {
			return block
		}
		rewritten := ResolveRelativeURLsInCSS(inner[1], baseDir)
		return strings.Replace(block, inner[1], rewritten, 1)
	})
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
