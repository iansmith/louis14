package js

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"louis14/pkg/html"

	"github.com/dop251/goja"
)

// TestWPT runs cherry-picked Web Platform Tests through the goja engine
// with our DOM bindings and a testharness.js shim.
func TestWPT(t *testing.T) {
	// Load the testharness shim
	shimPath := filepath.Join("testdata", "testharness-shim.js")
	shimBytes, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("failed to read testharness shim: %v", err)
	}
	shimJS := string(shimBytes)

	// Find all WPT test files
	pattern := filepath.Join("testdata", "wpt", "*.html")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no WPT test files found")
	}

	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			runWPTFile(t, file, shimJS)
		})
	}
}

func runWPTFile(t *testing.T, path, shimJS string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	// Parse the HTML to get DOM and script blocks.
	// The parser extracts <script> content into doc.Scripts.
	doc, err := html.Parse(string(content))
	if err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}

	// Create engine and register DOM
	vm := goja.New()
	c := &consoleAPI{}
	c.register(vm)
	ctx := registerDocument(vm, doc)
	_ = ctx

	// Run the testharness shim first
	_, err = vm.RunString(shimJS)
	if err != nil {
		t.Fatalf("failed to run testharness shim: %v", err)
	}

	// Run scripts extracted by the parser
	for i, script := range doc.Scripts {
		_, err = vm.RunString(script)
		if err != nil {
			t.Fatalf("script %d failed: %v", i, err)
		}
	}

	// Read results
	resultsVal := vm.Get("__wpt_results")
	if resultsVal == nil || goja.IsUndefined(resultsVal) || goja.IsNull(resultsVal) {
		t.Fatal("no __wpt_results found")
	}

	obj := resultsVal.ToObject(vm)
	length := obj.Get("length")
	if length == nil {
		t.Fatal("__wpt_results has no length")
	}

	n := int(length.ToInteger())
	if n == 0 {
		t.Log("WARNING: no tests ran")
		return
	}

	passed := 0
	failed := 0
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("%d", i)
		item := obj.Get(key)
		if item == nil {
			continue
		}
		itemObj := item.ToObject(vm)
		name := itemObj.Get("name").String()
		status := itemObj.Get("status").String()
		msg := itemObj.Get("message").String()

		if status == "PASS" {
			passed++
			t.Logf("  PASS: %s", name)
		} else {
			failed++
			t.Errorf("  FAIL: %s — %s", name, msg)
		}
	}

	t.Logf("Results: %d passed, %d failed out of %d tests", passed, failed, n)
}
