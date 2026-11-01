package js

import (
	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// collectChildrenAndRemoveFromOldParent returns the nodes inserting node
// stands for: a DocumentFragment's children, detached (leaving the
// fragment empty), or node itself, which html.Node.InsertBefore detaches
// from its old parent. Mirrors
// Blink's CollectChildrenAndRemoveFromOldParent (core/dom/container_node.cc:
// 168-184 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which
// ContainerNode::AppendChild / InsertBefore / ReplaceChild call first.
func collectChildrenAndRemoveFromOldParent(node *html.Node) []*html.Node {
	if node.Type == html.DocumentFragmentNode {
		nodes := node.Children
		for _, child := range nodes {
			child.Parent = nil
		}
		node.Children = nil
		return nodes
	}
	return []*html.Node{node}
}

// convertNodeUnionsIntoNodes converts ParentNode/ChildNode method arguments
// into the nodes to insert: strings become text nodes and a
// DocumentFragment contributes its children. Mirrors Blink's
// Node::ConvertNodeUnionsIntoNodes (core/dom/node.cc:915-997 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the caller's InsertBefore
// detaches each node from its old parent.
func (ctx *domContext) convertNodeUnionsIntoNodes(args []goja.Value) []*html.Node {
	var nodes []*html.Node
	for _, arg := range args {
		node := ctx.unwrapNode(arg)
		switch {
		case node == nil:
			nodes = append(nodes, &html.Node{Type: html.TextNode, Text: arg.String()})
		case node.Type == html.DocumentFragmentNode:
			nodes = append(nodes, collectChildrenAndRemoveFromOldParent(node)...)
		default:
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// insertNodesBefore inserts nodes, in order, before ref (nil appends).
func insertNodesBefore(parent *html.Node, nodes []*html.Node, ref *html.Node) {
	for _, n := range nodes {
		parent.InsertBefore(n, ref)
	}
}

// appendChildFn returns a JS function that implements node.appendChild(child).
// Cross-document moves take no special action — the next cascade pass
// re-parses the moved node's inline style against the new owner doc's
// BaseDir, which is louis14's equivalent of Blink's
// Element::DidMoveToNewDocument URL re-resolution hook.
func (e *elementAccessor) appendChildFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'appendChild': 1 argument required"))
		}
		child := e.ctx.unwrapNode(call.Arguments[0])
		if child == nil {
			panic(e.ctx.vm.NewTypeError("Failed to execute 'appendChild': parameter is not a Node"))
		}
		insertNodesBefore(e.node, collectChildrenAndRemoveFromOldParent(child), nil)
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
		// Blur focus if the focused element is inside the about-to-be-removed
		// subtree. Must run BEFORE detach so the ancestor-walk in
		// SetFocusedElement can reach the still-attached ancestors and
		// clear their HasFocusWithin bits.
		e.ctx.doc.NotifyNodeDetached(child)
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
		insertNodesBefore(e.node, collectChildrenAndRemoveFromOldParent(newChild), refChild)
		return e.ctx.elementProxy(newChild)
	}
}

// setInnerHTML parses the HTML string and replaces the node's children.
func (e *elementAccessor) setInnerHTML(htmlStr string) {
	// Blur focus if any existing child contains the focused element — those
	// children are about to be detached.
	for _, c := range e.node.Children {
		e.ctx.doc.NotifyNodeDetached(c)
	}
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

// appendFn returns a JS function for element.append(...nodes) (Node::append,
// core/dom/node.cc:1020 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (e *elementAccessor) appendFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		insertNodesBefore(e.node, e.ctx.convertNodeUnionsIntoNodes(call.Arguments), nil)
		return goja.Undefined()
	}
}

// prependFn returns a JS function for element.prepend(...nodes)
// (Node::prepend, core/dom/node.cc:999 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (e *elementAccessor) prependFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		nodes := e.ctx.convertNodeUnionsIntoNodes(call.Arguments)
		var firstChild *html.Node
		if len(e.node.Children) > 0 {
			firstChild = e.node.Children[0]
		}
		insertNodesBefore(e.node, nodes, firstChild)
		return goja.Undefined()
	}
}

// beforeFn returns a JS function for element.before(...nodes) (Node::before,
// core/dom/node.cc:1040 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (e *elementAccessor) beforeFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if e.node.Parent == nil {
			return goja.Undefined()
		}
		parent := e.node.Parent
		insertNodesBefore(parent, e.ctx.convertNodeUnionsIntoNodes(call.Arguments), e.node)
		return goja.Undefined()
	}
}

// afterFn returns a JS function for element.after(...nodes) (Node::after,
// core/dom/node.cc:1060 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
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
		insertNodesBefore(parent, e.ctx.convertNodeUnionsIntoNodes(call.Arguments), refNode)
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
		insertNodesBefore(parent, e.ctx.convertNodeUnionsIntoNodes(call.Arguments), e.node)
		// Remove this node
		e.ctx.doc.NotifyNodeDetached(e.node)
		parent.RemoveChild(e.node)
		return goja.Undefined()
	}
}

// replaceChildFn returns a JS function for parent.replaceChild(newChild, oldChild).
func (e *elementAccessor) replaceChildFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		newChild := e.ctx.unwrapNode(call.Arguments[0])
		oldChild := e.ctx.unwrapNode(call.Arguments[1])
		if newChild == nil || oldChild == nil {
			return goja.Undefined()
		}
		insertNodesBefore(e.node, collectChildrenAndRemoveFromOldParent(newChild), oldChild)
		e.ctx.doc.NotifyNodeDetached(oldChild)
		e.node.RemoveChild(oldChild)
		return e.ctx.elementProxy(oldChild)
	}
}

// replaceChildrenFn returns a JS function for element.replaceChildren(...nodes).
func (e *elementAccessor) replaceChildrenFn() func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		// Convert first: an argument may be one of the children being
		// cleared (Node::replaceChildren, core/dom/node.cc:1314 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		nodes := e.ctx.convertNodeUnionsIntoNodes(call.Arguments)
		for _, c := range e.node.Children {
			// Blur focus if this about-to-be-removed child contains it.
			e.ctx.doc.NotifyNodeDetached(c)
			c.Parent = nil
		}
		e.node.Children = nil
		insertNodesBefore(e.node, nodes, nil)
		return goja.Undefined()
	}
}
