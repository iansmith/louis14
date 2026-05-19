package css

import "testing"

// TestParserContext_CompleteURL pins the ParserContext URL resolver
// contract for filesystem-style bases (WPT fixtures): absolute and data:
// refs pass through, an empty BaseDir leaves the ref untouched, and
// relative refs are joined via path semantics. The absolute-base regime
// (`https://example.com/...`) is covered by TestCompleteURL in url_test.go.
func TestParserContext_CompleteURL(t *testing.T) {
	cases := []struct {
		name    string
		baseDir string
		raw     string
		want    string
	}{
		{"absolute https passthrough", "support", "https://example.com/foo.svg", "https://example.com/foo.svg"},
		{"absolute http passthrough", "support", "http://example.com/foo.svg", "http://example.com/foo.svg"},
		{"absolute file passthrough", "support", "file:///tmp/foo.svg", "file:///tmp/foo.svg"},
		{"root-relative passthrough", "support", "/foo.svg", "/foo.svg"},
		{"data url passthrough", "support", "data:image/png;base64,AAA", "data:image/png;base64,AAA"},
		{"empty base passes through relative", "", "foo.svg", "foo.svg"},
		{"dot base passes through relative", ".", "foo.svg", "foo.svg"},
		{"relative joined", "support", "foo.svg", "support/foo.svg"},
		{"relative with dotdot normalises", "support/imports", "../foo.svg", "support/foo.svg"},
		{"nested relative joined", "a/b", "c/d.svg", "a/b/c/d.svg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := NewParserContext(c.baseDir)
			got := ctx.CompleteURL(c.raw)
			if got != c.want {
				t.Fatalf("ctx{%q}.CompleteURL(%q) = %q, want %q",
					c.baseDir, c.raw, got, c.want)
			}
		})
	}
}

// TestParserContext_CollectUrlData verifies the Blink chokepoint records
// both the pre-resolution token (Relative) and the post-resolution form
// (Absolute) — matching CSSUrlData's two-field shape so future getComputed
// Style / serialization consumers can read either form without re-parsing.
func TestParserContext_CollectUrlData(t *testing.T) {
	ctx := NewParserContext("support")
	got := ctx.CollectUrlData("foo.svg")
	if got.Relative != "foo.svg" {
		t.Errorf("Relative = %q, want %q", got.Relative, "foo.svg")
	}
	if got.Absolute != "support/foo.svg" {
		t.Errorf("Absolute = %q, want %q", got.Absolute, "support/foo.svg")
	}

	// Absolute refs: Relative captures the raw token; Absolute matches it
	// (CompleteURL short-circuits scheme-prefixed inputs).
	abs := ctx.CollectUrlData("https://example.com/x.svg")
	if abs.Relative != "https://example.com/x.svg" || abs.Absolute != "https://example.com/x.svg" {
		t.Errorf("absolute case: got {%q, %q}, want both %q",
			abs.Relative, abs.Absolute, "https://example.com/x.svg")
	}

	// Top-level document (empty BaseDir): URLs stay relative — the
	// renderer's fetcher resolves them against its basePath. See
	// ParserContext.BaseDir doc comment for the contract.
	top := NewParserContext("").CollectUrlData("foo.svg")
	if top.Relative != "foo.svg" || top.Absolute != "foo.svg" {
		t.Errorf("empty-base case: got {%q, %q}, want both %q",
			top.Relative, top.Absolute, "foo.svg")
	}
}
