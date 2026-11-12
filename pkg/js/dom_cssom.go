package js

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// dom_cssom.go implements the CSSOM stylesheet surface:
//
//	document.styleSheets[n].cssRules[m].style.<camelCaseProp> = "value"
//	<style>.sheet / <link>.sheet
//	sheet.insertRule(text, index) / sheet.deleteRule(index)
//
// Blink shape mirrored (@ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//   - document.styleSheets — core/dom/document_or_shadow_root.idl
//     `[SameObject] readonly attribute StyleSheetList styleSheets`, a live
//     list of the document's author sheets in tree order.
//   - LinkStyle.sheet (HTMLStyleElement / HTMLLinkElement) — the element's
//     sheet (StyleElement / LinkStyle sheet_), or null.
//   - CSSStyleSheet.cssRules — `[SameObject] readonly attribute
//     CSSRuleList cssRules`, live over the sheet's top-level rules.
//   - CSSStyleSheet::insertRule / deleteRule (core/css/css_style_sheet.cc:
//     423-497), ported on css.StyleSheetContents; their DOMExceptions are
//     thrown by name.
//   - CSSStyleRule.style — core/css/css_style_rule.cc: the wrapper exposes
//     the rule's mutable property set, so CSSOM writes land directly in the
//     cached parse (doc.ParsedStylesheetsCache) the post-JS relayout
//     re-cascades from.
//
// Not covered: replace / replaceSync, selectorText / cssText, media,
// ownerRule, the grouping rules' own cssRules, constructed sheets.

// registerStyleSheetsAPI wires document.styleSheets onto docObj. Called from
// registerDocument alongside the other document.* bindings.
func registerStyleSheetsAPI(ctx *domContext, docObj *goja.Object) {
	docObj.Set("styleSheets", ctx.vm.NewDynamicObject(&liveList{
		vm:     ctx.vm,
		length: func() int { return len(ctx.styleSheets()) },
		at:     func(i int) goja.Value { return ctx.styleSheetWrapper(ctx.styleSheets()[i]) },
	}))
}

// styleSheets returns the document's author sheets as they stand now: the
// candidates are re-collected first (StyleEngine::UpdateActiveStyleSheets),
// so a <style> a script inserted has its sheet.
func (ctx *domContext) styleSheets() []*css.StyleSheetContents {
	ctx.doc.UpdateActiveStyleSheets()
	return css.DocumentStyleSheetContents(ctx.doc)
}

// ownedStyleSheet returns the wrapper of node's sheet (LinkStyle.sheet), or
// null when it has none.
func (ctx *domContext) ownedStyleSheet(node *html.Node) goja.Value {
	for _, sheet := range ctx.styleSheets() {
		if sheet.Owner == node {
			return ctx.styleSheetWrapper(sheet)
		}
	}
	return goja.Null()
}

// styleSheetWrapper returns sheet's CSSStyleSheet wrapper, one per sheet.
func (ctx *domContext) styleSheetWrapper(sheet *css.StyleSheetContents) goja.Value {
	if ctx.sheetWrappers == nil {
		ctx.sheetWrappers = map[*css.StyleSheetContents]goja.Value{}
	}
	if w, ok := ctx.sheetWrappers[sheet]; ok {
		return w
	}
	vm := ctx.vm
	obj := vm.NewObject()
	obj.Set("cssRules", vm.NewDynamicObject(&liveList{
		vm:     vm,
		length: sheet.RuleCount,
		at:     func(i int) goja.Value { return ctx.ruleWrapper(sheet, sheet.Rules()[i]) },
	}))
	// insertRule(rule, optional index = 0) and deleteRule(index)
	// (core/css/css_style_sheet.idl).
	obj.Set("insertRule", func(call goja.FunctionCall) goja.Value {
		index := 0
		if len(call.Arguments) > 1 {
			index = int(call.Argument(1).ToInteger())
		}
		n, err := sheet.InsertRule(call.Argument(0).String(), index)
		if err != nil {
			ctx.throwDOMException(err)
		}
		return vm.ToValue(n)
	})
	obj.Set("deleteRule", func(call goja.FunctionCall) goja.Value {
		if err := sheet.DeleteRule(int(call.Argument(0).ToInteger())); err != nil {
			ctx.throwDOMException(err)
		}
		return goja.Undefined()
	})
	ctx.sheetWrappers[sheet] = obj
	return obj
}

// ruleWrapper returns r's CSSRule wrapper, one per rule: a style rule's
// carries `style` (CSSStyleRule.style).
func (ctx *domContext) ruleWrapper(sheet *css.StyleSheetContents, r *css.CSSOMRule) goja.Value {
	if ctx.ruleWrappers == nil {
		ctx.ruleWrappers = map[*css.CSSOMRule]goja.Value{}
	}
	if w, ok := ctx.ruleWrappers[r]; ok {
		return w
	}
	obj := ctx.vm.NewObject()
	if sheet.RuleDeclarations(r) != nil {
		obj.Set("style", ctx.vm.NewDynamicObject(&ruleStyleAccessor{vm: ctx.vm, sheet: sheet, rule: r}))
	}
	ctx.ruleWrappers[r] = obj
	return obj
}

// throwDOMException throws err as a DOMException named by its code (a
// css.RuleMutationError), or as a Go error.
func (ctx *domContext) throwDOMException(err error) {
	var me *css.RuleMutationError
	if !errors.As(err, &me) {
		panic(ctx.vm.NewGoError(err))
	}
	ex, newErr := ctx.vm.New(ctx.vm.Get("Error"), ctx.vm.ToValue(me.Message))
	if newErr != nil {
		panic(ctx.vm.NewGoError(err))
	}
	ex.Set("name", me.Name)
	panic(ex)
}

// newIndexedList builds a fixed list of items in the liveList shape
// (getClientRects' DOMRectList).
func newIndexedList(vm *goja.Runtime, items []goja.Value) goja.Value {
	return vm.NewDynamicObject(&liveList{vm: vm,
		length: func() int { return len(items) },
		at:     func(i int) goja.Value { return items[i] },
	})
}

// liveList is the shared StyleSheetList / CSSRuleList shape: indexed
// getters, `length`, and `item(index)` returning null out of range (both IDL
// interfaces are identical modulo element type), over the items as they are
// at each access: length counts them, at builds the one asked for.
type liveList struct {
	vm     *goja.Runtime
	length func() int
	at     func(i int) goja.Value
}

func (l *liveList) Get(key string) goja.Value {
	switch key {
	case "length":
		return l.vm.ToValue(l.length())
	case "item":
		return l.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			idx := int(call.Argument(0).ToInteger())
			if idx < 0 || idx >= l.length() {
				return goja.Null()
			}
			return l.at(idx)
		})
	}
	if idx, err := strconv.Atoi(key); err == nil && idx >= 0 && idx < l.length() {
		return l.at(idx)
	}
	return goja.Undefined()
}

func (l *liveList) Set(string, goja.Value) bool { return false }
func (l *liveList) Has(key string) bool {
	if key == "length" || key == "item" {
		return true
	}
	idx, err := strconv.Atoi(key)
	return err == nil && idx >= 0 && idx < l.length()
}
func (l *liveList) Delete(string) bool { return false }
func (l *liveList) Keys() []string {
	keys := make([]string, l.length())
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	return keys
}

// ruleStyleAccessor maps JS camelCase property access onto a parsed rule's
// live declaration map — the rule-backed sibling of the inline-style
// styleAccessor (dom.go), which round-trips through the element's style
// attribute instead. Both mirror CSSStyleDeclaration's camelCase attribute
// handlers (core/css/css_style_declaration.cc:169/:182 AnonymousNamedGetter/
// AnonymousNamedSetter @ 4a32dc92) and share camelToKebab.
type ruleStyleAccessor struct {
	vm    *goja.Runtime
	sheet *css.StyleSheetContents
	rule  *css.CSSOMRule
}

// declarations is the rule's current declaration block (a re-parse after
// a rule mutation replaces it).
func (s *ruleStyleAccessor) declarations() *css.DeclarationResult {
	return s.sheet.RuleDeclarations(s.rule)
}

func (s *ruleStyleAccessor) Get(key string) goja.Value {
	val, _ := s.declarations().Value(camelToKebab(key))
	return s.vm.ToValue(val)
}

// Set parses the declaration as the stylesheet parser would and replaces
// what the property set before, not important — the attribute setter always
// writes important=false (Blink's SetPropertyInternal,
// core/css/abstract_property_set_css_style_declaration.cc:254 @ 4a32dc92).
// Assigning the empty string removes the declaration, matching styleAccessor.
func (s *ruleStyleAccessor) Set(key string, val goja.Value) bool {
	prop := camelToKebab(key)
	if v := val.String(); strings.TrimSpace(v) == "" {
		s.declarations().RemoveDeclaration(prop)
	} else if cssomValueAccepted(prop, v) {
		s.declarations().SetDeclaration(prop, v)
	}
	s.sheet.MarkRuleChanged(s.rule)
	return true
}

func (s *ruleStyleAccessor) Has(key string) bool {
	return true
}

func (s *ruleStyleAccessor) Delete(key string) bool {
	s.declarations().RemoveDeclaration(camelToKebab(key))
	s.sheet.MarkRuleChanged(s.rule)
	return true
}

func (s *ruleStyleAccessor) Keys() []string {
	return slices.Clone(s.declarations().Order)
}
