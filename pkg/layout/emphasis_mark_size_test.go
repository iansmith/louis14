package layout

import "testing"

// TestEmphasisMarkFontSize pins the emphasis mark's font size: Blink scales
// the mark glyph's SimpleFontData to lroundf(computed size * 0.5), with no
// minimum (SimpleFontData::EmphasisMarkFontData → CreateScaledFontData,
// kEmphasisMarkFontSizeMultiplier, platform/fonts/simple_font_data.cc:301-317
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b; reached from
// ShapeResult::EmphasisMarkGlyphData, shaping/shape_result.cc:957-969, same
// SHA). lroundf rounds halves away from zero.
func TestEmphasisMarkFontSize(t *testing.T) {
	for _, tc := range []struct{ base, want float64 }{
		{16, 8},
		{25, 13}, // 12.5 rounds up
		{17, 9},  // 8.5 rounds up
		{5, 3},   // 2.5 rounds up; no 4px floor
		{4, 2},
	} {
		if got := EmphasisMarkFontSize(tc.base); got != tc.want {
			t.Errorf("EmphasisMarkFontSize(%v) = %v, want lroundf(%v * 0.5) = %v", tc.base, got, tc.base, tc.want)
		}
	}
}
