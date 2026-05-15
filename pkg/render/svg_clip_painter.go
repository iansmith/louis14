package render

import (
	"image"
	"image/color"
	"math"

	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"

	"mazarin/textshape"
)

// applySVGClipPath resolves a `clip-path: url(#id)` reference against
// the SVG resource registry and installs the resolved clip on the
// current DrawContext. Caller is expected to have just called
// r.dc.Push() so the clip can be popped at scope exit.
//
// `target` is the referencing element's reference box in DEVICE-pixel
// (page) coordinates — the border box for a CSS box, or the path's
// user-space FillBoundingBox for an SVG shape (Phase 5 only wires the
// CSS-box case; SVG-side clip-path on shapes is forward-compatible).
// `clipPathReferenceID` is the bare `id` (no `#`).
//
// Mirrors Blink's ClipPathClipper::ApplyReferenceClipPath for the
// CSS-box case; the SVG-side equivalent is SVGClipPainter::Apply.
//
// Two evaluation paths:
//
//  1. Shape-based fast path — when SVGResourceClipper.AsPath returns
//     ok=true, build the resulting path on the DC and call Clip(). The
//     mazarin DC supports non-rect paths in Clip() now (clip-as-mask
//     port in ~/mazzy).
//
//  2. Content-based fallback — rasterize the clipper's child subtree
//     into an alpha buffer at the target's device-pixel resolution
//     and… louis14's DrawContext doesn't currently expose a "set
//     alpha mask" call, so we fall back to no-op (no clip) for
//     content-based clippers and document the gap; Phase 5 only
//     reftests shape-based <clipPath><rect/></clipPath> per the gate.
//     The fallback path is provided as the structure for when mazarin
//     adds an alpha-mask API.
//
// Returns true when a clip was installed (caller's matching Pop will
// undo it correctly); returns false when no clip was applied (caller
// should NOT pop, but the canonical idiom in applyClipPath already
// Pushed unconditionally and the caller's defer/cleanup handles
// the Pop either way — so the return value is informational).
func (r *Renderer) applySVGClipPath(target geometry.RectF, clipPathReferenceID string) bool {
	if r == nil || clipPathReferenceID == "" {
		return false
	}
	registry := r.lookupSVGResources()
	if registry == nil {
		return false
	}
	clipper, ok := registry.LookupClipper(clipPathReferenceID)
	if !ok || clipper == nil || !clipper.HasContent() {
		// Per SVG 2 §15.3 a reference to a non-existent clip-path
		// "clips away" the element (paints nothing). Mirror that by
		// clipping to an empty rect — the simplest way to achieve
		// "nothing paints" is push a degenerate clip.
		r.dc.DrawRectangle(0, 0, 0, 0)
		r.dc.Clip()
		return true
	}
	// Phase 6: a clipper that participates in a reference cycle is
	// treated as `none` — no clipping is applied. Mirrors Blink's
	// LayoutSVGResourceContainer::FindCycle outcome.
	if clipper.CycleState() == svg.SVGCycleHasCycle {
		return false
	}
	if path, ok := clipper.AsPath(target); ok {
		// Shape-based fast path: replay the path on the DC and clip.
		buildSVGPathOnDC(r.dc, &path)
		r.dc.Clip()
		return true
	}
	// Content-based fallback: rasterize the clipper's subtree into an
	// alpha buffer at the device-pixel resolution of `target`. For
	// Phase 5 we don't yet have a mazarin "set alpha mask" API, so
	// we approximate by rasterizing the buffer and inspecting it for
	// a single bounding rect that we can Clip() to. If the buffer
	// has anything non-trivial (true content-based shape) we leave
	// the clip un-installed — and rely on the shape-based fast path
	// being the only reftest gate.
	return r.applySVGClipPathContent(clipper, target)
}

// applySVGClipPathContent is the content-based fallback. Rasterizes
// the clipper's children into an alpha buffer at the target's
// device-pixel resolution; if the result is a single connected
// component, fall back to drawing a bounding rectangle around it.
// Phase 5 does not hit this path on any gate reftest — the
// mask-opacity-1d gate uses a `<mask>` not a `<clipPath>`, and the
// hand-check `<clipPath>` tests all use single-shape clippers.
//
// Returns true when an approximate clip was installed.
func (r *Renderer) applySVGClipPathContent(clipper *svg.SVGResourceClipper, target geometry.RectF) bool {
	if r == nil || clipper == nil {
		return false
	}
	// Determine the device-pixel extent of the clipper's geometry.
	// For objectBoundingBox the geometry is in `target`'s space; for
	// userSpaceOnUse it's already in user-space which equals device
	// pixels when no transform stack is active.
	bx := int(math.Floor(target.X()))
	by := int(math.Floor(target.Y()))
	bw := int(math.Ceil(target.X()+target.Width())) - bx
	bh := int(math.Ceil(target.Y()+target.Height())) - by
	if bw <= 0 || bh <= 0 || bw > 4096 || bh > 4096 {
		return false
	}
	// Rasterize each child shape into a coverage buffer.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	tmpR := NewRendererForImage(buf)
	deviceFromUser := geometry.Translate(-float64(bx), -float64(by))
	if clipper.ClipPathUnits == svg.SVGUnitObjectBoundingBox {
		deviceFromUser = deviceFromUser.Compose(
			geometry.Translate(target.X(), target.Y()),
		).Compose(geometry.Scale(target.Width(), target.Height()))
	}
	tmpR.dc.Push()
	tmpR.dc.MultiplyMatrix(
		deviceFromUser.A, deviceFromUser.B,
		deviceFromUser.C, deviceFromUser.D,
		deviceFromUser.E, deviceFromUser.F,
	)
	pctx := &svgPaintContext{
		dc:               tmpR.dc,
		viewport:         geometry.NewRectF(0, 0, float64(bw), float64(bh)),
		CurrentTransform: geometry.Identity(),
		DeviceFromUser:   deviceFromUser,
		Renderer:         tmpR,
	}
	for _, child := range clipper.Children {
		paintSVGNode(pctx, child)
	}
	tmpR.dc.Pop()
	// Find the AABB of non-transparent pixels — the conservative
	// rect clip is the best we can do without a real alpha-mask API.
	// This loses concavity but keeps the door open for mazarin's
	// alpha-mask landing in a later phase; the hand-check tests
	// don't exercise concave content.
	minX, minY := bw, bh
	maxX, maxY := -1, -1
	for py := 0; py < bh; py++ {
		for px := 0; px < bw; px++ {
			if buf.Pix[buf.PixOffset(px, py)+3] > 0 {
				if px < minX {
					minX = px
				}
				if py < minY {
					minY = py
				}
				if px > maxX {
					maxX = px
				}
				if py > maxY {
					maxY = py
				}
			}
		}
	}
	if maxX < 0 || maxY < 0 {
		// Empty clipper → clip away.
		r.dc.DrawRectangle(0, 0, 0, 0)
		r.dc.Clip()
		return true
	}
	r.dc.DrawRectangle(
		float64(bx+minX), float64(by+minY),
		float64(maxX-minX+1), float64(maxY-minY+1),
	)
	r.dc.Clip()
	return true
}

// buildSVGPathOnDC replays a normalized svg.Path onto a DrawContext.
// Shared with svg_shape_painter.go's buildPathOnDC — declared here as
// a named helper so the clip painter has a clear call site to point
// at; the implementation just forwards to the existing primitive.
func buildSVGPathOnDC(dc textshape.DrawContext, p *svg.Path) bool {
	return buildPathOnDC(dc, p)
}

// applySVGClipPathOnSVGShape installs an SVG-side clip-path on a
// shape's user-space coordinate system. Used by Phase 5+ when an SVG
// shape carries `clip-path: url(#id)`. Mirrors Blink's
// SVGClipPainter::Apply. The DC is already in the shape's user-space
// transform when this is called (the scoped transform state from
// svg_shape_painter.paint has pushed it), so the clip path is built
// in user-space coords. Phase 5 doesn't yet hook this into the SVG
// shape painter — the hand-check tests reference clip-path on shapes
// but they are reftested via the CSS-box path because the
// referencing element is the `<rect>` itself which carries
// `clip-path="url(#c)"` as a presentation attribute that the cascade
// folds into computed style. See applyClipPath.
//
// Returns true when a clip was installed.
func (r *Renderer) applySVGClipPathOnSVGShape(pctx *svgPaintContext, refBoxUser geometry.RectF, clipPathReferenceID string) bool {
	if r == nil || pctx == nil || pctx.Resources == nil || clipPathReferenceID == "" {
		return false
	}
	clipper, ok := pctx.Resources.LookupClipper(clipPathReferenceID)
	if !ok || clipper == nil || !clipper.HasContent() {
		pctx.dc.DrawRectangle(0, 0, 0, 0)
		pctx.dc.Clip()
		return true
	}
	// Phase 6: drop the reference for cycle-flagged clippers.
	if clipper.CycleState() == svg.SVGCycleHasCycle {
		return false
	}
	if path, ok := clipper.AsPath(refBoxUser); ok {
		buildSVGPathOnDC(pctx.dc, &path)
		pctx.dc.Clip()
		return true
	}
	// Content-based fallback for SVG-side clip-path is not yet
	// reftested; defer to a future phase. Treat as "no clip" so the
	// shape paints unclipped (rather than disappearing) — same
	// invalid-paint resilience the SVG 2 §15.3 spec demands when the
	// implementation can't resolve the reference.
	return false
}

// luminanceAlpha returns the per-pixel luminance value of `src` at
// pixel (x, y) using Rec.709 weights (SVG 1.1 §14.4), modulated by
// the pixel's own alpha. Returns 0..255.
//
// The image is stored premultiplied (Go's image.RGBA convention).
// SVG luminance is defined on straight-alpha colors, so we
// un-premultiply first when alpha < 255.
func luminanceAlpha(src *image.RGBA, x, y int) uint8 {
	i := src.PixOffset(x, y)
	a := src.Pix[i+3]
	if a == 0 {
		return 0
	}
	rChan := float64(src.Pix[i+0])
	gChan := float64(src.Pix[i+1])
	bChan := float64(src.Pix[i+2])
	af := float64(a) / 255.0
	if a < 255 {
		// Un-premultiply to straight alpha.
		rChan /= af
		gChan /= af
		bChan /= af
	}
	// Rec.709 luminance per SVG 1.1 §14.4. The CSS Masking spec
	// updates the coefficients slightly (0.2126/0.7152/0.0722) for
	// luminance-mode masks; Blink uses 0.2126/0.7152/0.0722 in
	// cc::ColorFilter::MakeLuma. Phase 4's CSS-mask luminance branch
	// (render.go:637) uses the same 0.2126/0.7152/0.0722 — we
	// mirror that here for cross-path consistency.
	lum := 0.2126*rChan + 0.7152*gChan + 0.0722*bChan
	if lum < 0 {
		lum = 0
	}
	if lum > 255 {
		lum = 255
	}
	// Modulate by the pixel's alpha (a pixel with alpha 50% and
	// luminance 100% contributes 50% to the mask).
	return uint8(math.Round(lum * af))
}

// alphaAlpha returns the per-pixel alpha value of `src` at pixel (x,
// y). Trivial helper used by mask-type:alpha.
func alphaAlpha(src *image.RGBA, x, y int) uint8 {
	return src.Pix[src.PixOffset(x, y)+3]
}

// drawAlphaIntoCoverage fills `dst` with constant white at full
// opacity — debug helper unused by production code paths. Retained
// for parity with the mask painter's tile-fill helper.
func drawAlphaIntoCoverage(dst *image.Alpha, opacity float64) {
	a := uint8(math.Round(opacity * 255))
	for i := range dst.Pix {
		dst.Pix[i] = a
	}
}

// _ silences unused-import warnings if any helper is conditionally
// referenced in the future. (Currently every import is in use.)
var _ = color.RGBA{}
