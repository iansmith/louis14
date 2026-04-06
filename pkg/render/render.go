package render

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strings"
	"sync"
	"unicode"

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

	// CSS Filters: render subtree to offscreen buffer, apply filters, composite.
	if layer.HasFilter {
		r.paintLayerWithFilter(layer)
		return
	}

	// CSS mix-blend-mode: render to offscreen buffer and blend-composite.
	if layer.BlendMode != css.MixBlendModeNormal && layer.BlendMode != "" {
		r.paintLayerWithBlend(layer)
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

// paintLayerWithFilter renders the entire layer subtree into an offscreen
// buffer, applies CSS filter effects, and composites the result back.
func (r *Renderer) paintLayerWithFilter(layer *PaintLayer) {
	box := layer.Box

	// Determine buffer bounds. For blur filters, we need extra padding.
	blurExtend := 0.0
	for _, f := range layer.Filters {
		if f.Name == "blur" {
			sigma := f.Value / 2
			blurExtend = math.Max(blurExtend, math.Ceil(sigma*3))
		}
	}

	// Buffer covers the element's border-box plus blur extension.
	bx := int(math.Floor(box.X - blurExtend))
	by := int(math.Floor(box.Y - blurExtend))
	bw := int(math.Ceil(box.Width + 2*blurExtend))
	bh := int(math.Ceil(box.Height + 2*blurExtend))

	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		// Fallback: render without filters to avoid OOM.
		r.paintLayerContent(layer)
		return
	}

	// Save original state and render into offscreen buffer.
	origDC := r.dc
	origTarget := r.target

	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	childDC := origDC.NewChildContext(buf)
	r.dc = childDC
	r.target = buf

	// Offset so painting coordinates map to buffer-local coordinates.
	r.dc.Translate(float64(-bx), float64(-by))

	// Paint the layer content (including transforms, opacity, children).
	if layer.HasTransform {
		r.dc.Push()
		r.applyTransforms(layer)
	}
	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}
	if layer.HasTransform {
		r.dc.Pop()
	}

	// Restore original DC.
	r.dc = origDC
	r.target = origTarget

	// Apply filter operations to the buffer.
	for _, f := range layer.Filters {
		switch f.Name {
		case "blur":
			sigma := f.Value / 2
			radius := int(math.Round(sigma))
			if radius > 0 {
				boxBlur(buf, radius)
				boxBlur(buf, radius)
				boxBlur(buf, radius)
			}
		case "grayscale":
			applyGrayscale(buf, clampFilter01(f.Value))
		case "brightness":
			applyBrightness(buf, f.Value)
		case "contrast":
			applyContrast(buf, f.Value)
		case "opacity":
			applyFilterOpacity(buf, clampFilter01(f.Value))
		case "saturate":
			applySaturate(buf, f.Value)
		case "sepia":
			applySepia(buf, clampFilter01(f.Value))
		case "invert":
			applyInvert(buf, clampFilter01(f.Value))
		case "hue-rotate":
			applyHueRotate(buf, f.Value)
		case "drop-shadow":
			applyDropShadow(buf, f)
		}
	}

	// Composite filtered buffer back to main canvas.
	r.dc.DrawImage(buf, bx, by)
}

// paintLayerWithBlend renders the entire layer subtree into an offscreen
// buffer, then blend-composites the result onto the destination using the
// specified CSS mix-blend-mode.
func (r *Renderer) paintLayerWithBlend(layer *PaintLayer) {
	box := layer.Box
	bx := int(math.Floor(box.X))
	by := int(math.Floor(box.Y))
	bw := int(math.Ceil(box.Width+(box.X-float64(bx)))) + 1
	bh := int(math.Ceil(box.Height+(box.Y-float64(by)))) + 1

	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		r.paintLayerContent(layer)
		return
	}

	// Render to offscreen buffer.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	origDC := r.dc
	origTarget := r.target
	childDC := origDC.NewChildContext(buf)
	r.dc = childDC
	r.target = buf
	r.dc.Translate(float64(-bx), float64(-by))

	// Apply transforms and opacity within the offscreen buffer.
	if layer.HasTransform {
		r.dc.Push()
		r.applyTransforms(layer)
	}
	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}
	if layer.HasTransform {
		r.dc.Pop()
	}

	// Restore original DC.
	r.dc = origDC
	r.target = origTarget

	// Blend-composite the buffer onto the destination.
	blendComposite(r.target, buf, bx, by, layer.BlendMode)
}

// blendComposite performs pixel-level blend compositing of src onto dst
// at offset (ox, oy) using the specified CSS mix-blend-mode.
func blendComposite(dst *image.RGBA, src *image.RGBA, ox, oy int, mode css.MixBlendMode) {
	srcBounds := src.Bounds()
	dstBounds := dst.Bounds()

	for sy := 0; sy < srcBounds.Dy(); sy++ {
		dy := sy + oy
		if dy < dstBounds.Min.Y || dy >= dstBounds.Max.Y {
			continue
		}
		for sx := 0; sx < srcBounds.Dx(); sx++ {
			dx := sx + ox
			if dx < dstBounds.Min.X || dx >= dstBounds.Max.X {
				continue
			}

			srcOff := sy*src.Stride + sx*4
			dstOff := (dy-dstBounds.Min.Y)*dst.Stride + (dx-dstBounds.Min.X)*4

			sa := float64(src.Pix[srcOff+3]) / 255
			if sa == 0 {
				continue
			}

			sr := float64(src.Pix[srcOff]) / 255
			sg := float64(src.Pix[srcOff+1]) / 255
			sb := float64(src.Pix[srcOff+2]) / 255
			dr := float64(dst.Pix[dstOff]) / 255
			dg := float64(dst.Pix[dstOff+1]) / 255
			db := float64(dst.Pix[dstOff+2]) / 255

			var rr, rg, rb float64
			switch mode {
			case css.MixBlendModeMultiply:
				rr, rg, rb = sr*dr, sg*dg, sb*db
			case css.MixBlendModeScreen:
				rr = sr + dr - sr*dr
				rg = sg + dg - sg*dg
				rb = sb + db - sb*db
			case css.MixBlendModeOverlay:
				rr = overlayChannel(dr, sr)
				rg = overlayChannel(dg, sg)
				rb = overlayChannel(db, sb)
			case css.MixBlendModeDarken:
				rr = math.Min(sr, dr)
				rg = math.Min(sg, dg)
				rb = math.Min(sb, db)
			case css.MixBlendModeLighten:
				rr = math.Max(sr, dr)
				rg = math.Max(sg, dg)
				rb = math.Max(sb, db)
			case css.MixBlendModeDifference:
				rr = math.Abs(sr - dr)
				rg = math.Abs(sg - dg)
				rb = math.Abs(sb - db)
			default:
				rr, rg, rb = sr, sg, sb
			}

			// Source-over compositing with blended color.
			da := float64(dst.Pix[dstOff+3]) / 255
			outA := sa + da*(1-sa)
			if outA > 0 {
				dst.Pix[dstOff] = clampByte((rr*sa + dr*da*(1-sa)) / outA * 255)
				dst.Pix[dstOff+1] = clampByte((rg*sa + dg*da*(1-sa)) / outA * 255)
				dst.Pix[dstOff+2] = clampByte((rb*sa + db*da*(1-sa)) / outA * 255)
				dst.Pix[dstOff+3] = clampByte(outA * 255)
			}
		}
	}
}

// overlayChannel implements the CSS overlay blend formula for a single channel.
func overlayChannel(backdrop, source float64) float64 {
	if backdrop < 0.5 {
		return 2 * backdrop * source
	}
	return 1 - 2*(1-backdrop)*(1-source)
}

// clampFilter01 clamps a value to the [0, 1] range.
func clampFilter01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// clampByte clamps a float64 to [0, 255] and returns a uint8.
func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// applyGrayscale applies a grayscale filter to an RGBA image.
// amount=1 is fully grayscale, amount=0 is no change.
func applyGrayscale(img *image.RGBA, amount float64) {
	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] == 0 {
			continue
		}
		r, g, b := float64(pix[i]), float64(pix[i+1]), float64(pix[i+2])
		lum := 0.2126*r + 0.7152*g + 0.0722*b
		pix[i] = clampByte(r*(1-amount) + lum*amount)
		pix[i+1] = clampByte(g*(1-amount) + lum*amount)
		pix[i+2] = clampByte(b*(1-amount) + lum*amount)
	}
}

// applyBrightness multiplies RGB channels by factor.
// factor=1 is no change, factor=0 is black, factor=2 is double brightness.
func applyBrightness(img *image.RGBA, factor float64) {
	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] == 0 {
			continue
		}
		pix[i] = clampByte(float64(pix[i]) * factor)
		pix[i+1] = clampByte(float64(pix[i+1]) * factor)
		pix[i+2] = clampByte(float64(pix[i+2]) * factor)
	}
}

// applyContrast adjusts contrast around the midpoint (127.5).
// factor=1 is no change, factor=0 is flat gray, factor>1 increases contrast.
func applyContrast(img *image.RGBA, factor float64) {
	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] == 0 {
			continue
		}
		pix[i] = clampByte((float64(pix[i])/255-0.5)*factor*255 + 127.5)
		pix[i+1] = clampByte((float64(pix[i+1])/255-0.5)*factor*255 + 127.5)
		pix[i+2] = clampByte((float64(pix[i+2])/255-0.5)*factor*255 + 127.5)
	}
}

// applyFilterOpacity multiplies the alpha channel by amount.
func applyFilterOpacity(img *image.RGBA, amount float64) {
	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		pix[i+3] = clampByte(float64(pix[i+3]) * amount)
	}
}

// applySaturate adjusts color saturation.
// factor=1 is no change, factor=0 is grayscale, factor>1 is over-saturated.
func applySaturate(img *image.RGBA, factor float64) {
	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] == 0 {
			continue
		}
		r, g, b := float64(pix[i]), float64(pix[i+1]), float64(pix[i+2])
		lum := 0.2126*r + 0.7152*g + 0.0722*b
		pix[i] = clampByte(lum + (r-lum)*factor)
		pix[i+1] = clampByte(lum + (g-lum)*factor)
		pix[i+2] = clampByte(lum + (b-lum)*factor)
	}
}

// applySepia applies a sepia tone filter.
// amount=1 is full sepia, amount=0 is no change.
func applySepia(img *image.RGBA, amount float64) {
	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] == 0 {
			continue
		}
		r, g, b := float64(pix[i]), float64(pix[i+1]), float64(pix[i+2])
		// Standard sepia matrix coefficients.
		sr := 0.393*r + 0.769*g + 0.189*b
		sg := 0.349*r + 0.686*g + 0.168*b
		sb := 0.272*r + 0.534*g + 0.131*b
		pix[i] = clampByte(r*(1-amount) + sr*amount)
		pix[i+1] = clampByte(g*(1-amount) + sg*amount)
		pix[i+2] = clampByte(b*(1-amount) + sb*amount)
	}
}

// applyInvert inverts RGB channels by the given amount.
// amount=1 is full inversion, amount=0 is no change.
func applyInvert(img *image.RGBA, amount float64) {
	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] == 0 {
			continue
		}
		r, g, b := float64(pix[i]), float64(pix[i+1]), float64(pix[i+2])
		pix[i] = clampByte(r*(1-amount) + (255-r)*amount)
		pix[i+1] = clampByte(g*(1-amount) + (255-g)*amount)
		pix[i+2] = clampByte(b*(1-amount) + (255-b)*amount)
	}
}

// applyHueRotate rotates the hue of all pixels by the given angle in degrees.
func applyHueRotate(img *image.RGBA, degrees float64) {
	// CSS filter hue-rotate uses the rotation matrix from the spec.
	// https://www.w3.org/TR/filter-effects-1/#funcdef-filter-hue-rotate
	rad := degrees * math.Pi / 180
	cosA := math.Cos(rad)
	sinA := math.Sin(rad)

	// Rotation matrix coefficients from the spec.
	m00 := 0.213 + cosA*0.787 - sinA*0.213
	m01 := 0.715 - cosA*0.715 - sinA*0.715
	m02 := 0.072 - cosA*0.072 + sinA*0.928
	m10 := 0.213 - cosA*0.213 + sinA*0.143
	m11 := 0.715 + cosA*0.285 + sinA*0.140
	m12 := 0.072 - cosA*0.072 - sinA*0.283
	m20 := 0.213 - cosA*0.213 - sinA*0.787
	m21 := 0.715 - cosA*0.715 + sinA*0.715
	m22 := 0.072 + cosA*0.928 + sinA*0.072

	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] == 0 {
			continue
		}
		r, g, b := float64(pix[i]), float64(pix[i+1]), float64(pix[i+2])
		pix[i] = clampByte(m00*r + m01*g + m02*b)
		pix[i+1] = clampByte(m10*r + m11*g + m12*b)
		pix[i+2] = clampByte(m20*r + m21*g + m22*b)
	}
}

// applyDropShadow creates a shadow of the element's alpha shape.
func applyDropShadow(img *image.RGBA, f css.FilterFunction) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	ox := int(math.Round(f.ShadowOffsetX))
	oy := int(math.Round(f.ShadowOffsetY))
	blurR := int(math.Round(f.ShadowBlur / 2))

	// Shadow color defaults to black if not specified.
	sr, sg, sb := uint8(0), uint8(0), uint8(0)
	if f.ShadowColor.R > 0 || f.ShadowColor.G > 0 || f.ShadowColor.B > 0 || f.ShadowColor.A > 0 {
		sr = uint8(f.ShadowColor.R)
		sg = uint8(f.ShadowColor.G)
		sb = uint8(f.ShadowColor.B)
	}

	// Create shadow image from alpha channel.
	shadow := image.NewRGBA(bounds)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcX := x - ox
			srcY := y - oy
			if srcX >= 0 && srcX < w && srcY >= 0 && srcY < h {
				si := srcY*img.Stride + srcX*4
				a := img.Pix[si+3]
				if a > 0 {
					di := y*shadow.Stride + x*4
					shadow.Pix[di] = sr
					shadow.Pix[di+1] = sg
					shadow.Pix[di+2] = sb
					shadow.Pix[di+3] = a
				}
			}
		}
	}

	// Blur the shadow.
	if blurR > 0 {
		boxBlur(shadow, blurR)
		boxBlur(shadow, blurR)
		boxBlur(shadow, blurR)
	}

	// Composite: shadow behind original content.
	// Draw shadow first, then overlay original on top.
	result := image.NewRGBA(bounds)
	copy(result.Pix, shadow.Pix)
	// Porter-Duff src-over compositing.
	for i := 0; i < len(img.Pix); i += 4 {
		sa := float64(img.Pix[i+3]) / 255
		if sa == 0 {
			continue
		}
		da := float64(result.Pix[i+3]) / 255
		outA := sa + da*(1-sa)
		if outA > 0 {
			result.Pix[i] = clampByte((float64(img.Pix[i])*sa + float64(result.Pix[i])*da*(1-sa)) / outA)
			result.Pix[i+1] = clampByte((float64(img.Pix[i+1])*sa + float64(result.Pix[i+1])*da*(1-sa)) / outA)
			result.Pix[i+2] = clampByte((float64(img.Pix[i+2])*sa + float64(result.Pix[i+2])*da*(1-sa)) / outA)
			result.Pix[i+3] = clampByte(outA * 255)
		}
	}
	copy(img.Pix, result.Pix)
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
			// skew(ax, ay) = matrix(1, tan(ay), tan(ax), 1, 0, 0)
			ax := t.Values[0] * math.Pi / 180
			ay := 0.0
			if len(t.Values) > 1 {
				ay = t.Values[1] * math.Pi / 180
			}
			r.dc.MultiplyMatrix(1, math.Tan(ay), math.Tan(ax), 1, 0, 0)
		case "matrix":
			// matrix(a, b, c, d, e, f) — general 2D affine transform
			if len(t.Values) >= 6 {
				r.dc.MultiplyMatrix(t.Values[0], t.Values[1], t.Values[2], t.Values[3], t.Values[4], t.Values[5])
			}
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

	// CSS clip-path: clips all content to a shape.
	clipPathActive := false
	if layer.ClipPath != nil {
		r.dc.Push()
		clipPathActive = true
		r.applyClipPath(layer)
	}

	// Step 0: Outset box shadows (paint behind everything).
	if len(layer.BoxShadows) > 0 {
		r.drawOutsetBoxShadows(layer)
	}

	// Step 1: Background and borders.
	r.drawBackground(layer)
	r.drawBorders(layer)

	// Step 1b: Inset box shadows (paint after background, inside borders).
	if len(layer.BoxShadows) > 0 {
		r.drawInsetBoxShadows(layer)
	}

	// Column rules (between multicol columns).
	if layer.IsMulticol && layer.ColumnRuleStyle != "none" && layer.ColumnRuleWidth > 0 && layer.ColumnCount > 1 {
		r.drawColumnRules(layer)
	}

	// Outline (outside border-box, doesn't affect layout).
	if layer.OutlineStyle != "none" && layer.OutlineWidth > 0 {
		r.drawOutline(layer)
	}

	// List markers (disc, circle, square, decimal, or custom ::marker content).
	if layer.IsListItem && (layer.ListStyleType != css.ListStyleTypeNone || layer.MarkerContent != "") {
		r.drawListMarker(layer)
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

	if clipPathActive {
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

// applyClipPath builds the clip-path shape and calls Clip().
// Caller must have already called Push(); will Pop() to restore.
func (r *Renderer) applyClipPath(layer *PaintLayer) {
	box := layer.Box
	cp := layer.ClipPath.ResolveClipPath(box.Width, box.Height)

	switch cp.Type {
	case css.ClipPathCircle:
		r.dc.DrawCircle(box.X+cp.Cx, box.Y+cp.Cy, cp.Radius)

	case css.ClipPathEllipse:
		cx, cy := box.X+cp.Cx, box.Y+cp.Cy
		rx, ry := cp.Rx, cp.Ry
		// Approximate ellipse with 4 cubic Bezier curves.
		// kappa = 4*(sqrt(2)-1)/3 ≈ 0.5522847498
		k := 0.5522847498
		r.dc.MoveTo(cx+rx, cy)
		r.dc.CubicTo(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry)
		r.dc.CubicTo(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy)
		r.dc.CubicTo(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry)
		r.dc.CubicTo(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy)
		r.dc.ClosePath()

	case css.ClipPathPolygon:
		pts := cp.Points
		if len(pts) < 4 {
			return // Need at least 2 points
		}
		for i := 0; i < len(pts)-1; i += 2 {
			px := box.X + pts[i]
			py := box.Y + pts[i+1]
			if i == 0 {
				r.dc.MoveTo(px, py)
			} else {
				r.dc.LineTo(px, py)
			}
		}
		r.dc.ClosePath()

	default:
		return // Unknown type, no clipping
	}

	r.dc.Clip()
}

// hasBorderRadius returns true if any corner radius is non-zero.
func hasBorderRadius(layer *PaintLayer) bool {
	return !layer.BorderRadius.IsZero()
}

// buildRoundedRectPath traces a rounded rectangle path using CubicTo for
// elliptical corner arcs. Uses kappa constant for quarter-ellipse approximation.
func (r *Renderer) buildRoundedRectPath(x, y, w, h float64, radii css.EllipticalRadii) {
	buildRoundedRectPathOnDC(r.dc, x, y, w, h, radii)
}

// buildRoundedRectPathReverse traces a rounded rectangle path in reverse (CCW)
// for use with even-odd fill rule to cut out inner regions.
func (r *Renderer) buildRoundedRectPathReverse(x, y, w, h float64, radii css.EllipticalRadii) {
	const k = 0.5522847498 // kappa: 4*(sqrt(2)-1)/3
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
	r.dc.MoveTo(x+tl.Rx, y)
	// top-left (reverse: CW to CCW)
	r.dc.CubicTo(x+tl.Rx-tl.Rx*k, y, x, y+tl.Ry-tl.Ry*k, x, y+tl.Ry)
	r.dc.LineTo(x, y+h-bl.Ry)
	// bottom-left (reverse)
	r.dc.CubicTo(x, y+h-bl.Ry+bl.Ry*k, x+bl.Rx-bl.Rx*k, y+h, x+bl.Rx, y+h)
	r.dc.LineTo(x+w-br.Rx, y+h)
	// bottom-right (reverse)
	r.dc.CubicTo(x+w-br.Rx+br.Rx*k, y+h, x+w, y+h-br.Ry+br.Ry*k, x+w, y+h-br.Ry)
	r.dc.LineTo(x+w, y+tr.Ry)
	// top-right (reverse)
	r.dc.CubicTo(x+w, y+tr.Ry-tr.Ry*k, x+w-tr.Rx+tr.Rx*k, y, x+w-tr.Rx, y)
	r.dc.ClosePath()
}

// buildRoundedRectPathOnDC traces an elliptical rounded rectangle path on any DrawContext.
// This is the single source of truth for rounded-rect path construction.
func buildRoundedRectPathOnDC(dc interface {
	MoveTo(x, y float64)
	LineTo(x, y float64)
	CubicTo(x1, y1, x2, y2, x3, y3 float64)
	ClosePath()
}, x, y, w, h float64, radii css.EllipticalRadii) {
	const k = 0.5522847498 // kappa: 4*(sqrt(2)-1)/3
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]

	dc.MoveTo(x+tl.Rx, y)
	// Top edge → top-right corner
	dc.LineTo(x+w-tr.Rx, y)
	dc.CubicTo(x+w-tr.Rx+tr.Rx*k, y, x+w, y+tr.Ry-tr.Ry*k, x+w, y+tr.Ry)
	// Right edge → bottom-right corner
	dc.LineTo(x+w, y+h-br.Ry)
	dc.CubicTo(x+w, y+h-br.Ry+br.Ry*k, x+w-br.Rx+br.Rx*k, y+h, x+w-br.Rx, y+h)
	// Bottom edge → bottom-left corner
	dc.LineTo(x+bl.Rx, y+h)
	dc.CubicTo(x+bl.Rx-bl.Rx*k, y+h, x, y+h-bl.Ry+bl.Ry*k, x, y+h-bl.Ry)
	// Left edge → top-left corner
	dc.LineTo(x, y+tl.Ry)
	dc.CubicTo(x, y+tl.Ry-tl.Ry*k, x+tl.Rx-tl.Rx*k, y, x+tl.Rx, y)
	dc.ClosePath()
}

// buildRoundedRectPathReverseDC traces a rounded rectangle path in reverse (CCW)
// on any DrawContext. Used with even-odd fill rule to cut out inner regions.
func buildRoundedRectPathReverseDC(dc interface {
	MoveTo(x, y float64)
	LineTo(x, y float64)
	CubicTo(x1, y1, x2, y2, x3, y3 float64)
	ClosePath()
}, x, y, w, h float64, radii css.EllipticalRadii) {
	const k = 0.5522847498 // kappa: 4*(sqrt(2)-1)/3
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
	dc.MoveTo(x+tl.Rx, y)
	// top-left (reverse: CW to CCW)
	dc.CubicTo(x+tl.Rx-tl.Rx*k, y, x, y+tl.Ry-tl.Ry*k, x, y+tl.Ry)
	dc.LineTo(x, y+h-bl.Ry)
	// bottom-left (reverse)
	dc.CubicTo(x, y+h-bl.Ry+bl.Ry*k, x+bl.Rx-bl.Rx*k, y+h, x+bl.Rx, y+h)
	dc.LineTo(x+w-br.Rx, y+h)
	// bottom-right (reverse)
	dc.CubicTo(x+w-br.Rx+br.Rx*k, y+h, x+w, y+h-br.Ry+br.Ry*k, x+w, y+h-br.Ry)
	dc.LineTo(x+w, y+tr.Ry)
	// top-right (reverse)
	dc.CubicTo(x+w, y+tr.Ry-tr.Ry*k, x+w-tr.Rx+tr.Rx*k, y, x+w-tr.Rx, y)
	dc.ClosePath()
}

// backgroundClipRectForClip computes the background painting area based on
// a background-clip value (border-box, padding-box, content-box).
func backgroundClipRectForClip(box *layout.Box, clip css.BackgroundClipType) (float64, float64, float64, float64) {
	switch clip {
	case css.BackgroundClipPaddingBox:
		return pixelSnap(
			box.X+box.Border.Left, box.Y+box.Border.Top,
			box.Width-box.Border.Left-box.Border.Right,
			box.Height-box.Border.Top-box.Border.Bottom)
	case css.BackgroundClipContentBox:
		return pixelSnap(
			box.X+box.Border.Left+box.Padding.Left,
			box.Y+box.Border.Top+box.Padding.Top,
			box.Width-box.Border.Left-box.Border.Right-box.Padding.Left-box.Padding.Right,
			box.Height-box.Border.Top-box.Border.Bottom-box.Padding.Top-box.Padding.Bottom)
	default: // border-box
		return pixelSnap(box.X, box.Y, box.Width, box.Height)
	}
}

// backgroundClipRect returns the clip rect for background-color.
// Per CSS spec, background-color is clipped by the bottom-most layer's clip.
func (r *Renderer) backgroundClipRect(layer *PaintLayer) (float64, float64, float64, float64) {
	clip := layer.BackgroundClip
	if fl := layer.BackgroundLayers; fl != nil {
		// Find the bottom layer's clip for background-color.
		for cur := fl; cur != nil; cur = cur.Next {
			if cur.Next == nil {
				clip = cur.Clip
			}
		}
	}
	return backgroundClipRectForClip(layer.Box, clip)
}

// backgroundClipRadiiForClip adjusts border radii for a background-clip inset.
// Uses Blink's Inset operation: shrink Rx by horizontal inset, Ry by vertical.
func backgroundClipRadiiForClip(layer *PaintLayer, clip css.BackgroundClipType) css.EllipticalRadii {
	radii := layer.BorderRadius
	box := layer.Box
	switch clip {
	case css.BackgroundClipPaddingBox:
		return radii.Inset(box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left)
	case css.BackgroundClipContentBox:
		return radii.Inset(
			box.Border.Top+box.Padding.Top,
			box.Border.Right+box.Padding.Right,
			box.Border.Bottom+box.Padding.Bottom,
			box.Border.Left+box.Padding.Left)
	}
	return radii
}

// backgroundClipRadii returns radii for background-color's clip area.
func backgroundClipRadii(layer *PaintLayer) css.EllipticalRadii {
	clip := layer.BackgroundClip
	if fl := layer.BackgroundLayers; fl != nil {
		for cur := fl; cur != nil; cur = cur.Next {
			if cur.Next == nil {
				clip = cur.Clip
			}
		}
	}
	return backgroundClipRadiiForClip(layer, clip)
}

// drawBackground paints the layer's background color and image layers (pre-computed).
// Layers are painted bottom-to-top (Blink's IterateFillLayersInReverseOrder).
// Background-color is painted only with the bottommost layer.
func (r *Renderer) drawBackground(layer *PaintLayer) {
	sx, sy, sw, sh := r.backgroundClipRect(layer)
	radii := backgroundClipRadii(layer)
	hasRadius := !radii.IsZero()

	fl := layer.BackgroundLayers

	if fl == nil {
		// No layers — just paint background-color.
		if c := layer.BackgroundColor; c.A > 0 {
			r.setColor(c)
			if hasRadius {
				r.buildRoundedRectPath(sx, sy, sw, sh, radii)
				r.dc.Fill()
			} else {
				r.dc.DrawRectangle(sx, sy, sw, sh)
				r.dc.Fill()
			}
		}
		return
	}

	// Paint from bottom to top.
	fl.IterateReverse(func(bg *css.FillLayer) {
		// Background-color only on bottom layer.
		if bg.IsBottomLayer() {
			if c := layer.BackgroundColor; c.A > 0 {
				r.setColor(c)
				if hasRadius {
					r.buildRoundedRectPath(sx, sy, sw, sh, radii)
					r.dc.Fill()
				} else {
					r.dc.DrawRectangle(sx, sy, sw, sh)
					r.dc.Fill()
				}
			}
		}

		// Paint gradient.
		if bg.Gradient != "" {
			if hasRadius {
				r.dc.Push()
				r.buildRoundedRectPath(sx, sy, sw, sh, radii)
				r.dc.Clip()
				r.drawLinearGradient(bg.Gradient, sx, sy, sw, sh)
				r.dc.Pop()
			} else {
				r.drawLinearGradient(bg.Gradient, sx, sy, sw, sh)
			}
		}

		// Paint image.
		if bg.Image != "" && r.imageFetcher != nil {
			if hasRadius {
				r.dc.Push()
				r.buildRoundedRectPath(sx, sy, sw, sh, radii)
				r.dc.Clip()
				r.drawBackgroundImageLayer(layer, bg)
				r.dc.Pop()
			} else {
				r.drawBackgroundImageLayer(layer, bg)
			}
		}
	})
}

// drawBackgroundImageLayer tiles a single background layer's image onto the layer's box.
// Reads image URL, origin, size, position, repeat from the FillLayer.
// Tiles are manually clipped to the box bounds because DrawImage bypasses
// the DrawContext clip mask (fast-path pixel blit ignores clipMask).
// All arithmetic is integer-based to avoid fractional pixel misalignment.
func (r *Renderer) drawBackgroundImageLayer(layer *PaintLayer, bg *css.FillLayer) {
	box := layer.Box
	img, err := images.LoadImageWithFetcher(bg.Image, r.imageFetcher)
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

	// CSS3 Backgrounds §3.6: background-origin determines the positioning area.
	var originX, originY, originW, originH float64
	switch bg.Origin {
	case css.BackgroundOriginBorderBox:
		originX = math.Round(box.X)
		originY = math.Round(box.Y)
		originW = math.Round(box.X+box.Width) - originX
		originH = math.Round(box.Y+box.Height) - originY
	case css.BackgroundOriginContentBox:
		originX = math.Round(box.X + box.Border.Left + box.Padding.Left)
		originY = math.Round(box.Y + box.Border.Top + box.Padding.Top)
		originW = math.Round(box.X+box.Width-box.Border.Right-box.Padding.Right) - originX
		originH = math.Round(box.Y+box.Height-box.Border.Bottom-box.Padding.Bottom) - originY
	default: // padding-box (default)
		originX = math.Round(box.X + box.Border.Left)
		originY = math.Round(box.Y + box.Border.Top)
		originW = math.Round(box.X+box.Width-box.Border.Right) - originX
		originH = math.Round(box.Y+box.Height-box.Border.Bottom) - originY
	}
	if originW < 0 {
		originW = 0
	}
	if originH < 0 {
		originH = 0
	}

	// CSS3 Backgrounds §3.9: Resolve background-size.
	bgSize := bg.Size
	if bgSize.Cover || bgSize.Contain {
		if originW > 0 && originH > 0 && imgW > 0 && imgH > 0 {
			scaleX := originW / imgW
			scaleY := originH / imgH
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
				newW = math.Round(originW * (-bgSize.Width) / 100)
			} else {
				newW = math.Round(bgSize.Width)
			}
		}
		if bgSize.Height != 0 {
			if bgSize.Height < 0 {
				newH = math.Round(originH * (-bgSize.Height) / 100)
			} else {
				newH = math.Round(bgSize.Height)
			}
		}
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

	pos := bg.Position
	startX := originX + pos.ResolveX(originW, imgW)
	startY := originY + pos.ResolveY(originH, imgH)

	repeat := bg.Repeat
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

// drawImage paints an <img> element with object-fit and object-position support.
func (r *Renderer) drawImage(layer *PaintLayer) {
	if layer.ImageSrc == "" || r.imageFetcher == nil {
		return
	}
	box := layer.Box
	img, err := images.LoadImageWithFetcher(layer.ImageSrc, r.imageFetcher)
	if err != nil {
		return
	}

	srcW := float64(img.Bounds().Dx())
	srcH := float64(img.Bounds().Dy())
	contentX := math.Round(box.X + box.Border.Left + box.Padding.Left)
	contentY := math.Round(box.Y + box.Border.Top + box.Padding.Top)
	cw := box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right
	ch := box.Height - box.Border.Top - box.Border.Bottom - box.Padding.Top - box.Padding.Bottom
	dstW := int(math.Round(cw))
	dstH := int(math.Round(ch))
	if dstW <= 0 || dstH <= 0 || srcW == 0 || srcH == 0 {
		return
	}

	// Compute actual draw dimensions based on object-fit.
	var drawW, drawH float64
	switch layer.ObjectFit {
	case css.ObjectFitContain:
		scale := math.Min(cw/srcW, ch/srcH)
		drawW, drawH = srcW*scale, srcH*scale
	case css.ObjectFitCover:
		scale := math.Max(cw/srcW, ch/srcH)
		drawW, drawH = srcW*scale, srcH*scale
	case css.ObjectFitNone:
		drawW, drawH = srcW, srcH
	case css.ObjectFitScaleDown:
		scale := math.Min(1, math.Min(cw/srcW, ch/srcH))
		drawW, drawH = srcW*scale, srcH*scale
	default: // ObjectFitFill
		drawW, drawH = cw, ch
	}

	// Scale image to draw dimensions.
	scaledW, scaledH := int(math.Round(drawW)), int(math.Round(drawH))
	if scaledW <= 0 || scaledH <= 0 {
		return
	}
	scaled := scaleImageNearest(img, int(srcW), int(srcH), scaledW, scaledH)

	// Position within content box using object-position.
	dx := contentX + (cw-drawW)*layer.ObjectPosition[0]
	dy := contentY + (ch-drawH)*layer.ObjectPosition[1]

	// Determine if clipping to the content box is needed (image may extend beyond).
	needsContentClip := layer.ObjectFit == css.ObjectFitCover || layer.ObjectFit == css.ObjectFitNone ||
		(layer.ObjectFit == css.ObjectFitScaleDown && (srcW > cw || srcH > ch))

	// finalImg and finalX/finalY will hold the image to draw after all clipping.
	var finalImg image.Image
	finalX, finalY := int(math.Round(dx)), int(math.Round(dy))

	if needsContentClip {
		// Crop the scaled image to the content box.
		cropX := int(math.Round(contentX - dx))
		cropY := int(math.Round(contentY - dy))
		cropW := dstW
		cropH := dstH
		if cropX < 0 {
			cropX = 0
		}
		if cropY < 0 {
			cropY = 0
		}
		if cropX+cropW > scaledW {
			cropW = scaledW - cropX
		}
		if cropY+cropH > scaledH {
			cropH = scaledH - cropY
		}
		if cropW <= 0 || cropH <= 0 {
			return
		}
		finalImg = scaled.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(cropX, cropY, cropX+cropW, cropY+cropH))
		finalX = finalX + cropX
		finalY = finalY + cropY
	} else {
		finalImg = scaled
	}

	// CSS clip: rect() — DrawImage bypasses the clip mask, so we must
	// manually crop the image to the CSS clip region.
	if layer.HasCSSClip {
		// CSSClipRect is [x, y, w, h] in absolute coordinates.
		cx := int(math.Round(layer.CSSClipRect[0]))
		cy := int(math.Round(layer.CSSClipRect[1]))
		ccw := int(math.Round(layer.CSSClipRect[2]))
		cch := int(math.Round(layer.CSSClipRect[3]))

		imgBounds := finalImg.Bounds()
		imgW := imgBounds.Dx()
		imgH := imgBounds.Dy()

		// Intersect clip region with image draw area.
		ix0 := finalX
		if cx > ix0 {
			ix0 = cx
		}
		iy0 := finalY
		if cy > iy0 {
			iy0 = cy
		}
		ix1 := finalX + imgW
		if cx+ccw < ix1 {
			ix1 = cx + ccw
		}
		iy1 := finalY + imgH
		if cy+cch < iy1 {
			iy1 = cy + cch
		}
		if ix1 <= ix0 || iy1 <= iy0 {
			return // Entirely clipped
		}
		// Extract the visible sub-image.
		sub := finalImg.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(
			imgBounds.Min.X+(ix0-finalX),
			imgBounds.Min.Y+(iy0-finalY),
			imgBounds.Min.X+(ix1-finalX),
			imgBounds.Min.Y+(iy1-finalY),
		))
		r.dc.DrawImage(sub, ix0, iy0)
		return
	}

	r.dc.DrawImage(finalImg, finalX, finalY)
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
		// However, this only works when the midline radii faithfully represent
		// the outer shape. When border-width/2 exceeds any outer radius, the
		// midline radius goes to 0 (sharp corner) while the outer edge should
		// still be rounded. Fall through to the ring approach in that case.
		hw := bw.Top / 2
		midRadii := layer.BorderRadius.Inset(hw, hw, hw, hw)
		midOk := true
		for i := 0; i < 4; i++ {
			if !layer.BorderRadius[i].IsZero() && midRadii[i].IsZero() {
				midOk = false
				break
			}
		}
		if midOk {
			r.setColor(layer.BorderColors[0])
			r.dc.SetLineWidth(bw.Top)
			r.buildRoundedRectPath(x+hw, y+hw, w-bw.Top, h-bw.Top, midRadii)
			r.dc.Stroke()
			return
		}
	}

	// Non-uniform borders with radius: draw each side using even-odd fill
	// between outer and inner rounded rects, clipped to each side's region.
	outerRadii := layer.BorderRadius

	// Inner rounded rect (border-box inset by border widths) — Blink's Inset.
	ix := x + bw.Left
	iy := y + bw.Top
	iw := w - bw.Left - bw.Right
	ih := h - bw.Top - bw.Bottom
	innerRadii := outerRadii.Inset(bw.Top, bw.Right, bw.Bottom, bw.Left)

	type borderSide struct {
		width float64
		style css.BorderStyle
		color css.Color
		clipX, clipY, clipW, clipH float64
	}

	// For clip region calculations, use the maximum of Rx and Ry per corner.
	or := [4]float64{
		math.Max(outerRadii[0].Rx, outerRadii[0].Ry),
		math.Max(outerRadii[1].Rx, outerRadii[1].Ry),
		math.Max(outerRadii[2].Rx, outerRadii[2].Ry),
		math.Max(outerRadii[3].Rx, outerRadii[3].Ry),
	}

	// Compute clip regions for each side.
	sides := [4]borderSide{
		{ // Top
			width: bw.Top, style: layer.BorderStyles[0], color: layer.BorderColors[0],
			clipX: x, clipY: y,
			clipW: w,
			clipH: math.Max(bw.Top, math.Max(or[0], or[1])),
		},
		{ // Right
			width: bw.Right, style: layer.BorderStyles[1], color: layer.BorderColors[1],
			clipX: x + w - math.Max(bw.Right, math.Max(or[1], or[2])),
			clipY: y,
			clipW: math.Max(bw.Right, math.Max(or[1], or[2])),
			clipH: h,
		},
		{ // Bottom
			width: bw.Bottom, style: layer.BorderStyles[2], color: layer.BorderColors[2],
			clipX: x,
			clipY: y + h - math.Max(bw.Bottom, math.Max(or[2], or[3])),
			clipW: w,
			clipH: math.Max(bw.Bottom, math.Max(or[2], or[3])),
		},
		{ // Left
			width: bw.Left, style: layer.BorderStyles[3], color: layer.BorderColors[3],
			clipX: x, clipY: y,
			clipW: math.Max(bw.Left, math.Max(or[0], or[3])),
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

// drawColumnRules draws vertical rules between multicol columns.
// Rules are centered in the gap between adjacent columns.
func (r *Renderer) drawColumnRules(layer *PaintLayer) {
	box := layer.Box
	ruleWidth := layer.ColumnRuleWidth
	colWidth := layer.ColumnWidth
	gap := layer.ColumnGap
	numCols := layer.ColumnCount

	if numCols < 2 || colWidth <= 0 {
		return
	}

	r.setColor(layer.ColumnRuleColor)

	// Content area start (inside border and padding).
	contentX := math.Round(box.X + box.Border.Left + box.Padding.Left)
	contentY := math.Round(box.Y + box.Border.Top + box.Padding.Top)
	contentH := math.Round(box.Y+box.Height-box.Border.Bottom-box.Padding.Bottom) - contentY

	// Draw a rule between each pair of adjacent columns.
	for i := 1; i < numCols; i++ {
		// Center of gap between column i-1 and column i.
		ruleX := contentX + float64(i)*(colWidth+gap) - gap/2

		switch layer.ColumnRuleStyle {
		case "solid":
			r.dc.DrawRectangle(ruleX-ruleWidth/2, contentY, ruleWidth, contentH)
			r.dc.Fill()
		case "dashed":
			r.drawDashedLine(ruleX, contentY, ruleX, contentY+contentH, ruleWidth)
		case "dotted":
			r.drawDottedLine(ruleX, contentY, ruleX, contentY+contentH, ruleWidth)
		case "double":
			thirdW := ruleWidth / 3
			r.dc.DrawRectangle(ruleX-ruleWidth/2, contentY, thirdW, contentH)
			r.dc.Fill()
			r.dc.DrawRectangle(ruleX+ruleWidth/2-thirdW, contentY, thirdW, contentH)
			r.dc.Fill()
		default:
			// For other styles (ridge, groove, inset, outset), fallback to solid.
			r.dc.DrawRectangle(ruleX-ruleWidth/2, contentY, ruleWidth, contentH)
			r.dc.Fill()
		}
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
			expandedRadii := layer.BorderRadius.Outset(off, off, off, off)
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

// applyTextTransform applies CSS text-transform to a string.
func applyTextTransform(s string, transform css.TextTransform) string {
	switch transform {
	case css.TextTransformUppercase:
		return strings.ToUpper(s)
	case css.TextTransformLowercase:
		return strings.ToLower(s)
	case css.TextTransformCapitalize:
		return capitalizeWords(s)
	}
	return s
}

// capitalizeWords capitalizes the first letter of each word (CSS capitalize).
func capitalizeWords(s string) string {
	inWord := false
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			inWord = false
			b.WriteRune(r)
		} else if !inWord {
			inWord = true
			b.WriteRune(unicode.ToUpper(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// toRoman converts a positive integer to a Roman numeral string.
// Uses subtractive notation (e.g., 4=IV, 9=IX).
func toRoman(n int) string {
	if n <= 0 || n > 3999 {
		return fmt.Sprintf("%d", n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// toAlpha converts a positive integer to alphabetic notation (a=1, b=2, ..., z=26, aa=27, ...).
func toAlpha(n int) string {
	if n <= 0 {
		return fmt.Sprintf("%d", n)
	}
	var b strings.Builder
	for n > 0 {
		n-- // make 0-indexed
		b.WriteByte(byte('a' + n%26))
		n /= 26
	}
	// Reverse
	s := []byte(b.String())
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return string(s)
}

// toGreek converts a positive integer to lower Greek letters (α=1, β=2, ...).
func toGreek(n int) string {
	// CSS counter-styles: lower-greek uses the 24-letter Greek alphabet
	greek := []rune{'α', 'β', 'γ', 'δ', 'ε', 'ζ', 'η', 'θ', 'ι', 'κ', 'λ', 'μ', 'ν', 'ξ', 'ο', 'π', 'ρ', 'σ', 'τ', 'υ', 'φ', 'χ', 'ψ', 'ω'}
	if n <= 0 || n > len(greek) {
		return fmt.Sprintf("%d", n)
	}
	return string(greek[n-1])
}

// formatListMarker returns the marker text for a given list-style-type and index.
func formatListMarker(lst css.ListStyleType, index int) string {
	switch lst {
	case css.ListStyleTypeDecimal:
		return fmt.Sprintf("%d.", index)
	case css.ListStyleTypeDecimalLeadingZero:
		return fmt.Sprintf("%02d.", index)
	case css.ListStyleTypeLowerAlpha, css.ListStyleTypeLowerLatin:
		return toAlpha(index) + "."
	case css.ListStyleTypeUpperAlpha, css.ListStyleTypeUpperLatin:
		return strings.ToUpper(toAlpha(index)) + "."
	case css.ListStyleTypeLowerRoman:
		return strings.ToLower(toRoman(index)) + "."
	case css.ListStyleTypeUpperRoman:
		return toRoman(index) + "."
	case css.ListStyleTypeLowerGreek:
		return toGreek(index) + "."
	case css.ListStyleTypeDisclosureOpen:
		return "\u25BE" // ▾ downward-pointing triangle
	case css.ListStyleTypeDisclosureClosed:
		return "\u25B8" // ▸ right-pointing triangle
	default:
		return fmt.Sprintf("%d.", index)
	}
}

// drawListMarker paints the list-item marker (bullet or number) to the left
// of the content box, inside the padding area created by the UA stylesheet.
func (r *Renderer) drawListMarker(layer *PaintLayer) {
	box := layer.Box
	fontSize := layer.FontSize
	if layer.HasMarkerFont {
		fontSize = layer.MarkerFontSize
	}
	markerSize := fontSize * 0.35

	// Position: to the left of the content box, vertically centered on first line.
	contentLeft := box.X + box.Border.Left + box.Padding.Left
	// Center marker in the padding area (between border and content).
	mx := contentLeft - box.Padding.Left/2
	// Vertically: approximately at the midpoint of the first line.
	my := box.Y + box.Border.Top + fontSize*0.55

	// Apply ::marker color if specified, else use text color.
	if layer.HasMarkerColor {
		r.setColor(layer.MarkerColor)
	} else {
		r.setColor(layer.TextColor)
	}

	// If ::marker has custom content, draw it as text.
	if layer.MarkerContent != "" {
		fontPath := r.fonts.FontPathForFamily(layer.FontFamily, layer.FontBold, layer.FontItalic, layer.FontMono, layer.FontAhem)
		fid := r.openFont(fontPath, fontSize)
		if fid >= 0 {
			tw := r.dc.MeasureText(layer.MarkerContent, fid)
			metrics := r.dc.GetFontMetrics(fid)
			ascent := float64(metrics.Ascent) / 64.0
			numX := contentLeft - tw - markerSize*0.5
			numY := box.Y + box.Border.Top + ascent
			r.dc.DrawText(layer.MarkerContent, fid, numX, numY)
		}
		return
	}

	switch layer.ListStyleType {
	case css.ListStyleTypeDisc:
		r.dc.DrawCircle(mx, my, markerSize/2)
		r.dc.Fill()
	case css.ListStyleTypeCircle:
		r.dc.DrawCircle(mx, my, markerSize/2)
		r.dc.SetLineWidth(1)
		r.dc.Stroke()
	case css.ListStyleTypeSquare:
		r.dc.DrawRectangle(mx-markerSize/2, my-markerSize/2, markerSize, markerSize)
		r.dc.Fill()
	default:
		// All text-based markers: decimal, alpha, roman, greek, disclosure, etc.
		numStr := formatListMarker(layer.ListStyleType, layer.ListItemIndex)
		fontPath := r.fonts.FontPathForFamily(layer.FontFamily, layer.FontBold, layer.FontItalic, layer.FontMono, layer.FontAhem)
		fid := r.openFont(fontPath, fontSize)
		if fid >= 0 {
			tw := r.dc.MeasureText(numStr, fid)
			metrics := r.dc.GetFontMetrics(fid)
			ascent := float64(metrics.Ascent) / 64.0
			// Right-align marker text to the left of content.
			numX := contentLeft - tw - markerSize*0.5
			numY := box.Y + box.Border.Top + ascent
			r.dc.DrawText(numStr, fid, numX, numY)
		}
	}
}

// drawText paints text content using pre-computed font/color properties.
func (r *Renderer) drawText(layer *PaintLayer) {
	box := layer.Box

	// Apply CSS text-transform.
	text := box.Text
	if layer.TextTransform != css.TextTransformNone {
		text = applyTextTransform(text, layer.TextTransform)
	}

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
		childDC.DrawText(text, fontID, 0, ascent)

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
		for _, ch := range text {
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

	// Text overflow: ellipsis — truncate text if it overflows the
	// nearest ancestor block container that has overflow:hidden.
	// CSS text-overflow applies to the block container; we check both
	// the text run's own style and ancestor styles for the property.
	text = r.applyTextOverflowEllipsis(layer, text, box, fontID)

	// Draw text shadows (behind actual text).
	if len(layer.TextShadows) > 0 {
		r.drawTextShadows(layer, text, box, fontID, ascent)
	}

	if layer.LetterSpacing != 0 || layer.WordSpacing != 0 {
		x := box.X
		baselineY := box.Y + ascent
		if layer.LetterSpacing != 0 {
			// Character-by-character rendering for letter-spacing.
			for _, ch := range text {
				r.dc.DrawText(string(ch), fontID, x, baselineY)
				charW := r.dc.MeasureText(string(ch), fontID)
				x += charW + layer.LetterSpacing
				if ch == ' ' {
					x += layer.WordSpacing
				}
			}
		} else {
			// Word-by-word rendering for word-spacing only.
			// Drawing words as units avoids sub-pixel accumulation errors
			// that occur with per-character rendering.
			words := strings.Split(text, " ")
			spaceW := r.dc.MeasureText(" ", fontID)
			for i, word := range words {
				if word != "" {
					r.dc.DrawText(word, fontID, x, baselineY)
					x += r.dc.MeasureText(word, fontID)
				}
				if i < len(words)-1 {
					x += spaceW + layer.WordSpacing
				}
			}
		}
	} else {
		r.dc.DrawText(text, fontID, box.X, box.Y+ascent)
	}

	// Draw text decoration lines (underline, overline, line-through).
	r.drawTextDecoration(layer, text, box, fontID, ascent)
}

// applyTextOverflowEllipsis truncates text and appends "…" when
// text-overflow:ellipsis is active on a block container with overflow:hidden.
// Returns the (possibly truncated) text string.
func (r *Renderer) applyTextOverflowEllipsis(layer *PaintLayer, text string, box *layout.Box, fontID int32) string {
	// Determine if text-overflow:ellipsis applies.
	// It may be set on the text run's own style (when the text node's
	// parent element is the block container) or on an ancestor.
	hasEllipsis := layer.TextOverflow == css.TextOverflowEllipsis

	// Walk up the box tree to find the nearest ancestor with overflow:hidden
	// and text-overflow:ellipsis.
	var clipAncestor *layout.Box
	for p := box.Parent; p != nil; p = p.Parent {
		if p.Style == nil {
			continue
		}
		overflow := p.Style.GetOverflow()
		if overflow == css.OverflowHidden || overflow == css.OverflowScroll || overflow == css.OverflowAuto {
			if !hasEllipsis && p.Style.GetTextOverflow() == css.TextOverflowEllipsis {
				hasEllipsis = true
			}
			if hasEllipsis {
				clipAncestor = p
			}
			break
		}
	}

	if !hasEllipsis || clipAncestor == nil {
		return text
	}

	// Compute available width: right edge of ancestor's padding box minus
	// the text run's starting X position.  The padding box is the overflow
	// clip boundary (CSS Overflow §3), so the ellipsis must fit within it.
	paddingRight := clipAncestor.X + clipAncestor.Width - clipAncestor.Border.Right
	availW := paddingRight - box.X
	if availW <= 0 {
		return text
	}

	// Account for letter-spacing and word-spacing in width measurements.
	ls := layer.LetterSpacing
	ws := layer.WordSpacing
	measureRunWidth := func(s string) float64 {
		if ls == 0 && ws == 0 {
			return r.dc.MeasureText(s, fontID)
		}
		total := 0.0
		runes := []rune(s)
		for i, ch := range runes {
			total += r.dc.MeasureText(string(ch), fontID)
			if i < len(runes)-1 {
				total += ls
			}
			if ch == ' ' {
				total += ws
			}
		}
		return total
	}

	textW := measureRunWidth(text)
	if textW <= availW {
		return text
	}

	// Text overflows — truncate and append ellipsis.
	const ellipsis = "\u2026" // "…"
	ellipsisW := r.dc.MeasureText(ellipsis, fontID)
	truncW := availW - ellipsisW

	if truncW <= 0 {
		// Not enough room even for the ellipsis — just show ellipsis.
		return ellipsis
	}

	// Find the truncation point by measuring characters.
	w := 0.0
	truncIdx := 0
	runes := []rune(text)
	for i, ch := range runes {
		cw := r.dc.MeasureText(string(ch), fontID)
		advance := cw
		if i < len(runes)-1 {
			advance += ls
		}
		if ch == ' ' {
			advance += ws
		}
		if w+cw > truncW {
			truncIdx = len(string(runes[:i]))
			break
		}
		w += advance
		truncIdx = len(string(runes[:i+1]))
	}

	return text[:truncIdx] + ellipsis
}

// drawTextShadows paints text shadows behind the actual text glyphs.
// Shadows are painted in reverse declaration order (last declared = behind).
func (r *Renderer) drawTextShadows(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64) {
	for i := len(layer.TextShadows) - 1; i >= 0; i-- {
		shadow := layer.TextShadows[i]
		if shadow.Blur > 0 {
			r.drawBlurredTextShadow(layer, text, box, fontID, ascent, shadow)
		} else {
			// No blur: just draw text at offset position with shadow color.
			r.setColor(shadow.Color)
			if layer.LetterSpacing != 0 || layer.WordSpacing != 0 {
				x := box.X + shadow.OffsetX
				baselineY := box.Y + ascent + shadow.OffsetY
				for _, ch := range text {
					r.dc.DrawText(string(ch), fontID, x, baselineY)
					charW := r.dc.MeasureText(string(ch), fontID)
					x += charW + layer.LetterSpacing
					if ch == ' ' {
						x += layer.WordSpacing
					}
				}
			} else {
				r.dc.DrawText(text, fontID, box.X+shadow.OffsetX, box.Y+ascent+shadow.OffsetY)
			}
		}
	}
	// Restore text color for actual text drawing.
	r.setColor(layer.TextColor)
}

// drawBlurredTextShadow renders a single text shadow with Gaussian-like blur
// by drawing text into an offscreen buffer, applying box blur, then compositing.
func (r *Renderer) drawBlurredTextShadow(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64, shadow css.TextShadow) {
	// Measure text to determine buffer size.
	textWidth := r.dc.MeasureText(text, fontID)
	metrics := r.dc.GetFontMetrics(fontID)
	textHeight := float64(metrics.Ascent-metrics.Descent) / 64.0

	sigma := shadow.Blur / 2
	extend := math.Ceil(sigma * 3)

	bw := int(math.Ceil(textWidth + 2*extend))
	bh := int(math.Ceil(textHeight + 2*extend))
	if bw <= 0 || bh <= 0 || bw > 2000 || bh > 2000 {
		// Fallback: draw without blur.
		r.setColor(shadow.Color)
		r.dc.DrawText(text, fontID, box.X+shadow.OffsetX, box.Y+ascent+shadow.OffsetY)
		return
	}

	// Render text into offscreen buffer.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	childDC := r.dc.NewChildContext(buf)
	childDC.SetColor(color.RGBA{
		R: shadow.Color.R,
		G: shadow.Color.G,
		B: shadow.Color.B,
		A: uint8(shadow.Color.A * 255),
	})
	childDC.DrawText(text, fontID, extend, extend+ascent)

	// Apply 3-pass box blur (approximates Gaussian blur).
	blurRadius := int(math.Round(sigma))
	boxBlur(buf, blurRadius)
	boxBlur(buf, blurRadius)
	boxBlur(buf, blurRadius)

	// Composite back at shadow offset position.
	dx := int(math.Round(box.X + shadow.OffsetX - extend))
	dy := int(math.Round(box.Y + shadow.OffsetY - extend))
	r.dc.DrawImage(buf, dx, dy)
}

// drawTextDecoration renders underline, overline, or line-through decoration
// lines for non-vertical, non-sideways text.
func (r *Renderer) drawTextDecoration(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64) {
	if layer.TextDecoration == css.TextDecorationNone || layer.TextDecoration == "" {
		return
	}

	metrics := r.dc.GetFontMetrics(fontID)
	descent := float64(metrics.Descent) / 64.0
	textWidth := r.dc.MeasureText(text, fontID)

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

	thickness := layer.TextDecorationThickness

	switch layer.TextDecorationStyle {
	case "dashed":
		r.drawDashedLine(box.X, lineY, box.X+textWidth, lineY, thickness)
	case "dotted":
		r.drawDottedLine(box.X, lineY, box.X+textWidth, lineY, thickness)
	case "double":
		r.drawDoubleLine(box.X, lineY, box.X+textWidth, lineY, thickness)
	case "wavy":
		r.drawWavyLine(box.X, lineY, textWidth, thickness)
	default: // "solid"
		r.dc.MoveTo(box.X, lineY)
		r.dc.LineTo(box.X+textWidth, lineY)
		r.dc.Stroke()
	}
}

// drawWavyLine draws a wavy (sinusoidal) decoration line using quadratic curves.
func (r *Renderer) drawWavyLine(x, y, width, thickness float64) {
	if width <= 0 {
		return
	}
	amplitude := thickness * 1.5
	wavelength := thickness * 4
	if wavelength < 4 {
		wavelength = 4
	}
	halfWave := wavelength / 2

	r.dc.SetLineWidth(thickness)
	r.dc.MoveTo(x, y)

	cx := x
	up := true
	for cx < x+width {
		endX := cx + halfWave
		if endX > x+width {
			endX = x + width
		}
		midX := (cx + endX) / 2
		if up {
			r.dc.QuadraticTo(midX, y-amplitude, endX, y)
		} else {
			r.dc.QuadraticTo(midX, y+amplitude, endX, y)
		}
		cx = endX
		up = !up
	}
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

// hasOverflowClipping returns true if the box has overflow:hidden/scroll/auto
// or paint containment (which also clips and contains positioned descendants).
func hasOverflowClipping(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	overflow := box.Style.GetOverflow()
	if overflow == css.OverflowHidden || overflow == css.OverflowScroll || overflow == css.OverflowAuto {
		return true
	}
	// CSS Containment: paint containment clips content and contains descendants.
	return box.Style.HasPaintContainment()
}

// isContainedByOverflow returns true if the child is clipped by the parent's
// overflow/containment and should NOT escape to the ancestor stacking context.
func isContainedByOverflow(child, parent *layout.Box) bool {
	if parent.Style == nil {
		return false
	}
	// CSS Containment: layout and paint containment contain all positioned
	// descendants (absolute and fixed), acting as a containing block.
	if parent.Style.HasLayoutContainment() || parent.Style.HasPaintContainment() {
		if child.Position != css.PositionStatic {
			return true
		}
	}
	if !hasOverflowClipping(parent) {
		return false
	}
	// position:relative and position:sticky children are always clipped by parent's overflow.
	if child.Position == css.PositionRelative || child.Position == css.PositionSticky {
		return true
	}
	// position:absolute children are clipped only when the parent is
	// positioned (i.e., is the containing block for the abs-pos child).
	if child.Position == css.PositionAbsolute && parent.Position != css.PositionStatic {
		return true
	}
	return false
}

// drawOutsetBoxShadows paints outset box shadows behind the element.
// Shadows are painted in reverse declaration order (last = behind).
func (r *Renderer) drawOutsetBoxShadows(layer *PaintLayer) {
	box := layer.Box
	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)

	for i := len(layer.BoxShadows) - 1; i >= 0; i-- {
		shadow := layer.BoxShadows[i]
		if shadow.Inset {
			continue
		}

		// Shadow rectangle: offset by (offsetX, offsetY), expanded by spread.
		sx := x + shadow.OffsetX - shadow.Spread
		sy := y + shadow.OffsetY - shadow.Spread
		sw := w + 2*shadow.Spread
		sh := h + 2*shadow.Spread

		if sw <= 0 || sh <= 0 {
			continue
		}

		// Expand border radii by spread for the shadow shape using the CSS
		// spec's cubic interpolation formula (§5.4 shadow shape).
		sp := shadow.Spread
		shadowRadii := layer.BorderRadius.OutsetForBoxShadow(sp, sp, sp, sp)

		// Use offscreen buffer to clip out the border box from the shadow.
		// Per CSS spec: outset shadows are only visible outside the border edge.
		r.drawOutsetShadowBuffer(sx, sy, sw, sh, shadowRadii,
			x, y, w, h, layer.BorderRadius, shadow)
	}
}

// drawOutsetShadowBuffer renders an outset shadow using an offscreen buffer.
// Fills the shadow shape, clears the border box area, optionally blurs, then composites.
func (r *Renderer) drawOutsetShadowBuffer(
	sx, sy, sw, sh float64, shadowRadii css.EllipticalRadii,
	bx, by, bw, bh float64, borderRadii css.EllipticalRadii,
	shadow css.BoxShadow,
) {
	sigma := shadow.Blur / 2
	extend := math.Ceil(sigma * 3)

	// Compute the bounding box that covers both shadow and border box + blur.
	minX := math.Min(sx, bx) - extend
	minY := math.Min(sy, by) - extend
	maxX := math.Max(sx+sw, bx+bw) + extend
	maxY := math.Max(sy+sh, by+bh) + extend

	bufW := int(math.Ceil(maxX - minX))
	bufH := int(math.Ceil(maxY - minY))
	if bufW <= 0 || bufH <= 0 || bufW > 4000 || bufH > 4000 {
		return
	}

	buf := image.NewRGBA(image.Rect(0, 0, bufW, bufH))

	childDC := r.dc.NewChildContext(buf)
	childDC.SetColor(color.RGBA{
		R: shadow.Color.R,
		G: shadow.Color.G,
		B: shadow.Color.B,
		A: uint8(shadow.Color.A * 255),
	})

	lsx, lsy := sx-minX, sy-minY
	lbx, lby := bx-minX, by-minY

	if !shadowRadii.IsZero() || !borderRadii.IsZero() {
		// When rounded corners are involved, use even-odd fill in a single
		// rasterization pass. This ensures the outer and inner boundaries
		// share identical sub-pixel coverage decisions, preventing
		// anti-aliasing gaps at rounded corners.
		if !shadowRadii.IsZero() {
			buildRoundedRectPathOnDC(childDC, lsx, lsy, sw, sh, shadowRadii)
		} else {
			childDC.DrawRectangle(lsx, lsy, sw, sh)
		}
		if !borderRadii.IsZero() {
			buildRoundedRectPathReverseDC(childDC, lbx, lby, bw, bh, borderRadii)
		} else {
			childDC.MoveTo(lbx, lby)
			childDC.LineTo(lbx, lby+bh)
			childDC.LineTo(lbx+bw, lby+bh)
			childDC.LineTo(lbx+bw, lby)
			childDC.ClosePath()
		}
		childDC.SetFillRule(textshape.FillRuleEvenOdd)
		childDC.Fill()
		childDC.SetFillRule(textshape.FillRuleWinding)
	} else {
		// For purely rectangular shadows, use the two-pass approach:
		// fill the outer shape, then clear the inner hole via mask.
		childDC.DrawRectangle(lsx, lsy, sw, sh)
		childDC.Fill()

		holeMask := image.NewRGBA(image.Rect(0, 0, bufW, bufH))
		holeDC := r.dc.NewChildContext(holeMask)
		holeDC.SetColor(color.White)
		holeDC.DrawRectangle(lbx, lby, bw, bh)
		holeDC.Fill()

		for py := 0; py < bufH; py++ {
			for px := 0; px < bufW; px++ {
				moff := py*holeMask.Stride + px*4
				a := holeMask.Pix[moff+3]
				if a > 0 {
					off := py*buf.Stride + px*4
					if a == 255 {
						buf.Pix[off+0] = 0
						buf.Pix[off+1] = 0
						buf.Pix[off+2] = 0
						buf.Pix[off+3] = 0
					} else {
						keep := 255 - uint16(a)
						buf.Pix[off+0] = uint8(uint16(buf.Pix[off+0]) * keep / 255)
						buf.Pix[off+1] = uint8(uint16(buf.Pix[off+1]) * keep / 255)
						buf.Pix[off+2] = uint8(uint16(buf.Pix[off+2]) * keep / 255)
						buf.Pix[off+3] = uint8(uint16(buf.Pix[off+3]) * keep / 255)
					}
				}
			}
		}
	}

	// Apply box blur if needed.
	if shadow.Blur > 0 {
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
	}

	r.dc.DrawImage(buf, int(math.Round(minX)), int(math.Round(minY)))
}

// drawInsetBoxShadows paints inset box shadows inside the element,
// after background and borders. Per CSS spec, inset shadows are drawn
// inside the padding box, creating a "donut" between the box edge and
// the shadow inner rect.
func (r *Renderer) drawInsetBoxShadows(layer *PaintLayer) {
	box := layer.Box
	// Inset shadows paint inside the padding box.
	px := box.X + box.Border.Left
	py := box.Y + box.Border.Top
	pw := box.Width - box.Border.Left - box.Border.Right
	ph := box.Height - box.Border.Top - box.Border.Bottom
	px, py, pw, ph = pixelSnap(px, py, pw, ph)

	if pw <= 0 || ph <= 0 {
		return
	}

	// Compute inner border radii (radii shrink by border width) — Blink's Inset.
	innerRadii := layer.BorderRadius.Inset(box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left)

	for i := len(layer.BoxShadows) - 1; i >= 0; i-- {
		shadow := layer.BoxShadows[i]
		if !shadow.Inset {
			continue
		}

		// Inner shadow rect: padding-box shrunk by spread, offset.
		// Positive spread shrinks the hole (makes shadow wider).
		ix := px + shadow.Spread + shadow.OffsetX
		iy := py + shadow.Spread + shadow.OffsetY
		iw := pw - 2*shadow.Spread
		ih := ph - 2*shadow.Spread

		// Adjust inner radii by spread using the CSS spec formula.
		// Positive spread shrinks radii (simple inset).
		// Negative spread grows radii (use cubic interpolation formula).
		sp := shadow.Spread
		var shadowInnerRadii css.EllipticalRadii
		if sp >= 0 {
			shadowInnerRadii = innerRadii.Inset(sp, sp, sp, sp)
		} else {
			shadowInnerRadii = innerRadii.OutsetForBoxShadow(-sp, -sp, -sp, -sp)
		}

		r.setColor(shadow.Color)

		// Use offscreen buffer for all inset shadows: fill the padding box
		// with shadow color, clear the inner hole, optionally blur, then
		// composite clipped to the padding box.
		r.drawInsetShadowBuffer(px, py, pw, ph, innerRadii,
			ix, iy, iw, ih, shadowInnerRadii, shadow)
	}
}

// drawInsetShadowBuffer renders an inset shadow using an offscreen buffer.
// Fills the buffer with shadow color, clears the inner hole, optionally blurs,
// then clips to the padding box and composites.
func (r *Renderer) drawInsetShadowBuffer(
	px, py, pw, ph float64, clipRadii css.EllipticalRadii,
	ix, iy, iw, ih float64, innerRadii css.EllipticalRadii,
	shadow css.BoxShadow,
) {
	sigma := shadow.Blur / 2
	extend := math.Ceil(sigma * 3)

	// Buffer covers the padding box + blur extend.
	bx := px - extend
	by := py - extend
	bw := int(math.Ceil(pw + 2*extend))
	bh := int(math.Ceil(ph + 2*extend))
	if bw <= 0 || bh <= 0 || bw > 2000 || bh > 2000 {
		return
	}

	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))

	// Fill the entire buffer with shadow color.
	sc := color.RGBA{
		R: shadow.Color.R,
		G: shadow.Color.G,
		B: shadow.Color.B,
		A: uint8(shadow.Color.A * 255),
	}
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			off := y*buf.Stride + x*4
			buf.Pix[off+0] = sc.R
			buf.Pix[off+1] = sc.G
			buf.Pix[off+2] = sc.B
			buf.Pix[off+3] = sc.A
		}
	}

	// Clear the inner hole (set to transparent) by drawing the inner shape
	// as a mask and clearing pixels inside it. Uses the draw context's
	// rasterizer for rounded corners to match other path rendering exactly.
	if iw > 0 && ih > 0 {
		lix := ix - bx
		liy := iy - by

		// Rasterize the inner shape to a mask buffer.
		holeMask := image.NewRGBA(image.Rect(0, 0, bw, bh))
		childDC := r.dc.NewChildContext(holeMask)
		childDC.SetColor(color.White)
		if !innerRadii.IsZero() {
			buildRoundedRectPathOnDC(childDC, lix, liy, iw, ih, innerRadii)
			childDC.Fill()
		} else {
			childDC.DrawRectangle(lix, liy, iw, ih)
			childDC.Fill()
		}

		// Clear pixels where the hole mask is opaque.
		for y := 0; y < bh; y++ {
			for x := 0; x < bw; x++ {
				moff := y*holeMask.Stride + x*4
				a := holeMask.Pix[moff+3] // alpha channel
				if a > 0 {
					off := y*buf.Stride + x*4
					if a == 255 {
						buf.Pix[off+0] = 0
						buf.Pix[off+1] = 0
						buf.Pix[off+2] = 0
						buf.Pix[off+3] = 0
					} else {
						// Partial coverage: blend.
						keep := 255 - uint16(a)
						buf.Pix[off+0] = uint8(uint16(buf.Pix[off+0]) * keep / 255)
						buf.Pix[off+1] = uint8(uint16(buf.Pix[off+1]) * keep / 255)
						buf.Pix[off+2] = uint8(uint16(buf.Pix[off+2]) * keep / 255)
						buf.Pix[off+3] = uint8(uint16(buf.Pix[off+3]) * keep / 255)
					}
				}
			}
		}
	}

	// Apply box blur if needed.
	if shadow.Blur > 0 {
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
	}

	// Clip the buffer to the padding box before compositing.
	r.clipInsetShadowBuffer(buf, bx, by, px, py, pw, ph, clipRadii)

	r.dc.DrawImage(buf, int(math.Round(bx)), int(math.Round(by)))
}

// clipInsetShadowBuffer zeroes pixels outside the clip rect in a shadow buffer.
// Uses a rasterized clip mask for rounded corners to match path rendering exactly.
func (r *Renderer) clipInsetShadowBuffer(buf *image.RGBA, bx, by, cx, cy, cw, ch float64, radii css.EllipticalRadii) {
	bounds := buf.Bounds()
	bw, bh := bounds.Dx(), bounds.Dy()
	hasRadius := !radii.IsZero()

	if !hasRadius {
		// Simple rectangular clip.
		for y := 0; y < bh; y++ {
			for x := 0; x < bw; x++ {
				px := bx + float64(x)
				py := by + float64(y)
				if px < cx || px >= cx+cw || py < cy || py >= cy+ch {
					off := y*buf.Stride + x*4
					buf.Pix[off+0] = 0
					buf.Pix[off+1] = 0
					buf.Pix[off+2] = 0
					buf.Pix[off+3] = 0
				}
			}
		}
		return
	}

	// Rasterize the rounded clip rect to a mask.
	clipMask := image.NewRGBA(image.Rect(0, 0, bw, bh))
	childDC := r.dc.NewChildContext(clipMask)
	childDC.SetColor(color.White)
	lcx, lcy := cx-bx, cy-by
	buildRoundedRectPathOnDC(childDC, lcx, lcy, cw, ch, radii)
	childDC.Fill()

	// Zero out pixels outside the clip mask.
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			moff := y*clipMask.Stride + x*4
			a := clipMask.Pix[moff+3]
			if a == 0 {
				off := y*buf.Stride + x*4
				buf.Pix[off+0] = 0
				buf.Pix[off+1] = 0
				buf.Pix[off+2] = 0
				buf.Pix[off+3] = 0
			} else if a < 255 {
				// Partial coverage: reduce.
				off := y*buf.Stride + x*4
				buf.Pix[off+0] = uint8(uint16(buf.Pix[off+0]) * uint16(a) / 255)
				buf.Pix[off+1] = uint8(uint16(buf.Pix[off+1]) * uint16(a) / 255)
				buf.Pix[off+2] = uint8(uint16(buf.Pix[off+2]) * uint16(a) / 255)
				buf.Pix[off+3] = uint8(uint16(buf.Pix[off+3]) * uint16(a) / 255)
			}
		}
	}
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
