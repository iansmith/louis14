package svg

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	stdnet "louis14/std/net"
)

// FetchedSVGDocument is the result of resolving an external SVG document
// referenced from a `filter: url(...)`, `mask: url(...)`, or similar
// SVG-resource property. Mirrors Blink's
// SVGResourceDocumentContent (third_party/blink/renderer/core/svg/
// svg_resource_document_content.h) — Blink owns the parsed Document and
// hands out SVGResources by id; louis14 mirrors the same idea but in a
// lighter form: callers parse and id-lookup downstream once the bytes
// arrive.
//
// Tainted=true when the resource is cross-origin without explicit CORS
// (louis14 currently has no `crossorigin` attribute path, so all
// cross-origin filter loads are tainted). Per Filter Effects 1 §3.1
// tainted external filters render as a null filter (downstream pipeline
// falls back to the source unfiltered).
type FetchedSVGDocument struct {
	// Data is the raw bytes of the resolved SVG document (or data: URL
	// payload).
	Data []byte
	// ResolvedURL is the absolute URL after relative resolution. Used
	// as the cache key.
	ResolvedURL string
	// Origin is the document's origin: scheme://host[:port] for
	// http(s) URLs, the host origin for data: URLs (data: URLs inherit
	// the host's origin per Fetch spec), and the directory containing
	// the file for file:// URLs.
	Origin string
	// Tainted is true when the resource is cross-origin without
	// matching CORS — downstream must treat the filter as a null filter
	// per Filter Effects 1 §3.1.
	Tainted bool
}

// NetworkFetcher is the minimal HTTP-fetch interface that SVGDocumentFetcher
// uses for `http(s)://` resources. Mirrors `pkg/resource.Fetcher` but
// declared locally so this package stays free of upward dependencies
// (`pkg/resource` itself depends on `pkg/render` and `pkg/layout`).
type NetworkFetcher interface {
	Fetch(uri string) (body []byte, contentType string, err error)
}

// SVGDocumentFetcher resolves SVG documents referenced by an external
// URL on `filter: url(...)`. Mirrors Blink's
// ExternalSVGResourceDocumentContent::Load(Document&, CrossOriginAttributeValue)
// (third_party/blink/renderer/core/svg/svg_resource.cc:227-283 @
// bf955d02bf0b0c67868b2e62359c0af199af9acc):
//
//   - data: URLs decode inline (no network).
//   - file:// URLs read from the local filesystem; treated as
//     same-origin per the test-fixture convention.
//   - http(s):// URLs delegate to the supplied NetworkFetcher; the
//     result is tainted if the URL's origin differs from the host
//     document's origin (no crossorigin attribute support yet).
//
// Cache by resolved URL — Blink's `if (document_content_) return;`
// idempotence guard at svg_resource.cc:241.
type SVGDocumentFetcher struct {
	base       NetworkFetcher
	hostOrigin string

	mu    sync.Mutex
	cache map[string]*FetchedSVGDocument
}

// NewSVGDocumentFetcher constructs a fetcher. base may be nil — in that
// case http(s):// URLs fail with an explicit error (data:/file:// still
// work). hostOrigin is the host document's origin (`scheme://host[:port]`
// form) used for cross-origin classification.
func NewSVGDocumentFetcher(base NetworkFetcher, hostOrigin string) *SVGDocumentFetcher {
	return &SVGDocumentFetcher{
		base:       base,
		hostOrigin: hostOrigin,
		cache:      make(map[string]*FetchedSVGDocument),
	}
}

// FetchSVGForFilter resolves and fetches an SVG document referenced by
// a `filter: url(...)` value. The input is the URL part (no fragment).
// Returns a *FetchedSVGDocument or an error if the resource cannot be
// fetched. Cross-origin http(s) results return ok with Tainted=true so
// the caller can apply the Filter Effects 1 §3.1 null-filter fallback.
func (f *SVGDocumentFetcher) FetchSVGForFilter(uri string) (*FetchedSVGDocument, error) {
	if uri == "" {
		return nil, fmt.Errorf("empty URI")
	}

	// Cache check (under lock) — Blink's idempotent early return.
	f.mu.Lock()
	if cached, ok := f.cache[uri]; ok {
		f.mu.Unlock()
		return cached, nil
	}
	f.mu.Unlock()

	doc, err := f.fetchUncached(uri)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	// Re-check after the fetch in case of a concurrent populate; first
	// writer wins so callers see a stable pointer.
	if cached, ok := f.cache[uri]; ok {
		f.mu.Unlock()
		return cached, nil
	}
	f.cache[uri] = doc
	f.mu.Unlock()
	return doc, nil
}

// fetchUncached performs the actual resolve+read without touching the
// cache.
func (f *SVGDocumentFetcher) fetchUncached(uri string) (*FetchedSVGDocument, error) {
	switch {
	case stdnet.IsDataURL(uri):
		body, _, err := stdnet.DecodeDataURL(uri)
		if err != nil {
			return nil, fmt.Errorf("data URL: %w", err)
		}
		return &FetchedSVGDocument{
			Data:        body,
			ResolvedURL: uri,
			Origin:      f.hostOrigin,
			Tainted:     false,
		}, nil

	case stdnet.IsFileURL(uri):
		p, err := stdnet.FileURLToPath(uri)
		if err != nil {
			return nil, fmt.Errorf("file URL: %w", err)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
		return &FetchedSVGDocument{
			Data:        data,
			ResolvedURL: uri,
			Origin:      "file://" + filepath.Dir(p),
			Tainted:     false,
		}, nil

	case stdnet.IsNetworkURL(uri):
		if f.base == nil {
			return nil, fmt.Errorf("no network fetcher configured for %s", uri)
		}
		body, _, err := f.base.Fetch(uri)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", uri, err)
		}
		origin, originErr := networkOrigin(uri)
		if originErr != nil {
			return nil, originErr
		}
		tainted := f.hostOrigin != "" && !sameOrigin(f.hostOrigin, origin)
		return &FetchedSVGDocument{
			Data:        body,
			ResolvedURL: uri,
			Origin:      origin,
			Tainted:     tainted,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported URI scheme: %s", uri)
	}
}

// SplitFilterURL splits a `filter: url(...)` ref into (docURL, fragID).
// A bare `#foo` yields ("", "foo"); `foo.svg#bar` yields ("foo.svg",
// "bar"); a URL with no fragment yields (url, ""). Matches Blink's
// KURL::FragmentIdentifier convention used by SVGResource ref parsing.
func SplitFilterURL(ref string) (docURL, fragID string) {
	hashIdx := strings.IndexByte(ref, '#')
	if hashIdx < 0 {
		return ref, ""
	}
	return ref[:hashIdx], ref[hashIdx+1:]
}

// networkOrigin returns the `scheme://host[:port]` origin of a network
// URL. Returns an error for malformed inputs.
func networkOrigin(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("URL missing scheme or host: %s", rawURL)
	}
	return u.Scheme + "://" + u.Host, nil
}

// sameOrigin compares two `scheme://host[:port]` strings byte-equal.
// hostOrigin may be empty (e.g. for tests with no document URL) — in
// that case sameOrigin returns true so cross-origin tainting is
// suppressed.
func sameOrigin(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	return a == b
}
