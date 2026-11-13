package render

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/text"
)

// RenderOption configures RenderPage and RenderDocument. Exported from a
// _test.go file, so only tests (this package's and render_test's) see it.
type RenderOption func(*renderPageConfig)

type renderPageConfig struct {
	imageFetcher       func(uri string) ([]byte, error)
	externalSVGFetcher func(uri string) ([]byte, error)
	withEditingState   bool
}

// WithEditingState passes the document's selection and focused element to
// the renderer, with the document's stylesheets for ::selection rules, as
// the reftest harness does (visualtest.helpers), so ::selection and the caret
// paint.
func WithEditingState() RenderOption {
	return func(c *renderPageConfig) { c.withEditingState = true }
}

// WithImageFetcher installs the image fetcher on the layout engine and the
// renderer, as production wires one fetcher into both.
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
	fonts, hasFaces := pageFontConfig(t, doc)
	if hasFaces {
		engine.SetFontConfig(fonts)
	}
	boxes := engine.Layout(doc)
	target := image.NewRGBA(image.Rect(0, 0, w, h))
	r := NewRendererForImage(target)
	if hasFaces {
		r.SetFonts(fonts)
		t.Cleanup(r.closeTempFonts)
	}
	if c.imageFetcher != nil {
		r.SetImageFetcher(c.imageFetcher)
	}
	if c.externalSVGFetcher != nil {
		r.SetExternalSVGFetcher(c.externalSVGFetcher)
	}
	if c.withEditingState {
		r.SetSelectionContext(doc.Selection, css.ParseDocumentStylesheets(doc), float64(w), float64(h))
		r.SetFocusedElement(doc.FocusedElement)
	}
	r.Render(boxes)
	return target, boxes
}

// pageFontConfig registers doc's @font-face rules, their url()s read from
// the repo's fonts/ directory, as the production and WPT paths register a
// document's faces. A face none of whose sources loads fails t, so a test
// never silently draws in a fallback face. ok is false when doc declares no
// faces.
func pageFontConfig(t testing.TB, doc *html.Document) (fc text.FontConfig, ok bool) {
	t.Helper()
	faces := css.DocumentFontFaces(doc)
	if len(faces) == 0 {
		return fc, false
	}
	// A face stops at its first source that loads, so every face loaded
	// exactly when there are as many successful fetches as faces.
	loaded := 0
	fc = text.DefaultFontConfig().WithUserFontFaces(faces, func(url string) ([]byte, error) {
		data, err := repoFontFetcher(url)
		if err == nil {
			loaded++
		}
		return data, err
	})
	if loaded != len(faces) {
		t.Fatalf("only %d of the page's %d @font-face rules loaded", loaded, len(faces))
	}
	return fc, true
}

// repoFontFetcher reads a font url() from the repo's fonts/ directory.
func repoFontFetcher(url string) ([]byte, error) {
	return os.ReadFile(filepath.Join("..", "..", "fonts", url))
}

// Every paint pass in this package's tests asserts it left no Push/Pop
// imbalance: a Pop past the renderer's own pushes panics the test that
// rendered, so it is attributed to that test even under t.Parallel.
func init() {
	paintPassDoneHook = func(r *Renderer) {
		if n := r.popUnderflows(); n != 0 {
			panic(fmt.Sprintf("render: this renderer's paint passes popped its draw context past its own pushes %d time(s): a Push/Pop imbalance", n))
		}
	}
}
