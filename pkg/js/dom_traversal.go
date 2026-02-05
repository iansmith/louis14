package js

import (
	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// Traversal property methods on elementAccessor

func (e *elementAccessor) firstChild() goja.Value {
	if len(e.node.Children) == 0 {
		return goja.Null()
	}
	return e.ctx.elementProxy(e.node.Children[0])
}

func (e *elementAccessor) lastChild() goja.Value {
	if len(e.node.Children) == 0 {
		return goja.Null()
	}
	return e.ctx.elementProxy(e.node.Children[len(e.node.Children)-1])
}

func (e *elementAccessor) firstElementChild() goja.Value {
	for _, child := range e.node.Children {
		if child.Type == html.ElementNode {
			return e.ctx.elementProxy(child)
		}
	}
	return goja.Null()
}

func (e *elementAccessor) lastElementChild() goja.Value {
	for i := len(e.node.Children) - 1; i >= 0; i-- {
		if e.node.Children[i].Type == html.ElementNode {
			return e.ctx.elementProxy(e.node.Children[i])
		}
	}
	return goja.Null()
}

func (e *elementAccessor) nextSibling() goja.Value {
	if e.node.Parent == nil {
		return goja.Null()
	}
	idx := e.node.IndexInParent()
	if idx < 0 || idx+1 >= len(e.node.Parent.Children) {
		return goja.Null()
	}
	return e.ctx.elementProxy(e.node.Parent.Children[idx+1])
}

func (e *elementAccessor) previousSibling() goja.Value {
	if e.node.Parent == nil {
		return goja.Null()
	}
	idx := e.node.IndexInParent()
	if idx <= 0 {
		return goja.Null()
	}
	return e.ctx.elementProxy(e.node.Parent.Children[idx-1])
}

func (e *elementAccessor) nextElementSibling() goja.Value {
	if e.node.Parent == nil {
		return goja.Null()
	}
	idx := e.node.IndexInParent()
	if idx < 0 {
		return goja.Null()
	}
	for i := idx + 1; i < len(e.node.Parent.Children); i++ {
		if e.node.Parent.Children[i].Type == html.ElementNode {
			return e.ctx.elementProxy(e.node.Parent.Children[i])
		}
	}
	return goja.Null()
}

func (e *elementAccessor) previousElementSibling() goja.Value {
	if e.node.Parent == nil {
		return goja.Null()
	}
	idx := e.node.IndexInParent()
	if idx < 0 {
		return goja.Null()
	}
	for i := idx - 1; i >= 0; i-- {
		if e.node.Parent.Children[i].Type == html.ElementNode {
			return e.ctx.elementProxy(e.node.Parent.Children[i])
		}
	}
	return goja.Null()
}

// registerDocumentProperties adds document.body, document.head, document.documentElement.
func registerDocumentProperties(ctx *domContext, docObj *goja.Object, doc *html.Document) {
	// Define getter-like properties by setting them as values that are
	// looked up dynamically. Since goja doesn't support defineProperty on
	// plain objects easily, we use functions that the docObj.Set will call.
	// However, document.body etc. are properties, not functions.
	// We use the Go-side to set them when accessed.

	// Find <html>, <head>, <body> in the document tree
	findElement := func(tag string) *html.Node {
		// Search direct children of root for <html>
		for _, child := range doc.Root.Children {
			if child.Type == html.ElementNode && child.TagName == tag {
				return child
			}
		}
		// Search inside <html> for <head> and <body>
		for _, child := range doc.Root.Children {
			if child.Type == html.ElementNode && child.TagName == "html" {
				for _, grandchild := range child.Children {
					if grandchild.Type == html.ElementNode && grandchild.TagName == tag {
						return grandchild
					}
				}
			}
		}
		return nil
	}

	// We need to use defineProperty for getter behavior.
	// For simplicity, we'll set them as functions that return values,
	// but since spec says they're properties, let's use goja's
	// Object.DefineAccessorProperty if available.
	// Actually, for a plain Object we can just use a proxy approach.
	// Let's use a simpler approach: set the values lazily.

	// Use goja's defineProperty via JS
	ctx.vm.RunString(`
		Object.defineProperty = Object.defineProperty || function(obj, prop, desc) {
			if (desc.get) obj[prop] = desc.get();
		};
	`)

	// Set document.documentElement
	htmlNode := findElement("html")
	if htmlNode != nil {
		docObj.Set("documentElement", ctx.elementProxy(htmlNode))
	} else {
		docObj.Set("documentElement", goja.Null())
	}

	// Set document.head
	headNode := findElement("head")
	if headNode != nil {
		docObj.Set("head", ctx.elementProxy(headNode))
	} else {
		docObj.Set("head", goja.Null())
	}

	// Set document.body
	bodyNode := findElement("body")
	if bodyNode != nil {
		docObj.Set("body", ctx.elementProxy(bodyNode))
	} else {
		docObj.Set("body", goja.Null())
	}
}
