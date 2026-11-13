package css

import (
	"strconv"
	"strings"
)

// parseFontFeatureValuesRule parses a `@font-feature-values <family-list> {
// <feature-value-blocks> }` at-rule into a [FontFeatureValuesRule]. Returns
// nil on parse failure.
//
// Grammar (CSS Fonts 4 §11.4):
//
//	@font-feature-values <family-name># {
//	  <feature-value-block-list>
//	}
//
//	<feature-value-block> = @stylistic { <name>: <int>; ... }
//	                       | @styleset { <name>: <int># ; ... }
//	                       | @character-variant { <name>: <int> | <int> <int>; ... }
//	                       | @swash { <name>: <int>; ... }
//	                       | @ornaments { <name>: <int>; ... }
//	                       | @annotation { <name>: <int>; ... }
//
// Mirrors Blink's CSSParserImpl::ConsumeFontFeatureValuesRule and the
// AtRuleDescriptor parse path in
// third_party/blink/renderer/core/css/parser/css_parser_impl.cc at Chromium
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func parseFontFeatureValuesRule(ruleStr string) *FontFeatureValuesRule {
	trimmed := strings.TrimSpace(ruleStr)
	if !strings.HasPrefix(trimmed, "@font-feature-values") {
		return nil
	}
	// Strip the at-rule keyword.
	body := strings.TrimSpace(trimmed[len("@font-feature-values"):])
	// Split the prelude (family list) from the block at the first `{`
	// outside strings (blockBounds).
	openPos, closePos, ok := blockBounds(body)
	if !ok {
		return nil
	}
	// Untrimmed: a trailing bad string's newline must survive.
	prelude := body[:openPos]
	innerBody := body[openPos+1 : closePos]

	families, ok := splitFamilyList(prelude)
	if !ok || len(families) == 0 {
		return nil
	}

	rule := &FontFeatureValuesRule{Families: families}

	// The body is a rule list (ConsumeRuleList with kFontFeatureRules,
	// css_parser_impl.cc:1783-1792 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
	// each inner at-rule is one rule (splitRules), so one without a block
	// ends at its `;` and is dropped alone.
	for _, inner := range splitRules(innerBody) {
		inner = strings.TrimSpace(inner)
		if !strings.HasPrefix(inner, "@") {
			continue
		}
		blockStart, blockEnd, ok := blockBounds(inner)
		if !ok {
			continue
		}
		// The at-keyword is the name right after `@` (`@ swash` is no
		// at-keyword); a prelude makes it no feature block.
		atName := strings.ToLower(strings.TrimRight(inner[1:blockStart], " \t\n\r\f"))
		blockBody := inner[blockStart+1 : blockEnd]

		// Parse the name → indexes map.
		entries := parseFeatureValueDeclarations(blockBody)
		if len(entries) == 0 {
			continue
		}
		switch atName {
		case "stylistic":
			ensureMap(&rule.Stylistic)
			mergeFeatureMap(rule.Stylistic, entries, 1)
		case "styleset":
			ensureMap(&rule.Styleset)
			mergeFeatureMap(rule.Styleset, entries, 0)
		case "character-variant":
			ensureMap(&rule.CharacterVariant)
			mergeFeatureMap(rule.CharacterVariant, entries, 2)
		case "swash":
			ensureMap(&rule.Swash)
			mergeFeatureMap(rule.Swash, entries, 1)
		case "ornaments":
			ensureMap(&rule.Ornaments)
			mergeFeatureMap(rule.Ornaments, entries, 1)
		case "annotation":
			ensureMap(&rule.Annotation)
			mergeFeatureMap(rule.Annotation, entries, 1)
		}
	}

	// A rule with no recognized inner blocks is still legal per spec (just
	// has no effect). Return it anyway so caller can store it.
	return rule
}

// splitFamilyList parses the `<family-name>#` prelude of @font-feature-values
// per CSS Fonts 4 §11.4. Comma-separated; a quoted family is its string
// token's unescaped value (ConsumeFamilyName,
// core/css/properties/css_parsing_utils.cc:6406-6419 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A bad string is no family
// name, so it invalidates the list (ok false).
func splitFamilyList(s string) (families []string, ok bool) {
	var out []string
	var cur strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case '"', '\'':
			value, n, closed := consumeStringToken(s[i+1:], c)
			if !closed {
				return nil, false
			}
			cur.WriteString(value)
			i += 1 + n
		case ',':
			name := strings.TrimSpace(cur.String())
			if name != "" {
				out = append(out, name)
			}
			cur.Reset()
			i++
		default:
			cur.WriteByte(c)
			i++
		}
	}
	if name := strings.TrimSpace(cur.String()); name != "" {
		out = append(out, name)
	}
	return out, true
}

// parseFeatureValueDeclarations parses the inner body of a feature-value
// block (`<name>: <int>(#)? ;`...) into a name → indexes map. Invalid
// declarations are silently skipped per CSS Syntax §5.5.
func parseFeatureValueDeclarations(body string) map[string][]int {
	out := make(map[string][]int)
	for _, decl := range strings.Split(body, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		colon := strings.IndexByte(decl, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(decl[:colon])
		valStr := strings.TrimSpace(decl[colon+1:])
		if name == "" || valStr == "" {
			continue
		}
		// Per spec: feature-value-name = <custom-ident>. Reject pure-numeric.
		// In practice, we don't enforce strict ident matching here — bad
		// values just produce nothing useful at lookup time.
		var indexes []int
		// Split on commas (for @styleset multi-index lists) AND whitespace
		// (for @character-variant 2-value syntax: `name: 1 3;`).
		for _, part := range strings.FieldsFunc(valStr, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 {
				// Invalid index → discard the whole declaration.
				indexes = nil
				break
			}
			indexes = append(indexes, n)
		}
		if len(indexes) == 0 {
			continue
		}
		out[name] = indexes
	}
	return out
}

// ensureMap allocates an empty map at *m if it is nil.
func ensureMap(m *map[string][]int) {
	if *m == nil {
		*m = make(map[string][]int)
	}
}

// mergeFeatureMap copies entries into dst. If maxIndexes > 0, declarations
// with more than maxIndexes values are rejected per CSS Fonts 4 §11.4 (only
// @styleset accepts an unbounded `#`; @stylistic/@swash/@ornaments/
// @annotation require exactly one; @character-variant allows 1 or 2).
func mergeFeatureMap(dst, src map[string][]int, maxIndexes int) {
	for name, idxs := range src {
		if maxIndexes > 0 && len(idxs) > maxIndexes {
			continue
		}
		dst[name] = idxs
	}
}

// isCSSWhitespace reports whether c is a CSS whitespace byte (Syntax §4).
func isCSSWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// LookupFontFeatureValues searches the stylesheet's @font-feature-values
// rules for a family match (case-insensitive) and returns the merged
// FontFeatureValuesRule. Later rules win on per-name collisions per CSS
// Fonts 4 §11.4 (mirrors Blink's FontFeatureValuesStorage::Set in
// font_feature_values_storage.cc at Chromium SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// The `family` argument is the resolved first family from the element's
// computed font-family list, quoted or not (a quoted family is unquoted
// here, its escapes resolved).
func LookupFontFeatureValues(rules []FontFeatureValuesRule, family string) FontFeatureValuesRule {
	target := strings.ToLower(unquoteCSSString(strings.TrimSpace(family)))
	merged := FontFeatureValuesRule{}
	for i := range rules {
		match := false
		for _, fam := range rules[i].Families {
			if strings.ToLower(fam) == target {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		// Merge: later wins.
		copyInto(&merged.Stylistic, rules[i].Stylistic)
		copyInto(&merged.Styleset, rules[i].Styleset)
		copyInto(&merged.CharacterVariant, rules[i].CharacterVariant)
		copyInto(&merged.Swash, rules[i].Swash)
		copyInto(&merged.Ornaments, rules[i].Ornaments)
		copyInto(&merged.Annotation, rules[i].Annotation)
	}
	return merged
}

func copyInto(dst *map[string][]int, src map[string][]int) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string][]int)
	}
	for k, v := range src {
		(*dst)[k] = v
	}
}
