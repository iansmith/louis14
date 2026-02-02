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
	// Phase 16: Apply CSS transforms
	transforms := box.Style.GetTransforms()
	if len(transforms) > 0 {
		r.context.Push() // Save graphics state
		r.applyTransforms(box, transforms)
		defer r.context.Pop() // Restore graphics state after drawing
	}

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

			// Phase 12: Check for border-radius
			borderRadius := box.Style.GetBorderRadius()
			if borderRadius > 0 {
				r.context.DrawRoundedRectangle(bgX, bgY, bgWidth, bgHeight, borderRadius)
			} else {
				r.context.DrawRectangle(bgX, bgY, bgWidth, bgHeight)
			}
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
	if !hasBorderColor {
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

	// Phase 12: Get border styles for each side
	borderStyles := box.Style.GetBorderStyle()

	// Phase 12: Check for border-radius
	borderRadius := box.Style.GetBorderRadius()

	// If border-radius is set and all borders are solid, use rounded rectangle
	if borderRadius > 0 &&
		borderStyles.Top == css.BorderStyleSolid &&
		borderStyles.Right == css.BorderStyleSolid &&
		borderStyles.Bottom == css.BorderStyleSolid &&
		borderStyles.Left == css.BorderStyleSolid &&
		box.Border.Top == box.Border.Right &&
		box.Border.Right == box.Border.Bottom &&
		box.Border.Bottom == box.Border.Left {
		// Draw rounded border (simplified - same width all around)
		r.context.SetLineWidth(box.Border.Top)
		borderX := box.X - box.Border.Left/2
		borderY := box.Y - box.Border.Top/2
		borderWidth := box.Width + box.Padding.Left + box.Padding.Right + box.Border.Left
		borderHeight := box.Height + box.Padding.Top + box.Padding.Bottom + box.Border.Top
		r.context.DrawRoundedRectangle(borderX, borderY, borderWidth, borderHeight, borderRadius)
		r.context.Stroke()
		return
	}

	// Draw each side with its specific style
	// Top border
	if box.Border.Top > 0 && borderStyles.Top != css.BorderStyleNone {
		r.drawBorderSide(
			box.X,
			box.Y-box.Border.Top,
			box.Width+box.Padding.Left+box.Padding.Right,
			box.Border.Top,
			borderStyles.Top,
			true, // horizontal
		)
	}

	// Right border
	if box.Border.Right > 0 && borderStyles.Right != css.BorderStyleNone {
		r.drawBorderSide(
			box.X+box.Width+box.Padding.Left+box.Padding.Right,
			box.Y-box.Border.Top,
			box.Border.Right,
			box.Height+box.Padding.Top+box.Padding.Bottom+box.Border.Top+box.Border.Bottom,
			borderStyles.Right,
			false, // vertical
		)
	}

	// Bottom border
	if box.Border.Bottom > 0 && borderStyles.Bottom != css.BorderStyleNone {
		r.drawBorderSide(
			box.X,
			box.Y+box.Height+box.Padding.Top+box.Padding.Bottom,
			box.Width+box.Padding.Left+box.Padding.Right,
			box.Border.Bottom,
			borderStyles.Bottom,
			true, // horizontal
		)
	}

	// Left border
	if box.Border.Left > 0 && borderStyles.Left != css.BorderStyleNone {
		r.drawBorderSide(
			box.X-box.Border.Left,
			box.Y-box.Border.Top,
			box.Border.Left,
			box.Height+box.Padding.Top+box.Padding.Bottom+box.Border.Top+box.Border.Bottom,
			borderStyles.Left,
			false, // vertical
		)
	}
}

// Phase 12: drawBorderSide draws a single border side with a specific style
func (r *Renderer) drawBorderSide(x, y, width, height float64, style css.BorderStyle, horizontal bool) {
	switch style {
	case css.BorderStyleSolid:
		// Solid border - filled rectangle
		r.context.DrawRectangle(x, y, width, height)
		r.context.Fill()

	case css.BorderStyleDashed:
		// Dashed border - series of dashes
		r.context.SetLineWidth(height)
		if horizontal {
			r.context.SetDash(10, 5)
			r.context.DrawLine(x, y+height/2, x+width, y+height/2)
		} else {
			r.context.SetDash(10, 5)
			r.context.DrawLine(x+width/2, y, x+width/2, y+height)
		}
		r.context.Stroke()
		r.context.SetDash() // Reset dash

	case css.BorderStyleDotted:
		// Dotted border - series of dots
		r.context.SetLineWidth(height)
		if horizontal {
			r.context.SetDash(2, 4)
			r.context.DrawLine(x, y+height/2, x+width, y+height/2)
		} else {
			r.context.SetDash(2, 4)
			r.context.DrawLine(x+width/2, y, x+width/2, y+height)
		}
		r.context.Stroke()
		r.context.SetDash() // Reset dash

	case css.BorderStyleDouble:
		// Double border - two parallel lines
		if horizontal {
			spacing := height / 3
			r.context.DrawRectangle(x, y, width, spacing)
			r.context.Fill()
			r.context.DrawRectangle(x, y+height-spacing, width, spacing)
			r.context.Fill()
		} else {
			spacing := width / 3
			r.context.DrawRectangle(x, y, spacing, height)
			r.context.Fill()
			r.context.DrawRectangle(x+width-spacing, y, spacing, height)
			r.context.Fill()
		}
	}
}

func (r *Renderer) drawText(box *layout.Box) {
	// Phase 11: Also handle pseudo-element content
	textContent := ""
	if box.Node.Type == html.TextNode && box.Node.Text != "" {
		textContent = box.Node.Text
	} else if box.PseudoContent != "" {
		textContent = box.PseudoContent
	}

	if textContent == "" {
		return
	}

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
		textWidth, _ := r.context.MeasureString(textContent)
		textX = box.X + (box.Width-textWidth)/2
	} else if textAlign == css.TextAlignRight {
		textWidth, _ := r.context.MeasureString(textContent)
		textX = box.X + box.Width - textWidth
	}

	// Draw text at calculated position
	// Add fontSize to Y for baseline alignment
	r.context.DrawString(textContent, textX, box.Y+fontSize)
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

// Phase 16: applyTransforms applies CSS transforms to the graphics context
func (r *Renderer) applyTransforms(box *layout.Box, transforms []css.Transform) {
	// Get transform origin
	origin := box.Style.GetTransformOrigin()
	
	// Calculate origin point in absolute coordinates
	originX := box.X + box.Padding.Left + origin.X*box.Width
	originY := box.Y + box.Padding.Top + origin.Y*box.Height
	
	// Translate to origin point
	r.context.Translate(originX, originY)
	
	// Apply each transform in order
	for _, transform := range transforms {
		switch transform.Type {
		case "translate":
			if len(transform.Values) >= 2 {
				tx := transform.Values[0]
				ty := transform.Values[1]
				
				// Handle percentage values (negative values indicate percentage)
				if tx < 0 {
					tx = (-tx / 100.0) * box.Width
				}
				if ty < 0 {
					ty = (-ty / 100.0) * box.Height
				}
				
				r.context.Translate(tx, ty)
			}
			
		case "rotate":
			if len(transform.Values) >= 1 {
				// Convert degrees to radians
				radians := transform.Values[0] * 3.14159265359 / 180.0
				r.context.Rotate(radians)
			}
			
		case "scale":
			if len(transform.Values) >= 2 {
				r.context.Scale(transform.Values[0], transform.Values[1])
			}
		}
	}
	
	// Translate back from origin
	r.context.Translate(-originX, -originY)
}
