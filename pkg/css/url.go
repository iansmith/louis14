package css

import urlpkg "louis14/pkg/url"

// CompleteURL resolves a relative URL reference against a base URL.
// Thin re-export of `pkg/url.CompleteURL` so existing pkg/css callers
// keep compiling. Phase 5 moved the implementation to `pkg/url` so
// `pkg/html` (below pkg/css) can call into the same primitives without
// reintroducing the import cycle. See `pkg/url/url.go` for the
// dispatch contract and the Blink references.
func CompleteURL(raw, base string) string {
	return urlpkg.CompleteURL(raw, base)
}

// URLDir returns the directory portion of a URL. Thin re-export of
// `pkg/url.URLDir`.
func URLDir(uri string) string {
	return urlpkg.URLDir(uri)
}

// ResolveURL is the historical entry point for parse-time URL resolution.
// Retained as a thin wrapper over CompleteURL so existing call sites
// (Style.BaseDir cross-document re-stamp per LOU-137 v2, ParserContext
// methods) keep compiling unchanged. Prefer CompleteURL or
// ParserContext.CompleteURL for new code.
func ResolveURL(raw, baseDir string) string {
	return urlpkg.CompleteURL(raw, baseDir)
}
