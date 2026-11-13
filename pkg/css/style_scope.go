package css

import (
	"slices"
	"strings"

	"louis14/pkg/html"
)

// StyleScope mirrors Blink's StyleScope (core/css/style_scope.h:23-85 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the scoping roots (`from`,
// the <scope-start> selector list) and scoping limits (`to`, the
// <scope-end> list) of an @scope rule, and the enclosing @scope (`parent`,
// set by RuleSet::AddChildRules via CopyWithParent, rule_set.cc:1057-1064).
// A StyleScope with no <scope-start> is implicit: its roots are the parent
// elements of the stylesheet's owner nodes (IsImplicit, style_scope.h:77).
type StyleScope struct {
	from   []Selector // nil for an implicit scope
	to     []Selector // nil when the scope has no limits
	parent *StyleScope

	// owner is the owner node of an implicit scope's stylesheet, set by
	// setImplicitScopeOwners; its parent element is the scope's root when
	// styles are computed (ImplicitScopeTrigger,
	// scoped_style_resolver.cc:444-454). Blink records the trigger on the
	// element's StyleScopeData when the sheet becomes active instead.
	// Written once, on a freshly parsed sheet before StyleSheetContents
	// publishes it under its mutex; matching reaches the sheet only through
	// ParsedStylesheet, which takes that mutex, so reads need no lock.
	owner *html.Node
}

// IsImplicit mirrors Blink StyleScope::IsImplicit (style_scope.h:77).
func (s *StyleScope) IsImplicit() bool { return s.from == nil }

// consumeStyleScope parses an @scope prelude, `[(<scope-start>)]? [to
// (<scope-end>)]?`, mirroring StyleScope::Consume (style_scope.cc:86-139)
// plus CSSParserImpl::ConsumeScopeRule (css_parser_impl.cc:2135-2152): an
// empty prelude is an implicit scope, and anything left over (an invalid
// selector list, `to` without a list) makes the rule invalid (ok false).
func consumeStyleScope(prelude string) (scope *StyleScope, ok bool) {
	rest := strings.TrimSpace(prelude)
	scope = &StyleScope{}
	if strings.HasPrefix(rest, "(") {
		if scope.from, rest, ok = consumeScopeBoundary(rest, false); !ok {
			return nil, false
		}
	}
	if after, isTo := consumeKeyword(rest, "to"); isTo {
		if after = strings.TrimSpace(after); strings.HasPrefix(after, "(") {
			// <scope-end> is parsed relative to the scope
			// (CSSNestingType::kScope, style_scope.cc:126-128).
			if scope.to, rest, ok = consumeScopeBoundary(after, true); !ok {
				return nil, false
			}
		}
	}
	return scope, rest == ""
}

// consumeScopeBoundary consumes `(<selector-list>)` and returns the parsed
// list and the trimmed rest (ConsumeEnclosedBoundary, style_scope.cc:66-82).
func consumeScopeBoundary(s string, relativeToScope bool) (selectors []Selector, rest string, ok bool) {
	list, rest, ok := consumeBalancedParens(s)
	if !ok {
		return nil, "", false
	}
	selectors, ok = parseScopeBoundarySelectors(list, relativeToScope)
	return selectors, strings.TrimSpace(rest), ok
}

// parseScopeBoundarySelectors parses a <scope-start> or <scope-end>
// selector list (CSSSelectorParser::ParseScopeBoundary). An `&` has no
// parent rule (nestingParentPseudo): in <scope-end> because
// CSSScopeifiedParentPseudoClass is stable (style_scope.cc:112-121), and
// in a top-level <scope-start>. The list is not forgiving: one invalid
// selector, or one with a pseudo-element (DisallowPseudoElementsScope,
// css_selector_parser.cc:190 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// drops it.
//
// A list parsed relative to the scope (CSSNestingType::kScope, as
// <scope-end> is) gives each selector without :scope the implicit :scope
// anchor (scopedSelectorText).
func parseScopeBoundarySelectors(list string, relativeToScope bool) ([]Selector, bool) {
	var selectors []Selector
	for _, sel := range splitSelectorGroup(list) {
		sel = strings.TrimSpace(sel)
		if !isValidSelector(sel) {
			return nil, false
		}
		if relativeToScope {
			sel = scopedSelectorText(sel)
		}
		// ParseScopeBoundary disallows pseudo-elements
		// (DisallowPseudoElementsScope, css_selector_parser.cc:190 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): one an :is() or
		// :where() does not enclose drops the boundary.
		parsed := parseSelector(sel)
		if unforgiven, _ := scanPseudoElements(sel); parsed.PseudoElement != "" || unforgiven {
			return nil, false
		}
		selectors = append(selectors, parsed)
	}
	return selectors, len(selectors) > 0
}

// anchoredToScope gives each selector without :scope the implicit :scope
// descendant anchor (scopedSelectorText): a selector list
// parsed with CSSNestingType::kScope.
func anchoredToScope(selectors []Selector) []Selector {
	out := make([]Selector, len(selectors))
	for i, sel := range selectors {
		out[i] = sel
		if text := scopedSelectorText(sel.Raw); text != sel.Raw {
			out[i] = parseSelector(text)
		}
	}
	return out
}

// scopedSelectorText prepends the implicit :scope descendant anchor to a
// selector parsed relative to a scope that does not contain :scope, as
// Blink's MaybeCreateImplicitDescendantAnchor does per complex selector
// (css_selector_parser.cc:800-823, called at :914-930 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The anchor is implicit
// (CreateImplicitAnchor, :786-797): it adds no specificity and takes no
// default namespace, written `*|*&` (a parentless & matches like :scope with
// no specificity; nestingParentPseudo). A
// selector starting with a combinator (`> .a`) is relative to the anchor
// the same way (ConsumeNestedRelativeSelector).
//
// A selector containing `&` counts as :scope-containing too
// (GetNestingTypeForSelectorList: nest-containing implies kScope).
func scopedSelectorText(sel string) string {
	if strings.Contains(sel, "&") || containsScopePseudo(sel) {
		return sel
	}
	return "*|*& " + sel
}

// containsScopePseudo reports whether a selector contains :scope anywhere,
// including inside functional pseudo-class arguments
// (GetNestingTypeForSelectorList, css_selector_parser.cc:727-752).
func containsScopePseudo(sel string) bool {
	lower := strings.ToLower(sel)
	for i := 0; ; {
		j := strings.Index(lower[i:], ":scope")
		if j < 0 {
			return false
		}
		end := i + j + len(":scope")
		if end == len(sel) || !isIdentContinue(sel[end]) {
			return true
		}
		i = end
	}
}

// styleScopeActivation mirrors Blink's StyleScopeActivation
// (core/css/style_scope_frame.h): a scoping root, and the number of
// generations between it and the element the activation is for. A nil
// root is the document.
type styleScopeActivation struct {
	root      *html.Node
	proximity uint32
}

// unscopedProximity is the proximity of an unscoped match, and of the
// default activation (DefaultActivations, selector_checker.cc:3876-3881).
const unscopedProximity = ^uint32(0)

// defaultActivations is the outer activation of an outermost scope: the
// document (DefaultActivations, selector_checker.cc:3876-3881). Read only.
var defaultActivations = []styleScopeActivation{{proximity: unscopedProximity}}

// styleScopeFrame mirrors Blink's StyleScopeFrame
// (core/css/style_scope_frame.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the activations of each scope for one element during one cascade pass,
// chained to the parent element's frame so CalculateActivations reuses the
// parent's results instead of walking to the root again. A nil frame
// caches nothing (a style computed outside the cascade pass).
type styleScopeFrame struct {
	element *html.Node
	parent  *styleScopeFrame
	data    map[*StyleScope][]styleScopeActivation
}

// newStyleScopeFrame is element's frame, chained to its parent's.
func newStyleScopeFrame(element *html.Node, parent *styleScopeFrame) *styleScopeFrame {
	return &styleScopeFrame{element: element, parent: parent}
}

// parentFrameFor is the frame of parent, f's element's parent element,
// when the chain has it (StyleScopeFrame::GetParentFrameOrNull).
func (f *styleScopeFrame) parentFrameFor(parent *html.Node) *styleScopeFrame {
	if f == nil || f.parent == nil || f.parent.element != parent {
		return nil
	}
	return f.parent
}

// ensureActivations mirrors SelectorChecker::EnsureActivations
// (selector_checker.cc:3918-3964 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the activations of scope for element, within the activations of the
// enclosing scope (or the default activation, the document), cached on
// element's frame.
func ensureActivations(element *html.Node, scope *StyleScope, frame *styleScopeFrame) []styleScopeActivation {
	outer := defaultActivations
	if scope.parent != nil {
		outer = ensureActivations(element, scope.parent, frame)
	}
	return calculateActivations(element, scope, outer, frame)
}

// calculateActivations mirrors SelectorChecker::CalculateActivations
// (selector_checker.cc:3967-4040): the parent element's activations stay
// active one generation further out unless element is a scoping limit,
// and element adds an activation of its own when it is a scoping root
// within an outer activation. Activations are in decreasing proximity.
// With element's frame, the result is cached there and the parent's comes
// from the parent's frame, as Blink caches it (:3974-3985, :4044-4047).
func calculateActivations(element *html.Node, scope *StyleScope, outer []styleScopeActivation, frame *styleScopeFrame) []styleScopeActivation {
	if cached, ok := frame.cached(scope); ok {
		return cached
	}
	activations := computeActivations(element, scope, outer, frame)
	frame.store(scope, activations)
	return activations
}

// cached is the frame's activations of scope, if computed.
func (f *styleScopeFrame) cached(scope *StyleScope) ([]styleScopeActivation, bool) {
	if f == nil {
		return nil, false
	}
	a, ok := f.data[scope]
	return a, ok
}

// store caches the activations of scope on the frame (none on nil).
func (f *styleScopeFrame) store(scope *StyleScope, activations []styleScopeActivation) {
	if f == nil {
		return
	}
	if f.data == nil {
		f.data = map[*StyleScope][]styleScopeActivation{}
	}
	f.data[scope] = activations
}

// computeActivations is calculateActivations without the cache.
func computeActivations(element *html.Node, scope *StyleScope, outer []styleScopeActivation, frame *styleScopeFrame) []styleScopeActivation {
	if len(outer) == 0 {
		return nil
	}
	var activations []styleScopeActivation
	// Remain within the outer scope: don't look above the highest outer
	// activation's root (ActivationCeiling, selector_checker.cc:3896-3905).
	if element != outer[0].root {
		if parent := parentElement(element); parent != nil {
			for _, a := range calculateActivations(parent, scope, outer, frame.parentFrameFor(parent)) {
				if !elementIsScopingLimit(scope, a, element) {
					activations = append(activations, styleScopeActivation{a.root, a.proximity + 1})
				}
			}
		}
	}
	// Element is a new scoping root: its implicit scope's trigger, or a
	// <scope-start> match within an outer activation.
	var isRoot bool
	if scope.IsImplicit() {
		isRoot = hasImplicitRoot(scope, element)
	} else {
		isRoot = slices.ContainsFunc(outer, func(o styleScopeActivation) bool {
			return matchesWithScope(element, scope.from, o.root)
		})
	}
	// A new activation can be limited at once (@scope (.x) to (.x)).
	if a := (styleScopeActivation{root: element}); isRoot && !elementIsScopingLimit(scope, a, element) {
		activations = append(activations, a)
	}
	return activations
}

// hasImplicitRoot mirrors HasImplicitRoot (selector_checker.cc:3909-3916):
// element is the implicit scope's trigger.
func hasImplicitRoot(scope *StyleScope, element *html.Node) bool {
	return scope.owner != nil && element == parentElement(scope.owner)
}

// setImplicitScopeOwners records the sheet's owner node on every implicit
// scope of sheet's rules, including an implicit scope only reached as
// another's parent (ForEachImplicitScopeTrigger,
// scoped_style_resolver.cc:456-475 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A sheet with no owner, or an
// owner with no parent element (a sheet linked by a processing
// instruction), gives its implicit scopes no root.
func setImplicitScopeOwners(sheet *Stylesheet, owner *html.Node) {
	if sheet == nil || owner == nil {
		return
	}
	for i := range sheet.Rules {
		for scope := sheet.Rules[i].Scope; scope != nil; scope = scope.parent {
			if scope.IsImplicit() {
				scope.owner = owner
			}
		}
	}
}

// parseScopeRule parses an @scope rule into its style rules, each scoped
// to it (CSSParserImpl::ConsumeScopeRule, css_parser_impl.cc:2135-2172 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A rule nested in it keeps
// its own scope, now inside this one (StyleScope::CopyWithParent at
// RuleSet::AddChildRules, rule_set.cc:1057-1064). An invalid prelude
// drops the rule.
func parseScopeRule(ruleStr string, ss *Stylesheet) []Rule {
	bracePos, closePos, ok := blockBounds(ruleStr)
	if !ok {
		return nil
	}
	prelude := strings.TrimSpace(ruleStr[:bracePos])
	scope, ok := consumeStyleScope(prelude[len("@scope"):])
	if !ok {
		return nil
	}
	rules := parseConditionalBody(ruleStr[bracePos+1:closePos], ss)
	copies := map[*StyleScope]*StyleScope{}
	// The body's style rules carry their implicit :scope anchors already
	// (anchorScopeBody, before CSS nesting).
	for i := range rules {
		rules[i].Scope = scopeWithin(rules[i].Scope, scope, copies)
	}
	return rules
}

// scopedRuleText gives each selector of a style rule in an @scope body its
// implicit :scope anchor (scopedSelectorText), before CSS nesting expands
// the rule, so `&` in its nested rules carries the anchor (Blink adds it
// when the outer rule is parsed).
func scopedRuleText(rule string) string {
	brace := indexTopLevelBrace(rule)
	if brace < 0 {
		return rule
	}
	return mapSelectorGroup(rule[:brace], scopedSelectorText) + " " + rule[brace:]
}

// mapSelectorGroup applies f to each selector of a selector list, trimmed,
// and joins the results back into a list.
func mapSelectorGroup(list string, f func(string) string) string {
	branches := splitSelectorGroup(list)
	for i, sel := range branches {
		branches[i] = f(strings.TrimSpace(sel))
	}
	return strings.Join(branches, ", ")
}

// scopeWithin places inner (nil for an unscoped rule) inside outer: the
// outermost scope of inner's chain gets outer as its parent. Changed
// scopes are copies (StyleScope::CopyWithParent, style_scope.cc:28-32),
// shared across the body's rules through copies. A nested scope's
// <scope-start> is parsed relative to the outer scope (kScope), so it gets
// the implicit :scope anchor.
func scopeWithin(inner, outer *StyleScope, copies map[*StyleScope]*StyleScope) *StyleScope {
	if inner == nil {
		return outer
	}
	if c, ok := copies[inner]; ok {
		return c
	}
	c := *inner
	if inner.parent == nil {
		c.parent = outer
		if !c.IsImplicit() {
			c.from = anchoredToScope(c.from)
		}
	} else {
		c.parent = scopeWithin(inner.parent, outer, copies)
	}
	copies[inner] = &c
	return &c
}

// elementIsScopingLimit mirrors SelectorChecker::ElementIsScopingLimit
// (selector_checker.cc:4086-4098).
func elementIsScopingLimit(scope *StyleScope, a styleScopeActivation, element *html.Node) bool {
	return scope.to != nil && matchesWithScope(element, scope.to, a.root)
}

// matchesWithScope mirrors SelectorChecker::MatchesWithScope
// (selector_checker.cc:4042-4069): element matches one of selectors with
// :scope as root.
func matchesWithScope(element *html.Node, selectors []Selector, root *html.Node) bool {
	return slices.ContainsFunc(selectors, func(sel Selector) bool {
		return MatchesSelectorInScope(element, sel, root)
	})
}

// parentElement is element's parent element, nil at the root (Blink
// Element::ParentOrShadowHostElement; louis14 has no shadow hosts).
func parentElement(element *html.Node) *html.Node {
	if p := element.Parent; p != nil && p.Type == html.ElementNode && p.TagName != "document" {
		return p
	}
	return nil
}

// matchRule matches rule's selector against node, within rule's @scope if
// it has one, and returns the matched copy with its proximity. Mirrors
// SelectorChecker::MatchForScopeActivation (selector_checker.cc:1037-1075):
// the selector is matched against each activation's root, the most
// proximate first, and the first match sets the proximity.
//
// frame is node's styleScopeFrame during the cascade pass, nil elsewhere.
func matchRule(node *html.Node, rule *Rule, frame *styleScopeFrame) (Rule, bool) {
	if rule.Scope == nil {
		if !MatchesSelector(node, rule.Selector) {
			return Rule{}, false
		}
		return *rule, true
	}
	activations := ensureActivations(node, rule.Scope, frame)
	for i := len(activations) - 1; i >= 0; i-- {
		if MatchesSelectorInScope(node, rule.Selector, activations[i].root) {
			matched := *rule
			matched.proximity = activations[i].proximity
			return matched, true
		}
	}
	return Rule{}, false
}
