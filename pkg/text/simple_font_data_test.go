package text

import "testing"

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
