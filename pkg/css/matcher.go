package css

import (
	"strings"

	xbidi "golang.org/x/text/unicode/bidi"

	"louis14/pkg/html"
)

// Phase 3: Selector matching

// Phase 17: MatchesSelector returns true if the node matches the complex selector
func MatchesSelector(node *html.Node, selector Selector) bool {
	if node.Type != html.ElementNode {
		return false
	}

	// Handle complex selector (with multiple parts and combinators)
	if len(selector.Parts) == 0 {
		return false
	}

	// Start matching from the rightmost part (the target element)
	return matchesCompoundSelector(node, selector, len(selector.Parts)-1)
}

// matchesCompoundSelector checks if the node matches the selector at the given part index
// and all ancestor requirements
func matchesCompoundSelector(node *html.Node, selector Selector, partIndex int) bool {
	// Match the current part against the node
	if !matchesSelectorPart(node, selector.Parts[partIndex]) {
		return false
	}

	// If this is the first part, we're done
	if partIndex == 0 {
		return true
	}

	// Check the combinator with the previous part
	combinator := selector.Combinators[partIndex-1]
	prevPartIndex := partIndex - 1

	switch combinator {
	case DescendantCombinator:
		// Match any ancestor
		return matchesAncestor(node, selector, prevPartIndex)

	case ChildCombinator:
		// Match direct parent only (skip synthetic document node)
		if node.Parent != nil && node.Parent.TagName != "document" {
			return matchesCompoundSelector(node.Parent, selector, prevPartIndex)
		}
		return false

	case AdjacentSiblingCombinator:
		// Match immediate previous sibling
		prevSibling := getPreviousSibling(node)
		if prevSibling != nil {
			return matchesCompoundSelector(prevSibling, selector, prevPartIndex)
		}
		return false

	case GeneralSiblingCombinator:
		// Match any previous sibling
		return matchesPreviousSibling(node, selector, prevPartIndex)
	}

	return false
}

// matchesSelectorPart checks if a node matches a single selector part
func matchesSelectorPart(node *html.Node, part SelectorPart) bool {
	// Match element
	if part.Element != "" && part.Element != "*" {
		if node.TagName != part.Element {
			return false
		}
	}

	// Match ID
	if part.ID != "" {
		if id, ok := node.GetAttribute("id"); !ok || id != part.ID {
			return false
		}
	}

	// Match classes
	if len(part.Classes) > 0 {
		classAttr, ok := node.GetAttribute("class")
		if !ok {
			return false
		}
		nodeClasses := strings.Split(classAttr, " ")
		for _, requiredClass := range part.Classes {
			found := false
			for _, nodeClass := range nodeClasses {
				if strings.TrimSpace(nodeClass) == requiredClass {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	// Match attributes
	for _, attrSel := range part.Attributes {
		if !matchesAttributeSelector(node, attrSel) {
			return false
		}
	}

	// Pseudo-classes
	for _, pc := range part.PseudoClasses {
		if !matchesPseudoClass(node, pc) {
			return false
		}
	}

	return true
}

// matchesAttributeSelector checks if a node matches an attribute selector
func matchesAttributeSelector(node *html.Node, attr AttributeSelector) bool {
	value, ok := node.GetAttribute(attr.Name)
	if !ok {
		return false
	}

	// If no operator, just check existence
	if attr.Operator == "" {
		return true
	}

	switch attr.Operator {
	case "=":
		// Exact match
		return value == attr.Value
	case "^=":
		// Starts with
		return strings.HasPrefix(value, attr.Value)
	case "$=":
		// Ends with
		return strings.HasSuffix(value, attr.Value)
	case "*=":
		// Contains
		return strings.Contains(value, attr.Value)
	case "~=":
		// Word match (whitespace-separated)
		words := strings.Fields(value)
		for _, word := range words {
			if word == attr.Value {
				return true
			}
		}
		return false
	case "|=":
		// Language prefix (starts with value or value-)
		return value == attr.Value || strings.HasPrefix(value, attr.Value+"-")
	}

	return false
}

// matchesAncestor checks if any ancestor matches the selector part
func matchesAncestor(node *html.Node, selector Selector, partIndex int) bool {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type == html.ElementNode && ancestor.TagName != "document" {
			if matchesCompoundSelector(ancestor, selector, partIndex) {
				return true
			}
		}
	}
	return false
}

// matchesPreviousSibling checks if any previous sibling matches the selector part
func matchesPreviousSibling(node *html.Node, selector Selector, partIndex int) bool {
	for sibling := getPreviousSibling(node); sibling != nil; sibling = getPreviousSibling(sibling) {
		if matchesCompoundSelector(sibling, selector, partIndex) {
			return true
		}
	}
	return false
}

// getPreviousSibling returns the previous element sibling of a node
func getPreviousSibling(node *html.Node) *html.Node {
	if node.Parent == nil {
		return nil
	}

	foundCurrent := false
	var prevElement *html.Node

	for _, sibling := range node.Parent.Children {
		if sibling == node {
			foundCurrent = true
			break
		}
		if sibling.Type == html.ElementNode {
			prevElement = sibling
		}
	}

	if foundCurrent {
		return prevElement
	}
	return nil
}

// matchesPseudoClass checks if a node matches a given pseudo-class.
func matchesPseudoClass(node *html.Node, pc string) bool {
	switch {
	case pc == "first-child":
		return isNthChild(node, 1)
	case pc == "last-child":
		return isLastChild(node)
	case pc == "only-child":
		return isNthChild(node, 1) && isLastChild(node)
	case pc == "root":
		if node.Parent == nil || node.Parent.TagName != "document" {
			return false
		}
		for _, child := range node.Parent.Children {
			if child.Type == html.ElementNode {
				return child == node
			}
		}
		return false
	case pc == "empty":
		return len(node.Children) == 0
	case strings.HasPrefix(pc, "nth-child("):
		arg := pc[len("nth-child(") : len(pc)-1] // strip "nth-child(" and ")"
		anb, ofSel, hasOf := splitNthChildArg(arg)
		if hasOf {
			return matchesNthChildOf(node, anb, ofSel, false)
		}
		return matchesNthChild(node, anb)
	case strings.HasPrefix(pc, "not("):
		arg := pc[len("not(") : len(pc)-1] // strip "not(" and ")"
		// Split by comma (paren-aware) to support selector lists: :not(A, B, C)
		// Element must NOT match ANY of the selectors in the list
		selectors := splitSelectorGroup(strings.TrimSpace(arg))
		for _, sel := range selectors {
			sel = strings.TrimSpace(sel)
			if sel == "" {
				continue
			}
			innerSel := ParseSelector(sel)
			if len(innerSel.Parts) > 0 && matchesSelectorPart(node, innerSel.Parts[len(innerSel.Parts)-1]) {
				return false // Element matches one of the :not() selectors → fail
			}
		}
		return true // Element doesn't match any → passes :not()
	case pc == "hover", pc == "active":
		// Dynamic pseudo-classes never match in a static renderer
		return false
	case pc == "focus":
		// Mirrors Blink's SelectorChecker::CheckPseudoFocus, which consults
		// the per-Element IsFocused bit maintained by
		// Document::SetFocusedElement. louis14 sets that bit via the JS
		// Element.focus() binding in pkg/js/dom.go.
		return node.IsFocused
	case pc == "visited":
		// :visited reflects browsing history, which a static renderer has no
		// general model for — so an arbitrary link is treated as unvisited.
		// But a hyperlink with an *empty* href is defined to resolve to the
		// current document's own URL, which is unconditionally in history
		// whenever that document is being rendered. So `<a href="">` always
		// matches :visited; this is exact, not a heuristic. A fragment href
		// ("#name") resolves to current-URL-plus-fragment — a distinct URL
		// that a reftest run did not navigate to — so it stays unvisited.
		switch node.TagName {
		case "a", "area", "link":
			href, ok := node.GetAttribute("href")
			if !ok {
				return false
			}
			return strings.TrimSpace(href) == ""
		}
		return false
	case pc == "focus-visible":
		// :focus-visible matches when the UA decides the focus indicator
		// should be drawn. In a static reftest engine driven only by JS
		// Element.focus(), the UA never has a heuristic input to mark focus
		// as "visible" (no keyboard tab, no script-focus-after-key-event
		// signal). Match nothing — same conservative answer Blink gives
		// when there's no keyboard input modality.
		return false
	case pc == "focus-within":
		// Mirrors Blink's SelectorChecker::CheckPseudoFocusWithin, which
		// consults the per-Element HasFocusWithin bit maintained by
		// Document::SetFocusedElement walking up from the focused element.
		// louis14 sets that bit via the JS Element.focus() binding which
		// calls Document.SetFocusedElement.
		return node.HasFocusWithin
	case pc == "target":
		// :target matches element with matching URL fragment — not available in static renderer
		return false
	case pc == "placeholder-shown":
		// :placeholder-shown matches an <input> or <textarea> whose effective
		// "show placeholder" flag is set: the element accepts placeholder, has
		// a non-empty `placeholder` attribute, and the current value is empty.
		// Per HTML "the placeholder IDL attribute" and CSS Selectors 4 §6.4.
		// Mirrors Blink core/html/forms/html_input_element.cc::
		// ShouldShowPlaceholder @ 4883d11fef4a.
		return matchesPlaceholderShown(node)
	case pc == "required":
		// :required matches input/select/textarea with the `required` attribute.
		// Per CSS Selectors 4 §6.5 and HTML form-control validity. Mirrors
		// Blink core/html/forms/html_form_control_element.cc::IsRequiredFormControl
		// @ 4883d11fef4a.
		return canBeRequired(node) && hasAttribute(node, "required")
	case pc == "optional":
		// :optional is the negation of :required, restricted to controls that
		// can have a required state. CSS Selectors 4 §6.5.
		return canBeRequired(node) && !hasAttribute(node, "required")
	case pc == "read-write":
		// :read-write matches a user-editable control. For an <input> or
		// <textarea>, that means not disabled and not readonly (and for input,
		// a type that supports text editing). For contenteditable elements,
		// see the contenteditable branch. CSS Selectors 4 §6.6.
		return matchesReadWrite(node)
	case pc == "read-only":
		// :read-only matches anything that is not :read-write. Per spec,
		// elements that cannot be either are :read-only by default.
		return !matchesReadWrite(node)
	case pc == "autofill", pc == "-webkit-autofill":
		// Autofill state: never applies in static renderer
		return false
	case pc == "paused", pc == "playing":
		// Animation state pseudo-classes: not applicable in static renderer
		return false
	case pc == "muted", pc == "volume-locked":
		// Media element state: not applicable in static renderer
		return false
	case pc == "local-link":
		// :local-link matches anchors linking to current page — can't determine in static render
		return false
	case pc == "any-link", pc == "-webkit-any-link":
		// :any-link matches <a>, <area>, <link> elements that have an href attribute
		if node.Type != html.ElementNode {
			return false
		}
		tag := node.TagName
		if tag != "a" && tag != "area" && tag != "link" {
			return false
		}
		_, hasHref := node.GetAttribute("href")
		return hasHref
	case pc == "scope":
		// :scope matches the element serving as the scope reference.
		// In a static document context, approximate as :root (first element child of document).
		if node.Parent == nil || node.Parent.TagName != "document" {
			return false
		}
		for _, child := range node.Parent.Children {
			if child.Type == html.ElementNode {
				return child == node
			}
		}
		return false
	case pc == "link":
		// Per CSS Selectors 4 §6.6.1, `:link` matches a non-visited hyperlink
		// and `:visited` matches a visited one — they are mutually exclusive
		// in general matching, but §6.6.1.1 carves out a privacy exception
		// for `:has()`: inside `:has()`, `:link` must match ANY hyperlink and
		// `:visited` must never match (to avoid history-leak side channels).
		//
		// louis14 treats `<a href="">` as the only :visited link (its href
		// resolves to the current document URL, unconditionally in history).
		// We can't distinguish "is inside :has()" cheaply at this layer, so
		// we ALSO treat the visited link as matching :link — which is the
		// spec-mandated behavior inside `:has()` and the simplest
		// pre-:has()-aware approximation for general matching (it preserves
		// existing test passes that rely on `:link` matching any hyperlink).
		switch node.TagName {
		case "a", "area", "link":
			_, hasHref := node.GetAttribute("href")
			return hasHref
		}
		return false
	case strings.HasPrefix(pc, "is("):
		// :is() matches if ANY selector in the comma-separated list matches.
		// Per CSS Selectors 4 §3.7, pseudo-elements are not allowed inside
		// :is(); a branch containing one is invalid. Per CSS Nesting spec,
		// when nesting expansion produces an :is() that contains a pseudo-
		// element argument, the resulting selector is contextually invalid
		// for element matching. We apply the same rule to authored :is()
		// containing a pseudo-element (matches Blink behavior for tests like
		// `:is(*, ::before) *`).
		arg := pc[len("is(") : len(pc)-1]
		return matchesIsLike(node, arg)
	case strings.HasPrefix(pc, "where("):
		// :where() is identical to :is() but with zero specificity. Same
		// contextual-invalidation rule applies for pseudo-element arguments.
		arg := pc[len("where(") : len(pc)-1]
		return matchesIsLike(node, arg)
	case strings.HasPrefix(pc, "has("):
		arg := pc[len("has(") : len(pc)-1]
		// Per CSS Selectors 4 §17.1.4 and CSSWG resolution
		// (https://github.com/w3c/csswg-drafts/issues/9600), :has() inside
		// :has() is invalid — the whole selector matches nothing. Detect
		// this at the matcher (rather than the parser) so nesting
		// expansion (which can produce :has(> :is(... :has(...) ...)))
		// is handled uniformly with author-written nested :has(). Mirrors
		// Blink's CSSSelectorParser::RestrictedWhere / has-pseudo gating
		// at 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		if containsHas(arg) {
			return false
		}
		return matchesHas(node, strings.TrimSpace(arg))
	case pc == "first-of-type":
		return nthOfTypeIndex(node) == 1
	case pc == "last-of-type":
		return nthOfTypeIndex(node) == countOfType(node)
	case pc == "only-of-type":
		return countOfType(node) == 1
	case strings.HasPrefix(pc, "nth-of-type("):
		arg := pc[len("nth-of-type(") : len(pc)-1]
		return matchesAnPlusB(nthOfTypeIndex(node), arg)
	case strings.HasPrefix(pc, "nth-last-of-type("):
		arg := pc[len("nth-last-of-type(") : len(pc)-1]
		posFromEnd := countOfType(node) - nthOfTypeIndex(node) + 1
		return matchesAnPlusB(posFromEnd, arg)
	case strings.HasPrefix(pc, "nth-last-child("):
		arg := pc[len("nth-last-child(") : len(pc)-1]
		anb, ofSel, hasOf := splitNthChildArg(arg)
		if hasOf {
			return matchesNthChildOf(node, anb, ofSel, true)
		}
		posFromEnd := totalElementChildren(node) - nthChildIndex(node) + 1
		return matchesAnPlusB(posFromEnd, anb)
	case pc == "enabled":
		return isFormElement(node) && !hasAttribute(node, "disabled")
	case pc == "disabled":
		return isFormElement(node) && hasAttribute(node, "disabled")
	case pc == "checked":
		return isCheckableElement(node) && hasAttribute(node, "checked")
	case strings.HasPrefix(pc, "dir(") && strings.HasSuffix(pc, ")"):
		// :dir(ltr) / :dir(rtl) matches if the element's directionality
		// matches the argument. Per Selectors 4 §15, directionality follows
		// the HTML "directionality of an element" algorithm.
		//
		// Scope: handles the static cases — explicit `dir=ltr`/`dir=rtl`
		// attributes, inheritance from ancestor, and default LTR. The
		// `dir=auto` case (which would require first-strong-Unicode
		// detection over text content) is treated as "no explicit
		// directionality on this element" and falls through to ancestor
		// inheritance / default LTR. Any value other than ltr/rtl on the
		// pseudo-class argument never matches (per the spec).
		arg := strings.ToLower(strings.TrimSpace(pc[len("dir(") : len(pc)-1]))
		switch arg {
		case "ltr":
			return elementDirectionality(node) == DirectionLTR
		case "rtl":
			return elementDirectionality(node) == DirectionRTL
		default:
			return false
		}
	default:
		return false
	}
}

// elementDirectionality returns the element's directionality per the HTML
// "directionality of an element" algorithm
// (https://html.spec.whatwg.org/#the-directionality):
//
//  1. If the element has a `dir` attribute of `ltr` or `rtl`, use it.
//  2. If the element has `dir=auto`, derive direction from a first-strong-
//     directional-character scan over descendant text content (HTML
//     "auto directionality" algorithm). If no strong character is found,
//     the element's own directionality is the parent's directionality (or
//     LTR as document default).
//  3. Otherwise (no `dir`, or an invalid value), inherit from the parent
//     element; document default is LTR.
//
// Mirrors Blink's Element::CachedDirectionality / ResolveAutoDirectionality
// (third_party/blink/renderer/core/dom/element.cc @ 4883d11fef4a). Without
// the per-element cache and invalidation graph Blink keeps — louis14's static
// renderer recomputes per :dir() query, which is correct but O(descendants)
// per match. For the WPT selectors corpus this is fine.
func elementDirectionality(node *html.Node) Direction {
	for n := node; n != nil; n = n.Parent {
		if n.Type != html.ElementNode {
			continue
		}
		dirAttr, ok := n.GetAttribute("dir")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(dirAttr)) {
		case "ltr":
			return DirectionLTR
		case "rtl":
			return DirectionRTL
		case "auto":
			if dir, found := autoDirectionality(n); found {
				return dir
			}
			// No strong-direction character found: continue walking
			// ancestors; if none give an answer, default LTR.
		}
		// Invalid values (e.g. "foopy"): inherit from ancestor.
	}
	return DirectionLTR
}

// autoDirectionality scans the element's descendant text content for the
// first character with a strong Unicode bidi class (L, R, or AL) and returns
// the corresponding direction. Per HTML "auto directionality" algorithm
// (https://html.spec.whatwg.org/#auto-directionality), descendant subtrees
// rooted at a `bdi`, `script`, `style`, `textarea` element, or any element
// that itself has a `dir` attribute (other than the starting element)
// are skipped. Returns (_, false) if no strong character is found.
//
// Mirrors Blink's HTMLElement::ResolveAutoDirectionality and the bidi-class
// walk in core/html/canvas/text_metrics.cc-derived helpers (Blink @
// 4883d11fef4a). Uses golang.org/x/text/unicode/bidi for the class lookup.
func autoDirectionality(root *html.Node) (Direction, bool) {
	var walk func(n *html.Node, isRoot bool) (Direction, bool)
	walk = func(n *html.Node, isRoot bool) (Direction, bool) {
		if n.Type == html.TextNode {
			for _, r := range n.Text {
				props, _ := xbidi.LookupRune(r)
				switch props.Class() {
				case xbidi.L:
					return DirectionLTR, true
				case xbidi.R, xbidi.AL:
					return DirectionRTL, true
				}
			}
			return DirectionLTR, false
		}
		if n.Type != html.ElementNode {
			return DirectionLTR, false
		}
		if !isRoot {
			// Skip descendant subtrees that contribute their own
			// directionality boundary (HTML spec).
			switch n.TagName {
			case "bdi", "script", "style", "textarea":
				return DirectionLTR, false
			}
			if _, has := n.GetAttribute("dir"); has {
				return DirectionLTR, false
			}
		}
		for _, c := range n.Children {
			if dir, found := walk(c, false); found {
				return dir, true
			}
		}
		return DirectionLTR, false
	}
	return walk(root, true)
}

// isNthChild returns true if the node is the nth element child (1-based).
func isNthChild(node *html.Node, n int) bool {
	if node.Parent == nil {
		return n == 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode {
			count++
			if c == node {
				return count == n
			}
		}
	}
	return false
}

// isLastChild returns true if the node is the last element child.
func isLastChild(node *html.Node) bool {
	if node.Parent == nil {
		return true
	}
	for i := len(node.Parent.Children) - 1; i >= 0; i-- {
		c := node.Parent.Children[i]
		if c.Type == html.ElementNode {
			return c == node
		}
	}
	return false
}

// matchesNthChild checks the An+B formula.
func matchesNthChild(node *html.Node, arg string) bool {
	arg = strings.TrimSpace(arg)

	if arg == "odd" {
		return nthChildIndex(node)%2 == 1
	}
	if arg == "even" {
		return nthChildIndex(node)%2 == 0
	}

	// Parse An+B; invalid arguments drop the selector (no element matches).
	a, b, ok := parseAnPlusBValid(arg)
	if !ok {
		return false
	}
	idx := nthChildIndex(node)
	if a == 0 {
		return idx == b
	}
	// Check if (idx - b) is divisible by a and non-negative
	diff := idx - b
	if a > 0 {
		return diff >= 0 && diff%a == 0
	}
	// a < 0: match when diff <= 0 and divisible
	return diff <= 0 && diff%a == 0
}

// splitNthChildArg parses a :nth-child / :nth-last-child argument into the
// An+B portion and the optional ` of <complex-selector-list>` portion, per
// CSS Selectors 4 §6.6.1. Mirrors the parser branch in Blink's
// CSSSelectorParser::ConsumeNth() at third_party/blink/renderer/core/css/
// parser/css_selector_parser.cc @ 4883d11fef4a, which extracts an optional
// selector_list payload on the pseudo-class CSSSelector.
//
// The `of` keyword is a CSS identifier token, so the split must respect
// token boundaries: an `of` that is part of a longer identifier (e.g.
// `software`) is NOT the keyword, but `of.x`, `of[x]`, `of#x`, `of:x` and
// `of *` are all valid splits because `.`/`[`/`#`/`:`/whitespace ends the
// identifier. The boundary before `of` must be whitespace or one of the
// An+B terminators (digit/`n`/`+`/`-`) because the only legal token
// preceding `of` per the grammar is a complete <an+b>.
//
// Returns (anPlusB, ofSelectorList, hasOf). When hasOf is false, ofSelectorList
// is empty and the caller should run the existing An+B-only path.
func splitNthChildArg(arg string) (string, string, bool) {
	depth := 0
	inString := byte(0)
	for i := 0; i < len(arg); i++ {
		ch := arg[i]
		if inString != 0 {
			if ch == '\\' && i+1 < len(arg) {
				i++
			} else if ch == inString {
				inString = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = ch
			continue
		}
		if ch == '(' || ch == '[' {
			depth++
			continue
		}
		if ch == ')' || ch == ']' {
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		// Match the keyword `of` at this position. The character before must
		// be a non-identifier (whitespace OK; digit/`n`/`+`/`-` from An+B OK
		// only if separated by whitespace per the grammar — but in practice
		// stripCSSComments emits a space and authors write `2n+1 of`, so we
		// require whitespace on the left). The character after must end the
		// identifier (whitespace or selector-start char or end).
		if i+1 >= len(arg) || (arg[i] != 'o' && arg[i] != 'O') {
			continue
		}
		if arg[i+1] != 'f' && arg[i+1] != 'F' {
			continue
		}
		// Left boundary: must be whitespace.
		if i == 0 {
			continue
		}
		left := arg[i-1]
		if left != ' ' && left != '\t' && left != '\n' && left != '\f' && left != '\r' {
			continue
		}
		// Right boundary: end-of-string, whitespace, or selector-start char.
		if i+2 >= len(arg) {
			continue // bare `of` with nothing after — malformed, skip.
		}
		right := arg[i+2]
		if !isNthOfRightBoundary(right) {
			continue
		}
		anb := strings.TrimSpace(arg[:i])
		ofSel := strings.TrimSpace(arg[i+2:])
		if anb == "" || ofSel == "" {
			return arg, "", false
		}
		return anb, ofSel, true
	}
	return arg, "", false
}

// isNthOfRightBoundary reports whether ch can legally follow the `of`
// keyword in `:nth-child(An+B of S)`. CSS Syntax §4 ends an identifier on
// whitespace OR any non-ident code point; the legal selector-start
// characters after `of` are whitespace and any selector token start
// (`.`/`#`/`[`/`:`/`*`/alpha/`(`).
func isNthOfRightBoundary(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	case '.', '#', '[', ':', '*', '(':
		return true
	}
	// Alpha — start of a tag-name selector like `target`.
	if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
		return true
	}
	return false
}

// matchesNthChildOf implements `:nth-child(An+B of S)` and
// `:nth-last-child(An+B of S)` per CSS Selectors 4 §6.6.1. Mirrors Blink's
// SelectorChecker::CheckPseudoNthChild() at third_party/blink/renderer/core/
// css/selector_checker.cc @ 4883d11fef4a, which filters siblings by S
// BEFORE computing the index, rather than post-filtering the candidate.
//
// Semantics:
//  1. The candidate node must itself match the selector list S.
//  2. Among its element siblings, only those matching S contribute to the
//     index count. The candidate's 1-based position in that filtered list
//     (from start for nth-child, from end for nth-last-child) is checked
//     against An+B.
//
// S is a comma-separated complex-selector-list; a sibling matches S if it
// matches ANY branch. Pseudo-elements in S are disallowed by the grammar;
// any branch containing one is treated as a parse error (the branch never
// matches) — Blink's CSSSelectorParser rejects them at parse time, which
// has the same effect.
func matchesNthChildOf(node *html.Node, anb, ofSel string, fromEnd bool) bool {
	branches := splitSelectorGroup(ofSel)
	parsedBranches := make([]Selector, 0, len(branches))
	for _, b := range branches {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		// Per spec, pseudo-elements are not allowed inside the `of` list.
		// A branch containing one never matches anything.
		if containsPseudoElementArg([]string{b}) {
			continue
		}
		parsedBranches = append(parsedBranches, ParseSelector(b))
	}
	if len(parsedBranches) == 0 {
		return false
	}
	// Candidate must itself match S.
	if !matchesAnyBranch(node, parsedBranches) {
		return false
	}
	if node.Parent == nil {
		return matchesAnPlusB(1, anb)
	}
	// Collect filtered siblings in tree order, find candidate's position.
	pos := 0
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type != html.ElementNode {
			continue
		}
		if !matchesAnyBranch(c, parsedBranches) {
			continue
		}
		count++
		if c == node {
			pos = count
		}
	}
	if pos == 0 {
		return false
	}
	idx := pos
	if fromEnd {
		idx = count - pos + 1
	}
	return matchesAnPlusB(idx, anb)
}

// matchesAnyBranch reports whether node matches any of the parsed complex
// selectors. Each branch is matched as a full complex selector (combinators
// resolved by walking ancestors/siblings), not just the rightmost compound.
func matchesAnyBranch(node *html.Node, branches []Selector) bool {
	for i := range branches {
		if MatchesSelector(node, branches[i]) {
			return true
		}
	}
	return false
}

// nthChildIndex returns the 1-based index of node among element siblings.
func nthChildIndex(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode {
			count++
			if c == node {
				return count
			}
		}
	}
	return 0
}

// parseAnPlusB parses an An+B expression like "2n+1", "3n", "5", "-n+3".
// parseAnPlusB parses an An+B production per CSS Syntax §3 / Selectors 4 §6.6.1.
// Returns (a, b, ok). When ok is false, the argument is not a valid An+B
// expression and the caller should treat the surrounding pseudo-class as a
// non-match (per the CSS error-recovery rule: an invalid selector is dropped).
// Mirrors Blink's CSSSelectorParser::ConsumeANPlusB @ 4883d11fef4a.
//
// Per the grammar, the only whitespace permitted is around the `+` / `-`
// joining the A-part dimension to the B-part integer — i.e. `An + B` or
// `An - B`. Whitespace BETWEEN the A coefficient and `n` (e.g. `1 n`) or
// inside the dimension is a parse error because A-and-n must form a single
// dimension token (`1n`, `2n`, `-n`). The strict check below tokenises
// around the central `+`/`-` separator before collapsing internal whitespace.
//
// Examples: "1" → (0, 1, true), "n" → (1, 0, true), "2n+1" → (2, 1, true),
// "-n+3" → (-1, 3, true), "1 n" → (_, _, false), "1 of" → (_, _, false).
func parseAnPlusBValid(s string) (a, b int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}

	// Split the input into the A-part (dimension/identifier containing 'n')
	// and the B-part (integer), joined by `+` or `-` with optional whitespace
	// around the operator. CSS Syntax §4 tokenisation: the operator must be
	// surrounded by whitespace if it follows a dimension (`2n+1` is a single
	// dimension+signed-integer; `2n + 1` is dimension, ws, +, ws, integer).
	// Both forms parse to the same An+B value.
	aPart := s
	bPart := ""
	bSign := 1
	// Search for a `+`/`-` that joins the parts. Skip a leading sign on the
	// whole expression (e.g. `-n+3`).
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if ch != '+' && ch != '-' {
			continue
		}
		// The operator must be preceded by `n`, an `n`+whitespace, or a digit
		// run (the An dimension can be just `n` with no leading number).
		left := strings.TrimRight(s[:i], " \t")
		if left == "" {
			continue
		}
		last := left[len(left)-1]
		if last != 'n' && last != 'N' {
			continue
		}
		aPart = left
		if ch == '-' {
			bSign = -1
		}
		bPart = strings.TrimLeft(s[i+1:], " \t")
		break
	}

	// The A-part must end in 'n' / 'N' to introduce the coefficient, OR be
	// just an integer (no n) — the B-only form.
	if strings.IndexAny(aPart, " \t") >= 0 {
		// Whitespace inside the A-part (e.g. "1 n") is invalid: the dimension
		// token must be contiguous.
		return 0, 0, false
	}

	nIdx := strings.IndexAny(aPart, "nN")
	if nIdx < 0 {
		// Pure B form: aPart must be an integer; bPart must be absent.
		if bPart != "" {
			return 0, 0, false
		}
		bVal, parsed := parseInt(aPart)
		if !parsed {
			return 0, 0, false
		}
		return 0, bVal, true
	}

	// A-part has 'n'. The portion after 'n' in aPart must be empty — the
	// integer after 'n' is the B-part split by `+`/`-`.
	if nIdx != len(aPart)-1 {
		return 0, 0, false
	}

	aStr := aPart[:nIdx]
	switch aStr {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		var parsed bool
		a, parsed = parseInt(aStr)
		if !parsed {
			return 0, 0, false
		}
	}

	if bPart == "" {
		return a, 0, true
	}
	bVal, parsed := parseInt(bPart)
	if !parsed {
		return 0, 0, false
	}
	return a, bSign * bVal, true
}

// parseAnPlusB is the legacy permissive entry point that returns (a, b)
// only. Existing callers that don't need validity (the An+B-only branches
// after splitNthChildArg has rejected `of`-form garbage) keep using it for
// brevity. Returns the same (a, b) as the strict parser for valid inputs and
// (0, 0) for invalid ones — equivalent to "match no element".
func parseAnPlusB(s string) (a, b int) {
	a, b, _ = parseAnPlusBValid(s)
	return a, b
}

func parseInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	if s[0] == '+' {
		s = s[1:]
	} else if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

// matchesIsLike implements the matching logic shared by :is() and :where().
// Per CSS Selectors 4, pseudo-elements are disallowed inside :is()/:where();
// per CSS Nesting, an :is() that contains a pseudo-element argument makes the
// resulting selector contextually invalid for element matching. We implement
// the strict reading: if ANY argument contains a pseudo-element token (::xxx
// or legacy :before/:after/:first-letter/:first-line), the whole :is()/:where()
// fails to match elements. This matches Blink behavior on the WPT
// contextually-invalid-selectors-* tests.
func matchesIsLike(node *html.Node, arg string) bool {
	selectors := splitSelectorGroup(strings.TrimSpace(arg))
	if containsPseudoElementArg(selectors) {
		return false
	}
	for _, sel := range selectors {
		innerSel := ParseSelector(strings.TrimSpace(sel))
		if len(innerSel.Parts) == 0 {
			continue
		}
		// MatchesSelector walks the full compound + combinator chain. The
		// previous implementation matched only the rightmost compound, which
		// caused `:is(#d + div, #d ~ #h)` to match every `div` instead of
		// only the one adjacent to `#d`. Mirrors Blink's
		// SelectorChecker::MatchSelector recursion @ 4883d11fef4a — :is()
		// dispatches each branch through the regular complex-selector path.
		if MatchesSelector(node, innerSel) {
			return true
		}
	}
	return false
}

// containsPseudoElementArg returns true if any selector in the list contains a
// pseudo-element token at top level (not nested inside a functional pseudo-
// class). Pseudo-elements: ::before, ::after, ::first-letter, ::first-line,
// ::marker, and the legacy single-colon forms (:before, :after, :first-letter,
// :first-line). The parser's findTopLevelPseudoElement helper does the same
// paren-aware scan used during selector parsing.
func containsPseudoElementArg(selectors []string) bool {
	tokens := []string{
		"::before", "::after", "::first-letter", "::first-line", "::marker",
		":before", ":after", ":first-letter", ":first-line",
	}
	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}
		for _, tok := range tokens {
			if findTopLevelPseudoElement(sel, tok) >= 0 {
				return true
			}
		}
	}
	return false
}

// containsHas reports whether s contains a `:has(` token outside of a string
// literal — used to enforce the "no nested :has()" rule from CSS Selectors 4
// §17.1.4. We scan character-by-character (not regex) so attribute selectors
// like `[data-x="…has(…)…"]` don't false-positive. Brackets, parens, and
// quotes are tracked to skip non-selector context.
func containsHas(s string) bool {
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inStr = c
			continue
		}
		// Look for `:has(` — only when preceded by selector-context
		// (not inside an identifier).
		if c == ':' && i+4 < len(s) && strings.HasPrefix(s[i:], ":has(") {
			return true
		}
	}
	return false
}

// matchesHas implements the :has() pseudo-class
func matchesHas(node *html.Node, selectorStr string) bool {
	selectors := splitSelectorGroup(strings.TrimSpace(selectorStr))
	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}
		if strings.HasPrefix(sel, ">") {
			// Direct child combinator
			innerSel := strings.TrimSpace(sel[1:])
			parsed := ParseSelector(innerSel)
			if len(parsed.Parts) > 0 {
				for _, child := range node.Children {
					if child.Type == html.ElementNode {
						if matchesSelectorPart(child, parsed.Parts[len(parsed.Parts)-1]) {
							return true
						}
					}
				}
			}
		} else if strings.HasPrefix(sel, "+") {
			// Adjacent sibling combinator
			innerSel := strings.TrimSpace(sel[1:])
			parsed := ParseSelector(innerSel)
			if len(parsed.Parts) > 0 {
				next := getNextElementSibling(node)
				if next != nil && matchesSelectorPart(next, parsed.Parts[len(parsed.Parts)-1]) {
					return true
				}
			}
		} else if strings.HasPrefix(sel, "~") {
			// General sibling combinator
			innerSel := strings.TrimSpace(sel[1:])
			parsed := ParseSelector(innerSel)
			if len(parsed.Parts) > 0 {
				for _, sib := range getSubsequentSiblings(node) {
					if matchesSelectorPart(sib, parsed.Parts[len(parsed.Parts)-1]) {
						return true
					}
				}
			}
		} else {
			// Default: descendant combinator
			parsed := ParseSelector(sel)
			if len(parsed.Parts) > 0 {
				if hasMatchingDescendant(node, parsed.Parts[len(parsed.Parts)-1]) {
					return true
				}
			}
		}
	}
	return false
}

func getNextElementSibling(node *html.Node) *html.Node {
	if node.Parent == nil {
		return nil
	}
	found := false
	for _, child := range node.Parent.Children {
		if found && child.Type == html.ElementNode {
			return child
		}
		if child == node {
			found = true
		}
	}
	return nil
}

func getSubsequentSiblings(node *html.Node) []*html.Node {
	if node.Parent == nil {
		return nil
	}
	var result []*html.Node
	found := false
	for _, child := range node.Parent.Children {
		if found && child.Type == html.ElementNode {
			result = append(result, child)
		}
		if child == node {
			found = true
		}
	}
	return result
}

func hasMatchingDescendant(node *html.Node, part SelectorPart) bool {
	for _, child := range node.Children {
		if child.Type == html.ElementNode {
			if matchesSelectorPart(child, part) {
				return true
			}
			if hasMatchingDescendant(child, part) {
				return true
			}
		}
	}
	return false
}

// nthOfTypeIndex returns the 1-based index among siblings with the same tag name
func nthOfTypeIndex(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode && c.TagName == node.TagName {
			count++
			if c == node {
				return count
			}
		}
	}
	return 0
}

// countOfType returns total siblings with same tag name
func countOfType(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode && c.TagName == node.TagName {
			count++
		}
	}
	return count
}

// totalElementChildren returns total element children of parent
func totalElementChildren(node *html.Node) int {
	if node.Parent == nil {
		return 1
	}
	count := 0
	for _, c := range node.Parent.Children {
		if c.Type == html.ElementNode {
			count++
		}
	}
	return count
}

// matchesAnPlusB checks if position matches An+B formula
func matchesAnPlusB(position int, arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "odd" {
		return position%2 == 1
	}
	if arg == "even" {
		return position%2 == 0
	}
	a, b, ok := parseAnPlusBValid(arg)
	if !ok {
		// Invalid An+B — the surrounding selector is a parse error and
		// matches no element. Mirrors CSS Syntax §4 invalid-selector recovery.
		return false
	}
	if a == 0 {
		return position == b
	}
	diff := position - b
	if a > 0 {
		return diff >= 0 && diff%a == 0
	}
	return diff <= 0 && diff%a == 0
}

func isFormElement(node *html.Node) bool {
	switch node.TagName {
	case "input", "select", "textarea", "button":
		return true
	}
	return false
}

func isCheckableElement(node *html.Node) bool {
	if node.TagName != "input" {
		return false
	}
	inputType, _ := node.GetAttribute("type")
	return inputType == "checkbox" || inputType == "radio" || inputType == ""
}

func hasAttribute(node *html.Node, name string) bool {
	_, exists := node.GetAttribute(name)
	return exists
}

// canBeRequired reports whether the element is a form control to which the
// `required` attribute applies. Per HTML §form-control-content-attributes,
// `required` applies to <input> (most types), <select>, and <textarea>.
// Mirrors Blink core/html/forms/html_form_control_element.cc::IsRequiredFormControl
// @ 4883d11fef4a — `:required` and `:optional` only match elements that *can*
// be either; otherwise neither pseudo matches.
func canBeRequired(node *html.Node) bool {
	switch node.TagName {
	case "select", "textarea":
		return true
	case "input":
		// Per HTML "the input element": required does NOT apply to type=
		// hidden, range, color, submit, image, reset, button.
		inputType, _ := node.GetAttribute("type")
		switch strings.ToLower(strings.TrimSpace(inputType)) {
		case "hidden", "range", "color", "submit", "image", "reset", "button":
			return false
		}
		return true
	}
	return false
}

// matchesPlaceholderShown reports whether the element is currently showing its
// placeholder. The element must be an <input> or <textarea> whose type accepts
// a placeholder, has a non-empty `placeholder` attribute, and whose current
// value is empty. Mirrors Blink core/html/forms/html_input_element.cc::
// IsPlaceholderVisible / TextControlElement::IsPlaceholderVisible @
// 4883d11fef4a.
//
// Scope: static reftests start from the parsed initial state. The current
// value is read from the `value` attribute (textarea uses text content as the
// default). JS-driven mutation of the value flips via the engine's existing
// attribute path.
func matchesPlaceholderShown(node *html.Node) bool {
	switch node.TagName {
	case "input":
		// Only text-like types accept a placeholder. Per HTML "the input
		// element", `placeholder` applies to: text, search, url, tel, email,
		// password, number. Other types (hidden, checkbox, radio, file,
		// submit, image, reset, button, range, color, date, time, etc.) do
		// not show a placeholder.
		inputType, _ := node.GetAttribute("type")
		switch strings.ToLower(strings.TrimSpace(inputType)) {
		case "", "text", "search", "url", "tel", "email", "password", "number":
			// accepts placeholder
		default:
			return false
		}
	case "textarea":
		// <textarea> always accepts placeholder.
	default:
		return false
	}
	placeholder, ok := node.GetAttribute("placeholder")
	if !ok || placeholder == "" {
		return false
	}
	if node.TagName == "textarea" {
		// textarea default value is its text content.
		for _, c := range node.Children {
			if c.Type == html.TextNode && c.Text != "" {
				return false
			}
		}
		return true
	}
	value, _ := node.GetAttribute("value")
	return value == ""
}

// matchesReadWrite reports whether the element is editable for :read-write
// purposes. Per CSS Selectors 4 §6.6, the user-editable controls are:
//   - <input> whose type accepts text editing and that is neither :disabled
//     nor :readonly,
//   - <textarea> that is neither :disabled nor :readonly,
//   - any element with `contenteditable` (other than "false").
//
// All other elements are :read-only. Mirrors Blink core/html/forms/
// html_form_control_element.cc::MatchesReadWritePseudoClass and
// core/dom/element.cc::MatchesReadOnlyPseudoClass @ 4883d11fef4a.
func matchesReadWrite(node *html.Node) bool {
	switch node.TagName {
	case "input":
		// Only text-editable input types are :read-write.
		inputType, _ := node.GetAttribute("type")
		switch strings.ToLower(strings.TrimSpace(inputType)) {
		case "", "text", "search", "url", "tel", "email", "password",
			"number", "date", "datetime-local", "month", "time", "week":
			// editable types
		default:
			return false
		}
		if hasAttribute(node, "disabled") || hasAttribute(node, "readonly") {
			return false
		}
		return true
	case "textarea":
		if hasAttribute(node, "disabled") || hasAttribute(node, "readonly") {
			return false
		}
		return true
	}
	// contenteditable: matches if the element is user-editable.
	if ce, ok := node.GetAttribute("contenteditable"); ok {
		switch strings.ToLower(strings.TrimSpace(ce)) {
		case "false":
			return false
		}
		return true
	}
	return false
}

// FindMatchingRules returns all rules that match the given node
// Phase 22: Added viewport dimensions for media query evaluation
func FindMatchingRules(node *html.Node, stylesheet *Stylesheet, viewportWidth, viewportHeight float64) []Rule {
	matches := make([]Rule, 0)

	for _, rule := range stylesheet.Rules {
		// Skip pseudo-element rules (they are applied via ComputePseudoElementStyle)
		if rule.Selector.PseudoElement != "" {
			continue
		}

		// Skip container query rules (evaluated separately in applyContainerQueryRules)
		if rule.ContainerQuery != nil {
			continue
		}

		// Phase 22: Check media query first (Kleene 3-valued — apply only when
		// the result is definitely true; Unknown does not apply). Combines
		// the rule's @media wrapper with any @import-level media-query-list
		// (CSS Cascade 4 §3.1).
		if !RuleMediaApplies(&rule, viewportWidth, viewportHeight) {
			continue
		}

		if MatchesSelector(node, rule.Selector) {
			matches = append(matches, rule)
		}
	}

	return matches
}
