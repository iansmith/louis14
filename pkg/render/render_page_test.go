package render

import (
	"image"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// RenderOption configures RenderPage and RenderDocument. Exported from a
// _test.go file, so only tests (this package's and render_test's) see it.
type RenderOption func(*renderPageConfig)

type renderPageConfig struct {
	imageFetcher       func(uri string) ([]byte, error)
	externalSVGFetcher func(uri string) ([]byte, error)
}

// WithImageFetcher installs the layout engine's image fetcher.
func WithImageFetcher(f func(uri string) ([]byte, error)) RenderOption {
	return func(c *renderPageConfig) { c.imageFetcher = f }
}

// WithExternalSVGFetcher installs the renderer's external SVG document
// fetcher (url(doc.svg#id) references).
func WithExternalSVGFetcher(f func(uri string) ([]byte, error)) RenderOption {
	return func(c *renderPageConfig) { c.externalSVGFetcher = f }
}

// RenderPage parses src, lays it out in a w×h viewport and renders it onto a
// fresh image, returning the image and the root box.
func RenderPage(t testing.TB, src string, w, h int, opts ...RenderOption) (*image.RGBA, *layout.Box) {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	return RenderDocument(doc, w, h, opts...)
}

// RenderDocument is RenderPage for a parsed (and possibly mutated)
// document.
func RenderDocument(doc *html.Document, w, h int, opts ...RenderOption) (*image.RGBA, *layout.Box) {
	var c renderPageConfig
	for _, o := range opts {
		o(&c)
	}
	engine := layout.NewLayoutEngine(float64(w), float64(h))
	if c.imageFetcher != nil {
		engine.SetImageFetcher(c.imageFetcher)
	}
	boxes := engine.Layout(doc)
	target := image.NewRGBA(image.Rect(0, 0, w, h))
	r := NewRendererForImage(target)
	if c.externalSVGFetcher != nil {
		r.SetExternalSVGFetcher(c.externalSVGFetcher)
	}
	r.Render(boxes)
	return target, boxes[0]
}
