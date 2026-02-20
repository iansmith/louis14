package resource

import (
	"fmt"
	"image"
	"log"

	"louis14/pkg/css"
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

// RenderAutoHeight performs layout to measure content height, then renders at full height.
// Returns the rendered image sized to the full content.
func (r *Louis14Renderer) RenderAutoHeight(htmlContent string, width int) (*image.RGBA, error) {
	viewportWidth := float64(width)
	// Use a large initial height for layout (doesn't clip, just sets viewport for vh units)
	viewportHeight := 10000.0

	cssFetcher, imageFetcher := r.buildFetchers()

	doc, err := html.ParseWithFetcher(htmlContent, cssFetcher)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	// Override viewport width from <meta name="viewport" content="width=...">
	if doc.ViewportWidth > 0 {
		viewportWidth = float64(doc.ViewportWidth)
	}

	// Layout pass to measure content height
	layoutEngine := layout.NewLayoutEngine(viewportWidth, viewportHeight)
	if imageFetcher != nil {
		layoutEngine.SetImageFetcher(imageFetcher)
	}
	boxes := layoutEngine.Layout(doc)

	// Execute JavaScript if engine is configured
	if r.jsEngine != nil && len(doc.Scripts) > 0 {
		if err := r.jsEngine.Execute(doc); err != nil {
			log.Printf("js: %v", err)
		}
		layoutEngine2 := layout.NewLayoutEngine(viewportWidth, viewportHeight)
		if imageFetcher != nil {
			layoutEngine2.SetImageFetcher(imageFetcher)
		}
		boxes = layoutEngine2.Layout(doc)
	}

	// Measure content height from box tree
	contentHeight := measureContentHeight(boxes)
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Register @font-face web fonts
	fonts := r.registerWebFonts(doc)

	// Create image at the measured height and render
	target := image.NewRGBA(image.Rect(0, 0, width, int(contentHeight+0.5)))
	renderer := render.NewRendererForImage(target)
	renderer.SetFonts(fonts)
	if imageFetcher != nil {
		renderer.SetImageFetcher(imageFetcher)
	}
	renderer.Render(boxes)

	return target, nil
}

// measureContentHeight walks the box tree and returns the maximum Y extent.
func measureContentHeight(boxes []*layout.Box) float64 {
	maxY := 0.0
	for _, box := range boxes {
		// For html/body with percentage heights, use children extent
		// instead of the inflated explicit height (which resolves against
		// the large initial viewport in auto-height mode)
		if isPercentageHeightRoot(box) {
			if childMax := measureContentHeight(box.Children); childMax > maxY {
				maxY = childMax
			}
			continue
		}

		bottom := box.Y + box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom + box.Margin.Bottom
		if bottom > maxY {
			maxY = bottom
		}
		if childMax := measureContentHeight(box.Children); childMax > maxY {
			maxY = childMax
		}
	}
	return maxY
}

// isPercentageHeightRoot returns true for html/body elements with percentage heights.
// These elements inflate to the viewport height during layout, but for auto-height
// rendering we want the actual content extent instead.
func isPercentageHeightRoot(box *layout.Box) bool {
	if box.Node == nil || box.Style == nil {
		return false
	}
	tag := box.Node.TagName
	if tag != "html" && tag != "body" {
		return false
	}
	_, hasPct := box.Style.GetPercentage("height")
	return hasPct
}

// buildFetchers creates CSS and image fetcher functions from the Fetcher interface.
func (r *Louis14Renderer) buildFetchers() (html.CSSFetcher, images.ImageFetcher) {
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

	return cssFetcher, imageFetcher
}

// registerWebFonts parses @font-face rules from document stylesheets and
// fetches/caches the font files. Returns an updated FontConfig with the registry.
func (r *Louis14Renderer) registerWebFonts(doc *html.Document) text.FontConfig {
	fc := r.fonts
	if r.fetcher == nil {
		return fc
	}

	// Collect all @font-face rules
	var allFaces []css.FontFaceRule
	for _, cssText := range doc.Stylesheets {
		if stylesheet, err := css.ParseStylesheet(cssText); err == nil {
			allFaces = append(allFaces, stylesheet.FontFaces...)
		}
	}

	if len(allFaces) == 0 {
		return fc
	}

	// Create registry and fetch fonts
	registry := text.NewFontRegistry()
	fontFetcher := func(uri string) ([]byte, error) {
		body, _, err := r.fetcher.Fetch(uri)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	for _, face := range allFaces {
		if _, err := registry.RegisterFontFace(face.Family, face.Src, face.Format, face.Weight, face.Style, fontFetcher); err != nil {
			log.Printf("font-face: %v", err)
		}
	}

	fc.Registry = registry
	return fc
}

// Render parses the HTML content, performs layout, and renders onto the target image.
// The viewport width and height are derived from the target image dimensions.
func (r *Louis14Renderer) Render(htmlContent string, target *image.RGBA) error {
	bounds := target.Bounds()
	viewportWidth := float64(bounds.Dx())
	viewportHeight := float64(bounds.Dy())

	cssFetcher, imageFetcher := r.buildFetchers()

	doc, err := html.ParseWithFetcher(htmlContent, cssFetcher)
	if err != nil {
		return fmt.Errorf("parsing HTML: %w", err)
	}

	// Layout
	layoutEngine := layout.NewLayoutEngine(viewportWidth, viewportHeight)
	if imageFetcher != nil {
		layoutEngine.SetImageFetcher(imageFetcher)
	}
	boxes := layoutEngine.Layout(doc)

	// Register @font-face web fonts
	fonts := r.registerWebFonts(doc)

	// Render onto target image
	renderer := render.NewRendererForImage(target)
	renderer.SetFonts(fonts)
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
		renderer2.SetFonts(fonts)
		if imageFetcher != nil {
			renderer2.SetImageFetcher(imageFetcher)
		}
		renderer2.Render(boxes2)
	}

	return nil
}
