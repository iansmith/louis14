package css

import (
	"net/url"
	"path"
	"strings"
)

// URL composition primitives mirror Blink's KURL
// (third_party/blink/renderer/platform/weborigin/kurl.{h,cc} @ chromium-main
// d4ecdfed88f962439247c2ad36b8fe47805b1520). Blink's KURL constructor
// `KURL(const KURL& base, const String& relative)` (kurl.cc::Init) performs
// RFC 3986 § 5.2 resolution: absolute refs returned as-is, scheme-relative
// inherit scheme, root-relative inherit scheme+authority, path-relative
// resolved against the base directory.
//
// louis14 has two flavors of base in play: filesystem-relative WPT fixtures
// (`"support"`, `"wpt-css3/filter-effects"`) and fully-qualified URLs
// (`"https://example.com/styles/"`). path.Join handles the first correctly
// but mangles the second by collapsing `https://` to `https:/`. net/url
// handles the second correctly but treats the first as opaque-path URLs
// whose ResolveReference semantics don't match path.Join. We dispatch on
// `isAbsoluteURL(base)` to use the right primitive for each case.

// CompleteURL resolves a relative URL reference against a base URL.
// Mirrors Blink's KURL(BaseURL(), url) resolution at kurl.cc::Init.
//
//   - Absolute ref (scheme:, //, /, data:): returned unchanged.
//   - Empty / "." base with relative ref: ref returned unchanged
//     (top-level filesystem documents leave URLs relative; the renderer's
//     fetcher resolves them).
//   - Absolute base + relative ref: net/url.ResolveReference (RFC 3986).
//   - Relative base + relative ref: path.Join (filesystem-WPT fixtures).
func CompleteURL(raw, base string) string {
	if raw == "" {
		return raw
	}
	if isAbsoluteURL(raw) {
		return raw
	}
	if base == "" || base == "." {
		return raw
	}
	if !isAbsoluteURL(base) {
		return path.Join(base, raw)
	}
	baseURL, errB := url.Parse(base)
	refURL, errR := url.Parse(raw)
	if errB != nil || errR != nil {
		return path.Join(base, raw)
	}
	return baseURL.ResolveReference(refURL).String()
}

// URLDir returns the directory portion of a URL. Mirrors taking `path.Dir`
// on a filesystem path but preserves scheme + authority on absolute URLs
// (path.Dir collapses `https://` to `https:/`). Trailing-slash convention
// differs by regime: relative paths follow path.Dir (no trailing slash);
// absolute URLs retain the trailing slash so the result can be fed back
// into CompleteURL for per-segment composition.
//
//   URLDir("support/main.css")                = "support"
//   URLDir("https://cdn.example.com/x/y.css") = "https://cdn.example.com/x/"
//   URLDir("/styles/main.css")                = "/styles/"
//   URLDir("https://example.com/main.css")    = "https://example.com/"
//   URLDir("")                                = ""
//
// For relative paths, the result has no trailing slash (matches path.Dir);
// for absolute URLs, it has a trailing slash so that feeding it back into
// CompleteURL produces the expected per-segment composition.
func URLDir(uri string) string {
	if uri == "" {
		return ""
	}
	if !isAbsoluteURL(uri) {
		return path.Dir(uri)
	}
	u, err := url.Parse(uri)
	if err != nil {
		return path.Dir(uri)
	}
	if i := strings.LastIndex(u.Path, "/"); i >= 0 {
		u.Path = u.Path[:i+1]
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// isAbsoluteURL reports whether s is an absolute URL form that must not be
// base-joined. Includes scheme-relative ("//foo"), root-relative ("/foo"),
// and the four scheme prefixes louis14 encounters in practice.
func isAbsoluteURL(s string) bool {
	return strings.HasPrefix(s, "//") ||
		strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "data:") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "file://")
}

// ResolveURL is the historical entry point for parse-time URL resolution.
// Retained as a thin wrapper over CompleteURL so existing call sites
// (Style.BaseDir cross-document re-stamp per LOU-137 v2, ParserContext
// methods) keep compiling unchanged. Prefer CompleteURL or
// ParserContext.CompleteURL for new code.
func ResolveURL(raw, baseDir string) string {
	return CompleteURL(raw, baseDir)
}
