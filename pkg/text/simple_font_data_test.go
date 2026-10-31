package text

import (
	"os"
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

// TestSimpleFontData_IdeographicAdvanceWidth (LOU-425 review, LOU-424): the
// 水 (U+6C34) advance of a SimpleFontData is present exactly when the font has
// the 水 glyph — including a glyph whose advance is 0. Its inline size is that
// advance unless the orientation is upright, where it is the vertical advance
// IdeographicAdvanceHeight: for these fonts, which have no vmtx, the
// FontMetrics::Height() fallback. Mirrors SimpleFontData::
// IdeographicAdvanceWidth, IdeographicAdvanceHeight and IdeographicInlineSize
// (platform/fonts/simple_font_data.cc:437-447/:449-459/:461-475 @
// 5992657f556bcd9d207a4aca187488462f027cab), as exercised by
// FontTest.IdeographicFullWidthAhem / IdeographicFullWidthCjkNarrow
// (platform/fonts/font_test.cc:109/:127 @ the same SHA). Expected widths are
// the WPT fonts' designed widths (IcTestHalfWidth: 0.5em, IcTestZeroWidth: 0);
// ExTest has no 水 glyph.
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
			wantInline := tc.want
			if o == css.FontOrientationVerticalUpright && tc.present {
				wantInline = fontMetricsHeight(20, tc.path)
			}
			if w, ok := d.IdeographicInlineSize(); w != wantInline || ok != tc.present {
				t.Errorf("%s, orientation %v: IdeographicInlineSize = (%v, %v), want (%v, %v)", tc.name, o, w, ok, wantInline, tc.present)
			}
		}
	}
}

// fontMetricsHeight is Blink's FontMetrics::Height() — int_ascent_ +
// int_descent_ (platform/fonts/font_metrics.h:129 @
// 5992657f556bcd9d207a4aca187488462f027cab) — of the font at fontPath with no
// metric overrides: the vertical advance OpenTypeVerticalData falls back to
// when the font has no vmtx (open_type_vertical_data.cc:298-299 @ the same
// SHA). The relational form of FontTest.IdeographicFullWidthUprightCjkNoVmtx's
// expectation, GetFontMetrics().Height().
func fontMetricsHeight(size float64, fontPath string) float64 {
	return FontAscentFromFont(size, fontPath, nil) + FontDescentFromFont(size, fontPath, nil)
}

// fetchAdobeTestFont is a FontFetcher over pkg/text/testdata/adobe-fonts,
// Blink's web_tests/external/wpt/fonts/adobe-fonts (BlinkWebTestsFontsTestDataPath,
// platform/testing/unit_test_helpers.cc:134-137 @
// 5992657f556bcd9d207a4aca187488462f027cab), vendored with its OFL LICENSE.
func fetchAdobeTestFont(url string) ([]byte, error) {
	return os.ReadFile("testdata/adobe-fonts/" + url)
}

// TestFontTest_IdeographicFullWidthUprightAhem (LOU-424): port of Blink's
// FontTest.IdeographicFullWidthUprightAhem (platform/fonts/font_test.cc:
// 141-147 @ 5992657f556bcd9d207a4aca187488462f027cab): a font with no 水
// glyph has no ideographic inline size. Blink's Ahem.woff has no 水; louis14's
// Ahem.ttf does, so ExTest.woff (no 水) stands in. Blink's test builds the
// font with the default orientation; the upright row also checks that the
// vertical branch keeps the presence gate.
func TestFontTest_IdeographicFullWidthUprightAhem(t *testing.T) {
	fr := NewFontRegistry()
	p := registerTestFace(t, fr, "LOU424-FT-NoWater", "ExTest.woff", fetchCSSValuesResource)
	sel := NewCSSFontSelector(configWithRegistry(fr))
	for _, o := range []css.FontOrientation{css.FontOrientationHorizontal, css.FontOrientationVerticalUpright} {
		if w, ok := sel.simpleFontData(p, 16, o).IdeographicInlineSize(); ok {
			t.Errorf("orientation %v: IdeographicInlineSize = (%v, true), want no value", o, w)
		}
	}
}

// TestFontTest_IdeographicFullWidthUprightCjkNoVmtx (LOU-424): port of Blink's
// FontTest.IdeographicFullWidthUprightCjkNoVmtx (platform/fonts/font_test.cc:
// 150-161 @ 5992657f556bcd9d207a4aca187488462f027cab): a font with the 水
// glyph and no vmtx synthesizes the upright advance as
// GetFontMetrics().Height(). Blink's mplus-1p-regular.woff is not in the
// repo; IcTestHalfWidth.woff2 (水 present, no vhea/vmtx, 0.5em wide) stands
// in, since its width differs from its Height.
func TestFontTest_IdeographicFullWidthUprightCjkNoVmtx(t *testing.T) {
	fr := NewFontRegistry()
	p := registerTestFace(t, fr, "LOU424-FT-NoVmtx", "IcTestHalfWidth.woff2", fetchCSSValuesResource)
	sel := NewCSSFontSelector(configWithRegistry(fr))
	height := fontMetricsHeight(16, p)
	if w, _ := sel.simpleFontData(p, 16, css.FontOrientationHorizontal).IdeographicAdvanceWidth(); w == height {
		t.Fatalf("fixture: IcTestHalfWidth width %v equals its Height; the test could not tell them apart", w)
	}
	w, ok := sel.simpleFontData(p, 16, css.FontOrientationVerticalUpright).IdeographicInlineSize()
	if !ok {
		t.Fatal("upright IdeographicInlineSize has no value, want one")
	}
	if w != height {
		t.Errorf("upright IdeographicInlineSize = %v, want FontMetrics Height %v", w, height)
	}
}

// TestFontTest_IdeographicFullWidthUprightCjkVmtx (LOU-424): port of Blink's
// FontTest.IdeographicFullWidthUprightCjkVmtx (platform/fonts/font_test.cc:
// 163-173 @ 5992657f556bcd9d207a4aca187488462f027cab), with the same font,
// adobe-fonts/CSSHWOrientationTest.otf: upright, the ideographic inline size
// is 水's vmtx advance (1000 of 1000 units, so 16 at 16px), not its
// horizontal half-width advance (500 units, 8 — Blink's
// IdeographicFullWidthCjkNarrow, :127-138).
func TestFontTest_IdeographicFullWidthUprightCjkVmtx(t *testing.T) {
	fr := NewFontRegistry()
	p := registerTestFace(t, fr, "CSSHWOrientationTest", "CSSHWOrientationTest.otf", fetchAdobeTestFont)
	sel := NewCSSFontSelector(configWithRegistry(fr))
	if w, ok := sel.simpleFontData(p, 16, css.FontOrientationHorizontal).IdeographicInlineSize(); !ok || w != 8 {
		t.Fatalf("fixture: horizontal IdeographicInlineSize = (%v, %v), want (8, true)", w, ok)
	}
	w, ok := sel.simpleFontData(p, 16, css.FontOrientationVerticalUpright).IdeographicInlineSize()
	if !ok {
		t.Fatal("upright IdeographicInlineSize has no value, want one")
	}
	if w != 16 {
		t.Errorf("upright IdeographicInlineSize = %v, want 16", w)
	}
}
