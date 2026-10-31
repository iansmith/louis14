package text

import (
	"errors"
	"testing"

	"louis14/pkg/css"
)

// TestFontConfig_DeterminePrimarySimpleFontData (LOU-423) pins the family-list
// walk shared by FontPathForFamilyWithSynthesis, FontPathForStrut and
// FontPathForCovering: a registered family is skipped when none of its faces'
// unicode-range covers the lookup character; a declared family whose fetch
// failed suppresses bold/italic synthesis in the serif fallback; an unknown
// family falls through to the serif tail. Mirrors Blink's
// FontFallbackList::DeterminePrimarySimpleFontDataCore, which skips a
// SegmentedFontData that does not ContainsCharacter(lookup_character)
// (platform/fonts/font_fallback_list.cc:87-140, the skip at :109 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestFontConfig_DeterminePrimarySimpleFontData(t *testing.T) {
	fr := NewFontRegistry()
	square := registerTestFace(t, fr, "LOU423-Square", "Ahem.ttf", fetchTestFont, css.UnicodeRange{From: 0x21, To: 0x7F})
	ex := registerTestFace(t, fr, "LOU423-Ex", "ExTest.woff", fetchCSSValuesResource)
	if _, err := fr.RegisterFontFace("LOU423-Failed", "missing.woff", "woff", "normal", "normal", func(string) ([]byte, error) {
		return nil, errors.New("no such font")
	}); err == nil {
		t.Fatal("fixture: the failing fetch must fail")
	}
	fc := configWithRegistry(fr)
	noRegistry := DefaultFontConfig()
	serif := noRegistry.FontPathForFamily("serif", false, false, false, false)
	boldSerif := noRegistry.FontPathForFamily("serif", true, false, false, false)
	if serif == boldSerif {
		t.Fatalf("fixture: bold and regular serif must differ (%s)", serif)
	}

	const auto = css.FontSynthesisStyleAuto
	type walker struct {
		name string
		walk func(fc FontConfig, family string, bold bool) string
	}
	covering := func(name string, cp rune) walker {
		return walker{"FontPathForCovering(" + name + ")", func(fc FontConfig, family string, bold bool) string {
			return fc.FontPathForCovering(family, bold, false, false, false, false, auto, cp)
		}}
	}
	var (
		withSynthesis = walker{"FontPathForFamilyWithSynthesis", func(fc FontConfig, family string, bold bool) string {
			return fc.FontPathForFamilyWithSynthesis(family, bold, false, false, false, false, auto)
		}}
		strut = walker{"FontPathForStrut", func(fc FontConfig, family string, bold bool) string {
			return fc.FontPathForStrut(family, bold, false, false, false, false, auto)
		}}
		covering0     = covering("0", 0)
		coveringA     = covering("'A'", 'A')
		coveringSpace = covering("' '", ' ')
		all           = []walker{withSynthesis, strut, covering0, coveringA, coveringSpace}
	)

	for _, tc := range []struct {
		desc    string
		fc      FontConfig
		family  string
		bold    bool
		walkers []walker
		want    string
	}{
		{"no gate, or a covered character, takes the first family", fc, "LOU423-Square, LOU423-Ex", false,
			[]walker{withSynthesis, covering0, coveringA}, square},
		{"U+0020 skips the family whose unicode-range excludes it", fc, "LOU423-Square, LOU423-Ex", false,
			[]walker{strut, coveringSpace}, ex},
		{"a declared family that failed to load suppresses synthetic bold", fc, "LOU423-Failed", true, all, serif},
		{"an unknown family falls through to serif", fc, "LOU423-Unknown", false, all, serif},
		{"without a Registry every walker agrees", noRegistry, "LOU423-Square, serif", false, all, serif},
	} {
		for _, w := range tc.walkers {
			if got := w.walk(tc.fc, tc.family, tc.bold); got != tc.want {
				t.Errorf("%s: %s(%q, bold=%v) = %s, want %s", tc.desc, w.name, tc.family, tc.bold, got, tc.want)
			}
		}
	}
}
