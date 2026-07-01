package js

import (
	"strconv"
	"strings"
	"unicode"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"

	"github.com/dop251/goja"
)

// domContext holds shared state for DOM bindings within a single execution.
// It maintains a node-to-proxy cache so the same JS object is returned for
// the same underlying *html.Node (needed for === identity checks).
type domContext struct {
	vm     *goja.Runtime
	doc    *html.Document
	cache  map[*html.Node]goja.Value
	engine *Engine // back-reference to register onload callbacks; may be nil

	// Layout snapshot for backing getComputedStyle. May be nil if the caller
	// did not populate layout state (e.g. pure DOM-manipulation tests).
	layoutStyles map[*html.Node]*css.Style
	layoutBoxes  map[*html.Node]*layout.Box

	// docEventListeners holds document.addEventListener callbacks keyed by
	// event type. Louis14 is a static one-shot renderer, so only events the
	// engine itself dispatches after script execution (currently WPT's
	// "TestRendered" from /common/reftest-wait.js) ever fire. The raw value
	// is retained so removeEventListener can match by JS identity.
	docEventListeners map[string][]documentEventListener

	// ranges maps each JS Range proxy (from document.createRange() or `new
	// Range()`) back to its Go-side *html.Range, the same
	// proxy-identity-to-Go-state pattern the node cache above uses (and
	// removeEventListener's raw.StrictEquals matching). Looked up by
	// Selection#addRange so it can read the boundary points
	// selectNodeContents/setStart/setEnd already wrote.
	ranges []rangeEntry

	// highlights maps each `new Highlight(...)` JS object (LOU-354,
	// dom_highlight.go) back to its Go-side []*html.Range + priority, the
	// same proxy-identity-to-Go-state pattern ranges above uses for Range
	// proxies. Looked up by CSS.highlights.set so it can read the ranges
	// the Highlight constructor recorded.
	highlights []highlightEntry

	// locationFragment / locationHrefValue back window.location's `hash` /
	// `href` (LOU-349, dom_location.go). locationFragment never includes
	// the leading '#'; locationHrefValue is the raw string last assigned to
	// .href (or "" if only .hash was ever touched — see locationHref's doc
	// comment).
	locationFragment  string
	locationHrefValue string
}

// rangeEntry pairs a Range JS proxy with its backing *html.Range.
type rangeEntry struct {
	proxy goja.Value
	r     *html.Range
}

// highlightEntry pairs a `new Highlight(...)` JS object with its backing
// []*html.Range and priority (LOU-354). priority is stored but never
// consulted by the paint side — no LOU-354 target test sets it (see
// dom_highlight.go's file doc comment) — kept for future-proofing per the
// ticket's checkpoint 1 spec.
type highlightEntry struct {
	obj      goja.Value
	ranges   []*html.Range
	priority int
}

// documentEventListener pairs the callable with the original JS value so
// removeEventListener can compare listener identity per the DOM spec.
type documentEventListener struct {
	raw goja.Value
	fn  goja.Callable
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
	// createElementNS(namespaceURI, qualifiedName). louis14's DOM doesn't
	// track namespaces — element identity in the renderer is by lowercase
	// tag name (matching the tokenizer). The namespace argument is accepted
	// for compatibility with WPT tests that build SVG nodes via
	// document.createElementNS('http://www.w3.org/2000/svg', 'rect').
	docObj.Set("createElementNS", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewTypeError("Failed to execute 'createElementNS' on 'Document': 2 arguments required"))
		}
		tag := strings.ToLower(call.Arguments[1].String())
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

	// document.addEventListener / removeEventListener. Only events the
	// engine dispatches after scripts run (WPT's "TestRendered") ever fire;
	// other event types are stored but never invoked, which matches the
	// one-shot rendering model (no user interaction, no async loads).
	docObj.Set("addEventListener", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) >= 2 {
			raw := call.Arguments[1]
			if fn, ok := goja.AssertFunction(raw); ok {
				typ := call.Arguments[0].String()
				if ctx.docEventListeners == nil {
					ctx.docEventListeners = make(map[string][]documentEventListener)
				}
				ctx.docEventListeners[typ] = append(ctx.docEventListeners[typ],
					documentEventListener{raw: raw, fn: fn})
			}
		}
		return goja.Undefined()
	})
	docObj.Set("removeEventListener", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) >= 2 && ctx.docEventListeners != nil {
			typ := call.Arguments[0].String()
			raw := call.Arguments[1]
			kept := ctx.docEventListeners[typ][:0]
			for _, l := range ctx.docEventListeners[typ] {
				if !l.raw.StrictEquals(raw) {
					kept = append(kept, l)
				}
			}
			ctx.docEventListeners[typ] = kept
		}
		return goja.Undefined()
	})

	// window.addEventListener / bare addEventListener (window IS the global
	// object) share the document listener table — in the one-shot rendering
	// model, window "load" and document listeners fire at the same point
	// (after scripts, before the only screenshot).
	if addFn := docObj.Get("addEventListener"); addFn != nil {
		vm.Set("addEventListener", addFn)
	}
	if rmFn := docObj.Get("removeEventListener"); rmFn != nil {
		vm.Set("removeEventListener", rmFn)
	}

	// Phase 2: querySelector/querySelectorAll on document
	registerQuerySelectors(ctx, docObj, doc.Root)

	// Phase 4: document.body, document.head, document.documentElement
	registerDocumentProperties(ctx, docObj, doc)

	// document.createRange / new Range() / getSelection() / activeElement /
	// execCommand("selectAll") (LOU-344, LOU-354). bodyNode is recomputed
	// via findBodyNode since registerDocumentProperties doesn't expose its
	// own lookup result.
	bodyNode := findBodyNode(doc.Root)
	registerSelectionAPI(ctx, docObj, bodyNode)

	// window.location / document.location (LOU-349).
	registerLocationAPI(ctx, docObj)

	// window.CSS.highlights / new Highlight() (LOU-354).
	registerHighlightAPI(ctx)

	// document.styleSheets CSSOM rule mutation (LOU-351).
	registerStyleSheetsAPI(ctx, docObj)

	vm.Set("document", docObj)

	// HTML5 named access: elements with an id attribute are exposed as
	// globals on the window object (e.g., <div id="flex"> → window.flex).
	registerNamedElements(ctx, doc.Root)

	// getComputedStyle returns a CSSStyleDeclaration proxy backed by the
	// layout snapshot (node→computed style + node→principal Box). For
	// layout-dependent properties (width, height, margin-*, padding-*,
	// border-*-width), the used value is read from the Box and formatted
	// as "Npx". Everything else falls back to the cascade Properties map.
	vm.Set("getComputedStyle", func(call goja.FunctionCall) goja.Value {
		var node *html.Node
		if len(call.Arguments) > 0 {
			node = ctx.unwrapNode(call.Arguments[0])
		}
		return vm.NewDynamicObject(&computedStyleAccessor{
			vm:     vm,
			node:   node,
			styles: ctx.layoutStyles,
			boxes:  ctx.layoutBoxes,
		})
	})

	// forceLayout: WPT tests call this as a layout-flush idiom. No-op for us.
	vm.Set("forceLayout", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	return ctx
}

// registerNamedElements walks the DOM tree and registers elements with id
// attributes as global variables, implementing HTML5 named access on Window.
func registerNamedElements(ctx *domContext, root *html.Node) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if id, ok := n.Attributes["id"]; ok && id != "" {
				// Only set if not already defined (don't shadow builtins like "document")
				if existing := ctx.vm.Get(id); existing == nil || goja.IsUndefined(existing) {
					ctx.vm.Set(id, ctx.elementProxy(n))
				}
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
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

// documentProxy builds a document-like JS object for the given *html.Document.
// It shares the same cache and vm as the outer domContext, so nodes created
// through this proxy (e.g. createTextNode) are discoverable by unwrapNode in
// the outer context — enabling cross-document DOM operations like appendChild.
func (ctx *domContext) documentProxy(doc *html.Document) goja.Value {
	vm := ctx.vm
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
	// querySelector/querySelectorAll on the nested document root
	registerQuerySelectors(ctx, docObj, doc.Root)
	// body, head, documentElement properties
	registerDocumentProperties(ctx, docObj, doc)
	return docObj
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
	case "innerText":
		// HTML Standard innerText (https://html.spec.whatwg.org/#the-innertext-idl-attribute)
		// is CSS-aware (collapses whitespace per CSS rendering, skips
		// display:none subtrees, etc.). For the assignment side that WPT
		// reftests rely on the simple form ("td.innerText = 'X'"),
		// behaving like textContent is observably correct: both replace
		// children with a single text node. The read side falls back to
		// the textContent serialization, which is close enough for the
		// reftests we currently care about; a CSS-aware implementation
		// can land later when a test demands it.
		return vm.ToValue(getTextContent(e.node))
	case "src":
		src, _ := e.node.GetAttribute("src")
		return vm.ToValue(src)
	case "type":
		// HTMLInputElement.type / HTMLButtonElement.type IDL attribute
		// reflects the `type` content attribute. For <input>, an invalid or
		// missing keyword is normalized to "text" per HTML "the input element"
		// spec; for <button> the missing default is "submit". Mirrors Blink
		// core/html/forms/html_input_element.cc::type @ 4883d11fef4a.
		typeAttr, _ := e.node.GetAttribute("type")
		typeAttr = strings.ToLower(strings.TrimSpace(typeAttr))
		switch e.node.TagName {
		case "input":
			switch typeAttr {
			case "text", "search", "url", "tel", "email", "password", "number",
				"date", "datetime-local", "month", "time", "week",
				"hidden", "submit", "image", "reset", "button",
				"checkbox", "radio", "file", "color", "range":
				return vm.ToValue(typeAttr)
			}
			return vm.ToValue("text")
		case "button":
			switch typeAttr {
			case "submit", "reset", "button", "menu":
				return vm.ToValue(typeAttr)
			}
			return vm.ToValue("submit")
		}
		return vm.ToValue("")
	case "value":
		// HTMLInputElement.value / HTMLTextAreaElement.value IDL attribute.
		// For <input>, returns the `value` content attribute (default ""
		// before user interaction). For <textarea>, returns the current
		// text content (which is the default value before any user input).
		// Mirrors Blink core/html/forms/html_input_element.cc::value @
		// 4883d11fef4a.
		switch e.node.TagName {
		case "input":
			v, _ := e.node.GetAttribute("value")
			return vm.ToValue(v)
		case "textarea":
			return vm.ToValue(getTextContent(e.node))
		}
		return vm.ToValue("")
	case "dir":
		// HTMLElement.dir IDL attribute (HTML §3.2.6) reflects the
		// "dir" content attribute, restricted to the canonical keyword
		// set {ltr, rtl, auto} — any other authored value is exposed
		// as the empty string. Mirrors Blink's HTMLElement::dir()
		// (third_party/blink/renderer/core/html/html_element.cc @
		// 4883d11fef4a).
		dirAttr, _ := e.node.GetAttribute("dir")
		switch strings.ToLower(strings.TrimSpace(dirAttr)) {
		case "ltr", "rtl", "auto":
			return vm.ToValue(strings.ToLower(strings.TrimSpace(dirAttr)))
		default:
			return vm.ToValue("")
		}
	case "splitText":
		// Text.splitText(offset): splits the text node at offset, returns the new node.
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if e.node.Type != html.TextNode || len(call.Arguments) == 0 {
				return goja.Undefined()
			}
			offset := int(call.Arguments[0].ToInteger())
			runes := []rune(e.node.Text)
			if offset < 0 || offset > len(runes) {
				return goja.Undefined()
			}
			second := string(runes[offset:])
			e.node.Text = string(runes[:offset])
			newNode := &html.Node{
				Type:   html.TextNode,
				Text:   second,
				Parent: e.node.Parent,
			}
			// Insert newNode immediately after e.node in parent's children.
			if parent := e.node.Parent; parent != nil {
				for i, child := range parent.Children {
					if child == e.node {
						rest := append([]*html.Node{newNode}, parent.Children[i+1:]...)
						parent.Children = append(parent.Children[:i+1], rest...)
						break
					}
				}
			}
			return e.ctx.elementProxy(newNode)
		})
	case "data":
		// CharacterData.data: returns the text content of a Text node.
		if e.node.Type == html.TextNode {
			return vm.ToValue(e.node.Text)
		}
		return goja.Undefined()
	case "appendData":
		// CharacterData.appendData(data): appends the given string to the text node's data.
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if e.node.Type != html.TextNode {
				return goja.Undefined()
			}
			if len(call.Arguments) > 0 {
				e.node.Text += call.Arguments[0].String()
			}
			return goja.Undefined()
		})
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
				e.ctx.doc.NotifyNodeDetached(e.node)
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
	case "replaceChild":
		return vm.ToValue(e.replaceChildFn())
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

	case "contentDocument":
		// HTMLIFrameElement.contentDocument: return a document-like proxy for
		// the nested document if one was retained during layout. This allows
		// cross-document DOM access (e.g. bidi-dynamic-iframe-001 pattern).
		if e.node.TagName == "iframe" || e.node.TagName == "object" {
			if e.node.NestedDocument != nil {
				return e.ctx.documentProxy(e.node.NestedDocument)
			}
		}
		return goja.Null()
	case "contentWindow":
		// HTMLIFrameElement.contentWindow: not fully supported; return null.
		return goja.Null()
	case "getBoundingClientRect":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			// Returns a DOMRect-like object. We don't have layout info here,
			// so return zeros. Tests that call this to force layout (e.g. to
			// flush style changes before reading dimensions) still work correctly
			// because the two-pass render re-runs layout after JS mutations.
			rect := vm.NewObject()
			for _, prop := range []string{"x", "y", "width", "height", "top", "right", "bottom", "left"} {
				_ = rect.Set(prop, 0)
			}
			return rect
		})

	case "offsetTop", "offsetLeft", "offsetWidth", "offsetHeight":
		// Layout-flush idiom: scripts access these to force a synchronous layout.
		// We return 0; the actual value is not used by tests that call these.
		return vm.ToValue(0)

	case "focus":
		// HTMLElement.focus(): make this element the document's focused
		// element. Mirrors Blink's Element::Focus → Document::SetFocusedElement
		// (third_party/blink/renderer/core/dom/element.cc Focus(), then
		// document.cc SetFocusedElement). Selectors 4 §9.4: focus on a
		// descendant flips :focus-within true on all ancestors via the bit
		// chain SetFocusedElement maintains. We accept the call on any element
		// — the spec actually restricts focusability (tabindex, form controls,
		// contenteditable, hyperlinks-with-href), but in a static reftest
		// engine the test author has already asserted the element is
		// focusable by calling .focus() on it.
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if e.ctx.doc != nil {
				e.ctx.doc.SetFocusedElement(e.node)
			}
			return goja.Undefined()
		})
	case "blur":
		// HTMLElement.blur(): clear focus if this element is currently focused.
		// Mirrors Blink's Element::blur → Document::SetFocusedElement(nullptr).
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if e.ctx.doc != nil && e.ctx.doc.FocusedElement == e.node {
				e.ctx.doc.SetFocusedElement(nil)
			}
			return goja.Undefined()
		})

	case "animate":
		// Element.animate(keyframes, options) — Web Animations API
		// (https://drafts.csswg.org/web-animations-1/#dom-animatable-animate).
		// Mirrors Blink ElementAnimation::animate (ElementAnimation::animate in
		// third_party/blink/renderer/core/animation/element_animation.cc) at
		// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		//
		// In the synchronous reftest harness there is no live timeline: the
		// returned Animation object is used exclusively via .pause() + .currentTime
		// assignment to "freeze" the element at a specific progress. When
		// currentTime is set, we compute the interpolated keyframe values at that
		// offset and write them directly into the element's inline style attribute,
		// so the second layout pass in helpers.go picks up the mutation.
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			// Parse keyframes argument: an array of {property: value} objects.
			// Each object maps CSS property names (camelCase or kebab-case) to
			// the value at that keyframe. Offset may be omitted; when omitted,
			// keyframes are distributed evenly across [0,1].
			var kfDecls []map[string]string
			if len(call.Arguments) > 0 {
				arr := call.Arguments[0].ToObject(vm)
				lenVal := arr.Get("length")
				if lenVal != nil && !goja.IsUndefined(lenVal) {
					n := int(lenVal.ToInteger())
					for i := 0; i < n; i++ {
						item := arr.Get(strconv.Itoa(i))
						if item == nil || goja.IsUndefined(item) || goja.IsNull(item) {
							continue
						}
						itemObj := item.ToObject(vm)
						decl := make(map[string]string)
						for _, k := range itemObj.Keys() {
							decl[k] = itemObj.Get(k).String()
						}
						kfDecls = append(kfDecls, decl)
					}
				}
			}

			// Parse options: number → duration in ms; object → {duration}.
			var durationMs float64 = 0
			if len(call.Arguments) > 1 {
				opt := call.Arguments[1]
				if opt.ExportType().Kind().String() == "float64" || !goja.IsUndefined(opt) {
					if obj := opt.ToObject(vm); obj != nil {
						if d := obj.Get("duration"); d != nil && !goja.IsUndefined(d) {
							durationMs = d.ToFloat()
						} else {
							durationMs = opt.ToFloat()
						}
					}
				}
			}

			// Build css.KeyframeRule list from the parsed keyframe objects.
			// Offsets are evenly distributed when not specified by the author.
			node := e.node
			makeRules := func(decls []map[string]string) []css.KeyframeRule {
				n := len(decls)
				if n == 0 {
					return nil
				}
				rules := make([]css.KeyframeRule, n)
				for i, d := range decls {
					var offset float64
					if n == 1 {
						offset = 1
					} else {
						offset = float64(i) / float64(n-1)
					}
					// Explicit "offset" key overrides the implicit even distribution.
					if ov, ok := d["offset"]; ok {
						if f, err := strconv.ParseFloat(strings.TrimSpace(ov), 64); err == nil {
							offset = f
						}
					}
					declarations := make(map[string]string, len(d))
					for k, v := range d {
						if k == "offset" || k == "easing" {
							continue
						}
						prop := camelToKebab(k)
						declarations[prop] = v
					}
					pct := strconv.FormatFloat(offset*100, 'f', -1, 64) + "%"
					rules[i] = css.KeyframeRule{Stop: pct, Declarations: declarations}
				}
				return rules
			}

			kfRules := makeRules(kfDecls)
			effect := css.BuildKeyframeEffect(kfRules, nil)

			// Return an Animation object backed by animationAccessor.
			anim := &animationAccessor{
				vm:          vm,
				node:        node,
				effect:      effect,
				durationMs:  durationMs,
				currentTime: -1, // not yet set
			}
			return vm.NewDynamicObject(anim)
		})
	}
	return goja.Undefined()
}

func (e *elementAccessor) Set(key string, val goja.Value) bool {
	switch key {
	case "textContent":
		setTextContent(e.node, val.String())
		return true
	case "innerText":
		// See the getter comment above. Assigning innerText replaces all
		// children with a single text node containing the assigned value,
		// matching the simple "td.innerText = 'X'" pattern that WPT
		// dynamic-DOM reftests use to populate cells.
		setTextContent(e.node, val.String())
		return true
	case "className":
		if e.node.Attributes == nil {
			e.node.Attributes = make(map[string]string)
		}
		e.node.Attributes["class"] = val.String()
		return true
	case "classList":
		// DOMTokenList is `[SameObject, PutForwards=value]` (DOM §7.1):
		// `element.classList = "x y"` forwards to
		// `element.classList.value = "x y"`. We update the class attribute
		// directly with the string form rather than walking through the
		// proxy, matching the spec contract for PutForwards.
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
	case "style":
		// CSSStyleDeclaration is `[SameObject, PutForwards=cssText]` on the
		// HTMLElement.style attribute (CSSOM §6.7.1): `element.style = "..."`
		// forwards to `element.style.cssText = "..."`, which replaces the whole
		// inline declaration block with the assigned string. We set the `style`
		// content attribute directly to the string form, matching the classList
		// PutForwards case above. Assigning "" clears the inline style.
		if e.node.Attributes == nil {
			e.node.Attributes = make(map[string]string)
		}
		e.node.Attributes["style"] = val.String()
		return true
	case "innerHTML":
		e.setInnerHTML(val.String())
		return true
	case "nodeValue", "data":
		if e.node.Type == html.TextNode {
			e.node.Text = val.String()
		}
		return true
	case "src":
		if e.node.Attributes == nil {
			e.node.Attributes = make(map[string]string)
		}
		e.node.Attributes["src"] = val.String()
		return true
	case "type":
		// HTMLInputElement.type / HTMLButtonElement.type / HTMLSelectElement.type
		// setter: reflects to the `type` content attribute. Per HTML spec, the
		// `<input>` getter normalizes invalid types to "text"; the setter
		// stores the raw value and lets selector matching consult it. Mirrors
		// Blink core/html/forms/html_input_element.cc::setType @
		// 4883d11fef4a — invalid types reflect verbatim so the getter is the
		// place that normalizes.
		switch e.node.TagName {
		case "input", "button", "select":
			if e.node.Attributes == nil {
				e.node.Attributes = make(map[string]string)
			}
			e.node.Attributes["type"] = val.String()
			return true
		}
		return false
	case "value":
		// HTMLInputElement.value / HTMLTextAreaElement.value setter: reflects
		// to the `value` content attribute for <input> (the DOM value and the
		// content attribute are kept in sync until the form is reset). For
		// <textarea> the IDL setter replaces the element's text content.
		// Mirrors Blink core/html/forms/html_input_element.cc::setValue and
		// html_text_area_element.cc::setValue @ 4883d11fef4a.
		switch e.node.TagName {
		case "input":
			if e.node.Attributes == nil {
				e.node.Attributes = make(map[string]string)
			}
			e.node.Attributes["value"] = val.String()
			return true
		case "textarea":
			setTextContent(e.node, val.String())
			return true
		}
		return false
	case "dir":
		// HTMLElement.dir setter: reflects to the "dir" content
		// attribute verbatim (the IDL setter does not normalize —
		// invalid values are stored as-is; the getter is what filters
		// them). Mirrors Blink HTMLElement::setDir().
		if e.node.Attributes == nil {
			e.node.Attributes = make(map[string]string)
		}
		e.node.Attributes["dir"] = val.String()
		return true
	case "onload":
		// Store element-level onload callback so the engine can fire it after
		// iframes (and other resources) have been loaded during layout.
		if e.ctx.engine != nil {
			if fn, ok := goja.AssertFunction(val); ok {
				e.ctx.engine.RegisterOnloadCallback(e.node, fn)
			}
		}
		return true
	}
	return false
}

func (e *elementAccessor) Has(key string) bool {
	switch key {
	case "tagName", "nodeName", "nodeType", "nodeValue", "id", "className",
		"textContent", "innerText", "innerHTML", "outerHTML", "src", "dir", "type", "value", "splitText", "data", "appendData",
		"getAttribute", "setAttribute", "hasAttribute", "removeAttribute",
		"children", "childNodes", "parentElement", "parentNode", "style",
		"appendChild", "removeChild", "insertBefore",
		"firstChild", "lastChild", "firstElementChild", "lastElementChild",
		"nextSibling", "previousSibling", "nextElementSibling", "previousElementSibling",
		"childElementCount",
		"querySelector", "querySelectorAll", "matches", "closest",
		"classList",
		"remove", "append", "prepend", "before", "after", "replaceChild", "replaceWith", "replaceChildren",
		"cloneNode", "contains", "hasChildNodes",
		"getElementsByTagName", "getElementsByClassName",
		"onload",
		"contentDocument", "contentWindow",
		"focus", "blur",
		"animate":
		return true
	}
	return false
}

func (e *elementAccessor) Delete(key string) bool {
	return false
}

func (e *elementAccessor) Keys() []string {
	return []string{
		"tagName", "nodeName", "nodeType", "nodeValue", "data", "id", "className",
		"textContent", "innerText", "innerHTML", "outerHTML",
		"src", "dir", "type", "value",
		"splitText", "appendData",
		"getAttribute", "setAttribute", "hasAttribute", "removeAttribute",
		"children", "childNodes", "parentElement", "parentNode", "style",
		"appendChild", "removeChild", "insertBefore",
		"firstChild", "lastChild", "firstElementChild", "lastElementChild",
		"nextSibling", "previousSibling", "nextElementSibling", "previousElementSibling",
		"childElementCount",
		"querySelector", "querySelectorAll", "matches", "closest",
		"classList",
		"remove", "append", "prepend", "before", "after", "replaceChild", "replaceWith", "replaceChildren",
		"cloneNode", "contains", "hasChildNodes",
		"getElementsByTagName", "getElementsByClassName",
		"onload",
		"contentDocument", "contentWindow",
		"focus", "blur",
		"animate",
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
	// CSSOM §6.7.3 CSSStyleDeclaration methods (LOU-352: highlight-styling-
	// 005.html mutates a root custom property via style.setProperty, which
	// previously fell through to the attribute lookup below and returned a
	// string — a TypeError at the call site, silently killing the script).
	// Mirrors Blink's AbstractPropertySetCSSStyleDeclaration::setProperty/
	// removeProperty/getPropertyValue (core/css/abstract_property_set_css_
	// style_declaration.cc:76,135,159 @ Chromium main
	// 72259ecfb56eacf259e21783dd2b31608ac951a3). The property argument is
	// used VERBATIM (no camelToKebab): CSSOM method arguments are already
	// CSS-cased, and custom properties (--*) are case-sensitive.
	switch key {
	case "setProperty":
		return s.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) < 2 {
				return goja.Undefined()
			}
			prop := call.Arguments[0].String()
			val := call.Arguments[1].String()
			styles := parseInlineStyle(s.getStyleAttr())
			// CSSOM §6.7.3 step 4: an empty value means removeProperty.
			if strings.TrimSpace(val) == "" {
				delete(styles, prop)
			} else {
				styles[prop] = val
			}
			s.setStyleAttr(serializeInlineStyle(styles))
			return goja.Undefined()
		})
	case "removeProperty":
		return s.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return s.vm.ToValue("")
			}
			prop := call.Arguments[0].String()
			styles := parseInlineStyle(s.getStyleAttr())
			old := styles[prop]
			delete(styles, prop)
			s.setStyleAttr(serializeInlineStyle(styles))
			return s.vm.ToValue(old)
		})
	case "getPropertyValue":
		return s.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return s.vm.ToValue("")
			}
			styles := parseInlineStyle(s.getStyleAttr())
			return s.vm.ToValue(styles[call.Arguments[0].String()])
		})
	case "getPropertyPriority":
		// Inline-style !important tracking isn't modeled; always "".
		return s.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return s.vm.ToValue("")
		})
	}
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
	// CSSOM: assigning the empty string removes the declaration
	// (CSSStyleDeclaration attribute setter; `el.style.counterSet = ""`).
	if v := val.String(); strings.TrimSpace(v) == "" {
		delete(styles, cssProp)
	} else {
		styles[cssProp] = v
	}
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

// animationAccessor implements goja.DynamicObject for Animation objects
// returned by Element.animate().
//
// The Web Animations API (https://drafts.csswg.org/web-animations-1/) defines
// Animation with a currentTime getter/setter and pause()/cancel()/finish()
// methods. In the synchronous louis14 reftest harness there is no live
// document timeline, so pause() is a no-op and the real work happens when JS
// assigns animation.currentTime: we sample the KeyframeEffect at the given
// time offset and write the interpolated values into the element's inline style
// attribute so the second layout pass in helpers.go captures the mutation.
//
// Mirrors Blink Animation::setCurrentTime + KeyframeEffect::ApplyEffectToTarget
// (third_party/blink/renderer/core/animation/animation.cc and
// third_party/blink/renderer/core/animation/keyframe_effect.cc)
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
type animationAccessor struct {
	vm          *goja.Runtime
	node        *html.Node
	effect      *css.KeyframeEffect
	durationMs  float64 // from the options argument to animate()
	currentTime float64 // milliseconds; -1 = not yet set
}

func (a *animationAccessor) Get(key string) goja.Value {
	vm := a.vm
	switch key {
	case "currentTime":
		if a.currentTime < 0 {
			return goja.Null()
		}
		return vm.ToValue(a.currentTime)
	case "pause":
		// pause(): in the static harness the animation has no running timeline,
		// so pause is a semantic no-op. The real "pause" is that the JS script
		// then sets currentTime explicitly.
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return goja.Undefined()
		})
	case "cancel", "finish", "play", "reverse":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return goja.Undefined()
		})
	case "playState":
		return vm.ToValue("paused")
	case "ready", "finished":
		// Return a resolved Promise-like. Tests may await these; in our
		// synchronous engine any .then() callback fires immediately.
		p, _ := vm.RunString("Promise.resolve()")
		return p
	}
	return goja.Undefined()
}

func (a *animationAccessor) Set(key string, val goja.Value) bool {
	switch key {
	case "currentTime":
		ms := val.ToFloat()
		a.currentTime = ms
		a.applyAtTime(ms)
		return true
	}
	return false
}

func (a *animationAccessor) Has(key string) bool {
	switch key {
	case "currentTime", "pause", "cancel", "finish", "play", "reverse", "playState", "ready", "finished":
		return true
	}
	return false
}

func (a *animationAccessor) Delete(key string) bool { return false }

func (a *animationAccessor) Keys() []string {
	return []string{"currentTime", "pause", "cancel", "finish", "play", "reverse", "playState"}
}

// applyAtTime samples the KeyframeEffect at time ms and writes the resulting
// property values into the element's inline style attribute. The duration
// provides the denominator for converting ms → [0,1] progress.
func (a *animationAccessor) applyAtTime(ms float64) {
	if a.effect == nil || a.node == nil {
		return
	}
	dur := a.durationMs
	if dur <= 0 {
		return
	}
	progress := ms / dur
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	// Sample keyframes with no base style (all values are explicit in the
	// keyframe list for the in-scope tests).
	values := css.ResolveKeyframeStyle(a.effect, progress, nil)
	if len(values) == 0 {
		return
	}
	// Merge into the element's inline style.
	if a.node.Attributes == nil {
		a.node.Attributes = make(map[string]string)
	}
	inline := parseInlineStyle(a.node.Attributes["style"])
	for prop, v := range values {
		inline[prop] = v
	}
	a.node.Attributes["style"] = serializeInlineStyle(inline)
}
