package render

import (
	"sort"
	"strings"

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

// paintLevel returns the CSS painting level for stacking order within the same z-index:
// 0 = blocks, 1 = floats, 2 = inline content (CSS 2.1 Appendix E)
func paintLevel(box *layout.Box) int {
	if box.Style == nil {
		return 0
	}
	if box.Style.GetFloat() != css.FloatNone {
		return 1
	}
	if disp, ok := box.Style.Get("display"); ok && disp == "inline" {
		return 2
	}
	return 0
}

// sortByZIndex sorts boxes by z-index and CSS painting order
func (r *Renderer) sortByZIndex(boxes []*layout.Box) {
	sort.SliceStable(boxes, func(i, j int) bool {
		if boxes[i].ZIndex != boxes[j].ZIndex {
			return boxes[i].ZIndex < boxes[j].ZIndex
		}
		// Within same z-index: blocks first, then floats, then inline
		return paintLevel(boxes[i]) < paintLevel(boxes[j])
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

	// Phase 19: Apply opacity (wraps all drawing for this box)
	opacity := box.Style.GetOpacity()
	if opacity < 1.0 {
		r.context.Push()
		defer r.context.Pop()
		// Note: gg doesn't have direct opacity support, we'll simulate with alpha in colors
	}

	// Phase 19: Draw box-shadow (drawn first, underneath the box)
	r.drawBoxShadow(box)

	// Phase 2: Draw background (content + padding area, not including margin)
	if bgColor, ok := box.Style.Get("background-color"); ok {
		if color, ok := css.ParseColor(bgColor); ok && color.A > 0 {
			r.context.SetRGBA(
				float64(color.R)/255.0,
				float64(color.G)/255.0,
				float64(color.B)/255.0,
				color.A,
			)

			// Background covers content + padding (but not margin or border)
			bgX := box.X
			bgY := box.Y
			bgWidth := box.Width + box.Padding.Left + box.Padding.Right
			bgHeight := box.Height + box.Padding.Top + box.Padding.Bottom

			if bgWidth > 0 && bgHeight > 0 {
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
	}

	// Phase 24: Draw background image
	r.drawBackgroundImage(box)

	// Phase 2: Draw border
	r.drawBorder(box)

	// Phase 21: overflow clipping
	// Note: gg's Clip() is permanent (not restored by Pop), so we track overflow
	// constraints and apply them manually rather than using context clipping.
	overflow := box.Style.GetOverflow()

	// Phase 8: Draw image
	r.drawImage(box)

	// Draw text
	r.drawText(box)

	// Phase 21: Draw scrollbar indicators for overflow: scroll or auto
	if overflow == css.OverflowScroll || overflow == css.OverflowAuto {
		r.drawScrollbarIndicators(box)
	}
}

// getBorderSideColor returns the color for a specific border side
func (r *Renderer) getBorderSideColor(box *layout.Box, side string) (css.Color, bool) {
	// Check per-side color first
	if colorStr, ok := box.Style.Get("border-" + side + "-color"); ok {
		if color, ok := css.ParseColor(colorStr); ok {
			return color, true
		}
	}
	// Fall back to global border-color
	if colorStr, ok := box.Style.Get("border-color"); ok {
		if color, ok := css.ParseColor(colorStr); ok {
			return color, true
		}
	}
	// Fall back to element's color property (CSS spec: border-color defaults to currentColor)
	if colorStr, ok := box.Style.Get("color"); ok {
		if color, ok := css.ParseColor(colorStr); ok {
			return color, true
		}
	}
	return css.Color{0, 0, 0, 1.0}, true
}

// drawBorder draws the border around a box
func (r *Renderer) drawBorder(box *layout.Box) {
	// Check if any border exists (has width > 0 and a style)
	if box.Border.Top <= 0 && box.Border.Right <= 0 && box.Border.Bottom <= 0 && box.Border.Left <= 0 {
		return
	}

	// Phase 12: Get border styles for each side
	borderStyles := box.Style.GetBorderStyle()

	// Phase 12: Check for border-radius
	borderRadius := box.Style.GetBorderRadius()

	// If border-radius is set and all borders are solid, use rounded rectangle
	if borderRadius > 0 {
		color, _ := r.getBorderSideColor(box, "top")
		if color.A > 0 {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.SetLineWidth(box.Border.Top)
			borderX := box.X - box.Border.Left/2
			borderY := box.Y - box.Border.Top/2
			borderWidth := box.Width + box.Padding.Left + box.Padding.Right + box.Border.Left
			borderHeight := box.Height + box.Padding.Top + box.Padding.Bottom + box.Border.Top
			r.context.DrawRoundedRectangle(borderX, borderY, borderWidth, borderHeight, borderRadius)
			r.context.Stroke()
		}
		return
	}

	// Calculate border box coordinates
	// box.X, box.Y is the padding edge (content + padding area start)
	outerLeft := box.X - box.Border.Left
	outerTop := box.Y - box.Border.Top
	outerRight := box.X + box.Width + box.Padding.Left + box.Padding.Right + box.Border.Right
	outerBottom := box.Y + box.Height + box.Padding.Top + box.Padding.Bottom + box.Border.Bottom
	innerLeft := box.X
	innerTop := box.Y
	innerRight := box.X + box.Width + box.Padding.Left + box.Padding.Right
	innerBottom := box.Y + box.Height + box.Padding.Top + box.Padding.Bottom

	// Draw each side as a trapezoid (CSS mitered border rendering)
	// Top border
	if box.Border.Top > 0 && borderStyles.Top != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "top"); ok && color.A > 0 {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerLeft, outerTop)
			r.context.LineTo(outerRight, outerTop)
			r.context.LineTo(innerRight, innerTop)
			r.context.LineTo(innerLeft, innerTop)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Right border
	if box.Border.Right > 0 && borderStyles.Right != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "right"); ok && color.A > 0 {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerRight, outerTop)
			r.context.LineTo(outerRight, outerBottom)
			r.context.LineTo(innerRight, innerBottom)
			r.context.LineTo(innerRight, innerTop)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Bottom border
	if box.Border.Bottom > 0 && borderStyles.Bottom != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "bottom"); ok && color.A > 0 {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerLeft, outerBottom)
			r.context.LineTo(outerRight, outerBottom)
			r.context.LineTo(innerRight, innerBottom)
			r.context.LineTo(innerLeft, innerBottom)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Left border
	if box.Border.Left > 0 && borderStyles.Left != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "left"); ok && color.A > 0 {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerLeft, outerTop)
			r.context.LineTo(outerLeft, outerBottom)
			r.context.LineTo(innerLeft, innerBottom)
			r.context.LineTo(innerLeft, innerTop)
			r.context.ClosePath()
			r.context.Fill()
		}
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

// Phase 19: drawBoxShadow draws box-shadow effects
func (r *Renderer) drawBoxShadow(box *layout.Box) {
	shadows := box.Style.GetBoxShadow()
	if len(shadows) == 0 {
		return
	}

	// Box dimensions (content + padding)
	boxX := box.X
	boxY := box.Y
	boxWidth := box.Width + box.Padding.Left + box.Padding.Right
	boxHeight := box.Height + box.Padding.Top + box.Padding.Bottom
	borderRadius := box.Style.GetBorderRadius()

	// Draw each shadow (in order, first shadow on bottom)
	for _, shadow := range shadows {
		// Calculate shadow position
		shadowX := boxX + shadow.OffsetX
		shadowY := boxY + shadow.OffsetY
		shadowWidth := boxWidth + shadow.Spread*2
		shadowHeight := boxHeight + shadow.Spread*2

		// Adjust for spread
		if shadow.Spread != 0 {
			shadowX -= shadow.Spread
			shadowY -= shadow.Spread
		}

		// Set shadow color with alpha
		r.context.SetRGBA(
			float64(shadow.Color.R)/255.0,
			float64(shadow.Color.G)/255.0,
			float64(shadow.Color.B)/255.0,
			shadow.Color.A,
		)

		// For simplicity, draw shadow as a blurred rectangle
		// (Real implementation would need proper gaussian blur)
		if shadow.Blur > 0 {
			// Simulate blur with multiple rectangles at decreasing opacity
			blurSteps := int(shadow.Blur / 2)
			if blurSteps < 1 {
				blurSteps = 1
			}
			if blurSteps > 10 {
				blurSteps = 10 // Limit for performance
			}

			baseAlpha := shadow.Color.A / float64(blurSteps)

			for i := 0; i < blurSteps; i++ {
				offset := float64(i) * 2
				alpha := baseAlpha * (1.0 - float64(i)/float64(blurSteps))

				r.context.SetRGBA(
					float64(shadow.Color.R)/255.0,
					float64(shadow.Color.G)/255.0,
					float64(shadow.Color.B)/255.0,
					alpha,
				)

				if borderRadius > 0 {
					r.context.DrawRoundedRectangle(
						shadowX-offset,
						shadowY-offset,
						shadowWidth+offset*2,
						shadowHeight+offset*2,
						borderRadius+offset,
					)
				} else {
					r.context.DrawRectangle(
						shadowX-offset,
						shadowY-offset,
						shadowWidth+offset*2,
						shadowHeight+offset*2,
					)
				}
				r.context.Fill()
			}
		} else {
			// No blur, just draw solid shadow
			if borderRadius > 0 {
				r.context.DrawRoundedRectangle(shadowX, shadowY, shadowWidth, shadowHeight, borderRadius)
			} else {
				r.context.DrawRectangle(shadowX, shadowY, shadowWidth, shadowHeight)
			}
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

	// Phase 20: Apply text-transform
	textTransform := box.Style.GetTextTransform()
	textContent = applyTextTransform(textContent, textTransform)

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
	textY := box.Y + fontSize
	r.context.DrawString(textContent, textX, textY)

	// Phase 17: Draw text decorations
	decoration := box.Style.GetTextDecoration()
	if decoration != css.TextDecorationNone {
		textWidth, _ := r.context.MeasureString(textContent)
		lineThickness := fontSize / 12.0 // Standard thickness: ~1/12 of font size
		if lineThickness < 1 {
			lineThickness = 1
		}

		r.context.SetLineWidth(lineThickness)

		switch decoration {
		case css.TextDecorationUnderline:
			// Draw line below text (slightly below baseline)
			underlineY := textY + fontSize*0.1
			r.context.DrawLine(textX, underlineY, textX+textWidth, underlineY)
			r.context.Stroke()

		case css.TextDecorationOverline:
			// Draw line above text
			overlineY := box.Y
			r.context.DrawLine(textX, overlineY, textX+textWidth, overlineY)
			r.context.Stroke()

		case css.TextDecorationLineThrough:
			// Draw line through middle of text
			lineThroughY := box.Y + fontSize*0.5
			r.context.DrawLine(textX, lineThroughY, textX+textWidth, lineThroughY)
			r.context.Stroke()
		}
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

// Phase 24: drawBackgroundImage renders a CSS background-image on a box
func (r *Renderer) drawBackgroundImage(box *layout.Box) {
	imgURL, ok := box.Style.GetBackgroundImage()
	if !ok {
		return
	}

	img, err := images.LoadImage(imgURL)
	if err != nil {
		return // silently skip if image can't be loaded
	}


	// Background area: content + padding
	bgX := box.X
	bgY := box.Y
	bgWidth := box.Width + box.Padding.Left + box.Padding.Right
	bgHeight := box.Height + box.Padding.Top + box.Padding.Bottom

	bounds := img.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())

	repeat := box.Style.GetBackgroundRepeat()
	pos := box.Style.GetBackgroundPosition()
	attachment := box.Style.GetBackgroundAttachment()

	// For fixed attachment, position is relative to viewport origin
	// The tiling origin is (0,0) + position offset, clipped to element bounds
	originX := bgX
	originY := bgY
	if attachment == "fixed" {
		originX = 0
		originY = 0
	}

	// Clip background image to the background area
	r.context.Push()
	r.context.DrawRectangle(bgX, bgY, bgWidth, bgHeight)
	r.context.Clip()

	drawClipped := func(drawX, drawY int) {
		r.context.DrawImage(img, drawX, drawY)
	}

	switch repeat {
	case css.BackgroundRepeatNoRepeat:
		drawClipped(int(originX+pos.X), int(originY+pos.Y))

	case css.BackgroundRepeatRepeatX:
		startX := pos.X
		for startX > 0 {
			startX -= imgW
		}
		// For fixed, need to cover from viewport origin to element bounds
		tileEndX := bgX + bgWidth - originX
		for x := startX; x < tileEndX; x += imgW {
			drawClipped(int(originX+x), int(originY+pos.Y))
		}

	case css.BackgroundRepeatRepeatY:
		startY := pos.Y
		for startY > 0 {
			startY -= imgH
		}
		tileEndY := bgY + bgHeight - originY
		for y := startY; y < tileEndY; y += imgH {
			drawClipped(int(originX+pos.X), int(originY+y))
		}

	default: // repeat
		startX := pos.X
		for startX > 0 {
			startX -= imgW
		}
		startY := pos.Y
		for startY > 0 {
			startY -= imgH
		}
		tileEndX := bgX + bgWidth - originX
		tileEndY := bgY + bgHeight - originY
		for y := startY; y < tileEndY; y += imgH {
			for x := startX; x < tileEndX; x += imgW {
				drawClipped(int(originX+x), int(originY+y))
			}
		}
	}

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

// Phase 20: applyTextTransform applies CSS text-transform to a string
func applyTextTransform(text string, transform css.TextTransform) string {
	switch transform {
	case css.TextTransformUppercase:
		return strings.ToUpper(text)
	case css.TextTransformLowercase:
		return strings.ToLower(text)
	case css.TextTransformCapitalize:
		// Capitalize first letter of each word
		words := strings.Fields(text)
		for i, word := range words {
			if len(word) > 0 {
				words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
			}
		}
		return strings.Join(words, " ")
	case css.TextTransformNone:
		return text
	}
	return text
}

// Phase 21: drawScrollbarIndicators draws visual scrollbar indicators
func (r *Renderer) drawScrollbarIndicators(box *layout.Box) {
	scrollbarWidth := 12.0
	scrollbarColor := css.Color{R: 200, G: 200, B: 200, A: 1.0}

	// Content area dimensions
	contentX := box.X + box.Padding.Left
	contentY := box.Y + box.Padding.Top
	contentWidth := box.Width
	contentHeight := box.Height

	// Draw vertical scrollbar on the right
	r.context.SetRGBA(
		float64(scrollbarColor.R)/255.0,
		float64(scrollbarColor.G)/255.0,
		float64(scrollbarColor.B)/255.0,
		scrollbarColor.A,
	)

	scrollbarX := contentX + contentWidth - scrollbarWidth
	r.context.DrawRectangle(scrollbarX, contentY, scrollbarWidth, contentHeight)
	r.context.Fill()

	// Draw horizontal scrollbar at the bottom
	scrollbarY := contentY + contentHeight - scrollbarWidth
	r.context.DrawRectangle(contentX, scrollbarY, contentWidth-scrollbarWidth, scrollbarWidth)
	r.context.Fill()
}
