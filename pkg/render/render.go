package render

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sync"

	"louis14/pkg/css"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/text"
	"mazarin/textshape"
)

// fontCacheKey identifies a font by path and size.
type fontCacheKey struct {
	path string
	size int32
}

// Renderer paints a tree of layout boxes onto an image.
type Renderer struct {
	dc           textshape.DrawContext
	target       *image.RGBA
	fonts        text.FontConfig
	imageFetcher images.ImageFetcher
	scrollY      float64

	// Font ID cache: (path, size) → fontID
	fontCache   map[fontCacheKey]int32
	fontCacheMu sync.Mutex
}

// newProvider creates a DirectGlyphProvider for the default fonts directory.
func newProvider() textshape.GlyphProvider {
	return textshape.NewDirectGlyphProvider(text.DefaultFontsDir())
}

// NewRenderer creates a new renderer with a fresh image of the given dimensions.
func NewRenderer(width, height int) *Renderer {
	dc := textshape.NewDrawContext(width, height, newProvider())
	text.SetTextLayout(dc.TextLayout())
	return &Renderer{
		dc:        dc,
		target:    dc.Image().(*image.RGBA),
		fonts:     text.DefaultFontConfig(),
		fontCache: make(map[fontCacheKey]int32),
	}
}

// NewRendererForImage creates a renderer that paints onto an existing image.
func NewRendererForImage(target *image.RGBA) *Renderer {
	dc := textshape.NewDrawContextForImage(target, newProvider())
	text.SetTextLayout(dc.TextLayout())
	return &Renderer{
		dc:        dc,
		target:    target,
		fonts:     text.DefaultFontConfig(),
		fontCache: make(map[fontCacheKey]int32),
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

// openFont returns the fontID for the given path+size, opening it if needed.
func (r *Renderer) openFont(fontPath string, fontSize float64) int32 {
	size := int32(math.Round(fontSize))
	key := fontCacheKey{path: fontPath, size: size}
	r.fontCacheMu.Lock()
	defer r.fontCacheMu.Unlock()
	if id, ok := r.fontCache[key]; ok {
		return id
	}
	metrics, err := r.dc.OpenFont(fontPath, 0, size)
	if err != nil {
		return -1
	}
	r.fontCache[key] = metrics.FontID
	return metrics.FontID
}

// Render paints the box tree onto the image.
// Implements CSS 2.1 Appendix E paint order (simplified).
func (r *Renderer) Render(boxes []*layout.Box) {
	// CSS 2.1 §14.2: Canvas background.
	// The root element's background becomes the canvas background.
	// If the root element (html) has no background, use the body's background.
	r.dc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	r.dc.Clear()
	r.paintCanvasBackground(boxes)

	// Paint via PaintLayer tree (CSS 2.1 Appendix E stacking order).
	for _, box := range boxes {
		layer := BuildPaintTree(box)
		r.paintLayer(layer)
	}
}

// paintCanvasBackground implements CSS 2.1 §14.2 canvas background propagation.
// If the root element has a background, use it. Otherwise, propagate from body.
func (r *Renderer) paintCanvasBackground(boxes []*layout.Box) {
	if len(boxes) == 0 {
		return
	}
	root := boxes[0]

	// Check if the root element has a background.
	if r.hasBackground(root) {
		r.fillCanvasWithBackground(root)
		return
	}

	// Root has no background — look for body element among its children.
	for _, child := range root.Children {
		if child.Node != nil && child.Node.TagName == "body" {
			if r.hasBackground(child) {
				r.fillCanvasWithBackground(child)
			}
			return
		}
	}
}

// hasBackground returns true if a box has a non-transparent background-color.
func (r *Renderer) hasBackground(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	bgStr, ok := box.Style.Get("background-color")
	if !ok || bgStr == "" || bgStr == "transparent" {
		return false
	}
	bgColor, ok := css.ParseColor(bgStr)
	if !ok || bgColor.A == 0 {
		return false
	}
	return true
}

// fillCanvasWithBackground fills the entire canvas with the box's background color.
func (r *Renderer) fillCanvasWithBackground(box *layout.Box) {
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
	r.dc.SetColor(color.RGBA{
		R: bgColor.R,
		G: bgColor.G,
		B: bgColor.B,
		A: uint8(bgColor.A * 255),
	})
	r.dc.Clear()
}

// clampColor clamps RGBA values to [0,255] and returns a color.RGBA.
func clampColor(rv, g, b, a float64) color.RGBA {
	clamp := func(v float64) uint8 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v + 0.5)
	}
	return color.RGBA{R: clamp(rv), G: clamp(g), B: clamp(b), A: clamp(a)}
}

// paintLayer paints a PaintLayer and its descendants using the pre-built
// stacking order. All paint decisions use pre-computed fields — no Style access.
func (r *Renderer) paintLayer(layer *PaintLayer) {
	if layer == nil {
		return
	}

	// Pre-computed visibility and opacity.
	if !layer.Visible {
		return
	}
	if layer.Opacity <= 0 {
		return
	}
	if layer.Opacity < 1.0 {
		// CSS3 Color §4.2: render subtree to offscreen buffer and composite.
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
		return
	}

	r.paintLayerContent(layer)
}

// paintLayerContent paints the layer's own box and children in
// CSS 2.1 Appendix E order using the pre-sorted PaintLayer lists.
func (r *Renderer) paintLayerContent(layer *PaintLayer) {
	// CSS clip: rect() — clips everything including backgrounds and borders.
	// Purely physical coordinates per CSS Writing Modes §7.6.
	cssClipping := false
	if layer.HasCSSClip {
		r.dc.Push()
		cssClipping = true
		r.dc.DrawRectangle(
			layer.CSSClipRect[0], layer.CSSClipRect[1],
			layer.CSSClipRect[2], layer.CSSClipRect[3])
		r.dc.Clip()
	}

	// Step 1: Background and borders.
	r.drawBackground(layer)
	r.drawBorders(layer)

	// Text content.
	if layer.Box.Text != "" {
		r.drawText(layer)
	}

	// Replaced element image (<img>).
	if layer.ImageSrc != "" {
		r.drawImage(layer)
	}

	// Overflow clip using pre-computed rectangle.
	clipping := false
	if layer.HasClip {
		r.dc.Push()
		clipping = true
		r.dc.DrawRectangle(
			layer.ClipRect[0], layer.ClipRect[1],
			layer.ClipRect[2], layer.ClipRect[3])
		r.dc.Clip()
	}

	// Step 2: Negative z-index stacking contexts.
	for _, child := range layer.NegativeZ {
		r.paintLayer(child)
	}

	// Steps 3-5: Non-positioned content in DOM order.
	for _, child := range layer.FlowChildren {
		r.paintLayer(child)
	}

	// Step 6: z-index:auto positioned + z-index:0 SCs.
	for _, child := range layer.AutoZero {
		r.paintLayer(child)
	}

	// Step 7: Positive z-index stacking contexts.
	for _, child := range layer.PositiveZ {
		r.paintLayer(child)
	}

	if clipping {
		r.dc.Pop()
	}

	if cssClipping {
		r.dc.Pop()
	}
}

// drawBackground paints the layer's background color and image (pre-computed).
func (r *Renderer) drawBackground(layer *PaintLayer) {
	box := layer.Box

	// Background color.
	if c := layer.BackgroundColor; c.A > 0 {
		r.setColor(c)
		r.dc.DrawRectangle(box.X, box.Y, box.Width, box.Height)
		r.dc.Fill()
	}

	// Background gradient (linear-gradient, etc.).
	if layer.BackgroundGradient != "" {
		r.drawLinearGradient(layer.BackgroundGradient, box.X, box.Y, box.Width, box.Height)
	}

	// Background image.
	if layer.BackgroundImage != "" && r.imageFetcher != nil {
		r.drawBackgroundImage(layer)
	}
}

// drawBackgroundImage tiles the background image onto the layer's box.
// Tiles are manually clipped to the box bounds because DrawImage bypasses
// the DrawContext clip mask (fast-path pixel blit ignores clipMask).
// All arithmetic is integer-based to avoid fractional pixel misalignment.
func (r *Renderer) drawBackgroundImage(layer *PaintLayer) {
	box := layer.Box
	img, err := images.LoadImageWithFetcher(layer.BackgroundImage, r.imageFetcher)
	if err != nil {
		return
	}
	imgWI := img.Bounds().Dx()
	imgHI := img.Bounds().Dy()
	if imgWI == 0 || imgHI == 0 {
		return
	}
	imgW := float64(imgWI)
	imgH := float64(imgHI)

	pos := layer.BackgroundPosition
	startX := box.X + pos.ResolveX(box.Width, imgW)
	startY := box.Y + pos.ResolveY(box.Height, imgH)

	repeat := layer.BackgroundRepeat
	repeatX := repeat == css.BackgroundRepeatRepeat || repeat == css.BackgroundRepeatRepeatX
	repeatY := repeat == css.BackgroundRepeatRepeat || repeat == css.BackgroundRepeatRepeatY

	// Convert box bounds and start position to integers.
	boxX0 := int(math.Round(box.X))
	boxY0 := int(math.Round(box.Y))
	boxX1 := int(math.Round(box.X + box.Width))
	boxY1 := int(math.Round(box.Y + box.Height))

	// Snap initial tile origin to pixels.
	x0 := int(math.Round(startX))
	y0 := int(math.Round(startY))

	// Extend tile origin left/up so it covers the box edge.
	if repeatX {
		for x0 > boxX0 {
			x0 -= imgWI
		}
	}
	if repeatY {
		for y0 > boxY0 {
			y0 -= imgHI
		}
	}

	for ty := y0; ty < boxY1; ty += imgHI {
		for tx := x0; tx < boxX1; tx += imgWI {
			// Clip tile to box bounds.
			dstX0 := tx
			if dstX0 < boxX0 {
				dstX0 = boxX0
			}
			dstX1 := tx + imgWI
			if dstX1 > boxX1 {
				dstX1 = boxX1
			}
			dstY0 := ty
			if dstY0 < boxY0 {
				dstY0 = boxY0
			}
			dstY1 := ty + imgHI
			if dstY1 > boxY1 {
				dstY1 = boxY1
			}
			if dstX1 <= dstX0 || dstY1 <= dstY0 {
				if !repeatX {
					break
				}
				continue
			}

			// Source sub-region within tile image.
			srcX0 := dstX0 - tx
			srcY0 := dstY0 - ty
			srcX1 := dstX1 - tx
			srcY1 := dstY1 - ty

			sub := img.(interface {
				SubImage(image.Rectangle) image.Image
			}).SubImage(image.Rect(srcX0, srcY0, srcX1, srcY1))
			r.dc.DrawImage(sub, dstX0, dstY0)

			if !repeatX {
				break
			}
		}
		if !repeatY {
			break
		}
	}
}

// drawImage paints an <img> element scaled to its box dimensions.
func (r *Renderer) drawImage(layer *PaintLayer) {
	if layer.ImageSrc == "" || r.imageFetcher == nil {
		return
	}
	box := layer.Box
	img, err := images.LoadImageWithFetcher(layer.ImageSrc, r.imageFetcher)
	if err != nil {
		return
	}
	// Scale image to box content area using nearest-neighbor into an RGBA buffer.
	srcW := img.Bounds().Dx()
	srcH := img.Bounds().Dy()
	contentX := math.Round(box.X + box.Border.Left + box.Padding.Left)
	contentY := math.Round(box.Y + box.Border.Top + box.Padding.Top)
	dstW := int(math.Round(box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right))
	dstH := int(math.Round(box.Height - box.Border.Top - box.Border.Bottom - box.Padding.Top - box.Padding.Bottom))
	if dstW <= 0 || dstH <= 0 || srcW == 0 || srcH == 0 {
		return
	}
	scaled := scaleImageNearest(img, srcW, srcH, dstW, dstH)

	// CSS clip: rect() — DrawImage bypasses the clip mask, so we must
	// manually crop the scaled image to the CSS clip region.
	drawX := int(contentX)
	drawY := int(contentY)
	if layer.HasCSSClip {
		// CSSClipRect is [x, y, w, h] in absolute coordinates.
		cx := int(math.Round(layer.CSSClipRect[0]))
		cy := int(math.Round(layer.CSSClipRect[1]))
		cw := int(math.Round(layer.CSSClipRect[2]))
		ch := int(math.Round(layer.CSSClipRect[3]))
		// Intersect clip region with image draw area.
		ix0 := drawX
		if cx > ix0 {
			ix0 = cx
		}
		iy0 := drawY
		if cy > iy0 {
			iy0 = cy
		}
		ix1 := drawX + dstW
		if cx+cw < ix1 {
			ix1 = cx + cw
		}
		iy1 := drawY + dstH
		if cy+ch < iy1 {
			iy1 = cy + ch
		}
		if ix1 <= ix0 || iy1 <= iy0 {
			return // Entirely clipped
		}
		// Extract the visible sub-image.
		sub := scaled.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(ix0-drawX, iy0-drawY, ix1-drawX, iy1-drawY))
		r.dc.DrawImage(sub, ix0, iy0)
		return
	}

	r.dc.DrawImage(scaled, drawX, drawY)
}

// scaleImageNearest scales src to dstW×dstH using nearest-neighbor.
func scaleImageNearest(src image.Image, srcW, srcH, dstW, dstH int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := 0; dy < dstH; dy++ {
		sy := dy * srcH / dstH
		for dx := 0; dx < dstW; dx++ {
			sx := dx * srcW / dstW
			dst.Set(dx, dy, src.At(src.Bounds().Min.X+sx, src.Bounds().Min.Y+sy))
		}
	}
	return dst
}

// drawBorders draws all four borders of the layer's box (pre-computed styles/colors).
func (r *Renderer) drawBorders(layer *PaintLayer) {
	box := layer.Box
	bw := box.Border
	if bw.Top == 0 && bw.Right == 0 && bw.Bottom == 0 && bw.Left == 0 {
		return
	}

	x, y, w, h := box.X, box.Y, box.Width, box.Height
	outerLeft, outerTop := x, y
	outerRight, outerBottom := x+w, y+h
	innerLeft := math.Floor(x + bw.Left)
	innerTop := math.Floor(y + bw.Top)
	innerRight := math.Ceil(x + w - bw.Right - 1e-9)
	innerBottom := math.Ceil(y + h - bw.Bottom - 1e-9)

	// Top border (index 0).
	if bw.Top > 0 && layer.BorderStyles[0] != css.BorderStyleNone {
		if c := layer.BorderColors[0]; c.A > 0 {
			r.setColor(c)
			r.dc.MoveTo(outerLeft, outerTop)
			r.dc.LineTo(outerRight, outerTop)
			r.dc.LineTo(innerRight, innerTop)
			r.dc.LineTo(innerLeft, innerTop)
			r.dc.ClosePath()
			r.dc.Fill()
		}
	}
	// Right border (index 1).
	if bw.Right > 0 && layer.BorderStyles[1] != css.BorderStyleNone {
		if c := layer.BorderColors[1]; c.A > 0 {
			r.setColor(c)
			r.dc.MoveTo(outerRight, outerTop)
			r.dc.LineTo(outerRight, outerBottom)
			r.dc.LineTo(innerRight, innerBottom)
			r.dc.LineTo(innerRight, innerTop)
			r.dc.ClosePath()
			r.dc.Fill()
		}
	}
	// Bottom border (index 2).
	if bw.Bottom > 0 && layer.BorderStyles[2] != css.BorderStyleNone {
		if c := layer.BorderColors[2]; c.A > 0 {
			r.setColor(c)
			r.dc.MoveTo(outerLeft, outerBottom)
			r.dc.LineTo(innerLeft, innerBottom)
			r.dc.LineTo(innerRight, innerBottom)
			r.dc.LineTo(outerRight, outerBottom)
			r.dc.ClosePath()
			r.dc.Fill()
		}
	}
	// Left border (index 3).
	if bw.Left > 0 && layer.BorderStyles[3] != css.BorderStyleNone {
		if c := layer.BorderColors[3]; c.A > 0 {
			r.setColor(c)
			r.dc.MoveTo(outerLeft, outerTop)
			r.dc.LineTo(innerLeft, innerTop)
			r.dc.LineTo(innerLeft, innerBottom)
			r.dc.LineTo(outerLeft, outerBottom)
			r.dc.ClosePath()
			r.dc.Fill()
		}
	}
}

// setColor sets the draw context color from a css.Color.
func (r *Renderer) setColor(c css.Color) {
	r.dc.SetColor(color.RGBA{
		R: c.R,
		G: c.G,
		B: c.B,
		A: uint8(c.A * 255),
	})
}

// drawText paints text content using pre-computed font/color properties.
func (r *Renderer) drawText(layer *PaintLayer) {
	box := layer.Box
	fontPath := r.fonts.FontPath(layer.FontBold, layer.FontItalic, layer.FontMono, layer.FontAhem)
	fontID := r.openFont(fontPath, layer.FontSize)
	if fontID < 0 {
		return
	}
	r.setColor(layer.TextColor)

	metrics := r.dc.GetFontMetrics(fontID)
	ascent := float64(metrics.Ascent) / 64.0

	// Sideways text: CSS Writing Modes §7.3.
	// sideways-rl / sideways-lr treat the entire line as horizontal text
	// rotated 90°.  The physical box already has Width=lineHeight,
	// Height=textAdvance.  Strategy: render the string horizontally into an
	// off-screen (textAdvance × lineHeight) buffer, then rotate the pixels
	// into a (lineHeight × textAdvance) destination and blit at box origin.
	if layer.IsSidewaysRL || layer.IsSidewaysLR {
		ta := int(math.Ceil(box.Height)) // text advance → off-screen width
		lh := int(math.Ceil(box.Width))  // line height  → off-screen height
		if ta <= 0 || lh <= 0 {
			return
		}

		// Draw horizontal text into an off-screen buffer.
		src := image.NewRGBA(image.Rect(0, 0, ta, lh))
		childDC := r.dc.NewChildContext(src)
		childDC.SetColor(color.RGBA{
			R: layer.TextColor.R,
			G: layer.TextColor.G,
			B: layer.TextColor.B,
			A: uint8(layer.TextColor.A * 255),
		})
		childDC.DrawText(box.Text, fontID, 0, ascent)

		// Rotate pixels 90° into destination (lh × ta) buffer.
		rot := image.NewRGBA(image.Rect(0, 0, lh, ta))
		for y := 0; y < lh; y++ {
			for x := 0; x < ta; x++ {
				c := src.RGBAAt(x, y)
				if c.A == 0 {
					continue
				}
				if layer.IsSidewaysRL {
					// 90° CW: (x,y) → (lh-1-y, x)
					rot.SetRGBA(lh-1-y, x, c)
				} else {
					// 90° CCW: (x,y) → (y, ta-1-x)
					rot.SetRGBA(y, ta-1-x, c)
				}
			}
		}

		r.dc.DrawImage(rot, int(math.Round(box.X)), int(math.Round(box.Y)))
		return
	}

	// Vertical text: draw each character stacked vertically.
	// CSS Writing Modes §5.1: in vertical writing modes, upright glyphs
	// advance in the block-progression (vertical) direction. For Ahem
	// (1em × 1em squares) and other upright text, each character cell
	// is fontSize tall and the glyph is centered horizontally.
	if layer.IsVerticalText {
		y := box.Y
		for _, ch := range box.Text {
			charStr := string(ch)
			charW := r.dc.MeasureText(charStr, fontID)
			xOffset := (box.Width - charW) / 2
			if xOffset < 0 {
				xOffset = 0
			}
			r.dc.DrawText(charStr, fontID, box.X+xOffset, y+ascent)
			y += layer.FontSize
			if layer.LetterSpacing != 0 {
				y += layer.LetterSpacing
			}
		}
		return
	}

	if layer.LetterSpacing != 0 {
		x := box.X
		baselineY := box.Y + ascent
		for _, ch := range box.Text {
			r.dc.DrawText(string(ch), fontID, x, baselineY)
			charW := r.dc.MeasureText(string(ch), fontID)
			x += charW + layer.LetterSpacing
		}
	} else {
		r.dc.DrawText(box.Text, fontID, box.X, box.Y+ascent)
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

// hasOverflowClipping returns true if the box has overflow:hidden/scroll/auto.
func hasOverflowClipping(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	overflow := box.Style.GetOverflow()
	return overflow == css.OverflowHidden || overflow == css.OverflowScroll || overflow == css.OverflowAuto
}

// isContainedByOverflow returns true if the child is clipped by the parent's
// overflow and should NOT escape to the ancestor stacking context.
func isContainedByOverflow(child, parent *layout.Box) bool {
	if !hasOverflowClipping(parent) {
		return false
	}
	// position:relative children are always clipped by parent's overflow.
	if child.Position == css.PositionRelative {
		return true
	}
	// position:absolute children are clipped only when the parent is
	// positioned (i.e., is the containing block for the abs-pos child).
	if child.Position == css.PositionAbsolute && parent.Position != css.PositionStatic {
		return true
	}
	return false
}
