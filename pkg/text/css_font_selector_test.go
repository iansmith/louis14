package text

import (
	"math"
	"os"
	"testing"

	"louis14/pkg/css"
)

// TestCSSFontSelector_IdeographicInlineSizePresence (LOU-431): CreateFont
// reports the 水 (U+6C34) advance as present whenever a declared family covers
// 水 — including a zero advance — and as absent when none does, so the ic unit
// can tell a measured 0 from the 1em fallback. Mirrors Blink's
// SimpleFontData::IdeographicAdvanceWidth, which sets its std::optional only
// when the font has the 水 glyph (platform/fonts/simple_font_data.cc:435-445 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). WPT fonts: css-values/resources.
func TestCSSFontSelector_IdeographicInlineSizePresence(t *testing.T) {
	fr := NewFontRegistry()
	for _, file := range []string{"IcTestZeroWidth.woff2", "IcTestHalfWidth.woff2"} {
		if _, err := fr.RegisterFontFace("LOU431-"+file, file, "woff2", "normal", "normal", fetchCSSValuesResource); err != nil {
			t.Fatalf("%s: RegisterFontFace: %v", file, err)
		}
	}
	sel := NewCSSFontSelector(configWithRegistry(fr))

	for _, tc := range []struct {
		family  string
		want    float64 // advance of 水 at 20px
		present bool
	}{
		{"LOU431-IcTestZeroWidth.woff2", 0, true},
		{"LOU431-IcTestHalfWidth.woff2", 10, true},
		{"LOU431-none", 0, false},
	} {
		s := css.NewStyle()
		s.Set("font-size", "20px")
		s.Set("font-family", tc.family)
		f := sel.CreateFont(s)
		if f.IdeographicInlineSize != tc.want || f.HasIdeographicInlineSize != tc.present {
			t.Errorf("%s: ideographic inline size = (%v, %v), want (%v, %v)",
				tc.family, f.IdeographicInlineSize, f.HasIdeographicInlineSize, tc.want, tc.present)
		}
	}
}

// fetchTestFont is a FontFetcher over the one font committed under the repo's
// fonts/ (Ahem.ttf); zero_advance_test.go reads the same file.
func fetchTestFont(url string) ([]byte, error) {
	return os.ReadFile("../../fonts/" + url)
}

// registerTestFace registers file as an @font-face for family with the given
// unicode-range (none = all codepoints) and returns its font path.
func registerTestFace(t *testing.T, fr *FontRegistry, family, file string, fetch FontFetcher, ranges ...css.UnicodeRange) string {
	t.Helper()
	// The format hint is unused: RegisterFontFace sniffs WOFF/WOFF2/SFNT.
	path, err := fr.RegisterFontFace(family, file, "", "normal", "normal", fetch)
	if err != nil {
		t.Fatalf("%s: RegisterFontFace(%s): %v", family, file, err)
	}
	fr.SetUnicodeRanges(path, ranges)
	return path
}

// configWithRegistry returns the default FontConfig with fr as its
// @font-face Registry.
func configWithRegistry(fr *FontRegistry) FontConfig {
	fc := DefaultFontConfig()
	fc.Registry = fr
	return fc
}

// createFontFor runs CreateFont over fr for a style with the given font-size,
// font-family and (unless empty) font-size-adjust.
func createFontFor(fr *FontRegistry, size, family, adjust string) css.Font {
	s := css.NewStyle()
	s.Set("font-size", size)
	s.Set("font-family", family)
	if adjust != "" {
		s.Set("font-size-adjust", adjust)
	}
	return NewCSSFontSelector(configWithRegistry(fr)).CreateFont(s)
}

// noSpaceThenSquare is the ex-unit-004 shape: a first family whose
// unicode-range leaves out U+0020 (ExTest-NoSpace, sxHeight = 1/8 em),
// then Ahem (x-height and cap-height 0.8em). Returns the registry and both
// faces' paths.
func noSpaceThenSquare(t *testing.T) (fr *FontRegistry, noSpace, square string) {
	fr = NewFontRegistry()
	noSpace = registerTestFace(t, fr, "LOU423-NoSpace", "ExTest-NoSpace.woff", fetchCSSValuesResource, css.UnicodeRange{From: 0x21, To: 0xFF})
	square = registerTestFace(t, fr, "LOU423-Square", "Ahem.ttf", fetchTestFont)
	return fr, noSpace, square
}

// noDigitsThenZeroWidth is the ch-unit-018 shape: Ahem with a unicode-range
// that leaves out 0-9 (but covers U+0020), then ChTestZeroWidthZero, whose
// "0" has a zero advance. Returns the registry and both faces' paths.
func noDigitsThenZeroWidth(t *testing.T) (fr *FontRegistry, noDigits, zeroW string) {
	fr = NewFontRegistry()
	noDigits = registerTestFace(t, fr, "LOU423-NoDigits", "Ahem.ttf", fetchTestFont,
		css.UnicodeRange{From: 0x00, To: 0x2F}, css.UnicodeRange{From: 0x3A, To: 0x7F})
	zeroW = registerTestFace(t, fr, "LOU423-ZeroW", "ChTestZeroWidthZero.woff", fetchCSSValuesResource)
	return fr, noDigits, zeroW
}

// TestCSSFontSelector_ExCapFromSpacePrimaryFont (LOU-423): ex and cap are
// measured from the first family whose unicode-range covers U+0020, not from
// the first family listed. Mirrors Blink's CSSToLengthConversionData::
// FontSizes::Ex / Cap → Font::PrimaryFont() (core/css/
// css_to_length_conversion_data.cc:97/:171 @
// 5992657f556bcd9d207a4aca187488462f027cab), which is
// FontFallbackList::PrimarySimpleFontDataWithSpace
// (platform/fonts/font_fallback_list.h:73 @ the same SHA).
func TestCSSFontSelector_ExCapFromSpacePrimaryFont(t *testing.T) {
	fr, noSpace, square := noSpaceThenSquare(t)
	wantX, wantCap := FontXHeightFromFont(80, square), FontCapHeightFromFont(80, square)
	if FontXHeightFromFont(80, noSpace) == wantX || FontCapHeightFromFont(80, noSpace) == wantCap {
		t.Fatalf("fixture: the two faces must differ in x-height and cap-height (noSpace %v/%v, square %v/%v)",
			FontXHeightFromFont(80, noSpace), FontCapHeightFromFont(80, noSpace), wantX, wantCap)
	}

	f := createFontFor(fr, "80px", "LOU423-NoSpace, LOU423-Square", "")
	if f.XHeight != wantX || f.CapHeight != wantCap {
		t.Errorf("(XHeight, CapHeight) = (%v, %v), want (%v, %v) from the U+0020-covering face",
			f.XHeight, f.CapHeight, wantX, wantCap)
	}
}

// TestCSSFontSelector_ChFromDigitZeroPrimaryFont (LOU-423): ch is measured
// from the first family whose unicode-range covers U+0030, while ex keeps
// coming from the U+0020 primary font — the probe is per metric. Mirrors
// Blink's FontSizes::Ch → Font::PrimaryFontWithDigitZero()
// (core/css/css_to_length_conversion_data.cc:119 @
// 5992657f556bcd9d207a4aca187488462f027cab) versus FontSizes::Ex →
// PrimaryFont() (:97).
func TestCSSFontSelector_ChFromDigitZeroPrimaryFont(t *testing.T) {
	fr, noDigits, zeroW := noDigitsThenZeroWidth(t)
	wantX := FontXHeightFromFont(80, noDigits)
	if w, _ := MeasureText("0", 80, noDigits); w.Float64() == 0 {
		t.Fatalf("fixture: the no-digits face must give \"0\" a non-zero advance")
	}
	if w, _ := MeasureText("0", 80, zeroW); w.Float64() != 0 {
		t.Fatalf("fixture: ChTestZeroWidthZero \"0\" advance = %v, want 0", w.Float64())
	}
	if FontXHeightFromFont(80, zeroW) == wantX {
		t.Fatalf("fixture: the two faces must differ in x-height (%v)", wantX)
	}

	f := createFontFor(fr, "80px", "LOU423-NoDigits, LOU423-ZeroW", "")
	if f.ZeroWidth != 0 {
		t.Errorf("ZeroWidth = %v, want 0 from the U+0030-covering face", f.ZeroWidth)
	}
	if f.XHeight != wantX {
		t.Errorf("XHeight = %v, want %v from the U+0020-covering face", f.XHeight, wantX)
	}
}

// TestCSSFontSelector_FontSizeAdjustAspectFromSpacePrimaryFont (LOU-423): the
// font-size-adjust aspect value comes from the U+0020 primary font for every
// metric — ch-width included — and the used size then measures ch from the
// U+0030 primary font. Mirrors Blink's FontBuilder::UpdateAdjustedSize, which
// takes the aspect from font->PrimaryFont() (core/css/resolver/
// font_builder.cc:397 @ 5992657f556bcd9d207a4aca187488462f027cab). Expected
// sizes follow CSS Fonts 5 §1.7: used = specified × value / aspect, with
// Ahem's x-height 0.8em and "0" advance 1em.
func TestCSSFontSelector_FontSizeAdjustAspectFromSpacePrimaryFont(t *testing.T) {
	const tolerance = 1.0 / 64

	t.Run("ex-height aspect skips a face without U+0020", func(t *testing.T) {
		fr, _, _ := noSpaceThenSquare(t)
		f := createFontFor(fr, "20px", "LOU423-NoSpace, LOU423-Square", "ex-height 0.5")
		if want := 20 * 0.5 / 0.8; math.Abs(f.AdjustedSize-want) > tolerance {
			t.Errorf("AdjustedSize = %v, want %v (aspect from the U+0020-covering Ahem face)", f.AdjustedSize, want)
		}
	})

	t.Run("ch-width aspect from the space font, ch from the digit-zero font", func(t *testing.T) {
		fr, _, _ := noDigitsThenZeroWidth(t)
		f := createFontFor(fr, "20px", "LOU423-NoDigits, LOU423-ZeroW", "ch-width 0.5")
		if want := 20 * 0.5 / 1.0; math.Abs(f.AdjustedSize-want) > tolerance {
			t.Errorf("AdjustedSize = %v, want %v (ch-width aspect from the U+0020 primary font)", f.AdjustedSize, want)
		}
		if f.ZeroWidth != 0 {
			t.Errorf("ZeroWidth = %v, want 0 from the U+0030-covering face at the used size", f.ZeroWidth)
		}
	})
}
