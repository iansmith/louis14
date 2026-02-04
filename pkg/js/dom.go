package js

import (
	"strconv"
	"strings"
	"unicode"

	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// registerDocument sets up the global `document` object on the goja runtime.
func registerDocument(vm *goja.Runtime, doc *html.Document) {
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
		return newElementProxy(vm, node)
	})
	docObj.Set("getElementsByTagName", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return newElementArray(vm, nil)
		}
		tag := strings.ToLower(call.Arguments[0].String())
		nodes := getElementsByTagName(doc.Root, tag)
		return newElementArray(vm, nodes)
	})
	docObj.Set("getElementsByClassName", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return newElementArray(vm, nil)
		}
		cls := call.Arguments[0].String()
		nodes := getElementsByClassName(doc.Root, cls)
		return newElementArray(vm, nodes)
	})
	vm.Set("document", docObj)
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

// newElementArray creates a JS array of Element proxies.
func newElementArray(vm *goja.Runtime, nodes []*html.Node) goja.Value {
	arr := vm.NewArray()
	for i, n := range nodes {
		arr.Set(strconv.Itoa(i), newElementProxy(vm, n))
	}
	arr.Set("length", len(nodes))
	return arr
}

// newElementProxy creates a JS DynamicObject that wraps an html.Node,
// supporting get/set for textContent, className, and other DOM properties.
func newElementProxy(vm *goja.Runtime, node *html.Node) goja.Value {
	return vm.NewDynamicObject(&elementAccessor{vm: vm, node: node})
}

// elementAccessor implements goja.DynamicObject to intercept property access
// on DOM element proxies.
type elementAccessor struct {
	vm   *goja.Runtime
	node *html.Node
}

func (e *elementAccessor) Get(key string) goja.Value {
	switch key {
	case "tagName":
		return e.vm.ToValue(strings.ToUpper(e.node.TagName))
	case "id":
		id, _ := e.node.GetAttribute("id")
		return e.vm.ToValue(id)
	case "className":
		cls, _ := e.node.GetAttribute("class")
		return e.vm.ToValue(cls)
	case "textContent":
		return e.vm.ToValue(getTextContent(e.node))
	case "getAttribute":
		return e.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return goja.Null()
			}
			name := call.Arguments[0].String()
			val, ok := e.node.GetAttribute(name)
			if !ok {
				return goja.Null()
			}
			return e.vm.ToValue(val)
		})
	case "setAttribute":
		return e.vm.ToValue(func(call goja.FunctionCall) goja.Value {
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
	case "children":
		var elChildren []*html.Node
		for _, child := range e.node.Children {
			if child.Type == html.ElementNode {
				elChildren = append(elChildren, child)
			}
		}
		return newElementArray(e.vm, elChildren)
	case "parentElement":
		if e.node.Parent != nil && e.node.Parent.Type == html.ElementNode {
			return newElementProxy(e.vm, e.node.Parent)
		}
		return goja.Null()
	case "style":
		return newStyleProxy(e.vm, e.node)
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
	}
	return false
}

func (e *elementAccessor) Has(key string) bool {
	switch key {
	case "tagName", "id", "className", "textContent",
		"getAttribute", "setAttribute", "children",
		"parentElement", "style":
		return true
	}
	return false
}

func (e *elementAccessor) Delete(key string) bool {
	return false
}

func (e *elementAccessor) Keys() []string {
	return []string{"tagName", "id", "className", "textContent",
		"getAttribute", "setAttribute", "children", "parentElement", "style"}
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
