package text

import (
	"fmt"
	"reflect"
	"testing"

	"louis14/pkg/css"
	"mazarin/textshape"
)

func ff(tag string, v uint32) textshape.FontFeature {
	return textshape.FontFeature{Tag: [4]byte{tag[0], tag[1], tag[2], tag[3]}, Value: v}
}

// TestParseFontFeatureSettings ports the grammar of Blink's
// ConsumeFontSettings / ConsumeFontSettingsTagAndValue
// (core/css/properties/css_parsing_utils.cc:6630-6704 @
// 5992657f556bcd9d207a4aca187488462f027cab): `normal`, or a comma list of a
// 4-character string tag (U+0020-U+007E) optionally followed by an integer,
// `on` or `off` (default 1). Any malformed entry invalidates the whole
// value — Blink returns nullptr for the list, never a partial one.
func TestParseFontFeatureSettings(t *testing.T) {
	cases := []struct {
		in     string
		want   []textshape.FontFeature
		wantOK bool
	}{
		{`normal`, nil, true},
		{`"liga" 0, "smcp"`, []textshape.FontFeature{ff("liga", 0), ff("smcp", 1)}, true},
		{`'dlig' on,"onum" off`, []textshape.FontFeature{ff("dlig", 1), ff("onum", 0)}, true},
		{`"ss01" 3`, []textshape.FontFeature{ff("ss01", 3)}, true},
		{`"a,bc" 1`, []textshape.FontFeature{ff("a,bc", 1)}, true},
		{`"liga" ON`, []textshape.FontFeature{ff("liga", 1)}, true},
		{`"lig" 1`, nil, false},         // 3-character tag
		{`"ligat" 1`, nil, false},       // 5-character tag
		{`liga 1`, nil, false},          // tag is not a string
		{`"liga" 1.5`, nil, false},      // not an <integer>
		{`"liga" 1,`, nil, false},       // trailing comma
		{`"liga" 1 "smcp"`, nil, false}, // missing comma
		{`"smcp" 1, "x" 1`, nil, false}, // one bad entry drops the list
		{"\"li\tg\" 1", nil, false},     // control character in the tag
		{`"liga" yes`, nil, false},      // unknown keyword
		{`normal, "liga"`, nil, false},  // normal is not a list item
	}
	for _, c := range cases {
		got, ok := ParseFontFeatureSettings(c.in)
		if ok != c.wantOK || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseFontFeatureSettings(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// TestFontRegistry_AddUserFontFaceRules registers each @font-face rule's
// face and parses its font-feature-settings descriptor once, at
// registration, into the per-face feature list Blink's FontFace hands
// CSSFontFace::GetFontData (core/css/font_face.cc:1122-1129,
// core/css/css_font_face.cc:149-151 @
// 5992657f556bcd9d207a4aca187488462f027cab). A face without the descriptor,
// or with a malformed one (dropped by the CSS parser in Blink), has none.
func TestFontRegistry_AddUserFontFaceRules(t *testing.T) {
	src := func(file string) []css.FontFaceSrcValue {
		return []css.FontFaceSrcValue{{URL: css.CSSURIValue{Data: css.URLData{Relative: file, Absolute: file}}}}
	}
	fr := NewFontRegistry()
	fr.AddUserFontFaceRules([]css.FontFaceRule{
		{Family: "LOU424-Smcp", Src: src("Ahem.ttf"), Weight: "normal", Style: "normal", FeatureSettings: `"smcp" 1, "liga" off`},
		{Family: "LOU424-Plain", Src: src("Ahem.ttf"), Weight: "normal", Style: "normal"},
		{Family: "LOU465-Bad", Src: src("Ahem.ttf"), Weight: "normal", Style: "normal", FeatureSettings: `"smcp" 1, "toolong" 1`},
	}, fetchTestFont)

	smcp := fr.Lookup("LOU424-Smcp", false, false)
	plain := fr.Lookup("LOU424-Plain", false, false)
	bad := fr.Lookup("LOU465-Bad", false, false)
	if smcp == "" || plain == "" || bad == "" {
		t.Fatalf("faces not registered: smcp %q, plain %q, bad %q", smcp, plain, bad)
	}
	want := []textshape.FontFeature{ff("smcp", 1), ff("liga", 0)}
	if got := fr.FontFeatureSettingsForPath(smcp); !reflect.DeepEqual(got, want) {
		t.Errorf("FontFeatureSettingsForPath(smcp face) = %v, want %v", got, want)
	}
	if got := fr.FontFeatureSettingsForPath(plain); got != nil {
		t.Errorf("FontFeatureSettingsForPath(plain face) = %v, want none", got)
	}
	if got := fr.FontFeatureSettingsForPath(bad); got != nil {
		t.Errorf("FontFeatureSettingsForPath(malformed descriptor) = %v, want none", got)
	}
}

// TestRegisterFontFace_DataURLGoesThroughFetcher: the font registry hands a
// data: src to its fetcher like any other URL. Decoding data: URLs is the
// fetcher layer's job (Blink's ResourceFetcher::CreateResourceForStaticData,
// platform/loader/fetch/resource_fetcher.cc:1061-1063 @
// 5992657f556bcd9d207a4aca187488462f027cab), not the registry's.
func TestRegisterFontFace_DataURLGoesThroughFetcher(t *testing.T) {
	const src = "data:font/ttf;base64,AAAA"
	var requested []string
	fetch := func(url string) ([]byte, error) {
		requested = append(requested, url)
		return fetchTestFont("Ahem.ttf")
	}
	fr := NewFontRegistry()
	if _, err := fr.RegisterFontFace("LOU465-data", src, "", "normal", "normal", fetch); err != nil {
		t.Fatalf("RegisterFontFace: %v", err)
	}
	if fmt.Sprint(requested) != fmt.Sprint([]string{src}) {
		t.Errorf("fetcher saw %q, want the data: URL", requested)
	}
}
