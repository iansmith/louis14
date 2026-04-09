package resource

import (
	"image"
	"log"
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
// and no resource fetcher (no network access).
func NewWebEngine() *WebEngine {
	return &WebEngine{
		renderer: NewLouis14Renderer(nil),
	}
}

// Render takes raw HTML bytes and a viewport size, renders the content,
// and returns the resulting image. The HTML may be a fragment — it is
// wrapped in a minimal document structure if needed by the parser.
func (e *WebEngine) Render(html []byte, width, height int) *image.RGBA {
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	if err := e.renderer.Render(string(html), target); err != nil {
		log.Printf("WebEngine.Render: %v", err)
	}
	return target
}
