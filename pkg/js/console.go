package js

import (
	"fmt"
	"os"
	"strings"

	"github.com/dop251/goja"
)

// consoleAPI implements console.log, console.warn, and console.error.
type consoleAPI struct{}

func (c *consoleAPI) register(vm *goja.Runtime) {
	console := vm.NewObject()
	console.Set("log", c.log)
	console.Set("warn", c.warn)
	console.Set("error", c.errorFn)
	vm.Set("console", console)
}

func (c *consoleAPI) log(call goja.FunctionCall) goja.Value {
	fmt.Println(formatArgs(call.Arguments))
	return goja.Undefined()
}

func (c *consoleAPI) warn(call goja.FunctionCall) goja.Value {
	fmt.Fprintln(os.Stderr, "WARN:", formatArgs(call.Arguments))
	return goja.Undefined()
}

func (c *consoleAPI) errorFn(call goja.FunctionCall) goja.Value {
	fmt.Fprintln(os.Stderr, "ERROR:", formatArgs(call.Arguments))
	return goja.Undefined()
}

func formatArgs(args []goja.Value) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = arg.String()
	}
	return strings.Join(parts, " ")
}
