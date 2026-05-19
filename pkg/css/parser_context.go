package css

import (
	"regexp"
	"strings"

	"louis14/pkg/html"
)

// ParserContext mirrors Blink's `CSSParserContext`
// (third_party/blink/renderer/core/css/parser/css_parser_context.h:153 @
// chromium-main d4ecdfed88f962439247c2ad36b8fe47805b1520). It carries the
// base URL used to resolve `url()` references during a single CSS parse and
// is intended to be immutable for that parse's lifetime.
//
// Future fields (intentionally deferred until a test demands them): Charset,
// Referrer, IsOriginClean, IsAdRelated, Mode, Fetcher. Phase 6 of LOU-138
// adds Fetcher when @import resolution moves into ParseStylesheet.
//
// Methods are nil-safe: a nil receiver is treated as an empty context
// (BaseDir = ""). This lets tests pass nil for terseness without losing the
// Blink chokepoint shape — production code always constructs a real context.
type ParserContext struct {
	// BaseDir mirrors `CSSParserContext::base_url_`. Empty for top-level
	// documents (URLs stay relative; the renderer's fetcher resolves them
	// against its basePath); non-empty for nested documents and per-sheet
	// contexts introduced in later phases of LOU-138.
	BaseDir string
}

// NewParserContext returns a ParserContext with the given BaseDir.
func NewParserContext(baseDir string) *ParserContext {
	return &ParserContext{BaseDir: baseDir}
}

// NewParserContextFromDocument mirrors Blink's
// `CSSParserContext(const Document&)` constructor at
// core/css/parser/css_parser_context.cc:85 @ d4ecdfed8. The document's BaseDir
// becomes the base URL for inline `style=""` attributes and `<style>` blocks
// — anything that shares the document's URL identity.
func NewParserContextFromDocument(doc *html.Document) *ParserContext {
	return NewParserContext(doc.BaseDir)
}

// baseDir returns the context's BaseDir, treating a nil receiver as empty.
func (c *ParserContext) baseDir() string {
	if c == nil {
		return ""
	}
	return c.BaseDir
}

// CompleteURL mirrors `CSSParserContext::CompleteURL` at
// core/css/parser/css_parser_context.cc:202 @ d4ecdfed8. Resolution
// dispatches on the base form: filesystem-relative bases (WPT fixtures)
// use path.Join; absolute bases (`https://...`, `/...`) use
// net/url.ResolveReference per RFC 3986 § 5.2. See `pkg/css/url.go`.
func (c *ParserContext) CompleteURL(raw string) string {
	return CompleteURL(raw, c.baseDir())
}

// CollectUrlData mirrors
// `core/css/properties/css_parsing_utils.cc::CollectUrlData` at line 1777 @
// d4ecdfed8. This is the single chokepoint every `url()` token must funnel
// through during a parse: the raw inner string becomes the URLData's
// Relative form, and the context-resolved form becomes Absolute.
func (c *ParserContext) CollectUrlData(raw string) URLData {
	return URLData{Relative: raw, Absolute: c.CompleteURL(raw)}
}

// cssURLDoubleQuote matches url("...") with double quotes.
var cssURLDoubleQuote = regexp.MustCompile(`(?i)url\(\s*"([^"]+)"\s*\)`)

// cssURLSingleQuote matches url('...') with single quotes.
var cssURLSingleQuote = regexp.MustCompile(`(?i)url\(\s*'([^']+)'\s*\)`)

// cssURLNoQuote matches url(...) without quotes (no parens/quotes inside).
var cssURLNoQuote = regexp.MustCompile(`(?i)url\(\s*([^)"'\s][^)]*?)\s*\)`)

// RewriteURLs rewrites every relative `url(...)` token in cssText to its
// CompleteURL form against this context's BaseDir. Mirrors Blink's parse-time
// rewrite — each url() token in a property value funnels through
// CSSParserContext::CompleteURL before the resulting CSSUrlData is stored
// (core/css/properties/css_parsing_utils.cc::CollectUrlData :1777 @
// d4ecdfed8). louis14 applies the rewrite once at the source-text level so
// every consumer of Stylesheet.Rules / Style.Properties sees absolute URLs;
// the per-property typed-wrapper migration (Phase 7) moves the chokepoint
// down to per-token CollectUrlData calls.
//
// Empty / "." BaseDir is a no-op — top-level documents leave URLs relative
// for the renderer's fetcher. Already-absolute refs (scheme-prefixed, data:,
// root-relative) pass through unchanged via ResolveURL's short-circuit.
func (c *ParserContext) RewriteURLs(cssText string) string {
	baseDir := c.baseDir()
	if baseDir == "" || baseDir == "." {
		return cssText
	}
	// Most CSS rules contain no url() tokens; skip the three regex passes
	// when the text trivially has none.
	if !strings.Contains(cssText, "url(") && !strings.Contains(cssText, "URL(") {
		return cssText
	}
	return rewriteCSSURLs(cssText, func(match, uri, quote string) string {
		resolved := ResolveURL(uri, baseDir)
		if resolved == uri {
			return match
		}
		return "url(" + quote + resolved + quote + ")"
	})
}

// rewriteCSSURLs walks each url(...) reference in cssText across the three
// quoting variants (double, single, none) and replaces each with the result
// of fn(match, uri, quote).
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
