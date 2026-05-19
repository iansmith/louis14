package css

import "testing"

// TestParseStylesheet_RewritesURLs verifies the Phase 2 invariant: every
// `url(...)` token consumed during ParseStylesheet is resolved to its
// absolute form against ctx.BaseDir before being stored in any Rule's
// Declarations. Mirrors Blink's CollectUrlData chokepoint
// (core/css/properties/css_parsing_utils.cc:1777 @ d4ecdfed8) — by the time
// a CSSValue reaches a consumer, its URLs are already absolute.
func TestParseStylesheet_RewritesURLs(t *testing.T) {
	cases := []struct {
		name    string
		baseDir string
		css     string
		// declValue checks the resulting declaration value text for the
		// first rule's first declaration.
		property  string
		declValue string
	}{
		{
			name:      "relative filter url joined against base",
			baseDir:   "sub",
			css:       `.x { filter: url(foo.svg); }`,
			property:  "filter",
			declValue: "url(sub/foo.svg)",
		},
		{
			name:      "double-quoted url joined",
			baseDir:   "sub",
			css:       `.x { background-image: url("foo.png"); }`,
			property:  "background-image",
			declValue: `url("sub/foo.png")`,
		},
		{
			name:      "single-quoted url joined",
			baseDir:   "sub",
			css:       `.x { background-image: url('foo.png'); }`,
			property:  "background-image",
			declValue: `url('sub/foo.png')`,
		},
		{
			name:      "data url passthrough",
			baseDir:   "sub",
			css:       `.x { background-image: url(data:image/png;base64,AAA); }`,
			property:  "background-image",
			declValue: "url(data:image/png;base64,AAA)",
		},
		{
			name:      "absolute https passthrough",
			baseDir:   "sub",
			css:       `.x { background-image: url(https://example.com/foo.png); }`,
			property:  "background-image",
			declValue: "url(https://example.com/foo.png)",
		},
		{
			name:      "root-relative passthrough",
			baseDir:   "sub",
			css:       `.x { background-image: url(/foo.png); }`,
			property:  "background-image",
			declValue: "url(/foo.png)",
		},
		{
			name:      "empty base no-op",
			baseDir:   "",
			css:       `.x { background-image: url(foo.png); }`,
			property:  "background-image",
			declValue: "url(foo.png)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := NewParserContext(c.baseDir)
			ss, err := ParseStylesheet(c.css, ctx)
			if err != nil {
				t.Fatalf("ParseStylesheet returned error: %v", err)
			}
			if len(ss.Rules) != 1 {
				t.Fatalf("got %d rules, want 1", len(ss.Rules))
			}
			got, ok := ss.Rules[0].Declarations[c.property]
			if !ok {
				t.Fatalf("declaration %q not found; have keys: %v",
					c.property, declKeys(ss.Rules[0].Declarations))
			}
			if got != c.declValue {
				t.Errorf("declaration value for %q = %q, want %q",
					c.property, got, c.declValue)
			}
		})
	}
}

// TestParseStylesheet_AtMediaURL verifies url()s inside @media rule bodies
// are rewritten. The rewrite pass runs on the raw source before
// expandNesting/at-rule splitting, so nested rules inherit the rewrite.
func TestParseStylesheet_AtMediaURL(t *testing.T) {
	ctx := NewParserContext("sub")
	ss, err := ParseStylesheet(
		`@media (min-width: 100px) { .x { background-image: url(foo.png); } }`,
		ctx,
	)
	if err != nil {
		t.Fatalf("ParseStylesheet returned error: %v", err)
	}
	if len(ss.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(ss.Rules))
	}
	got, ok := ss.Rules[0].Declarations["background-image"]
	if !ok {
		t.Fatalf("background-image declaration not found; have %v",
			declKeys(ss.Rules[0].Declarations))
	}
	if got != "url(sub/foo.png)" {
		t.Errorf("background-image = %q, want %q", got, "url(sub/foo.png)")
	}
}

// TestParseStylesheet_FontFaceSrcURL verifies @font-face descriptor url()s
// are rewritten — registerWebFonts is a production caller that consumes
// stylesheet.FontFaces, so the chokepoint invariant must hold for the
// at-rule-descriptor parse path as well as ordinary declarations.
func TestParseStylesheet_FontFaceSrcURL(t *testing.T) {
	ctx := NewParserContext("sub")
	ss, err := ParseStylesheet(
		`@font-face { font-family: "X"; src: url(foo.woff2); }`,
		ctx,
	)
	if err != nil {
		t.Fatalf("ParseStylesheet returned error: %v", err)
	}
	if len(ss.FontFaces) != 1 {
		t.Fatalf("got %d FontFaces, want 1", len(ss.FontFaces))
	}
	// parseFontFaceSrc strips the url() wrapper, so the rewrite shows up as
	// the resolved Src.Data.Absolute (Phase 7.5 typed wrapper).
	src := ss.FontFaces[0].Src
	if src == nil {
		t.Fatalf("font-face Src is nil; want wrapped %q", "sub/foo.woff2")
	}
	if src.Data.Absolute != "sub/foo.woff2" {
		t.Errorf("font-face src = %q, want %q",
			src.Data.Absolute, "sub/foo.woff2")
	}
}

// TestParseStylesheet_MultipleURLs verifies multiple url() tokens in one
// declaration value all get rewritten (e.g. mask-image: url(a), url(b)).
func TestParseStylesheet_MultipleURLs(t *testing.T) {
	ctx := NewParserContext("sub")
	ss, err := ParseStylesheet(`.x { mask-image: url(a.png), url(b.png); }`, ctx)
	if err != nil {
		t.Fatalf("ParseStylesheet returned error: %v", err)
	}
	if len(ss.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(ss.Rules))
	}
	got, ok := ss.Rules[0].Declarations["mask-image"]
	if !ok {
		t.Fatalf("mask-image declaration not found; have %v",
			declKeys(ss.Rules[0].Declarations))
	}
	want := "url(sub/a.png), url(sub/b.png)"
	if got != want {
		t.Errorf("mask-image = %q, want %q", got, want)
	}
}

// TestParseInlineStyle_RewritesURLs verifies the Phase 2 invariant for the
// `style=""` parsing path — same chokepoint, applied to a single-declaration
// stream rather than a full stylesheet.
func TestParseInlineStyle_RewritesURLs(t *testing.T) {
	cases := []struct {
		name      string
		baseDir   string
		styleAttr string
		property  string
		want      string
	}{
		{
			name:      "filter url joined",
			baseDir:   "sub",
			styleAttr: "filter: url(foo.svg)",
			property:  "filter",
			want:      "url(sub/foo.svg)",
		},
		{
			name:      "background-image url joined",
			baseDir:   "sub",
			styleAttr: "background-image: url(foo.png)",
			property:  "background-image",
			want:      "url(sub/foo.png)",
		},
		{
			name:      "absolute url passthrough",
			baseDir:   "sub",
			styleAttr: `background-image: url("https://example.com/foo.png")`,
			property:  "background-image",
			want:      `url("https://example.com/foo.png")`,
		},
		{
			name:      "empty base no-op",
			baseDir:   "",
			styleAttr: "background-image: url(foo.png)",
			property:  "background-image",
			want:      "url(foo.png)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := ParseInlineStyle(c.styleAttr, NewParserContext(c.baseDir))
			got, ok := s.Get(c.property)
			if !ok {
				t.Fatalf("property %q not set; have: %v",
					c.property, declKeys(s.Properties))
			}
			if got != c.want {
				t.Errorf("%s = %q, want %q", c.property, got, c.want)
			}
		})
	}
}

// TestParseInlineStyle_NilContext verifies the nil-safe API: a nil
// ParserContext behaves like an empty BaseDir (no rewriting), while a
// real "sub" context against the same input rewrites the url(). Locks in
// the contract documented on ParserContext.
func TestParseInlineStyle_NilContext(t *testing.T) {
	const styleAttr = "background-image: url(foo.png)"

	withNil := ParseInlineStyle(styleAttr, nil)
	withSub := ParseInlineStyle(styleAttr, NewParserContext("sub"))

	gotNil, _ := withNil.Get("background-image")
	gotSub, _ := withSub.Get("background-image")

	if gotNil != "url(foo.png)" {
		t.Errorf("nil ctx: background-image = %q, want %q",
			gotNil, "url(foo.png)")
	}
	if gotSub != "url(sub/foo.png)" {
		t.Errorf("ctx{sub}: background-image = %q, want %q",
			gotSub, "url(sub/foo.png)")
	}
	if gotNil == gotSub {
		t.Errorf("nil and ctx{sub} produced identical results — test is degenerate")
	}
}

// TestGetFilter_AfterParseTimeResolution checks the Phase 2 idempotency fix:
// once ParseInlineStyle has resolved the url(), GetFilter must not re-resolve
// against s.BaseDir (which would double-prepend the base).
func TestGetFilter_AfterParseTimeResolution(t *testing.T) {
	ctx := NewParserContext("sub")
	s := ParseInlineStyle("filter: url(foo.svg)", ctx)
	// The cascade post-stamps s.BaseDir; mirror that here to expose the
	// double-resolve trap GetFilter must avoid.
	s.BaseDir = "sub"

	filters := s.GetFilter()
	if len(filters) != 1 {
		t.Fatalf("got %d filter functions, want 1", len(filters))
	}
	if filters[0].Name != "url" {
		t.Fatalf("filter name = %q, want %q", filters[0].Name, "url")
	}
	if filters[0].URL.Absolute != "sub/foo.svg" {
		t.Errorf("filter URL.Absolute = %q, want %q",
			filters[0].URL.Absolute, "sub/foo.svg")
	}
}

func declKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
