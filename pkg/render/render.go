package render

import (
	"sort"

	"github.com/fogleman/gg"
	"louis14/pkg/css"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/text"
)

type Renderer struct {
	context *gg.Context
	scrollY float64 // Viewport scroll offset - non-fixed content is shifted by -scrollY
}

func NewRenderer(width, height int) *Renderer {
	return &Renderer{context: gg.NewContext(width, height)}
}

// SetScrollY sets the viewport scroll offset for rendering.
// Non-fixed content will be shifted up by this amount.
// Fixed-positioned content remains at its absolute position.
func (r *Renderer) SetScrollY(scrollY float64) {
	r.scrollY = scrollY
}

// Render renders boxes using tree-based paint order (CSS 2.1 Appendix E).
// This maintains proper parent-child relationships while respecting z-index stacking.
// Fixed elements are painted in their natural tree order (not extracted and painted last).
// This matches modern browser behavior where position:fixed creates a stacking context.
func (r *Renderer) Render(boxes []*layout.Box) {
	r.context.SetRGB(1, 1, 1)
	r.context.Clear()

	// Render each root box as a stacking context (the root always forms one)
	// This ensures proper CSS 2.1 Appendix E paint order for the entire document
	for _, box := range boxes {
		r.paintStackingContext(box)
	}
}

// paintStackingContext paints a box that creates a stacking context,
// following CSS 2.1 Appendix E paint order for ALL descendants.
func (r *Renderer) paintStackingContext(box *layout.Box) {
	if box == nil {
		return
	}

	// Step 1: Background and borders of this element
	r.drawBoxBackgroundAndBorders(box)

	// Collect ALL descendants, categorized by paint order
	var negativeZ, zeroAutoZ, positiveZ []*layout.Box
	var blocks, floats, inlines []*layout.Box

	r.collectDescendantsForPaintOrder(box, &negativeZ, &blocks, &floats, &inlines, &zeroAutoZ, &positiveZ)

	// Sort z-index groups
	sort.SliceStable(negativeZ, func(i, j int) bool {
		return negativeZ[i].ZIndex < negativeZ[j].ZIndex
	})
	sort.SliceStable(positiveZ, func(i, j int) bool {
		return positiveZ[i].ZIndex < positiveZ[j].ZIndex
	})

	// Step 2: Child stacking contexts with negative z-index
	for _, child := range negativeZ {
		r.paintStackingContext(child)
	}

	// Step 3: In-flow, non-positioned, block-level descendants (backgrounds/borders)
	for _, child := range blocks {
		r.drawBoxBackgroundAndBorders(child)
	}

	// Step 4: Non-positioned floats
	// Floats are painted with their own internal paint order (like a mini stacking context)
	for _, child := range floats {
		r.paintStackingContext(child)
	}

	// Step 5: In-flow, inline-level descendants (content paints here)
	// This includes inline elements AND content of block elements
	for _, child := range inlines {
		r.drawBoxBackgroundAndBorders(child)
		r.drawBoxContent(child)
	}

	// Also paint content of blocks at step 5 (text/images inside blocks)
	for _, child := range blocks {
		r.drawBoxContent(child)
	}

	// Paint this box's own content
	r.drawBoxContent(box)

	// Step 6: Positioned descendants with z-index: auto or 0
	// These are painted "as if they generated a new stacking context" (CSS 2.1 Appendix E)
	for _, child := range zeroAutoZ {
		r.paintStackingContext(child)
	}

	// Step 7: Child stacking contexts with positive z-index
	for _, child := range positiveZ {
		r.paintStackingContext(child)
	}
}

// collectDescendantsForPaintOrder recursively collects all descendants,
// categorizing them by paint order. Stops at child stacking contexts.
func (r *Renderer) collectDescendantsForPaintOrder(box *layout.Box,
	negativeZ, blocks, floats, inlines, zeroAutoZ, positiveZ *[]*layout.Box) {

	for _, child := range box.Children {
		if child.Position == css.PositionFixed {
			// Fixed elements create stacking contexts in modern browsers
			*zeroAutoZ = append(*zeroAutoZ, child)
		} else if layout.BoxCreatesStackingContext(child) {
			// Child creates stacking context - categorize by z-index
			if child.ZIndex < 0 {
				*negativeZ = append(*negativeZ, child)
			} else if child.ZIndex > 0 {
				*positiveZ = append(*positiveZ, child)
			} else {
				*zeroAutoZ = append(*zeroAutoZ, child)
			}
			// Don't recurse into stacking contexts - they paint atomically
		} else if layout.IsPositioned(child) {
			// Positioned but no stacking context - paint at step 6
			// "as if it generated a new stacking context" per CSS 2.1 Appendix E
			// Don't recurse - its children are painted within its own paint order
			*zeroAutoZ = append(*zeroAutoZ, child)
		} else if layout.IsFloat(child) {
			*floats = append(*floats, child)
			// Don't recurse into float children - floats paint atomically at step 4
		} else if layout.IsInline(child) {
			*inlines = append(*inlines, child)
			// Recurse into inline's descendants (inline content is part of step 5)
			r.collectDescendantsForPaintOrder(child, negativeZ, blocks, floats, inlines, zeroAutoZ, positiveZ)
		} else {
			// Block element
			*blocks = append(*blocks, child)
			// Recurse into block's descendants to find inline content for step 5
			r.collectDescendantsForPaintOrder(child, negativeZ, blocks, floats, inlines, zeroAutoZ, positiveZ)
		}
	}
}

// RenderLegacy uses the old flat-list rendering approach (kept for comparison)
func (r *Renderer) RenderLegacy(boxes []*layout.Box) {
	r.context.SetRGB(1, 1, 1)
	r.context.Clear()

	allBoxes := r.collectAllBoxes(boxes)
	r.sortByZIndex(allBoxes)

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

// paintLevel returns the CSS painting level for stacking order within the same z-index
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
		return paintLevel(boxes[i]) < paintLevel(boxes[j])
	})
}

// getEffectiveY returns the Y coordinate adjusted for scroll offset.
// Fixed-positioned elements are not affected by scroll.
func (r *Renderer) getEffectiveY(box *layout.Box) float64 {
	if box.Position == css.PositionFixed {
		return box.Y // Fixed elements stay at their absolute position
	}
	return box.Y - r.scrollY // Non-fixed content is shifted up by scrollY
}

// drawBoxBackgroundAndBorders draws only the background and borders of a box.
func (r *Renderer) drawBoxBackgroundAndBorders(box *layout.Box) {
	if box == nil || box.Style == nil {
		return
	}

	// Apply CSS transforms
	transforms := box.Style.GetTransforms()
	if len(transforms) > 0 {
		r.context.Push()
		r.applyTransforms(box, transforms)
		defer r.context.Pop()
	}

	// Draw box-shadow (underneath the box)
	r.drawBoxShadow(box)

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Draw background color
	if bgColor, ok := box.Style.Get("background-color"); ok {
		if color, ok := css.ParseColor(bgColor); ok && color.A > 0 {
			r.context.SetRGBA(
				float64(color.R)/255.0,
				float64(color.G)/255.0,
				float64(color.B)/255.0,
				color.A,
			)

			bgX := box.X
			bgY := effectiveY
			bgWidth := box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right
			bgHeight := box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom

			if bgWidth > 0 && bgHeight > 0 {
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

	// Draw background image
	r.drawBackgroundImage(box)

	// Draw border
	r.drawBorder(box)
}

// drawBoxContent draws the content of a box (text, images, scrollbars).
func (r *Renderer) drawBoxContent(box *layout.Box) {
	if box == nil || box.Style == nil {
		return
	}

	// Apply CSS transforms
	transforms := box.Style.GetTransforms()
	if len(transforms) > 0 {
		r.context.Push()
		r.applyTransforms(box, transforms)
		defer r.context.Pop()
	}

	// Draw image
	r.drawImage(box)

	// Draw text
	r.drawText(box)

	// Draw scrollbar indicators
	overflow := box.Style.GetOverflow()
	if overflow == css.OverflowScroll || overflow == css.OverflowAuto {
		r.drawScrollbarIndicators(box)
	}
}

// drawBox draws a complete box (used by legacy renderer)
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
	}

	// Phase 19: Draw box-shadow (drawn first, underneath the box)
	r.drawBoxShadow(box)

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Phase 2: Draw background (content + padding area, not including margin)
	if bgColor, ok := box.Style.Get("background-color"); ok {
		if color, ok := css.ParseColor(bgColor); ok && color.A > 0 {
			r.context.SetRGBA(
				float64(color.R)/255.0,
				float64(color.G)/255.0,
				float64(color.B)/255.0,
				color.A,
			)

			// CSS 2.1 §14.2.1: Background covers content + padding + border area
			// box.X/Y is the border-box edge (outside of border)
			bgX := box.X
			bgY := effectiveY
			bgWidth := box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right
			bgHeight := box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom

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
	return css.Color{R: 0, G: 0, B: 0, A: 1.0}, false
}

func (r *Renderer) drawBorder(box *layout.Box) {
	if box.Border.Top == 0 && box.Border.Right == 0 && box.Border.Bottom == 0 && box.Border.Left == 0 {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Phase 12: Get border styles for each side
	borderStyles := box.Style.GetBorderStyle()

	// Phase 12: Check for uniform rounded borders
	borderRadius := box.Style.GetBorderRadius()
	if borderRadius > 0 && box.Border.Top == box.Border.Right &&
		box.Border.Right == box.Border.Bottom && box.Border.Bottom == box.Border.Left {
		// Draw uniform rounded border
		if color, ok := r.getBorderSideColor(box, "top"); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.SetLineWidth(box.Border.Top)
			borderX := box.X + box.Border.Left/2
			borderY := effectiveY + box.Border.Top/2
			borderWidth := box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right - box.Border.Left
			borderHeight := box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom - box.Border.Top
			r.context.DrawRoundedRectangle(borderX, borderY, borderWidth, borderHeight, borderRadius)
			r.context.Stroke()
		}
		return
	}

	// Calculate border box coordinates using effective Y
	outerLeft := box.X
	outerTop := effectiveY
	outerRight := box.X + box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right
	outerBottom := effectiveY + box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom
	innerLeft := box.X + box.Border.Left
	innerTop := effectiveY + box.Border.Top
	innerRight := box.X + box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right
	innerBottom := effectiveY + box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom

	// Draw each side as a trapezoid (CSS mitered border rendering)
	// Top border
	if box.Border.Top > 0 && borderStyles.Top != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "top"); ok {
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
		if color, ok := r.getBorderSideColor(box, "right"); ok {
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
		if color, ok := r.getBorderSideColor(box, "bottom"); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerLeft, outerBottom)
			r.context.LineTo(innerLeft, innerBottom)
			r.context.LineTo(innerRight, innerBottom)
			r.context.LineTo(outerRight, outerBottom)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Left border
	if box.Border.Left > 0 && borderStyles.Left != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "left"); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerLeft, outerTop)
			r.context.LineTo(innerLeft, innerTop)
			r.context.LineTo(innerLeft, innerBottom)
			r.context.LineTo(outerLeft, outerBottom)
			r.context.ClosePath()
			r.context.Fill()
		}
	}
}

func (r *Renderer) drawBoxShadow(box *layout.Box) {
	shadows := box.Style.GetBoxShadow()
	if len(shadows) == 0 {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Box dimensions (content + padding)
	boxX := box.X
	boxY := effectiveY
	boxWidth := box.Width + box.Padding.Left + box.Padding.Right
	boxHeight := box.Height + box.Padding.Top + box.Padding.Bottom
	borderRadius := box.Style.GetBorderRadius()

	// Draw shadows in reverse order (first declared = topmost)
	for i := len(shadows) - 1; i >= 0; i-- {
		shadow := shadows[i]

		// Skip inset shadows for now (they render inside the box)
		if shadow.Inset {
			continue
		}

		// Calculate shadow rectangle
		shadowX := boxX + shadow.OffsetX - shadow.Spread
		shadowY := boxY + shadow.OffsetY - shadow.Spread
		shadowWidth := boxWidth + 2*shadow.Spread
		shadowHeight := boxHeight + 2*shadow.Spread

		// For blur, we'll draw multiple layers with decreasing opacity
		// This is a simple approximation of Gaussian blur
		if shadow.Blur > 0 {
			layers := int(shadow.Blur / 2)
			if layers < 3 {
				layers = 3
			}
			if layers > 10 {
				layers = 10
			}

			for layer := layers; layer >= 0; layer-- {
				// Expand each layer slightly
				expand := float64(layer) * (shadow.Blur / float64(layers))
				layerX := shadowX - expand
				layerY := shadowY - expand
				layerWidth := shadowWidth + 2*expand
				layerHeight := shadowHeight + 2*expand

				// Decrease opacity for outer layers
				layerAlpha := shadow.Color.A * (1.0 - float64(layer)/float64(layers+1)) * 0.3

				r.context.SetRGBA(
					float64(shadow.Color.R)/255.0,
					float64(shadow.Color.G)/255.0,
					float64(shadow.Color.B)/255.0,
					layerAlpha,
				)

				if borderRadius > 0 {
					r.context.DrawRoundedRectangle(layerX, layerY, layerWidth, layerHeight, borderRadius+expand)
				} else {
					r.context.DrawRectangle(layerX, layerY, layerWidth, layerHeight)
				}
				r.context.Fill()
			}
		} else {
			// No blur - just draw a solid shadow
			r.context.SetRGBA(
				float64(shadow.Color.R)/255.0,
				float64(shadow.Color.G)/255.0,
				float64(shadow.Color.B)/255.0,
				shadow.Color.A,
			)

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
	textContent, ok := box.Style.Get("text-content")
	if !ok || textContent == "" {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Phase 6 Enhancement: Calculate X position based on text-align
	textX := box.X
	textAlign := box.Style.GetTextAlign()
	fontSize := box.Style.GetFontSize()

	r.context.SetRGB(0, 0, 0)
	if colorStr, ok := box.Style.Get("color"); ok {
		if color, ok := css.ParseColor(colorStr); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
		}
	}

	// Draw text at calculated position
	textY := effectiveY + fontSize
	r.context.DrawString(textContent, textX, textY)

	// Phase 17: Draw text decorations
	decoration := box.Style.GetTextDecoration()
	if decoration != css.TextDecorationNone {
		bold := box.Style.GetFontWeight() == css.FontWeightBold
		textWidth, _ := text.MeasureTextWithWeight(textContent, fontSize, bold)

		r.context.SetLineWidth(1)
		switch decoration {
		case css.TextDecorationUnderline:
			underlineY := effectiveY + fontSize + 2
			if textAlign == css.TextAlignCenter {
				r.context.DrawLine(textX-textWidth/2, underlineY, textX+textWidth/2, underlineY)
			} else {
				r.context.DrawLine(textX, underlineY, textX+textWidth, underlineY)
			}
			r.context.Stroke()

		case css.TextDecorationOverline:
			overlineY := effectiveY
			r.context.DrawLine(textX, overlineY, textX+textWidth, overlineY)
			r.context.Stroke()

		case css.TextDecorationLineThrough:
			lineThroughY := effectiveY + fontSize*0.5
			r.context.DrawLine(textX, lineThroughY, textX+textWidth, lineThroughY)
			r.context.Stroke()
		}
	}
	_ = textAlign // Used in decoration drawing
}

func (r *Renderer) drawImage(box *layout.Box) {
	if box.ImagePath == "" {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Load the image
	img, err := images.LoadImage(box.ImagePath)
	if err != nil {
		// Image failed to load, draw placeholder
		r.context.SetRGB(0.9, 0.9, 0.9)
		r.context.DrawRectangle(box.X, effectiveY, box.Width, box.Height)
		r.context.Fill()

		r.context.SetRGB(0.5, 0.5, 0.5)
		r.context.SetLineWidth(2)
		r.context.DrawLine(box.X, effectiveY, box.X+box.Width, effectiveY+box.Height)
		r.context.DrawLine(box.X+box.Width, effectiveY, box.X, effectiveY+box.Height)
		r.context.Stroke()
		return
	}

	r.context.Push()
	r.context.Translate(box.X+box.Border.Left+box.Padding.Left, effectiveY+box.Border.Top+box.Padding.Top)

	bounds := img.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())

	scaleX := box.Width / imgW
	scaleY := box.Height / imgH

	r.context.Scale(scaleX, scaleY)
	r.context.DrawImage(img, 0, 0)
	r.context.Pop()
}

// drawBackgroundImage renders a CSS background-image on a box
func (r *Renderer) drawBackgroundImage(box *layout.Box) {
	imgURL, ok := box.Style.GetBackgroundImage()
	if !ok {
		return
	}

	img, err := images.LoadImage(imgURL)
	if err != nil {
		return
	}

	effectiveY := r.getEffectiveY(box)

	bgX := box.X
	bgY := effectiveY
	bgWidth := box.Border.Left + box.Padding.Left + box.Width + box.Padding.Right + box.Border.Right
	bgHeight := box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom

	bounds := img.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())

	repeat := box.Style.GetBackgroundRepeat()
	pos := box.Style.GetBackgroundPosition()
	attachment := box.Style.GetBackgroundAttachment()

	originX := bgX
	originY := bgY
	if attachment == "fixed" {
		originX = 0
		originY = 0
	}

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

func (r *Renderer) applyTransforms(box *layout.Box, transforms []css.Transform) {
	origin := box.Style.GetTransformOrigin()
	effectiveY := r.getEffectiveY(box)

	originX := box.X + box.Padding.Left + origin.X*box.Width
	originY := effectiveY + box.Padding.Top + origin.Y*box.Height

	r.context.Translate(originX, originY)

	for _, t := range transforms {
		switch t.Type {
		case "translate":
			if len(t.Values) >= 2 {
				r.context.Translate(t.Values[0], t.Values[1])
			} else if len(t.Values) >= 1 {
				r.context.Translate(t.Values[0], 0)
			}
		case "rotate":
			if len(t.Values) >= 1 {
				r.context.Rotate(t.Values[0])
			}
		case "scale":
			if len(t.Values) >= 2 {
				r.context.Scale(t.Values[0], t.Values[1])
			} else if len(t.Values) >= 1 {
				r.context.Scale(t.Values[0], t.Values[0])
			}
		case "skew":
			// Approximate skew using shear matrix
		}
	}

	r.context.Translate(-originX, -originY)
}

func (r *Renderer) drawScrollbarIndicators(box *layout.Box) {
	scrollbarWidth := 12.0
	scrollbarColor := css.Color{R: 200, G: 200, B: 200, A: 1.0}

	effectiveY := r.getEffectiveY(box)

	contentX := box.X + box.Padding.Left
	contentY := effectiveY + box.Padding.Top
	contentWidth := box.Width
	contentHeight := box.Height

	r.context.SetRGBA(
		float64(scrollbarColor.R)/255.0,
		float64(scrollbarColor.G)/255.0,
		float64(scrollbarColor.B)/255.0,
		scrollbarColor.A,
	)

	// Vertical scrollbar
	r.context.DrawRectangle(
		contentX+contentWidth-scrollbarWidth,
		contentY,
		scrollbarWidth,
		contentHeight,
	)
	r.context.Fill()

	// Horizontal scrollbar
	r.context.DrawRectangle(
		contentX,
		contentY+contentHeight-scrollbarWidth,
		contentWidth-scrollbarWidth,
		scrollbarWidth,
	)
	r.context.Fill()
}
