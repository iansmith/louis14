package js

import (
	"strconv"
	"strings"
	"unicode"

	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// domContext holds shared state for DOM bindings within a single execution.
// It maintains a node-to-proxy cache so the same JS object is returned for
// the same underlying *html.Node (needed for === identity checks).
type domContext struct {
	vm    *goja.Runtime
	doc   *html.Document
	cache map[*html.Node]goja.Value
}

func newDOMContext(vm *goja.Runtime, doc *html.Document) *domContext {
	return &domContext{
		vm:    vm,
		doc:   doc,
		cache: make(map[*html.Node]goja.Value),
	}
}

// registerDocument sets up the global `document` object on the goja runtime.
func registerDocument(vm *goja.Runtime, doc *html.Document) *domContext {
	ctx := newDOMContext(vm, doc)

	docObj := vm.NewObject()
	docObj.Set("getElementById", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Null()
		}
		id := call.Arguments[0].String()
		node := getElementById(doc.Root, id)
		if node == nil {
			return goja.Null()
		}
		return ctx.elementProxy(node)
	})
	docObj.Set("getElementsByTagName", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return ctx.elementArray(nil)
		}
		tag := strings.ToLower(call.Arguments[0].String())
		nodes := getElementsByTagName(doc.Root, tag)
		return ctx.elementArray(nodes)
	})
	docObj.Set("getElementsByClassName", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return ctx.elementArray(nil)
		}
		cls := call.Arguments[0].String()
		nodes := getElementsByClassName(doc.Root, cls)
		return ctx.elementArray(nodes)
	})

	// Phase 1: createElement, createTextNode
	docObj.Set("createElement", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(vm.NewTypeError("Failed to execute 'createElement' on 'Document': 1 argument required"))
		}
		tag := strings.ToLower(call.Arguments[0].String())
		node := &html.Node{
			Type:       html.ElementNode,
			TagName:    tag,
			Attributes: make(map[string]string),
			Children:   make([]*html.Node, 0),
		}
		return ctx.elementProxy(node)
	})
	docObj.Set("createTextNode", func(call goja.FunctionCall) goja.Value {
		text := ""
		if len(call.Arguments) > 0 {
			text = call.Arguments[0].String()
		}
		node := &html.Node{
			Type: html.TextNode,
			Text: text,
		}
		return ctx.elementProxy(node)
	})

	// Phase 2: querySelector/querySelectorAll on document
	registerQuerySelectors(ctx, docObj, doc.Root)

	// Phase 4: document.body, document.head, document.documentElement
	registerDocumentProperties(ctx, docObj, doc)

	vm.Set("document", docObj)
	return ctx
}

// getElementById walks the tree and returns the first node with matching id.
func getElementById(node *html.Node, id string) *html.Node {
	if node.Type == html.ElementNode {
		if val, ok := node.Attributes["id"]; ok && val == id {
			return node
		}
	}
	for _, child := range node.Children {
		if found := getElementById(child, id); found != nil {
			return found
		}
	}
	return nil
}

// getElementsByTagName collects all element nodes with the given tag name.
func getElementsByTagName(node *html.Node, tag string) []*html.Node {
	var result []*html.Node
	if node.Type == html.ElementNode && node.TagName == tag {
		result = append(result, node)
	}
	for _, child := range node.Children {
		result = append(result, getElementsByTagName(child, tag)...)
	}
	return result
}

// getElementsByClassName collects all element nodes that have the given class.
func getElementsByClassName(node *html.Node, cls string) []*html.Node {
	var result []*html.Node
	if node.Type == html.ElementNode {
		if classes, ok := node.Attributes["class"]; ok {
			for _, c := range strings.Fields(classes) {
				if c == cls {
					result = append(result, node)
					break
				}
			}
		}
	}
	for _, child := range node.Children {
		result = append(result, getElementsByClassName(child, cls)...)
	}
	return result
}

// elementArray creates a JS array of Element proxies.
func (ctx *domContext) elementArray(nodes []*html.Node) goja.Value {
	arr := ctx.vm.NewArray()
	for i, n := range nodes {
		arr.Set(strconv.Itoa(i), ctx.elementProxy(n))
	}
	arr.Set("length", len(nodes))
	return arr
}

// elementProxy creates (or retrieves from cache) a JS DynamicObject wrapping an html.Node.
func (ctx *domContext) elementProxy(node *html.Node) goja.Value {
	if v, ok := ctx.cache[node]; ok {
		return v
	}
	v := ctx.vm.NewDynamicObject(&elementAccessor{ctx: ctx, node: node})
	ctx.cache[node] = v
	return v
}

// unwrapNode extracts the *html.Node from a goja value that wraps an elementAccessor.
func (ctx *domContext) unwrapNode(val goja.Value) *html.Node {
	if val == nil || goja.IsNull(val) || goja.IsUndefined(val) {
		return nil
	}
	// Look through the cache for a match
	obj := val.ToObject(ctx.vm)
	for node, cached := range ctx.cache {
		if cached.SameAs(obj) {
			return node
		}
	}
	return nil
}

// elementAccessor implements goja.DynamicObject to intercept property access
// on DOM element proxies.
type elementAccessor struct {
	ctx  *domContext
	node *html.Node
}

func (e *elementAccessor) Get(key string) goja.Value {
	vm := e.ctx.vm

	switch key {
	case "__node__":
		// Internal: used by unwrapNode as a fallback identifier
		return vm.ToValue(true)
	case "nodeType":
		if e.node.Type == html.TextNode {
			return vm.ToValue(3) // Node.TEXT_NODE
		}
		return vm.ToValue(1) // Node.ELEMENT_NODE
	case "nodeName":
		if e.node.Type == html.TextNode {
			return vm.ToValue("#text")
		}
		return vm.ToValue(strings.ToUpper(e.node.TagName))
	case "nodeValue":
		if e.node.Type == html.TextNode {
			return vm.ToValue(e.node.Text)
		}
		return goja.Null()
	case "tagName":
		if e.node.Type == html.TextNode {
			return goja.Undefined()
		}
		return vm.ToValue(strings.ToUpper(e.node.TagName))
	case "id":
		id, _ := e.node.GetAttribute("id")
		return vm.ToValue(id)
	case "className":
		cls, _ := e.node.GetAttribute("class")
		return vm.ToValue(cls)
	case "textContent":
		return vm.ToValue(getTextContent(e.node))
	case "getAttribute":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return goja.Null()
			}
			name := call.Arguments[0].String()
			val, ok := e.node.GetAttribute(name)
			if !ok {
				return goja.Null()
			}
			return vm.ToValue(val)
		})
	case "setAttribute":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) < 2 {
				return goja.Undefined()
			}
			name := call.Arguments[0].String()
			val := call.Arguments[1].String()
			if e.node.Attributes == nil {
				e.node.Attributes = make(map[string]string)
			}
			e.node.Attributes[name] = val
			return goja.Undefined()
		})
	case "hasAttribute":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return vm.ToValue(false)
			}
			name := call.Arguments[0].String()
			_, ok := e.node.GetAttribute(name)
			return vm.ToValue(ok)
		})
	case "removeAttribute":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return goja.Undefined()
			}
			name := call.Arguments[0].String()
			if e.node.Attributes != nil {
				delete(e.node.Attributes, name)
			}
			return goja.Undefined()
		})
	case "children":
		var elChildren []*html.Node
		for _, child := range e.node.Children {
			if child.Type == html.ElementNode {
				elChildren = append(elChildren, child)
			}
		}
		return e.ctx.elementArray(elChildren)
	case "childNodes":
		return e.ctx.elementArray(e.node.Children)
	case "parentElement":
		if e.node.Parent != nil && e.node.Parent.Type == html.ElementNode &&
			e.node.Parent.TagName != "document" {
			return e.ctx.elementProxy(e.node.Parent)
		}
		return goja.Null()
	case "parentNode":
		if e.node.Parent != nil {
			if e.node.Parent.TagName == "document" {
				// Return the document object itself? For simplicity, return null
				return goja.Null()
			}
			return e.ctx.elementProxy(e.node.Parent)
		}
		return goja.Null()
	case "style":
		return newStyleProxy(vm, e.node)

	// Mutation methods (Phase 1)
	case "appendChild":
		return vm.ToValue(e.appendChildFn())
	case "removeChild":
		return vm.ToValue(e.removeChildFn())
	case "insertBefore":
		return vm.ToValue(e.insertBeforeFn())
	case "innerHTML":
		return vm.ToValue(e.node.Serialize())
	case "outerHTML":
		return vm.ToValue(e.node.SerializeOuter())

	// Traversal (Phase 4)
	case "firstChild":
		return e.firstChild()
	case "lastChild":
		return e.lastChild()
	case "firstElementChild":
		return e.firstElementChild()
	case "lastElementChild":
		return e.lastElementChild()
	case "nextSibling":
		return e.nextSibling()
	case "previousSibling":
		return e.previousSibling()
	case "nextElementSibling":
		return e.nextElementSibling()
	case "previousElementSibling":
		return e.previousElementSibling()
	case "childElementCount":
		count := 0
		for _, c := range e.node.Children {
			if c.Type == html.ElementNode {
				count++
			}
		}
		return vm.ToValue(count)

	// Selectors (Phase 2)
	case "querySelector":
		return vm.ToValue(querySelectorFn(e.ctx, e.node))
	case "querySelectorAll":
		return vm.ToValue(querySelectorAllFn(e.ctx, e.node))
	case "matches":
		return vm.ToValue(matchesFn(e.ctx, e.node))
	case "closest":
		return vm.ToValue(closestFn(e.ctx, e.node))

	// classList (Phase 3)
	case "classList":
		return newClassListProxy(e.ctx, e.node)

	// Convenience methods (Phase 3)
	case "remove":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if e.node.Parent != nil {
				e.node.Parent.RemoveChild(e.node)
			}
			return goja.Undefined()
		})
	case "append":
		return vm.ToValue(e.appendFn())
	case "prepend":
		return vm.ToValue(e.prependFn())
	case "before":
		return vm.ToValue(e.beforeFn())
	case "after":
		return vm.ToValue(e.afterFn())
	case "replaceWith":
		return vm.ToValue(e.replaceWithFn())
	case "replaceChildren":
		return vm.ToValue(e.replaceChildrenFn())

	// Phase 4
	case "cloneNode":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			deep := false
			if len(call.Arguments) > 0 {
				deep = call.Arguments[0].ToBoolean()
			}
			clone := e.node.CloneNode(deep)
			return e.ctx.elementProxy(clone)
		})
	case "contains":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return vm.ToValue(false)
			}
			other := e.ctx.unwrapNode(call.Arguments[0])
			if other == nil {
				return vm.ToValue(false)
			}
			return vm.ToValue(e.node.Contains(other))
		})
	case "hasChildNodes":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(len(e.node.Children) > 0)
		})

	case "getElementsByTagName":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return e.ctx.elementArray(nil)
			}
			tag := strings.ToLower(call.Arguments[0].String())
			// Search within this element's children (not including self)
			var result []*html.Node
			for _, child := range e.node.Children {
				result = append(result, getElementsByTagName(child, tag)...)
			}
			return e.ctx.elementArray(result)
		})
	case "getElementsByClassName":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return e.ctx.elementArray(nil)
			}
			cls := call.Arguments[0].String()
			var result []*html.Node
			for _, child := range e.node.Children {
				result = append(result, getElementsByClassName(child, cls)...)
			}
			return e.ctx.elementArray(result)
		})
	}
	return goja.Undefined()
}

func (e *elementAccessor) Set(key string, val goja.Value) bool {
	switch key {
	case "textContent":
		setTextContent(e.node, val.String())
		return true
	case "className":
		if e.node.Attributes == nil {
			e.node.Attributes = make(map[string]string)
		}
		e.node.Attributes["class"] = val.String()
		return true
	case "id":
		if e.node.Attributes == nil {
			e.node.Attributes = make(map[string]string)
		}
		e.node.Attributes["id"] = val.String()
		return true
	case "innerHTML":
		e.setInnerHTML(val.String())
		return true
	case "nodeValue":
		if e.node.Type == html.TextNode {
			e.node.Text = val.String()
		}
		return true
	}
	return false
}

func (e *elementAccessor) Has(key string) bool {
	switch key {
	case "tagName", "nodeName", "nodeType", "nodeValue", "id", "className",
		"textContent", "innerHTML", "outerHTML",
		"getAttribute", "setAttribute", "hasAttribute", "removeAttribute",
		"children", "childNodes", "parentElement", "parentNode", "style",
		"appendChild", "removeChild", "insertBefore",
		"firstChild", "lastChild", "firstElementChild", "lastElementChild",
		"nextSibling", "previousSibling", "nextElementSibling", "previousElementSibling",
		"childElementCount",
		"querySelector", "querySelectorAll", "matches", "closest",
		"classList",
		"remove", "append", "prepend", "before", "after", "replaceWith", "replaceChildren",
		"cloneNode", "contains", "hasChildNodes",
		"getElementsByTagName", "getElementsByClassName":
		return true
	}
	return false
}

func (e *elementAccessor) Delete(key string) bool {
	return false
}

func (e *elementAccessor) Keys() []string {
	return []string{
		"tagName", "nodeName", "nodeType", "nodeValue", "id", "className",
		"textContent", "innerHTML", "outerHTML",
		"getAttribute", "setAttribute", "hasAttribute", "removeAttribute",
		"children", "childNodes", "parentElement", "parentNode", "style",
		"appendChild", "removeChild", "insertBefore",
		"firstChild", "lastChild", "firstElementChild", "lastElementChild",
		"nextSibling", "previousSibling", "nextElementSibling", "previousElementSibling",
		"childElementCount",
		"querySelector", "querySelectorAll", "matches", "closest",
		"classList",
		"remove", "append", "prepend", "before", "after", "replaceWith", "replaceChildren",
		"cloneNode", "contains", "hasChildNodes",
		"getElementsByTagName", "getElementsByClassName",
	}
}

// getTextContent returns the concatenated text content of a node and its descendants.
func getTextContent(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Text
	}
	var sb strings.Builder
	for _, child := range node.Children {
		sb.WriteString(getTextContent(child))
	}
	return sb.String()
}

// setTextContent replaces all children with a single text node.
func setTextContent(node *html.Node, text string) {
	node.Children = nil
	if text != "" {
		node.AppendText(text)
	}
}

// newStyleProxy creates a goja DynamicObject that maps JS camelCase
// property access to CSS kebab-case on the node's inline style attribute.
func newStyleProxy(vm *goja.Runtime, node *html.Node) goja.Value {
	return vm.NewDynamicObject(&styleAccessor{vm: vm, node: node})
}

type styleAccessor struct {
	vm   *goja.Runtime
	node *html.Node
}

func (s *styleAccessor) Get(key string) goja.Value {
	cssProp := camelToKebab(key)
	styles := parseInlineStyle(s.getStyleAttr())
	if val, ok := styles[cssProp]; ok {
		return s.vm.ToValue(val)
	}
	return s.vm.ToValue("")
}

func (s *styleAccessor) Set(key string, val goja.Value) bool {
	cssProp := camelToKebab(key)
	styles := parseInlineStyle(s.getStyleAttr())
	styles[cssProp] = val.String()
	s.setStyleAttr(serializeInlineStyle(styles))
	return true
}

func (s *styleAccessor) Has(key string) bool {
	return true
}

func (s *styleAccessor) Delete(key string) bool {
	cssProp := camelToKebab(key)
	styles := parseInlineStyle(s.getStyleAttr())
	delete(styles, cssProp)
	s.setStyleAttr(serializeInlineStyle(styles))
	return true
}

func (s *styleAccessor) Keys() []string {
	styles := parseInlineStyle(s.getStyleAttr())
	keys := make([]string, 0, len(styles))
	for k := range styles {
		keys = append(keys, k)
	}
	return keys
}

func (s *styleAccessor) getStyleAttr() string {
	if s.node.Attributes == nil {
		return ""
	}
	return s.node.Attributes["style"]
}

func (s *styleAccessor) setStyleAttr(val string) {
	if s.node.Attributes == nil {
		s.node.Attributes = make(map[string]string)
	}
	s.node.Attributes["style"] = val
}

// parseInlineStyle parses a CSS inline style string into a map.
func parseInlineStyle(s string) map[string]string {
	result := make(map[string]string)
	s = strings.TrimSpace(s)
	if s == "" {
		return result
	}
	for _, decl := range strings.Split(s, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		idx := strings.IndexByte(decl, ':')
		if idx < 0 {
			continue
		}
		prop := strings.TrimSpace(decl[:idx])
		val := strings.TrimSpace(decl[idx+1:])
		result[prop] = val
	}
	return result
}

// serializeInlineStyle converts a map back to a CSS inline style string.
func serializeInlineStyle(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+": "+v)
	}
	return strings.Join(parts, "; ")
}

// camelToKebab converts a JS camelCase property name to CSS kebab-case.
func camelToKebab(s string) string {
	if s == "cssFloat" {
		return "float"
	}
	var sb strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				sb.WriteByte('-')
			}
			sb.WriteRune(unicode.ToLower(r))
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// newElementProxy is a convenience for callers that don't have a domContext.
// Deprecated: use ctx.elementProxy instead within JS bindings.
func newElementProxy(vm *goja.Runtime, node *html.Node) goja.Value {
	ctx := newDOMContext(vm, nil)
	return ctx.elementProxy(node)
}

// newElementArray is a convenience for callers that don't have a domContext.
// Deprecated: use ctx.elementArray instead within JS bindings.
func newElementArray(vm *goja.Runtime, nodes []*html.Node) goja.Value {
	ctx := newDOMContext(vm, nil)
	return ctx.elementArray(nodes)
}
