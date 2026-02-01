package render

import (
	"github.com/fogleman/gg"
	"louis14/pkg/css"
	"louis14/pkg/layout"
)

type Renderer struct {
	context *gg.Context
}

func NewRenderer(width, height int) *Renderer {
	return &Renderer{context: gg.NewContext(width, height)}
}

func (r *Renderer) Render(boxes []*layout.Box) {
	r.context.SetRGB(1, 1, 1)
	r.context.Clear()
	for _, box := range boxes {
		r.drawBox(box)
		// Phase 2: Recursively render children
		r.renderBoxTree(box)
	}
}

// renderBoxTree recursively renders a box and its children
func (r *Renderer) renderBoxTree(box *layout.Box) {
	for _, child := range box.Children {
		r.drawBox(child)
		r.renderBoxTree(child)
	}
}

func (r *Renderer) drawBox(box *layout.Box) {
	// Phase 2: Draw background (content + padding area, not including margin)
	if bgColor, ok := box.Style.Get("background-color"); ok {
		if color, ok := css.ParseColor(bgColor); ok {
			r.context.SetRGB(
				float64(color.R)/255.0,
				float64(color.G)/255.0,
				float64(color.B)/255.0,
			)

			// Background covers content + padding (but not margin or border)
			bgX := box.X
			bgY := box.Y
			bgWidth := box.Width + box.Padding.Left + box.Padding.Right
			bgHeight := box.Height + box.Padding.Top + box.Padding.Bottom

			r.context.DrawRectangle(bgX, bgY, bgWidth, bgHeight)
			r.context.Fill()
		}
	}

	// Phase 2: Draw border
	r.drawBorder(box)

	// Draw text
	r.drawText(box)
}

// drawBorder draws the border around a box
func (r *Renderer) drawBorder(box *layout.Box) {
	// Check if border is specified
	borderColor, hasBorderColor := box.Style.Get("border-color")
	borderStyle, _ := box.Style.Get("border-style")

	if !hasBorderColor || borderStyle == "" {
		return
	}

	// Parse border color
	color, ok := css.ParseColor(borderColor)
	if !ok {
		color = css.Color{0, 0, 0} // Default to black
	}

	r.context.SetRGB(
		float64(color.R)/255.0,
		float64(color.G)/255.0,
		float64(color.B)/255.0,
	)

	// Border is drawn outside the background area
	// For simplicity in Phase 2, draw as solid rectangles for each side
	if borderStyle == "solid" {
		// Top border
		if box.Border.Top > 0 {
			r.context.DrawRectangle(
				box.X,
				box.Y-box.Border.Top,
				box.Width+box.Padding.Left+box.Padding.Right,
				box.Border.Top,
			)
			r.context.Fill()
		}

		// Right border
		if box.Border.Right > 0 {
			r.context.DrawRectangle(
				box.X+box.Width+box.Padding.Left+box.Padding.Right,
				box.Y-box.Border.Top,
				box.Border.Right,
				box.Height+box.Padding.Top+box.Padding.Bottom+box.Border.Top+box.Border.Bottom,
			)
			r.context.Fill()
		}

		// Bottom border
		if box.Border.Bottom > 0 {
			r.context.DrawRectangle(
				box.X,
				box.Y+box.Height+box.Padding.Top+box.Padding.Bottom,
				box.Width+box.Padding.Left+box.Padding.Right,
				box.Border.Bottom,
			)
			r.context.Fill()
		}

		// Left border
		if box.Border.Left > 0 {
			r.context.DrawRectangle(
				box.X-box.Border.Left,
				box.Y-box.Border.Top,
				box.Border.Left,
				box.Height+box.Padding.Top+box.Padding.Bottom+box.Border.Top+box.Border.Bottom,
			)
			r.context.Fill()
		}
	}
}

func (r *Renderer) drawText(box *layout.Box) {
	if box.Node.Type != 0 {
		text := box.Node.TagName
		r.context.SetRGB(0, 0, 0)
		fontSize := 12.0
		r.context.LoadFontFace("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", fontSize)
		textX := box.X + 10
		textY := box.Y + 20
		r.context.DrawString(text, textX, textY)
	}
}

func (r *Renderer) SavePNG(filename string) error {
	return r.context.SavePNG(filename)
}
