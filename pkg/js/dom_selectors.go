package js

import (
	"louis14/pkg/css"
	"louis14/pkg/html"

	"github.com/dop251/goja"
)

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
		selectorStr := call.Arguments[0].String()
		selectors := css.SplitSelectorGroup(selectorStr)

		var result *html.Node
		walkTree(root, func(n *html.Node) bool {
			if n == root {
				return false // skip root itself
			}
			for _, sel := range selectors {
				parsed := css.ParseSelector(sel)
				if css.MatchesSelector(n, parsed) {
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
		selectorStr := call.Arguments[0].String()
		selectors := css.SplitSelectorGroup(selectorStr)

		var results []*html.Node
		walkTree(root, func(n *html.Node) bool {
			if n == root {
				return false
			}
			for _, sel := range selectors {
				parsed := css.ParseSelector(sel)
				if css.MatchesSelector(n, parsed) {
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
		selectorStr := call.Arguments[0].String()
		selectors := css.SplitSelectorGroup(selectorStr)
		for _, sel := range selectors {
			parsed := css.ParseSelector(sel)
			if css.MatchesSelector(node, parsed) {
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
		selectorStr := call.Arguments[0].String()
		selectors := css.SplitSelectorGroup(selectorStr)

		for current := node; current != nil; current = current.Parent {
			if current.Type != html.ElementNode || current.TagName == "document" {
				continue
			}
			for _, sel := range selectors {
				parsed := css.ParseSelector(sel)
				if css.MatchesSelector(current, parsed) {
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
