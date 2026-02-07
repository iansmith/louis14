package render

import (
	"fmt"
	"image"
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
	context      *gg.Context
	scrollY      float64              // Viewport scroll offset - non-fixed content is shifted by -scrollY
	imageFetcher images.ImageFetcher  // Optional fetcher for network images
	fonts        text.FontConfig      // Font configuration for text rendering
	lastFontKey  string               // Tracks loaded font to avoid redundant loads
}

func NewRenderer(width, height int) *Renderer {
	return &Renderer{
		context: gg.NewContext(width, height),
		fonts:   text.DefaultFontConfig(),
	}
}

// NewRendererForImage creates a renderer that draws onto the provided RGBA image.
// The viewport dimensions are derived from the image bounds.
func NewRendererForImage(target *image.RGBA) *Renderer {
	return &Renderer{
		context: gg.NewContextForRGBA(target),
		fonts:   text.DefaultFontConfig(),
	}
}

// SetFonts sets the font configuration used for text rendering.
func (r *Renderer) SetFonts(fonts text.FontConfig) {
	r.fonts = fonts
}

// SetImageFetcher sets the image fetcher used to load network images during rendering.
func (r *Renderer) SetImageFetcher(fetcher images.ImageFetcher) {
	r.imageFetcher = fetcher
}

// loadFont loads a font face on the gg context for the given size and style.
// Skips reloading if the same font+size is already active.
func (r *Renderer) loadFont(fontSize float64, bold, italic, mono bool) {
	fontPath := r.fonts.FontPath(bold, italic, mono)
	key := fmt.Sprintf("%s@%.1f", fontPath, fontSize)
	if key == r.lastFontKey {
		return
	}
	if err := r.context.LoadFontFace(fontPath, fontSize); err == nil {
		r.lastFontKey = key
	}
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

	// DEBUG: Check pixel value at span center before saving
	img := r.context.Image().(*image.RGBA)
	centerX, centerY := 250, 335
	pixelIndex := centerY*img.Stride + centerX*4
	if pixelIndex >= 0 && pixelIndex+2 < len(img.Pix) {
		r := img.Pix[pixelIndex+0]
		g := img.Pix[pixelIndex+1]
		b := img.Pix[pixelIndex+2]
		a := img.Pix[pixelIndex+3]
		fmt.Printf("DEBUG FINAL: Before SavePNG, pixel at (%d,%d) = RGBA(%d,%d,%d,%d)\n",
			centerX, centerY, r, g, b, a)
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
	fmt.Printf("=== STEP 2: Negative Z (%d elements) ===\n", len(negativeZ))
	for _, child := range negativeZ {
		r.paintStackingContext(child)
	}

	// Step 3: In-flow, non-positioned, block-level descendants (backgrounds/borders)
	fmt.Printf("=== STEP 3: Blocks (%d elements) ===\n", len(blocks))
	for _, child := range blocks {
		r.drawBoxBackgroundAndBorders(child)
	}

	// Step 4: Non-positioned floats
	// Floats are painted with their own internal paint order (like a mini stacking context)
	fmt.Printf("=== STEP 4: Floats (%d elements) ===\n", len(floats))
	for _, child := range floats {
		r.paintStackingContext(child)
	}

	// Step 5: In-flow, inline-level descendants (content paints here)
	// This includes inline elements AND content of block elements
	fmt.Printf("=== STEP 5: Inlines (%d elements) ===\n", len(inlines))
	for _, child := range inlines {
		if child.Node != nil && child.Node.Type != html.TextNode {
			bgColor := ""
			borderColor := ""
			if child.Style != nil {
				if bg, ok := child.Style.Get("background-color"); ok {
					bgColor = bg
				}
				if bc, ok := child.Style.Get("border-color"); ok {
					borderColor = bc
				}
			}
			fmt.Printf("DEBUG RENDER: Rendering inline <%s> at (%.1f,%.1f) size %.1fx%.1f bg=%s border=%s\n",
				child.Node.TagName, child.X, child.Y, child.Width, child.Height, bgColor, borderColor)
			fmt.Printf("DEBUG RENDER:   Box.Padding=%.1f/%.1f/%.1f/%.1f  Box.Border=%.1f/%.1f/%.1f/%.1f\n",
				child.Padding.Top, child.Padding.Right, child.Padding.Bottom, child.Padding.Left,
				child.Border.Top, child.Border.Right, child.Border.Bottom, child.Border.Left)
		}
		r.drawBoxBackgroundAndBorders(child)
		r.drawBoxContent(child)
	}

	// Also paint content of blocks at step 5 (text/images inside blocks)
	fmt.Printf("=== STEP 5 continued: Block content (%d elements) ===\n", len(blocks))
	for _, child := range blocks {
		if child.Node != nil {
			fmt.Printf("DEBUG BLOCK CONTENT: Drawing content for <%s> at (%.1f,%.1f) size %.1fx%.1f\n",
				child.Node.TagName, child.X, child.Y, child.Width, child.Height)
		}
		r.drawBoxContent(child)
	}

	// Paint this box's own content
	fmt.Printf("=== STEP 5 continued: Box own content ===\n")
	r.drawBoxContent(box)

	// Step 6: Positioned descendants with z-index: auto or 0
	// These are painted "as if they generated a new stacking context" (CSS 2.1 Appendix E)
	fmt.Printf("=== STEP 6: Z-Index Auto/0 (%d elements) ===\n", len(zeroAutoZ))
	for _, child := range zeroAutoZ {
		if child.Node != nil {
			id := ""
			if child.Node.Attributes != nil {
				if i, ok := child.Node.Attributes["id"]; ok {
					id = " id=" + i
				}
			}
			fmt.Printf("DEBUG STEP 6: Painting <%s%s> at (%.1f,%.1f)\n", child.Node.TagName, id, child.X, child.Y)
		}
		r.paintStackingContext(child)
	}

	// Step 7: Child stacking contexts with positive z-index
	fmt.Printf("=== STEP 7: Positive Z (%d elements) ===\n", len(positiveZ))
	for _, child := range positiveZ {
		r.paintStackingContext(child)
	}
}

// collectDescendantsForPaintOrder recursively collects all descendants,
// categorizing them by paint order. Stops at child stacking contexts.
func (r *Renderer) collectDescendantsForPaintOrder(box *layout.Box,
	negativeZ, blocks, floats, inlines, zeroAutoZ, positiveZ *[]*layout.Box) {

	for _, child := range box.Children {
		// DEBUG: Log position for divs with IDs
		if child.Node != nil && child.Node.TagName == "div" && child.Node.Attributes != nil {
			if id, ok := child.Node.Attributes["id"]; ok && (id == "reference" || id == "test") {
				fmt.Printf("DEBUG PAINT: div#%s Position=%v, IsPositioned=%v\n", id, child.Position, layout.IsPositioned(child))
			}
		}

		if child.Position == css.PositionFixed {
			// Fixed elements create stacking contexts in modern browsers
			if child.Node != nil && child.Node.Attributes != nil {
				if id, ok := child.Node.Attributes["id"]; ok && (id == "reference" || id == "test") {
					fmt.Printf("DEBUG PAINT: div#%s -> Fixed, adding to zeroAutoZ\n", id)
				}
			}
			*zeroAutoZ = append(*zeroAutoZ, child)
		} else if layout.BoxCreatesStackingContext(child) {
			// Child creates stacking context - categorize by z-index
			if child.Node != nil && child.Node.Attributes != nil {
				if id, ok := child.Node.Attributes["id"]; ok && (id == "reference" || id == "test") {
					fmt.Printf("DEBUG PAINT: div#%s -> BoxCreatesStackingContext=true, ZIndex=%d, adding to zeroAutoZ\n", id, child.ZIndex)
				}
			}
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
			if child.Node != nil && child.Node.Attributes != nil {
				if id, ok := child.Node.Attributes["id"]; ok && (id == "reference" || id == "test") {
					fmt.Printf("DEBUG PAINT: div#%s -> IsPositioned=true, adding to zeroAutoZ\n", id)
				}
			}
			*zeroAutoZ = append(*zeroAutoZ, child)
		} else if layout.IsFloat(child) {
			*floats = append(*floats, child)
			// Don't recurse into float children - floats paint atomically at step 4
		} else if layout.IsInline(child) {
			if child.Node != nil && child.Node.TagName == "span" {
				fmt.Printf("DEBUG COLLECT: Adding span to inlines collection\n")
			}
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

	// DEBUG: Check Y values for red background
	if bgColor, ok := box.Style.Get("background-color"); ok && bgColor == "red" {
		effectiveY := r.getEffectiveY(box)
		fmt.Printf("DEBUG RENDER Y: red div box.Y=%.1f effectiveY=%.1f scrollY=%.1f\n",
			box.Y, effectiveY, r.scrollY)
	}

	if box.Node != nil {
		tagName := box.Node.TagName
		if tagName == "div" || tagName == "span" {
			bgColor := ""
			if bg, ok := box.Style.Get("background-color"); ok {
				bgColor = bg
			}
			zindex := "auto"
			if box.ZIndex != 0 {
				zindex = fmt.Sprintf("%d", box.ZIndex)
			}
			posType := "static"
			if box.Position == css.PositionRelative {
				posType = "relative"
			} else if box.Position == css.PositionAbsolute {
				posType = "absolute"
			}
			fmt.Printf("DEBUG DRAW: Drawing <%s> at (%.1f,%.1f) size %.1fx%.1f bg=%s pos=%s z=%s\n",
				tagName, box.X, box.Y, box.Width, box.Height, bgColor, posType, zindex)
			if tagName == "span" && len(box.Children) > 0 {
				for i, child := range box.Children {
					childBg := "none"
					if child.Style != nil {
						if bg, ok := child.Style.Get("background-color"); ok {
							childBg = bg
						}
					}
					childTag := "?"
					if child.Node != nil {
						childTag = child.Node.TagName
					}
					fmt.Printf("DEBUG DRAW:   Child[%d]: <%s> at (%.1f,%.1f) size %.1fx%.1f bg=%s\n",
						i, childTag, child.X, child.Y, child.Width, child.Height, childBg)
				}
			}
		}
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
			// Check for white background
			if color.R > 250 && color.G > 250 && color.B > 250 && box.Node != nil {
				fmt.Printf("DEBUG WHITE: Drawing WHITE background for <%s> at (%.1f,%.1f) size %.1fx%.1f\n",
					box.Node.TagName, box.X, box.Y, box.Width, box.Height)
			}
			if box.Node != nil && box.Node.TagName == "span" {
				fmt.Printf("DEBUG COLOR: Parsed color for span: R=%d G=%d B=%d A=%.2f\n",
					color.R, color.G, color.B, color.A)
			}
			rVal := float64(color.R) / 255.0
			gVal := float64(color.G) / 255.0
			bVal := float64(color.B) / 255.0

			if box.Node != nil && box.Node.TagName == "span" {
				fmt.Printf("DEBUG RENDER: Setting color R=%.3f G=%.3f B=%.3f A=%.2f\n",
					rVal, gVal, bVal, color.A)
			}

			r.context.SetRGBA(rVal, gVal, bVal, color.A)

			bgX := box.X
			bgY := effectiveY
			bgWidth := box.Width // Border-box dimensions
			bgHeight := box.Height // Border-box dimensions

			if box.Node != nil && box.Node.TagName == "span" {
				fmt.Printf("DEBUG COORDS: box.Y=%.1f, effectiveY=%.1f, scrollY=%.1f\n",
					box.Y, effectiveY, r.scrollY)
				fmt.Printf("DEBUG SIZE: bgWidth=%.1f, bgHeight=%.1f (check: %v)\n",
					bgWidth, bgHeight, bgWidth > 0 && bgHeight > 0)
			}

			if bgWidth > 0 && bgHeight > 0 {
				if box.Node != nil && box.Node.TagName == "span" {
					fmt.Printf("DEBUG PRE-DRAW: About to call DrawRectangle(%.1f, %.1f, %.1f, %.1f)\n",
						bgX, bgY, bgWidth, bgHeight)
					// Check what color is currently set
					fmt.Printf("DEBUG PRE-DRAW: Current gg context color should be G=0.502\n")
				}
				borderRadius := box.Style.GetBorderRadius()
				if borderRadius > 0 {
					r.context.DrawRoundedRectangle(bgX, bgY, bgWidth, bgHeight, borderRadius)
				} else {
					r.context.DrawRectangle(bgX, bgY, bgWidth, bgHeight)
				}
				if box.Node != nil && box.Node.TagName == "span" {
					fmt.Printf("DEBUG PRE-FILL: About to call Fill()\n")
				}
				r.context.Fill()
				if box.Node != nil && box.Node.TagName == "span" {
					fmt.Printf("DEBUG DRAW BG: Finished Fill\n")
					// Read back the pixel at the center of the rectangle to verify it was drawn
					centerX := int(bgX + bgWidth/2)
					centerY := int(bgY + bgHeight/2)
					img := r.context.Image().(*image.RGBA)
					pixelIndex := centerY*img.Stride + centerX*4
					if pixelIndex >= 0 && pixelIndex+2 < len(img.Pix) {
						r := img.Pix[pixelIndex+0]
						g := img.Pix[pixelIndex+1]
						b := img.Pix[pixelIndex+2]
						a := img.Pix[pixelIndex+3]
						fmt.Printf("DEBUG PIXEL: After Fill, pixel at (%d,%d) = RGBA(%d,%d,%d,%d)\n",
							centerX, centerY, r, g, b, a)
					}
				}
			} else {
				if box.Node != nil && box.Node.TagName == "span" {
					fmt.Printf("DEBUG DRAW BG: SKIPPED because bgWidth=%f or bgHeight=%f is <=0\n", bgWidth, bgHeight)
				}
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

	if box.Node != nil && box.Node.TagName == "span" {
		fmt.Printf("DEBUG CONTENT: drawBoxContent for span\n")
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
	if box.Node != nil && box.Node.TagName == "span" {
		fmt.Printf("DEBUG CONTENT: About to call drawText for span\n")
	}
	r.drawText(box)
	if box.Node != nil && box.Node.TagName == "span" {
		fmt.Printf("DEBUG CONTENT: Finished drawText for span\n")
	}

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
			bgWidth := box.Width // Border-box dimensions
			bgHeight := box.Height // Border-box dimensions

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
	// resolveCurrentColor resolves "currentcolor" to the element's color property
	resolveCurrentColor := func(colorStr string) (css.Color, bool) {
		if strings.EqualFold(colorStr, "currentcolor") {
			if c, ok := box.Style.Get("color"); ok {
				if color, ok := css.ParseColor(c); ok {
					return color, true
				}
			}
			// Default currentcolor is black
			return css.Color{R: 0, G: 0, B: 0, A: 1.0}, true
		}
		return css.ParseColor(colorStr)
	}

	// Check per-side color first
	if colorStr, ok := box.Style.Get("border-" + side + "-color"); ok {
		if color, ok := resolveCurrentColor(colorStr); ok {
			return color, true
		}
	}
	// Fall back to global border-color
	if colorStr, ok := box.Style.Get("border-color"); ok {
		if color, ok := resolveCurrentColor(colorStr); ok {
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
			borderWidth := box.Width - box.Border.Left // Border-box dimensions
			borderHeight := box.Height - box.Border.Top // Border-box dimensions
			r.context.DrawRoundedRectangle(borderX, borderY, borderWidth, borderHeight, borderRadius)
			r.context.Stroke()
		}
		return
	}

	// Calculate border box coordinates using effective Y
	outerLeft := box.X
	outerTop := effectiveY
	outerRight := box.X + box.Width // Border-box dimensions
	outerBottom := effectiveY + box.Height // Border-box dimensions
	innerLeft := box.X + box.Border.Left
	innerTop := effectiveY + box.Border.Top
	innerRight := box.X + box.Width - box.Border.Right // Border-box dimensions
	innerBottom := effectiveY + box.Height - box.Border.Bottom // Border-box dimensions

	// Draw each side as a trapezoid (CSS mitered border rendering).
	// Drawing order: bottom → left → right → top. Later-drawn sides
	// overwrite boundary pixels at diagonal miters, so this order gives
	// CSS priority: top > right > left > bottom at shared corners.

	// Bottom border
	if box.Border.Bottom > 0 && borderStyles.Bottom != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "bottom"); ok {
			if box.Node != nil && box.Node.TagName == "span" {
				fmt.Printf("DEBUG BORDER: Drawing span bottom border with color R=%d G=%d B=%d\n",
					color.R, color.G, color.B)
			}
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
	// Skip left border for LastFragment of split inline (CSS 2.1 §9.2.1.1)
	if box.Border.Left > 0 && borderStyles.Left != css.BorderStyleNone && !box.IsLastFragment {
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

	// Right border
	// Skip right border for FirstFragment of split inline (CSS 2.1 §9.2.1.1)
	if box.Border.Right > 0 && borderStyles.Right != css.BorderStyleNone && !box.IsFirstFragment {
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
	boxWidth := box.Width - box.Border.Left - box.Border.Right // Padding box
	boxHeight := box.Height - box.Border.Top - box.Border.Bottom // Padding box
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
	// Multi-line text containers have children (one per line) that draw the
	// actual text. Drawing the container's full text would duplicate it.
	if len(box.Children) > 0 && box.Node != nil && box.Node.Type == html.TextNode {
		return
	}

	// Determine text content: from DOM text node or pseudo-element content
	textContent := ""
	if box.PseudoContent != "" {
		textContent = box.PseudoContent
	} else if box.Node != nil && box.Node.Type == html.TextNode {
		textContent = box.Node.Text
	}
	if box.Node != nil && textContent != "" {
		fmt.Printf("DEBUG TEXT: <%s> drawing textContent=%q at (%.1f,%.1f)\n",
			box.Node.TagName, textContent, box.X, box.Y)
	}
	if textContent == "" {
		return
	}

	// Skip drawing parent's PseudoContent if it has children (child boxes draw the actual content)
	// This prevents the parent from drawing the full text which would be covered by child boxes
	if box.PseudoContent != "" && len(box.Children) > 0 {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Calculate X position based on text-align
	textX := box.X
	textAlign := box.Style.GetTextAlign()
	fontSize := box.Style.GetFontSize()
	bold := box.Style.GetFontWeight() == css.FontWeightBold
	italic := box.Style.GetFontStyle() == css.FontStyleItalic
	mono := box.Style.IsMonospaceFamily()

	// Load the appropriate font face
	r.loadFont(fontSize, bold, italic, mono)

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
}

func (r *Renderer) drawImage(box *layout.Box) {
	if box.ImagePath == "" {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Load the image (use fetcher if available)
	img, err := images.LoadImageWithFetcher(box.ImagePath, r.imageFetcher)
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
	if box.Node != nil && box.Node.TagName == "span" {
		fmt.Printf("DEBUG BG-IMG: drawBackgroundImage called for span\n")
	}
	imgURL, ok := box.Style.GetBackgroundImage()
	if !ok {
		if box.Node != nil && box.Node.TagName == "span" {
			fmt.Printf("DEBUG BG-IMG: No background-image, returning early\n")
		}
		return
	}
	if box.Node != nil && box.Node.TagName == "span" {
		fmt.Printf("DEBUG BG-IMG: Has background-image: %s\n", imgURL)
	}

	img, err := images.LoadImageWithFetcher(imgURL, r.imageFetcher)
	if err != nil {
		return
	}

	effectiveY := r.getEffectiveY(box)

	bgX := box.X
	bgY := effectiveY
	bgWidth := box.Width // Border-box dimensions
	bgHeight := box.Height // Border-box dimensions

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
