package js

import (
	"louis14/pkg/css"
	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// queryScope is the :scope of a query run on root (SelectorMatches,
// core/css/selector_query.cc:102-113; CheckPseudoScope,
// core/css/selector_checker.cc:3650-3660 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): root itself when it is an
// element, and nil otherwise, whose :scope is the document element. A
// fragment's descendants are never the document element, so nothing in a
// fragment matches :scope, as in Blink.
func queryScope(root *html.Node) *html.Node {
	if root.Type != html.ElementNode || root.TagName == "document" {
		return nil
	}
	return root
}

// parsedSelectors parses each selector of a selector list.
func parsedSelectors(selectorStr string) []css.Selector {
	var parsed []css.Selector
	for _, sel := range css.SplitSelectorGroup(selectorStr) {
		parsed = append(parsed, css.ParseSelector(sel))
	}
	return parsed
}

// registerQuerySelectors adds querySelector/querySelectorAll to a document object.
func registerQuerySelectors(ctx *domContext, obj *goja.Object, root *html.Node) {
	obj.Set("querySelector", querySelectorFn(ctx, root))
	obj.Set("querySelectorAll", querySelectorAllFn(ctx, root))
}

// querySelectorFn returns a JS function implementing querySelector.
func querySelectorFn(ctx *domContext, root *html.Node) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(ctx.vm.NewTypeError("Failed to execute 'querySelector': 1 argument required"))
		}
		selectors, scope := parsedSelectors(call.Arguments[0].String()), queryScope(root)

		var result *html.Node
		walkTree(root, func(n *html.Node) bool {
			if n == root {
				return false // skip root itself
			}
			for _, parsed := range selectors {
				if css.MatchesSelectorInScope(n, parsed, scope) {
					result = n
					return true // stop
				}
			}
			return false
		})

		if result == nil {
			return goja.Null()
		}
		return ctx.elementProxy(result)
	}
}

// querySelectorAllFn returns a JS function implementing querySelectorAll.
func querySelectorAllFn(ctx *domContext, root *html.Node) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(ctx.vm.NewTypeError("Failed to execute 'querySelectorAll': 1 argument required"))
		}
		selectors, scope := parsedSelectors(call.Arguments[0].String()), queryScope(root)

		var results []*html.Node
		walkTree(root, func(n *html.Node) bool {
			if n == root {
				return false
			}
			for _, parsed := range selectors {
				if css.MatchesSelectorInScope(n, parsed, scope) {
					results = append(results, n)
					break // don't add same node twice for multiple matching selectors
				}
			}
			return false
		})

		return ctx.elementArray(results)
	}
}

// matchesFn returns a JS function implementing element.matches(selector).
func matchesFn(ctx *domContext, node *html.Node) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(ctx.vm.NewTypeError("Failed to execute 'matches': 1 argument required"))
		}
		for _, parsed := range parsedSelectors(call.Arguments[0].String()) {
			if css.MatchesSelectorInScope(node, parsed, node) {
				return ctx.vm.ToValue(true)
			}
		}
		return ctx.vm.ToValue(false)
	}
}

// closestFn returns a JS function implementing element.closest(selector).
func closestFn(ctx *domContext, node *html.Node) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(ctx.vm.NewTypeError("Failed to execute 'closest': 1 argument required"))
		}
		selectors := parsedSelectors(call.Arguments[0].String())
		for current := node; current != nil; current = current.Parent {
			if current.Type != html.ElementNode || current.TagName == "document" {
				continue
			}
			for _, parsed := range selectors {
				// :scope is the element closest was called on
				// (SelectorQuery::Closest, selector_query.cc:128-143).
				if css.MatchesSelectorInScope(current, parsed, node) {
					return ctx.elementProxy(current)
				}
			}
		}
		return goja.Null()
	}
}

// walkTree performs a DFS walk over the tree. The callback returns true to stop.
func walkTree(node *html.Node, fn func(*html.Node) bool) bool {
	if node.Type == html.ElementNode {
		if fn(node) {
			return true
		}
	}
	for _, child := range node.Children {
		if walkTree(child, fn) {
			return true
		}
	}
	return false
}
