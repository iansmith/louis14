package render

import (
	"image"
	"image/color"
	"image/png"
	"os"

	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/text"
)

// Renderer paints a tree of layout boxes onto an image.
type Renderer struct {
	target *image.RGBA
	fonts  text.FontConfig
	// imageFetcher is used to load network images during painting.
	imageFetcher images.ImageFetcher
	scrollY      float64
}

// NewRenderer creates a new renderer with a fresh image of the given dimensions.
func NewRenderer(width, height int) *Renderer {
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	return &Renderer{target: target}
}

// NewRendererForImage creates a renderer that paints onto an existing image.
func NewRendererForImage(target *image.RGBA) *Renderer {
	return &Renderer{target: target}
}

// SetFonts configures font paths for text rendering.
func (r *Renderer) SetFonts(fonts text.FontConfig) {
	r.fonts = fonts
}

// SetImageFetcher sets the image fetcher for loading network images during painting.
func (r *Renderer) SetImageFetcher(fetcher images.ImageFetcher) {
	r.imageFetcher = fetcher
}

// SetScrollY sets the vertical scroll offset.
func (r *Renderer) SetScrollY(scrollY float64) {
	r.scrollY = scrollY
}

// Render paints the box tree onto the image.
//
// STUB: Fills the image with white.
// This will be replaced with the real CSS Appendix E paint-order walker.
func (r *Renderer) Render(boxes []*layout.Box) {
	bounds := r.target.Bounds()
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r.target.SetRGBA(x, y, white)
		}
	}
}

// SavePNG writes the rendered image to a PNG file.
func (r *Renderer) SavePNG(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, r.target)
}
