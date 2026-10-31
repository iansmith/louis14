package render

import (
	"image"
	"testing"

	"louis14/pkg/text"
)

// TestUprightVerticalRuneAdvance_MatchesLayout (LOU-424): the upright paint
// advance — the one the IsVerticalText paint loop steps y by, and the one
// measureSegmentAdvance / measureSegmentVisualWidth size ::selection
// highlights with (LOU-347) — is layout's MeasureTextVerticalFromFont, as
// Blink paints the shape result that layout measured (HarfBuzzGetGlyphVertical
// Advance, platform/fonts/shaping/harfbuzz_face.cc:359-372 @
// 5992657f556bcd9d207a4aca187488462f027cab). The default serif (Liberation
// Serif) has no vmtx, so each glyph advances by its FontMetrics Height, not
// 1em. Letter-spacing adds one gap per rune, minus the trailing one for the
// visual width.
func TestUprightVerticalRuneAdvance_MatchesLayout(t *testing.T) {
	const size = 20.0
	r := NewRendererForImage(image.NewRGBA(image.Rect(0, 0, 10, 10)))
	layer := &PaintLayer{FontFamily: "serif", FontSize: size, IsVerticalText: true}
	fontPath := r.fonts.FontPathForFamilyWithSynthesis(layer.FontFamily, false, false, false, false,
		layer.FontSynthesisWeight, layer.FontSynthesisStyle)
	height := text.FontAscentFromFont(size, fontPath, nil) + text.FontDescentFromFont(size, fontPath, nil)
	if height == size {
		t.Fatalf("fixture: the serif Height at %vpx must differ from 1em", size)
	}
	want := 5 * height
	if inline, _ := text.MeasureTextVerticalFromFont("00000", size, fontPath); inline.Float64() != want {
		t.Errorf("layout MeasureTextVerticalFromFont = %v, want %v", inline.Float64(), want)
	}
	fontID := r.fontIDForLayer(layer)

	if got := r.measureSegmentAdvance("00000", fontID, layer); got != want {
		t.Errorf("measureSegmentAdvance = %v, want %v", got, want)
	}
	layer.LetterSpacing = 2
	if got := r.measureSegmentAdvance("00000", fontID, layer); got != want+10 {
		t.Errorf("letter-spacing 2: measureSegmentAdvance = %v, want %v", got, want+10)
	}
	if got := r.measureSegmentVisualWidth("00000", fontID, layer); got != want+8 {
		t.Errorf("letter-spacing 2: measureSegmentVisualWidth = %v, want %v", got, want+8)
	}
}
