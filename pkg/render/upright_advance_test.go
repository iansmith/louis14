package render

import (
	"image"
	"os"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/layout"
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
	// Liberation Serif's height fallback at 20px: hhea 1825/-443 at upem
	// 2048 → 18 + 4 (pkg/text TestAscentDescentWithHacks_HeightFallback).
	const height = 22.0
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

// TestDrawVerticalTextDecoration_LengthIsUprightAdvance (LOU-424): an upright
// run's decoration line spans the run's painted inline extent — the glyphs'
// upright advances — not 1em per character. Blink's TextDecorationInfo takes
// the fragment's width (width_(width), core/paint/text_decoration_info.cc:238)
// and an upright fragment advances by HarfBuzzGetGlyphVerticalAdvance
// (platform/fonts/shaping/harfbuzz_face.cc:359-372), both @
// 5992657f556bcd9d207a4aca187488462f027cab. Serif
// (Liberation Serif, no vmtx) at 20px advances its 22px height fallback per
// glyph, so a 5-glyph line-through ends at 5 x 22, past 5em.
func TestDrawVerticalTextDecoration_LengthIsUprightAdvance(t *testing.T) {
	const size, x0, y0 = 20.0, 20, 10
	target := image.NewRGBA(image.Rect(0, 0, 80, 140))
	r := NewRendererForImage(target)
	red := css.Color{R: 255, A: 1}
	box := &layout.Box{Text: "00000", X: x0, Y: y0, Width: size, Height: 5 * size, IsVerticalText: true}
	layer := &PaintLayer{
		Box: box, FontFamily: "serif", FontSize: size, IsVerticalText: true,
		TextDecoration: css.TextDecorationLineThrough, TextDecorationColor: red, TextDecorationThickness: 2,
	}
	// Liberation Serif's height fallback at 20px: hhea 1825/-443 at upem
	// 2048 → 18 + 4, so 5 glyphs end 10px past 5em.
	const height = 22.0
	fontID := r.fontIDForLayer(layer)
	advances := uprightVerticalAdvances(layer, box.Text, r.fontPathForLayer(layer))
	r.drawVerticalTextDecoration(layer, box.Text, advances, box, fontID, float64(r.dc.GetFontMetrics(fontID).Ascent)/64)

	rowHasRed := func(y int) bool {
		for x := 0; x < target.Bounds().Dx(); x++ {
			if c := target.RGBAAt(x, y); c.R > 200 && c.G < 60 && c.B < 60 {
				return true
			}
		}
		return false
	}
	end := y0 + int(5*height)
	if !rowHasRed(end - 2) {
		t.Errorf("no line-through at y=%d, inside the run's 5 x Height extent", end-2)
	}
	if rowHasRed(end + 2) {
		t.Errorf("line-through at y=%d, past the run's 5 x Height extent (%d); it should end there, not at 5em (%d)", end+2, end, y0+5*int(size))
	}
}

// TestUprightVerticalAdvance_PaintSumMatchesLayout (LOU-466 item 5): paint
// and layout read the same upright advances and round the same way, as
// Blink paints the glyph advances of the ShapeResult layout measured
// (ShapeResult::ComputeGlyphPositions stores each glyph's unrounded advance,
// platform/fonts/shaping/shape_result.cc:1715-1764; layout snaps the run
// width once, ShapeResult::SnappedWidth) — all @
// 5992657f556bcd9d207a4aca187488462f027cab. ChTestShortZero's "0" has a
// 0.25em vmtx advance, 20.1px at 80.4px, off the 1/64px grid: five of them
// paint 100.5px, which layout's once-snapped advance also is. Snapping each
// rune's advance up to 1/64px paints 5 x 20.109375 = 100.546875.
func TestUprightVerticalAdvance_PaintSumMatchesLayout(t *testing.T) {
	const size = 80.4
	fontPath, err := text.NewFontRegistry().RegisterFontFace("LOU466-PaintSum", "ChTestShortZero.woff", "", "normal", "normal",
		func(url string) ([]byte, error) {
			return os.ReadFile("../visualtest/testdata/wpt-css3/css-values/resources/" + url)
		})
	if err != nil {
		t.Fatal(err)
	}
	layer := &PaintLayer{FontSize: size, IsVerticalText: true}
	inline, _ := text.MeasureTextVerticalFromFont("00000", size, fontPath)
	if inline.Float64() != 100.5 {
		t.Fatalf("fixture: layout advance = %v, want 100.5 (5 x 0.25em at 80.4px)", inline.Float64())
	}
	if got := measureUprightVerticalSegmentAdvance("00000", layer, fontPath, true); got != inline.Float64() {
		t.Errorf("paint advance of 5 upright \"0\"s = %v, want layout's %v", got, inline.Float64())
	}
}
