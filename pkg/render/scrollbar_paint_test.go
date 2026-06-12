package render_test

import (
	"testing"
)

// TestScrollbar_CustomColorTrackAndThumb pins the classic-scrollbar widget
// painter (LOU-257 / LOU-290). A 50x50 div with overflow:scroll and
// scrollbar-color: blue white reserves a classic gutter on the physical
// right (vertical bar) and bottom (horizontal bar). The painter must fill
// the reserved gutter with the track colour (white) and a leading-half
// thumb in the thumb colour (blue).
//
// Blink analog: ScrollableAreaPainter::PaintScrollbar /
// PaintOverflowControls (third_party/blink/renderer/core/paint/
// scrollable_area_painter.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f);
// scrollbar-color thumb/track mapping per CSS Scrollbars 1.
//
// Sample points are chosen to be valid regardless of the exact classic
// scrollbar width (the gutter spans x∈[33,50] at the engine's 17px
// reservation; x=44 sits inside it).
func TestScrollbar_CustomColorTrackAndThumb(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><head><style>` +
		`body{margin:0;padding:0}` +
		`div{width:50px;height:50px;overflow:scroll;scrollbar-color:blue white;background:yellow}` +
		`</style></head><body><div></div></body></html>`

	img := renderToImage(t, htmlContent, 200, 200)

	isBlue := func(x, y int) bool {
		r, g, b, a := img.At(x, y).RGBA()
		return a > 0 && r>>8 < 64 && g>>8 < 64 && b>>8 > 192
	}
	isWhite := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return r>>8 > 192 && g>>8 > 192 && b>>8 > 192
	}

	// Vertical bar: thumb (blue) in the top half, track (white) below.
	if !isBlue(44, 5) {
		r, g, b, _ := img.At(44, 5).RGBA()
		t.Errorf("vertical thumb at (44,5): got (%d,%d,%d), want blue", r>>8, g>>8, b>>8)
	}
	if !isWhite(44, 40) {
		r, g, b, _ := img.At(44, 40).RGBA()
		t.Errorf("vertical track at (44,40): got (%d,%d,%d), want white", r>>8, g>>8, b>>8)
	}

	// Horizontal bar (bottom gutter, y∈[33,50]): thumb (blue) leading half,
	// track (white) trailing portion.
	if !isBlue(3, 44) {
		r, g, b, _ := img.At(3, 44).RGBA()
		t.Errorf("horizontal thumb at (3,44): got (%d,%d,%d), want blue", r>>8, g>>8, b>>8)
	}
	if !isWhite(28, 44) {
		r, g, b, _ := img.At(28, 44).RGBA()
		t.Errorf("horizontal track at (28,44): got (%d,%d,%d), want white", r>>8, g>>8, b>>8)
	}
}
