package render_test

import (
	"testing"
)

// TestOutsetBoxShadow_NoLeakInsideBorderBox is the reproduction for the
// outset box-shadow blur leak (filter-effects/backdrop-filter-box-shadow.html).
//
// Per CSS Backgrounds 3 §7.1, an outset box-shadow must not paint inside the
// border box at all. louis14 builds the shadow in an offscreen buffer, clears
// the border-box "hole", then runs a Gaussian box blur. Because the blur runs
// *after* the hole is cleared, the blurred shadow spreads back across the
// cleared boundary into the border box by a few pixels. With an opaque element
// background this is invisible (the background paints over it), but with a
// transparent background the gray shadow shows through.
//
// Scenario: a transparent element with an offset, blurred outset box-shadow
// over a white page. The shadow is offset down-right (20,20), so the only
// uncleared shadow adjacent to the border box lies past its right and bottom
// edges; the blur leaks that gray inward along those two inner edges. The
// border-box interior must stay pure white.
func TestOutsetBoxShadow_NoLeakInsideBorderBox(t *testing.T) {
	const htmlContent = `<!DOCTYPE html><html><body style="margin:0;background:#fff">` +
		`<div style="position:absolute;left:50px;top:50px;width:100px;height:100px;` +
		`box-shadow:20px 20px 10px 0 #888888"></div>` +
		`</body></html>`

	img := renderToImage(t, htmlContent, 220, 220)

	// Scan the border-box interior, inset by 2px on every side to skip the
	// boundary anti-aliasing at the box edges. Every interior pixel must be
	// white (no gray shadow leak). The leak is worst at the bottom-right inner
	// corner, where the blur pulls gray from both the right and bottom strips.
	worstX, worstY, worstMin := -1, -1, 255
	for y := 52; y < 148; y++ {
		for x := 52; x < 148; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			lo := min(int(r>>8), int(g>>8), int(b>>8))
			if lo < worstMin {
				worstMin, worstX, worstY = lo, x, y
			}
		}
	}

	// White is 255; allow a 2-level tolerance for incidental AA. A leak drives
	// the channel well below that (gray 136 at ~60% coverage over white ≈ 184).
	if worstMin < 253 {
		t.Errorf("outset box-shadow leaked into the border box: darkest interior pixel at (%d,%d) has min channel %d, want >= 253 (pure white)",
			worstX, worstY, worstMin)
	}
}
