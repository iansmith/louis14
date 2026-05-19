package css

import "strings"

// parseImportURL extracts the URL from an `@import` at-rule. Mirrors
// Blink's parser-side @import URL extraction at
// `core/css/parser/at_rule_descriptor_parser.cc` @ chromium-main
// d4ecdfed88f962439247c2ad36b8fe47805b1520 (the loose form;
// louis14's lexer doesn't emit URI tokens separately yet, so a
// scanner-style extraction is used here).
//
// Supports:
//
//	@import url("foo.css");
//	@import url(foo.css);
//	@import "foo.css";
//	@import 'foo.css';
//	@import "foo.css" screen;   (media query suffix tolerated, not honored)
//
// Returns the empty string if no URL can be extracted.
//
// Relocated from pkg/html/parser.go in Phase 6 of LOU-138 — @import
// resolution moved from HTML-parse time to CSS-parse time so the
// imported sheet's URL identity survives.
func parseImportURL(rule string) string {
	rule = strings.TrimSpace(rule)
	rule = strings.TrimSuffix(rule, ";")
	rule = strings.TrimSpace(rule)

	rule = strings.TrimPrefix(rule, "@import")
	rule = strings.TrimSpace(rule)

	// url(...) form.
	if strings.HasPrefix(rule, "url(") {
		closeIdx := strings.Index(rule, ")")
		if closeIdx == -1 {
			return ""
		}
		inner := strings.TrimSpace(rule[4:closeIdx])
		if len(inner) >= 2 {
			if (inner[0] == '"' && inner[len(inner)-1] == '"') ||
				(inner[0] == '\'' && inner[len(inner)-1] == '\'') {
				inner = inner[1 : len(inner)-1]
			}
		}
		return inner
	}

	// Bare string form: @import "foo.css" or @import 'foo.css', optionally
	// followed by a media query that we don't honor today.
	if len(rule) >= 2 {
		if (rule[0] == '"' && rule[len(rule)-1] == '"') ||
			(rule[0] == '\'' && rule[len(rule)-1] == '\'') {
			return rule[1 : len(rule)-1]
		}
		if rule[0] == '"' {
			if endQuote := strings.Index(rule[1:], "\""); endQuote >= 0 {
				return rule[1 : endQuote+1]
			}
		}
		if rule[0] == '\'' {
			if endQuote := strings.Index(rule[1:], "'"); endQuote >= 0 {
				return rule[1 : endQuote+1]
			}
		}
	}

	return ""
}

// resolveAtImport handles a single `@import` at-rule encountered during
// ParseStylesheet, mirroring Blink's `StyleRuleImport::NotifyFinished`
// (core/css/style_rule_import.cc:77-82 @ d4ecdfed8): the imported sheet
// is fetched via the importing context's Fetcher, then parsed with a
// FRESH ParserContext whose BaseDir is the imported sheet's URL
// directory. url() refs inside the imported sheet therefore resolve
// against the imported sheet's own location, not the importing
// sheet's.
//
// The imported Stylesheet's Rules / FontFaces / CounterStyles /
// Keyframes / LayerOrder are appended to `dst` in source order — the
// at-rule scanner has already enforced @import-before-other-rules.
//
// No-ops gracefully when:
//   - ctx is nil or ctx.Fetcher is nil (no resource lifecycle).
//   - parseImportURL returns "" (malformed @import).
//   - the fetcher returns an error (imported sheet not found / load fail).
//
// Cycle detection is intentionally absent — LOU-139 owns the resource
// cache that closes that gap; no test today exercises self-importing
// sheets.
func resolveAtImport(ctx *ParserContext, atImportRule string, dst *Stylesheet) {
	if ctx == nil || ctx.Fetcher == nil {
		return
	}
	rawURL := parseImportURL(atImportRule)
	if rawURL == "" {
		return
	}
	importedURL := ctx.CompleteURL(rawURL)
	importedCSS, err := ctx.Fetcher(importedURL)
	if err != nil {
		return
	}
	importedCtx := &ParserContext{
		BaseDir: URLDir(importedURL),
		Fetcher: ctx.Fetcher,
	}
	imported, err := ParseStylesheet(importedCSS, importedCtx)
	if err != nil || imported == nil {
		return
	}
	dst.Rules = append(dst.Rules, imported.Rules...)
	dst.FontFaces = append(dst.FontFaces, imported.FontFaces...)
	dst.CounterStyles = append(dst.CounterStyles, imported.CounterStyles...)
	for _, name := range imported.LayerOrder {
		found := false
		for _, existing := range dst.LayerOrder {
			if existing == name {
				found = true
				break
			}
		}
		if !found {
			dst.LayerOrder = append(dst.LayerOrder, name)
		}
	}
	if dst.Keyframes == nil && len(imported.Keyframes) > 0 {
		dst.Keyframes = make(map[string][]KeyframeRule)
	}
	for name, frames := range imported.Keyframes {
		dst.Keyframes[name] = frames
	}
}
