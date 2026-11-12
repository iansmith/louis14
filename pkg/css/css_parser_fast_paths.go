package css

import (
	"slices"
	"strings"
)

// keywordPropertyValues are the keyword properties louis14 validates and
// the keywords each accepts, from CSSParserFastPaths::
// IsValidKeywordPropertyAndValue (core/css/parser/css_parser_fast_paths.cc:
// 1142 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A keyword property
// not listed here is not checked yet.
var keywordPropertyValues = map[string][]string{
	"caret-shape": {"auto", "bar", "block", "underscore"}, // :1191-1194
}

// consumeKeywordPropertyValue returns the keyword value is, if it is one of
// the keywords of prop, a property in keywordPropertyValues. A keyword
// matches ASCII case-insensitively and is stored as Blink stores its
// CSSValueID: lower-case.
func consumeKeywordPropertyValue(prop, value string) (string, bool) {
	kw := asciiToLower(strings.TrimSpace(value))
	return kw, slices.Contains(keywordPropertyValues[prop], kw)
}
