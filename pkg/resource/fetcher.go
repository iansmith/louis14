package resource

import (
	"fmt"
	"strings"

	stdnet "louis14/std/net"
)

// Fetcher retrieves resources by URI.
type Fetcher interface {
	Fetch(uri string) (body []byte, contentType string, err error)
}

// DefaultFetcher fetches resources over HTTP/HTTPS, resolving relative URIs
// against a base URL.
type DefaultFetcher struct {
	baseURL string
}

// NewFetcher creates a DefaultFetcher with the given base URL.
// Relative URIs passed to Fetch will be resolved against this base.
func NewFetcher(baseURL string) *DefaultFetcher {
	return &DefaultFetcher{baseURL: baseURL}
}

// Fetch retrieves the resource at the given URI.
// Relative URIs are resolved against the fetcher's base URL.
func (f *DefaultFetcher) Fetch(uri string) ([]byte, string, error) {
	resolved := uri
	if !stdnet.IsNetworkURL(uri) && f.baseURL != "" {
		resolved = stdnet.ResolveURL(f.baseURL, uri)
	}
	if !stdnet.IsNetworkURL(resolved) {
		return nil, "", fmt.Errorf("cannot fetch non-network URI: %s", resolved)
	}
	return stdnet.Fetch(resolved)
}

// FetchCSS fetches a stylesheet URI and returns its text content.
// Returns an error if the content type does not look like CSS or text.
func (f *DefaultFetcher) FetchCSS(uri string) (string, error) {
	body, contentType, err := f.Fetch(uri)
	if err != nil {
		return "", err
	}
	// Accept text/css, text/plain, or any text/* content type
	ct := strings.ToLower(contentType)
	if ct != "" && !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "css") {
		return "", fmt.Errorf("unexpected content type for CSS: %s", contentType)
	}
	return string(body), nil
}

// FetchImage fetches an image URI and returns its raw bytes.
func (f *DefaultFetcher) FetchImage(uri string) ([]byte, error) {
	body, _, err := f.Fetch(uri)
	if err != nil {
		return nil, err
	}
	return body, nil
}
