package render

import (
	"errors"

	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// externalSVGCache fetches external SVG documents (filter: url(doc#id)) for
// one paint pass, remembering each document's bytes and each filter built
// from it, so every resolver — an SVG element's lone url(), a filter
// chain's url() stage, the empty-paint-box check, a CSS layer's filter —
// fetches and parses a document once per paint. A nil cache has no fetcher.
//
// no Blink analog: Blink's SVGResourceDocumentCache holds external
// documents across the page's lifetime; louis14 re-fetches per paint.
type externalSVGCache struct {
	fetchFn func(uri string) ([]byte, error)
	bytes   map[string]fetchResult
	filters map[externalFilterKey]*svg.SVGResourceFilter
}

type fetchResult struct {
	data []byte
	err  error
}

// externalFilterKey names a built external filter: its document, its
// fragment, and the viewport its lengths resolve against.
type externalFilterKey struct {
	docURL, fragID string
	viewport       geometry.SizeF
}

// newExternalSVGCache returns a cache over fetch, or nil when there is no
// fetcher.
func newExternalSVGCache(fetch func(uri string) ([]byte, error)) *externalSVGCache {
	if fetch == nil {
		return nil
	}
	return &externalSVGCache{fetchFn: fetch, bytes: map[string]fetchResult{}, filters: map[externalFilterKey]*svg.SVGResourceFilter{}}
}

// fetch returns uri's document, fetching it on first use.
func (c *externalSVGCache) fetch(uri string) ([]byte, error) {
	if c == nil {
		return nil, errNoExternalSVGFetcher
	}
	if r, ok := c.bytes[uri]; ok {
		return r.data, r.err
	}
	data, err := c.fetchFn(uri)
	c.bytes[uri] = fetchResult{data, err}
	return data, err
}

// filter returns the <filter id="fragID"> of docURL built against
// lengthCtx's viewport, building it on first use; nil when it does not
// resolve.
func (c *externalSVGCache) filter(docURL, fragID string, lengthCtx svg.SVGLengthContext) *svg.SVGResourceFilter {
	if c == nil || fragID == "" {
		return nil
	}
	key := externalFilterKey{docURL, fragID, lengthCtx.ViewportSize()}
	if f, ok := c.filters[key]; ok {
		return f
	}
	f := externalSVGFilter(c.fetch, docURL, fragID, lengthCtx)
	c.filters[key] = f
	return f
}

var errNoExternalSVGFetcher = errors.New("render: no external SVG fetcher")
