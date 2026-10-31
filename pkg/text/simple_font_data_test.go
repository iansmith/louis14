package text

import (
	"testing"

	"louis14/pkg/css"
)

// TestGlyphForCharacter (LOU-425): glyphForCharacter reports whether a font's
// character-to-glyph mapping has a glyph for a character, as Blink's
// SimpleFontData::GlyphForCharacter does through HarfBuzz
// (platform/fonts/simple_font_data.cc:252 @
// 5992657f556bcd9d207a4aca187488462f027cab) — the presence bit behind
// zero_glyph_ (ch) and the IdeographicAdvanceWidth optional (ic). Modelled on
// FontTest.IdeographicFullWidthAhem / IdeographicFullWidthCjkNarrow
// (platform/fonts/font_test.cc:109/:127 @ the same SHA); ExTest.woff stands in
// for Blink's 水-less Ahem.woff, because louis14's Ahem.ttf maps 水. Expected
// presence comes from an independent go-text cmap probe of each font, not
// from louis14 output. A font that fails to open has no glyph (Blink's
// PlatformGlyphInit zeroes zero_glyph_ for a typeface with no glyphs,
// simple_font_data.cc:230-234).
func TestGlyphForCharacter(t *testing.T) {
	fr := NewFontRegistry()
	exTest := registerTestFace(t, fr, "LOU425-Glyph-ExTest", "ExTest.woff", fetchCSSValuesResource)
	zeroW := registerTestFace(t, fr, "LOU425-Glyph-ZeroW", "ChTestZeroWidthZero.woff", fetchCSSValuesResource)
	halfW := registerTestFace(t, fr, "LOU425-Glyph-IcHalf", "IcTestHalfWidth.woff2", fetchCSSValuesResource)
	serif := DefaultFontConfig().resolveBuiltinFamily("serif", false, false)

	for _, tc := range []struct {
		name    string
		path    string
		c       rune
		present bool
	}{
		{"ExTest '0'", exTest, kDigitZero, false},
		{"ChTestZeroWidthZero '0' (zero advance)", zeroW, kDigitZero, true},
		{"Ahem.ttf '0'", "../../fonts/Ahem.ttf", kDigitZero, true},
		{"ExTest 水", exTest, kCjkWater, false},
		{"serif (LiberationSerif) 水", serif, kCjkWater, false},
		{"IcTestHalfWidth 水", halfW, kCjkWater, true},
		{"unopenable font '0'", "/nonexistent/LOU425-missing.ttf", kDigitZero, false},
	} {
		if got := glyphForCharacter(tc.path, 20, tc.c) != 0; got != tc.present {
			t.Errorf("%s: glyph present = %v, want %v", tc.name, got, tc.present)
		}
	}
}

// TestSimpleFontData_IdeographicAdvanceWidth (LOU-425 review): the 水
// (U+6C34) advance of a SimpleFontData is present exactly when the font has
// the 水 glyph — including a glyph whose advance is 0 — and its inline size is
// that advance in every orientation until LOU-424 ports the vmtx upright
// branch. Mirrors SimpleFontData::IdeographicAdvanceWidth and
// IdeographicInlineSize (platform/fonts/simple_font_data.cc:437-447/:461-475
// @ 5992657f556bcd9d207a4aca187488462f027cab), as exercised by
// FontTest.IdeographicFullWidthAhem / IdeographicFullWidthCjkNarrow
// (platform/fonts/font_test.cc:109/:127 @ the same SHA). Expected advances
// are the WPT fonts' designed widths (IcTestHalfWidth: 0.5em, IcTestZeroWidth:
// 0); ExTest has no 水 glyph.
func TestSimpleFontData_IdeographicAdvanceWidth(t *testing.T) {
	fr := NewFontRegistry()
	exTest := registerTestFace(t, fr, "LOU425-Ideo-ExTest", "ExTest.woff", fetchCSSValuesResource)
	halfW := registerTestFace(t, fr, "LOU425-Ideo-IcHalf", "IcTestHalfWidth.woff2", fetchCSSValuesResource)
	zeroW := registerTestFace(t, fr, "LOU425-Ideo-IcZero", "IcTestZeroWidth.woff2", fetchCSSValuesResource)
	sel := NewCSSFontSelector(configWithRegistry(fr))

	for _, tc := range []struct {
		name    string
		path    string
		want    float64 // advance at 20px
		present bool
	}{
		{"IcTestHalfWidth", halfW, 10, true},
		{"IcTestZeroWidth (zero-advance 水)", zeroW, 0, true},
		{"ExTest (no 水)", exTest, 0, false},
	} {
		for _, o := range []css.FontOrientation{css.FontOrientationHorizontal, css.FontOrientationVerticalMixed, css.FontOrientationVerticalUpright} {
			d := sel.simpleFontData(tc.path, 20, o)
			if w, ok := d.IdeographicAdvanceWidth(); w != tc.want || ok != tc.present {
				t.Errorf("%s, orientation %v: IdeographicAdvanceWidth = (%v, %v), want (%v, %v)", tc.name, o, w, ok, tc.want, tc.present)
			}
			if w, ok := d.IdeographicInlineSize(); w != tc.want || ok != tc.present {
				t.Errorf("%s, orientation %v: IdeographicInlineSize = (%v, %v), want (%v, %v)", tc.name, o, w, ok, tc.want, tc.present)
			}
		}
	}
}
