package resource

import (
	"fmt"
	"image"
	"log"

	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/js"
	"louis14/pkg/layout"
	"louis14/pkg/render"
	"louis14/pkg/text"
)

// Renderer renders HTML content onto an image.
type Renderer interface {
	Render(htmlContent string, target *image.RGBA) error
}

// Louis14Renderer renders HTML using the louis14 layout and rendering engine.
type Louis14Renderer struct {
	fetcher  Fetcher
	fonts    text.FontConfig
	jsEngine *js.Engine // nil = skip JS execution
}

// SetJSEngine configures a JavaScript engine for DOM manipulation.
// When set, the renderer performs a two-pass render: first pass renders
// the initial state, then JS executes and mutates the DOM, then a
// second layout+render pass produces the final output.
func (r *Louis14Renderer) SetJSEngine(engine *js.Engine) {
	r.jsEngine = engine
}

// NewLouis14Renderer creates a new Louis14Renderer with the given fetcher and font paths.
// The fetcher is used to load external stylesheets and images.
// If fonts is nil or zero-value, the default bundled fonts are used.
func NewLouis14Renderer(fetcher Fetcher, fonts ...text.FontConfig) *Louis14Renderer {
	fc := text.DefaultFontConfig()
	if len(fonts) > 0 && fonts[0].Regular != "" {
		fc = fonts[0]
	}
	return &Louis14Renderer{fetcher: fetcher, fonts: fc}
}

// Render parses the HTML content, performs layout, and renders onto the target image.
// The viewport width and height are derived from the target image dimensions.
func (r *Louis14Renderer) Render(htmlContent string, target *image.RGBA) error {
	bounds := target.Bounds()
	viewportWidth := float64(bounds.Dx())
	viewportHeight := float64(bounds.Dy())

	// Build a CSS fetcher function from our Fetcher interface
	var cssFetcher html.CSSFetcher
	if r.fetcher != nil {
		cssFetcher = func(uri string) (string, error) {
			if df, ok := r.fetcher.(*DefaultFetcher); ok {
				return df.FetchCSS(uri)
			}
			body, _, err := r.fetcher.Fetch(uri)
			if err != nil {
				return "", err
			}
			return string(body), nil
		}
	}

	// Parse HTML with CSS fetcher
	doc, err := html.ParseWithFetcher(htmlContent, cssFetcher)
	if err != nil {
		return fmt.Errorf("parsing HTML: %w", err)
	}

	// Build an image fetcher function from our Fetcher interface
	var imageFetcher images.ImageFetcher
	if r.fetcher != nil {
		imageFetcher = func(uri string) ([]byte, error) {
			if df, ok := r.fetcher.(*DefaultFetcher); ok {
				return df.FetchImage(uri)
			}
			body, _, err := r.fetcher.Fetch(uri)
			if err != nil {
				return nil, err
			}
			return body, nil
		}
	}

	// Layout
	layoutEngine := layout.NewLayoutEngine(viewportWidth, viewportHeight)
	if imageFetcher != nil {
		layoutEngine.SetImageFetcher(imageFetcher)
	}
	boxes := layoutEngine.Layout(doc)

	// Render onto target image
	renderer := render.NewRendererForImage(target)
	renderer.SetFonts(r.fonts)
	if imageFetcher != nil {
		renderer.SetImageFetcher(imageFetcher)
	}
	renderer.Render(boxes)

	// Execute JavaScript if engine is configured
	if r.jsEngine != nil && len(doc.Scripts) > 0 {
		if err := r.jsEngine.Execute(doc); err != nil {
			log.Printf("js: %v", err)
		}

		// Second pass: re-layout and re-render with JS modifications
		layoutEngine2 := layout.NewLayoutEngine(viewportWidth, viewportHeight)
		if imageFetcher != nil {
			layoutEngine2.SetImageFetcher(imageFetcher)
		}
		boxes2 := layoutEngine2.Layout(doc)

		renderer2 := render.NewRendererForImage(target)
		renderer2.SetFonts(r.fonts)
		if imageFetcher != nil {
			renderer2.SetImageFetcher(imageFetcher)
		}
		renderer2.Render(boxes2)
	}

	return nil
}
