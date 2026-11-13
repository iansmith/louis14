package render

import (
	"image/color"
	"testing"
)

// TestComplexOutline_MultiRect pins Blink's non-auto outline around an
// inline split across two lines (OutlinePainter::PaintOutlineRects →
// ComplexOutlinePainter::Paint, core/paint/outline_painter.cc:438-526,
// :1018-1031 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the fragment
// rects, outset by outline-offset plus outline-width, are united into a
// right-angle path (ComputeRightAnglePath), and the band between it and
// the same path shrunk by the width (ShrinkRightAnglePath) is filled. The
// span's fragments are (90,10)-(130,30) and (10,30)-(90,50), so a 2px
// solid outline runs along y 28-29 above the second fragment and nothing
// runs along their bounding box's top and bottom edges.
func TestComplexOutline_MultiRect(t *testing.T) {
	doc := ParsePage(t, splitInlineOutlinePage("2px solid blue"))
	img, _ := RenderDocument(t, doc, 160, 80)
	blue := color.RGBA{0, 0, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	opPixel(t, img, 110, 9, blue, "guard: the first fragment's top edge is outlined")
	opPixel(t, img, 50, 28, blue, "second fragment's top edge, outer row")
	opPixel(t, img, 50, 29, blue, "second fragment's top edge, inner row")
	opPixel(t, img, 50, 9, black, "no outline along the bounding box's top edge over \"XXX \"")
	opPixel(t, img, 110, 51, black, "no outline along the bounding box's bottom edge under the first fragment")
}

// TestComplexOutline_DoubleMultiRect: a double outline's stripes are a
// third of its width each: the outer one between the outer right-angle
// path and that path shrunk by the stripe, the inner one between the path
// shrunk by width - stripe and the inner edge (ComplexOutlinePainter::PaintDoubleOutline,
// core/paint/outline_painter.cc:528-548 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 6px outline above the
// second fragment fills y 24-25 and 28-29 and leaves 26-27 empty.
func TestComplexOutline_DoubleMultiRect(t *testing.T) {
	doc := ParsePage(t, splitInlineOutlinePage("6px double blue"))
	img, _ := RenderDocument(t, doc, 160, 80)
	blue := color.RGBA{0, 0, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	for y, want := range map[int]color.RGBA{24: blue, 25: blue, 26: black, 27: black, 28: blue, 29: blue} {
		opPixel(t, img, 50, y, want, "outer stripe 24-25, gap 26-27, inner stripe 28-29")
	}
}
