package js

import (
	"strconv"
	"strings"

	"louis14/pkg/css"

	"github.com/dop251/goja"
)

// dom_cssom.go implements the minimal CSSOM stylesheet surface needed by the
// LOU-351 css-pseudo target-text-dynamic reftests:
//
//	document.styleSheets[n].cssRules[m].style.<camelCaseProp> = "value"
//
// Blink shape mirrored (all @ chromium-main
// 4a32dc92bd262649caef90a6e34d351c24008eee):
//   - document.styleSheets — core/dom/document_or_shadow_root.idl:18
//     `[SameObject] readonly attribute StyleSheetList styleSheets`.
//   - StyleSheetList / CSSRuleList — core/css/style_sheet_list.idl and
//     core/css/css_rule_list.idl: identical `item(index)` + `length` shape,
//     hence the shared newIndexedList builder below.
//   - CSSStyleRule.style — core/css/css_style_rule.cc:64-70: the wrapper
//     exposes the parsed StyleRule's *mutable* property set, so CSSOM writes
//     land directly in the StyleSheetContents-owned parse. louis14's analog
//     is mutating the *css.Rule inside the doc.ParsedStylesheetsCache parse
//     (pkg/css/style_sheet_contents.go) — the visualtest harness's post-JS
//     relayout (pkg/visualtest/helpers.go) then re-cascades from the mutated
//     rule, the one-shot equivalent of Blink's DidMutateRules →
//     SetNeedsActiveStyleUpdate invalidation chain
//     (core/css/style_rule_css_style_declaration.cc:50-68).
//
// Scope cut (see docs/plan-lou-351.md): no insertRule/deleteRule/replace/
// selectorText/cssText/media/ownerRule and no constructed stylesheets — the
// 3 target tests only index the lists and assign one camelCase property.
// The lists are static snapshots built at registration time; louis14's
// parser also flattens comma-selector groups and conditional at-rule bodies
// into one Rule per selector, so cssRules indices match the spec's
// CSSRuleList only for sheets of single-selector style rules (true for all
// target tests).

// registerStyleSheetsAPI wires document.styleSheets onto docObj. Called from
// registerDocument alongside the other document.* bindings. Parsing goes
// through css.ParseDocumentStylesheets so the wrappers hold the SAME parsed
// rules the layout/paint pipeline reads from doc.ParsedStylesheetsCache.
func registerStyleSheetsAPI(ctx *domContext, docObj *goja.Object) {
	parsed := css.ParseDocumentStylesheets(ctx.doc)
	sheets := make([]goja.Value, len(parsed))
	for i, sheet := range parsed {
		sheets[i] = newCSSStyleSheet(ctx.vm, sheet)
	}
	docObj.Set("styleSheets", newIndexedList(ctx.vm, sheets))
}

// newCSSStyleSheet builds the CSSStyleSheet wrapper: only `cssRules`
// (core/css/css_style_sheet.idl `[SameObject, RaisesException] readonly
// attribute CSSRuleList cssRules` @ 4a32dc92) is exposed.
func newCSSStyleSheet(vm *goja.Runtime, sheet *css.Stylesheet) goja.Value {
	rules := make([]goja.Value, len(sheet.Rules))
	for m := range sheet.Rules {
		// Index into the value slice — the element pointer, not a copy —
		// so style writes mutate the cached parse.
		rules[m] = newCSSStyleRule(vm, &sheet.Rules[m])
	}
	obj := vm.NewObject()
	obj.Set("cssRules", newIndexedList(vm, rules))
	return obj
}

// newCSSStyleRule builds the CSSStyleRule wrapper: only `style`
// (core/css/css_style_rule.idl `[SameObject, PutForwards=cssText] readonly
// attribute CSSStyleDeclaration style` @ 4a32dc92) is exposed.
func newCSSStyleRule(vm *goja.Runtime, rule *css.Rule) goja.Value {
	obj := vm.NewObject()
	obj.Set("style", vm.NewDynamicObject(&ruleStyleAccessor{vm: vm, rule: rule}))
	return obj
}

// newIndexedList builds the shared StyleSheetList/CSSRuleList shape: indexed
// getters, `length`, and `item(index)` returning null out of range (both IDL
// interfaces are identical modulo element type — see file doc comment).
func newIndexedList(vm *goja.Runtime, items []goja.Value) goja.Value {
	obj := vm.NewObject()
	for i, item := range items {
		obj.Set(strconv.Itoa(i), item)
	}
	obj.Set("length", len(items))
	obj.Set("item", func(call goja.FunctionCall) goja.Value {
		idx := int(call.Argument(0).ToInteger())
		if idx < 0 || idx >= len(items) {
			return goja.Null()
		}
		return items[idx]
	})
	return obj
}

// ruleStyleAccessor maps JS camelCase property access onto a parsed rule's
// live declaration map — the rule-backed sibling of the inline-style
// styleAccessor (dom.go), which round-trips through the element's style
// attribute instead. Both mirror CSSStyleDeclaration's camelCase attribute
// handlers (core/css/css_style_declaration.cc:169/:182 AnonymousNamedGetter/
// AnonymousNamedSetter @ 4a32dc92) and share camelToKebab.
type ruleStyleAccessor struct {
	vm   *goja.Runtime
	rule *css.Rule
}

func (s *ruleStyleAccessor) Get(key string) goja.Value {
	if val, ok := s.rule.Declarations[camelToKebab(key)]; ok {
		return s.vm.ToValue(val)
	}
	return s.vm.ToValue("")
}

// Set replaces the declaration and clears any stale !important flag — the
// attribute setter always writes important=false (Blink's SetPropertyInternal,
// core/css/abstract_property_set_css_style_declaration.cc:254 @ 4a32dc92).
// Assigning the empty string removes the declaration, matching styleAccessor.
func (s *ruleStyleAccessor) Set(key string, val goja.Value) bool {
	prop := camelToKebab(key)
	if v := val.String(); strings.TrimSpace(v) == "" {
		delete(s.rule.Declarations, prop)
	} else {
		if s.rule.Declarations == nil {
			s.rule.Declarations = make(map[string]string)
		}
		s.rule.Declarations[prop] = v
	}
	delete(s.rule.Important, prop)
	return true
}

func (s *ruleStyleAccessor) Has(key string) bool {
	return true
}

func (s *ruleStyleAccessor) Delete(key string) bool {
	prop := camelToKebab(key)
	delete(s.rule.Declarations, prop)
	delete(s.rule.Important, prop)
	return true
}

func (s *ruleStyleAccessor) Keys() []string {
	keys := make([]string, 0, len(s.rule.Declarations))
	for k := range s.rule.Declarations {
		keys = append(keys, k)
	}
	return keys
}
