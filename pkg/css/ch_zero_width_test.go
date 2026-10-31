package css

import "testing"

// TestChUnit_MeasuredZeroAdvance (LOU-414): a font whose "0" glyph has a zero
// advance gives ch = 0, not the 0.5em fallback. Only an unmeasured ch falls
// back. Blink: SimpleFontData::ZeroInlineSize uses the zero glyph's advance
// whenever the glyph exists (platform/fonts/simple_font_data.cc:561), and
// FontSizes::Ch returns it unchanged (core/css/css_to_length_conversion_data.cc:117),
// both @ 5992657f556bcd9d207a4aca187488462f027cab. WPT: css-values/ch-unit-016.
func TestChUnit_MeasuredZeroAdvance(t *testing.T) {
	newStyle := func() *Style {
		s := NewStyle()
		s.Set("font-size", "20px")
		s.Set("width", "calc(100px + 5ch)")
		return s
	}

	measured := newStyle()
	measured.SetZeroWidth(0)
	if got, _ := measured.GetLength("width"); got != 100 {
		t.Errorf("measured zero-advance ch: calc(100px + 5ch) = %v, want 100 (ch = 0)", got)
	}

	unmeasured := newStyle()
	if got, _ := unmeasured.GetLength("width"); got != 150 {
		t.Errorf("unmeasured ch: calc(100px + 5ch) = %v, want 150 (ch falls back to 0.5em = 10px)", got)
	}
}

// TestChUnit_AnonymousStylesKeepMeasuredZeroWidth (LOU-414): anonymous boxes
// copy the parent's measured "0" advance together with HasZeroWidth, so ch on
// an anonymous box uses the parent font's measurement, not the 0.5em fallback.
func TestChUnit_AnonymousStylesKeepMeasuredZeroWidth(t *testing.T) {
	parent := NewStyle()
	parent.Set("font-size", "20px")
	parent.SetZeroWidth(0)
	for _, display := range anonymousBoxDisplays {
		s := CreateAnonymousStyleWithDisplay(parent, string(display))
		s.Set("width", "calc(100px + 5ch)")
		if got, _ := s.GetLength("width"); got != 100 {
			t.Errorf("%s: calc(100px + 5ch) = %v, want 100 (parent's measured ch = 0)", display, got)
		}
	}
}

// TestAnonymousStylesCopyStyleResolutionContext (LOU-428, LOU-431, LOU-427):
// every anonymous-box style and every clone carries its parent's full
// resolution context — all font metrics (with their measured flags), the used
// font-size, the root font-size rem resolves against and the font selector —
// as Blink builds an anonymous style from the parent style, inheriting its Font
// (StyleResolver::CreateAnonymousStyleBuilderWithDisplay,
// core/css/resolver/style_resolver.cc:3607 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestAnonymousStylesCopyStyleResolutionContext(t *testing.T) {
	parent := NewStyle()
	parent.Set("font-size", "20px")
	parent.ViewportWidth, parent.ViewportHeight = 800, 600
	parent.SetZeroWidth(11)
	parent.UsedFontSize, parent.UsedFontSizeSet = 18, true
	parent.XHeight, parent.CapHeight, parent.LhSize = 9, 14, 30
	parent.IcWidth, parent.HasIdeographicInlineSize = 19, true
	parent.BaseDir = "base/"
	parent.fontSelector = fakeFontSelector{}
	parent.remFontSize = optionalFloat{value: 12, ok: true}
	newStyles := map[string]func(*Style) *Style{"Clone": (*Style).Clone}
	for _, display := range anonymousBoxDisplays {
		newStyles["CreateAnonymousStyleWithDisplay("+string(display)+")"] = func(p *Style) *Style {
			return CreateAnonymousStyleWithDisplay(p, string(display))
		}
	}
	for name, newAnon := range newStyles {
		s := newAnon(parent)
		if s.ViewportWidth != 800 || s.ViewportHeight != 600 || s.ChWidth != 11 || !s.HasZeroWidth ||
			s.UsedFontSize != 18 || !s.UsedFontSizeSet || s.XHeight != 9 || s.CapHeight != 14 ||
			s.LhSize != 30 || s.IcWidth != 19 || !s.HasIdeographicInlineSize || s.BaseDir != "base/" ||
			s.fontSelector != parent.fontSelector || s.remFontSize != parent.remFontSize {
			t.Errorf("%s dropped resolution context: viewport %vx%v ch %v/%v used %v/%v ex %v cap %v lh %v ic %v/%v base %q selector %v rem %v",
				name, s.ViewportWidth, s.ViewportHeight, s.ChWidth, s.HasZeroWidth, s.UsedFontSize, s.UsedFontSizeSet,
				s.XHeight, s.CapHeight, s.LhSize, s.IcWidth, s.HasIdeographicInlineSize, s.BaseDir, s.fontSelector, s.remFontSize)
		}
	}
}

// TestChUnit_FontSizesChReadsZeroWidth (LOU-425): ch reads the font's
// ZeroWidth as given, with no orientation logic of its own, as
// CSSToLengthConversionData::FontSizes::Ch does (core/css/
// css_to_length_conversion_data.cc:117-126 @
// 5992657f556bcd9d207a4aca187488462f027cab); the upright 1em lives in
// SimpleFontData::ZeroInlineSize, inside the font. With no font selector (a
// louis14-only mode; Blink always has a Font) ch falls back to 0.5em, or 1em
// upright.
func TestChUnit_FontSizesChReadsZeroWidth(t *testing.T) {
	newStyle := func(writingMode, textOrientation string) *Style {
		s := NewStyle()
		s.Set("font-size", "20px")
		s.Set("writing-mode", writingMode)
		s.Set("text-orientation", textOrientation)
		s.Set("width", "calc(5ch)")
		return s
	}

	upright := newStyle("vertical-rl", "upright")
	upright.SetZeroWidth(5)
	if got, _ := upright.GetLength("width"); got != 25 {
		t.Errorf("upright, ZeroWidth 5: calc(5ch) = %v, want 25 (ch = ZeroWidth)", got)
	}
	if got, _ := newStyle("vertical-rl", "upright").GetLength("width"); got != 100 {
		t.Errorf("upright, no selector: calc(5ch) = %v, want 100 (ch falls back to 1em)", got)
	}
	if got, _ := newStyle("horizontal-tb", "mixed").GetLength("width"); got != 50 {
		t.Errorf("horizontal, no selector: calc(5ch) = %v, want 50 (ch falls back to 0.5em)", got)
	}
}
