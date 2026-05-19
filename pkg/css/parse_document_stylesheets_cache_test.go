package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestParseDocumentStylesheets_CachesParsedStylesheet verifies the
// per-document cache: calling ParseDocumentStylesheets twice on the same
// Document returns the same *Stylesheet pointers without re-parsing.
// This is the load-bearing claim that lets cascade, layout tree builder,
// and the renderer share one parse per layout pass.
func TestParseDocumentStylesheets_CachesParsedStylesheet(t *testing.T) {
	doc := html.NewDocument()
	doc.Stylesheets = []string{
		`.a { color: red; }`,
		`.b { color: blue; }`,
	}

	first := ParseDocumentStylesheets(doc)
	second := ParseDocumentStylesheets(doc)

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected 2 stylesheets each call, got first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("stylesheet[%d] re-parsed across calls: first=%p second=%p", i, first[i], second[i])
		}
	}
}

// TestParseDocumentStylesheets_InvalidatesOnTextChange verifies the cache
// is rebuilt when doc.Stylesheets contents change (e.g. JS appending a
// <style> between layout passes). Identity must NOT be preserved across a
// content change; otherwise consumers would see stale rules.
func TestParseDocumentStylesheets_InvalidatesOnTextChange(t *testing.T) {
	doc := html.NewDocument()
	doc.Stylesheets = []string{`.a { color: red; }`}

	first := ParseDocumentStylesheets(doc)
	if len(first) != 1 {
		t.Fatalf("expected 1 stylesheet, got %d", len(first))
	}

	// Mutate the source — same length, different text. Cache must invalidate.
	doc.Stylesheets[0] = `.a { color: green; }`
	second := ParseDocumentStylesheets(doc)
	if len(second) != 1 {
		t.Fatalf("expected 1 stylesheet after mutation, got %d", len(second))
	}
	if first[0] == second[0] {
		t.Errorf("stylesheet[0] not re-parsed after text mutation — cache stale")
	}

	// Append a sheet — length differs. Cache must invalidate.
	doc.Stylesheets = append(doc.Stylesheets, `.b { color: blue; }`)
	third := ParseDocumentStylesheets(doc)
	if len(third) != 2 {
		t.Fatalf("expected 2 stylesheets after append, got %d", len(third))
	}
	if third[0] == second[0] {
		t.Errorf("stylesheet[0] not re-parsed after length change — cache stale")
	}
}

// TestParseDocumentStylesheets_CacheRespectsBaseDir verifies cached
// stylesheets carry the document's BaseDir through to url() rewriting.
// Mirrors Phase 2's parse-time invariant — the cache must not bypass it.
func TestParseDocumentStylesheets_CacheRespectsBaseDir(t *testing.T) {
	doc := html.NewDocument()
	doc.BaseDir = "sub"
	doc.Stylesheets = []string{`.x { background-image: url(foo.png); }`}

	parsed := ParseDocumentStylesheets(doc)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 stylesheet, got %d", len(parsed))
	}
	got := parsed[0].Rules[0].Declarations["background-image"]
	want := "url(sub/foo.png)"
	if got != want {
		t.Errorf("background-image = %q, want %q", got, want)
	}

	// Second call must return the same pointer AND the same rewritten value.
	again := ParseDocumentStylesheets(doc)
	if again[0] != parsed[0] {
		t.Errorf("cache did not return the same *Stylesheet on second call")
	}
}
