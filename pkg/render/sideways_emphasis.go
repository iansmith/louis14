package render

import (
	"math"

	"louis14/pkg/text"
)

// sidewaysEmphasisGeometry is what places a sideways text fragment's
// emphasis mark buffer: the fragment's physical left edge and width, the
// base font's integer ascent and descent, and the mark font's ascent,
// descent and buffer height.
type sidewaysEmphasisGeometry struct {
	boxLeft, boxWidth       float64
	ascent, descent         float64
	markAscent, markDescent float64
	annotH                  int
}

// screenX returns the physical X of the mark buffer's left column, for a
// mark on the line-over (over) or line-under side, in a clockwise (every
// vertical writing mode but sideways-lr) or counter-clockwise rotation.
//
// Blink paints the text in its line-relative frame from the text origin
// (fragment left, fragment top + integer ascent; TextFragmentPainter::Paint,
// core/paint/text_fragment_painter.cc:516-522), offsets the marks'
// baseline by TextPainter::SetEmphasisMark (core/paint/text_painter.cc:
// 510-550: over floor(-ascent - markDescent), under ceil(descent +
// markAscent)) and maps line-relative (lx, ly) to physical with
// LineRelativeRect::ComputeRelativeToPhysicalTransform
// (core/paint/line_relative_rect.cc:19-75: x = -ly + L + O + B clockwise,
// x = ly + L - O counter-clockwise), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. The buffer holds the mark with
// its baseline markAscent below its top, so its left column is
// L + B - rel + markAscent - annotH clockwise and L + rel - markAscent
// counter-clockwise, rel being the mark baseline less the fragment top.
// The text item's ruby annotation ascent (SetEmphasisMark's
// AnnotationMetrics) is not ported: louis14 has no emphasis-with-ruby
// metrics.
func (g sidewaysEmphasisGeometry) screenX(over, clockwise bool) float64 {
	rel := math.Floor(-g.markDescent)
	if !over {
		rel = g.ascent + math.Ceil(g.descent+g.markAscent)
	}
	if clockwise {
		return g.boxLeft + g.boxWidth - rel + g.markAscent - float64(g.annotH)
	}
	return g.boxLeft + rel - g.markAscent
}

// integerAscentDescent returns the font's integer ascent and descent at size,
// FontMetrics::Ascent / Descent (FontMetrics::AscentDescentWithHacks,
// platform/fonts/font_metrics.cc:113-117 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which place the text origin.
func (r *Renderer) integerAscentDescent(size float64, fontPath string) (ascent, descent float64) {
	return text.FontAscentFromFont(size, fontPath, r.fonts.Registry),
		text.FontDescentFromFont(size, fontPath, r.fonts.Registry)
}
