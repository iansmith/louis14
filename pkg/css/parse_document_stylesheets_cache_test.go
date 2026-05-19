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
	doc.Stylesheets = []html.StylesheetSource{
		{Text: `.a { color: red; }`},
		{Text: `.b { color: blue; }`},
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
	doc.Stylesheets = []html.StylesheetSource{{Text: `.a { color: red; }`}}

	first := ParseDocumentStylesheets(doc)
	if len(first) != 1 {
		t.Fatalf("expected 1 stylesheet, got %d", len(first))
	}

	// Mutate the source — same length, different text. Cache must invalidate.
	doc.Stylesheets[0] = html.StylesheetSource{Text: `.a { color: green; }`}
	second := ParseDocumentStylesheets(doc)
	if len(second) != 1 {
		t.Fatalf("expected 1 stylesheet after mutation, got %d", len(second))
	}
	if first[0] == second[0] {
		t.Errorf("stylesheet[0] not re-parsed after text mutation — cache stale")
	}

	// Append a sheet — length differs. Cache must invalidate.
	doc.Stylesheets = append(doc.Stylesheets, html.StylesheetSource{Text: `.b { color: blue; }`})
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
	doc.Stylesheets = []html.StylesheetSource{
		{Text: `.x { background-image: url(foo.png); }`},
	}

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

// TestParseDocumentStylesheets_LinkSheetsGetOwnBaseDir verifies that an
// external <link href="X"> sheet resolves url() refs against X's directory,
// not the owning document's BaseDir. This is the central Phase 5 contract.
func TestParseDocumentStylesheets_LinkSheetsGetOwnBaseDir(t *testing.T) {
	doc := html.NewDocument()
	// Top-level doc: BaseDir empty. Link href points into a subdir.
	doc.Stylesheets = []html.StylesheetSource{
		{
			Text: `.x { background-image: url(foo.png); }`,
			Href: "support/sub/styles.css",
		},
	}

	parsed := ParseDocumentStylesheets(doc)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 stylesheet, got %d", len(parsed))
	}
	got := parsed[0].Rules[0].Declarations["background-image"]
	want := "url(support/sub/foo.png)"
	if got != want {
		t.Errorf("link sheet url() should resolve against sheet's own dir:\n"+
			"  got  = %q\n  want = %q", got, want)
	}
}

// TestParseDocumentStylesheets_InvalidatesOnBaseDirChange verifies the
// cache invalidates when doc.BaseDir shifts (e.g. an iframe layout pass
// after the parent's BaseDir resolves). Otherwise per-sheet BaseDirs
// computed against the stale doc base would persist across the change.
func TestParseDocumentStylesheets_InvalidatesOnBaseDirChange(t *testing.T) {
	doc := html.NewDocument()
	doc.Stylesheets = []html.StylesheetSource{
		{Text: `.x { background-image: url(foo.png); }`, Href: "styles.css"},
	}

	// First pass: doc.BaseDir empty. sheetBaseDir reduces to
	// URLDir(CompleteURL("styles.css", "")) = URLDir("styles.css") = ".",
	// and ParserContext treats "." as a no-op (parser_context.go:95). The
	// url() therefore stays unresolved — the contract this assertion pins.
	first := ParseDocumentStylesheets(doc)
	firstURL := first[0].Rules[0].Declarations["background-image"]
	if firstURL != "url(foo.png)" {
		t.Fatalf("with empty doc.BaseDir + href=\"styles.css\": expected url(foo.png) (dot-base no-op), got %q", firstURL)
	}

	// doc.BaseDir shifts (iframe nested base resolved). Re-cascade must
	// re-derive per-sheet BaseDirs.
	doc.BaseDir = "iframe-dir"
	second := ParseDocumentStylesheets(doc)
	if first[0] == second[0] {
		t.Errorf("cache did not invalidate after doc.BaseDir changed")
	}
	secondURL := second[0].Rules[0].Declarations["background-image"]
	wantSecond := "url(iframe-dir/foo.png)"
	if secondURL != wantSecond {
		t.Errorf("after doc.BaseDir=iframe-dir, link sheet should resolve url() against iframe-dir/:\n  got  = %q\n  want = %q",
			secondURL, wantSecond)
	}
}
