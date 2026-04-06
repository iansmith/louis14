package css

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"louis14/pkg/html"
)

type Style struct {
	Properties      map[string]string
	ViewportWidth   float64 // Viewport width in pixels (for vw/vmin/vmax units)
	ViewportHeight  float64 // Viewport height in pixels (for vh/vmin/vmax units)
	ChWidth         float64 // Measured advance width of "0" in the element's font (0 = use heuristic)
}

func NewStyle() *Style {
	return &Style{Properties: make(map[string]string)}
}

// Clone returns a deep copy of this Style with all properties copied.
func (s *Style) Clone() *Style {
	dst := &Style{
		Properties:     make(map[string]string, len(s.Properties)),
		ViewportWidth:  s.ViewportWidth,
		ViewportHeight: s.ViewportHeight,
		ChWidth:        s.ChWidth,
	}
	for k, v := range s.Properties {
		dst.Properties[k] = v
	}
	return dst
}

func (s *Style) Get(property string) (string, bool) {
	val, ok := s.Properties[property]
	if !ok {
		return val, ok
	}
	// Resolve var() references in the value
	if strings.Contains(val, "var(") {
		val = s.resolveVarReferences(val)
	}
	// Resolve env() references in the value
	if strings.Contains(val, "env(") {
		val = resolveEnvValue(val)
	}
	return val, ok
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

		// Look up the custom property (bypass Get to avoid recursion)
		resolved := ""
		if propVal, ok := s.Properties[varName]; ok {
			resolved = propVal
		} else if fallback != "" {
			resolved = fallback
		}

		// Replace var(...) with the resolved value
		value = value[:idx] + resolved + value[end+1:]
	}
	return value
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
	s.Properties[property] = value
}

func (s *Style) GetLength(property string) (float64, bool) {
	val, ok := s.Get(property)
	if !ok {
		return 0, false
	}
	return parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale())
}

// chScale returns the ch unit multiplier relative to fontSize for this style's font.
// CSS Values §6.1: ch is the advance measure of "0" in the element's font.
// In horizontal modes, this is the horizontal advance width (measured by ChWidth).
// In vertical modes, this is the vertical advance height (≈1em for most fonts).
func (s *Style) chScale() float64 {
	wm, _ := s.Get("writing-mode")
	if wm == "vertical-rl" || wm == "vertical-lr" ||
		wm == "sideways-rl" || wm == "sideways-lr" {
		// Vertical modes: ch = vertical advance height of "0" ≈ 1em
		return 1.0
	}
	// Horizontal mode: use measured horizontal advance width if available
	if s.ChWidth > 0 {
		fs := s.GetFontSize()
		if fs > 0 {
			return s.ChWidth / fs
		}
	}
	return 0.5
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
	return parseLengthFullWithCh(val, fontSize, viewportWidth, viewportHeight, 0.5)
}

// parseLengthFullWithCh is the internal implementation that accepts a custom ch multiplier.
// chScale is the multiplier for the ch unit relative to fontSize (0.5 for horizontal, 1.0 for vertical).
func parseLengthFullWithCh(val string, fontSize, viewportWidth, viewportHeight, chScale float64) (float64, bool) {
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
	// Handle calc() expressions — pass full context so ch/vw/vh units resolve correctly.
	if strings.HasPrefix(val, "calc(") && strings.HasSuffix(val, ")") {
		expr := val[5 : len(val)-1] // strip "calc(" and ")"
		ctx := calcContext{
			fontSize:       fontSize,
			viewportWidth:  viewportWidth,
			viewportHeight: viewportHeight,
			chScale:        chScale,
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
	// ex unit: x-height of the current font, approximately 0.5em.
	if strings.HasSuffix(val, "ex") {
		numStr := strings.TrimSuffix(val, "ex")
		num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
		if err != nil {
			return 0, false
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
	// Handle percentage values: resolve against percentBase
	if strings.HasSuffix(token, "%") && ctx.percentBase > 0 {
		numStr := strings.TrimSuffix(token, "%")
		if num, err := strconv.ParseFloat(numStr, 64); err == nil {
			return calcResult{value: num * ctx.percentBase / 100, pos: pos + 1}, true
		}
	}
	// Parse as a length value using full context (viewport, ch scale)
	val, ok := parseLengthFullWithCh(token, ctx.fontSize, ctx.viewportWidth, ctx.viewportHeight, ctx.chScale)
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
	Top       float64
	Right     float64
	Bottom    float64
	Left      float64
	AutoTop   bool // True if margin-top: auto
	AutoRight bool // True if margin-right: auto
	AutoBottom bool // True if margin-bottom: auto
	AutoLeft  bool // True if margin-left: auto
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
	var corners BorderRadiusCorners

	// Check individual corners first (higher specificity)
	// parseBorderRadiusValue handles "75px 50px" two-value syntax (returns first value)
	if r := s.parseBorderRadiusFirst("border-top-left-radius"); r > 0 {
		corners.TopLeft = r
	}
	if r := s.parseBorderRadiusFirst("border-top-right-radius"); r > 0 {
		corners.TopRight = r
	}
	if r := s.parseBorderRadiusFirst("border-bottom-right-radius"); r > 0 {
		corners.BottomRight = r
	}
	if r := s.parseBorderRadiusFirst("border-bottom-left-radius"); r > 0 {
		corners.BottomLeft = r
	}

	// If any individual corner was set, return those values
	if corners.TopLeft > 0 || corners.TopRight > 0 || corners.BottomRight > 0 || corners.BottomLeft > 0 {
		return corners
	}

	// Fall back to shorthand border-radius (already expanded by expandShorthand)
	if r, ok := s.GetLength("border-radius"); ok {
		return BorderRadiusCorners{r, r, r, r}
	}

	return corners // all zeros
}

// parseBorderRadiusFirst parses a border-radius value, returning the first (horizontal) radius.
// Handles both single value "10px" and two-value "75px 50px" syntax.
func (s *Style) parseBorderRadiusFirst(property string) float64 {
	val, ok := s.Get(property)
	if !ok {
		return 0
	}
	// Try as single value first
	if r, ok := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale()); ok {
		return r
	}
	// Try two-value syntax: "75px 50px"
	parts := strings.Fields(val)
	if len(parts) >= 1 {
		if r, ok := parseLengthFullWithCh(parts[0], s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale()); ok {
			return r
		}
	}
	return 0
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
	if r, ok := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale()); ok {
		return EllipticalRadius{r, r}
	}
	// Two-value syntax: "75px 50px"
	parts := strings.Fields(val)
	var result EllipticalRadius
	if len(parts) >= 1 {
		if r, ok := parseLengthFullWithCh(parts[0], s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale()); ok {
			result.Rx = r
			result.Ry = r // default same
		}
	}
	if len(parts) >= 2 {
		if r, ok := parseLengthFullWithCh(parts[1], s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale()); ok {
			result.Ry = r
		}
	}
	return result
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
	Top    float64
	Right  float64
	Bottom float64
	Left   float64
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
	if length, ok := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale()); ok {
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

func ParseInlineStyle(styleAttr string) *Style {
	style := NewStyle()
	declarations := strings.Split(styleAttr, ";")
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
		// columns shorthand: <column-width> <column-count> | auto
		parts := strings.Fields(value)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "auto" {
				continue
			}
			if _, err := strconv.Atoi(part); err == nil {
				style.Set("column-count", part)
			} else {
				style.Set("column-width", part)
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
		expandBorderSideProperty(style, "border-left", value)
		expandBorderSideProperty(style, "border-right", value)
		storeBorderLogicalMarker(style, "_border-inline-start", value)
		storeBorderLogicalMarker(style, "_border-inline-end", value)
	case "border-block":
		expandBorderSideProperty(style, "border-top", value)
		expandBorderSideProperty(style, "border-bottom", value)
		storeBorderLogicalMarker(style, "_border-block-start", value)
		storeBorderLogicalMarker(style, "_border-block-end", value)
	case "border-inline-start":
		expandBorderSideProperty(style, "border-left", value)
		storeBorderLogicalMarker(style, "_border-inline-start", value)
	case "border-inline-end":
		expandBorderSideProperty(style, "border-right", value)
		storeBorderLogicalMarker(style, "_border-inline-end", value)
	case "border-block-start":
		expandBorderSideProperty(style, "border-top", value)
		storeBorderLogicalMarker(style, "_border-block-start", value)
	case "border-block-end":
		expandBorderSideProperty(style, "border-bottom", value)
		storeBorderLogicalMarker(style, "_border-block-end", value)
	case "border-inline-start-width":
		style.Set("border-left-width", value)
		style.Set("_border-inline-start-width", value)
	case "border-inline-end-width":
		style.Set("border-right-width", value)
		style.Set("_border-inline-end-width", value)
	case "border-block-start-width":
		style.Set("border-top-width", value)
		style.Set("_border-block-start-width", value)
	case "border-block-end-width":
		style.Set("border-bottom-width", value)
		style.Set("_border-block-end-width", value)
	case "border-inline-start-style":
		style.Set("border-left-style", value)
		style.Set("_border-inline-start-style", value)
	case "border-inline-end-style":
		style.Set("border-right-style", value)
		style.Set("_border-inline-end-style", value)
	case "border-block-start-style":
		style.Set("border-top-style", value)
		style.Set("_border-block-start-style", value)
	case "border-block-end-style":
		style.Set("border-bottom-style", value)
		style.Set("_border-block-end-style", value)
	case "border-inline-start-color":
		style.Set("border-left-color", value)
		style.Set("_border-inline-start-color", value)
	case "border-inline-end-color":
		style.Set("border-right-color", value)
		style.Set("_border-inline-end-color", value)
	case "border-block-start-color":
		style.Set("border-top-color", value)
		style.Set("_border-block-start-color", value)
	case "border-block-end-color":
		style.Set("border-bottom-color", value)
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
		// Store animation value as-is; static renderer shows initial (t=0) state
		style.Set("animation", value)
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
	default:
		// Regular property
		style.Set(property, value)
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

// GetColumnRuleWidth returns the column-rule-width in pixels
func (s *Style) GetColumnRuleWidth() float64 {
	if v, ok := s.Get("column-rule-width"); ok {
		v = strings.TrimSpace(v)
		if px, ok2 := ParseLength(v); ok2 {
			return px
		}
		switch v {
		case "thin":
			return 1
		case "medium":
			return 3
		case "thick":
			return 5
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
		if px, ok2 := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale()); ok2 {
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
	// Ignore elliptical radii (slash syntax) for now
	if strings.Contains(value, "/") {
		// Just use the first (horizontal) set
		value = strings.TrimSpace(strings.SplitN(value, "/", 2)[0])
	}

	parts := strings.Fields(value)
	switch len(parts) {
	case 1:
		// All corners the same — store as shorthand for backward compat
		style.Set("border-radius", parts[0])
	case 2:
		// TL+BR, TR+BL
		style.Set("border-top-left-radius", parts[0])
		style.Set("border-bottom-right-radius", parts[0])
		style.Set("border-top-right-radius", parts[1])
		style.Set("border-bottom-left-radius", parts[1])
	case 3:
		// TL, TR+BL, BR
		style.Set("border-top-left-radius", parts[0])
		style.Set("border-top-right-radius", parts[1])
		style.Set("border-bottom-left-radius", parts[1])
		style.Set("border-bottom-right-radius", parts[2])
	case 4:
		// TL, TR, BR, BL
		style.Set("border-top-left-radius", parts[0])
		style.Set("border-top-right-radius", parts[1])
		style.Set("border-bottom-right-radius", parts[2])
		style.Set("border-bottom-left-radius", parts[3])
	}
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
//           "10px 20px 30px" (top h bottom), "10px 20px 30px 40px" (t r b l)
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

	// Now apply the specified values
	parts := strings.Fields(value)
	for _, part := range parts {
		if bw, ok := borderWidthKeyword(part); ok {
			style.Set("border-width", bw)
			style.Set("border-top-width", bw)
			style.Set("border-right-width", bw)
			style.Set("border-bottom-width", bw)
			style.Set("border-left-width", bw)
		} else if _, ok := ParseLength(part); ok {
			// Width (px, em, mm, or bare number)
			style.Set("border-width", part)
			style.Set("border-top-width", part)
			style.Set("border-right-width", part)
			style.Set("border-bottom-width", part)
			style.Set("border-left-width", part)
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" || part == "none" || part == "inset" || part == "outset" || part == "groove" || part == "ridge" {
			// Style
			style.Set("border-style", part)
			style.Set("border-top-style", part)
			style.Set("border-right-style", part)
			style.Set("border-bottom-style", part)
			style.Set("border-left-style", part)
		} else {
			// Color
			style.Set("border-color", part)
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
// It extracts url(...), color, no-repeat, and position components.
func expandBackgroundProperty(style *Style, value string) {
	// If value contains var(), defer expansion — store as background-color
	// (var() will be resolved later when the property is read)
	if strings.Contains(value, "var(") {
		style.Set("background-color", value)
		return
	}

	// Handle "none" - resets background
	trimmed := strings.TrimSpace(value)
	if trimmed == "none" {
		style.Set("background-color", "transparent")
		style.Set("background-image", "none")
		return
	}

	// Handle image-set() / -webkit-image-set() — extract the first URL candidate
	// and treat it as a plain url() reference. Must happen before the url() check
	// since image-set() contains url() inside it.
	lowerValue := strings.ToLower(value)
	if strings.Contains(lowerValue, "image-set(") {
		// Find the image-set( span (possibly prefixed with -webkit-)
		setIdx := strings.Index(lowerValue, "image-set(")
		// Walk backwards to include an optional "-webkit-" prefix
		prefixStart := setIdx
		if setIdx >= 8 && lowerValue[setIdx-8:setIdx] == "-webkit-" {
			prefixStart = setIdx - 8
		}
		// Find matching close paren for image-set(
		depth := 1
		setEnd := setIdx + len("image-set(")
		for setEnd < len(value) && depth > 0 {
			switch value[setEnd] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth > 0 {
				setEnd++
			}
		}
		setEnd++ // include the closing ')'
		// Extract the first URL from the image-set
		imageSetExpr := value[prefixStart:setEnd]
		if url, ok := extractImageSetFirstURL(imageSetExpr); ok {
			style.Set("background-image", "url(\""+url+"\")")
			// Remove image-set(...) from value so remaining tokens can be parsed
			value = value[:prefixStart] + value[setEnd:]
		}
		// Fall through to parse remaining tokens (repeat, position, color)
	}

	// Extract url(...) first since it may contain spaces (e.g. data URIs).
	// Must happen before gradient check, as background can have both url() and gradient
	// in comma-separated layers (e.g., "url(img.svg), linear-gradient(...)").
	urlStart := strings.Index(value, "url(")
	if urlStart >= 0 {
		// Find matching closing paren, accounting for nested parens
		depth := 0
		urlEnd := -1
		for i := urlStart + 4; i < len(value); i++ {
			if value[i] == '(' {
				depth++
			} else if value[i] == ')' {
				if depth == 0 {
					urlEnd = i + 1
					break
				}
				depth--
			}
		}
		if urlEnd > urlStart {
			urlPart := value[urlStart:urlEnd]
			style.Set("background-image", urlPart)
			// Remove url(...) from value to parse remaining parts
			value = value[:urlStart] + value[urlEnd:]
		}
	}

	// Check for gradient functions in the remaining value (after URL extraction)
	for _, gradPrefix := range []string{"repeating-linear-gradient(", "repeating-radial-gradient(", "conic-gradient(", "linear-gradient(", "radial-gradient("} {
		if idx := strings.Index(value, gradPrefix); idx >= 0 {
			// Extract the gradient function with balanced parens
			depth := 0
			gradEnd := -1
			for i := idx + len(gradPrefix) - 1; i < len(value); i++ {
				if value[i] == '(' {
					depth++
				} else if value[i] == ')' {
					depth--
					if depth == 0 {
						gradEnd = i + 1
						break
					}
				}
			}
			if gradEnd > idx {
				gradientPart := value[idx:gradEnd]
				style.Set("background-image", gradientPart)
				// Parse remaining tokens (before and after gradient) for repeat/position
				remaining := strings.TrimSpace(value[:idx] + value[gradEnd:])
				for _, token := range strings.Fields(remaining) {
					if token == "no-repeat" || token == "repeat" || token == "repeat-x" || token == "repeat-y" {
						style.Set("background-repeat", token)
					} else if token == "center" || token == "left" || token == "right" || token == "top" || token == "bottom" {
						if prev, ok := style.Get("background-position"); ok {
							style.Set("background-position", prev+" "+token)
						} else {
							style.Set("background-position", token)
						}
					}
				}
			} else {
				// Fallback: store whole value
				style.Set("background", value)
			}
			return
		}
	}

	// Extract rgb()/rgba()/hsl()/hsla() color functions before field-splitting,
	// since they may contain spaces (e.g., "rgb(153, 153, 255)").
	colorFound := false
	colorValue := ""
	for _, prefix := range []string{"rgba(", "rgb(", "hsla(", "hsl(", "oklch(", "lch(", "hwb(", "color-mix(", "color("} {
		if idx := strings.Index(value, prefix); idx >= 0 {
			// Find matching closing paren (depth-aware for nested parens in color-mix)
			depth := 0
			end := -1
			for j := idx; j < len(value); j++ {
				switch value[j] {
				case 40:
					depth++
				case 41:
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
				colorFunc := value[idx : end+1]
				if _, ok := ParseColor(colorFunc); ok {
					colorFound = true
					colorValue = colorFunc
					// Remove from value for remaining parsing
					value = value[:idx] + value[end+1:]
				}
			}
			break
		}
	}

	// Parse remaining tokens for color, repeat, position
	parts := strings.Fields(value)
	positionParts := []string{}
	// CSS spec: in the background shorthand, box values (border-box/padding-box/content-box)
	// appear as: [<box> || <box>] where first applies to background-origin and second to
	// background-clip. If only one is given, it applies to both.
	boxValues := []string{}
	for _, part := range parts {
		if part == "no-repeat" || part == "repeat" || part == "repeat-x" || part == "repeat-y" {
			style.Set("background-repeat", part)
		} else if part == "border-box" || part == "padding-box" || part == "content-box" {
			boxValues = append(boxValues, part)
		} else if _, ok := ParseColor(part); ok {
			if colorFound {
				// Two color values = invalid declaration, skip entirely
				return
			}
			colorFound = true
			colorValue = part
		} else if part == "transparent" {
			if colorFound {
				return
			}
			colorFound = true
			colorValue = "transparent"
		} else if _, ok := ParseLength(part); ok {
			positionParts = append(positionParts, part)
		} else if part == "center" || part == "left" || part == "right" || part == "top" || part == "bottom" {
			positionParts = append(positionParts, part)
		} else if part == "fixed" || part == "scroll" || part == "local" {
			style.Set("background-attachment", part)
		}
	}
	// Apply box values: first is background-origin, second (if present) is background-clip.
	// If only one value given, it applies to both origin and clip.
	if len(boxValues) >= 1 {
		style.Set("background-origin", boxValues[0])
		if len(boxValues) >= 2 {
			style.Set("background-clip", boxValues[1])
		} else {
			style.Set("background-clip", boxValues[0])
		}
	}
	if colorFound {
		style.Set("background-color", colorValue)
	}
	if len(positionParts) > 0 {
		style.Set("background-position", strings.Join(positionParts, " "))
	}
}

// Phase 19: Enhanced color with alpha channel
type Color struct {
	R, G, B uint8
	A       float64 // Alpha: 0.0 (transparent) to 1.0 (opaque), default 1.0
}

// parseSpaceSeparatedColorArgs parses space-separated color function arguments
// with optional slash for alpha: "L C H" or "L C H / alpha"
// Returns the parts before the slash and the alpha value (default 1.0).
func parseSpaceSeparatedColorArgs(inner string) (parts []string, alpha float64) {
	alpha = 1.0
	// Handle the slash separator for alpha
	if idx := strings.Index(inner, "/"); idx >= 0 {
		alphaPart := strings.TrimSpace(inner[idx+1:])
		var a float64
		n, _ := fmt.Sscanf(alphaPart, "%f", &a)
		if n == 1 {
			if strings.HasSuffix(strings.TrimSpace(alphaPart), "%") {
				a /= 100.0
			}
			alpha = a
		}
		inner = inner[:idx]
	}
	parts = strings.Fields(inner)
	return
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

// parseHueDegrees parses a hue value in degrees.
// Accepts plain numbers (degrees), "Ndeg", "Nrad", "Nturn".
func parseHueDegrees(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "deg") {
		var v float64
		fmt.Sscanf(strings.TrimSuffix(s, "deg"), "%f", &v)
		return v
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

	// Handle rgb() format (3-arg and 4-arg)
	if strings.HasPrefix(colorStr, "rgb(") && strings.HasSuffix(colorStr, ")") {
		values := strings.TrimSuffix(strings.TrimPrefix(colorStr, "rgb("), ")")
		parts := strings.Split(values, ",")
		if len(parts) == 3 {
			var r, g, b int
			fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &r)
			fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &g)
			fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &b)
			if r >= 0 && r <= 255 && g >= 0 && g <= 255 && b >= 0 && b <= 255 {
				return Color{uint8(r), uint8(g), uint8(b), 1.0}, true
			}
		} else if len(parts) == 4 {
			// CSS Color Level 4: rgb() with 4 arguments (alpha)
			var r, g, b int
			var a float64
			fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &r)
			fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &g)
			fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &b)
			fmt.Sscanf(strings.TrimSpace(parts[3]), "%f", &a)
			if r >= 0 && r <= 255 && g >= 0 && g <= 255 && b >= 0 && b <= 255 {
				return Color{uint8(r), uint8(g), uint8(b), a}, true
			}
		}
	}

	// Handle rgba() format
	if strings.HasPrefix(colorStr, "rgba(") && strings.HasSuffix(colorStr, ")") {
		values := strings.TrimSuffix(strings.TrimPrefix(colorStr, "rgba("), ")")
		parts := strings.Split(values, ",")
		if len(parts) == 4 {
			var r, g, b int
			var a float64
			fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &r)
			fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &g)
			fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &b)
			fmt.Sscanf(strings.TrimSpace(parts[3]), "%f", &a)
			if r >= 0 && r <= 255 && g >= 0 && g <= 255 && b >= 0 && b <= 255 {
				return Color{uint8(r), uint8(g), uint8(b), a}, true
			}
		}
	}

	// Handle hsl()/hsla() format
	if (strings.HasPrefix(colorStr, "hsl(") || strings.HasPrefix(colorStr, "hsla(")) && strings.HasSuffix(colorStr, ")") {
		inner := colorStr
		if strings.HasPrefix(inner, "hsla(") {
			inner = strings.TrimSuffix(strings.TrimPrefix(inner, "hsla("), ")")
		} else {
			inner = strings.TrimSuffix(strings.TrimPrefix(inner, "hsl("), ")")
		}
		parts := strings.Split(inner, ",")
		if len(parts) >= 3 {
			var h float64
			fmt.Sscanf(strings.TrimSpace(parts[0]), "%f", &h)
			sStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), "%"))
			lStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[2]), "%"))
			var s, l float64
			fmt.Sscanf(sStr, "%f", &s)
			fmt.Sscanf(lStr, "%f", &l)
			s /= 100.0
			l /= 100.0
			a := 1.0
			if len(parts) == 4 {
				fmt.Sscanf(strings.TrimSpace(parts[3]), "%f", &a)
			}
			// HSL to RGB conversion
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
		"red":     {255, 0, 0, 1.0},
		"green":   {0, 128, 0, 1.0},
		"blue":    {0, 0, 255, 1.0},
		"yellow":  {255, 255, 0, 1.0},
		"cyan":    {0, 255, 255, 1.0},
		"aqua":    {0, 255, 255, 1.0},
		"magenta": {255, 0, 255, 1.0},
		"fuchsia": {255, 0, 255, 1.0},
		"white":   {255, 255, 255, 1.0},
		"black":   {0, 0, 0, 1.0},
		"gray":    {128, 128, 128, 1.0},
		"grey":    {128, 128, 128, 1.0},
		"orange":  {255, 165, 0, 1.0},
		"purple":  {128, 0, 128, 1.0},
		"pink":    {255, 192, 203, 1.0},
		"brown":   {165, 42, 42, 1.0},
		"lime":    {0, 255, 0, 1.0},
		"navy":    {0, 0, 128, 1.0},
		"teal":    {0, 128, 128, 1.0},
		"silver":  {192, 192, 192, 1.0},
		"maroon":  {128, 0, 0, 1.0},
		"olive":      {128, 128, 0, 1.0},
		"lightblue":  {173, 216, 230, 1.0},
		"lightgreen": {144, 238, 144, 1.0},
		"lightgray":  {211, 211, 211, 1.0},
		"lightgrey":  {211, 211, 211, 1.0},
		"lightyellow": {255, 255, 224, 1.0},
		"lightcoral":  {240, 128, 128, 1.0},
		"lightcyan":   {224, 255, 255, 1.0},
		"lightpink":   {255, 182, 193, 1.0},
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
	color, ok := namedColors[colorStr]
	return color, ok
}

// Phase 6: Text rendering helpers

// GetFontSize returns the font-size in pixels (default: 16px)
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

// GetFontSynthesis returns the font-synthesis value as a struct indicating
// which synthesis types are allowed.
// Default allows weight and style synthesis (but not small-caps).
func (s *Style) GetFontSynthesis() struct{ Weight, Style, SmallCaps bool } {
	v, _ := s.Get("font-synthesis")
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" || v == "weight style small-caps" || v == "weight style" {
		return struct{ Weight, Style, SmallCaps bool }{true, true, false}
	}
	if v == "none" {
		return struct{ Weight, Style, SmallCaps bool }{false, false, false}
	}
	result := struct{ Weight, Style, SmallCaps bool }{}
	if strings.Contains(v, "weight") {
		result.Weight = true
	}
	if strings.Contains(v, "style") {
		result.Style = true
	}
	if strings.Contains(v, "small-caps") {
		result.SmallCaps = true
	}
	return result
}

// GetFontSizeAdjust returns the font-size-adjust value, or -1 if "none" or unset.
// font-size-adjust preserves the aspect ratio (x-height/font-size) relative to a
// reference font. Parsed for compatibility; full effect requires per-font x-height data.
func (s *Style) GetFontSizeAdjust() float64 {
	v, _ := s.Get("font-size-adjust")
	v = strings.TrimSpace(v)
	if v == "" || v == "none" {
		return -1
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return -1
	}
	return f
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

// GetTextUnderlineOffset returns the text-underline-offset value in pixels (default: 0)
func (s *Style) GetTextUnderlineOffset() float64 {
	if val, ok := s.Get("text-underline-offset"); ok {
		if l, ok := ParseLengthWithFontSize(val, s.GetFontSize()); ok {
			return l
		}
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
	OffsetX float64
	OffsetY float64
	Blur    float64
	Spread  float64
	Color   Color
	Inset   bool
}

// GetBoxShadow parses and returns box-shadow values
func (s *Style) GetBoxShadow() []BoxShadow {
	shadowStr, ok := s.Get("box-shadow")
	if !ok || shadowStr == "none" {
		return nil
	}

	// Parse box-shadow: offsetX offsetY blur spread color
	// Example: "2px 2px 5px 0px rgba(0,0,0,0.3)"
	shadows := make([]BoxShadow, 0)

	// Split by comma for multiple shadows
	parts := strings.Split(shadowStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		shadow := parseBoxShadowValue(part)
		if shadow != nil {
			shadows = append(shadows, *shadow)
		}
	}

	return shadows
}

// parseBoxShadowValue parses a single box-shadow value
func parseBoxShadowValue(s string) *BoxShadow {
	s = strings.TrimSpace(s)
	tokens := strings.Fields(s)

	if len(tokens) < 2 {
		return nil
	}

	shadow := &BoxShadow{
		Color: Color{0, 0, 0, 1.0}, // Default: currentcolor (approximated as black)
	}

	tokenIndex := 0

	// Check for 'inset'
	if tokens[tokenIndex] == "inset" {
		shadow.Inset = true
		tokenIndex++
	}

	// Parse offset-x
	if tokenIndex < len(tokens) {
		if val, ok := ParseLength(tokens[tokenIndex]); ok {
			shadow.OffsetX = val
			tokenIndex++
		}
	}

	// Parse offset-y
	if tokenIndex < len(tokens) {
		if val, ok := ParseLength(tokens[tokenIndex]); ok {
			shadow.OffsetY = val
			tokenIndex++
		}
	}

	// Parse blur radius (optional)
	if tokenIndex < len(tokens) && !isColor(tokens[tokenIndex]) {
		if val, ok := ParseLength(tokens[tokenIndex]); ok {
			shadow.Blur = val
			tokenIndex++
		}
	}

	// Parse spread radius (optional)
	if tokenIndex < len(tokens) && !isColor(tokens[tokenIndex]) {
		if val, ok := ParseLength(tokens[tokenIndex]); ok {
			shadow.Spread = val
			tokenIndex++
		}
	}

	// Parse color (rest of the string)
	if tokenIndex < len(tokens) {
		colorStr := strings.Join(tokens[tokenIndex:], " ")
		if color, ok := ParseColor(colorStr); ok {
			shadow.Color = color
		}
	}

	return shadow
}

// isColor checks if a token might be a color value
func isColor(s string) bool {
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "rgb") || strings.HasPrefix(s, "hsl") ||
		strings.HasPrefix(s, "oklch(") || strings.HasPrefix(s, "lch(") ||
		strings.HasPrefix(s, "hwb(") || strings.HasPrefix(s, "color-mix(") ||
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
	DisplayBlock           DisplayType = "block"
	DisplayInline          DisplayType = "inline"
	DisplayInlineBlock     DisplayType = "inline-block"
	DisplayNone            DisplayType = "none"
	DisplayTable           DisplayType = "table"
	DisplayTableRow        DisplayType = "table-row"
	DisplayTableCell       DisplayType = "table-cell"
	DisplayTableHeaderGroup DisplayType = "table-header-group"
	DisplayTableRowGroup   DisplayType = "table-row-group"
	DisplayTableFooterGroup DisplayType = "table-footer-group"
	DisplayListItem        DisplayType = "list-item" // Phase 23
	DisplayFlex            DisplayType = "flex"
	DisplayInlineFlex      DisplayType = "inline-flex"
	DisplayGrid            DisplayType = "grid"
	DisplayInlineGrid      DisplayType = "inline-grid"
	DisplayContents        DisplayType = "contents"
	DisplayTableCaption    DisplayType = "table-caption"
	DisplayFlowRoot        DisplayType = "flow-root"
	DisplayRuby            DisplayType = "ruby"
	DisplayRubyText        DisplayType = "ruby-text"
	DisplayRubyBase        DisplayType = "ruby-base"
	DisplayInlineTable     DisplayType = "inline-table"
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

// GetBoxSizing returns the box-sizing value (default: content-box)
func (s *Style) GetBoxSizing() string {
	if val, ok := s.Get("box-sizing"); ok {
		return val
	}
	return "content-box"
}

// GetDisplay returns the display value (default: block)
func (s *Style) GetDisplay() DisplayType {
	if display, ok := s.Get("display"); ok {
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
		case "ruby":
			return DisplayRuby
		case "ruby-text":
			return DisplayRubyText
		case "ruby-base":
			return DisplayRubyBase
		case "inline-table":
			return DisplayInlineTable
		}
	}
	return DisplayBlock
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
func (s *Style) GetLineHeight() float64 {
	val, ok := s.Get("line-height")
	if !ok {
		return s.GetFontSize() * 1.2
	}
	// Try as a standard CSS length first (px, em, etc.)
	// Use writing-mode-aware ch scale for vertical writing modes.
	if lh, ok := parseLengthFullWithCh(val, s.GetFontSize(), s.ViewportWidth, s.ViewportHeight, s.chScale()); ok {
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

// GetColumnGapMulticol returns the column-gap for multicol layout (default: 1em)
func (s *Style) GetColumnGapMulticol() float64 {
	if val, ok := s.Get("column-gap"); ok {
		val = strings.TrimSpace(val)
		if val == "normal" {
			return s.GetFontSize()
		}
		if w, ok := ParseLength(val); ok {
			return w
		}
	}
	return s.GetFontSize() // multicol default is "normal" = 1em
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
	AlignItemsNormal       AlignItems = "normal"  // initial value; resolves to stretch in flex context
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
	AlignContentNormal       AlignContent = "normal"  // initial value; resolves to stretch in flex context
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
	if length, ok := ParseLengthWithFontSize(basis, s.GetFontSize()); ok {
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
		if length, ok := ParseLengthWithFontSize(basis, s.GetFontSize()); ok {
			return length
		}
	}
	return -1
}

// AlignSelf represents the align-self property value
type AlignSelf string

const (
	AlignSelfAuto         AlignSelf = "auto"    // initial value — use container's align-items
	AlignSelfNormal       AlignSelf = "normal"  // like auto for flex items
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
	Type  string // "text", "url", "counter", "attr", "open-quote", "close-quote"
	Value string // The actual value (text content, URL path, counter name, attr name)
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

	return ParseContentValues(raw), true
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

		// Check for function-style values: counter(), url(), attr()
		// First check if the raw string starts with a known function name followed by (
		funcIdx := -1
		funcName := ""
		for _, fn := range []string{"counter", "url", "attr", "counters"} {
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
					values = append(values, ContentValue{Type: "counter", Value: arg})
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

// Phase 15: CSS Grid properties

// GridTrack represents a single grid track (column or row)
type GridTrack struct {
	Size           float64     // Size in pixels (0 for auto)
	Auto           bool        // true if track is auto-sized
	Fr             float64     // fractional unit value (0 if not fr)
	Percent        float64     // percentage value (e.g., 75 for "75%")
	IsMinMax       bool        // true if this is a minmax() track
	MinSize        float64     // minimum size (px) for minmax()
	MaxFr          float64     // maximum as fr value for minmax()
	MaxSize        float64     // maximum as fixed size (px) for minmax()
	MaxAuto        bool        // true if max is "auto" for minmax()
	MinContent     bool        // true if value is "min-content"
	MaxContent     bool        // true if value is "max-content"
	IsFitContent   bool        // true if this is a fit-content() track
	FitContentMax  float64     // argument to fit-content() in pixels
	AutoFill       bool        // true if this is a repeat(auto-fill, ...) sentinel
	AutoFit        bool        // true if this is a repeat(auto-fit, ...) sentinel
	AutoTemplate   []GridTrack // template tracks for auto-fill/auto-fit
	IsSubgrid      bool        // true if this represents a "subgrid" keyword
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
	Start   int  // Starting line (1-indexed), 0 if auto
	End     int  // Ending line (1-indexed, exclusive), 0 if auto
	IsSpan  bool // true if this is a span-only placement (no explicit start)
	SpanCount int // number of tracks to span (used when IsSpan=true or end is "span N")
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

// Transform represents a CSS transform
type Transform struct {
	Type   string    // "translate", "rotate", "scale", "skew"
	Values []float64 // Parameter values
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
		values := make([]float64, 0)
		for _, part := range parts {
			if val := parseTransformValue(strings.TrimSpace(part)); val != nil {
				values = append(values, *val)
			}
		}
		if len(values) == 1 {
			values = append(values, 0) // y defaults to 0
		}
		if len(values) >= 2 {
			return &Transform{Type: "translate", Values: values[:2]}
		}
		
	case "translateX":
		if val := parseTransformValue(args); val != nil {
			return &Transform{Type: "translate", Values: []float64{*val, 0}}
		}
		
	case "translateY":
		if val := parseTransformValue(args); val != nil {
			return &Transform{Type: "translate", Values: []float64{0, *val}}
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

// parseTransformValue parses a length value that might be pixels or percentage
func parseTransformValue(val string) *float64 {
	val = strings.TrimSpace(val)
	
	// Check for percentage
	if strings.HasSuffix(val, "%") {
		percentStr := strings.TrimSuffix(val, "%")
		if percent, err := strconv.ParseFloat(percentStr, 64); err == nil {
			// Return negative value to indicate percentage (will be resolved later with element size)
			result := -percent // Negative indicates percentage
			return &result
		}
	}
	
	// Check for px or unitless
	val = strings.TrimSuffix(val, "px")
	if length, err := strconv.ParseFloat(val, 64); err == nil {
		return &length
	}
	
	return nil
}

// parseAngle parses an angle value (deg, rad, turn)
func parseAngle(val string) *float64 {
	val = strings.TrimSpace(val)
	
	// Degrees
	if strings.HasSuffix(val, "deg") {
		degStr := strings.TrimSuffix(val, "deg")
		if deg, err := strconv.ParseFloat(degStr, 64); err == nil {
			return &deg
		}
	}
	
	// Radians
	if strings.HasSuffix(val, "rad") {
		radStr := strings.TrimSuffix(val, "rad")
		if rad, err := strconv.ParseFloat(radStr, 64); err == nil {
			deg := rad * 180 / 3.14159265359
			return &deg
		}
	}
	
	// Turns
	if strings.HasSuffix(val, "turn") {
		turnStr := strings.TrimSuffix(val, "turn")
		if turn, err := strconv.ParseFloat(turnStr, 64); err == nil {
			deg := turn * 360
			return &deg
		}
	}
	
	return nil
}

// TransformOrigin represents the transform-origin property
type TransformOrigin struct {
	X float64 // 0.0 = left, 0.5 = center, 1.0 = right
	Y float64 // 0.0 = top, 0.5 = center, 1.0 = bottom
}

// GetTransformOrigin parses transform-origin (default: center center = 50% 50%)
func (s *Style) GetTransformOrigin() TransformOrigin {
	if val, ok := s.Get("transform-origin"); ok {
		parts := strings.Fields(val)
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

// parseOriginValue parses a single origin value (left/center/right/top/bottom or percentage)
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

// GetIndividualTranslate returns the CSS `translate` individual transform property as (tx, ty, hasValue).
// A single value sets the X translation only.
func (s *Style) GetIndividualTranslate() (float64, float64, bool) {
	if val, ok := s.Get("translate"); ok {
		if val == "none" || val == "" {
			return 0, 0, false
		}
		parts := strings.Fields(val)
		if len(parts) == 1 {
			if v, ok2 := ParseLength(parts[0]); ok2 {
				return v, 0, true
			}
		} else if len(parts) >= 2 {
			vx, ok1 := ParseLength(parts[0])
			vy, ok2 := ParseLength(parts[1])
			if ok1 && ok2 {
				return vx, vy, true
			}
		}
	}
	return 0, 0, false
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

// BackgroundRepeatType represents background-repeat values
type BackgroundRepeatType string

const (
	BackgroundRepeatRepeat   BackgroundRepeatType = "repeat"
	BackgroundRepeatNoRepeat BackgroundRepeatType = "no-repeat"
	BackgroundRepeatRepeatX  BackgroundRepeatType = "repeat-x"
	BackgroundRepeatRepeatY  BackgroundRepeatType = "repeat-y"
)

// GetBackgroundAttachment returns the background-attachment value (default: scroll)
func (s *Style) GetBackgroundAttachment() string {
	if val, ok := s.Get("background-attachment"); ok {
		return val
	}
	return "scroll"
}

// GetBackgroundRepeat returns the background-repeat value (default: repeat)
func (s *Style) GetBackgroundRepeat() BackgroundRepeatType {
	if val, ok := s.Get("background-repeat"); ok {
		switch val {
		case "no-repeat":
			return BackgroundRepeatNoRepeat
		case "repeat-x":
			return BackgroundRepeatRepeatX
		case "repeat-y":
			return BackgroundRepeatRepeatY
		}
	}
	return BackgroundRepeatRepeat
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
}

// ResolveX converts X to pixels: offset = (containerWidth - imageWidth) * percentage
func (p BackgroundPosition) ResolveX(containerW, imageW float64) float64 {
	if p.XIsPixel {
		return p.X
	}
	if p.X < 0 {
		return (containerW - imageW) * (-p.X) / 100
	}
	return p.X
}

// ResolveY converts Y to pixels: offset = (containerHeight - imageHeight) * percentage
func (p BackgroundPosition) ResolveY(containerH, imageH float64) float64 {
	if p.YIsPixel {
		return p.Y
	}
	if p.Y < 0 {
		return (containerH - imageH) * (-p.Y) / 100
	}
	return p.Y
}

// GetBackgroundPosition parses background-position (default: 0 0)
func (s *Style) GetBackgroundPosition() BackgroundPosition {
	val, ok := s.Get("background-position")
	if !ok {
		return BackgroundPosition{0, 0, false, false}
	}
	return parseBackgroundPosition(val, s.GetFontSize())
}

// ParseBackgroundPosition parses a background-position value string (uses default 16px font-size)
func ParseBackgroundPosition(val string) BackgroundPosition {
	return parseBackgroundPosition(val, 16.0)
}

func parseBackgroundPosition(val string, fontSize float64) BackgroundPosition {
	parts := strings.Fields(val)
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
			pos.X, pos.XIsPixel = parsePositionComponent(parts[0], fontSize)
			pos.Y = -50 // center
		}
	} else if len(parts) >= 2 {
		pos.X, pos.XIsPixel = parsePositionComponent(parts[0], fontSize)
		pos.Y, pos.YIsPixel = parsePositionComponent(parts[1], fontSize)
	}
	return pos
}

func parsePositionComponent(val string, fontSize float64) (float64, bool) {
	switch val {
	case "left", "top":
		return 0, false
	case "right", "bottom":
		return -100, false // 100% stored as negative
	case "center":
		return -50, false // 50% stored as negative
	}
	if strings.HasSuffix(val, "%") {
		if pct, err := strconv.ParseFloat(strings.TrimSuffix(val, "%"), 64); err == nil {
			return -pct, false // Store percentage as negative
		}
	}
	if length, ok := ParseLengthWithFontSize(val, fontSize); ok {
		return length, true // Pixel value (may be negative)
	}
	return 0, false
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

// Phase 23: List styling

// ListStyleType represents the list-style-type property value
type ListStyleType string

const (
	ListStyleTypeDisc    ListStyleType = "disc"
	ListStyleTypeCircle  ListStyleType = "circle"
	ListStyleTypeSquare  ListStyleType = "square"
	ListStyleTypeDecimal ListStyleType = "decimal"
	ListStyleTypeNone    ListStyleType = "none"
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

// Phase 25: clip-path, filter, mix-blend-mode, mask-image

// ClipPathType represents the type of clip-path shape
type ClipPathType string

const (
	ClipPathNone    ClipPathType = "none"
	ClipPathCircle  ClipPathType = "circle"
	ClipPathEllipse ClipPathType = "ellipse"
	ClipPathPolygon ClipPathType = "polygon"
)

// ClipPath represents a parsed clip-path value.
// Pixel values are stored directly. Percentage values (0-100) are stored
// in the Pct variants and the corresponding px field is set to -1.
type ClipPath struct {
	Type   ClipPathType
	Radius float64 // circle radius in px (-1 = default closest-side or use RadiusPct)
	Rx, Ry float64 // ellipse radii in px (-1 = default or use RxPct/RyPct)
	Cx, Cy float64 // center position in px (-1 = default center or use CxPct/CyPct)
	Points []float64 // polygon points [x1, y1, x2, y2, ...] in px or pct (see PointsPct)

	// Percentage flags — when true, the corresponding value is a percentage (0-100).
	RadiusPct float64 // circle radius as percentage (-1 = not set)
	RxPct     float64 // ellipse rx as percentage (-1 = not set)
	RyPct     float64 // ellipse ry as percentage (-1 = not set)
	CxPct     float64 // center x as percentage (-1 = not set)
	CyPct     float64 // center y as percentage (-1 = not set)
	PointsPct []bool  // per-coordinate: true = percentage, false = px
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
	return nil
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
	Name  string  // "opacity", "contrast", "grayscale", "blur", etc.
	Value float64 // The function argument (0-1 for opacity, 0-N for contrast, etc.)
}

// GetFilter parses the filter property and returns filter functions
func (s *Style) GetFilter() []FilterFunction {
	val, ok := s.Get("filter")
	if !ok || val == "none" {
		return nil
	}
	var filters []FilterFunction
	val = strings.TrimSpace(val)
	for len(val) > 0 {
		val = strings.TrimSpace(val)
		parenIdx := strings.Index(val, "(")
		if parenIdx < 0 {
			break
		}
		name := strings.TrimSpace(val[:parenIdx])
		closeIdx := strings.Index(val[parenIdx:], ")")
		if closeIdx < 0 {
			break
		}
		arg := strings.TrimSpace(val[parenIdx+1 : parenIdx+closeIdx])
		var value float64
		if pct, ok := ParsePercentage(arg); ok {
			value = pct / 100.0
		} else if name == "hue-rotate" {
			// hue-rotate takes an angle value (deg, rad, turn)
			if a := parseAngle(arg); a != nil {
				value = *a
			}
		} else if f, err := strconv.ParseFloat(arg, 64); err == nil {
			value = f
		}
		filters = append(filters, FilterFunction{Name: name, Value: value})
		val = val[parenIdx+closeIdx+1:]
	}
	return filters
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
	TextOverflowClip     TextOverflowType = iota
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

// GetMaskImage returns the mask-image property value
func (s *Style) GetMaskImage() string {
	if val, ok := s.Get("mask-image"); ok {
		return val
	}
	// Also check -webkit-mask-image
	if val, ok := s.Get("-webkit-mask-image"); ok {
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

// GetBackdropFilter parses the backdrop-filter property and returns filter functions.
func (s *Style) GetBackdropFilter() []FilterFunction {
	val, ok := s.Get("backdrop-filter")
	if !ok || val == "none" || val == "" {
		return nil
	}
	// Reuse the same filter parsing logic as GetFilter
	var filters []FilterFunction
	val = strings.TrimSpace(val)
	for len(val) > 0 {
		val = strings.TrimSpace(val)
		parenIdx := strings.Index(val, "(")
		if parenIdx < 0 {
			break
		}
		name := strings.TrimSpace(val[:parenIdx])
		closeIdx := strings.Index(val[parenIdx:], ")")
		if closeIdx < 0 {
			break
		}
		arg := strings.TrimSpace(val[parenIdx+1 : parenIdx+closeIdx])
		var value float64
		if pct, ok := ParsePercentage(arg); ok {
			value = pct / 100.0
		} else if f, err := strconv.ParseFloat(arg, 64); err == nil {
			value = f
		}
		filters = append(filters, FilterFunction{Name: name, Value: value})
		val = val[parenIdx+closeIdx+1:]
	}
	return filters
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
func (s *Style) GetBorderImageWidth(borderWidths [4]float64) [4]float64 {
	v, ok := s.Get("border-image-width")
	if !ok || strings.TrimSpace(v) == "" {
		return borderWidths // default = border-width values
	}
	v = strings.TrimSpace(v)
	parts := strings.Fields(v)
	var vals [4]float64
	for i, p := range parts {
		if i >= 4 {
			break
		}
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

// GetTextEmphasisMark returns the actual character to use as emphasis mark.
// Returns "" if no emphasis should be drawn.
func (s *Style) GetTextEmphasisMark() string {
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
	// If only "filled" or "open" (no shape specified), default to circle
	if style == "filled" {
		filled = true
		shape = "circle"
	} else if style == "open" {
		filled = false
		shape = "circle"
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
		// dir="auto" determines direction from first strong character.
		// For now, default to ltr (full implementation would inspect text content).
		style.Set("direction", "ltr")
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
func ComputeStyleWithLogical(node *html.Node, stylesheets []*Stylesheet, viewportWidth, viewportHeight float64) *Style {
	style := ComputeStyle(node, stylesheets, viewportWidth, viewportHeight)
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

	// Remap inline-size / block-size — but skip for inherited writing-mode,
	// because transformToVerticalRL handles positioning as a post-pass and
	// expects children to use horizontal dimensions.
	if inherited, _ := style.Get("_writing-mode-inherited"); inherited == "true" {
		return
	}
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
