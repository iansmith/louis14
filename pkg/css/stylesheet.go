package css

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// anonymousLayerCounter assigns a unique synthetic name to each anonymous
// `@layer { ... }` block. Per CSS Cascade 5 §6.2 each anonymous layer is its
// own distinct layer (not merged with other anonymous siblings, and not
// merged with unlayered rules). The synthetic name is opaque — it only has
// to be unique and to sort in source order. The leading `#` is invalid in
// author-declared layer names (which are CSS identifiers), so a synthetic
// name cannot collide with one an author wrote. Mirrors Blink's
// CSSAtRuleLayerBlockRule path where each anonymous block creates its own
// CascadeLayer instance — third_party/blink/renderer/core/css/layer_*.cc
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
var anonymousLayerCounter atomic.Uint64

// Phase 3: CSS stylesheet structures

// Phase 17: Enhanced selector system for complex selectors

// Selector represents a CSS selector which may be compound (multiple parts with combinators)
type Selector struct {
	Raw           string           // Original selector string
	Parts         []SelectorPart   // Parts of a compound selector
	Combinators   []CombinatorType // Combinators between parts (len = len(Parts)-1)
	Specificity   int              // Specificity score for cascade
	PseudoElement string           // Phase 11: Pseudo-element (::before, ::after)

	// Legacy fields for backward compatibility with simple selectors
	Type  SelectorType // Deprecated: use Parts instead
	Value string       // Deprecated: use Parts instead
}

// SelectorPart represents a single part of a compound selector (e.g., "div.class1.class2")
type SelectorPart struct {
	Element       string              // Element name ("div", "p", "*" for universal, "" for none)
	Classes       []string            // Class names (without the .)
	ID            string              // ID (without the #)
	Attributes    []AttributeSelector // Attribute selectors
	PseudoClasses []string            // Pseudo-classes (e.g., "hover", "focus", "active", "visited")
}

// AttributeSelector represents an attribute selector like [type="text"]
type AttributeSelector struct {
	Name     string // Attribute name
	Operator string // =, ^=, $=, *=, ~=, |=
	Value    string // Attribute value
}

// CombinatorType represents the type of combinator between selector parts
type CombinatorType int

const (
	DescendantCombinator      CombinatorType = iota // space: .parent .child
	ChildCombinator                                 // >: .parent > .child
	AdjacentSiblingCombinator                       // +: .box + .box
	GeneralSiblingCombinator                        // ~: .box ~ .box
)

// Legacy: keep for backward compatibility with simple selectors
type SelectorType int

const (
	ElementSelector SelectorType = iota // div, p, span
	ClassSelector                       // .classname
	IDSelector                          // #idname
)

// Rule represents a CSS rule (selector + declarations)
type Rule struct {
	Selector       Selector
	Declarations   map[string]string // property -> value
	Important      map[string]bool   // tracks which properties are !important
	MediaQuery     *MediaQuery       // Phase 22: Optional media query wrapper
	ContainerQuery *ContainerQuery   // Optional @container query wrapper
	LayerName      string            // @layer name (empty = unlayered)

	// ImportMediaQueries carries the comma-separated media-query-list from a
	// conditional `@import url(...) <media-query-list>;` that brought this
	// rule into the importing sheet. Nil/empty when the rule was not imported
	// or the @import had no media-query suffix. Multiple entries combine
	// with OR (CSS Cascade 4 §3.1 "conditional import"); each entry uses
	// Kleene 3-valued AND internally. The list is evaluated at apply time
	// AND-combined with rule.MediaQuery (the inner `@media` wrapper, if any),
	// mirroring Blink's `CSSImportRule::ApplyRule` chained media check at
	// third_party/blink/renderer/core/css/css_import_rule.cc @ 4883d11f.
	ImportMediaQueries []*MediaQuery
}

// MediaQueryResult mirrors Blink's KleeneValue
// (third_party/blink/renderer/core/css/kleene_value.h @ 4883d11f). A media
// query evaluates to one of three states: true, false, or unknown.
// Per CSS Media Queries 4 §3.1, an unknown result does NOT apply (treated
// like false at the rule-activation level), but `not unknown` stays
// unknown — distinct from `not false → true`.
type MediaQueryResult int

const (
	MediaQueryFalse   MediaQueryResult = iota // Definitely does not match.
	MediaQueryTrue                            // Definitely matches.
	MediaQueryUnknown                         // Unknown feature / parse failure — does not apply.
)

// MediaQueryRestrictor mirrors Blink's MediaQuery::RestrictorType
// (third_party/blink/renderer/core/css/media_query.h @ 4883d11f).
type MediaQueryRestrictor int

const (
	MediaRestrictorNone MediaQueryRestrictor = iota
	MediaRestrictorNot
	MediaRestrictorOnly
)

// Phase 22: MediaQuery represents a @media rule condition.
// MediaType holds the bare type identifier exactly as parsed (e.g. "screen",
// "print", "all", or an unknown identifier like "unknown"). Negation and
// "only" handling are kept separate in Restrictor — do NOT pre-collapse a
// negated query into a different type, because that loses the 3-valued
// semantics needed for unknown features. Empty MediaType means no type was
// given (e.g. `@media (min-width: 500px)` or `@media not (unknown)`).
type MediaQuery struct {
	Restrictor MediaQueryRestrictor // "not" / "only" / none
	MediaType  string               // "screen", "print", "all", "" (no type given), or an unknown ident
	Conditions []MediaCondition     // parenthesized media features
}

// Phase 22: MediaCondition represents a single parenthesized media feature
// expression like `(min-width: 768px)` or `(unknown)`.
type MediaCondition struct {
	Feature string // "min-width", "max-width", "orientation", etc.
	Value   string // "768px", "landscape", etc.
}

// ContainerQuery represents a @container rule condition
type ContainerQuery struct {
	Name       string               // optional container name (empty = any container)
	Conditions []ContainerCondition // size conditions
}

// ContainerCondition represents a single container query condition
type ContainerCondition struct {
	Feature string // "min-width", "max-width", "width"
	Value   string // "200px", etc.
}

// FontFaceRule represents a parsed @font-face rule. Src is typed as
// *CSSURIValue per LOU-138 phase 7.5 — mirrors Blink's `CSSURIValue`
// wrapping of @font-face src tokens (core/css/css_uri_value.h @
// d4ecdfed8). Nil when the rule had no parseable src.
type FontFaceRule struct {
	Family string       // font-family (unquoted)
	Src    *CSSURIValue // URL from src: url(...); nil if missing
	Format string       // "truetype", "opentype", "woff", "woff2", or ""
	Weight string       // font-weight value (e.g. "bold", "400", "700")
	Style  string       // font-style value (e.g. "italic", "normal")
}

// KeyframeRule represents a single keyframe stop in a @keyframes rule.
type KeyframeRule struct {
	Stop         string            // "from", "to", "0%", "50%", "100%", etc.
	Declarations map[string]string // CSS declarations at this stop
}

// CounterStyleRule represents a parsed @counter-style rule.
type CounterStyleRule struct {
	Name            string           // Counter style name
	System          string           // cyclic, numeric, alphabetic, symbolic, additive, fixed
	Symbols         []string         // Symbols list (for cyclic, symbolic, alphabetic, fixed)
	AdditiveSymbols []AdditiveSymbol // Additive symbols (for additive system)
	Suffix          string           // Suffix appended after counter (default ". ")
	Prefix          string           // Prefix prepended before counter
	Fallback        string           // Fallback counter style name
}

// AdditiveSymbol represents a single additive-symbols entry (value + symbol).
type AdditiveSymbol struct {
	Value  int
	Symbol string
}

// Stylesheet represents a parsed CSS stylesheet
type Stylesheet struct {
	Rules         []Rule
	FontFaces     []FontFaceRule
	LayerOrder    []string                  // @layer declaration order (first declared = lowest priority)
	Keyframes     map[string][]KeyframeRule // animation name → keyframe stops
	CounterStyles []CounterStyleRule        // @counter-style rules
}

// stripCSSComments removes all /* ... */ and <!-- ... --> comments from CSS source,
// while preserving string literals (comments inside strings are not stripped).
// <!-- ... --> is the "HTML comment" syntax historically used in <style> tags to hide
// CSS from ancient browsers; CSS parsers treat them as CDO/CDC tokens (whitespace-like).
//
// Each comment is replaced with a single space so that adjacent tokens
// don't fuse together — e.g. `rgb(10/* x */175)` becomes `rgb(10 175)`,
// matching CSS Syntax §4 where comments act as token-stream whitespace.
func stripCSSComments(css string) string {
	var b strings.Builder
	b.Grow(len(css))
	i := 0
	inString := byte(0)
	for i < len(css) {
		// Handle string literals
		if inString != 0 {
			b.WriteByte(css[i])
			if css[i] == '\\' && i+1 < len(css) {
				i++
				b.WriteByte(css[i])
			} else if css[i] == inString {
				inString = 0
			}
			i++
			continue
		}
		if css[i] == '"' || css[i] == '\'' {
			inString = css[i]
			b.WriteByte(css[i])
			i++
			continue
		}
		if i+1 < len(css) && css[i] == '/' && css[i+1] == '*' {
			// Skip until */, replacing the comment with a single space so
			// adjacent tokens stay separated (CSS Syntax §4).
			b.WriteByte(' ')
			i += 2
			for i < len(css) {
				if i+1 < len(css) && css[i] == '*' && css[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			// If we reached end of input, the comment was unterminated — just stop
		} else if i+3 < len(css) && css[i] == '<' && css[i+1] == '!' && css[i+2] == '-' && css[i+3] == '-' {
			// CSS CDO token: skip <!-- ... --> (HTML comment in CSS).
			// Mirror /*...*/ behavior — emit a single space.
			b.WriteByte(' ')
			i += 4
			for i < len(css) {
				if i+2 < len(css) && css[i] == '-' && css[i+1] == '-' && css[i+2] == '>' {
					i += 3
					break
				}
				i++
			}
		} else {
			b.WriteByte(css[i])
			i++
		}
	}
	return b.String()
}

// expandNesting recursively expands CSS native nesting into flat rules.
// parentSelector is the selector of the enclosing rule ("" for top-level).
func expandNesting(css string, parentSelector string) string {
	var result strings.Builder
	parts := splitRules(css)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		bracePos := strings.Index(part, "{")
		if bracePos < 0 {
			// No block — pass through (e.g., @layer statement)
			result.WriteString(part)
			result.WriteByte('\n')
			continue
		}

		sel := strings.TrimSpace(part[:bracePos])
		closePos := strings.LastIndex(part, "}")
		if closePos < 0 {
			continue
		}
		body := part[bracePos+1 : closePos]

		// Handle at-rules nested inside a selector block (e.g., @media, @supports, @container)
		selLower := strings.ToLower(sel)
		if strings.HasPrefix(selLower, "@media") || strings.HasPrefix(selLower, "@supports") ||
			strings.HasPrefix(selLower, "@container") {
			if parentSelector != "" {
				// Nested @media inside a selector: lift and wrap parent selector inside
				// @media (cond) { parentSel { decls } }
				flatDecls, nestedParts := separateDeclsAndNested(body)
				var innerBlock strings.Builder
				if strings.TrimSpace(flatDecls) != "" {
					innerBlock.WriteString(parentSelector)
					innerBlock.WriteString(" { ")
					innerBlock.WriteString(flatDecls)
					innerBlock.WriteString(" }\n")
				}
				// Recurse for any nested rules inside the @media body
				innerBlock.WriteString(expandNesting(nestedParts, parentSelector))
				result.WriteString(sel)
				result.WriteString(" { ")
				result.WriteString(innerBlock.String())
				result.WriteString("}\n")
			} else {
				// Top-level at-rule — pass through unchanged
				result.WriteString(part)
				result.WriteByte('\n')
			}
			continue
		}

		// Skip other at-rules (@keyframes, @font-face, @layer, etc.) — pass through
		if strings.HasPrefix(selLower, "@") {
			result.WriteString(part)
			result.WriteByte('\n')
			continue
		}

		// Regular selector block
		flatDecls, nestedParts := separateDeclsAndNested(body)

		if parentSelector != "" {
			// This is a NESTED rule inside a parent rule
			resolvedSel := resolveNestedSelector(parentSelector, sel)
			// Emit flat rule with this selector's declarations
			if strings.TrimSpace(flatDecls) != "" {
				result.WriteString(resolvedSel)
				result.WriteString(" { ")
				result.WriteString(flatDecls)
				result.WriteString(" }\n")
			}
			// Recursively expand nested rules inside this nested rule
			if strings.TrimSpace(nestedParts) != "" {
				result.WriteString(expandNesting(nestedParts, resolvedSel))
			}
		} else {
			// Top-level rule — separate flat declarations from nested rules
			if strings.TrimSpace(flatDecls) != "" {
				result.WriteString(sel)
				result.WriteString(" { ")
				result.WriteString(flatDecls)
				result.WriteString(" }\n")
			}
			// Expand any nested rules inside, passing sel as parent
			if strings.TrimSpace(nestedParts) != "" {
				result.WriteString(expandNesting(nestedParts, sel))
			}
		}
	}
	return result.String()
}

// resolveNestedSelector resolves a nested selector relative to the parent.
// Per CSS Nesting (https://drafts.csswg.org/css-nesting/#nest-selector), when
// `&` is substituted with the parent selector, the substitution is wrapped in
// an implicit `:is(...)`. This makes the substitution behave like a single
// compound regardless of how many comma branches the parent has, and exposes
// the "contextually invalid" rule when the parent contains pseudo-elements
// (since :is() cannot contain pseudo-elements per Selectors 4).
//
// When the nested selector has no `&`, the parent is implicitly prepended with
// a descendant combinator. The same :is() wrapping applies, so that nesting
// inside a comma-separated parent like ".a, .b" produces ":is(.a, .b) .child"
// rather than ".a, .b .child" (which would distribute incorrectly).
func resolveNestedSelector(parent, nested string) string {
	wrappedParent := wrapParentInIs(parent)
	if strings.Contains(nested, "&") {
		return strings.ReplaceAll(nested, "&", wrappedParent)
	}
	// Implicit: ".child" inside ".parent" becomes ":is(.parent) .child"
	return wrappedParent + " " + nested
}

// wrapParentInIs wraps a parent selector in :is(...) for nesting substitution.
// Single simple selectors that already are bare functional pseudo-classes
// (e.g. ":is(.foo)") or that are unambiguous as compounds (single .class, #id,
// element) could in principle skip the wrap, but the spec is explicit that the
// wrap is unconditional — and is required for the contextually-invalid rule to
// fire when the parent contains pseudo-elements.
func wrapParentInIs(parent string) string {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return parent
	}
	return ":is(" + parent + ")"
}

// separateDeclsAndNested splits a CSS rule body into:
// - flatDecls: property declarations (no nested blocks)
// - nestedParts: string containing all nested rule blocks (selector { ... } or @media { ... })
func separateDeclsAndNested(body string) (flatDecls, nestedParts string) {
	var decls strings.Builder
	var nested strings.Builder

	depth := 0
	start := 0
	nestedBlockStart := -1 // tracks start of a nested block (selector position)
	inString := false
	var stringChar byte

	for i := 0; i < len(body); i++ {
		ch := body[i]
		if inString {
			if ch == stringChar && (i == 0 || body[i-1] != '\\') {
				inString = false
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			stringChar = ch
			continue
		}
		switch ch {
		case '{':
			if depth == 0 {
				// Record where this nested block starts (from the last start)
				nestedBlockStart = start
			}
			depth++
		case '}':
			depth--
			if depth == 0 && nestedBlockStart >= 0 {
				// Write the entire nested block (selector + body + closing brace) to nested
				nested.WriteString(strings.TrimSpace(body[nestedBlockStart : i+1]))
				nested.WriteByte('\n')
				start = i + 1
				nestedBlockStart = -1
			}
		case ';':
			if depth == 0 {
				// This is a flat declaration
				decls.WriteString(body[start : i+1])
				start = i + 1
			}
		}
	}
	// Remaining content (unterminated declaration without semicolon)
	if remaining := strings.TrimSpace(body[start:]); remaining != "" && depth == 0 {
		decls.WriteString(remaining)
	}

	return decls.String(), nested.String()
}

// ParseStylesheet parses CSS stylesheet content into rules. ctx carries
// the parse-time base URL used to resolve `url()` references — mirrors
// Blink's CSSParserContext threaded into every Consume* call. Pass nil for
// "no base URL" (preserves today's relative-URL-as-stored behavior, used
// by test fixtures that don't exercise URL composition).
func ParseStylesheet(css string, ctx *ParserContext) (*Stylesheet, error) {
	stylesheet := &Stylesheet{
		Rules: make([]Rule, 0),
	}

	// Strip comments before parsing
	css = stripCSSComments(css)

	// Resolve every `url(...)` token against ctx.BaseDir BEFORE any further
	// processing. Equivalent to threading CollectUrlData through every
	// property parser — at the source-text level, a single pass funnels all
	// declarations (and at-rule descriptors like @font-face src) through the
	// Blink chokepoint. Per-property typed wrappers (Phase 7) replace this
	// with per-token CollectUrlData calls.
	css = ctx.RewriteURLs(css)

	// Expand CSS native nesting into flat rules before parsing
	css = expandNesting(css, "")

	// Simple parser: split by } to get rules
	css = strings.TrimSpace(css)
	if css == "" {
		return stylesheet, nil
	}

	// Find each rule (selector { declarations })
	rules := splitRules(css)

	for _, ruleStr := range rules {
		trimmed := strings.TrimSpace(ruleStr)
		if strings.HasPrefix(trimmed, "@") {
			// Phase 22: Handle @media; skip all other at-rules
			if strings.HasPrefix(trimmed, "@media") {
				mediaRules := parseMediaRule(ruleStr)
				stylesheet.Rules = append(stylesheet.Rules, mediaRules...)
			} else if strings.HasPrefix(trimmed, "@font-face") {
				if ff := parseFontFaceRule(trimmed); ff != nil {
					stylesheet.FontFaces = append(stylesheet.FontFaces, *ff)
				}
			} else if strings.HasPrefix(trimmed, "@supports") {
				supportsRules := parseSupportsRule(ruleStr)
				stylesheet.Rules = append(stylesheet.Rules, supportsRules...)
			} else if strings.HasPrefix(trimmed, "@container") {
				containerRules := parseContainerRule(ruleStr)
				stylesheet.Rules = append(stylesheet.Rules, containerRules...)
			} else if strings.HasPrefix(trimmed, "@layer") {
				layerRules, layerNames := parseLayerRule(ruleStr, stylesheet.LayerOrder)
				for _, name := range layerNames {
					found := false
					for _, existing := range stylesheet.LayerOrder {
						if existing == name {
							found = true
							break
						}
					}
					if !found {
						stylesheet.LayerOrder = append(stylesheet.LayerOrder, name)
					}
				}
				stylesheet.Rules = append(stylesheet.Rules, layerRules...)
			} else if strings.HasPrefix(trimmed, "@keyframes") || strings.HasPrefix(trimmed, "@-webkit-keyframes") {
				// Parse and store keyframes; static renderer uses initial state only
				name, frames := parseKeyframesRule(ruleStr)
				if name != "" {
					if stylesheet.Keyframes == nil {
						stylesheet.Keyframes = make(map[string][]KeyframeRule)
					}
					stylesheet.Keyframes[name] = frames
				}
			} else if strings.HasPrefix(trimmed, "@counter-style") {
				cs := parseCounterStyleRule(ruleStr)
				if cs.Name != "" {
					stylesheet.CounterStyles = append(stylesheet.CounterStyles, cs)
				}
			} else if strings.HasPrefix(trimmed, "@import") {
				// Fetch and parse the imported sheet with a fresh
				// ParserContext rooted at the imported sheet's own URL
				// dir, so url() refs inside resolve against that base
				// (mirrors Blink's StyleRuleImport::NotifyFinished —
				// core/css/style_rule_import.cc:77-82 @ d4ecdfed8).
				// Rules + FontFaces + CounterStyles + Keyframes +
				// LayerOrder fold into the importing stylesheet at the
				// @import's position. No-op when ctx.Fetcher is nil
				// (matches pre-Phase-6 silent-skip behavior).
				resolveAtImport(ctx, trimmed, stylesheet)
			}
			// Unknown at-rules (@three-dee, …) are silently skipped.
			continue
		}

		rules, err := parseRules(ruleStr)
		if err != nil {
			// Skip malformed rules
			continue
		}
		stylesheet.Rules = append(stylesheet.Rules, rules...)
	}

	return stylesheet, nil
}

// splitRules splits CSS into individual rules, with robust error recovery
// for unclosed blocks, strings, and mismatched braces.
func splitRules(css string) []string {
	rules := make([]string, 0)
	depth := 0
	start := 0
	inString := byte(0) // 0 = not in string, '"' or '\'' = in that string

	for i := 0; i < len(css); i++ {
		ch := css[i]

		// Handle string literals — skip their contents
		if inString != 0 {
			if ch == '\\' && i+1 < len(css) {
				i++ // skip escaped character
			} else if ch == inString {
				inString = 0
			}
			continue
		}

		// Handle backslash escapes outside strings (e.g., \} in property values)
		if ch == '\\' && i+1 < len(css) {
			i++ // skip escaped character
			continue
		}

		if ch == '"' || ch == '\'' {
			inString = ch
			continue
		}

		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth <= 0 {
				// Found complete rule (or recovered from negative depth)
				depth = 0
				ruleStr := css[start : i+1]
				if strings.TrimSpace(ruleStr) != "" {
					rules = append(rules, ruleStr)
				}
				start = i + 1
			}
		} else if depth == 0 && ch == ';' {
			// CSS 2.1 error recovery: a stray ';' after '}' at the top level
			// becomes part of the next rule's text, making its selector invalid.
			// This is tested by Acid2 line 102: ".parser { m\argin: 2em; };"
			// where the ';' should cause the next rule to be skipped.
			// However, ';' that terminates an at-rule (e.g., @import url(...);)
			// should be skipped normally.
			isAfterCloseBrace := false
			for j := i - 1; j >= 0; j-- {
				c := css[j]
				if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
					continue
				}
				if c == '}' {
					isAfterCloseBrace = true
				}
				break
			}
			if !isAfterCloseBrace {
				// At-rule terminator (no body) — emit rule text for at-rules
				// that don't carry a `{ … }` body but still need to reach the
				// at-rule scanner: @layer (declaration form) and @import.
				ruleText := strings.TrimSpace(css[start : i+1])
				if strings.HasPrefix(ruleText, "@layer") || strings.HasPrefix(ruleText, "@import") {
					rules = append(rules, ruleText)
				}
				start = i + 1
			}
			// If after '}', leave the ';' in the next rule's text
			// so isValidSelector will reject the selector starting with ';'
		}
	}

	// CSS Syntax Level 3 §9 (error handling): at EOF, treat any open block as
	// if it were closed. This handles CDATA-wrapped stylesheets where the
	// trailing "]]>" stands in for the closing "}". Real browsers apply this
	// recovery; Blink's CSS parser does the same via its tokenizer.
	if depth > 0 && start < len(css) && strings.TrimSpace(css[start:]) != "" {
		synthesized := css[start:] + strings.Repeat("}", depth)
		rules = append(rules, synthesized)
	}
	return rules
}

// isValidSelector checks if a selector string looks valid enough to parse.
// Returns false for clearly malformed selectors that should cause the rule to be skipped.
func isValidSelector(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Selector must not start with } or ; or {
	if s[0] == '}' || s[0] == ';' || s[0] == '{' {
		return false
	}
	// Check for unbalanced brackets
	bracketDepth := 0
	for _, ch := range s {
		switch ch {
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
			if bracketDepth < 0 {
				return false
			}
		case '{', '}':
			// Braces inside selector text are invalid
			return false
		}
	}
	if bracketDepth != 0 {
		return false
	}
	return true
}

// parseRules parses a CSS rule string, expanding comma-separated selector
// groups into multiple rules with the same declarations.
// e.g., "h1, h2, h3 { color: red }" → 3 separate rules.
func parseRules(ruleStr string) ([]Rule, error) {
	// Find the opening brace
	bracePos := strings.Index(ruleStr, "{")
	if bracePos == -1 {
		return nil, fmt.Errorf("no opening brace found")
	}

	selectorStr := strings.TrimSpace(ruleStr[:bracePos])

	// Split selector by commas (but not commas inside brackets or parens)
	selectors := splitSelectorGroup(selectorStr)
	if len(selectors) <= 1 {
		// No commas or only one selector — use the original parseRule
		rule, err := parseRule(ruleStr)
		if err != nil {
			return nil, err
		}
		return []Rule{rule}, nil
	}

	// Extract declarations (shared by all selectors)
	declStart := bracePos + 1
	declEnd := strings.LastIndex(ruleStr, "}")
	if declEnd == -1 {
		declEnd = len(ruleStr)
	}
	declStr := ruleStr[declStart:declEnd]
	declResult := parseDeclarations(declStr)

	rules := make([]Rule, 0, len(selectors))
	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" || !isValidSelector(sel) {
			continue
		}
		selector := parseSelector(sel)
		rules = append(rules, Rule{
			Selector:     selector,
			Declarations: declResult.Declarations,
			Important:    declResult.Important,
		})
	}

	if len(rules) == 0 {
		return nil, fmt.Errorf("no valid selectors in group")
	}
	return rules, nil
}

// splitSelectorGroup splits a selector group by commas, respecting brackets
// and parentheses (e.g., attribute selectors like [attr~="a,b"]).
func splitSelectorGroup(s string) []string {
	var parts []string
	depth := 0
	inString := byte(0)
	start := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
			} else if ch == inString {
				inString = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = ch
			continue
		}
		if ch == '(' || ch == '[' {
			depth++
		} else if ch == ')' || ch == ']' {
			depth--
		} else if ch == ',' && depth == 0 {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseRule parses a single CSS rule
func parseRule(ruleStr string) (Rule, error) {
	// Find the opening brace
	bracePos := strings.Index(ruleStr, "{")
	if bracePos == -1 {
		return Rule{}, fmt.Errorf("no opening brace found")
	}

	// Extract selector
	selectorStr := strings.TrimSpace(ruleStr[:bracePos])

	// Validate selector — skip entire rule if invalid
	if !isValidSelector(selectorStr) {
		return Rule{}, fmt.Errorf("invalid selector: %q", selectorStr)
	}

	selector := parseSelector(selectorStr)

	// Extract declarations (between { and })
	declStart := bracePos + 1
	declEnd := strings.LastIndex(ruleStr, "}")
	if declEnd == -1 {
		declEnd = len(ruleStr)
	}

	declStr := ruleStr[declStart:declEnd]
	declResult := parseDeclarations(declStr)

	return Rule{
		Selector:     selector,
		Declarations: declResult.Declarations,
		Important:    declResult.Important,
	}, nil
}

// Phase 22: parseMediaRule parses a @media rule and returns its inner rules.
// When the media query is a comma-separated media-query-list
// (CSS Media Queries 4 §2.1, e.g. `@media (color-gamut: srgb), not (color-
// gamut: srgb)`), the rule is duplicated once per branch so each branch is
// gated by its own MediaQuery and the implicit OR is encoded by emitting
// multiple copies — if any branch evaluates True the rule applies. Mirrors
// Blink's StyleRuleMedia carrying a MediaQuerySet of multiple MediaQueries
// at third_party/blink/renderer/core/css/style_rule_media.h @ 4883d11f.
func parseMediaRule(ruleStr string) []Rule {
	rules := make([]Rule, 0)

	// Find the opening brace
	bracePos := strings.Index(ruleStr, "{")
	if bracePos == -1 {
		return rules
	}

	// Extract media query string: @media (conditions). Parse as a comma-
	// separated media-query-list so each branch becomes one MediaQuery; OR
	// semantics across branches are realized by duplicating inner rules per
	// branch below. Empty list (bare `@media {`) falls back to a single
	// default-True MediaQuery so the inner rules remain emitted.
	mediaStr := strings.TrimSpace(ruleStr[:bracePos])
	mediaQueries := parseMediaQueryList(strings.TrimSpace(strings.TrimPrefix(mediaStr, "@media")))
	if len(mediaQueries) == 0 {
		mediaQueries = []*MediaQuery{parseMediaQuery(mediaStr)}
	}

	// Extract inner CSS (between outermost { and })
	innerStart := bracePos + 1
	innerEnd := strings.LastIndex(ruleStr, "}")
	if innerEnd == -1 || innerEnd <= innerStart {
		return rules
	}

	innerCSS := ruleStr[innerStart:innerEnd]

	// Parse inner rules
	innerRules := splitRules(innerCSS)

	for _, innerRuleStr := range innerRules {
		innerTrimmed := strings.TrimSpace(innerRuleStr)
		// Handle nested @media inside @media (e.g. @media screen { @media (min-width:1120px) { ... } })
		if strings.HasPrefix(innerTrimmed, "@media") {
			nestedRules := parseMediaRule(innerRuleStr)
			// Nested media queries override the outer media query (inner wins)
			rules = append(rules, nestedRules...)
			continue
		}
		// Handle @supports nested inside @media — required by at-supports-002.
		// The @media constraint propagates onto every rule the @supports
		// expansion produces, mirroring Blink's StyleRuleMedia containing
		// StyleRuleSupports.
		if strings.HasPrefix(innerTrimmed, "@supports") {
			supportsRules := parseSupportsRule(innerRuleStr)
			for _, mq := range mediaQueries {
				for _, sr := range supportsRules {
					sr.MediaQuery = mq
					rules = append(rules, sr)
				}
			}
			continue
		}
		// Use parseRules (plural) to handle comma-separated selector groups
		parsedRules, err := parseRules(innerRuleStr)
		if err != nil {
			continue
		}
		// Attach media query to all resulting rules. For a comma-separated
		// media-query-list, emit one copy per branch — Kleene OR is realized
		// by per-branch evaluation at apply time.
		for _, mq := range mediaQueries {
			for _, rule := range parsedRules {
				rule.MediaQuery = mq
				rules = append(rules, rule)
			}
		}
	}

	return rules
}

// parseContainerRule parses a @container rule and returns its inner rules
func parseContainerRule(ruleStr string) []Rule {
	rules := make([]Rule, 0)

	// Find the opening brace
	bracePos := strings.Index(ruleStr, "{")
	if bracePos == -1 {
		return rules
	}

	// Extract container query string: @container [name] (conditions)
	queryStr := strings.TrimSpace(ruleStr[:bracePos])
	containerQuery := parseContainerQuery(queryStr)

	// Extract inner CSS (between outermost { and })
	innerStart := bracePos + 1
	innerEnd := strings.LastIndex(ruleStr, "}")
	if innerEnd == -1 || innerEnd <= innerStart {
		return rules
	}

	innerCSS := ruleStr[innerStart:innerEnd]

	// Parse inner rules
	innerRules := splitRules(innerCSS)

	for _, innerRuleStr := range innerRules {
		parsedRules, err := parseRules(innerRuleStr)
		if err != nil {
			continue
		}
		for _, rule := range parsedRules {
			rule.ContainerQuery = containerQuery
			rules = append(rules, rule)
		}
	}

	return rules
}

// parseLayerRule parses an @layer rule. Two forms:
//   - Statement form: "@layer reset, base;" — declares layer order, no rules
//   - Block form: "@layer base { selector { ... } }" — rules belonging to a layer
//
// Returns (rules, layerNames) where layerNames are declared/used layer names.
// existingLayerOrder is used to assign LayerName to rules when a block form is used.
func parseLayerRule(ruleStr string, existingLayerOrder []string) ([]Rule, []string) {
	trimmed := strings.TrimSpace(ruleStr)
	rules := make([]Rule, 0)
	layerNames := make([]string, 0)

	// Remove "@layer" prefix
	rest := strings.TrimPrefix(trimmed, "@layer")
	rest = strings.TrimSpace(rest)

	// Statement form: ends with ";" (no block)
	// e.g. "@layer reset, base;" or "@layer utilities;"
	if strings.HasSuffix(rest, ";") {
		// Remove trailing semicolon
		nameList := strings.TrimSuffix(rest, ";")
		nameList = strings.TrimSpace(nameList)
		// Parse comma-separated names
		for _, name := range strings.Split(nameList, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				layerNames = append(layerNames, name)
			}
		}
		// Statement form has no CSS rules; just returns layer names for ordering
		return rules, layerNames
	}

	// Block form: "@layer name { ... }" or "@layer { ... }" (anonymous)
	bracePos := strings.Index(rest, "{")
	if bracePos == -1 {
		return rules, layerNames
	}

	// Extract layer name (may be empty for anonymous layer)
	layerName := strings.TrimSpace(rest[:bracePos])

	// CSS Cascade 5 §6.2: each anonymous `@layer { ... }` block is its own
	// distinct cascade layer, not merged with other anonymous blocks or
	// with the unlayered context. Mint a unique synthetic name so the
	// cascade sort and revert-layer snapshot logic in cascade.go can
	// treat each anonymous block as its own priority bucket. The `#`
	// prefix cannot appear in an author-declared layer name (which is a
	// CSS ident), so the synthetic name is collision-free.
	if layerName == "" {
		layerName = fmt.Sprintf("#anon-%d", anonymousLayerCounter.Add(1))
	}

	// Extract inner CSS (between outermost { and last })
	innerStart := strings.Index(ruleStr, "{") + 1
	innerEnd := strings.LastIndex(ruleStr, "}")
	if innerEnd == -1 || innerEnd <= innerStart {
		return rules, layerNames
	}

	innerCSS := ruleStr[innerStart:innerEnd]

	// Parse inner rules
	innerRules := splitRules(innerCSS)
	for _, innerRuleStr := range innerRules {
		innerTrimmed := strings.TrimSpace(innerRuleStr)
		// Handle nested @media inside @layer
		if strings.HasPrefix(innerTrimmed, "@media") {
			mediaRules := parseMediaRule(innerRuleStr)
			for i := range mediaRules {
				mediaRules[i].LayerName = layerName
			}
			rules = append(rules, mediaRules...)
		} else if strings.HasPrefix(innerTrimmed, "@supports") {
			supportsRules := parseSupportsRule(innerRuleStr)
			for i := range supportsRules {
				supportsRules[i].LayerName = layerName
			}
			rules = append(rules, supportsRules...)
		} else {
			parsed, err := parseRules(innerRuleStr)
			if err != nil {
				continue
			}
			for i := range parsed {
				parsed[i].LayerName = layerName
			}
			rules = append(rules, parsed...)
		}
	}

	// Record this layer name. Synthetic anonymous names are also recorded
	// (layerName was reassigned above) so the stylesheet's LayerOrder
	// reflects source-order priority across anonymous + named blocks alike.
	if layerName != "" {
		layerNames = append(layerNames, layerName)
	}

	return rules, layerNames
}

// parseContainerQuery parses a container query string like "@container sidebar (min-width: 200px)"
func parseContainerQuery(queryStr string) *ContainerQuery {
	// Remove @container prefix
	queryStr = strings.TrimPrefix(queryStr, "@container")
	queryStr = strings.TrimSpace(queryStr)

	cq := &ContainerQuery{
		Conditions: make([]ContainerCondition, 0),
	}

	// Check if there's a container name before the first (
	parenPos := strings.Index(queryStr, "(")
	if parenPos > 0 {
		namePart := strings.TrimSpace(queryStr[:parenPos])
		if namePart != "" && namePart != "not" {
			cq.Name = namePart
		}
	}

	// Parse conditions: (min-width: 200px) and (max-width: 500px)
	conditionStrs := strings.Split(queryStr, "and")
	for _, condStr := range conditionStrs {
		condStr = strings.TrimSpace(condStr)
		// Skip the name part (non-parenthesized)
		if !strings.Contains(condStr, "(") {
			continue
		}
		// Remove parentheses
		condStr = strings.Trim(condStr, "()")
		condStr = strings.TrimSpace(condStr)

		// Split by : to get feature and value
		colonPos := strings.Index(condStr, ":")
		if colonPos == -1 {
			continue
		}
		feature := strings.TrimSpace(condStr[:colonPos])
		value := strings.TrimSpace(condStr[colonPos+1:])
		cq.Conditions = append(cq.Conditions, ContainerCondition{
			Feature: feature,
			Value:   value,
		})
	}

	return cq
}

// Phase 22: parseMediaQuery parses a media query string like
// "@media screen and (min-width: 768px)". Mirrors
// third_party/blink/renderer/core/css/parser/media_query_parser.cc @ 4883d11f
// in that the restrictor ("not" / "only") is kept separate from the media
// type identifier. Unrecognized identifiers are preserved verbatim — they
// are NOT silently rewritten to "all" or "none". The negation is applied at
// evaluation time via applyMediaRestrictor() so that unknown features
// remain unknown (Kleene 3-valued logic).
func parseMediaQuery(mediaStr string) *MediaQuery {
	// Remove @media prefix
	mediaStr = strings.TrimPrefix(mediaStr, "@media")
	mediaStr = strings.TrimSpace(mediaStr)

	mq := &MediaQuery{
		Restrictor: MediaRestrictorNone,
		Conditions: make([]MediaCondition, 0),
	}

	// Handle "not" and "only" modifiers
	if strings.HasPrefix(mediaStr, "not ") {
		mq.Restrictor = MediaRestrictorNot
		mediaStr = strings.TrimSpace(mediaStr[4:])
	} else if strings.HasPrefix(mediaStr, "only ") {
		mq.Restrictor = MediaRestrictorOnly
		mediaStr = strings.TrimSpace(mediaStr[5:])
	}

	// If the remaining string starts with an identifier (not a "("), parse it
	// as the media type identifier. The identifier is preserved as-is so the
	// evaluator can recognize unknown types and apply 3-valued logic.
	if mediaStr != "" && mediaStr[0] != '(' {
		end := 0
		for end < len(mediaStr) {
			ch := mediaStr[end]
			if ch == ' ' || ch == ',' || ch == '\t' || ch == '\n' {
				break
			}
			end++
		}
		mq.MediaType = strings.ToLower(mediaStr[:end])
		mediaStr = strings.TrimSpace(mediaStr[end:])
	}

	// Remove leading "and" keyword
	mediaStr = strings.TrimPrefix(mediaStr, "and")
	mediaStr = strings.TrimSpace(mediaStr)

	// Parse conditions: (min-width: 768px) and (max-width: 1024px)
	// Split by " and " at paren depth 0 to handle nested parens in values
	conditionStrs := splitMediaConditions(mediaStr)

	for _, condStr := range conditionStrs {
		condStr = strings.TrimSpace(condStr)
		if condStr == "" {
			continue
		}

		// Remove outer parentheses
		if strings.HasPrefix(condStr, "(") && strings.HasSuffix(condStr, ")") {
			condStr = condStr[1 : len(condStr)-1]
		}
		condStr = strings.TrimSpace(condStr)

		// Split by : to get feature and value
		parts := strings.SplitN(condStr, ":", 2)
		if len(parts) == 2 {
			feature := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			mq.Conditions = append(mq.Conditions, MediaCondition{
				Feature: feature,
				Value:   value,
			})
		} else if len(parts) == 1 && condStr != "" {
			// Boolean media feature like (color) or (hover) — no value
			feature := strings.TrimSpace(parts[0])
			if feature != "" {
				mq.Conditions = append(mq.Conditions, MediaCondition{
					Feature: feature,
					Value:   "", // empty value = boolean true test
				})
			}
		}
	}

	return mq
}

// parseMediaQueryList parses a comma-separated media-query-list
// (CSS Media Queries 4 §2.1) into one *MediaQuery per branch. Used by
// the conditional `@import url(...) <media-query-list>` form. Whitespace-only
// or empty input returns an empty slice. Each branch is parsed with
// parseMediaQuery so that unknown features/types preserve Kleene 3-valued
// semantics at evaluation time.
func parseMediaQueryList(s string) []*MediaQuery {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	branches := splitMediaQueryListBranches(s)
	out := make([]*MediaQuery, 0, len(branches))
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		out = append(out, parseMediaQuery(branch))
	}
	return out
}

// splitMediaQueryListBranches splits a media-query-list by top-level commas
// (CSS Media Queries 4 §2.1). Commas inside parenthesized media features are
// ignored. Mirrors the comma-OR split in Blink's
// `MediaQueryParser::ParseMediaQuerySet` at
// third_party/blink/renderer/core/css/parser/media_query_parser.cc @ 4883d11f.
func splitMediaQueryListBranches(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// splitMediaConditions splits a media query condition string by " and " at paren depth 0.
// This handles cases like "(min-width: 100px) and (max-width: 500px)".
func splitMediaConditions(s string) []string {
	var parts []string
	depth := 0
	start := 0
	const sep = " and "

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && i+len(sep) <= len(s) && strings.EqualFold(s[i:i+len(sep)], sep) {
			parts = append(parts, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseSupportsRule parses an @supports rule and returns the inner rules if condition is met
func parseSupportsRule(ruleStr string) []Rule {
	ruleStr = strings.TrimSpace(ruleStr)
	braceIdx := strings.Index(ruleStr, "{")
	if braceIdx < 0 {
		return nil
	}

	conditionStr := strings.TrimSpace(ruleStr[len("@supports"):braceIdx])

	// Find matching closing brace
	innerEnd := strings.LastIndex(ruleStr, "}")
	if innerEnd <= braceIdx {
		return nil
	}
	innerCSS := ruleStr[braceIdx+1 : innerEnd]

	if !evaluateSupportsCondition(conditionStr) {
		return nil
	}

	innerRules := splitRules(innerCSS)
	var rules []Rule
	for _, inner := range innerRules {
		innerTrimmed := strings.TrimSpace(inner)
		// Handle @media nested inside @supports — required by at-supports-003.
		// Mirrors Blink's StyleRuleSupports containing StyleRuleMedia.
		if strings.HasPrefix(innerTrimmed, "@media") {
			rules = append(rules, parseMediaRule(inner)...)
			continue
		}
		// Handle nested @supports inside @supports.
		if strings.HasPrefix(innerTrimmed, "@supports") {
			rules = append(rules, parseSupportsRule(inner)...)
			continue
		}
		parsedRules, err := parseRules(inner)
		if err != nil {
			continue
		}
		rules = append(rules, parsedRules...)
	}
	return rules
}

// evaluateSupportsCondition evaluates a CSS @supports condition string per
// CSS Conditional Rules 3 §6.4. Mirrors Blink's
// CSSSupportsParser::ConsumeSupportsCondition() in
// third_party/blink/renderer/core/css/parser/css_supports_parser.cc
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Grammar:
//
//	<supports-condition> = not <supports-in-parens>
//	                     | <supports-in-parens> [ and <supports-in-parens> ]*
//	                     | <supports-in-parens> [ or <supports-in-parens> ]*
//	<supports-in-parens> = ( <supports-condition> )
//	                     | <supports-feature>
//	                     | <general-enclosed>
//	<supports-feature>   = <supports-decl> | <supports-selector-fn> | ...
//	<supports-decl>      = ( <declaration> )
//
// Unlike Blink, louis14 currently does not implement selector()/font-format()/
// font-tech()/at-rule() supports-feature functions — those are out-of-scope
// for W8.19. All function-tokens fall through to <general-enclosed>, which
// per spec evaluates to false (kUnsupported in Blink terms).
func evaluateSupportsCondition(condition string) bool {
	res, rest := consumeSupportsCondition(condition)
	if strings.TrimSpace(rest) != "" {
		// Trailing garbage means the condition didn't fully parse.
		return false
	}
	return res == supportsTrue
}

// supportsResult mirrors Blink's three-valued Result enum
// (kSupported / kUnsupported / kParseFailure). Parse-failure surfaces upward
// so an outer wrapper can decide whether the overall condition is invalid.
type supportsResult int

const (
	supportsParseFailure supportsResult = iota
	supportsFalse
	supportsTrue
)

// consumeSupportsCondition parses a <supports-condition> and returns the
// boolean result plus any unconsumed trailing input. Whitespace at the front
// is consumed; trailing whitespace after a successful consume is also eaten.
func consumeSupportsCondition(s string) (supportsResult, string) {
	s = trimLeadingWS(s)
	// `not <supports-in-parens>` — the keyword `not` must be a standalone
	// identifier, not a function-token (`not(`). Per CSS Syntax, `not(` is a
	// <function-token> and falls into <general-enclosed> instead.
	if rest, ok := consumeKeyword(s, "not"); ok {
		res, after := consumeSupportsInParens(trimLeadingWS(rest))
		if res == supportsParseFailure {
			return supportsParseFailure, s
		}
		return notResult(res), trimLeadingWS(after)
	}

	res, rest := consumeSupportsInParens(s)
	if res == supportsParseFailure {
		return supportsParseFailure, s
	}
	rest = trimLeadingWS(rest)

	// Decide chain operator based on what follows. Mixing `and`/`or` at the
	// same level is a syntax error per spec; we stick to whichever appears
	// first and stop if the chain breaks.
	if after, ok := consumeKeyword(rest, "and"); ok {
		for {
			next, tail := consumeSupportsInParens(trimLeadingWS(after))
			if next == supportsParseFailure {
				return supportsParseFailure, s
			}
			res = andResult(res, next)
			rest = trimLeadingWS(tail)
			var more bool
			after, more = consumeKeyword(rest, "and")
			if !more {
				break
			}
		}
		return res, rest
	}
	if after, ok := consumeKeyword(rest, "or"); ok {
		for {
			next, tail := consumeSupportsInParens(trimLeadingWS(after))
			if next == supportsParseFailure {
				return supportsParseFailure, s
			}
			res = orResult(res, next)
			rest = trimLeadingWS(tail)
			var more bool
			after, more = consumeKeyword(rest, "or")
			if !more {
				break
			}
		}
		return res, rest
	}

	return res, rest
}

// consumeSupportsInParens parses <supports-in-parens>. Returns the result
// plus the rest of the input after the consumed token.
//
// Per spec the production accepts three alternatives:
//
//  1. `( <supports-condition> )` — a nested grouping.
//  2. `<supports-feature>` — currently only <supports-decl>, since selector(),
//     font-format(), font-tech(), and at-rule() functional notations are not
//     implemented (W8.19 scope guard).
//  3. `<general-enclosed>` — any function-token whose body tokenizes, OR any
//     parenthesized block whose body doesn't otherwise parse. Always
//     evaluates to false (kUnsupported in Blink).
func consumeSupportsInParens(s string) (supportsResult, string) {
	s = trimLeadingWS(s)
	if strings.HasPrefix(s, "(") {
		body, after, ok := consumeBalancedParens(s)
		if !ok {
			return supportsParseFailure, s
		}
		inner := strings.TrimSpace(body)

		// First try as `( <supports-condition> )` — a nested grouping.
		if strings.HasPrefix(inner, "(") || hasLeadingKeyword(inner, "not") {
			res, rest := consumeSupportsCondition(inner)
			if res != supportsParseFailure && strings.TrimSpace(rest) == "" {
				return res, after
			}
			// Fall through to <general-enclosed> if it didn't fully parse.
		}

		// Then as `<supports-decl>`.
		if res, ok := consumeSupportsDecl(inner); ok {
			if res {
				return supportsTrue, after
			}
			return supportsFalse, after
		}

		// Otherwise treat the parenthesized block as <general-enclosed>:
		// consumed but unsupported.
		return supportsFalse, after
	}

	// Function-token form: ident followed by `(` — falls into
	// <general-enclosed>. Includes the unimplemented `selector(...)`,
	// `font-format(...)`, `font-tech(...)`, etc. Evaluates to false.
	if after, ok := consumeGeneralEnclosedFunction(s); ok {
		return supportsFalse, after
	}

	return supportsParseFailure, s
}

// consumeGeneralEnclosedFunction consumes a function-token form
// `ident( <any> )` from the head of s and returns (rest, true) on success.
// Per CSS Conditional 3 §6.4 / Blink's CSSSupportsParser::ConsumeGeneralEnclosed,
// any well-formed function call qualifies even if the function name is
// unknown — the supports-result is just kUnsupported.
func consumeGeneralEnclosedFunction(s string) (string, bool) {
	// Read ident.
	i := 0
	for i < len(s) {
		c := s[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		isExtra := c == '-' || c == '_'
		if i == 0 {
			if !isAlpha && !isExtra {
				return s, false
			}
		} else {
			if !isAlpha && !isDigit && !isExtra {
				break
			}
		}
		i++
	}
	if i == 0 || i >= len(s) || s[i] != '(' {
		return s, false
	}
	_, after, ok := consumeBalancedParens(s[i:])
	if !ok {
		return s, false
	}
	return after, true
}

// consumeSupportsDecl reports whether inner is a valid <declaration> for
// @supports purposes. Returns (resultBool, parsedOK).
//
//   - parsedOK is false if the content doesn't even tokenize as a declaration
//     (no colon, empty property, trailing semicolon, etc.). The caller then
//     treats the wrapping parens as <general-enclosed>.
//   - resultBool is true if the declaration is "supported" (known property OR
//     a custom property `--*`, with a non-empty value that parses to at least
//     one valid longhand). False for unknown properties.
//
// Per CSS Syntax §5.4.6: a declaration is `<ident-token> S* : S* <component-
// value>+ ['!' <important>]`. A trailing semicolon means the input contains
// MORE than one declaration — invalid as a single <supports-decl>.
func consumeSupportsDecl(inner string) (result bool, ok bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return false, false
	}
	colon := strings.Index(inner, ":")
	if colon <= 0 {
		return false, false
	}
	property := strings.TrimSpace(inner[:colon])
	value := strings.TrimSpace(inner[colon+1:])
	if property == "" || value == "" {
		return false, false
	}
	// Property name must be a valid <ident-token>: starts with letter or '-'.
	if !isValidPropertyIdent(property) {
		return false, false
	}
	// A trailing semicolon means the input is multiple declarations chained,
	// not a single <declaration>. Per at-supports-039 this must be invalid.
	if strings.HasSuffix(value, ";") {
		return false, false
	}

	// Custom properties (`--foo`) are always supported per CSS Variables 1
	// §2.1: "Custom properties are not subject to the same restrictions as
	// ordinary CSS properties." Blink mirrors this: any --* with a non-empty
	// value succeeds in CSSSupportsParser.
	if strings.HasPrefix(property, "--") {
		return true, true
	}

	// Strip optional `!important` (or any `!<ident>` flag) from the value to
	// validate the property:value pair itself. The spec considers the
	// declaration supported iff the UA recognizes it; `!important` is part of
	// the declaration's outer syntax but doesn't affect supports semantics.
	if bang := strings.Index(value, "!"); bang >= 0 {
		bare := strings.TrimSpace(value[:bang])
		if bare == "" {
			return false, false
		}
		value = bare
	}

	return isSupportedDeclaration(property, value), true
}

// isSupportedDeclaration reports whether the given property:value pair is
// recognized by the UA. Used by @supports.
func isSupportedDeclaration(property, value string) bool {
	if !isSupportedCSSProperty(property) {
		return false
	}
	// For properties with limited keyword grammars, validate the value.
	switch property {
	case "display":
		validDisplay := map[string]bool{
			"block": true, "inline": true, "inline-block": true,
			"flex": true, "inline-flex": true,
			"grid": true, "inline-grid": true,
			"none": true, "contents": true,
			"table": true, "table-row": true, "table-cell": true,
			"table-caption": true, "table-row-group": true,
			"table-header-group": true, "table-footer-group": true,
			"table-column": true, "table-column-group": true,
			"list-item":   true,
			"-webkit-box": true, "-webkit-inline-box": true,
			"-webkit-flex": true, "-webkit-inline-flex": true,
		}
		return validDisplay[strings.ToLower(value)]
	case "position":
		validPosition := map[string]bool{
			"static": true, "relative": true, "absolute": true,
			"fixed": true, "sticky": true,
		}
		return validPosition[strings.ToLower(value)]
	}
	// For other recognized properties, accept any non-empty value (the spec
	// only requires "if the user agent supports the property:value pair"; we
	// take a permissive interpretation matching Blink's behavior of treating
	// most properties as accepting any syntactically-valid <declaration-value>).
	return true
}

// isSupportedCSSProperty returns true for properties louis14 recognizes for
// @supports purposes. This is intentionally broader than the set we actually
// implement layout for — `(margin: 0)` should evaluate true even if a
// particular margin computation isn't fully supported.
func isSupportedCSSProperty(property string) bool {
	p := strings.ToLower(property)
	// Custom properties — always supported.
	if strings.HasPrefix(p, "--") {
		return true
	}
	if _, ok := supportedCSSProperties[p]; ok {
		return true
	}
	return false
}

// supportedCSSProperties enumerates the CSS properties louis14 considers
// "known" for @supports evaluation. Pulled from the union of properties
// recognized by expandShorthand, the cascade, parseDeclarations validators,
// and common longhands. Add to this list as new properties get implemented.
var supportedCSSProperties = map[string]struct{}{
	// Box model
	"width": {}, "height": {}, "min-width": {}, "max-width": {},
	"min-height": {}, "max-height": {},
	"margin": {}, "margin-top": {}, "margin-right": {}, "margin-bottom": {}, "margin-left": {},
	"margin-inline": {}, "margin-block": {},
	"margin-inline-start": {}, "margin-inline-end": {},
	"margin-block-start": {}, "margin-block-end": {},
	"padding": {}, "padding-top": {}, "padding-right": {}, "padding-bottom": {}, "padding-left": {},
	"padding-inline": {}, "padding-block": {},
	"padding-inline-start": {}, "padding-inline-end": {},
	"padding-block-start": {}, "padding-block-end": {},
	"box-sizing": {},
	// Borders
	"border": {}, "border-top": {}, "border-right": {}, "border-bottom": {}, "border-left": {},
	"border-width": {}, "border-style": {}, "border-color": {},
	"border-top-width": {}, "border-right-width": {}, "border-bottom-width": {}, "border-left-width": {},
	"border-top-style": {}, "border-right-style": {}, "border-bottom-style": {}, "border-left-style": {},
	"border-top-color": {}, "border-right-color": {}, "border-bottom-color": {}, "border-left-color": {},
	"border-radius":          {},
	"border-top-left-radius": {}, "border-top-right-radius": {},
	"border-bottom-left-radius": {}, "border-bottom-right-radius": {},
	"border-inline": {}, "border-block": {},
	"border-inline-start": {}, "border-inline-end": {},
	// Background
	"background": {}, "background-color": {}, "background-image": {},
	"background-repeat": {}, "background-position": {}, "background-attachment": {},
	"background-size": {}, "background-clip": {}, "background-origin": {},
	// Color / typography
	"color": {},
	"font":  {}, "font-family": {}, "font-size": {}, "font-style": {},
	"font-weight": {}, "font-variant": {}, "font-stretch": {},
	"font-kerning": {}, "font-feature-settings": {},
	"font-synthesis":            {},
	"font-synthesis-weight":     {},
	"font-synthesis-style":      {},
	"font-synthesis-small-caps": {},
	"font-synthesis-position":   {},
	"line-height": {}, "letter-spacing": {}, "word-spacing": {},
	"text-align": {}, "text-decoration": {}, "text-decoration-line": {},
	"text-decoration-color": {}, "text-decoration-style": {}, "text-decoration-thickness": {},
	"text-emphasis": {}, "text-emphasis-color": {}, "text-emphasis-style": {}, "text-emphasis-position": {},
	"text-indent": {}, "text-transform": {}, "text-shadow": {}, "text-overflow": {},
	"white-space": {}, "word-break": {}, "word-wrap": {}, "overflow-wrap": {},
	"writing-mode": {}, "direction": {}, "unicode-bidi": {},
	// Layout
	"display":          {},
	"position":         {},
	"top":              {},
	"right":            {},
	"bottom":           {},
	"left":             {},
	"float":            {},
	"clear":            {},
	"z-index":          {},
	"visibility":       {},
	"overflow":             {},
	"overflow-x":           {},
	"overflow-y":           {},
	"overflow-clip-margin": {},
	"vertical-align":   {},
	"aspect-ratio":     {},
	"opacity":          {},
	"transform":        {},
	"transform-origin": {},
	"filter":           {},
	"clip":             {},
	"clip-path":        {},
	"will-change":      {},
	"isolation":        {},
	// Flexbox
	"flex": {}, "flex-direction": {}, "flex-wrap": {}, "flex-flow": {},
	"flex-grow": {}, "flex-shrink": {}, "flex-basis": {},
	"order": {}, "gap": {}, "row-gap": {}, "column-gap": {},
	"justify-content": {}, "justify-items": {}, "justify-self": {},
	"align-items": {}, "align-self": {}, "align-content": {},
	"place-items": {}, "place-self": {}, "place-content": {},
	// Grid
	"grid":                  {},
	"grid-template":         {},
	"grid-template-columns": {}, "grid-template-rows": {}, "grid-template-areas": {},
	"grid-auto-columns": {}, "grid-auto-rows": {}, "grid-auto-flow": {},
	"grid-area": {}, "grid-column": {}, "grid-row": {},
	"grid-column-start": {}, "grid-column-end": {},
	"grid-row-start": {}, "grid-row-end": {},
	// Lists, tables
	"list-style": {}, "list-style-type": {}, "list-style-position": {}, "list-style-image": {},
	"table-layout":    {},
	"border-collapse": {},
	"border-spacing":  {},
	"caption-side":    {},
	"empty-cells":     {},
	// Effects
	"box-shadow": {},
	"outline":    {}, "outline-width": {}, "outline-style": {}, "outline-color": {}, "outline-offset": {},
	// Multicol — column-gap is listed under Flexbox above
	"column-count": {}, "column-width": {}, "columns": {},
	"column-rule":       {},
	"column-rule-width": {}, "column-rule-style": {}, "column-rule-color": {},
	"column-span":       {},
	"column-fill":       {},
	"break-before":      {},
	"break-after":       {},
	"break-inside":      {},
	"page-break-before": {}, "page-break-after": {}, "page-break-inside": {},
	// Animations / transitions
	"transition": {}, "transition-property": {}, "transition-duration": {},
	"transition-timing-function": {}, "transition-delay": {},
	"animation": {}, "animation-name": {}, "animation-duration": {},
	"animation-timing-function": {}, "animation-delay": {},
	"animation-iteration-count": {}, "animation-direction": {},
	"animation-fill-mode": {}, "animation-play-state": {},
	// Misc
	"content":           {},
	"counter-reset":     {},
	"counter-increment": {},
	"quotes":            {},
	"cursor":            {},
	"pointer-events":    {},
	"user-select":       {},
	"resize":            {},
	"scroll-behavior":   {},
	// CSS3 sizing keywords / logical props
	"inline-size":     {},
	"block-size":      {},
	"min-inline-size": {},
	"max-inline-size": {},
	"min-block-size":  {},
	"max-block-size":  {},
	// Ruby
	"ruby-position": {}, "ruby-align": {}, "ruby-merge": {},
	// Tables (logical edges)
	"all": {},
}

// trimLeadingWS removes ASCII whitespace from the head of s, matching the
// CSS whitespace set (space, tab, LF, CR, FF).
func trimLeadingWS(s string) string {
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' {
			i++
			continue
		}
		break
	}
	return s[i:]
}

// consumeKeyword consumes the ASCII-case-insensitive ident `kw` from the front
// of s if it appears as a standalone identifier (not part of a longer ident
// or function token). Returns (rest-after-keyword, ok).
func consumeKeyword(s, kw string) (string, bool) {
	if len(s) < len(kw) {
		return s, false
	}
	if !strings.EqualFold(s[:len(kw)], kw) {
		return s, false
	}
	// Must be followed by something that breaks the ident — whitespace, `(`
	// is NOT acceptable (would be `kw(` = function-token, not ident `kw`).
	if len(s) == len(kw) {
		return s[len(kw):], true
	}
	next := s[len(kw)]
	if next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '\f' {
		return s[len(kw):], true
	}
	// Anything else (alpha/digit/`(`/`-`/`_`) makes this a different token.
	return s, false
}

// hasLeadingKeyword reports whether s starts with `kw` as a standalone ident.
func hasLeadingKeyword(s, kw string) bool {
	_, ok := consumeKeyword(s, kw)
	return ok
}

// consumeBalancedParens, given input starting with `(`, returns the contents
// between the opening `(` and its matching closing `)`, the rest of the
// input after that `)`, and ok=true. If parens don't balance, returns ok=false.
func consumeBalancedParens(s string) (body, rest string, ok bool) {
	if len(s) == 0 || s[0] != '(' {
		return "", s, false
	}
	depth := 0
	inString := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inString {
				inString = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inString = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], true
			}
		}
	}
	return "", s, false
}

// isValidPropertyIdent reports whether s is a valid CSS ident-start for a
// property name. Accepts letter, `-`, or `_` first; subsequent chars must be
// letter/digit/`-`/`_`. (Numeric leading char rejected per CSS Syntax §4.3.)
func isValidPropertyIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		isExtra := c == '-' || c == '_'
		if i == 0 {
			if !isAlpha && !isExtra {
				return false
			}
		} else {
			if !isAlpha && !isDigit && !isExtra {
				return false
			}
		}
	}
	return true
}

// notResult applies the `not` operator to a supports-result.
func notResult(r supportsResult) supportsResult {
	switch r {
	case supportsTrue:
		return supportsFalse
	case supportsFalse:
		return supportsTrue
	}
	return r
}

// andResult applies the `and` operator. Per spec, `and` short-circuits to
// false if either operand is false; otherwise true.
func andResult(a, b supportsResult) supportsResult {
	if a == supportsParseFailure || b == supportsParseFailure {
		return supportsParseFailure
	}
	if a == supportsFalse || b == supportsFalse {
		return supportsFalse
	}
	return supportsTrue
}

// orResult applies the `or` operator. True if either operand is true.
func orResult(a, b supportsResult) supportsResult {
	if a == supportsParseFailure || b == supportsParseFailure {
		return supportsParseFailure
	}
	if a == supportsTrue || b == supportsTrue {
		return supportsTrue
	}
	return supportsFalse
}

// findTopLevelPseudoElement returns the byte index of the first occurrence of
// token in s that is NOT inside parentheses or brackets, or -1 if none exists.
// For the ":before" (single-colon) tokens, the preceding character must not be
// ':' (which would make it a "::before" already matched on a prior pass) or an
// identifier character (avoids matching ":beforexyz" by chance).
func findTopLevelPseudoElement(s, token string) int {
	depth := 0
	inString := byte(0)
	tlen := len(token)
	singleColon := strings.HasPrefix(token, ":") && !strings.HasPrefix(token, "::")
	for i := 0; i+tlen <= len(s); i++ {
		ch := s[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inString = ch
			continue
		case '(', '[':
			depth++
			continue
		case ')', ']':
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if s[i:i+tlen] != token {
			continue
		}
		// For single-colon legacy form, guard against matching ":beforexyz"
		// or matching the ":before" tail of "::before". The double-colon form
		// is matched first by the caller's ordered list, so by the time we
		// look for ":before" any "::before" has already been extracted.
		if singleColon {
			if i > 0 && s[i-1] == ':' {
				continue
			}
		}
		return i
	}
	return -1
}

// Phase 17: parseSelector parses a complex CSS selector
func parseSelector(selectorStr string) Selector {
	selectorStr = strings.TrimSpace(selectorStr)

	if selectorStr == "" {
		return Selector{Raw: "", Parts: []SelectorPart{}, Specificity: 0}
	}

	// Phase 11: Check for pseudo-element (::before/::after or CSS 2.1 :before/:after)
	// If there's a space before the pseudo-element (e.g., ".foo :after"), it applies to
	// descendants only, not the element matched by the selector itself.
	//
	// Pseudo-element tokens nested inside functional pseudo-classes (e.g.,
	// :is(*, ::before)) MUST NOT be extracted here — they belong to the
	// inner selector list and are handled by the :is() matcher's contextual
	// invalidation logic. Use a paren-depth-aware scan rather than
	// strings.Contains so we only extract top-level pseudo-elements.
	pseudoElement := ""
	pseudoElementForDescendants := false
	// Ordered so longer/double-colon variants are tried first (e.g. "::before"
	// matched before ":before", "::first-letter" before ":first-letter").
	pseudoElementTokens := []struct {
		token string
		name  string
	}{
		{"::before", "before"},
		{"::after", "after"},
		{"::first-letter", "first-letter"},
		{"::first-line", "first-line"},
		{"::marker", "marker"},
		{":before", "before"},
		{":after", "after"},
		{":first-letter", "first-letter"},
		{":first-line", "first-line"},
	}
	for _, pe := range pseudoElementTokens {
		idx := findTopLevelPseudoElement(selectorStr, pe.token)
		if idx < 0 {
			continue
		}
		pseudoElement = pe.name
		if idx > 0 && selectorStr[idx-1] == ' ' {
			pseudoElementForDescendants = true
		}
		selectorStr = selectorStr[:idx] + selectorStr[idx+len(pe.token):]
		selectorStr = strings.TrimSpace(selectorStr)
		break
	}
	// If pseudo-element is for descendants only, clear it from direct matching
	// but record it somehow (we'll use a convention: if PseudoElement starts with "descendant:",
	// it means the element must be a descendant of the selector match)
	if pseudoElementForDescendants && pseudoElement != "" {
		pseudoElement = "descendant:" + pseudoElement
	}

	// Per CSS Selectors Level 3 §6.6, a bare pseudo-element like "::marker" is
	// shorthand for "*::marker" — the universal selector with the pseudo-element
	// attached. Without this, the parts list ends up empty and MatchesSelector
	// rejects every node, so rules like "::marker { color: white }" never apply.
	if selectorStr == "" && pseudoElement != "" {
		selectorStr = "*"
	}

	// Split by combinators while preserving them
	parts := make([]SelectorPart, 0)
	combinators := make([]CombinatorType, 0)

	// Tokenize the selector
	tokens := tokenizeSelector(selectorStr)

	// Build selector parts and combinators
	currentPart := ""
	for _, token := range tokens {
		switch token {
		case ">", "+", "~":
			if currentPart != "" {
				parts = append(parts, parseSelectorPart(currentPart))
				currentPart = ""
			}
			// If last combinator was a space (descendant), replace it with the explicit combinator
			// This handles "A > B" being tokenized as ["A", " ", ">", " ", "B"]
			var comb CombinatorType
			switch token {
			case ">":
				comb = ChildCombinator
			case "+":
				comb = AdjacentSiblingCombinator
			case "~":
				comb = GeneralSiblingCombinator
			}
			if len(combinators) > 0 && len(combinators) == len(parts) {
				// Replace trailing space combinator
				combinators[len(combinators)-1] = comb
			} else {
				combinators = append(combinators, comb)
			}
		case " ":
			// Descendant combinator (space)
			if currentPart != "" {
				parts = append(parts, parseSelectorPart(currentPart))
				currentPart = ""
				combinators = append(combinators, DescendantCombinator)
			}
		default:
			currentPart += token
		}
	}

	// Add final part
	if currentPart != "" {
		parts = append(parts, parseSelectorPart(currentPart))
	}

	// Calculate specificity: count IDs (100), classes (10), elements (1)
	specificity := 0
	for _, part := range parts {
		if part.ID != "" {
			specificity += 100
		}
		specificity += len(part.Classes) * 10
		specificity += len(part.Attributes) * 10
		for _, pc := range part.PseudoClasses {
			if strings.HasPrefix(pc, "is(") {
				// :is() takes specificity of its most specific argument
				arg := pc[len("is(") : len(pc)-1]
				sels := splitSelectorGroup(strings.TrimSpace(arg))
				maxSpec := 0
				for _, sel := range sels {
					innerSel := parseSelector(strings.TrimSpace(sel))
					if innerSel.Specificity > maxSpec {
						maxSpec = innerSel.Specificity
					}
				}
				specificity += maxSpec
			} else if strings.HasPrefix(pc, "where(") {
				// :where() has zero specificity
			} else {
				specificity += 10
			}
		}
		if part.Element != "" && part.Element != "*" {
			specificity += 1
		}
	}

	// Set legacy fields for backward compatibility (simple selectors only)
	legacyType := ElementSelector
	legacyValue := ""
	if len(parts) == 1 && len(combinators) == 0 {
		part := parts[0]
		if part.ID != "" {
			legacyType = IDSelector
			legacyValue = part.ID
		} else if len(part.Classes) == 1 && part.Element == "" {
			legacyType = ClassSelector
			legacyValue = part.Classes[0]
		} else if part.Element != "" && part.ID == "" && len(part.Classes) == 0 {
			legacyType = ElementSelector
			legacyValue = part.Element
		}
	}

	return Selector{
		Raw:           selectorStr,
		Parts:         parts,
		Combinators:   combinators,
		Specificity:   specificity,
		PseudoElement: pseudoElement,
		Type:          legacyType,
		Value:         legacyValue,
	}
}

// tokenizeSelector splits a selector into tokens (handling combinators)
func tokenizeSelector(s string) []string {
	tokens := make([]string, 0)
	current := ""
	inBracket := false
	parenDepth := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '(' {
			parenDepth++
			current += string(ch)
		} else if ch == ')' {
			parenDepth--
			current += string(ch)
		} else if ch == '[' {
			inBracket = true
			current += string(ch)
		} else if ch == ']' {
			inBracket = false
			current += string(ch)
		} else if !inBracket && parenDepth == 0 && (ch == '>' || ch == '+' || ch == '~' || ch == ' ') {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
			if ch != ' ' || (ch == ' ' && len(tokens) > 0) {
				// Only add space if it's a meaningful separator
				if ch == ' ' {
					// Check if last token was a combinator
					if len(tokens) > 0 {
						lastToken := tokens[len(tokens)-1]
						if lastToken != ">" && lastToken != "+" && lastToken != "~" && lastToken != " " {
							tokens = append(tokens, " ")
						}
					}
				} else {
					tokens = append(tokens, string(ch))
				}
			}
		} else {
			current += string(ch)
		}
	}

	if current != "" {
		tokens = append(tokens, current)
	}

	return tokens
}

// parseSelectorPart parses a single selector part like "div.class1.class2#id[attr=value]"
func parseSelectorPart(s string) SelectorPart {
	part := SelectorPart{
		Classes:       make([]string, 0),
		Attributes:    make([]AttributeSelector, 0),
		PseudoClasses: make([]string, 0),
	}

	s = strings.TrimSpace(s)
	if s == "" {
		return part
	}

	// Parse element, classes, ID, and attributes
	i := 0

	// Check for element (must come first)
	if s[i] != '.' && s[i] != '#' && s[i] != '[' && s[i] != ':' {
		// Read element name until we hit a special character
		j := i
		for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' && s[j] != ':' {
			j++
		}
		part.Element = strings.ToLower(s[i:j])
		i = j
	}

	// Parse classes, ID, and attributes
	for i < len(s) {
		if s[i] == '.' {
			// Class
			i++
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' && s[j] != ':' {
				j++
			}
			part.Classes = append(part.Classes, s[i:j])
			i = j
		} else if s[i] == '#' {
			// ID
			i++
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '#' && s[j] != '[' && s[j] != ':' {
				j++
			}
			part.ID = s[i:j]
			i = j
		} else if s[i] == ':' {
			// Pseudo-class (skip :: pseudo-elements, handled separately)
			if i+1 < len(s) && s[i+1] == ':' {
				// This is a pseudo-element, stop parsing
				break
			}
			i++ // skip the ':'
			j := i
			depth := 0
			for j < len(s) {
				if s[j] == '(' {
					depth++
				} else if s[j] == ')' {
					depth--
					if depth == 0 {
						j++ // include the closing paren
						break
					}
				} else if depth == 0 && (s[j] == '.' || s[j] == '#' || s[j] == '[' || s[j] == ':') {
					break
				}
				j++
			}
			if j > i {
				part.PseudoClasses = append(part.PseudoClasses, s[i:j])
			}
			i = j
		} else if s[i] == '[' {
			// Attribute
			j := i + 1
			for j < len(s) && s[j] != ']' {
				j++
			}
			if j < len(s) {
				attrStr := s[i+1 : j]
				attr := parseAttributeSelector(attrStr)
				part.Attributes = append(part.Attributes, attr)
				i = j + 1
			} else {
				break
			}
		} else {
			i++
		}
	}

	return part
}

// parseAttributeSelector parses an attribute selector like "type=text" or "href^=https"
func parseAttributeSelector(s string) AttributeSelector {
	// Find the operator
	operators := []string{"^=", "$=", "*=", "~=", "|=", "="}

	for _, op := range operators {
		if idx := strings.Index(s, op); idx != -1 {
			name := strings.TrimSpace(s[:idx])
			value := strings.TrimSpace(s[idx+len(op):])
			// Remove quotes from value
			value = strings.Trim(value, `"'`)
			// Handle CSS escape sequences (e.g., second\ two → second two)
			value = strings.ReplaceAll(value, `\ `, " ")
			return AttributeSelector{
				Name:     name,
				Operator: op,
				Value:    value,
			}
		}
	}

	// No operator, just attribute name (existence check)
	return AttributeSelector{
		Name:     strings.TrimSpace(s),
		Operator: "",
		Value:    "",
	}
}

// Phase 22: EvaluateMediaQuery returns the 3-valued result of evaluating a
// media query for the given viewport. Mirrors Blink's
// MediaQueryEvaluator::Eval() in
// third_party/blink/renderer/core/css/media_query_evaluator.cc @ 4883d11f.
//
// Per CSS Media Queries 4, media TYPES are 2-valued (a known type matches
// or fails; an unknown type fails), but media FEATURES are 3-valued: an
// unknown feature evaluates to Unknown. The `not` restrictor uses Kleene
// logic — `not unknown == unknown` — so unknown queries do not become
// true under negation. Callers must explicitly check
// `result == MediaQueryTrue` to decide whether a rule applies; an Unknown
// or False result both mean "do not apply".
func EvaluateMediaQuery(mq *MediaQuery, viewportWidth, viewportHeight float64) MediaQueryResult {
	if mq == nil {
		// No media query = always matches
		return MediaQueryTrue
	}

	// Type-level match (Blink's MediaTypeMatch): empty string and "all"
	// always match; otherwise we accept "screen" because we render to a
	// screen-style raster. Unknown types fail at this gate — they are not
	// 3-valued; they're just false.
	typeResult := MediaQueryFalse
	switch mq.MediaType {
	case "", "all", "screen":
		typeResult = MediaQueryTrue
	}
	if typeResult == MediaQueryFalse {
		return applyMediaRestrictor(mq.Restrictor, MediaQueryFalse)
	}

	// Combine all feature conditions with AND, using Kleene logic.
	// AND-identity is True; True∧X = X; False short-circuits to False;
	// Unknown ∧ True = Unknown; Unknown ∧ Unknown = Unknown.
	exprResult := MediaQueryTrue
	for _, cond := range mq.Conditions {
		condResult := evaluateMediaCondition(cond, viewportWidth, viewportHeight)
		exprResult = mediaAnd(exprResult, condResult)
		if exprResult == MediaQueryFalse {
			break // false absorbs in AND
		}
	}

	return applyMediaRestrictor(mq.Restrictor, exprResult)
}

// EvaluateMediaQueryList returns the Kleene 3-valued result of evaluating a
// comma-separated media-query-list (CSS Media Queries 4 §2.1), used by the
// conditional `@import url(...) <media-query-list>` form (CSS Cascade 4 §3.1).
// The branches are combined with OR using Kleene logic — True absorbs;
// otherwise Unknown propagates. An empty/nil list means "no condition" and
// evaluates to True (the unconditional @import case). Mirrors Blink's
// `MediaQuerySet::HasMediaQueries` + `MediaQueryEvaluator::Eval` over each
// branch at third_party/blink/renderer/core/css/media_query_set.cc and
// media_query_evaluator.cc @ 4883d11f.
func EvaluateMediaQueryList(list []*MediaQuery, viewportWidth, viewportHeight float64) MediaQueryResult {
	if len(list) == 0 {
		return MediaQueryTrue
	}
	result := MediaQueryFalse
	for _, mq := range list {
		r := EvaluateMediaQuery(mq, viewportWidth, viewportHeight)
		result = mediaOr(result, r)
		if result == MediaQueryTrue {
			return MediaQueryTrue // True absorbs in OR.
		}
	}
	return result
}

// RuleMediaApplies returns whether a rule's combined media gating evaluates
// to definitely-true at the given viewport. Combines the @import-level
// media-query-list (OR-of-MQs) AND-with the @media-level single MediaQuery,
// mirroring Blink's `CSSImportRule::ApplyRule` chained check at
// third_party/blink/renderer/core/css/css_import_rule.cc @ 4883d11f.
// Per CSS Media Queries 4 §3.1, the result is Kleene 3-valued and only a
// definitely-True result causes the rule to apply.
func RuleMediaApplies(rule *Rule, viewportWidth, viewportHeight float64) bool {
	importResult := EvaluateMediaQueryList(rule.ImportMediaQueries, viewportWidth, viewportHeight)
	mediaResult := EvaluateMediaQuery(rule.MediaQuery, viewportWidth, viewportHeight)
	return mediaAnd(importResult, mediaResult) == MediaQueryTrue
}

// applyMediaRestrictor mirrors Blink's ApplyRestrictor() in
// third_party/blink/renderer/core/css/media_query_evaluator.cc @ 4883d11f.
// The "not" restrictor swaps True↔False but leaves Unknown unchanged.
// "only" is parsed for compatibility with older user agents but has no
// effect on evaluation.
func applyMediaRestrictor(r MediaQueryRestrictor, v MediaQueryResult) MediaQueryResult {
	if r != MediaRestrictorNot {
		return v
	}
	switch v {
	case MediaQueryTrue:
		return MediaQueryFalse
	case MediaQueryFalse:
		return MediaQueryTrue
	}
	return MediaQueryUnknown
}

// mediaOr is Kleene OR: T∨X=T, F∨X=X, U∨U=U, U∨F=U.
func mediaOr(a, b MediaQueryResult) MediaQueryResult {
	if a == MediaQueryTrue || b == MediaQueryTrue {
		return MediaQueryTrue
	}
	if a == MediaQueryUnknown || b == MediaQueryUnknown {
		return MediaQueryUnknown
	}
	return MediaQueryFalse
}

// mediaAnd is Kleene AND: F∧X=F, T∧X=X, U∧U=U, U∧T=U, U∧F=F.
func mediaAnd(a, b MediaQueryResult) MediaQueryResult {
	if a == MediaQueryFalse || b == MediaQueryFalse {
		return MediaQueryFalse
	}
	if a == MediaQueryUnknown || b == MediaQueryUnknown {
		return MediaQueryUnknown
	}
	return MediaQueryTrue
}

// Phase 22: evaluateMediaCondition evaluates a single parenthesized media
// feature expression. Returns Kleene 3-valued: True/False for recognized
// features, Unknown for any feature name the renderer doesn't model.
// Mirrors Blink's per-feature Eval*() dispatch in
// third_party/blink/renderer/core/css/media_query_evaluator.cc @ 4883d11f,
// which returns KleeneValue::kUnknown for features that the evaluator does
// not implement.
func evaluateMediaCondition(cond MediaCondition, viewportWidth, viewportHeight float64) MediaQueryResult {
	feature := strings.TrimSpace(strings.ToLower(cond.Feature))
	value := strings.TrimSpace(strings.ToLower(cond.Value))

	boolToResult := func(b bool) MediaQueryResult {
		if b {
			return MediaQueryTrue
		}
		return MediaQueryFalse
	}

	// Handle non-numeric media features
	switch feature {
	case "prefers-color-scheme":
		// We render static PNGs in light mode
		return boolToResult(value == "light")
	case "prefers-reduced-motion":
		// Static renderer — no motion, default is no-preference
		return boolToResult(value == "no-preference")
	case "prefers-reduced-transparency":
		return boolToResult(value == "no-preference" || value == "")
	case "prefers-contrast":
		return boolToResult(value == "no-preference" || value == "")

	// Forced colors — no forced colors in desktop static renderer
	case "forced-colors":
		return boolToResult(value == "none")
	case "inverted-colors":
		return boolToResult(value == "none")

	// Interaction media features — desktop defaults (fine pointer/mouse, hover capable)
	case "pointer":
		// Desktop has a fine pointer (mouse); coarse = touch = false; none = false
		return boolToResult(value == "fine" || value == "")
	case "any-pointer":
		// Desktop has fine pointer; also accept "none" as secondary device
		return boolToResult(value == "fine" || value == "")
	case "hover":
		// Desktop supports hover
		return boolToResult(value == "hover" || value == "")
	case "any-hover":
		// Desktop supports hover
		return boolToResult(value == "hover" || value == "")

	// Orientation — based on viewport aspect ratio (800×600 = landscape)
	case "orientation":
		if viewportWidth >= viewportHeight {
			return boolToResult(value == "landscape")
		}
		return boolToResult(value == "portrait")

	// Display mode — we render as a standard browser
	case "display-mode":
		return boolToResult(value == "browser" || value == "")

	// Scripting — we don't execute JS but claim "none" so sites don't show JS-only fallback
	case "scripting":
		return boolToResult(value == "none" || value == "")

	// Color media features
	case "color":
		// boolean feature: true if it has color (our renderer supports color)
		// value is empty for boolean test, or a number for min-color etc.
		return MediaQueryTrue
	case "color-index":
		// We don't use an indexed color palette
		return boolToResult(value == "0" || value == "")
	case "monochrome":
		// We are not monochrome
		return boolToResult(value == "0" || value == "")

	// Resolution — always match for static renderer
	case "resolution", "min-resolution", "max-resolution":
		return MediaQueryTrue

	// Legacy device dimension features — approximate as viewport
	case "device-width", "device-height",
		"min-device-width", "min-device-height",
		"max-device-width", "max-device-height":
		return MediaQueryTrue

	// color-gamut — Media Queries 4 §4.8. louis14 renders sRGB output, so
	// `srgb` matches and the wider gamuts (`p3`, `rec2020`) do not. Returning
	// definite True/False (not Unknown) is required so that the `not (color-
	// gamut: X)` branch in the `(X), not (X)` OR pattern (mq-gamut-001/002/
	// 004) flips cleanly under Kleene `not`. Mirrors Blink's
	// MediaQueryEvaluator::EvalColorGamut at
	// third_party/blink/renderer/core/css/media_query_evaluator.cc @ 4883d11f.
	case "color-gamut":
		switch value {
		case "", "srgb":
			return MediaQueryTrue
		case "p3", "rec2020":
			return MediaQueryFalse
		}
		return MediaQueryUnknown

	// dynamic-range — report standard
	case "dynamic-range":
		return boolToResult(value == "standard" || value == "")

	// video-dynamic-range
	case "video-dynamic-range":
		return boolToResult(value == "standard" || value == "")

	// overflow-block, overflow-inline
	case "overflow-block":
		return boolToResult(value == "scroll" || value == "optional-paged" || value == "")
	case "overflow-inline":
		return boolToResult(value == "scroll" || value == "")

	// update — we render to a static image (none update)
	case "update":
		return boolToResult(value == "none" || value == "")

	// grid — not a grid device (bitmap display)
	case "grid":
		return boolToResult(value == "0" || value == "")

	// aspect-ratio / device-aspect-ratio — Media Queries 4 §4.6/§4.7.
	// louis14 treats device-aspect-ratio identically to aspect-ratio because
	// the static renderer has no device pixel concept distinct from the
	// viewport. Comparison uses cross-multiplication (vw*den vs vh*num) to
	// avoid float drift and to handle 0/0 correctly: per the spec note in
	// aspect-ratio-004 / device-aspect-ratio-002, `0/0` is the "infinity"
	// special case where both sides of `min`/`max` collapse to 0 OP 0 and
	// always match. Mirrors Blink's CompareAspectRatioValue + EvalAspectRatio
	// at third_party/blink/renderer/core/css/media_query_evaluator.cc @
	// 4883d11f.
	case "aspect-ratio", "min-aspect-ratio", "max-aspect-ratio",
		"device-aspect-ratio", "min-device-aspect-ratio", "max-device-aspect-ratio":
		if viewportHeight <= 0 {
			return MediaQueryUnknown
		}
		num, den, ok := parseRatio(cond.Value)
		if !ok {
			return MediaQueryUnknown
		}
		vwDen := viewportWidth * den
		vhNum := viewportHeight * num
		switch feature {
		case "min-aspect-ratio", "min-device-aspect-ratio":
			return boolToResult(vwDen >= vhNum)
		case "max-aspect-ratio", "max-device-aspect-ratio":
			return boolToResult(vwDen <= vhNum)
		case "aspect-ratio", "device-aspect-ratio":
			return boolToResult(vwDen == vhNum)
		}

	case "min-width", "max-width", "min-height", "max-height", "width", "height":
		numVal, unit := parseMediaLength(cond.Value)
		if unit != "px" {
			// Unparseable dimension — treat as Unknown so 3-valued logic
			// preserves the "don't apply" semantics without flipping under `not`.
			return MediaQueryUnknown
		}
		switch feature {
		case "min-width":
			return boolToResult(viewportWidth >= numVal)
		case "max-width":
			return boolToResult(viewportWidth <= numVal)
		case "min-height":
			return boolToResult(viewportHeight >= numVal)
		case "max-height":
			return boolToResult(viewportHeight <= numVal)
		case "width":
			return boolToResult(viewportWidth == numVal)
		case "height":
			return boolToResult(viewportHeight == numVal)
		}
	}

	// Unknown feature (e.g. `(unknown)` in at-media-002). Per CSS Media
	// Queries 4 §3.1, this is the 3-valued Unknown state, distinct from
	// False — important so that `not (unknown)` stays Unknown rather than
	// flipping to True.
	return MediaQueryUnknown
}

// parseRatio parses a <ratio> per CSS Values 4 §4.5.6 / Media Queries 4 §4.6:
// either `<number> / <number>` or a bare `<number>` (denominator defaults to
// 1). Whitespace around the slash is allowed. Returns numerator, denominator,
// ok. Negative values are rejected. The degenerate `0/0` case is rewritten to
// `1/0` per the aspect-ratio-004 / device-aspect-ratio-002 / -004 spec note —
// "0/0 is converted into 1/0" (infinity) — so that cross-multiplication in
// the aspect-ratio comparison yields the spec-mandated min=never / max=always
// behavior. Mirrors Blink's CSSRatioValue parse + MediaQueryExpValue::Ratio()
// at third_party/blink/renderer/core/css/parser/media_query_parser.cc @
// 4883d11f.
func parseRatio(val string) (num, den float64, ok bool) {
	val = strings.TrimSpace(val)
	parts := strings.SplitN(val, "/", 2)
	n, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil || n < 0 {
		return 0, 0, false
	}
	if len(parts) == 1 {
		return n, 1, true
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil || d < 0 {
		return 0, 0, false
	}
	// "0/0" → "1/0" (infinity) per Media Queries 4 spec note carried in the
	// WPT aspect-ratio-004 / device-aspect-ratio-002 / -004 assertions.
	if n == 0 && d == 0 {
		n = 1
	}
	return n, d, true
}

// Phase 22: parseMediaLength parses a length value and returns value and unit
func parseMediaLength(val string) (float64, string) {
	val = strings.TrimSpace(val)

	// Handle calc() expressions
	if strings.HasPrefix(val, "calc(") && strings.HasSuffix(val, ")") {
		inner := val[5 : len(val)-1]
		// 16px for font-size (media queries use initial font size, not element font-size)
		if result, ok := EvalCalcWithPercent(inner, 16.0, 0); ok {
			return result, "px"
		}
	}

	// Handle rem units (1rem = 16px for media queries per CSS spec)
	if strings.HasSuffix(val, "rem") {
		numStr := strings.TrimSuffix(val, "rem")
		var num float64
		if _, err := fmt.Sscanf(numStr, "%f", &num); err == nil {
			return num * 16.0, "px"
		}
	}

	// Handle em units (1em = 16px for media queries — initial font size)
	if strings.HasSuffix(val, "em") {
		numStr := strings.TrimSuffix(val, "em")
		var num float64
		if _, err := fmt.Sscanf(numStr, "%f", &num); err == nil {
			return num * 16.0, "px"
		}
	}

	// Check for px suffix
	if strings.HasSuffix(val, "px") {
		numStr := strings.TrimSuffix(val, "px")
		if num, err := fmt.Sscanf(numStr, "%f", new(float64)); err == nil && num == 1 {
			var value float64
			fmt.Sscanf(numStr, "%f", &value)
			return value, "px"
		}
	}

	// Try to parse as plain number (assume px)
	var value float64
	if _, err := fmt.Sscanf(val, "%f", &value); err == nil {
		return value, "px"
	}

	return 0, ""
}

// splitDeclarationParts splits a declaration block by semicolons,
// respecting string literals so semicolons inside strings are not split on.
func splitDeclarationParts(declStr string) []string {
	var parts []string
	start := 0
	inString := byte(0)
	parenDepth := 0

	for i := 0; i < len(declStr); i++ {
		ch := declStr[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(declStr) {
				i++ // skip escaped char
			} else if ch == inString {
				inString = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = ch
			continue
		}
		if ch == '(' {
			parenDepth++
			continue
		}
		if ch == ')' && parenDepth > 0 {
			parenDepth--
			continue
		}
		if ch == ';' && parenDepth == 0 {
			parts = append(parts, declStr[start:i])
			start = i + 1
		}
	}
	// Last segment (after final semicolon or if no semicolon)
	if start < len(declStr) {
		parts = append(parts, declStr[start:])
	}
	return parts
}

// unescapeCSS decodes CSS escape sequences per CSS 2.1 §4.1.3
// - \X where X is not a hex digit → literal character X (e.g., \r → r)
// - \HHHHHH (1-6 hex digits) → Unicode code point (e.g., \45 → E since 0x45 = 69 = 'E')
// - Trailing whitespace after hex escape is consumed
func unescapeCSS(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '\\' || i+1 >= len(s) {
			result.WriteByte(s[i])
			i++
			continue
		}
		// Found backslash - check what follows
		i++ // skip backslash
		// Check for hex escape (1-6 hex digits)
		hexStart := i
		for i < len(s) && i-hexStart < 6 && isHexDigit(s[i]) {
			i++
		}
		if i > hexStart {
			// Parse hex value
			hexStr := s[hexStart:i]
			codePoint, _ := strconv.ParseInt(hexStr, 16, 32)
			// CSS Syntax §4.3.8 "Consume an escaped code point": if the number
			// is zero, a surrogate (0xD800-0xDFFF), or greater than the maximum
			// allowed code point (0x10FFFF), return U+FFFD REPLACEMENT CHARACTER.
			if codePoint == 0 || (codePoint >= 0xD800 && codePoint <= 0xDFFF) || codePoint > 0x10FFFF {
				result.WriteRune('�')
			} else {
				result.WriteRune(rune(codePoint))
			}
			// Consume one trailing whitespace if present
			if i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == '\f') {
				i++
			}
		} else {
			// Single character escape - just output the character
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// isHexDigit returns true if c is a hexadecimal digit
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// DeclarationResult holds parsed declarations and their important flags
type DeclarationResult struct {
	Declarations map[string]string
	Important    map[string]bool
}

// parseDeclarations parses CSS declarations into a map.
// Invalid declarations are silently skipped (error recovery).
func parseDeclarations(declStr string) DeclarationResult {
	result := DeclarationResult{
		Declarations: make(map[string]string),
		Important:    make(map[string]bool),
	}

	parts := splitDeclarationParts(declStr)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split property: value at first colon
		colonPos := strings.Index(part, ":")
		if colonPos == -1 {
			// No colon — invalid declaration, skip
			continue
		}

		rawProperty := strings.TrimSpace(part[:colonPos])
		rawValue := strings.TrimSpace(part[colonPos+1:])

		// Unescape CSS escape sequences in both property and value
		property := unescapeCSS(rawProperty)
		// IMPORTANT: Don't unescape the quotes property value here, as it needs special handling
		// in parseQuotes (layout.go) to preserve the quote string structure
		var value string
		if property != "quotes" {
			value = unescapeCSS(rawValue)
		} else {
			value = rawValue
		}

		// Skip declarations with empty property or value
		if property == "" || value == "" {
			continue
		}

		// Skip properties that start with invalid characters
		// (valid CSS properties start with a letter or hyphen)
		if property[0] != '-' && (property[0] < 'a' || property[0] > 'z') && (property[0] < 'A' || property[0] > 'Z') {
			continue
		}

		// Handle !important: strip it if valid, reject if malformed.
		// Apply the same strip to rawValue so downstream custom-property
		// validation doesn't see the priority suffix as a top-level `!`
		// token. `rawValue` differs from `value` only in unescaped sequences
		// (escapes can't appear inside the !important suffix), so the same
		// suffix-locator works for both.
		isImportant := false
		if strings.Contains(value, "!") {
			bangIdx := strings.Index(value, "!")
			afterBang := strings.TrimSpace(value[bangIdx+1:])
			if strings.EqualFold(afterBang, "important") {
				value = strings.TrimSpace(value[:bangIdx])
				isImportant = true
			} else {
				// Invalid use of ! (e.g., "red ! error") — reject entire declaration
				continue
			}
		}
		if rawBangIdx := strings.LastIndex(rawValue, "!"); rawBangIdx >= 0 {
			afterRawBang := strings.TrimSpace(rawValue[rawBangIdx+1:])
			if strings.EqualFold(afterRawBang, "important") {
				rawValue = strings.TrimSpace(rawValue[:rawBangIdx])
			}
		}

		// CSS Custom Properties §2 / §3: validate the custom-property name and
		// declared value. An invalid declaration is discarded entirely and must
		// NOT overwrite an earlier valid one — mirrors Blink's
		// CSSPropertyParser::ParseCustomPropertyDeclaration at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. We mirror the
		// isImageProperty / isValidImageValue gate pattern above so the
		// rejection path stays uniform across property categories.
		//
		// Name validation runs against the RAW (pre-unescape) form because
		// CSS escape sequences extend the set of code points an ident-token
		// can name to anything — `\27 d` legitimately names `'d`, which after
		// unescape would otherwise look like a name with an invalid `'` in
		// it. The escape-aware validator inspects the source tokens directly.
		if strings.HasPrefix(rawProperty, "--") {
			if !isValidCustomPropertyName(rawProperty) || !isValidCustomPropertyValue(rawValue) {
				continue
			}
		}

		// CSS Variables §3: a var() reference's first argument must itself be a
		// valid custom-property name. A malformed name (e.g. `var(--, fallback)`
		// where `--` is not a legal name) makes the entire declaration invalid
		// regardless of which property it's on. Reject before the value can
		// overwrite an earlier valid declaration. Use the RAW (pre-unescape)
		// form so escape sequences inside the name (e.g. `var(--foo\27 d, …)`)
		// validate against the source tokens, not the unescaped string.
		if strings.Contains(rawValue, "var(") && hasInvalidVarReference(rawValue) {
			continue
		}

		// CSS 2.1: Reject bare non-zero numbers for length properties (must have units)
		if isLengthProperty(property) && isInvalidBareNumber(value) {
			continue
		}

		// Validate color property values before they enter the cascade
		if isColorProperty(property) {
			if !isValidColorValue(value) {
				continue
			}
		}

		// Validate image property values (e.g. background-image) before
		// they enter the cascade. A failed-to-parse declaration must NOT
		// overwrite an earlier valid one — this mirrors Blink's
		// CSSPropertyParser::ParseValue gate, where parse failure causes
		// the declaration to be discarded before reaching the property set.
		if isImageProperty(property) {
			if !isValidImageValue(value) {
				continue
			}
		}

		// The `all` shorthand cannot be expanded at parse time because its
		// resolution depends on cascade-time context (the property snapshot for
		// `revert`, inheritance for `inherit`, and the per-element set of
		// declared longhands that must be reset). Store it verbatim so the
		// cascade-time expandShorthand call applies it after every other
		// declaration in the rule. Mirrors Blink's all.cc which never participates
		// in StyleBuilder expansion the way other shorthands do; it's handled by
		// the cascade's CSSProperty(kAll)::ApplyValue at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		if property == "all" {
			result.Declarations["all"] = value
			if isImportant {
				result.Important["all"] = true
			}
			continue
		}

		// Expand shorthand properties (reuse from Phase 2)
		style := NewStyle()
		expandShorthand(style, property, value)

		// Copy all expanded properties to declarations, validating color values
		for k, v := range style.Properties {
			if isColorProperty(k) {
				if !isValidColorValue(v) {
					continue
				}
			}
			if isImageProperty(k) {
				if !isValidImageValue(v) {
					continue
				}
			}
			result.Declarations[k] = v
			if isImportant {
				result.Important[k] = true
			}
		}
	}

	return result
}

// isLengthProperty returns true for CSS properties that expect length values
func isLengthProperty(prop string) bool {
	switch prop {
	case "width", "height", "min-width", "min-height", "max-width", "max-height",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border-width", "border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
		"top", "right", "bottom", "left",
		"font-size", "letter-spacing", "word-spacing",
		"text-indent", "vertical-align":
		// NOTE: line-height is NOT included here because CSS allows bare unitless
		// numbers (e.g., "line-height: 2") as multipliers of font-size.
		return true
	}
	return false
}

// isColorProperty returns true for CSS properties that expect color values
func isColorProperty(prop string) bool {
	switch prop {
	case "color", "background-color",
		"border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		return true
	}
	// Note: "border-color" is a shorthand that accepts multiple space-separated
	// color values (e.g., "green green green green"). It must NOT be validated
	// as a single color — expandShorthand will split it into longhands.
	return false
}

// isValidColorValue checks if a value is a valid CSS color (parsed color,
// currentcolor, or one of the CSS-wide keywords accepted on every property).
func isValidColorValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "currentcolor" {
		return true
	}
	// CSS Cascade 4 §6.1: every CSS property accepts the five CSS-wide
	// keywords. They reach the longhand value verbatim and are resolved at
	// cascade time by resolveCSSWideKeywords / resolveInheritValues.
	if lower == "inherit" || lower == "initial" || lower == "unset" ||
		lower == "revert" || lower == "revert-layer" {
		return true
	}
	// var() references are resolved later — always valid at parse time
	if strings.Contains(value, "var(") {
		return true
	}
	_, ok := ParseColor(value)
	return ok
}

// isImageProperty returns true for CSS properties that expect <image> values.
// Used by the declaration parser to reject invalid image values (e.g. a
// gradient with an invalid angle unit spelling like "90degree") before they
// can poison the cascade by overwriting an earlier valid declaration.
//
// Blink reference: properties whose grammar is <image> in
// third_party/blink/renderer/core/css_properties.json5
// (Chromium 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func isImageProperty(prop string) bool {
	switch prop {
	case "background-image":
		return true
	}
	return false
}

// isValidImageValue reports whether value is a valid CSS <image> (or
// comma-separated list of <image>s, as used for multi-layer backgrounds).
// Each layer must be one of: "none", a url(...), a gradient function, an
// image-set()/-webkit-image-set(), or a global keyword (inherit/initial/
// unset/revert). var() references defer validation.
func isValidImageValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	// var() references are resolved later — always valid at parse time.
	if strings.Contains(trimmed, "var(") {
		return true
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "none", "inherit", "initial", "unset", "revert", "revert-layer":
		return true
	}
	// Multi-layer values: every comma-separated layer must be a valid <image>.
	for _, layer := range splitCommaSeparated(trimmed) {
		if !isValidSingleImageLayer(strings.TrimSpace(layer)) {
			return false
		}
	}
	return true
}

// isValidSingleImageLayer validates one layer of a (possibly multi-layer)
// <image> value. See isValidImageValue for the layer grammar.
func isValidSingleImageLayer(layer string) bool {
	if layer == "" {
		return false
	}
	lower := strings.ToLower(layer)
	switch lower {
	case "none":
		return true
	}
	// url(...) is always accepted at parse time (resource resolution
	// happens later; an unresolved URL still constitutes a valid <image>
	// per the CSS Images spec).
	if strings.HasPrefix(lower, "url(") {
		return true
	}
	// image-set() / -webkit-image-set() — accept the wrapper; inner
	// candidate validation is not required for cascade gating.
	if strings.HasPrefix(lower, "image-set(") || strings.HasPrefix(lower, "-webkit-image-set(") {
		return true
	}
	// Gradient functions — defer to the gradient parser, which already
	// rejects invalid spellings (e.g. "90degree" for angles).
	if strings.HasPrefix(lower, "linear-gradient(") ||
		strings.HasPrefix(lower, "radial-gradient(") ||
		strings.HasPrefix(lower, "conic-gradient(") ||
		strings.HasPrefix(lower, "repeating-linear-gradient(") ||
		strings.HasPrefix(lower, "repeating-radial-gradient(") ||
		strings.HasPrefix(lower, "repeating-conic-gradient(") {
		_, ok := GetGradient(layer)
		return ok
	}
	return false
}

// isInvalidBareNumber returns true if value is a non-zero number with no unit
func isInvalidBareNumber(value string) bool {
	num, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false // not a bare number
	}
	return num != 0 // zero without units is valid
}

// isValidCustomPropertyName reports whether name is a syntactically valid
// CSS custom-property name per CSS Custom Properties §2.
//
// A custom property name starts with two U+002D HYPHEN-MINUS characters,
// followed by a non-empty <ident-token>-style sequence. The bare string "--"
// is NOT a valid name (Blink CSS-Variables tests treat it as an invalid
// declaration; mirrored by Chromium CSSPropertyParser at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// The input is the RAW pre-unescape form. We accept ident code points
// (letters, digits, hyphen, underscore, non-ASCII) directly, and treat
// `\<hex...>` or `\<single-non-newline>` as a valid escape that consumes
// one ident position regardless of the unescaped value (CSS Syntax §4.2 /
// §4.3.8 — escape sequences extend the ident grammar to any code point).
func isValidCustomPropertyName(name string) bool {
	if !strings.HasPrefix(name, "--") {
		return false
	}
	rest := name[2:]
	if rest == "" {
		return false
	}
	i := 0
	for i < len(rest) {
		c := rest[i]
		if c == '\\' && i+1 < len(rest) {
			// CSS escape sequence: \<hex>{1,6} (optional trailing whitespace)
			// or \<any-non-newline>. Either form contributes one ident
			// position to the name.
			j := i + 1
			hexEnd := j
			for hexEnd < len(rest) && hexEnd-j < 6 && isHexDigit(rest[hexEnd]) {
				hexEnd++
			}
			if hexEnd > j {
				i = hexEnd
				// One whitespace after a hex escape is consumed by the
				// tokenizer (CSS Syntax §4.3.8 step 2).
				if i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n' || rest[i] == '\r' || rest[i] == '\f') {
					i++
				}
			} else {
				// Single-char escape — must not be a newline.
				next := rest[j]
				if next == '\n' || next == '\r' || next == '\f' {
					return false
				}
				i = j + 1
			}
			continue
		}
		if c == '-' || c == '_' || (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			c >= 0x80 {
			i++
			continue
		}
		return false
	}
	return true
}

// hasInvalidVarReference reports whether value contains any var() function
// call whose custom-property-name argument (the substring before the first
// top-level comma inside the var() parens) is not a valid custom-property
// name per isValidCustomPropertyName.
//
// Per CSS Variables §3 the var() notation's first argument MUST be a valid
// <custom-property-name>; otherwise the var() reference is a parse error and
// the entire declaration containing it is invalid. Mirrors Blink's
// CSSVariableParser::ConsumeVariableReference at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which fails fast on a malformed
// name token before the rest of the value is considered.
func hasInvalidVarReference(value string) bool {
	inString := byte(0)
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(value) {
				i++
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = ch
			continue
		}
		if ch == '(' && isVarFunctionAt(value, i) {
			end := matchClosingParen(value, i)
			if end < 0 {
				// Unmatched var() paren — value is invalid regardless.
				return true
			}
			inner := value[i+1 : end]
			name := inner
			if commaIdx := findTopLevelComma(inner); commaIdx >= 0 {
				name = inner[:commaIdx]
			}
			name = strings.TrimSpace(name)
			if !isValidCustomPropertyName(name) {
				return true
			}
			// Recurse into the fallback to catch nested var() with bad names.
			if commaIdx := findTopLevelComma(inner); commaIdx >= 0 {
				fallback := strings.TrimSpace(inner[commaIdx+1:])
				if fallback != "" && hasInvalidVarReference(fallback) {
					return true
				}
			}
			i = end
		}
	}
	return false
}

// isValidCustomPropertyValue reports whether value is a syntactically valid
// CSS custom-property declared value per CSS Custom Properties §3.
//
// The token stream must not contain:
//   - unmatched <)-token>, <]-token>, or <}-token>
//   - <bad-string-token> (unterminated string)
//   - top-level <semicolon-token> (handled upstream by splitDeclarationParts,
//     re-checked here for safety)
//   - top-level <delim-token> with value "!" outside !important (the parse
//     loop strips !important first, so any remaining "!" outside string/paren
//     scope at top level is banned)
//
// The rule is applied recursively to each var() fallback (the part after the
// first comma): a fallback is itself a declaration value, so a top-level
// <semicolon-token> inside it is also banned. This matches Chromium's
// CSSVariableParser behavior at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
// — see test variable-declaration-12.html which encodes
// `--a: var(--b,;)` as invalid because the fallback's `;` is top-level.
//
// Returns true for the empty string; an empty value is rejected upstream
// (parseDeclarations skips empty values before this is called).
func isValidCustomPropertyValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return isValidDeclarationValueTokens(value, true)
}

// isValidDeclarationValueTokens walks a CSS token stream and checks the
// custom-property declaration-value rules (matched brackets, no top-level
// `;`, no top-level bare `!`, recursive check of var() fallbacks). When
// topLevel is true the outer top-level checks apply; when false (recursing
// into a var() fallback) the same rules apply to that fallback.
func isValidDeclarationValueTokens(s string, topLevel bool) bool {
	inString := byte(0)
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	i := 0
	for i < len(s) {
		ch := s[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if ch == inString {
				inString = 0
			}
			if ch == '\n' {
				// Unescaped newline inside a string = <bad-string-token>.
				return false
			}
			i++
			continue
		}
		switch ch {
		case '"', '\'':
			inString = ch
			i++
		case '(':
			// If the function name preceding `(` is `var` (case-insensitive),
			// validate the fallback recursively against the same rules.
			if isVarFunctionAt(s, i) {
				end := matchClosingParen(s, i)
				if end < 0 {
					return false
				}
				inner := s[i+1 : end]
				// Split on first top-level comma — fallback is everything
				// after it. Per CSS Variables §3 the fallback is itself a
				// declaration value, so apply the same rules to it.
				if commaIdx := findTopLevelComma(inner); commaIdx >= 0 {
					fallback := strings.TrimSpace(inner[commaIdx+1:])
					if fallback != "" && !isValidDeclarationValueTokens(fallback, true) {
						return false
					}
				}
				i = end + 1
				continue
			}
			parenDepth++
			i++
		case ')':
			if parenDepth == 0 {
				return false
			}
			parenDepth--
			i++
		case '[':
			bracketDepth++
			i++
		case ']':
			if bracketDepth == 0 {
				return false
			}
			bracketDepth--
			i++
		case '{':
			braceDepth++
			i++
		case '}':
			if braceDepth == 0 {
				return false
			}
			braceDepth--
			i++
		case ';':
			if topLevel && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return false
			}
			i++
		case '!':
			if topLevel && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return false
			}
			i++
		case '<':
			// CDO token `<!--` is a single token per CSS Syntax §4.3.4 and is
			// permitted in a custom-property value (its `!` is part of the
			// CDO, not a standalone delim-token, so the top-level `!` ban
			// doesn't apply). Skip past it as a unit.
			if i+3 < len(s) && s[i+1] == '!' && s[i+2] == '-' && s[i+3] == '-' {
				i += 4
				continue
			}
			i++
		case '-':
			// CDC token `-->` is a single token. Skip as a unit.
			if i+2 < len(s) && s[i+1] == '-' && s[i+2] == '>' {
				i += 3
				continue
			}
			i++
		default:
			i++
		}
	}
	if inString != 0 || parenDepth != 0 || bracketDepth != 0 || braceDepth != 0 {
		return false
	}
	return true
}

// isVarFunctionAt reports whether the `(` at position openIdx is preceded by
// the function name `var`, with a word boundary before it so that identifiers
// like `bar` or `myvar` don't match. Case-sensitive to match the existing
// var()-resolution convention (resolveVarReferences) in style.go.
func isVarFunctionAt(s string, openIdx int) bool {
	if openIdx < 3 {
		return false
	}
	if s[openIdx-3:openIdx] != "var" {
		return false
	}
	if openIdx == 3 {
		return true
	}
	prev := s[openIdx-4]
	// Word boundary: any non-ident char qualifies.
	if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') ||
		(prev >= '0' && prev <= '9') || prev == '_' || prev == '-' || prev >= 0x80 {
		return false
	}
	return true
}

// matchClosingParen returns the index of the matching `)` for the `(` at
// position openIdx, honoring nested parens and quoted strings. Returns -1 if
// no match is found.
func matchClosingParen(s string, openIdx int) int {
	depth := 0
	inString := byte(0)
	for i := openIdx; i < len(s); i++ {
		ch := s[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inString = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// findTopLevelComma finds the index of the first comma at paren/bracket/brace
// depth 0 (and outside strings). Returns -1 if none.
func findTopLevelComma(s string) int {
	inString := byte(0)
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inString = ch
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case ',':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return i
			}
		}
	}
	return -1
}

// ParseSelector parses a CSS selector string into a Selector struct.
func ParseSelector(selectorStr string) Selector {
	return parseSelector(selectorStr)
}

// SplitSelectorGroup splits a comma-separated selector group into individual selectors.
func SplitSelectorGroup(s string) []string {
	return splitSelectorGroup(s)
}

// parseFontFaceRule parses a @font-face { ... } rule string.
func parseFontFaceRule(ruleStr string) *FontFaceRule {
	// Extract the block between { and }
	start := strings.Index(ruleStr, "{")
	end := strings.LastIndex(ruleStr, "}")
	if start < 0 || end <= start {
		return nil
	}
	block := ruleStr[start+1 : end]

	ff := &FontFaceRule{Weight: "normal", Style: "normal"}

	for _, decl := range strings.Split(block, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			continue
		}
		prop := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch prop {
		case "font-family":
			// Strip quotes
			ff.Family = strings.Trim(val, `"'`)
		case "src":
			// Parse url(...) format(...). The url() inner has already
			// been resolved by ctx.RewriteURLs at the top of
			// ParseStylesheet, so both URLData fields hold the absolute
			// form.
			srcURL, fmtName := parseFontFaceSrc(val)
			if srcURL != "" {
				ff.Src = &CSSURIValue{Data: URLData{Relative: srcURL, Absolute: srcURL}}
			}
			ff.Format = fmtName
		case "font-weight":
			ff.Weight = val
		case "font-style":
			ff.Style = val
		}
	}

	if ff.Family == "" || ff.Src == nil {
		return nil
	}
	return ff
}

// parseKeyframesRule parses a @keyframes or @-webkit-keyframes rule.
// Returns the animation name and a slice of KeyframeRule stops.
func parseKeyframesRule(ruleStr string) (string, []KeyframeRule) {
	rest := strings.TrimSpace(ruleStr)
	// Strip the at-rule prefix (@keyframes or @-webkit-keyframes)
	for _, prefix := range []string{"@-webkit-keyframes", "@keyframes"} {
		if strings.HasPrefix(rest, prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			break
		}
	}

	// Find the opening brace — name is everything before it
	bracePos := strings.Index(rest, "{")
	if bracePos < 0 {
		return "", nil
	}
	name := strings.TrimSpace(rest[:bracePos])
	if name == "" {
		return "", nil
	}

	// Extract the inner block (between outermost { and last })
	innerEnd := strings.LastIndex(rest, "}")
	if innerEnd <= bracePos {
		return "", nil
	}
	inner := rest[bracePos+1 : innerEnd]

	// Parse each keyframe stop: "from { ... }", "to { ... }", "50% { ... }"
	var frames []KeyframeRule
	for _, part := range splitRules(inner) {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, "{") {
			continue
		}
		bp := strings.Index(part, "{")
		stop := strings.TrimSpace(part[:bp])
		if stop == "" {
			continue
		}
		declEnd := strings.LastIndex(part, "}")
		if declEnd <= bp {
			continue
		}
		declStr := part[bp+1 : declEnd]
		decls := parseDeclarations(declStr)
		frames = append(frames, KeyframeRule{
			Stop:         stop,
			Declarations: decls.Declarations,
		})
	}
	return name, frames
}

// parseCounterStyleRule parses a @counter-style rule and returns a CounterStyleRule.
func parseCounterStyleRule(ruleStr string) CounterStyleRule {
	ruleStr = strings.TrimSpace(ruleStr)

	// Extract the name: everything between "@counter-style" and "{"
	bracePos := strings.Index(ruleStr, "{")
	if bracePos < 0 {
		return CounterStyleRule{}
	}
	nameStr := strings.TrimSpace(ruleStr[len("@counter-style"):bracePos])

	// Extract the block body (between { and last })
	innerEnd := strings.LastIndex(ruleStr, "}")
	if innerEnd <= bracePos {
		return CounterStyleRule{}
	}
	body := ruleStr[bracePos+1 : innerEnd]

	rule := CounterStyleRule{
		Name:    nameStr,
		System:  "symbolic",
		Suffix:  ". ",
		Prefix:  "",
		Symbols: nil,
	}

	// Parse declarations inside the block
	for _, line := range strings.Split(body, ";") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		colonPos := strings.Index(line, ":")
		if colonPos < 0 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(line[:colonPos]))
		val := strings.TrimSpace(line[colonPos+1:])
		switch prop {
		case "system":
			rule.System = strings.TrimSpace(val)
		case "symbols":
			rule.Symbols = parseCounterSymbolList(val)
		case "additive-symbols":
			rule.AdditiveSymbols = parseAdditiveSymbols(val)
		case "suffix":
			rule.Suffix = unquoteCounterString(val)
		case "prefix":
			rule.Prefix = unquoteCounterString(val)
		case "fallback":
			rule.Fallback = strings.TrimSpace(val)
		}
	}

	return rule
}

// parseCounterSymbolList parses a CSS symbols list (space-separated quoted strings or identifiers).
func parseCounterSymbolList(val string) []string {
	var symbols []string
	val = strings.TrimSpace(val)
	for len(val) > 0 {
		val = strings.TrimSpace(val)
		if val == "" {
			break
		}
		if val[0] == '"' || val[0] == '\'' {
			// Quoted string
			q := val[0]
			i := 1
			var sb strings.Builder
			for i < len(val) {
				if val[i] == '\\' && i+1 < len(val) {
					i++
					// Could be a hex escape like \1F44D
					hexStart := i
					for i < len(val) && i-hexStart < 6 && isHexDigit(val[i]) {
						i++
					}
					if i > hexStart {
						hexStr := val[hexStart:i]
						if codePoint, err := strconv.ParseInt(hexStr, 16, 32); err == nil && codePoint > 0 {
							sb.WriteRune(rune(codePoint))
						}
						// Consume optional trailing whitespace after hex escape
						if i < len(val) && (val[i] == ' ' || val[i] == '\t') {
							i++
						}
					} else {
						// Single char escape
						sb.WriteByte(val[i])
						i++
					}
					continue
				}
				if val[i] == byte(q) {
					i++ // skip closing quote
					break
				}
				sb.WriteByte(val[i])
				i++
			}
			symbols = append(symbols, sb.String())
			val = val[i:]
		} else {
			// Identifier (e.g., "a", "b", or "A", "B")
			end := strings.IndexAny(val, " \t\n\r")
			if end < 0 {
				end = len(val)
			}
			sym := val[:end]
			if sym != "" {
				symbols = append(symbols, sym)
			}
			val = val[end:]
		}
	}
	return symbols
}

// parseAdditiveSymbols parses additive-symbols like "100 C, 50 L, 10 X, 5 V, 1 I"
func parseAdditiveSymbols(val string) []AdditiveSymbol {
	var result []AdditiveSymbol
	entries := strings.Split(val, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// Format: "<integer> <symbol>"
		parts := strings.SplitN(entry, " ", 2)
		if len(parts) != 2 {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		sym := unquoteCounterString(strings.TrimSpace(parts[1]))
		result = append(result, AdditiveSymbol{Value: n, Symbol: sym})
	}
	return result
}

// unquoteCounterString strips surrounding quotes from a CSS string value.
func unquoteCounterString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		inner := s[1 : len(s)-1]
		// Process escape sequences
		var sb strings.Builder
		i := 0
		for i < len(inner) {
			if inner[i] == '\\' && i+1 < len(inner) {
				i++
				hexStart := i
				for i < len(inner) && i-hexStart < 6 && isHexDigit(inner[i]) {
					i++
				}
				if i > hexStart {
					hexStr := inner[hexStart:i]
					if codePoint, err := strconv.ParseInt(hexStr, 16, 32); err == nil && codePoint > 0 {
						sb.WriteRune(rune(codePoint))
					}
					if i < len(inner) && (inner[i] == ' ' || inner[i] == '\t') {
						i++
					}
				} else {
					sb.WriteByte(inner[i])
					i++
				}
				continue
			}
			sb.WriteByte(inner[i])
			i++
		}
		return sb.String()
	}
	return s
}

// parseFontFaceSrc extracts the URL and format from a src value like:
// url("font.woff") format("woff"), url("font.ttf") format("truetype")
// Returns the first usable URL and its format.
func parseFontFaceSrc(src string) (string, string) {
	// Split by comma for multiple sources
	sources := strings.Split(src, ",")
	for _, source := range sources {
		source = strings.TrimSpace(source)
		// Extract url(...)
		urlStart := strings.Index(source, "url(")
		if urlStart < 0 {
			continue
		}
		urlContent := source[urlStart+4:]
		urlEnd := strings.Index(urlContent, ")")
		if urlEnd < 0 {
			continue
		}
		url := strings.TrimSpace(urlContent[:urlEnd])
		url = strings.Trim(url, `"'`)

		// Extract format(...) if present
		format := ""
		fmtStart := strings.Index(source, "format(")
		if fmtStart >= 0 {
			fmtContent := source[fmtStart+7:]
			fmtEnd := strings.Index(fmtContent, ")")
			if fmtEnd >= 0 {
				format = strings.Trim(strings.TrimSpace(fmtContent[:fmtEnd]), `"'`)
			}
		}

		// Skip WOFF2 (requires Brotli decoder)
		if format == "woff2" {
			continue
		}

		return url, format
	}
	return "", ""
}
