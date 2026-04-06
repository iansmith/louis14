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
		// CSS 2.1 §14.2: root element's background paints the entire canvas.
		layer.PaintsCanvasBackground = true
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

// hasBackground returns true if a box has a visible background
// (non-transparent background-color or a background-image/gradient).
func (r *Renderer) hasBackground(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	// Check background-color.
	if bgStr, ok := box.Style.Get("background-color"); ok && bgStr != "" && bgStr != "transparent" {
		if bgColor, ok := css.ParseColor(bgStr); ok && bgColor.A > 0 {
			return true
		}
	}
	// Check background-image (url or gradient).
	if _, ok := box.Style.GetBackgroundImage(); ok {
		return true
	}
	if val, ok := box.Style.Get("background-image"); ok && isGradientValue(val) {
		return true
	}
	return false
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

	// Apply CSS transform if present (wraps around opacity handling).
	if layer.HasTransform {
		r.dc.Push()
		r.applyTransforms(layer)
	}

	if layer.Opacity < 1.0 {
		// CSS3 Color §4.2: render subtree to offscreen buffer and composite.
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}

	if layer.HasTransform {
		r.dc.Pop()
	}
}

// applyTransforms applies the CSS transform list to the draw context.
// Transforms are applied relative to the transform-origin point.
func (r *Renderer) applyTransforms(layer *PaintLayer) {
	box := layer.Box
	ox := box.X + layer.TransformOrigin[0]
	oy := box.Y + layer.TransformOrigin[1]

	// Move origin to transform-origin point.
	r.dc.Translate(ox, oy)

	// Apply transforms in order.
	for _, t := range layer.Transforms {
		switch t.Type {
		case "translate":
			tx := t.Values[0]
			ty := 0.0
			if len(t.Values) > 1 {
				ty = t.Values[1]
			}
			r.dc.Translate(tx, ty)
		case "rotate":
			// parseAngle() returns degrees; DrawContext.Rotate() takes radians.
			r.dc.Rotate(t.Values[0] * math.Pi / 180)
		case "scale":
			sx := t.Values[0]
			sy := sx
			if len(t.Values) > 1 {
				sy = t.Values[1]
			}
			r.dc.Scale(sx, sy)
		case "skew":
			// Skew via matrix: [1, tan(ay), tan(ax), 1, 0, 0]
			// DrawContext doesn't have Skew directly; skip for MVP.
		case "matrix":
			// matrix(a, b, c, d, e, f)
			// DrawContext doesn't have a SetMatrix; skip for MVP.
		}
	}

	// Move back from transform-origin.
	r.dc.Translate(-ox, -oy)
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
		cx, cy, cw, ch := pixelSnap(layer.CSSClipRect[0], layer.CSSClipRect[1],
			layer.CSSClipRect[2], layer.CSSClipRect[3])
		r.dc.DrawRectangle(cx, cy, cw, ch)
		r.dc.Clip()
	}

	// Step 0: Box shadows (paint behind everything).
	if len(layer.BoxShadows) > 0 {
		r.drawBoxShadows(layer)
	}

	// Step 1: Background and borders.
	r.drawBackground(layer)
	r.drawBorders(layer)

	// Outline (outside border-box, doesn't affect layout).
	if layer.OutlineStyle != "none" && layer.OutlineWidth > 0 {
		r.drawOutline(layer)
	}

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
		ox, oy, ow, oh := pixelSnap(layer.ClipRect[0], layer.ClipRect[1],
			layer.ClipRect[2], layer.ClipRect[3])
		if hasBorderRadius(layer) {
			r.buildRoundedRectPath(ox, oy, ow, oh, layer.BorderRadius)
		} else {
			r.dc.DrawRectangle(ox, oy, ow, oh)
		}
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

// pixelSnap rounds a box's position and size to integer pixel boundaries.
// Width/height are computed by rounding the far edges to prevent cumulative
// rounding errors. This matches Blink's approach of snapping coordinates
// before painting to avoid sub-pixel boundary artifacts.
func pixelSnap(x, y, w, h float64) (float64, float64, float64, float64) {
	sx := math.Round(x)
	sy := math.Round(y)
	sw := math.Round(x+w) - sx
	sh := math.Round(y+h) - sy
	return sx, sy, sw, sh
}

// hasBorderRadius returns true if any corner radius is non-zero.
func hasBorderRadius(layer *PaintLayer) bool {
	return layer.BorderRadius != [4]float64{}
}

// buildRoundedRectPath traces a rounded rectangle path using QuadraticTo for corners.
// radii: [TopLeft, TopRight, BottomRight, BottomLeft].
func (r *Renderer) buildRoundedRectPath(x, y, w, h float64, radii [4]float64) {
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
	r.dc.MoveTo(x+tl, y)
	r.dc.LineTo(x+w-tr, y)
	r.dc.QuadraticTo(x+w, y, x+w, y+tr)     // top-right
	r.dc.LineTo(x+w, y+h-br)
	r.dc.QuadraticTo(x+w, y+h, x+w-br, y+h) // bottom-right
	r.dc.LineTo(x+bl, y+h)
	r.dc.QuadraticTo(x, y+h, x, y+h-bl)     // bottom-left
	r.dc.LineTo(x, y+tl)
	r.dc.QuadraticTo(x, y, x+tl, y)         // top-left
	r.dc.ClosePath()
}

// buildRoundedRectPathReverse traces a rounded rectangle path in reverse (CCW)
// for use with even-odd fill rule to cut out inner regions.
func (r *Renderer) buildRoundedRectPathReverse(x, y, w, h float64, radii [4]float64) {
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
	r.dc.MoveTo(x+tl, y)
	r.dc.QuadraticTo(x, y, x, y+tl)         // top-left (reverse)
	r.dc.LineTo(x, y+h-bl)
	r.dc.QuadraticTo(x, y+h, x+bl, y+h)     // bottom-left (reverse)
	r.dc.LineTo(x+w-br, y+h)
	r.dc.QuadraticTo(x+w, y+h, x+w, y+h-br) // bottom-right (reverse)
	r.dc.LineTo(x+w, y+tr)
	r.dc.QuadraticTo(x+w, y, x+w-tr, y)     // top-right (reverse)
	r.dc.ClosePath()
}

// drawBackground paints the layer's background color and image (pre-computed).
func (r *Renderer) drawBackground(layer *PaintLayer) {
	box := layer.Box
	sx, sy, sw, sh := pixelSnap(box.X, box.Y, box.Width, box.Height)

	// Background color.
	if c := layer.BackgroundColor; c.A > 0 {
		r.setColor(c)
		if hasBorderRadius(layer) {
			r.buildRoundedRectPath(sx, sy, sw, sh, layer.BorderRadius)
			r.dc.Fill()
		} else {
			r.dc.DrawRectangle(sx, sy, sw, sh)
			r.dc.Fill()
		}
	}

	// Background gradient (linear-gradient, etc.).
	if layer.BackgroundGradient != "" {
		if hasBorderRadius(layer) {
			r.dc.Push()
			r.buildRoundedRectPath(sx, sy, sw, sh, layer.BorderRadius)
			r.dc.Clip()
			r.drawLinearGradient(layer.BackgroundGradient, sx, sy, sw, sh)
			r.dc.Pop()
		} else {
			r.drawLinearGradient(layer.BackgroundGradient, sx, sy, sw, sh)
		}
	}

	// Background image.
	if layer.BackgroundImage != "" && r.imageFetcher != nil {
		if hasBorderRadius(layer) {
			r.dc.Push()
			r.buildRoundedRectPath(sx, sy, sw, sh, layer.BorderRadius)
			r.dc.Clip()
			r.drawBackgroundImage(layer)
			r.dc.Pop()
		} else {
			r.drawBackgroundImage(layer)
		}
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

	// CSS3 Backgrounds §3.6: background-origin defaults to padding-box.
	// The background image is positioned relative to the padding box, but
	// clipped to the border box (background-clip defaults to border-box).
	// Pixel-snap all coordinates to match the snapped background color fill.
	paddingX := math.Round(box.X + box.Border.Left)
	paddingY := math.Round(box.Y + box.Border.Top)
	paddingW := math.Round(box.X+box.Width-box.Border.Right) - paddingX
	paddingH := math.Round(box.Y+box.Height-box.Border.Bottom) - paddingY
	if paddingW < 0 {
		paddingW = 0
	}
	if paddingH < 0 {
		paddingH = 0
	}

	// CSS3 Backgrounds §3.9: Resolve background-size.
	bgSize := layer.BackgroundSize
	if bgSize.Cover || bgSize.Contain {
		if paddingW > 0 && paddingH > 0 && imgW > 0 && imgH > 0 {
			scaleX := paddingW / imgW
			scaleY := paddingH / imgH
			var scale float64
			if bgSize.Cover {
				scale = math.Max(scaleX, scaleY)
			} else {
				scale = math.Min(scaleX, scaleY)
			}
			imgW = math.Round(imgW * scale)
			imgH = math.Round(imgH * scale)
			img = scaleImageNearest(img, imgWI, imgHI, int(imgW), int(imgH))
			imgWI = int(imgW)
			imgHI = int(imgH)
		}
	} else if bgSize.Width != 0 || bgSize.Height != 0 {
		newW := imgW
		newH := imgH
		if bgSize.Width != 0 {
			if bgSize.Width < 0 {
				// Negative = percentage of padding-box width
				newW = math.Round(paddingW * (-bgSize.Width) / 100)
			} else {
				newW = math.Round(bgSize.Width)
			}
		}
		if bgSize.Height != 0 {
			if bgSize.Height < 0 {
				// Negative = percentage of padding-box height
				newH = math.Round(paddingH * (-bgSize.Height) / 100)
			} else {
				newH = math.Round(bgSize.Height)
			}
		}
		// Handle auto dimension: maintain aspect ratio
		if bgSize.Width != 0 && bgSize.Height == 0 && imgW > 0 {
			newH = math.Round(imgH * newW / imgW)
		} else if bgSize.Width == 0 && bgSize.Height != 0 && imgH > 0 {
			newW = math.Round(imgW * newH / imgH)
		}
		if newW > 0 && newH > 0 && (int(newW) != imgWI || int(newH) != imgHI) {
			img = scaleImageNearest(img, imgWI, imgHI, int(newW), int(newH))
			imgW = newW
			imgH = newH
			imgWI = int(newW)
			imgHI = int(newH)
		}
	}

	pos := layer.BackgroundPosition
	startX := paddingX + pos.ResolveX(paddingW, imgW)
	startY := paddingY + pos.ResolveY(paddingH, imgH)

	repeat := layer.BackgroundRepeat
	repeatX := repeat == css.BackgroundRepeatRepeat || repeat == css.BackgroundRepeatRepeatX
	repeatY := repeat == css.BackgroundRepeatRepeat || repeat == css.BackgroundRepeatRepeatY

	// Clip bounds: normally the border box, but for the root element's
	// background CSS 2.1 §14.2 says the background paints the entire canvas.
	var boxX0, boxY0, boxX1, boxY1 int
	if layer.PaintsCanvasBackground {
		bounds := r.target.Bounds()
		boxX0 = bounds.Min.X
		boxY0 = bounds.Min.Y
		boxX1 = bounds.Max.X
		boxY1 = bounds.Max.Y
	} else {
		boxX0 = int(math.Round(box.X))
		boxY0 = int(math.Round(box.Y))
		boxX1 = int(math.Round(box.X + box.Width))
		boxY1 = int(math.Round(box.Y + box.Height))
	}

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

	// Rounded borders: delegate to specialized method.
	if hasBorderRadius(layer) {
		r.drawRoundedBorders(layer)
		return
	}

	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)
	outerLeft, outerTop := x, y
	outerRight, outerBottom := x+w, y+h
	innerLeft := math.Round(x + bw.Left)
	innerTop := math.Round(y + bw.Top)
	innerRight := math.Round(x + w - bw.Right)
	innerBottom := math.Round(y + h - bw.Bottom)

	// Top border (index 0).
	if bw.Top > 0 && layer.BorderStyles[0] != css.BorderStyleNone {
		if c := layer.BorderColors[0]; c.A > 0 {
			r.setColor(c)
			switch layer.BorderStyles[0] {
			case css.BorderStyleDashed:
				midY := (outerTop + innerTop) / 2
				r.drawDashedLine(outerLeft, midY, outerRight, midY, bw.Top)
			case css.BorderStyleDotted:
				midY := (outerTop + innerTop) / 2
				r.drawDottedLine(outerLeft, midY, outerRight, midY, bw.Top)
			case css.BorderStyleDouble:
				midY := (outerTop + innerTop) / 2
				r.drawDoubleLine(outerLeft, midY, outerRight, midY, bw.Top)
			default: // Solid, Hidden, Groove, Ridge, Inset, Outset — trapezoid
				r.dc.MoveTo(outerLeft, outerTop)
				r.dc.LineTo(outerRight, outerTop)
				r.dc.LineTo(innerRight, innerTop)
				r.dc.LineTo(innerLeft, innerTop)
				r.dc.ClosePath()
				r.dc.Fill()
			}
		}
	}
	// Right border (index 1).
	if bw.Right > 0 && layer.BorderStyles[1] != css.BorderStyleNone {
		if c := layer.BorderColors[1]; c.A > 0 {
			r.setColor(c)
			switch layer.BorderStyles[1] {
			case css.BorderStyleDashed:
				midX := (outerRight + innerRight) / 2
				r.drawDashedLine(midX, outerTop, midX, outerBottom, bw.Right)
			case css.BorderStyleDotted:
				midX := (outerRight + innerRight) / 2
				r.drawDottedLine(midX, outerTop, midX, outerBottom, bw.Right)
			case css.BorderStyleDouble:
				midX := (outerRight + innerRight) / 2
				r.drawDoubleLine(midX, outerTop, midX, outerBottom, bw.Right)
			default:
				r.dc.MoveTo(outerRight, outerTop)
				r.dc.LineTo(outerRight, outerBottom)
				r.dc.LineTo(innerRight, innerBottom)
				r.dc.LineTo(innerRight, innerTop)
				r.dc.ClosePath()
				r.dc.Fill()
			}
		}
	}
	// Bottom border (index 2).
	if bw.Bottom > 0 && layer.BorderStyles[2] != css.BorderStyleNone {
		if c := layer.BorderColors[2]; c.A > 0 {
			r.setColor(c)
			switch layer.BorderStyles[2] {
			case css.BorderStyleDashed:
				midY := (outerBottom + innerBottom) / 2
				r.drawDashedLine(outerLeft, midY, outerRight, midY, bw.Bottom)
			case css.BorderStyleDotted:
				midY := (outerBottom + innerBottom) / 2
				r.drawDottedLine(outerLeft, midY, outerRight, midY, bw.Bottom)
			case css.BorderStyleDouble:
				midY := (outerBottom + innerBottom) / 2
				r.drawDoubleLine(outerLeft, midY, outerRight, midY, bw.Bottom)
			default:
				r.dc.MoveTo(outerLeft, outerBottom)
				r.dc.LineTo(innerLeft, innerBottom)
				r.dc.LineTo(innerRight, innerBottom)
				r.dc.LineTo(outerRight, outerBottom)
				r.dc.ClosePath()
				r.dc.Fill()
			}
		}
	}
	// Left border (index 3).
	if bw.Left > 0 && layer.BorderStyles[3] != css.BorderStyleNone {
		if c := layer.BorderColors[3]; c.A > 0 {
			r.setColor(c)
			switch layer.BorderStyles[3] {
			case css.BorderStyleDashed:
				midX := (outerLeft + innerLeft) / 2
				r.drawDashedLine(midX, outerTop, midX, outerBottom, bw.Left)
			case css.BorderStyleDotted:
				midX := (outerLeft + innerLeft) / 2
				r.drawDottedLine(midX, outerTop, midX, outerBottom, bw.Left)
			case css.BorderStyleDouble:
				midX := (outerLeft + innerLeft) / 2
				r.drawDoubleLine(midX, outerTop, midX, outerBottom, bw.Left)
			default:
				r.dc.MoveTo(outerLeft, outerTop)
				r.dc.LineTo(innerLeft, innerTop)
				r.dc.LineTo(innerLeft, innerBottom)
				r.dc.LineTo(outerLeft, outerBottom)
				r.dc.ClosePath()
				r.dc.Fill()
			}
		}
	}
}

// drawRoundedBorders draws borders for a box with border-radius.
func (r *Renderer) drawRoundedBorders(layer *PaintLayer) {
	box := layer.Box
	bw := box.Border
	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)

	// Check if all sides have the same width, style, and color (uniform case).
	uniform := bw.Top == bw.Right && bw.Right == bw.Bottom && bw.Bottom == bw.Left &&
		layer.BorderStyles[0] == layer.BorderStyles[1] &&
		layer.BorderStyles[1] == layer.BorderStyles[2] &&
		layer.BorderStyles[2] == layer.BorderStyles[3] &&
		layer.BorderColors[0] == layer.BorderColors[1] &&
		layer.BorderColors[1] == layer.BorderColors[2] &&
		layer.BorderColors[2] == layer.BorderColors[3]

	if uniform && bw.Top > 0 && layer.BorderStyles[0] != css.BorderStyleNone {
		// Simple case: draw a single stroked rounded rect at the midline.
		hw := bw.Top / 2
		midRadii := [4]float64{
			math.Max(0, layer.BorderRadius[0]-hw),
			math.Max(0, layer.BorderRadius[1]-hw),
			math.Max(0, layer.BorderRadius[2]-hw),
			math.Max(0, layer.BorderRadius[3]-hw),
		}
		r.setColor(layer.BorderColors[0])
		r.dc.SetLineWidth(bw.Top)
		r.buildRoundedRectPath(x+hw, y+hw, w-bw.Top, h-bw.Top, midRadii)
		r.dc.Stroke()
		return
	}

	// Non-uniform borders with radius: draw each side using even-odd fill
	// between outer and inner rounded rects, clipped to each side's region.
	outerRadii := layer.BorderRadius

	// Inner rounded rect (border-box inset by border widths).
	ix := x + bw.Left
	iy := y + bw.Top
	iw := w - bw.Left - bw.Right
	ih := h - bw.Top - bw.Bottom
	innerRadii := [4]float64{
		math.Max(0, outerRadii[0]-math.Max(bw.Left, bw.Top)),
		math.Max(0, outerRadii[1]-math.Max(bw.Right, bw.Top)),
		math.Max(0, outerRadii[2]-math.Max(bw.Right, bw.Bottom)),
		math.Max(0, outerRadii[3]-math.Max(bw.Left, bw.Bottom)),
	}

	type borderSide struct {
		width float64
		style css.BorderStyle
		color css.Color
		clipX, clipY, clipW, clipH float64
	}

	// Compute clip regions for each side.
	sides := [4]borderSide{
		{ // Top
			width: bw.Top, style: layer.BorderStyles[0], color: layer.BorderColors[0],
			clipX: x, clipY: y,
			clipW: w,
			clipH: math.Max(bw.Top, math.Max(outerRadii[0], outerRadii[1])),
		},
		{ // Right
			width: bw.Right, style: layer.BorderStyles[1], color: layer.BorderColors[1],
			clipX: x + w - math.Max(bw.Right, math.Max(outerRadii[1], outerRadii[2])),
			clipY: y,
			clipW: math.Max(bw.Right, math.Max(outerRadii[1], outerRadii[2])),
			clipH: h,
		},
		{ // Bottom
			width: bw.Bottom, style: layer.BorderStyles[2], color: layer.BorderColors[2],
			clipX: x,
			clipY: y + h - math.Max(bw.Bottom, math.Max(outerRadii[2], outerRadii[3])),
			clipW: w,
			clipH: math.Max(bw.Bottom, math.Max(outerRadii[2], outerRadii[3])),
		},
		{ // Left
			width: bw.Left, style: layer.BorderStyles[3], color: layer.BorderColors[3],
			clipX: x, clipY: y,
			clipW: math.Max(bw.Left, math.Max(outerRadii[0], outerRadii[3])),
			clipH: h,
		},
	}

	for _, side := range sides {
		if side.width <= 0 || side.style == css.BorderStyleNone || side.color.A <= 0 {
			continue
		}
		r.dc.Push()
		r.dc.DrawRectangle(side.clipX, side.clipY, side.clipW, side.clipH)
		r.dc.Clip()
		r.setColor(side.color)
		// Outer path (CW) + inner path (CCW) with even-odd fill.
		r.buildRoundedRectPath(x, y, w, h, outerRadii)
		if iw > 0 && ih > 0 {
			r.buildRoundedRectPathReverse(ix, iy, iw, ih, innerRadii)
		}
		r.dc.SetFillRule(textshape.FillRuleEvenOdd)
		r.dc.Fill()
		r.dc.SetFillRule(textshape.FillRuleWinding)
		r.dc.Pop()
	}
}

// drawOutline draws the CSS outline around the border-box, offset by outline-offset.
func (r *Renderer) drawOutline(layer *PaintLayer) {
	box := layer.Box
	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)

	// Outline is drawn at: border-box + offset + width/2 (stroke centered on path).
	off := layer.OutlineOffset + layer.OutlineWidth/2
	ox := x - off
	oy := y - off
	ow := w + 2*off
	oh := h + 2*off

	if ow <= 0 || oh <= 0 {
		return
	}

	r.setColor(layer.OutlineColor)

	switch layer.OutlineStyle {
	case "solid":
		r.dc.SetLineWidth(layer.OutlineWidth)
		if hasBorderRadius(layer) {
			expandedRadii := expandRadii(layer.BorderRadius, off)
			r.buildRoundedRectPath(ox, oy, ow, oh, expandedRadii)
		} else {
			r.dc.DrawRectangle(ox, oy, ow, oh)
		}
		r.dc.Stroke()
	case "dashed":
		midOff := layer.OutlineOffset + layer.OutlineWidth/2
		mx, my := x-midOff, y-midOff
		mw, mh := w+2*midOff, h+2*midOff
		r.drawDashedLine(mx, my, mx+mw, my, layer.OutlineWidth)       // top
		r.drawDashedLine(mx+mw, my, mx+mw, my+mh, layer.OutlineWidth) // right
		r.drawDashedLine(mx, my+mh, mx+mw, my+mh, layer.OutlineWidth) // bottom
		r.drawDashedLine(mx, my, mx, my+mh, layer.OutlineWidth)       // left
	case "dotted":
		midOff := layer.OutlineOffset + layer.OutlineWidth/2
		mx, my := x-midOff, y-midOff
		mw, mh := w+2*midOff, h+2*midOff
		r.drawDottedLine(mx, my, mx+mw, my, layer.OutlineWidth)
		r.drawDottedLine(mx+mw, my, mx+mw, my+mh, layer.OutlineWidth)
		r.drawDottedLine(mx, my+mh, mx+mw, my+mh, layer.OutlineWidth)
		r.drawDottedLine(mx, my, mx, my+mh, layer.OutlineWidth)
	case "double":
		midOff := layer.OutlineOffset + layer.OutlineWidth/2
		mx, my := x-midOff, y-midOff
		mw, mh := w+2*midOff, h+2*midOff
		r.drawDoubleLine(mx, my, mx+mw, my, layer.OutlineWidth)
		r.drawDoubleLine(mx+mw, my, mx+mw, my+mh, layer.OutlineWidth)
		r.drawDoubleLine(mx, my+mh, mx+mw, my+mh, layer.OutlineWidth)
		r.drawDoubleLine(mx, my, mx, my+mh, layer.OutlineWidth)
	default:
		// Treat unknown styles as solid.
		r.dc.SetLineWidth(layer.OutlineWidth)
		r.dc.DrawRectangle(ox, oy, ow, oh)
		r.dc.Stroke()
	}
}

// expandRadii expands border radii by the given amount for outline/shadow shapes.
func expandRadii(radii [4]float64, amount float64) [4]float64 {
	return [4]float64{
		math.Max(0, radii[0]+amount),
		math.Max(0, radii[1]+amount),
		math.Max(0, radii[2]+amount),
		math.Max(0, radii[3]+amount),
	}
}

// drawDashedLine draws a dashed line from (x1,y1) to (x2,y2) with the given width.
// Dash length and gap are both 3× the border width per CSS spec.
func (r *Renderer) drawDashedLine(x1, y1, x2, y2, width float64) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}

	dashLen := width * 3
	gapLen := width * 3
	ux, uy := dx/length, dy/length
	nx, ny := -uy, ux // perpendicular
	hw := width / 2

	along := 0.0
	for along < length {
		segEnd := along + dashLen
		if segEnd > length {
			segEnd = length
		}

		sx, sy := x1+ux*along, y1+uy*along
		ex, ey := x1+ux*segEnd, y1+uy*segEnd

		r.dc.MoveTo(sx+nx*hw, sy+ny*hw)
		r.dc.LineTo(ex+nx*hw, ey+ny*hw)
		r.dc.LineTo(ex-nx*hw, ey-ny*hw)
		r.dc.LineTo(sx-nx*hw, sy-ny*hw)
		r.dc.ClosePath()
		r.dc.Fill()

		along = segEnd + gapLen
	}
}

// drawDottedLine draws a dotted line from (x1,y1) to (x2,y2) with the given width.
// Dot diameter equals the border width; gap equals the border width.
func (r *Renderer) drawDottedLine(x1, y1, x2, y2, width float64) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}

	dotRadius := width / 2
	spacing := width * 2 // dot diameter + gap
	ux, uy := dx/length, dy/length

	along := dotRadius
	for along < length {
		cx := x1 + ux*along
		cy := y1 + uy*along
		r.dc.DrawCircle(cx, cy, dotRadius)
		r.dc.Fill()
		along += spacing
	}
}

// drawDoubleLine draws two parallel lines from (x1,y1) to (x2,y2).
// Each line is width/3 thick with a width/3 gap between them.
// Falls back to solid for borders thinner than 3px.
func (r *Renderer) drawDoubleLine(x1, y1, x2, y2, width float64) {
	if width < 3 {
		// Too thin for double — draw as solid line
		r.drawSolidLine(x1, y1, x2, y2, width)
		return
	}
	nx, ny := -(y2 - y1), (x2 - x1)
	length := math.Sqrt(nx*nx + ny*ny)
	if length == 0 {
		return
	}
	nx, ny = nx/length, ny/length

	lineW := width / 3
	offset := width/2 - lineW/2
	// Outer line
	r.drawSolidLine(x1+nx*offset, y1+ny*offset, x2+nx*offset, y2+ny*offset, lineW)
	// Inner line
	r.drawSolidLine(x1-nx*offset, y1-ny*offset, x2-nx*offset, y2-ny*offset, lineW)
}

// drawSolidLine draws a filled rectangle along a line from (x1,y1) to (x2,y2) with the given width.
func (r *Renderer) drawSolidLine(x1, y1, x2, y2, width float64) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}
	nx, ny := -dy/length, dx/length
	hw := width / 2

	r.dc.MoveTo(x1+nx*hw, y1+ny*hw)
	r.dc.LineTo(x2+nx*hw, y2+ny*hw)
	r.dc.LineTo(x2-nx*hw, y2-ny*hw)
	r.dc.LineTo(x1-nx*hw, y1-ny*hw)
	r.dc.ClosePath()
	r.dc.Fill()
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

	fontPath := r.fonts.FontPathForFamily(layer.FontFamily, layer.FontBold, layer.FontItalic, layer.FontMono, layer.FontAhem)
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

	// Draw text decoration lines (underline, overline, line-through).
	r.drawTextDecoration(layer, box, fontID, ascent)
}

// drawTextDecoration renders underline, overline, or line-through decoration
// lines for non-vertical, non-sideways text.
func (r *Renderer) drawTextDecoration(layer *PaintLayer, box *layout.Box, fontID int32, ascent float64) {
	if layer.TextDecoration == css.TextDecorationNone || layer.TextDecoration == "" {
		return
	}

	metrics := r.dc.GetFontMetrics(fontID)
	descent := float64(metrics.Descent) / 64.0
	textWidth := r.dc.MeasureText(box.Text, fontID)

	r.setColor(layer.TextDecorationColor)
	r.dc.SetLineWidth(layer.TextDecorationThickness)

	var lineY float64
	switch layer.TextDecoration {
	case css.TextDecorationUnderline:
		lineY = box.Y + ascent + math.Abs(descent)*0.25
	case css.TextDecorationOverline:
		lineY = box.Y
	case css.TextDecorationLineThrough:
		lineY = box.Y + ascent*0.65
	default:
		return
	}

	r.dc.MoveTo(box.X, lineY)
	r.dc.LineTo(box.X+textWidth, lineY)
	r.dc.Stroke()
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

// drawBoxShadows paints outset box shadows behind the element.
// Shadows are painted in reverse declaration order (last = behind).
func (r *Renderer) drawBoxShadows(layer *PaintLayer) {
	box := layer.Box
	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)

	for i := len(layer.BoxShadows) - 1; i >= 0; i-- {
		shadow := layer.BoxShadows[i]
		if shadow.Inset {
			continue // Skip inset shadows for now
		}

		// Shadow rectangle: offset by (offsetX, offsetY), expanded by spread.
		sx := x + shadow.OffsetX - shadow.Spread
		sy := y + shadow.OffsetY - shadow.Spread
		sw := w + 2*shadow.Spread
		sh := h + 2*shadow.Spread

		if sw <= 0 || sh <= 0 {
			continue
		}

		r.setColor(shadow.Color)

		if hasBorderRadius(layer) {
			// Expand radii by spread amount for shadow shape.
			shadowRadii := [4]float64{
				math.Max(0, layer.BorderRadius[0]+shadow.Spread),
				math.Max(0, layer.BorderRadius[1]+shadow.Spread),
				math.Max(0, layer.BorderRadius[2]+shadow.Spread),
				math.Max(0, layer.BorderRadius[3]+shadow.Spread),
			}
			if shadow.Blur > 0 {
				r.drawBlurredShadow(sx, sy, sw, sh, shadowRadii, shadow)
			} else {
				r.buildRoundedRectPath(sx, sy, sw, sh, shadowRadii)
				r.dc.Fill()
			}
		} else {
			if shadow.Blur > 0 {
				r.drawBlurredShadow(sx, sy, sw, sh, [4]float64{}, shadow)
			} else {
				r.dc.DrawRectangle(sx, sy, sw, sh)
				r.dc.Fill()
			}
		}
	}
}

// drawBlurredShadow renders a shadow shape to an offscreen buffer, applies
// a 3-pass box blur (approximating Gaussian blur), then composites back.
func (r *Renderer) drawBlurredShadow(sx, sy, sw, sh float64, radii [4]float64, shadow css.BoxShadow) {
	// CSS box-shadow blur radius = 2*sigma, so sigma = blur/2.
	// Extend buffer by 3*sigma on each side for the blur kernel.
	sigma := shadow.Blur / 2
	extend := math.Ceil(sigma * 3)

	bx := sx - extend
	by := sy - extend
	bw := int(math.Ceil(sw + 2*extend))
	bh := int(math.Ceil(sh + 2*extend))

	if bw <= 0 || bh <= 0 {
		return
	}

	// Cap buffer size to prevent OOM on huge shadows.
	if bw > 2000 || bh > 2000 {
		// Fall back to non-blurred shadow.
		r.setColor(shadow.Color)
		if radii != [4]float64{} {
			r.buildRoundedRectPath(sx, sy, sw, sh, radii)
			r.dc.Fill()
		} else {
			r.dc.DrawRectangle(sx, sy, sw, sh)
			r.dc.Fill()
		}
		return
	}

	// Create offscreen buffer.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	childDC := r.dc.NewChildContext(buf)

	// Draw shadow shape into buffer at local coordinates.
	localX := sx - bx
	localY := sy - by
	childDC.SetColor(color.RGBA{
		R: shadow.Color.R,
		G: shadow.Color.G,
		B: shadow.Color.B,
		A: uint8(shadow.Color.A * 255),
	})
	if radii != [4]float64{} {
		tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
		childDC.MoveTo(localX+tl, localY)
		childDC.LineTo(localX+sw-tr, localY)
		childDC.QuadraticTo(localX+sw, localY, localX+sw, localY+tr)
		childDC.LineTo(localX+sw, localY+sh-br)
		childDC.QuadraticTo(localX+sw, localY+sh, localX+sw-br, localY+sh)
		childDC.LineTo(localX+bl, localY+sh)
		childDC.QuadraticTo(localX, localY+sh, localX, localY+sh-bl)
		childDC.LineTo(localX, localY+tl)
		childDC.QuadraticTo(localX, localY, localX+tl, localY)
		childDC.ClosePath()
		childDC.Fill()
	} else {
		childDC.DrawRectangle(localX, localY, sw, sh)
		childDC.Fill()
	}

	// Apply 3-pass box blur (approximates Gaussian).
	boxBlur(buf, int(math.Round(sigma)))
	boxBlur(buf, int(math.Round(sigma)))
	boxBlur(buf, int(math.Round(sigma)))

	// Composite blurred buffer back to main canvas.
	r.dc.DrawImage(buf, int(math.Round(bx)), int(math.Round(by)))
}

// boxBlur applies a separable box blur (horizontal then vertical pass)
// to an RGBA image. The kernel size is (2*radius+1).
func boxBlur(img *image.RGBA, radius int) {
	if radius <= 0 {
		return
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return
	}

	stride := img.Stride

	// Temporary buffer for intermediate results.
	tmp := make([]uint8, len(img.Pix))

	// Horizontal pass: read from img.Pix, write to tmp.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rSum, gSum, bSum, aSum uint32
			count := uint32(0)
			for kx := -radius; kx <= radius; kx++ {
				sx := x + kx
				if sx < 0 || sx >= w {
					continue
				}
				off := y*stride + sx*4
				rSum += uint32(img.Pix[off+0])
				gSum += uint32(img.Pix[off+1])
				bSum += uint32(img.Pix[off+2])
				aSum += uint32(img.Pix[off+3])
				count++
			}
			off := y*stride + x*4
			tmp[off+0] = uint8(rSum / count)
			tmp[off+1] = uint8(gSum / count)
			tmp[off+2] = uint8(bSum / count)
			tmp[off+3] = uint8(aSum / count)
		}
	}

	// Vertical pass: read from tmp, write back to img.Pix.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rSum, gSum, bSum, aSum uint32
			count := uint32(0)
			for ky := -radius; ky <= radius; ky++ {
				sy := y + ky
				if sy < 0 || sy >= h {
					continue
				}
				off := sy*stride + x*4
				rSum += uint32(tmp[off+0])
				gSum += uint32(tmp[off+1])
				bSum += uint32(tmp[off+2])
				aSum += uint32(tmp[off+3])
				count++
			}
			off := y*stride + x*4
			img.Pix[off+0] = uint8(rSum / count)
			img.Pix[off+1] = uint8(gSum / count)
			img.Pix[off+2] = uint8(bSum / count)
			img.Pix[off+3] = uint8(aSum / count)
		}
	}
}
