package render

import "testing"

// TestSidewaysEmphasisMarkScreenX pins where a sideways text fragment's
// emphasis mark buffer lands on screen. Blink paints the text in its
// line-relative frame from the text origin (fragment left, fragment top +
// integer ascent; TextFragmentPainter::Paint, core/paint/
// text_fragment_painter.cc:516-522), offsets the marks' baseline by
// TextPainter::SetEmphasisMark (core/paint/text_painter.cc:510-550: over =
// floor(-ascent - markDescent)), and maps
// line-relative (lx, ly) to physical with LineRelativeRect::
// ComputeRelativeToPhysicalTransform (core/paint/line_relative_rect.cc:
// 19-75): x = -ly + L + O + B clockwise (every vertical writing mode but
// sideways-lr), x = ly + L - O counter-clockwise (sideways-lr), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. A mark buffer annotH tall whose
// glyph baseline sits markAscent from its top then starts at
// L + B - rel + markAscent - annotH clockwise and at L + rel - markAscent
// counter-clockwise, rel being the mark baseline less the fragment top.
//
// The item's annotation ascent lowers the over offset by its em-height
// adjustment (SetEmphasisMark, text_painter.cc:529, from
// AdjustTextOverUnderOffsetsForEmHeight, core/layout/inline/ruby_utils.cc:
// 36-79, same SHA). Liberation Serif 16px: fragment at L = 100, width
// B = 17, adjustment floor(14 - 12.203125) = 1; mark ascent 6.09375,
// descent 1.90625, buffer 8 px: rel = 1 + floor(-1.90625) = -1.
func TestSidewaysEmphasisMarkScreenX(t *testing.T) {
	g := sidewaysEmphasisGeometry{
		boxLeft: 100, boxWidth: 17, overAdjust: 1,
		markAscent: 6.09375, markDescent: 1.90625, annotH: 8,
	}
	for _, tc := range []struct {
		name      string
		clockwise bool
		want      float64
	}{
		{"clockwise", true, 100 + 17 + 1 + 6.09375 - 8},
		{"counter-clockwise", false, 100 - 1 - 6.09375},
	} {
		if got := g.screenX(tc.clockwise); got != tc.want {
			t.Errorf("%s: screenX = %v, want %v", tc.name, got, tc.want)
		}
	}
}
