package css

import (
	"regexp"
	"strconv"
	"strings"

	"louis14/pkg/html"
)

// siblingIndexRE / siblingCountRE match sibling-index() and sibling-count(),
// case-insensitively, with optional whitespace inside the parentheses.
var (
	siblingIndexRE = regexp.MustCompile(`(?i)sibling-index\(\s*\)`)
	siblingCountRE = regexp.MustCompile(`(?i)sibling-count\(\s*\)`)
)

// resolveTreeCountingFunctions replaces sibling-index() and sibling-count()
// in node's declared values with their integer values for node: its 1-based
// index among its parent's element children (nthChildIndex), and their
// count (totalElementChildren). Mirrors CSSMathExpressionSiblingFunction::
// ComputeDouble (core/css/css_math_expression_node.cc:5580-5600 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which resolves them for the
// element whose style is computed (NthIndexCache::NthChildIndex and
// NthChildIndex + NthLastChildIndex − 1). Custom properties are skipped, as
// Blink resolves at the using element; a var() that carries one into a
// property stays unresolved here (LOU-650).
func resolveTreeCountingFunctions(node *html.Node, style *Style) {
	for prop, val := range style.Properties {
		if strings.HasPrefix(prop, "--") || !containsFoldASCII(val, "sibling-") {
			continue
		}
		val = siblingIndexRE.ReplaceAllLiteralString(val, strconv.Itoa(nthChildIndex(node)))
		style.Properties[prop] = siblingCountRE.ReplaceAllLiteralString(val, strconv.Itoa(totalElementChildren(node)))
	}
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
