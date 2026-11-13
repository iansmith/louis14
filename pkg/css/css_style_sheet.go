package css

import (
	"slices"
	"strings"
)

// Rule mutation on a sheet's contents: CSSStyleSheet::insertRule /
// deleteRule (core/css/css_style_sheet.cc:423-497) through
// StyleSheetContents::WrapperInsertRule / WrapperDeleteRule
// (core/css/style_sheet_contents.cc:278-400) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
//
// louis14 keeps a sheet as text: its top-level rules are
// Stylesheet.RuleTexts, and a mutation edits that list and re-parses the
// whole sheet (Blink parses only the inserted rule; louis14's re-parse
// fetches the sheet's @imports again). A rule changed through its CSSOM
// style first writes its declarations back into its text, so the
// re-parse keeps the change.

// RuleMutationError is the DOMException a rule mutation throws, named by
// its DOMExceptionCode.
type RuleMutationError struct{ Name, Message string }

func (e *RuleMutationError) Error() string { return e.Name + ": " + e.Message }

// CSSOMRule is a top-level rule of a sheet as CSSRuleList shows it: an
// identity that stays with the rule across mutations, which shift its
// index.
type CSSOMRule struct {
	changed bool               // its declarations changed through its CSSOM style
	decls   *DeclarationResult // its declaration block when a style rule
}

// ruleKind classifies a top-level rule for WrapperInsertRule's ordering.
type ruleKind int

const (
	ruleKindOther ruleKind = iota
	ruleKindImport
	ruleKindNamespace
	ruleKindLayerStatement
)

func ruleKindOf(text string) ruleKind {
	switch {
	case hasAtKeyword(text, "@import"):
		return ruleKindImport
	case hasAtKeyword(text, "@namespace"):
		return ruleKindNamespace
	case hasAtKeyword(text, "@layer") && indexTopLevel(text, '{') < 0:
		return ruleKindLayerStatement
	}
	return ruleKindOther
}

// hasAtKeyword reports whether text starts with the at-keyword kw, matched
// as the parser matches at-rules (case-sensitively).
func hasAtKeyword(text, kw string) bool {
	return len(text) > len(kw) && strings.HasPrefix(text, kw) && !isIdentContinue(text[len(kw)])
}

// insertableAtRules are the at-rules CSSParser::ParseRule turns into a rule
// louis14 parses; another at-keyword is no rule.
var insertableAtRules = []string{
	"@import", "@namespace", "@layer", "@media", "@supports", "@container",
	"@font-face", "@keyframes", "@-webkit-keyframes", "@counter-style",
	"@font-feature-values", "@property", "@page", "@scope", "@starting-style",
}

// ensureRules builds the CSSOM rule list from the parse, once.
func (s *StyleSheetContents) ensureRules() *Stylesheet {
	sheet, _ := s.ParsedStylesheet()
	if s.rules == nil && sheet != nil {
		s.rules = make([]*CSSOMRule, len(sheet.RuleTexts))
		for i := range s.rules {
			s.rules[i] = &CSSOMRule{}
		}
		s.bindDeclarations(sheet)
	}
	return sheet
}

// bindDeclarations points each style rule at its declaration block in
// sheet: the block its own selectors' flat Rules share (not a nested
// rule's).
func (s *StyleSheetContents) bindDeclarations(sheet *Stylesheet) {
	for _, r := range s.rules {
		r.decls = nil
	}
	for k := range sheet.Rules {
		i := sheet.Rules[k].SourceRuleIndex
		if i < 0 || i >= len(s.rules) || s.rules[i].decls != nil || strings.HasPrefix(sheet.RuleTexts[i], "@") {
			continue
		}
		openPos, _, ok := blockBounds(sheet.RuleTexts[i])
		if !ok {
			continue
		}
		flat := sheet.Rules[k].Selector
		for _, sel := range splitSelectorGroup(strings.TrimSpace(sheet.RuleTexts[i][:openPos])) {
			if own := ParseSelector(strings.TrimSpace(sel)); own.Raw == flat.Raw && own.PseudoElement == flat.PseudoElement {
				s.rules[i].decls = sheet.Rules[k].DeclarationResult
				break
			}
		}
	}
}

// Rules returns the sheet's top-level rules (CSSRuleList's items).
func (s *StyleSheetContents) Rules() []*CSSOMRule {
	s.ensureRules()
	return s.rules
}

// RuleCount is StyleSheetContents::RuleCount: the number of top-level
// rules.
func (s *StyleSheetContents) RuleCount() int { return len(s.Rules()) }

// RuleDeclarations returns the declaration block of r when it is a style
// rule (the block the selector list's flat Rules share), else nil.
func (s *StyleSheetContents) RuleDeclarations(r *CSSOMRule) *DeclarationResult {
	s.ensureRules()
	return r.decls
}

// SetRuleDeclaration sets property on r's declarations through its CSSOM
// style. url()s in value resolve against the sheet's base, as the sheet's
// own declarations did when parsed (Blink resolves a url() when it parses
// the value, CSSUrlData); the re-parse after a rule mutation does not
// resolve them again.
func (s *StyleSheetContents) SetRuleDeclaration(r *CSSOMRule, property, value string) {
	s.RuleDeclarations(r).SetDeclaration(property, s.Context.RewriteURLs(value))
	r.changed = true
}

// RemoveRuleDeclaration removes property from r's declarations through
// its CSSOM style.
func (s *StyleSheetContents) RemoveRuleDeclaration(r *CSSOMRule, property string) {
	s.RuleDeclarations(r).RemoveDeclaration(property)
	r.changed = true
}

// InsertRule is CSSStyleSheet::insertRule: it inserts rule, which must parse
// as exactly one rule, before the rule at index, and returns index.
func (s *StyleSheetContents) InsertRule(rule string, index int) (int, error) {
	texts := s.currentRuleTexts()
	if index < 0 || index > len(texts) {
		return 0, &RuleMutationError{"IndexSizeError", "the index is larger than the rule count"}
	}
	text, ok := s.parseOneRule(rule)
	if !ok {
		return 0, &RuleMutationError{"SyntaxError", "failed to parse the rule " + rule}
	}
	kind := ruleKindOf(text)
	if !canInsertRule(texts, kind, index) {
		if kind == ruleKindNamespace {
			return 0, &RuleMutationError{"InvalidStateError", "failed to insert the rule"}
		}
		return 0, &RuleMutationError{"HierarchyRequestError", "failed to insert the rule"}
	}
	s.rules = slices.Insert(s.rules, index, &CSSOMRule{})
	s.reparse(slices.Insert(texts, index, text))
	return index, nil
}

// DeleteRule is CSSStyleSheet::deleteRule: it removes the rule at index.
func (s *StyleSheetContents) DeleteRule(index int) error {
	texts := s.currentRuleTexts()
	if index < 0 || index >= len(texts) {
		return &RuleMutationError{"IndexSizeError", "the index is not a rule's"}
	}
	if layers, imports, namespaces := leadingRuleCounts(texts); index >= layers+imports && index < layers+imports+namespaces && layers+imports+namespaces < len(texts) {
		// StyleSheetContents::WrapperDeleteRule (style_sheet_contents.cc:
		// 397-406) refuses to delete a namespace rule while child_rules_
		// is non-empty.
		return &RuleMutationError{"InvalidStateError", "failed to delete the rule"}
	}
	s.rules = slices.Delete(s.rules, index, index+1)
	s.reparse(slices.Delete(texts, index, index+1))
	return nil
}

// parseOneRule is CSSParser::ParseRule's validity (css_parser_impl.cc:
// 457-485): rule, comments stripped and url()s resolved against the
// sheet's base, is exactly one rule that parses, with nothing after it. A
// block left open at EOF closes, as the parser's block guard closes it
// (splitRules closes it). It returns the rule's text.
func (s *StyleSheetContents) parseOneRule(rule string) (string, bool) {
	text := strings.TrimSpace(s.Context.RewriteURLs(stripTopLevelCDOTokens(StripComments(rule))))
	parts := splitRules(text)
	if len(parts) != 1 {
		return "", false
	}
	part := strings.TrimSpace(parts[0])
	if !strings.HasPrefix(part, text) || !isValidTopLevelRule(part) {
		return "", false
	}
	return part, true
}

// isValidTopLevelRule reports whether text parses as a rule Blink keeps in
// the sheet's rule list: an at-rule louis14 parses (a valid @namespace,
// an @import with a URI: ConsumeImportRule's null rule has none), or a
// style rule whose selector list is all valid selectors.
func isValidTopLevelRule(text string) bool {
	if strings.HasPrefix(text, "@") {
		if !slices.ContainsFunc(insertableAtRules, func(kw string) bool { return hasAtKeyword(text, kw) }) {
			return false
		}
		switch ruleKindOf(text) {
		case ruleKindNamespace:
			_, _, ok := consumeNamespaceRule(text)
			return ok
		case ruleKindImport:
			_, _, ok := consumeStringOrURI(statementAtRulePrelude(text, "@import"))
			return ok
		}
		return true
	}
	// The block opens at the first `{` outside strings and `(` / `[`
	// blocks (blockBounds).
	openPos, _, ok := blockBounds(text)
	if !ok || !strings.HasSuffix(text, "}") || strings.TrimSpace(text[:openPos]) == "" {
		return false
	}
	for _, sel := range splitSelectorGroup(strings.TrimSpace(text[:openPos])) {
		if sel = strings.TrimSpace(sel); sel == "" || !isValidSelector(sel) {
			return false
		}
	}
	return true
}

// leadingRuleCounts splits texts as StyleSheetContents does: the leading
// @layer statements (pre_import_layer_statement_rules_), then the @import
// rules, then the @namespace rules; the rest are child_rules_. Leading
// @layer statements of a sheet with no @import or @namespace are child
// rules (style_sheet_contents.cc:291-295).
func leadingRuleCounts(texts []string) (layers, imports, namespaces int) {
	count := func(from int, k ruleKind) int {
		n := 0
		for from+n < len(texts) && ruleKindOf(texts[from+n]) == k {
			n++
		}
		return n
	}
	layers = count(0, ruleKindLayerStatement)
	imports = count(layers, ruleKindImport)
	namespaces = count(layers+imports, ruleKindNamespace)
	if imports == 0 && namespaces == 0 {
		layers = 0
	}
	return layers, imports, namespaces
}

// canInsertRule is StyleSheetContents::WrapperInsertRule's ordering
// (style_sheet_contents.cc:287-370): leading @layer statements, then
// @import, then @namespace, then the other rules (leadingRuleCounts).
func canInsertRule(texts []string, kind ruleKind, index int) bool {
	layers, imports, namespaces := leadingRuleCounts(texts)
	hasOtherRules := layers+imports+namespaces < len(texts)
	if index < layers || (index == layers && kind == ruleKindLayerStatement) {
		return kind == ruleKindLayerStatement
	}
	index -= layers
	if index < imports || (index == imports && kind == ruleKindImport) {
		return kind == ruleKindImport
	}
	if kind == ruleKindImport {
		return false
	}
	index -= imports
	if index < namespaces || (index == namespaces && kind == ruleKindNamespace) {
		return kind == ruleKindNamespace && !hasOtherRules
	}
	return kind != ruleKindNamespace
}

// currentRuleTexts returns the sheet's top-level rule texts, a rule
// changed through its CSSOM style written back from its declarations. A
// rule with nested rules keeps its text: writing back only its
// declarations would drop them, so its CSSOM edits do not survive a later
// insertRule / deleteRule.
func (s *StyleSheetContents) currentRuleTexts() []string {
	sheet := s.ensureRules()
	if sheet == nil {
		return nil
	}
	texts := slices.Clone(sheet.RuleTexts)
	for i, r := range s.rules {
		if !r.changed {
			continue
		}
		// A rule with no nested block serializes from its declarations.
		openPos, closePos, ok := blockBounds(texts[i])
		if d := r.decls; d != nil && ok && indexTopLevel(texts[i][openPos+1:closePos], '{') < 0 {
			texts[i] = strings.TrimSpace(texts[i][:openPos]) + " { " + d.cssText() + " }"
		}
	}
	return texts
}

// reparse makes texts the sheet's rules and parses it again.
func (s *StyleSheetContents) reparse(texts []string) {
	sheet, err := parseResolvedStylesheet(strings.Join(texts, "\n"), s.Context)
	s.mu.Lock()
	setImplicitScopeOwners(sheet, s.Owner)
	s.parsed, s.parseErr = sheet, err
	s.mu.Unlock()
	for _, r := range s.rules {
		r.changed = false
	}
	if sheet != nil {
		s.bindDeclarations(sheet)
	}
}

// cssText serializes the block's declarations, a property set by a
// pending substitution as its shorthand's value, once.
func (d *DeclarationResult) cssText() string {
	var out []string
	seen := map[string]bool{}
	for _, key := range d.Order {
		v := d.Declarations[key]
		name := strings.TrimPrefix(key, "_")
		if shorthand, value, pending := parsePendingSubstitution(v); pending {
			if seen[shorthand] {
				continue
			}
			seen[shorthand] = true
			name, v = shorthand, value
		}
		decl := name + ": " + v
		if d.Important[key] {
			decl += " !important"
		}
		out = append(out, decl+";")
	}
	return strings.Join(out, " ")
}
