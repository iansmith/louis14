package render

import (
	"image"
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

// TestComplexOutline_TranslucentDashedCornerOverlap: a dashed or dotted
// outline whose color is not opaque paints in an alpha layer at the color's
// alpha, with the color made opaque (ComplexOutlinePainter::Paint,
// core/paint/outline_painter.cc:474-479 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so where two edges' dashes
// overlap at a corner (PaintStraightEdge extends each edge past it) the
// color is applied once. The second fragment's top-left corner square,
// x 6-10, y 26-30, is covered by both the top and the left edge's first
// dash, x 11 by the top edge's alone: half-transparent blue on black is
// about (0, 0, 128) at both, not the (0, 0, 191) of compositing it twice.
func TestComplexOutline_TranslucentDashedCornerOverlap(t *testing.T) {
	img, _ := RenderDocument(t, ParsePage(t, splitInlineOutlinePage("4px dashed rgba(0, 0, 255, 0.5)")), 160, 80)
	for _, p := range []struct {
		x, y int
		why  string
	}{
		{11, 27, "guard: one edge's dash alone"},
		{7, 27, "two edges' dashes overlap at the corner, composited once"},
	} {
		if got := img.RGBAAt(p.x, p.y); got.R > 8 || got.G > 8 || got.B < 120 || got.B > 136 {
			t.Errorf("pixel (%d,%d) = %v, want about (0, 0, 128): %s", p.x, p.y, got, p.why)
		}
	}
}

// TestComplexOutline_RoundedDottedFollowsCorners: a dashed or dotted
// multi-rect outline with border-radius strokes its whole rounded centre
// path with one dash pattern sized to the path's length
// (ComplexOutlinePainter::PaintDottedOrDashedOutline, rounded branch,
// core/paint/outline_painter.cc:564-572 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), rather than dotting each
// straight edge. The span's two 80px fragments (20,20)-(100,40) and
// (20,40)-(100,60) unite into one rect; outset by the 6px width it is
// (14,14)-(106,66), and its centre path, shrunk by 3px, is 86x46 with
// 13px radii: 241px around (Path::length, truncated). Round dots 6px
// across fit 20 to that closed path (SelectBestDashGap,
// platform/graphics/styled_stroke_data.cc:40-58), so the outline is 20
// separate dots.
func TestComplexOutline_RoundedDottedFollowsCorners(t *testing.T) {
	doc := ParsePage(t, `<!doctype html><style>
body { margin: 0; background: black }
#d { position: absolute; left: 20px; top: 20px; width: 80px; font: 20px/20px Ahem; color: transparent }
#s { outline: 6px dotted blue; border-radius: 10px }
</style><div id=d><span id=s>XXXX XXXX</span></div>`)
	img, _ := RenderDocument(t, doc, 120, 80)
	isDot := func(c color.Color) bool {
		_, _, b, _ := c.RGBA()
		return b>>8 >= 128
	}
	if got := countBlobs(img, isDot); got != 20 {
		t.Errorf("outline has %d dots, want 20", got)
	}
}

// countBlobs counts the 8-connected regions of img's pixels for which in
// is true.
func countBlobs(img *image.RGBA, in func(color.Color) bool) int {
	b := img.Bounds()
	seen := make([]bool, b.Dx()*b.Dy())
	visit := func(p image.Point) bool {
		i := (p.Y-b.Min.Y)*b.Dx() + p.X - b.Min.X
		if !p.In(b) || seen[i] || !in(img.At(p.X, p.Y)) {
			return false
		}
		seen[i] = true
		return true
	}
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !visit(image.Pt(x, y)) {
				continue
			}
			n++
			stack := []image.Point{image.Pt(x, y)}
			for len(stack) > 0 {
				q := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						if r := q.Add(image.Pt(dx, dy)); visit(r) {
							stack = append(stack, r)
						}
					}
				}
			}
		}
	}
	return n
}
