package render

import "testing"

// TestSidewaysEmphasisMarkScreenX pins where a sideways text fragment's
// emphasis mark buffer lands on screen. Blink paints the text in its
// line-relative frame from the text origin (fragment left, fragment top +
// integer ascent; TextFragmentPainter::Paint, core/paint/
// text_fragment_painter.cc:516-522), offsets the marks' baseline by
// TextPainter::SetEmphasisMark (core/paint/text_painter.cc:510-550: over =
// floor(-ascent - markDescent), under = ceil(descent + markAscent)), and maps
// line-relative (lx, ly) to physical with LineRelativeRect::
// ComputeRelativeToPhysicalTransform (core/paint/line_relative_rect.cc:
// 19-75): x = -ly + L + O + B clockwise (every vertical writing mode but
// sideways-lr), x = ly + L - O counter-clockwise (sideways-lr), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. A mark buffer annotH tall whose
// glyph baseline sits markAscent from its top then starts at
// L + B - rel + markAscent - annotH clockwise and at L + rel - markAscent
// counter-clockwise, rel being the mark baseline less the fragment top.
//
// Fragment at L = 100, width B = 17, integer ascent 14 and descent 3; mark
// ascent 6.09375, descent 1.90625, buffer 8 px:
//   - over:  rel = floor(-1.90625) = -2
//   - under: rel = 14 + ceil(3 + 6.09375) = 24
func TestSidewaysEmphasisMarkScreenX(t *testing.T) {
	g := sidewaysEmphasisGeometry{
		boxLeft: 100, boxWidth: 17, ascent: 14, descent: 3,
		markAscent: 6.09375, markDescent: 1.90625, annotH: 8,
	}
	for _, tc := range []struct {
		name            string
		over, clockwise bool
		want            float64
	}{
		{"over, clockwise", true, true, 100 + 17 + 2 + 6.09375 - 8},
		{"under, clockwise", false, true, 100 + 17 - 24 + 6.09375 - 8},
		{"over, counter-clockwise", true, false, 100 - 2 - 6.09375},
		{"under, counter-clockwise", false, false, 100 + 24 - 6.09375},
	} {
		if got := g.screenX(tc.over, tc.clockwise); got != tc.want {
			t.Errorf("%s: screenX = %v, want %v", tc.name, got, tc.want)
		}
	}
}
