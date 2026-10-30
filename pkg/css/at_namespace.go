package css

// consumeNamespaceRule parses a `@namespace [prefix] <string|url()>;`
// statement.
func consumeNamespaceRule(ruleText string) (prefix, uri string, ok bool) {
	return "", "", false
}

// parserAddNamespace records a parsed @namespace rule on the sheet.
func (ss *Stylesheet) parserAddNamespace(prefix, uri string) {}

// resolveNamespaces resolves the selector's type selectors against
// defaultNamespace.
func (s *Selector) resolveNamespaces(defaultNamespace string) {}

// parserAppendRules appends rules to the sheet.
func (ss *Stylesheet) parserAppendRules(rules []Rule) {
	ss.Rules = append(ss.Rules, rules...)
}
