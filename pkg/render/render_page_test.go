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

// ParsePage parses src, failing t on a parse error.
func ParsePage(t testing.TB, src string) *html.Document {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	return doc
}

// RenderPage parses src, lays it out in a w×h viewport and renders it onto a
// fresh image.
func RenderPage(t testing.TB, src string, w, h int, opts ...RenderOption) *image.RGBA {
	t.Helper()
	img, _ := RenderDocument(t, ParsePage(t, src), w, h, opts...)
	return img
}

// RenderDocument is RenderPage for a parsed (and possibly mutated or
// scripted) document; it also returns the laid-out root boxes.
func RenderDocument(t testing.TB, doc *html.Document, w, h int, opts ...RenderOption) (*image.RGBA, []*layout.Box) {
	t.Helper()
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
	return target, boxes
}
