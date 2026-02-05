package js

import (
	"strconv"
	"strings"

	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// newClassListProxy creates a JS DynamicObject implementing the DOMTokenList
// interface for element.classList.
func newClassListProxy(ctx *domContext, node *html.Node) goja.Value {
	return ctx.vm.NewDynamicObject(&classListAccessor{ctx: ctx, node: node})
}

type classListAccessor struct {
	ctx  *domContext
	node *html.Node
}

func (cl *classListAccessor) classes() []string {
	attr, _ := cl.node.GetAttribute("class")
	if attr == "" {
		return nil
	}
	return strings.Fields(attr)
}

func (cl *classListAccessor) setClasses(classes []string) {
	if cl.node.Attributes == nil {
		cl.node.Attributes = make(map[string]string)
	}
	cl.node.Attributes["class"] = strings.Join(classes, " ")
}

func (cl *classListAccessor) Get(key string) goja.Value {
	vm := cl.ctx.vm
	classes := cl.classes()

	switch key {
	case "length":
		return vm.ToValue(len(classes))
	case "value":
		return vm.ToValue(strings.Join(classes, " "))
	case "add":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			cls := cl.classes()
			for _, arg := range call.Arguments {
				token := arg.String()
				if !containsToken(cls, token) {
					cls = append(cls, token)
				}
			}
			cl.setClasses(cls)
			return goja.Undefined()
		})
	case "remove":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			cls := cl.classes()
			for _, arg := range call.Arguments {
				token := arg.String()
				cls = removeToken(cls, token)
			}
			cl.setClasses(cls)
			return goja.Undefined()
		})
	case "toggle":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(vm.NewTypeError("Failed to execute 'toggle': 1 argument required"))
			}
			token := call.Arguments[0].String()
			cls := cl.classes()

			// Optional force parameter
			if len(call.Arguments) > 1 {
				force := call.Arguments[1].ToBoolean()
				if force {
					if !containsToken(cls, token) {
						cls = append(cls, token)
					}
					cl.setClasses(cls)
					return vm.ToValue(true)
				}
				cls = removeToken(cls, token)
				cl.setClasses(cls)
				return vm.ToValue(false)
			}

			if containsToken(cls, token) {
				cls = removeToken(cls, token)
				cl.setClasses(cls)
				return vm.ToValue(false)
			}
			cls = append(cls, token)
			cl.setClasses(cls)
			return vm.ToValue(true)
		})
	case "contains":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return vm.ToValue(false)
			}
			token := call.Arguments[0].String()
			return vm.ToValue(containsToken(classes, token))
		})
	case "replace":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) < 2 {
				panic(vm.NewTypeError("Failed to execute 'replace': 2 arguments required"))
			}
			oldToken := call.Arguments[0].String()
			newToken := call.Arguments[1].String()
			cls := cl.classes()
			replaced := false
			for i, c := range cls {
				if c == oldToken {
					cls[i] = newToken
					replaced = true
					break
				}
			}
			if replaced {
				cl.setClasses(cls)
			}
			return vm.ToValue(replaced)
		})
	case "item":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				return goja.Null()
			}
			idx := int(call.Arguments[0].ToInteger())
			if idx < 0 || idx >= len(classes) {
				return goja.Null()
			}
			return vm.ToValue(classes[idx])
		})
	case "toString":
		return vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(strings.Join(classes, " "))
		})
	default:
		// Numeric index access
		if idx, err := strconv.Atoi(key); err == nil && idx >= 0 && idx < len(classes) {
			return vm.ToValue(classes[idx])
		}
	}
	return goja.Undefined()
}

func (cl *classListAccessor) Set(key string, val goja.Value) bool {
	if key == "value" {
		if cl.node.Attributes == nil {
			cl.node.Attributes = make(map[string]string)
		}
		cl.node.Attributes["class"] = val.String()
		return true
	}
	return false
}

func (cl *classListAccessor) Has(key string) bool {
	switch key {
	case "length", "value", "add", "remove", "toggle", "contains",
		"replace", "item", "toString":
		return true
	}
	if idx, err := strconv.Atoi(key); err == nil && idx >= 0 {
		return true
	}
	return false
}

func (cl *classListAccessor) Delete(key string) bool {
	return false
}

func (cl *classListAccessor) Keys() []string {
	return []string{"length", "value", "add", "remove", "toggle",
		"contains", "replace", "item", "toString"}
}

func containsToken(tokens []string, token string) bool {
	for _, t := range tokens {
		if t == token {
			return true
		}
	}
	return false
}

func removeToken(tokens []string, token string) []string {
	result := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if t != token {
			result = append(result, t)
		}
	}
	return result
}
