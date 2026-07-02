package text

import "testing"

// TestZeroAdvanceTextMeasuresZero is the red test for LOU-358 category B's
// residual: measureWidth carried a `w < 1 → 1` clamp left over from the
// pre-HarfBuzz gg-based measurement path (a make-it-visible guard on rounded
// integer widths). Blink has no such clamp — a ShapeResult over zero-advance
// content (U+200B ZERO WIDTH SPACE is default-ignorable) has width 0
// (ShapeResult::Width; the snap is LayoutUnit::FromFloatCeil in
// shape_result.h, which keeps exact 0 at 0).
//
// Consequence of the clamp: a floated ::before whose content is a lone ZWSP
// plus its ::marker measured 1px wider than the same marker text in inline
// flow, offsetting everything after the float by 1px (WPT css-pseudo/
// marker-font-variant-numeric-default/-normal, .before/.after columns).
//
// Ahem defines U+200B as a zero-advance glyph by design, and Ahem.ttf is the
// one font committed to the repo, so it is always available to unit tests.
func TestZeroAdvanceTextMeasuresZero(t *testing.T) {
	const ahem = "../../fonts/Ahem.ttf"

	if w, _ := MeasureText("​", 16, ahem); w.Float64() != 0 {
		t.Errorf(`MeasureText("​") = %v, want 0 (zero-advance run must not clamp to 1px)`, w.Float64())
	}
	// Sanity: a real glyph still measures its full advance (Ahem: 1em).
	if w, _ := MeasureText("X", 16, ahem); w.Float64() != 16 {
		t.Errorf(`MeasureText("X") = %v, want 16`, w.Float64())
	}
	// A zero-advance rune must not change a run's width when appended.
	base, _ := MeasureText("X. ", 16, ahem)
	withZWSP, _ := MeasureText("X. ​", 16, ahem)
	if base.Raw() != withZWSP.Raw() {
		t.Errorf(`MeasureText("X. ​") = %v, want %v (ZWSP is zero-advance)`, withZWSP.Float64(), base.Float64())
	}
}
