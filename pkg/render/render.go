package render

import (
	"sort"

	"github.com/fogleman/gg"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/text"
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

	// Phase 4: Collect all boxes into flat list and sort by z-index
	allBoxes := r.collectAllBoxes(boxes)
	r.sortByZIndex(allBoxes)

	// Render in z-index order
	for _, box := range allBoxes {
		r.drawBox(box)
	}
}

// collectAllBoxes flattens the box tree into a single list
func (r *Renderer) collectAllBoxes(boxes []*layout.Box) []*layout.Box {
	result := make([]*layout.Box, 0)
	for _, box := range boxes {
		result = append(result, box)
		result = append(result, r.collectAllBoxes(box.Children)...)
	}
	return result
}

// sortByZIndex sorts boxes by z-index (lower values rendered first)
func (r *Renderer) sortByZIndex(boxes []*layout.Box) {
	sort.SliceStable(boxes, func(i, j int) bool {
		// If z-index is the same, preserve document order (stable sort)
		return boxes[i].ZIndex < boxes[j].ZIndex
	})
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

	// Phase 8: Draw image
	r.drawImage(box)

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
	// Phase 6: Render text nodes properly
	if box.Node.Type == html.TextNode && box.Node.Text != "" {
		// Get text color from style
		color := box.Style.GetColor()
		r.context.SetRGB(
			float64(color.R)/255.0,
			float64(color.G)/255.0,
			float64(color.B)/255.0,
		)

		// Get font properties from style
		fontSize := box.Style.GetFontSize()
		fontWeight := box.Style.GetFontWeight()

		// Phase 6 Enhancement: Select font based on weight
		fontPath := text.DefaultFontPath
		if fontWeight == css.FontWeightBold {
			fontPath = text.BoldFontPath
		}

		// Load font
		if err := r.context.LoadFontFace(fontPath, fontSize); err != nil {
			// If font loading fails, skip rendering
			return
		}

		// Phase 6 Enhancement: Calculate X position based on text-align
		textX := box.X
		textAlign := box.Style.GetTextAlign()
		if textAlign == css.TextAlignCenter {
			textWidth, _ := r.context.MeasureString(box.Node.Text)
			textX = box.X + (box.Width-textWidth)/2
		} else if textAlign == css.TextAlignRight {
			textWidth, _ := r.context.MeasureString(box.Node.Text)
			textX = box.X + box.Width - textWidth
		}

		// Draw text at calculated position
		// Add fontSize to Y for baseline alignment
		r.context.DrawString(box.Node.Text, textX, box.Y+fontSize)
	}
}

// Phase 8: drawImage renders an image element
func (r *Renderer) drawImage(box *layout.Box) {
	if box.ImagePath == "" {
		return
	}

	// Load the image
	img, err := images.LoadImage(box.ImagePath)
	if err != nil {
		// Image failed to load, draw placeholder
		r.context.SetRGB(0.9, 0.9, 0.9) // Light gray background
		r.context.DrawRectangle(box.X, box.Y, box.Width, box.Height)
		r.context.Fill()

		// Draw X to indicate broken image
		r.context.SetRGB(0.5, 0.5, 0.5)
		r.context.SetLineWidth(2)
		r.context.DrawLine(box.X, box.Y, box.X+box.Width, box.Y+box.Height)
		r.context.DrawLine(box.X+box.Width, box.Y, box.X, box.Y+box.Height)
		r.context.Stroke()
		return
	}

	// Save current context state
	r.context.Push()

	// Translate to image position
	r.context.Translate(box.X, box.Y)

	// Calculate scale factors
	bounds := img.Bounds()
	imgWidth := float64(bounds.Dx())
	imgHeight := float64(bounds.Dy())
	scaleX := box.Width / imgWidth
	scaleY := box.Height / imgHeight

	// Scale to fit box dimensions
	r.context.Scale(scaleX, scaleY)

	// Draw the image at origin (already translated)
	r.context.DrawImage(img, 0, 0)

	// Restore context state
	r.context.Pop()
}

func (r *Renderer) SavePNG(filename string) error {
	return r.context.SavePNG(filename)
}
