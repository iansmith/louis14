package css

import "testing"

// TestNormalizeAndValidateFontFamily covers the CSS Fonts 3 §3.1 /
// CSS Syntax 3 §4.3.10 / CSS 2.1 §15.3 rules for the `font-family` longhand:
//   - Numeric identifiers and digit-leading tokens are rejected.
//   - Quoted strings adjacent to bare identifiers in the same comma entry
//     invalidate the whole declaration.
//   - System-font keywords (caption, icon, menu, message-box, small-caption,
//     status-bar) are rejected anywhere they appear as the first token of an
//     unquoted entry.
//   - Whitespace runs inside an unquoted entry condense to one ASCII space.
//   - CSS-wide keywords pass through verbatim for cascade-time resolution.
//   - Unicode whitespace beyond CSS-spec whitespace (e.g. U+3000) stays part
//     of the identifier, so localized family-names like
//     `ＣＳＳテスト　フォント名` survive.
//
// Mirrors Blink's ConsumeFamilyName / font_family.cc::ParseSingleValue at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func TestNormalizeAndValidateFontFamily(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOk    bool
		wantValue string
	}{
		// Valid cases.
		{"single unquoted ident", "Arial", true, "Arial"},
		{"unquoted multi-ident", "CSSTest FamilyName", true, "CSSTest FamilyName"},
		{"multi-space condensed", "CSSTest       FamilyName, CSSTest Fallback", true, "CSSTest FamilyName, CSSTest Fallback"},
		{"quoted string", `"Helvetica Neue"`, true, `"Helvetica Neue"`},
		{"quoted + generic", `"Helvetica Neue", sans-serif`, true, `"Helvetica Neue", sans-serif`},
		{"generic family", "sans-serif", true, "sans-serif"},
		{"localized unicode w/ U+3000", "ＣＳＳテスト　フォント名, CSSTest Fallback", true, "ＣＳＳテスト　フォント名, CSSTest Fallback"},
		{"CSS-wide keyword inherit", "inherit", true, "inherit"},
		{"CSS-wide keyword initial", "initial", true, "initial"},
		{"var() defers", "var(--my-font)", true, "var(--my-font)"},

		// Invalid: digit-leading / numeric tokens (test 016).
		{"trailing numeric token", "CSSTest Weights 400", false, ""},
		{"leading digit", "400 Foo", false, ""},
		{"hyphen-digit", "-400px", false, ""},

		// Invalid: mixed quoted + unquoted (test 021).
		{"quoted then bare", `"CSSTest" Fallback`, false, ""},

		// Invalid: system-font keyword as first token (tests 022, 024).
		{"system-font keyword caption alone", "caption", false, ""},
		{"system-font keyword caption first w/ trailing tokens", "caption CSSTest Fallback", false, ""},
		{"system-font keyword caption in comma list", "caption, CSSTest Fallback", false, ""},
		{"system-font keyword icon", "icon, serif", false, ""},
		{"system-font keyword small-caption", "small-caption, serif", false, ""},
		{"system-font keyword status-bar", "status-bar, serif", false, ""},

		// Invalid: empty entries.
		{"empty value", "", false, ""},
		{"trailing comma only", ", ", false, ""},
		{"leading comma", ", serif", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeAndValidateFontFamily(tc.input)
			if ok != tc.wantOk {
				t.Fatalf("ok=%v want %v (got=%q)", ok, tc.wantOk, got)
			}
			if ok && got != tc.wantValue {
				t.Errorf("normalized=%q want %q", got, tc.wantValue)
			}
		})
	}
}

// TestFontFamilyDeclarationDropped confirms that an invalid font-family
// declaration is dropped at parseDeclarations time so it cannot overwrite an
// earlier valid declaration in the cascade.
func TestFontFamilyDeclarationDropped(t *testing.T) {
	// First a valid font-family, then an invalid one; the valid one must win.
	res := parseDeclarations(`font-family: "Helvetica"; font-family: caption, serif`)
	got, ok := res.Declarations["font-family"]
	if !ok {
		t.Fatalf("font-family not present")
	}
	if got != `"Helvetica"` {
		t.Errorf("font-family=%q want %q (the invalid second declaration must be dropped, not overwriting the valid first)", got, `"Helvetica"`)
	}
}
