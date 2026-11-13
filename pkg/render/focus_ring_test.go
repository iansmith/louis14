package render

import (
	"image/color"
	"testing"
)

// TestFocusRing_SingleRect pins Blink's focus ring for outline-style: auto
// around one rect (PaintFocusRing / PaintSingleFocusRing / FocusRingOffset,
// core/paint/outline_painter.cc:38-74, :758-811, :813-859, :861-930 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; DrawPlatformFocusRing,
// platform/graphics/platform_focus_ring.cc, same SHA): a 3px ring of a 2px
// outer stroke in white centred one pixel outside the outline rect, then a
// 2px inner stroke in outline-color centred on it, corner radius 3. With no
// border, outline-offset 0 and the box's left edge at x = 20, the outer
// stroke covers x 18-19 and the inner, painted over it, x 19-20 (one pixel
// inside the box): x 18 white, 19 and 20 blue, 21 the box's green.
func TestFocusRing_SingleRect(t *testing.T) {
	doc := ParsePage(t, `<!doctype html><style>
body { margin: 0; background: black }
#t { position: absolute; left: 20px; top: 20px; width: 100px; height: 50px; background: lime; outline: auto 1px blue }
</style><div id=t></div>`)
	img, _ := RenderDocument(t, doc, 160, 100)
	white := color.RGBA{255, 255, 255, 255}
	blue := color.RGBA{0, 0, 255, 255}
	lime := color.RGBA{0, 255, 0, 255}
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{
		{18, 45, white}, {19, 45, blue}, {20, 45, blue}, {21, 45, lime}, // left edge
		{70, 18, white}, {70, 19, blue}, {70, 20, blue}, {70, 21, lime}, // top edge
		{121, 45, white}, {120, 45, blue}, {119, 45, blue}, {118, 45, lime}, // right edge
	} {
		if got := img.RGBAAt(p.x, p.y); got != p.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
}

// TestFocusRing_MultiRect pins Blink's focus ring around an inline split
// across two lines: the fragment rects are united as a region whose
// boundary is stroked (PaintSingleFocusRing → ComputeRightAnglePath,
// core/paint/outline_painter.cc:87-101, :813-859 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), not their bounding box. The
// span's first fragment is (90,10)-(130,30) and its second (10,30)-(90,50)
// in Ahem 20px, so the inner ring covers y 29-30 under the first line's
// "XXX " and nothing runs along the bounding box's top and bottom edges
// there.
func TestFocusRing_MultiRect(t *testing.T) {
	doc := ParsePage(t, `<!doctype html><style>
body { margin: 0; background: black }
#d { position: absolute; left: 10px; top: 10px; width: 120px; font: 20px/20px Ahem; color: transparent }
#s { outline: auto 1px blue }
</style><div id=d>XXX <span id=s>XX XXXX</span></div>`)
	img, _ := RenderDocument(t, doc, 160, 80)
	blue := color.RGBA{0, 0, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	opPixel(t, img, 110, 9, blue, "guard: the first fragment's top edge is ringed")
	opPixel(t, img, 50, 29, blue, "second fragment's top edge, outer row")
	opPixel(t, img, 50, 30, blue, "second fragment's top edge, inner row")
	opPixel(t, img, 50, 9, black, "no ring along the bounding box's top edge over \"XXX \"")
	opPixel(t, img, 110, 49, black, "no ring along the bounding box's bottom edge under the first fragment")
}
