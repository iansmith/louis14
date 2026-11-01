package layout

import (
	"testing"

	"louis14/pkg/html"
)

// scrolledRects lays src out at 800x600 after giving the element #s the
// requested scroll offset (as Element.scrollLeft/scrollTop would) and
// returns the page rects of every id'd box.
func scrolledRects(t *testing.T, src string, offset html.ScrollOffset) map[string][]fragRect {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	var find func(n *html.Node) *html.Node
	find = func(n *html.Node) *html.Node {
		if id, ok := n.GetAttribute("id"); ok && id == "s" {
			return n
		}
		for _, c := range n.Children {
			if f := find(c); f != nil {
				return f
			}
		}
		return nil
	}
	scroller := find(doc.Root)
	if scroller == nil {
		t.Fatal("no #s element")
	}
	scroller.ScrollOffset = offset
	out := map[string][]fragRect{}
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Node != nil && b.Node.Type == html.ElementNode {
			if id, ok := b.Node.GetAttribute("id"); ok {
				out[id] = append(out[id], fragRect{b.X, b.Y, b.Width, b.Height})
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
		walk(b)
	}
	return out
}

// TestScrollOffsetTranslatesScrolledContents pins the scroll translation:
// a scroll container's contents move by -scroll offset, the container
// itself does not, and the offset is clamped to [MinimumScrollOffset,
// MaximumScrollOffset] derived from the scrollable overflow and the scroll
// origin (PaintLayerScrollableArea::ClampScrollOffset / UpdateScrollOrigin,
// paint/paint_layer_scrollable_area.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestScrollOffsetTranslatesScrolledContents(t *testing.T) {
	const ltr = `<!DOCTYPE html><body style="margin:0">` +
		`<div id=s style="overflow:hidden;width:100px;height:100px;border:5px solid">` +
		`<div id=c style="width:300px;height:400px"></div></div>`
	const vrl = `<!DOCTYPE html><body style="margin:0">` +
		`<div id=s style="overflow:hidden;writing-mode:vertical-rl;width:200px;height:100px">` +
		`<div id=c style="width:500px;height:50px"></div></div>`
	cases := []struct {
		name       string
		src        string
		offset     html.ScrollOffset
		wantX      float64
		wantY      float64
		scrollerX0 float64
	}{
		{"zero", ltr, html.ScrollOffset{}, 5, 5, 0},
		{"in range", ltr, html.ScrollOffset{X: 30, Y: 100}, 5 - 30, 5 - 100, 0},
		// Max = scrollable size: overflow 300x400 minus client 100x100.
		{"clamped to maximum", ltr, html.ScrollOffset{X: 9000, Y: 9000}, 5 - 200, 5 - 300, 0},
		// Min = -ScrollOrigin = 0 without leftward/upward overflow.
		{"clamped to minimum", ltr, html.ScrollOffset{X: -50, Y: -50}, 5, 5, 0},
		// vertical-rl overflows leftward: ScrollOrigin.x = 300, so the
		// range is [-300, 0] and a negative scrollLeft reveals the left.
		{"vertical-rl negative", vrl, html.ScrollOffset{X: -100}, -300 + 100, 0, 0},
		{"vertical-rl clamped", vrl, html.ScrollOffset{X: -9000}, 0, 0, 0},
		{"vertical-rl positive clamped", vrl, html.ScrollOffset{X: 50}, -300, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scrolledRects(t, tc.src, tc.offset)
			if s := got["s"]; len(s) != 1 || s[0].x != tc.scrollerX0 || s[0].y != 0 {
				t.Fatalf("scroller rects = %+v, want one at (%v,0)", s, tc.scrollerX0)
			}
			c := got["c"]
			if len(c) != 1 || c[0].x != tc.wantX || c[0].y != tc.wantY {
				t.Errorf("content rects = %+v, want one at (%v,%v)", c, tc.wantX, tc.wantY)
			}
		})
	}
}

// TestScrollOffsetIgnoredOnNonScrollContainer pins that only a scroll
// container has a scrollable area: overflow:visible and overflow:clip
// boxes never translate their contents (ScrollableAxes is none,
// paint_layer_scrollable_area.cc:2484-2490 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestScrollOffsetIgnoredOnNonScrollContainer(t *testing.T) {
	for _, overflow := range []string{"visible", "clip"} {
		t.Run(overflow, func(t *testing.T) {
			got := scrolledRects(t, `<!DOCTYPE html><body style="margin:0">`+
				`<div id=s style="overflow:`+overflow+`;width:100px;height:100px">`+
				`<div id=c style="width:300px;height:400px"></div></div>`,
				html.ScrollOffset{X: 30, Y: 30})
			if c := got["c"]; len(c) != 1 || c[0].x != 0 || c[0].y != 0 {
				t.Errorf("content rects = %+v, want one at (0,0)", c)
			}
		})
	}
}

// TestVerticalRLScrollbarGutterIsOnTheRight pins Blink's vertical
// scrollbar placement (ComputedStyle::
// ShouldPlaceBlockDirectionScrollbarOnLogicalLeft, core/style/
// computed_style.h:2369-2371 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// only a horizontal RTL box puts it on the left. In vertical-rl the right
// gutter is the block-start edge, so the first child sits left of it; in
// horizontal RTL the left gutter is inline-end and the child stays flush
// right.
func TestVerticalRLScrollbarGutterIsOnTheRight(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div id=v style="width:200px;height:200px;overflow:scroll;writing-mode:vertical-rl">`+
		`<div id=vc style="width:80px;height:180px"></div></div>`+
		`<div id=r style="width:200px;height:200px;overflow:scroll;direction:rtl">`+
		`<div id=rc style="width:80px;height:180px"></div></div>`)
	if v := got["v"]; len(v) != 1 {
		t.Fatalf("v rects = %+v", v)
	}
	if vc := got["vc"]; len(vc) != 1 || vc[0].x != 200-17-80 {
		t.Errorf("vertical-rl child rects = %+v, want x=%v", vc, 200-17-80)
	}
	if rc := got["rc"]; len(rc) != 1 || rc[0].x != 200-80 {
		t.Errorf("rtl child rects = %+v, want x=%v", rc, 200-80)
	}
}
