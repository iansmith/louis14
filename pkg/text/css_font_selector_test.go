package text

import (
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
	fc := DefaultFontConfig()
	fc.Registry = fr
	sel := NewCSSFontSelector(fc)

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
