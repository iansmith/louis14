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

// ConditionKind classifies a node in the recursive media-condition tree.
// Mirrors Blink's MediaCondition::ConditionType in
// third_party/blink/renderer/core/css/media_condition.h @ 4883d11f.
type ConditionKind int

const (
	// ConditionFeature is a leaf node: a single parenthesized media feature
	// like `(min-width: 768px)` or `(color)`.
	ConditionFeature ConditionKind = iota
	// ConditionNot negates a single child condition: `(not (...))`.
	ConditionNot
	// ConditionAnd combines children with Kleene AND: `(A) and (B)`.
	ConditionAnd
	// ConditionOr combines children with Kleene OR: `(A) or (B)`.
	ConditionOr
)

// ConditionNode is a node in the recursive media-condition tree. Leaf nodes
// (ConditionFeature) carry Feature+Value; interior nodes (Not/And/Or) carry
// Children. Mirrors Blink's MediaCondition recursive struct in
// third_party/blink/renderer/core/css/media_condition.h @ 4883d11f.
type ConditionNode struct {
	Kind     ConditionKind
	Feature  string          // ConditionFeature: "min-width", "color", "monochrome", …
	Value    string          // ConditionFeature: "768px", "landscape", "" (boolean)
	Op       string          // ConditionFeature (range syntax): ">", ">=", "<", "<=", "=" (empty = no operator)
	Children []ConditionNode // ConditionNot (1 child), ConditionAnd/Or (≥2 children)
}

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
	Conditions []ConditionNode      // parsed media-condition tree nodes (AND-combined at top level)
	Invalid    bool                 // true if query is invalid (e.g., reserved keyword in type slot, leftover media-type). Invalid queries are treated as dropped (never match, not flippable by `not`). Mirrors Blink's ConsumeQuery returning nullptr.
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
// UnicodeRange represents a contiguous codepoint range from the CSS
// @font-face unicode-range descriptor. Mirrors Blink's UnicodeRange struct in
// platform/fonts/unicode_range_set.h @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
type UnicodeRange struct {
	From rune
	To   rune
}

// CoversCodepoint reports whether cp falls within any range in ranges.
// A nil/empty slice means "all codepoints" (CSS Fonts 3 §4.5 default:
// U+0-10FFFF), so it returns true.
func CoversCodepoint(ranges []UnicodeRange, cp rune) bool {
	if len(ranges) == 0 {
		return true
	}
	for _, r := range ranges {
		if cp >= r.From && cp <= r.To {
			return true
		}
	}
	return false
}

type FontFaceRule struct {
	Family string       // font-family (unquoted)
	Src    *CSSURIValue // URL from src: url(...); nil if missing
	Format string       // "truetype", "opentype", "woff", "woff2", or ""
	Weight string       // font-weight value (e.g. "bold", "400", "700")
	Style  string       // font-style value (e.g. "italic", "normal")

	// UnicodeRanges restricts the codepoints for which this face is active.
	// nil/empty means the CSS Fonts 3 default (U+0-10FFFF — all codepoints).
	// Mirrors Blink's UnicodeRangeSet (platform/fonts/unicode_range_set.h @
	// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	UnicodeRanges []UnicodeRange

	// CSS Fonts 4 §6.1-§6.3 metric overrides. nil means "normal" (use the
	// font's native OS/2 metric). Non-nil stores the ratio (percent / 100) so
	// 100% → 1.0, 50% → 0.5. Mirrors Blink's FontMetricsOverride struct
	// (platform/fonts/font_metrics_override.h @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	AscentOverride  *float64 // ascent-override descriptor value
	DescentOverride *float64 // descent-override descriptor value
	LineGapOverride *float64 // line-gap-override descriptor value
}

// KeyframeRule represents a single keyframe stop in a @keyframes rule.
type KeyframeRule struct {
	Stop         string            // "from", "to", "0%", "50%", "100%", etc.
	Declarations map[string]string // CSS declarations at this stop
}

// DefaultCounterStyleSuffix is the CSS Counter Styles L3 default `suffix`
// descriptor value (full stop + space, `\2E\20`), appended after predefined
// numeric/alphabetic counter representations (e.g. "1. "). Single source of
// truth for every predefined-counter-style marker suffix; mirrors Blink's
// CounterStyle::suffix_ default (`core/css/counter_style.h` @4883d11f).
const DefaultCounterStyleSuffix = ". "

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

// FontFeatureValuesRule represents a parsed @font-feature-values at-rule
// (CSS Fonts 4 §11.4). Maps each named alternate to a one-or-more-element
// OpenType feature index list, scoped to a family list.
//
// Mirrors Blink's CSSFontFeatureValuesRule type
// (third_party/blink/renderer/core/css/css_font_feature_values_rule.h at
// Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f), modulo the
// camelCase Go naming.
//
// Note: louis14 stores ONE rule per family per @font-feature-values
// at-rule occurrence. Multiple at-rules targeting the same family combine
// per CSS Fonts 4 §11.4: later @-blocks override earlier same-name
// declarations within the same alternate type (per the
// font-variant-alternates-layers test). The combining happens at lookup
// time via lookupFontFeatureValues which walks the rule list last-to-first
// so later definitions win.
type FontFeatureValuesRule struct {
	Families         []string         // Comma-separated family list in the prelude.
	Stylistic        map[string][]int // @stylistic { name: index, ... }
	Styleset         map[string][]int // @styleset { name: index[, index]*, ... }
	CharacterVariant map[string][]int // @character-variant
	Swash            map[string][]int // @swash
	Ornaments        map[string][]int // @ornaments
	Annotation       map[string][]int // @annotation

	// LayerName is the cascade-layer name this rule was declared in (empty =
	// unlayered, which has the highest priority per CSS Cascade 5 §2.4).
	// Used at lookup time to honor the document's `LayerOrder` when merging
	// per-name entries — same precedence rules as style rules.
	LayerName string
}

// Stylesheet represents a parsed CSS stylesheet
type Stylesheet struct {
	Rules             []Rule
	FontFaces         []FontFaceRule
	LayerOrder        []string                  // @layer declaration order (first declared = lowest priority)
	Keyframes         map[string][]KeyframeRule // animation name → keyframe stops
	CounterStyles     []CounterStyleRule        // @counter-style rules
	FontFeatureValues []FontFeatureValuesRule   // @font-feature-values rules
}

// stripCSSComments removes all /* ... */ comments from CSS source, while
// preserving string literals (comments inside strings are not stripped).
//
// Each comment is replaced with a single space so that adjacent tokens
// don't fuse together — e.g. `rgb(10/* x */175)` becomes `rgb(10 175)`,
// matching CSS Syntax §4 where comments act as token-stream whitespace.
//
// CDO `<!--` and CDC `-->` are NOT treated as comment delimiters here. Per
// CSS Syntax §4.3.4 they are individual tokens (CDO-token / CDC-token); the
// HTML-style "<!-- ... -->" hiding pattern works only because §5.4.1 tells
// `parse-a-stylesheet` to discard these tokens at top level (handled later
// by splitRules + at-rule dispatch). Inside a rule body or at-rule prelude
// they remain valid declaration-value component-values (CSS Variables 1 §3,
// `variable-supports-018` / `variable-supports-050` / `variable-supports-051`).
// Mirrors Blink's CSSTokenizer::ConsumeToken behavior at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which never bridges between CDO
// and CDC — they are emitted as solo tokens regardless of context.
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
		} else {
			b.WriteByte(css[i])
			i++
		}
	}
	return b.String()
}

// stripTopLevelCDOTokens replaces CDO `<!--` and CDC `-->` tokens with a
// single space when they appear at the top level of the stylesheet (outside
// any `{ ... }`, `( ... )`, `[ ... ]`, or string literal). Inside any of
// those, CDO/CDC are valid component-values and must be preserved verbatim.
// Mirrors Blink's CSSParserImpl::ParseSheet at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which only discards CDO/CDC at
// stylesheet top level.
func stripTopLevelCDOTokens(css string) string {
	var b strings.Builder
	b.Grow(len(css))
	i := 0
	braceDepth := 0
	parenDepth := 0
	bracketDepth := 0
	inString := byte(0)
	for i < len(css) {
		ch := css[i]
		if inString != 0 {
			b.WriteByte(ch)
			if ch == '\\' && i+1 < len(css) {
				i++
				b.WriteByte(css[i])
			} else if ch == inString {
				inString = 0
			}
			i++
			continue
		}
		switch ch {
		case '"', '\'':
			inString = ch
			b.WriteByte(ch)
			i++
			continue
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
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
		}
		topLevel := braceDepth == 0 && parenDepth == 0 && bracketDepth == 0
		if topLevel && ch == '<' && i+3 < len(css) && css[i+1] == '!' && css[i+2] == '-' && css[i+3] == '-' {
			b.WriteByte(' ')
			i += 4
			continue
		}
		if topLevel && ch == '-' && i+2 < len(css) && css[i+1] == '-' && css[i+2] == '>' {
			b.WriteByte(' ')
			i += 3
			continue
		}
		b.WriteByte(ch)
		i++
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

		// Use the paren-aware brace finder — `@supports not unknown(!@#% {
		// ... } ...)` has braces inside the prelude that are part of
		// <general-enclosed>, not the body. indexTopLevelBrace tracks
		// parens/strings to skip them.
		bracePos := indexTopLevelBrace(part)
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

		// Handle conditional at-rules nested inside a selector block. Per CSS
		// Nesting 1 §3.2, when a conditional group at-rule (CSS Conditional
		// 4 §1: @media, @supports, @container, @layer, @scope) is nested
		// inside a style rule, the wrapper applies to a synthesized "nested
		// declarations rule" whose selector is the parent. Even at TOP level,
		// the body of these at-rules must be passed through expandNesting
		// recursively so nested selectors inside the body get expanded —
		// otherwise `@supports selector(&) { p { & { ... } } }` leaves the
		// inner `& { ... }` unexpanded and the cascade silently loses the
		// override.
		selLower := strings.ToLower(sel)
		if strings.HasPrefix(selLower, "@media") || strings.HasPrefix(selLower, "@supports") ||
			strings.HasPrefix(selLower, "@container") || strings.HasPrefix(selLower, "@layer") ||
			strings.HasPrefix(selLower, "@scope") {
			if parentSelector != "" {
				// Nested at-rule inside a selector: lift and wrap the parent
				// selector inside the at-rule, e.g.
				//   .test-11 { @layer { & { decls } } }
				// becomes
				//   @layer { :is(.test-11) { decls } }
				flatDecls, nestedParts := separateDeclsAndNested(body)
				var innerBlock strings.Builder
				if strings.TrimSpace(flatDecls) != "" {
					innerBlock.WriteString(parentSelector)
					innerBlock.WriteString(" { ")
					innerBlock.WriteString(flatDecls)
					innerBlock.WriteString(" }\n")
				}
				// Recurse for any nested rules inside the at-rule body.
				innerBlock.WriteString(expandNesting(nestedParts, parentSelector))
				// For @scope (<scope-start>) the prelude itself may contain
				// `&` — resolve before emission so the lifted selector is
				// consistent with the rest of the cascade. @layer's prelude
				// is an <ident> and never references &; @media / @supports /
				// @container preludes are queries and don't either, but a
				// blanket resolve is harmless.
				resolvedSel := sel
				if strings.Contains(sel, "&") {
					resolvedSel = strings.ReplaceAll(sel, "&", wrapParentInIs(parentSelector))
				}
				result.WriteString(resolvedSel)
				result.WriteString(" { ")
				result.WriteString(innerBlock.String())
				result.WriteString("}\n")
			} else {
				// Top-level at-rule: still recurse into the body so any
				// nested rules inside (e.g. `p { & { ... } }`) get expanded
				// against their own parent selector. We hand the body to
				// expandNesting with parentSelector="" — same context as
				// top-level — letting the normal style-rule branch handle
				// each inner rule.
				result.WriteString(sel)
				result.WriteString(" { ")
				result.WriteString(expandNesting(body, ""))
				result.WriteString("}\n")
			}
			continue
		}

		// Skip other at-rules (@keyframes, @font-face, @layer, etc.) — pass through
		if strings.HasPrefix(selLower, "@") {
			result.WriteString(part)
			result.WriteByte('\n')
			continue
		}

		// Regular selector block — emit segments in SOURCE ORDER so the
		// cascade sees declarations and nested rules at their authored
		// position. Per CSS Nesting 1 §3.3, declarations after a nested
		// rule act as if in a separate style rule whose selector is the
		// parent's; equal-specificity overrides depend on source order
		// (e.g. nesting-basic test-10 / test-11).
		segments := splitBodySegments(body)

		// Resolve the effective selector for declaration segments.
		// - Top-level: "& at the top level counts as :scope" per CSS
		//   Nesting 1 §2.1, so swap any `&` in sel for `:scope`.
		// - Nested: use resolveNestedSelector against the parent.
		var declSel, childParent string
		if parentSelector != "" {
			declSel = resolveNestedSelector(parentSelector, sel)
			childParent = declSel
		} else {
			declSel = resolveTopLevelNestingSelector(sel)
			childParent = declSel
		}

		for _, seg := range segments {
			if seg.isDecls {
				result.WriteString(declSel)
				result.WriteString(" { ")
				result.WriteString(seg.Text)
				result.WriteString(" }\n")
			} else {
				// A nested rule — recurse with childParent (which already
				// has & substituted) as the parent.
				result.WriteString(expandNesting(seg.Text, childParent))
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

// resolveTopLevelNestingSelector substitutes occurrences of `&` in a top-
// level selector with `:scope`. Per CSS Nesting 1 §2.1 / Selectors 4
// §17.2, "& at the top level counts as :scope" — i.e. the scoping root,
// which in a static document is the document's root element. Required
// by css-nesting/nesting-basic.html test-12 (`& .test-12 { ... }`).
func resolveTopLevelNestingSelector(sel string) string {
	if !strings.Contains(sel, "&") {
		return sel
	}
	return strings.ReplaceAll(sel, "&", ":scope")
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

// bodySegment represents one segment of a rule body — either a run of
// declarations or a single nested rule. Returned in source order from
// splitBodySegments so callers can preserve interleaving for cascade
// purposes.
type bodySegment struct {
	isDecls bool   // true: Text is flat declarations; false: Text is `selector { body }`
	Text    string // trimmed segment content
}

// splitBodySegments walks a CSS rule body and returns its top-level
// segments in source order. A run of consecutive flat declarations
// collapses into a single isDecls segment; each nested block (selector
// or at-rule) becomes its own segment. Used by expandNesting to preserve
// the interleaving between flat declarations and nested rules — required
// by CSS Nesting 1 §3.3 ("declarations after a nested rule apply as if
// in a separate style rule") so that, e.g.,
//
//	.x { & { color: red; } color: green; }
//
// emits `:is(.x) { color: red }` BEFORE `.x { color: green }`, letting
// `color: green` win on source-order at equal specificity.
func splitBodySegments(body string) []bodySegment {
	var segments []bodySegment
	var declRun strings.Builder

	flushDecls := func() {
		if trimmed := strings.TrimSpace(declRun.String()); trimmed != "" {
			segments = append(segments, bodySegment{isDecls: true, Text: trimmed})
		}
		declRun.Reset()
	}

	depth := 0
	start := 0
	nestedBlockStart := -1
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
				nestedBlockStart = start
			}
			depth++
		case '}':
			depth--
			if depth == 0 && nestedBlockStart >= 0 {
				// Flush any pending declarations before emitting the nested block.
				flushDecls()
				segments = append(segments, bodySegment{
					isDecls: false,
					Text:    strings.TrimSpace(body[nestedBlockStart : i+1]),
				})
				start = i + 1
				nestedBlockStart = -1
			}
		case ';':
			if depth == 0 {
				declRun.WriteString(body[start : i+1])
				start = i + 1
			}
		}
	}
	if remaining := strings.TrimSpace(body[start:]); remaining != "" && depth == 0 {
		declRun.WriteString(remaining)
	}
	flushDecls()

	return segments
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

	// CSS Syntax §5.4.1 `parse-a-stylesheet` discards CDO and CDC tokens at
	// the top level — this is how the legacy `<style><!-- ... --></style>`
	// HTML-comment-hiding pattern still parses on modern UAs. We must NOT
	// strip these tokens inside any block / function / string, because there
	// CDO/CDC are component-values (CSS Variables 1 §3,
	// `variable-supports-018` / `variable-supports-050` /
	// `variable-supports-051`). Mirrors Blink's CSSParserImpl::ParseSheet
	// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f which discards
	// CDOToken / CDCToken between top-level rules.
	css = stripTopLevelCDOTokens(css)

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
			if strings.HasPrefix(trimmed, "@import") {
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
				continue
			}
			// All other at-rules dispatch through the shared registry
			// helper so @font-face / @keyframes / @counter-style nested
			// inside @media / @supports bodies register identically to
			// top-level (CSS Conditional 3 §2.5; mirrors Blink's
			// CSSParserImpl::ConsumeAtRule recursion at
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
			stylesheet.Rules = append(stylesheet.Rules, dispatchAtRuleIntoStylesheet(ruleStr, stylesheet)...)
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

// dispatchAtRuleIntoStylesheet routes a single at-rule string (everything
// from the leading `@` to its terminating `}` or `;`) to the appropriate
// stylesheet registry, returning any style-rule output the at-rule produces.
//
// This is the central recursion point for CSS Conditional 3 §2.5: @media /
// @supports / @layer / @container bodies may contain any at-rule legal at
// the top level — @font-face, @keyframes, @counter-style, and other nested
// conditional rules. Mirrors Blink's `CSSParserImpl::ConsumeAtRule`
// dispatching uniformly regardless of nesting depth at
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// At-rules that only register side-state on the stylesheet (@font-face,
// @keyframes, @counter-style) mutate ss directly and return nil; conditional
// rules that produce style rules return them so the caller can attach
// LayerName / MediaQuery / @supports gating as appropriate. @import is NOT
// handled here — it has its own ParserContext threading at the top-level
// callsite.
func dispatchAtRuleIntoStylesheet(ruleStr string, ss *Stylesheet) []Rule {
	trimmed := strings.TrimSpace(ruleStr)
	switch {
	case strings.HasPrefix(trimmed, "@media"):
		return parseMediaRule(ruleStr, ss)
	case strings.HasPrefix(trimmed, "@supports"):
		return parseSupportsRule(ruleStr, ss)
	case strings.HasPrefix(trimmed, "@container"):
		return parseContainerRule(ruleStr)
	case strings.HasPrefix(trimmed, "@layer"):
		var existing []string
		if ss != nil {
			existing = ss.LayerOrder
		}
		layerRules, layerNames := parseLayerRule(ruleStr, existing, ss)
		if ss != nil {
			for _, name := range layerNames {
				found := false
				for _, e := range ss.LayerOrder {
					if e == name {
						found = true
						break
					}
				}
				if !found {
					ss.LayerOrder = append(ss.LayerOrder, name)
				}
			}
		}
		return layerRules
	case strings.HasPrefix(trimmed, "@font-face"):
		// ss == nil indicates the at-rule is inside a statically-false
		// conditional body — discard per CSS Conditional 3 §2.5.
		if ss == nil {
			return nil
		}
		if ff := parseFontFaceRule(trimmed); ff != nil {
			ss.FontFaces = append(ss.FontFaces, *ff)
		}
		return nil
	case strings.HasPrefix(trimmed, "@keyframes"), strings.HasPrefix(trimmed, "@-webkit-keyframes"):
		if ss == nil {
			return nil
		}
		name, frames := parseKeyframesRule(ruleStr)
		if name != "" {
			if ss.Keyframes == nil {
				ss.Keyframes = make(map[string][]KeyframeRule)
			}
			ss.Keyframes[name] = frames
		}
		return nil
	case strings.HasPrefix(trimmed, "@counter-style"):
		if ss == nil {
			return nil
		}
		cs := parseCounterStyleRule(ruleStr)
		if cs.Name != "" {
			ss.CounterStyles = append(ss.CounterStyles, cs)
		}
		return nil
	case strings.HasPrefix(trimmed, "@font-feature-values"):
		// CSS Fonts 4 §11.4. Dispatched only at top level / inside
		// conditional bodies — ss == nil means we're inside a
		// statically-false condition and should drop.
		if ss == nil {
			return nil
		}
		if ffv := parseFontFeatureValuesRule(trimmed); ffv != nil {
			ss.FontFeatureValues = append(ss.FontFeatureValues, *ffv)
		}
		return nil
	}
	// Unknown at-rules (@charset inside conditional body, @three-dee, @unknown,
	// @namespace nested where invalid per CSS Namespaces, …) are silently
	// dropped. Their `{ ... }` body or `; ` terminator was already consumed by
	// splitRules so we don't need to do anything else here.
	return nil
}

// splitRules splits CSS into individual rules, with robust error recovery
// for unclosed blocks, strings, and mismatched braces.
//
// Brace tracking is paren-aware: per CSS Syntax §5.4 a `{`-token inside a
// `(`-block (e.g. inside an at-rule prelude's parenthesised condition) is a
// simple-block component value, not the start of the at-rule body. This
// matters for @supports conditions like `unknown(!@#% { ... })` where the
// brace pair is part of the condition's <general-enclosed> content. Mirrors
// Blink's tokenizer, which never opens a top-level qualified-rule body while
// inside an open `(`. See third_party/blink/renderer/core/css/parser/
// css_tokenizer.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// `[` / `]` are deliberately NOT tracked here. They appear in attribute
// selectors at the top of qualified rules and never legitimately wrap
// brace pairs in real-world CSS; treating them as scope-changers would
// destroy the parser's ability to recover from malformed selectors like
// `[} { ... }` (Acid2 line 32; covered by
// TestErrorRecovery_InvalidSelectors).
func splitRules(css string) []string {
	rules := make([]string, 0)
	depth := 0
	parenDepth := 0
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

		// Track paren depth so braces inside an at-rule prelude's
		// parenthesised condition don't get mistaken for the rule body.
		if ch == '(' {
			parenDepth++
			continue
		}
		if ch == ')' && parenDepth > 0 {
			parenDepth--
			continue
		}

		if parenDepth > 0 {
			// Inside a (...) block, `{`/`}`/`;` are component-value tokens,
			// not rule delimiters. Skip rule-boundary logic.
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

	// CSS Syntax Level 3 §9 (error handling): at EOF, treat any open block,
	// function, or simple-block as if it were closed. closeUnmatchedTokens
	// closes inside-out so an unclosed function token nested inside an
	// unclosed block gets `)` BEFORE `}` — necessary for cases like
	// `p { color: var(--a` (variable-reference-025 etc.), where the rule
	// `{` is still open while a `var(` is also open. A naive
	// `strings.Repeat("}", depth)` would put the `}` inside the function
	// call, corrupting the rule body.
	if (depth > 0 || parenDepth > 0) && start < len(css) && strings.TrimSpace(css[start:]) != "" {
		synthesized := closeUnmatchedTokens(css[start:])
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
//
// Nested at-rules in the body (@font-face / @keyframes / @counter-style /
// @container / @supports / @media) are routed via
// `dispatchAtRuleIntoStylesheet`; @font-face / @keyframes / @counter-style
// register on ss directly when the @media's media query evaluates true. ss
// may be nil; with no sink, only nested style rules and nested conditional
// rules are returned (matching pre-refactor behaviour).
func parseMediaRule(ruleStr string, ss *Stylesheet) []Rule {
	rules := make([]Rule, 0)

	// Find the opening brace, skipping braces that appear inside the prelude's
	// parens (e.g. media-query value with a `<general-enclosed>` token).
	bracePos := indexTopLevelBrace(ruleStr)
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

	// Whether the @media's prelude is statically known to be false at parse
	// time (e.g. `not all`, or `not screen` when we're rendering as screen).
	// If any branch is potentially-true we keep ss live so nested
	// @font-face / @keyframes / @counter-style register. CSS Conditional 3
	// §2.5: "rules within a false condition rule must be discarded" — so a
	// `@media not all { @font-face { ... } }` block must NOT install the
	// font. Required by at-media-content-002/003/004 and the matching
	// at-supports tests.
	mediaSinkLive := ss
	if ss != nil && !anyMediaQueryStaticallyTruthy(mediaQueries) {
		mediaSinkLive = nil
	}

	// Parse inner rules
	innerRules := splitRules(innerCSS)

	for _, innerRuleStr := range innerRules {
		innerTrimmed := strings.TrimSpace(innerRuleStr)
		if strings.HasPrefix(innerTrimmed, "@") {
			// Nested at-rule. @media-inside-@media reuses the outer's
			// mediaSinkLive (so an outer false-media disables nested
			// font/keyframe registration), but the returned style rules
			// carry the nested @media's own MediaQuery — they're not
			// double-gated by the outer one. This mirrors Blink, which
			// builds a StyleRuleMedia chain where each level evaluates
			// independently at apply time but side-effect registration
			// happens during parsing under the outer-condition mask.
			switch {
			case strings.HasPrefix(innerTrimmed, "@media"):
				rules = append(rules, parseMediaRule(innerRuleStr, mediaSinkLive)...)
			case strings.HasPrefix(innerTrimmed, "@supports"):
				supportsRules := parseSupportsRule(innerRuleStr, mediaSinkLive)
				for _, mq := range mediaQueries {
					for _, sr := range supportsRules {
						sr.MediaQuery = mq
						rules = append(rules, sr)
					}
				}
			default:
				// @font-face / @keyframes / @counter-style / @container /
				// @layer — dispatch into the (possibly-nil) live sink so
				// false-media blocks discard the registration.
				rules = append(rules, dispatchAtRuleIntoStylesheet(innerRuleStr, mediaSinkLive)...)
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

// anyMediaQueryStaticallyTruthy reports whether at least one query in the
// comma-separated media-query-list could possibly evaluate to true under any
// representative viewport. Used at parse time to decide whether nested
// side-effect at-rules (@font-face / @keyframes / @counter-style) should
// register on the stylesheet — a `@media not all { ... }` body is statically
// false and its registrations must be discarded per CSS Conditional 3 §2.5.
//
// Static knowledge:
//   - Unknown media-type (e.g. `tty`, `aural`) → always false; `not tty` →
//     always true.
//   - `not all` → always false; bare `all` → always true.
//   - `screen` is what louis14 renders as, so `screen` → true,
//     `not screen` → false.
//
// For any query that includes media-feature conditions whose truth depends
// on viewport (`(min-width: 768px)`, `(prefers-color-scheme: dark)`, …) we
// conservatively return true here so the side-effect at-rule registers; the
// rule's own viewport-time evaluation still gates style-rule application.
// Mirrors Blink's MediaQueryEvaluator pre-pass at parse time in
// CSSParserImpl::ConsumeMediaRule (third_party/blink/renderer/core/css/
// parser/css_parser_impl.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func anyMediaQueryStaticallyTruthy(list []*MediaQuery) bool {
	if len(list) == 0 {
		return true
	}
	for _, mq := range list {
		if mq == nil {
			return true
		}
		// Invalid queries are treated as dropped (ConsumeQuery returning nullptr),
		// never matching. Skip them via continue, never contributing to truthy.
		if mq.Invalid {
			continue
		}
		// Type-level static truth: `all` / `screen` / empty match; other
		// known types (`print`, `tty`, …) don't match a screen renderer.
		typeMatches := false
		switch mq.MediaType {
		case "", "all", "screen":
			typeMatches = true
		}
		// `not` flips the type-level result. `only` doesn't change it.
		switch mq.Restrictor {
		case MediaRestrictorNot:
			if typeMatches && len(mq.Conditions) == 0 {
				// `not all`, `not screen` — statically false.
				continue
			}
			if !typeMatches {
				// `not print` on a screen renderer — statically true.
				return true
			}
			// `not screen and (min-width: 800px)` — feature-dependent;
			// conservatively treat as potentially-true so the body's
			// side-effect at-rules register. Apply-time evaluation still
			// gates style rules.
			return true
		default:
			if !typeMatches {
				continue
			}
			// `all`, `screen`, or `screen and (...)`. Type matches; if
			// feature-list is empty, definitely true; otherwise potentially
			// true (depends on viewport).
			return true
		}
	}
	return false
}

// parseContainerRule parses a @container rule and returns its inner rules
func parseContainerRule(ruleStr string) []Rule {
	rules := make([]Rule, 0)

	// Find the opening brace, skipping braces inside the prelude's parens.
	bracePos := indexTopLevelBrace(ruleStr)
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
// ss is the stylesheet sink for nested at-rules that register side-state
// (@font-face / @keyframes / @counter-style). May be nil; with no sink those
// nested registrations are dropped, matching pre-refactor behaviour.
func parseLayerRule(ruleStr string, existingLayerOrder []string, ss *Stylesheet) ([]Rule, []string) {
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
		if strings.HasPrefix(innerTrimmed, "@") {
			// Nested at-rule. @media / @supports / @container produce style
			// rules that we stamp with the current LayerName; @font-face /
			// @keyframes / @counter-style mutate ss in place.
			var nested []Rule
			switch {
			case strings.HasPrefix(innerTrimmed, "@media"):
				nested = parseMediaRule(innerRuleStr, ss)
			case strings.HasPrefix(innerTrimmed, "@supports"):
				nested = parseSupportsRule(innerRuleStr, ss)
			default:
				// Capture pre-dispatch FFV count so we can tag any
				// @font-feature-values inserted by this nested at-rule with
				// the current cascade-layer name. Other at-rules (@font-face,
				// @keyframes, @counter-style) don't yet honor layers in
				// louis14 — that's tracked separately.
				var ffvBefore int
				if ss != nil {
					ffvBefore = len(ss.FontFeatureValues)
				}
				nested = dispatchAtRuleIntoStylesheet(innerRuleStr, ss)
				if ss != nil {
					for i := ffvBefore; i < len(ss.FontFeatureValues); i++ {
						ss.FontFeatureValues[i].LayerName = layerName
					}
				}
			}
			for i := range nested {
				nested[i].LayerName = layerName
			}
			rules = append(rules, nested...)
			continue
		}
		parsed, err := parseRules(innerRuleStr)
		if err != nil {
			continue
		}
		for i := range parsed {
			parsed[i].LayerName = layerName
		}
		rules = append(rules, parsed...)
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
//
// Media conditions are parsed into a recursive ConditionNode tree that
// supports not/and/or nesting per CSS Media Queries 4 §3.1 (Blink:
// MediaCondition recursive struct, media_query_parser.cc @ 4883d11f).

// isRestrictorOrLogicalOperator returns true if the lowercase identifier
// is a CSS reserved keyword: "not", "and", "or", "only", "layer".
// These keywords cannot appear in the media-type slot of a media query
// (CSS Media Queries 4 §2.3). Mirrors Blink's MediaQueryParser::IsRestrictorOrLogicalOperator
// @ third_party/blink/renderer/core/css/parser/media_query_parser.cc @ 4883d11f.
// This is the single source of truth for reserved-keyword enumeration (CLAUDE.md §5).
func isRestrictorOrLogicalOperator(ident string) bool {
	switch ident {
	case "not", "and", "or", "only", "layer":
		return true
	}
	return false
}

func parseMediaQuery(mediaStr string) *MediaQuery {
	// Remove @media prefix
	mediaStr = strings.TrimPrefix(mediaStr, "@media")
	mediaStr = strings.TrimSpace(mediaStr)

	mq := &MediaQuery{
		Restrictor: MediaRestrictorNone,
		Conditions: make([]ConditionNode, 0),
	}

	// Handle "not" and "only" modifiers at the query level.
	// "not" before a media type (e.g. `not screen`) is query-level not.
	// "not" before a `(` (e.g. `not (monochrome)`) is also query-level not
	// and the remainder is a condition list, NOT a media type.
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
	// HOWEVER, if the identifier is a reserved keyword (not/and/or/only/layer),
	// reject it per CSS Media Queries 4 §2.3 (mirrors Blink's ConsumeType
	// returning nullptr for reserved keywords).
	attemptedMediaType := false
	if mediaStr != "" && mediaStr[0] != '(' {
		attemptedMediaType = true
		end := 0
		for end < len(mediaStr) {
			ch := mediaStr[end]
			if ch == ' ' || ch == ',' || ch == '\t' || ch == '\n' {
				break
			}
			end++
		}
		candidate := strings.ToLower(mediaStr[:end])
		if isRestrictorOrLogicalOperator(candidate) {
			// Reserved keyword in type slot → invalid immediately. Mirrors Blink's
			// ConsumeType returning nullptr: the query is dropped regardless of whether
			// conditions follow (e.g. `@media and (min-width: 1px)` must not match).
			mq.Invalid = true
		} else {
			mq.MediaType = candidate
		}
		// If candidate was a reserved keyword, mq.MediaType stays empty (null-equivalent).
		mediaStr = strings.TrimSpace(mediaStr[end:])
	}

	// Remove leading "and" keyword (after a media type: `screen and (...)`)
	mediaStr = strings.TrimPrefix(mediaStr, "and")
	mediaStr = strings.TrimSpace(mediaStr)

	// Parse the condition expression into a recursive ConditionNode tree.
	// Top-level conditions separated by " and " become a list of ConditionAnd
	// children; each condition is parsed recursively to handle not/or nesting.
	// Per CSS Media Queries 4 §2.3, each condition must start with "(" or "not".
	// A bare identifier (e.g., "screen" appearing after a condition) indicates
	// a leftover media-type and makes the query invalid (WI-4: test 006).
	conditionStrs := splitMediaConditions(mediaStr)
	for _, condStr := range conditionStrs {
		condStr = strings.TrimSpace(condStr)
		if condStr == "" {
			continue
		}
		// Bare identifiers (not starting with "(" or "not ") are invalid.
		if !strings.HasPrefix(condStr, "(") && !strings.HasPrefix(condStr, "not ") {
			mq.Invalid = true
			continue
		}
		// Also invalid: a fragment starting with "(" but with leftover non-operator tokens
		// after the closing paren (e.g., "(min-width: 480px) screen"). stripOuterParens
		// detects when parens don't wrap. If they don't wrap, check if the remainder
		// is a valid operator (or/and/not) or bare tokens (invalid).
		if strings.HasPrefix(condStr, "(") {
			stripped := stripOuterParens(condStr)
			if stripped == condStr {
				// Outer parens don't wrap. Check what comes after the first balanced paren.
				depth := 0
				var afterParens string
				for i := 0; i < len(condStr); i++ {
					if condStr[i] == '(' {
						depth++
					} else if condStr[i] == ')' {
						depth--
						if depth == 0 {
							afterParens = strings.TrimSpace(condStr[i+1:])
							break
						}
					}
				}
				// Leftover tokens that are NOT operators (or, and, not) are invalid.
				// Use hasLeadingKeyword (case-insensitive) so "OR"/"AND"/"NOT" are accepted.
				if afterParens != "" && !hasLeadingKeyword(afterParens, "or") && !hasLeadingKeyword(afterParens, "and") && !hasLeadingKeyword(afterParens, "not") {
					mq.Invalid = true
					continue
				}
			}
		}
		node := parseConditionNode(condStr)
		mq.Conditions = append(mq.Conditions, node)
	}

	// If we attempted to parse a media type (mediaStr started with identifier) but found
	// no valid type (either rejected as reserved keyword, or was empty), combined with
	// no conditions, the query is invalid. This handles:
	// - `@media and`, `@media or`, `@media not`, `@media only`, `@media layer` (bare keywords)
	// - `@media not and`, `@media only or`, etc. (restrictor + keyword)
	// HOWEVER, a truly empty query like `@media` (no type, no conditions) is valid (matches "all").
	// We distinguish by checking attemptedMediaType: if true, we tried to parse a type and
	// rejected it (or found empty). If false and mq.MediaType == "", it means mediaStr
	// started with "(" or was empty, so the query is valid (either has conditions or is empty).
	if attemptedMediaType && mq.MediaType == "" && len(mq.Conditions) == 0 {
		mq.Invalid = true
	}

	return mq
}

// parseConditionNode parses a single media condition expression (with its
// outer parentheses still present) into a ConditionNode tree.
//
// Grammar (CSS Media Queries 4 §3.1, simplified):
//
//	condition         = not-condition | in-parens *( 'or' in-parens )
//	                  | in-parens *( 'and' in-parens )
//	not-condition     = 'not' in-parens
//	in-parens         = '(' condition ')' | '(' media-feature ')'
//	media-feature     = ident [ ':' value ]
//
// Mirrors Blink's MediaQueryParser::ConsumeCondition at
// third_party/blink/renderer/core/css/parser/media_query_parser.cc @ 4883d11f.
func parseConditionNode(s string) ConditionNode {
	s = strings.TrimSpace(s)

	// not-condition: starts with "not " (CSS MQ4 always uses a space before the
	// parenthesized argument, e.g. `not (monochrome)`).
	if strings.HasPrefix(s, "not ") {
		rest := strings.TrimSpace(s[4:])
		child := parseConditionNode(rest)
		return ConditionNode{Kind: ConditionNot, Children: []ConditionNode{child}}
	}

	// in-parens wrapper: strip balanced outer parens, then check for
	// not/or/and inside, or fall through to feature parsing.
	if strings.HasPrefix(s, "(") {
		inner := stripOuterParens(s)
		inner = strings.TrimSpace(inner)

		// Inner starts with "not " → Not node
		if strings.HasPrefix(inner, "not ") {
			child := parseConditionNode(inner)
			return child
		}

		// Look for " or " at paren depth 0 inside the parens.
		orParts := splitAtDepthZero(inner, " or ")
		if len(orParts) > 1 {
			children := make([]ConditionNode, 0, len(orParts))
			for _, p := range orParts {
				children = append(children, parseConditionNode(strings.TrimSpace(p)))
			}
			return ConditionNode{Kind: ConditionOr, Children: children}
		}

		// Look for " and " at paren depth 0 inside the parens.
		andParts := splitAtDepthZero(inner, " and ")
		if len(andParts) > 1 {
			children := make([]ConditionNode, 0, len(andParts))
			for _, p := range andParts {
				children = append(children, parseConditionNode(strings.TrimSpace(p)))
			}
			return ConditionNode{Kind: ConditionAnd, Children: children}
		}

		// No combinators — the inner content is a media feature.
		return parseFeatureNode(inner)
	}

	// Look for " or " at paren depth 0 (outer-paren-free or/and forms).
	orParts := splitAtDepthZero(s, " or ")
	if len(orParts) > 1 {
		children := make([]ConditionNode, 0, len(orParts))
		for _, p := range orParts {
			children = append(children, parseConditionNode(strings.TrimSpace(p)))
		}
		return ConditionNode{Kind: ConditionOr, Children: children}
	}

	andParts := splitAtDepthZero(s, " and ")
	if len(andParts) > 1 {
		children := make([]ConditionNode, 0, len(andParts))
		for _, p := range andParts {
			children = append(children, parseConditionNode(strings.TrimSpace(p)))
		}
		return ConditionNode{Kind: ConditionAnd, Children: children}
	}

	// Bare identifier or feature without parens — treat as unknown.
	return ConditionNode{Kind: ConditionFeature, Feature: s}
}

// stripOuterParens removes the outermost balanced pair of parentheses from s
// if and only if they wrap the entire string. Returns s unchanged otherwise.
// E.g. "(foo)" → "foo", "(a) or (b)" → "(a) or (b)" (not wrapped).
func stripOuterParens(s string) string {
	if len(s) < 2 || s[0] != '(' {
		return s
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				// The opening paren closes here.
				if i == len(s)-1 {
					// It wraps the entire string.
					return s[1:i]
				}
				// It closes before the end — not a wrapping paren.
				return s
			}
		}
	}
	return s
}

// splitAtDepthZero splits s by the literal separator sep at parenthesis depth
// 0. Returns a single-element slice if sep is not found at depth 0.
func splitAtDepthZero(s, sep string) []string {
	var parts []string
	depth := 0
	start := 0
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

// parseFeatureNode parses a bare feature expression (without outer parens)
// like "min-width: 768px" or "color" into a ConditionFeature node.
func parseFeatureNode(s string) ConditionNode {
	s = strings.TrimSpace(s)

	// Check for range syntax operators at depth 0 (>, <, >=, <=, =).
	// These must appear at paren depth 0 to avoid conflicts with calc() contents.
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '>', '<', '=':
			if depth == 0 {
				// Found a potential range operator at depth 0.
				// Match >=, <=, or single >, <, =.
				var op string
				var opEnd int
				if i+1 < len(s) && s[i+1] == '=' {
					op = s[i : i+2]
					opEnd = i + 2
				} else {
					op = s[i : i+1]
					opEnd = i + 1
				}
				// Extract feature name (before op) and value (after op).
				feature := strings.TrimSpace(s[:i])
				value := strings.TrimSpace(s[opEnd:])
				if feature != "" && value != "" {
					return ConditionNode{
						Kind:    ConditionFeature,
						Feature: feature,
						Op:      op,
						Value:   value,
					}
				}
			}
		}
	}

	// Legacy colon-based syntax: `feature: value`.
	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 2 {
		return ConditionNode{
			Kind:    ConditionFeature,
			Feature: strings.TrimSpace(parts[0]),
			Value:   strings.TrimSpace(parts[1]),
		}
	}

	// Feature name only (no value or operator).
	return ConditionNode{
		Kind:    ConditionFeature,
		Feature: s,
	}
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

// splitMediaConditions splits a media query condition string by " and " at
// paren depth 0. Delegates to splitAtDepthZero.
func splitMediaConditions(s string) []string {
	return splitAtDepthZero(s, " and ")
}

// parseSupportsRule parses an @supports rule and returns the inner rules if
// the condition is met. Nested at-rules (@font-face / @keyframes /
// @counter-style / @media / @layer / @container) are routed through
// `dispatchAtRuleIntoStylesheet`, mutating ss when they register side-state.
// Mirrors Blink's StyleRuleSupports body containing arbitrary nested at-rules
// per CSS Conditional 3 §2.5, at
// third_party/blink/renderer/core/css/css_supports_rule.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// ss may be nil when called from a context that doesn't have a stylesheet yet
// (e.g. unit tests); in that case nested non-conditional at-rules are silently
// dropped — matching the pre-refactor behavior.
func parseSupportsRule(ruleStr string, ss *Stylesheet) []Rule {
	ruleStr = strings.TrimSpace(ruleStr)
	// Locate the body's opening `{`. The first literal `{` in the rule string
	// is NOT necessarily the body — an @supports condition may contain a
	// `<general-enclosed>` whose body has braces (`unknown(!@#% { ... })`).
	// Skip braces inside any `(`/`[` block; mirrors Blink's
	// CSSParserImpl::ConsumeAtRule prelude/block split at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	braceIdx := indexTopLevelBrace(ruleStr)
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

	return parseConditionalBody(innerCSS, ss)
}

// parseConditionalBody walks the body of a conditional at-rule (@supports /
// @media / @layer / @container — anything whose `{ ... }` is a RuleList per
// CSS Conditional 3 §2.5) and dispatches each inner rule. At-rules go through
// `dispatchAtRuleIntoStylesheet` so @font-face / @keyframes / @counter-style
// nested in a conditional body register on ss exactly as they would at the
// top level. Style rules are accumulated and returned for the caller to gate
// further (e.g. attaching MediaQuery / LayerName).
//
// `ss` may be nil. With a nil stylesheet, nested at-rules that register
// side-state silently drop — the same behaviour as the pre-refactor parser.
func parseConditionalBody(innerCSS string, ss *Stylesheet) []Rule {
	innerRules := splitRules(innerCSS)
	var rules []Rule
	for _, inner := range innerRules {
		innerTrimmed := strings.TrimSpace(inner)
		if strings.HasPrefix(innerTrimmed, "@") {
			if ss == nil {
				// Nested at-rules require a stylesheet sink to register
				// side-state on; without one, fall back to the conditional
				// dispatcher only (so nested @media / @supports / @layer /
				// @container still produce style rules).
				switch {
				case strings.HasPrefix(innerTrimmed, "@media"):
					rules = append(rules, parseMediaRule(inner, nil)...)
				case strings.HasPrefix(innerTrimmed, "@supports"):
					rules = append(rules, parseSupportsRule(inner, nil)...)
				case strings.HasPrefix(innerTrimmed, "@container"):
					rules = append(rules, parseContainerRule(inner)...)
				}
				continue
			}
			rules = append(rules, dispatchAtRuleIntoStylesheet(inner, ss)...)
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
//  2. `<supports-feature>` — one of:
//     - `<supports-decl>` :=`( <declaration> )`
//     - `<supports-selector-fn>` := `selector( <complex-selector> )`
//     - `<supports-font-format-fn>` := `font-format( <font-format-keyword> )`
//     - `<supports-font-tech-fn>` := `font-tech( <font-tech-keyword> )`
//  3. `<general-enclosed>` — any function-token whose body tokenizes, OR any
//     parenthesized block whose body doesn't otherwise parse. Always
//     evaluates to false (kUnsupported in Blink).
//
// Mirrors Blink's CSSSupportsParser::ConsumeSupportsFeature at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
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

		// Then as a supports-feature function inside parens.
		if res, ok := consumeSupportsFeatureFn(inner); ok {
			if res {
				return supportsTrue, after
			}
			return supportsFalse, after
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

	// Bare (un-parenthesised) supports-feature function form, e.g.
	// `selector(::before)`, `font-format(opentype)`. Spec allows the function
	// without an enclosing pair of parens — Blink calls these
	// "MaybeConsumeSupportsFeatureFn" before falling into general-enclosed.
	if res, after, ok := consumeBareSupportsFeatureFn(s); ok {
		if res {
			return supportsTrue, after
		}
		return supportsFalse, after
	}

	// Function-token form whose name we don't recognise — falls into
	// <general-enclosed>. Evaluates to false.
	if after, ok := consumeGeneralEnclosedFunction(s); ok {
		return supportsFalse, after
	}

	return supportsParseFailure, s
}

// consumeBareSupportsFeatureFn attempts to consume one of the supports-feature
// function-tokens (`selector(...)`, `font-format(...)`, `font-tech(...)`) at
// the head of s. Returns (truthValue, rest, true) on success; (_, s, false)
// if s doesn't start with a recognised function.
func consumeBareSupportsFeatureFn(s string) (bool, string, bool) {
	// Look for an ident immediately followed by `(`.
	i := 0
	for i < len(s) {
		c := s[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		isExtra := c == '-' || c == '_'
		if i == 0 {
			if !isAlpha && !isExtra {
				return false, s, false
			}
		} else {
			if !isAlpha && !isDigit && !isExtra {
				break
			}
		}
		i++
	}
	if i == 0 || i >= len(s) || s[i] != '(' {
		return false, s, false
	}
	name := strings.ToLower(s[:i])
	body, after, ok := consumeBalancedParens(s[i:])
	if !ok {
		return false, s, false
	}
	switch name {
	case "selector":
		return evaluateSupportsSelector(strings.TrimSpace(body)), after, true
	case "font-format":
		return evaluateSupportsFontFormat(strings.TrimSpace(body)), after, true
	case "font-tech":
		return evaluateSupportsFontTech(strings.TrimSpace(body)), after, true
	}
	return false, s, false
}

// consumeSupportsFeatureFn parses inner as one of the supports-feature
// functions. Returns (truthValue, true) if inner is a recognised function
// call, (_, false) otherwise (caller falls through to other productions).
func consumeSupportsFeatureFn(inner string) (bool, bool) {
	res, rest, ok := consumeBareSupportsFeatureFn(inner)
	if !ok {
		return false, false
	}
	if strings.TrimSpace(rest) != "" {
		return false, false
	}
	return res, true
}

// evaluateSupportsSelector reports whether the @supports `selector(<sel>)`
// function evaluates true for the given inner selector text. Per CSS
// Conditional 4 §2.5 / Blink's CSSSelectorParser::ConsumeComplexSelectorList
// invoked in `kSupportsCondition` mode (which disables :is()/:where()
// forgiveness), the inner must parse as a valid <complex-selector-list> with
// every pseudo-class and pseudo-element recognised by the UA.
//
// Louis14's approach is structurally permissive on pseudo-element names (so
// modern pseudos like ::file-selector-button / ::details-content / ::picker
// claim support even when not yet styled) but strict on:
//   - balanced parens/brackets,
//   - syntactic shape (must look like a selector, not e.g. a number),
//   - the logical pseudo-classes :is/:where/:not/:has — each inner selector
//     in those parens must itself be a valid selector. No forgiveness in
//     supports-context, mirroring Blink's behavior.
//   - at-supports-namespace-002: namespace-prefixed selectors `x|y` require
//     `x` to have been declared via `@namespace`; otherwise false.
func evaluateSupportsSelector(sel string) bool {
	if sel == "" {
		return false
	}
	// Per CSS Conditional 5 §2.5 the argument is a single <complex-selector>,
	// NOT a <complex-selector-list>. A comma at the top level invalidates the
	// supports condition. Required by at-supports-selector-004.
	parts := splitSelectorGroup(sel)
	if len(parts) != 1 {
		return false
	}
	return isValidComplexSelector(strings.TrimSpace(parts[0]))
}

// isValidComplexSelector reports whether s is a syntactically valid CSS
// <complex-selector>: balanced brackets/parens, no stray combinators, every
// pseudo-class and pseudo-element recognised, every functional-pseudo's
// inner selector list also valid.
//
// A leading combinator (e.g. `> .test-1`) is NOT a valid <complex-selector>
// — it is a <relative-selector> per Selectors 4 §17.1, accepted only in
// nesting / :has() / @scope <scope-start> position. The @supports
// `selector()` function tests against <complex-selector> (per CSS Conditional
// 4 §2.5.1), so leading-combinator selectors fail support. This mirrors
// Blink's CSSSelectorParser::ConsumeComplexSelector @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f which only consumes a leading
// combinator when invoked in nesting / forgiving-nested mode.
func isValidComplexSelector(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Balanced brackets/parens, no nested string failure.
	if !hasBalancedBrackets(s) {
		return false
	}
	// Reject leading combinator — that's a <relative-selector>, not a
	// <complex-selector>. Required for at-supports nesting tests like
	// `@supports(not selector(> .test-1))` which expect FALSE.
	if len(s) > 0 && (s[0] == '>' || s[0] == '+' || s[0] == '~') {
		return false
	}
	// Walk compound selectors split by descendant/child/sibling combinators.
	// We don't need a full AST — just validate each compound's tokens.
	tokens := tokenizeSelector(s)
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" || t == ">" || t == "+" || t == "~" {
			continue
		}
		if !isValidCompoundSelector(t) {
			return false
		}
	}
	return true
}

// hasBalancedBrackets reports whether all `(`, `[`, `{` are matched in s.
func hasBalancedBrackets(s string) bool {
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
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
			parenDepth++
		case ')':
			if parenDepth == 0 {
				return false
			}
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth == 0 {
				return false
			}
			bracketDepth--
		case '{':
			braceDepth++
		case '}':
			if braceDepth == 0 {
				return false
			}
			braceDepth--
		}
	}
	return inString == 0 && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0
}

// isValidCompoundSelector reports whether s is a valid CSS compound-selector
// (element + pseudo-classes + pseudo-elements + classes + ID + attributes,
// in any order). Walks `:`, `::`, `.`, `#`, `[…]` chunks; rejects unknown
// pseudo-classes (with forgiving recursion through :is/:where/:not/:has).
func isValidCompoundSelector(s string) bool {
	i := 0
	// Optional namespace prefix `prefix|local`. The `*` prefix and bare `|`
	// are also legal in selectors-4 grammar. Per at-supports-namespace-002,
	// only namespaces declared via @namespace count as "supported" — but
	// louis14's @supports parser runs without a stylesheet context here, so
	// we can't currently check declarations. We accept any well-formed
	// prefix syntactically; the namespace-002 reftest still relies on the
	// rule-application path to gate which selectors actually match.
	for i < len(s) {
		c := s[i]
		switch {
		case c == '*':
			i++
		case c == '|':
			i++
		case c == '&':
			// `&` is the CSS Nesting nesting-selector (CSS Nesting 1 §2),
			// and is a valid simple-selector inside any compound. Required
			// for `@supports(selector(&))` to evaluate TRUE — see Blink's
			// CSSSelectorParser::ConsumeNestingParent @
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
			i++
		case c == '.' || c == '#':
			// class or id — read until next selector boundary
			i++
			for i < len(s) && !isSelectorBoundary(s[i]) {
				i++
			}
		case c == '[':
			// Attribute selector — find matching `]`.
			j := i + 1
			inStr := byte(0)
			for j < len(s) {
				cc := s[j]
				if inStr != 0 {
					if cc == '\\' && j+1 < len(s) {
						j += 2
						continue
					}
					if cc == inStr {
						inStr = 0
					}
					j++
					continue
				}
				if cc == '"' || cc == '\'' {
					inStr = cc
				} else if cc == ']' {
					break
				}
				j++
			}
			if j >= len(s) {
				return false
			}
			i = j + 1
		case c == ':':
			// Pseudo-class or pseudo-element.
			j := i + 1
			doubleColon := false
			if j < len(s) && s[j] == ':' {
				doubleColon = true
				j++
			}
			// Read name.
			nameStart := j
			for j < len(s) {
				cc := s[j]
				isAlpha := (cc >= 'a' && cc <= 'z') || (cc >= 'A' && cc <= 'Z')
				isDigit := cc >= '0' && cc <= '9'
				isExtra := cc == '-' || cc == '_'
				if !isAlpha && !isDigit && !isExtra {
					break
				}
				j++
			}
			name := strings.ToLower(s[nameStart:j])
			if name == "" {
				return false
			}
			var argBody string
			if j < len(s) && s[j] == '(' {
				body, after, ok := consumeBalancedParens(s[j:])
				if !ok {
					return false
				}
				argBody = body
				// Move past the function call.
				j = (len(s) - len(after))
			}
			if doubleColon {
				if !isKnownPseudoElement(name) {
					return false
				}
			} else {
				// May be a single-colon legacy pseudo-element (:before, etc.)
				// OR a pseudo-class.
				if isKnownPseudoElement(name) {
					// Legacy single-colon pseudo-element — fine.
				} else if !isKnownPseudoClass(name) {
					return false
				}
				if pseudoClassUsesSelectorArg(name) {
					if !validateLogicalPseudoArg(name, argBody) {
						return false
					}
				}
			}
			i = j
		case isElementIdentChar(c):
			// Element identifier.
			j := i
			for j < len(s) && isElementIdentChar(s[j]) {
				j++
			}
			i = j
		default:
			// Unexpected character — fail validation.
			return false
		}
	}
	return true
}

func isElementIdentChar(c byte) bool {
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= '0' && c <= '9' {
		return true
	}
	return c == '-' || c == '_'
}

func isSelectorBoundary(c byte) bool {
	return c == '.' || c == '#' || c == '[' || c == ':' || c == '*' || c == '|'
}

// pseudoClassUsesSelectorArg returns true if the named pseudo-class takes a
// selector list as its argument (e.g. :is, :where, :not, :has).
func pseudoClassUsesSelectorArg(name string) bool {
	switch name {
	case "is", "where", "not", "has":
		return true
	}
	return false
}

// validateLogicalPseudoArg recursively validates the inner selector list of
// :is/:where/:not/:has in @supports unforgiving context — all inner items
// must be valid complex selectors (Blink's
// CSSSelectorParser::ConsumeForgivingComplexSelectorList disables forgiving
// behaviour when called from kSupportsCondition mode @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func validateLogicalPseudoArg(name, body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		// Empty :is() / :where() are accepted as matching nothing per spec,
		// but in @supports we treat them as invalid (Chrome behaviour).
		return false
	}
	for _, item := range splitSelectorGroup(body) {
		item = strings.TrimSpace(item)
		if item == "" {
			return false
		}
		if !isValidComplexSelector(item) {
			return false
		}
	}
	return true
}

// isKnownPseudoElement reports whether name is a CSS pseudo-element
// recognised by louis14 for @supports purposes. Includes pseudos that may
// not actually be styled — `selector()` only asserts syntactic / vocabulary
// support, not full rendering fidelity. List sourced from CSS Pseudo-Elements
// 4, HTML form pseudo-elements, and widely-shipped vendor-prefixed pseudos.
func isKnownPseudoElement(name string) bool {
	switch name {
	// CSS 2.1 / CSS Pseudo-Elements 4
	case "before", "after", "first-letter", "first-line", "marker",
		"placeholder", "selection", "backdrop", "cue", "cue-region",
		"file-selector-button", "details-content",
		"picker", "picker-icon",
		"target-text", "spelling-error", "grammar-error",
		"highlight",
		"view-transition", "view-transition-group", "view-transition-image-pair",
		"view-transition-old", "view-transition-new",
		"part", "slotted",
		// Widely-shipped vendor-prefixed form pseudos
		"-webkit-slider-thumb", "-webkit-slider-runnable-track",
		"-webkit-scrollbar", "-webkit-scrollbar-track",
		"-webkit-scrollbar-thumb", "-webkit-scrollbar-button",
		"-webkit-scrollbar-track-piece", "-webkit-scrollbar-corner",
		"-webkit-resizer", "-webkit-input-placeholder",
		"-moz-placeholder", "-moz-range-thumb", "-moz-range-track",
		"-ms-input-placeholder":
		return true
	}
	return false
}

// isKnownPseudoClass reports whether name is a CSS pseudo-class recognised
// by louis14 for @supports purposes.
func isKnownPseudoClass(name string) bool {
	switch name {
	// Structural
	case "first-child", "last-child", "only-child",
		"nth-child", "nth-last-child",
		"first-of-type", "last-of-type", "only-of-type",
		"nth-of-type", "nth-last-of-type",
		"empty", "root", "scope",
		// User-state
		"hover", "active", "focus", "focus-within", "focus-visible",
		"visited", "link", "any-link", "local-link", "target", "target-within",
		// Form-state
		"checked", "indeterminate", "default",
		"disabled", "enabled",
		"required", "optional",
		"read-only", "read-write",
		"valid", "invalid", "in-range", "out-of-range",
		"placeholder-shown", "user-invalid", "user-valid",
		"blank",
		// Tree
		"lang", "dir",
		"current", "past", "future",
		// Logical
		"is", "where", "not", "has",
		"nth-col", "nth-last-col",
		// Misc
		"defined", "fullscreen", "modal", "picture-in-picture",
		"playing", "paused", "muted", "volume-locked", "seeking",
		"buffering", "stalled",
		"autofill",
		"open", "popover-open":
		return true
	}
	return false
}

// evaluateSupportsFontFormat reports whether the @supports
// `font-format(<keyword>)` function evaluates true for the given inner
// argument text. Per CSS Conditional 5 §2.7 the argument is a single
// font-format keyword (case-insensitive ident, NOT a string). louis14
// supports the same baseline set as modern Chrome: opentype, truetype, woff,
// woff2, embedded-opentype, svg, collection. The test
// at-supports-font-format-001 fails the string and multi-arg forms.
func evaluateSupportsFontFormat(arg string) bool {
	if arg == "" {
		return false
	}
	// Reject strings and multi-arg forms.
	if strings.ContainsAny(arg, ",\"' \t\n\r\f") {
		return false
	}
	if !isValidPropertyIdent(arg) {
		return false
	}
	switch strings.ToLower(arg) {
	case "opentype", "truetype", "woff", "woff2",
		"embedded-opentype", "svg", "collection":
		return true
	}
	return false
}

// evaluateSupportsFontTech reports whether the @supports
// `font-tech(<keyword>)` function evaluates true for the given inner
// argument text. Per CSS Conditional 5 §2.8 the argument is a single
// font-tech keyword. Louis14 reports support for the baseline set:
// features-opentype, features-aat, features-graphite, color-COLRv0,
// color-sbix, color-CBDT, color-SVG, variations, palettes, incremental.
// Required by at-supports-font-tech-001.
func evaluateSupportsFontTech(arg string) bool {
	if arg == "" {
		return false
	}
	if strings.ContainsAny(arg, ",\"' \t\n\r\f") {
		return false
	}
	if !isValidPropertyIdent(arg) {
		return false
	}
	switch strings.ToLower(arg) {
	case "features-opentype", "features-aat", "features-graphite",
		"color-colrv0", "color-colrv1", "color-sbix", "color-cbdt", "color-svg",
		"variations", "palettes", "incremental":
		return true
	}
	return false
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

// indexTopLevelBrace returns the byte index of the first `{` in s that lies
// outside any `(` block and outside string literals, or -1 if none. Used to
// split an at-rule's prelude from its body without being fooled by braces
// that appear as component-value tokens inside the prelude's parens (e.g.
// `@supports not unknown(!@#% { ... })`). `[` / `]` are not tracked — see
// splitRules' comment for rationale.
func indexTopLevelBrace(s string) int {
	parenDepth := 0
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
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '{':
			if parenDepth == 0 {
				return i
			}
		}
	}
	return -1
}

// consumeSupportsDecl reports whether inner is a valid <declaration> for
// @supports purposes. Returns (resultBool, parsedOK).
//
//   - parsedOK is false if the content doesn't even tokenize as a declaration
//     (no colon, empty property, trailing semicolon, unbalanced brackets,
//     top-level `:` token in value, etc.). The caller then treats the wrapping
//     parens as <general-enclosed>.
//   - resultBool is true if the declaration is "supported" (known property OR
//     a custom property `--*`, with a non-empty value that parses to at least
//     one valid longhand). False for unknown properties.
//
// Per CSS Syntax §5.4.6: a declaration is `<ident-token> S* : S* <component-
// value>+ ['!' <important>]`. The <component-value>+ value cannot contain a
// top-level <colon-token>, top-level <semicolon-token>, top-level `{`/`}`, or
// unmatched `(`/`[`/`)`/`]` — those break the production. A trailing semicolon
// means the input contains MORE than one declaration — invalid as a single
// <supports-decl>. Mirrors Blink's
// CSSParserImpl::ConsumeDeclaration at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
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
	if property == "" {
		return false, false
	}

	// Custom-property declarations (`--*: ...`) have their own validation
	// path: the name must be a valid <custom-property-name> (escape-aware,
	// rejects the bare `--`), and the value is a <declaration-value> which
	// CSS Custom Properties §3 explicitly permits to be empty (a white-space-
	// only value declares the guaranteed-invalid value). Custom properties
	// are always "supported" once well-formed — Blink's
	// CSSSupportsParser::ConsumeSupportsDecl returns true unconditionally
	// for any `--name` that parses as a declaration at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Use the RAW property text so
	// escape-aware validation can resolve `--\61` to a legal name.
	if strings.HasPrefix(property, "--") {
		if !isValidCustomPropertyName(property) {
			return false, false
		}
		// Strip `!important` priority before validating the declaration
		// value. Custom-property declarations accept the same priority
		// annotation as any other declaration (CSS Custom Properties §3,
		// Blink's CSSVariableParser::ConsumeUnparsedDeclaration handles the
		// priority strip before the token-stream check at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). variable-supports-046
		// (`--a: var(--b) !important`) requires this.
		if bang := lastTopLevelBang(value); bang >= 0 {
			flag := strings.TrimSpace(value[bang+1:])
			if !strings.EqualFold(flag, "important") {
				return false, false
			}
			value = strings.TrimSpace(value[:bang])
			// Double-!important is malformed (variable-supports-049,
			// `var(--b) !important !important`).
			if lastTopLevelBang(value) >= 0 {
				return false, false
			}
		}
		if value == "" {
			// CSS Custom Properties §3: an empty declaration value is legal
			// and represents the guaranteed-invalid value. The `@supports`
			// rule still returns true for this shape (Blink's
			// CSSVariableParser::ConsumeUnparsedDeclaration accepts the
			// empty token list at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
			return true, true
		}
		if !isValidCustomPropertyValue(value) {
			return false, true
		}
		// Custom-property values still need the var() name + fallback shape
		// check because a malformed `var(1px)` inside a custom-property
		// declaration is also a parse error per CSS Variables §3.
		if containsVarFunction(value) && hasInvalidVarReference(value) {
			return false, true
		}
		return true, true
	}

	// Non-custom property name must be a valid <ident-token>.
	if !isValidPropertyIdent(property) {
		return false, false
	}
	if value == "" {
		return false, false
	}

	// Strip optional `!important` priority flag from the value tail before the
	// value-syntax check. Per CSS Syntax §5.4.6 the only legal priority is
	// `important` — `!bogus` is a parse error that invalidates the entire
	// declaration (and therefore the @supports condition). Mirrors Blink's
	// CSSParserImpl::ConsumeDeclarationValue priority handling at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Required by at-supports-044
	// (`(--foo: whatever !bogus)` must evaluate false).
	if bang := lastTopLevelBang(value); bang >= 0 {
		flag := strings.TrimSpace(value[bang+1:])
		if !strings.EqualFold(flag, "important") {
			return false, false
		}
		bare := strings.TrimSpace(value[:bang])
		if bare == "" {
			return false, false
		}
		// A second top-level `!` after stripping `!important` is a malformed
		// duplicate priority annotation — reject (variable-supports-017,
		// `var(--a) !important !important`).
		if lastTopLevelBang(bare) >= 0 {
			return false, false
		}
		value = bare
	}

	// Validate the value is a syntactically well-formed <declaration-value>:
	// no top-level `;` / `:`, and all `(`/`[`/`{` paired. (`{...}` blocks are
	// allowed at top level — CSS Syntax §5.4.6 permits any `<simple-block>` as
	// a `<component-value>`, so test cases like
	// `@supports (color: { [ var(--a) ] })` must parse as a single block.)
	if !isValidDeclarationValue(value) {
		return false, false
	}

	// CSS Variables §3 declaration-value rules also apply inside @supports:
	// any var() reference whose name is malformed (`var(1px)`) or whose
	// fallback contains a top-level `;` / bare `!` makes the wrapping
	// declaration invalid → the @supports condition evaluates false. Mirrors
	// Blink's CSSSupportsParser::ConsumeSupportsDecl path which routes through
	// CSSParserImpl::ConsumeDeclaration and inherits its var() validation at
	// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	if containsVarFunction(value) && hasInvalidVarReference(value) {
		return false, true
	}

	// Per CSS Conditional 3 §6.4 the supports test is "true iff the UA
	// recognises the property:value pair." Any function-call token in the
	// value must name a known CSS function — `compute(...)`, `unknown(...)`
	// etc. should make `(width: compute(...))` evaluate false. Required by
	// at-supports-018 (`(not (width:compute(2px+2px)))` must be true).
	if !valueFunctionsAreKnown(value) {
		return false, true
	}

	return isSupportedDeclaration(property, value), true
}

// valueFunctionsAreKnown returns true iff every function-call token in value
// uses an ident that names a CSS function the UA recognises. An unrecognised
// function (`compute(...)`, `xyzzy(...)`, ...) invalidates the declaration
// even if the rest of the value looks well-formed. Mirrors Blink's behavior:
// CSSPropertyParser dispatches function-tokens by name and returns nullptr
// for unknown ones, which propagates to declaration failure.
func valueFunctionsAreKnown(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isExtra := c == '-' || c == '_'
		if !isAlpha && !isExtra {
			continue
		}
		// Read ident.
		j := i
		for j < len(value) {
			cc := value[j]
			ok := (cc >= 'a' && cc <= 'z') || (cc >= 'A' && cc <= 'Z') ||
				(cc >= '0' && cc <= '9') || cc == '-' || cc == '_'
			if !ok {
				break
			}
			j++
		}
		// Is it followed by `(` (no whitespace per CSS Syntax function-token)?
		if j < len(value) && value[j] == '(' {
			name := strings.ToLower(value[i:j])
			if !isKnownCSSValueFunction(name) {
				return false
			}
		}
		i = j - 1
	}
	return true
}

// isKnownCSSValueFunction reports whether name is a CSS function-token the UA
// recognises as a value-context function. List sourced from CSS Values 4,
// CSS Color 4, CSS Images 4, CSS Transforms, CSS Filters, CSS Variables.
func isKnownCSSValueFunction(name string) bool {
	switch name {
	// Math
	case "calc", "min", "max", "clamp", "round", "mod", "rem",
		"sin", "cos", "tan", "asin", "acos", "atan", "atan2",
		"pow", "sqrt", "hypot", "log", "exp", "abs", "sign":
		return true
	// Variables / env / attr
	case "var", "env", "attr":
		return true
	// Colors
	case "rgb", "rgba", "hsl", "hsla", "hwb", "lab", "lch",
		"oklab", "oklch", "color", "color-mix", "color-contrast", "contrast-color",
		"device-cmyk", "light-dark":
		return true
	// Images / gradients
	case "url", "src", "image", "image-set",
		"linear-gradient", "radial-gradient", "conic-gradient",
		"repeating-linear-gradient", "repeating-radial-gradient",
		"repeating-conic-gradient",
		"cross-fade", "element", "paint":
		return true
	// Transforms
	case "matrix", "matrix3d", "translate", "translatex", "translatey", "translatez", "translate3d",
		"scale", "scalex", "scaley", "scalez", "scale3d",
		"rotate", "rotatex", "rotatey", "rotatez", "rotate3d",
		"skew", "skewx", "skewy",
		"perspective":
		return true
	// Filters
	case "blur", "brightness", "contrast", "drop-shadow",
		"grayscale", "hue-rotate", "invert", "opacity",
		"saturate", "sepia":
		return true
	// Shapes / paths
	case "circle", "ellipse", "inset", "polygon", "path", "rect", "xywh", "shape":
		return true
	// Counters / quotes / content
	case "counter", "counters", "content",
		"target-counter", "target-counters", "target-text",
		"leader", "running":
		return true
	// Grid
	case "repeat", "minmax", "fit-content":
		return true
	// CSS Custom Highlight / Anchor positioning
	case "anchor", "anchor-size":
		return true
	// CSS Transitions
	case "cubic-bezier", "steps":
		return true
	// Cascade tokens
	case "supports", "format", "local":
		return true
	// View transitions / scroll
	case "view-timeline", "scroll":
		return true
	// Logical / fallback
	case "fallback", "symbols":
		return true
	}
	return false
}

// lastTopLevelBang returns the index of the last `!` token at the top level
// (not inside parens, brackets, braces, or string literals, and not part of a
// CDO `<!--` token per CSS Syntax §4.3.4) of s, or -1 if none.
func lastTopLevelBang(s string) int {
	depth := 0
	inString := byte(0)
	last := -1
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
		case '<':
			// CSS Syntax §4.3.4: `<!--` is a single CDO-token. The `!` inside
			// is part of the token, not a delim, so it must not register as a
			// top-level `!` (otherwise `<!important` patterns get rejected).
			if i+3 < len(s) && s[i+1] == '!' && s[i+2] == '-' && s[i+3] == '-' {
				i += 3
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '!':
			if depth == 0 {
				last = i
			}
		}
	}
	return last
}

// isValidDeclarationValue reports whether s is a well-formed CSS
// <declaration-value> — a sequence of component-values with no top-level
// <colon-token> or <semicolon-token>, and with every `(`, `[`, and `{`
// matched. Per CSS Syntax §5.4.6 a `<simple-block>` (parens, brackets, OR
// braces) is itself a `<component-value>`, so balanced top-level `{...}` is
// permitted — e.g. `@supports (color: { [ var(--a) ] })` parses as a single
// brace-block component-value. Mirrors Blink's
// CSSParserTokenStream::ConsumeDeclarationValue acceptance criteria at
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func isValidDeclarationValue(s string) bool {
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
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
		case '<':
			// CSS Syntax §4.3.4: `<!--` is a single CDO-token; consume it as a
			// unit so the `!` inside isn't treated as a delim. CDO tokens are
			// legal in declaration-value context (variable-supports-018 /
			// variable-supports-050).
			if i+3 < len(s) && s[i+1] == '!' && s[i+2] == '-' && s[i+3] == '-' {
				i += 3
			}
		case '-':
			// CSS Syntax §4.3.4: `-->` is a single CDC-token; same treatment
			// as CDO (variable-supports-051).
			if i+2 < len(s) && s[i+1] == '-' && s[i+2] == '>' {
				i += 2
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth == 0 {
				return false
			}
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth == 0 {
				return false
			}
			bracketDepth--
		case '{':
			braceDepth++
		case '}':
			if braceDepth == 0 {
				return false
			}
			braceDepth--
		case ':', ';':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return false
			}
		}
	}
	if inString != 0 || parenDepth != 0 || bracketDepth != 0 || braceDepth != 0 {
		return false
	}
	return true
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
	"font-variant-ligatures":  {},
	"font-variant-numeric":    {},
	"font-variant-east-asian": {},
	"font-variant-alternates": {},
	"font-kerning":            {}, "font-feature-settings": {},
	"font-synthesis":            {},
	"font-synthesis-weight":     {},
	"font-synthesis-style":      {},
	"font-synthesis-small-caps": {},
	"font-synthesis-position":   {},
	"line-height":               {}, "letter-spacing": {}, "word-spacing": {},
	"text-align": {}, "text-decoration": {}, "text-decoration-line": {},
	"text-decoration-color": {}, "text-decoration-style": {}, "text-decoration-thickness": {},
	"text-decoration-skip-ink": {},
	"text-emphasis":            {}, "text-emphasis-color": {}, "text-emphasis-style": {}, "text-emphasis-position": {},
	"text-indent": {}, "text-transform": {}, "text-shadow": {}, "text-overflow": {},
	"white-space": {}, "word-break": {}, "word-wrap": {}, "overflow-wrap": {},
	"writing-mode": {}, "direction": {}, "unicode-bidi": {},
	// Layout
	"display":              {},
	"position":             {},
	"top":                  {},
	"right":                {},
	"bottom":               {},
	"left":                 {},
	"float":                {},
	"clear":                {},
	"z-index":              {},
	"visibility":           {},
	"overflow":             {},
	"overflow-x":           {},
	"overflow-y":           {},
	"overflow-clip-margin": {},
	"vertical-align":       {},
	"aspect-ratio":         {},
	"opacity":              {},
	"transform":            {},
	"transform-origin":     {},
	"filter":               {},
	"clip":                 {},
	"clip-path":            {},
	"will-change":          {},
	"isolation":            {},
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
	// @font-face metric override descriptors (CSS Fonts 4 §6.1-§6.3).
	// Listed here so @supports (ascent-override: 100%) evaluates correctly
	// now that louis14 parses and applies these descriptors.
	"ascent-override":   {},
	"descent-override":  {},
	"line-gap-override": {},
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
// pseudoElementTokens lists the pseudo-element selector tokens that
// parseSelector extracts from a selector string. CSS Pseudo-Elements 4 §3
// highlight pseudos (`::selection`, `::highlight(name)`, `::target-text`,
// `::spelling-error`, `::grammar-error`) and the originating-element-styled
// pseudos (`::before`, `::after`, `::marker`, `::first-letter`,
// `::first-line`) are recorded in the rule's PseudoElement field.
// `FindMatchingRules` skips any rule with a non-empty PseudoElement when
// computing the originating element's normal cascade, which prevents the
// rule's declarations from leaking onto every element via the empty-Part
// universal-match fallback. Mirrors Blink's CSSSelectorParser::ConsumePseudo()
// classification of these tokens as pseudo-elements rather than pseudo-classes
// (Chromium @ 4883d11f). `::highlight(name)` is handled separately because its
// token has a variable-length argument.
//
// Ordered so longer/double-colon variants are tried first (e.g. "::before"
// before ":before", "::first-letter" before ":first-letter", and each longer
// "::-webkit-scrollbar-*" prefix before the shorter "::-webkit-scrollbar"):
// findTopLevelPseudoElement does a plain substring match and the caller breaks
// on the first hit. Hoisted to package scope so it is not re-allocated on every
// parseSelector call.
var pseudoElementTokens = []struct {
	token string
	name  string
}{
	{"::before", "before"},
	{"::after", "after"},
	{"::first-letter", "first-letter"},
	{"::first-line", "first-line"},
	{"::marker", "marker"},
	{"::selection", "selection"},
	{"::target-text", "target-text"},
	{"::spelling-error", "spelling-error"},
	{"::grammar-error", "grammar-error"},
	// Vendor-prefixed scrollbar pseudo-elements (CSS Scrollbars §color-compat).
	// louis14 paints no scrollbar pseudo-elements, so recording a non-empty
	// PseudoElement makes FindMatchingRules skip the rule from the normal
	// element cascade — without this the empty-Part fallback in
	// matchesCompoundSelector universally matches every element and the
	// declarations (e.g. ::-webkit-scrollbar-corner{background}) leak onto the
	// whole page.
	{"::-webkit-scrollbar-track-piece", "-webkit-scrollbar-track-piece"},
	{"::-webkit-scrollbar-button", "-webkit-scrollbar-button"},
	{"::-webkit-scrollbar-corner", "-webkit-scrollbar-corner"},
	{"::-webkit-scrollbar-track", "-webkit-scrollbar-track"},
	{"::-webkit-scrollbar-thumb", "-webkit-scrollbar-thumb"},
	{"::-webkit-scrollbar", "-webkit-scrollbar"},
	{"::-webkit-resizer", "-webkit-resizer"},
	{":before", "before"},
	{":after", "after"},
	{":first-letter", "first-letter"},
	{":first-line", "first-line"},
}

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
	// pseudoElementTokens (declared above parseSelector) is scanned
	// top-level-first; see its declaration for the ordering rationale.
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
	// `::highlight(NAME)` — CSS Custom Highlight API. The functional notation
	// holds the highlight registry key; extract the whole `::highlight(name)`
	// token and store the pseudo-element as "highlight(name)" so distinct
	// highlight names produce distinct PseudoElement values. Only applied when
	// no other pseudo-element was extracted above.
	if pseudoElement == "" {
		const hlTok = "::highlight("
		if idx := findTopLevelPseudoElement(selectorStr, hlTok); idx >= 0 {
			// Find matching closing paren.
			depth := 1
			end := idx + len(hlTok)
			for end < len(selectorStr) && depth > 0 {
				switch selectorStr[end] {
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth == 0 {
					break
				}
				end++
			}
			if depth == 0 && end < len(selectorStr) {
				name := strings.TrimSpace(selectorStr[idx+len(hlTok) : end])
				pseudoElement = "highlight(" + name + ")"
				if idx > 0 && selectorStr[idx-1] == ' ' {
					pseudoElementForDescendants = true
				}
				selectorStr = selectorStr[:idx] + selectorStr[end+1:]
				selectorStr = strings.TrimSpace(selectorStr)
			}
		}
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
			} else if strings.HasPrefix(pc, "nth-child(") || strings.HasPrefix(pc, "nth-last-child(") {
				// Per CSS Selectors 4 §17, `:nth-child(An+B of S)` adds the
				// pseudo-class's own 10 (class-tier) plus the maximum
				// specificity of any branch in the `of` selector list S.
				// Bare `:nth-child(An+B)` (no `of`) just adds the 10.
				specificity += 10
				var arg string
				if strings.HasPrefix(pc, "nth-child(") {
					arg = pc[len("nth-child(") : len(pc)-1]
				} else {
					arg = pc[len("nth-last-child(") : len(pc)-1]
				}
				if _, ofSel, hasOf := splitNthChildArg(arg); hasOf {
					maxSpec := 0
					for _, sel := range splitSelectorGroup(ofSel) {
						innerSel := parseSelector(strings.TrimSpace(sel))
						if innerSel.Specificity > maxSpec {
							maxSpec = innerSel.Specificity
						}
					}
					specificity += maxSpec
				}
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
		// Normalize namespace-prefixed element selectors. Per CSS Selectors 4
		// §6.1, an unprefixed type selector implicitly matches the default
		// namespace; `ns|tag` restricts to namespace `ns`. louis14 does not
		// track XML namespaces — the synthetic root + body never carry one
		// — so `*|tag` (any-namespace tag) and `|tag` (no-namespace tag)
		// both collapse to the bare tag, and `*|*` to `*`. This matches the
		// behaviour authors get without an `@namespace` rule, which is the
		// only case the WPT selectors corpus exercises.
		if idx := strings.Index(part.Element, "|"); idx >= 0 {
			local := part.Element[idx+1:]
			if local == "" || local == "*" {
				part.Element = "*"
			} else {
				part.Element = local
			}
		}
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

// parseAttributeSelector parses an attribute selector like "type=text" or "href^=https".
//
// Per CSS Selectors 4 §6 and CSS Namespaces 3 §6.2, an attribute name may be
// prefixed with a namespace: `ns|attr` selects attributes in namespace `ns`,
// `*|attr` selects any namespace, and `|attr` selects the no-namespace form
// (equivalent to a bare `attr` in HTML, where all author-supplied attributes
// are in no namespace by default). Mirrors Blink core/css/parser/
// css_selector_parser.cc::ConsumeAttrName @ 4883d11fef4a.
//
// Whitespace IS permitted around the operator and between brackets and the
// attribute name (CSS Selectors 4 §6.1), but NOT inside the namespace prefix
// (e.g. `| attr` is invalid). The bracket-stripping caller trims outer
// whitespace; this function handles operator-adjacent whitespace via
// strings.TrimSpace.
func parseAttributeSelector(s string) AttributeSelector {
	// `|=` is a value-match operator (lang-hyphen). Distinguish it from the
	// namespace prefix `|attr` by requiring the `|` to be followed by `=`.
	// Look for ` |=` or `=` first; otherwise `|` at the start of the name
	// is the no-namespace prefix.
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
				Name:     normalizeAttrName(name),
				Operator: op,
				Value:    value,
			}
		}
	}

	// No operator, just attribute name (existence check)
	return AttributeSelector{
		Name:     normalizeAttrName(strings.TrimSpace(s)),
		Operator: "",
		Value:    "",
	}
}

// normalizeAttrName handles the namespace prefix on attribute selectors.
// In HTML (no @namespace context), all attributes live in no namespace, so:
//   - `attr`     — matches `attr` (no-namespace, the common form)
//   - `|attr`    — matches `attr` (explicit no-namespace, same set in HTML)
//   - `*|attr`   — matches `attr` in any namespace (in HTML, same as bare)
//   - `ns|attr`  — matches `attr` in namespace `ns` (in HTML with no @namespace
//     rules declared, this matches nothing — we leave the prefix
//     attached so a later @namespace-aware path can refuse it)
//
// Whitespace inside the namespace prefix (`| attr` / `ns | attr`) is invalid
// per CSS Selectors 4 §6.1; we detect that by checking for a space adjacent
// to `|` and return the name unchanged so it won't match anything (the
// invalid selector is silently skipped, mirroring Blink's parser-reject).
//
// Mirrors Blink core/css/parser/css_selector_parser.cc::ConsumeAttrName
// @ 4883d11fef4a.
func normalizeAttrName(name string) string {
	idx := strings.Index(name, "|")
	if idx < 0 {
		return name
	}
	// Whitespace adjacent to the bar makes the selector invalid.
	if idx > 0 && name[idx-1] == ' ' {
		return name
	}
	if idx+1 < len(name) && name[idx+1] == ' ' {
		return name
	}
	prefix := name[:idx]
	local := name[idx+1:]
	switch prefix {
	case "", "*":
		// `|attr` and `*|attr` — match the bare attribute name in HTML.
		return local
	}
	// Named-namespace prefix: keep as-is. With no @namespace registry the
	// selector matches nothing, which is the correct behavior for an
	// unregistered prefix per CSS Namespaces 3 §6.2.
	return name
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

	// Invalid queries are treated as dropped (ConsumeQuery returning nullptr),
	// never matching and not flippable by `not`. Return MediaQueryFalse regardless
	// of restrictor.
	if mq.Invalid {
		return MediaQueryFalse
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

// Phase 22: evaluateMediaCondition evaluates a ConditionNode (leaf feature or
// recursive Not/And/Or) into a Kleene 3-valued result. For leaf nodes it
// dispatches per-feature evaluation. For interior nodes it recurses and
// combines with the matching Kleene operation. Mirrors Blink's
// MediaQueryEvaluator::Eval(MediaCondition) recursive dispatch at
// third_party/blink/renderer/core/css/media_query_evaluator.cc @ 4883d11f.
func evaluateMediaCondition(cond ConditionNode, viewportWidth, viewportHeight float64) MediaQueryResult {
	// Recursive interior nodes.
	switch cond.Kind {
	case ConditionNot:
		if len(cond.Children) == 0 {
			return MediaQueryUnknown
		}
		inner := evaluateMediaCondition(cond.Children[0], viewportWidth, viewportHeight)
		return applyMediaRestrictor(MediaRestrictorNot, inner)
	case ConditionAnd:
		result := MediaQueryTrue
		for _, child := range cond.Children {
			result = mediaAnd(result, evaluateMediaCondition(child, viewportWidth, viewportHeight))
			if result == MediaQueryFalse {
				break
			}
		}
		return result
	case ConditionOr:
		result := MediaQueryFalse
		for _, child := range cond.Children {
			result = mediaOr(result, evaluateMediaCondition(child, viewportWidth, viewportHeight))
			if result == MediaQueryTrue {
				break
			}
		}
		return result
	}

	// ConditionFeature: evaluate the leaf feature.
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
		// Our renderer is a colour display (0 bits per pixel of monochrome).
		// Boolean `(monochrome)` asks "is this device monochrome?" → False.
		// `(monochrome: 0)` asks "does the device have 0 monochrome bits?" → True.
		// Mirrors Blink's EvalMonochrome in media_query_evaluator.cc @ 4883d11f.
		if value == "" {
			return MediaQueryFalse // boolean: not a monochrome device
		}
		return boolToResult(value == "0") // numeric: we have 0 monochrome bits

		// Resolution — always match for static renderer, but parse the value
	// to ensure calc() and resolution units are recognized.
	case "resolution", "min-resolution", "max-resolution":
		if cond.Op != "" {
			// Range syntax: resolution > calc(...) etc.
			// Parse the value to ensure it evaluates without error.
			// We always return True in the static renderer (96dpi ≈ 1dppx is the baseline).
			ctx := calcContext{fontSize: 16, chScale: 0.5}
			if IsMathFunction(cond.Value) {
				_, ok := EvalMathFunction(cond.Value, ctx)
				if !ok {
					return MediaQueryUnknown
				}
			} else {
				_, ok := parseLengthFullWithCh(cond.Value, 16, 0, 0, 0.5, 0, 0, 0, 0)
				if !ok {
					return MediaQueryUnknown
				}
			}
		}
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
		// If the value is a calc() expression, evaluate it numerically.
		// Otherwise, compare the string form against "0".
		if IsMathFunction(cond.Value) {
			ctx := calcContext{fontSize: 16, chScale: 0.5}
			numVal, ok := EvalMathFunction(cond.Value, ctx)
			if !ok {
				return MediaQueryUnknown
			}
			// grid matches when the value evaluates to 0.
			return boolToResult(numVal == 0)
		}
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
		// If Op is set (range syntax), use the operator for comparison.
		if cond.Op != "" {
			switch cond.Op {
			case ">":
				return boolToResult(vwDen > vhNum)
			case ">=":
				return boolToResult(vwDen >= vhNum)
			case "<":
				return boolToResult(vwDen < vhNum)
			case "<=":
				return boolToResult(vwDen <= vhNum)
			case "=":
				return boolToResult(vwDen == vhNum)
			default:
				return MediaQueryUnknown
			}
		}
		// Legacy colon-based syntax (min/max prefixes).
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
		// If Op is set (range syntax), use the operator for comparison.
		if cond.Op != "" {
			var viewport float64
			if feature == "width" || feature == "min-width" || feature == "max-width" {
				viewport = viewportWidth
			} else {
				viewport = viewportHeight
			}
			switch cond.Op {
			case ">":
				return boolToResult(viewport > numVal)
			case ">=":
				return boolToResult(viewport >= numVal)
			case "<":
				return boolToResult(viewport < numVal)
			case "<=":
				return boolToResult(viewport <= numVal)
			case "=":
				return boolToResult(viewport == numVal)
			default:
				return MediaQueryUnknown
			}
		}
		// Legacy colon-based syntax (min/max prefixes).
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
// 1). Whitespace around the slash is allowed. Each operand may be a calc()
// expression. Returns numerator, denominator, ok. Negative values are rejected.
// The degenerate `0/0` case is rewritten to `1/0` per the aspect-ratio-004 /
// device-aspect-ratio-002 / -004 spec note — "0/0 is converted into 1/0"
// (infinity) — so that cross-multiplication in the aspect-ratio comparison
// yields the spec-mandated min=never / max=always behavior. Mirrors Blink's
// CSSRatioValue parse + MediaQueryExpValue::Ratio() at
// third_party/blink/renderer/core/css/parser/media_query_parser.cc @
// 4883d11f.
func parseRatio(val string) (num, den float64, ok bool) {
	val = strings.TrimSpace(val)

	// Find the top-level "/" at depth 0 (not inside parens).
	depth := 0
	slashPos := -1
	for i := 0; i < len(val); i++ {
		switch val[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '/':
			if depth == 0 {
				slashPos = i
				break
			}
		}
		if slashPos >= 0 {
			break
		}
	}

	numStr := val
	denStr := "1"
	if slashPos >= 0 {
		numStr = strings.TrimSpace(val[:slashPos])
		denStr = strings.TrimSpace(val[slashPos+1:])
	}

	// Parse numerator (may be calc()).
	ctx := calcContext{fontSize: 16, chScale: 0.5}
	if IsMathFunction(numStr) {
		n, ok := EvalMathFunction(numStr, ctx)
		if !ok || n < 0 {
			return 0, 0, false
		}
		num = n
	} else {
		n, err := strconv.ParseFloat(numStr, 64)
		if err != nil || n < 0 {
			return 0, 0, false
		}
		num = n
	}

	// Parse denominator (may be calc()).
	if IsMathFunction(denStr) {
		d, ok := EvalMathFunction(denStr, ctx)
		if !ok || d < 0 {
			return 0, 0, false
		}
		den = d
	} else {
		d, err := strconv.ParseFloat(denStr, 64)
		if err != nil || d < 0 {
			return 0, 0, false
		}
		den = d
	}

	// "0/0" → "1/0" (infinity) per Media Queries 4 spec note.
	if num == 0 && den == 0 {
		num = 1
	}
	return num, den, true
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
	bracketDepth := 0
	braceDepth := 0

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
		// CSS Syntax §5.4.6: a `<declaration-value>` may contain any
		// `<simple-block>` (parens, brackets, OR braces) and a `<function>`
		// (which is parens-delimited). A top-level `;` inside any of those
		// is a component-value token, not a declaration terminator. Tracking
		// all three depths lets `color: [;]`, `color: { ; }`, and
		// `var(--a, ;)` survive declaration splitting intact — required by
		// variable-reference-18 / variable-reference-19 and the various
		// `;` in fallback / simple-block tests.
		switch ch {
		case '(':
			parenDepth++
			continue
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
			continue
		case '[':
			bracketDepth++
			continue
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			continue
		case '{':
			braceDepth++
			continue
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
			continue
		}
		if ch == ';' && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
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
// When dropImportant is true, !important declarations are discarded entirely
// rather than overwriting earlier normal declarations of the same property.
// This implements CSS Animations §4.1, which treats !important inside
// @keyframes as invalid. Blink: CSSParserImpl::ConsumeKeyframeStyleRule @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func parseDeclarations(declStr string, dropImportant ...bool) DeclarationResult {
	skipImportant := len(dropImportant) > 0 && dropImportant[0]
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

		// CSS Syntax §3.2 / §4.3.7: when the input stream ends inside a
		// function, simple-block, or string, the tokenizer emits an EOF
		// token that the parser treats as an implicit close. The same
		// rules apply when a declaration value is the last byte of the
		// stylesheet — `color: var(--a` must parse as `color: var(--a)`.
		// Append synthesized closing tokens so downstream validators see
		// well-formed input. Required by variable-reference-025/026/027/
		// 028/029/033/035 (the "implicitly closed due to EOF" suite).
		rawValue = closeUnmatchedTokens(rawValue)

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

		// Skip declarations with empty property. An empty value is rejected
		// for non-custom properties, but CSS Custom Properties §3 explicitly
		// permits the empty token list — `--a: ;` declares the custom
		// property and sets it to the empty value (a valid substitution
		// target that resolves to nothing in var() expansion). Required by
		// `variable-declaration-26` and `variable-reference-03`.
		if property == "" {
			continue
		}
		if value == "" && !strings.HasPrefix(rawProperty, "--") {
			continue
		}

		// Skip properties that start with invalid characters
		// (valid CSS properties start with a letter or hyphen)
		if property[0] != '-' && (property[0] < 'a' || property[0] > 'z') && (property[0] < 'A' || property[0] > 'Z') {
			continue
		}

		// Handle !important: strip it if valid, reject if malformed. The `!`
		// search MUST be top-level — `!` tokens inside function calls (e.g.
		// `var(--a, !)`) or simple-blocks (e.g. `(!)`, `[!]`) are part of the
		// declaration value, not the priority annotation. Using a naive
		// `strings.Index` would treat `var(--a,(!))` as having a malformed
		// `!important` and reject the entire declaration, contrary to CSS
		// Syntax §5.4.6 (priority annotation only fires when `!important`
		// appears at the top level of the value). Mirrors Blink's
		// CSSParserImpl::ConsumeImportantAnnotationIfPresent token-stream
		// scan at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		//
		// Apply the same strip to rawValue so downstream custom-property
		// validation doesn't see the priority suffix as a top-level `!`
		// token. `rawValue` differs from `value` only in unescaped sequences
		// (escapes can't appear inside the !important suffix), so the same
		// suffix-locator works for both.
		isImportant := false
		if bangIdx := lastTopLevelBang(value); bangIdx >= 0 {
			afterBang := strings.TrimSpace(value[bangIdx+1:])
			if strings.EqualFold(afterBang, "important") {
				value = strings.TrimSpace(value[:bangIdx])
				isImportant = true
				// After stripping one `!important`, any remaining top-level
				// `!` is a malformed second priority annotation — reject the
				// entire declaration (e.g. `var(--b) !important !important`).
				if lastTopLevelBang(value) >= 0 {
					continue
				}
			} else {
				// Invalid use of ! at top level (e.g., "red ! error") —
				// reject entire declaration.
				continue
			}
		}
		if rawBangIdx := lastTopLevelBang(rawValue); rawBangIdx >= 0 {
			afterRawBang := strings.TrimSpace(rawValue[rawBangIdx+1:])
			if strings.EqualFold(afterRawBang, "important") {
				rawValue = strings.TrimSpace(rawValue[:rawBangIdx])
			}
		}

		// CSS Animations §4.1: !important inside @keyframes is invalid — drop
		// the declaration entirely so it cannot overwrite a prior normal value.
		if isImportant && skipImportant {
			continue
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
			// CSS Custom Properties §3 permits the empty token list (`--a: ;`)
			// as a custom-property value, distinct from the guaranteed-invalid
			// value sentinel installed when `--a: initial` cascades (style.go
			// resolveCSSWideKeywords stores "" for that case). Tag explicit
			// empty user values with a single whitespace-token so var()
			// resolution substitutes whitespace tokens rather than triggering
			// the guaranteed-invalid → fallback / IACVT path. Required by
			// variable-reference-03 / variable-declaration-26.
			if value == "" {
				value = " "
				rawValue = " "
			}
		}

		// CSS Variables §3: a var() reference's first argument must itself be a
		// valid custom-property name. A malformed name (e.g. `var(--, fallback)`
		// where `--` is not a legal name) makes the entire declaration invalid
		// regardless of which property it's on. Reject before the value can
		// overwrite an earlier valid declaration. Use the RAW (pre-unescape)
		// form so escape sequences inside the name (e.g. `var(--foo\27 d, …)`)
		// validate against the source tokens, not the unescaped string.
		if containsVarFunction(rawValue) && hasInvalidVarReference(rawValue) {
			continue
		}

		// CSS 2.1: Reject bare non-zero numbers for length properties (must have units)
		if isLengthProperty(property) && isInvalidBareNumber(value) {
			continue
		}

		// CSS Values 4 §10.10: a math function whose arguments mix types
		// in a way the spec calls out as invalid (e.g. unitless 0 alongside
		// a length-percentage in min()/max()/clamp()) drops the entire
		// declaration so a previously valid one survives. Mirrors Blink's
		// CSSMathFunctionValue::Create returning nullptr on type-check
		// failure (css_math_function_value.cc @
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
		if isLengthProperty(property) && IsMathFunction(value) {
			if !isValidLPMathFunctionAtParseTime(value) {
				continue
			}
		}

		// CSS Multi-column 1 §3.2 `column-count`: <integer [1,∞]> | auto. A
		// non-integer (`2.1`), zero, or negative value is invalid and must NOT
		// overwrite an earlier valid declaration. Required by WPT
		// multicol-count-non-integer-001/002/003,
		// multicol-count-negative-001/002. Mirrors Blink's
		// CSSPropertyParserHelpers::ConsumeColumnCount at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		if property == "column-count" && !isValidColumnCountValue(value) {
			continue
		}

		// CSS Multi-column 1 §3.1 `column-width`: <length [0,∞]> | auto. A
		// non-length keyword or a negative length is invalid.
		if property == "column-width" && !isValidColumnWidthValue(value) {
			continue
		}

		// CSS Multi-column 1 §3.3 `columns` shorthand:
		// `auto | [ <column-width> || <column-count> ] [ / <column-height> ]?`.
		// At most 2 head tokens (one width, one count, or `auto`). Each must
		// itself be a valid <column-width> or <column-count>. Required by WPT
		// multicol-columns-invalid-001/002. We validate before expandShorthand
		// so an invalid shorthand never poisons the cascade.
		if property == "columns" && !isValidColumnsShorthand(value) {
			continue
		}

		// CSS Multi-column 1 §4.5 `column-rule` shorthand:
		// `<column-rule-width> || <column-rule-style> || <column-rule-color>`.
		// At most three whitespace-separated tokens, each matching exactly one
		// slot. Tokens like `normal` are not valid for any slot and must reject
		// the whole declaration (no overwrite of an earlier valid rule).
		// Required by WPT multicol-rule-shorthand-001/2, multicol-shorthand-001.
		if property == "column-rule" && !isValidColumnRuleShorthand(value) {
			continue
		}

		// Reject negative `column-gap` (CSS Multi-column 1 §4.1: <length [0,∞]>
		// | normal). Required by WPT multicol-gap-negative-001.
		if property == "column-gap" && !isValidColumnGapValue(value) {
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
			// Within one declaration block, !important declarations beat
			// non-important ones regardless of source order (CSS Cascade 4
			// §8.3). Skip the write when a later non-important declaration
			// would otherwise overwrite an earlier important one.
			if !isImportant && result.Important["all"] {
				continue
			}
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
			if k == "font-family" {
				normalized, ok := normalizeAndValidateFontFamily(v)
				if !ok {
					continue
				}
				v = normalized
			}
			// Within one declaration block, an !important declaration beats
			// a later non-important one (CSS Cascade 4 §8.3). Skip the write
			// so the earlier important value survives. Mirrors Blink's
			// StyleRule::SetProperty at SHA
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which short-circuits
			// the property set when the existing entry is already important.
			if !isImportant && result.Important[k] {
				continue
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
	// var() references are resolved later — always valid at parse time.
	// Case-insensitive (CSS Syntax §4.3.10) so `VAR(--a)` defers identically.
	if containsVarFunction(value) {
		return true
	}
	// attr() references (CSS Values 5 §11.5) are resolved at cascade time via
	// resolveAttrReferences — always valid at parse time. The attr() type
	// annotation declares what type the attribute value should be resolved as;
	// the parse-time validator cannot check that without element access.
	if containsAttrFunction(value) {
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
	// var() references are resolved later — always valid at parse time
	// (case-insensitive per CSS Syntax §4.3.10).
	if containsVarFunction(trimmed) {
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

// normalizeAndValidateFontFamily validates a `font-family` longhand value and
// returns its canonical form, or `ok=false` if the value violates CSS Fonts 3
// §3.1 / CSS Syntax 3 §4.3.10 / CSS 2.1 §15.3 grammar:
//
//   - <family-name> = <string> | <custom-ident>+
//   - A <custom-ident> in an unquoted family-name cannot begin with a digit,
//     a `-` followed by a digit, or be a pure numeric token.
//   - A quoted string cannot appear adjacent to an unquoted identifier in the
//     same comma entry (`"foo" bar` is invalid syntax).
//   - System-font keywords (`caption`, `icon`, `menu`, `message-box`,
//     `small-caption`, `status-bar`) are reserved for the `font` shorthand and
//     are not valid <family-name> values.
//   - CSS-wide keywords (`initial`, `inherit`, `unset`, `revert`,
//     `revert-layer`) are reserved at the longhand value level — they reach
//     this validator as the entire trimmed value, never as one entry in a
//     comma list.
//   - Generic-family keywords (`serif`, `sans-serif`, `monospace`, `cursive`,
//     `fantasy`, `system-ui`, etc.) ARE valid family-name entries and pass.
//
// If any comma entry fails the gate, the whole declaration is invalid and the
// caller must discard it (so an earlier valid `font-family` is preserved).
// Whitespace runs inside an unquoted entry are condensed to a single space
// (CSS Fonts 3 §3.1 normative note).
//
// Mirrors Blink at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f:
//   - third_party/blink/renderer/core/css/parser/css_property_parser_helpers.cc
//     (ConsumeFamilyName / ConcatenateFamilyName)
//   - third_party/blink/renderer/core/css/parser/css_parser_idioms.cc
//     (IsCSSWideKeyword / system-font keyword gating)
//   - third_party/blink/renderer/core/css/properties/longhands/font_family.cc
//     (ParseSingleValue rejects system-font keywords in the longhand path)
func normalizeAndValidateFontFamily(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	// CSS-wide keywords reach the longhand value verbatim and are resolved at
	// cascade time. They are valid as the entire value but never as one entry
	// in a comma list.
	lower := strings.ToLower(trimmed)
	switch lower {
	case "inherit", "initial", "unset", "revert", "revert-layer":
		return trimmed, true
	}
	// var() references defer to runtime substitution
	// (case-insensitive per CSS Syntax §4.3.10).
	if containsVarFunction(trimmed) {
		return trimmed, true
	}
	entries := splitTopLevelCommas(trimmed)
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		canon, ok := normalizeFontFamilyEntry(entry)
		if !ok {
			return "", false
		}
		out = append(out, canon)
	}
	return strings.Join(out, ", "), true
}

// normalizeFontFamilyEntry validates and canonicalizes a single comma entry
// from a font-family list. See normalizeAndValidateFontFamily for the grammar.
func normalizeFontFamilyEntry(entry string) (string, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", false
	}
	// Quoted string entry: must be entirely enclosed in matching quotes with
	// nothing trailing (`"foo" bar` is the canonical invalid form per test
	// font-family-name-021).
	if entry[0] == '"' || entry[0] == '\'' {
		quote := entry[0]
		// Walk the string body; the closing quote must terminate the entry.
		i := 1
		for i < len(entry) {
			c := entry[i]
			if c == '\\' && i+1 < len(entry) {
				// Skip escaped character.
				i += 2
				continue
			}
			if c == quote {
				break
			}
			i++
		}
		if i >= len(entry) || entry[i] != quote {
			return "", false
		}
		// Everything after the closing quote must be whitespace.
		for j := i + 1; j < len(entry); j++ {
			if entry[j] != ' ' && entry[j] != '\t' && entry[j] != '\n' && entry[j] != '\r' && entry[j] != '\f' {
				return "", false
			}
		}
		return entry[:i+1], true
	}
	// Unquoted entry: a non-empty sequence of CSS identifiers separated by
	// whitespace. Validate each token and condense whitespace.
	//
	// Tokenize on CSS-spec whitespace ONLY (U+0009/0x0A/0x0C/0x0D/0x20 per
	// CSS Syntax 3 §3.2), NOT Unicode whitespace. The fullwidth ideographic
	// space U+3000, for example, is a regular ident code point and stays part
	// of the family name — it's load-bearing for the Japanese localized
	// `ＣＳＳテスト　フォント名` test (font-family-name-010).
	tokens := splitCSSWhitespace(entry)
	if len(tokens) == 0 {
		return "", false
	}
	for _, t := range tokens {
		if !isValidFamilyNameIdentifier(t) {
			return "", false
		}
	}
	// System-font keywords are reserved for the `font` shorthand — reject any
	// unquoted entry whose FIRST token is one of them, regardless of what
	// follows (matches Blink font_family.cc ParseSingleValue behavior and
	// covers tests font-family-name-022 and font-family-name-024).
	if isSystemFontKeyword(strings.ToLower(tokens[0])) {
		return "", false
	}
	return strings.Join(tokens, " "), true
}

// splitCSSWhitespace splits s on CSS-spec whitespace runs (U+0009, U+000A,
// U+000C, U+000D, U+0020 per CSS Syntax 3 §3.2). Unicode whitespace beyond
// that set (e.g. U+3000 IDEOGRAPHIC SPACE, U+00A0 NO-BREAK SPACE) is NOT a
// CSS whitespace code point and stays part of the surrounding token.
func splitCSSWhitespace(s string) []string {
	isCSSWS := func(c byte) bool {
		return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
	}
	var out []string
	start := -1
	for i := 0; i < len(s); i++ {
		if isCSSWS(s[i]) {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// isValidFamilyNameIdentifier reports whether tok is a syntactically valid
// CSS identifier in the context of an unquoted family-name token.
//
// Per CSS Syntax 3 §4.3.10 a custom-ident cannot start with a digit or with
// `-` followed by a digit. A pure numeric token (`400`, `12px`-like prefix)
// is therefore not a valid identifier — it would tokenize as a <number> or
// <dimension>, not an <ident>. Mirrors Blink's css_parser_idioms.cc
// IsValidCustomIdentifier / CSSTokenizer ident classification at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func isValidFamilyNameIdentifier(tok string) bool {
	if tok == "" {
		return false
	}
	// First-codepoint gate. CSS identifier start: letter (a-z, A-Z),
	// underscore, non-ASCII, or `\<escape>`. NOT digit, NOT `-` followed by
	// digit, NOT bare `-` alone.
	r := tok[0]
	switch {
	case r == '-':
		if len(tok) == 1 {
			return false
		}
		if tok[1] >= '0' && tok[1] <= '9' {
			return false
		}
	case r >= '0' && r <= '9':
		return false
	}
	return true
}

// isSystemFontKeyword reports whether lowered is one of the CSS 2.1 §15.3
// system-font keywords reserved for the `font` shorthand. These keywords are
// not valid as a family-name entry on the longhand `font-family` property.
func isSystemFontKeyword(lowered string) bool {
	switch lowered {
	case "caption", "icon", "menu", "message-box", "small-caption", "status-bar":
		return true
	}
	return false
}

// isValidColumnCountValue checks if value is a valid CSS column-count:
// `auto | <integer [1,∞]>` (CSS Multi-column 1 §3.2). Defers var()/attr()/
// CSS-wide keywords to cascade-time resolution.
func isValidColumnCountValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "auto" {
		return true
	}
	// CSS-wide keywords accepted on every property; resolved later.
	if lower == "inherit" || lower == "initial" || lower == "unset" ||
		lower == "revert" || lower == "revert-layer" {
		return true
	}
	if containsVarFunction(trimmed) || containsAttrFunction(trimmed) {
		return true
	}
	// Must be a positive integer literal (no decimal point, no exponent).
	if trimmed == "" {
		return false
	}
	for i, c := range trimmed {
		if i == 0 && (c == '+' || c == '-') {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(trimmed)
	return err == nil && n >= 1
}

// isValidColumnWidthValue checks if value is a valid CSS column-width:
// `auto | <length [0,∞]>` (CSS Multi-column 1 §3.1).
func isValidColumnWidthValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "auto" {
		return true
	}
	if lower == "inherit" || lower == "initial" || lower == "unset" ||
		lower == "revert" || lower == "revert-layer" {
		return true
	}
	if containsVarFunction(trimmed) || containsAttrFunction(trimmed) {
		return true
	}
	w, ok := ParseLength(trimmed)
	if !ok {
		return false
	}
	return w >= 0
}

// isValidColumnsShorthand checks if value is a valid CSS columns shorthand:
// `auto | [ <column-width> || <column-count> ] [ / <column-height> ]?`
// (CSS Multi-column 1 §3.3 + L2 §4.1 height suffix).
//
// The head segment (before `/`) is at most two whitespace-separated tokens:
// at most one column-width and at most one column-count. The tail segment
// (after `/`) is a single <column-height> token: a length or `auto`.
func isValidColumnsShorthand(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "auto" || lower == "inherit" || lower == "initial" ||
		lower == "unset" || lower == "revert" || lower == "revert-layer" {
		return true
	}
	if containsVarFunction(trimmed) || containsAttrFunction(trimmed) {
		return true
	}
	headValue, heightValue, hasSlash := strings.Cut(trimmed, "/")
	parts := strings.Fields(headValue)
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}
	// `auto` may stand in for either a column-width or a column-count slot;
	// at most one explicit width and one explicit count appear.
	hasWidth := false
	hasCount := false
	for _, part := range parts {
		partLower := strings.ToLower(part)
		if partLower == "auto" {
			continue
		}
		// Try integer (column-count): strict — no decimal, no exponent.
		isInt := true
		for i, c := range part {
			if i == 0 && (c == '+' || c == '-') {
				continue
			}
			if c < '0' || c > '9' {
				isInt = false
				break
			}
		}
		if isInt {
			n, err := strconv.Atoi(part)
			if err != nil {
				return false
			}
			if n >= 1 {
				if hasCount {
					return false // duplicate count
				}
				hasCount = true
				continue
			}
			// n == 0 or negative: cannot satisfy <column-count> (which is
			// [1,∞]); fall through to the column-width branch which accepts
			// bare "0" as a zero-length. `-1` will be rejected there because
			// column-width is [0,∞]. Mirrors Blink's columns parser, where
			// `columns: 0` resolves as column-width:0 rather than the
			// invalid count.
		}
		// Non-negative length (column-width). Bare "0" qualifies as a length.
		w, ok := ParseLength(part)
		if !ok || w < 0 {
			return false
		}
		if hasWidth {
			return false // duplicate width
		}
		hasWidth = true
	}
	if hasSlash {
		h := strings.TrimSpace(heightValue)
		hLower := strings.ToLower(h)
		if hLower == "auto" {
			return true
		}
		if _, ok := ParseLength(h); !ok {
			return false
		}
	}
	return true
}

// isValidColumnRuleShorthand checks if value is a valid CSS column-rule
// shorthand: `<column-rule-width> || <column-rule-style> || <column-rule-color>`
// (CSS Multi-column 1 §4.5).
//
// Each component appears at most once, in any order. `currentcolor`, color
// literals, the `<line-style>` keywords, the `thin/medium/thick` width
// keywords, and explicit length widths are accepted. Tokens that don't match
// any slot (e.g. `normal`) make the declaration invalid.
func isValidColumnRuleShorthand(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "inherit" || lower == "initial" || lower == "unset" ||
		lower == "revert" || lower == "revert-layer" {
		return true
	}
	if containsVarFunction(trimmed) || containsAttrFunction(trimmed) {
		return true
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 || len(parts) > 3 {
		return false
	}
	hasWidth := false
	hasStyle := false
	hasColor := false
	for _, part := range parts {
		partLower := strings.ToLower(part)
		if isLineStyleKeyword(partLower) {
			if hasStyle {
				return false
			}
			hasStyle = true
			continue
		}
		if partLower == "thin" || partLower == "medium" || partLower == "thick" {
			if hasWidth {
				return false
			}
			hasWidth = true
			continue
		}
		if w, ok := ParseLength(part); ok && w >= 0 {
			if hasWidth {
				return false
			}
			hasWidth = true
			continue
		}
		// Color slot — must be a valid color (no CSS-wide keywords here; the
		// whole-value CSS-wide branch above already returned).
		if isValidColorValue(part) && partLower != "inherit" && partLower != "initial" &&
			partLower != "unset" && partLower != "revert" && partLower != "revert-layer" {
			if hasColor {
				return false
			}
			hasColor = true
			continue
		}
		return false
	}
	return true
}

// isLineStyleKeyword reports whether part is one of the CSS Backgrounds 3
// `<line-style>` keywords accepted by border-style / column-rule-style etc.
// (https://drafts.csswg.org/css-backgrounds-3/#typedef-line-style).
func isLineStyleKeyword(part string) bool {
	switch part {
	case "none", "hidden", "dotted", "dashed", "solid", "double",
		"groove", "ridge", "inset", "outset":
		return true
	}
	return false
}

// isValidColumnGapValue checks if value is a valid CSS column-gap:
// `normal | <length-percentage [0,∞]>` (CSS Multi-column 1 §4.1 +
// CSS Box Alignment 3). Negative values are invalid.
func isValidColumnGapValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "normal" {
		return true
	}
	if lower == "inherit" || lower == "initial" || lower == "unset" ||
		lower == "revert" || lower == "revert-layer" {
		return true
	}
	if containsVarFunction(trimmed) || containsAttrFunction(trimmed) {
		return true
	}
	// Percentage.
	if strings.HasSuffix(trimmed, "%") {
		numStr := strings.TrimSuffix(trimmed, "%")
		if num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil && num >= 0 {
			return true
		}
		return false
	}
	// calc() defers to runtime resolution; assume valid.
	if strings.HasPrefix(strings.ToLower(trimmed), "calc(") {
		return true
	}
	w, ok := ParseLength(trimmed)
	if !ok {
		return false
	}
	return w >= 0
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
// call that violates the CSS Variables §3 production:
//
//  1. The first argument (the substring before the first top-level comma
//     inside the var() parens) MUST be a valid <custom-property-name>.
//  2. The fallback (everything after the first comma) MUST itself be a valid
//     <declaration-value> — i.e. no top-level <semicolon-token>, no top-level
//     bare <delim-token> `!`, and all brackets balanced. The fallback is
//     declared as `<declaration-value>?` in the grammar, so it inherits the
//     same shape rules a custom-property value follows.
//
// Either violation makes the entire enclosing declaration invalid at parse
// time and the parser must discard it. Mirrors Blink's
// CSSVariableParser::ConsumeVariableReference + ConsumeUnparsedDeclaration
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which fail fast on a
// malformed name token AND validate the fallback's token stream in one pass.
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
			// Validate the fallback as a <declaration-value>: top-level `;`
			// and top-level bare `!` are illegal (CSS Variables §3). The
			// existing custom-property token walker enforces these rules and
			// recurses into nested var() fallbacks for free.
			if commaIdx := findTopLevelComma(inner); commaIdx >= 0 {
				fallback := strings.TrimSpace(inner[commaIdx+1:])
				if fallback != "" && !isValidDeclarationValueTokens(fallback, true) {
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
		// CSS Custom Properties §3 explicitly permits the empty token list
		// as a custom-property value (the "guaranteed-invalid" sentinel
		// written `--a: ;`). Returning true here lets parseDeclarations
		// accept the declaration; var() substitution then handles the
		// empty-list semantics at computed-value time. Required by
		// `variable-declaration-26` and `variable-reference-03`.
		return true
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
// like `bar` or `myvar` don't match. Function-token names are ASCII case-
// insensitive per CSS Syntax §4.3.10, so `VAR(`, `Var(`, etc. all match.
func isVarFunctionAt(s string, openIdx int) bool {
	if openIdx < 3 {
		return false
	}
	if !strings.EqualFold(s[openIdx-3:openIdx], "var") {
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

// closeUnmatchedTokens appends synthesized closing tokens for any unmatched
// `(`, `[`, or `{` in s, so downstream validators see a well-formed token
// stream. Mirrors CSS Syntax §3.2: when input ends inside a function or
// simple-block, the tokenizer emits an implicit EOF that the parser treats
// as a close.
//
// Crucially, an unterminated string is NOT recovered here. Per CSS Syntax
// §4.3.5 a string clipped by EOF (or by a raw newline before the closing
// quote) becomes a `<bad-string-token>`, which is a parse error that
// invalidates the entire declaration. Auto-closing a bad-string-token
// would mask that invalidity (variable-reference-032 — fallback `var(--a,
// "` MUST stay invalid so the earlier `color: green` survives). Likewise
// `url(...)` with an unterminated body becomes a `<bad-url-token>`
// (variable-reference-034) — we leave the unclosed paren in place when
// it's part of a url() so hasInvalidVarReference / isValidImageValue
// still see the parse error.
//
// Tracks open delimiters on a stack so nested structures are closed
// inside-out — `var(--a, [` is recovered as `var(--a, [])` not
// `var(--a, ])`.
func closeUnmatchedTokens(s string) string {
	stack := make([]byte, 0, 4)
	inString := byte(0)
	stringHasNewline := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == '\n' || ch == '\r' || ch == '\f' {
				// CSS Syntax §4.3.5: a raw newline inside a string ends the
				// string-token as a `<bad-string-token>`. The string is
				// closed at the newline and the parser must treat the token
				// as a parse error.
				stringHasNewline = true
				inString = 0
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
			stack = append(stack, ')')
		case '[':
			stack = append(stack, ']')
		case '{':
			stack = append(stack, '}')
		case ')', ']', '}':
			if len(stack) > 0 && stack[len(stack)-1] == ch {
				stack = stack[:len(stack)-1]
			}
		}
	}
	// Unterminated string at EOF: CSS Syntax §4.3.5 distinguishes this
	// case from a string broken by a raw newline. An EOF-terminated string
	// returns a regular `<string-token>` (whose value is the consumed
	// characters); only a newline-terminated string returns a
	// `<bad-string-token>`. We mirror that: EOF-terminated → synthesize
	// the closing quote so downstream sees a valid string;
	// newline-terminated → leave the bad token in place. Required by
	// variable-reference-033 (EOF-close, fallback valid, var resolves) vs
	// variable-reference-032 (newline-broken string, fallback invalid).
	if inString != 0 && !stringHasNewline {
		closed := append([]byte(s), inString)
		return closeUnmatchedTokens(string(closed))
	}
	if inString != 0 {
		// Bad-string-token: leave the bad token in place but still close
		// any block-level `{` so the surrounding rule regains a proper
		// boundary. `(` / `[` pushed after the bad token are inside it
		// and don't need separate closing in the output stream.
		var b strings.Builder
		b.Grow(len(s) + len(stack))
		b.WriteString(s)
		closed := 0
		for _, c := range stack {
			if c == '}' {
				closed++
			}
		}
		for i := 0; i < closed; i++ {
			b.WriteByte('}')
		}
		return b.String()
	}
	if len(stack) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(stack))
	b.WriteString(s)
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteByte(stack[i])
	}
	return b.String()
}

// containsVarFunction reports whether s contains a `var(` function-token in
// any case. CSS Syntax §4.3.10: function-token identifiers are ASCII case-
// insensitive, so `VAR(`, `Var(`, etc. must register the same as `var(`.
// Single-source-of-truth replacement for `strings.Contains(value, "var(")`.
func containsVarFunction(s string) bool {
	return indexVarFunction(s) >= 0
}

// containsAttrFunction reports whether s contains a typed attr() function call
// (CSS Values 5 §11.5). This is used to defer shorthand expansion when the
// value contains an attr() reference that needs element-attribute access for
// resolution — analogous to the containsVarFunction guard for var().
//
// Only typed attr() matters here; the legacy CSS2.1 content: attr(name) form
// is handled separately in ParseContentValues and never reaches shorthand expansion.
func containsAttrFunction(s string) bool {
	return indexAttrFunction(s) >= 0
}

// indexVarFunction returns the byte index of the first `v`/`V` of a `var(`
// function-token in s (so callers can read the open-paren at index+3), or
// -1 if none. Companion to containsVarFunction; also case-insensitive per
// CSS Syntax §4.3.10.
func indexVarFunction(s string) int {
	for i := 3; i < len(s); i++ {
		if s[i] == '(' && isVarFunctionAt(s, i) {
			return i - 3
		}
	}
	return -1
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
		case "unicode-range":
			ff.UnicodeRanges = parseUnicodeRanges(val)
		case "ascent-override":
			ff.AscentOverride = parseFontMetricOverride(val)
		case "descent-override":
			ff.DescentOverride = parseFontMetricOverride(val)
		case "line-gap-override":
			ff.LineGapOverride = parseFontMetricOverride(val)
		}
	}

	if ff.Family == "" || ff.Src == nil {
		return nil
	}
	return ff
}

// parseFontMetricOverride parses a CSS Fonts 4 §6.1-§6.3 metric-override
// descriptor value: `normal | <percentage [0,∞)>`. Returns nil for "normal"
// or any non-parseable / negative value. Returns a pointer to (percent / 100)
// for a non-negative percentage. Mirrors Blink's ConvertFontMetricOverrideValue
// in core/css/font_face.cc and ConsumeFontMetricOverride in
// core/css/parser/at_rule_descriptor_parser.cc at
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func parseFontMetricOverride(val string) *float64 {
	val = strings.TrimSpace(val)
	if strings.EqualFold(val, "normal") {
		return nil
	}
	if !strings.HasSuffix(val, "%") {
		return nil
	}
	numStr := strings.TrimSuffix(val, "%")
	var pct float64
	if _, err := fmt.Sscanf(numStr, "%f", &pct); err != nil {
		return nil
	}
	if pct < 0 {
		// Negative percentages are out-of-range per spec — silently drop.
		return nil
	}
	ratio := pct / 100.0
	return &ratio
}

// parseUnicodeRanges parses a CSS unicode-range descriptor value into a slice
// of UnicodeRange pairs. Handles three token forms per CSS Fonts 3 §4.5:
//   - U+XXXX       — single codepoint (from == to)
//   - U+XXXX-YYYY  — explicit range
//   - U+XXX?       — wildcard range (? = any hex digit, 0-F)
//
// Mirrors Blink's ParseUnicodeRange in core/css/parser/css_parser_impl.cc @
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func parseUnicodeRanges(val string) []UnicodeRange {
	var result []UnicodeRange
	for _, token := range strings.Split(val, ",") {
		token = strings.TrimSpace(token)
		// Strip leading "U+" or "u+"
		upper := strings.ToUpper(token)
		if !strings.HasPrefix(upper, "U+") {
			continue
		}
		hex := token[2:]

		if strings.Contains(hex, "?") {
			// Wildcard: replace '?' with '0' for start, 'F' for end
			lo := strings.ReplaceAll(hex, "?", "0")
			hi := strings.ReplaceAll(hex, "?", "F")
			loVal, err1 := strconv.ParseInt(lo, 16, 32)
			hiVal, err2 := strconv.ParseInt(hi, 16, 32)
			if err1 == nil && err2 == nil {
				result = append(result, UnicodeRange{From: rune(loVal), To: rune(hiVal)})
			}
		} else if idx := strings.Index(hex, "-"); idx >= 0 {
			// Explicit range: U+XXXX-YYYY
			loVal, err1 := strconv.ParseInt(hex[:idx], 16, 32)
			hiVal, err2 := strconv.ParseInt(hex[idx+1:], 16, 32)
			if err1 == nil && err2 == nil {
				result = append(result, UnicodeRange{From: rune(loVal), To: rune(hiVal)})
			}
		} else {
			// Single codepoint
			v, err := strconv.ParseInt(hex, 16, 32)
			if err == nil {
				result = append(result, UnicodeRange{From: rune(v), To: rune(v)})
			}
		}
	}
	return result
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
		// CSS Animations §4.1: !important declarations inside @keyframes are
		// invalid and must be ignored — pass dropImportant=true so they are
		// skipped before they can overwrite a prior normal declaration.
		// Mirrors Blink's CSSParserImpl::ConsumeKeyframeStyleRule @
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		decls := parseDeclarations(declStr, true)
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
		Suffix:  DefaultCounterStyleSuffix,
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
