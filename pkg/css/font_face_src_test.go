package css

import (
	"strings"
	"testing"
)

// TestParseFontFaceRule_DataURLSrc pins that an @font-face src whose data:
// URL carries a ';' (the ";base64" parameter) and a ',' (the payload
// separator) survives descriptor splitting whole. Blink parses an
// @font-face block with the same declaration-list consumer as a style
// rule, so a ';' or ',' inside url("...") is part of the string token, not
// a separator (WPT css-values/ch-unit-019 loads its icon font this way).
func TestParseFontFaceRule_DataURLSrc(t *testing.T) {
	const dataURL = "data:font/woff2;base64,d09GMgABAAAA"
	ff := parseFontFaceRule(`@font-face {
		font-family: "icon-font";
		src: url("` + dataURL + `") format("woff2");
		font-weight: bold;
	}`)
	if ff == nil {
		t.Fatal("parseFontFaceRule returned nil")
	}
	if len(ff.Src) != 1 || ff.Src[0].URL.Data.Absolute != dataURL {
		t.Fatalf("Src = %+v, want one source with Absolute %q", ff.Src, dataURL)
	}
	if ff.Weight != "bold" {
		t.Errorf("Weight = %q, want bold (descriptor after the data: src)", ff.Weight)
	}
}

// TestParseFontFaceSrc_CommaInsideURL pins that the src list splits on
// top-level commas only: the comma inside a quoted data: URL does not end
// the first source.
func TestParseFontFaceSrc_CommaInsideURL(t *testing.T) {
	const dataURL = "data:font/woff;base64,d09GRgABAAAA"
	got := parseFontFaceSrc(`url("` + dataURL + `"), url(fallback.ttf)`)
	if len(got) != 2 || got[0].URL.Data.Absolute != dataURL || got[1].URL.Data.Absolute != "fallback.ttf" {
		t.Errorf("parseFontFaceSrc = %+v, want [%q fallback.ttf]", got, dataURL)
	}
}

// TestParseFontFaceSrc_WOFF2Format pins that a format("woff2") source is
// usable: Blink's IsSupportedFontFormat accepts woff2
// (core/css/parser/at_rule_descriptor_parser.cc:85-92 @
// 5992657f556bcd9d207a4aca187488462f027cab), and pkg/text decodes WOFF2
// (RegisterFontFace → decodeWebFont).
func TestParseFontFaceSrc_WOFF2Format(t *testing.T) {
	got := parseFontFaceSrc(`url(icon.woff2) format("woff2"), url(fallback.ttf)`)
	if len(got) == 0 || got[0].URL.Data.Absolute != "icon.woff2" || got[0].Format != "woff2" {
		t.Errorf("parseFontFaceSrc = %+v, want icon.woff2 format woff2 first", got)
	}
}

// TestParseFontFaceRule_FeatureSettingsDescriptor pins the @font-face
// font-feature-settings descriptor: its value is kept (FontFace::
// feature_settings_, core/css/font_face.cc:522-523 @
// 5992657f556bcd9d207a4aca187488462f027cab), and "normal" is no settings.
func TestParseFontFaceRule_FeatureSettingsDescriptor(t *testing.T) {
	for _, tc := range []struct{ decl, want string }{
		{`font-feature-settings: "smcp" 1;`, `"smcp" 1`},
		{`font-feature-settings: normal;`, ""},
		{``, ""},
	} {
		ff := parseFontFaceRule(`@font-face { font-family: F; src: url(f.ttf); ` + tc.decl + ` }`)
		if ff == nil {
			t.Fatalf("%q: parseFontFaceRule returned nil", tc.decl)
		}
		if ff.FeatureSettings != tc.want {
			t.Errorf("%q: FeatureSettings = %q, want %q", tc.decl, ff.FeatureSettings, tc.want)
		}
	}
}

// TestParseFontFaceRule_FeatureSettingsNormalCaseInsensitive pins that the
// descriptor's 'normal' keyword matches ASCII case-insensitively, like every
// CSS keyword (CSS Syntax §2.3), so it records no descriptor.
func TestParseFontFaceRule_FeatureSettingsNormalCaseInsensitive(t *testing.T) {
	ff := parseFontFaceRule(`@font-face { font-family: X; src: url(x.ttf); font-feature-settings: Normal; }`)
	if ff == nil {
		t.Fatal("parseFontFaceRule returned nil")
	}
	if ff.FeatureSettings != "" {
		t.Errorf("FeatureSettings = %q, want none for 'Normal'", ff.FeatureSettings)
	}
}

// TestParseFontFaceSrc_DropsUnsupportedFormats pins that a source whose
// format() Blink's IsSupportedFontFormat rejects (embedded-opentype, svg, an
// unknown keyword) is dropped at parse time, so the loader never fetches it
// (core/css/parser/at_rule_descriptor_parser.cc:85-92 and
// core/css/properties/css_parsing_utils.cc:6832-6844 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestParseFontFaceSrc_DropsUnsupportedFormats(t *testing.T) {
	got := parseFontFaceSrc(`url(a.eot) format("embedded-opentype"), url(a.svg) format("svg"), ` +
		`url(a.x) format("bogus"), url(a.woff2) format("WOFF2"), url(a.ttf) format("truetype-variations"), url(a.otf)`)
	var urls []string
	for _, v := range got {
		urls = append(urls, v.URL.Data.Absolute)
	}
	want := []string{"a.woff2", "a.ttf", "a.otf"}
	if strings.Join(urls, " ") != strings.Join(want, " ") {
		t.Errorf("parseFontFaceSrc kept %v, want %v", urls, want)
	}
}
