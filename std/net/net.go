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
// The louis14 counterpart of Blink's network_utils::ParseDataURL
// (platform/network/network_utils.h:38 @
// 5992657f556bcd9d207a4aca187488462f027cab), minus the response half: the
// payload is percent-decoded ('+' stays '+'; an invalid escape such as an
// SVG's width='100%' passes through), then base64-decoded when the header
// says ;base64 (whitespace in the base64 payload is ignored).
func DecodeDataURL(s string) (body []byte, mediaType string, err error) {
	if !IsDataURL(s) {
		return nil, "", fmt.Errorf("not a data URL: %q", DataURLForDisplay(s))
	}
	rest := s[len("data:"):]
	commaIdx := strings.IndexByte(rest, ',')
	if commaIdx < 0 {
		return nil, "", fmt.Errorf("malformed data URL: missing comma")
	}
	meta := rest[:commaIdx]
	payload := lenientPercentDecode(rest[commaIdx+1:])

	mediaType, isBase64 := strings.CutSuffix(meta, ";base64")
	if !isBase64 {
		return []byte(payload), mediaType, nil
	}
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(asciiWhitespace, r) {
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

// asciiWhitespace is the fetch spec's ASCII whitespace set.
const asciiWhitespace = " \t\n\r\f"

// DataURLForDisplay returns s for a log or error message: a data: URL keeps
// only its header, never its (possibly megabytes-long) payload; any other
// URL is returned unchanged.
func DataURLForDisplay(s string) string {
	if !IsDataURL(s) {
		return s
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		return fmt.Sprintf("%s,<%d bytes>", s[:i], len(s)-i-1)
	}
	return s
}

// lenientPercentDecode decodes %XX escapes and passes through a '%' not
// followed by two hex digits (url.PathUnescape rejects the whole string
// instead, which breaks SVGs like width='100%').
func lenientPercentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var buf strings.Builder
	buf.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, lo := unhex(s[i+1]), unhex(s[i+2])
			if hi >= 0 && lo >= 0 {
				buf.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		buf.WriteByte(s[i])
	}
	return buf.String()
}

func unhex(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c - 'a' + 10)
	case 'A' <= c && c <= 'F':
		return int(c - 'A' + 10)
	}
	return -1
}

// RequestResource returns the fetcher image and font loads go through: a data: URL is static data, decoded in place and
// never handed to fetch; any other URL goes to fetch (an error when fetch is
// nil). Mirrors ResourceFetcher::RequestResource sending a data: URL to
// CreateResourceForStaticData, which decodes it with
// network_utils::ParseDataURL instead of issuing a request
// (platform/loader/fetch/resource_fetcher.cc:1463-1474, 1061-1063 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func RequestResource(fetch func(uri string) ([]byte, error)) func(uri string) ([]byte, error) {
	return func(uri string) ([]byte, error) {
		if IsDataURL(uri) {
			body, _, err := DecodeDataURL(uri)
			return body, err
		}
		if fetch == nil {
			return nil, fmt.Errorf("no fetcher for %s", uri)
		}
		return fetch(uri)
	}
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
