package text

import "testing"

// TestMeasureTextVerticalFromFont_AdvanceHeight (LOU-424): the inline advance
// of upright text is the sum of each glyph's vertical advance — Blink shapes
// upright text top-to-bottom with HarfBuzzGetGlyphVerticalAdvance, which
// returns OpenTypeVerticalData::AdvanceHeight (the vmtx advance, or the
// FontMetrics::Height() fallback when the font has no vmtx) — the same value
// ch reads (platform/fonts/shaping/harfbuzz_face.cc:359-372 @
// 5992657f556bcd9d207a4aca187488462f027cab). So N ch and N upright "0"s agree.
// ChTestShortZero: "0" has a 0.25em vmtx advance. Liberation Serif has no
// vmtx and a Height that differs from 1em. Ahem has no vmtx and a Height of
// exactly 1em at integer sizes.
func TestMeasureTextVerticalFromFont_AdvanceHeight(t *testing.T) {
	fr := NewFontRegistry()
	shortZero := registerTestFace(t, fr, "LOU424-MTV-ShortZero", "ChTestShortZero.woff", fetchCSSValuesResource)
	serif := DefaultFontConfig().resolveBuiltinFamily("serif", false, false)
	serifHeight := fontMetricsHeight(20, serif)
	if serifHeight == 20 {
		t.Fatal("fixture: Liberation Serif's Height at 20px must differ from 1em")
	}
	for _, tc := range []struct {
		name, text, path string
		size, want       float64
	}{
		{"ChTestShortZero vmtx", "00000", shortZero, 80, 100},
		{"Liberation Serif, no vmtx", "00000", serif, 20, 5 * serifHeight},
		{"Ahem, no vmtx", "XXXXX", "../../fonts/Ahem.ttf", 20, 100},
	} {
		inline, _ := MeasureTextVerticalFromFont(tc.text, tc.size, tc.path)
		if got := inline.Float64(); got != tc.want {
			t.Errorf("%s: inline advance of %q at %vpx = %v, want %v", tc.name, tc.text, tc.size, got, tc.want)
		}
	}
}
