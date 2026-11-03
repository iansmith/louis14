package render

import (
	"mazarin/textshape"

	"louis14/pkg/graphics"
)

// The clip calls of Blink's GraphicsContext, applied to the Renderer's draw
// context (platform/graphics/graphics_context.h:376-393,
// graphics_context.cc:913-954 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Each forwards to SkCanvas::clipPath(path, op, doAntiAlias), which the
// draw context mirrors as ClipPath. Fills, strokes and images anti-alias by
// default (GraphicsContextState, graphics_context_state.cc:22-30); clips
// choose per call.

// clipPath clips to the path built on dc (GraphicsContext::ClipPath,
// graphics_context.cc:942-947; Blink's default is kNotAntiAliased).
func clipPath(dc textshape.DrawContext, aa graphics.AntiAliasingMode, op textshape.ClipOp) {
	dc.ClipPath(op, aa == graphics.AntiAliased)
}

// clipContouredRect clips to rr (GraphicsContext::ClipContouredRect,
// graphics_context.cc:913-929, anti-aliased by default).
func (r *Renderer) clipContouredRect(rr FloatRoundedRect, op textshape.ClipOp, aa graphics.AntiAliasingMode) {
	rr.addPath(r.dc)
	clipPath(r.dc, aa, op)
}

// clipOutContouredRect removes rr from the clip, anti-aliased
// (GraphicsContext::ClipOutContouredRect, graphics_context.cc:931-933).
func (r *Renderer) clipOutContouredRect(rr FloatRoundedRect) {
	r.clipContouredRect(rr, textshape.ClipOpDifference, graphics.AntiAliased)
}

// clipOut removes the rect (x, y, w, h) from the clip without
// anti-aliasing (GraphicsContext::ClipOut, graphics_context.h:382-387).
func (r *Renderer) clipOut(x, y, w, h float64) {
	r.dc.DrawRectangle(x, y, w, h)
	clipPath(r.dc, graphics.NotAntiAliased, textshape.ClipOpDifference)
}
