package render

import (
	"image"
	"image/png"
	"math"
	"os"

	"github.com/fogleman/gg"
	"louis14/pkg/css"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/text"
)

// Renderer paints a tree of layout boxes onto an image.
type Renderer struct {
	context      *gg.Context
	target       *image.RGBA
	fonts        text.FontConfig
	imageFetcher images.ImageFetcher
	scrollY      float64
}

// NewRenderer creates a new renderer with a fresh image of the given dimensions.
func NewRenderer(width, height int) *Renderer {
	ctx := gg.NewContext(width, height)
	return &Renderer{
		context: ctx,
		target:  ctx.Image().(*image.RGBA),
		fonts:   text.DefaultFontConfig(),
	}
}

// NewRendererForImage creates a renderer that paints onto an existing image.
func NewRendererForImage(target *image.RGBA) *Renderer {
	ctx := gg.NewContextForRGBA(target)
	return &Renderer{
		context: ctx,
		target:  target,
		fonts:   text.DefaultFontConfig(),
	}
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
// Implements CSS 2.1 Appendix E paint order (simplified).
func (r *Renderer) Render(boxes []*layout.Box) {
	// Fill with white background.
	r.context.SetRGBA(1, 1, 1, 1)
	r.context.Clear()

	for _, box := range boxes {
		r.paintBox(box)
	}
}

// paintBox paints a single box and its children using CSS paint order.
func (r *Renderer) paintBox(box *layout.Box) {
	if box == nil {
		return
	}

	// Step 1: Paint this box's background and borders.
	r.drawBackground(box)
	r.drawBorders(box)

	// Paint text content.
	if box.Text != "" {
		r.drawText(box)
	}

	// Steps 2-7: Paint children (simplified — full stacking context later).
	for _, child := range box.Children {
		r.paintBox(child)
	}
}

// drawBackground paints the box's background color.
func (r *Renderer) drawBackground(box *layout.Box) {
	if box.Style == nil {
		return
	}

	bgStr, ok := box.Style.Get("background-color")
	if !ok {
		return
	}
	bgColor, ok := css.ParseColor(bgStr)
	if !ok || bgColor.A == 0 {
		return
	}

	r.context.SetRGBA(
		float64(bgColor.R)/255.0,
		float64(bgColor.G)/255.0,
		float64(bgColor.B)/255.0,
		bgColor.A,
	)
	r.context.DrawRectangle(box.X, box.Y, box.Width, box.Height)
	r.context.Fill()
}

// drawBorders draws all four borders of the box as trapezoids.
func (r *Renderer) drawBorders(box *layout.Box) {
	if box.Style == nil {
		return
	}

	bw := box.Border
	if bw.Top == 0 && bw.Right == 0 && bw.Bottom == 0 && bw.Left == 0 {
		return
	}

	x := box.X
	y := box.Y
	w := box.Width
	h := box.Height

	// Outer edges (border-box).
	outerLeft := x
	outerTop := y
	outerRight := x + w
	outerBottom := y + h

	// Inner edges (padding-box).
	innerLeft := math.Floor(x + bw.Left)
	innerTop := math.Floor(y + bw.Top)
	innerRight := math.Ceil(x + w - bw.Right - 1e-9)
	innerBottom := math.Ceil(y + h - bw.Bottom - 1e-9)

	borderStyles := box.Style.GetBorderStyle()

	// Top border.
	if bw.Top > 0 && borderStyles.Top != "none" && borderStyles.Top != "" {
		c := getBorderColor(box.Style, "border-top-color")
		if c.A > 0 {
			r.setColor(c)
			r.context.MoveTo(outerLeft, outerTop)
			r.context.LineTo(outerRight, outerTop)
			r.context.LineTo(innerRight, innerTop)
			r.context.LineTo(innerLeft, innerTop)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Right border.
	if bw.Right > 0 && borderStyles.Right != "none" && borderStyles.Right != "" {
		c := getBorderColor(box.Style, "border-right-color")
		if c.A > 0 {
			r.setColor(c)
			r.context.MoveTo(outerRight, outerTop)
			r.context.LineTo(outerRight, outerBottom)
			r.context.LineTo(innerRight, innerBottom)
			r.context.LineTo(innerRight, innerTop)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Bottom border.
	if bw.Bottom > 0 && borderStyles.Bottom != "none" && borderStyles.Bottom != "" {
		c := getBorderColor(box.Style, "border-bottom-color")
		if c.A > 0 {
			r.setColor(c)
			r.context.MoveTo(outerLeft, outerBottom)
			r.context.LineTo(innerLeft, innerBottom)
			r.context.LineTo(innerRight, innerBottom)
			r.context.LineTo(outerRight, outerBottom)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Left border.
	if bw.Left > 0 && borderStyles.Left != "none" && borderStyles.Left != "" {
		c := getBorderColor(box.Style, "border-left-color")
		if c.A > 0 {
			r.setColor(c)
			r.context.MoveTo(outerLeft, outerTop)
			r.context.LineTo(innerLeft, innerTop)
			r.context.LineTo(innerLeft, innerBottom)
			r.context.LineTo(outerLeft, outerBottom)
			r.context.ClosePath()
			r.context.Fill()
		}
	}
}

// setColor sets the gg context color from a css.Color.
func (r *Renderer) setColor(c css.Color) {
	r.context.SetRGBA(
		float64(c.R)/255.0,
		float64(c.G)/255.0,
		float64(c.B)/255.0,
		c.A,
	)
}

// getBorderColor returns the color for a border side property.
func getBorderColor(style *css.Style, prop string) css.Color {
	if val, ok := style.Get(prop); ok {
		if c, ok := css.ParseColor(val); ok {
			return c
		}
	}
	// Default border color is the element's color (currentColor).
	if val, ok := style.Get("color"); ok {
		if c, ok := css.ParseColor(val); ok {
			return c
		}
	}
	return css.Color{R: 0, G: 0, B: 0, A: 1.0} // Default: black.
}

// drawText paints text content within a box.
func (r *Renderer) drawText(box *layout.Box) {
	if box.Style == nil {
		return
	}

	fontSize := box.Style.GetFontSize()
	if fontSize <= 0 {
		fontSize = 16
	}
	bold := box.Style.GetFontWeight() == css.FontWeightBold
	italic := box.Style.GetFontStyle() == css.FontStyleItalic
	mono := box.Style.IsMonospaceFamily()
	ahem := box.Style.IsAhemFamily()

	fontPath := r.fonts.FontPath(bold, italic, mono, ahem)
	if err := r.context.LoadFontFace(fontPath, fontSize); err != nil {
		return
	}

	// Text color (default: black).
	c := css.Color{R: 0, G: 0, B: 0, A: 1.0}
	if val, ok := box.Style.Get("color"); ok {
		if pc, ok := css.ParseColor(val); ok {
			c = pc
		}
	}
	r.setColor(c)

	// Draw text at the baseline. The box's Y is the top of the text;
	// adding ascent gives the baseline position for DrawString.
	ascent := text.FontAscent(fontSize, bold, italic, mono, ahem)
	r.context.DrawString(box.Text, box.X, box.Y+ascent)
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
