package css

import (
	"strings"
	"sync"
	"testing"
)

// TestStyleSheetContents_ParseOnce verifies that ParsedStylesheet runs the
// underlying ParseStylesheet at most once per StyleSheetContents instance,
// even under concurrent access. This is the cache contract every consumer
// of the per-document cache relies on.
func TestStyleSheetContents_ParseOnce(t *testing.T) {
	const cssText = `.x { background-image: url(foo.png); } .y { color: red; }`
	contents := NewStyleSheetContents(cssText, NewParserContext("sub"))

	const n = 16
	var wg sync.WaitGroup
	results := make([]*Stylesheet, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			results[i], errs[i] = contents.ParsedStylesheet()
		}()
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("ParsedStylesheet[%d] returned error: %v", i, errs[i])
		}
		if results[i] != results[0] {
			t.Errorf("ParsedStylesheet[%d] returned a different *Stylesheet than [0] — parse ran twice", i)
		}
	}
}

// TestStyleSheetContents_ContextThreadedToParse pins that StyleSheetContents
// passes its Context through to ParseStylesheet so url() rewriting happens
// against the configured BaseDir, not silently degraded to nil.
func TestStyleSheetContents_ContextThreadedToParse(t *testing.T) {
	contents := NewStyleSheetContents(
		`.x { background-image: url(foo.png); }`,
		NewParserContext("sub"),
	)
	parsed, err := contents.ParsedStylesheet()
	if err != nil {
		t.Fatalf("ParsedStylesheet returned error: %v", err)
	}
	if len(parsed.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(parsed.Rules))
	}
	got := parsed.Rules[0].Declarations["background-image"]
	if !strings.Contains(got, "sub/foo.png") {
		t.Errorf("background-image = %q, want url() rewritten to sub/foo.png", got)
	}
}

// TestStyleSheetContents_NilContext verifies the nil-safe contract carries
// through to the wrapper: nil Context behaves like an empty BaseDir.
func TestStyleSheetContents_NilContext(t *testing.T) {
	contents := NewStyleSheetContents(
		`.x { background-image: url(foo.png); }`,
		nil,
	)
	parsed, err := contents.ParsedStylesheet()
	if err != nil {
		t.Fatalf("ParsedStylesheet returned error: %v", err)
	}
	got := parsed.Rules[0].Declarations["background-image"]
	if got != "url(foo.png)" {
		t.Errorf("nil ctx: background-image = %q, want %q (no rewrite)",
			got, "url(foo.png)")
	}
}
