package text

import (
	"fmt"
	"strings"
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
	src := func(file string) []css.FontFaceSrcValue {
		return []css.FontFaceSrcValue{{URL: css.CSSURIValue{Data: css.URLData{Relative: file, Absolute: file}}}}
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

// TestFontRegistry_AddUserFontFaceRules_SrcFallback pins that a face whose
// first src fails to load falls back to the next src in the list, as
// Blink's CSSFontFace::FontLoaded drops a failed
// CSSFontFaceSource and loads the next (core/css/css_font_face.cc:76-91 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestFontRegistry_AddUserFontFaceRules_SrcFallback(t *testing.T) {
	ss, err := css.ParseStylesheet(`@font-face {
		font-family: "LOU456-Fallback";
		src: url(missing.woff2) format("woff2"), url(Ahem.ttf) format("truetype");
	}`, css.NewParserContext(""))
	if err != nil {
		t.Fatal(err)
	}
	fetch := func(url string) ([]byte, error) {
		if url == "missing.woff2" {
			return nil, fmt.Errorf("404 %s", url)
		}
		return fetchTestFont(url)
	}
	fr := NewFontRegistry()
	fr.AddUserFontFaceRules(ss.FontFaces, fetch)
	if got := fr.Lookup("LOU456-Fallback", false, false); got == "" {
		t.Fatal("face not registered: the second src should load when the first fails")
	}
}

// TestRegisterFontFace_DataURLErrorOmitsPayload pins that a failed data: font
// is reported without its (possibly huge) base64 payload.
func TestRegisterFontFace_DataURLErrorOmitsPayload(t *testing.T) {
	payload := strings.Repeat("!", 4096) // not base64: DecodeDataURI fails
	fr := NewFontRegistry()
	_, err := fr.RegisterFontFace("LOU456-bad", "data:font/woff2;base64,"+payload, "woff2", "normal", "normal", fetchTestFont)
	if err == nil {
		t.Fatal("RegisterFontFace of a garbage data: font succeeded")
	}
	if strings.Contains(err.Error(), payload[:256]) {
		t.Errorf("error embeds the data: payload (%d bytes): %.120s...", len(err.Error()), err.Error())
	}
}
