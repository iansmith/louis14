package render

import (
	"math"

	"mazarin/textshape"

	"louis14/pkg/graphics"
)

// drawLineAsStroke paints a dashed or dotted horizontal text-decoration
// line in the rect at (x, top), width wide and thickness tall: Blink's
// DrawLineAsStroke (core/paint/decoration_line_painter.cc:55-101 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), reached from
// DecorationLinePainter::Paint (:437-476). The line is snapped by
// GetSnappedPointsForTextLine (:47-53), its dashes fitted to its length by
// StyledStrokeData::ConvertToStrokeData, and it is stroked through
// strokeWithDash, starting the pattern at its own start.
//
// keep, when not nil, is the parts of the line that skip-ink leaves: the
// whole line strokes with the stretches between them clipped out, so its
// dashes run on unbroken through the skipped ink, as Blink's
// non-anti-aliased ClipOut of each glyph intercept does (TextPainter::ClipDecorationLine,
// core/paint/text_painter.cc:601-627; GraphicsContext::ClipOut,
// platform/graphics/graphics_context.h:385-387, same SHA).
func (r *Renderer) drawLineAsStroke(x, top, width, thickness float64, style strokeStyle, keep []inkSegment) {
	// GetSnappedPointsForTextLine: whole-pixel ends (gfx::Point truncates)
	// on the floored middle of the rect.
	midY := math.Floor(top + math.Max(thickness/2, 0.5))
	startX, endX := math.Trunc(x), math.Trunc(x+width)
	t := math.Round(thickness)
	dash, dashed := dashEffectFromStrokeStyle(style, t, endX-startX, false)

	y := midY
	if int(t)%2 == 1 {
		y += 0.5 // odd widths sit on the pixel grid
	}
	if !strokeIsDashed(t, style) {
		// Round dots' caps reach past the line's ends; pull them in.
		startX, endX = startX+t/2, endX-t/2
	}

	if keep != nil {
		// Clip out the skipped stretches between the kept segments, over the
		// line's band (Blink outsets each intercept 1px vertically).
		r.dc.Push()
		defer r.dc.Pop()
		bandTop, bandHeight := top-1, thickness+2
		from := x
		for _, s := range keep {
			if s.Start > from {
				r.dc.DrawRectangle(from, bandTop, s.Start-from, bandHeight)
			}
			from = max(from, s.End)
		}
		if x+width > from {
			r.dc.DrawRectangle(from, bandTop, x+width-from, bandHeight)
		}
		clipPath(r.dc, graphics.NotAntiAliased, textshape.ClipOpDifference)
	}
	r.strokeWithDash(func(b pathBuilder) {
		b.MoveTo(startX, y)
		b.LineTo(endX, y)
	}, thickness, dash, dashed)
}

// decorationStrokeStyle maps a dashed or dotted text-decoration-style to
// its stroke style (TextDecorationStyleToStrokeStyle,
// core/paint/text_decoration_info.cc:94-108 @ a9f50e52); ok is false for
// any other style.
func decorationStrokeStyle(style string) (strokeStyle, bool) {
	switch style {
	case "dashed":
		return dashedStroke, true
	case "dotted":
		return dottedStroke, true
	}
	return 0, false
}
