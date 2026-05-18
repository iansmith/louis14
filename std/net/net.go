package net

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const userAgent = "louis14/1.0 (compatible; Go)"

// httpClient is a shared HTTP client with reasonable timeouts.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// Fetch retrieves the content at the given URL via HTTP/HTTPS.
// Returns the response body, content type, and any error.
func Fetch(rawURL string) (body []byte, contentType string, err error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, rawURL)
	}

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response body: %w", err)
	}

	contentType = resp.Header.Get("Content-Type")
	return body, contentType, nil
}

// ResolveURL resolves a possibly-relative URI against a base URL.
// If ref is already absolute, it is returned as-is.
func ResolveURL(base, ref string) string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return ref
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return baseURL.ResolveReference(refURL).String()
}

// IsNetworkURL returns true if the string looks like an HTTP or HTTPS URL.
func IsNetworkURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// IsFileURL returns true if the string is a file:// URL.
func IsFileURL(s string) bool {
	return strings.HasPrefix(s, "file://")
}

// IsDataURL returns true if the string is a data: URL.
func IsDataURL(s string) bool {
	return strings.HasPrefix(s, "data:")
}

// DecodeDataURL parses a `data:[<mediatype>][;base64],<data>` URL per RFC 2397.
// Returns the decoded body, the media type (empty if unspecified), and any error.
// Supports both base64-encoded and URL-encoded (percent-encoded) payloads.
func DecodeDataURL(s string) (body []byte, mediaType string, err error) {
	if !strings.HasPrefix(s, "data:") {
		return nil, "", fmt.Errorf("not a data URL: %q", s)
	}
	rest := s[len("data:"):]
	commaIdx := strings.IndexByte(rest, ',')
	if commaIdx < 0 {
		return nil, "", fmt.Errorf("malformed data URL: missing comma")
	}
	meta := rest[:commaIdx]
	payload := rest[commaIdx+1:]

	isBase64 := false
	if strings.HasSuffix(meta, ";base64") {
		isBase64 = true
		mediaType = strings.TrimSuffix(meta, ";base64")
	} else {
		mediaType = meta
	}
	// Strip any other parameters (e.g. ";charset=utf-8") from the trailing portion;
	// callers only need the core media type. Keep it simple and pass through.

	if isBase64 {
		// Per RFC 2397 the base64 payload may contain whitespace which we strip.
		clean := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
				return -1
			}
			return r
		}, payload)
		body, err = base64.StdEncoding.DecodeString(clean)
		if err != nil {
			return nil, mediaType, fmt.Errorf("base64 decode: %w", err)
		}
		return body, mediaType, nil
	}

	// URL-encoded payload.
	decoded, err := url.QueryUnescape(payload)
	if err != nil {
		return nil, mediaType, fmt.Errorf("url decode: %w", err)
	}
	return []byte(decoded), mediaType, nil
}

// FileURLToPath converts a `file://` URL to a local filesystem path.
// Accepts `file:///foo/bar` and `file://localhost/foo/bar` (the latter per RFC 8089).
// Returns an error for malformed input.
func FileURLToPath(s string) (string, error) {
	if !strings.HasPrefix(s, "file://") {
		return "", fmt.Errorf("not a file URL: %q", s)
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("parse file URL: %w", err)
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("file URL with non-local host: %q", u.Host)
	}
	if u.Path == "" {
		return "", fmt.Errorf("file URL missing path: %q", s)
	}
	return u.Path, nil
}
