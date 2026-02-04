package js

import (
	"fmt"

	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// Engine executes JavaScript against an HTML document's DOM.
type Engine struct {
	vm *goja.Runtime
}

// New creates a new JS engine with a fresh goja runtime.
func New() *Engine {
	vm := goja.New()
	e := &Engine{vm: vm}

	// Register console API
	c := &consoleAPI{}
	c.register(vm)

	return e
}

// Execute runs all scripts from the document against the DOM.
// Scripts are executed in order. Any JS errors are returned but
// callers may choose to log and continue rather than fail.
func (e *Engine) Execute(doc *html.Document) error {
	// Register document global pointing at this document's DOM
	registerDocument(e.vm, doc)

	// Execute each script in document order
	for i, script := range doc.Scripts {
		_, err := e.vm.RunString(script)
		if err != nil {
			return fmt.Errorf("script %d: %w", i, err)
		}
	}

	return nil
}
