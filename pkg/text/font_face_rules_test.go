package text

import (
	"testing"

	"louis14/pkg/css"
)

// TestFontRegistry_AddUserFontFaceRules registers each @font-face rule's
// face and keeps its font-feature-settings descriptor per face, the value
// Blink's CSSFontFace::GetFontData merges into the face's FontDescription
// (core/css/css_font_face.cc:149-151 @
// 5992657f556bcd9d207a4aca187488462f027cab). A face without the descriptor
// has none.
func TestFontRegistry_AddUserFontFaceRules(t *testing.T) {
	src := func(file string) *css.CSSURIValue {
		return &css.CSSURIValue{Data: css.URLData{Relative: file, Absolute: file}}
	}
	fr := NewFontRegistry()
	fr.AddUserFontFaceRules([]css.FontFaceRule{
		{Family: "LOU424-Smcp", Src: src("Ahem.ttf"), Weight: "normal", Style: "normal", FeatureSettings: `"smcp" 1`},
		{Family: "LOU424-Plain", Src: src("Ahem.ttf"), Weight: "normal", Style: "normal"},
	}, fetchTestFont)

	smcp := fr.Lookup("LOU424-Smcp", false, false)
	plain := fr.Lookup("LOU424-Plain", false, false)
	if smcp == "" || plain == "" {
		t.Fatalf("faces not registered: smcp %q, plain %q", smcp, plain)
	}
	if got := fr.FontFeatureSettingsForPath(smcp); got != `"smcp" 1` {
		t.Errorf("FontFeatureSettingsForPath(smcp face) = %q, want %q", got, `"smcp" 1`)
	}
	if got := fr.FontFeatureSettingsForPath(plain); got != "" {
		t.Errorf("FontFeatureSettingsForPath(plain face) = %q, want none", got)
	}
}
