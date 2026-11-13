package render

import (
	"image"
	"math"

	"mazarin/textshape"

	"louis14/pkg/graphics"
)

// complexOutline paints a non-auto outline around several rects whose
// union is not their first rect: Blink's ComplexOutlinePainter
// (core/paint/outline_painter.cc:438-756 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), reached from
// OutlinePainter::PaintOutlineRects (:1018-1031). The rects outset by
// outline-offset and then outline-width unite into a right-angle path; the
// band between it and the path shrunk by the width is clipped to and
// painted in the outline style, with border-radius baked into both paths.
//
// louis14 folds groove/ridge/inset/outset into solid (pkg/css), so solid,
// double, dashed and dotted are the reachable styles. Not ported:
// dashed/dotted strokes along a rounded path (each straight edge of the
// right-angle center path is dashed instead).
func (r *Renderer) complexOutline(layer *PaintLayer, rects []image.Rectangle) {
	// LayoutObject::OutlineInfo's int width and offset truncate.
	width, offset := int(layer.OutlineWidth), int(layer.OutlineOffset)
	if width == 0 {
		return
	}
	style := layer.OutlineStyle
	if width <= 2 && style == "double" {
		style = "solid"
	}
	outer := rightAnglePath(rects, offset, width)
	if len(outer) == 0 {
		return
	}
	// path traces contours, with border-radius outset to the given convex
	// and concave outsets baked in (ComputeRadii, :666-669).
	path := func(contours [][]image.Point, convexOutset, concaveOutset int) {
		for _, c := range contours {
			if !hasBorderRadius(layer) {
				addContourPath(r.dc, c)
				continue
			}
			cv, cc := float64(offset+convexOutset), float64(offset+concaveOutset)
			addCornerRadiiToPath(r.dc, c, layer.BorderRadius.Outset(cv, cv, cv, cv), layer.BorderRadius.Outset(cc, cc, cc, cc))
		}
	}
	// A translucent dashed or dotted outline paints opaque into a layer at
	// its color's alpha, so dashes overlapping at corners composite once
	// (:474-479).
	color := layer.OutlineColor
	if color.A < 1 && style != "solid" && style != "double" {
		alpha := color.A
		color.A = 1
		r.dc.PushGroup()
		defer r.dc.PopGroupWithAlpha(alpha)
	}
	bounds := contoursBounds(outer)
	fillBounds := func() {
		r.fillIntRect(intRect{bounds.Min.X, bounds.Min.Y, bounds.Dx(), bounds.Dy()}, color)
	}

	r.dc.Push()
	defer r.dc.Pop()
	r.dc.SetFillRule(textshape.FillRuleWinding)
	path(outer, width, 0)
	clipPath(r.dc, graphics.AntiAliased, textshape.ClipOpIntersect)
	path(shrinkRightAnglePath(outer, width), 0, width)
	r.clipOutTracedPath(bounds)
	r.setColor(color)

	switch style {
	case "double":
		// PaintDoubleOutline (:528-548).
		stripe := int(math.Round(float64(width) / 3))
		path(shrinkRightAnglePath(outer, width-stripe), stripe, width-stripe)
		r.dc.Fill()
		path(shrinkRightAnglePath(outer, stripe), width-stripe, stripe)
		r.clipOutTracedPath(bounds)
		fillBounds()
	case "dashed", "dotted":
		// PaintDottedOrDashedOutline (:550-584), edge by edge along the
		// center path (PaintStraightEdge, :727-746).
		center := shrinkRightAnglePath(outer, width/2)
		st := dashedStroke
		if style == "dotted" {
			st = dottedStroke
		}
		// An odd width draws wider to fill the clip when the stroke is dashed.
		thickness := float64(width)
		if width%2 == 1 && strokeIsDashed(float64(width), st) {
			thickness = float64(width + 2)
		}
		joint := (width + 1) / 2
		for _, c := range center {
			for i, start := range c {
				end := c[(i+1)%len(c)]
				if start.X > end.X || start.Y > end.Y {
					start, end = end, start
				}
				if start.X == end.X {
					start.Y, end.Y = start.Y-joint, end.Y+joint
				} else {
					start.X, end.X = start.X-joint, end.X+joint
				}
				r.drawLineWithStyle(start, end, st, thickness)
			}
		}
	default:
		fillBounds()
	}
}

// clipOutTracedPath clips out the area the path traced on r.dc encloses:
// Blink's MakeClipOutPath (outline_painter.cc:651-664), which adds a
// counter-clockwise rect around bounds so that under the non-zero rule
// clockwise-enclosed areas fall out while areas enclosed by
// counter-clockwise crossings stay in.
func (r *Renderer) clipOutTracedPath(bounds image.Rectangle) {
	b := bounds
	r.dc.MoveTo(float64(b.Min.X), float64(b.Min.Y))
	r.dc.LineTo(float64(b.Min.X), float64(b.Max.Y))
	r.dc.LineTo(float64(b.Max.X), float64(b.Max.Y))
	r.dc.LineTo(float64(b.Max.X), float64(b.Min.Y))
	r.dc.ClosePath()
	clipPath(r.dc, graphics.AntiAliased, textshape.ClipOpIntersect)
}

// contoursBounds is the bounding box of right-angle contours.
func contoursBounds(contours [][]image.Point) image.Rectangle {
	b := image.Rectangle{Min: contours[0][0], Max: contours[0][0]}
	for _, c := range contours {
		for _, p := range c {
			b.Min.X, b.Min.Y = min(b.Min.X, p.X), min(b.Min.Y, p.Y)
			b.Max.X, b.Max.Y = max(b.Max.X, p.X), max(b.Max.Y, p.Y)
		}
	}
	return b
}
