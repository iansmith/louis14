package js

import (
	"fmt"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"

	"github.com/dop251/goja"
)

// Engine executes JavaScript against an HTML document's DOM.
type Engine struct {
	vm *goja.Runtime
	// onloadCallbacks stores element-level onload callbacks registered via
	// element.onload = fn. Keyed by *html.Node for fast lookup.
	onloadCallbacks map[*html.Node]goja.Callable

	// Layout snapshot from the most recent layout pass, used to back
	// getComputedStyle. Set via SetLayoutSnapshot before Execute.
	layoutStyles map[*html.Node]*css.Style
	layoutBoxes  map[*html.Node]*layout.Box
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

	// requestAnimationFrame: execute callbacks immediately in our
	// single-threaded test environment. Tests use nested rAF calls to wait
	// for "next frame" — in our case, we execute synchronously same tick.
	var rafID int64
	vm.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value {
		rafID++
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			// Pass fake DOMHighResTimeStamp (0) as first argument
			fn(goja.Undefined(), vm.ToValue(0)) //nolint:errcheck
		}
		return vm.ToValue(rafID)
	})
	// cancelAnimationFrame: no-op in test env (rAF has already fired synchronously).
	vm.Set("cancelAnimationFrame", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	return e
}

// RegisterOnloadCallback stores an element-level onload callback.
// Called from DOM bindings when JS does element.onload = fn.
func (e *Engine) RegisterOnloadCallback(node *html.Node, fn goja.Callable) {
	e.onloadCallbacks[node] = fn
}

// SetLayoutSnapshot stores the computed-style map and node→box index from the
// most recent layout pass so that getComputedStyle can return real values.
// Callers should invoke this after layout completes but before Execute.
func (e *Engine) SetLayoutSnapshot(styles map[*html.Node]*css.Style, boxes map[*html.Node]*layout.Box) {
	e.layoutStyles = styles
	e.layoutBoxes = boxes
}

// Execute runs all scripts from the document against the DOM.
// Scripts are executed in order. Any JS errors are returned but
// callers may choose to log and continue rather than fail.
// After all scripts run, any onload handler registered on the global
// scope (window.onload = ... or just onload = ...) is called.
// Then <body onload="..."> attribute is fired, and finally any
// element-level onload callbacks (e.g. iframe.onload) are fired.
func (e *Engine) Execute(doc *html.Document) error {
	// Register document global pointing at this document's DOM
	ctx := registerDocument(e.vm, doc)
	// Give the DOM context a reference to the engine so element proxies
	// can register onload callbacks.
	ctx.engine = e
	ctx.layoutStyles = e.layoutStyles
	ctx.layoutBoxes = e.layoutBoxes

	// Execute each script in document order
	for i, script := range doc.Scripts {
		_, err := e.vm.RunString(script)
		if err != nil {
			return fmt.Errorf("script %d: %w", i, err)
		}
	}

	// Fire window.onload if any script registered it
	if onloadVal := e.vm.Get("onload"); onloadVal != nil &&
		!goja.IsUndefined(onloadVal) && !goja.IsNull(onloadVal) {
		if fn, ok := goja.AssertFunction(onloadVal); ok {
			if _, err := fn(goja.Undefined()); err != nil {
				return fmt.Errorf("onload: %w", err)
			}
		}
	}

	// Fire <body onload="..."> HTML attribute if present.
	// Per HTML spec, the body element's onload attribute is an event handler
	// for the Window's load event. Many WPT tests use this form directly.
	if bodyNode := findBodyNode(doc.Root); bodyNode != nil {
		if onloadAttr, ok := bodyNode.GetAttribute("onload"); ok && onloadAttr != "" {
			if _, err := e.vm.RunString(onloadAttr); err != nil {
				return fmt.Errorf("body onload: %w", err)
			}
		}
	}

	// Fire element-level onload callbacks (e.g. iframe.onload = fn).
	// These were registered during script execution above. The iframe has
	// already loaded (first layout pass fetched the content), so firing
	// onload now is correct. rAF callbacks are synchronous in our engine,
	// so any DOM mutations they make are immediately visible.
	for node, fn := range e.onloadCallbacks {
		_ = node // node identity tracked for future use
		if _, err := fn(goja.Undefined()); err != nil {
			// Log but continue — one broken callback shouldn't stop others
			_ = err
		}
	}

	return nil
}

// findBodyNode walks the document tree to find the <body> element.
func findBodyNode(root *html.Node) *html.Node {
	if root == nil {
		return nil
	}
	if root.Type == html.ElementNode && root.TagName == "body" {
		return root
	}
	for _, child := range root.Children {
		if found := findBodyNode(child); found != nil {
			return found
		}
	}
	return nil
}
