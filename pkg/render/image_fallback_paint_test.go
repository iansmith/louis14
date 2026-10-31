package render

// LOU-433: an <img> with fallback content (HTMLImageElement::LayoutDisposition
// kFallbackContent) is a LayoutBlockFlow, not a LayoutImage
// (HTMLImageElement::CreateLayoutObject, html_image_element.cc:517-543 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): it paints its alt-text content
// like any block, never an image, never as a multicol container
// (LayoutBlockFlow::AllowsColumns, layout_block_flow.cc:641-645 @ same SHA),
// and gets scrollbars when it is a scroll container.

import (
	"errors"
	"image"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
)

func renderWithErroringFetcher(t *testing.T, src string, w, h int) (*image.RGBA, *layout.Box) {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	engine := layout.NewLayoutEngine(float64(w), float64(h))
	engine.SetImageFetcher(func(uri string) ([]byte, error) { return nil, errors.New("404: " + uri) })
	boxes := engine.Layout(doc)
	target := image.NewRGBA(image.Rect(0, 0, w, h))
	NewRendererForImage(target).Render(boxes)
	return target, boxes[0]
}

func findImgBox(b *layout.Box) *layout.Box {
	if b == nil {
		return nil
	}
	if b.Node != nil && b.Node.TagName == "img" {
		if id, _ := b.Node.GetAttribute("id"); id != "alttext-image" {
			return b
		}
	}
	for _, c := range b.Children {
		if f := findImgBox(c); f != nil {
			return f
		}
	}
	return nil
}

func TestPaint_FallbackImage(t *testing.T) {
	t.Run("no image source captured", func(t *testing.T) {
		_, root := renderWithErroringFetcher(t, `<!DOCTYPE html><body style="margin:0"><img src="missing.png" alt="X"></body>`, 100, 100)
		box := findImgBox(root)
		if box == nil {
			t.Fatal("no img box")
		}
		if got := newPaintLayer(box).ImageSrc; got != "" {
			t.Errorf("fallback img ImageSrc = %q, want empty (a LayoutBlockFlow paints no image)", got)
		}
	})
	t.Run("not a multicol paint layer", func(t *testing.T) {
		_, root := renderWithErroringFetcher(t, `<!DOCTYPE html><body style="margin:0"><img src="missing.png" alt="X X" style="display:block; width:100px; columns:5; column-rule:2px solid blue"></body>`, 200, 100)
		box := findImgBox(root)
		if box == nil {
			t.Fatal("no img box")
		}
		if newPaintLayer(box).IsMulticol {
			t.Errorf("fallback img paint layer IsMulticol = true, want false (AllowsColumns vetoes <img>)")
		}
	})
	t.Run("block host is not atomic-painted as replaced", func(t *testing.T) {
		// UA overflow:clip on img made every img tag atomic-paint; a
		// display:block fallback host is an ordinary block.
		_, root := renderWithErroringFetcher(t, `<!DOCTYPE html><body style="margin:0"><img src="missing.png" alt="X" style="display:block"></body>`, 100, 100)
		box := findImgBox(root)
		if box == nil {
			t.Fatal("no img box")
		}
		if isAtomicInlineForPaint(newPaintLayer(box)) {
			t.Errorf("display:block fallback img is atomic-painted; want ordinary block paint")
		}
	})
	t.Run("scroll container gets scrollbars", func(t *testing.T) {
		img, _ := renderWithErroringFetcher(t, `<!DOCTYPE html><body style="margin:0"><img src="missing.png" alt="X" style="display:block; width:50px; height:50px; overflow:scroll; scrollbar-color:blue white"></body>`, 100, 100)
		// The vertical gutter's leading thumb (blue) sits at the right edge.
		if r, g, b := rgbAt(img, 44, 5); !(r < 64 && g < 64 && b > 192) {
			t.Errorf("pixel (44,5) = (%d,%d,%d); want the blue scrollbar thumb", r, g, b)
		}
	})
	t.Run("alt text glyph painted beside the icon", func(t *testing.T) {
		img, _ := renderWithErroringFetcher(t, `<!DOCTYPE html><body style="margin:0"><img src="missing.png" alt="X" style="font-family:Ahem; font-size:20px; line-height:20px; color:black"></body>`, 100, 100)
		// The 16px icon float comes first; the Ahem "X" covers x 16..36,
		// y 0..20 (line-height equals the em box).
		if r, g, b := rgbAt(img, 26, 10); r != 0 || g != 0 || b != 0 {
			t.Errorf("pixel (26,10) = (%d,%d,%d); want the black alt-text glyph", r, g, b)
		}
	})
}
