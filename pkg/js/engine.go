package js

import (
	"fmt"
	"strings"

	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// Engine executes JavaScript against an HTML document's DOM.
type Engine struct {
	vm              *goja.Runtime
	onloadCallbacks map[*html.Node]goja.Callable
	rafID           int
}

// New creates a new JS engine with a fresh goja runtime.
func New() *Engine {
	vm := goja.New()
	e := &Engine{
		vm:              vm,
		onloadCallbacks: make(map[*html.Node]goja.Callable),
	}

	// Register console API
	c := &consoleAPI{}
	c.register(vm)

	// In browsers, `window` is the global object. Set it so scripts using
	// `window.onload`, `window.document`, etc. work as expected.
	vm.Set("window", vm.GlobalObject())

	// requestAnimationFrame: fires synchronously in single-threaded test env.
	// The callback is called immediately; a fresh int id is returned.
	vm.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value {
		e.rafID++
		id := e.rafID
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			fn(goja.Undefined(), vm.ToValue(0)) //nolint:errcheck
		}
		return vm.ToValue(id)
	})

	// cancelAnimationFrame: no-op in test env (rAF has already fired synchronously).
	vm.Set("cancelAnimationFrame", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	return e
}

// RegisterOnloadCallback stores an element-level onload callback to be fired
// after all scripts have run, mirroring browser element load event dispatch.
func (e *Engine) RegisterOnloadCallback(node *html.Node, fn goja.Callable) {
	e.onloadCallbacks[node] = fn
}

// Execute runs all scripts from the document against the DOM.
// Scripts are executed in order. Any JS errors are returned but
// callers may choose to log and continue rather than fail.
// After all scripts run, any onload handler registered on the global
// scope (window.onload = ... or just onload = ...) is called,
// followed by element-level onload callbacks and the <body onload> attribute.
func (e *Engine) Execute(doc *html.Document) error {
	// Register document global pointing at this document's DOM
	ctx := registerDocument(e.vm, doc)
	// Wire the engine back into the DOM context so elementAccessor.Set
	// can call RegisterOnloadCallback.
	ctx.engine = e

	// Execute each script in document order
	for i, script := range doc.Scripts {
		_, err := e.vm.RunString(script)
		if err != nil {
			return fmt.Errorf("script %d: %w", i, err)
		}
	}

	// Fire window.onload if registered (e.g. `onload = function() {...}`)
	if onloadVal := e.vm.Get("onload"); onloadVal != nil &&
		!goja.IsUndefined(onloadVal) && !goja.IsNull(onloadVal) {
		if fn, ok := goja.AssertFunction(onloadVal); ok {
			if _, err := fn(goja.Undefined()); err != nil {
				return fmt.Errorf("onload: %w", err)
			}
		}
	}

	// Fire <body onload="..."> attribute if present (HTML attribute form).
	if body := findBodyNode(doc.Root); body != nil {
		if attr, ok := body.Attributes["onload"]; ok && attr != "" {
			if _, err := e.vm.RunString(attr); err != nil {
				return fmt.Errorf("body onload attribute: %w", err)
			}
		}
	}

	// Fire element-level onload callbacks (e.g. iframe.onload = fn).
	for _, fn := range e.onloadCallbacks {
		if _, err := fn(goja.Undefined()); err != nil {
			return fmt.Errorf("element onload: %w", err)
		}
	}

	return nil
}

// findBodyNode walks the DOM tree looking for the <body> element.
func findBodyNode(node *html.Node) *html.Node {
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && strings.EqualFold(node.TagName, "body") {
		return node
	}
	for _, child := range node.Children {
		if found := findBodyNode(child); found != nil {
			return found
		}
	}
	return nil
}
