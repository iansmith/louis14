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
	"mazarin/textshape"
)

// Renderer renders HTML content onto an image.
type Renderer interface {
	Render(htmlContent string, target *image.RGBA) error
}

// Louis14Renderer renders HTML using the louis14 layout and rendering engine.
type Louis14Renderer struct {
	fetcher       Fetcher
	fonts         text.FontConfig
	jsEngine      *js.Engine              // nil = skip JS execution
	glyphProvider textshape.GlyphProvider // nil = use default DirectGlyphProvider
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

// SetGlyphProvider configures a custom GlyphProvider for font rasterization.
// When set, the renderer uses this provider instead of the default
// DirectGlyphProvider (which requires font files on the local filesystem).
func (r *Louis14Renderer) SetGlyphProvider(provider textshape.GlyphProvider) {
	r.glyphProvider = provider
}

// newRenderer creates a render.Renderer for the given target image,
// using the custom GlyphProvider if one was configured.
func (r *Louis14Renderer) newRenderer(target *image.RGBA) *render.Renderer {
	if r.glyphProvider != nil {
		return render.NewRendererForImageWithProvider(target, r.glyphProvider)
	}
	return render.NewRendererForImage(target)
}

// newRendererForDC creates a render.Renderer using an existing DrawContext.
// The DC's image, translation, and provider are used directly.
func (r *Louis14Renderer) newRendererForDC(dc textshape.DrawContext) *render.Renderer {
	return render.NewRendererForDrawContext(dc)
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

	// Collect @counter-style rules
	counterStyles := collectCounterStyles(doc, viewportWidth, viewportHeight)

	// Create image at the measured height and render
	target := image.NewRGBA(image.Rect(0, 0, width, int(contentHeight+0.5)))
	renderer := r.newRenderer(target)
	renderer.SetFonts(fonts)
	if imageFetcher != nil {
		renderer.SetImageFetcher(imageFetcher)
	}
	if len(counterStyles) > 0 {
		renderer.SetCounterStyles(counterStyles)
	}
	renderer.Render(boxes)

	return target, nil
}

// measureContentHeight walks the box tree and returns the maximum Y extent.
// For html and body roots we ALWAYS use the children's extent rather than
// the box's own bottom. The layout engine inflates body's height to fill
// the (deliberately huge) viewport when no explicit height is set, which
// would make the auto-height render produce a viewport-tall image of mostly
// blank space. Trusting only the children gives us the true content extent.
func measureContentHeight(boxes []*layout.Box) float64 {
	maxY := 0.0
	for _, box := range boxes {
		if isAutoHeightRoot(box) {
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

// isAutoHeightRoot returns true for html and body elements. These are
// treated as auto-height containers when measuring content extent for
// auto-height rendering — the layout engine inflates them to the
// viewport height (which is intentionally large to allow vh units to
// resolve), and using their bottom would yield a viewport-sized image
// of mostly blank space instead of just the actual content.
func isAutoHeightRoot(box *layout.Box) bool {
	if box.Node == nil {
		return false
	}
	tag := box.Node.TagName
	return tag == "html" || tag == "body"
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
	for _, stylesheet := range css.ParseDocumentStylesheets(doc) {
		allFaces = append(allFaces, stylesheet.FontFaces...)
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
		if face.Src == nil {
			continue
		}
		if _, err := registry.RegisterFontFace(face.Family, face.Src.Data.Absolute, face.Format, face.Weight, face.Style, fontFetcher); err != nil {
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

	// Collect @counter-style rules
	counterStyles := collectCounterStyles(doc, viewportWidth, viewportHeight)

	// Render onto target image
	renderer := r.newRenderer(target)
	renderer.SetFonts(fonts)
	if imageFetcher != nil {
		renderer.SetImageFetcher(imageFetcher)
	}
	if len(counterStyles) > 0 {
		renderer.SetCounterStyles(counterStyles)
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
		if len(counterStyles) > 0 {
			renderer2.SetCounterStyles(counterStyles)
		}
		renderer2.Render(boxes2)
	}

	return nil
}

// RenderWithDC renders HTML content using the provided DrawContext.
// The DC's current translation and clipping define the viewport.
// viewportW and viewportH specify the layout viewport size in pixels.
func (r *Louis14Renderer) RenderWithDC(htmlContent string, dc textshape.DrawContext, viewportW, viewportH float64) error {
	cssFetcher, imageFetcher := r.buildFetchers()

	doc, err := html.ParseWithFetcher(htmlContent, cssFetcher)
	if err != nil {
		return fmt.Errorf("parsing HTML: %w", err)
	}

	layoutEngine := layout.NewLayoutEngine(viewportW, viewportH)
	if imageFetcher != nil {
		layoutEngine.SetImageFetcher(imageFetcher)
	}
	boxes := layoutEngine.Layout(doc)

	fonts := r.registerWebFonts(doc)
	counterStyles := collectCounterStyles(doc, viewportW, viewportH)

	renderer := r.newRendererForDC(dc)
	renderer.SetFonts(fonts)
	if imageFetcher != nil {
		renderer.SetImageFetcher(imageFetcher)
	}
	if len(counterStyles) > 0 {
		renderer.SetCounterStyles(counterStyles)
	}
	// Use RenderEmbedded so the canvas-background fill is scoped to the
	// caller-supplied viewport rect rather than calling dc.Clear() (which
	// would blank the entire host DrawContext, including sibling UI).
	renderer.RenderEmbedded(boxes, viewportW, viewportH)

	return nil
}

// collectCounterStyles extracts all @counter-style rules from a
// document's stylesheets, filtering out rules whose enclosing @media
// query doesn't match the current viewport.
func collectCounterStyles(doc *html.Document, viewportWidth, viewportHeight float64) []css.CounterStyleRule {
	return css.FilterCounterStylesByMedia(css.ParseDocumentStylesheets(doc), viewportWidth, viewportHeight)
}
