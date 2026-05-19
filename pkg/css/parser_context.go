package css

import "louis14/pkg/html"

// ParserContext mirrors Blink's `CSSParserContext`
// (third_party/blink/renderer/core/css/parser/css_parser_context.h:153 @
// chromium-main d4ecdfed88f962439247c2ad36b8fe47805b1520). It carries the
// base URL used to resolve `url()` references during a single CSS parse and
// is intended to be immutable for that parse's lifetime.
//
// Future fields (intentionally deferred until a test demands them): Charset,
// Referrer, IsOriginClean, IsAdRelated, Mode, Fetcher. Phase 6 of LOU-138
// adds Fetcher when @import resolution moves into ParseStylesheet.
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

// CompleteURL mirrors `CSSParserContext::CompleteURL` at
// core/css/parser/css_parser_context.cc:202 @ d4ecdfed8. Phase 1 preserves
// the existing path.Join-based ResolveURL semantics; Phase 4 of LOU-138
// swaps in net/url-based RFC 3986 composition so scheme-prefixed bases
// survive intact.
func (c *ParserContext) CompleteURL(raw string) string {
	return ResolveURL(raw, c.BaseDir)
}

// CollectUrlData mirrors
// `core/css/properties/css_parsing_utils.cc::CollectUrlData` at line 1777 @
// d4ecdfed8. This is the single chokepoint every `url()` token must funnel
// through during a parse: the raw inner string becomes the URLData's
// Relative form, and the context-resolved form becomes Absolute.
func (c *ParserContext) CollectUrlData(raw string) URLData {
	return URLData{Relative: raw, Absolute: c.CompleteURL(raw)}
}
