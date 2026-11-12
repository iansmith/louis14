package js

import "testing"

// TestWindowInnerSize: window.innerWidth / innerHeight are the viewport's
// size, scrollbars included, as integers (LocalDOMWindow::innerWidth /
// innerHeight → GetViewportSize → LocalFrameView::Size, core/frame/
// local_dom_window.cc:1723-1771 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// runWithLayout lays the page out in an 800×600 viewport.
func TestWindowInnerSize(t *testing.T) {
	for _, page := range []string{
		`<!DOCTYPE html><body></body>`,
		`<!DOCTYPE html><style>html{overflow:scroll}body{height:3000px}</style><body></body>`,
	} {
		runWithLayout(t, page, `
			if (window.innerWidth !== 800 || innerHeight !== 600) {
				throw new Error("inner size " + window.innerWidth + "x" + innerHeight + ", want 800x600");
			}
		`)
	}
}
