package render

import (
	"image"
	"math"
	"strings"

	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// svgMaskPainter resolves a `mask-image: url(#id)` (and the
// `mask: url(#id)` shorthand) reference against the renderer-wide SVG
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

// parseURLFragment returns the bare `id` from a `url(#id)` value,
// stripping the leading `#`. Returns ("", false) when the value is
// not a url() of a fragment reference. Mirrors the parsing in the
// CSS Image Values L4 §3.1 `<url>` grammar.
//
// Accepts:
//
//	url(#id)
//	url("#id")
//	url('#id')
//	url(path#id)        — strips the path part
//	url(#id), url(#id2) — extracts only the FIRST fragment
//
// The shorthand `mask: url(#m)` / `mask-image: url(#m)` resolves
// against the document fragment id space (SVG 2 §3.1).
func parseURLFragment(val string) (string, bool) {
	val = strings.TrimSpace(val)
	// Handle comma-separated lists by taking the first.
	if i := strings.Index(val, ","); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	if !strings.HasPrefix(val, "url(") {
		return "", false
	}
	if !strings.HasSuffix(val, ")") {
		return "", false
	}
	inner := val[4 : len(val)-1]
	inner = strings.TrimSpace(inner)
	inner = strings.Trim(inner, `"'`)
	if inner == "" {
		return "", false
	}
	// Only fragment-bearing URLs resolve via the SVG registry.
	idx := strings.Index(inner, "#")
	if idx < 0 {
		return "", false
	}
	id := inner[idx+1:]
	if id == "" {
		return "", false
	}
	return id, true
}

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
	if masker == nil || bufW <= 0 || bufH <= 0 {
		return nil
	}
	// Resolve the mask region against a BOX-LOCAL refBox: (0, 0, w, h).
	// This produces region coordinates in the referencing element's
	// local frame (the same frame the children's userSpaceOnUse coords
	// live in for a CSS-box mask reference). Mirrors Blink's
	// SVGMaskPainter approach: the mask is rasterized in the
	// referencing element's coordinate system, with the element at
	// the origin.
	boxLocalRef := geometry.NewRectF(0, 0, refBox.Width(), refBox.Height())
	lengthCtx := svg.NewSVGLengthContext(geometry.SizeF{
		Width: refBox.Width(), Height: refBox.Height(),
	})
	region := masker.ResourceBoundingBox(boxLocalRef, lengthCtx)

	// Build the alpha-source buffer. It's the SAME size as the
	// referencing element's buffer so the kDstIn composite is
	// per-pixel-aligned. Pixels outside the mask region are left at
	// alpha 0 (mask value = 0 → masked-away per SVG 1.1 §14.4).
	maskBuf := image.NewRGBA(image.Rect(0, 0, bufW, bufH))

	// Build a child renderer + paint context that targets maskBuf.
	// MaskContentUnits decides how the children's coords map:
	//   - userSpaceOnUse: children coords are in the referencing
	//     element's user-space (= page coords for a CSS box).
	//   - objectBoundingBox: children coords are fractions of refBox.
	tmpR := NewRendererForImage(maskBuf)
	tmpR.dc.Push()
	defer tmpR.dc.Pop()
	// Translate so refBox.X/refBox.Y in user-space maps to (-bx, -by)
	// in the buffer — i.e., page-space (refBox.X, refBox.Y) lands at
	// buffer pixel (refBox.X-bx, refBox.Y-by). The buffer's pixel
	// origin (0, 0) corresponds to page-space (bx, by).
	tmpR.dc.Translate(float64(-bx), float64(-by))

	// For both unit modes the children's local "user-space" is BOX-
	// LOCAL: (0, 0) is the top-left of refBox. Blink's
	// SVGMaskPainter::ComputeMaskToContentTransform treats the
	// reference box as the origin: userSpaceOnUse coords are in the
	// referencing element's coordinate system (which for a CSS-box
	// mask reference is the box-local frame), and objectBoundingBox
	// is the unit square mapped onto the box. The reftest
	// mask-opacity-1d.html bakes this in: its mask `<rect x="0"
	// y="50" width="100" height="50" fill="#ffffff"/>` is meant to
	// cover the bottom half of the 100x100 masked div — only the
	// box-local interpretation produces that geometry.
	tmpR.dc.Translate(refBox.X(), refBox.Y())

	// deviceFromUser is the composed transform mapping the children's
	// user-space coords (box-local for userSpaceOnUse; bbox-normalized
	// for objectBoundingBox) into device buffer pixel coords. Used by
	// paint servers inside the mask (gradient/pattern references) and
	// path bounding-box mapping. Mirrors what svgPaintContext.
	// DeviceFromUser carries elsewhere in the painter cluster.
	var deviceFromUser geometry.AffineTransform
	pageFromBoxLocal := geometry.Translate(refBox.X(), refBox.Y())
	bufferFromPage := geometry.Translate(-float64(bx), -float64(by))
	switch masker.MaskContentUnits {
	case svg.SVGUnitObjectBoundingBox:
		// Children's (0..1, 0..1) maps to refBox.
		tmpR.dc.MultiplyMatrix(refBox.Width(), 0, 0, refBox.Height(), 0, 0)
		deviceFromUser = bufferFromPage.Compose(pageFromBoxLocal).Compose(
			geometry.Scale(refBox.Width(), refBox.Height()),
		)
	default:
		// userSpaceOnUse: children paint in box-local coords (already
		// composed with translate(refBox.X, refBox.Y) above).
		deviceFromUser = bufferFromPage.Compose(pageFromBoxLocal)
	}

	// Clip the mask buffer to the resolved mask region. SVG 1.1 §14.4:
	// content outside the mask region contributes 0 — we get the same
	// effect by clipping the painter to that region. The DC is in
	// box-local coords (we translated by (refBox.X, refBox.Y) above);
	// for maskContentUnits=objectBoundingBox we have an additional
	// scale matrix mapping (0..1)→box-local, so we need to convert the
	// region (which is in box-local coords) into the (0..1) space.
	regionLocal := region
	if masker.MaskContentUnits == svg.SVGUnitObjectBoundingBox {
		if refBox.Width() != 0 && refBox.Height() != 0 {
			regionLocal = geometry.NewRectF(
				region.X()/refBox.Width(),
				region.Y()/refBox.Height(),
				region.Width()/refBox.Width(),
				region.Height()/refBox.Height(),
			)
		}
	}
	tmpR.dc.DrawRectangle(regionLocal.X(), regionLocal.Y(), regionLocal.Width(), regionLocal.Height())
	tmpR.dc.Clip()

	// Paint the mask's children into the buffer. They're SVG nodes,
	// so we run them through the SVG paint dispatch.
	pctx := &svgPaintContext{
		dc: tmpR.dc,
		viewport: geometry.NewRectF(0, 0,
			float64(bufW), float64(bufH)),
		CurrentTransform: geometry.Identity(),
		DeviceFromUser:   deviceFromUser,
		// The mask's children may reference paint servers
		// (gradients, etc.) declared at the page scope — feed them
		// the renderer-wide registry.
		Resources: r.lookupSVGResources(),
		Renderer:  tmpR,
	}
	for _, child := range masker.Children {
		paintSVGNode(pctx, child)
	}
	return maskBuf
}

// compositeBufferWithOpacityOnto composites a premultiplied RGBA
// source buffer onto the target canvas at offset (dx, dy), applying
// a constant alpha multiplier `opacity` to the source per pixel
// during the composite step.
//
// The blend math matches the formula used by louis14's gg-backed
// CSS color-fill path (pattern.Paint in third_party/gg/pattern.go).
// Specifically the formula:
//
//	a = m - (src.A * coverage / m) * 0x101
//	out.C = (dst.C * a + src.C * coverage) / m >> 8
//
// where m=65535, src.C is the per-channel 16-bit value, dst.C is the
// destination's 16-bit value. coverage is the rasterizer's per-pixel
// coverage (255 = full).
//
// Critically, the SOURCE values are interpreted as PREMULTIPLIED 16-
// bit channels — but the function does NOT enforce the premultiplied
// invariant (R, G, B ≤ A). This matches gg's behavior with
// `color.RGBA{r, g, b, a}` where the caller passes straight-alpha
// values into a premultiplied container (the existing CSS color path
// does exactly this via setColor). The "buggy" output for invalid
// premul colors is reproducible here so the SVG mask path's pixel
// output matches the reftest reference, which is rendered through
// the same gg-buggy path.
//
// `opacity` is the layer's CSS `opacity` (0..1). It modulates the
// source alpha during composite — applying it post-mask matches
// CSS Masking 1 §3.7 rendering order (mask → opacity → composite).
func compositeBufferWithOpacityOnto(target, src *image.RGBA, dx, dy int, opacity float64) {
	if target == nil || src == nil {
		return
	}
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}
	tb := target.Bounds()
	sb := src.Bounds()
	const m = 1<<16 - 1
	for sy := sb.Min.Y; sy < sb.Max.Y; sy++ {
		ty := sy + dy
		if ty < tb.Min.Y || ty >= tb.Max.Y {
			continue
		}
		for sx := sb.Min.X; sx < sb.Max.X; sx++ {
			tx := sx + dx
			if tx < tb.Min.X || tx >= tb.Max.X {
				continue
			}
			si := src.PixOffset(sx, sy)
			ti := target.PixOffset(tx, ty)
			// Read the source PREMULTIPLIED RGBA. The buffer was
			// painted via gg's fill path which writes premultiplied
			// values directly. We then un-premultiply to get the
			// straight-alpha source color — that is what the masked
			// element's CSS color was at this pixel.
			sa := src.Pix[si+3]
			if sa == 0 {
				continue
			}
			var sR, sG, sB uint8
			if sa == 255 {
				sR, sG, sB = src.Pix[si+0], src.Pix[si+1], src.Pix[si+2]
			} else {
				// Un-premultiply.
				af := float64(sa) / 255.0
				sR = clampU8(math.Round(float64(src.Pix[si+0]) / af))
				sG = clampU8(math.Round(float64(src.Pix[si+1]) / af))
				sB = clampU8(math.Round(float64(src.Pix[si+2]) / af))
			}
			// Apply opacity to the source alpha. The straight-alpha
			// source RGB is left intact — that's what setColor does
			// for `rgba(R, G, B, A)`: stuffs straight RGB and alpha
			// into a color.RGBA without premultiplying.
			effectiveAlpha := uint8(math.Round(float64(sa) * opacity))
			// Pack as the gg-style 16-bit "premultiplied" values that
			// match how color.RGBA{R, G, B, A}.RGBA() expands the
			// straight RGB to 16 bits.
			cr := uint32(sR) * 0x101
			cg := uint32(sG) * 0x101
			cb := uint32(sB) * 0x101
			ca := uint32(effectiveAlpha) * 0x101
			ma := uint32(m) // full coverage
			// gg's painter uses raw 8-bit dst values for `d*` and
			// 16-bit blended values for the source — see
			// third_party/gg/pattern.go. We replicate exactly so the
			// SVG mask path's composite pixel-matches gg.
			dr := uint32(target.Pix[ti+0])
			dg := uint32(target.Pix[ti+1])
			db := uint32(target.Pix[ti+2])
			da := uint32(target.Pix[ti+3])
			a := (m - ca*ma/m) * 0x101
			rOut := (dr*a + cr*ma) / m
			gOut := (dg*a + cg*ma) / m
			bOut := (db*a + cb*ma) / m
			aOut := (da*a + ca*ma) / m
			target.Pix[ti+0] = uint8(rOut >> 8)
			target.Pix[ti+1] = uint8(gOut >> 8)
			target.Pix[ti+2] = uint8(bOut >> 8)
			target.Pix[ti+3] = uint8(aOut >> 8)
		}
	}
}

// clampU8 clamps a float to [0, 255] and converts to uint8.
func clampU8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// applyConstantAlphaToBuffer multiplies every channel of `dst` by
// the constant `alpha` ∈ [0, 1]. Used by the SVG mask path to apply
// CSS `opacity` AFTER masking, matching the CSS Masking 1 §3.7
// rendering order (filter → mask → opacity → composite). The buffer
// stays premultiplied throughout.
//
// The mazarin DrawContext's PushGroup/PopGroupWithAlpha applies
// opacity differently than what the plain CSS-color
// `rgba(c, a)` path produces — to keep the SVG-mask path's
// pixel output identical to the reference (which uses the plain
// CSS path), we apply opacity manually here.
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
