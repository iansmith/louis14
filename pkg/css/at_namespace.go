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

// resolveNamespaces gives the selector's compounds the default namespace,
// as Blink's CSSSelectorParser does at parse time. defaultNamespace ""
// (no @namespace) leaves the selector unconstrained.
//
//   - A type or universal selector written without a `prefix|` takes it:
//     DetermineNamespace(null prefix) -> DefaultNamespace()
//     (core/css/parser/css_selector_parser.cc:2244-2256 @
//     d4ecdfed88f962439247c2ad36b8fe47805b1520), for a type-only compound
//     (:1333-1343) and for a type selector PrependTypeSelectorIfNeeded
//     prepends to a longer compound (:2283-2290).
//   - A compound without a type selector takes it too, as the implicit
//     `ns|*` PrependTypeSelectorIfNeeded prepends whenever
//     DefaultNamespace() is not `*` (:2270-2290), so under an XHTML
//     default `.container` is `xhtml|*.container`.
//   - Except the subject compound of a functional pseudo-class argument
//     with no type selector: with resistDefaultNamespace (Blink
//     resist_default_namespace_, set for :is/:where/:has/:not) the last
//     compound ignores the default namespace (ignore_default_namespace_,
//     :1310-1322), so `*|*:is(:disabled)` still matches in any namespace
//     (is-default-ns-002). Like Blink's AtEndIgnoringWhitespace check, only
//     the last branch of the argument list qualifies: parseNestedSelector
//     passes resistDefaultNamespace false for the others.
//
// Prefixed selectors keep matching any namespace (no prefix map, LOU-437
// L4). No Blink analog for running this after parsing: louis14's
// parseSelector takes no sheet context because it also serves
// context-free callers (ParseSelector for pkg/js, @supports selector()).
func (s *Selector) resolveNamespaces(defaultNamespace string, resistDefaultNamespace bool) {
	if defaultNamespace == "" {
		return
	}
	s.defaultNamespace = defaultNamespace
	subject := len(s.Parts) - 1
	for i := range s.Parts {
		p := &s.Parts[i]
		if p.hasNamespacePrefix || (p.Element == "" && resistDefaultNamespace && i == subject) {
			continue
		}
		p.NamespaceURI = defaultNamespace
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
		rules[i].Selector.resolveNamespaces(ss.DefaultNamespace, false)
	}
	ss.Rules = append(ss.Rules, rules...)
}
