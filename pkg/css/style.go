package css

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"louis14/pkg/html"
)

// URLData mirrors Blink's `CSSUrlData` (`core/css/css_url_data.h:57-150` @
// chromium-main bf955d02bf0b0c67868b2e62359c0af199af9acc): a parsed
// `url(...)` reference carries both the relative form (as written in the
// source) and the absolute form (resolved against the parsing
// document's base URL at parse time). louis14 stores both so consumers
// fetch by `Absolute` while `Relative` survives for serialization /
// getComputedStyle / cross-document re-resolution on the next cascade pass.
type URLData struct {
	Relative string // The original `url()` inner value as written.
	Absolute string // Resolved against the owning document's BaseDir.
}

type Style struct {
	Properties     map[string]string
	ViewportWidth  float64 // Viewport width in pixels (for vw/vmin/vmax units)
	ViewportHeight float64 // Viewport height in pixels (for vh/vmin/vmax units)
	ChWidth        float64 // Measured advance width of "0" in the element's font (0 = use heuristic)

	// UsedFontSize is the post-`font-size-adjust` font-size in pixels; 0 is a
	// valid value (font-size-adjust: 0 — glyphs collapse to invisible). Read
	// UsedFontSizeSet to distinguish "not yet computed" from "intentionally
	// zero". CSS Fonts 5 §1.7: the used font-size becomes
	//   specified_size * (adjust_value / first_font.actual_aspect_ratio)
	// when font-size-adjust is set to a <number>. All font-relative units on
	// this element (em, ex, ch, …) resolve against the used font-size; child
	// elements still inherit the *computed* (pre-adjust) size for their own
	// em resolution per Fonts 5. Mirrors Blink's `FontDescription::EffectiveSize`
	// at third_party/blink/renderer/platform/fonts/font_description.cc.
	UsedFontSize    float64
	UsedFontSizeSet bool

	// XHeight is the measured x-height in pixels of the element's font at the
	// used font-size (0 = not measured; ex unit then falls back to 0.5em).
	// Populated by the same layout-time pass that fills ChWidth.
	XHeight float64

	// BaseDir is the owning document's BaseDir, propagated by the cascade
	// (see ApplyStylesToDocument). Used by inline `style=""` parsing in
	// cascade.go (via NewParserContext(s.BaseDir)) and as the cross-
	// document re-stamp signal for the LOU-137 v2 adoptNode path
	// (when a moved node lands in a new document, the next cascade pass
	// re-stamps this field). Empty for top-level documents.
	//
	// Post-LOU-138 phase 7: getter-time `url()` resolution no longer
	// reads this field — typed wrappers (CSSImageValue / CSSURIValue)
	// store the parse-time-resolved absolute form and consumers read
	// .Data.Absolute directly. Filter / backdrop-filter values likewise
	// carry pre-resolved URLs in their URLData.
	BaseDir string

	// AppliedTextDecorations is the accumulated text-decoration vector for this
	// element. CSS Text Decor 3 §2: text-decoration is NOT inherited; instead,
	// each box that establishes a text decoration appends an AppliedTextDecoration
	// to its parent's vector. A child cannot remove an ancestor's entry.
	// Mirrors Blink's `AppliedTextDecorationVector` on `ComputedStyle`
	// (third_party/blink/renderer/core/style/applied_text_decoration.h:50 at
	// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Populated post-cascade by
	// ResolveAppliedTextDecorations.
	AppliedTextDecorations []AppliedTextDecoration
}

func NewStyle() *Style {
	return &Style{Properties: make(map[string]string)}
}

// Clone returns a deep copy of this Style with all properties copied.
func (s *Style) Clone() *Style {
	dst := &Style{
		Properties:      make(map[string]string, len(s.Properties)),
		ViewportWidth:   s.ViewportWidth,
		ViewportHeight:  s.ViewportHeight,
		ChWidth:         s.ChWidth,
		UsedFontSize:    s.UsedFontSize,
		UsedFontSizeSet: s.UsedFontSizeSet,
		XHeight:         s.XHeight,
		BaseDir:         s.BaseDir,
	}
	for k, v := range s.Properties {
		dst.Properties[k] = v
	}
	if len(s.AppliedTextDecorations) > 0 {
		dst.AppliedTextDecorations = make([]AppliedTextDecoration, len(s.AppliedTextDecorations))
		copy(dst.AppliedTextDecorations, s.AppliedTextDecorations)
	}
	return dst
}

func (s *Style) Get(property string) (string, bool) {
	val, ok := s.Properties[property]
	if !ok {
		return val, ok
	}
	hadVar := strings.Contains(val, "var(")
	// Resolve var() references in the value
	if hadVar {
		val = s.resolveVarReferences(val)
	}
	// Resolve env() references in the value
	if strings.Contains(val, "env(") {
		val = resolveEnvValue(val)
	}
	// CSS Variables §3 "Invalid at Computed Value Time" (IACVT): when a
	// property's declared value contained a var() reference and the
	// substituted result is not a valid value for that property, the
	// declaration must be treated as invalid at computed-value time. For
	// inherited properties this means use the inherited value; for
	// non-inherited, the initial value. Mirrors Blink's
	// CSSVariableResolver::ResolveVariableReferences IACVT path at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	//
	// We can only IACVT-gate properties whose validator we have. Today that's
	// color-typed properties — the existing isValidColorValue gate. Report
	// "no value" by returning ok=false so the inheritance pass in
	// ApplyInheritedProperties / resolveInheritValues falls back to the
	// parent's computed value (for inherited properties) or the property's
	// initial value (for non-inherited).
	if hadVar && isColorProperty(property) {
		if !isResolvedColorValue(val) {
			return "", false
		}
	}
	return val, ok
}

// isResolvedColorValue reports whether the post-substitution value parses as
// a CSS color or is one of the always-valid keywords. This is the
// substitution-time complement to isValidColorValue: by this point any
// var()/env() reference has been substituted, so we can run ParseColor.
//
// Keyword comparison is ASCII case-insensitive (CSS Syntax §6: identifiers
// are matched ASCII-case-insensitively). strings.ToLower would Unicode-fold
// "İ" (U+0130) to "i" and falsely match "İnitial" as the `initial` keyword;
// asciiToLower preserves non-ASCII bytes as-is.
func isResolvedColorValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	lower := asciiToLower(trimmed)
	if lower == "currentcolor" {
		return true
	}
	if lower == "inherit" || lower == "initial" || lower == "unset" ||
		lower == "revert" || lower == "revert-layer" {
		return true
	}
	_, ok := ParseColor(trimmed)
	return ok
}

// containsVarReferenceTo reports whether value contains a `var(<name>` token
// (case-sensitive, name matched as a whole word). Used by resolveVarReferences
// to detect a single-property self-cycle before recursing into the value.
func containsVarReferenceTo(value, name string) bool {
	idx := 0
	for {
		i := strings.Index(value[idx:], "var(")
		if i < 0 {
			return false
		}
		j := idx + i + 4
		// Skip whitespace.
		for j < len(value) && (value[j] == ' ' || value[j] == '\t' || value[j] == '\n' || value[j] == '\r' || value[j] == '\f') {
			j++
		}
		end := j + len(name)
		if end <= len(value) && value[j:end] == name {
			// Check that the next byte is not an ident-continuation char,
			// so we don't match `var(--ab)` for name="--a".
			if end == len(value) {
				return true
			}
			next := value[end]
			if !isIdentContinue(next) {
				return true
			}
		}
		idx = idx + i + 4
	}
}

// asciiToLower lowercases A-Z to a-z but leaves all other bytes (including
// non-ASCII UTF-8 sequences) untouched. Used for ASCII-case-insensitive CSS
// keyword matching where Unicode case folding would over-match.
func asciiToLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// resolveVarReferences resolves CSS var() function references in a value string.
// Supports var(--name) and var(--name, fallback) syntax with nested var() in fallbacks.
func (s *Style) resolveVarReferences(value string) string {
	// Limit recursion depth to prevent infinite loops
	for depth := 0; depth < 10; depth++ {
		idx := strings.Index(value, "var(")
		if idx == -1 {
			break
		}

		// Find the matching closing paren
		parenDepth := 0
		end := -1
		for i := idx + 3; i < len(value); i++ {
			if value[i] == '(' {
				parenDepth++
			} else if value[i] == ')' {
				parenDepth--
				if parenDepth == 0 {
					end = i
					break
				}
			}
		}
		if end == -1 {
			break // Malformed var() — no closing paren
		}

		// Extract the content between var( and )
		content := strings.TrimSpace(value[idx+4 : end])

		// Split on first comma (separating property name from fallback)
		varName := content
		fallback := ""
		if commaIdx := findCommaOutsideParens(content); commaIdx >= 0 {
			varName = strings.TrimSpace(content[:commaIdx])
			fallback = strings.TrimSpace(content[commaIdx+1:])
		}

		// Look up the custom property (bypass Get to avoid recursion).
		// The empty-string sentinel means the property is set to the
		// guaranteed-invalid value (CSS Custom Properties §3) — treat as
		// "not defined" so the fallback is used per CSS Variables §3.
		//
		// Self-reference cycle (CSS Variables §3 Cycles): if the property's
		// own value contains a var() pointing back at itself, the property is
		// in a cycle and resolves to the guaranteed-invalid value, which
		// triggers fallback selection. Mirrors Blink's
		// CSSVariableResolver::ResolveVariableTokens cycle-set check at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. This catches the
		// single-property self-cycle (`--a: var(--a)`); multi-property cycles
		// like a→b→a are not yet detected (out of scope for Theme 1).
		resolved := ""
		if propVal, ok := s.Properties[varName]; ok && propVal != "" {
			if !containsVarReferenceTo(propVal, varName) {
				resolved = propVal
			} else if fallback != "" {
				resolved = fallback
			}
		} else if fallback != "" {
			resolved = fallback
		}

		// Per CSS Variables §3, var() substitution is at the TOKEN level —
		// not the string level. When the byte immediately after the var()
		// reference is an ident-continuation character, naive string
		// concatenation would fuse the resolved value's trailing token with
		// the following ident (e.g. `var(--b)red` with --b=orange becoming
		// "orangered" — a valid but wrong color). Insert a single space
		// between the resolved value and a trailing ident-continuation byte
		// to preserve the token boundary. Same on the leading side, though
		// CSS rarely emits an ident immediately before `var(` since `var`
		// itself is an ident; the leading guard handles edge cases like
		// `xvar(--b)` which our isVarFunctionAt rejects anyway, so the
		// trailing guard is the load-bearing one for tests 14/53/54/55.
		var sb strings.Builder
		sb.WriteString(value[:idx])
		if resolved != "" && idx > 0 && isIdentContinue(value[idx-1]) {
			sb.WriteByte(' ')
		}
		sb.WriteString(resolved)
		if resolved != "" && end+1 < len(value) && isIdentContinue(value[end+1]) {
			sb.WriteByte(' ')
		}
		sb.WriteString(value[end+1:])
		value = sb.String()
	}
	return value
}

// isIdentContinue reports whether b is a byte that can appear inside a CSS
// ident-token (letter, digit, hyphen, underscore, or any non-ASCII leading
// byte). Used by var() substitution to preserve token boundaries when
// concatenating the resolved value with surrounding source text.
func isIdentContinue(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '-' || b == '_' || b >= 0x80
}

// resolveEnvValue replaces env(variable-name) and env(variable-name, fallback)
// with either the known static value or the fallback.
// In our static renderer (no notch/safe areas), all env() values resolve to 0px.
// env(name) → "0px"; env(name, fallback) → fallback value.
func resolveEnvValue(val string) string {
	// Known env() variables and their static values
	knownEnvVars := map[string]string{
		"safe-area-inset-top":    "0px",
		"safe-area-inset-right":  "0px",
		"safe-area-inset-bottom": "0px",
		"safe-area-inset-left":   "0px",
		"titlebar-area-x":        "0px",
		"titlebar-area-y":        "0px",
		"titlebar-area-width":    "100%",
		"titlebar-area-height":   "0px",
		"keyboard-inset-top":     "0px",
		"keyboard-inset-right":   "0px",
		"keyboard-inset-bottom":  "0px",
		"keyboard-inset-left":    "0px",
		"keyboard-inset-height":  "0px",
		"keyboard-inset-width":   "0px",
	}

	// Replace all env(...) occurrences
	for i := 0; i < 20; i++ { // limit iterations to prevent infinite loops
		lower := strings.ToLower(val)
		start := strings.Index(lower, "env(")
		if start < 0 {
			break
		}

		// Find the matching closing paren (handle nested parens)
		depth := 0
		end := start + 4 // position after "env("
		found := false
		for end < len(val) {
			switch val[end] {
			case '(':
				depth++
			case ')':
				if depth == 0 {
					found = true
				} else {
					depth--
				}
			}
			if found {
				break
			}
			end++
		}
		if !found {
			break // malformed env()
		}

		inner := strings.TrimSpace(val[start+4 : end])

		// Split on the first comma (outside parens) to get variable name and optional fallback
		commaIdx := findCommaOutsideParens(inner)
		varName := strings.ToLower(strings.TrimSpace(inner))
		fallback := "0px"
		if commaIdx >= 0 {
			varName = strings.ToLower(strings.TrimSpace(inner[:commaIdx]))
			fallback = strings.TrimSpace(inner[commaIdx+1:])
		}

		resolved := fallback // default: use fallback if present, else "0px"
		if known, ok := knownEnvVars[varName]; ok {
			resolved = known
		}

		val = val[:start] + resolved + val[end+1:]
	}
	return val
}

// findCommaOutsideParens finds the index of the first comma not inside parentheses.
func findCommaOutsideParens(s string) int {
	depth := 0
	for i, ch := range s {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (s *Style) Set(property, value string) {
	// CSS spec: box-shadow accepts either "none" OR a comma-separated list of
	// shadows, but not both. Reject invalid declarations so they don't overwrite
	// a previous valid value in the cascade.
	if property == "box-shadow" && value != "none" {
		parts := splitCommaSeparated(value)
		for _, part := range parts {
			if strings.TrimSpace(part) == "none" {
				return // Invalid: "none" mixed with other values
			}
		}
	}
	s.Properties[property] = value
}

func (s *Style) GetLength(property string) (float64, bool) {
	val, ok := s.Get(property)
	if !ok {
		return 0, false
	}
	return parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight)
}

// chScale returns the ch unit multiplier relative to fontSize for this style's font.
// CSS Values §6.1: ch is the advance measure of "0" in the inline axis.
// Per CSS Writing Modes §7.5 and Blink's IsHorizontalTypographicMode():
//   - vertical-rl/vertical-lr with text-orientation: upright → ch = 1em (vertical advance)
//   - vertical-rl/vertical-lr with text-orientation: sideways or mixed (default) → ch = horizontal advance
//   - sideways-rl/sideways-lr → always rotated, ch = horizontal advance
//   - horizontal-tb → ch = horizontal advance
func (s *Style) chScale() float64 {
	wm, _ := s.Get("writing-mode")

	horizontalCh := func() float64 {
		if s.ChWidth > 0 {
			if fs := s.GetFontSize(); fs > 0 {
				return s.ChWidth / fs
			}
		}
		return 0.5
	}

	switch wm {
	case "vertical-rl", "vertical-lr":
		to, _ := s.Get("text-orientation")
		if to == "upright" {
			// Upright glyphs: ch = vertical advance height of "0" ≈ 1em
			return 1.0
		}
		// "sideways" or "mixed" (default): Latin "0" is rotated sideways
		return horizontalCh()
	case "sideways-rl", "sideways-lr":
		// sideways-* always rotates glyphs regardless of text-orientation
		return horizontalCh()
	default:
		// horizontal-tb: use measured horizontal advance width
		return horizontalCh()
	}
}

// ParseURLReference extracts the fragment id from a CSS `url(#id)`
// value. Accepts the full CSS Image Values L4 §3.1 `<url>` grammar:
//
//	url(#id)
//	url("#id")
//	url('#id')
//	url( #id )           — whitespace around the inner expression
//	url(path#id)         — path part is stripped
//	url(#id), url(#id2)  — extracts only the FIRST fragment from a list
//	url(#id-with-hyphens-and_underscores.dots)
//
// Returns (id, true) on success. Returns ("", false) for:
//
//	"" / "none" / non-`url()` syntax
//	`url()` (empty inner)
//	`url(http://...)` (no `#` fragment, non-empty path → URL without fragment)
//	`url(#)` (empty fragment)
//
// This is the shared helper for `mask`/`mask-image`, `clip-path`,
// `filter`, and `fill`/`stroke` paint server resolution — replaces the
// per-callsite parsers that scattered across the SVG-foundation phases.
// Mirrors Blink's `CSSURIValue::FragmentIdentifier` + its consumers
// (core/css/css_uri_value.cc, core/style/style_image.cc).
func ParseURLReference(val string) (string, bool) {
	val = strings.TrimSpace(val)
	// Take the first item of a comma-separated list. CSS Masking 1
	// allows `mask-image: url(#a), url(#b)` — the SVG fast-path
	// currently uses only the first reference (matches mask-opacity-1d).
	if i := strings.Index(val, ","); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	if !strings.HasPrefix(val, "url(") {
		return "", false
	}
	if !strings.HasSuffix(val, ")") {
		return "", false
	}
	inner := val[4 : len(val)-1]
	inner = strings.TrimSpace(inner)
	// Strip surrounding quotes (CSS allows both forms).
	if len(inner) >= 2 {
		first := inner[0]
		last := inner[len(inner)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			inner = inner[1 : len(inner)-1]
		}
	}
	if inner == "" {
		return "", false
	}
	// Extract the fragment portion after `#`. The path part (if any)
	// is dropped — louis14's resolution is in-document only, matching
	// Blink's TreeScope-scoped lookup behavior.
	idx := strings.Index(inner, "#")
	if idx < 0 {
		// No fragment → not a same-document SVG resource reference.
		return "", false
	}
	id := inner[idx+1:]
	if id == "" {
		return "", false
	}
	return id, true
}

// ParsePercentage parses a percentage value (e.g., "140%") and returns the number (e.g., 140).
func ParsePercentage(val string) (float64, bool) {
	val = strings.TrimSpace(val)
	if !strings.HasSuffix(val, "%") {
		return 0, false
	}
	numStr := strings.TrimSuffix(val, "%")
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, false
	}
	return num, true
}

// GetPercentage returns the percentage value of a property (e.g., "140%" returns 140).
func (s *Style) GetPercentage(property string) (float64, bool) {
	val, ok := s.Get(property)
	if !ok {
		return 0, false
	}
	return ParsePercentage(val)
}

// ParseLength parses a length value (e.g., "100px" or "100")
// Does not handle em units — use ParseLengthWithFontSize for that.
func ParseLength(val string) (float64, bool) {
	return ParseLengthFull(val, 16.0, 0, 0)
}

// ParseLengthWithFontSize parses a length value with em and rem support.
func ParseLengthWithFontSize(val string, fontSize float64) (float64, bool) {
	return ParseLengthFull(val, fontSize, 0, 0)
}

// ParseLengthOrPercentageFontRelative parses a CSS value where percentages
// resolve against the element's font-size (1em) — e.g. text-underline-offset,
// text-decoration-thickness per CSS Text Decor L4. Returns the resolved
// pixel value. Handles bare `%`, plain lengths, and `calc()` expressions
// whose % terms also resolve against font-size.
func ParseLengthOrPercentageFontRelative(val string, fontSize float64) (float64, bool) {
	v := strings.TrimSpace(val)
	if strings.HasSuffix(v, "%") {
		if pct, ok := ParsePercentage(v); ok {
			return pct / 100.0 * fontSize, true
		}
		return 0, false
	}
	if strings.HasPrefix(v, "calc(") && strings.HasSuffix(v, ")") {
		return EvalCalcWithPercent(v[5:len(v)-1], fontSize, fontSize)
	}
	return ParseLengthWithFontSize(val, fontSize)
}

// splitCSSFunctionArgs splits comma-separated args respecting nested parens
func splitCSSFunctionArgs(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, ch := range s {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
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

// parseMathArg resolves a CSS value that may be a length or percentage
// Percentages are resolved against the viewport width (best approximation)
func parseMathArg(s string, fontSize, vw, vh float64) (float64, bool) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		numStr := strings.TrimSuffix(s, "%")
		num, err := strconv.ParseFloat(numStr, 64)
		if err == nil && vw > 0 {
			return num * vw / 100, true
		}
		return 0, false
	}
	return ParseLengthFull(s, fontSize, vw, vh)
}

func evalMinMax(argsStr, mode string, fontSize, vw, vh float64) (float64, bool) {
	args := splitCSSFunctionArgs(argsStr)
	if len(args) < 2 {
		return 0, false
	}
	result, ok := parseMathArg(args[0], fontSize, vw, vh)
	if !ok {
		return 0, false
	}
	for _, arg := range args[1:] {
		val, ok := parseMathArg(arg, fontSize, vw, vh)
		if !ok {
			return 0, false
		}
		if mode == "min" && val < result {
			result = val
		} else if mode == "max" && val > result {
			result = val
		}
	}
	return result, true
}

func evalClamp(argsStr string, fontSize, vw, vh float64) (float64, bool) {
	args := splitCSSFunctionArgs(argsStr)
	if len(args) != 3 {
		return 0, false
	}
	minVal, ok1 := parseMathArg(args[0], fontSize, vw, vh)
	prefVal, ok2 := parseMathArg(args[1], fontSize, vw, vh)
	maxVal, ok3 := parseMathArg(args[2], fontSize, vw, vh)
	if !ok1 || !ok2 || !ok3 {
		return 0, false
	}
	if prefVal < minVal {
		return minVal, true
	}
	if prefVal > maxVal {
		return maxVal, true
	}
	return prefVal, true
}

// ParseLengthFull parses a length value with em, rem, and viewport unit support.
// Uses a default ch multiplier of 0.5em (horizontal writing mode approximation).
func ParseLengthFull(val string, fontSize, viewportWidth, viewportHeight float64) (float64, bool) {
	return parseLengthFullWithCh(val, fontSize, viewportWidth, viewportHeight, 0.5, 0)
}

// parseLengthFullWithCh is the internal implementation that accepts a custom ch multiplier.
// chScale is the multiplier for the ch unit relative to fontSize (0.5 for horizontal, 1.0 for vertical).
// xHeight is the measured x-height in pixels for the ex unit (0 = use 0.5em heuristic).
// Mirrors Blink's `LengthValue::ToCSSValue` font-relative branch in
// third_party/blink/renderer/core/css/css_to_length_conversion_data.cc at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which sources the ex unit from
// FontMetrics::XHeight rather than a fixed em fraction.
func parseLengthFullWithCh(val string, fontSize, viewportWidth, viewportHeight, chScale, xHeight float64) (float64, bool) {
	val = strings.TrimSpace(val)
	// Resolve env() variables before any other parsing
	if strings.Contains(val, "env(") {
		val = resolveEnvValue(val)
		val = strings.TrimSpace(val)
	}
	// Handle min(), max(), clamp() functions
	if strings.HasPrefix(val, "min(") && strings.HasSuffix(val, ")") {
		return evalMinMax(val[4:len(val)-1], "min", fontSize, viewportWidth, viewportHeight)
	}
	if strings.HasPrefix(val, "max(") && strings.HasSuffix(val, ")") {
		return evalMinMax(val[4:len(val)-1], "max", fontSize, viewportWidth, viewportHeight)
	}
	if strings.HasPrefix(val, "clamp(") && strings.HasSuffix(val, ")") {
		return evalClamp(val[6:len(val)-1], fontSize, viewportWidth, viewportHeight)
	}
	// Handle calc() expressions — pass full context so ch/vw/vh/ex units resolve correctly.
	if strings.HasPrefix(val, "calc(") && strings.HasSuffix(val, ")") {
		expr := val[5 : len(val)-1] // strip "calc(" and ")"
		ctx := calcContext{
			fontSize:       fontSize,
			viewportWidth:  viewportWidth,
			viewportHeight: viewportHeight,
			chScale:        chScale,
			xHeight:        xHeight,
		}
		result, ok := evalCalcFull(expr, ctx)
		if ok {
			return result, true
		}
		return 0, false
	}
	// Dynamic/static/large viewport units — treat all as equivalent to vh/vw
	// in static renderer (no browser chrome changes). Must be checked before
	// vmin/vmax/vw/vh to avoid suffix conflicts (e.g. "dvmin" ends with "min").
	for _, suffix := range []string{"dvmin", "svmin", "lvmin"} {
		if strings.HasSuffix(val, suffix) {
			numStr := strings.TrimSuffix(val, suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, false
			}
			return num * math.Min(viewportWidth, viewportHeight) / 100, true
		}
	}
	for _, suffix := range []string{"dvmax", "svmax", "lvmax"} {
		if strings.HasSuffix(val, suffix) {
			numStr := strings.TrimSuffix(val, suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, false
			}
			return num * math.Max(viewportWidth, viewportHeight) / 100, true
		}
	}
	for _, suffix := range []string{"dvw", "svw", "lvw"} {
		if strings.HasSuffix(val, suffix) {
			numStr := strings.TrimSuffix(val, suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, false
			}
			return num * viewportWidth / 100, true
		}
	}
	for _, suffix := range []string{"dvh", "svh", "lvh"} {
		if strings.HasSuffix(val, suffix) {
			numStr := strings.TrimSuffix(val, suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, false
			}
			return num * viewportHeight / 100, true
		}
	}
	// Container query units — approximate as viewport units when no container context.
	// cqw/cqi (inline axis) ≈ vw; cqh/cqb (block axis) ≈ vh.
	for _, suffix := range []string{"cqw", "cqi"} {
		if strings.HasSuffix(val, suffix) {
			numStr := strings.TrimSuffix(val, suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, false
			}
			return num * viewportWidth / 100, true
		}
	}
	for _, suffix := range []string{"cqh", "cqb"} {
		if strings.HasSuffix(val, suffix) {
			numStr := strings.TrimSuffix(val, suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, false
			}
			return num * viewportHeight / 100, true
		}
	}
	// Viewport units (check vmin/vmax before vw/vh to avoid suffix conflicts)
	if strings.HasSuffix(val, "vmin") {
		numStr := strings.TrimSuffix(val, "vmin")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * math.Min(viewportWidth, viewportHeight) / 100, true
	}
	if strings.HasSuffix(val, "vmax") {
		numStr := strings.TrimSuffix(val, "vmax")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * math.Max(viewportWidth, viewportHeight) / 100, true
	}
	if strings.HasSuffix(val, "vw") {
		numStr := strings.TrimSuffix(val, "vw")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * viewportWidth / 100, true
	}
	if strings.HasSuffix(val, "vh") {
		numStr := strings.TrimSuffix(val, "vh")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * viewportHeight / 100, true
	}
	if strings.HasSuffix(val, "rem") {
		// rem is relative to root font size (typically 16px)
		numStr := strings.TrimSuffix(val, "rem")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 16.0, true // Root font size = 16px
	}
	if strings.HasSuffix(val, "em") {
		numStr := strings.TrimSuffix(val, "em")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * fontSize, true
	}
	// ch unit: advance measure of '0' character in the inline axis.
	// In horizontal writing modes, this is the horizontal advance width ≈ 0.5em.
	// In vertical writing modes, this is the vertical advance height ≈ 1.0em.
	// The chScale parameter controls this multiplier.
	if strings.HasSuffix(val, "ch") {
		numStr := strings.TrimSuffix(val, "ch")
		num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
		if err != nil {
			return 0, false
		}
		return num * fontSize * chScale, true
	}
	// ex unit: x-height of the current font (CSS Values 4 §6.1.1). Measured
	// per-font and threaded through xHeight; fall back to a 0.5em heuristic
	// when no measurement is available (e.g. for callers that don't have a
	// Style in hand, like ParseLengthFull).
	if strings.HasSuffix(val, "ex") {
		numStr := strings.TrimSuffix(val, "ex")
		num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
		if err != nil {
			return 0, false
		}
		if xHeight > 0 {
			return num * xHeight, true
		}
		return num * fontSize * 0.5, true
	}
	// rlh unit: root line-height (must be checked before lh to avoid suffix conflict).
	// Approximate as 16px * 1.2 = 19.2px.
	if strings.HasSuffix(val, "rlh") {
		numStr := strings.TrimSuffix(val, "rlh")
		num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
		if err != nil {
			return 0, false
		}
		return num * 19.2, true // 16px default font size * 1.2 line-height
	}
	// lh unit: line-height of the element, approximate as 1.2em.
	if strings.HasSuffix(val, "lh") {
		numStr := strings.TrimSuffix(val, "lh")
		num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
		if err != nil {
			return 0, false
		}
		return num * fontSize * 1.2, true
	}
	// ic unit: advance measure of the full-width CJK character.
	// Approximate as 1em (full character width).
	if strings.HasSuffix(val, "ic") {
		numStr := strings.TrimSuffix(val, "ic")
		num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
		if err != nil {
			return 0, false
		}
		return num * fontSize, true
	}
	// cap unit: cap-height (uppercase letter height), approximately 0.7em.
	if strings.HasSuffix(val, "cap") {
		numStr := strings.TrimSuffix(val, "cap")
		num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
		if err != nil {
			return 0, false
		}
		return num * fontSize * 0.7, true
	}
	if strings.HasSuffix(val, "mm") {
		numStr := strings.TrimSuffix(val, "mm")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 3.7795275591, true // 1mm ≈ 3.78px at 96dpi
	}
	if strings.HasSuffix(val, "in") {
		numStr := strings.TrimSuffix(val, "in")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 96.0, true // 1in = 96px at 96dpi
	}
	if strings.HasSuffix(val, "cm") {
		numStr := strings.TrimSuffix(val, "cm")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 37.7952755906, true // 1cm = 96/2.54 px
	}
	if strings.HasSuffix(val, "pc") {
		numStr := strings.TrimSuffix(val, "pc")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 16.0, true // 1pc = 16px
	}
	if strings.HasSuffix(val, "pt") {
		numStr := strings.TrimSuffix(val, "pt")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * (96.0 / 72.0), true // 1pt = 96/72 px
	}
	if strings.HasSuffix(val, "px") {
		val = strings.TrimSuffix(val, "px")
	} else {
		// CSS 2.1: lengths require units (except 0)
		// Bare numbers without units are invalid
		num, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false
		}
		if num == 0 {
			return 0, true
		}
		return 0, false // non-zero without unit is invalid
	}
	num, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, false
	}
	return num, true
}

// IsCalcWithPercent returns true if the value is a calc() expression containing a % term.
func IsCalcWithPercent(val string) bool {
	val = strings.TrimSpace(val)
	return strings.HasPrefix(val, "calc(") && strings.HasSuffix(val, ")") &&
		strings.Contains(val, "%")
}

// calcContext holds all parameters needed to resolve values inside calc() expressions.
type calcContext struct {
	fontSize       float64
	percentBase    float64
	viewportWidth  float64
	viewportHeight float64
	chScale        float64
	// xHeight is the element's measured x-height in pixels for the ex unit
	// (0 = caller has no measurement; ex falls back to 0.5em).
	xHeight float64
	// percentResolvesToZero, when true, causes percent tokens to resolve to 0
	// instead of failing the parse. Mirrors Blink's intrinsic-sizing behavior
	// where text-indent's percentage is treated as 0 (third_party/blink/
	// renderer/core/layout/inline/inline_node.cc — InlineNode::ComputeMinMaxSizes
	// uses LayoutUnit() for HasPercent text-indent).
	percentResolvesToZero bool
}

// evalCalcExpr evaluates a CSS calc() expression with proper operator precedence.
// Supports +, -, *, / operators and px/em/rem/%  values.
func evalCalcExpr(expr string, fontSize float64) (float64, bool) {
	return EvalCalcWithPercent(expr, fontSize, 0)
}

// evalCalcFull evaluates a calc() expression with full context (viewport, ch scale, percent base).
func evalCalcFull(expr string, ctx calcContext) (float64, bool) {
	expr = strings.TrimSpace(expr)
	if strings.Contains(expr, "env(") {
		expr = resolveEnvValue(expr)
		expr = strings.TrimSpace(expr)
	}
	tokens := tokenizeCalc(expr)
	if len(tokens) == 0 {
		return 0, false
	}
	result, ok := parseCalcAddSub(tokens, 0, ctx)
	if !ok {
		return 0, false
	}
	return result.value, true
}

// EvalCalcWithPercent evaluates a CSS calc() expression with percent base support.
// percentBase is the reference size for resolving % values (e.g. containing block width).
func EvalCalcWithPercent(expr string, fontSize, percentBase float64) (float64, bool) {
	return evalCalcFull(expr, calcContext{
		fontSize:    fontSize,
		percentBase: percentBase,
		chScale:     0.5,
	})
}

// EvalCalcIntrinsic evaluates a calc() expression in an intrinsic-sizing
// context: percent terms resolve to 0 instead of failing the parse. The
// length, em, and viewport-unit components contribute normally. Mirrors
// Blink's treatment of percent text-indent in InlineNode::ComputeMinMaxSizes.
func EvalCalcIntrinsic(expr string, fontSize float64) (float64, bool) {
	return evalCalcFull(expr, calcContext{
		fontSize:              fontSize,
		percentBase:           0,
		percentResolvesToZero: true,
		chScale:               0.5,
	})
}

type calcResult struct {
	value float64
	pos   int // position in token slice after consuming
}

func parseCalcAddSub(tokens []string, pos int, ctx calcContext) (calcResult, bool) {
	left, ok := parseCalcMulDiv(tokens, pos, ctx)
	if !ok {
		return calcResult{}, false
	}
	for left.pos < len(tokens) {
		op := tokens[left.pos]
		if op != "+" && op != "-" {
			break
		}
		right, ok := parseCalcMulDiv(tokens, left.pos+1, ctx)
		if !ok {
			return calcResult{}, false
		}
		if op == "+" {
			left.value += right.value
		} else {
			left.value -= right.value
		}
		left.pos = right.pos
	}
	return left, true
}

func parseCalcMulDiv(tokens []string, pos int, ctx calcContext) (calcResult, bool) {
	left, ok := parseCalcAtom(tokens, pos, ctx)
	if !ok {
		return calcResult{}, false
	}
	for left.pos < len(tokens) {
		op := tokens[left.pos]
		if op != "*" && op != "/" {
			break
		}
		right, ok := parseCalcAtom(tokens, left.pos+1, ctx)
		if !ok {
			return calcResult{}, false
		}
		if op == "*" {
			left.value *= right.value
		} else {
			if right.value == 0 {
				return calcResult{}, false
			}
			left.value /= right.value
		}
		left.pos = right.pos
	}
	return left, true
}

func parseCalcAtom(tokens []string, pos int, ctx calcContext) (calcResult, bool) {
	if pos >= len(tokens) {
		return calcResult{}, false
	}
	token := tokens[pos]
	// Handle parenthesized sub-expressions
	if token == "(" {
		result, ok := parseCalcAddSub(tokens, pos+1, ctx)
		if !ok || result.pos >= len(tokens) || tokens[result.pos] != ")" {
			return calcResult{}, false
		}
		result.pos++ // consume ")"
		return result, true
	}
	// Handle percentage values: resolve against percentBase. With
	// percentResolvesToZero, percent terms evaluate to 0 (used in intrinsic
	// sizing where there is no containing block to resolve against).
	if strings.HasSuffix(token, "%") && (ctx.percentBase > 0 || ctx.percentResolvesToZero) {
		numStr := strings.TrimSuffix(token, "%")
		if num, err := strconv.ParseFloat(numStr, 64); err == nil {
			return calcResult{value: num * ctx.percentBase / 100, pos: pos + 1}, true
		}
	}
	// Parse as a length value using full context (viewport, ch scale, x-height)
	val, ok := parseLengthFullWithCh(token, ctx.fontSize, ctx.viewportWidth, ctx.viewportHeight, ctx.chScale, ctx.xHeight)
	if ok {
		return calcResult{value: val, pos: pos + 1}, true
	}
	// Try as plain number (unitless, e.g., divisor)
	num, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return calcResult{}, false
	}
	return calcResult{value: num, pos: pos + 1}, true
}

// tokenizeCalc splits a calc expression into tokens: numbers with units, operators, parens.
func tokenizeCalc(expr string) []string {
	var tokens []string
	i := 0
	for i < len(expr) {
		ch := expr[i]
		if ch == ' ' || ch == '\t' {
			i++
			continue
		}
		if ch == '(' || ch == ')' {
			tokens = append(tokens, string(ch))
			i++
			continue
		}
		if ch == '+' || ch == '*' || ch == '/' {
			tokens = append(tokens, string(ch))
			i++
			continue
		}
		// Minus: could be operator or negative sign
		if ch == '-' {
			// It's an operator if the previous token is a number/closing paren
			if len(tokens) > 0 {
				prev := tokens[len(tokens)-1]
				if prev != "+" && prev != "-" && prev != "*" && prev != "/" && prev != "(" {
					tokens = append(tokens, "-")
					i++
					continue
				}
			}
			// Otherwise it's part of a number (negative sign) — fall through to number parsing
		}
		// Number (possibly with unit suffix)
		start := i
		if expr[i] == '-' {
			i++
		}
		for i < len(expr) && ((expr[i] >= '0' && expr[i] <= '9') || expr[i] == '.') {
			i++
		}
		// Consume unit suffix (px, em, rem, %, etc.)
		unitStart := i
		for i < len(expr) && ((expr[i] >= 'a' && expr[i] <= 'z') || expr[i] == '%') {
			i++
		}
		if i > start {
			token := expr[start:i]
			// Handle bare % at unitStart
			if i > unitStart && expr[unitStart:i] == "%" {
				// Keep the full token with %
			}
			tokens = append(tokens, token)
		} else {
			// Unknown character, skip
			i++
		}
	}
	return tokens
}

// Phase 2: Box model helpers

// BoxEdge represents the four sides of a box (top, right, bottom, left)
type BoxEdge struct {
	Top        float64
	Right      float64
	Bottom     float64
	Left       float64
	AutoTop    bool // True if margin-top: auto
	AutoRight  bool // True if margin-right: auto
	AutoBottom bool // True if margin-bottom: auto
	AutoLeft   bool // True if margin-left: auto
}

// GetMargin returns the margin values for all four sides
func (s *Style) GetMargin() BoxEdge {
	top, autoTop := s.getLengthOrAuto("margin-top")
	right, autoRight := s.getLengthOrAuto("margin-right")
	bottom, autoBottom := s.getLengthOrAuto("margin-bottom")
	left, autoLeft := s.getLengthOrAuto("margin-left")

	return BoxEdge{
		Top:        top,
		Right:      right,
		Bottom:     bottom,
		Left:       left,
		AutoTop:    autoTop,
		AutoRight:  autoRight,
		AutoBottom: autoBottom,
		AutoLeft:   autoLeft,
	}
}

// resolveMarginEdge resolves a single margin property, handling percentage values.
// Per CSS 2.1 §8.3, margin values CAN be negative — no clamp to zero.
func (s *Style) resolveMarginEdge(prop string, containingWidth float64) float64 {
	if val, ok := s.Get(prop); ok {
		if val == "auto" {
			return 0
		}
		trimmed := strings.TrimSpace(val)
		if strings.HasSuffix(trimmed, "%") {
			if pct, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, "%"), 64); err == nil {
				return pct / 100.0 * containingWidth
			}
		}
		// CSS Values §10.2: calc() with percent. Resolve against containingWidth.
		// Mirrors padding's resolvePaddingEdge but without the negative-clamp,
		// since margins are allowed to be negative.
		if IsCalcWithPercent(trimmed) {
			if v, ok := EvalCalcWithPercent(
				trimmed[5:len(trimmed)-1], s.GetFontSize(), containingWidth,
			); ok {
				return v
			}
		}
	}
	return s.getLengthOrZero(prop)
}

// GetMarginForWidth resolves margin values including percentage values against the given containing block width.
// Use this for inline elements where margin percentages must be resolved.
func (s *Style) GetMarginForWidth(containingWidth float64) BoxEdge {
	return BoxEdge{
		Top:    0, // Inline top/bottom margins ignored per CSS 2.1 §8.3
		Right:  s.resolveMarginEdge("margin-right", containingWidth),
		Bottom: 0, // Inline top/bottom margins ignored per CSS 2.1 §8.3
		Left:   s.resolveMarginEdge("margin-left", containingWidth),
	}
}

// GetAllMarginsForWidth resolves all four margin values including percentage values
// against the given containing block inline-size. Per CSS 2.1 §8.3, ALL margin
// percentages (including top/bottom) resolve against the containing block's width
// (inline-size in the containing block's writing mode).
func (s *Style) GetAllMarginsForWidth(containingWidth float64) BoxEdge {
	return BoxEdge{
		Top:    s.resolveMarginEdge("margin-top", containingWidth),
		Right:  s.resolveMarginEdge("margin-right", containingWidth),
		Bottom: s.resolveMarginEdge("margin-bottom", containingWidth),
		Left:   s.resolveMarginEdge("margin-left", containingWidth),
	}
}

// GetPadding returns the padding values for all four sides
func (s *Style) GetPadding() BoxEdge {
	return BoxEdge{
		Top:    s.getLengthOrZero("padding-top"),
		Right:  s.getLengthOrZero("padding-right"),
		Bottom: s.getLengthOrZero("padding-bottom"),
		Left:   s.getLengthOrZero("padding-left"),
	}
}

// GetPaddingForWidth resolves all four padding values including percentage values
// against the given containing block inline-size. Per CSS 2.1 §8.4, ALL padding
// percentages (including top/bottom) resolve against the containing block's width
// (inline-size in the containing block's writing mode).
func (s *Style) GetPaddingForWidth(containingWidth float64) BoxEdge {
	return BoxEdge{
		Top:    s.resolvePaddingEdge("padding-top", containingWidth),
		Right:  s.resolvePaddingEdge("padding-right", containingWidth),
		Bottom: s.resolvePaddingEdge("padding-bottom", containingWidth),
		Left:   s.resolvePaddingEdge("padding-left", containingWidth),
	}
}

// resolvePaddingEdge resolves a single padding property, handling percentage values.
// Padding cannot be negative (CSS 2.1 §8.4).
func (s *Style) resolvePaddingEdge(prop string, containingWidth float64) float64 {
	if val, ok := s.Get(prop); ok {
		trimmed := strings.TrimSpace(val)
		if strings.HasSuffix(trimmed, "%") {
			if pct, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, "%"), 64); err == nil {
				result := pct / 100.0 * containingWidth
				if result < 0 {
					return 0
				}
				return result
			}
		}
		// CSS Values §10.2: calc() with percent. Resolve against containingWidth.
		if IsCalcWithPercent(trimmed) {
			if v, ok := EvalCalcWithPercent(
				trimmed[5:len(trimmed)-1], s.GetFontSize(), containingWidth,
			); ok {
				if v < 0 {
					return 0
				}
				return v
			}
		}
	}
	result := s.getLengthOrZero(prop)
	if result < 0 {
		return 0
	}
	return result
}

// GetBorderWidth returns the border width for all four sides
func (s *Style) GetBorderWidth() BoxEdge {
	styles := s.GetBorderStyle()
	edge := BoxEdge{
		Top:    s.getLengthOrZero("border-top-width"),
		Right:  s.getLengthOrZero("border-right-width"),
		Bottom: s.getLengthOrZero("border-bottom-width"),
		Left:   s.getLengthOrZero("border-left-width"),
	}
	// CSS 2.1 §8.5.1: border-style:none computes border-width to 0
	if styles.Top == BorderStyleNone {
		edge.Top = 0
	}
	if styles.Right == BorderStyleNone {
		edge.Right = 0
	}
	if styles.Bottom == BorderStyleNone {
		edge.Bottom = 0
	}
	if styles.Left == BorderStyleNone {
		edge.Left = 0
	}
	return edge
}

// getLengthOrZero returns the length value or 0 if not found
func (s *Style) getLengthOrZero(property string) float64 {
	val, ok := s.GetLength(property)
	if !ok {
		return 0
	}
	return val
}

// getLengthOrAuto returns the length value and whether it's "auto"
// Returns (value, isAuto) where value is 0 if auto
func (s *Style) getLengthOrAuto(property string) (float64, bool) {
	if val, ok := s.Get(property); ok {
		if val == "auto" {
			return 0, true
		}
	}
	return s.getLengthOrZero(property), false
}

// Phase 12: Border styling

// BorderStyle represents the border-style property value
type BorderStyle string

const (
	BorderStyleNone   BorderStyle = "none"
	BorderStyleSolid  BorderStyle = "solid"
	BorderStyleDashed BorderStyle = "dashed"
	BorderStyleDotted BorderStyle = "dotted"
	BorderStyleDouble BorderStyle = "double"
)

// BorderStyleEdge represents border styles for all four sides
type BorderStyleEdge struct {
	Top    BorderStyle
	Right  BorderStyle
	Bottom BorderStyle
	Left   BorderStyle
}

// GetBorderStyle returns the border style for all four sides
func (s *Style) GetBorderStyle() BorderStyleEdge {
	return BorderStyleEdge{
		Top:    s.getBorderStyleSide("border-top-style"),
		Right:  s.getBorderStyleSide("border-right-style"),
		Bottom: s.getBorderStyleSide("border-bottom-style"),
		Left:   s.getBorderStyleSide("border-left-style"),
	}
}

// getBorderStyleSide returns the border style for a specific side (default: solid)
func (s *Style) getBorderStyleSide(property string) BorderStyle {
	if style, ok := s.Get(property); ok {
		switch style {
		case "none":
			return BorderStyleNone
		case "dashed":
			return BorderStyleDashed
		case "dotted":
			return BorderStyleDotted
		case "double":
			return BorderStyleDouble
		}
	}
	return BorderStyleSolid // Default to solid
}

// EllipticalRadius holds horizontal and vertical radii for a border corner.
type EllipticalRadius struct {
	Rx, Ry float64
}

// IsCircular returns true if both radii are equal.
func (e EllipticalRadius) IsCircular() bool { return e.Rx == e.Ry }

// IsZero returns true if both radii are zero.
func (e EllipticalRadius) IsZero() bool { return e.Rx == 0 && e.Ry == 0 }

// EllipticalRadii holds per-corner elliptical radii for a rounded rectangle.
// Order: [0]=TopLeft, [1]=TopRight, [2]=BottomRight, [3]=BottomLeft.
// Mirrors Blink's FloatRoundedRect::Radii.
type EllipticalRadii [4]EllipticalRadius

// IsZero returns true if all corners have zero radii.
func (r EllipticalRadii) IsZero() bool {
	return r[0].IsZero() && r[1].IsZero() && r[2].IsZero() && r[3].IsZero()
}

// IsCircular returns true if all corners have equal Rx and Ry.
func (r EllipticalRadii) IsCircular() bool {
	return r[0].IsCircular() && r[1].IsCircular() && r[2].IsCircular() && r[3].IsCircular()
}

// ConstrainRadii implements CSS Backgrounds 5.5 corner overlap reduction.
// If the sum of adjacent radii exceeds the corresponding side length,
// all radii are scaled down by a uniform factor.
func (r EllipticalRadii) ConstrainRadii(w, h float64) EllipticalRadii {
	if w <= 0 || h <= 0 {
		return EllipticalRadii{}
	}
	f := 1.0
	// Top edge: TL.Rx + TR.Rx vs width
	if s := r[0].Rx + r[1].Rx; s > 0 && w/s < f {
		f = w / s
	}
	// Bottom edge: BL.Rx + BR.Rx vs width
	if s := r[3].Rx + r[2].Rx; s > 0 && w/s < f {
		f = w / s
	}
	// Left edge: TL.Ry + BL.Ry vs height
	if s := r[0].Ry + r[3].Ry; s > 0 && h/s < f {
		f = h / s
	}
	// Right edge: TR.Ry + BR.Ry vs height
	if s := r[1].Ry + r[2].Ry; s > 0 && h/s < f {
		f = h / s
	}
	if f >= 1 {
		return r
	}
	var out EllipticalRadii
	for i := range r {
		out[i].Rx = r[i].Rx * f
		out[i].Ry = r[i].Ry * f
		// Per Blink: if either component is zero after scaling, zero both.
		if out[i].Rx <= 0 || out[i].Ry <= 0 {
			out[i] = EllipticalRadius{}
		}
	}
	return out
}

// Inset shrinks radii inward by the given amounts (Blink's Radii::Shrink).
// TL: Rx -= left, Ry -= top; TR: Rx -= right, Ry -= top;
// BR: Rx -= right, Ry -= bottom; BL: Rx -= left, Ry -= bottom.
// If either component <= 0, BOTH become 0. Zero radii stay zero.
func (r EllipticalRadii) Inset(top, right, bottom, left float64) EllipticalRadii {
	var out EllipticalRadii
	// TL
	if !r[0].IsZero() {
		out[0].Rx = r[0].Rx - left
		out[0].Ry = r[0].Ry - top
		if out[0].Rx <= 0 || out[0].Ry <= 0 {
			out[0] = EllipticalRadius{}
		}
	}
	// TR
	if !r[1].IsZero() {
		out[1].Rx = r[1].Rx - right
		out[1].Ry = r[1].Ry - top
		if out[1].Rx <= 0 || out[1].Ry <= 0 {
			out[1] = EllipticalRadius{}
		}
	}
	// BR
	if !r[2].IsZero() {
		out[2].Rx = r[2].Rx - right
		out[2].Ry = r[2].Ry - bottom
		if out[2].Rx <= 0 || out[2].Ry <= 0 {
			out[2] = EllipticalRadius{}
		}
	}
	// BL
	if !r[3].IsZero() {
		out[3].Rx = r[3].Rx - left
		out[3].Ry = r[3].Ry - bottom
		if out[3].Rx <= 0 || out[3].Ry <= 0 {
			out[3] = EllipticalRadius{}
		}
	}
	return out
}

// Outset expands radii outward by the given amounts (Blink's Radii::Outset).
// Only adjusts non-zero radii. Zero radii stay zero.
func (r EllipticalRadii) Outset(top, right, bottom, left float64) EllipticalRadii {
	var out EllipticalRadii
	for i := range r {
		out[i] = r[i]
	}
	if !r[0].IsZero() {
		out[0].Rx += left
		out[0].Ry += top
	}
	if !r[1].IsZero() {
		out[1].Rx += right
		out[1].Ry += top
	}
	if !r[2].IsZero() {
		out[2].Rx += right
		out[2].Ry += bottom
	}
	if !r[3].IsZero() {
		out[3].Rx += left
		out[3].Ry += bottom
	}
	return out
}

// adjustRadiusForSpread implements the CSS Backgrounds spec §5.4 shadow shape
// radius adjustment. When r >= spread, it's simply r + spread. When r < spread,
// a cubic interpolation is used: r + spread × (1 + (r/spread − 1)³).
// See https://www.w3.org/TR/css-backgrounds-3/#shadow-shape
func adjustRadiusForSpread(r, spread float64) float64 {
	if spread <= 0 || r <= 0 {
		return r + spread
	}
	if r >= spread {
		return r + spread
	}
	ratio := r / spread
	f := 1.0 + (ratio-1.0)*(ratio-1.0)*(ratio-1.0)
	return r + spread*f
}

// OutsetForBoxShadow expands radii outward using the CSS shadow shape formula.
// Unlike Outset (which uses simple addition), this applies the spec's cubic
// interpolation when a radius component is smaller than the spread.
func (r EllipticalRadii) OutsetForBoxShadow(top, right, bottom, left float64) EllipticalRadii {
	var out EllipticalRadii
	for i := range r {
		out[i] = r[i]
	}
	if !r[0].IsZero() {
		out[0].Rx = adjustRadiusForSpread(r[0].Rx, left)
		out[0].Ry = adjustRadiusForSpread(r[0].Ry, top)
	}
	if !r[1].IsZero() {
		out[1].Rx = adjustRadiusForSpread(r[1].Rx, right)
		out[1].Ry = adjustRadiusForSpread(r[1].Ry, top)
	}
	if !r[2].IsZero() {
		out[2].Rx = adjustRadiusForSpread(r[2].Rx, right)
		out[2].Ry = adjustRadiusForSpread(r[2].Ry, bottom)
	}
	if !r[3].IsZero() {
		out[3].Rx = adjustRadiusForSpread(r[3].Rx, left)
		out[3].Ry = adjustRadiusForSpread(r[3].Ry, bottom)
	}
	return out
}

// BorderRadiusCorners holds the radius for each corner of a box.
type BorderRadiusCorners struct {
	TopLeft     float64
	TopRight    float64
	BottomRight float64
	BottomLeft  float64
}

// IsUniform returns true if all corners have the same radius.
func (r BorderRadiusCorners) IsUniform() bool {
	return r.TopLeft == r.TopRight && r.TopRight == r.BottomRight && r.BottomRight == r.BottomLeft
}

// MaxRadius returns the largest corner radius.
func (r BorderRadiusCorners) MaxRadius() float64 {
	m := r.TopLeft
	if r.TopRight > m {
		m = r.TopRight
	}
	if r.BottomRight > m {
		m = r.BottomRight
	}
	if r.BottomLeft > m {
		m = r.BottomLeft
	}
	return m
}

// GetBorderRadius returns the border-radius value (simplified - single value for all corners)
func (s *Style) GetBorderRadius() float64 {
	corners := s.GetBorderRadiusCorners()
	return corners.MaxRadius()
}

// GetBorderRadiusCorners returns per-corner border-radius values.
func (s *Style) GetBorderRadiusCorners() BorderRadiusCorners {
	return s.GetBorderRadiusCornersResolved(0, 0)
}

// GetBorderRadiusCornersResolved returns per-corner border-radius values,
// resolving percentage values against the given box dimensions.
// Per CSS spec, horizontal radius percentages resolve against box width,
// vertical radius percentages resolve against box height.
// For the simplified (circular) case, we use the geometric mean approach:
// percentage is resolved as pct * sqrt(width*height) for consistency, but
// since this returns only horizontal radius, we resolve against width.
func (s *Style) GetBorderRadiusCornersResolved(boxWidth, boxHeight float64) BorderRadiusCorners {
	var corners BorderRadiusCorners

	// For percentage resolution of circular border-radius, the spec says:
	// horizontal radius: percentage of width; vertical radius: percentage of height.
	// For the simplified single-radius case, we use the respective dimension.
	// The diagonal reference (sqrt(w²+h²)/sqrt(2)) is NOT correct per spec.

	// Check individual corners first (higher specificity)
	if r := s.parseBorderRadiusFirstWithRef("border-top-left-radius", boxWidth); r > 0 {
		corners.TopLeft = r
	}
	if r := s.parseBorderRadiusFirstWithRef("border-top-right-radius", boxWidth); r > 0 {
		corners.TopRight = r
	}
	if r := s.parseBorderRadiusFirstWithRef("border-bottom-right-radius", boxWidth); r > 0 {
		corners.BottomRight = r
	}
	if r := s.parseBorderRadiusFirstWithRef("border-bottom-left-radius", boxWidth); r > 0 {
		corners.BottomLeft = r
	}

	// If any individual corner was set, return those values
	if corners.TopLeft > 0 || corners.TopRight > 0 || corners.BottomRight > 0 || corners.BottomLeft > 0 {
		return corners
	}

	// Fall back to shorthand border-radius (already expanded by expandShorthand)
	// Check for percentage in shorthand
	if val, ok := s.Get("border-radius"); ok {
		if pct, ok := ParsePercentage(val); ok {
			r := pct / 100 * boxWidth
			return BorderRadiusCorners{r, r, r, r}
		}
		if r, ok := s.GetLength("border-radius"); ok {
			return BorderRadiusCorners{r, r, r, r}
		}
	}

	return corners // all zeros
}

// parseBorderRadiusFirst parses a border-radius value, returning the first (horizontal) radius.
// Handles both single value "10px" and two-value "75px 50px" syntax.
func (s *Style) parseBorderRadiusFirst(property string) float64 {
	return s.parseBorderRadiusFirstWithRef(property, 0)
}

// parseBorderRadiusFirstWithRef parses a border-radius value with a reference length for percentage resolution.
// Per CSS spec: if either component is zero or negative, the corner is not rounded.
func (s *Style) parseBorderRadiusFirstWithRef(property string, refLen float64) float64 {
	val, ok := s.Get(property)
	if !ok {
		return 0
	}
	// splitShorthandParts is paren-aware; strings.Fields would mis-split
	// `calc(10px + 5%) 0` into `["calc(10px", "+", "5%)", "0"]`.
	parts := splitShorthandParts(val)

	// Two-value syntax: "25px 0" or "50px -25px" — check both components.
	if len(parts) == 2 {
		rx := parseBorderRadiusComponent(parts[0], s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight, refLen)
		ry := parseBorderRadiusComponent(parts[1], s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight, refLen)
		// If either component is zero or negative, the corner is sharp.
		// Negative values make the declaration invalid per CSS spec.
		if rx <= 0 || ry <= 0 {
			return 0
		}
		return rx
	}

	// Single value: try percentage, then length.
	if pct, ok := ParsePercentage(val); ok {
		r := pct / 100 * refLen
		if r <= 0 {
			return 0
		}
		return r
	}
	if r, ok := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok {
		if r <= 0 {
			return 0
		}
		return r
	}
	return 0
}

// parseBorderRadiusComponent parses a single border-radius component (length, percentage, or calc()).
// Returns a negative value for invalid/negative inputs so callers can detect it.
// For calc() with %, percent terms resolve against refLen (the relevant box
// dimension — width for Rx, height for Ry).
func parseBorderRadiusComponent(val string, fontSize, vw, vh, chScale, xHeight, refLen float64) float64 {
	val = strings.TrimSpace(val)
	if pct, ok := ParsePercentage(val); ok {
		return pct / 100 * refLen
	}
	// CSS Values §10.2: calc() with percent resolves against refLen.
	if IsCalcWithPercent(val) {
		ctx := calcContext{
			fontSize:       fontSize,
			percentBase:    refLen,
			viewportWidth:  vw,
			viewportHeight: vh,
			chScale:        chScale,
			xHeight:        xHeight,
		}
		if r, ok := evalCalcFull(val[5:len(val)-1], ctx); ok {
			return r
		}
	}
	if r, ok := parseLengthFullWithCh(val, fontSize, vw, vh, chScale, xHeight); ok {
		return r
	}
	return -1 // unparseable = invalid
}

// GetBorderRadiusCornersElliptical returns per-corner elliptical border-radius values.
// Each corner may have different horizontal and vertical radii.
func (s *Style) GetBorderRadiusCornersElliptical() [4]EllipticalRadius {
	var corners [4]EllipticalRadius
	props := [4]string{
		"border-top-left-radius", "border-top-right-radius",
		"border-bottom-right-radius", "border-bottom-left-radius",
	}
	for i, prop := range props {
		corners[i] = s.parseBorderRadiusElliptical(prop)
	}
	// Check if any was set
	anySet := false
	for _, c := range corners {
		if c.Rx > 0 || c.Ry > 0 {
			anySet = true
			break
		}
	}
	if !anySet {
		if r, ok := s.GetLength("border-radius"); ok {
			for i := range corners {
				corners[i] = EllipticalRadius{r, r}
			}
		}
	}
	return corners
}

// HasEllipticalBorderRadius returns true if any corner has different Rx and Ry.
func (s *Style) HasEllipticalBorderRadius() bool {
	corners := s.GetBorderRadiusCornersElliptical()
	for _, c := range corners {
		if c.Rx != c.Ry && (c.Rx > 0 || c.Ry > 0) {
			return true
		}
	}
	return false
}

func (s *Style) parseBorderRadiusElliptical(property string) EllipticalRadius {
	val, ok := s.Get(property)
	if !ok {
		return EllipticalRadius{}
	}
	// Try as single value first
	if r, ok := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok {
		return EllipticalRadius{r, r}
	}
	// Two-value syntax: "75px 50px" or "calc(...) calc(...)" (paren-aware).
	parts := splitShorthandParts(val)
	var result EllipticalRadius
	if len(parts) >= 1 {
		if r, ok := parseLengthFullWithCh(parts[0], s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok {
			result.Rx = r
			result.Ry = r // default same
		}
	}
	if len(parts) >= 2 {
		if r, ok := parseLengthFullWithCh(parts[1], s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok {
			result.Ry = r
		}
	}
	return result
}

// GetBorderRadiiResolved returns per-corner elliptical border-radius values,
// resolving percentages: Rx% against boxWidth, Ry% against boxHeight.
// Per CSS spec, negative values and values where either component is zero
// result in a sharp (zero) corner.
func (s *Style) GetBorderRadiiResolved(boxWidth, boxHeight float64) EllipticalRadii {
	var radii EllipticalRadii
	props := [4]string{
		"border-top-left-radius", "border-top-right-radius",
		"border-bottom-right-radius", "border-bottom-left-radius",
	}
	for i, prop := range props {
		radii[i] = s.parseBorderRadiusEllipticalResolved(prop, boxWidth, boxHeight)
	}
	// Check if any was set via individual properties
	anySet := false
	for _, c := range radii {
		if !c.IsZero() {
			anySet = true
			break
		}
	}
	if !anySet {
		// Fall back to shorthand border-radius
		if val, ok := s.Get("border-radius"); ok {
			rx := parseBorderRadiusComponent(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight, boxWidth)
			if rx > 0 {
				// Single-value shorthand: same Rx and Ry
				// For percentage, Ry resolves against height
				ry := parseBorderRadiusComponent(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight, boxHeight)
				if ry > 0 {
					e := EllipticalRadius{rx, ry}
					return EllipticalRadii{e, e, e, e}
				}
			}
		}
	}
	return radii
}

// parseBorderRadiusEllipticalResolved parses a per-corner border-radius value
// with percentage resolution against the given box dimensions.
func (s *Style) parseBorderRadiusEllipticalResolved(property string, boxWidth, boxHeight float64) EllipticalRadius {
	val, ok := s.Get(property)
	if !ok {
		return EllipticalRadius{}
	}
	// Paren-aware split: `calc(10px + 5%) calc(...)` would be mis-split by strings.Fields.
	parts := splitShorthandParts(val)

	fs := s.GetFontSize()
	vw, vh, ch, xh := s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight

	if len(parts) == 2 {
		// Two-value syntax: "25px 50%" or "50px -25px"
		rx := parseBorderRadiusComponent(parts[0], fs, vw, vh, ch, xh, boxWidth)
		ry := parseBorderRadiusComponent(parts[1], fs, vw, vh, ch, xh, boxHeight)
		// If either component is zero or negative, corner is sharp
		if rx <= 0 || ry <= 0 {
			return EllipticalRadius{}
		}
		return EllipticalRadius{rx, ry}
	}

	// Single value: resolves against both dimensions
	rx := parseBorderRadiusComponent(val, fs, vw, vh, ch, xh, boxWidth)
	ry := parseBorderRadiusComponent(val, fs, vw, vh, ch, xh, boxHeight)
	if rx <= 0 || ry <= 0 {
		return EllipticalRadius{}
	}
	return EllipticalRadius{rx, ry}
}

// GetMaxWidth returns the max-width value if set
func (s *Style) GetMaxWidth() (float64, bool) {
	return s.GetLength("max-width")
}

// Phase 4: Positioning helpers

// Position type constants
type PositionType string

const (
	PositionStatic   PositionType = "static"
	PositionRelative PositionType = "relative"
	PositionAbsolute PositionType = "absolute"
	PositionFixed    PositionType = "fixed"
	PositionSticky   PositionType = "sticky"
)

// GetPosition returns the position type (default: static)
func (s *Style) GetPosition() PositionType {
	pos, ok := s.Get("position")
	if !ok {
		return PositionStatic
	}
	switch pos {
	case "relative":
		return PositionRelative
	case "absolute":
		return PositionAbsolute
	case "fixed":
		return PositionFixed
	case "sticky":
		return PositionSticky
	default:
		return PositionStatic
	}
}

// GetPositionOffset returns the offset values for positioned elements
type PositionOffset struct {
	Top       float64
	Right     float64
	Bottom    float64
	Left      float64
	HasTop    bool
	HasRight  bool
	HasBottom bool
	HasLeft   bool
}

// GetPositionOffset returns positioning offset values.
// Percentage values are not resolved (use GetPositionOffsetResolved for that).
func (s *Style) GetPositionOffset() PositionOffset {
	return s.GetPositionOffsetResolved(0, 0)
}

// GetPositionOffsetResolved returns positioning offset values with percentage
// resolution. For top/bottom, percentages resolve against cbHeight. For
// left/right, percentages resolve against cbWidth.
func (s *Style) GetPositionOffsetResolved(cbWidth, cbHeight float64) PositionOffset {
	offset := PositionOffset{}
	offset.Top, offset.HasTop = s.resolveLengthOrPercent("top", cbHeight)
	offset.Right, offset.HasRight = s.resolveLengthOrPercent("right", cbWidth)
	offset.Bottom, offset.HasBottom = s.resolveLengthOrPercent("bottom", cbHeight)
	offset.Left, offset.HasLeft = s.resolveLengthOrPercent("left", cbWidth)
	return offset
}

// resolveLengthOrPercent tries to parse a property as a length or percentage.
// If it's a percentage, it's resolved against the given reference value.
func (s *Style) resolveLengthOrPercent(property string, reference float64) (float64, bool) {
	val, ok := s.Get(property)
	if !ok || val == "auto" {
		return 0, false
	}
	// Handle calc() with percentage terms using the correct percentage base.
	// Pass full context (viewport, ch scale) so that ch/vw/vh units resolve correctly.
	if IsCalcWithPercent(val) {
		ctx := calcContext{
			fontSize:       s.GetFontSize(),
			percentBase:    reference,
			viewportWidth:  s.ViewportWidth,
			viewportHeight: s.ViewportHeight,
			chScale:        s.chScale(),
		}
		if result, calcOK := evalCalcFull(val[5:len(val)-1], ctx); calcOK {
			return result, true
		}
	}
	if length, ok := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok {
		return length, true
	}
	if pct, ok := ParsePercentage(val); ok {
		return pct / 100.0 * reference, true
	}
	return 0, false
}

// GetZIndex returns the z-index value (default: 0)
func (s *Style) GetZIndex() int {
	if zindex, ok := s.Get("z-index"); ok {
		// Simple integer parsing
		var z int
		if _, err := fmt.Sscanf(zindex, "%d", &z); err == nil {
			return z
		}
	}
	return 0
}

// HasExplicitZIndex returns true if z-index is set to an integer value
// (not "auto" or unset). Only positioned elements with explicit z-index
// create new stacking contexts.
func (s *Style) HasExplicitZIndex() bool {
	zindex, ok := s.Get("z-index")
	if !ok || zindex == "auto" || zindex == "" {
		return false
	}
	var z int
	_, err := fmt.Sscanf(zindex, "%d", &z)
	return err == nil
}

// stripImportantSuffix returns (value-without-!important, true) if value has a
// trailing "!important" token; otherwise returns (value, false). Used by
// shorthand expanders so the parser can ignore the priority suffix in the
// value-token stream. The actual cascade-priority bookkeeping is handled by
// the per-rule importantProps tracking in cascade.go and by the rule parser's
// Rule.Important map (cascade.go:466). For inline styles we currently treat
// !important as best-effort: the suffix is stripped so the longhand value
// parses correctly, but the priority is not yet tracked separately from the
// inline ordering. This matches the pre-Phase-1 behavior for shorthand
// properties (e.g. `text-decoration: underline !important`).
func stripImportantSuffix(value string) (string, bool) {
	v := strings.TrimSpace(value)
	if len(v) < len("!important") {
		return value, false
	}
	// Case-insensitive trailing match.
	tail := v[len(v)-len("!important"):]
	if strings.EqualFold(tail, "!important") {
		return strings.TrimSpace(v[:len(v)-len("!important")]), true
	}
	return value, false
}

// ParseInlineStyle parses a `style=""` attribute's contents. ctx carries
// the owning document's BaseDir; every url() token in the value is rewritten
// to its absolute form against ctx before being stored in Style.Properties.
// Pass nil for "no base URL" (test fixtures that don't exercise URL composition).
func ParseInlineStyle(styleAttr string, ctx *ParserContext) *Style {
	style := NewStyle()
	// Resolve every `url(...)` token against ctx.BaseDir up front — matches
	// the Blink invariant that every url() consumed during parse goes
	// through CSSParserContext::CompleteURL. See ParseStylesheet for the
	// rationale on rewriting at the source-text level.
	styleAttr = ctx.RewriteURLs(styleAttr)
	// Use the same paren/quote-aware splitter the stylesheet parser uses.
	// A naive strings.Split(";") mangles values like
	// `filter: url("data:image/svg+xml;utf8,...")` at the `;` inside the
	// data-URL MIME prefix.
	declarations := splitDeclarationParts(styleAttr)
	for _, decl := range declarations {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			continue
		}
		property := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		// Strip the !important suffix before parsing. Inline-style priority is
		// not tracked separately from inline-normal in cascade.go today (the
		// note at cascade.go:494 acknowledges this), so the suffix must be
		// removed from the value or it leaks into longhand parsers. Mirrors
		// the rule-parser path at stylesheet.go:1917-1929 which has always
		// stripped !important.
		if stripped, _ := stripImportantSuffix(value); stripped != value {
			value = stripped
		}

		// CSS Custom Properties §2 / §3: validate name + value (mirrors the
		// rule-parser gate in parseDeclarations).
		if strings.HasPrefix(property, "--") {
			if !isValidCustomPropertyName(property) || !isValidCustomPropertyValue(value) {
				continue
			}
		}

		// Phase 2: Expand shorthand properties
		expandShorthand(style, property, value)
	}
	return style
}

// normalizeVendorPrefixedValue resolves common vendor-prefixed length/width keywords
// to their standard CSS equivalents.
func normalizeVendorPrefixedValue(val string) string {
	lower := strings.ToLower(strings.TrimSpace(val))
	switch lower {
	case "-webkit-fill-available", "-moz-available", "-webkit-stretch", "stretch":
		return "100%"
	case "-webkit-fit-content", "-moz-fit-content":
		return "fit-content"
	case "-webkit-max-content", "-moz-max-content":
		return "max-content"
	case "-webkit-min-content", "-moz-min-content":
		return "min-content"
	}
	return val
}

// expandShorthand expands shorthand CSS properties into individual properties
func expandShorthand(style *Style, property, value string) {
	// Custom properties (--*) are never shorthands — store as-is
	if strings.HasPrefix(property, "--") {
		style.Set(property, value)
		return
	}
	// If value contains var(), skip shorthand expansion for most properties
	// (var() will be resolved later when the property is read)
	if strings.Contains(value, "var(") {
		switch property {
		case "margin", "padding", "border", "border-top", "border-right",
			"border-bottom", "border-left", "border-width", "border-style",
			"border-color", "font", "flex", "flex-flow", "list-style", "gap":
			// Store as the shorthand property — var() resolved at read time
			style.Set(property, value)
			return
		}
		// For "background", let expandBackgroundProperty handle it (has its own var() check)
	}
	switch property {
	case "margin":
		// margin: 10px -> margin-top/right/bottom/left: 10px
		expandBoxProperty(style, "margin", value)
	case "padding":
		// padding: 10px -> padding-top/right/bottom/left: 10px
		expandBoxProperty(style, "padding", value)
	case "border":
		// border: 1px solid black -> border-width/style/color
		expandBorderProperty(style, value)
	case "border-top", "border-right", "border-bottom", "border-left":
		expandBorderSideProperty(style, property, value)
	case "border-width":
		expandBorderBoxProperty(style, value, "width")
	case "border-style":
		expandBorderBoxProperty(style, value, "style")
	case "border-color":
		expandBorderBoxProperty(style, value, "color")
	case "border-radius":
		expandBorderRadiusProperty(style, value)
	case "background":
		expandBackgroundProperty(style, value)
	case "font":
		expandFontProperty(style, value)
	case "flex":
		expandFlexProperty(style, value)
	case "flex-flow":
		expandFlexFlowProperty(style, value)
	case "list-style":
		// list-style shorthand: sets list-style-type, list-style-position, list-style-image
		// Parse the shorthand by tokenizing and categorizing each component.
		// Positions: inside, outside. Types: none, disc, circle, square, decimal, etc.
		// Image: url(...) or none.
		// Tokenize preserving url(...) as a single token.
		var listTokens []string
		rest := value
		for rest != "" {
			rest = strings.TrimSpace(rest)
			if rest == "" {
				break
			}
			if strings.HasPrefix(rest, "url(") {
				// Find matching close paren, respecting quotes.
				depth := 0
				inQ := byte(0)
				i := 0
				for i < len(rest) {
					ch := rest[i]
					if inQ != 0 {
						if ch == inQ {
							inQ = 0
						}
					} else if ch == '\'' || ch == '"' {
						inQ = ch
					} else if ch == '(' {
						depth++
					} else if ch == ')' {
						depth--
						if depth == 0 {
							i++
							break
						}
					}
					i++
				}
				listTokens = append(listTokens, rest[:i])
				rest = rest[i:]
			} else {
				idx := strings.IndexByte(rest, ' ')
				if idx < 0 {
					listTokens = append(listTokens, rest)
					rest = ""
				} else {
					listTokens = append(listTokens, rest[:idx])
					rest = rest[idx+1:]
				}
			}
		}
		foundType := false
		foundPos := false
		foundImage := false
		for _, tok := range listTokens {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if strings.HasPrefix(tok, "url(") {
				style.Set("list-style-image", tok)
				foundImage = true
			} else if tok == "inside" || tok == "outside" {
				style.Set("list-style-position", tok)
				foundPos = true
			} else if tok == "none" {
				if !foundType {
					style.Set("list-style-type", "none")
					foundType = true
				} else if !foundImage {
					style.Set("list-style-image", "none")
					foundImage = true
				}
			} else {
				style.Set("list-style-type", tok)
				foundType = true
			}
		}
		_ = foundPos
	case "gap":
		// gap shorthand: sets both row-gap and column-gap
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("row-gap", parts[0])
			style.Set("column-gap", parts[0])
		} else if len(parts) == 2 {
			style.Set("row-gap", parts[0])
			style.Set("column-gap", parts[1])
		}
	case "place-items":
		// place-items shorthand: <align-items> <justify-items>
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("align-items", parts[0])
			style.Set("justify-items", parts[0])
		} else if len(parts) >= 2 {
			style.Set("align-items", parts[0])
			style.Set("justify-items", parts[1])
		}
	case "place-content":
		// place-content shorthand: <align-content> <justify-content>
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("align-content", parts[0])
			style.Set("justify-content", parts[0])
		} else if len(parts) >= 2 {
			style.Set("align-content", parts[0])
			style.Set("justify-content", parts[1])
		}
	case "place-self":
		// place-self shorthand: <align-self> <justify-self>
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("align-self", parts[0])
			style.Set("justify-self", parts[0])
		} else if len(parts) >= 2 {
			style.Set("align-self", parts[0])
			style.Set("justify-self", parts[1])
		}
	case "columns":
		// columns shorthand. CSS Multicol L1: <column-width> || <column-count>.
		// CSS Multicol L2 §4.1 extends this with an optional `/ <column-height>`
		// suffix: `columns: 2 / 100px`, `columns: 20em auto / 200px`, etc.
		// Split on `/` first: the segment after the `/` is column-height (any
		// length or `auto`). The segment before is the L1 shorthand.
		headValue, heightValue, hasSlash := strings.Cut(value, "/")
		for _, part := range strings.Fields(headValue) {
			if part == "auto" {
				continue
			}
			if _, err := strconv.Atoi(part); err == nil {
				style.Set("column-count", part)
			} else {
				style.Set("column-width", part)
			}
		}
		if hasSlash {
			h := strings.TrimSpace(heightValue)
			if h != "" {
				style.Set("column-height", h)
			}
		}
	case "overflow":
		// overflow shorthand: 1 or 2 values for overflow-x and overflow-y
		parts := strings.Fields(value)
		if len(parts) == 2 {
			style.Set("overflow-x", parts[0])
			style.Set("overflow-y", parts[1])
			style.Set("overflow", parts[0]) // fallback for GetOverflow()
		} else {
			style.Set("overflow", value)
			style.Set("overflow-x", value)
			style.Set("overflow-y", value)
		}
	// CSS Logical Properties — resolve to physical properties
	// Assumes horizontal-tb writing mode (default) with LTR direction
	case "margin-inline-start":
		// Store under marker; resolved by resolveLogicalBoxProperties based on writing-mode
		style.Set("margin-left", value)
		style.Set("_margin-inline-start", value)
	case "margin-inline-end":
		style.Set("margin-right", value)
		style.Set("_margin-inline-end", value)
	case "margin-block-start":
		style.Set("margin-top", value)
		style.Set("_margin-block-start", value)
	case "margin-block-end":
		style.Set("margin-bottom", value)
		style.Set("_margin-block-end", value)
	case "margin-inline":
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("margin-left", parts[0])
			style.Set("margin-right", parts[0])
			style.Set("_margin-inline-start", parts[0])
			style.Set("_margin-inline-end", parts[0])
		} else if len(parts) >= 2 {
			style.Set("margin-left", parts[0])
			style.Set("margin-right", parts[1])
			style.Set("_margin-inline-start", parts[0])
			style.Set("_margin-inline-end", parts[1])
		}
	case "margin-block":
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("margin-top", parts[0])
			style.Set("margin-bottom", parts[0])
			style.Set("_margin-block-start", parts[0])
			style.Set("_margin-block-end", parts[0])
		} else if len(parts) >= 2 {
			style.Set("margin-top", parts[0])
			style.Set("margin-bottom", parts[1])
			style.Set("_margin-block-start", parts[0])
			style.Set("_margin-block-end", parts[1])
		}
	case "padding-inline-start":
		style.Set("padding-left", value)
		style.Set("_padding-inline-start", value)
	case "padding-inline-end":
		style.Set("padding-right", value)
		style.Set("_padding-inline-end", value)
	case "padding-block-start":
		style.Set("padding-top", value)
		style.Set("_padding-block-start", value)
	case "padding-block-end":
		style.Set("padding-bottom", value)
		style.Set("_padding-block-end", value)
	case "padding-inline":
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("padding-left", parts[0])
			style.Set("padding-right", parts[0])
			style.Set("_padding-inline-start", parts[0])
			style.Set("_padding-inline-end", parts[0])
		} else if len(parts) >= 2 {
			style.Set("padding-left", parts[0])
			style.Set("padding-right", parts[1])
			style.Set("_padding-inline-start", parts[0])
			style.Set("_padding-inline-end", parts[1])
		}
	case "padding-block":
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("padding-top", parts[0])
			style.Set("padding-bottom", parts[0])
			style.Set("_padding-block-start", parts[0])
			style.Set("_padding-block-end", parts[0])
		} else if len(parts) >= 2 {
			style.Set("padding-top", parts[0])
			style.Set("padding-bottom", parts[1])
			style.Set("_padding-block-start", parts[0])
			style.Set("_padding-block-end", parts[1])
		}
	case "border-inline":
		storeBorderLogicalMarker(style, "_border-inline-start", value)
		storeBorderLogicalMarker(style, "_border-inline-end", value)
	case "border-block":
		storeBorderLogicalMarker(style, "_border-block-start", value)
		storeBorderLogicalMarker(style, "_border-block-end", value)
	case "border-inline-start":
		storeBorderLogicalMarker(style, "_border-inline-start", value)
	case "border-inline-end":
		storeBorderLogicalMarker(style, "_border-inline-end", value)
	case "border-block-start":
		storeBorderLogicalMarker(style, "_border-block-start", value)
	case "border-block-end":
		storeBorderLogicalMarker(style, "_border-block-end", value)
	case "border-inline-start-width":
		style.Set("_border-inline-start-width", value)
	case "border-inline-end-width":
		style.Set("_border-inline-end-width", value)
	case "border-block-start-width":
		style.Set("_border-block-start-width", value)
	case "border-block-end-width":
		style.Set("_border-block-end-width", value)
	case "border-inline-start-style":
		style.Set("_border-inline-start-style", value)
	case "border-inline-end-style":
		style.Set("_border-inline-end-style", value)
	case "border-block-start-style":
		style.Set("_border-block-start-style", value)
	case "border-block-end-style":
		style.Set("_border-block-end-style", value)
	case "border-inline-start-color":
		style.Set("_border-inline-start-color", value)
	case "border-inline-end-color":
		style.Set("_border-inline-end-color", value)
	case "border-block-start-color":
		style.Set("_border-block-start-color", value)
	case "border-block-end-color":
		style.Set("_border-block-end-color", value)
	case "inset-inline-start":
		style.Set("left", value)
		style.Set("_inset-inline-start", value)
	case "inset-inline-end":
		style.Set("right", value)
		style.Set("_inset-inline-end", value)
	case "inset-block-start":
		style.Set("top", value)
		style.Set("_inset-block-start", value)
	case "inset-block-end":
		style.Set("bottom", value)
		style.Set("_inset-block-end", value)
	case "inset-inline":
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("left", parts[0])
			style.Set("right", parts[0])
		} else if len(parts) >= 2 {
			style.Set("left", parts[0])
			style.Set("right", parts[1])
		}
	case "inset-block":
		parts := strings.Fields(value)
		if len(parts) == 1 {
			style.Set("top", parts[0])
			style.Set("bottom", parts[0])
		} else if len(parts) >= 2 {
			style.Set("top", parts[0])
			style.Set("bottom", parts[1])
		}
	case "inset":
		parts := strings.Fields(value)
		switch len(parts) {
		case 1:
			style.Set("top", parts[0])
			style.Set("right", parts[0])
			style.Set("bottom", parts[0])
			style.Set("left", parts[0])
		case 2:
			style.Set("top", parts[0])
			style.Set("right", parts[1])
			style.Set("bottom", parts[0])
			style.Set("left", parts[1])
		case 3:
			style.Set("top", parts[0])
			style.Set("right", parts[1])
			style.Set("bottom", parts[2])
			style.Set("left", parts[1])
		default:
			style.Set("top", parts[0])
			style.Set("right", parts[1])
			style.Set("bottom", parts[2])
			style.Set("left", parts[3])
		}
	case "inline-size":
		// Default mapping: inline-size → width for horizontal-tb.
		// For vertical writing modes, resolveLogicalSizeProperties re-maps to height.
		style.Set("width", normalizeVendorPrefixedValue(value))
		style.Set("_inline-size", normalizeVendorPrefixedValue(value))
	case "block-size":
		style.Set("height", normalizeVendorPrefixedValue(value))
		style.Set("_block-size", normalizeVendorPrefixedValue(value))
	case "min-inline-size":
		style.Set("min-width", normalizeVendorPrefixedValue(value))
		style.Set("_min-inline-size", normalizeVendorPrefixedValue(value))
	case "min-block-size":
		style.Set("min-height", normalizeVendorPrefixedValue(value))
		style.Set("_min-block-size", normalizeVendorPrefixedValue(value))
	case "max-inline-size":
		style.Set("max-width", normalizeVendorPrefixedValue(value))
		style.Set("_max-inline-size", normalizeVendorPrefixedValue(value))
	case "max-block-size":
		style.Set("max-height", normalizeVendorPrefixedValue(value))
		style.Set("_max-block-size", normalizeVendorPrefixedValue(value))
	case "outline":
		expandOutlineShorthand(style, value)
	case "column-rule":
		expandColumnRuleShorthand(style, value)
	case "grid-template":
		// grid-template shorthand: <row-tracks> / <col-tracks>
		// Also handles: grid-template: none
		if value == "none" {
			style.Set("grid-template-rows", "none")
			style.Set("grid-template-columns", "none")
			style.Set("grid-template-areas", "none")
		} else if slashIdx := strings.Index(value, " / "); slashIdx >= 0 {
			rowPart := strings.TrimSpace(value[:slashIdx])
			colPart := strings.TrimSpace(value[slashIdx+3:])
			style.Set("grid-template-rows", rowPart)
			style.Set("grid-template-columns", colPart)
		} else {
			style.Set("grid-template-rows", value)
		}
	case "transition":
		// Store transition value as-is; static renderer ignores it
		style.Set("transition", value)
	case "animation":
		// CSS Animations §4: decompose the `animation` shorthand into its 8
		// longhands so the keyframe-application pass (pkg/css/animation.go) can
		// read them. The raw `animation` key is intentionally NOT stored: doing
		// so would let the cascade re-expand it and race a same-rule
		// animation-* longhand declaration (declaration order is lost once a
		// rule's declarations are a map).
		//
		// Two value kinds must NOT be tokenized as a shorthand list:
		//   - a CSS-wide keyword applies to every animation-* longhand;
		//   - an unresolved var() can't be decomposed (doing so would freeze
		//     the non-name longhands to defaults before var() resolution).
		switch trimmed := strings.ToLower(strings.TrimSpace(value)); trimmed {
		case "inherit", "initial", "unset", "revert", "revert-layer":
			for _, lh := range []string{"animation-name", "animation-duration",
				"animation-timing-function", "animation-delay",
				"animation-iteration-count", "animation-direction",
				"animation-fill-mode", "animation-play-state"} {
				style.Set(lh, trimmed)
			}
		default:
			if strings.Contains(value, "var(") {
				style.Set("animation", value)
			} else {
				expandAnimationShorthand(style, value)
			}
		}
	case "transition-property", "transition-duration", "transition-timing-function", "transition-delay",
		"transition-behavior":
		style.Set(property, value)
	case "animation-name", "animation-duration", "animation-timing-function", "animation-delay",
		"animation-iteration-count", "animation-direction", "animation-fill-mode", "animation-play-state",
		"animation-range", "animation-timeline":
		style.Set(property, value)
	case "text-emphasis":
		// text-emphasis shorthand: <style> || <color>
		// Either component may appear first; color is detected by ParseColor.
		parts := strings.Fields(value)
		if len(parts) == 0 {
			break
		}
		// Collect non-color parts as style, color part as color
		var styleParts []string
		colorSet := false
		for _, p := range parts {
			if !colorSet {
				if _, ok := ParseColor(p); ok {
					style.Set("text-emphasis-color", p)
					colorSet = true
					continue
				}
			}
			styleParts = append(styleParts, p)
		}
		if len(styleParts) > 0 {
			style.Set("text-emphasis-style", strings.Join(styleParts, " "))
		}
	case "width", "height", "min-width", "max-width", "min-height", "max-height", "flex-basis":
		// Normalize vendor-prefixed size keywords before storing
		style.Set(property, normalizeVendorPrefixedValue(value))
	case "border-image":
		// border-image shorthand: <source> [<slice> [ / <width> [ / <outset> ]] ] <repeat>
		// We split on "/" to separate source+slice from width from outset+repeat.
		// Find the source (url() or gradient function) first.
		v := strings.TrimSpace(value)
		// Split into slash-separated parts (but be careful of slashes inside parens)
		slashParts := splitBorderImageSlashes(v)
		// First part contains source, possibly followed by slice
		if len(slashParts) >= 1 {
			firstPart := strings.TrimSpace(slashParts[0])
			// Identify where the source image ends and slice begins.
			// Source is a url() or *-gradient(...) function.
			srcEnd := findBorderImageSourceEnd(firstPart)
			src := strings.TrimSpace(firstPart[:srcEnd])
			rest := strings.TrimSpace(firstPart[srcEnd:])
			if src == "" {
				src = "none"
			}
			style.Set("border-image-source", src)
			if rest != "" {
				style.Set("border-image-slice", rest)
			}
		}
		if len(slashParts) >= 2 {
			style.Set("border-image-width", strings.TrimSpace(slashParts[1]))
		}
		if len(slashParts) >= 3 {
			// Could be "outset repeat" or just "outset"
			last := strings.TrimSpace(slashParts[2])
			parts := strings.Fields(last)
			if len(parts) >= 1 {
				style.Set("border-image-outset", parts[0])
			}
			if len(parts) >= 2 {
				style.Set("border-image-repeat", strings.Join(parts[1:], " "))
			}
		}
	case "text-decoration":
		// CSS Text Decor 4 §2.6: text-decoration is a shorthand for:
		//   text-decoration-line, text-decoration-style,
		//   text-decoration-color, text-decoration-thickness.
		// Each omitted component is reset to its initial value.
		// Mirrors Blink at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		expandTextDecorationShorthand(style, value)
	case "font-synthesis":
		// CSS Fonts 4 §6.5: font-synthesis is a shorthand for:
		//   font-synthesis-weight, font-synthesis-style,
		//   font-synthesis-small-caps, font-synthesis-position.
		// Grammar: none | [ weight || style || small-caps || position ].
		// Any longhand not listed in the value is set to `none`; `none`
		// alone sets all four to `none`. Mirrors Blink's
		// `font_synthesis.cc::ParseShorthand` at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		expandFontSynthesisShorthand(style, value)
	case "all":
		// CSS Cascade 3 §8 / CSS Cascade 4 §6.3: the `all` shorthand resets
		// every longhand CSS property to the supplied CSS-wide keyword, with
		// three exclusions: `direction`, `unicode-bidi`, and custom properties
		// (--*). Mirrors Blink's all.cc expansion at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which iterates the longhand
		// table via CSSPropertyIDIterator with these same exclusions.
		expandAllShorthand(style, value)
	default:
		// Regular property
		style.Set(property, value)
	}
}

// expandAllShorthand implements the `all` CSS shorthand per CSS Cascade 3 §8
// and CSS Cascade 4 §6.3. The shorthand expands to every longhand CSS property
// except `direction`, `unicode-bidi`, and custom properties (--*). Accepted
// values are the CSS-wide keywords: initial, inherit, unset, revert,
// revert-layer.
//
// Implementation strategy per keyword:
//   - initial: delete each longhand from the style map so the property's
//     getter fallback returns its initial value.
//   - inherit: store the literal "inherit" string for each longhand so the
//     post-cascade resolveInheritValues pass copies the parent's value.
//   - unset: behaves as `inherit` for inherited properties, as `initial`
//     otherwise. The inherited set is louis14's existing inheritableProperties
//     map (the source-of-truth for inheritability used by ApplyInheritedProperties).
//   - revert / revert-layer: store the literal "revert" string. ComputeStyle
//     takes a UA+presentational-attribute snapshot before stylesheet rules run;
//     a post-cascade pass restores those snapshot values for any property whose
//     final value is "revert" (and deletes the property otherwise). This gives
//     `revert` its spec semantics of rolling back to the prior cascade origin,
//     which for author-origin declarations is the UA result.
//
// Mirrors Blink's properties/shorthands/all.cc at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which iterates CSSPropertyIDIterator
// over every longhand and skips kDirection, kUnicodeBidi, and custom properties.
func expandAllShorthand(style *Style, value string) {
	keyword := strings.ToLower(strings.TrimSpace(value))
	switch keyword {
	case "initial", "inherit", "unset", "revert", "revert-layer":
		// valid CSS-wide keywords — fall through to expansion below.
	default:
		// Per spec, the `all` shorthand only accepts CSS-wide keywords; any
		// other value (including var()) is invalid and the declaration is
		// dropped. Mirrors Blink's CSSPropertyParser::ParseSingleValue check.
		return
	}
	for _, prop := range cssLonghandProperties {
		// Exclusions per CSS Cascade 3 §8:
		//   - direction, unicode-bidi (their initial values are based on
		//     the document language and unicode-bidi context, not on CSS).
		//   - custom properties (--*) — filtered structurally elsewhere
		//     since the longhand list never contains them.
		if prop == "direction" || prop == "unicode-bidi" {
			continue
		}
		switch keyword {
		case "initial":
			if init, ok := nonDefaultInitialValues[prop]; ok {
				// Some longhands have a CSS-spec initial value that differs
				// from louis14's getter fallback (e.g. `display` initial is
				// `inline` per CSS Display §2, but GetDisplay returns
				// DisplayBlock when the property is absent). Setting the
				// canonical initial keeps `all: initial` spec-conformant
				// rather than relying on the absence-of-property fallback.
				style.Set(prop, init)
			} else {
				delete(style.Properties, prop)
			}
		case "inherit":
			style.Set(prop, "inherit")
		case "unset":
			if inheritableProperties[prop] {
				style.Set(prop, "inherit")
			} else if init, ok := nonDefaultInitialValues[prop]; ok {
				style.Set(prop, init)
			} else {
				delete(style.Properties, prop)
			}
		case "revert":
			// Post-cascade resolveCSSWideKeywords replaces this sentinel with
			// the UA + presentational-attribute snapshot value (or deletes
			// the property if there was none). Mirrors CSS Cascade 4 §6.1.2.
			style.Set(prop, "revert")
		case "revert-layer":
			// CSS Cascade 5 §6.1.3: `revert-layer` is distinct from `revert`
			// — it rolls back to the start of the current cascade layer, not
			// to the previous origin. Preserve the sentinel literally so the
			// per-layer resolveRevertLayerOnly pass in ComputeStyle uses the
			// correct snapshot. Collapsing it to "revert" here (the previous
			// bug) made `all: revert-layer` equivalent to `all: revert` and
			// broke revert-layer-003.
			style.Set(prop, "revert-layer")
		}
	}
}

// nonDefaultInitialValues maps CSS longhand properties whose `initial` /
// `unset` resolution must store the canonical initial value explicitly,
// rather than deleting the property and relying on the getter fallback.
//
// Two reasons a property needs an entry here:
//
//  1. The getter's absence-of-property fallback differs from the spec
//     initial (e.g. GetDisplay returns DisplayBlock when absent, but the
//     CSS Display §2 initial is `inline`).
//
//  2. The property is INHERITABLE. For inheritable properties, the
//     post-cascade ApplyInheritedProperties pass copies the parent's value
//     into any absent slot — which would clobber an `initial`-induced
//     deletion. Storing the explicit initial value makes the slot present,
//     so the inherit-when-absent pass leaves it alone. Mirrors Blink's
//     StyleResolver writing CSSInitialValue into the ComputedStyleBuilder
//     rather than leaving the property unset (Blink SHA
//     4883d11fef4a8713e32cd582ecef6dc5457c8c3f, style_cascade.cc).
//
// Properties NOT listed here are non-inheritable AND have spec-conformant
// getter fallbacks, so deletion is the correct resolution.
var nonDefaultInitialValues = map[string]string{
	// CSS Display §2: the initial value of `display` is `inline`. louis14's
	// GetDisplay returns DisplayBlock when the property is absent (a defensive
	// choice for elements with no UA rule), so `all: initial` must store
	// `inline` explicitly to override it.
	"display": "inline",
	// CSS Color 4 §3.2: initial value of `color` is `canvastext`
	// (historically black on most platforms). `color` IS inherited, so
	// without an explicit entry here ApplyInheritedProperties would re-fill
	// the deleted slot with the parent's color — breaking the contract
	// "`color: initial` resets to the initial value, NOT the inherited".
	// Required by text-decoration-subelements-003 where
	// `sup.unhide { color: initial; }` must render black against a
	// `color: transparent` parent.
	"color": "canvastext",
}

// applyDeclarationWithVisitedFilter expands `property: value` into `style`,
// but if `visitedOnly` is true (the rule's selector contains `:visited`),
// only the visitedAllowedProperties subset of resulting longhands is kept.
// Used by ComputeStyle to gate every declaration from a `:visited`-matched
// rule per CSS Selectors 4 §17.2, including the longhands a `:visited`-scoped
// `all` shorthand would otherwise touch.
//
// Implementation: when visitedOnly is true, expand into a fresh temp style,
// then copy only the visited-allowed longhands to the destination.
//
// For revert tracking, the temp style needs the same revert-snapshot machinery
// as the real style — but since `all: revert` from a :visited rule only
// affects visited-allowed properties, we copy only those over. The revert
// sentinel survives in the destination and is resolved by the post-cascade
// resolveCSSWideKeywords pass against the snapshot already captured for the
// destination style.
func applyDeclarationWithVisitedFilter(style *Style, property, value string, visitedOnly bool) {
	if !visitedOnly {
		expandShorthand(style, property, value)
		return
	}
	// Fast path: a single-longhand declaration whose property is not in the
	// visited-allowed set is dropped entirely without invoking the expander.
	if property != "all" && !shorthandProducesVisitedLonghand(property) {
		if !visitedAllowedProperties[property] {
			return
		}
	}
	tmp := &Style{Properties: make(map[string]string)}
	expandShorthand(tmp, property, value)
	for k, v := range tmp.Properties {
		if visitedAllowedProperties[k] {
			style.Set(k, v)
		}
	}
}

// shorthandProducesVisitedLonghand returns true for shorthand property names
// whose expansion can touch one or more visited-allowed longhands. Used by
// applyDeclarationWithVisitedFilter to decide whether the temp-style detour
// is worth taking. `all` is always included; the others cover the shorthands
// louis14 expands that resolve to a color-eligible longhand.
func shorthandProducesVisitedLonghand(property string) bool {
	switch property {
	case "all", "background", "border", "border-color",
		"border-top", "border-right", "border-bottom", "border-left",
		"outline", "text-decoration", "text-emphasis":
		return true
	}
	return false
}

// visitedAllowedProperties is the CSS Selectors 4 §17.2 subset of properties
// whose declared value from a `:visited`-matched rule may affect the rendered
// visited link. Mirrors Blink's StyleResolverState `IsVisitedDependentProperty`
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Declarations outside this
// set (including non-color longhands reset by `all`) are dropped from
// `:visited` rules to preserve user-browsing-history privacy.
var visitedAllowedProperties = map[string]bool{
	"color":                 true,
	"background-color":      true,
	"border-top-color":      true,
	"border-right-color":    true,
	"border-bottom-color":   true,
	"border-left-color":     true,
	"outline-color":         true,
	"column-rule-color":     true,
	"text-decoration-color": true,
	"text-emphasis-color":   true,
	"caret-color":           true,
	"fill":                  true,
	"stroke":                true,
}

// selectorMatchesVisited reports whether any part of the given selector
// contains the `:visited` pseudo-class — i.e. whether the rule's declarations
// must be filtered to the visited-allowed subset before being applied.
func selectorMatchesVisited(sel Selector) bool {
	for _, part := range sel.Parts {
		for _, pc := range part.PseudoClasses {
			if pc == "visited" {
				return true
			}
		}
	}
	return false
}

// cssLonghandProperties is the canonical list of CSS longhand properties the
// louis14 engine knows about. Sourced from the union of every style.Get / Set
// call site in the engine — i.e. every property a reader or writer references.
// Shorthand spellings (border, margin, padding, font, background, etc.) are
// deliberately excluded; the `all` shorthand expands to longhands only, matching
// Blink's all.cc which iterates CSSPropertyIDIterator over longhands.
//
// When adding support for a new CSS property, append its longhand name(s) here
// so `all: initial` resets it correctly. This is the one place that enumerates
// "every CSS property" — there is no other registry.
var cssLonghandProperties = []string{
	// Box model
	"display", "position", "float", "clear", "visibility",
	"top", "right", "bottom", "left",
	"width", "min-width", "max-width", "height", "min-height", "max-height",
	"margin-top", "margin-right", "margin-bottom", "margin-left",
	"padding-top", "padding-right", "padding-bottom", "padding-left",
	"box-sizing", "overflow", "overflow-x", "overflow-y",
	"aspect-ratio",
	// Border
	"border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
	"border-top-style", "border-right-style", "border-bottom-style", "border-left-style",
	"border-top-color", "border-right-color", "border-bottom-color", "border-left-color",
	"border-top-left-radius", "border-top-right-radius",
	"border-bottom-left-radius", "border-bottom-right-radius",
	"border-image-source", "border-image-slice", "border-image-width",
	"border-image-outset", "border-image-repeat",
	"border-collapse", "border-spacing",
	// Outline
	"outline-width", "outline-style", "outline-color", "outline-offset",
	// Background
	"background-color", "background-image", "background-repeat",
	"background-position", "background-size", "background-attachment",
	"background-clip", "background-origin",
	// Color & opacity
	"color", "opacity",
	// Font
	"font-family", "font-size", "font-weight", "font-style", "font-variant",
	"font-feature-settings", "font-optical-sizing", "font-size-adjust",
	"font-synthesis-weight", "font-synthesis-style",
	"font-synthesis-small-caps", "font-synthesis-position",
	"font-variant-caps", "font-variant-ligatures",
	"font-variant-numeric",
	// Text
	"line-height", "text-align", "text-align-last", "text-indent",
	"text-transform", "text-decoration-line", "text-decoration-style",
	"text-decoration-color", "text-decoration-thickness",
	"text-decoration-inset", "text-decoration-skip-spaces",
	"text-underline-offset", "text-overflow",
	"text-shadow", "text-wrap", "text-emphasis-color",
	"text-emphasis-style", "text-emphasis-position",
	"letter-spacing", "word-spacing", "white-space",
	"word-break", "overflow-wrap", "word-wrap", "hyphens",
	"tab-size", "vertical-align",
	// Writing modes
	"writing-mode", "text-orientation",
	// Lists
	"list-style-type", "list-style-position", "list-style-image",
	// Tables
	"caption-side", "empty-cells", "table-layout",
	// Flex
	"flex-direction", "flex-wrap", "flex-grow", "flex-shrink", "flex-basis",
	"align-items", "align-content", "align-self",
	"justify-content", "justify-items", "justify-self",
	"order", "row-gap", "column-gap",
	// Grid
	"grid-template-columns", "grid-template-rows", "grid-template-areas",
	"grid-auto-columns", "grid-auto-rows", "grid-auto-flow",
	"grid-column-start", "grid-column-end", "grid-row-start", "grid-row-end",
	// Multi-column
	"column-count", "column-width", "column-fill",
	"column-rule-width", "column-rule-style", "column-rule-color",
	"column-span",
	// Transform / animation
	"transform", "transform-origin", "translate", "rotate", "scale",
	"transition", "animation-name", "animation-duration",
	"animation-timing-function", "animation-delay", "animation-iteration-count",
	"animation-direction", "animation-fill-mode", "animation-play-state",
	// Effects
	"box-shadow", "filter", "backdrop-filter", "mix-blend-mode", "isolation",
	"clip", "clip-path", "mask-image",
	// SVG paint
	"fill", "fill-rule", "stroke", "stroke-dasharray", "stroke-dashoffset",
	"stroke-linecap", "stroke-linejoin", "stroke-miterlimit", "stroke-width",
	"flood-color", "flood-opacity", "stop-color", "stop-opacity",
	"lighting-color",
	// Misc
	"content", "counter-increment", "counter-reset", "quotes",
	"cursor", "z-index", "will-change", "contain",
	"break-before", "break-after", "break-inside",
	"image-rendering", "object-fit", "object-position",
	"scrollbar-color", "scrollbar-gutter", "scrollbar-width",
	"box-decoration-break", "initial-letter",
	// Vendor-prefixed longhands louis14 reads
	"-webkit-box-align", "-webkit-box-direction", "-webkit-box-orient",
	"-webkit-box-decoration-break", "-webkit-line-clamp", "-webkit-mask-image",
}

// allShorthandRevertSnapshot captures the cascade origin baseline against
// which `all: revert` and `all: revert-layer` resolve. ComputeStyle calls
// snapshotForAllRevert after the UA + presentational-attribute pass (i.e.
// after every value the spec considers "below the author origin") to record
// the pre-author state. resolveAllRevertValues then replays each "revert"
// sentinel against that snapshot.
type allShorthandRevertSnapshot map[string]string

func snapshotForAllRevert(s *Style) allShorthandRevertSnapshot {
	snap := make(allShorthandRevertSnapshot, len(s.Properties))
	for k, v := range s.Properties {
		snap[k] = v
	}
	return snap
}

// resolveCSSWideKeywords handles the longhand-level CSS-wide keywords that
// expandShorthand stores verbatim — `initial`, `unset`, `revert`, and
// `revert-layer`. `inherit` is resolved separately in resolveInheritValues
// since it needs the parent style.
//
// Per CSS Cascade 4 §6.1:
//   - `initial`: replace with the spec initial value. For louis14, the getter
//     fallback returns the initial value for most properties, so delete the
//     entry; for properties listed in nonDefaultInitialValues, store the
//     canonical initial explicitly.
//   - `unset`: behaves as `inherit` for inheritable properties, `initial`
//     otherwise. We rewrite to `inherit` and let resolveInheritValues finish
//     the work, mirroring Blink's CSSUnsetValue handling that defers to
//     either inherit or initial based on the property metadata.
//   - `revert`: roll back to the prior cascade origin (UA + presentational
//     attributes — every value below the author origin). `originSnap` carries
//     that baseline.
//   - `revert-layer`: roll back the current cascade layer's contribution.
//     `layerSnap` carries the most-recent-prior-layer snapshot the layer
//     iterator captured during author-origin application. When no prior
//     layer existed, falls back to `originSnap` so the property reverts to
//     UA (which matches Chrome's behavior for layerless `revert-layer`).
//
// Mirrors Blink's CSSRevertValue / CSSRevertLayerValue handling at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func resolveCSSWideKeywords(s *Style, originSnap, layerSnap allShorthandRevertSnapshot) {
	for prop, val := range s.Properties {
		isCustom := strings.HasPrefix(prop, "--")
		switch val {
		case "initial":
			if isCustom {
				// CSS Custom Properties §3 (Initial Value): the initial value
				// of any custom property is the guaranteed-invalid value. Per
				// CSS Variables §3, a var() reference to a guaranteed-invalid
				// custom property triggers IACVT and uses the var()'s
				// fallback. Store an empty string sentinel so:
				//   - resolveVarReferences sees the empty value and uses the
				//     fallback when one is supplied;
				//   - the explicit-initial declaration blocks the regular
				//     custom-property inheritance pass (which would otherwise
				//     copy the parent's value over this child's intent).
				// Mirrors Blink's CSSCustomPropertyDeclaration with value
				// type kInitial at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
				s.Set(prop, "")
				continue
			}
			if init, ok := nonDefaultInitialValues[prop]; ok {
				s.Set(prop, init)
			} else {
				delete(s.Properties, prop)
			}
		case "unset":
			if isCustom {
				// CSS Custom Properties §2.2: custom properties are inherited
				// by default. `unset` on an inheritable property behaves as
				// `inherit`. Rewrite to "inherit" so resolveInheritValues (or
				// the post-cascade custom-property inheritance copy in
				// ApplyInheritedProperties) does the actual lookup.
				s.Set(prop, "inherit")
				continue
			}
			if inheritableProperties[prop] {
				s.Set(prop, "inherit")
			} else if init, ok := nonDefaultInitialValues[prop]; ok {
				s.Set(prop, init)
			} else {
				delete(s.Properties, prop)
			}
		case "revert":
			if snapVal, ok := originSnap[prop]; ok {
				s.Set(prop, snapVal)
			} else {
				delete(s.Properties, prop)
			}
		case "revert-layer":
			if snapVal, ok := layerSnap[prop]; ok {
				s.Set(prop, snapVal)
			} else if snapVal, ok := originSnap[prop]; ok {
				s.Set(prop, snapVal)
			} else {
				delete(s.Properties, prop)
			}
		}
	}
}

// resolveRevertLayerOnly rolls back any property currently holding the
// literal `revert-layer` sentinel to the value captured in `layerSnap`
// (start-of-layer baseline). Falls back to `originSnap` (UA + presentational
// attribute baseline) when the property was not present at the layer
// boundary, mirroring CSS Cascade 5 §6.1.3's "as if the property had not
// been declared in this layer" semantics — when there was nothing to revert
// to within layers, `revert-layer` reduces to `revert`.
//
// Called at each cascade-layer boundary in ComputeStyle so chained
// revert-layer across layers (revert-layer-007) and revert-layer-into-
// previous-layer scenarios (revert-layer-001 / -003 / -005) resolve to
// the layer's cascaded value, not to a downstream layer's literal
// `revert-layer` sentinel. Mirrors Blink's per-layer
// CascadeResolver::ResolveRevertLayer pass at SHA 4883d11fef.
func resolveRevertLayerOnly(s *Style, originSnap, layerSnap allShorthandRevertSnapshot) {
	for prop, val := range s.Properties {
		if val != "revert-layer" {
			continue
		}
		if snapVal, ok := layerSnap[prop]; ok && snapVal != "revert-layer" {
			s.Set(prop, snapVal)
			continue
		}
		if snapVal, ok := originSnap[prop]; ok {
			s.Set(prop, snapVal)
			continue
		}
		delete(s.Properties, prop)
	}
}

// expandOutlineShorthand parses the outline shorthand into outline-width, outline-style, outline-color.
// Format: "3px solid blue" — order of components is not significant.
func expandOutlineShorthand(style *Style, value string) {
	// Reset all outline sub-properties to initial values
	style.Set("outline-width", "3px") // medium = 3px
	style.Set("outline-style", "none")
	style.Set("outline-color", "currentcolor")

	parts := strings.Fields(value)
	for _, part := range parts {
		if part == "none" {
			style.Set("outline-style", "none")
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" ||
			part == "groove" || part == "ridge" || part == "inset" || part == "outset" {
			style.Set("outline-style", part)
		} else if bw, ok := borderWidthKeyword(part); ok {
			style.Set("outline-width", bw)
		} else if _, ok := ParseLength(part); ok {
			style.Set("outline-width", part)
		} else if part != "" {
			// Assume it's a color
			style.Set("outline-color", part)
		}
	}
}

// expandColumnRuleShorthand parses the column-rule shorthand into column-rule-width, column-rule-style, column-rule-color.
// Format: "4px solid green" — order: width style color (order not significant per spec).
func expandColumnRuleShorthand(style *Style, value string) {
	// Reset column-rule sub-properties to initial values
	style.Set("column-rule-width", "medium")
	style.Set("column-rule-style", "none")
	style.Set("column-rule-color", "currentcolor")

	parts := strings.Fields(value)
	for _, part := range parts {
		if part == "none" {
			style.Set("column-rule-style", "none")
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" ||
			part == "groove" || part == "ridge" || part == "inset" || part == "outset" {
			style.Set("column-rule-style", part)
		} else if bw, ok := borderWidthKeyword(part); ok {
			style.Set("column-rule-width", bw)
		} else if _, ok := ParseLength(part); ok {
			style.Set("column-rule-width", part)
		} else if part != "" {
			// Try as color
			if _, ok2 := ParseColor(part); ok2 {
				style.Set("column-rule-color", part)
			}
		}
	}
}

// expandTextDecorationShorthand expands the `text-decoration` shorthand into
// its four longhands: text-decoration-line, text-decoration-style,
// text-decoration-color, text-decoration-thickness.
//
// Per CSS Text Decor 4 §2.6
// (https://drafts.csswg.org/css-text-decor-4/#text-decoration-property), each
// omitted component is reset to its initial value:
//   - line      → none
//   - style     → solid
//   - color     → currentcolor
//   - thickness → auto
//
// Mirrors Blink's `ConsumeShorthandGreedily` logic for the text-decoration
// shorthand at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func expandTextDecorationShorthand(style *Style, value string) {
	// Initial values (reset before parsing).
	style.Set("text-decoration-line", "none")
	style.Set("text-decoration-style", "solid")
	style.Set("text-decoration-color", "currentcolor")
	style.Set("text-decoration-thickness", "auto")

	v := strings.TrimSpace(value)
	if v == "" {
		return
	}

	// Tokenize, but keep functional notations like rgb(...) / hsl(...) as
	// single tokens. Walk paren depth.
	var tokens []string
	depth := 0
	start := 0
	for i := 0; i < len(v); i++ {
		ch := v[i]
		switch ch {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ' ', '\t', '\n':
			if depth == 0 {
				if i > start {
					tokens = append(tokens, v[start:i])
				}
				start = i + 1
			}
		}
	}
	if start < len(v) {
		tokens = append(tokens, v[start:])
	}

	// Collect line keywords (may be multiple: underline overline line-through blink).
	// Single style keyword. Single color. Single thickness (auto|from-font|<length-percentage>).
	var lineTokens []string
	styleSet := false
	colorSet := false
	thicknessSet := false
	for _, tok := range tokens {
		lower := strings.ToLower(tok)
		switch lower {
		case "none":
			// `none` is only a valid line keyword (and only standalone). Keep
			// the initial "none" — no other line tokens allowed.
			lineTokens = append(lineTokens, "none")
			continue
		case "underline", "overline", "line-through", "blink":
			lineTokens = append(lineTokens, lower)
			continue
		case "solid", "double", "dotted", "dashed", "wavy":
			if !styleSet {
				style.Set("text-decoration-style", lower)
				styleSet = true
			}
			continue
		case "auto", "from-font":
			if !thicknessSet {
				style.Set("text-decoration-thickness", lower)
				thicknessSet = true
			}
			continue
		}
		// Numeric / percentage / calc → thickness.
		if !thicknessSet {
			if _, ok := ParseLength(tok); ok {
				style.Set("text-decoration-thickness", tok)
				thicknessSet = true
				continue
			}
			if strings.HasSuffix(tok, "%") {
				if _, ok := ParsePercentage(tok); ok {
					style.Set("text-decoration-thickness", tok)
					thicknessSet = true
					continue
				}
			}
		}
		// Color (named, hex, functional).
		if !colorSet {
			if _, ok := ParseColor(tok); ok {
				style.Set("text-decoration-color", tok)
				colorSet = true
				continue
			}
		}
	}

	if len(lineTokens) > 0 {
		style.Set("text-decoration-line", strings.Join(lineTokens, " "))
	}
}

// expandFontSynthesisShorthand expands the `font-synthesis` shorthand into
// its four longhands: font-synthesis-weight, font-synthesis-style,
// font-synthesis-small-caps, font-synthesis-position.
//
// Per CSS Fonts 4 §6.5
// (https://drafts.csswg.org/css-fonts-4/#font-synthesis), the grammar is:
//
//	none | [ weight || style || small-caps || position ]
//
// Each keyword present in the value sets the corresponding longhand to
// `auto`; each omitted keyword (and every longhand when the value is `none`)
// is set to `none`. The four longhands' initial value is `auto`, but a value
// like `font-synthesis: weight` must explicitly *reset* the others to `none`.
// Mirrors Blink's font_synthesis.cc::ParseShorthand at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func expandFontSynthesisShorthand(style *Style, value string) {
	v := strings.TrimSpace(strings.ToLower(value))
	// `none` → all four longhands set to `none`.
	// Any other value: walk the token list; explicitly listed keywords
	// become `auto`, the rest stay `none`.
	style.Set("font-synthesis-weight", "none")
	style.Set("font-synthesis-style", "none")
	style.Set("font-synthesis-small-caps", "none")
	style.Set("font-synthesis-position", "none")
	if v == "" || v == "none" {
		return
	}
	for _, tok := range strings.Fields(v) {
		switch tok {
		case "weight":
			style.Set("font-synthesis-weight", "auto")
		case "style":
			style.Set("font-synthesis-style", "auto")
		case "small-caps":
			style.Set("font-synthesis-small-caps", "auto")
		case "position":
			style.Set("font-synthesis-position", "auto")
		}
	}
}

// splitBorderImageSlashes splits a border-image value string on "/" characters
// that are not inside parentheses, returning up to 3 parts.
func splitBorderImageSlashes(v string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if ch == '(' {
			depth++
		} else if ch == ')' {
			if depth > 0 {
				depth--
			}
		} else if ch == '/' && depth == 0 {
			parts = append(parts, v[start:i])
			start = i + 1
		}
	}
	parts = append(parts, v[start:])
	return parts
}

// findBorderImageSourceEnd returns the index in s at which the source image
// value ends. The source is either a url(...) or a *gradient(...) function.
// Returns len(s) if the whole string is the source.
func findBorderImageSourceEnd(s string) int {
	s = strings.TrimSpace(s)
	offset := len(s) - len(strings.TrimLeft(s, " \t")) // leading spaces already trimmed by caller
	_ = offset
	// Walk paren-depth to find end of leading function token
	i := 0
	depth := 0
	for i < len(s) {
		if s[i] == '(' {
			depth++
			i++
		} else if s[i] == ')' {
			depth--
			i++
			if depth == 0 {
				// End of the function; skip trailing spaces
				return i
			}
		} else if s[i] == ' ' && depth == 0 {
			// Space outside any parens — source ends here
			return i
		} else {
			i++
		}
	}
	return i
}

// GetColumnRuleWidth returns the column-rule-width in pixels. Lengths in
// font-relative units (em/rem/ex/ch) resolve against this style's own
// computed font-size, matching how GetBorderWidth and GetColumnGapMulticol
// handle the same units.
func (s *Style) GetColumnRuleWidth() float64 {
	if v, ok := s.Get("column-rule-width"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "thin":
			return 1
		case "medium":
			return 3
		case "thick":
			return 5
		}
		if px, ok2 := parseLengthFullWithCh(v, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok2 {
			return px
		}
	}
	return 0
}

// GetColumnRuleStyle returns the column-rule-style (none, solid, dashed, dotted, etc.)
func (s *Style) GetColumnRuleStyle() string {
	if v, ok := s.Get("column-rule-style"); ok {
		return strings.TrimSpace(v)
	}
	return "none"
}

// HasColumnRule returns true when column-rule-style is set to a visible value.
// Mirrors Blink's ComputedStyle::HasColumnRule().
func (s *Style) HasColumnRule() bool {
	rs := s.GetColumnRuleStyle()
	return rs != "" && rs != "none"
}

// GetColumnRuleColor returns the column-rule-color as a Color
func (s *Style) GetColumnRuleColor() Color {
	if v, ok := s.Get("column-rule-color"); ok {
		v = strings.TrimSpace(v)
		if v != "currentcolor" {
			if c, ok2 := ParseColor(v); ok2 {
				return c
			}
		}
	}
	// Default: currentColor (use text color)
	if v, ok := s.Get("color"); ok {
		if c, ok2 := ParseColor(v); ok2 {
			return c
		}
	}
	return Color{R: 0, G: 0, B: 0, A: 1}
}

// GetOutlineStyle returns the outline-style value (default: "none")
func (s *Style) GetOutlineStyle() string {
	if val, ok := s.Get("outline-style"); ok {
		return val
	}
	return "none"
}

// GetOutlineWidth returns the outline width in pixels.
// Returns 0 when outline-style is "none" or outline-width is not set.
func (s *Style) GetOutlineWidth() float64 {
	outlineStyle := s.GetOutlineStyle()
	if outlineStyle == "none" || outlineStyle == "" {
		return 0
	}
	if val, ok := s.Get("outline-width"); ok {
		switch strings.ToLower(val) {
		case "thin":
			return 1
		case "medium":
			return 3
		case "thick":
			return 5
		}
		if px, ok2 := ParseLength(val); ok2 {
			return px
		}
	}
	return 3 // default medium = 3px
}

// GetOutlineColor returns the outline color as RGBA components.
// Defaults to currentColor (element's text color), falling back to black.
func (s *Style) GetOutlineColor() (r, g, b uint8, a float64) {
	colorStr := "currentcolor"
	if val, ok := s.Get("outline-color"); ok {
		colorStr = val
	}
	if strings.EqualFold(colorStr, "currentcolor") || colorStr == "" {
		// Use element's text color
		if textColor, ok := s.Get("color"); ok {
			if c, ok2 := ParseColor(textColor); ok2 {
				return c.R, c.G, c.B, c.A
			}
		}
		return 0, 0, 0, 1.0 // default black
	}
	if c, ok := ParseColor(colorStr); ok {
		return c.R, c.G, c.B, c.A
	}
	return 0, 0, 0, 1.0 // fallback black
}

// GetOutlineOffset returns the outline-offset in pixels (default: 0).
func (s *Style) GetOutlineOffset() float64 {
	if val, ok := s.Get("outline-offset"); ok {
		if px, ok2 := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok2 {
			return px
		}
	}
	return 0
}

// expandBorderRadiusProperty expands border-radius shorthand into per-corner properties.
// CSS spec: border-radius: TL TR BR BL (1-4 values, same pattern as margin/padding)
// Single value: all corners. Two values: TL+BR, TR+BL. Three: TL, TR+BL, BR. Four: TL TR BR BL.
// Note: the "/" syntax for elliptical radii is not supported.
func expandBorderRadiusProperty(style *Style, value string) {
	// If the value is "inherit" or "initial", expand to all individual corner properties
	// so that resolveInheritValues can find each corner on the parent.
	lower := strings.TrimSpace(strings.ToLower(value))
	if lower == "inherit" || lower == "initial" || lower == "unset" {
		style.Set("border-top-left-radius", lower)
		style.Set("border-top-right-radius", lower)
		style.Set("border-bottom-right-radius", lower)
		style.Set("border-bottom-left-radius", lower)
		return
	}

	// Handle elliptical radii (slash syntax): "10px 20px / 5px 15px"
	// First set = horizontal radii (Rx), second set = vertical radii (Ry).
	// splitOnSlashOutsideParens / splitShorthandParts respect parentheses so
	// `calc(...)` tokens with embedded spaces and `/` are preserved.
	var hParts, vParts []string
	if hHalf, vHalf, hasSlash := splitOnSlashOutsideParens(value); hasSlash {
		hParts = splitShorthandParts(strings.TrimSpace(hHalf))
		vParts = splitShorthandParts(strings.TrimSpace(vHalf))
	} else {
		hParts = splitShorthandParts(value)
		vParts = nil // no vertical set = same as horizontal
	}

	// Expand 1-4 values to exactly 4 per CSS spec (TL, TR, BR, BL pattern).
	hExpanded := expandFourValues(hParts)
	var vExpanded [4]string
	if vParts != nil {
		vExpanded = expandFourValues(vParts)
	}

	if hExpanded == [4]string{} {
		return // no valid values
	}

	if vParts == nil && len(hParts) == 1 {
		// Single value, no slash — store as shorthand for backward compat
		style.Set("border-radius", hParts[0])
		return
	}

	// Store per-corner as "Rx Ry" two-value or just "Rx" single-value
	corners := [4]string{
		"border-top-left-radius", "border-top-right-radius",
		"border-bottom-right-radius", "border-bottom-left-radius",
	}
	for i, prop := range corners {
		if vParts != nil && vExpanded[i] != "" {
			style.Set(prop, hExpanded[i]+" "+vExpanded[i])
		} else {
			style.Set(prop, hExpanded[i])
		}
	}
}

// expandFourValues expands 1-4 CSS values to exactly 4 using the standard
// TL TR BR BL pattern (same as margin/padding).
func expandFourValues(parts []string) [4]string {
	switch len(parts) {
	case 1:
		return [4]string{parts[0], parts[0], parts[0], parts[0]}
	case 2:
		return [4]string{parts[0], parts[1], parts[0], parts[1]}
	case 3:
		return [4]string{parts[0], parts[1], parts[2], parts[1]}
	case 4:
		return [4]string{parts[0], parts[1], parts[2], parts[3]}
	default:
		return [4]string{}
	}
}

// splitOnSlashOutsideParens splits a CSS shorthand at the first `/` that is
// not inside parentheses. Used by border-radius (and other shorthands where
// `/` separates two value sets) so that `calc(... / ...)` is not mis-split.
// Returns (left, right, true) when a top-level slash is found; otherwise
// returns ("", "", false).
func splitOnSlashOutsideParens(value string) (string, string, bool) {
	depth := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '/':
			if depth == 0 {
				return value[:i], value[i+1:], true
			}
		}
	}
	return "", "", false
}

// splitShorthandParts splits a CSS shorthand value by whitespace, respecting parentheses.
// Unlike strings.Fields, correctly handles values like "calc(10px + 1%) 0 0 0".
func splitShorthandParts(value string) []string {
	var parts []string
	depth := 0
	start := -1
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch == '(' {
			depth++
			if start == -1 {
				start = i
			}
		} else if ch == ')' {
			depth--
			if start == -1 {
				start = i
			}
		} else if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if depth == 0 && start != -1 {
				parts = append(parts, value[start:i])
				start = -1
			}
		} else {
			if start == -1 {
				start = i
			}
		}
	}
	if start != -1 {
		parts = append(parts, value[start:])
	}
	return parts
}

// expandBoxProperty expands margin/padding shorthand
// Supports: "10px" (all), "10px 20px" (vertical horizontal),
//
//	"10px 20px 30px" (top h bottom), "10px 20px 30px 40px" (t r b l)
func expandBoxProperty(style *Style, prefix, value string) {
	parts := splitShorthandParts(value)

	switch len(parts) {
	case 1:
		// All sides the same
		style.Set(prefix+"-top", parts[0])
		style.Set(prefix+"-right", parts[0])
		style.Set(prefix+"-bottom", parts[0])
		style.Set(prefix+"-left", parts[0])
	case 2:
		// Vertical, horizontal
		style.Set(prefix+"-top", parts[0])
		style.Set(prefix+"-bottom", parts[0])
		style.Set(prefix+"-right", parts[1])
		style.Set(prefix+"-left", parts[1])
	case 3:
		// Top, horizontal, bottom
		style.Set(prefix+"-top", parts[0])
		style.Set(prefix+"-right", parts[1])
		style.Set(prefix+"-left", parts[1])
		style.Set(prefix+"-bottom", parts[2])
	case 4:
		// Top, right, bottom, left
		style.Set(prefix+"-top", parts[0])
		style.Set(prefix+"-right", parts[1])
		style.Set(prefix+"-bottom", parts[2])
		style.Set(prefix+"-left", parts[3])
	}
}

// expandBorderBoxProperty expands border-width/style/color shorthand (1-4 values)
func expandBorderBoxProperty(style *Style, value string, suffix string) {
	parts := strings.Fields(value)
	var top, right, bottom, left string
	switch len(parts) {
	case 1:
		top, right, bottom, left = parts[0], parts[0], parts[0], parts[0]
	case 2:
		top, bottom = parts[0], parts[0]
		right, left = parts[1], parts[1]
	case 3:
		top, right, left, bottom = parts[0], parts[1], parts[1], parts[2]
	case 4:
		top, right, bottom, left = parts[0], parts[1], parts[2], parts[3]
	default:
		return
	}
	style.Set("border-top-"+suffix, top)
	style.Set("border-right-"+suffix, right)
	style.Set("border-bottom-"+suffix, bottom)
	style.Set("border-left-"+suffix, left)
}

// borderWidthKeyword resolves thin/medium/thick to pixel values.
func borderWidthKeyword(val string) (string, bool) {
	switch strings.ToLower(val) {
	case "thin":
		return "1px", true
	case "medium":
		return "3px", true
	case "thick":
		return "5px", true
	}
	return "", false
}

// expandBorderProperty expands border shorthand
// Format: "1px solid black" or "2px dotted #FF0000"
// Per CSS spec, shorthand properties reset ALL sub-properties to their initial values,
// then apply the specified values.
func expandBorderProperty(style *Style, value string) {
	// Reset all sub-properties to their initial values first
	// Initial values: width=medium (3px), style=none, color=currentcolor
	sides := []string{"top", "right", "bottom", "left"}
	for _, side := range sides {
		style.Set("border-"+side+"-width", "3px") // medium = 3px
		style.Set("border-"+side+"-style", "none")
		style.Set("border-"+side+"-color", "currentcolor")
	}

	// Now apply the specified values.
	// IMPORTANT: Store ONLY longhands (border-top-width, etc.), NOT intermediate
	// shorthand keys (border-width, border-style, border-color). Storing shorthands
	// causes them to be re-expanded by the cascade when iterating rule.Declarations,
	// which may overwrite longhands set by later declarations in the same rule
	// (e.g. border-width: 40px 20px 60px 30px appearing after border: 0px solid).
	parts := strings.Fields(value)
	for _, part := range parts {
		if bw, ok := borderWidthKeyword(part); ok {
			style.Set("border-top-width", bw)
			style.Set("border-right-width", bw)
			style.Set("border-bottom-width", bw)
			style.Set("border-left-width", bw)
		} else if _, ok := ParseLength(part); ok {
			// Width (px, em, mm, or bare number)
			style.Set("border-top-width", part)
			style.Set("border-right-width", part)
			style.Set("border-bottom-width", part)
			style.Set("border-left-width", part)
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" || part == "none" || part == "inset" || part == "outset" || part == "groove" || part == "ridge" {
			// Style
			style.Set("border-top-style", part)
			style.Set("border-right-style", part)
			style.Set("border-bottom-style", part)
			style.Set("border-left-style", part)
		} else {
			// Color
			style.Set("border-top-color", part)
			style.Set("border-right-color", part)
			style.Set("border-bottom-color", part)
			style.Set("border-left-color", part)
		}
	}
}

// expandFlexProperty expands the flex shorthand.
// CSS spec: flex: none | [ <flex-grow> <flex-shrink>? || <flex-basis> ]
// Keywords: "none" = "0 0 auto", "auto" = "1 1 auto", "initial" = "0 1 auto"
// IMPORTANT: When omitted from shorthand, flex-grow defaults to 1, flex-basis defaults to 0
// (different from individual property defaults of 0 and auto)
func expandFlexProperty(style *Style, value string) {
	value = strings.TrimSpace(value)
	switch value {
	case "none":
		style.Set("flex-grow", "0")
		style.Set("flex-shrink", "0")
		style.Set("flex-basis", "auto")
		return
	case "auto":
		style.Set("flex-grow", "1")
		style.Set("flex-shrink", "1")
		style.Set("flex-basis", "auto")
		return
	case "initial":
		style.Set("flex-grow", "0")
		style.Set("flex-shrink", "1")
		style.Set("flex-basis", "auto")
		return
	}

	parts := strings.Fields(value)
	// Default shorthand values (different from individual property defaults!)
	grow, shrink, basis := "1", "1", "0"

	switch len(parts) {
	case 1:
		// Could be a number (flex-grow) or a length/keyword (flex-basis)
		if isFlexNumber(parts[0]) {
			grow = parts[0]
			basis = "0"
		} else {
			basis = parts[0]
		}
	case 2:
		// flex-grow flex-shrink OR flex-grow flex-basis
		grow = parts[0]
		if isFlexNumber(parts[1]) {
			shrink = parts[1]
			basis = "0"
		} else {
			basis = parts[1]
		}
	case 3:
		// flex-grow flex-shrink flex-basis
		grow = parts[0]
		shrink = parts[1]
		basis = parts[2]
	}

	style.Set("flex-grow", grow)
	style.Set("flex-shrink", shrink)
	style.Set("flex-basis", basis)
}

// isFlexNumber returns true if the value is a unitless number (for flex-grow/flex-shrink)
func isFlexNumber(val string) bool {
	_, err := strconv.ParseFloat(val, 64)
	return err == nil && !strings.ContainsAny(val, "%")
}

// expandFlexFlowProperty expands flex-flow shorthand into flex-direction and flex-wrap.
// Per CSS Flexbox spec, shorthand resets all sub-properties to initial values first.
func expandFlexFlowProperty(style *Style, value string) {
	// Reset both sub-properties to their initial values first
	style.Set("flex-direction", "row")
	style.Set("flex-wrap", "nowrap")

	parts := strings.Fields(value)
	for _, part := range parts {
		switch part {
		case "row", "row-reverse", "column", "column-reverse":
			style.Set("flex-direction", part)
		case "nowrap", "wrap", "wrap-reverse":
			style.Set("flex-wrap", part)
		}
	}
}

// expandBorderSideProperty expands border-top/right/bottom/left shorthands.
// Per CSS spec, shorthand properties reset ALL sub-properties to their initial values,
// then apply the specified values.
// storeBorderLogicalMarker parses a border shorthand value and stores the
// sub-properties under the given marker prefix (e.g. "_border-inline-start").
// Unlike expandBorderSideProperty, this does NOT prefix "border-" to the marker.
func storeBorderLogicalMarker(style *Style, marker, value string) {
	// Parse the value the same way as expandBorderSideProperty
	width, borderStyle, color := "3px", "none", "currentcolor"
	parts := strings.Fields(value)
	for _, part := range parts {
		if part == "0" {
			width = "0"
		} else if bw, ok := borderWidthKeyword(part); ok {
			width = bw
		} else if _, ok := ParseLength(part); ok {
			width = part
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" || part == "none" || part == "inset" || part == "outset" || part == "groove" || part == "ridge" {
			borderStyle = part
		} else {
			color = part
		}
	}
	style.Set(marker+"-width", width)
	style.Set(marker+"-style", borderStyle)
	style.Set(marker+"-color", color)
}

func expandBorderSideProperty(style *Style, property, value string) {
	// property is "border-top", "border-right", etc.
	side := strings.TrimPrefix(property, "border-")

	// Reset all sub-properties to their initial values first
	// Initial values: width=medium (3px), style=none, color=currentcolor
	style.Set("border-"+side+"-width", "3px") // medium = 3px
	style.Set("border-"+side+"-style", "none")
	style.Set("border-"+side+"-color", "currentcolor")

	// Now apply the specified values
	parts := strings.Fields(value)
	for _, part := range parts {
		if part == "0" {
			style.Set("border-"+side+"-width", "0")
		} else if bw, ok := borderWidthKeyword(part); ok {
			style.Set("border-"+side+"-width", bw)
		} else if _, ok := ParseLength(part); ok {
			style.Set("border-"+side+"-width", part)
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" || part == "none" || part == "inset" || part == "outset" || part == "groove" || part == "ridge" {
			style.Set("border-"+side+"-style", part)
		} else {
			style.Set("border-"+side+"-color", part)
		}
	}
}

// expandFontProperty expands the font shorthand.
// Format: [style] [variant] [weight] size[/line-height] family[, family...]
func expandFontProperty(style *Style, value string) {
	parts := strings.Fields(value)
	if len(parts) < 2 {
		return
	}

	i := 0
	// Skip optional font-style
	if i < len(parts) && (parts[i] == "italic" || parts[i] == "oblique" || parts[i] == "normal") {
		style.Set("font-style", parts[i])
		i++
	}
	// Skip optional font-variant
	if i < len(parts) && parts[i] == "small-caps" {
		style.Set("font-variant", parts[i])
		style.Set("font-variant-caps", parts[i])
		i++
	}
	// Skip optional font-weight
	if i < len(parts) {
		switch parts[i] {
		case "bold", "bolder", "lighter", "100", "200", "300", "400", "500", "600", "700", "800", "900":
			style.Set("font-weight", parts[i])
			i++
		}
	}
	// Next should be size[/line-height]
	if i < len(parts) {
		sizeStr := parts[i]
		if idx := strings.Index(sizeStr, "/"); idx >= 0 {
			style.Set("font-size", sizeStr[:idx])
			style.Set("line-height", sizeStr[idx+1:])
		} else {
			style.Set("font-size", sizeStr)
			// CSS spec: font shorthand resets line-height to normal when
			// not explicitly specified (CSS Fonts 4 §4).
			style.Set("line-height", "normal")
		}
		i++
	}
	// Remaining is font-family
	if i < len(parts) {
		family := strings.Join(parts[i:], " ")
		style.Set("font-family", family)
	}
}

// expandBackgroundProperty expands the background shorthand.
// Handles comma-separated layers: each layer parsed independently, longhands
// stored as comma-joined strings.
func expandBackgroundProperty(style *Style, value string) {
	// If value contains var(), defer expansion
	if strings.Contains(value, "var(") {
		style.Set("background-color", value)
		return
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "none" {
		style.Set("background-color", "transparent")
		style.Set("background-image", "none")
		return
	}

	// Split into comma-separated layers (depth-aware).
	layers := splitCommaSeparated(trimmed)

	// Per-layer accumulators for each longhand.
	images := make([]string, len(layers))
	repeats := make([]string, len(layers))
	positions := make([]string, len(layers))
	sizes := make([]string, len(layers))
	origins := make([]string, len(layers))
	clips := make([]string, len(layers))
	bgColor := ""

	for li, layerStr := range layers {
		layerStr = strings.TrimSpace(layerStr)
		remaining := layerStr

		// Extract image-set() first
		lowerRemaining := strings.ToLower(remaining)
		if strings.Contains(lowerRemaining, "image-set(") {
			setIdx := strings.Index(lowerRemaining, "image-set(")
			prefixStart := setIdx
			if setIdx >= 8 && lowerRemaining[setIdx-8:setIdx] == "-webkit-" {
				prefixStart = setIdx - 8
			}
			depth := 1
			setEnd := setIdx + len("image-set(")
			for setEnd < len(remaining) && depth > 0 {
				switch remaining[setEnd] {
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth > 0 {
					setEnd++
				}
			}
			setEnd++
			imageSetExpr := remaining[prefixStart:setEnd]
			if url, ok := extractImageSetFirstURL(imageSetExpr); ok {
				images[li] = "url(\"" + url + "\")"
				remaining = remaining[:prefixStart] + remaining[setEnd:]
			}
		}

		// Extract url()
		if images[li] == "" {
			if urlStart := strings.Index(remaining, "url("); urlStart >= 0 {
				depth := 0
				urlEnd := -1
				for i := urlStart + 4; i < len(remaining); i++ {
					if remaining[i] == '(' {
						depth++
					} else if remaining[i] == ')' {
						if depth == 0 {
							urlEnd = i + 1
							break
						}
						depth--
					}
				}
				if urlEnd > urlStart {
					images[li] = remaining[urlStart:urlEnd]
					remaining = remaining[:urlStart] + remaining[urlEnd:]
				}
			}
		}

		// Extract gradient function
		if images[li] == "" {
			for _, gradPrefix := range []string{"repeating-linear-gradient(", "repeating-radial-gradient(", "repeating-conic-gradient(", "conic-gradient(", "linear-gradient(", "radial-gradient("} {
				if idx := strings.Index(remaining, gradPrefix); idx >= 0 {
					depth := 0
					gradEnd := -1
					for i := idx + len(gradPrefix) - 1; i < len(remaining); i++ {
						if remaining[i] == '(' {
							depth++
						} else if remaining[i] == ')' {
							depth--
							if depth == 0 {
								gradEnd = i + 1
								break
							}
						}
					}
					if gradEnd > idx {
						images[li] = remaining[idx:gradEnd]
						remaining = remaining[:idx] + remaining[gradEnd:]
					}
					break
				}
			}
		}

		// Extract color functions before field-splitting
		colorFound := false
		colorValue := ""
		for _, prefix := range []string{"rgba(", "rgb(", "hsla(", "hsl(", "oklch(", "lch(", "lab(", "hwb(", "color-mix(", "color("} {
			if idx := strings.Index(remaining, prefix); idx >= 0 {
				depth := 0
				end := -1
				for j := idx; j < len(remaining); j++ {
					switch remaining[j] {
					case '(':
						depth++
					case ')':
						depth--
						if depth == 0 {
							end = j
						}
					}
					if end >= 0 {
						break
					}
				}
				if end >= 0 {
					colorFunc := remaining[idx : end+1]
					if _, ok := ParseColor(colorFunc); ok {
						colorFound = true
						colorValue = colorFunc
						remaining = remaining[:idx] + remaining[end+1:]
					}
				}
				break
			}
		}

		// Tokenize remaining with parenthesis awareness so that
		// calc() expressions aren't broken by whitespace splitting.
		parts := splitBackgroundPositionTokens(remaining)
		var positionTokens []string
		var boxValues []string
		// Per CSS Backgrounds 3 §3.10 shorthand, <repeat-style> may be one
		// or two axis keywords. Collect consecutive repeat keywords and
		// join them so longhand parsing sees the full per-layer value.
		var repeatTokens []string
		for _, part := range parts {
			if isAxisRepeatToken(part) || part == "repeat-x" || part == "repeat-y" {
				repeatTokens = append(repeatTokens, part)
			} else if part == "border-box" || part == "padding-box" || part == "content-box" {
				boxValues = append(boxValues, part)
			} else if _, ok := ParseColor(part); ok {
				if !colorFound {
					colorFound = true
					colorValue = part
				}
			} else if part == "transparent" {
				if !colorFound {
					colorFound = true
					colorValue = "transparent"
				}
			} else if _, ok := ParseLength(part); ok {
				positionTokens = append(positionTokens, part)
			} else if strings.HasPrefix(part, "calc(") {
				positionTokens = append(positionTokens, part)
			} else if strings.HasSuffix(part, "%") {
				positionTokens = append(positionTokens, part)
			} else if part == "center" || part == "left" || part == "right" || part == "top" || part == "bottom" {
				positionTokens = append(positionTokens, part)
			} else if part == "fixed" || part == "scroll" || part == "local" {
				style.Set("background-attachment", part)
			}
		}

		// Up to two axis keywords form a <repeat-style>; the "repeat-x" /
		// "repeat-y" shorthand tokens are single-token only.
		if len(repeatTokens) > 0 {
			n := len(repeatTokens)
			if n > 2 {
				n = 2
			}
			repeats[li] = strings.Join(repeatTokens[:n], " ")
		}

		if len(boxValues) >= 1 {
			origins[li] = boxValues[0]
			if len(boxValues) >= 2 {
				clips[li] = boxValues[1]
			} else {
				clips[li] = boxValues[0]
			}
		}

		// CSS spec: background-color only in the last (bottommost) layer
		if colorFound {
			if li == len(layers)-1 {
				bgColor = colorValue
			}
		}

		if len(positionTokens) > 0 {
			positions[li] = strings.Join(positionTokens, " ")
		}

		if images[li] == "" {
			images[li] = "none"
		}
	}

	// Store longhands as comma-joined strings.
	style.Set("background-image", strings.Join(images, ", "))
	if joinNonEmpty(repeats) != "" {
		style.Set("background-repeat", joinNonEmpty(repeats))
	}
	if joinNonEmpty(positions) != "" {
		style.Set("background-position", joinNonEmpty(positions))
	}
	if joinNonEmpty(sizes) != "" {
		style.Set("background-size", joinNonEmpty(sizes))
	}
	if joinNonEmpty(origins) != "" {
		style.Set("background-origin", joinNonEmpty(origins))
	}
	if joinNonEmpty(clips) != "" {
		style.Set("background-clip", joinNonEmpty(clips))
	}
	if bgColor != "" {
		style.Set("background-color", bgColor)
	}
}

// joinNonEmpty joins strings with commas, skipping empty entries.
// Returns empty string if all entries are empty.
func joinNonEmpty(parts []string) string {
	hasNonEmpty := false
	for _, p := range parts {
		if p != "" {
			hasNonEmpty = true
			break
		}
	}
	if !hasNonEmpty {
		return ""
	}
	return strings.Join(parts, ", ")
}

// Phase 19: Enhanced color with alpha channel
type Color struct {
	R, G, B uint8
	A       float64 // Alpha: 0.0 (transparent) to 1.0 (opaque), default 1.0
}

// parseSpaceSeparatedColorArgs parses space-separated color function arguments
// with optional slash for alpha: "L C H" or "L C H / alpha"
// Returns the parts before the slash and the alpha value (default 1.0).
//
// CSS comments (`/* ... */`) inside the argument string are stripped before
// tokenization so that `rgb(10/* c */175/* c */10/100%)` parses identically
// to `rgb(10 175 10 / 100%)`.
func parseSpaceSeparatedColorArgs(inner string) (parts []string, alpha float64) {
	alpha = 1.0
	inner = stripCSSComments(inner)
	// Handle the slash separator for alpha
	if idx := strings.Index(inner, "/"); idx >= 0 {
		alpha = parseAlpha(inner[idx+1:])
		inner = inner[:idx]
	}
	parts = strings.Fields(inner)
	return
}

// parseAlpha parses a CSS alpha component, which may be a <number> in
// [0,1] or a <percentage> in [0%, 100%] per CSS Color 4 §1.4. Returns
// 1.0 on parse failure (caller treats this as the default).
func parseAlpha(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1.0
	}
	return parseColorFloat01(s, 1.0)
}

// parseColorFloat01 parses a color component value as a fraction in [0,1].
// If the value ends with '%', it is divided by maxPercent to get [0,1].
// Otherwise it is divided by maxValue to get [0,1].
func parseColorFloat01(s string, maxValue float64) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		var v float64
		fmt.Sscanf(strings.TrimSuffix(s, "%"), "%f", &v)
		return v / 100.0
	}
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v / maxValue
}

// parseRGBByte parses one R/G/B component of an rgb()/rgba() functional
// notation. Accepts integer/number form (0-255) or percent form (0%-100%) per
// CSS Color Level 4 §"rgb()". Returns the byte value and a validity flag
// (false if out of range). Mirrors Blink's CSSPropertyParserHelpers handling
// where percent and number forms produce the same final byte after rounding.
func parseRGBByte(s string) (uint8, bool) {
	v := parseColorFloat01(s, 255.0)
	if v < 0 || v > 1 {
		return 0, false
	}
	return uint8(math.Round(v * 255)), true
}

// parseSpaceSeparatedRGB parses CSS4 space-separated rgb/rgba syntax:
// "R G B" or "R G B / A" where values can be numbers (0-255) or percentages.
func parseSpaceSeparatedRGB(inner string) (Color, bool) {
	parts, alpha := parseSpaceSeparatedColorArgs(inner)
	if len(parts) < 3 {
		return Color{}, false
	}
	r := int(math.Round(parseColorFloat01(parts[0], 255.0) * 255))
	g := int(math.Round(parseColorFloat01(parts[1], 255.0) * 255))
	b := int(math.Round(parseColorFloat01(parts[2], 255.0) * 255))
	if r < 0 {
		r = 0
	} else if r > 255 {
		r = 255
	}
	if g < 0 {
		g = 0
	} else if g > 255 {
		g = 255
	}
	if b < 0 {
		b = 0
	} else if b > 255 {
		b = 255
	}
	return Color{uint8(r), uint8(g), uint8(b), alpha}, true
}

// parseLabComponent parses a Lab a/b component.
// Can be a raw number (-125 to 125) or a percentage (-100% to 100% of maxVal).
func parseLabComponent(s string, maxVal float64) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		var v float64
		fmt.Sscanf(strings.TrimSuffix(s, "%"), "%f", &v)
		return v / 100.0 * maxVal
	}
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}

// parseHueDegrees parses a hue value in degrees.
// Accepts plain numbers (degrees), "Ndeg", "Ngrad", "Nrad", "Nturn"
// per CSS Values 4 §6.1 <angle>. The "grad" check must precede "rad"
// because HasSuffix("Xgrad", "rad") is also true.
func parseHueDegrees(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "deg") {
		var v float64
		fmt.Sscanf(strings.TrimSuffix(s, "deg"), "%f", &v)
		return v
	}
	if strings.HasSuffix(s, "grad") {
		var v float64
		fmt.Sscanf(strings.TrimSuffix(s, "grad"), "%f", &v)
		return v * 360.0 / 400.0
	}
	if strings.HasSuffix(s, "rad") {
		var v float64
		fmt.Sscanf(strings.TrimSuffix(s, "rad"), "%f", &v)
		return v * 180.0 / math.Pi
	}
	if strings.HasSuffix(s, "turn") {
		var v float64
		fmt.Sscanf(strings.TrimSuffix(s, "turn"), "%f", &v)
		return v * 360.0
	}
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}

// parseHSLArgs parses the inner argument string of an hsl()/hsla()
// function (CSS comments already stripped by caller). Accepts both the
// legacy comma-separated form `H, S%, L%[, A]` and the modern
// space-separated form `H S% L%[ / A]`. Returns (hue in degrees,
// saturation 0..1, lightness 0..1, alpha 0..1).
//
// Modern form is detected by the absence of a comma at depth-0 in the
// inner string — comments have already been stripped, so this is safe.
func parseHSLArgs(inner string) (h, s, l, a float64, ok bool) {
	a = 1.0
	var hStr, sStr, lStr string
	if strings.Contains(inner, ",") {
		parts := strings.Split(inner, ",")
		if len(parts) < 3 {
			return
		}
		hStr = strings.TrimSpace(parts[0])
		sStr = strings.TrimSpace(parts[1])
		lStr = strings.TrimSpace(parts[2])
		if len(parts) >= 4 {
			a = parseAlpha(parts[3])
		}
	} else {
		var parts []string
		parts, a = parseSpaceSeparatedColorArgs(inner)
		if len(parts) < 3 {
			return
		}
		hStr = parts[0]
		sStr = parts[1]
		lStr = parts[2]
	}
	h = parseHueDegrees(hStr)
	s = parseColorFloat01(sStr, 100.0)
	l = parseColorFloat01(lStr, 100.0)
	ok = true
	return
}

// hslToColor converts HSL components (hue in degrees, s/l in [0,1],
// alpha in [0,1]) to an sRGB Color. Mirrors CSS Color 4 §5.2's reference
// HSL → RGB algorithm and Blink's `HSLToRGB` in color.cc.
func hslToColor(h, s, l, a float64) Color {
	c := (1 - math.Abs(2*l-1)) * s
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r1, g1, b1 float64
	switch {
	case h < 60:
		r1, g1, b1 = c, x, 0
	case h < 120:
		r1, g1, b1 = x, c, 0
	case h < 180:
		r1, g1, b1 = 0, c, x
	case h < 240:
		r1, g1, b1 = 0, x, c
	case h < 300:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	return Color{
		R: uint8(math.Round((r1 + m) * 255)),
		G: uint8(math.Round((g1 + m) * 255)),
		B: uint8(math.Round((b1 + m) * 255)),
		A: a,
	}
}

// parseColorFunction parses a CSS color() function and returns RGBA uint8 components.
// Supports color spaces: srgb, display-p3, a98-rgb, rec2020, xyz-d65.
// Format: color(colorspace r g b) or color(colorspace r g b / alpha)
func parseColorFunction(s string) (uint8, uint8, uint8, uint8, bool) {
	start := strings.Index(s, "(")
	end := strings.LastIndex(s, ")")
	if start < 0 || end < 0 || end <= start {
		return 0, 0, 0, 255, false
	}
	inner := strings.TrimSpace(s[start+1 : end])

	parts, alpha := parseSpaceSeparatedColorArgs(inner)
	if len(parts) < 4 {
		return 0, 0, 0, 255, false
	}

	colorSpace := strings.ToLower(parts[0])
	var r, g, b float64
	fmt.Sscanf(parts[1], "%f", &r)
	fmt.Sscanf(parts[2], "%f", &g)
	fmt.Sscanf(parts[3], "%f", &b)

	// Convert to sRGB based on the color space
	switch colorSpace {
	case "srgb":
		// Already sRGB — just clamp
		r = colorClamp01(r)
		g = colorClamp01(g)
		b = colorClamp01(b)
	case "display-p3":
		r, g, b = p3ToSRGB(r, g, b)
	case "a98-rgb":
		r, g, b = a98RGBToSRGB(r, g, b)
	case "rec2020":
		r, g, b = rec2020ToSRGB(r, g, b)
	case "xyz-d65":
		r, g, b = xyzD65ToSRGB(r, g, b)
	case "xyz", "xyz-d50":
		// Approximate: treat as xyz-d65 (close enough for rendering)
		r, g, b = xyzD65ToSRGB(r, g, b)
	default:
		return 0, 0, 0, 255, false
	}

	clampU8 := func(v float64) uint8 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 255
		}
		return uint8(v*255 + 0.5)
	}
	return clampU8(r), clampU8(g), clampU8(b), clampU8(alpha), true
}

// systemColors holds RGB values for CSS Color 4 §9 system colors (light theme).
//
// Blink reference: `third_party/blink/renderer/platform/theme/web_theme_engine_default.cc`
// (`WebThemeEngineDefault::GetSystemColor`, Chromium @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). The UA picks the actual RGB; the
// values below mirror Blink's light-theme defaults. Tests only require that
// every deprecated alias resolves to the SAME RGB as its modern equivalent
// (CSS Color 4 §10), which is guaranteed by routing both through this single
// map via `deprecatedSystemColorAliases`.
//
// Keys are lowercase (CSS ident keywords are ASCII case-insensitive).
var systemColors = map[string]Color{
	"accentcolor":      {0, 95, 204, 1.0},
	"accentcolortext":  {255, 255, 255, 1.0},
	"activetext":       {255, 0, 0, 1.0},
	"buttonborder":     {118, 118, 118, 1.0},
	"buttonface":       {239, 239, 239, 1.0},
	"buttontext":       {0, 0, 0, 1.0},
	"canvas":           {255, 255, 255, 1.0},
	"canvastext":       {0, 0, 0, 1.0},
	"field":            {255, 255, 255, 1.0},
	"fieldtext":        {0, 0, 0, 1.0},
	"graytext":         {128, 128, 128, 1.0},
	"highlight":        {180, 213, 255, 1.0},
	"highlighttext":    {0, 0, 0, 1.0},
	"linktext":         {0, 0, 238, 1.0},
	"mark":             {255, 255, 0, 1.0},
	"marktext":         {0, 0, 0, 1.0},
	"selecteditem":     {180, 213, 255, 1.0},
	"selecteditemtext": {0, 0, 0, 1.0},
	"visitedtext":      {85, 26, 139, 1.0},
}

// deprecatedSystemColorAliases maps the 23 deprecated CSS2 system colors
// to their modern CSS Color 4 §9 equivalents per CSS Color 4 §10
// (https://drafts.csswg.org/css-color-4/#deprecated-system-colors).
//
// Blink reference: `third_party/blink/renderer/core/css/css_system_color.cc`
// (alias table, Chromium @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// Keys are lowercase (CSS ident keywords are ASCII case-insensitive); each
// value MUST exist as a key in `systemColors`.
var deprecatedSystemColorAliases = map[string]string{
	"activeborder":        "buttonborder",
	"activecaption":       "canvas",
	"appworkspace":        "canvas",
	"background":          "canvas",
	"buttonhighlight":     "buttonface",
	"buttonshadow":        "buttonface",
	"captiontext":         "canvastext",
	"inactiveborder":      "buttonborder",
	"inactivecaption":     "canvas",
	"inactivecaptiontext": "graytext",
	"infobackground":      "canvas",
	"infotext":            "canvastext",
	"menu":                "canvas",
	"menutext":            "canvastext",
	"scrollbar":           "canvas",
	"threeddarkshadow":    "buttonborder",
	"threedface":          "buttonface",
	"threedhighlight":     "buttonborder",
	"threedlightshadow":   "buttonborder",
	"threedshadow":        "buttonborder",
	"window":              "canvas",
	"windowframe":         "buttonborder",
	"windowtext":          "canvastext",
}

func ParseColor(colorStr string) (Color, bool) {
	colorStr = strings.TrimSpace(colorStr)

	// Reject quoted values — CSS color values are never strings
	if strings.HasPrefix(colorStr, "'") || strings.HasPrefix(colorStr, "\"") {
		return Color{}, false
	}

	colorStr = strings.ToLower(colorStr)

	// Handle transparent
	if colorStr == "transparent" {
		return Color{0, 0, 0, 0.0}, true
	}

	// Handle rgb() / rgba() format. CSS Color 4 §5.1 unifies the two —
	// both accept legacy comma form and modern slash-alpha form, and
	// `rgb()` may carry alpha.
	if (strings.HasPrefix(colorStr, "rgb(") || strings.HasPrefix(colorStr, "rgba(")) && strings.HasSuffix(colorStr, ")") {
		var values string
		if strings.HasPrefix(colorStr, "rgba(") {
			values = strings.TrimSuffix(strings.TrimPrefix(colorStr, "rgba("), ")")
		} else {
			values = strings.TrimSuffix(strings.TrimPrefix(colorStr, "rgb("), ")")
		}
		// Strip comments before splitting so `rgb(10,/* x */175,/* y */10)`
		// is treated as `rgb(10, 175, 10)`.
		values = stripCSSComments(values)
		parts := strings.Split(values, ",")
		if len(parts) == 3 {
			r, rOK := parseRGBByte(parts[0])
			g, gOK := parseRGBByte(parts[1])
			b, bOK := parseRGBByte(parts[2])
			if rOK && gOK && bOK {
				return Color{r, g, b, 1.0}, true
			}
		} else if len(parts) == 4 {
			// Legacy comma form with alpha (Color 4 §5.1 allows `rgb()`
			// to carry alpha; `rgba()` is an alias).
			r, rOK := parseRGBByte(parts[0])
			g, gOK := parseRGBByte(parts[1])
			b, bOK := parseRGBByte(parts[2])
			a := parseAlpha(parts[3])
			if rOK && gOK && bOK {
				return Color{r, g, b, a}, true
			}
		} else if len(parts) == 1 {
			// Modern space-separated form: rgb(R G B) or rgb(R G B / A).
			if c, ok := parseSpaceSeparatedRGB(values); ok {
				return c, true
			}
		}
	}

	// Handle hsl()/hsla() format — CSS Color 4 §5.2.
	if (strings.HasPrefix(colorStr, "hsl(") || strings.HasPrefix(colorStr, "hsla(")) && strings.HasSuffix(colorStr, ")") {
		inner := colorStr
		if strings.HasPrefix(inner, "hsla(") {
			inner = strings.TrimSuffix(strings.TrimPrefix(inner, "hsla("), ")")
		} else {
			inner = strings.TrimSuffix(strings.TrimPrefix(inner, "hsl("), ")")
		}
		// Strip comments before form-detection so comments-as-separators
		// don't break the comma/space distinction.
		inner = stripCSSComments(inner)
		if h, sat, l, a, ok := parseHSLArgs(inner); ok {
			return hslToColor(h, sat, l, a), true
		}
	}

	// Handle lab() format: lab(L a b) or lab(L a b / alpha)
	// L is 0-100 or 0%-100%, a is -125 to 125, b is -125 to 125
	if strings.HasPrefix(colorStr, "lab(") && strings.HasSuffix(colorStr, ")") {
		inner := colorStr[4 : len(colorStr)-1]
		parts, alpha := parseSpaceSeparatedColorArgs(inner)
		if len(parts) >= 3 {
			L := parseColorFloat01(parts[0], 100.0) * 100.0
			a := parseLabComponent(parts[1], 125.0)
			bLab := parseLabComponent(parts[2], 125.0)
			r, g, b := labToRGB(L, a, bLab)
			return Color{
				R: uint8(math.Round(r * 255)),
				G: uint8(math.Round(g * 255)),
				B: uint8(math.Round(b * 255)),
				A: alpha,
			}, true
		}
	}

	// Handle oklch() format: oklch(L C H) or oklch(L C H / alpha)
	// L is 0-1 or 0%-100%, C is 0-0.4, H is 0-360
	if strings.HasPrefix(colorStr, "oklch(") && strings.HasSuffix(colorStr, ")") {
		inner := colorStr[6 : len(colorStr)-1]
		parts, alpha := parseSpaceSeparatedColorArgs(inner)
		if len(parts) >= 3 {
			L := parseColorFloat01(parts[0], 1.0)
			C := parseColorFloat01(parts[1], 0.4)
			H := parseHueDegrees(parts[2])
			r, g, b := oklchToRGB(L, C, H)
			return Color{
				R: uint8(math.Round(r * 255)),
				G: uint8(math.Round(g * 255)),
				B: uint8(math.Round(b * 255)),
				A: alpha,
			}, true
		}
	}

	// Handle lch() format: lch(L C H) or lch(L C H / alpha)
	// L is 0-100, C is 0-230, H is 0-360
	if strings.HasPrefix(colorStr, "lch(") && strings.HasSuffix(colorStr, ")") {
		inner := colorStr[4 : len(colorStr)-1]
		parts, alpha := parseSpaceSeparatedColorArgs(inner)
		if len(parts) >= 3 {
			L := parseColorFloat01(parts[0], 100.0)
			C := parseColorFloat01(parts[1], 230.0)
			H := parseHueDegrees(parts[2])
			r, g, b := lchToRGB(L, C, H)
			return Color{
				R: uint8(math.Round(r * 255)),
				G: uint8(math.Round(g * 255)),
				B: uint8(math.Round(b * 255)),
				A: alpha,
			}, true
		}
	}

	// Handle hwb() format: hwb(H W% B%) or hwb(H W% B% / alpha)
	// H is 0-360, W and B are percentages 0-100
	if strings.HasPrefix(colorStr, "hwb(") && strings.HasSuffix(colorStr, ")") {
		inner := colorStr[4 : len(colorStr)-1]
		parts, alpha := parseSpaceSeparatedColorArgs(inner)
		if len(parts) >= 3 {
			H := parseHueDegrees(parts[0])
			W := parseColorFloat01(parts[1], 100.0) * 100.0 // convert fraction to percentage
			B := parseColorFloat01(parts[2], 100.0) * 100.0
			r, g, b := hwbToRGB(H, W, B)
			return Color{
				R: uint8(math.Round(r * 255)),
				G: uint8(math.Round(g * 255)),
				B: uint8(math.Round(b * 255)),
				A: alpha,
			}, true
		}
	}

	// Handle color() function: color(colorspace r g b [/ alpha])
	if strings.HasPrefix(colorStr, "color(") && strings.HasSuffix(colorStr, ")") {
		if r, g, b, a, ok := parseColorFunction(colorStr); ok {
			return Color{r, g, b, float64(a) / 255.0}, true
		}
	}

	// Handle color-mix() format: color-mix(in colorspace, color1 [pct%], color2 [pct%])
	if strings.HasPrefix(colorStr, "color-mix(") && strings.HasSuffix(colorStr, ")") {
		return parseColorMix(colorStr)
	}

	// Try hex color first (#RGB or #RRGGBB)
	if strings.HasPrefix(colorStr, "#") {
		hex := colorStr[1:]
		var r, g, b uint8

		if len(hex) == 3 {
			// #RGB format - expand to #RRGGBB
			n, _ := fmt.Sscanf(hex, "%1x%1x%1x", &r, &g, &b)
			if n != 3 {
				return Color{}, false
			}
			r = r*16 + r
			g = g*16 + g
			b = b*16 + b
			return Color{r, g, b, 1.0}, true
		} else if len(hex) == 6 {
			// #RRGGBB format
			n, _ := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
			if n != 3 {
				return Color{}, false
			}
			return Color{r, g, b, 1.0}, true
		}
	}

	// Try named colors
	namedColors := map[string]Color{
		"red":            {255, 0, 0, 1.0},
		"green":          {0, 128, 0, 1.0},
		"blue":           {0, 0, 255, 1.0},
		"yellow":         {255, 255, 0, 1.0},
		"cyan":           {0, 255, 255, 1.0},
		"aqua":           {0, 255, 255, 1.0},
		"magenta":        {255, 0, 255, 1.0},
		"fuchsia":        {255, 0, 255, 1.0},
		"white":          {255, 255, 255, 1.0},
		"black":          {0, 0, 0, 1.0},
		"gray":           {128, 128, 128, 1.0},
		"grey":           {128, 128, 128, 1.0},
		"orange":         {255, 165, 0, 1.0},
		"purple":         {128, 0, 128, 1.0},
		"pink":           {255, 192, 203, 1.0},
		"brown":          {165, 42, 42, 1.0},
		"lime":           {0, 255, 0, 1.0},
		"navy":           {0, 0, 128, 1.0},
		"teal":           {0, 128, 128, 1.0},
		"silver":         {192, 192, 192, 1.0},
		"maroon":         {128, 0, 0, 1.0},
		"olive":          {128, 128, 0, 1.0},
		"lightblue":      {173, 216, 230, 1.0},
		"lightgreen":     {144, 238, 144, 1.0},
		"lightgray":      {211, 211, 211, 1.0},
		"lightgrey":      {211, 211, 211, 1.0},
		"lightyellow":    {255, 255, 224, 1.0},
		"lightcoral":     {240, 128, 128, 1.0},
		"lightcyan":      {224, 255, 255, 1.0},
		"lightpink":      {255, 182, 193, 1.0},
		"turquoise":      {64, 224, 208, 1.0},
		"coral":          {255, 127, 80, 1.0},
		"violet":         {238, 130, 238, 1.0},
		"bisque":         {255, 228, 196, 1.0},
		"limegreen":      {50, 205, 50, 1.0},
		"darkgreen":      {0, 100, 0, 1.0},
		"darkblue":       {0, 0, 139, 1.0},
		"darkred":        {139, 0, 0, 1.0},
		"darkgray":       {169, 169, 169, 1.0},
		"darkgrey":       {169, 169, 169, 1.0},
		"dimgray":        {105, 105, 105, 1.0},
		"dimgrey":        {105, 105, 105, 1.0},
		"gold":           {255, 215, 0, 1.0},
		"indigo":         {75, 0, 130, 1.0},
		"khaki":          {240, 230, 140, 1.0},
		"lavender":       {230, 230, 250, 1.0},
		"salmon":         {250, 128, 114, 1.0},
		"crimson":        {220, 20, 60, 1.0},
		"tomato":         {255, 99, 71, 1.0},
		"skyblue":        {135, 206, 235, 1.0},
		"steelblue":      {70, 130, 180, 1.0},
		"slategray":      {112, 128, 144, 1.0},
		"slategrey":      {112, 128, 144, 1.0},
		"whitesmoke":     {245, 245, 245, 1.0},
		"ivory":          {255, 255, 240, 1.0},
		"beige":          {245, 245, 220, 1.0},
		"wheat":          {245, 222, 179, 1.0},
		"tan":            {210, 180, 140, 1.0},
		"chocolate":      {210, 105, 30, 1.0},
		"firebrick":      {178, 34, 34, 1.0},
		"orangered":      {255, 69, 0, 1.0},
		"deeppink":       {255, 20, 147, 1.0},
		"hotpink":        {255, 105, 180, 1.0},
		"mediumblue":     {0, 0, 205, 1.0},
		"royalblue":      {65, 105, 225, 1.0},
		"dodgerblue":     {30, 144, 255, 1.0},
		"cornflowerblue": {100, 149, 237, 1.0},
		"darkviolet":     {148, 0, 211, 1.0},
		"plum":           {221, 160, 221, 1.0},
		"orchid":         {218, 112, 214, 1.0},
		"sienna":         {160, 82, 45, 1.0},
		"peru":           {205, 133, 63, 1.0},
		"linen":          {250, 240, 230, 1.0},
		"seagreen":       {46, 139, 87, 1.0},
		"forestgreen":    {34, 139, 34, 1.0},
		"olivedrab":      {107, 142, 35, 1.0},
		"yellowgreen":    {154, 205, 50, 1.0},
		"darkslategray":  {47, 79, 79, 1.0},
		"darkslategrey":  {47, 79, 79, 1.0},
		"darkorange":     {255, 140, 0, 1.0},
		"darkcyan":       {0, 139, 139, 1.0},
		"aquamarine":     {127, 255, 212, 1.0},
		"rosybrown":      {188, 143, 143, 1.0},
		"thistle":        {216, 191, 216, 1.0},
		"gainsboro":      {220, 220, 220, 1.0},
		"aliceblue":      {240, 248, 255, 1.0},
		"ghostwhite":     {248, 248, 255, 1.0},
		"honeydew":       {240, 255, 240, 1.0},
		"seashell":       {255, 245, 238, 1.0},
		"mintcream":      {245, 255, 250, 1.0},
		"snow":           {255, 250, 250, 1.0},
		"floralwhite":    {255, 250, 240, 1.0},
		"oldlace":        {253, 245, 230, 1.0},
		"papayawhip":     {255, 239, 213, 1.0},
		"blanchedalmond": {255, 235, 205, 1.0},
		"moccasin":       {255, 228, 181, 1.0},
		"navajowhite":    {255, 222, 173, 1.0},
		"peachpuff":      {255, 218, 185, 1.0},
		"mistyrose":      {255, 228, 225, 1.0},
		"antiquewhite":   {250, 235, 215, 1.0},
	}
	if color, ok := namedColors[colorStr]; ok {
		return color, true
	}

	// CSS Color 4 §9 system colors (modern) and §10 deprecated aliases.
	// Aliases resolve to a modern name first, then to its RGB. Both paths
	// share the single `systemColors` map so deprecated → modern always
	// produces the same Color value as the modern keyword (the contract
	// the deprecated-sameas-* WPT reftests verify).
	//
	// Blink reference: `core/css/properties/longhands/color.cc`
	// `ColorPropertyFunctions::ApplyValue` resolves the keyword via
	// `ColorFromCSSValue` and then `WebThemeEngine::GetSystemColor`
	// (Chromium @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	if modern, ok := deprecatedSystemColorAliases[colorStr]; ok {
		colorStr = modern
	}
	if color, ok := systemColors[colorStr]; ok {
		return color, true
	}
	return Color{}, false
}

// Phase 6: Text rendering helpers

// GetFontSize returns the *computed* (specified) font-size in pixels — the
// post-cascade value, BEFORE the `font-size-adjust` used-size adjustment.
// CSS box geometry (em-based margins, line-height, ch unit base, …) resolves
// against this value per Blink's observed behavior: only glyph rendering
// scales to the used size, while line boxes, margins, and inherited
// em resolution stay anchored to the specified size. Mirrors Blink's
// `FontDescription::SpecifiedSize()` at
// third_party/blink/renderer/platform/fonts/font_description.h @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Default: 16px when font-size is unset or invalid.
func (s *Style) GetFontSize() float64 {
	val, ok := s.Get("font-size")
	if !ok {
		return 16.0
	}
	// For font-size, em is relative to parent's font-size (use 16px as default parent)
	if size, ok := ParseLengthWithFontSize(val, 16.0); ok {
		// Cap at 500px to prevent OOM when rasterizing glyphs at absurd sizes.
		// Browsers cap similarly (Chrome: ~1000px). This affects rendering only;
		// the computed value is still correct for CSS calc()/inheritance purposes.
		if size > 500 {
			size = 500
		}
		return size
	}
	return 16.0
}

// GetUsedFontSize returns the *used* font-size in pixels — the specified
// size with CSS Fonts 5 §1.7 `font-size-adjust` applied. Returns
// [GetFontSize] when font-size-adjust is `none` or the layout-time
// metric pass has not yet populated UsedFontSize. Glyph rasterization
// and text shaping should pass this value to the shaper; box geometry
// stays on [GetFontSize] (see that accessor's contract).
//
// Mirrors Blink's `FontDescription::EffectiveSize()` in
// third_party/blink/renderer/platform/fonts/font_description.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, called from
// `Font::DrawText` to source the shaper's font size.
func (s *Style) GetUsedFontSize() float64 {
	if s.UsedFontSizeSet {
		return s.UsedFontSize
	}
	return s.GetFontSize()
}

// GetColor returns the text color (default: black)
func (s *Style) GetColor() Color {
	if colorStr, ok := s.Get("color"); ok {
		if color, ok := ParseColor(colorStr); ok {
			return color
		}
	}
	return Color{0, 0, 0, 1.0} // Default to black
}

// Phase 5: Float layout helpers

// FloatType represents the float property value
type FloatType string

const (
	FloatNone  FloatType = "none"
	FloatLeft  FloatType = "left"
	FloatRight FloatType = "right"
)

// GetFloat returns the float value (default: none).
// Resolves CSS logical values inline-start/inline-end using the computed direction.
func (s *Style) GetFloat() FloatType {
	if floatVal, ok := s.Get("float"); ok {
		switch floatVal {
		case "left":
			return FloatLeft
		case "right":
			return FloatRight
		case "inline-start":
			if s.GetDirection() == DirectionRTL {
				return FloatRight
			}
			return FloatLeft
		case "inline-end":
			if s.GetDirection() == DirectionRTL {
				return FloatLeft
			}
			return FloatRight
		}
	}
	return FloatNone
}

// ClearType represents the clear property value
type ClearType string

const (
	ClearNone  ClearType = "none"
	ClearLeft  ClearType = "left"
	ClearRight ClearType = "right"
	ClearBoth  ClearType = "both"
)

// GetClear returns the clear value (default: none).
// Resolves CSS logical values inline-start/inline-end/inline/block using the computed direction.
func (s *Style) GetClear() ClearType {
	if clearVal, ok := s.Get("clear"); ok {
		switch clearVal {
		case "left":
			return ClearLeft
		case "right":
			return ClearRight
		case "both":
			return ClearBoth
		case "inline-start":
			if s.GetDirection() == DirectionRTL {
				return ClearRight
			}
			return ClearLeft
		case "inline-end":
			if s.GetDirection() == DirectionRTL {
				return ClearLeft
			}
			return ClearRight
		case "inline":
			return ClearBoth
		}
	}
	return ClearNone
}

// Phase 6 Enhancements: Text styling

// TextAlign represents the text-align property value
type TextAlign string

const (
	TextAlignLeft   TextAlign = "left"
	TextAlignCenter TextAlign = "center"
	TextAlignRight  TextAlign = "right"
)

// GetTextAlign returns the text-align value (default: left)
func (s *Style) GetTextAlign() TextAlign {
	if align, ok := s.Get("text-align"); ok {
		switch align {
		case "center":
			return TextAlignCenter
		case "right":
			return TextAlignRight
		}
	}
	return TextAlignLeft
}

// GetTextAlignLast returns the text-align-last value (default: "auto").
// Controls alignment of the last line (or a line before a forced break).
func (s *Style) GetTextAlignLast() string {
	v, _ := s.Get("text-align-last")
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "left", "right", "center", "justify", "start", "end":
		return v
	default:
		return "auto"
	}
}

// Direction represents the CSS direction property
type Direction string

const (
	DirectionLTR Direction = "ltr"
	DirectionRTL Direction = "rtl"
)

// GetDirection returns the direction value (default: ltr)
func (s *Style) GetDirection() Direction {
	if dir, ok := s.Get("direction"); ok {
		if dir == "rtl" {
			return DirectionRTL
		}
	}
	return DirectionLTR
}

// FontWeight represents the font-weight property value
type FontWeight string

const (
	FontWeightNormal FontWeight = "normal"
	FontWeightBold   FontWeight = "bold"
)

// GetFontWeight returns the font-weight value (default: normal)
func (s *Style) GetFontWeight() FontWeight {
	if weight, ok := s.Get("font-weight"); ok {
		switch weight {
		case "bold", "700", "800", "900":
			return FontWeightBold
		}
	}
	return FontWeightNormal
}

// FontStyle represents the font-style property value
type FontStyle string

const (
	FontStyleNormal FontStyle = "normal"
	FontStyleItalic FontStyle = "italic"
)

// GetFontStyle returns the font-style value (default: normal)
func (s *Style) GetFontStyle() FontStyle {
	if style, ok := s.Get("font-style"); ok {
		switch style {
		case "italic", "oblique":
			return FontStyleItalic
		}
	}
	return FontStyleNormal
}

// GetFontVariantCaps returns the font-variant-caps value.
// Checks font-variant-caps first, then font-variant for legacy small-caps.
func (s *Style) GetFontVariantCaps() string {
	if v, ok := s.Get("font-variant-caps"); ok && v != "" && v != "normal" {
		return v
	}
	// Legacy: font-variant shorthand with small-caps
	if v, ok := s.Get("font-variant"); ok && v == "small-caps" {
		return "small-caps"
	}
	return "normal"
}

// GetFontVariantNumeric returns the font-variant-numeric value (default: normal).
func (s *Style) GetFontVariantNumeric() string {
	if v, ok := s.Get("font-variant-numeric"); ok && v != "" {
		return v
	}
	return "normal"
}

// InitialLetterValue holds the parsed initial-letter property.
type InitialLetterValue struct {
	Size float64 // how many lines tall
	Sink int     // how many lines to sink (default = floor(Size))
	Set  bool    // true if initial-letter is specified
}

// GetInitialLetter parses the initial-letter property.
// Syntax: initial-letter: <size> [<sink>]
// Where <size> is the number of lines the initial letter spans,
// and <sink> is the number of lines it sinks (defaults to floor(size)).
func (s *Style) GetInitialLetter() InitialLetterValue {
	v, ok := s.Get("initial-letter")
	if !ok {
		return InitialLetterValue{}
	}
	v = strings.TrimSpace(v)
	if v == "" || v == "normal" {
		return InitialLetterValue{}
	}
	parts := strings.Fields(v)
	size, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || size <= 0 {
		return InitialLetterValue{}
	}
	sink := int(math.Floor(size))
	if len(parts) >= 2 {
		if s2, err2 := strconv.Atoi(parts[1]); err2 == nil {
			sink = s2
		}
	}
	return InitialLetterValue{Size: size, Sink: sink, Set: true}
}

// GetFontVariantLigatures returns the font-variant-ligatures value.
// Default is "normal" (standard ligatures enabled).
func (s *Style) GetFontVariantLigatures() string {
	v, _ := s.Get("font-variant-ligatures")
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "normal"
	}
	return v
}

// FontKerning enumerates the three CSS Fonts 4 §6.2 font-kerning values.
// Mirrors Blink's FontDescription::Kerning enum
// (third_party/blink/renderer/platform/fonts/font_description.h, kAuto/kNormal/kNone
// at Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
type FontKerning string

const (
	FontKerningAuto   FontKerning = "auto"
	FontKerningNormal FontKerning = "normal"
	FontKerningNone   FontKerning = "none"
)

// GetFontKerning returns the font-kerning property value.
// Per CSS Fonts 4 §6.2 the default is `auto`, which UAs implement as
// `normal` for proportional fonts (HarfBuzz default = kern on). Only the
// explicit `none` keyword has observable effect in louis14 today; it pushes
// `kern=0`/`vkrn=0` into the shaper feature list.
func (s *Style) GetFontKerning() FontKerning {
	v, _ := s.Get("font-kerning")
	switch strings.TrimSpace(strings.ToLower(v)) {
	case string(FontKerningNone):
		return FontKerningNone
	case string(FontKerningNormal):
		return FontKerningNormal
	default:
		return FontKerningAuto
	}
}

// FontSynthesis enumerates which synthesis types the UA is permitted to apply.
// Each field is `true` when the corresponding longhand resolves to `auto`
// (UA may synthesize) and `false` when it resolves to `none` (UA must not
// synthesize that aspect). The shape mirrors Blink's `FontDescription`
// `Synthesis*` flag accessors at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
type FontSynthesis struct {
	Weight    bool
	Style     bool
	SmallCaps bool
	Position  bool
}

// GetFontSynthesis returns which font-synthesis aspects the UA may apply, per
// CSS Fonts 4 §6.6 (each longhand is `auto` | `none`, initial `auto`).
// Reads the four longhands populated by `expandFontSynthesisShorthand`;
// falls back to the initial value `auto` for any longhand not set on this
// style (which happens for the root element when no font-synthesis rule
// applied — inheritance brings the resolved value down to children).
func (s *Style) GetFontSynthesis() FontSynthesis {
	read := func(prop string) bool {
		v, ok := s.Get(prop)
		if !ok {
			return true // initial: auto → allowed
		}
		return strings.TrimSpace(strings.ToLower(v)) != "none"
	}
	return FontSynthesis{
		Weight:    read("font-synthesis-weight"),
		Style:     read("font-synthesis-style"),
		SmallCaps: read("font-synthesis-small-caps"),
		Position:  read("font-synthesis-position"),
	}
}

// FontSizeAdjustMetric identifies which font metric `font-size-adjust` is
// keyed off of. Mirrors Blink's `FontSizeAdjust::Metric` enum in
// third_party/blink/renderer/platform/fonts/font_size_adjust.h at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. Default is ex-height.
type FontSizeAdjustMetric int

const (
	FontSizeAdjustExHeight  FontSizeAdjustMetric = iota // x-height / font-size (default)
	FontSizeAdjustCapHeight                             // cap-height / font-size
	FontSizeAdjustChWidth                               // 0-glyph advance width / font-size
	FontSizeAdjustIcWidth                               // 水-glyph advance width / font-size
	FontSizeAdjustIcHeight                              // 水-glyph advance height / font-size
)

// FontSizeAdjust is the typed CSS Fonts 5 §1.7 `font-size-adjust` value.
// Grammar: `none | [ ex-height | cap-height | ch-width | ic-width | ic-height ]?
// [ from-font | <number> ]`.
//
// - None=true: property is `none` or unset; no adjustment applied.
// - FromFont=true: the value comes from the first font's actual aspect ratio
//   at runtime (no-op adjustment, but tracked for serialization).
// - Otherwise the used font-size becomes
//   specified_size * (Value / first_font.actual_aspect_ratio(Metric)).
type FontSizeAdjust struct {
	None     bool
	FromFont bool
	Metric   FontSizeAdjustMetric
	Value    float64
}

// IsActive reports whether the adjust value should scale the used font-size.
// `none` and `from-font` are non-scaling (from-font matches the first font's
// own aspect, yielding a no-op factor).
func (a FontSizeAdjust) IsActive() bool {
	return !a.None && !a.FromFont
}

// GetFontSizeAdjust parses the computed `font-size-adjust` value into the
// typed form. Returns `{None: true}` for unset or `none`. Invalid values
// also map to `{None: true}` per the cascade contract.
func (s *Style) GetFontSizeAdjust() FontSizeAdjust {
	v, ok := s.Get("font-size-adjust")
	if !ok {
		return FontSizeAdjust{None: true}
	}
	v = strings.TrimSpace(v)
	if v == "" || v == "none" {
		return FontSizeAdjust{None: true}
	}
	metric := FontSizeAdjustExHeight
	rest := v
	for _, name := range []struct {
		token  string
		metric FontSizeAdjustMetric
	}{
		{"ex-height", FontSizeAdjustExHeight},
		{"cap-height", FontSizeAdjustCapHeight},
		{"ch-width", FontSizeAdjustChWidth},
		{"ic-width", FontSizeAdjustIcWidth},
		{"ic-height", FontSizeAdjustIcHeight},
	} {
		if strings.HasPrefix(rest, name.token) {
			after := strings.TrimSpace(rest[len(name.token):])
			if after != rest {
				metric = name.metric
				rest = after
				break
			}
		}
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return FontSizeAdjust{None: true}
	}
	if rest == "from-font" {
		return FontSizeAdjust{FromFont: true, Metric: metric}
	}
	// Number, percentage, or calc() expression evaluating to <number>.
	// CSS Fonts 5 §1.7 grammar restricts the value to `<number>` / `from-font`;
	// calc() expressions are accepted because the spec defers to CSS Values 4
	// for `<number>` resolution (calc() that resolves to a number is valid).
	if strings.HasPrefix(rest, "calc(") && strings.HasSuffix(rest, ")") {
		// Reuse the calc evaluator with no length context — only arithmetic on
		// unitless numbers is meaningful for font-size-adjust.
		if f, ok := evalCalcFull(rest[5:len(rest)-1], calcContext{}); ok && f >= 0 {
			return FontSizeAdjust{Metric: metric, Value: f}
		}
		return FontSizeAdjust{None: true}
	}
	f, err := strconv.ParseFloat(rest, 64)
	if err != nil || f < 0 {
		return FontSizeAdjust{None: true}
	}
	return FontSizeAdjust{Metric: metric, Value: f}
}

// GetFontOpticalSizing returns whether optical sizing is enabled.
// Returns true unless font-optical-sizing is explicitly set to "none".
func (s *Style) GetFontOpticalSizing() bool {
	v, _ := s.Get("font-optical-sizing")
	return strings.TrimSpace(v) != "none"
}

// IsMonospaceFamily returns true if the computed font-family is a monospace font.
func (s *Style) IsMonospaceFamily() bool {
	if family, ok := s.Get("font-family"); ok {
		lower := strings.ToLower(family)
		for _, mono := range []string{"monospace", "mono", "courier", "consolas", "menlo", "monaco"} {
			if strings.Contains(lower, mono) {
				return true
			}
		}
	}
	return false
}

// IsAhemFamily returns true if the computed font-family is the Ahem test font.
// Ahem is a special test font where all glyphs are 1em x 1em squares, designed for CSS testing.
func (s *Style) IsAhemFamily() bool {
	if family, ok := s.Get("font-family"); ok {
		lower := strings.ToLower(family)
		return strings.Contains(lower, "ahem")
	}
	return false
}

// Phase 17: Text decoration

// TextDecoration represents the text-decoration property value
type TextDecoration string

const (
	TextDecorationNone        TextDecoration = "none"
	TextDecorationUnderline   TextDecoration = "underline"
	TextDecorationOverline    TextDecoration = "overline"
	TextDecorationLineThrough TextDecoration = "line-through"
)

// GetTextDecoration returns the text-decoration value (default: none)
func (s *Style) GetTextDecoration() TextDecoration {
	if decoration, ok := s.Get("text-decoration"); ok {
		switch decoration {
		case "underline":
			return TextDecorationUnderline
		case "overline":
			return TextDecorationOverline
		case "line-through":
			return TextDecorationLineThrough
		case "none":
			return TextDecorationNone
		}
	}
	return TextDecorationNone
}

// GetTextDecorationColor returns the text-decoration-color if set
func (s *Style) GetTextDecorationColor() (Color, bool) {
	if val, ok := s.Get("text-decoration-color"); ok {
		return ParseColor(val)
	}
	return Color{}, false
}

// TextShadow represents a single text-shadow layer
type TextShadow struct {
	OffsetX float64
	OffsetY float64
	Blur    float64
	Color   Color
}

// GetTextShadow parses the text-shadow property and returns a slice of shadows.
// Syntax: [<color>? <offset-x> <offset-y> <blur>? <color>?], ...
func (s *Style) GetTextShadow() []TextShadow {
	val, ok := s.Get("text-shadow")
	if !ok || val == "none" || val == "" {
		return nil
	}
	var shadows []TextShadow
	for _, layer := range strings.Split(val, ",") {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			continue
		}
		shadow := parseTextShadowLayer(layer, s.GetFontSize())
		if shadow != nil {
			shadows = append(shadows, *shadow)
		}
	}
	return shadows
}

// parseTextShadowLayer parses one text-shadow layer: [color?] <x> <y> [blur?] [color?]
func parseTextShadowLayer(s string, fontSize float64) *TextShadow {
	fields := strings.Fields(s)
	var lengths []float64
	var shadowColor *Color
	for i := 0; i < len(fields); {
		// Try to parse as a length
		if l, ok := ParseLengthWithFontSize(fields[i], fontSize); ok {
			lengths = append(lengths, l)
			i++
			continue
		}
		// Try to parse as a color (may span multiple tokens for rgba/hsl)
		// Try combining increasing numbers of tokens
		parsed := false
		for n := 4; n >= 1; n-- {
			if i+n > len(fields) {
				continue
			}
			candidate := strings.Join(fields[i:i+n], " ")
			if c, ok := ParseColor(candidate); ok {
				shadowColor = &c
				i += n
				parsed = true
				break
			}
		}
		if !parsed {
			i++
		}
	}
	if len(lengths) < 2 {
		return nil
	}
	sh := &TextShadow{
		OffsetX: lengths[0],
		OffsetY: lengths[1],
	}
	if len(lengths) >= 3 {
		sh.Blur = lengths[2]
	}
	if shadowColor != nil {
		sh.Color = *shadowColor
	} else {
		sh.Color = Color{R: 0, G: 0, B: 0, A: 1.0}
	}
	return sh
}

// GetTextUnderlineOffset returns the text-underline-offset value in pixels.
// Percentages and calc() % terms resolve against the element's font-size
// (1em) per CSS Text Decor L4 §"text-underline-offset". Returns 0 for `auto`,
// the initial value, or any unparseable input.
func (s *Style) GetTextUnderlineOffset() float64 {
	val, ok := s.Get("text-underline-offset")
	if !ok {
		return 0
	}
	v := strings.TrimSpace(val)
	if v == "" || strings.EqualFold(v, "auto") {
		return 0
	}
	if px, ok := ParseLengthOrPercentageFontRelative(val, s.GetFontSize()); ok {
		return px
	}
	return 0
}

// Phase 20: Additional text properties

// GetLetterSpacing returns the letter-spacing value in pixels (default: 0)
func (s *Style) GetLetterSpacing() float64 {
	if spacing, ok := s.GetLength("letter-spacing"); ok {
		return spacing
	}
	return 0.0
}

// GetWordSpacing returns the word-spacing value in pixels (default: 0)
func (s *Style) GetWordSpacing() float64 {
	if spacing, ok := s.GetLength("word-spacing"); ok {
		return spacing
	}
	return 0.0
}

// TextTransform represents the text-transform property value
type TextTransform string

const (
	TextTransformNone       TextTransform = "none"
	TextTransformUppercase  TextTransform = "uppercase"
	TextTransformLowercase  TextTransform = "lowercase"
	TextTransformCapitalize TextTransform = "capitalize"
)

// GetTextTransform returns the text-transform value (default: none)
func (s *Style) GetTextTransform() TextTransform {
	if transform, ok := s.Get("text-transform"); ok {
		switch transform {
		case "uppercase":
			return TextTransformUppercase
		case "lowercase":
			return TextTransformLowercase
		case "capitalize":
			return TextTransformCapitalize
		case "none":
			return TextTransformNone
		}
	}
	return TextTransformNone
}

// WhiteSpace represents the white-space property value
type WhiteSpace string

const (
	WhiteSpaceNormal  WhiteSpace = "normal"
	WhiteSpaceNowrap  WhiteSpace = "nowrap"
	WhiteSpacePre     WhiteSpace = "pre"
	WhiteSpacePreWrap WhiteSpace = "pre-wrap"
	WhiteSpacePreLine WhiteSpace = "pre-line"
)

// GetWhiteSpace returns the white-space value (default: normal)
func (s *Style) GetWhiteSpace() WhiteSpace {
	if ws, ok := s.Get("white-space"); ok {
		switch ws {
		case "nowrap":
			return WhiteSpaceNowrap
		case "pre":
			return WhiteSpacePre
		case "pre-wrap":
			return WhiteSpacePreWrap
		case "pre-line":
			return WhiteSpacePreLine
		case "normal":
			return WhiteSpaceNormal
		}
	}
	return WhiteSpaceNormal
}

// Phase 21: Overflow properties

// OverflowType represents the overflow property value
type OverflowType string

const (
	OverflowVisible OverflowType = "visible"
	OverflowHidden  OverflowType = "hidden"
	OverflowScroll  OverflowType = "scroll"
	OverflowAuto    OverflowType = "auto"
	OverflowClip    OverflowType = "clip"
)

// GetOverflow returns the overflow value (default: visible)
func (s *Style) GetOverflow() OverflowType {
	if overflow, ok := s.Get("overflow"); ok {
		switch overflow {
		case "hidden":
			return OverflowHidden
		case "scroll":
			return OverflowScroll
		case "auto", "overlay":
			return OverflowAuto
		case "visible":
			return OverflowVisible
		case "clip":
			return OverflowClip
		}
	}
	return OverflowVisible
}

// GetOverflowX returns the overflow-x value (default: overflow value)
// CSS Overflow Level 3: if overflow-y is non-visible and overflow-x is visible,
// overflow-x computes to auto.
func (s *Style) GetOverflowX() OverflowType {
	result := s.GetOverflow()
	if overflowX, ok := s.Get("overflow-x"); ok {
		switch overflowX {
		case "hidden":
			result = OverflowHidden
		case "scroll":
			result = OverflowScroll
		case "auto", "overlay":
			result = OverflowAuto
		case "visible":
			result = OverflowVisible
		case "clip":
			result = OverflowClip
		}
	}
	if result == OverflowVisible {
		otherAxis := s.getRawOverflowY()
		if otherAxis != OverflowVisible {
			return OverflowAuto
		}
	}
	return result
}

// GetOverflowY returns the overflow-y value (default: overflow value)
// CSS Overflow Level 3: if overflow-x is non-visible and overflow-y is visible,
// overflow-y computes to auto.
func (s *Style) GetOverflowY() OverflowType {
	result := s.GetOverflow()
	if overflowY, ok := s.Get("overflow-y"); ok {
		switch overflowY {
		case "hidden":
			result = OverflowHidden
		case "scroll":
			result = OverflowScroll
		case "auto", "overlay":
			result = OverflowAuto
		case "visible":
			result = OverflowVisible
		case "clip":
			result = OverflowClip
		}
	}
	if result == OverflowVisible {
		otherAxis := s.getRawOverflowX()
		if otherAxis != OverflowVisible {
			return OverflowAuto
		}
	}
	return result
}

// getRawOverflowX returns the raw overflow-x value without interdependency logic.
func (s *Style) getRawOverflowX() OverflowType {
	if overflowX, ok := s.Get("overflow-x"); ok {
		switch overflowX {
		case "hidden":
			return OverflowHidden
		case "scroll":
			return OverflowScroll
		case "auto", "overlay":
			return OverflowAuto
		case "visible":
			return OverflowVisible
		case "clip":
			return OverflowClip
		}
	}
	return s.GetOverflow()
}

// getRawOverflowY returns the raw overflow-y value without interdependency logic.
func (s *Style) getRawOverflowY() OverflowType {
	if overflowY, ok := s.Get("overflow-y"); ok {
		switch overflowY {
		case "hidden":
			return OverflowHidden
		case "scroll":
			return OverflowScroll
		case "auto", "overlay":
			return OverflowAuto
		case "visible":
			return OverflowVisible
		case "clip":
			return OverflowClip
		}
	}
	return s.GetOverflow()
}

// OverflowClipMarginBox identifies the visual reference box used by the
// overflow-clip-margin property (CSS Overflow 3 §3.2). The clip rectangle
// is the named box edge, expanded outward by Length.
type OverflowClipMarginBox string

const (
	// OverflowClipMarginContentBox: clip edge starts at the content box.
	OverflowClipMarginContentBox OverflowClipMarginBox = "content-box"
	// OverflowClipMarginPaddingBox: clip edge starts at the padding box (default).
	OverflowClipMarginPaddingBox OverflowClipMarginBox = "padding-box"
	// OverflowClipMarginBorderBox: clip edge starts at the border box.
	OverflowClipMarginBorderBox OverflowClipMarginBox = "border-box"
)

// OverflowClipMargin holds the parsed value of `overflow-clip-margin`.
// Per CSS Overflow 3, the grammar is:
//
//	<visual-box> || <length [0,∞]>
//
// Default visual box is `padding-box`; default length is 0.
type OverflowClipMargin struct {
	Box    OverflowClipMarginBox
	Length float64
}

// GetOverflowClipMargin parses the `overflow-clip-margin` property.
// Returns the (visual-box, length) pair plus a `ok` flag indicating whether
// the property was set (and parsed successfully). When ok is false the
// caller should treat the value as the spec default (padding-box, 0).
//
// Grammar: <visual-box> || <length [0,∞]>; visual-box ∈ {content-box,
// padding-box, border-box}. Per the spec, negative lengths are invalid.
func (s *Style) GetOverflowClipMargin() (OverflowClipMargin, bool) {
	v, ok := s.Get("overflow-clip-margin")
	if !ok {
		return OverflowClipMargin{Box: OverflowClipMarginPaddingBox}, false
	}
	result := OverflowClipMargin{Box: OverflowClipMarginPaddingBox}
	sawLength := false
	sawBox := false
	for _, tok := range strings.Fields(v) {
		switch tok {
		case "content-box":
			if sawBox {
				return OverflowClipMargin{Box: OverflowClipMarginPaddingBox}, false
			}
			result.Box = OverflowClipMarginContentBox
			sawBox = true
		case "padding-box":
			if sawBox {
				return OverflowClipMargin{Box: OverflowClipMarginPaddingBox}, false
			}
			result.Box = OverflowClipMarginPaddingBox
			sawBox = true
		case "border-box":
			if sawBox {
				return OverflowClipMargin{Box: OverflowClipMarginPaddingBox}, false
			}
			result.Box = OverflowClipMarginBorderBox
			sawBox = true
		default:
			if sawLength {
				return OverflowClipMargin{Box: OverflowClipMarginPaddingBox}, false
			}
			length, parsed := parseLengthFullWithCh(tok, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight)
			if !parsed || length < 0 {
				return OverflowClipMargin{Box: OverflowClipMarginPaddingBox}, false
			}
			result.Length = length
			sawLength = true
		}
	}
	if !sawBox && !sawLength {
		return OverflowClipMargin{Box: OverflowClipMarginPaddingBox}, false
	}
	return result, true
}

// GetVisibility returns the visibility value (default: "visible")
func (s *Style) GetVisibility() string {
	if v, ok := s.Get("visibility"); ok {
		return v
	}
	return "visible"
}

// Phase 19: Visual effects

// GetOpacity returns the opacity value (0.0 to 1.0, default: 1.0)
func (s *Style) GetOpacity() float64 {
	if opacityStr, ok := s.Get("opacity"); ok {
		var opacity float64
		if _, err := fmt.Sscanf(opacityStr, "%f", &opacity); err == nil {
			// Clamp to 0.0 - 1.0
			if opacity < 0.0 {
				opacity = 0.0
			} else if opacity > 1.0 {
				opacity = 1.0
			}
			return opacity
		}
	}
	return 1.0 // Fully opaque by default
}

// BoxShadow represents a box-shadow effect
type BoxShadow struct {
	OffsetX         float64
	OffsetY         float64
	Blur            float64
	Spread          float64
	Color           Color
	Inset           bool
	UseCurrentColor bool // true when color is currentcolor or omitted
}

// GetBoxShadow parses and returns box-shadow values
func (s *Style) GetBoxShadow() []BoxShadow {
	shadowStr, ok := s.Get("box-shadow")
	if !ok || shadowStr == "none" {
		return nil
	}

	// Split by comma for multiple shadows, respecting parentheses in rgba() etc.
	parts := splitCommaSeparated(shadowStr)

	// CSS spec: box-shadow accepts either "none" OR a comma-separated list of shadows.
	// Mixing "none" with other values is invalid — reject the entire declaration.
	for _, part := range parts {
		if strings.TrimSpace(part) == "none" {
			return nil
		}
	}

	shadows := make([]BoxShadow, 0)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		shadow := parseBoxShadowValue(part)
		if shadow != nil {
			shadows = append(shadows, *shadow)
		}
	}

	return shadows
}

// parseBoxShadowValue parses a single box-shadow value.
// CSS syntax: [inset?] [<color>]? <offset-x> <offset-y> [<blur>]? [<spread>]? [<color>]? [inset?]
// The color and inset keyword can appear at either the start or end.
func parseBoxShadowValue(s string) *BoxShadow {
	s = strings.TrimSpace(s)
	// Tokenize respecting parenthesized groups like calc(), rgba(), etc.
	tokens := tokenizeRespectingParens(s)

	if len(tokens) < 2 {
		return nil
	}

	shadow := &BoxShadow{
		Color:           Color{0, 0, 0, 1.0}, // Placeholder; resolved to currentcolor later
		UseCurrentColor: true,                // Default per CSS spec
	}

	// Pre-scan: extract 'inset' and color from either end, leaving only
	// length tokens for the positional parse.
	var lengths []string
	var colorTokens []string
	for _, tok := range tokens {
		if tok == "inset" {
			shadow.Inset = true
			continue
		}
		if _, ok := ParseLength(tok); ok {
			lengths = append(lengths, tok)
		} else {
			colorTokens = append(colorTokens, tok)
		}
	}

	// Parse lengths: offset-x, offset-y, [blur], [spread]
	if len(lengths) >= 2 {
		shadow.OffsetX, _ = ParseLength(lengths[0])
		shadow.OffsetY, _ = ParseLength(lengths[1])
	}
	if len(lengths) >= 3 {
		shadow.Blur, _ = ParseLength(lengths[2])
		if shadow.Blur < 0 {
			return nil // Negative blur radius is invalid per spec
		}
	}
	if len(lengths) >= 4 {
		shadow.Spread, _ = ParseLength(lengths[3])
	}

	// Parse color from non-length, non-inset tokens.
	if len(colorTokens) > 0 {
		colorStr := strings.Join(colorTokens, " ")
		if strings.EqualFold(colorStr, "currentcolor") {
			shadow.UseCurrentColor = true
		} else if c, ok := ParseColor(colorStr); ok {
			shadow.Color = c
			shadow.UseCurrentColor = false
		}
	}

	return shadow
}

// tokenizeRespectingParens splits a string by whitespace but keeps
// parenthesized groups (like calc(), rgba()) together as single tokens.
func tokenizeRespectingParens(s string) []string {
	var tokens []string
	depth := 0
	start := -1
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '(':
			depth++
			if start == -1 {
				// Find the start of this token (function name before the paren)
				start = i
				for start > 0 && s[start-1] != ' ' {
					start--
				}
			}
		case ch == ')':
			depth--
			if depth == 0 && start >= 0 {
				tokens = append(tokens, s[start:i+1])
				start = -1
			}
		case ch == ' ' || ch == '\t':
			if depth > 0 {
				continue // Inside parens, skip whitespace
			}
			if start >= 0 {
				tokens = append(tokens, s[start:i])
				start = -1
			}
		default:
			if start == -1 {
				start = i
			}
		}
	}
	if start >= 0 && start < len(s) {
		tokens = append(tokens, s[start:])
	}
	return tokens
}

// isColor checks if a token might be a color value
func isColor(s string) bool {
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "rgb") || strings.HasPrefix(s, "hsl") ||
		strings.HasPrefix(s, "oklch(") || strings.HasPrefix(s, "lch(") ||
		strings.HasPrefix(s, "lab(") || strings.HasPrefix(s, "hwb(") || strings.HasPrefix(s, "color-mix(") ||
		strings.HasPrefix(s, "color(") {
		return true
	}
	// Bare numbers and lengths are not colors
	if _, ok := ParseLength(s); ok {
		return false
	}
	// Named colors and other non-length, non-keyword tokens
	if s == "inset" {
		return false
	}
	_, ok := ParseColor(s)
	return ok
}

// Phase 7: Display modes

// DisplayType represents the display property value
type DisplayType string

const (
	DisplayBlock            DisplayType = "block"
	DisplayInline           DisplayType = "inline"
	DisplayInlineBlock      DisplayType = "inline-block"
	DisplayNone             DisplayType = "none"
	DisplayTable            DisplayType = "table"
	DisplayTableRow         DisplayType = "table-row"
	DisplayTableCell        DisplayType = "table-cell"
	DisplayTableHeaderGroup DisplayType = "table-header-group"
	DisplayTableRowGroup    DisplayType = "table-row-group"
	DisplayTableFooterGroup DisplayType = "table-footer-group"
	DisplayListItem         DisplayType = "list-item" // Phase 23
	// DisplayInlineListItem is an inline-level box with the list-item inner
	// display type — CSS Display L3 `display: inline list-item` (and the
	// `inline flow-root list-item` variant). Mirrors Blink EDisplay::
	// kInlineListItem. It is inline-level (flows in an IFC, shrink-to-fits)
	// and IsListItemDisplay() is true so it gets its own ::marker box.
	DisplayInlineListItem DisplayType = "inline-list-item"
	DisplayFlex           DisplayType = "flex"
	DisplayInlineFlex     DisplayType = "inline-flex"
	DisplayGrid           DisplayType = "grid"
	DisplayInlineGrid     DisplayType = "inline-grid"
	DisplayContents       DisplayType = "contents"
	DisplayTableCaption   DisplayType = "table-caption"
	DisplayFlowRoot       DisplayType = "flow-root"
	DisplayRuby           DisplayType = "ruby"
	DisplayRubyText       DisplayType = "ruby-text"
	// DisplayBlockRuby is the block-level form of ruby (CSS Display L3
	// two-keyword `display: block ruby`). Mirrors Blink's
	// EDisplay::kBlockRuby. Blink generates a LayoutRubyAsBlock principal
	// LayoutBlockFlow wrapping a single anonymous inline `display:ruby`
	// box that holds all children (`layout_ruby_as_block.{h,cc}`).
	DisplayBlockRuby   DisplayType = "block ruby"
	DisplayInlineTable DisplayType = "inline-table"
)

// GetTextIndent returns the text-indent value in pixels (default: 0).
// For percentage values (e.g. "50%"), returns (fraction, true) where fraction=0.5;
// caller must multiply by container width. For length values, returns (pixels, false).
func (s *Style) GetTextIndent() (float64, bool) {
	if val, ok := s.Get("text-indent"); ok {
		trimmed := strings.TrimSpace(val)
		if strings.HasSuffix(trimmed, "%") {
			if pct, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, "%"), 64); err == nil {
				return pct / 100.0, true
			}
		}
		if length, ok := ParseLength(val); ok {
			return length, false
		}
	}
	return 0, false
}

// ResolveTextIndent resolves text-indent against the containing block's
// inline-size. Handles length (`5em`), percentage (`50%`), and calc() with
// percent (`calc(50% - 3px)`). Returns 0 for unset/auto/invalid values.
// Mirrors Blink's resolution at use time
// (third_party/blink/renderer/core/css/properties/longhands/text_indent.cc).
func (s *Style) ResolveTextIndent(containingWidth float64) float64 {
	val, ok := s.Get("text-indent")
	if !ok {
		return 0
	}
	trimmed := strings.TrimSpace(val)
	if strings.HasSuffix(trimmed, "%") {
		if pct, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, "%"), 64); err == nil {
			return pct / 100.0 * containingWidth
		}
	}
	// CSS Values §10.2: calc() with percent. Resolve against containingWidth.
	if IsCalcWithPercent(trimmed) {
		if v, ok := EvalCalcWithPercent(
			trimmed[5:len(trimmed)-1], s.GetFontSize(), containingWidth,
		); ok {
			return v
		}
	}
	if length, ok := s.GetLength("text-indent"); ok {
		return length
	}
	return 0
}

// ResolveTextIndentIntrinsic resolves text-indent for intrinsic-sizing
// (min/max-content) contexts. Percent terms (bare or inside calc()) resolve
// to 0 since the containing block's inline-size is unknown. Mirrors Blink's
// InlineNode::ComputeMinMaxSizes which treats HasPercent text-indent as
// LayoutUnit() (= 0) and adds the non-percent calc() contribution.
func (s *Style) ResolveTextIndentIntrinsic() float64 {
	val, ok := s.Get("text-indent")
	if !ok {
		return 0
	}
	trimmed := strings.TrimSpace(val)
	if strings.HasSuffix(trimmed, "%") {
		return 0 // bare percent → 0 for intrinsic
	}
	if IsCalcWithPercent(trimmed) {
		if v, ok := EvalCalcIntrinsic(trimmed[5:len(trimmed)-1], s.GetFontSize()); ok {
			return v
		}
		return 0
	}
	if length, ok := s.GetLength("text-indent"); ok {
		return length
	}
	return 0
}

// GetBoxSizing returns the box-sizing value (default: content-box)
func (s *Style) GetBoxSizing() string {
	if val, ok := s.Get("box-sizing"); ok {
		return val
	}
	return "content-box"
}

// GetDisplay returns the display value (default: block).
//
// Supports CSS Display L3 two-keyword forms for ruby:
//
//	`display: ruby`           → DisplayRuby (inline-level)
//	`display: block ruby`     → DisplayBlockRuby (block-level)
//	`display: inline ruby`    → DisplayRuby (explicit inline outer)
//
// Mirrors Blink's CSSDisplayValue parsing
// (third_party/blink/renderer/core/css/css_properties.json5:3416 — the
// `display` keyword list contains only `ruby` and `ruby-text` among
// ruby values; `block ruby` is the two-keyword L3 form).
//
// `ruby-base`, `ruby-base-container`, and `ruby-text-container` are not
// real display values in modern Blink (no kRubyBase / kRubyBaseContainer
// / kRubyTextContainer in EDisplay); they fall through to the default.
func (s *Style) GetDisplay() DisplayType {
	if display, ok := s.Get("display"); ok {
		// CSS Display L3 §2.1: the multi-value `display` syntax. Split on ANY
		// whitespace (space/tab/newline — strings.Fields handles all) and map
		// the supported two-keyword forms to a single DisplayType before the
		// single-keyword switch. Mirrors Blink's EDisplay parsing
		// (css_parsing_utils ConsumeDisplay → EDisplay::kInlineListItem etc.).
		if fields := strings.Fields(display); len(fields) > 1 {
			hasListItem := false
			outer := "block" // default outer display
			inner := ""
			for _, f := range fields {
				switch f {
				case "list-item":
					hasListItem = true
				case "inline", "block":
					outer = f
				case "flow", "flow-root", "flex", "grid", "table", "ruby":
					inner = f
				}
			}
			if hasListItem {
				if outer == "inline" {
					return DisplayInlineListItem
				}
				return DisplayListItem
			}
			switch outer + " " + inner {
			case "inline flex":
				return DisplayInlineFlex
			case "inline grid":
				return DisplayInlineGrid
			case "inline table":
				return DisplayInlineTable
			case "inline flow-root":
				return DisplayInlineBlock
			case "block flow-root":
				return DisplayFlowRoot
			case "inline ruby":
				return DisplayRuby
			case "block ruby":
				return DisplayBlockRuby
			}
			// Remaining forms collapse to the outer type: `inline flow` →
			// inline; `block flow` and unrecognized inners → block (via the
			// single-keyword switch / default below).
			if outer == "inline" {
				return DisplayInline
			}
		}
		switch display {
		case "inline":
			return DisplayInline
		case "inline-block":
			return DisplayInlineBlock
		case "none":
			return DisplayNone
		case "table":
			return DisplayTable
		case "table-row":
			return DisplayTableRow
		case "table-cell":
			return DisplayTableCell
		case "table-header-group":
			return DisplayTableHeaderGroup
		case "table-row-group":
			return DisplayTableRowGroup
		case "table-footer-group":
			return DisplayTableFooterGroup
		case "list-item":
			return DisplayListItem
		case "inline-list-item":
			return DisplayInlineListItem
		case "flex":
			return DisplayFlex
		case "inline-flex":
			return DisplayInlineFlex
		case "grid":
			return DisplayGrid
		case "inline-grid":
			return DisplayInlineGrid
		case "contents":
			return DisplayContents
		case "table-caption":
			return DisplayTableCaption
		case "flow-root":
			return DisplayFlowRoot
		case "-webkit-box", "-webkit-flex":
			return DisplayFlex
		case "-webkit-inline-box", "-webkit-inline-flex":
			return DisplayInlineFlex
		case "ruby", "inline ruby":
			return DisplayRuby
		case "block ruby":
			return DisplayBlockRuby
		case "ruby-text":
			return DisplayRubyText
		case "inline-table":
			return DisplayInlineTable
		}
	}
	return DisplayBlock
}

// EquivalentInlineDisplay returns the inline-level display value that a
// block-level display maps to when its formatting context demands an
// inline child. Mirrors Blink's
// `core/css/resolver/style_adjuster.cc:303-356` `EquivalentInlineDisplay`.
//
// Used during the CSS Ruby §2.2 `#anon-gen-inlinize` step: when the
// layout parent is `display: ruby` or `display: ruby-text`, every
// in-flow child has its used display set to this equivalent so the
// child participates in the inline ruby line. Also used to convert
// `block ruby` back to `ruby` when inlinified.
func EquivalentInlineDisplay(d DisplayType) DisplayType {
	switch d {
	case DisplayBlock, DisplayFlowRoot, DisplayListItem:
		return DisplayInlineBlock
	case DisplayFlex:
		return DisplayInlineFlex
	case DisplayGrid:
		return DisplayInlineGrid
	case DisplayTable:
		return DisplayInlineTable
	case DisplayBlockRuby:
		return DisplayRuby
	}
	return d
}

// EquivalentBlockDisplay returns the block-level display value that an
// inline-level display maps to when its formatting context demands a
// block child. Mirrors Blink's
// `core/css/resolver/style_adjuster.cc:247-301` `EquivalentBlockDisplay`.
//
// Currently only the ruby cases are used by louis14 Phase 1: `kRuby`
// becomes `kBlockRuby`, `kRubyText` becomes `kBlock`.
func EquivalentBlockDisplay(d DisplayType) DisplayType {
	switch d {
	case DisplayInlineBlock, DisplayInline, DisplayFlowRoot:
		return DisplayBlock
	case DisplayInlineFlex:
		return DisplayFlex
	case DisplayInlineGrid:
		return DisplayGrid
	case DisplayInlineTable:
		return DisplayTable
	case DisplayRuby:
		return DisplayBlockRuby
	case DisplayRubyText:
		return DisplayBlock
	}
	return d
}

// IsListItemDisplay reports whether the computed display establishes a
// list-item — `display: list-item` (block-level) or `display: inline
// list-item` (inline-level). Mirrors Blink's ComputedStyle::IsDisplayListItem
// (EDisplay::kListItem || kInlineListItem). The ::marker box is generated
// whenever this is true, regardless of the box's inline/block level.
func (s *Style) IsListItemDisplay() bool {
	d := s.GetDisplay()
	return d == DisplayListItem || d == DisplayInlineListItem
}

// IsAtomicInlineDisplay reports whether the computed display generates
// an atomic inline-level box (CSS Display 3 §2.2): a single,
// indivisible inline-level box that establishes its own formatting
// context and is opaque to outer inline-axis effects. Used by the
// text-decoration propagation gate (CSS Text Decor 4 §1.3) — an atomic
// inline does NOT inherit decorations propagated by an outer inline
// or block decorator.
//
// Mirrors Blink's `EDisplay::IsAtomicInlineLevel` family — currently the
// inline equivalents of block/flex/grid/table plus the inline list-item
// variant (`inline list-item`). Replaced elements (img, video, svg,
// canvas, iframe, embed, object, button, input, textarea, select) ALSO
// generate atomic inlines per CSS Display 3, regardless of their
// computed `display`; that branch is the caller's responsibility (see
// IsReplacedElement in pkg/layout/inline_item.go — both checks must be
// combined where the propagation gate runs).
func (s *Style) IsAtomicInlineDisplay() bool {
	switch s.GetDisplay() {
	case DisplayInlineBlock, DisplayInlineFlex, DisplayInlineGrid,
		DisplayInlineTable, DisplayInlineListItem:
		return true
	}
	return false
}

// EstablishesNewFormattingContext reports whether the computed display
// makes this element establish a new (block or inline) formatting context
// that contains its descendants. Used by callers that need to know
// whether they should descend across the box boundary — for instance,
// the ::first-letter walk treats a flow-root child as opaque because its
// content belongs to a separate first formatted line.
//
// Detects flow-root via the multi-keyword `display` syntax — `inline
// flow-root list-item` collapses to DisplayInlineListItem in
// GetDisplay() (the list-item check wins), losing the flow-root bit, so
// we read the raw property here. Mirrors Blink's
// LayoutObject::CreatesNewFormattingContext for the relevant display
// keywords.
func (s *Style) EstablishesNewFormattingContext() bool {
	switch s.GetDisplay() {
	case DisplayFlowRoot, DisplayInlineBlock, DisplayTableCell,
		DisplayTableCaption, DisplayFlex, DisplayInlineFlex,
		DisplayGrid, DisplayInlineGrid:
		return true
	}
	// Inspect the raw display string for the `flow-root` inner keyword
	// combined with other outers (notably `inline flow-root list-item`,
	// which GetDisplay() folds into DisplayInlineListItem).
	if raw, ok := s.Get("display"); ok {
		for _, f := range strings.Fields(raw) {
			if f == "flow-root" {
				return true
			}
		}
	}
	return false
}

// IsInlineRuby reports whether the element should be treated as an
// inline-level ruby box (i.e. `display: ruby`, equivalently `display:
// inline ruby`). Mirrors Blink's LayoutObject::IsInlineRuby at
// `core/layout/layout_object.cc:522-525` (`IsLayoutInline() &&
// Display()==kRuby`), which `core/layout/inline/inline_items_builder.cc`
// (`:1550-1595`) uses to gate ruby column item emission.
// Vetted against Chromium main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func (s *Style) IsInlineRuby() bool {
	return s.GetDisplay() == DisplayRuby
}

// IsInlineRubyText reports whether the element should be treated as an
// inline-level ruby annotation box (`display: ruby-text`). Mirrors
// Blink's LayoutObject::IsInlineRubyText at
// `core/layout/layout_object.cc:527-529` (`IsLayoutInline() &&
// Display()==kRubyText`); used alongside IsInlineRuby in the
// inline-item-collection ruby box-fixup. Vetted against Chromium main
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func (s *Style) IsInlineRubyText() bool {
	return s.GetDisplay() == DisplayRubyText
}

// GetLineClamp returns the -webkit-line-clamp value (0 = no clamping)
func (s *Style) GetLineClamp() int {
	if val, ok := s.Get("-webkit-line-clamp"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// GetBoxOrient returns the -webkit-box-orient value (empty string if not set)
func (s *Style) GetBoxOrient() string {
	if val, ok := s.Get("-webkit-box-orient"); ok {
		return strings.TrimSpace(val)
	}
	return ""
}

// GetContain returns the contain property value (default: "none").
// Values include: none, layout, paint, size, style, content, strict.
func (s *Style) GetContain() string {
	if v, ok := s.Get("contain"); ok {
		return strings.TrimSpace(v)
	}
	return "none"
}

// HasLayoutContainment returns true if the element has layout containment.
// This is set by contain: layout, contain: content, or contain: strict,
// as well as space-separated combinations like "layout paint".
func (s *Style) HasLayoutContainment() bool {
	v := s.GetContain()
	if v == "none" {
		return false
	}
	if v == "strict" || v == "content" {
		return true
	}
	return strings.Contains(v, "layout")
}

// HasPaintContainment returns true if the element has paint containment.
// This is set by contain: paint, contain: content, or contain: strict,
// as well as space-separated combinations like "layout paint".
func (s *Style) HasPaintContainment() bool {
	v := s.GetContain()
	if v == "none" {
		return false
	}
	if v == "strict" || v == "content" {
		return true
	}
	return strings.Contains(v, "paint")
}

// HasSizeContainment returns true if the element has size containment.
// This is set by contain: size or contain: strict,
// as well as space-separated combinations like "layout paint size".
func (s *Style) HasSizeContainment() bool {
	v := s.GetContain()
	if v == "none" || v == "content" {
		return false
	}
	if v == "strict" {
		return true
	}
	return strings.Contains(v, "size")
}

// HasInlineSizeContainment returns true if the element has inline-size containment.
// This is set by contain: inline-size, or space-separated combinations.
func (s *Style) HasInlineSizeContainment() bool {
	v := s.GetContain()
	if v == "none" {
		return false
	}
	return strings.Contains(v, "inline-size")
}

// HasStyleContainment returns true if the element has style containment.
// This is set by contain: style, contain: content, or contain: strict,
// as well as space-separated combinations like "layout style". Mirrors
// Blink ComputedStyle::ContainsStyle (computed_style.h at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f) and CSS Contain 1 §3.3 —
// style containment establishes a fresh counter scope and quote depth
// for the contained element's subtree.
func (s *Style) HasStyleContainment() bool {
	v := s.GetContain()
	if v == "none" {
		return false
	}
	if v == "strict" || v == "content" {
		return true
	}
	return strings.Contains(v, "style")
}

// HasAnyContainment returns true if the element has any form of containment.
func (s *Style) HasAnyContainment() bool {
	v := s.GetContain()
	return v != "none" && v != ""
}

// VerticalAlign represents the vertical-align property value
type VerticalAlign string

const (
	VerticalAlignBaseline VerticalAlign = "baseline"
	VerticalAlignTop      VerticalAlign = "top"
	VerticalAlignMiddle   VerticalAlign = "middle"
	VerticalAlignBottom   VerticalAlign = "bottom"
)

// GetVerticalAlign returns the vertical-align keyword value (default: baseline).
// For length values, returns VerticalAlignBaseline; use GetVerticalAlignOffset for the offset.
func (s *Style) GetVerticalAlign() VerticalAlign {
	if align, ok := s.Get("vertical-align"); ok {
		switch align {
		case "top":
			return VerticalAlignTop
		case "middle":
			return VerticalAlignMiddle
		case "bottom":
			return VerticalAlignBottom
		default:
			// Length value — keyword is baseline, offset comes from GetVerticalAlignOffset
			return VerticalAlignBaseline
		}
	}
	return VerticalAlignBaseline
}

// GetVerticalAlignOffset returns the pixel offset for length-based vertical-align values
// (e.g., "10px" → 10.0). Positive means raise up (toward smaller Y). Returns 0 if not a length.
func (s *Style) GetVerticalAlignOffset() float64 {
	if align, ok := s.Get("vertical-align"); ok {
		switch align {
		case "top", "middle", "bottom", "baseline", "sub", "super", "text-top", "text-bottom":
			return 0
		default:
			if px, ok := ParseLength(align); ok {
				return px
			}
		}
	}
	return 0
}

// GetLineHeight returns the line-height in pixels (default: 1.2 * font-size).
// CSS line-height accepts unitless numbers (e.g., "1.5") meaning a multiplier
// of the current font-size, unlike other CSS length properties where bare
// numbers are invalid.
// IsLineHeightNormal returns true when line-height is 'normal' (the initial value).
// When true, the used line-height should come from the font's own metrics
// (ascent + descent) rather than a fixed multiplier.
func (s *Style) IsLineHeightNormal() bool {
	val, ok := s.Get("line-height")
	if !ok {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(val), "normal")
}

func (s *Style) GetLineHeight() float64 {
	val, ok := s.Get("line-height")
	if !ok {
		return s.GetFontSize() * 1.2
	}
	// Try as a standard CSS length first (px, em, etc.)
	// Use writing-mode-aware ch scale for vertical writing modes.
	if lh, ok := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok {
		return lh
	}
	// Try as a unitless multiplier (e.g., "1.5" means 1.5 × font-size)
	val = strings.TrimSpace(val)
	if num, err := strconv.ParseFloat(val, 64); err == nil && num > 0 {
		return num * s.GetFontSize()
	}
	// Try as a percentage (e.g., "150%" means 1.5 × font-size)
	if pct, ok := ParsePercentage(val); ok {
		return pct / 100.0 * s.GetFontSize()
	}
	return s.GetFontSize() * 1.2
}

// Phase 9: Table layout

// BorderCollapse represents the border-collapse property value
type BorderCollapse string

const (
	BorderCollapseSeparate BorderCollapse = "separate"
	BorderCollapseCollapse BorderCollapse = "collapse"
)

// GetBorderCollapse returns the border-collapse value (default: separate)
func (s *Style) GetBorderCollapse() BorderCollapse {
	if bc, ok := s.Get("border-collapse"); ok {
		switch bc {
		case "collapse":
			return BorderCollapseCollapse
		}
	}
	return BorderCollapseSeparate
}

// GetBorderSpacing returns the horizontal border-spacing value (default: 0 per CSS 2.1).
// If two values are given (horizontal vertical), returns the first (horizontal) value.
// CSS 2.1 §17.6.1: border-spacing takes one or two <length> values.
// One value → same for horizontal and vertical.
// Two values → first is horizontal, second is vertical.
func (s *Style) GetBorderSpacing() float64 {
	if val, ok := s.Get("border-spacing"); ok {
		// Handle two-value syntax: "96px 96px"
		parts := strings.Fields(val)
		if len(parts) >= 1 {
			if spacing, ok := ParseLength(parts[0]); ok {
				return spacing
			}
		}
	}
	return 0 // CSS 2.1 initial value
}

// GetBorderSpacingV returns the vertical border-spacing value (default: 0 per CSS 2.1).
// If only one value is given, it applies to both horizontal and vertical.
// If two values are given (horizontal vertical), returns the second (vertical) value.
func (s *Style) GetBorderSpacingV() float64 {
	if val, ok := s.Get("border-spacing"); ok {
		parts := strings.Fields(val)
		if len(parts) >= 2 {
			if spacing, ok := ParseLength(parts[1]); ok {
				return spacing
			}
		}
		// One value: same for both horizontal and vertical
		if len(parts) >= 1 {
			if spacing, ok := ParseLength(parts[0]); ok {
				return spacing
			}
		}
	}
	return 0 // CSS 2.1 initial value
}

// TableLayout represents the table-layout property value
type TableLayout string

const (
	TableLayoutAuto  TableLayout = "auto"
	TableLayoutFixed TableLayout = "fixed"
)

// GetCaptionSide returns the caption-side value (default: top)
func (s *Style) GetCaptionSide() string {
	if val, ok := s.Get("caption-side"); ok {
		return val
	}
	return "top"
}

// GetEmptyCells returns the empty-cells value (default: show)
func (s *Style) GetEmptyCells() string {
	if val, ok := s.Get("empty-cells"); ok {
		return val
	}
	return "show"
}

// GetTableLayout returns the table-layout value (default: auto)
func (s *Style) GetTableLayout() TableLayout {
	if val, ok := s.Get("table-layout"); ok {
		if val == "fixed" {
			return TableLayoutFixed
		}
	}
	return TableLayoutAuto
}

// GetColumnCount returns the column-count value (0 means "auto"/not set)
func (s *Style) GetColumnCount() int {
	if val, ok := s.Get("column-count"); ok {
		val = strings.TrimSpace(val)
		if val == "auto" {
			return 0
		}
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// GetColumnWidth returns the column-width value (0 means "auto"/not set)
func (s *Style) GetColumnWidth() float64 {
	if val, ok := s.Get("column-width"); ok {
		val = strings.TrimSpace(val)
		if val == "auto" {
			return 0
		}
		if w, ok := ParseLength(val); ok {
			return w
		}
	}
	return 0
}

// GetColumnSpan returns the column-span value ("none" or "all").
func (s *Style) GetColumnSpan() string {
	if v, ok := s.Get("column-span"); ok {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "all" {
			return "all"
		}
	}
	return "none"
}

// GetColumnFill returns the column-fill value ("balance", "auto", or "balance-all").
// Default is "balance" per CSS Multicol §3.5.
func (s *Style) GetColumnFill() string {
	if v, ok := s.Get("column-fill"); ok {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "auto" || v == "balance" || v == "balance-all" {
			return v
		}
	}
	return "balance"
}

// GetBreakBefore returns the break-before value ("auto", "column", "page", etc.).
// Default is "auto" (no forced break).
func (s *Style) GetBreakBefore() string {
	if v, ok := s.Get("break-before"); ok {
		return strings.TrimSpace(strings.ToLower(v))
	}
	return "auto"
}

// GetBreakAfter returns the break-after value ("auto", "column", "page", etc.).
// Default is "auto" (no forced break).
func (s *Style) GetBreakAfter() string {
	if v, ok := s.Get("break-after"); ok {
		return strings.TrimSpace(strings.ToLower(v))
	}
	return "auto"
}

// GetBreakInside returns the break-inside value ("auto", "avoid", "avoid-column",
// "avoid-page"). Default is "auto" (no break-avoidance).
func (s *Style) GetBreakInside() string {
	if v, ok := s.Get("break-inside"); ok {
		return strings.TrimSpace(strings.ToLower(v))
	}
	return "auto"
}

// GetColumnHeight returns the column-height value in CSS pixels.
// Returns -1 for "auto" (the default) or when unset or invalid.
// CSS Multi-column Level 2 §4.2: auto | <length [0,∞]>; no percentage.
func (s *Style) GetColumnHeight() float64 {
	if val, ok := s.Get("column-height"); ok {
		v := strings.TrimSpace(strings.ToLower(val))
		if v == "" || v == "auto" {
			return -1
		}
		if h, ok := ParseLengthWithFontSize(v, s.GetFontSize()); ok && h >= 0 {
			return h
		}
	}
	return -1
}

// GetColumnWrap returns the column-wrap value ("auto", "nowrap", or "wrap").
// Default is "auto" per CSS Multi-column Level 2 §4.2; "auto" resolves to
// "wrap" when column-height is non-auto, "nowrap" otherwise (Blink:
// ColumnLayoutAlgorithm::ShouldWrapColumns).
func (s *Style) GetColumnWrap() string {
	if v, ok := s.Get("column-wrap"); ok {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "wrap" || v == "nowrap" {
			return v
		}
	}
	return "auto"
}

// GetColumnGapMulticol returns the column-gap for multicol layout (default: 1em).
// Percentage values are NOT resolved here (no percent base) — callers that have
// a percent base should use GetColumnGapMulticolWithBase.
func (s *Style) GetColumnGapMulticol() float64 {
	return s.GetColumnGapMulticolWithBase(0)
}

// GetColumnGapMulticolWithBase returns the column-gap with percentage values
// resolved against `percentBase` (the multicol container's content-box
// inline-size, per CSS Multicol L1 §2.3 "Percentages [for column-gap] are
// relative to the dimension of the box."). When percentBase is 0, percentages
// fall through to the 1em default — preserves the no-base call sites'
// behavior.
func (s *Style) GetColumnGapMulticolWithBase(percentBase float64) float64 {
	if val, ok := s.Get("column-gap"); ok {
		val = strings.TrimSpace(val)
		if val == "normal" {
			return s.GetFontSize()
		}
		if strings.HasSuffix(val, "%") && percentBase > 0 {
			numStr := strings.TrimSuffix(val, "%")
			if num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
				return num * percentBase / 100.0
			}
		}
		if w, ok := ParseLengthWithFontSize(val, s.GetFontSize()); ok {
			return w
		}
	}
	return s.GetFontSize() // multicol default is "normal" = 1em
}

// GetRowGapMulticol returns the row-gap between column rows in a
// `column-wrap: wrap` multicol (CSS Multicol L2 §4.2.6). Default `normal`
// computes to 1em, matching `column-gap` and Blink's row_gap_ resolution
// in column_layout_algorithm.cc. Mirrors GetColumnGapMulticol so em
// resolution goes through the element's own font-size.
func (s *Style) GetRowGapMulticol() float64 {
	if val, ok := s.Get("row-gap"); ok {
		val = strings.TrimSpace(val)
		if val == "normal" {
			return s.GetFontSize()
		}
		if w, ok := ParseLengthWithFontSize(val, s.GetFontSize()); ok {
			return w
		}
	}
	return s.GetFontSize()
}

// Phase 10: Flexbox layout

// FlexDirection represents the flex-direction property value
type FlexDirection string

const (
	FlexDirectionRow           FlexDirection = "row"
	FlexDirectionRowReverse    FlexDirection = "row-reverse"
	FlexDirectionColumn        FlexDirection = "column"
	FlexDirectionColumnReverse FlexDirection = "column-reverse"
)

// GetFlexDirection returns the flex-direction value (default: row)
func (s *Style) GetFlexDirection() FlexDirection {
	if dir, ok := s.Get("flex-direction"); ok {
		switch dir {
		case "row-reverse":
			return FlexDirectionRowReverse
		case "column":
			return FlexDirectionColumn
		case "column-reverse":
			return FlexDirectionColumnReverse
		}
	}
	return FlexDirectionRow
}

// FlexWrap represents the flex-wrap property value
type FlexWrap string

const (
	FlexWrapNowrap      FlexWrap = "nowrap"
	FlexWrapWrap        FlexWrap = "wrap"
	FlexWrapWrapReverse FlexWrap = "wrap-reverse"
)

// GetFlexWrap returns the flex-wrap value (default: nowrap)
func (s *Style) GetFlexWrap() FlexWrap {
	if wrap, ok := s.Get("flex-wrap"); ok {
		switch wrap {
		case "wrap":
			return FlexWrapWrap
		case "wrap-reverse":
			return FlexWrapWrapReverse
		}
	}
	return FlexWrapNowrap
}

// JustifyContent represents the justify-content property value
type JustifyContent string

const (
	JustifyContentFlexStart    JustifyContent = "flex-start"
	JustifyContentFlexEnd      JustifyContent = "flex-end"
	JustifyContentCenter       JustifyContent = "center"
	JustifyContentSpaceBetween JustifyContent = "space-between"
	JustifyContentSpaceAround  JustifyContent = "space-around"
	JustifyContentSpaceEvenly  JustifyContent = "space-evenly"
	JustifyContentStretch      JustifyContent = "stretch"
	JustifyContentLeft         JustifyContent = "left"
	JustifyContentRight        JustifyContent = "right"
)

// stripSafeUnsafe removes an optional "safe " or "unsafe " prefix from an alignment value.
// Returns the stripped value and whether "safe" was specified.
func stripSafeUnsafe(val string) (string, bool) {
	if strings.HasPrefix(val, "safe ") {
		return val[5:], true
	}
	if strings.HasPrefix(val, "unsafe ") {
		return val[7:], false
	}
	return val, false
}

// GetJustifyContent returns the justify-content value (default: flex-start).
// Strips any "safe"/"unsafe" prefix; use IsSafeJustifyContent to check the safe flag.
// Note: "normal" resolves to "flex-start" in flex context per CSS Flexbox spec.
func (s *Style) GetJustifyContent() JustifyContent {
	if jc, ok := s.Get("justify-content"); ok {
		jc, _ = stripSafeUnsafe(jc)
		switch jc {
		case "flex-start", "start", "normal":
			return JustifyContentFlexStart
		case "flex-end", "end":
			return JustifyContentFlexEnd
		case "center":
			return JustifyContentCenter
		case "space-between":
			return JustifyContentSpaceBetween
		case "space-around":
			return JustifyContentSpaceAround
		case "space-evenly":
			return JustifyContentSpaceEvenly
		case "stretch":
			return JustifyContentStretch
		case "left":
			return JustifyContentLeft
		case "right":
			return JustifyContentRight
		}
	}
	return JustifyContentFlexStart
}

// IsSafeJustifyContent returns true if justify-content has the "safe" overflow keyword.
func (s *Style) IsSafeJustifyContent() bool {
	if jc, ok := s.Get("justify-content"); ok {
		_, safe := stripSafeUnsafe(jc)
		return safe
	}
	return false
}

// AlignItems represents the align-items property value
type AlignItems string

const (
	AlignItemsNormal       AlignItems = "normal" // initial value; resolves to stretch in flex context
	AlignItemsFlexStart    AlignItems = "flex-start"
	AlignItemsFlexEnd      AlignItems = "flex-end"
	AlignItemsCenter       AlignItems = "center"
	AlignItemsStretch      AlignItems = "stretch"
	AlignItemsBaseline     AlignItems = "baseline"
	AlignItemsLastBaseline AlignItems = "last-baseline"
	AlignItemsSelfStart    AlignItems = "self-start"
	AlignItemsSelfEnd      AlignItems = "self-end"
)

// GetAlignItems returns the align-items value (default: normal, which acts as stretch in flex).
// Strips any "safe"/"unsafe" prefix; use IsSafeAlignItems to check the safe flag.
func (s *Style) GetAlignItems() AlignItems {
	if ai, ok := s.Get("align-items"); ok {
		ai, _ = stripSafeUnsafe(ai)
		switch ai {
		case "normal":
			return AlignItemsNormal
		case "flex-start", "start":
			return AlignItemsFlexStart
		case "flex-end", "end":
			return AlignItemsFlexEnd
		case "center":
			return AlignItemsCenter
		case "baseline", "first baseline":
			return AlignItemsBaseline
		case "last baseline":
			return AlignItemsLastBaseline
		case "stretch":
			return AlignItemsStretch
		case "self-start":
			return AlignItemsSelfStart
		case "self-end":
			return AlignItemsSelfEnd
		}
	}
	return AlignItemsNormal
}

// IsSafeAlignItems returns true if align-items has the "safe" overflow keyword.
func (s *Style) IsSafeAlignItems() bool {
	if ai, ok := s.Get("align-items"); ok {
		_, safe := stripSafeUnsafe(ai)
		return safe
	}
	return false
}

// AlignContent represents the align-content property value
type AlignContent string

const (
	AlignContentNormal       AlignContent = "normal" // initial value; resolves to stretch in flex context
	AlignContentFlexStart    AlignContent = "flex-start"
	AlignContentFlexEnd      AlignContent = "flex-end"
	AlignContentCenter       AlignContent = "center"
	AlignContentStretch      AlignContent = "stretch"
	AlignContentSpaceBetween AlignContent = "space-between"
	AlignContentSpaceAround  AlignContent = "space-around"
	AlignContentSpaceEvenly  AlignContent = "space-evenly"
)

// GetAlignContent returns the align-content value (default: normal, which acts as stretch in flex).
// Strips any "safe"/"unsafe" prefix; use IsSafeAlignContent to check the safe flag.
func (s *Style) GetAlignContent() AlignContent {
	if ac, ok := s.Get("align-content"); ok {
		ac, _ = stripSafeUnsafe(ac)
		switch ac {
		case "normal":
			return AlignContentNormal
		case "flex-start", "start":
			return AlignContentFlexStart
		case "flex-end", "end":
			return AlignContentFlexEnd
		case "center":
			return AlignContentCenter
		case "stretch":
			return AlignContentStretch
		case "space-between":
			return AlignContentSpaceBetween
		case "space-around":
			return AlignContentSpaceAround
		case "space-evenly":
			return AlignContentSpaceEvenly
		}
	}
	return AlignContentNormal
}

// IsSafeAlignContent returns true if align-content has the "safe" overflow keyword.
func (s *Style) IsSafeAlignContent() bool {
	if ac, ok := s.Get("align-content"); ok {
		_, safe := stripSafeUnsafe(ac)
		return safe
	}
	return false
}

// GetFlexGrow returns the flex-grow value (default: 0)
func (s *Style) GetFlexGrow() float64 {
	if val, ok := s.Get("flex-grow"); ok {
		if f, err := strconv.ParseFloat(val, 64); err == nil && f >= 0 {
			return f
		}
	}
	return 0.0
}

// GetFlexShrink returns the flex-shrink value (default: 1)
func (s *Style) GetFlexShrink() float64 {
	if val, ok := s.Get("flex-shrink"); ok {
		if f, err := strconv.ParseFloat(val, 64); err == nil && f >= 0 {
			return f
		}
	}
	return 1.0
}

// FlexBasisValue represents a flex-basis value which can be auto, a length, a percentage, or a calc() expression.
type FlexBasisValue struct {
	IsAuto     bool
	IsContent  bool    // flex-basis: content — always use content size, ignore width/height
	Length     float64 // absolute length in pixels (if not auto and not percentage)
	Percentage float64 // percentage value (if IsPercent)
	IsPercent  bool
	CalcExpr   string  // raw calc() expression string (if IsCalc); resolve with EvalCalcWithPercent
	FontSize   float64 // font size at parse time, needed for em units in CalcExpr
	IsCalc     bool
}

// GetFlexBasisValue returns the structured flex-basis value (default: auto)
func (s *Style) GetFlexBasisValue() FlexBasisValue {
	basis, ok := s.Get("flex-basis")
	if !ok || basis == "auto" {
		return FlexBasisValue{IsAuto: true}
	}
	if basis == "content" {
		return FlexBasisValue{IsContent: true}
	}
	if pct, ok := ParsePercentage(basis); ok {
		return FlexBasisValue{Percentage: pct, IsPercent: true}
	}
	// Handle calc() expressions — defer percentage resolution until container size is known
	if strings.HasPrefix(basis, "calc(") && strings.HasSuffix(basis, ")") {
		expr := basis[5 : len(basis)-1]
		return FlexBasisValue{IsCalc: true, CalcExpr: expr, FontSize: s.GetFontSize()}
	}
	if length, ok := parseLengthFullWithCh(basis, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok {
		// CSS Flexbox §7.3.3: flex-basis does not accept negative lengths
		if length < 0 {
			return FlexBasisValue{IsAuto: true}
		}
		return FlexBasisValue{Length: length}
	}
	return FlexBasisValue{IsAuto: true}
}

// GetFlexBasis returns the flex-basis value (default: auto, returns -1 for auto)
// Deprecated: Use GetFlexBasisValue for proper percentage support.
func (s *Style) GetFlexBasis() float64 {
	if basis, ok := s.Get("flex-basis"); ok {
		if basis == "auto" || basis == "content" {
			return -1
		}
		if length, ok := parseLengthFullWithCh(basis, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight); ok {
			return length
		}
	}
	return -1
}

// AlignSelf represents the align-self property value
type AlignSelf string

const (
	AlignSelfAuto         AlignSelf = "auto"   // initial value — use container's align-items
	AlignSelfNormal       AlignSelf = "normal" // like auto for flex items
	AlignSelfFlexStart    AlignSelf = "flex-start"
	AlignSelfFlexEnd      AlignSelf = "flex-end"
	AlignSelfCenter       AlignSelf = "center"
	AlignSelfStretch      AlignSelf = "stretch"
	AlignSelfBaseline     AlignSelf = "baseline"
	AlignSelfLastBaseline AlignSelf = "last-baseline"
	AlignSelfSelfStart    AlignSelf = "self-start"
	AlignSelfSelfEnd      AlignSelf = "self-end"
)

// GetAlignSelf returns the align-self value (default: auto).
// "auto" and "normal" both mean: use the container's align-items value.
func (s *Style) GetAlignSelf() AlignSelf {
	if as, ok := s.Get("align-self"); ok {
		as, _ = stripSafeUnsafe(as)
		switch as {
		case "auto":
			return AlignSelfAuto
		case "normal":
			return AlignSelfNormal
		case "flex-start", "start":
			return AlignSelfFlexStart
		case "flex-end", "end":
			return AlignSelfFlexEnd
		case "self-start":
			return AlignSelfSelfStart
		case "self-end":
			return AlignSelfSelfEnd
		case "center":
			return AlignSelfCenter
		case "stretch":
			return AlignSelfStretch
		case "initial":
			// CSS spec: 'initial' for align-self is 'auto' — fall through to auto.
			return AlignSelfAuto
		case "baseline", "first baseline":
			return AlignSelfBaseline
		case "last baseline":
			return AlignSelfLastBaseline
		}
	}
	return AlignSelfAuto
}

// IsSafeAlignSelf returns true if align-self has the "safe" overflow keyword.
func (s *Style) IsSafeAlignSelf() bool {
	if as, ok := s.Get("align-self"); ok {
		_, safe := stripSafeUnsafe(as)
		return safe
	}
	return false
}

// GetOrder returns the order value (default: 0)
func (s *Style) GetOrder() int {
	if order, ok := s.Get("order"); ok {
		var o int
		if _, err := fmt.Sscanf(order, "%d", &o); err == nil {
			return o
		}
	}
	return 0
}

// Phase 11: Pseudo-elements

// ContentValue represents a single value in the content property
type ContentValue struct {
	Type  string // "text", "url", "counter", "counters", "attr", "open-quote", "close-quote"
	Value string // The actual value (text content, URL path, counter name, attr name)

	// Separator is the join string for "counters" (counter list separator).
	// For "counter" and the non-counter Types it is unused.
	Separator string

	// Style is the optional <counter-style> argument for "counter" or
	// "counters". Empty means UA default ("decimal"). Phase 1 captures
	// it but does not consume it; Phase 5 will resolve via CounterStyle.
	Style string
}

// GetContent returns the content property value for pseudo-elements
// Returns the content string and true if content is set, or "", false if not
func (s *Style) GetContent() (string, bool) {
	if content, ok := s.Get("content"); ok {
		// Handle "none" and "normal" (no content)
		if content == "none" || content == "normal" {
			return "", false
		}

		// Remove quotes from string content
		content = strings.TrimSpace(content)
		if len(content) >= 2 {
			// Remove single or double quotes
			if (content[0] == '"' && content[len(content)-1] == '"') ||
				(content[0] == '\'' && content[len(content)-1] == '\'') {
				content = content[1 : len(content)-1]
			}
		}

		return content, true
	}
	return "", false
}

// GetContentValues returns the parsed content property as a list of values
// This handles complex content like: counter(ctr) url(img.png) "text" attr(class)
func (s *Style) GetContentValues() ([]ContentValue, bool) {
	raw, ok := s.Get("content")
	if !ok {
		return nil, false
	}

	// Handle "none" and "normal" (no content)
	raw = strings.TrimSpace(raw)
	if raw == "none" || raw == "normal" {
		return nil, false
	}

	values := ParseContentValues(raw)
	// CSS Lists 3 §counter-functions: if any counter()/counters()
	// name is a CSS-wide keyword (an invalid <custom-ident>), the
	// entire content declaration is invalid and is treated as if
	// `content: normal` were specified. Detect that here so the
	// caller does not silently render the surrounding text.
	for _, v := range values {
		if (v.Type == "counter" || v.Type == "counters") && isCSSWideKeyword(v.Value) {
			return nil, false
		}
	}
	return values, true
}

// isCSSWideKeyword reports whether an identifier is one of the CSS-wide
// keywords reserved against use as a <custom-ident> (CSS Values 4
// §custom-idents). Counter names must be valid <custom-ident>s, so
// `counter(inherit)` is a parse error.
func isCSSWideKeyword(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "inherit", "initial", "unset", "revert", "revert-layer", "default", "none":
		return true
	}
	return false
}

// ParseContentValues parses a CSS content value into individual parts
func ParseContentValues(raw string) []ContentValue {
	var values []ContentValue
	raw = strings.TrimSpace(raw)

	for len(raw) > 0 {
		raw = strings.TrimSpace(raw)
		if len(raw) == 0 {
			break
		}

		// Check for quoted string
		if raw[0] == '"' || raw[0] == '\'' {
			quote := raw[0]
			end := 1
			for end < len(raw) && raw[end] != quote {
				if raw[end] == '\\' && end+1 < len(raw) {
					end += 2 // Skip escaped character
				} else {
					end++
				}
			}
			if end < len(raw) {
				text := raw[1:end]
				// Unescape common sequences
				text = strings.ReplaceAll(text, "\\0022", "\"")
				text = strings.ReplaceAll(text, "\\\"", "\"")
				values = append(values, ContentValue{Type: "text", Value: text})
				raw = raw[end+1:]
			} else {
				// Unclosed quote - take rest as text
				values = append(values, ContentValue{Type: "text", Value: raw[1:]})
				break
			}
			continue
		}

		// Check for function-style values: counter(), counters(), url(), attr()
		// First check if the raw string starts with a known function name followed by (
		// (longer "counters" tested before "counter" so the prefix match wins).
		funcIdx := -1
		funcName := ""
		for _, fn := range []string{"counters", "counter", "url", "attr"} {
			if strings.HasPrefix(strings.ToLower(raw), fn+"(") {
				funcIdx = len(fn)
				funcName = fn
				break
			}
		}
		if funcIdx > 0 {
			idx := funcIdx
			// Find matching closing paren
			depth := 1
			start := idx + 1
			end := start
			for end < len(raw) && depth > 0 {
				if raw[end] == '(' {
					depth++
				} else if raw[end] == ')' {
					depth--
				}
				end++
			}
			if depth == 0 {
				arg := strings.TrimSpace(raw[start : end-1])
				switch funcName {
				case "url":
					// Strip quotes if present
					arg = strings.Trim(arg, "\"'")
					values = append(values, ContentValue{Type: "url", Value: arg})
				case "counter":
					// counter(name) or counter(name, style)
					name, _, style := splitCounterArgs(arg)
					values = append(values, ContentValue{
						Type:  "counter",
						Value: name,
						Style: style,
					})
				case "counters":
					// counters(name, separator [, style])
					name, sepAndStyle, _ := splitCounterArgs(arg)
					// Re-split sepAndStyle by comma to separate the
					// separator (quoted) from the optional style.
					sep, style := splitCountersSepAndStyle(sepAndStyle)
					values = append(values, ContentValue{
						Type:      "counters",
						Value:     name,
						Separator: sep,
						Style:     style,
					})
				case "attr":
					values = append(values, ContentValue{Type: "attr", Value: arg})
				}
				raw = raw[end:]
				continue
			}
		}

		// Check for keywords
		lowerRaw := strings.ToLower(raw)
		if strings.HasPrefix(lowerRaw, "open-quote") {
			values = append(values, ContentValue{Type: "open-quote", Value: ""})
			raw = raw[10:]
			continue
		}
		if strings.HasPrefix(lowerRaw, "close-quote") {
			values = append(values, ContentValue{Type: "close-quote", Value: ""})
			raw = raw[11:]
			continue
		}
		if strings.HasPrefix(lowerRaw, "no-open-quote") {
			raw = raw[13:]
			continue
		}
		if strings.HasPrefix(lowerRaw, "no-close-quote") {
			raw = raw[14:]
			continue
		}

		// Unknown content - skip to next space or take rest
		if idx := strings.IndexAny(raw, " \t"); idx > 0 {
			raw = raw[idx:]
		} else {
			break
		}
	}

	return values
}

// splitCounterArgs splits a counter()/counters() argument string at
// its first top-level comma, returning (name, rest, style).
//
//	counter(c)              -> ("c", "", "")
//	counter(c, decimal)     -> ("c", "", "decimal")
//	counters(c, ".")        -> ("c", `"."`, "")
//	counters(c, ".", lower-alpha) -> ("c", `"."`, "lower-alpha")
//
// For counter() (two parts) name and style. For counters() (two or
// three parts) call splitCountersSepAndStyle on `rest` afterward.
func splitCounterArgs(arg string) (name, rest, style string) {
	// Find first top-level comma (respecting nested parens and quotes).
	depth := 0
	inQuote := byte(0)
	first := -1
	for i := 0; i < len(arg); i++ {
		ch := arg[i]
		if inQuote != 0 {
			if ch == inQuote {
				inQuote = 0
			} else if ch == '\\' && i+1 < len(arg) {
				i++
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inQuote = ch
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 && first < 0 {
				first = i
			}
		}
	}
	if first < 0 {
		return strings.TrimSpace(arg), "", ""
	}
	name = strings.TrimSpace(arg[:first])
	tail := strings.TrimSpace(arg[first+1:])
	return name, tail, tail
}

// splitCountersSepAndStyle splits the tail of a counters() argument
// into the separator (quoted string, returned with quotes stripped)
// and an optional style identifier. The tail is the substring after
// the first top-level comma of counters(name, ...).
//
//	`"."` -> (".", "")
//	`".", decimal` -> (".", "decimal")
func splitCountersSepAndStyle(tail string) (sep, style string) {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return "", ""
	}
	// The separator must be a quoted string; find its end.
	if tail[0] != '"' && tail[0] != '\'' {
		// Malformed — keep what we have as the separator literal.
		// Try to split off a trailing style identifier.
		if comma := strings.IndexByte(tail, ','); comma >= 0 {
			return strings.TrimSpace(tail[:comma]), strings.TrimSpace(tail[comma+1:])
		}
		return tail, ""
	}
	quote := tail[0]
	end := 1
	for end < len(tail) && tail[end] != quote {
		if tail[end] == '\\' && end+1 < len(tail) {
			end += 2
		} else {
			end++
		}
	}
	if end >= len(tail) {
		// Unterminated quote — take the rest as the separator.
		return tail[1:], ""
	}
	sep = tail[1:end]
	rest := strings.TrimSpace(tail[end+1:])
	if strings.HasPrefix(rest, ",") {
		style = strings.TrimSpace(rest[1:])
	}
	return sep, style
}

// Phase 15: CSS Grid properties

// GridTrack represents a single grid track (column or row)
type GridTrack struct {
	Size          float64     // Size in pixels (0 for auto)
	Auto          bool        // true if track is auto-sized
	Fr            float64     // fractional unit value (0 if not fr)
	Percent       float64     // percentage value (e.g., 75 for "75%")
	IsMinMax      bool        // true if this is a minmax() track
	MinSize       float64     // minimum size (px) for minmax()
	MaxFr         float64     // maximum as fr value for minmax()
	MaxSize       float64     // maximum as fixed size (px) for minmax()
	MaxAuto       bool        // true if max is "auto" for minmax()
	MinContent    bool        // true if value is "min-content"
	MaxContent    bool        // true if value is "max-content"
	IsFitContent  bool        // true if this is a fit-content() track
	FitContentMax float64     // argument to fit-content() in pixels
	AutoFill      bool        // true if this is a repeat(auto-fill, ...) sentinel
	AutoFit       bool        // true if this is a repeat(auto-fit, ...) sentinel
	AutoTemplate  []GridTrack // template tracks for auto-fill/auto-fit
	IsSubgrid     bool        // true if this represents a "subgrid" keyword
}

// GetGridTemplateColumns parses grid-template-columns and returns track sizes
func (s *Style) GetGridTemplateColumns() []GridTrack {
	if val, ok := s.Get("grid-template-columns"); ok {
		return parseGridTracks(val)
	}
	return nil
}

// GetGridTemplateRows parses grid-template-rows and returns track sizes
func (s *Style) GetGridTemplateRows() []GridTrack {
	if val, ok := s.Get("grid-template-rows"); ok {
		return parseGridTracks(val)
	}
	return nil
}

// GetGridTemplateColumnsWithNames parses grid-template-columns and returns track sizes
// along with a map of named grid lines (name → 1-indexed line number).
func (s *Style) GetGridTemplateColumnsWithNames() ([]GridTrack, map[string]int) {
	if val, ok := s.Get("grid-template-columns"); ok {
		return parseGridTracksWithNames(val)
	}
	return nil, nil
}

// GetGridTemplateRowsWithNames parses grid-template-rows and returns track sizes
// along with a map of named grid lines (name → 1-indexed line number).
func (s *Style) GetGridTemplateRowsWithNames() ([]GridTrack, map[string]int) {
	if val, ok := s.Get("grid-template-rows"); ok {
		return parseGridTracksWithNames(val)
	}
	return nil, nil
}

// GetGridTemplateColumnsIsSubgrid returns true if grid-template-columns is "subgrid".
func (s *Style) GetGridTemplateColumnsIsSubgrid() bool {
	if val, ok := s.Get("grid-template-columns"); ok {
		return strings.TrimSpace(strings.ToLower(val)) == "subgrid"
	}
	return false
}

// GetGridTemplateRowsIsSubgrid returns true if grid-template-rows is "subgrid".
func (s *Style) GetGridTemplateRowsIsSubgrid() bool {
	if val, ok := s.Get("grid-template-rows"); ok {
		return strings.TrimSpace(strings.ToLower(val)) == "subgrid"
	}
	return false
}

// GetGridAutoFlow returns the grid-auto-flow value ("row" or "column").
func (s *Style) GetGridAutoFlow() string {
	if val, ok := s.Get("grid-auto-flow"); ok {
		val = strings.TrimSpace(val)
		// grid-auto-flow can be "row", "column", "row dense", "column dense", etc.
		if strings.HasPrefix(val, "column") {
			return "column"
		}
	}
	return "row"
}

// GetGridAutoRows returns the grid-auto-rows track size for implicit rows
func (s *Style) GetGridAutoRows() *GridTrack {
	if val, ok := s.Get("grid-auto-rows"); ok {
		tracks := parseGridTracks(val)
		if len(tracks) > 0 {
			return &tracks[0]
		}
	}
	return nil
}

// GetGridAutoColumns returns the grid-auto-columns track size for implicit columns
func (s *Style) GetGridAutoColumns() *GridTrack {
	if val, ok := s.Get("grid-auto-columns"); ok {
		tracks := parseGridTracks(val)
		if len(tracks) > 0 {
			return &tracks[0]
		}
	}
	return nil
}

// expandRepeatTracks expands repeat() notation in grid track lists.
// For repeat(N, trackList): returns N copies of the track list tokens.
// For repeat(auto-fill, trackList): returns a single sentinel token "auto-fill:trackList".
// For repeat(auto-fit, trackList): returns a single sentinel token "auto-fit:trackList".
func expandRepeatTracks(parts []string) []string {
	var result []string
	for _, part := range parts {
		if strings.HasPrefix(part, "repeat(") && strings.HasSuffix(part, ")") {
			inner := strings.TrimSpace(part[7 : len(part)-1]) // strip "repeat(" and ")"
			// Find the first comma at depth 0
			commaIdx := -1
			depth := 0
			for i := 0; i < len(inner); i++ {
				switch inner[i] {
				case '(':
					depth++
				case ')':
					depth--
				case ',':
					if depth == 0 {
						commaIdx = i
					}
				}
				if commaIdx >= 0 {
					break
				}
			}
			if commaIdx < 0 {
				// Malformed repeat(), pass through as-is
				result = append(result, part)
				continue
			}
			countStr := strings.TrimSpace(inner[:commaIdx])
			trackListStr := strings.TrimSpace(inner[commaIdx+1:])

			if countStr == "auto-fill" || countStr == "auto-fit" {
				// Sentinel token: "auto-fill:trackList" or "auto-fit:trackList"
				result = append(result, countStr+":"+trackListStr)
			} else {
				// Numeric repeat: expand N times
				count := 0
				if n, err := strconv.Atoi(countStr); err == nil && n > 0 {
					count = n
				}
				templateParts := splitGridTrackValues(trackListStr)
				for i := 0; i < count; i++ {
					result = append(result, templateParts...)
				}
			}
		} else {
			result = append(result, part)
		}
	}
	return result
}

// splitGridTrackValues splits a grid track value string into individual track tokens,
// respecting parentheses (so "minmax(0, 1fr)" stays as one token).
func splitGridTrackValues(val string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(val); i++ {
		switch val[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ' ', '\t':
			if depth == 0 {
				part := strings.TrimSpace(val[start:i])
				if part != "" {
					parts = append(parts, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(val[start:]); part != "" {
		parts = append(parts, part)
	}
	return parts
}

// parseGridTracksWithNames parses grid track definitions and also extracts named line names.
// Named lines like [left] in "100px [left] 200px [right]" map the name to the line index (1-indexed).
// Line 1 is before track 1, line 2 is between track 1 and 2, etc.
// Returns (tracks, lineNames map[name]lineNumber).
func parseGridTracksWithNames(val string) ([]GridTrack, map[string]int) {
	if val == "none" {
		return nil, nil
	}
	if strings.TrimSpace(strings.ToLower(val)) == "subgrid" {
		return []GridTrack{{IsSubgrid: true}}, nil
	}

	tracks := make([]GridTrack, 0)
	lineNames := make(map[string]int)
	rawParts := splitGridTrackValues(val)
	parts := expandRepeatTracks(rawParts)

	// currentLine is 1-indexed: line 1 is before the first track
	currentLine := 1
	pendingNames := []string{} // names to assign to the next line

	for _, part := range parts {
		// Named line: [name] or [name1 name2]
		if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			inner := part[1 : len(part)-1]
			for _, name := range strings.Fields(inner) {
				pendingNames = append(pendingNames, name)
			}
			continue
		}

		// Assign pending names to current line before this track
		for _, name := range pendingNames {
			if _, exists := lineNames[name]; !exists {
				lineNames[name] = currentLine
			}
		}
		pendingNames = pendingNames[:0]

		// Parse the track itself
		var newTrack *GridTrack
		if strings.HasPrefix(part, "auto-fill:") || strings.HasPrefix(part, "auto-fit:") {
			colonIdx := strings.Index(part, ":")
			mode := part[:colonIdx]
			trackListStr := part[colonIdx+1:]
			templateTracks, _ := parseGridTracksWithNames(trackListStr)
			sentinel := GridTrack{AutoTemplate: templateTracks}
			if mode == "auto-fill" {
				sentinel.AutoFill = true
			} else {
				sentinel.AutoFit = true
			}
			newTrack = &sentinel
		} else if part == "auto" {
			t := GridTrack{Auto: true}
			newTrack = &t
		} else if part == "min-content" {
			t := GridTrack{MinContent: true}
			newTrack = &t
		} else if part == "max-content" {
			t := GridTrack{MaxContent: true}
			newTrack = &t
		} else if strings.HasPrefix(part, "minmax(") && strings.HasSuffix(part, ")") {
			inner := part[7 : len(part)-1]
			commaIdx := strings.Index(inner, ",")
			if commaIdx >= 0 {
				minStr := strings.TrimSpace(inner[:commaIdx])
				maxStr := strings.TrimSpace(inner[commaIdx+1:])
				track := GridTrack{IsMinMax: true}
				if minStr == "0" || minStr == "0px" {
					track.MinSize = 0
				} else if minStr == "auto" {
					// MinSize stays 0
				} else if size, ok := ParseLength(minStr); ok {
					track.MinSize = size
				}
				if strings.HasSuffix(maxStr, "fr") {
					frStr := strings.TrimSuffix(maxStr, "fr")
					if fr, err := strconv.ParseFloat(frStr, 64); err == nil {
						track.MaxFr = fr
					}
				} else if maxStr == "auto" {
					track.MaxAuto = true
				} else if size, ok := ParseLength(maxStr); ok {
					track.MaxSize = size
				}
				newTrack = &track
			}
		} else if strings.HasPrefix(part, "fit-content(") && strings.HasSuffix(part, ")") {
			argStr := strings.TrimSpace(part[12 : len(part)-1])
			if size, ok := ParseLength(argStr); ok {
				t := GridTrack{IsFitContent: true, FitContentMax: size}
				newTrack = &t
			}
		} else if strings.HasSuffix(part, "fr") {
			frStr := strings.TrimSuffix(part, "fr")
			if fr, err := strconv.ParseFloat(frStr, 64); err == nil {
				t := GridTrack{Fr: fr}
				newTrack = &t
			}
		} else if strings.HasSuffix(part, "%") {
			numStr := strings.TrimSuffix(part, "%")
			if pct, err := strconv.ParseFloat(numStr, 64); err == nil {
				t := GridTrack{Percent: pct}
				newTrack = &t
			}
		} else if size, ok := ParseLength(part); ok {
			t := GridTrack{Size: size}
			newTrack = &t
		}

		if newTrack != nil {
			tracks = append(tracks, *newTrack)
			currentLine++ // advance to the line after this track
		}
	}

	// Handle trailing named lines (after last track)
	for _, name := range pendingNames {
		if _, exists := lineNames[name]; !exists {
			lineNames[name] = currentLine
		}
	}

	if len(lineNames) == 0 {
		return tracks, nil
	}
	return tracks, lineNames
}

// parseGridTracks parses a space-separated list of track sizes (e.g., "100px 200px auto 1fr")
// Supports minmax(), min-content, max-content, fr, px, rem, auto, and subgrid values.
func parseGridTracks(val string) []GridTrack {
	if val == "none" {
		return nil
	}
	// Handle "subgrid" keyword — return a single sentinel track
	if strings.TrimSpace(strings.ToLower(val)) == "subgrid" {
		return []GridTrack{{IsSubgrid: true}}
	}
	tracks := make([]GridTrack, 0)
	rawParts := splitGridTrackValues(val)
	parts := expandRepeatTracks(rawParts)

	for _, part := range parts {
		if strings.HasPrefix(part, "auto-fill:") || strings.HasPrefix(part, "auto-fit:") {
			colonIdx := strings.Index(part, ":")
			mode := part[:colonIdx]
			trackListStr := part[colonIdx+1:]
			templateTracks := parseGridTracks(trackListStr)
			sentinel := GridTrack{AutoTemplate: templateTracks}
			if mode == "auto-fill" {
				sentinel.AutoFill = true
			} else {
				sentinel.AutoFit = true
			}
			tracks = append(tracks, sentinel)
		} else if part == "auto" {
			tracks = append(tracks, GridTrack{Auto: true})
		} else if part == "min-content" {
			tracks = append(tracks, GridTrack{MinContent: true})
		} else if part == "max-content" {
			tracks = append(tracks, GridTrack{MaxContent: true})
		} else if strings.HasPrefix(part, "minmax(") && strings.HasSuffix(part, ")") {
			inner := part[7 : len(part)-1] // strip "minmax(" and ")"
			// Split on comma at depth 0
			commaIdx := strings.Index(inner, ",")
			if commaIdx < 0 {
				continue
			}
			minStr := strings.TrimSpace(inner[:commaIdx])
			maxStr := strings.TrimSpace(inner[commaIdx+1:])
			track := GridTrack{IsMinMax: true}
			// Parse min
			if minStr == "0" || minStr == "0px" {
				track.MinSize = 0
			} else if minStr == "auto" {
				// MinSize stays 0, treated as auto
			} else if size, ok := ParseLength(minStr); ok {
				track.MinSize = size
			}
			// Parse max
			if strings.HasSuffix(maxStr, "fr") {
				frStr := strings.TrimSuffix(maxStr, "fr")
				if fr, err := strconv.ParseFloat(frStr, 64); err == nil {
					track.MaxFr = fr
				}
			} else if maxStr == "auto" {
				track.MaxAuto = true
			} else if size, ok := ParseLength(maxStr); ok {
				track.MaxSize = size
			}
			tracks = append(tracks, track)
		} else if strings.HasPrefix(part, "fit-content(") && strings.HasSuffix(part, ")") {
			// fit-content(X) = min(max-content, max(min-content, X))
			argStr := strings.TrimSpace(part[12 : len(part)-1])
			if size, ok := ParseLength(argStr); ok {
				tracks = append(tracks, GridTrack{IsFitContent: true, FitContentMax: size})
			}
		} else if strings.HasSuffix(part, "fr") {
			frStr := strings.TrimSuffix(part, "fr")
			if fr, err := strconv.ParseFloat(frStr, 64); err == nil {
				tracks = append(tracks, GridTrack{Fr: fr})
			}
		} else if strings.HasSuffix(part, "%") {
			numStr := strings.TrimSuffix(part, "%")
			if pct, err := strconv.ParseFloat(numStr, 64); err == nil {
				tracks = append(tracks, GridTrack{Percent: pct})
			}
		} else if size, ok := ParseLength(part); ok {
			tracks = append(tracks, GridTrack{Size: size})
		}
	}

	return tracks
}

// GetGridGap returns the grid-gap value (shorthand for row-gap and column-gap)
func (s *Style) GetGridGap() (rowGap, columnGap float64) {
	// Try grid-gap first (older syntax)
	if gap, ok := s.GetLength("grid-gap"); ok {
		return gap, gap
	}

	// Try gap (newer syntax)
	if gap, ok := s.GetLength("gap"); ok {
		return gap, gap
	}

	// Try individual properties
	rowGap, _ = s.GetLength("row-gap")
	columnGap, _ = s.GetLength("column-gap")

	return rowGap, columnGap
}

// GridPlacement represents grid-column or grid-row placement
type GridPlacement struct {
	Start     int  // Starting line (1-indexed), 0 if auto
	End       int  // Ending line (1-indexed, exclusive), 0 if auto
	IsSpan    bool // true if this is a span-only placement (no explicit start)
	SpanCount int  // number of tracks to span (used when IsSpan=true or end is "span N")
}

// GetGridColumn parses grid-column property (e.g., "1 / 3" or "1 / span 2")
func (s *Style) GetGridColumn() *GridPlacement {
	if val, ok := s.Get("grid-column"); ok {
		return parseGridPlacement(val)
	}
	return nil
}

// GetGridColumnWithNames parses grid-column (or individual grid-column-start/end) with named line resolution.
func (s *Style) GetGridColumnWithNames(colLineNames map[string]int) *GridPlacement {
	// Check for individual properties first (grid-column-start / grid-column-end)
	startVal, hasStart := s.Get("grid-column-start")
	endVal, hasEnd := s.Get("grid-column-end")
	if hasStart || hasEnd {
		p := &GridPlacement{}
		if hasStart && startVal != "auto" {
			if strings.HasPrefix(startVal, "span ") {
				var n int
				fmt.Sscanf(strings.TrimSpace(startVal[5:]), "%d", &n)
				if n <= 0 {
					n = 1
				}
				p.IsSpan = true
				p.SpanCount = n
			} else {
				p.Start = resolveLineName(startVal, colLineNames)
			}
		}
		if hasEnd && endVal != "auto" {
			if strings.HasPrefix(endVal, "span ") {
				var n int
				fmt.Sscanf(strings.TrimSpace(endVal[5:]), "%d", &n)
				if n <= 0 {
					n = 1
				}
				p.SpanCount = n
				if p.Start > 0 {
					p.End = p.Start + n
				} else {
					p.IsSpan = true
				}
			} else {
				p.End = resolveLineName(endVal, colLineNames)
			}
		}
		if p.Start == 0 && p.End == 0 && !p.IsSpan {
			return nil
		}
		if p.Start > 0 && p.End == 0 && !p.IsSpan {
			p.End = p.Start + 1
		}
		return p
	}
	if val, ok := s.Get("grid-column"); ok {
		return parseGridPlacementWithNames(val, colLineNames, nil)
	}
	return nil
}

// GetGridRow parses grid-row property (e.g., "2 / 4")
func (s *Style) GetGridRow() *GridPlacement {
	if val, ok := s.Get("grid-row"); ok {
		return parseGridPlacement(val)
	}
	return nil
}

// GetGridRowWithNames parses grid-row (or individual grid-row-start/end) with named line resolution.
func (s *Style) GetGridRowWithNames(rowLineNames map[string]int) *GridPlacement {
	// Check for individual properties first (grid-row-start / grid-row-end)
	startVal, hasStart := s.Get("grid-row-start")
	endVal, hasEnd := s.Get("grid-row-end")
	if hasStart || hasEnd {
		p := &GridPlacement{}
		if hasStart && startVal != "auto" {
			if strings.HasPrefix(startVal, "span ") {
				var n int
				fmt.Sscanf(strings.TrimSpace(startVal[5:]), "%d", &n)
				if n <= 0 {
					n = 1
				}
				p.IsSpan = true
				p.SpanCount = n
			} else {
				p.Start = resolveLineName(startVal, rowLineNames)
			}
		}
		if hasEnd && endVal != "auto" {
			if strings.HasPrefix(endVal, "span ") {
				var n int
				fmt.Sscanf(strings.TrimSpace(endVal[5:]), "%d", &n)
				if n <= 0 {
					n = 1
				}
				p.SpanCount = n
				if p.Start > 0 {
					p.End = p.Start + n
				} else {
					p.IsSpan = true
				}
			} else {
				p.End = resolveLineName(endVal, rowLineNames)
			}
		}
		if p.Start == 0 && p.End == 0 && !p.IsSpan {
			return nil
		}
		if p.Start > 0 && p.End == 0 && !p.IsSpan {
			p.End = p.Start + 1
		}
		return p
	}
	if val, ok := s.Get("grid-row"); ok {
		return parseGridPlacementWithNames(val, rowLineNames, nil)
	}
	return nil
}

// GridAreaInfo holds grid area placement (1-indexed, end exclusive)
type GridAreaInfo struct {
	RowStart, RowEnd, ColStart, ColEnd int
}

// GetGridTemplateAreas parses the grid-template-areas property.
// Returns a map from area name to its grid position.
func (s *Style) GetGridTemplateAreas() map[string]GridAreaInfo {
	v, ok := s.Get("grid-template-areas")
	if !ok || v == "" || v == "none" {
		return nil
	}
	areas := make(map[string]GridAreaInfo)

	// Extract quoted row strings
	rowNum := 0
	i := 0
	for i < len(v) {
		// Skip whitespace between rows
		for i < len(v) && (v[i] == ' ' || v[i] == '\t' || v[i] == '\n' || v[i] == '\r') {
			i++
		}
		if i >= len(v) {
			break
		}
		if v[i] == '"' || v[i] == '\'' {
			quote := v[i]
			i++ // skip opening quote
			rowNum++
			rowStr := ""
			for i < len(v) && v[i] != quote {
				rowStr += string(v[i])
				i++
			}
			if i < len(v) {
				i++ // skip closing quote
			}
			// Parse cells in this row
			cells := strings.Fields(rowStr)
			for colIdx, cell := range cells {
				if cell == "." || cell == "" {
					continue
				}
				colNum := colIdx + 1
				if info, exists := areas[cell]; exists {
					// Extend the area (for multi-row areas)
					newInfo := info
					if rowNum+1 > newInfo.RowEnd {
						newInfo.RowEnd = rowNum + 1
					}
					if colNum+1 > newInfo.ColEnd {
						newInfo.ColEnd = colNum + 1
					}
					areas[cell] = newInfo
				} else {
					areas[cell] = GridAreaInfo{
						RowStart: rowNum,
						RowEnd:   rowNum + 1,
						ColStart: colNum,
						ColEnd:   colNum + 1,
					}
				}
			}
		} else {
			i++
		}
	}
	return areas
}

// GetGridArea returns the grid-area value (a named area or "row-start/col-start/row-end/col-end")
func (s *Style) GetGridArea() string {
	if v, ok := s.Get("grid-area"); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// resolveLineName resolves a grid line token to a line number.
// If the token is a number, it returns that number directly.
// If it's a named line (and lineNames is non-nil), it looks it up in the map.
// Returns 0 if unresolvable.
func resolveLineName(token string, lineNames map[string]int) int {
	token = strings.TrimSpace(token)
	if token == "" || token == "auto" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(token, "%d", &n); err == nil && n != 0 {
		return n
	}
	if lineNames != nil {
		if lineNum, ok := lineNames[token]; ok {
			return lineNum
		}
	}
	return 0
}

// parseGridPlacement parses grid line placement (e.g., "1 / 3", "1 / span 2", "span 4", "1")
// lineNames optionally maps named grid lines to 1-indexed line numbers.
func parseGridPlacement(val string) *GridPlacement {
	return parseGridPlacementWithNames(val, nil, nil)
}

// parseGridPlacementWithNames parses grid line placement with optional named line resolution.
// colLineNames is for column (grid-column) placements; rowLineNames for row (grid-row) placements.
// Only one of the two is used depending on which axis is being parsed — pass the relevant one.
func parseGridPlacementWithNames(val string, lineNames map[string]int, _ map[string]int) *GridPlacement {
	val = strings.TrimSpace(val)
	if val == "" || val == "auto" {
		return nil
	}

	parts := strings.Split(val, "/")

	start := strings.TrimSpace(parts[0])

	// Single value cases
	if len(parts) == 1 {
		// "span N" — auto start, span N tracks
		if strings.HasPrefix(start, "span ") {
			spanStr := strings.TrimSpace(start[5:])
			var n int
			fmt.Sscanf(spanStr, "%d", &n)
			if n <= 0 {
				n = 1
			}
			return &GridPlacement{IsSpan: true, SpanCount: n}
		}
		// Try as number or named line
		startNum := resolveLineName(start, lineNames)
		if startNum == 0 {
			return nil
		}
		return &GridPlacement{Start: startNum, End: startNum + 1}
	}

	// Two-part: "start / end"
	end := strings.TrimSpace(parts[1])

	// Parse start
	startNum := 0
	if start != "auto" {
		startNum = resolveLineName(start, lineNames)
	}

	// Parse end: may be "span N" or a line number / named line
	if strings.HasPrefix(end, "span ") {
		spanStr := strings.TrimSpace(end[5:])
		var n int
		fmt.Sscanf(spanStr, "%d", &n)
		if n <= 0 {
			n = 1
		}
		if startNum > 0 {
			return &GridPlacement{Start: startNum, End: startNum + n, SpanCount: n}
		}
		// auto start + span N
		return &GridPlacement{IsSpan: true, SpanCount: n}
	}

	endNum := 0
	if end != "auto" {
		endNum = resolveLineName(end, lineNames)
	}
	if startNum == 0 && endNum == 0 {
		return nil
	}
	if startNum == 0 {
		// auto / N — treat as line N, span 1 back
		return &GridPlacement{Start: endNum - 1, End: endNum}
	}
	if endNum == 0 {
		return &GridPlacement{Start: startNum, End: startNum + 1}
	}
	return &GridPlacement{Start: startNum, End: endNum}
}

// JustifyItems represents the justify-items property value for grid
type JustifyItems string

const (
	JustifyItemsStart   JustifyItems = "start"
	JustifyItemsEnd     JustifyItems = "end"
	JustifyItemsCenter  JustifyItems = "center"
	JustifyItemsStretch JustifyItems = "stretch"
)

// GetJustifyItems returns the justify-items value (default: stretch)
func (s *Style) GetJustifyItems() JustifyItems {
	if val, ok := s.Get("justify-items"); ok {
		switch val {
		case "start":
			return JustifyItemsStart
		case "end":
			return JustifyItemsEnd
		case "center":
			return JustifyItemsCenter
		}
	}
	return JustifyItemsStretch
}

// Note: We can reuse AlignItems from flexbox for align-items in grid

// Phase 16: CSS Transforms

// Transform represents a CSS transform.
// For translate*, each Values[i] is either a pixel length or a percentage; the
// corresponding IsPercent[i] disambiguates. Non-translate transforms ignore
// IsPercent (angles, scalars).
type Transform struct {
	Type      string
	Values    []float64
	IsPercent []bool
}

// GetTransforms parses the transform property and returns a list of transforms
func (s *Style) GetTransforms() []Transform {
	if val, ok := s.Get("transform"); ok {
		if val == "none" {
			return nil
		}
		return parseTransforms(val)
	}
	return nil
}

// parseTransforms parses transform functions (e.g., "translate(10px, 20px) rotate(45deg)")
func parseTransforms(val string) []Transform {
	transforms := make([]Transform, 0)

	// Simple parser for transform functions
	i := 0
	for i < len(val) {
		// Skip whitespace
		for i < len(val) && val[i] == ' ' {
			i++
		}
		if i >= len(val) {
			break
		}

		// Find function name
		start := i
		for i < len(val) && val[i] != '(' {
			i++
		}
		if i >= len(val) {
			break
		}

		funcName := val[start:i]
		i++ // Skip '('

		// Find function arguments
		argStart := i
		depth := 1
		for i < len(val) && depth > 0 {
			if val[i] == '(' {
				depth++
			} else if val[i] == ')' {
				depth--
			}
			i++
		}

		args := val[argStart : i-1]

		// Parse the transform
		transform := parseTransformFunction(funcName, args)
		if transform != nil {
			transforms = append(transforms, *transform)
		}
	}

	return transforms
}

// parseTransformFunction parses a single transform function
func parseTransformFunction(name, args string) *Transform {
	name = strings.TrimSpace(name)
	args = strings.TrimSpace(args)

	switch name {
	case "translate":
		// translate(x, y) or translate(x)
		parts := strings.Split(args, ",")
		values := make([]float64, 0, 2)
		pct := make([]bool, 0, 2)
		for _, part := range parts {
			if v, isPct, ok := parseTransformValue(strings.TrimSpace(part)); ok {
				values = append(values, v)
				pct = append(pct, isPct)
			}
		}
		if len(values) == 1 {
			values = append(values, 0)
			pct = append(pct, false)
		}
		if len(values) >= 2 {
			return &Transform{Type: "translate", Values: values[:2], IsPercent: pct[:2]}
		}

	case "translateX":
		if v, isPct, ok := parseTransformValue(args); ok {
			return &Transform{Type: "translate", Values: []float64{v, 0}, IsPercent: []bool{isPct, false}}
		}

	case "translateY":
		if v, isPct, ok := parseTransformValue(args); ok {
			return &Transform{Type: "translate", Values: []float64{0, v}, IsPercent: []bool{false, isPct}}
		}

	case "rotate":
		// rotate(45deg)
		if val := parseAngle(args); val != nil {
			return &Transform{Type: "rotate", Values: []float64{*val}}
		}

	case "scale":
		// scale(x, y) or scale(x)
		parts := strings.Split(args, ",")
		values := make([]float64, 0)
		for _, part := range parts {
			if val, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err == nil {
				values = append(values, val)
			}
		}
		if len(values) == 1 {
			values = append(values, values[0]) // y defaults to x
		}
		if len(values) >= 2 {
			return &Transform{Type: "scale", Values: values[:2]}
		}

	case "scaleX":
		if val, err := strconv.ParseFloat(args, 64); err == nil {
			return &Transform{Type: "scale", Values: []float64{val, 1}}
		}

	case "scaleY":
		if val, err := strconv.ParseFloat(args, 64); err == nil {
			return &Transform{Type: "scale", Values: []float64{1, val}}
		}

	case "skewX":
		if val := parseAngle(args); val != nil {
			return &Transform{Type: "skew", Values: []float64{*val, 0}}
		}

	case "skewY":
		if val := parseAngle(args); val != nil {
			return &Transform{Type: "skew", Values: []float64{0, *val}}
		}

	case "skew":
		parts := strings.Split(args, ",")
		values := make([]float64, 0)
		for _, part := range parts {
			if val := parseAngle(strings.TrimSpace(part)); val != nil {
				values = append(values, *val)
			}
		}
		if len(values) == 1 {
			values = append(values, 0)
		}
		if len(values) >= 2 {
			return &Transform{Type: "skew", Values: values[:2]}
		}

	case "matrix":
		// matrix(a, b, c, d, e, f) — 2D affine transform matrix
		// a=XX, b=YX, c=XY, d=YY, e=X0, f=Y0
		parts := strings.Split(args, ",")
		values := make([]float64, 0)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if val, err := strconv.ParseFloat(part, 64); err == nil {
				values = append(values, val)
			}
		}
		if len(values) == 6 {
			return &Transform{Type: "matrix", Values: values}
		}
	}

	return nil
}

// parseTransformValue parses a length-or-percentage value for a translate
// component. Returns (value, isPercent, ok). Legitimate negative pixel
// lengths (e.g. `-50px`) are preserved — a prior sign-sentinel encoding
// silently flipped such values into positive percentages.
func parseTransformValue(val string) (float64, bool, bool) {
	val = strings.TrimSpace(val)

	if strings.HasSuffix(val, "%") {
		percentStr := strings.TrimSuffix(val, "%")
		if percent, err := strconv.ParseFloat(percentStr, 64); err == nil {
			return percent, true, true
		}
		return 0, false, false
	}

	val = strings.TrimSuffix(val, "px")
	if length, err := strconv.ParseFloat(val, 64); err == nil {
		return length, false, true
	}

	return 0, false, false
}

// parseAngle parses an angle value and returns degrees, or nil on failure.
// Accepts the four CSS Values 3 §6.2 angle units: deg, grad, rad, turn
// (case-insensitive). Delegates to parseAngleValue (gradient.go) so transform
// and gradient angle parsing share one strict unit table.
func parseAngle(val string) *float64 {
	deg, ok := parseAngleValue(val)
	if !ok {
		return nil
	}
	return &deg
}

// TransformOrigin represents the transform-origin property
type TransformOrigin struct {
	X float64 // 0.0 = left, 0.5 = center, 1.0 = right
	Y float64 // 0.0 = top, 0.5 = center, 1.0 = bottom
}

// GetTransformOrigin parses transform-origin (default: center center = 50% 50%)
func (s *Style) GetTransformOrigin() TransformOrigin {
	if val, ok := s.Get("transform-origin"); ok {
		// Paren-aware split so `calc(50% - 3px)` survives as one token.
		parts := splitShorthandParts(val)
		origin := TransformOrigin{X: 0.5, Y: 0.5} // Default center center

		if len(parts) >= 1 {
			origin.X = parseOriginValue(parts[0])
		}
		if len(parts) >= 2 {
			origin.Y = parseOriginValue(parts[1])
		}

		return origin
	}
	return TransformOrigin{X: 0.5, Y: 0.5} // Default center center
}

// ResolveTransformOriginPx returns transform-origin resolved to absolute
// pixel offsets within the element's border box, given the box dimensions.
// Supports length, percent, calc() (including calc-with-percent), and the
// `left/center/right/top/bottom` keywords. Returns (originX, originY) in
// pixels, relative to the box's top-left corner. Defaults to center center.
// Mirrors Blink's TransformOrigin::ResolveAxisFromConvert with width/height.
//
// Per CSS Transforms 1 §6: with two keyword tokens, order is interchangeable —
// `top left` is equivalent to `left top`. Length/percent components must
// appear in (x, y) order.
func (s *Style) ResolveTransformOriginPx(boxW, boxH float64) (float64, float64) {
	val, ok := s.Get("transform-origin")
	if !ok {
		return 0.5 * boxW, 0.5 * boxH
	}
	parts := splitShorthandParts(val)
	fs := s.GetFontSize()
	vw, vh, ch, xh := s.ViewportWidth, s.ViewportHeight, s.chScale(), s.XHeight
	x := 0.5 * boxW
	y := 0.5 * boxH
	if len(parts) == 1 {
		// Single value: y-keyword applies to vertical axis (x defaults to center);
		// anything else applies to horizontal axis (y defaults to center).
		if isVerticalKeyword(parts[0]) {
			y = resolveOriginAxisKeywordOrLength(parts[0], fs, vw, vh, ch, xh, boxH)
		} else {
			x = resolveOriginAxisKeywordOrLength(parts[0], fs, vw, vh, ch, xh, boxW)
		}
		return x, y
	}
	if len(parts) >= 2 {
		// Two values: swap if the first is a vertical keyword AND the second
		// is a horizontal keyword (e.g. `top left` → treat as `left top`).
		first, second := parts[0], parts[1]
		if isVerticalKeyword(first) && isHorizontalKeyword(second) {
			first, second = second, first
		}
		x = resolveOriginAxisKeywordOrLength(first, fs, vw, vh, ch, xh, boxW)
		y = resolveOriginAxisKeywordOrLength(second, fs, vw, vh, ch, xh, boxH)
	}
	return x, y
}

// isHorizontalKeyword reports whether val names an x-axis position keyword.
func isHorizontalKeyword(val string) bool {
	switch strings.TrimSpace(val) {
	case "left", "right":
		return true
	}
	return false
}

// isVerticalKeyword reports whether val names a y-axis position keyword.
func isVerticalKeyword(val string) bool {
	switch strings.TrimSpace(val) {
	case "top", "bottom":
		return true
	}
	return false
}

// resolveOriginAxisKeywordOrLength resolves a single transform-origin
// component to absolute pixels along the given axis. boxLen is the box's
// extent in this axis. Accepts left/top (= 0), right/bottom (= boxLen),
// center (= 0.5*boxLen), percent, length, and calc() (including calc with %).
func resolveOriginAxisKeywordOrLength(val string, fontSize, vw, vh, chScale, xHeight, boxLen float64) float64 {
	val = strings.TrimSpace(val)
	switch val {
	case "left", "top":
		return 0
	case "center":
		return 0.5 * boxLen
	case "right", "bottom":
		return boxLen
	}
	// Percentage
	if strings.HasSuffix(val, "%") {
		if percent, err := strconv.ParseFloat(strings.TrimSuffix(val, "%"), 64); err == nil {
			return percent / 100.0 * boxLen
		}
	}
	// CSS Values §10.2: calc() with percent resolves against the axis extent.
	if IsCalcWithPercent(val) {
		ctx := calcContext{
			fontSize:       fontSize,
			percentBase:    boxLen,
			viewportWidth:  vw,
			viewportHeight: vh,
			chScale:        chScale,
			xHeight:        xHeight,
		}
		if r, ok := evalCalcFull(val[5:len(val)-1], ctx); ok {
			return r
		}
	}
	// Plain length / calc() without percent.
	if length, ok := parseLengthFullWithCh(val, fontSize, vw, vh, chScale, xHeight); ok {
		return length
	}
	return 0.5 * boxLen // Default to center on parse failure
}

// parseOriginValue parses a single origin value (left/center/right/top/bottom or percentage)
// Returns a 0-1 fraction. Use ResolveTransformOriginPx for calc/length-aware resolution.
func parseOriginValue(val string) float64 {
	val = strings.TrimSpace(val)

	switch val {
	case "left", "top":
		return 0.0
	case "center":
		return 0.5
	case "right", "bottom":
		return 1.0
	}

	// Try percentage
	if strings.HasSuffix(val, "%") {
		percentStr := strings.TrimSuffix(val, "%")
		if percent, err := strconv.ParseFloat(percentStr, 64); err == nil {
			return percent / 100.0
		}
	}

	// Try pixels (convert to 0-1 range... but we don't know element size here)
	// For now, just use as-is
	if length, ok := ParseLength(val); ok {
		return length / 100.0 // Rough approximation
	}

	return 0.5 // Default to center
}

// GetIndividualScale returns the CSS `scale` individual transform property as (sx, sy, hasValue).
// A single value sets both axes uniformly.
func (s *Style) GetIndividualScale() (float64, float64, bool) {
	if val, ok := s.Get("scale"); ok {
		if val == "none" || val == "" {
			return 1, 1, false
		}
		parts := strings.Fields(val)
		if len(parts) == 1 {
			if v, err := strconv.ParseFloat(parts[0], 64); err == nil {
				return v, v, true
			}
		} else if len(parts) >= 2 {
			vx, err1 := strconv.ParseFloat(parts[0], 64)
			vy, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 == nil && err2 == nil {
				return vx, vy, true
			}
		}
	}
	return 1, 1, false
}

// GetIndividualRotate returns the CSS `rotate` individual transform property as (degrees, hasValue).
// Supports deg, turn, and rad angle units.
func (s *Style) GetIndividualRotate() (float64, bool) {
	if val, ok := s.Get("rotate"); ok {
		if val == "none" || val == "" {
			return 0, false
		}
		val = strings.TrimSpace(val)
		if strings.HasSuffix(val, "deg") {
			if v, err := strconv.ParseFloat(strings.TrimSuffix(val, "deg"), 64); err == nil {
				return v, true
			}
		} else if strings.HasSuffix(val, "turn") {
			if v, err := strconv.ParseFloat(strings.TrimSuffix(val, "turn"), 64); err == nil {
				return v * 360, true
			}
		} else if strings.HasSuffix(val, "rad") {
			if v, err := strconv.ParseFloat(strings.TrimSuffix(val, "rad"), 64); err == nil {
				return v * 180 / math.Pi, true
			}
		}
		// Try bare number as degrees
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

// GetIndividualTranslate returns the CSS `translate` individual transform
// property as (tx, ty, txPercent, tyPercent, hasValue). A single value sets
// the X translation only. Percentage and length values are distinguished by
// the explicit flags rather than a sign sentinel, so `translate: 0 -50px`
// round-trips correctly.
func (s *Style) GetIndividualTranslate() (float64, float64, bool, bool, bool) {
	if val, ok := s.Get("translate"); ok {
		if val == "none" || val == "" {
			return 0, 0, false, false, false
		}
		parts := strings.Fields(val)
		if len(parts) == 1 {
			if v, isPct, ok := parseTransformValue(parts[0]); ok {
				return v, 0, isPct, false, true
			}
		} else if len(parts) >= 2 {
			vx, xPct, okx := parseTransformValue(parts[0])
			vy, yPct, oky := parseTransformValue(parts[1])
			if okx && oky {
				return vx, vy, xPct, yPct, true
			}
		}
	}
	return 0, 0, false, false, false
}

// Phase 24: Background image support

// ParseURLValue extracts the URL from a CSS url(...) value.
// Handles url(path), url('path'), url("path").
// Returns the URL string and true if valid, or "", false otherwise.
func ParseURLValue(val string) (string, bool) {
	val = strings.TrimSpace(val)
	if !strings.HasPrefix(val, "url(") || !strings.HasSuffix(val, ")") {
		return "", false
	}
	inner := val[4 : len(val)-1]
	inner = strings.TrimSpace(inner)
	// Remove quotes if present
	if len(inner) >= 2 {
		if (inner[0] == '"' && inner[len(inner)-1] == '"') ||
			(inner[0] == '\'' && inner[len(inner)-1] == '\'') {
			inner = inner[1 : len(inner)-1]
		}
	}
	if inner == "" {
		return "", false
	}
	return inner, true
}

// GetBackgroundImage returns the background-image URL if set.
// Checks both background-image and the background shorthand.
// Handles CSS multiple background layers (comma-separated) by extracting the first url().
// Also handles image-set() / -webkit-image-set() by extracting the first candidate URL.
func (s *Style) GetBackgroundImage() (string, bool) {
	if val, ok := s.Get("background-image"); ok {
		lowerVal := strings.ToLower(val)

		// Handle image-set() / -webkit-image-set() — select first URL candidate
		if strings.Contains(lowerVal, "image-set(") {
			if url, ok := extractImageSetFirstURL(val); ok {
				return url, true
			}
		}

		// For multi-layer values like "url(img.svg), linear-gradient(...)",
		// use extractFirstURL which correctly handles nested parens.
		// ParseURLValue would greedily match from url( to the last ).
		if strings.Contains(val, ",") {
			if url, ok := extractFirstURL(val); ok {
				return url, true
			}
		}
		if url, ok := ParseURLValue(val); ok {
			return url, true
		}
	}
	return "", false
}

// extractFirstURL extracts the URL from the first url() function in a value
// that may contain multiple comma-separated background layers.
func extractFirstURL(val string) (string, bool) {
	idx := strings.Index(val, "url(")
	if idx < 0 {
		return "", false
	}
	// Find matching closing paren
	depth := 0
	for i := idx + 4; i < len(val); i++ {
		if val[i] == '(' {
			depth++
		} else if val[i] == ')' {
			if depth == 0 {
				urlPart := val[idx : i+1]
				return ParseURLValue(urlPart)
			}
			depth--
		}
	}
	return "", false
}

// extractImageSetFirstURL extracts the first URL from an image-set() or -webkit-image-set() expression.
// image-set(url("a.png") 1x, url("b.png") 2x)  → "a.png"
// image-set("a.png" 1x, "b.png" 2x)             → "a.png"  (bare string, no url() wrapper)
// image-set(url("a.avif") type("image/avif"), url("b.jpg") type("image/jpeg")) → "a.avif"
// Returns the extracted URL string (without url() wrapper) and true on success.
func extractImageSetFirstURL(val string) (string, bool) {
	lower := strings.ToLower(val)

	// Find "image-set(" or "-webkit-image-set("
	setIdx := strings.Index(lower, "image-set(")
	if setIdx < 0 {
		return "", false
	}
	// Advance past "image-set("
	innerStart := setIdx + len("image-set(")

	// Find the matching close paren for the image-set() call
	depth := 1
	innerEnd := innerStart
	for innerEnd < len(val) && depth > 0 {
		switch val[innerEnd] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth > 0 {
			innerEnd++
		}
	}
	if depth != 0 {
		return "", false
	}

	// inner is the content inside image-set(...)
	inner := val[innerStart:innerEnd]

	// Split into comma-separated candidates respecting nested parens
	entries := splitCSSFunctionArgs(inner)
	if len(entries) == 0 {
		return "", false
	}

	// Use the first entry
	first := strings.TrimSpace(entries[0])

	// Each entry has the form: url("path") <descriptor>  OR  "path" <descriptor>
	// where <descriptor> is 1x / 2x / type("...") etc.
	// We need to strip the trailing descriptor token(s) that come after the URL/string.

	// Case 1: entry starts with url(...)
	if strings.HasPrefix(strings.ToLower(first), "url(") {
		// Extract just the url(...) portion
		urlEnd := 4 // past "url("
		d := 1
		for urlEnd < len(first) && d > 0 {
			switch first[urlEnd] {
			case '(':
				d++
			case ')':
				d--
			}
			if d > 0 {
				urlEnd++
			}
		}
		urlPart := first[:urlEnd+1]
		return ParseURLValue(urlPart)
	}

	// Case 2: bare quoted string — e.g. "img-1x.png" 1x
	first = strings.TrimSpace(first)
	// Strip trailing descriptor tokens (anything after the closing quote)
	if len(first) > 0 && (first[0] == '"' || first[0] == '\'') {
		q := first[0]
		closeQ := strings.IndexByte(first[1:], q)
		if closeQ >= 0 {
			urlStr := first[1 : closeQ+1] // content between quotes
			return urlStr, true
		}
	}

	return "", false
}

// FillLayer represents one layer in a multi-layer background (or mask).
// Linked list: head = topmost CSS layer (painted last), tail = bottommost.
// Mirrors Blink's FillLayer (third_party/blink/renderer/core/style/fill_layer.h).
type FillLayer struct {
	Next *FillLayer // next layer toward bottom; nil = bottommost

	Image      *CSSImageValue // typed url() ref, nil = none (LOU-138 phase 7)
	Gradient   string         // raw gradient string, empty = none
	Repeat     BackgroundRepeat
	Position   BackgroundPosition
	Size       BackgroundSize
	Origin     BackgroundOriginType
	Clip       BackgroundClipType
	Attachment BackgroundAttachmentType

	// Per-property "set" flags — true if explicitly specified for this layer.
	ImageSet      bool
	RepeatSet     bool
	PositionSet   bool
	SizeSet       bool
	OriginSet     bool
	ClipSet       bool
	AttachmentSet bool
}

// IsBottomLayer returns true if this is the last layer (background-color applies here).
func (fl *FillLayer) IsBottomLayer() bool { return fl.Next == nil }

// Len returns the number of layers in the list.
func (fl *FillLayer) Len() int {
	n := 0
	for cur := fl; cur != nil; cur = cur.Next {
		n++
	}
	return n
}

// IterateReverse calls fn for each layer from bottom to top (Blink paint order).
func (fl *FillLayer) IterateReverse(fn func(*FillLayer)) {
	if fl == nil {
		return
	}
	if fl.Next != nil {
		fl.Next.IterateReverse(fn)
	}
	fn(fl)
}

// FillUnsetProperties cycles unset properties from the head of the list,
// mirroring Blink's FillLayer::FillUnsetProperties.
func (fl *FillLayer) FillUnsetProperties() {
	if fl == nil {
		return
	}
	for cur := fl; cur != nil; cur = cur.Next {
		if !cur.RepeatSet {
			cur.Repeat = BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}
		}
		if !cur.PositionSet {
			cur.Position = BackgroundPosition{}
		}
		if !cur.SizeSet {
			cur.Size = BackgroundSize{}
		}
		if !cur.OriginSet {
			cur.Origin = BackgroundOriginPaddingBox
		}
		if !cur.ClipSet {
			cur.Clip = BackgroundClipBorderBox
		}
		if !cur.AttachmentSet {
			cur.Attachment = BackgroundAttachmentScroll
		}
	}
}

// CullEmptyLayers removes trailing layers that have no image or gradient.
// Keeps at least one layer (the head) even if empty.
func (fl *FillLayer) CullEmptyLayers() *FillLayer {
	if fl == nil {
		return nil
	}
	// Always keep the head. Cull from the tail.
	fl.Next = fl.Next.cullEmpty()
	return fl
}

func (fl *FillLayer) cullEmpty() *FillLayer {
	if fl == nil {
		return nil
	}
	fl.Next = fl.Next.cullEmpty()
	if !fl.ImageSet && fl.Gradient == "" && fl.Image == nil {
		return fl.Next
	}
	return fl
}

// BackgroundRepeatType represents a per-axis background-repeat value.
// Mirrors Blink's EFillRepeat (kNoRepeatFill / kRepeatFill / kRoundFill /
// kSpaceFill) at third_party/blink/renderer/core/style/style_background_data.h
// @ 4883d11fef. The two-value shorthand (e.g. "repeat-x" / "space round") is
// expanded into BackgroundRepeat.X and BackgroundRepeat.Y.
type BackgroundRepeatType string

const (
	BackgroundRepeatRepeat   BackgroundRepeatType = "repeat"
	BackgroundRepeatNoRepeat BackgroundRepeatType = "no-repeat"
	BackgroundRepeatRepeatX  BackgroundRepeatType = "repeat-x"
	BackgroundRepeatRepeatY  BackgroundRepeatType = "repeat-y"
	BackgroundRepeatSpace    BackgroundRepeatType = "space"
	BackgroundRepeatRound    BackgroundRepeatType = "round"
)

// BackgroundRepeat holds the per-axis background-repeat values after
// shorthand expansion. Each axis is one of repeat/no-repeat/space/round.
type BackgroundRepeat struct {
	X BackgroundRepeatType
	Y BackgroundRepeatType
}

// ExpandRepeatShorthand turns a one- or two-value background-repeat token
// list into per-axis values.
//   - "repeat-x" → (repeat, no-repeat)
//   - "repeat-y" → (no-repeat, repeat)
//   - single value v → (v, v)
//   - "v1 v2"   → (v1, v2)
// Returns the default (repeat, repeat) for an unrecognized first token.
func ExpandRepeatShorthand(val string) BackgroundRepeat {
	val = strings.TrimSpace(val)
	if val == "" {
		return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}
	}
	parts := strings.Fields(val)
	if len(parts) == 0 {
		return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}
	}
	first, ok := parseAxisRepeatKeyword(parts[0])
	if !ok {
		// Handle the single-token shorthands first.
		switch strings.ToLower(parts[0]) {
		case "repeat-x":
			return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatNoRepeat}
		case "repeat-y":
			return BackgroundRepeat{X: BackgroundRepeatNoRepeat, Y: BackgroundRepeatRepeat}
		}
		return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}
	}
	if len(parts) == 1 {
		// "repeat-x" / "repeat-y" already handled above; single axis keyword
		// applies to both axes.
		return BackgroundRepeat{X: first, Y: first}
	}
	second, ok := parseAxisRepeatKeyword(parts[1])
	if !ok {
		return BackgroundRepeat{X: first, Y: first}
	}
	return BackgroundRepeat{X: first, Y: second}
}

// isAxisRepeatToken returns true if val is one of the per-axis repeat
// keywords (repeat / no-repeat / space / round). Used by the background
// shorthand tokenizer.
func isAxisRepeatToken(val string) bool {
	_, ok := parseAxisRepeatKeyword(val)
	return ok
}

// parseAxisRepeatKeyword parses a single per-axis repeat keyword.
// Accepts repeat / no-repeat / space / round (not the repeat-x / repeat-y
// shorthand tokens — those are expanded by ExpandRepeatShorthand).
func parseAxisRepeatKeyword(val string) (BackgroundRepeatType, bool) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "repeat":
		return BackgroundRepeatRepeat, true
	case "no-repeat":
		return BackgroundRepeatNoRepeat, true
	case "space":
		return BackgroundRepeatSpace, true
	case "round":
		return BackgroundRepeatRound, true
	}
	return BackgroundRepeatRepeat, false
}

// BackgroundAttachmentType represents the background-attachment property value.
type BackgroundAttachmentType string

const (
	BackgroundAttachmentScroll BackgroundAttachmentType = "scroll" // default
	BackgroundAttachmentFixed  BackgroundAttachmentType = "fixed"
	BackgroundAttachmentLocal  BackgroundAttachmentType = "local"
)

// GetBackgroundAttachment returns the background-attachment value (default: scroll)
func (s *Style) GetBackgroundAttachment() BackgroundAttachmentType {
	if val, ok := s.Get("background-attachment"); ok {
		switch strings.TrimSpace(strings.ToLower(val)) {
		case "fixed":
			return BackgroundAttachmentFixed
		case "local":
			return BackgroundAttachmentLocal
		default:
			return BackgroundAttachmentScroll
		}
	}
	return BackgroundAttachmentScroll
}

// GetBackgroundRepeat returns the per-axis background-repeat value
// (default: repeat on both axes). Accepts the full CSS Backgrounds 3 §3.5
// shorthand: single keyword, two keywords (x then y), or "repeat-x" /
// "repeat-y".
func (s *Style) GetBackgroundRepeat() BackgroundRepeat {
	if val, ok := s.Get("background-repeat"); ok {
		return ExpandRepeatShorthand(val)
	}
	return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}
}

// BackgroundPosition represents background-position x,y values.
// Percentage values and keywords are stored as negative numbers (e.g., -100 = 100%).
// Pixel values are stored directly. Negative pixel offsets use XIsPixel/YIsPixel flags
// to distinguish from percentages.
// Use ResolveX/ResolveY to convert to pixels given container and image dimensions.
type BackgroundPosition struct {
	X        float64
	Y        float64
	XIsPixel bool // true when X is a pixel value (may be negative)
	YIsPixel bool // true when Y is a pixel value (may be negative)
	// XOffset/YOffset hold the pixel offset from a calc() expression
	// that combines a percentage and a pixel term, e.g. calc(100% - 278px).
	// The percentage part is stored in X/Y (as negative), and the pixel
	// offset is in XOffset/YOffset.
	XOffset float64
	YOffset float64
}

// ResolveX converts X to pixels: offset = (containerWidth - imageWidth) * percentage + pixelOffset
func (p BackgroundPosition) ResolveX(containerW, imageW float64) float64 {
	if p.XIsPixel {
		return p.X
	}
	if p.X < 0 {
		return (containerW-imageW)*(-p.X)/100 + p.XOffset
	}
	return p.X + p.XOffset
}

// ResolveY converts Y to pixels: offset = (containerHeight - imageHeight) * percentage + pixelOffset
func (p BackgroundPosition) ResolveY(containerH, imageH float64) float64 {
	if p.YIsPixel {
		return p.Y
	}
	if p.Y < 0 {
		return (containerH-imageH)*(-p.Y)/100 + p.YOffset
	}
	return p.Y + p.YOffset
}

// GetBackgroundPosition parses background-position (default: 0 0)
func (s *Style) GetBackgroundPosition() BackgroundPosition {
	val, ok := s.Get("background-position")
	if !ok {
		return BackgroundPosition{}
	}
	return parseBackgroundPosition(val, s.GetFontSize())
}

// ParseBackgroundPosition parses a background-position value string (uses default 16px font-size)
func ParseBackgroundPosition(val string) BackgroundPosition {
	return parseBackgroundPosition(val, 16.0)
}

// splitBackgroundPositionTokens splits a background-position value into
// tokens, respecting parenthesized expressions like calc(100% - 278px).
func splitBackgroundPositionTokens(val string) []string {
	val = strings.TrimSpace(val)
	var tokens []string
	i := 0
	for i < len(val) {
		// Skip whitespace.
		for i < len(val) && (val[i] == ' ' || val[i] == '\t') {
			i++
		}
		if i >= len(val) {
			break
		}
		start := i
		// If we're at a function call (e.g., calc(...)), consume the whole thing.
		if j := strings.IndexByte(val[i:], '('); j >= 0 && !strings.ContainsAny(val[i:i+j], " \t") {
			// Found opening paren without intervening space → function token.
			i += j + 1 // skip past '('
			depth := 1
			for i < len(val) && depth > 0 {
				if val[i] == '(' {
					depth++
				} else if val[i] == ')' {
					depth--
				}
				i++
			}
		} else {
			// Regular token: consume until whitespace.
			for i < len(val) && val[i] != ' ' && val[i] != '\t' {
				i++
			}
		}
		tokens = append(tokens, val[start:i])
	}
	return tokens
}

func parseBackgroundPosition(val string, fontSize float64) BackgroundPosition {
	parts := splitBackgroundPositionTokens(val)
	pos := BackgroundPosition{}
	if len(parts) == 1 {
		// Single keyword: CSS 2.1 §14.2.1 — vertical keywords set Y, horizontal set X
		switch parts[0] {
		case "top":
			pos.X = -50 // center
			pos.Y = 0   // top
		case "bottom":
			pos.X = -50  // center
			pos.Y = -100 // bottom
		case "left":
			pos.X = 0   // left
			pos.Y = -50 // center
		case "right":
			pos.X = -100 // right
			pos.Y = -50  // center
		case "center":
			pos.X = -50 // center
			pos.Y = -50 // center
		default:
			rx := parsePositionComponentFull(parts[0], fontSize)
			pos.X, pos.XIsPixel, pos.XOffset = rx.value, rx.isPixel, rx.offset
			pos.Y = -50 // center
		}
	} else if len(parts) >= 2 {
		rx := parsePositionComponentFull(parts[0], fontSize)
		ry := parsePositionComponentFull(parts[1], fontSize)
		pos.X, pos.XIsPixel, pos.XOffset = rx.value, rx.isPixel, rx.offset
		pos.Y, pos.YIsPixel, pos.YOffset = ry.value, ry.isPixel, ry.offset
	}
	return pos
}

// positionComponentResult holds the parsed result of a position component.
// For calc() expressions, it may have both a percentage and a pixel offset.
type positionComponentResult struct {
	value   float64 // percentage (negative) or pixel value
	isPixel bool
	offset  float64 // additional pixel offset from calc()
}

func parsePositionComponent(val string, fontSize float64) (float64, bool) {
	r := parsePositionComponentFull(val, fontSize)
	return r.value, r.isPixel
}

func parsePositionComponentFull(val string, fontSize float64) positionComponentResult {
	switch val {
	case "left", "top":
		return positionComponentResult{value: 0}
	case "right", "bottom":
		return positionComponentResult{value: -100} // 100% stored as negative
	case "center":
		return positionComponentResult{value: -50} // 50% stored as negative
	}
	// Handle calc() expressions.
	if strings.HasPrefix(val, "calc(") && strings.HasSuffix(val, ")") {
		inner := val[5 : len(val)-1]
		return parseCalcPositionComponent(inner, fontSize)
	}
	if strings.HasSuffix(val, "%") {
		if pct, err := strconv.ParseFloat(strings.TrimSuffix(val, "%"), 64); err == nil {
			return positionComponentResult{value: -pct} // Store percentage as negative
		}
	}
	if length, ok := ParseLengthWithFontSize(val, fontSize); ok {
		return positionComponentResult{value: length, isPixel: true}
	}
	return positionComponentResult{}
}

// parseCalcPositionComponent parses a calc() inner expression that may combine
// a percentage and pixel terms, e.g. "100% - 278px".
func parseCalcPositionComponent(expr string, fontSize float64) positionComponentResult {
	expr = strings.TrimSpace(expr)
	// Split into terms separated by + or - (respecting parentheses).
	var pctSum float64
	var pxSum float64
	hasPct := false

	// Simple tokenizer: split on + and - at the top level.
	// We handle expressions like "100% - 278px" and "100% + 8px".
	sign := 1.0
	i := 0
	for i < len(expr) {
		// Skip whitespace.
		for i < len(expr) && (expr[i] == ' ' || expr[i] == '\t') {
			i++
		}
		if i >= len(expr) {
			break
		}
		// Check for sign.
		if expr[i] == '+' {
			sign = 1.0
			i++
			continue
		}
		if expr[i] == '-' {
			sign = -1.0
			i++
			continue
		}
		// Consume a term.
		start := i
		for i < len(expr) && expr[i] != ' ' && expr[i] != '\t' && expr[i] != '+' && expr[i] != '-' {
			i++
		}
		term := expr[start:i]
		if strings.HasSuffix(term, "%") {
			if pct, err := strconv.ParseFloat(strings.TrimSuffix(term, "%"), 64); err == nil {
				pctSum += sign * pct
				hasPct = true
			}
		} else if length, ok := ParseLengthWithFontSize(term, fontSize); ok {
			pxSum += sign * length
		}
		sign = 1.0 // reset sign for next term
	}

	if hasPct {
		return positionComponentResult{value: -pctSum, offset: pxSum}
	}
	return positionComponentResult{value: pxSum, isPixel: true}
}

// BackgroundSize represents a parsed background-size value
type BackgroundSize struct {
	Width   float64 // Computed width in pixels (0 = auto)
	Height  float64 // Computed height in pixels (0 = auto)
	Cover   bool
	Contain bool
}

// GetBackgroundSize parses the background-size property.
// Returns the parsed size with cover/contain flags or explicit dimensions.
func (s *Style) GetBackgroundSize() BackgroundSize {
	val, ok := s.Get("background-size")
	if !ok {
		return BackgroundSize{} // auto auto
	}
	val = strings.TrimSpace(val)
	switch val {
	case "cover":
		return BackgroundSize{Cover: true}
	case "contain":
		return BackgroundSize{Contain: true}
	case "auto":
		return BackgroundSize{}
	}

	parts := strings.Fields(val)
	var size BackgroundSize
	if len(parts) >= 1 && parts[0] != "auto" {
		if w, ok := ParseLength(parts[0]); ok {
			size.Width = w
		} else if pct, ok := ParsePercentage(parts[0]); ok {
			// Store as negative to signal percentage (resolved at render time)
			size.Width = -pct
		}
	}
	if len(parts) >= 2 && parts[1] != "auto" {
		if h, ok := ParseLength(parts[1]); ok {
			size.Height = h
		} else if pct, ok := ParsePercentage(parts[1]); ok {
			size.Height = -pct
		}
	}
	return size
}

// BackgroundClipType represents the background-clip property value
type BackgroundClipType string

const (
	BackgroundClipBorderBox  BackgroundClipType = "border-box"
	BackgroundClipPaddingBox BackgroundClipType = "padding-box"
	BackgroundClipContentBox BackgroundClipType = "content-box"
)

// GetBackgroundClip returns the background-clip value (default: border-box)
func (s *Style) GetBackgroundClip() BackgroundClipType {
	if val, ok := s.Get("background-clip"); ok {
		switch strings.TrimSpace(val) {
		case "padding-box":
			return BackgroundClipPaddingBox
		case "content-box":
			return BackgroundClipContentBox
		case "border-box":
			return BackgroundClipBorderBox
		}
	}
	return BackgroundClipBorderBox
}

// BackgroundOriginType represents the background-origin property value.
type BackgroundOriginType string

const (
	BackgroundOriginBorderBox  BackgroundOriginType = "border-box"
	BackgroundOriginPaddingBox BackgroundOriginType = "padding-box" // default
	BackgroundOriginContentBox BackgroundOriginType = "content-box"
)

// GetBackgroundOrigin returns the background-origin value (default: padding-box).
func (s *Style) GetBackgroundOrigin() BackgroundOriginType {
	if val, ok := s.Get("background-origin"); ok {
		switch strings.TrimSpace(strings.ToLower(val)) {
		case "border-box":
			return BackgroundOriginBorderBox
		case "content-box":
			return BackgroundOriginContentBox
		default:
			return BackgroundOriginPaddingBox
		}
	}
	return BackgroundOriginPaddingBox
}

// GetBackgroundColorClip returns the background-clip value for background-color.
// Per CSS spec, this is the clip of the bottom-most image layer, cycling clip values.
func (s *Style) GetBackgroundColorClip() BackgroundClipType {
	imgVal, _ := s.Get("background-image")
	clipVal, clipOK := s.Get("background-clip")
	if !clipOK {
		return BackgroundClipBorderBox
	}

	clipParts := splitCommaSeparated(clipVal)
	if len(clipParts) == 0 {
		return BackgroundClipBorderBox
	}

	// Determine layer count from background-image (or 1 if absent).
	layerCount := 1
	if imgVal != "" && imgVal != "none" {
		layerCount = len(splitCommaSeparated(imgVal))
	}

	// The bottom layer's clip is at index (layerCount-1) % len(clipParts).
	idx := (layerCount - 1) % len(clipParts)
	if c, ok := parseClipValue(clipParts[idx]); ok {
		return c
	}
	return BackgroundClipBorderBox
}

// splitCommaSeparated splits a CSS value by commas, respecting parentheses depth.
func splitCommaSeparated(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

// parseRepeatValue parses a single per-layer background-repeat value.
// The value may be one or two axis keywords (e.g. "repeat", "space round"),
// or the "repeat-x" / "repeat-y" shorthand. Returns the expanded per-axis
// pair and whether the value was a valid background-repeat token.
func parseRepeatValue(val string) (BackgroundRepeat, bool) {
	val = strings.TrimSpace(val)
	if val == "" {
		return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}, false
	}
	parts := strings.Fields(val)
	if len(parts) == 0 {
		return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}, false
	}
	// Validate every token before accepting (so unknown tokens don't
	// silently fall through to "repeat repeat").
	if len(parts) == 1 {
		switch strings.ToLower(parts[0]) {
		case "repeat-x", "repeat-y":
			return ExpandRepeatShorthand(val), true
		}
		if _, ok := parseAxisRepeatKeyword(parts[0]); !ok {
			return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}, false
		}
		return ExpandRepeatShorthand(val), true
	}
	// len(parts) >= 2: both tokens must be axis keywords (not the
	// repeat-x / repeat-y shorthands, which are single-token only).
	for _, p := range parts[:2] {
		if _, ok := parseAxisRepeatKeyword(p); !ok {
			return BackgroundRepeat{X: BackgroundRepeatRepeat, Y: BackgroundRepeatRepeat}, false
		}
	}
	return ExpandRepeatShorthand(val), true
}

// parseSizeValue parses a single background-size value.
func parseSizeValue(val string) (BackgroundSize, bool) {
	val = strings.TrimSpace(val)
	switch val {
	case "cover":
		return BackgroundSize{Cover: true}, true
	case "contain":
		return BackgroundSize{Contain: true}, true
	case "auto", "":
		return BackgroundSize{}, false
	}
	parts := strings.Fields(val)
	var size BackgroundSize
	set := false
	if len(parts) >= 1 && parts[0] != "auto" {
		if w, ok := ParseLength(parts[0]); ok {
			size.Width = w
			set = true
		} else if pct, ok := ParsePercentage(parts[0]); ok {
			size.Width = -pct
			set = true
		}
	}
	if len(parts) >= 2 && parts[1] != "auto" {
		if h, ok := ParseLength(parts[1]); ok {
			size.Height = h
			set = true
		} else if pct, ok := ParsePercentage(parts[1]); ok {
			size.Height = -pct
			set = true
		}
	}
	return size, set
}

// parseClipValue parses a single background-clip value.
func parseClipValue(val string) (BackgroundClipType, bool) {
	switch strings.TrimSpace(val) {
	case "padding-box":
		return BackgroundClipPaddingBox, true
	case "content-box":
		return BackgroundClipContentBox, true
	case "border-box":
		return BackgroundClipBorderBox, true
	}
	return BackgroundClipBorderBox, false
}

// parseBgOriginValue parses a single background-origin value.
func parseBgOriginValue(val string) (BackgroundOriginType, bool) {
	switch strings.TrimSpace(strings.ToLower(val)) {
	case "border-box":
		return BackgroundOriginBorderBox, true
	case "content-box":
		return BackgroundOriginContentBox, true
	case "padding-box":
		return BackgroundOriginPaddingBox, true
	}
	return BackgroundOriginPaddingBox, false
}

// GetBackgroundLayers builds a FillLayer linked list from the style's background
// longhand properties. Layer count is determined by background-image (the longest
// longhand). Other longhands cycle. Mirrors Blink's FillLayer construction.
func (s *Style) GetBackgroundLayers() *FillLayer {
	imgVal, _ := s.Get("background-image")
	if imgVal == "" || imgVal == "none" {
		return nil
	}

	// Split background-image by commas (depth-aware) to get layer count.
	imgParts := splitCommaSeparated(imgVal)
	if len(imgParts) == 0 {
		return nil
	}

	// Split other longhands.
	var repeatParts, posParts, sizeParts, clipParts, originParts, attachParts []string
	if v, ok := s.Get("background-repeat"); ok {
		repeatParts = splitCommaSeparated(v)
	}
	if v, ok := s.Get("background-position"); ok {
		posParts = splitCommaSeparated(v)
	}
	if v, ok := s.Get("background-size"); ok {
		sizeParts = splitCommaSeparated(v)
	}
	if v, ok := s.Get("background-clip"); ok {
		clipParts = splitCommaSeparated(v)
	}
	if v, ok := s.Get("background-origin"); ok {
		originParts = splitCommaSeparated(v)
	}
	if v, ok := s.Get("background-attachment"); ok {
		attachParts = splitCommaSeparated(v)
	}

	fontSize := s.GetFontSize()

	// Build linked list head→tail (head = topmost CSS layer = first in declaration).
	var head, prev *FillLayer
	for i, imgStr := range imgParts {
		layer := &FillLayer{}

		// Image: check for url() or gradient
		imgStr = strings.TrimSpace(imgStr)
		if imgStr != "none" && imgStr != "" {
			if isGradient(imgStr) {
				layer.Gradient = imgStr
				layer.ImageSet = true
			} else if url, ok := ParseURLValue(imgStr); ok {
				// ctx.RewriteURLs has already resolved this URL against the
				// stylesheet's BaseDir; both URLData fields hold the
				// absolute form. CSSOM serialization will see absolute
				// until Phase 7+ threads ctx into FillLayer parsing.
				layer.Image = &CSSImageValue{Data: URLData{Relative: url, Absolute: url}}
				layer.ImageSet = true
			} else if strings.Contains(strings.ToLower(imgStr), "image-set(") {
				if url, ok := extractImageSetFirstURL(imgStr); ok {
					layer.Image = &CSSImageValue{Data: URLData{Relative: url, Absolute: url}}
					layer.ImageSet = true
				}
			}
		}

		// Repeat
		if i < len(repeatParts) {
			if r, ok := parseRepeatValue(repeatParts[i]); ok {
				layer.Repeat = r
				layer.RepeatSet = true
			}
		}

		// Position
		if i < len(posParts) {
			layer.Position = parseBackgroundPosition(posParts[i], fontSize)
			layer.PositionSet = true
		}

		// Size
		if i < len(sizeParts) {
			if sz, ok := parseSizeValue(sizeParts[i]); ok {
				layer.Size = sz
				layer.SizeSet = true
			}
		}

		// Clip
		if i < len(clipParts) {
			if c, ok := parseClipValue(clipParts[i]); ok {
				layer.Clip = c
				layer.ClipSet = true
			}
		}

		// Origin
		if i < len(originParts) {
			if o, ok := parseBgOriginValue(originParts[i]); ok {
				layer.Origin = o
				layer.OriginSet = true
			}
		}

		// Attachment
		if i < len(attachParts) {
			switch strings.TrimSpace(strings.ToLower(attachParts[i])) {
			case "fixed":
				layer.Attachment = BackgroundAttachmentFixed
				layer.AttachmentSet = true
			case "local":
				layer.Attachment = BackgroundAttachmentLocal
				layer.AttachmentSet = true
			case "scroll":
				layer.Attachment = BackgroundAttachmentScroll
				layer.AttachmentSet = true
			}
		}

		if head == nil {
			head = layer
		} else {
			prev.Next = layer
		}
		prev = layer
	}

	if head != nil {
		head.FillUnsetProperties()
		head = head.CullEmptyLayers()
	}

	return head
}

// isGradient returns true if the value looks like a CSS gradient function.
func isGradient(val string) bool {
	lower := strings.ToLower(strings.TrimSpace(val))
	for _, prefix := range []string{"linear-gradient(", "radial-gradient(", "repeating-linear-gradient(", "repeating-radial-gradient(", "conic-gradient(", "repeating-conic-gradient("} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// Phase 23: List styling

// ListStyleType represents the list-style-type property value
type ListStyleType string

const (
	ListStyleTypeDisc               ListStyleType = "disc"
	ListStyleTypeCircle             ListStyleType = "circle"
	ListStyleTypeSquare             ListStyleType = "square"
	ListStyleTypeDecimal            ListStyleType = "decimal"
	ListStyleTypeNone               ListStyleType = "none"
	ListStyleTypeDecimalLeadingZero ListStyleType = "decimal-leading-zero"
	ListStyleTypeLowerAlpha         ListStyleType = "lower-alpha"
	ListStyleTypeUpperAlpha         ListStyleType = "upper-alpha"
	ListStyleTypeLowerLatin         ListStyleType = "lower-latin"
	ListStyleTypeUpperLatin         ListStyleType = "upper-latin"
	ListStyleTypeLowerRoman         ListStyleType = "lower-roman"
	ListStyleTypeUpperRoman         ListStyleType = "upper-roman"
	ListStyleTypeLowerGreek         ListStyleType = "lower-greek"
	ListStyleTypeDisclosureOpen     ListStyleType = "disclosure-open"
	ListStyleTypeDisclosureClosed   ListStyleType = "disclosure-closed"
)

// GetListStyleType returns the list-style-type value (default: disc)
func (s *Style) GetListStyleType() ListStyleType {
	if val, ok := s.Get("list-style-type"); ok {
		switch val {
		case "disc":
			return ListStyleTypeDisc
		case "circle":
			return ListStyleTypeCircle
		case "square":
			return ListStyleTypeSquare
		case "decimal":
			return ListStyleTypeDecimal
		case "none":
			return ListStyleTypeNone
		case "decimal-leading-zero":
			return ListStyleTypeDecimalLeadingZero
		case "lower-alpha", "lower-latin":
			return ListStyleTypeLowerAlpha
		case "upper-alpha", "upper-latin":
			return ListStyleTypeUpperAlpha
		case "lower-roman":
			return ListStyleTypeLowerRoman
		case "upper-roman":
			return ListStyleTypeUpperRoman
		case "lower-greek":
			return ListStyleTypeLowerGreek
		case "disclosure-open":
			return ListStyleTypeDisclosureOpen
		case "disclosure-closed":
			return ListStyleTypeDisclosureClosed
		default:
			// Handle custom string values (quoted strings like "\2022")
			// Strip quotes if present
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'')) {
				return ListStyleType(val[1 : len(val)-1])
			}
			// Return as-is for other values
			return ListStyleType(val)
		}
	}
	return ListStyleTypeDisc
}

// GetListStylePosition returns the list-style-position value.
// Returns "outside" (default) or "inside".
func (s *Style) GetListStylePosition() string {
	if val, ok := s.Get("list-style-position"); ok {
		if val == "inside" {
			return "inside"
		}
	}
	return "outside"
}

// Phase 25: clip-path, filter, mix-blend-mode, mask-image

// ClipPathType represents the type of clip-path shape
type ClipPathType string

const (
	ClipPathNone    ClipPathType = "none"
	ClipPathCircle  ClipPathType = "circle"
	ClipPathEllipse ClipPathType = "ellipse"
	ClipPathPolygon ClipPathType = "polygon"
	// ClipPathReference is the `url(#id)` variant. Carries a
	// document-fragment id resolved at paint time against the SVG
	// resource registry. Mirrors Blink's ReferenceClipPathOperation
	// (core/style/clip_path_operation.h).
	ClipPathReference ClipPathType = "url"
)

// ClipPath represents a parsed clip-path value.
// Pixel values are stored directly. Percentage values (0-100) are stored
// in the Pct variants and the corresponding px field is set to -1.
type ClipPath struct {
	Type   ClipPathType
	Radius float64   // circle radius in px (-1 = default closest-side or use RadiusPct)
	Rx, Ry float64   // ellipse radii in px (-1 = default or use RxPct/RyPct)
	Cx, Cy float64   // center position in px (-1 = default center or use CxPct/CyPct)
	Points []float64 // polygon points [x1, y1, x2, y2, ...] in px or pct (see PointsPct)

	// Percentage flags — when true, the corresponding value is a percentage (0-100).
	RadiusPct float64 // circle radius as percentage (-1 = not set)
	RxPct     float64 // ellipse rx as percentage (-1 = not set)
	RyPct     float64 // ellipse ry as percentage (-1 = not set)
	CxPct     float64 // center x as percentage (-1 = not set)
	CyPct     float64 // center y as percentage (-1 = not set)
	PointsPct []bool  // per-coordinate: true = percentage, false = px

	// ReferenceID is the fragment id of a `url(#id)` clip-path
	// reference (without the leading `#`). Populated only when Type
	// is ClipPathReference. Resolved by the SVG resource registry at
	// paint time. Mirrors Blink's ReferenceClipPathOperation::url().
	ReferenceID string
}

// GetClipPath parses the clip-path property
func (s *Style) GetClipPath() *ClipPath {
	val, ok := s.Get("clip-path")
	if !ok || val == "none" {
		return nil
	}
	val = strings.TrimSpace(val)

	if strings.HasPrefix(val, "circle(") {
		return parseClipPathCircle(val)
	}
	if strings.HasPrefix(val, "ellipse(") {
		return parseClipPathEllipse(val)
	}
	if strings.HasPrefix(val, "polygon(") {
		return parseClipPathPolygon(val)
	}
	if strings.HasPrefix(val, "url(") {
		return parseClipPathReference(val)
	}
	return nil
}

// parseClipPathReference parses the `url(#id)` clip-path variant.
// Returns a ClipPath with Type=ClipPathReference and the fragment id
// stripped of the leading `#`. Non-fragment URLs (`url(file.svg#id)`)
// are accepted but only the fragment portion is kept — louis14's
// document fragment resolution is in-document only (same as Blink's
// LookupSVGElementById which scans the current TreeScope).
//
// Returns nil when the value isn't a parseable url() or carries no id.
func parseClipPathReference(val string) *ClipPath {
	if !strings.HasPrefix(val, "url(") || !strings.HasSuffix(val, ")") {
		return nil
	}
	inner := val[4 : len(val)-1]
	inner = strings.TrimSpace(inner)
	// Strip surrounding quotes (the CSS grammar allows both forms).
	inner = strings.Trim(inner, `"'`)
	if inner == "" {
		return nil
	}
	// Extract the fragment after the `#`. If absent, treat the whole
	// value as the id — being lenient covers `url(myMask)` which
	// authors sometimes write (Blink ignores it as a malformed URL,
	// but accepting it matches what the existing mask-image parser
	// implicitly does for the same syntax).
	id := inner
	if i := strings.Index(inner, "#"); i >= 0 {
		id = inner[i+1:]
	}
	if id == "" {
		return nil
	}
	return &ClipPath{
		Type:        ClipPathReference,
		ReferenceID: id,
		RadiusPct:   -1, RxPct: -1, RyPct: -1, CxPct: -1, CyPct: -1,
	}
}

// ClipRect represents a parsed CSS clip: rect(top, right, bottom, left) value.
// All values are in pixels, relative to the element's border box top-left corner.
// The clip property is purely physical (CSS Writing Modes §7.6).
type ClipRect struct {
	Top    float64
	Right  float64
	Bottom float64
	Left   float64
}

// GetClipRect parses the legacy CSS clip: rect(top, right, bottom, left) property.
// Returns nil if clip is not set or is "auto".
func (s *Style) GetClipRect() *ClipRect {
	val, ok := s.Get("clip")
	if !ok || val == "auto" || val == "none" {
		return nil
	}
	val = strings.TrimSpace(val)
	if !strings.HasPrefix(val, "rect(") || !strings.HasSuffix(val, ")") {
		return nil
	}
	inner := val[5 : len(val)-1]
	// rect() values can be separated by commas or spaces
	inner = strings.ReplaceAll(inner, ",", " ")
	parts := strings.Fields(inner)
	if len(parts) != 4 {
		return nil
	}
	cr := &ClipRect{}
	if parts[0] == "auto" {
		cr.Top = 0
	} else if v, ok := ParseLength(parts[0]); ok {
		cr.Top = v
	} else {
		return nil
	}
	if parts[1] == "auto" {
		cr.Right = -1 // sentinel: use element width
	} else if v, ok := ParseLength(parts[1]); ok {
		cr.Right = v
	} else {
		return nil
	}
	if parts[2] == "auto" {
		cr.Bottom = -1 // sentinel: use element height
	} else if v, ok := ParseLength(parts[2]); ok {
		cr.Bottom = v
	} else {
		return nil
	}
	if parts[3] == "auto" {
		cr.Left = 0
	} else if v, ok := ParseLength(parts[3]); ok {
		cr.Left = v
	} else {
		return nil
	}
	return cr
}

// parseClipPathValue parses a length or percentage for clip-path.
// Returns (value, isPercent, ok). Percentages are returned as 0-100.
func parseClipPathValue(s string) (float64, bool, bool) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		numStr := strings.TrimSuffix(s, "%")
		if v, err := strconv.ParseFloat(numStr, 64); err == nil {
			return v, true, true
		}
		return 0, false, false
	}
	if v, ok := ParseLength(s); ok {
		return v, false, true
	}
	return 0, false, false
}

func parseClipPathCircle(val string) *ClipPath {
	// circle() or circle(radius) or circle(radius at cx cy)
	inner := extractParens(val, "circle")
	cp := &ClipPath{Type: ClipPathCircle, Radius: -1, Cx: -1, Cy: -1, RadiusPct: -1, RxPct: -1, RyPct: -1, CxPct: -1, CyPct: -1}
	if inner == "" {
		return cp // defaults
	}
	parts := strings.SplitN(inner, " at ", 2)
	radiusPart := strings.TrimSpace(parts[0])
	if radiusPart != "" && radiusPart != "closest-side" && radiusPart != "farthest-side" {
		if v, isPct, ok := parseClipPathValue(radiusPart); ok {
			if isPct {
				cp.RadiusPct = v
			} else {
				cp.Radius = v
			}
		}
	}
	if len(parts) == 2 {
		parseClipPathPosition(strings.TrimSpace(parts[1]), cp)
	}
	return cp
}

func parseClipPathEllipse(val string) *ClipPath {
	// ellipse(rx ry at cx cy)
	inner := extractParens(val, "ellipse")
	cp := &ClipPath{Type: ClipPathEllipse, Rx: -1, Ry: -1, Cx: -1, Cy: -1, RadiusPct: -1, RxPct: -1, RyPct: -1, CxPct: -1, CyPct: -1}
	if inner == "" {
		return cp
	}
	parts := strings.SplitN(inner, " at ", 2)
	radii := strings.Fields(strings.TrimSpace(parts[0]))
	if len(radii) >= 2 {
		if v, isPct, ok := parseClipPathValue(radii[0]); ok {
			if isPct {
				cp.RxPct = v
			} else {
				cp.Rx = v
			}
		}
		if v, isPct, ok := parseClipPathValue(radii[1]); ok {
			if isPct {
				cp.RyPct = v
			} else {
				cp.Ry = v
			}
		}
	}
	if len(parts) == 2 {
		parseClipPathPosition(strings.TrimSpace(parts[1]), cp)
	}
	return cp
}

func parseClipPathPolygon(val string) *ClipPath {
	// polygon(x1 y1, x2 y2, ...)
	inner := extractParens(val, "polygon")
	cp := &ClipPath{Type: ClipPathPolygon, RadiusPct: -1, RxPct: -1, RyPct: -1, CxPct: -1, CyPct: -1}
	if inner == "" {
		return cp
	}
	// Skip optional fill-rule (nonzero, evenodd)
	if strings.HasPrefix(inner, "nonzero,") || strings.HasPrefix(inner, "evenodd,") {
		inner = inner[strings.Index(inner, ",")+1:]
	}
	pairs := strings.Split(inner, ",")
	for _, pair := range pairs {
		coords := strings.Fields(strings.TrimSpace(pair))
		if len(coords) >= 2 {
			xVal, xPct, xOk := parseClipPathValue(coords[0])
			yVal, yPct, yOk := parseClipPathValue(coords[1])
			if xOk && yOk {
				cp.Points = append(cp.Points, xVal, yVal)
				cp.PointsPct = append(cp.PointsPct, xPct, yPct)
			}
		}
	}
	return cp
}

// extractParens extracts the content between parentheses for a function like "circle(...)"
func extractParens(val, funcName string) string {
	start := len(funcName) + 1 // skip "funcName("
	end := strings.LastIndex(val, ")")
	if end <= start {
		return ""
	}
	return strings.TrimSpace(val[start:end])
}

// parsePosition parses "cx cy" values, setting -1 for "center" keyword
func parsePosition(pos string, cx, cy *float64) {
	fields := strings.Fields(pos)
	if len(fields) >= 1 {
		if fields[0] == "center" {
			*cx = -1
		} else if v, ok := ParseLength(fields[0]); ok {
			*cx = v
		}
	}
	if len(fields) >= 2 {
		if fields[1] == "center" {
			*cy = -1
		} else if v, ok := ParseLength(fields[1]); ok {
			*cy = v
		}
	}
}

// parseClipPathPosition parses "cx cy" for clip-path, supporting percentages.
func parseClipPathPosition(pos string, cp *ClipPath) {
	fields := strings.Fields(pos)
	if len(fields) >= 1 {
		if fields[0] == "center" {
			cp.Cx = -1
		} else if v, isPct, ok := parseClipPathValue(fields[0]); ok {
			if isPct {
				cp.CxPct = v
				cp.Cx = -1 // signal to use CxPct
			} else {
				cp.Cx = v
			}
		}
	}
	if len(fields) >= 2 {
		if fields[1] == "center" {
			cp.Cy = -1
		} else if v, isPct, ok := parseClipPathValue(fields[1]); ok {
			if isPct {
				cp.CyPct = v
				cp.Cy = -1 // signal to use CyPct
			} else {
				cp.Cy = v
			}
		}
	}
}

// ResolveClipPath resolves a ClipPath's default values and percentages
// against a box's border-box dimensions. Returns absolute pixel coordinates.
func (cp *ClipPath) ResolveClipPath(boxWidth, boxHeight float64) *ClipPath {
	resolved := *cp
	// Copy slices so we don't mutate the original.
	if len(cp.Points) > 0 {
		resolved.Points = make([]float64, len(cp.Points))
		copy(resolved.Points, cp.Points)
	}

	// Resolve center position.
	if resolved.CxPct >= 0 {
		resolved.Cx = resolved.CxPct / 100 * boxWidth
	} else if resolved.Cx < 0 {
		resolved.Cx = boxWidth / 2
	}
	if resolved.CyPct >= 0 {
		resolved.Cy = resolved.CyPct / 100 * boxHeight
	} else if resolved.Cy < 0 {
		resolved.Cy = boxHeight / 2
	}

	switch cp.Type {
	case ClipPathCircle:
		if resolved.RadiusPct >= 0 {
			// Per spec, percentage radius for circle() is relative to
			// sqrt(width^2 + height^2) / sqrt(2).
			resolved.Radius = resolved.RadiusPct / 100 * math.Sqrt(boxWidth*boxWidth+boxHeight*boxHeight) / math.Sqrt(2)
		} else if resolved.Radius < 0 {
			// closest-side: distance from center to closest edge
			resolved.Radius = math.Min(
				math.Min(resolved.Cx, boxWidth-resolved.Cx),
				math.Min(resolved.Cy, boxHeight-resolved.Cy),
			)
		}
	case ClipPathEllipse:
		if resolved.RxPct >= 0 {
			resolved.Rx = resolved.RxPct / 100 * boxWidth
		} else if resolved.Rx < 0 {
			resolved.Rx = boxWidth / 2
		}
		if resolved.RyPct >= 0 {
			resolved.Ry = resolved.RyPct / 100 * boxHeight
		} else if resolved.Ry < 0 {
			resolved.Ry = boxHeight / 2
		}
	case ClipPathPolygon:
		for i := 0; i < len(resolved.Points)-1; i += 2 {
			if i < len(resolved.PointsPct) && resolved.PointsPct[i] {
				resolved.Points[i] = resolved.Points[i] / 100 * boxWidth
			}
			if i+1 < len(resolved.PointsPct) && resolved.PointsPct[i+1] {
				resolved.Points[i+1] = resolved.Points[i+1] / 100 * boxHeight
			}
		}
	}
	return &resolved
}

// FilterFunction represents a single CSS filter function
type FilterFunction struct {
	Name  string  // "opacity", "contrast", "grayscale", "blur", "drop-shadow", "url", etc.
	Value float64 // The function argument (0-1 for opacity, 0-N for contrast, etc.)

	// drop-shadow specific fields.
	ShadowOffsetX float64
	ShadowOffsetY float64
	ShadowBlur    float64
	ShadowColor   Color
	// ShadowUseCurrentColor is true when the drop-shadow color is omitted or
	// the literal "currentcolor"; the painter resolves it from the element's
	// computed color. Mirrors CSS Filter Effects: drop-shadow's color
	// defaults to currentColor, not black.
	ShadowUseCurrentColor bool

	// URL is set for a url(#id) reference filter (Name == "url").
	// URL.Absolute is the parse-time-resolved form consumers should fetch;
	// URL.Relative preserves the original `url()` inner value.
	URL URLData
}

// GetFilter parses the filter property and returns filter functions. The
// stored value has already been resolved at CSS-parse time by
// ParserContext.RewriteURLs (LOU-138 phase 2), so parseFilterList here
// is purely tokenization — no URL resolution.
func (s *Style) GetFilter() []FilterFunction {
	val, ok := s.Get("filter")
	if !ok || val == "none" {
		return nil
	}
	return parseFilterList(val)
}

// parseFilterList parses a CSS <filter-value-list> — a whitespace-separated
// list of filter functions and/or url() references. Shared by GetFilter and
// GetBackdropFilter. url() inners come in pre-resolved (the stylesheet's
// ParserContext.RewriteURLs ran at CSS-parse time) so both URLData fields
// hold the absolute form. Matches Blink's `CSSUrlData` parse-time
// resolution (`core/css/css_url_data.cc:82-95` @ chromium-main
// bf955d02bf0b0c67868b2e62359c0af199af9acc).
func parseFilterList(val string) []FilterFunction {
	var filters []FilterFunction
	val = strings.TrimSpace(val)
	for len(val) > 0 {
		val = strings.TrimSpace(val)
		parenIdx := strings.Index(val, "(")
		if parenIdx < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(val[:parenIdx]))
		// Find matching close paren (handles nested parens like rgb() in drop-shadow).
		closeIdx := findMatchingParen(val[parenIdx:])
		if closeIdx < 0 {
			break
		}
		arg := strings.TrimSpace(val[parenIdx+1 : parenIdx+closeIdx])
		switch name {
		case "url":
			u := strings.TrimSpace(arg)
			u = strings.Trim(u, "\"'")
			filters = append(filters, FilterFunction{
				Name: "url",
				URL:  URLData{Relative: u, Absolute: u},
			})
		case "drop-shadow":
			filters = append(filters, parseDropShadowFunction(arg))
		case "blur":
			// blur() takes an optional <length>; default 0.
			filters = append(filters, FilterFunction{Name: name, Value: parseLengthValue(arg)})
		case "hue-rotate":
			// hue-rotate takes an optional <angle>; default 0.
			ff := FilterFunction{Name: name}
			if a := parseAngle(arg); a != nil {
				ff.Value = *a
			}
			filters = append(filters, ff)
		default:
			// grayscale/sepia/saturate/invert/opacity/brightness/contrast:
			// optional <number> or <percentage>. Per Filter Effects 1 the
			// argument may be omitted: brightness/contrast/opacity/saturate
			// default to 1, grayscale/sepia/invert also default to 1.
			value := 1.0
			if arg != "" {
				if pct, ok := ParsePercentage(arg); ok {
					value = pct / 100.0
				} else if f, err := strconv.ParseFloat(strings.TrimSpace(arg), 64); err == nil {
					value = f
				}
			}
			filters = append(filters, FilterFunction{Name: name, Value: value})
		}
		val = val[parenIdx+closeIdx+1:]
	}
	return filters
}

// parseDropShadowFunction parses a drop-shadow() argument:
// [<color>?] <offset-x> <offset-y> <blur-radius>? [<color>?]. Per spec the
// color defaults to currentColor.
func parseDropShadowFunction(arg string) FilterFunction {
	ff := FilterFunction{Name: "drop-shadow", ShadowUseCurrentColor: true}
	parts := splitFilterArgs(arg)
	// A leading or trailing token may be a color. Identify color tokens by
	// attempting to parse them; lengths fail ParseColor.
	isColorTok := func(tok string) bool {
		t := strings.TrimSpace(tok)
		if strings.EqualFold(t, "currentcolor") {
			return true
		}
		_, ok := ParseColor(t)
		return ok
	}
	// Pull off a leading color.
	if len(parts) > 0 && isColorTok(parts[0]) {
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "currentcolor") {
			if c, ok := ParseColor(strings.TrimSpace(parts[0])); ok {
				ff.ShadowColor = c
				ff.ShadowUseCurrentColor = false
			}
		}
		parts = parts[1:]
	}
	// Pull off a trailing color. rgb()/rgba()/hsl() come through splitFilterArgs
	// as a single token, so a trailing color is the last token only.
	if len(parts) > 2 && isColorTok(parts[len(parts)-1]) {
		last := strings.TrimSpace(parts[len(parts)-1])
		if !strings.EqualFold(last, "currentcolor") {
			if c, ok := ParseColor(last); ok {
				ff.ShadowColor = c
				ff.ShadowUseCurrentColor = false
			}
		}
		parts = parts[:len(parts)-1]
	}
	// Remaining: offset-x offset-y [blur].
	if len(parts) >= 2 {
		ff.ShadowOffsetX = parseLengthValue(parts[0])
		ff.ShadowOffsetY = parseLengthValue(parts[1])
	}
	if len(parts) >= 3 {
		ff.ShadowBlur = parseLengthValue(parts[2])
	}
	return ff
}

// findMatchingParen finds the matching closing paren for an opening paren at s[0].
// Returns the index of the matching ')' relative to s, or -1 if not found.
func findMatchingParen(s string) int {
	depth := 0
	for i, ch := range s {
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitFilterArgs splits a drop-shadow argument string into parts,
// handling rgb()/rgba() color function arguments.
func splitFilterArgs(s string) []string {
	var parts []string
	s = strings.TrimSpace(s)
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ' ', '\t':
			if depth == 0 && i > start {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			} else if depth == 0 {
				start = i + 1
			}
		case ',':
			// Inside rgb(), commas are part of the color
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if start < len(s) {
		parts = append(parts, strings.TrimSpace(s[start:]))
	}
	return parts
}

// MixBlendMode represents the mix-blend-mode property value
type MixBlendMode string

const (
	MixBlendModeNormal     MixBlendMode = "normal"
	MixBlendModeDifference MixBlendMode = "difference"
	MixBlendModeMultiply   MixBlendMode = "multiply"
	MixBlendModeScreen     MixBlendMode = "screen"
	MixBlendModeOverlay    MixBlendMode = "overlay"
	MixBlendModeDarken     MixBlendMode = "darken"
	MixBlendModeLighten    MixBlendMode = "lighten"
)

// GetMixBlendMode returns the mix-blend-mode value (default: normal)
func (s *Style) GetMixBlendMode() MixBlendMode {
	if val, ok := s.Get("mix-blend-mode"); ok {
		switch val {
		case "difference":
			return MixBlendModeDifference
		case "multiply":
			return MixBlendModeMultiply
		case "screen":
			return MixBlendModeScreen
		case "overlay":
			return MixBlendModeOverlay
		case "darken":
			return MixBlendModeDarken
		case "lighten":
			return MixBlendModeLighten
		}
	}
	return MixBlendModeNormal
}

// TextOverflowType represents the text-overflow CSS property
type TextOverflowType int

const (
	TextOverflowClip TextOverflowType = iota
	TextOverflowEllipsis
)

// GetTextOverflow returns the text-overflow value (default: clip)
func (s *Style) GetTextOverflow() TextOverflowType {
	if val, ok := s.Get("text-overflow"); ok {
		if val == "ellipsis" {
			return TextOverflowEllipsis
		}
	}
	return TextOverflowClip
}

// GetWordBreak returns the word-break value (default: "normal")
func (s *Style) GetWordBreak() string {
	if val, ok := s.Get("word-break"); ok {
		return val
	}
	return "normal"
}

// GetOverflowWrap returns the overflow-wrap value (default: "normal").
// Also checks the legacy word-wrap alias.
func (s *Style) GetOverflowWrap() string {
	if val, ok := s.Get("overflow-wrap"); ok {
		return val
	}
	if val, ok := s.Get("word-wrap"); ok {
		return val
	}
	return "normal"
}

// BoxDecorationBreak represents the box-decoration-break CSS property.
// slice (default): decorations are treated as belonging to the whole element —
//
//	block-start appears only on the first fragment, block-end only on the last.
//
// clone: each fragment is an independent decorated box — all four borders
//
//	appear on every fragment, backgrounds repeat on each fragment.
type BoxDecorationBreak int

const (
	BoxDecorationBreakSlice BoxDecorationBreak = iota
	BoxDecorationBreakClone
)

// GetBoxDecorationBreak returns the box-decoration-break value (default: slice).
// Also checks the -webkit-box-decoration-break vendor-prefixed alias.
func (s *Style) GetBoxDecorationBreak() BoxDecorationBreak {
	if val, ok := s.Get("box-decoration-break"); ok {
		if val == "clone" {
			return BoxDecorationBreakClone
		}
		return BoxDecorationBreakSlice
	}
	if val, ok := s.Get("-webkit-box-decoration-break"); ok {
		if val == "clone" {
			return BoxDecorationBreakClone
		}
	}
	return BoxDecorationBreakSlice
}

// ObjectFit represents the object-fit CSS property
type ObjectFit int

const (
	ObjectFitFill ObjectFit = iota
	ObjectFitContain
	ObjectFitCover
	ObjectFitNone
	ObjectFitScaleDown
)

// GetTextWrap returns the text-wrap value (default: "normal").
// Supported values: "normal", "balance", "pretty", "nowrap", "stable".
func (s *Style) GetTextWrap() string {
	if v, ok := s.Get("text-wrap"); ok {
		return v
	}
	return "normal"
}

// GetObjectFit returns the object-fit value (default: fill)
func (s *Style) GetObjectFit() ObjectFit {
	if val, ok := s.Get("object-fit"); ok {
		switch val {
		case "contain":
			return ObjectFitContain
		case "cover":
			return ObjectFitCover
		case "none":
			return ObjectFitNone
		case "scale-down":
			return ObjectFitScaleDown
		}
	}
	return ObjectFitFill
}

// GetObjectPosition returns the object-position as (x%, y%) in range [0,1].
// Default is (0.5, 0.5) which centers the image.
func (s *Style) GetObjectPosition() (float64, float64) {
	val, ok := s.Get("object-position")
	if !ok {
		return 0.5, 0.5
	}
	parts := strings.Fields(val)
	x, y := 0.5, 0.5
	if len(parts) >= 1 {
		x = parsePositionKeyword(parts[0])
	}
	if len(parts) >= 2 {
		y = parsePositionKeyword(parts[1])
	}
	return x, y
}

// parsePositionKeyword converts a position keyword or percentage to a 0-1 fraction.
func parsePositionKeyword(s string) float64 {
	switch s {
	case "left", "top":
		return 0.0
	case "center":
		return 0.5
	case "right", "bottom":
		return 1.0
	}
	if strings.HasSuffix(s, "%") {
		if v, err := strconv.ParseFloat(s[:len(s)-1], 64); err == nil {
			return v / 100
		}
	}
	return 0.5
}

// AspectRatio represents a parsed aspect-ratio CSS value
type AspectRatio struct {
	Width  float64 // Numerator (e.g., 16 in 16/9)
	Height float64 // Denominator (e.g., 9 in 16/9)
	IsSet  bool    // True if aspect-ratio was explicitly set
}

// GetAspectRatio parses the aspect-ratio property.
// Formats: "auto", "16/9", "16 / 9", "1.5", "auto 16/9"
func (s *Style) GetAspectRatio() AspectRatio {
	val, ok := s.Get("aspect-ratio")
	if !ok || val == "auto" {
		return AspectRatio{}
	}
	// Strip "auto" prefix (e.g., "auto 16/9")
	val = strings.TrimSpace(strings.TrimPrefix(val, "auto"))
	if val == "" {
		return AspectRatio{}
	}
	// Try "W / H" or "W/H" format
	if idx := strings.Index(val, "/"); idx >= 0 {
		wStr := strings.TrimSpace(val[:idx])
		hStr := strings.TrimSpace(val[idx+1:])
		w, errW := strconv.ParseFloat(wStr, 64)
		h, errH := strconv.ParseFloat(hStr, 64)
		if errW == nil && errH == nil && w > 0 && h > 0 {
			return AspectRatio{Width: w, Height: h, IsSet: true}
		}
	}
	// Try single number (e.g., "1.5" means 1.5/1)
	if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && f > 0 {
		return AspectRatio{Width: f, Height: 1, IsSet: true}
	}
	return AspectRatio{}
}

// GetMaskImage returns the mask-image property value.
//
// Resolution order (matches CSS Masking 1 §6.2 + the SVG `mask`
// presentation attribute):
//
//  1. `mask-image` longhand
//  2. `-webkit-mask-image` vendor longhand
//  3. `mask` shorthand (covers the SVG presentation attribute case:
//     `<rect mask="url(#m)"/>` cascades into `mask: url(#m)`, which
//     the CSS Masking shorthand expands to `mask-image: url(#m)`).
//     We treat the raw shorthand value as the image source — Phase 5
//     doesn't yet parse the full shorthand grammar, but the SVG
//     reftests in scope only ever use the `url(#…)` form, which is
//     a valid single-image mask-image value.
func (s *Style) GetMaskImage() string {
	if val, ok := s.Get("mask-image"); ok {
		return val
	}
	// Also check -webkit-mask-image
	if val, ok := s.Get("-webkit-mask-image"); ok {
		return val
	}
	if val, ok := s.Get("mask"); ok && val != "" && val != "none" {
		return val
	}
	return "none"
}

// GetTabSize returns the tab-size property value.
// Returns (value, isLength) where:
//   - isLength=true: value is a pixel length (e.g., tab-size: 80px → 80.0)
//   - isLength=false: value is a character count (e.g., tab-size: 4 → 4.0)
//
// Default is 8 characters (isLength=false).
func (s *Style) GetTabSize() (float64, bool) {
	if val, ok := s.Get("tab-size"); ok {
		val = strings.TrimSpace(val)
		// Try length first (px, em, etc.)
		if length, ok2 := ParseLength(val); ok2 {
			return length, true // isLength=true
		}
		// Try numeric (character count)
		if num, err := strconv.ParseFloat(val, 64); err == nil {
			return num, false // isLength=false
		}
	}
	return 8, false // Default: 8 characters
}

// GetBackdropFilter parses the backdrop-filter property and returns filter
// functions. Phase 2 of LOU-138 makes the stored value already-absolute —
// see GetFilter for the idempotency rationale.
func (s *Style) GetBackdropFilter() []FilterFunction {
	val, ok := s.Get("backdrop-filter")
	if !ok || val == "none" || val == "" {
		return nil
	}
	// backdrop-filter accepts the same <filter-value-list> as filter.
	return parseFilterList(val)
}

// GetBorderImageSource returns the border-image-source value.
func (s *Style) GetBorderImageSource() string {
	if v, ok := s.Get("border-image-source"); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// BorderImageSlice holds the 4 slice values and fill flag.
type BorderImageSlice struct {
	Top, Right, Bottom, Left float64 // percentage (0-100) or pixel values
	IsPercent                bool
	Fill                     bool
}

// GetBorderImageSlice parses border-image-slice.
func (s *Style) GetBorderImageSlice() BorderImageSlice {
	v, ok := s.Get("border-image-slice")
	if !ok || strings.TrimSpace(v) == "" {
		return BorderImageSlice{Top: 100, Right: 100, Bottom: 100, Left: 100, IsPercent: true}
	}
	v = strings.TrimSpace(v)
	fill := false
	if strings.HasSuffix(v, " fill") {
		fill = true
		v = strings.TrimSuffix(v, " fill")
	}
	isPercent := strings.Contains(v, "%")
	parts := strings.Fields(v)
	var vals [4]float64
	for i, p := range parts {
		if i >= 4 {
			break
		}
		p = strings.TrimSuffix(p, "%")
		p = strings.TrimSuffix(p, "px")
		f, _ := strconv.ParseFloat(p, 64)
		vals[i] = f
	}
	// Fill missing values per CSS shorthand rules
	switch len(parts) {
	case 1:
		vals[1] = vals[0]
		vals[2] = vals[0]
		vals[3] = vals[0]
	case 2:
		vals[2] = vals[0]
		vals[3] = vals[1]
	case 3:
		vals[3] = vals[1]
	}
	return BorderImageSlice{Top: vals[0], Right: vals[1], Bottom: vals[2], Left: vals[3], IsPercent: isPercent, Fill: fill}
}

// GetBorderImageWidth returns the 4 border-image-width values in pixels.
// Values can be <number> (multiplier of border-width), <length>, or auto.
// borderWidths is [top, right, bottom, left] in pixels.
//
// Per the CSS shorthand expansion rules, the 1/2/3 value forms are first
// expanded into the 4-value form, and only then is each number multiplied
// by the corresponding border-width. The previous in-place expansion
// (multiply then copy vals[0]) silently dropped the per-side multiplier
// when border widths differed.
func (s *Style) GetBorderImageWidth(borderWidths [4]float64) [4]float64 {
	v, ok := s.Get("border-image-width")
	if !ok || strings.TrimSpace(v) == "" {
		return borderWidths // default = border-width values
	}
	parts := strings.Fields(strings.TrimSpace(v))
	if len(parts) == 0 {
		return borderWidths
	}
	if len(parts) > 4 {
		parts = parts[:4]
	}
	// Expand to 4 values per CSS shorthand expansion rules.
	var four [4]string
	switch len(parts) {
	case 1:
		four = [4]string{parts[0], parts[0], parts[0], parts[0]}
	case 2:
		four = [4]string{parts[0], parts[1], parts[0], parts[1]}
	case 3:
		four = [4]string{parts[0], parts[1], parts[2], parts[1]}
	case 4:
		four = [4]string{parts[0], parts[1], parts[2], parts[3]}
	}
	var vals [4]float64
	for i, p := range four {
		if p == "auto" {
			vals[i] = borderWidths[i]
		} else if strings.HasSuffix(p, "px") {
			f, _ := strconv.ParseFloat(strings.TrimSuffix(p, "px"), 64)
			vals[i] = f
		} else {
			// number = multiplier of corresponding border-width
			f, _ := strconv.ParseFloat(p, 64)
			vals[i] = f * borderWidths[i]
		}
	}
	return vals
}

// GetBorderImageOutset returns the 4 border-image-outset values in pixels.
// Per CSS Backgrounds 3 §6.2: values can be <length> (px) or <number>
// (multiplier of the corresponding border-width). The painted area is
// extended outward by these amounts; the layout box is not affected.
// borderWidths is [top, right, bottom, left] in pixels.
//
// Per the CSS shorthand expansion rules, the 1/2/3 value forms are first
// expanded into the 4-value form (so a single number maps to all four
// sides), and only then is each number multiplied by the corresponding
// border-width.
func (s *Style) GetBorderImageOutset(borderWidths [4]float64) [4]float64 {
	v, ok := s.Get("border-image-outset")
	if !ok || strings.TrimSpace(v) == "" {
		return [4]float64{} // initial: 0 on all sides
	}
	parts := strings.Fields(strings.TrimSpace(v))
	if len(parts) == 0 {
		return [4]float64{}
	}
	if len(parts) > 4 {
		parts = parts[:4]
	}
	// Expand to 4 values per CSS shorthand expansion rules.
	var four [4]string
	switch len(parts) {
	case 1:
		four = [4]string{parts[0], parts[0], parts[0], parts[0]}
	case 2:
		four = [4]string{parts[0], parts[1], parts[0], parts[1]}
	case 3:
		four = [4]string{parts[0], parts[1], parts[2], parts[1]}
	case 4:
		four = [4]string{parts[0], parts[1], parts[2], parts[3]}
	}
	var vals [4]float64
	for i, p := range four {
		if strings.HasSuffix(p, "px") {
			f, _ := strconv.ParseFloat(strings.TrimSuffix(p, "px"), 64)
			vals[i] = f
		} else {
			// Unit-less number = multiplier of corresponding border-width.
			f, _ := strconv.ParseFloat(p, 64)
			vals[i] = f * borderWidths[i]
		}
	}
	return vals
}

// GetBorderImageRepeat returns the repeat keywords [horizontal, vertical].
func (s *Style) GetBorderImageRepeat() [2]string {
	v, ok := s.Get("border-image-repeat")
	if !ok || strings.TrimSpace(v) == "" {
		return [2]string{"stretch", "stretch"}
	}
	parts := strings.Fields(strings.TrimSpace(v))
	if len(parts) == 1 {
		return [2]string{parts[0], parts[0]}
	}
	return [2]string{parts[0], parts[1]}
}

// GetIsolation returns the isolation property value (default: "auto")
func (s *Style) GetIsolation() string {
	if val, ok := s.Get("isolation"); ok {
		return val
	}
	return "auto"
}

// GetWillChange returns the list of property names specified in will-change.
// Returns nil for "will-change: auto" or if not specified.
func (s *Style) GetWillChange() []string {
	val, ok := s.Get("will-change")
	if !ok || val == "" || val == "auto" {
		return nil
	}
	parts := strings.Split(val, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != "auto" {
			result = append(result, p)
		}
	}
	return result
}

// GetTextDecorationStyle returns the text-decoration-style value (default: "solid")
func (s *Style) GetTextDecorationStyle() string {
	if val, ok := s.Get("text-decoration-style"); ok {
		return val
	}
	return "solid"
}

// GetTextDecorationThickness returns the text-decoration-thickness value in pixels (default: 1)
func (s *Style) GetTextDecorationThickness() float64 {
	if val, ok := s.Get("text-decoration-thickness"); ok {
		if val == "auto" || val == "from-font" {
			return 1
		}
		if length, ok := ParseLengthWithFontSize(val, s.GetFontSize()); ok {
			return length
		}
	}
	return 1
}

// GetHyphens returns the hyphens property value.
// Values: "none", "manual", "auto".
// Default is "manual" (CSS default for HTML content).
// "auto" is treated as "manual" (no dictionary-based hyphenation).
func (s *Style) GetHyphens() string {
	if val, ok := s.Get("hyphens"); ok {
		val = strings.TrimSpace(strings.ToLower(val))
		switch val {
		case "none", "manual", "auto":
			return val
		}
	}
	return "manual"
}

// GetTextEmphasisStyle returns the text-emphasis-style value.
func (s *Style) GetTextEmphasisStyle() string {
	v, _ := s.Get("text-emphasis-style")
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "none"
	}
	return v
}

// GetTextEmphasisColor returns the text-emphasis color (defaults to currentColor).
func (s *Style) GetTextEmphasisColor() Color {
	v, _ := s.Get("text-emphasis-color")
	v = strings.TrimSpace(v)
	if v == "" {
		return s.GetColor() // currentColor
	}
	if c, ok := ParseColor(v); ok {
		return c
	}
	return s.GetColor()
}

// GetTextEmphasisPosition returns where emphasis marks appear ("over right" by default).
func (s *Style) GetTextEmphasisPosition() string {
	v, _ := s.Get("text-emphasis-position")
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "over right"
	}
	return v
}

// GetTextEmphasisLineLogicalOver resolves the text-emphasis-position keyword
// combination through the writing mode and returns true when the mark paints
// on the kOver line-logical side, false for kUnder.
//
// Mirrors Blink's ComputedStyle::GetTextEmphasisLineLogicalSide()
// (third_party/blink/renderer/core/style/computed_style.cc @ Chromium SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Per CSS Text Decor §3.4 the
// position keywords resolve as:
//
//   - horizontal writing modes:       over → kOver, under → kUnder
//   - vertical-rl / vertical-lr /
//     sideways-rl:                    right → kOver, left → kUnder
//   - sideways-lr:                    left  → kOver, right → kUnder
//
// kOver means "above the inline progression line" — in horizontal-tb that is
// physically above the text; in vertical-rl/-lr/sideways-rl it is physically
// to the right of the text column; in sideways-lr it is physically to the
// left of the column.
func (s *Style) GetTextEmphasisLineLogicalOver(isHorizontal, isSidewaysLR bool) bool {
	pos := s.GetTextEmphasisPosition()
	// `auto` and any value missing an axis keyword default to kOver per
	// Blink's resolver — over for horizontal-tb (kUnder for macrolanguage
	// Chinese, not modeled here yet) and right (= kOver) for the vertical
	// modes including sideways-rl; sideways-lr resolves auto to kUnder
	// in Blink, but the WPT auto/default tests pin only the horizontal
	// and vertical-rl defaults.
	if pos == "auto" {
		if isSidewaysLR {
			return false
		}
		return true
	}
	hasOver := strings.Contains(pos, "over")
	hasUnder := strings.Contains(pos, "under")
	hasRight := strings.Contains(pos, "right")
	hasLeft := strings.Contains(pos, "left")
	if isHorizontal {
		// If neither over nor under is present (e.g. only `right`/`left`),
		// fall back to kOver — matches Blink's behavior of treating the
		// missing axis as its default.
		if !hasOver && !hasUnder {
			return true
		}
		return hasOver
	}
	if isSidewaysLR {
		// sideways-lr flips: left → kOver. If neither right nor left is
		// present, fall back to kUnder (sideways-lr's default).
		if !hasRight && !hasLeft {
			return false
		}
		return !hasRight
	}
	// vertical-rl, vertical-lr, sideways-rl: right → kOver. If neither
	// right nor left is present (e.g. only `over`/`under`), fall back to
	// kOver — vertical's "auto" default.
	if !hasRight && !hasLeft {
		return true
	}
	return hasRight
}

// GetTextEmphasisMark returns the actual character to use as emphasis mark.
// Returns "" if no emphasis should be drawn.
//
// `isHorizontal` selects the writing-mode-dependent default mark for bare
// `filled`/`open` values. Per CSS Text Decor §3.4.4 and Blink's
// `ComputedStyle::TextEmphasisMarkString()` at Chromium SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f: `filled` (with no shape) is
// "filled circle" (U+25CF) in horizontal writing modes and "filled sesame"
// (U+FE45) in vertical writing modes; `open` mirrors with U+25CB / U+FE46.
// Explicit shape keywords (e.g. `filled dot`, `open triangle`) override the
// writing-mode default.
func (s *Style) GetTextEmphasisMark(isHorizontal bool) string {
	style := s.GetTextEmphasisStyle()
	if style == "none" || style == "" {
		return ""
	}

	// Custom string: "x" or 'x' (quoted string)
	if len(style) >= 2 && (style[0] == '"' || style[0] == '\'') {
		return style[1 : len(style)-1]
	}

	// Standard shapes
	filled := true
	if strings.Contains(style, "open") {
		filled = false
	}

	shape := "circle" // default
	if strings.Contains(style, "double-circle") {
		shape = "double-circle"
	} else if strings.Contains(style, "sesame") {
		shape = "sesame"
	} else if strings.Contains(style, "triangle") {
		shape = "triangle"
	} else if strings.Contains(style, "dot") {
		shape = "dot"
	} else if strings.Contains(style, "circle") {
		shape = "circle"
	}
	// If only "filled" or "open" (no shape specified), default to circle for
	// horizontal writing modes and sesame for vertical writing modes.
	if style == "filled" {
		filled = true
		if isHorizontal {
			shape = "circle"
		} else {
			shape = "sesame"
		}
	} else if style == "open" {
		filled = false
		if isHorizontal {
			shape = "circle"
		} else {
			shape = "sesame"
		}
	}

	switch shape {
	case "dot":
		if filled {
			return "\u2022" // •
		}
		return "\u25e6" // ◦
	case "circle":
		if filled {
			return "\u25cf" // ●
		}
		return "\u25cb" // ○
	case "double-circle":
		if filled {
			return "\u25c9" // ◉
		}
		return "\u25ce" // ◎
	case "triangle":
		if filled {
			return "\u25b2" // ▲
		}
		return "\u25b3" // △
	case "sesame":
		if filled {
			return "\ufe45" // ﹅
		}
		return "\ufe46" // ﹆
	default:
		if filled {
			return "\u25cf" // ●
		}
		return "\u25cb" // ○
	}
}

// ScrollbarColorValue holds the thumb and track colors for scrollbar-color.
type ScrollbarColorValue struct {
	Thumb  Color
	Track  Color
	IsAuto bool
}

// splitScrollbarColorValues splits a CSS value string containing two color values.
// Colors can be functions like rgb(r,g,b) which contain commas, so we track paren depth.
func splitScrollbarColorValues(v string) []string {
	var results []string
	depth := 0
	start := 0
	for i, c := range v {
		if c == '(' {
			depth++
		}
		if c == ')' {
			depth--
		}
		if depth == 0 && c == ' ' && i > start {
			token := strings.TrimSpace(v[start:i])
			if token != "" {
				results = append(results, token)
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(v[start:]); last != "" {
		results = append(results, last)
	}
	return results
}

// GetScrollbarColor returns the scrollbar-color value.
// Returns IsAuto=true for "auto" or when unset.
func (s *Style) GetScrollbarColor() ScrollbarColorValue {
	v, ok := s.Get("scrollbar-color")
	if !ok {
		return ScrollbarColorValue{IsAuto: true}
	}
	v = strings.TrimSpace(v)
	if v == "" || v == "auto" {
		return ScrollbarColorValue{IsAuto: true}
	}
	parts := splitScrollbarColorValues(v)
	if len(parts) >= 2 {
		thumbColor, thumbOk := ParseColor(parts[0])
		trackColor, trackOk := ParseColor(parts[1])
		if thumbOk && trackOk {
			return ScrollbarColorValue{Thumb: thumbColor, Track: trackColor}
		}
	}
	return ScrollbarColorValue{IsAuto: true}
}

// GetScrollbarGutter returns the scrollbar-gutter value (default: "auto").
func (s *Style) GetScrollbarGutter() string {
	v, ok := s.Get("scrollbar-gutter")
	if !ok {
		return "auto"
	}
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "auto"
	}
	return v
}

// GetScrollbarWidth returns the scrollbar-width keyword (default: "auto").
// Possible values: "auto", "thin", "none".
func (s *Style) GetScrollbarWidth() string {
	v, ok := s.Get("scrollbar-width")
	if !ok {
		return "auto"
	}
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "auto"
	}
	return v
}

// ApplyHTMLDirAttribute maps the HTML dir attribute to CSS direction and
// unicode-bidi properties. Per the HTML specification, dir="rtl" implies
// direction:rtl and unicode-bidi:isolate (or isolate-override for <bdo>).
// This is called as a post-processing step after CSS cascade to handle
// the HTML presentational hint that the dir attribute represents.
func ApplyHTMLDirAttribute(node *html.Node, style *Style) {
	if node.Type != html.ElementNode {
		return
	}
	dirAttr, ok := node.GetAttribute("dir")
	if !ok {
		return
	}
	dirAttr = strings.ToLower(strings.TrimSpace(dirAttr))

	// Only set direction if not already explicitly set by author CSS.
	// The dir attribute acts as a presentational hint (lower priority than CSS).
	// However, for proper bidi behavior, we always apply it since the UA stylesheet
	// normally handles this and our cascade doesn't have a UA rule for [dir].
	switch dirAttr {
	case "rtl":
		style.Set("direction", "rtl")
	case "ltr":
		style.Set("direction", "ltr")
	case "auto":
		// dir="auto" is resolved per HTML §3.2.6 by scanning the
		// element's descendant text for the first character with a
		// strong Unicode bidi class. The shared elementDirectionality
		// helper (matcher.go) walks ancestors when no strong character
		// is found, matching the spec's "parent fallback" rule.
		if elementDirectionality(node) == DirectionRTL {
			style.Set("direction", "rtl")
		} else {
			style.Set("direction", "ltr")
		}
	}

	// Per HTML spec, dir attribute also implies unicode-bidi.
	// <bdo> gets isolate-override; all other elements get isolate.
	if dirAttr == "rtl" || dirAttr == "ltr" || dirAttr == "auto" {
		if _, hasBidi := style.Get("unicode-bidi"); !hasBidi {
			if node.TagName == "bdo" {
				style.Set("unicode-bidi", "isolate-override")
			} else {
				style.Set("unicode-bidi", "isolate")
			}
		}
	}
}

// ApplyHTMLDirToTree walks the entire DOM tree and applies the HTML dir
// attribute mapping for any node that has it. This should be called after
// CSS cascade (ApplyStylesToDocument) but before layout.
func ApplyHTMLDirToTree(node *html.Node, styles map[*html.Node]*Style) {
	if node.Type == html.ElementNode {
		if style, ok := styles[node]; ok {
			ApplyHTMLDirAttribute(node, style)
		}
	}
	for _, child := range node.Children {
		ApplyHTMLDirToTree(child, styles)
	}
}

// ComputeStyleWithLogical wraps ComputeStyle and also applies HTML dir attribute
// mapping and resolves logical properties. This should be used by layout code
// that calls ComputeStyle for individual nodes after the initial cascade.
func ComputeStyleWithLogical(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64, ctx *ParserContext) *Style {
	style := ComputeStyle(node, stylesheets, viewportWidth, viewportHeight, ctx)
	ApplyHTMLDirAttribute(node, style)
	ResolveLogicalProperties(node, style)
	return style
}

// ResolveLogicalProperties remaps CSS logical properties (border-inline-start,
// margin-block-end, etc.) to their physical counterparts based on the element's
// computed writing-mode and direction. This must be called after the full CSS
// cascade and dir attribute application.
//
// The expandShorthand function in the cascade already maps logical properties
// to physical, but it assumes horizontal-tb + ltr. This function fixes up
// the mapping for non-default writing modes by using the -x-logical-* marker
// properties that expandShorthand stores.
func ResolveLogicalProperties(node *html.Node, style *Style) {
	if node.Type != html.ElementNode {
		return
	}
	wm, _ := style.Get("writing-mode")
	dir := style.GetDirection()

	// Only need to fix up if writing-mode is vertical/sideways or direction is rtl
	isVertical := wm == "vertical-rl" || wm == "vertical-lr"
	isSideways := wm == "sideways-lr" || wm == "sideways-rl"
	isRTL := dir == DirectionRTL

	if !isVertical && !isSideways && !isRTL {
		return // horizontal-tb + ltr: default mapping is correct
	}

	// Determine the physical side mapping for each logical direction.
	// Default (horizontal-tb + ltr) already applied by expandShorthand:
	//   inline-start → left,  inline-end → right
	//   block-start  → top,   block-end  → bottom
	//
	// We need to determine the CORRECT mapping and re-apply.
	var inlineStart, inlineEnd, blockStart, blockEnd string
	if isVertical {
		// vertical-rl or vertical-lr: inline axis is vertical (top-to-bottom)
		if isRTL {
			inlineStart = "bottom"
			inlineEnd = "top"
		} else {
			inlineStart = "top"
			inlineEnd = "bottom"
		}
		if wm == "vertical-rl" {
			blockStart = "right"
			blockEnd = "left"
		} else { // vertical-lr
			blockStart = "left"
			blockEnd = "right"
		}
	} else if isSideways {
		if wm == "sideways-lr" {
			// sideways-lr: inline direction is bottom-to-top, block is left-to-right
			if isRTL {
				inlineStart = "top"
				inlineEnd = "bottom"
			} else {
				inlineStart = "bottom"
				inlineEnd = "top"
			}
			blockStart = "left"
			blockEnd = "right"
		} else { // sideways-rl
			// sideways-rl: inline direction is top-to-bottom, block is right-to-left
			if isRTL {
				inlineStart = "bottom"
				inlineEnd = "top"
			} else {
				inlineStart = "top"
				inlineEnd = "bottom"
			}
			blockStart = "right"
			blockEnd = "left"
		}
	} else {
		// horizontal-tb + rtl
		inlineStart = "right"
		inlineEnd = "left"
		blockStart = "top"
		blockEnd = "bottom"
	}

	// Mapping from logical direction to physical side
	logicalToPhysical := map[string]string{
		"inline-start": inlineStart,
		"inline-end":   inlineEnd,
		"block-start":  blockStart,
		"block-end":    blockEnd,
	}

	// Default mapping (htb+ltr) used by expandShorthand
	defaultMapping := map[string]string{
		"inline-start": "left",
		"inline-end":   "right",
		"block-start":  "top",
		"block-end":    "bottom",
	}

	// Remap border logical properties
	for _, logicalDir := range []string{"inline-start", "inline-end", "block-start", "block-end"} {
		physSide := logicalToPhysical[logicalDir]
		defaultSide := defaultMapping[logicalDir]

		// Skip if the correct mapping is the same as the default
		if physSide == defaultSide {
			continue
		}

		// border-* shorthand (e.g. border-inline-start: 5px green solid)
		if val, ok := style.Get("_border-" + logicalDir); ok {
			// Clear the wrongly-set default physical properties
			delete(style.Properties, "border-"+defaultSide+"-width")
			delete(style.Properties, "border-"+defaultSide+"-style")
			delete(style.Properties, "border-"+defaultSide+"-color")
			// Set the correct physical properties
			expandBorderSideProperty(style, "border-"+physSide, val)
		}

		// border-*-width
		if val, ok := style.Get("_border-" + logicalDir + "-width"); ok {
			delete(style.Properties, "border-"+defaultSide+"-width")
			style.Set("border-"+physSide+"-width", val)
		}
		// border-*-style
		if val, ok := style.Get("_border-" + logicalDir + "-style"); ok {
			delete(style.Properties, "border-"+defaultSide+"-style")
			style.Set("border-"+physSide+"-style", val)
		}
		// border-*-color
		if val, ok := style.Get("_border-" + logicalDir + "-color"); ok {
			delete(style.Properties, "border-"+defaultSide+"-color")
			style.Set("border-"+physSide+"-color", val)
		}
	}

	// Remap margin logical properties
	for _, logicalDir := range []string{"inline-start", "inline-end", "block-start", "block-end"} {
		physSide := logicalToPhysical[logicalDir]
		defaultSide := defaultMapping[logicalDir]
		if physSide == defaultSide {
			continue
		}
		if val, ok := style.Get("_margin-" + logicalDir); ok {
			delete(style.Properties, "margin-"+defaultSide)
			if _, exists := style.Properties["margin-"+physSide]; !exists {
				style.Set("margin-"+physSide, val)
			}
		}
	}

	// Remap padding logical properties
	for _, logicalDir := range []string{"inline-start", "inline-end", "block-start", "block-end"} {
		physSide := logicalToPhysical[logicalDir]
		defaultSide := defaultMapping[logicalDir]
		if physSide == defaultSide {
			continue
		}
		if val, ok := style.Get("_padding-" + logicalDir); ok {
			delete(style.Properties, "padding-"+defaultSide)
			// Don't overwrite an existing physical property from the author CSS.
			// The logical property may come from a lower-priority source (e.g. UA
			// stylesheet's padding-inline-start on <ul>) while the physical property
			// was explicitly set by the author (e.g. padding-top: 1em).
			if _, exists := style.Properties["padding-"+physSide]; !exists {
				style.Set("padding-"+physSide, val)
			}
		}
	}

	// Remap inset logical properties
	for _, logicalDir := range []string{"inline-start", "inline-end", "block-start", "block-end"} {
		physSide := logicalToPhysical[logicalDir]
		defaultSide := defaultMapping[logicalDir]
		if physSide == defaultSide {
			continue
		}
		if val, ok := style.Get("_inset-" + logicalDir); ok {
			delete(style.Properties, defaultSide)
			style.Set(physSide, val)
		}
	}

	// Remap inline-size / block-size for both explicit and inherited
	// writing-mode — louis14 lays out natively in logical space.
	if val, ok := style.Get("_inline-size"); ok {
		if isVertical {
			style.Set("height", val)
		} else {
			style.Set("width", val) // htb+rtl: width is still inline size
		}
	}
	if val, ok := style.Get("_block-size"); ok {
		if isVertical {
			style.Set("width", val)
		} else {
			style.Set("height", val)
		}
	}
	if val, ok := style.Get("_min-inline-size"); ok {
		if isVertical {
			style.Set("min-height", val)
		} else {
			style.Set("min-width", val)
		}
	}
	if val, ok := style.Get("_min-block-size"); ok {
		if isVertical {
			style.Set("min-width", val)
		} else {
			style.Set("min-height", val)
		}
	}
	if val, ok := style.Get("_max-inline-size"); ok {
		if isVertical {
			style.Set("max-height", val)
		} else {
			style.Set("max-width", val)
		}
	}
	if val, ok := style.Get("_max-block-size"); ok {
		if isVertical {
			style.Set("max-width", val)
		} else {
			style.Set("max-height", val)
		}
	}
}

// ResolveLogicalPropertiesInTree walks the DOM tree and resolves logical properties.
func ResolveLogicalPropertiesInTree(node *html.Node, styles map[*html.Node]*Style) {
	if node.Type == html.ElementNode {
		if style, ok := styles[node]; ok {
			ResolveLogicalProperties(node, style)
		}
	}
	for _, child := range node.Children {
		ResolveLogicalPropertiesInTree(child, styles)
	}
}

// ----- will-change canonical value model -------------------------------------
//
// The WillChange type mirrors Blink's StyleWillChangeData
// (third_party/blink/renderer/core/style/style_will_change_data.h @ SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// Side-effect predicates (containing-block, stacking-context, transform-related)
// must all flow through this single resolved model; never re-parse the raw
// will-change string at the call site. The Blink comment is load-bearing:
// "The bitset only contains resolved longhand CSSPropertyID(s). No aliases."
//
// louis14 keys CSS by property *name* rather than enum id, so the bitset is
// modeled as map[string]bool. Aliases and shorthand idents are normalized at
// parse time so consumers can query by longhand name only.

// WillChange is the resolved, parsed view of the `will-change` property.
//
// Field correspondence with Blink's StyleWillChangeData:
//
//	Values                   ↔ values                       (raw idents, source order)
//	Longhands                ↔ resolved_longhand_ids        (resolved longhand names only)
//	HasScrollPosition        ↔ has_scroll_position_value
//	HasTransformProperty     ↔ has_transform_property       (narrow: only "transform")
//	HasAnyTransformProperty  ↔ has_any_transform_property   (broad set)
type WillChange struct {
	// Values preserves the raw ident list in source order, lowercased and
	// trimmed. Used for serialization parity with getComputedStyle().
	Values []string
	// Longhands is the set of resolved longhand property names named by
	// `will-change` after alias normalization and shorthand expansion.
	// No aliases. No shorthands. (Mirrors Blink's resolved_longhand_ids
	// CSSBitset comment.) The empty-map case is distinct from a nil
	// *WillChange — nil means `auto` / unset / empty, while an empty map
	// (rare) means only `auto` or `scroll-position` tokens were present.
	Longhands map[string]bool
	// HasScrollPosition is true if `scroll-position` appeared. This is
	// not a real longhand — Blink keeps it as a separate bool.
	HasScrollPosition bool
	// HasTransformProperty is the narrow hint: true iff the literal
	// `transform` longhand was named. Mirrors Blink's
	// HasWillChangeTransformProperty() — see computed_style.h:1287.
	HasTransformProperty bool
	// HasAnyTransformProperty is the broad hint: true iff any of
	// `transform`, `translate`, `scale`, `rotate`, `perspective`,
	// `offset-path`, `transform-style`, `offset-position` was named.
	// This is what HasTransformRelatedProperty() folds in
	// (computed_style.h:2266) and what ComputeIsFixedContainer reads
	// via HasWillChangeAnyTransformProperty() at layout_object.cc:1888.
	HasAnyTransformProperty bool
}

// willChangeBroadTransformSet is the set of resolved longhands that
// contribute to the "any transform property" hint, mirroring Blink's
// has_any_transform_property field. Source: the union enumerated in
// HasPropertyThatCreatesStackingContext's transform-related members
// (computed_style.cc:1319) intersected with the transform-related
// properties tested in HasTransformRelatedProperty (computed_style.h:2266).
var willChangeBroadTransformSet = map[string]bool{
	"transform":       true,
	"translate":       true,
	"scale":           true,
	"rotate":          true,
	"perspective":     true,
	"offset-path":     true,
	"transform-style": true,
	"offset-position": true,
}

// willChangeAliasMap normalizes vendor-prefixed will-change idents to their
// unprefixed longhand. Mirrors Blink's CSS property alias resolution that
// happens before resolved_longhand_ids is built.
//
// Only aliases known to appear in WPT css-will-change tests + Blink's
// canonical SC set are listed.
var willChangeAliasMap = map[string]string{
	"-webkit-perspective": "perspective",
	"-webkit-transform":   "transform",
	"-webkit-filter":      "filter",
	// `-webkit-mask-box-image-source` is the legacy alias for
	// `mask-border-source`; Blink's SC set lists kWebkitMaskBoxImageSource
	// as its OWN longhand, not a `mask-border-source` alias, so we keep it
	// untouched here.
}

// willChangeShorthandExpansion maps shorthand idents to the set of
// longhand idents they expand to FOR THE PURPOSES OF WILL-CHANGE
// SIDE EFFECTS. This is not the full shorthand-to-longhand table — only
// the longhands that influence will-change predicates (the rest are
// inert side-effect-wise and can be dropped from the resolved set).
//
// Blink's general shorthand expander runs in the cascade and would
// expand `mask` to mask-image, mask-mode, mask-position, mask-clip,
// mask-origin, mask-size, mask-composite, and (depending on flags)
// mask-repeat. Of those, only mask-image and -webkit-mask-box-image-source
// appear in HasPropertyThatCreatesStackingContext (computed_style.cc:1319).
// So `mask` expands here to {mask-image, -webkit-mask-box-image-source}.
var willChangeShorthandExpansion = map[string][]string{
	"mask": {"mask-image", "-webkit-mask-box-image-source"},
}

// willChangeStackingContextSet is the canonical set of resolved longhands
// that, when named in `will-change`, cause the element to establish a
// stacking context per CSS Will Change Level 1 §3. Mirrors Blink's
// HasPropertyThatCreatesStackingContext at computed_style.cc:1319 verbatim:
//
//	kOpacity, kTransform, kTransformStyle, kPerspective, kTranslate,
//	kRotate, kScale, kOffsetPath, kOffsetPosition, kMaskImage,
//	kWebkitMaskBoxImageSource, kClipPath, kWebkitBoxReflect, kFilter,
//	kBackdropFilter, kPosition, kMixBlendMode, kIsolation, kContain,
//	kViewTransitionName,
//	kZIndex (only if allows_z_index — gated separately in
//	WillChangeCreatesStackingContext).
var willChangeStackingContextSet = map[string]bool{
	"opacity":                       true,
	"transform":                     true,
	"transform-style":               true,
	"perspective":                   true,
	"translate":                     true,
	"rotate":                        true,
	"scale":                         true,
	"offset-path":                   true,
	"offset-position":               true,
	"mask-image":                    true,
	"-webkit-mask-box-image-source": true,
	"clip-path":                     true,
	"-webkit-box-reflect":           true,
	"filter":                        true,
	"backdrop-filter":               true,
	"position":                      true,
	"mix-blend-mode":                true,
	"isolation":                     true,
	"contain":                       true,
	"view-transition-name":          true,
	// "z-index" is handled by the allowsZIndex gate in the predicate.
}

// GetWillChangeData parses the `will-change` property into the canonical
// resolved value model. Returns nil for `auto`, unset, empty, or for any
// declaration that contains only non-longhand-resolving tokens (none of
// which currently exist in the spec — but be defensive).
//
// This is the single source of truth for will-change side-effect predicates.
// Phases 2-5 route all consumers through it; the legacy GetWillChange()
// []string accessor is left intact for callers that need raw idents.
func (s *Style) GetWillChangeData() *WillChange {
	val, ok := s.Get("will-change")
	if !ok {
		return nil
	}
	val = strings.TrimSpace(val)
	if val == "" || val == "auto" {
		return nil
	}
	parts := strings.Split(val, ",")
	wc := &WillChange{
		Values:    make([]string, 0, len(parts)),
		Longhands: make(map[string]bool, len(parts)),
	}
	for _, p := range parts {
		ident := strings.TrimSpace(strings.ToLower(p))
		if ident == "" || ident == "auto" {
			continue
		}
		wc.Values = append(wc.Values, ident)
		// scroll-position is a special non-longhand keyword.
		if ident == "scroll-position" {
			wc.HasScrollPosition = true
			continue
		}
		// Alias normalization (-webkit-* → unprefixed where applicable).
		if canon, isAlias := willChangeAliasMap[ident]; isAlias {
			ident = canon
		}
		// Shorthand expansion: replace the shorthand ident with its
		// SC/CB-relevant longhand set.
		if longs, isShorthand := willChangeShorthandExpansion[ident]; isShorthand {
			for _, lh := range longs {
				wc.Longhands[lh] = true
			}
			continue
		}
		// Otherwise treat as a longhand-or-unknown ident; record it.
		// Unknown idents (e.g. `foo-bar`) are kept in the set so a future
		// property expansion picks them up automatically — they won't
		// match any predicate set, so they are inert.
		wc.Longhands[ident] = true
	}
	// Cache predicate flags at parse time so consumers are O(1).
	wc.HasTransformProperty = wc.Longhands["transform"]
	for lh := range wc.Longhands {
		if willChangeBroadTransformSet[lh] {
			wc.HasAnyTransformProperty = true
			break
		}
	}
	// An entry that named only `auto` / `scroll-position` and nothing
	// else still produces a non-nil *WillChange (Blink does too — the
	// HasWillChangeScrollPosition() accessor depends on it). The nil
	// case is reserved for the auto/unset/empty-value paths above.
	return wc
}

// HasWillChangeProperty mirrors Blink's
// ComputedStyle::HasWillChangeProperty(CSSPropertyID id)
// (computed_style.h:1278). Returns true iff the named *resolved longhand*
// property appears in the will-change ident list.
//
// `name` MUST be a longhand property name. Shorthand or alias inputs
// return false (Blink DCHECKs this; we silently return false).
func (s *Style) HasWillChangeProperty(name string) bool {
	wc := s.GetWillChangeData()
	if wc == nil {
		return false
	}
	return wc.Longhands[name]
}

// HasWillChangeScrollPosition mirrors
// ComputedStyle::HasWillChangeScrollPosition() (computed_style.h:1284).
func (s *Style) HasWillChangeScrollPosition() bool {
	wc := s.GetWillChangeData()
	return wc != nil && wc.HasScrollPosition
}

// HasWillChangeTransformProperty is the narrow hint: only the literal
// `transform` longhand. Mirrors
// ComputedStyle::HasWillChangeTransformProperty() (computed_style.h:1287).
func (s *Style) HasWillChangeTransformProperty() bool {
	wc := s.GetWillChangeData()
	return wc != nil && wc.HasTransformProperty
}

// HasWillChangeAnyTransformProperty is the broad hint covering every
// transform-related longhand (transform, translate, scale, rotate,
// perspective, offset-path, transform-style, offset-position). Mirrors
// ComputedStyle::HasWillChangeAnyTransformProperty()
// (computed_style.h:1290), which folds into HasTransformRelatedProperty()
// at computed_style.h:2266 and is consulted by ComputeIsFixedContainer
// at layout_object.cc:1888.
func (s *Style) HasWillChangeAnyTransformProperty() bool {
	wc := s.GetWillChangeData()
	return wc != nil && wc.HasAnyTransformProperty
}

// WillChangeCreatesStackingContext mirrors Blink's static helper
// HasPropertyThatCreatesStackingContext(WillChange(), allows_z_index)
// at computed_style.cc:1319, called from
// CalculateIsStackingContextWithoutContainment at computed_style.cc:2927.
//
// The `allowsZIndex` parameter gates the kZIndex contribution per Blink:
// only positioned-or-flex/grid items have AllowsZIndex set
// (style_adjuster.cc:1240-1247), so a static-position non-flex/grid box
// with `will-change: z-index` does NOT establish a stacking context.
//
// Returns false for `auto`/unset.
func (s *Style) WillChangeCreatesStackingContext(allowsZIndex bool) bool {
	wc := s.GetWillChangeData()
	if wc == nil {
		return false
	}
	if allowsZIndex && wc.Longhands["z-index"] {
		return true
	}
	for lh := range wc.Longhands {
		if willChangeStackingContextSet[lh] {
			return true
		}
	}
	return false
}

// willChangeFixedContainingBlockSet is the canonical set of resolved
// longhands that, when named in `will-change`, cause the element to
// establish a containing block for FIXED-positioned descendants per CSS
// Will Change Level 1 §2.2. These are the properties that — when applied
// non-initially — would themselves create a CB for fixed descendants
// (transform-family + filters + clip-path + mask-image + contain). Mirrors
// the union of Blink's ComputeIsFixedContainer at layout_object.cc:~2150
// and ComputeIsAbsoluteContainer at ~2190 (the abs set is a superset —
// see willChangeAbsContainingBlockSet below).
//
// `position` is intentionally excluded: `will-change: position` mimics
// position: relative, which establishes a CB for abs-pos but NOT fixed.
var willChangeFixedContainingBlockSet = map[string]bool{
	"transform":                     true,
	"perspective":                   true,
	"translate":                     true,
	"rotate":                        true,
	"scale":                         true,
	"offset-path":                   true,
	"offset-position":               true,
	"transform-style":               true,
	"filter":                        true,
	"backdrop-filter":               true,
	"clip-path":                     true,
	"mask-image":                    true,
	"-webkit-mask-box-image-source": true,
	"contain":                       true,
	"view-transition-name":          true,
}

// willChangeAbsContainingBlockSet adds `position` to the fixed set: any
// property that would establish a CB for fixed descendants also does so
// for abs-pos descendants (fixed ⊂ absolute), AND `position` itself
// (mimicking position:relative) does so for abs-pos only.
var willChangeAbsContainingBlockSet = func() map[string]bool {
	m := make(map[string]bool, len(willChangeFixedContainingBlockSet)+1)
	for k, v := range willChangeFixedContainingBlockSet {
		m[k] = v
	}
	m["position"] = true
	return m
}()

// WillChangeEstablishesContainingBlock reports whether the element's
// `will-change` value causes it to establish a containing block for
// positioned descendants per CSS Will Change Level 1 §2.2. The
// `forFixedPos` flag selects between the absolute-pos and fixed-pos
// containing-block sets — `position` only establishes a CB for abs-pos
// (matching position:relative semantics), while transform/filter/etc.
// establish CBs for both. Mirrors Blink's ComputeIsAbsoluteContainer and
// ComputeIsFixedContainer at layout_object.cc:~2150-2190.
//
// Returns false for `auto`/unset.
func (s *Style) WillChangeEstablishesContainingBlock(forFixedPos bool) bool {
	wc := s.GetWillChangeData()
	if wc == nil {
		return false
	}
	set := willChangeAbsContainingBlockSet
	if forFixedPos {
		set = willChangeFixedContainingBlockSet
	}
	for lh := range wc.Longhands {
		if set[lh] {
			return true
		}
	}
	return false
}

// willChangeInlineApplicableSet is the subset of CB-establishing properties
// whose effect actually applies to a non-replaced inline box. Inlines are
// not "transformable elements" per CSS Transforms §3, so the transform
// family (transform, perspective, translate, rotate, scale, offset-path,
// offset-position, transform-style) never establishes a CB on an inline
// even via will-change — see WPT will-change-transform-inline.html.
// `contain` does not apply to inlines either. `view-transition-name` and
// `position` likewise don't make an inline a CB on their own.
//
// Filter, backdrop-filter, clip-path, and mask-image DO apply to inlines.
var willChangeInlineApplicableSet = map[string]bool{
	"filter":                        true,
	"backdrop-filter":               true,
	"clip-path":                     true,
	"mask-image":                    true,
	"-webkit-mask-box-image-source": true,
}

// WillChangeEstablishesInlineContainingBlock is the inline-only variant of
// WillChangeEstablishesContainingBlock: it filters out properties that
// don't apply to inline boxes (transform-family, contain, etc.) before
// reporting CB establishment. Use this when the element is a non-replaced
// inline. The transform-family/contain exclusion mirrors WPT's
// will-change-transform-inline.html assertion that
// `will-change: transform` on a `<span>` does NOT create a CB.
func (s *Style) WillChangeEstablishesInlineContainingBlock() bool {
	wc := s.GetWillChangeData()
	if wc == nil {
		return false
	}
	for lh := range wc.Longhands {
		if willChangeInlineApplicableSet[lh] {
			return true
		}
	}
	return false
}

// splitTopLevelCommas splits s on commas that are not nested inside parentheses
// or quotes. Used for comma-separated CSS list values (animation, transition).
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	inQ := byte(0)
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQ != 0:
			if c == inQ {
				inQ = 0
			}
		case c == '"' || c == '\'':
			inQ = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

// splitTopLevelFields splits s on whitespace runs that are not nested inside
// parentheses or quotes (so "steps(2, end)" stays one token).
func splitTopLevelFields(s string) []string {
	var fields []string
	depth := 0
	inQ := byte(0)
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		isSpace := c == ' ' || c == '\t' || c == '\n' || c == '\r'
		switch {
		case inQ != 0:
			if c == inQ {
				inQ = 0
			}
		case c == '"' || c == '\'':
			inQ = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && inQ == 0 && isSpace {
			if start >= 0 {
				fields = append(fields, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		fields = append(fields, s[start:])
	}
	return fields
}

// isAnimationTimingFunctionToken reports whether a token in the `animation`
// shorthand is an <easing-function>.
func isAnimationTimingFunctionToken(tok string) bool {
	t := strings.ToLower(tok)
	switch t {
	case "linear", "ease", "ease-in", "ease-out", "ease-in-out",
		"step-start", "step-end":
		return true
	}
	return strings.HasPrefix(t, "steps(") || strings.HasPrefix(t, "cubic-bezier(") ||
		strings.HasPrefix(t, "linear(")
}

// isAnimationDirectionToken reports whether a token is an <animation-direction>.
func isAnimationDirectionToken(tok string) bool {
	switch strings.ToLower(tok) {
	case "normal", "reverse", "alternate", "alternate-reverse":
		return true
	}
	return false
}

// isAnimationFillModeToken reports whether a token is an <animation-fill-mode>.
func isAnimationFillModeToken(tok string) bool {
	switch strings.ToLower(tok) {
	case "none", "forwards", "backwards", "both":
		return true
	}
	return false
}

// isAnimationPlayStateToken reports whether a token is an <animation-play-state>.
func isAnimationPlayStateToken(tok string) bool {
	switch strings.ToLower(tok) {
	case "running", "paused":
		return true
	}
	return false
}

// isTimeToken reports whether a token is a CSS <time> value.
func isTimeToken(tok string) bool {
	t := strings.ToLower(tok)
	if strings.HasSuffix(t, "ms") {
		_, err := strconv.ParseFloat(t[:len(t)-2], 64)
		return err == nil
	}
	if strings.HasSuffix(t, "s") {
		_, err := strconv.ParseFloat(t[:len(t)-1], 64)
		return err == nil
	}
	return false
}

// expandAnimationShorthand decomposes the `animation` shorthand into its 8
// longhands per CSS Animations §4 (the <single-animation># grammar). Each
// comma-separated <single-animation> contributes one entry to each longhand
// list; omitted components reset to their initial value. The first <time> in a
// <single-animation> is the duration, the second is the delay.
func expandAnimationShorthand(style *Style, value string) {
	items := splitTopLevelCommas(value)
	var names, durations, timingFns, delays, iterCounts, directions, fillModes, playStates []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		// Per-animation initial values (CSS Animations §4 / §3.x).
		name := "none"
		duration := "0s"
		timingFn := "ease"
		delay := "0s"
		iterCount := "1"
		direction := "normal"
		fillMode := "none"
		playState := "running"

		sawDuration := false
		sawTiming := false
		sawIterCount := false
		sawDirection := false
		sawFillMode := false
		sawPlayState := false
		sawName := false

		for _, tok := range splitTopLevelFields(item) {
			switch {
			case isTimeToken(tok):
				if !sawDuration {
					duration = tok
					sawDuration = true
				} else {
					delay = tok
				}
			case isAnimationTimingFunctionToken(tok):
				if !sawTiming {
					timingFn = tok
					sawTiming = true
				}
			case isAnimationDirectionToken(tok) && !sawDirection:
				// "normal" is also a fill-mode-adjacent keyword but direction
				// owns it; fill-mode "none" is distinct. Order: direction first.
				direction = tok
				sawDirection = true
			case isAnimationFillModeToken(tok) && !sawFillMode &&
				!(strings.EqualFold(tok, "none") && !sawName):
				// "none" is ambiguous: it is both a fill-mode and the
				// keyframes-name "none". Treat a lone "none" as the name (the
				// common case: `animation: anim` vs `animation: none`); only a
				// "none" appearing after the name is the fill-mode.
				fillMode = tok
				sawFillMode = true
			case isAnimationPlayStateToken(tok) && !sawPlayState:
				playState = tok
				sawPlayState = true
			case isNumberToken(tok) && !sawIterCount:
				iterCount = tok
				sawIterCount = true
			case strings.EqualFold(tok, "infinite") && !sawIterCount:
				iterCount = "infinite"
				sawIterCount = true
			default:
				// <keyframes-name> (or "none" meaning no animation).
				if !sawName {
					name = tok
					sawName = true
				}
			}
		}
		names = append(names, name)
		durations = append(durations, duration)
		timingFns = append(timingFns, timingFn)
		delays = append(delays, delay)
		iterCounts = append(iterCounts, iterCount)
		directions = append(directions, direction)
		fillModes = append(fillModes, fillMode)
		playStates = append(playStates, playState)
	}
	style.Set("animation-name", strings.Join(names, ", "))
	style.Set("animation-duration", strings.Join(durations, ", "))
	style.Set("animation-timing-function", strings.Join(timingFns, ", "))
	style.Set("animation-delay", strings.Join(delays, ", "))
	style.Set("animation-iteration-count", strings.Join(iterCounts, ", "))
	style.Set("animation-direction", strings.Join(directions, ", "))
	style.Set("animation-fill-mode", strings.Join(fillModes, ", "))
	style.Set("animation-play-state", strings.Join(playStates, ", "))
}

// isNumberToken reports whether tok is a plain number (no unit).
func isNumberToken(tok string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(tok), 64)
	return err == nil
}

// =============================================================================
// CSS Text Decoration 3 — AppliedTextDecoration model
//
// Mirrors Blink's `core/style/applied_text_decoration.{h,cc}` at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. The class fields lines_/style_/
// color_/thickness_/underline_offset_ (h:43-47) are reproduced here as
// AppliedTextDecoration.Lines/Style/Color (+HasColor)/Thickness/UnderlineOffset.
// The vector typedef `AppliedTextDecorationVector` (h:50) maps to []AppliedTextDecoration.
//
// Per CSS Text Decor 3 §2 (https://www.w3.org/TR/css-text-decor-3/#line-decoration)
// text-decoration is NOT inherited; instead, each box that establishes a text
// decoration appends a new entry to the parent's accumulated vector. A child
// cannot remove an ancestor's entry — `text-decoration-line: none` simply
// contributes nothing.
// =============================================================================

// TextDecorationLine is a bitfield of the lines that an AppliedTextDecoration
// contributes. Mirrors Blink's `TextDecorationLine` enum (kTextDecorationLineBits
// in applied_text_decoration.h:43).
type TextDecorationLine uint8

const (
	TextDecorationLineNone        TextDecorationLine = 0
	TextDecorationLineUnderline   TextDecorationLine = 1 << 0
	TextDecorationLineOverline    TextDecorationLine = 1 << 1
	TextDecorationLineLineThrough TextDecorationLine = 1 << 2
	// "blink" is a valid keyword in the syntax that has no visual effect; not
	// represented in the bitfield because Blink produces no glyphs for it
	// (computed value: keyword retained for serialization only; ignored here).
)

// Has reports whether the bitfield contains the given line bit.
func (l TextDecorationLine) Has(bit TextDecorationLine) bool { return l&bit != 0 }

// IsNone reports whether no lines are set.
func (l TextDecorationLine) IsNone() bool { return l == TextDecorationLineNone }

// TextDecorationThicknessKind enumerates the value-type of a
// text-decoration-thickness. Mirrors the auto / from-font / <length-percentage>
// branches in Blink's `TextDecorationThickness` value type.
type TextDecorationThicknessKind uint8

const (
	TextDecorationThicknessAuto     TextDecorationThicknessKind = 0
	TextDecorationThicknessFromFont TextDecorationThicknessKind = 1
	TextDecorationThicknessLength   TextDecorationThicknessKind = 2
)

// TextDecorationThickness is the resolved thickness for one AppliedTextDecoration.
// Kind=Length carries Value (pixels) when ValueIsPercent is false, or a percentage
// (0..100) when ValueIsPercent is true. Mirrors Blink's `TextDecorationThickness`
// value type (lines_/style_ siblings in applied_text_decoration.h:46).
type TextDecorationThickness struct {
	Kind           TextDecorationThicknessKind
	Value          float64
	ValueIsPercent bool
}

// TextDecorationInset is the resolved text-decoration-inset value for an
// AppliedTextDecoration. InlineStart and InlineEnd trim the inline-start and
// inline-end edges of the decoration rect (positive = shrink the line;
// negative = extend it beyond the text run). Mirrors the CSS Text Decor L4
// `text-decoration-inset` longhand (Mozilla-authored WPT tests; Blink at the
// pinned SHA has not shipped this property — semantics from the spec draft
// and WPT reference files).
type TextDecorationInset struct {
	InlineStart float64 // pixels; 0 = no inset on the start side
	InlineEnd   float64 // pixels; 0 = no inset on the end side
}

// AppliedTextDecoration is one decoration entry on the accumulated vector that
// each element carries (see Style.AppliedTextDecorations). Mirrors Blink's
// class of the same name at applied_text_decoration.h:18-48 (SHA pinned above).
//
// Fields in 1:1 correspondence with Blink:
//
//	Lines           ↔ lines_ (bitfield)         applied_text_decoration.h:43
//	Style           ↔ style_ ("solid"/"double"/"dotted"/"dashed"/"wavy")  :44
//	Color           ↔ color_                    applied_text_decoration.h:45
//	Thickness       ↔ thickness_                applied_text_decoration.h:46
//	UnderlineOffset ↔ underline_offset_         applied_text_decoration.h:47
//
// Inset has no Blink counterpart (L4 not-yet-shipped); we carry it as a
// louis14-native field. HasColor=false means "currentcolor" — the resolver
// freezes the resolved color at append-time so a descendant cannot change it.
type AppliedTextDecoration struct {
	Lines           TextDecorationLine
	Style           string // solid | double | dotted | dashed | wavy
	Color           Color  // valid only when HasColor
	HasColor        bool   // false = currentcolor at the originating element
	Thickness       TextDecorationThickness
	UnderlineOffset float64 // resolved pixels (0 = auto/initial)
	Inset           TextDecorationInset

	// Decorating-box fragment-continuity metadata (LOU-149 Phase 4). Mirrors
	// Blink's `InlinePaintContext::DecoratingBoxList` + `OffsetFromDecoratingBox`
	// (core/paint/inline_paint_context.h:20-26, text_decoration_info.cc:583-596
	// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). louis14 stores the
	// resolved per-fragment extent directly here because we don't carry a
	// paint-time context object. HasDecoratingBox=false → painter falls back
	// to fragment-local geometry (LOU-142 behavior).
	//
	// DecoratingBoxOffsetX and DecoratingBoxWidth are coordinate-system-
	// independent: the offset is expressed RELATIVE TO this text fragment's
	// own start. The painter resolves the absolute logical-start as
	// (box.X + DecoratingBoxOffsetX). This avoids the layout/paint axis
	// translation that line-box-local coords would otherwise need.
	HasDecoratingBox     bool    // false → DecoratingBoxOffsetX/Width unset; painter uses fragment-local extent
	DecoratingBoxOffsetX float64 // signed offset from this fragment's start (box.X) to the decorating box's logical-start edge; ≤ 0 (decoration starts at or before this fragment)
	DecoratingBoxWidth   float64 // total inline-content width across all fragments of this decorating box on this row
	IsFirstFragment      bool    // source-order first fragment of the decorating box on this line (matches layout.Box.IsFirstFragment)
	IsLastFragment       bool    // source-order last fragment of the decorating box on this line (matches layout.Box.IsLastFragment)
}

// GetAppliedTextDecorations returns the accumulated decoration vector for this
// element (Phase 1: populated by ResolveAppliedTextDecorations during cascade).
// Returns nil if there are no active decorations for this element.
func (s *Style) GetAppliedTextDecorations() []AppliedTextDecoration {
	return s.AppliedTextDecorations
}

// ParseTextDecorationLine parses the `text-decoration-line` value (a
// space-separated list of underline/overline/line-through/blink/none). Returns
// the bitfield. Invalid combinations (e.g. duplicate blink, unknown token) map
// to None per CSS Text Decor §2.1. The "blink" keyword is accepted as valid
// syntax but contributes no bit (it never paints).
func ParseTextDecorationLine(value string) TextDecorationLine {
	v := strings.TrimSpace(strings.ToLower(value))
	if v == "" || v == "none" {
		return TextDecorationLineNone
	}
	var result TextDecorationLine
	seen := map[string]bool{}
	for _, tok := range strings.Fields(v) {
		if seen[tok] {
			// Duplicate keyword → entire value invalid.
			return TextDecorationLineNone
		}
		seen[tok] = true
		switch tok {
		case "underline":
			result |= TextDecorationLineUnderline
		case "overline":
			result |= TextDecorationLineOverline
		case "line-through":
			result |= TextDecorationLineLineThrough
		case "blink":
			// Valid but contributes no bit.
		case "none":
			// "none" combined with any other token is invalid per spec.
			return TextDecorationLineNone
		default:
			return TextDecorationLineNone
		}
	}
	return result
}

// SerializeTextDecorationLine renders a TextDecorationLine bitfield back to
// canonical CSS text. Used so the parsed longhand can round-trip through the
// Style.Properties map (which is `map[string]string`).
func SerializeTextDecorationLine(l TextDecorationLine) string {
	if l == TextDecorationLineNone {
		return "none"
	}
	parts := make([]string, 0, 3)
	if l.Has(TextDecorationLineUnderline) {
		parts = append(parts, "underline")
	}
	if l.Has(TextDecorationLineOverline) {
		parts = append(parts, "overline")
	}
	if l.Has(TextDecorationLineLineThrough) {
		parts = append(parts, "line-through")
	}
	return strings.Join(parts, " ")
}

// GetTextDecorationLine returns the parsed text-decoration-line longhand for
// this element (default: none).
func (s *Style) GetTextDecorationLine() TextDecorationLine {
	if val, ok := s.Get("text-decoration-line"); ok {
		return ParseTextDecorationLine(val)
	}
	return TextDecorationLineNone
}

// GetTextDecorationThicknessResolved returns the parsed thickness longhand as
// a TextDecorationThickness value. Mirrors Blink's `TextDecorationThickness`
// value computation. Unlike the legacy GetTextDecorationThickness which always
// returns float64, this preserves the auto/from-font keywords and percentage
// units that Phase 2 (TextDecorationInfo) needs.
func (s *Style) GetTextDecorationThicknessResolved() TextDecorationThickness {
	val, ok := s.Get("text-decoration-thickness")
	if !ok {
		return TextDecorationThickness{Kind: TextDecorationThicknessAuto}
	}
	v := strings.TrimSpace(strings.ToLower(val))
	switch v {
	case "", "auto":
		return TextDecorationThickness{Kind: TextDecorationThicknessAuto}
	case "from-font":
		return TextDecorationThickness{Kind: TextDecorationThicknessFromFont}
	}
	// Percentage?
	if strings.HasSuffix(v, "%") {
		if pct, ok := ParsePercentage(v); ok {
			return TextDecorationThickness{
				Kind:           TextDecorationThicknessLength,
				Value:          pct,
				ValueIsPercent: true,
			}
		}
	}
	// Length or calc() — calc() with % terms resolves against font-size here.
	if l, ok := ParseLengthOrPercentageFontRelative(val, s.GetFontSize()); ok {
		return TextDecorationThickness{
			Kind:  TextDecorationThicknessLength,
			Value: l,
		}
	}
	return TextDecorationThickness{Kind: TextDecorationThicknessAuto}
}

// GetTextDecorationInset returns the resolved text-decoration-inset value
// for this element. Per CSS Text Decor L4 §"text-decoration-skip-inset-property"
// the value is `auto` or one or two `<length-percentage>`s — one value applies
// to both inline edges, two values are inline-start and inline-end. Percent
// and calc() resolve against the element's font-size. Negative values extend
// the decoration beyond the text run.
func (s *Style) GetTextDecorationInset() TextDecorationInset {
	val, ok := s.Get("text-decoration-inset")
	if !ok {
		return TextDecorationInset{}
	}
	// splitShorthandParts is paren-aware; strings.Fields would mis-split a
	// value like `calc(1em + 2px) 0` into four tokens.
	parts := splitShorthandParts(val)
	if len(parts) == 1 && strings.EqualFold(parts[0], "auto") {
		return TextDecorationInset{}
	}
	fontSize := s.GetFontSize()
	switch len(parts) {
	case 1:
		if px, ok := ParseLengthOrPercentageFontRelative(parts[0], fontSize); ok {
			return TextDecorationInset{InlineStart: px, InlineEnd: px}
		}
	case 2:
		start, ok1 := ParseLengthOrPercentageFontRelative(parts[0], fontSize)
		end, ok2 := ParseLengthOrPercentageFontRelative(parts[1], fontSize)
		if ok1 && ok2 {
			return TextDecorationInset{InlineStart: start, InlineEnd: end}
		}
	}
	return TextDecorationInset{}
}

// TextDecorationSkipSpaces is the resolved text-decoration-skip-spaces value
// for an element. SkipStart/SkipEnd indicate whether decoration painting
// should skip leading/trailing whitespace at the start/end of the line.
// Mirrors CSS Text Decor L4 §"text-decoration-skip-spaces-property".
// Blink at the pinned SHA still implements the L3 behavior (always skip at
// line edges); louis14 follows the same default so existing browser tests
// match.
type TextDecorationSkipSpaces struct {
	SkipStart bool
	SkipEnd   bool
}

// GetTextDecorationSkipSpaces returns the resolved text-decoration-skip-spaces
// value for this element. Per CSS Text Decor L4 the syntax is
// `none | all | <[ start || end || space-all ]>`. Initial per L4 is `none`,
// but louis14 (matching Blink's L3-compatible behavior) treats an unset value
// as `start end` so existing WPT tests pass without explicit overrides.
func (s *Style) GetTextDecorationSkipSpaces() TextDecorationSkipSpaces {
	val, ok := s.Get("text-decoration-skip-spaces")
	if !ok {
		// Default mirrors Blink/Firefox at SHA 4883d11f: leading/trailing
		// spaces on a line do not get an underline. WPT test stylesheets
		// for skip-spaces note explicitly: "Theoretically not needed, as
		// that's the default behavior per L3".
		return TextDecorationSkipSpaces{SkipStart: true, SkipEnd: true}
	}
	v := strings.TrimSpace(strings.ToLower(val))
	switch v {
	case "", "auto":
		return TextDecorationSkipSpaces{SkipStart: true, SkipEnd: true}
	case "none":
		return TextDecorationSkipSpaces{}
	case "all":
		return TextDecorationSkipSpaces{SkipStart: true, SkipEnd: true}
	}
	var out TextDecorationSkipSpaces
	for _, tok := range strings.Fields(v) {
		switch tok {
		case "start":
			out.SkipStart = true
		case "end":
			out.SkipEnd = true
		}
	}
	return out
}

// computeOwnTextDecorationContribution returns the element's own contribution
// to its AppliedTextDecorations vector — i.e. the `AppliedTextDecoration` that
// would be *appended* to the parent's resolved vector, if any. Returns false
// if `text-decoration-line` is none/empty (the element contributes nothing).
//
// Mirrors Blink's style-builder behavior described in the Phase 1 vetting log:
// "When a box that 'establishes' a text decoration is encountered, the style
// builder appends a new AppliedTextDecoration to the inherited vector."
func (s *Style) computeOwnTextDecorationContribution() (AppliedTextDecoration, bool) {
	line := s.GetTextDecorationLine()
	if line.IsNone() {
		return AppliedTextDecoration{}, false
	}
	td := AppliedTextDecoration{
		Lines:           line,
		Style:           s.GetTextDecorationStyle(),
		Thickness:       s.GetTextDecorationThicknessResolved(),
		UnderlineOffset: s.GetTextUnderlineOffset(),
		Inset:           s.GetTextDecorationInset(),
		// Cascade-time defaults assume a single, unfragmented inline. Layout
		// (LOU-149 Item 2) overwrites these per fragment when it knows better.
		IsFirstFragment: true,
		IsLastFragment:  true,
	}
	if c, ok := s.GetTextDecorationColor(); ok {
		td.Color = c
		td.HasColor = true
	} else {
		// `text-decoration-color: currentcolor` (the initial value) resolves
		// to THIS element's `color` at the moment the contribution is
		// recorded. Mirrors Blink's StyleBuilder, which freezes the resolved
		// color onto the AppliedTextDecoration entry at append-time so a
		// descendant cannot retroactively change the ancestor's stroke color.
		td.Color = s.GetColor()
		td.HasColor = true
	}
	return td, true
}

// ResolveAppliedTextDecorations populates s.AppliedTextDecorations by appending
// this element's own contribution (if any) to the parent's resolved vector.
// Mirrors Blink's `StyleBuilder` path where a box that establishes a text
// decoration appends to the inherited vector
// (applied_text_decoration.h:18-50, SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// The `node` argument is the DOM element this style belongs to (may be nil for
// pseudo-elements / synthetic root). It is consulted only for tag-based
// classification of replaced elements via html.IsReplacedElementTag, which is
// the single source of truth for the replaced-tag list.
//
// CSS Text Decor 4 §1.3 propagation boundaries (the "Phase 4 wiring" the
// original Phase 1 comment promised):
//
//   - Atomic inline-level boxes (CSS Display 3 §2.2: inline-block,
//     inline-flex, inline-grid, inline-table, inline list-item, AND every
//     replaced element regardless of computed display) form a new
//     formatting context that an ancestor's propagating decoration does
//     NOT cross into. Inherited vector is reset to empty before the
//     element's own contribution is appended.
//
//   - Out-of-flow descendants (float, position:absolute/fixed) leave the
//     in-flow box tree; the ancestor decoration doesn't follow them.
//     Inherited vector is reset to empty.
//
//   - display:contents elements generate NO box (the element disappears
//     from the box tree). Per spec they neither inherit propagating
//     decorations as a normal style nor contribute one to descendants —
//     the decoration on a `display:contents` element is a no-op for
//     line-decoration painting. We keep the parent's vector intact (so
//     descendants' inheritance via their own ResolveAppliedTextDecorations
//     call sees the grandparent's accumulation) and skip own contribution.
//
// Note: nested block-level descendants (e.g. a block inside an underlined
// block) DO continue to accumulate; that case has no boundary because the
// inner block is still in-flow and not atomic — verified by
// text-decoration-propagation-04 (a passing test that exercises three
// levels of block nesting with overlapping decorations).
func (s *Style) ResolveAppliedTextDecorations(parent *Style, node *html.Node) {
	var inherited []AppliedTextDecoration
	if parent != nil {
		inherited = parent.AppliedTextDecorations
	}

	// display:contents: keep inherited as-is, skip own contribution.
	// The element has no box, so it neither establishes a propagating
	// decoration nor breaks an ancestor's propagation.
	if s.GetDisplay() == DisplayContents {
		if len(inherited) == 0 {
			s.AppliedTextDecorations = nil
		} else {
			s.AppliedTextDecorations = append([]AppliedTextDecoration(nil), inherited...)
		}
		return
	}

	// Propagation boundaries: an atomic inline, replaced, or out-of-flow
	// descendant gets a fresh empty inherited vector. Its own contribution
	// (if any) still applies — atomic inlines can themselves establish a
	// decoration that propagates inside their own formatting context.
	if isPropagationBoundary(s, node) {
		inherited = nil
	}

	own, hasOwn := s.computeOwnTextDecorationContribution()
	if !hasOwn {
		// Element contributes nothing — vector is exactly the (post-reset) parent's.
		if len(inherited) == 0 {
			s.AppliedTextDecorations = nil
		} else {
			s.AppliedTextDecorations = append([]AppliedTextDecoration(nil), inherited...)
		}
		return
	}
	combined := make([]AppliedTextDecoration, 0, len(inherited)+1)
	combined = append(combined, inherited...)
	combined = append(combined, own)
	s.AppliedTextDecorations = combined
}

// isPropagationBoundary reports whether the element with style s and DOM
// node node forms a boundary that an ancestor's text-decoration does not
// cross (CSS Text Decor 4 §1.3). The boundary set is:
//
//   - atomic inline-level boxes (style.IsAtomicInlineDisplay)
//   - replaced elements (html.IsReplacedElementTag) — atomic inline-level
//     per CSS Display 3 §2.2 even when their computed display says inline
//   - out-of-flow boxes (float != none, position: absolute|fixed)
//
// display:contents is handled separately by ResolveAppliedTextDecorations
// (it is NOT a boundary — it keeps the parent vector intact).
func isPropagationBoundary(s *Style, node *html.Node) bool {
	if s.IsAtomicInlineDisplay() {
		return true
	}
	if node != nil && html.IsReplacedElementTag(node.TagName) {
		return true
	}
	switch s.GetPosition() {
	case PositionAbsolute, PositionFixed:
		return true
	}
	if s.GetFloat() != FloatNone {
		return true
	}
	return false
}
