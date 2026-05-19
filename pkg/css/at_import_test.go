package css

import "testing"

// TestParseImportURL covers the three @import URL extraction forms.
func TestParseImportURL(t *testing.T) {
	cases := []struct {
		rule string
		want string
	}{
		{`@import url("foo.css");`, "foo.css"},
		{`@import url('foo.css');`, "foo.css"},
		{`@import url(foo.css);`, "foo.css"},
		{`@import "foo.css";`, "foo.css"},
		{`@import 'foo.css';`, "foo.css"},
		{`@import "foo.css" screen;`, "foo.css"},
		{`@import url("sub/dir/foo.css");`, "sub/dir/foo.css"},
		{`@import url(  spaced.css  );`, "spaced.css"},
		// Malformed inputs return "".
		{`@import;`, ""},
		{`@import url(;`, ""},
		{`not an import`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			got := parseImportURL(tc.rule)
			if got != tc.want {
				t.Errorf("parseImportURL(%q): got %q, want %q", tc.rule, got, tc.want)
			}
		})
	}
}

// TestResolveAtImport_FetchesViaCtxFetcher verifies the central Phase 6
// contract: the @import target is resolved against the importing
// context's BaseDir, fetched via ctx.Fetcher, and parsed with a FRESH
// ParserContext whose BaseDir is the imported sheet's URL directory.
// url() refs inside therefore resolve against the imported sheet's
// location, not the importing sheet's.
func TestResolveAtImport_FetchesViaCtxFetcher(t *testing.T) {
	// Importing sheet sits at "support/outer.css"; @import-ing
	// "imports/inner.css" should fetch "support/imports/inner.css".
	// Inner sheet's url(foo.png) must resolve to support/imports/foo.png,
	// NOT support/foo.png.
	fetched := ""
	fetcher := func(uri string) (string, error) {
		fetched = uri
		return `.x { background-image: url(foo.png); }`, nil
	}
	ctx := &ParserContext{
		BaseDir: "support",
		Fetcher: fetcher,
	}
	dst := &Stylesheet{}
	resolveAtImport(ctx, `@import "imports/inner.css";`, dst)

	if fetched != "support/imports/inner.css" {
		t.Errorf("fetched = %q, want support/imports/inner.css", fetched)
	}
	if len(dst.Rules) != 1 {
		t.Fatalf("got %d imported rules, want 1", len(dst.Rules))
	}
	got := dst.Rules[0].Declarations["background-image"]
	want := "url(support/imports/foo.png)"
	if got != want {
		t.Errorf("imported rule url() did not resolve against the imported sheet's BaseDir:\n  got  = %q\n  want = %q",
			got, want)
	}
}

// TestResolveAtImport_NilFetcherNoOp verifies graceful no-op when
// ctx.Fetcher is nil (e.g. inline style="" parses, or tests that
// don't wire a fetcher). Matches pre-Phase-6 silent-skip behavior.
func TestResolveAtImport_NilFetcherNoOp(t *testing.T) {
	ctx := &ParserContext{BaseDir: "support"}
	dst := &Stylesheet{}
	resolveAtImport(ctx, `@import "inner.css";`, dst)
	if len(dst.Rules) != 0 {
		t.Errorf("expected no rules with nil Fetcher, got %d", len(dst.Rules))
	}
}

// TestParseStylesheet_AtImportFoldsRulesInPosition verifies the
// integration path through ParseStylesheet: @import inside a sheet's
// body produces a Stylesheet whose Rules include the imported rules.
func TestParseStylesheet_AtImportFoldsRulesInPosition(t *testing.T) {
	fetcher := func(uri string) (string, error) {
		if uri == "support/imports/inner.css" {
			return `.imported { color: blue; }`, nil
		}
		return "", nil
	}
	ctx := &ParserContext{BaseDir: "support", Fetcher: fetcher}
	sheet, err := ParseStylesheet(
		`@import "imports/inner.css"; .outer { color: red; }`,
		ctx,
	)
	if err != nil {
		t.Fatalf("ParseStylesheet returned error: %v", err)
	}
	if len(sheet.Rules) != 2 {
		t.Fatalf("got %d rules, want 2 (imported + outer)", len(sheet.Rules))
	}
	// Imported rule appears in source order: @import is the first rule, so
	// imported rules come before the outer rule.
	if got, want := sheet.Rules[0].Declarations["color"], "blue"; got != want {
		t.Errorf("first rule color = %q, want %q (imported)", got, want)
	}
	if got, want := sheet.Rules[1].Declarations["color"], "red"; got != want {
		t.Errorf("second rule color = %q, want %q (outer)", got, want)
	}
}
