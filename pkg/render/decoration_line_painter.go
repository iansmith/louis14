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
		// Blink outsets each intercept 1px vertically.
		r.dc.Push()
		defer r.dc.Pop()
		r.clipOutSkippedInk(x, width, top-1, thickness+2, keep)
	}
	r.strokeWithDash(func(b pathBuilder) {
		b.MoveTo(startX, y)
		b.LineTo(endX, y)
	}, thickness, dash, dashed)
}

// clipOutSkippedInk clips out the stretches of [x, x+width] that skip-ink
// leaves between the kept segments, over the band [bandTop,
// bandTop+bandHeight], so the line paints on unbroken through them as
// Blink's non-anti-aliased ClipOut of each glyph intercept does
// (TextPainter::ClipDecorationLine, core/paint/text_painter.cc:574-627 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The caller saves and restores
// the clip.
func (r *Renderer) clipOutSkippedInk(x, width, bandTop, bandHeight float64, keep []inkSegment) {
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

// waveDefinition is Blink's WaveDefinition: one cubic Bézier per
// wavelength, its control points controlPointDistance either side of the
// centreline, the path starting phase before the line
// (core/paint/decoration_line_painter.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type waveDefinition struct {
	wavelength, controlPointDistance, phase float64
}

// makeWave is Blink's MakeWave (core/paint/decoration_line_painter.cc:179-194
// @ a9f50e52): half-pixel wavelength and control-point distance for
// cleaner anti-aliasing, and a one-wavelength lead-in so the line clips
// the same way at both ends. math.Round, like std::round, rounds half away
// from zero.
func makeWave(thickness float64) waveDefinition {
	t := math.Max(1, thickness)
	wavelength := 1 + 2*math.Round(2*t+0.5)
	return waveDefinition{
		wavelength:           wavelength,
		controlPointDistance: 0.5 + math.Round(3*t+0.5),
		phase:                -wavelength,
	}
}

// wavyPaintRect is WavyGeometry::PaintRect's vertical extent
// (decoration_line_painter.cc:281-289 @ a9f50e52) for a line whose rect
// top is top: the wave's stroke bounds about its centreline (path y 0.5,
// ComputeWavyPatternRect :200-212, floored and ceiled to whole pixels),
// moved by wavyOffset. A cubic from (0,0) through (w/2, ±d) to (w, 0)
// peaks d·√3/6 off its chord, and the stroke adds half its thickness.
func wavyPaintRect(top, thickness, wavyOffset float64) (rectTop, rectHeight float64) {
	reach := makeWave(thickness).controlPointDistance*math.Sqrt(3)/6 + thickness/2
	boundsTop := math.Floor(0.5 - reach)
	return top + wavyOffset + boundsTop, math.Ceil(0.5+reach) - boundsTop
}

// drawWavyLine paints a wavy text-decoration line whose rect is at (x, top),
// width wide and thickness thick: Blink's WavyGeometry::PaintRibbon
// (core/paint/decoration_line_painter.cc:325-349 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which the HTML tile shader
// of PaintWavyTextDecoration (:477-524) paints identically. The
// centreline (WavyCenterlinePath, :129-144) runs wavyOffset + 0.5 below
// top from one wavelength before x to one past the end, first bending
// away from the text, and is stroked with a default StrokeData
// (strokeWithDash: miter joins, butt caps) thickness wide and clipped to
// wavyPaintRect. wavyOffset is TextDecorationInfo::ComputeLineData's
// (core/paint/text_decoration_info.cc:255-280): thickness + 1 for an
// underline, its negation for an overline, 0 for a line-through. keep is
// as for drawLineAsStroke.
func (r *Renderer) drawWavyLine(x, top, width, thickness, wavyOffset float64, keep []inkSegment) {
	if width <= 0 {
		return
	}
	wave := makeWave(thickness)
	rectTop, rectHeight := wavyPaintRect(top, thickness, wavyOffset)
	r.dc.Push()
	defer r.dc.Pop()
	r.dc.DrawRectangle(x, rectTop, width, rectHeight)
	clipPath(r.dc, graphics.AntiAliased, textshape.ClipOpIntersect)
	if keep != nil {
		// ClipDecorationLine insets the bounds 0.5px vertically, then
		// outsets each intercept 1px.
		r.clipOutSkippedInk(x, width, rectTop-0.5, rectHeight+1, keep)
	}

	y := top + wavyOffset + 0.5
	half := wave.wavelength / 2
	r.strokeWithDash(func(b pathBuilder) {
		cx := x + wave.phase
		b.MoveTo(cx, y)
		for end := x + width + wave.wavelength; cx < end; cx += wave.wavelength {
			b.CubicTo(cx+half, y+wave.controlPointDistance, cx+half, y-wave.controlPointDistance, cx+wave.wavelength, y)
		}
	}, thickness, dashDescription{}, false)
}

// doubleOffsetFromThickness is how far a double line's second line, and a
// wavy line's centreline offset, sit from the first line: thickness + 1
// (TextDecorationInfo::ComputeLineData's double_offset_from_thickness,
// core/paint/text_decoration_info.cc:259-260 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), away from the text for an
// underline or overline.
func doubleOffsetFromThickness(thickness float64) float64 { return thickness + 1 }

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
