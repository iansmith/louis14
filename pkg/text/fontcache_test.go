package text

import (
	"testing"

	"louis14/pkg/css"
)

// TestLookupWithPolicy_ObliqueOnly verifies the CSS Fonts 4 §6.6
// `font-synthesis-style: oblique-only` matcher rule: an italic request
// must NOT substitute an oblique-declared face; it falls through to the
// family's normal face instead. No Blink analog at the pinned SHA
// (`4883d11fef4a8713e32cd582ecef6dc5457c8c3f` has only auto/none in
// FontSynthesisStyle), so this is a louis14 spec-direct implementation.
func TestLookupWithPolicy_ObliqueOnly(t *testing.T) {
	fr := NewFontRegistry()
	// Manually register two faces (skip the fetcher); the matcher only
	// reads bufferEntry, not the buffer data.
	fr.entries = []bufferEntry{
		{Family: "test", Weight: "normal", Style: "normal", Variant: 0},
		{Family: "test", Weight: "normal", Style: "oblique", Variant: 1},
	}

	normalPath := syntheticFontPath("test", 0)
	obliquePath := syntheticFontPath("test", 1)

	cases := []struct {
		name   string
		italic bool
		policy css.FontSynthesisStyleValue
		want   string
	}{
		// Non-italic request always matches the normal face.
		{"normal request always picks normal", false, css.FontSynthesisStyleAuto, normalPath},
		{"normal request with none policy picks normal", false, css.FontSynthesisStyleNone, normalPath},
		{"normal request with oblique-only policy picks normal", false, css.FontSynthesisStyleObliqueOnly, normalPath},
		// Italic request — auto / none allow italic→oblique substitution
		// (mirrors Blink at the pinned SHA; §6.6 says `none` forbids skew
		// synthesis only, not face substitution).
		{"italic + auto picks oblique", true, css.FontSynthesisStyleAuto, obliquePath},
		{"italic + none picks oblique (still substitution-allowed)", true, css.FontSynthesisStyleNone, obliquePath},
		// Italic request + oblique-only — blocks substitution; falls
		// through to the normal face.
		{"italic + oblique-only picks normal (substitution blocked)", true, css.FontSynthesisStyleObliqueOnly, normalPath},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fr.LookupWithPolicy("test", false, tc.italic, tc.policy)
			if got != tc.want {
				t.Errorf("italic=%v policy=%v: got %q, want %q",
					tc.italic, tc.policy, got, tc.want)
			}
		})
	}
}

// TestLookupWithPolicy_ItalicDeclaredWins verifies that an explicit
// `font-style: italic` @font-face always wins over an oblique-declared one
// for an italic request, regardless of policy.
func TestLookupWithPolicy_ItalicDeclaredWins(t *testing.T) {
	fr := NewFontRegistry()
	fr.entries = []bufferEntry{
		{Family: "test", Weight: "normal", Style: "italic", Variant: 1},
		{Family: "test", Weight: "normal", Style: "oblique", Variant: 2},
	}
	italicPath := syntheticFontPath("test", 1)

	for _, policy := range []css.FontSynthesisStyleValue{
		css.FontSynthesisStyleAuto,
		css.FontSynthesisStyleNone,
		css.FontSynthesisStyleObliqueOnly,
	} {
		got := fr.LookupWithPolicy("test", false, true, policy)
		if got != italicPath {
			t.Errorf("policy=%v: italic-declared face must win; got %q want %q",
				policy, got, italicPath)
		}
	}
}

// TestNormalizeStyleVerbatim verifies italic / oblique remain distinct
// while `oblique <angle>` collapses to `oblique`.
func TestNormalizeStyleVerbatim(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"normal", "normal"},
		{"italic", "italic"},
		{"oblique", "oblique"},
		{"oblique 30deg", "oblique"},
		{"ITALIC", "italic"},
		{"Oblique", "oblique"},
		{"", "normal"},
		{"garbage", "normal"},
	}
	for _, tc := range cases {
		if got := normalizeStyleVerbatim(tc.in); got != tc.want {
			t.Errorf("normalizeStyleVerbatim(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
