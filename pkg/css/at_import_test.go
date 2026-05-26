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

// TestParseImport_MediaQuerySuffix covers @import url-and-media-list
// splitting for both quoted-string and url() forms.
func TestParseImport_MediaQuerySuffix(t *testing.T) {
	cases := []struct {
		rule      string
		wantURL   string
		wantMedia string
	}{
		// No media-query suffix.
		{`@import "foo.css";`, "foo.css", ""},
		{`@import url("foo.css");`, "foo.css", ""},

		// Single media-query suffix.
		{`@import "foo.css" screen;`, "foo.css", "screen"},
		{`@import url("foo.css") screen;`, "foo.css", "screen"},

		// Parenthesized feature.
		{`@import "foo.css" (min-width: 1px);`, "foo.css", "(min-width: 1px)"},

		// Comma-separated media-query-list.
		{
			`@import "foo.css" (min-width: 1px), nonsense;`,
			"foo.css",
			"(min-width: 1px), nonsense",
		},
		{
			`@import "foo.css" (min-width: 1px) and (max-width: 40000in), nonsense;`,
			"foo.css",
			"(min-width: 1px) and (max-width: 40000in), nonsense",
		},

		// CSS Cascade 5 §3.1: layer / layer(name) / supports() prefixes
		// are stripped — only the media-query-list portion is returned.
		// Layer + supports semantics live in their own subsystems.
		{`@import url("foo.css") layer;`, "foo.css", ""},
		{`@import url("foo.css") layer(A);`, "foo.css", ""},
		{`@import url("foo.css") layer(A) (min-width: 1px);`, "foo.css", "(min-width: 1px)"},
		{`@import "foo.css" layer (min-width: 1px);`, "foo.css", "(min-width: 1px)"},
		{`@import "foo.css" supports(display: block);`, "foo.css", ""},
		{`@import "foo.css" supports(display: block) (min-width: 1px);`, "foo.css", "(min-width: 1px)"},
		{`@import "foo.css" layer(A) supports(display: block) (min-width: 1px);`, "foo.css", "(min-width: 1px)"},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			gotURL, gotMedia := parseImport(tc.rule)
			if gotURL != tc.wantURL {
				t.Errorf("parseImport(%q) URL: got %q, want %q", tc.rule, gotURL, tc.wantURL)
			}
			if gotMedia != tc.wantMedia {
				t.Errorf("parseImport(%q) media: got %q, want %q", tc.rule, gotMedia, tc.wantMedia)
			}
		})
	}
}

// TestResolveAtImport_MediaQueryGates_RuleApplies verifies the conditional
// @import path: when the @import media-query-list evaluates to true at the
// given viewport, the imported rule applies; when it evaluates to false,
// the rule is gated out. Mirrors the WPT css-cascade/import-conditional-001
// fixture's structure (true-branch and false-branch sheets).
func TestResolveAtImport_MediaQueryGates_RuleApplies(t *testing.T) {
	fetcher := func(uri string) (string, error) {
		return `.test { background: green; }`, nil
	}
	ctx := &ParserContext{Fetcher: fetcher}

	// True-branch: (min-width: 1px) at vw=800 → true. Rule must apply.
	{
		dst := &Stylesheet{}
		resolveAtImport(ctx, `@import "x.css" (min-width: 1px);`, dst)
		if len(dst.Rules) != 1 {
			t.Fatalf("got %d rules, want 1", len(dst.Rules))
		}
		if !RuleMediaApplies(&dst.Rules[0], 800, 600) {
			t.Errorf("rule under @import (min-width: 1px) should apply at vw=800")
		}
	}

	// False-branch: (max-width: 1px) at vw=800 → false. Rule must not apply.
	{
		dst := &Stylesheet{}
		resolveAtImport(ctx, `@import "x.css" (max-width: 1px);`, dst)
		if len(dst.Rules) != 1 {
			t.Fatalf("got %d rules, want 1", len(dst.Rules))
		}
		if RuleMediaApplies(&dst.Rules[0], 800, 600) {
			t.Errorf("rule under @import (max-width: 1px) should not apply at vw=800")
		}
	}

	// OR-list with one true branch: (min-width: 1px), nonsense → true.
	{
		dst := &Stylesheet{}
		resolveAtImport(ctx, `@import "x.css" (min-width: 1px), nonsense;`, dst)
		if len(dst.Rules) != 1 {
			t.Fatalf("got %d rules, want 1", len(dst.Rules))
		}
		if !RuleMediaApplies(&dst.Rules[0], 800, 600) {
			t.Errorf("OR-list with one true branch should apply at vw=800")
		}
	}

	// OR-list with all false branches: (max-width: 1px), nonsense → false.
	{
		dst := &Stylesheet{}
		resolveAtImport(ctx, `@import "x.css" (max-width: 1px), nonsense;`, dst)
		if len(dst.Rules) != 1 {
			t.Fatalf("got %d rules, want 1", len(dst.Rules))
		}
		if RuleMediaApplies(&dst.Rules[0], 800, 600) {
			t.Errorf("OR-list with all-false branches should not apply at vw=800")
		}
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
