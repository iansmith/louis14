package text

import (
	"os"
	"testing"
)

// TestRegisterFontFace_WOFF2 (LOU-415): a WOFF2 @font-face source decodes to
// a usable SFNT, so the face's own glyph advances are measured. The WPT
// IcTest fonts give 水 (U+6C34) a full-width (1em), half-width (0.5em) or zero
// advance. Mirrors Blink's DecodeFontWithIft kWoff2Tag branch
// (platform/fonts/web_font_decoder.cc:189 @
// 5992657f556bcd9d207a4aca187488462f027cab) → woff2::ConvertWOFF2ToTTF.
func TestRegisterFontFace_WOFF2(t *testing.T) {
	const resources = "../visualtest/testdata/wpt-css3/css-values/resources/"
	fetch := func(url string) ([]byte, error) { return os.ReadFile(resources + url) }

	for _, tc := range []struct {
		file string
		want float64 // advance of 水 at 20px
	}{
		{"IcTestFullWidth.woff2", 20},
		{"IcTestHalfWidth.woff2", 10},
		{"IcTestZeroWidth.woff2", 0},
	} {
		fr := NewFontRegistry()
		path, err := fr.RegisterFontFace("LOU415-"+tc.file, tc.file, "woff2", "normal", "normal", fetch)
		if err != nil {
			t.Errorf("%s: RegisterFontFace: %v", tc.file, err)
			continue
		}
		if got, _ := MeasureText("水", 20, path); got.Float64() != tc.want {
			t.Errorf("%s: advance of 水 at 20px = %v, want %v", tc.file, got.Float64(), tc.want)
		}
	}
}
