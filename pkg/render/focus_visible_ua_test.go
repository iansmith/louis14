package render

import (
	"image/color"
	"testing"
)

// TestFocusVisible_UARing: Blink's UA stylesheet gives a :focus-visible
// element `outline: auto 1px -webkit-focus-ring-color`, and html/body
// `outline: none` (core/html/resources/html.css:1584-1586 and :1613-1615 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). -webkit-focus-ring-color is
// LayoutTheme::FocusRingColor, which Chrome on Linux sets to #101010
// (chrome/browser/renderer_preferences_util.cc:114-115, same SHA). A focused
// div therefore gets the focus ring with a #101010 inner ring: with its left
// edge at x = 20, x 19 and 20 are #101010 and x 18 the white outer ring.
func TestFocusVisible_UARing(t *testing.T) {
	const page = `<!doctype html><style>
body { margin: 0; background: black }
#t { position: absolute; left: 20px; top: 20px; width: 100px; height: 50px; background: lime }
</style><div id=t tabindex=0></div>`
	ring := color.RGBA{0x10, 0x10, 0x10, 255}
	white := color.RGBA{255, 255, 255, 255}

	doc := ParsePage(t, page)
	doc.SetFocusedElement(doc.Root.ElementByID("t"))
	img, _ := RenderDocument(t, doc, 160, 100)
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{{18, 45, white}, {19, 45, ring}, {20, 45, ring}} {
		if got := img.RGBAAt(p.x, p.y); got != p.want {
			t.Errorf("focused div: pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}

	doc = ParsePage(t, page)
	doc.SetFocusedElement(doc.Root.ElementByID("t").Parent)
	img, _ = RenderDocument(t, doc, 160, 100)
	if got := img.RGBAAt(0, 0); got != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("focused body: pixel (0,0) = %v, want the black page (no ring: html/body :focus-visible { outline: none })", got)
	}
}
