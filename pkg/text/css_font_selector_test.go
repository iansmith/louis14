package text

import (
	"math"
	"os"
	"testing"

	"louis14/pkg/css"
)

// TestCSSFontSelector_IdeographicInlineSizePresence (LOU-431, LOU-425):
// CreateFont reports the 水 (U+6C34) advance as present when the U+6C34
// primary font has the 水 glyph — including a zero advance — and as absent
// when it does not, so the ic unit can tell a measured 0 from the 1em
// fallback. The primary font is Blink's Font::PrimaryFontWithCjkWater
// (platform/fonts/font.h:243 @ 5992657f556bcd9d207a4aca187488462f027cab):
// the family walk of FontFallbackList::DeterminePrimarySimpleFontDataCore
// (font_fallback_list.cc:87-140 @ the same SHA), which stops at a builtin
// family whatever its glyphs. Presence mirrors
// SimpleFontData::IdeographicAdvanceWidth, which sets its std::optional only
// when GlyphForCharacter(kCjkWater) is non-zero (simple_font_data.cc:437 @ the
// same SHA). WPT fonts: css-values/resources.
func TestCSSFontSelector_IdeographicInlineSizePresence(t *testing.T) {
	fr := NewFontRegistry()
	for _, file := range []string{"IcTestZeroWidth.woff2", "IcTestHalfWidth.woff2"} {
		if _, err := fr.RegisterFontFace("LOU431-"+file, file, "woff2", "normal", "normal", fetchCSSValuesResource); err != nil {
			t.Fatalf("%s: RegisterFontFace: %v", file, err)
		}
	}
	// Latin faces with no unicode-range (they cover 水 by range) and no 水
	// glyph.
	registerTestFace(t, fr, "LOU425-Ic-ExTest", "ExTest.woff", fetchCSSValuesResource)
	registerTestFace(t, fr, "LOU425-Ic-ZeroW", "ChTestZeroWidthZero.woff", fetchCSSValuesResource)
	sel := NewCSSFontSelector(configWithRegistry(fr))

	for _, tc := range []struct {
		family  string
		want    float64 // advance of 水 at 20px
		present bool
	}{
		{"LOU431-IcTestZeroWidth.woff2", 0, true},
		{"LOU431-IcTestHalfWidth.woff2", 10, true},
		{"LOU431-none", 0, false},
		{"LOU425-Ic-ExTest", 0, false},
		{"LOU425-Ic-ZeroW", 0, false},
		// The walk stops at the builtin serif (LiberationSerif, no 水).
		{"serif, LOU431-IcTestHalfWidth.woff2", 0, false},
	} {
		f := createFontWith(sel, "20px", tc.family)
		if f.IdeographicInlineSize != tc.want || f.HasIdeographicInlineSize != tc.present {
			t.Errorf("%s: ideographic inline size = (%v, %v), want (%v, %v)",
				tc.family, f.IdeographicInlineSize, f.HasIdeographicInlineSize, tc.want, tc.present)
		}
	}
}

// fetchTestFont is a FontFetcher over the repo's fonts/: the committed
// Ahem.ttf (zero_advance_test.go reads the same file) and the gitignored
// builtin set the tests already resolve through DefaultFontConfig.
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
	var props []string
	if adjust != "" {
		props = []string{"font-size-adjust", adjust}
	}
	return createFontWith(NewCSSFontSelector(configWithRegistry(fr)), size, family, props...)
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

// createFontWith runs sel.CreateFont for a style with the given font-size,
// font-family and extra properties (name/value pairs).
func createFontWith(sel *CSSFontSelector, size, family string, props ...string) css.Font {
	s := css.NewStyle()
	s.Set("font-size", size)
	s.Set("font-family", family)
	for i := 0; i+1 < len(props); i += 2 {
		s.Set(props[i], props[i+1])
	}
	return sel.CreateFont(s)
}

// TestSimpleFontData_ZeroInlineSizeNoZeroGlyphFallback (LOU-425): when the
// U+0030 primary font has no "0" glyph, ZeroWidth is 0.5 x the font's size,
// not the advance of the .notdef glyph shaping substitutes. Mirrors Blink's
// SimpleFontData::ZeroInlineSize no-glyph branch, size * 0.5 with
// CSSChUnitSpecCompliantFallback stable (platform/fonts/
// simple_font_data.cc:587 @ 5992657f556bcd9d207a4aca187488462f027cab), where
// size is the font's own (font-size-adjust used) size, font_.getSize(). No
// Blink unit test covers this branch. ExTest.woff and IcTestZeroWidth.woff2
// have no "0" (go-text cmap probe); ExTest's .notdef advance is 1em.
func TestSimpleFontData_ZeroInlineSizeNoZeroGlyphFallback(t *testing.T) {
	fr := NewFontRegistry()
	registerTestFace(t, fr, "LOU425-Zero-ExTest", "ExTest.woff", fetchCSSValuesResource)
	registerTestFace(t, fr, "LOU425-Zero-IcZero", "IcTestZeroWidth.woff2", fetchCSSValuesResource)

	t.Run("no '0' glyph gives 0.5em", func(t *testing.T) {
		sel := NewCSSFontSelector(configWithRegistry(fr))
		for _, family := range []string{"LOU425-Zero-ExTest", "LOU425-Zero-IcZero"} {
			if f := createFontWith(sel, "20px", family); f.ZeroWidth != 10 {
				t.Errorf("%s at 20px: ZeroWidth = %v, want 10 (0.5em)", family, f.ZeroWidth)
			}
		}
	})

	t.Run("fallback uses the exact size, not the rounded cache key", func(t *testing.T) {
		sel := NewCSSFontSelector(configWithRegistry(fr))
		for _, tc := range []struct {
			size string
			want float64
		}{{"10px", 5}, {"10.4px", 5.2}} {
			if f := createFontWith(sel, tc.size, "LOU425-Zero-ExTest"); f.ZeroWidth != tc.want {
				t.Errorf("ExTest at %s: ZeroWidth = %v, want %v", tc.size, f.ZeroWidth, tc.want)
			}
		}
	})

	t.Run("fallback base is the font-size-adjust used size", func(t *testing.T) {
		sel := NewCSSFontSelector(configWithRegistry(fr))
		f := createFontWith(sel, "20px", "LOU425-Zero-ExTest", "font-size-adjust", "ex-height 0.5")
		if f.AdjustedSize == 20 {
			t.Fatalf("fixture: font-size-adjust must change the used size (AdjustedSize = 20)")
		}
		if want := 0.5 * f.AdjustedSize; f.ZeroWidth != want {
			t.Errorf("ZeroWidth = %v, want %v (0.5 x AdjustedSize %v)", f.ZeroWidth, want, f.AdjustedSize)
		}
	})
}

// TestSimpleFontData_ZeroInlineSizeOrientation (LOU-425): ZeroWidth depends on
// the font's orientation, derived from writing-mode and text-orientation as
// ComputedStyleBuilder::ComputeFontOrientation does (core/style/
// computed_style.cc:3256-3272 @ 5992657f556bcd9d207a4aca187488462f027cab).
// Without a "0" glyph, SimpleFontData::ZeroInlineSize gives size when
// IsVerticalNonCjkUpright and size * 0.5 otherwise (platform/fonts/
// simple_font_data.cc:579-598 @ the same SHA). With the glyph and upright
// orientation it returns the vertical advance OpenTypeVerticalData::
// AdvanceHeight (:572-576); neither ChTestZeroWidthZero nor Liberation Serif
// has vmtx, so that is the FontMetrics::Height() fallback
// (open_type_vertical_data.cc:298-299 @ the same SHA).
func TestSimpleFontData_ZeroInlineSizeOrientation(t *testing.T) {
	fr := NewFontRegistry()
	registerTestFace(t, fr, "LOU425-Orient-ExTest", "ExTest.woff", fetchCSSValuesResource)
	zeroW := registerTestFace(t, fr, "LOU425-Orient-ZeroW", "ChTestZeroWidthZero.woff", fetchCSSValuesResource)
	sel := NewCSSFontSelector(configWithRegistry(fr))
	serif := DefaultFontConfig().resolveBuiltinFamily("serif", false, false)
	if fontMetricsHeight(20, serif) == 20 {
		t.Fatal("fixture: Liberation Serif's Height at 20px must differ from 1em")
	}

	for _, tc := range []struct {
		family, writingMode, textOrientation string
		want                                 float64 // ZeroWidth at 20px
	}{
		// ExTest: no "0" glyph.
		{"LOU425-Orient-ExTest", "horizontal-tb", "", 10},
		{"LOU425-Orient-ExTest", "vertical-rl", "mixed", 10},
		{"LOU425-Orient-ExTest", "vertical-rl", "sideways", 10},
		{"LOU425-Orient-ExTest", "sideways-rl", "upright", 10},
		{"LOU425-Orient-ExTest", "vertical-lr", "upright", 20},
		// ChTestZeroWidthZero: a "0" glyph with a zero advance.
		{"LOU425-Orient-ZeroW", "horizontal-tb", "", 0},
		{"LOU425-Orient-ZeroW", "vertical-rl", "upright", fontMetricsHeight(20, zeroW)},
		// Builtin serif (LiberationSerif, "0" present).
		{"serif", "vertical-rl", "upright", fontMetricsHeight(20, serif)},
	} {
		props := []string{"writing-mode", tc.writingMode}
		if tc.textOrientation != "" {
			props = append(props, "text-orientation", tc.textOrientation)
		}
		if f := createFontWith(sel, "20px", tc.family, props...); f.ZeroWidth != tc.want {
			t.Errorf("%s, %s / %q: ZeroWidth = %v, want %v",
				tc.family, tc.writingMode, tc.textOrientation, f.ZeroWidth, tc.want)
		}
	}
}

// TestSimpleFontData_ZeroInlineSizeUprightVmtx (LOU-424): upright, ZeroWidth
// is the "0" glyph's vmtx advance scaled by size / unitsPerEm at the font's
// exact size. ChTestShortZero.woff (WPT css-values resource, the font of
// ch-unit-017) has upem 1024 and a "0" vmtx advance of 256, so 0.25em: 20 at
// 80px and 20.1 at 80.4px (not the rounded cache key's 20). Mirrors
// SimpleFontData::ZeroInlineSize (platform/fonts/simple_font_data.cc:572-576)
// and HarfBuzzFontData's size_per_unit_ = platform_data.size() / upem
// (platform/fonts/shaping/harfbuzz_font_data.h:77) @
// 5992657f556bcd9d207a4aca187488462f027cab.
func TestSimpleFontData_ZeroInlineSizeUprightVmtx(t *testing.T) {
	fr := NewFontRegistry()
	registerTestFace(t, fr, "LOU424-ShortZero", "ChTestShortZero.woff", fetchCSSValuesResource)
	sel := NewCSSFontSelector(configWithRegistry(fr))
	for _, tc := range []struct {
		size string
		want float64
	}{{"80px", 20}, {"80.4px", 80.4 / 4}} {
		f := createFontWith(sel, tc.size, "LOU424-ShortZero", "writing-mode", "vertical-rl", "text-orientation", "upright")
		if f.ZeroWidth != tc.want {
			t.Errorf("at %s: upright ZeroWidth = %v, want %v (0.25em)", tc.size, f.ZeroWidth, tc.want)
		}
	}
}

// TestCSSFontSelector_FontSizeAdjustChWidthNoZeroGlyph (LOU-425): the
// font-size-adjust ch-width aspect of a primary font with no "0" glyph is
// 0.5, from the ZeroInlineSize fallback, not the .notdef advance. Blink's
// AspectValue kChWidth reads FontMetrics::ZeroWidth whenever HasZeroWidth
// (core/css/font_size_functions.cc:143-145 @
// 5992657f556bcd9d207a4aca187488462f027cab), and PlatformGlyphInit always sets
// it to ZeroInlineSize (platform/fonts/simple_font_data.cc:245). Used size per
// CSS Fonts 5 §1.7: 20 x 0.25 / 0.5 = 10.
func TestCSSFontSelector_FontSizeAdjustChWidthNoZeroGlyph(t *testing.T) {
	fr := NewFontRegistry()
	registerTestFace(t, fr, "LOU425-Adjust-ExTest", "ExTest.woff", fetchCSSValuesResource)
	f := createFontFor(fr, "20px", "LOU425-Adjust-ExTest", "ch-width 0.25")
	if f.AdjustedSize != 10 || f.ZeroWidth != 5 {
		t.Errorf("(AdjustedSize, ZeroWidth) = (%v, %v), want (10, 5)", f.AdjustedSize, f.ZeroWidth)
	}
}
