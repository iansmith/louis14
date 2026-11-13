package render

import (
	"math"

	"louis14/pkg/layout"
)

// sidewaysEmphasisGeometry is what places a sideways text fragment's
// emphasis mark buffer: the fragment's physical left edge and width, and
// the mark font's ascent, descent and buffer height.
type sidewaysEmphasisGeometry struct {
	boxLeft, boxWidth       float64
	overAdjust              float64
	markAscent, markDescent float64
	annotH                  int
}

// screenX returns the physical X of the mark buffer's left column, for a
// mark on the line-over side, in a clockwise (every vertical writing mode but
// sideways-lr) or counter-clockwise rotation. louis14 paints every mark on
// the over side (see drawSidewaysEmphasisMarks).
//
// Blink paints the text in its line-relative frame from the text origin
// (fragment left, fragment top + integer ascent; TextFragmentPainter::Paint,
// core/paint/text_fragment_painter.cc:516-522), offsets the marks'
// baseline by TextPainter::SetEmphasisMark (core/paint/text_painter.cc:
// 510-550: over floor(-ascent - markDescent)) and maps line-relative (lx, ly) to physical with
// LineRelativeRect::ComputeRelativeToPhysicalTransform
// (core/paint/line_relative_rect.cc:19-75: x = -ly + L + O + B clockwise,
// x = ly + L - O counter-clockwise), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. The buffer holds the mark with
// its baseline markAscent below its top, so its left column is
// L + B - rel + markAscent - annotH clockwise and L + rel - markAscent
// counter-clockwise, rel being the mark baseline less the fragment top.
// The over offset also takes the text item's annotation ascent: with
// TextEmphasisAsRuby stable every emphasized item has one, the negated
// em-height adjustment overAdjust (SetEmphasisMark, text_painter.cc:529;
// AdjustTextOverUnderOffsetsForEmHeight, core/layout/inline/ruby_utils.cc:
// 36-79, same SHA). The base ascent is an integer and overAdjust is whole,
// so rel = overAdjust + floor(-markDescent).
func (g sidewaysEmphasisGeometry) screenX(clockwise bool) float64 {
	rel := g.overAdjust + math.Floor(-g.markDescent)
	if clockwise {
		return g.boxLeft + g.boxWidth - rel + g.markAscent - float64(g.annotH)
	}
	return g.boxLeft + rel - g.markAscent
}

// sidewaysEmphasisCentralBaseline is a stub; see
// TestSidewaysEmphasisCentralBaseline.
func sidewaysEmphasisCentralBaseline(box *layout.Box) bool { return false }
