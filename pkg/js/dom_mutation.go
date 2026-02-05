package js

import (
	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// appendChildFn returns a JS function that implements node.appendChild(child).
func (e *elementAccessor) appendChildFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'appendChild': 1 argument required"))
		}
		child := e.ctx.unwrapNode(call.Arguments[0])
		if child == nil {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'appendChild': parameter is not a Node"))
		}
		// Remove from old parent if already in tree
		if child.Parent != nil {
			child.Parent.RemoveChild(child)
		}
		e.node.AddChild(child)
		return e.ctx.elementProxy(child)
	}
}

// removeChildFn returns a JS function that implements node.removeChild(child).
func (e *elementAccessor) removeChildFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'removeChild': 1 argument required"))
		}
		child := e.ctx.unwrapNode(call.Arguments[0])
		if child == nil {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'removeChild': parameter is not a Node"))
		}
		removed := e.node.RemoveChild(child)
		if removed == nil {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'removeChild': The node to be removed is not a child of this node"))
		}
		return e.ctx.elementProxy(removed)
	}
}

// insertBeforeFn returns a JS function that implements node.insertBefore(newNode, refNode).
func (e *elementAccessor) insertBeforeFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'insertBefore': 1 argument required"))
		}
		newChild := e.ctx.unwrapNode(call.Arguments[0])
		if newChild == nil {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'insertBefore': parameter 1 is not a Node"))
		}
		var refChild *html.Node
		if len(call.Arguments) > 1 && !goja.IsNull(call.Arguments[1]) && !goja.IsUndefined(call.Arguments[1]) {
			refChild = e.ctx.unwrapNode(call.Arguments[1])
		}
		e.node.InsertBefore(newChild, refChild)
		return e.ctx.elementProxy(newChild)
	}
}

// setInnerHTML parses the HTML string and replaces the node's children.
func (e *elementAccessor) setInnerHTML(htmlStr string) {
	// Clear existing children
	e.node.Children = nil

	if htmlStr == "" {
		return
	}

	// Parse as fragment
	children, err := html.ParseFragment(htmlStr)
	if err != nil {
		return
	}

	// Adopt all parsed children
	for _, child := range children {
		child.Parent = e.node
		e.node.Children = append(e.node.Children, child)
	}
}

// Convenience mutation methods (Phase 3)

// appendFn returns a JS function for element.append(...nodes).
// Accepts nodes and strings (strings become text nodes).
func (e *elementAccessor) appendFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		for _, arg := range call.Arguments {
			node := e.ctx.unwrapNode(arg)
			if node != nil {
				if node.Parent != nil {
					node.Parent.RemoveChild(node)
				}
				e.node.AddChild(node)
			} else {
				// Treat as string -> text node
				e.node.AppendText(arg.String())
			}
		}
		return goja.Undefined()
	}
}

// prependFn returns a JS function for element.prepend(...nodes).
func (e *elementAccessor) prependFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		// Collect nodes to prepend (in order)
		var toInsert []*html.Node
		for _, arg := range call.Arguments {
			node := e.ctx.unwrapNode(arg)
			if node != nil {
				if node.Parent != nil {
					node.Parent.RemoveChild(node)
				}
				toInsert = append(toInsert, node)
			} else {
				toInsert = append(toInsert, &html.Node{
					Type: html.TextNode,
					Text: arg.String(),
				})
			}
		}
		// Insert before first child
		var firstChild *html.Node
		if len(e.node.Children) > 0 {
			firstChild = e.node.Children[0]
		}
		for _, n := range toInsert {
			e.node.InsertBefore(n, firstChild)
		}
		return goja.Undefined()
	}
}

// beforeFn returns a JS function for element.before(...nodes).
func (e *elementAccessor) beforeFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if e.node.Parent == nil {
			return goja.Undefined()
		}
		parent := e.node.Parent
		for _, arg := range call.Arguments {
			node := e.ctx.unwrapNode(arg)
			if node != nil {
				if node.Parent != nil {
					node.Parent.RemoveChild(node)
				}
			} else {
				node = &html.Node{
					Type: html.TextNode,
					Text: arg.String(),
				}
			}
			parent.InsertBefore(node, e.node)
		}
		return goja.Undefined()
	}
}

// afterFn returns a JS function for element.after(...nodes).
func (e *elementAccessor) afterFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if e.node.Parent == nil {
			return goja.Undefined()
		}
		parent := e.node.Parent
		// Find the next sibling to use as reference
		idx := e.node.IndexInParent()
		var refNode *html.Node
		if idx >= 0 && idx+1 < len(parent.Children) {
			refNode = parent.Children[idx+1]
		}
		for _, arg := range call.Arguments {
			node := e.ctx.unwrapNode(arg)
			if node != nil {
				if node.Parent != nil {
					node.Parent.RemoveChild(node)
				}
			} else {
				node = &html.Node{
					Type: html.TextNode,
					Text: arg.String(),
				}
			}
			parent.InsertBefore(node, refNode)
		}
		return goja.Undefined()
	}
}

// replaceWithFn returns a JS function for element.replaceWith(...nodes).
func (e *elementAccessor) replaceWithFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if e.node.Parent == nil {
			return goja.Undefined()
		}
		parent := e.node.Parent
		// Insert all new nodes before this one
		for _, arg := range call.Arguments {
			node := e.ctx.unwrapNode(arg)
			if node != nil {
				if node.Parent != nil {
					node.Parent.RemoveChild(node)
				}
			} else {
				node = &html.Node{
					Type: html.TextNode,
					Text: arg.String(),
				}
			}
			parent.InsertBefore(node, e.node)
		}
		// Remove this node
		parent.RemoveChild(e.node)
		return goja.Undefined()
	}
}

// replaceChildrenFn returns a JS function for element.replaceChildren(...nodes).
func (e *elementAccessor) replaceChildrenFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		// Clear all children
		e.node.Children = nil

		// Append new children
		for _, arg := range call.Arguments {
			node := e.ctx.unwrapNode(arg)
			if node != nil {
				if node.Parent != nil {
					node.Parent.RemoveChild(node)
				}
				e.node.AddChild(node)
			} else {
				e.node.AppendText(arg.String())
			}
		}
		return goja.Undefined()
	}
}
