package render

import (
	"image"
	"math"

	"mazarin/textshape"

	"louis14/pkg/geometry"
)

// This file ports Blink's dashed/dotted straight-line drawing:
// DrawLineWithStyle and EnforceDotsAtEndpoints
// (core/paint/box_border_painter.cc:401-560 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and the StyledStrokeData dash
// setup it drives (platform/graphics/styled_stroke_data.cc:40-176, same
// SHA). The dashing itself is Skia's SkDashPathEffect, which textshape's
// SetDash implements.

// strokeStyle is Blink's StrokeStyle for a styled line (stroke_data.h).
type strokeStyle int

const (
	dottedStroke strokeStyle = iota
	dashedStroke
)

// strokeIsDashed is StyledStrokeData::StrokeIsDashed
// (styled_stroke_data.cc:174-176): dashed, or dotted at most 3px wide,
// which draws square dots as dashes.
func strokeIsDashed(width float64, style strokeStyle) bool {
	return style == dashedStroke || (style == dottedStroke && width <= 3)
}

// selectBestDashGap is SelectBestDashGap (styled_stroke_data.cc:40-58):
// the gap, near gapLength, that fits a whole number of dashes in the
// stroke.
func selectBestDashGap(strokeLength, dashLength, gapLength float64, closedPath bool) float64 {
	available := strokeLength + gapLength
	if closedPath {
		available = strokeLength
	}
	minDashes := math.Floor(available / (dashLength + gapLength))
	maxDashes := minDashes + 1
	minGaps, maxGaps := minDashes-1, maxDashes-1
	if closedPath {
		minGaps, maxGaps = minDashes, maxDashes
	}
	minGap := (strokeLength - minDashes*dashLength) / minGaps
	maxGap := (strokeLength - maxDashes*dashLength) / maxGaps
	if maxGap <= 0 || math.Abs(minGap-gapLength) < math.Abs(maxGap-gapLength) {
		return minGap
	}
	return maxGap
}

// dashLengthRatio and dashGapRatio are DashLengthRatio / DashGapRatio
// (styled_stroke_data.cc:64-74): thin lines get longer dashes and gaps.
func dashLengthRatio(thickness float64) float64 {
	if thickness >= 3 {
		return 2
	}
	return 3
}

func dashGapRatio(thickness float64) float64 {
	if thickness >= 3 {
		return 1
	}
	return 2
}

// dashDescription is a dash pattern: on-length on, off-length off, and
// round caps (a zero-length dash with round caps is a dot).
type dashDescription struct {
	on, off  float64
	roundCap bool
}

// dashEffectFromStrokeStyle is DashEffectFromStrokeStyle
// (styled_stroke_data.cc:81-134) for a stroke whose dash thickness is
// dashWidth over pathLength; ok is false for a plain solid stroke.
func dashEffectFromStrokeStyle(style strokeStyle, dashWidth, pathLength float64, closedPath bool) (dashDescription, bool) {
	if strokeIsDashed(dashWidth, style) {
		dashLength, gapLength := dashWidth, dashWidth
		if style == dashedStroke {
			dashLength *= dashLengthRatio(dashWidth)
			gapLength *= dashGapRatio(dashWidth)
		}
		if pathLength <= dashLength*2 {
			return dashDescription{}, false // no space for dashes
		}
		twoDashesWithGap := 2*dashLength + gapLength
		if closedPath {
			twoDashesWithGap += gapLength
		}
		if pathLength <= twoDashesWithGap {
			m := pathLength / twoDashesWithGap
			return dashDescription{on: dashLength * m, off: gapLength * m}, true
		}
		gap := gapLength
		if style == dashedStroke {
			gap = selectBestDashGap(pathLength, dashLength, gapLength, closedPath)
		}
		return dashDescription{on: dashLength, off: gap}, true
	}
	// Dotted wider than 3px: round dots, spaced as evenly as possible.
	perDot := dashWidth * 2
	if pathLength < perDot {
		return dashDescription{off: perDot, roundCap: true}, true
	}
	// The epsilon keeps the last dot from being dropped to rounding.
	const epsilon = 1.0e-2
	gap := selectBestDashGap(pathLength, dashWidth, dashWidth, closedPath)
	return dashDescription{off: gap + dashWidth - epsilon, roundCap: true}, true
}

// drawLineWithStyle is DrawLineWithStyle (box_border_painter.cc:501-560): a
// horizontal or vertical dashed or dotted line from p1 to p2, thickness
// wide, in the current color.
func (r *Renderer) drawLineWithStyle(p1, p2 image.Point, style strokeStyle, thickness float64) {
	vertical := p1.X == p2.X
	width := int(math.Round(thickness))
	d := p2.Sub(p1)
	length := d.X + d.Y
	dash, dashed := dashEffectFromStrokeStyle(style, float64(width), float64(length), false)

	a := geometry.PointF{X: float64(p1.X), Y: float64(p1.Y)}
	b := geometry.PointF{X: float64(p2.X), Y: float64(p2.Y)}
	if style == dottedStroke {
		if strokeIsDashed(float64(width), style) {
			r.enforceDotsAtEndpoints(&a, &b, length, width, vertical)
		} else {
			// Round dots' caps reach past the line's ends; pull them in.
			half := float64(width) / 2
			if vertical {
				a.Y, b.Y = a.Y+half, b.Y-half
			} else {
				a.X, b.X = a.X+half, b.X-half
			}
		}
	}
	// An odd width centres on a half pixel.
	if width%2 == 1 {
		if vertical {
			a.X, b.X = a.X+0.5, b.X+0.5
		} else {
			a.Y, b.Y = a.Y+0.5, b.Y+0.5
		}
	}
	r.strokeAxisLine(a, b, thickness, dash, dashed)
}

// enforceDotsAtEndpoints is EnforceDotsAtEndpoints
// (box_border_painter.cc:401-499): for a dotted line at most 3px wide whose
// length is not an odd multiple of its width, it draws whole start and end
// dots itself and moves the line's ends in past them.
func (r *Renderer) enforceDotsAtEndpoints(p1, p2 *geometry.PointF, length, width int, vertical bool) {
	mod4, mod6 := length%4, length%6
	useStartDot, startDotGrowth, startLineOffset := false, 0, 0
	useEndDot, endDotGrowth := false, 0
	if (width == 1 && length%2 == 0) || (width == 3 && mod6 == 0) {
		useStartDot, startDotGrowth, startLineOffset = true, 1, 1
	}
	if (width == 2 && (mod4 == 0 || mod4 == 1)) || (width == 3 && (mod6 == 1 || mod6 == 2)) {
		useStartDot, startLineOffset = true, -1
	}
	if (width == 2 && mod4 == 0) || (width == 3 && mod6 == 1) {
		useEndDot = true
	}
	if (width == 2 && mod4 == 3) || (width == 3 && (mod6 == 4 || mod6 == 5)) {
		useStartDot, startLineOffset = true, 1
	}
	if width == 3 && mod6 == 5 {
		useEndDot = true
	} else if width == 3 && mod6 == 0 {
		useEndDot, endDotGrowth = true, 1
	}
	w, half := float64(width), float64(width/2)
	if useStartDot {
		g := float64(startDotGrowth)
		if vertical {
			r.fillRectF(p1.X-half, p1.Y, w, w+g)
			p1.Y += 2*w + float64(startLineOffset)
		} else {
			r.fillRectF(p1.X, p1.Y-half, w+g, w)
			p1.X += 2*w + float64(startLineOffset)
		}
	}
	if useEndDot {
		g := float64(endDotGrowth)
		if vertical {
			r.fillRectF(p2.X-half, p2.Y-w-g, w, w+g)
			p2.Y -= w + g + 1
		} else {
			r.fillRectF(p2.X-w-g, p2.Y-half, w+g, w)
			p2.X -= w + g + 1
		}
	}
}

// strokeAxisLine strokes the line a→b, width wide with butt caps, dashed
// by dash when dashed.
func (r *Renderer) strokeAxisLine(a, b geometry.PointF, width float64, dash dashDescription, dashed bool) {
	r.strokeWithDash(func(p pathBuilder) {
		p.MoveTo(a.X, a.Y)
		p.LineTo(b.X, b.Y)
	}, width, dash, dashed)
}

// strokeStyledPath strokes the path build adds, styled dashed or dotted:
// StyledStrokeData::ConvertToStrokeData sized to the path's length and
// closedness (styled_stroke_data.cc:163-172), then
// GraphicsContext::StrokePath. dashWidth is the dash pattern's thickness,
// which can differ from the stroke's thickness.
func (r *Renderer) strokeStyledPath(build func(pathBuilder), style strokeStyle, thickness, dashWidth float64) {
	var m pathMeasure
	build(&m)
	dash, dashed := dashEffectFromStrokeStyle(style, dashWidth, math.Trunc(m.length), m.closed)
	r.strokeWithDash(build, thickness, dash, dashed)
}

// strokeWithDash strokes the path build adds with a StrokeData: thickness
// wide, miter joins, butt caps or the dash's round caps, dashed by dash
// when dashed (an on+off SkDashPathEffect with phase 0).
func (r *Renderer) strokeWithDash(build func(pathBuilder), thickness float64, dash dashDescription, dashed bool) {
	r.dc.Push()
	defer r.dc.Pop()
	r.dc.SetLineWidth(thickness)
	r.dc.SetLineJoin(textshape.LineJoinMiter)
	r.dc.SetMiterLimit(4)
	r.dc.SetLineCap(textshape.LineCapButt)
	if dash.roundCap {
		r.dc.SetLineCap(textshape.LineCapRound)
	}
	r.dc.SetDash()
	if dashed {
		r.dc.SetDash(dash.on, dash.off)
	}
	r.dc.SetDashPhase(0)
	build(r.dc)
	r.dc.Stroke()
}

// fillRectF fills a rect in the current color.
func (r *Renderer) fillRectF(x, y, w, h float64) {
	if w <= 0 || h <= 0 {
		return
	}
	r.dc.DrawRectangle(x, y, w, h)
	r.dc.Fill()
}
