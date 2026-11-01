package text

import (
	"fmt"
	"strings"
	"testing"

	"louis14/pkg/css"
	stdnet "louis14/std/net"
)

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
	payload := strings.Repeat("!", 4096) // not base64: DecodeDataURL fails
	fr := NewFontRegistry()
	_, err := fr.RegisterFontFace("LOU456-bad", "data:font/woff2;base64,"+payload, "woff2", "normal", "normal", stdnet.RequestResource(fetchTestFont))
	if err == nil {
		t.Fatal("RegisterFontFace of a garbage data: font succeeded")
	}
	if strings.Contains(err.Error(), payload[:256]) {
		t.Errorf("error embeds the data: payload (%d bytes): %.120s...", len(err.Error()), err.Error())
	}
}
