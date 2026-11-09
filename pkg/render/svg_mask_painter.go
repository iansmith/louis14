package render

import (
	"image"
	"math"

	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// svgMaskPainter resolves a `mask-image: url(#id)` (and the
// `mask: url(#id)` shorthand) reference against the document's SVG
// resource registry and rasterizes the resolved `<mask>` element's
// children into an alpha buffer suitable for kDstIn compositing
// (dst.A *= mask.A) onto the masked element's offscreen buffer.
//
// Mirrors Blink's SVGMaskPainter
// (core/paint/svg_mask_painter.{h,cc}): the masker's contents are
// rasterized over the referencing element's reference box, the
// resulting buffer's luminance (or alpha, per mask-type) becomes the
// mask value, and the masked content is composited as kDstIn.
//
// Phase 5 only implements the case where the masked element is a CSS
// box (not an SVG shape) — that's the case the
// css-masking/mask-opacity-1d gate exercises. The CSS hook is in
// paintLayerWithMask's mask resolution: if `mask-image` is
// `url(#fragment)` and the fragment resolves to an SVGResourceMasker,
// take the SVG branch.

// Phase 6: URL fragment parsing moved to css.ParseURLReference. All
// callsites in this package now consume that shared helper so `mask`,
// `mask-image`, `clip-path`, `filter`, and `fill`/`stroke` paint
// server references parse uniformly. Mirrors Blink's
// `CSSURIValue::FragmentIdentifier`.

// rasterizeSVGMask rasterizes the `<mask>`'s children into an
// *image.RGBA at the device-pixel resolution of `refBox` (the
// referencing element's border-box reference rect, already in device
// pixels). Returns the buffer aligned so pixel (0, 0) corresponds to
// the top-left of `refBox`.
//
// Mirrors Blink's SVGMaskPainter::PaintSVGMaskForCSS for the CSS-box
// case: paint the mask subtree into an offscreen buffer scaled to
// MaskToContentTransform, then read out luminance or alpha per
// MaskType.
//
// `bufW`, `bufH` are the buffer dimensions (must match the caller's
// element buffer for the composite step). The mask region (resolved
// via masker.ResourceBoundingBox) is the area within `refBox` where
// the mask actually has content; outside the mask region the mask
// value is 0 (so the referencing element is fully clipped there per
// SVG 1.1 §14.4: "Outside the bounding box defined by x, y, width
// and height the effect is as if the mask had a fill value of 0
// (black, transparent)").
func (r *Renderer) rasterizeSVGMask(masker *svg.SVGResourceMasker, refBox geometry.RectF, bufW, bufH int, bx, by int) *image.RGBA {
	// A CSS box's user space has its origin at the box (mask-opacity-1d's
	// mask <rect y="50"> covers the bottom half of its 100x100 div), and the
	// box is its viewport.
	size := geometry.SizeF{Width: refBox.Width(), Height: refBox.Height()}
	return r.rasterizeSVGElementMask(masker, geometry.NewRectF(0, 0, size.Width, size.Height), size,
		bufW, bufH, bx, by, geometry.Translate(refBox.X(), refBox.Y()).ToTransform())
}

// compositeBufferWithOpacityOnto draws a premultiplied buffer onto the target at
// offset (dx, dy) under a layer's `opacity` (0..1): Blink's SaveLayerAlphaOp
// (ConversionContext::StartEffect,
// platform/graphics/compositing/paint_chunks_to_cc_layer.cc:734 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). CSS Masking 1 §3.7: mask, then
// opacity, then composite.
func compositeBufferWithOpacityOnto(target, src *image.RGBA, dx, dy int, opacity float64) {
	if target == nil || src == nil {
		return
	}
	sb := src.Bounds()
	rasterPipelineSpriteBlit(target, sb.Add(image.Pt(dx, dy)), src, sb.Min, clampFilter01(opacity))
}

// applyConstantAlphaToBuffer multiplies every channel of `dst` by
// the constant `alpha` ∈ [0, 1], for a layer that goes through blendComposite.
// The buffer stays premultiplied throughout.
func applyConstantAlphaToBuffer(dst *image.RGBA, alpha float64) {
	if dst == nil || alpha >= 1.0 {
		return
	}
	if alpha < 0 {
		alpha = 0
	}
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i+0] = uint8(math.Round(float64(dst.Pix[i+0]) * alpha))
		dst.Pix[i+1] = uint8(math.Round(float64(dst.Pix[i+1]) * alpha))
		dst.Pix[i+2] = uint8(math.Round(float64(dst.Pix[i+2]) * alpha))
		dst.Pix[i+3] = uint8(math.Round(float64(dst.Pix[i+3]) * alpha))
	}
}

// applySVGMaskToBuffer modulates the alpha channel of `dst` by the
// mask values derived from `maskSrc` per `maskType`. This is the
// kDstIn composite: dst.A *= mask.A (where mask.A is luminance(src)
// for SVGMaskTypeLuminance and alpha(src) for SVGMaskTypeAlpha).
//
// `dst` is the referencing element's offscreen buffer (premultiplied
// RGBA, image.NewRGBA convention). `maskSrc` is the rasterized mask
// buffer (same dimensions as dst).
//
// Mirrors Blink's kDstIn step in SVGMaskPainter::ApplyMaskInImage.
func applySVGMaskToBuffer(dst *image.RGBA, maskSrc *image.RGBA, maskType svg.SVGMaskType) {
	if dst == nil || maskSrc == nil {
		return
	}
	if dst.Bounds() != maskSrc.Bounds() {
		// Shouldn't happen — rasterizeSVGMask sizes the mask buffer
		// to dst's dimensions. Bail rather than risk an out-of-bounds.
		return
	}
	w := dst.Bounds().Dx()
	h := dst.Bounds().Dy()
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			var maskValue uint8
			switch maskType {
			case svg.SVGMaskTypeAlpha:
				maskValue = alphaAlpha(maskSrc, px, py)
			default: // SVGMaskTypeLuminance
				maskValue = luminanceAlpha(maskSrc, px, py)
			}
			if maskValue == 255 {
				continue // unchanged
			}
			di := dst.PixOffset(px, py)
			if maskValue == 0 {
				dst.Pix[di+0] = 0
				dst.Pix[di+1] = 0
				dst.Pix[di+2] = 0
				dst.Pix[di+3] = 0
				continue
			}
			f := float64(maskValue) / 255.0
			dst.Pix[di+0] = uint8(math.Round(float64(dst.Pix[di+0]) * f))
			dst.Pix[di+1] = uint8(math.Round(float64(dst.Pix[di+1]) * f))
			dst.Pix[di+2] = uint8(math.Round(float64(dst.Pix[di+2]) * f))
			dst.Pix[di+3] = uint8(math.Round(float64(dst.Pix[di+3]) * f))
		}
	}
}
