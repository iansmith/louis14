package resource

import (
	"image"
	"log"

	"mazarin/textshape"
)

// WebEngine wraps a [Louis14Renderer] to satisfy the WebRenderEngine
// interface defined in mancini/std. It renders HTML fragments (which
// may not be well-formed) into an RGBA image of the requested size.
//
// No network fetching is performed — the HTML must be self-contained
// with no remote references.
type WebEngine struct {
	renderer *Louis14Renderer
}

// NewWebEngine creates a WebEngine using the default font configuration
// and no resource fetcher (no network access). Uses the default
// DirectGlyphProvider which loads fonts from the local filesystem.
func NewWebEngine() *WebEngine {
	return &WebEngine{
		renderer: NewLouis14Renderer(nil),
	}
}

// NewWebEngineWithProvider creates a WebEngine that uses the supplied
// GlyphProvider for font rasterization instead of loading fonts from
// the local filesystem. Use this in environments where fonts are
// served via IPC (e.g., mazzy's fontsvc).
func NewWebEngineWithProvider(provider textshape.GlyphProvider) *WebEngine {
	r := NewLouis14Renderer(nil)
	r.SetGlyphProvider(provider)
	return &WebEngine{renderer: r}
}

// RenderDC renders raw HTML bytes using the provided DrawContext.
// The DC's translation and clipping define the render area.
// viewportW and viewportH specify the layout dimensions.
//
// Returns a non-nil error if the underlying renderer could not produce
// output for this HTML (e.g. tokenizer rejected the markup); callers are
// expected to paint their own placeholder so the user gets visible
// feedback instead of stale pixels from a previous render.
func (e *WebEngine) RenderDC(html []byte, dc textshape.DrawContext, viewportW, viewportH float64) error {
	if err := e.renderer.RenderWithDC(string(html), dc, viewportW, viewportH); err != nil {
		log.Printf("WebEngine.RenderDC: %v", err)
		return err
	}
	return nil
}

// RenderToImage lays the HTML out at the given fixed width and renders
// into a fresh RGBA buffer whose height is the natural content height
// (no clipping). Wraps the renderer's RenderAutoHeight, which performs
// the layout-then-render-tall pass so callers can cache the result and
// blit a viewport from it on subsequent draws — saving a full layout
// pipeline per scroll or redraw frame.
//
// Returns a non-nil error and a nil image on parser/layout failure.
func (e *WebEngine) RenderToImage(html []byte, width int) (*image.RGBA, error) {
	img, err := e.renderer.RenderAutoHeight(string(html), width)
	if err != nil {
		log.Printf("WebEngine.RenderToImage: %v", err)
		return nil, err
	}
	return img, nil
}
