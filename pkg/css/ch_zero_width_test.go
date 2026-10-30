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
