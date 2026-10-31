package css

import "strings"

// consumeNamespaceRule parses a body-less `@namespace [prefix] <string|url()>;`
// statement (CSS Namespaces 3 §2), returning the optional prefix and the
// namespace URI verbatim (no base-URL resolution). ok is false on a parse
// error: no URI, or anything after it. Mirrors Blink
// CSSParserImpl::ConsumeNamespaceRule (core/css/parser/css_parser_impl.cc:
// 1209-1230 @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
func consumeNamespaceRule(ruleText string) (prefix, uri string, ok bool) {
	const keyword = "@namespace"
	trimmed := strings.TrimSpace(ruleText)
	if !strings.HasPrefix(trimmed, keyword) ||
		(len(trimmed) > len(keyword) && isIdentContinue(trimmed[len(keyword)])) {
		return "", "", false // a different at-keyword, e.g. @namespaces
	}
	prelude := statementAtRulePrelude(trimmed, keyword)
	// Optional <namespace-prefix> ident. `url(` is a URL/function token,
	// not an ident.
	n := 0
	for n < len(prelude) && isIdentContinue(prelude[n]) {
		n++
	}
	if n > 0 && !strings.HasPrefix(prelude[n:], "(") {
		prefix = prelude[:n]
		prelude = strings.TrimSpace(prelude[n:])
	}
	uri, rest, ok := consumeStringOrURI(prelude)
	if !ok || rest != "" {
		return "", "", false
	}
	return prefix, uri, true
}

// parserAddNamespace records a parsed @namespace rule. Only the default
// namespace (no prefix) is kept: louis14 has no prefix map yet, so a named
// prefix is a no-op and `prefix|tag` keeps matching any namespace
// (LOU-437 decision L4). Mirrors Blink StyleSheetContents::ParserAddNamespace
// (core/css/style_sheet_contents.cc:419-427 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func (ss *Stylesheet) parserAddNamespace(prefix, uri string) {
	if prefix == "" {
		ss.DefaultNamespace = uri
	}
}

// resolveNamespaces gives every type or universal selector written without
// a `prefix|` the default namespace, as Blink's CSSSelectorParser does with
// DetermineNamespace(null prefix) -> DefaultNamespace()
// (core/css/parser/css_selector_parser.cc:2244-2256 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), for a type-only compound
// (:1333-1343) and for the type selector PrependTypeSelectorIfNeeded
// prepends to a longer compound (:2283-2295). defaultNamespace ""
// (no @namespace) leaves the selector unconstrained.
//
// Compounds without a type selector are deliberately left unconstrained:
// louis14 ports neither the implicit `ns|*` PrependTypeSelectorIfNeeded
// adds to them under a non-`*` default namespace (:2278-2284) nor the
// resist_default_namespace_ / ignore_default_namespace_ rule for :is/:where/
// :not/:has arguments (:1318-1321) yet (LOU-438). Leaving them unconstrained
// is what keeps is-default-ns-002 (`*|*:is(:disabled)`) matching; adding
// the implicit `ns|*` without the ignore rule would break it.
//
// No Blink analog for running this after parsing: louis14's parseSelector
// takes no sheet context because it also serves context-free callers
// (ParseSelector for pkg/js, @supports selector()).
func (s *Selector) resolveNamespaces(defaultNamespace string) {
	if defaultNamespace == "" {
		return
	}
	s.defaultNamespace = defaultNamespace
	for i := range s.Parts {
		if s.Parts[i].Element != "" && !s.Parts[i].hasNamespacePrefix {
			s.Parts[i].NamespaceURI = defaultNamespace
		}
	}
}

// parserAppendRules resolves rules' selectors against this sheet's default
// namespace and appends them. Every own-sheet style rule (including rules
// nested in @media/@supports/@container/@layer) enters the sheet here, and
// the @namespace ordering gate in ParseStylesheet guarantees the default
// namespace is final before the first one does. @import'ed rules are
// appended by resolveAtImport already resolved against their own sheet.
// Mirrors Blink StyleSheetContents::ParserAppendRule
// (core/css/style_sheet_contents.cc:183 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func (ss *Stylesheet) parserAppendRules(rules []Rule) {
	for i := range rules {
		rules[i].Selector.resolveNamespaces(ss.DefaultNamespace)
	}
	ss.Rules = append(ss.Rules, rules...)
}
