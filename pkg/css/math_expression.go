// Package css — math expressions (CSS Values 4 §10).
//
// This file consolidates the math-function dispatch for calc(), min(),
// max(), and clamp(). The string-based calcContext + token-stream
// evaluator in style.go remains the primitive; this layer adds:
//
//  1. A unified entry point IsMathFunction / EvalMathFunction that recognizes
//     all four heads and routes to the right sub-evaluator while threading
//     calcContext (so percent base, viewport, em/rem/ch all propagate through
//     nested function calls).
//
//  2. Nested math-function support inside calc() atoms: tokens like
//     `calc`, `min`, `max`, `clamp` followed by `(` are detected and the
//     sub-expression is evaluated recursively rather than falling through
//     to the length parser (which doesn't know what to do with bare
//     identifiers).
//
//  3. Percent-base–aware min/max/clamp argument evaluation. The legacy
//     evalMinMax / evalClamp in style.go fall back to viewport-width for
//     percent terms; the ctx-aware variants here route % through
//     parseLengthFullWithCh's calc branch with the caller-supplied percent
//     base, matching the layout layer's intent.
//
// Mirrors Chromium Blink's third_party/blink/renderer/core/css/
// css_math_expression_node.cc at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
// — specifically CSSMathExpressionOperation::Operation::kMin/kMax/kClamp
// and CSSMathExpressionOperation::EvaluateOperator.
package css

import (
	"strconv"
	"strings"
)

// IsMathFunction reports whether val is one of the four math-function heads
// recognized by CSS Values 4 §10: calc(), min(), max(), clamp().
//
// The check is structural (prefix + balanced suffix), not validating. A
// well-formed but type-mismatched call like `min(0, 100%)` still returns
// true here; downstream evaluation will reject it.
func IsMathFunction(val string) bool {
	v := strings.TrimSpace(val)
	if !strings.HasSuffix(v, ")") {
		return false
	}
	return strings.HasPrefix(v, "calc(") ||
		strings.HasPrefix(v, "min(") ||
		strings.HasPrefix(v, "max(") ||
		strings.HasPrefix(v, "clamp(")
}

// IsMathFunctionWithPercent reports whether val is a math function whose
// inner expression contains a percentage term. Mirrors IsCalcWithPercent's
// role for the calc()-only path: callers use this to decide whether to
// thread the layout-time percent base into evaluation.
func IsMathFunctionWithPercent(val string) bool {
	if !IsMathFunction(val) {
		return false
	}
	return strings.Contains(val, "%")
}

// EvalMathFunction evaluates a CSS math function (calc / min / max / clamp,
// possibly nested) with the supplied context. Returns (result, true) on
// success, (0, false) on parse or type errors.
//
// ctx.percentBase is the resolution base for `<percentage>` terms. For
// length-percentage callers in the layout layer this is the containing
// block's inline-size or block-size, depending on the property.
func EvalMathFunction(val string, ctx calcContext) (float64, bool) {
	v := strings.TrimSpace(val)
	if !strings.HasSuffix(v, ")") {
		return 0, false
	}
	switch {
	case strings.HasPrefix(v, "calc("):
		return evalCalcFull(v[len("calc("):len(v)-1], ctx)
	case strings.HasPrefix(v, "min("):
		return evalMinMaxCtx(v[len("min("):len(v)-1], "min", ctx)
	case strings.HasPrefix(v, "max("):
		return evalMinMaxCtx(v[len("max("):len(v)-1], "max", ctx)
	case strings.HasPrefix(v, "clamp("):
		return evalClampCtx(v[len("clamp("):len(v)-1], ctx)
	}
	return 0, false
}

// EvalMathFunctionWithPercent is a convenience wrapper around EvalMathFunction
// that constructs a calcContext from (fontSize, percentBase) — the same shape
// fragment_geometry's resolvers already use for calc-with-percent. Mirrors
// EvalCalcWithPercent for the broader math-function family.
func EvalMathFunctionWithPercent(val string, fontSize, percentBase float64) (float64, bool) {
	return EvalMathFunction(val, calcContext{
		fontSize:    fontSize,
		percentBase: percentBase,
		chScale:     0.5,
	})
}

// parseMathArgCtx evaluates a single comma-separated argument to a math
// function. Unlike the legacy parseMathArg (style.go), this routes through
// parseLengthFullWithCh — which means percentages, nested math functions,
// and the full set of length units all resolve against the supplied ctx.
//
// CSS Values 4 §10.7: each min/max/clamp argument is itself a math
// expression — so `min(300px, 25% + 100px)` has two arguments, the second
// of which is a sum expression. We delegate to evalCalcFull (the additive
// expression engine in style.go) when the argument contains a top-level
// +/-/*/`/` operator; otherwise atom-level parsing handles it.
//
// CSS Values 4 §10.10: a single argument must reduce to a single typed value.
// A unitless 0 inside a length-percentage math function is a type mismatch
// per CSS Values 4 §10.10 "Type Checking" — see evalMinMaxCtx for the
// list-level rejection.
func parseMathArgCtx(s string, ctx calcContext) (float64, bool) {
	s = strings.TrimSpace(s)
	// Nested math function: delegate.
	if IsMathFunction(s) {
		return EvalMathFunction(s, ctx)
	}
	// Bare percentage (no operators): resolve against ctx.percentBase.
	if strings.HasSuffix(s, "%") && !containsTopLevelOperator(s) {
		numStr := strings.TrimSuffix(s, "%")
		if num, err := strconv.ParseFloat(numStr, 64); err == nil {
			if ctx.percentBase > 0 || ctx.percentResolvesToZero {
				return num * ctx.percentBase / 100, true
			}
			// No percent base supplied — fall back to viewport-width
			// for compatibility with legacy callers (parseLengthFull).
			if ctx.viewportWidth > 0 {
				return num * ctx.viewportWidth / 100, true
			}
		}
		return 0, false
	}
	// Sum/product expression at top level: feed through the calc()
	// evaluator. CSS Values 4 §10.7 allows a math-function argument to
	// be any math expression, so `min(300px, 25% + 100px)` is valid;
	// each arg is parsed as if wrapped in calc().
	if containsTopLevelOperator(s) {
		return evalCalcFull(s, ctx)
	}
	// Other lengths and numbers — go through the length parser, which
	// handles px/em/rem/vw/vh/ch/ex/cap/lh/calc()/min()/max()/clamp().
	return parseLengthFullWithCh(s, ctx.fontSize, ctx.viewportWidth, ctx.viewportHeight, ctx.chScale, ctx.xHeight, ctx.capHeight, ctx.lhSize, ctx.icWidth)
}

// containsTopLevelOperator reports whether s has a top-level math operator
// (+ - * /) outside any parenthesised sub-expression. + and - require
// surrounding whitespace per CSS Values 4 §10.7; * and / do not.
func containsTopLevelOperator(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '*', '/':
			if depth == 0 {
				return true
			}
		case '+', '-':
			if depth == 0 && i > 0 && i < len(s)-1 &&
				s[i-1] == ' ' && s[i+1] == ' ' {
				return true
			}
		}
	}
	return false
}

// argType classifies a math-function argument's CSS type for §10.10 type
// checking. Only the categories that occur in min()/max()/clamp() length-
// percentage callsites are tracked here; angle/time/number contexts are
// not currently in scope for C114.
type argType int

const (
	argTypeUnknown argType = iota
	argTypeUnitlessZero
	argTypeNumber  // unitless non-zero (e.g. flex factor)
	argTypeLength  // px/em/rem/vw/vh/ch/ex/cap/lh — also calc() reducing to length
	argTypePercent // bare percentage or percent-only expression
	argTypeLP      // length-percentage mix (e.g. calc(50% - 10px))
)

// classifyMathArg returns the CSS type category of a single math-function
// argument. Used to enforce §10.10 type rules — in particular that a
// unitless 0 cannot be mixed with length or percentage terms.
func classifyMathArg(s string) argType {
	s = strings.TrimSpace(s)
	if s == "" {
		return argTypeUnknown
	}
	// Bare percentage.
	if strings.HasSuffix(s, "%") && !strings.Contains(s, "(") {
		return argTypePercent
	}
	// Plain number (unitless).
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n == 0 {
			return argTypeUnitlessZero
		}
		return argTypeNumber
	}
	// Nested math function: walk into the inner expression. We treat a
	// math function with a percent term as length-percentage (LP); without
	// it, as length. This is a coarse classifier — sufficient for §10.10
	// "unitless zero mixed with LP" detection but not a full type system.
	if IsMathFunction(s) {
		if strings.Contains(s, "%") {
			if strings.ContainsAny(s, "+-") {
				return argTypeLP
			}
			return argTypePercent
		}
		return argTypeLength
	}
	// Anything with a "%" but inside something — treat as LP.
	if strings.Contains(s, "%") {
		return argTypeLP
	}
	return argTypeLength
}

// evalMinMaxCtx evaluates a min() or max() argument list with full ctx.
// Replaces the legacy evalMinMax for callers that need percent-base
// propagation. Enforces CSS Values 4 §10.10 type checking: a unitless 0
// argument is rejected when any sibling argument is length-percentage or
// percent — the test css-values/max-unitless-zero-invalid asserts this
// (max(100%) is valid; max(0, 100%) is not).
func evalMinMaxCtx(argsStr, mode string, ctx calcContext) (float64, bool) {
	args := splitCSSFunctionArgs(argsStr)
	if len(args) < 1 {
		return 0, false
	}
	// First pass: classify all args and reject type mixes per §10.10.
	hasUnitlessZero := false
	hasLengthOrPct := false
	types := make([]argType, len(args))
	for i, a := range args {
		t := classifyMathArg(a)
		types[i] = t
		switch t {
		case argTypeUnitlessZero:
			hasUnitlessZero = true
		case argTypeLength, argTypePercent, argTypeLP:
			hasLengthOrPct = true
		}
	}
	if hasUnitlessZero && hasLengthOrPct {
		return 0, false
	}
	// Second pass: evaluate.
	result, ok := parseMathArgCtx(args[0], ctx)
	if !ok {
		return 0, false
	}
	for _, arg := range args[1:] {
		val, ok := parseMathArgCtx(arg, ctx)
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

// evalClampCtx evaluates a clamp(min, val, max) call with full ctx.
// clamp(min, val, max) ≡ max(min, min(val, max)) per CSS Values 4 §10.4.
// Accepts the keyword "none" for min/max (CSS Values 4 §10.4: "If the
// minimum value is greater than the maximum value, the result is the
// minimum"). The bare-keyword "none" forms aren't covered by any of the
// failing tests, so they're not implemented here.
func evalClampCtx(argsStr string, ctx calcContext) (float64, bool) {
	args := splitCSSFunctionArgs(argsStr)
	if len(args) != 3 {
		return 0, false
	}
	minVal, ok1 := parseMathArgCtx(args[0], ctx)
	prefVal, ok2 := parseMathArgCtx(args[1], ctx)
	maxVal, ok3 := parseMathArgCtx(args[2], ctx)
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

// isValidLPMathFunctionAtParseTime reports whether a top-level math function
// is syntactically + type-wise valid for a length-percentage CSS property.
// Run at declaration parse time so an invalid math expression cannot
// overwrite an earlier valid value in the cascade.
//
// CSS Values 4 §10.10 "Type Checking":
//   - A unitless 0 inside a math function is treated as a <number>, never
//     a <length>. Mixing it with a <length-percentage> argument is invalid.
//
// This is a structural check: we walk the comma-separated argument list of
// min()/max()/clamp() and reject the case where a sibling is a bare 0 and
// any other sibling is length-percentage. calc() with a single argument
// has no list so the check trivially passes.
func isValidLPMathFunctionAtParseTime(val string) bool {
	v := strings.TrimSpace(val)
	if !strings.HasSuffix(v, ")") {
		return true // caller's IsMathFunction gate already covers structural failures.
	}
	var inner string
	switch {
	case strings.HasPrefix(v, "calc("):
		inner = v[len("calc(") : len(v)-1]
		// calc() takes one expression — type-check that expression alone.
		return isValidLPMathExprAtParseTime(inner)
	case strings.HasPrefix(v, "min("):
		inner = v[len("min(") : len(v)-1]
	case strings.HasPrefix(v, "max("):
		inner = v[len("max(") : len(v)-1]
	case strings.HasPrefix(v, "clamp("):
		inner = v[len("clamp(") : len(v)-1]
	default:
		return true
	}
	args := splitCSSFunctionArgs(inner)
	hasUnitlessZero := false
	hasLPOrLength := false
	for _, a := range args {
		t := classifyMathArg(a)
		switch t {
		case argTypeUnitlessZero:
			hasUnitlessZero = true
		case argTypeLength, argTypePercent, argTypeLP:
			hasLPOrLength = true
		}
		if !isValidLPMathExprAtParseTime(a) {
			return false
		}
	}
	if hasUnitlessZero && hasLPOrLength {
		return false
	}
	return true
}

// isValidLPMathExprAtParseTime checks a single expression (one calc()
// argument or one min/max/clamp argument) for §10.10 type compliance. A
// bare 0 inside a +/- expression with a length-percentage term is invalid
// per the same rule that catches it at the function-list level.
//
// The check is recursive: nested math functions are themselves walked.
func isValidLPMathExprAtParseTime(expr string) bool {
	expr = strings.TrimSpace(expr)
	if IsMathFunction(expr) {
		return isValidLPMathFunctionAtParseTime(expr)
	}
	// Only check additive expressions — the failure mode is mixing
	// bare 0 with length-percentage terms across + / -. Multiplicative
	// (*, /) is always type-compatible.
	if !strings.ContainsAny(expr, "+-") {
		return true
	}
	// Splitting on +/- naively is wrong (- can be a sign of a token);
	// rely on splitting at top level only. The existing splitCSSFunctionArgs
	// splits on commas at depth 0 — for additive split we walk manually.
	terms := splitAdditiveTerms(expr)
	hasUnitlessZero := false
	hasLPOrLength := false
	for _, t := range terms {
		switch classifyMathArg(t) {
		case argTypeUnitlessZero:
			hasUnitlessZero = true
		case argTypeLength, argTypePercent, argTypeLP:
			hasLPOrLength = true
		}
	}
	if hasUnitlessZero && hasLPOrLength {
		return false
	}
	return true
}

// splitAdditiveTerms splits a calc-expression-body string on top-level
// + and - operators (respecting nested parens). Used by §10.10 type
// checking to enumerate the operands of an additive chain.
func splitAdditiveTerms(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case '+', '-':
			// Only split at depth 0, and only when surrounded by whitespace
			// (CSS Values 4 §10.7: whitespace required around + and -).
			if depth == 0 && i > 0 && i < len(s)-1 {
				if s[i-1] == ' ' && s[i+1] == ' ' {
					out = append(out, strings.TrimSpace(s[start:i]))
					start = i + 1
				}
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

// mathFunctionNames lists the four identifiers recognized as math-function
// heads inside a calc() atom. Mirrors the CSSValueID switch in Blink's
// CSSMathExpressionOperation::ParseMathFunction.
var mathFunctionNames = map[string]bool{
	"calc":  true,
	"min":   true,
	"max":   true,
	"clamp": true,
}

// extractNestedMathCall examines tokens starting at pos. If they form a
// math-function call (identifier followed by `(` … matching `)`), the
// function returns the original source substring (e.g. "min(10px, 20px)"),
// the number of tokens consumed, and true. Otherwise returns ("", 0, false).
//
// The token stream emitted by tokenizeCalc loses original whitespace, so we
// reconstruct the call by walking forward until paren depth returns to zero.
// This is sufficient for re-feeding the substring into EvalMathFunction —
// EvalMathFunction's own tokenizer will re-parse the inner argument list.
func extractNestedMathCall(tokens []string, pos int) (string, int, bool) {
	if pos+1 >= len(tokens) {
		return "", 0, false
	}
	head := tokens[pos]
	if !mathFunctionNames[head] {
		return "", 0, false
	}
	if tokens[pos+1] != "(" {
		return "", 0, false
	}
	depth := 1
	end := pos + 2
	for end < len(tokens) && depth > 0 {
		switch tokens[end] {
		case "(":
			depth++
		case ")":
			depth--
		}
		end++
	}
	if depth != 0 {
		return "", 0, false
	}
	// Rebuild the call. We insert spaces between tokens — calc/min/max/
	// clamp don't care about whitespace beyond +/- operator delimiters,
	// which we preserve by always interposing a space.
	var sb strings.Builder
	sb.WriteString(head)
	sb.WriteString("(")
	// Tokens between (exclusive) the opening `(` at pos+1 and the
	// matching `)` at end-1.
	first := true
	for i := pos + 2; i < end-1; i++ {
		t := tokens[i]
		if !first && t != ")" && t != "," {
			// Need a space separator before each non-closing token,
			// except right after `(` or `,`.
			prev := tokens[i-1]
			if prev != "(" && prev != "," {
				sb.WriteString(" ")
			}
		}
		if t == "," {
			sb.WriteString(",")
		} else {
			sb.WriteString(t)
		}
		first = false
	}
	sb.WriteString(")")
	return sb.String(), end - pos, true
}
