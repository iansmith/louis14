package render_test

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

// renderScrolled lays htmlContent out after giving the element #s the
// requested scroll offset (as Element.scrollTo would) and renders it.
func renderScrolled(t *testing.T, htmlContent string, offset html.ScrollOffset) *image.RGBA {
	t.Helper()
	doc, err := html.Parse(htmlContent)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if id, ok := n.GetAttribute("id"); ok && id == "s" {
			n.ScrollOffset = offset
		}
		for _, c := range n.Children {
			find(c)
		}
	}
	find(doc.Root)
	boxes := layout.NewLayoutEngine(200, 200).Layout(doc)
	target := image.NewRGBA(image.Rect(0, 0, 200, 200))
	render.NewRendererForImage(target).Render(boxes)
	return target
}

func rgbAt(img *image.RGBA, x, y int) color.RGBA {
	c := img.RGBAAt(x, y)
	return color.RGBA{c.R, c.G, c.B, 255}
}

// TestLocalAttachmentBackgroundScrollsWithContents pins the local branch
// of BoxPainterBase::PaintFillLayer (box_painter_base.cc:1378-1386 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) → BoxFragmentPainter::
// AdjustRectForScrolledContent (box_fragment_painter.cc:2321-2343): a
// background-attachment:local layer on a scroll container is positioned
// in the scrolled rect (offset by −ScrollPosition, sized to the scroll
// size plus borders) and clipped to the overflow clip rect.
func TestLocalAttachmentBackgroundScrollsWithContents(t *testing.T) {
	const src = `<!DOCTYPE html><body style="margin:0">` +
		`<div id=s style="overflow:hidden;width:100px;height:100px;background:blue ` +
		`linear-gradient(lime,lime) 0 0 / 100px 100px no-repeat local">` +
		`<div style="width:300px;height:100px"></div></div>`
	lime := color.RGBA{0, 255, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	for _, tc := range []struct {
		name   string
		offset html.ScrollOffset
		want   color.RGBA
	}{
		{"unscrolled", html.ScrollOffset{}, lime},
		// Scrolled by 100px: the 100px tile at the scrolled origin is out
		// of view, the color fills the whole scrolled rect.
		{"scrolled", html.ScrollOffset{X: 100}, blue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderScrolled(t, src, tc.offset)
			if got := rgbAt(img, 50, 50); got != tc.want {
				t.Errorf("(50,50) = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLocalAttachmentContentBoxClipScrolls pins that background-clip:
// content-box on a local layer clips to the scrolled content edge: the
// non-rounded kContent clip contracts scrolled_paint_rect by border +
// padding (box_painter_base.cc:1533-1551 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so at the maximum scroll
// offset the start padding is scrolled away and the end padding shows.
func TestLocalAttachmentContentBoxClipScrolls(t *testing.T) {
	const src = `<!DOCTYPE html><body style="margin:0">` +
		`<div id=s style="box-sizing:border-box;overflow:hidden;width:100px;height:100px;` +
		`border:1px solid black;padding:20px;background:blue local content-box">` +
		`<div style="width:400px;height:400px"></div></div>`
	white := color.RGBA{255, 255, 255, 255}
	blue := color.RGBA{0, 0, 255, 255}
	img := renderScrolled(t, src, html.ScrollOffset{X: 9000, Y: 9000})
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{
		{5, 5, blue},    // start padding scrolled out of view
		{50, 50, blue},  // content
		{90, 50, white}, // end padding (right)
		{50, 90, white}, // end padding (bottom)
	} {
		if got := rgbAt(img, p.x, p.y); got != p.want {
			t.Errorf("(%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
}
