package css

import "strings"

// parseImportURL extracts the URL from an `@import` at-rule and discards
// any conditional suffix. Most callers want both pieces — see parseImport.
// This thin wrapper exists for tests and for callers that don't care about
// the media-query-list.
func parseImportURL(rule string) string {
	url, _ := parseImport(rule)
	return url
}

// parseImport extracts the URL and the trailing media-query-list (if any)
// from an `@import` at-rule. Mirrors Blink's parser-side @import handling
// at `core/css/parser/at_rule_descriptor_parser.cc` @ chromium-main
// d4ecdfed88f962439247c2ad36b8fe47805b1520 (the loose form; louis14's
// lexer doesn't emit URI tokens separately yet, so a scanner-style
// extraction is used here).
//
// Supports:
//
//	@import url("foo.css");
//	@import url(foo.css);
//	@import "foo.css";
//	@import 'foo.css';
//	@import "foo.css" screen;
//	@import "foo.css" (min-width: 1px), nonsense;
//	@import url("foo.css") (min-width: 1px);
//	@import url("foo.css") layer;
//	@import url("foo.css") layer(A);
//	@import url("foo.css") layer(A) (min-width: 1px);
//	@import url("foo.css") supports(display: block);
//
// Returns ("", "") if no URL can be extracted. The CSS Cascade 5 @import
// grammar is `<url> <layer-prefix>? <supports-condition>? <media-query-list>?`
// — louis14 strips `layer` / `layer(...)` / `supports(...)` from the
// returned mediaList so that only the media-query-list portion reaches
// parseMediaQueryList. layer/supports semantics are out of scope here
// (handled separately; see Rule.LayerName for layer plumbing).
//
// Relocated from pkg/html/parser.go in Phase 6 of LOU-138 — @import
// resolution moved from HTML-parse time to CSS-parse time so the
// imported sheet's URL identity survives.
func parseImport(rule string) (url, mediaList string) {
	rule = strings.TrimSpace(rule)
	rule = strings.TrimSuffix(rule, ";")
	rule = strings.TrimSpace(rule)

	rule = strings.TrimPrefix(rule, "@import")
	rule = strings.TrimSpace(rule)

	// url(...) form.
	if strings.HasPrefix(rule, "url(") {
		closeIdx := strings.Index(rule, ")")
		if closeIdx == -1 {
			return "", ""
		}
		inner := strings.TrimSpace(rule[4:closeIdx])
		if len(inner) >= 2 {
			if (inner[0] == '"' && inner[len(inner)-1] == '"') ||
				(inner[0] == '\'' && inner[len(inner)-1] == '\'') {
				inner = inner[1 : len(inner)-1]
			}
		}
		suffix := strings.TrimSpace(rule[closeIdx+1:])
		return inner, stripImportConditionPrefixes(suffix)
	}

	// Bare string form: @import "foo.css" or @import 'foo.css', optionally
	// followed by a media-query-list (everything after the closing quote).
	if len(rule) >= 2 && (rule[0] == '"' || rule[0] == '\'') {
		quote := rule[0]
		// Fast path: the whole remainder is a single quoted string.
		if rule[len(rule)-1] == quote && strings.Count(rule, string(quote)) == 2 {
			return rule[1 : len(rule)-1], ""
		}
		// Otherwise locate the closing quote and split.
		if endQuote := strings.IndexByte(rule[1:], quote); endQuote >= 0 {
			urlPart := rule[1 : endQuote+1]
			suffix := strings.TrimSpace(rule[endQuote+2:])
			return urlPart, stripImportConditionPrefixes(suffix)
		}
	}

	return "", ""
}

// stripImportConditionPrefixes removes the optional `layer` / `layer(...)`
// and `supports(...)` prefixes from an `@import` conditional suffix, leaving
// only the trailing media-query-list portion. Per CSS Cascade 5 §3.1, the
// @import grammar is:
//
//	@import <url> <layer-prefix>? <supports-condition>? <media-query-list>?;
//
// Layer/supports handling lives in their own subsystems; louis14 only honors
// the media-query-list here. Whitespace tolerant; case-insensitive on the
// `layer` / `supports` keywords.
func stripImportConditionPrefixes(s string) string {
	s = strings.TrimSpace(s)
	// Layer prefix: `layer` or `layer(name)`.
	if lower := strings.ToLower(s); strings.HasPrefix(lower, "layer") {
		rest := s[len("layer"):]
		if len(rest) == 0 {
			return ""
		}
		switch rest[0] {
		case '(':
			// layer(name) — find the matching ')' (no nesting expected).
			if closeIdx := strings.IndexByte(rest, ')'); closeIdx >= 0 {
				s = strings.TrimSpace(rest[closeIdx+1:])
			} else {
				return "" // malformed; drop the whole suffix.
			}
		case ' ', '\t', '\n':
			s = strings.TrimSpace(rest)
		default:
			// `layerfoo` or similar — not a layer prefix; leave untouched.
		}
	}
	// Supports prefix: `supports(condition)`.
	if lower := strings.ToLower(s); strings.HasPrefix(lower, "supports(") {
		// Match parens with depth tracking — supports() values may contain
		// nested parens (e.g. `supports((display: block))`).
		depth := 0
		end := -1
		for i := len("supports"); i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end >= 0 {
			s = strings.TrimSpace(s[end+1:])
		} else {
			return "" // malformed; drop the whole suffix.
		}
	}
	return s
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
	rawURL, mediaListStr := parseImport(atImportRule)
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
	// Tag each imported rule with the @import-level media-query-list
	// (CSS Cascade 4 §3.1 "conditional import"). Evaluation happens at
	// apply time via RuleMediaApplies, AND-combined with any inner
	// @media wrapper the rule already carries. Mirrors Blink's
	// `CSSImportRule::ApplyRule` chained media check at
	// third_party/blink/renderer/core/css/css_import_rule.cc @ 4883d11f.
	importMQs := parseMediaQueryList(mediaListStr)
	if len(importMQs) > 0 {
		for i := range imported.Rules {
			// Prepend so that nested @import media-query-lists from
			// inside the imported sheet stay closest to the rule.
			imported.Rules[i].ImportMediaQueries = append(
				append([]*MediaQuery{}, importMQs...),
				imported.Rules[i].ImportMediaQueries...,
			)
		}
	}
	dst.Rules = append(dst.Rules, imported.Rules...)
	dst.FontFaces = append(dst.FontFaces, imported.FontFaces...)
	dst.CounterStyles = append(dst.CounterStyles, imported.CounterStyles...)
	dst.FontFeatureValues = append(dst.FontFeatureValues, imported.FontFeatureValues...)
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
