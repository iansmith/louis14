package layout

import (
	"testing"

	"louis14/pkg/html"
)

// viewScrolledLayout lays src out at 800x600 after giving the layout
// viewport (the Document node's scrollable area) the requested scroll
// offset, as window.scrollTo / the scrolling element's scrollLeft would.
// It returns the page rects of every id'd box and the LayoutView's box
// from NodeBoxes (keyed by the Document node, LayoutView::GetNode).
func viewScrolledLayout(t *testing.T, src string, offset html.ScrollOffset) (map[string]fragRect, *Box) {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	doc.Root.ScrollOffset = offset
	le := NewLayoutEngine(800, 600)
	out := map[string]fragRect{}
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Node != nil && b.Node.Type == html.ElementNode {
			if id, ok := b.Node.GetAttribute("id"); ok {
				if _, seen := out[id]; !seen {
					out[id] = fragRect{b.X, b.Y, b.Width, b.Height}
				}
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range le.Layout(doc) {
		walk(b)
	}
	return out, le.NodeBoxes()[doc.Root]
}

// TestLayoutViewScrollTranslatesDocument pins viewport scrolling: the
// LayoutView is a scroll container over the document's scrollable overflow
// (the ICB united with the root's overflow), its scroll offset clamps to
// [MinimumScrollOffset, MaximumScrollOffset] on both axes whatever the
// overflow values (PaintLayerScrollableArea::ScrollableAxes,
// paint/paint_layer_scrollable_area.cc:2484-2508 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the clamped offset
// translates the whole document — except a fixed-position box the view
// contains, which is not in the view's scrolling contents.
func TestLayoutViewScrollTranslatesDocument(t *testing.T) {
	const src = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div id="wide" style="width:1072px;height:10px"></div>` +
		`<div id="fixed" style="position:fixed;left:5px;top:7px;width:10px;height:10px"></div>` +
		`<div id="abs" style="position:absolute;left:5px;top:30px;width:10px;height:10px"></div>` +
		`</body></html>`
	rects, view := viewScrolledLayout(t, src, html.ScrollOffset{X: 5000, Y: 40})
	if view == nil {
		t.Fatal("NodeBoxes has no LayoutView box for the Document node")
	}
	if want := (html.ScrollOffset{X: 272}); view.ScrollOffset != want {
		t.Errorf("view ScrollOffset = %+v, want %+v (x clamped to 1072-800, y to 0)", view.ScrollOffset, want)
	}
	if view.ScrollSize.Width != 1072 || view.ScrollSize.Height != 600 {
		t.Errorf("view ScrollSize = %+v, want 1072x600", view.ScrollSize)
	}
	if got := rects["wide"]; got.x != -272 || got.y != 0 {
		t.Errorf("#wide at (%v,%v), want (-272,0)", got.x, got.y)
	}
	if got := rects["abs"]; got.x != 5-272 || got.y != 30 {
		t.Errorf("#abs at (%v,%v), want (%v,30): the view's absolute children scroll", got.x, got.y, 5-272)
	}
	if got := rects["fixed"]; got.x != 5 || got.y != 7 {
		t.Errorf("#fixed at (%v,%v), want (5,7): fixed boxes do not scroll with the view", got.x, got.y)
	}
}

// TestLayoutViewScrollOriginVerticalRL: a vertical-rl document overflows
// leftward, so the view's scroll origin is on the right
// (PaintLayerScrollableArea::UpdateScrollOrigin,
// paint_layer_scrollable_area.cc:927-938 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): offsets run from
// −(overflow) to 0 and a positive request clamps to 0, leaving the
// block-start (right) edge in view.
func TestLayoutViewScrollOriginVerticalRL(t *testing.T) {
	const src = `<!DOCTYPE html><html style="writing-mode:vertical-rl"><body style="margin:0">` +
		`<div id="tall" style="width:1000px;height:10px"></div></body></html>`
	rects, view := viewScrolledLayout(t, src, html.ScrollOffset{X: 1000})
	if view == nil {
		t.Fatal("NodeBoxes has no LayoutView box for the Document node")
	}
	if view.ScrollOffset != (html.ScrollOffset{}) {
		t.Errorf("view ScrollOffset = %+v, want {0 0}", view.ScrollOffset)
	}
	if want := (html.ScrollOffset{X: -200}); view.MinimumScrollOffset != want {
		t.Errorf("view MinimumScrollOffset = %+v, want %+v", view.MinimumScrollOffset, want)
	}
	if got := rects["tall"]; got.x != -200 {
		t.Errorf("#tall x = %v, want -200 (right-aligned, unscrolled)", got.x)
	}
	rects, _ = viewScrolledLayout(t, src, html.ScrollOffset{X: -50})
	if got := rects["tall"]; got.x != -150 {
		t.Errorf("scrolled -50: #tall x = %v, want -150", got.x)
	}
}
