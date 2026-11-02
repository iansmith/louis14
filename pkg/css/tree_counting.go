package css

import (
	"strconv"
	"strings"

	"louis14/pkg/html"
)

// resolveTreeCountingFunctions replaces sibling-index() and sibling-count()
// function tokens in node's declared values with their integer values for
// node: its 1-based index among its parent's element children
// (nthChildIndex), and their count (totalElementChildren). Mirrors
// CSSMathExpressionSiblingFunction::ComputeDouble (core/css/
// css_math_expression_node.cc:5580-5600 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which resolves them for the
// element whose style is computed (NthIndexCache::NthChildIndex and
// NthChildIndex + NthLastChildIndex − 1); a pseudo-element passes its
// originating element. Custom properties are skipped, as Blink resolves at
// the using element; a var() that carries one into a property stays
// unresolved here (LOU-650).
func resolveTreeCountingFunctions(node *html.Node, style *Style) {
	index, count := -1, -1
	for prop, val := range style.Properties {
		if strings.HasPrefix(prop, "--") || !containsFoldASCII(val, "sibling-") {
			continue
		}
		if index < 0 {
			index, count = nthChildIndex(node), totalElementChildren(node)
		}
		style.Properties[prop] = replaceTreeCountingFunctions(val, index, count)
	}
}

// replaceTreeCountingFunctions replaces each sibling-index() /
// sibling-count() function token in v (ASCII case-insensitive, optional
// whitespace inside the parentheses) with index / count. Text inside
// string tokens and identifiers that merely end in "sibling-index" (a
// preceding name code point) are left alone, as the CSS tokenizer makes
// only a whole <function-token> of that name.
func replaceTreeCountingFunctions(v string, index, count int) string {
	var b strings.Builder
	b.Grow(len(v))
	var quote byte
	for i := 0; i < len(v); {
		c := v[i]
		switch {
		case quote != 0:
			if c == '\\' && i+1 < len(v) {
				b.WriteString(v[i : i+2])
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case (c == 's' || c == 'S') && (i == 0 || !isIdentContinue(v[i-1])):
			if n, value, ok := matchTreeCountingFunction(v[i:], index, count); ok {
				b.WriteString(strconv.Itoa(value))
				i += n
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// matchTreeCountingFunction reports whether s starts with sibling-index()
// or sibling-count(), returning the matched length and its value.
func matchTreeCountingFunction(s string, index, count int) (n, value int, ok bool) {
	for _, f := range []struct {
		name  string
		value int
	}{{"sibling-index(", index}, {"sibling-count(", count}} {
		if len(s) < len(f.name) || !strings.EqualFold(s[:len(f.name)], f.name) {
			continue
		}
		j := len(f.name)
		for j < len(s) && isCSSWhitespace(s[j]) {
			j++
		}
		if j < len(s) && s[j] == ')' {
			return j + 1, f.value, true
		}
	}
	return 0, 0, false
}

// containsFoldASCII reports whether s contains the lowercase ASCII sub,
// ignoring ASCII case, without allocating.
func containsFoldASCII(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i]|0x20 == sub[0] && strings.EqualFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}
